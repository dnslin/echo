# Planning evidence — Issue #15 WebSocket room state

## Sources inspected

- GitHub Issue #15 via `gh issue view 15`: requires WebSocket snapshot, member joined/left broadcasts, basic heartbeat, valid-session-only connection, credential rejection cases, and multi-client integration tests.
- GitHub Issue #14 via `gh issue view 14`: closed; delivered WebSocket contract docs and explicitly did not implement the hub.
- `prd.md`: MVP users need member lists and connection/voice state in temporary rooms.
- `design.md`: business service owns product state; LiveKit is media-only; WebSocket hub is a planned API service component.
- `implement.md`: Task 10 names `services/api/internal/ws/hub.go` and `hub_test.go`; project stack includes `coder/websocket`.
- `docs/api/websocket.md`: full MVP room-state WebSocket contract.
- `services/api/internal/http/*`: existing create/join/leave/fresh-token handlers and router option pattern.
- `services/api/internal/room/service.go`: existing create/join/leave and `AuthorizeMemberContext` behavior.
- `services/api/internal/session/token.go`: HMAC room session token sign/verify behavior.
- `services/api/internal/store/*`: existing room/member persistence, authorization reads, leave lifecycle behavior.
- `.trellis/spec/backend/*`: backend credential, database, directory, logging, and quality guidance.
- `.trellis/spec/guides/*`: cross-layer and code-reuse guidance relevant to message contracts.

## Key anchors

- Endpoint/auth contract: `docs/api/websocket.md:39-68`.
- Pre-upgrade error matrix: `docs/api/websocket.md:69-93`.
- Snapshot payload rules: `docs/api/websocket.md:222-269`.
- Join/leave messages: `docs/api/websocket.md:270-307`.
- Heartbeat behavior: `docs/api/websocket.md:478-507`, `docs/api/websocket.md:594-607`.
- Security/logging: `docs/api/websocket.md:650-657`.
- Existing HTTP credential issuance and authorization mapping: `services/api/internal/http/handlers.go:208-243`, `services/api/internal/http/handlers.go:365-392`.
- Existing room authorization: `services/api/internal/room/service.go:276-317`.
- Existing active states: `services/api/internal/room/service.go:474-476`.
- Existing room session verify: `services/api/internal/session/token.go:77-117`.
- Existing router option style: `services/api/internal/http/router.go:8-75`.
- Existing room/member schema: `services/api/internal/store/models.go:4-36`.
- Existing leave lifecycle: `services/api/internal/store/sqlite.go:229-330`.
- Current dependency gap: `services/api/go.mod:4-9` has no direct `coder/websocket` requirement yet.

## Planning conclusions

- The issue is complex enough to require `prd.md`, `design.md`, and `implement.md`.
- No parent/child split is needed: the deliverable is one independently verifiable backend feature.
- No user product decision remains: Issue #15 and the WebSocket contract define scope and exclusions.
- The implementation should reuse existing HTTP/room/session/store contracts and add a small single-instance hub rather than inventing a new product state owner.
- Tests should be Go integration tests using the real HTTP router and WebSocket clients, with short injectable heartbeat durations.
