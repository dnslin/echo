# Use separate public domains for API and LiveKit

Echo v0.1 will expose the business service and LiveKit on separate public hostnames, such as `api.<domain>` for Gin HTTP/WebSocket APIs and `livekit.<domain>` for LiveKit WSS/signaling. Keeping these hostnames separate makes Nginx routing, TLS, logs, proxy timeouts, and LiveKit client configuration easier to reason about than path-prefix routing through a single domain. Path-based routing can be revisited later, but it is not the default for MVP.
