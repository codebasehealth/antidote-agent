# Antidote Agent

A lightweight agent that runs on your servers and connects to Antidote Cloud. The agent reports server state, executes commands from the cloud, and streams output back in real-time.

## Features

- **WebSocket Connection** - Persistent connection with automatic reconnection
- **Server Discovery** - Reports OS, services, languages, apps, Docker containers
- **Command Execution** - Executes commands from Antidote Cloud
- **Output Streaming** - Real-time stdout/stderr streaming
- **Health Monitoring** - Periodic system metrics (CPU, memory, disk, load)
- **Secure** - Token-based authentication, TLS required

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/codebasehealth/antidote-agent/main/scripts/install.sh | \
  ANTIDOTE_TOKEN=ant_xxx \
  bash
```

Or manually:

```bash
# Download
curl -fsSL https://github.com/codebasehealth/antidote-agent/releases/latest/download/antidote-agent-linux-amd64 -o antidote-agent
chmod +x antidote-agent
sudo mv antidote-agent /usr/local/bin/

# Run
ANTIDOTE_TOKEN=ant_xxx antidote-agent
```

## Usage

```bash
# With environment variable (recommended)
export ANTIDOTE_TOKEN="ant_your_token"
antidote-agent

# Or with flags
antidote-agent --token=ant_xxx --endpoint=wss://antidote.example.com/agent/ws
```

### Command Line Options

```
--token     Agent token (or ANTIDOTE_TOKEN env)
--endpoint  WebSocket endpoint (or ANTIDOTE_ENDPOINT env)
            Default: wss://antidote.codebasehealth.com/agent/ws
--version   Show version and exit
```

### Running as a Service (systemd)

```ini
[Unit]
Description=Antidote Agent
After=network.target

[Service]
Type=simple
User=root
Environment=ANTIDOTE_TOKEN=ant_your_token
ExecStart=/usr/local/bin/antidote-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## Protocol

The agent uses a simple WebSocket protocol:

| Message | Direction | Purpose |
|---------|-----------|---------|
| `auth` | Agent → Cloud | Authenticate with token |
| `auth_ok` | Cloud → Agent | Authentication successful |
| `discover` | Cloud → Agent | Request server discovery |
| `discovery` | Agent → Cloud | Server state (OS, services, apps) |
| `command` | Cloud → Agent | Execute a shell command |
| `output` | Agent → Cloud | Streaming stdout/stderr |
| `complete` | Agent → Cloud | Command finished + exit code |
| `health` | Agent → Cloud | System metrics |
| `heartbeat` | Agent → Cloud | Keep-alive |

## Discovery

When requested, the agent discovers and reports:

- **System**: OS, architecture, distro, kernel, uptime
- **Services**: nginx, mysql, redis, php-fpm, etc. (status + version)
- **Languages**: PHP, Node, Python, Ruby, Go (version + path)
- **Apps**: Laravel, Rails, Django, Next.js, etc. (path + git info)
- **Docker**: Containers (name, image, status)

## Security

- Token-based authentication (`ant_` prefix)
- Commands only accepted from authenticated Antidote Cloud connection
- TLS required in production (wss://)
- No config files with secrets on server

## Development

```bash
# Build
go build -o bin/antidote-agent ./cmd/antidote-agent

# Run
ANTIDOTE_TOKEN=test ./bin/antidote-agent --endpoint=ws://localhost:8081/agent/ws

# Test
go test ./...
```

## Architecture

```
antidote-agent
├── cmd/antidote-agent/    # Entry point
└── internal/
    ├── connection/        # WebSocket client
    ├── discovery/         # Server discovery
    ├── executor/          # Command execution
    ├── health/            # System metrics
    ├── messages/          # Protocol types
    └── router/            # Message handling
```

## License

MIT
