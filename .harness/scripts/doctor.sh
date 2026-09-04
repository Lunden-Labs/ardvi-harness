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

for cmd in git python3 go curl ps; do
  check_command "$cmd" required
done
check_command codex optional
check_command claude optional

for path in \
  "$UPSTREAMS_DIR/agent-skills/skills" \
  "$UPSTREAMS_DIR/agency-agents" \
  "$UPSTREAMS_DIR/ponytail/skills" \
  "$UPSTREAMS_DIR/writing-skills/for-agents"; do
  if [[ -d "$path" ]]; then
    printf 'OK       upstream: %s\n' "$path"
  else
    printf 'MISSING  upstream: %s\n' "$path"
    failed=1
  fi
done

python3 "$HARNESS_DIR/scripts/validate_writing_skills.py" \
  "$UPSTREAMS_DIR/writing-skills/for-agents" || failed=1

for path in \
  "$REPO_ROOT/AGENTS.md" \
  "$REPO_ROOT/CLAUDE.md" \
  "$REPO_ROOT/.ardvi/project.json" \
  "$REPO_ROOT/.codex/config.toml" \
  "$REPO_ROOT/.mcp.json" \
  "$REPO_ROOT/.agents/skills/communication/SKILL.md" \
  "$REPO_ROOT/.claude/skills/communication/SKILL.md"; do
  if [[ -e "$path" ]]; then
    printf 'OK       file: %s\n' "${path#$REPO_ROOT/}"
  else
    printf 'MISSING  file: %s\n' "${path#$REPO_ROOT/}"
    failed=1
  fi
done

for path in \
  "$HARNESS_DIR/skills/communication/SKILL.md" \
  "$HARNESS_DATA_DIR/skills/communication/SKILL.md" \
  "$HARNESS_BIN_DIR/ardvi-mcp" \
  "$HUB_CATALOG" \
  "$UPSTREAM_LOCK"; do
  if [[ -f "$path" ]]; then
    printf 'OK       managed: %s\n' "$path"
  else
    printf 'MISSING  managed: %s\n' "$path"
    failed=1
  fi
done

exit "$failed"
