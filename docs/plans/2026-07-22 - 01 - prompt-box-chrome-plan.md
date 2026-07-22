# Plan — prompt-box chrome: top-edge row + uniform thin rules

**Date:** 2026-07-22. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session, forwarding skills: `coding-standards`. Two small owner-requested visual changes to the
TUI's bottom chrome:

1. **Extend the black background one row upward.** The black region currently starts at the input
   box's `╭───╮` top border. Add one more full-width black row directly above it, carrying a
   dark-gray hairline at the very top of that row.
2. **Uniform thin frame lines (reported as a bug).** The box's top border is thin `─`
   (lipgloss `RoundedBorder`), but the divider below the input and the footer's bottom rule are
   drawn by `ruleMix()`, which mixes heavy `━` with light `─` (a layout.md flourish). The owner
   wants the thinner line everywhere.

Target look:

```
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔   <- NEW row: dark-gray hairline at top edge, black below
╭──────────────────────────╮
│ Send a message…          │
├──────────────────────────┤   <- was ├━━━──━━┤
│ host ✦ model ✦ 32k   ask │
╰──────────────────────────╯   <- was ╰━━━──━━╯
```

## Where things stand (grounded, verified 2026-07-22)

- The input box renders in `inputView()` (`internal/tui/model.go:970`) via the `inputBorder`
  style (`internal/tui/theme.go:158-164`): `RoundedBorder()` with `BorderBottom(false)` — its
  apparent bottom edge is really the footer's top divider.
- `footerView()` (`internal/tui/model.go:979-992`) composes both footer rules from
  `ruleMix(w-2)` between hand-placed corners (`├`/`┤`, `╰`/`╯`). `ruleMix` lives at
  `internal/tui/render.go:513-524` and has no other callers and no test references (verified by
  grep: `ruleMix|━` matches only render.go, theme.go:139's comment, and model.go:985).
- Height budget: `layout()` (`internal/tui/model.go:758`) uses
  `inputBoxHeight := m.input.Height() + 1` (content rows + top border). The viewport gets the
  remainder, floored at 1.
- Mouse hit-testing is bottom-anchored to the border row:
  `boxTop := m.height - footerHeight - (h + 1)` (`internal/tui/mouse.go:82`). A row added
  *above* the border does not move it, so `mouse.go` and the `24 - footerHeight - 1` constants
  in `mouse_test.go` stay valid untouched.
- The dark-gray-on-black rule style is `footerRule` (`internal/tui/theme.go:124,168`), used at
  `model.go:985` (rules) and `model.go:1007` (footer `│` bars). Palette: `colDarkGray = #4a4a4a`,
  `colBlack = #000000` (`theme.go:26-27`).
- The bottom-chrome design sketch lives at `layout.md:44-52` and shows the mixed `━`/`─` rules —
  it must be updated to keep matching the code.
- `View()` stacks `…, m.inputView(), m.footerView()` at `model.go:887`; the autocomplete
  dropdown and skill chips render just above the input box.

### Decisions locked with the owner (2026-07-22 — do not re-litigate)

- **The hairline character is `▔` (U+2594 UPPER ONE EIGHTH BLOCK)** — chosen from a
  side-by-side preview against centered `─`. The line sits at the top edge of the cell, black
  fills the rest beneath, so it marks the exact top of the extended black region.
- **The existing `╭───╮` rounded top border stays.** The new row sits above it.
- **Both footer rules go thin** (the divider *and* the bottom rule), not just the divider —
  a lone heavy rule would recreate the mismatch.
- **The new row lives inside `inputView()`** (prepended via `lipgloss.JoinVertical`), so
  `View()`'s stacking is untouched and the dropdown/chips still float directly above the black
  region.
- **`footerRule` is renamed `chromeRule`** — it now styles the input's top-edge row as well as
  the footer's rules and bars, so the old name would lie.

## 1. Uniform thin rules in the bottom-chrome frame

**What:** in `footerView()` (`internal/tui/model.go:984-986`), replace `ruleMix(w-2)` with
`strings.Repeat("─", w-2)`. Delete the now-unused `ruleMix` (`internal/tui/render.go:513-524`).
Update `footerView`'s doc comment (drop the "decorative" mixed-rule wording) and the `newTheme`
comment (`internal/tui/theme.go:136-139`) — the hand-composition rationale (the `├`/`┤`
junction corners a lipgloss border cannot produce) survives; the mixed-weight wording goes.
Update the `layout.md` sketch (~lines 44-52): both rules become uniform `─`.

**Tests:** existing suite stays green untouched. Add one assertion in `internal/tui`'s tests:
`footerView()`'s output contains no `━` (strip ANSI first, as `model_test.go:849` does).

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green;
`grep -rn "ruleMix\|━" --include="*.go" internal/ cmd/` returns nothing. Commit:
`fix(tui): uniform thin rules for the input-box and footer frame`.

---

## 2. Top-edge hairline row above the input box

**What:** (depends on item 1.) Rename `footerRule` → `chromeRule` (struct field + comment
`internal/tui/theme.go:124`, `newTheme` `theme.go:168`, call sites `model.go:985` and
`model.go:1007`). In `inputView()` (`model.go:970-972`), prepend a full-width top-edge row:
`lipgloss.JoinVertical(lipgloss.Left, m.th.chromeRule.Render(strings.Repeat("▔", m.width)), <existing bordered box>)`,
and update its doc comment to name the row. In `layout()` (`model.go:758`), change
`inputBoxHeight := m.input.Height() + 1` to `+ 2` and update the comment (content rows +
top-edge row + top border); the existing `vpHeight < 1` floor covers tiny windows. Do NOT touch
`mouse.go` — its geometry is bottom-anchored to the border row (see Where things stand). Update
the `layout.md` sketch: add the `▔` row above `╭───╮`.

**Tests:** existing suite stays green with no edits to `mouse_test.go` (its constants are
bottom-anchored). Add assertions in `internal/tui`'s tests: `inputView()`'s first line is
exactly `m.width` `▔` runes (ANSI-stripped), and after `layout()` on a fixed window the
viewport height is one row smaller than before this change
(`m.height - statusHeight - gapHeight - (inputRows+2) - footerHeight`).

**Acceptance:** `go build ./... && go vet ./... && go test ./...` green. Commit:
`feat(tui): top-edge hairline row above the input box`.

## Explicitly NOT in this plan

- No change to the `╭───╮` rounded border, the input box's growth behaviour
  (`minInputRows`/`maxInputRows`), the footer content line, or the status line.
- No change to `mouse.go` hit-testing (bottom-anchored, unaffected — verified above).
- No theming beyond the `chromeRule` rename: colors stay `colDarkGray` on `colBlack`.

## Critical files

- `internal/tui/model.go` — `footerView`, `inputView`, `layout()`
- `internal/tui/render.go` — delete `ruleMix`
- `internal/tui/theme.go` — `footerRule` → `chromeRule`, `newTheme` comment
- `layout.md` — bottom-chrome sketch

## Owner-run checklist (after implementation)

- [ ] Run `apogee` against the live endpoint and eyeball the bottom chrome: the `▔` hairline row
  sits directly above the box on a black row spanning the full width; every frame line is thin
  `─`; the box still grows to `maxInputRows` with the viewport shrinking correctly; the
  autocomplete dropdown and skill chips render above the new row; click-to-position-caret in the
  input box still lands on the right cell.
