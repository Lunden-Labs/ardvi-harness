package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const hookHTTPTimeout = 5 * time.Second

// hookStdin is the JSON object Claude Code and Codex pipe into hook
// commands on stdin. Every field is optional: a client may omit any of them.
type hookStdin struct {
	SessionID     string `json:"session_id"`
	Cwd           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`
	Prompt        string `json:"prompt"`
}

// hookMapping is the per-(client, client session, project) file that ties a
// native Codex/Claude session to its registered Ardvi session.
type hookMapping struct {
	ArdviSessionID string   `json:"ardvi_session_id"`
	Name           string   `json:"name"`
	ProjectID      string   `json:"project_id"`
	Client         string   `json:"client"`
	SeenIDs        []string `json:"seen_ids,omitempty"`
}

type hookMessage struct {
	ID          string    `json:"id"`
	From        string    `json:"from"`
	Scope       string    `json:"scope"`
	Thread      string    `json:"thread_id"`
	Body        string    `json:"body"`
	AckRequired bool      `json:"ack_required"`
	Created     time.Time `json:"created"`
}

// hookCommand is the ardvi hook <event> entry point. Per contract it never
// fails the client's prompt: every error is reported on stderr and the
// command still exits 0.
func hookCommand(args []string) error {
	if err := runHook(args); err != nil {
		fmt.Fprintln(os.Stderr, "ardvi hook:", err)
	}
	return nil
}

func runHook(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ardvi hook session-start|prompt|session-end --client claude|codex")
	}
	event := args[0]
	f := flag.NewFlagSet("hook "+event, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	client := f.String("client", "", "claude|codex")
	url := f.String("url", defaultMCPURL(), "Ardvi MCP URL")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if *client != "claude" && *client != "codex" {
		return errors.New("--client must be claude or codex")
	}
	var in hookStdin
	if data, err := io.ReadAll(os.Stdin); err == nil {
		_ = json.Unmarshal(data, &in) // tolerate missing/malformed stdin
	}
	switch event {
	case "session-start":
		return hookSessionStart(os.Stdout, *client, *url, in)
	case "prompt":
		return hookPrompt(os.Stdout, *client, *url, in)
	case "session-end":
		return hookSessionEnd(*client, *url, in)
	default:
		return fmt.Errorf("unknown hook event %q", event)
	}
}

func mappingKey(client, clientSessionID, projectID string) string {
	sum := sha256.Sum256([]byte(client + "|" + clientSessionID + "|" + projectID))
	return hex.EncodeToString(sum[:])[:24]
}

// ardviStateDir is where hook and inbox commands persist mapping and
// seen-message state, matching XDG_STATE_HOME convention.
func ardviStateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "ardvi", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// findProject walks up from cwd (or the process cwd when empty) looking for
// .ardvi/project.json.
func findProject(cwd string) (id, name string, err error) {
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return "", "", err
		}
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	for {
		data, readErr := os.ReadFile(filepath.Join(dir, ".ardvi", "project.json"))
		if readErr == nil {
			var p struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &p) == nil && p.ID != "" {
				return p.ID, p.Name, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", errNoProject
		}
		dir = parent
	}
}

func loadMapping(path string) (hookMapping, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hookMapping{}, false
	}
	var m hookMapping
	if json.Unmarshal(data, &m) != nil || m.ArdviSessionID == "" {
		return hookMapping{}, false
	}
	return m, true
}

func saveMapping(path string, m hookMapping) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeAtomic(path, string(data))
}

// uniqueRunningName appends -2, -3, … to base until it does not collide with
// a currently running session's name in this project.
func uniqueRunningName(ctx context.Context, url, project, base string) (string, error) {
	raw, err := callTool(ctx, url, project, "agents_list", map[string]any{"limit": 100})
	if err != nil {
		return "", err
	}
	var out struct {
		Sessions []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"sessions"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	running := make(map[string]bool, len(out.Sessions))
	for _, s := range out.Sessions {
		if s.State == "running" {
			running[s.Name] = true
		}
	}
	if !running[base] {
		return base, nil
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !running[candidate] {
			return candidate, nil
		}
	}
}

// hookSessionStart registers this native session with the Ardvi MCP server,
// or on resume/compact reuses the existing mapping instead of registering
// again, then prints the identity paragraph and any unread messages.
func hookSessionStart(out io.Writer, client, url string, in hookStdin) error {
	projectID, projectName, err := findProject(in.Cwd)
	if err != nil {
		return nil // no project: nothing to register, and not an error worth reporting
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, mappingKey(client, in.SessionID, projectID)+".json")
	ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
	defer cancel()
	mapping, ok := loadMapping(path)
	if !ok {
		name := os.Getenv("ARDVI_SESSION_NAME")
		if name == "" {
			name = client + "-" + projectName
		}
		if name, err = uniqueRunningName(ctx, url, projectID, name); err != nil {
			return err
		}
		raw, err := callTool(ctx, url, projectID, "session_start", map[string]any{"name": name, "client": client})
		if err != nil {
			return err
		}
		var session struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(raw, &session); err != nil {
			return err
		}
		mapping = hookMapping{ArdviSessionID: session.ID, Name: name, ProjectID: projectID, Client: client}
		if err = saveMapping(path, mapping); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Ardvi MCP: this session is registered as name=%s session_id=%s (project %s). Use this session_id for message_send, message_ack, claim_* and session_end; do not call session_start again.\n",
		mapping.Name, mapping.ArdviSessionID, mapping.ProjectID)
	return printUnread(ctx, out, url, path, &mapping)
}

// hookPrompt prints unseen messages for the mapped session, registering
// first (as hookSessionStart) if no mapping exists yet.
func hookPrompt(out io.Writer, client, url string, in hookStdin) error {
	projectID, _, err := findProject(in.Cwd)
	if err != nil {
		return nil
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, mappingKey(client, in.SessionID, projectID)+".json")
	mapping, ok := loadMapping(path)
	if !ok {
		return hookSessionStart(out, client, url, in)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
	defer cancel()
	return printUnread(ctx, out, url, path, &mapping)
}

// hookSessionEnd ends the mapped Ardvi session and removes the mapping file
// regardless of whether the server call succeeds, so a dead server never
// leaves a stale mapping that would block re-registration on the next start.
func hookSessionEnd(client, url string, in hookStdin) error {
	projectID, _, err := findProject(in.Cwd)
	if err != nil {
		return nil
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, mappingKey(client, in.SessionID, projectID)+".json")
	mapping, ok := loadMapping(path)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
	defer cancel()
	_, callErr := callTool(ctx, url, mapping.ProjectID, "session_end", map[string]any{"session_id": mapping.ArdviSessionID})
	_ = os.Remove(path)
	return callErr
}

func fetchInbox(ctx context.Context, url, project, sessionID string) ([]hookMessage, error) {
	raw, err := callTool(ctx, url, project, "inbox_read", map[string]any{"session_id": sessionID, "limit": 100})
	if err != nil {
		return nil, err
	}
	var out struct {
		Messages []hookMessage `json:"messages"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}

func toSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// printNewMessages writes every message in messages not present in seen,
// oldest first, and returns the ids that were newly printed. Shared by hook
// prompt and the standalone ardvi inbox command so both use the same format.
func printNewMessages(w io.Writer, sessionID string, messages []hookMessage, seen map[string]bool) []string {
	var newIDs []string
	for i := len(messages) - 1; i >= 0; i-- { // inbox_read is newest-first; print oldest-first
		m := messages[i]
		if seen[m.ID] {
			continue
		}
		fmt.Fprintf(w, "Ardvi MCP message id=%s from=%s scope=%s thread=%s created=%s ack_required=%t\n%s\n\n",
			m.ID, m.From, m.Scope, m.Thread, m.Created.Format(time.RFC3339), m.AckRequired, m.Body)
		newIDs = append(newIDs, m.ID)
	}
	if len(newIDs) > 0 {
		fmt.Fprintf(w, "Acknowledge with message_ack(session_id=%s, message_id=…) for each message above (ids: %s) and reply in the same thread when a reply is expected.\n",
			sessionID, strings.Join(newIDs, ", "))
	}
	return newIDs
}

func printUnread(ctx context.Context, out io.Writer, url, path string, mapping *hookMapping) error {
	messages, err := fetchInbox(ctx, url, mapping.ProjectID, mapping.ArdviSessionID)
	if err != nil {
		return err
	}
	newIDs := printNewMessages(out, mapping.ArdviSessionID, messages, toSet(mapping.SeenIDs))
	if len(newIDs) == 0 {
		return nil
	}
	mapping.SeenIDs = append(mapping.SeenIDs, newIDs...)
	return saveMapping(path, *mapping)
}
