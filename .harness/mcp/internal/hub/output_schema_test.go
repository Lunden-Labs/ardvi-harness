package hub_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// validateOutputSchema calls toolName and validates the CallToolResult's
// StructuredContent against the OutputSchema the same tool advertises via
// ListTools, exactly as a schema-checking MCP client (such as Claude Code)
// does before accepting a tool result. It returns the decoded structured
// content for callers that want to assert on individual fields.
//
// This is the check that was missing: mcp.AddTool's own server-side
// validation and a client library's independent schema check are two
// different code paths, and only reproducing the latter here catches a
// schema/output shape drift between them.
func validateOutputSchema(t *testing.T, session *mcp.ClientSession, toolName string, args map[string]any) map[string]any {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("%s: ListTools: %v", toolName, err)
	}
	var schemaAny any
	for _, tool := range tools.Tools {
		if tool.Name == toolName {
			schemaAny = tool.OutputSchema
			break
		}
	}
	if schemaAny == nil {
		t.Fatalf("%s: tool not found or has no output schema", toolName)
	}
	schemaBytes, err := json.Marshal(schemaAny)
	if err != nil {
		t.Fatalf("%s: marshal output schema: %v", toolName, err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("%s: unmarshal output schema: %v", toolName, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: resolve output schema: %v", toolName, err)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: toolName, Arguments: args})
	if err != nil {
		t.Fatalf("%s: call error: %v", toolName, err)
	}
	if result.IsError {
		encoded, _ := json.Marshal(result)
		t.Fatalf("%s: tool returned an error result: %s", toolName, encoded)
	}

	contentBytes, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("%s: marshal structured content: %v", toolName, err)
	}
	var instance any
	if err := json.Unmarshal(contentBytes, &instance); err != nil {
		t.Fatalf("%s: unmarshal structured content: %v", toolName, err)
	}
	if err := resolved.Validate(instance); err != nil {
		t.Fatalf("%s: structured content %s does not match its own output schema %s: %v", toolName, contentBytes, schemaBytes, err)
	}

	var out map[string]any
	_ = json.Unmarshal(contentBytes, &out)
	return out
}

// TestOutputSchemaMatchesStructuredContent covers every tool that can carry
// the unread piggyback (plus the two read-only tools next to them), both with
// and without a pending unread message, so a future output type that drifts
// from its own inferred schema (e.g. by reintroducing anonymous struct
// embedding) fails here instead of in a client's own validation.
func TestOutputSchemaMatchesStructuredContent(t *testing.T) {
	session := newSession(t)
	a := call(t, session, "session_start", map[string]any{"name": "a", "client": "codex"})
	aID := a["id"].(string)
	b := call(t, session, "session_start", map[string]any{"name": "b", "client": "codex"})
	bID := b["id"].(string)

	validateOutputSchema(t, session, "session_start", map[string]any{"name": "c", "client": "codex"})

	// message_send with no pending unread for the sender.
	validateOutputSchema(t, session, "message_send", map[string]any{"session_id": aID, "to": bID, "body": "first"})

	// Give a two unread messages so message_ack and claim_release still
	// carry a non-empty piggyback after acknowledging one of them.
	call(t, session, "message_send", map[string]any{"session_id": bID, "to": aID, "body": "hi1"})
	call(t, session, "message_send", map[string]any{"session_id": bID, "to": aID, "body": "hi2"})

	// message_send with a pending unread piggyback present.
	validateOutputSchema(t, session, "message_send", map[string]any{"session_id": aID, "to": bID, "body": "reply"})

	out := validateOutputSchema(t, session, "claim_acquire", map[string]any{"session_id": aID, "resource": "file.go"})
	unread, _ := out["unread"].([]any)
	if len(unread) < 2 {
		t.Fatalf("claim_acquire: expected 2 unread messages in the piggyback, got %#v", out)
	}
	msgID := unread[0].(map[string]any)["id"].(string)

	// message_ack with a leftover unread message still present.
	validateOutputSchema(t, session, "message_ack", map[string]any{"session_id": aID, "message_id": msgID})

	// claim_release with the same leftover unread message.
	validateOutputSchema(t, session, "claim_release", map[string]any{"session_id": aID, "resource": "file.go"})

	// message_ack with no unread left, so the piggyback is entirely absent.
	call(t, session, "claim_acquire", map[string]any{"session_id": aID, "resource": "file2.go"})
	remaining := call(t, session, "inbox_read", map[string]any{"session_id": aID})
	msgs, _ := remaining["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 remaining unread message, got %#v", remaining)
	}
	lastID := msgs[0].(map[string]any)["id"].(string)
	validateOutputSchema(t, session, "message_ack", map[string]any{"session_id": aID, "message_id": lastID})
	validateOutputSchema(t, session, "claim_release", map[string]any{"session_id": aID, "resource": "file2.go"})

	validateOutputSchema(t, session, "inbox_read", map[string]any{"session_id": bID})
}
