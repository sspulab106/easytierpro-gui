#!/bin/bash
set -e
VER=1.25.4
cd /tmp
for url in "https://golang.google.cn/dl/go${VER}.linux-amd64.tar.gz" "https://mirrors.aliyun.com/golang/go${VER}.linux-amd64.tar.gz" "https://go.dev/dl/go${VER}.linux-amd64.tar.gz"; do
  echo "trying $url"
  if curl -fsSL --connect-timeout 15 -o /tmp/go.tgz "$url"; then
    echo "DOWNLOADED $(du -h /tmp/go.tgz | cut -f1)"
    break
  fi
done
[ -s /tmp/go.tgz ] || { echo "NO TARBALL"; exit 1; }
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tgz
/usr/local/go/bin/go version
