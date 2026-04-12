#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/daemon-linux.sh"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

MOCK_BIN="$TMPDIR/bin"
mkdir -p "$MOCK_BIN"

LOG_FILE="$TMPDIR/systemctl.log"
HOME_DIR="$TMPDIR/home"
mkdir -p "$HOME_DIR"

# mock uname to return Linux
cat > "$MOCK_BIN/uname" <<'EOF'
#!/bin/bash
if [[ "$*" == "-s" ]]; then
  echo "Linux"
else
  /usr/bin/uname "$@"
fi
EOF
chmod +x "$MOCK_BIN/uname"

# mock systemctl
cat > "$MOCK_BIN/systemctl" <<EOF
#!/bin/bash
printf '%s\n' "\$*" >> "\$MOCK_SYSTEMCTL_LOG"

# daemon-reload detection used by detect_systemd_mode
if [[ "\$1" == "--user" && "\$2" == "daemon-reload" ]]; then
  exit 0
fi

if [[ "\$1" == "--user" && "\$2" == "is-active" && "\$3" == "--quiet" ]]; then
  exit 0
fi

if [[ "\$1" == "--user" && "\$2" == "status" ]]; then
  cat <<'OUT'
* memknow.service - Memknow Server
     Loaded: loaded
     Active: active (running) since Mon 2024-01-01 00:00:00 UTC
OUT
fi
EOF
chmod +x "$MOCK_BIN/systemctl"

cat > "$TMPDIR/server" <<'EOF'
#!/bin/bash
exit 0
EOF
chmod +x "$TMPDIR/server"

cat > "$TMPDIR/config.yaml" <<'EOF'
apps: []
server:
  port: 8080
EOF

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_SYSTEMCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" install --binary "$TMPDIR/server" --work-dir "$TMPDIR" --config "$TMPDIR/config.yaml"

SERVICE_PATH="$HOME_DIR/.config/systemd/user/memknow.service"
test -f "$SERVICE_PATH"
grep -q "$TMPDIR/server" "$SERVICE_PATH"
grep -q "$TMPDIR" "$SERVICE_PATH"
grep -q 'MEMKNOW_CONFIG' "$SERVICE_PATH"
grep -q "$TMPDIR/config.yaml" "$SERVICE_PATH"
grep -q '^--user daemon-reload$' "$LOG_FILE"
grep -q '^--user enable memknow.service$' "$LOG_FILE"
grep -q '^--user restart memknow.service$' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_SYSTEMCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" status > "$TMPDIR/status.txt"
grep -q 'Installed: yes' "$TMPDIR/status.txt"
grep -q 'Running: yes' "$TMPDIR/status.txt"
grep -q 'Mode: user' "$TMPDIR/status.txt"
grep -q '^--user status memknow.service --no-pager$' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_SYSTEMCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" stop
grep -q '^--user stop memknow.service$' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_SYSTEMCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" start
grep -q '^--user start memknow.service$' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_SYSTEMCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" restart
grep -q '^--user restart memknow.service$' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_SYSTEMCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" uninstall
test ! -f "$SERVICE_PATH"
grep -q '^--user stop memknow.service$' "$LOG_FILE"
grep -q '^--user disable memknow.service$' "$LOG_FILE"
grep -q '^--user daemon-reload$' "$LOG_FILE"

# ── WSL2 without systemd detection ───────────────────────────────────────────
WSL_TMPDIR="$TMPDIR/wsl-test"
mkdir -p "$WSL_TMPDIR/bin"

# mock systemctl that always fails daemon-reload
cat > "$WSL_TMPDIR/bin/systemctl" <<'EOF'
#!/bin/bash
exit 1
EOF
chmod +x "$WSL_TMPDIR/bin/systemctl"

# mock uname too
cp "$MOCK_BIN/uname" "$WSL_TMPDIR/bin/uname"

if ! PATH="$WSL_TMPDIR/bin:$PATH" HOME="$HOME_DIR" WSL_DISTRO_NAME="Ubuntu" \
  bash "$SCRIPT" install --binary "$TMPDIR/server" --work-dir "$TMPDIR" --config "$TMPDIR/config.yaml" > "$WSL_TMPDIR/out.txt" 2> "$WSL_TMPDIR/err.txt"; then
  : # expected to fail
else
  echo "Expected install to fail on WSL2 without systemd"
  exit 1
fi

grep -q 'WSL2 环境但未启用 systemd 支持' "$WSL_TMPDIR/err.txt"

echo "daemon linux test passed"
