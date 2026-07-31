# Sub-agent indicator rail: continuous, run-scoped, orange

- **Goal:** the vertical sub-agent rail (`│`) on the left of nested transcript blocks is
  continuous through the blank spacer lines inside a sub-agent run, never bridges two
  distinct consecutive sub-agent calls, and is painted in the tool-header orange.
- **Date:** 2026-07-31
- **Status:** not started
- **Authoritative sources:**
  - The ISSUES.md entry this plan implements, quoted verbatim below (the entry itself is
    removed by item 3, so the quote here is the pinned requirement). If any item's prose
    disagrees with this quote, the quote wins:
    > the vertical sub-agent indicator line on the very left of the chat should be
    > continuous. Currently each new action (new tool, new response, ...) are separated
    > with an empty line. The sub-agent indicator line should be displayed even in these
    > spacer lines. A new sub agent tool call directly after a prior sub-agent tool call
    > should NOT be visually connected to the prior call.
    > I'd also like to have the sub agent line in orange (same color as tool headers)
  - The P3.14 sub-agent framing design: `internal/tui/doc.go` (P3.14 paragraph, ~lines
    35–40) — run boundaries are derived from each entry's `depth` inside
    `transcript.renderView`; the Model holds no new state (ADR 0011: render-only).
  - Rendering seams: `internal/tui/render.go` — `renderView`/`appendBlock` (~lines
    48–98, the `""` spacer), `renderSubAgentLabel` (~145), `railLines`/`railedWidth`
    (~575–602). Theme roles: `internal/tui/theme.go` — `subRail` (~99, ~148), the
    orange tone `colCode = #f0883e` (~31) that `toolLabel` carries (~97).
- **Standing requirements:** forward the `coding-standards` skill when executing this
  plan. Run `make check` before every commit (AGENTS.md).
- **Out of scope:**
  - The clickable/collapsible tool-call module (its own ISSUES.md entry; needs grilling).
  - Parallel or interleaved sub-agent event streams. Depth-derived run boundaries assume
    sequential nesting (doc.go P3.14); this plan preserves that assumption.
  - The streaming-preview block's depth (it renders at depth 0 today; unchanged).
  - The status-line activity phrase ("sub-agent · …", activity.go) — its styling is not
    the rail's.
  - Any version identifier change (see closing note).

## 1. Rail the spacer lines inside a sub-agent run

**What:** In `internal/tui/render.go`, `renderView`'s `appendBlock` closure (~line 56)
currently separates blocks with a bare `""` line, which visibly breaks the `│` rail
between a run's blocks. Give `appendBlock` the depth of the block being appended and
have it remember the previously appended block's depth in a closure variable (the loop's
`prevDepth` is per-entry, not per-appended-block — the ⤷ label blocks appended by the
descent loop at ~line 73 have their own depths). The spacer between two blocks is railed
at `join = min(previous block's depth, this block's depth)`: for `join == 0` it stays
the bare `""` (the flat depth-0 transcript renders byte-for-byte as today); for
`join > 0` it is the `th.subRail`-styled gutter of `join` rail glyphs with trailing
whitespace trimmed (a depth-1 spacer line's visible text is `│`, depth-2 `│ │` — never
a trailing space). Call sites pass: `d` for each ⤷ label block, `e.depth` for entry and
grouped-run blocks, `0` for the streaming preview block.

This min-rule alone yields every behavior the issue asks: spacers inside a run (label →
block, block → block) are railed, so the rail is continuous; a climb-out spacer
(depth 2 → 1) keeps the outer rails only; and two consecutive `sub_agent` calls are
never connected, because the second call's own tool-call block sits at the parent's
depth between the two runs — the join dips to the parent depth, the rail breaks, and
the existing depth-increase logic (~line 73) opens a fresh ⤷ label for the second run.
No change to that label logic, to `userBlocks`/`lastUserStart` bookkeeping (the spacer
is still exactly one physical line), or to any transcript state.

