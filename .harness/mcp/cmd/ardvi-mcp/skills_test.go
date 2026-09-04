package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintSkillsGroupsSourcesAndRevisions(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	err = printSkills(listedSkills{
		Skills:    []listedSkill{{Name: "writing", Source: "writing-skills"}, {Name: "communication", Source: "harness"}},
		Revisions: map[string]string{"writing-skills": "abc123"},
	}, false)
	os.Stdout = original
	write.Close()
	output, readErr := io.ReadAll(read)
	read.Close()
	if err != nil || readErr != nil {
		t.Fatalf("print failed: %v %v", err, readErr)
	}
	for _, want := range []string{"harness (built-in)", "communication", "writing-skills (abc123)", "writing"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q in %q", want, output)
		}
	}
}
