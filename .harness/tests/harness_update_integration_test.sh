#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT

source_repo="$workspace/source"
target="$workspace/target"
mkdir -p "$source_repo" "$target"
git -C "$source_repo" init -q -b main
git -C "$target" init -q
cp -a "$repo_root/.harness" "$source_repo/.harness"
cat > "$source_repo/Makefile" <<'EOF'
ARDVI_HARNESS_SHORT_TARGETS := 1
include .harness/harness.mk
EOF
cat > "$source_repo/.harness/harness-source.tsv" <<EOF
# name	repository	revision	update_policy
ardvi-harness	$source_repo	main	fast-forward-replace-managed
EOF
git -C "$source_repo" add .
git -C "$source_repo" -c user.name=Harness -c user.email=harness@example.invalid \
  commit -qm 'fixture v1'

TARGET="$target" make --no-print-directory -C "$source_repo" copy >/dev/null
[[ -f "$target/.harness/.managed-state.json" ]]
python3 "$target/.harness/scripts/manage_harness.py" verify "$target/.harness" >/dev/null
HARNESS_REPO_ROOT="$target" python3 "$target/.harness/scripts/sync_instructions.py" >/dev/null
[[ -f "$target/AGENTS.md" && -f "$target/CLAUDE.md" ]]

printf 'v2\n' > "$source_repo/.harness/update-marker"
git -C "$source_repo" add .harness/update-marker
git -C "$source_repo" -c user.name=Harness -c user.email=harness@example.invalid \
  commit -qm 'fixture v2'
v2="$(git -C "$source_repo" rev-parse HEAD)"

bash "$target/.harness/scripts/update_harness.sh" > "$workspace/update.log"
grep -Fqx 'v2' "$target/.harness/update-marker"
grep -Fq "$v2" "$workspace/update.log"
[[ "$(python3 "$target/.harness/scripts/manage_harness.py" show "$target/.harness" commit)" == "$v2" ]]

printf '\nlocal edit\n' >> "$target/.harness/README.md"
printf 'v3\n' > "$source_repo/.harness/update-marker"
git -C "$source_repo" add .harness/update-marker
git -C "$source_repo" -c user.name=Harness -c user.email=harness@example.invalid \
  commit -qm 'fixture v3'
if bash "$target/.harness/scripts/update_harness.sh" > "$workspace/conflict.log" 2>&1; then
  echo 'modified managed harness was overwritten' >&2
  exit 1
fi
grep -Fq 'refusing to overwrite modified harness' "$workspace/conflict.log"
grep -Fqx 'v2' "$target/.harness/update-marker"

source_checkout="$workspace/source-checkout"
mkdir -p "$source_checkout"
git -C "$source_checkout" init -q
git -C "$source_checkout" remote add origin \
  git@github.com:Lunden-Labs/ardvi-harness.git
cp -a "$repo_root/.harness" "$source_checkout/.harness"
source_output="$(bash "$source_checkout/.harness/scripts/update_harness.sh")"
grep -Fq 'Harness source checkout: update .harness through normal Git workflow' \
  <<< "$source_output"

echo "harness update integration: PASS"
