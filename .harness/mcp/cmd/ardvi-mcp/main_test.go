package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ardvi/harness/mcp/internal/catalog"
	"github.com/ardvi/harness/mcp/internal/hub"
	"github.com/ardvi/harness/mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerTransport struct{ base http.RoundTripper }

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Ardvi-Project", "11111111-1111-4111-8111-111111111111")
	return h.base.RoundTrip(r)
}

func TestStreamableHTTP(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	server := hub.New(s, &catalog.Catalog{Version: 1})
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: headerTransport{http.DefaultTransport}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 17 {
		t.Fatalf("got %d tools", len(tools.Tools))
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "session_start", Arguments: map[string]any{"name": "test", "client": "codex"}})
	if err != nil || result.IsError {
		t.Fatalf("session_start failed: %v %#v", err, result)
	}
}

func TestRejectsHostAndOrigin(t *testing.T) {
	next := safeHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, tc := range []struct{ host, origin string }{{"evil.example", ""}, {"127.0.0.1:8765", "https://evil.example"}} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8765/mcp", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		next.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("accepted host=%q origin=%q", tc.host, tc.origin)
		}
	}
}
