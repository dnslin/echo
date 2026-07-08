# Implementation Plan: Join Temporary Room by Invite Code API

## Ordered checklist

1. Confirm workspace state and branch
   - ensure `master` is up to date with remote if available;
   - create a feature branch from `master` for Issue #11.

2. Invite normalization
   - add `invite.Normalize` and invite-code validation sentinel(s);
   - cover mixed case, spaces, hyphen, invalid length, invalid characters, and empty-after-ignored input.

3. Domain and store support
   - add member states needed for capacity counting (`reconnecting`, `disconnected`);
   - add repository methods for room lookup by invite, member count by states, member creation, and marking a room expired;
   - test lookup, count, member creation, and duplicate invite/create behavior remains intact.

4. Room service join behavior
   - add `JoinInput`, `JoinResult`, and `JoinContext` / `Join`;
   - reuse existing identity/display validation rules;
   - normalize invite code before lookup;
   - reject invalid, missing, expired, and full rooms with stable errors;
   - create a non-host online member with default voice state;
   - allow duplicate nicknames;
   - test success, normalization, unknown invite, expired room, full room, duplicate nickname, and validation errors.

5. HTTP route and handler
   - add joiner interface and route option without breaking create-only tests;
   - add `POST /v1/rooms/join` with the same request-size cap pattern as create-room;
   - map room service validation/product errors to the standard JSON envelope and required Chinese messages;
   - test success, normalized invite, invalid format, unknown invite, expired room, full room, duplicate nickname, and oversized/malformed request behavior.

6. Main wiring
   - pass the room service to both create and join route options.

7. OpenAPI contract
   - document `POST /v1/rooms/join` request body, 200 response, and all public error responses/codes;
   - keep existing `POST /v1/rooms` schema and examples valid.

8. Validation
   - run targeted tests while implementing;
   - run full backend verification before handing to Trellis check:

```bash
go test -count=1 ./services/api/...
git diff --check
```

9. Trellis quality check
   - run `trellis-check` as required by the workflow;
   - if it fails or changes code, rerun validation and repeat until clean.

## Requirement-driven test scenarios

### Happy path

- Create a room, then join it with an invite code containing lowercase letters, spaces, and hyphens.
- Join response returns the existing room and a new non-host online member.
- Duplicate nickname is accepted.

### Edge cases

- Invite input normalizes to the same code across case/space/hyphen variations.
- Exactly 10 online/reconnecting members are allowed, and the next join fails.
- Reconnecting members count toward capacity; disconnected members do not.

### Error handling

- Empty invite code returns `empty_invite_code` / `请输入邀请码`.
- Wrong length or invalid characters return `invalid_invite_format` / `邀请码应为 6 位字母或数字`.
- Unknown normalized invite returns `invite_not_found` / `邀请码无效，请检查后重试`.
- Expired room returns `room_expired` / `该房间已过期，请让朋友重新创建`.
- Full room returns `room_full` / `房间人数已满，暂时无法加入`.
- Invalid anonymous ID, nickname, or avatar ID reuse existing create-room validation errors.
- Malformed or oversized JSON returns `invalid_request` / `请求格式无效` and does not call the service.

### State transitions

- Joining an active non-full room creates one online member.
- Joining an expired room creates no member.
- Joining a full room creates no member.
- Marking an active room expired due to `expires_at <= now` does not make it joinable.

## Risky files and rollback points

- `services/api/internal/room/service.go`: central product rules; keep changes small and tested.
- `services/api/internal/store/sqlite.go`: persistence translation; avoid leaking raw GORM errors.
- `services/api/internal/http/handlers.go`: public error mapping; keep Chinese messages exact.
- `services/api/openapi.yaml`: contract must match handler behavior.

Rollback by reverting the files listed above; create-room behavior should remain unchanged and covered by existing tests.
