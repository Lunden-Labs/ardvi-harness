# Unified Ardvi update

Status: implementation scope for the user's request to update with one command.

## Outcome

`ardvi update` installs one published release of the host CLI and bundled
integration, updates the MCP service through the existing installer, and
refreshes the current initialized Git project's integration. Outside a project
it updates only the host. `make update` delegates to it; `ardvi skills update`
retains its service/catalog-only behavior.

The updater uses the release manifest's platform archive and SHA-256, validates
the archive before executing it, and reuses `install.sh` for host promotion and
service health checks. It never replaces this repository's source checkout.
Other projects are not scanned or modified. No dependencies or server schema
changes are needed.

## Preservation and failure behavior

- Project UUID, settings, owned instructions and foreign hooks survive updates.
  Existing copied-harness conflicts fail before host installation. An explicit
  `--replace-harness` backs up a modified copied `.harness` before replacement;
  it never authorizes overwriting project-owned files or source checkouts.
- Preserve an already-created project backup if later bootstrap fails; report
  the completed host update and failed project refresh as a partial failure.
  Retrying the command is safe. Do not roll the server back across migrations.
- Reject invalid checksums, unsupported platforms, links, archive traversal,
  special files and oversized archives before running release code.
- Serialize host updates with a host-local lock. Preserve custom installation
  paths and the existing `--manifest`, `--config-dir` and `--no-start` options.
- Existing CLIs require a one-time bootstrap command. A root `upgrade.sh`
  invokes the same Python updater with standard Python 3.10+, avoiding a second
  implementation of download, checksum and install logic.

## Implementation and verification

Use a Python standard-library updater in `.harness/scripts/update_release.py`,
called by the Go host CLI; reuse `manage_harness.py` checks and bootstrap merge
scripts. Follow existing subprocess argument lists and atomic directory swaps.
For example, invoke `subprocess.run(["bash", installer, "--manifest", manifest],
check=True)` without a shell command string.

Implement download/validation and host installation first, then project refresh,
CLI/Make wiring and the one-time bootstrap. Each slice needs a runnable check.
Go tests remain under `.harness/mcp`; Python and shell integration fixtures live
under `.harness/tests` and use temporary releases, projects and fake Docker.

Required checks: `go -C .harness/mcp test -race ./...`,
`go -C .harness/mcp vet ./...`, `go -C .harness/mcp build ./...`,
`bash -n install.sh upgrade.sh .harness/scripts/*.sh`, `make -n help`, ShellCheck,
and all `.harness/tests/*_test.sh` fixtures. Cover checksums and archive attacks,
failed install, modified project refusal/replacement backup, repeated updates,
custom paths, source checkout protection and a preserved handover opt-in.

Do not modify another real project for testing, discard user changes or add
credentials. Source updates and publication follow the normal Git/CI release
flow. Record any unimplemented follow-up separately.
