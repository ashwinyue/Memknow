# Wiki Skill — Knowledge Compounding

This skill defines how to turn reusable chat outcomes into persistent knowledge and keep index/log up to date.

## Goal

- Prevent high-value conclusions from being lost in chat history
- Keep knowledge searchable, reviewable, and incrementally improvable
- Make every write auditable through index and log updates

## Data Layout

```
$WORKSPACE_DIR/memory/
├── index.md               # Content index (topic/page oriented)
├── log.md                 # Chronological change log (append-only)
└── wiki/
    └── YYYY-MM-DD-*.md    # Individual knowledge pages
```

## Hard Prohibitions

- Do not scan the whole wiki before reading `memory/index.md`.
- Do not fill gaps from memory when evidence is weak.
- Do not update `memory/wiki/*.md` without also syncing `memory/index.md` and `memory/log.md`.
- Do not repeatedly create new pages for the same topic when an existing page should be extended.
- Do not write outside the current workspace.

## Failure Signals

If any of the following happens, the workflow has drifted and should be corrected immediately:

- The answer does not cite concrete page paths.
- The read scope is too broad and did not start with `memory/index.md`.
- A page was updated but index or log was not synced.
- The answer sounds too certain for the available evidence.
- Multiple highly overlapping pages exist for the same topic without a merge decision.

## Fixed Retrieval Order

1. Read `memory/index.md` first to locate candidate pages.
2. Read only high-relevance pages in full instead of broad sweeps.
3. If page evidence is insufficient, state uncertainty instead of over-claiming.
4. Stop expanding reads once additional pages no longer improve answer quality.

## Page Reuse Rules

- Prefer updating an existing topic page over creating a duplicate.
- Create a new page only when the material is distinct enough to stand on its own.
- Avoid repeated near-duplicate pages created under different dates.
- Consolidate stable methods, standard decisions, and long-lived comparisons into one canonical page.

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

## Workflow

### `ingest`

1. Extract reusable conclusions from the current conversation or provided material.
2. Decide whether to update an existing page or create a new one.
3. Write to `memory/wiki/YYYY-MM-DD-<slug>.md`.
4. Update `memory/index.md` with link + one-line summary, refreshing the summary if the page meaning changed.
5. Append a matching entry to `memory/log.md` with time, action, page, and reason.

### `query`

1. Follow the fixed retrieval order.
2. Synthesize the answer from the matching pages.
3. Cite the page paths used in the answer.
4. Separate known facts from inference.
5. Mark uncertainty when evidence is incomplete.
6. Persist the answer back into `memory/wiki/` only when the user explicitly asks for it.

### `lint`

Run periodic health checks and report:

- orphan pages in `memory/wiki/` that are not represented in `memory/index.md`
- duplicate or overlapping topic pages that should likely be merged
- pages updated without matching index or log synchronization
- pages that may be stale because they have not been refreshed for a long time
- pages whose conclusions are stronger than their evidence sections support

### `update`

1. Update the target page content.
2. Refresh the `memory/index.md` summary when needed.
3. Append to `memory/log.md` with the reason and scope of the change.

## Write Contract

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

## Output Contract

- State which pages were used for this answer.
- Distinguish known facts from inference.
- Mark uncertainty explicitly when evidence is incomplete.

## Constraints

- Preserve user intent; do not alter the decision direction without confirmation
- Prefer updating existing wiki pages over creating duplicates
- Rely only on general abilities: file reads, search, edits, and log appends
- Keep all paths inside the current workspace
