#!/usr/bin/env bash
# ============================================================
# EasyTier Pro server installer (headless Linux)
#
# Installs the server binary, creates a data dir, registers a
# systemd service (auto-start + auto-restart = process daemon),
# and prints the web management address + first-run token.
#
# Usage:
#   sudo ./install-server.sh [path-to-easytier-pro-server-binary]
#
# If no path is given, the script downloads the latest release
# binary from the URL below (edit RELEASE_URL or pre-place the
# binary next to this script).
# ============================================================
set -euo pipefail

BINARY_SRC="${1:-}"
INSTALL_DIR="/usr/local/bin"
BIN_NAME="easytier-pro-server"
DATA_DIR="/var/lib/easytier-pro"
SERVICE_NAME="easytier-pro"
RELEASE_URL="${RELEASE_URL:-}"

# --- pick the binary -------------------------------------------------------
if [ -z "$BINARY_SRC" ]; then
  # Same directory as this script?
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  for cand in "$SCRIPT_DIR/easytier-pro-server" \
              "$SCRIPT_DIR/easytier-pro-server-linux-amd64" \
              "$SCRIPT_DIR/easytier-pro-server-linux-arm64"; do
    if [ -f "$cand" ]; then BINARY_SRC="$cand"; break; fi
  done
fi
if [ -z "$BINARY_SRC" ] && [ -n "$RELEASE_URL" ]; then
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64)  SUFFIX="amd64" ;;
    aarch64) SUFFIX="arm64" ;;
    *) echo "unsupported arch: $ARCH"; exit 1 ;;
  esac
  echo ">> downloading $RELEASE_URL/easytier-pro-server-linux-$SUFFIX"
  mkdir -p /tmp/etpro-install
  curl -fL "$RELEASE_URL/easytier-pro-server-linux-$SUFFIX" -o /tmp/etpro-install/$BIN_NAME
  BINARY_SRC="/tmp/etpro-install/$BIN_NAME"
fi
if [ -z "$BINARY_SRC" ] || [ ! -f "$BINARY_SRC" ]; then
  echo "usage: sudo ./install-server.sh <path-to-easytier-pro-server-binary>"
  echo "  or place easytier-pro-server next to this script."
  exit 1
fi

[ "$(id -u)" -eq 0 ] || { echo "must run as root (sudo)"; exit 1; }

# --- install ---------------------------------------------------------------
echo ">> installing binary to $INSTALL_DIR/$BIN_NAME"
install -m 0755 "$BINARY_SRC" "$INSTALL_DIR/$BIN_NAME"

echo ">> creating data dir $DATA_DIR"
mkdir -p "$DATA_DIR"

# --- systemd unit (process daemon: auto-start at boot, restart on crash) ---
echo ">> writing systemd service $SERVICE_NAME"
cat > /etc/systemd/system/$SERVICE_NAME.service <<UNIT
[Unit]
Description=EasyTier Pro server (web-managed EasyTier control plane)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$BIN_NAME --bind 0.0.0.0 --port 56000 --data-dir $DATA_DIR
Restart=always
RestartSec=3
# easytier-core needs root to create the TUN adapter
User=root
# tidy process handling
KillSignal=SIGTERM
TimeoutStopSec=15
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable $SERVICE_NAME
systemctl restart $SERVICE_NAME
sleep 2
systemctl --no-pager status $SERVICE_NAME | head -8

# --- first-run info --------------------------------------------------------
echo
echo "============================================================"
echo " EasyTier Pro server installed."
echo " Web management: http://$(hostname -I 2>/dev/null | awk '{print $1}'):56000"
INFO="$DATA_DIR/web.info"
if [ -f "$INFO" ]; then
  TOKEN=$(sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "$INFO")
  echo " Bootstrap token: $TOKEN"
  echo " (open the URL once to create the admin account;"
  echo "  after that the token is only used for scripts)"
else
  echo " (web.info not written yet — check: journalctl -u $SERVICE_NAME)"
fi
echo
echo " Commands:"
echo "   systemctl status  $SERVICE_NAME"
echo "   journalctl -fu    $SERVICE_NAME   # live logs"
echo "   systemctl disable $SERVICE_NAME   # remove from boot"
echo
echo " Enroll this server into a hub (optional): web UI -> Settings"
echo "   -> Managed Hub, or use it AS the hub: web UI -> Devices"
echo "============================================================"
