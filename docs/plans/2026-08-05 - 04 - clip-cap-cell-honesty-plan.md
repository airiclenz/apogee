# Clip-cap cell honesty — implementation plan

- **Goal:** close the two remaining clip-path entries in ISSUES.md — the confirmed rune-vs-cell defect in the status line's target cap (a double-width target clipped to 32 runes paints up to 64 cells and pushes the context gauge off an 80-column row), the SUSPECTED-UNPROBED twin at the transcript detail cap, and the stale "139 cells" figure two comments disagree about.
- **Date:** 2026-08-05
- **Status:** not started
- **Authoritative sources:**
  - ISSUES.md — the rune-vs-cell entry (the `clipRunes`/`statusTargetRunes` paragraph plus its SUSPECTED `clipDetail` paragraph) and the stale-139 entry beneath it. Each item below removes exactly the entry it resolves.
  - `internal/tui/width.go` — the width authority: ONE display-width measure for the whole TUI, the painter's own, riding on `theme` (`th.measure`). Any cell-spent budget in this plan goes through it; a hard-coded `ansi` method is the defect that file exists to prevent.
  - `internal/tui/activity.go:117-121` — the cap's own contract: `statusTargetRunes` exists so the left slot "must not push the gauge off the line". The promise is CELLS; the rune spend is the breach. Value 32 is the settled budget.
  - `internal/tui/paint_test.go:887-913` — the probed figures: a 32-rune tab-bearing target painted **91** cells (the correct number; the "139" at `activity.go:157` is the stale one), and `TestPaintedTabBearingToolTargetKeepsTheGauge` is the fixture pattern item 1 extends.
  - Tab-sweep precedent: 35f4245 (`expandTabs` before the cap at this very site), completed by 1015b9b, 92786ca, 86ae843, 6cd6b3b, 5a479a1. The tab half is FIXED and stays fixed — `expandTabs` remains in front of the cap.
- **Ratified design calls:**
  1. **Detail cap keeps its rune spend; resolved by probe + pin + document** (owner, 2026-08-05, via AskUserQuestion in the planning session). No threading of the width authority into the presenter layer, no fixed-method cell clip. A paint test probes the suspected half and pins the bounded behavior (extra soft-wrapped rows, nothing displaced, ≤2× the nominal rows since one rune paints at most two cells), and the comments state the bound as deliberate.
  2. **Status cap spends CELLS via the width authority, value unchanged at 32** (plan author, 2026-08-05 — derived, not invented: the cap's documented intent at `activity.go:117-121` is a cell budget, and `width.go` binds every cell measure to the painter's method). The ellipsis is spent INSIDE the budget (`measure.Truncate(s, 32, "…")` ≤ 32 cells total), where `clipRunes` appended it beyond (32 runes + "…"); the budget is the promise, so the tighter spelling is the correct one.
