#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

slug="$(require_project_slug)"
created=()
preserved=()

create_from_template() {
  local template="$1"
  local target="$2"
  if [[ -e "$target" ]]; then
    preserved+=("${target#$REPO_ROOT/}")
    return
  fi
  mkdir -p "$(dirname "$target")"
  cp "$template" "$target"
  created+=("${target#$REPO_ROOT/}")
}

render_from_template() {
  local template="$1"
  local target="$2"
  if [[ -e "$target" ]]; then
    preserved+=("${target#$REPO_ROOT/}")
    return
  fi
  mkdir -p "$(dirname "$target")"
  local tmp
  tmp="$(mktemp "${target}.tmp.XXXXXX")"
  sed "s/__PROJECT_SLUG__/$slug/g" "$template" > "$tmp"
  mv "$tmp" "$target"
  created+=("${target#$REPO_ROOT/}")
}

detect_existing_dir() {
  local kind="$1"
  local candidates=()
  if [[ "$kind" == "adr" ]]; then
    candidates=("docs/adr" "docs/adrs" "docs/ADR" "docs/ADRs" "adr" "adrs" "ADR" "ADRs")
  else
    candidates=("docs/specs" "docs/spec" "docs/specifications" "specs" "spec" "specifications")
  fi

  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -d "$REPO_ROOT/$candidate" ]]; then
      printf '%s' "$REPO_ROOT/$candidate"
      return 0
    fi
  done

  while IFS= read -r candidate; do
    local base lower
    base="$(basename "$candidate")"
    lower="${base,,}"
    if [[ "$kind" == "adr" && ( "$lower" == "adr" || "$lower" == "adrs" ) ]]; then
      printf '%s' "$candidate"
      return 0
    fi
    if [[ "$kind" == "spec" && ( "$lower" == "spec" || "$lower" == "specs" || "$lower" == "specification" || "$lower" == "specifications" ) ]]; then
      printf '%s' "$candidate"
      return 0
    fi
  done < <(find "$REPO_ROOT" -maxdepth 3 -type d \
    -not -path '*/.git/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' -not -path '*/.harness/*' | sort)
  return 1
}

create_from_template "$TEMPLATES_DIR/project/AGENTS.md" "$REPO_ROOT/AGENTS.md"
create_from_template "$TEMPLATES_DIR/project/CLAUDE.md" "$REPO_ROOT/CLAUDE.md"
HARNESS_REPO_ROOT="$REPO_ROOT" python3 "$HARNESS_DIR/scripts/sync_instructions.py"

if adr_dir="$(detect_existing_dir adr)"; then
  preserved+=("${adr_dir#$REPO_ROOT/}/ (existing ADR directory; untouched)")
else
  create_from_template "$TEMPLATES_DIR/project/docs/adr/README.md" "$REPO_ROOT/docs/adr/README.md"
  create_from_template "$TEMPLATES_DIR/project/docs/adr/ADR-0000-template.md" "$REPO_ROOT/docs/adr/ADR-0000-template.md"
fi

if specs_dir="$(detect_existing_dir spec)"; then
  preserved+=("${specs_dir#$REPO_ROOT/}/ (existing specs directory; untouched)")
else
  create_from_template "$TEMPLATES_DIR/project/docs/specs/README.md" "$REPO_ROOT/docs/specs/README.md"
  create_from_template "$TEMPLATES_DIR/project/docs/specs/SPEC-0000-template.md" "$REPO_ROOT/docs/specs/SPEC-0000-template.md"
fi

if [[ -d "$REPO_ROOT/tasks" ]]; then
  preserved+=("tasks/ (existing task directory; untouched)")
else
  create_from_template "$TEMPLATES_DIR/project/tasks/README.md" "$REPO_ROOT/tasks/README.md"
fi

mkdir -p "$REPO_ROOT/.cao/agents" "$REPO_ROOT/.cao/skills"
if [[ ! -e "$REPO_ROOT/.cao/project.env" ]]; then
  printf 'PROJECT_SLUG=%s\n' "$slug" > "$REPO_ROOT/.cao/project.env"
  created+=(".cao/project.env")
else
  preserved+=(".cao/project.env")
fi

for profile in architect backend-claude backend-codex reviewer-claude reviewer-codex; do
  render_from_template \
    "$TEMPLATES_DIR/agents/${profile}.md.tpl" \
    "$REPO_ROOT/.cao/agents/${slug}-${profile}.md"
done

render_from_template \
  "$TEMPLATES_DIR/skills/project-context/SKILL.md.tpl" \
  "$REPO_ROOT/.cao/skills/${slug}-project-context/SKILL.md"

render_from_template \
  "$TEMPLATES_DIR/skills/external-catalog/SKILL.md.tpl" \
  "$REPO_ROOT/.cao/skills/${slug}-external-catalog/SKILL.md"

echo "Project slug: $slug"
if ((${#created[@]})); then
  printf 'Created:   %s\n' "${created[@]}"
fi
if ((${#preserved[@]})); then
  printf 'Preserved: %s\n' "${preserved[@]}"
fi

if command -v cao >/dev/null 2>&1; then
  python3 "$HARNESS_DIR/scripts/register_cao.py"
else
  echo "CAO is not installed; project files are ready. Run: make init"
fi
