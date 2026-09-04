# Changelog

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
