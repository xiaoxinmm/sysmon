#!/bin/bash
set -e

# Sysmon v2 Installation Script
# Supports: Ubuntu, Debian, CentOS, RHEL, Fedora, Arch Linux, openSUSE

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/sysmon"
SERVICE_FILE="/etc/systemd/system/sysmon.service"
REPO="xiaoxinmm/sysmon"
BRANCH="v2"

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

install_dependencies() {
    log_info "Installing dependencies..."

    case $OS in
        ubuntu|debian)
            apt-get update -qq
            apt-get install -y curl wget tar libpcap0.8 > /dev/null 2>&1
            ;;
        centos|rhel|fedora)
            if command -v dnf &> /dev/null; then
                dnf install -y curl wget tar libpcap > /dev/null 2>&1
            else
                yum install -y curl wget tar libpcap > /dev/null 2>&1
            fi
            ;;
        arch|manjaro)
            pacman -Sy --noconfirm curl wget tar libpcap > /dev/null 2>&1
            ;;
        opensuse*)
            zypper install -y curl wget tar libpcap > /dev/null 2>&1
            ;;
        *)
            log_warn "Unknown OS, skipping dependency installation"
            ;;
    esac
}

check_go() {
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed. Installing Go..."
        install_go
    else
        GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
        log_info "Go version: $GO_VERSION"
    fi
}

install_go() {
    log_info "Installing Go 1.25..."

    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            GO_ARCH="amd64"
            ;;
        aarch64|arm64)
            GO_ARCH="arm64"
            ;;
        armv7l)
            GO_ARCH="armv6l"
            ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    GO_VERSION="1.25.0"
    GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"

    cd /tmp
    wget -q "https://go.dev/dl/${GO_TAR}" || {
        log_error "Failed to download Go"
        exit 1
    }

    rm -rf /usr/local/go
    tar -C /usr/local -xzf "$GO_TAR"

    export PATH=$PATH:/usr/local/go/bin
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile

    log_info "Go installed successfully"
}

build_sysmon() {
    log_info "Building sysmon from source..."

    BUILD_DIR="/tmp/sysmon-build"
    rm -rf "$BUILD_DIR"
    mkdir -p "$BUILD_DIR"
    cd "$BUILD_DIR"

    log_info "Cloning repository..."
    git clone -b "$BRANCH" "https://github.com/${REPO}.git" . > /dev/null 2>&1 || {
        log_error "Failed to clone repository"
        exit 1
    }

    log_info "Compiling..."
    /usr/local/go/bin/go build -o sysmon . || {
        log_error "Build failed"
        exit 1
    }

    log_info "Installing binary..."
    cp sysmon "$INSTALL_DIR/sysmon"
    chmod +x "$INSTALL_DIR/sysmon"

    cd /
    rm -rf "$BUILD_DIR"
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
}

main() {
    log_info "Starting Sysmon v2 installation..."

    check_root
    detect_os
    install_dependencies
    check_go
    build_sysmon
    create_config
    create_systemd_service
    start_service
    show_info
}

main
