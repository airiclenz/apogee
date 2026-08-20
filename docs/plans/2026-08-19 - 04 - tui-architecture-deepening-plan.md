# TUI architecture deepening — implementation plan

**Goal:** Execute all twelve candidates and the smaller findings from the 2026-08-19 TUI
architecture review: delete duplicated state machinery, stop re-deriving facts a composer
already computed, and finish the file splits ADR 0043 started. Behaviour-preserving
throughout — nothing in this plan changes what the user sees or how the TUI behaves.

- **Date:** 2026-08-19
- **Status:** not started
- **Sized for:** ~200k-context host

## Authoritative sources

- `docs/reviews/2026-08-19 - 00 - tui-architecture-review.md` — the canonical record.
  Its evidence is pinned at commit `030ab021`; **line numbers have drifted** (the
  split-diff plan landed since) — anchor on the function names, section banners, and
  duplication counts it cites, never on its line numbers. When an item below disagrees
  with the review's evidence, the review wins.
- ADR 0011 (thin renderer, value-type `Model`), ADR 0030 (width authority), ADR 0043
  (flat package + doc.go map), ADR 0035/0037/0041 (settings display projection lives in
  `cmd/apogee`), ADR 0052 (split diffs). When the review disagrees with an ADR, the ADR
  wins.
- `docs/plans/2026-08-19 - 03 - split-diff-display-plan.md` — must be archived before
  any item here runs; item 1 gates.

## Ratified design calls

1. **Scope = everything** — all 12 candidates plus the smaller-findings table, sequenced
   per the review's Recommended sequence. (User, 2026-08-19.)
2. **Merge policy = preserve and parameterize.** Where merged duplicates disagree on
   user-visible behaviour (↓ at the bottom of a list: wrap vs stop; caret glyphs ▌ / ▏ /
   lineEditor's; TrimSpace vs TrimRight on settings commit), each pane keeps its exact
   current behaviour; the shared module takes the difference as a parameter, and the
   per-pane choice is documented at the call site. Zero visible change. (User,
   2026-08-19.)
3. **New-file names** (binding; each costs one doc.go line in the same commit):
   `heartbeat.go`, `ask.go`, `boxdraw.go`, `toolview.go`, `toolregistry.go`,
   `diffbody.go`, `toolargs.go`, `textutil.go`, `sanitize.go`, `toolbody.go`,
   `reportpane.go`, `listsurface.go`, `settingswatcher.go`, `settingsapply.go`,
   `entrykind.go`, `blocktarget.go`, `parkedcall.go`. (Plan author, from the review's
   deepened shapes, 2026-08-19.)
4. **New ADRs ride their module items:** ADR 0053 (shared list surface) is written
   inside item 15; ADR 0054 (Options interface regrouping) inside item 25. (Review's
   closing recommendation.)

## Standing requirements

- skills: coding-standards
- Every item is behaviour-preserving. Golden/layout/mouse tests pin behaviour; an item
  that needs to change a test's expectation is off the rails — stop and report BLOCKED.
- ADR 0043: `internal/tui` stays flat; every file added or deleted updates `doc.go` in
  the same commit (`docmap_test.go` enforces).
- ADR 0011: `Model` stays a value type — no mutex, no self-pointer, no no-copy type held
  by value (`TestModelNoBuilderByValue` guards).
- ADR 0030: no `lipgloss.Width` / `ansi.StringWidth` / `runewidth` outside the declared
  widget mirrors.
- For items inside `model.go` / `settings.go` / `toolpresent.go`: read only the named
  cluster span (the review's cluster map gives boundaries) plus what the change touches —
  never the whole file.
- Any authorized deviation from item text must land as a dated NOTES line under the item.
- No version identifier changes anywhere in this plan (see the closing note).

## Out of scope (the review's "Verified healthy — do not re-litigate")

- Reshaping the tool registry (moves whole in item 6, never reshaped).
- The event fold, the width authority, `theme.go`, the paint cache's core.
- `inputaccent.go`'s widget mirrors (deliberate; consolidating reintroduces caret bugs).
- Merging `lineEditor` → `promptEditor` (layering is sound).
- Inventing a seam for `commandrun.go`; the four small verb files' shape.
- Removing the prose-parsing residue in the six stat hooks (documented trade, design
  call 14) — only doc.go's overclaiming sentence is fixed (item 10).
- The transcript codec's unknown-kind degrade path.
- Moving the settings display projection out of `cmd/apogee` (ADR 0035/0037).
- The `toolView.sanitize()` structural test — absorbed by the split-diff plan's item 5.

---

## 1. Verify the split-diff plan is archived — ✅ DONE (2026-08-20)

**What:** Confirm `docs/plans/archived/` contains
`2026-08-19 - 03 - split-diff-display-plan.md` and that it is gone from `docs/plans/`.
That plan carries the time-critical halves of Candidates 4 and 5 (targetless-arm wiring,
`EditRegions` stat, sanitize structural test); nothing below may run before it lands. If
it is not archived, report BLOCKED — do not proceed.

**Files:** none (read-only gate; the verifier's commit carries only this plan file's
done-mark).

**Tests:** none.

**Acceptance:** `ls "docs/plans/archived/" | grep "split-diff-display-plan"` succeeds
and `ls docs/plans/ | grep -c split-diff` prints 0.

**Commit:** `chore(plans): gate tui deepening on the landed split-diff plan`

## 2. Move the heartbeat cluster into heartbeat.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item lists `foldBeatFailure` and `blockedUpstream` but not their
neighbours inside the same contiguous span — the whole span between the worker-lifecycle and
layout banners moved as the item's "and the rest of the contiguous cluster" clause directs,
which additionally carries `heartbeatLive`, `offlineFailureThreshold`, `onlineNote`,
`rebindFailNote`, `unknownWindowNote`, `windowWord`, `serverSwitchNote` and `upstreamBlockNote`.

**Source:** review §Candidate 6, cluster 1.

**What:** Pure same-package move, zero call-site churn, zero behaviour change. Move the
heartbeat / rebind / server-switch cluster out of `internal/tui/model.go` into new
`internal/tui/heartbeat.go`: `heartbeatState`, `rebindIntent`, `offlineNote`, `beatCmd`,
`armBeat`, `beatTick`, `foldBeat`, `observeBinding`, `applyRebind`,
`applyPendingRebind`, `rebindNote`, `foldServerSwitch`, `foldBeatFailure`,
`blockedUpstream`, and the rest of the contiguous cluster (≈493 lines; the review's
cluster map places it between the worker-lifecycle and layout-arithmetic sections —
locate by these names). `Model` struct fields stay in model.go. Add the doc.go line in
the same commit.

**Files:** internal/tui/model.go, internal/tui/heartbeat.go, internal/tui/doc.go

**Tests:** existing suite only (pure move).

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): move the heartbeat/rebind cluster into heartbeat.go`

## 3. Move the ask_user pane into ask.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item's "ask key handling ... as a named method" needed a shape the inline
block did not have — the block fell THROUGH to the input routing on any key it did not act on.
`askChoiceKey` therefore returns `(bool, tea.Model, tea.Cmd)`, matching `usageKey`/`inspectorKey`'s
existing claim signature, and `handleKey` keeps it in exactly the same position between the
approval branch and the `inputEditable` block. Mutation-free on every unclaimed path, so the
fall-through is byte-identical to before.

**Source:** review §Candidate 6, cluster 2.

**What:** Move the ask pane (~370 lines) out of model.go into new `internal/tui/ask.go`,
mirroring approval.go's both-halves layout (state + keys + layout in one file, "so a row
can never be paintable and unreachable"). Move: the `askReqMsg` Update-arm body (as a
named `Model` method called from the arm), the ask key handling currently inline in
`handleKey` (make it a named method living in ask.go, called from handleKey),
`submitAnswer`, `checkedLabels`, `restoreAskDraft`, and the ask-pane layout section at
the end of model.go. Struct fields stay in model.go. Zero behaviour change. doc.go line
in the same commit.

**Files:** internal/tui/model.go, internal/tui/ask.go, internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): move the ask_user pane into ask.go beside approval.go`

## 4. Move the box/join paint primitives into boxdraw.go — ✅ DONE (2026-08-20)

**Source:** review §Candidate 6, cluster 3.

**What:** Move `squareLine`, `squareOnField`, `drawBox`, `drawTitledBox`,
`joinScrollbar`, `joinFrame` (≈159 contiguous lines in model.go's paint-primitives
section) into new `internal/tui/boxdraw.go` beside wrap.go. Signatures unchanged —
callers (popup.go, userblock.go, startupbox.go) untouched. doc.go line in the same
commit.

**Files:** internal/tui/model.go, internal/tui/boxdraw.go, internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): move box/join paint primitives into boxdraw.go`

## 5. Move the tool card type and view lifecycle into toolview.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item names the first banner section and the view-lifecycle section; the file's
own header banner (`Tool presentation (P2.7 …)`) stayed in toolpresent.go because the last third of it
narrates the registry and the stat hooks, which stay there for items 6 and 8. toolview.go opens with a
new banner of its own naming the card and the two lifecycle moments — the only text in the new file that
is not moved verbatim.

**Source:** review §Candidate 7, spans 1 and 3. Depends on item 1.

**What:** Pure move out of `internal/tui/toolpresent.go` into new
`internal/tui/toolview.go`, anchored on toolpresent.go's own section banners: the card
value types (`toolView`, `toolBody`, `detailLine` and their neighbours in the first
banner section) and the view lifecycle section (`presentToolCall`, `enrichWithResult`,
the sanitize/shorten helpers, run aggregation). Zero call-site churn. Test files stay
where they are. doc.go line in the same commit.

**Files:** internal/tui/toolpresent.go, internal/tui/toolview.go, internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): move the tool card type and view lifecycle into toolview.go`

## 6. Move the presenter registry and tool hooks into toolregistry.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the file's own header banner (`Tool presentation (P2.7 …)`) moved with this item
rather than staying behind — item 5's note left it for "items 6 and 8" because it narrates the registry
and the stat hooks, and this item moves both. It is moved verbatim; `toolregistry.go` therefore opens
with it and `toolpresent.go` is left with no header banner until item 7 or 8 gives it one or deletes it.
NOTES (2026-08-20): the split of "per-tool body hooks" from "the diff-body cluster (item 7 moves that)"
was drawn at the diff machinery's own edge: `readFileBody` (read off the typed summary) moved, while
`viewDiffBody`, `diffBody`, the region recovery, the `editPair`/`changedLines` family, the git-diff walk,
the stacked rows and the four argument-derived bodies built on them (`singleReplacementBody`,
`multiReplacementBody`, `fileEditBody`, `writtenLines`) stayed together for item 7. `diffCounts` and
`pairCounts` sit inside the stat section and moved with it (`diffCounts` keeps its third caller in
`toolview.go`'s run aggregation, unchanged).

**Source:** review §Candidate 7, spans 2 and 4. Depends on item 5.

**What:** Pure move out of toolpresent.go into new `internal/tui/toolregistry.go`: the
`toolPresenter` type (8 hooks), the `toolRegistry` table (27 entries), and the per-tool
stat/target/body hooks — **except** the diff-body cluster (item 7 moves that). The
registry is verified healthy: move it whole, never reshape it; adding a tool must remain
a one-edit change. doc.go line in the same commit.

**Files:** internal/tui/toolpresent.go, internal/tui/toolregistry.go,
internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): move the presenter registry and tool hooks into toolregistry.go`

## 7. Move the diff-body cluster into diffbody.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item allows folding into the split-diff plan's existing composer file instead;
the user ruled on 2026-08-20 that `diffbody.go` is created as its own file — `splitdiff.go` composes
the wide reading only, and items 11 and 12 name `diffbody.go` by that name.
NOTES (2026-08-20): the moved span is byte-identical, but the new file opens with a header banner of
its own (the four sibling files all carry one, and item 6 left `toolpresent.go` with none) — the only
text in `diffbody.go` that is not moved verbatim. The moved cluster's own `git_diff_range` banner
travelled with it unchanged.
NOTES (2026-08-20): two prose pointers at the moved code were corrected in the same commit because
this move is what staled them — `splitdiff.go`'s header and `doc.go`'s splitdiff entry both said the
stacked rows are built in `toolpresent.go`; both now say `diffbody.go`. `splitdiff.go` is outside the
item's Files line for that one-word reason.

**Source:** review §Candidate 7 ("diff bodies beside the split-diff plan's
`splitdiff.go`"). Depends on item 6.

**What:** Pure move of the whole diff-body hook cluster out of toolpresent.go into new
`internal/tui/diffbody.go`, beside the composer file the split-diff plan created (find
it in doc.go). If that plan already established an equivalent home for these hooks, fold
into it instead of creating diffbody.go and record a dated NOTES line here. doc.go in
the same commit.

**Files:** internal/tui/toolpresent.go, internal/tui/diffbody.go, internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): move the diff-body cluster into diffbody.go`

