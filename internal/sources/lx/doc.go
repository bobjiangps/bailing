// Package lx provides LX Music (落雪音乐) custom-source compatibility:
// drop an LX custom-source .js file into <data>/lx-scripts (or upload it in
// Settings) and its platforms become searchable and playable.
//
//	prelude.js        builds the global `lx` object inside the sandbox
//	                  (lx.on / lx.send / lx.request / lx.utils / EVENT_NAMES)
//	runtime.go        one goja VM + event loop per script; timers, HTTP,
//	                  crypto bridged to Go; watchdog for runaway code
//	hostfns.go        crypto (md5, aes ecb/cbc, raw rsa), zlib, HTTP, header
//	manager.go        loads/hot-reloads scripts, enable/disable, resolve+cache
//	platforms.go,
//	platform_impl.go  search adapters for kw / kg / tx / wy (after
//	                  lx-music-desktop, Apache-2.0)
//	source.go         one "lx:<platform>" sources.Source per platform
//
// Security: scripts have no file system, process or require(); lx.request
// can only reach public http(s) addresses (internal/netguard) and every
// synchronous run is interrupted after a few seconds. Only admins can
// upload or enable scripts.
package lx
