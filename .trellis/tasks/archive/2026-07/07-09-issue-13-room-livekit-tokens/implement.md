# Implement：Issue 13 房间会话凭证与 LiveKit 短期凭证

## Preconditions

- Base branch: latest `master`.
- Before implementation, create a feature branch from latest `master`.
- Do not run `task.py start` until PRD/design/implement and OQ1 are reviewed.
- Keep scope backend-only under `services/api/**` and `docs/api`/OpenAPI.

## Test Scenarios From Requirements

### Create/join credential issuance

- Create room returns existing `room`/`member` plus `room_session_token`, `livekit_url`, and `livekit_token`.
- Join room returns existing `room`/`member` plus credentials.
- Existing create/join validation, expiry, capacity, duplicate nickname, leave lifecycle, and OpenAPI behavior remain passing.
- Credential issuance failure due to missing token config returns `500 internal_error` without leaking secrets.

### Session token

- Sign/verify happy path returns room/member claims.
- Expired token is rejected.
- Tampered payload/signature is rejected.
- Wrong secret is rejected.
- Malformed token is rejected.
- Missing room/member claim is rejected.
- Unsupported version is rejected.

### Product-member authorization

- Active `online` member in an active room is authorized.
- `reconnecting` member is authorized for credential recovery.
- Missing room returns room-not-found sentinel.
- Expired room returns room-expired sentinel.
- Missing member returns member-not-found sentinel.
- `disconnected` member is rejected as inactive.
- Member from a different room is rejected.

### LiveKit token

- Generated JWT uses member `LiveKitIdentity` as identity.
- Generated JWT grants only the expected LiveKit room.
- Generated JWT expiry is now + configured TTL.
- Publish and subscribe are allowed for voice participants.
- Admin/SIP/agent grants are absent.
- Blank key/secret/room/identity or non-positive TTL returns error.

### Fresh LiveKit endpoint

- Valid bearer room session for active member returns `livekit_url` and a scoped `livekit_token`.
- Missing bearer returns `401 invalid_room_session`.
- Tampered token returns `401 invalid_room_session`.
- Expired session token returns `401 room_session_expired`.
- Path room mismatch returns `403 room_session_mismatch`.
- Missing/disconnected/wrong-room member is rejected.
- Expired room returns `410 room_expired`.

### Logging/persistence

- No token strings or secrets are logged by new code paths.
- No token strings are stored in SQLite models or migrations.

## Implementation Steps

### 1. Prepare branch and dependency

- Confirm clean working tree.
- Fetch/switch latest `master` and create a feature branch, e.g. `issue-13-room-livekit-tokens`.
- Add `github.com/livekit/protocol` dependency from `services/api` after tests require it.

Validation:

```bash
git status --short --branch
cd services/api && go test ./...
```

### 2. Implement `internal/session` with tests first

Create:

- `services/api/internal/session/token.go`
- `services/api/internal/session/token_test.go`

Contracts:

- HMAC-SHA256 signed token, URL-safe base64 segments.
- Claims include version, room ID, member ID, expiry.
- Verification accepts a deterministic `now` for tests.
- Typed sentinel errors for invalid, expired, mismatch-worthy cases.

Validation:

```bash
cd services/api && go test -count=1 ./internal/session -v
```

### 3. Implement `internal/livekit` with tests first

Create:

- `services/api/internal/livekit/tokens.go`
- `services/api/internal/livekit/tokens_test.go`

Contracts:

- Use `github.com/livekit/protocol/auth`.
- Validate blank input and TTL before generating JWT.
- Set identity, display name, TTL, and minimal room join grant.
- Tests decode JWT payload to verify identity, room, TTL, and absence of unrelated grants.

Validation:

```bash
cd services/api && go test -count=1 ./internal/livekit -v
```

### 4. Add product-member authorization in store and room service

Modify:

- `services/api/internal/store/sqlite.go`
- `services/api/internal/store/*_test.go` or new `credentials_test.go`
- `services/api/internal/room/service.go`
- `services/api/internal/room/*_test.go` or new `credentials_test.go`

Add repository read methods:

