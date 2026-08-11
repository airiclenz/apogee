# Tool display overhaul — implementation plan

- **Goal:** implement the ratified tool-block layout: right-aligned outcome slot with
  dotted leader, mixed-type super-groups under `✦ Tools (N calls)`, sub-agent
  same-label grouping, click-anywhere/see-less interaction, and a keyboard block
  cursor — per the canon spec `docs/layout/tool-layout.md`.
- **Date:** 2026-08-10  **Status:** not started
- **sized for:** ~200k-context host
- **Authoritative sources:** `docs/layout/tool-layout.md` (canon, ratified 2026-08-10 —
  when any item's wording disagrees with it, the spec wins), `layout.md` (global
  grammar: width, colors, path shortening, body quoting), ADR 0011 (value-copied
  Model), `internal/tui/doc.go` (package rules).
- **skills:** coding-standards
- **Ratified design calls (owner, grill session 2026-08-10):**
  1. Super-group forms at 2+ adjacent same-depth runs of different labels (a lone call
     is a run of 1); breakers = today's group breakers (any non-tool entry) plus any
     sub-agent block.
  2. Super-groups fold **live** — the umbrella appears when the second
     different-label run starts; the running call is its last row with the spinner star.
  3. The right slot carries the **whole** summary: typed stats, promoted one-line
     output, and red `error: …` / `denied` / `cancelled`.
  4. Overflow order: dots flex to a floor of 1 `⋯` → the left target truncates with
     `…` → the right slot always prints whole.
  5. Promote-guard: a one-line output is promoted only if the row keeps ≥ 15 cells of
     target + 1 dot; otherwise it stays a body line and the slot shows the typed stat.
  6. Click toggles, drag selects: press+release without movement = toggle; deepest
     element under the pointer wins.
  7. Keyboard = modal block cursor: `alt+up`/`alt+down` enters + moves, plain
     `up`/`down`/`enter`/`esc` inside, any printable key returns to the prompt.
     Highlight = the existing selection-bar style.
  8. Exactly two fold states per call; `see less…` footer is an extra collapse target.
  9. Umbrella header reads `✦ Tools (N calls)` (N = total calls, faint count tone);
     its floor is its type rows; clicking it closes all open children.
  10. Type-row aggregate: red `N errors` first, else natural sum, else blank.
  11. Failure marking: red right-slot summary only — no glyph or header color changes.
  12. Sub-agents group with each other (`✦ Sub-Agent (N)`), never join super-groups.
  13. Per-tool table ratified as written, including label renames (Run→Terminal,
      Search→Grep, Web Search→Search, Edit File split into Edit / Replace, …).
  14. `N steps` / durations appear only where the engine **already** exposes them —
      no engine or wire changes for presentation; otherwise `done`/`failed` and bare
      exit codes.
  15. `tool-leader` role is new but seeded from the existing faint indicator tone
      (dark + light).
  16. `docs/layout/tool-layout.md` is canon; `layout.md`'s tool sections shrink to a
      pointer.
- **Out of scope:** three-state folding; a one-line umbrella fold; sub-agent
  membership in super-groups; ADR 0039 fan-out UI beyond the grouping here; any
  engine/wire change (step counts, durations); version bumps.

## 1. `tool-leader` role and the right-aligned outcome row — ✅ DONE (2026-08-10)

NOTES (2026-08-10): the `tool-leader` role lives in `internal/scheme/scheme.go` +
`schemes/dark.yaml` + `schemes/light.yaml` (the scheme vocabulary), not in
`internal/tui/colorscheme.go` — that file is the `/color-scheme` command and holds no roles.
The item's acceptance grep therefore reads `grep -n "tool-leader" internal/scheme/scheme.go`.

