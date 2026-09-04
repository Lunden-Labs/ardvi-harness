package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectInitUsesInstalledHarnessAndForwardsPrompt(t *testing.T) {
	dir := t.TempDir()
	harness := filepath.Join(dir, "harness")
	project := filepath.Join(dir, "project")
	bin := filepath.Join(dir, "bin")
	for _, path := range []string{filepath.Join(harness, ".harness"), project, bin} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(harness, ".harness", "harness.mk"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "make.log")
	makeScript := "#!/bin/sh\nprintf '%s|%s\\n' \"$*\" \"$PROMPT\" >> \"$MAKE_LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "make"), []byte(makeScript), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAKE_LOG", log)
	if err := projectInit([]string{"--path", project, "--harness", harness, "--prompt", "Build API"}); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "copy TARGET="+project) || !strings.Contains(text, "harness-init|Build API") {
		t.Fatalf("unexpected make calls: %s", text)
	}
}
