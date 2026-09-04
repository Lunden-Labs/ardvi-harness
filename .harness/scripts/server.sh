#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if ! command -v cao-server >/dev/null 2>&1; then
  echo "cao-server is missing. Run: make -f .harness/Makefile install" >&2
  exit 1
fi
if ! command -v tmux >/dev/null 2>&1; then
  echo "tmux is required" >&2
  exit 1
fi

port="${CAO_PORT:-9889}"
session="project-cao-server"

configure_tmux_mouse() {
  tmux set-hook -g 'client-attached[90]' \
    'if-shell -F "#{m/r:^cao-,#{hook_session_name}}" "set-option -t \"#{hook_session_name}\" mouse off"'
  tmux set-hook -g 'session-created[90]' \
    'if-shell -F "#{m/r:^cao-,#{hook_session_name}}" "run-shell -b '\''sleep 1; tmux set-option -t \"#{hook_session_name}\" mouse off'\''"'
}

if tmux has-session -t "$session" 2>/dev/null; then
  configure_tmux_mouse
  echo "CAO server tmux session already exists: $session"
  exit 0
fi

tmux new-session -d -s "$session" \
  "cao-server --host 127.0.0.1 --port '$port'"
configure_tmux_mouse

for _ in {1..30}; do
  if curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1; then
    echo "CAO server: http://127.0.0.1:$port"
    echo "tmux session: $session"
    exit 0
  fi
  sleep 1
done

echo "CAO server did not become ready. Inspect: tmux attach -t $session" >&2
exit 1
