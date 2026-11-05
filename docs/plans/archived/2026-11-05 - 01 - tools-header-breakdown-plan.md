# Tools umbrella header: per-tool-kind breakdown

**Goal:** The `✦ Tools` super-group header states how its calls divide by tool kind —
`✦ Tools (3 Read, 4 Terminal) ⋯⋯ ▼` — on a leader row whose dots flex to the block edge, open
and folded alike, and every doc that sketches the header shows that shape.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** f7ec72b6
**Closes:** tools-header-breakdown

**Regression check (2026-11-05, f7ec72b6):**
- 1: guard folded — cmd/apogee e2e pins and goldens move with item 1 (decision 1); marker-painting seam required; star-flip goal bound to `blockState.star`; `./internal/tui/` `-run` pattern extended; cmd/apogee run pattern set to `TestE2EToolsUmbrella|TestE2EToolsFoldOverZero|TestE2ESubAgentView` (decision 1, second round).
- 1: rejected — extend the `-run` pattern with `|DragOutlives` for the pins at mouse_test.go:4260-4275; those lines sit in `TestSmallUmbrellaHeaderFoldsItselfOnly` (already matched by `Umbrella|Fold`), and `TestTranscriptDragOutlivesASettingsHighlight` (mouse_test.go:3730) pins no header.
- 2: guard folded — cmd/ comments are item 2's (decision 2); Files gain the stale comment/prose sites; the rule grep widened to the prose shapes.

**Sources:**
- `.capcom/issues/tools-header-breakdown.md` (owner sketch + acceptance)
- `docs/layout/tool-layout.md` (Vocabulary, Rules, super-group sketches) — `layout.md` defers to it
- `internal/tui/toolblock.go` — `renderSuperGroup`; `internal/tui/toolleader.go` — `leaderRowIn`

**Ratified design calls** (owner, 2026-11-05):
- **Single-call kind:** numbered in the header — `1 Read`, never bare `Read`.
- **Overflow:** breakdown when it fits with ≥ 1 leader dot; otherwise the `(N calls)` total; below that the row clips like a type row. Header is always one line.
- **Kind order:** time order of each kind's first call.
- **Tone:** the whole parenthetical — names, numbers, commas — in the faint `toolIndicator` tone; `Tools` keeps `toolLabel` gold.

**Standing requirements:**
- skills: coding-standards
- Pi 5 box: run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<pattern>' ./internal/tui/` (one package, a `-run` pattern, never two runs at once); pass this to every sub-agent.
- Bubble Tea v2 (`charm.land/bubbletea/v2`); the `Model` is copied by value — no no-copy types held by value.

**Out of scope:**
- `✦ Sub-Agent (N)` and skill group headers (`renderSubAgentGroup`) — unchanged.
- Fold semantics (`targetUmbrella`, `ui.tools-open`, `ui.tools-fold-over`, small-umbrella flag) — paint only.
- Type rows and member rows — unchanged.
- Point-in-time records naming `✦ Tools (N calls)`: `CHANGELOG.md`, ADR notes and addenda, archived plans/reviews.

## 1. Paint the umbrella header as a breakdown leader row — ✅ DONE (2026-11-05)

NOTES (2026-11-05): leaderRowIn gained a marker-painter argument (func(theme, string, bool) string); the type rows and leaderRow pass paintRowMarker unchanged (output byte-identical), the umbrella header passes the new paintHeaderMarker (th.toolHeader). Its avail/budget arithmetic is now named (leaderAvail, leaderTargetBudget) so the header's breakdown-vs-total choice measures with the row's own numbers.
NOTES (2026-11-05): e2e matchers became an umbrellaHeaderMatch value (text + glyph, matched on one row with only dots/blanks between them) because the leader's dot run makes "header ▶" non-contiguous; the row is not required to END in the glyph, since the approval pane draws a ┃ past a run view's edge.
NOTES (2026-11-05): umbrellaHeaders (mouse_test.go), headerStar (blocktarget_test.go) and the folded-live subtest in toolblock_test.go now collapse the leader (collapseLeader) like renderPlain does, so their goldens read the shared umbrellaHeaderRow / blinkedUmbrellaHeaderRow helpers added beside leaderEdgeRow.
NOTES (2026-11-05): comments left for item 2 (in its Files): render_test.go's "✦ Tools (8 calls)" layout-golden comment, transcriptbridge_test.go:2297, toolshape_test.go:653, and the non-test comments in doc.go, transcript.go and toolregistry.go. The cmd/apogee comments tied to the changed constants and matchers were updated here, since item 1's text names them.
NOTES (2026-11-05): no CLOSES section: item 1 carries no **Closes:** line; the plan-level tools-header-breakdown is left for the plan's closeout once item 2's docs land.

