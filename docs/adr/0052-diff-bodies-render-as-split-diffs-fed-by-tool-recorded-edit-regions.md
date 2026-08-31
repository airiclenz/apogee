---
Status: accepted
---

# Diff bodies render as split diffs fed by tool-recorded Edit regions

## Context

`IDEAS.md` carried "better UI for diffs": the expanded body of an edit block is a flat `-`/`+`
list today, and the owner — red-green weak — reads its green additions against red removals as
near-neighbours. The sketch (`docs/layout/siff-layout.md`, retired into
`docs/layout/split-diff-layout.md`) asked for a two-pane before/after reading with line numbers,
falling back to a vertical list where the terminal is narrow.

Two facts about the codebase bound the design. First, an edit block's body is **derived from the
call's arguments alone** (`changedLines`, `internal/tui/diffbody.go`): no file is read, no
token is spent — and therefore no line numbers and no unchanged context exist anywhere in the
view. Second, of the five diff-bodied blocks (`edit_existing_file`, `single_find_and_replace`,
`multi_find_and_replace`, `view_diff`, `git_diff_range`), only the last two carry position data a
renderer could recover: `view_diff`'s body is a whole-file diff countable from line 1, and
`git_diff_range` emits git's `@@` headers. The three edit tools return prose only.

The owner resolved the open questions on 2026-08-19 by question round; this record ratifies
them. It supersedes nothing, but it does **revise one documented stance**: `changedLines`'s
"derived from the arguments and goes nowhere near the wire" stays true of the *fallback*
rendering, while the primary rendering now consumes data the tool records.

## Decision

**Every diff-bodied block renders one body in two width-decided readings — a two-pane Split
diff, or a Stacked diff where panes cannot breathe — fed, for the edit tools, by Edit regions
the tool records at apply time.** Terms in `CONTEXT.md` (Split diff / Stacked diff, Edit
regions); row-level layout in `docs/layout/split-diff-layout.md`.

**1 — The edit tools record Edit regions at apply time, as a Tool summary variant.** The tool
holds both sides of the change in hand at the moment it applies — the renderer never does, and a
paint that re-read the file would race the very next edit and breach the thin-renderer rule
([ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)). Each region carries
its before/after start lines, the removed and inserted lines, and up to **three** unchanged
context lines each side, counted from the applied operations themselves — the same
counted-not-reparsed rule `unifiedLineDiff`'s diffstat already follows. Neighbouring changes stay
SEPARATE regions whose context ranges TILE the lines between them without overlap — the earlier
region takes up to three of them as its trailing context, the later takes whatever is left as its
leading — and the renderers omit the `⋯` separator between regions that are contiguous in line
numbering, so the paint reads exactly as a merge would. The variant is a new sealed
`domain.ToolSummary`, so the Tool summary contract is untouched: display data, never sent to the
model, and with no wire form of its own — the session record carries `domain.Message`'s `Content`
alone and no `ToolSummary` value ever reaches disk
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)); what the TUI's
transcript codec mirrors of the regions for a width-dependent re-paint is decision 5's additive
field on the codec's own wire type, not persistence of the summary. The edit blocks'
`+A −R` slot reads the same summary instead of re-deriving from arguments.

*Amendment (2026-08-31) — `write_file` is a fourth recorder.* Decision 1 named the three edit
tools because they were the whole of the group on the day it was written. `write_file` now
records Edit regions too, cut the same way from the file it read before the write against the
content it wrote (`okEditRegions`; a target with no readable before side records the whole
content as one pure insertion, `okInsertedRegion`). The recorders are therefore FOUR —
`write_file`, `edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace` — and
`domain.WroteBytes`, the byte count `write_file` used to attach instead, is retired with no
producer left. Nothing else in this decision moves: the same one builder cuts the regions, the
same "no regions ⇒ no summary" floor applies, and `write_file`'s outcome slot keeps stating the
line count its own REQUEST holds rather than reading the regions back.

**2 — The other two diff blocks change no tool: the renderer recovers their positions.**
`view_diff`'s body is a whole-file diff, so its line numbers are counted walking the tagged
lines from 1; `git_diff_range`'s come from parsing the `@@` headers git already emits. Both are
then trimmed to the same three-context regions the edit tools record — which ends expanded
`view_diff` painting the entire file as context, a deliberate behaviour change ratified here.

