# The session's name rides the top rule — and the window title is withdrawn

**Goal:** the frame says which session it is, on a row apogee actually paints. The `▔` top-edge
hairline that already caps the bottom chrome carries the session's name, centered, in place of the
rule runes it would otherwise draw there — `▔▔▔▔ the name ▔▔▔▔`. The terminal window/tab title is
withdrawn whole: it was landed on 2026-08-03 and, on the owner's own machines, shows nothing.

**Date:** 2026-08-03
**Status:** ready to execute
**Depends on:** nothing. `Model.sessionName` and `nameSession` (`internal/tui/autotitle.go`) already
landed and are kept — this plan changes only what READS them.

## Authoritative sources

- **The shape:** `docs/layout/prompt-box-layout.md` — the owner's sketch, updated 2026-08-03. The
  hairline row that was blank in the sketch now reads
  `▔▔▔▔▔▔ centered session name ▔▔▔▔▔▔`. If any item below disagrees with that sketch, the sketch
  wins.
- **The row it lands on:** `layout.md` — the frame's row inventory (the gap row, the `▔` hairline,
  the status line, the box, the footer, the `▁` hairline). This plan adds NO row.
- **The measure:** [ADR 0030](docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md)
  — everything painted into the cell buffer is measured with `m.th.measure`, in cells. This is the
  substantive difference from the withdrawn window title, which clipped in RUNES because a title bar
  is not a cell buffer apogee measures.
- **The value-copy invariant:** [ADR 0011](docs/adr/0011-the-tui-model-is-a-value.md) and
  `internal/tui/doc.go` — the `Model` is copied on every `Update`; nothing added here may hold a
  no-copy type by value.

## Why the window title goes rather than stays