NOTES (2026-08-10): consequences of the one-row leader shape, all recorded deliberately:
(a) the `▶`/`▼` moved off a targeted block's HEADER onto its branch row (canon sketch);
the targetless shape, having no branch row, keeps it on the header. (b) A clipped target
alone no longer makes a block a toggle target — the row is identical open and closed, so
expanding revealed nothing; canon says a row with nothing to expand carries no indicator.
(c) `collapsedTargetRows` is gone: a targeted branch is always one row, so a collapsed
block is at most three rows. (d) Added the tail design call 4 does not word: an outcome
wider than the whole row is itself clipped, otherwise the row overruns the frame and the
viewport folds it. (e) `renderPlain` in the tests collapses a painted leader to a single
`⋯` so goldens read as shape rather than as width arithmetic.

NOTES (2026-08-10): checkpoint — done: `tool-leader` scheme role + theme style + both
built-in schemes; `leaderRow`/`toolRowCells`/`summaryStyle`/`failedSummary` in render.go;
indicator moved to the branch row; `branchText`/`groupMemberText`/`groupTargetCells`/
`collapsedBranch`/`collapsedTargetRows`/`groupMemberRows` deleted; ~45 existing goldens
updated (`go build` clean, 2 tests still red). Remaining: rewrite
`TestGroupMemberKeepsItsSummaryAndClipsTheTarget` (obsolete shared-summary-column
invariant) and `TestTranscriptClickTogglesTheBlock/a clipped target row expands the
block` (dead premise); add the item's new tests (row-painter table + `tool-leader` role
test in both schemes); CHANGELOG entry.

NOTES (2026-08-10): continuation — the item's overflow order needed one step the item's text does
not word, and the new painter table test caught it: when the target's budget is narrower than the
clip tail itself, `clipCells` returns a string WIDER than the budget it was given (`fitClipTail`
appends " …" whatever room is left), so the row overran its width by those cells and the viewport
would have folded it. `leaderRow` now drops the target outright in that case rather than painting a
lone " …" stub — the same order taken one step further, and the reading NOTES (d) above already
applies to the outcome slot. One existing golden moved with it (the width-11 wrapped-marker case in
`TestRenderMarksTheWholeBlockAndItsMarker`). The obsolete shared-summary-column helper
`summaryColumn` was deleted with the test that was its only caller.

**What:** Introduce the new row shape for single-block branch lines and same-label
group member rows: left `<tool-details>` (the target), flexing `⋯` leader, right-aligned
`<tool-top-level-details>` (the whole branch summary — typed stat, promoted line, or red
error text), then the `▶`/`▼` indicator. Add the `tool-leader` scheme role
(`internal/tui/colorscheme.go` + `theme.go`), dark and light values seeded equal to the
existing faint indicator tone. Implement the overflow order: dots flex to a floor of
1 `⋯`, then the left target truncates with `…`, the right slot never truncates
(design calls 3–4; promote-guard is item 2). The row painter lives in
`internal/tui/render.go`; the presenter (`toolpresent.go`) stays pure and its summary
content is unchanged in this item. Update existing golden strings in
`render_test.go` to the new shape.

**Tests:** table tests for the row painter: short/long target × short/long summary ×
narrow/wide width, asserting dot floor, left truncation, right slot integrity, and
error tone on `error: …` summaries; colorscheme test for the new role in both schemes.

**Acceptance:** `go test ./internal/tui/` green; `grep -n "tool-leader" internal/tui/colorscheme.go` finds the role.

**Commit:** `feat(tui): right-aligned outcome slot with dotted leader on tool rows`

## 2. Promote-guard and overflow edge cases — ✅ DONE (2026-08-10)

Depends on item 1.

NOTES (2026-08-10): three calls the item's text does not word, all deliberate.
(a) The guard is applied at the BLOCK's entrance (`guardPromotions`, called from `renderToolBlock`)
rather than inside `leaderRow`: demotion gives the call a body, which is the very question the
header indicator, the click surface and the `+N more lines` marker are answered from
(`blockHidesWhenCollapsed`) — a guard applied at the row would leave those three saying the block
hid nothing while the paint had just hidden a line. It still depends on the width alone, never on
the block's state, so a row does not change shape when it opens.
(b) The guard is scoped to promotions that OFFER a fallback: `promotedOutput` now takes the typed
stat beside the line, `outputDetail`'s one-line case passes `1 line`, and ask_user's answer passes
`""` and is never demoted — its body is the RECORD of the exchange (`askUserAnswerRecord`), which
the answer would be repeated above rather than folded into, and the spec's wording is "a one-line
output". A targetless call is likewise never demoted: it paints no branch row, so there is no
target for the guard to protect.
(c) `toolView.stat` was added to the wire (`wireToolView.Stat`, additive within
`transcriptVersion`), which item 2 does not name. Decode never re-runs a presenter, so a record
that came back without its stat could no longer be demoted and a resumed session would paint a
different shape at exactly the widths the guard exists for — the round trip's own invariant. The
codec's structural member-list guard test was updated with it.

