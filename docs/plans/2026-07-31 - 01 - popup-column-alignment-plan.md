# Pop-up module column alignment — implementation plan

- **Goal:** rows in the pop-up module (slash menu, `/skill` picker, model/server/load
  pickers, session browser) render their fields as vertically aligned columns instead of
  ad-hoc concatenated strings, closing ISSUES.md's "items in the pop-up module are
  currently terribly alligned" entry.
- **Date:** 2026-07-31 · **Status:** not started
- **Authoritative sources:**
  - Ticket: `ISSUES.md:7`.
  - Layout ground truth: `layout.md` — `## The prompt box's mini-language` (dropdown
    rows) and the "One overlay for 'which one?'" passage (picker row grammar). Where the
    **Column contract** below deliberately amends that grammar, this plan governs; item 5
    lands the amendment back into `layout.md`.
  - Renderer boundary being renegotiated: the contract comment at the top of
    `internal/tui/popup.go` (rows "arrive pre-composed" — that sentence is what this plan
    retires).
- **Standing requirements:** run `make check` before every commit; never touch `VERSION`
  or any CHANGELOG release heading (version bumps are the owner's call — see closing
  note); no AI-attribution trailers in commits; the Bubble Tea `Model` is copied by
  value — never let a `strings.Builder` or other no-copy type reach it (ADR 0011,
  `internal/tui/doc.go`); any authorized deviation from an item's text must land as a
  dated NOTES line under that item.
- **Out of scope:** markdown-table rendering in the chat (separate plan,
  `2026-07-31 - 00`); the collapsible tool-call module (ISSUES.md:13); per-column width
  caps or ratio budgets when the terminal is narrow (whole-row truncation stays);
  changing the 8-row window caps, popup title/body/hint rendering, or scroll behavior;
  multi-column content for the ask/approval prompts (they stay single-cell).

## Column contract (ratified design — items below implement it)

1. A popup row is a slice of **cells** (`type popupRow []string`); the popup module —
   not the row producers — owns column alignment, alongside its existing
   marker/highlight/truncation/windowing duties.
2. Column width = the **maximum display width** (`lipgloss.Width` / ANSI-aware, not
   `len([]rune)`) of that column's cells across **all rows of the spec**, not just the
   visible window, so alignment never shifts while scrolling.
3. Cells in the same row are joined with a minimum gutter of **two spaces**. Grammar
   separators from `layout.md` (`— `, `· `, `(:port)`) survive as leading content of
   their own cell (e.g. `["alpha", "— llamacpp", "· 32k"]`), so the separator glyphs
   themselves also line up.
4. Each popup kind uses a **fixed column schema**; an absent optional tier is an empty
   cell (padded like any other), so later columns stay aligned. A column empty in every
   row collapses (contributes no width and no gutter).
5. The composed row is right-trimmed, then goes through the existing pipeline: 2-cell
   selection marker prepended, display-width truncation to the inner width with a
   trailing `…`. Truncation is whole-row; no per-column truncation.
6. A single-cell row renders exactly as today — file suggestions, ask prompt, approval
   prompt, and popup `body` text are unaffected.

## 1. Popup core: cell-based rows with a column-alignment engine

**What:** In `internal/tui/popup.go`: add `type popupRow []string`; change
`popupSpec.rows` from `[]string` to `[]popupRow`; implement the Column contract
(width-per-column over all rows, two-space gutter, empty-column collapse, right-trim)
before the existing marker/highlight/window/truncate steps. Replace the rune-count
truncation in `truncateLabel` (`internal/tui/autocomplete.go:662`) with display-width
ANSI-aware truncation owned by `popup.go` (use `github.com/charmbracelet/x/ansi`
`Truncate`/`StringWidth`, already a dependency), updating its call sites. Migrate all
five producers (`renderAutocomplete`, `renderPicker`, `renderSessionBrowser`,
`askPrompt`, `approvalPrompt`) mechanically via a `singleCellRows(labels []string)
[]popupRow` helper — no visual change in this item. Rewrite the contract comment at the
top of `popup.go` to describe the cell model. Precedent for the padding math:
`renderToolBranch` / `startupLabelWidth` in `internal/tui/render.go`.

**Tests:** in `internal/tui/popup_test.go` — new: second-column start offset identical
across rows of a multi-cell spec; widths computed from all rows (a wide off-window row
still sets the column); absent-tier empty cell keeps later columns aligned; all-empty
column collapses; CJK/wide-rune cells measured in display cells; truncation of a
wide-rune row never exceeds the line width; a single-cell spec renders byte-identical to
the pre-change output. Existing invariants must keep passing unmodified:
`TestRenderPopupLinesAreExactWidth`, `TestRenderPopupIsFullyBlackFilled`,
`TestRenderPopupLongRowDoesNotWrap`, `TestRenderPopupSelectedRowHighlight`,
`TestRenderPopupNoSelection`, `TestPopupRowWindow`, `TestRenderPopupDegenerateWidth`.

