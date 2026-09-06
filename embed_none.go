//go:build !windows && !(linux && amd64) && !(darwin && amd64) && !(darwin && arm64)

package main

import "embed"

// Fallback for platform/arch combinations without a bundled core: the GUI
// expects a system-wide easytier-core/easytier-cli on PATH instead.
var (
	embeddedBin     embed.FS
	embeddedDrivers embed.FS
)

const (
	embeddedBinRoot     = ""
	embeddedDriversRoot = ""
	haveEmbeddedCore    = false
)
