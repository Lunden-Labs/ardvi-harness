package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

const codexBridgeProvenance = "[Ardvi MCP notification — delivered by ardvi codex-bridge, not typed by the user]"

type codexBridgeOptions struct {
	session  string
	project  string
	thread   string
	socket   string
	url      string
	text     string
	interval time.Duration
	once     bool
}

type codexRPCResponse struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type codexCallError struct {
	method  string
	message string
	data    json.RawMessage
}

func (e *codexCallError) Error() string { return e.method + ": " + e.message }

type codexRPCClient struct {
	conn   *websocket.Conn
	nextID int
}

type bridgePID struct {
	file   *os.File
	closed bool
}

func runCodexBridge(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("codex-bridge", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var options codexBridgeOptions
	f.StringVar(&options.session, "session", "", "Ardvi session id")
	f.StringVar(&options.project, "project", "", "project UUID")
	f.StringVar(&options.thread, "thread", "", "native Codex thread id")
	f.StringVar(&options.socket, "socket", "", "Codex app-server Unix socket")
	f.DurationVar(&options.interval, "interval", 20*time.Second, "poll interval")
	f.StringVar(&options.url, "url", defaultMCPURL(), "Ardvi MCP URL")
	f.BoolVar(&options.once, "once", false, "attempt one delivery and exit")
	f.StringVar(&options.text, "text", "", "deliver text instead of polling Ardvi")
	if err := f.Parse(args); err != nil {
		return err
	}
	if options.session == "" || options.project == "" || options.thread == "" {
		return errors.New("--session, --project, and --thread are required")
	}
	if options.interval <= 0 {
		return errors.New("--interval must be greater than zero")
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	key := mappingKey("codex", options.thread, options.project)
	pid, acquired, err := acquireBridgePID(dir, key)
	if err != nil || !acquired {
		return err
	}
	defer pid.close()
	if options.text != "" {
		options.once = true
	}
	if options.once {
		socket, err := resolveCodexSocket(ctx, options.socket)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		err = codexBridgePoll(ctx, options, dir, key, socket)
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	backoff := time.Second
	var missingSince time.Time
	for {
		socket, resolveErr := resolveCodexSocket(ctx, options.socket)
		if resolveErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			if missingSince.IsZero() {
				missingSince = time.Now()
			}
			fmt.Fprintf(os.Stderr, "ardvi codex-bridge: %v; retrying in %s\n", resolveErr, backoff)
			if time.Since(missingSince) >= 5*time.Minute {
				return nil
			}
			if !waitBridge(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		missingSince = time.Time{}
		if err = codexBridgePoll(ctx, options, dir, key, socket); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "ardvi codex-bridge: %v; reconnecting in %s\n", err, backoff)
			if !waitBridge(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		if !waitBridge(ctx, options.interval) {
			return nil
		}
	}
}

func codexBridgePoll(ctx context.Context, options codexBridgeOptions, dir, key, socket string) error {
	if options.text != "" {
		text := options.text
		if !strings.HasPrefix(text, codexBridgeProvenance) {
			text = codexBridgeProvenance + "\n" + text
		}
		_, err := codexDeliver(ctx, socket, options.thread, text)
		return err
	}
	session, mapping := codexBridgeSession(dir, key, options)
	seenPath := filepath.Join(dir, "inbox-"+session+".json")
	var legacy []string
	if mapping != nil {
		legacy = mapping.SeenIDs
	}
	err := withSeen(seenPath, legacy, func(seen map[string]bool) ([]string, error) {
		fetchCtx, cancel := context.WithTimeout(ctx, hookHTTPTimeout)
		messages, err := fetchInbox(fetchCtx, options.url, options.project, session)
		cancel()
		if err != nil {
			return nil, err
		}
		text, newIDs := formatNewMessages(session, messages, seen)
		if len(newIDs) == 0 {
			return nil, nil
		}
		delivered, err := codexDeliver(ctx, socket, options.thread,
			codexBridgeProvenance+"\n"+text)
		if err != nil || !delivered {
			var callErr *codexCallError
			if errors.As(err, &callErr) && (bytes.Contains(callErr.data, []byte("activeTurnNotSteerable")) || strings.Contains(callErr.message, "activeTurnNotSteerable")) {
				fmt.Fprintf(os.Stderr, "ardvi codex-bridge: %v; retrying next poll\n", err)
				return nil, nil
			}
			return nil, err
		}
		return newIDs, nil
	})
	if errors.Is(err, errSeenBusy) {
		return nil
	}
	return err
}

// codexBridgeSession follows a reconciled Ardvi session only when the mapping
// proves it belongs to this bridge's native Codex thread and Project. Manual
// --once/--text calls keep their explicit session argument.
func codexBridgeSession(dir, key string, options codexBridgeOptions) (string, *hookMapping) {
	mapping, ok := loadMapping(filepath.Join(dir, key+".json"))
	if !ok || !matchingNativeMapping(mapping, "codex", options.project, options.thread) || mapping.ArdviSessionID == "" {
		return options.session, nil
	}
	if !mapping.Stable || options.once {
		if mapping.ArdviSessionID == options.session {
			return options.session, &mapping
		}
		return options.session, nil
	}
	return mapping.ArdviSessionID, &mapping
}

func resolveCodexSocket(ctx context.Context, override string) (string, error) {
	socket := override
	if socket == "" {
		commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		output, err := exec.CommandContext(commandCtx, "codex", "app-server", "daemon", "version").Output()
		if err != nil {
			return "", fmt.Errorf("discover Codex socket: %w", err)
		}
		var status struct {
			SocketPath string `json:"socketPath"`
		}
		if err = json.Unmarshal(output, &status); err != nil || status.SocketPath == "" {
			return "", errors.New("discover Codex socket: daemon did not return socketPath")
		}
		socket = status.SocketPath
	}
	info, err := os.Stat(socket)
	if err != nil {
		return "", fmt.Errorf("Codex socket %s: %w", socket, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return "", fmt.Errorf("Codex socket %s is not a Unix socket", socket)
	}
	return socket, nil
}

func waitBridge(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func acquireBridgePID(dir, key string) (*bridgePID, bool, error) {
	path := filepath.Join(dir, "bridge-"+key+".pid")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err = file.Truncate(0); err == nil {
		_, err = fmt.Fprintf(file, "%d\n", os.Getpid())
	}
	if err != nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		return nil, false, err
	}
	return &bridgePID{file: file}, true, nil
}

func (p *bridgePID) close() {
	if p == nil || p.closed {
		return
	}
	p.closed = true
	_ = p.file.Truncate(0)
	_ = syscall.Flock(int(p.file.Fd()), syscall.LOCK_UN)
	_ = p.file.Close()
}

func codexDeliver(ctx context.Context, socket, thread, text string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		return false, err
	}
	defer conn.CloseNow()
	client := &codexRPCClient{conn: conn}
	if err = client.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "ardvi-codex-bridge", "title": "Ardvi MCP bridge", "version": version},
		"capabilities": map[string]any{"experimentalApi": true, "requestAttestation": false},
	}, nil); err != nil {
		return false, err
	}
	if err = client.notify(ctx, "initialized", map[string]any{}); err != nil {
		return false, err
	}
	var read struct {
		Thread struct {
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
			CanAcceptDirectInput bool   `json:"canAcceptDirectInput"`
			ParentThreadID       string `json:"parentThreadId"`
		} `json:"thread"`
	}
	if err = client.call(ctx, "thread/read", map[string]any{"threadId": thread}, &read); err != nil {
		return false, err
	}
	if !read.Thread.CanAcceptDirectInput || read.Thread.Status.Type == "notLoaded" || read.Thread.ParentThreadID != "" {
		log.Printf("ardvi codex-bridge: skipped thread=%s status=%s canAcceptDirectInput=%t subagent=%t",
			thread, read.Thread.Status.Type, read.Thread.CanAcceptDirectInput, read.Thread.ParentThreadID != "")
		return false, nil
	}
	if err = client.call(ctx, "turn/start", map[string]any{
		"threadId":    thread,
		"input":       []any{map[string]any{"type": "text", "text": text}},
		"turnTrigger": "ardvi-inbox",
	}, nil); err != nil {
		return false, err
	}
	log.Printf("ardvi codex-bridge: delivered to thread=%s status=%s", thread, read.Thread.Status.Type)
	return true, nil
}

func (c *codexRPCClient) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID
	c.nextID++
	if err := c.write(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}
		var response codexRPCResponse
		if json.Unmarshal(data, &response) != nil || response.ID == nil || *response.ID != id || response.Method != "" || (len(response.Result) == 0 && response.Error == nil) {
			continue
		}
		if response.Error != nil {
			return &codexCallError{method: method, message: response.Error.Message, data: response.Error.Data}
		}
		if result != nil {
			if err = json.Unmarshal(response.Result, result); err != nil {
				return fmt.Errorf("%s: %w", method, err)
			}
		}
		return nil
	}
}

func (c *codexRPCClient) notify(ctx context.Context, method string, params any) error {
	return c.write(ctx, map[string]any{"method": method, "params": params})
}

func (c *codexRPCClient) write(ctx context.Context, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}
