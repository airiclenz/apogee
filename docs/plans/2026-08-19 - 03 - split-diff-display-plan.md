# Split-diff display plan

**Goal:** Every diff-bodied tool block renders its change as a Split diff (two panes,
line numbers, wrap) where width allows, and as a numbered Stacked diff below that,
fed by tool-recorded Edit regions — with the turquoise/red accessibility palette.

**Date:** 2026-08-19
**Status:** in progress
**Sized for:** ~200k-context host

**Authoritative sources** (when an item and a source disagree, the source wins):
- `docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md`
  (the ratified design: tool-recorded regions, width rule, colors, codec rule, fallback)
- `docs/layout/split-diff-layout.md` (row-level layout: panes, gutters, markers, wrap,
  `⋯` separators, both mock sketches)
- `CONTEXT.md` — terms **Split diff**, **Stacked diff**, **Edit regions**, **Tool
  summary** (the summary is display data: never sent to the model, never in the
  session record)

**Ratified design calls** (owner, 2026-08-19 grill session; codifications marked *plan
author, same date* follow directly from those answers):
1. All five diff-bodied blocks take the new rendering: `single_find_and_replace`,
   `multi_find_and_replace`, `edit_existing_file`, `view_diff`, `git_diff_range`.
2. Edit tools record Edit regions at apply time as a typed summary; `view_diff` and
   `git_diff_range` change no tool — the renderer recovers positions.
3. Context is up to 3 unchanged lines each side; regions whose context ranges would
   touch or overlap merge into one (*plan author*: merge when the gap between two
   changes is ≤ 6 lines).
4. Long pane lines wrap (continuation rows carry no number and no marker); panes stay
   row-aligned by padding.
5. Split paints only when each pane gives the code ≥ 40 columns after its gutters
   (named constant); below that, the Stacked reading — same regions, numbers, context.
6. Marker glyphs `-`/`+` stay in both readings; color never carries a change alone.
7. Colors: `diff-add` turquoise (dark `#2dd4bf`, light `#0f766e`), `diff-del` stays
   red; `success` moves into the turquoise family a visible step from `diff-add`
   (*plan author*: dark `#5eead4`, light `#115e59`); gutters and the pane divider wear
   the existing `muted` role; no new scheme keys.
8. Codec: the region structure travels in a new ADDITIVE field beside `Details`;
   `Details` keeps the stacked rows so older builds replay unchanged.
9. No summary → the old argument-derived `-`/`+` list, exactly as before.
10. Multi-file `git_diff_range` output renders one muted file-header row per file
    section, that file's regions beneath (*plan author*: forced by ratified call 1 —
    the tool's output spans files; a parse that fails falls back to today's plain
    output rendering).

**Standing requirements:**
- skills: coding-standards
- Modularity is binding where an item's **What** names it: one region builder shared
  by all three edit tools, one stacked-row builder shared by all five blocks, one pure
  split composer shared by both paint paths. Do not duplicate any of these per tool or
  per paint path.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Collapsed-state changes (`collapsedBodyRows = 0` stands; collapsed blocks paint no
  body).
- The wire: no tool's `Content` prose changes shape, no session-record schema change,
  no new scheme keys, no config surface.
- Version bump (see closing note).

---

