---
name: lets-go
description: Start or resume project work by loading the minimum shared context, inbox, memory, and current task state.
---

# Let's go

Use this at the start of a work session or when the user says to continue.

1. Read `AGENTS.md`, then the nearest relevant specs, ADRs, and `tasks/NEXT.md` if present.
2. Register this native client with `session_start`; keep the returned session ID for this session.
3. Read the project inbox and search project memory only for the current task.
4. Inspect repository state before changing files. Do not infer completion from stale memory.
5. State the task you are continuing in one short sentence, then work.

The repository is authoritative. MCP memory is supporting context, not a replacement for tracked specs, ADRs, or task files.
