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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	ArdviSessionID  string   `json:"ardvi_session_id"`
	Name            string   `json:"name"`
	ProjectID       string   `json:"project_id"`
	Client          string   `json:"client"`
	NativeSessionID string   `json:"native_session_id,omitempty"`
	SeenIDs         []string `json:"seen_ids,omitempty"`
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
		mapping = hookMapping{ArdviSessionID: session.ID, Name: name, ProjectID: projectID, Client: client, NativeSessionID: in.SessionID}
		if err = saveMapping(path, mapping); err != nil {
			return err
		}
	} else if mapping.NativeSessionID == "" {
		mapping.NativeSessionID = in.SessionID
		if err = saveMapping(path, mapping); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Ardvi MCP: this session is registered as name=%s session_id=%s (project %s). Use this session_id for message_send, message_ack, claim_* and session_end; do not call session_start again.\n",
		mapping.Name, mapping.ArdviSessionID, mapping.ProjectID)
	if client == "codex" {
		if err = startCodexBridge(ctx, url, path, mapping); err != nil {
			fmt.Fprintln(os.Stderr, "ardvi hook: start Codex bridge:", err)
		}
	}
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

func startCodexBridge(ctx context.Context, url, mappingPath string, mapping hookMapping) error {
	if os.Getenv("ARDVI_CODEX_BRIDGE_DISABLE") == "1" {
		return nil
	}
	if _, err := resolveCodexSocket(ctx, ""); err != nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	key := strings.TrimSuffix(filepath.Base(mappingPath), ".json")
	logFile, err := os.OpenFile(filepath.Join(filepath.Dir(mappingPath), "bridge-"+key+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	command := exec.Command(executable, "codex-bridge",
		"--session", mapping.ArdviSessionID,
		"--project", mapping.ProjectID,
		"--thread", mapping.NativeSessionID,
		"--url", url,
	)
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func stopCodexBridge(dir string, mapping hookMapping) {
	pidPath := filepath.Join(dir, "bridge-"+mappingKey("codex", mapping.NativeSessionID, mapping.ProjectID)+".pid")
	file, err := os.OpenFile(pidPath, os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return
	} else if !errors.Is(err, syscall.EWOULDBLOCK) {
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return
	}
	if !bridgeProcessMatches(pid, mapping) {
		fmt.Fprintf(os.Stderr, "ardvi hook: refusing to stop unmatched bridge pid %d\n", pid)
		return
	}
	if process, findErr := os.FindProcess(pid); findErr == nil {
		if signalErr := process.Signal(syscall.SIGTERM); signalErr != nil {
			fmt.Fprintln(os.Stderr, "ardvi hook: stop Codex bridge:", signalErr)
		}
	}
}

func bridgeProcessMatches(pid int, mapping hookMapping) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		data, err = exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err != nil {
			return false
		}
	}
	command := strings.ReplaceAll(string(data), "\x00", " ")
	return strings.Contains(command, " codex-bridge ") &&
		strings.Contains(command, " --session "+mapping.ArdviSessionID+" ") &&
		strings.Contains(command, " --project "+mapping.ProjectID+" ") &&
		strings.Contains(command, " --thread "+mapping.NativeSessionID)
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
	if client == "codex" {
		if mapping.NativeSessionID == "" {
			mapping.NativeSessionID = in.SessionID
		}
		stopCodexBridge(dir, mapping)
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
func printNewMessages(w io.Writer, sessionID string, messages []hookMessage, seen map[string]bool) ([]string, error) {
	text, newIDs := formatNewMessages(sessionID, messages, seen)
	_, err := io.WriteString(w, text)
	return newIDs, err
}

func formatNewMessages(sessionID string, messages []hookMessage, seen map[string]bool) (string, []string) {
	var out strings.Builder
	var newIDs []string
	for i := len(messages) - 1; i >= 0; i-- { // inbox_read is newest-first; print oldest-first
		m := messages[i]
		if seen[m.ID] {
			continue
		}
		fmt.Fprintf(&out, "Ardvi MCP message id=%s from=%s scope=%s thread=%s created=%s ack_required=%t\n%s\n\n",
			m.ID, m.From, m.Scope, m.Thread, m.Created.Format(time.RFC3339), m.AckRequired, m.Body)
		newIDs = append(newIDs, m.ID)
	}
	if len(newIDs) > 0 {
		fmt.Fprintf(&out, "Acknowledge with message_ack(session_id=%s, message_id=…) for each message above (ids: %s) and reply in the same thread when a reply is expected.\n",
			sessionID, strings.Join(newIDs, ", "))
	}
	return out.String(), newIDs
}

func printUnread(ctx context.Context, out io.Writer, url, path string, mapping *hookMapping) error {
	seenPath := filepath.Join(filepath.Dir(path), "inbox-"+mapping.ArdviSessionID+".json")
	err := withSeen(seenPath, mapping.SeenIDs, func(seen map[string]bool) ([]string, error) {
		messages, err := fetchInbox(ctx, url, mapping.ProjectID, mapping.ArdviSessionID)
		if err != nil {
			return nil, err
		}
		newIDs, err := printNewMessages(out, mapping.ArdviSessionID, messages, seen)
		if err != nil {
			return nil, err
		}
		if len(newIDs) > 0 {
			mapping.SeenIDs = append(mapping.SeenIDs, newIDs...)
			if err = saveMapping(path, *mapping); err != nil {
				return nil, err
			}
		}
		return newIDs, nil
	})
	if errors.Is(err, errSeenBusy) {
		return nil
	}
	return err
}
