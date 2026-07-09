# Design — Fix PR 35 code review findings

## First-Principles Analysis

### Challenge assumptions

- Assumption: `config.Default()` is enough for an executable service. This is false once credential fields are mandatory and deployment docs expose `ECHO_*` env keys.
- Assumption: `CanPublish: true` means voice-only publishing because echo is a voice app. This is false; the LiveKit grant controls media capabilities and must encode the actual permission boundary.
- Assumption: truncated `eyJ...` examples are harmless. This is risky because the project credential spec forbids real-looking reusable token examples in docs.
- Assumption: tests can print response bodies freely. This is false for bearer secrets; test logs are still logs.

### Bedrock truths

- The running API gets configuration from process state and code. If code never reads the environment, documented env values cannot affect runtime behavior.
- Create/join now require non-blank LiveKit URL/key/secret and room session secret before mutating product state.
- A LiveKit token is the actual media authorization artifact. Any grant not encoded into the token is not enforced by product intent.
- Bearer tokens are secrets whether produced in production or tests; logs and docs can persist and spread them.

### Rebuild from ground up

1. Runtime startup must construct `config.Config` from defaults plus explicit environment overrides.
2. HTTP startup should pass only already-loaded config values into `httpapi.CredentialConfig`.
3. LiveKit token construction should explicitly set the minimal MVP media permission: room join, subscribe, microphone publish only, no data publish.
4. Public docs should show placeholder token strings, not token-like byte patterns.
5. Test assertions should verify presence and scope without printing token values.

### Contrast with convention

A conventional shortcut would add test-only config values or accept `CanPublish: true` as sufficient because many examples do that. The fundamental credential boundary is stricter: runtime config must be reachable in the executable path, and token claims must encode the exact permission the product intends.

### Conclusion

The smallest correct fix is to add env loading at the config owner, constrain LiveKit publish claims in the token owner, and remove token-shaped values from documentation/test failure text.

## Technical design

### Config loading

Add a config owner function under `services/api/internal/config`:

```go
func FromEnv() Config
```

`FromEnv` starts from `Default()` and overlays non-empty documented env keys:

- `ECHO_HTTP_ADDR` -> `HTTPAddr`
- `ECHO_DATABASE_PATH` -> `DatabasePath`
- `ECHO_LIVEKIT_URL` -> `LiveKitURL`
- `ECHO_LIVEKIT_API_KEY` -> `LiveKitAPIKey`
- `ECHO_LIVEKIT_API_SECRET` -> `LiveKitAPISecret`
- `ECHO_ROOM_SESSION_SECRET` -> `RoomSessionSecret`
- `ECHO_LOG_DIR` -> `LogDir`

`cmd/api/main.go` uses `config.FromEnv()` instead of `config.Default()`.

No environment variables are added for TTL in this fix; TTL defaults remain explicit and already covered by Issue #13 tests.

### Startup feedback loop

Expose a small constructor seam in `cmd/api/main.go` so tests can verify startup wiring without starting a network listener:

```go
func newRouter(cfg config.Config, db *gorm.DB) http.Handler
```

This keeps tests at the correct seam: config value -> main wiring -> router -> create-room handler.

### LiveKit media grant

Update `services/api/internal/livekit.JoinToken` to set:

- `CanPublish: true`
- `CanSubscribe: true`
- `CanPublishData: false`
- `CanPublishSources: []string{<microphone source>}`

Use the LiveKit protocol constant for microphone source if available in the imported SDK; otherwise use the SDK's string representation that appears in JWT claims. Tests decode the JWT and assert the claim shape.

### Docs and logs

- Replace OpenAPI token-like examples with placeholders.
- Change credential assertion failures to report booleans or field names only, not token values.

## Compatibility

- Existing API success fields do not change.
- Existing error envelope does not change.
- Blank credential config still fails safely before create/join mutation.
- No database schema changes.

## Risks

- LiveKit claim JSON key for publish sources must match the SDK output. Mitigate with token package tests that decode the generated JWT.
- Startup seam must not duplicate production wiring. Keep `main()` and tests calling the same `newRouter` helper.
