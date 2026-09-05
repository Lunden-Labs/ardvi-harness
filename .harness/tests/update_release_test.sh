#!/usr/bin/env bash
set -Eeuo pipefail
test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHONDONTWRITEBYTECODE=1 python3 "$test_dir/update_release_test.py"
