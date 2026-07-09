# Fix PR 37 code review findings — Implementation Plan

## Preconditions

- Work on PR branch `issue-15-websocket-room-state`.
- Do not merge or close PR manually.
- Keep fixes scoped to reviewed PR #37 findings.

## Ordered checklist

### 1. Build failing regression tests

Write focused tests first for:

1. valid cross-origin WebSocket dial succeeds with configured allowed origin;
2. `redactedTokenRequest` redacts both `URL.RawQuery` and `RequestURI` while preserving original request for hub authentication;
3. private snapshots/heartbeat do not advance shared room broadcast sequence for other clients;
4. a connection cannot miss a room event between snapshot creation and registration;
5. one member cannot leave another member through HTTP leave and close their WebSocket without valid matching room session credentials;
6. room hub state is deleted after the last connection closes and HTTP notifications without connected clients do not retain empty rooms;
7. burst `room.resync_requested` is bounded and returns `room.error`/rate limit while a normal single resync still works.

### 2. Fix WebSocket origin handling

- Add hub config for `OriginPatterns` or equivalent.
- Pass it to `websocket.AcceptOptions`.
- Wire runtime defaults suitable for expected desktop/WebView2/local clients without wildcard arbitrary origin.
- Keep tests injectable.

### 3. Fix request redaction

- Update `redactedTokenRequest` to rewrite `RequestURI` consistently with redacted `URL`.
- Ensure handler restores or defers request state safely.
- Add tests covering `RequestURI` and original-token handoff.

### 4. Fix sequence and registration semantics

- Separate shared room sequence from private message sequence behavior.
- Ensure `room.snapshot.last_seq` is the latest shared sequence position visible to the receiver.
- Ensure heartbeat and private `room.error` do not create false gaps for other clients.
- Keep initial snapshot as the first queued event.
- Register connection in a way that avoids the snapshot/register broadcast gap.

### 5. Fix leave authorization and notification safety

- Prefer requiring `Authorization: Bearer <room_session_token>` on HTTP leave and verifying it matches path room and requested member.
- Use existing `session.Verify` and `AuthorizeMemberContext` before `LeaveContext`.
- Update OpenAPI/docs/tests if HTTP leave auth changes.
- Ensure successful leave still broadcasts `member.left` and closes the leaving member's socket.

### 6. Fix hub cleanup

- Delete empty `h.rooms[roomID]` entries after unregister.
- Avoid creating sequence/room entries for no-op join/leave notifications when no clients exist.
- Keep replacement connection cleanup safe.

### 7. Fix resync burst behavior

- Add per-connection resync rate limiting or coalescing.
- Return `room.error` with `rate_limited` for bursts.
- Keep normal resync path returning `room.snapshot`.

### 8. Update specs/docs if contracts changed

- Update `services/api/openapi.yaml` and relevant docs/specs for HTTP leave auth or origin config if changed.
- Keep WebSocket contract aligned.

### 9. Verify and update PR

Run:

```bash
go test -count=1 ./services/api/...
git diff --check
```

Then commit fixes on PR branch and push.

## Review gates

- Every code-review finding has a targeted fix or documented refutation.
- Regression tests prove the reviewed failure mode.
- No new out-of-scope feature slips in.
- No token/secret plaintext is introduced in logs/docs/fixtures beyond placeholders.
