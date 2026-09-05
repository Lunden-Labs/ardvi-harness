package hub_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ardvi/harness/mcp/internal/catalog"
	"github.com/ardvi/harness/mcp/internal/hub"
	"github.com/ardvi/harness/mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerTransport struct{ base http.RoundTripper }

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Ardvi-Project", "99999999-9999-4999-8999-999999999999")
	return h.base.RoundTrip(r)
}

// newSession spins up a real hub.New server behind httptest, matching the
// harness used by cmd/ardvi-mcp's TestStreamableHTTP: exercising the actual
// wire contract catches more than a hand-rolled JSON-RPC fake would.
func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	server := hub.New(s, &catalog.Catalog{Version: 1}, "test")
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	ts := httptest.NewServer(mux)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: headerTransport{http.DefaultTransport}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close(); ts.Close(); s.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.IsError {
		encoded, _ := json.Marshal(result)
		t.Fatalf("%s failed: %s", name, encoded)
	}
	b, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUnreadPiggyback(t *testing.T) {
	session := newSession(t)
	a := call(t, session, "session_start", map[string]any{"name": "a", "client": "codex"})
	b := call(t, session, "session_start", map[string]any{"name": "b", "client": "codex"})
	aID, bID := a["id"].(string), b["id"].(string)

	// b sends a to an unacknowledged message.
	call(t, session, "message_send", map[string]any{"session_id": bID, "to": aID, "body": "hi"})

	// a's own message_send should carry that unread message as a piggyback.
	out := call(t, session, "message_send", map[string]any{"session_id": aID, "to": bID, "body": "reply"})
	if n, _ := out["unread_count"].(float64); n != 1 {
		t.Fatalf("message_send: expected unread_count 1, got %#v", out["unread_count"])
	}
	unread, _ := out["unread"].([]any)
	if len(unread) != 1 {
		t.Fatalf("message_send: expected one unread message, got %#v", out["unread"])
	}
	msgID := unread[0].(map[string]any)["id"].(string)

	// claim_acquire carries the same piggyback.
	claimOut := call(t, session, "claim_acquire", map[string]any{"session_id": aID, "resource": "file.go"})
	if n, _ := claimOut["unread_count"].(float64); n != 1 {
		t.Fatalf("claim_acquire: missing unread piggyback: %#v", claimOut)
	}

	// acknowledging drops the count to zero, so the field disappears (omitempty).
	ackOut := call(t, session, "message_ack", map[string]any{"session_id": aID, "message_id": msgID})
	if _, has := ackOut["unread"]; has {
		t.Fatalf("message_ack: expected no unread field once inbox is empty, got %#v", ackOut)
	}
	if _, has := ackOut["unread_count"]; has {
		t.Fatalf("message_ack: expected no unread_count field once inbox is empty, got %#v", ackOut)
	}

	releaseOut := call(t, session, "claim_release", map[string]any{"session_id": aID, "resource": "file.go"})
	if _, has := releaseOut["unread"]; has {
		t.Fatalf("claim_release: expected no unread field, got %#v", releaseOut)
	}

	// session_end and inbox_read are unaffected: no unread field at all.
	endResult, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "session_end", Arguments: map[string]any{"session_id": bID}})
	if err != nil || endResult.IsError {
		t.Fatalf("session_end failed: %v %#v", err, endResult)
	}
	raw, _ := json.Marshal(endResult.StructuredContent)
	if strings.Contains(string(raw), "unread") {
		t.Fatalf("session_end must not carry the unread piggyback: %s", raw)
	}
}

func TestMessageAndMemoryTextLimit(t *testing.T) {
	session := newSession(t)
	a := call(t, session, "session_start", map[string]any{"name": "a", "client": "codex"})
	aID := a["id"].(string)

	tooBig := strings.Repeat("x", 16*1024+1)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "message_send", Arguments: map[string]any{"session_id": aID, "body": tooBig}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("message_send: expected a body over 16 KiB to be rejected")
	}

	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_put", Arguments: map[string]any{"text": tooBig}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("memory_put: expected text over 16 KiB to be rejected")
	}

	exact := strings.Repeat("x", 16*1024)
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "message_send", Arguments: map[string]any{"session_id": aID, "body": exact}})
	if err != nil || result.IsError {
		t.Fatalf("message_send: expected exactly 16 KiB to be accepted: %v %#v", err, result)
	}
}
