# AGENTS.md

## Project Overview

**Memknow** — 基于飞书（Feishu）的长期记忆 AI Agent 平台。

每个 bot 拥有一个独立的 Claude Code workspace，具备结构化文件记忆系统。用户在飞书中发送消息，框架将消息路由到对应 workspace 执行 `claude` CLI，通过交互式卡片以流式方式返回结果。Bot 能够编辑文件、执行命令，并在持续的对话、heartbeat 和 schedule 中自我构建与进化。

## Architecture Overview

| Component | Tech | Description |
|---|---|---|
| **Server** (Go binary) | Go 1.25 + Viper + GORM | 单进程主程序：WS 接收、子进程编排、卡片回写、调度 |
| **Claude CLI** | 子进程 (`claude --output-format stream-json`) | 每个会话一个长驻 CLI 实例，stdin/stdout 走 stream-json |
| **Feishu Lark SDK** | `larksuite/oapi-sdk-go/v3` | WS 推送接收、消息/卡片发送、群事件订阅 |
| **SQLite (WAL)** | GORM + glebarez/sqlite | 单文件持久化：会话、消息、摘要、调度、FTS5 全文索引 |
| **Filelock binary** | `cmd/filelock` | 跨进程文件读写互斥（用于 workspace 文件并发保护） |

Infrastructure dependencies:
- **SQLite (WAL)** — 单文件 `bot.db`，启用 WAL 模式，无需外部数据库
- **Claude CLI** — 必须本地可用，通过 PATH 调用
- **Feishu Open Platform** — WS 推送 + Bot Token + 卡片消息 API

## Tech Stack

### Backend (Go)
- **Language**: Go 1.25.3
- **Config**: Viper（YAML 文件 + 文件监听热加载）
- **ORM**: GORM v1.25 + SQLite (glebarez 驱动，纯 Go 实现)
- **Feishu SDK**: `github.com/larksuite/oapi-sdk-go/v3`
- **Cron**: `github.com/go-co-op/gocron/v2`
- **File Lock**: `github.com/gofrs/flock`
- **YAML**: `gopkg.in/yaml.v3`
- **UUID**: `github.com/google/uuid`
- **Logging**: 标准库 `log/slog`（snake_case 键、结构化）

### Tooling
- **Lint**: `golangci-lint` (`.golangci.yml`) — 20+ linter，启用 gofumpt/gci/goimports formatter
- **Hooks**: `.husky/` git pre-commit — 大文件检查 + AGENTS.md 同步检查 + 并行 go lint + go test
- **CI AGENTS.md 自动同步**: `.github/workflows/update-agents-md.yml` — push 触发 claude-code-action（DeepSeek）自动读代码更新 AGENTS.md 并开 PR
- **CI Issue 模板**: `.github/ISSUE_TEMPLATE/` — Bug / Feature / RFC
- **PR 模板**: `.github/pull_request_template.md`
- **Daemon**: macOS launchd / Linux systemd 脚本（`daemon-mac.sh` / `daemon-linux.sh`）

## Project Structure

