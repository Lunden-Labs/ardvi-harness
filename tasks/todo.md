# Codex bridge checks

- [x] Unix WebSocket one-shot delivery and thread eligibility fake tests pass.
- [x] Inbox deduplication, retry, and single-instance tests pass.
- [x] Codex hook start/stop/disable tests pass; Claude hooks remain unchanged.
- [x] Delivery docs, diagnostics, and changelog match behavior.
- [x] Independent review, `go test -race ./...`, `go vet ./...`, hook integration,
  shell syntax and `make -n help` pass.
- [x] Own-session one-shot transport verified on Codex 0.153.2: exit 0 and the
  labelled notification arrived in the active primary session.
- [x] Background delivery of a real Ardvi inbox message to the active primary
  session verified; the bridge logged `status=active`.
- [ ] Real inbox message wakes an idle session, without user intervention.
- [ ] Branch commit handed off for review; no main push or release tag.
