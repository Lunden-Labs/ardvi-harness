#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

command -v ardvi >/dev/null 2>&1 || { echo "Ardvi is not installed." >&2; exit 1; }
echo "Stopping the machine-wide service; other Ardvi projects will disconnect."
ardvi service stop
