---
title: "winlabel: two confining sessions each spare the shared root, then strand the Low label unrecoverably (audit 2026-09-20 High)"
status: done
type: bug
priority: 2
aliases: [apogee-ea3]
created: 2026-09-21T20:21:42Z
closed: 2026-09-23T06:06:58Z
reason: "Plan 2026-09-22 - 00 item 10: a root spared for a live sibling is handed off, never deleted."
---

Source: docs/reviews/code-audit-2026-09-20.md, High 'two confining sessions each spare the shared root, then strand the Low label on disk unrecoverably'. Windows only.
Where: internal/platform/winlabel/retire.go:218, walk_windows.go:445-447, journal.go:341-344.
What: two sessions confining one workspace — each revertibleRoots gates on the sibling's PID being alive, so each spares the shared root to the other and deletes its own journal; both files gone while the Low label stays on disk. Recover and ResidueIn skip journals whose owner is alive, so the residue is unrecoverable and invisible. Needs the overlapping-close interleave; a sequential teardown clears correctly and a crash between the closes is healed by the surviving journal.
Audit fix: a root spared for aliveness keeps this journal's root entry alive until the sibling is gone — handed off like restorablePriors — and retire must not delete a file that still carries an undischarged root. Design together with the sibling forgeable-journal bead — same files.
