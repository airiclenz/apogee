---
title: "One Driver host for process-scope facilities (architecture review 2026-10-27 #7)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:21Z
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#7), re-verified 2026-11-05 at f7ec72b6. The TUI (cmd/apogee/wire.go), headless (runHeadlessBody in headless.go, now about 530 lines) and the daemon (newDaemonWiring in daemonfire.go) each still hand-assemble the process-scope preamble: resolveRoots and scratch GC, the session store, the confiner and its teardown, the stats recorder, Auto posture and prewarm, and the startup notices. Fix commits were a Driver missing one step (6de72666, snapshot-dir GC in headless and the daemon; d00f0f9c, the daemon's stats recorder), and each step is tested once per Driver in paired TestDaemon / TestHeadless tests (headless_test.go is now 5144 lines). The review proposes one cmd-internal module that takes resolved Options and a notice sink and returns roots, store, confiner and stats recorder with sweeps and posture notices done, plus one close(), so Drivers differ only in where notices go; a follow-on event-chain composer would replace firingInputs (wire_firing.go, 18 fields). No ADR conflict and no engine change; keep the ADR 0103 D1 / ADR 0075 D9 sink ordering if the composer follows.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
