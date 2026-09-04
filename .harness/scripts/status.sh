#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

command -v ardvi >/dev/null 2>&1 || { echo "Ardvi: not installed"; exit 1; }
ardvi version
ardvi service status
if ardvi healthcheck >/dev/null 2>&1; then
  echo "Ardvi MCP: healthy at http://127.0.0.1:8765/mcp"
else
  echo "Ardvi MCP: not healthy"
fi
