---
title: "A url-safety move is one call on the MCP holder (architecture review 2026-11-10 #4)"
status: parked
type: task
priority: 3
created: 2026-11-10T19:37:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified VALID: admission is decided in six places (`denyMCP`'s two `mcp.Admit` partitions in cmd/apogee/wire_settings.go, plus `requestServerLocked`, `canRedialDroppedLocked`, `dropDenied` and `foldLocked` in cmd/apogee/wire_mcp.go). A denied server is stored two ways: `retire` deletes its state, while the request path and the commit-time deny keep it as `isAdmitted=false`. settingsApplier sequences retire → dropDenied → swap → request itself. Commits 5ff32123, 90379b24, 53ba5ea7 and df5e9257 all touched this deny path on one day.

Problem: the caller sequences the holder's internals and owns half of the admit partition, so every deny fix edits both files.

Solution: the holder takes the url-safety move whole and owns partition, retire, drop and re-dial, with one representation of denied; settingsApplier only words the deny clauses. Fits ADR 0109 D5 and D7.

Caveats: the host-list swap can be refused mid-Turn, and today nothing is requested then — a single call made before the swap would queue dials under lists that never took effect. Keep two phases (deny before the swap, readmit after) or pass the swap in as a callback. The holder must also cover a config file that fails to load (only live sessions on a denied endpoint are dropped, `dropDeniedMCP`).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
