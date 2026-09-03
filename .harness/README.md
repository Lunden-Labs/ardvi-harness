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

# Refresh managed instructions, CAO, and external skills/profiles
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

`make init` creates missing project files, adds a small managed communication
block to existing `AGENTS.md` and `CLAUDE.md`, installs external sources, and
registers the skill/profile directories with CAO. Repeating it is safe.

`make update` first fast-forwards the checksum-verified `.harness` copy from
the source declared in `.harness/harness-source.tsv`. It then refreshes the
managed communication block and fast-forwards each external source declared in
`.harness/upstreams.tsv`. It prints the old/new state and full commit SHA, then
writes the resolved external set to
`~/.local/share/project-harness/upstreams.lock.tsv`. A dirty managed checkout,
locally changed harness file, unexpected origin/branch, broken writing
dependency, or edited managed instruction block stops the command with a
non-zero exit. Project text outside the marked block is not rewritten.

## Console and tmux handbook

Run CAO from the project root:

```bash
make init       # once: create project files, install skills, register CAO
make up         # start http://127.0.0.1:9889
make architect  # alternatively, launch the architect in this terminal
```

When creating an agent in the Web UI, select the project profile and set the
working directory to the repository root. The Web UI and tmux connect to the
same process and context; creating a second terminal does not create a second
agent.

### Find and attach to a session

List CAO sessions and inspect their conductor and workers:

```bash
cao session list
cao session status <session>
cao session status <session> --workers
```

Use the exact session name shown by `cao session list`. From a normal terminal:

```bash
tmux attach-session -t <session>
```

If the terminal is already inside tmux, do not nest another tmux client. Switch
the current client instead:

```bash
tmux switch-client -t <session>
```

To leave the agent running, press `Ctrl-b`, release both keys, then press `d`.
Closing the terminal after detaching does not stop the CAO session.

### Conductor and worker windows

A CAO session is normally one tmux session. Its conductor and spawned workers
appear as windows inside that session, not as separate tmux sessions. List them
from another terminal with:

```bash
tmux list-windows -t <session>
```

Inside tmux, the useful default keys are:

| Keys | Action |
|---|---|
| `Ctrl-b w` | Open the window chooser |
| `Ctrl-b n` | Next window |
| `Ctrl-b p` | Previous window |
| `Ctrl-b l` | Return to the last window |
| `Ctrl-b 0` … `Ctrl-b 9` | Select a window by number |
| `Ctrl-b d` | Detach and keep the session running |

From another shell, select a known window directly:

```bash
tmux select-window -t <session>:<window-index>
```

Do not kill a worker with `tmux kill-window`: CAO may still consider its
terminal active. Close workers through the orchestrator, and use this command
only when the entire session should stop:

```bash
cao shutdown --session <session>
```

`make down` is broader: it stops every CAO session on the machine and the local
control plane.

### Send messages without attaching

Send a blocking message to the conductor:

```bash
cao session send <session> 'Continue with the next task.'
```

Send to a specific worker using the terminal ID shown by `--workers`:

```bash
cao session send <session> --terminal <terminal-id> 'Report current status.'
```

Use `--async` when the shell should return immediately:

```bash
cao session send <session> --async 'Continue in the background.'
```

Avoid typing through the Web UI and tmux at the same time. Both interfaces send
keystrokes to the same PTY.

### Selection, copy, paste, and scrollback

Mouse selection is controlled jointly by tmux and the terminal emulator. If
tmux captures the mouse, ordinary terminal selection can appear and immediately
disappear.

For normal terminal-style selection, hold `Shift` while dragging with the
mouse. Most terminal emulators then handle selection, copy, and the right-click
menu directly.

To make the terminal own the mouse permanently for the current tmux server:

```bash
tmux set-option -g mouse off
```

Restore tmux mouse scrolling and window/pane selection with:

```bash
tmux set-option -g mouse on
```

For tmux-native scrollback, press `Ctrl-b [` to enter copy mode. Check the key
style with:

```bash
tmux show-option -g mode-keys
```

With `vi` keys, press `Space` to begin selection and `Enter` to copy. With
`emacs` keys, use `Ctrl-Space` and then `Alt-w`. Press `q` to leave copy mode and
`Ctrl-b ]` to paste the tmux buffer.

Right-click copy/paste behavior belongs to the terminal emulator, so its exact
menu and clipboard shortcuts may differ. When predictable system-clipboard
integration matters, keep this tmux option enabled in `~/.tmux.conf`:

```tmux
set -g set-clipboard on
```

### Clean context for the next task

Detaching preserves the same agent and context. For a genuinely clean context,
stop the old CAO session and create a new one from the Web UI or CLI:

```bash
cao shutdown --session <session>
```

Before stopping it, save durable results in Git: task status under `tasks/`,
accepted decisions under `docs/adr/`, specifications under `docs/specs/`, and
any deliberately exported CAO memory described below.

## Writing and communication

Every CAO profile loads the machine-wide managed `communication` skill. It
keeps normal terminal answers direct and does not run a heavy humanizing pass
for ordinary conversation. Durable prose—READMEs, documentation, runbooks,
reports, ADRs, RFCs, and design documents—routes through the upstream `writing`
router, which loads only the relevant writing skill.

