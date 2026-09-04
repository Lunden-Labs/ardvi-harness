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
make -C "$fixture" -n harness-skill-path SKILL=writing >/dev/null
! make -C "$fixture" -n init >/dev/null 2>&1
make -C "$repo_root" -n init >/dev/null

mkdir -p "$fixture/.cao/agents" "$fixture/bin" "$fixture/home"
printf 'PROJECT_SLUG=fixture\n' > "$fixture/.cao/project.env"
printf '%s\n' '---' 'name: fixture-architect' '---' > \
  "$fixture/.cao/agents/fixture-architect.md"
cat > "$fixture/bin/cao" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "${1:-}" in
  init) ;;
  config)
    case "${2:-}" in
      get) printf '[]\n' ;;
      set) ;;
      *) exit 2 ;;
    esac
    ;;
  launch)
    printf '%s\n' "$@" > "$CAO_LAUNCH_ARGS"
    ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$fixture/bin/cao"
prompt='Inspect this repository; prepare suitable agents and a plan.'
HOME="$fixture/home" PATH="$fixture/bin:$PATH" CAO_LAUNCH_ARGS="$fixture/launch.args" \
  make --no-print-directory -C "$fixture" harness-architect PROVIDER=codex \
    PROMPT="$prompt" >/dev/null
[[ "$(tail -n 1 "$fixture/launch.args")" == "$prompt" ]]
[[ "$(grep -Fxc "$prompt" "$fixture/launch.args")" == 1 ]]
grep -Fqx -- '--provider' "$fixture/launch.args"
grep -Fqx 'codex' "$fixture/launch.args"
! grep -Fqx 'claude_code' "$fixture/launch.args"
HOME="$fixture/home" PATH="$fixture/bin:$PATH" \
  CAO_LAUNCH_ARGS="$fixture/launch-without-prompt.args" \
  make --no-print-directory -C "$fixture" harness-architect >/dev/null
[[ "$(tail -n 1 "$fixture/launch-without-prompt.args")" == fixture-architect ]]
[[ "$(wc -l < "$fixture/launch-without-prompt.args")" == 3 ]]

echo "make integration: PASS"
