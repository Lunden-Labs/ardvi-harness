package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

const watchInterval = 20 * time.Second

type nativeProcess struct {
	PID   int
	Birth string
}

var inspectNativeProcess = nativeProcessInfo
var discoverNativeProcess = nativeClientProcess
var launchLeaseKeeper = func(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

// machineIdentity is host state, never repository state. It contains only a
// random identifier and is locked so concurrent client starts share it.
func machineIdentity(dir string) (string, error) {
	path := filepath.Join(filepath.Dir(dir), "machine-id")
	read := func() (string, bool, error) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		id := strings.TrimSpace(string(data))
		if len(id) != 32 {
			return "", false, errors.New("invalid Ardvi machine identity")
		}
		if _, err = hex.DecodeString(id); err != nil {
			return "", false, errors.New("invalid Ardvi machine identity")
		}
		return id, true, nil
	}
	if id, ok, err := read(); err != nil || ok {
		return id, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if id, ok, err := read(); err != nil || ok {
		return id, err
	}
	buf := make([]byte, 16)
	if _, err = rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	if err = writeAtomic(path, id+"\n"); err != nil {
		return "", err
	}
	return id, nil
}

func gitSnapshot(cwd string) (branch, head string, dirty bool) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	run := func(args ...string) (string, bool) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	var status string
	var branchOK, headOK, statusOK bool
	branch, branchOK = run("rev-parse", "--abbrev-ref", "HEAD")
	head, headOK = run("rev-parse", "HEAD")
	status, statusOK = run("status", "--porcelain")
	if !branchOK || !headOK || !statusOK {
		return "unknown", "unknown", true // Never report an unreadable repository as clean.
	}
	dirty = status != ""
	return
}

func nativeProcessInfo(pid int) (nativeProcess, string, error) {
	if pid <= 1 {
		return nativeProcess{}, "", errors.New("invalid process id")
	}
	value := func(field string) (string, error) {
		out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", field+"=").Output()
		return strings.TrimSpace(string(out)), err
	}
	birth, err := value("lstart")
	if err != nil || birth == "" {
		return nativeProcess{}, "", errors.New("cannot read native process birth time")
	}
	command, err := value("comm")
	if err != nil || command == "" {
		return nativeProcess{}, "", errors.New("cannot read native process command")
	}
	return nativeProcess{PID: pid, Birth: birth}, filepath.Base(command), nil
}

func nativeClientProcess(client string) (nativeProcess, bool) {
	pid := os.Getppid()
	for range 8 { // Inspect only hook ancestors; never scan system processes.
		process, command, err := inspectNativeProcess(pid)
		if err != nil {
			return nativeProcess{}, false
		}
		if command == client {
			return process, true
		}
		out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
		if err != nil {
			return nativeProcess{}, false
		}
		next, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil || next <= 1 || next == pid {
			return nativeProcess{}, false
		}
		pid = next
	}
	return nativeProcess{}, false
}

func nativeProcessAlive(client string, mapping hookMapping) bool {
	if mapping.ClientPID <= 1 || mapping.ClientBirth == "" {
		return false
	}
	p, command, err := inspectNativeProcess(mapping.ClientPID)
	return err == nil && command == client && p.Birth == mapping.ClientBirth
}

func withMappingLock(path string, fn func() error) error {
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}

