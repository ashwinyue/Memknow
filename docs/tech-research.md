# 技术调研报告

> 状态: 已落地
> 最后更新: 2026-04-10

## 一、飞书接入

### 1. WebSocket 模式

最终采用 `github.com/larksuite/oapi-sdk-go/v3/ws`：

- 本地开发无需公网 IP
- 长连接建立后无需逐次校验 webhook 签名
- 适合单机私有化部署

当前实现位于 `internal/feishu/receiver.go`。

### 2. 消息模型

消息接入后会被标准化为 `IncomingMessage`，包含：

- `app_id`
- `channel_key`
- `chat_type`
- `chat_id`
- `thread_id`
- `sender_id`
- `message_id`
- `prompt`
- `receive_id` / `receive_type`

当前 channel_key 约定：

| `ChatType` | channel_key |
|---|---|
| `p2p` | `p2p:{chat_id}:{app_id}` |
| `group` | `group:{chat_id}:{app_id}` |
| `topic` / `topic_group` | `thread:{chat_id}:{thread_id}:{app_id}` |

### 3. 消息发送

`internal/feishu/sender.go` 会按内容复杂度选择：

- 纯文本：`text`
- 一般 Markdown：`post`
- 含代码块 / 表格等复杂内容：`interactive card`

思考中状态和最终结果优先通过卡片 PATCH 更新。

---

## 二、技术选型

| 组件 | 选型 | 理由 |
|---|---|---|
| 飞书 SDK | `oapi-sdk-go/v3` | 官方 SDK，WS 支持完整 |
| 配置 | Viper | YAML 映射简单 |
| ORM | GORM | AutoMigrate 与模型组织方便 |
| 数据库 | `glebarez/sqlite` | CGO-free，单机友好 |
| 调度 | `gocron/v2` | 适合单机 cron 任务 |
| 日志 | `log/slog` | 标准库即可 |
| Claude 集成 | `exec.Cmd` + interactive session | 复用 Claude CLI 能力与 `--resume` |
| UUID | `google/uuid` | session / schedule / log 主键 |

SQLite 采用 WAL 模式，兼顾单机吞吐与稳定性。

---

## 三、Claude CLI 集成结论

### 1. 为什么选择 interactive executor

Memknow 当前采用长生命周期 interactive Claude 进程，而不是每次 one-shot 启动：

- 减少重复启动开销
- 更稳定地保留 `claude_session_id`
- 便于接入 stream-json 事件流

核心实现位于 `internal/claude/interactive.go`。

### 2. 关键参数

当前主要参数包括：

- `--output-format stream-json`
- `--input-format stream-json`
- `--permission-prompt-tool stdio`
- `--permission-mode <app.claude.permission_mode>`
- `--max-turns <cfg.claude.max_turns>`
- `--resume <claude_session_id>`（存在时）
- `--model <app.claude.model>`（存在时）
- `--allowedTools "..."`
- `--append-system-prompt "<base + workspace prompt>"`

不是通过 `--cwd` 或生成 `CLAUDE.md` 来注入 prompt，而是通过动态拼接的 system prompt。

### 3. Prompt 分层

最终方案：

- 框架 prompt：`chat.md` / `heartbeat.md` / `schedule.md`
- workspace prompt：`SOUL.md` + `IDENTITY.md` + skills 索引
- 用户与长期记忆：按需从 `USER.md`、`MEMORY.md`、`memory/*.md` 读取或检索注入

这样可以把“平台规则”和“bot 个性”分层维护。

### 4. 子进程安全

当前安全措施：

- `context` 控制生命周期
- 进程组隔离，避免孤儿进程
- `WaitDelay` 防止管道阻塞
- 过滤 `CLAUDECODE` / `CLAUDE_CODE_*` 环境变量
- 扫描缓冲提升到 1 MiB，避免大行截断

---

## 四、上下文增强

最终没有重建一个完整的“外部 prompt 编排器”，而是采用最小增强策略：

- 保留 Claude 自己的 `--resume`
- 用 `session_summaries` 处理跨 session 记忆
- 用 FTS5 做历史消息检索
- 用 overflow recovery 处理空结果场景

这是比“自己实现一套会话状态机”更轻量、也更符合当前产品阶段的方案。

---

## 五、调度与后台任务

### 1. Schedule

最终方案是数据库持久化而不是 YAML：

- 自然语言解析创建 schedule
- 存入 `schedules`
- 由 `gocron` 注册执行
- 执行日志写入 `schedule_logs`

### 2. Heartbeat

Heartbeat 是独立内置服务：

- 周期由 `config.yaml` 控制
- prompt 内容来自 workspace 的 `HEARTBEAT.md`
- 每次执行使用 `heartbeat` session 类型

---

## 六、结论

本项目的技术路线不是构建一个“大而全”的 Agent 平台，而是把现成可用的能力进行工程化编排：

- 飞书负责消息入口
- Claude CLI 负责智能执行
- Go 框架负责状态、调度、隔离与可运维性

这使得系统在保持较低复杂度的同时，已经具备可用的多 bot、长期记忆、定时任务与自维护能力。
