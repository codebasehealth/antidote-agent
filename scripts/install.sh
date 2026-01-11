#!/bin/bash
set -e

# Antidote Agent Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/codebasehealth/antidote-agent/main/scripts/install.sh | bash
# Or with token: curl -fsSL ... | ANTIDOTE_TOKEN=ant_xxx ANTIDOTE_ENDPOINT=wss://... bash

REPO="codebasehealth/antidote-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/antidote"
BINARY_NAME="antidote-agent"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() { echo -e "${BLUE}==>${NC} $1"; }
success() { echo -e "${GREEN}==>${NC} $1"; }
warn() { echo -e "${YELLOW}==>${NC} $1"; }
error() { echo -e "${RED}==>${NC} $1"; exit 1; }

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux) OS="linux" ;;
        darwin) OS="darwin" ;;
        *) error "Unsupported operating system: $OS" ;;
    esac

    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac

    PLATFORM="${OS}-${ARCH}"
    info "Detected platform: $PLATFORM"
}

# Get latest release version
get_latest_version() {
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        error "Failed to get latest version"
    fi
    info "Latest version: $VERSION"
}

# Download and install binary
install_binary() {
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_NAME}-${PLATFORM}"
    TEMP_FILE=$(mktemp)

    info "Downloading ${BINARY_NAME}..."
    if ! curl -fsSL "$DOWNLOAD_URL" -o "$TEMP_FILE"; then
        rm -f "$TEMP_FILE"
        error "Failed to download binary from $DOWNLOAD_URL"
    fi

    info "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
    chmod +x "$TEMP_FILE"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$TEMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        sudo mv "$TEMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    success "Binary installed successfully!"
}

# Create config file
create_config() {
    if [ -f "${CONFIG_DIR}/antidote.yml" ]; then
        warn "Config file already exists at ${CONFIG_DIR}/antidote.yml"
        return
    fi

    # Prompt for values if not provided via env
    if [ -z "$ANTIDOTE_TOKEN" ]; then
        echo ""
        read -p "Enter your Antidote token (ant_...): " ANTIDOTE_TOKEN
    fi

    if [ -z "$ANTIDOTE_ENDPOINT" ]; then
        ANTIDOTE_ENDPOINT="wss://antidote.codebasehealth.com/agent/ws"
        read -p "Enter Antidote endpoint [$ANTIDOTE_ENDPOINT]: " INPUT_ENDPOINT
        if [ -n "$INPUT_ENDPOINT" ]; then
            ANTIDOTE_ENDPOINT="$INPUT_ENDPOINT"
        fi
    fi

    if [ -z "$SERVER_NAME" ]; then
        SERVER_NAME=$(hostname)
        read -p "Enter server name [$SERVER_NAME]: " INPUT_NAME
        if [ -n "$INPUT_NAME" ]; then
            SERVER_NAME="$INPUT_NAME"
        fi
    fi

    if [ -z "$SERVER_ENV" ]; then
        SERVER_ENV="production"
        read -p "Enter environment [$SERVER_ENV]: " INPUT_ENV
        if [ -n "$INPUT_ENV" ]; then
            SERVER_ENV="$INPUT_ENV"
        fi
    fi

    info "Creating config directory..."
    if [ -w "$(dirname $CONFIG_DIR)" ]; then
        mkdir -p "$CONFIG_DIR"
    else
        sudo mkdir -p "$CONFIG_DIR"
    fi

    info "Creating config file..."
    CONFIG_CONTENT="server:
  name: \"${SERVER_NAME}\"
  environment: \"${SERVER_ENV}\"

connection:
  endpoint: \"${ANTIDOTE_ENDPOINT}\"
  token: \"${ANTIDOTE_TOKEN}\"
  heartbeat: 30s
  reconnect:
    initial_delay: 1s
    max_delay: 30s

actions:
  artisan_down:
    description: \"Put application in maintenance mode\"
    command: \"php artisan down\"
    timeout: 30s

  artisan_up:
    description: \"Bring application out of maintenance mode\"
    command: \"php artisan up\"
    timeout: 30s

  restart_queue:
    description: \"Restart queue workers\"
    command: \"php artisan queue:restart\"
    timeout: 30s

  clear_cache:
    description: \"Clear all caches\"
    command: \"php artisan cache:clear && php artisan config:clear && php artisan view:clear\"
    timeout: 60s

  restart_php:
    description: \"Restart PHP-FPM\"
    command: \"sudo systemctl restart php8.2-fpm\"
    timeout: 30s

  restart_nginx:
    description: \"Restart Nginx\"
    command: \"sudo systemctl restart nginx\"
    timeout: 30s
"

    if [ -w "$CONFIG_DIR" ] 2>/dev/null; then
        echo "$CONFIG_CONTENT" > "${CONFIG_DIR}/antidote.yml"
    else
        echo "$CONFIG_CONTENT" | sudo tee "${CONFIG_DIR}/antidote.yml" > /dev/null
    fi

    success "Config file created at ${CONFIG_DIR}/antidote.yml"
}

