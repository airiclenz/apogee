# No-jump submit & collapsible huge prompts — implementation plan

- **Goal:** (1) A new prompt appends naturally at the tail of the session chat instead of
  jumping to the top of an emptied view (ISSUES.md line 9). (2) Prompts taller than three
  wrapped rows collapse to a 3-row shape with an in-line, right-aligned toggle marker,
  reusing the transcript's existing collapse machinery (ISSUES.md line 11).
- **Date:** 2026-08-03
- **Status:** PLANNED
- **Authoritative sources:**
  - The **Ratified decisions** section below — user-ratified in the grill session of
    2026-08-03. If any item text disagrees with it, the Ratified decisions win.
  - `ISSUES.md` lines 9 and 11 (the original complaints).
  - `layout.md` — "Collapsed and expanded blocks" (§ at ~315–373) for the existing
    collapse grammar; the opening scroll paragraph (~1–12) for the current scroll spec.
    Both sections are amended BY this plan (items 1 and 3); the pre-plan wording is
    authority only for what must stay unchanged (sticky header, detach/re-attach,
    toggle anchoring).
  - `internal/tui/doc.go` + ADR 0011 — the value-copied Model rule (no `strings.Builder`
    or other no-copy type held by value anywhere it reaches).
- **Standing requirements:**
  - Forward skills at invocation: `coding-standards`.
  - Any authorized deviation from item text must land as a dated NOTES line under the item.
  - Run `make check` before committing (each item's Acceptance includes it).
- **Out of scope:**
  - `show-scrollbar` config key and terminal-scrollbar hiding (ISSUES.md line 6 — owned
    by plan `2026-08-03 - 02 - scrollbar-visibility-plan.md`).
  - Keyboard path for collapse/expand (ISSUES.md line 21 — deliberately deferred).
  - Toggle-click responsiveness under 100% GPU load (ISSUES.md line 15).
  - Sub-agent context usage display (ISSUES.md line 13).
  - Any config key for the collapse numbers (constants only — ratified).
  - Any version bump (see closing note).

## Ratified decisions (grill session 2026-08-03)

Scroll (issue 9):

- **D1.** Remove the submit-time pad entirely: `refreshViewport` stops padding short
  content, so `GotoBottom()` lands on the true tail. A new prompt appends at the bottom;
  auto-scroll follows the stream; the prompt reaches the top only naturally, where the
  sticky header takes over. The sticky-header mechanism itself is untouched.
- **D2.** layout.md's "an answer shorter than the visible session area opens with its
  prompt stuck to the top of that area" behavior is deleted from the spec, not preserved
  anywhere (not even for the first prompt of an empty session).
- **D3.** Submit still re-arms follow (`detached = false`) — unchanged.

Prompt collapse (issue 11):

- **D4.** Trigger: a prompt whose body soft-wraps to **more than 3 rows** at the current
  width collapses. Measured at paint time — a render-time act on retained facts
  (layout.md grammar); a terminal resize can change whether a prompt is collapsed.
- **D5.** Collapsed shape is **exactly 3 rows**: rows 1–2 full; row 3's content truncated
  (house ellipsis) to leave a gap, with the marker **in-line and right-aligned** on
  row 3: `see more (+N lines)…` where N = total wrapped rows − 3 (plural-aware:
  "+1 line"). No dedicated marker row, no spacer row.
- **D6.** Expanded shape: full body plus **one trailing row** with `see less…`
  right-aligned. (The body must render in full, so the collapse marker never truncates a
  content row.)
- **D7.** Marker strings and the content–marker gap are **named constants** (easily
  changeable); the marker is **highlighted** — a distinct theme style, visually set off
  from the prompt body.
- **D8.** **Clicking anywhere in the prompt block toggles** collapsed⇄expanded (whole
  block, chip row included, is the toggle surface). Drag still selects — the existing
  motionless-click vs drag arbitration is unchanged. After a toggle the clicked row keeps
  its screen position (existing anchored repaint).
- **D9.** Scope: both regular prompts (`❯`, `entryUser`) and interjected prompts
  (`⧖`, `entryInterjected`). Collapsed is the default, always (including the prompt just
  sent); expansion state is view-only and never persisted; resumed/replayed sessions
  paint every over-threshold prompt collapsed.
- **D10.** Sticky header shows the block's **rendered state** — no special-casing. A
  collapsed huge prompt sticks as its 3-row shape; a deliberately expanded one sticks
  expanded (self-inflicted, undone by one click).
- **D11.** Architecture: **shared primitives, no new abstraction layer.** Widen the
  per-entry expanded-state gate to user-authored kinds; the prompt painter emits its own
  click targets; tool and prompt painters stay separate. No "collapsible block"
  interface.
- **D12.** Collapse numbers stay Go constants; no `ui:` config key.

## 1. Remove the submit-time jump-to-top pad — ✅ DONE (2026-08-03)

NOTES (2026-08-03): `wrappedOffset` had a second guard the item did not name,
`TestWrappedOffsetFloors` (`render_test.go` ~50) — removed with the function, along with the
now-unused `viewport` import in that file. Four mouse/spinner tests also encoded the pad by
clicking screen row 0 for the prompt block (`TestTranscriptDragSelectsAndCopies`,
`TestTranscriptMidDragSurvivesRepaint`, `TestTranscriptDragCopiesInEveryState`, and the shared
`armTranscriptSelection` helper): the helper now takes the row explicitly, a new `promptRow`
helper locates the latest user block, and `TestBlinkingStarDropsOnlyTheSelectionsSpanningIt`
passes its deliberate row 0. No behavior beyond the pad was touched.

**What:** In `internal/tui/model.go`, `refreshViewport` (~2596–2627): delete the padding
block — the `wrappedOffset(rendered.lines[:rendered.lastUserStart], …)` call and the
blank-line append — keeping `SetContentLines` + `GotoBottom()` and the existing
detached-repaint branch. Submit path (`model.go` ~1025–1032) is behaviorally unchanged
(`detached = false`, `addUser`, `layout()`). Then grep for now-dead code: if the pad was
the sole consumer, remove `wrappedOffset` (`internal/tui/render.go` ~1128–1154, with its
docstring) and the `lastUserStart` field/tracking in `renderedTranscript`
(`render.go` ~34–49, ~151). The sticky header reads `userBlocks`, not these — it must
keep working untouched. If other consumers exist, leave them and add a NOTES line.
Update `layout.md`'s opening scroll paragraph (~lines 1–12) per D1/D2: follow-the-tail
on submit and stream; sticky header on natural arrival at the top; detach/re-attach
wording kept. Check off the line-9 entry in `ISSUES.md`.

**Tests:** Update `TestStickyPinsShortReply` (`internal/tui/model_test.go` ~2687) to the
new contract (short exchange stays at the tail; no blank padding; rename accordingly).
Remove `TestWrappedOffsetMatchesViewport` (`internal/tui/render_test.go` ~26) if
`wrappedOffset` is removed. Add a regression test: with history taller than the
viewport, submitting a prompt leaves the viewport at the true bottom — content has no
trailing blank rows, the new prompt is on the last content rows, and one page-up reveals
the prior history. The existing follow/detach/sticky suite
(`TestSubmitReattachesFollow`, `TestFollowsTailOfLongStreamedReply`,
`TestStickyHeaderHandoffOnScroll`, `TestDetachedRepaintHoldsPosition`, …) must pass,
adjusted only where they encoded the pad.

**Acceptance:** `go test ./internal/tui/ && make check`

**Commit:** `fix(tui): append new prompts at the tail instead of pinning them to the top`

## 2. Open the collapse state gate to user-authored entries — ✅ DONE (2026-08-03)

NOTES (2026-08-03): the kind set landed as a named predicate, `hasBlockState(kind entryKind) bool`
(`transcript.go`, above `setExpanded`), so the gate and its rationale read in one place. The shared
cap helper is a GENERIC, `splitAtCap[T any](lines []T, limit int) (shown []T, hidden int)` — the
repo's first use of type parameters, and what "usable by both" requires: the tool path splits
`[]detailLine` and item 3's painter will split `[]string`, so a `[]string`-only helper could not
serve both. It clamps a negative cap instead of panicking on the slice (unreachable from either
call site's constants, but this is the repaint path). The test rename is
`TestToggleExpandedTargetsToolCallsOnly` → `TestToggleExpandedTargetsCollapsibleKinds`. No CHANGELOG
entry: nothing user-visible changes until item 3, which carries the feature's entry.

**What:** In `internal/tui/transcript.go`, widen `setExpanded` (~516–522) and thereby
`toggleExpanded` (~528–533) from `kind != entryToolCall` to a kind set
{`entryToolCall`, `entryUser`, `entryInterjected`}; update the doc comments
(`transcript.go` ~68–74 rationale stays: view-only, never persisted). Holding
`expanded = true` on an under-threshold prompt is harmless — the painter ignores it
(item 3). In `internal/tui/render.go`, extract the shown/hidden cap arithmetic from
`collapsedDetails` (~819–829) into a small plain-lines helper usable by both the tool
path and item 3's prompt painter — strictly behavior-preserving for tools (diff cap 20,
non-diff cap 1, `… +N more lines` wording untouched).

**Tests:** Rewrite `TestToggleExpandedTargetsToolCallsOnly`
(`internal/tui/transcript_test.go` ~1156) to pin the new gate: tool, user, and
interjected entries accept expanded state; assistant/note/other kinds still refuse. All
existing collapse goldens (`render_test.go` ~624, ~687, ~758; `transcript_test.go`
~906–1156) stay green unchanged — that is the behavior-preservation proof.

**Acceptance:** `go test ./internal/tui/ && make check`

**Commit:** `refactor(tui): open per-entry expanded state to user-authored blocks`

## 3. Collapse huge prompts to three rows with an in-line toggle marker

Depends on item 2. Independent of item 1.

**What:** Implement D4–D10 in `internal/tui/render.go` + `internal/tui/theme.go`:

- Named constants next to the other display caps: collapsed row cap (3), the marker
  strings (`see more (+N lines)…` built with the house `plural` helper; `see less…`),
  and the content–marker gap (D7).
- A highlighted marker style in `theme.go` (own entry, applied inside the `userBlock`
  background) — visually distinct from prompt body text (D7).
- `renderUserBlock` (`render.go` ~414–429) and the `renderView` dispatch
  (`render.go` ~210, ~246–254): when the wrapped body exceeds 3 rows and the entry is
  not expanded, emit rows 1–2 plus row 3 truncated (house `measure.Truncate` ellipsis,
  cf. the chip-row precedent at `render.go` ~444) with the see-more marker right-aligned
  on row 3 (D5); when expanded, emit the full body plus the trailing right-aligned
  see-less row (D6). Under-threshold prompts render exactly as today. Applies to
  `entryUser` and `entryInterjected` alike (D9).
- Click targets: every row of an over-threshold prompt block — chip row included —
  carries `targetHeader` so the existing routing (`toggleBlockUnder`,
  `internal/tui/mouse.go` ~438–458) toggles it with the existing anchored repaint;
  under-threshold prompt rows stay `targetNone` (D8). The prompt block therefore stops
  being a `plainPaint`; update the "Only a tool call has a click surface" comment
  (`render.go` ~239–242).
- `layout.md` "Collapsed and expanded blocks" (~315–373): add the prompt grammar —
  D4–D10 in the section's own voice (trigger, 3-row shape with in-line marker, trailing
  see-less row, click-anywhere toggle, collapsed-by-default/never-persisted, sticky
  shows rendered state, interjections included).
