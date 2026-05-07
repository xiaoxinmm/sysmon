#!/bin/bash
set -e

# Sysmon v2 Installation Script
# Supports: Ubuntu, Debian, CentOS, RHEL, Fedora, Arch Linux, openSUSE

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/sysmon"
SERVICE_FILE="/etc/systemd/system/sysmon.service"
REPO="xiaoxinmm/sysmon"
VERSION="latest"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "Please run as root or with sudo"
        exit 1
    fi
}

detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
        VER=$VERSION_ID
    elif [ -f /etc/redhat-release ]; then
        OS="rhel"
    else
        OS=$(uname -s)
    fi

    log_info "Detected OS: $OS"
}

detect_arch() {
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l)
            ARCH="armv7"
            ;;
        i386|i686)
            ARCH="386"
            ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
    log_info "Detected architecture: $ARCH"
}

install_dependencies() {
    log_info "Installing dependencies..."

    case $OS in
        ubuntu|debian)
            apt-get update -qq
            apt-get install -y curl wget tar libpcap0.8 ca-certificates > /dev/null 2>&1
            ;;
        centos|rhel|fedora)
            if command -v dnf &> /dev/null; then
                dnf install -y curl wget tar libpcap ca-certificates > /dev/null 2>&1
            else
                yum install -y curl wget tar libpcap ca-certificates > /dev/null 2>&1
            fi
            ;;
        arch|manjaro)
            pacman -Sy --noconfirm curl wget tar libpcap ca-certificates > /dev/null 2>&1
            ;;
        opensuse*)
            zypper install -y curl wget tar libpcap ca-certificates > /dev/null 2>&1
            ;;
        *)
            log_warn "Unknown OS, attempting to continue..."
            ;;
    esac
}

get_latest_release() {
    log_info "Fetching latest release information..."

    # Try to get latest release tag from GitHub API
    RELEASE_URL="https://api.github.com/repos/${REPO}/releases/latest"
    LATEST_TAG=$(curl -s "$RELEASE_URL" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")

    if [ -z "$LATEST_TAG" ]; then
        log_warn "Could not fetch latest release, using v2 branch"
        LATEST_TAG="v2"
    fi

    log_info "Using version: $LATEST_TAG"
}

download_binary() {
    log_info "Downloading sysmon binary..."

    BINARY_NAME="sysmon-linux-${ARCH}"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${BINARY_NAME}"

    # Fallback to raw branch if release not found
    FALLBACK_URL="https://github.com/${REPO}/raw/v2/bin/${BINARY_NAME}"

    TMP_FILE="/tmp/sysmon-download"

    # Try release first
    if curl -fsSL "$DOWNLOAD_URL" -o "$TMP_FILE" 2>/dev/null; then
        log_info "Downloaded from release"
    elif curl -fsSL "$FALLBACK_URL" -o "$TMP_FILE" 2>/dev/null; then
        log_info "Downloaded from repository"
    else
        log_error "Failed to download sysmon binary"
        log_error "Tried: $DOWNLOAD_URL"
        log_error "And: $FALLBACK_URL"
        exit 1
    fi

    # Install binary
    mv "$TMP_FILE" "$INSTALL_DIR/sysmon"
    chmod +x "$INSTALL_DIR/sysmon"

    log_info "Binary installed to $INSTALL_DIR/sysmon"
}

create_config() {
    log_info "Creating configuration..."

    mkdir -p "$CONFIG_DIR"

    if [ ! -f "$CONFIG_DIR/sysmon.json" ]; then
        cat > "$CONFIG_DIR/sysmon.json" <<EOF
{
  "port": 8888,
  "password": "sysmon$(date +%Y)",
  "refreshInterval": 1500,
  "maxProcesses": 50,
  "historyDuration": 3600,
  "history_db": "/var/lib/sysmon/sysmon.db",
  "history_retention_days": 7,
  "enable_traffic": false,
  "traffic_iface": "",
  "traffic_interval": 60,
  "enableShell": false,
  "shell_password": "",
  "logLevel": "info",
  "logFormat": "text"
}
EOF
        log_info "Config created at $CONFIG_DIR/sysmon.json"
        log_warn "Default password: sysmon$(date +%Y)"
    else
        log_info "Config already exists, skipping"
    fi

    mkdir -p /var/lib/sysmon
    mkdir -p /var/log/sysmon
}

create_systemd_service() {
    log_info "Creating systemd service..."

    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Sysmon v2 - System and Network Monitor
After=network.target

[Service]
Type=simple
User=root
ExecStart=$INSTALL_DIR/sysmon -config $CONFIG_DIR/sysmon.json
Restart=on-failure
RestartSec=5s
StandardOutput=append:/var/log/sysmon/sysmon.log
StandardError=append:/var/log/sysmon/sysmon.log

# Security
NoNewPrivileges=false
PrivateTmp=true

# Capabilities for packet capture
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    log_info "Systemd service created"
}

start_service() {
    log_info "Starting sysmon service..."

    systemctl enable sysmon > /dev/null 2>&1
    systemctl restart sysmon

    sleep 2

    if systemctl is-active --quiet sysmon; then
        log_info "Sysmon is running!"
    else
        log_error "Failed to start sysmon"
        log_info "Check logs: journalctl -u sysmon -f"
        exit 1
    fi
}

show_info() {
    PORT=$(grep -oP '"port":\s*\K\d+' "$CONFIG_DIR/sysmon.json" || echo "8888")
    PASSWORD=$(grep -oP '"password":\s*"\K[^"]+' "$CONFIG_DIR/sysmon.json" || echo "not set")

    echo ""
    echo "=========================================="
    log_info "Sysmon v2 installed successfully!"
    echo "=========================================="
    echo ""
    echo "Web Interface: http://$(hostname -I | awk '{print $1}'):$PORT"
    echo "Password: $PASSWORD"
    echo ""
    echo "Useful commands:"
    echo "  systemctl status sysmon    # Check status"
    echo "  systemctl restart sysmon   # Restart service"
    echo "  systemctl stop sysmon      # Stop service"
    echo "  journalctl -u sysmon -f    # View logs"
    echo ""
    echo "Config file: $CONFIG_DIR/sysmon.json"
    echo "After editing config, run: systemctl restart sysmon"
    echo ""
    echo "To enable traffic monitoring, edit config and set:"
    echo "  \"enable_traffic\": true"
    echo ""
}

main() {
    log_info "Starting Sysmon v2 installation..."

    check_root
    detect_os
    detect_arch
    install_dependencies
    get_latest_release
    download_binary
    create_config
    create_systemd_service
    start_service
    show_info
}

main