**What:** Apply the promote-guard in `internal/tui/toolpresent.go`: a one-line output
is promoted into the right slot only when the row keeps ≥ 15 cells of target plus
1 dot at the current width; otherwise it remains a body line and the summary falls back
to the typed stat (e.g. `1 line`). Promotion therefore becomes width-aware at paint
time — keep the presenter pure by having it carry both candidates (promoted line +
typed fallback) and letting `render.go` choose by measure; pick the mechanism that
keeps `toolpresent.go` lipgloss-free.

**Tests:** guard boundary tests (exactly at/below the 15-cell threshold), a monster
one-line output staying a body, quoted-line respell-protection unchanged
(`toolpresent_test.go`, `render_test.go`).

**Acceptance:** `go test ./internal/tui/ -run 'Present|Render'` green.

**Commit:** `feat(tui): width-aware promote-guard for one-line outputs`

## 3. Per-tool registry conformance to the ratified table — ✅ DONE (2026-08-11)

Depends on item 1.

NOTES (2026-08-10): label CASING follows the item's own enumeration (Title Case: `Find Files`,
`Git Status`, `Diff Preview`, `Ask User`, `Sub-Agent`) rather than the table's sentence case
(`Find files`, `Git status`, …). The spec contradicts itself here — its Rules section writes the
sub-agent group header `✦ Sub-Agent (N)`, which item 7 also uses — and Title Case is what every
other label in the app already reads as. The RENAMES themselves are the table's, unchanged.

NOTES (2026-08-10): `git_diff_range`'s target stays `base...head` (three dots) where the table
writes `base..head`. The tool actually runs `git diff base...head`, so the two-dot spelling would
misstate which diff was taken; the table's cell is read as notation for "the two refs joined",
not as a change of git semantics.

NOTES (2026-08-10): five stats are worded off a HEADER the tool writes into its own output —
`run_tests` (`PASS`/`FAIL`), `find_files` (`N files`), `git_status` (`N changed`), `git_log`
(`N commits`), `git_commit` (short hash), `git_diff_range` (`+A −R`). `toolpresent.go`'s opening
note argues against re-deriving facts from prose, but design call 14 rules out growing the engine
for presentation, so prose is the only source left. Each derivation is anchored on a token the tool
formats deliberately and each is TOTAL: an unrecognised shape returns false and the tool's own
first line stays in the slot, so a wording change in `internal/tools` degrades such a card to what
it showed before rather than to something untrue. The table's unavailable halves degrade the same
way: no duration anywhere, no `M files` beside grep's hits, no `size` beside an HTTP status, no
`N steps` beside a delegation's `done` (design call 14).

NOTES (2026-08-11): correction to the note above — `git_commit` is the one of those six that is NOT
read off a header the tool writes. On success `git_commit` discards git's `[branch hash] subject`
output and returns `git log -1 --oneline` instead (`internal/tools/git.go`), so the slot is read off
git's own oneline (`^a1b2c3d subject`); the bracketed shape is kept as a second alternative because
the tool still falls back to it when the summary command fails. Both shapes stay TOTAL in the same
way: any other content returns false and keeps the prose floor.

