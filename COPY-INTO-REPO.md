# Project harness bootstrap

## Empty repository or repository without a Makefile

Copy both items into the repository root:

```bash
cp -a ardvi-harness/.harness /path/to/repository/
cp ardvi-harness/Makefile /path/to/repository/Makefile
```

## Repository with an existing Makefile

Copy `.harness` and add one line to the existing root `Makefile`:

```make
include .harness/harness.mk
```

## Commands

```bash
cd /path/to/repository

make init
make up
```

Later:

```bash
make update
make down
```

The harness discovers the Git repository root automatically. No path or project-name variable is required.
