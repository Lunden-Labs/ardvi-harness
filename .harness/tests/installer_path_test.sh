#!/usr/bin/env bash
set -Eeuo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/release/harness/.harness" "$fixture/tools" "$fixture/user"
cp "$repo_root/install.sh" "$fixture/release/install.sh"
printf '#!/bin/sh\nexit 0\n' > "$fixture/tools/docker"
printf '#!/bin/sh\nexit 0\n' > "$fixture/release/ardvi"
chmod +x "$fixture/tools/docker" "$fixture/release/ardvi"
base_path="$fixture/tools:/usr/bin:/bin"
run_install() {
  env -u ARDVI_BIN_DIR -u ARDVI_DATA_DIR -u ZDOTDIR HOME="$fixture/user" \
    SHELL=/bin/bash PATH="$base_path" "$@" bash "$fixture/release/install.sh" > "$fixture/output"
}
check_path() {
  env HOME="$fixture/user" PATH=/usr/bin:/bin EXPECTED_BIN="$1" \
    bash --noprofile --norc -c '
      set -eu
      source "$2"
      [[ "$(command -v ardvi)" == "$EXPECTED_BIN/ardvi" ]]
      original=$PATH
      source "$2"
      [[ "$PATH" == "$original" ]]
    ' bash unused "$2"
}

# A transient PATH entry must not suppress persistence in fresh shells.
run_install PATH="$fixture/user/.local/bin:$base_path"
[[ -f "$fixture/user/.bashrc" && -f "$fixture/user/.profile" ]]
check_path "$fixture/user/.local/bin" "$fixture/user/.bashrc"
check_path "$fixture/user/.local/bin" "$fixture/user/.profile"
cp "$fixture/user/.bashrc" "$fixture/bashrc.before"
cp "$fixture/user/.profile" "$fixture/profile.before"
run_install
cmp "$fixture/bashrc.before" "$fixture/user/.bashrc"
cmp "$fixture/profile.before" "$fixture/user/.profile"

# Existing standard exports and unrelated settings remain byte-identical.
printf 'export PATH="$HOME/.local/bin:$PATH"\n# user setting\n' > "$fixture/user/.bashrc"
cp "$fixture/user/.bashrc" "$fixture/bashrc.before"
run_install
cmp "$fixture/bashrc.before" "$fixture/user/.bashrc"

# Honor Bash login-file precedence without creating a shadowing .bash_profile.
printf '# login settings\n' > "$fixture/user/.bash_login"
run_install
[[ ! -e "$fixture/user/.bash_profile" ]]
check_path "$fixture/user/.local/bin" "$fixture/user/.bash_login"

# Quote custom directories as data, including spaces, apostrophes and dollars.
cp "$fixture/profile.before" "$fixture/user/.bashrc"
custom_bin="$fixture/custom ' \$(false) bin"
run_install ARDVI_BIN_DIR="$custom_bin"
check_path "$custom_bin" "$fixture/user/.bashrc"
cp "$fixture/user/.bashrc" "$fixture/bashrc.before"
run_install ARDVI_BIN_DIR="$custom_bin"
cmp "$fixture/bashrc.before" "$fixture/user/.bashrc"

# Preserve a user's existing absolute custom export, not only our own spelling.
custom_bin="$fixture/simple custom bin"
printf 'export PATH="%s:$PATH"\n' "$custom_bin" > "$fixture/user/.bashrc"
cp "$fixture/user/.bashrc" "$fixture/bashrc.before"
run_install ARDVI_BIN_DIR="$custom_bin"
cmp "$fixture/bashrc.before" "$fixture/user/.bashrc"

# Zsh honors ZDOTDIR; the generated fragment is also valid POSIX shell.
run_install SHELL=/bin/zsh ZDOTDIR="$fixture/zsh config"
[[ -f "$fixture/zsh config/.zshrc" ]]
check_path "$fixture/user/.local/bin" "$fixture/zsh config/.zshrc"
env HOME="$fixture/user" PATH=/usr/bin:/bin sh -c '. "$1"; command -v ardvi' sh \
  "$fixture/zsh config/.zshrc" | grep -Fx "$fixture/user/.local/bin/ardvi"

# Unsupported shells get instructions, not Bash code in their configuration.
run_install SHELL=/usr/bin/fish
[[ ! -e "$fixture/user/.config/fish/config.fish" ]]
grep -Fq 'PATH was not configured' "$fixture/output"

# An unwritable startup target cannot report complete installation success.
mkdir -p "$fixture/blocked/.zshrc"
if run_install SHELL=/bin/zsh ZDOTDIR="$fixture/blocked" 2> "$fixture/warnings"; then
  echo 'Startup-file write failure was accepted' >&2; exit 1
fi
grep -Fq 'PATH could not be configured' "$fixture/warnings"

# An earlier executable is diagnosed without deleting it or reordering PATH.
cp "$fixture/release/ardvi" "$fixture/tools/ardvi"
run_install 2> "$fixture/warnings"
grep -Fq "current PATH resolves ardvi to $fixture/tools/ardvi" "$fixture/warnings"
[[ -x "$fixture/tools/ardvi" ]]

# Failed service installation leaves startup files untouched.
cp "$fixture/user/.bashrc" "$fixture/bashrc.before"
printf '#!/bin/sh\nexit 23\n' > "$fixture/release/ardvi"
if run_install; then echo 'Failed installation was accepted' >&2; exit 1; fi
cmp "$fixture/bashrc.before" "$fixture/user/.bashrc"

# Invalid PATH entries fail before invoking the service or changing dotfiles.
if run_install ARDVI_BIN_DIR="$fixture/invalid:bin" 2> "$fixture/warnings"; then
  echo 'Colon in PATH entry was accepted' >&2; exit 1
fi
grep -Fq 'cannot contain colons' "$fixture/warnings"
echo 'installer PATH tests passed'
