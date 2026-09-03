#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if ! command -v git >/dev/null 2>&1; then
  echo "git is required to update external skills and profiles" >&2
  exit 1
fi

if [[ ! -f "$UPSTREAM_MANIFEST" ]]; then
  echo "Upstream manifest is missing: $UPSTREAM_MANIFEST" >&2
  exit 1
fi

mkdir -p "$UPSTREAMS_DIR" "$(dirname "$AGENCY_CAO_DIR")"

names=()
repositories=()
revisions=()
installed_paths=()
policies=()
statuses=()
commits=()

sync_repo() {
  local name="$1"
  local url="$2"
  local revision="$3"
  local target="$UPSTREAMS_DIR/$name"
  local before=""

  if [[ -d "$target/.git" ]]; then
    local actual_url
    actual_url="$(git -C "$target" remote get-url origin)"
    if [[ "$actual_url" != "$url" && "$actual_url" != "${url%.git}" ]]; then
      echo "Unexpected origin for $target: $actual_url" >&2
      exit 1
    fi
    if [[ -n "$(git -C "$target" status --porcelain)" ]]; then
      echo "Refusing to update modified managed checkout: $target" >&2
      exit 1
    fi
    before="$(git -C "$target" rev-parse HEAD)"
    if [[ "$(git -C "$target" symbolic-ref --short HEAD)" != "$revision" ]]; then
      echo "Unexpected branch for $target; expected $revision" >&2
      exit 1
    fi
    git -C "$target" pull --ff-only --prune origin "$revision"
  elif [[ -e "$target" ]]; then
    echo "Refusing to replace non-Git path: $target" >&2
    exit 1
  else
    git clone --depth 1 --branch "$revision" "$url" "$target"
  fi

  local after
  after="$(git -C "$target" rev-parse HEAD)"
  if [[ -z "$before" ]]; then
    statuses+=("cloned")
  elif [[ "$before" == "$after" ]]; then
    statuses+=("current")
  else
    statuses+=("updated ${before:0:12}..${after:0:12}")
  fi
  commits+=("$after")
}

while IFS=$'\t' read -r name repository revision installed_path policy extra; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  if [[ -n "${extra:-}" || -z "$repository" || -z "$revision" || -z "$installed_path" || -z "$policy" ]]; then
    echo "Invalid upstream manifest row for: $name" >&2
    exit 1
  fi
  case "$policy" in
    fast-forward|fast-forward+generate-cao-profiles) ;;
    *)
      echo "Unsupported update policy for $name: $policy" >&2
      exit 1
      ;;
  esac
  names+=("$name")
  repositories+=("$repository")
  revisions+=("$revision")
  installed_paths+=("$installed_path")
  policies+=("$policy")
  sync_repo "$name" "$repository" "$revision"
  expected="$HARNESS_DATA_DIR/$installed_path"
  if [[ ! -d "$expected" ]]; then
    echo "Managed upstream $name is missing installed path: $expected" >&2
    exit 1
  fi
done < "$UPSTREAM_MANIFEST"

if ((${#names[@]} == 0)); then
  echo "Upstream manifest contains no repositories: $UPSTREAM_MANIFEST" >&2
  exit 1
fi

python3 "$HARNESS_DIR/scripts/generate_agency_profiles.py" \
  "$UPSTREAMS_DIR/agency-agents" \
  "$AGENCY_CAO_DIR"

python3 "$HARNESS_DIR/scripts/validate_writing_skills.py" \
  "$UPSTREAMS_DIR/writing-skills/for-agents"

echo "Installed external revisions:"
for index in "${!names[@]}"; do
  printf '  %-15s %s  %s\n' "${names[$index]}" "${commits[$index]}" "${statuses[$index]}"
done

lock_tmp="$(mktemp "${HARNESS_DATA_DIR}/.upstreams.lock.XXXXXX")"
trap 'rm -f "$lock_tmp"' EXIT
printf '# name\trepository\trevision\tresolved_commit\tinstalled_path\tupdate_policy\n' > "$lock_tmp"
for index in "${!names[@]}"; do
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${names[$index]}" \
    "${repositories[$index]}" \
    "${revisions[$index]}" \
    "${commits[$index]}" \
    "${installed_paths[$index]}" \
    "${policies[$index]}" >> "$lock_tmp"
done
mv "$lock_tmp" "$UPSTREAM_LOCK"
trap - EXIT
echo "Revision lock: $UPSTREAM_LOCK"
