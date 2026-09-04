#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

binary="$HARNESS_BIN_DIR/ardvi-mcp"
port=8765
pid_file="$HUB_STATE_DIR/server.pid"
log_file="$HUB_STATE_DIR/server.log"
health="http://127.0.0.1:$port/healthz"

if [[ ! -x "$binary" ]]; then
  echo "Ardvi MCP is missing. Run: make init" >&2
  exit 1
fi
mkdir -p "$HUB_STATE_DIR" "$HUB_DATA_DIR"
if [[ -f "$pid_file" ]]; then
  old_pid="$(cat "$pid_file")"
  if [[ "$old_pid" =~ ^[0-9]+$ ]] && kill -0 "$old_pid" 2>/dev/null \
    && ps -p "$old_pid" -o args= | grep -Fq "$binary serve"; then
    if curl -fsS "$health" 2>/dev/null | grep -q '"status":"ok"'; then
      echo "Ardvi MCP already running: http://127.0.0.1:$port/mcp"
      exit 0
    fi
    echo "Ardvi MCP process $old_pid is alive but unhealthy. Log: $log_file" >&2
    exit 1
  fi
  rm -f "$pid_file"
fi
if curl -fsS "$health" >/dev/null 2>&1; then
  echo "Port $port is already used by a process not managed by this harness." >&2
  exit 1
fi

nohup "$binary" serve --listen "127.0.0.1:$port" --data "$HUB_DATA_DIR" \
  --catalog "$HUB_CATALOG" >"$log_file" 2>&1 </dev/null &
pid=$!
printf '%s\n' "$pid" > "$pid_file"

for _ in {1..50}; do
  if curl -fsS "$health" 2>/dev/null | grep -q '"status":"ok"'; then
    echo "Ardvi MCP: http://127.0.0.1:$port/mcp"
    echo "PID: $pid"
    echo "Log: $log_file"
    exit 0
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

kill "$pid" 2>/dev/null || true
rm -f "$pid_file"
echo "Ardvi MCP did not become ready. Log: $log_file" >&2
tail -n 20 "$log_file" >&2 || true
exit 1
