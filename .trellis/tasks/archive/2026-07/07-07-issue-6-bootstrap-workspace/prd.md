# [S01] 初始化工作区、工具链和契约骨架

## Goal

Create the repository bootstrap for GitHub Issue #6 so later echo MVP work has stable landing zones for the Wails desktop app, Gin API service, frontend tests, Go workspace coordination, deployment examples, and WebSocket contract documentation.

This task is complete only when the skeleton is usable by later issues and validated by executable commands. It must not implement product behavior such as rooms, voice, tray lifecycle, release publishing, or deployment runtime.

## Background and Evidence

- GitHub Issue #6 is part of epic #5 and is titled `[S01] 初始化工作区、工具链和契约骨架`.
- `README.md` defines the expected top-level structure: `apps/desktop`, `services/api`, `deploy/server`, `docs/api`, and `.github/workflows` as future implementation areas.
- Root `design.md` requires independent Go modules for desktop and API, coordinated by a root `go.work`; desktop and API must not import each other's internal packages.
- ADR evidence:
  - `docs/adr/0030-monorepo-apps-services-structure.md` fixes `apps/desktop`, `services/api`, and `deploy/server` as the monorepo layout.
  - `docs/adr/0031-independent-go-modules-with-workspace.md` requires separate Go modules plus a root workspace.
  - `docs/adr/0032-openapi-http-websocket-contract-docs.md` requires HTTP OpenAPI and separate WebSocket message documentation.
- Local toolchain evidence on 2026-07-07:
  - Go: `go1.24.5 windows/amd64`.
  - Node: `v22.17.0`.
  - npm: `10.9.2`.
  - Wails: `v3.0.0-alpha2.115`.
- Branch setup: `issue-6-bootstrap-workspace` was created from `origin/master` after a retry succeeded.

## Requirements

### R1. Monorepo directories

Create only the directories required for the bootstrap landing zones:

- `apps/desktop/` for the Wails 3 desktop app.
- `services/api/` for the Gin API service.
- `docs/api/` for API/WebSocket contracts.
- `deploy/server/` for future single-server deployment examples.

### R2. Independent Go workspace

Create a root `go.work` that includes exactly the desktop and API Go modules created by this task:

- `./apps/desktop`
- `./services/api`

The desktop module and API module must remain independent. Do not introduce shared Go packages or generated clients in this task.

### R3. Desktop scaffold and frontend test command

Create a Wails 3 React/TypeScript desktop scaffold under `apps/desktop/` and add frontend test tooling that can run a minimal requirement-driven smoke test through the public UI/component entry point.

The test command must be documented and runnable from `apps/desktop/frontend`.

### R4. API scaffold and Go test command

Create `services/api` as a Go module with a minimal API entry point and configuration skeleton. The module must expose a minimal public HTTP surface suitable for a smoke test, such as `GET /healthz`, without implementing room, invite, member, token, WebSocket runtime, persistence, or LiveKit behavior.

Add a Go test that proves the API module can run tests through its public router/handler surface.

### R5. Contract and deployment placeholders

Create the contract and deployment placeholder files needed by later issues:

- `docs/api/websocket.md` with endpoint location and concrete message names from root `design.md`.
- `deploy/server/env.example` with environment variable names needed by the future API and LiveKit deployment path.

Do not implement Docker Compose, LiveKit config, Nginx config, or release workflows in this task unless required to keep the created placeholder valid.

### R6. Respect MVP boundaries

This bootstrap must not implement:

- Room creation, join, leave, expiry, member state, or invite code logic.
- Real WebSocket server runtime.
- LiveKit token signing or media integration.
- Tray behavior, keyboard hooks, device selection, or product UI flows.
- Docker Compose runtime, GitHub Release publishing, CI release workflows, or product deployment.
- Shared package abstractions that conflict with ADR 0030/0031.
- Spike conclusions that have not yet been validated by later tasks.

## Acceptance Criteria

- [ ] AC1: `go work sync` exits 0 from the repository root.
- [ ] AC2: `go test ./services/api/...` exits 0 and includes at least one API module smoke test.
- [ ] AC3: `npm run test:run` exits 0 from `apps/desktop/frontend` and includes at least one frontend smoke test.
- [ ] AC4: A Wails desktop scaffold exists at `apps/desktop` with its own `go.mod` and frontend `package.json`.
- [ ] AC5: `docs/api/websocket.md` exists and lists the WebSocket endpoint plus the server-to-client and client-to-server message names from root `design.md`.
- [ ] AC6: `deploy/server/env.example` exists and names the future API, database, LiveKit, session secret, and log directory configuration keys.
- [ ] AC7: The implementation does not add product features or cross-module shared packages excluded by R6.
- [ ] AC8: Root `README.md`, `prd.md`, `design.md`, and `implement.md` target structures are not contradicted by the created paths.
- [ ] AC9: `trellis-check` passes after implementation; any failure is fixed and re-run until passing.

## Test Scenarios

1. Workspace resolution: run root workspace sync after both Go modules exist.
2. API happy path: exercise `GET /healthz` through the API public router/handler and expect a healthy response.
3. API command availability: run all API module tests with `go test ./services/api/...`.
4. Frontend happy path: render the desktop frontend smoke entry point and assert visible echo bootstrap content.
5. Frontend command availability: run `npm run test:run` from `apps/desktop/frontend`.
6. Boundary check: inspect the diff for absence of rooms, invite logic, LiveKit runtime, WebSocket runtime, tray, keyboard hook, deployment runtime, and shared package abstractions.

## Out of Scope

All product behavior after scaffolding is out of scope for this task. Later issues own the technical spikes, room lifecycle, LiveKit integration, WebSocket runtime, settings, tray lifecycle, deployment stack, CI/release workflows, and manual Windows media acceptance.

## Open Questions

None. The repository documents and Issue #6 provide enough scope for planning.
