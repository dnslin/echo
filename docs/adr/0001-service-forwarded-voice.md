# Use service-forwarded voice for MVP

MVP voice traffic will be forwarded through a voice service instead of using pure peer-to-peer client connections. The product target is stable 2-10 person Windows game voice with clear room membership, reconnect state, and a hard 10-person room limit; service forwarding centralizes those controls and avoids making MVP reliability depend on each user's NAT, firewall, or home upload capacity. Pure P2P remains a rejected option for MVP because its connection success and upstream bandwidth requirements become least-predictable exactly when room size grows.
