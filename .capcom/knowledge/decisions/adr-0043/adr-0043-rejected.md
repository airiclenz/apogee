---
title: ADR 0043 considered options
---

- **Leave file-level structure to case-by-case judgment.** Rejected by evidence: judgment produced
  a 4,743-line `model.go` and a 2,932-line `wire.go` in a repo whose package layout is exemplary,
  and the findability probes fail exactly where the written rule was missing.
- **Sub-package `internal/tui` (e.g. `tui/approval`, `tui/session`).** Rejected: the `Model` is one
  coordinator that owns all of that state, so sub-packages would export seams purely to let the
  coordinator reach its own fields — churn against ADR 0011's thin-renderer shape, for a
  navigational win the concern-cluster files already deliver.
- **Move the whole settings cluster, `settingsrows.go` / `settingsedit.go` included, into
  `internal/config`.** Rejected: those two files are the /settings **display** projection, and their
  residency in the composition root is the ADR 0011 split working as designed. Moving them would put
  renderer-shaped output in a package that has no business knowing what a pane paints.
- **Generate the doc.go maps.** Rejected: the value of the map is the human-written half-line saying
  what a file is *for*; a generated list of names narrates nothing. The test pins coverage only, so
  the prose stays hand-written and cannot silently rot.
- **A hard line-count limit per file.** Rejected as the primary rule: size is the symptom that makes
  the drift visible, but the cure is a concern boundary. ~400 lines stays a smell threshold, not a
  gate.
