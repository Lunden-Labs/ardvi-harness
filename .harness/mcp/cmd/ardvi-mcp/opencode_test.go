package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var errOpenCodeExec = errors.New("stop before exec")

type openCodeAPICall struct {
	operation string
	params    map[string]string
	payload   any
}

func TestBoundOpenCodeLaunchRegistersTrustedStableIdentityAndResumes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	config := t.TempDir()
	t.Setenv("ARDVI_CONFIG_DIR", config)
	t.Setenv("ARDVI_AGENT_KEY", "poisoned-env")
	t.Setenv("ARDVI_SESSION_NAME", "poisoned-name")
	if err := os.WriteFile(filepath.Join(config, "agents.json"), []byte(`{"schema":1,"agents":[{"client":"codex","model":"gpt","model_provider":"openai","agent_key":"main","session_name":"cloud-main"},{"client":"opencode","model":"qwen3.8-27b","model_provider":"vllm","agent_key":"local-qwen38","session_name":"qwen38-local"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	project := "23232323-2323-4232-8232-232323232323"
	dir := writeTestProject(t, project)
	t.Chdir(dir)
	url := newHookTestServer(t)
	t.Setenv("ARDVI_MCP_URL", url)
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")

	originalAPI, originalService, originalExec, originalBridge := runOpenCodeAPI, startOpenCodeService, execOpenCode, launchOpenCodeBridge
	t.Cleanup(func() {
		runOpenCodeAPI, startOpenCodeService, execOpenCode, launchOpenCodeBridge = originalAPI, originalService, originalExec, originalBridge
	})
	var calls []openCodeAPICall
	startOpenCodeService = func(context.Context, string) error { return nil }
	launchOpenCodeBridge = func(string, string, string, string) error { return nil }
	runOpenCodeAPI = func(_ context.Context, _ string, operation string, params map[string]string, payload any, out any) error {
		calls = append(calls, openCodeAPICall{operation: operation, params: params, payload: payload})
		if operation == "v2.session.create" || operation == "v2.session.get" {
			value := out.(*opencodeSession)
			value.ID = "oc-1"
			value.Location.Directory = dir
			value.Model.ID, value.Model.ProviderID = "qwen3.8-27b", "vllm"
		}
		return nil
	}
	var execArgs []string
	execOpenCode = func(_ string, args []string, _ []string) error {
		execArgs = append([]string(nil), args...)
		return errOpenCodeExec
	}

	if err := launchBoundOpenCode("opencode", "local-qwen38", nil); !errors.Is(err, errOpenCodeExec) {
		t.Fatalf("fresh launch = %v", err)
	}
	if got, want := operationNames(calls), []string{"v2.session.create", "v2.session.synthetic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fresh API calls = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(execArgs, []string{"opencode", "--session", "oc-1"}) {
		t.Fatalf("exec args = %v", execArgs)
	}
	if payload, ok := calls[1].payload.(map[string]any); !ok || payload["resume"] != false || payload["delivery"] != "queue" {
		t.Fatalf("fresh lifecycle materialization = %#v", calls[1].payload)
	} else if text, _ := payload["text"].(string); !strings.Contains(text, "context_bootstrap(session_id=") || !strings.Contains(text, "project=23232323-2323-4232-8232-232323232323") {
		t.Fatalf("fresh lifecycle context = %q", text)
	}
	state, _ := ardviStateDir()
	open := openCodeMapping(t, state, project, "oc-1")
	if open.Client != "opencode" || open.AgentKey != "local-qwen38" || open.Name != "qwen38-local" || open.NativeThreadID != "oc-1" {
		t.Fatalf("OpenCode mapping = %+v", open)
	}
	binding, ok, err := loadOpenCodeBinding(state, project, "oc-1")
	if err != nil || !ok || binding.AgentKey != "local-qwen38" || binding.Model != "qwen3.8-27b" || binding.ModelProvider != "vllm" || binding.ModelVariant != "default" {
		t.Fatalf("OpenCode binding = %+v, %t, %v", binding, ok, err)
	}
	if err := hookSessionStartMode(io.Discard, "claude", url, hookStdin{SessionID: "cloud", Cwd: dir, NativeVerified: true, AgentKey: "main", SessionName: "cloud-main"}, false); err != nil {
		t.Fatal(err)
	}
	cloud := testMapping(t, state, "claude", project, "cloud")
	if cloud.AgentID == open.AgentID || cloud.Client != "claude" {
		t.Fatalf("cross-client identities cloud=%+v open=%+v", cloud, open)
	}
	calls = nil
	if err := launchBoundOpenCode("opencode", "local-qwen38", nil); !errors.Is(err, errOpenCodeExec) {
		t.Fatalf("default reopen = %v", err)
	}
	if got, want := operationNames(calls), []string{"v2.session.get"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default reopen API calls = %v, want %v", got, want)
	}
	if current := openCodeMapping(t, state, project, "oc-1"); current.AgentID != open.AgentID || current.ArdviSessionID != open.ArdviSessionID {
		t.Fatalf("default reopen changed identity: old=%+v current=%+v", open, current)
	}

	if err := hookSessionEnd("opencode", url, hookStdin{SessionID: "oc-1", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	raw, err := callTool(context.Background(), url, project, "message_send", map[string]any{"session_id": cloud.ArdviSessionID, "to_agent_id": open.AgentID, "kind": "request", "body": "offline OpenCode work", "idempotency_key": "open-offline"})
	if err != nil || len(raw) == 0 {
		t.Fatalf("queue offline request: %s %v", raw, err)
	}
	calls = nil
	if err = launchBoundOpenCode("opencode", "local-qwen38", []string{"--session", "oc-1"}); !errors.Is(err, errOpenCodeExec) {
		t.Fatalf("resume launch = %v", err)
	}
	if got, want := operationNames(calls), []string{"v2.session.get"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resume API calls = %v, want %v", got, want)
	}
	resumed := openCodeMapping(t, state, project, "oc-1")
	if resumed.AgentID != open.AgentID || resumed.ArdviSessionID == open.ArdviSessionID {
		t.Fatalf("resume identity old=%+v new=%+v", open, resumed)
	}
	seenPath := filepath.Join(state, "inbox-"+resumed.ArdviSessionID+".json")
	if _, err := os.Stat(seenPath); !os.IsNotExist(err) {
		t.Fatalf("registration marked queued work seen: %v", err)
	}
}

func TestOpenCodeDoesNotMatchCodexOnlyAgentConfig(t *testing.T) {
	config := t.TempDir()
	t.Setenv("ARDVI_CONFIG_DIR", config)
	if err := os.WriteFile(filepath.Join(config, "agents.json"), []byte(`{"schema":1,"agents":[{"client":"codex","model":"qwen","model_provider":"vllm","agent_key":"local"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadNativeAgents()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.byClientKey("opencode", "local"); ok {
		t.Fatal("OpenCode accepted a Codex-only binding")
	}
	if err := os.WriteFile(filepath.Join(config, "agents.json"), []byte(`{"schema":1,"agents":[{"client":"opencode","model":"qwen","model_provider":"vllm","agent_key":"local"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadNativeAgents()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.byClientKey("codex", "local"); ok {
		t.Fatal("Codex accepted an OpenCode-only binding")
	}
}

func TestOpenCodeAPIReadsV2Envelope(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "opencode")
	response := `{"data":{"id":"oc-envelope","parentID":"","model":{"id":"qwen","providerID":"vllm","variant":"default"},"location":{"directory":"/work"}}}`
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '"+response+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var session opencodeSession
	if err := opencodeAPI(context.Background(), binary, "v2.session.get", map[string]string{"sessionID": "oc-envelope"}, nil, &session); err != nil {
		t.Fatal(err)
	}
	if session.ID != "oc-envelope" || session.Model.ProviderID != "vllm" || session.Location.Directory != "/work" {
		t.Fatalf("envelope session = %+v", session)
	}
}

func TestOpenCodeAPIFencesAuthoritativeMissingSession(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '{\"_tag\":\"SessionNotFoundError\",\"sessionID\":\"gone\"}'\necho 'HTTP 404 Not Found' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var session opencodeSession
	if err := opencodeAPI(context.Background(), binary, "v2.session.get", map[string]string{"sessionID": "gone"}, nil, &session); !errors.Is(err, errOpenCodeMissing) {
		t.Fatalf("missing session error = %v", err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '{\"_tag\":\"ServiceNotFoundError\",\"sessionID\":\"gone\"}'\necho 'HTTP 404 Not Found' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := opencodeAPI(context.Background(), binary, "v2.session.get", map[string]string{"sessionID": "gone"}, nil, &session); errors.Is(err, errOpenCodeMissing) {
		t.Fatalf("service failure was incorrectly fenced: %v", err)
	}
}

func TestOpenCodeBridgeDeliversOneMessagePerStableRetryAndRecordsReceipt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	project := "25252525-2525-4252-8252-252525252525"
	dir := writeTestProject(t, project)
	url := newHookTestServer(t)
	state, _ := ardviStateDir()
	agent := nativeAgent{Client: "opencode", AgentKey: "local", SessionName: "qwen-local", Model: "qwen", ModelProvider: "vllm"}
	if err := saveOpenCodeBinding(state, project, "oc-bridge", agent, dir); err != nil {
		t.Fatal(err)
	}
	if err := hookSessionStartMode(io.Discard, "opencode", url, hookStdin{SessionID: "oc-bridge", Cwd: dir, NativeVerified: true, AgentKey: agent.AgentKey, SessionName: agent.SessionName}, false); err != nil {
		t.Fatal(err)
	}
	if err := hookSessionStartMode(io.Discard, "claude", url, hookStdin{SessionID: "cloud", Cwd: dir, NativeVerified: true, AgentKey: "main", SessionName: "cloud-main"}, false); err != nil {
		t.Fatal(err)
	}
	open := openCodeMapping(t, state, project, "oc-bridge")
	cloud := testMapping(t, state, "claude", project, "cloud")
	send := func(key, body string) string {
		raw, err := callTool(context.Background(), url, project, "message_send", map[string]any{"session_id": cloud.ArdviSessionID, "to_agent_id": open.AgentID, "kind": "request", "body": body, "idempotency_key": key})
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(raw, &result); err != nil || result.ID == "" {
			t.Fatalf("send = %s %v", raw, err)
		}
		return result.ID
	}
	first := send("open-first", "first work")
	originalAPI := runOpenCodeAPI
	t.Cleanup(func() { runOpenCodeAPI = originalAPI })
	var promptIDs []string
	failFirst := true
	badResponse := false
	runOpenCodeAPI = func(_ context.Context, _ string, operation string, _ map[string]string, payload any, out any) error {
		switch operation {
		case "v2.session.get":
			value := out.(*opencodeSession)
			value.ID, value.Location.Directory = "oc-bridge", dir
			value.Model.ID, value.Model.ProviderID, value.Model.Variant = "qwen", "vllm", "default"
			return nil
		case "v2.session.prompt":
			value := payload.(map[string]any)
			id := value["id"].(string)
			if text, _ := value["text"].(string); !strings.Contains(text, "context_bootstrap(session_id=") || !strings.Contains(text, "Ardvi MCP notification") {
				t.Fatalf("delivery lifecycle context = %q", text)
			}
			promptIDs = append(promptIDs, id)
			if failFirst {
				failFirst = false
				return errors.New("OpenCode temporarily unavailable")
			}
			response := out.(*opencodePrompt)
			if badResponse {
				response.ID, response.SessionID = "wrong", "wrong"
				return nil
			}
			response.ID, response.SessionID = id, "oc-bridge"
			return nil
		default:
			t.Fatalf("unexpected OpenCode API operation %q", operation)
			return nil
		}
	}
	options := opencodeBridgeOptions{project: project, session: "oc-bridge", binary: "opencode", url: url, interval: time.Millisecond}
	if err := opencodeBridgePoll(context.Background(), options, state); err == nil {
		t.Fatal("failed prompt admission was accepted")
	}
	seenPath := filepath.Join(state, "inbox-"+open.ArdviSessionID+".json")
	if _, err := os.Stat(seenPath); !os.IsNotExist(err) {
		t.Fatalf("failed prompt was marked seen: %v", err)
	}
	second := send("open-second", "second work")
	if err := opencodeBridgePoll(context.Background(), options, state); err != nil {
		t.Fatal(err)
	}
	if len(promptIDs) != 2 || promptIDs[0] != promptIDs[1] {
		t.Fatalf("retry ID changed when inbox grew: %v", promptIDs)
	}
	seen, err := loadSeen(seenPath)
	if err != nil || !reflect.DeepEqual(seen, []string{first}) {
		t.Fatalf("first delivery seen = %v %v", seen, err)
	}
	raw, err := callTool(context.Background(), url, project, "inbox_read", map[string]any{"session_id": open.ArdviSessionID})
	if err != nil || !jsonContains(raw, "delivered") {
		t.Fatalf("delivery receipt = %s %v", raw, err)
	}
	raw, err = callTool(context.Background(), url, project, "request_accept", map[string]any{"session_id": open.ArdviSessionID, "message_id": first})
	if err != nil {
		t.Fatal(err)
	}
	var accepted struct {
		AcceptanceToken string `json:"acceptance_token"`
	}
	if err = json.Unmarshal(raw, &accepted); err != nil || accepted.AcceptanceToken == "" {
		t.Fatalf("request accept = %s %v", raw, err)
	}
	if _, err = callTool(context.Background(), url, project, "request_complete", map[string]any{"session_id": open.ArdviSessionID, "message_id": first, "acceptance_token": accepted.AcceptanceToken, "result": "OpenCode complete"}); err != nil {
		t.Fatal(err)
	}
	raw, err = callTool(context.Background(), url, project, "inbox_read", map[string]any{"session_id": cloud.ArdviSessionID})
	if err != nil || !jsonContains(raw, "OpenCode complete") {
		t.Fatalf("result routing = %s %v", raw, err)
	}
	if err = opencodeBridgePoll(context.Background(), options, state); err != nil {
		t.Fatal(err)
	}
	seen, err = loadSeen(seenPath)
	if err != nil || !reflect.DeepEqual(seen, []string{first, second}) {
		t.Fatalf("second delivery seen = %v %v", seen, err)
	}
	third := send("open-third", "third work")
	badResponse = true
	if err = opencodeBridgePoll(context.Background(), options, state); err == nil {
		t.Fatal("mismatched native admission was accepted")
	}
	seen, err = loadSeen(seenPath)
	if err != nil || !reflect.DeepEqual(seen, []string{first, second}) {
		t.Fatalf("mismatched native admission marked seen = %v %v third=%s", seen, err, third)
	}
}

func TestOpenCodeBridgeFencesChangedSessionAndBusyQueue(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	project := "26262626-2626-4262-8262-262626262626"
	dir := writeTestProject(t, project)
	url := newHookTestServer(t)
	state, _ := ardviStateDir()
	agent := nativeAgent{Client: "opencode", AgentKey: "local", Model: "qwen", ModelProvider: "vllm"}
	if err := saveOpenCodeBinding(state, project, "oc-fence", agent, dir); err != nil {
		t.Fatal(err)
	}
	if err := hookSessionStartMode(io.Discard, "opencode", url, hookStdin{SessionID: "oc-fence", Cwd: dir, NativeVerified: true, AgentKey: "local"}, false); err != nil {
		t.Fatal(err)
	}
	open := openCodeMapping(t, state, project, "oc-fence")
	options := opencodeBridgeOptions{project: project, session: "oc-fence", binary: "opencode", url: url, interval: time.Millisecond}
	originalAPI := runOpenCodeAPI
	t.Cleanup(func() { runOpenCodeAPI = originalAPI })
	runOpenCodeAPI = func(_ context.Context, _ string, operation string, _ map[string]string, _ any, out any) error {
		if operation != "v2.session.get" {
			t.Fatalf("fenced bridge sent %s", operation)
		}
		value := out.(*opencodeSession)
		value.ID, value.Location.Directory = "oc-fence", dir
		value.ParentID = "parent"
		value.Model.ID, value.Model.ProviderID, value.Model.Variant = "qwen", "vllm", "default"
		return nil
	}
	if err := opencodeBridgePoll(context.Background(), options, state); !errors.Is(err, errOpenCodeFenced) {
		t.Fatalf("changed root = %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "inbox-"+open.ArdviSessionID+".json")); !os.IsNotExist(err) {
		t.Fatalf("fenced bridge touched seen state: %v", err)
	}

	runOpenCodeAPI = func(_ context.Context, _ string, operation string, _ map[string]string, _ any, out any) error {
		if operation != "v2.session.get" {
			t.Fatalf("busy bridge sent %s", operation)
		}
		value := out.(*opencodeSession)
		value.ID, value.Location.Directory = "oc-fence", dir
		value.Model.ID, value.Model.ProviderID, value.Model.Variant = "qwen", "vllm", "default"
		return nil
	}
	var lock sync.WaitGroup
	locked := make(chan struct{})
	lock.Add(1)
	go func() {
		defer lock.Done()
		file, err := os.OpenFile(filepath.Join(state, "inbox-"+open.ArdviSessionID+".json.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
			t.Error(err)
			return
		}
		defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		close(locked)
		time.Sleep(30 * time.Millisecond)
	}()
	<-locked
	if err := opencodeBridgePoll(context.Background(), options, state); err != nil {
		t.Fatalf("busy bridge = %v", err)
	}
	lock.Wait()
}

func TestBoundOpenCodeRejectsSubagentAndIdentityDriftBeforeRegistration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	config := t.TempDir()
	t.Setenv("ARDVI_CONFIG_DIR", config)
	if err := os.WriteFile(filepath.Join(config, "agents.json"), []byte(`{"schema":1,"agents":[{"client":"opencode","model":"qwen","model_provider":"vllm","agent_key":"local"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	project := "24242424-2424-4242-8242-242424242424"
	dir := writeTestProject(t, project)
	t.Chdir(dir)
	originalAPI, originalService, originalExec, originalBridge := runOpenCodeAPI, startOpenCodeService, execOpenCode, launchOpenCodeBridge
	t.Cleanup(func() {
		runOpenCodeAPI, startOpenCodeService, execOpenCode, launchOpenCodeBridge = originalAPI, originalService, originalExec, originalBridge
	})
	startOpenCodeService = func(context.Context, string) error { return nil }
	launchOpenCodeBridge = func(string, string, string, string) error { return nil }
	execOpenCode = func(string, []string, []string) error { t.Fatal("identity drift reached OpenCode exec"); return nil }
	for _, tc := range []struct {
		name   string
		mutate func(*opencodeSession)
	}{
		{"subagent", func(session *opencodeSession) { session.ParentID = "root" }},
		{"wrong-provider", func(session *opencodeSession) { session.Model.ProviderID = "openai" }},
		{"wrong-project", func(session *opencodeSession) { session.Location.Directory = t.TempDir() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runOpenCodeAPI = func(_ context.Context, _ string, operation string, _ map[string]string, _ any, out any) error {
				if operation != "v2.session.get" {
					t.Fatalf("unexpected API operation %q", operation)
				}
				value := out.(*opencodeSession)
				value.ID, value.Location.Directory = "oc-drift", dir
				value.Model.ID, value.Model.ProviderID = "qwen", "vllm"
				tc.mutate(value)
				return nil
			}
			if err := launchBoundOpenCode("opencode", "local", []string{"--session", "oc-drift"}); err == nil {
				t.Fatal("identity drift was accepted")
			}
			state, _ := ardviStateDir()
			if _, exists := loadMapping(filepath.Join(state, mappingKey("opencode", "oc-drift", project)+".json")); exists {
				t.Fatal("identity drift registered a mapping")
			}
		})
	}
}

func operationNames(calls []openCodeAPICall) []string {
	out := make([]string, len(calls))
	for i := range calls {
		out[i] = calls[i].operation
	}
	return out
}

func openCodeMapping(t *testing.T, state, project, session string) hookMapping {
	t.Helper()
	mapping, ok := loadMapping(filepath.Join(state, mappingKey("opencode", session, project)+".json"))
	if !ok {
		t.Fatalf("missing OpenCode mapping for %s", session)
	}
	return mapping
}

func testMapping(t *testing.T, state, client, project, session string) hookMapping {
	t.Helper()
	mapping, ok := loadMapping(filepath.Join(state, mappingKey(client, session, project)+".json"))
	if !ok {
		t.Fatalf("missing %s mapping for %s", client, session)
	}
	return mapping
}

func jsonContains(raw []byte, want string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(value any) bool {
		switch value := value.(type) {
		case string:
			return value == want
		case []any:
			for _, item := range value {
				if visit(item) {
					return true
				}
			}
		case map[string]any:
			for _, item := range value {
				if visit(item) {
					return true
				}
			}
		}
		return false
	}
	return visit(value)
}
