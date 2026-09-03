#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

port="${CAO_PORT:-9889}"
if tmux has-session -t project-cao-server 2>/dev/null; then
  echo "CAO server tmux: running"
else
  echo "CAO server tmux: stopped"
fi

if curl -fsS "http://127.0.0.1:$port/" >/dev/null 2>&1; then
  echo "CAO local UI: http://127.0.0.1:$port"
else
  echo "CAO local UI: unreachable"
fi

if command -v cao >/dev/null 2>&1; then
  cao session list 2>/dev/null || true
fi
