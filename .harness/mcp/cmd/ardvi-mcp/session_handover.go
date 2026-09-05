package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// The caller holds the stable-identity lock. Only an explicitly configured
// native SessionStart may replace another conversation, even with a shared PID.
// Server session_end preserves stable inboxes and fences old work ownership.
func endPreviousCodexSessions(url, dir, project, machine, agentKey, current string) error {
	// ponytail: scan host mappings linearly; index by identity if startup latency grows.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		old, ok := loadMapping(path)
		if !ok || (!old.Stable && !old.EndPending) || old.Client != "codex" ||
			old.ProjectID != project || old.MachineID != machine || old.AgentKey != agentKey ||
			old.NativeSessionID == "" || old.NativeSessionID == current ||
			entry.Name() != mappingKey("codex", old.NativeSessionID, project)+".json" {
			continue
		}
		if err := withMappingLock(path, func() error {
			latest, exists := loadMapping(path)
			if !exists || latest.ArdviSessionID != old.ArdviSessionID {
				return nil
			}
			return finishCodexHandover(url, dir, path, latest)
		}); err != nil {
			return err
		}
	}
	return nil
}

// Both identity and mapping locks are held. Persist the fence BEFORE the RPC:
// a crash or lost response must not let an old prompt revive registration.
// EndPending retains the session ID for an idempotent retry on the next start
// or late SessionEnd. Completed tombstones remain until explicit native resume.
func finishCodexHandover(url, dir, path string, mapping hookMapping) error {
	if !mapping.EndPending {
		mapping.Stable, mapping.Superseded, mapping.EndPending = false, true, true
		if err := saveMapping(path, mapping); err != nil {
			return err
		}
	}
	stopCodexBridge(dir, mapping)
	ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
	defer cancel()
	if _, err := callTool(ctx, url, mapping.ProjectID, "session_end", map[string]any{"session_id": mapping.ArdviSessionID}); err != nil {
		return fmt.Errorf("end previous Codex session (handover pending; retry SessionStart): %w", err)
	}
	mapping.EndPending = false
	return saveMapping(path, mapping)
}
