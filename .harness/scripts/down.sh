#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

if command -v cao >/dev/null 2>&1; then
  cao shutdown --all >/dev/null 2>&1 || true
fi

if tmux has-session -t project-cao-server 2>/dev/null; then
  tmux kill-session -t project-cao-server
fi

echo "CAO stopped"
