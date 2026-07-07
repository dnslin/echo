# Issue #6 Evidence Summary

## GitHub issue

- Number: #6
- Title: `[S01] 初始化工作区、工具链和契约骨架`
- State: open
- Parent: #5
- Labels: `priority:p0`, `mode:afk`, `area:desktop`, `area:docs`, `area:api`, `type:feature`, `epic:echo-risk-validation`

## Issue objective

Build the minimum working engineering skeleton so later issues have clear landing zones. The task creates structure and verification commands only; it does not implement product behavior.

## Issue acceptance

- Go workspace resolves desktop and API modules.
- Frontend test command exists and can run a minimal test.
- API service module can run a minimal Go test.
- WebSocket contract documentation location exists for later work.
- README/PRD/design/implement target structure is not contradicted by implementation paths.

## Issue boundaries

- Do not implement room, voice, tray, or deployment release behavior.
- Do not introduce shared package abstractions that conflict with root `design.md`.
- Do not implement unvalidated spike solutions early.

## Repository evidence

- `README.md` expects `apps/desktop`, `services/api`, `deploy/server`, `docs/api`, and `.github/workflows` as later landing zones.
- `design.md` requires independent desktop/API Go modules coordinated by a root `go.work`.
- `implement.md` Task 1 describes bootstrap files and validation commands; Issue #6 additionally requires a runnable frontend test command now.
- `docs/adr/0030-monorepo-apps-services-structure.md` fixes the monorepo layout.
- `docs/adr/0031-independent-go-modules-with-workspace.md` fixes independent Go modules plus root workspace and forbids premature shared packages.
- `docs/adr/0032-openapi-http-websocket-contract-docs.md` fixes `services/api/openapi.yaml` for HTTP later and `docs/api/websocket.md` for WebSocket messages.

## Local toolchain evidence

Collected on 2026-07-07:

- Go: `go1.24.5 windows/amd64`
- Node: `v22.17.0`
- npm: `10.9.2`
- Wails: `v3.0.0-alpha2.115`
