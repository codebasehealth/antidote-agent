# Antidote Agent

A lightweight Go agent that runs on your servers to enable Antidote's self-healing capabilities. The agent maintains a persistent WebSocket connection to the Antidote service and executes commands on behalf of the orchestrator.

## Features

- **WebSocket Connection** - Persistent connection with automatic reconnection
- **Command Execution** - Run pre-defined actions (rollback, restart, clear cache, etc.)
- **Output Streaming** - Real-time stdout/stderr streaming back to Antidote
- **Health Monitoring** - Periodic health checks and system metrics reporting
- **Secure by Default** - Only executes pre-configured actions, not arbitrary commands

## Installation

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/davekiss/antidote-agent/main/scripts/install.sh | bash
```

This will:
- Download the correct binary for your platform
- Prompt for your Antidote token and endpoint
- Create the config file at `/etc/antidote/antidote.yml`
- Optionally set up a systemd service (Linux)

**Non-interactive install:**
```bash
curl -fsSL https://raw.githubusercontent.com/davekiss/antidote-agent/main/scripts/install.sh | \
  ANTIDOTE_TOKEN=ant_xxx \
  ANTIDOTE_ENDPOINT=wss://antidote.yourdomain.com/agent/ws \
  SERVER_NAME=my-server \
  bash
```

### Manual Download

Download the latest release for your platform from [GitHub Releases](https://github.com/davekiss/antidote-agent/releases):

```bash
# Linux (amd64)
curl -fsSL https://github.com/davekiss/antidote-agent/releases/latest/download/antidote-agent-linux-amd64 -o antidote-agent
chmod +x antidote-agent
sudo mv antidote-agent /usr/local/bin/

# macOS (Apple Silicon)
curl -fsSL https://github.com/davekiss/antidote-agent/releases/latest/download/antidote-agent-darwin-arm64 -o antidote-agent
chmod +x antidote-agent
sudo mv antidote-agent /usr/local/bin/
```

### From Source

```bash
git clone https://github.com/davekiss/antidote-agent.git
cd antidote-agent
make build
```

## Configuration

Create `antidote.yml` in one of these locations:
- `./antidote.yml` (current directory)
- `/etc/antidote/antidote.yml`

```yaml
server:
  name: "production-web-1"
  environment: "production"

connection:
  endpoint: "wss://your-antidote-instance.com:8081/agent/ws"
  token: "${ANTIDOTE_TOKEN}"
  heartbeat: 30s
  reconnect:
    initial_delay: 1s
    max_delay: 30s

actions:
  rollback:
    description: "Rollback to previous deployment"
    command: |
      cd /app
      git checkout ${COMMIT:-HEAD~1}
      npm ci --production
      pm2 restart all
    timeout: 180s
    working_dir: "/app"

  restart:
    description: "Restart application"
    command: "pm2 restart all"
    timeout: 30s

  health_check:
    description: "Check application health"
    command: "curl -sf http://localhost:3000/health"
    timeout: 10s

  clear_cache:
    description: "Clear application caches"
    command: "redis-cli FLUSHDB && pm2 restart all"
    timeout: 30s

logs:
  paths:
    - path: "/app/logs/*.log"
      type: "application"
    - path: "/var/log/nginx/error.log"
      type: "nginx"
```

## Usage

### Basic

```bash
# With config file
antidote-agent --config=/etc/antidote/antidote.yml

# With environment variables
export ANTIDOTE_TOKEN="ant_your_token_here"
export ANTIDOTE_ENDPOINT="wss://your-instance.com:8081/agent/ws"
antidote-agent
```

### Command Line Options

```
--config    Path to config file (default: auto-detect)
--token     Agent token (overrides config)
--endpoint  WebSocket endpoint (overrides config)
--version   Show version and exit
```

### Running as a Service (systemd)

Create `/etc/systemd/system/antidote-agent.service`:

```ini
[Unit]
Description=Antidote Agent
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/antidote-agent --config=/etc/antidote/antidote.yml
Restart=always
RestartSec=5
Environment=ANTIDOTE_TOKEN=ant_your_token_here

[Install]
WantedBy=multi-user.target
```

Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable antidote-agent
sudo systemctl start antidote-agent
```

## Actions

Actions are pre-defined commands that Antidote can trigger. Each action specifies:

| Field | Description |
|-------|-------------|
| `command` | Shell command to execute |
| `timeout` | Maximum execution time |
| `working_dir` | Directory to run command in |
| `env` | Environment variables |
| `requires_approval` | If true, requires human approval before execution |

Parameters can be passed from Antidote using `${PARAM_NAME}` syntax with optional defaults: `${PARAM:-default}`.

## Security

- **Token Authentication** - Each agent authenticates with a unique token
- **Pre-defined Actions Only** - Agent only executes actions defined in config
- **No Arbitrary Commands** - Shell access is disabled by default
- **TLS Required** - WebSocket connections use WSS (TLS)

## Development

```bash
# Build
make build

# Build for all platforms
make build-all

# Run tests
make test

# Run locally
make run
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      antidote-agent                          │
│                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │   Config    │  │ Connection  │  │  Executor   │         │
│  │   Manager   │  │   Manager   │  │    Pool     │         │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘         │
│         │                │                │                 │
│         └────────────────┼────────────────┘                 │
│                          │                                  │
│  ┌─────────────┐  ┌──────┴──────┐  ┌─────────────┐         │
│  │   Health    │  │   Message   │  │     Log     │         │
│  │   Monitor   │  │   Router    │  │   Streamer  │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## License

MIT
