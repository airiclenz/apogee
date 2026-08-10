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

## 1. `tool-leader` role and the right-aligned outcome row

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

## 2. Promote-guard and overflow edge cases

Depends on item 1.

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

## 3. Per-tool registry conformance to the ratified table

Depends on item 1.

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
