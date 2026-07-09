# Design：Issue 13 房间会话凭证与 LiveKit 短期凭证

## First-Principles Analysis

### Challenge Assumptions

- Assumption: `anonymous_id` can identify a joined member. Status: wrong for authorization, because it is client-controlled local identity and does not prove a successful join.
- Assumption: LiveKit participant presence can prove product-room membership. Status: wrong boundary, because LiveKit owns media forwarding only; product rooms, members, invite codes, capacity, and lifecycle belong to the business service.
- Assumption: create/join can keep returning only room/member and let the client later ask for media credentials by IDs. Status: insufficient, because IDs alone are not signed and can be guessed or replayed.
- Assumption: a short LiveKit token is enough. Status: incomplete, because the API also needs a separate business credential for WebSocket/fresh-token authorization.
- Assumption: token errors can be precise for every failure. Status: risky for users and logs; public errors should be stable and non-secret while tests verify exact internal cases.

### Bedrock Truths

- The API already creates a durable member row only after product rules pass.
- A client-supplied anonymous identity is not a secret.
- A signed token can prove server issuance if the secret remains server-only and signature verification is constant-time.
- Expiry is necessary because a leaked token without expiry remains useful until the room naturally disappears.
- LiveKit JWTs must contain a room grant and participant identity for the client to join the media room.
- Tokens are bearer credentials; logging or persisting plaintext tokens increases blast radius.
- Product membership must be checked against the business database, not against LiveKit.

### Rebuild from Ground Up

1. Treat the persisted `member_id` created by the business service as the authorization subject.
2. After create/join succeeds, sign `{room_id, member_id, exp}` into a room session token.
3. In the same response, issue a LiveKit token scoped to the product room's `LiveKitRoomName` and the member's `LiveKitIdentity`.
4. For a later fresh LiveKit token request, verify the room session token first, then reload product room/member state from SQLite.
5. Only if the room is active and the member is `online` or `reconnecting`, issue a new short-lived LiveKit token.
6. Never store either token; regenerate from current product state and server secrets.
7. Return stable JSON error envelopes and keep token strings out of logs.

### Contrast with Convention

A conventional shortcut would use anonymous identity or LiveKit room participant identity directly as authorization. That is suboptimal here because neither proves successful product-room membership under echo's domain model. The essential difference is that echo's business service is the only authority for product-room membership and lifecycle.

### Conclusion

The minimum correct mechanism is two-token issuance: a business room session token proving product membership, plus a LiveKit token scoped to media join. Every fresh LiveKit-token request must verify both the signed room session token and current product-room member state.

## Architecture Boundaries

### Packages

- `services/api/internal/session`
  - Owns room session token signing and verification.
  - Uses HMAC-SHA256 with URL-safe base64 payload/signature.
  - Exposes typed claims and typed errors.

- `services/api/internal/livekit`
  - Owns LiveKit JWT creation through `github.com/livekit/protocol/auth`.
  - Does not know HTTP, SQLite, invite codes, or product lifecycle.

- `services/api/internal/room`
  - Continues to own product-room/member state rules.
  - Adds an authorization method that loads room/member state for credential issuance.

- `services/api/internal/store`
  - Adds repository methods to load room by ID and member by room/member ID.
  - Does not sign tokens and does not call LiveKit.

- `services/api/internal/http`
  - Owns transport binding, bearer token extraction, response shape, and error envelope mapping.
  - Does not duplicate token cryptography or room lifecycle rules.

- `services/api/internal/config`
  - Adds explicit TTLs: `RoomSessionTokenTTL` and `LiveKitTokenTTL`.
  - Existing secret/URL fields remain the source for token issuance.

## Data Flow

### Create room / join room

```text
HTTP request
  -> room.Service CreateContext/JoinContext validates product input
  -> store transaction persists/loads room + member
  -> http credential issuer signs room_session_token(room_id, member_id, exp)
  -> livekit.JoinToken(livekit_room_name, livekit_identity, nickname, 10m)
  -> HTTP response includes room, member, room_session_token, livekit_url, livekit_token
```

### Fresh LiveKit token

```text
POST /v1/rooms/{room_id}/livekit-token
  Authorization: Bearer <room_session_token>
  -> session.Verify(secret, token, now)
  -> path room_id must equal claims.room_id
  -> room.Service AuthorizeMemberContext(room_id, member_id)
  -> store loads room/member
  -> reject expired room or inactive member
  -> livekit.JoinToken(room.livekit_room_name, member.livekit_identity, member.nickname, 10m)
  -> response includes livekit_url and livekit_token
```