- `FindRoomByID(ctx, roomID)`
- `FindMemberByRoomAndID(ctx, roomID, memberID)`

Add room service method, name can be adjusted to surrounding style:

- `AuthorizeMemberContext(ctx, roomID, memberID) (domain.Room, domain.Member, error)`

Rules:

- Room must exist and be active.
- Member must exist in the room.
- Member state must be `online` or `reconnecting`.
- Do not use `anonymous_id` alone as authorization.

Validation:

```bash
cd services/api && go test -count=1 ./internal/store ./internal/room -v
```

### 5. Add config TTL fields and credential issuer wiring

Modify:

- `services/api/internal/config/config.go`
- optionally add `services/api/internal/config/config_test.go` if env/default parsing changes.

Add:

- `RoomSessionTokenTTL time.Duration`
- `LiveKitTokenTTL time.Duration`

Defaults:

- `LiveKitTokenTTL = 10 * time.Minute`
- `RoomSessionTokenTTL = 2 * time.Hour`.

Add small HTTP-layer credential helper if useful, but keep signing in `session` and LiveKit JWT creation in `livekit`.

### 6. Update HTTP handlers and router with tests first

Modify:

- `services/api/internal/http/handlers.go`
- `services/api/internal/http/router.go`
- existing handler tests plus new `handlers_credentials_test.go`.

Changes:

- `NewHandlers` / router options accept credential config and room member authorizer.
- Create/join responses add top-level credential fields.
- Add `POST /v1/rooms/:room_id/livekit-token` route.
- Extract `Authorization: Bearer <room_session_token>`.
- Verify session token before room/member authorization.
- Generate fresh LiveKit token only after product state authorization.
- Map credential errors to stable JSON envelope.

Validation:

```bash
cd services/api && go test -count=1 ./internal/http -v
```

### 7. Wire API main without logging secrets

Modify:

- `services/api/cmd/api/main.go`

Rules:

- Pass config into HTTP handler/router.
- Keep startup logs free of token plaintext and secret values.
- If adding config validation, log only which config category is missing, not the value.

Validation:

```bash
cd services/api && go test -count=1 ./cmd/api ./... 
```

If `./cmd/api` has no tests, the command still verifies package compilation.

### 8. Update OpenAPI contract

Modify:

- `services/api/openapi.yaml`

Document:

- Added credential fields in create/join success schema.
- New `LiveKitTokenResponse` schema.
- New `POST /v1/rooms/{room_id}/livekit-token` path.
- Authorization header requirement.
- New error codes: `invalid_room_session`, `room_session_expired`, `room_session_mismatch`, `member_not_active` and reused room/member/internal errors.

Manual check:

- Compare OpenAPI schemas against Go request/response structs and tests.

### 9. Final validation and review

Run from repo root:

```bash
go test -count=1 ./services/api/...
git diff --check
```

Review checklist:

- No token/secrets in logs, docs examples, committed test fixtures, or debug output.
- No token persistence in models/migrations.
- LiveKit remains media-only; product membership comes from room/store.
- `anonymous_id` is not accepted as an authorization credential.
- Existing create/join/leave lifecycle tests still pass.
- OpenAPI matches handlers.

### 10. Trellis check

After implementation, run `trellis-check` per workflow. If it reports failures, fix and repeat until clean.

## Risk Points

- Adding credentials to create/join can break existing response tests if assertions require exact JSON shape. Prefer additive top-level fields and update test structs intentionally.
- LiveKit JWT claim field names may differ from assumptions. Tests should decode real generated tokens from the chosen SDK version rather than only mocking.
- Missing config must not lead to insecure empty-secret tokens.
- Public error messages should remain stable and not leak whether a particular member ID exists beyond needed product errors.
- Session token TTL decision (OQ1) affects long game sessions versus leak blast radius.

## Rollback Points

- After Step 2: remove `internal/session` if token format changes before HTTP integration.
- After Step 3: remove `internal/livekit` and dependency if SDK shape is incompatible.
- After Step 6: router/handler changes are the main integration risk; revert together with OpenAPI if contract changes need redesign.
