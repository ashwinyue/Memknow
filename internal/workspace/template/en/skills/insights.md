# Insights Skill — Reflection Recording

This skill defines how to quickly record, categorize, and review fragmented reflections.

Ideal for capturing fleeting thoughts: a one-sentence realization, post-mortem reflection, new understanding of a problem, or an experience worth preserving.

---

## Data File Structure

```
memory/
└── insights/
    ├── index.md                  # Category index (update after each addition)
    └── YYYY-MM.md                # Monthly archive; all insights for the same month go here
```

Paths are relative to the workspace root.

---

## Reflection Categories

| Category | Suitable Content |
|----------|------------------|
| Work | Professional skills, business insights, workplace experience |
| Learning | Knowledge accumulation, cognitive upgrades, methodologies |
| Growth | Personal reflection, habits, mindset, values |
| Life | Daily feelings, relationships, hobbies |
| General | Miscellaneous thoughts that don't fit elsewhere |

Category is auto-assigned by AI; the user can correct it.

---

## Trigger Recognition

| User says | Action |
|-----------|--------|
| "record this", "I have a thought", "realization", "reflection" | → **Quick record** |
| "show my insights", "review", "what did I write before" | → **Review** |
| "any insights about XX", "search" | → **Search** |

---

## Feature 1: Quick Record

1. User states the reflection
2. AI distills a short label (2–4 characters/words as the heading)
3. Auto-assign a category (default to "General" if uncertain)
4. Write to the monthly file and update the index

### Entry Format

```markdown
### [Short Label]

> **[Category]** · YYYY-MM-DD

[Reflection text, preserving the user's original expression. AI may polish slightly without changing meaning.]

---
```

### Write Flow

```bash
flock -x .memory.lock -c "
  mkdir -p memory/insights
  MONTH_FILE=\"memory/insights/$(date +%Y-%m).md\"
  if [ ! -f ${MONTH_FILE} ]; then
    printf '# Insights — %s-%s\n\n' \"$(date +%Y)\" \"$(date +%m)\" > ${MONTH_FILE}
  fi
  cat >> ${MONTH_FILE} << 'EOF'

### [Short Label]

> **[Category]** · YYYY-MM-DD

[Reflection text]

---
EOF
"
```

---

## Feature 2: Update Index

After each new reflection, update `memory/insights/index.md`:

```markdown
# Insights Index

> Last updated: YYYY-MM-DD

## Work
- [YYYY-MM Label](YYYY-MM.md#short-label) — One-sentence summary

## Learning
- [YYYY-MM Label](YYYY-MM.md#short-label) — One-sentence summary
```

---

## Notes

- Always use `flock -x .memory.lock` for file writes
- Create `memory/insights/` on demand with `mkdir -p`
- Prefer appending to insight files; avoid modifying historical content
