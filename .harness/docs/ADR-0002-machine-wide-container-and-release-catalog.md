# ADR-0002: Machine-wide container and release catalog

- **Status:** Accepted by user direction
- **Date:** 2026-09-04

## Context

Several repositories on one computer need the same MCP channel. Starting a
host daemon or installing complete upstream skill trees from each repository
duplicates state, creates port/lifecycle conflicts, and makes versions unclear.
End users also need a release installation that does not require a Go toolchain.

## Decision

Ship one `ardvi` host binary and one multi-platform MCP image per release. The
binary resolves a GitHub release manifest, requires an immutable image digest,
and manages a fixed Compose project named `ardvi`. Compose publishes only
`127.0.0.1:8765` and stores mutable state in the named volume `ardvi-data`.

Complete managed skill and persona trees are built into the read-only image at
exact commits recorded in `upstreams.lock.tsv`. Projects receive only native
entry skills, instructions, client configuration, and a stable UUID. Project
custom skills remain project-owned.

## Consequences

- `make up` in any project is idempotent and addresses the same service.
- `make down` is machine-wide and must say so explicitly.
- Updating the release image updates the MCP implementation and all managed
  catalogs together; every running project sees the same revisions.
- The catalog is reproducible and available without runtime Git/network access.
- Project namespaces isolate normal traffic, but the UUID header is not an
  authentication boundary; remote exposure remains unsupported.