## 1. Domain: the EditRegions tool summary — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the item's Files list names only the two `internal/domain` files, but the
variant would then be unnameable by an embedder and `toolsummary.go`'s own "the root facade
re-exports every one" would be false; added the two-line `EditRegion` / `EditRegions` aliases to
`apogee.go` beside the other summary aliases. No other file touched.
NOTES (2026-08-19): `internal/domain/doc.go` already reads "ToolSummary and its seven variants"
(written ahead of this plan, as CONTEXT.md's "nine built-ins" was) — it is correct as of this
item, so it was left untouched. `internal/tools/doc.go`'s "exactly SIX built-ins attach one" is
still true until item 3 makes a tool attach the new variant, so it stays for that item.

**What:** Add the sealed `domain.ToolSummary` variant carrying Edit regions to
`internal/domain/toolsummary.go`, beside `DiffStat`:

```go
// EditRegion is one changed region of an applied edit ...
type EditRegion struct {
    BeforeStart int      // 1-based first line of the region in the before file (leading context included)
    AfterStart  int      // same, in the after file
    Leading     []string // up to 3 unchanged lines before the change
    Removed     []string
    Inserted    []string
    Trailing    []string // up to 3 unchanged lines after the change
}
type EditRegions struct{ Regions []EditRegion }
func (EditRegions) isToolSummary() {}
```

Add a method `func (e EditRegions) Stat() DiffStat` counting Added/Removed by summing
`len(Inserted)`/`len(Removed)` over regions — the one derivation every consumer (slot,
tests) reuses instead of recounting. Doc comments follow the file's house voice:
say the summary is display data on the Tool summary contract (never model-sent, never
persisted in the session record) and cite ADR 0052.

**Files:** `internal/domain/toolsummary.go`, `internal/domain/toolsummary_test.go`

**Tests:** `Stat()` sums across multiple regions; zero-value `EditRegions` yields the
zero stat.

**Acceptance:** `go build ./... && go test ./internal/domain/`

**Commit:** `feat(domain): add EditRegions tool summary for split-diff rendering`

## 2. Tools: one shared region builder — ✅ DONE (2026-08-19)

NOTES (2026-08-19): ratified call 3's literal "regions ... merge into one" wording is SUPERSEDED
for the whole plan by the owner's decision of 2026-08-19: neighbours within the ≤ 6-line gap
stay SEPARATE regions whose context ranges TILE the interior lines without overlap — the earlier
region takes up to 3 of them as `Trailing`, the later takes the remainder as `Leading`, and a
gap of ≤ 6 always splits cleanly. `Removed`/`Inserted` stay changed-only, so
`EditRegions.Stat()` matches `unifiedLineDiff` exactly, and `domain.EditRegion` is NOT changed.
Consequence for later items: the renderers (items 5 and 6) must omit the `⋯` separator between
regions that are contiguous in line numbering (`next.BeforeStart == prev.BeforeStart +
len(Leading)+len(Removed)+len(Trailing)`), so the painted result is identical to a merge.
NOTES (2026-08-19): attempt 1's `TestEditRegions_MergesNeighboursWithinTheGap` was rewritten as
`TestEditRegions_NeighboursTileTheirContext` — gaps of 3, 6 and 7 unchanged lines, each with an
explicit adjacency assertion — and `TestEditRegions_StatMatchesTheLineDiff` was added to pin the
count parity with `unifiedLineDiff` that the decision turns on. The `editRegionMergeGap`
constant is gone: nothing branches on the gap width any more, the tiling falls out of
`editRegionContext` alone.
NOTES (2026-08-19): the item's Files list names only `regions.go` and `regions_test.go`, but
`internal/tools/doc.go` had to be edited too — its package map must name every non-test file and
`TestDocMapNamesEveryFile` (`internal/tools/docmap_test.go`) fails otherwise. The spine count
moved from five files to six and `regions.go` got its one-line role, worded for the tiling rule.
No other file touched.

**What:** New file `internal/tools/regions.go`: ONE function
`editRegions(oldText, newText string) domain.EditRegions` that every edit tool calls —
no per-tool variant. Build it on the existing Myers machinery (`diffLines`,
`internal/tools/diff.go:225`): walk the ops keeping before/after line counters, cut
regions of consecutive changes, attach up to 3 context lines each side, and merge
regions whose gap is ≤ 6 unchanged lines (ratified call 3). Reuse the
`maxDiffTableCells` guard exactly as `unifiedLineDiff` does (`diff.go:105-112`): an
over-budget pair returns the zero `EditRegions` (no regions → the renderer's fallback,
ratified call 9). Identical inputs → zero value.

**Files:** `internal/tools/regions.go`, `internal/tools/regions_test.go`

**Tests:** single change mid-file (numbers and context correct); change at file head
and at tail (short context, no underflow); two changes 6 lines apart merge, 7 apart
stay separate; pure insertion and pure deletion; identical inputs → no regions;
over-budget pair → no regions.

**Acceptance:** `go test ./internal/tools/ -run Regions && go build ./internal/tools/`

**Commit:** `feat(tools): add shared edit-region builder over the line diff`

Depends on item 1.

## 3. Tools: find-and-replace records regions — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the item's Files list names only `find_replace.go` and its test, but the
"attach the summary only when regions were cut" rule belongs to all three edit tools, so it landed
as one shared helper `okEditRegions` in `internal/tools/regions.go` (beside the builder it guards)
instead of being spelled out at each `okSummary` call site — the plan's standing modularity
requirement, and item 4 returns through the same helper. The item's literal "okResult becomes
okSummary" is satisfied through it: a success with regions attaches them, a success without them
still returns `okResult`.
NOTES (2026-08-19): `internal/tools/doc.go` had to be edited too — its "Exactly SIX built-ins
attach one" sentence and its list of tools that deliberately attach nothing both became false the
moment these two tools attached `EditRegions` (item 2's NOTES already recorded that this item owns
that update). The count is now EIGHT, the find-replace pair moved into the attaching list, and
`regions.go`'s spine line gained `okEditRegions`. No other file touched.
NOTES (2026-08-19): `domain.EditRegion`'s own doc comment still described neighbouring changes as
recorded MERGED into one region; it was reworded to the tiling rule item 2's NOTES ratified, as
part of this item's commit.

**What:** In `internal/tools/find_replace.go`, both tools hold the file's old and new
content at apply time; call `editRegions(old, new)` and return the summary —
`okResult` becomes `okSummary` (the `internal/tools` result helper that attaches a
`domain.ToolSummary`; see `diff.go:88`) on the success paths of `single_find_and_replace`
(`find_replace.go:122`) and `multi_find_and_replace` (`find_replace.go:257`). Prose
`Content` is byte-for-byte unchanged. A zero `EditRegions` (over-budget) attaches no
summary.

**Files:** `internal/tools/find_replace.go`, `internal/tools/find_replace_test.go`

**Tests:** each tool's success result carries an `EditRegions` summary whose regions
match the applied change (line numbers, context); replace-all with several hits yields
several (or merged) regions; failure paths carry no summary.

**Acceptance:** `go test ./internal/tools/ -run 'FindReplace|FindAndReplace|Replace' && go build ./internal/tools/`

**Commit:** `feat(tools): find-and-replace tools record edit regions at apply time`

Depends on item 2.

## 4. Tools: edit_existing_file records regions — ✅ DONE (2026-08-19)

NOTES (2026-08-19): the item's Tests list asks for "a patch that resolves via fuzzy matching still
reports the regions of what actually landed", but no fuzzy matcher exists — `applyPatch`
(`file_edit.go`) locates a hunk by `strings.Index` of its joined old lines, verbatim. The nearest
true behaviour was pinned instead, in `TestEditExistingFile_RegionsFollowWhereThePatchLanded`: a
hunk whose old text REPEATS in the file lands on the first occurrence, not necessarily the one the
model pictured, and the regions report that landing site because they are cut from the read/written
pair and never from the patch's own account of itself.
NOTES (2026-08-19): the item's Files list names only `file_edit.go` and its test, but
`internal/tools/doc.go` had to be edited too — its "Exactly EIGHT built-ins attach one" count and
its list of tools that deliberately attach nothing both became false the moment
`edit_existing_file` attached `EditRegions`. The count is now NINE and `edit_existing_file` moved
out of the "quoting a fixed one-line sentence" list into the attaching one, beside the
find-replace pair (item 3 made the same edit for its two tools). No other file touched.

**What:** In `internal/tools/file_edit.go`, both forms (the `*** Begin Patch` hunk
form, `file_edit.go:108`, and the full-content form, `file_edit.go:114`) compute
`editRegions(oldContent, newContent)` from the whole before/after file content they
already hold, and return it via `okSummary`. Same rules as item 3: prose unchanged,
zero regions → no summary.

**Files:** `internal/tools/file_edit.go`, `internal/tools/file_edit_test.go`

**Tests:** patch form with two hunks → two regions with correct numbers; full-content
form → regions equal to `editRegions` of the two contents; a patch that resolves via
fuzzy matching still reports the regions of what actually landed.

**Acceptance:** `go test ./internal/tools/ -run 'FileEdit|EditExisting|Patch' && go build ./internal/tools/`

**Commit:** `feat(tools): edit_existing_file records edit regions at apply time`

Depends on item 2.

## 5. TUI: consume EditRegions — stacked rows and the stat

**What:** In `internal/tui/toolpresent.go`:

- `toolView` grows a field `Regions []domain.EditRegion` beside `Details`
  (`toolpresent.go:192`) — reuse the domain type, no TUI mirror struct.
- ONE shared builder `stackedDiffLines(regions []domain.EditRegion) []detailLine`
  renders the Stacked reading per `docs/layout/split-diff-layout.md`: per region —
  leading context, `-` rows (before numbers), `+` rows (after numbers), trailing
  context; number gutter right-aligned to the widest number in the body; one damped
  `⋯` separator line (`detailPlain`) between regions; kinds are the existing
  `detailDiffAdded`/`detailDiffRemoved`/`detailPlain`. Every block that has regions —
  this item's edit tools and items 7–8's two diff tools — renders its `Details`
  through this ONE function; the existing wrap machinery (`hangingWrap`) supplies
  continuation rows.
- In the result-enrichment path (`enrichWithResult`, around `toolpresent.go:1120`):
  when the result's summary is `domain.EditRegions`, set `tv.Regions` and REPLACE the
  call-time argBody lines with `stackedDiffLines`. No summary → argBody lines stay,
  exactly as today (ratified call 9).
- Region text crosses the display seam like every other producer string: extend
  `toolView.sanitize()` (`toolpresent.go:839`) to strip the new `Regions` field's
  lines (`Leading`/`Removed`/`Inserted`/`Trailing`) — regions carry tool-recorded
  FILE CONTENT, which is exactly what the escape-strip seam exists for. Add a
  structural guard test asserting every string-carrying field of `toolView` is
  reached by `sanitize` (the member-list idiom `transcriptcodec_test.go` already
  uses for the wire structs), so the next field cannot slip past the seam silently.
- The three edit tools' `+A −R` slot reads `EditRegions.Stat()` via a `stat:` hook,
  falling back to the existing `argStat` when no summary arrived. Keep the stat
  TYPED beside its text: when a summary supplied it, `toolView` carries the
  `domain.DiffStat` value, and the run-aggregate summer (`sumDiffCounts`,
  `toolpresent.go:960`) prefers typed values over re-parsing `+A −R` display text
  (`parseDiffCounts` stays as the fallback for text-only producers) — this feature
  must not become the inverse parser's sixth input.

NOTES (2026-08-19, owner-authorized pre-run amendment): the sanitize bullet and the
typed-stat sentence were added from the TUI architecture review
(`docs/reviews/2026-08-19 - 00 - tui-architecture-review.md`, Candidate 5 and the
sanitize row of §Smaller findings) before execution started.

**Files:** `internal/tui/toolpresent.go`, `internal/tui/toolpresent_test.go`

**Tests:** an edit result carrying regions renders numbered stacked rows matching the
layout doc's stacked sketch (numbers, markers, `⋯` separator, gutter width); a result
without a summary keeps the argument-derived body verbatim (pin against the existing
`TestEditCallsCarryTheirChangedLines` fixtures); the slot reads the summary's stat and
falls back to argStat without one; a region whose lines carry ANSI escapes or control
characters paints stripped; the structural sanitize-coverage guard fails when a
string-carrying `toolView` field is not reached by the strip; a run of edit blocks
sums its `+A −R` aggregate from typed stats (assert `parseDiffCounts` is not what fed
the sum — e.g. via a spelling the parser would reject).

**Acceptance:** `go test ./internal/tui/ -run 'Stacked|Edit|ToolStat' && go build ./internal/tui/`

**Commit:** `feat(tui): edit blocks consume recorded regions as numbered stacked rows`

Depends on item 1. (Items 3–4 make the summaries real but this item's tests construct
results directly; no dependency on them.)

## 6. TUI: the pure split composer

**What:** New file `internal/tui/splitdiff.go`: a PURE composition module, no paint
integration yet — both paint paths in item 7 call this one composer.

- `const splitPaneMinCols = 40` — the per-pane content floor (ratified call 5).
- `splitDiffFits(regions []domain.EditRegion, width int) bool` — true when
  `(width - divider) / 2 - numberGutter - markerCols ≥ splitPaneMinCols`, gutter
  measured from the widest line number in the regions.
- `splitDiffRows(th theme, regions []domain.EditRegion, width int) []string` —
  styled rows per `docs/layout/split-diff-layout.md`: left pane = before (numbers,
  `-` marker + `th.diffRemoved` on removed rows), right pane = after (numbers, `+`
  marker + `th.diffAdded` on added rows), context in both panes in the open tone
  (`th.toolDetailBright`), numbers/divider `│`/`⋯` separator in `th.toolDetail`
  (muted). Within a region the removed and inserted stacks start on the same row and
  the shorter side pads; wrapped continuations carry no number and no marker and both
  panes stay row-aligned. Wrap through the width authority (`th.measure`,
  `theme.go:130`) — never `len()` or a second wrap idiom.

Keep the module deep and self-contained: pane assembly, gutter sizing, wrap and
padding are private helpers here; nothing about blocks, entries or fold state leaks in.

**Files:** `internal/tui/splitdiff.go`, `internal/tui/splitdiff_test.go`

**Tests:** golden rows for the layout doc's wide sketch (wrap + padding + `⋯` rule +
drifting numbers); `splitDiffFits` flips at the boundary width; a one-sided region
(pure insertion) pads the whole left stack; continuation rows carry no number/marker;
row alignment holds when the wrapping side alternates.