The most common routes are:

```text
terminal/chat       -> communication
README/docs         -> writing -> general-writing
report/ADR/RFC      -> evidence and structure -> writing -> consistency pass
explicit humanize  -> humanizer
```

Commands, code, identifiers, paths, versions, numbers, units, protocol names,
and factual qualifications stay literal unless the task explicitly changes
them. The policy supports English and Russian technical prose. A separately
installed `stop-slop` remains available as an explicit audit/debug tool; the
harness never invokes it automatically.

CAO discovers the skills from its registered directories. A direct Codex or
Claude Code session receives the same policy through `AGENTS.md` / `CLAUDE.md`.
Registration removes legacy per-project `.harness/skills` entries so an older
copy cannot shadow the shared managed `communication` skill.
If a direct client does not expose an external skill by name, locate its exact
managed file without hard-coding a machine path:

```bash
make harness-skill-path SKILL=communication
make harness-skill-path SKILL=writing
make harness-skill-path SKILL=general-writing
```

## Verify MCP, Chrome, and memory

First confirm that the profile is valid and contains `cao-mcp-server`:

```bash
cao profile validate .cao/agents/<project>-architect.md
cao profile show <project>-architect
```

Then ask a running agent to exercise the server. A successful response proves
that CAO orchestration tools are available to the agent:

```bash
cao session send cao-SESSION \
  'Call the CAO MCP list_siblings tool once. Reply exactly MCP_OK if it succeeds.'
```

If Chrome DevTools is configured for Codex, verify it independently:

```bash
cao session send cao-SESSION \
  'Use Chrome DevTools to open data:text/html,<title>CHROME_OK</title>, read document.title, and return it.'
```

Memory must be enabled before testing it:

```bash
cao config get memory.enabled
cao session send cao-SESSION \
  'Use memory_store to save "CAO memory smoke test" with project scope and key cao-memory-smoke. Then use memory_recall for that key and reply MEMORY_OK only if found.'
cao memory show cao-memory-smoke --scope project
cao memory delete cao-memory-smoke --scope project --yes
```

CAO memory is local by default. Durable project truth should live in `tasks/`,
accepted ADRs, approved specs, tests, and Git. To carry CAO project memory to
another machine as well, export a redacted bundle into the repository:

```bash
cao memory export --scope project --output .cao/memory --redact
git add .cao/memory

# After cloning on another machine:
cao memory import .cao/memory --scope project --conflict merge --dry-run
cao memory import .cao/memory --scope project --conflict merge
```

`make improve` starts an interactive Codex maintenance pass in the target
repository. It reads `AGENTS.md` and this harness guide, analyzes the harness
first, then makes at most one small reviewable portability or safety
improvement with narrow checks. It does not touch product code or commit or
push automatically. Review its diff before committing.

## Managed upstreams

The skeleton itself tracks `Lunden-Labs/ardvi-harness` `main` through
`.harness/harness-source.tsv`. External skills and profiles use the separate
manifest below. The harness repository is private, so self-update requires SSH
access to its configured Git origin.

`make init` and every `make update` clone or fast-forward these repositories:

| Source | Integration |
|---|---|
| `addyosmani/agent-skills` | Every directory under `skills/` is registered with CAO |
| `msitarzewski/agency-agents` | Markdown personas are regenerated as CAO-compatible `agency-*` profiles |
| `DietrichGebert/ponytail` | Every directory under `skills/` is registered with CAO |
| `msimchowitz/writing-skills` | The complete `for-agents/` tree is registered; `writing` is the router |

Managed checkouts live under:

```text
~/.local/share/project-harness/upstreams/
```

The installer does not execute scripts supplied by these repositories. It clones their content, validates expected paths, converts Agency Agents locally, and registers the resulting directories with CAO.

The declarative external source list is `.harness/upstreams.tsv`. It records each name,
repository, tracked revision, installed path, and update policy. The generated
lock records the full resolved commit SHA for the current machine. An update
refuses to proceed if a managed checkout has local modifications or its
`origin` URL or branch differs from the manifest.

Ownership is explicit:

```text
.harness/**                                      harness-managed (checksum guarded)
~/.local/share/project-harness/upstreams/**      upstream-managed
~/.local/share/project-harness/skills/**         harness-managed shared skills
AGENTS.md, CLAUDE.md, docs/, specs/, custom skills  project-owned
```

Only the checksum-marked communication block inside `AGENTS.md` and
`CLAUDE.md` is harness-managed. Editing that block causes a conflict on update;
text outside it remains project-owned. `make copy` records checksums in the
ignored `.harness/.managed-state.json`; self-update refuses changed, missing,
or unexpected harness files and refuses non-fast-forward history. In the
standalone harness source checkout, use the normal Git workflow instead.

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
- Existing instructions receive one checksum-protected communication block;
  repeated init/update does not duplicate it.
- CAO `agents.extra_dirs` and `skills.extra_dirs` are merged; unrelated entries
  are retained and obsolete per-project harness skill paths are removed.

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
.harness/skills/communication/SKILL.md
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
