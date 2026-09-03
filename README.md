# ARDVI Harness

ARDVI Harness adds CAO, ready-made Codex and Claude Code agent profiles,
project instructions, managed skills, and a local Web UI to an existing Git
repository.

The quick start below uses `make harness-*` commands. These commands work in
every project, including projects that already have their own `Makefile`.

## Before you start

Run the commands from Bash on Linux or in a compatible Unix environment. On
Ubuntu or Debian, install the required system tools with:

```bash
sudo apt-get update
sudo apt-get install -y git make python3 curl tmux coreutils
```

Codex CLI and Claude Code use `npm`. Install Node.js 18 or newer and confirm
that `node` and `npm` are available:

```bash
node --version
npm --version
```

Install both agent CLIs with their official package names:

```bash
npm install -g @openai/codex
npm install -g @anthropic-ai/claude-code
```

Authenticate each CLI once:

```bash
codex --login
claude
```

Complete the account prompts. After Claude Code opens, exit it with `Ctrl-C`.
Then confirm that both commands are available:

```bash
codex --version
claude --version
```

See the official [Codex CLI setup](https://help.openai.com/en/articles/11096431)
and [Claude Code setup](https://docs.anthropic.com/en/docs/claude-code/getting-started)
if installation or authentication fails.

The target project must be a Git repository. For a new empty project:

```bash
mkdir -p /absolute/path/to/your-project
cd /absolute/path/to/your-project
git init
```

For an existing project, use its Git root. This command prints the correct
directory:

```bash
git -C /path/to/your-project rev-parse --show-toplevel
```

## Install the harness

Replace the two example paths, then copy and run this block:

```bash
PROJECT_DIR="/absolute/path/to/your-project"
HARNESS_DIR="/absolute/path/to/ardvi-harness"

git clone git@github.com:Lunden-Labs/ardvi-harness.git "$HARNESS_DIR"
make -C "$HARNESS_DIR" copy TARGET="$PROJECT_DIR"
make -C "$PROJECT_DIR" harness-init
make -C "$PROJECT_DIR" harness-up
```

The repository is private. The `git clone` command requires GitHub SSH access
to `Lunden-Labs/ardvi-harness`.

The copy command creates `.harness/` in the project. If the project already has
a regular `Makefile`, the command preserves its content and adds
`include .harness/harness.mk`. If no `Makefile` exists, it creates one.

`harness-init` may take a few minutes on its first run. It installs CAO, creates
the missing project files, installs the managed skills, and registers the agent
profiles. Existing project files are preserved.

When `harness-up` finishes, open this address in Chrome:

```text
http://127.0.0.1:9889
```

## Start the first agent

In the CAO Web UI:

1. Create a session.
2. Select the profile whose name ends with `-architect`.
3. Set the working directory to the target repository root.
4. Start the session.

The architect can delegate work to Codex and Claude Code workers. The Web UI
shows the same running processes that are available through tmux.

To start the architect directly in the current terminal instead of using the
Web UI:

```bash
cd /absolute/path/to/your-project
make harness-architect
```

## Continue a Web session in the terminal

Find the session name:

```bash
cao session list
```

Attach from a normal terminal:

```bash
tmux attach-session -t <session-name>
```

If the terminal is already inside tmux, switch to the CAO session instead of
nesting another tmux session:

```bash
tmux switch-client -t <session-name>
```

CAO workers appear as windows inside the same tmux session. The main keys are:

| Keys | Action |
|---|---|
| `Ctrl-b w` | Open the window list |
| `Ctrl-b n` | Move to the next window |
| `Ctrl-b p` | Move to the previous window |
| `Ctrl-b 0` through `Ctrl-b 9` | Select a window by number |
| `Ctrl-b d` | Detach without stopping the agents |

Hold `Shift` while selecting text with the mouse if tmux captures the
selection. The [console and tmux handbook](.harness/README.md#console-and-tmux-handbook)
describes copy mode, clipboard behavior, worker windows, and clean sessions.

## Daily commands

Run these commands from the target project:

| Command | Result |
|---|---|
| `make harness-up` | Start the CAO Web UI on `127.0.0.1:9889` |
| `make harness-status` | Show the server and session status |
| `make harness-architect` | Start the project architect in the terminal |
| `make harness-update` | Update CAO, the harness, profiles, and all managed skills |
| `make harness-doctor` | Check dependencies, generated files, skills, and CAO registration |
| `make harness-down` | Stop every local CAO session and the CAO Web UI |

If the harness created the project's root `Makefile`, the shorter forms also
work: `make up`, `make status`, `make architect`, `make update`, `make doctor`,
and `make down`.

`make harness-down` calls `cao shutdown --all`. It stops CAO sessions from
other projects on the same machine as well.

## Update an installed project

Do not copy the harness again when the target already contains `.harness/`.
Update it from the target project:

```bash
cd /absolute/path/to/your-project
make harness-update
```

The update uses the repositories declared in `.harness/harness-source.tsv` and
`.harness/upstreams.tsv`. It prints the resolved commit for every source and
returns a non-zero status if a managed file or checkout was modified locally.
Project-owned files and custom skills are not overwritten.

## Files added to the project

The first initialization creates missing files and directories from this set:

```text
.harness/
.cao/agents/
.cao/skills/
.cao/project.env
AGENTS.md
CLAUDE.md
docs/adr/
docs/specs/
tasks/
```

The harness manages `.harness/` and a marked communication block inside
`AGENTS.md` and `CLAUDE.md`. The rest of those instruction files remains owned
by the project. Existing documentation, ADRs, specifications, tasks, agent
profiles, and custom skills are preserved.

External skills are shared between projects on the same machine:

```text
~/.local/share/project-harness/
```

`make harness-init` installs them on a new machine. `make harness-update` moves
their managed Git checkouts forward and records the resolved revisions in
`~/.local/share/project-harness/upstreams.lock.tsv`.

## If something fails

Run the status and diagnostic commands first:

```bash
make harness-status
make harness-doctor
```

Common errors:

- `target is not a Git repository`: run `git init` in the project root.
- `target must be the Git repository root`: use the path printed by
  `git rev-parse --show-toplevel`.
- `target already has .harness`: run `make harness-update` in that project.
- `codex` or `claude` is missing: install and authenticate the missing CLI.
- The Web UI is unreachable: run `make harness-status`. Inspect the server with
  `tmux attach-session -t project-cao-server`.

The [full harness manual](.harness/README.md) covers MCP verification, Chrome
DevTools, memory export and import, writing skills, managed-file protection,
optional settings, and the complete tmux workflow.
