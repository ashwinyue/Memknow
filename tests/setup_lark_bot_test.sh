#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/setup_lark_bot.sh"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

MOCK_BIN="$TMPDIR/bin"
mkdir -p "$MOCK_BIN"

LOG_FILE="$TMPDIR/mock.log"
CONFIG_FILE="$TMPDIR/config.yaml"

cat > "$MOCK_BIN/lark-cli" <<'EOF'
#!/bin/bash
set -euo pipefail

echo "$*" >> "$MOCK_LOG_FILE"

if [[ "$1" == "config" && "$2" == "init" && "$3" == "--new" ]]; then
  exit 0
fi

if [[ "$1" == "auth" && "$2" == "login" && "$3" == "--recommend" ]]; then
  exit 0
fi

if [[ "$1" == "config" && "$2" == "show" ]]; then
  cat <<'JSON'
Config file path: /tmp/fake-lark-config.json
{
  "appId": "cli_mock_app_123",
  "appSecret": "****",
  "brand": "feishu",
  "lang": "zh",
  "profile": "cli_mock_app_123",
  "users": "Tester (ou_xxx)"
}
JSON
  exit 0
fi

echo "unexpected args: $*" >&2
exit 1
EOF
chmod +x "$MOCK_BIN/lark-cli"

OUTPUT_FILE="$TMPDIR/output.txt"
PATH="$MOCK_BIN:$PATH" \
MOCK_LOG_FILE="$LOG_FILE" \
CONFIG_FILE="$CONFIG_FILE" \
bash "$SCRIPT" demo-bot ./workspaces/demo-bot > "$OUTPUT_FILE"

grep -q '^config init --new$' "$LOG_FILE"
grep -q '^auth login --recommend$' "$LOG_FILE"
grep -q '^config show$' "$LOG_FILE"
grep -q '请手动将 config.yaml 中的 feishu_app_secret' "$OUTPUT_FILE"
grep -q '内置模板' "$OUTPUT_FILE"
grep -q '^apps:$' "$CONFIG_FILE"
grep -q 'id: "demo-bot"' "$CONFIG_FILE"

PATH="$MOCK_BIN:$PATH" \
MOCK_LOG_FILE="$LOG_FILE" \
CONFIG_FILE="$CONFIG_FILE" \
bash "$SCRIPT" second-bot ./workspaces/second-bot > "$OUTPUT_FILE"

test "$(grep -c '^apps:$' "$CONFIG_FILE")" = "1"
grep -q 'id: "demo-bot"' "$CONFIG_FILE"
grep -q 'id: "second-bot"' "$CONFIG_FILE"

echo "setup_lark_bot.sh test passed"
