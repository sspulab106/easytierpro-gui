//go:build linux && amd64

package main

import "embed"

// Linux amd64 build bundles the official easytier core/CLI (v2.4.0 release).
// No TUN driver files are needed on Linux (kernel provides /dev/net/tun).

//go:embed all:resources/bin-linux-amd64
var embeddedBin embed.FS

const embeddedBinRoot = "resources/bin-linux-amd64"

var embeddedDrivers embed.FS

const embeddedDriversRoot = ""

const haveEmbeddedCore = true
