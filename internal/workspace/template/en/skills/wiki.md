# Wiki Skill — Knowledge Compounding

This skill defines how to turn reusable chat outcomes into persistent knowledge and keep index/log up to date.

## Goal

- Prevent high-value conclusions from being lost in chat history
- Keep knowledge searchable, reviewable, and incrementally improvable

## Data Layout

```
$WORKSPACE_DIR/memory/
├── index.md               # Content index (topic/page oriented)
├── log.md                 # Chronological change log (append-only)
└── wiki/
    └── YYYY-MM-DD-*.md    # Individual knowledge pages
```

## Trigger Signals

- User says "record this", "capture this", "write this down"
- A reply contains stable methods, comparisons, or reusable checklists
- The same question appears repeatedly and deserves a canonical page

### Chat-to-Wiki Trigger Keywords

Prioritize wiki compounding when you see:

- Direct intent: `record this`, `capture this`, `turn this into docs`, `add to knowledge base`
- Retrospective intent: `summarize this`, `post-mortem`, `what should we do next time`
- Reuse intent: `standard approach`, `use this from now on`, `make a template`
- Comparison intent: `A vs B`, `pros and cons`, `decision criteria`

If the same turn also matches `todo/cases/insights`:

1. Explicit actionable tasks first → `todo`
2. Concrete event retrospectives first → `cases`
3. Personal reflections first → `insights`
4. Reusable methods/conclusions in parallel → `wiki`

## Write Flow

1. Create or update `memory/wiki/YYYY-MM-DD-<slug>.md`
2. Update `memory/index.md` with link + one-line summary
3. Append a new entry to `memory/log.md` with time/action/file/reason
4. Use `flock -x "$WORKSPACE_DIR/.memory.lock"` when writing multiple files

## Page Template

```markdown
# Title

## Context

## Conclusion

## Evidence

## Next Actions
```

## Constraints

- Preserve user intent; do not alter the decision direction without confirmation
- Prefer updating existing wiki pages over creating duplicates
- Keep all paths inside the current workspace