NOTES (2026-08-10): three calls the item's text does not word.
(a) `ask_user` gets NO stat hook, so its slot keeps the human's answer rather than reading
`answered`/`pending`. Item 2 ratified that promotion as never-demotable because the block's body is
the RECORD of the exchange — the answer would be repeated above it, or duplicated against a ticked
choice — and design call 3 lets a promoted line be the whole summary. The table's cell is the one
deviation of substance here.
(b) The stat column is TWO hooks, not one: `stat` reads the result, `argStat` reads the call's own
arguments and is settled at presentation time (write_file, the three edit tools). Handing the
arguments to a result-time hook instead would have meant retaining a write's whole file content on
the view for the life of the session — the thing `toolView.args` exists to avoid — and the argument
half is knowable before the result lands anyway, so a write's `3 lines` now shows from the moment
the call is announced.
(c) `open_file`'s locate report moved from the slot to the block's BODY (a new result-shaped `body`
hook, which view_diff now shares). The table gives the slot to `N lines` and the term to the target;
without this the located line numbers would have reached the screen nowhere. `summaryLine` and
`openedFileLine` are gone with it — every typed summary is now worded by its own tool's hook.

**What:** Bring the presenter registry (`internal/tui/toolpresent.go`) to the ratified
"Display details per tool" table in `docs/layout/tool-layout.md`: new labels (Read,
Open, Write, Edit, Replace, Copy, Move, Delete, List, Find Files, Grep, Terminal,
Python, Tests, Git Status/Log/Branch/Commit/Diff, Diff Preview, Diagnostics, HTTP,
Fetch, Search, Present, Ask User, Sub-Agent), per-tool `<tool-details>` (left) and
`<tool-top-level-details>` (right) wording per the table. Grouping keys on the label,
so `edit_existing_file` (Edit) stops co-grouping with the find-replace pair (Replace) —
ratified. Durations (`· 1.2s`) only where the result already carries them; per the
2026-08-10 survey none do — render bare `exit 0`, `PASS`/`FAIL` (design call 14).
Update goldens.

**Tests:** presenter table tests updated per tool; a test pinning the Edit/Replace
grouping split; render goldens.

**Acceptance:** `go test ./internal/tui/` green.

**Commit:** `feat(tui): per-tool details and labels per ratified tool-layout table`

## 4. Click-anywhere toggle and the `see less…` footer

Depends on item 1.

