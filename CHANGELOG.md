# Changelog

## [0.2.1] - 2026-09-05

### Added

- `.harness/LICENSE`, a verbatim copy of the repository's MIT `LICENSE`, so the license notice travels with every copied `.harness/` directory. It is picked up automatically as part of the managed file set (checksummed in `.managed-state.json`, verified by `manage_harness.py verify`); `prepare_image_skills.sh` and the Docker image are unaffected since neither copies `.harness/` wholesale.

### Fixed

- `.harness/.managed-state.json` was gitignored, so a fresh clone of a project carrying the harness had no managed state: `update_harness.sh` exited 1 with "Managed harness state is missing", and the suggested recovery (`make copy`) always failed too, since copy refuses an existing `.harness` directory. The state file is fully derived (commit, repository, revision, file checksums) and is now tracked and committed alongside `.harness/`; the "state missing" branch now tells a tracked-but-locally-deleted state file apart from a never-installed one and prints the exact command to fix each.
- `harness_update_integration_test.sh` copied the *local* `.harness/.managed-state.json` (if the checkout running the test happened to have one) into both of its fixtures, then rewrote each fixture's manifest, which broke checksum verification for reasons that had nothing to do with the code under test. The fixtures now start from a clean tree regardless of local state.

## [0.2.0] - 2026-09-04

### Added

- Native `SessionStart`/`UserPromptSubmit`/`SessionEnd` hooks for both Codex and Claude Code, installed and kept current by `project_config.py` in `.codex/hooks.json` and `.claude/settings.json` without disturbing any other tool's hook entries.
- `ardvi hook session-start|prompt|session-end --client claude|codex` and `ardvi inbox --session <id> [--follow]` commands on the host binary.
- `unread` piggyback on `message_send`, `message_ack`, and `claim_*` results, and a 16 KiB message body limit.

## [0.1.1] - 2026-09-04

### Fixed

- Harness skill catalog roots published the build-time staging path (e.g. `/rootfs/opt/ardvi/skills/...`) instead of the runtime image path, breaking `skill_read` for every `harness:*` skill after image build.
- `frontmatter()` read only single-line YAML scalars, turning a `description: >` or `description: |` block scalar into the literal indicator character and truncating frontmatter parsing past 40 lines.

## [0.1.0] - 2026-09-04

### Added

- Machine-wide Ardvi MCP service distributed as a hardened Docker image.
- Native Codex and Claude Code project configuration without a provider wrapper.
- Project-isolated messages, memory, sessions, and resource claims, with explicit global scope.
- Lazy managed catalogs for Agent Skills, Agency Agents personas, Ponytail, and Writing Skills.
- `communication`, `writing`, `lets-go`, `session-end`, `project-context`, and `skills-list` entry skills.
- Release archives for Linux and macOS on AMD64 and ARM64.

### Security

- Immutable image digests, pinned upstream revisions, loopback-only publishing, non-root execution, and read-only container filesystem.
- Conflict-safe updates for managed project files and protected project-owned skills.
