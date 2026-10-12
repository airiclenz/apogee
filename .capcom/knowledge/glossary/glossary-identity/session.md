---
title: Session / Session record
aliases: [session-record]
---

**Session** / **Session record**:
A **Session** is one conversation the engine holds — the versioned `domain.Session` envelope
`{Version, State}`, opaque to everything outside the engine. A **Session record** is how that
Session is *persisted*: the on-disk `session.Record` wrapper (`internal/session`) around **two
opaque payloads** — the untouched engine Session **and** the versioned **transcript blob**
written by the neutral codec in `internal/session` (the scrollback: user/assistant text, tool
cards, notes, sub-agent `Depth`, the **run id** of the delegation each delegated entry belongs to
and the **call-ID** of the `sub_agent` call that spawned it, so a resumed fan-out regroups per
child even where two children share a call-ID; a record written before the run id existed regroups
by `Depth` and call-ID) — plus
browsable `Meta` (title, timestamps, workspace, model, message count, last context fill). A
forked record carries its parent's id in `Meta.ParentID` — an additive key like the Schedule
identity, empty on every record that is not a fork, cleared (never refused) on load when it is
not a valid id. A fork's copied scrollback keeps each run card's context fill but none of the
parent's spend: its `Usage*` fields are cleared at the cut, so the child starts at zero spend,
delegate spend included, and its `Meta.DelegateUsage` counts only the calls the child makes. Not
every scrollback entry is persisted: an **ephemeral** entry is display-only — rendered exactly
like its kind, skipped by the encoder — because it is *re-derived* at each startup or resume
rather than earned by the conversation. Today those are the start-up box, the `resumed: <title>`
notice (with its no-scrollback degrade variant and the interrupted-mid-exchange note) and the
`context: …` notice; persisting any of them would append a fresh copy on every resume. The
record is saved **per-Turn** (at each quiescent boundary, so a crash loses at most one Turn),
listed and resumed from inside the TUI through the `/sessions` **browser**, and replayed on
resume so the view repaints instead of showing a bare box over a remembering engine. A
**progress save** is the one write that does not wait for the boundary: while a delegation runs
the TUI re-persists the record at the child's tool boundaries, pairing the LIVE transcript with
the last boundary snapshot (never a fresh one), so a record read mid-run shows the delegation
instead of ending at the previous tool call — and a resumed one closes those still-open calls as
*interrupted*. Each of the three schema versions is rejected/degraded only by its owning layer
(store / TUI / engine). Agent mode, approvals, Confinement, and MCP connections are **not** in the
record — live host state, re-confirmed on resume
(ADR 0008 (ADR 0008)). A record is
**held** by the live instance that has it open (`session.Store.Hold`: the kernel's advisory lock on
`<id>.lock` beside the record, taken at the record's birth and released when that apogee exits or
moves to another session), so every door into it from a second instance — `--resume`,
`--continue`, the browser's resume, rename and delete — is refused with `session <id> is open in another
apogee (pid N) — fork it to work alongside`; the hold guards only against *other* instances, and
`/fork` is the way to work alongside. See ADR 0022.
_Avoid_: "session file" for the Session itself (the *record* is the file; the Session is its
engine payload), "history" (that is the browser's list of records, not one Session — and it is not
Prompt recall either, which is the prompt box's own list of sent inputs).
