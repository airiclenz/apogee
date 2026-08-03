# Popup spacer-row consistency plan

**Goal:** every overlay pane sits flush on the bottom chrome — no pane paints an empty
row between its bottom border and the ▔ hairline — so the `/sessions` browser and the
`/model` | `/server` picker read consistently with the `/`, `@` and skill autocomplete
dropdown.

**Date:** 2026-08-03
**Status:** ready to execute
**Issue:** `ISSUES.md` — "pop-up lists like /sessions, /skills or @file are painted
inconsistent. Some have an empty row between the bottom prompt/status section and some
have not: server: unwanted spacer row / session: unwanted spacer row" (marked `[P]`).

**Authoritative sources**

- `internal/tui/model.go` — `Model.View` (the frame stack, currently lines 2746–2814 at
  commit `ba4e257`), `frameOverlays` (2670–2715), `frameFixedRows` (2441),
  `transcriptBudget` (2514).
- `layout.md` — "What 'height' means: one row budget, and the transcript pays it"
  (the overlay-slot prose) and the eight-row frame-floor paragraph.
- Pinned baseline: commit `ba4e257` (originally pinned at `8fe54b6`; all line references
  re-verified unchanged at `ba4e257`, 2026-08-03, after the table-cell-wrapping wave
  landed — its only `model.go` touch was one line in `inputElisionEdge`, no shift).

**Root cause (verified 2026-08-03):** `Model.View` stacks the frame as
`transcript → [prompt] → [browser] → [picker] → "" (gap row) → ▔ topRule → status →
[dropdown] → [queued] → input box → footer → ▁`. The blank gap row is appended
unconditionally AFTER the transcript-side panes (`model.go:2798`), so any pane in that
slot — the `/sessions` browser, the picker (`/model`, `/server`, and the launcher-load
picker), and the approval/ask prompt — shows one empty row between its bottom border and
the ▔ hairline. The autocomplete dropdown and the staged-interjection band sit BELOW the
status line, flush against the input box, with no such spacer — that asymmetry is the
reported inconsistency.

**Chosen fix:** reorder the stack so the gap row sits ABOVE the slot:
`transcript → "" → [pane] → ▔ → status → …`. With no pane open the frame is unchanged
(blank row above the ▔, exactly as today). This is a pure stacking reorder — the frame
still contains exactly one gap row in every composition, so `frameFixedRows`,
`transcriptBudget`, `frameOverlays.height()`, `frameRowPlan`, and the mouse's
transcript bound (`pointTranscriptRow` / `contentLineAt`) all stay correct untouched.
The rejected alternatives, for the record: dropping the gap row while a pane is open
(touches the `frameFixedRows` const arithmetic and the D2/FOLLOW-UP-K floor invariants
for no visual gain), and moving the browser/picker into the dropdown's slot below the
status line (contradicts layout.md's slot prose and moves a modal surface for a
one-row cosmetic fix).

**Standing requirements**

- Forward `skills: coding-standards` when executing this plan (Go extension applies).
- **Precondition:** execute only on a clean working tree. SATISFIED 2026-08-03: the
  table-cell-wrapping work (plan `2026-08-03 - 04`) landed as `7bf48d0`…`ba4e257`; the
  tree holds only untracked docs plus a leftover `internal/tui/zzscratch_test.go`
  scratch file (delete it before executing — it is not part of this plan).
- Any authorized deviation from item text must land as a dated NOTES line under the item.

**Out of scope**

- Every other ISSUES.md entry (tool-header click responsiveness, sub-agent context
  usage, keyboard collapse/expand, inline skill accents, the `/skill` idle-only tag,
  queued-message ghosts).
- Any change to `renderPopup` / `popup.go` (the pane painter is not the problem).
- Any change to the frame's row arithmetic (`frameFixedRows`, `transcriptBudget`,
  `frameRowPlan`) or the give-way order.
- Version bump (see closing note).

## 1. Seat the transcript-side overlay panes flush on the bottom chrome — ✅ DONE (2026-08-03)

NOTES (2026-08-03): design call answered — ALL THREE panes (approval/ask prompt, browser, picker)
moved below the gap row, so `View` keeps one append order with no per-pane branching. Two prose
sites beyond the item's list were touched, both stale for the same reason and neither arithmetic:
the `frameFixedRows` block comment (`model.go:2422`, "separates the transcript from the chrome" —
now "from whatever comes next") and the doc comment of `TestFrameRowBoundaryAgreesWithTheMouseMapping`
(`mouse_test.go`), whose assertion was the one existing test pinning the old order (repaired to
expect the blank gap row on the boundary and the pane one row below it). `mouse.go`'s overlay
comments (lines ~50 and ~181) describe the slot without stating an order, so they needed no change.
New tests live in `model_test.go` beside `TestTopRuleHairlineRow`.

