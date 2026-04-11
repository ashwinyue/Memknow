#!/bin/bash
# setup_lark_bot.sh — 通过 lark-cli 初始化应用，并将 app 信息追加到项目 config.yaml
#
# Usage:
#   ./setup_lark_bot.sh <app-id> <workspace-dir> [workspace-template]

set -euo pipefail

PLACEHOLDER_SECRET="__FILL_ME_FROM_LARK_CLI_OR_OPEN_PLATFORM__"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

info()    { echo -e "${GREEN}✅ $*${NC}"; }
warn()    { echo -e "${YELLOW}⚠️  $*${NC}"; }
error()   { echo -e "${RED}❌ $*${NC}" >&2; }
step()    { echo -e "${BOLD}── $*${NC}"; }

usage() {
    echo "Usage: $0 <app-id> <workspace-dir> [workspace-template]"
    echo ""
    echo "Arguments:"
    echo "  app-id        唯一应用标识（只含字母、数字、下划线、连字符）"
    echo "  workspace-dir      workspace 目录（绝对或相对路径）"
    echo "  workspace-template 可选，default / product-assistant / code-review（默认 default）"
    exit 1
}

if [[ $# -lt 2 ]]; then
    usage
fi

APP_ID="$1"
WORKSPACE_DIR="$2"
WORKSPACE_TEMPLATE="${3:-default}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LARK_CLI_BIN="${LARK_CLI_BIN:-lark-cli}"
CONFIG_FILE="${CONFIG_FILE:-$SCRIPT_DIR/config.yaml}"

if [[ ! "$APP_ID" =~ ^[a-zA-Z0-9_-]+$ ]]; then
    error "app-id 只能包含字母、数字、下划线、连字符，当前值: $APP_ID"
    exit 1
fi

case "$WORKSPACE_TEMPLATE" in
    default|product-assistant|code-review) ;;
    *)
        error "workspace-template 只能是 default / product-assistant / code-review，当前值: $WORKSPACE_TEMPLATE"
        exit 1
        ;;
esac

step "通过 lark-cli 创建或配置应用"
"$LARK_CLI_BIN" config init --new

step "通过 lark-cli 完成推荐授权"
"$LARK_CLI_BIN" auth login --recommend

step "读取当前 lark-cli 配置"
CONFIG_SHOW_OUTPUT="$("$LARK_CLI_BIN" config show)"

FEISHU_APP_ID="$(printf '%s\n' "$CONFIG_SHOW_OUTPUT" | python3 -c '
import json, sys

text = sys.stdin.read()
start = text.find("{")
if start == -1:
    raise SystemExit("failed to locate JSON payload in `lark-cli config show` output")
data = json.loads(text[start:])
app_id = data.get("appId", "").strip()
if not app_id:
    raise SystemExit("missing appId in `lark-cli config show` output")
print(app_id)
')"

info "已解析当前 lark-cli app_id: $FEISHU_APP_ID"

step "追加 config.yaml"

# Ensure config.yaml exists
if [[ ! -f "$CONFIG_FILE" ]]; then
    touch "$CONFIG_FILE"
fi

if grep -Eq "^[[:space:]]+- id: \"$APP_ID\"$" "$CONFIG_FILE"; then
    error "config.yaml 中已存在 app-id: $APP_ID"
    exit 1
fi

# Create workspace dir so it's ready (server will auto-init from template on first run)
mkdir -p "$WORKSPACE_DIR"

# Append app block to config.yaml
if ! grep -Eq '^apps:$' "$CONFIG_FILE"; then
cat >> "$CONFIG_FILE" <<EOF

apps:
EOF
fi

cat >> "$CONFIG_FILE" <<EOF
  - id: "$APP_ID"
    feishu_app_id: "$FEISHU_APP_ID"
    feishu_app_secret: "$PLACEHOLDER_SECRET"
    feishu_verification_token: "$PLACEHOLDER_SECRET"
    feishu_encrypt_key: ""
    workspace_dir: "$WORKSPACE_DIR"
    workspace_template: "$WORKSPACE_TEMPLATE"
    allowed_chats: []
    claude:
      permission_mode: "acceptEdits"
      allowed_tools:
        - "Bash"
        - "Read"
        - "Edit"
        - "Write"
EOF

info "已追加到 $CONFIG_FILE"

echo ""
warn "请手动将 config.yaml 中的 feishu_app_secret 和 feishu_verification_token 从占位符改为真实值。"
warn "补齐后首次启动 go run ./cmd/server 时，框架会使用内置模板自动初始化 workspace（template=${WORKSPACE_TEMPLATE}）。"
