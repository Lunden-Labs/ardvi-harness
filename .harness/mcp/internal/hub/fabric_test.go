package hub_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ardvi/harness/mcp/internal/catalog"
	"github.com/ardvi/harness/mcp/internal/hub"
	"github.com/ardvi/harness/mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fabricTransport struct{ project string }

func (h fabricTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Ardvi-Project", h.project)
	return http.DefaultTransport.RoundTrip(r)
}
func fabricClients(t *testing.T, projects ...string) (*store.Store, []*mcp.ClientSession) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := hub.New(s, &catalog.Catalog{Version: 1}, "test")
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	ts := httptest.NewServer(handler)
	clients := make([]*mcp.ClientSession, 0, len(projects))
	for _, project := range projects {
		client := mcp.NewClient(&mcp.Implementation{Name: "fabric-test", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: &http.Client{Transport: fabricTransport{project}}, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, session)
	}
	t.Cleanup(func() {
		for _, client := range clients {
			client.Close()
		}
		ts.Close()
		s.Close()
	})
	return s, clients
}
func registerFabric(t *testing.T, client *mcp.ClientSession, provider, project, native string) map[string]any {
	t.Helper()
	return call(t, client, "session_start", map[string]any{"name": provider + "-" + project, "client": provider, "machine_id": "test-machine", "project_name": project, "native_session_id": native})
}

func TestFabricOfflineNativeRestartAndRequestResult(t *testing.T) {
	const a = "11111111-1111-4111-8111-111111111111"
	const b = "22222222-2222-4222-8222-222222222222"
	_, clients := fabricClients(t, a, b)
	codex, claude := clients[0], clients[1]
	c := registerFabric(t, codex, "codex", "core", "C1")
	d := registerFabric(t, claude, "claude", "sdk", "D1")
	cID, dID := c["id"].(string), d["id"].(string)
	boot := validateOutputSchema(t, codex, "context_bootstrap", map[string]any{"session_id": cID})
	self := boot["self"].(map[string]any)
	if self["agent_id"] != c["agent_id"] || self["project_id"] != a {
		t.Fatalf("wrong self: %v", self)
	}
	for _, key := range []string{"protocol", "operating_rules", "spaces", "peers", "pending_requests", "claims", "memory", "message_quota"} {
		if _, ok := boot[key]; !ok {
			t.Fatalf("bootstrap missing %s", key)
		}
	}
	quota := boot["message_quota"].(map[string]any)
	if quota["limit"] != float64(1000) || quota["warning"] != false {
		t.Fatalf("wrong quota context: %v", quota)
	}
	call(t, claude, "session_end", map[string]any{"session_id": dID})
	peers := call(t, codex, "agents_discover", map[string]any{"client_type": "claude", "project_id": b})["agents"].([]any)
	if len(peers) != 1 || peers[0].(map[string]any)["state"] != "offline" {
		t.Fatalf("offline peer missing: %v", peers)
	}
	args := map[string]any{"session_id": cID, "to_agent_id": d["agent_id"], "to_project_id": b, "space_id": "global://default", "kind": "request", "body": "Is endpoint X ready?", "idempotency_key": "endpoint-X", "authorization_ref": "human-task-1"}
	message := validateOutputSchema(t, codex, "message_send", args)
	if retry := call(t, codex, "message_send", args); retry["id"] != message["id"] {
		t.Fatal("retry duplicated request")
	}
	d2 := registerFabric(t, claude, "claude", "sdk", "D2")
	if d2["agent_id"] != d["agent_id"] || d2["id"] == dID {
		t.Fatalf("restart identity lost: %v", d2)
	}
	d2ID := d2["id"].(string)
	boot = validateOutputSchema(t, claude, "context_bootstrap", map[string]any{"session_id": d2ID})
	requests := boot["pending_requests"].([]any)
	if len(requests) != 1 || requests[0].(map[string]any)["id"] != message["id"] {
		t.Fatalf("pending lost: %v", requests)
	}
	accepted := validateOutputSchema(t, claude, "request_accept", map[string]any{"session_id": d2ID, "message_id": message["id"]})
	call(t, claude, "message_ack", map[string]any{"session_id": d2ID, "message_id": message["id"]})
	if pending := call(t, claude, "requests_list", map[string]any{"session_id": d2ID})["messages"].([]any); len(pending) != 1 {
		t.Fatal("ACK removed unfinished work")
	}
	complete := map[string]any{"session_id": d2ID, "message_id": message["id"], "acceptance_token": accepted["acceptance_token"], "result": "Endpoint X passes its contract tests."}
	validateOutputSchema(t, claude, "request_complete", complete)
	call(t, claude, "request_complete", complete)
	replies := call(t, codex, "inbox_read", map[string]any{"session_id": cID})["messages"].([]any)
	if len(replies) != 1 || replies[0].(map[string]any)["thread_id"] != message["thread_id"] {
		t.Fatalf("reply missing or duplicated: %v", replies)
	}
	call(t, claude, "session_end", map[string]any{"session_id": d2ID})
	d3 := registerFabric(t, claude, "claude", "sdk", "D3")
	if inbox := call(t, claude, "inbox_read", map[string]any{"session_id": d3["id"]})["messages"].([]any); len(inbox) != 0 {
		t.Fatalf("ACK lost after restart: %v", inbox)
	}
}

