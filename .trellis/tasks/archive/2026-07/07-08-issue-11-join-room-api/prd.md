# [S06] Join Temporary Room by Invite Code API

## Goal

Implement the backend vertical path for joining an existing temporary room with an invite code, so a second client can submit a normalized invite code plus local anonymous identity fields and receive a room/member response or a precise product error.

## Background and confirmed facts

- GitHub Issue: #11, `[S06] 邀请码加入临时房间 API 垂直路径`.
- Parent epic: #3.
- Blocker #10 is already merged into `master`; current code contains `POST /v1/rooms` create-room support.
- Current create-room code already persists `rooms` and host `members` through `services/api/internal/store`.
- Current `invite` package can generate 6-character uppercase alphanumeric codes but does not yet normalize user input.
- Current `room.Service` only supports create-room behavior and validation.
- Current HTTP router only exposes `GET /healthz` and `POST /v1/rooms` when a room service is configured.
- Product source of truth requires invite-code input to be case-insensitive and to ignore spaces and short hyphens.
- Product source of truth allows duplicate nicknames in the same room.
- Product source of truth limits each temporary room to 10 online or reconnecting members.
- Product source of truth excludes room-owner management, invite revocation, room passwords, and account identity checks.

## Requirements

### R1. Invite-code normalization

The backend must normalize invite-code input by:

- ignoring whitespace and ASCII short hyphens (`-`);
- converting letters to uppercase;
- accepting only the generated invite alphabet `A-Z0-9` after normalization;
- requiring exactly 6 normalized characters.

Invalid empty input and invalid format must produce distinct product-facing validation messages:

- empty after ignored characters: `请输入邀请码`;
- non-6-character or non-alphanumeric input: `邀请码应为 6 位字母或数字`.

### R2. Join existing active room

`POST /v1/rooms/join` must accept:

- `invite_code`;
- `anonymous_id`;
- `nickname`;
- `avatar_id`.

On success it must:

- find the matching active temporary room by normalized invite code;
- create a new non-host member in that room;
- trim and validate identity/display fields using the existing create-room rules;
- return the same room/member response shape used by create-room, with the joined member marked `is_host: false`, `state: online`, `muted: false`, `speaking: false`, and `voice_mode: push_to_talk`.

### R3. Product error handling

The join endpoint must return the standard JSON error envelope:

```json
{
  "error": {
    "code": "...",
    "message": "..."
  }
}
```

Required product errors:

| Scenario | Required message |
| --- | --- |
| malformed JSON or oversized body | `请求格式无效` |
| empty invite input | `请输入邀请码` |
| invalid invite format | `邀请码应为 6 位字母或数字` |
| invite not found | `邀请码无效，请检查后重试` |
| expired room | `该房间已过期，请让朋友重新创建` |
| room full | `房间人数已满，暂时无法加入` |
| invalid anonymous ID / nickname / avatar ID | reuse existing create-room validation messages |
| unexpected server/store failure | `服务器错误` |

### R4. Expiry check

A room must be rejected as expired when:

- its persisted state is `expired`; or
- its `expires_at` is non-null and `expires_at <= now`.

An expired room cannot be joined even if the invite code matches.

### R5. Capacity check

A room is full when it already has 10 members whose state is `online` or `reconnecting`. A full room must reject the next join attempt with the product full-room error.

### R6. Duplicate nicknames

The join path must allow two or more members in the same room to use the same nickname. Nickname uniqueness must not be added to the service or database layer.

### R7. API contract documentation

`services/api/openapi.yaml` must document:

- `POST /v1/rooms/join` request body;
- success response;
- all public error responses and codes added for join-room behavior;
- unchanged create-room behavior.

## Out of scope

- Room-owner management: kick, transfer, close room, or owner-only permissions.
- Invite-code revocation or custom/long-lived invite codes.
- Treating invite code as a password.
- Account login, account identity verification, friends, or cross-device identity.
- LiveKit token issuance, room session tokens, WebSocket state, reconnect restore semantics, or leave-room lifecycle beyond what is needed to count persisted `online` / `reconnecting` members.
- Frontend join-room UI.

## Acceptance criteria

- [ ] AC1: Invite normalization maps different case, spaces, and short-hyphen forms of the same input to the same 6-character code.
- [ ] AC2: A valid normalized invite code can join an existing active room through `POST /v1/rooms/join`.
- [ ] AC3: Empty, malformed, and unknown invite codes return precise JSON errors with the required Chinese messages.
- [ ] AC4: An expired room cannot be joined and returns the required expired-room error.
- [ ] AC5: The 11th online/reconnecting member cannot join a room and returns the required full-room error.
- [ ] AC6: A member can join with a nickname already used by another member in the same room.
- [ ] AC7: OpenAPI documents join-room request, response, and errors.
- [ ] AC8: Existing create-room behavior and tests remain passing.
- [ ] AC9: `go test -count=1 ./services/api/...` passes.
- [ ] AC10: `git diff --check` passes.

## Requirement-to-test mapping

| Requirement | Test coverage |
| --- | --- |
| R1 | `invite.Normalize` unit tests plus join HTTP success with mixed-case/spaced/hyphenated code |
| R2 | room service join tests and HTTP join success test |
| R3 | HTTP join validation and product-error mapping tests |
| R4 | room service or HTTP test with `expires_at <= now` and/or `state=expired` |
| R5 | service/HTTP capacity test that rejects the next member after 10 online/reconnecting members |
| R6 | service/HTTP duplicate-nickname success test |
| R7 | OpenAPI diff/review plus backend check |

## First-principles analysis

### Challenge assumptions

- It is tempting to treat invite codes as passwords. That is unverified and conflicts with product scope; invite codes are only temporary room locators.
- It is tempting to add owner controls when invite leakage is considered. That is out of scope and conflicts with MVP boundaries.
- It is tempting to require nickname uniqueness to reduce ambiguity. Product facts explicitly allow duplicate nicknames.
- It is tempting to solve all reconnect behavior now. Issue #11 only needs capacity to count online/reconnecting states; full reconnect restoration belongs to later WebSocket/lifecycle work.
- It is tempting to return raw database errors for speed. Existing HTTP contracts require a stable JSON envelope and product-approved Chinese messages.

### Bedrock truths

- A generated invite code has 6 characters from `A-Z0-9`.
- User input may contain lower-case letters, spaces, or short hyphens; all clients must converge to the same canonical code.
- The server is the authority for room validity, expiry, and capacity.
- A user can join without an account; `anonymous_id` is not authentication.
- Capacity is a hard product limit: no more than 10 online/reconnecting members.
- The repository already persists rooms and members; the join path must build on that durable state instead of LiveKit or frontend assumptions.

### Rebuilt solution

From those truths, the smallest correct backend mechanism is:

1. Normalize invite input to a canonical 6-character code before lookup.
2. Validate identity/display fields at the room-service boundary using existing create-room rules.
3. Read the matching persisted room by invite code.
4. Reject not-found, expired, and full states before member creation.
5. Persist the new member as an online non-host member.
6. Expose that through `POST /v1/rooms/join` with the same response style and error envelope as create-room.
7. Document the public contract in OpenAPI.

### Convention contrast

A conventional shortcut would only add a handler that queries `rooms.invite_code = input` and inserts a member. That fails the fundamental requirements because it misses normalization, expiry, capacity, product errors, and duplicate-nickname proof.

## Open questions

None. Repository evidence and Issue #11 answer the planning-relevant scope decisions.