```
Memknow/
├── cmd/
│   ├── server/main.go                    # 入口：加载配置、连线各组件、启动；遵循 os.Exit(run()) 模式
│   └── filelock/main.go                  # 独立工具：以 flock 包装任意命令的文件互斥执行器
├── internal/
│   ├── config/                           # Viper YAML 配置 + 热加载回调
│   │   └── config.go                     #   Config / AppConfig / 校验 / 文件 watch
│   ├── model/                            # GORM 数据模型
│   │   ├── models.go                     #   Channel / Session / Message / SessionSummary / Schedule
│   │   └── session_types.go              #   SessionType 常量 + NormalizeSessionType()
│   ├── db/                               # SQLite 连接封装（WAL、外键、FTS5 索引初始化）
│   │   └── db.go
│   ├── claude/                           # Claude CLI 子进程编排
│   │   ├── executor.go                   #   ExecutorInterface + 默认实现 + 系统提示词渲染
│   │   ├── executor_unix.go              #   Unix 平台子进程管理
│   │   ├── executor_windows.go           #   Windows 平台子进程管理
│   │   ├── interactive.go                #   长驻交互式会话：stream-json 双工 IO
│   │   ├── prompts.go                    #   嵌入 prompts/ 目录下的 Markdown 模板
│   │   ├── prompts/                      #   Prompt 模板（base/chat/heartbeat/schedule + zh 中文变体）
│   │   └── *_test.go                     #   系统提示词、技能注入、E2E 测试
│   ├── feishu/                           # 飞书 WS 接收 + 卡片/消息发送
│   │   ├── receiver.go                   #   WS 客户端 + 事件路由（消息/反应/群成员变更）
│   │   ├── sender.go                     #   卡片 SendThinking / UpdateCard / Reaction / SendText / SendCard
│   │   └── *_test.go
│   ├── session/                          # channel_key → Worker 队列（核心调度层）
│   │   ├── manager.go                    #   Manager.Dispatch() 按 channel_key 路由到 Worker
│   │   ├── worker.go                     #   Worker 串行执行队列：会话生命周期、错误恢复、context 注入
│   │   ├── compact_stream.go             #   卡片流式渲染器（debounce + minInterval）
│   │   ├── context_payload.go            #   把历史摘要 + 命中检索结果拼成 prompt 前缀
│   │   ├── retriever.go                  #   FTS5 + 摘要混合检索 + 评分融合
│   │   ├── search.go / search_format.go  #   FTS5 query 净化（含 CJK bigram） + 结果分组
│   │   ├── summary.go                    #   异步会话摘要（由 claude 重新调用）
│   │   └── memory.go                     #   工作区级 MEMORY.md 注入
│   ├── heartbeat/                        # 内置 heartbeat（系统维护循环）
│   │   └── service.go                    #   定时跑 HEARTBEAT.md prompt + 配置 watch 重启
│   ├── schedule/                         # 业务调度（自然语言创建/管理）
│   │   ├── service.go                    #   gocron + 持久化 + ManageFromMessage 自然语言命令
│   │   └── intent_parser.go              #   用 claude LLM 解析「每小时提醒喝水」类输入到结构化 Intent
│   ├── cleanup/                          # 附件清理服务（cron 触发）
│   │   └── service.go                    #   按保留天数清理 workspace/.../attachments/
│   ├── websearch/                        # 本地联网搜索（Tavily / DuckDuckGo），CLI 形式给 claude 调用
│   │   ├── cli.go                        #   web-search 子命令入口（由主二进制 dispatch）
│   │   └── search.go                     #   provider 选择 + 超时控制
│   └── workspace/                        # workspace 目录初始化（每 app 一个）
│       ├── init.go                       #   从 embed FS 复制模板 + 写 .search.json + 生成 bin/web-search
│       ├── prompts.go                    #   渲染基础 prompt（注入路径变量）
│       ├── session_paths.go              #   session/heartbeat/schedule 目录约定
│       ├── template/                     #   嵌入式默认模板：SOUL/IDENTITY/USER/MEMORY/HEARTBEAT + skills/ + zh/en/
│       └── template_variants/            #   备选模板：product-assistant、code-review
├── workspaces/                           # 运行时 workspace 实例（gitignored）
├── tests/                                # 集成 / 端到端测试
├── docs/                                 # 设计文档
├── .github/                              # CI workflows + Issue/PR 模板
├── .husky/                               # Git pre-commit hooks
├── .golangci.yml                         # golangci-lint 配置
├── config.yaml                           # 运行配置（gitignored）
├── config.yaml.template                  # 配置示例
├── Makefile                              # 常用命令封装
├── daemon-mac.sh / daemon-linux.sh       # launchd / systemd 守护脚本
├── setup_lark_bot.sh                     # 新 bot 引导脚本
└── start.sh                              # 简易前台启动脚本
```

## Architecture

### 整体数据流

```
飞书用户
  → 飞书 WS 推送
  → feishu.Receiver         （解析消息 / 下载附件 / 群欢迎事件直接回复）
  → session.Manager.Dispatch()
  → session.Worker          （按 channel_key 串行队列）
  → claude.InteractiveExecutor  （长驻 claude CLI 子进程，stream-json 双工）
  → session.compactCardStreamer （流式渲染到飞书卡片，debounce + min-interval）
  → feishu.Sender           （PATCH 卡片展示最终结果 + Reaction 标记完成）
```

### channel_key 格式

`channel_key` 是所有路由、持久化、检索的稳定主键。

| 飞书渠道 | channel_key |
|---|---|
| 单聊 | `p2p:{chat_id}:{app_id}` |
| 群聊 | `group:{chat_id}:{app_id}` |
| 话题群 | `thread:{chat_id}:{thread_id}:{app_id}` |

修改 channel_key 格式属于破坏性变更，必须同步更新：`internal/feishu/receiver.go`、`internal/session/`、`internal/cleanup/`、所有相关测试。

