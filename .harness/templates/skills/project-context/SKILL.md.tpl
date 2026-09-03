---
name: __PROJECT_SLUG__-project-context
description: Locates and applies repository instructions, accepted ADRs, approved specifications, durable tasks, and verification requirements for __PROJECT_SLUG__
---

# Project context protocol

Before making a plan or changing code:

1. Read root `AGENTS.md` and `CLAUDE.md` when present, then load `communication` for user-facing output.
2. Locate ADR directories case-insensitively under the repository root and `docs/`. Read every ADR relevant to the task and record its status.
3. Locate spec/specification directories under the repository root and `docs/`. Read relevant approved specifications.
4. Read task briefs and architecture documents that constrain the affected area.
5. Inspect current code and tests to determine whether implementation matches documented intent.

Precedence:

1. accepted ADRs;
2. approved specifications;
3. repository instructions and architecture documents;
4. code and tests.

If these conflict, stop implementation and report the exact conflict to the supervisor.

When required behavior has no specification, create a Proposed/Draft spec before implementation. When a durable architectural decision is missing, create a Proposed ADR. Never mark an ADR Accepted without explicit authorization.

For completion, provide changed files, test commands and results, acceptance evidence, unresolved risks, and follow-up tasks.
