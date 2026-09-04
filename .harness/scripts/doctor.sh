#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

failed=0

check_command() {
  local cmd="$1"
  local required="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    printf 'OK       command: %s\n' "$cmd"
  elif [[ "$required" == "required" ]]; then
    printf 'MISSING  command: %s\n' "$cmd"
    failed=1
  else
    printf 'OPTIONAL command: %s (not installed)\n' "$cmd"
  fi
}

for cmd in git python3 docker ardvi; do
  check_command "$cmd" required
done
check_command codex optional
check_command claude optional

if docker compose version >/dev/null 2>&1; then
  echo "OK       Docker Compose"
else
  echo "MISSING  Docker Compose plugin"
  failed=1
fi

for path in \
  "$REPO_ROOT/AGENTS.md" \
  "$REPO_ROOT/CLAUDE.md" \
  "$REPO_ROOT/.ardvi/project.json" \
  "$REPO_ROOT/.codex/config.toml" \
  "$REPO_ROOT/.mcp.json" \
  "$REPO_ROOT/.agents/skills/communication/SKILL.md" \
  "$REPO_ROOT/.claude/skills/communication/SKILL.md" \
  "$REPO_ROOT/.agents/skills/skills-list/SKILL.md" \
  "$REPO_ROOT/.claude/skills/skills-list/SKILL.md"; do
  if [[ -e "$path" ]]; then
    printf 'OK       file: %s\n' "${path#$REPO_ROOT/}"
  else
    printf 'MISSING  file: %s\n' "${path#$REPO_ROOT/}"
    failed=1
  fi
done

for path in "$HARNESS_DIR/skills/communication/SKILL.md" "$HARNESS_DIR/upstreams.lock.tsv"; do
  if [[ -f "$path" ]]; then
    printf 'OK       managed: %s\n' "$path"
  else
    printf 'MISSING  managed: %s\n' "$path"
    failed=1
  fi
done

if ardvi healthcheck >/dev/null 2>&1; then
  echo "OK       Ardvi MCP health"
else
  echo "MISSING  healthy Ardvi MCP service"
  failed=1
fi
if ardvi skills list --json >/dev/null 2>&1; then
  echo "OK       MCP skill catalog"
else
  echo "MISSING  readable MCP skill catalog"
  failed=1
fi

exit "$failed"
