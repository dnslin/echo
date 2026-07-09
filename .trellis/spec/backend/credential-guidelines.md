# Credential Guidelines

> Room session token and LiveKit token contracts for the echo API service.

---

## Scenario: Room session credentials and LiveKit join tokens

### 1. Scope / Trigger

- Trigger: adding or modifying room session tokens, LiveKit token issuance, credential-bearing room API responses, WebSocket room-session handshakes, or credential authorization endpoints under `services/api/**`.
- Applies to `services/api/internal/session/**`, `services/api/internal/livekit/**`, credential fields and credential-bearing routes in `services/api/internal/http/**`, credential config in `services/api/internal/config/**`, product-member authorization in `services/api/internal/room/**` and `services/api/internal/store/**`, room-state WebSocket code in `services/api/internal/ws/**`, and `services/api/openapi.yaml`.
- Echo MVP uses the business service as the authority for product-room membership. LiveKit only receives short-lived media join tokens after product membership has been validated.

### 2. Signatures

Room session token package:

```go
const CurrentVersion = 1
const DefaultTTL = 2 * time.Hour

type Claims struct {
	Version   int       `json:"version"`
	RoomID    string    `json:"room_id"`
	MemberID  string    `json:"member_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SignInput struct {
	Secret   string
	RoomID   string
	MemberID string
	Now      time.Time
	TTL      time.Duration
}

type VerifyInput struct {
	Secret string
	Token  string
	Now    time.Time
}

func Sign(input SignInput) (string, Claims, error)
func SignClaims(secret string, claims Claims) (string, error)
func Verify(input VerifyInput) (Claims, error)
```

LiveKit token package:

```go
const DefaultTokenTTL = 10 * time.Minute

type JoinTokenInput struct {
	APIKey    string
	APISecret string
	RoomName  string
	Identity  string
	Name      string
	ValidFor  time.Duration
}

func JoinToken(input JoinTokenInput) (string, error)
```

Room service authorization:

```go
type AuthorizeMemberInput struct {
	RoomID   string
	MemberID string
}

type AuthorizeMemberResult struct {
	Room   domain.Room
	Member domain.Member
}

func (s *Service) AuthorizeMemberContext(ctx context.Context, input AuthorizeMemberInput) (AuthorizeMemberResult, error)
```

Store read methods for authorization:

```go
func (r *Repository) FindRoomByID(ctx context.Context, roomID string) (domain.Room, error)
func (r *Repository) FindMemberByRoomAndID(ctx context.Context, roomID string, memberID string) (domain.Member, error)
```

HTTP credential config and response fields:

```go
type CredentialConfig struct {
	LiveKitURL          string
	LiveKitAPIKey       string
	LiveKitAPISecret    string
	RoomSessionSecret   string
	RoomSessionTokenTTL time.Duration
	LiveKitTokenTTL     time.Duration
	Now                 func() time.Time
}

type createRoomResponse struct {
	Room             roomResponse   `json:"room"`
	Member           memberResponse `json:"member"`
	RoomSessionToken string         `json:"room_session_token"`
	LiveKitURL       string         `json:"livekit_url"`
	LiveKitToken     string         `json:"livekit_token"`
}

