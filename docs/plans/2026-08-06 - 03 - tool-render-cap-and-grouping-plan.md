# Tool rendering: uniform collapsed cap + per-member grouping — implementation plan

- **Goal**: every collapsed tool block is hard-capped at its header plus three content
  rows (no unbounded soft-wrap), uniformly across all tool shapes; tool grouping is
  extended to body-carrying calls with a group count, one-line member rows, and
  per-member expand/collapse — per the sketch in `docs/layout/tool-layout.md`.
- **Date**: 2026-08-06
- **Status**: not started
- **Authoritative sources**:
  - `docs/layout/tool-layout.md` — the owner's layout sketch (untracked at write time;
    item 1 commits it into the repo). This is the visual ground truth.
  - `layout.md` sections "The rules behind the tool-call sketch" (~line 402) and
    "Collapsed and expanded blocks" (~line 540) — the current prose spec; item 8
    rewrites these to match. Until then, where the sketch and `layout.md` disagree on
    the points below, the sketch wins.
  - ADR 0011 (Bubble Tea Model copied by value), ADR 0030 (paint lines and click-marks
    emitted together).
- **Precedence**: Ratified design calls (below) > `docs/layout/tool-layout.md` >
  `layout.md` current prose. Never let an artifact produced by an earlier item of this
  plan override these sources.

## Ratified design calls

Decided by the owner via AskUserQuestion, 2026-08-06:

1. **Cap accounting**: a collapsed tool block is its `✦`-header row plus at most
   **3 content rows** — target clipped to ≤2 wrapped rows ending in `" …"`, then the
   `+N more lines` marker. At most 4 screen rows total.
2. **No collapsed output preview**: collapsed shows only the clipped target; the marker
   counts the **entire** hidden body (sketch: `+5 more lines` above a 5-line output).
   Today's single body-preview line goes away for tool blocks.
3. **Grouping scope**: consecutive same-label, same-depth calls group **even when they
   carry bodies** (Run output, edit diffs). Answered user-question records, firing/
   schedule blocks, and sub-agent head blocks stay ungrouped.
4. **Glyphs**: expand/collapse indicators become `▶` (U+25B6) / `▼` (U+25BC),
   replacing `▸`/`▾` everywhere tool blocks wear them; `layout.md` updated to match.

Decided by the plan author from the sketch, 2026-08-06 (binding):

5. Marker wording is `+N more lines` — the current `"… "` prefix is dropped (the
   clipped target already ends in `" …"`). N counts hidden **body** lines only; a
   clipped target signals its own continuation via its trailing `" …"`.
6. A group header reads `✦ Label (N)` for N ≥ 2; the count renders in the faint
   indicator style (not the bold orange label style). The group header wears **no**
   indicator and is **not** a click target — members own their expand state.
7. An expanded group member ends with a right-aligned `see less…` row, sharing the
   prompt block's existing `see less…` constant (one vocabulary, one constant).
8. The expanded-member continuation gutter is `│` styled in the detail gray — never
   the orange sub-agent rail style, so the two cannot be confused when nested.
9. Expanded blocks paint their detail text in a new brighter gray `#b2b2b2`
   (`colFaint` is `#8a8a8a`); exact value tunable at review, the contrast step is the
   requirement.
