# TUI polish — numbered write bodies, a green `done`, a clickable mode marker

**Goal:** close three `IDEAS.md` items. Every tool body that lays out file content carries line
numbers (write_file records none today); a finished delegation's `done` verdict reads in the
scheme's `success` colour; clicking the footer's mode marker opens a four-rung mode picker.

**Date:** 2026-08-31 · **Status:** unexecuted · **sized for:** ~200k-context host
**Base commit:** `ad962676`

**Sources:** `IDEAS.md` (items 3–5) · `docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md`
· `docs/layout/tool-layout.md` · `docs/layout/split-diff-layout.md` · `layout.md` ("The outcome
slot", "The status line's right slot") · `CONTEXT.md` §Tool summary

**Ratified design calls** (owner, 2026-08-31, this session):
- **Mode click:** opens a four-rung mode picker, never a cycle; ⏎ takes the row through the same `SetMode` path `shift+tab` uses.
- **Hit area:** the whole footer mode marker — symbol, word, and Auto's blast-radius word — is one rect acting on the MODE; the confinement word stays display-only.
- **Success tone:** only a delegation's `done` verdict goes `success`; `stopped at its step cap` stays neutral, failures stay red, no other tool's slot changes.
- **Collapsed run:** `succeeded` is carried forward by `subAgentSummary` exactly as `failed` is, so a finished run's slot is green in both readings.
- **write_file body:** the tool records real Edit regions (read-before-write, `okEditRegions`); its outcome slot keeps the ratified `N lines` wording.
- **Announce numbering:** write_file and edit_existing_file's full-content form number their argument bodies 1..N; the patch form and the two find-and-replace bodies stay unnumbered — positions are unknown until applied.
- **WroteBytes:** deleted with its `apogee` alias once write_file no longer emits it.
- **Audit scope:** the six file-content bodies; prose bodies (command output, git_status, a delegation report, diagnostics) stay unnumbered.

**Regression check (2026-08-31, `ad962676`):**
- **1** — guard folded: an absent original is ZERO before-lines, not one empty one.
- **2** — guard folded: the built-in count stays **ten** (write_file swaps its summary rather than
  losing it); the three test files naming `domain.WroteBytes`, the sealed-sum count and the "eight
  variants" sentences fold in with it.
- **3** — guard folded: `internal/tui/diffbody_test.go` does not exist; **Files** and **Acceptance**
  re-pointed at the pins that do, `toolbranch_test.go` included.
- **4** — guard folded; supersedes `docs/layout/tool-layout.md:78-79`, the outcome slot's two-tone
  rule, named in the item.
- **5** — guard folded: `pickerMode` gains its `pickerKindCases()` case.
- **6** — decision applied: the footer's mode claim is gated on the picker's OWN entitlement, not on
  `!m.picker.open` alone.
- **7** — guard folded: the prose bodies become a rule walked over `toolRegistry`, and "the
  recorded-regions path" widens to the presenter's `regions` hook.

**Standing requirements:** `skills: coding-standards`.

**Out of scope:** a `/mode` verb for the new picker (`shift+tab` stays the keyboard route) · the
approval pane's argument rendering · the three `IDEAS.md` items not selected (`/inspect`
readability, sub-agent auto-naming, per-job sub-agent server routing).

## 1. write_file records the Edit regions of what it wrote — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the plan's `okEditRegions(..., string(original), ...)` call could not express the ratified "an absent original is ZERO before-lines" guard, so the read-failure path goes through a new sibling helper `okInsertedRegion` (internal/tools/regions.go) that builds the {BeforeStart: 1, AfterStart: 1} all-inserted region directly; the ordinary read-succeeded path is the plan's `okEditRegions` call verbatim.
NOTES (2026-08-31): the unreadable-original test stages its original with mode 0o000 and skips under euid 0 (root reads it anyway); verified passing as a non-root user, not merely skipping.

