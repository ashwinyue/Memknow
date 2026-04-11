# AGENTS.md

This repository runs a Feishu-based Claude Code framework in Go.

## Project Overview

**Memknow** — 基于 Feishu 的长期记忆 AI Agent 平台。

每个 bot 拥有一个独立的 Claude Code workspace，具备结构化文件记忆系统。用户在飞书中发送消息，框架将消息路由到对应 workspace 执行 `claude` CLI，通过交互式卡片返回结果。bot 能够编辑文件、执行命令，并在持续的对话与 heartbeat 中自我构建与进化。

## Directory Structure

```
Memknow/
├── cmd/
│   └── server/main.go          # 入口：加载配置、连线各组件、启动
├── internal/
│   ├── config/                 # Viper YAML 配置结构
│   ├── model/                  # GORM 数据模型
│   ├── db/                     # SQLite WAL 连接
│   ├── claude/                 # 子进程调用 claude CLI
│   ├── feishu/                 # WS 事件接收 + 卡片发送
│   ├── session/                # channel_key → Worker 队列
│   ├── heartbeat/              # 内置 heartbeat 调度
│   ├── schedule/               # 内置 schedule 调度（自然语言创建/管理）
│   ├── cleanup/                # 附件清理服务
│   ├── websearch/              # 本地联网搜索（Tavily / DuckDuckGo）
│   └── workspace/              # workspace 目录初始化
├── internal/workspace/template/ # 新 workspace 默认模板（内嵌到二进制中）
├── workspaces/                 # 运行时 workspace 实例
├── config.yaml.template        # 配置示例
└── go.mod / go.sum
```

## Architecture

### 整体数据流

```
飞书用户
  → 飞书 WS 推送
  → feishu.Receiver（解析消息 / 下载附件 / 欢迎事件直接回复）
  → session.Manager.Dispatch()
  → session.Worker（按 channel_key 串行队列）
  → claude.Executor（子进程 claude CLI，stream-json）
  → feishu.Sender（PATCH 卡片展示结果）
```

### channel_key 格式

| 飞书渠道 | channel_key |
|---|---|
| 单聊 | `p2p:{chat_id}:{app_id}` |
| 群聊 | `group:{chat_id}:{app_id}` |
| 话题群 | `thread:{chat_id}:{thread_id}:{app_id}` |

### Session Types

Session directories are physically separated by type:

- `sessions/chat/<session-id>/`
- `sessions/heartbeat/<session-id>/`
- `sessions/schedule/<session-id>/`

The database `sessions` table also stores the session `type`. Do not introduce path-only classification without keeping the DB model in sync.

### Heartbeat vs Schedule

- `heartbeat` is a built-in system maintenance loop.
- `schedule` is a built-in business scheduler owned by `internal/schedule`.
- Do not implement heartbeat by generating task YAML files.
- Heartbeat behavior is configured in `config.yaml` under `heartbeat`.
- Users create/manage schedules through natural language ("每小时提醒我喝水", "我的提醒有哪些", "把喝水提醒改成每天10点", "删掉喝水提醒").

## Workspace Assumptions

Each app workspace contains:

- `SOUL.md` (generated from embedded default if missing)
- `IDENTITY.md` (generated from embedded default if missing)
- `USER.md` (generated from embedded default if missing)
- `MEMORY.md` (generated from embedded default if missing)
- `HEARTBEAT.md` (generated from embedded default if missing)
- `bin/web-search` (generated local web search entrypoint)
- `.search.json` (derived runtime search config from global `config.yaml`)
- `skills/` (generated from embedded defaults if missing)
- `memory/`
- `sessions/`

`HEARTBEAT.md` is read by the built-in heartbeat runner. It is not a task template. AI may edit it during heartbeat turns to adjust the checklist.

## Development Rules

- Keep runtime behavior aligned across code, docs, and internal/workspace/template skills.
- When changing session layout, update:
  - `internal/model`
  - `internal/workspace`
  - `internal/claude`
  - cleanup/tests/docs
- When changing heartbeat behavior, update:
  - `internal/heartbeat`
  - `config.yaml.template`
  - `internal/workspace/prompts/zh/HEARTBEAT.md` and `internal/workspace/prompts/en/HEARTBEAT.md`
  - any task docs that mention heartbeat
- When changing local web search behavior, update:
  - `internal/websearch`
  - `internal/workspace`
  - `config.yaml.template`
  - related docs and workspace skills
- New reminder/schedule creation must not depend on writing YAML files from agent prompts.

## Verification

Before finishing substantial changes, run:

```bash
go build ./...
go test ./...
go vet ./...
```

## Common Commands

```bash
# Build
go build ./...

# Run (需先配置 config.yaml)
go run ./cmd/server

# Test
go test ./...

# Test with coverage
go test ./... -cover

# Vet
go vet ./...

# Tidy dependencies
go mod tidy
```
