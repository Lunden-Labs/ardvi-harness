# ARDVI Harness

Portable CAO bootstrap for ARDVI repositories. It copies a local `.harness/`
directory into a target repository; it is not a runtime dependency or a shared
submodule.

## Install in a target repository

```bash
cp -a ardvi-harness/.harness /path/to/repository/
cp ardvi-harness/Makefile /path/to/repository/Makefile
cd /path/to/repository
make init
```

If the target already has a `Makefile`, copy `.harness/` and add:

```make
include .harness/harness.mk
```

## Commands

`make init` bootstraps the project and CAO; `make update` updates harness
dependencies; `make up`, `make down`, `make status`, `make architect`, and
`make doctor` manage or inspect CAO. See [COPY-INTO-REPO.md](COPY-INTO-REPO.md)
and [.harness/README.md](.harness/README.md) for details.

`make doctor` validates dependencies and project files, then runs CAO
registration (`cao init` and configuration updates). Run it intentionally: it
may change the active local CAO configuration.

## Periodic maintenance audit

Run this interactively from any checkout, replacing the focused prompt as
needed:

```bash
codex -C /path/to/ardvi-harness 'Audit the harness before editing. Propose and implement only one small, reviewable portability or safety improvement. Run the narrowest relevant checks. Do not add secrets, absolute local paths, global-path coupling, or unrelated refactors.'
```

Review the diff before committing. The harness must remain self-contained and
safe to copy into an arbitrary ARDVI repository.
