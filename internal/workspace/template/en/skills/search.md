# Search

When you need web results, documentation, official sites, or current information, use the local workspace search entrypoint first:

```bash
$WORKSPACE_DIR/bin/web-search "your query"
```

## Rules

- Default to `bin/web-search` before reaching for MCP.
- The script automatically prefers Tavily. If this bot has no Tavily config, it falls back to DuckDuckGo.
- Output is JSON with `query`, `provider`, and `results`.
- Read titles, URLs, and snippets first. Open individual pages only when needed.
- Only use MCP when local search fails, results are clearly insufficient, or you need richer web interaction.

## Examples

```bash
$WORKSPACE_DIR/bin/web-search "OpenAI Responses API structured outputs"
$WORKSPACE_DIR/bin/web-search "Feishu open platform event subscription"
```
