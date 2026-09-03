#!/usr/bin/env bash
set -Eeuo pipefail

harness_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
target="${TARGET:-}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

if [[ -z "$target" ]]; then
  [[ -t 0 ]] || fail "set TARGET to an existing Git repository root"
  read -r -p "Target Git repository root: " target
fi

case "$target" in
  '~') target="${HOME:?HOME is required to expand ~}" ;;
  '~/'*) target="${HOME:?HOME is required to expand ~}/${target#\~/}" ;;
esac

[[ -d "$target" ]] || fail "target directory does not exist: $target"
target="$(cd "$target" && pwd -P)"
git_root="$(git -C "$target" rev-parse --show-toplevel 2>/dev/null)" || fail "target is not a Git repository: $target"
git_root="$(cd "$git_root" && pwd -P)"
[[ "$target" == "$git_root" ]] || fail "target must be the Git repository root: $git_root"

[[ ! -e "$target/.harness" && ! -L "$target/.harness" ]] || fail "target already has .harness"

makefile="$target/Makefile"
command_hint='make harness-init'
if [[ -e "$makefile" || -L "$makefile" ]]; then
  [[ -f "$makefile" && ! -L "$makefile" ]] || fail "target Makefile must be a regular file"
  [[ -r "$makefile" && -w "$makefile" ]] || fail "target Makefile must be readable and writable"
else
  [[ -w "$target" ]] || fail "target directory must be writable"
  makefile_was_missing=1
fi

cp -a "$harness_dir" "$target/.harness"

if [[ "${makefile_was_missing:-}" == 1 ]]; then
  if ! (set -o noclobber; printf 'ARDVI_HARNESS_SHORT_TARGETS := 1\n.DEFAULT_GOAL := help\n\ninclude .harness/harness.mk\n' > "$makefile"); then
    if [[ -e "$makefile" || -L "$makefile" ]]; then
      fail "Makefile appeared during harness copy; refusing to overwrite it"
    fi
    fail "unable to create target Makefile"
  fi
elif ! grep -Fqx 'include .harness/harness.mk' "$makefile"; then
  printf '\ninclude .harness/harness.mk\n' >> "$makefile"
fi

printf 'Harness copied. Next: cd %q && %s\n' "$target" "$command_hint"
