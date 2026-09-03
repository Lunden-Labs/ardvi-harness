# Harness maintenance contract

## Scope

Maintain this repository's `.harness/`, root bootstrap files, and documentation.
Do not modify a target repository while working here.

## Constraints

- Keep the harness self-contained, portable, and idempotent when copied into an
  arbitrary Git repository.
- Do not add secrets, absolute local paths, symlinks, submodules, filesystem
  scans outside the target repository, or target-specific policy.
- Preserve existing target files: bootstrap must not overwrite them.
- Keep functional changes small; update active files and matching templates
  together.

## Verification

Before a commit, run `bash -n .harness/scripts/*.sh`, a dry Make target such as
`make -n help`, and inspect the diff for secrets and local paths. For template
changes, verify rendered and template content agree. `make doctor` is optional
state-changing integration validation: it runs `cao init` and CAO registration,
so use it only intentionally.

## Periodic improvement

1. Audit current behavior and a focused maintenance concern before editing.
2. Make one small reviewable improvement, with the narrowest relevant check.
3. Review portability, idempotency, secrets, and global-path coupling.
4. Record unresolved follow-up separately; do not bundle it into the change.
