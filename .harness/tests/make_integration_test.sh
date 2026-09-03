#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

cp -a "$repo_root/.harness" "$fixture/.harness"
cat > "$fixture/Makefile" <<'EOF'
.PHONY: help
help:
	@echo "product help"

include .harness/harness.mk
EOF

help_output="$(make -C "$fixture" help 2>&1)"
[[ "$help_output" == *"product help"* ]]
[[ "$help_output" != *"overriding recipe"* ]]
make -C "$fixture" -n harness-init >/dev/null
! make -C "$fixture" -n init >/dev/null 2>&1
make -C "$repo_root" -n init >/dev/null

echo "make integration: PASS"
