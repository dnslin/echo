# Use client-generated anonymous identity

Echo clients will generate an anonymous identity on first launch and persist it in local settings on the current machine. The business service will accept that identity when joining or creating rooms and use it only for room membership, reconnect support, and host labeling; it is not an account, login credential, friend identity, or cross-device profile. Server-issued accounts or recoverable identities were rejected for MVP because the product promise is no registration and no account system.
