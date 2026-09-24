# Run view: a three-row breadcrumb band, and the viewed run's own gauge — plan

**Goal:** An open run view's breadcrumb header becomes a three-row black band: a blank black row above the trail, the trail, and a blank black row below it. The whole band is one click target that goes one level up. Inside a run view, the status line's right slot states the **viewed run's** context gauge, never the top-level agent's.
**Date:** 2026-09-24
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 69565337

**Regression check (2026-09-24, 69565337):**
- 1: guard folded — the four header-indexing tests named and `render_test.go` added; the ⌥↓ breadcrumb stop's bar stays on the trail; a short screen freezes only the trail; `docs/manual/commands.md` joins the doc sweep, ADR 0063 changes only by dated amendment.
- 2: guard folded — the gauge test folds the child's usage by call id before the report; the way-back test sets `m.ctxUsed` after its setup check; `docs/manual/commands.md` joins the doc sweep.

**Sources:**
- `docs/adr/0063-sub-agent-runs-are-user-addressable-views.md` — D4
- `layout.md` — the run view section ("The breadcrumb is the header, and it is the way back", "`esc` means back before it means stop"), "Where it ends", "Where the window went"
- `CONTEXT.md` — "Run view" entry
- `IDEAS.md` — "Sub-Agents" items 1 and 2

**Ratified design calls** (owner, 2026-09-24):
- **Band:** the run view's breadcrumb row gains one blank row above and one below, painted on the same `th.breadcrumb` (black `surface`) field and squared to the full width. All three rows are `targetBreadcrumb`: a click on any of them goes one level up.
- **Spacer:** the unpainted, untargeted spacer row under the header stays. The sticky header is four rows: band, trail, band, spacer.
- **Umbrellas:** `✦ Sub-Agent (N)`, `✦ Skill (N)` and other grouped headers are unchanged.
- **Right slot in a view:** shows the viewed run's gauge (its head's `ctxUsed`/`ctxLimit`) once that run has reported usage. Until then it shows `esc back`, under the existing `runViewOwnsEsc` rule. The top-level gauge never shows inside a view. The breadcrumb band keeps advertising `esc back`.

**Standing requirements:**
- skills: coding-standards

**Out of scope:**
- Spacing of the `✦` umbrella headers in the transcript
- The footer's static window while a view is open
- A parent agent stopping or messaging its sub-agents (IDEAS item 3; see the handoff `docs/handoffs/2026-09-24 - 00 - sub-agent-control-handoff.md`)

## 1. The run view's breadcrumb is a three-row black band, clickable as one

**What:**
**Goal:** A rooted paint's sticky header (`renderedTranscript.header`) is four lines:
- Lines 0 and 2 are blank rows painted edge-to-edge on `th.breadcrumb`.
- Line 1 is the trail row (`breadcrumbRow`).
- Line 3 is the unpainted spacer, with a zero `lineTarget`.

Lines 0–2 carry `lineTarget{kind: targetBreadcrumb}`, so a motionless click on any of them leaves the view one level up. The four lines freeze together as the sticky header.
**Approach (assumed at the header base):** In `transcript.renderView` (`internal/tui/render.go`), the `root.rooted()` branch emits the trail row and then the spacer, with `header = userBlock{start: 0, count: 2}`.
- Add a band row before and after the trail. Use one helper next to `breadcrumbRow` in `internal/tui/subagentblock.go` that returns `th.breadcrumb.Render(squareLine(th.measure, "", width))`.
- Mark each band row `targetBreadcrumb` with `cells` `-1`, and set `count: 4`.
- Re-read every reader of `header.count`/`stickyHeaderSpan` (`render.go` `appendJoined` separator guard, `Model.stickyHeaderSpan`, `blockcursor.go`, `mouse.go` `targetBreadcrumb`) and confirm each one takes the count rather than assuming 2.
- Remove `IDEAS.md`'s first "Sub-Agents" item.

