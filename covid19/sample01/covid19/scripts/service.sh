#!/bin/bash
set -e

SERVICE="covid19"
UPDATE_SERVICE="covid19-update"
UPDATE_TIMER="covid19-update.timer"

#PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${PROJECT_DIR}/covid19"
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
    go build -o covid19 .
    echo "Build completed: $BIN"
}

install() {
    build

    echo "=== Creating ${SERVICE}.service ==="
    sudo tee "/etc/systemd/system/${SERVICE}.service" >/dev/null <<EOF
[Unit]
Description=Steememory COVID19 Web Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
WorkingDirectory=${PROJECT_DIR}
Environment=LISTEN_ADDR=:18081
ExecStart=${BIN}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    echo "=== Creating ${UPDATE_SERVICE}.service ==="
    sudo tee "/etc/systemd/system/${UPDATE_SERVICE}.service" >/dev/null <<EOF
[Unit]
Description=Steememory COVID19 Weekly Update
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=${RUN_USER}
WorkingDirectory=${PROJECT_DIR}
ExecStart=${BIN} -update
EOF

    echo "=== Creating ${UPDATE_TIMER} ==="
    sudo tee "/etc/systemd/system/${UPDATE_TIMER}" >/dev/null <<EOF
[Unit]
Description=Run COVID19 Update Every Thursday at 00:00

[Timer]
OnCalendar=Thu *-*-* 00:00:00
Persistent=true
Unit=${UPDATE_SERVICE}

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
    echo
    sudo systemctl --no-pager status "$SERVICE" || true
    echo
    systemctl list-timers --all | grep "$UPDATE_TIMER" || true
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
    systemctl list-timers --all | grep "$UPDATE_TIMER" || true
}

logs() {
    sudo journalctl -u "$SERVICE" -u "$UPDATE_SERVICE" -f
}

update() {
    sudo systemctl start "$UPDATE_SERVICE"
    echo
    sudo journalctl -u "$UPDATE_SERVICE" -n 30 --no-pager
}

timer() {
    systemctl list-timers --all | grep "$UPDATE_TIMER" || true
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
