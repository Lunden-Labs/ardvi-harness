# Add the harness to a repository

Release users should install Ardvi once, then run:

```bash
cd /path/to/git-repository
ardvi init
```

The release bundle calls the same repository copy target described below.

## From a source checkout

```bash
git clone https://github.com/Lunden-Labs/ardvi-harness.git
cd ardvi-harness
make copy TARGET=/path/to/git-repository
cd /path/to/git-repository
make init
```

The target must be a Git repository root. Copy refuses an existing `.harness`
directory and never overwrites a `Makefile`. When a Makefile already exists, it
adds only:

```make
include .harness/harness.mk
```

Use namespaced commands if the project's Makefile already owns short targets:

```bash
make harness-init
make harness-up
make harness-skills
make harness-update
```

`make down` and `make harness-down` stop the one machine-wide service used by
all initialized projects.
