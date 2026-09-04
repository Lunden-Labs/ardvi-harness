#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

skill="${1:-}"
if [[ ! "$skill" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "usage: skill_path.sh SKILL_NAME" >&2
  exit 2
fi

path="$HARNESS_DIR/skills/$skill/SKILL.md"

if [[ ! -f "$path" ]]; then
  echo "Native entry skill is not installed: $skill; use 'ardvi skills list' for the MCP catalog." >&2
  exit 1
fi
printf '%s\n' "$path"
