package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	skills := make([]catalog.Entry, 101)
	for i := range skills {
		skills[i] = catalog.Entry{Name: fmt.Sprintf("skill-%03d", i), Source: "fixture", Entry: "SKILL.md"}
	}
	server := hub.New(s, &catalog.Catalog{Version: 1, Skills: skills, Revisions: map[string]string{"fixture": "abc123"}}, "test")
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
	if len(tools.Tools) != 18 {
		t.Fatalf("got %d tools", len(tools.Tools))
	}
	foundSkillsList := false
	for _, tool := range tools.Tools {
		if tool.Name == "skills_list" {
			foundSkillsList = true
			break
		}
	}
	if !foundSkillsList {
		t.Fatal("skills_list tool is missing")
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "skills_list", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("skills_list failed: %v %#v", err, result)
	}
	b, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		Skills []listedSkill `json:"skills"`
		Next   string        `json:"next_cursor"`
	}
	if err = json.Unmarshal(b, &first); err != nil || len(first.Skills) != 100 || first.Next == "" {
		t.Fatalf("bad first skills page: %v %#v", err, first)
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "skills_list", Arguments: map[string]any{"cursor": first.Next}})
	if err != nil || result.IsError {
		t.Fatalf("second skills_list failed: %v %#v", err, result)
	}
	b, _ = json.Marshal(result.StructuredContent)
	var second struct {
		Skills []listedSkill `json:"skills"`
		Next   string        `json:"next_cursor"`
	}
	if err = json.Unmarshal(b, &second); err != nil || len(second.Skills) != 1 || second.Next != "" {
		t.Fatalf("bad second skills page: %v %#v", err, second)
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "session_start", Arguments: map[string]any{"name": "test", "client": "codex"}})
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
