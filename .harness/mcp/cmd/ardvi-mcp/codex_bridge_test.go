package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type fakeCodexDaemon struct {
	t      *testing.T
	socket string
	server *http.Server

	mu          sync.Mutex
	messages    []map[string]any
	accept      bool
	status      string
	parent      string
	requestOnly bool
	rejectTurns int
}

func newFakeCodexDaemon(t *testing.T, accept bool, status string) *fakeCodexDaemon {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "codex.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeCodexDaemon{t: t, socket: socket, accept: accept, status: status}
	fake.server = &http.Server{Handler: http.HandlerFunc(fake.serveHTTP)}
	go func() { _ = fake.server.Serve(listener) }()
	t.Cleanup(func() {
		_ = fake.server.Shutdown(context.Background())
		_ = listener.Close()
	})
	return fake
}

func (f *fakeCodexDaemon) serveHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		f.t.Error(err)
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()
	for {
		var message map[string]any
		if err = wsjson.Read(ctx, conn, &message); err != nil {
			return
		}
		f.mu.Lock()
		f.messages = append(f.messages, message)
		f.mu.Unlock()
		id, hasID := message["id"]
		if !hasID {
			continue
		}
		if f.requestOnly && message["method"] == "initialize" {
			_ = wsjson.Write(ctx, conn, map[string]any{"id": id, "method": "test/request", "params": map[string]any{}})
			continue
		}
		f.mu.Lock()
		rejectTurn := message["method"] == "turn/start" && f.rejectTurns > 0
		if rejectTurn {
			f.rejectTurns--
		}
		f.mu.Unlock()
		if rejectTurn {
			_ = wsjson.Write(ctx, conn, map[string]any{"id": id, "error": map[string]any{
				"code": -32600, "message": "activeTurnNotSteerable",
				"data": map[string]any{"codexErrorInfo": map[string]any{"activeTurnNotSteerable": map[string]any{"turnKind": "compact"}}},
			}})
			continue
		}
		var result any = map[string]any{}
		if message["method"] == "thread/read" {
			result = map[string]any{"thread": map[string]any{
				"id": "native-thread", "status": map[string]any{"type": f.status},
				"canAcceptDirectInput": f.accept, "parentThreadId": nullableString(f.parent),
			}}
		}
		if err = wsjson.Write(ctx, conn, map[string]any{"id": id, "result": result}); err != nil {
			return
		}
	}
}

func TestCodexBridgeDoesNotTreatServerRequestAsResponse(t *testing.T) {
	fake := newFakeCodexDaemon(t, true, "idle")
	fake.requestOnly = true
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := codexDeliver(ctx, fake.socket, "native-thread", "hello"); err == nil {
		t.Fatal("server request with matching id was accepted as a response")
	}
	messages := fake.received()
	if len(messages) != 1 || messages[0]["method"] != "initialize" {
		t.Fatalf("client advanced before initialize response: %#v", messages)
	}
}

func TestCodexBridgeSecondInstanceIsNoOpAndStalePIDFileIsReusable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := osStateDir(t)
	key := mappingKey("codex", "native-thread", "project-id")
	first, acquired, err := acquireBridgePID(dir, key)
	if err != nil || !acquired {
		t.Fatalf("first lock: acquired=%t err=%v", acquired, err)
	}
	defer first.close()
	second, acquired, err := acquireBridgePID(dir, key)
	if err != nil || acquired || second != nil {
		t.Fatalf("second lock: lock=%v acquired=%t err=%v", second, acquired, err)
	}
	fake := newFakeCodexDaemon(t, true, "idle")
	if err = runCodexBridge(context.Background(), []string{
		"--session", "ardvi-session", "--project", "project-id", "--thread", "native-thread",
		"--socket", fake.socket, "--once", "--text", "must not send",
	}); err != nil {
		t.Fatal(err)
	}
	if messages := fake.received(); len(messages) != 0 {
		t.Fatalf("second instance contacted Codex: %#v", messages)
	}
	first.close()
	third, acquired, err := acquireBridgePID(dir, key)
	if err != nil || !acquired {
		t.Fatalf("stale pidfile reuse: acquired=%t err=%v", acquired, err)
	}
	third.close()
}

