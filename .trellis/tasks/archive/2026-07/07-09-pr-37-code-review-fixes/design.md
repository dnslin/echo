# Fix PR 37 code review findings — Design

## First-principles analysis

### Challenge assumptions

- Assumption: a valid room session token alone is enough for browser WebSocket acceptance. Incomplete: the WebSocket library also enforces Origin by default.
- Assumption: queuing snapshot before registration always preserves first-event ordering. Partially wrong: it preserves local ordering but creates a TOCTOU gap for broadcasts while the connection is invisible.
- Assumption: HTTP leave remains safe because it existed before PR #37. Wrong after this PR: the new notifier turns unauthenticated leave into a realtime close/broadcast side effect.
- Assumption: redacting only `URL.RawQuery` is enough. Wrong for Go server requests: `httputil.DumpRequest` prefers `RequestURI`.
- Assumption: every server-to-client message can share one room sequence. Wrong for client-private messages because other clients never receive those sequence numbers.
- Assumption: empty in-memory room state is harmless. Wrong for temporary rooms at scale: memory grows with room churn.
- Assumption: resync is cheap. Wrong: each resync hits SQLite and serializes full room state.

### Bedrock truths

- Browser/WebView2 sends an Origin; `coder/websocket.Accept` rejects cross-origin by default unless configured.
- Authorization must happen before upgrade and before any client payload is trusted.
- A room event stream sequence only helps clients if every sequence value in the stream is deliverable to each relevant receiver or represented by that receiver's snapshot position.
- WebSocket connection maps are process memory; without deletion, memory use grows monotonically with unique room IDs.
- Token query strings are bearer credentials; any log/recovery surface exposing them is a replay risk until expiry.
- SQLite reads and JSON writes consume finite per-request resources; authenticated clients can still be abusive or buggy.

### Rebuild from truths

1. Make cross-origin acceptance explicit through hub config. Runtime can default to the expected browser/WebView2/local development origins, not wildcard arbitrary access.
2. Represent shared room sequence separately from private transport messages. Initial snapshots carry current shared sequence and private messages do not advance it.
3. Register a connection before later broadcasts can include/exclude it, but queue the initial snapshot before registration so the write queue still emits snapshot first; use current shared sequence at that point.
4. Require HTTP leave credentials before notifying the hub. Reuse `session.Verify` + `AuthorizeMemberContext` so only the authenticated current member can leave itself.
5. Redact both `URL.RawQuery` and `RequestURI` on logging/recovery-visible request copies.
6. Delete empty room state on unregister and avoid creating room state for no-recipient notifications where possible.
7. Rate-limit per-connection resync requests with a small interval; normal manual resync remains available, bursts return `room.error`.

## Feedback loops

Add deterministic Go regression tests before fixes where feasible:

- Cross-origin valid token dial should succeed.
- Panic/recovery or request-copy test must show redacted `RequestURI`.
- Two-client sequence test: second client private snapshot/heartbeat must not make first client's next broadcast skip sequence.
- Ordering test: connection registered atomically enough that a post-snapshot concurrent broadcast is represented either in snapshot position or subsequent event.
- Leave attack test: Bob cannot POST leave for Alice and close Alice's WebSocket without Alice's token.
- Cleanup test: after final disconnect, hub no longer retains room entry.
- Resync burst test: repeated resync commands produce bounded snapshots and a rate-limit error.

## Compatibility

- Existing create/join/fresh-token behavior remains unchanged.
- HTTP leave response may gain credential requirement only if contract/docs/tests are updated in the same PR branch; if changing HTTP leave auth is too broad, suppress hub close/broadcast unless the leave request is authenticated as that member.
- No schema migration is needed.
- No frontend work is required.

## Risks

- Cross-origin config must avoid `*` by default.
- Fixing sequence semantics requires tests to assert contract behavior, not implementation counters.
- Leave auth change may affect existing leave tests and OpenAPI contract; update them consistently.
