---
name: project-context
description: Find project-specific context across tracked instructions, MCP memory, agent messages, skills, and optional personas without loading whole catalogs.
---

# Project context

The `unread` field on hook output and on tool results such as `message_send`, `message_ack`, and `claim_*` is the normal way messages arrive; `inbox_read` is for catching up, not the primary channel.

Read tracked repository instructions first. Use MCP search calls for only the topic needed:

- `memory_search` for durable observations and prior handoffs;
- `inbox_read` and `thread_read` for agent communication;
- `skills_search` then `skill_read` for reusable procedures;
- `skills_list` when the user asks for the complete installed catalog;
- `personas_search` then `persona_read` for an optional perspective.

Do not assume a fixed architect, reviewer, provider, or starting agent. A persona is guidance, not authority. Resolve conflicts in favor of current repository state and explicit user instructions.
