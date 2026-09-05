# Native agent hub implementation plan

Follow-on work is tracked in [SPEC-agent-fabric.md](SPEC-agent-fabric.md).

- [x] Core state and catalog
  - Acceptance: project isolation, persistence, claims, memory export, and safe
    skill reads pass focused Go tests.
  - Verify: `go test ./internal/store ./internal/catalog`

- [x] MCP transport and tools
  - Acceptance: the documented tools are listed and callable over Streamable
    HTTP; project identity comes from the connection header.
  - Verify: `go test ./...` and HTTP integration smoke test.

- [x] Safe project bootstrap
  - Acceptance: empty and existing repositories receive project identity,
    project-scoped Codex/Claude MCP entries, shared instructions, and entry
    skills without overwriting local content.
  - Verify: bootstrap integration tests and a second byte-identical init.

- [x] Managed skills and updates
  - Acceptance: every declared upstream and harness skill is installed at init,
    catalogued with its revision, validated before activation, and updated
    without modifying project-owned skills.
  - Verify: writing/catalog/update integration tests with local Git fixtures.

- [x] Lifecycle, memory portability, and documentation
  - Acceptance: `up`, `down`, `status`, `doctor`, memory export/import, native
    client quick starts, `lets-go`, and `session-end` work without CAO/tmux.
  - Verify: all shell tests, Go race tests, shellcheck, and clean temp-repo smoke.

- [x] Removal and independent review
  - Acceptance: no executable/doc/template/test references to CAO, tmux, Agent
    Mail, fixed roles, or provider launch wrappers remain; reviewer finds no
    blocking correctness/security issue.
  - Verify: repository search, diff review, full suite.

- [x] Machine-wide release packaging
  - Acceptance: one digest-pinned Compose service serves all project UUIDs;
    release images contain complete pinned catalogs; host archives initialize
    projects without a Go toolchain; `skills_list` and CLI enumeration agree.
  - Verify: lifecycle/unit tests, image build, release manifest validation, and
    temporary project integration tests.
