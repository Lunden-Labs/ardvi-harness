---
name: session-end
description: Close a work session cleanly by recording durable facts, sending handoff context, releasing claims, and ending the MCP session.
---

# End session

Use when the user closes, resets, or hands off a session.

1. Verify the actual repository and test state.
2. Put only durable, non-secret facts in project memory. Store decisions in tracked ADRs/specs when they belong there.
3. Send a concise handoff message when another active agent needs it.
4. Update `tasks/NEXT.md` only when the task state changed and the file is already used by the project.
5. Call `session_end`; this releases the session's claims.

Never claim work is complete unless its required checks passed. Never store credentials, tokens, private keys, or copied environment contents.
