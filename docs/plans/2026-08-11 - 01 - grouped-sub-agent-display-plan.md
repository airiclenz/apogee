# Grouped sub-agent display — implementation plan

- **Goal:** implement the "Grouped Sub-agents" section of
  `docs/layout/tool-layout.md`: drop the `⤷ sub-agent` label, give expanded
  sub-agents a `┌─┶` header + column-0 gold rail + `┊` closer, open the
  body with the delegation's full prompt (markdown-rendered), then its tools,
  then its response, and mark finished sub-agents with a green `✓`.
- **Date:** 2026-08-11 · **Status:** saved, unexecuted
- **Sized for:** ~200k-context host
- **Authoritative source:** `docs/layout/tool-layout.md`, section
  "Grouped Sub-agents" incl. the finished-sub-agent example (committed to
  HEAD by item 1). If any item below disagrees with that section, the spec
  section wins.
- **Ratified design calls** (owner, 2026-08-11, plan-writing session):
  1. Execution is gated on plan `2026-08-11 - 00 - tool-slot-color-and-inline-marker-plan.md`
     being archived first — both plans touch the shared leader-row painter.
  2. Corner glyph is `┌` (owner revised the sketch from `╭`). Colour:
     **only** `┌`, `│`, `┊` take the tool-header gold (today's `th.subRail`
     style); `─` and `┶` render in the same tone as the other branch markers
     (`┝`/`┕`). No new scheme role for the rail.
  3. A lone (ungrouped) sub-agent gets the exact same expanded shape.
  4. Live view: the new shape renders live — `┌─┶` header wears the spinner
     star, streaming tokens inside the gold rail; settling changes nothing.
  5. The group header keeps its `(N)` count per the section's Rules bullet;
     the sketch omitted it informally.
  6. A **finished** sub-agent shows a `✓` after its name — collapsed and
     expanded, grouped and lone; the right slot still prints `done`. The `✓`
     is **green**, via a new scheme role (owner). A **failed** run gets no
     glyph — red right-slot text only, per the spec's failure-marking rule
     ("no glyph or header color changes"). Role name `success` (generic,
     reusable beyond sub-agents) is the plan author's call.
- **Standing requirements:** skills: coding-standards. Any authorized
  deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:** the tool-marker/`tool-marker-bright` slot-colour work
  (owned by plan `2026-08-11 - 00`); any engine/wire change (the task text
  already arrives in the tool-call args); VERSION/CHANGELOG release headings;
  new markdown features beyond the existing `renderMarkdownBody` pipeline.

## 1. Gate on the tool-slot-color plan and land the spec section — ✅ DONE (2026-08-11)

NOTES (2026-08-11): gate satisfied —
`docs/plans/archived/2026-08-11 - 00 - tool-slot-color-and-inline-marker-plan.md`
is present. `docs/layout/tool-layout.md` was already clean at HEAD (its
"Grouped Sub-agents" section incl. the finished-sub-agent `✓` example landed in
`dc748aa`), so no docs commit was needed for this item.

**What:** Verify
`docs/plans/archived/2026-08-11 - 00 - tool-slot-color-and-inline-marker-plan.md`
exists — if it does not, STOP and report BLOCKED (design call 1: the shared
row painter must settle first). Then ensure the "Grouped Sub-agents" section
of `docs/layout/tool-layout.md` (including the finished-sub-agent `✓`
example) is committed: if the file still carries uncommitted changes, commit
them as a docs-only commit; if already clean, record that in a NOTES line and
make no commit.

**Tests:** none (housekeeping item).

**Acceptance:**
- `ls "docs/plans/archived/" | grep -c "tool-slot-color"` → ≥ 1
- `git status --porcelain -- "docs/layout/tool-layout.md"` → empty
- `git show HEAD:"docs/layout/tool-layout.md" | grep -c "Grouped Sub-agents"` → ≥ 1

**Commit:** `docs(layout): land the grouped sub-agent layout spec`

## 2. Retain the full sub-agent task text and persist it — ✅ DONE (2026-08-11)

**What:** In `internal/tui/toolpresent.go` the `sub_agent` presenter drops
`tv.args` after present (`presentToolCall` keeps args only when an `outcome`
hook exists; `sub_agent` registers `detail: outputDetail` — see
`toolpresent.go:611-617`, `:695-697`). Retain the full `task` argument text on
the sub-agent head's `toolView` at present time (field name is implementer's
choice). Extend the transcript codec (`internal/tui/transcriptcodec.go`) so
the retained text survives session save/load. No engine change.

**Tests:** `toolpresent_test.go` — a multi-line `task` is retained verbatim on
the head's view; `transcriptcodec_test.go` — round-trip keeps it.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): retain the full sub-agent task text through present and codec`

## 3. Add the green `success` scheme role

Depends on item 1 (the scheme files are dirty from that plan's run until it
lands).

**What:** New scheme role `success` in `internal/scheme/scheme.go`, with green
values in both `internal/scheme/schemes/dark.yaml` and
`internal/scheme/schemes/light.yaml` (readable on each background). Wire it
exactly like the recent `tool-leader` role addition. Design call 6 is the
authority; the role's first consumer is item 4's `✓`.

**Tests:** extend the existing scheme/builtins tests so both builtin schemes
must define `success` (colour-agnostic, matching the style of `5555c44`).

**Acceptance:** `go test ./internal/scheme/` passes; `make check` passes.

**Commit:** `feat(scheme): add a green success role for done markers`

## 4. Expanded grouped member: `┌─┶` header, column-0 gold rail, `┊` closer, done-`✓`