**Design call (needs the user before implementing).**
Q: Should the approval/ask prompt move below the gap row together with the browser and
picker (recommended — one slot, one ordering rule, and the prompt shows the identical
unwanted spacer today), or should only the browser and picker move, leaving the prompt
above the gap as the issue's letter requires? The recommended answer keeps `View` to a
single append order with no per-pane branching.

**What:** In `internal/tui/model.go`, `Model.View`: move the blank gap row `""` from the
`rows = append(rows, "", m.topRule(), m.statusLine())` call (currently `model.go:2798`)
to BEFORE the slot's pane appends (currently lines 2783–2794), so the order becomes
`transcript → "" → [prompt] → [browser] → [picker] → m.topRule() → m.statusLine()`
(prompt placement per the design call). Update the stale prose to match the new order:
the `View` doc comment ("sits between the transcript and the blank line",
`model.go:2736–2745`), the comment above the gap-row append ("The single blank line
between chat content and the bottom chrome"), and the `frameOverlays` comment
(`model.go:2659–2661`) — plus `mouse.go`'s overlay comments (near lines 50 and 181) if
any of them state the old order (they describe the slot, so likely comment-only or no
change). No arithmetic, no `frameOverlays` field, and no mouse code changes: the frame
row count is identical in every composition.

**Tests:** in `internal/tui/model_test.go` (or `paint_test.go` if the verifier finds the
frame-paint tests live there — follow the existing `plain(m.View())` /
`transcriptRows(t, m)` idiom):

- `TestOverlayPaneSitsFlushOnBottomChrome` — for each of: the `/sessions` browser open,
  the picker open (`pickerServer`), and (per the design call) the approval prompt
  pending: compose `plain(m.View())`, locate the ▔ hairline row, and assert (a) the row
  directly above it is the pane's bottom border row (contains `╰`), i.e. NO blank row
  between pane and chrome, and (b) exactly one blank row sits directly below the
  transcript block (above the pane's `╭` title row).
- `TestFrameGapRowWithoutOverlay` — with no overlay open, the row directly above the
  ▔ hairline is blank (pins that the no-pane frame is unchanged).
- Both assert `lipgloss.Height` of the composed frame still equals `m.height` (D2: the
  reorder must not change the frame's total rows).
- Run the full package; repair any existing assertion that pinned the old
  pane-above-gap order (candidates: `sessions_test.go`, `picker_test.go`,
  `model_test.go`, `mouse_test.go`, `seam_test.go`) — ordering assertions only, never
  the row-budget arithmetic ones.

**Acceptance:**

- `go test ./internal/tui/ -run 'TestOverlayPaneSitsFlushOnBottomChrome|TestFrameGapRowWithoutOverlay' -v` — both pass.
- `go test ./internal/tui/` — full package green.
- `make check` — green.

**Commit:** `fix(tui): seat overlay panes flush on the bottom chrome, gap row above the slot`

## 2. Record the new slot order in layout.md, CHANGELOG and ISSUES.md — ✅ DONE (2026-08-03)

Depends on item 1.

**What:**

- `layout.md`, section "What 'height' means: one row budget, and the transcript pays
  it": add one or two sentences stating the settled order — a pane in the
  transcript-side slot sits flush on the ▔ hairline, and the frame's single blank gap
  row sits ABOVE the pane (between the session area and whatever comes next), so no
  pane ever shows an empty row against the bottom chrome. Sweep the section (and the
  eight-row frame-floor paragraph, which lists the no-pane composition and should need
  no change) for any sentence contradicting the new order.
- `CHANGELOG.md`: add a `### Fixed` entry under `## [Unreleased]` describing the fix in
  the file's house voice (the `/sessions` browser and the `/model` | `/server` picker —
  and the approval/ask prompt, if the design call included it — no longer paint a blank
  spacer row against the ▔ hairline; the gap row moved above the pane). Do NOT add a
  release heading and do NOT touch `VERSION`.
- `ISSUES.md`: flip the popup-inconsistency entry from `[P]` to `[X]`.

**Tests:** none (docs only).

**Acceptance:**

- `grep -n "flush" layout.md` shows the new slot-order sentence in the height section.
- `grep -n "spacer" CHANGELOG.md` shows the Unreleased Fixed entry; `git diff` (before
  commit) shows no `VERSION` change and no new release heading.
- `grep -n "\[X\] pop-up lists" ISSUES.md` shows the issue marked executed.
- `make check` — still green.

**Commit:** `docs: record the popup gap-row reorder in layout.md, CHANGELOG and ISSUES`

---

**Suggested version bump (not performed):** patch (`v0.10.13` → `v0.10.14`) once item 2
lands — a user-visible TUI fix in the same class as the fixes that took `v0.10.x` to
`.13`. The bump is the owner's call.
