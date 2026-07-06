# Business service owns product rooms; LiveKit owns media rooms

Echo product rooms are owned by the business service, not by LiveKit. The business service is responsible for temporary room lifecycle, invite code validation, anonymous identity, nickname/avatar/host labels, the 10-person limit, reconnect/leave state, and issuing short-lived LiveKit join tokens. LiveKit rooms are media rooms used for WebRTC audio publishing, subscribing, and forwarding only; this keeps product rules such as 30-minute retention and invite expiry out of the media layer.
