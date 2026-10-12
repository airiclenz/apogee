---
title: ADR 0043 context
---

ADR 0010 settled the layout **above**
the file: a domain core, subsystem packages that never import root, and a thin root facade. It says
nothing about what happens **inside** a package, and for most of this repo nothing needed saying —
the 2026-08-10 doc-landscape audit (`../reviews/2026-08-10 - 00 - doc-landscape-audit.md`, §Flags 3–4)
found the package layout exemplary, with `internal/domain` mirroring CONTEXT.md
file-by-file and 5/5 findability probes passing at package level.

Every probe that failed, failed one level down:

- **`internal/tui/model.go` — 4,743 lines, 120 functions.** The name says "the Model"; the content
  is the coordinator plus at least three uninvited concern clusters — the session-save write queue,
  approval key handling and prompt rendering, and command running/refusal. "Where is the session-save
  queue?" answers `model.go`, which is the same as no answer.
- **`cmd/apogee/wire.go` — 2,932 lines, 102 functions, no file-top map, 15+ internal imports.**
  Every "where is X constructed" answer is this one haystack.
- **The `cmd/apogee` config cluster — ~5,000 lines of `config.go`, `configwrite.go`,
  `configmigrate.go`, `configwatch.go`, `registry.go` and the `options` struct, plus ~800 lines of
  /settings projection.** A coherent concept with its own three ADRs
  (0035 (ADR 0035),
  0037 (ADR 0037),
  0041 (ADR 0041)) living in `package main` because that is where it
  started, not because anything put it there.
- **`internal/tui/doc.go` — a 594-line narration that names every file in the package**, load-bearing
  for navigation, hand-maintained, and guarded by nothing. `internal/mechanisms/doc.go` shows the
  end state of that arrangement: a 14-line stub covering 27 files.

The yardstick these are measured against is not invented here. The coding-standards skill
(`airiclenz/skills`, commit `da76213`) gained three narrow rules the audit argued would have
prevented exactly these gaps: coordinator types split their methods across files by concern cluster;
composition roots split by subsystem seam and carry a map comment; a package past ~10 files
maintains a file map, test-enforced where the language allows.

The repo already knows how to do the first two — `internal/tui/prompteditor.go` (the input cluster)
and `internal/tui/fold.go` (the event fold) were lifted out of `model.go` exactly this way, and
`internal/tui/doc.go` is the map rule done by hand. What was missing was a decision that makes it
the rule rather than a habit that holds until the next busy week.

The owner ratified the four calls below on 2026-08-10; the mission brief is
`../handoffs/2026-08-10 - 00 - structure-refactor-model-wire-split.md` and the execution is
`../plans/2026-08-10 - 02 - structure-split-plan.md`.
