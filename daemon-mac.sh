#!/bin/bash

set -euo pipefail

LABEL="com.memknow.service"
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

launch_agents_dir() {
    echo "$HOME/Library/LaunchAgents"
}

plist_path() {
    echo "$(launch_agents_dir)/$LABEL.plist"
}

domain() {
    echo "gui/$(id -u)"
}

target() {
    echo "$(domain)/$LABEL"
}

ensure_macos() {
    if [[ "$(uname -s)" != "Darwin" ]]; then
        echo "This script only supports macOS launchd"
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

escape_xml() {
    local value="$1"
    value="${value//&/&amp;}"
    value="${value//</&lt;}"
    value="${value//>/&gt;}"
    printf '%s' "$value"
}

write_plist() {
    local plist
    plist="$(plist_path)"

    mkdir -p "$(launch_agents_dir)"

    cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>$(escape_xml "$BINARY")</string>
        <string>-config</string>
        <string>$(escape_xml "$CONFIG_PATH")</string>
    </array>
    <key>WorkingDirectory</key>
    <string>$(escape_xml "$WORK_DIR")</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <true/>
    </dict>
    <key>EnvironmentVariables</key>
    <dict>
        <key>MEMKNOW_CONFIG</key>
        <string>$(escape_xml "$CONFIG_PATH")</string>
        <key>PATH</key>
        <string>$(escape_xml "${PATH:-/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin}")</string>
    </dict>
    <key>StandardOutPath</key>
    <string>$(escape_xml "$STDOUT_PATH")</string>
    <key>StandardErrorPath</key>
    <string>$(escape_xml "$STDERR_PATH")</string>
</dict>
</plist>
EOF
}

install_service() {
    ensure_macos
    require_file "$BINARY" "binary"
    require_file "$WORK_DIR" "work dir"
    require_file "$CONFIG_PATH" "config"
    BINARY="$(abs_path "$BINARY")"
    WORK_DIR="$(abs_path "$WORK_DIR")"
    CONFIG_PATH="$(abs_path "$CONFIG_PATH")"
    STDOUT_PATH="$(abs_path "$STDOUT_PATH")"
    STDERR_PATH="$(abs_path "$STDERR_PATH")"

    write_plist
    launchctl bootout "$(target)" >/dev/null 2>&1 || true
    launchctl bootstrap "$(domain)" "$(plist_path)"
    launchctl kickstart -kp "$(target)"
    echo "Installed: $(plist_path)"
}

uninstall_service() {
    ensure_macos
    launchctl bootout "$(target)" >/dev/null 2>&1 || true
    rm -f "$(plist_path)"
    echo "Uninstalled: $(plist_path)"
}

start_service() {
    ensure_macos
    if [[ ! -f "$(plist_path)" ]]; then
        echo "Plist not installed: $(plist_path)"
        exit 1
    fi
    launchctl bootstrap "$(domain)" "$(plist_path)" >/dev/null 2>&1 || true
    launchctl kickstart -kp "$(target)"
    echo "Started: $LABEL"
}

stop_service() {
    ensure_macos
    launchctl bootout "$(target)"
    echo "Stopped: $LABEL"
}

restart_service() {
    ensure_macos
    launchctl bootout "$(target)" 2>/dev/null || true
    # 等待服务完全停止，避免 bootstrap 时冲突
    sleep 0.5
    if [[ ! -f "$(plist_path)" ]]; then
        echo "Plist not installed: $(plist_path)"
        exit 1
    fi
    launchctl bootstrap "$(domain)" "$(plist_path)"
    launchctl kickstart -kp "$(target)"
    echo "Restarted: $LABEL"
}

status_service() {
    ensure_macos
    local plist
    local installed="no"
    local running="no"
    local pid=""
    local last_exit=""
    local output=""
    plist="$(plist_path)"

    if [[ -f "$plist" ]]; then
        installed="yes"
        output="$(launchctl print "$(target)" 2>/dev/null || true)"
        if grep -q 'state = running' <<<"$output"; then
            running="yes"
        fi
        pid="$(sed -n 's/.*pid = \([0-9][0-9]*\).*/\1/p' <<<"$output" | head -n 1)"
        last_exit="$(sed -n 's/.*last exit code = \(.*\)/\1/p' <<<"$output" | head -n 1)"
        if [[ -n "$pid" ]]; then
            running="yes"
        fi
    fi

    echo "Installed: $installed"
    echo "Running: $running"
    if [[ -n "$pid" ]]; then
        echo "PID: $pid"
    fi
    if [[ -n "$last_exit" ]]; then
        echo "Last exit: $last_exit"
    fi
    if [[ "$installed" == "yes" ]]; then
        echo "Plist: $plist"
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
