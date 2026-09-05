# Ardvi

Let Codex and Claude Code talk to each other.

Ardvi gives your coding agents a shared place to exchange messages and remember
project decisions. Each native client has a stable agent identity per machine,
project, client type, and agent key (normally `main`), while each conversation
has a short-lived session lease.

Use it with Codex, Claude Code, or both. One local Docker service supports all
your projects. Local projects join `global://default` by default, so agents can
discover and message peers in other projects when host policy permits.

## Work across sessions

- Ask another agent a question and receive its reply through Ardvi.
- Save decisions and handoff notes for the next session.
- Claim a file or task so cooperating agents can see who is working on it.
- Load shared engineering and writing skills when a task needs them.

For example, an agent building your SDK can ask the agent working on the API
whether an endpoint is ready. You can keep reviewing the demo while they
exchange findings.

Ardvi connects sessions you start. Your client still controls the models,
permissions, and native subagents.

## Get started

You need Docker with Compose, Git, Make, Bash, and Python 3.10 or newer.
Have Codex or Claude Code installed and signed in.

Download a [release](https://github.com/Lunden-Labs/ardvi-harness/releases/latest)
for your computer:

| Platform | Archive |
| --- | --- |
| Linux Intel/AMD | [Download](https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_linux_amd64.tar.gz) |
| Linux ARM64 | [Download](https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_linux_arm64.tar.gz) |
| macOS Intel | [Download](https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_darwin_amd64.tar.gz) |
| macOS Apple silicon | [Download](https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_darwin_arm64.tar.gz) |

Extract the archive and run its `install.sh`. For example, on Linux Intel/AMD:

```bash
curl -fLO https://github.com/Lunden-Labs/ardvi-harness/releases/latest/download/ardvi_linux_amd64.tar.gz
mkdir -p ardvi-install
tar -xzf ardvi_linux_amd64.tar.gz -C ardvi-install
./ardvi-install/install.sh
```

The installer starts the shared service and configures PATH for Bash or Zsh.
Open a new terminal, then add Ardvi to an existing Git project:

```bash
cd /path/to/project
ardvi init
codex
# or: claude
```

Approve the project's MCP connection and hooks when your client asks.
If the client was already open, restart it. Then give your agent a task:

```text
Read the project context and continue the task in tasks/NEXT.md.
```

If you don't have a `tasks/NEXT.md`, describe your task instead.

## Keep your workflow

Ardvi preserves your project instructions and custom skills. Repeating
`ardvi init` does not replace project-owned files. Commit the generated
integration files, including `.harness/.managed-state.json`, with your project.

Messages arrive through client hooks. A native startup is already registered by
the hook; the agent must call `context_bootstrap(session_id)` before substantive
work and after a resume or context clear/compact. `lets-go` is optional.
See the [agent protocol](.harness/docs/agent-protocol.md) for the model-facing contract.

Ardvi's service stores messages and memory locally. Project memory is private;
global memory is published explicitly. The MCP endpoint binds to loopback and
must not be exposed to a network. Your AI provider's data handling still applies
to content agents read.

## Everyday commands

```bash
ardvi service status   # Check the shared service
ardvi skills list      # Browse installed skills
ardvi update           # Update the shared service and skill catalogs
make update            # Refresh this project's managed harness files
```

For a host CLI upgrade, rerun `install.sh` from the new release archive, then
refresh each project's managed integration. `ardvi update` updates the shared
service and catalog; it does not replace the host executable. Back up the data
volume before moving from a pre-Fabric release. The next native hook replaces
an outdated Codex bridge for the same thread.
For the v0.4.0 upgrade, restart native clients to refresh cached MCP output
schemas and background watchers. An older open client can reject the new
bootstrap response even while the service and message transport are healthy.

Before ending a session, ask the agent:

```text
Use session-end and leave a concise handoff.
```

The [user guide](docs/guide.md) covers setup details, updates, and troubleshooting.
For the included skill sources, see
[shared skills](docs/guide.md#skills).

MIT licensed. See [LICENSE](LICENSE).
