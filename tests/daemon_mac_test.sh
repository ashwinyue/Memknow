#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/daemon-mac.sh"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

MOCK_BIN="$TMPDIR/bin"
mkdir -p "$MOCK_BIN"

LOG_FILE="$TMPDIR/launchctl.log"
HOME_DIR="$TMPDIR/home"
mkdir -p "$HOME_DIR"

cat > "$MOCK_BIN/launchctl" <<'EOF'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$MOCK_LAUNCHCTL_LOG"

if [[ "$1" == "print" ]]; then
  cat <<'OUT'
pid = 43210
state = running
OUT
fi
EOF
chmod +x "$MOCK_BIN/launchctl"

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

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_LAUNCHCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" install --binary "$TMPDIR/server" --work-dir "$TMPDIR" --config "$TMPDIR/config.yaml"

PLIST_PATH="$HOME_DIR/Library/LaunchAgents/com.memknow.service.plist"
test -f "$PLIST_PATH"
grep -q "$TMPDIR/server" "$PLIST_PATH"
grep -q "$TMPDIR" "$PLIST_PATH"
grep -q 'MEMKNOW_CONFIG' "$PLIST_PATH"
grep -q "$TMPDIR/config.yaml" "$PLIST_PATH"
grep -q '^bootstrap gui/' "$LOG_FILE"
grep -q '^kickstart -kp gui/' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_LAUNCHCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" status > "$TMPDIR/status.txt"
grep -q 'Installed: yes' "$TMPDIR/status.txt"
grep -q 'Running: yes' "$TMPDIR/status.txt"
grep -q 'PID: 43210' "$TMPDIR/status.txt"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_LAUNCHCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" stop
grep -q '^bootout gui/' "$LOG_FILE"

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_LAUNCHCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" start

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_LAUNCHCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" restart

PATH="$MOCK_BIN:$PATH" HOME="$HOME_DIR" MOCK_LAUNCHCTL_LOG="$LOG_FILE" \
  bash "$SCRIPT" uninstall
test ! -f "$PLIST_PATH"

echo "daemon mac test passed"