func TestFabricVisibilityAmbiguityAndBoundedBootstrap(t *testing.T) {
	const a = "11111111-1111-4111-8111-111111111111"
	const b = "22222222-2222-4222-8222-222222222222"
	const c = "33333333-3333-4333-8333-333333333333"
	s, clients := fabricClients(t, a, b, c)
	first := registerFabric(t, clients[0], "codex", "duplicate", "one")
	second := registerFabric(t, clients[1], "claude", "duplicate", "two")
	registerFabric(t, clients[2], "claude", "private", "three")
	if err := s.SetGlobalAccess(c, false); err != nil {
		t.Fatal(err)
	}
	resolved := validateOutputSchema(t, clients[0], "project_resolve", map[string]any{"name": "duplicate"})
	if resolved["ambiguous"] != true || len(resolved["projects"].([]any)) != 2 {
		t.Fatalf("ambiguity hidden: %v", resolved)
	}
	if peers := call(t, clients[2], "agents_discover", map[string]any{"project_id": a})["agents"].([]any); len(peers) != 0 {
		t.Fatalf("private project enumerated global peers: %v", peers)
	}
	private := call(t, clients[1], "memory_put", map[string]any{"text": "private-decision-sdk", "scope": "project", "durable": true})
	call(t, clients[1], "memory_put", map[string]any{"text": "published-decision-sdk", "scope": "global", "durable": true})
	memories := call(t, clients[0], "memory_search", map[string]any{"query": "decision-sdk"})["memories"].([]any)
	if len(memories) != 1 || memories[0].(map[string]any)["id"] == private["id"] {
		t.Fatalf("private memory leaked: %v", memories)
	}
	for i := 0; i < 12; i++ {
		call(t, clients[1], "message_send", map[string]any{"session_id": second["id"], "to_agent_id": first["agent_id"], "space_id": "global://default", "body": strings.Repeat("界", 5000)})
	}
	boot := validateOutputSchema(t, clients[0], "context_bootstrap", map[string]any{"session_id": first["id"]})
	if boot["unread_count"] != float64(12) || len(boot["unread"].([]any)) != 10 {
		t.Fatal("bootstrap collection is not bounded with total count")
	}
	encoded, _ := json.Marshal(boot)
	if len(encoded) > 24000 {
		t.Fatalf("bootstrap too large: %d bytes", len(encoded))
	}
	for _, msg := range boot["unread"].([]any) {
		if msg.(map[string]any)["truncated"] != true {
			t.Fatal("truncated body not labelled")
		}
	}
}
