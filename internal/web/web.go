// Package web holds Encorr's web interface, built into the program file.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// Static returns the web UI files.
func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
