# Tool-call collapse uniformity — every block collapsible, one budget, a state-wearing header — implementation plan

**Goal.** Close ISSUES.md's tool-call display entry: every tool call in the transcript follows the
same collapsible pattern — no collapsed block taller than the house budget, a header that says
whether the block is expanded or collapsed, targetless calls no longer exempt, diffs no longer an
outlier, and the write/edit tools finally showing their changed lines. Plus the separately ratified
live-star timing change (0.5 s `✦`, 0.5 s bare cell).

**Date.** 2026-08-04. **Status.** Finalized 2026-08-04, unexecuted. All design decisions —
including the two formerly open DESIGN-CALLs (targetless collapse, diff cap) — were ratified by
the owner on 2026-08-04 and are embedded below. Nothing is re-asked at execution time.

**Authoritative sources.**
- `ISSUES.md` — the tool-call display entry (the "not more than 3 to 4 lines… all tools must
  follow the same general formatting rules and be collapsable… `Run Python ▾` / `Run Python ▸`"
  item). The ask of record.
- `layout.md` §"The rules behind the tool-call sketch" and §"Collapsed and expanded blocks" — the
  look spec of record. Where an item below deliberately changes the spec, the **Decisions** section
  of this plan is the ratified delta and the item lands the layout.md amendment; any OTHER conflict
  between an item and layout.md → stop and consult, do not pick silently.
- Owner directive 2026-08-04 (in-session): the live star blinks 0.5 s `✦` then 0.5 s empty cell.
- `docs/plans/archived/tool-call-layout-plan.md` and `…/2026-08-02 - 00 -
  collapsed-expanded-blocks-plan.md` — the two landed waves this plan builds on; do not re-open
  their settled decisions beyond the deltas named here.

**Standing requirements (every item).**
- skills: coding-standards
- ADR 0011: the Bubble Tea Model is value-copied — no `strings.Builder` (or any no-copy type) held
  by value anywhere it reaches; renderer-only changes, no agent logic in the TUI.
- ADR 0030 discipline: click-surface marks are emitted in the same act that emits the lines —
  never derived in a second pass.
- Truncation stays a **render-time act on retained facts**: entries keep every body line; caps
  apply at paint. No item may truncate at build time.
- The paint cache (`internal/tui/paintcache.go`) keys must keep naming every input a paint depends
  on. `expanded` and `done` are already in `spanFlags`; an item adding a new paint input must
  extend the key and say so in its NOTES.
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Version identifiers are never touched (see the closing note).

**Out of scope (deliberate, do not drift into):**
- The keyboard collapse/expand path (block-cursor mode) — a separate ISSUES.md entry, deliberately
  deferred; toggling stays mouse-only.
- The prompt/interjection block's own collapse vocabulary (`see more (+N lines)…` / `see less…`)
  and any unification of it with the tool marker — settled by the collapse wave.
- Row-based capping: tool-body caps stay LINE-based even though a 160-rune clipped line can
  soft-wrap to 2+ rows at narrow widths. Known, accepted for this wave.
- The `✦` glyph's colour-role split (unstyled in tool headers, orange as sub-agent rail) — no
  restyling of the star itself.
- The transcript codec's quoted-summary provenance loss (`fromWireToolView` re-marking) —
  documented as deliberate; unrelated.
- The sub-agent context-usage gauge (its own ISSUES.md entry).

**Decisions this plan implements** (all ratified by the owner 2026-08-04; none re-asked at
execution):

1. **Live star timing (ratified 2026-08-04, owner directive):** `✦` shows for half a second, then
   its cell is a bare space for half a second. The phase is derived from the spinner's frame count
   and the style's own interval — the transcript still carries no timer of its own — and the tick
   repaints the viewport only when the phase actually flips.
2. **The header wears the block's state:** a trailing indicator after the label — `▸` collapsed,
   `▾` expanded — painted in the faint detail tone, never the label's orange, and present exactly
   where the header is a toggle target (a block with nothing to reveal wears none, which also
   finally makes clickability visible). Glyph choice per the issue's own suggestion; the owner
   invited alternatives, so proposing different glyphs at review is welcome.
3. **One collapsed budget, the diff included (ratified 2026-08-04):** the collapsed body cap
   becomes a named constant shared by every body kind, sized so a collapsed body-carrying block is
   at most 4 rows at spec width (header, branch, 1 body line, remainder marker). The diff gives up
   its 20-line cap: `diffDetailCap` is deleted and diffs flow through the shared cap (item 4).
