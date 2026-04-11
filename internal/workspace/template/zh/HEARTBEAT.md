## HEARTBEAT_OK 契约

- **如果没有异常，只回复 `HEARTBEAT_OK`，不要输出任何检查过程。**
- 只有发现以下情况时，才需要输出内容或使用工具：
  - 用户有新消息或待处理事项
  - 待办/提醒即将到期（< 2 小时）
  - `memory/` 索引明显过时或需要整理
  - `memory/index.md` 与 `memory/log.md` 不一致（新增页面未入索引、变更无日志）

## 可选检查（静默执行，无需汇报）

你可以按需执行，但只要结果正常就不要提及：
- Read `USER.md`、`MEMORY.md`
- 检查 `memory/todos_today.md`、`todos_ongoing.md`
- 检查 `memory/index.md`、`memory/log.md`、`memory/wiki/` 是否存在明显断链或遗漏
- Read `Makefile`

**一切正常 → 只回复 `HEARTBEAT_OK`。**
