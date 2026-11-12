---
title: "Session log: the collapsed transcript block of a session's routine start-up notes"
refs: [layout.md]
---

**Session log**:
The one collapsed transcript block that gathers the **routine** notes a **Session** starts with —
the context files it loaded, Auto's disclosed residuals, model facts such as mandatory reasoning,
the first heartbeat's rebind notes — so they do not each take a line of the first screen. Its header
is `✦ Session log (N notes)` (`(1 note)` for one) under a `▶`, in the task-list card's header-alone
grammar; opened by a click or `enter` at the block cursor, each note is its own `· <text>` row. Each
session boundary — launch, `/clear`, `/new`, a `/sessions` restore — opens a fresh log, which absorbs
routine notes until the session's first Exchange launch (`Model.enterRunning`); later notes print as
their own lines. A notice that needs the human (Auto unconfined, degraded Auto, an unreadable context
file, a refused rebind, a routing notice) is **loud** and never enters it, and a start with no routine
note has no log. The log is an **ephemeral** entry (see **Session**): never encoded into a Session
record. Its fold is per-block and session-only, never the shared `ui.task-list-open`. Only the TUI's
transcript has one: stderr, `apogee headless` and `apogee daemon` print each notice as before. A
residual disclosed inside the collapsed log counts as told (`adr-0112-amend-1112`). Spec:
`layout.md` §"The start-up box".
_Avoid_: "start-up banner" for the log (the banner was the stderr line; the log is a transcript
block), "notices panel" (it is not a **Side panel** section — that move is the open issue
`session-log-side-panel`).
