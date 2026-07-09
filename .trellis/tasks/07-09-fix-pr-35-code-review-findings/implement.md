# Implement — Fix PR 35 code review findings

## Feedback loops

1. Config/startup loop:
   - Add config env-loading tests that set `ECHO_*` values with `t.Setenv` and assert `config.FromEnv()` returns them.
   - Add a `cmd/api` router-wiring test that passes env-loaded config into the same startup helper used by `main()` and verifies `POST /v1/rooms` returns 201 with credential fields.

2. LiveKit grant loop:
   - Extend LiveKit token tests to decode JWT claims and assert microphone-only publish sources and no data publish permission.

3. Token-safe docs/log loop:
   - Search OpenAPI/docs for `eyJ` token-shaped examples and ensure credential examples use placeholders.
   - Update credential test assertions so failures do not include token plaintext.

## Reproduction expectations before fix

- `config.FromEnv` test does not exist and would fail if written because only `Default()` exists.
- Startup seam using env-loaded config cannot be written cleanly until `cmd/api` exposes a router helper; current `main()` uses `config.Default()` directly.
- LiveKit token claim test for microphone-only publish sources fails because `CanPublishSources` and `CanPublishData` are not set.
- `grep eyJ services/api/openapi.yaml` finds token-shaped examples.
- `handlers_credentials_test.go` includes a `t.Fatalf` that prints token values.

## Ranked hypotheses

1. If runtime config loading is the P1 cause, then adding `config.FromEnv()` and using it from `main()` will make an env-backed create-room startup test pass.
2. If LiveKit publish grant is too broad, then adding `CanPublishSources` for microphone and disabling data publish will make decoded JWT assertions pass.
3. If token-shaped docs are the P3 cause, then replacing examples with placeholders will make `grep eyJ services/api/openapi.yaml` return no credential examples.
4. If test logs expose token plaintext, then changing assertion messages to booleans/field names will remove token values from failure messages without weakening assertions.

## Ordered implementation checklist

1. Update `services/api/internal/config/config.go`:
   - add env key constants if helpful;
   - add `FromEnv()` overlaying non-empty documented env values onto `Default()`.

2. Update config tests:
   - keep TTL default test;
   - add `FromEnv` env override test for documented keys;
   - assert TTL defaults survive env loading.

3. Refactor API startup:
   - move router construction into a small helper reused by `main()` and tests;
   - make `main()` call `config.FromEnv()`.

4. Add startup wiring test under `services/api/cmd/api`:
   - use temp SQLite DB;
   - set credential config through env-loaded config or direct config;
   - call `POST /v1/rooms` through `httptest`;
   - assert 201 and non-empty credential fields without logging token values.

5. Update `services/api/internal/livekit/tokens.go`:
   - set `CanPublishData` false;
   - restrict `CanPublishSources` to microphone audio.

6. Update LiveKit token tests:
   - assert `canPublishSources` contains only microphone;
   - assert data publish is false or absent only if SDK omits false values; prefer asserting explicit false if present in JWT.

7. Update token-safe docs and tests:
   - replace OpenAPI `eyJ...` credential examples with `<room_session_token>` and `<livekit_token>` placeholders;
   - change credential assertion failure message to avoid printing token values.

8. Run verification:
   - targeted tests first:
     - `go test -count=1 ./services/api/internal/config ./services/api/internal/livekit ./services/api/internal/http ./services/api/cmd/api`
   - full backend:
     - `go test -count=1 ./services/api/...`
   - token-shaped examples:
     - search `services/api/openapi.yaml` for `eyJ`
   - whitespace:
     - `git diff --check`
   - Trellis validation:
     - `python ./.trellis/scripts/task.py validate 07-09-fix-pr-35-code-review-findings`

## Rollback points

- Config loading is isolated to `internal/config` and `cmd/api` startup helper.
- LiveKit grant changes are isolated to `internal/livekit` and tests.
- OpenAPI/test log changes are mechanical and reversible.
