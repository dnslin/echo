# Use self-hosted LiveKit as the MVP SFU

MVP will use self-hosted LiveKit for WebRTC SFU media forwarding instead of building an SFU from lower-level WebRTC primitives. LiveKit provides rooms, participant access tokens, WebRTC signaling, audio track publishing/subscribing, SDKs, and self-host deployment options, which match echo's need to validate temporary rooms and 2-10 person voice before investing in custom media infrastructure. A custom SFU was rejected for MVP because it would move the project into low-level real-time media reliability work before the core product flow is proven.
