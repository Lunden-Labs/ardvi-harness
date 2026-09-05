package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// defaultMCPURL is the local Ardvi MCP endpoint used by hook and inbox
// commands unless overridden, so tests can point at an httptest server.
func defaultMCPURL() string {
	if v := os.Getenv("ARDVI_MCP_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8765/mcp"
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}
type rpcCallToolResult struct {
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
	Content           []struct {
		Text string `json:"text"`
	} `json:"content"`
}
type rpcResponse struct {
	Result *rpcCallToolResult `json:"result"`
	Error  *rpcError          `json:"error"`
}

// callTool invokes one MCP tool as a single stateless JSON-RPC request and
// returns its structured content. The server's stateless mode synthesizes an
// implicit initialize/initialized handshake for a bare tools/call request, so
// no session setup is needed beforehand — see ephemeralConnectOpts in the
// go-sdk streamable transport.
func callTool(ctx context.Context, url, project, name string, args any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Ardvi-Project", project)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %s", name, resp.Status)
	}
	var out rpcResponse
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", name, out.Error.Message)
	}
	if out.Result == nil {
		return nil, fmt.Errorf("%s: empty result", name)
	}
	if out.Result.IsError {
		texts := make([]string, len(out.Result.Content))
		for i, c := range out.Result.Content {
			texts[i] = c.Text
		}
		return nil, fmt.Errorf("%s: %s", name, strings.Join(texts, "; "))
	}
	return out.Result.StructuredContent, nil
}

var errNoProject = errors.New("no .ardvi/project.json found")
