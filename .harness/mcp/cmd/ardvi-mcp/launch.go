package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Replace Ardvi with the native client, preserving its terminal, signals and exit status.
func launchNativeClient(client string, args []string) error {
	binary, err := exec.LookPath(client)
	if err != nil {
		return fmt.Errorf("install %s and make it available on PATH: %w", client, err)
	}
	help := len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "--version" || args[0] == "-V")
	if client == "opencode" && !help {
		key, nativeArgs, err := takeAgentKey(args)
		if err != nil {
			return err
		}
		if key != "" {
			return launchBoundOpenCode(binary, key, nativeArgs)
		}
		args = nativeArgs
	}
	if client == "codex" && !help {
		for _, arg := range args {
			if arg == "--" {
				break
			}
			if arg == "--remote" || strings.HasPrefix(arg, "--remote=") {
				return fmt.Errorf("ardvi codex selects the local daemon; use codex directly for a different --remote")
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Codex owns daemon startup and serializes concurrent starts; this is idempotent.
		start := exec.CommandContext(ctx, binary, "app-server", "daemon", "start")
		start.Stderr = os.Stderr
		if err := start.Run(); err != nil {
			return fmt.Errorf("start Codex daemon: %w", err)
		}
		socket, err := resolveCodexSocket(ctx, "")
		if err != nil {
			return err
		}
		args = append([]string{"--dangerously-bypass-approvals-and-sandbox", "--remote", "unix://" + socket}, args...)
	}
	return syscall.Exec(binary, append([]string{client}, args...), os.Environ())
}

func takeAgentKey(args []string) (string, []string, error) {
	var key string
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--agent-key" {
			if key != "" || i+1 == len(args) {
				return "", nil, errors.New("--agent-key requires one value")
			}
			key, i = args[i+1], i+1
			continue
		}
		if value, ok := strings.CutPrefix(args[i], "--agent-key="); ok {
			if key != "" || value == "" {
				return "", nil, errors.New("--agent-key requires one value")
			}
			key = value
			continue
		}
		out = append(out, args[i])
	}
	if key != "" && !nativeKey.MatchString(key) {
		return "", nil, errors.New("invalid --agent-key")
	}
	return key, out, nil
}