10. Clicks toggle on motionless release only; drag remains text selection (the
    prompt block's pattern).

## Standing requirements (every item)

- skills: coding-standards
- ADR 0011: the Model is copied by value on every Update — no `strings.Builder` or
  other no-copy types anywhere it reaches; per-entry view state is written through the
  shared `t.entries` backing array (see `transcript.go` `setExpanded`).
- ADR 0030: paint lines and their click-marks are emitted in the same
  `blockPaint.add`/`join` call — never post-process finished lines.
- Any new paint-affecting state must join the paint cache key
  (`internal/tui/paintcache.go` — `paintKey`/`spanFlags`); a field missing there is a
  stale frame on screen.
- Width can change mid-session (settings live-apply can toggle the scrollbar): compute
  all layout from the `width` passed at paint time; never cache width-derived values
  outside the paint key.
- All measurement via `th.measure.Width` (the width authority), `expandTabs` before
  measuring — never `len()`.
- Run `make check` before each commit.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

## Out of scope

- The prompt block's own 3-row collapse (`promptCollapsedRows`) — unchanged.
- User-question layout, settings screens, and the in-flight settings-live-apply work
  (the dirty tree at write time belongs to plan `2026-08-06 - 00`; commit or clear it
  before executing this plan).
- Keyboard expand/collapse (deliberately absent per `layout.md`).
- Persisting expanded state (stays view-only, never serialized).
- Any version bump (see closing note).

---

## 1. Row-capped clip primitive — ✅ DONE (2026-08-07)

NOTES (2026-08-07): `docs/layout/tool-layout.md` was already committed to the repo
(`4a82dc3`) before this item ran, so the `git add` it asks for is a no-op — the plan's
visual source is in the repo, just not via this item's commit.

**What**: In `internal/tui/render.go` (near `hangingWrap`, ~line 1397), add a
row-capped variant — suggested shape
`clipWrap(th theme, style lipgloss.Style, marker, text string, width, maxRows int) (lines []string, clipped bool)`
— that wraps exactly like `hangingWrap` (same `hangingPrefixes`/`wrapText` path, same
`expandTabs`, same hanging continuation indent) but returns at most `maxRows` physical
rows; when input overflows, the last kept row is re-fit so a trailing `" …"` fits
within `width`, measured via `th.measure.Width`. No call sites change in this item.
Also `git add docs/layout/tool-layout.md` so the plan's visual source enters the repo
with this item's commit.

**Tests**: table test in `internal/tui/` covering: text that fits returns output
identical to `hangingWrap` and `clipped == false`; overflow at `maxRows` 1 and 2
returns exactly that many rows ending in `" …"` within width; wide runes (CJK) and
tab-bearing text stay within width (mirror the probes in `paint_test.go`
`TestPaintedTabBearingToolTargetKeepsItsColumn`).

**Acceptance**: `go test ./internal/tui/ -run 'Clip'` passes; `go build ./...` clean;
`git status` shows `docs/layout/tool-layout.md` staged/committed with the item.

**Commit**: `feat(tui): add row-capped clip primitive for tool layout`

## 2. Collapsed cap on targeted single blocks; ▶/▼; marker wording — ✅ DONE (2026-08-07)

NOTES (2026-08-07): `collapsedBodyCap` is retired from the TARGETED path only — the targetless
branch list still spends it, that shape being item 3's to rebind ("up to 2 branch rows"); retiring it
outright here would have meant doing item 3's work. The targeted body's own budget is the new named
zero `collapsedBodyRows`. Test collateral beyond the named list: `TestFiringBlockCollapsesToThe
AnswersFirstLine` is renamed `TestFiringBlockCollapsesToItsRemainderMarker` (its name stated the
retired invariant), the running Firing now wears an indicator (a one-line body is hidden like any
other), and `paintedDetailRows` (paint_test.go) expands its block so the per-line rune cap it probes
still reaches the grid instead of measuring the new row budget.

Depends on item 1.

**What**: In `internal/tui/render.go` + `theme.go`:

- Swap `glyphCollapsed`/`glyphExpanded` (`theme.go` ~84–85) to `▶`/`▼` (design call 4).
- Marker wording (`collapseAtCap`, `render.go` ~1266): `+N more lines`, no `"… "`
  prefix (design call 5). Marker keeps its `toolMarker` style and its open-only click
  meaning.
- Collapsed paint of a **targeted single** tool block (`renderToolBlock`/
  `renderToolBranch`): the target renders through `clipWrap` with `maxRows = 2`; **no
  body lines are painted** collapsed; if the block has a body, the marker row follows,
  with N = the full body length (design calls 1–2). The summary keeps riding the
  branch as today, inside the clipped rows.
- Expanded paint: unchanged shape — full target via `hangingWrap` (unclipped), full
  body (glyph swap aside).
- `blockHidesWhenCollapsed` (~line 1070) and the header-indicator predicate
  (~line 965) extend to: a block hides content iff it has a body **or** its target
  clipped at the current width. Indicator and click-target must remain **one
  predicate** (it becomes width-dependent — plumb `width` to it).
- Retire `collapsedBodyCap` from tool-block paths (the prompt block's own constants
  are untouched).

**Tests**: update `TestCollapsedPaintTruncatesRetainedBodies`,
`TestExpandedBlockPaintsItsWholeBody`, `TestRenderMarksHeaderAndMarkerLines`,
`TestHeaderIndicatorFollowsTheBlockState`, `TestRemainderMarkerCarriesItsOwnStyle` (and
any other test pinning `▸`/`▾` or `… +N`). New regressions: a 400-char command at
width 80 collapses to exactly 4 screen rows (header + 2 clipped rows + marker); a
long-target **bodiless** block wears the indicator, is a toggle target, and expands to
the full target.

**Acceptance**: `go test ./internal/tui/` passes; `go build ./...` clean.

**Commit**: `feat(tui): cap collapsed tool blocks at header plus three content rows`

## 3. Uniform cap across the remaining tool shapes — ✅ DONE (2026-08-07)

NOTES (2026-08-07): `collapsedBodyCap` keeps its name while its value goes 1 → 2 — the item rebinds
the budget, not the identifier — and the new per-line row budget beside it is `collapsedBranchRows`
(1), spent by a new `clipDetails`. Test collateral beyond the named list, all of it the same
arithmetic moving from one shown branch line to two: `TestRenderNoTargetStandalone` (its `{"a":1}`
premise no longer overflows the cap, so it now calls with two keys), `TestUnregisteredCallLabels
ItsArguments`, the targetless case of `TestRenderMarksHeaderAndMarkerLines`, and
`TestTranscriptLayoutGolden`. The new sweep test covers the targeted single block and the sub-agent
head as well as the three shapes the item names, since they are the shapes it asks be verified.

Depends on item 2.

**What**: Sweep every other tool-shaped block through the same budget (header +
≤3 content rows collapsed), in `internal/tui/render.go`: targetless branch lists
(`renderDetails`/`branchDetails` — bind: up to 2 branch rows shown, each clipped to
one row, then `+N more lines` counting all hidden rows), orphan results
(`renderOrphanResult`), schedule/firing blocks (the `entrySchedule` path), the
sub-agent head block (~line 423), and plain result blocks. Shapes that already flow
through the item-2 painter need only a verifying test; fix any straggler path that
still soft-wraps unbounded when collapsed.

**Tests**: update `TestTargetlessBlocksCollapseToTheBudget` to the new budget; add
per-shape regressions (schedule block, orphan result, targetless block) with overlong
content at width 60: collapsed paint ≤ 4 screen rows each.

**Acceptance**: `go test ./internal/tui/` passes.

**Commit**: `feat(tui): uniform collapsed cap across all tool block shapes`

## 4. Grouping: scope, (N) count, one-line members

Depends on items 1 and 2.

**What**: In `internal/tui/render.go` (+ `toolpresent.go` for the solo flag):

- `groupable` (~line 1339) becomes: a call groups iff `Target != ""` and the
  presentation is not marked solo. Drop the `Details.len() == 0` and
  `Summary.Kind == detailPlain` clauses (design call 3). Add an unexported `solo bool`
  to `toolView`, set by the ask_user presenter for answered question records (and any
  other presenter that must never group) — this replaces the body-exclusion as the
  never-group mechanism. `toolCallRun`'s run-breakers (kind, depth, label, intervening
  entries) are unchanged.
