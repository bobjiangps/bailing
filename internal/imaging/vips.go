// Package imaging wraps the libvips command line tools and a small job queue.
//
// libvips is invoked as an external process (vips / vipsheader) instead of
// being linked, so the Go binary stays cgo-free and the LGPL licence of
// libvips does not affect this project.
package imaging

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Vips runs vips commands.
type Vips struct {
	binDir  string
	nice    int
	quality int
	useNice bool
}

// NewVips creates a runner. binDir may be empty (use PATH).
func NewVips(binDir string, nice, quality int) *Vips {
	v := &Vips{binDir: binDir, nice: nice, quality: quality}
	if quality <= 0 || quality > 100 {
		v.quality = 80
	}
	if runtime.GOOS != "windows" && nice > 0 {
		if _, err := exec.LookPath("nice"); err == nil {
			v.useNice = true
		}
	}
	return v
}

func (v *Vips) bin(name string) string {
	if v.binDir != "" {
		return filepath.Join(v.binDir, name)
	}
	return name
}

func (v *Vips) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if v.useNice {
		cmd = exec.CommandContext(ctx, "nice",
			append([]string{"-n", strconv.Itoa(v.nice), v.bin(name)}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, v.bin(name), args...)
	}
	// One thread per vips process: keeps burstable-instance CPU credits in check.
	cmd.Env = append(os.Environ(), "VIPS_CONCURRENCY=1")
	return cmd
}

// Version returns `vips --version` output or an error when vips is missing.
func (v *Vips) Version() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, v.bin("vips"), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("vips not available: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Thumbnail writes a WebP whose long edge is at most size pixels.
// It never upscales and applies EXIF orientation (vips default).
// The file is written to a temp name first and renamed, so readers never see
// a half-written file.
func (v *Vips) Thumbnail(ctx context.Context, in, out string, size int) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".tmp.webp"
	target := fmt.Sprintf("%s[Q=%d,strip]", tmp, v.quality)
	cmd := v.command(ctx, "vips", "thumbnail", in, target, strconv.Itoa(size),
		"--size", "down")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("vips thumbnail %s: %v: %s", filepath.Base(in), err,
			strings.TrimSpace(stderr.String()))
	}
	return os.Rename(tmp, out)
}

// Header returns the fields printed by `vipsheader -a` as a map.
// Values look like "Apple (Apple, ASCII, 6 components, 6 bytes)".
func (v *Vips) Header(ctx context.Context, in string) (map[string]string, error) {
	cmd := v.command(ctx, "vipsheader", "-a", in)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("vipsheader %s: %w", filepath.Base(in), err)
	}
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		k, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return m, nil
}
