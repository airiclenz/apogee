---
title: "A start-up notice's class is declared once (architecture review 2026-11-21 #8)"
status: parked
type: task
priority: 4
labels: [tui]
created: 2026-11-21T12:27:13Z
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f. Rated Speculative.

Problem: loud vs routine and the transcript wording of each start-up notice are decided by its producer. `tui.Notice{Loud, Log}` is built at 9 sites in 4 files (cmd/apogee/wire_boot.go, wire_live.go, wire_verbs.go, mcp_cmd.go). Context files use a different flag, `notice.ContextNotice.Anomaly`, mapped to loud at internal/tui/model.go:1056. `Notice.Log` only half works: read only from `Options.StartupNotices`, ignored on `RebindResult.Notices` (internal/tui/tui.go:1296-1301). The 14 loud kinds exist only as prose — 3b8d0444 edited three docs (glossary, layout.md, commands.md) to list them, and no test pins the list. Layout leaks out of the TUI: internal/probe/confinement.go carries two wordings of one fact (`ResidualNotice` for stderr, `ResidualNotes` for the log, 459682a6), and `foldHardBreaks` (3fc69ea5) exists to undo text shaped for stderr.

Solution: the TUI Driver declares each start-up notice kind's class and transcript wording once; the loud list becomes code a test pins. Feeds #2 (startup-note-routing-scattered).

Must keep: internal/notice/doc.go (composers return text only; each Driver does its own routing), so the class must not move into `notice` or `domain`. ADR 0112 amend-1112 §2: stderr, headless and daemon output stay byte-for-byte unchanged.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