**What.** `internal/tools/write_file.go`'s `Execute` reads the target before it writes — through
`readWriteTarget(ctx, args.Path, t.root)`, the helper `file_edit.go:89` already uses — and returns
`okEditRegions(call.ID, <today's sentence>, string(original), args.Content)` in place of
`okSummary(..., domain.WroteBytes{...})` (`write_file.go:92-94`). The result's CONTENT sentence
("wrote %d bytes to %s%s") is unchanged. A pre-read that fails for ANY reason degrades to an empty
before-side — the ordinary create, where nothing is there yet, and the rare unreadable original
alike: a tool that reads nothing today must not start failing writes on a read error. Identical
content records no regions, so `okEditRegions` falls back to `okResult` and the card keeps its prose
floor. No registry change: the slot's `N lines` comes from the request-side `writtenLinesStat`
(`toolregistry.go:209`), which `enrichWithResult` re-applies after the result lands
(`toolview.go:1048`), and the ratified `tool-layout.md` row keeps that wording.
**Files:** `internal/tools/write_file.go`, `internal/tools/write_file_test.go`
**Tests.** Create (no original) → one region, `BeforeStart`/`AfterStart` 1, every content line
Inserted and `Removed` EMPTY — including a content holding a blank line, which records no context
row. Overwrite of a 40-line file changing one line → one region carrying the real
`BeforeStart`/`AfterStart` and three lines of context each side. Identical content → result carries no summary. Unreadable original →
the write still succeeds and the whole content reads as inserted. The fence and refusal paths are
unchanged (existing `write_file` escape tests still pass).
**Acceptance.** `go test ./internal/tools/ -run 'TestWriteFile'`
**Regression guard.** The pre-read must not move the write's fence: `safeWriteFile` still performs
the write through the `os.Root` pinned at `t.root`, and `resolvedTargetNote` is still read BEFORE
the write. A read error must never become an error result. And an ABSENT original is ZERO
before-lines, not one empty one: `strings.Split("", "\n")` is `[""]`, so handing `editRegions` an
empty string records a phantom removed blank line and reads a blank line in the content as unchanged
context. Record `{BeforeStart: 1, AfterStart: 1}` with every content line Inserted and `Removed`
empty — and keep the summary: `internal/tui/toolsummary_pin_test.go:95-99,163-166` requires
write_file to attach one.
`fix(tools): write_file records the Edit regions of what it wrote`

## 2. Retire `WroteBytes` and correct every line that enumerates the summary variants — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the grep is a floor — four sites beyond the item's named list were folded in: `internal/tools/regions.go:24,56` and `internal/tui/toolview.go:466,1112,1218` (Edit-regions producer claims), plus a line-wrapped `the three edit\n// tools share` in `internal/tools/doc.go:295` the item's grep pattern could not match.
NOTES (2026-08-31): `internal/domain/toolsummary.go:11` and `CONTEXT.md:1184` listed "a byte count" among the facts a summary carries — the one remaining naming of `WroteBytes`' fact — and were corrected to "a match count".
NOTES (2026-08-31): `internal/tui/toolregistry.go:181`'s claim that every summary-bearing tool words its slot "from that summary through their stat hook" became false for `write_file` under the ratified `N lines` call; the sentence now names `write_file` as the one exception (`writtenLinesStat`, request-derived).
NOTES (2026-08-31): `docs/adr/0002`'s dated 2026-08-20 amendment also calls the producers "the three edit tools". Left as found — it is a dated historical amendment and ADR house style amends rather than rewrites; its "nine built-ins" count was already stale (git_status made it ten on 2026-08-26), pre-existing debt this item did not create.
NOTES (2026-08-31): sentences about the three edit tools' SLOT wording (`editRegionsStat`, `toolregistry.go:541`, `toolsummary_pin_test.go:125`, `toolpresent_test.go:2484`) and about their argument-derived bodies (`toolregistry.go:197`, `diffbody.go:292`, `internal/tui/doc.go:903`) are still exactly true of three tools and were deliberately left alone; `internal/tools/doc.go:275` already enumerated "write_file and the three edit tools" correctly.
NOTES (2026-08-31): `internal/tui/render_test.go`'s golden and `TestWriteBodySurvivesItsByteCountSummary` pass unchanged with the `EditRegions` result substituted for `WroteBytes` — the write card's slot and body read the same as before.

Depends on item 1.

