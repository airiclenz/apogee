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

## 3. Add the green `success` scheme role — ✅ DONE (2026-08-11)

NOTES (2026-08-11): three additions beyond the item's literal text, all of them
the `tool-marker-bright` precedent (`8d96941`) rather than new ground: the role
sits beside `error` in the semantic block (it is that role's counterpart, not a
chrome accent); `CONTEXT.md`'s stated role count went 27 → 28; and the
colour-agnostic guard `TestBuiltinSchemesKeepSuccessAndErrorDistinct` joins the
existing pair guards, so a shipped scheme cannot say "it came off" and "it did
not" in one voice. The `theme.go` style that paints the `✓` is deliberately
NOT wired here — item 4 owns it and is its only consumer, so wiring it now
would land a style nothing reads.

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

## 4. Expanded grouped member: `┌─┶` header, column-0 gold rail, `┊` closer, done-`✓` — ✅ DONE (2026-08-11)

NOTES (2026-08-11): four deviations, all forced by where the seams already sit.
(a) The `┊` closer lands at the shared block-JOIN seam (`railJoin`, wrap.go) and
not inside the group painter: an open member's span is painted by
`renderView`'s own walk, outside the group block, so no other seam can emit a
line after it — and the sketch puts the closer where the blank separator would
be, not beside it. Consequence: a LONE expanded run already closes with `┊`
too; item 5's remaining work there is its `┌─┶` header and deleting the `⤷`
constants. (b) The closer fires only at a join, never at the end of the
transcript — a still-streaming run must not be marked closed (item 7). (c) The
done `✓` rides a new paint-time `toolView.finished` field rather than a flag
threaded through the shared `renderGroupMember`, so ONE reading serves the
collapsed row, the `┌─┶` header and item 5's lone run; it is not on the wire.
(d) Removing the `⤷` descent label re-pinned tests beyond the item's named
files: `model_test.go`, `transcriptcodec_test.go`, and
`TestTranscriptDepthLabelsEachLevel` → `TestTranscriptDepthFramesEachLevel`
(the rule it named is gone); `doc.go`'s rail narration was updated with them.

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

## 5. Lone sub-agent runs adopt the same shape; delete the `⤷` label — ✅ DONE (2026-08-11)

NOTES (2026-08-11): four deviations. (a) `subAgentLabel` was NOT deleted outright: the status
line's phrase for an unnamed delegate ("sub-agent · responding", `activity.go`) is a second
consumer that has nothing to do with the transcript label, so the constant was deleted from
`theme.go` and re-declared beside its only remaining reader as `subAgentActivityName` — deleting
it whole would have silently taken the word off the status line. (b) The lone run's `┌─┶` reaches
the single-block painter through a new `blockState.marker` (read via `branchMarkerIn`) rather than
by `renderSubAgentRun` composing a header and a branch of its own: a second copy of
`renderToolBlock`'s toggle/label/star logic is exactly how the lone and grouped shapes would come
to disagree. (c) The live-preview label emission (`render.go` `paintPreview`) is the "remaining ⤷
emission site" this item names, so it went here and re-pinned
`TestSubAgentStreamPreviewRailedWhenRunExpanded`; the live `┌─┶` header itself is still item 7's.
Its removal also left `prevDepth` unread in `renderView`, and it was dropped with it. (d) The
done-`✓` on a lone COLLAPSED row re-pinned goldens beyond the item's named files:
`TestSubAgentSummaryTempi`, `TestSubAgentCountIsTransitive`,
`TestNestedSubAgentRunStaysCollapsedInsideAnExpandedParent`,
`TestSubAgentRunCollapsesToItsCallBlock` and `TestParentMessageKeepsTheDelegatesStreamInsideItsRun`
(`transcript_test.go`), plus the block-mark case in `render_test.go` and the `⤷`-absence
assertions in `model_test.go`/`transcript_test.go`, which the acceptance's grep now guards instead.

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

## 6. Prompt body: markdown-rendered task text inside the rail — ✅ DONE (2026-08-11)

NOTES (2026-08-11): three deviations. (a) Only the OPENING blank rail line is emitted here — the
closing one is the block separator the span's first block already brings with it, railed at the
span's depth (`railJoin`, wrap.go). The painted shape is the sketch's exactly; emitting a second
blank row would open a two-row gap under the prompt. (b) The prompt is appended after the head's
OWN rows rather than spliced under the header row, so a delegation whose report was long enough to
lay out as a body (`Found 4 gaps…` + see-less footer) shows that body above the prompt. The item's
words are "the expanded SPAN's body opens with the task text", and it does — the prompt is the
first thing inside the rail; moving the head's report to the end of the span is a different change
and no item owns it. (c) Re-pinning reached beyond `render_test.go`: the expanded-run goldens in
`transcript_test.go` (`TestSubAgentRunCollapsesToItsCallBlock`,
`TestNestedSubAgentRunStaysCollapsedInsideAnExpandedParent`,
`TestParentMessageKeepsTheDelegatesStreamInsideItsRun`) and the marks case in
`TestRenderMarksTheWholeBlock` all paint an open delegation. `doc.go`'s rail narration gained the
prompt with them.

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

## 7. Live view wears the new shape — ✅ DONE (2026-08-11)

NOTES (2026-08-11): three deviations. (a) The live-preview path the item names (`render.go`
`paintPreview`) needed no change at all — item 5 already took the `⤷` off it and the preview has
always been railed at the depth that filled it. What was missing was the HEAD's frame: `subAgentSpan`
answers 0 for a delegate whose words are still in the streaming buffer, so `renderView` routed an
open expanded delegation to the ordinary tool-block branch and the frame snapped open only once the
first entry committed. The gate is now one predicate, `subAgentFramed` (`subagentblock.go`), read by
`renderView`'s run branch and by `renderSubAgentGroup`'s `spanned` — the grouped shape had the same
gap for the same reason, and two wordings of "is this delegation framed" is exactly how the lone and
grouped live shapes would come to disagree. (b) The `TestSubAgentStream*` family gained two cases
beyond the re-pinned preview golden: an open GROUP member's live frame, and the settling claim
itself — the live paint and the committed paint compared byte for byte, which is the half of design
call 4 no single golden can state. (c) `doc.go`'s rail narration already claimed the frame opened on
the live path; it now says what opens it, since that sentence was aspirational until this item.

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

## 8. Docs closeout — ✅ DONE (2026-08-11)

NOTES (2026-08-11): three deviations. (a) `layout.md` needed more than the verification the item
asks for: its top-of-file sketch and two prose paragraphs still drew and named the deleted
`⤷ sub-agent` label, so they were re-drawn to the shipped shape (`┌─┶` header, the prompt inside
the rail, the `┊` standing in the separator's place) under the item's own "fix only if stale".
(b) Two `[Unreleased]` CHANGELOG lines carried the same staleness — "railed and labelled exactly as
before" and a `⤷-railed shape` — and were corrected alongside the new entry rather than left
contradicting it; released sections were not touched. (c) The spec section carries an uncommitted
bullet (owner, working tree) suppressing the `┊` after a group's LAST expanded member; that is
item 4's territory and is not implemented, so the "As implemented" note states what the join seam
actually draws and the gap is reported as a follow-up rather than coded here.

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
