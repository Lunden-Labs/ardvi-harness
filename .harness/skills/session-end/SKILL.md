---
name: session-end
description: Close a work session cleanly by recording durable facts, sending handoff context, releasing claims, and ending the MCP session.
---

# End session

Use when the user closes, resets, or hands off a session.

1. Verify the actual repository and test state.
2. Delegate the closing edits to a cheap-model subagent: update the project's state documents (state file, tasks/NEXT.md, roadmap/history, todo) from the verified repository state; commit and push; confirm HEAD == origin.
3. Print the project's full task table when the project provides one (e.g. `python3 tasks/roadmap-table.py`), verbatim, in the final message — the owner reads the whole list at the end, not a summary.
4. Put only durable, non-secret facts in project memory. Store decisions in tracked ADRs/specs when they belong there.
5. Save a concise project memory item tagged `handoff` for the next session of this stable agent; the handoff names the pushed hash, what was closed, what is open and what the next session starts from. Send a handoff to a stable Agent/Project destination only when communication is within the user's authorized task; preserve the thread and original assignment reference.
6. Update `tasks/NEXT.md` only when the task state changed and the file is already used by the project.
7. Call `session_end`; this releases the ephemeral session's claims and request ownership. The SessionEnd hook also calls it on exit; repeated end is safe. Stable Agent identity, pending inbox and memory survive. Context compaction alone is not the end of the agent: repeat bootstrap when context returns.

Never claim work is complete unless its required checks passed. Never store credentials, tokens, private keys, or copied environment contents.
