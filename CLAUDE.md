# Antidote Agent

Lightweight Go agent that runs on customer servers to enable Antidote's self-healing capabilities. Maintains a persistent WebSocket connection to the Antidote service and executes pre-defined commands.

## Installation

**Quick install (on target server):**
```bash
curl -fsSL https://raw.githubusercontent.com/codebasehealth/antidote-agent/main/scripts/install.sh | bash
```

**Non-interactive:**
```bash
curl -fsSL https://raw.githubusercontent.com/codebasehealth/antidote-agent/main/scripts/install.sh | \
  ANTIDOTE_TOKEN=ant_xxx \
  ANTIDOTE_ENDPOINT=wss://antidote.codebasehealth.com/agent/ws \
  SERVER_NAME=my-server \
  bash
```

## Build & Test

```bash
# Build
go build -o bin/antidote-agent ./cmd/antidote-agent

# Build all platforms
make build-all

# Test
go test ./...

# Format
go fmt ./...

# Run locally
./bin/antidote-agent --config=./antidote.yml
```

## Architecture

```
cmd/antidote-agent/     - CLI entry point, flags parsing
internal/
  config/               - YAML config loader with ${VAR} substitution
  connection/           - WebSocket client, auto-reconnect, heartbeat
  executor/             - Command execution pool, output streaming
  messages/             - Protocol message types (must match Laravel)
  router/               - Message routing to handlers
  health/               - System metrics (CPU, memory, disk)
```

## WebSocket Protocol

Protocol must match Laravel server implementation at:
- `app/Services/Antidote/WebSocket/AgentGateway.php`
- `app/Services/Antidote/WebSocket/Messages/*.php`
- `app/WebSocket/AgentWebSocketServer.php`

### Message Types

| Type | Direction | Purpose |
|------|-----------|---------|
| `auth` | Agent → Server | Authenticate with token |
| `auth_response` | Server → Agent | Auth result + server ID |
| `heartbeat` | Agent → Server | Keep-alive (configurable interval) |
| `command` | Server → Agent | Execute pre-defined action |
| `command_output` | Agent → Server | Stream stdout/stderr |
| `command_complete` | Agent → Server | Execution finished + exit code |
| `health` | Agent → Server | System metrics update |
| `log_request` | Server → Agent | Request log file contents |
| `log_response` | Agent → Server | Return log entries |

### Token Format

Tokens use `ant_` prefix + 32 random characters. Server stores SHA256 hash in `config->api_token_hash`.

## Config File

Located at `./antidote.yml` or `/etc/antidote/antidote.yml`:

```yaml
server:
  name: "production-web-1"
  environment: "production"

connection:
  endpoint: "wss://your-app.com:8081/agent/ws"
  token: "${ANTIDOTE_TOKEN}"  # Env var substitution
  heartbeat: 30s
  reconnect:
    initial_delay: 1s
    max_delay: 30s

actions:
  rollback:
    command: "cd /app && git checkout ${COMMIT:-HEAD~1}"
    timeout: 180s
    working_dir: "/app"
  restart:
    command: "pm2 restart all"
    timeout: 30s
```

## Dependencies

- `github.com/gorilla/websocket` - WebSocket client
- `gopkg.in/yaml.v3` - YAML config parsing
- `github.com/shirou/gopsutil/v3` - System metrics collection

## Security

- Only executes pre-defined actions from config file
- No arbitrary command execution
- Token-based authentication
- TLS required in production (wss://)

## Testing with Laravel

1. Create server with token in Laravel:
   ```php
   $token = 'ant_' . Str::random(32);
   Server::create([
       'name' => 'test-server',
       'config' => ['api_token_hash' => hash('sha256', $token)],
   ]);
   ```

2. Start gateway: `php artisan antidote:gateway --port=8082`

3. Run agent with test config pointing to `ws://127.0.0.1:8082/agent/ws`
