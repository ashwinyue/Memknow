# 需求文档

> 状态: 已实现
> 最后更新: 2026-04-10

## 一、项目定位

Memknow 是一个基于 Feishu 的长期记忆 AI Agent 平台。每个业务场景对应一个飞书应用和一个独立 workspace，系统负责把飞书消息路由到本地 Claude CLI 执行环境，并把结果回传给对应会话。

---

## 二、核心需求

### 1. 多应用隔离

- 每个飞书应用绑定一个独立 workspace
- 不同 app 的人格、技能、记忆、发送权限相互隔离
- 同一套服务进程可同时挂多个 app

### 2. 会话管理

需要支持三类 Feishu 场景：

| 场景 | channel_key | 说明 |
|---|---|---|
| 单聊 | `p2p:{chat_id}:{app_id}` | 支持 `/new` |
| 群聊 | `group:{chat_id}:{app_id}` | 支持 `/new` |
| 话题群 | `thread:{chat_id}:{thread_id}:{app_id}` | 不支持 `/new` |

约束：

- 同一 `channel_key` 串行处理
- 不同 `channel_key` 并发处理
- Worker 空闲超时后自动退出并归档 session

### 3. Claude 执行集成

- 框架通过子进程调用 `claude` CLI
- 通过 `--resume` 复用 Claude 原生会话上下文
- 使用 stream-json 解析系统事件、assistant 文本、工具调用与结果
- 通过 `--append-system-prompt` 动态注入框架 prompt 与 workspace prompt

### 4. Workspace 机制

每个 workspace 至少包含：

- `SOUL.md`
- `IDENTITY.md`
- `USER.md`
- `MEMORY.md`
- `HEARTBEAT.md`
- `skills/`
- `memory/`
- `sessions/`

要求：

- 首次启动可由嵌入模板自动初始化
- 用户可持续编辑 workspace 文件，作为 bot 的长期配置与记忆

### 5. 长期记忆与上下文增强

系统需要补齐 Claude `--resume` 无法覆盖的能力：

- 跨 session 摘要
- 历史消息检索
- 上下文溢出后的自动摘要重试

实现形态：

- `session_summaries` 表
- FTS5 消息检索
- Worker 执行前注入检索结果

### 6. 飞书消息能力

- WebSocket 长连接接收消息与事件
- 支持文本、图片、文件、post 富文本
- 支持发送文本、post、交互式卡片
- 支持欢迎事件直接回卡片

### 7. 群聊参与策略

群聊默认应谨慎发言：

- 显式提到其他 bot 时不响应
- 未直接提到当前 bot 的消息可走 probe 判断
- 默认倾向静默，不抢答

### 8. Schedule

- 用户可用自然语言创建、查看、修改、删除 schedule
- Schedule 存储在数据库，不依赖文件系统 YAML
- 默认发送目标跟随当前聊天上下文
- 服务启动时自动恢复 enabled schedule

### 9. Heartbeat

- Heartbeat 是框架内置维护机制，不依赖 schedule YAML
- 行为由 `config.yaml` 和 workspace 下 `HEARTBEAT.md` 共同定义
- 可选择把 heartbeat 结果发送到指定会话

### 10. 附件处理

- 下载附件到临时目录
- 转移到 session 附件目录
- 在 prompt 中注入绝对路径
- 对纯附件消息先缓存，等待用户补充说明

---

## 三、非功能需求

| 需求 | 方案 |
|---|---|
| 部署方式 | 单机、单进程、SQLite WAL |
| 数据持久化 | SQLite + GORM |
| 并发模型 | `channel_key` 串行，跨 channel 并发 |
| 网络接入 | 飞书 WebSocket，无需公网入口 |
| 安全 | 附件大小限制、HTTP timeout、最小环境变量注入 |
| 优雅关闭 | 等待 worker 退出，再关调度和 HTTP |
| 兼容性 | 优先支持 macOS / Linux，本地开发友好 |

---

## 四、实现映射

| 模块 | 主要实现 |
|---|---|
| 配置 | `internal/config` |
| 数据库 | `internal/db` |
| 数据模型 | `internal/model` |
| 飞书接入 | `internal/feishu` |
| 会话编排 | `internal/session` |
| Claude 集成 | `internal/claude` |
| Schedule | `internal/schedule` |
| Heartbeat | `internal/heartbeat` |
| Cleanup | `internal/cleanup` |
| Workspace 初始化 | `internal/workspace` |
