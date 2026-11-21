---
title: "Confinement posture is one verdict per Driver (architecture review 2026-11-21 #5)"
status: dropped
type: task
priority: 3
labels: [security, driver-parity]
created: 2026-11-21T12:27:13Z
closed: 2026-11-21T16:14:32Z
reason: "Obsolete: already denied by ADR 0083 part 5 (2026-09-15); reopens only on a 4th Driver or boot drift. Triage 2026-11-21."
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f.

Problem: the TUI, headless and daemon boot paths each rebuild the same confinement decision from backend caps, mode, `confine-to-workspace` and attended/unattended. Unconfined-Auto warning: cmd/apogee/daemonfire.go:417, headless.go:929, wire_boot.go:358. Degraded notice: wire_boot.go:365 (headless and daemon refuse instead). Residual notice (`probe.ResidualNotice`): headless.go:944, daemon.go:253, wire_boot.go:383. Label-walk prewarm: daemonfire.go:431, headless.go:961, wire_boot.go:403. Each site carries long comments on why it differs. 86afc00e (AF_UNIX residual) touched daemon.go, headless.go, wire_boot.go and all three of their tests.

Solution: one posture verdict over those inputs — refuse, degraded, residuals, unconfined warning, prewarm — computed once; each Driver only words and prints it. The per-Driver cell tests (headless_test degraded cell, daemon_test, wire_boot_test) become one verdict table plus thin render checks. `Config.ConfinementBox` and the single `Confine` call (subprocess.go:480) are already well-shaped and stay.

Must keep: ADR 0012 (never both unsupervised and unbounded; unattended Auto refused when FSWrite is false), ADR 0112 (residuals are write-class disclosure, never a blocker), ADR 0020 (prewarm), ADR 0031 (engine sufficient for every Driver).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): OBSOLETE — already denied by ADR 0083 part 5 (owner, 2026-09-15); reopens only on a 4th Driver or boot drift; the 11-21 review missed it
