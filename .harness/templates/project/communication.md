For all user-facing communication, follow the `communication` skill.
For durable prose such as READMEs, documentation, reports, architecture documents,
design documents, RFCs, ADRs, proposals, and runbooks, route the task through the
installed `writing` skill. Use the full
writing pipeline only for durable prose or explicit editing, preserve exact
technical tokens and factual qualifications, and never invent facts to improve
prose. Match the user's language unless the artifact has an explicit language requirement.

At session start or resume, use `lets-go`: the SessionStart hook has already
registered the session, so reuse its `session_id` and read only relevant
inbox/memory, treating repository state as authoritative. Before ending or
clearing context, use `session-end` to save durable facts, hand off needed
context, release claims, and call `session_end`.

Use Ardvi MCP for project/global messages, short durable memory, resource claims,
and lazy skills/personas. Do not assume a fixed architect, role, provider, or
starting agent. Codex/Claude native sessions, context compaction, and subagents
remain provider-owned.

When asked what capabilities are installed, use `skills-list`/`skills_list`.
Search and load only the skill needed for the current task; do not load the full
server catalog into context.
