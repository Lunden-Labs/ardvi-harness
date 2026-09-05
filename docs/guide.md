# Ardvi user guide

Ardvi lets ordinary Codex and Claude Code sessions share messages, memory,
resource claims, skills, and optional agent personas. You keep using the normal
Codex or Claude interface. Ardvi does not start agents and has no web UI, tmux,
or provider wrapper.

One Docker container runs for the whole computer. Project A and project B use
the same service but receive different UUID namespaces, so their project data
does not mix.

## Install once on a computer

You need:

- Docker Desktop, or Docker Engine with `docker compose`;
- Git, Make, Bash, and Python 3.10 or newer;
- Codex, Claude Code, or both, already installed and authenticated.

Open the repository's **Releases** page and download the archive for your
computer:

| Computer | Archive |
| --- | --- |
| Linux, Intel/AMD | `ardvi_linux_amd64.tar.gz` |
| Linux, ARM64 | `ardvi_linux_arm64.tar.gz` |
| macOS, Intel | `ardvi_darwin_amd64.tar.gz` |
| macOS, Apple silicon | `ardvi_darwin_arm64.tar.gz` |

Linux Intel/AMD example:

```bash
curl -fLO https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_linux_amd64.tar.gz
mkdir -p ardvi-install
tar -xzf ardvi_linux_amd64.tar.gz -C ardvi-install
./ardvi-install/install.sh
```

Apple silicon example:

```bash
curl -fLO https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_darwin_arm64.tar.gz
mkdir -p ardvi-install
tar -xzf ardvi_darwin_arm64.tar.gz -C ardvi-install
./ardvi-install/install.sh
```

The installer copies `ardvi` to `~/.local/bin`, installs the harness bundle,
pulls the exact release image by digest, and starts the shared service. For Bash,
it configures `~/.bashrc` and the first existing login file (`~/.bash_profile`,
`~/.bash_login`, or `~/.profile`, creating `.profile` if none exists). For Zsh,
it configures `${ZDOTDIR:-$HOME}/.zshrc`. The PATH guard adds the installation
directory only when absent. Reinstalling preserves the guard and recognized
existing exports without rewriting the file.

Set `ARDVI_BIN_DIR` to use another installation directory. Other shells need
manual PATH configuration; the installer prints a notice. If a startup file
cannot be written, the CLI and service remain installed, but the installer
returns an error and prints the PATH setting to add manually.

The installer cannot change an already-open terminal's environment. Open a new
terminal, or for the default installation directory run:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Check the installation:

```bash
ardvi version
ardvi service status
ardvi skills list
```

## Add Ardvi to a project

The directory must be a Git repository. For a new empty project:

```bash
mkdir my-project
cd my-project
git init
ardvi init
```

For an existing project:

```bash
cd /path/to/existing-project
ardvi init
```

You can initialize a project without changing directories:

```bash
ardvi init --path /path/to/existing-project
```

To leave the first task for the agent:

```bash
ardvi init --path /path/to/project \
  --prompt 'Inspect this repository and prepare an implementation plan.'
```

For a long task description:

```bash
ardvi init --path /path/to/project --prompt-file /path/to/task.md
```

The same first-task feature is available through Make inside a copied harness:

```bash
make init PROMPT='Inspect this repository and prepare an implementation plan.'
make init PROMPT_FILE=/path/to/task.md
```

Commit the copied `.harness/` directory, including the tracked
`.managed-state.json`, to the project's own Git history. That file records the
installed commit and file checksums; without it, `make update` and a fresh
clone of the project cannot self-update.

The prompt is written once to `tasks/NEXT.md`; it is not sent to Codex or
Claude automatically. Existing `AGENTS.md`, `CLAUDE.md`, docs, ADRs, specs,
custom skills, and non-Ardvi MCP settings are preserved. A repeated `make init`
is safe and does not replace project-owned files.

## Daily use

Enter the project, ensure the shared service is running, then start your normal
client:

```bash
cd /path/to/project
make up
codex
```

or:

```bash
cd /path/to/project
make up
claude
```

A `SessionStart` hook registers the session and prints unread messages before
you type anything, and a `UserPromptSubmit` hook keeps delivering new ones as
they arrive, so most sessions no longer need this first instruction. Give it
anyway if you want the agent to also pick up `tasks/NEXT.md`:

```text
Use lets-go and continue the task in tasks/NEXT.md.
```

On the first run, trust the Git project when Codex asks, approve the project
MCP server when Claude asks, and trust the project hooks when Codex asks
(`/hooks` to review and trust). Restart the client after `make init` if the
Ardvi tools are not visible yet.

Before clearing context or handing work to another session:

```text
Use session-end and leave a concise handoff.
```

Codex and Claude still own their sessions, resume commands, context compaction,
UI, and native subagents. Ardvi only supplies the shared channel.

## Skills

To list every skill installed on the running MCP server:

