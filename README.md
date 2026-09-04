# Ardvi project harness

Ardvi adds shared communication, memory, skills, and resource claims to normal
Codex and Claude projects. It does not replace either application and does not
start agents for you.

## Install in a project

The target must already be a Git repository. Clone this repository once, then
copy the harness:

```bash
git clone https://github.com/Lunden-Labs/ardvi-harness.git
cd ardvi-harness
make copy TARGET=/path/to/your-project
cd /path/to/your-project
make init
```

`make init` installs the local MCP server and all managed skills, creates a
stable project identity, and configures both clients. Existing `AGENTS.md`,
`CLAUDE.md`, docs, specs, ADRs, custom skills, and non-Ardvi MCP settings are
preserved.

To give the first agent a task during initialization:

```bash
make init PROMPT='Inspect this repository and propose the first implementation plan.'
```

The prompt is written once to `tasks/NEXT.md`. It is not sent to a provider and
an existing `tasks/NEXT.md` is never overwritten. For a long prompt:

```bash
make init PROMPT_FILE=/path/to/prompt.md
```

## Daily use

Start the shared local service:

```bash
make up
make status
```

Open the client you already use, from the project directory:

```bash
codex
# or
claude
```

Codex reads `AGENTS.md`, `.codex/config.toml`, and `.agents/skills/`. Claude
reads `CLAUDE.md`, `.mcp.json`, and `.claude/skills/`. `CLAUDE.md` imports
`AGENTS.md`, so both clients receive the same project policy.

At the start of a session, ask the agent to use `lets-go`. It registers the
native session, reads relevant messages and memory, and continues from the
repository state. Before clearing context or handing work to another agent, ask
it to use `session-end`.

Examples:

```text
Use lets-go and continue the task in tasks/NEXT.md.

Send the SDK agent a project message with the API decision.

Claim src/auth while you edit it, then release the claim.

Save this compatibility constraint as durable project memory.

Use session-end and leave a concise handoff.
```

Each project has its own UUID in `.ardvi/project.json`, so several projects can
use one server without mixing project messages or memory. An agent must request
`scope: global` explicitly for a cross-project message or global memory item.

Stop the service when no client needs it:

```bash
make down
```

There is no tmux session or Ardvi web interface. Codex and Claude keep their own
sessions, resume commands, context compaction, UI, and native subagents.

## Skills and personas

`make init` installs the complete managed catalogs. `make update` refreshes all
of them and prints their resolved commit SHA:

```bash
make update
```

Stop the hub before an update, then start it again so the new catalog becomes
visible:

```bash
make down
make update
make up
```

Installed upstreams:

- `addyosmani/agent-skills`;
- `msitarzewski/agency-agents` as optional personas;
- `DietrichGebert/ponytail`;
- `msimchowitz/writing-skills`, including the `writing` router,
  `general-writing`, `humanizer`, `writing-cadence`, `better-usage`,
  `non-autoregressive-writing-pass`, and `academic-voice`.

Built-in skills are `communication`, `writing`, `lets-go`, `session-end`, and
`project-context`. The small native entry points are discoverable immediately;
the full catalogs are searched and loaded through MCP only when needed.

`stop-slop`, if installed separately, is optional audit/debug tooling. It is not
part of the default writing pipeline.

## Memory between machines

Stop the hub, export durable project memory, inspect it for secrets, and commit
it only if appropriate:

```bash
make down
make memory-export OUTPUT=.ardvi/memory.jsonl
git diff -- .ardvi/memory.jsonl
```

On another machine, after `make init`:

```bash
make down
make memory-import INPUT=.ardvi/memory.jsonl
make up
```

Only durable project memory is exported. Global memory is excluded. Tracked
specs, ADRs, and task files remain the source of truth.

## Checks and troubleshooting

```bash
make doctor
make status
curl -fsS http://127.0.0.1:8765/healthz
```

The health response must be `{"status":"ok"}`. Inside Codex or Claude, open
`/mcp` and confirm that `ardvi` is connected. To verify persistence, ask one
session to store `memory-check-001` as durable project memory, end that session,
open a new native session, use `lets-go`, and ask it to search project memory for
`memory-check-001`.

If a client was open during `make init`, restart that client so it reloads MCP
configuration. On first open, approve/trust the repository and its project MCP
entry when Codex or Claude asks. The service listens only on `127.0.0.1:8765`;
it is not exposed to the network. Logs are stored under
`~/.local/share/project-harness/hub/server.log`.

Requirements: Linux, macOS, or WSL with Git, Python 3.10+, Go 1.25+, `curl`,
internet access for `make init`/`make update`, and at least one of Codex or
Claude Code.

## Harness development

```bash
bash -n .harness/scripts/*.sh
go -C .harness/mcp test -race ./...
bash .harness/tests/make_integration_test.sh
bash .harness/tests/copy_integration_test.sh
bash .harness/tests/writing_integration_test.sh
bash .harness/tests/harness_update_integration_test.sh
```

Architecture and behavior are recorded in `.harness/docs/`.

## License

MIT. See `LICENSE`.
