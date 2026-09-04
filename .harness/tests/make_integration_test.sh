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
    printf '%s\0' "$@" > "$CAO_LAUNCH_ARGS"
    printf '%s\n' "${CAO_MCP_REQUEST_TIMEOUT:-}" > "$CAO_TIMEOUT_VALUE"
    ;;
  *) exit 2 ;;
esac
EOF
cat > "$fixture/bin/tmux" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${1:-}" == set-hook ]]; then
  printf '%s\0' "$@" >> "$TMUX_HOOK_ARGS"
  exit
fi
if [[ "${1:-}" == has-session ]]; then
  exit
fi
printf '%s\0' "$@" > "$TMUX_ARGS"
EOF
printf '#!/usr/bin/env bash\n' > "$fixture/bin/cao-server"
chmod +x "$fixture/bin/cao"
chmod +x "$fixture/bin/tmux"
chmod +x "$fixture/bin/cao-server"
HOME="$fixture/home" PATH="$fixture/bin:$PATH" \
  TMUX_HOOK_ARGS="$fixture/tmux-hook.args" \
  bash "$fixture/.harness/scripts/server.sh" >/dev/null
tmux_hook_args=()
while IFS= read -r -d '' arg; do tmux_hook_args+=("$arg"); done < \
  "$fixture/tmux-hook.args"
[[ "${tmux_hook_args[*]}" == *'session-created[90]'* ]]
[[ "${tmux_hook_args[*]}" == *'client-attached[90]'* ]]
[[ "${tmux_hook_args[*]}" == *'#{m/r:^cao-,#{hook_session_name}}'* ]]
[[ "${tmux_hook_args[*]}" == *'mouse off'* ]]
prompt=$'- Inspect this repository.\nPrepare suitable agents and a plan.'
HOME="$fixture/home" PATH="$fixture/bin:$PATH" CAO_LAUNCH_ARGS="$fixture/launch.args" \
  CAO_TIMEOUT_VALUE="$fixture/timeout.value" TMUX_ARGS="$fixture/tmux.args" \
  TMUX_HOOK_ARGS="$fixture/tmux-hook-unused.args" \
  make --no-print-directory -C "$fixture" harness-architect PROVIDER=codex \
    PROMPT="$prompt" >/dev/null
launch_args=()
while IFS= read -r -d '' arg; do launch_args+=("$arg"); done < "$fixture/launch.args"
[[ "${launch_args[$((${#launch_args[@]} - 1))]}" == "$prompt" ]]
[[ "${launch_args[*]}" == *'--provider codex'* ]]
[[ "${launch_args[*]}" == *'--headless --async'* ]]
[[ "${launch_args[*]}" != *'claude_code'* ]]
for ((i = 0; i < ${#launch_args[@]}; i++)); do
  if [[ "${launch_args[$i]}" == --session-name ]]; then
    launch_session="${launch_args[$((i + 1))]}"
  fi
done
[[ -n "${launch_session:-}" ]]
[[ "$(cat "$fixture/timeout.value")" == 120 ]]
tmux_args=()
while IFS= read -r -d '' arg; do tmux_args+=("$arg"); done < "$fixture/tmux.args"
[[ "${tmux_args[*]}" == "attach-session -t cao-$launch_session" ]]
HOME="$fixture/home" PATH="$fixture/bin:$PATH" \
  CAO_LAUNCH_ARGS="$fixture/launch-without-prompt.args" \
  CAO_TIMEOUT_VALUE="$fixture/timeout-without-prompt.value" \
  TMUX_HOOK_ARGS="$fixture/tmux-hook-unused.args" \
  make --no-print-directory -C "$fixture" harness-architect >/dev/null
launch_without_prompt=()
while IFS= read -r -d '' arg; do launch_without_prompt+=("$arg"); done < \
  "$fixture/launch-without-prompt.args"
[[ "${launch_without_prompt[*]}" == 'launch --agents fixture-architect' ]]

echo "make integration: PASS"
