#!/bin/bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

MOCK_SCRIPT="$TMPDIR/mock-setup-lark-bot.sh"
ARGS_FILE="$TMPDIR/args.txt"

cat > "$MOCK_SCRIPT" <<'EOF'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$@" > "$ARGS_FILE"
EOF
chmod +x "$MOCK_SCRIPT"

make -C "$ROOT_DIR" fs \
  APP_ID=demo-bot \
  WORKSPACE=./workspaces/demo-bot \
  TEMPLATE=product-assistant \
  SETUP_LARK_BOT_SCRIPT="$MOCK_SCRIPT" \
  ARGS_FILE="$ARGS_FILE" >/dev/null

ARG_0="$(sed -n '1p' "$ARGS_FILE")"
ARG_1="$(sed -n '2p' "$ARGS_FILE")"
ARG_2="$(sed -n '3p' "$ARGS_FILE")"

[[ "$ARG_0" == "demo-bot" ]]
[[ "$ARG_1" == "./workspaces/demo-bot" ]]
[[ "$ARG_2" == "product-assistant" ]]

printf 'interactive-bot\n2\n' | make -C "$ROOT_DIR" fs \
  SETUP_LARK_BOT_SCRIPT="$MOCK_SCRIPT" \
  ARGS_FILE="$ARGS_FILE" >/dev/null

ARG_0="$(sed -n '1p' "$ARGS_FILE")"
ARG_1="$(sed -n '2p' "$ARGS_FILE")"
ARG_2="$(sed -n '3p' "$ARGS_FILE")"

[[ "$ARG_0" == "interactive-bot" ]]
[[ "$ARG_1" == "./workspaces/interactive-bot" ]]
[[ "$ARG_2" == "product-assistant" ]]

make -C "$ROOT_DIR" fs \
  APP_ID=review-bot \
  WORKSPACE=./workspaces/review-bot \
  TEMPLATE=code-review \
  SETUP_LARK_BOT_SCRIPT="$MOCK_SCRIPT" \
  ARGS_FILE="$ARGS_FILE" >/dev/null

ARG_0="$(sed -n '1p' "$ARGS_FILE")"
ARG_1="$(sed -n '2p' "$ARGS_FILE")"
ARG_2="$(sed -n '3p' "$ARGS_FILE")"

[[ "$ARG_0" == "review-bot" ]]
[[ "$ARG_1" == "./workspaces/review-bot" ]]
[[ "$ARG_2" == "code-review" ]]

echo "make fs test passed"
