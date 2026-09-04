#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

slug="$(require_project_slug)"
profile="${slug}-architect"

if ! command -v cao >/dev/null 2>&1; then
  echo "CAO is missing. Run the install target." >&2
  exit 1
fi
if [[ ! -f "$REPO_ROOT/.cao/agents/$profile.md" ]]; then
  echo "Profile is missing: $profile. Run the bootstrap target." >&2
  exit 1
fi

python3 "$HARNESS_DIR/scripts/register_cao.py"
cd "$REPO_ROOT"
provider_args=()
if [[ -n "${PROVIDER:-}" ]]; then
  provider_args+=(--provider "$PROVIDER")
fi
if [[ -n "${PROMPT:-}" ]]; then
  session_name="harness-architect-$(date +%s)-$$"
  CAO_MCP_REQUEST_TIMEOUT="${CAO_MCP_REQUEST_TIMEOUT:-120}" \
    cao launch --agents "$profile" "${provider_args[@]}" \
      --session-name "$session_name" --headless --async "$PROMPT"
  exec tmux attach-session -t "cao-$session_name"
fi
exec cao launch --agents "$profile" "${provider_args[@]}"
