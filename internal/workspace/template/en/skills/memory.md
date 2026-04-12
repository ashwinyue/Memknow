# Memory

Long-term memory lives in the workspace file system.

## What goes where

- Short facts, preferences, habits → `USER.md`
- Your own notes, summaries, follow-ups, long-form links → `MEMORY.md`
- Long-form content (cases, logs, reports) → `memory/*.md`, with links in `MEMORY.md` under "Long-form Archive"
- Knowledge navigation index → `memory/index.md`
- Knowledge timeline log → `memory/log.md`

## Rules

- **Read `USER.md` and `MEMORY.md` at the start of every conversation.**
- **Write updates immediately** when the user shares a preference, corrects you, or asks you to remember something. Don't just say "OK" — actually edit the file.
- **Before ending every conversation, run the memory audit checklist:** ① Any case to record? (update `memory/cases/` and `cases/index.md`) ② Any insight worth capturing? (update `memory/insights/` and `insights/index.md`) ③ Is `memory/index.md` synced with the latest entries? ④ Is `memory/log.md` appended with this session's changes? Do not conclude the conversation until complete.
- Use `flock` if writing multiple files in one batch.
- **Never use Claude Code's built-in `/memory` command.** All memory must live in the current workspace.
- **Memory files must stay inside the current workspace.** Use `USER.md` and `MEMORY.md` as root files; use `memory/*.md` for long-form or categorized records. Never write to `~/.claude/projects/` or any external path.

## Related Skills

- Task management → `skills/todo.md`
- Case recording → `skills/cases.md`
- Reflection recording → `skills/insights.md`
- Knowledge compounding → `skills/wiki.md`
