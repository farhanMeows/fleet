// Package webdist embeds the built dashboard (dist/) into the fleet binary.
// Run `make web` (npm run build in this directory) before `go build` to
// refresh the embedded assets.
package webdist

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the dashboard filesystem rooted at the dist directory.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
