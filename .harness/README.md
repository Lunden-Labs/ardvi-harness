# Harness maintainer guide

The copied `.harness/` directory installs one local Go MCP service and configures
the original Codex and Claude clients. It contains no agent launcher, tmux layer,
dashboard, fixed role system, CAO integration, or Agent Mail integration.

## Lifecycle

- `make init` runs idempotent project bootstrap, builds `ardvi-mcp`, installs all
  managed upstreams, builds the lazy catalog, and runs `doctor`.
- `make update` safely updates a managed harness copy, merges managed project
  instructions/configuration, updates every upstream, rebuilds the catalog, and
  prints resolved revisions.
- `make up` starts only `ardvi-mcp` on `127.0.0.1:8765`.
- `make down` stops only the harness-managed MCP process.

The repository owns `.harness/`. Upstream-managed data lives under
`${PROJECT_HARNESS_DATA_DIR:-~/.local/share/project-harness}`. Project-owned
files remain in the target repository. Managed instruction blocks and native
skill copies refuse locally modified replacements.

## Project files

Bootstrap creates files only when absent and merges only the Ardvi entries:

```text
.ardvi/project.json       stable project UUID
.codex/config.toml        Codex project MCP configuration
.mcp.json                 Claude project MCP configuration
.agents/skills/           small Codex entry skills
.claude/skills/           small Claude entry skills
AGENTS.md                 shared policy source
CLAUDE.md                 imports AGENTS.md
```

It never removes an old `.cao/` directory from a target project; that content may
belong to the user even though the harness no longer reads it.

## MCP service

The service uses stateless Streamable HTTP. `X-Ardvi-Project` selects the project
namespace and is not authentication. The listener is loopback-only and rejects
non-local `Host` and `Origin` values.

Provider sessions stay native. Agents explicitly call `session_start` and pass
the returned `session_id` to inbox, acknowledgement, and claim tools. Claims
expire automatically; `session_end` also releases them. Tasks and status remain
in Git rather than a second task database.

State is serialized to a bounded JSON snapshot using sync and atomic rename. A
second writer is rejected with a process lock.

## Managed catalogs

`upstreams.tsv` is the source manifest and `upstreams.lock.tsv` records resolved
commit SHA values. `build_catalog.py` indexes skill entry points and Agency Agents
personas. MCP search returns metadata first; `skill_read` and `persona_read`
load content on demand. File reads are limited to managed checkout roots and
reject absolute paths, symlink escapes, `.git`, non-regular files, and files over
1 MiB.

Writing dependencies are installed as the complete upstream `for-agents/`
directory. Do not copy individual `SKILL.md` files out of that tree.

## Validation

Run before committing:

```bash
bash -n .harness/scripts/*.sh
make -n help
go -C .harness/mcp test -race ./...
for test in .harness/tests/*_test.sh; do bash "$test"; done
```

`make doctor` is the state-changing integration check because it expects the
installed binary, upstream catalog, and generated project configuration.
