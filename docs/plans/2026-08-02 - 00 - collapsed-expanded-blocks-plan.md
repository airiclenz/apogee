# Collapsed and expanded blocks — implementation plan

- **Goal:** implement the collapse/expand feature for transcript tool blocks exactly as
  specified in `layout.md` § "Collapsed and expanded blocks": the block is the unit, two
  states, collapsed is today's compact paint and the default always (in-flight included),
  a motionless click on a header line or `… +N more lines` marker toggles, sub-agent runs
  collapse to their call block with a cascading summary, and a live block's header star
  blinks.
- **Date:** 2026-08-02
- **Status:** not started
- **Authoritative source:** `layout.md` § "Collapsed and expanded blocks" is the ground
  truth for every item. If any item below disagrees with that section, the layout.md
  section wins and the deviation lands as a dated NOTES line under the item. The design
  was ratified in the 2026-08-02 grill session; no design calls remain open — no item
  carries a DESIGN-CALL gate.
- **Standing requirements:**
  - Forward skills at invocation: `coding-standards`.
  - Vocabulary discipline: the feature's identifiers say *collapsed/expanded*, never
    *fold* — "fold" is reserved by ADR 0011 (the Event fold) and ADR 0018 (the emergency
    fold).
  - The Bubble Tea `Model` is copied by value on every Update (ADR 0011,
    `internal/tui/doc.go`): collapse state lives on the transcript's entries (shared
    backing array, same pattern as `entry.done`), never as a new map or no-copy type held
    by value on `Model`.
  - Before executing this plan, commit the pending spec edits together with this plan
    file: the working tree currently holds the new `layout.md` section and the ISSUES.md
    keyboard-follow-up entry uncommitted (Execute mode's dirty-tree exception covers only
    the plan file itself).
- **Out of scope:**
  - Keyboard toggling / the block-cursor mode (deliberately deferred; tracked as its own
    ISSUES.md entry).
  - Persisting expanded state across sessions (spec: ephemeral view state; the codec
    work in this plan is exclusion tests only, no format change, no version bump).
  - Any three-state model, per-kind default configuration, or config surface.
  - The popup pane's separate `… (+N more lines)` marker (`internal/tui/popup.go:355`) —
    different wording, different mechanism, untouched.
  - An ADR (offered and declined; layout.md is the record).

## 1. Tool outcomes retain full bodies; truncation becomes a paint-time act — ✅ DONE (2026-08-02)

