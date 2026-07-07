# Fix PR 28 code review findings

## Goal

Fix the verified PR #28 code-review findings in priority order so the bootstrap workspace remains buildable from a clean checkout, keeps public API contracts in their documented source of truth, and does not lock in UI/install surfaces that violate current echo MVP rules.

## Background

PR #28 bootstraps the echo engineering workspace with independent Go modules, a Wails 3 desktop scaffold, an API scaffold, WebSocket documentation, deployment env placeholders, and Trellis/spec updates.

The `/code-review max pr #28` pass returned six verified findings:

1. **P1 contract** — `apps/desktop/main.go:13` embeds `all:frontend/dist`, while `apps/desktop/.gitignore:3` excludes `frontend/dist`. A clean checkout without a frontend build fails `go test ./apps/desktop/...` with `pattern all:frontend/dist: no matching files found`.
2. **P2 contract** — `services/api/internal/http/router.go:12` exposes `GET /healthz`, but `services/api/openapi.yaml` does not exist despite `AGENTS.md:170` and `design.md:405` requiring HTTP API contracts in `services/api/openapi.yaml`.
3. **P2 contract** — `apps/desktop/build/windows/Taskfile.yml:109` signs `build/windows/nsis/{{.APP_NAME}}-installer.exe`, while `apps/desktop/build/windows/nsis/project.nsi:75` writes `bin/${INFO_PROJECTNAME}-${ARCH}-installer.exe`.
4. **P3 conventions** — `apps/desktop/build/windows/nsis/project.nsi:68` uses English installer UI via `!insertmacro MUI_LANGUAGE "English"`, while `AGENTS.md:178` requires short Simplified Chinese UI copy except `echo` and invite codes.
5. **P3 conventions** — `apps/desktop/frontend/src/App.tsx:11` and `apps/desktop/frontend/src/App.tsx:14` contain visible English UI copy, while `AGENTS.md:178` requires short Simplified Chinese UI copy except `echo` and invite codes.
6. **P3 conventions** — `apps/desktop/frontend/public/style.css:24` uses `radial-gradient`, while `AGENTS.md:179` and `docs/ui/client-ui-spec.md:142` require no gradients / no large-area gradients.

## Requirements

### R1. Clean-checkout desktop Go compile must not require a pre-existing ignored frontend bundle

- Fix the `apps/desktop/main.go:13` embed failure without committing generated `frontend/dist` output.
- Preserve the ability for `wails3 build` to embed the real production frontend bundle after the frontend build task runs.
- Keep generated frontend assets ignored; do not turn `frontend/dist` into a committed source tree.
- The clean-checkout compile path must work before `npm run build` has created `frontend/dist`.

### R2. Public HTTP smoke route must be represented in the OpenAPI contract source of truth

- Add `services/api/openapi.yaml` as the HTTP contract owner for the bootstrap API surface.
- Include `GET /healthz` with a `200` JSON response schema containing `status: ok`.
- Do not add room, invite, member, token, persistence, WebSocket runtime, or LiveKit HTTP behavior beyond the bootstrap health route.
- Keep WebSocket message documentation separate in `docs/api/websocket.md`.

### R3. Installer signing task must target the actual NSIS output

- Update `sign:installer` so it signs the installer file actually emitted by `project.nsi`.
- Preserve support for the existing `ARCH` task variable if the surrounding Wails taskfile still exposes it.
- Do not introduce a new release pipeline or certificate requirement; only repair the existing signing task contract.

### R4. User-visible bootstrap UI and installer language must follow current Chinese copy rules

- Replace visible desktop bootstrap English copy with short Simplified Chinese copy, keeping `echo` as the allowed product-name exception.
- Update the frontend smoke test to assert the new user-visible copy instead of the old English copy.
- Change the NSIS MUI language from English to Simplified Chinese unless the installed NSIS environment proves that identifier unavailable.

### R5. Bootstrap styling must not use gradients

- Remove the `radial-gradient` body background from `apps/desktop/frontend/public/style.css`.
- Preserve a simple dark/cold-light bootstrap presentation suitable for a non-product scaffold.
- Do not redesign the MVP client or implement room UI in this task.

## Out of Scope

- Broad simplification of Wails template tasks not reported in the final findings list.
- Removing package manager branches, ARM64 branches, garble/obfuscation, MSIX templates, or unused future config fields unless required to satisfy the six accepted findings.
- Implementing product room, invite, LiveKit, WebSocket runtime, tray, device, or push-to-talk behavior.
- Adding full future OpenAPI endpoints from `implement.md`; this task only covers the bootstrap route exposed by PR #28.
- Changing dependency versions unless directly required by the fixes.

## Acceptance Criteria

- [ ] AC1: In a simulated clean checkout where `apps/desktop/frontend/dist` is absent, `go test ./apps/desktop/...` exits 0.
- [ ] AC2: `wails3 build` from `apps/desktop` exits 0 and produces `apps/desktop/bin/echo.exe` after rebuilding the frontend bundle.
- [ ] AC3: `services/api/openapi.yaml` exists and documents `GET /healthz` with a JSON `status` field.
- [ ] AC4: `go test ./services/api/...` exits 0.
- [ ] AC5: `apps/desktop/build/windows/Taskfile.yml` signs the same installer path/name emitted by `apps/desktop/build/windows/nsis/project.nsi` for the active `ARCH`.
- [ ] AC6: `apps/desktop/build/windows/nsis/project.nsi` no longer selects English as the installer MUI language.
- [ ] AC7: `npm run test:run` from `apps/desktop/frontend` exits 0 and asserts the revised Simplified Chinese bootstrap copy.
- [ ] AC8: `npm run build` from `apps/desktop/frontend` exits 0.
- [ ] AC9: A scope search finds no remaining `echo desktop bootstrap`, `Engineering skeleton ready`, `MUI_LANGUAGE "English"`, or `radial-gradient` in the touched scaffold surfaces.
