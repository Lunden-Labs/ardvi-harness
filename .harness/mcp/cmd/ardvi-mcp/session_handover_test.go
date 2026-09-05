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
	"sync"
	"testing"
	"time"
)

func handoverFixture(t *testing.T) (string, string, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	t.Setenv("ARDVI_AGENT_KEY", "main")
	discover, inspect, launch := discoverNativeProcess, inspectNativeProcess, launchLeaseKeeper
	t.Cleanup(func() { discoverNativeProcess, inspectNativeProcess, launchLeaseKeeper = discover, inspect, launch })
	// Both conversations are served by the same long-lived app-server.
	discoverNativeProcess = func(string) (nativeProcess, bool) { return nativeProcess{PID: 77, Birth: "shared"}, true }
	inspectNativeProcess = func(pid int) (nativeProcess, string, error) {
		return nativeProcess{PID: pid, Birth: "shared"}, "codex", nil
	}
	launchLeaseKeeper = func(*exec.Cmd) error { return nil }
	const project = "11111111-1111-4111-8111-111111111111"
	return newHookTestServer(t), writeTestProject(t, project), project
}

func handoverMapping(t *testing.T, project, native string) hookMapping {
	t.Helper()
	dir, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := loadMapping(filepath.Join(dir, mappingKey("codex", native, project)+".json"))
	if !ok {
		t.Fatal("mapping missing for " + native)
	}
	return m
}

func TestSingleOrchestratorHandoverPreservesInboxAndFencesOldHooks(t *testing.T) {
	url, dir, project := handoverFixture(t)
	old := hookStdin{SessionID: "old", Cwd: dir}
	if err := hookSessionStart(io.Discard, "codex", url, old); err != nil {
		t.Fatal(err)
	}
	before := handoverMapping(t, project, old.SessionID)
	ctx := context.Background()
	rpc := func(name string, args map[string]any) []byte {
		t.Helper()
		raw, err := callTool(ctx, url, project, name, args)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var sender struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rpc("session_start", map[string]any{"client": "claude", "name": "sender", "machine_id": "sender-machine", "native_session_id": "sender-native"}), &sender); err != nil {
		t.Fatal(err)
	}
	message := rpc("message_send", map[string]any{"session_id": sender.ID, "to_agent_id": before.AgentID, "body": "survives handover", "ack_required": true, "kind": "request"})
	var request struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(message, &request); err != nil {
		t.Fatal(err)
	}
	rpc("request_accept", map[string]any{"session_id": before.ArdviSessionID, "message_id": request.ID})
	rpc("claim_acquire", map[string]any{"session_id": before.ArdviSessionID, "resource": "test-resource"})

	next := hookStdin{SessionID: "next", Cwd: dir, SingleOrchestrator: true}
	var output bytes.Buffer
	if err := hookSessionStart(&output, "codex", url, next); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "survives handover") || strings.Contains(output.String(), "degraded") {
		t.Fatalf("handover output: %s", &output)
	}
	after := handoverMapping(t, project, next.SessionID)
	if before.AgentID != after.AgentID || before.ArdviSessionID == after.ArdviSessionID || after.NativeThreadID != "next" {
		t.Fatalf("identity changed incorrectly: before=%+v after=%+v", before, after)
	}
	retired := handoverMapping(t, project, old.SessionID)
	if retired.Stable || !retired.Superseded {
		t.Fatalf("old mapping not fenced: %+v", retired)
	}
	if _, err := callTool(ctx, url, project, "session_heartbeat", map[string]any{"session_id": before.ArdviSessionID}); err == nil {
		t.Fatal("old session renewed")
	}
	if err := hookLease("codex", url, old, func(time.Duration) { t.Fatal("old keeper must exit immediately") }); err != nil {
		t.Fatal(err)
	}
	bootstrap := string(rpc("context_bootstrap", map[string]any{"session_id": after.ArdviSessionID}))
	if !strings.Contains(bootstrap, `"status":"pending"`) || strings.Contains(bootstrap, "test-resource") {
		t.Fatalf("ownership not released: %s", bootstrap)
	}
	if !strings.Contains(string(rpc("inbox_read", map[string]any{"session_id": after.ArdviSessionID})), "survives handover") {
		t.Fatal("handover acknowledged message")
	}
	if err := hookSessionStart(io.Discard, "codex", url, next); err != nil {
		t.Fatal(err)
	}
	if again := handoverMapping(t, project, next.SessionID); again.ArdviSessionID != after.ArdviSessionID {
		t.Fatal("duplicate start replaced own session")
	}
	// A late end must neither end the successor nor erase the old prompt fence.
	if err := hookSessionEnd("codex", url, old); err != nil {
		t.Fatal(err)
	}
	rpc("session_heartbeat", map[string]any{"session_id": after.ArdviSessionID})
	if err := hookSessionEnd("codex", url, next); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := hookPrompt(&output, "codex", url, old); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "superseded") {
		t.Fatalf("old prompt revived after successor ended: %s", &output)
	}
	old.SingleOrchestrator = true
	if err := hookSessionStart(io.Discard, "codex", url, old); err != nil {
		t.Fatal(err)
	}
	if resumed := handoverMapping(t, project, old.SessionID); !resumed.Stable || resumed.AgentID != before.AgentID {
		t.Fatalf("explicit resume failed: %+v", resumed)
	}
}

