# Cases System Specification

This skill defines how to record and retrieve important concrete events (cases),沉淀经验 through daily conversations.

---

## When to Trigger

### Auto-Record

Proactively ask "Would you like me to record this case?" when the user describes:
- A specific time + scenario + process or outcome
- An event worth analyzing or reviewing (not casual chat)
- Or when the user explicitly says "record this" or "write this down"

### Auto-Retrieve

Search `memory/cases/` when you see signals like:
- "last time", "before", "remember", "anything similar"
- The current situation is highly similar to a historical case
- Historical experience can help answer the user's question

---

## Directory Structure

```
memory/cases/
├── index.md                            # Category index (update after each new case)
└── YYYY-MM-DD-{slug}.md               # Individual case file
```

Paths are relative to the workspace root. Create `memory/cases/` on demand.

---

## Case File Template

File name: `YYYY-MM-DD-{2-4 lowercase keywords hyphenated}.md`

```markdown
---
date: YYYY-MM-DD
category: [category, see index]
keywords: [keyword1, keyword2, keyword3]
status: open | resolved
---

# [Concise Title]

## Objective Facts
[Restore facts: time / place / trigger / process / reactions. No subjective judgment.]

## Analysis
[Behavior interpretation, causal mechanisms, connections to relevant background knowledge.]

## Response Strategy
[Strategies discussed or implemented this time. Can include multiple options.]

## Tracking Log

| Date | Update |
|------|--------|
| YYYY-MM-DD | Initial record |
```

---

## Index File Template

File: `memory/cases/index.md`

```markdown
# Cases Index

> Last updated: YYYY-MM-DD

## [Category 1]
- [YYYY-MM-DD Title](file-name.md) — One-sentence summary

## [Category 2]
- [YYYY-MM-DD Title](file-name.md) — One-sentence summary
```

---

## Operational Rules

1. After writing a case file, **always update** `memory/cases/index.md`
2. Use `flock -x .memory.lock` when writing multiple files
3. Create `memory/cases/` on demand with `mkdir -p`
