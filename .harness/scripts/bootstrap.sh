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
    lower="$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]')"
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
HARNESS_REPO_ROOT="$REPO_ROOT" python3 "$HARNESS_DIR/scripts/project_config.py"

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

mkdir -p "$REPO_ROOT/.agents/skills" "$REPO_ROOT/.claude/skills"
if [[ ! -e "$REPO_ROOT/.agents/project.env" ]]; then
  printf 'PROJECT_SLUG=%s\n' "$slug" > "$REPO_ROOT/.agents/project.env"
  created+=(".agents/project.env")
else
  preserved+=(".agents/project.env")
fi

python3 "$HARNESS_DIR/scripts/install_project_skills.py"

if [[ -n "${PROMPT:-}" && -n "${PROMPT_FILE:-}" ]]; then
  echo "Set only PROMPT or PROMPT_FILE, not both." >&2
  exit 1
fi
if [[ -n "${PROMPT_FILE:-}" ]]; then
  [[ -f "$PROMPT_FILE" ]] || { echo "PROMPT_FILE not found: $PROMPT_FILE" >&2; exit 1; }
  PROMPT="$(<"$PROMPT_FILE")"
fi
if [[ -n "${PROMPT:-}" ]]; then
  if [[ -e "$REPO_ROOT/tasks/NEXT.md" ]]; then
    echo "Refusing to replace existing tasks/NEXT.md; add the prompt manually." >&2
    exit 1
  fi
  printf '# Next task\n\n%s\n' "$PROMPT" > "$REPO_ROOT/tasks/NEXT.md"
  created+=("tasks/NEXT.md")
fi

echo "Project slug: $slug"
if ((${#created[@]})); then
  printf 'Created:   %s\n' "${created[@]}"
fi
if ((${#preserved[@]})); then
  printf 'Preserved: %s\n' "${preserved[@]}"
fi
