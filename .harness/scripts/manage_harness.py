#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import pathlib
import sys


STATE_NAME = ".managed-state.json"


def files(root: pathlib.Path) -> dict[str, str]:
    result = {}
    for path in sorted(root.rglob("*")):
        relative = path.relative_to(root)
        if relative.name == STATE_NAME or "__pycache__" in relative.parts:
            continue
        if path.is_symlink():
            raise RuntimeError(f"managed harness contains a symlink: {relative}")
        if path.is_file() and path.suffix != ".pyc":
            result[str(relative)] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def load(root: pathlib.Path) -> dict:
    state = root / STATE_NAME
    if not state.is_file():
        raise RuntimeError(f"managed harness state is missing: {state}")
    return json.loads(state.read_text(encoding="utf-8"))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("record", "verify", "show"))
    parser.add_argument("root", type=pathlib.Path)
    parser.add_argument("values", nargs="*")
    args = parser.parse_args()
    root = args.root.resolve()

    try:
        if args.action == "record":
            if len(args.values) != 3:
                parser.error("record requires REPOSITORY REVISION COMMIT")
            repository, revision, commit = args.values
            state = {
                "repository": repository,
                "revision": revision,
                "commit": commit,
                "files": files(root),
            }
            (root / STATE_NAME).write_text(
                json.dumps(state, indent=2, sort_keys=True) + "\n", encoding="utf-8"
            )
            return 0

        state = load(root)
        if args.action == "show":
            if len(args.values) != 1 or args.values[0] not in {
                "repository",
                "revision",
                "commit",
            }:
                parser.error("show requires repository, revision, or commit")
            print(state[args.values[0]])
            return 0

        if args.values:
            parser.error("verify accepts no extra values")
        actual = files(root)
        if actual != state.get("files"):
            expected_paths = set(state.get("files", {}))
            actual_paths = set(actual)
            changed = sorted(
                path
                for path in expected_paths & actual_paths
                if state["files"][path] != actual[path]
            )
            details = []
            if changed:
                details.append("changed: " + ", ".join(changed))
            if expected_paths - actual_paths:
                details.append("missing: " + ", ".join(sorted(expected_paths - actual_paths)))
            if actual_paths - expected_paths:
                details.append("unmanaged: " + ", ".join(sorted(actual_paths - expected_paths)))
            raise RuntimeError("refusing to overwrite modified harness (" + "; ".join(details) + ")")
        print(f"Managed harness verified: {state['commit']}")
        return 0
    except (OSError, KeyError, ValueError, RuntimeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
