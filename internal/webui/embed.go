// Package webui embeds the built SvelteKit front end.
//
// `npm run build` (in web/) writes its output into internal/webui/dist, which
// is compiled into the Go binary here. Before the first front-end build the
// directory only holds .gitkeep; the server then shows a hint page.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built front end rooted at dist/.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Built reports whether index.html exists (i.e. the front end was built).
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
