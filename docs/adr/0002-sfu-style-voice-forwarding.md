# Use SFU-style voice forwarding for MVP

The MVP voice service will forward each member's audio stream separately and will not mix the room into a single server-side audio stream. This fits the product need to show per-member speaking, mute, and reconnect state, keeps server audio processing simple for 2-10 person rooms, and preserves a path to later per-member volume or local mute. Server-side room mixing was rejected for MVP because it moves real-time audio complexity onto the service and makes member-level controls harder to evolve.
