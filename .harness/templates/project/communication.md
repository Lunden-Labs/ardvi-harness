For all user-facing communication, read and follow `.harness/skills/communication/SKILL.md`.
For durable prose such as READMEs, documentation, reports, architecture documents,
design documents, RFCs, ADRs, proposals, and runbooks, route the task through the
registered `writing` skill. If the client cannot resolve it, run `make
harness-skill-path SKILL=writing` and read the returned `SKILL.md`. Use the full
writing pipeline only for durable prose or explicit editing, preserve exact
technical tokens and factual qualifications, and never invent facts to improve
prose. Match the user's language unless the artifact has an explicit language
requirement.
