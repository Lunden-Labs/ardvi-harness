package hub_test

import "testing"

func TestMessageDeliveryToolAppearsOnRetryAndThread(t *testing.T) {
	const project = "11111111-1111-4111-8111-111111111111"
	_, clients := fabricClients(t, project)
	client := clients[0]
	sender := registerFabric(t, client, "codex", "one", "sender")
	receiver := registerFabric(t, client, "claude", "one", "receiver")
	args := map[string]any{"session_id": sender["id"], "to_agent_id": receiver["agent_id"], "body": "receipt", "idempotency_key": "receipt-key"}
	message := validateOutputSchema(t, client, "message_send", args)
	delivery := message["delivery"].(map[string]any)["agent:"+receiver["agent_id"].(string)].(map[string]any)
	if delivery["status"] != "pending" {
		t.Fatalf("send delivery = %#v", delivery)
	}
	result := validateOutputSchema(t, client, "message_delivery", map[string]any{"session_id": receiver["id"], "message_ids": []string{message["id"].(string)}, "status": "delivered"})
	if result["messages"].([]any)[0].(map[string]any)["delivery"].(map[string]any)["agent:"+receiver["agent_id"].(string)].(map[string]any)["status"] != "delivered" {
		t.Fatalf("delivery result = %#v", result)
	}
	if retry := call(t, client, "message_send", args); retry["delivery"].(map[string]any)["agent:"+receiver["agent_id"].(string)].(map[string]any)["status"] != "delivered" {
		t.Fatalf("retry delivery = %#v", retry)
	}
	thread := call(t, client, "thread_read", map[string]any{"thread_id": message["thread_id"]})["messages"].([]any)
	if len(thread) != 1 || thread[0].(map[string]any)["delivery"].(map[string]any)["agent:"+receiver["agent_id"].(string)].(map[string]any)["status"] != "delivered" {
		t.Fatalf("thread delivery = %#v", thread)
	}
}