## 8. Split toolpresent.go's tail and delete the file — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item names only the two new files, but the deleted file's name survived in prose across the repo, so the DECISION's sweep landed in this commit: eleven source/test comments, three `doc.go` narration sentences, four `ISSUES.md` pointers and two ADRs. The boundary drawn — and the reason `grep -r toolpresent` is not empty — is LIVE pointer vs HISTORICAL record: anything a reader follows to navigate today's code was corrected; the CHANGELOG's past entries (which this run may never edit), `docs/plans/archived/*`, and `docs/reviews/*` (whose evidence is explicitly pinned at commit `030ab021`) were left as written, since rewriting them would falsify the record rather than fix a pointer.
NOTES (2026-08-20): both new files open with a header banner of their own — the only text in either that is not moved verbatim — following items 5 and 7's precedent, since every sibling file in the cluster carries one. The moved spans were verified line-for-line identical to the deleted file's; only its `package`/`import` clause and three separator blank lines did not travel.
NOTES (2026-08-20): the item leaves "anything still left" to be filed "by its banner", but item 6 moved the file's only banner, so the tail was filed by module instead: `parseArgs` went to `toolargs.go` (it is the same argument bytes read as a map for the registry's target extractors) and `resolvedPathNote` too (it words one argument-derived target fact, and both surfaces that disclose it already read `toolargs.go` for the arguments beside it).
NOTES (2026-08-20): `toolpresent_test.go` keeps its name — the item says test files stay put — so the package now holds a suite named after a file that no longer exists; `toolshape_test.go`'s naming rationale was reworded to say so rather than to hide it.

**Source:** review §Candidate 7, span 5. Depends on item 7.

**What:** Move the two homeless modules in toolpresent.go's tail: the JSON-argument
display module (`argumentDetails` and friends — approval.go consumes it) into new
`internal/tui/toolargs.go`; the generic text utilities (`clipDetail`, `plural`,
`firstLine`, … — called from 7 files) into new `internal/tui/textutil.go`. Anything
still left in toolpresent.go moves to whichever of the item-5/6/7 files matches its
banner. Delete toolpresent.go; doc.go drops its line and gains the two new ones, same
commit. Test files stay put.

**Files:** internal/tui/toolpresent.go (deleted), internal/tui/toolargs.go,
internal/tui/textutil.go, internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): split toolpresent.go's tail into toolargs.go and textutil.go`

## 9. Rehome the escape-stripping security seam into sanitize.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the moved span is verbatim — `stripEscapes`, `bidiControl` and `stripEscapesAll` with their full doc comments, diffed line-for-line against `HEAD:internal/tui/transcript.go`. The only text in `sanitize.go` that did not travel is its header banner and `package`/`import` clause, following items 5, 7 and 8's precedent, since every sibling file in the package carries a banner of its own. `transcript.go` keeps the "Formatting helpers" banner for the five helpers still under it (`flattenField`, `blankLine`, `trimBlankLines`, `trimTrailingBlankLines`, `prettyJSON`).

NOTES (2026-08-20): the item asks the invariant wording to point at the new home, and `doc.go`'s invariant paragraph named no file at all before this — it described the seams that strip, never where the strip itself lived. One sentence was added naming `sanitize.go` as that home; the rest of the paragraph is untouched.

NOTES (2026-08-20): no live prose pointer elsewhere ties the seam to `transcript.go`. The repo-wide grep hits are all HISTORICAL record on item 8's boundary — `CHANGELOG.md`'s past entries, `docs/plans/archived/*`, `docs/reviews/archived/*` and the review pinned at commit `030ab021` — and were left as written; the five live Go comments naming `transcript.go` all point at the scrollback model, which did not move.

**Source:** review §Candidate 7 ("also misfiled").

**What:** Move `stripEscapes`, `bidiControl`, `stripEscapesAll` — the package's security
seam, referenced from 19 files, doc.go's second invariant — out of
`internal/tui/transcript.go` into new `internal/tui/sanitize.go`. Pure move, zero
call-site churn; their tests stay where they are. Update doc.go's file map (and keep its
invariant wording pointing at the new home) in the same commit.

**Files:** internal/tui/transcript.go, internal/tui/sanitize.go, internal/tui/doc.go

**Tests:** existing suite only.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): rehome the escape-stripping security seam into sanitize.go`

## 10. Correct doc.go's ADR 0011 narration — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item says "the doc.go sentence", singular, but the same overclaim is written twice — once in the candidate-03 narration paragraph and once in the file-map line for `toolregistry.go`, which said the stat words the slot off `domain.ToolSummary` full stop. Both were corrected; leaving the second would have kept a reader one grep away from the claim the item exists to retire.
NOTES (2026-08-20): the review's evidence phrase "five package regexes" was deliberately NOT carried into `doc.go`. It does not hold for the six hooks as they stand: `commitCountStat` and `diffLinesStat` use no regex at all (line and prefix counting), and one of the five package regexes, `exitCodeMarker`, belongs to `exitCodeFailure` rather than to any of the six. The new prose names the six hooks and the reading they take instead of counting regexes, which is what `toolregistry.go`'s own note does.

**Source:** review §Verified healthy, last bullet.

**What:** doc.go's ADR 0011 narration reads as if prose parsing was fully eliminated;
the honest statement lives beside the six stat hooks (post-split: toolregistry.go).
Reword the doc.go sentence to match reality — the residue is a documented trade (design
call 14), total hooks that return false on unrecognised shapes. Docs-only; do not touch
the hooks.

**Files:** internal/tui/doc.go

**Tests:** none beyond the docmap test already in the suite.

**Acceptance:** `go test ./internal/tui`

**Commit:** `docs(tui): correct doc.go's ADR 0011 narration about residual prose parsing`

## 11. One body painter behind the five tool-body frames — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item's Files line names `internal/tui/diffbody.go`, but nothing in it needed changing — it produces BODIES (the recorded and recovered regions, the stacked rows built from them) and never the frames those bodies are drawn in, and no prose in it pointed at code this item moved.
NOTES (2026-08-20): `splitBody` moved from `toolbranch.go` into `toolbody.go` (same-package move, body verbatim; two sentences of its doc comment reworded because the move staled them — it named `renderSubDetails`/`gutteredWrap` as "the detail-line painters" and narrated the three call sites it no longer has). It IS the split-vs-stacked decision the item asks to fold into the painter, so putting it beside the painter is what makes `toolbody.go` the single home of ADR 0052's rendering rule; its only caller is now `paintToolBody`.
NOTES (2026-08-20): `renderSubDetails` changed signature from `(th, details, indent, width)` to `(th, tv, indent, width)` — folding its path's split wiring in requires the call's recorded regions, and `renderToolBranch` is its only caller. `renderDetails` and `clipDetails` deliberately kept theirs: `blockstate.go` and `toolbranch_test.go` call them and are outside this item's scope. They therefore paint through the frame's own painter (`bodyFrame.paint`) while the three split-capable paths go through `paintToolBody` — which is also the honest split, since neither of those two ever had a reading to choose (`clipDetails` is the collapsed shape; `renderDetails` is spent on the summary line that closes a split body, which is not a body line).
NOTES (2026-08-20): four frame constructors, not five — `renderExpandedMember` and `renderSubAgentMemberRows` frame a body identically (`openMemberFrame`, differing only in the gutter string their caller holds) and differ solely in the reading they ask for, which the sub-agent site's comment states. The new test still pins all five SITES separately, each against the primitive calls its own loop used to make.
NOTES (2026-08-20): the item's Files line names no test file; the tests it asks for landed in new `internal/tui/toolbody_test.go` (`TestEveryToolBodyFrameKeepsItsOwnFraming` over the five sites, plus `TestTheToolBodyFramesStayDistinct` so a merge that collapsed two frames into one could not pass by collapsing both sides of a per-frame pin).

**Source:** review §Candidate 4 (the remaining deepening; the time-critical half landed
with the split-diff plan). Depends on items 1 and 7.

**What:** Create new `internal/tui/toolbody.go` with one body painter — (detail lines,
frame spec, width) → rows — and make the five frame sites call it: `renderDetails`
(expanded flat), `clipDetails` (collapsed), `renderSubDetails` (marker indent) in
toolbranch.go; `renderExpandedMember` (│ gutter) in toolblock.go;
`renderSubAgentMemberRows` (│ gutter) in subagentblock.go. Fold the split-diff plan's
per-path wiring (the split-vs-stacked decision) into the painter so ADR 0052's rendering
rule lives in one place. Per merge policy, each site's current physical framing (gutter,
indent, clip, wrap) is preserved exactly via the frame spec parameter. The primitive
chain underneath (`toolRowCells` → `leaderRow` → `indicatorRow` → `gutteredWrap` →
`seeLessRow`) is healthy — the painter sits one level above it, never replaces it.
doc.go line in the same commit.

**Files:** internal/tui/toolbranch.go, internal/tui/toolblock.go,
internal/tui/subagentblock.go, internal/tui/diffbody.go, internal/tui/toolbody.go,
internal/tui/doc.go

**Tests:** existing golden/layout tests pass unchanged; add a unit test that each of the
five frame specs reproduces today's framing for a fixed detail body.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): render all five tool-body frames through one body painter`

## 12. Typed stat values replace the prose round-trip — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item's Files line names three files; `internal/tui/diffbody.go` needed no change (it produces BODIES — the recorded and recovered regions and the rows built from them — and words no stat: `diffCounts` and `pairCounts` live in `toolregistry.go` since item 6). Three files outside the line were touched instead, each for one reason the change itself created: `textutil.go` splits `pluralNoun` out of `plural` so a typed count is spelled by the very same rule (the item asks `spell()` to reuse that wording); `toolleader.go` corrects one stale sentence in `failedSummary`'s doc — it said the aggregate "is read through the very parser that wrote it (countPhrase)", and countPhrase no longer writes anything; `doc.go` corrects the one clause of its `toolview.go` map line that said the aggregate is worded "out of what its members already say".
NOTES (2026-08-20): `transcriptcodec.go` is the fourth, and it is a wire ADDITION rather than a pointer fix. The typed stat was never persisted (`hasDiffStat`/`diffStat` did not ride the wire), so a replayed transcript summed its runs BY PARSING the phrases on disk — deleting the parser without persisting the value would have blanked every resumed session's type-row total, which the plan's "nothing changes what the user sees" forbids. `wireStatValue` is written only where a phrase has arithmetic in it (`wireBranchSummary.Stat`, `wireToolView.StatValue`), so it is additive within `transcriptVersion` on the file's standing rule and `TestTranscriptCodecGoldenV1` is byte-unchanged; the wire member census was updated, which is that test's own "widening the wire needs its own decision" mechanism.
NOTES (2026-08-20): `absorbRegions` was deleted rather than kept. Its two acts were the rows (`showRegions`) and the typed diffstat, and the diffstat is now the slot's — counted off the same recorded regions by the tool's own stat hook (`editRegionsStat`, which all three edit tools carry). What is left is a one-line wrapper the deletion test says to remove; its load-bearing sentence (the body is REPLACED, not grown) moved onto `showRegions`' doc.
NOTES (2026-08-20): two test expectations changed because the code they pinned is the code the item deletes. `TestRunAggregateSumsTypedDiffStats`' sub-test "a text-only run still sums through the parser" is removed — it existed to pin `parseDiffCounts` as the floor — and the surviving sub-tests now desync a member's wording from its value to prove the sum reads the value. `TestEditResultWithoutRegionsKeepsTheArgumentBody` asserted `!hasDiffStat`; a summary-less edit result DOES now leave a typed slot stat, the argument-derived one, so the assertion states that instead. Every other expectation is untouched: `TestRunAggregate`'s want column is verbatim, only its fixtures became the values a presenter builds rather than phrases read back.
NOTES (2026-08-20): a consequence the item's deletions imply, recorded because it is a behaviour difference and not only a refactor — a member whose slot holds a PROSE sentence that happens to read like a count ("12 files" as a tool's own first line, where its stat hook declined) used to join its run's sum through the parser and no longer does. Only stats a presenter typed are summed now, which is what "typed stat values replace the prose round-trip" means; no fixture or golden test covered such a run.

