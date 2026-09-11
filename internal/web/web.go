// Package web serves the console's frontend from assets embedded into the
// binary at build time, so no external asset files are required at
// runtime.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler serving the embedded frontend bundle,
// including the placeholder UI shell at the root path.
func Handler() (http.Handler, error) {
	assets, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(assets)), nil
}
