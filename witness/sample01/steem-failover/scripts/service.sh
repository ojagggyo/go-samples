#!/bin/bash
set -e

SERVICE="steem-failover"
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUN_USER="$(id -un)"

usage() {
    cat <<'EOF'
使い方:
  ./service.sh install
  ./service.sh start
  ./service.sh stop
  ./service.sh restart
  ./service.sh status
  ./service.sh logs
  ./service.sh uninstall
EOF
}

install() {
    echo "=== Creating ${SERVICE}.service ==="
    sudo tee "/etc/systemd/system/${SERVICE}.service" >/dev/null <<EOF
[Unit]
Description=Steem Witness Failover Controller
After=network-online.target
Wants=network-online.target

[Service]
Type=simple

User=steem
Group=steem

WorkingDirectory=/home/steem/github/go-samples/witness/sample01/steem-failover
EnvironmentFile=/home/steem/github/go-samples/witness/sample01/steem-failover/steem-failover.env
ExecStart=/home/steem/github/go-samples/witness/sample01/steem-failover/steem-failover

Restart=always
RestartSec=5

NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable "$SERVICE"
    sudo systemctl restart "$SERVICE"

    echo
    echo "=== Installation completed ==="
    sudo systemctl --no-pager status "$SERVICE" || true
}

start() {
    sudo systemctl start "$SERVICE"
}

stop() {
    sudo systemctl stop "$SERVICE"
}

restart() {
    sudo systemctl restart "$SERVICE"
}

status() {
    echo "=== steem-failover ==="
    systemctl --no-pager status "$SERVICE" || true
    echo
    systemctl list-timers --all | grep steem-failover || true
}

logs() {
    sudo journalctl -u "$SERVICE" -f -n 100
}

uninstall() {
    sudo systemctl stop "$SERVICE" 2>/dev/null || true
    sudo systemctl disable "$SERVICE" 2>/dev/null || true
    sudo rm -f "/etc/systemd/system/${SERVICE}.service"
    sudo systemctl daemon-reload
    sudo systemctl reset-failed
    echo "=== Uninstall completed ==="
}

case "${1:-}" in
    install) install ;;
    start) start ;;
    stop) stop ;;
    restart) restart ;;
    status) status ;;
    logs) logs ;;
    uninstall) uninstall ;;
    *) usage ;;
esac
