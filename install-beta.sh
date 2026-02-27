#!/bin/bash
# sysmon BETA installer
# Usage: bash <(curl -sL https://raw.githubusercontent.com/xiaoxinmm/sysmon/master/install-beta.sh)
#
# ⚠️  This is a beta/dev version — NOT for production use!

set -e

echo "⚠️  WARNING: This is a beta/dev version of sysmon."
echo "   It includes experimental features (WebShell/Web Terminal)."
echo "   Do NOT use in production environments."
echo ""

# detect arch and os
ARCH=$(uname -m)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux) ;;
  darwin) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

BINARY="sysmon-${OS}-${ARCH}"
VERSION="v1.0.3-beta.dev"
URL="https://github.com/xiaoxinmm/sysmon/releases/download/${VERSION}/${BINARY}.tar.gz"

echo "Downloading sysmon ${VERSION} (beta) for ${OS}/${ARCH}..."
TMP=$(mktemp -d)
curl -sL "$URL" -o "${TMP}/sysmon.tar.gz"
tar xzf "${TMP}/sysmon.tar.gz" -C "${TMP}"
chmod +x "${TMP}/${BINARY}"

# install binary
sudo mv "${TMP}/${BINARY}" /usr/local/bin/sysmon
rm -rf "$TMP"

# create default config if not exists
if [ ! -f /etc/sysmon.json ]; then
  sudo tee /etc/sysmon.json > /dev/null << 'EOF'
{
  "port": 8888,
  "refreshInterval": 1500,
  "maxProcesses": 50,
  "password": "",
  "historyDuration": 3600,
  "enableShell": false,
  "shell_password": ""
}
EOF
  echo "Created default config at /etc/sysmon.json"
fi

# create systemd service if applicable
if [ ! -f /etc/systemd/system/sysmon.service ] && command -v systemctl &>/dev/null; then
  sudo tee /etc/systemd/system/sysmon.service > /dev/null << 'EOF'
[Unit]
Description=sysmon system monitor
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sysmon -config /etc/sysmon.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  sudo systemctl daemon-reload
  sudo systemctl enable sysmon
  echo "Created systemd service"
fi

echo ""
echo "✅ sysmon ${VERSION} (beta) installed to /usr/local/bin/sysmon"
echo ""
echo "⚠️  To enable WebShell (Web Terminal), edit /etc/sysmon.json:"
echo '   "enableShell": true,'
echo '   "shell_password": "your-secure-password"'
echo ""
echo "   IMPORTANT: Set both 'password' and 'shell_password' before enabling shell!"
echo ""
echo "Start with:"
echo "  systemctl start sysmon"
echo "  # or"
echo "  sysmon -config /etc/sysmon.json"
echo ""
echo "Open http://localhost:8888 in your browser"