**Source:** review §Candidate 5 (the remaining deepening). Depends on items 1 and 8.

**What:** Introduce a small typed stat value — a sum type of (count + noun) and
(added + removed) that knows `add()` and `spell()` — carried beside the rendered text on
`toolView`. Stat hooks produce the value (including the split-diff plan's `EditRegions`
producer); group/run aggregate rows sum values instead of parsing strings. Delete
`parseDiffCounts`, the parsing leg of `sumCountPhrases`, and the string round-trip in
`sumStats`/`sumDiffCounts`. `spell()` reuses the existing `countPhrase`/`plural`
wording so rendered text is byte-identical (golden tests pin it).

**Files:** internal/tui/toolview.go, internal/tui/toolregistry.go,
internal/tui/diffbody.go

**Tests:** unit tests for `add()`/`spell()` on both variants; existing golden tests
unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): carry typed stat values beside tool summaries instead of re-parsing prose`

## 13. Merge the /usage and /inspect panes into one reportPane — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item says usage.go and inspector.go "reduce to row builders plus their pane titles", but its Files line names neither `model.go` nor any test file, and both read those panes by their own names — `View`/`handleKey`/`handleMouseClick` call `renderUsage`, `usageKey`, `usageWheel`, `handleUsageClick` and their /inspect twins, and `usage_test.go`, `inspector_test.go`, `mouse_test.go` and `popup_test.go` call `usageSpec`, `usagePaneRect`, `usageWindow`, `dismissUsage`, `inspectorSpec`, `inspectorRows` and both `*Pane{open: true}` literals. Each pane therefore KEEPS its eight entry names as one-line namings of the shared body (the shape `closeUsagePane` already had in this file), and model.go and every existing test file are untouched. What collapsed is the bodies: nine duplicated functions became one set, and mouse.go lost 237 lines.
NOTES (2026-08-20): `usagePane` and `inspectorPane` are now type ALIASES of `reportPane` rather than two identical structs. That is what lets the Model keep its two field declarations and the tests keep their `usagePane{open: true}` / `inspectorPane{open: true}` literals while there is only one state type to maintain.
NOTES (2026-08-20): `usageScrollStep` moved into `reportpane.go` as `reportScrollStep`. The item's "usageScrollStep stays shared" holds — it is still the one table both panes answer through, and it had no test and two callers — but a `usage`-prefixed name inside the merged module would re-plant exactly the "one pane's copy is the other's source" reading the merge exists to delete.
NOTES (2026-08-20): the merged key contract reads the PAINTER's window (`reportWindow`, which is `usageKey`'s form and the form both wheels already used) where `inspectorKey` read the composed spec's own `rowTop`/`maxRows`. The two agree exactly on every frame that seats at least one row — the spec's top is already clamped to the last full window, and with flat one-line rows the placement's `start`/`end-start` are that top and that grant. They differ only where the frame grants a seated pane ZERO rows with rows to show: nothing of the list is drawn there, and either form's offset is re-clamped the next time the pane is composed. Merge policy's "preserve exactly" is about user-visible behaviour, and there is none to see here.
NOTES (2026-08-20): `frameOverlays.block` — the pane→block lookup the stacking order is walked through — is declared in `reportpane.go` rather than beside its type in `model.go`, which the item's Files line does not name and which needed no edit.
NOTES (2026-08-20): the item's Files line names no test file; the tests it asks for landed in new `internal/tui/reportpane_test.go` (item 11's precedent) — the key contract and the window math driven through the shared functions for BOTH kinds, plus a structural pin that every pane of the transcript-side slot is named exactly once in its stated order and that the /inspect box begins on the row the /usage box closes on.
NOTES (2026-08-20): one prose pointer was corrected in the same commit because this move is what staled it — `doc.go`'s mouse narration said a FOURTH and a FIFTH rectangle are implemented in `mouse.go`; it now says they are one rectangle written once in `reportpane.go`, and names `[Model.reportWindow]` in place of the two per-pane windows.

**Source:** review §Candidate 2.

**What:** Create new `internal/tui/reportpane.go` with one value-typed `reportPane`
owning `{open, top}`, the key contract, dismiss, the render + spec budget path, and the
mouse family (pane rect, visible window, click, wheel) parameterised by what stacks
above — the stacking order stated once, not hardcoded in two `above` slices. usage.go
and inspector.go reduce to row builders (`usageRows` / `inspectorRows`) plus their pane
titles; the eight copied functions in usage.go/inspector.go/mouse.go collapse into the
one set. `usageScrollStep` stays shared. Value type, ADR 0011-safe. doc.go line in the
same commit.

**Files:** internal/tui/usage.go, internal/tui/inspector.go, internal/tui/mouse.go,
internal/tui/reportpane.go, internal/tui/doc.go

**Tests:** existing usage/inspector/mouse tests pass unchanged; add direct unit tests on
`reportPane` keys and window math.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): merge the /usage and /inspect panes into one reportPane module`

## 14. Route the three raw filter buffers through lineEditor — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item's parenthetical groups the glyphs as "picker ▌; settings/sessions ▏", but the /sessions FILTER has always painted `▌` — it goes through the shared `overlayFilterLine`, which appended `pickerFilterCursor`; only the /sessions RENAME row paints `▏`. Merge policy ("each pane keeps its exact current behaviour", design call 2) wins over the parenthetical, so the four fields carry ▌ / ▌ / ▏ / ▏ and the painted filter line is byte-identical (`picker_test.go`'s and `sessions_test.go`'s existing `pickerFilterLead+…+pickerFilterCursor` pins pass unchanged).
NOTES (2026-08-20): the caret glyph became a parameter of the FIELD (a `caret` slot set by `newLineEditor`, read by `textWithCaret()`) rather than staying a parameter of `textWithCaret`, which it already was. That is what puts `settings.go` on the item's Files line: its two paint sites drop the argument, and `newSettingsEditor` becomes a one-line naming of the new shared `newPopupField` (the reportPane precedent from item 13).
NOTES (2026-08-20): only the two keys each pane ALREADY routed to its buffer reach the field — a printable keypress and backspace. Everything else stays swallowed whole by the modal, so ←/→, the word jumps and home/end are still inert inside an overlay filter. Handing the modal's unclaimed keys to the widget's key map would have been the larger deepening, and it would have changed what `TestPickerChordsMoveOrAreSwallowedRatherThanTyped` documents — behaviour preservation is the plan's standing requirement, so the routing was left exactly as it was and only the buffer half was deleted, which is what the item asks for.
NOTES (2026-08-20): the field is built LAZILY, on the first key that reaches it (`typeIntoOverlayFilter`), not at each of the nine sites that open a picker. Those sites assign the whole struct at once (`m.picker = picker{}`, in `picker.go`, `keymigration.go`, `prebound.go`, `schedule.go`) and a zero `lineEditor` is an inert widget that answers "" — so the whole-struct zeroing still clears the filter, and neither `keymigration.go` nor `prebound.go` needed touching. `schedule.go`'s `acceptCycle` is the one PARTIAL reset and now assigns `lineEditor{}` in place of `""`.
NOTES (2026-08-20): 26 test lines changed ACCESSOR FORM only — `m.picker.filter` → `m.picker.filter.value()`, `renameBuf = "x"` → `renameBuf.setValue("x")` — because the field type changed from `string` to `lineEditor`. No test expectation was changed, and no assertion was added, removed or weakened. Two test-local builders (`typedFilter` in picker_test.go, `renamedField` in sessions_test.go) exist for the tests that SEED a filter or a rename buffer instead of typing one, since a field is a widget rather than a string.
NOTES (2026-08-20): `model_test.go`'s `TestPromptScrollReseatCannotSpin` changed one line — its result channel became `chan *outcome`. Three more `textarea.Model` values pushed `Model` from 63,896 to 104,600 bytes and Go refuses a channel element over 64kB. Mechanical, not an expectation change; see the DEFER on the size.

**Source:** review §Candidate 1, Sequencing (prerequisite slice).

**What:** `picker.filter`, `sessionBrowser.filter`, and `sessionBrowser.renameBuf` are
raw string buffers with hand-written rune backspace/append; the package answers "what
does backspace do" in five places and draws a caret three ways. Route all three through
`lineEditor`; delete the per-pane buffer code (including the twice-written
type-to-filter block's buffer half — the filter semantics move in item 15). Per merge
policy, the caret glyph becomes a lineEditor parameter so each pane keeps its exact
current glyph (picker `▌`; settings/sessions `▏`; the prompt keeps `textWithCaret`'s
current look). Do NOT merge lineEditor and promptEditor — that layering is verified
healthy.

**Files:** internal/tui/picker.go, internal/tui/sessions.go, internal/tui/settings.go,
internal/tui/lineeditor.go

**Tests:** existing picker/sessions/settings tests pass unchanged; add lineEditor unit
tests for the caret-glyph parameter.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): route the three raw filter buffers through lineEditor`

## 15. Introduce listSurface; adopt it in picker and sessions; write ADR 0053 — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the surface is EMBEDDED anonymously, which is the plan's own word ("future popup surfaces embed listSurface") and the review's. That is what keeps `m.picker.selected`, `m.sessionBrowser.filter` and `heartbeat.go`'s promoted `m.picker.clampSelection(…)` call reading exactly as before, so roughly fifty test lines and one live call site needed no edit at all. Three KEYED composite literals had to name the embedded value (`listSurface: listSurface{selected: m.currentServerRow()}`) because a keyed literal cannot set a promoted field — one in `picker.go` and two in `prebound.go`, which is outside the item's Files line for that one mechanical reason.
NOTES (2026-08-20): the DECISION's Model-size condition did not fire, and the plan's own text settles why — the review's shape is ONE surface per pane, and each pane already held exactly one filter, so the fields MOVED rather than multiplied: `Model` went from 104,600 to 104,592 bytes (8 bytes smaller, padding). ADR 0053 decision 9 records the rule the later adoptions need: a pane whose sub-lists cannot share one surface names its surfaces as fields and reuses the editor it already has, never embeds several `lineEditor`s.
NOTES (2026-08-20): `popup.go` is on the item's Files line but needed no change. The two filter pads cannot leave `popupSpec`: they are applied inside `renderPopupPlaced` against the WRAPPED body's height and dropped whole when they do not fit, which a caller handing over a body STRING cannot compute. What the item asks for is delivered the other way — `listsurface.go` is now their ONLY setter, derived once from the filter it owns, so no pane sets them and none can set them out of step with the budget claim. Item 17's "remove `popupSpec.bodyPadAbove`/`bodyPadBelow` entirely if item 15 left residue" is left that judgement, which is what its own wording anticipates.
NOTES (2026-08-20): the key ORDER inside `sessionBrowserKey` changed — the shared surface is asked first, so esc/↑/↓/⏎/backspace/printable are tested before ^a/^d/^r where the old switch tested the three chords before backspace and the printable catch-all. Behaviour is byte-identical: none of the three chords' `msg.String()` values collide with the surface's keys, and a modifier chord carries no `Text` (bubbletea's own contract), so nothing can be claimed by a different branch than before. Recorded because it is a reorder of an existing switch.
NOTES (2026-08-20): the browser's rows are now composed ONCE per keypress (`sessionBrowser.unfilteredRows`) where the old code called `m.sessionBrowserView()` twice around a filter edit, each with a fresh `time.Now()`. Same answers on every path; it closes the (theoretical) window where a relative-time cell ticking between the two reads could hand one keypress two different counts. `sessionConfirmKey` and `sessionRenameKey` are untouched — they are modal surfaces within the modal, and no filter is typed inside them.
NOTES (2026-08-20): the moved span's CODE is byte-identical (`pickerView`, `offeringIndex`, `filterPopupRows`, `rowMatchesFilter`, `pickerFilterLead`, `pickerFilterCursor`, `overlayFilterLine`, diffed line-for-line against `HEAD:internal/tui/picker.go`). Four of their doc comments were reworded, and only where the move itself made them false: they described the picker as the owner ("the open kind's offering", "the kind's FULL offering", "both overlays that filter", "every other cell this file composes"). `pickerFilterLine` was deleted rather than moved — `renderList` composes the filter line for every pane, so the picker's one-line naming of `overlayFilterLine` had no caller left.
NOTES (2026-08-20): three live prose pointers were corrected in the same commit because this move is what staled them — `picker.go`'s banner named `sessionBrowser.clampSelection`, which no longer exists; `sessions.go` named `picker.go` as `pickerView`'s home and called `rowMatchesFilter` "the picker's"; and `lineeditor.go`'s caret-field comment named "picker.go's filter" as one of the two fields carrying `pickerFilterCursor`. `lineeditor.go` is outside the item's Files line for that one-clause reason.
NOTES (2026-08-20): the item's Files line names no test file; the tests it asks for landed in new `internal/tui/listsurface_test.go` (items 11 and 13's precedent) — the wrap flag proved at both ends and per flag, the same pair proved again at the bottom of a FILTERED list where the end is where the filter put it, the verdict each key earns, both clamps of a keypress, and the accept resolving through the filter.

**Source:** review §Candidate 1. Depends on item 14.

**What:** Create new `internal/tui/listsurface.go`: one value-typed `listSurface`
(rows + selection + filter + window) owning the key verdicts (↑/↓ with a per-pane wrap
parameter — merge policy — preserving each pane's current answer to "↓ at the bottom"),
`clampSelection`, type-to-filter (keeping `filterPopupRows`' accept-against-filtered
semantics), esc, and the budget→render call (`popupFloor` claim → `popupBudget` → seated
check → `renderPopup`). The filter-line body pads (`popupSpec.bodyPadAbove`/
`bodyPadBelow`, set identically by both callers from `filter != ""`) ride this module,
not popupSpec. Adopt it in picker (all 7 `pickerKind`s) and sessions (all 3 modes); each
pane keeps only rows, accept, and hint. Write `docs/adr/0053-*.md` in the same commit:
future popup surfaces embed listSurface (rows + accept is the marginal cost of a new
pane). doc.go line in the same commit.

**Files:** internal/tui/listsurface.go, internal/tui/picker.go,
internal/tui/sessions.go, internal/tui/popup.go, internal/tui/doc.go,
docs/adr/0053-popup-surfaces-embed-one-list-surface.md

**Tests:** direct unit tests on `listSurface` keys — including "↓ at the bottom of a
filtered list" per wrap flag; existing picker/sessions tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): introduce listSurface and adopt it in picker and sessions`