Depends on items 1, 3.

**What:** Implement the spec sketches (expanded member + finished-sub-agent
example) for a `✦ Sub-Agent (N)` group, in `internal/tui/`:

- `theme.go` central glyph block: add the corner (`┌`), the header tee (`┶`),
  the closer (`┊`), and the done marker (`✓`) as named glyphs (precedent:
  leader-dot move, commit `01dda2a`).
- Expanded member header row: prefix `┌─┶ ` replaces the `  ┝`/`  ┕` indent
  (also when the expanded member is the last one); the row keeps its dotted
  leaders and `▼` and stays a toggle target for mouse and block cursor.
- The member's whole span renders behind a column-0 `│ ` rail; one lone `┊`
  line closes the span before the next member row.
- Colour per design call 2: `┌`, `│`, `┊` in tool-header gold; `─` and `┶` in
  the branch-marker tone of `┝`/`┕`.
- Done marker per design call 6: a finished member's row appends ` ✓` after
  the name, before the leaders, painted in the `success` role — on collapsed
  member rows and on the expanded `┌─┶` header alike. A running member shows
  no `✓` (spinner rules unchanged); a failed member shows no glyph — its red
  right-slot text is the only failure mark. The right slot keeps printing
  `done`/`failed` as today.
- Remove the `⤷ sub-agent` label emission from this (committed-run) path in
  `render.go` (~`:260-264`).
- Nested blocks keep their own indent inside the rail (the sketch's
  `│   ┝ …`); a deeper expanded sub-agent draws the same shape at its own
  left edge inside the parent's rail.
- Re-pin affected tests: `TestRenderSubAgentGroupSketchStates` (to the new
  sketches), `TestSubRailPaintedInToolHeaderGold`,
  `TestSubAgentReflowAtSmallWidths`, `TestSubAgentGroupMemberClickOpensItsSpan`
  (`mouse_test.go`), affected `transcript_test.go` cases.

Files: `theme.go`, `subagentblock.go`, `render.go`, `render_test.go`,
`mouse_test.go`, `transcript_test.go`.

**Tests:** the re-pinned tests above; the sketch test asserts `┌─┶`, the
column-0 `│`, the `┊` closer, the done-`✓` (present when finished, absent
while running and on failure), and that `⤷` no longer appears in group output.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): grouped sub-agent expands behind a column-0 gold rail`

## 5. Lone sub-agent runs adopt the same shape; delete the `⤷` label

Depends on item 4.

**What:** Per design call 3, an expanded lone run (`renderSubAgentRun`,
`internal/tui/subagentblock.go:135`) renders identically: `┌─┶` header,
column-0 gold rail, `┊` closer, and the done-`✓` rule from design call 6 on
its collapsed row and expanded header. Remove the remaining `⤷` emission
sites and delete `glyphSubLabel`, `subAgentLabel`, and `renderSubAgentLabel`
outright.

**Tests:** `render_test.go` — lone-run expanded shape and `✓` behaviour match
the group member's; no `⤷` remains in the package.

**Acceptance:** `go test ./internal/tui/` passes;
`grep -rc "⤷" internal/tui/` finds nothing; `make check` passes.

**Commit:** `feat(tui): lone sub-agent runs share the rail shape; drop the ⤷ label`

## 6. Prompt body: markdown-rendered task text inside the rail

Depends on items 2, 4, 5.

**What:** The expanded span's body opens with the retained task text (item 2),
rendered through the existing `renderMarkdownBody` pipeline
(`internal/tui/markdown.go`) and wrapped to the railed width, framed exactly
as the sketch: blank rail line, prompt, blank rail line, then the nested tool
blocks, then the sub-agent's response. Applies to lone and grouped runs alike.

**Tests:** `render_test.go` — a multi-line markdown task wraps and formats
behind the rail; the blank-line framing is pinned; a missing/empty task falls
back to no prompt block (no stray blank lines).

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): expanded sub-agents open with their markdown-rendered prompt`

## 7. Live view wears the new shape

Depends on items 4, 5.

**What:** Per design call 4, a still-running delegation renders the same
expanded shape live: `┌─┶` header wearing the existing spinner star, streaming
tokens inside the gold rail (live-preview path, `render.go` ~`:230-246`), no
`⤷`, no `✓` until finished. Settling into the committed run changes nothing
visually beyond the `✓` appearing.

**Tests:** the `TestSubAgentStream*` cases in `transcript_test.go` updated to
the new live shape.

**Acceptance:** `go test ./internal/tui/` passes; `make check` passes.

**Commit:** `feat(tui): live sub-agent streams inside the new rail shape`

## 8. Docs closeout

Depends on items 4–7.

**What:** `docs/layout/tool-layout.md` — record the Grouped Sub-agents section
as implemented in the file's "As implemented" header notes, matching the
existing style; `layout.md` — verify its sub-agent wording still defers to
`tool-layout.md` and fix only if stale; `CHANGELOG.md` — one `[Unreleased]`
entry for the sub-agent display change. No VERSION change (see closing note).

**Tests:** none.

**Acceptance:** `grep -n "Grouped Sub-agents" "docs/layout/tool-layout.md"`
shows the implemented note nearby; the `[Unreleased]` entry exists;
`make check` passes.

**Commit:** `docs(layout): grouped sub-agent display marked implemented`

---

**Suggested version bump:** micro (v0.12.0 → v0.12.1) once shipped — a
user-visible TUI feature per the house convention. The owner decides; no item
in this plan touches VERSION.
