# Pop-up transcript-tail occlusion — fix plan

**Goal:** while any pop-up pane is open, the transcript's last rows must be reachable by
scrolling (and by the block cursor), as `layout.md` already promises — today exactly the
pane's height of tail lines is unreachable at every scroll position.

**Date:** 2026-08-23 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:**
- The defect: `ISSUES.md:26` (open-defects section).
- The contract the fix restores: `layout.md:4-15` (the view follows the tail, nothing is
  padded below the content) and `layout.md:119-122` (the approval and ask prompts "leave
  the transcript scrollable under them").
- The one row derivation: `Model.transcriptRows()` — `internal/tui/model.go:1957-1968`
  declares it "THE derivation" of the transcript's drawn rows.
- Constraints: ADR 0011 (value-copied `Model` — cached geometry must be plain scalars),
  ADR 0030 (widths through `m.th.measure` only; heights via `lipgloss.Height`),
  ADR 0043 (one file per concern).

**The mechanism (established 2026-08-23 by code reading, recorded so no item re-derives
it):** `layout()` sets the viewport widget's height to `transcriptBudget()`
(`internal/tui/model.go:1714`) — the full budget, overlay-blind — while `View` paints only
`transcriptRows()` = budget − `frameOverlays().height()` rows (`model.go:2183-2189`). The
bubbles v2 viewport clamps every scroll to `maxYOffset = total − Height()`
(viewport.go:303-306, 464-466, 591-593), so with `H` = the open panes' measured height,
the last `H` content lines can never be painted. Side-symptoms of the same arithmetic:
`m.detached` stays false while the tail is off-screen, the scrollbar thumb never reaches
the bottom, the block cursor cannot enter the hidden band, and `PageDown` steps by more
than a visible screenful.

**Ratified design calls (decided at write time, 2026-08-23):**
1. **Fix = flip the widget's height authority** (the report's Approach A): `layout()`
   feeds the widget `transcriptRows()` instead of `transcriptBudget()`. The clamp-only
   alternative is unreachable (the widget exposes no unclamped offset path) and padding
   the content below is forbidden by `layout.md:4-8` and `refreshViewport`'s own doc
   (`model.go:1809-1812`). Decided by the plan author from the evidence above; no
   user-visible variant exists.
2. **Compensate with the MEASURED overlay height** (`frameOverlays().height()`,
   `lipgloss.Height` sums), never `frameRowPlan`'s grant — a pane may draw shorter than
   its grant (`reportpane.go:47-53`).
3. **`layout()` stays the single setter of the widget height.** No second setter in
   `refreshViewport`; instead, item 2 audits that every pane-height change reaches
   `layout()`. The feedback-loop fear in the old comments does not apply:
   `transcriptBudget()` derives from `m.height`/`frameFixedRows`/`inputBoxRows()` only,
   and nothing in the package reads `m.viewport.Height()` back into layout.
4. **The `max(1, …)` floor stays** on the widget height: when `transcriptRows()` is 0
   (e.g. the settings pane claims the whole budget), the widget is 1 row tall and paints
   0 — the widget cannot be zero-height and remain a scroll surface.

**Standing requirements:** skills: coding-standards.

**Out of scope:** any change to `frameRowPlan`'s row allocation or `transcriptReserve`;
any change to pop-up rendering (`popup.go`) or pane give-way order; the wheel-routing
chain in `mouse.go` (already correct); `layout.md` prose (it already states the desired
behaviour); the ISSUES.md pop-up entry's *other* wish-list ideas — this plan fixes
reachability only.

---

## 1. Flip the viewport widget's height to the drawn row count — ✅ DONE (2026-08-23)

NOTES (2026-08-23): no CHANGELOG entry in this sidecar — item 3's text owns the defect's closed trail ("record the fix under [Unreleased]"), so an entry here would duplicate it.

NOTES (2026-08-23): rewrote a third comment beyond the two the item names — layout()'s own doc comment stated the old rule ("the viewport gets the height left after … the ▁ hairline"), which the change makes false; one clause added naming the overlay subtraction.

NOTES (2026-08-23): the adjusted TestMouseClickOnOverlayRowsArmsNoSelection renames its local laidOut → budget (it now reads m.transcriptBudget()); its walked row range is unchanged.

NOTES (2026-08-23): guard test verified both ways — TestTranscriptTailReachableWithAPaneOpen fails against the pre-fix SetHeight(transcriptBudget()) line and passes after the flip.

