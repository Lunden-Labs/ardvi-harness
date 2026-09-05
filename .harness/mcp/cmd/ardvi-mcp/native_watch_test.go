package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeRPCServer(t *testing.T, respond func(string, map[string]any) any) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request struct {
			ID     any `json:"id"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode rpc: %v", err)
			return
		}
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"structuredContent": respond(request.Params.Name, request.Params.Arguments), "isError": false}})
	}))
	t.Cleanup(server.Close)
	return server.URL, &calls
}

func TestLeaseLauncherRequiresVerifiedClientAndOmitsPrompt(t *testing.T) {
	originalInspect, originalLaunch := inspectNativeProcess, launchLeaseKeeper
	t.Cleanup(func() { inspectNativeProcess, launchLeaseKeeper = originalInspect, originalLaunch })
	inspectNativeProcess = func(pid int) (nativeProcess, string, error) { return nativeProcess{pid, "born"}, "codex", nil }
	path := filepath.Join(t.TempDir(), "mapping.json")
	mapping := hookMapping{ArdviSessionID: "session", Stable: true, ClientPID: 55, ClientBirth: "born"}
	if err := saveMapping(path, mapping); err != nil {
		t.Fatal(err)
	}
	calls := 0
	launchLeaseKeeper = func(command *exec.Cmd) error {
		calls++
		payload, err := io.ReadAll(command.Stdin)
		if err != nil {
			return err
		}
		if strings.Contains(string(payload), "private human text") {
			t.Fatal("forwarded human prompt to lease keeper")
		}
		if !strings.Contains(string(payload), "native-session") {
			t.Fatal("missing lifecycle identity")
		}
		return nil
	}
	in := hookStdin{SessionID: "native-session", Cwd: "repo", Prompt: "private human text"}
	if err := startNativeLeaseKeeper("codex", "http://localhost", path, in); err != nil {
		t.Fatal(err)
	}
	mapping.ClientBirth = "old process"
	if err := saveMapping(path, mapping); err != nil {
		t.Fatal(err)
	}
	if err := startNativeLeaseKeeper("codex", "http://localhost", path, in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("launch count %d; stale PID must not launch", calls)
	}
}

func TestMachineIdentityIsStableHostState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	first, err := machineIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := machineIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 32 {
		t.Fatalf("machine identity = %q, %q", first, second)
	}
}

func TestNativeProcessAliveRequiresBirthFingerprint(t *testing.T) {
	original := inspectNativeProcess
	t.Cleanup(func() { inspectNativeProcess = original })
	inspectNativeProcess = func(pid int) (nativeProcess, string, error) {
		return nativeProcess{PID: pid, Birth: "born"}, "claude", nil
	}
	mapping := hookMapping{ClientPID: 42, ClientBirth: "born"}
	if !nativeProcessAlive("claude", mapping) {
		t.Fatal("matching process should be live")
	}
	mapping.ClientBirth = "reused"
	if nativeProcessAlive("claude", mapping) {
		t.Fatal("PID reuse must not renew a lease")
	}
}

func TestClaudeWatchWakesOnceForUnreadWithVerifiedClient(t *testing.T) {
	url := newHookTestServer(t)
	const projectID = "77777777-7777-4777-8777-777777777777"
	dir := writeTestProject(t, projectID)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	originalDiscover, originalInspect := discoverNativeProcess, inspectNativeProcess
	originalLaunch := launchLeaseKeeper
	launchLeaseKeeper = func(*exec.Cmd) error { return nil }
	t.Cleanup(func() { launchLeaseKeeper = originalLaunch })
	t.Cleanup(func() { discoverNativeProcess, inspectNativeProcess = originalDiscover, originalInspect })
	discoverNativeProcess = func(string) (nativeProcess, bool) { return nativeProcess{PID: 77, Birth: "born"}, true }
	inspectNativeProcess = func(pid int) (nativeProcess, string, error) {
		return nativeProcess{PID: pid, Birth: "born"}, "claude", nil
	}
	in := hookStdin{SessionID: "claude-native", Cwd: dir}
	if err := hookSessionStart(&bytes.Buffer{}, "claude", url, in); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	raw, err := callTool(ctx, url, projectID, "session_start", map[string]any{"name": "sender", "client": "codex"})
	if err != nil {
		t.Fatal(err)
	}
	var sender struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(raw, &sender); err != nil {
		t.Fatal(err)
	}
	if _, err = callTool(ctx, url, projectID, "message_send", map[string]any{"session_id": sender.ID, "to": "claude-proj", "body": "wake now"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = hookWatch(&out, "claude", url, in, func(time.Duration) { t.Fatal("watch should wake before polling again") })
	wake, ok := err.(*hookWake)
	if !ok {
		t.Fatalf("watch error = %v", err)
	}
	if out.Len() != 0 || !strings.Contains(wake.text, "wake now") {
		t.Fatalf("wake must be returned only for stderr delivery: out=%q wake=%q", out.String(), wake.text)
	}
}

func TestNativeHooksRegisterAndBootstrapWithoutMonitor(t *testing.T) {
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	url := newHookTestServer(t)
	const projectID = "88888888-8888-4888-8888-888888888888"
	dir := writeTestProject(t, projectID)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	start := func(client, native, key string) hookMapping {
		t.Setenv("ARDVI_AGENT_KEY", key)
		in := hookStdin{SessionID: native, Cwd: dir, HookEventName: "SessionStart"}
		var out bytes.Buffer
		if err := hookSessionStart(&out, client, url, in); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"stable agent=", "session=", "project=", "context_bootstrap(session_id=", "not human authorization"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s startup context missing %q: %s", client, want, text)
			}
		}
		if strings.Contains(strings.ToLower(text), "lets-go") {
			t.Fatalf("startup used monitor wording: %s", text)
		}
		state, err := ardviStateDir()
		if err != nil {
			t.Fatal(err)
		}
		mapping, ok := loadMapping(filepath.Join(state, mappingKey(client, native, projectID)+".json"))
		if !ok || !mapping.Stable || mapping.AgentID == "" || mapping.MachineID == "" || mapping.NativeSessionID != native {
			t.Fatalf("invalid native provenance: %+v", mapping)
		}
		raw, err := callTool(context.Background(), url, projectID, "context_bootstrap", map[string]any{"session_id": mapping.ArdviSessionID})
		if err != nil {
			t.Fatal(err)
		}
		var bootstrap struct {
			Self struct {
				AgentID   string `json:"agent_id"`
				SessionID string `json:"session_id"`
				ProjectID string `json:"project_id"`
				MachineID string `json:"machine_id"`
			} `json:"self"`
		}
		if err = json.Unmarshal(raw, &bootstrap); err != nil {
			t.Fatal(err)
		}
		if bootstrap.Self.AgentID != mapping.AgentID || bootstrap.Self.SessionID != mapping.ArdviSessionID || bootstrap.Self.ProjectID != projectID || bootstrap.Self.MachineID != mapping.MachineID {
			t.Fatalf("bootstrap provenance mismatch: %+v mapping=%+v", bootstrap.Self, mapping)
		}
		return mapping
	}

	for _, client := range []string{"codex", "claude"} {
		first := start(client, client+"-one", "")
		if err := hookSessionEnd(client, url, hookStdin{SessionID: client + "-one", Cwd: dir}); err != nil {
			t.Fatal(err)
		}
		second := start(client, client+"-two", "")
		if second.AgentID != first.AgentID || second.ArdviSessionID == first.ArdviSessionID {
			t.Fatalf("%s restart did not recover stable agent with new session: first=%+v second=%+v", client, first, second)
		}
		fork := start(client, client+"-fork", "fork-test")
		if fork.AgentID == second.AgentID || fork.AgentKey != "fork-test" {
			t.Fatalf("%s explicit fork key did not create independent agent: main=%+v fork=%+v", client, second, fork)
		}
	}
}

func TestUnstableMappingRetriesAndUpgradesWhenServiceUpdates(t *testing.T) {
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const projectID = "99999999-9999-4999-8999-999999999999"
	dir := writeTestProject(t, projectID)
	var registrations atomic.Int32
	url, _ := nativeRPCServer(t, func(name string, args map[string]any) any {
		if name == "session_start" {
			if registrations.Add(1) == 1 {
				return map[string]any{"id": "legacy"}
			}
			return map[string]any{"id": "stable", "agent_id": "agent", "machine_id": args["machine_id"], "native_session_id": args["native_session_id"], "native_thread_id": args["native_thread_id"]}
		}
		return map[string]any{"messages": []any{}}
	})
	in := hookStdin{SessionID: "native", Cwd: dir}
	var first bytes.Buffer
	if err := hookSessionStart(&first, "codex", url, in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "lifecycle degraded") {
		t.Fatalf("legacy startup = %s", first.String())
	}
	var second bytes.Buffer
	if err := hookSessionStart(&second, "codex", url, in); err != nil {
		t.Fatal(err)
	}
	if registrations.Load() != 2 || !strings.Contains(second.String(), "stable agent=agent") {
		t.Fatalf("service upgrade was not reconciled: calls=%d output=%s", registrations.Load(), second.String())
	}
	state, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	mapping, ok := loadMapping(filepath.Join(state, mappingKey("codex", "native", projectID)+".json"))
	if !ok || !mapping.Stable || mapping.ArdviSessionID != "stable" {
		t.Fatalf("upgraded mapping = %+v", mapping)
	}
}

func TestLeaseKeeperRenewsOnlyWithVerifiedNativeProcess(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const projectID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	dir := writeTestProject(t, projectID)
	url, calls := nativeRPCServer(t, func(name string, args map[string]any) any { return map[string]any{} })
	state, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, mappingKey("codex", "native", projectID)+".json")
	if err = saveMapping(path, hookMapping{ArdviSessionID: "session", ProjectID: projectID, Client: "codex", NativeSessionID: "native", Stable: true, ClientPID: 7, ClientBirth: "born"}); err != nil {
		t.Fatal(err)
	}
	original := inspectNativeProcess
	t.Cleanup(func() { inspectNativeProcess = original })
	inspectNativeProcess = func(pid int) (nativeProcess, string, error) {
		return nativeProcess{PID: pid, Birth: "born"}, "codex", nil
	}
	if err = hookLease("codex", url, hookStdin{SessionID: "native", Cwd: dir}, func(time.Duration) { _ = os.Remove(path) }); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one verified heartbeat, got %d", calls.Load())
	}
	if err = saveMapping(path, hookMapping{ArdviSessionID: "session", ProjectID: projectID, Client: "codex", NativeSessionID: "native", Stable: true, ClientPID: 7, ClientBirth: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err = hookLease("codex", url, hookStdin{SessionID: "native", Cwd: dir}, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("stale birth fingerprint renewed lease: %d calls", calls.Load())
	}
}

func TestPromptRecoveryAnnouncesReconciledSession(t *testing.T) {
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	const projectID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	url := newHookTestServer(t)
	dir := writeTestProject(t, projectID)
	in := hookStdin{SessionID: "native", Cwd: dir}
	if err := hookSessionStart(&bytes.Buffer{}, "codex", url, in); err != nil {
		t.Fatal(err)
	}
	state, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, mappingKey("codex", "native", projectID)+".json")
	before, ok := loadMapping(path)
	if !ok {
		t.Fatal("missing initial mapping")
	}
	if _, err = callTool(context.Background(), url, projectID, "session_end", map[string]any{"session_id": before.ArdviSessionID}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = hookPrompt(&out, "codex", url, in); err != nil {
		t.Fatal(err)
	}
	after, ok := loadMapping(path)
	if !ok {
		t.Fatal("missing recovered mapping")
	}
	if after.ArdviSessionID == before.ArdviSessionID || !strings.Contains(out.String(), "context_bootstrap(session_id=") || !strings.Contains(out.String(), after.ArdviSessionID) {
		t.Fatalf("prompt did not announce recovered identity: before=%+v after=%+v output=%s", before, after, out.String())
	}
}