- Group header (N ≥ 2): `✦ Label (N)`, the `(N)` in the faint `toolIndicator` style;
  no expand indicator; header marked `targetNone` (design call 6). A lone groupable
  call keeps the item-2 single shape, no count.
- Member rows, all collapsed in this item: exactly **one screen row** each —
  `┝`/`┕`, target clipped via `clipWrap` `maxRows = 1` to the room left by the summary
  (if any) and the right-aligned indicator; the shared summary column survives where
  targets are short (existing padding logic); a summary is never dropped; the `▶`
  right-aligns at the block edge **only** when the member hides content (body or
  clipped target); an in-flight member paints a bare row, no indicator.
- Per-member expansion arrives in item 5; until then group members are inert
  (clicking does nothing on a group) — acceptable intermediate state.

**Tests**: flip the body-carrying case in `TestRenderGroupBreakers` (a call with
output now joins a run); retire `TestExpandedGroupPaintsIdentically` (the invariant
dies) and replace it with a collapsed-members-shape test; keep passing:
`TestFiringBlockJoinsNoToolGrouping`, `TestAnsweredAskUserBlocksNeverGroup` (now via
the solo flag), `TestRenderGroupsOneLineOutputCalls`,
`TestRenderGroupsDifferentToolsSharingALabel`. New: header count present for N ≥ 2 and
faint-styled, absent for a lone call; three Run-with-output calls group with one-row
members and right-aligned `▶`.

**Acceptance**: `go test ./internal/tui/` passes.

**Commit**: `feat(tui): group body-carrying calls with count and one-line members`

## 5. Per-member expand state, expanded-member paint, member click targets

Depends on item 4.

**What**: In `internal/tui/render.go`, `mouse.go`, `paintcache.go`:

- Read each member's own `entry.expanded` (the flag already exists per entry and is
  written through the backing array; today only the head's is read at ~line 218).
  Plumb per-member identity into `renderToolBlock` (member entry offsets or an
  expanded-flags slice) and return per-line member attribution in `blockPaint` so
  `renderView`'s `appendBlock` (~lines 160–167) can stamp the **member's** entry index
  into `lineTarget` — representation is implementer's choice (new `targetKind` or
  member-bearing `lineTarget`), but marks must be emitted in the same call as the
  lines (ADR 0030).
- `toggleBlockAt` (`mouse.go` ~484): a click on any row of a collapsed member expands
  that member; a click on any row of an expanded member (including its `see less…`
  row) collapses it; other members and the header are unaffected.
