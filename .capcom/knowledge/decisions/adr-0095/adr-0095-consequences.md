---
title: ADR 0095 consequences
---

- **ADR 0047 point 8 is amended, narrowly.** apogee now stores secrets it minted itself: MCP OAuth
  tokens and the client registrations that go with them. Every secret the user brings (API keys,
  `headers-env:` values, `client-secret-env:`) still comes from the user's own source, and apogee
  still stores none of them.
- **A new directory under `~/.apogee`.** `~/.apogee/mcp-auth/` holds bearer and refresh tokens in
  plain files, protected by file mode, like `~/.ssh` or a cloud CLI's credentials. It sits inside the
  control plane that the `write-apogee-control-plane` rule (`internal/security/rules.go`) already
  guards: a terminal command that writes or deletes there needs approval in every mode, so the
  model cannot quietly change or remove a token.
- **A rotating refresh token is written back on every refresh.** Two apogee processes sharing a
  server must not lose a rotation; the store's atomic write and re-read on refresh carry that.
- **New user surface.** `auth:`, `client-id:` and `client-secret-env:` on `mcp-servers:` entries, the
  `apogee mcp login|logout <name>` subcommands, and a startup login on stderr. The manual, the MCP
  client design contract and `CONTEXT.md` describe them.
- **Headless and daemon runs are unaffected.** They never contact MCP.
