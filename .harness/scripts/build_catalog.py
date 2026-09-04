#!/usr/bin/env python3
"""Build the local, lazy skill/persona catalog from managed roots."""

from __future__ import annotations

import json
import os
import pathlib
import tempfile


def frontmatter(path: pathlib.Path) -> tuple[str, str]:
    text = path.read_text(encoding="utf-8", errors="replace")
    name, description = path.parent.name, ""
    if text.startswith("---\n"):
        for line in text.split("\n", 40)[1:]:
            if line == "---":
                break
            key, _, value = line.partition(":")
            if key == "name" and value.strip():
                name = value.strip().strip('"\'')
            elif key == "description":
                description = value.strip().strip('"\'')
    return name, description


def main() -> int:
    harness = pathlib.Path(__file__).resolve().parent.parent
    data = pathlib.Path(os.environ.get("ARDVI_CATALOG_DATA_DIR", pathlib.Path.home() / ".local/share/ardvi/catalog"))
    scan_upstreams = pathlib.Path(os.environ.get("HARNESS_CATALOG_SCAN_UPSTREAMS", data / "upstreams"))
    root_upstreams = pathlib.Path(os.environ.get("HARNESS_CATALOG_ROOT_UPSTREAMS", data / "upstreams"))
    skills: list[dict[str, str]] = []
    roots = [
        ("harness", data / "skills", data / "skills", data / "skills"),
        ("agent-skills", scan_upstreams / "agent-skills/skills", root_upstreams / "agent-skills/skills", root_upstreams / "agent-skills"),
        ("ponytail", scan_upstreams / "ponytail/skills", root_upstreams / "ponytail/skills", root_upstreams / "ponytail"),
        ("writing-skills", scan_upstreams / "writing-skills/for-agents", root_upstreams / "writing-skills/for-agents", root_upstreams / "writing-skills"),
    ]
    for source, scan_root, published_root, boundary in roots:
        if not scan_root.is_dir():
            raise SystemExit(f"missing managed skill root: {scan_root}")
        for entry in sorted(scan_root.rglob("SKILL.md")):
            name, description = frontmatter(entry)
            relative = entry.parent.relative_to(scan_root)
            skills.append({"name": name, "description": description, "source": source, "root": str(published_root / relative), "boundary": str(boundary), "entry": "SKILL.md"})

    personas: list[dict[str, str]] = []
    persona_scan = scan_upstreams / "agency-agents"
    persona_root = root_upstreams / "agency-agents"
    if not persona_scan.is_dir():
        raise SystemExit(f"missing managed persona root: {persona_scan}")
    ignored = {"README.md", "LICENSE.md", "CONTRIBUTING.md", "CODE_OF_CONDUCT.md"}
    for entry in sorted(persona_scan.glob("*/*.md")):
        if entry.name in ignored:
            continue
        text = entry.read_text(encoding="utf-8", errors="replace")
        title = next((line.removeprefix("# ").strip() for line in text.splitlines() if line.startswith("# ")), entry.stem)
        personas.append({"name": entry.stem, "description": title, "source": "agency-agents", "root": str(persona_root), "boundary": str(persona_root), "entry": str(entry.relative_to(persona_scan))})

    revisions: dict[str, str] = {}
    lock = pathlib.Path(os.environ.get("HARNESS_CATALOG_LOCK", data / "upstreams.lock.tsv"))
    if lock.exists():
        for line in lock.read_text().splitlines():
            if line and not line.startswith("#"):
                fields = line.split("\t")
                if len(fields) >= 4:
                    revisions[fields[0]] = fields[3]
    result = {"version": 1, "skills": skills, "personas": personas, "revisions": revisions}
    data.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=data, delete=False) as f:
        json.dump(result, f, indent=2)
        f.write("\n")
        temporary = pathlib.Path(f.name)
    output = pathlib.Path(os.environ.get("HARNESS_CATALOG_OUTPUT", data / "catalog.json"))
    output.parent.mkdir(parents=True, exist_ok=True)
    os.replace(temporary, output)
    print(f"Catalog: {len(skills)} skills, {len(personas)} personas")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
