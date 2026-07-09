# Fix PR 37 code review findings

## Goal

Fix and optimize the real code-review findings reported for PR #37 so the room-state WebSocket implementation accepts intended clients, preserves room event delivery semantics, avoids token leakage, avoids unauthorized leave side effects, and does not accumulate unbounded hub state or resync load.

## Source context

- PR #37: `feat(api): implement room websocket hub`.
- Review target: backend room-state WebSocket implementation for Issue #15.
- Review findings to address:
  1. default `websocket.AcceptOptions{}` rejects valid browser/WebView2 cross-origin clients;
  2. connection registration after snapshot construction can miss concurrent join/leave events;
  3. unauthenticated HTTP leave plus new hub notification can let a caller close another member's WebSocket;
  4. WebSocket token redaction does not rewrite `RequestURI`, so Gin recovery dumps can leak tokens;
  5. private snapshot/heartbeat/error messages consume room-wide sequence numbers and create false gaps;
  6. empty per-room hub entries are never removed;
  7. unlimited `room.resync_requested` can force repeated SQLite snapshot reads.

## Requirements

### R1. WebSocket origin acceptance

- Valid browser/WebView2 WebSocket clients with a valid room session token must be able to connect from configured/allowed origins.
- The fix must not silently open the server to arbitrary cross-origin CSRF risk without an explicit safe default or config boundary.
- Add regression coverage for a valid cross-origin WebSocket handshake.

### R2. Initial snapshot and event ordering

- A newly accepted connection must not miss a concurrent room event after its snapshot sequence position is chosen.
- The first client-visible event must remain `room.snapshot`.
- Add a deterministic regression test that fails on the old ordering bug.

### R3. Leave authorization / broadcast safety

- HTTP leave must not allow a caller to disconnect or broadcast leave for another active member solely by sending that member's ID.
- If credential-based authorization already exists for related endpoints, reuse that room session token model for leave or otherwise prevent cross-member leave side effects.
- Add a regression test showing another member cannot force a victim's WebSocket to receive `member.left` and close.

### R4. Query-token redaction

- Redacted WebSocket requests must remove token plaintext from `URL.RawQuery` and `RequestURI` surfaces visible to Gin recovery/logging.
- Authentication must still receive the original token.
- Add regression coverage for `RequestURI` redaction and original-token preservation.

### R5. Sequence semantics

- Room-wide `seq` must only advance for events that belong to the shared room event stream.
- Private snapshots, heartbeat pings, and private room errors must not create false sequence gaps for other clients.
- `room.snapshot.payload.last_seq` must reflect the latest shared room event sequence visible to that receiver.
- Add regression coverage showing a second client's snapshot/heartbeat does not cause the first client to observe a broadcast sequence gap.

### R6. Hub state cleanup

- When the last WebSocket connection leaves a room, empty in-memory room connection state must be deleted unless required for current event delivery.
- No HTTP-only join/leave notification should create permanent empty room state.
- Add regression coverage for room entry pruning.

### R7. Resync abuse control

- Repeated `room.resync_requested` messages from one connection must be bounded by rate limiting, coalescing, or another deterministic guard.
- The guard must not break normal single resync behavior.
- Add regression coverage for a burst of resync requests producing a bounded response/error behavior.

## Acceptance criteria

- [ ] Each R1-R7 has at least one deterministic regression test that fails against the reviewed behavior and passes after the fix.
- [ ] Existing Issue #15 tests still pass.
- [ ] `go test -count=1 ./services/api/...` passes.
- [ ] `git diff --check` passes.
- [ ] PR #37 is updated with the fix commit(s).
- [ ] No raw room session token, LiveKit token, API secret, room session secret, sensitive request body, or audio data is added to logs/docs/fixtures beyond placeholder values.

## Out of scope

- Do not implement chat, mute/speaking product-state commands, reconnect product-state transitions, Redis, cross-instance broadcasting, frontend UI, accounts, or friends.
- Do not broaden the scope beyond fixing the reviewed PR #37 findings.
