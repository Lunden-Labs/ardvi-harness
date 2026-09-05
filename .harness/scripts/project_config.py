#!/usr/bin/env python3
"""Create stable project identity and merge native MCP client configuration."""

from __future__ import annotations

import json
import hashlib
import os
import pathlib
import re
import sys
import tempfile
import uuid

START = "# project-harness:ardvi-mcp"
END = "# /project-harness:ardvi-mcp"
URL = "http://127.0.0.1:8765/mcp"

# Client hooks have the same outer JSON shape, but Claude alone supports
# asyncRewake. Its watcher is started at SessionStart and rearmed by Stop.
HOOK_EVENTS = {
    "codex": (
        ("SessionStart", "session-start", "startup|resume|clear|compact", {}),
        ("UserPromptSubmit", "prompt", None, {}),
        ("SessionEnd", "session-end", None, {}),
    ),
    "claude": (
        ("SessionStart", "session-start", "startup|resume|clear|compact|fork", {}),
        ("UserPromptSubmit", "prompt", None, {}),
        ("SessionEnd", "session-end", None, {}),
        ("SessionStart", "watch", "startup|resume|clear|compact|fork", {"asyncRewake": True, "timeout": 86400}),
        ("Stop", "watch", None, {"asyncRewake": True, "timeout": 86400}),
    ),
}
HOOK_TIMEOUT_SEC = 10


