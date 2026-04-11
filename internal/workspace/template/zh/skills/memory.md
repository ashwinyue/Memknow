# 记忆

长期记忆存在于 workspace 文件系统中。
运行时当前目录通常在 `sessions/...`，不是 workspace 根目录。

## 路径规则（必须遵守）

- 所有 `Read` / `Edit` / `Write` / `exec` 涉及记忆文件时，使用绝对路径。
- 优先使用 `$WORKSPACE_DIR/...`（或 `SESSION_CONTEXT.md` 中给出的绝对路径），不要使用 `workspaces/...` 这类猜测路径。
- 禁止把记忆写到 session 目录里。

## 写什么放哪

- 短事实、偏好、习惯 → `$WORKSPACE_DIR/USER.md`
- 你自己的笔记、摘要、待跟进、长文入口 → `$WORKSPACE_DIR/MEMORY.md`
- 长文内容（案例、日志、报告）→ `$WORKSPACE_DIR/memory/*.md`，并在 `$WORKSPACE_DIR/MEMORY.md` 的「长文存档」里留链接
- 知识沉淀总导航 → `$WORKSPACE_DIR/memory/index.md`
- 沉淀变更时间线 → `$WORKSPACE_DIR/memory/log.md`

## 规则

- **每次对话开始时读取 `$WORKSPACE_DIR/USER.md` 和 `$WORKSPACE_DIR/MEMORY.md`。**
- **用户分享偏好、纠正你、或让你记东西时，立即写入。** 不要只说"好的"，要真的去改文件。
- 批量写多个文件时用 `flock -x "$WORKSPACE_DIR/.memory.lock"`。
- **严禁使用 Claude Code 内置的 `/memory` 命令。** 所有记忆必须写在当前 workspace 里。
- **记忆文件必须写在当前 workspace 内。** 根文件用 `$WORKSPACE_DIR/USER.md`、`$WORKSPACE_DIR/MEMORY.md`；长文与分类记录用 `$WORKSPACE_DIR/memory/*.md`。严禁写入 `~/.claude/projects/` 或其他任何外部路径。

## 相关技能

- 待办管理 → `skills/todo.md`
- 事件记录 → `skills/cases.md`
- 感悟记录 → `skills/insights.md`
- 知识沉淀 → `skills/wiki.md`
