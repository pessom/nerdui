// Package web embeds the compiled frontend (web/dist) into the nerdui binary.
//
// Build the frontend with `make web` before `go build`; otherwise only the
// placeholder file is embedded and the UI responds with a hint.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the compiled frontend file system rooted at web/dist.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // unreachable: "dist" is embedded at compile time
	}
	return sub
}
