# Implement — Issue 14 WebSocket 房间状态契约

## Implementation Checklist

1. Read source contracts before editing:
   - `docs/api/websocket.md`
   - `design.md` section 14
   - ADR `0015`, `0016`, `0019`, `0020`, `0021`, `0032`
   - `services/api/internal/session/token.go`
   - `services/api/internal/http/handlers.go` credential/error helpers
2. Replace `docs/api/websocket.md` placeholder content with a complete English contract.
3. Preserve the existing endpoint and message-name set unless a source document requires an explicit correction.
4. Add sections for:
   - purpose and boundaries;
   - endpoint and authentication;
   - common envelope;
   - shared room/member/error schemas;
   - server-to-client messages;
   - client-to-server messages;
   - heartbeat;
   - reconnect semantics;
   - error handling and close guidance;
   - security/privacy/logging notes;
   - non-goals;
   - implementation test checklist for the later hub task.
5. Ensure every Issue #14 required message appears with a payload schema and at least one representative JSON example where useful:
   - `room.snapshot`
   - `member.joined`
   - `member.left`
   - `member.reconnecting`
   - `member.disconnected`
   - `member.restored`
   - `member.muted_changed`
   - `member.speaking_changed`
   - `room.expired`
   - `room.error`
   - `room.resync_required`
   - `heartbeat.ping`
   - `heartbeat.pong`
   - `member.mute_changed`
   - `member.voice_mode_changed`
   - `member.leave_requested`
   - `room.resync_requested`
6. Verify identity and authorization rules are explicit:
   - server derives room/member from token;
   - client payload `member_id` is not trusted;
   - token query parameter is sensitive and must be redacted;
   - LiveKit state is not product authorization.
7. Verify reconnect rules are explicit:
   - 30-second member preservation;
   - speaking cleared on reconnecting;
   - slot preserved until timeout;
   - restoration within window;
   - disconnected after timeout;
   - LiveKit token refresh remains HTTP.
8. Run validation commands.
9. Run `trellis-check`; fix and repeat until passing.

## Validation Commands

```bash
git diff --check
go test -count=1 ./services/api/...
```

Expected results:

- `git diff --check` exits 0.
- Backend tests still pass; this docs task must not break existing API code.

## Review Gates

Before `task.py start`:

- Planning artifacts exist: `prd.md`, `design.md`, `implement.md`.
- User has approved proceeding from planning to implementation.

Before completion:

- `docs/api/websocket.md` meets every PRD acceptance criterion.
- No token-shaped real values, secrets, API keys, or reusable credentials are introduced into docs.
- MVP exclusions are explicitly stated.
- `trellis-check` passes.

## Rollback Points

- Primary changed file should be `docs/api/websocket.md`.
- If the contract accidentally expands scope into code implementation, revert code changes and keep this task documentation-only.
- If a contradiction with `design.md` or ADRs is discovered, stop and update planning before implementing a divergent contract.
