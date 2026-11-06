# Monitoring Agent

![Logo](https://monitoring.akeb.ru/images/apple-icon-180x180.png)

- Server
![Docker Pulls](https://img.shields.io/docker/pulls/akeb/monitoring) ![Docker Image Size](https://img.shields.io/docker/image-size/akeb/monitoring/latest) ![Docker Version](https://img.shields.io/docker/v/akeb/monitoring) ![Docker Stars](https://img.shields.io/docker/stars/akeb/monitoring) ![License](https://img.shields.io/badge/license-AGPLv3-blue)

- Worker
![Docker Pulls](https://img.shields.io/docker/pulls/akeb/monitoring-worker-go) ![Docker Image Size](https://img.shields.io/docker/image-size/akeb/monitoring-worker-go/latest) ![Docker Version](https://img.shields.io/docker/v/akeb/monitoring-worker-go) ![Docker Stars](https://img.shields.io/docker/stars/akeb/monitoring-worker-go) ![License](https://img.shields.io/badge/license-AGPLv3-blue)

- Agent
![Docker Pulls](https://img.shields.io/docker/pulls/akeb/monitoring-agent) ![Docker Image Size](https://img.shields.io/docker/image-size/akeb/monitoring-agent/latest) ![Docker Version](https://img.shields.io/docker/v/akeb/monitoring-agent) ![Docker Stars](https://img.shields.io/docker/stars/akeb/monitoring-agent) ![License](https://img.shields.io/badge/license-AGPLv3-blue)

Monitoring is a powerful distributed monitoring system with a web interface, built on a Server / Worker / Agent architecture. Monitor websites, services, Docker containers and host metrics from geographically distributed points.

- [Website](https://akeb.github.io/monitoring/)

Docker images:
- [akeb/monitoring:latest](https://hub.docker.com/r/akeb/monitoring)
- [akeb/monitoring-worker-go:latest](https://hub.docker.com/r/akeb/monitoring-worker-go)
- [akeb/monitoring-agent:latest](https://hub.docker.com/r/akeb/monitoring-agent)

Go host agent for [AKEB monitoring](https://github.com/AKEB/monitoring-server): single static binary that pushes host metrics to the monitoring server every 30 seconds.

![License](https://img.shields.io/badge/license-AGPLv3-blue)

- [Monitoring website](https://akeb.github.io/monitoring/)
- [Documentation](https://github.com/AKEB/monitoring/wiki)

## What it reports

Every `REPORT_INTERVAL_SEC` (default 30):

- Host: hostname, OS/platform/kernel/arch, uptime, virtualization
- CPU: cores, load1/5/15, usage %
- Memory / swap
- Disks (mounts, usage %)
- Network interfaces: IPs, rx/tx bytes and bps
- Docker containers (if `/var/run/docker.sock` is reachable)
- Proxmox VE guests: QEMU VMs and LXC (name, status, CPU/RAM/disk, uptime) when `/etc/pve` is present
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
REPORT_INTERVAL_SEC=30
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
| `REPORT_INTERVAL_SEC` | `30` | Push interval (15–600) |
| `AGENT_VERSION` | ldflags / `local` | Reported agent version |
| `DEBUG` | `false` | Verbose stderr |
| `COLLECT_DOCKER` | `true` | Docker inventory |
| `COLLECT_PROXMOX` | `true` | Proxmox VE VM/LXC inventory |
| `COLLECT_AUTH` | `true` | Auth log delta |
| `COLLECT_PROCESSES` | `true` | Process snapshot |
| `PROCESS_TOP_N` | `20` | Top processes by CPU |
| `TZ` | `UTC` | Timezone |
| `AUTO_UPDATE` | `true` | Honor `update` from report API response |
| `UPDATE_FROM_GITHUB` | `true` | Allow only `github.com/AKEB/monitoring-agent` release URLs |

## Remote updates

The monitoring server can set a **desired agent version** (Admin → Servers). On each successful report the API may return:

```json
{
  "status": 0,
  "server_id": 1,
  "update": {
    "version": "v1.0.2",
    "url": "https://github.com/AKEB/monitoring-agent/releases/download/v1.0.2/monitoring-agent-linux-amd64",
    "sha256": "...",
    "arch": "amd64"
  }
}
```

When `AUTO_UPDATE=true`, the agent downloads that asset, verifies SHA-256, replaces its binary, and exits (`systemd` `Restart=always` starts the new build).

Requirements:

- Host can reach `https://github.com` (Releases)
- Agent runs as a user that can replace `/opt/monitoring-agent/monitoring-agent` (typically root via the packaged unit)
- Rollback: set desired version back to an older tag (or clear it)

Disable updates on a host with `AUTO_UPDATE=false`.

## CI and releases

`.github/workflows/build.yml`:

- push/PR → build artifacts (`linux/amd64`, `linux/arm64`) + `.sha256` / `checksums.txt`
- tag `v*` → GitHub Release with binaries and checksums
- non-PR → Docker Hub `akeb/monitoring-agent` (amd64+arm64)

```bash
git tag v1.0.0
git push origin v1.0.0
```
