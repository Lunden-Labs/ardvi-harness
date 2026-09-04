#!/usr/bin/env python3
"""Build the local, lazy skill/persona catalog from managed roots."""

from __future__ import annotations

import json
import os
import pathlib
import tempfile


_BLOCK_SCALARS = {">", ">-", ">+", "|", "|-", "|+"}


def frontmatter(path: pathlib.Path) -> tuple[str, str]:
    text = path.read_text(encoding="utf-8", errors="replace")
    name, description = path.parent.name, ""
    if text.startswith("---\n"):
        lines = text.split("\n")
        i = 1
        while i < len(lines) and lines[i] != "---":
            key, _, value = lines[i].partition(":")
            value = value.strip()
            if key == "name" and value:
                name = value.strip('"\'')
            elif key == "description":
                if value in _BLOCK_SCALARS:
                    block: list[str] = []
                    i += 1
                    while i < len(lines) and lines[i] != "---" and (lines[i] == "" or lines[i][:1] in (" ", "\t")):
                        block.append(lines[i])
                        i += 1
                    indents = [len(b) - len(b.lstrip(" ")) for b in block if b.strip()]
                    common = min(indents, default=0)
                    joiner = "\n" if value[0] == "|" else " "
                    description = joiner.join(b[common:] for b in block).strip()
                    continue
                description = value.strip('"\'')
            i += 1
    return name, description


def main() -> int:
    harness = pathlib.Path(__file__).resolve().parent.parent
    data = pathlib.Path(os.environ.get("ARDVI_CATALOG_DATA_DIR", pathlib.Path.home() / ".local/share/ardvi/catalog"))
    scan_upstreams = pathlib.Path(os.environ.get("HARNESS_CATALOG_SCAN_UPSTREAMS", data / "upstreams"))
    root_upstreams = pathlib.Path(os.environ.get("HARNESS_CATALOG_ROOT_UPSTREAMS", data / "upstreams"))
    skills: list[dict[str, str]] = []
    # Harness skills ship from this repo's checkout, not an upstream clone: scanning
    # happens at build time under `data` (e.g. /rootfs/opt/ardvi during image build),
    # but the published root must be the *runtime* path the server will read from
    # (e.g. /opt/ardvi). root_upstreams already carries that build-vs-runtime split
    # via HARNESS_CATALOG_ROOT_UPSTREAMS, so derive the harness root from it instead
    # of re-deriving another env var.
    harness_root = root_upstreams.parent / "skills"
    roots = [
        ("harness", data / "skills", harness_root, harness_root),
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