NOTES (2026-08-02): the collapsed cap landed at the BODY seam only — `renderToolBranch` truncates
the Details laid out beneath a branch line (`collapsedDetails`, render.go); `branchDetails` /
`renderDetails` (the targetless shape, where the detail lines ARE the block's ┝/┕ branches) is
deliberately untouched, because capping there would hide an unregistered tool's verbatim arguments
and an orphan result's lines — contradicting this item's byte-identical-paint requirement and
layout.md (collapsed IS today's compact shape; the approval surface never hides the model's
request). One consequence is not byte-identical: a registered body-producing tool whose target
argument is missing (e.g. `sub_agent` with an empty task) now paints its whole body as branches
instead of a first line plus marker. Item 3's target rule follows the same line — a targetless
block hides nothing and is therefore no toggle target.

NOTES (2026-08-02): two test edits beyond the three the item lists — `TestDiffStatSpansTheWholeDiff`
(its body-length assertion was the moved cap) and `TestPresentToolCall`'s docstring oracle. Per-line
`clipDetail` stays in both builders (it is a one-line display cap, not a body truncation), so
`outputDetail` now clips every retained line as `diffBody` always has.

**What:** Move body truncation from presentation-build time to paint time, with paint
output byte-identical to today (no expanded state exists yet — this item is pure
relocation).

- `internal/tui/toolpresent.go`: `outputDetail` (:493) returns the full body — first
  non-empty line plus **all** remaining lines as `detailPlain` detail lines, and no
  `… +N more lines` marker line in `Details` (the single-line → `summaryOnly` branch and
  the summary-vs-body split stay exactly as they are — that split is structural, not a
  truncation). `diffBody` (:530) tags and returns **all** diff lines — no
  `diffDetailCap` slice, no marker line. The `diffDetailCap` constant moves to (or is
  consumed from) the painter.
- `internal/tui/render.go`: the body-painting seam (`branchDetails` :429 /
  `renderSubDetails` :451) applies the collapsed caps at paint and synthesizes the
  marker line: a body containing any diff-kind line (`detailDiffAdded`/`detailDiffRemoved`)
  paints up to `diffDetailCap` lines then `… +N more lines`; any other multi-line body
  paints its first line then `… +N more lines`. (A diff body always carries at least one
  tagged line — a no-change diff never reaches `diffBody`, per the existing
  "No changes detected" rule — so kind-sniffing is exact.) The marker is synthesized by
  the painter, never stored, which is also what makes it identifiable to item 3's map.
- `internal/tui/transcriptcodec.go`: no schema change. `wireToolView.Details` now
  carries full bodies for newly encoded records — larger session files, accepted. The
  decode path and `TestTranscriptCodecGoldenV1` fixtures are untouched; if an encode-side
  golden snapshots presentation-built Details, regenerate it and note why.

**Tests:** update `TestPresentToolCall`, `TestDiffBody`, `TestPresentToolCallOutcomeSplit`
(toolpresent_test.go) — outcomes now carry full bodies and no marker line. Add a
paint-truncation unit test (render_test.go) asserting the marker text and cap for both
body flavors. The existing block-shape tests must pass **unchanged** — byte-identical
paint is the point: `TestRenderDiffMatchesLayoutSketch`, `TestRenderDiffStatSurvivesTheBodyCap`,
`TestRenderOneLineOutputRidesTheBranch`, `TestRenderMultiDetailStandalone`,
`TestTranscriptLayoutGolden`, `TestTranscriptToolTurnGolden`.

**Acceptance:** `go test ./internal/tui/` green; `git diff` shows no change to any
render_test.go golden/expected strings for the tests listed as unchanged;
`grep -n "more line" internal/tui/toolpresent.go` shows no marker synthesis remaining in
the outcome builders.

**Commit:** `refactor(tui): tool outcomes retain full bodies; truncation moves to paint`

## 2. Blocks carry an expanded state and paint their full bodies — ✅ DONE (2026-08-02)

Depends on item 1.

**What:**

- `internal/tui/transcript.go`: `entry` gains `expanded bool` (view-only, zero value =
  collapsed = the default; sits beside `done`). Add a transcript method to toggle it by
  entry index, validating the index targets an `entryToolCall`.
- `internal/tui/render.go`: the painter takes the entry's state; expanded paints every
  retained body line and no marker. Collapsed stays item 1's paint.
- `internal/tui/transcriptcodec.go`: deliberately **no** wire field. Add the exclusion
  test mirroring `TestTranscriptCodecRoundTripExcludesEphemeral`: encode an expanded
  entry, decode, assert it comes back collapsed.
- Grouped blocks: groupable calls carry no bodies by definition (layout.md), so both
  states paint identically — no special case in the painter; whether a group is even a
  click target is item 3's rule.

**Tests:** render test — an expanded entry with a multi-line body paints all lines, no
marker; a collapsed one paints the item-1 truncation; toggle round-trip. Codec exclusion
test as above. `TestModelNoBuilderByValue` still green (state is on the entry, not the
Model).

**Acceptance:** `go test ./internal/tui/` green, including the new exclusion test;
`grep -n "expanded" internal/tui/transcriptcodec.go` shows no wire encoding of the field
(test references only).

**Commit:** `feat(tui): blocks carry an expanded state and paint their full bodies`

## 3. The painter records each block's header and marker lines — ✅ DONE (2026-08-02)

Depends on item 2.

NOTES (2026-08-02): "built in lockstep with line emission" landed as a type rather than a
convention — `blockPaint` (lines + a parallel `targetKind` per line, grown only through `add`/`join`)
is what `renderEntryLines` / `renderToolBlock` / `renderToolBranch` now return, and `renderView`
alone stamps the head-entry index onto the marks as it lays each block down (a block painter says
WHAT each line is, the transcript says WHOSE). `collapsedDetails` accordingly returns
`(shown, remainder, truncated)` instead of one concatenated slice, so the marker's lines are laid
out — and marked — on their own, and `truncated` doubles as the target rule's oracle
(`blockHidesWhenCollapsed`). Two mechanical call-site edits followed the return-type change:
`model_test.go`'s `TestStatusLineAlignsWithTranscriptText` and `render_test.go`'s
`TestToolHeaderLabelStyled` now take `.lines`.

**What:** the hit-test map, built by the one width authority (ADR 0030) in lockstep with
line emission — never re-derived elsewhere.

- `internal/tui/render.go`: `renderedTranscript` (:39) gains a per-rendered-line target
  index alongside `userBlocks`: for each physical line, whether it belongs to a block
  **header** (every physical line of a wrapped header counts) or is a synthesized
  **remainder marker**, each carrying the index of the block's head entry. Built inside
  `renderView`/`renderToolBlock` exactly where those lines are emitted.
- **Target rule:** a header is recorded as a toggle target only when the block's
  collapsed paint hides something — a truncated body, or (item 5) a sub-agent run's
  elided span. A body-less group's header is not a target; a motionless click there
  stays today's no-op.
- `internal/tui/model.go` `refreshViewport` (:2539): stash the map beside `m.lines` /
  `m.userBlocks` so the mouse path reads the same accounting the paint used.

**Tests:** unit tests over `renderView`: a truncated body yields header-target lines and
one marker-target line at the expected indices; an expanded block yields header targets
but no marker; a body-less group yields no targets; a wrapped header marks all its
physical lines. Consistency with the mouse mapping in the style of
`TestFrameRowBoundaryAgreesWithTheMouseMapping`.

**Acceptance:** `go test ./internal/tui/` green; the new map is populated only in
render.go (grep shows no second producer).

**Commit:** `feat(tui): the painter records each block's header and marker lines`

## 4. A motionless click on a header or marker toggles the block — ✅ DONE (2026-08-02)

Depends on item 3.

NOTES (2026-08-02): one file beyond the two the item names — `transcript.go` gained `setExpanded(index,
bool)`, and `toggleExpanded` now delegates to it. The marker's rule ("expand, never collapse") needs a
write that is not a flip, and the alternative was reaching into `transcript.entries` from mouse.go to
read the state before flipping, duplicating the kind/range guard outside its owner. `setExpanded` is
now the one writer of `entry.expanded` (item 2's invariant, restated on the new method). Anchoring
landed as the item's second option — `refreshViewportAnchored` (model.go) calls `refreshViewport` and
then overrides where it parked the view, re-deriving `detached` from the result exactly as
`scrollViewport` does, so "detached ⇔ off the bottom" stays total when the anchor holds the view off
the tail.

**What:**

- `internal/tui/mouse.go`: hook the zero-width release branch (`handleMouseRelease`,
  the `anchor == head` return at :395-398): resolve the click's content line via the
  existing `pointTranscriptRow`/`contentLineAt` path, look it up in item 3's map —
  header target → toggle that block; marker target → expand (never collapse); no target
  → today's no-op. Drag behavior is untouched: motion arbitrates, exactly as now.
- Anchoring: after a toggle, re-render and set the scroll so the clicked block's header
  keeps its screen row — `viewport.SetYOffset(headerLine − screenRow)`, clamped. Do
  **not** route through plain `refreshViewport`'s attached path (it ends at
  `GotoBottom`, which would yank the view to the tail); either extend it with an
  anchored mode or set the offset after it, but the invariant is the spec's: the line
  under the cursor never moves.
- The toggle drops any live transcript selection per the existing keep-if-unchanged rule
  (`spanUnchanged`) — that is the rule working, not a regression.

**Tests:** update `TestTranscriptBareClickCopiesNothing` (bare click on a non-target line
still copies nothing; on a header it toggles and still copies nothing). New:
click-on-header collapses and re-expands; click-on-marker expands; click on a body line
is a no-op; a drag starting on a header line still selects (motion wins); the header's
screen row is identical before and after a toggle in both directions, attached and
scrolled-up (detached) alike.

**Acceptance:** `go test ./internal/tui/` green, including
`TestTranscriptDragSelectsAndCopies` and `TestPromptAndTranscriptSelectionsAreExclusive`
unchanged.

**Commit:** `feat(tui): a motionless click on a header or marker toggles the block`

## 5. A sub-agent run collapses to its call block with a cascading summary

Depends on item 4.

**What:** the run is a block; one rule at every depth.

- **Span:** the run's head is the depth-*d* `sub_agent` `entryToolCall`; its span is the
  maximal following run of entries with depth > *d* (`transcript.entries` already orders
  them head-first; the report folds back into the head, `transcript.go:443-450`). Nothing
  new marks the span — the painter walks it, as `renderView`'s `prevDepth` logic already
  does.
- **Collapsed (default):** `internal/tui/render.go` elides the span entirely — inner
  blocks, `renderSubAgentLabel` descent blocks, rails, spacers. The head block paints
  alone, its summary slot carrying the cascading summary in two tempi: while the run
  works (head not `done`), `N tool calls · ` + the live phrase composed from the span's
  most recent open `entryToolCall` (its view's `Verb`/`Target`, the same composition as
  `toolActivityLabel`, activity.go:133 — derived from span entries at paint, no new
  activity state); once the report lands, `N tool calls · ` + the report's first line
  (the head's existing gist). N counts every `entryToolCall` in the span, all depths —
  transitive by construction.
- **Expanded:** the head paints as an ordinary expanded block (full report body) and the
  span paints as today — each inner block in its **own** state, so a nested collapsed
  run elides its own sub-span by this same rule, recursively.
- Item 3's map covers the head's header lines automatically; the target rule extends: a
  sub-agent head with a non-empty span (or a truncated body) is a toggle target.

**Tests:** goldens beside `TestTranscriptDepthNestedSequenceGolden` /
`TestTranscriptDepthLabelsEachLevel` (transcript_test.go): default paint hides the span
and shows the count summary; expanding reveals inner blocks in their own states; a nested
run stays collapsed inside an expanded parent; descent label blocks are elided when
collapsed; running vs finished summary tempi; transitive count includes nested calls.
`TestRenderGroupsInsideSubAgent` and the rail tests pass unchanged for the expanded
paint.

**Acceptance:** `go test ./internal/tui/` green; a golden test demonstrably contains the
`N tool calls · ` summary in collapsed paint and the unchanged railed span in expanded
paint.

**Commit:** `feat(tui): a sub-agent run collapses to its call block with a cascading summary`

## 6. A live block's header star blinks on the spinner tick

Depends on item 5.

**What:**

- `internal/tui/render.go`: a block whose head still awaits its result (head not `done`,
  or any open call among its group's views or its run's span) paints its header glyph
  from the blink phase — `✦`/`✧` alternating on `m.spin.frame` parity; a settled block
  always paints `✦`.
- `internal/tui/model.go` `spinnerTickMsg` case (:625-634): in addition to advancing the
  frame, refresh the transcript **only while** `m.transcript.hasOpenToolCall()` — the
  spinner tick does not repaint the viewport today, and must not start doing so
  unconditionally. The refresh must update the visible lines in both attached and
  detached (scrolled-up) states; verify `refreshViewport`'s detached early-return still
  swaps the repainted content, and extend it if it does not.
- A selection spanning a blinking header drops on the flip — `spanUnchanged` doing its
  ordinary job; no new selection rule.

**Tests:** glyph alternates with frame parity while a call is open and settles to `✦`
when the result lands (render unit test with both parities); the tick performs no
viewport refresh when no call is open (guard test); a sub-agent run's head blinks while
its span holds an open call; selection spanning the flipping header drops
(`TestSpanUnchangedTable` style).

**Acceptance:** `go test ./internal/tui/` green; `make check` green (this is the last
code item — run the full gate here, not only the package tests).

**Commit:** `feat(tui): a live block's header star blinks on the spinner tick`

## 7. Docs closeout

Depends on item 6.

**What:** this item owns every cross-cutting doc amendment — no other item touches these
files.

- `CHANGELOG.md`: add the feature entry under the current unreleased/in-progress heading
  following the file's own convention. Do **not** add a release heading and do **not**
  touch `VERSION` (version policy: bumps are the user's act).
- `ISSUES.md`: mark the collapse idea (the "klick on tool calls" item) executed per the
  file's own legend (`X`), leaving the deferred keyboard block-cursor entry open.
- `layout.md`: verify the shipped behavior matches § "Collapsed and expanded blocks"
  after items 1–6; if any authorized deviation landed as a NOTES line in this plan,
  reconcile the spec section to match reality and say so in the commit body.

**Tests:** none (docs only).

**Acceptance:** `grep -n "collaps" CHANGELOG.md` finds the new entry;
`grep -n "klick on tool calls" ISSUES.md` shows the item marked `[X]`; `git diff` for
this item touches only CHANGELOG.md, ISSUES.md, and (if reconciled) layout.md;
`VERSION` is unchanged.

**Commit:** `docs: record the collapse wave in the changelog and close the issue`

---

**Suggested version bump:** minor — `0.8.0` → `0.9.0` — a user-facing interaction
feature across the whole transcript (collapse/expand, cascading sub-agent summaries, the
live star). Not performed by this plan; whether and when to bump is the owner's call.
