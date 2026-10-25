module bailing

go 1.26.0

// Dependencies are added by `go mod tidy` (run it yourself after cloning):
//   modernc.org/sqlite          pure-Go SQLite
//   github.com/dhowden/tag      audio metadata / cover / lyrics
//   github.com/goccy/go-yaml    config
//   golang.org/x/crypto         argon2id
//   golang.org/x/image          webp decode (dominant colour)
//   golang.org/x/text           GBK lyrics
//   golang.org/x/term           password prompt

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8
	github.com/dop251/goja v0.0.0-20261014135309-2cb8274eed06
	github.com/goccy/go-yaml v1.19.2
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	golang.org/x/term v0.47.0
	golang.org/x/text v0.43.0
	modernc.org/sqlite v1.60.1
)

require (
	github.com/dlclark/regexp2/v2 v2.8.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20260802141513-ef3492d7dac3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.49.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
