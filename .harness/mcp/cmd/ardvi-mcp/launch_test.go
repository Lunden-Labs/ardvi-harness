package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchNativeHelper(t *testing.T) {
	if os.Getenv("ARDVI_LAUNCH_TEST_HELPER") != "1" {
		return
	}
	if err := launchNativeClient(os.Args[3], os.Args[4:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestNativeClientLaunch(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, client, failure string
		args                  []string
		exit                  int
	}{
		{name: "codex", client: "codex", args: []string{"-m", "test model", "prompt with spaces"}, exit: 23},
		{name: "claude", client: "claude", args: []string{"--model", "test model", "prompt with spaces"}, exit: 23},
		{name: "daemon fails", client: "codex", failure: "start", exit: 1},
		{name: "socket missing", client: "codex", failure: "socket", exit: 1},
		{name: "remote override", client: "codex", args: []string{"--remote=unix:///other"}, exit: 1},
		{name: "help", client: "codex", args: []string{"--help"}, exit: 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			socket := filepath.Join(dir, "daemon.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			log := filepath.Join(dir, "calls.jsonl")
			script := `import json, os, sys
args = sys.argv[1:]
with open(os.environ['LAUNCH_CALLS'], 'a') as f:
    f.write(json.dumps({'args': args, 'cwd': os.getcwd()}) + '\n')
if args == ['app-server', 'daemon', 'start']:
    print('daemon internal status')
    sys.exit(1 if os.environ['LAUNCH_FAILURE'] == 'start' else 0)
if args == ['app-server', 'daemon', 'version']:
    print(json.dumps({'socketPath': os.environ['LAUNCH_SOCKET'] + ('.missing' if os.environ['LAUNCH_FAILURE'] == 'socket' else '')}))
    sys.exit(0)
print('native output')
sys.exit(23)
`
			if err := os.WriteFile(filepath.Join(dir, tc.client), []byte("#!"+python+"\n"+script), 0755); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], append([]string{"-test.run=^TestLaunchNativeHelper$", "--", tc.client}, tc.args...)...)
			command.Dir = dir
			command.Env = append(os.Environ(), "ARDVI_LAUNCH_TEST_HELPER=1", "PATH="+dir,
				"LAUNCH_CALLS="+log, "LAUNCH_SOCKET="+socket, "LAUNCH_FAILURE="+tc.failure)
			output, err := command.CombinedOutput()
			if err == nil || command.ProcessState.ExitCode() != tc.exit {
				t.Fatalf("exit=%v, output=%s", err, output)
			}
			data, _ := os.ReadFile(log)
			var calls []struct {
				Args []string `json:"args"`
				Cwd  string   `json:"cwd"`
			}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == "" {
					continue
				}
				var call struct {
					Args []string `json:"args"`
					Cwd  string   `json:"cwd"`
				}
				if err := json.Unmarshal([]byte(line), &call); err != nil {
					t.Fatal(err)
				}
				calls = append(calls, call)
			}
			switch tc.name {
			case "codex":
				want := append([]string{"--dangerously-bypass-approvals-and-sandbox", "--remote", "unix://" + socket}, tc.args...)
				if len(calls) != 3 || !reflect.DeepEqual(calls[2].Args, want) || calls[2].Cwd != dir {
					t.Fatalf("unexpected calls: %+v", calls)
				}
			case "claude", "help":
				if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, tc.args) || calls[0].Cwd != dir {
					t.Fatalf("unexpected calls: %+v", calls)
				}
			case "daemon fails":
				if len(calls) != 1 {
					t.Fatalf("continued after daemon failure: %+v", calls)
				}
			case "socket missing":
				if len(calls) != 2 {
					t.Fatalf("launched without socket: %+v", calls)
				}
			case "remote override":
				if len(calls) != 0 {
					t.Fatalf("accepted remote override: %+v", calls)
				}
			}
			if tc.exit == 23 && !strings.Contains(string(output), "native output") {
				t.Fatalf("native stdout lost: %s", output)
			}
			if strings.Contains(string(output), "daemon internal status") {
				t.Fatalf("daemon setup output leaked into native terminal: %s", output)
			}
		})
	}
}
