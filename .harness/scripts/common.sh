#!/usr/bin/env bash
set -Eeuo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if git_root="$(git -C "$HARNESS_DIR" rev-parse --show-toplevel 2>/dev/null)"; then
  REPO_ROOT="$git_root"
else
  REPO_ROOT="$(cd "$HARNESS_DIR/.." && pwd)"
fi
TEMPLATES_DIR="$HARNESS_DIR/templates"
HARNESS_DATA_DIR="${PROJECT_HARNESS_DATA_DIR:-$HOME/.local/share/project-harness}"
UPSTREAMS_DIR="$HARNESS_DATA_DIR/upstreams"
HARNESS_BIN_DIR="${PROJECT_HARNESS_BIN_DIR:-$HARNESS_DATA_DIR/bin}"
HUB_STATE_DIR="$HARNESS_DATA_DIR/hub"
HUB_DATA_DIR="$HARNESS_DATA_DIR/data"
HUB_CATALOG="$HARNESS_DATA_DIR/catalog.json"
UPSTREAM_MANIFEST="${HARNESS_UPSTREAM_MANIFEST:-$HARNESS_DIR/upstreams.tsv}"
UPSTREAM_LOCK="$HARNESS_DATA_DIR/upstreams.lock.tsv"

export PATH="$HARNESS_BIN_DIR:$HOME/.local/bin:$PATH"

project_slug() {
  if [[ -n "${PROJECT_SLUG:-}" ]]; then
    printf '%s' "$PROJECT_SLUG" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
    return
  fi

  local env_file
  for env_file in "$REPO_ROOT/.agents/project.env"; do
    [[ -f "$env_file" ]] || continue
    local saved
    saved="$(sed -n 's/^PROJECT_SLUG=//p' "$env_file" | head -n 1)"
    if [[ "$saved" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
      printf '%s' "$saved"
      return
    fi
  done

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