type liveKitTokenResponse struct {
	LiveKitURL   string `json:"livekit_url"`
	LiveKitToken string `json:"livekit_token"`
}
```

Fresh LiveKit token endpoint:

```http
POST /v1/rooms/{room_id}/livekit-token
Authorization: Bearer <room_session_token>
```

### 3. Contracts

- `POST /v1/rooms` and `POST /v1/rooms/join` success responses include the existing `room` and `member` objects plus top-level `room_session_token`, `livekit_url`, and `livekit_token`.
- Room session tokens are HMAC-SHA256 bearer credentials signed by the API service. They must contain version, room ID, member ID, and expiry.
- Room session token default TTL is 2 hours. LiveKit token default TTL is 10 minutes.
- Credential TTLs belong in config (`RoomSessionTokenTTL`, `LiveKitTokenTTL`); handlers pass configured values into token packages instead of hard-coding them locally.
- LiveKit tokens use `github.com/livekit/protocol/auth` and grant only the media permissions needed for an MVP voice participant:
  - `RoomJoin: true`
  - `Room: domain.Room.LiveKitRoomName`
  - `Identity: domain.Member.LiveKitIdentity`
  - `Name: domain.Member.Nickname`
  - `CanPublish: true`
  - `CanSubscribe: true`
  - `CanPublishData: false`
  - `CanPublishSources: [microphone]`
- Do not grant LiveKit admin, SIP, agent dispatch, room-management, data publishing, camera publishing, screen-share publishing, or unrelated permissions for the MVP member join path.
- `POST /v1/rooms/{room_id}/livekit-token` verifies the bearer room session token, checks that token room matches the path room, then loads product room/member state through the room service and store.
- `GET /v1/rooms/{room_id}/ws?token=<room_session_token>` verifies the query room session token because browser/WebView2 WebSocket handshakes cannot attach arbitrary `Authorization` headers.
- WebSocket room-session handshakes must apply the same token verification rules as bearer-token credential endpoints: verify signature, version, expiry, non-empty room/member claims, and path-room match before trusting client payloads.
- WebSocket identity must derive from verified token claims plus persisted product room/member state only. Do not accept body fields, client-sent member IDs, `anonymous_id`, or LiveKit participant presence as authorization.
- WebSocket router/middleware code must redact the `token` query parameter from Gin context/recovery/log surfaces while still passing the original request or extracted token to the authentication logic.
- A member is eligible for fresh LiveKit token issuance or room-state WebSocket connection only when the room is active and the member state is `online` or `reconnecting`.
- `anonymous_id` is never sufficient authorization for credential issuance or room-state WebSocket access. Product authorization is `room_session_token -> room_id/member_id -> persisted room/member state`.
- Credential config must be complete before create/join mutates product state. If LiveKit URL/key/secret or room session secret is missing, return `500 internal_error` before creating a room or adding a member.
- Do not persist room session tokens or LiveKit tokens in SQLite.
- Do not log token plaintext, API secrets, room session secrets, sensitive request bodies, or audio data.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Room session signing input has blank secret, room ID, member ID, or non-positive TTL | Return `session.ErrInvalidToken`; do not issue a token. |
| Room session token is blank, malformed, invalid base64, invalid JSON, unsigned/tampered, wrong-secret, unsupported version, or missing required claims | Return `session.ErrInvalidToken`; HTTP maps to `401 invalid_room_session`. |
| Room session token expiry is not after verification time | Return `session.ErrExpiredToken`; HTTP maps to `401 room_session_expired`. |
| Bearer header is missing or not `Bearer <token>` | Return `401 invalid_room_session`. |
| Token `room_id` does not match path `{room_id}` | Return `403 room_session_mismatch`; do not load or issue a LiveKit token. |
| Credential config is incomplete for create/join/fresh token | Return `500 internal_error` without exposing which secret value is missing. Create/join must not mutate room/member state. |
| Room authorization repository is unavailable | Return `500 internal_error`. |
| Persisted room is missing | Return/map `room.ErrRoomNotFound` to `404 room_not_found`. |
| Persisted room state is `expired` | Return/map `room.ErrRoomExpired` to `410 room_expired`. |
| Persisted member is missing | Return/map `room.ErrMemberNotFound` to `404 member_not_found` for existing member lookup paths, or credential-specific inactive rejection when appropriate. |
| Persisted member state is `disconnected` or otherwise not `online`/`reconnecting` | Return/map `room.ErrMemberNotActive` to `403 member_not_active`. |
| LiveKit token input has blank key/secret/room/identity or non-positive TTL | Return `livekit.ErrInvalidInput`; HTTP maps to `500 internal_error`. |
| LiveKit token generation succeeds | Return token only in HTTP response body; do not log or store it. |

### 5. Good/Base/Bad Cases

- Good: create-room handler validates JSON, preflights credential config, calls room service, signs a room session token for the returned room/member, issues a LiveKit token for `LiveKitRoomName` + `LiveKitIdentity`, and returns all fields in one response.
- Good: fresh-token handler verifies `Authorization: Bearer`, rejects room mismatch, authorizes the member through persisted product state, then issues a new 10-minute LiveKit token.
- Base: token packages are small and deterministic; tests inject `Now` instead of sleeping.
- Bad: handler creates a room before discovering credential secrets are missing.
- Bad: a fresh LiveKit token endpoint accepts `anonymous_id`, `member_id` body fields, or LiveKit participant presence as proof of membership.
- Bad: token plaintext or token-shaped values such as `eyJ...` appear in logs, test assertion failures, test fixtures, SQLite models, OpenAPI examples, panic messages, or docs.

### 6. Tests Required

- Session token unit tests:
  - sign/verify happy path returns version, room ID, member ID, and expiry;
  - default 2-hour TTL usage is asserted at config or handler boundary;
  - expired token returns `ErrExpiredToken`;
  - tampered payload/signature and wrong secret return `ErrInvalidToken`;
  - malformed token, invalid base64 payload, invalid JSON payload with valid signature, missing expiry, missing room/member, and unsupported version are rejected.
- LiveKit token unit tests:
  - generated JWT identity equals member `LiveKitIdentity`;
  - room grant equals product `LiveKitRoomName`;
  - expiry reflects configured 10-minute TTL;
  - publish and subscribe grants are true;
  - data publishing is false;
  - publish sources contain only microphone audio;
  - admin/SIP/agent/camera/screen-share/unrelated grants are absent;
  - blank credentials/room/identity and non-positive TTL are rejected.
- Room/store authorization tests:
  - `online` and `reconnecting` members in active rooms are authorized;
  - missing room, expired room, missing member, wrong-room member, and disconnected member are rejected with stable sentinels;
  - authorization reads persisted product state and never uses LiveKit presence or anonymous identity alone.
- HTTP tests:
  - create and join return `room`, `member`, `room_session_token`, `livekit_url`, and `livekit_token`;
  - invalid credential config returns `500 internal_error` before room/create or join service mutation;
  - fresh LiveKit endpoint succeeds for an active member with valid bearer token;
  - missing bearer, tampered token, expired token, room mismatch, missing/disconnected member, and expired room map to expected JSON error envelopes;
  - existing create/join/leave validation and lifecycle tests still pass.
- Contract check:
  - `services/api/openapi.yaml` documents added credential fields, `POST /v1/rooms/{room_id}/livekit-token`, authorization header expectation, success response, and every public credential error code.
- Full backend check:

```bash
go test -count=1 ./services/api/...
git diff --check
```

### 7. Wrong vs Correct

#### Wrong

```go
// Accepts client-controlled identity and skips product membership verification.
func (h *Handlers) FreshLiveKitToken(c *gin.Context) {
	identity := c.PostForm("anonymous_id")
	roomName := c.Param("room_id")
	token, _ := livekit.JoinToken(livekit.JoinTokenInput{
		APIKey: h.config.LiveKitAPIKey,
		APISecret: h.config.LiveKitAPISecret,
		RoomName: roomName,
		Identity: identity,
		ValidFor: livekit.DefaultTokenTTL,
	})
	c.JSON(http.StatusOK, gin.H{"livekit_token": token})
}
```

Why wrong: `anonymous_id` is client-controlled and not proof of a successful join; `{room_id}` is a product room ID, not necessarily the LiveKit room name; the handler may issue a media credential to a non-member.

#### Correct

```go
claims, err := session.Verify(session.VerifyInput{
	Secret: config.RoomSessionSecret,
	Token: bearer,
	Now: now,
})
if err != nil {
	writeSessionError(c, err)
	return
}
if claims.RoomID != strings.TrimSpace(c.Param("room_id")) {
	writeError(c, http.StatusForbidden, "room_session_mismatch", "连接凭证与房间不匹配")
	return
}
authorized, err := h.roomAuthorizer.AuthorizeMemberContext(c.Request.Context(), room.AuthorizeMemberInput{
	RoomID: claims.RoomID,
	MemberID: claims.MemberID,
})
if err != nil {
	writeCredentialRoomError(c, err)
	return
}
token, err := livekit.JoinToken(livekit.JoinTokenInput{
	APIKey: config.LiveKitAPIKey,
	APISecret: config.LiveKitAPISecret,
	RoomName: authorized.Room.LiveKitRoomName,
	Identity: authorized.Member.LiveKitIdentity,
	Name: authorized.Member.Nickname,
	ValidFor: config.LiveKitTokenTTL,
})
```

Why correct: the bearer token proves server-issued room/member claims, the room service verifies current persisted product state, and the LiveKit token is scoped to the exact media room and participant identity.

---

## Common Mistakes

- Do not issue or refresh LiveKit tokens based only on `anonymous_id`, request body `member_id`, or LiveKit participant presence.
- Do not create/join product rooms before checking that credential config is complete; otherwise clients can create unusable rooms.
- Do not put bearer token examples with real-looking reusable values in OpenAPI or docs; use placeholder text only.
- Do not treat `room_id` as the LiveKit room name. Use `domain.Room.LiveKitRoomName`.
- Do not catch all token verification errors as `500`; invalid and expired bearer credentials are client authorization failures.
