# Repository Guidelines

## Project Overview

`echo` is a Windows 10/11 x64 lightweight voice-chat app for PC gamers. The MVP lets 2-10 invited friends join a temporary room through a 6-character invite code and use real-time voice with push-to-talk, free-talk, mute, device selection, reconnect state, and tray persistence.

Current repository state: documentation-first. Product, glossary, design, implementation plan, UI spec, and ADRs exist; application code, deploy files, API contracts, CI, and acceptance records are created later by `implement.md` tasks.

## Architecture & Data Flow

Planned runtime architecture:

```text
Windows client
  Wails 3 Go shell: window lifecycle, tray, keyboard hook, local settings, logs
  React / TypeScript / Vite / Tailwind UI: screens, room state, device controls
  LiveKit JS in WebView2: WebRTC microphone capture and remote audio playback

Public server
  external Nginx: HTTPS/WSS routing
  Gin API service: temporary rooms, invite codes, members, tokens, lifecycle
  SQLite via GORM: product room lifecycle persistence
  LiveKit: SFU media forwarding only
```

Data flow:

1. Client creates or joins a temporary room through HTTP.
2. API service validates invite codes, room capacity, expiry, anonymous identity, and membership.
3. API service returns a room session token and short-lived LiveKit token.
4. Client uses WebSocket for product room state and LiveKit for media.
5. SQLite persists room lifecycle; reconnect, speaking, WebSocket connections, and short-lived presence stay in memory for MVP.

Hard boundaries:

- Desktop and API are separate Go modules. Do not import each other's `internal` packages.
- LiveKit owns media rooms only; product rooms, invite codes, membership, lifecycle, and UI state belong to the business service.
- Do not add accounts, friends, chat, fixed rooms, auto-update, TURN, Redis, PostgreSQL, Electron, Kubernetes, or server-side audio mixing unless the product docs and ADRs change first.

## Key Directories

Current directories:

- `docs/adr/` - architecture decision records, currently ADR `0001` through `0032`.
- `docs/ui/client-ui-spec.md` - PC client UI implementation spec derived from the v4 prototype.
- `artifacts/` - generated design artifacts and prototype images.
- `.trellis/` - Trellis workflow, task, workspace, and spec scaffolding.
- `.claude/`, `.codex/`, `.pi/`, `.agents/` - AI tooling configuration and bundled skills, not echo product code.

Planned directories from `implement.md`:

- `apps/desktop/` - Wails 3 desktop app.
  - `internal/app`, `internal/config`, `internal/keyboard`, `internal/logging`, `internal/tray` for native shell behavior.
  - `frontend/src` for React UI, API client, room socket, LiveKit wrapper, state, settings, and tests.
- `services/api/` - Gin/GORM/SQLite business service.
  - `cmd/api/main.go` entrypoint.
  - `internal/domain`, `config`, `http`, `invite`, `livekit`, `room`, `session`, `store`, `ws`, `logging`.
- `deploy/server/` - Docker Compose, LiveKit config, external Nginx example, environment sample.
- `docs/api/` - HTTP/OpenAPI and WebSocket contract docs.
- `docs/spikes/` - recorded results for required media/device/keyboard spikes.
- `.github/workflows/` - CI and Windows release workflows.

## Development Commands

There are no product build commands yet until `apps/`, `services/`, and `deploy/` are created. Use these commands when the relevant files exist.

Session/context:

```powershell
python ./.trellis/scripts/get_context.py
python ./.trellis/scripts/get_context.py --mode phase
python ./.trellis/scripts/get_context.py --mode packages
```

Tool prerequisites:

```powershell
wails3 version
go version          # Go 1.22+
node --version      # Node 20+
npm --version
```

Bootstrap and workspace:

```powershell
wails3 init -n desktop -t react-ts
go mod init echo/services/api
go work sync
```

Backend:

```powershell
cd services\api
go test ./...
go test ./internal/invite -v
go test ./internal/store -v
go test ./internal/room -v
go test ./internal/http -v
go test ./internal/ws -v
```

Desktop/frontend:

```powershell
cd apps\desktop\frontend
npm install
npm run test:run

cd apps\desktop
wails3 dev
wails3 build
wails3 package
```

Deployment and release checks:

```powershell
docker compose -f deploy/server/docker-compose.yml config
```

## Code Conventions & Common Patterns

Source of truth order:

1. `CONTEXT.md` for business vocabulary.
2. `prd.md` for product scope and acceptance.
3. `docs/adr/` for architectural decisions and rationale.
4. `design.md` for technical design and boundaries.
5. `implement.md` for task order, planned files, commands, and verification.
6. `docs/ui/client-ui-spec.md` for client UI details.

Business language:

- Use `临时房间`, not fixed room/server/channel, for MVP rooms.
- `房主` is only an identity label in MVP. Do not add kick, revoke, close-room, or transfer-owner controls.
- Invite codes are 6 uppercase alphanumeric characters; input is case-insensitive and ignores spaces/hyphens.
- Anonymous identity is local to the client machine. Do not introduce login/account semantics.

Backend patterns:

