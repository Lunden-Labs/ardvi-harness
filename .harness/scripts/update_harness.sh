#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

normalize_repository() {
  local repository="${1%.git}"
  case "$repository" in
    git@github.com:*) repository="https://github.com/${repository#git@github.com:}" ;;
    ssh://git@github.com/*) repository="https://github.com/${repository#ssh://git@github.com/}" ;;
  esac
  printf '%s' "$repository"
}

if [[ "${HARNESS_SKIP_SELF_UPDATE:-0}" == 1 ]]; then
  echo "Harness source update: skipped by HARNESS_SKIP_SELF_UPDATE=1"
  exit 0
fi

manifest="${HARNESS_SOURCE_MANIFEST:-$HARNESS_DIR/harness-source.tsv}"
IFS=$'\t' read -r name repository revision policy extra < <(sed '/^[[:space:]]*#/d; /^[[:space:]]*$/d' "$manifest")
if [[ -n "${extra:-}" || "$name" != ardvi-harness || -z "$repository" || -z "$revision" || "$policy" != fast-forward-replace-managed ]]; then
  echo "Invalid harness source manifest: $manifest" >&2
  exit 1
fi

state="$HARNESS_DIR/.managed-state.json"
if [[ ! -f "$state" ]]; then
  project_origin="$(git -C "$REPO_ROOT" remote get-url origin 2>/dev/null || true)"
  if [[ "$(normalize_repository "$project_origin")" == "$(normalize_repository "$repository")" ]]; then
    echo "Harness source checkout: update .harness through normal Git workflow"
    exit 0
  fi
  echo "Managed harness state is missing; reinstall with make copy before self-update" >&2
  exit 1
fi

python3 "$HARNESS_DIR/scripts/manage_harness.py" verify "$HARNESS_DIR"
installed="$(python3 "$HARNESS_DIR/scripts/manage_harness.py" show "$HARNESS_DIR" commit)"
latest="$(git ls-remote "$repository" "refs/heads/$revision" | awk 'NR == 1 {print $1}')"
if [[ ! "$latest" =~ ^[0-9a-f]{40}$ ]]; then
  echo "Cannot resolve harness revision $revision from $repository" >&2
  exit 1
fi
if [[ "$installed" == "$latest" ]]; then
  echo "Harness revision: $installed  current"
  exit 0
fi

parent="$(dirname "$HARNESS_DIR")"
temporary="$(mktemp -d "$parent/.harness-update.XXXXXX")"
cleanup() { rm -rf "$temporary"; }
trap cleanup EXIT
git clone --quiet --branch "$revision" "$repository" "$temporary/source"
resolved="$(git -C "$temporary/source" rev-parse HEAD)"
if [[ "$resolved" != "$latest" ]]; then
  echo "Harness source changed during update; retry ($latest -> $resolved)" >&2
  exit 1
fi
if ! git -C "$temporary/source" merge-base --is-ancestor "$installed" "$latest"; then
  echo "Refusing non-fast-forward harness update: $installed -> $latest" >&2
  exit 1
fi

cp -a "$temporary/source/.harness" "$temporary/replacement"
for required in harness.mk scripts/manage_harness.py scripts/install.sh \
  scripts/sync_instructions.py scripts/project_config.py upstreams.tsv \
  upstreams.lock.tsv skills/skills-list/SKILL.md mcp/go.mod; do
  if [[ ! -f "$temporary/replacement/$required" ]]; then
    echo "Updated harness is missing required file: $required" >&2
    exit 1
  fi
done
python3 "$HARNESS_DIR/scripts/manage_harness.py" record \
  "$temporary/replacement" "$repository" "$revision" "$latest"
mv "$HARNESS_DIR" "$temporary/previous"
if ! mv "$temporary/replacement" "$HARNESS_DIR"; then
  mv "$temporary/previous" "$HARNESS_DIR"
  exit 1
fi
rm -rf "$temporary/previous"
echo "Harness revision: $installed -> $latest  updated"
