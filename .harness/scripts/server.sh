#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

command -v ardvi >/dev/null 2>&1 || { echo "Ardvi is not installed; run the release installer." >&2; exit 1; }
ardvi service ensure
ardvi healthcheck
echo "Ardvi MCP: http://127.0.0.1:8765/mcp"
