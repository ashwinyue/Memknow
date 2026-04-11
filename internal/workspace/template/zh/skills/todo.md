# Todo Skill — 待办管理

本 skill 规范待办事项的**提醒、添加、标记完成、每日日志、历史回顾**五项功能。

---

## 数据文件结构

```
$WORKSPACE_DIR/memory/
├── todos_today.md              # 今日待办（每天覆盖/清空）
├── todos_ongoing.md            # 持续跟进（长期任务，不自动清空）
└── daily_logs/
    ├── YYYY-MM-DD.md           # 每日工作日志（收工时生成）
    └── todos_YYYY-MM-DD.md     # 未来某日待办（非今天的日期安排）
```

> `daily_logs/` 中存在两种文件：
> - `YYYY-MM-DD.md`：当日收工时生成的工作日志
> - `todos_YYYY-MM-DD.md`：提前记录的未来某日待办

执行读写时必须使用绝对路径（`$WORKSPACE_DIR/...`）。写多个文件时用 `$WORKSPACE_DIR/.memory.lock` 加锁。

---

## 触发识别

| 用户说 | 触发动作 |
|--------|---------|
| 「今天要做什么」「我的待办」「提醒我」「今日任务」 | → **提醒** |
| 「加一条」「记下来」「添加」「新增待办」 | → **添加** |
| 「完成了」「做完了」「标记完成」「✓ XXX」 | → **标记完成** |
| 「收工」「下班了」「今天做了什么」「生成日志」 | → **记录日志** |
| 「上周」「昨天」「回顾」「X月X日做了什么」 | → **回顾历史** |

---

## 功能一：提醒（Reminder）

读取当天待办和持续跟进，格式化输出。**调用前先检查跨日**，若日期不符则归档昨天并初始化今天。

### 跨日检查

`$WORKSPACE_DIR/memory/todos_today.md` 第一行格式应为：`# 今日待办 — YYYY-MM-DD`

若首行日期与当前日期不一致：
1. 若文件中有未完成的条目，可选择性地提醒用户是否需要移到持续跟进
2. 写入新的日期头部覆盖 `$WORKSPACE_DIR/memory/todos_today.md`
3. **不要自动清空**已完成的条目；次日用户说「清空今日待办」或「新的一天」时再清空

### 输出格式

```
📋 **今日待办**（YYYY-MM-DD）

**今天**
- [ ] 任务 A
- [x] 任务 B（已完成）

**持续跟进**
- [ ] 长期事项 C（持续跟进中）

共 N 项未完成。
```

---

## 功能二：添加（Add）

向今日待办、未来某日待办或持续跟进中追加新条目。

### 判断规则

| 用户描述 | 写入目标 |
|---------|---------|
| 含「今天」或无日期 | `$WORKSPACE_DIR/memory/todos_today.md` |
| 含具体日期（「下周二」「3月17日」「明天」等） | `$WORKSPACE_DIR/memory/daily_logs/todos_YYYY-MM-DD.md` |
| 含「持续跟进」「长期」「一段时间」 | `$WORKSPACE_DIR/memory/todos_ongoing.md` |
| 无法判断 | 询问「是今天的任务、某天的安排，还是需要持续跟进的事项？」 |

### 写入格式

```
- [ ] <任务描述>（录入于 YYYY-MM-DD）
```

### 今日待办写入示例

```bash
flock -x \"$WORKSPACE_DIR/.memory.lock\" -c "
  mkdir -p \"$WORKSPACE_DIR/memory/daily_logs\"
  if [ ! -f \"$WORKSPACE_DIR/memory/todos_today.md\" ] || ! head -1 \"$WORKSPACE_DIR/memory/todos_today.md\" | grep -q '[0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}'; then
    echo '# 今日待办 — $(date +%Y-%m-%d)' > \"$WORKSPACE_DIR/memory/todos_today.md\"
  fi
  echo '- [ ] <任务描述>（录入于 $(date +%Y-%m-%d)）' >> \"$WORKSPACE_DIR/memory/todos_today.md\"
"
```

### 每日提醒时合并当天待办

若 `$WORKSPACE_DIR/memory/daily_logs/todos_YYYY-MM-DD.md`（今天日期）存在，将其内容合并展示到今日待办中，提示用户可归入 `$WORKSPACE_DIR/memory/todos_today.md`。

---

## 功能三：标记完成 / 取消完成（Toggle）

将指定任务的 `[ ]` 改为 `[x]`，或反向操作。

- 「完成了 XXX」「XXX 做完了」→ 标记 `[x]`
- 「XXX 没做完」「取消完成」→ 恢复 `[ ]`
- 若描述模糊，列出当前未完成项让用户选择

在 `$WORKSPACE_DIR/memory/todos_today.md` 和 `$WORKSPACE_DIR/memory/todos_ongoing.md` 中同时查找匹配项。完成后回复确认。

---

## 功能四：记录每日日志（Daily Log）

在用户「收工」或主动要求时，将当天工作汇总写入日志文件。

### 日志文件路径

`$WORKSPACE_DIR/memory/daily_logs/YYYY-MM-DD.md`

### 日志模板

```markdown
# 工作日志 — YYYY-MM-DD（周X）

## 今日待办完成情况

| 状态 | 事项 |
|------|------|
| ✅ 完成 | 事项 A |
| ❌ 未完成 | 事项 B |

**完成率**：N/M（N%）

## 持续跟进状态

- [ ] 长期事项 C（进行中）

## 今日小结

> [3-5 句话，由 AI 根据完成情况生成，或由用户补充]

---
_记录时间：HH:MM_
```

### 收工后处理

1. 将 `$WORKSPACE_DIR/memory/todos_today.md` 的已完成条目保留，供第二天对比参考
2. **不自动清空** `$WORKSPACE_DIR/memory/todos_today.md`

---

## 功能五：历史回顾（Review）

查询并展示过去某天或某段时间的工作日志。

| 用户说 | 解析 |
|--------|------|
| 「昨天做了什么」 | 读取 `$WORKSPACE_DIR/memory/daily_logs/YYYY-MM-DD.md`（昨天日期） |
| 「上周做了什么」 | 读取上周各工作日日志，汇总 |
| 「3月10日做了什么」 | 读取 `$WORKSPACE_DIR/memory/daily_logs/2026-03-10.md` |
| 「最近一周工作回顾」 | 读取最近 7 天日志，生成总结 |

---

## 注意事项

- 所有文件读写使用 `flock -x "$WORKSPACE_DIR/.memory.lock"` 加锁
- `$WORKSPACE_DIR/memory/daily_logs/` 目录按需创建（`mkdir -p "$WORKSPACE_DIR/memory/daily_logs"`）
- 持续跟进的条目不受「清空今日待办」影响
