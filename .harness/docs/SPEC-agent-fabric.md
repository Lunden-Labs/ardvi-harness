# Local Agent Fabric

Status: user-approved product requirements, 2026-09-05.
Supersedes the session-addressing and explicit-global opt-in portions of
SPEC-native-agent-hub. ADR-0002's local container and native clients remain.

## Capability map and order

| Module | Responsibility | Depends on |
| --- | --- | --- |
| identity | Persistent machines, agents, projects; ephemeral session leases | — |
| messaging | Stable inboxes, threads, acknowledgements, request ownership | identity |
| context | Discovery, bounded bootstrap, memory and operating rules | identity, messaging |
| native | Hooks, delivery adapters, managed instructions and entry skills | context |

Implement and test in this order. Keep the existing Go store and MCP SDK;
no broker, new database, network deployment, or provider launcher is required.

## Agreed behavior

- One trusted local user, one machine, several projects and native clients.
- A stable agent is keyed by machine, project, client and an optional explicit
  agent key. Default key is `main`: each project has its own Claude and Codex.
  Names are cosmetic; new conversations recover the agent's opaque ID.
- Native resume/compact reconciles idempotently. A new conversation gets a new
  session. A second live conversation cannot silently take the same inbox;
  an explicit different agent key creates an independent agent.
- Native subagents remain provider-owned. Ardvi never launches a client.
- All local projects join `global://default` by default. Project communication
  remains available in `project://<uuid>`. Host-owned policy may deny global
  access; model calls cannot change that policy.
- Internal memory is project-private. Explicit global publications are shared;
  origin project and update time remain attached. Only the origin may archive.
- Human assignments permit necessary delegation across projects. Messages carry
  the original assignment reference through the thread. That reference is
  provenance, not a new grant or cryptographic proof of human authorization.
  The receiving model verifies scope against its trusted human instructions.
- Stable agent/project messages survive offline periods. Project requests have
  one atomic accepting session. Broadcast is separate from work acceptance.
- Delivery is not acknowledgment; acknowledgment is not request completion.
- Repository state is authoritative; memory and peer reports support it.
- Installed instructions designate the primary native session as orchestrator.
  It discovers relevant harness skills, delegates bounded independent tasks to
  the least expensive adequate supported model, keeps orchestration flat, and
  verifies worker evidence. Cheap mechanical work must not default to the most
  expensive model; unavailable model overrides must never be invented.

## Identity, transport and lifecycle

Project UUID is committed in `.ardvi/project.json`. Worktrees/clones retaining
that UUID are the same logical project; an independent fork needs a new UUID.
Machine ID is generated once in host Ardvi state, never committed to a project.
The registry stores project names and IDs, never cross-project filesystem paths.

SessionStart reconciles with the service before claiming registration succeeded.
It injects a compact identity paragraph directing `context_bootstrap(session_id)`.
Compatibility uses an explicit session ID: current stateless HTTP request context
does not securely identify a native session. The project header is namespace
selection, not authentication. This release trusts local processes and binds to
loopback; remote deployment requires authenticated host/session binding first.

Session activity renews a short lease. Expired or ended sessions cannot perform
work until reconciled by a native hook. Discovery computes offline from expiry;
an unattended delivery adapter must not keep a crashed client advertised live.
SessionEnd ends only the ephemeral session and releases its claims/ownership.
Agent identity, pending inboxes, completed conversations and memory persist.

## Messaging and recovery

Primary destinations: `to_agent_id`, `to_project_id`, `space_id`.
Agent-only delivery uses that agent's stable inbox in its registered project.
Project-only requests are visible to eligible agents; one accepts atomically.
`to_session_id` is exclusive of stable destinations and intentionally ephemeral.
Legacy `to` remains compatibility-only and is never guessed into stable identity.