## 16. Adopt listSurface in settings and autocomplete — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the module was SPLIT rather than adopted whole, and ADR 0053 gained a dated amendment saying so. Its decision 9 forbids multiplying `lineEditor`s, and none of this item's four lists filters — /settings is walked with arrows and answers backspace with an armed reset, the dropdown is narrowed by the token in the chat box under it. Embedding `listSurface` in the two panes would have added three `textarea`s (13,568 bytes each, +40,704 on a `Model` copied every `Update`), which is exactly what decision 9 exists to stop. So `listCursor` ({selected}, 8 bytes) now holds the clamp, the wrap rule, `highlight` and the key contract, and `listSurface` embeds it and adds only the field and the keys that TYPE. Decisions 1 and 8 read differently after that — recorded in the ADR's amendment rather than left to a reader to reconcile.
NOTES (2026-08-20): `picker.go`, `sessions.go` and `prebound.go` are outside the item's Files line and were touched for that split only: three keyed composite literals had to name the now-nested value (`listSurface{listCursor: listCursor{selected: …}}`), the two render call sites moved to `renderFilterList`, and four prose pointers went stale in the same move (`picker.go`'s `[listSurface.clampSelection]` and its two `renderList` mentions, `sessions.go`'s one, and both panes' field comments claiming the surface is "the state EVERY list overlay keeps"). `m.picker.selected`, `m.picker.filter`, `m.sessionBrowser.*` and `heartbeat.go`'s promoted `clampSelection` call all still read exactly as before — Go promotes through both levels — so no other line of either pane changed.
NOTES (2026-08-20): `listCursor.highlight` takes the row COUNT where `listSurface.highlight` took the rows. That is what let `settingsSelection` — the pane's own clamp-to-highlight copy, six callers — become a one-line naming of the shared one: the /settings key list clamps against the SETTING rows and paints the display rows that interleave section headers, so a `[]popupRow` parameter could not have answered for it. The three call sites that had rows (`picker.go`, `sessions.go`, one test) pass `len(rows)`.
NOTES (2026-08-20): `settingsPane` embeds the key list's cursor (so `m.settings.selected` is unchanged at all 38 sites, 32 of them tests) and NAMES the sub-list's `sub` — ADR 0053 decision 9's shape. 25 references became `m.settings.sub.selected`, nine of them in `settings_test.go`; accessor form only, no expectation changed, nothing added, removed or weakened. `m.settings.sub = 0` became `= listCursor{}` at the eight reset sites.
NOTES (2026-08-20): `listContent` gained four fields — `body`, `bodyLead`, `bodyPad`, `menuRows` — because the /settings sub-lists have a body block of their own (the key's question) where the filtering panes' body IS their filter line. `renderList` therefore takes the block as stated, and the new `renderFilterList` is the six lines that fill it from a filter before delegating. ADR 0053 decision 6 is unchanged: the line, its label and its two pads are still written in exactly one place.
NOTES (2026-08-20): two guards were added where today's code would panic in a state neither pane can reach — ⏎/space over an EMPTY mechanism catalogue (`toggles[clampInt(sub, 0, -1)]`) and ⏎ over an empty enum vocabulary. Both targets (`settingsMechanismTarget`, `settingsEnumTarget`) already refuse an empty list, so nothing observable changes; the shared contract simply answers `listSwallowed` for a ⏎ with no row to take.
NOTES (2026-08-20): `renderAutocomplete` now clamps the highlight it hands the painter (`ac.highlight(len(rows))`) where it passed `ac.selected` raw. Every writer of that field (`computeAutocomplete`, `reselectRow`) already leaves it in range, so this is defensive only — but it is the one line of the dropdown's render that is not byte-for-byte the old call.
NOTES (2026-08-20): the item's Files line names no test file; the tests it asks for landed in `internal/tui/listsurface_test.go` (items 11, 13 and 15's precedent) — the cursor handing each typing key back per key, and the surface claiming those same keys out of what the cursor returns, which is the pair the split has to keep apart.

**Source:** review §Candidate 1. Depends on item 15.

**What:** Move the three settings sub-lists (the three wrap-arrow pairs) and
autocomplete's two variants onto `listSurface`, deleting their copies of the wrap
idiom, clamp, and budget→render boilerplate. Wrap behaviour preserved per pane (merge
policy).

**Files:** internal/tui/settings.go, internal/tui/autocomplete.go,
internal/tui/listsurface.go

**Tests:** existing settings/autocomplete tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): adopt listSurface in settings and autocomplete`

## 17. Adopt listSurface for the approval and ask selections — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the two panes take the `listCursor`'s WALK (`move`, `clampSelection`, `highlight`) and NOT its `key` contract, which the item's "move the non-wrapping arrow variants onto listSurface" is read as asking for and the merge policy requires. Routing `key` here would have CHANGED behaviour: it claims esc, ⏎ and ^p/^n on top of ↑/↓. esc and ⏎ are claimed upstream in `handleKey` before either pane is reached (two dead branches), and ^p/^n reach both panes today and go elsewhere — to `scrollViewport` under the approval prompt, to the textarea under the question. `TestModelApprovalIgnoresOtherKeys` states the contract in its own prose: "the menu claims those three and nothing else". So each pane keeps its own switch over WHICH keys it claims, and the per-pane choice is documented at both call sites and in `listCursor`'s doc (merge policy, design call 2).
NOTES (2026-08-20): `model.go` is outside the item's Files line and had to change — the two selections ARE `Model` fields (`askSel`, `approvalSel`, the review's own "model.go (ask pane)"), so the type change and the `m.approvalSel = listCursor{}` reset land there. The field NAMES are untouched: neither pane has a struct of its own to embed the cursor in, so they name it, which is decision 9's shape one step further and is written into ADR 0053 as a dated adoption note (also outside the Files line, for that one reason — decision 7 says "embedded rather than named" and would otherwise read false for these two).
NOTES (2026-08-20): the item's conditional "remove `popupSpec.bodyPadAbove`/`bodyPadBelow` entirely if item 15 left residue" did NOT fire (user's decision, 2026-08-20): both fields are live, set from `listContent.bodyPad` with `renderFilterList` as their sole setter, and item 15's own notes record why they cannot leave `popupSpec` (they are spent against the WRAPPED body's height inside `renderPopupPlaced`). `popup.go` is therefore on the item's Files line and unchanged.
NOTES (2026-08-20): two clamps became slightly MORE defensive, in the direction item 16 already took. The ask arrows were `if m.askSel > 0 { m.askSel-- }` / `if m.askSel < n-1 { m.askSel++ }`, which move an out-of-range selection by one instead of clamping it into range; `move` clamps first. Nothing observable changes — the field is set to zero on every fold and only ever moved within range — and every in-range answer is byte-identical. Likewise `min(max(sel, 0), n-1)` became `highlight(n)`, which is the same arithmetic plus the −1 the empty case already guarded for by hand (the ␣ toggle's `sel >= 0`).
NOTES (2026-08-20): the accessor form changed in `model_test.go` (11 sites) and `recall_test.go` (4 sites) — `m.askSel` → `m.askSel.selected` — which is item 16's precedent (a field's type changed, so its readers name the field inside it). No expectation was changed, weakened, added or removed. The item names no test file and needs no new test: the shared walk is already proved per wrap flag at both ends in `listsurface_test.go` (item 15), and both panes' own arrow behaviour stays pinned by `TestModelApprovalArrowsClampWithoutWrapping` and the recall test's ask arrows.
NOTES (2026-08-20): four prose claims in `listsurface.go` and one in `doc.go` were corrected in the same commit because this adoption is what would otherwise have made them false — the file banner's "It is MODAL" bullet (true of the list overlays, not of the two soft-modal panes), `listCursor`'s "a pane EMBEDS it", `key`'s "the key contract EVERY list in the package shares", and `listWrap`'s adopter list, which still named only the picker and the browser as wrappers after item 16 moved /settings and the dropdown onto `listWrapsAround`.

**Source:** review §Candidate 1. Depends on items 3 and 15.

**What:** Move approval.go's and the ask pane's non-wrapping arrow variants onto
`listSurface` with wrap=false (their current behaviour, preserved). The /usage and
/inspect panes stay on `reportPane` (item 13) — do not force listSurface there. Remove
`popupSpec.bodyPadAbove`/`bodyPadBelow` entirely if item 15 left residue.

**Files:** internal/tui/approval.go, internal/tui/ask.go, internal/tui/listsurface.go,
internal/tui/popup.go

**Tests:** existing approval/ask tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): adopt listSurface for the approval and ask selections`

