# Backend Development Guidelines

> Code-specs for backend development in this project.

---

## Overview

This directory contains executable backend contracts for the echo API service. Follow the relevant scenario before editing backend code.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | Active: API module/workspace bootstrap |
| [Invite Guidelines](./invite-guidelines.md) | Invite-code generation and normalization contracts | Active: create/join room invite codes |
| [Database Guidelines](./database-guidelines.md) | GORM/SQLite persistence contracts and room/member repository rules | Active: API SQLite persistence, join, leave, and empty-room lifecycle |
| [Credential Guidelines](./credential-guidelines.md) | Room session token, LiveKit token, credential response, and authorization contracts | Active: room credentials and LiveKit token issuance |
| [Error Handling](./error-handling.md) | HTTP validation error envelope and create/join/leave-room error mapping | Active: room HTTP validation |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |

---

## Pre-Development Checklist

When working under `services/api/**`:

1. Read [Directory Structure](./directory-structure.md) before changing module layout, router construction, config fields, or workspace files.
2. Read [Database Guidelines](./database-guidelines.md) before adding or changing persistence code, GORM models, SQLite migrations, or repository methods.
3. Read [Credential Guidelines](./credential-guidelines.md) before adding or changing room session tokens, LiveKit token issuance, credential-bearing responses, credential config, or fresh-token authorization endpoints.
4. Read [Error Handling](./error-handling.md) before adding or changing HTTP handlers, validation behavior, error codes, or OpenAPI error examples.
5. Read [Quality Guidelines](./quality-guidelines.md) if a task creates new backend patterns not already covered by a scenario.
6. Read [Logging Guidelines](./logging-guidelines.md) before adding backend logs, especially near tokens, secrets, request bodies, or media-related code.

## Quality Check

For backend changes, verify the relevant scenarios and run:

```bash
go test -count=1 ./services/api/...
git diff --check
```

If the change touches OpenAPI, compare `services/api/openapi.yaml` against handler request/response structs and tests.

If the change touches credentials, verify `services/api/openapi.yaml`, handler structs, token TTL config, authorization tests, no-token-persistence behavior, and no-plaintext-token/secret logging against [Credential Guidelines](./credential-guidelines.md).

---

**Language**: All documentation should be written in **English**.
