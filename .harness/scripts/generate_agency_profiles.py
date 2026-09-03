#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile


EXCLUDED_DIRS = {
    ".git",
    ".github",
    "scripts",
    "docs",
    "integrations",
    "node_modules",
}


def split_frontmatter(text: str) -> tuple[dict[str, str], str]:
    if not text.startswith("---\n"):
        return {}, text
    end = text.find("\n---\n", 4)
    if end < 0:
        return {}, text
    raw = text[4:end]
    metadata: dict[str, str] = {}
    for line in raw.splitlines():
        match = re.match(r"^([A-Za-z_][A-Za-z0-9_-]*):\s*(.*)$", line)
        if not match:
            continue
        value = match.group(2).strip().strip('"\'')
        metadata[match.group(1)] = value
    return metadata, text[end + 5 :]


def safe_name(stem: str) -> str:
    value = re.sub(r"[^a-z0-9]+", "-", stem.lower()).strip("-")
    return f"agency-{value}"


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: generate_agency_profiles.py SOURCE OUTPUT", file=sys.stderr)
        return 2
    source = pathlib.Path(sys.argv[1]).resolve()
    output = pathlib.Path(sys.argv[2]).resolve()
    if not (source / ".git").is_dir():
        print(f"Agency Agents checkout is invalid: {source}", file=sys.stderr)
        return 1

    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = pathlib.Path(tempfile.mkdtemp(prefix=".agency-cao-", dir=output.parent))
    count = 0
    try:
        for division in sorted(source.iterdir()):
            if not division.is_dir() or division.name in EXCLUDED_DIRS or division.name.startswith("."):
                continue
            for agent_file in sorted(division.glob("*.md")):
                metadata, body = split_frontmatter(agent_file.read_text(encoding="utf-8"))
                name = safe_name(agent_file.stem)
                title = metadata.get("name") or agent_file.stem.replace("-", " ").title()
                description = metadata.get("description") or f"Agency Agents persona: {title}"
                description = re.sub(r"\s+", " ", description).strip()
                capability = description[:128]
                profile = (
                    "---\n"
                    f"name: {name}\n"
                    f"description: {json.dumps(description)}\n"
                    "role: developer\n"
                    "tags:\n"
                    "  - agency-agents\n"
                    f"  - {division.name}\n"
                    "capabilities:\n"
                    f"  - {json.dumps(capability)}\n"
                    "---\n\n"
                    f"# Agency Agents persona: {title}\n\n"
                    "Apply repository instructions, accepted ADRs, and approved specifications before this persona. "
                    "Return evidence and verification results to the CAO supervisor.\n\n"
                    f"{body.lstrip()}"
                )
                (temporary / f"{name}.md").write_text(profile, encoding="utf-8")
                count += 1

        if count == 0:
            raise RuntimeError("No Agency Agents Markdown profiles were found")

        commit = subprocess.check_output(
            ["git", "-C", str(source), "rev-parse", "HEAD"], text=True
        ).strip()
        (temporary / ".source-commit").write_text(commit + "\n", encoding="utf-8")

        previous = output.with_name(f".{output.name}.previous")
        if previous.exists():
            shutil.rmtree(previous)
        if output.exists():
            os.replace(output, previous)
        os.replace(temporary, output)
        if previous.exists():
            shutil.rmtree(previous)
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)

    print(f"Generated {count} CAO profiles from Agency Agents")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
