#!/usr/bin/env python3
"""Install small native skill entry points without touching custom skills."""

from __future__ import annotations

import hashlib
import os
import pathlib
import shutil
import sys


def tree_hash(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    for file in sorted(path.rglob("*")):
        if file.is_symlink():
            raise RuntimeError(f"managed skill must not contain symlinks: {file}")
        if not file.is_file():
            continue
        digest.update(str(file.relative_to(path)).encode())
        digest.update(file.read_bytes())
    return digest.hexdigest()


def install(source: pathlib.Path, target: pathlib.Path) -> str:
    state = target / ".managed-sha256"
    wanted = tree_hash(source)
    if target.is_symlink():
        raise RuntimeError(f"refusing to replace symlinked skill: {target}")
    if target.exists():
        if not state.exists():
            raise RuntimeError(f"refusing to replace unmanaged skill: {target}")
        recorded = state.read_text().strip()
        digest = hashlib.sha256()
        for file in sorted(p for p in target.rglob("*") if p.is_file() and p != state):
            digest.update(str(file.relative_to(target)).encode())
            digest.update(file.read_bytes())
        if digest.hexdigest() != recorded:
            raise RuntimeError(f"refusing to overwrite modified managed skill: {target}")
        if recorded == wanted:
            return "current"
        shutil.rmtree(target)
    shutil.copytree(source, target)
    state.write_text(wanted + "\n")
    return "installed"


def main() -> int:
    harness = pathlib.Path(__file__).resolve().parent.parent
    root = pathlib.Path(os.environ.get("HARNESS_REPO_ROOT", harness.parent)).resolve()
    try:
        for source in sorted((harness / "skills").iterdir()):
            if not (source / "SKILL.md").is_file():
                continue
            for base in (root / ".agents/skills", root / ".claude/skills"):
                print(f"Native skill {install(source, base / source.name)}: {(base / source.name).relative_to(root)}")
    except (OSError, RuntimeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
