# Changelog

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
