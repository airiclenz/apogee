---
title: ADR 0091 consequences
---

- `.local` endpoints work in release builds; `docs/manual/configuration.md` ("The servers you run
  models on") says so and explains the unresolved-host refusal.
- Only the provider transport falls back. The llama-launcher module's own HTTP clients, MCP
  transports, the web tools, webhooks and the url-safety guard's lookup still resolve through the
  system alone.
- IPv4 multicast only: a network that carries mDNS over IPv6 alone is not reached, and the fallback
  does no service discovery and keeps no cache — each failed system lookup costs up to one second.
- Nothing here is a Mechanism. It changes how a configured endpoint is reached, not what the model
  may do, so there is nothing for Bypass to switch off.
