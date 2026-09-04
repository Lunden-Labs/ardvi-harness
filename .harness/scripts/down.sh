#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

pid_file="$HUB_STATE_DIR/server.pid"
if [[ ! -f "$pid_file" ]]; then
  echo "Ardvi MCP is not managed by this harness"
  exit 0
fi

pid="$(cat "$pid_file")"
if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
  echo "Invalid Ardvi MCP PID file: $pid_file" >&2
  exit 1
fi
if ! kill -0 "$pid" 2>/dev/null; then
  rm -f "$pid_file"
  echo "Ardvi MCP was already stopped"
  exit 0
fi
if ! ps -p "$pid" -o args= | grep -Fq "$HARNESS_BIN_DIR/ardvi-mcp serve"; then
  echo "Refusing to signal unrelated PID $pid; remove stale file $pid_file manually." >&2
  exit 1
fi

kill "$pid"
for _ in {1..50}; do
  if ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$pid_file"
    echo "Ardvi MCP stopped"
    exit 0
  fi
  sleep 0.1
done

echo "Ardvi MCP did not stop after SIGTERM (PID $pid)" >&2
exit 1