The owner ran the shipped build on 2026-08-03 and no tab or window showed the name:
Terminal.app read `apogee apogee - 80x24`, VS Code read `apogee`, Zed read `airic - zsh`. The
research behind that — VS Code surfaces an OSC title only for an allowlist of agent CLIs matched on
**process name** (`claude`, `codex`, `commandcode`, `copilot`, `gemini`; VS Code 1.117, PR
microsoft/vscode#304528), and emitting `OSC 0` instead of `OSC 2` would change nothing because
xterm.js routes both to one title event — is recorded in the NOTES of item 3 of
`docs/plans/2026-08-03 - 00 - session-name-window-title-plan.md`, which item 1 archives. Nothing of
that research needs re-deriving; it is why the route is being abandoned rather than repaired.

## Standing requirements

- Forward **`/coding-standards`** to every implementer and verifier (the owner asked for it on the
  previous wave in this package, and it still applies).
- Commit straight to `main`; **no AI attribution trailers**; run `make check` before every commit.
- **Never** touch `VERSION` or the CHANGELOG release heading — see the closing note.
- Any authorized deviation from an item's text lands as a dated `NOTES` line under that item.

## Precondition — a clean tree

The working tree carries uncommitted doc edits from the research session (`layout.md`, `README.md`,
`TODO.md`) plus the owner's `docs/layout/prompt-box-layout.md` sketch. Item 1 owns those regions
**whole**, so either committing them or `git checkout --`-ing the three doc files works — but the
tree must be clean before the run starts, or Phase 0 stops.

## Out of scope

- The naming machinery itself: `Model.sessionName`, `nameSession`, `/rename`, the automatic naming
  call and the `/sessions` browser's `r` are untouched. This plan only reads the name they set.
- Any new row, any change to the row budget, the transcript's height, or mouse hit-testing. The rule
  row already exists (`topRuleHeight`); it gains content, not neighbours.
- Any status decoration on the rule — no spinner, no clock, no working marker. The rule states an
  IDENTITY; everything that changes every frame already has a home in the chrome below it.
- Any further attempt to reach the terminal's own title bar or tab, by any escape sequence, and any
  upstream PR to VS Code. That route is closed, not deferred.
- `VERSION`, the CHANGELOG release heading, tags.

---

## 1. Withdraw the terminal window title — ✅ DONE (2026-08-03)

NOTES (2026-08-03): the item's mandate — "every claim that documents it" — reached four files its
bullet list does not name, all comment-only, all of them left either citing the deleted
`windowtitle.go` or asserting a terminal window that no longer exists: `internal/title/title.go`
(the `StripEscapes` doc block named `internal/tui/windowtitle.go` as its second consumer and
justified the export by the `OSC 2` BEL-termination hazard — reworded to the frame's own rows, where
a control character breaks a row's measure, so the export stays justified for item 2 without citing
a deleted file), `internal/tui/model.go` (the `sessionName` field block and the `/clear` reset's
trailing comment), `internal/tui/autotitle.go` + `autotitle_test.go` and
`internal/tui/sessions.go` + `sessions_test.go` (prose reworded from "the window" to "the frame";
the *prompt*-window and context-window senses of the word were left alone). None of them name the
new seam — item 3 owns naming `sessionrule.go`. Also: `docs/handoffs/archived/2026-08-03 - 00 -
window-title-vscode-tab-question.md` and the archived plan itself keep their window-title prose as
historical record, and the two stale citations of the moved plan path (this file's line 37, that
handoff's line 14) were left as written.

**What:** remove the OSC-2 window title and every claim that documents it, keeping the two things
item 2 needs (`Model.sessionName`, `title.StripEscapes`).

Code:

- Delete `internal/tui/windowtitle.go` and `internal/tui/windowtitle_test.go` whole.
- `internal/tui/model.go` — delete both `v.WindowTitle = m.windowTitle()` assignments and the
  comment block above each: the one in `View`'s `!m.ready` branch (~line 2749-2752, the placeholder
  `tea.NewView("apogee — starting…")` stays) and the one at the end of `View` (~line 2805-2808).
  `View` must return a `tea.View` that never sets `WindowTitle`.
- `internal/tui/doc.go` — remove the `windowtitle.go` clause from the package inventory (~line
  415-417, "windowtitle.go the one thing the frame says OUTSIDE its own rows … `[tea.View.WindowTitle]`
  assignment comes from"). Leave the rest of the sentence's list intact and grammatical.
- **Keep** `Model.sessionName`, `nameSession` and `sessionTitle` — item 2's source. **Keep**
  `title.StripEscapes` exported (`internal/title/title.go`): it was exported for the OSC payload and
  item 2 keeps using it for the same reason, so it does not become dead.

Docs — this item owns the REMOVAL of every window-title claim; item 3 owns the new text:

- `layout.md` — delete the whole `## The terminal window's title` section (its heading through the
  paragraph ending `set -g set-titles on`, plus the trailing `---` that separates it from
  `## The staged-interjection band`, leaving exactly one separator between the sections that remain).
  Any VS Code/allowlist paragraph the research session added inside that section goes with it.
- `README.md` — delete the sessions bullet beginning "**The terminal window is named after the
  session**" whole, including its VS Code and Zed sentences and its tmux sentence.
- `CHANGELOG.md` — delete the `[Unreleased] → Added` entry beginning "**The terminal window wears
  the session's name**". It was never in a released version, so there is nothing to record as
  removed: an unreleased entry for a withdrawn feature is simply deleted.
- `TODO.md` — if the research session left a `## VS Code names agent CLIs from an allowlist` entry,
  delete it and add one line to `## Closed entries — the one-line trail`:
  the allowlist route is moot because apogee no longer sets a window title at all (2026-08-03).
- `docs/plans/2026-08-03 - 00 - session-name-window-title-plan.md` — mark its item 3 **WITHDRAWN**
  with a dated NOTES line (the owner's three terminals showed nothing; the feature is withdrawn by
  this plan, so the manual pass is moot), then `git mv` the file into `docs/plans/archived/`.
- `ISSUES.md` — no change. The bullet "I'd like to see the current session's name somewhere" was
  retired by the withdrawn wave and this plan is what actually satisfies it; do not re-add it.

**Tests:** no new tests. `go build ./...` and `make check` must be green with `windowtitle_test.go`
gone — in particular `internal/tui` must not reference `formatWindowTitle`, `windowTitle`,
`windowTitleMark`, `windowTitleRunes` or `windowTitleUnnamed` anywhere after this item.

**Acceptance:**

- `grep -rn "WindowTitle\|windowTitle\|formatWindowTitle" internal/ layout.md README.md CHANGELOG.md`
  returns nothing.
- `ls internal/tui/windowtitle*.go` finds no file.
- `grep -n "The terminal window" layout.md README.md CHANGELOG.md` returns nothing.
- `ls "docs/plans/archived/2026-08-03 - 00 - session-name-window-title-plan.md"` succeeds and the
  file is no longer in `docs/plans/`.
- `make check` green.

**Commit:** `revert(tui): withdraw the terminal window title`

---

## 2. The top rule carries the session's name — ✅ DONE (2026-08-03)

NOTES (2026-08-03): one test the item's list does not name had to change, because this item
invalidates its premise rather than its assertions:
`TestAutoTitleLeavesTheTranscriptUntouched` (`internal/tui/autotitle_test.go`) asserted
`plain(withTitle.View()) == plain(without.View())` under the claim "titles surface only in the
browser" — now false by design, since a landed title lands on the rule. The assertion was narrowed
rather than dropped: every row of the two views must still match EXCEPT the rule row, and the rule
row is asserted positively on both sides (the named model wears the landed title and not the
heuristic; the unnamed one wears the heuristic from its opening request), so the "naming is not a
Turn" guarantee is still pinned over the whole frame. Two constants beyond the item's sketch:
`sessionRuleRune = "▔"` (the rule's glyph, so the layout and its tests share one definition of it)
and no other addition. `internal/tui/doc.go` was left alone — item 3 owns naming `sessionrule.go`
in the inventory.

**What:** the existing `▔` hairline row renders the session's name centered on it. New file
`internal/tui/sessionrule.go`; one existing function changes (`Model.topRule`, `internal/tui/model.go`
~line 3088-3095).

Depends on item 1 (it deletes the file whose ladder this item re-homes).

The design, decided — an implementer should not re-derive any of it:

- **Shape.** `<▔ run><space><name><space><▔ run>`, the two spaces always present so the name never
  touches a rule rune. Total width is exactly `m.width` cells, measured with `m.th.measure`
  (ADR 0030) — this row is painted into the cell buffer, unlike the withdrawn title.
- **Which name.** The same three-answer ladder the withdrawn `windowTitle()` used, minus its
  `apogee` fallback: `m.sessionName` if set; else `sessionTitle(m.transcript.firstUserText())` when
  the transcript has a first user text (which names the rule from the human's opening request
  instantly, hours before a naming call can answer); else **no name**. The gate is "the transcript
  has a first user text", not "sessionTitle returned something" — `sessionTitle` answers a dated
  `Session 2026-08-03` fallback for a session that has said nothing, and a rule naming the calendar
  would be worse than a rule naming nothing.
- **An unnamed session gets an unbroken rule.** No `apogee` placeholder: the rule is a rule that
  CARRIES a name when there is one, and the frame is already unmistakably apogee's from every other
  row. `/clear` therefore returns the row to a plain rule with the session it starts, through the
  existing `m.sessionName = ""` at `model.go:1200`.
- **The name is untrusted twice** — a model's reply to the naming call, and a stored record's
  `Meta.Title` read back off disk, which nothing sanitizes on the way in. It goes through
  `title.StripEscapes` (whole escape sequences AND every non-whitespace control character, the
  strong strip) and then `strings.Fields`/`Join` to collapse whitespace runs, so a pasted multi-line
  name occupies one row rather than smuggling a newline into the frame. Here a control character is
  a LAYOUT bug as much as a security one: it breaks the row's measure, and every row of this frame is
  squared to the window.
- **The only cap is the room the rule leaves** (owner's call, 2026-08-03). There is no fixed
  maximum: a name is shown WHOLE whenever it fits, and is clipped only when it would not.
  `sessionRuleMinSegment = 3` — three `▔` cells and one space stay on EACH side, always, so the row
  reads as a rule carrying a name rather than as a caption with two stubs. That fixes the room at
  `w - 2*(sessionRuleMinSegment + 1)` — **`w - 8`** — and the name is truncated (with `…`, via
  `measure.Truncate`) to that and only that. A short name in a wide window keeps its long `▔` runs;
  it is the runs that flex, not the name that shrinks to some arbitrary number.
- **Degradation.** `sessionRuleMinName = 6` — below six cells a name is not a name, it is an
  ellipsis with a hint. So when `w - 8 < sessionRuleMinName` the row degrades to a plain unbroken
  rule. The boundary is crisp and testable: `w = 13` gives a plain rule, `w = 14` gives a six-cell
  name between two three-cell runs.
- **Centering.** `fill := w - measure.Width(name) - 2`; the lead run gets `fill / 2` and the trail
  run gets the remainder, so an odd column goes to the RIGHT and the name sits one column left of
  dead centre. State this in the doc comment — a reader who counts will otherwise think it a bug.
  Note that the three-cell guarantee falls straight out of the `w - 8` clip and needs no second
  check: a name at exactly the clip leaves `fill == 6`, which is three cells to each run, and every
  shorter name leaves more.
- **Style.** The `▔` runs keep `m.th.hairline` (dim gray on black, the recessive rule role they
  already have). The label — the name AND its two spaces — is rendered with `m.th.statusBar` (faint
  on black), so it reads as a label sitting on the rule rather than as more rule, and the black
  field stays unbroken across the seam. No new theme style is added.

The code:

```go
// in internal/tui/sessionrule.go
// sessionRuleMinSegment is the ▔ run that stays on each side of the name, and with the space
// beside it fixes the name's room at w - 2*(sessionRuleMinSegment+1).
const sessionRuleMinSegment = 3
const sessionRuleMinName = 6

// sessionRuleName: the ladder above. Returns "" for an unnamed session.
func (m Model) sessionRuleName() string

// sessionRuleLayout splits one rule row of width w into its three pieces: the ▔ run before the
// label, the label itself (the two spaces included), and the ▔ run after it. An unnamed session —
// or a window too narrow to show a name honestly — returns the whole row as lead, an empty label
// and an empty trail. Pure, so the geometry is testable without a theme.
func sessionRuleLayout(measure widthAuthority, name string, w int) (lead, label, trail string)
```

`Model.topRule` (`internal/tui/model.go`) then composes the three pieces with the two styles and
returns the row; its doc comment gains what the row now carries and why (identity, not gauge), and
keeps what it already says about where the row sits. Guard `w <= 0` — `strings.Repeat` panics on a
negative count, and a zero-width model is reachable before the first `WindowSizeMsg`.

**Tests:** `internal/tui/sessionrule_test.go`.

- `TestSessionRuleLayout` — a table over the pure function, asserting for every case that
  `measure.Width(lead+label+trail) == w` and that the row is composed only of `▔`, the label's two
  spaces, and the name: an unnamed session (whole row `▔`); a plain short name (label is
  `" name "`, both runs non-empty); the centering rule (odd `fill` puts the extra column in the
  TRAIL run); **a name that fits is never touched** — a 40-cell name at `w = 100` keeps all 40 cells
  and carries no `…`, which is the case a fixed cap would get wrong; a name exactly at `w - 8`
  (whole, and both runs exactly 3); a name past `w - 8` (clipped to exactly `w - 8` WITH the `…`
  counted inside it, both runs still exactly 3); a CJK name clipped in CELLS, not runes — the ADR
  0030 point, and the case a rune-based clip gets wrong; a name carrying
  `\x1b]2;pwned\x07`, a bare `BEL`, a bare `ESC`, a tab and a newline (no rune for which
  `unicode.IsControl` holds survives — the `transcript_test.go:439` posture); an all-control name
  and an empty name (plain rule); and widths `0, 1, 5, 13, 14, 200` (never panics, the sum invariant
  holds, `13` is a plain rule and `14` carries a six-cell name).
- `TestTopRuleCarriesSessionName` — on the `Model`: unnamed → `ansi.Strip(m.topRule())` is
  `strings.Repeat("▔", m.width)`; after `nameSession("the hairline wave")` → the stripped row
  contains that name, still measures exactly `m.width`, and still starts and ends with `▔`; after a
  first user prompt with no name yet → the row carries the heuristic name; after `/clear` → the
  plain rule again.
- `TestTopRuleHairlineRow` (`internal/tui/model_test.go:2000`) — its "the whole row is `▔`"
  assertion now holds only for an UNNAMED session. Check `newTestModel(t)`: if its transcript has no
  user text the test passes unchanged and only wants a comment saying why; if it does have one, name
  the expectation explicitly rather than weakening the assertion.

**Acceptance:**

- `go test ./internal/tui/ -run 'SessionRule|TopRule' -count=1` passes.
- The frame's row inventory is unchanged: `grep -n "frameFixedRows\|topRuleHeight" internal/tui/model.go`
  shows the same arithmetic as before this item, and `go test ./internal/tui/ -count=1` passes
  whole (the layout, mouse and sessions-browser suites all budget that row).
- `make check` green.

**Commit:** `feat(tui): the top rule wears the session's name`

---

## 3. The docs say what the rule says — ✅ DONE (2026-08-03)

NOTES (2026-08-03): two elaborations on the item's literal text, both in `layout.md`. The sketch at
line 49 carries the owner's own placeholder wording — `centered session name`, the exact label in
`docs/layout/prompt-box-layout.md` — rather than an invented example name, because "Match
`docs/layout/prompt-box-layout.md`" reads most safely as matching it verbatim; the row still measures
the sketch's 87 columns (32 `▔`, space, 21-cell label, space, 32 `▔`). And the new section's **What it
says** paragraph closes with one sentence on the centering bias (the extra column goes RIGHT), which
the item lists only for item 2's doc comment: a reader of the spec who counts the sketch's two runs
would otherwise meet an off-by-one and take it for a bug. Everything the item enumerates is present
under the six bold lead-ins it names.

Depends on items 1 and 2.

**What:** documentation only — no code.

- `layout.md`:
  - The sketch at the top of the file (~line 49) — the `▔` row above the status line gains a
    centered name, so the sketch a reader meets first shows the row as it paints. Match
    `docs/layout/prompt-box-layout.md`.
  - A new `## The top rule wears the session's name` section in the slot item 1 emptied (between
    `## The footer's upstream slot` and `## The staged-interjection band`). In the house voice, with
    the file's bold lead-ins: **What it says** (the shape, the two spaces, and that the name is
    clipped ONLY by the room the rule leaves — `w - 8`, three `▔` and a space each side — measured
    in CELLS and why cells and not runes); **which name** (the three-answer ladder and why the gate
    is "has a first user text"); **what an unnamed session gets** (the unbroken rule, and why there
    is no placeholder); **what it deliberately does not say** (no spinner, no clock — identity, not
    gauge, and everything live has a home in the chrome below); **how it degrades** (the
    minimum-name floor and the `w = 13` / `w = 14` boundary); and **the strip**
    (`title.StripEscapes`, untrusted twice, a control character breaks the measure as well as the
    trust). It must NOT mention terminal titles, OSC sequences, VS Code, Zed or tmux — that whole
    subject left the document with item 1.
- `README.md`: one sessions bullet replacing the one item 1 deleted — the session's name is written
  on the rule above the status line, so a screen full of panes says which conversation each is.
  Two facts a user can act on and no more: it follows `/rename` and the automatic name, and an
  unnamed session shows a plain rule. No terminal configuration is involved in any of it.
- `CHANGELOG.md` under `## [Unreleased]` → `### Added`: the new entry in the house voice — what the
  row says, that a name is clipped only when the rule has no room for it, the unbroken rule when
  unnamed, that it costs the frame no row because
  it rides the hairline that was already there, and (one clause) that the terminal-title route tried
  earlier the same day was withdrawn because it reached no tab on any terminal tested.
- `internal/tui/doc.go`: name `sessionrule.go` in the package inventory, in the slot the
  `windowtitle.go` clause vacated — the session name the top rule wears, and where `Model.topRule`
  gets it.

**Tests:** none beyond `make check` (docs only).

**Acceptance:**

- `grep -n "top rule" layout.md README.md CHANGELOG.md` finds the new text in all three.
- `grep -rn "OSC\|WindowTitle\|terminal.integrated" layout.md README.md` returns nothing about the
  window title.
- `grep -n "sessionrule.go" internal/tui/doc.go` finds the inventory line.
- `make check` green.

**Commit:** `docs(tui): specify the session name on the top rule`

---

## Suggested version bump

This wave withdraws an unreleased feature and adds a user-visible one, all inside `[Unreleased]`;
`VERSION` is `v0.10.9`. A **patch** bump to `v0.10.10` would be reasonable whenever the owner wants
one. **Do not bump it as part of this plan** — no item here touches `VERSION`, the CHANGELOG release
heading or a tag.
