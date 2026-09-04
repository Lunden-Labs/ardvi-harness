#!/usr/bin/env bash
set -Eeuo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${1:-}"
[[ -n "$target" && "$target" == /* && "$target" != / ]] || {
  echo "usage: prepare_image_skills.sh /absolute/staging-root" >&2
  exit 2
}
for command in git python3; do
  command -v "$command" >/dev/null 2>&1 || { echo "missing build dependency: $command" >&2; exit 1; }
done

data="$target/opt/ardvi"
mkdir -p "$data/skills" "$data/upstreams" "$target/var/lib/ardvi"
cp -a "$HARNESS_DIR/skills/." "$data/skills/"
cp "$HARNESS_DIR/upstreams.lock.tsv" "$data/upstreams.lock.tsv"

while IFS=$'\t' read -r name repository revision resolved installed_path policy extra; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  [[ -z "${extra:-}" && "$name" =~ ^[a-z0-9][a-z0-9-]*$ && -n "$revision" && "$resolved" =~ ^[0-9a-f]{40}$ && "$policy" == fast-forward ]] || {
    echo "invalid upstream lock row: $name" >&2
    exit 1
  }
  destination="$data/upstreams/$name"
  git clone --quiet "$repository" "$destination"
  git -C "$destination" checkout --quiet --detach "$resolved"
  [[ "$(git -C "$destination" rev-parse HEAD)" == "$resolved" ]] || {
    echo "upstream checkout mismatch: $name" >&2
    exit 1
  }
  rm -rf "$destination/.git"
  [[ -n "$(find "$destination" -maxdepth 2 -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) -print -quit)" ]] || {
    echo "upstream license file is missing: $name" >&2
    exit 1
  }
  [[ -d "$data/$installed_path" ]] || { echo "missing installed path for $name: $installed_path" >&2; exit 1; }
done < "$HARNESS_DIR/upstreams.lock.tsv"

python3 "$HARNESS_DIR/scripts/validate_writing_skills.py" "$data/upstreams/writing-skills/for-agents"
ARDVI_CATALOG_DATA_DIR="$data" \
HARNESS_CATALOG_SCAN_UPSTREAMS="$data/upstreams" \
HARNESS_CATALOG_ROOT_UPSTREAMS="/opt/ardvi/upstreams" \
HARNESS_CATALOG_LOCK="$data/upstreams.lock.tsv" \
HARNESS_CATALOG_OUTPUT="$data/catalog.json" \
  python3 "$HARNESS_DIR/scripts/build_catalog.py"
chmod -R a=rX "$data"
chmod 0700 "$target/var/lib/ardvi"