# Set up systemd service
setup_systemd() {
    if [ "$OS" != "linux" ]; then
        warn "Systemd service setup is only available on Linux"
        return
    fi

    # Auto-setup in non-interactive mode (when ANTIDOTE_TOKEN was provided via env)
    if [ -n "$NONINTERACTIVE" ]; then
        SETUP_SERVICE="Y"
    else
        echo ""
        read -p "Set up systemd service to run agent on boot? [Y/n]: " SETUP_SERVICE
        SETUP_SERVICE=${SETUP_SERVICE:-Y}
    fi

    if [[ ! "$SETUP_SERVICE" =~ ^[Yy]$ ]]; then
        info "Skipping systemd setup. Run manually with: antidote-agent --config=/etc/antidote/antidote.yml"
        return
    fi

    SERVICE_FILE="/etc/systemd/system/antidote-agent.service"
    SERVICE_CONTENT="[Unit]
Description=Antidote Agent
After=network.target

[Service]
Type=simple
User=root
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --config=${CONFIG_DIR}/antidote.yml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
"

    info "Creating systemd service..."
    echo "$SERVICE_CONTENT" | sudo tee "$SERVICE_FILE" > /dev/null

    info "Enabling and starting service..."
    sudo systemctl daemon-reload
    sudo systemctl enable antidote-agent
    sudo systemctl start antidote-agent

    success "Service installed and started!"
    echo ""
    info "Useful commands:"
    echo "  sudo systemctl status antidote-agent   # Check status"
    echo "  sudo systemctl restart antidote-agent  # Restart"
    echo "  sudo journalctl -u antidote-agent -f   # View logs"
}

# Main
main() {
    # Detect non-interactive mode (env vars provided)
    if [ -n "$ANTIDOTE_TOKEN" ]; then
        NONINTERACTIVE=1
    fi

    echo ""
    echo "  ___        _   _     _       _"
    echo " / _ \      | | (_)   | |     | |"
    echo "/ /_\ \_ __ | |_ _  __| | ___ | |_ ___"
    echo "|  _  | '_ \| __| |/ _\` |/ _ \| __/ _ \\"
    echo "| | | | | | | |_| | (_| | (_) | ||  __/"
    echo "\_| |_/_| |_|\__|_|\__,_|\___/ \__\___|"
    echo ""
    echo "          Agent Installer"
    echo ""

    detect_platform
    get_latest_version
    install_binary
    create_config
    setup_systemd

    echo ""
    success "Antidote Agent installation complete!"
    echo ""
    if [ -n "$NONINTERACTIVE" ] && [ "$OS" = "linux" ]; then
        echo "The agent is now running and will start automatically on boot."
        echo ""
        info "Useful commands:"
        echo "  sudo systemctl status antidote-agent   # Check status"
        echo "  sudo journalctl -u antidote-agent -f   # View logs"
    else
        echo "Next steps:"
        echo "  1. Edit ${CONFIG_DIR}/antidote.yml to customize actions"
        echo "  2. Ensure your server is registered in Antidote dashboard"
        echo "  3. Start the agent or reboot to connect"
    fi
    echo ""
}

main "$@"