def atomic(path: pathlib.Path, text: str) -> None:
    if path.is_symlink():
        raise ValueError(f"refusing to write through symlink: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as f:
        f.write(text)
        tmp = pathlib.Path(f.name)
    os.replace(tmp, path)


def identity(root: pathlib.Path) -> dict[str, str]:
    path = root / ".ardvi/project.json"
    if path.is_symlink():
        raise ValueError(f"project identity must be a regular file: {path}")
    if path.exists():
        value = json.loads(path.read_text(encoding="utf-8"))
        uuid.UUID(value["id"])
        if not isinstance(value.get("name"), str) or not value["name"]:
            raise ValueError(f"invalid project name in {path}")
        return value
    value = {"id": str(uuid.uuid4()), "name": root.name}
    atomic(path, json.dumps(value, indent=2) + "\n")
    return value


def codex(root: pathlib.Path, project_id: str) -> tuple[str, pathlib.Path, str]:
    path = root / ".codex/config.toml"
    if path.is_symlink():
        raise ValueError(f"Codex config must be a regular file: {path}")
    original = path.read_text(encoding="utf-8") if path.exists() else ""
    body = (
        f'[mcp_servers.ardvi]\nurl = "{URL}"\n'
        f'http_headers = {{ X-Ardvi-Project = "{project_id}" }}\n'
    )
    checksum = hashlib.sha256(body.encode()).hexdigest()
    block = f"{START} sha256={checksum}\n{body}{END}\n"
    pattern = re.compile(re.escape(START) + r" sha256=([0-9a-f]{64})\n(.*?)" + re.escape(END) + r"\n?", re.S)
    if START in original or END in original:
        matches = list(pattern.finditer(original))
        if len(matches) != 1:
            raise ValueError(f"invalid managed MCP block in {path}")
        if hashlib.sha256(matches[0].group(2).encode()).hexdigest() != matches[0].group(1):
            raise ValueError(f"refusing to overwrite modified managed MCP block in {path}")
        updated = pattern.sub(block, original)
        status = "current" if updated == original else "updated"
    else:
        if re.search(r"(?m)^\s*\[mcp_servers\.(?:ardvi|\"ardvi\"|'ardvi')\]\s*$", original):
            raise ValueError(f"refusing to overwrite custom mcp_servers.ardvi in {path}")
        updated = original.rstrip() + ("\n\n" if original.strip() else "") + block
        status = "added" if original else "created"
    return status, path, updated


def claude(root: pathlib.Path, project_id: str) -> tuple[str, pathlib.Path, str]:
    path = root / ".mcp.json"
    if path.is_symlink():
        raise ValueError(f"Claude config must be a regular file: {path}")
    value = json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
    if not isinstance(value, dict):
        raise ValueError(f"expected a JSON object in {path}")
    servers = value.setdefault("mcpServers", {})
    if not isinstance(servers, dict):
        raise ValueError(f"expected mcpServers to be an object in {path}")
    desired = {
        "type": "http",
        "url": URL,
        "headers": {"X-Ardvi-Project": project_id},
    }
    existing = servers.get("ardvi")
    if existing is not None and existing != desired:
        raise ValueError(f"refusing to overwrite custom mcpServers.ardvi in {path}")
    status = "current" if existing == desired else ("added" if path.exists() else "created")
    servers["ardvi"] = desired
    return status, path, json.dumps(value, indent=2) + "\n"


def _is_ours(entry: object) -> bool:
    return (
        isinstance(entry, dict)
        and isinstance(entry.get("command"), str)
        and entry["command"].startswith("ardvi hook ")
    )


def _desired_block(command: str, matcher: str | None, options: dict) -> dict:
    block: dict = {}
    if matcher is not None:
        block["matcher"] = matcher
    block["hooks"] = [{"type": "command", "command": command, "timeout": HOOK_TIMEOUT_SEC, **options}]
    return block


def _merge_event(existing: object, desired: list[dict]) -> list:
    if existing is None:
        blocks: list = []
    elif isinstance(existing, list):
        blocks = existing
    else:
        raise ValueError("expected a list of hook matcher blocks")

    result = []
    for block in blocks:
        if isinstance(block, dict) and isinstance(block.get("hooks"), list):
            new_hooks = [entry for entry in block["hooks"] if not _is_ours(entry)]
            # Drop a managed-only block. Blocks with foreign hooks stay in their
            # original order and keep every foreign entry untouched.
            if not new_hooks and set(block).issubset({"matcher", "hooks"}):
                continue
            block = {**block, "hooks": new_hooks}
        result.append(block)
    return result + desired


def hooks(root: pathlib.Path, relpath: str, client: str) -> tuple[str, pathlib.Path, str]:
    """Merge our SessionStart/UserPromptSubmit/SessionEnd hooks into a client's hooks file.

    Never removes or reorders another tool's matcher blocks or hook entries;
    only adds our own entry (identified by its "ardvi hook " command prefix)
    or updates it in place when its command/timeout has drifted.
    """
    path = root / relpath
    if path.is_symlink():
        raise ValueError(f"hooks file must be a regular file: {path}")
    value = json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
    if not isinstance(value, dict):
        raise ValueError(f"expected a JSON object in {path}")
    events = value.setdefault("hooks", {})
    if not isinstance(events, dict):
        raise ValueError(f"expected \"hooks\" to be an object in {path}")
    wanted: dict[str, list[dict]] = {}
    for event_name, subcommand, matcher, options in HOOK_EVENTS[client]:
        command = f"ardvi hook {subcommand} --client {client}"
        wanted.setdefault(event_name, []).append(_desired_block(command, matcher, options))
    for event_name, desired in wanted.items():
        events[event_name] = _merge_event(events.get(event_name), desired)
    text = json.dumps(value, indent=2) + "\n"
    status = "created" if not path.exists() else ("current" if path.read_text(encoding="utf-8") == text else "updated")
    return status, path, text


def main() -> int:
    root = pathlib.Path(os.environ["HARNESS_REPO_ROOT"]).resolve()
    try:
        value = identity(root)
        codex_status, codex_path, codex_text = codex(root, value["id"])
        claude_status, claude_path, claude_text = claude(root, value["id"])
        claude_hooks_status, claude_hooks_path, claude_hooks_text = hooks(root, ".claude/settings.json", "claude")
        codex_hooks_status, codex_hooks_path, codex_hooks_text = hooks(root, ".codex/hooks.json", "codex")
        if not codex_path.exists() or codex_path.read_text(encoding="utf-8") != codex_text:
            atomic(codex_path, codex_text)
        if not claude_path.exists() or claude_path.read_text(encoding="utf-8") != claude_text:
            atomic(claude_path, claude_text)
        if not claude_hooks_path.exists() or claude_hooks_path.read_text(encoding="utf-8") != claude_hooks_text:
            atomic(claude_hooks_path, claude_hooks_text)
        if not codex_hooks_path.exists() or codex_hooks_path.read_text(encoding="utf-8") != codex_hooks_text:
            atomic(codex_hooks_path, codex_hooks_text)
        print(f"Project identity: {value['id']}")
        print(f"Codex MCP config: {codex_status}")
        print(f"Claude MCP config: {claude_status}")
        print(f"Claude hooks: {claude_hooks_status}")
        print(f"Codex hooks: {codex_hooks_status}")
    except (OSError, ValueError, KeyError, json.JSONDecodeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
