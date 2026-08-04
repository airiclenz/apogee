# Plan — scheduled firings render as expandable blocks in the session chat

**Date:** 2026-08-04
**Status:** complete — all six items (1–6) landed 2026-08-04.
**Goal:** replace the one-line firing notes (`schedule <name> — firing now`, `… — finished:
<title>`) with one expandable tool-call-style block per Firing in the session chat. The block's
payoff is the Firing's actual **answer** — today it is buried in the saved record and the note
restates the prompt twice. The feature is two-sided: outcome plumbing (`internal/run` →
`internal/schedule` → the Fire seam) so the answer and the run's stats reach the TUI at all, and a
new transcript block kind that reuses the existing tool-block machinery (collapse/expand, retained
body, codec persistence). Builds directly on the landed scheduler plan
(`archived/2026-08-03 - 08 - scheduler-library-plan.md`) and ADR 0033.

**Owner decisions taken 2026-08-04 (grill session — do not re-open; implement as written):**

1. **The final answer travels into the chat.** `run.Result` and `schedule.Outcome` gain the
   Firing's final assistant text. The expanded block shows the whole answer; collapsed, its first
   line is visible. Captured from the run's event tap (the last top-level MessageEvent), never by
   decoding the snapshot — so it works for unpersisted runs too.
2. **One block per Firing.** EventFired appends the block; EventCompleted / EventFailed enrich it
   in place (the addToolResult pairing pattern). created / stopped / skipped stay one-line notes —
   lifecycle facts with no body. A failed event with no open block (a Gate refusal — the Firing
   never started) stays a note too.
3. **The body carries:** the full prompt, the run stats (turns, denied count, wall-clock elapsed),
   and the record pointer (the saved record's title, the name to find in `/sessions`). Schedule
   facts (cycle / mode / next fire) are NOT restated per firing — the created note and bare
   `/schedule` already state them.
4. **Collapsed, the answer's first line is visible** — quoted verbatim, never path-shortened. On
   failure the summary slot reads `error: …`; while the Firing runs it holds a static running
   marker.
5. **Own entry kind, tool look.** A new `entrySchedule` kind carries the same `toolView`
   presentation value and paints through the same block painter, under its own `⟳` marker (the
   `/sessions` tag's glyph). Never `entryToolCall`: `hasOpenToolCall` (the live status line),
   approval semantics and sub-agent grouping stay uncontaminated.

**Plan-level prescriptions (not grilled — flag deviations as NOTES, they are open to owner
review, unlike the five above):**

- **The answer follows `outputDetail`'s grammar** (`internal/tui/toolpresent.go`): an answer that
  comes to one line is promoted onto the branch as the quoted summary; a multi-line answer is the
  body's LEADING lines with the summary slot empty — the collapsed paint shows a body's first line
  plus the `… +N more lines` marker, so the answer's first line is visible either way (decision 4
  is satisfied by the grammar, not by duplicating line one in two slots). Per-line `clipDetail`
  as every retained body line takes; the retained body is never truncated.
- **Body order after the answer:** the prompt (verbatim, quoted — a Firing submits it as typed),
  then one stats line — turns and elapsed always, denied only when > 0 (a denial is an alarm, not
  a stat) — then the record pointer, omitted when nothing was persisted
  (`Outcome.RecordID == ""`).
- **`⟳` is static.** No live-star blink and no spinner chain for a firing block — the spinner
  belongs to the worker, and the interactive session is idle while a Firing runs. The running
  state is the summary slot's static text (decision 4). `⟳` (U+27F3) measures one cell under both
  width methods — proven by the `/sessions` tag (scheduler plan item 7 NOTES).
- **Elapsed is the scheduler's measurement**, taken around `Fire` on its own `Clock`, carried on
  EventCompleted AND EventFailed — so every Driver gets it and the fake clock pins it.
- **EventFailed stops discarding the Outcome.** The Fire seam returns whatever the runner salvaged
  (partial record id, turns, denied) beside the error, and the Event carries it — a failed block
  can still point at the partial record.
- **The wire kind `"schedule"` is additive within `transcriptVersion` 1** (the codec's
  additive-within-a-version rule; no version bump). Decode marks a still-open (un-done) schedule
  block finished with an interrupted summary — schedules die with the TUI (ADR 0033), so a resumed
  block claiming to be running would be a lie.

**Authoritative sources (precedence in this order):**

1. The **owner decisions above** — the ratified record of the 2026-08-04 grill.
2. [ADR 0033](../adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)
   — the scheduler decisions this surface serves. Note for ADR 0031's invariant 4: the answer
   text flows **model → surface** and injects nothing model-visible, so nothing here needs
   benching and the facade-export question stays closed.
3. [ADR 0022](../adr/0022-sessions-persist-per-turn-as-dual-representation-records.md) + addenda —
   the persisted-vs-ephemeral test (the block records something that happened → persisted), and
   the codec's own header rules (`internal/tui/transcriptcodec.go`: string-enum kinds, additive
   within a version, defensive re-strip on decode).
