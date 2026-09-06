package main

import (
	"embed"
	"io/fs"
)

// Embedded web UI (built frontend). Shared by the desktop app and the
// headless server binary.
//
//go:embed all:frontend/dist
var assets embed.FS

// webAssets is the frontend tree rooted for the http.FileServer.
var webAssets fs.FS

func init() {
	var err error
	webAssets, err = fs.Sub(assets, "frontend/dist")
	if err != nil {
		panic(err)
	}
}