- Expanded member paint (per the sketch's "middle one expanded"): first row is the
  branch marker + the start of the full target with `▼` right-aligned; continuation
  rows carry a `│` gutter in the detail gray (design call 8); full target unclipped
  (`hangingWrap`), then the full body verbatim, then a right-aligned `see less…` row
  sharing the prompt block's constant (design call 7). No `+N` marker inside an
  expanded member.
- Paint cache: verify the group block's key covers **every** member's expanded bit
  (`spanFlags` packs `expanded` at bit 0 per entry — confirm `blockKey` spans the
  whole run, not just the head; extend if not).

**Tests**: mouse — clicking the middle member of three expands only it; clicking any
of its rows (and its `see less…`) collapses it; head and siblings unaffected;
`TestBlockMarksAgreeWithTheMouseMapping` extended to member marks. Paint — expanded
member matches the sketch shape (gutter, right-aligned `▼`, trailing `see less…`);
the same group railed inside a sub-agent paints the member gutter in detail gray,
distinct from the orange rail. Cache — toggling one member changes the painted block
(no stale frame).

**Acceptance**: `go test ./internal/tui/` passes.

**Commit**: `feat(tui): per-member expand state and click targets in tool groups`

## 6. Whole-block click surface for single blocks

Depends on items 2 and 5 (5 reshapes `lineTarget`; sequencing avoids rework).

**What**: In `internal/tui/render.go` + `mouse.go`: every painted row of a **single**
tool block (header, clipped target rows, and — when expanded — full target and body
rows) becomes a toggle target whenever the block hides content, under the same single
predicate as the indicator; the `+N more lines` marker keeps its open-only meaning.
Motionless release toggles; drag remains selection (design call 10; the prompt block's
whole-surface pattern at ~line 536 is the model). `toggleBlockAt` routes body-row
clicks to `toggleExpanded` on the block's head entry. Group members already got their
whole-row surface in item 5.

**Tests**: rework `TestRenderMarksHeaderAndMarkerLines` (body/target rows now marked);
mouse — clicking a body row of an expanded block collapses it and the anchor row stays
sane (`refreshViewportAnchored`); clicking the clipped target row of a collapsed block
expands it; a drag across body rows still selects and never toggles; a block with
nothing hidden marks no toggle rows.

**Acceptance**: `go test ./internal/tui/` passes.

**Commit**: `feat(tui): whole-block click surface for tool blocks`

## 7. Brighter detail gray for expanded blocks

Depends on items 2 and 5.

**What**: In `internal/tui/theme.go` + `render.go`: new color
`colFaintBright = "#b2b2b2"` (design call 9; value tunable) and a style field (e.g.
`toolDetailBright`); expanded single blocks and expanded group members paint their
branch/target/body text in the brighter gray; collapsed rows keep `colFaint`;
diff-colored lines (`detailDiffAdded`/`detailDiffRemoved`) keep their diff colors in
both states; the indicator and marker styles are unchanged. Thread expanded state into
`detailStyle`/`renderToolBranch`/`renderSubDetails` as needed. The expanded bit is
already in the paint key (verified in item 5) — no cache change expected.

**Tests**: SGR assertions — the same block's detail rows carry different foreground
SGR collapsed vs expanded; diff lines carry identical SGR in both states.

**Acceptance**: `go test ./internal/tui/` passes.

**Commit**: `feat(tui): brighter detail gray for expanded tool blocks`

## 8. Rewrite the layout prose to the shipped rules

Depends on items 1–7.

**What**: Rewrite `layout.md` sections "The rules behind the tool-call sketch" and
"Collapsed and expanded blocks" to state the new rules: header + ≤3-content-row cap
with clip semantics; no collapsed output preview; `+N more lines` wording; grouping
scope including body-carrying calls with the `(N)` count and one-line members;
per-member expand state; whole-block click surface (marker still open-only); `▶`/`▼`;
brighter expanded gray; group header inert. Name `docs/layout/tool-layout.md` as the
originating sketch. Cross-check the docstrings in `internal/tui/render.go` that quote
these sections by name and fix any quote that no longer matches the prose (prose-only
item otherwise — no behavior change). Check `CONTEXT.md` for collapse/expand
terminology needing sync; amend only if a term changed.

**Tests**: none (prose).

**Acceptance**: `! grep -q "four rows at most" layout.md`;
`! grep -q "▸" layout.md`; `go test ./internal/tui/` still passes;
`make check` clean.

**Commit**: `docs(layout): rewrite tool-block rules for cap, grouping and per-member state`

---

**Suggested version bump**: minor (v0.11.8 → v0.12.0) once executed — this is a
user-visible overhaul of transcript tool rendering. The owner decides; no item in this
plan touches VERSION or CHANGELOG release headings.
