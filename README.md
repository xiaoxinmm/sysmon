# sysmon

A lightweight, real-time system monitor with a terminal-inspired web UI. Written in Go. Single binary, zero dependencies at runtime.

![screenshot](docs/mobile-1.jpg)

## Features

- **Real-time monitoring** — CPU, memory, disk, network, load average
- **Process list** — sortable by CPU/memory/PID
- **History charts** — CPU & memory usage over time (configurable duration)
- **Docker containers** — live stats if Docker is available
- **Password auth** — optional, token-based with HMAC-SHA256
- **Dark theme** — monospace, terminal-style UI
- **Mobile friendly** — responsive layout for phones and tablets
- **Single binary** — all assets embedded, just run it
- **Config file + env vars** — JSON config with environment variable overrides

## Quick Start

```bash
go build -o sysmon .
./sysmon
```

Open `http://localhost:8888` in your browser.

### With config file

```bash
./sysmon -config sysmon.json
```

### With environment variables

```bash
PORT=9090 SYSMON_PASSWORD=secret ./sysmon
```

## Configuration

Create a `sysmon.json` (see `sysmon.example.json`):

```json
{
  "port": 8888,
  "refreshInterval": 1500,
  "maxProcesses": 50,
  "password": "",
  "historyDuration": 3600
}
```

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `port` | `PORT` | `8888` | HTTP listen port |
| `refreshInterval` | `SYSMON_REFRESH` | `1500` | Data push interval (ms) |
| `maxProcesses` | `SYSMON_MAX_PROCS` | `50` | Max processes shown |
| `password` | `SYSMON_PASSWORD` | `""` | Auth password (empty = no auth) |
| `historyDuration` | `SYSMON_HISTORY` | `3600` | History retention (seconds) |

## License

AGPL-3.0. See [LICENSE](LICENSE).