// startNativeLeaseKeeper creates a detached process for a verified native
// client. The child lock follows a replacement mapping for the same native
// identity; the Codex bridge and Claude watcher are never liveness evidence.
func startNativeLeaseKeeper(client, url, mappingPath string, in hookStdin) error {
	mapping, ok := loadMapping(mappingPath)
	if !ok || !mapping.Stable || !nativeProcessAlive(client, mapping) {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(hookStdin{SessionID: in.SessionID, Cwd: in.Cwd})
	if err != nil {
		return err
	}
	key := strings.TrimSuffix(filepath.Base(mappingPath), ".json")
	logFile, err := os.OpenFile(filepath.Join(filepath.Dir(mappingPath), "lease-"+key+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	command := exec.Command(executable, "hook", "lease", "--client", client, "--url", url)
	command.Stdin = strings.NewReader(string(payload))
	command.Stdout, command.Stderr = logFile, logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return launchLeaseKeeper(command)
}

func hookLease(client, url string, in hookStdin, sleep func(time.Duration)) error {
	project, _, err := findProject(in.Cwd)
	if err != nil {
		return nil
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, mappingKey(client, in.SessionID, project)+".json")
	lock, err := os.OpenFile(path+".lease.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	for {
		mapping, ok := loadMapping(path)
		if !ok || !matchingNativeMapping(mapping, client, project, in.SessionID) || !mapping.Stable || !nativeProcessAlive(client, mapping) {
			return nil
		}
		var renewErr error
		for attempt := 0; attempt < 3; attempt++ {
			// Check immediately before each renewal. A PID alone is never enough.
			if !nativeProcessAlive(client, mapping) {
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
			_, renewErr = callTool(ctx, url, mapping.ProjectID, "session_heartbeat", map[string]any{"session_id": mapping.ArdviSessionID})
			cancel()
			if renewErr == nil {
				break
			}
			if attempt < 2 {
				sleep(time.Second)
			}
		}
		if renewErr != nil {
			return renewErr
		}
		sleep(watchInterval)
	}
}

func matchingNativeMapping(mapping hookMapping, client, project, nativeSession string) bool {
	return mapping.Client == client && mapping.ProjectID == project && mapping.NativeSessionID == nativeSession
}

type hookWake struct{ text string }

func (e *hookWake) Error() string { return "new Ardvi inbox message" }

func hookWatch(out io.Writer, client, url string, in hookStdin, sleep func(time.Duration)) error {
	project, _, err := findProject(in.Cwd)
	if err != nil {
		return nil
	}
	dir, err := ardviStateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, mappingKey(client, in.SessionID, project)+".json")
	lock, err := os.OpenFile(path+".watch.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	for attempts := 0; attempts < 100; attempts++ { // Covers a 5s registration plus bounded Git snapshot.
		mapping, ok := loadMapping(path)
		if ok {
			if !mapping.Stable || !nativeProcessAlive(client, mapping) {
				return nil
			}
			for {
				current, exists := loadMapping(path)
				if !exists || !matchingNativeMapping(current, client, project, in.SessionID) || !current.Stable || !nativeProcessAlive(client, current) {
					return nil
				}
				mapping = current // Follow reconciliation within the same native conversation.
				var text string
				seenPath := filepath.Join(dir, "inbox-"+mapping.ArdviSessionID+".json")
				ctx, cancel := context.WithTimeout(context.Background(), hookHTTPTimeout)
				err = withSeen(seenPath, nil, func(seen map[string]bool) ([]string, error) {
					messages, readErr := fetchInbox(ctx, url, mapping.ProjectID, mapping.ArdviSessionID)
					if readErr != nil {
						return nil, readErr
					}
					var ids []string
					text, ids = formatNewMessages(mapping.ArdviSessionID, messages, seen)
					return ids, nil
				})
				cancel()
				if err != nil && !errors.Is(err, errSeenBusy) {
					return err
				}
				if text != "" {
					return &hookWake{text: text}
				}
				if !nativeProcessAlive(client, mapping) {
					return nil
				}
				sleep(watchInterval)
			}
		}
		sleep(100 * time.Millisecond)
	}
	return nil
}

func lifecycleReport(mapping hookMapping) string {
	if !mapping.Stable {
		return "Ardvi lifecycle degraded: the service did not return a stable native identity. Update Ardvi, then restart this client; do not rely on this session for stable delivery.\n"
	}
	if mapping.ClientPID <= 1 || mapping.ClientBirth == "" {
		return "Ardvi liveness: native client process could not be verified; hooks reconcile activity, but no detached heartbeat is running.\n"
	}
	return ""
}

type registrationError struct{ err error }

func (e *registrationError) Error() string {
	return fmt.Sprintf("native registration unavailable: %v", e.err)
}
func (e *registrationError) Unwrap() error    { return e.err }
func stableRegistrationError(err error) error { return &registrationError{err: err} }