**What.** Delete `domain.WroteBytes` (`internal/domain/toolsummary.go:68-71`) and the
`apogee.WroteBytes` alias (`apogee.go:334-335`) — after item 1 it has no producer. Then the prose:
**every sentence that enumerates the typed tool summaries, or names write_file's outcome, or calls
the Edit-regions producers "the three edit tools", is corrected to the four producers this plan
leaves.** Find them with `rg -ni 'wrotebytes|three edit tools|eight (variants|below)|\*{0,2}ten\*{0,2} built-ins'`
— known sites are `internal/tools/doc.go:22`, `CONTEXT.md:1189-1192` (the count stays **ten**;
write_file leaves the plain enumeration and joins the Edit-regions group), `internal/domain/doc.go:72`,
`internal/domain/toolsummary.go:41` and `:131`, `internal/tui/toolregistry.go:179-198`,
`internal/tui/diffbody.go:24-30` — and the list is the grep's, not this line's. ADR 0052 gains a
dated amendment paragraph under its decision 1 recording write_file as a fourth recorder.
**Files:** `internal/domain/toolsummary.go`, `internal/domain/toolsummary_test.go`,
`internal/domain/doc.go`, `apogee.go`, `internal/tools/doc.go`, `CONTEXT.md`,
`docs/adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md`,
`internal/tui/toolregistry.go`, `internal/tui/diffbody.go`, `internal/tui/toolpresent_test.go`,
`internal/tui/render_test.go`
**Tests.** No new behaviour; the build is the test. `rg -n 'WroteBytes'` returns nothing outside
this plan's own history. The sealed-sum test drops the variant from both its lists and its
`const want = 8` becomes 7 (`internal/domain/toolsummary_test.go:13,30,38`); the two write_file card
cases (`internal/tui/toolpresent_test.go:109-115`, `:1255`) and `internal/tui/render_test.go:298` are
re-pointed at the EditRegions result item 1 now produces, and `toolpresent_test.go:1831`'s negative
case names another variant.
**Acceptance.** `go build ./... && go test ./internal/domain/ ./internal/tools/ ./internal/tui/`
**Regression guard.** The removal is a public-API deletion in a sealed sum: no other variant's
marker method, and no codec, may be touched. `internal/session`'s transcript codec never named
`WroteBytes` and must still round-trip every other summary. The plan's "ten built-ins → nine" claim
is WRONG and must not be written into CONTEXT.md — write_file SWAPS WroteBytes for Edit regions
rather than losing its summary, so the count of summary-bearing built-ins stays TEN and only the
GROUPING moves: write_file leaves the plain enumeration and joins the parenthetical Edit-regions
group beside the three edit tools. Item 2's Files must also list every test file naming
`domain.WroteBytes`, since the first `go test ./internal/domain/ ./internal/tui/` after the deletion
fails without them. The sum's own count moves with it — `const want = 8` → 7 — and so do the two
sentences that spell it, `internal/domain/doc.go:72` ("its eight variants") and
`internal/domain/toolsummary.go:41` ("the eight below").
`refactor(domain): retire WroteBytes now that write_file records regions`