## 18. Merge the settings twins — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the buffer/text merge parameterizes the pair slightly differently from the item's "commit-key and trim parameters". The commit KEY is claimed by each state before the shared router is reached (`settingsBufferKey` takes ⏎, `settingsTextKey` takes ctrl+s) rather than passed down to it, and the trim arrives as the already-trimmed VALUE plus the state's own blank verdict rather than as a function parameter. Reason: the two states disagree on the trim AND on what "empty" is (`value == ""` against `strings.TrimSpace(value) == ""`, which part ways on a field cleared to spaces), so preserving both exactly (merge policy) would have cost two func-valued parameters to save two call-site lines — and a key claimed at the call site is where the merge policy asks the per-pane difference to be documented anyway.
NOTES (2026-08-20): `settingsEditKey` takes one parameter the item does not name — `relayout` — because the two key routers disagree on a second thing besides the commit key: the multi-line field IS the pane's row list and lays the frame out again on every edited character, where the one-line buffer is one cell of one row and does not. Both behaviours are preserved exactly.
NOTES (2026-08-20): all four twin names are KEPT as one- and two-line namings of the shared pair instead of being deleted (item 16's "a one-line naming of the shared one" precedent). Three prose pointers outside the item's Files line name them — `lineeditor.go`'s "settingsCommitBuffer's TrimSpace", `listsurface.go`'s "(settingsBufferKey)", `mouse.go`'s "(renderSettingsEnum)" — so keeping the names is what let this item touch exactly the one file its Files line allows.
NOTES (2026-08-20): the DECISION's reading held — item 16 had already collapsed the two renderers to their row-building loop and their hint, so the "content parameter" is exactly those two arguments (`renderSettingsSubList(row, values, hint)`); no third difference was left to merge.

**Source:** review §Candidate 12 (twin merges). Depends on item 16.

**What:** Two 25-30-line function pairs in settings.go are written twice: merge
`renderSettingsEnum` / `renderSettingsMechanisms` behind a content parameter (item 16's
listSurface adoption may have already collapsed most of this — finish whatever remains),
and merge `settingsBufferKey`/`settingsCommitBuffer` with
`settingsTextKey`/`settingsCommitText` behind commit-key and trim parameters (⏎ +
TrimSpace vs ctrl+s + TrimRight, both preserved exactly — merge policy).

**Files:** internal/tui/settings.go

**Tests:** existing settings tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): merge the settings enum/mechanism and buffer/text twins`

## 19. Split settings.go along its surface seams — ✅ DONE (2026-08-20)

NOTES (2026-08-20): both moved spans are byte-identical, diffed line-for-line against `HEAD:internal/tui/settings.go`; `settings.go`'s own diff is 675 deleted lines and zero added ones (669 moved plus the six import lines the move made unused — `errors`, `fmt`, `os/exec`, `time`, and the `domain` import). The only text in either new file that did not travel is its `package`/`import` clause and its header banner, following items 5, 7, 8 and 9's precedent, since every sibling file in the package carries one.

NOTES (2026-08-20): the item names five clusters for two files; the boundaries drawn are the review's own spans widened at three places, each because leaving the piece behind would have split a chain across the two files. `settingRowOf` rode with the watcher (the review's watcher span stops one function short of it) — its own doc calls it "the lookup both halves of the round trip need" and its three callers are all in the moved clusters. `applyReloaded` likewise sits between the two moved clusters at the pin and is called only from them. And `settingsWrite`/`settingsPersist`/`settingsApplied` — the persist step between the armed reset and the live-apply router the item does name — moved with them, because `settingsApplied` IS the join of two of the three named clusters (it calls `settingsApplyLive` and `recordSettingEdit`) and leaving it behind would have put the pipeline's middle in one file and both its ends in the other.

NOTES (2026-08-20): the `settingKey*` const block moved too. It sits inside the moved contiguous span and its doc is written from the apply's side ("the renderer-owned keys settingsApplyLocal puts into effect itself"), but it is genuinely shared — four of its readers are pane-core (`settingsEnter`, `settingsToggleMechanism`, `settingsVocabulary`, `settingsMechanisms`) and one is `colorscheme.go`. Same package, so no call site changed either way.

NOTES (2026-08-20): the armed reset's TARGET and PREDICATES (`settingsResetTarget`, `settingsResettable`, `settingsResetKind`) stayed in `settings.go` rather than following the three verbs. They sit inside the pane's target/predicate group and are written as the parallel of `settingsEnumTarget`, `settingsMechanismTarget`, `settingsBufferTarget` and `settingsBufferable`, which all stay — moving three of nine would have fragmented a group the pane core reads as one.

NOTES (2026-08-20): `model.go` is outside the item's Files line and two of its comments were corrected in the same commit because this move is what staled them — the `settingsEditedMsg` and `configChangedMsg` Update arms both pointed a reader at `(settings.go)` for the fold they call, and both folds are now in `settingswatcher.go`. Nothing else in the repo carries a live pointer at the moved code: every other `settings.go` mention in source or docs names the pane, its rows or its keys, which did not move.

**Source:** review §Candidate 12 (file split). Depends on item 18.

**What:** Pure same-package moves out of settings.go into two new files: the config
watcher + external-editor clusters into `internal/tui/settingswatcher.go`; the
live-apply router + edit journal + armed-reset clusters into
`internal/tui/settingsapply.go`. The pane core stays in settings.go. The display
projection stays in `cmd/apogee` (ADR 0035/0037) — untouched. doc.go gains two lines,
same commit.

**Files:** internal/tui/settings.go, internal/tui/settingswatcher.go,
internal/tui/settingsapply.go, internal/tui/doc.go

**Tests:** existing suite only (pure move).

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): split settings.go along its surface seams`

## 20. The frame publishes its geometry once — ✅ DONE (2026-08-20)

NOTES (2026-08-20): moved `transcriptSlotPanes` from reportpane.go to model.go beside `View`, per the item's "stacking order is stated once, in View", carrying View's per-pane placement prose onto the list; `frameOverlays.block` stays in reportpane.go.

NOTES (2026-08-20): two files beyond the item's list — doc.go (its map claimed the slot's order is stated in reportpane.go) and mouse_test.go (two added guard tests: published spans equal a fresh composition, and the click chain never carries its frame back to Bubble Tea). No existing test changed.

**Source:** review §Candidate 3. Depends on item 13.

**What:** `View`'s composer knows every block's row span while composing and discards
it; three near-identical `*PaneRect` functions in mouse.go reconstruct the prefix sum.
Make the composer return the frame string together with each block's `[y0, y0+h)` span
(a plain value on the Model — ADR 0011-safe); painter and mouse read the same value. The
`*PaneRect` prefix sums (settings, and reportPane's post-item-13 version) become span
lookups; the per-mouse-event repeated `frameOverlays()` calls collapse to reads of the
published spans. Stacking order is stated once, in `View`. Transcript-side hit-testing
already reads the painter's `lineTargets` — leave it alone.

**Files:** internal/tui/model.go, internal/tui/mouse.go, internal/tui/reportpane.go

**Tests:** the mouse hit-test suite passes unchanged (it pins every rect).

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): publish frame block spans from View instead of re-deriving pane rects`

## 21. Every Update arm delegates; the key-claim order becomes data — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item's "~10+ lines" was drawn on the arm as PRINTED — its narration plus its body — which is how the review measures the arms it names (`askReqMsg` 721-746, `compactDoneMsg` 833-855, `spinnerTickMsg` 949-971). Twelve arms qualify and were converted: `tea.ModeReportMsg`, `tea.KeyboardEnhancementsMsg`, `tea.PasteMsg`, `approvalReqMsg`, `routingNoticeMsg`, `cancelledMsg`, `errMsg`, `compactDoneMsg`, `spinnerTickMsg`, `beatMsg`, `tea.MouseWheelMsg` and `default`. The twelve arms under the threshold (`tea.WindowSizeMsg`, `ctrlCResetMsg`, `eventMsg`, `scheduleEventMsg`, `presentedMsg`, `exchangeDoneMsg`, `interjectedMsg`, `skillsReloadedMsg`, `skillsRescannedMsg`, `recallLoadedMsg`, `heartbeatTickMsg`, `flashClearMsg`) stay inline, and the fifteen that already delegate were not touched — they are the shape being applied.

NOTES (2026-08-20): each converted arm's narration moved onto its fold's doc comment and the arm keeps a two-line summary naming the file the fold lives in, which is the shape the fifteen delegating arms already had. That is what the review's "Update ~477 → ~120" measures; carrying the full prose along to the arm would have shrunk nothing. `Update` is 262 lines and `handleKey` 192. Every moved BODY is byte-identical: a normalised diff of the whole change shows no removed code line that does not reappear, except the seventeen lines of `handleKey`'s guard block, which is the half the item replaces with data.

NOTES (2026-08-20): `internal/tui/ask.go` is on the item's Files line but needed no change — the ask arm's fold (`foldAskRequest`) landed with item 3. `askChoiceKey` deliberately stays where item 3 put it, below the two `msg.String()` switches, and is NOT in the claimant list: the list is walked before the frame's own verbs, so hoisting the ask's choice keys into it would change which surface claims a key, and the plan forbids that.

NOTES (2026-08-20): seven claimants, not the review's eight. The pre-switch block holds seven guards today — sessions browser, settings pane, picker, autocomplete overlay, /usage report, /inspect pane, block cursor — and all seven are in `keyClaimOrder` in exactly that order, with each guard's own comment as its entry's documentation. The state gate each guard carried in its `if` is the entry's `open` field; a nil `open` is a surface whose own key contract is the whole test, which is the two report panes' shape and the block cursor's.

NOTES (2026-08-20): `foldCancelled` takes no message. `cancelledMsg` carries no fields the arm read, so the arm reads `return m.foldCancelled()` — the shape `m.foldSkillsReloaded()` already had in this switch — rather than passing a value nothing looks at.

NOTES (2026-08-20): files beyond the item's line, each for one reason the change itself created — `width.go`, `prompteditor.go`, `approval.go`, `commandrun.go`, `spinner.go` and `mouse.go` are the concern files receiving folds, which the item's Files line asks the implementer to enumerate; `doc.go` had one stale clause (its prompt-legend narration said the `tea.KeyboardEnhancementsMsg` arm in `model.go` sets the disambiguation flag — the fold that sets it is in `prompteditor.go`); and the tests the item asks for landed in new `internal/tui/keyclaim_test.go` (items 11, 13 and 15's precedent), which is a concern-named test file like `mode_test.go` and `seam_test.go` beside it. No existing test changed.

**Source:** review §Candidate 8. Depends on items 2 and 3.

**What:** The six 3-line Update arms that delegate to a named fold are the winning
shape; apply it to every inline arm of ~10+ lines (`compactDoneMsg`, `spinnerTickMsg`,
the ask arm now in ask.go, …): each becomes `return m.foldX(msg)` with the fold living
in its concern file. In `handleKey`, the eight sequential "does overlay X claim this
key?" guards become an ordered list of claimants — data, not hand-written ifs — with the
order preserved exactly (it is load-bearing; the existing ~70 lines of comment become
the list's documentation).

**Files:** internal/tui/model.go, internal/tui/ask.go, internal/tui/heartbeat.go, plus
the concern files receiving folds (implementer enumerates the inline arms; scope is
bounded to `Update` and `handleKey` plus fold destinations)

**Tests:** existing suite passes unchanged; add a unit test that the claimant list's
order matches the documented precedence.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): delegate every Update arm to concern folds and table the key-claim order`

## 22. The command table absorbs its satellite lists — ✅ DONE (2026-08-20)

NOTES (2026-08-20): `touchesServer` is carried by the FOUR verbs that switch or actuate the server (/model, /server, /stop-server, /unload-model), not by all six names the latch used to list, and `actuationBlocked` reads `spec.opensExchange || spec.touchesServer`. Stating the /continue and /compact pair on two flags would be a fact one row could lose without the other noticing, and deriving it is what makes a future Exchange-opening verb latched by the one flag it already declares — which is the drift the item exists to end. The six blocked names are unchanged and are now pinned by name in `TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs`.

NOTES (2026-08-20): the item's structural test — "every arg-taking spec has a `parseArgs` hook" — cannot hold on the table as it stands, and the true statement was pinned instead. Four of the eight `takesArgs` verbs have no grammar of their own: /model, /server and /rename read the plain token list, and /schedule reads the line's RAW TAIL, which a `func([]string) (any, error)` hook cannot produce at all. `TestOnlyTheGrammarVerbsCarryAParseArgsHook` pins what is real — a hook only ever sits on a row the parser hands arguments to, every hook words its BARE form as a report rather than an error (which is what lets `verbArgsOf` answer a zero value for a line the hook never ran for), and the set carrying one is named, exactly the switch that was deleted, so a fifth grammar is a deliberate edit at that line rather than a silent one.

