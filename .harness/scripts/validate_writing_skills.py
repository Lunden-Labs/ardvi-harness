#!/usr/bin/env python3
from __future__ import annotations

import pathlib
import re
import sys
from urllib.parse import unquote


REQUIRED = {
    "writing",
    "general-writing",
    "humanizer",
    "writing-cadence",
    "better-usage",
    "non-autoregressive-writing-pass",
    "academic-voice",
}
LINK_RE = re.compile(r"!?(?:\[[^\]]*\])\(([^)]+)\)")
SKILL_REF_RE = re.compile(r"\.\./([a-z0-9][a-z0-9-]*)/SKILL\.md")
CODE_PATH_RE = re.compile(r"`((?:\.\.?/)[^`\s]+)`")


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: validate_writing_skills.py FOR_AGENTS_DIR", file=sys.stderr)
        return 2

    root = pathlib.Path(sys.argv[1]).resolve()
    missing = sorted(name for name in REQUIRED if not (root / name / "SKILL.md").is_file())
    if missing:
        print("Missing required writing skills: " + ", ".join(missing), file=sys.stderr)
        return 1

    failures: list[str] = []
    for document in root.rglob("*.md"):
        text = document.read_text(encoding="utf-8")
        candidates = [match.group(1) for match in LINK_RE.finditer(text)]
        candidates += [f"../{name}/SKILL.md" for name in SKILL_REF_RE.findall(text)]
        candidates += CODE_PATH_RE.findall(text)
        for raw in candidates:
            raw = raw.strip().strip("<>").split("#", 1)[0]
            if not raw or "://" in raw or raw.startswith(("#", "mailto:")):
                continue
            target = (document.parent / unquote(raw)).resolve()
            try:
                target.relative_to(root.parent)
            except ValueError:
                failures.append(f"{document}: reference escapes checkout: {raw}")
                continue
            if not target.exists():
                failures.append(f"{document}: missing reference: {raw}")

    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print(f"Writing skills validated: {root}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
