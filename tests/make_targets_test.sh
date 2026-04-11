#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

MOCK_START="$TMPDIR/mock-start.sh"
MOCK_GO="$TMPDIR/go"
LOG_FILE="$TMPDIR/log.txt"

cat > "$MOCK_START" <<'EOF'
#!/bin/bash
set -euo pipefail
echo "start.sh $*" >> "$MAKE_TARGETS_LOG"
EOF
chmod +x "$MOCK_START"

cat > "$MOCK_GO" <<'EOF'
#!/bin/bash
set -euo pipefail
echo "go $*" >> "$MAKE_TARGETS_LOG"
EOF
chmod +x "$MOCK_GO"

PATH="$TMPDIR:$PATH" MAKE_TARGETS_LOG="$LOG_FILE" make -C "$ROOT_DIR" build START_SCRIPT="$MOCK_START" >/dev/null
PATH="$TMPDIR:$PATH" MAKE_TARGETS_LOG="$LOG_FILE" make -C "$ROOT_DIR" run START_SCRIPT="$MOCK_START" >/dev/null
PATH="$TMPDIR:$PATH" MAKE_TARGETS_LOG="$LOG_FILE" make -C "$ROOT_DIR" start START_SCRIPT="$MOCK_START" >/dev/null
PATH="$TMPDIR:$PATH" MAKE_TARGETS_LOG="$LOG_FILE" make -C "$ROOT_DIR" stop START_SCRIPT="$MOCK_START" >/dev/null
PATH="$TMPDIR:$PATH" MAKE_TARGETS_LOG="$LOG_FILE" make -C "$ROOT_DIR" restart START_SCRIPT="$MOCK_START" >/dev/null
PATH="$TMPDIR:$PATH" MAKE_TARGETS_LOG="$LOG_FILE" make -C "$ROOT_DIR" status START_SCRIPT="$MOCK_START" >/dev/null

grep -q '^go build -o server ./cmd/server$' "$LOG_FILE"
grep -q '^go run ./cmd/server -config ./config.yaml$' "$LOG_FILE"
grep -q '^start.sh start$' "$LOG_FILE"
grep -q '^start.sh stop$' "$LOG_FILE"
grep -q '^start.sh restart$' "$LOG_FILE"
grep -q '^start.sh status$' "$LOG_FILE"

echo "make targets test passed"
