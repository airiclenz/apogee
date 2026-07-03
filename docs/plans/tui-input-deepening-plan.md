# Plan — TUI input deepening: fix the prompt-box scroll, lift the promptEditor, transcript drag-select

**Date:** 2026-07-03
**Status:** READY (design grilled 2026-07-03; all D-items resolved, **no needs-design-call
escalation should be needed** — the one contingency is flagged in item 1). Runs **entirely inside
`internal/tui`** (+ docs), so it is safe to execute in parallel with the parser/prompt-seam
smoke-test follow-through (any fix commits from that land in `internal/processing` /
`internal/agent`). It does **not** pre-empt the `parser-seam-follow-through-plan.md` item-4 track
decision — the `/server` track stays open.
**Source:** `ISSUES.md` (the four open TUI entries) + architecture-review candidate #3 — "Lift
the chat input out of the god-Model" (`docs/architecture-review-20260629-110828.html`,
re-verified 2026-07-01: 25+ fields, 8 concerns, `model.go` ~1271 lines today).
**Track:** post-`v1.0.0` TUI quality — bug fixes + an in-process structural deepening. No domain
or engine types change; ADR 0010/0011 boundaries untouched.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs. `go test ./...`, `go test -race ./...`, `gofmt`, `go vet` green gate every item.

---

## The problem (grounded, verified 2026-07-03)

Four open `ISSUES.md` entries cluster on the chat input and transcript:

1. **`/clear` gauge (STALE — verified).** The fix landed in `6cc2c94` (2026-07-02 15:03 —
   "ClearContext now zeroes ctxUsed/tokPerSec like a fold does"; today `model.go:565-580`)
   **54 minutes before** the ISSUES entry was filed (`f2d6d8b`, 15:57) — observed on a stale
   binary. Post-`/clear` the gauge self-hides by design (`model.go:1174-1177` — usage is unknown
   until the next Turn reports it; the static window stays in the footer). **Grilled decision:
   close as stale, no code.**
2. **Prompt-box scroll after auto-grow (REAL — root cause verified in the bubbles source).**
   bubbles v2.1.0 `textarea.SetHeight()` calls `repositionView()`
   (`textarea.go:1190-1200` / `:1066-1074`), which only scrolls when the cursor is *outside* the
   view — it never clamps a stale downward `YOffset` when the box **grows**. Apogee's key order
   triggers it exactly: `handleKey` runs `m.input.Update(msg)` at the *old* height (the textarea
   scrolls down 1 chasing the cursor past the wrap), **then** `m.layout()` (`model.go:725-737`)
   calls `SetHeight(2)` — the offset stays 1, so a 2-row box renders rows 1–2 of a 2-row
   document: first line hidden above, cursor on the top visual row, phantom empty row below.
   The reported symptom, byte for byte.
