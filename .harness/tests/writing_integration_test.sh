#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT

commit_repo() {
  local path="$1"
  git -C "$path" add .
  git -C "$path" -c user.name=Harness -c user.email=harness@example.invalid \
    commit -qm fixture
}

new_upstream() {
  local name="$1"
  local path="$workspace/sources/$name"
  mkdir -p "$path"
  git -C "$path" init -q -b main
  printf '%s\n' "$path"
}

mkdir -p "$workspace/sources" "$workspace/bin" "$workspace/home"

addy="$(new_upstream agent-skills)"
mkdir -p "$addy/skills/using-agent-skills"
printf '%s\n' '---' 'name: using-agent-skills' 'description: fixture' '---' > \
  "$addy/skills/using-agent-skills/SKILL.md"
commit_repo "$addy"

agency="$(new_upstream agency-agents)"
mkdir -p "$agency/engineering"
printf '%s\n' '---' 'name: Fixture Engineer' 'description: fixture' '---' '# Fixture' > \
  "$agency/engineering/fixture.md"
commit_repo "$agency"

ponytail="$(new_upstream ponytail)"
mkdir -p "$ponytail/skills/ponytail"
printf '%s\n' '---' 'name: ponytail' 'description: fixture' '---' > \
  "$ponytail/skills/ponytail/SKILL.md"
commit_repo "$ponytail"

writing="$(new_upstream writing-skills)"
writing_names=(
  abstract-writing academic-voice better-usage general-writing grant-planning
  grant-writing humanizer improve-human-writing-guide literature-review
  non-autoregressive-writing-pass paper-writing presentation-making
  prompt-improving rebuttal-writing writing writing-cadence
)
for name in "${writing_names[@]}"; do
  mkdir -p "$writing/for-agents/$name/agents"
  printf '%s\n' '---' "name: $name" 'description: fixture' '---' > \
    "$writing/for-agents/$name/SKILL.md"
  printf 'interface: fixture\n' > "$writing/for-agents/$name/agents/openai.yaml"
done
mkdir -p "$writing/for-agents/general-writing/references"
printf 'fixture\n' > "$writing/for-agents/general-writing/references/eval.md"
printf '\n[eval](references/eval.md)\n' >> "$writing/for-agents/general-writing/SKILL.md"
for name in "${writing_names[@]}"; do
  [[ "$name" == writing ]] && continue
  printf '[%s](../%s/SKILL.md)\n' "$name" "$name" >> \
    "$writing/for-agents/writing/SKILL.md"
done
commit_repo "$writing"

manifest="$workspace/upstreams.tsv"
cat > "$manifest" <<EOF
# name	repository	revision	installed_path	update_policy
agent-skills	$addy	main	upstreams/agent-skills/skills	fast-forward
agency-agents	$agency	main	upstreams/agency-agents	fast-forward
ponytail	$ponytail	main	upstreams/ponytail/skills	fast-forward
writing-skills	$writing	main	upstreams/writing-skills/for-agents	fast-forward
EOF

for command in claude codex curl; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$workspace/bin/$command"
  chmod +x "$workspace/bin/$command"
done

project="$workspace/project"
mkdir -p "$project"
git -C "$project" init -q
cp -a "$repo_root/.harness" "$project/.harness"
cat > "$project/Makefile" <<'EOF'
ARDVI_HARNESS_SHORT_TARGETS := 1
include .harness/harness.mk
EOF

export HOME="$workspace/home"
export PATH="$workspace/bin:$PATH"
export PROJECT_HARNESS_DATA_DIR="$workspace/data"
export HARNESS_UPSTREAM_MANIFEST="$manifest"
export HARNESS_SKIP_SELF_UPDATE=1

make --no-print-directory -C "$project" init > "$workspace/init-1.log"
for skill in writing general-writing humanizer writing-cadence better-usage \
  non-autoregressive-writing-pass academic-voice; do
  [[ -f "$PROJECT_HARNESS_DATA_DIR/upstreams/writing-skills/for-agents/$skill/SKILL.md" ]]
done
[[ -f "$project/.harness/skills/communication/SKILL.md" ]]
cmp "$project/.harness/skills/communication/SKILL.md" \
  "$PROJECT_HARNESS_DATA_DIR/skills/communication/SKILL.md"
[[ -f "$PROJECT_HARNESS_DATA_DIR/skills/communication/.managed-sha256" ]]
[[ -f "$project/AGENTS.md" && -f "$project/CLAUDE.md" ]]
[[ -f "$project/.ardvi/project.json" ]]
[[ -f "$project/.codex/config.toml" && -f "$project/.mcp.json" ]]
[[ -x "$PROJECT_HARNESS_DATA_DIR/bin/ardvi-mcp" ]]
[[ -f "$PROJECT_HARNESS_DATA_DIR/catalog.json" ]]
for skill in communication writing lets-go session-end project-context; do
  [[ -f "$project/.agents/skills/$skill/SKILL.md" ]]
  [[ -f "$project/.claude/skills/$skill/SKILL.md" ]]