**Regression guard.** Every comment or doc line that states the header's row count or describes its rows ("TWO rows", "plus the blank spacer", "the first row of the view") is updated to the four-row shape. Find them with `grep -n -i -E 'two rows|spacer|header row|first row' internal/tui/*.go layout.md CONTEXT.md docs/manual/commands.md docs/adr/0063*`. The `layout.md` run-view sketch shows the band; `docs/manual/commands.md` (139–141) states the band as one click target. ADR 0063 (line 94) changes only by a dated amendment (item 2 adds one), never in place.
- The ⌥↓ breadcrumb stop's selection bar lands on the trail, never on a band row alone: either `highlightBlockCursor` shades every row of the `targetBreadcrumb` surface (lines 0–2), or the stop `cursorStops` yields for that surface is the trail row (line 1).
- A short screen keeps the view's content: when `transcriptRows() <= header.count+1`, `Model.stickyHeaderSpan` freezes only the trail (start 1, count 1), so at height 12 (`transcriptRows()==4`) the view still draws run lines under the trail, and `followBlockCursor`'s `header < rows` stays reachable.

**Files:** internal/tui/render.go; internal/tui/subagentblock.go; internal/tui/blocktarget.go; internal/tui/runview.go; internal/tui/model.go; internal/tui/blockcursor.go; internal/tui/subagentblock_test.go; internal/tui/runview_test.go; internal/tui/mouse_test.go; internal/tui/render_test.go; layout.md; docs/manual/commands.md; IDEAS.md
**Read first:** internal/tui/render.go — transcript.renderView, appendJoined; internal/tui/model.go — Model.stickyHeaderSpan, drawnLineAt; internal/tui/blockcursor.go — cursorStops, highlightBlockCursor;
internal/tui/subagentblock.go — breadcrumbRow; internal/tui/render_test.go — TestRunViewHeaderIsDrawnByTheStickyOverlay

**Tests:**
- `subagentblock_test.go`: `TestRunViewHeaderIsABandOfThreeBreadcrumbRows`. Paint rooted at a run. Assert:
  - lines 0–2 are full width on the breadcrumb field (the way `TestBreadcrumbRowIsPaintedEdgeToEdgeOnTheSurfaceField` checks it);
  - line 1 holds the trail and lines 0 and 2 hold no glyphs;
  - targets 0–2 are `targetBreadcrumb`, line 3 is empty with a zero target;
  - `header.count == 4`.
- `mouse_test.go` / `runview_test.go`: a click on band row 0 and a click on band row 2 each leave the view one level up. Model these on the existing breadcrumb-click test.
- Re-point the four existing tests that index the header rows to line 1 = trail, lines 0/2 = band, line 3 = spacer: `TestRootedPaintRegistersNoUserBlock` and `TestRunViewHeaderIsDrawnByTheStickyOverlay` (`render_test.go`, count 4), `TestRunViewBreadcrumbHintFollowsTheKey` (`runview_test.go`, its `header(t, m)` reads `m.lines[1]`), `TestFinishedRunSaysItsReportOnce` (`subagentblock_test.go`, trail on line 1, lines 0 and 2..task blank).
- `runview_test.go`: ⌥↓ inside a view puts the block-cursor highlight on the trail row.
- `runview_test.go` / `render_test.go`: at height 12 an open view freezes only the trail and draws run content beneath it.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -race -count=1 -run 'Breadcrumb|RunView|Header|Sticky' ./internal/tui/`
- `go test -race -count=1 ./internal/tui/`

**Commit:** `feat(tui): the run view's breadcrumb is a three-row band, clickable as one`

## 2. Inside a run view the status line states the viewed run's gauge

**What:**
**Goal:** While a run view is open, `Model.statusRight` never renders the top-level gauge (`m.ctxUsed` against `m.opts.ContextWindow`).
- Once the viewed run's head reports usage, the slot renders that run's gauge: its `ctxUsed` against its `ctxLimit`, falling back to `m.opts.ContextWindow` when `ctxLimit` is 0.
- Before that, the slot falls through to the existing hints, so a view with no child usage reads `esc back`.
- A primed ctrl+c, a primed esc and a flash still take the slot first.
- At the top level the slot is unchanged.

