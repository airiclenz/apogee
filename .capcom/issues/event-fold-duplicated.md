---
title: "One Event to Entry fold (architecture review 2026-10-27 #12)"
status: parked
type: task
priority: 4
created: 2026-11-05T19:56:21Z
---

Found by docs/reviews/archived/architecture-review-2026-10-27.html (#12), re-verified 2026-11-05 at f7ec72b6. The review rates it speculative. Two folds still turn the Event stream into session-record entries: internal/run/transcript.go for headless and daemon runs, and internal/tui's fold.go / transcript.go / transcriptbridge.go for the TUI, and run's boundArgsFieldCap is kept equal to tui's wireArgsFieldCap (wireargs.go) only by a "MIRRORS" comment; five commits had to touch both folds. The review proposes one neutral fold of stream facts in internal/session (or a sibling package) feeding the session codec, with the TUI layering its Presenter decisions on top. It may not split cleanly, since the TUI folds through its own presentation model; ADR 0022 and ADR 0031 support the direction.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
