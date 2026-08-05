# User-questions menu layout — implementation plan

- **Goal:** Restyle the two decision surfaces (approval prompt, ask prompt) into menu-style
  boxes per the owner's mockup: title embedded in the top border, selectable rows with
  `❯`/`·` pointers, arrow-key navigation, Enter confirms, letter shortcuts select-and-send,
  wrapped multi-line options.
- **Date:** 2026-08-04 · **Status:** ready
- **Authoritative sources (precedence order):**
  1. `docs/design/user-questions-layout.md` — the owner's mockup (pinned spec for visuals),
     as amended by the ratified design calls below. Where an item disagrees with the mockup
     + design calls, the mockup + design calls win.
  2. `layout.md` — the TUI layout spec; item 7 reconciles it, earlier items must not
     contradict its width/height/give-way rules.
  3. ADR 0011 (value-copied Model — no no-copy types by value), ADR 0030 (one width
     authority — all lines squared through `th.measure`).
- **Ratified design calls (owner, 2026-08-04, this session — these override the mockup where they touch it):**
  - **Full chat-area width** — both boxes keep today's full popup width; fit-to-content was rejected.
  - **Scope: decision prompts only** — picker, `/sessions` browser, and autocomplete keep
    today's look; new painter capabilities are opt-in via `popupSpec` fields defaulting off.
  - **Free-text custom answers stay** on the ask prompt (typing swaps the highlight off and
    sends the typed text); the ask box keeps a one-line dim hint row; **no digit shortcuts**
    (they would collide with typing).
  - **Selection style: pointer + color** — `❯` with an accented label on the selected row,
    dim `·` rows otherwise, **no background bar** (today's `userBlock` bar is not used in
    menu-style rows).
  - **`Cancel [esc]` row = today's Esc semantics** — cancel the in-flight run (`stopWorker`).
    No fourth `ApprovalDecision` is added; the engine seams (`Approver`, `Asker`) are untouched.
  - **Approval drops its hint legend** — the inline `[a] [s] [d] [esc]` shortcut column
    replaces it. Default selection is the first row (`Allow`), per the mockup.
  - **Ask prompt drops its border title** (`the assistant is asking:` goes away); the
    question text is the body. Blank separator lines between ask options, per the mockup.
- **Standing requirements:**
  - skills: coding-standards
  - Never change VERSION / CHANGELOG release heading / tags (see closing note).
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
  - `make check` green before every commit.
- **Out of scope:** restyling picker/sessions/autocomplete (capabilities land in the shared
  painter; adoption is a later plan); fit-to-content width; digit shortcuts; any change to
  `domain.ApprovalRequest`/`ApprovalDecision`/`AskRequest`/`AskAnswer` or the engine-side
  Approver/Asker contracts; version bump.

---

