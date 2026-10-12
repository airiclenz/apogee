---
title: "ADR 0083 part 1: The session record keeps presenter verdicts on the wire (2026-09-20, review 09-20 #11)"
---

**Proposed.** One Driver-neutral fold in `internal/session` — `session.Fold(Event) → Entry`,
facts only (kind, text, depth, spawn id, tool name, bounded args, chars, Edit regions, prune and
abort marks) — with the TUI's presenter deriving label, verb, target, stat and solo at paint or
decode time; the eight presenter verdicts `session.ToolView` carries (`Label`, `Verb`, `Target`,
`Solo`, `Stat`, `StatValue`, `Summary.Quoted`, `Details[].Kind`) would leave the wire, and
`run.transcriptFold`'s raw-name cards and the TUI's friendly ones would stop being two shapes of
one blob. The 2026-09-14 review's candidate 12 ("session-record rules live in `internal/session`")
was the same proposal; plan `2026-09-15 - 01` denied it ("the `contentArgs` divergence is
documented design") and the 2026-09-20 review re-raised it on new evidence — verdicts on the
wire, three consumers (`internal/tui/transcriptbridge.go`, `internal/run/transcript.go`,
`cmd/demorig`), and a Firing's record replaying with raw-name cards where a TUI record replays
with friendly ones.

**Denied** (owner, 2026-09-20). ADR 0022
decision 2 (as amended 2026-08-31: the blob is `internal/session`'s, Driver-neutral, so that any
Driver can write and replay a scrollback) and ADR 0052
decision 5 (as amended 2026-08-24: the codec stores the RENDERED tool view and nothing on the
replay path re-runs a presenter) stand. The record is a scrollback, not an event log: resume
repaints *exactly* what the session showed, and a record that came back without verdicts would
replay as a scrollback that changed shape across a restart — the wording an older build chose is
the wording that session showed, and a replay that re-decides it is a replay that lies about
what the user saw. `fromWireToolView` re-deriving `Solo` from the live registry
(`internal/tui/transcriptbridge.go`) is the **documented exception, not a precedent**: it runs in
the one direction that can ADD solo, for a blob written before `Solo` rode the wire, so that two
span-less heads do not fold into one counted block — a repair of a record that predates the
fact, not a preference for the current word over the recorded one. That a Firing's record
carries raw names where a TUI record carries friendly ones is the documented `contentArgs`
divergence: `run.transcriptFold` "does NOT fold anything a PRESENTER decided", by design.

**Reopens on.** A second Driver that must *paint* a record (not merely write one) and cannot
carry the TUI's presenter — a daemon or web scrollback that needs the verdicts re-derived
because it has no rendered form to replay; or a codec version bump that has to migrate the
verdicts anyway. The three-consumer count and the Firing-replay divergence do not reopen it on
their own; they are what the design chose.
