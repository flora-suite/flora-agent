# flora-agent

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Lightweight edge agent for syncing ROS recording files (MCAP, bag, db3) to [flora-server](../flora-server).

> **WARNING:** This codebase is unstable and may undergo major changes. Do not use it in production.

## Features

- 📁 **File Monitoring** - Real-time detection via fsnotify + periodic reconciliation
- ✅ **Validation** - MCAP/bag/db3 integrity checking with metadata extraction
- ☁️ **Cloud Sync** - HTTP/2 upload with multipart support for large files
- 💾 **State Persistence** - SQLite-backed for crash recovery and resume
- 🔄 **Resumable Uploads** - Multipart continuation after interruption and fixed-cycle retry after failures
- 📊 **Observability** - Prometheus metrics (optional)

## Quick Start

### Installation

```bash
# Linux and macOS (amd64 or arm64)
curl -fsSL https://raw.githubusercontent.com/flora-suite/flora-agent/main/scripts/install.sh | sh

# Choose an installation directory (default: ~/.local/bin)
curl -fsSL https://raw.githubusercontent.com/flora-suite/flora-agent/main/scripts/install.sh | INSTALL_DIR=/usr/local/bin sh

# Homebrew (macOS and Linux)
brew tap flora-suite/flora
brew install flora-agent

# Or build from source
git clone https://github.com/flora-suite/flora-agent.git
cd flora-agent
make build
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/flora-suite/flora-agent/main/scripts/install.ps1 | iex
```

Windows users can also install it through Scoop after adding the Flora bucket:

```powershell
scoop bucket add flora-suite https://github.com/flora-suite/scoop-flora
scoop install flora-agent
```

### Configuration

```bash
# Copy example config
sudo mkdir -p /etc/flora-agent
sudo cp configs/agent.example.yaml /etc/flora-agent/agent.yaml

# Edit config - set a device token (or a user token for first-run registration) and watch paths
sudo vim /etc/flora-agent/agent.yaml
```

### Running

```bash
# Run directly
flora-agent run --config /etc/flora-agent/agent.yaml

# Or with environment variables
FLORA_SERVER_DEVICE_TOKEN=your-token \
FLORA_WATCH_PATHS=/data/recordings \
flora-agent run

# Install as systemd service (Linux)
make install-service
sudo systemctl enable --now flora-agent
```

## Configuration

See [configs/agent.example.yaml](configs/agent.example.yaml) for all options.

Key settings:

| Setting | Env Variable | Description |
|---------|--------------|-------------|
| `server.url` | `FLORA_SERVER_URL` | flora-server URL |
| `server.device_token` | `FLORA_SERVER_DEVICE_TOKEN` | Device authentication token |
| `server.user_token` | `FLORA_SERVER_USER_TOKEN` | One-time registration token when no device token is stored |
| `watch.paths` | `FLORA_WATCH_PATHS` | Directories to monitor |
| `upload.chunk_size` | `FLORA_UPLOAD_CHUNK_SIZE` | Multipart chunk size (default 10MB) |

## Commands

```bash
flora-agent run              # Run the agent daemon
flora-agent sync             # One-time sync and exit
flora-agent register --server https://api.flora.fan  # Register with an explicit API URL
flora-agent config validate  # Validate configuration
flora-agent version          # Show version info
flora-agent --help           # Show help
```

`flora-agent register` warns when `--server` is omitted and falls back to
`https://api.flora.fan`. Always pass `--server` when registering against a
self-hosted Flora server.

## Docker

```bash
docker run -d \
  --name flora-agent \
  -v /data/recordings:/data/recordings:ro \
  -v flora-agent-data:/var/lib/flora-agent \
  -e FLORA_SERVER_DEVICE_TOKEN=your-token \
  -e FLORA_SERVER_URL=https://api.flora.fan \
  ghcr.io/flora-suite/flora-agent:latest
```

## Building

```bash
make build           # Build for current platform
make build-all       # Build for all platforms
make test            # Run tests
make test-integration # Run tagged integration tests
make lint            # Run linter
make docker-build    # Build Docker image
```

## Supported Platforms

| Platform | Architecture | Status |
|----------|-------------|--------|
| Linux | amd64, arm64, armv7 | ✅ Primary |
| macOS | amd64, arm64 | ✅ Supported |
| Windows | amd64, arm64 | ⚠️ Experimental |

## Supported File Formats

| Format | Extension | Validation | Metadata Extraction |
|--------|-----------|------------|---------------------|
| MCAP | `.mcap` | ✅ Full | ✅ Topics, duration, message count |
| ROS 1 Bag | `.bag` | ✅ Magic check | 🚧 Basic |
| ROS 2 db3 | `.db3` | ✅ SQLite check | 🚧 Basic |

## License

MIT
