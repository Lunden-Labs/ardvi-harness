#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

for source_dir in "$HARNESS_DIR"/skills/*; do
  [[ -f "$source_dir/SKILL.md" ]] || continue
  name="$(basename "$source_dir")"
  target_dir="$HARNESS_DATA_DIR/skills/$name"
  target_file="$target_dir/SKILL.md"
  state_file="$target_dir/.managed-sha256"
  source_hash="$(sha256sum "$source_dir/SKILL.md" | awk '{print $1}')"
  mkdir -p "$target_dir"
  if [[ -e "$target_file" ]]; then
    [[ -f "$state_file" ]] || { echo "Refusing to replace unmanaged skill: $target_file" >&2; exit 1; }
    recorded="$(<"$state_file")"
    actual="$(sha256sum "$target_file" | awk '{print $1}')"
    [[ "$actual" == "$recorded" ]] || { echo "Refusing to overwrite modified managed skill: $target_file" >&2; exit 1; }
  fi
  if [[ -f "$target_file" && "$(sha256sum "$target_file" | awk '{print $1}')" == "$source_hash" ]]; then
    echo "Managed skill current: $name"
    continue
  fi
  temporary="$(mktemp "$target_dir/.SKILL.md.XXXXXX")"
  cp "$source_dir/SKILL.md" "$temporary"
  mv "$temporary" "$target_file"
  printf '%s\n' "$source_hash" > "$state_file"
  echo "Managed skill installed: $name"
done
