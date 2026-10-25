// Command bailing-desktop is the desktop edition of 百灵 (Wails v2).
//
// It runs the normal bailing server in-process on a fixed loopback port
// (no login, auth.mode none) and shows it in a native window. The web front
// end embedded in the bailing module is used unchanged, so the desktop app
// looks and behaves exactly like the website.
//
// Editions: the same source builds three apps, chosen at build time
//
//	-ldflags "-X main.edition=all"      百灵 (albums + music)
//	-ldflags "-X main.edition=photos"   百灵相册
//	-ldflags "-X main.edition=music"    百灵音乐
//
// The front end must be built for the same edition first
// (make web APP=photos), see docs/dev-guide.md "桌面端".
//
// Remote mode: set `remote_url` in desktop.yaml to open your family server
// instead of a local library (the window then simply shows that site).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bailing/internal/app"
	"bailing/internal/config"
	"bailing/internal/httpapi"

	"github.com/goccy/go-yaml"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// edition is set at build time: all | photos | music.
var edition = "all"

var version = "dev"

type editionInfo struct {
	title, dir, start string
	port              int
	photos, music     bool
}

var editions = map[string]editionInfo{
	"all":    {title: "百灵", dir: "Bailing", start: "/", port: 47821, photos: true, music: true},
	"photos": {title: "百灵相册", dir: "Bailing Photos", start: "/albums", port: 47822, photos: true},
	"music":  {title: "百灵音乐", dir: "Bailing Music", start: "/music", port: 47823, music: true},
}

// desktopCfg is desktop.yaml next to config.yaml (user-editable).
type desktopCfg struct {
	// RemoteURL opens a bailing server instead of the local library.
	RemoteURL string `yaml:"remote_url"`
	Width     int    `yaml:"width"`
	Height    int    `yaml:"height"`
}

func main() {
	ed, ok := editions[edition]
	if !ok {
		ed = editions["all"]
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(ed, log); err != nil {
		log.Error("bailing desktop", "err", err)
		os.Exit(1)
	}
}

func run(ed editionInfo, log *slog.Logger) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(base, ed.dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dc := loadDesktopCfg(filepath.Join(dir, "desktop.yaml"))

	target := strings.TrimRight(dc.RemoteURL, "/")
	var shutdown func()
	if target == "" {
		url, stop, err := startLocal(ed, dir, log)
		if err != nil {
			return err
		}
		target, shutdown = url, stop
	}
	start := target + ed.start

	// The window loads this tiny page, which hands over to the real site.
	// Using a real loopback HTTP server (not Wails' asset handler) keeps
	// audio range requests, cookies and streaming exactly as in a browser.
	boot := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>
<body style="background:#efebe6;margin:0"><script>location.replace(%q)</script></body>`, ed.title, start)
	})

	return wails.Run(&options.App{
		Title:            ed.title,
		Width:            orDefault(dc.Width, 1280),
		Height:           orDefault(dc.Height, 860),
		MinWidth:         400,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 239, G: 235, B: 230, A: 255},
		AssetServer:      &assetserver.Options{Handler: boot},
		OnShutdown: func(context.Context) {
			if shutdown != nil {
				shutdown()
			}
		},
	})
}

// startLocal runs the bailing server for this edition on 127.0.0.1.
func startLocal(ed editionInfo, dir string, log *slog.Logger) (string, func(), error) {
	cfgPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(cfgPath, []byte(defaultConfig(ed)), 0o644); err != nil {
			return "", nil, err
		}
		log.Info("created config", "path", cfgPath)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return "", nil, err
	}
	// the edition decides the halves; the file can only switch them off
	cfg.Features.Photos = cfg.Features.Photos && ed.photos
	cfg.Features.Music = cfg.Features.Music && ed.music

	a, err := app.New(cfg, log, version)
	if err != nil {
		return "", nil, err
	}
	h, err := httpapi.New(a)
	if err != nil {
		a.Close()
		return "", nil, err
	}
	// A fixed port keeps the same origin between launches, so the browser
	// storage (theme, queue, login-free session) survives restarts.
	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		log.Warn("fixed port busy, using a random one (settings stored in the window will not persist)",
			"listen", cfg.Server.Listen, "err", err)
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			a.Close()
			return "", nil, err
		}
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	a.Start()
	go srv.Serve(ln)
	url := "http://" + ln.Addr().String()
	log.Info("local server", "url", url, "edition", edition)
	return url, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		a.Close()
	}, nil
}

func defaultConfig(ed editionInfo) string {
	home, _ := os.UserHomeDir()
	photos := filepath.ToSlash(filepath.Join(home, "Pictures"))
	music := filepath.ToSlash(filepath.Join(home, "Music"))
	return fmt.Sprintf(`# 百灵 desktop (%s) — edit and restart the app.
server:
  listen: "127.0.0.1:%d"           # fixed loopback port (one per edition)
  base_url: ""

paths:                           # a single folder or a list: ["D:/Music", "E:/More"]
  photos: %q
  music:  %q
  data:   "./data"                 # next to this file

auth:
  mode: "none"                     # single user on this computer

features:
  photos: %t
  music: %t

sources:
  proxy: { enabled: true, max_concurrent: 4, link_ttl: "24h", allow_private: true }
  radio: { enabled: true }
  lx:    { enabled: true, scripts_dir: "", platforms: [kw, kg, tx, wy, mg] }
`, ed.title, ed.port, photos, music, ed.photos, ed.music)
}

func loadDesktopCfg(path string) desktopCfg {
	var c desktopCfg
	if b, err := os.ReadFile(path); err == nil {
		yaml.Unmarshal(b, &c)
	} else if errors.Is(err, os.ErrNotExist) {
		os.WriteFile(path, []byte(`# remote_url: "https://music.example.com"   # open your server instead of local folders
# width: 1280
# height: 860
`), 0o644)
	}
	return c
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}
