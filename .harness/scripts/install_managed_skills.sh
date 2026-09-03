#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

source_file="$HARNESS_DIR/skills/communication/SKILL.md"
target_dir="$HARNESS_DATA_DIR/skills/communication"
target_file="$target_dir/SKILL.md"
state_file="$target_dir/.managed-sha256"
source_hash="$(sha256sum "$source_file" | awk '{print $1}')"

mkdir -p "$target_dir"
if [[ -e "$target_file" ]]; then
  if [[ ! -f "$state_file" ]]; then
    echo "Refusing to replace unmanaged communication skill: $target_file" >&2
    exit 1
  fi
  recorded="$(cat "$state_file")"
  actual="$(sha256sum "$target_file" | awk '{print $1}')"
  if [[ "$actual" != "$recorded" ]]; then
    echo "Refusing to overwrite modified managed communication skill: $target_file" >&2
    exit 1
  fi
fi

if [[ -f "$target_file" && "$(sha256sum "$target_file" | awk '{print $1}')" == "$source_hash" ]]; then
  echo "Managed skill current: communication"
  exit 0
fi

temporary="$(mktemp "$target_dir/.SKILL.md.XXXXXX")"
trap 'rm -f "$temporary"' EXIT
cp "$source_file" "$temporary"
mv "$temporary" "$target_file"
printf '%s\n' "$source_hash" > "$state_file"
trap - EXIT
echo "Managed skill installed: communication"