## Contracts

### Room session token

Proposed payload before signing:

```json
{
  "version": 1,
  "room_id": "room_abc",
  "member_id": "mem_abc",
  "expires_at": "2026-07-09T14:00:00Z"
}
```

Implementation may encode `expires_at` as Unix seconds if tests and docs treat it as the stable contract. Verification must reject:

- malformed token format;
- invalid base64;
- invalid JSON;
- invalid signature;
- blank room/member claims;
- expired token;
- unsupported version.

### LiveKit token

`livekit.JoinToken` input:

- `APIKey`
- `APISecret`
- `RoomName`
- `Identity`
- `Name`
- `ValidFor`

Grant shape:

- `RoomJoin: true`
- `Room: <room.LiveKitRoomName>`
- `CanPublish: true`
- `CanSubscribe: true`
- no admin, SIP, agent, or room-management grants.

### HTTP success responses

Create/join response keeps the existing nested shape and adds top-level credentials:

```json
{
  "room": { "id": "room_abc", "invite_code": "K7M9Q2" },
  "member": { "id": "mem_abc", "livekit_identity": "mem_abc" },
  "room_session_token": "...",
  "livekit_url": "wss://livekit.example.com",
  "livekit_token": "..."
}
```

Fresh LiveKit token response:

```json
{
  "livekit_url": "wss://livekit.example.com",
  "livekit_token": "..."
}
```

### HTTP error mapping

Use existing `apiErrorResponse` envelope.

Suggested mappings:

| Condition | Status | Code | Message |
| --- | --- | --- | --- |
| Missing/invalid bearer token | 401 | `invalid_room_session` | `连接凭证无效，请重新进入房间` |
| Expired room session token | 401 | `room_session_expired` | `连接凭证已过期，请重新进入房间` |
| Token room differs from path | 403 | `room_session_mismatch` | `连接凭证与房间不匹配` |
| Room missing | 404 | `room_not_found` | `房间不存在或已失效` |
| Member missing/inactive | 403 | `member_not_active` | `成员不在房间中` |
| Room expired | 410 | `room_expired` | `该房间已过期，请让朋友重新创建` |
| Token config/generation failure | 500 | `internal_error` | `服务器错误` |

## Persistence and Migration

- No new token tables.
- No token columns on `rooms` or `members`.
- Add read methods only:
  - `FindRoomByID(ctx, roomID) (domain.Room, error)`
  - `FindMemberByRoomAndID(ctx, roomID, memberID) (domain.Member, error)`
- Existing `RoomModel.LiveKitRoomName` and `MemberModel.LiveKitIdentity` remain the source for LiveKit token scope.

## Configuration

Add to `config.Config`:

- `RoomSessionTokenTTL time.Duration`
- `LiveKitTokenTTL time.Duration`

Defaults:

- `LiveKitTokenTTL = 10 * time.Minute` per `design.md`.
- `RoomSessionTokenTTL = 2 * time.Hour`.

Credential issuance rejects missing:

- `RoomSessionSecret` for room session token signing.
- `LiveKitURL`, `LiveKitAPIKey`, or `LiveKitAPISecret` for LiveKit token issuance.

## Compatibility Notes

- Existing tests that assert `room` and `member` should continue to pass because the response adds top-level fields rather than changing nested fields.
- Frontend clients are not yet implemented; no frontend migration is needed.
- OpenAPI must be updated in the same task so generated or future clients see credential fields.

## Security and Logging

- Treat `room_session_token` and `livekit_token` as bearer secrets.
- Do not log request bodies for credential endpoints.
- Do not log token strings, LiveKit API secret, or room session secret.
- Tests and failure messages must not print complete tokens unless a test failure unavoidably prints a local variable; prefer checking booleans/claims after decoding.

## Rollback

This task is backend-only. If implementation fails, revert changes under:

- `services/api/internal/session/`
- `services/api/internal/livekit/`
- modified `services/api/internal/http/`
- modified `services/api/internal/room/`
- modified `services/api/internal/store/`
- modified `services/api/internal/config/`
- `services/api/openapi.yaml`
- `services/api/go.mod` / `go.sum`

No database migration rollback is expected because no schema changes are planned.
