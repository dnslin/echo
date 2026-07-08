# 修复 PR32 code review findings Implementation Plan

## Ordered Checklist

1. Activate task with `task.py start`.
2. Build feedback loop by adding failing regression tests:
   - handler passes request context to room creator;
   - oversized request body returns `400 invalid_request` and does not call room creator;
   - service rejects overlong `anonymous_id` and `avatar_id`;
   - HTTP layer maps those validation errors.
3. Update HTTP handler dependency interface and body limit.
4. Update room service validation constants and errors.
5. Update store `OpenSQLite` migration failure cleanup.
6. Update OpenAPI error schema/examples/responses.
7. Run targeted tests, then full backend tests.
8. Run `git diff --check`.

## Validation Commands

```bash
go test -count=1 ./services/api/internal/room ./services/api/internal/http ./services/api/internal/store
go test -count=1 ./services/api/...
git diff --check
```

## Rollback Points

- If context-aware handler changes ripple unexpectedly, keep `Service.Create` for non-HTTP callers and only change the handler interface to `CreateContext`.
- If DB cleanup failure is hard to test black-box, keep the implementation minimal and preserve the original migration error.

## Scope Guard

Do not implement join-room, LiveKit token/session token, WebSocket, expiry runtime, or deployment changes.
