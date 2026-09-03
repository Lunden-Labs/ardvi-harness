# Project harness bootstrap

## Recommended

From this harness repository, copy into an existing Git repository root:

```bash
make copy
```

Enter the target when prompted, or use `make copy TARGET=/path/to/repository`
for noninteractive automation. It never overwrites `.harness` or `Makefile`.
A missing Makefile receives short commands; an existing Makefile receives
namespaced commands such as `make harness-init` and `make harness-up`.

## Manual: repository without a Makefile

Copy both items into the repository root:

```bash
cp -a ardvi-harness/.harness /path/to/repository/
cp ardvi-harness/Makefile /path/to/repository/Makefile
```

## Manual: repository with an existing Makefile

Copy `.harness` and add one line to the existing root `Makefile`:

```make
include .harness/harness.mk
```

## Commands

```bash
cd /path/to/repository

make init # or: make harness-init with an existing Makefile
make up # or: make harness-up with an existing Makefile
```

Later:

```bash
make update
make down
```

The harness discovers the Git repository root automatically. No path or project-name variable is required.
