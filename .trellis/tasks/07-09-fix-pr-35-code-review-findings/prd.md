# Fix PR 35 code review findings

## Goal

Fix the verified PR #35 code-review findings so the Issue #13 credential implementation is runnable from the documented API startup path, keeps LiveKit media permissions scoped to MVP voice use, and does not normalize token-shaped plaintext in docs or test logs.

## Background

PR #35 added room session credentials, LiveKit short-lived join tokens, and a fresh LiveKit token endpoint for Issue #13. The max code review reported four verified findings, ranked by impact:

1. `services/api/cmd/api/main.go:29` — runtime credential config is wired into the router, but `config.Default()` never loads documented `ECHO_*` environment values, so the executable API returns `500 internal_error` for create/join under the documented deployment path.
2. `services/api/internal/livekit/tokens.go:42` — LiveKit tokens set `CanPublish: true` without constraining publish sources, so a participant can publish camera, screen-share, or data tracks instead of voice-only media.
3. `services/api/openapi.yaml:76` — OpenAPI examples use real-looking `eyJ...` token prefixes instead of placeholders.
4. `services/api/internal/http/handlers_credentials_test.go:201` — a failing credential assertion prints room session and LiveKit token plaintext into test logs.

## Requirements

### R1 — Runtime credential env loading

The API service must load documented environment variables into `config.Config` before wiring HTTP credential config:

- `ECHO_HTTP_ADDR`
- `ECHO_DATABASE_PATH`
- `ECHO_LIVEKIT_URL`
- `ECHO_LIVEKIT_API_KEY`
- `ECHO_LIVEKIT_API_SECRET`
- `ECHO_ROOM_SESSION_SECRET`
- `ECHO_LOG_DIR`

TTL defaults remain explicit in code for MVP:

- `RoomSessionTokenTTL = 2 * time.Hour`
- `LiveKitTokenTTL = 10 * time.Minute`

Blank credential env values must still fail through the existing safe `500 internal_error` path; the fix is to make documented non-blank env values reachable, not to add insecure defaults.

### R2 — Voice-only LiveKit publish grants

LiveKit join tokens for MVP room members must grant only the publish sources required for voice participation.

- Members can publish microphone audio.
- Members can subscribe to room media.
- Members must not receive permission to publish camera, screen-share, or data tracks through the MVP credential path.
- The token package should expose this as deterministic behavior and test it by decoding token claims.

### R3 — Token-safe documentation examples

OpenAPI credential examples must use placeholders rather than real-looking JWT or room-session-token prefixes.

- Do not use `eyJ...` token-shaped values in OpenAPI examples.
- Use clear placeholders such as `<room_session_token>` and `<livekit_token>`.

### R4 — Token-safe test failures

Credential tests must not print token plaintext on assertion failures.

- Failing assertions may report whether token fields are present/non-empty.
- Failing assertions must not include `room_session_token` or `livekit_token` values.

## Out of Scope

- Automatic token renewal beyond the existing fresh-token endpoint.
- Long-lived token persistence.
- Changing product room lifecycle semantics.
- Adding accounts, friends, TURN, Redis, PostgreSQL, frontend behavior, or deployment compose changes.
- Posting GitHub review comments.

## Acceptance Criteria

- [ ] AC1: A deterministic config test proves documented `ECHO_*` env values populate `config.Config`.
- [ ] AC2: A deterministic HTTP/startup seam test proves a router wired from env-loaded config can create a room and return credential fields instead of `500 internal_error`.
- [ ] AC3: Existing blank credential config behavior still returns `500 internal_error` before create/join mutation.
- [ ] AC4: LiveKit token tests prove `canPublishSources` is limited to microphone audio and data publishing is disabled or absent according to the SDK claim shape.
- [ ] AC5: OpenAPI examples for `room_session_token` and `livekit_token` use placeholders, not `eyJ...` token-shaped values.
- [ ] AC6: Credential test failure messages do not print token plaintext.
- [ ] AC7: `go test -count=1 ./services/api/...` passes.
- [ ] AC8: `git diff --check` passes.
- [ ] AC9: Trellis task validation passes before commit.