**What:** In `internal/tui/model.go`, change `layout()`'s
`m.viewport.SetHeight(max(1, m.transcriptBudget()))` (`:1714`) to set
`max(1, m.transcriptRows())`, composing the overlays once in `layout()` for the purpose
(the same composition `View` performs each frame). Rewrite the two comment blocks that
document the old rule — `model.go:1727-1733` and the `transcriptRows` doc at
`:1957-1966` — to state the new rule: the widget's height IS the drawn height, the
clamp is its fourth reader, and the budget/overlay derivation never reads the widget
back (no feedback loop). `View`'s local `vp.SetHeight(transcriptHeight)`
(`model.go:2185`) stays as a harmless re-assertion. Fix the one test whose setup
depends on the old mismatch: `internal/tui/mouse_test.go:2453-2463`
(`TestMouseClickOnOverlayRowsArmsNoSelection`) asserts `drawn < laidOut` via
`m.viewport.Height()` — compare against `m.transcriptBudget()` instead. Add the guard
test the defect fell through the absence of: with an ask prompt open and transcript
content longer than the drawn rows, `GotoBottom`/scroll-to-max makes the LAST content
line paintable (assert via the rendered frame or `YOffset() + transcriptRows() >=
total lines`), and `AtBottom()`/`m.detached` agree with what is on screen.

**Files:** `internal/tui/model.go`, `internal/tui/mouse_test.go`,
`internal/tui/model_test.go`

**Tests:** the new tail-reachability guard test (ask prompt open); the adjusted
`TestMouseClickOnOverlayRowsArmsNoSelection`; existing frame invariants
(`TestFrameNeverExceedsTheTerminalHeight`, `TestFrameFitsEveryHeightDownToItsFloor`,
`TestOverlayPaneSitsFlushOnBottomChrome`, `TestPopupBudgetShrinksToNothing`) must pass
unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): scroll clamp follows the drawn transcript rows, not the full budget`

## 2. Freshness audit — every pane-height change must reach layout() — ✅ DONE (2026-08-23)

NOTES (2026-08-23): audit verdict, one row per enumerated `refreshViewport()`-without-`layout()` site (plan line numbers, as of `1f0d72c3`):

| site | verdict |
| --- | --- |
| `spinner.go:412` | cannot change pane height — the live-star blink repaints transcript lines only |
| `heartbeat.go:184` (`foldBeatMsg`) | **fixed: now lays out** — a beat replaces `m.hb.models`, which is the offering an open `/model` picker derives its rows from (`pickerFilteredView`) |
| `heartbeat.go:335` (`applyPendingRebind`) | cannot — bindings plus one transcript note |
| `heartbeat.go:547` (`foldRoutingNotice`) | cannot — one ephemeral note |
| `actuation.go:355` (`foldActuation`) | cannot — one narration note |
| `actuation.go:437` (`foldActuationDone`, error) | cannot — one error note |
| `actuation.go:449` (unload outcome) | cannot — one note |
| `actuation.go:456` (load landed) | cannot — the note plus `recordLoadedProfile`, which writes only the transcript |
| `actuation.go:569` (`foldRestore`) | cannot — one note |
| `sessions.go:356` (delete confirm `y`) | cannot — the armed confirm is a fourth CELL on an existing row, not a row; the delete's re-list repaints through `foldSessionList`, which now lays out |
| `sessions.go:506` (load failure) | cannot — one note; the browser closed before the load was dispatched |
| `sessions.go:511` (restore failure) | cannot — same |
| `commandrun.go:31` (`refuseUnknownSlash`) | cannot — one note |
| `commandrun.go:427` (`foldCompactDone`) | cannot — `finishWorker` already lays out ahead of it |
| `interject.go:272` (`foldInterjected`) | **fixed: now lays out** — delivered rows leave the staged band, so the band is drawn shorter or not at all |
| `model.go:783` (`eventMsg`) | **fixed: now lays out** — `foldEvent` feeds the Inspector's ring (`foldWire`) and the `/usage` accounting, and both report panes derive their rows from what it wrote |
| `model.go:803` (`foldScheduleEvent`) | cannot — a Firing block or one note |
| `model.go:817` (`presentedMsg`) | cannot — one transcript entry |
| `model.go:1409` (actuation-block note) | cannot — one note |
| `model.go:1419` (upstream-block note) | cannot — one note |
| `model.go:1566` (`foldCancelled`) | cannot — `finishWorker` already lays out ahead of it |
| `model.go:1590` (`foldLoopError`) | cannot — same |
| `sessionsave.go:293,297` (not enumerated; same grep) | cannot — save-failure and recovery notes |
| `model.go:1886` (`refreshViewportAnchored`, not enumerated) | cannot — a transcript block toggle |

NOTES (2026-08-23): deviation — five sites OUTSIDE the enumerated list needed the same fix, because the item's premise that "open/close edges are already covered" does not hold. `sessions.go:119`, which the item cites for the browser, is the unwired-host degrade path, not the open edge. Fixed, each with its own case in the new guard test: `autocomplete.go:654` (esc closes the dropdown — the one `dismissAutocomplete` caller with no `layout()` after it), `sessions.go:157` (`foldSessionList`'s SUCCESS path opens or refreshes the browser), `sessions.go:315` (`^a` widens the browser to every workspace — a different row set), `sessions.go:297` (a filter rune prunes the browser's rows), `picker.go:632` (a filter rune prunes the picker's rows). Leaving them would have made the item's own title false.

NOTES (2026-08-23): `foldBeat` now also reports "the view changed" when an OPEN picker's row count moved under the beat, counted through `pickerCount()` before and after the offering is replaced (and only while the pane is up, so the ten-second repaint economy the fold's comments guard is untouched). Without it a beat that moved nothing else repainted nothing at all, and the enumerated `heartbeat.go:184` fix alone would not have reached the stale clamp. Its doc comment gained the third mover.

NOTES (2026-08-23): the eight regression tests are ONE table-driven guard, `TestPaneHeightChangeReachesLayout` in `model_test.go`, rather than one test per file: the invariant under test is `layout()`'s, and eight copies of the same three assertions would have been eight places for it to drift. Verified both ways — every subtest fails with the six source files stashed and passes with them in place. `model_test.go` gained the `internal/heartbeat` and `internal/session` imports its arrangements need.

NOTES (2026-08-23): no CHANGELOG entry in this sidecar — item 3's text owns the defect's closed trail ("record the fix under `[Unreleased]`"), and this item is the same defect, so an entry here would duplicate it. Item 1's sidecar declined for the same reason.

NOTES (2026-08-23): the invariant "every pane-height change reaches `layout()`" has no structural enforcement — it is now correct at thirteen call sites and nothing fails when the fourteenth forgets. A guard that walks every pane and asserts the widget height after each mutation route is not derivable from the code, so the durable answer is probably a `layout()`-on-claim rule in `handleKey`'s claimant walk (one line, covers every pane keypress) traded against a second full transcript render per pane key. Recorded as an idea only; not in this plan's scope.

Depends on item 1.

**What:** With `layout()` the single setter (ratified call 3), a pane whose DRAWN height
changes without a `layout()` call leaves a stale scroll clamp. Open/close edges are
already covered (`ask.go:41`, `approval.go:64,:170`, `picker.go:218`, `sessions.go:119`,
`settings.go:339`, `usage.go:91`, `inspector.go:127`, `autocomplete.go:279,838,876`,
`interject.go:203,239,314` all go through `layout()`). Audit the enumerated
`refreshViewport()`-without-`layout()` sites for content-driven pane-height changes —
`spinner.go:412`, `heartbeat.go:184,335,547`, `actuation.go:355,437,449,456,569`,
`sessions.go:356,506,511`, `commandrun.go:31,427`, `interject.go:272`,
`model.go:783,803,817,1409,1419,1566,1590` (line numbers as of `1f0d72c3`; re-locate by
call site if drifted) — and for each site that can change an open pane's drawn height
(the staged-interjection band and the autocomplete dropdown are the likeliest), route it
through `layout()` (or have it call `layout()` before `refreshViewport()`). Record the
audit verdict per site as a table in a dated NOTES line under this item: site → "cannot
change pane height" or "fixed: now lays out". Add one regression test for each site that
needed fixing (grow the staged band / dropdown while open; assert the widget height
tracks `transcriptRows()`).

**Files:** `internal/tui/interject.go`, `internal/tui/autocomplete.go`,
`internal/tui/model.go`, `internal/tui/interject_test.go`,
`internal/tui/autocomplete_test.go` (final set per audit; stay within `internal/tui/`)

**Tests:** the per-fixed-site regression tests; `TestScrollMidHistoryHoldsPositionOnAppend`
and `TestScrollWhileRunningViaPgKeysAndWheel` must pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): pane-height changes re-lay out so the scroll clamp stays fresh`

