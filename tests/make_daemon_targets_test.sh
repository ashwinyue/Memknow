#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

MOCK_DAEMON="$TMPDIR/mock-daemon.sh"
LOG_FILE="$TMPDIR/log.txt"

cat > "$MOCK_DAEMON" <<'EOF'
#!/bin/bash
set -euo pipefail
echo "$*" >> "$MAKE_DAEMON_LOG"
EOF
chmod +x "$MOCK_DAEMON"

MAKE_DAEMON_LOG="$LOG_FILE" make -C "$ROOT_DIR" daemon-install \
  DAEMON_SCRIPT="$MOCK_DAEMON" \
  DAEMON_BINARY="./server-bin" \
  DAEMON_WORK_DIR="./workspace-root" \
  CONFIG="./conf.yaml" >/dev/null
MAKE_DAEMON_LOG="$LOG_FILE" make -C "$ROOT_DIR" daemon-start DAEMON_SCRIPT="$MOCK_DAEMON" >/dev/null
MAKE_DAEMON_LOG="$LOG_FILE" make -C "$ROOT_DIR" daemon-stop DAEMON_SCRIPT="$MOCK_DAEMON" >/dev/null
MAKE_DAEMON_LOG="$LOG_FILE" make -C "$ROOT_DIR" daemon-restart DAEMON_SCRIPT="$MOCK_DAEMON" >/dev/null
MAKE_DAEMON_LOG="$LOG_FILE" make -C "$ROOT_DIR" daemon-status DAEMON_SCRIPT="$MOCK_DAEMON" >/dev/null
MAKE_DAEMON_LOG="$LOG_FILE" make -C "$ROOT_DIR" daemon-uninstall DAEMON_SCRIPT="$MOCK_DAEMON" >/dev/null

grep -q '^install --binary ./server-bin --work-dir ./workspace-root --config ./conf.yaml$' "$LOG_FILE"
grep -q '^start$' "$LOG_FILE"
grep -q '^stop$' "$LOG_FILE"
grep -q '^restart$' "$LOG_FILE"
grep -q '^status$' "$LOG_FILE"
grep -q '^uninstall$' "$LOG_FILE"

echo "make daemon targets test passed"
