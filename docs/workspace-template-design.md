# Workspace 模板设计文档

> 状态: 已实现
> 最后更新: 2026-04-10

## 一、设计原则

1. 模板内嵌到二进制  
   默认模板位于 `internal/workspace/template/`，通过 `go:embed` 打包进二进制，首次初始化时自动写入目标 workspace。

2. Prompt 动态注入，不生成 session 级 `CLAUDE.md`  
   session 启动时由框架通过 `--append-system-prompt` 动态拼接基础 prompt 与 workspace prompt，避免“应该改 CLAUDE 还是改 SOUL”的歧义。

3. 文件职责分层明确  
   框架 prompt 负责平台规则，workspace 文件负责 bot 身份、用户画像与长期记忆。

4. Skills 按需加载  
   框架只注入紧凑索引，Claude 需要时再 `Read skills/<name>.md`。

---

## 二、模板结构

```text
internal/workspace/template/
├── zh/
│   ├── SOUL.md
│   ├── IDENTITY.md
│   ├── USER.md
│   ├── MEMORY.md
│   ├── HEARTBEAT.md
│   └── skills/
└── en/
    └── ...
```

运行时 workspace：

```text
<workspace_dir>/
├── bin/
│   └── web-search
├── SOUL.md
├── IDENTITY.md
├── USER.md
├── MEMORY.md
├── HEARTBEAT.md
├── .search.json
├── skills/
├── memory/
├── sessions/
├── .memory.lock
└── .skill.lock
```

---

## 三、核心文件职责

| 文件 | 职责 |
|---|---|
| `SOUL.md` | bot 的人格、行为原则、业务边界 |
| `IDENTITY.md` | bot 的名字、气质、自我定义 |
| `USER.md` | 用户偏好与关系上下文 |
| `MEMORY.md` | 当前最重要的长期记忆入口 |
| `HEARTBEAT.md` | heartbeat 自检 checklist |
| `bin/web-search` | workspace 内统一联网搜索入口 |
| `.search.json` | 由全局 `config.yaml` 派生的搜索运行时配置 |
| `skills/*.md` | 场景特定操作规范 |

框架级 prompt 位于：

- `internal/claude/prompts/chat.md`
- `internal/claude/prompts/heartbeat.md`
- `internal/claude/prompts/schedule.md`

它们不属于 workspace 模板，但与模板文件共同组成最终 system prompt。

---

## 四、注入机制

### 4.1 SESSION_CONTEXT.md

每次执行前，框架在 session 目录下写入 `SESSION_CONTEXT.md`，供 Claude 获取：

- workspace 绝对路径
- session 绝对路径
- attachments 绝对路径
- DB 路径
- channel key

### 4.2 最终 prompt 组成

执行时的 system prompt 由两层组成：

1. 基础 prompt  
   根据 session 类型选择 `chat.md` / `heartbeat.md` / `schedule.md`
2. Workspace prompt  
   拼接 `SOUL.md`、`IDENTITY.md` 和 skills 索引

`USER.md` 与 `MEMORY.md` 不直接常驻在 system prompt 中，而是由 Claude 按要求读取，或由框架在检索时注入相关片段。

---

## 五、扩展方式

### 5.1 新增 skill

直接在 workspace 下新增 `skills/<name>.md` 即可，下次 session 启动会自动出现在 skills 索引中。

### 5.2 调整 bot 个性

优先改 `SOUL.md` 与 `IDENTITY.md`，不需要改框架 prompt。

### 5.3 调整平台规则

应修改 `internal/claude/prompts/*.md`，而不是把平台级安全规则复制到每个 workspace 的 `SOUL.md` 中。

### 5.4 调整本地搜索能力

- 搜索实现位于 `internal/websearch/`
- workspace 初始化时会生成 `.search.json` 与 `bin/web-search`
- 模板中的 `skills/search.md` 和 `SOUL.md` 负责告诉 bot 优先使用本地搜索入口

---

## 六、实施建议

1. bot 个性和业务边界写在 workspace 文件里，不要和框架规则混写。
2. skill 尽量保持自包含，方便按需读取。
3. memory 文件按主题拆分，便于检索精准命中。
4. 修改模板时要同步关注 `internal/workspace/init.go` 与主设计文档。
