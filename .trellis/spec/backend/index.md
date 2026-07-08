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
| [Database Guidelines](./database-guidelines.md) | GORM/SQLite persistence contracts and create-room storage rules | Active: API SQLite persistence |
| [Error Handling](./error-handling.md) | HTTP validation error envelope and create-room error mapping | Active: create-room HTTP validation |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |

---

## Pre-Development Checklist

When working under `services/api/**`:

1. Read [Directory Structure](./directory-structure.md) before changing module layout, router construction, config fields, or workspace files.
2. Read [Database Guidelines](./database-guidelines.md) before adding or changing persistence code, GORM models, SQLite migrations, or repository methods.
3. Read [Error Handling](./error-handling.md) before adding or changing HTTP handlers, validation behavior, error codes, or OpenAPI error examples.
4. Read [Quality Guidelines](./quality-guidelines.md) if a task creates new backend patterns not already covered by a scenario.
5. Read [Logging Guidelines](./logging-guidelines.md) before adding backend logs.

## Quality Check

For backend changes, verify the relevant scenarios and run:

```bash
go test -count=1 ./services/api/...
git diff --check
```

If the change touches OpenAPI, compare `services/api/openapi.yaml` against handler request/response structs and tests.

---

**Language**: All documentation should be written in **English**.
