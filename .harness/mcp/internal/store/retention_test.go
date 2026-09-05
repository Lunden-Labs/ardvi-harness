package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fillPending(s *Store, project, agent string, n int) {
	for i := 0; i < n; i++ {
		s.state.Messages = append(s.state.Messages, Message{ID: id(), Project: project, FromAgentID: agent, ToAgentID: agent, Kind: "message", Status: "pending", Created: s.now().UTC()})
	}
}

func TestSharedRetentionAndRetiredKeyPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	owner := register(t, s, "p", "native", "main")
	input := SendInput{SessionID: owner.ID, ToProjectID: "p", Body: "notification", IdempotencyKey: "shared-key"}
	first, err := s.SendMessage("p", input)
	if err != nil {
		t.Fatal(err)
	}
	fillPending(s, "p", owner.AgentID, maxProjectMessages-1)
	if _, err = s.SendMessage("p", SendInput{SessionID: owner.ID, ToAgentID: owner.AgentID, Body: "full"}); err == nil {
		t.Fatal("fresh shared message evicted")
	}
	now = now.Add(sharedHistoryRetention)
	owner = register(t, s, "p", "native2", "main")
	input.SessionID = owner.ID
	if _, err = s.SendMessage("p", SendInput{SessionID: owner.ID, ToAgentID: owner.AgentID, Body: "new"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range s.state.Messages {
		if m.ID == first.ID {
			t.Fatal("expired shared notification remains")
		}
	}
	if _, err = s.SendMessage("p", input); err == nil || !strings.Contains(err.Error(), "expired history") {
		t.Fatalf("retired retry: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.now = func() time.Time { return now }
	if _, err = s.SendMessage("p", input); err == nil || !strings.Contains(err.Error(), "expired history") {
		t.Fatalf("retired key lost on restart: %v", err)
	}
	now = now.Add(retiredKeyRetention)
	owner = register(t, s, "p", "native3", "main")
	input.SessionID = owner.ID
	// Free one acknowledged direct slot; key reuse is allowed only after its documented window.
	s.state.Messages[0].Status = "acknowledged"
	m, err := s.SendMessage("p", input)
	if err != nil || m.ID == first.ID {
		t.Fatalf("expired key reuse: %v", err)
	}
}

func TestRetentionProtectsWorkAndAdmissionIsAtomic(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner := register(t, s, "p", "native", "main")
	old := s.now().Add(-2 * sharedHistoryRetention)
	records := []Message{
		{ID: id(), Project: "p", FromAgentID: owner.AgentID, ToAgentID: owner.AgentID, Kind: "message", Status: "pending", Created: old},
		{ID: id(), Project: "p", FromAgentID: owner.AgentID, ToProjectID: "p", Kind: "request", Status: "pending", Created: old},
		{ID: id(), Project: "p", To: "legacy", Status: "pending", Created: old},
	}
	for _, m := range records {
		if s.evictable(m, s.now()) {
			t.Fatalf("pending record eligible: %s", m.Kind)
		}
	}
	// An overcommitted migrated queue needs two slots, but only one record can retire.
	fillPending(s, "p", owner.AgentID, maxProjectMessages)
	s.state.Messages = append(s.state.Messages, Message{ID: id(), Project: "p", Status: "acknowledged"})
	before, _ := json.Marshal(s.state)
	if _, err = s.SendMessage("p", SendInput{SessionID: owner.ID, ToAgentID: owner.AgentID, Body: "no room"}); err == nil {
		t.Fatal("overcommitted queue admitted")
	}
	after, _ := json.Marshal(s.state)
	if string(before) != string(after) {
		t.Fatal("failed admission changed state")
	}
	if !s.MessageQuota("p").Overcommitted {
		t.Fatal("missing migration pressure")
	}
}

func TestAcceptanceReservesCompletionAtQuota(t *testing.T) {
	for _, sameProject := range []bool{false, true} {
		t.Run(map[bool]string{false: "cross-project", true: "same-project"}[sameProject], func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			sender := register(t, s, "source", "sender", "main")
			target := "target"
			if sameProject {
				target = "source"
			}
			worker := register(t, s, target, "worker", "worker")
			request, err := s.SendMessage("source", SendInput{SessionID: sender.ID, ToProjectID: target, SpaceID: "global://default", Kind: "request", Body: "work", IdempotencyKey: "work-key"})
			if err != nil {
				t.Fatal(err)
			}
			count := maxProjectMessages - 2
			if sameProject {
				count--
			}
			fillPending(s, target, worker.AgentID, count)
			accept, err := s.RequestAccept(target, worker.ID, request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if q := s.MessageQuota(target); q.ReservedResults != 1 || q.Available != 1 {
				t.Fatalf("no reservation: %+v", q)
			}
			if _, err = s.SendMessage(target, SendInput{SessionID: worker.ID, ToAgentID: worker.AgentID, Body: "last"}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.SendMessage(target, SendInput{SessionID: worker.ID, ToAgentID: worker.AgentID, Body: "steal reservation"}); err == nil {
				t.Fatal("send stole completion capacity")
			}
			result, err := s.RequestComplete(target, worker.ID, request.ID, accept.AcceptanceToken, "done")
			if err != nil {
				t.Fatal(err)
			}
			if q := s.MessageQuota(target); q.Used != maxProjectMessages || q.ReservedResults != 0 {
				t.Fatalf("bad completion accounting: %+v", q)
			}
			retry, err := s.RequestComplete(target, worker.ID, request.ID, accept.AcceptanceToken, "done")
			if err != nil || retry.ID != result.ID {
				t.Fatalf("completion retry: %v", err)
			}
			for _, m := range s.state.Messages {
				if m.ID == request.ID && s.evictable(m, s.now()) {
					t.Fatal("request evictable before result ACK")
				}
			}
			if err = s.Ack("source", sender.ID, result.ID); err != nil {
				t.Fatal(err)
			}
			for _, m := range s.state.Messages {
				if m.ID == request.ID && !s.evictable(m, s.now()) {
					t.Fatal("completed history pinned after result ACK")
				}
			}
		})
	}
}

func TestRetiredKeyQuotaAndWarning(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owner := register(t, s, "p", "native", "main")
	fillPending(s, "p", owner.AgentID, 799)
	if s.MessageQuota("p").Warning {
		t.Fatal("early warning")
	}
	fillPending(s, "p", owner.AgentID, 1)
	if !s.MessageQuota("p").Warning {
		t.Fatal("missing 80 percent warning")
	}
	fillPending(s, "p", owner.AgentID, 199)
	s.state.Messages = append(s.state.Messages, Message{ID: id(), Project: "p", FromAgentID: owner.AgentID, IdempotencyKey: "retire", Status: "acknowledged"})
	for i := 0; i < maxRetiredKeys; i++ {
		s.state.RetiredKeys = append(s.state.RetiredKeys, retiredKey{Project: "p", Key: id(), Expires: s.now().Add(time.Hour)})
	}
	before, _ := json.Marshal(s.state)
	if _, err = s.SendMessage("p", SendInput{SessionID: owner.ID, ToAgentID: owner.AgentID, Body: "full keys"}); err == nil {
		t.Fatal("discarded a live key receipt")
	}
	after, _ := json.Marshal(s.state)
	if string(before) != string(after) {
		t.Fatal("retired key quota failure mutated state")
	}
}

func TestSharedResultUsesInformationalRetention(t *testing.T) {
	now := time.Now().UTC()
	m := Message{FromAgentID: "sender", ToProjectID: "p", Kind: "result", Status: "pending", Created: now.Add(-sharedHistoryRetention)}
	if !expiredShared(m, now) {
		t.Fatal("shared result never retires")
	}
	m.ToAgentID = "recipient"
	if expiredShared(m, now) {
		t.Fatal("direct pending result expires")
	}
}
