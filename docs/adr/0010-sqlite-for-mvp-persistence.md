# Use SQLite for MVP persistence

MVP will use SQLite through GORM for lightweight business persistence such as temporary room records, invite codes, host anonymous identity, creation time, last-empty time, and expiry time. Short-lived runtime state such as WebSocket connections, online presence, speaking state, and reconnect windows should remain in memory. PostgreSQL was rejected for v0.1 because the product is a 20-50 person invited beta and should avoid unnecessary database operations until public scale, multi-instance deployment, or stronger governance requirements appear.