4. **Targetless calls join the collapse system (ratified 2026-08-04):** the unregistered/MCP
   argument dump, the missing-target registered call, the orphan result all collapse to the house
   budget with a toggle. This deliberately reverses layout.md's never-hide sentence; the approval
   popup remains the approval surface, the transcript block is the record (item 3).
5. **Write/edit tools show changed lines as display-only diff bodies derived from the call's own
   arguments at presentation time.** Nothing the model sends or receives changes: no tool result
   grows content, the engine stays wire-silent, token cost is untouched. Presentation reads what
   is already in the arguments.

---

## 1. The live star blinks half a second on, half a second bare — ✅ DONE (2026-08-05)

NOTES (2026-08-05): beyond the item's "one entry for the new blink", the `[Unreleased]` **Added**
entry's existing "A live block's star blinks" sub-bullet — which still said `✦`/`✧` alternate — was
corrected in place to the bare cell. Both statements live in the same unreleased notes, so leaving it
would have shipped a contradiction rather than the overlap item 7 is meant to merge. No new paint
input was added: the blink phase already rides `blockKey.blink`, so `paintcache.go`'s key is
unchanged.

**What.** Implement decision 1 in `internal/tui`:
- `spinner.go`: add `const starBlinkHalfPeriod = 500 * time.Millisecond` and
  `func (s spinnerAnim) framesPerBlinkHalf() int` returning
  `max(1, int(starBlinkHalfPeriod/s.interval()))` (5 at classic's 10 fps, 6 at snake's 12, 10 at
  glitter's 20; the floor keeps a hypothetical slower-than-phase style blinking). Reimplement
  `blink()` as `(s.frame/s.framesPerBlinkHalf())%2 == 1` and rewrite its doc comment: derived from
  the frame count and nothing else, no transcript timer, a fresh chain starts at the ✦-showing
  phase.
- `render.go` `blockState.star()`: the `live && blink` arm returns `" "` (a space, not an empty
  string — it holds the glyph's column so the label never shifts). Update the function's doc
  comment and the two comments above `renderToolBlock` / `blockState` that name `✧`.
- `theme.go`: delete `glyphAssistantHollow` (it has no remaining user); note the blink on
  `glyphAssistant`'s comment instead.
- `model.go`, the `spinnerTickMsg` case: capture `wasBlink := m.spin.blink()` before `m.spin.frame++`
  and call `m.refreshViewport()` only when `m.spin.blink() != wasBlink &&
  m.transcript.hasOpenToolCall()`. Update the comment: the repaint happens only on the tick that
  flips the phase — every other tick paints byte-identically and would only put the
  keep-if-unchanged rule between the human and their held selection.
- Comment sweeps: `mouse.go` (the collapsed-selection exemption paragraph naming "✦/✧ … every
  50–100 ms"), `doc.go` (the P3.14 paragraph naming "frame parity"), `paintcache.go` (`blockKey`'s
  `blink` field comment "would miss on every spinner tick" → "on every phase flip").
- `layout.md` §"The live star": `✦` shows for half a second, then its cell is bare for the same,
  phase carried on the spinner's own tick; the bare phase is a space that holds the column. The
  selection-drop and settle sentences stay.
- `CHANGELOG.md` under `[Unreleased]`: one entry for the new blink.

**Tests.**
- `render_test.go` `TestLiveBlockHeaderStarBlinks`: flipped-phase expectations become the
  two-leading-space headers (`"  Run"`, `"  Read File"`, `"  Sub-Agent"`); update the table
  comment (the bare phase keeps the star's column as leading spaces).
- `spinner_test.go` `TestSpinnerTickRepaintsOnlyWhileACallIsOpen` → rename to
  `TestSpinnerTickRepaintsOnlyOnAFlipWhileACallIsOpen`, three cases: no call open on a flipping
  tick → no repaint; call open on a non-flipping tick (frame 0→1) → no repaint (the new guard);
  call open with `spin.frame` primed to `framesPerBlinkHalf()-1` → repaint. The sentinel-in-
  `m.lines` observable stays.
- `spinner_test.go` `TestTickRepaintReachesADetachedViewport`: prime the frame to the boundary
  before the tick; the after-assertion becomes "the settled `✦ Run` is gone from the view".
- `spinner_test.go` `TestBlinkingStarDropsOnlyTheSelectionsSpanningIt`: prime both models' frames
  to the boundary so the tick actually flips.
- `mouse_test.go` `TestTranscriptClickTogglesALiveBlockAcrossTheBlink`: prime the frame before the
  tick step; update its doc comment ("every half second" now).
- `TestFiringBlockHeaderNeverBlinks` must keep passing unchanged (the glyph override answers
  before the conjunction).

**Acceptance.** `gofmt -l internal/tui` empty; `go test ./internal/tui/...` passes;
`grep -rn "✧" internal/ layout.md` finds nothing outside archived docs/CHANGELOG history.

**Commit.** `feat(tui): the live star blinks half a second on, half a second bare`

---

## 2. The header wears the block's state: trailing ▸/▾, and the marker earns a style — ✅ DONE (2026-08-05)

NOTES (2026-08-05): paint-cache key UNCHANGED, as the item predicted — both new paints derive from
`expanded` (already in `spanFlags`) and from view content (immutable per entry), so `paintcache.go`
gained no field. Two additions beyond the item's literal text, both to keep the shipped behaviour and
its spec in step: `layout.md`'s new indicator paragraph also states the remainder marker's new tone
(the item changes that paint, so a spec silent on it would ship stale), and `render.go`'s
`renderToolBlock` doc comment took the same amendment its layout.md counterpart did — its opening
line said the header carries "the label alone", which the indicator would otherwise have contradicted.
The new flip test drives `toggleExpanded` rather than `setExpanded` — the same act, and the one the
neighbouring collapse goldens already use, so a there-and-back walk needs no second helper.

**What.** Implement decision 2:
- `render.go` `renderToolBlock`: when the header is a toggle target (the existing
  `state.elides || blockHidesWhenCollapsed(views)` predicate — the same one that marks
  `targetHeader`), append a trailing state indicator to the header line: `▾` when
  `state.expanded`, `▸` otherwise, separated from the label by one space, painted in the faint
  detail tone (`colFaint`) via a new theme style so it never reads as part of the orange label. A
  header that hides nothing (a body-less group, a targetless call until item 3 lands) wears none —
  the indicator IS the clickability hint. The sub-agent run head (always a target via `elides`)
  and the Firing block (`entrySchedule`, via the same predicate) wear it by the same rule; no
  special cases.
- New glyph constants in `theme.go` (`glyphCollapsed = "▸"`, `glyphExpanded = "▾"`) with comments
  naming their one purpose.
- The remainder marker `… +N more lines` gets its own style role: a new theme entry rendering the
  marker line in the light gray-blue `#8db4e6` foreground (no background, not bold — the prompt's
  `see more` keeps its heavier treatment), so a marker is never mistakable for a body line that
  happens to start with `…`. Wording unchanged.
- Paint-cache note: both new paints derive from `expanded` (already in `spanFlags`) and from view
  content (immutable per entry) — no new key field; say so in NOTES.
- `layout.md`: amend §"The rules behind the tool-call sketch" ("The label" — the header is `✦ ` +
  label, plus a trailing state indicator exactly where the header is a toggle target) and
  §"Collapsed and expanded blocks" (a short indicator paragraph: which glyph in which state, faint
  tone, absent where there is nothing to toggle — the affordance and the click-target rule are one
  predicate). `CHANGELOG.md` entry.

**Tests.** `TestTranscriptLayoutGolden` updated (headers with hidden content gain ` ▸`).
`TestRenderMarksHeaderAndMarkerLines`: assert indicator presence mirrors the `targetHeader` mark in
each of the existing cases (body-less group: no mark, no indicator; capped body: mark and `▸`).
New test: `setExpanded` flips the glyph `▸`→`▾` and back. Marker-style test: the marker line's
style differs from a body line's (`detailStyle` vs the new role). Firing-block and sub-agent-run
headers wear the indicator.

**Acceptance.** `go test ./internal/tui/...` passes; `grep -n "▸\|▾" internal/tui/theme.go` shows
the two constants.

**Commit.** `feat(tui): tool headers wear a ▸/▾ state indicator where a click can toggle them`

---

## 3. Targetless calls join the collapse system — ✅ DONE (2026-08-05)

NOTES (2026-08-05): paint-cache key UNCHANGED — `spanFlags` already packs every covered entry's
`expanded` bit whatever its kind, so `entryToolResult` joining `hasBlockState` added no paint input.
Five deviations, all in service of the item's own text. (a) **Item 4's seam moved**: rather than
duplicate the cap and the `… +N more lines` wording in the targetless path, `collapsedDetails` was
split into `collapsedCall` (the shape switch, now the single oracle both `renderToolBranch` and
`blockHidesWhenCollapsed` ask), `collapsedLimit` (the caps — the one place `1` and `diffDetailCap`
live) and `collapseAtCap` (the split + the marker's wording). **Item 4 must name its constant inside
`collapsedLimit`; the literal `limit := 1` no longer exists, so item 4's `grep -n "limit := 1"`
acceptance is already vacuously true.** (b) `renderOrphanResult` now returns a `blockPaint` and takes
`expanded` — a stray result cannot be a toggle target while its click surface is dropped by
`plainPaint`. (c) The golden gained an `mcp_search` call (item 3's Acceptance requires a collapsed
unregistered-tool block *in the golden*), which pushed the sub-agent read's call id `c6`→`c7`.
(d) `TestTranscriptDepthRendersFramedBlock` expands the stray result before asserting the rail: its
subject is the rail reaching a body's continuation lines, and that second line is now behind the cap.
(e) CHANGELOG — beyond the new sub-bullet, the existing `[Unreleased]` "The state is the view's alone"
sub-bullet still said an unregistered tool's arguments "are never capped"; corrected in place, on
item 1's precedent, so the unreleased notes do not ship a contradiction.

**Decision (ratified 2026-08-04).** `layout.md` today deliberately leaves a targetless call's
lines uncapped in both states ("what the model actually asked for is never hidden from the human
approving it") — an unregistered/MCP call with a 60-line JSON argument blob prints 61 permanent
rows. The owner ratified the reversal: targetless calls (unregistered/MCP fallback, registered
calls whose target argument is missing, orphan `result` blocks) are capped at the house budget
and collapsible like every block. Rationale: the approval popup is where a human approves an
action and it shows the verbatim arguments at decision time; the transcript block is the
*record*, and a record may collapse.

**What.**
- `render.go` `renderToolBranch`'s targetless path: honor `expanded` — collapsed, emit the first
  branch lines up to the house cap (item 4's constant; until item 4 lands, the current 1-line
  collapsed cap) plus the `… +N more lines` marker; expanded, emit all. Branch glyphs stay `┝/┕`.
- `blockHidesWhenCollapsed` stops skipping `Target == ""` views, so the header becomes a toggle
  target (and wears item 2's indicator) exactly when the argument list overflows the cap.
- Orphan results: `entryToolResult` joins `hasBlockState` in `transcript.go` so the `✦ result`
  block toggles by the same machinery.
- `layout.md`: rewrite the "call with no target … uncapped in both states" sentences in both
  sections to the new rule, keeping the *approval-surface* rationale in one sentence so the old
  rule's reasoning is not lost. `CHANGELOG.md` entry.

**Tests.** Unregistered tool with a many-line pretty-printed argument blob: collapsed paint is
within budget with marker, header marked `targetHeader` and wearing `▸`, expanded shows every line;
same for a `terminal` call missing its `command` key; orphan result block toggles.
`TestRenderMarksHeaderAndMarkerLines`'s "a targetless block hides nothing and marks nothing" case
inverts (it now marks when overflowing; a two-line targetless call still marks nothing). Golden
updated.

**Acceptance.** `go test ./internal/tui/...` passes; a collapsed unregistered-tool block in the
golden is ≤ 4 rows.

**Commit.** `feat(tui): targetless calls collapse to the house budget like every block`

---

## 4. One named collapsed budget; the diff joins it — ✅ DONE (2026-08-05)

NOTES (2026-08-05): paint-cache key UNCHANGED — the cap is a compile-time constant, so no paint
gained an input. Deviations, all recorded: (a) **`collapsedLimit` is deleted rather than made the
constant's home** (item 3's NOTES asked for the latter). With one budget it had nothing left to
decide, and a function that takes a body, ignores it and returns a constant is indirection that
reads as a lie; `collapsedBodyCap` now goes straight to `collapseAtCap` from both `collapsedDetails`
and `collapsedCall`, which is item 4's own text ("name the literal … and use it in
`collapsedDetails`"). Verify with `grep -n "collapsedBodyCap" internal/tui/render.go` — the item's
`limit := 1` grep was vacuous either way. (b) **`toolBody.diff`/`isDiff()` now has no production
reader** — the diff cap was its one caller. The machinery stays untouched (deleting it is out of
scope and item 5 names its tests as still-passing); flagged for item 7 or a later cleanup pass.
(c) Two tests beyond the item's list had to move because a collapsed diff no longer shows two
coloured lines: `TestRenderDiffDetailStandalone` and `TestRenderDiffMatchesLayoutSketch` now expand
the block before asserting the body — both are about the body's colouring, which is an expanded
fact now. (d) `TestTranscriptCodecSettlesTheBodyKindOnDecode` lost its cap-based assertion (the kind
sizes nothing any more) and pins the constructor seam across the wire instead. (e) CHANGELOG —
beyond the new sub-bullet, the existing `[Unreleased]` bullet still said a collapsed diff shows
"its first 20"; corrected in place on items 1 and 3's precedent.

**Decision (ratified 2026-08-04).** The collapsed diff body is capped at `diffDetailCap = 20`, so
a collapsed `View Diff` block can stand 23 rows tall — the single largest outlier against the
issue's 3-to-4-line ask; every other body collapses to a bare `limit := 1` literal. The owner
ratified folding the diff into the shared 1-line cap: collapsed = header + branch + 1 body line +
marker = 4 rows; the `+2 -2` summary already carries the scale and one click reveals the hunks.
`diffDetailCap` is deleted.

**What.**
- `render.go`: name the literal — `const collapsedBodyCap = 1` (doc comment: the house collapsed
  budget, the one number behind "a collapsed block is at most 4 rows") — and use it in
  `collapsedDetails`. `diffDetailCap` is deleted; the diff flows through `collapsedBodyCap`.
  Exactly one constant survives, with a comment naming its owner.
- `layout.md`: the collapsed-caps sentence in §"Collapsed and expanded blocks" names ONE budget;
  the diff's exception dies. `CHANGELOG.md` entry.

**Tests.** `TestCollapsedPaintTruncatesRetainedBodies` re-pinned to the unified cap;
`TestRenderDiffStatSurvivesTheBodyCap` and `TestRenderDiffMatchesLayoutSketch` updated; golden
(the `View Diff` block shrinks to budget).

**Acceptance.** `go test ./internal/tui/...` passes; `grep -n "limit := 1" internal/tui/render.go`
finds nothing (the literal is named).

**Commit.** `refactor(tui): one named collapsed body cap, the diff included`

---

## 5. Edit calls carry their changed lines as a diff body (depends on item 4) — ✅ DONE (2026-08-05)

NOTES (2026-08-05): paint-cache key UNCHANGED — the body is entry content settled when the call is
announced, exactly like every other body, so `paintcache.go` gained no field. Four deviations from
the item's literal text, all recorded. (a) **A new `argBody` registry field rather than a grown
`body`**: `toolPresenter.body` takes a RESULT's content and runs in `enrichWithResult`, and an
argument-derived body has to exist before any result — overloading one field with two sources would
have meant a `body` whose parameter means different things per entry. `argBody func(args
map[string]any) []detailLine` runs in `presentToolCall` beside the target extractor, off the same
parsed argument map. (b) **`edit_existing_file`'s full-replacement form** carries a pair that
removes nothing and inserts every line (all `+`); the item's text speaks only of replacement pairs,
which covers that tool's patch form (one pair per `@@` hunk) but not the other half of what the tool
accepts — and leaving its commonest form bodyless would have missed the item's own goal. This is not
item 6's work: item 6 owns `write_file`. (c) **`layout.md` and `CHANGELOG.md` amended** though item 5's
What names neither — the plan's standing rule ("the item lands the layout.md amendment" where a
ratified decision changes the spec) and decision 5 being one of those deltas. layout.md gained a
short paragraph on the derivation and its wire-silence, plus the edit's mention in the two-halves,
grouping and standalone paragraphs. (d) **The golden gained TWO edit calls** (the item's own text
predicts "consecutive edits standing alone"), which pushed `mcp_search` c6→c8 and the sub-agent read
c7→c9. Also: a patch's CONTEXT lines are dropped from the body — a block showing what changed has
nothing to say about a line that is there so the applier can find the place.

**What.** Implement decision 5 for the three edit presenters in `internal/tui/toolpresent.go`
(`single_find_and_replace`, `multi_find_and_replace`, `edit_existing_file`): grow a `body`
extractor that derives a display-only diff from the call's own arguments — per replacement pair,
the removed string's lines prefixed `-` then the inserted string's lines prefixed `+`, pairs in
argument order — flowing through the existing diff body kind so `diffAdded`/`diffRemoved` paint
it and item 4's cap collapses it. The `toolBody` constructor seam is respected (the AST guard
allows construction only inside `newToolBody`). Summaries stay exactly as they are; arguments and
results the model sees are untouched. A body-carrying edit stops grouping with its neighbours —
that is the standing "a call carrying a body breaks the run" rule doing its job, not a new rule;
the golden will show consecutive edits standing alone.

**Tests.** Presenter tests: single pair (one `-` line, one `+` line, multi-line strings kept
line-per-line); multi with several pairs (order preserved); `edit_existing_file`; malformed/absent
arguments degrade to no body (never a panic, never a build-time truncation).
`TestBodyKindMatchesItsLinesEverywhere` (whole-registry walk) and
`TestToolBodyIsBuiltOnlyByItsConstructor` keep passing. Golden updated (edit blocks gain capped
diff bodies and stop grouping).

**Acceptance.** `go test ./internal/tui/...` passes; the golden shows an `Edit File` block with a
`-`/`+` body within budget.

**Commit.** `feat(tui): edit calls carry their changed lines as a collapsed diff body`

---

## 6. A write call carries the written lines (depends on item 4) — ✅ DONE (2026-08-05)

NOTES (2026-08-05): paint-cache key UNCHANGED — the body is entry content settled when the call is
announced, exactly as an edit's is, so `paintcache.go` gained no field. Three deviations from the
item's literal text. (a) **`layout.md` and `CHANGELOG.md` amended** though item 6's What names
neither, on item 5's precedent and the plan's standing rule that the item lands the spec delta of a
ratified decision (decision 5 covers the write): layout.md's derivation, two-halves and standalone
paragraphs now name the write beside the edit. (b) **The golden gained a `write_file` call** (the
item asks for it), which pushed `mcp_search` c8→c9 and the sub-agent read c9→c10. (c) The
"collapses to the cap with marker and expands whole" test landed as a case in the existing
`TestExpandedBlockPaintsItsWholeBody` table rather than as a presenter test — it is a fact about the
paint, and that table is where the collapsed↔expanded round trip already lives; the presenter test
(`TestWriteCallCarriesTheWrittenLines`) pins the derived lines themselves. Also: `writtenLines`
reuses the full-replacement pair the edit body already builds (`replacedText("", content)` →
`changedLines`), so a write and an `edit_existing_file` that say the same thing read identically.

**What.** The `write_file` presenter derives a body from its `content` argument: every line
prefixed `+`, through the same diff body kind as item 5, collapsed to item 4's cap. The
`+N bytes` typed summary stays on the branch beside the target. Display-only, same invariants as
item 5. A body-carrying write stands alone (same standing rule).

**Tests.** Presenter tests: multi-line content collapses to the cap with marker and expands whole;
single-line content still carries a one-line body (the summary slot is occupied by `+N bytes`, so
no promotion); empty/missing content argument → no body. Golden updated.

**Acceptance.** `go test ./internal/tui/...` passes.

**Commit.** `feat(tui): a write call carries the written lines as a collapsed diff body`

---

## 7. Closeout: the spec reads as one document and the issue closes (depends on 1–6) — ✅ DONE (2026-08-05)

NOTES (2026-08-05): the five follow-ups routed here resolved as follows. (a) CHANGELOG's Fixed entry
on the click-during-repaint bug said the star "alternates ✦/✧" in the present tense — reworded to
"blinks with the spinner", which is the fact that entry actually needs. (b)+(c) layout.md's opening
sketch is normative (§"The rules behind the tool-call sketch" explains it by name), so `✦ Run` gained
`▸` and `✦ View Diff` gained `▾`: the diff stays drawn open deliberately, since a collapsed sketch
would never show a full body's shape, and the indicator is what makes the two states legible side by
side — §"Collapsed and expanded blocks" now says so in one sentence. The sketch's `✦ Sub Agent` block
is left ALONE and wears no indicator: its "Sub Agent 1: Agent Name (= brief one line summary)" lines
are not stale spec but the still-parked TODO.md entry "Naming Sub-Agents", so it depicts an
unimplemented wish rather than today's run block — out of this item's scope. (d) **The body kind
stays.** `toolBody.diff`/`isDiff()` has no painter left, but deleting it would take `bodyIsDiff`, the
codec's decode settling and the `newToolBody` constructor guard with it — a design call about the
seam, not a closeout edit. The judgement is recorded where a reader meets the code: the `toolBody`,
`isDiff` and `bodyIsDiff` comments now say the kind sizes nothing and survives for the seam, and the
two test comments that justified the invariant by a per-kind cap were reworded. (e) The
find-and-replace grouping example STAYS: "Edit File" is the only label the registry gives to more
than one tool, so there is no other pair to teach with; the paragraph now says so and adds that the
pair groups only where neither call's arguments say anything about a change. Two further edits beyond
the follow-up list, both squarely "no orphaned sentence": layout.md's "The block's shape" still opened
"One header line carrying the label alone", which item 2's indicator contradicted; and the two
`[Unreleased]` blink entries were merged — the Changed entry framed `✦`/`✧` as prior behaviour that
never shipped in any release, so its substance (the half-second timing, the bare cell holding the
column, the flip-only repaint) folded into the Added sub-bullet and the Changed entry was deleted.

NOTES (2026-08-05, run follow-up — not a plan item): judgement (d) above is SUPERSEDED by the owner's
decision the same day — the body kind is DELETED, not kept. `toolBody.diff`, `isDiff()` and
`bodyIsDiff` are gone, together with the tests that only pinned them
(`TestBodyKindIsSettledWhereTheLinesAre`, `TestBodyKindFollowsTheProducer`,
`TestBodyKindMatchesItsLinesEverywhere` + `assertBodyKindMatchesLines`,
`TestToolBodyIsBuiltOnlyByItsConstructor` + `funcDeclNamed`, and the codec's
`TestTranscriptCodecSettlesTheBodyKindOnDecode`) and the comments (d) recorded. The `newToolBody`
constructor STAYS — the decision named its GUARD, and the seam is still where lines become a body,
whether or not anything is derived there. Dead-machinery removal only: no paint input, no paint and
no golden changed. Neither `layout.md` nor the `[Unreleased]` notes ever described the kind (both
speak of display body kinds and the one budget, which are unchanged), so neither was amended.

NOTES (2026-08-05, run follow-up — not a plan item): the deliberate deferral of the sketch's
`✦ Sub Agent` block (judgement (b)/(c) above) is OVERRIDDEN by the owner's decision the same day —
the block now wears an indicator like every other toggleable run head. The "Naming Sub-Agents"
depiction is untouched; only the indicator was added, plus the one clause in §"Collapsed and
expanded blocks" that names it beside the diff. The indicator is `▾`, not the `▸` the follow-up's
title named: the block is drawn WITH its three body rows, and `▸` there would have contradicted both
the one-line collapsed budget and "a collapsed sub-agent run stands alone" — so the block is drawn
open on the same footing as `View Diff ▾`, which is what keeps the sketch from contradicting the
rule it is meant to obey. Documentation only: no code, no golden, no CHANGELOG entry (the
`[Unreleased]` indicator bullet already covers the rule).

**What.** Read `layout.md`'s two amended sections end-to-end and reconcile any contradiction the
per-item amendments left (one voice, no orphaned sentence still describing the old star, the old
caps, or the targetless exemption); read the `[Unreleased]` CHANGELOG entries as one story and
merge overlaps; delete the solved tool-call display entry from `ISSUES.md` (the block-cursor
keyboard entry STAYS — it is deliberately deferred and its layout.md sentence "keyboard toggling
is deliberately absent" must survive); run the full `make check`.

**Tests.** `make check` (the whole suite is the test).

**Acceptance.** `make check` passes; `grep -n "collapsable" ISSUES.md` no longer matches the
solved entry; `grep -n "✧\|diffDetailCap" layout.md internal/tui` reflects the wave's end state.

**Commit.** `docs: close the tool-call collapse-uniformity issue`

---

**Suggested version bump (not performed):** after this wave lands, `0.8.0` → `0.9.0` — a
user-visible transcript behaviour wave (new affordance glyphs, changed collapse behaviour for
several tools) reads as a minor bump under the 0.x scheme. The owner decides.
