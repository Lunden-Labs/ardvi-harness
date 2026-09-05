#!/usr/bin/env bash
# One-time bootstrap for installations whose CLI cannot update itself yet.
set -Eeuo pipefail
command -v python3 >/dev/null || { echo "Install Python 3.10+ first." >&2; exit 1; }
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/Lunden-Labs/ardvi-harness/main/.harness/scripts/update_release.py \
  -o "$temporary/update_release.py"
python3 "$temporary/update_release.py" "$@"
