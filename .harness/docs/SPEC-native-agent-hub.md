# Native agent hub

## Objective

Let Codex and Claude run through their normal applications while sharing a
small, project-aware MCP service for sessions, messages, work claims, durable
memory, and lazily loaded skills. Remove CAO, tmux, provider launch wrappers,
fixed roles, and generated CAO profiles from the harness.

## Supported workflow

```bash
make init
make up
codex   # or: claude
```

`make init PROMPT='...'` may seed `tasks/NEXT.md` in a new project. It never
starts an agent. `make up` ensures the machine-wide Docker service is running;
`make down` stops it for every project. Neither manages Codex or Claude.

## Project integration

- `.ardvi/project.json` contains a stable project UUID and display name. It is
  created once and is project-owned afterwards.
- `.codex/config.toml` contains one checksum-protected `ardvi` MCP block.
- `.mcp.json` contains one managed `mcpServers.ardvi` entry.
- `AGENTS.md` is the common policy source. `CLAUDE.md` imports `@AGENTS.md`.
- Existing project files and non-Ardvi MCP entries are preserved.

## MCP contract

The server uses Streamable HTTP. The project UUID is bound by the
`X-Ardvi-Project` header configured in the repository, not repeated in tool
arguments. Project scope is the default; cross-project operations require an
explicit global scope.

Tools:

- sessions: `session_start`, `session_end`, `agents_list`;
- messages: `message_send`, `inbox_read`, `message_ack`, `thread_read`;
- claims: `claims_list`, `claim_acquire`, `claim_release`;
- memory: `memory_put`, `memory_search`, `memory_archive`;
- catalog: `skills_list`, `skills_search`, `skill_read`, `personas_search`,
  `persona_read`.

All list/search calls are bounded. Claims are atomic within the one server
process and expire by TTL. Session end releases that session's claims. Durable
tasks and status remain in repository files rather than a second task database.

## Persistence

The hub stores one bounded JSON state snapshot in Docker volume `ardvi-data`.
Each write is serialized, synced to a temporary file, and atomically renamed.
Malformed or oversized state fails startup visibly. An exclusive process lock
prevents two writers.

Project memory export contains only project-scoped entries marked durable.
Global memory is never exported into a project repository.

## Skills

Harness-owned skills are `communication`, `writing`, `lets-go`, `session-end`,
`project-context`, and `skills-list`. `upstreams.tsv` records update branches;
`upstreams.lock.tsv` pins release commits. Complete managed trees are built
into the image and `make update` activates a new digest only after health
validation. The server returns metadata first and reads supporting files only
on request. Arbitrary filesystem reads and path traversal are rejected.

Codex and Claude receive the six small native entry skills; the complete
catalog stays lazy on MCP.
Agency Agents content is exposed as personas, without assigning a default role.

## Security boundaries

- Compose publishes the container only on host loopback. A non-loopback server
  listener requires the explicit container environment and flag.
- Non-local `Host` and `Origin` values are rejected.
- Request bodies, identifiers, limits, paths, and stored record sizes are
  validated at the boundary.
- Skills are readable only through roots declared in the generated catalog.
- No secrets are written to repository files or logs.

## Success criteria

1. Empty and existing repositories initialize idempotently without overwrites.
2. Codex and Claude project MCP configurations point at the same local hub.
3. Two project IDs are isolated; explicit global messages cross projects.
4. Concurrent claims have exactly one owner and stale claims expire.
5. Messages, sessions, claims, and memory survive a server restart.
6. Project memory export/import round-trips without global data.
7. Every managed skill and required relative dependency can be loaded.
8. A failed image/catalog update leaves the previous configuration active and
   exits nonzero.
9. No CAO, tmux, provider launcher, or fixed-role runtime remains.
10. Shell checks, Go tests including race tests, and harness integration tests pass.

## Explicit non-goals

- Starting, stopping, resuming, compacting, or displaying provider sessions.
- A dashboard, terminal multiplexer, scheduler, or autonomous wake-up channel.
- Replacing Codex or Claude native subagents.
- A distributed database or live multi-machine replication.
