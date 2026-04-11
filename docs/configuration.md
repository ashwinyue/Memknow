# 配置参考

完整的 `config.yaml` 配置说明。

---

## 完整示例

```yaml
# 应用列表（支持配置多个飞书应用，每个对应一个 workspace）
apps:
  - id: "investment-assistant"          # 唯一标识（字母/数字/连字符）
    feishu_app_id: "cli_xxx"            # 飞书 App ID
    feishu_app_secret: "xxx"            # 飞书 App Secret
    feishu_verification_token: "xxx"    # 飞书事件订阅 Verification Token
    feishu_encrypt_key: ""              # 消息加密密钥（可选，不加密则留空）
    workspace_dir: "/data/workspaces/investment-assistant"
    allowed_chats: []                   # 白名单 chat_id 列表，空表示不限制
    claude:
      permission_mode: "acceptEdits"    # acceptEdits（推荐）或 bypassPermissions
      # model: "sonnet"                 # 可选，覆盖 Claude CLI 默认模型
      allowed_tools:                    # 允许 Claude 使用的工具，空表示不限制
        - "Bash"
        - "Read"
        - "Edit"
        - "Write"
        - "Glob"
        - "Grep"
        - "WebFetch"
        - "WebSearch"

  - id: "code-review"
    feishu_app_id: "cli_yyy"
    feishu_app_secret: "yyy"
    feishu_verification_token: "yyy"
    feishu_encrypt_key: ""
    workspace_dir: "/data/workspaces/code-review"
    allowed_chats:
      - "oc_abc123"                     # 仅允许特定群聊
    claude:
      permission_mode: "acceptEdits"
      allowed_tools:
        - "Read"
        - "Bash"

server:
  port: 8989                            # HTTP 监听端口（健康检查 GET /health）

claude:
  timeout_minutes: 5                    # 单次 claude 执行超时（分钟）
  max_turns: 20                         # claude CLI --max-turns 参数

session:
  worker_idle_timeout_minutes: 30       # Worker 空闲超时，触发 session 归档

cleanup:
  attachments_retention_days: 7         # session 归档后附件保留天数
  attachments_max_days: 30              # 强制清理天数上限
  schedule: "0 2 * * *"                 # 清理任务 cron

heartbeat:
  enabled: false                        # 是否开启内置 heartbeat
  interval_minutes: 720                 # 触发间隔（分钟）
  prompt_file: "HEARTBEAT.md"           # workspace 根目录中的 prompt 文件
```

---

## 字段说明

### `apps[]`

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 应用唯一标识 |
| `feishu_app_id` | string | 飞书开放平台 App ID |
| `feishu_app_secret` | string | 飞书开放平台 App Secret |
| `feishu_verification_token` | string | 事件订阅校验 Token |
| `feishu_encrypt_key` | string | 消息加密密钥（可选） |
| `workspace_dir` | string | workspace 根目录路径 |
| `workspace_mode` | string | `work`=显示进度卡片；`companion`=直接输出文本 |
| `workspace_template` | string | `default` / `product-assistant` / `code-review` |
| `allowed_chats` | []string | 白名单 chat_id，空数组表示不限制 |

### `apps[].claude`

| 字段 | 类型 | 说明 |
|------|------|------|
| `permission_mode` | string | `acceptEdits` 自动接受文件编辑；`bypassPermissions` 跳过所有确认（高风险） |
| `model` | string | 可选，原样传给 claude CLI 的 `--model` |
| `allowed_tools` | []string | 限制 Claude 可用工具，建议按最小权限原则配置 |

### `server`

| 字段 | 类型 | 说明 |
|------|------|------|
| `port` | int | HTTP 服务端口，提供 `/health` 健康检查 |

### `claude`

| 字段 | 类型 | 说明 |
|------|------|------|
| `timeout_minutes` | int | 单次执行超时 |
| `max_turns` | int | Claude 最大对话轮数 |

### `session`

| 字段 | 类型 | 说明 |
|------|------|------|
| `worker_idle_timeout_minutes` | int | Worker 空闲多久后自动归档 session |

### `cleanup`

| 字段 | 类型 | 说明 |
|------|------|------|
| `attachments_retention_days` | int | 归档后附件保留天数 |
| `attachments_max_days` | int | 附件强制清理上限天数 |
| `schedule` | string | 清理任务 cron 表达式 |

### `heartbeat`

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | bool | 是否开启内置 heartbeat |
| `interval_minutes` | int | heartbeat 执行间隔 |
| `prompt_file` | string | workspace 根目录中读取的 prompt 文件名 |
| `notify_target_type` | string | 通知目标类型：`p2p` / `group`，空表示不发送通知 |
| `notify_target_id` | string | 通知目标 ID |

---

## 支持的模型

Memknow 本身不维护模型列表。`apps[].claude.model` 中填写的值会**原样**传给 claude CLI 的 `--model` 参数。

常用 Anthropic 模型参考（以 CLI 实际支持为准）：

| 名称 | 说明 |
|------|------|
| `sonnet` | 最佳编码模型（推荐） |
| `opus` | 最深推理能力 |
| `haiku` | 轻量快速，成本最低 |
