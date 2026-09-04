#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

for cmd in python3 docker ardvi; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing dependency: $cmd" >&2
    echo "Install Docker with Compose and the Ardvi release, then retry." >&2
    exit 1
  fi
done

python3 - <<'PY'
import sys
if sys.version_info < (3, 10):
    raise SystemExit("Python 3.10 or newer is required")
PY

docker compose version >/dev/null
ardvi service ensure
echo "Shared Ardvi MCP service is ready"
