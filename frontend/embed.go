// SPDX-License-Identifier: MIT

// Package frontend embeds the user interface of the desktop application.
// Run "make frontend" before building the binary; a fresh checkout holds
// only a placeholder, which is enough to compile and test the Go code.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist/wails
var wails embed.FS

// Wails returns the build for the desktop application, rooted at its
// index.html.
func Wails() fs.FS {
	s, err := fs.Sub(wails, "dist/wails")
	if err != nil {
		panic(err) // the directory is embedded above
	}
	return s
}

// Built reports whether assets holds a real build rather than the
// placeholder of a fresh checkout.
func Built(assets fs.FS) bool {
	_, err := fs.Stat(assets, "index.html")
	return err == nil
}
