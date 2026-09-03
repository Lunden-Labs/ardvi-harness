#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

for cmd in python3 tmux curl git; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing dependency: $cmd" >&2
    echo "Ubuntu/Debian: sudo apt-get install -y python3 tmux curl git" >&2
    exit 1
  fi
done

python3 - <<'PY'
import sys
if sys.version_info < (3, 10):
    raise SystemExit("Python 3.10 or newer is required")
PY

if ! command -v uv >/dev/null 2>&1; then
  echo "Installing uv..."
  curl -LsSf https://astral.sh/uv/install.sh | sh
  export PATH="$HOME/.local/bin:$PATH"
fi

if [[ "${CAO_INSTALL_SOURCE:-stable}" == "main" ]]; then
  uv tool install 'git+https://github.com/awslabs/cli-agent-orchestrator.git@main' --upgrade
else
  uv tool install cli-agent-orchestrator --upgrade
fi

cao init >/dev/null
bash "$HARNESS_DIR/scripts/install_upstreams.sh"
echo "CAO installed: $(cao --version 2>/dev/null || echo installed)"
