//go:build darwin && amd64

package main

import "embed"

// macOS Intel build bundles the official easytier core/CLI.

//go:embed all:resources/bin-darwin-amd64
var embeddedBin embed.FS

const embeddedBinRoot = "resources/bin-darwin-amd64"

var embeddedDrivers embed.FS

const embeddedDriversRoot = ""

const haveEmbeddedCore = true