func TestCodexBridgePollFollowsReconciledMappingSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := osStateDir(t)
	options := codexBridgeOptions{session: "expired-session", project: "project-id", thread: "native-thread"}
	key := mappingKey("codex", options.thread, options.project)
	path := filepath.Join(dir, key+".json")
	if err := saveMapping(path, hookMapping{
		ArdviSessionID: "reconciled-session", ProjectID: options.project, Client: "codex",
		NativeSessionID: options.thread, Stable: true, SeenIDs: []string{"already-seen"},
	}); err != nil {
		t.Fatal(err)
	}
	session, mapping := codexBridgeSession(dir, key, options)
	if session != "reconciled-session" || mapping == nil || len(mapping.SeenIDs) != 1 {
		t.Fatalf("bridge did not follow reconciled mapping: session=%q mapping=%+v", session, mapping)
	}
	if err := saveMapping(path, hookMapping{ArdviSessionID: "wrong", ProjectID: options.project, Client: "claude", NativeSessionID: options.thread, Stable: true}); err != nil {
		t.Fatal(err)
	}
	session, mapping = codexBridgeSession(dir, key, options)
	if session != options.session || mapping != nil {
		t.Fatalf("bridge accepted mismatched mapping: session=%q mapping=%+v", session, mapping)
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (f *fakeCodexDaemon) received() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.messages...)
}

func TestCodexBridgeOnceTextUsesUnixWebSocketProtocol(t *testing.T) {
	fake := newFakeCodexDaemon(t, true, "idle")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	err := runCodexBridge(context.Background(), []string{
		"--session", "ardvi-session", "--project", "project-id", "--thread", "native-thread",
		"--socket", fake.socket, "--once", "--text", "hello bridge",
	})
	if err != nil {
		t.Fatal(err)
	}

	messages := fake.received()
	if len(messages) != 4 {
		t.Fatalf("got %d messages, want initialize, initialized, thread/read, turn/start: %#v", len(messages), messages)
	}
	for _, message := range messages {
		if _, ok := message["jsonrpc"]; ok {
			t.Fatalf("Codex app-server messages must omit jsonrpc: %#v", message)
		}
	}
	wantInitialize := map[string]any{
		"id": float64(0), "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "ardvi-codex-bridge", "title": "Ardvi MCP bridge", "version": version},
			"capabilities": map[string]any{"experimentalApi": true, "requestAttestation": false},
		},
	}
	assertJSONEqual(t, messages[0], wantInitialize)
	assertJSONEqual(t, messages[1], map[string]any{"method": "initialized", "params": map[string]any{}})
	assertJSONEqual(t, messages[2], map[string]any{"id": float64(1), "method": "thread/read", "params": map[string]any{"threadId": "native-thread"}})
	assertJSONEqual(t, messages[3], map[string]any{
		"id": float64(2), "method": "turn/start",
		"params": map[string]any{
			"threadId": "native-thread", "turnTrigger": "ardvi-inbox",
			"input": []any{map[string]any{"type": "text", "text": codexBridgeProvenance + "\nhello bridge"}},
		},
	})
}

func TestCodexBridgeDiscoversSocketFromCodexDaemon(t *testing.T) {
	fake := newFakeCodexDaemon(t, true, "idle")
	binDir := t.TempDir()
	script := filepath.Join(binDir, "codex")
	contents := "#!/bin/sh\nprintf '{\"socketPath\":\"%s\"}\\n' '" + fake.socket + "'\n"
	if err := os.WriteFile(script, []byte(contents), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := runCodexBridge(context.Background(), []string{
		"--session", "ardvi-session", "--project", "project-id", "--thread", "native-thread",
		"--once", "--text", "discovered",
	}); err != nil {
		t.Fatal(err)
	}
	for _, message := range fake.received() {
		if message["method"] == "turn/start" {
			return
		}
	}
	t.Fatal("no turn/start received through discovered socket")
}

func TestCodexBridgeSkipsThreadsThatCannotAcceptInput(t *testing.T) {
	for _, test := range []struct {
		name   string
		accept bool
		status string
		parent string
	}{
		{name: "direct input disabled", status: "idle"},
		{name: "not loaded", accept: true, status: "notLoaded"},
		{name: "subagent", accept: true, status: "idle", parent: "parent-thread"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCodexDaemon(t, test.accept, test.status)
			fake.parent = test.parent
			delivered, err := codexDeliver(context.Background(), fake.socket, "native-thread", "do not send")
			if err != nil {
				t.Fatal(err)
			}
			if delivered {
				t.Fatal("message unexpectedly delivered")
			}
			for _, message := range fake.received() {
				if message["method"] == "turn/start" {
					t.Fatalf("turn/start sent to rejected thread: %#v", message)
				}
			}
		})
	}
}

