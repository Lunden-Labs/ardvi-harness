# Repository agent contract

## Source of truth

Use this precedence order:

1. accepted ADRs;
2. approved specifications;
3. repository instructions and architecture documentation;
4. current implementation and tests.

When sources conflict, stop and report the conflict. Do not silently reinterpret an accepted ADR.

## Change workflow

Before implementation:

1. read the relevant ADRs and specifications;
2. inspect the affected code and tests;
3. state assumptions and acceptance criteria;
4. create a proposed spec when required behavior is undocumented;
5. create a proposed ADR when the change introduces a durable architectural decision.

Only a human or an explicitly authorized architecture owner may mark an ADR as Accepted.

## Delivery rules

- Keep changes scoped to the assigned task.
- Preserve unrelated user changes.
- Add or update tests for behavior changes.
- Run the narrowest relevant checks, then the broader project checks.
- Record unresolved risks and follow-up work.
- One writable parallel worker must use one branch and one worktree.
- A reviewer must be independent from the producer when practical.
