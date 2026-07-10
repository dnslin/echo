# Issue 16 evidence summary

## Source issue

- GitHub Issue #16: `[S11] WebSocket 静音、正在说话和重连状态广播`
- Acceptance requires:
  - mute state broadcast;
  - speaking-state broadcast without member-list reordering;
  - reconnecting state after WebSocket drop;
  - restore within 30 seconds with the original member identity;
  - timeout removal after 30 seconds;
  - reconnecting members still count toward room capacity.
- Explicit boundaries:
  - speaking is a UI signal only;
  - no server-side audio detection;
  - no cross-device login recovery.

## Current delivered baseline

Issue #15 is already implemented in code:

- WebSocket handshake verifies `?token=` room session credentials and authorizes persisted room/member state (`services/api/internal/ws/hub.go:260-303`).
- Valid connections receive `room.snapshot` immediately, with stable ordering from persisted member rows (`services/api/internal/ws/hub.go:378-411`, `services/api/internal/ws/hub.go:539-562`).
- HTTP join/leave already notify the hub for `member.joined` / `member.left` (`services/api/internal/http/handlers.go:192-229`).
- Unknown established-connection messages still return `room.error` and do not mutate product state (`services/api/internal/ws/hub.go:564-615`, `services/api/internal/ws/hub_test.go:494-526`).

## Current domain / persistence constraints

- Domain already has `MemberStateOnline`, `MemberStateReconnecting`, `MemberStateDisconnected`, plus persisted `Muted` and `Speaking` fields (`services/api/internal/domain/types.go:23-35`, `services/api/internal/domain/types.go:52-65`).
- Domain `VoiceMode` currently exposes only `push_to_talk`; `free_talk` is not yet modeled (`services/api/internal/domain/types.go:31-35`).
- `members` rows already persist `state`, `muted`, `speaking`, and `voice_mode`, but there is no durable `reconnect_until` column (`services/api/internal/store/models.go:24-37`).
- Snapshot ordering is already contract-tested as `joined_at ASC`, then `id ASC`, and disconnected members are excluded (`services/api/internal/store/member_list_test.go:11-49`).
- Join-room capacity already counts `online + reconnecting` and excludes `disconnected` (`services/api/internal/store/join_test.go:43-69`, `services/api/internal/room/service_join_test.go:292-296`).

## Important implementation gap

- The current hub does not implement `member.mute_changed`, `member.speaking_changed`, `member.reconnecting`, `member.restored`, or `member.disconnected`.
- The current disconnect behavior only closes/removes transport connections; it does not create reconnect product state.
- Current leave persistence immediately marks the member `disconnected` and clears `speaking`, then starts empty-room retention when the room becomes empty (`services/api/internal/store/sqlite.go:252-352`).

## Planning decision recorded in this task

This task stays strictly scoped to:

- `mute`
- `speaking`
- `reconnect`

This task explicitly does **not** include:

- `member.voice_mode_changed`
- `free_talk`
- frontend reducer / UI work
- LiveKit media reconnection logic
- cross-instance fan-out

## First-principles design implication

The core problem is not “support every future room-state command”; it is “make room state truthful enough that users know who is muted, who is speaking, and who is temporarily disconnected.”

Given the current codebase facts:

1. Product room authority already lives in the API service, not LiveKit.
2. Reconnect windows are short-lived transport state in a single-instance MVP.
3. There is no durable `reconnect_until` field today.
4. The room snapshot path already projects persisted members and stable ordering.

The simplest design direction is therefore:

- keep reconnect-window timers in hub memory;
- preserve persisted member rows as the capacity source of truth;
- overlay reconnecting metadata into snapshot/event projections;
- add explicit room-service/store mutation paths only where durable state must actually change (`muted`, `speaking`, timeout-to-disconnected).
