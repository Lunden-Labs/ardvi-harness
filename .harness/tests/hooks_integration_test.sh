#!/usr/bin/env bash
set -Eeuo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"; trap 'rm -rf "$workspace"' EXIT

# Fresh project: bootstrap.sh (via project_config.py) installs both hook files
# with the expected three ardvi hook commands each.
fresh="$workspace/fresh"; mkdir -p "$fresh"; git -C "$fresh" init -q; cp -a "$repo_root/.harness" "$fresh/.harness"
rm -f "$fresh/.harness/.managed-state.json"  # fixture must not inherit a local dev checkout's state
HARNESS_REPO_ROOT="$fresh" bash "$fresh/.harness/scripts/bootstrap.sh" >/dev/null

for path in "$fresh/.claude/settings.json" "$fresh/.codex/hooks.json"; do
  [[ -f "$path" ]]
  python3 - "$path" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
hooks = value["hooks"]
for event in ("SessionStart", "UserPromptSubmit", "SessionEnd"):
    assert event in hooks, f"missing event {event} in {sys.argv[1]}"
PY
done
grep -Fq '"command": "ardvi hook session-start --client claude"' "$fresh/.claude/settings.json"
grep -Fq '"command": "ardvi hook prompt --client claude"' "$fresh/.claude/settings.json"
grep -Fq '"command": "ardvi hook session-end --client claude"' "$fresh/.claude/settings.json"
grep -Fq '"matcher": "startup|resume|clear|compact"' "$fresh/.claude/settings.json"
grep -Fq '"command": "ardvi hook session-start --client codex"' "$fresh/.codex/hooks.json"
grep -Fq '"command": "ardvi hook prompt --client codex"' "$fresh/.codex/hooks.json"
grep -Fq '"command": "ardvi hook session-end --client codex"' "$fresh/.codex/hooks.json"

# Re-running is idempotent (byte-identical), including through bootstrap.sh again.
before_claude="$(sha256sum "$fresh/.claude/settings.json")"
before_codex="$(sha256sum "$fresh/.codex/hooks.json")"
HARNESS_REPO_ROOT="$fresh" bash "$fresh/.harness/scripts/bootstrap.sh" >/dev/null
[[ "$before_claude" == "$(sha256sum "$fresh/.claude/settings.json")" ]]
[[ "$before_codex" == "$(sha256sum "$fresh/.codex/hooks.json")" ]]

# A pre-existing .claude/settings.json with a foreign hook entry, an unrelated
# event, and other top-level keys keeps all of it; a drifted ardvi entry is
# repaired in place without disturbing the rest.
foreign="$workspace/foreign"; mkdir -p "$foreign/.claude" "$foreign/.codex"; git -C "$foreign" init -q
cp -a "$repo_root/.harness" "$foreign/.harness"
rm -f "$foreign/.harness/.managed-state.json"
cat > "$foreign/.claude/settings.json" <<'EOF'
{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "echo foreign-hook"}]},
      {"matcher": "startup|resume|clear|compact", "hooks": [{"type": "command", "command": "ardvi hook session-start --client claude", "timeout": 999}]}
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "echo unrelated-event"}]}]
  }
}
EOF
echo '{}' > "$foreign/.codex/hooks.json"
HARNESS_REPO_ROOT="$foreign" python3 "$foreign/.harness/scripts/project_config.py" >/dev/null
grep -Fq '"command": "echo foreign-hook"' "$foreign/.claude/settings.json"
grep -Fq '"command": "echo unrelated-event"' "$foreign/.claude/settings.json"
grep -Fq '"Bash(ls:*)"' "$foreign/.claude/settings.json"
grep -Fq '"command": "ardvi hook session-start --client claude"' "$foreign/.claude/settings.json"
! grep -Fq '999' "$foreign/.claude/settings.json"
grep -Fq '"timeout": 10' "$foreign/.claude/settings.json"

# Invalid JSON fails closed with a clear error and leaves every file untouched.
broken="$workspace/broken"; mkdir -p "$broken/.codex"; git -C "$broken" init -q
cp -a "$repo_root/.harness" "$broken/.harness"
rm -f "$broken/.harness/.managed-state.json"
printf 'not valid json{{{' > "$broken/.codex/hooks.json"
before="$(sha256sum "$broken/.codex/hooks.json")"
if HARNESS_REPO_ROOT="$broken" python3 "$broken/.harness/scripts/project_config.py" >/dev/null 2>"$workspace/broken.log"; then
  echo 'invalid Codex hooks.json was accepted' >&2
  exit 1
fi
grep -Fq 'ERROR' "$workspace/broken.log"
[[ "$before" == "$(sha256sum "$broken/.codex/hooks.json")" ]]
[[ ! -e "$broken/.claude/settings.json" ]]

echo "hooks integration: PASS"
