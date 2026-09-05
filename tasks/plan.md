# Codex message bridge

Implement on `codex-bridge`; do not update `main` or create a release tag.
The existing hooks, inbox formatter and seen-message state remain the delivery
foundation. Codex transport uses WebSocket frames over the socket discovered by
`codex app-server daemon version`, without a `jsonrpc` field.

## Ordered work

1. Deliver one explicitly labelled notification through `initialize`,
   `initialized`, `thread/read`, and `turn/start`. Test with a fake Unix socket;
   reject subagents and unloaded threads without resuming them.
2. Poll Ardvi inbox with shared deduplication, retry transient failures without
   marking undelivered messages seen, and enforce one bridge per session.
3. Start and stop the detached bridge through Codex session hooks. Preserve
   Claude behavior, old mappings, and never-failing hooks. Test opt-out and
   process lifecycle with fake executables.
4. Document delivery and opt-out, add daemon diagnostics, and independently
   review the complete diff. Run race tests, vet, and hook integration checks.
5. Test an explicit notification against the current native Codex session,
   then verify a real inbox message wakes an idle session. Report observed
   frames and hand the branch commit to the release owner.

## Risks

Concurrent hook/poller delivery must not lose messages or overwrite seen state.
Stale pidfiles must not cause unrelated processes to be terminated. Notification
text is agent correspondence, not a new human instruction; label its source.
Do not claim idle wakeup from an active-turn test.

## Transport verification, 2026-09-05

The fake Unix daemon verified the following outbound frames, with the thread ID
and text substituted for the target session. The same transport delivered the
labelled `ARDVI BRIDGE TEST` notification to the active primary session on
Codex 0.153.2; the CLI exited 0 and the notification appeared as an incoming
message. The first live attempt exposed that `thread.status` is an object with
a `type` field, not a string; the parser and fake response were corrected.

```json
{"id":0,"method":"initialize","params":{"clientInfo":{"name":"ardvi-codex-bridge","title":"Ardvi MCP bridge","version":"dev"},"capabilities":{"experimentalApi":true,"requestAttestation":false}}}
{"method":"initialized","params":{}}
{"id":1,"method":"thread/read","params":{"threadId":"<native-thread-id>"}}
{"id":2,"method":"turn/start","params":{"threadId":"<native-thread-id>","input":[{"type":"text","text":"[Ardvi MCP notification — delivered by ardvi codex-bridge, not typed by the user] ARDVI BRIDGE TEST: own-session transport check only; continue the existing SDK demo and bridge task."}],"turnTrigger":"ardvi-inbox"}}
```

This proves active-turn delivery only. Idle wakeup remains an unchecked gate.
