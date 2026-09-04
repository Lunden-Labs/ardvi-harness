package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsolationClaimsPersistenceAndExport(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir); err == nil {
		other.Close()
		t.Fatal("second writer opened store")
	}
	a, _ := s.StartSession("project-a", "a", "codex", "")
	b, _ := s.StartSession("project-b", "b", "claude", "")
	if _, err = s.Send("project-a", "project", a.ID, "*", "", "private", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Inbox("project-b", b.ID, 10); len(got) != 0 {
		t.Fatalf("project message leaked: %#v", got)
	}
	if _, err = s.Send("project-a", "global", a.ID, "*", "", "shared", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Inbox("project-b", b.ID, 10); len(got) != 1 || got[0].Body != "shared" {
		t.Fatalf("global message missing: %#v", got)
	}
	directed, _ := s.Send("project-a", "project", a.ID, a.ID, "", "directed", true)
	if err := s.Ack("project-b", b.ID, directed.ID); err == nil {
		t.Fatal("unrelated session acknowledged directed message")
	}
	if err := s.Ack("project-a", a.ID, directed.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Inbox("project-a", a.ID, 10); len(got) > 0 && got[0].ID == directed.ID {
		t.Fatal("acknowledged message remained in inbox")
	}

	c, _ := s.StartSession("project-a", "c", "codex", "")
	winner := make(chan string, 2)
	wins := 0
	var race sync.WaitGroup
	for _, sid := range []string{a.ID, c.ID} {
		race.Add(1)
		go func(owner string) {
			defer race.Done()
			if _, e := s.Acquire("project-a", owner, "src/api", time.Minute); e == nil {
				winner <- owner
			}
		}(sid)
	}
	race.Wait()
	for len(winner) > 0 {
		<-winner
		wins++
	}
	if wins != 1 {
		t.Fatalf("expected one competing claim winner, got %d", wins)
	}
	base := time.Now()
	s.now = func() time.Time { return base.Add(2 * time.Minute) }
	if len(s.Claims("project-a")) != 0 {
		t.Fatal("expired claim remains visible")
	}
	if _, err = s.Acquire("project-a", c.ID, "src/api", time.Minute); err != nil {
		t.Fatal("expired claim was not released")
	}

	if _, err = s.PutMemory("project-a", "project", "durable", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutMemory("project-a", "global", "global", nil, true); err != nil {
		t.Fatal(err)
	}
	if got := s.ExportMemory("project-a"); len(got) != 1 || got[0].Text != "durable" {
		t.Fatalf("bad export: %#v", got)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.SearchMemory("project-a", "durable", "project", 10); len(got) != 1 {
		t.Fatalf("memory not persisted: %#v", got)
	}
}

func TestMemoryExportMaximumRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "one"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("x", 65536)
	m, err := s.PutMemory("one", "project", text, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "memory.jsonl")
	if err = WriteExport(file, s.ExportMemory("one")); err != nil {
		t.Fatal(err)
	}
	s.Close()
	items, err := ReadExport(file)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(filepath.Join(dir, "two"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.state.Memories[m.ID] = Memory{ID: m.ID, Project: "other", Scope: "global", Text: "keep"}
	if err = other.ImportMemory("two", items); err != nil {
		t.Fatal(err)
	}
	if got := other.SearchMemory("two", text, "project", 10); len(got) != 1 || got[0].ID == m.ID {
		t.Fatalf("round-trip or collision remap failed: %#v", got)
	}
}

func TestCorruptStateFailsVisibly(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.StartSession("p", "a", "codex", "")
	s.Close()
	path := dir + "/state.json"
	os.WriteFile(path, []byte(`{"broken"`), 0600)
	if _, err := Open(dir); err == nil {
		t.Fatal("corrupt state accepted")
	}
}
