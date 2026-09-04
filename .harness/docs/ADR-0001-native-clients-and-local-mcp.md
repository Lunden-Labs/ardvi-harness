# ADR-0001: Native clients with a local Ardvi MCP hub

- **Status:** Accepted by user direction
- **Date:** 2026-09-04

## Context

The CAO-based harness added a web UI, tmux behavior, session lifecycle races,
fixed roles, and provider wrappers. The required outcome is much smaller: keep
the continuously updated Codex and Claude interfaces and give their agents a
shared communication, memory, work, and skills channel.

MCP Agent Mail covers part of this surface, but its current license contains an
OpenAI/Anthropic restriction and its runtime brings substantially more machinery
than this harness needs.

## Decision

Build one local Go service using the official MCP Go SDK and Streamable HTTP.
Bind each connection to a committed project UUID through project-scoped Codex
and Claude configuration. Store low-volume state in an atomic bounded JSON snapshot. Keep
provider session lifecycle entirely provider-owned.

## Consequences

- Codex and Claude can be launched normally with no wrapper or tmux.
- One daemon serves several isolated repositories.
- The implementation owns a small MCP contract and persistence format.
- Agents poll inboxes at workflow boundaries; unsolicited wake-up is deferred.
- Remote access is deferred; v1 rejects non-loopback listeners.
