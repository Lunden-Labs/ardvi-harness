#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import os
import pathlib
import re
import stat
import sys
import tempfile


START_RE = re.compile(
    r"^<!-- project-harness:communication sha256=([0-9a-f]{64}) -->$",
    re.MULTILINE,
)
END = "<!-- /project-harness:communication -->"


def digest(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def managed_block(body: str) -> str:
    body = body.rstrip() + "\n"
    return (
        f"<!-- project-harness:communication sha256={digest(body)} -->\n"
        f"{body}{END}\n"
    )


def plan(path: pathlib.Path, body: str, initial: str) -> tuple[str, str]:
    if path.is_symlink():
        raise RuntimeError(f"instruction target must be a regular file: {path}")
    if not path.exists():
        return "created", initial.rstrip() + "\n\n" + managed_block(body)
    if not path.is_file():
        raise RuntimeError(f"instruction target must be a regular file: {path}")

    original = path.read_text(encoding="utf-8")
    starts = list(START_RE.finditer(original))
    ends = [match.start() for match in re.finditer(re.escape(END), original)]
    if not starts and not ends:
        separator = "" if not original or original.endswith("\n\n") else "\n"
        return "added", original + separator + managed_block(body)
    if len(starts) != 1 or len(ends) != 1 or ends[0] <= starts[0].end():
        raise RuntimeError(f"invalid managed communication block: {path}")

    start = starts[0]
    actual_body = original[start.end() + 1 : ends[0]]
    if digest(actual_body) != start.group(1):
        raise RuntimeError(
            f"refusing to overwrite locally modified managed block: {path}"
        )

    replacement = managed_block(body)
    old_block_end = ends[0] + len(END)
    if old_block_end < len(original) and original[old_block_end] == "\n":
        old_block_end += 1
    updated = original[: start.start()] + replacement + original[old_block_end:]
    return ("current" if updated == original else "updated"), updated


def write_atomic(path: pathlib.Path, content: str) -> None:
    mode = stat.S_IMODE(path.stat().st_mode) if path.exists() else 0o644
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", dir=path.parent, delete=False
    ) as handle:
        handle.write(content)
        temporary = pathlib.Path(handle.name)
    os.chmod(temporary, mode)
    os.replace(temporary, path)


def main() -> int:
    harness = pathlib.Path(__file__).resolve().parent.parent
    repository = pathlib.Path(
        os.environ.get("HARNESS_REPO_ROOT", harness.parent)
    ).resolve()
    targets = [repository / "AGENTS.md", repository / "CLAUDE.md"]

    try:
        changes = [
            (
                path,
                *plan(
                    path,
                    (
                        (harness / "templates/project/communication.md").read_text(encoding="utf-8")
                        if path.name == "AGENTS.md"
                        else "@AGENTS.md\n\nClaude Code uses the same project policy and the `ardvi` MCP server from `.mcp.json`.\n"
                    ),
                    (harness / "templates/project" / path.name).read_text(
                        encoding="utf-8"
                    ),
                ),
            )
            for path in targets
        ]
    except (OSError, RuntimeError, UnicodeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1

    for path, status, content in changes:
        if status != "current":
            write_atomic(path, content)
        print(f"Instructions {status}: {path.relative_to(repository)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
