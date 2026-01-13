#!/bin/bash
set -e

# Antidote Agent Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/codebasehealth/antidote-agent/main/scripts/install.sh | ANTIDOTE_TOKEN=ant_xxx bash

REPO="codebasehealth/antidote-agent"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="antidote-agent"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

# Collect token
collect_token() {
    if [ -z "$ANTIDOTE_TOKEN" ]; then
        echo ""
        read -p "Enter your Antidote token (ant_...): " ANTIDOTE_TOKEN
    fi

    if [ -z "$ANTIDOTE_TOKEN" ]; then
        error "Antidote token is required"
    fi
}

# Fetch signing key from Antidote Cloud
fetch_signing_key() {
    ANTIDOTE_API_URL="${ANTIDOTE_ENDPOINT:-https://antidote.codebasehealth.com}"
    # Remove /agent/ws suffix if present (convert WebSocket URL to API URL)
    ANTIDOTE_API_URL="${ANTIDOTE_API_URL%/agent/ws}"

    info "Fetching signing key from ${ANTIDOTE_API_URL}..."

    SIGNING_KEY_RESPONSE=$(curl -fsSL "${ANTIDOTE_API_URL}/api/antidote/signing-key" 2>/dev/null || echo "")

    if [ -n "$SIGNING_KEY_RESPONSE" ]; then
        ANTIDOTE_SIGNING_KEY=$(echo "$SIGNING_KEY_RESPONSE" | grep -o '"public_key":"[^"]*"' | sed 's/"public_key":"//;s/"$//')
        if [ -n "$ANTIDOTE_SIGNING_KEY" ]; then
            success "Signing key retrieved successfully"
        else
            warn "Could not parse signing key - command signing will be disabled"
        fi
    else
        warn "Could not fetch signing key - command signing will be disabled"
    fi
}

# Set up systemd service
setup_systemd() {
    if [ "$OS" != "linux" ]; then
        warn "Systemd service setup is only available on Linux"
        echo ""
        echo "Run manually with:"
        if [ -n "$ANTIDOTE_SIGNING_KEY" ]; then
            echo "  ANTIDOTE_TOKEN=$ANTIDOTE_TOKEN ANTIDOTE_SIGNING_KEY=$ANTIDOTE_SIGNING_KEY antidote-agent"
        else
            echo "  ANTIDOTE_TOKEN=$ANTIDOTE_TOKEN antidote-agent"
        fi
        return
    fi

    if [ -z "$NONINTERACTIVE" ]; then
        echo ""
        read -p "Set up systemd service to run agent on boot? [Y/n]: " SETUP_SERVICE
        SETUP_SERVICE=${SETUP_SERVICE:-Y}
    else
        SETUP_SERVICE="Y"
    fi

    if [[ ! "$SETUP_SERVICE" =~ ^[Yy]$ ]]; then
        info "Skipping systemd setup."
        echo ""
        echo "Run manually with:"
        if [ -n "$ANTIDOTE_SIGNING_KEY" ]; then
            echo "  ANTIDOTE_TOKEN=$ANTIDOTE_TOKEN ANTIDOTE_SIGNING_KEY=$ANTIDOTE_SIGNING_KEY antidote-agent"
        else
            echo "  ANTIDOTE_TOKEN=$ANTIDOTE_TOKEN antidote-agent"
        fi
        return
    fi

    SERVICE_FILE="/etc/systemd/system/antidote-agent.service"

    # Build environment variables
    ENV_VARS="Environment=ANTIDOTE_TOKEN=${ANTIDOTE_TOKEN}"
    if [ -n "$ANTIDOTE_SIGNING_KEY" ]; then
        ENV_VARS="${ENV_VARS}
Environment=ANTIDOTE_SIGNING_KEY=${ANTIDOTE_SIGNING_KEY}"
    fi
    if [ -n "$ANTIDOTE_ENDPOINT" ]; then
        ENV_VARS="${ENV_VARS}
Environment=ANTIDOTE_ENDPOINT=${ANTIDOTE_ENDPOINT}"
    fi

    SERVICE_CONTENT="[Unit]
Description=Antidote Agent
After=network.target

[Service]
Type=simple
User=root
${ENV_VARS}
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
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
}

# Main
main() {
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
    collect_token
    fetch_signing_key
    setup_systemd

    echo ""
    success "Antidote Agent installation complete!"
    echo ""
    if [ "$OS" = "linux" ]; then
        echo "The agent is now running and will start automatically on boot."
        echo ""
        info "Useful commands:"
        echo "  sudo systemctl status antidote-agent   # Check status"
        echo "  sudo journalctl -u antidote-agent -f   # View logs"
    fi
    echo ""
}

main "$@"