- `ISSUES.md`: check off the line-11 entry and drop its stray `line 9 & 11` prefix typo.

**Tests:** In `internal/tui/`: render goldens for the collapsed shape (exactly 3 rows;
row 3 truncated with gap; right-aligned marker with correct N and pluralization), the
expanded shape (full body + trailing see-less row), the 3-row boundary (no collapse at
exactly 3 wrapped rows; collapse at 4), a width change crossing the threshold at paint
time, and the interjected variant. Target tests: all rows of an over-threshold block are
`targetHeader` (chip row included); an under-threshold prompt stays all `targetNone`.
Mouse tests (extend the patterns of `TestTranscriptClickTogglesTheBlock`,
`TestTranscriptDragFromHeaderStillSelects`,
`TestTranscriptToggleKeepsTheClickedHeaderRow`): motionless click on any prompt row
toggles both ways; drag from a prompt row still selects; the clicked row keeps its
screen position after a toggle. Codec: extend
`TestTranscriptCodecExcludesExpandedState` with an expanded user entry. Sticky: a
collapsed over-threshold prompt sticks as its 3-row shape.

**Acceptance:** `go test ./internal/tui/ && make check`

**Commit:** `feat(tui): collapse huge prompts to three rows with an in-line toggle marker`

---

**Suggested version bump:** minor — `v0.11.0` (two user-visible TUI behavior changes:
submit scrolling and prompt collapsing). Not performed by this plan; whether and when to
bump is the owner's call.