**What:**
**Goal:** `renderSuperGroup` paints its header as ONE row: the block's star, `Tools` in `toolLabel`,
then `(<n> <Kind>, <n> <Kind>, …)` in `toolIndicator` — one entry per distinct run label, summed
across non-adjacent runs of that label, numbered even at 1, in order of first appearance — then a
`toolLeader` dot run (≥ 1 dot) to the `▶`/`▼` right-aligned in the same column as the type rows'
glyphs. Open (`▼`) and folded (`▶`) differ only in the glyph. When the breakdown cannot fit with
`leaderGap` + 1 dot, the parenthetical is `(N calls)` (`plural(calls, superCallNoun)`); when even
that cannot fit, the left content clips by the leader row's rule. The header stays marked
`targetUmbrella` in every state; a live block's star flips as today.
**Approach (assumed at the header base):** in `internal/tui/toolblock.go`, replace the
`hangingWrap(th, th.toolHeader, state.star(th)+" ", label, width)` header (both the folded early
return and the open path) with a leader row built through `leaderRowIn` — the existing overflow
arithmetic, no second copy — with the star as the row marker, no outcome slot (empty
`branchSummary`, `noRemainder`), `room := toolRowCells(th, width)`, finished by `indicatorRow` with
`stateIndicator(th, !fold.folded)`. The star keeps its own styling (`state.star(th)`), not
`paintRowMarker`'s detail tone — add a marker-painting seam to `leaderRowIn` only if needed, and
keep the type-row call sites' output byte-identical. Build the breakdown text in one small
function over `runs` (keyed on `views[0].Label`); pick breakdown vs total by measuring against
the room the leader row leaves. Rewrite `renderSuperGroup`'s doc comment and the
`superGroupLabel` const comment to the new shape; reword the `renderToolBlock` comment that cites
the umbrella's `"✦ Tools (7 calls)"` shape.
**Regression guard.** The cmd/apogee pins move with item 1 so no commit leaves cmd/apogee red — add cmd/apogee/e2e_toolsfold_test.go, cmd/apogee/e2e_toolsfold_subagent_test.go, cmd/apogee/testdata/frames/t19-tools-umbrella-folded.txt and cmd/apogee/testdata/frames/t18-run-view-finished.txt to Files (and e2e_subagent_view_test.go only if its own text pins the header); update their header constants, Find/click matchers, comments and golden frames to the new header row (regenerate goldens by the repo's own update mechanism, never by hand-typing dots); add to Acceptance `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EToolsUmbrella|TestE2EToolsFoldOverZero|TestE2ESubAgentView' ./cmd/apogee/` and widen the leftover-pin grep to `grep -rn 'Tools ([0-9]* calls)' internal/tui/*_test.go cmd/apogee/*_test.go cmd/apogee/testdata/frames`, which must print only fallback-width tests and comments item 2 owns.
The marker-painting seam is REQUIRED, not optional: `leaderRowIn` always paints its marker through
`paintRowMarker` → `detailTone` (toolbranch.go), while today's star is bare (`th.toolHeader` is
`lipgloss.NewStyle()`); give `leaderRowIn` a marker painter argument (not a pre-styled marker —
`paintRowMarker`'s comment rules out measuring escapes), type-row output byte-identical. The Goal's
"a live block's star flips as today" binds as: while any call is open the star alternates with a
blank cell holding its column (`blockState.star`). Extend the `./internal/tui/` `-run` pattern with
`|MarksTheWholeBlock|StarBlinks|LayoutGolden|AnsweredQuestionSolo` (blocktarget_test.go, render_test.go, transcriptbridge_test.go pins).
**Files:** internal/tui/toolblock.go, internal/tui/toolleader.go, internal/tui/toolblock_test.go, internal/tui/toolbranch_test.go, internal/tui/toolshape_test.go, internal/tui/transcriptbridge_test.go, internal/tui/blocktarget_test.go, internal/tui/mouse_test.go, internal/tui/render_test.go, cmd/apogee/e2e_toolsfold_test.go, cmd/apogee/e2e_toolsfold_subagent_test.go, cmd/apogee/testdata/frames/t19-tools-umbrella-folded.txt, cmd/apogee/testdata/frames/t18-run-view-finished.txt
**Read first:** internal/tui/toolblock.go — renderSuperGroup, indicatorRow; internal/tui/toolleader.go — leaderRowIn; internal/tui/toolbranch.go — paintRowMarker; internal/tui/blockstate.go — blockState.star;
internal/tui/render_test.go — leaderEdgeRow; internal/tui/mouse_test.go — umbrellaHeaders; cmd/apogee/e2e_toolsfold_test.go — toolsFoldHeader
**Tests:**
- New in `toolblock_test.go`: Read, Terminal×2, Read, Terminal×2, Read → header `✦ Tools (3 Read, 4 Terminal) ⋯…` + edge `▼` open, `▶` folded, nothing else differing; `1 Read` numbering; first-appearance order (Terminal first when it ran first); 1-dot floor at the width where the breakdown just fits; `(N calls)` fallback one cell narrower; clip below that; tone check on a styled render (parenthetical in `toolIndicator`, dots in `toolLeader`, the star cell in its own unstyled tone, not `detailTone`); live star flip still blanks only the star cell.
- Update every pin of the old `✦ Tools (N calls) <glyph>` header in the Files' test files to the new row (a shared golden helper beside `leaderEdgeRow` in `render_test.go`); `umbrellaHeaders` in `mouse_test.go` must still find headers.
- Update the cmd/apogee e2e header constants, Find/click matchers, comments and the two golden frames (regenerated by the repo's update mechanism).
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Umbrella|SuperGroup|Group|Tools|Fold|Target|Shape|Bridge|MarksTheWholeBlock|StarBlinks|LayoutGolden|AnsweredQuestionSolo' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EToolsUmbrella|TestE2EToolsFoldOverZero|TestE2ESubAgentView' ./cmd/apogee/`
- `grep -rn 'Tools ([0-9]* calls)' internal/tui/*_test.go cmd/apogee/*_test.go cmd/apogee/testdata/frames` prints only fallback-width tests and comments item 2 owns.
**Commit:** `feat(tui): break the Tools umbrella header down by tool kind`

## 2. Update the docs and comments that describe the header — ✅ DONE (2026-11-05)

NOTES (2026-11-05): mouse_test.go:3950 and the cmd/apogee e2e comments (e2e_toolsfold_test.go, e2e_toolsfold_subagent_test.go) were already rewritten to the breakdown shape by item 1's commit; both files are in this item's Files but needed no change, so they are not in FILES.
NOTES (2026-11-05): tool-layout.md's dated 2026-09-03 task_list note ("the same faint count tone as the umbrella's `(N calls)`") was reworded to "the umbrella's breakdown" so the Acceptance grep lists only fallback-rule mentions; the tone claim it makes still holds.
NOTES (2026-11-05): test comments name the breakdown each test actually renders — render_test.go's layout golden "(3 Read, 1 Terminal, 1 Diff Preview, 2 Replace, 1 Write)", transcriptbridge_test.go's two skill fetches "(2 Skill)" — rather than a generic example.
NOTES (2026-11-05): no CLOSES section: the item carries no **Closes:** line; the plan-level tools-header-breakdown is the plan closeout's, as item 1's notes left it — this item lands the docs half of its acceptance.

Depends on item 1.
**What:**
**Goal:** every living doc, config comment, glossary Note and code comment that describes the
umbrella header's shape shows the breakdown leader row and the ratified rules (numbered entries,
first-appearance order, faint tone, `(N calls)` fallback, ≥ 1 dot); none presents
`✦ Tools (N calls)` as the standard header.
**Approach (assumed at the header base):** the rule: every non-historical site naming
`Tools (N calls)` / `Tools (<digits> calls)` as the header's shape. Find them with
`grep -rnE 'Tools \([N0-9]* calls\)|\(N calls\)|after (the|its) count\b' docs/layout docs/manual layout.md internal .capcom/knowledge/glossary cmd`.
In `docs/layout/tool-layout.md`, update the Vocabulary **super-group** entry, the Rules fold
paragraph and every super-group sketch (open sketches gain the header dots and edge `▼`, folded
ones the edge `▶`; the "one run" sketch reads `✦ Tools (4 Terminal) ⋯ ▼`); state the ratified rules
once, in Vocabulary. In `layout.md`, `docs/manual/sessions.md`, `docs/manual/configuration.md`,
`internal/config/defaults/config.yaml` and the glossary Note `settings-surface`, a site that only
NAMES the block becomes "the `✦ Tools` umbrella". Comments in `internal/tui/doc.go`,
`transcript.go`, `toolregistry.go`, and test comments outside item 1's pins, follow the same rule.
Leave `.capcom/knowledge/decisions/`, `CHANGELOG.md` and archived docs as written. After editing
the glossary Note, run `capcom check`.
**Regression guard.** widen item 2's site grep and its Acceptance grep to include cmd/ (comments in cmd/apogee e2e tests that describe the header shape and were not already rewritten by item 1 are item 2's).
The rule also covers prose that describes the old shape without the literal: layout.md:795 ("plus its
total call count `(N calls)`"), tool-layout.md:182 and :291, blocktarget.go:44 ("▶/▼ after its count");
the site and Acceptance grep is `grep -rnE 'Tools \([N0-9]* calls\)|\(N calls\)|after (the|its) count\b'`
(the `\b` keeps cmd/'s "after the counts/counters" out). Stale comments at uiprefs.go:79,
render_test.go:266, toolshape_test.go:653, transcriptbridge_test.go:2297 and mouse_test.go:3950 are this item's.
**Files:** docs/layout/tool-layout.md, layout.md, docs/manual/sessions.md, docs/manual/configuration.md, internal/config/defaults/config.yaml, .capcom/knowledge/glossary/glossary-safety/settings-surface.md, internal/tui/doc.go, internal/tui/transcript.go, internal/tui/toolregistry.go, internal/tui/blocktarget.go, internal/domain/uiprefs.go, internal/tui/render_test.go, internal/tui/toolshape_test.go, internal/tui/transcriptbridge_test.go, internal/tui/mouse_test.go, cmd/apogee/e2e_toolsfold_test.go
**Read first:** docs/layout/tool-layout.md — Vocabulary super-group, Rules umbrella-fold paragraph, "Grouped tools folded (large umbrella)", one-run sketch; layout.md — "The rules behind the tool-call sketch";
internal/tui/blocktarget.go — targetUmbrella comment; internal/domain/uiprefs.go — ToolsOpen; docs/manual/configuration.md — ui.tools-open paragraph
**Tests:** none new; existing doc/config drift tests must stay green.
**Acceptance:**
- `grep -rnE 'Tools \([N0-9]* calls\)|\(N calls\)|after (the|its) count\b' docs/layout docs/manual layout.md internal .capcom/knowledge/glossary cmd` lists only fallback-rule mentions and item 1's fallback tests.
- `go build ./... && GOMEMLIMIT=2GiB go test -count=1 ./internal/config/` and `capcom check`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Doc|Drift|Manual|Layout' ./internal/tui/`
**Commit:** `docs(layout): sketch the Tools umbrella header as a per-kind breakdown`