**Acceptance:** `go test ./internal/tui/ -run SplitDiff && go build ./internal/tui/`

**Commit:** `feat(tui): pure split-diff composer with width rule and wrap`

Depends on item 1 (the domain type only — parallel-safe beside item 5: disjoint files).

## 7. TUI: paint integration — split where it fits

**What:** Wire the composer into ALL THREE expanded body paint paths a diff-bodied
block can reach, choosing per paint: regions present AND `splitDiffFits` →
`splitDiffRows`; otherwise the existing detail-line painting (whose lines are already
the stacked reading after item 5).

- Targeted branch: `renderToolBranch` → the `renderSubDetails` call
  (`toolbranch.go:77`).
- Targetless expanded branch: `renderToolBranch`'s `tv.Target == ""` arm
  (`toolbranch.go:58`, the `renderDetails` call). Reachable by any diff-bodied call
  whose target resolves empty — the common case is a bare `git_diff_range` with no
  `base` and no `head`, where `refRangeTarget` returns `""` (`toolpresent.go:1666`).
  Only the body chooses split; the branch-list framing around it stays.
- Grouped expanded member: `renderExpandedMember` (`toolblock.go:351`). This one
  path also covers unspanned sub-agent group members, which route through
  `renderGroupMember` → `renderExpandedMember` (`subagentblock.go:417`).

