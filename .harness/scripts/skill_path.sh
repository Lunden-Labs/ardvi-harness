#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

skill="${1:-}"
if [[ ! "$skill" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "usage: skill_path.sh SKILL_NAME" >&2
  exit 2
fi

if [[ -f "$HARNESS_DIR/skills/$skill/SKILL.md" && "$skill" != writing ]]; then
  path="$HARNESS_DIR/skills/$skill/SKILL.md"
else
  path="$UPSTREAMS_DIR/writing-skills/for-agents/$skill/SKILL.md"
fi

if [[ ! -f "$path" ]]; then
  echo "Skill is not installed: $skill (run make update)" >&2
  exit 1
fi
printf '%s\n' "$path"
