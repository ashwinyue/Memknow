#!/bin/bash

set -euo pipefail

SERVICE="memknow.service"
DEFAULT_WORK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_BINARY="$DEFAULT_WORK_DIR/server"
DEFAULT_CONFIG="$DEFAULT_WORK_DIR/config.yaml"
DEFAULT_STDOUT="$DEFAULT_WORK_DIR/server.log"
DEFAULT_STDERR="$DEFAULT_WORK_DIR/server.log.wf"

COMMAND="${1:-}"
if [[ -z "$COMMAND" ]]; then
    echo "Usage: $0 {install|uninstall|start|stop|restart|status} [--binary PATH] [--work-dir PATH] [--config PATH]"
    exit 1
fi
shift || true

BINARY="$DEFAULT_BINARY"
WORK_DIR="$DEFAULT_WORK_DIR"
CONFIG_PATH="$DEFAULT_CONFIG"
STDOUT_PATH="$DEFAULT_STDOUT"
STDERR_PATH="$DEFAULT_STDERR"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --binary)
            if [[ -z "${2:-}" ]]; then
                echo "Error: --binary requires a value"
                exit 1
            fi
            BINARY="$2"
            shift 2
            ;;
        --work-dir)
            if [[ -z "${2:-}" ]]; then
                echo "Error: --work-dir requires a value"
                exit 1
            fi
            WORK_DIR="$2"
            shift 2
            ;;
        --config)
            if [[ -z "${2:-}" ]]; then
                echo "Error: --config requires a value"
                exit 1
            fi
            CONFIG_PATH="$2"
            shift 2
            ;;
        --stdout)
            if [[ -z "${2:-}" ]]; then
                echo "Error: --stdout requires a value"
                exit 1
            fi
            STDOUT_PATH="$2"
            shift 2
            ;;
        --stderr)
            if [[ -z "${2:-}" ]]; then
                echo "Error: --stderr requires a value"
                exit 1
            fi
            STDERR_PATH="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1"
            exit 1
            ;;
    esac
done

user_systemd_dir() {
    echo "$HOME/.config/systemd/user"
}

system_systemd_dir() {
    echo "/etc/systemd/system"
}

detect_systemd_mode() {
    if systemctl --user daemon-reload >/dev/null 2>&1; then
        echo "user"
    else
        echo "system"
    fi
}

SERVICE_MODE="$(detect_systemd_mode)"

systemd_dir() {
    if [[ "$SERVICE_MODE" == "user" ]]; then
        user_systemd_dir
    else
        system_systemd_dir
    fi
}

service_path() {
    echo "$(systemd_dir)/$SERVICE"
}

sc() {
    if [[ "$SERVICE_MODE" == "user" ]]; then
        systemctl --user "$@"
    else
        systemctl "$@"
    fi
}

ensure_linux() {
    if [[ "$(uname -s)" != "Linux" ]]; then
        echo "This script only supports Linux systemd"
        exit 1
    fi
}

require_file() {
    local path="$1"
    local name="$2"
    if [[ ! -e "$path" ]]; then
        echo "$name does not exist: $path"
        exit 1
    fi
}

abs_path() {
    local path="$1"
    if [[ -d "$path" ]]; then
        (cd "$path" && pwd)
        return
    fi
    local dir
    dir="$(dirname "$path")"
    local base
    base="$(basename "$path")"
    (cd "$dir" && printf '%s/%s\n' "$(pwd)" "$base")
}

write_service() {
    local svc
    svc="$(service_path)"
    mkdir -p "$(systemd_dir)"

    cat > "$svc" <<EOF
[Unit]
Description=Memknow Server
After=network.target

[Service]
Type=simple
ExecStart=$BINARY -config $CONFIG_PATH
WorkingDirectory=$WORK_DIR
Environment=MEMKNOW_CONFIG=$CONFIG_PATH
Environment=PATH=$PATH
Restart=on-success
StandardOutput=append:$STDOUT_PATH
StandardError=append:$STDERR_PATH

[Install]
WantedBy=default.target
EOF
}

install_service() {
    ensure_linux
    require_file "$BINARY" "binary"
    require_file "$WORK_DIR" "work dir"
    require_file "$CONFIG_PATH" "config"
    BINARY="$(abs_path "$BINARY")"
    WORK_DIR="$(abs_path "$WORK_DIR")"
    CONFIG_PATH="$(abs_path "$CONFIG_PATH")"
    STDOUT_PATH="$(abs_path "$STDOUT_PATH")"
    STDERR_PATH="$(abs_path "$STDERR_PATH")"

    write_service
    sc daemon-reload
    sc enable "$SERVICE"
    sc restart "$SERVICE"
    echo "Installed: $(service_path)"
}

uninstall_service() {
    ensure_linux
    sc stop "$SERVICE" >/dev/null 2>&1 || true
    sc disable "$SERVICE" >/dev/null 2>&1 || true
    rm -f "$(service_path)"
    sc daemon-reload
    echo "Uninstalled: $(service_path)"
}

start_service() {
    ensure_linux
    if [[ ! -f "$(service_path)" ]]; then
        echo "Service not installed: $(service_path)"
        exit 1
    fi
    sc start "$SERVICE"
    echo "Started: $SERVICE"
}

stop_service() {
    ensure_linux
    sc stop "$SERVICE"
    echo "Stopped: $SERVICE"
}

restart_service() {
    ensure_linux
    sc restart "$SERVICE"
    echo "Restarted: $SERVICE"
}

status_service() {
    ensure_linux
    local svc
    local installed="no"
    local running="no"
    svc="$(service_path)"

    if [[ -f "$svc" ]]; then
        installed="yes"
        if sc is-active --quiet "$SERVICE" 2>/dev/null; then
            running="yes"
        fi
    fi

    echo "Installed: $installed"
    echo "Running: $running"
    echo "Mode: $SERVICE_MODE"
    if [[ "$installed" == "yes" ]]; then
        echo "Service: $svc"
        sc status "$SERVICE" --no-pager || true
    fi
}

case "$COMMAND" in
    install)
        install_service
        ;;
    uninstall)
        uninstall_service
        ;;
    start)
        start_service
        ;;
    stop)
        stop_service
        ;;
    restart)
        restart_service
        ;;
    status)
        status_service
        ;;
    *)
        echo "Usage: $0 {install|uninstall|start|stop|restart|status} [--binary PATH] [--work-dir PATH] [--config PATH]"
        exit 1
        ;;
esac
