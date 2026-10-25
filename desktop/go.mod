// The module path must start with "bailing/" so it may import
// bailing/internal/... (Go's internal-package rule is path based).
module bailing/desktop

go 1.26.0

// Desktop shell (Wails v2). A separate module so the server build never
// needs Wails or cgo. After cloning, run in this folder:
//
//	go get github.com/wailsapp/wails/v2@latest
//	go mod tidy
//
// The bailing module (one level up) is used through the replace below.

require bailing v0.0.0

replace bailing => ../


