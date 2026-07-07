# Implementation Plan: Issue #6 Bootstrap Workspace

## Execution rules

- Use vertical TDD slices: write or expose one behavior test, see it fail when practical, implement only enough scaffold for that behavior, then run the relevant command.
- Do not implement product features excluded by the task PRD.
- Keep modules independent; do not add shared packages.
- Prefer generated Wails scaffold for framework boilerplate, then add the smallest verified smoke test needed by acceptance criteria.
- If an external command fails from a transient environment problem, capture the error, retry once, and document any fallback.

## Ordered checklist

### 1. Preflight and scaffold desktop module

- [ ] Verify toolchain versions: `go version`, `node --version`, `npm --version`, `wails3 version`.
- [ ] Run Wails 3 React/TypeScript scaffold into `apps/desktop`.
- [ ] Confirm `apps/desktop/go.mod` and `apps/desktop/frontend/package.json` exist.

Validation after this slice:

```bash
cd apps/desktop
go test ./...
cd frontend
npm install
npm run build
```

Use the actual generated scripts if the Wails template names differ.

### 2. Add API module smoke surface using TDD

- [ ] Create `services/api/go.mod` with required bootstrap dependencies.
- [ ] Add a failing smoke test for `GET /healthz` through a public router/handler constructor.
- [ ] Implement the router/handler and `cmd/api/main.go` so the test passes.
- [ ] Add `internal/config/config.go` with defaults and future config field names.

Validation:

```bash
go test ./services/api/...
```

### 3. Add root Go workspace

- [ ] Create `go.work` including `./apps/desktop` and `./services/api`.
- [ ] Run workspace sync.

Validation:

```bash
go work sync
go test ./services/api/...
```

### 4. Add frontend test command using TDD

- [ ] Add Vitest and required DOM test dependencies to `apps/desktop/frontend`.
- [ ] Add `test:run` script.
- [ ] Add one smoke test for the public frontend entry component.
- [ ] Adjust the generated entry component only as needed for stable bootstrap content.

Validation:

```bash
cd apps/desktop/frontend
npm run test:run
```

### 5. Add contract and deployment placeholders

- [ ] Create `docs/api/websocket.md` with endpoint and message names from root `design.md`.
- [ ] Create `deploy/server/env.example` with future API/database/LiveKit/session/log env keys.
- [ ] Inspect diff to ensure no runtime WebSocket, LiveKit, room, tray, keyboard, Docker Compose, or shared package implementation was introduced.

Validation:

```bash
test -f docs/api/websocket.md
test -f deploy/server/env.example
```

### 6. Final verification

Run all requirement commands:

```bash
go work sync
go test ./services/api/...
cd apps/desktop/frontend
npm run test:run
```

If Wails scaffold supports a non-interactive build in the local environment, also run:

```bash
cd apps/desktop
wails3 build
```

Then run Trellis quality gate:

```bash
# Use the trellis-check skill/sub-agent per workflow context.
```

Fix any failure and repeat until green.

## Review gates before `task.py start`

- [ ] `prd.md` has converged and contains no duplicate brainstorm leftovers.
- [ ] `design.md` and this `implement.md` exist because this is a complex task.
- [ ] `implement.jsonl` and `check.jsonl` contain real spec/research entries.
- [ ] User has reviewed or explicitly approved starting implementation.

## Risk points

- Wails 3 alpha template output may differ from root `implement.md` examples; prefer the generated structure unless it violates the required landing paths.
- npm dependency installation may update lockfiles substantially; keep those changes scoped under `apps/desktop/frontend`.
- Frontend smoke tests must target observable rendered behavior, not internal implementation details.
- API tests must exercise the public router/handler surface, not private helper internals.

## Rollback points

- After desktop scaffold: remove `apps/desktop` if scaffold generation is wrong.
- After API scaffold: remove `services/api` if module layout is wrong.
- After workspace creation: remove `go.work` if module paths are wrong.
- Before final verification: inspect diff for out-of-scope feature creep and revert any product behavior.
