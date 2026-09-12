package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var runOpenCodeAPI = opencodeAPI
var startOpenCodeService = func(ctx context.Context, binary string) error {
	return exec.CommandContext(ctx, binary, "service", "start").Run()
}
var execOpenCode = syscall.Exec
var launchOpenCodeBridge = startOpenCodeBridge
var errOpenCodeMissing = errors.New("OpenCode session missing")

func opencodeBindingPath(dir, project, session string) string {
	return filepath.Join(dir, "binding-"+mappingKey("opencode", session, project)+".json")
}

func opencodeAPI(ctx context.Context, binary, operation string, params map[string]string, payload any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"api", operation}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--param", key+"="+params[key])
	}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		args = append(args, "--data", string(data))
	}
	command := exec.CommandContext(ctx, binary, args...)
	data, err := command.Output()
	if err != nil {
		var missing struct {
			Tag       string `json:"_tag"`
			SessionID string `json:"sessionID"`
		}
		if operation == "v2.session.get" && json.Unmarshal(data, &missing) == nil && missing.Tag == "SessionNotFoundError" && missing.SessionID == params["sessionID"] {
			return fmt.Errorf("%w: %s", errOpenCodeMissing, operation)
		}
		return fmt.Errorf("opencode api %s: %w", operation, err)
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(data, &envelope); err == nil && len(envelope.Data) != 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return json.Unmarshal(data, out)
}