`message_send` is retry-safe with a sender-agent-scoped idempotency key;
conflicting payload reuse fails. Pending messages are not evicted at quota.
Request acceptance is leased and returns a fencing token. Completion requires
the current owner and token, and emits one stable reply in the original thread.
Expired ownership allows another eligible session to accept. External code edits
are not transactional: executors inspect repository state before retrying work.

All discovery, reads, sends, replies and bootstrap data respect current space
visibility. Hidden membership is not disclosed in errors. Ambiguous project names
return candidates rather than picking one. Offline known agents remain searchable.

## Bootstrap and native context

`context_bootstrap` returns versioned rules, self identity, reported Git snapshot,
visible spaces/projects/peers, unread previews, pending requests, claims and
recent memory. Collections and text previews are bounded; omitted content is
explicit with the tool to retrieve it. Bootstrap never acknowledges or accepts.
Repeat after clear/compact/resume; registration is not repeated by the model.

Managed AGENTS instructions are short and checksum-protected. CLAUDE imports
AGENTS. Existing user text survives initialization and updates. `lets-go` uses
bootstrap, but ordinary native startup must work without invoking a skill.
Native notifications always label peer content as agent correspondence, even
when the provider transport uses a user-message role.

## Verification and constraints

Use existing table-driven Go tests and shell/Python integration fixtures. No new
test framework. Build: `cd .harness/mcp && go build ./...`.
Test: `cd .harness/mcp && go test -race ./...`.
Shell: `bash -n .harness/scripts/*.sh`; Make: `make -n help`.
Run bootstrap/hooks integration fixtures and compare managed templates to output.
Do not run the live `make doctor`, deploy, or modify another project for testing.

Acceptance: Codex A and Claude B bootstrap without a skill prompt, discover each
other globally, queue a request while B is offline, and deliver after a fresh
native conversation with the same agent ID. Verify stale session exclusion,
ACK surviving restart, duplicate starts/sends, concurrent project acceptance,
claim batch atomicity, private memory, denied visibility, ambiguous project names,
bounded bootstrap, shutdown and service restart. Native client smoke tests are
reported separately from deterministic hook/MCP simulations.

## Execution ledger

- [x] Identity, leases and project/space discovery, with store regression tests.
- [x] Stable messaging, ACK, request ownership and claims, with concurrency tests.
- [x] MCP bootstrap/discovery/routing and precise tool descriptions.
- [x] Native lifecycle, managed instructions, lets-go and protocol documentation.
- [x] Cross-project integration, compatibility, portability and primary review.

Verification: all Go packages pass race tests, vet and build; bootstrap, hooks,
writing, copy, harness update, installer path, Make and catalog fixtures pass.
The initial independent reviewer hit its runtime usage limit; the primary
completed that review. The final retention and bridge changes received focused
independent reviews and regression coverage for the resulting corrections.
Live local validation on 2026-09-05 used Codex 0.153.4 and Claude Code 2.1.261:
four projects discovered one another, all eight diagnostic requests completed,
Codex bridge delivery worked, and Claude confirmed an isolated idle wake through
its Stop/asyncRewake hook without a helper Monitor. Native client crashes and
restarts were simulated separately; this is not a claim of live crash testing.
An old Codex bridge survived the original host update; native reconciliation now
checks binary identity and replaces the matching outdated adapter.

History admission now protects unfinished/direct pending work, retains modern
shared informational messages for 30 days, and retains evicted idempotency keys
for another 30 days (10,000 per-origin receipts). Bootstrap warns at 80% pressure.
Request acceptance reserves result capacity before allowing work to begin.

## Follow-up outside this local implementation

- Record real client version compatibility for Codex's experimental daemon
  delivery and Claude's `asyncRewake`; deterministic transport fixtures remain
  the automated baseline.
- Add authenticated host/session binding before any remote deployment. The
  committed project UUID is deliberately not a credential.
- Back up the state volume before release migration. Older service binaries do
  not understand Fabric records and must not rewrite an upgraded snapshot.
