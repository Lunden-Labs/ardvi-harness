---
name: lets-go
description: Start or resume project work by loading the minimum shared context, inbox, memory, and current task state.
---

# Let's go

Use this at the start of a work session or when the user says to continue.

1. Read `AGENTS.md`, then the nearest relevant specs, ADRs, and `tasks/NEXT.md` if present.
2. The SessionStart hook has already registered this session and printed its `session_id` and name; reuse them. Call `session_start` yourself only if no such line is in context.
3. Read the project inbox and search project memory only for the current task.
4. Inspect repository state before changing files. Do not infer completion from stale memory.
5. State the task you are continuing in one short sentence, then work.
6. Claude Code only: arm a persistent `Monitor` running `ardvi inbox --session <id> --follow` so messages arrive during long turns. Codex has no equivalent; it relies on the prompt hook and the `unread` field returned by `message_send` / `message_ack` / `claim_*` calls.

The repository is authoritative. MCP memory is supporting context, not a replacement for tracked specs, ADRs, or task files.
