#!/usr/bin/env bash
set -Eeuo pipefail

source_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bin_dir="${ARDVI_BIN_DIR:-$HOME/.local/bin}"
data_root="${ARDVI_DATA_DIR:-$HOME/.local/share/ardvi}"
target="$data_root/harness"

# A PATH entry cannot represent a colon; keep generated startup code one line.
[[ "$bin_dir" != *:* && "$bin_dir" != *$'\n'* && "$bin_dir" != *$'\r'* ]] || {
  echo "ARDVI_BIN_DIR cannot contain colons or line breaks." >&2
  exit 1
}

[[ -x "$source_dir/ardvi" && -d "$source_dir/harness/.harness" ]] || {
  echo "Run this installer from an extracted Ardvi release archive." >&2
  exit 1
}
command -v docker >/dev/null 2>&1 || { echo "Install Docker Desktop or Docker Engine with Compose first." >&2; exit 1; }
docker compose version >/dev/null
mkdir -p "$bin_dir" "$data_root"
bin_dir="$(cd "$bin_dir" && pwd)"
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

# Do not infer persistence from this process's PATH: it may be a one-off export.
quoted_bin="'${bin_dir//\'/\'\\\'\'}'"
if [[ "$bin_dir" == "$HOME/.local/bin" ]]; then
  quoted_bin='"$HOME/.local/bin"'
fi
path_export="export PATH=$quoted_bin:\"\$PATH\""
configure_path() {
  local rc_file="$1"
  # Recognize our guard and the direct export previously documented for users.
  if [[ -f "$rc_file" ]] && {
    grep -Fqx "  *) $path_export ;;" "$rc_file" ||
    grep -Fqx "$path_export" "$rc_file" ||
    { [[ "$bin_dir" == "$HOME/.local/bin" ]] &&
      grep -Fqx 'export PATH="$HOME/.local/bin:$PATH"' "$rc_file"; }
  }; then
    return
  fi
  # Preserve common absolute-path exports too, without evaluating user dotfiles.
  if [[ "$bin_dir" != *[\$\`\"\\]* && -f "$rc_file" ]] && {
    grep -Fqx "export PATH=\"$bin_dir:\$PATH\"" "$rc_file" ||
    grep -Fqx "export PATH=\"\$PATH:$bin_dir\"" "$rc_file"
  }; then
    return
  fi
  if ! { mkdir -p "$(dirname "$rc_file")" &&
    printf '\n# Ardvi PATH\ncase ":$PATH:" in\n  *:%s:*) ;;\n  *) %s ;;\nesac\n' \
      "$quoted_bin" "$path_export" >> "$rc_file"; }; then
    echo "Ardvi installed, but PATH could not be configured in $rc_file." >&2
    echo "Add this to your shell configuration: $path_export" >&2
    return 1
  fi
  echo "Configured PATH in $rc_file"
}

case "${SHELL##*/}" in
  bash)
    configure_path "$HOME/.bashrc"
    # Bash reads only the first existing login file. Never shadow a user's file.
    login_file="$HOME/.profile"
    for candidate in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
      if [[ -e "$candidate" ]]; then login_file="$candidate"; break; fi
    done
    configure_path "$login_file"
    ;;
  zsh) configure_path "${ZDOTDIR:-$HOME}/.zshrc" ;;
  *) echo "PATH was not configured for ${SHELL:-an unknown shell}; add $bin_dir to its startup configuration." ;;
esac

resolved_ardvi="$(command -v ardvi || true)"
if [[ -n "$resolved_ardvi" && ! "$resolved_ardvi" -ef "$bin_dir/ardvi" ]]; then
  echo "Warning: current PATH resolves ardvi to $resolved_ardvi, not $bin_dir/ardvi." >&2
fi
echo 'Open a new terminal, or for this Bash/Zsh terminal run:'
echo "  $path_export"

echo
echo "Installed. In a Git project run:"
echo "  ardvi init --path /path/to/project"