**Tests:** In `internal/tui/render_test.go` (reuse the `feed`/`readCall`/`renderPlain`
helpers and the golden style of `TestRenderGroupsInsideSubAgent`, ~line 161):
- Update `TestRenderGroupsInsideSubAgent`'s golden: the spacer between `│ ⤷ sub-agent`
  and `│ ✦ Read File` becomes `│`.
- New: two separate same-depth blocks inside one run (e.g. two different-label tool
  calls at depth 1) → the spacer between them is `│`.
- New: a depth-1 block followed by a depth-0 block → the spacer is bare `""` (the rail
  ends at the run's last line).
- New (the issue's core case): parent `sub_agent` tool call at depth 0, a depth-1 block,
  then a second `sub_agent` call at depth 0 and another depth-1 block → the spacer above
  the second call block is bare, and a second `⤷ sub-agent` label opens the second run.
- New: a 0→2 descent → the two stacked labels are joined by a `│` spacer (railed at
  depth 1); a 2→1 climb-out spacer is railed at depth 1.
- Regression: a depth-0-only transcript renders identically to before (existing goldens
  such as `TestRenderGroupsConsecutiveSameLabelCalls` already pin this; keep them green
  unmodified).

**Acceptance:** `go test ./internal/tui/ -count=1` green; `make check` green.

**Commit:** `fix(tui): keep the sub-agent rail continuous through spacer lines`

## 2. Paint the rail in the tool-header orange

**What:** In `internal/tui/theme.go` (~line 148) change the `subRail` role's foreground
from `colFaint` to `colCode` — the `#f0883e` orange the tool header's `toolLabel`
already carries (theme.go ~line 97) and the issue names as "same color as tool
headers". The ⤷ sub-agent label (`renderSubAgentLabel`, render.go ~line 145) shares the
`subRail` role and deliberately rides along to orange: one style role for the whole
sub-agent frame, coherent with the orange ✦ tool markers. Update the `subRail` comment
at theme.go ~line 99 ("dim" → the orange tone). Independent of item 1.

**Tests:** In `internal/tui/` tests, a style-presence guard patterned on
`TestToolHeaderLabelStyled` (render_test.go ~line 95): assert
`th.subRail.Render("│")` equals `lipgloss.NewStyle().Foreground(colCode).Render("│")`,
and that it renders escape sequences at all (a no-op role would leave the rail
unstyled) — loose equality against the theme's own render, no lipgloss byte-goldens.

**Acceptance:** `go test ./internal/tui/ -count=1` green; `make check` green.

**Commit:** `feat(tui): paint the sub-agent rail and label in the tool-header orange`

## 3. Document the rail rule and close the issue

Depends on items 1 and 2. This is the single owning item for every cross-cutting prose
amendment — no other item edits these files.

**What:**
- `internal/tui/doc.go`, the P3.14 paragraph (~lines 35–40): extend it with the landed
  rule — inter-block spacers are railed at the min of the adjacent blocks' depths, so a
  run's frame is continuous and breaks exactly at run boundaries; the frame's tone is
  the tool-header orange (`colCode`).
- `layout.md`, the "**Blank lines.**" paragraph (~lines 114–116): amend "Exactly one
  empty line between blocks" with the sub-agent case — inside a sub-agent run the
  separating row carries the `│` rail gutter; it is still exactly one row.
- `CHANGELOG.md`, under `## [Unreleased]`: one entry (Fixed or Changed) describing the
  continuous orange sub-agent rail. Do not touch any release heading.
- `ISSUES.md`: remove the implemented sub-agent indicator entry (the two-line item
  quoted in this plan's header).

**Tests:** none (prose only).

**Acceptance:** `make check` green; `grep -n "rail" layout.md internal/tui/doc.go`
shows the amended prose; the quoted entry is gone from `ISSUES.md`; `git diff --stat`
for this item touches only the four files above.

**Commit:** `docs: spec the continuous orange sub-agent rail and close the ISSUES entry`

---

**Suggested version bump (not performed):** a patch-level release (v0.8.x) would cover
this — one user-visible rendering fix plus a cosmetic change, no API surface. Whether
and when to bump is the owner's call; no item in this plan changes VERSION, any release
heading, or tags.
