#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import pathlib
import shutil
import subprocess
import sys


HARNESS_DIR = pathlib.Path(__file__).resolve().parent.parent
try:
    REPO_ROOT = pathlib.Path(
        subprocess.check_output(
            ["git", "-C", str(HARNESS_DIR), "rev-parse", "--show-toplevel"],
            text=True,
            stderr=subprocess.DEVNULL,
        ).strip()
    )
except (subprocess.CalledProcessError, FileNotFoundError):
    REPO_ROOT = HARNESS_DIR.parent
AGENT_DIR = str((REPO_ROOT / ".cao" / "agents").resolve())
SKILL_DIR = str((REPO_ROOT / ".cao" / "skills").resolve())
HARNESS_DATA_DIR = pathlib.Path(
    os.environ.get(
        "PROJECT_HARNESS_DATA_DIR",
        str(pathlib.Path.home() / ".local" / "share" / "project-harness"),
    )
).expanduser().resolve()
UPSTREAMS_DIR = HARNESS_DATA_DIR / "upstreams"
AGENCY_CAO_DIR = HARNESS_DATA_DIR / "generated" / "agency-agents-cao"


def run(*args: str) -> str:
    completed = subprocess.run(
        args,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return completed.stdout.strip()


def parse_json_output(raw: str):
    raw = raw.strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        pass
    for line in reversed(raw.splitlines()):
        line = line.strip()
        if not line:
            continue
        try:
            return json.loads(line)
        except json.JSONDecodeError:
            continue
    raise ValueError(f"CAO returned a non-JSON value: {raw!r}")


def merge_dirs(
    key: str, paths: list[str], *, remove_legacy_harness_skills: bool = False
) -> tuple[list[str], list[str]]:
    current = parse_json_output(run("cao", "config", "get", key))
    if current is None:
        current = []
    if not isinstance(current, list) or not all(isinstance(item, str) for item in current):
        raise TypeError(f"{key} is not a string array: {current!r}")

    removed = []
    retained = []
    for item in current:
        resolved = pathlib.Path(item).expanduser().resolve()
        if remove_legacy_harness_skills and resolved.parts[-2:] == (".harness", "skills"):
            removed.append(item)
        else:
            retained.append(item)

    normalized = {str(pathlib.Path(item).expanduser().resolve()) for item in retained}
    additions = [path for path in paths if path not in normalized]
    if additions or removed:
        run("cao", "config", "set", key, json.dumps([*retained, *additions]))
    return additions, removed


def main() -> int:
    if shutil.which("cao") is None:
        print("CAO is not installed; cannot register project directories", file=sys.stderr)
        return 1
    subprocess.run(["cao", "init"], check=True, stdout=subprocess.DEVNULL)
    agent_dirs = [
        str(path)
        for path in (pathlib.Path(AGENT_DIR), AGENCY_CAO_DIR)
        if path.is_dir()
    ]
    skill_dirs = [
        str(path)
        for path in (
            pathlib.Path(SKILL_DIR),
            HARNESS_DATA_DIR / "skills",
            UPSTREAMS_DIR / "agent-skills" / "skills",
            UPSTREAMS_DIR / "ponytail" / "skills",
            UPSTREAMS_DIR / "writing-skills" / "for-agents",
        )
        if path.is_dir()
    ]
    if not agent_dirs and not skill_dirs:
        print("No project or external CAO directories exist yet", file=sys.stderr)
        return 1

    added_agents, _ = merge_dirs("agents.extra_dirs", agent_dirs)
    added_skills, removed_skills = merge_dirs(
        "skills.extra_dirs", skill_dirs, remove_legacy_harness_skills=True
    )
    for path in agent_dirs:
        print(f"CAO agents: {'registered' if path in added_agents else 'already registered'}: {path}")
    for path in skill_dirs:
        print(f"CAO skills: {'registered' if path in added_skills else 'already registered'}: {path}")
    for path in removed_skills:
        print(f"CAO skills: removed legacy harness registration: {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
