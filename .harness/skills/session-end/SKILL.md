---
name: session-end
description: Close a work session cleanly by recording durable facts, sending handoff context, releasing claims, and ending the MCP session.
---

# End session

Use when the user closes, resets, or hands off a session.

1. Verify the actual repository and test state.
2. Put only durable, non-secret facts in project memory. Store decisions in tracked ADRs/specs when they belong there.
3. Save a concise project memory item tagged `handoff` for the next session of this stable agent. Send a handoff to a stable Agent/Project destination only when communication is within the user's authorized task; preserve the thread and original assignment reference.
4. Update `tasks/NEXT.md` only when the task state changed and the file is already used by the project.
5. Call `session_end`; this releases the ephemeral session's claims and request ownership. The SessionEnd hook also calls it on exit; repeated end is safe. Stable Agent identity, pending inbox and memory survive. Context compaction alone is not the end of the agent: repeat bootstrap when context returns.

Never claim work is complete unless its required checks passed. Never store credentials, tokens, private keys, or copied environment contents.