done
[[ "$(grep -Fc '<!-- project-harness:communication sha256=' "$project/AGENTS.md")" == 1 ]]
[[ "$(grep -Fc '<!-- project-harness:communication sha256=' "$project/CLAUDE.md")" == 1 ]]
grep -Fq 'session_start' "$project/AGENTS.md"
grep -Fq '@AGENTS.md' "$project/CLAUDE.md"
[[ -f "$PROJECT_HARNESS_DATA_DIR/upstreams/agent-skills/skills/using-agent-skills/SKILL.md" ]]

cp "$project/AGENTS.md" "$workspace/AGENTS.after-first-init"
cp "$project/CLAUDE.md" "$workspace/CLAUDE.after-first-init"
make --no-print-directory -C "$project" init > "$workspace/init-2.log"
cmp "$workspace/AGENTS.after-first-init" "$project/AGENTS.md"
cmp "$workspace/CLAUDE.after-first-init" "$project/CLAUDE.md"

mkdir -p "$project/.agents/skills/project-owned" "$project/.agents/roles/custom"
printf 'project-owned\n' > "$project/.agents/skills/project-owned/SKILL.md"
printf 'custom role\n' > "$project/.agents/roles/custom/keep.md"
printf 'outside managed block\n' >> "$project/AGENTS.md"

printf '\nupdated fixture\n' >> "$writing/for-agents/general-writing/SKILL.md"
commit_repo "$writing"
writing_sha="$(git -C "$writing" rev-parse HEAD)"
make --no-print-directory -C "$project" update > "$workspace/update.log"
grep -Fq "$writing_sha" "$workspace/update.log"
grep -Fq "$writing_sha" "$PROJECT_HARNESS_DATA_DIR/upstreams.lock.tsv"
grep -Fqx 'project-owned' "$project/.agents/skills/project-owned/SKILL.md"
grep -Fqx 'custom role' "$project/.agents/roles/custom/keep.md"
grep -Fqx 'outside managed block' "$project/AGENTS.md"

communication_path="$(make --no-print-directory -C "$project" harness-skill-path SKILL=communication)"
writing_path="$(make --no-print-directory -C "$project" harness-skill-path SKILL=writing)"
[[ "$communication_path" == "$project/.harness/skills/communication/SKILL.md" ]]
[[ "$writing_path" == "$PROJECT_HARNESS_DATA_DIR/upstreams/writing-skills/for-agents/writing/SKILL.md" ]]

grep -Fq 'agency-agents' "$PROJECT_HARNESS_DATA_DIR/upstreams.lock.tsv"
grep -Fq 'fixture' "$PROJECT_HARNESS_DATA_DIR/catalog.json"

grep -Fq 'Do not run `humanizer`' "$project/.harness/skills/communication/SKILL.md"
grep -Fq 'For Russian' "$project/.harness/skills/communication/SKILL.md"
grep -Fq 'code blocks, inline code, commands' "$project/.harness/skills/communication/SKILL.md"
python3 "$project/.harness/scripts/validate_writing_skills.py" \
  "$PROJECT_HARNESS_DATA_DIR/upstreams/writing-skills/for-agents" >/dev/null

sed -i 's/For all user-facing communication/For altered user-facing communication/' \
  "$project/AGENTS.md"
if make --no-print-directory -C "$project" update > "$workspace/conflict.log" 2>&1; then
  echo 'modified managed instruction block was overwritten' >&2
  exit 1
fi
grep -Fq 'refusing to overwrite locally modified managed block' "$workspace/conflict.log"

existing="$workspace/existing"
mkdir -p "$existing/docs/ADR" "$existing/docs/specs" "$existing/.cao/skills/custom" "$existing/.agents/roles"
git -C "$existing" init -q
cp -a "$repo_root/.harness" "$existing/.harness"
printf 'existing agents\n' > "$existing/AGENTS.md"
printf 'existing claude\n' > "$existing/CLAUDE.md"
printf 'existing ADR\n' > "$existing/docs/ADR/keep.md"
printf 'existing spec\n' > "$existing/docs/specs/keep.md"
printf 'custom skill\n' > "$existing/.cao/skills/custom/SKILL.md"
printf 'custom role\n' > "$existing/.agents/roles/custom.md"
cat > "$existing/Makefile" <<'EOF'
include .harness/harness.mk
EOF
make --no-print-directory -C "$existing" harness-bootstrap >/dev/null
grep -Fqx 'existing agents' "$existing/AGENTS.md"
grep -Fqx 'existing claude' "$existing/CLAUDE.md"
grep -Fqx 'existing ADR' "$existing/docs/ADR/keep.md"
grep -Fqx 'existing spec' "$existing/docs/specs/keep.md"
grep -Fqx 'custom skill' "$existing/.cao/skills/custom/SKILL.md"
grep -Fqx 'custom role' "$existing/.agents/roles/custom.md"

echo "writing integration: PASS"
