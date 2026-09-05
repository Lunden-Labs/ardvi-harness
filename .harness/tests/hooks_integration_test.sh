#!/usr/bin/env bash
set -Eeuo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace="$(mktemp -d)"
background_pids=()
cleanup() {
  for pid in "${background_pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$workspace"
}
trap cleanup EXIT

# Fresh project: bootstrap.sh (via project_config.py) installs both hook files
# with native lifecycle hooks. Claude also receives its asyncRewake watcher.
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
grep -Fq '"matcher": "startup|resume|clear|compact|fork"' "$fresh/.claude/settings.json"
grep -Fq '"command": "ardvi hook watch --client claude"' "$fresh/.claude/settings.json"
grep -Fq '"asyncRewake": true' "$fresh/.claude/settings.json"
grep -Fq '"timeout": 86400' "$fresh/.claude/settings.json"
grep -Fq '"command": "ardvi hook session-start --client codex"' "$fresh/.codex/hooks.json"
grep -Fq '"command": "ardvi hook prompt --client codex"' "$fresh/.codex/hooks.json"
grep -Fq '"command": "ardvi hook session-end --client codex"' "$fresh/.codex/hooks.json"
! grep -Fq 'ardvi hook watch --client codex' "$fresh/.codex/hooks.json"

# Re-running is idempotent (byte-identical), including through bootstrap.sh again.
before_claude="$(sha256sum "$fresh/.claude/settings.json")"
before_codex="$(sha256sum "$fresh/.codex/hooks.json")"
HARNESS_REPO_ROOT="$fresh" bash "$fresh/.harness/scripts/bootstrap.sh" >/dev/null
[[ "$before_claude" == "$(sha256sum "$fresh/.claude/settings.json")" ]]
[[ "$before_codex" == "$(sha256sum "$fresh/.codex/hooks.json")" ]]

# Explicit project policy survives regeneration and affects only Codex startup.
python3 - "$fresh/.ardvi/project.json" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
value = json.loads(path.read_text())
value["codex_single_orchestrator"] = True
path.write_text(json.dumps(value) + "\n")
PY
HARNESS_REPO_ROOT="$fresh" python3 "$fresh/.harness/scripts/project_config.py" >/dev/null
grep -Fq 'ardvi hook session-start --client codex --single-orchestrator' "$fresh/.codex/hooks.json"
! grep -Fq -- '--single-orchestrator' "$fresh/.claude/settings.json"
before_codex="$(sha256sum "$fresh/.codex/hooks.json")"
before_identity="$(sha256sum "$fresh/.ardvi/project.json")"
HARNESS_REPO_ROOT="$fresh" bash "$fresh/.harness/scripts/bootstrap.sh" >/dev/null
[[ "$before_codex" == "$(sha256sum "$fresh/.codex/hooks.json")" ]]
[[ "$before_identity" == "$(sha256sum "$fresh/.ardvi/project.json")" ]]
python3 - "$fresh" <<'PY'
import json, pathlib, re, sys
root = pathlib.Path(sys.argv[1])
events = json.loads((root / '.codex/hooks.json').read_text())['hooks']
for source in ('startup', 'resume', 'clear', 'compact'):
    commands = [h['command'] for block in events['SessionStart']
                if re.search(block['matcher'], source) for h in block['hooks']]
    assert len(commands) == 1, (source, commands)
    assert ('--single-orchestrator' in commands[0]) == (source in ('startup', 'resume'))
for event, blocks in events.items():
    if event != 'SessionStart':
        assert all('--single-orchestrator' not in h['command'] for b in blocks for h in b['hooks'])
PY

# Reject invalid opt-ins before writing client files; false removes the flag.
python3 - "$fresh" <<'PY'
import json, os, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1])
path = root / '.ardvi/project.json'
value = json.loads(path.read_text())
files = [root / p for p in ('.codex/hooks.json', '.codex/config.toml', '.mcp.json', '.claude/settings.json')]
before = {p: p.read_bytes() for p in files}
for invalid in ('true', 1, None, {}):
    value['codex_single_orchestrator'] = invalid
    path.write_text(json.dumps(value))
    result = subprocess.run([sys.executable, str(root / '.harness/scripts/project_config.py')],
                            env={**os.environ, 'HARNESS_REPO_ROOT': str(root)}, capture_output=True)
    assert result.returncode != 0
    assert b'codex_single_orchestrator must be a boolean' in result.stderr
    assert before == {p: p.read_bytes() for p in files}
