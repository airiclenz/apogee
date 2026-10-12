---
title: ADR 0054 context
---

`internal/tui.Options` is the whole interface between the composition root (`cmd/apogee`) and the
renderer. The 2026-08-19 TUI architecture review
(`docs/reviews/2026-08-19 - 00 - tui-architecture-review.md`, Candidate 9) measured what it had
become: **63 fields, about 30 of them one-purpose bare `func` values** — the settings pair-plus-two,
the server family, the launcher family, the scheme family, the heartbeat pair, and more. The seam
grew one field per host capability, exactly as fast as the implementation behind it. That is the
definition of a **shallow** module: the interface a caller must learn is as large as the thing it
hides.

Two facts bounded the design.

**The deep shape was already in the same file.** `Engine`, `SkillCatalog`, `SessionHost`,
`RecallHost` and `Scheduler` are named interfaces, each carrying a family of methods with one doc
comment stating the contract the family shares — nil means unwired, who owns what, which goroutine
calls. Nothing about the func families was different in kind; they were simply never named.

**Nothing could be faked alone.** `m.opts` is read in 19 non-test files, and a test that wanted to
exercise one family had to build a whole `Options` and a whole `Model` around it. There was no value
to hand a test that meant "the settings seam".

The func families also carried a contract nobody had written down: **a nil member means that act is
unavailable, and the pane says so** — the renderer never treats an unwired seam as an error and
never papers over it. That contract had to survive the regrouping, because the degrades it produces
are what a bench or headless Driver composes deliberately
(ADR 0031).

This record ratifies the shape introduced by
`docs/plans/2026-08-19 - 04 - tui-architecture-deepening-plan.md` item 25. It supersedes nothing.
