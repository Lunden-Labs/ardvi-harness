# Portable CAO project harness

The harness lives inside the target repository:

```text
repository/
├── Makefile
└── .harness/
    ├── harness.mk
    ├── scripts/
    └── templates/
```

The root `Makefile` contains:

```make
ARDVI_HARNESS_SHORT_TARGETS := 1
include .harness/harness.mk
```

This standalone mode enables the short commands below. If the project already
has a `Makefile`, add only `include .harness/harness.mk` instead of replacing
it; use namespaced commands such as `make harness-init` to avoid collisions.

To copy this harness into an existing Git repository root, run `make copy`
from a standalone harness checkout, or `make harness-copy` from an existing
product Makefile, then enter the target when prompted. `TARGET=/path/to/repository`
is the optional noninteractive form. It refuses existing `.harness` directories
and non-regular Makefiles, never overwrites, and prints the correct next init
command.

## Primary commands

```bash
# First-time initialization
make init

# Update CAO and all external skill/profile repositories
make update

# Start the local CAO control plane
make up

# Stop CAO sessions and the control plane
make down
```

With an existing product Makefile, prefix each command with `harness-`, for
example `make harness-init`, `make harness-up`, and `make harness-down`.

Optional commands:

```bash
make status
make architect
make improve
make doctor
```

`make up` starts the CAO Web UI without opening an interactive agent in the current terminal. Create sessions from the UI or run `make architect`.

`make improve` starts an interactive Codex maintenance pass in the target
repository. It reads `AGENTS.md` and this harness guide, analyzes the harness
first, then makes at most one small reviewable portability or safety
improvement with narrow checks. It does not touch product code or commit or
push automatically. Review its diff before committing.

## Bundled current upstreams

`make init` and every `make update` clone or fast-forward these repositories:

| Source | Integration |
|---|---|
| `addyosmani/agent-skills` | Every directory under `skills/` is registered with CAO |
| `msitarzewski/agency-agents` | Markdown personas are regenerated as CAO-compatible `agency-*` profiles |
| `DietrichGebert/ponytail` | Every directory under `skills/` is registered with CAO |

Managed checkouts live under:

```text
~/.local/share/project-harness/upstreams/
```

The installer does not execute scripts supplied by these repositories. It clones their content, validates expected paths, converts Agency Agents locally, and registers the resulting directories with CAO.

An update refuses to proceed if a managed checkout has local modifications or its `origin` URL differs from the expected upstream.

## Project discovery

The scripts call:

```bash
git rev-parse --show-toplevel
```

Therefore `.harness` can be invoked from any subdirectory through the root `Makefile` and still resolves the correct project root.

`PROJECT_SLUG` is an optional override only. Normally the harness derives it from the Git repository directory name and persists it in `.cao/project.env`.

Example only when an explicit stable namespace is required:

```bash
PROJECT_SLUG=ardvi make init
```

## Idempotency contract

The bootstrap never overwrites an existing project file.

- Existing ADR directories under the root or `docs/` remain untouched.
- Existing spec/specification directories under the root or `docs/` remain untouched.
- Missing directories are created as `docs/adr/` and `docs/specs/` with templates.
- Existing `AGENTS.md`, `CLAUDE.md`, `tasks/`, `.cao/agents/`, and `.cao/skills/` content is preserved.
- CAO `agents.extra_dirs` and `skills.extra_dirs` are merged; existing entries are retained.

## Generated project content

```text
AGENTS.md
CLAUDE.md
docs/adr/
docs/specs/
tasks/
.cao/project.env
.cao/agents/<project>-architect.md
.cao/agents/<project>-backend-claude.md
.cao/agents/<project>-backend-codex.md
.cao/agents/<project>-reviewer-claude.md
.cao/agents/<project>-reviewer-codex.md
.cao/skills/<project>-project-context/SKILL.md
.cao/skills/<project>-external-catalog/SKILL.md
```

## Network boundary

`make up` binds `cao-server` only to loopback:

```text
http://127.0.0.1:9889
```

Remote access, reverse proxies, VPNs, authentication, DNS, TLS, firewall policy, and tunnel lifecycle are deliberately outside this harness.

## Optional overrides

```bash
# Install CAO main instead of the latest tagged PyPI release
CAO_INSTALL_SOURCE=main make update

# Relocate shared upstream checkouts
PROJECT_HARNESS_DATA_DIR=/srv/cao-harness make init

# Change the local CAO port
CAO_PORT=9989 make up
```

`make down` invokes `cao shutdown --all`; it stops all CAO-managed sessions on this machine because `cao-server` is a machine-wide control plane.
