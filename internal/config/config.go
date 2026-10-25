// Package config loads and validates config.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Config is the root of config.yaml. Durations are kept as strings in YAML
// ("10m", "6h") and converted by the helper methods below.
type Config struct {
	Server   Server   `yaml:"server"`
	Paths    Paths    `yaml:"paths"`
	Auth     Auth     `yaml:"auth"`
	Scan     Scan     `yaml:"scan"`
	Imaging  Imaging  `yaml:"imaging"`
	Lyrics   Lyrics   `yaml:"lyrics"`
	Traffic  Traffic  `yaml:"traffic"`
	Sources  Sources  `yaml:"sources"`
	Features Features `yaml:"features"`
	LogLevel string   `yaml:"log_level"`
	file     string
}

type Server struct {
	Listen         string   `yaml:"listen"`
	BaseURL        string   `yaml:"base_url"`
	BasePath       string   `yaml:"base_path"` // e.g. "/bailing" when served under a sub path
	TrustedProxies []string `yaml:"trusted_proxies"`
	AllowedOrigins []string `yaml:"allowed_origins"` // extra origins for dev (vite :5173)
}

type Paths struct {
	// Photos / Music: one directory or a list of directories. All of them
	// are loaded as one library (see library.Roots).
	Photos PathList `yaml:"photos"`
	Music  PathList `yaml:"music"`
	Data   string   `yaml:"data"`
}

// PathList accepts a single path ("~/Music") or a list (["~/Music", "/mnt/nas/music"]).
type PathList []string

// UnmarshalYAML implements yaml.InterfaceUnmarshaler.
func (p *PathList) UnmarshalYAML(unmarshal func(any) error) error {
	var one string
	if err := unmarshal(&one); err == nil {
		*p = nil
		if strings.TrimSpace(one) != "" {
			*p = PathList{one}
		}
		return nil
	}
	var many []string
	if err := unmarshal(&many); err != nil {
		return fmt.Errorf("must be a path or a list of paths: %w", err)
	}
	*p = many
	return nil
}

type Auth struct {
	Mode        string    `yaml:"mode"` // password | none
	SessionDays int       `yaml:"session_days"`
	LoginRate   LoginRate `yaml:"login_rate"`
}

type LoginRate struct {
	MaxFailures int    `yaml:"max_failures"`
	Window      string `yaml:"window"`
	Lock        string `yaml:"lock"`
}

type Scan struct {
	OnStart   bool     `yaml:"on_start"`
	Interval  string   `yaml:"interval"`
	PhotoExts []string `yaml:"photo_exts"`
	MusicExts []string `yaml:"music_exts"`
}

type Imaging struct {
	VipsBinDir string `yaml:"vips_bin_dir"`
	Workers    int    `yaml:"workers"`
	Nice       int    `yaml:"nice"`
	Sizes      Sizes  `yaml:"sizes"`
	Quality    int    `yaml:"quality"`
}

type Sizes struct {
	S int `yaml:"s"`
	M int `yaml:"m"`
}

type Lyrics struct {
	Online          bool   `yaml:"online"`
	LRCLibBase      string `yaml:"lrclib_base"`
	NegativeTTL     string `yaml:"negative_ttl"`
	Timeout         string `yaml:"timeout"`
	SaveBesideMusic bool   `yaml:"save_beside_music"`
}

type Traffic struct {
	MonthlyQuotaGB float64 `yaml:"monthly_quota_gb"` // 0 = no quota
	WarnPercent    int     `yaml:"warn_percent"`
	BlockProxyOver bool    `yaml:"block_proxy_over_quota"`
	ResetDay       int     `yaml:"reset_day"` // day of month the billing month starts (1-28)
}

type Sources struct {
	Proxy ProxyCfg `yaml:"proxy"`
	Radio RadioCfg `yaml:"radio"`
	LX    LXCfg    `yaml:"lx"`
}

type ProxyCfg struct {
	Enabled bool `yaml:"enabled"`
	// MaxConcurrent limits simultaneous relayed audio streams (HLS segment
	// requests are short and not limited).
	MaxConcurrent int `yaml:"max_concurrent"`
	// LinkTTL is how long a signed relay link stays valid.
	LinkTTL string `yaml:"link_ttl"`
	// AllowPrivate lets direct URLs / relays reach LAN addresses
	// (192.168.x.x …). Keep false on a public server (SSRF protection).
	AllowPrivate bool `yaml:"allow_private"`
}

type RadioCfg struct {
	Enabled bool   `yaml:"enabled"`
	APIBase string `yaml:"api_base"`
}

type LXCfg struct {
	Enabled        bool     `yaml:"enabled"`
	ScriptsDir     string   `yaml:"scripts_dir"`
	Platforms      []string `yaml:"platforms"`
	RequestTimeout string   `yaml:"request_timeout"`
	// CacheTTL keeps resolved song URLs (they usually expire upstream).
	CacheTTL string `yaml:"cache_ttl"`
}

