#!/usr/bin/env bash
set -Eeuo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"; trap 'rm -rf "$workspace"' EXIT
project="$workspace/project"; mkdir -p "$project"; git -C "$project" init -q; cp -a "$repo_root/.harness" "$project/.harness"
rm -f "$project/.harness/.managed-state.json"  # fixture must not inherit a local dev checkout's state
printf '%s\n' '{"mcpServers":{"other":{"command":"other"}}}' > "$project/.mcp.json"
HARNESS_REPO_ROOT="$project" PROMPT='Build the API.' bash "$project/.harness/scripts/bootstrap.sh" >/dev/null
id="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["id"])' "$project/.ardvi/project.json")"
grep -Fq 'Build the API.' "$project/tasks/NEXT.md"; grep -Fq '"other"' "$project/.mcp.json"; grep -Fq "$id" "$project/.codex/config.toml"; grep -Fq "$id" "$project/.mcp.json"
HARNESS_REPO_ROOT="$project" bash "$project/.harness/scripts/bootstrap.sh" >/dev/null
[[ "$id" == "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["id"])' "$project/.ardvi/project.json")" ]]

conflict="$workspace/conflict"; mkdir -p "$conflict/.codex"; git -C "$conflict" init -q; cp -a "$repo_root/.harness" "$conflict/.harness"
rm -f "$conflict/.harness/.managed-state.json"
printf '%s\n' '[mcp_servers."ardvi"]' 'url = "http://custom"' > "$conflict/.codex/config.toml"
printf '%s\n' '{"mcpServers":{"keep":{"command":"keep"}}}' > "$conflict/.mcp.json"
before="$(sha256sum "$conflict/.mcp.json")"
if HARNESS_REPO_ROOT="$conflict" python3 "$conflict/.harness/scripts/project_config.py" >/dev/null 2>&1; then echo 'conflicting Codex config accepted' >&2; exit 1; fi
[[ "$before" == "$(sha256sum "$conflict/.mcp.json")" ]]
echo "bootstrap integration: PASS"
