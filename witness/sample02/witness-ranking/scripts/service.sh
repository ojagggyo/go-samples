#!/bin/bash
set -e

SERVICE="witness-ranking"
UPDATE_SERVICE="witness-ranking-update"
UPDATE_TIMER="witness-ranking-update.timer"

#PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${PROJECT_DIR}/witness-ranking"
RUN_USER="$(id -un)"

usage() {
    cat <<'EOF'
使い方:
  ./service.sh build
  ./service.sh install
  ./service.sh start
  ./service.sh stop
  ./service.sh restart
  ./service.sh status
  ./service.sh logs
  ./service.sh update
  ./service.sh timer
  ./service.sh uninstall
EOF
}

build() {
    echo "=== Go build ==="
    cd "$PROJECT_DIR"
    go build -o witness-ranking .
    echo "Build completed: $BIN"
}

install() {
    build

    echo "=== Creating ${SERVICE}.service ==="
    sudo tee "/etc/systemd/system/${SERVICE}.service" >/dev/null <<EOF
[Unit]
Description=Witness Ranking Web Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
WorkingDirectory=${PROJECT_DIR}
Environment=LISTEN_ADDR=:18080
Environment="STEEM_RPC_URL=https://api.steememory.com"
ExecStart=${BIN} serve
Restart=always
RestartSec=5


[Install]
WantedBy=multi-user.target
EOF

    echo "=== Creating ${UPDATE_SERVICE}.service ==="
    sudo tee "/etc/systemd/system/${UPDATE_SERVICE}.service" >/dev/null <<EOF
[Unit]
Description=Witness Ranking Update
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=${RUN_USER}
WorkingDirectory=${PROJECT_DIR}
ExecStart=${BIN} update
EOF

    echo "=== Creating ${UPDATE_TIMER} ==="
    sudo tee "/etc/systemd/system/${UPDATE_TIMER}" >/dev/null <<EOF
[Unit]
Description=Run Witness Ranking Update Every Minute

[Timer]
OnBootSec=30
OnUnitActiveSec=60
Persistent=true

[Install]
WantedBy=timers.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable "$SERVICE"
    sudo systemctl enable "$UPDATE_TIMER"
    sudo systemctl restart "$SERVICE"
    sudo systemctl restart "$UPDATE_TIMER"

    echo
    echo "=== Installation completed ==="
    sudo systemctl --no-pager status "$SERVICE" || true
}

start() {
    sudo systemctl start "$SERVICE"
    sudo systemctl start "$UPDATE_TIMER"
}

stop() {
    sudo systemctl stop "$SERVICE"
    sudo systemctl stop "$UPDATE_TIMER"
}

restart() {
    sudo systemctl restart "$SERVICE"
    sudo systemctl restart "$UPDATE_TIMER"
}

status() {
    echo "=== Web Server ==="
    systemctl --no-pager status "$SERVICE" || true
    echo
    echo "=== Update Timer ==="
    systemctl --no-pager status "$UPDATE_TIMER" || true
    echo
    systemctl list-timers --all | grep witness-ranking || true
}

logs() {
    sudo journalctl -u "$SERVICE" -u "$UPDATE_SERVICE" -f
}

update() {
    sudo systemctl start "$UPDATE_SERVICE"
    sudo journalctl -u "$UPDATE_SERVICE" -n 30 --no-pager
}

timer() {
    systemctl list-timers --all | grep witness-ranking || true
}

uninstall() {
    sudo systemctl stop "$SERVICE" 2>/dev/null || true
    sudo systemctl stop "$UPDATE_TIMER" 2>/dev/null || true
    sudo systemctl disable "$SERVICE" 2>/dev/null || true
    sudo systemctl disable "$UPDATE_TIMER" 2>/dev/null || true

    sudo rm -f "/etc/systemd/system/${SERVICE}.service"
    sudo rm -f "/etc/systemd/system/${UPDATE_SERVICE}.service"
    sudo rm -f "/etc/systemd/system/${UPDATE_TIMER}"

    sudo systemctl daemon-reload
    sudo systemctl reset-failed
    echo "=== Uninstall completed ==="
}

case "${1:-}" in
    build) build ;;
    install) install ;;
    start) start ;;
    stop) stop ;;
    restart) restart ;;
    status) status ;;
    logs) logs ;;
    update) update ;;
    timer) timer ;;
    uninstall) uninstall ;;
    *) usage ;;
esac
