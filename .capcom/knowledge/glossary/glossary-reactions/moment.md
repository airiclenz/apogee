---
title: Moment
---

**Moment**:
A point the loop passes, on which a **Reaction** may fire — `domain.Moment`, one string vocabulary
of sixteen. A **seam** Moment is in-loop and synchronous, its payload an editable working value, and
its five are listed in the order a Turn passes them: `pre-request`, `post-response`,
`pre-tool-exec`, `post-tool-result`, `history-rewrite`. A **notice** Moment is post-hoc, its payload
sealed, and there are **eleven** of them. Six report a fact of their own: `exchange-finished`,
`turn-finished`, `file-changed`, `approval-requested`, `approval-decided`, `error`; `file-changed`
is also one of the two Moments an advise **Reaction** fires at, beside `post-tool-result`. Five are the
**seam-closing** notices — one per seam, spelled `<seam>-finished`: `pre-request-finished`,
`post-response-finished`, `pre-tool-exec-finished`, `post-tool-result-finished`,
`history-rewrite-finished` — each reporting that its seam's cascade ran to its end and the loop
moved on with whatever came out of it. A seam-closing notice carries the seam's **full working
value**, not a digest: the payload is a read-only reference valid for the duration of the emit,
serialized only where an entry actually subscribes and never copied or logged otherwise, so the
observability costs nothing when nobody is watching. The strings are the contract — they are what a
configuration names and what reaches an observer on a `ReactionFiredEvent` — and the set grows
additively.
_Avoid_: "Hook point" (retired — a seam Moment is what it named), "Hook event" (retired — a notice
Moment is what it named), "stage", "phase".
