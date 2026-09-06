#!/bin/bash
# Reproduce the CI mkdeb flow locally (WSL).
set -e
export PATH=/usr/local/go/bin:$PATH
cd /mnt/d/easytierpro/easytier-pro-gui
VER=2.6.4
go build -o /tmp/mkdeb ./build/mkdeb
/tmp/mkdeb -out /tmp/srv.deb -bin build/bin/easytier-pro-server-linux-amd64 -arch amd64 -version "$VER" && echo SERVER-DEB-OK
/tmp/mkdeb -out /tmp/gui.deb -bin build/bin/easytier-pro-gui-linux-amd64 -arch amd64 -version "$VER" -mode gui -ico build/appicon.png && echo GUI-DEB-OK