3. **"Auto sizing prompt box is not working" (SUPERSEDED — same bug).** The auto-size machinery
   shipped 2026-06-24 (`add3790` — `inputRows()`/`inputContentRows()`, `model.go:748-751`,
   `render.go:324-330`) and *works* (the newer entry says "the box increases its size
   correctly"); what the user sees broken is the scroll artifact above. Closes with #2.
4. **Transcript drag-select (REAL — missing feature).** Prompt selection shipped (`mouse.go`,
   `promptSel`, rune-offset anchors, OSC52 copy via `tea.SetClipboard`); the transcript has
   nothing. The render pipeline is **one-way**: `entry.text` → markdown + baked ANSI
   (`markdown.go:114-140`) → `ansi.Wrap` (`render.go:275-280`) → flat `lines []string` cached on
   the Model (`model.go:112`) — no line→entry mapping survives. Mouse mode is always
   CellMotion (`model.go:851`), so terminal-native selection is unavailable in-app; an app-side
   selection is the only way to copy from the transcript.

Structurally, all of the input-side state lives as loose fields on the 28-field god-Model —
the five concerns the review calls one coherent concept (textarea, autocomplete + `skillRegion`,
`pendingSkills` chips, `*fileCache`, `promptSel` drag-select). `acceptAutocomplete`
(`autocomplete.go:295-317`) mutates `m.input` across the file boundary and is only testable
through the full `Update` loop.

## Design record (grilled 2026-07-03 — do not re-derive)

- **D1 — Scroll fix is apogee-side: re-seat the caret after a height change.** In `layout()`,
  after `SetHeight`, clamp the textarea's internal scroll so
  `ScrollYOffset() == clamp(offset, 0, max(0, contentRows-height))` with the cursor visible —
  implemented via the **`MoveToBegin()` + `CursorDown()`/`SetCursorColumn()` re-seat idiom that
  `mouse.go caretTo` (`:89-104`) already uses** ("unscrolls", then walks back to the caret).
  Cost is O(rows), bounded by `maxInputRows = 10`. At the max height the textarea's *legitimate*
  internal scrolling (offset > 0 with content > height) must keep working — the clamp formula
  covers it. (*Rejected:* patching/upgrading bubbles — not ours to schedule, the pin stays
  v2.1.0 and the fix must not depend on upstream behavior changing; `MoveToEnd()` — its
  `repositionView` has the same no-clamp hole and it moves the user's caret.)
- **D2 — Stale entries close with evidence, not code.** `/clear`-gauge: cite `6cc2c94` and its
  tests; the self-hiding gauge at zero is by design (grilled: accepted). "Auto sizing": mark
  superseded-by-#2; it closes when item 1 lands. No 0%-display change (grilled: rejected).
- **D3 — The lift target is the review's shape, inside the package.** New `promptEditor` struct
  (own file) owning exactly the five concerns: `input textarea.Model`, `autocomplete
  autocompleteState` + `skillRegion`, `files *fileCache`, `pendingSkills []string`, `sel
  promptSel`. Surface (package-internal): construct, `Update` (key/paste editing path),
  `Submit() → (text, fileRefs, skillIDs)` via the existing `command.go` parser, `Reset`,
  `Focus`/`Blur`, sizing (`Rows()`/width), `View(theme)`, and the three mouse handlers for its
  own rectangle. **The Model keeps** lifecycle state machine, transcript + render cache, stats/
  gauge, theme, layout — the review's explicit "genuinely fused to `View`/`layout`" boundary.
  The editor **never touches the engine** — it returns parsed ingredients; the Model calls
  `eng` (ADR 0011 C1 unchanged; ADR 0010 layout unchanged — everything stays in
  `internal/tui`). Ask-mode keeps borrowing the same editor (a Model state decides
  Submit-vs-answer, as today). Value-copy semantics preserved: the editor is a value field and
  follows the package's copy-and-return method idiom. Behaviour-preserving: **all existing
  Model-level tests stay green unmodified** (they are the refactor's safety net); new
  editor-direct unit tests are the payoff. (*Rejected:* a nested `tea.Model` sub-program — the
  review's noted framework tension; Bubble Tea favours the flat value-Model, and the deepening
  is the input cluster, not a framework fight. *Rejected:* lifting stats/transcript too — the
  review explicitly scopes them out.)
- **D4 — Transcript selection is screen-space, Model-side (grilled decision).** "Copy what you
  see": anchors live in **content coordinates** (rendered-line index + display cell), mapped
  from screen rows via `viewport.YOffset()`; extraction slices the cached rendered
  `m.lines` with `ansi.Cut` per line, `ansi.Strip`s, trims trailing pad, joins with `\n`, and
  copies via `tea.SetClipboard` (OSC52) with the existing `flash` confirmation — the exact
  machinery prompt selection proved (`shadeCells`, `mouse.go:275-281`). Highlighting overlays
  the visible slice like `highlightInput` does. Content-anchored coords survive wheel-scroll
  mid-drag; selection **clears on any transcript change** (stream token/commit — `m.lines`
  regenerates), on resize (like the prompt), and on a click elsewhere. Region arbitration in
  the mouse handlers: point-in-input-rect → editor; point-in-viewport → transcript; the two
  selections never coexist. Lives on the **Model** (a viewport concern), NOT in `promptEditor`.
  (*Rejected:* source-mapped line→entry reverse index — makes the one-way render pipeline
  bidirectional, every future renderer change must maintain the index; the copy artifacts
  (markers, rail gutters, soft-wrap newlines) are accepted terminal-native semantics. The
  gutter-stripping hybrid is additive later. Drag auto-scroll at the viewport edge: deferred,
  noted in the item.)
- **D5 — Docs posture: no ADR, no CONTEXT.md term.** The lift is easy to reverse (fails the ADR
  bar); "prompt editor" is implementation structure, not domain language — the glossary stays
  domain-only. Rationale lives in this Design record (house posture, matching the seam plans).
  Each item carries its own CHANGELOG line + ISSUES tick in the same commit; item 4 sweeps the
  residuals (stale-gauge close-out, architecture-review addendum).
- **Scope guard (grilled):** `internal/tui` + docs only. **Deliberate deferrals stay deferred**
  (recorded in the 2026-06-26 handoffs — do not silently reverse): mid-string (non-trailing)
  autocomplete completion; the Inspector; session-management UI. No bubbles upgrade. No
  prompt-side behavior changes beyond the scroll fix.

Reference pointers (read before implementing): `internal/tui/model.go` (`layout` :725,
`inputRows` :748, `handleKey` :351/:440, the `/clear` case :565, mouse cases :313-323,
`refreshViewport` :759, render cache :112), `internal/tui/mouse.go` (`caretTo` :89, `promptSel`
:37, `selectionText` :154, `shadeCells` :275), `internal/tui/autocomplete.go`
(`acceptAutocomplete` :295), `internal/tui/render.go` (`renderView` :46, `inputContentRows`
:324, `wrapText` :275), `internal/tui/filecache.go`, `internal/tui/command.go`, the textarea
source at `~/go/pkg/mod/charm.land/bubbles/v2@v2.1.0/textarea/textarea.go` (`SetHeight` :1190,
`repositionView` :1066), existing tests `mouse_test.go` / `render_test.go` / `model_test.go`
(idioms to extend), ADR 0010/0011, review candidate #3 in
`docs/architecture-review-20260629-110828.html`.

---

## 1. Fix the prompt-box scroll re-seat after auto-grow (+ close ISSUES #2 and #4) — ✅ DONE (2026-07-03)

**What:** per D1 — in `layout()` after `m.input.SetHeight(...)`, re-seat the caret through the
`MoveToBegin()`+walk idiom (extract a shared helper so `caretTo` and `layout` use one
implementation) so the textarea's internal scroll offset is clamped and the cursor lands on its
real line. A grown box must show the full content from row 0 with the cursor on the last
content line; at `maxInputRows` legitimate internal scrolling must survive.

**Tests (the flagged gap):** keystroke-by-keystroke growth (type past the wrap width; assert
per keystroke: `inputRows()` grows, `ScrollYOffset() == 0`, the rendered `View()` has no
trailing blank row, cursor on the last visual line); shrink back (delete to one line); content
past `maxInputRows` (offset > 0 is then correct — pin the clamp formula); multi-line paste;
unit rows for `inputContentRows` edge cases (empty value, trailing newline, exact-width line);
existing mouse `caretTo`/click tests stay green (they consume `ScrollYOffset`).

**Contingency (the one flagged risk):** if the re-seat idiom proves insufficient because of
textarea-internal state the public API can't reach, STOP and flag `needs-design-call` rather
than reaching for reflection or a fork.

**Docs (same commit):** CHANGELOG Unreleased fix entry; tick ISSUES #2 and #4 (superseded by
#2 — note the supersession inline).

**Acceptance:** gates green; `git diff --stat` touches only `internal/tui` + the two doc files.
Commit: `fix(tui): clamp the prompt textarea scroll after auto-grow (re-seat the caret)`.

---

## 2. Lift the chat input into a `promptEditor` module (review candidate #3) — ✅ DONE (2026-07-03)

> **✅ RESOLVED — design call made 2026-07-03: Option C chosen (anonymous-embed, partial method
> move). C1 intentionally relaxed; C2 fully met (all existing tests pass unmodified); C3 partially
> met. Implemented — see below. The original blocking analysis is kept for the record.**
>
> Item 2's three acceptance constraints cannot all hold against the current codebase. Any
> two are satisfiable; all three are not:
> - **(C1) Named value field** — the Model embeds `editor promptEditor` (access like
>   `m.editor.input`).
> - **(C2) All existing `internal/tui` tests pass UNMODIFIED.**
> - **(C3) The input-cluster fields AND their Model-receiver methods move onto the
>   `promptEditor` receiver** (all of `autocomplete.go`'s methods, `attachSkill`, the
>   input-rect mouse handlers, `highlightInput`, etc.).
>
> **Why they collide:**
> - Existing tests reference the cluster fields directly and heavily: `m.input` (~107 sites),
>   `m.sel` (~36), `m.autocomplete` (~35), `m.pendingSkills` (~23), plus promoted method calls
>   `m.computeAutocomplete()` (~20), `m.inputInnerWidth()` (~3), `m.highlightInput()`,
>   `m.inputContentRect()`. A **named** `editor` field breaks all ~200 of these sites →
>   **C1 contradicts C2.** Only **anonymous** embedding (`type Model struct { promptEditor; ... }`),
>   which promotes fields/methods so `m.input` and `m.computeAutocomplete()` still resolve, can
>   keep the tests unmodified.
> - But moving the methods onto the `promptEditor` receiver (C3) requires Model-owned state
>   those methods currently read: `opts` (`Skills`/`Workspace`/`ReloadSkills`), `th` (theme),
>   `width`, `height`. Since the promoted call sites can't gain parameters without editing
>   tests, the editor would have to carry **duplicated** copies of that Model state and keep
>   them in sync — genuine new logic, **not** the "mechanical, behaviour-preserving" move the
>   plan promises, and it re-couples exactly what the lift is meant to decouple. So
>   **C3 (with C2) breaks the "mechanical" promise.**
> - Conversely, keeping those methods on the Model (reading the promoted embedded fields) is
>   clean, mechanical, and keeps every test green — but then the methods are **not** moved onto
>   `promptEditor`, contradicting **C3 / design note D3.**
>
> **Resolution options (a design call is required before implementing — pick one):**
> - **Option A** — literal named `editor promptEditor` field: **RULED OUT**, rewrites ~200 test
>   sites, violates C2.
> - **Option B** — anonymous-embed `promptEditor` AND move all methods onto its receiver:
>   forces the editor to duplicate `opts`/`th`/`width`/`height` with sync logic; non-mechanical,
>   partial re-coupling.
> - **Option C (recommended by the blocked implementer)** — anonymous-embed `promptEditor`;
>   move only the genuinely self-contained methods (`Submit`/`Reset`/`Rows`/`Focus`/`Blur`) onto
>   the editor and add editor-direct tests; **keep** the Model-state-coupled methods
>   (`computeAutocomplete`, `highlightInput`, `inputContentRect`, `attachSkill`, the input-rect
>   mouse handlers) on the Model. Clean, mechanical, zero state duplication, all ~200 tests stay
>   green; only **partially** meets "all methods move to the editor."
>
> **The single open decision:** how `promptEditor` attaches to the Model and how far the method
> move goes — choose **B** or **C**, or relax one of C1–C3 in the acceptance criteria below.
> **Branch baseline when resuming:** item 1 (commit `a7afbf1`) and item 3 (commit `ffd1cd5`) are
> committed; no `promptEditor` type exists yet.

**What:** per D3 — new `internal/tui/prompteditor.go` defining `promptEditor` with the five
concerns moved off the Model (fields AND their methods: construction `model.go:124-134`, sizing,
the editing key/paste path, all of `autocomplete.go`'s Model-receiver methods, `attachSkill`,
chip state, `promptSel` + the input-rect mouse handlers + `highlightInput`). The Model embeds
`editor promptEditor` and delegates; `submit()` consumes `editor.Submit()`. **Mechanical,
behaviour-preserving move** — no logic changes ride along (the item-1 fix moves as-is).

**Tests:** all existing package tests pass **unmodified** (the safety net — if one needs
editing beyond a constructor/accessor rename, that's a smell to justify in the commit message).
New editor-direct unit tests, no fake engine, no full `Update` loop: `Submit()` parsing
(text/fileRefs/skillIDs), `acceptAutocomplete` splice + chip attach, chip pop on
backspace-empty, sizing rows, prompt-selection extraction.

**Docs (same commit):** `internal/tui/doc.go` gains the module map (Model = thin coordinator;
promptEditor = the input cluster); CHANGELOG refactor entry.

**Acceptance:** gates green; no diff outside `internal/tui` + docs; Model field count visibly
drops (record before/after in the commit message). Commit:
`refactor(tui): lift the chat input into a promptEditor module (review candidate #3)`.

---

## 3. Transcript drag-select-to-copy, screen-space (+ close ISSUES #3) — ✅ DONE (2026-07-03)

**What:** per D4 — `transcriptSel` (content-coordinate anchor/head) on the Model; mouse
arbitration (input rect → editor, viewport → transcript); motion extends; release extracts from
the cached rendered lines (`ansi.Cut` per line → `ansi.Strip` → trim trailing pad → join `\n`),
copies via `tea.SetClipboard`, flashes the confirmation; selection highlight overlays the
visible slice (reuse `shadeCells`); clears on transcript change/resize/click-elsewhere. Bare
click in the transcript selects nothing (matches the prompt's bare-click rule). Drag
auto-scroll at the viewport edge is explicitly deferred (note it in ISSUES as a polish item
only if the user asks).

**Tests:** extraction math over fake rendered lines (ANSI-styled, wide glyphs, trailing pad);
click→drag→release flow copies the expected plain text (extend the `mouse_test.go` idiom);
selection survives wheel-scroll mid-drag (content anchoring); clears on new stream token and on
resize; prompt and transcript selections never coexist; sticky-header row behaves as a plain
viewport row.

**Docs (same commit):** CHANGELOG feat entry; tick ISSUES #3.

**Acceptance:** gates green; no diff outside `internal/tui` + docs. Commit:
`feat(tui): drag-select-to-copy in the transcript (screen-space)`.

---

## 4. Docs close-outs — ✅ DONE (2026-07-03)

- Tick ISSUES #1 (`/clear` gauge) as **stale** — cite `6cc2c94` (fix predates the entry by
  54 min) and the grilled acceptance of the self-hiding gauge (D2). ISSUES #5 (parity umbrella)
  stays open — it tracks TODO.md.
- Append the candidate-#3 addendum to `docs/architecture-review-20260629-110828.html` (the
  2026-07-01 addendum's pattern): shipped, commit refs, field-count delta.
- Consistency sweep: doc.go/CHANGELOG entries from items 1–3 present and coherent.

**Acceptance:** docs consistent; `git status` clean. Commit:
`docs: close out the TUI input ISSUES and record the promptEditor lift`.

---

## Explicitly NOT in this plan

- **Source-mapped transcript selection / a line→entry reverse index** — D4's rejected
  alternative; the render pipeline stays one-way. The gutter-stripping copy hybrid is additive
  later.
- **Drag auto-scroll at the viewport edge** — deferred polish (item 3).
- **A 0%-display for the gauge after `/clear`** — grilled and rejected; self-hiding is by design.
- **Mid-string autocomplete completion** — deliberately deferred in the 2026-06-26 handoff;
  stays deferred.
- **Session-management UI, Inspector, `/server`** — separate TODO tracks (the `/server`
  follow-through item-4 decision stays open).
- **A bubbles upgrade or fork** — the pin stays v2.1.0; the fix is apogee-side (D1).
- **An ADR or CONTEXT.md term for the promptEditor** — D5; the design record above is the
  authoritative rationale.
