package main

import (
	"context"
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
		start.Stdout, start.Stderr = os.Stdout, os.Stderr
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
