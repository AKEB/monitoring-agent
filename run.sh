#!/bin/bash
set -euo pipefail

VERSION="${VERSION:-v1.3.0}"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) BINARY="monitoring-agent-linux-amd64" ;;
  aarch64|arm64) BINARY="monitoring-agent-linux-arm64" ;;
  *) echo "Unsupported arch: $ARCH"; exit 1 ;;
esac

if [ -z "${AGENT_KEY_HASH:-}" ]; then
  echo "Set AGENT_KEY_HASH (and optionally SERVER_HOST, VERSION) before running."
  echo "Example:"
  echo "  SERVER_HOST=https://monitoring.example.com/ AGENT_KEY_HASH=... ./run.sh"
  exit 1
fi

SERVER_HOST="${SERVER_HOST:-https://monitoring.akeb.ru/}"
case "$SERVER_HOST" in
  */) ;;
  *) SERVER_HOST="${SERVER_HOST}/" ;;
esac

mkdir -p /opt/monitoring-agent
cd /opt/monitoring-agent

wget -q -O "$BINARY" "https://github.com/AKEB/monitoring-agent/releases/download/${VERSION}/${BINARY}"
wget -q -O monitoring-agent.service "https://raw.githubusercontent.com/AKEB/monitoring-agent/refs/heads/main/deploy/monitoring-agent.service"
chmod +x "$BINARY"
mv -f "$BINARY" monitoring-agent
cp -f monitoring-agent.service /etc/systemd/system/monitoring-agent.service

cat >/opt/monitoring-agent/.env <<EOF
TZ=${TZ:-Europe/Moscow}
SERVER_HOST=${SERVER_HOST}
AGENT_KEY_HASH=${AGENT_KEY_HASH}
REPORT_INTERVAL_SEC=${REPORT_INTERVAL_SEC:-15}
EOF

systemctl daemon-reload
systemctl enable --now monitoring-agent
systemctl --no-pager --full status monitoring-agent || true