func TestCodexBridgeDeliversOneInboxMessageInHookFormat(t *testing.T) {
	const created = "2026-09-05T01:02:03Z"
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ardvi-Project") != "project-id" {
			t.Errorf("project header = %q", r.Header.Get("X-Ardvi-Project"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"messages":[{"id":"message-id","from":"sender","scope":"project","thread_id":"topic","body":"hello from Ardvi","ack_required":true,"created":"` + created + `"}]},"isError":false}}`))
	}))
	t.Cleanup(mcp.Close)
	fake := newFakeCodexDaemon(t, true, "idle")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	err := runCodexBridge(context.Background(), []string{
		"--session", "ardvi-session", "--project", "project-id", "--thread", "native-thread",
		"--socket", fake.socket, "--url", mcp.URL, "--once",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "[Ardvi MCP notification — delivered by ardvi codex-bridge, not typed by the user]\n" +
		"Ardvi MCP message id=message-id from=sender scope=project thread=topic created=" + created + " ack_required=true\n" +
		"hello from Ardvi\n\n" +
		"Acknowledge with message_ack(session_id=ardvi-session, message_id=…) for each message above (ids: message-id) and reply in the same thread when a reply is expected.\n"
	var sent string
	for _, message := range fake.received() {
		if message["method"] != "turn/start" {
			continue
		}
		data, _ := json.Marshal(message["params"])
		if !strings.Contains(string(data), "hello from Ardvi") {
			t.Fatalf("unexpected turn/start: %s", data)
		}
		params := message["params"].(map[string]any)
		sent = params["input"].([]any)[0].(map[string]any)["text"].(string)
	}
	if sent != want {
		t.Fatalf("delivered text:\n%s\nwant:\n%s", sent, want)
	}

	seenPath := filepath.Join(osStateDir(t), "inbox-ardvi-session.json")
	seen, err := loadSeen(seenPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "message-id" {
		t.Fatalf("seen ids = %#v", seen)
	}
}

func TestCodexBridgeRetriesActiveTurnNotSteerableWithoutMarkingSeen(t *testing.T) {
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"messages":[{"id":"retry-id","from":"sender","scope":"project","thread_id":"topic","body":"retry me","ack_required":false,"created":"2026-09-05T01:02:03Z"}]},"isError":false}}`))
	}))
	t.Cleanup(mcp.Close)
	fake := newFakeCodexDaemon(t, true, "active")
	fake.rejectTurns = 1
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	args := []string{
		"--session", "ardvi-session", "--project", "project-id", "--thread", "native-thread",
		"--socket", fake.socket, "--url", mcp.URL, "--once",
	}

	if err := runCodexBridge(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	seenPath := filepath.Join(osStateDir(t), "inbox-ardvi-session.json")
	seen, err := loadSeen(seenPath)
	if err != nil || len(seen) != 0 {
		t.Fatalf("failed delivery marked seen: %#v, %v", seen, err)
	}
	if err = runCodexBridge(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	seen, err = loadSeen(seenPath)
	if err != nil || len(seen) != 1 || seen[0] != "retry-id" {
		t.Fatalf("retried delivery seen state: %#v, %v", seen, err)
	}
	turnStarts := 0
	for _, message := range fake.received() {
		if message["method"] == "turn/start" {
			turnStarts++
		}
	}
	if turnStarts != 2 {
		t.Fatalf("turn/start count = %d, want 2", turnStarts)
	}
}

func TestCodexBridgeMigratesLegacyMappingSeenIDsWithoutReplay(t *testing.T) {
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"messages":[` +
			`{"id":"new-id","from":"sender","scope":"project","thread_id":"topic","body":"new body","created":"2026-09-05T01:03:00Z"},` +
			`{"id":"legacy-id","from":"sender","scope":"project","thread_id":"topic","body":"legacy body","created":"2026-09-05T01:02:00Z"}` +
			`]},"isError":false}}`))
	}))
	t.Cleanup(mcp.Close)
	fake := newFakeCodexDaemon(t, true, "idle")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := osStateDir(t)
	key := mappingKey("codex", "native-thread", "project-id")
	if err := saveMapping(filepath.Join(dir, key+".json"), hookMapping{
		ArdviSessionID: "ardvi-session", ProjectID: "project-id", Client: "codex",
		NativeSessionID: "native-thread", SeenIDs: []string{"legacy-id"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := runCodexBridge(context.Background(), []string{
		"--session", "ardvi-session", "--project", "project-id", "--thread", "native-thread",
		"--socket", fake.socket, "--url", mcp.URL, "--once",
	}); err != nil {
		t.Fatal(err)
	}
	for _, message := range fake.received() {
		if message["method"] != "turn/start" {
			continue
		}
		params := message["params"].(map[string]any)
		text := params["input"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "new body") || strings.Contains(text, "legacy body") {
			t.Fatalf("legacy delivery replayed or new delivery missing: %q", text)
		}
	}
	seen, err := loadSeen(filepath.Join(dir, "inbox-ardvi-session.json"))
	if err != nil || len(seen) != 2 || seen[0] != "legacy-id" || seen[1] != "new-id" {
		t.Fatalf("migrated seen state = %#v, %v", seen, err)
	}
}

func osStateDir(t *testing.T) string {
	t.Helper()
	dir, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("got %s\nwant %s", gotJSON, wantJSON)
	}
}
