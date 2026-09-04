#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

command -v git >/dev/null 2>&1 || { echo "git is required to update external skills" >&2; exit 1; }
[[ -f "$UPSTREAM_MANIFEST" ]] || { echo "Upstream manifest is missing: $UPSTREAM_MANIFEST" >&2; exit 1; }
mkdir -p "$UPSTREAMS_DIR"
if [[ -f "$HUB_STATE_DIR/server.pid" ]] && kill -0 "$(<"$HUB_STATE_DIR/server.pid")" 2>/dev/null; then
  echo "Stop the Ardvi MCP hub with make down before updating skills." >&2
  exit 1
fi
stage="$(mktemp -d "$HARNESS_DATA_DIR/.upstreams-stage.XXXXXX")"
backup="$(mktemp -d "$HARNESS_DATA_DIR/.upstreams-backup.XXXXXX")"
mkdir -p "$stage/upstreams"
activation_started=0
success=0
cleanup(){
  rm -rf "$stage"
  if ((success || !activation_started)); then
    rm -rf "$backup"
  else
    echo "Update failed; recovery backup preserved: $backup" >&2
  fi
}
trap cleanup EXIT

names=(); repositories=(); revisions=(); installed_paths=(); policies=(); commits=(); statuses=()
while IFS=$'\t' read -r name repository revision installed_path policy extra; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  if [[ -n "${extra:-}" || -z "$repository" || -z "$revision" || -z "$installed_path" || "$policy" != fast-forward ]]; then
    echo "Invalid upstream manifest row for: $name" >&2; exit 1
  fi
  if [[ ! "$name" =~ ^[a-z0-9][a-z0-9-]*$ || "$installed_path" != "upstreams/$name" && "$installed_path" != "upstreams/$name/"* ]]; then
    echo "Unsafe upstream name or installed path: $name $installed_path" >&2; exit 1
  fi
  target="$UPSTREAMS_DIR/$name"
  before=""
  if [[ -d "$target/.git" ]]; then
    actual_url="$(git -C "$target" remote get-url origin)"
    [[ "$actual_url" == "$repository" || "$actual_url" == "${repository%.git}" ]] || { echo "Unexpected origin for $target: $actual_url" >&2; exit 1; }
    [[ -z "$(git -C "$target" status --porcelain)" ]] || { echo "Refusing to update modified managed checkout: $target" >&2; exit 1; }
    before="$(git -C "$target" rev-parse HEAD)"
  elif [[ -e "$target" ]]; then
    echo "Refusing to replace non-Git path: $target" >&2; exit 1
  fi
  git clone --quiet --branch "$revision" "$repository" "$stage/upstreams/$name"
  after="$(git -C "$stage/upstreams/$name" rev-parse HEAD)"
  if [[ -n "$before" ]] && ! git -C "$stage/upstreams/$name" merge-base --is-ancestor "$before" "$after"; then
    echo "Refusing non-fast-forward upstream update for $name: $before -> $after" >&2
    exit 1
  fi
  [[ -d "$stage/$installed_path" ]] || { echo "Managed upstream $name is missing installed path: $installed_path" >&2; exit 1; }
  names+=("$name"); repositories+=("$repository"); revisions+=("$revision"); installed_paths+=("$installed_path"); policies+=("$policy"); commits+=("$after")
  if [[ -z "$before" ]]; then statuses+=("cloned"); elif [[ "$before" == "$after" ]]; then statuses+=("current"); else statuses+=("updated ${before:0:12}..${after:0:12}"); fi
done < "$UPSTREAM_MANIFEST"

((${#names[@]})) || { echo "Upstream manifest contains no repositories" >&2; exit 1; }
python3 "$HARNESS_DIR/scripts/validate_writing_skills.py" "$stage/upstreams/writing-skills/for-agents"

staged_lock="$stage/upstreams.lock.tsv"
printf '# name\trepository\trevision\tresolved_commit\tinstalled_path\tupdate_policy\n' > "$staged_lock"
for index in "${!names[@]}"; do
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "${names[$index]}" "${repositories[$index]}" "${revisions[$index]}" "${commits[$index]}" "${installed_paths[$index]}" "${policies[$index]}" >> "$staged_lock"
done
HARNESS_CATALOG_SCAN_UPSTREAMS="$stage/upstreams" \
HARNESS_CATALOG_ROOT_UPSTREAMS="$UPSTREAMS_DIR" \
HARNESS_CATALOG_LOCK="$staged_lock" \
HARNESS_CATALOG_OUTPUT="$stage/catalog.json" \
  python3 "$HARNESS_DIR/scripts/build_catalog.py"

activated=()
metadata_moved=()
activation_started=1
rollback(){
  for name in "${activated[@]}"; do rm -rf "$UPSTREAMS_DIR/$name"; [[ ! -e "$backup/$name" ]] || mv "$backup/$name" "$UPSTREAMS_DIR/$name"; done
  for file in "${metadata_moved[@]}"; do
    rm -f "$HARNESS_DATA_DIR/$file"
    [[ ! -e "$backup/$file" ]] || mv "$backup/$file" "$HARNESS_DATA_DIR/$file"
  done
}
for name in "${names[@]}"; do
  if [[ -e "$UPSTREAMS_DIR/$name" ]] && ! mv "$UPSTREAMS_DIR/$name" "$backup/$name"; then rollback; exit 1; fi
  if ! mv "$stage/upstreams/$name" "$UPSTREAMS_DIR/$name"; then
    [[ ! -e "$backup/$name" ]] || mv "$backup/$name" "$UPSTREAMS_DIR/$name"
    rollback; exit 1
  fi
  activated+=("$name")
done
for file in upstreams.lock.tsv catalog.json; do
  if [[ -e "$HARNESS_DATA_DIR/$file" ]] && ! mv "$HARNESS_DATA_DIR/$file" "$backup/$file"; then rollback; exit 1; fi
  metadata_moved+=("$file")
done
if ! mv "$stage/upstreams.lock.tsv" "$UPSTREAM_LOCK" || ! mv "$stage/catalog.json" "$HUB_CATALOG"; then rollback; exit 1; fi
success=1

echo "Installed external revisions:"
for index in "${!names[@]}"; do printf '  %-16s %s  %s\n' "${names[$index]}" "${commits[$index]}" "${statuses[$index]}"; done
echo "Revision lock: $UPSTREAM_LOCK"