**What:** Every row a block paints becomes a toggle target for its own block: extend
`lineTargets` (`internal/tui/render.go`) so body rows and the new right-aligned
`see less…` footer row (painted with the existing `promptToggle`-class treatment at the
expanded body's end) map to the owning entry, and extend `toggleBlockAt`
(`internal/tui/mouse.go`) to handle them — press+release without movement toggles,
any drag stays selection+copy (design call 6). Deepest element wins: a group member's
body row toggles that member, not the group.

**Tests:** mouse tests: click on body row collapses, click on footer collapses, drag
across a body still selects and copies, member-row click inside a group toggles the
member only.

**Acceptance:** `go test ./internal/tui/ -run 'Mouse|Toggle|Render'` green.

**Commit:** `feat(tui): click-anywhere toggle and see-less footer on tool blocks`

## 5. Super-group modeling in the transcript

**What:** Model the umbrella in `internal/tui/transcript.go`: 2+ adjacent same-depth
runs of different labels fold under one super-group entry (a lone call counts as a run
of 1); breakers = any non-tool entry between calls and any sub-agent block (design
call 1). Formation is live: the umbrella exists as soon as the second different-label
run starts, and grows as runs append (design call 2). Expansion state per level —
umbrella children (type rows) and members keep independent expanded flags on their
entries, so state survives scrolling and appends. No rendering in this item. Respect
ADR 0011 (no no-copy types by value).

**Tests:** transcript table tests: formation at 2 runs, run-of-1 membership, breaker
entries splitting umbrellas, sub-agent block splitting, live growth (append call →
same umbrella), state retention across appends.

**Acceptance:** `go test ./internal/tui/ -run Transcript` green.

**Commit:** `feat(tui): model mixed-type tool super-groups in the transcript`

## 6. Super-group rendering and umbrella interaction

Depends on items 4 and 5.

**What:** Paint the umbrella (`internal/tui/render.go`): header `✦ Tools (N calls)` —
N = total calls, count in the faint tone (design call 9); one type row per run in time
order (`<tool-type-header> (<group-count>)` left, aggregate right); expanding a type
row reveals member rows in item 1's shape; expanding a member opens its body (the
sketch's 2nd step). Type-row aggregate comes from a pure per-label aggregation seam in
`toolpresent.go`: red `N errors` when any member failed, else the natural sum where
the label's stat sums (lines, `+A −R`, hits · files, entries, changes), else blank
(design call 10). Mouse: umbrella header click closes all open children (its floor is
the type rows); type-row and member clicks toggle their own level via item 4's
deepest-wins targets.

**Tests:** render goldens for the three sketch states (collapsed / 1st step / 2nd
step); aggregate tests (errors-first, summable, blank); mouse tests for
umbrella-close-all and per-level toggling; live state: running last row wears the
spinner star.

**Acceptance:** `go test ./internal/tui/` green.

**Commit:** `feat(tui): render tool super-groups with aggregated type rows`

## 7. Sub-agent same-label grouping

Depends on items 5 and 6.

**What:** Let adjacent sub-agent blocks group with each other as `✦ Sub-Agent (N)`
(`transcript.go` lifts the standalone rule for sub-agent–sub-agent adjacency only;
they still break super-groups — design call 12). Member row: the agent's name left
(task head fallback), stat right — `done`/`failed`, plus step count only if the result
already exposes one (per the 2026-08-10 survey it does not — render `done`/`failed`
alone, design call 14). Expanding a member opens that sub-agent's span with its
existing depth rails (`render.go`).

**Tests:** transcript tests (sub-agents group together, never join umbrellas); render
goldens for a fan-out of 3 collapsed/one-expanded; mouse toggle on a member.

**Acceptance:** `go test ./internal/tui/` green.

**Commit:** `feat(tui): group adjacent sub-agent calls with expandable spans`

## 8. Keyboard block cursor

Depends on items 4 and 6.

**What:** A modal block cursor in a new `internal/tui/blockcursor.go` (keep
`model.go` from growing — it is a flagged split candidate): `alt+up`/`alt+down`
enters transcript-nav mode and moves a highlight across exactly the toggle targets
the mouse has, at the deepest visible level; inside the mode plain `up`/`down` move,
`enter` toggles, `esc` or any printable key exits to the prompt (a printable key also
lands in the prompt, not swallowed). Highlight paints the targeted row with the
existing selection-bar style (design call 7). Cursor state lives on the Model by
value (ADR 0011 — plain ints/bools only). Viewport follows the cursor
(`refreshViewportAnchored`).

**Tests:** key-sequence tests: enter mode, walk across a single block, a group, an
umbrella (deepest-visible order), toggle with enter, exit via esc and via a printable
key reaching the prompt; highlight golden for one row.

**Acceptance:** `go test ./internal/tui/ -run 'Cursor|Key'` green; `make check` green.

**Commit:** `feat(tui): modal keyboard block cursor for tool blocks`

## 9. Docs: layout.md points at the canon spec

Depends on items 1–8.

**What:** Shrink `layout.md`'s tool-block sections ("The rules behind the tool-call
sketch", the tool parts of "Collapsed and expanded blocks", and any other prose the
new shape contradicts) to short summaries pointing at `docs/layout/tool-layout.md`;
global grammar (width, colors, path shortening, body quoting) stays in `layout.md`
(design call 16). Sweep `internal/tui` doc comments that cite the rewritten
`layout.md` section names and retarget them. Update `docs/layout/tool-layout.md`'s
status line from ratified-spec to implemented. Remove the "tool-layout.md sketch =
unimplemented design" flag from `ISSUES.md` if present.

**Tests:** none (docs). `make check` as regression backstop.

**Acceptance:** `grep -n "tool-layout.md" layout.md` shows the pointer;
`make check` green.

**Commit:** `docs(layout): tool-layout.md becomes canon; layout.md points to it`

---

**Suggested version bump:** minor (0.12.0 → 0.13.0) once executed — a visible,
user-facing TUI feature set. Not performed by this plan; owner's call.
