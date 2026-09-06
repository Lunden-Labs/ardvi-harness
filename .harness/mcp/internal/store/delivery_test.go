package store

import "testing"

func TestDeliverySurvivesRetryAndStableHandover(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sender := register(t, s, "one", "sender-native", "sender")
	receiver := register(t, s, "one", "receiver-native", "receiver")
	message, err := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: receiver.AgentID, Body: "deliver", IdempotencyKey: "delivery-key"})
	if err != nil {
		t.Fatal(err)
	}
	key := "agent:" + receiver.AgentID
	if got := message.Delivery[key].Status; got != "pending" {
		t.Fatalf("new message delivery = %q, want pending", got)
	}
	if _, err = s.Delivery("one", receiver.ID, []string{message.ID}, "undeliverable", "adapter offline"); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession("one", receiver.ID); err != nil {
		t.Fatal(err)
	}
	receiver2 := register(t, s, "one", "receiver-next", "receiver")
	recorded, err := s.Delivery("one", receiver2.ID, []string{message.ID}, "delivered", "")
	if err != nil {
		t.Fatal(err)
	}
	if receipt := recorded[0].Delivery[key]; receipt.Status != "delivered" || receipt.SessionID != receiver2.ID {
		t.Fatalf("handover receipt = %#v", receipt)
	}
	if recorded[0].Status != "pending" || len(recorded[0].Acked) != 0 {
		t.Fatalf("receipt changed message state: %#v", recorded[0])
	}
	if _, err = s.Delivery("one", receiver2.ID, []string{message.ID}, "undeliverable", "late failure"); err != nil {
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
	retry, err := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: receiver.AgentID, Body: "deliver", IdempotencyKey: "delivery-key"})
	if err != nil || retry.ID != message.ID || retry.Delivery[key].Status != "delivered" {
		t.Fatalf("retry lost durable delivery: %#v %v", retry, err)
	}
}

func TestDeliveryRejectsUnauthorizedReceiptAtomically(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sender := register(t, s, "one", "sender-native", "sender")
	receiver := register(t, s, "one", "receiver-native", "receiver")
	other := register(t, s, "one", "other-native", "other")
	message, err := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: receiver.AgentID, Body: "private"})
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := s.SendMessage("one", SendInput{SessionID: sender.ID, ToAgentID: other.AgentID, Body: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Delivery("one", other.ID, []string{message.ID}, "delivered", ""); err == nil {
		t.Fatal("unrelated session recorded a receipt")
	}
	if got := s.state.Messages[0].Delivery["agent:"+receiver.AgentID].Status; got != "pending" {
		t.Fatalf("unauthorized receipt changed delivery to %q", got)
	}
	if _, err = s.Delivery("one", receiver.ID, []string{message.ID, otherMessage.ID}, "delivered", ""); err == nil {
		t.Fatal("mixed receipt batch succeeded")
	}
	if got := s.state.Messages[0].Delivery["agent:"+receiver.AgentID].Status; got != "pending" {
		t.Fatalf("failed mixed batch changed delivery to %q", got)
	}
	if _, err = s.Delivery("one", receiver.ID, []string{message.ID}, "undeliverable", " "); err == nil {
		t.Fatal("undeliverable receipt without reason succeeded")
	}
}

func TestLegacyBroadcastHasNoRecipientReceipt(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sender, err := s.StartSession("one", "sender", "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.Send("one", "project", sender.ID, "*", "", "broadcast", false)
	if err != nil || message.Delivery != nil {
		t.Fatalf("broadcast receipt = %#v, %v", message.Delivery, err)
	}
}
