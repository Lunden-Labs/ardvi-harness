#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT

new_repo() {
  local path="$1"
  mkdir -p "$path"
  git -C "$path" init -q
}

run_copy() {
  TARGET="$1" make --no-print-directory -C "$repo_root" copy
}

fresh="$workspace/fresh"
new_repo "$fresh"
fresh_output="$(run_copy "$fresh")"
[[ -d "$fresh/.harness" ]]
[[ -f "$fresh/.harness/skills/communication/SKILL.md" ]]
[[ -f "$fresh/.harness/upstreams.tsv" ]]
[[ -f "$fresh/.harness/mcp/go.mod" ]]
[[ -f "$fresh/.harness/skills/lets-go/SKILL.md" ]]
[[ -f "$fresh/.harness/skills/skills-list/SKILL.md" ]]
[[ -f "$fresh/.harness/LICENSE" ]]
diff -q "$repo_root/.harness/LICENSE" "$fresh/.harness/LICENSE" >/dev/null
python3 "$fresh/.harness/scripts/manage_harness.py" verify "$fresh/.harness" >/dev/null
grep -Fqx 'ARDVI_HARNESS_SHORT_TARGETS := 1' "$fresh/Makefile"
grep -Fqx '.DEFAULT_GOAL := help' "$fresh/Makefile"
grep -Fqx 'include .harness/harness.mk' "$fresh/Makefile"
printf -v fresh_next 'cd %q && make harness-init' "$fresh"
[[ "$fresh_output" == *"$fresh_next"* ]]
make -C "$fresh" -n init >/dev/null
make -C "$fresh" -n harness-skill-path SKILL=communication >/dev/null
make -C "$fresh" -n up >/dev/null
make -C "$fresh" -n skills >/dev/null
! run_copy "$fresh" >/dev/null 2>&1
[[ "$(grep -Fxc 'include .harness/harness.mk' "$fresh/Makefile")" == 1 ]]

existing="$workspace/existing"
new_repo "$existing"
cat > "$existing/Makefile" <<'EOF'
.PHONY: help
help:
	@echo "product help"
EOF
existing_output="$(run_copy "$existing")"
[[ "$(head -n 4 "$existing/Makefile")" == *"product help"* ]]
[[ "$(grep -Fxc 'include .harness/harness.mk' "$existing/Makefile")" == 1 ]]
printf -v existing_next 'cd %q && make harness-init' "$existing"
[[ "$existing_output" == *"$existing_next"* ]]
help_output="$(make -C "$existing" help 2>&1)"
[[ "$help_output" == *"product help"* ]]
[[ "$help_output" != *"overriding recipe"* ]]
make -C "$existing" -n harness-init >/dev/null

not_git="$workspace/not-git"
mkdir "$not_git"
! run_copy "$not_git" >/dev/null 2>&1
[[ ! -e "$not_git/.harness" ]]
subdir_repo="$workspace/subdir-repo"
new_repo "$subdir_repo"
mkdir "$subdir_repo/subdir"
! run_copy "$subdir_repo/subdir" >/dev/null 2>&1
[[ ! -e "$subdir_repo/.harness" ]]

occupied="$workspace/occupied"
new_repo "$occupied"
mkdir "$occupied/.harness"
! run_copy "$occupied" >/dev/null 2>&1

dangling="$workspace/dangling"
new_repo "$dangling"
ln -s "$workspace/missing" "$dangling/.harness"
! run_copy "$dangling" >/dev/null 2>&1

linked_makefile="$workspace/linked-makefile"
new_repo "$linked_makefile"
printf 'outside\n' > "$workspace/outside-makefile"
ln -s "$workspace/outside-makefile" "$linked_makefile/Makefile"
! run_copy "$linked_makefile" >/dev/null 2>&1
[[ ! -e "$linked_makefile/.harness" ]]

raced="$workspace/raced"
new_repo "$raced"
race_bin="$workspace/race-bin"
mkdir "$race_bin"
real_cp="$(command -v cp)"
cat > "$race_bin/cp" <<'EOF'
#!/usr/bin/env bash
"$REAL_CP" "$@"
printf 'raced Makefile\n' > "$RACE_MAKEFILE"
EOF
chmod +x "$race_bin/cp"
! PATH="$race_bin:$PATH" REAL_CP="$real_cp" RACE_MAKEFILE="$raced/Makefile" TARGET="$raced" \
  make --no-print-directory -C "$repo_root" copy >/dev/null 2>&1
[[ "$(cat "$raced/Makefile")" == 'raced Makefile' ]]
[[ -d "$raced/.harness" ]]

echo "copy integration: PASS"