### Session Types

会话目录按类型物理隔离：

- `sessions/chat/<session-id>/`
- `sessions/heartbeat/<session-id>/`
- `sessions/schedule/<session-id>/`

数据库 `sessions.type` 字段同步存储类型。**禁止只通过路径区分类型而不维护 DB 字段**。

### Heartbeat vs Schedule（必读）

| | Heartbeat | Schedule |
|---|---|---|
| **属性** | 系统内置维护循环 | 业务调度（用户驱动） |
| **触发** | `config.yaml` 配置间隔 | 用户自然语言 / API |
| **执行** | 读取 workspace 的 `HEARTBEAT.md` 作为 prompt | 用户创建时定义的 prompt + cron |
| **管理** | 仅通过 `config.yaml` | 自然语言（"每小时提醒喝水" / "把喝水提醒改成每天10点" / "删掉喝水提醒" / "我的提醒有哪些"） |
| **持久化** | 无（无状态循环） | `schedules` 表 |
| **owner 包** | `internal/heartbeat` | `internal/schedule` |

**禁止事项**：
- 不要把 heartbeat 实现为生成 task YAML 文件
- 不要让 schedule 创建依赖 agent 写 YAML（必须走结构化 Intent）
- 不要混用两套机制

### Stream-JSON 协议（claude CLI 交互）

`internal/claude/interactive.go` 维护长驻 claude 进程：

- **输入**：`--input-format stream-json` — 每行一个 user message JSON
- **输出**：`--output-format stream-json` — 每行一个 event JSON（thinking / tool_use / tool_result / text / result）
- **会话恢复**：通过 `--resume <claude_session_id>` 重连，断线由 `alive atomic.Bool` 标记后下次自动重建
- **环境隔离**：剥离 `CLAUDECODE` / `CLAUDE_CODE_*` 前缀的环境变量，避免被外层 claude 环境污染

## Workspace Assumptions

每个 app workspace 在启动时由 `internal/workspace/init.go` 检查并按需生成：

| 文件 / 目录 | 用途 |
|---|---|
| `SOUL.md` | 灵魂与信念 — 不可丢的核心定义 |
| `IDENTITY.md` | 身份与人格 |
| `USER.md` | 用户偏好、沟通风格、已知事实 |
| `MEMORY.md` | 核心记忆与待办 |
| `HEARTBEAT.md` | heartbeat 提示词 / 检查清单 |
| `skills/` | 技能索引（按需 `Read` 加载完整文件） |
| `memory/` | 长期记忆文件存储区 |
| `sessions/<type>/<session-id>/` | 单次会话工作目录 |
| `bin/web-search` | 本地 web 搜索入口（指向主二进制 `web-search` 子命令） |
| `.search.json` | 从全局 `config.yaml.web_search` 派生的运行时配置 |

**约定**：
- 缺失文件由 `internal/workspace/template/` 内嵌默认填充
- AI 可在 heartbeat / 对话中编辑 `HEARTBEAT.md` 调整 checklist
- 不要在 `~/.claude/projects/` 或外部路径写记忆，必须落在 workspace 内

## Development Rules

### Lint & Formatting
- 提交前运行 `make lint` 或依赖 husky 自动触发
- gci 顺序：`standard` → `default` → `prefix(github.com/ashwinyue/Memknow)`
- 错误消息：静态字符串用 `errors.New(...)`；含变量的用 `fmt.Errorf(... %w, err)`
- slog 键名：snake_case（`message_id` 而非 `messageID`）；禁止用 `time` / `level` / `msg` / `source` 这类保留键
- main 函数：使用 `os.Exit(run())` 模式，避免 `os.Exit` 跳过 `defer`

### 修改 session 布局
必须同步更新：
- `internal/model` (Session struct + type 字段)
- `internal/workspace` (session_paths.go 路径约定)
- `internal/claude` (executor 写入 SESSION_CONTEXT.md 路径)
- `internal/cleanup` (清理路径需匹配)
- 相关测试 + 本文档

### 修改 heartbeat 行为
必须同步更新：
- `internal/heartbeat/service.go`
- `config.yaml.template` 的 `heartbeat:` 段
- `internal/workspace/template/zh/HEARTBEAT.md` 与 `internal/workspace/template/en/HEARTBEAT.md`（如果路径变更）
- 所有提及 heartbeat 的文档

