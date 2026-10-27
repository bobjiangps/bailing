# 百灵 bailing — run these yourself (see docs/dev-guide.md).
# Windows without make: use the equivalent PowerShell commands in docs/dev-guide.md.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath
DIST    := dist
CONFIG  ?= config.dev.yaml
# edition: all | photos | music (front end, PWA manifest and desktop app)
APP     ?= all
# Wails CLI: from PATH, else where `go install` puts it ($GOPATH/bin, usually ~/go/bin)
WAILS   ?= $(shell command -v wails 2>/dev/null || echo "$$(go env GOPATH)/bin/wails")

.PHONY: help deps dev-api dev-web web build build-linux build-linux-arm64 build-windows build-mac test lint fmt clean desktop-deps desktop-doctor desktop desktop-windows desktop-dev

help:
	@echo "make deps              go mod tidy + npm install"
	@echo "make dev-api           run Go API on :8080 with $(CONFIG)"
	@echo "make dev-web           run Vite dev server on :5173"
	@echo "make web               build front end into internal/webui/dist"
	@echo "make build             build for this machine -> dist/bailing"
	@echo "make build-linux       Ubuntu x86_64 -> dist/bailing-linux-amd64"
	@echo "make build-linux-arm64 ARM ECS -> dist/bailing-linux-arm64"
	@echo "make build-windows     -> dist/bailing-windows-amd64.exe"
	@echo "make desktop           desktop app (Wails) -> desktop/build/bin"
	@echo "make desktop-windows   Windows desktop app built on this Mac -> desktop/build/bin/*.exe"
	@echo "  APP=photos|music      build only the albums or only the music edition"
	@echo "                        (web, build*, desktop; e.g. make web APP=music)"
	@echo "make test / lint / fmt / clean"

deps:
	go mod tidy
	cd web && npm install

dev-api:
	go run ./cmd/bailing serve -config $(CONFIG)

dev-web:
	cd web && VITE_BAILING_APP=$(APP) npm run dev

web:
	cd web && VITE_BAILING_APP=$(APP) npm run build
	@touch internal/webui/dist/.gitkeep

build: web
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/bailing ./cmd/bailing

build-linux: web
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/bailing-linux-amd64 ./cmd/bailing

build-linux-arm64: web
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/bailing-linux-arm64 ./cmd/bailing

build-windows: web
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/bailing-windows-amd64.exe ./cmd/bailing

build-mac: web
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/bailing-darwin-arm64 ./cmd/bailing
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/bailing-darwin-amd64 ./cmd/bailing

test:
	go test ./...
	cd web && npm run check

lint:
	go vet ./...
	cd web && npm run check

fmt:
	gofmt -w cmd internal

clean:
	rm -rf $(DIST)
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete

# ---- desktop (Wails v2, needs cgo + the Wails CLI; see docs/dev-guide.md) ----
desktop-deps:
	cd desktop && go get github.com/wailsapp/wails/v2@latest && go mod tidy

# runs `wails doctor` even when ~/go/bin is not on PATH
desktop-doctor:
	"$(WAILS)" doctor

desktop: web
	cp desktop/build/appicon-$(APP).png desktop/build/appicon.png
	cd desktop && "$(WAILS)" build -clean -ldflags "-X main.edition=$(APP) -X main.version=$(VERSION)" -o bailing-$(APP)

# Windows desktop app built on macOS / Linux (cross compile; the Windows
# target needs no cgo). build/windows is removed first so Wails regenerates
# icon.ico from this edition's appicon.png.
# -> desktop/build/bin/bailing-$(APP).exe  (needs WebView2 on the PC; Win10/11 have it)
desktop-windows: web
	cp desktop/build/appicon-$(APP).png desktop/build/appicon.png
	rm -rf desktop/build/windows
	cd desktop && "$(WAILS)" build -clean -platform windows/amd64 -ldflags "-X main.edition=$(APP) -X main.version=$(VERSION)" -o bailing-$(APP).exe

desktop-dev: web
	cd desktop && "$(WAILS)" dev -ldflags "-X main.edition=$(APP)"