## 1. Painter: title embedded in the top border — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the title is spliced by COMPOSING the top border row in the box painter that
already owns border assembly (new `drawTitledBox` in `internal/tui/model.go`, with `drawBox` now its
untitled call), not by post-processing `drawBox`'s already-styled top line — that line is an ANSI
stream, and splicing into it would put a second border assembly in the tree. The width authority
still measures and fits the title exactly as the item asks (`popupTitleLine` → `truncateToWidth` at
the pane's inner width, then centred in `th.measure`). Chrome accounting is per-spec:
`popupBudget` gained a `chrome` parameter (existing callers pass `popupChrome`, a title-in-border
caller passes the new `popupTitleBorderChrome`); `frameRowPlan`'s per-pane floor stays `popupChrome`,
which cannot know a spec's title placement and is deliberately one row generous. The commit line's
"previously untracked" files were already committed before this item ran.

**What:** Add an opt-in title-in-border mode to the shared popup painter
`internal/tui/popup.go` (`renderPopup`, ~line 120; spec struct `popupSpec` ~line 104).
New `popupSpec` field (e.g. `titleInBorder bool`), default false. When set and `title` is
non-empty, the title is spliced into the top border line, centered, with one space each
side — `╭────── Approve terminal? ──────╮` per the mockup — instead of occupying a body
row; the freed row reduces the popup chrome for that spec (today `popupChrome = 4`,
popup.go:84 — audit every consumer of that constant and the height/budget math in
`internal/tui/model.go` (`popupBudget` ~line 3949, `frameRowPlan` ~line 3877) so a
title-in-border popup reports its true height). When set and `title` is empty, the top
border is plain (needed by the ask prompt later). The border is currently produced by the
lipgloss style `th.popupBorder` (theme.go:230) — splice the title by post-processing the
rendered top line through the width authority (`th.measure`, ADR 0030), preserving the
existing narrow-terminal title elision behavior (`popupTitleLine`, popup.go:423). Existing
consumers (approval/ask as of this item, picker, sessions, autocomplete) pass the flag
false and must render byte-identical to before.

**Tests:** In `internal/tui/popup_test.go`: title-in-border renders the title inside the
top border line at exact width; empty title yields a plain border; flag-off output is
unchanged for a representative spec; narrow-width elision still applies to an embedded
title; height accounting (chrome) is one row smaller with the flag on.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestPopup|TestRenderPopup' -v` — new cases pass, no prior case broken
- `make check`

**Commit:** `feat(tui): popup painter learns title-in-border mode` — this commit also adds
the previously untracked `docs/design/user-questions-layout.md` and this plan file.

## 2. Painter: menu-style row rendering (pointer + color, no bar) — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the accent the owner's decision calls for is `colCode` (#f0883e orange) — the tone
the theme already spends on every "this is apogee's own" accent (tool label, sub-agent rail, auto-mode
marker), so no colour was introduced; the new style is `theme.popupAccent` (bold accent on the pane's
black, applied to the ❯ and the label only, never squared to the inner width, so no bar appears). Two
additions the item's text implies but does not name: the unselected marker needed a glyph, added as
`glyphMenuUnselected` ("·") beside `glyphUser` in theme.go, and the tests needed an SGR probe helper
(`styleSGR`, popup_test.go). The style is named `popupAccent` rather than something longer because a
field name past `startupBorder`'s width makes gofmt realign the whole theme struct — a 70-line
whitespace diff over lines this item does not otherwise touch.

**What:** Add an opt-in menu row style to `internal/tui/popup.go`. New `popupSpec` field
(e.g. `menuRows bool`), default false. When set: the selected row renders `❯ ` plus the
row text in a new accent style (add to `internal/tui/theme.go`, mirroring the *role* of
llama-launcher's bold-cyan selected row — pick the accent per the existing theme palette,
theme.go:22-53); unselected rows render `· ` plus the row text in `th.statusFaint`; **no
full-width `userBlock` background bar** on any row. Flag off preserves today's bar
rendering exactly (popup.go:295-309) for the picker/sessions/autocomplete. Column
alignment of multi-cell rows (popup.go:208-220) works unchanged in menu style — the
approval item relies on it for the right-hand `[a]`-style shortcut cells.

**Tests:** In `internal/tui/popup_test.go`: menu-style selected row shows `❯` and no
background-bar styling; unselected rows show `·` and faint style; two-cell rows stay
column-aligned in menu style; flag-off rendering is byte-identical to before for a spec
with rows and a selection.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestPopup|TestRenderPopup' -v`
- `make check`

**Commit:** `feat(tui): popup painter learns menu-style rows (pointer + accent, no bar)`

## 3. Painter: wrapped multi-line rows and blank separators — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the hidden-row count stays ALL-OR-NOTHING — a partially shown (scrolled) list still
reports nothing, contrary to this item's parenthetical, because `layout.md` (precedence source 2) rules
that the rows outside a window the pane did get are "one keypress away and need no marker" and that only a
window seating NO row is counted onto the title. What the line accounting adds is one new way to reach that
zero case: a budget too small for even the selected row's own height seats nothing and counts every row,
rather than painting two thirds of an answer. The window authority itself is now line-based rather than
row-based (`popupRowWindow(selected, heights, gap, budget)`, one function instead of two, verified to
return the identical window for one-line rows with no gap); the wrapping is `wrapText`, the body block's
own break, applied at `inner - popupRowIndent` so the hanging indent is a rectangle under the marker
column. `popupRowIndent` (2) and the per-line styling split (`popupRowLine`) are additions the item's text
implies but does not name.

NOTES (2026-08-05): follow-up (run-level, owner-approved; not a plan item) — this item's rowGap policy is
UNCHANGED ("between rows only"), but the mockup also draws a blank line between the body block and the first
row, so the painter gained SEPARATE flags for the block's own ends: `popupSpec.rowPadAbove` and
`rowPadBelow` (`popupRowPadLines`, and `popupRowBlockLines` now takes the pad beside the gap). Separate from
the gap because the two say different things — the gap divides the options from EACH OTHER, the pad divides
the offering from the pane — and the approval menu asks for the pad without the gap. Separate from EACH
OTHER because the mockup does not spend both on both panes: the ask box closes its offering with a blank
(lines 25-40), the approval box ends on `· Cancel [esc]` with the border directly under it (lines 13-22), so
only the ask pane sets `rowPadBelow`. It is line-accurate the way the gap is: the callers state the padded
cost to `popupBudget` (`popupFlatRowHeights` is the non-wrapping caller's per-row cost), and the painter
draws the blanks only out of what the seated window LEFT OVER, dropping both ends whole where it did not, so
a blank line can never scroll an option off a pane that had the room to show it.

**What:** Extend `internal/tui/popup.go` so menu-style rows can wrap and be separated by
blank lines, both opt-in via `popupSpec` fields (e.g. `wrapRows bool`, `rowGap bool`),
default false. Wrapped rows: a row whose text exceeds the available width word-wraps onto
continuation lines with a hanging indent aligned under the first text column (two cells of
indent, past the `❯ `/`· ` marker), per the mockup's third option (mockup lines 33-35).
Today rows never wrap — they elide (popup.go:369-397); with `wrapRows` off that elision
path must remain untouched. `rowGap` inserts one blank fill line between consecutive rows
(not before the first or after the last). The row scroll window (`popupRowWindow`,
popup.go:443) and `maxRows` budget currently count one line per row — rework the
accounting so a wrapped row consumes its real line count and separators are counted,
keeping the shrink guarantees of layout.md (a partially shown list still names how many
rows are hidden). Selection highlighting covers every line of a wrapped selected row.

**Tests:** In `internal/tui/popup_test.go`: a long row wraps with hanging indent at exact
width; separators appear between rows only; selection accent spans all lines of a wrapped
row; window/budget math counts wrapped lines and separators (a capped list reports hidden
rows correctly); `wrapRows`/`rowGap` off → byte-identical output.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestPopup|TestRenderPopup' -v`
- `make check`

**Commit:** `feat(tui): popup painter learns wrapped rows and row separators`

## 4. Approval prompt becomes a navigable menu — ✅ DONE (2026-08-04)

Depends on items 1 and 2.

NOTES (2026-08-04): the pane hands `popupBudget` a NEW chrome constant — `popupBorderChrome`
(`internal/tui/popup.go`, `popupTitleBorderChrome - 1`) — rather than the `popupTitleBorderChrome`
item 1's note anticipated for a title-in-border caller. That constant budgets for a hint row, and
this pane draws neither a title row NOR a hint row (the legend is dropped per the ratified call), so
its two borders are its whole frame; `popupBudget` documents chrome as the CALLER's precisely because
only the caller knows which rows its spec draws, and the honest value is what puts a decision row on
the screen between 12 and 15 terminal rows instead of leaving the freed row unclaimed. `approvalKeys`
is now DERIVED from the new `approvalMenu` list (`approvalMenuKeys`) instead of standing as a second
literal map, so the letters the keys accept and the `[a]`-style cells the pane paints cannot drift —
the map's name and behaviour are unchanged. Beyond the tests the item names, the chrome and label
changes forced three more: `TestModelApprovalNamesTheProseItCannotShow` (its placement assertion now
reads the border row, and its unreachable no-body-budget branch became the body's floor-of-one-row
marker), `TestDecisionSurfaceStaysOnTheFrame`'s approval probe, and `TestModelSeamMessageTransitions`'
lower-case "allow"/"deny" check. The hidden-row count stays all-or-nothing (item 3), so a window
seating only some of the four options scrolls rather than counting them onto the border title.

NOTES (2026-08-05): follow-up (run-level, owner-approved; not a plan item) — the body's parts now join with
`"\n"` rather than `"\n\n"`: the mockup keeps `Reason:` and `Command:` ADJACENT (two labelled facts about one
call) and spends that blank line between the body and the first menu row instead (`popupSpec.rowPadAbove`,
item 3's follow-up note). That one blank is the whole of this pane's spacing — the mockup's four decisions
are adjacent to each other and `· Cancel [esc]` sits directly on the bottom border, so `rowPadBelow` stays
off here and only the ask pane sets it. So the pane's row demand is its menu in LINES rather than its row
COUNT — `popupRowBlockLines(popupFlatRowHeights(len(rows)), 0, popupRowPadLines(true, false))` — which is
what books the blank. Where the window leaves the body fewer rows than it wants, the pad now costs it that
line, counted as ever in the "… (+N more lines)" marker; where the window cannot cover it at all it gives
way whole, so every decision row this item put on the screen is still on it at every height.

**What:** Rebuild the approval prompt on the new painter capabilities. In
`internal/tui/model.go`:

- `approvalPrompt` (~line 3984): title becomes `Approve <tool>?` (capitalized, escape-
  stripped tool name kept verbatim as today), rendered title-in-border (item 1). Body keeps
  its current content this item (reason + pretty-printed arguments — item 5 restyles it).
  Rows (menu style, item 2), two cells each — label + shortcut hint, column-aligned:
  `Allow` `[a]` / `Always allow this session` `[s]` / `Deny` `[d]` / `Cancel` `[esc]`.
  The hint legend line is dropped entirely. Selection state: new `Model.approvalSel int`
  (plain value, ADR 0011), reset to 0 (`Allow`) on each `approvalReqMsg` fold
  (model.go:521-529).
- Key handling (`handleApprovalKey`, model.go:1035; routing at model.go:922-924): ↑/↓ move
  `approvalSel`, clamped, non-wrapping (matching the ask prompt's arrows, model.go:931-945).
  Enter resolves the selected row — replace the current Enter no-op for
  `stateAwaitingApproval` (model.go:898-899). Letter keys `a`/`s`/`d` keep their current
  direct select-and-send behavior (approvalKeys map, model.go:1022). Rows 0-2 reply on
  `m.pending.Reply` exactly as today; row 3 (`Cancel`) and the Esc key both take today's
  Esc path — cancel the in-flight worker (model.go:871-877 semantics; the prompt clears on
  `cancelledMsg` as today, model.go:1587). Other keys still fall through to
  `scrollViewport` (model.go:1044) — the prompt stays soft-modal, with pgup/pgdn scrolling
  while ↑/↓ now belong to the menu.

**Tests:** Update/extend `internal/tui/model_test.go`: rewrite
`TestModelApprovalPromptPopupChrome` (~line 757) for the new chrome — asserts
`Approve write_file?` in the top border, the four row labels with `❯` on `Allow`, aligned
`[a]`/`[s]`/`[d]`/`[esc]` cells, and the absence of the old hint legend.
`TestModelApprovalDecisionKeys` (~line 660) still passes for a/d/s. New tests: ↓↓ then
Enter sends `ApprovalDeny`; Enter with no navigation sends `ApprovalAllow`; ↓ to `Cancel`
then Enter cancels the worker (state stays `stateAwaitingApproval` until `cancelledMsg`,
mirroring `TestModelApprovalCancelClearsPrompt` ~line 733); an unrelated key still scrolls
and sends nothing (`TestModelApprovalIgnoresOtherKeys` ~line 713 updated for arrows/Enter
now being live). Check `internal/tui/e2e_test.go:234-243` (auto-approve via `'a'`) still
holds.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestModelApproval' -v`
- `go test ./internal/tui/` — full package
- `make check`

**Commit:** `feat(tui): approval prompt becomes a navigable menu with inline shortcuts`

## 5. Approval body: Reason and Command rendering — ✅ DONE (2026-08-04)

Depends on item 4.

NOTES (2026-08-04): the `Command:` block has a THIRD fallback trigger beyond the "malformed/missing"
pair this item names — any FURTHER argument beside `command` (a `workdir`, a `timeout_seconds`) also
falls back to the JSON. The item's own reasoning is why: the JSON "keeps the security property that
the user sees the raw arguments", and the terminal case may drop it only because the shell line is
the whole of that tool's blast radius. A `workdir` naming where the line runs is not in the line, so
a command-only body would hide it — the fallback is what makes "display-only" true rather than
"display-most". The mockup's shape is unaffected: `{"command": …}` alone is what it draws. Also
disqualifying: a whitespace-only command (read as the missing case — the label would head an empty
line). The tool is matched by its wire name `"terminal"` as a package-local constant, not imported
from `internal/tools`, per this package's standing no-import rule (`internal/tui/doc.go`) and exactly
as `toolPresenters` already keys on it.

**What:** Restyle the approval body per the mockup (lines 11-15). In
`internal/tui/model.go` `approvalPrompt`: the reason line becomes `Reason: <reason>`
(capitalized label). For the subprocess/terminal tool (the tool gated by the
`subprocess execution…` reason — locate its canonical name in `internal/tools` and the
gate in `internal/agent/resolution.go:495-510`), extract the command string from
`req.Arguments` and render it as a `Command:` label with the command on its own indented
line(s), instead of pretty-printed JSON. Every other tool keeps the pretty-printed
arguments JSON exactly as today (the mockup only specifies the terminal case; JSON keeps
the security property that the user sees the raw arguments). Extraction is display-only —
`domain.ApprovalRequest` is untouched; malformed/missing command field falls back to the
JSON rendering. Escape-stripping and body-cap behavior
(`TestModelApprovalNamesTheProseItCannotShow`, model_test.go:877) are preserved.

**Tests:** In `internal/tui/model_test.go`: terminal-tool approval renders `Reason:` and
`Command:` with the indented command and no argument JSON; a non-terminal tool still
renders its arguments JSON; malformed terminal arguments fall back to JSON; existing
wrap/cap/escape tests (`TestModelApprovalReasonWrapsInFull` ~778,
`TestModelApprovalLongArgsCapsBody` ~837, `TestModelApprovalEscapeStrips` ~805) updated
for the capitalized labels and still passing.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestModelApproval' -v`
- `make check`

**Commit:** `feat(tui): approval body renders Reason and Command labels for subprocess calls`

## 6. Ask prompt adopts the menu layout — ✅ DONE (2026-08-04)

Depends on items 1, 2, and 3.

NOTES (2026-08-04): `maxAskChoiceRows` caps the window in the lines that many ROWS cost
(`popupRowBlockLines(heights[:min(n, 8)], gap)`), not as a literal eight-line cap: with a blank line
between options, eight literal lines would scroll five one-line choices — the top of the schema's own
2-5 range — on a terminal with room for all of them, and the constant is named for an offering the
human counts in options. Two painter additions the item's text implies but does not name: the caller
now has to state its budget in lines the painter has not laid out yet, so `popupWrappedRowHeights`
(popup.go) composes exactly the calls `renderPopup` makes to reach the same per-row heights, and
`popupInnerWidth` is the inner-width computation both now share (renderPopup's own subtraction moved
into it, unchanged); `askRowGap` names the separator's cost. `internal/domain/ask.go`'s **comment** on
`Choices` was reworded with the schema — the type, and every engine seam, is untouched, but a doc
comment still promising "short, single-line" answers would contradict the tool description this item
relaxes. `sanitiseChoices` is unchanged: it neither rejects nor mangles long options, and a literal
newline is already broken into lines by the painter's `wrapText`. Three test-side consequences:
`TestOverlayNamesTheRowsItCannotShow`'s ask range is 12-15 rather than 12-16 (the row the dropped
title bought back lets the pane seat its first answer at 16, which is scrolling, not hiding — item
3's all-or-nothing count) and it reads the marker off the border row; `TestDecisionSurfaceStaysOnTheFrame`'s
ask probe is the question, with the elision marker IN THE PANE as the fallback, because a pane with no
title has no identity line left when the window grants it no body row.

**What:** Restyle the ask prompt per the mockup (lines 25-37). In
`internal/tui/model.go` `askPrompt` (~line 4023):

- Drop the `the assistant is asking:` title — plain top border (item 1's empty-title
  mode); the escape-stripped, wrapped question stays as the body.
- Choice rows render menu-style (item 2) with blank separators and wrapping with hanging
  indent (item 3); `maxAskChoiceRows` (model.go:4009) keeps capping the window, now in
  real lines per item 3's accounting.
- Behavior is unchanged (ratified call): ↑/↓ clamped while the input box is empty
  (model.go:931-945), Enter sends the selected choice label, typing swaps to free-text
  and drops the highlight (model.go:4024), draft borrow/restore untouched
  (model.go:531-550, 1534-1545). The one-line dim hint row **stays**, reworded to match
  the new surface (e.g. `↑↓ select · ⏎ send · type for a custom answer · esc cancel` /
  free-text variant, model.go:4027-4030).
- In `internal/tools/ask_user.go` (schema, lines 11-22): relax the choices description —
  drop "short, single-line" so the model may offer longer prose options (they now wrap);
  keep the 2-5 count. Adjust `sanitiseChoices` (ask_user.go:88-102) only if it rejects or
  mangles what the painter now supports (e.g. long options); newline handling stays
  whatever the painter needs — wrapped display comes from the painter, not from literal
  newlines.

**Tests:** In `internal/tui/model_test.go`: ask view has no title row and a plain top
border; options are `❯`/`·` menu rows with blank separators; a long option wraps with
hanging indent inside the box; `TestModelAskChoicesRoundTrip` (~1090),
`TestModelAskTypedTextOverridesChoices` (~1115), `TestModelAskArrowsWithTextKeepSelection`
(~1141), and the draft-borrow test (~1014) updated for the new chrome and still passing.
`internal/tools/ask_user_test.go` (if present) updated for the schema wording.

**Acceptance:**
- `go test ./internal/tui/ -run 'TestModelAsk|TestAsk' -v`
- `go test ./internal/tools/ -run 'TestAskUser|TestSanitise' -v`
- `make check`

**Commit:** `feat(tui): ask prompt adopts the menu layout with wrapped choice rows`

## 7. Reconcile layout.md with the new decision surfaces — ✅ DONE (2026-08-04)

Depends on items 4, 5, and 6. This item owns **all** prose amendments — earlier items
change no docs beyond code comments.

NOTES (2026-08-04): the re-derived arithmetic came out of the real panes rather than off the item's text, and
it moved two of the passages further than the item anticipated. `popupBudget` floors the BODY at one row
whenever a pane is seated (`maxBody = avail - maxRows`, `maxRows ≤ avail-1`), so with `popupBorderChrome`
the approval prompt has one body line AND one decision row at every window it is drawn in: its marker is
never on the title row, and the "with none left it moves onto the title row" case is now the browser/picker/
dropdown's and the ask prompt's ROW count. That is also why the narrow-ladder example is re-anchored on the
`/sessions` browser (`saved sessions  (all workspaces)  … +8` → `saved session…  … +8`) — the approval's
border title can no longer reach the state that example illustrates — and why the old line about the ask
"counting its four answers beside the question line it also dropped" became a plain statement that the four
answers ride the border while the question keeps its row. Widths in the amended prose are rendered, not
estimated. Two calls beyond the item's list: `CONTEXT.md` is left untouched — its Approval and Ask-user
entries describe the delegates and the free-text/permission distinction, none of which the restyle changes,
and neither carries a key legend or a title string to correct — and a CHANGELOG `### Changed` entry was
added under `[Unreleased]`, since this item owns all prose and the feature is user-visible (no release
heading, no version field touched).

**What:** Update `layout.md` wherever it describes the old surfaces, keeping its voice and
prose style: the row-budget/give-way passages naming the approval and ask prompts
(layout.md:87-137) — behavior unchanged, but re-derive the minimum-pane arithmetic
("smallest honest pane is four rows: its two borders, its title, and its key hint",
layout.md:197-200) for a title-in-border approval box with no hint row and an untitled ask
box with a hint; the shrink examples quoting `approve write_file?` (layout.md:205-228) —
now `Approve write_file?` in the border, and the count-what-you-drop guarantees restated
over wrapped rows; the column-contract note listing "the ask and approval prompts" among
single-cell surfaces (layout.md:902-904) — approval rows are now two-cell (label +
shortcut), ask rows remain single-cell; the recall note (layout.md:932-934) stays true.
Check `CONTEXT.md`'s Approver/Asker passage (~309-313) and ADR 0025's "a/d/s and Enter-
dismiss own the keyboard" line (adr 0025 ~line 156) for contradictions — amend the
CONTEXT.md wording if it now misdescribes the surface; ADRs are historical records: do not
edit ADR 0025's body, but if its statement is now materially wrong, add a dated
clarifying note at its end. `docs/design/user-questions-layout.md` stays as the pinned
mockup (committed in item 1); add a one-line status note at its top pointing at this plan.

**Tests:** none (prose). `grep -n "a allow" layout.md` returns nothing;
`grep -n "the assistant is asking" layout.md CONTEXT.md` returns nothing or only
deliberately historical mentions.

**Acceptance:**
- `grep -rn "a allow · d deny" layout.md CONTEXT.md docs/design/` — no hits
- `go test ./internal/tui/` — still green (no code change expected)
- `make check`

**Commit:** `docs: reconcile layout.md and CONTEXT.md with the menu-style decision prompts`

---

**Suggested version bump:** minor (`v0.10.16` → `v0.11.0`) — user-visible TUI behavior
change (new navigation keys on the approval prompt). Owner decides; no item touches
VERSION or CHANGELOG release headings.
