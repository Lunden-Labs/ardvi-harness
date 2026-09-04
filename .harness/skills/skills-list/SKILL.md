---
name: skills-list
description: List the skills currently installed on the shared Ardvi MCP server, including their source and pinned upstream revision. Use when the user asks what skills are available, installed, or updatable.
---

# Installed skills

Call the Ardvi MCP tool `skills_list`. Group the result by `source`, show the
skill names, and report each source revision. Do not load every skill body.

Use `skills_search` to narrow a large catalog and `skill_read` only when the
user asks for a skill's instructions or when that skill is needed for the task.

Explain that harness entry skills are copied into the project for native client
discovery, while the full managed catalog is stored and served by Ardvi MCP.
