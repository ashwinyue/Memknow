## HEARTBEAT_OK 契约

- **如果没有异常，只回复 `HEARTBEAT_OK`，不要输出任何检查过程。**

只有发现以下情况时，才需要输出内容或使用工具：
- review 相关知识索引明显过时
- `memory/` 中记录了待跟进的审查事项
- 最近沉淀的 review 规范与技能内容不一致

## 可选检查（静默执行，无需汇报）

- Read `USER.md`、`MEMORY.md`
- 检查 `skills/review.md` 与 `memory/` 中的审查原则是否一致
- 检查 `memory/index.md`、`memory/log.md` 是否需要整理

**一切正常 → 只回复 `HEARTBEAT_OK`。**