**3 — Width decides the reading, per pane, at paint time.** A Split diff paints only when each
pane can give the code at least **40 columns** after its number gutter and marker — a named
constant about readable panes, not a magic terminal width; anything narrower paints the Stacked
diff, which shows the very same regions, numbers and context vertically. Composition happens at
paint time against the one width authority
([ADR 0030](0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md)), so a resize
re-flows wrapped lines and can flip the reading; nothing is composed into strings ahead of the
paint.

**4 — Color never carries a change alone.** Each pane keeps its `-`/`+` marker column, so the
change survives a monochrome pipe, a copy-paste, and any palette. The shipped schemes move
`diff-add` from green to turquoise (dark: a bright turquoise; light: a dark teal that carries on
white) with `diff-del` staying red — the owner's red-green weakness is the reason and the
accessibility rule is general. `success` moves into the same turquoise family, kept a visible
step from `diff-add` as its scheme comments already require, so the whole UI speaks one pairing:
turquoise for came-in/came-off, red for went-out/failed. Gutters and the pane divider wear the
existing `muted` role; no new scheme keys
([ADR 0040](0040-color-schemes-are-embedded-roles-with-user-shadowing.md)).

**5 — Persistence is additive on the transcript codec, and the stacked rows remain what
`Details` carries.** The codec persists rendered rows (`session.DetailLine`, kind integers pinned);
the region structure a re-flow needs travels in a **new additive field** beside `Details`, while
`Details` itself keeps the stacked rows. An older build replays the stacked body unchanged; a
record written before this decodes with no regions and paints as it always did.

**6 — No summary means the old rendering, exactly.** A result carrying no Edit regions — an
embedder's tool ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)),
a malformed call, a pre-upgrade record — renders the argument-derived `-`/`+` list as before.
The split diff is an enhancement over recorded data, never a prerequisite.

### Rejected alternatives

- **Renderer-only positions.** `view_diff` and `git_diff_range` would get numbers and the three
  edit tools would not — the sketch unfulfilled precisely on the blocks the agent uses most, and
  two body shapes alive in one transcript.
- **The renderer reads the file at paint time.** Races the next edit (the file on disk has
  moved on by the time the block repaints), and puts filesystem reads into a thin renderer
  (ADR 0011).
- **Regions in the prose `Content`.** Spends tokens on display data the model has no use for,
  and a Mechanism rewriting `Content` on `PostToolResult` would invalidate what the words claim
  — the exact failure the Tool summary contract exists to avoid.
- **Persisting the summary in the session record.** Reopens ADR 0022's contract for a rendering
  concern the transcript codec already owns; the codec's additive-field rule covers it without
  touching the session schema.

## Consequences

- Expanded `view_diff` stops painting the whole file: same regions as every other diff block.
  Anyone wanting the full content has `read_file`.
- The three edit tools compute a little display data per apply; headless and bench hosts read
  or ignore it freely, and `CONTEXT.md`'s Tool summary count moves from six to nine.
- The scheme comments that pitch `success` relative to a green `diff-add` are reworded with the
  turquoise family.
- The stacked rows stay the codec's `Details` payload, so scrollback replayed on an older build
  keeps its shape — the same additive rule every codec field change has followed.

## Amendment (2026-08-19) — the change is a background band, not coloured text

Decision 4 above ("Color never carries a change alone") stands in full, and this amendment moves
the surface it is painted on. A diff body line no longer carries `diff-add` / `diff-del` as its
TEXT colour. It carries a **background band** — the new `diff-add-bg` / `diff-del-bg` roles,
turquoise and red out of the same two families — while its text wears the plain detail tone of
its block's state, exactly like every other detail line (`detailStyle`, `internal/tui/toolbranch.go`).
The band is the same in both states: the tone step an opened block takes says how loudly the block
is speaking, the band says which way the line went, and after this they no longer share a channel.

The contract, in four parts:

- **The band carries the signal; the text does not.** Diff-line text is `detailTone`'s collapsed
  `muted` or expanded `muted-bright`, the same as context lines and plain detail.
- **The band is full width.** It runs from the marker column to the pane edge in the Split diff and
  to the wrap rail in the Stacked and flat readings — under a short line's trailing space and under
  a wrapped continuation row's text alike — so a region reads as a block rather than as a ragged run
  of tinted words.
- **The gutter stays chrome.** Line numbers, the gutter columns and a continuation row's
  gutter-width leading spaces stay `muted` and OUTSIDE the band.
