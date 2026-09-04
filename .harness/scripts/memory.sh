#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

action="${1:-}"
file="${2:-}"
[[ "$action" == export || "$action" == import ]] || { echo "usage: memory.sh export|import FILE" >&2; exit 2; }
[[ -n "$file" ]] || file="$REPO_ROOT/.ardvi/memory.jsonl"
[[ -x "$HARNESS_BIN_DIR/ardvi-mcp" ]] || { echo "Run make init first." >&2; exit 1; }
project="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$REPO_ROOT/.ardvi/project.json")"
if [[ -f "$HUB_STATE_DIR/server.pid" ]] && kill -0 "$(<"$HUB_STATE_DIR/server.pid")" 2>/dev/null; then
  echo "Stop the hub with make down before memory $action." >&2
  exit 1
fi
if [[ "$action" == export ]]; then
  "$HARNESS_BIN_DIR/ardvi-mcp" memory-export --data "$HUB_DATA_DIR" --project "$project" --file "$file"
else
  [[ -f "$file" ]] || { echo "Memory file not found: $file" >&2; exit 1; }
  "$HARNESS_BIN_DIR/ardvi-mcp" memory-import --data "$HUB_DATA_DIR" --project "$project" --file "$file"
fi
echo "Project memory ${action}ed: $file"