## 3. The write and full-content edit bodies carry line numbers — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the wrapper is named `numberedBody`, not the item's `numberedLines` — `numberedLines` is already a test helper in `internal/tui/render_test.go:359` and renaming an existing identifier is out of scope.
NOTES (2026-08-31): `internal/tui/splitdiff_test.go` (listed under the item's Files) needed no edit — its `TestStacked…` gutter pins pass unchanged; the item's Files list names it as a pin that must still hold, which it does.

**What.** `writtenLines` (`internal/tui/diffbody.go:905`) and the full-content branch of
`fileEditBody` (`diffbody.go:834`) number their `+` lines 1..N in each line's `Gutter`
(`toolview.go:57-70`), right-aligned to the widest number through the existing `stackedGutter`
(`diffbody.go:790`) rather than a second arithmetic. The numbering goes in a wrapper over
`changedLines`' output — `changedLines` itself is NOT touched, because the two find-and-replace
bodies share it and must stay unnumbered. Unnumbered by ratified call and stated in the code:
`fileEditBody`'s patch branch (apogee's patch dialect writes `@@` with no ranges — `patchOpener`,
`diffbody.go:850`), `singleReplacementBody` and `multiReplacementBody` (a needle's position is
unknown until the tool has run). The announce-time numbers are the AFTER file's 1..N and are
replaced by the recorded regions' real numbers when the result lands; that re-reading is the
ratified behaviour, not a flicker to suppress.
**Files:** `internal/tui/diffbody.go`, `internal/tui/toolpresent_test.go`,
`internal/tui/splitdiff_test.go`, `internal/tui/toolbranch_test.go`
**Tests.** A 3-line write → gutters `"1 "`, `"2 "`, `"3 "`. A 12-line write → right-aligned to
width 2. `fileEditBody` full-content form numbered; its patch form unchanged. The two
find-and-replace bodies byte-identical to today. A body whose lines are numbered still carries its
`detailDiffAdded` Kind and its `+ ` marker inside `Text`. `TestExpandedBlockPaintsItsWholeBody`'s
write case (`toolbranch_test.go:328-336`) restates its expectation as the numbered rows.
**Acceptance.** `go test ./internal/tui/ -run 'TestWriteCall|TestWriteBody|TestEditCalls|TestExpandedBlock|TestStacked'`
**Regression guard.** The number is the line's `Gutter` and never the head of `Text`: `Text` keeps
the marker so the clip still takes the tail, and the band still tints `Text` alone (ADR 0052's
2026-08-19 amendment, `stackedRow.line`). `internal/tui/diffbody_test.go` does not exist: the bodies'
pins are `TestWriteCallCarriesTheWrittenLines` and `TestEditCallsCarryTheirChangedLines`
(`toolpresent_test.go:1170`, `:1039`) plus `splitdiff_test.go`, and a filled `Gutter` widens the row's
lead at `toolbody.go:103` — so `TestExpandedBlockPaintsItsWholeBody`'s pinned `"    + alpha"` rows
(`toolbranch_test.go:334`) are restated as numbered here, never left to fail.
`feat(tui): the write and full-content edit bodies carry line numbers`

