---
title: "Startup is the MCP holder's first request (architecture review 2026-11-10 #7)"
status: parked
type: task
priority: 3
created: 2026-11-10T19:37:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified VALID: the startup connect (`connectMCPServers`, cmd/apogee/mcp_cmd.go) dials outside the holder; callers must run `newLiveMCP` then `emitThrough(sink, skipped)` before any request (cmd/apogee/wire_live.go); the holder carries a seed-only `skipped` list outside `servers` with its own removal branch in `request` (cmd/apogee/wire_mcp.go).

The review's suspected divergence is real but deliberate and low-impact: a repo server skipped at startup for a moved pin is unknown to the holder, so the first later request that still configures it (even an unrelated adoption or url-safety edit) dials it once, whereas a server that failed mid-session is never re-dialled (ADR 0109 D7). See the `liveMCP.skipped` comment and plan 2026-11-08-03 item 17, pinned by `TestMCPSeedCarriesTheStartupState`. Cost: one cheap pin re-check (refused before launch), one extra `mcp connect failed` note, one config-reload poke. ADR 0109 does not record the exception.

Solution: the holder's constructor runs the startup connect as its first request, synchronous and fatal (ADR 0109 D11 stands); a skipped server becomes an ordinary failed state and the outbox is born with the holder, deleting `skipped` and its branch.

Caveats: startup offers OAuth logins on stderr while the holder's connect recipe never does, so the first request needs its own recipe; startup sessions live on the wiring context (`mcpEntry.cancel` is nil); making skipped an ordinary failed state changes the deliberate "first request re-dials it" behaviour to D7's — decide and record in ADR 0109 either way.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
