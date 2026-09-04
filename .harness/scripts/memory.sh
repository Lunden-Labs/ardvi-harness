#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

action="${1:-}"
file="${2:-}"
[[ "$action" == export || "$action" == import ]] || { echo "usage: memory.sh export|import FILE" >&2; exit 2; }
[[ -n "$file" ]] || file="$REPO_ROOT/.ardvi/memory.jsonl"
command -v ardvi >/dev/null 2>&1 || { echo "Ardvi is not installed." >&2; exit 1; }
project="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["id"])' "$REPO_ROOT/.ardvi/project.json")"
[[ "$action" != import || -f "$file" ]] || { echo "Memory file not found: $file" >&2; exit 1; }
ardvi memory "$action" --project "$project" --file "$file"
echo "Project memory ${action}ed: $file"
