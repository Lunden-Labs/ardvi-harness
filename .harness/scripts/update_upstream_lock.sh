#!/usr/bin/env bash
set -Eeuo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$HARNESS_DIR/upstreams.tsv"
lock="$HARNESS_DIR/upstreams.lock.tsv"
temporary="$(mktemp "${lock}.XXXXXX")"
trap 'rm -f "$temporary"' EXIT

printf '# name\trepository\trevision\tresolved_commit\tinstalled_path\tupdate_policy\n' > "$temporary"
while IFS=$'\t' read -r name repository revision installed_path policy extra; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  [[ -z "${extra:-}" && "$policy" == fast-forward ]] || { echo "invalid upstream manifest row: $name" >&2; exit 1; }
  resolved="$(git ls-remote "$repository" "refs/heads/$revision" | awk 'NR == 1 {print $1}')"
  [[ "$resolved" =~ ^[0-9a-f]{40}$ ]] || { echo "cannot resolve $name $revision" >&2; exit 1; }
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$repository" "$revision" "$resolved" "$installed_path" "$policy" >> "$temporary"
done < "$manifest"

cmp -s "$temporary" "$lock" && { echo "Upstream lock is current"; exit 0; }
mv "$temporary" "$lock"
echo "Updated upstream lock:"
awk -F '\t' '!/^#/ {printf "  %-16s %s\n", $1, $4}' "$lock"
