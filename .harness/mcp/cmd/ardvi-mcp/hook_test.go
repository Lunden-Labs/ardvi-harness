package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ardvi/harness/mcp/internal/catalog"
	"github.com/ardvi/harness/mcp/internal/hub"
	"github.com/ardvi/harness/mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newHookTestServer wires the real hub server behind an httptest server, so
// hook and inbox commands are exercised against the actual JSON-RPC contract
// rather than a hand-written fake.
func newHookTestServer(t *testing.T) string {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	server := hub.New(s, &catalog.Catalog{Version: 1}, "test")
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}

func writeTestProject(t *testing.T, id string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".ardvi"), 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"id":%q,"name":"proj"}`, id)
	if err := os.WriteFile(filepath.Join(dir, ".ardvi", "project.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHookSessionStartRegistersAndWritesMapping(t *testing.T) {
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	url := newHookTestServer(t)
	const projectID = "11111111-1111-4111-8111-111111111111"
	dir := writeTestProject(t, projectID)
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv("ARDVI_SESSION_NAME", "hooktest-a")

	in := hookStdin{SessionID: "client-sess-1", Cwd: dir, HookEventName: "SessionStart"}
	var out bytes.Buffer
	if err := hookSessionStart(&out, "codex", url, in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "name=hooktest-a") || !strings.Contains(out.String(), "do not call session_start again") {
		t.Fatalf("missing registration paragraph: %s", out.String())
	}

	path := filepath.Join(stateDir, "ardvi", "sessions", mappingKey("codex", "client-sess-1", projectID)+".json")
	mapping, ok := loadMapping(path)
	if !ok {
		t.Fatal("mapping was not written")
	}
	if mapping.Name != "hooktest-a" || mapping.ProjectID != projectID || mapping.ArdviSessionID == "" || mapping.NativeSessionID != "client-sess-1" {
		t.Fatalf("bad mapping: %+v", mapping)
	}
}

func TestHookPromptPrintsUnseenOnceThenNothing(t *testing.T) {
	url := newHookTestServer(t)
	const projectID = "22222222-2222-4222-8222-222222222222"
	dir := writeTestProject(t, projectID)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ARDVI_SESSION_NAME", "hooktest-b")

	in := hookStdin{SessionID: "client-sess-2", Cwd: dir}
	var start bytes.Buffer
	if err := hookSessionStart(&start, "claude", url, in); err != nil {
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
	if _, err = callTool(ctx, url, projectID, "message_send", map[string]any{"session_id": sender.ID, "to": "hooktest-b", "body": "hello there"}); err != nil {
		t.Fatal(err)
	}

	var first bytes.Buffer
	if err = hookPrompt(&first, "claude", url, in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "hello there") {
		t.Fatalf("expected message body in first prompt output: %s", first.String())
	}
	if !strings.Contains(first.String(), "Acknowledge with message_ack") {
		t.Fatalf("expected ack hint: %s", first.String())
	}
	if strings.Count(first.String(), "Ardvi MCP message id=") != 1 {
		t.Fatalf("expected exactly one message printed: %s", first.String())
	}
	stateDir, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	seen, err := loadSeen(filepath.Join(stateDir, "inbox-"+mappingSessionID(t, "claude", "client-sess-2", projectID)+".json"))
	if err != nil || len(seen) != 1 {
		t.Fatalf("shared seen state = %#v, %v", seen, err)
	}

	var second bytes.Buffer
	if err = hookPrompt(&second, "claude", url, in); err != nil {
		t.Fatal(err)
	}
	if second.String() != "" {
		t.Fatalf("expected nothing on repeat prompt, got: %s", second.String())
	}
}

func mappingSessionID(t *testing.T, client, native, project string) string {
	t.Helper()
	dir, err := ardviStateDir()
	if err != nil {
		t.Fatal(err)
	}
	mapping, ok := loadMapping(filepath.Join(dir, mappingKey(client, native, project)+".json"))
	if !ok {
		t.Fatal("mapping missing")
	}
	return mapping.ArdviSessionID
}

func TestHookSessionEndDeletesMapping(t *testing.T) {
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	url := newHookTestServer(t)
	const projectID = "33333333-3333-4333-8333-333333333333"
	dir := writeTestProject(t, projectID)
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Setenv("ARDVI_SESSION_NAME", "hooktest-c")

	in := hookStdin{SessionID: "client-sess-3", Cwd: dir}
	var out bytes.Buffer
	if err := hookSessionStart(&out, "codex", url, in); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "ardvi", "sessions", mappingKey("codex", "client-sess-3", projectID)+".json")
	if _, ok := loadMapping(path); !ok {
		t.Fatal("mapping missing before session-end")
	}

	if err := hookSessionEnd("codex", url, in); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadMapping(path); ok {
		t.Fatal("mapping should have been deleted")
	}
}

func TestHookCommandNeverFailsOnServerDown(t *testing.T) {
	t.Setenv("ARDVI_CODEX_BRIDGE_DISABLE", "1")
	const projectID = "44444444-4444-4444-8444-444444444444"
	dir := writeTestProject(t, projectID)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("ARDVI_SESSION_NAME", "hooktest-d")

	stdin, err := json.Marshal(hookStdin{SessionID: "client-sess-4", Cwd: dir})
	if err != nil {
		t.Fatal(err)
	}
	restore := swapStdin(t, stdin)
	defer restore()

	// Port 1 is reserved and never accepts connections; the call must fail
	// fast without hookCommand ever returning a non-nil error.
	if err := hookCommand([]string{"session-start", "--client", "codex", "--url", "http://127.0.0.1:1/mcp"}); err != nil {
		t.Fatalf("hookCommand must never return an error, got %v", err)
	}
}

func TestHookCommandMalformedStdinAndNoProject(t *testing.T) {
	dir := t.TempDir() // no .ardvi/project.json anywhere above a bare tmp dir
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Malformed stdin plus a cwd carrying no project: both must be tolerated
	// silently rather than propagated as errors. hookStdin.Cwd stands in for
	// the client's reported cwd so the test does not touch the process cwd.
	restore := swapStdin(t, []byte("not json"))
	defer restore()
	stdinAfterParse := hookStdin{Cwd: dir}
	if err := hookPrompt(&bytes.Buffer{}, "claude", "http://127.0.0.1:1/mcp", stdinAfterParse); err != nil {
		t.Fatalf("hookPrompt must treat a project-less cwd as a no-op, got %v", err)
	}
	if err := hookCommand([]string{"prompt", "--client", "claude", "--url", "http://127.0.0.1:1/mcp"}); err != nil {
		t.Fatalf("hookCommand must never return an error, got %v", err)
	}
}

// swapStdin replaces os.Stdin with a pipe fed by data and returns a restore func.
func swapStdin(t *testing.T, data []byte) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = r
	go func() {
		w.Write(data)
		w.Close()
	}()
	return func() { os.Stdin = original }
}

func TestInboxLoopPrintsOnceThenNothing(t *testing.T) {
	url := newHookTestServer(t)
	const projectID = "55555555-5555-4555-8555-555555555555"
	writeTestProject(t, projectID) // exercised only for parity; --project is passed explicitly here
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	ctx := context.Background()
	raw, err := callTool(ctx, url, projectID, "session_start", map[string]any{"name": "watcher", "client": "codex"})
	if err != nil {
		t.Fatal(err)
	}
	var watcher struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(raw, &watcher); err != nil {
		t.Fatal(err)
	}
	if _, err = callTool(ctx, url, projectID, "message_send", map[string]any{"session_id": watcher.ID, "to": watcher.ID, "body": "ping"}); err != nil {
		t.Fatal(err)
	}

	args := []string{"--session", watcher.ID, "--project", projectID, "--url", url}
	var first bytes.Buffer
	if err = runInboxLoop(&first, args, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "ping") {
		t.Fatalf("expected message body, got: %s", first.String())
	}

	var second bytes.Buffer
	if err = runInboxLoop(&second, args, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	if second.String() != "" {
		t.Fatalf("expected nothing on repeat inbox call, got: %s", second.String())
	}
}

func TestInboxSkipsImmediatelyWhenBridgeOwnsSeenState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox-session.json")
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err = inboxOnce(&bytes.Buffer{}, "http://127.0.0.1:1", "project", "session", path); err != nil {
		t.Fatalf("busy seen state should be a no-op, got %v", err)
	}
}