NOTES (2026-08-20): 14 test lines changed ACCESSOR FORM only — `got.confine` → `verbArgsOf[confineArgs](got)` and its three siblings, in `command_test.go` and `undo_test.go` — because the four typed fields became one opaque value. No expectation was changed, weakened, added or removed, and no existing test file gained a test.

NOTES (2026-08-20): `doc.go` was deliberately left untouched. Its command.go narration calls /confine "one of the two argument-taking verbs with a grammar of its own (/color-scheme is the other)", which is a count staled by the items that added /effort and /undo, not by this move — everything the sentence says about how a grammar is READ is still true. Pre-existing debt this change merely makes easier to see (the table now declares four hooks), so it is reported rather than fixed here.

**Source:** review §Candidate 10 + Appendix B (the literal-scatter map).

**What:** Fold three behavioural policies onto `commandSpec`: `opensExchange` (replaces
commandrun.go's `continue`/`compact` literal gate), `touchesServer` (replaces
actuation.go's hardcoded six-name latch list), and a
`parseArgs func([]string) (any, error)` hook (replaces command.go's second grammar
switch); `parsedInput` carries one opaque args value instead of one typed field per
arg-taking verb (`confine`, `colorScheme`, `effort`, `undo`). `runCommand`'s dispatch
switch stays. Behaviour of all 21 verbs preserved exactly; Appendix B is the checklist
of literals to delete.

**Files:** internal/tui/command.go, internal/tui/commandrun.go,
internal/tui/actuation.go

**Tests:** `TestCommandSpecsReadAlphabetically` still pins ordering; add a structural
test that every arg-taking spec has a `parseArgs` hook; existing verb tests pass
unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): fold exchange/server/grammar policies into commandSpec`

## 23. entryKind answers for itself, with a completeness test — ✅ DONE (2026-08-20)

NOTES (2026-08-20): moved the `entryKind` type and its const block out of transcript.go into the new entrykind.go — the item named only the table. The item's own claim that "enum row + painter case remain the two edit points for a new kind" only holds when the const row and its table row sit side by side, and the house rule puts a type in the file that bears its name.

NOTES (2026-08-20): the table carries two columns beyond the four the item names — `hasLiveStar` and `isUserPrompt`. Neither of the four covers render.go's tail classification (`e.kind == entryToolCall && !e.done`, `e.kind == entryUser`), which the item lists among the six predicates to collapse.

NOTES (2026-08-20): `hasBlockState(kind)` is gone, replaced by the column name the item binds (`kind.carriesBlockState()`); its call site in schedule_test.go and three prose references were respelled. No test expectation changed.

NOTES (2026-08-20): added a second small guard, `TestEntryKindPersistedNamesAreUnique`, beyond the completeness test the item asks for — the decode-side inverse map is only faithful while no two kinds claim one wire string.

**Source:** review §Candidate 11 + Appendix C (the decision-site map).

**What:** Create new `internal/tui/entrykind.go` with a behaviour table on the kind —
`persistedName`, `carriesBlockState`, `isHostNote`, `cacheable` — collapsing the six
kind-keyed predicates (transcript.go's `isHostNote`/`hasBlockState`,
transcriptcodec.go's name map + inverse, paintcache.go's `cacheable`, render.go's tail
classification). Add a fold-style completeness test mirroring
`TestFoldEventCoversEveryEventVariant` so an unanswered kind fails structurally. The
paint switch in render.go stays a switch (enum row + painter case remain the two edit
points for a new kind). The codec's documented unknown-kind degrade path is untouched.
doc.go line in the same commit.

**Files:** internal/tui/entrykind.go, internal/tui/transcript.go,
internal/tui/transcriptcodec.go, internal/tui/paintcache.go, internal/tui/render.go,
internal/tui/doc.go

**Tests:** the new completeness test; existing codec/paint tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): give entryKind a behaviour table with a completeness test`

## 24. Name the sub-agent run-head predicates — ✅ DONE (2026-08-20)

NOTES (2026-08-20): `internal/tui/toolview.go` is outside the item's Files line, for the one reason the change created — the codec's "third derivation" is asked of a decoded `toolView`, before any entry exists, so the NAME half of the rule became `toolView.headsRun()` and lives in the file that bears the type's name (item 23's house rule); `entry.headsRun()` is the kind plus that card rule, so the two can never disagree about what a delegation is.

NOTES (2026-08-20): the three predicates are methods on `entry` and stayed in `transcript.go` — where the promoted `headsSubAgentRun` already lived and where `entry` is declared — rather than moving beside `subAgentToolName` in `subagentblock.go`: the const is read from `transcript.go` today already, and the scrollback model and the codec must not read a RENDERER file for a fact they both ask.

NOTES (2026-08-20): the `!done`-vs-phase distinction is stated on `opensRun` (why openness reads the call's own pairing), and `subAgentReported`'s doc — the site the review cites — was left as written, because it explains why THAT predicate reads the phase and is the other question's own home. The two now name each other, so the distinction is remembered once per question instead of at each call site.

NOTES (2026-08-20): two sites keep a conjunct of their own on purpose. `subAgentGist` still stops its walk at the most recent OPEN tool call whatever tool it is and only then asks `e.headsRun()` — folding it into `opensRun()` would walk PAST a non-delegation and change the answer. `transcript.continuesOpenRun` still reads `subAgentHeads(entries, head) && !entries[head].done` because it asks the open-head question of a POSITION that may be -1, which is the bounds-checked form `subAgentHeads` exists for.

NOTES (2026-08-20): the item's Files line names no test file; the three predicates' unit tests landed in the existing `transcript_test.go` (`TestRunHeadPredicates`), beside the code they pin, per the `{source}_test.go` convention rather than as a new file. No existing test expectation changed.

**Source:** review §Candidate 11, "Related (do together)". Depends on item 23.

**What:** The predicate "is this a sub-agent run head" is spelled inline 12 times with
varying conjuncts while the named `headsSubAgentRun` has one caller — extracted at the
wrong granularity. Promote to the questions the sites actually ask — `headsRun()`,
`opensRun()`, `headsRunFor(callID)` — and replace the inline spellings (sites mapped in
the review: transcript.go ×5, subagentblock.go ×6, usage.go, transcriptcodec.go's third
derivation). The `!done`-vs-phase distinction currently documented at one call site
moves to the predicates' doc comment — remembered once.

**Files:** internal/tui/transcript.go, internal/tui/subagentblock.go,
internal/tui/usage.go, internal/tui/transcriptcodec.go

**Tests:** existing suite passes unchanged; add unit tests for the three predicates.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): name the sub-agent run-head predicates`

## 25. Options: settings and scheme funcs become named interfaces; write ADR 0054 — ✅ DONE (2026-08-20)

NOTES (2026-08-20): `SchemeHost.Resolve` gained a third return (`ok bool`) that the old `Options.ResolveScheme` func did not have. It is what preserves the per-member nil-seam degrade the item's "audit every call site's nil check" requires: `errNoSchemeResolver` ("the new scheme applies at the next start") is reachable for a host that CAN list but cannot resolve, which is a state `TestSettingsPaneSaysASchemeSwitchNeedsAResolver` pins — it needs the sub-list populated in order to answer it, so a whole-family nil could not express it. Production always answers `true` (the load is forgiving). Every other member's unwired answer fitted its existing signature: no rows, an error out of Write/Reset/Export, and an Apply that reports nothing.

NOTES (2026-08-20): one contract nuance the regrouping moves, recorded because it is a difference in a degrade rather than only a refactor — a host that is WIRED but whose Apply does nothing now reports "already in effect", so a committed `mode` updates the footer's mirror, where a nil `ApplySetting` func returned before that line. Production wires all four acts and no test exercises it; the honest statement of the family-level contract is that a host claiming success is taken at its word, which is what `SettingsHost.Apply`'s doc and ADR 0054 decision 3 now say.

NOTES (2026-08-20): both adapters live in `cmd/apogee/wire_options.go`, beside the projection that hands them over, rather than one per named file — `wire_settings.go` owns the live holder, the apply dispatcher and the per-model re-resolution, and none of those is the renderer-facing seam. It is on the item's Files line for the two prose pointers this rename staled (`applySettingFor`'s doc).

NOTES (2026-08-20): three files outside the item's Files line were touched, each for one pointer this change staled — `internal/tui/{commandrun,keymigration,model,doc}.go` and `cmd/apogee/{wire,doc}.go` name the seams in prose, and `docs/adr/0037` names `Options.ApplySetting` in a live pointer sentence (item 8's LIVE pointer vs HISTORICAL record boundary: the CHANGELOG's past entries, the archived plans and the pinned review were left as written).

NOTES (2026-08-20): no new test file. The item's "per-family fakes replace whole-Model construction" landed as `fakeSettingsHost` (settings_test.go) and `fakeSchemeHost` (colorscheme_test.go), used at every wiring site there is — 22 settings, 3 scheme; a pane test still opens a real pane, because that is what it asserts about. Every changed test line is wiring FORM or a failure MESSAGE naming the seam it drives: no expectation was changed, added, removed or weakened, except `wire_test.go`'s "the resolve seam is wired" assertion, which moved from a nil-func check onto the new `ok` return because that is where the same claim now lives.

**Source:** review §Candidate 9.

**What:** `Options` carries ~30 one-purpose bare func fields; the same file already
proves the deep shape (`Engine`, `SessionHost`, `SkillCatalog`, `RecallHost`,
`Scheduler`). Fold the settings func family (`WriteSetting`/`ResetSetting`/
`ApplySetting` + its fourth member) into a named `SettingsHost` interface and the
scheme family (×3) into `SchemeHost`, beside the existing five. The nil-means-unwired
contract each family assumes is preserved (a nil interface replaces nil funcs — audit
every call site's nil check). Production wiring adapts in `cmd/apogee`'s
`wire_settings.go` / `wire_options.go`; tests get per-family fakes. Write
`docs/adr/0054-*.md` in the same commit: host capabilities join Options as named
interfaces per family, not bare funcs.

**Files:** internal/tui/tui.go, internal/tui/settings.go, cmd/apogee/wire_settings.go,
cmd/apogee/wire_options.go, docs/adr/0054-options-groups-host-capabilities-into-named-interfaces.md

**Tests:** per-family fakes replace whole-Model construction where these families are
exercised; `go test ./cmd/apogee` covers the wiring.

**Acceptance:** `go build ./... && go test ./internal/tui ./cmd/apogee`

**Commit:** `refactor(tui): fold the settings and scheme option funcs into named host interfaces`

## 26. Options: server and heartbeat funcs become ServerHost — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item's "nil-means-unwired preserved; audit every call site" could not land as item 25's shape alone. Four of the six acts are decided ABOUT before they are performed — `Heartbeat != nil` armed the tick chain in `newModel` and gated `blockedUpstream`, `Rebind != nil` decided whether an observed change was CAPTURED at all and worded `/model`'s "the display is read-only" rung, `SwitchServer != nil` refused `/server` up front (`TestServerCommand…/no servers configured/unwired seam` pins that an unwired switch and an empty list are ONE sentence), `BindServer != nil` gated the pre-bound picker and `noBindSeamNote`. Calling to find out would BE the act, so the interface carries `Acts() ServerActs` (four bools; zero value = unwired, which is also what a nil host answers) and each old nil check became one flag read. `List` and `RecordChoice` need no flag: no servers and `false` already ARE their degrades. Production answers all four true.

NOTES (2026-08-20): `docs/adr/0054-*.md` was edited outside the item's Files line — decision **3a** records the "reported, not attempted" rule above, and one consequence line names `ServerHost` as what it was written from. It is the record item 27 (LauncherHost) follows, and its decision 3 as written ("expressed where the act is") would otherwise be the only guidance for a family whose acts cannot say it.

NOTES (2026-08-20): the adapter lives in `cmd/apogee/wire_server.go`, as the item's Files line says, rather than in `wire_options.go` beside item 25's two (ADR 0054 decision 4). It holds the wiring itself instead of what the acts need, because every act already IS a verb on the wiring (`wire_verbs.go`: `beat`, `rebind`, `switchServer`, `bindServer`, `recordServerChoice`) — none of them moved, and the value is only where the renderer's six names meet them.

NOTES (2026-08-20): eleven files outside the item's Files line carry one changed prose pointer each — `internal/tui/{doc,model,settings,prebound,listsurface}.go` and `cmd/apogee/{doc,upstream,wire_live,wire_settings,wire_options}.go` name these seams in prose. `docs/adr/0028` names `Options.Heartbeat` / `Options.Rebind` / `Options.SwitchServer` inside its own Decision narration and was left as written (item 8's LIVE pointer vs HISTORICAL record boundary), as were the CHANGELOG's past entries and the pinned review.

NOTES (2026-08-20): no new test file. The per-family fake is `fakeServerHost` (picker_test.go) — one func per act, the documented unwired answer for any act a test leaves nil, and `Acts()` derived from which funcs are wired, so one nil func is one provable per-member degrade. It is reached through `serverSeams(&opts)`, which finds the host already on the Options instead of replacing it (`wireHeartbeat` adds a beat over a list its caller wired); `cmd/apogee` gets `serverActsOf` for its "is it wired" assertions. Every changed test line is wiring FORM or a failure MESSAGE naming the seam: no expectation was changed, added, removed or weakened.

**Source:** review §Candidate 9. Depends on items 2 and 25.

**What:** Fold the server family (`Servers`/`SwitchServer`/`BindServer`/
`RecordServerChoice`) and the `Heartbeat`/`Rebind` pair into a named `ServerHost`
interface, following item 25's pattern and ADR 0054. Nil-means-unwired preserved;
wiring adapts in `cmd/apogee/wire_server.go`; per-family fake in tests.

**Files:** internal/tui/tui.go, internal/tui/heartbeat.go, internal/tui/picker.go,
cmd/apogee/wire_server.go

**Tests:** per-family fake; existing heartbeat/picker tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui ./cmd/apogee`

