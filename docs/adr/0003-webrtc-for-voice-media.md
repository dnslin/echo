# Use WebRTC for voice media

MVP voice media will use WebRTC with Opus, while room state and signaling will use HTTP and WebSocket-style control channels. This keeps the product focused on stable Windows game voice instead of implementing audio encoding, jitter buffering, packet ordering, packet loss handling, encryption, or NAT traversal from scratch. Custom UDP/TCP/WebSocket media transport was rejected for MVP because it would move the hardest real-time audio reliability work into the application before the core product flow has been validated.
