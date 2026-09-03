#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if ! command -v git >/dev/null 2>&1; then
  echo "git is required to update external skills and profiles" >&2
  exit 1
fi

mkdir -p "$UPSTREAMS_DIR" "$(dirname "$AGENCY_CAO_DIR")"

sync_repo() {
  local name="$1"
  local url="$2"
  local target="$UPSTREAMS_DIR/$name"

  if [[ -d "$target/.git" ]]; then
    local actual_url
    actual_url="$(git -C "$target" remote get-url origin)"
    if [[ "$actual_url" != "$url" && "$actual_url" != "${url%.git}" ]]; then
      echo "Unexpected origin for $target: $actual_url" >&2
      exit 1
    fi
    if [[ -n "$(git -C "$target" status --porcelain)" ]]; then
      echo "Refusing to update modified managed checkout: $target" >&2
      exit 1
    fi
    git -C "$target" pull --ff-only --prune
  elif [[ -e "$target" ]]; then
    echo "Refusing to replace non-Git path: $target" >&2
    exit 1
  else
    git clone --depth 1 "$url" "$target"
  fi
}

sync_repo agent-skills https://github.com/addyosmani/agent-skills.git
sync_repo agency-agents https://github.com/msitarzewski/agency-agents.git
sync_repo ponytail https://github.com/DietrichGebert/ponytail.git

python3 "$HARNESS_DIR/scripts/generate_agency_profiles.py" \
  "$UPSTREAMS_DIR/agency-agents" \
  "$AGENCY_CAO_DIR"

echo "Installed external revisions:"
for name in agent-skills agency-agents ponytail; do
  printf '  %-15s %s\n' "$name" "$(git -C "$UPSTREAMS_DIR/$name" rev-parse --short=12 HEAD)"
done
