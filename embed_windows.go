//go:build windows

package main

import "embed"

// Windows build bundles the official easytier core/CLI plus TUN drivers.

//go:embed all:resources/bin
var embeddedBin embed.FS

const embeddedBinRoot = "resources/bin"

//go:embed all:resources/drivers
var embeddedDrivers embed.FS

const embeddedDriversRoot = "resources/drivers"

const haveEmbeddedCore = true
