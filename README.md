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
`make doctor` manage or inspect CAO. `make improve` starts an interactive
Codex maintenance pass from the repository root. See [COPY-INTO-REPO.md](COPY-INTO-REPO.md)
and [.harness/README.md](.harness/README.md) for details.

`make doctor` validates dependencies and project files, then runs CAO
registration (`cao init` and configuration updates). Run it intentionally: it
may change the active local CAO configuration.

## Periodic maintenance audit

Run this interactively from the harness repository or a copied target:

```bash
make improve
```

It reads the repository instructions and harness documentation, analyzes first,
then makes at most one focused portability or safety improvement. Review the
diff before committing. It does not touch product code or commit or push
automatically. The harness must remain self-contained and safe to copy into an
arbitrary ARDVI repository.
