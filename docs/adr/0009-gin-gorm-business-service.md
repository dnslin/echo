# Use Gin and GORM for the business service

Echo's MVP business service will use Go with Gin for HTTP APIs and GORM for relational persistence. Gin provides the routing, middleware, JSON binding, and JSON response shape needed for room and token APIs; GORM gives a consistent model/persistence layer for room records, invite codes, and lifecycle timestamps. A standard-library-only backend was rejected because the project owner prefers Gin and GORM, and the MVP should optimize for a familiar, maintainable Go backend rather than minimizing dependencies at all costs.
