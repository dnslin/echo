# Issue short-lived LiveKit tokens through the business service

The business service will issue LiveKit join tokens only after product-room checks such as invite validity, room capacity, and reconnect eligibility pass. Tokens will be scoped to the corresponding LiveKit room and participant identity, default to a 10-minute validity window, and be reissued for create, join, and reconnect flows instead of being stored long-term by the client. Product room membership remains authoritative in the business service; a LiveKit token is only a temporary media-join credential.
