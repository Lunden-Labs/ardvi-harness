# Harness maintainer guide

The copied `.harness/` directory configures native Codex and Claude clients for
one machine-wide Ardvi MCP container. It contains no agent launcher, web UI,
tmux layer, fixed roles, CAO integration, or provider wrapper.

## Lifecycle

- `make init` merges project instructions/configuration and ensures the already
  installed global service is running.
- `make update` safely refreshes the managed harness copy, merges project files,
  and updates the global MCP image and every managed skill.
- `make up` idempotently ensures the global Compose service is running.
- `make down` stops that machine-wide service and affects every project.
- `make skills` lists the catalog reported by the running MCP service.

The release installer owns `~/.config/ardvi`, `~/.local/share/ardvi/harness`,
the `ardvi` host binary, the `ardvi-mcp` container, and Docker volume
`ardvi-data`. A target repository owns its `.ardvi/` identity, instructions,
custom skills, docs, ADRs, specs, and task files.

## Project files

Bootstrap creates missing files and merges only checksum-protected Ardvi blocks:

```text
.ardvi/project.json       stable project UUID
.codex/config.toml        Codex project MCP configuration
.mcp.json                 Claude project MCP configuration
.agents/skills/           small Codex entry skills
.claude/skills/           small Claude entry skills
AGENTS.md                 shared policy source
CLAUDE.md                 imports AGENTS.md
```

Existing files and non-Ardvi MCP entries are preserved. Modified managed blocks
or entry skills fail closed instead of being overwritten.

## MCP service and persistence

The service uses stateless Streamable HTTP at `127.0.0.1:8765`. Project state
tools require `X-Ardvi-Project`; this UUID is namespace isolation, not
authentication. The port must not be exposed beyond loopback.

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
service becomes healthy.

Before tagging:

```bash
make harness-upstream-lock
bash -n install.sh .harness/scripts/*.sh
make -n help
go -C .harness/mcp test -race ./...
go -C .harness/mcp vet ./...
for test in .harness/tests/*_test.sh; do bash "$test"; done
```