4. `layout.md` — "Collapsed and expanded blocks", "The live star", the tool-block grammar the new
   kind must obey; ADR 0030 (widths come from the painter's measure); ADR 0011 +
   `internal/tui/doc.go` (the value-copied Model; per-entry state through the shared entries
   slice, the `setExpanded` pattern).
5. [ADR 0025](../adr/0025-interjections-commit-at-the-between-steps-boundary.md) — chronology
   honesty: the block is enriched at the position EventFired appended it.
6. ADR 0010 — layering: `internal/run` and `internal/schedule` never import `internal/tui`; the
   answer crosses as plain data and the TUI sanitizes at its own seam.

**Standing requirements:**

- Invoke with forwarded skills: `coding-standards` (Go).
- `make check` green before every commit; it runs `-race`. One commit per item.
- **Never** bump `VERSION`, a CHANGELOG release heading, or any other version identifier. The
  closing note carries the suggestion; the owner decides.
- No live LLM endpoint needed — every test runs on the fake upstream / fake clock.
- Any authorized deviation from an item's text lands as a dated NOTES line under that item.

**Out of scope (deliberate):**

- Streaming a Firing's tokens or tool events into the block — v1 firings run without event
  streaming (ADR 0033); the block is announce-then-enrich, not live.
- Click-to-resume the Firing's record from the block. The record pointer names it for
  `/sessions`; opening from the chat is future work.
- Any change to the created / skipped / stopped notes beyond leaving them as they are, and any
  `/sessions` browser change (the ⟳ label landed with the scheduler plan).
- MCP tools in firings, carry-over context, the facade export — all unchanged (ADR 0033).

---

## 1. `internal/run` — the Firing's final answer on `Result` — ✅ DONE (2026-08-04)

NOTES (2026-08-04): owner decision — the depth-scoping is tested BEHAVIOURALLY, not by pinning the
tap type structurally. `TestOnceIgnoresASubAgentsAnswer` scripts a real `sub_agent` delegation
through the fake upstream (the sub-agent's fresh conversation is told apart from its parent's by
the delegated task text) and cancels the parent before it can answer, so the depth-1 message is
the only one on the stream; a recording sink proves it reached the tap, and `Result.FinalText`
must still be empty. Verified load-bearing by mutation: dropping the `Depth != 0` guard fails it.
Harness gained message-content decoding (`request.Texts` / `lastTextHas`), `writeToolCallWithText`
and `recordingSink`. The tap is renamed `usageTap` → `eventTap` for its widened job (the item
leaves that to the implementer).

**What:**

- `run.Result` gains `FinalText string`: the text of the run's final **top-level** assistant
  message, empty when the run produced none (cancelled mid-tool, errored before an answer). Doc
  comment states both halves of the contract: it is raw model output — a surface sanitizes at its
  own render seam, this library does not — and depth > 0 messages (a sub-agent's) never fill it.
- Capture it in `run.Once` via the existing event tap (`usageTap`, `internal/run/run.go` ~`:100`),
  which already observes every event for `CtxUsed`: record the latest `domain.MessageEvent` with
  `Depth == 0` alongside the usage fill. Renaming the tap to match its widened job is the
  implementer's call. NOT from the snapshot: the snapshot only exists on the persisting path, and
  the answer must reach an unpersisted run's Result too (owner decision 1).

**Tests** (`internal/run`, fake upstream): a firing whose scripted upstream answers a known
sentence returns it in `Result.FinalText`; a run cancelled before any assistant message returns
`""`; `FinalText` is filled with a nil `Store` (no record, still an answer); a multi-Turn script
(tool call then answer) returns the LAST message, not the first. If the fake-upstream harness
cannot cheaply script a depth > 0 message, pin the depth-0 rule with a direct test of the tap
type instead — flag which as a NOTES line.

**Acceptance:** `go test ./internal/run/...`; `make check`.

**Commit:** `feat(run): the firing's final answer on Result`

---

## 2. `internal/schedule` — the Outcome grows up and Events learn time — ✅ DONE (2026-08-04)

**What:**

- `schedule.Outcome` gains `FinalText string`, `Turns int`, `Denied int` beside `RecordID` /
  `Title`. Doc: the runner's report, passed through — the library reads none of them (it stays
  runner-agnostic, ADR 0033).
- `schedule.Event` gains two fields:
  - `Prompt string` — set on **EventFired** only: the Firing's prompt, so a surface can show what
    was submitted without a second seam read (the live `Status` deliberately does not carry it,
    and the Schedule may be gone by the time a surface looks).
  - `Elapsed time.Duration` — set on **EventCompleted and EventFailed**: measured around the
    `Fire` call on the Scheduler's own `Clock`, so every Driver gets it and the fake clock pins
    it.
- **EventFailed carries the Outcome the runner salvaged.** `Event.Outcome`'s doc changes from
  "zero otherwise" to: the saved run on EventCompleted, whatever the runner reported beside its
  error on EventFailed, zero otherwise. The firing goroutine stops discarding Fire's Outcome on
  the error path.

**Tests** (`internal/schedule`, fake clock, under `-race` via `make check`): EventFired carries
the Spec's prompt verbatim; a Fire callback that advances the fake clock yields exactly that
Elapsed on its completed event; a failing Fire's event carries Err AND the partial Outcome AND its
Elapsed; every other event kind leaves the new fields zero; existing ordering/policy tests
untouched.

**Acceptance:** `go test ./internal/schedule/...`; `make check`.

**Commit:** `feat(schedule): outcome details, the fired prompt, and elapsed time on events`

---

## 3. `cmd/apogee` — the Fire seam reports everything it knows — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the "recording Notify the existing tests use" did not exist in `cmd/apogee`, so
the two seam tests bring their own composition harness — a channel Notify plus a hand-driven
`schedule.Clock` — which is what lets a REAL Scheduler drive the real `fire` without waiting out
`MinCycle`. The failing-firing cell is a **cancelled** Firing (the Driver going away, ADR 0033)
rather than a broken upstream: the agent loop absorbs an upstream error into an ErrorEvent and
`run.Once` then returns no error at all, so cancellation is the only failure that leaves something
to salvage — and it is the one a Firing actually dies of. Both tests proven load-bearing by
mutation (zeroing the new fields / restoring `schedule.Outcome{}` on the error path fails them).

Depends on items 1 and 2.

**What:**

- `scheduleWiring.fire` (`cmd/apogee/schedule.go` ~`:65`) maps the widened result through:
  `Outcome{RecordID, Title, FinalText: res.FinalText, Turns: res.Turns, Denied: res.Denied}`.
- The error paths return that same best-effort Outcome BESIDE the error instead of
  `schedule.Outcome{}` — the partial-save id stays in the error text as today, and now also
  reaches the surface structurally via `Outcome.RecordID` when the partial record saved.

**Tests** (`cmd/apogee`, existing scripted-upstream harness): a completed firing's event carries
the answer text and turn count through the seam; a failing firing's event still carries the
salvaged Outcome (assert at the `schedule.Event` level through the recording Notify the existing
tests use).

**Acceptance:** `go test ./cmd/...`; `make check`.

**Commit:** `feat(cmd): the firing's answer and stats cross the Fire seam`

---

## 4. `internal/tui` — the firing block — ✅ DONE (2026-08-04)

NOTES (2026-08-04): implementer choices, part A. **The glyph seam (owner left it to the
implementer):** `blockState` gains a `glyph string` override whose ZERO VALUE keeps today's ✦/✧ star,
read in `blockState.star()` before the live/blink conjunction; the `entrySchedule` arm passes
`scheduleTagGlyph` (`⟳`, already a const in `sessions.go`) and leaves `live`/`blink` false, so a
firing block can never blink and no existing caller changes. **Placement:** the block's presenters
and the two transcript methods (`addFiring` / `enrichFiring`) live in `internal/tui/schedule.go`
rather than in `toolpresent.go` / `transcript.go` — the render.go precedent for transcript methods
outside their own file — which keeps the `internal/schedule` import out of `transcript.go` and the
whole surface in one file. **`finishDisplay` takes the ZERO `workspaceRoot`:** nothing the block shows
names a path the way a tool target does, and the plan's own parenthetical says the name, prompt and
answer are never respelled — so the sanitize half runs and the shortening half is a no-op by
construction rather than by luck. **The prompt's first body line carries a `prompt: ` lead:** once the
answer lands ahead of it the body is two quoted voices in a row, and this is what tells them apart; it
rides the first line rather than taking one of its own so the collapsed paint's single body row still
carries prompt text. **An empty answer is worded `finished — no answer`** rather than reused from
`outputDetail`'s `(no output)`, which is a command's phrase and not a Firing's.

NOTES (2026-08-04): checkpoint — PART A done (the `entrySchedule` kind + `hasBlockState`, the fold's
fired/completed/failed transitions and the pairing, the presenters and wording, the fold tests, and
the existing note table updated) / PART B remaining (the `render.go` arm + the glyph seam above,
`layout.md`, and the rendering test groups: collapsed vs expanded paint, the ⟳ header that never
blinks, and the `toolCallRun` / `subAgentSpan` no-contamination pins).

NOTES (2026-08-04): PART B done as recorded above — `blockState.glyph` answers inside `star()` BEFORE
the live/blink conjunction, so the firing block's header is static by construction rather than by its
caller leaving two fields false (proven by mutation: moving the check after the conjunction and
setting `live`/`blink` in the arm blinks the header and fails the test). The three paint test groups
landed in `render_test.go` beside the block painter they exercise — part A's fold tests already
pointed there — and each was proven load-bearing by mutation (dropping the glyph override, and
dropping the `entryToolCall` checks in `toolCallRun` / `subAgentSpan`). `internal/tui/doc.go` gained
one sentence recording the borrowed shape, since its renderToolBlock paragraph is the package's
account of that painter.

Depends on item 2 (the Event fields); shows real answers once item 3 lands.

**What:**

- New `entryKind`: `entrySchedule` (`internal/tui/transcript.go`). It reuses the entry's existing
  `tool toolView` slot and `callID` / `done` fields — `callID` holds the **ScheduleID**, the
  pairing key. `hasBlockState` admits the kind (click-to-expand via the existing `setExpanded` /
  mouse seams — no new mouse code).
- **The fold** (`internal/tui/schedule.go`, `foldScheduleEvent`):
  - **EventFired** appends the block: Label `Schedule`, Target = the schedule name, summary = a
    static running marker (the block's own words — named), body = the prompt lines (from
    `ev.Prompt`, quoted, per-line clipped), `done` false. Built through a pure presenter function
    beside `presentToolCall` and finished through `finishDisplay` (strip-then-shorten, though
    nothing here names a path — the answer, prompt and name are QUOTED and never respelled).
  - **EventCompleted / EventFailed** scan from the tail for the open (`!done`) `entrySchedule`
    with that ScheduleID — serial-per-schedule means at most one — and enrich in place
    (chronology: the block stays where EventFired put it, ADR 0025): the answer per the
    `outputDetail` grammar (header prescription — one-liner rides the branch quoted, multi-line
    leads the body), then the body lines in the prescribed order (prompt — already present from
    the fired fold, answer lines land AHEAD of it; stats worded from `Outcome.Turns`,
    `Event.Elapsed` (spell via `formatCycle`), `Outcome.Denied` when > 0; the record pointer from
    `Outcome.Title`, omitted when `RecordID` is empty). Failure: summary
    `error: <scheduleErrText(ev.Err)>` (named), body still gains stats and any salvaged pointer.
    Mark `done`.
  - **No open block** (a Gate refusal's failed event, a defensive orphan): completed / failed fall
    back to today's notes. created / stopped / skipped keep their note arms untouched;
    `scheduleEventNote` loses only its fired/completed/failed duties.
- **The renderer** (`internal/tui/render.go`): an `entrySchedule` arm in `renderEntryLines`
  painting through `renderToolBlock`, with a leading-glyph seam so this kind's header leads with
  `⟳` instead of the ✦/✧ star — the seam's shape is the implementer's (e.g. a glyph override on
  `blockState` whose zero value keeps the star), but a firing block never blinks: `live`/`blink`
  stay false (header prescription). The `⟳` styling matches the tool header's.
- **No contamination** (owner decision 5): `hasOpenToolCall`, `toolCallRun` grouping and
  `subAgentSpan` key on `entryToolCall` and must not admit `entrySchedule` — pin each with a test
  rather than trusting the switch shape.
- `layout.md`: a short "The firing block" passage beside the tool-block prose (collapsed/expanded
  shape, the static ⟳, what the body carries). Mind `internal/tui/doc.go` (no `strings.Builder`
  by value anywhere the Model reaches).

**Tests** (`internal/tui`): fired→completed enrichment pairs by ScheduleID, and two interleaved
schedules enrich the right blocks; one-line vs multi-line answer grammar (quoted branch summary
vs leading body lines, empty summary); collapsed paint shows the answer's first line and the
`+N more` marker, expanded shows the full answer, prompt, stats and pointer; failed enrichment
words the error and keeps the salvaged pointer; a failed event with no open block lands as a
note; created/stopped/skipped notes byte-identical to today (update the existing schedule_test
tables); escape sequences smuggled into the answer, the prompt and the schedule name are
stripped; the header leads with ⟳ and never blinks; an open firing block leaves
`hasOpenToolCall` false and joins no tool-call group; click toggling works through the existing
`toggleExpanded` path.

**Acceptance:** `go test ./internal/tui/...`; `make check`.

**Commit:** `feat(tui): scheduled firings render as expandable blocks in the chat`

---

## 5. `internal/tui` — the block survives the record — ✅ DONE (2026-08-04)

NOTES (2026-08-04): implementer choices. **The interrupted close is `closeInterruptedFiring` in
`schedule.go`**, not inline in the codec — it is the block's own wording and the decode-side twin of
`enrichFiring`, so both things that can close a block read together; the codec's arm is one guarded
call with the reason on it. **The round-trip fixture answers with a MULTI-line answer** because a
one-line answer's block cannot round-trip byte-for-byte by design: the summary's `quoted` mark is
deliberately absent from the wire (the codec header's rule, already pinned by
`TestTranscriptCodecReplaysAPromotedSummaryAsShown`), so a promoted answer comes back as a named
summary — harmless, since decode respells nothing, but it makes the multi-line shape the honest
DeepEqual fixture. **The unknown-kind tolerance was verified, not written**: the pre-existing
`TestTranscriptCodecUnknownKindSkipped` already proves a v1 blob carrying an unrecognised kind drops
only that entry, which is exactly the older-build case, so nothing about it changed; the new
`TestTranscriptCodecDecodesALegacyBlobUnchanged` covers the other half (an old file decoding
identically now that the map has grown a name). **The ESC test was extended rather than duplicated**
— a finished firing block joined `TestTranscriptCodecStripsEscapesOnDecode`'s fixture, whose loop
already asserts every entry's tool fields; it is finished on purpose, since an open one is re-worded
on decode and would prove nothing about the strip.

Depends on item 4.

**What:**

- `internal/tui/transcriptcodec.go`: encode `entrySchedule` as wire kind `"schedule"` (the string
  enum), reusing the existing `Tool` / `CallID` / `Done` wire fields; decode rebuilds the entry
  through the same constructors every tool view takes (`fromWireToolView`) with the codec's
  defensive re-strip. `transcriptVersion` stays 1 — the kind is additive within the version (the
  codec header's own rule; an older build skips an unknown kind by design — verify that tolerance
  with a test, change nothing about it).
- A decoded schedule block still `!done` comes back **done**, its summary an interrupted line in
  the block's own words (e.g. "never finished — schedules die with the TUI"): the Firing died
  with its TUI (ADR 0033), and a resumed block must not claim to be running.

**Tests** (`internal/tui`): encode/decode round-trips a done block byte-for-byte (view fields,
kind, body kinds); an un-done block round-trips to done + interrupted summary; a legacy blob
without the kind decodes exactly as before; an ESC byte planted in a stored answer line is
stripped on decode; the block passes the persisted-not-ephemeral seam (`encodeTranscript`
includes it).

**Acceptance:** `go test ./internal/tui/...`; `make check`.

**Commit:** `feat(tui): persist the firing block in the transcript codec`

---

## 6. Close-out — the docs trail — ✅ DONE (2026-08-04)

NOTES (2026-08-04): three deviations from the item's literal text. **The CHANGELOG lines land INSIDE
the still-unreleased `/schedule` entry** rather than as a new top-level bullet: that entry ships in the
same release as the block, so a separate bullet would narrate a change to something no user has seen,
and its `The transcript narrates it` sub-bullet — which said fired/finished/failed are notes — is what
the block falsifies, so it was reworded into the block's story (with the lifecycle notes kept as their
own sub-bullet) and the plumbing sentence rides the `Under that surface` bullet. **README was left
alone**: its two `/schedule` mentions are the command-table row and the `/sessions` tag bullet — neither
describes the firing notices — so the item's "otherwise leave alone" applies. **One cross-reference did
not resolve and was fixed**: this plan's ADR 0025 link named
`0025-esc-cancels-the-exchange-and-interjections-join-it.md`, which does not exist; the file is
`0025-interjections-commit-at-the-between-steps-boundary.md`. ADR 0033's Consequences DO state the
notice shape ("commands, pickers, status rows and notices"), so it gained the dated scoping addendum.
The local-time fix's own `### Fixed` entry (commit e6d9e26) was left untouched and nothing added here
restates it.

Depends on items 1–5.

**What:**

- `CHANGELOG.md` — feature lines under the current unreleased/top section in the file's voice:
  the firing block (answer visible in the chat, expandable details), the outcome/elapsed plumbing.
  **Touch no release heading and no `VERSION`.**
- `README.md` — check the `/schedule` bullets: where they describe the firing notices, reword for
  the block; otherwise leave alone.
- ADR 0033 — if its Consequences state that a surface renders scheduler events as notices, append
  one dated addendum sentence recording the block surface as a scoping (notes remain for
  created/stopped/skipped), not a reversal. If the ADR never states the notice shape, touch
  nothing and say so in a NOTES line.
- Verify every cross-reference this plan introduced resolves (this plan ↔ ADR 0033; `layout.md`'s
  new passage).

**Tests:** none (docs only).

**Acceptance:** `grep -n "firing" CHANGELOG.md` shows the new lines; `make check` green.

**Commit:** `docs: record the firing block in the changelog and the schedule docs`

---

**Suggested version bump (owner decides, not an item):** minor. The scheduler family is still
unreleased at `v0.10.15` (the scheduler plan suggested `v0.11.0`); this surface rides the same
minor rather than earning its own.
