# Feishu

This project uses `lark-cli` for Feishu operations.

## Routing

Internal channel key format (used for session routing):
- `p2p:{chat_id}:{app_id}`
- `group:{chat_id}:{app_id}`
- `thread:{chat_id}:{thread_id}:{app_id}`

Routing key for `lark-cli` (remove `:{app_id}` suffix):
- `p2p:{chat_id}`
- `group:{chat_id}`

## Common commands

```bash
# Send text
lark-cli im +messages-send --as bot --chat-id "<chat_id>" --text "msg"

# Send markdown
lark-cli im +messages-send --as bot --chat-id "<chat_id>" --markdown $'## title\nbody'

# Fetch doc
lark-cli docs +fetch --url "<doc-url>"

# Read sheet
lark-cli sheets +read --url "<sheet-url>" --range "A1:D10"
```

Prefer `--as bot`. Use `--as user` only when accessing private user resources.
If you already sent a message via `lark-cli`, keep the final reply brief.
