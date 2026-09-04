#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT

commit_repo() {
  git -C "$1" add .
  git -C "$1" -c user.name=Harness -c user.email=harness@example.invalid commit -qm fixture
}
new_upstream() {
  local path="$workspace/sources/$1"
  mkdir -p "$path"
  git -C "$path" init -q -b main
  printf '%s\n' "$path"
}

mkdir -p "$workspace/sources" "$workspace/bin" "$workspace/data/upstreams" "$workspace/home"
addy="$(new_upstream agent-skills)"
mkdir -p "$addy/skills/using-agent-skills"
printf '%s\n' '---' 'name: using-agent-skills' 'description: fixture' '---' > "$addy/skills/using-agent-skills/SKILL.md"
commit_repo "$addy"
agency="$(new_upstream agency-agents)"
mkdir -p "$agency/engineering"
printf '%s\n' '# Fixture engineer' > "$agency/engineering/fixture.md"
commit_repo "$agency"
ponytail="$(new_upstream ponytail)"
mkdir -p "$ponytail/skills/ponytail"
printf '%s\n' '---' 'name: ponytail' 'description: fixture' '---' > "$ponytail/skills/ponytail/SKILL.md"
commit_repo "$ponytail"
writing="$(new_upstream writing-skills)"
writing_names=(abstract-writing academic-voice better-usage general-writing grant-planning grant-writing humanizer improve-human-writing-guide literature-review non-autoregressive-writing-pass paper-writing presentation-making prompt-improving rebuttal-writing writing writing-cadence)
for name in "${writing_names[@]}"; do
  mkdir -p "$writing/for-agents/$name/agents"
  printf '%s\n' '---' "name: $name" 'description: fixture' '---' > "$writing/for-agents/$name/SKILL.md"
  printf 'interface: fixture\n' > "$writing/for-agents/$name/agents/openai.yaml"
done
mkdir -p "$writing/for-agents/general-writing/references"
printf 'fixture\n' > "$writing/for-agents/general-writing/references/eval.md"
printf '\n[eval](references/eval.md)\n' >> "$writing/for-agents/general-writing/SKILL.md"
for name in "${writing_names[@]}"; do
  [[ "$name" == writing ]] || printf '[%s](../%s/SKILL.md)\n' "$name" "$name" >> "$writing/for-agents/writing/SKILL.md"
done
commit_repo "$writing"

cp -a "$repo_root/.harness/skills" "$workspace/data/skills"
cp -a "$addy" "$workspace/data/upstreams/agent-skills"
cp -a "$agency" "$workspace/data/upstreams/agency-agents"
cp -a "$ponytail" "$workspace/data/upstreams/ponytail"
cp -a "$writing" "$workspace/data/upstreams/writing-skills"
lock="$workspace/data/upstreams.lock.tsv"
printf '# name\trepository\trevision\tresolved_commit\tinstalled_path\tupdate_policy\n' > "$lock"
for name in agent-skills agency-agents ponytail writing-skills; do
  source_path="$workspace/data/upstreams/$name"
  printf '%s\tfixture\tmain\t%s\tupstreams/%s\tfast-forward\n' "$name" "$(git -C "$source_path" rev-parse HEAD)" "$name" >> "$lock"
done
ARDVI_CATALOG_DATA_DIR="$workspace/data" python3 "$repo_root/.harness/scripts/build_catalog.py" >/dev/null
python3 "$repo_root/.harness/scripts/validate_writing_skills.py" "$workspace/data/upstreams/writing-skills/for-agents" >/dev/null
grep -Fq 'using-agent-skills' "$workspace/data/catalog.json"
grep -Fq 'general-writing' "$workspace/data/catalog.json"
grep -Fq 'fixture' "$workspace/data/catalog.json"

for command in codex claude; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$workspace/bin/$command"
  chmod +x "$workspace/bin/$command"
done
cat > "$workspace/bin/docker" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == "compose version" ]]
EOF
cat > "$workspace/bin/ardvi" <<'EOF'
#!/usr/bin/env bash
case "$1 ${2:-}" in
  'service ensure'|'service status'|'healthcheck '|'update ') exit 0 ;;
  'skills list') printf '%s\n' '{"skills":[{"name":"writing","source":"writing-skills"}],"revisions":{"writing-skills":"fixture"}}' ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$workspace/bin/docker" "$workspace/bin/ardvi"

project="$workspace/project"
mkdir -p "$project"
git -C "$project" init -q
cp -a "$repo_root/.harness" "$project/.harness"
printf '%s\n' 'ARDVI_HARNESS_SHORT_TARGETS := 1' 'include .harness/harness.mk' > "$project/Makefile"
export HOME="$workspace/home"
export PATH="$workspace/bin:$PATH"
export HARNESS_SKIP_SELF_UPDATE=1

make --no-print-directory -C "$project" init >/dev/null
for skill in communication writing lets-go session-end project-context skills-list; do
  [[ -f "$project/.agents/skills/$skill/SKILL.md" ]]
  [[ -f "$project/.claude/skills/$skill/SKILL.md" ]]
done
[[ -f "$project/.ardvi/project.json" && -f "$project/.codex/config.toml" && -f "$project/.mcp.json" ]]
first_agents="$(sha256sum "$project/AGENTS.md")"
first_claude="$(sha256sum "$project/CLAUDE.md")"
make --no-print-directory -C "$project" init >/dev/null
[[ "$first_agents" == "$(sha256sum "$project/AGENTS.md")" ]]
[[ "$first_claude" == "$(sha256sum "$project/CLAUDE.md")" ]]

mkdir -p "$project/.agents/skills/project-owned"
printf 'project-owned\n' > "$project/.agents/skills/project-owned/SKILL.md"
printf 'outside managed block\n' >> "$project/AGENTS.md"
make --no-print-directory -C "$project" update >/dev/null
grep -Fqx 'project-owned' "$project/.agents/skills/project-owned/SKILL.md"
grep -Fqx 'outside managed block' "$project/AGENTS.md"
grep -Fq 'Do not run `humanizer`' "$project/.harness/skills/communication/SKILL.md"
grep -Fq 'For Russian' "$project/.harness/skills/communication/SKILL.md"
[[ "$(make --no-print-directory -C "$project" harness-skill-path SKILL=writing)" == "$project/.harness/skills/writing/SKILL.md" ]]

sed -i 's/For all user-facing communication/For altered user-facing communication/' "$project/AGENTS.md"
if make --no-print-directory -C "$project" update >/dev/null 2>&1; then
  echo 'modified managed instruction block was overwritten' >&2
  exit 1
fi

echo "writing integration: PASS"