## 3. Symptom sweep — pin the behaviours the flip repairs, close the defect

Depends on items 1 and 2.

**What:** Add tests pinning the side-symptoms so they cannot regress independently, all
with a pane open and content overflowing: (a) the scrollbar thumb reaches the bottom of
its track at max scroll (`renderScrollbar`/`scrollbarThumb`, `boxdraw.go:144-153`) —
production code should need no change, the fix falls out of the clamp; (b) the block
cursor can reach and toggle the last transcript block (`blockcursor.go:184-229`);
(c) `PageDown` advances by the drawn height, i.e. one visible screenful. Verify
`paint_test.go:130-137` (`transcriptPaintRows`) and `mouse_test.go:1985-1988` still hold
now that `viewport.Height()` equals the drawn height — adjust only if red. Then close the
defect: remove the pop-up-covers-last-rows entry from `ISSUES.md` (`:26`) and record the
fix under `[Unreleased]` in `CHANGELOG.md` (the changelog is the sole closed trail).

**Files:** `internal/tui/model_test.go`, `internal/tui/blockcursor_test.go`,
`internal/tui/mouse_test.go`, `internal/tui/paint_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the three new symptom tests; full `internal/tui` suite green.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `test(tui): pin tail reachability symptoms; close the pop-up occlusion defect`

---

**Deviation rule:** any authorized deviation from item text lands as a dated NOTES line
under the item.

**Suggested version bump:** patch (a user-visible defect fix, no new surface) — the
owner decides; no item changes VERSION.
