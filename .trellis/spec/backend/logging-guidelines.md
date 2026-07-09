# Logging Guidelines

> Backend logging and sensitive-data handling conventions for the echo API service.

---

## Overview

Echo backend logs may describe server behavior, request outcomes, lifecycle events, and diagnostic failures, but they must not expose credentials, secrets, sensitive request bodies, or media data. Room membership and media credentials are bearer-style access material; once logged, they can be replayed or copied outside the user's temporary-room context.

---

## Log Levels

- Use error-level logging for unexpected infrastructure or internal failures that need operator attention.
- Use warning-level logging for recoverable abnormal conditions such as rejected malformed input, without including sensitive values.
- Use info-level logging for coarse service lifecycle events only when useful for operation.
- Avoid debug logs around credential issuance, WebSocket handshake authentication, request bodies, or audio/media paths unless every sensitive field is redacted first.

---

## Structured Logging

- Prefer stable fields such as route name, HTTP method, status code, public error code, room state, and package/component name.
- Do not put raw URLs, raw query strings, headers, request bodies, or token-bearing structs into structured log fields.
- If framework middleware can include the request URL in logs or recovery output, pass it a sanitized request copy with credential-bearing query parameters redacted.
- Authentication and authorization code must still receive the original request or extracted credential value; redaction must be limited to logging/recovery/context surfaces.

---

## What to Log

- Service startup and shutdown outcomes.
- Public route, method, status code, and stable public error code.
- Non-sensitive room lifecycle facts needed for operations, such as active/expired state transitions and aggregate counts.
- Dependency or storage failures without raw SQL values that contain sensitive fields.

---

## What NOT to Log

Never log or commit:

- room session tokens, LiveKit tokens, API keys, API secrets, room session secrets, or token-shaped values;
- `Authorization` header contents;
- WebSocket `token` query parameter contents or raw query strings containing it;
- sensitive request bodies or credential-bearing response bodies;
- audio data, voice content, media frames, or microphone/output device payloads;
- reusable invite-token history beyond the current public invite code semantics already exposed to users.

For WebSocket room-state routes, the public endpoint uses `?token=<room_session_token>` because browser/WebView2 clients cannot attach arbitrary handshake headers. Router code must redact that query parameter before any Gin context, recovery, or log surface can observe it, while preserving non-sensitive query parameters and passing the original request into the hub for authentication.
