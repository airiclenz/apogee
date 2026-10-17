---
title: ADR 0052 decision
---

**Every diff-bodied block renders one body in two width-decided readings — a two-pane Split
diff, or a Stacked diff where panes cannot breathe — fed, for the edit tools, by Edit regions
the tool records at apply time.** Terms in `CONTEXT.md` (Split diff / Stacked diff, Edit
regions); row-level layout in `docs/layout/split-diff-layout.md`.

**1 — The edit tools record Edit regions at apply time, as a Tool summary variant.** The tool
holds both sides of the change in hand at the moment it applies — the renderer never does, and a
paint that re-read the file would race the very next edit and breach the thin-renderer rule
(ADR 0011). Each region carries
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
(ADR 0022); what the TUI's
transcript codec mirrors of the regions for a width-dependent re-paint is decision 5's additive
field on the codec's own wire type, not persistence of the summary. The edit blocks'
`+A −R` slot reads the same summary instead of re-deriving from arguments.

> **Amended 2026-10-17 (ADR 0103, `adr-0052-amend-1017`).** "With no wire form of its own" no
> longer holds: the summary gains a wire mirror owned by `internal/eventjson`, decoded only for
> live watching. The session record still carries no `ToolSummary` value.

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
(ADR 0030), so a resize
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
(ADR 0040).

**5 — Persistence is additive on the transcript codec, and the stacked rows remain what
`Details` carries.** The codec persists rendered rows (`session.DetailLine`, kind integers pinned);
the region structure a re-flow needs travels in a **new additive field** beside `Details`, while
`Details` itself keeps the stacked rows. An older build replays the stacked body unchanged; a
record written before this decodes with no regions and paints as it always did.

**6 — No summary means the old rendering, exactly.** A result carrying no Edit regions — an
embedder's tool (ADR 0002),
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