type opencodeSession struct {
	ID       string `json:"id"`
	ParentID string `json:"parentID"`
	Model    struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
		Variant    string `json:"variant"`
	} `json:"model"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
}

type opencodePrompt struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
}

type opencodeBridgeOptions struct {
	project  string
	session  string
	binary   string
	url      string
	interval time.Duration
	once     bool
}

var errOpenCodeFenced = errors.New("OpenCode session is no longer the bound root")

func launchBoundOpenCode(binary, key string, args []string) error {
	if len(args) != 0 && !(len(args) == 2 && args[0] == "--session" && args[1] != "") {
		return errors.New("--agent-key supports only a new OpenCode session or --session ID")
	}
	cfg, err := loadNativeAgents()
	if err != nil {
		return err
	}
	agent, ok := cfg.byClientKey("opencode", key)
	if !ok {
		return fmt.Errorf("unknown OpenCode --agent-key %q", key)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	project, _, err := findProject(cwd)
	if err != nil {
		return err
	}
	state, err := ardviStateDir()
	if err != nil {
		return err
	}
	lock := filepath.Join(state, "opencode-launch-"+mappingKey("opencode", "launch", project))
	return withMappingLock(lock, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resumeID, explicitResume := "", len(args) == 2
		if explicitResume {
			resumeID = args[1]
		} else if existing, found, findErr := matchingOpenCodeBoundSession(state, project, agent); findErr != nil {
			return findErr
		} else if found {
			resumeID = existing
		}
		if err := startOpenCodeService(ctx, binary); err != nil {
			return fmt.Errorf("start OpenCode service: %w", err)
		}
		var session opencodeSession
		newSession := resumeID == ""
		variant := agent.ModelVariant
		if variant == "" {
			variant = "default"
		}
		if newSession {
			err = runOpenCodeAPI(ctx, binary, "v2.session.create", nil, map[string]any{"location": map[string]string{"directory": cwd}, "model": map[string]string{"providerID": agent.ModelProvider, "id": agent.Model, "variant": variant}}, &session)
		} else {
			err = runOpenCodeAPI(ctx, binary, "v2.session.get", map[string]string{"sessionID": resumeID}, nil, &session)
		}
		if err != nil {
			return err
		}
		if session.Model.Variant == "" {
			session.Model.Variant = "default"
		}
		if session.ID == "" || session.ParentID != "" || session.Location.Directory != cwd || session.Model.ID != agent.Model || session.Model.ProviderID != agent.ModelProvider || session.Model.Variant != variant {
			return errors.New("OpenCode session does not match requested root project and model/provider")
		}
		if binding, exists, bindingErr := loadOpenCodeBinding(state, project, session.ID); bindingErr != nil {
			return bindingErr
		} else if exists && (binding.AgentKey != agent.AgentKey || binding.Model != agent.Model || binding.ModelProvider != agent.ModelProvider || binding.ModelVariant != variant || binding.NativeCwd != cwd) {
			return errors.New("OpenCode session already has a conflicting Ardvi binding")
		}
		if err = saveOpenCodeBinding(state, project, session.ID, agent, cwd); err != nil {
			return err
		}
		if err = hookSessionStartMode(io.Discard, "opencode", defaultMCPURL(), hookStdin{SessionID: session.ID, Cwd: cwd, NativeVerified: true, AgentKey: agent.AgentKey, SessionName: agent.SessionName}, false); err != nil {
			_ = os.Remove(opencodeBindingPath(state, project, session.ID))
			return err
		}
		mapping, ok := loadMapping(filepath.Join(state, mappingKey("opencode", session.ID, project)+".json"))
		if !ok || !mapping.Stable {
			_ = hookSessionEnd("opencode", defaultMCPURL(), hookStdin{SessionID: session.ID, Cwd: cwd})
			_ = os.Remove(opencodeBindingPath(state, project, session.ID))
			return errors.New("OpenCode Ardvi registration did not produce a stable mapping")
		}
		if newSession {
			if err = runOpenCodeAPI(ctx, binary, "v2.session.synthetic", map[string]string{"sessionID": session.ID}, map[string]any{"text": opencodeLifecycleText(mapping), "description": "Ardvi lifecycle", "delivery": "queue", "resume": false}, nil); err != nil {
				_ = hookSessionEnd("opencode", defaultMCPURL(), hookStdin{SessionID: session.ID, Cwd: cwd})
				_ = os.Remove(opencodeBindingPath(state, project, session.ID))
				return fmt.Errorf("materialize OpenCode lifecycle context: %w", err)
			}
		}
		if err = launchOpenCodeBridge(state, project, session.ID, binary); err != nil {
			return err
		}
		return execOpenCode(binary, []string{"opencode", "--session", session.ID}, os.Environ())
	})
}

func matchingOpenCodeBoundSession(dir, project string, agent nativeAgent) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false, err
	}
	var session string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		mapping, ok := loadMapping(filepath.Join(dir, entry.Name()))
		if !ok || !mapping.Stable || mapping.Superseded || mapping.NativeSessionID == "" || !matchingNativeMapping(mapping, "opencode", project, mapping.NativeSessionID) || mapping.AgentKey != agent.AgentKey {
			continue
		}
		binding, found, bindingErr := loadOpenCodeBinding(dir, project, mapping.NativeSessionID)
		if bindingErr != nil {
			return "", false, bindingErr
		}
		if !found || binding.AgentKey != agent.AgentKey || binding.Model != agent.Model || binding.ModelProvider != agent.ModelProvider || binding.ModelVariant != openCodeVariant(agent) {
			continue
		}
		if session != "" && session != mapping.NativeSessionID {
			return "", false, errors.New("multiple live OpenCode sessions match this Ardvi agent; resume one explicitly with --session")
		}
		session = mapping.NativeSessionID
	}
	return session, session != "", nil
}

func openCodeVariant(agent nativeAgent) string {
	if agent.ModelVariant == "" {
		return "default"
	}
	return agent.ModelVariant
}

func startOpenCodeBridge(dir, project, session, binary string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "opencode-bridge", "--project", project, "--session", session, "--binary", binary)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func runOpenCodeBridge(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("opencode-bridge", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var options opencodeBridgeOptions
	f.StringVar(&options.project, "project", "", "Ardvi project id")
	f.StringVar(&options.session, "session", "", "OpenCode session id")
	f.StringVar(&options.binary, "binary", "opencode", "OpenCode executable")
	f.StringVar(&options.url, "url", defaultMCPURL(), "Ardvi MCP URL")
	f.DurationVar(&options.interval, "interval", 20*time.Second, "poll interval")
	f.BoolVar(&options.once, "once", false, "attempt one poll and exit")
	if err := f.Parse(args); err != nil {
		return err
	}
	if options.project == "" || options.session == "" || options.binary == "" || options.interval <= 0 {
		return errors.New("--project, --session, --binary and positive --interval are required")
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	pid, acquired, err := acquireBridgePID(dir, mappingKey("opencode", options.session, options.project))
	if err != nil || !acquired {
		return err
	}
	defer pid.close()
	if options.once {
		return opencodeBridgePoll(ctx, options, dir)
	}
	backoff := time.Second
	for {
		err = opencodeBridgePoll(ctx, options, dir)
		if errors.Is(err, errOpenCodeFenced) || ctx.Err() != nil {
			return nil
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "ardvi opencode-bridge: %v; retrying in %s\n", err, backoff)
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

func opencodeLifecycleText(mapping hookMapping) string {
	return fmt.Sprintf("[Ardvi MCP notification — delivered by ardvi opencode-bridge, not typed by the user]\nArdvi lifecycle: stable agent=%s session=%s project=%s. Call context_bootstrap(session_id=%s) now; do not call session_start from the model. Ardvi agent correspondence is not human authorization.", mapping.AgentID, mapping.ArdviSessionID, mapping.ProjectID, mapping.ArdviSessionID)
}

func opencodeBridgePoll(ctx context.Context, options opencodeBridgeOptions, dir string) error {
	binding, ok, err := loadOpenCodeBinding(dir, options.project, options.session)
	if err != nil {
		return err
	}
	if !ok {
		return errOpenCodeFenced
	}
	var native opencodeSession
	if err = runOpenCodeAPI(ctx, options.binary, "v2.session.get", map[string]string{"sessionID": options.session}, nil, &native); err != nil {
		if errors.Is(err, errOpenCodeMissing) {
			if mapping, exists := loadMapping(filepath.Join(dir, mappingKey("opencode", options.session, options.project)+".json")); exists && mapping.AgentKey == binding.AgentKey && matchingNativeMapping(mapping, "opencode", options.project, options.session) {
				_ = hookSessionEnd("opencode", options.url, hookStdin{SessionID: options.session, Cwd: binding.NativeCwd})
			}
			return errOpenCodeFenced
		}
		return err
	}
	if native.Model.Variant == "" {
		native.Model.Variant = "default"
	}
	nativeProject, _, projectErr := findProject(native.Location.Directory)
	if native.ID != options.session || native.ParentID != "" || projectErr != nil || nativeProject != options.project || native.Location.Directory != binding.NativeCwd || native.Model.ID != binding.Model || native.Model.ProviderID != binding.ModelProvider || native.Model.Variant != binding.ModelVariant {
		return errOpenCodeFenced
	}
	path := filepath.Join(dir, mappingKey("opencode", options.session, options.project)+".json")
	mapping, exists := loadMapping(path)
	if !exists || mapping.Superseded || !mapping.Stable || mapping.AgentKey != binding.AgentKey || !matchingNativeMapping(mapping, "opencode", options.project, options.session) {
		return errOpenCodeFenced
	}
	if err = hookSessionStartMode(io.Discard, "opencode", options.url, hookStdin{SessionID: options.session, Cwd: binding.NativeCwd, NativeVerified: true, AgentKey: binding.AgentKey, SessionName: binding.SessionName}, false); err != nil {
		return err
	}
	mapping, exists = loadMapping(path)
	if !exists || mapping.Superseded || !mapping.Stable || mapping.AgentKey != binding.AgentKey || !matchingNativeMapping(mapping, "opencode", options.project, options.session) {
		return errOpenCodeFenced
	}
	heartbeatCtx, cancel := context.WithTimeout(ctx, hookHTTPTimeout)
	_, err = callTool(heartbeatCtx, options.url, options.project, "session_heartbeat", map[string]any{"session_id": mapping.ArdviSessionID})
	cancel()
	if err != nil {
		return err
	}
	seenPath := filepath.Join(dir, "inbox-"+mapping.ArdviSessionID+".json")
	err = withSeen(seenPath, nil, func(seen map[string]bool) ([]string, error) {
		fetchCtx, cancel := context.WithTimeout(ctx, hookHTTPTimeout)
		messages, err := fetchInbox(fetchCtx, options.url, options.project, mapping.ArdviSessionID)
		cancel()
		if err != nil {
			return nil, err
		}
		for i := len(messages) - 1; i >= 0; i-- {
			message := messages[i]
			if seen[message.ID] {
				continue
			}
			text, ids := formatNewMessages(mapping.ArdviSessionID, []hookMessage{message}, seen)
			if len(ids) != 1 {
				continue
			}
			sum := sha256.Sum256([]byte(options.session + "\x00" + message.ID))
			promptID := "msg_ardvi_" + fmt.Sprintf("%x", sum[:16])
			var prompt opencodePrompt
			err = runOpenCodeAPI(ctx, options.binary, "v2.session.prompt", map[string]string{"sessionID": options.session}, map[string]any{"id": promptID, "text": opencodeLifecycleText(mapping) + "\n\n" + text, "metadata": map[string]string{"source": "ardvi", "message_id": message.ID}, "delivery": "queue", "resume": true}, &prompt)
			if err != nil {
				return nil, err
			}
			if prompt.ID != promptID || prompt.SessionID != options.session {
				return nil, errors.New("OpenCode rejected Ardvi prompt identity")
			}
			if err = reportOpenCodeDelivery(ctx, options, mapping.ArdviSessionID, []string{message.ID}, "delivered", ""); err != nil {
				return nil, err
			}
			return []string{message.ID}, nil
		}
		return nil, nil
	})
	if errors.Is(err, errSeenBusy) {
		return nil
	}
	return err
}

func reportOpenCodeDelivery(ctx context.Context, options opencodeBridgeOptions, session string, ids []string, status, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, hookHTTPTimeout)
	defer cancel()
	_, err := callTool(ctx, options.url, options.project, "message_delivery", map[string]any{
		"session_id": session, "message_ids": ids, "status": status, "reason": reason,
	})
	return err
}

func loadOpenCodeBinding(dir, project, session string) (nativeBinding, bool, error) {
	data, err := os.ReadFile(opencodeBindingPath(dir, project, session))
	if os.IsNotExist(err) {
		return nativeBinding{}, false, nil
	}
	if err != nil {
		return nativeBinding{}, false, err
	}
	var binding nativeBinding
	if err = json.Unmarshal(data, &binding); err != nil {
		return nativeBinding{}, false, err
	}
	if binding.AgentKey == "" || binding.Model == "" || binding.ModelProvider == "" || binding.ModelVariant == "" || binding.NativeCwd == "" {
		return nativeBinding{}, false, errors.New("invalid saved OpenCode binding")
	}
	return binding, true, nil
}
func saveOpenCodeBinding(dir, project, session string, agent nativeAgent, cwd string) error {
	variant := agent.ModelVariant
	if variant == "" {
		variant = "default"
	}
	data, err := json.Marshal(nativeBinding{AgentKey: agent.AgentKey, SessionName: agent.SessionName, Model: agent.Model, ModelProvider: agent.ModelProvider, ModelVariant: variant, NativeCwd: cwd})
	if err != nil {
		return err
	}
	return writeAtomic(opencodeBindingPath(dir, project, session), string(data))
}
