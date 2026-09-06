//go:build darwin && arm64

package main

import "embed"

// macOS Apple Silicon build bundles the official easytier core/CLI.

//go:embed all:resources/bin-darwin-arm64
var embeddedBin embed.FS

const embeddedBinRoot = "resources/bin-darwin-arm64"

var embeddedDrivers embed.FS

const embeddedDriversRoot = ""

const haveEmbeddedCore = true
