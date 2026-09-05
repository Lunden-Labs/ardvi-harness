package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// inboxCommand implements `ardvi inbox --session <id> [--project <uuid>]
// [--follow] [--interval 20s]`, the standalone poller a persistent Monitor
// can run against a registered session.
func inboxCommand(args []string) error {
	return runInboxLoop(os.Stdout, args, time.Sleep)
}

func runInboxLoop(out io.Writer, args []string, sleep func(time.Duration)) error {
	f := flag.NewFlagSet("inbox", flag.ContinueOnError)
	session := f.String("session", "", "Ardvi session id")
	project := f.String("project", "", "project UUID (default: walk up from cwd for .ardvi/project.json)")
	follow := f.Bool("follow", false, "keep printing new messages as they arrive")
	interval := f.Duration("interval", 20*time.Second, "poll interval for --follow")
	url := f.String("url", defaultMCPURL(), "Ardvi MCP URL")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *session == "" {
		return errors.New("--session is required")
	}
	projectID := *project
	if projectID == "" {
		id, _, err := findProject("")
		if err != nil {
			return fmt.Errorf("--project not given and no .ardvi/project.json found: %w", err)
		}
		projectID = id
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	seenPath := filepath.Join(dir, "inbox-"+*session+".json")
	for {
		if err := inboxOnce(out, *url, projectID, *session, seenPath); err != nil {
			if !*follow {
				return err
			}
			fmt.Fprintln(os.Stderr, "ardvi inbox:", err)
		}
		if !*follow {
			return nil
		}
		sleep(*interval)
	}
}

func inboxOnce(out io.Writer, url, project, session, seenPath string) error {
	err := withSeen(seenPath, nil, func(seen map[string]bool) ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
		defer cancel()
		messages, err := fetchInbox(ctx, url, project, session)
		if err != nil {
			return nil, err
		}
		return printNewMessages(out, session, messages, seen)
	})
	if errors.Is(err, errSeenBusy) {
		return nil
	}
	return err
}

var errSeenBusy = errors.New("seen state busy")

func withSeen(path string, legacy []string, fn func(map[string]bool) ([]string, error)) error {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errSeenBusy
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ids, err := loadSeen(path)
	if err != nil {
		return err
	}
	seen := toSet(ids)
	changed := false
	for _, id := range legacy {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
			changed = true
		}
	}
	newIDs, err := fn(seen)
	if err != nil {
		return err
	}
	for _, id := range newIDs {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveSeen(path, ids)
}

type seenIDs struct {
	SeenIDs []string `json:"seen_ids,omitempty"`
}

func loadSeen(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s seenIDs
	if err = json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return s.SeenIDs, nil
}

func saveSeen(path string, ids []string) error {
	data, err := json.Marshal(seenIDs{SeenIDs: ids})
	if err != nil {
		return err
	}
	return writeAtomic(path, string(data))
}