**Commit:** `refactor(tui): fold the server and heartbeat option funcs into a ServerHost interface`

## 27. Options: the launcher family becomes LauncherHost — ✅ DONE (2026-08-20)

NOTES (2026-08-20): `LauncherActs` carries exactly ONE flag where `ServerActs` carries four, and the interface's own doc says why. Six of the seven acts say their degrade in their own answer (ADR 0054 decision 3): the four verbs with `ErrNoLauncher` — already their answer for the integration being switched off — and `RecordProfile` with `false, nil`, while `Enabled` is a report rather than an act. `Restore` is the one that cannot: `restoreCmd` decides whether to ISSUE the start-up Cmd at all, and "nothing to restore" is a decision the host MADE rather than a statement that it makes none. `TestStartupRestoreIsSilentWhenUnwired` pins that boundary (a launcher-wired session whose restore member is absent issues no Cmd, so `Init`'s batch collapses to what it held before the feature existed), so the flag is what keeps that expectation unchanged.

NOTES (2026-08-20): `Model.startServerActuation` lost its `act` parameter and now takes the verb alone, resolving the seam through the new `Model.endpointActuation` — the two endpoint verbs are members of one host rather than two fields a caller can hand over, and `commandrun.go` cannot name a method on a possibly-nil interface. The nil branch it guards is unchanged in meaning (a host that is not there answers the same sentence as one switched off) and the three test call sites changed in wiring form only.

NOTES (2026-08-20): `Enabled`'s old "nil ⇒ unknown, let the verbs through" is now stated as the ANSWER a host that cannot tell gives — true — because a wired interface always has the method. `launcherOff()` reads `Launcher != nil && !Launcher.Enabled()`, so the nil host, the host answering false and the host answering true all behave exactly as their func-shaped predecessors did; `fakeLauncherHost` answers true for a nil member, which is what keeps `TestUnloadAndStopWithTheLauncherSwitchedOff` and its unwired sibling apart.

NOTES (2026-08-20): the adapter lives in `cmd/apogee/launcher.go`, as the item's Files line says, and holds the wiring (`launcherHost struct{ w *rootWiring }`, the `serverHost` posture) rather than seven closures — six acts are `launcherWiring`'s own methods and the seventh is `wire_verbs.go`'s `recordLaunchProfile`, none of which moved. `ADR 0054` was NOT edited: item 27 introduces no new rule, it follows decision 3a, which item 26 wrote for it.

NOTES (2026-08-20): six files outside the item's Files line carry the call sites and one changed prose pointer each — `internal/tui/{actuation,picker,commandrun,doc}.go` hold every reader of these seams, and `cmd/apogee/{doc,wire_live}.go` name them in prose. `docs/adr/0048` names two of the renamed seams in its live **Realisations** pointer list and was updated for that reason (item 8's LIVE pointer vs HISTORICAL record boundary); `docs/adr/0029`'s `LoadProfile` mentions are the LIBRARY's own API name and were left as written, as were the CHANGELOG's past entries, the archived plans and the pinned review.

NOTES (2026-08-20): no new test file. The per-family fake is `fakeLauncherHost` (actuation_test.go) — one func per act, the documented unwired answer for any act a test leaves nil, and `Acts()` derived from whether the restore member is wired, so one nil func is one provable per-member degrade. It is reached through `launcherSeams(&opts)`, `serverSeams`' twin, which finds the host already on the Options instead of replacing it. Every changed test line is wiring FORM or a failure MESSAGE naming the seam it drives; the single collapse is `wire_test.go`'s five-member "is it wired" map, which becomes one non-nil-host check — the same claim at the family's granularity (ADR 0054 decision 2). No expectation was changed, added, removed or weakened.

**Source:** review §Candidate 9. Depends on item 25.

**What:** Fold the seven launcher funcs into a named `LauncherHost` interface, following
item 25's pattern and ADR 0054. Nil-means-unwired preserved; wiring adapts in
`cmd/apogee` (the launcher wiring lives around launcher.go and its wire file —
implementer locates the exact wire seam); per-family fake in tests.

**Files:** internal/tui/tui.go, cmd/apogee/launcher.go, plus the cmd/apogee wire file
that constructs Options' launcher fields

**Tests:** per-family fake; existing launcher tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui ./cmd/apogee`

**Commit:** `refactor(tui): fold the launcher option funcs into a LauncherHost interface`

## 28. Per-lifetime state gets its own reset; one shared replay function — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the SESSION-NAMING cluster (`autoTitleFired`, `titleTouched`, `pendingTitle`, `pendingSource`, `sessionName`) was deliberately NOT grouped into a value here, although `startNewSession` resets four of the five. Item 29 ("the session title gets one owner") introduces exactly that value with its `adopt`/`stash`/`flush`/`restash` verbs and its doc, so grouping it here would have been that item's work under this item's commit. What this item owes the cluster — the `titleTouched` asymmetry preserved and documented at the reset site — is delivered at `resumeLoaded`.

NOTES (2026-08-20): the item's "four sites" are the review's — `finishWorker`, `startNewSession`, `resumeLoaded` and CONSTRUCTION. Construction calls no reset and needed none: a fresh `Model` is already the zero value both new types reset to, which is what makes "the zero value is the reset" true of them rather than a coincidence.

NOTES (2026-08-20): `detached` and `flash` fall at both session boundaries too and were left as their own lines. They are two unrelated concerns that happen to fall together — follow-the-tail is the viewport's, the flash is the status line's, and each has other writers (`interject.go`, `mouse.go`, the scroll folds) — so a value holding the pair would be an extraction of incidental similarity rather than of one reason to change.

NOTES (2026-08-20): `finishWorker` keeps `m.genStart = time.Time{}` and `foldCompactDone` keeps `m.ctxUsed = 0` as single lines rather than gaining methods of their own: neither boundary discards the conversation, so neither drops the whole live reading, and a one-field method per partial boundary would be indirection with nothing behind it. `liveStats.reset`'s doc names both so a reader meets the partial boundaries where the whole one is defined.

NOTES (2026-08-20): three prose pointers were corrected in the same commit because this change is what staled them — `noteContextFiles`' "(replayResumed)" now names `replayScrollback`, which is where the ephemeral-notice reason moved; `replayResumed`'s and `resumeLoaded`'s docs both described the replay block they no longer hold and now point at the shared one, each keeping the half that is still its own (the host-resolved `ResumedSession` for one, the persistent failure notes for the other). `commandrun.go` also dropped its now-unused `time` import.

NOTES (2026-08-20): the `usage` asymmetry the item names is a DEFECT, not a decision, and is deferred rather than fixed here (the item's own instruction). Evidence: the field's own doc calls it "the MAIN agent's cumulative token accounting for the session"; a `/sessions` restore DOES replace it; `/clear` mints a fresh Session record (the queued Rotate) and leaves it standing, so the fresh session's first save writes the closed session's totals into the new record (`sessionsave.go`'s `snapshotPayload`) and `/usage` reports them as the new session's spend — beside a sub-agent list that /clear DID empty, since those totals live on transcript run heads. Not in ISSUES.md.

**Source:** review §Smaller findings, row 1. Depends on item 21 (the folds settle
model.go's shape first).

**What:** "Reset the session" is a hand-kept checklist in four places (`finishWorker`
resets 11 fields of 8 concerns; `startNewSession` vs `resumeLoaded` differ silently).
Group the per-lifetime fields into small state values with their own `reset()` methods
and call those from the four sites. Replace the byte-for-byte replay block written in
model.go and sessions.go with one shared function. Per merge policy: the observed
asymmetries (`usage` not reset by /clear while `ctxUsed` is; `titleTouched` not reset on
resume while `autoTitleFired` is) are preserved exactly and documented at the reset
site; if the implementer finds evidence one is a bug, record a dated NOTES line and a
DEFER — never change the behaviour here.

**Files:** internal/tui/model.go, internal/tui/commandrun.go, internal/tui/sessions.go

**Tests:** existing session-lifecycle tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): give per-lifetime state its own reset and share the replay block`

## 29. The session title gets one owner — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the verb set is `adopt` / `flush` / `restash` / `drop`, with `stashed()` and `clobbered()` as its two queries — the plan named `adopt`/`stash`/`flush`/`restash`. The five distinct writes need a hold, a take, a re-hold, a rule-drop and a boundary-drop, and `stash` survives as the type name (`titleStash`) and its "is one waiting" query (`stashed`). The rule-drop and the take stayed separate verbs because the `ActiveID()` check must keep sitting BETWEEN them: the never-clobber drop happens even when no id was minted, so folding the two into one verb would have changed when a stash is given up.

NOTES (2026-08-20): the value owns `pendingTitle`/`pendingSource` only — the review's `model.go:105-106` — and not the whole naming cluster item 28's NOTES gestured at. `autoTitleFired`, `titleTouched` and `sessionName` stay plain `Model` fields: this item's text scopes it to the eight `pendingTitle`/`pendingSource` write sites, `titleTouched` is read by `foldAutoTitle` outside the stash entirely, and `sessionName` is the display half with its own single writer (`nameSession`). The two verbs that consult the never-clobber rule take `touched` as a parameter instead.

NOTES (2026-08-20): `restashTitle` (sessionsave.go) is deleted rather than kept as a wrapper — its doc and its rule moved onto `titleStash.restash`, and `foldRecordWrite` asks the value directly, which is what "all 8 write sites go through the verbs" costs.

NOTES (2026-08-20): `autotitle_test.go`'s field accesses were retargeted mechanically (`m.pendingTitle` → `m.pendingTitle.name`, `m.pendingSource` → `m.pendingTitle.src`, two arrange-only assignments → `adopt`); no test expectation changed. Six new verb tests were added beside them (`TestTitleStash*`), Model-free as the item asks.

**Source:** review §Smaller findings, row 2.

**What:** `pendingTitle`/`pendingSource` are written at 8 sites in 5 files and the
stash/restash invariant lives in ~60 lines of comment with no code home. Introduce a
small title value with `adopt`/`stash`/`flush`/`restash` verbs (living in autotitle.go);
the comment block becomes the value's doc; all 8 write sites go through the verbs.
Behaviour preserved exactly.

