# Deploy v0.1 as a single instance

The v0.1 invited beta will run as a single deployment unit: one business service instance, one SQLite database file, one self-hosted LiveKit instance, and TURN only if needed for connectivity. The business service may keep online presence, WebSocket connections, speaking state, and reconnect windows in memory because the target beta is 20-50 invited users. Multi-instance deployment, Redis, distributed locks, pub/sub presence, Kubernetes, and LiveKit clustering are deferred until public scale or reliability requirements justify the operational complexity.