**Acceptance:** `go test ./internal/tui/ -run 'TestRenderPopup|TestPopup'` passes;
`make check` passes.

**Commit:** `feat(tui): cell-based popup rows with vertical column alignment`

## 2. Slash menus: commands and skills emit cells

Depends on item 1.

**What:** In `internal/tui/autocomplete.go`: replace `acItem.label` (one pre-concatenated
string, `autocomplete.go:51`) with a `cells popupRow` field. Producers emit fixed
schemas — `commandSuggestions` (`:275`): `["/name", "<summary>", "<idle-only tag>"]`
(tag cell empty unless busy-gated); `slashSuggestions` (`:335`): `["✦ /value",
"<label>"]` — commands and skills share the merged "/" menu, so their summary columns
align with each other; `skillSuggestions` (`:362`): `["DisplayName", "<Summary>"]`;
`fileSuggestions` (`:398`) stays single-cell. Prefix matching and accept-on-enter keep
operating on `value`/`name`, never on display cells. The idle-only tag keeps its faint
styling per `layout.md:353`.

**Tests:** update the label assertions in `internal/tui/autocomplete_test.go`
(`TestAutocompleteSkillDropdownChrome`, `TestAutocompleteCommandAndFileTitles`,
`TestAutocompleteDropdownSpansFullWidth`), `internal/tui/skill_test.go:273`,
`internal/tui/command_test.go:178`, and `internal/tui/interject_test.go:694` to the cell
model; new test: in a merged "/" menu with names of different lengths, every summary
starts at the same column.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): align command and skill columns in the slash menus`

## 3. Pickers emit cells

Depends on item 1.

**What:** In `internal/tui/picker.go`: `modelRows` (`:628`) → `["model", "— 32k"]`;
`serverRows` (`:644`) → `["name", "— endpoint", "· current"]` (last cell empty for
non-current rows); `launchProfileRows` (`:407`) → fixed five-column schema `["name",
"— backend", "· 32k", "(:8080)", "· running"]` with empty cells for absent tiers.
`stripEscapes` (`picker.go:425`) is applied per cell.

**Tests:** rewrite the exact-string assertions in `internal/tui/picker_test.go` against
cells: `TestModelPickerListsTheLaunchProfiles` (the `reflect.DeepEqual` at `:807-810`),
`TestServerPickerListsTheConfiguredServers` (the `HasSuffix(…, currentRowSuffix)`
check), `TestModelPickerListsTheOffering`; new test: profiles with different name
lengths render `— backend` at one shared column.

**Acceptance:** `go test ./internal/tui/ -run 'TestModelPicker|TestServerPicker'`
passes; `make check` passes.

**Commit:** `feat(tui): align picker rows into columns`

## 4. Session browser emits cells

Depends on item 1.

**What:** In `internal/tui/sessions.go`: `sessionRows` (`:390`) / `sessionRowLabel`
(`:408`) → `["Title", "· relative time", "· N msgs"]`. The all-workspaces variant keeps
the workspace base inside the title cell (it qualifies the title, it is not a tier of
its own — see `sessions_test.go:466`).

**Tests:** update `internal/tui/sessions_test.go` label assertions; new test: sessions
with different title lengths render the time column at one shared offset.

**Acceptance:** `go test ./internal/tui/ -run 'TestSession'` passes; `make check`
passes.

**Commit:** `feat(tui): align session browser rows into columns`

## 5. Docs and ticket closeout

Depends on items 1–4.

**What:** Amend `layout.md`: in `## The prompt box's mini-language`, state that dropdown
rows render name and summary as vertically aligned columns; in the "One overlay for
'which one?'" passage, update the literal row-grammar examples to the aligned form and
add the Column contract in prose (per-column max display width across all rows,
two-space gutter, separators lead their cells, alignment stable while scrolling,
whole-row truncation). Close the entry at `ISSUES.md:7` following the same convention
commit `bf527ed` used for the sub-agent-rail entry. Add a CHANGELOG.md entry under the
current unreleased section — do not add or modify any release heading and do not touch
`VERSION`.

**Tests:** none (docs only).

**Acceptance:** `grep -i "aligned" layout.md` hits the mini-language section; the
ISSUES.md pop-up-alignment entry is closed; `make check` still passes.

**Commit:** `docs: spec popup column alignment and close the ISSUES entry`

## Suggested version bump

This is a visible TUI behavior change across every popup kind; a patch-level bump (next
0.10.x) at the owner's next release cut seems warranted. No version identifier is
changed by this plan — whether and when to bump is the owner's decision.
