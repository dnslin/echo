# LiveKit Go Token Research

## Question

How should the echo API service issue a short-lived LiveKit join token for one product-room member?

## Evidence

- Context7 selected `/websites/livekit_io` as the authoritative LiveKit docs source.
- LiveKit Go examples use `github.com/livekit/protocol/auth`.
- A join token is created with `auth.NewAccessToken(apiKey, apiSecret)`.
- The token grants room join through `auth.VideoGrant{RoomJoin: true, Room: roomName}`.
- The participant identity is set with `SetIdentity(identity)` and optional display name with `SetName(name)`.
- Token lifetime is set with `SetValidFor(duration)`.
- `ToJWT()` returns the token string.
- Docs also show optional `CanPublish` and `CanSubscribe` pointers on `VideoGrant`; echo should explicitly set both to `true` for the voice-room participant path and avoid any admin/SIP/agent grants.

## Planning Implications

- Implement `services/api/internal/livekit` as a small wrapper around `github.com/livekit/protocol/auth`.
- Inputs must be explicit: API key, API secret, room name, identity, display name, TTL.
- The wrapper must reject blank credentials, blank room name, blank identity, and non-positive TTL before calling LiveKit auth.
- Tests should decode the generated JWT payload and assert room, identity, grant shape, and expiry metadata without printing token plaintext.
- The HTTP layer should return the generated token but never log it or persist it.