## 4. A finished delegation's `done` reads in the scheme's success colour — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the transcript-codec round-trip case the item's Tests ask for went into `internal/tui/transcriptbridge_test.go` (`TestTranscriptCodecReplaysAFinishedDelegationGreen`), which the item's Files list does not name — that file is where every `TestTranscriptCodec…` lives and where the codec helpers are, and the acceptance regex names that prefix. Its import block gained `internal/scheme` for the tone assertion.
NOTES (2026-08-31): `internal/tui/toolpresent_test.go` is listed under the item's Files but needed no edit — its delegation pins are the slot's SPELLINGS (`toolpresent_test.go:1652`, unchanged by this item) and the tone claims belong beside the existing outcome-slot verdict sections in `toolbranch_test.go` and `subagentblock_test.go`, both also listed.
NOTES (2026-08-31): every tone fixture uses a TWO-LINE delegation report on purpose. A one-line report is PROMOTED into the slot and marked quoted, and a quoted summary carries neither verdict (the item's own regression guard) — so a slot reading `done` is only reachable with a report that hangs in the body, or after the promote-guard demotes.
NOTES (2026-08-31): prose guard — the sites amended are `layout.md`'s outcome-slot paragraph and its "an open block reads a step brighter" cross-reference, `docs/layout/tool-layout.md:79`'s `<tool-top-level-details>` Colour rule, `internal/tui/theme.go`'s `successMark` and `toolMarker` comments, `typedSummary`'s "never a verdict" comment, `namedSummary`'s, `subAgentSummary`'s and `summaryStyle`'s. `internal/tui/toolbranch.go:479` and `internal/tui/theme.go:155` were deliberately left alone: both say the outcome slot is off the `muted`/`muted-bright` EMPHASIS ramp and already elide the failure red, so neither asserts a count of non-failure tones.
NOTES (2026-08-31): the predicate `succeededSummary` (internal/tui/toolleader.go, beside `failedSummary`) matches the whole phrase — `done`, or `done · steered by …` — and `rg` confirms `delegationDoneVerdict` is the only producer of that word in the package, so no non-delegation named summary can reach the green.

**What.** `branchSummary` (`internal/tui/toolview.go:75`) gains `succeeded`, the mirror of `failed`.
It is set from the delegation vocabulary at the two seams that WORD a slot and never by a painter:
`typedSummary` (`toolview.go:167`) on the live path, where a delegation's plain stat is spelled, and
`namedSummary` (`toolview.go:144`) on the replay path, where a plain stat is restored as a named
summary (`transcriptbridge.go:341`) — one predicate, anchored on `delegationDoneVerdict` and its
steered form (`toolregistry.go:719-728`), rejecting `delegationCappedVerdict`, `clean`, `PASS`,
`exit 0` and any phrase merely containing the word. `subAgentSummary` (`subagentblock.go:632`)
carries the flag forward exactly as it carries `failed`, so a collapsed run's composed line is green
too. `summaryStyle` (`toolleader.go:283`) returns the scheme's `success` foreground —
`theme.successMark`, reused rather than duplicated into a second field holding the same value, its
comment widened to cover both users — when `succeeded` and not `failed`; `failed` still wins.
**Prose guard.** `typedSummary`'s comment states that a stat's phrase "is a READING and never a
verdict", and `layout.md`'s outcome-slot section states the slot walks the
`tool-marker`/`tool-marker-bright` pair and nothing else. The rule: **every comment or spec line
asserting the outcome slot has exactly one non-failure tone is amended to name this one exception
and why it is anchored on the delegation vocabulary.** Find them with
`rg -n 'tool-marker-bright|never a verdict|outcome slot' layout.md docs/layout internal/tui`.
**Files:** `internal/tui/toolview.go`, `internal/tui/toolleader.go`, `internal/tui/subagentblock.go`,
`internal/tui/theme.go`, `layout.md`, `docs/layout/tool-layout.md`,
`internal/tui/toolpresent_test.go`, `internal/tui/subagentblock_test.go`,
`internal/tui/toolbranch_test.go`
**Tests.** A delegation result → slot rendered with the scheme's `success` hex. A step-capped one →
`tool-marker`. A failed one → red, and `succeeded` never overrides it. The collapsed head row green
while its text is a tool-call count. A transcript-codec round trip of a finished delegation replays
green. The predicate rejects the five phrases named above.
**Acceptance.** `go test ./internal/tui/ -run 'TestDelegation|TestSubAgent|TestSummaryStyle|TestTranscriptCodec'`
**Regression guard.** No painter may read the slot's TEXT for a verdict (F-28/F-29): the flag is
derived once at the wording seam and carried, exactly as `failed` is. `quotedSummary` still carries
neither verdict, so a promoted output line reading `done` stays neutral. The single-tone rule this
item amends is recorded as INTENDED at `docs/layout/tool-layout.md:78-79` ("the `tool-marker` role
while the block is collapsed and `tool-marker-bright` once it is open") — that ratified line is
superseded here and must itself name the delegation exception. Ownership of that file splits: item 7
owns its write_file body row, this item owns its outcome-slot tone lines.
`feat(tui): a finished delegation's done verdict reads in the success colour`

## 5. A mode picker offers the four autonomy rungs — ✅ DONE (2026-08-31)

NOTES (2026-08-31): the accept restates `SetMode` + `opts.Mode` rather than extracting a helper Shift+Tab would also call — the item spells those two statements and `model.go` is not in its Files (item 6 owns that file); `acceptMode`'s comment names the chord as its twin.
NOTES (2026-08-31): consequential edit — internal/domain/doc.go: made necessary by exporting `ModeLadder`, which the config.go file-map line enumerates the mode-ladder surface without ("Mode, ParseMode, NextMode, TighterMode").
NOTES (2026-08-31): consequential edit — internal/tui/picker_test.go: `TestPickerHintsLeadWithTypeToFilter`'s kind list enumerates the kinds whose hint leads with the filter segment and gains `pickerMode` (beyond the item's own `pickerKindCases` case).
NOTES (2026-08-31): one test beyond the item's list — `TestPickerModeAutoRungActsOnEveryHost` pins the ratified call that Auto is offered and acts even where `/schedule`'s blocked-auto answer would refuse it.

**What.** `internal/domain/config.go` exports the ladder — `func ModeLadder() []Mode` returning a
copy of `modeLadder` (`config.go:642`) — so the picker and `NextMode` read ONE list rather than two
that agree today. `internal/tui/picker.go` gains a `pickerMode` kind: one row per rung in ladder
order, each row the mode's marker (`modeMarker`, `model.go:3077`) with a short sentence for what the
rung permits; the hint is the existing "⏎ choose" variant (`pickerHintFor`, `picker.go:147`); accept
performs exactly what `shift+tab` performs — `m.eng.SetMode(mode)` then `m.opts.Mode = mode`
(`model.go:1536-1544`) — and closes the overlay. The Auto row is offered on every host and taking it
ACTS, unlike `acceptScheduleMode`'s blocked-auto answer: this is the session's own ladder, which
`shift+tab` already reaches unconditionally, and refusing it here would be a new restriction.
**Files:** `internal/domain/config.go`, `internal/domain/config_test.go`, `internal/tui/picker.go`,
`internal/tui/picker_test.go`
**Tests.** Rows are the four rungs in ladder order, each carrying the marker `modeMarker` renders
for it. ⏎ on `allow edits` moves both the engine's mode and `m.opts.Mode`. `esc` closes and changes
neither. `ModeLadder()` returns a copy — mutating the result does not move `NextMode`. And
`pickerKindCases()` gains a `pickerMode` case: filter down to a rung that is NOT the first, ⏎ takes
THAT rung.
**Acceptance.** `go test ./internal/domain/ ./internal/tui/ -run 'TestModeLadder|TestNextMode|TestPickerMode|TestPickerFiltered'`
**Regression guard.** The kind is added to every switch `pickerKind` fans out through
(`pickerHintFor`, the title, the rows and the accept — `picker.go:147`, `:836`, `:1017`); a kind
missing from one of them renders an empty pane. `pickerKindCases()` (`picker_test.go:1801`) claims to
cover "every kind the overlay lists" and is what `TestPickerFilteredViewAgreesOnRowsCountAndAccept`
(`picker_test.go:1941`) drives: a `pickerMode` added without a case there leaves that claim false and
the new kind's filtered-accept mapping unexercised.
`feat(tui): a mode picker offers the four autonomy rungs`

## 6. Clicking the footer's mode marker opens that picker — ✅ DONE (2026-08-31)

NOTES (2026-08-31): `footerContent`'s left half is extracted alongside the span as `footerLeftText` — `footerModeSpan(w)` has to measure the info and offline segments to know whether the marker fits, and the item's signature takes only `w`, so composing that half once is what keeps the extraction from adding a second arithmetic instead of removing one. The plain marker text itself is joined in one place (`footerModeText`), which both the span and the painter's two-tone `unconfined` split read.
NOTES (2026-08-31): the item's `handleMouseClick` insertion point is honoured as written — after the settings pane and both report panes, before the prompt and transcript rects — and the claim is gated exactly on `m.state.live() && !m.picker.open && !m.sessionBrowser.open && !m.settingsOwnsInput()` per the Regression guard.
NOTES (2026-08-31): the footer row is derived by a named `footerRowY()` (`m.height - bottomRuleHeight - footerHeight`) rather than a literal, mirroring `inputContentRect`'s term-by-term posture; `TestClickOnBottomChromeSelectsNothing` already pins that same row arithmetic.

Depends on item 5.

**What.** `footerContent` (`internal/tui/model.go:2884`) composes the marker's text and places it by
arithmetic the pointer would otherwise have to repeat. Extract that composition — `footerModeSpan(w)
(text string, col int, ok bool)` — and have `footerContent` PAINT from it, so the cells the marker is
drawn on and the cells a click may address are one value rather than two arithmetics that agree
today. `ok` is false in the narrow branch where the marker drops whole (`model.go:2926-2933`), and a
click there names nothing. `handleMouseClick` (`mouse.go:398`) asks the footer after the settings
pane and both report panes have had their claim, and only while the picker's own rung would own the
keyboard (the guard below), before the prompt and transcript rects; a hit opens `pickerMode`. The
rect is the footer's own row and the columns `footerModeSpan` reports — the WHOLE marker, Auto's blast-radius
word included, which acts on the mode like every other cell of it.
**Files:** `internal/tui/model.go`, `internal/tui/mouse.go`, `internal/tui/mouse_test.go`,
`internal/tui/model_test.go`, `layout.md`, `docs/manual/commands.md`
**Tests.** A click on the marker's first, middle and last cell opens the picker; one cell to its
left does not; a click in a window too narrow for the marker opens nothing; a click while a picker
is already open does not stack a second; nor does one at `stateAwaitingApproval`, with the /settings
pane owning input, or with the /sessions browser up. Painter/pointer agreement: render the real
footer and assert the cells at the reported columns hold exactly the string `modeMarker` produced,
in Auto with its blast-radius word.
**Acceptance.** `go test ./internal/tui/ -run 'TestFooter|TestModeMarker|TestClick'`
**Regression guard.** The footer's existing shape is unchanged — the black field to the full window
width, the `bodyIndent` right margin, the whole-marker drop in the narrow branch, and `unconfined`
in the error tone. The extraction must not re-order the styled runs. Gate the footer's mode claim on
the picker's OWN entitlement, not on `!m.picker.open` alone: the claim fires only while
`m.state.live()` and no higher modal rung is up — no picker, no /sessions browser, no /settings pane
owning input (`m.state.live() && !m.picker.open && !m.sessionBrowser.open && !m.settingsOwnsInput()`,
mirroring the rungs at `internal/tui/model.go:1249`, `:1258`, `:1267`). Verified against the tree at
`ad962676`: `renderPicker` paints on `m.picker.open` alone (`internal/tui/picker.go:997`) while the
picker rung claims keys only under `m.state.live() && m.picker.open`, so any other gating opens a
modal the human can neither answer nor close — the very thing that rung's comment forbids.
`shift+tab` stays the every-state route, and the item's Tests gain a case per blocked state (awaiting
approval, /settings up, /sessions up).
`feat(tui): clicking the footer mode marker opens the mode picker`

