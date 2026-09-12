#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT

run_config() {
  HARNESS_REPO_ROOT="$1" python3 "$repo_root/.harness/scripts/project_config.py" >/dev/null
}

project="$workspace/project"
mkdir -p "$project/.harness"
cp "$repo_root/.harness/scripts/project_config.py" "$project/.harness/project_config.py"
run_config "$project"

python3 - "$project/.opencode/opencode.json" "$project/.ardvi/project.json" <<'PY'
import json, pathlib, sys
config = json.loads(pathlib.Path(sys.argv[1]).read_text())
project_id = json.loads(pathlib.Path(sys.argv[2]).read_text())['id']
assert config == {
    'mcp': {'servers': {'ardvi': {
        'type': 'remote', 'url': 'http://127.0.0.1:8765/mcp',
        'headers': {'X-Ardvi-Project': project_id}, 'oauth': False, 'codemode': False,
    }}}
}
PY
before="$(sha256sum "$project/.opencode/opencode.json")"
run_config "$project"
[[ "$before" == "$(sha256sum "$project/.opencode/opencode.json")" ]]

python3 - "$project/.opencode/opencode.json" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
value = json.loads(path.read_text())
value['foreign'] = {'keep': True}
path.write_text(json.dumps(value) + '\n')
PY
run_config "$project"
grep -Fq '"foreign"' "$project/.opencode/opencode.json"

conflict="$workspace/conflict"
mkdir -p "$conflict/.harness" "$conflict/.opencode"
cp "$repo_root/.harness/scripts/project_config.py" "$conflict/.harness/project_config.py"
printf '%s\n' '{"mcp":{"servers":{"ardvi":{"type":"remote","url":"http://custom"}}}}' > "$conflict/.opencode/opencode.json"
before="$(sha256sum "$conflict/.opencode/opencode.json")"
if run_config "$conflict"; then exit 1; fi
[[ "$before" == "$(sha256sum "$conflict/.opencode/opencode.json")" ]]

jsonc="$workspace/jsonc"
mkdir -p "$jsonc/.harness" "$jsonc/.opencode"
cp "$repo_root/.harness/scripts/project_config.py" "$jsonc/.harness/project_config.py"
printf '%s\n' '{mcp: {servers: {ardvi: {}}}}' > "$jsonc/.opencode/opencode.jsonc"
if run_config "$jsonc"; then exit 1; fi
[[ ! -e "$jsonc/.opencode/opencode.json" ]]

symlink="$workspace/symlink"
mkdir -p "$symlink/.harness" "$symlink/.opencode"
cp "$repo_root/.harness/scripts/project_config.py" "$symlink/.harness/project_config.py"
printf '{}\n' > "$workspace/foreign.json"
ln -s "$workspace/foreign.json" "$symlink/.opencode/opencode.json"
if run_config "$symlink"; then exit 1; fi

root_conflict="$workspace/root-conflict"
mkdir -p "$root_conflict/.harness"
cp "$repo_root/.harness/scripts/project_config.py" "$root_conflict/.harness/project_config.py"
printf '%s\n' '{"mcp":{"servers":{"ardvi":{"url":"http://custom"}}}}' > "$root_conflict/opencode.json"
if run_config "$root_conflict"; then exit 1; fi
[[ ! -e "$root_conflict/.opencode/opencode.json" ]]

root_existing="$workspace/root-existing"
mkdir -p "$root_existing/.harness"
mkdir -p "$root_existing/.ardvi"
cp "$repo_root/.harness/scripts/project_config.py" "$root_existing/.harness/project_config.py"
printf '%s\n' '{"id":"22222222-2222-4222-8222-222222222222","name":"root-existing"}' > "$root_existing/.ardvi/project.json"
printf '%s\n' '{"mcp":{"servers":{"ardvi":{"type":"remote","url":"http://127.0.0.1:8765/mcp","headers":{"X-Ardvi-Project":"22222222-2222-4222-8222-222222222222"},"oauth":false,"codemode":false,"disabled":false}}}}' > "$root_existing/opencode.json"
printf '%s\n' '{"mcp":{"servers":{"ardvi":{"type":"remote","url":"http://127.0.0.1:8765/mcp","headers":{"X-Ardvi-Project":"22222222-2222-4222-8222-222222222222"},"oauth":false,"codemode":false}}}}' > "$root_existing/opencode.jsonc"
before_json="$(sha256sum "$root_existing/opencode.json")"
before_jsonc="$(sha256sum "$root_existing/opencode.jsonc")"
run_config "$root_existing"
[[ ! -e "$root_existing/.opencode/opencode.json" ]]
[[ "$before_json" == "$(sha256sum "$root_existing/opencode.json")" ]]
[[ "$before_jsonc" == "$(sha256sum "$root_existing/opencode.jsonc")" ]]

root_symlink="$workspace/root-symlink"
mkdir -p "$root_symlink/.harness"
cp "$repo_root/.harness/scripts/project_config.py" "$root_symlink/.harness/project_config.py"
printf '%s\n' '{}' > "$workspace/root-config.json"
ln -s "$workspace/root-config.json" "$root_symlink/opencode.json"
if run_config "$root_symlink"; then exit 1; fi
[[ ! -e "$root_symlink/.opencode/opencode.json" ]]

echo "opencode config: PASS"
