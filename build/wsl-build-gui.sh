#!/bin/bash
# Linux desktop GUI build (run inside WSL/Ubuntu-22.04 as root):
#   wsl.exe -d Ubuntu-22.04 -u root -- bash /mnt/d/easytierpro/easytier-pro-gui/build/wsl-build-gui.sh
set -e
export PATH=/usr/local/go/bin:$PATH
export GOCACHE=/root/.cache/go-build
export GOMODCACHE=/root/go/pkg/mod
export GOPATH=/root/go
go env -w GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct

cd /mnt/d/easytierpro/easytier-pro-gui
echo "go: $(go version)"
pkg-config --modversion webkit2gtk-4.0 gtk+-3.0

# Modules are fetched once into the WSL-native cache (/mnt/d is slow for IO).
go mod download

echo "=== building linux GUI (amd64) ==="
CGO_ENABLED=1 go build -trimpath -tags "desktop,production" -ldflags "-s -w" -o build/bin/easytier-pro-gui-linux-amd64 .
echo "BUILD OK: $(ls -la build/bin/easytier-pro-gui-linux-amd64 | awk '{print $5}') bytes"
