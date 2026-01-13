# Antidote Agent

Lightweight Go agent that connects to Antidote Cloud. Reports server state, executes commands, streams output.

## Quick Start

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/codebasehealth/antidote-agent/main/scripts/install.sh | ANTIDOTE_TOKEN=ant_xxx bash

# Or run directly
ANTIDOTE_TOKEN=ant_xxx antidote-agent
```

## Build & Test

```bash
go build -o bin/antidote-agent ./cmd/antidote-agent
go test ./...
```

## Architecture

```
cmd/antidote-agent/     # Entry point (token + endpoint flags)
internal/
  connection/           # WebSocket client, auto-reconnect
  discovery/            # Server discovery (OS, services, apps)
  executor/             # Command execution, output streaming
  health/               # System metrics (CPU, mem, disk)
  messages/             # Protocol types
  router/               # Message routing
```

## Protocol

| Type | Direction | Purpose |
|------|-----------|---------|
| `auth` | Agent → Cloud | Authenticate |
| `auth_ok` | Cloud → Agent | Auth success |
| `discover` | Cloud → Agent | Request discovery |
| `discovery` | Agent → Cloud | Server state |
| `command` | Cloud → Agent | Execute command |
| `output` | Agent → Cloud | Streaming output |
| `complete` | Agent → Cloud | Exit code |
| `health` | Agent → Cloud | System metrics |

## Command Message

```json
{
  "type": "command",
  "id": "cmd_xxx",
  "command": "php artisan cache:clear",
  "working_dir": "/home/forge/app",
  "timeout": 60
}
```

## Discovery Response

Reports: OS, distro, kernel, services (nginx, mysql, redis, php-fpm), languages (PHP, Node, Python), apps (Laravel, Rails, etc.), Docker containers.

## No Config File

Agent takes only:
- `--token` or `ANTIDOTE_TOKEN` (required)
- `--endpoint` or `ANTIDOTE_ENDPOINT` (default: wss://antidote.codebasehealth.com/agent/ws)

All command logic lives in Antidote Cloud, not on the server.
