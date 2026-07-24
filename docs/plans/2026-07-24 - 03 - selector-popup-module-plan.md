# Plan — Selector popups: one shared painter, full chat-area width

**Date:** 2026-07-24
**Status:** READY (design grilled 2026-07-24; both forks resolved by the owner — no
needs-design-call escalation expected). Runs **entirely inside `internal/tui`** (+ CHANGELOG).
**Source:** owner request 2026-07-24 — (a) the /sessions popup box must use the complete
available width of the chat area (minus the right gutter); (b) the same base module that paints
this kind of popup should serve skill-selection, file-selection, and so on.
**Track:** post-`v1.0.0` TUI quality — a visual fix plus an in-package structural deepening. No
domain or engine types change; ADR 0010/0011 boundaries untouched.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`, no
PRs. `go test ./...`, `go test -race ./...`, `gofmt`, `go vet` green gate every item.

---

## The problem (grounded, verified 2026-07-24)

Two selector overlays exist and each paints itself with its own private code:

1. **The /sessions history browser** (`internal/tui/sessions.go:355` `renderSessionBrowser`) —
   a bordered box (`th.startupBorder` chrome) whose width is
   `max(20, min(m.width-4, 72))`. On any terminal wider than ~76 columns the box stops at 72
   and does **not** span the chat area. Two latent defects hide in that line:
   - **Width-semantics mismatch.** lipgloss v2's `Style.Width(n)` sets the **total** rendered
     width — border and padding fold *into* n (charm.land/lipgloss/v2@v2.0.4 `set.go:283`
     "your styled content will exactly equal the size set here"; `renderStartupBox`
     (`render.go:244-252`) already relies on exactly this). But `renderSessionBrowser`
     truncates its rows to the *total* width (`truncateLabel(…, inner)` with the box rendered
     `Width(inner)`), while the real content area is `inner − 4` (border + padding). A row in
     the 4-column overlap wraps inside the box and breaks the pane's layout.
   - **The floor-20 overflow.** On a window narrower than ~24 columns the `max(20, …)` floor
     exceeds the window and the box wraps the whole View.
2. **Skill-, file-, and command-selection are already one component** — the autocomplete
   overlay (`internal/tui/autocomplete.go` `renderAutocomplete`, kinds `acCommand` / `acFile` /
   `acSkill`) — but it paints **borderless** faint rows truncated to the raw `m.width`, a
   different look from the session box, with its own per-row marker/highlight logic.

The chat-area content width the owner means is the budget the startup card already spans:
`transcriptWidth()` (`model.go:1088`) = window − the reserved scroll-bar column
(`scrollbarWidth`) − the right gutter (`bodyRightGutter`) = `m.width − 2`. The startup card's
right border lands on the exact column the transcript's wrapped text ends at; the popups should
land on the same one.

Test exposure is low: no test pins the borderless dropdown look or the 72-column box — the
view-level tests assert `strings.Contains` on ANSI-stripped output (`plain`, `model_test.go:77`)
and `sessions_test.go:100` greps for row substrings. The harness window is 100×30
(`sessions_test.go:25`), so "full width" is 98 columns there.

## Decisions (grilled 2026-07-24)

- **D1 — full unification.** The /command, @file, and skill dropdowns adopt the same bordered
  popup look as the session browser via the new shared painter: title row, rows with the ❯
  selected-row highlight, key-hint footer, full chat-area width. (Owner picked this over
  "border only" and "share internals only".)
- **D2 — approval/ask prompts stay out.** `approvalPrompt` / `askPrompt` keep their plain-text
  form this plan; the module's doc comment names them as deliberate future adopters.
- **D3 — width.** Every popup's total width is `transcriptWidth()` — the startup card's railed
  budget — so all boxed chrome shares one right edge. No artificial floor: a degenerate-narrow
  window truncates rows rather than overflowing the View (the rest of the chrome already
  degrades this way, e.g. `footerView` below 3 columns).
- **D4 — positions unchanged.** The session browser keeps its slot above the blank gap row
  (the approval-prompt slot); the dropdowns keep theirs between the status line and the input
  box. `View`'s viewport-shrink math is already `lipgloss.Height`-generic, so the taller
  dropdown simply costs the viewport three more rows while open.

---

## 1. The shared popup painter — `internal/tui/popup.go` — ✅ DONE (2026-07-24)

NOTES (2026-07-24): renderPopup early-returns `""` when `width ≤ th.popupBorder`'s
horizontal frame size. lipgloss cannot render a bordered box narrower than frame+1 (the
`inner = max(1, …)` floor keeps content ≥ 1 cell, so the box floors at frame+1 = 5), so a
sub-frame width would overflow the View. The guard makes the degenerate-width test bullet
("neither panics nor exceeds `width`") satisfiable — same degrade as `footerView` below 3
columns (plan D3). `maxRows ≤ 0` ("shows all") is handled caller-side in renderPopup
(capRows := len(rows)); `popupRowWindow` stays the verbatim moved windowing. The
`sessionRowWindow` → `popupRowWindow` move (per this item) repointed its one caller in
`renderSessionBrowser` and updated a stale doc reference in the `maxSessionRows` comment;
`renderSessionBrowser`'s full rebuild is item 2. On the inward-only acceptance: renderPopup
calls no overlay logic — the sole cross-file symbol is `truncateLabel` (a shared leaf helper
that physically lives in autocomplete.go), which the item text directs using; it was left in
place rather than moved, to avoid an out-of-scope autocomplete.go edit.

New file `popup.go` (+ `popup_test.go`), the single place selector-popup chrome is painted:

- `popupSpec` — one boxed selector popup's description: `title string` (empty drops the row),
  `rows []string` (pre-composed plain labels, already escape-stripped by callers),
  `selected int` (index into rows; −1 = no selection highlight), `hint string` (empty drops the
  row), `maxRows int` (scroll-window cap; ≤ 0 shows all).
- `renderPopup(th theme, spec popupSpec, width int) string` — paints the bordered pane.
  `width` is the **total** box width (lipgloss v2 semantics — see problem §1); the inner
  content budget is `max(1, width − th.popupBorder.GetHorizontalFrameSize())`, following the
  style like `renderStartupBox` does rather than a hard-coded 4. Every content line is the
  marker column + label, `truncateLabel`-ed to the inner budget — **no line can ever wrap the
  box** (this is what fixes the session browser's width-semantics defect). The module owns the
  marker: `glyphUser + " "` on the selected row, two spaces otherwise (both current renderers
  do this identically today). Selected row renders `th.userBlock.Width(inner)` (the full-bar
  highlight the session browser already has); other rows `th.statusFaint`; title
  `th.presentTitle`; hint `th.statusFaint`. Box: `th.popupBorder.Width(width)`.
- `popupRowWindow(selected, total, capRows int) (int, int)` — `sessionRowWindow`
  (`sessions.go:468`) moved verbatim and renamed; the windowing now lives in the module, so
  callers pass the FULL row list plus the global selected index and `renderPopup` windows
  around the selection itself.
- `theme.popupBorder` (theme.go) — a new style with `startupBorder`'s exact definition
  (RoundedBorder, `colDarkGray` foreground, transparent, `Padding(0, 1)`), under its own name
  so popup chrome and the startup card can diverge later; both doc comments cross-reference
  the other.
- The file-head doc comment records the module's contract (total-width semantics, module-owned
  marker/highlight/windowing) and names `approvalPrompt`/`askPrompt` as deliberate future
  adopters (D2).

**Tests** (`popup_test.go`, on `ansi.Strip`-ed lines except where noted):

- every rendered line is exactly `width` display cells (`lipgloss.Width`), for a spec with and
  without title/hint;
- a row longer than the inner budget does **not** wrap: the line count is exactly
  2 (borders) + title + shown rows + hint, and the long row ends in "…";
- the selected row carries the ❯ marker and its un-stripped line carries `userBlock`'s SGR
  (loose contains, not a byte golden — a lipgloss renderer change must not false-fail);
- `selected = −1` renders no marker and no highlight (all rows faint);
- empty title and empty hint each drop their row;
- `popupRowWindow` table test: total ≤ cap → `[0, total)`; a mid-list selection is roughly
  centred; the window clamps at both ends (the cases the old inline windowing satisfied);
- a degenerate width (≤ the frame size) neither panics nor exceeds `width`.

**Acceptance:** gates green; `popup.go` references nothing from `sessions.go` /
`autocomplete.go` (the dependency points inward only — callers import the painter, never the
reverse). Commit: `feat(tui): shared selector-popup painter`.

---

## 2. /sessions browser onto the painter, full chat-area width

All in `internal/tui/sessions.go` (+ its tests):

- `renderSessionBrowser` rebuilds onto `renderPopup`: width `m.transcriptWidth()` (D3), spec =
  title `"saved sessions  (<scope>)"` (unchanged text), hint `sessionBrowserHint`, `maxRows
  maxSessionRows`, selected `b.selected`. The `max(20, min(m.width-4, 72))` line is deleted —
  the 72 ceiling, the width-semantics mismatch, and the floor-20 overflow all go with it.
- Row composition stays caller-side: a new `sessionRows(b sessionBrowser, workspace string,
  now time.Time) []string` returns the FULL filtered label list (via the unchanged
  `sessionRowLabel`), with the selected row's rename (`"rename: " + buf + "▏"`) or
  delete-confirm (`label + "   delete? y/n"`) decoration applied — exactly the three cases
  today's `sessionRow` switches on. `sessionRow` itself is deleted (marker, highlight,
  truncation, and windowing are now module-owned); `sessionRowLabel`, `msgsLabel`,
  `workspaceBase`, `relativeTime` are untouched.
- The empty-view note (`"no sessions in this workspace — press a to see all"`) becomes the
  single row of a spec with `selected: -1`.
- The §Rendering comment block updates: the pane is painted by the popup module, not "like the
  approval/ask prompts".

**Tests** (`sessions_test.go`):

- at the 100×30 harness window, every line of the rendered browser is exactly **98** columns
  (`m.width − scrollbarWidth − bodyRightGutter`) — the startup card's right edge;
- a session whose title is wider than the box adds no extra line (count the pane's physical
  lines: 2 borders + title + rows + hint) and its row ends in "…";
- with more than `maxSessionRows` sessions the pane still shows a window around the selection
  (line count capped at 2 + 1 + maxSessionRows + 1);
- the existing contains-assertions (title, scope, rows, hint, rename/confirm flows) pass
  unmodified.

**Acceptance:** gates green; `grep -n '72\|min(m.width' internal/tui/sessions.go` returns
nothing; `sessionRowWindow` no longer exists in `sessions.go` (moved to the module in item 1).
Commit: `feat(tui): /sessions popup spans the chat-area width`.
**Depends on:** item 1.

---

## 3. Command/file/skill dropdowns adopt the popup chrome

All in `internal/tui/autocomplete.go` (+ view-level tests). This is D1 — full unification:

- `renderAutocomplete` rebuilds onto `renderPopup`: rows are the `acItem` labels verbatim (the
  per-row marker and style logic is deleted — module-owned now), selected `ac.selected`, width
  `m.transcriptWidth()` (was the raw `m.width`), `maxRows maxAutocompleteItems` (a no-op today
  — every producer already caps at 8 — but a producer that grows gets windowing for free).
- Title by kind: `acCommand` → `"commands"`, `acFile` → `"files"`, `acSkill` → `"skills"`.
- New const `autocompleteHint = "↑/↓ select · ⏎/tab accept · esc dismiss"` as the hint row
  (coarse like `sessionBrowserHint` — the exact-match Enter-submits nuance stays undocumented
  in the legend, as the session hint also elides its modes).
- Position unchanged (D4): the dropdown keeps its slot between the status line and the input
  box in `View` (`model.go:1207`); no `View` changes are needed — the shrink math already
  measures with `lipgloss.Height`.
- The file-head comment's "It mirrors the approval-prompt overlay" description updates to name
  the popup module.

**Tests** (`skill_test.go` / `minilang_test.go` style, `plain(m.View())`):

- with the skill picker open, the View contains the `"skills"` title row and the box's ╭/╰
  border glyphs above the input box; the selected row leads with ❯;
- a `/` command menu shows the `"commands"` title, an `@` token the `"files"` title;
- every dropdown line is exactly 98 columns at the harness window;
- the existing dropdown tests pass unmodified (verified 2026-07-24: they assert
  `m.autocomplete` state or `strings.Contains` on the View — nothing pins the borderless look).

**Acceptance:** gates green; no per-row marker/style logic remains in `renderAutocomplete`
(one `renderPopup` call composes the pane). Commit:
`feat(tui): command/file/skill dropdowns adopt the popup chrome`.
**Depends on:** item 1.

---

## 4. Closeout — CHANGELOG + stale-look sweep

- CHANGELOG: one entry per visible change — the /sessions popup spans the chat area; the
  command/file/skill dropdowns share the same boxed popup chrome (title + ❯ rows + hint).
- Sweep the package for stale look-descriptions of either overlay: `sessions.go` §Rendering
  and `autocomplete.go`'s head comment are rewritten in items 2–3 — verify nothing else
  (e.g. `doc.go`, `model.go`'s View comments) still describes the dropdown as borderless or
  the session pane as 72-capped. `layout.md` needs nothing (verified 2026-07-24: it does not
  describe either overlay).
- Full gates one last time: `go test ./...`, `go test -race ./...`, `gofmt -l .` empty,
  `go vet ./...`.

**Acceptance:** gates green;
`grep -rn 'mirrors the approval-prompt overlay\|min(m.width-4, 72)' internal/tui
--include='*.go'` returns nothing. Commit:
`docs(tui,changelog): record the selector-popup unification`.
**Depends on:** items 2, 3.
