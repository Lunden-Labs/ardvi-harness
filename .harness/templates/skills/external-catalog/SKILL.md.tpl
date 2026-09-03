---
name: __PROJECT_SLUG__-external-catalog
description: Routes __PROJECT_SLUG__ work through current engineering, specialist, writing, and complexity-control skills
---

# External engineering catalog

These sources are installed and updated by the project harness:

- Addy Osmani `agent-skills`: engineering lifecycle processes;
- Agency Agents: specialist role profiles exposed to CAO as `agency-*`;
- Ponytail: minimal implementation and over-engineering review skills.
- `msimchowitz/writing-skills`: `writing` router and its complete lazy-loaded
  `for-agents/` catalog.

For every non-trivial task:

1. Load `using-agent-skills` and select only the Addy skills relevant to the current phase.
2. Use `spec-driven-development` when required behavior is not already specified.
3. Use `planning-and-task-breakdown` before multi-component implementation.
4. Use `test-driven-development` or the repository's stronger testing contract during implementation.
5. Use `code-review-and-quality` before acceptance.
6. Apply the relevant Ponytail skill before implementation and use `ponytail-review` as an additional complexity review. Ponytail never replaces correctness, security, or performance review.
7. Search CAO profiles for an `agency-*` specialist when domain expertise materially improves the task. Repository instructions, accepted ADRs, and approved specifications override persona guidance.
8. Use `communication` for user-facing output. Route durable prose through
   `writing`; use `humanizer` only when explicitly requested or selected by the
   writing pipeline. Treat any separately installed `stop-slop` skill as an
   optional audit/debug tool, never a default pass.

Skills are loaded lazily. Do not load the entire catalog into context at once.
