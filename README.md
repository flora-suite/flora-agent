# flora-agent

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Lightweight edge agent for syncing ROS recording files (MCAP, bag, db3) to [flora-server](../flora-server).

## Features

- 📁 **File Monitoring** - Real-time detection via fsnotify + periodic reconciliation
- ✅ **Validation** - MCAP/bag/db3 integrity checking with metadata extraction
- ☁️ **Cloud Sync** - HTTP/2 upload with multipart support for large files
- 💾 **State Persistence** - SQLite-backed for crash recovery and resume
- 🔄 **Resumable Uploads** - Automatic retry and continuation after failures
- 📊 **Observability** - Prometheus metrics (optional)

## Quick Start

### Installation

```bash
# Download binary (Linux amd64)
curl -Lo flora-agent https://github.com/flora-suite/flora-agent/releases/latest/download/flora-agent-linux-amd64
chmod +x flora-agent
sudo mv flora-agent /usr/local/bin/

# Or build from source
git clone https://github.com/flora-suite/flora-agent.git
cd flora-agent
make build
```

### Configuration

```bash
# Copy example config
sudo mkdir -p /etc/flora-agent
sudo cp configs/agent.example.yaml /etc/flora-agent/agent.yaml

# Edit config - set your device token and watch paths
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
| `watch.paths` | `FLORA_WATCH_PATHS` | Directories to monitor |
| `upload.chunk_size` | `FLORA_UPLOAD_CHUNK_SIZE` | Multipart chunk size (default 10MB) |

## Commands

```bash
flora-agent run              # Run the agent daemon
flora-agent sync             # One-time sync and exit
flora-agent config validate  # Validate configuration
flora-agent version          # Show version info
flora-agent --help           # Show help
```

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
make lint            # Run linter
make docker-build    # Build Docker image
```

## Supported Platforms

| Platform | Architecture | Status |
|----------|-------------|--------|
| Linux | amd64, arm64, armv7 | ✅ Primary |
| macOS | amd64, arm64 | ✅ Supported |
| Windows | amd64 | ⚠️ Experimental |

## Supported File Formats

| Format | Extension | Validation | Metadata Extraction |
|--------|-----------|------------|---------------------|
| MCAP | `.mcap` | ✅ Full | ✅ Topics, duration, message count |
| ROS 1 Bag | `.bag` | ✅ Magic check | 🚧 Basic |
| ROS 2 db3 | `.db3` | ✅ SQLite check | 🚧 Basic |

## License

MIT