**Approach (assumed at the header base):** This fixes a defect against ADR 0063 D4. `statusRight` returns `contextGauge()` before the run-view branch, so inside a view it shows the **parent's** fill and `esc back` never appears once the parent has usage. `TestRunViewStatusSlotOffersTheWayBack` passes only because its fixture has `ctxUsed == 0`.
- `contextGauge` reads the innermost viewed run via `m.viewedChild()` (`internal/tui/runview.go`). When there is one, it builds `contextUsage` from that head's `ctxUsed`/`ctxLimit` (written by `transcript.applyUsage`, limit from `childWindow`). One function stays the single owner of which gauge the chrome shows.
- Docs, owned by this item:
  - Add a dated amendment to ADR 0063 D4.
  - Update the `CONTEXT.md` "Run view" entry and `layout.md`'s "`esc` means back", "Where it ends" and "Where the window went" paragraphs (the last currently says the chrome "gains nothing from a delegate having run").
  - Update `internal/tui/doc.go`'s run-view note.
- Remove `IDEAS.md`'s context-gauge "Sub-Agents" item.

**Regression guard.** Every doc or comment line that says the right slot reads `esc back` "while a view is open", or that the chrome's gauge is the top-level one, is updated. Find them with `grep -n -i -E "right slot|esc back|gauge" CONTEXT.md layout.md docs/manual/commands.md docs/adr/0063* internal/tui/doc.go internal/tui/model.go internal/tui/runview.go` — `docs/manual/commands.md` (140–141) included.
- The gauge test's usage reaches the head by call id, not run id: `subAgentCall`'s head carries no run id (`entry.headsRunFor` falls to the legacy call-id key) and `openSubAgentHead` skips a done head, so the child's `UsageEvent{EventBase{Depth: 1, CallID: "s1"}, TotalTokens, ContextWindow}` is folded between `subAgentCall` and `subAgentReport` (or on a live run).
- `TestRunViewStatusSlotOffersTheWayBack` sets `m.ctxUsed` after its `esc×2 stop` setup assertion and before `enterOnLastBlock`, so the setup check still reads the stop gesture.

**Files:** internal/tui/model.go; internal/tui/runview.go; internal/tui/doc.go; internal/tui/runview_test.go; docs/adr/0063-sub-agent-runs-are-user-addressable-views.md; CONTEXT.md; layout.md; docs/manual/commands.md; IDEAS.md
**Read first:** internal/tui/model.go — Model.statusRight, Model.contextGauge; internal/tui/runview.go — Model.viewedChild, Model.runViewOwnsEsc;
internal/tui/transcript.go — transcript.applyUsage, openSubAgentHead, entry.headsRunFor; internal/tui/runview_test.go — TestRunViewStatusSlotOffersTheWayBack

**Tests:**
- `runview_test.go`: `TestRunViewStatusSlotOffersTheWayBack`. Set a nonzero top-level `m.ctxUsed` after the setup assertion and before entering the view, and still expect `breadcrumbHint`. Bite: this fails on the base tree.
- `runview_test.go`: `TestRunViewGaugeStatesTheViewedRun`.
  - Fold the child's depth-1 `UsageEvent` (`CallID: "s1"`, a `ContextWindow` different from the session's) between `subAgentCall` and `subAgentReport`. Open its view.
  - Assert the slot shows the child's used/limit and not the parent's.
  - Back out and assert the parent's gauge returns.
  - For a nested view, assert the innermost run's gauge.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -race -count=1 -run 'RunView|StatusRight|Gauge|ContextUsage' ./internal/tui/`
- `go test -race -count=1 ./internal/tui/`. Bite check: the amended `TestRunViewStatusSlotOffersTheWayBack` and `TestRunViewGaugeStatesTheViewedRun` fail on the base tree.

**Commit:** `fix(tui): a run view's status line states the viewed run's gauge, not the parent's`
