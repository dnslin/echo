# Directory Structure

> How backend code is organized in this project.

---

## Scenario: API module and root Go workspace bootstrap

### 1. Scope / Trigger

- Trigger: creating or modifying the echo API service module, root Go workspace, or bootstrap API command.
- Applies to `services/api/**`, root `go.work`, and root `go.work.sum`.
- This is a code-spec because Go module/workspace files and API command paths are executable contracts used by build and test commands.

### 2. Signatures

- Root workspace file:

```go
// go.work
use (
	./apps/desktop
	./services/api
)
```

- API module path:

```text
services/api/go.mod module echo/services/api
```

- API command entrypoint:

```text
services/api/cmd/api/main.go
```

- Runtime config loader:

```go
func Default() Config
func FromEnv() Config
```

- Bootstrap HTTP route:

```http
GET /healthz -> 200 application/json
```

### 3. Contracts

- `go.work` must include only deployable Go modules that exist in the repository.
- `services/api` must be an independent Go module; it must not import `apps/desktop/internal/*`.
- API code that needs an executable smoke surface should expose a public router/handler constructor instead of starting a network listener in tests.
- Bootstrap config field names and `FromEnv()` overlays must match the deployment env keys:
  - `ECHO_HTTP_ADDR`
  - `ECHO_DATABASE_PATH`
  - `ECHO_LIVEKIT_URL`
  - `ECHO_LIVEKIT_API_KEY`
  - `ECHO_LIVEKIT_API_SECRET`
  - `ECHO_ROOM_SESSION_SECRET`
  - `ECHO_WS_ORIGIN_PATTERNS`
  - `ECHO_LOG_DIR`
- `Default()` owns safe local defaults, bounded WebSocket origin defaults, and explicit TTL defaults. `FromEnv()` starts from `Default()` and overlays non-empty env values. Runtime `cmd/api` startup must use `FromEnv()`, not raw `Default()`, when wiring externally documented config into handlers.
- If a dependency requires a newer Go version, keep module/workspace `go` directives aligned with the resolved dependency/toolchain and validate with the effective Go toolchain reported by `go env GOVERSION`.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| `go.work` references a missing module | Fix the path before merging; `go work sync` must pass. |
| API imports desktop internals | Reject the change; modules must remain independently deployable. |
| Smoke route test starts a real listener | Replace with router-level test through `httptest`. |
| Dependency requires newer Go than local binary | Document/verify Go auto toolchain behavior; do not silently lower `go` directive below dependency requirements. |
| Env key appears in docs but not config skeleton | Add the matching config field or remove the premature env key. |
| Documented env key exists but `FromEnv()` does not load it | Add the env overlay and a deterministic `t.Setenv` test. |
| Runtime startup wires credential/config-dependent handlers from raw `Default()` only | Change startup to use `FromEnv()` and add a router-level startup test that proves env-backed requests reach the success path. |

### 5. Good/Base/Bad Cases

- Good: `go work sync` passes and `go test ./services/api/...` exercises `GET /healthz` through the router.
- Base: API module has no product endpoints yet, but it has a healthy smoke route and config skeleton.
- Bad: root workspace points at a non-existent module, or API tests pass only because no tests exist.

### 6. Tests Required

- `go work sync` from repository root.
- `go test ./services/api/...` from repository root.
- API smoke test assertion points:
  - request method/path is `GET /healthz`;
  - status is `200 OK`;
  - JSON body includes `status: "ok"`.
- Runtime config tests when env-backed fields are used by handlers:
  - `FromEnv()` loads each documented `ECHO_*` key through `t.Setenv`;
  - TTL defaults remain explicit after env loading;
  - a router-level startup seam test proves env-loaded credential config reaches create/join handlers without starting a real listener.

### 7. Wrong vs Correct

#### Wrong

```go
func TestAPIStarts(t *testing.T) {
	go main()
	// sleeps and calls localhost:8080
}
```

Why wrong: it couples tests to a port, process lifecycle, and environment timing.

#### Correct

```go
router := httpapi.NewRouter()
request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
response := httptest.NewRecorder()
router.ServeHTTP(response, request)
```

Why correct: the test exercises the public HTTP surface without external side effects.
