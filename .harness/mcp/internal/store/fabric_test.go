package store

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func register(t *testing.T, s *Store, project, native, key string) Session {
	t.Helper()
	v, err := s.ReconcileSession(project, Registration{
		MachineID: "machine", AgentKey: key, ProjectName: project,
		Name: key, Client: "codex", NativeSessionID: native,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestStableInboxAckSurvivesOfflineRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sender := register(t, s, "one", "sender-native", "sender")
	receiver := register(t, s, "one", "receiver-native", "receiver")
	message, err := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: receiver.AgentID, Body: "persist", AckRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession("one", receiver.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	receiver2 := register(t, s, "one", "receiver-next", "receiver")
	if receiver2.AgentID != receiver.AgentID || receiver2.ID == receiver.ID {
		t.Fatalf("identity/session lifecycle mismatch: %#v %#v", receiver, receiver2)
	}
	if got, _ := s.Inbox("one", receiver2.ID, 10); len(got) != 1 || got[0].ID != message.ID {
		t.Fatalf("stable inbox did not survive restart: %#v", got)
	}
	if err = s.Ack("one", receiver2.ID, message.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Inbox("one", receiver2.ID, 10); len(got) != 0 {
		t.Fatalf("stable acknowledgement did not clear inbox: %#v", got)
	}
}

func TestReconcileConflictsUntilLeaseExpires(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Now().UTC()
	s.now = func() time.Time { return base }
	first := register(t, s, "one", "native-1", "main")
	again := register(t, s, "one", "native-1", "main")
	if again.ID != first.ID {
		t.Fatal("same native session was not reconciled idempotently")
	}
	if _, err = s.ReconcileSession("one", Registration{MachineID: "machine", AgentKey: "main", Name: "main", Client: "codex", NativeSessionID: "native-2"}); err == nil || !strings.Contains(err.Error(), "agent_key") {
		t.Fatalf("expected actionable live identity conflict, got %v", err)
	}
	s.now = func() time.Time { return base.Add(LeaseDuration + time.Second) }
	next := register(t, s, "one", "native-2", "main")
	if next.AgentID != first.AgentID || next.ID == first.ID {
		t.Fatal("expired native mapping did not create a new session for the stable agent")
	}
	if _, err = s.Renew("one", first.ID); err == nil {
		t.Fatal("renew revived expired session")
	}
}

func TestProjectRequestAcceptRecoveryAndFencing(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Now().UTC()
	s.now = func() time.Time { return base }
	sender := register(t, s, "source", "source-native", "main")
	a := register(t, s, "target", "target-a", "a")
	b := register(t, s, "target", "target-b", "b")
	request, err := s.SendMessage("source", SendInput{SessionID: sender.ID, ToProjectID: "target", SpaceID: "global://default", Kind: "request", Body: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Ack("target", a.ID, request.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.PendingRequests("target", a.ID, 10); err != nil || len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("ack removed pending request: %#v %v", pending, err)
	}
	var accepted Message
	wins := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, session := range []string{a.ID, b.ID} {
		wg.Add(1)
		go func(session string) {
			defer wg.Done()
			v, e := s.RequestAccept("target", session, request.ID)
			if e == nil {
				mu.Lock()
				wins++
				accepted = v
				mu.Unlock()
			}
		}(session)
	}
	wg.Wait()
	if wins != 1 || accepted.AcceptanceToken == "" {
		t.Fatalf("expected one accepting session, got %d: %#v", wins, accepted)
	}
	oldSession, newSession := accepted.AcceptedBy, a.ID
	if oldSession == a.ID {
		newSession = b.ID
	}
	s.now = func() time.Time { return base.Add(LeaseDuration + time.Second) }
	// Renew the contender at the edge by reconciling its native session, then recover the request.
	contender := s.state.Sessions[newSession]
	contender.LeaseExpires = s.now().Add(LeaseDuration)
	contender.Updated = s.now()
	s.state.Sessions[newSession] = contender
	reaccepted, err := s.RequestAccept("target", newSession, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reaccepted.AcceptanceToken == accepted.AcceptanceToken {
		t.Fatal("reaccept reused fencing token")
	}
	if _, err = s.RequestComplete("target", oldSession, request.ID, accepted.AcceptanceToken, "stale"); err == nil {
		t.Fatal("expired owner completed reaccepted request")
	}
	result, err := s.RequestComplete("target", newSession, request.ID, reaccepted.AcceptanceToken, "done")
	if err != nil || result.Kind != "result" || result.ToAgentID != sender.AgentID || result.Thread != request.Thread {
		t.Fatalf("bad completion result: %#v %v", result, err)
	}
	again, err := s.RequestComplete("target", newSession, request.ID, reaccepted.AcceptanceToken, "done")
	if err != nil || again.ID != result.ID {
		t.Fatalf("completion retry was not idempotent: %#v %v", again, err)
	}
	if _, err = s.RequestComplete("target", newSession, request.ID, reaccepted.AcceptanceToken, "different"); err == nil {
		t.Fatal("completion retry changed the recorded result")
	}
	if result.AuthorizationRef != request.AuthorizationRef {
		t.Fatal("completion result lost authorization provenance")
	}
}

func TestRenewExtendsLiveRequestOwnership(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Now().UTC()
	s.now = func() time.Time { return base }
	sender := register(t, s, "one", "sender", "sender")
	worker := register(t, s, "one", "worker", "worker")
	request, _ := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: worker.AgentID, Kind: "request", Body: "work", AuthorizationRef: "assignment"})
	accepted, _ := s.RequestAccept("one", worker.ID, request.ID)
	s.now = func() time.Time { return base.Add(time.Minute) }
	if _, err = s.Renew("one", worker.ID); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return base.Add(2*time.Minute + 30*time.Second) }
	result, err := s.RequestComplete("one", worker.ID, request.ID, accepted.AcceptanceToken, "done")
	if err != nil || result.AuthorizationRef != "assignment" {
		t.Fatalf("heartbeat did not extend request ownership: %#v %v", result, err)
	}
}

func TestStableRoutingDoesNotGuessNamesAndHonorsGlobalDenial(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := register(t, s, "a", "a-native", "main")
	b := register(t, s, "b", "b-native", "main")
	legacy, _ := s.StartSession("b", a.Name, "codex", "")
	if _, err = s.SendMessage("a", SendInput{SessionID: a.ID, ToAgentID: legacy.Name, SpaceID: "global://default", Body: "no guessing"}); err == nil {
		t.Fatal("stable routing guessed an agent from a legacy name")
	}
	if err = s.SetGlobalAccess("b", false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Discover("a", DiscoveryFilter{ProjectID: "b"}); err != nil || len(got) != 0 {
		t.Fatalf("denied project leaked through discovery: %#v %v", got, err)
	}
	if _, err = s.SendMessage("a", SendInput{SessionID: a.ID, ToAgentID: b.AgentID, SpaceID: "global://default", Body: "denied"}); err == nil {
		t.Fatal("message crossed denied global boundary")
	}
	hiddenErr := err
	if _, randomErr := s.SendMessage("a", SendInput{SessionID: a.ID, ToAgentID: "random", SpaceID: "global://default", Body: "denied"}); randomErr == nil || hiddenErr.Error() != randomErr.Error() {
		t.Fatalf("hidden destination was distinguishable from absent destination: %v / %v", hiddenErr, randomErr)
	}
	if _, err = s.PutMemory("b", "global", "denied", nil, true); err == nil {
		t.Fatal("denied project published global memory")
	}
	if _, err = s.PutMemory("a", "global", "shared", nil, true); err != nil {
		t.Fatal(err)
	}
	if got := s.SearchMemory("b", "shared", "", 10); len(got) != 0 {
		t.Fatalf("denied project read global memory: %#v", got)
	}
	if err = s.SetGlobalAccess("a", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SendMessage("a", SendInput{SessionID: a.ID, SpaceID: "global://default", Kind: "broadcast", Body: "denied locally"}); err == nil {
		t.Fatal("denied project sent to global space without a cross-project destination")
	}
	if err = s.SetGlobalAccess("b", true); err != nil {
		t.Fatal(err)
	}
	_, hiddenErr = s.SendMessage("a", SendInput{SessionID: a.ID, ToAgentID: b.AgentID, Body: "probe using default private space"})
	_, absentErr := s.SendMessage("a", SendInput{SessionID: a.ID, ToAgentID: "absent", Body: "probe using default private space"})
	if hiddenErr == nil || absentErr == nil || hiddenErr.Error() != absentErr.Error() {
		t.Fatalf("private caller learned global membership through routing errors: %v / %v", hiddenErr, absentErr)
	}
}

func TestSendIdempotencyAndAtomicClaims(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := register(t, s, "one", "native-a", "a")
	b := register(t, s, "one", "native-b", "b")
	in := SendInput{SessionID: a.ID, ToAgentID: b.AgentID, Body: "once", IdempotencyKey: "key"}
	first, err := s.SendMessage("one", in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SendMessage("one", in)
	if err != nil || again.ID != first.ID {
		t.Fatalf("same idempotency key duplicated message: %#v %v", again, err)
	}
	in.Body = "different"
	if _, err = s.SendMessage("one", in); err == nil {
		t.Fatal("conflicting idempotency key was accepted")
	}
	if _, err = s.Acquire("one", b.ID, "two", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcquireMany("one", a.ID, []string{"one", "two"}, time.Minute); err == nil {
		t.Fatal("partially conflicting claim batch succeeded")
	}
	if got := s.Claims("one"); len(got) != 1 || got[0].Resource != "two" {
		t.Fatalf("failed batch acquired a partial claim: %#v", got)
	}
}

func TestMessageQuotaPreservesPendingMessages(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	session := register(t, s, "one", "native", "main")
	for i := 0; i < maxProjectMessages; i++ {
		s.state.Messages = append(s.state.Messages, Message{ID: id(), Project: "one", Scope: "project", FromAgentID: session.AgentID, ToAgentID: session.AgentID, Status: "pending"})
	}
	first := s.state.Messages[0].ID
	if _, err = s.SendMessage("one", SendInput{SessionID: session.ID, ToAgentID: session.AgentID, Body: "overflow"}); err == nil {
		t.Fatal("message quota accepted overflow by deleting pending work")
	}
	if len(s.state.Messages) != maxProjectMessages || s.state.Messages[0].ID != first {
		t.Fatal("message quota deleted pending work")
	}
	// Completed work may be retained as history even if its transport ACK was
	// requested; it must not permanently consume pending queue capacity.
	s.state.Messages[0].Kind = "request"
	s.state.Messages[0].Status = "completed"
	s.state.Messages[0].AckRequired = true
	if _, err = s.SendMessage("one", SendInput{SessionID: session.ID, ToAgentID: session.AgentID, Body: "new work"}); err != nil {
		t.Fatalf("completed request blocked queue capacity: %v", err)
	}
}

func TestExpiredSessionsReportStaleAndReturnedSlicesAreDetached(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := time.Now().UTC()
	s.now = func() time.Time { return base }
	session := register(t, s, "one", "native", "main")
	memory, err := s.PutMemory("one", "project", "remember", []string{"original"}, true)
	if err != nil {
		t.Fatal(err)
	}
	memory.Tags[0] = "mutated"
	if got := s.SearchMemory("one", "remember", "project", 10); got[0].Tags[0] != "original" {
		t.Fatal("returned memory tags mutated store state")
	}
	if _, err = s.PutMemory("one", "global", "published", nil, true); err != nil {
		t.Fatal(err)
	}
	if got := s.SearchMemory("one", "", "project", 10); len(got) != 1 || got[0].Text != "remember" {
		t.Fatalf("explicit project memory search included global publications: %#v", got)
	}
	s.now = func() time.Time { return base.Add(LeaseDuration + time.Second) }
	sessions := s.Sessions("one", 10)
	if len(sessions) != 1 || sessions[0].ID != session.ID || sessions[0].State != "stale" {
		t.Fatalf("expired session did not report stale: %#v", sessions)
	}
}

func TestRegistrationAndDiscoveryFieldsAreBounded(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.ReconcileSession("one", Registration{Client: "codex", NativeSessionID: strings.Repeat("x", 257)}); err == nil {
		t.Fatal("oversized native session ID was accepted")
	}
	register(t, s, "one", "native", "main")
	if _, err = s.Discover("one", DiscoveryFilter{ProjectName: strings.Repeat("x", 121)}); err == nil {
		t.Fatal("oversized discovery filter was accepted")
	}
}

func TestNativeActivityRenewsWorkAndCrashReleasesClaims(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	sender := register(t, s, "one", "sender", "sender")
	worker := register(t, s, "one", "worker", "main")
	request, err := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: worker.AgentID, Kind: "request", Body: "work"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.RequestAccept("one", worker.ID, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Acquire("one", worker.ID, "file.go", time.Hour); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	register(t, s, "one", "worker", "main") // Native prompt reconciliation is real activity.
	now = now.Add(90 * time.Second)
	if _, err = s.RequestComplete("one", worker.ID, request.ID, accepted.AcceptanceToken, "done"); err != nil {
		t.Fatalf("native prompt activity lost request ownership: %v", err)
	}
	now = now.Add(time.Minute) // Simulate crash without SessionEnd, before resource TTL.
	replacement := register(t, s, "one", "fresh-conversation", "main")
	if _, err = s.Acquire("one", replacement.ID, "file.go", time.Hour); err != nil {
		t.Fatalf("crashed native session kept its resource claim: %v", err)
	}
}