- **The marker is a glyph signal on the band.** The implementing plan's ratified calls 6 and 7
  (`docs/plans/archived/2026-08-19 - 03 - split-diff-display-plan.md`) put the `-`/`+` marker in the
  TEXT's colour so it would travel with the line's meaning. That rationale is **superseded**: the
  marker sits inside the banded run and reads against it, and decision 4's palette-proof guarantee
  now rests on the glyph plus the band rather than on the glyph plus a foreground.

Two new scheme keys land with it (`diff-add-bg`, `diff-del-bg`), additive in ADR 0040's sense — an
omitted key keeps the built-in default, so no user scheme file broke. The `diff-add` / `diff-del`
foreground roles are unchanged and keep their vocabulary place; the bands are their quiet
counterparts, not their replacements.

## Amendment (2026-08-20) — the diff bodies moved out of `toolpresent.go`

The Context above pointed `changedLines` at `internal/tui/toolpresent.go`. That file has since
been split along its four seams and deleted (ADR 0043;
`docs/plans/2026-08-19 - 04 - tui-architecture-deepening-plan.md`, items 5–8): the whole diff-body
cluster — `changedLines` over the edit tools' arguments, the `view_diff` and `git_diff_range`
region recovery, and the stacked rows — now lives in `internal/tui/diffbody.go`, beside
`splitdiff.go`, which composes the Split reading decision 3 picks by width. The pointer in Context
is updated to match. Pure file moves: no decision recorded here changes.

## Amendment (2026-08-22) — neighbouring regions TILE their context; they do not merge

Decision 1 above called a region's bracketing context **merged** — up to three unchanged lines each
side, folded together with a neighbour's. The implementing plan's ratified call 3 was written that
way and the owner superseded it on 2026-08-19, before the shared region builder was written
(`docs/plans/archived/2026-08-19 - 03 - split-diff-display-plan.md`, item 2 NOTES): neighbours
within the gap stay SEPARATE regions whose context ranges TILE the interior lines without overlap —
the earlier region takes up to three of them as its `Trailing`, the later takes the remainder as its
`Leading`. No line is context for two regions at once, `Removed`/`Inserted` stay changed-only so
`EditRegions.Stat()` matches `unifiedLineDiff` exactly, and a gap of at most six lines always splits
cleanly (nothing branches on the gap width any more — the tiling falls out of the context rule
alone). What the merge was wanted for is delivered at the paint instead: both readings omit the `⋯`
separator between regions that are contiguous in line numbering, so a small gap reads end to end and
the painted result is identical to a merge. Decision 1's text above is amended to match; no other
decision recorded here changes.

## Amendment (2026-08-24) — the summary has no wire form; the codec's region mirror is decision 5's

Decision 1 above said the Edit regions variant is "display data, never sent to the model, never
persisted in the session record", and `internal/domain/toolsummary.go` said the same in two places:
the `NEVER PERSISTED` paragraph of the Tool summary block comment (on `ToolSummary`) and the
`EditRegions` doc comment. Read literally, that contradicted **decision 5**, which ratifies the
region structure travelling in an additive field on the transcript codec's wire — and
`internal/session/transcript.go` performs exactly that (`session.EditRegion`, projected from the
TUI's cards by `toWireToolView` / `fromWireToolView` in `internal/tui/transcriptbridge.go`).
*(File amended 2026-08-31: the codec moved out of `internal/tui/transcriptcodec.go` when it became
Driver-neutral; the contract below is unchanged.)*

The rule, stated once so both halves are true:

- **The summary VALUE never reaches disk and has no wire form.** `domain.Message` carries `Content`
  only, the codec stores the RENDERED tool view rather than the `ToolResult`, and nothing on the
  replay path re-runs a presenter or reconstructs a `ToolSummary` — so a session written before a
  variant existed reopens unchanged and ADR 0022's contract is untouched. This half was always
  load-bearing and stays.
- **The transcript codec MAY mirror the FACTS a variant carries onto a wire type of its own**,
  where a replayed view cannot be re-composed without them. The neutral codec in `internal/session`
  does that for the regions, because the Split reading is composed at paint time against the live
  width and has nothing to compose from once the regions are gone. That mirror is the codec's own
  additive contract (decision 5, the `session.Entry` omitempty rule), not persistence of
  `domain.EditRegions`. *(Owner amended 2026-08-31 — it read "a host's codec" while the codec lived
  in the TUI; the permission and its bound are unchanged.)*

The two Go comments are rewritten to that wording; decision 5, the codec and its round-trip test are
untouched. The rejected alternative "Persisting the summary in the session record" stands as
written: it rejects putting the summary itself into the session schema, which is what the first
bullet keeps out.