// Features switches the two halves of the app. A photos-only or music-only
// server skips scanning, routes and navigation of the other half, so the
// same binary can be deployed (and packaged as PWA / desktop) separately.
type Features struct {
	Photos bool `yaml:"photos"`
	Music  bool `yaml:"music"`
}

// Default returns a config with sensible defaults; Load overlays the file on top.
func Default() *Config {
	return &Config{
		Server: Server{Listen: "127.0.0.1:8080", TrustedProxies: []string{"127.0.0.1", "::1"}},
		Paths:  Paths{Data: "./data"},
		Auth: Auth{Mode: "password", SessionDays: 30,
			LoginRate: LoginRate{MaxFailures: 5, Window: "10m", Lock: "15m"}},
		Scan: Scan{OnStart: true, Interval: "6h",
			PhotoExts: []string{"jpg", "jpeg", "png", "webp", "heic", "heif", "gif"},
			MusicExts: []string{"mp3", "m4a", "aac", "flac", "ogg", "opus", "wav"}},
		Imaging: Imaging{Workers: 1, Nice: 10, Sizes: Sizes{S: 480, M: 2048}, Quality: 80},
		Lyrics: Lyrics{Online: true, LRCLibBase: "https://lrclib.net",
			NegativeTTL: "168h", Timeout: "8s", SaveBesideMusic: true},
		Traffic: Traffic{WarnPercent: 80, ResetDay: 1, BlockProxyOver: true},
		Sources: Sources{
			Proxy: ProxyCfg{Enabled: true, MaxConcurrent: 4, LinkTTL: "24h"},
			Radio: RadioCfg{Enabled: true, APIBase: "https://de1.api.radio-browser.info"},
			LX: LXCfg{Enabled: true, Platforms: []string{"kw", "kg", "tx", "wy", "mg"},
				RequestTimeout: "15s", CacheTTL: "10m"},
		},
		Features: Features{Photos: true, Music: true},
		LogLevel: "info",
	}
}

// Load reads the YAML file at path (if it exists) over the defaults.
func Load(path string) (*Config, error) {
	c := Default()
	c.file = path
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := c.normalize(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) normalize() error {
	base := filepath.Dir(c.file)
	abs := func(p string) string {
		if p == "" {
			return ""
		}
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		return filepath.Clean(p)
	}
	absList := func(list PathList) PathList {
		var out PathList
		seen := map[string]bool{}
		for _, p := range list {
			if strings.TrimSpace(p) == "" {
				continue
			}
			p = abs(p)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		return out
	}
	c.Paths.Photos = absList(c.Paths.Photos)
	c.Paths.Music = absList(c.Paths.Music)
	c.Paths.Data = abs(c.Paths.Data)
	if c.Paths.Data == "" {
		return fmt.Errorf("paths.data is required")
	}
	if c.Sources.LX.ScriptsDir == "" {
		c.Sources.LX.ScriptsDir = filepath.Join(c.Paths.Data, "lx-scripts")
	} else {
		c.Sources.LX.ScriptsDir = abs(c.Sources.LX.ScriptsDir)
	}
	if c.Sources.Proxy.MaxConcurrent < 1 {
		c.Sources.Proxy.MaxConcurrent = 1
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
	bp := strings.Trim(c.Server.BasePath, "/")
	if bp != "" {
		bp = "/" + bp
	}
	c.Server.BasePath = bp
	if c.Auth.Mode != "password" && c.Auth.Mode != "none" {
		return fmt.Errorf("auth.mode must be password or none")
	}
	if c.Auth.Mode == "none" && !isLoopback(c.Server.Listen) {
		return fmt.Errorf("auth.mode=none is only allowed when listening on 127.0.0.1/localhost")
	}
	if !c.Features.Photos && !c.Features.Music {
		return fmt.Errorf("features: enable at least one of photos / music")
	}
	if c.Imaging.Workers < 1 {
		c.Imaging.Workers = 1
	}
	if c.Traffic.ResetDay < 1 || c.Traffic.ResetDay > 28 {
		c.Traffic.ResetDay = 1
	}
	for i, e := range c.Scan.PhotoExts {
		c.Scan.PhotoExts[i] = strings.ToLower(strings.TrimPrefix(e, "."))
	}
	for i, e := range c.Scan.MusicExts {
		c.Scan.MusicExts[i] = strings.ToLower(strings.TrimPrefix(e, "."))
	}
	return nil
}

func isLoopback(listen string) bool {
	return strings.HasPrefix(listen, "127.") || strings.HasPrefix(listen, "localhost") ||
		strings.HasPrefix(listen, "[::1]")
}

// Dur parses a duration string, returning def when empty or invalid.
// "0" means disabled and returns 0.
func Dur(s string, def time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if s == "0" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// SecureCookies reports whether cookies must carry the Secure flag.
func (c *Config) SecureCookies() bool {
	return strings.HasPrefix(c.Server.BaseURL, "https://")
}

// File is the path of the loaded configuration file.
func (c *Config) File() string { return c.file }

// DataPath joins elements under the data directory.
func (c *Config) DataPath(elem ...string) string {
	return filepath.Join(append([]string{c.Paths.Data}, elem...)...)
}
