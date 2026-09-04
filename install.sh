#!/usr/bin/env bash
set -Eeuo pipefail

source_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin_dir="${ARDVI_BIN_DIR:-$HOME/.local/bin}"
data_root="${ARDVI_DATA_DIR:-$HOME/.local/share/ardvi}"
target="$data_root/harness"

[[ -x "$source_dir/ardvi" && -d "$source_dir/harness/.harness" ]] || {
  echo "Run this installer from an extracted Ardvi release archive." >&2
  exit 1
}
command -v docker >/dev/null 2>&1 || { echo "Install Docker Desktop or Docker Engine with Compose first." >&2; exit 1; }
docker compose version >/dev/null
mkdir -p "$bin_dir" "$data_root"
staging="$(mktemp -d "$data_root/.install.XXXXXX")"
promoted=0
cleanup() {
  if (( ! promoted )); then
    if [[ -e "$staging/previous-harness" ]]; then
      [[ ! -e "$target" ]] || mv "$target" "$staging/failed-harness"
      mv "$staging/previous-harness" "$target"
    fi
    if [[ -e "$staging/previous-ardvi" ]]; then
      [[ ! -e "$bin_dir/ardvi" ]] || mv "$bin_dir/ardvi" "$staging/failed-ardvi"
      mv "$staging/previous-ardvi" "$bin_dir/ardvi"
    fi
  fi
  rm -rf "$staging"
}
trap cleanup EXIT
cp -a "$source_dir/harness" "$staging/harness"
install -m 0755 "$source_dir/ardvi" "$staging/ardvi"

# Update the service before replacing the working CLI/bundle. The CLI itself
# keeps the previous Compose configuration when the new image is unhealthy.
"$staging/ardvi" install "$@"

if [[ -e "$target" ]]; then
  mv "$target" "$staging/previous-harness"
fi
mv "$staging/harness" "$target"
if [[ -e "$bin_dir/ardvi" ]]; then
  mv "$bin_dir/ardvi" "$staging/previous-ardvi"
fi
mv "$staging/ardvi" "$bin_dir/ardvi"
promoted=1

echo
echo "Installed. In a Git project run:"
echo "  ardvi init --path /path/to/project"
