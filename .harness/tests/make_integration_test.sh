#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

cp -a "$repo_root/.harness" "$fixture/.harness"
git -C "$fixture" init -q
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
make -C "$fixture" -n harness-skill-path SKILL=writing >/dev/null
make -C "$fixture" -n harness-up >/dev/null
make -C "$fixture" -n harness-down >/dev/null
make -C "$fixture" -n harness-memory-export OUTPUT=.ardvi/memory.jsonl >/dev/null
! make -C "$fixture" -n init >/dev/null 2>&1
make -C "$repo_root" -n init >/dev/null

echo "make integration: PASS"
