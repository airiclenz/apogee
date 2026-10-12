---
title: ADR 0025 considered options
---

- **Reuse the Exchange-scoped deferred-injection pipe (ADR 0017 F6).** Rejected on two counts, each
  fatal: it is cleared by `closeExchange`, and it delivers request-scoped. An interjection must
  survive the Exchange, the compaction that summarises it, and the session file that stores it.
- **Keep it request-scoped like `InjectContext`.** Rejected — the same lifetime problem, plus the
  transcript would show a message the history does not contain, which is the dishonesty the whole
  design is arranged against.
- **A second `pendingInput` slot on the Agent, consumed by `step()`.** Rejected: it puts the queue
  in the engine, where the hold-on-stop rule, the pop-back-to-editor and the display rows have no
  business being, and it would deliver a Turn later than the boundary does.
- **Give `Interject` a mutex and let the `Update` goroutine call it any time.** Rejected. A lock over
  `Conversation` would have to be taken by every reader in the hot loop to be worth anything, and it
  would still be *wrong*: a message appended mid-`Step` could land after the request was built,
  reaching the model a Turn late or not at all. The boundary is both cheaper and stronger — the same
  argument ADR 0024 made for idle-only rebind.
- **Deliver an unmarked user message and let the Exchange re-open.** Rejected: it collapses the
  derived Exchange to the remark and strips the shared task context out from under nine Mechanism
  call sites, mid-task. The marker exists to make the honest history and the stable boundary
  compatible.
- **Cache the Exchange opening instead of marking messages.** Rejected — ADR 0017's whole point is
  that the Exchange is *derived*; the one cached boundary (the abort rollback) is a recorded
  exception, not a pattern to grow.
- **Bump `SessionVersion` for the marker.** Rejected as unnecessary: the unknown-sibling passthrough
  and `omitempty` make the field bidirectionally compatible already, and a version bump would refuse
  sessions that are perfectly readable.
- **Auto-send the queue after Esc too.** Rejected by owner decision 2: Esc must mean stop, without
  a footnote. A held queue that needs one `⏎` is a smaller surprise than a stop that sends three
  messages.
- **A new engine `Event` for interjections.** Rejected for now — the TUI notifies itself
  (`interjectedMsg`), and an `events.go` variant stays additively available if an embedder ever wants
  the observability.
- **A drain hook on `Agent.Run` for embedders.** Rejected: `Run` is the convenience loop, and an
  embedder who wants mid-run interjection drives `Step` themselves, which is exactly what the TUI
  does. `Run`'s doc says so. *Stands after the 2026-09-14 amendment:* the pre-emption seam
  (`Config.InterjectionPending`) is a predicate, not a drain — the engine asks whether a message
  waits and skips the delegations it has not started; the commit is still the boundary's, and
  the drain is still the driving goroutine's.
- **Clock-timed scheduling ("send at 15:00").** Rejected — a misreading of the issue's word
  "scheduled", and a timer is a feature nobody asked for.
- **Freeze transcript repaints while a drag is held**, to keep a selection alive across the stream.
  Rejected in the second grill: the stream would visibly stall under every drag. The
  keep-if-unchanged rule (below) achieves the same guarantee without stopping anything.
