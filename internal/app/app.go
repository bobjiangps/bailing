// Package app wires all services together. Both the web server
// (cmd/bailing) and the future desktop shell (cmd/bailing-desktop) use it.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"bailing/internal/auth"
	"bailing/internal/config"
	"bailing/internal/imaging"
	"bailing/internal/library/music"
	"bailing/internal/library/photo"
	"bailing/internal/lyrics"
	"bailing/internal/netguard"
	"bailing/internal/proxy"
	"bailing/internal/sources"
	"bailing/internal/sources/direct"
	"bailing/internal/sources/local"
	"bailing/internal/sources/lx"
	"bailing/internal/sources/radio"
	"bailing/internal/stats"
	"bailing/internal/store"
	"bailing/internal/traffic"
)

// App holds every long-lived service.
type App struct {
	Cfg     *config.Config
	Log     *slog.Logger
	Version string

	DB      *store.DB
	Auth    *auth.Service
	Vips    *imaging.Vips
	Queue   *imaging.Queue
	Photos  *photo.Library
	Music   *music.Library
	Lyrics  *lyrics.Service
	Stats   *stats.Service
	Traffic *traffic.Meter
	Sources *sources.Registry

	// P2 / P3: online sources
	HTTP       *http.Client // outbound API calls (SSRF-guarded, 20s timeout)
	StreamHTTP *http.Client // relayed streams (SSRF-guarded, no overall timeout)
	Radio      *radio.Source
	Signer     *proxy.Signer
	LX         *lx.Manager // nil when sources.lx.enabled = false

	stop     chan struct{}
	wg       sync.WaitGroup
	scanMu   sync.Mutex
	lastScan time.Time
}

// New opens the database and constructs all services.
func New(cfg *config.Config, log *slog.Logger, version string) (*App, error) {
	if err := os.MkdirAll(cfg.Paths.Data, 0o755); err != nil {
		return nil, err
	}
	db, err := store.Open(cfg.DataPath("bailing.db"))
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Log: log, Version: version, DB: db, stop: make(chan struct{})}

	lr := cfg.Auth.LoginRate
	a.Auth = auth.NewService(db, cfg.Auth.SessionDays, cfg.Auth.Mode == "none", lr.MaxFailures,
		config.Dur(lr.Window, 10*time.Minute), config.Dur(lr.Lock, 15*time.Minute))

	a.Vips = imaging.NewVips(cfg.Imaging.VipsBinDir, cfg.Imaging.Nice, cfg.Imaging.Quality)
	a.Queue = imaging.NewQueue(cfg.Imaging.Workers)
	a.Photos = photo.New(db, cfg.Paths.Photos, cfg.DataPath("cache", "photo"), cfg.Scan.PhotoExts,
		a.Vips, a.Queue, cfg.Imaging.Sizes.S, cfg.Imaging.Sizes.M, log.With("mod", "photo"))
	a.Music = music.New(db, cfg.Paths.Music, cfg.DataPath("cache", "cover"), cfg.Scan.MusicExts,
		a.Vips, a.Queue, log.With("mod", "music"))
	a.Lyrics = lyrics.New(db, a.Music, lyrics.Options{
		Online:          cfg.Lyrics.Online,
		LRCLibBase:      cfg.Lyrics.LRCLibBase,
		NegativeTTL:     config.Dur(cfg.Lyrics.NegativeTTL, 7*24*time.Hour),
		Timeout:         config.Dur(cfg.Lyrics.Timeout, 8*time.Second),
		SaveBesideMusic: cfg.Lyrics.SaveBesideMusic,
		Version:         version,
	}, log.With("mod", "lyrics"))
	a.Stats = stats.New(db)
	a.Traffic = traffic.New(db, cfg.Traffic.MonthlyQuotaGB, cfg.Traffic.WarnPercent,
		cfg.Traffic.ResetDay, log.With("mod", "traffic"))

	a.Sources = sources.NewRegistry()
	a.Sources.Register(local.New(a.Music, cfg.Server.BasePath))

	// outbound clients with SSRF protection: short requests and long streams
	allowPriv := cfg.Sources.Proxy.AllowPrivate
	a.HTTP = netguard.NewClient(netguard.Options{AllowPrivate: allowPriv, Timeout: 20 * time.Second})
	a.StreamHTTP = netguard.NewClient(netguard.Options{AllowPrivate: allowPriv})

	a.Sources.Register(direct.New(a.HTTP))
	if cfg.Sources.Radio.Enabled && cfg.Features.Music {
		a.Radio = radio.New(cfg.Sources.Radio.APIBase, version, a.HTTP)
		a.Sources.Register(a.Radio)
	}
	key, err := proxy.LoadOrCreateKey(cfg.DataPath("relay.key"))
	if err != nil {
		return nil, err
	}
	a.Signer = proxy.NewSigner(key, cfg.Server.BasePath, config.Dur(cfg.Sources.Proxy.LinkTTL, 24*time.Hour))
	if cfg.Sources.LX.Enabled && cfg.Features.Music {
		a.LX = lx.NewManager(cfg.Sources.LX.ScriptsDir, db, a.HTTP,
			config.Dur(cfg.Sources.LX.RequestTimeout, 15*time.Second),
			config.Dur(cfg.Sources.LX.CacheTTL, 10*time.Minute), log.With("mod", "lx"))
		for _, p := range cfg.Sources.LX.Platforms {
			a.Sources.Register(lx.NewSource(p, a.LX, a.HTTP))
		}
	}

	if v, err := a.Vips.Version(); err != nil {
		log.Warn("libvips not found: thumbnails / HEIC will not work", "err", err)
	} else {
		log.Info("libvips", "version", v)
	}
	return a, nil
}

// Start launches background loops: initial scan, periodic scan, traffic
// flushing and session cleanup.
func (a *App) Start() {
	if a.LX != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.LX.Run(a.stop)
		}()
	}
	a.wg.Add(2)
	go func() {
		defer a.wg.Done()
		a.Traffic.Run(30*time.Second, a.stop)
	}()
	go func() {
		defer a.wg.Done()
		if a.Cfg.Scan.OnStart {
			a.Scan("all")
		}
		interval := config.Dur(a.Cfg.Scan.Interval, 6*time.Hour)
		tick := time.Hour
		if interval > 0 && interval < tick {
			tick = interval
		}
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-a.stop:
				return
			case <-t.C:
				a.Auth.PurgeExpired()
				if interval > 0 && time.Since(a.LastScan()) >= interval {
					a.Scan("all")
				}
			}
		}
	}()
}

// Scan runs a scan synchronously. what = photos | music | all.
func (a *App) Scan(what string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-a.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	if (what == "photos" || what == "all") && a.Cfg.Features.Photos {
		if err := a.Photos.Scan(ctx); err != nil {
			a.Log.Warn("photo scan", "err", err)
		}
	}
	if (what == "music" || what == "all") && a.Cfg.Features.Music {
		if err := a.Music.Scan(ctx); err != nil {
			a.Log.Warn("music scan", "err", err)
		}
	}
	a.scanMu.Lock()
	a.lastScan = time.Now()
	a.scanMu.Unlock()
}

// LastScan returns the time the last scan finished.
func (a *App) LastScan() time.Time {
	a.scanMu.Lock()
	defer a.scanMu.Unlock()
	return a.lastScan
}

// Close stops background work and closes the database.
func (a *App) Close() {
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
	a.wg.Wait()
	a.Queue.Close()
	a.Traffic.Flush()
	a.DB.Close()
}
