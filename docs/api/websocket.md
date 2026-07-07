# echo Room WebSocket Contract

This file is the documentation landing zone for the future room WebSocket contract. Issue #6 does not implement a WebSocket server runtime.

## Endpoint

- `GET /v1/rooms/{room_id}/ws`
- Authorization: future room session token issued after a successful room create or join flow.

## Server to client message names

- `room.snapshot`
- `member.joined`
- `member.left`
- `member.reconnecting`
- `member.disconnected`
- `member.restored`
- `member.muted_changed`
- `member.speaking_changed`
- `room.expired`
- `room.error`
- `room.resync_required`
- `heartbeat.ping`

## Client to server message names

- `heartbeat.pong`
- `member.mute_changed`
- `member.speaking_changed`
- `member.voice_mode_changed`
- `member.leave_requested`
- `room.resync_requested`
