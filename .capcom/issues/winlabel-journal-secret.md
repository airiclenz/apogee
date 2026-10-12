---
title: "winlabel: journal claims carry no per-run secret and liveness is PID-only"
status: done
type: bug
priority: 2
aliases: [apogee-winlabel-journal-secret]
created: 2026-09-23T06:06:55Z
closed: 2026-09-23T12:05:00Z
reason: "Resolved by 'winlabel-journal-trust-and-landlock-udp-proof-plan' (381aad92..1c0ffeb6): journal entries bind file identity (vol/fid) and Recover refuses a mismatch; owner liveness pinned to PID + process creation time; journal dir fenced as a protected box root."
---

Remainder of apogee-73s (audit docs/reviews/code-audit-2026-09-20.md, High 'a forgeable confinement journal drives the victim's next Recover/Retire to NULL-SACL or label-write arbitrary paths'). Plan 'docs/plans/archived/2026-09-22 - 00 - audit-design-findings-plan.md' item 11 shipped the Linux-provable half (isVolumeRoot applied to RootJudged entries; a foreign prior under an unexpected label is carried with a bounded life, never dropped or restored). Out of scope there by owner call (2026-09-22), beaded here:
(a) Per-run secret: journal claims (labels-<pid>.json under ~/.apogee/confinement) are read before judgePriors and trusted verbatim; prior_sddl is attacker-chosen. Write a per-run secret into the journal and into the labelled root's own state (or ACL/HMAC) and verify it before any SetSDDL or clear.
(b) Creation-time liveness: journal-owner liveness is PID-only, so a recycled PID reads live and a dead one can be impersonated. Pin liveness to process creation time (GetProcessTimes).
Where: internal/platform/winlabel/retire.go (revertibleRoots, restorablePriors, judgeEntries), walk_windows.go (revertJournal, judgePriors), journal.go, session.go. Windows only: verifiable only on a Windows runner (compile-check here with GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/).
