# Issue 10 创建临时房间 API Implementation Plan

## Scope

Implement the backend-only create temporary room path for GitHub Issue #10. Work only under `services/api/**`, `services/api/openapi.yaml`, and this Trellis task directory unless a test reveals a necessary adjacent backend change.

## Ordered Checklist

### 1. Add domain and invite primitives

- Create `services/api/internal/domain/types.go` with room/member states and structs.
- Create `services/api/internal/invite/service.go`.
- Create `services/api/internal/invite/service_test.go`.
- Cover:
  - generated code length is 6;
  - generated chars are only `A-Z0-9`;
  - invalid generation length fails.

Validation:

```bash
cd services/api && go test ./internal/invite -v
```

### 2. Add SQLite persistence for rooms and members

- Create `services/api/internal/store/models.go`.
- Create `services/api/internal/store/sqlite.go`.
- Create `services/api/internal/store/sqlite_test.go`.
- Add GORM AutoMigrate for `RoomModel` and `MemberModel`.
- Add unique index on `rooms.invite_code`.
- Keep models small and aligned with Issue #10 fields.

Validation:

```bash
cd services/api && go test ./internal/store -v
```

### 3. Implement room creation service

- Create `services/api/internal/room/service.go`.
- Create `services/api/internal/room/service_test.go`.
- Implement:
  - `Create(input CreateInput) (CreateResult, error)`;
  - validation for anonymous ID, avatar ID, nickname, room name;
  - default room name when blank;
  - ID generation;
  - invite generation with 5 collision retries;
  - transaction that creates room and host member.
- Use typed/sentinel errors or validation error struct so HTTP can map messages deterministically.

Requirement-driven tests:

- Success returns active room, 6-char invite, host member.
- Empty nickname returns nickname validation error.
- Nickname > 16 runes fails.
- Room name > 24 runes fails.
- Initial room has nil `last_empty_at` and `expires_at`.
- Initial member is host, online, unmuted, not speaking, `push_to_talk`.

Validation:

```bash
cd services/api && go test ./internal/room -v
```

### 4. Add HTTP handler and router wiring

- Change `services/api/internal/http/router.go` so `NewRouter` accepts handlers or dependencies without breaking healthz tests.
- Add `services/api/internal/http/handlers.go`.
- Add or update `services/api/internal/http/router_test.go` / `handlers_test.go`.
- Keep `/healthz` behavior unchanged.
- Add `POST /v1/rooms` returning `201`.
- Return `400` with `{ "error": { "code", "message" } }` for validation failures.

Requirement-driven HTTP tests:

- Create success returns `201`, 6-char invite, host member, active room.
- Empty nickname returns `400` and `请输入昵称`.
- Nickname too long returns `400` and `昵称最多 16 个字符`.
- Room name too long returns `400` and `房间名称最多 24 个字符`.
- Healthz test still passes.

Validation:

```bash
cd services/api && go test ./internal/http -v
```

### 5. Wire API main to SQLite and room service

- Modify `services/api/cmd/api/main.go`.
- Keep config defaults.
- Open SQLite via store layer.
- Construct room service and HTTP handlers/router.
- On DB open failure, log and exit.
- Do not add environment loading unless needed by existing config tests; avoid broad deployment changes in this issue.

Validation:

```bash
cd services/api && go test ./cmd/api ./... 
```

If `go test ./cmd/api` is not a valid package target due to module layout, use `go test ./...` from `services/api`.

### 6. Update OpenAPI contract

- Expand `services/api/openapi.yaml` with:
  - `POST /v1/rooms`;
  - `CreateRoomRequest`;
  - `CreateRoomResponse`;
  - `Room`;
  - `Member`;
  - `ErrorResponse`.
- Keep `/healthz` contract.
- Ensure `400` response examples include the product messages.

Validation:

```bash
cd services/api && go test ./...
```

### 7. Final check

Run from repository root:

```bash
go test ./services/api/...
python ./.trellis/scripts/task.py current --source
git status --short
```

Then run the project Trellis check skill as requested. Any failure must be fixed and rechecked before reporting completion.

## Rollback Points

- If invite/store/room service design is wrong, revert the corresponding new package files before handler wiring.
- If router constructor breaks healthz tests, restore `/healthz` test first, then reintroduce dependencies with a compatible constructor.
- If OpenAPI becomes inconsistent with tests, prefer changing OpenAPI or response structs to match PRD/Issue requirements, not broadening scope.

## Out-of-Scope Guardrail

Do not implement:

- `POST /v1/rooms/join`;
- room session token;
- LiveKit token;
- WebSocket routes;
- leave/reconnect/expiry runtime behavior;
- desktop/frontend changes;
- deployment or CI changes.