- Keep domain rules explicit and testable in small packages (`invite`, `room`, `session`, etc.).
- Prefer typed errors for expected product failures such as invalid invite, room full, expired room, and invalid token.
- Use Gin for HTTP, GORM for SQLite persistence, and `coder/websocket` for WebSocket state streaming.
- Keep HTTP command APIs and WebSocket room-state events separate. HTTP contract belongs in `services/api/openapi.yaml`; WebSocket messages belong in `docs/api/websocket.md`.
- Do not log voice content, audio data, long-lived plaintext invite history, token plaintext, or sensitive request bodies.

Frontend/client patterns:

- Start with React state, context, reducers, and focused hooks. Do not pre-emptively add Redux or Zustand.
- Room state transitions must be explicit and testable. Use reducers for WebSocket event application.
- Keep LiveKit media concerns in a dedicated wrapper; the Go shell does not capture, mix, encode, decode, or play voice audio.
- UI copy is short Simplified Chinese. `echo` and invite codes are allowed exceptions.
- Follow `docs/ui/client-ui-spec.md`: cold light consumer style, cobalt accent, no gradients, no purple glow, no recent rooms, no chat, no member volume sliders, no member action menu for MVP.

Execution order:

- Work in `implement.md` order.
- Do not start full feature implementation until the three required spikes are recorded: Wails/WebView2/LiveKit audio, device/tray behavior, foreground-game push-to-talk.
- Keep commits small; each implementation task defines its own verification and commit checkpoint.

## Important Files

- `README.md` - current repo status, doc map, target architecture, contribution rules.
- `CONTEXT.md` - glossary and forbidden vocabulary drift.
- `prd.md` - product requirements, flows, error copy, MVP scope, acceptance metrics.
- `design.md` - architecture, module boundaries, data flow, persistence, API/WebSocket, deployment, testing strategy.
- `implement.md` - authoritative implementation sequence and planned commands.
- `docs/ui/client-ui-spec.md` - client UI tokens, screens, components, states, Chinese copy, accessibility requirements.
- `docs/adr/0001-0032-*.md` - architectural decisions. Read the relevant ADR before changing a decision.
- `.trellis/workflow.md` - Plan/Execute/Finish workflow and task artifact rules.
- `.trellis/scripts/get_context.py` - session/workflow/spec context loader.

Future important files once implemented:

- `apps/desktop/main.go` - Wails desktop entrypoint.
- `apps/desktop/frontend/src/app/App.tsx` - desktop UI root.
- `apps/desktop/frontend/src/state/roomReducer.ts` - WebSocket room-state reducer.
- `apps/desktop/frontend/src/state/voiceState.ts` - voice send/mute/mode state rules.
- `apps/desktop/frontend/src/livekit/livekitClient.ts` - LiveKit client integration.
- `services/api/cmd/api/main.go` - API service entrypoint.
- `services/api/openapi.yaml` - HTTP API contract.
- `services/api/internal/room/service.go` - room lifecycle service.
- `services/api/internal/ws/hub.go` - WebSocket room state hub.
- `deploy/server/docker-compose.yml` - single-server API/LiveKit deployment.

## Runtime/Tooling Preferences

- Primary platform: Windows 10/11 x64.
- Desktop shell: Wails 3. Electron is not a fallback.
- Backend: Go 1.22+, Gin, GORM, SQLite.
- Frontend: React, TypeScript, Vite, Tailwind CSS, npm.
- Media: LiveKit JS in WebView2; self-hosted LiveKit server for SFU forwarding.
- Deployment: Docker Compose on one public server, behind existing external Nginx.
- Release: GitHub Actions builds Windows installer and publishes GitHub Releases. No app auto-update in MVP.
- Trellis: complex implementation work should have task artifacts and follow `.trellis/workflow.md`. Current `.trellis/spec/*` package guidelines are mostly templates; rely on concrete project docs until filled.

## Testing & QA

Current state: product tests are not runnable yet because app source does not exist. Testing expectations are planned in `design.md`, `implement.md`, and ADR `0029`.

Automated tests once code exists:

- Backend Go package tests for invite normalization/generation, SQLite models, room lifecycle, session tokens, LiveKit token issuance, HTTP handlers, and WebSocket hub.
- Frontend Vitest/jsdom tests for voice-state rules, settings persistence helpers, API client behavior, and room reducer event handling.
- CI should run `go test ./services/api/...` and `npm run test:run` under `apps/desktop/frontend`.

Manual/HITL acceptance is required for desktop/media behavior:

- Two users join one temporary room and hear each other.
- 3-10 user room works; 11th user gets clear rejection.
- Microphone/output device switching and input level meter work.
- Push-to-talk recognizes 10 consecutive press/release cycles while a game is foregrounded.
- Free-talk only starts after explicit user action.
- Mute blocks sending.
- Closing the window sends the app to tray without interrupting voice.
- Reconnect succeeds within 30 seconds and fails clearly after 30 seconds.
- Empty room expires after 30 minutes.
- GitHub Releases installer installs and starts on Windows x64.

Verification rule: run the specific command or manual scenario for the task before calling it complete. If a command fails, fix the cause before moving on.

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues for `dnslin/echo` through `gh`. See `docs/agents/issue-tracker.md`.

### Domain docs

This repository uses a single-context layout: root `CONTEXT.md` plus `docs/adr/`. See `docs/agents/domain.md`.

<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->
