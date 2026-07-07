# Design: Issue #6 Bootstrap Workspace

## First-principles analysis

### Challenge assumptions

- Assumption: bootstrapping should include all files listed in the root `implement.md` target tree. This is unverified for Issue #6 because the issue explicitly says to create structure and verification commands only, not product functionality.
- Assumption: a minimal implementation means placeholder-only code. This is potentially wrong because the issue requires executable validation for Go workspace, API tests, and frontend tests.
- Assumption: frontend tests can wait until the later root implementation task for local state. This conflicts with Issue #6 acceptance, which requires a frontend test command now.
- Assumption: shared packages help monorepo bootstraps. ADR 0030/0031 reject premature shared abstractions until API payloads stabilize.
- Assumption: deployment files should include Docker Compose now. Issue #6 only requires skeleton and contract/document locations; root `implement.md` Task 1 specifically creates `deploy/server/env.example`, not the full deployment stack.

### Bedrock truths

- A Go workspace can resolve modules only after real module directories and `go.mod` files exist.
- A test command is useful only if it executes at least one test and exits non-zero on failure.
- Later work needs stable paths more than product behavior in this issue.
- Cross-module imports would couple deployable programs and violate the ADR boundary.
- Product features create state/contracts that later spike results may invalidate; Issue #6 explicitly excludes them.

### Rebuild from verified truths

1. Create the two deployable modules first: desktop and API.
2. Add root `go.work` pointing only at those modules, then verify with `go work sync`.
3. Add one public API smoke route and test because a Go module with no exercised surface does not satisfy the API test acceptance.
4. Add Vitest and one frontend smoke test because the issue requires a runnable frontend test command.
5. Add contract/deployment placeholder documents at the final paths so later issues can fill them without path churn.
6. Stop before product logic, runtime WebSocket, LiveKit, tray, keyboard hook, Docker Compose, release workflows, or shared packages.

### Contrast with convention

A conventional scaffold might either generate a large app shell with unused framework defaults or create empty placeholder directories. Both miss the core constraint: later issues need stable paths plus passing verification, not broad product behavior. The essential difference is that this task adds only mechanisms demanded by acceptance criteria and verifies each through executable commands.

### Conclusion

The correct bootstrap is a tested engineering skeleton: real desktop/API modules, root workspace, minimal public smoke surfaces, frontend/API test commands, and contract placeholder files. It should be more complete than empty directories but much smaller than the full MVP implementation.

## Architecture and boundaries

```text
/
├── apps/desktop/          Wails 3 desktop module
├── services/api/          Gin API module
├── docs/api/              API and WebSocket contracts
├── deploy/server/         Future single-server deployment examples
└── go.work                Local workspace over the two Go modules
```

Boundary rules:

- `apps/desktop` and `services/api` each own their own `go.mod`.
- Root `go.work` is local coordination only; it is not a shared package mechanism.
- The desktop module must not import `services/api/internal/*`.
- The API module must not import `apps/desktop/internal/*`.
- No `pkg/`, `shared/`, generated API client, or shared schema package is introduced in this issue.

## Desktop design

Use `wails3 init` with the React/TypeScript template to produce a real Wails 3 scaffold under `apps/desktop`.

Add test tooling inside `apps/desktop/frontend`:

- Vitest as the test runner.
- jsdom / React Testing Library only if the generated React scaffold needs DOM rendering.
- `npm run test:run` as the non-watch CI-style command.
- One smoke test that renders the public frontend entry component and verifies visible bootstrap content.

Do not implement product UI flows. If the Wails template includes sample UI, keep or reduce it only as needed for a stable smoke test; do not add room, device, LiveKit, tray, or settings flows.

## API design

Create `services/api` as a Go module. Use a small public router/handler constructor so tests can exercise the HTTP surface without starting a network listener.

Expected minimal API files:

```text
services/api/
├── go.mod
├── go.sum
├── cmd/api/main.go
└── internal/
    ├── config/config.go
    └── http/
        ├── router.go
        └── router_test.go
```

Minimal public behavior:

- `GET /healthz` returns HTTP 200 and a small JSON status body.
- `cmd/api/main.go` builds the router and serves on the configured address.
- `internal/config.Default()` centralizes bootstrap defaults such as HTTP address and future config field names.

Do not add room lifecycle, invite, LiveKit token, persistence, WebSocket hub, or deployment runtime logic.

## Contracts and placeholder docs

`docs/api/websocket.md` records the future WebSocket contract location and message names from root `design.md`:

- Endpoint: `GET /v1/rooms/{room_id}/ws`.
- Authorization: future room session token.
- Server-to-client and client-to-server message type names.

The file is documentation only in this issue; no WebSocket server is implemented.

`deploy/server/env.example` records future environment key names only. It should not imply a runnable deployment stack yet.

## Compatibility and migration

- No existing app code is present, so no runtime migration is required.
- The created paths must align with README/design/ADR expectations to avoid future path churn.
- The root branch was fetched and the task branch was created from `origin/master`; no rebase is planned inside this task unless explicitly needed.

## Tradeoffs

- Including Vitest now duplicates a later root implementation-plan task, but Issue #6 explicitly requires a frontend test command now; satisfying the issue is higher priority than the older task ordering.
- Adding only `env.example` instead of full Compose keeps this task focused on bootstrap and avoids unvalidated deployment behavior.
- A health endpoint is product-neutral and sufficient to prove the API module builds/tests without committing to room API contracts prematurely.

## Rollback

The task is additive. Rollback can remove the created bootstrap directories and `go.work`:

- `apps/desktop/`
- `services/api/`
- `docs/api/`
- `deploy/server/env.example`
- `go.work`

No database, release asset, external service, or deployment side effect is expected.
