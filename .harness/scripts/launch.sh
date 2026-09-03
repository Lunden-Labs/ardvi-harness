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
exec cao launch --agents "$profile" --provider claude_code