**Files:** internal/tui/autotitle.go, internal/tui/sessionsave.go,
internal/tui/sessions.go, internal/tui/commandrun.go, internal/tui/model.go

**Tests:** unit tests on the verbs (the invariant becomes directly testable without a
whole Model); existing autotitle tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): give the session title one owner with adopt/stash/flush verbs`

## 30. Name the uiState predicates — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the four names map to the four sets the call sites actually asked for — `live` = idle|running (the review's "BOTH live states", 8 inline spellings), `editable` = idle|ask|running (the state half of `Model.inputEditable`), `decisionPending` = approval|ask, `busy` = running|approval|ask (composed as `running || decisionPending()`). The item named the predicates but not their membership; this is the mapping the existing sites forced.

NOTES (2026-08-20): `Model.busy()` was kept as a one-line delegate to `uiState.busy()` rather than rewritten at its 11 call sites — the item says name the predicates on the state, not rename the Model's existing spelling, and the set is now stated exactly once. `Model.inputEditable()` likewise keeps its name and adds only the focus half; its doc no longer restates the state list (it points at `uiState.editable`), since that duplication is the thing this item removes.

NOTES (2026-08-20): `decisionPending` replaced exactly one inline site (`blockCursorOwnsKeys`). The approval/ask pairs in `frameOverlays` and `openPanes` and the two guards in `ask.go`/`model.go` were left as exact-state comparisons on purpose: each pairs its state with ITS OWN payload (`pending`, `pendingAsk`), which the item keeps where it is. The single-state checks (`settingsOwnsInput`, the spinner's `stateRunning`, the browser's `stateIdle`) are the "genuinely one-off" checks the item says to keep inline, as are the three `switch m.state` folds.

NOTES (2026-08-20): the predicate names carry no `is`/`has` prefix, against the coding-standards boolean rule — the item's text names them literally, and the neighbours they join (`busy`, `inputEditable`, `headsRun`, `opensRun`) already read that way; a prefixed twin beside them would be the odd one out.

NOTES (2026-08-20): no new tests. The item's Tests line asks only that the existing suite pass unchanged, and it does — `go test ./internal/tui` green with no expectation touched, `docmap_test.go` included (no file added or deleted, so `doc.go` needed no line).

**Source:** review §Smaller findings, row 3.

**What:** `uiState` is compared open-coded 34 times across 9 files; the idle-or-running
set is named only in prose. Add named predicates on the state — `editable`, `live`,
`busy`, `decisionPending` — and replace the inline comparisons that express those
questions (mechanical; grep the comparisons, keep genuinely one-off checks inline). The
enum stays a bare int; the state↔payload invariants stay where they are.

**Files:** internal/tui/model.go plus the files holding the comparisons (implementer
enumerates by grep; read only the touched functions)

**Tests:** existing suite passes unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): name the uiState predicates`

## 31. Painters take a stated input record — ✅ DONE (2026-08-20)

NOTES (2026-08-20): two files beyond the item's list changed because the signature change reached them — blockstate.go (`anyOpenCall` / `memberFlags` now take `[]paintInput`) and doc.go (one clause of the package map pointing at the record; no file added or deleted, so the ADR 0043 map is otherwise unchanged).

NOTES (2026-08-20): `toolCallRun` now returns the run's `[]paintInput` rather than `[]toolView` (`toolViews` extracts the views at paint time) — that is what gives toolbranch.go the record without moving the run-finding walk; its existing tests read `len()`/nil and were untouched. Three test files needed call-site updates only (`renderEntryLines` / `renderUserBlock` literals); no test expectation changed.

NOTES (2026-08-20): the walk keeps reading entries — where a block ENDS is a question about the list (`subAgentSpan`, `subAgentGroupAt`, `toolSuperGroup`, `sameLabelRun`) — and only what is downstream of a block's boundaries takes the record; the boundary is stated in `paintInput`'s doc and at the walk.

**Source:** review §Smaller findings, row 5. Depends on item 23.

**What:** `paintKey` completeness is a comment-level contract ("a field missing here is
a stale paint on screen") enforced by a hand-enumerated mutation test. Define a stated
input record carrying exactly the fields painters may read; the five painter files take
the record instead of raw `entry`, and `paintKey` derives from the record — a new
painted field becomes a compile-visible decision instead of a remembered rule.
Behaviour identical; the existing mutation test and golden tests pin it.

**Files:** internal/tui/paintcache.go, internal/tui/render.go,
internal/tui/toolblock.go, internal/tui/subagentblock.go, internal/tui/toolbranch.go,
internal/tui/userblock.go

**Tests:** `TestPaintCacheMatchesAColdRenderThroughEveryMutation` and
`TestTranscriptLayoutGolden` pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): paint from a stated input record so paintKey stays complete by construction`

## 32. One resolver for block shape, span, and closure in renderView — ✅ DONE (2026-08-20)

NOTES (2026-08-20): `appendBlock`, the two-line wrapper the five branches used to reach `appendJoined` by, went with the chain — the resolved block now carries its own `closes` answer, leaving the wrapper one caller (the streaming preview), which calls `appendJoined` directly.

**Source:** review §Smaller findings, row 6. Depends on item 31.

**What:** `renderView` is a 245-line function: a 5-branch block-shape chain locked to a
5-value enum in another file, with hand-written index advancement per branch — the one
place an off-by-one silently skips or double-paints a block. Introduce one "what block
starts at i" resolver returning shape + span + closure together; the branch chain and
per-branch advancement collapse to resolver + loop. Behaviour identical.

**Files:** internal/tui/render.go, internal/tui/paintcache.go

**Tests:** `TestTranscriptLayoutGolden` and the paintcache tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): resolve block shape, span and closure in one place in renderView`

## 33. Name the click-map module: blocktarget.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the moved primitives are the click-map VOCABULARY — `targetKind` with its four constants, `lineTarget` and `lineMark` (52 lines, matching the review's "~60 lines out of render.go"). `blockPaint` and its `add`/`addFor`/`join`/`railed` builders stayed in render.go: they are the paint vehicle that carries the marks, doc.go already names them there, and moving them would have doubled the item's size.

NOTES (2026-08-20): two consumer comments the move made stale were repointed — `blockcursor.go` and `mouse.go` each named `render.go` as the home of `[lineTarget]` / `lineMark`. Comment text only, no code; the item's "consumers untouched" holds for code.

**Source:** review §Smaller findings, row 7. Depends on item 32.

**What:** `blocktarget_test.go` (737 lines) names a module that has no implementation
file. Move the click-map primitives (~60 lines, mostly out of render.go) into new
`internal/tui/blocktarget.go`. Pure move + naming; consumers (mouse.go, blockcursor.go)
untouched. doc.go line in the same commit.

**Files:** internal/tui/render.go, internal/tui/blocktarget.go, internal/tui/doc.go

**Tests:** blocktarget_test.go passes unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): name the click-map module blocktarget.go`

## 34. One parked-call helper; the cross-goroutine idioms get named — ✅ DONE (2026-08-20)

**Source:** review §Smaller findings, row 8.

**What:** `uiApprover.Approve` and `uiAsker.Ask` are structurally identical 10-line
rendezvous bodies. Extract one generic parked-call helper into new
`internal/tui/parkedcall.go`; both delegates use it. Add a doc paragraph (in
parkedcall.go, referenced from doc.go) naming the three cross-goroutine idioms —
rendezvous (approver/asker), mailbox (`interjectBox`), fire-and-forget (`uiPresenter`,
`Bridge.Notify*`) — and pointing at ADR 0011's legality classes. The helper lives
host-side (the delegates), never on the value-typed Model. doc.go line in the same
commit.

**Files:** internal/tui/approver.go, internal/tui/asker.go, internal/tui/parkedcall.go,
internal/tui/doc.go

**Tests:** existing approver/asker tests pass unchanged; add one unit test on the
helper.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): share one parked-call helper and name the cross-goroutine idioms`

## 35. One scrollbar thumb-geometry function — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item says "Rendering byte-identical", and it is not quite — the transcript's old position went through `viewport.ScrollPercent()`'s float before truncating, which disagrees with the shared integer division at rare offsets (brute force over height ≤ 80, total ≤ 3000, every offset: ~4.2k mismatches out of ~3.6e8 combinations, e.g. height 23 / total 265 / offset 165 → old 14, new 15, exact 15 — the float always landed the row early). Adopted the integer general case as the item directs ("popup.go's version is the general case … the transcript … calls it with its two conflated counts pulled apart"); every existing test passes unchanged and no golden pins a thumb row.

NOTES (2026-08-20): took the item's stated mechanical choice and put `scrollbarThumb` in boxdraw.go beside `joinScrollbar` rather than in popup.go, so the transcript's model.go does not reach into the popup file for arithmetic; doc.go's boxdraw.go line gained the symbol (no new file, so the doc map is otherwise unchanged).

**Source:** review §Smaller findings, row 9.

**What:** Thumb arithmetic exists twice; popup.go's version is the general case and its
comment already names the relationship. Extract one thumb-geometry function
(window, total, painted-height) in popup.go (or boxdraw.go if it reads better beside
`joinScrollbar` — implementer's mechanical choice); the transcript scrollbar in model.go
calls it with its two conflated counts pulled apart. Rendering byte-identical.

**Files:** internal/tui/model.go, internal/tui/popup.go

**Tests:** existing scrollbar-covering tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): compute scrollbar thumb geometry in one function`

## 36. Dissolve chromelayout.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): `clampInt`'s destination was the item's stated mechanical choice — `textutil.go`, the package's declared home for the pure helpers it spells ONCE (ADR 0043); its banner and intro widened from "text helpers" to "helpers" to cover a numeric clamp, and doc.go's textutil line with them.

NOTES (2026-08-20): ADR 0030 §6 named `render.go` for `inputContentRows` (stale since the render split moved it to chromelayout.go), so the amendment note both corrects the file and records the two hops; the mirror list and the rule itself are unchanged.

NOTES (2026-08-20): `internal/tui/chromelayout_test.go` — the `inputContentRows` suite and its widget oracle — was left exactly where it is: the item's Files line names only chromelayout.go for deletion, renaming files is out of scope, and it follows the precedent item 8 set when `toolpresent.go` was deleted and `toolpresent_test.go` stayed. The docmap guard covers non-test files only, so nothing enforces a rename either way.

**Source:** review §Smaller findings, row 10.

**What:** chromelayout.go (72 lines) holds an ADR 0030 widget mirror
(`inputContentRows`) and a generic `clampInt` (~15 call sites) — nothing in common. Move
`inputContentRows` beside the other mirrors in inputaccent.go, keeping its declared-
mirror status and comment intact, and update every place that declares the mirror list
(doc.go and, if ADR 0030's text names the file, a dated amendment note there). Rehome
`clampInt` into whichever existing file reads naturally (mechanical choice). Delete
chromelayout.go; doc.go drops its line, same commit.

**Files:** internal/tui/chromelayout.go (deleted), internal/tui/inputaccent.go,
internal/tui/doc.go, plus clampInt's destination file

**Tests:** existing suite passes unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): dissolve chromelayout.go into its two real homes`

## 37. Fold the ask-only popupSpec fields into one named row style

**Source:** review §Smaller findings, watch item. Depends on items 3 and 17.

**What:** `popupSpec` is at 20 fields; `titleFromBody`, `rowGap`, `rowPadBelow` have
exactly one caller (the ask prompt). Fold the trio into one named row-style the ask pane
passes; popupSpec shrinks by three fields (the filter pads are already gone via items
15/17). Rendering byte-identical.

**Files:** internal/tui/popup.go, internal/tui/ask.go

**Tests:** existing popup/ask tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): fold the ask-only popupSpec fields into one named row style`

---

## Suggested version bump

No item changes a version identifier. When the plan lands, a **patch** bump is suggested
(pure behaviour-preserving refactor; the `Options` regrouping changes an internal
package API consumed only by `cmd/apogee` in the same module). Whether and when to bump
is the user's call.