## 7. Pin that every file-content body is numbered, and say which are not

Depends on items 1 and 3.

**What.** One table-driven guard test in `internal/tui` over the six bodies that lay out FILE
CONTENT — write_file, `edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace`,
`view_diff`, `git_diff_range` — asserting that once each one's result lands, its body's diff lines
carry a `Gutter` (they reach `stackedDiffLines`, the one builder). For the prose bodies the same
table states the RULE rather than a list: the test walks `toolRegistry` and asserts every entry whose
lines come from `outputBody`/`outputDetail` carries no `Gutter`. A command's output, `git_status`, a
delegation report and `diagnostics` are named there as the ratified EXAMPLES and never as the
boundary — `python_exec`, `git_branch` and `git_log` floor on `outputDetail` too, and a closed list
would leave them unguarded. `docs/layout/tool-layout.md:267` (write_file's row: "the written
content") is corrected to say the body is the recorded diff, numbered.
**Files:** `internal/tui/toolpresent_test.go`, `docs/layout/tool-layout.md`
**Tests.** The table above IS the test; it fails if a seventh file-content body is added without a
numbered reading, and if ANY registry entry bodying out through `outputBody`/`outputDetail` starts
carrying a gutter.
**Acceptance.** `go test ./internal/tui/ -run 'TestFileContentBodiesAreNumbered'`
**Regression guard.** The test drives each tool through the presenter with a real result, whether the
tool RECORDED the regions or the `regions` hook recovered them — `view_diff` and `git_diff_range`
record none, theirs coming from `viewDiffRegions` (`toolregistry.go:272`) and `gitDiffRangeRegions`
(`:367`) — and never by calling the body hooks directly, so a tool that stops recording regions fails
it. `diagnostics` (`:384-390`) registers no `body` hook at all and the delegation report floors on
`outputDetail`, so neither is "all `outputBody`".
`test(tui): pin that every file-content tool body carries line numbers`

## Suggested version bump

Three user-visible TUI changes plus a tool-summary change: a **patch** bump (`v0.19.7`) at closeout.
The owner decides; no item here touches `VERSION`.
