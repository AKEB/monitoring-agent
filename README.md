# Monitoring Agent

Go host agent for [AKEB monitoring](https://github.com/AKEB/monitoring-server): single static binary that pushes host metrics to the monitoring server every minute.

![License](https://img.shields.io/badge/license-AGPLv3-blue)

- [Monitoring website](https://akeb.github.io/monitoring/)
- [Documentation](https://github.com/AKEB/monitoring/wiki)

## What it reports

Every `REPORT_INTERVAL_SEC` (default 60):

- Host: hostname, OS/platform/kernel/arch, uptime, virtualization
- CPU: cores, load1/5/15, usage %
- Memory / swap
- Disks (mounts, usage %)
- Network interfaces: IPs, rx/tx bytes and bps
- Docker containers (if `/var/run/docker.sock` is reachable)
- Auth events (SSH/sudo from `/var/log/auth.log` or `/var/log/secure`, delta)
- Top processes (+ interesting daemons)

## Build

```bash
go build -buildvcs=false -o monitoring-agent ./cmd/agent/
```

Cross-compile:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -o monitoring-agent-linux-amd64 ./cmd/agent/
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -buildvcs=false -o monitoring-agent-linux-arm64 ./cmd/agent/
```

## Install (systemd)

1. In Monitoring UI: **Admin → Servers → Create**, copy `AGENT_KEY_HASH`.
2. On the host:

```bash
sudo mkdir -p /opt/monitoring-agent
sudo cp monitoring-agent-linux-amd64 /opt/monitoring-agent/monitoring-agent   # or arm64 on RPi
sudo cp deploy/monitoring-agent.service /etc/systemd/system/
sudo tee /opt/monitoring-agent/.env >/dev/null <<EOF
TZ=Europe/Moscow
SERVER_HOST=https://monitoring.example.com/
AGENT_KEY_HASH=paste_key_here
REPORT_INTERVAL_SEC=60
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now monitoring-agent
```

Works on Proxmox VE, Debian, Raspberry Pi OS, LXC (privileged or with needed caps), and Proxmox Backup Server.

### Docker

```bash
docker run -d --name monitoring-agent --restart unless-stopped \
  --net host \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e SERVER_HOST=https://monitoring.example.com/ \
  -e AGENT_KEY_HASH=... \
  akeb/monitoring-agent:latest
```

## Environment

| Variable | Default | Meaning |
|----------|---------|---------|
| `SERVER_HOST` | — | Monitoring base URL (required) |
| `AGENT_KEY_HASH` | — | Server key from UI (required) |
| `REPORT_INTERVAL_SEC` | `60` | Push interval (15–600) |
| `AGENT_VERSION` | ldflags / `local` | Reported agent version |
| `DEBUG` | `false` | Verbose stderr |
| `COLLECT_DOCKER` | `true` | Docker inventory |
| `COLLECT_AUTH` | `true` | Auth log delta |
| `COLLECT_PROCESSES` | `true` | Process snapshot |
| `PROCESS_TOP_N` | `20` | Top processes by CPU |
| `TZ` | `UTC` | Timezone |

## CI and releases

`.github/workflows/build.yml`:

- push/PR → build artifacts (`linux/amd64`, `linux/arm64`)
- tag `v*` → GitHub Release with both binaries
- non-PR → Docker Hub `akeb/monitoring-agent` (amd64+arm64)

```bash
git tag v1.0.0
git push origin v1.0.0
```