- **Standing requirements:**
  - skills: coding-standards
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:**
  - Changing `detailClipRunes`' value or `clipDetail`'s clipping semantics — item 2 touches only comments, tests, and ISSUES.md (ratified call 1).
  - Threading the width authority into the presenter layer (`presentToolCall`'s registry, `transcript.addPresented`, `schedule.go`) — foreclosed by ratified call 1.
  - `uiPresenter.climb`'s pre-clips (`presenter.go:124,133`) — redundant (its Reason is re-clipped authoritatively by `addPresented`, `transcript.go:252-256`) but harmless; untouched.
  - The block-cursor keyboard ISSUES entry — deliberately deferred there; not this plan's business.
  - The uncommitted `cmd/apogee/probe*` stdout work in the tree — unrelated in-flight work; no item touches those files.

## 1. Spend the status-line target cap in cells via the width authority — ✅ DONE (2026-08-06)

NOTES (2026-08-06): beyond the item's literal list — a CHANGELOG `[Unreleased] / Fixed` entry (repo
convention for a user-visible fix); the `statusTargetRunes` mention in `paint_test.go`'s fixture
comment rewritten too (the item's own grep acceptance requires it); and the unit clip test
(`activity_test.go`) gained a double-width sub-case beside the ASCII one while its rune arithmetic
was converted to a cell assertion. `subAgentSummary` took the measure parameter as the minimal
thread from `renderSubAgentRun`'s `th.measure` down to `subAgentGist`.

**What:** Move the cap's spend from runes to the cells it promised, at the cap site.

- `internal/tui/activity.go`: rename `statusTargetRunes` → `statusTargetCells` (value stays 32) and rewrite its comment for the new spend. `toolPhrase` gains the measure — `toolPhrase(measure widthAuthority, tv toolView)` — and clips with `measure.Truncate(expandTabs(tv.Target), statusTargetCells, "…")`. `expandTabs` STAYS in front of the cap: a tab is rewritten to its spaces by the style before any measure reads the result (the existing comment's reason; tab-sweep precedent above), so the cap must count the expanded form.
- Thread the measure to both callers: `toolActivityLabel` (`activity.go:137`, called at `activity.go:202` where `m.th.measure` is in reach) and the collapsed-run gist chain in `internal/tui/render.go` (`subAgentGist` at render.go:430, plus whatever of its enclosing summary-builder chain needs the parameter — thread minimally; the theme already travels the render layer).
- Rewrite the `toolPhrase` comment block (`activity.go:152-160`): the rune-spend story it tells becomes history. The stale "**139 cells**" figure dies with the rewrite — the probed figure is **91** (`paint_test.go:897, :913`, which are correct and stay). Any surviving mention of the old defect states the fix, not the breach.
- `internal/tui/toolpresent.go`: `clipRunes` loses this caller and keeps `clipDetail` as its only one — update its comment's mention of `statusTargetRunes` (item 2 finishes that comment's story; here it just must not dangle).
- `ISSUES.md`: remove the stale-139 entry (the "Two comments left by 35f4245 disagree" paragraph) — resolved by the comment rewrite. The rune-vs-cell entry stays for item 2.
- Behavior note (binding, from ratified call 2): the phrase now totals ≤ 32 cells including the ellipsis where it was 32 runes + "…" before; a plain-ASCII target under the cap is unchanged. Update any test arithmetic that expected 33.

**Tests:** extend `TestPaintedTabBearingToolTargetKeepsTheGauge` (`paint_test.go:911`) with a double-width target case — a CJK path past the cap either way, e.g. `strings.Repeat("字", 32) + ".go"` — asserting the context gauge survives on the 80-column status row, under both paint methods, alongside the existing plain and tab-bearing cases. This is the probe the ISSUES entry called for, landed as the regression test: it must FAIL against the rune spend and PASS after. Update the signature at `toolPhrase`/`toolActivityLabel` test call sites (`activity_test.go:86,93`, `transcript_test.go:643,652`, `workspacepath_test.go:363`).

**Acceptance:**
- `go test ./internal/tui/ -run 'TestPaintedTabBearingToolTargetKeepsTheGauge' -v` — all cases pass, including the new double-width one.
- `go test ./internal/tui/` passes.
- `grep -rn "139" internal/tui/activity.go` finds nothing; `grep -n "statusTargetRunes" internal/tui/` finds nothing.
- ISSUES.md no longer contains the "Two comments left by 35f4245 disagree" entry.
- `make check` passes.

**Commit:** `fix(tui): spend the status target cap in cells via the width authority`

## 2. Probe and pin the detail cap's bound; document the deliberate rune spend

Depends on item 1.

**What:** Resolve the SUSPECTED half the way ratified call 1 states — no behavior change; the probe becomes the pin.

- `internal/tui/paint_test.go`: a paint test (suggested name `TestPaintedWideDetailLineWrapsWithoutDisplacement`) drives a tool result whose detail line is double-width text past the cap (e.g. ≥ `detailClipRunes` of CJK) through the transcript and asserts the bound the comments will state: the line clips at 160 runes, soft-wraps into extra rows, **no neighbouring element is dropped or displaced** (the entry painted after it is still present and intact), and the detail's painted row count is at most twice that of a same-rune-count ASCII detail (the ≤2-cells-per-rune bound). The test's comment records this as the probe the ISSUES entry said nobody had run.
- `internal/tui/toolpresent.go`: rewrite the `detailClipRunes`/`clipDetail`/`clipRunes` comments (`toolpresent.go:954-972`) to state the settled design: the detail cap is a flood/size bound deliberately spent in runes — one rune paints at most two cells, so 160 runes bound 320 cells and at most ~2× the nominal soft-wrapped rows; the transcript soft-wraps and displaces nothing, so cell-exactness is the STATUS LINE's requirement (item 1's cells spend via the width authority), not the transcript's. Name the pinning test.
- `ISSUES.md`: remove the whole rune-vs-cell entry (both paragraphs) — the confirmed half is fixed by item 1, the suspected half is probed and pinned here.

**Tests:** the probe test above IS the test of this item.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestPaintedWideDetailLineWrapsWithoutDisplacement' -v` passes (adjust the name to what the implementer chose if a NOTES line records a deviation).
- `go test ./internal/tui/` passes.
- ISSUES.md no longer contains the `clipRunes` rune-vs-cell entry; the only remaining `[ ]` entry is the block-cursor keyboard one.
- `make check` passes.

**Commit:** `test(tui): probe and pin the detail cap's row bound; document the rune spend`

---

**Suggested version bump:** none required by this plan; if the owner cuts a release after it lands, patch level (v0.11.3) covers it — a user-visible status-line fix and a documentation/test hardening. The bump is the owner's call and no item performs one.
