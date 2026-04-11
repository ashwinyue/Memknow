# Heartbeat Skill

## Purpose

Heartbeat is a periodic system-triggered self-check. Users can customize what gets checked on each heartbeat by editing `HEARTBEAT.md`.

## Core File

- `HEARTBEAT.md` — the heartbeat checklist, located in the workspace root

## When to Use

Edit `HEARTBEAT.md` when the user says things like:
- "set heartbeat to remind me to sleep"
- "every heartbeat check my todos"
- "remind me to drink water during heartbeat"
- "let heartbeat check my repo status"

## How To

1. `Read` `HEARTBEAT.md`
2. Append the user's custom item to the checklist
3. `Write` it back to `HEARTBEAT.md`
4. Briefly confirm the update to the user

## Notes

- Do not create `tasks/*.yaml` or a schedule for "every heartbeat" — heartbeat already supports this
- Keep the checklist concise, one check per line
- Never modify global templates under `internal/workspace/template/`
