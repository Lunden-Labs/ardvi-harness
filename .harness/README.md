# Harness maintainer guide

## OpenCode native lifecycle

Configure a host-owned `agents.json` entry with `client: "opencode"`, exact
`model`, `model_provider`, optional `model_variant`, `agent_key`, and session
name. Run `ardvi opencode --agent-key KEY` to create or reuse the one matching
root OpenCode session in the current project. The launcher registers Ardvi
before attaching OpenCode; delivery uses the verified native session only.

The copied `.harness/` directory configures native Codex and Claude clients for
one machine-wide Ardvi MCP container. The host CLI offers `ardvi codex` and
`ardvi claude` shortcuts that replace themselves with the native client process.
Codex uses YOLO mode and its local daemon; Claude keeps its normal permissions.
There is no separate agent runtime, web UI, tmux layer, fixed roles or CAO integration.

## Lifecycle

- `make init` merges project instructions/configuration and ensures the already
  installed global service is running.
- `make update` delegates to `ardvi update`: it installs the released CLI,
  bundle, MCP image and skills, then refreshes this project's integration.
- `make up` idempotently ensures the global Compose service is running.
- `make down` stops that machine-wide service and affects every project.
- `make skills` lists the catalog reported by the running MCP service.

The release installer owns `~/.config/ardvi`, `~/.local/share/ardvi/harness`,
the `ardvi` host binary, the `ardvi-mcp` container, and Docker volume
`ardvi-data`. A target repository owns its `.ardvi/` identity, instructions,
custom skills, docs, ADRs, specs, and task files.

A target repository must also commit `.harness/` in full, including
`.managed-state.json` and `LICENSE` (a verbatim copy of this repository's MIT
license, so the notice travels with the copied harness). `manage_harness.py`
writes the state file deterministically (sorted keys, stable indent, trailing
newline) so it diffs cleanly; it records the installed commit and a checksum
of every managed file, including `LICENSE`, and `make update` uses it to
detect local edits before replacing anything. A fresh clone that is missing
the state file requires `ardvi update --replace-harness`, which retains a backup.

## Project files

Bootstrap creates missing files and merges only checksum-protected Ardvi blocks:

```text
.ardvi/project.json       stable project UUID
.codex/config.toml        Codex project MCP configuration
.codex/hooks.json         Codex session-start/prompt/session-end hooks
.mcp.json                 Claude project MCP configuration
.claude/settings.json     Claude session-start/prompt/session-end hooks
.agents/skills/           small Codex entry skills
.claude/skills/           small Claude entry skills
AGENTS.md                 shared policy source
CLAUDE.md                 imports AGENTS.md
```

Existing files and non-Ardvi MCP entries are preserved. Modified managed blocks
or entry skills fail closed instead of being overwritten.

## Message delivery

Three complementary delivery layers:

1. **Hooks (both clients).** `project_config.py` merges a `SessionStart`,
   `UserPromptSubmit`, and `SessionEnd` entry — identified by its `ardvi hook `
   command prefix — into `.claude/settings.json` and `.codex/hooks.json`,
   without touching any other tool's entries in those files. `SessionStart`
   reconciles the stable agent and ephemeral session, then directs the model to
   call `context_bootstrap(session_id)`; `UserPromptSubmit` reconciles prompt
   context and surfaces only messages not already shown; `SessionEnd` ends the
   session. Codex asks
   the user to trust project hooks the first time it runs one (`/hooks` to
   review and trust, or `--dangerously-bypass-hook-trust` for automation); this
   is a one-time step per project, not a harness bug.
2. **The `unread` piggyback.** Tool results from `message_send`, `message_ack`,
   and `claim_*` calls carry an `unread` field, so messages surface during
   ordinary MCP calls even between hook firings.
3. **Background delivery.** Native delivery remains implementation-specific.
   Ardvi never launches Codex, Claude, or provider-owned subagents. Delivery is
   labelled as agent correspondence and does not acknowledge a message.

Claude configuration starts `ardvi hook watch --client claude` at SessionStart
and rearms it at Stop using `asyncRewake`. The one-listener lock prevents duplicate
watchers. Codex starts its optional `codex-bridge` when the app-server daemon
socket is available; `ARDVI_CODEX_BRIDGE_DISABLE=1` opts out. Native process
liveness is maintained by a separate lease keeper for each verified client,
independently of inbox delivery. See the [native delivery contract](docs/agent-protocol.md#native-delivery)
for compatibility limits and recovery behavior.

`inbox_read` remains available for catching up on history; it is not the
primary delivery path.

## MCP service and persistence

The service uses stateless Streamable HTTP at `127.0.0.1:8765`. Project state
tools require `X-Ardvi-Project`; this header selects a namespace and is not
authentication. The service trusts local processes only. Remote deployment is
unsupported until authenticated host/session binding exists; do not expose the
loopback port beyond the machine.

See [the agent protocol](docs/agent-protocol.md) for identity,
bootstrap, routing, ownership, and recovery semantics.

Provider sessions stay native. Claims expire and `session_end` releases live
claims. The bounded JSON state snapshot is stored in Docker volume `ardvi-data`
with an exclusive writer lock, sync, and atomic rename. Per-project history
quotas stop one project from consuming the complete shared store.

## Managed catalogs

`upstreams.tsv` records update branches. `upstreams.lock.tsv` pins exact release
commits. The image build copies complete harness skills and complete upstream
trees into `/opt/ardvi`, removes Git metadata, validates Writing Skills
dependencies, and builds `catalog.json` with container paths.

`skills_list` returns bounded metadata pages plus source revisions.
`skills_search` narrows the catalog and `skill_read` loads one entry point or
dependency. Agency Agents are optional personas exposed through
`personas_search` and `persona_read`.

Only entry skills are copied into projects: `communication`, `writing`,
`lets-go`, `session-end`, `project-context`, and `skills-list`.

## Release

Tagging `v*` runs `.github/workflows/ci.yml`. It publishes:

- the multi-platform image `ghcr.io/lunden-labs/ardvi-mcp`;
- Linux/macOS `amd64` and `arm64` host archives;
- `release-manifest.json` with the immutable image digest, artifact checksums,
  harness commit, and upstream commit SHAs;
- `SHA256SUMS`, image SBOM, and provenance.

The host binary embeds its version and release base URL. `ardvi install` uses
the matching release manifest; `ardvi update` and `ardvi skills update` use the
latest manifest. Persistent Compose metadata is promoted only after the new
service becomes healthy. `ardvi update` verifies the platform archive and uses
its installer to update the host, then refreshes the current project from that
same release. Source checkouts keep their tracked harness. `ardvi skills update`
retains its service/catalog-only behavior. Older CLIs can bootstrap the unified
updater with the root `upgrade.sh`. Native hooks replace outdated matching Codex
bridge processes on their next invocation after a host binary upgrade.

Before tagging:

```bash
make harness-upstream-lock
bash -n install.sh upgrade.sh .harness/scripts/*.sh
make -n help
go -C .harness/mcp test -race ./...
go -C .harness/mcp vet ./...
for test in .harness/tests/*_test.sh; do bash "$test"; done
```
