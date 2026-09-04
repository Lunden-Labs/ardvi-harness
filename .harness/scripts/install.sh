#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

for cmd in python3 git go sha256sum curl ps; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing dependency: $cmd" >&2
    echo "Install Python 3, Git, Go, and coreutils, then retry." >&2
    exit 1
  fi
done

python3 - <<'PY'
import sys
if sys.version_info < (3, 10):
    raise SystemExit("Python 3.10 or newer is required")
PY

if [[ -f "$HUB_STATE_DIR/server.pid" ]] && kill -0 "$(<"$HUB_STATE_DIR/server.pid")" 2>/dev/null; then
  echo "Stop the Ardvi MCP hub with make down before installing or updating." >&2
  exit 1
fi

mkdir -p "$HARNESS_BIN_DIR"
go build -C "$HARNESS_DIR/mcp" -o "$HARNESS_BIN_DIR/ardvi-mcp" ./cmd/ardvi-mcp
bash "$HARNESS_DIR/scripts/install_managed_skills.sh"
bash "$HARNESS_DIR/scripts/install_upstreams.sh"
echo "Ardvi MCP installed: $HARNESS_BIN_DIR/ardvi-mcp"