value['codex_single_orchestrator'] = False
path.write_text(json.dumps(value))
subprocess.run([sys.executable, str(root / '.harness/scripts/project_config.py')],
               env={**os.environ, 'HARNESS_REPO_ROOT': str(root)}, check=True, stdout=subprocess.DEVNULL)
assert '--single-orchestrator' not in (root / '.codex/hooks.json').read_text()
PY

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
grep -Fq '"command": "ardvi hook watch --client claude"' "$foreign/.claude/settings.json"
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

# A Codex SessionStart launches one detached bridge against the daemon socket;
# SessionEnd stops it. The disable flag suppresses autostart. Both services are
# local fakes so this test cannot connect to a real Codex or Ardvi session.
(cd "$repo_root/.harness/mcp" && go build -o "$workspace/ardvi" ./cmd/ardvi-mcp)
socket="$workspace/codex.sock"
python3 - "$socket" <<'PY' &
import socket, sys
s = socket.socket(socket.AF_UNIX)
s.bind(sys.argv[1])
s.listen()
while True:
    conn, _ = s.accept()
    conn.close()
PY
background_pids+=("$!")
while [[ ! -S "$socket" ]]; do sleep 0.02; done

mkdir -p "$workspace/bin"
cat > "$workspace/bin/codex" <<'SH'
#!/usr/bin/env bash
printf '{"status":"running","socketPath":"%s"}\n' "$FAKE_CODEX_SOCKET"
SH
chmod +x "$workspace/bin/codex"

port_file="$workspace/mcp-port"
python3 - "$port_file" <<'PY' &
import json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        name = request['params']['name']
        params = request['params'].get('arguments', {})
        values = {
            'session_start': {'id': 'ardvi-test-session', 'agent_id': 'ardvi-test-agent', 'machine_id': params.get('machine_id'), 'native_session_id': params.get('native_session_id'), 'native_thread_id': params.get('native_thread_id')},
            'session_heartbeat': {},
            'inbox_read': {'messages': []},
            'session_end': {},
        }
        body = json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': {
            'structuredContent': values[name], 'isError': False,
        }}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *_): pass
server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
open(sys.argv[1], 'w').write(str(server.server_port))
server.serve_forever()
PY
background_pids+=("$!")
while [[ ! -s "$port_file" ]]; do sleep 0.02; done

runtime="$workspace/runtime"; mkdir -p "$runtime/.ardvi"
printf '{"id":"11111111-1111-4111-8111-111111111111","name":"runtime"}\n' > "$runtime/.ardvi/project.json"
state="$workspace/state"
hook_env=(env "PATH=$workspace/bin:$PATH" "FAKE_CODEX_SOCKET=$socket" "XDG_STATE_HOME=$state" "ARDVI_SESSION_NAME=bridge-test")
hook_input="{\"session_id\":\"native-thread\",\"cwd\":\"$runtime\"}"
printf '%s\n' "$hook_input" | "${hook_env[@]}" "$workspace/ardvi" hook session-start --client codex --url "http://127.0.0.1:$(cat "$port_file")" >/dev/null
pid_file=""
for _ in {1..100}; do
  pid_file="$(find "$state/ardvi/sessions" -name 'bridge-*.pid' -size +0c -print -quit 2>/dev/null || true)"
  [[ -n "$pid_file" ]] && break
  sleep 0.02
done
[[ -n "$pid_file" ]]
bridge_pid="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["pid"])' "$pid_file")"
background_pids+=("$bridge_pid")
kill -0 "$bridge_pid"
printf '%s\n' "$hook_input" | "${hook_env[@]}" "$workspace/ardvi" hook session-end --client codex --url "http://127.0.0.1:$(cat "$port_file")" >/dev/null
for _ in {1..100}; do
  [[ ! -s "$pid_file" ]] && break
  sleep 0.02
done
if [[ -s "$pid_file" ]]; then
  ps -o pid,ppid,stat,args -p "$bridge_pid" >&2 || true
  cat "$state"/ardvi/sessions/bridge-*.log >&2 || true
  echo 'SessionEnd did not stop Codex bridge' >&2
  exit 1
fi

disabled_input="{\"session_id\":\"native-disabled\",\"cwd\":\"$runtime\"}"
printf '%s\n' "$disabled_input" | ARDVI_CODEX_BRIDGE_DISABLE=1 "${hook_env[@]}" "$workspace/ardvi" hook session-start --client codex --url "http://127.0.0.1:$(cat "$port_file")" >/dev/null
[[ -z "$(find "$state/ardvi/sessions" -name 'bridge-*.pid' -size +0c -print -quit 2>/dev/null || true)" ]]

echo "hooks integration: PASS"
