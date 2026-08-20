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

## 14. Route the three raw filter buffers through lineEditor

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

## 15. Introduce listSurface; adopt it in picker and sessions; write ADR 0053

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

## 16. Adopt listSurface in settings and autocomplete

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

## 17. Adopt listSurface for the approval and ask selections

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

## 18. Merge the settings twins

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

## 19. Split settings.go along its surface seams

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

## 20. The frame publishes its geometry once

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

## 21. Every Update arm delegates; the key-claim order becomes data

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

## 22. The command table absorbs its satellite lists

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

## 23. entryKind answers for itself, with a completeness test

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

## 24. Name the sub-agent run-head predicates

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

## 25. Options: settings and scheme funcs become named interfaces; write ADR 0054

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

## 26. Options: server and heartbeat funcs become ServerHost

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

## 27. Options: the launcher family becomes LauncherHost

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

## 28. Per-lifetime state gets its own reset; one shared replay function

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

## 29. The session title gets one owner

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

## 30. Name the uiState predicates

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

## 31. Painters take a stated input record

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

## 32. One resolver for block shape, span, and closure in renderView

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

## 33. Name the click-map module: blocktarget.go

**Source:** review §Smaller findings, row 7. Depends on item 32.

**What:** `blocktarget_test.go` (737 lines) names a module that has no implementation
file. Move the click-map primitives (~60 lines, mostly out of render.go) into new
`internal/tui/blocktarget.go`. Pure move + naming; consumers (mouse.go, blockcursor.go)
untouched. doc.go line in the same commit.

**Files:** internal/tui/render.go, internal/tui/blocktarget.go, internal/tui/doc.go

**Tests:** blocktarget_test.go passes unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui`

**Commit:** `refactor(tui): name the click-map module blocktarget.go`

## 34. One parked-call helper; the cross-goroutine idioms get named

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

## 35. One scrollbar thumb-geometry function

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

## 36. Dissolve chromelayout.go

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