Deliberately NOT wired: the spanned sub-agent member loop
(`renderSubAgentMemberRows`, `subagentblock.go:424`) — it paints the `sub_agent`
call's own report body, and `sub_agent` is not a diff-bodied tool, so no region can
reach it. (Its near-duplicate body loop is a separate deepening finding — review
Candidate 4 — not this plan's concern.)

The decision is made at paint time against the width parameter each path already
holds, so a resize re-flows and can flip the reading (ADR 0052 §3). No change to
collapsed painting.

NOTES (2026-08-19, owner-authorized pre-run amendment): the targetless arm was added
and the sub-agent exclusion recorded, from the TUI architecture review
(`docs/reviews/2026-08-19 - 00 - tui-architecture-review.md`, Candidate 4) — without
it a bare `git_diff_range` would keep the stacked reading at every width while the
same diff with refs named paints split.

**Files:** `internal/tui/toolbranch.go`, `internal/tui/toolblock.go`,
`internal/tui/toolbranch_test.go`, `internal/tui/toolblock_test.go`

**Tests:** an expanded edit block at 140 columns paints two panes; the same block at
80 columns paints the stacked rows; a grouped expanded member does the same; a
TARGETLESS expanded block carrying regions (a bare `git_diff_range`) paints split at
140 columns and stacked at 80; blocks without regions paint exactly as before (pin
with an existing-fixture assertion); shape budgets in `toolshape_test.go` still hold.

**Acceptance:** `go test ./internal/tui/ -run 'ToolBranch|ToolBlock|Shape|SplitDiff' && go build ./internal/tui/`

**Commit:** `feat(tui): expanded diff bodies paint split panes where width allows`

Depends on items 5 and 6.

## 8. TUI: view_diff recovers regions

**What:** In `internal/tui/toolpresent.go`, rework `viewDiffBody`
(`toolpresent.go:1954`) / `diffBody` (`toolpresent.go:1995`): `view_diff`'s body is a
whole-file tagged diff, so walk it counting before/after line numbers from 1, build
`[]domain.EditRegion` trimmed to ≤ 3 context lines each side (merge per ratified call
3 — reuse the region-cutting idiom, but note the input here is tagged lines, not two
texts), set `tv.Regions`, and render `Details` through item 5's `stackedDiffLines`.
Expanded `view_diff` therefore stops painting the whole file (ADR 0052 §2 —
deliberate). The "No changes detected" prose result and the over-budget
diffstat-only sentence carry no tags and keep today's plain rendering.

**Files:** `internal/tui/toolpresent.go`, `internal/tui/toolpresent_test.go`

**Tests:** a whole-file diff body yields regions with correct absolute numbers and
trimmed context; untagged prose bodies render plain as today (pin against
`TestViewDiffNoChangesRendersAsProse`); `TestDiffStatSpansTheWholeDiff` still passes
(the slot's stat is untouched — it comes from `domain.DiffStat`).

**Acceptance:** `go test ./internal/tui/ -run 'ViewDiff|Diff' && go build ./internal/tui/`

**Commit:** `feat(tui): view_diff derives regions from its whole-file body`

Depends on items 5 and 7.

## 9. TUI: git_diff_range recovers regions

**What:** In `internal/tui/toolpresent.go`, give `git_diff_range`
(`toolpresent.go:594-600`) a `body:` hook (it has none today — its output renders
uncoloured through `outputDetail`): parse git's unified output — `diff --git` file
sections, `@@ -a,b +c,d @@` hunk headers — into per-file region groups: one muted
file-header row naming the file (ratified call 10), then that file's regions via
`stackedDiffLines`, regions and headers stored so item 7's composer paints them
per file section. Any line the parser does not recognize → fall back to today's
`outputDetail` rendering for the WHOLE body (never a half-parsed mix). The existing
`diffLinesStat` slot stays.

**Files:** `internal/tui/toolpresent.go`, `internal/tui/toolpresent_test.go`

**Tests:** a two-file git diff yields two header rows with each file's regions and
correct numbers from the `@@` headers; binary-file and rename-only sections fall back
to plain rendering; a malformed body falls back wholesale.

**Acceptance:** `go test ./internal/tui/ -run 'GitDiff|Diff' && go build ./internal/tui/`

**Commit:** `feat(tui): git_diff_range parses hunk headers into split-diff regions`

Depends on items 5 and 7.

## 10. Codec: regions travel additively

**What:** In `internal/tui/transcriptcodec.go`, persist `toolView.Regions` as a new
ADDITIVE field on `wireToolView` (`transcriptcodec.go:149-159`) — e.g.
`Regions []wireEditRegion \`json:"regions,omitempty"\`` with a `wireEditRegion`
mirroring `domain.EditRegion` — mapped in `toWireToolView`
(`transcriptcodec.go:340`) and `fromWireToolView` (`transcriptcodec.go:451`).
`Details` keeps carrying the stacked rows (ADR 0052 §5); no `transcriptVersion` bump —
follow the file's documented additive-field rule (`transcriptcodec.go:49-58`) and
write the same style of field comment its Solo/Stat/Task fields carry.

**Files:** `internal/tui/transcriptcodec.go`, `internal/tui/transcriptcodec_test.go`

**Tests:** round-trip preserves regions (extend `TestTranscriptCodecRoundTrip`); a
record without the field decodes with nil regions and paints stacked
(`TestTranscriptCodecDecodesALegacyBlobUnchanged` untouched);
`TestTranscriptCodecGoldenV1` must pass UNMODIFIED — omitempty keeps old blobs
byte-identical.

**Acceptance:** `go test ./internal/tui/ -run TranscriptCodec && go build ./internal/tui/`

**Commit:** `feat(tui): transcript codec carries edit regions additively`

Depends on item 5.

## 11. Schemes: the turquoise palette

**What:** In `internal/scheme/schemes/dark.yaml` and `light.yaml` (ratified call 7):
dark `diff-add: "#2dd4bf"`, `success: "#5eead4"`; light `diff-add: "#0f766e"`,
`success: "#115e59"`. `diff-del` values unchanged. Reword the two `success` comments
that pitch it relative to a *green* `diff-add` (`dark.yaml:27`, `light.yaml:26`) to
state the same one-step relation in the turquoise family, and keep every comment's
role-relation claim true of the new values (visible step apart:
`TestBuiltinSchemesKeepSuccessAndErrorDistinct`, `builtins_test.go:133`, must hold).

**Files:** `internal/scheme/schemes/dark.yaml`, `internal/scheme/schemes/light.yaml`

**Tests:** existing scheme/builtins suites pin structure and distinctness — run them;
update any test that pins the old hex values.

**Acceptance:** `go test ./internal/scheme/`

**Commit:** `feat(scheme): turquoise additions and success for red-green-safe palette`

No dependencies. (May run any time; listed late so the palette lands with its feature.)

## 12. Docs: tool-layout pointers and the IDEAS closure

**What:**
- `docs/layout/tool-layout.md`: the per-tool table's five diff rows
  (`edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace`,
  `view_diff`, `git_diff_range` — table at the "per-tool table" section) point their
  `<tool-details-row-*>` cell at `split-diff-layout.md` ("split/stacked diff, see
  split-diff-layout.md"), and a dated note records that `git_diff_range`'s body is now
  parsed and coloured (it was plain output) and that expanded `view_diff` no longer
  paints the whole file.
- `IDEAS.md`: remove the `[P]` split-diff item (per that file's conventions a
  delivered item is removed; the run's CHANGELOG entries are the closed trail).

**Files:** `docs/layout/tool-layout.md`, `IDEAS.md`

**Tests:** none (docs).

**Acceptance:** `grep -c "split-diff-layout.md" docs/layout/tool-layout.md` reports ≥ 5;
`grep -ci "diff" IDEAS.md` reports 0.

**Commit:** `docs(layout): point diff rows at split-diff-layout and close the IDEAS item`

Depends on items 7, 8, 9.

---

**Suggested version bump:** minor (user-visible feature: new diff rendering and
palette across five tool blocks). The user decides; no item bumps anything.
