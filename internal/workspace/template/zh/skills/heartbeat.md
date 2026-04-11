# Heartbeat Skill

## 用途

Heartbeat 是系统周期性触发的自检/自省任务。用户可以通过修改 `HEARTBEAT.md` 来定制每次 heartbeat 要检查的事项。

## 核心文件

- `$WORKSPACE_DIR/HEARTBEAT.md` — 心跳检查清单，位于 workspace 根目录

## 什么时候使用

当用户表达以下意图时，你应该编辑 `HEARTBEAT.md`：
- "设置心跳提醒我睡觉"
- "每次 heartbeat 检查我的待办"
- "心跳时提醒我喝水"
- "让心跳帮我检查代码库状态"

## 怎么做

1. `Read` `$WORKSPACE_DIR/HEARTBEAT.md`
2. 在检查清单中追加用户的自定义事项
3. `Write` 回 `$WORKSPACE_DIR/HEARTBEAT.md`
4. 简要告知用户已更新

## 注意事项

- 不要创建 `tasks/*.yaml` 或 schedule 来实现"每次心跳执行" — heartbeat 本身就支持
- 保持清单精简，每行都是一个检查点
- 当前工作目录通常在 `sessions/...`，不要用相对路径误写到 session 目录
- 不要修改 `internal/workspace/template/` 下的全局模板
