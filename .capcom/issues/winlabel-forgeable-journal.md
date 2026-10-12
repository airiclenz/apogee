---
title: "winlabel: a forgeable confinement journal drives the next Recover/Retire to NULL-SACL or label-write arbitrary paths (audit 2026-09-20 High)"
status: done
type: bug
priority: 2
aliases: [apogee-73s]
created: 2026-09-21T20:21:42Z
closed: 2026-09-23T06:06:59Z
reason: Linux-provable half shipped by plan 2026-09-22 - 00 item 11; per-run secret and creation-time liveness remain in apogee-winlabel-journal-secret.
---

Source: docs/reviews/code-audit-2026-09-20.md, High 'a forgeable confinement journal drives the victim's next Recover/Retire to NULL-SACL or label-write arbitrary paths'. Windows only.
Where: internal/platform/winlabel/retire.go:210,257 (revertibleRoots/restorablePriors), walk_windows.go:400 (revertJournal), session.go:221.
What: journal claims are read before judgePriors and never guarded by a per-run secret; prior_sddl is attacker-chosen verbatim (only the path's current label is checked); liveness is PID-only (a handoff drops PID, so the claim later reads pid == 0 -> dead); the persisted RootJudged flag skips isVolumeRoot entirely. A planted labels-<pid>.json under ~/.apogee/confinement makes the next NewConfiner's Recover NULL-SACL an arbitrary tree or SetSDDL an arbitrary descriptor. Trigger needs same-user write into ~/.apogee/confinement (unconfined-mode model, compromised local MCP server); damage is label manipulation/DoS, no privilege boundary crossed. Also the priorRestorable inversion: only a current Low label makes a foreign prior restorable, any other label drops it permanently.
Audit fix: per-run secret written into the journal and into the labelled root's own state (or ACL/HMAC), verified before any SetSDDL/clear; apply isVolumeRoot even to RootJudged entries (the flag skips the label re-read, never the guardrail); pin liveness to process creation time (GetProcessTimes). Design together with the sibling stranded-label bead — same files.