```bash
ardvi skills list
# inside a harness project, the same command is:
make skills
```

You can also ask an agent:

```text
Use skills-list and show the skills installed on Ardvi MCP, grouped by source.
```

The catalog is lazy: agents call `skills_search` first and `skill_read` only for
skills needed by the current task. Skill bodies and their `references/`,
`scripts/`, `templates/`, and other dependencies are retained together.

Managed sources in the release image are:

- [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills);
- [msitarzewski/agency-agents](https://github.com/msitarzewski/agency-agents),
  exposed as optional personas through `personas_search`;
- [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail);
- [msimchowitz/writing-skills](https://github.com/msimchowitz/writing-skills),
  including `writing`, `general-writing`, `humanizer`, `writing-cadence`,
  `better-usage`, `non-autoregressive-writing-pass`, and `academic-voice`.

Built-in entry skills are `communication`, `writing`, `lets-go`, `session-end`,
`project-context`, and `skills-list`.

`stop-slop` is not part of the default pipeline. If installed separately, treat
it only as optional audit/debug tooling.

### Where the skills are stored

Yes: the complete Addy Osmani, Ponytail, Writing Skills, and Agency Agents
trees live in the MCP container under `/opt/ardvi`; they are not copied into
every project. They are read-only and tied to the release's exact commit SHA.

Each project receives only six small entry skills in `.agents/skills/` and
`.claude/skills/`. These let Codex and Claude discover the workflow before they
contact MCP. Project-owned custom skills remain in the project and are never
updated by Ardvi.

## Updates

Update the shared MCP image and every managed catalog at once:

```bash
ardvi update
# equivalent:
ardvi skills update
```

The command downloads the latest release manifest, pulls an image pinned by
digest, starts it, waits for health, and keeps the previous configuration if the
new service fails.

Run this in each initialized project when you also want its managed harness
files refreshed:

```bash
cd /path/to/project
make update
```

The container and skills update is machine-wide, so doing it from one project
updates the server used by all projects. The copied `.harness/` directory is
per-project, so `make update` must be run separately in every project that needs
new bootstrap files. Local changes to managed blocks cause a visible conflict;
they are never silently overwritten.

## Several projects at once

```bash
cd /work/project-a && make up
cd /work/project-b && make up
```

Both commands address the same Compose project named `ardvi`. The second is an
idempotent health/start operation; it does not create a second MCP container.
Project messages and memory stay isolated by `.ardvi/project.json`. A tool call
must explicitly use `scope: global` to cross project boundaries.

`make down` stops the machine-wide service and therefore disconnects every
project. Usually leave it running. Use `make down` only when no project needs
Ardvi.

## Files and persistent data

```text
~/.config/ardvi/compose.yaml          global Compose configuration
~/.config/ardvi/installed-release.json exact image and upstream revisions
Docker volume ardvi-data              messages, memory, sessions, claims
/opt/ardvi inside the image           complete managed skill/persona catalogs

project/.ardvi/project.json           stable project UUID
project/.codex/config.toml            Codex MCP connection
project/.codex/hooks.json             Codex session-start/prompt/session-end hooks
project/.mcp.json                     Claude MCP connection
project/.claude/settings.json         Claude session-start/prompt/session-end hooks
project/AGENTS.md                     shared project instructions
project/CLAUDE.md                     imports AGENTS.md
project/.agents/skills/               Codex entry skills
project/.claude/skills/               Claude entry skills
```

The MCP endpoint is `http://127.0.0.1:8765/mcp`. It is deliberately published
only on loopback. The project UUID is isolation, not authentication; do not
expose this port to a network.

## Memory between machines

Only durable project memory is exported. Global memory is excluded. Stop the
shared service first because the store permits one writer:

```bash
cd /path/to/project
make down
make memory-export OUTPUT=.ardvi/memory.jsonl
make up
```

Inspect the file for secrets before committing it. On another machine:

```bash
cd /path/to/project
make down
make memory-import INPUT=.ardvi/memory.jsonl
make up
```

Use tracked ADRs, specs, and task files as the source of truth. MCP memory is
for concise observations and handoffs.

## Troubleshooting

```bash
make status
make doctor
docker logs ardvi-mcp
ardvi skills list --json
```

If port `8765` is already occupied, stop the unrelated process or change that
service. Ardvi intentionally uses one fixed local endpoint so every project can
share it.

## Maintainer release flow

Refresh and review pinned upstream revisions before a release:

```bash
make harness-upstream-lock
git diff -- .harness/upstreams.lock.tsv
```

A `v*` tag runs CI, tests the harness, builds Linux/macOS host binaries, builds
the Linux `amd64`/`arm64` MCP image, publishes it to GHCR with SBOM and
provenance, and creates `release-manifest.json` plus checksums. The manifest
contains the exact image digest and every upstream commit SHA. The GHCR package
must be public for installation without `docker login`.

## License

MIT. See [LICENSE](../LICENSE).
