# Painted fields never erased — implementation plan

**Goal:** every painted field (bottom chrome, sent prompt blocks, breadcrumb, popups, staged band) shows its own colour to the right border on Terminal.app too. The renderer never clears a painted run with an erase sequence the terminal may draw in its own background; every painted cell at a row's edge is written as a character.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** c4a3439f

**Regression check (2026-11-05, c4a3439f):**
- 1: guard folded (sealed cell reset-bracketed; background-SGR prefilter and `sync.Pool` buffer; `StyledString.Draw` route; renderer test route; per-frame field contribution)
- 2: guard folded (prose grep scoped to live docs; earlier dated amendments not rewritten)

**Sources:**
- `.capcom/issues/bottom-chrome-erased-cells.md` (root cause), `.capcom/issues/prompt-box-uneven-background.md` (closed predecessor)
- `layout.md` (colour scheme paragraph: "The bottom chrome is one band"), `capcom know show "ADR 0040" --with-children` (`adr-0040-amend-1105b`, `adr-0040-amend-1105c`)
- ultraviolet `terminal_renderer.go` at the `go.mod` pin: `canClearWith`, `transformLine`, `clearToEnd`, `emitRange`, `el0Cost`
- `docs/design/test-drivers.md` (frame judging)

**Ratified design calls** (owner, 2026-11-05):
- **Scope:** every painted field, wherever a row ends (or starts) in painted blanks: bottom chrome, sent prompt blocks, breadcrumb, popups, staged band, and any future field.
- **Mechanism (writer, no visible difference):** a painted blank cell at a row's edge carries the conceal attribute (SGR 8), which ultraviolet's `canClearWith` refuses, so the renderer writes the row out instead of sending `EL`. Content stays a space, so copied text is unchanged.
- **Placement (writer):** one frame-level pass in `joinFrame`, which every row already passes through; no per-site fill helper.
- **Evidence for edges only:** mid-row runs sent as `ECH` (footer row 1's gap, the input box interior) render correctly in Terminal.app screenshots; only a row's trailing and leading runs (`EL` 0/1/2) are sealed.

**Closes:** none at plan level (each item names its own).

**Standing requirements:**
- skills: coding-standards
- Tests run one package and named tests only (`go test -race -count=1 -run '<names>' ./internal/tui/`); never a multi-package `go test`.
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Unpainted rows (transcript, the gap row above `▔`): their erase uses the default pen and stays.
- Mid-row blank runs (`ECH`).
- Patching or forking ultraviolet, or overriding its terminal capabilities.

## 1. Seal painted row edges so the renderer writes them instead of erasing

**What:** fix for `bottom-chrome-erased-cells` (present since fields were first painted to the full width; `prompt-box-uneven-background` closed without it). The cell buffer is already right; the bytes the renderer emits for it are not.
**Regression guard.** Emit a sealed cell as `ansi.ResetStyle + st.String() + " " + ansi.ResetStyle` (uv `Style.String()` emits no reset, so an unbracketed seal leaks conceal or an open underline into the row). Parse only lines whose bytes carry a background SGR (`48;`, `4x`/`10x`), and take the one-row buffer from a `sync.Pool` — never a Model field or package var (doc.go: "the theme's styles are shared and immutable"; View runs on parallel copies). Parse route: `uv.NewStyledString(ln).Draw` into a w×1 `uv.ScreenBuffer` with `Method = m.th.measure.Method()`, then read `CellAt(0,0)` and `CellAt(w-1,0)` — never `StyledString.Lines`, which yields no cells at this pin (styled.go:207, zero Rectangle).
**Goal:** in every `View` frame, no row's last cell and no row's first cell is a plain space with a background set and without the conceal attribute; and rendering the frame's painted-to-the-edge rows through ultraviolet's `TerminalRenderer` under `TERM=xterm-256color` emits no erase-in-line (`CSI K`, `CSI 0 K`, `CSI 1 K`, `CSI 2 K`) and no erase-in-display (`CSI J` family) beyond the renderer's initial screen clear, on a first paint and on a repaint. `TestBottomChromePaintsOneBackground` and `TestViewKeepsTheTerminalBackground` still pass.
**Approach (assumed at the header base):**
- One function in `internal/tui/boxdraw.go`, called by `joinFrame` on each squared line: when the line's last cell (and, separately, its first cell) is a width-1 space with `Bg` set and no conceal, re-emit that one cell with the same style plus conceal; otherwise return the line unchanged and unallocated. Its doc comment states the why (ultraviolet's `canClearWith` / `clearToEnd` / EL 1 path; Terminal.app draws erased cells in its own background).
- Decide with ultraviolet's own cell parse (`uv.NewStyledString` drawn into a reused one-row buffer, the parse the renderer itself runs), never a hand-rolled SGR tracker; rebuild the cell from its `uv.Style` sequence. Widths go through `m.th.measure` (`squareLine`'s rule).
- One deep module: `joinFrame` is the only caller; no field renderer changes. Unpainted rows (`Bg` nil at the edge) pass through untouched.
**Files:** internal/tui/boxdraw.go, internal/tui/background_test.go
**Read first:** internal/tui/boxdraw.go — joinFrame, squareLine, squareOnField; internal/tui/background_test.go — TestBottomChromePaintsOneBackground, schemeModel, sameColor; internal/tui/mode_test.go — withGitSnapshot, footerGitTestSnapshot; internal/tui/model_test.go — footerFactsModelIn, newTestModel, step
ultraviolet terminal_renderer.go — canClearWith, transformLine, clearToEnd; ultraviolet styled.go — StyledString.Draw, printString; bubbletea cursed_renderer.go — flush; internal/tui/doc.go — Model copy and shared-theme invariants
**Tests:** in `internal/tui/background_test.go`:
- `TestPaintedRowEdgesAreSealed`: draw `m.View().Content` into a `uv.ScreenBuffer`; for every row assert the first and last cell is not (space ∧ `Bg` set ∧ no `uv.AttrConceal`); no non-edge cell carries `uv.AttrConceal`, and each non-edge cell's Content and Style equal the unsealed parse. Frames: idle status line (`ctxUsed = 0`); a two-row footer with a git snapshot (`footerFactsModelIn`, `withGitSnapshot`, `footerGitTestSnapshot` in `mode_test.go`); a sent prompt block in the transcript; an open popup. Per frame, first assert the named field contributes ≥1 painted-edge row (the prompt row ends in the scrollbar column, popups in border glyphs), or drop that frame from the coverage claim.
- `TestPaintedRowsAreNeverErased`: draw the rows whose last cell has `Bg` set into a `uv.NewScreenBuffer` and pass its `.RenderBuffer` to `Render` (as bubbletea's cursed_renderer.go does); create the renderer via `uv.NewTerminalRenderer(&out, []string{"TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal"})`, call `SetFullscreen(true)` and `Erase()` before the first `Render`, then `Flush`; assert no EL/ED except that one initial `\x1b[2J`; change the chrome (gauge → idle; a shorter git snapshot), render again, assert no EL/ED at all.
- Both must fail at the header base.
**Acceptance:**
- `go build ./internal/tui/`
- `go test -race -count=1 -run 'TestPaintedRowEdgesAreSealed|TestPaintedRowsAreNeverErased|TestBottomChromePaintsOneBackground|TestViewKeepsTheTerminalBackground' ./internal/tui/`
- `go test -race -count=1 ./internal/tui/`
**Closes:** bottom-chrome-erased-cells
**Commit:** `fix(tui): write painted row edges instead of leaving them to an erase`

## 2. Record the sealed-edge rule in layout.md and ADR 0040

**What:** depends on item 1.
**Regression guard.** Scope the prose-rule grep to live docs: `grep -rn 'TestBottomChromePaintsOneBackground' layout.md docs/layout docs/manual docs/design .capcom/knowledge/glossary`, excluding `archived/`. Earlier dated amendments (`adr-0040-amend-1105b`, `adr-0040-amend-1105c`) and archived plans are point-in-time records and are not rewritten: the new tests are cited only in `adr-0040-amend-1105d`.
**Goal:** `layout.md`'s colour scheme paragraph states that a painted field's cells at a row's edge are written as characters, never left to a terminal erase, and ADR 0040 carries a dated amendment `adr-0040-amend-1105d` saying so, pinned by `TestPaintedRowEdgesAreSealed` and `TestPaintedRowsAreNeverErased`, named on ADR 0040's Status line and in its reading-order list.
**Approach (assumed at the header base):** one sentence after "The bottom chrome is one band: …" in `layout.md`. The amendment is a child Note (`capcom know add --id adr-0040-amend-1105d --parent adr-0040`) in the shape of `adr-0040-amend-1105c`: the cause (ultraviolet `EL` on a clearable edge run; Terminal.app), the rule (painted edge blanks carry conceal, sealed once in `joinFrame`), the scope (every painted field, owner 2026-11-05), the pins. Prose rule: every doc sentence that pins the full-width field with `TestBottomChromePaintsOneBackground` alone gains the new tests; find them with the live-docs grep in the guard above.
**Files:** layout.md, .capcom/knowledge/decisions/adr-0040.md, .capcom/knowledge/decisions/adr-0040/adr-0040-amend-1105d.md
**Read first:** layout.md — colour scheme paragraph ("The bottom chrome is one band"); .capcom/knowledge/decisions/adr-0040.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0040/adr-0040-amend-1105c.md — amendment shape, pin sentence; .capcom/issues/bottom-chrome-erased-cells.md — Acceptance, Scope
**Tests:** none (docs only); `capcom check` covers Note structure.
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0040" | grep -q 'adr-0040-amend-1105d'`
- `grep -q 'TestPaintedRowsAreNeverErased' .capcom/knowledge/decisions/adr-0040/adr-0040-amend-1105d.md`
**Commit:** `docs(layout): record that painted row edges are never erased`