func TestSingleOrchestratorDefaultConflictAndAgentKeyIsolation(t *testing.T) {
	url, dir, project := handoverFixture(t)
	if err := hookSessionStart(io.Discard, "codex", url, hookStdin{SessionID: "main", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := hookSessionStart(&out, "codex", url, hookStdin{SessionID: "conflict", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "degraded") {
		t.Fatalf("default conflict missing: %s", &out)
	}
	t.Setenv("ARDVI_AGENT_KEY", "worker")
	if err := hookSessionStart(io.Discard, "codex", url, hookStdin{SessionID: "worker", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	worker := handoverMapping(t, project, "worker")
	t.Setenv("ARDVI_AGENT_KEY", "main")
	if err := hookSessionStart(io.Discard, "codex", url, hookStdin{SessionID: "next", Cwd: dir, SingleOrchestrator: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(context.Background(), url, project, "session_heartbeat", map[string]any{"session_id": worker.ArdviSessionID}); err != nil {
		t.Fatal("other key ended:", err)
	}
}

func TestSingleOrchestratorConcurrentStartsSerialize(t *testing.T) {
	url, dir, project := handoverFixture(t)
	var wg sync.WaitGroup
	for _, native := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			err := hookSessionStart(&out, "codex", url, hookStdin{SessionID: native, Cwd: dir, SingleOrchestrator: true})
			if err != nil || strings.Contains(out.String(), "degraded") {
				t.Errorf("start: %v %s", err, &out)
			}
		}()
	}
	wg.Wait()
	one, two := handoverMapping(t, project, "one"), handoverMapping(t, project, "two")
	if one.Stable == two.Stable || one.AgentID != two.AgentID {
		t.Fatalf("expected one owner of same agent: %+v %+v", one, two)
	}
}

func TestSingleOrchestratorOptionCannotComeFromStdin(t *testing.T) {
	var in hookStdin
	if err := json.Unmarshal([]byte(`{"session_id":"x","SingleOrchestrator":true,"single_orchestrator":true}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.SingleOrchestrator {
		t.Fatal("stdin enabled takeover")
	}
	for _, args := range [][]string{{"prompt", "--client", "codex", "--single-orchestrator"}, {"session-start", "--client", "claude", "--single-orchestrator"}} {
		if err := runHook(args); err == nil {
			t.Fatal("invalid option accepted", args)
		}
	}
}

func TestSingleOrchestratorFailedEndKeepsMapping(t *testing.T) {
	url, dir, project := handoverFixture(t)
	if err := hookSessionStart(io.Discard, "codex", url, hookStdin{SessionID: "old", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	state, _ := ardviStateDir()
	old := handoverMapping(t, project, "old")
	if err := hookSessionEnd("codex", "http://127.0.0.1:0", hookStdin{SessionID: "old", Cwd: dir}); err == nil {
		t.Fatal("failed ordinary SessionEnd accepted")
	}
	if current := handoverMapping(t, project, "old"); !current.Stable || current.ArdviSessionID != old.ArdviSessionID {
		t.Fatal("ordinary SessionEnd lost recovery state")
	}
	// A failed end keeps the session ID for retry and fences old activity.
	if err := endPreviousCodexSessions("http://127.0.0.1:0", state, project, old.MachineID, "main", "next"); err == nil {
		t.Fatal("failed end accepted")
	}
	if current := handoverMapping(t, project, "old"); current.Stable || !current.Superseded || !current.EndPending || current.ArdviSessionID != old.ArdviSessionID {
		t.Fatalf("lost mapping on failure: %+v", current)
	}
	if err := hookSessionEnd("codex", "http://127.0.0.1:0", hookStdin{SessionID: "old", Cwd: dir}); err == nil {
		t.Fatal("failed SessionEnd accepted")
	}
	if current := handoverMapping(t, project, "old"); !current.EndPending || current.ArdviSessionID != old.ArdviSessionID {
		t.Fatal("failed SessionEnd removed recovery state")
	}
	// Changing keys on the same native ID must not finish another agent's end.
	t.Setenv("ARDVI_AGENT_KEY", "worker")
	var out bytes.Buffer
	if err := hookSessionStart(&out, "codex", url, hookStdin{SessionID: "old", Cwd: dir, SingleOrchestrator: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "another identity") {
		t.Fatal("pending end was adopted by a different key:", &out)
	}
	if _, err := callTool(context.Background(), url, project, "session_heartbeat", map[string]any{"session_id": old.ArdviSessionID}); err != nil {
		t.Fatal("different key ended the pending session:", err)
	}
	t.Setenv("ARDVI_AGENT_KEY", "main")
	// Missing native identity must not terminate anything.
	if err := hookSessionStart(io.Discard, "codex", url, hookStdin{Cwd: dir, SingleOrchestrator: true}); err == nil {
		t.Fatal("empty native identity accepted")
	}
	if _, err := os.Stat(filepath.Join(state, mappingKey("codex", "old", project)+".json")); err != nil {
		t.Fatal(err)
	}
}

type handoverWriter func([]byte) (int, error)

func (f handoverWriter) Write(p []byte) (int, error) { return f(p) }

func TestSingleOrchestratorInFlightPromptCannotOverwriteFence(t *testing.T) {
	url, dir, project := handoverFixture(t)
	old := hookStdin{SessionID: "old", Cwd: dir}
	if err := hookSessionStart(io.Discard, "codex", url, old); err != nil {
		t.Fatal(err)
	}
	mapping := handoverMapping(t, project, "old")
	if _, err := callTool(context.Background(), url, project, "message_send", map[string]any{"session_id": mapping.ArdviSessionID, "to_agent_id": mapping.AgentID, "body": "in flight"}); err != nil {
		t.Fatal(err)
	}
	state, _ := ardviStateDir()
	path := filepath.Join(state, mappingKey("codex", "old", project)+".json")
	writer := handoverWriter(func(p []byte) (int, error) {
		// Pause the old delivery after inbox fetch, before its SeenIDs write.
		err := hookSessionStart(io.Discard, "codex", url, hookStdin{SessionID: "next", Cwd: dir, SingleOrchestrator: true})
		return len(p), err
	})
	if err := printUnread(context.Background(), writer, url, path, &mapping); err != nil {
		t.Fatal(err)
	}
	if current := handoverMapping(t, project, "old"); current.Stable || !current.Superseded {
		t.Fatalf("in-flight prompt erased fence: %+v", current)
	}
}

func TestSingleOrchestratorAutomaticCompactMustNotTakeBackOwnership(t *testing.T) {
	url, dir, project := handoverFixture(t)
	old := hookStdin{SessionID: "old", Cwd: dir, SingleOrchestrator: true}
	if err := hookSessionStart(io.Discard, "codex", url, old); err != nil {
		t.Fatal(err)
	}
	next := hookStdin{SessionID: "next", Cwd: dir, SingleOrchestrator: true}
	if err := hookSessionStart(io.Discard, "codex", url, next); err != nil {
		t.Fatal(err)
	}
	var compact hookStdin
	payload, _ := json.Marshal(map[string]string{"session_id": "old", "cwd": dir, "hook_event_name": "SessionStart", "source": "compact"})
	if err := json.Unmarshal(payload, &compact); err != nil {
		t.Fatal(err)
	}
	compact.SingleOrchestrator = true // tolerate hooks generated by the original local patch
	for _, source := range []string{"compact", "clear", "unknown"} {
		compact.Source = source
		if err := hookSessionStart(io.Discard, "codex", url, compact); err != nil {
			t.Fatal(err)
		}
		if current := handoverMapping(t, project, "next"); !current.Stable || current.Superseded {
			t.Fatal("automatic lifecycle event in old conversation superseded the new orchestrator:", source)
		}
	}
	// Compact still renews the current conversation without changing its session.
	before := handoverMapping(t, project, "next")
	next.Source = "compact"
	if err := hookSessionStart(io.Discard, "codex", url, next); err != nil {
		t.Fatal(err)
	}
	if after := handoverMapping(t, project, "next"); after.ArdviSessionID != before.ArdviSessionID {
		t.Fatal("current compact replaced its own session")
	}
}

func TestSingleOrchestratorLostEndResponseMustFenceOldPrompt(t *testing.T) {
	for _, recovery := range []string{"new-start", "old-start", "late-end"} {
		t.Run(recovery, func(t *testing.T) {
			url, dir, project := handoverFixture(t)
			old := hookStdin{SessionID: "old", Cwd: dir}
			if err := hookSessionStart(io.Discard, "codex", url, old); err != nil {
				t.Fatal(err)
			}
			before := handoverMapping(t, project, "old")
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Simulate session_end committed by the real store, but its response lost.
				if _, err := callTool(context.Background(), url, project, "session_end", map[string]any{"session_id": before.ArdviSessionID}); err != nil {
					t.Error(err)
				}
				http.Error(w, "response lost after commit", http.StatusBadGateway)
			}))
			defer proxy.Close()
			var output bytes.Buffer
			next := hookStdin{SessionID: "next", Cwd: dir, SingleOrchestrator: true}
			if err := hookSessionStart(&output, "codex", proxy.URL, next); err != nil {
				t.Fatal(err)
			}
			if err := hookPrompt(io.Discard, "codex", url, old); err != nil {
				t.Fatal(err)
			}
			if current := handoverMapping(t, project, "old"); current.Stable && current.ArdviSessionID != before.ArdviSessionID {
				t.Fatal("old prompt revived registration after committed handover end lost its response")
			}
			if current := handoverMapping(t, project, "old"); !current.EndPending {
				t.Fatal("lost response did not retain retry state")
			}
			if recovery == "late-end" {
				if err := hookSessionEnd("codex", url, old); err != nil {
					t.Fatal(err)
				}
			} else {
				if recovery == "old-start" {
					next.SessionID = "old"
				}
				output.Reset()
				if err := hookSessionStart(&output, "codex", url, next); err != nil || strings.Contains(output.String(), "degraded") {
					t.Fatalf("retry failed: %v %s", err, &output)
				}
				if current := handoverMapping(t, project, next.SessionID); !current.Stable || current.AgentID != before.AgentID {
					t.Fatal("retry did not recover stable identity")
				}
			}
			if current := handoverMapping(t, project, "old"); current.EndPending {
				t.Fatal("retry did not complete pending end")
			}
		})
	}
}

func TestSingleOrchestratorMissingMappingDoesNotGuessOwner(t *testing.T) {
	url, dir, project := handoverFixture(t)
	old := hookStdin{SessionID: "old", Cwd: dir}
	if err := hookSessionStart(io.Discard, "codex", url, old); err != nil {
		t.Fatal(err)
	}
	before := handoverMapping(t, project, "old")
	state, _ := ardviStateDir()
	if err := os.Remove(filepath.Join(state, mappingKey("codex", "old", project)+".json")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := hookSessionStart(&out, "codex", url, hookStdin{SessionID: "next", Cwd: dir, SingleOrchestrator: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "degraded") {
		t.Fatal("missing ownership evidence allowed handover")
	}
	if _, err := callTool(context.Background(), url, project, "session_heartbeat", map[string]any{"session_id": before.ArdviSessionID}); err != nil {
		t.Fatal("unproven owner was ended:", err)
	}
}

func TestSingleOrchestratorSubagentEventCannotHandover(t *testing.T) {
	url, dir, project := handoverFixture(t)
	if err := hookSessionStart(io.Discard, "codex", url, hookStdin{SessionID: "root", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	before := handoverMapping(t, project, "root")
	child := hookStdin{SessionID: "child", Cwd: dir, HookEventName: "SubagentStart", Source: "startup", SingleOrchestrator: true}
	if err := hookSessionStart(io.Discard, "codex", url, child); err != nil {
		t.Fatal(err)
	}
	if after := handoverMapping(t, project, "root"); !after.Stable || after.ArdviSessionID != before.ArdviSessionID {
		t.Fatal("subagent event superseded the root")
	}
	state, _ := ardviStateDir()
	if _, ok := loadMapping(filepath.Join(state, mappingKey("codex", "child", project)+".json")); ok {
		t.Fatal("subagent event created a root registration")
	}
}
