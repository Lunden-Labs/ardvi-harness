package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUpdateUsesInstalledBundleAndForwardsOptions(t *testing.T) {
	data := t.TempDir()
	t.Setenv("ARDVI_DATA_DIR", data)
	t.Setenv("ARDVI_HARNESS_DIR", "")
	scripts := filepath.Join(data, "harness", ".harness", "scripts")
	if err := os.MkdirAll(scripts, 0755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(data, "arguments.json")
	t.Setenv("UPDATE_TEST_OUTPUT", output)
	script := "import json, os, sys\nwith open(os.environ['UPDATE_TEST_OUTPUT'], 'w') as f: json.dump(sys.argv[1:], f)\nsys.exit(int(os.environ.get('UPDATE_TEST_FAIL', '0')))\n"
	if err := os.WriteFile(filepath.Join(scripts, "update_release.py"), []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--manifest", "manifest with spaces.json", "--replace-harness", "--no-start"}
	if err := updateRelease(args); err != nil {
		t.Fatal(err)
	}
	dataBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(dataBytes, &got); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	executable, _ = filepath.EvalSymlinks(executable)
	want := append([]string{"--bin-dir", filepath.Dir(executable)}, args...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	t.Setenv("UPDATE_TEST_FAIL", "1")
	if err := updateRelease(args); err == nil {
		t.Fatal("updater failure was swallowed")
	}
}
