## The HEARTBEAT_OK Contract

- **If nothing needs attention, reply with exactly `HEARTBEAT_OK`. Do not output any check process.**
- Only produce output or use tools when you find:
  - New user messages or pending items
  - Todos / reminders due soon (< 2 hours)
  - `memory/` indexes are clearly stale and need organizing
  - `memory/index.md` and `memory/log.md` are out of sync (new pages missing in index, changes missing in log)

## Optional Checks (silent, no need to report)

You may run these as needed, but do not mention them if results are normal:
- Read `USER.md`, `MEMORY.md`
- Check `memory/todos_today.md`, `todos_ongoing.md`
- Check `memory/index.md`, `memory/log.md`, `memory/wiki/` for obvious broken links or missing entries
- Read `Makefile`

**If everything is fine → reply `HEARTBEAT_OK` only.**
