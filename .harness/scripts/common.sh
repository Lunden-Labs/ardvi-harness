#!/usr/bin/env bash
set -Eeuo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if git_root="$(git -C "$HARNESS_DIR" rev-parse --show-toplevel 2>/dev/null)"; then
  REPO_ROOT="$git_root"
else
  REPO_ROOT="$(cd "$HARNESS_DIR/.." && pwd)"
fi
# shellcheck disable=SC2034 # consumed by scripts that source this file
TEMPLATES_DIR="$HARNESS_DIR/templates"
export PATH="$HOME/.local/bin:$PATH"

project_slug() {
  if [[ -n "${PROJECT_SLUG:-}" ]]; then
    printf '%s' "$PROJECT_SLUG" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
    return
  fi

  local env_file="$REPO_ROOT/.agents/project.env"
  if [[ -f "$env_file" ]]; then
    local saved
    saved="$(sed -n 's/^PROJECT_SLUG=//p' "$env_file" | head -n 1)"
    if [[ "$saved" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
      printf '%s' "$saved"
      return
    fi
  fi

  basename "$REPO_ROOT" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
}

require_project_slug() {
  local slug
  slug="$(project_slug)"
  if [[ -z "$slug" ]]; then
    echo "Cannot derive PROJECT_SLUG. Set PROJECT_SLUG explicitly." >&2
    exit 1
  fi
  printf '%s' "$slug"
}
