#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

binary="$HARNESS_BIN_DIR/ardvi-mcp"
port=8765
pid_file="$HUB_STATE_DIR/server.pid"
health="http://127.0.0.1:$port/healthz"

if [[ -x "$binary" ]]; then
  "$binary" --version
else
  echo "Ardvi MCP: not installed"
fi

if curl -fsS "$health" 2>/dev/null | grep -q '"status":"ok"'; then
  echo "Ardvi MCP: running at http://127.0.0.1:$port/mcp"
else
  echo "Ardvi MCP: stopped"
fi

if [[ -f "$pid_file" ]]; then
  echo "PID: $(cat "$pid_file")"
fi
echo "Storage: $HUB_DATA_DIR"
if [[ -f "$UPSTREAM_LOCK" ]]; then
  echo "Upstream revisions:"
  awk -F '\t' '!/^#/ {printf "  %-16s %s\n", $1, $4}' "$UPSTREAM_LOCK"
fi