### 修改 web 搜索行为
必须同步更新：
- `internal/websearch` (provider 实现)
- `internal/workspace` (`bin/web-search` 入口生成 + `.search.json` 派生逻辑)
- `config.yaml.template` 的 `web_search:` 段
- 相关文档与 workspace skills

### Schedule（自然语言提醒）
- 新建 / 修改 / 删除 / 查询全部走自然语言 → LLM 结构化 Intent
- **禁止**生成 task YAML 文件来表示 schedule
- 解析失败时回退到关键字规则（`ParseManageIntent`）

### Filesystem 并发
- 跨进程 / 跨 goroutine 写入同一 workspace 文件时使用 `cmd/filelock` 包装
- workspace 内部仍允许直接读写，由 session worker 串行化保证安全

## Verification

完成实质性改动前必跑：

```bash
go build ./...
go test ./...
go vet ./...
golangci-lint run ./...
```

发布前额外验证：

```bash
make build           # 产出 ./server
./server -config config.yaml   # 本地启动校验
```

## Common Commands

| Command | Description |
|---|---|
| `make setup` | 安装 git pre-commit hooks（`.husky/`） |
| `make build` | 编译 `./server` 二进制 |
| `make run` | 直接 `go run`（使用 `./config.yaml`） |
| `make test` | `go test ./... -v -race` |
| `make lint` | `golangci-lint run ./...` |
| `make vet` | `go vet ./...` |
| `make tidy` | `go mod tidy` |
| `make fs` | 新建 bot workspace（交互式选模板） |
| `make daemon-install` | 注册 launchd/systemd 守护进程 |
| `make daemon-start` / `ds` | 启动守护进程 |
| `make daemon-stop` / `dsp` | 停止守护进程 |
| `make daemon-restart` / `dr` | 重启（先 kill 8786 端口） |
| `make daemon-status` | 查看守护状态 |
| `./server web-search --config <path> <query>` | 调用内置 web 搜索（claude 工具调用入口） |

## Configuration

主配置文件 `config.yaml`（由 `config.yaml.template` 复制并修改），核心字段：

| Section | Purpose |
|---|---|
| `server` | HTTP 健康检查端口（默认 8786） |
| `claude` | CLI 超时、最大轮次（认证由 `~/.claude/settings.json` 管理，框架不注入 `ANTHROPIC_*`） |
| `session` | Worker idle 超时、可选 probe |
| `cleanup` | 附件保留天数 + cron 表达式 |
| `heartbeat` | 内置维护循环间隔 + 通知目标（可选） |
| `web_search` | Tavily key / base_url / 超时（同时写到每个 workspace 的 `.search.json`） |
| `language` | `zh` / `en`，决定使用哪套 prompt 模板 |
| `apps[]` | 多 bot 配置数组 |

每个 app 字段：
- `id` — 框架内部唯一标识
- `feishu_app_id` / `feishu_app_secret` / `feishu_verification_token` / `feishu_encrypt_key`
- `workspace_dir` — workspace 路径
- `workspace_mode` — `work`（流式卡片显示工具调用）/ `companion`（仅输出最终文本）
- `workspace_template` — `default` / `product-assistant` / `code-review`
- `allowed_chats[]` — 白名单 chat_id（空 = 全部允许）
- `claude.permission_mode` — `acceptEdits` / `plan` / `default`
- `claude.model` — 覆盖 CLI 默认模型（可选）
- `claude.allowed_tools` — 工具白名单

**热加载**：`config.yaml` 修改后由 fsnotify 检测，目前 heartbeat 段会自动重启对应服务。其他段需要重启进程。

## Database Tables

- `sessions` — 会话元数据（id、channel_key、type、status、claude_session_id、title、时间戳）
- `messages` — 消息内容（session_id、role、content、attachments、created_at）
- `messages_fts` — FTS5 全文索引（自动维护，CJK bigram 净化由 `internal/session/search.go`）
- `session_summaries` — 会话摘要（异步生成，供长会话压缩）
- `schedules` — 业务调度（id、app_id、name、cron_expr、command、created_by、enabled）

## Safety / Operational Notes

- 主进程通过 `memknow.lock` 单实例锁防止 launchd/KeepAlive 启动重复实例
- WS 重连由 lark SDK 自动处理；断流时 session worker 不会丢弃已入队消息
- Claude CLI 子进程崩溃时由 `alive.Store(false)` 标记，下次请求自动重建并 `--resume` 上次 session
- 大附件下载有 100 MiB 上限（`maxAttachmentBytes`）
- HTTP 服务有 5s/10s 读写超时；shutdown 有 10s 超时
