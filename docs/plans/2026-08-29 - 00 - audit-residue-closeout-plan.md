# Audit-residue closeout — implementation plan

**Goal:** close every finding the two code audits (`docs/reviews/code-audit-2026-08-25.md`,
`docs/reviews/code-audit-2026-08-28.md`) left open after plans `2026-08-26 - 01…05`,
`2026-08-28 - 02` and `2026-08-28 - 03` ran: the four `ISSUES.md` "Audit residue" entries
(C-13, C-04, C-20/F-08, C-15), the two architecture candidates parked beside them (S-1, S-2), and
the one residual the 2026-08-28 fixes run deferred (`Provider.Suggest`'s doc). After this plan the
"Audit residue" section and the "Residuals deferred out of the 2026-08-28 code-audit fixes run"
section of `ISSUES.md` are gone, and every audit finding of both reviews is either fixed or
recorded as closed-by-decision in `CHANGELOG.md`. 15 items; CHANGELOG entries land at the closeout
from the sidecars, per the skill.

**Date:** 2026-08-29
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `docs/reviews/code-audit-2026-08-25.md` — C-04 (§"The complexity classifier's phrase cap
  saturates"), C-13 (§"The library store rewrites the whole file on disk while holding its write
  lock"), C-15 (§"The terminal measurement engine has no test against an answering terminal"),
  C-20 (§"Windows `tokenConfiner.Close` races `Confine` on an abnormal exit").
- `ISSUES.md` §"Audit residue (2026-08-25 refocus / security / code audits)" — the C-13 / C-04 /
  C-20+F-08 / C-15 entries and the S-1 / S-2 architecture candidates (lines 772–829 at the base
  commit); §"Residuals deferred out of the 2026-08-28 code-audit fixes run" (the `Provider.Suggest`
  doc, lines 75–88).
- `AGENTS.md` (Bypass floor, regressions-never-deferred, ISSUES/CHANGELOG convention: a resolved
  item is REMOVED from `ISSUES.md` and recorded in `CHANGELOG.md`); ADR 0011 (value-copied TUI
  Model — not touched here), ADR 0015 (`Deps` derived from Config; `GrammarConstraint` "remains the
  inert false seam" — superseded by item 14), ADR 0016 §6 (the first Validated set is pinned
  verbatim), ADR 0020 (the Windows token backend; §2 journal-first labelling and crash recovery),
  ADR 0039 (parallel sub-agents — the fan-out that makes C-13 bite), ADR 0043 (the composition
  root split by seam — the production side S-1 mirrors).
- `docs/design/mechanism-catalogue.md` (Tables A/B/C rows for `grammar`; the gemma Validated-set
  row at line 504), `internal/validated/shipped.json` + `shipped_test.go` (the pinned set).
- Line numbers below were verified on 2026-08-29 against `main` at `21a7a5a2`; the symbol name is
  the anchor, the number a hint.

**Ratified design calls (owner, 2026-08-29, via the plan-writer's questions):**
- **Scope:** C-13, C-20/F-08, S-1, S-2 are in; C-15 and the `Provider.Suggest` doc are in (test-only
  / one-line doc, no call needed).
- **C-04 is closed as by-design, not recalibrated.** The audit's proposed fix ("sum per-match
  points, cap the category total afterwards") is arithmetically identical to
  `decomposeCountPhraseMatches` as written — the only lever that changes behaviour is the
  calibration `perMatch = cap/2`, which is the apogee-sim `countPhraseMatches` @pin. The plan pins
  the intent in the code and a table test, removes the `ISSUES.md` entry, and the CHANGELOG records
  it closed as designed; any recalibration stays an apogee-sim bench question.
- **C-13 write model: an async coalescing writer.** `Record` / `RecordSuccess` mutate memory and
  mark the store dirty; one writer goroutine debounces and writes a snapshot; `Store.Close()`
  flushes with a bounded wait and is wired into `Agent.Close`. A caller never touches the disk.
- **C-20/F-08 proof: a `windows-latest` CI test job** running the Windows-tagged platform tests on
  every push, so the race test and the revert test are not a manual owner step.
- **S-2: delete the grammar Mechanism** and `Deps.GrammarConstraint`; drop `grammar` from the
  shipped gemma Validated set; a config that still names the retired ID is tolerated with a notice,
  never refused (regression guard, item 14).
- **S-1: split `cmd/apogee/wire_test.go` by production seam** in four mechanical moves; no test is
  rewritten, renamed or dropped.

**Regression check (2026-08-29, base `21a7a5a2`):** four independent read-only reviewers
(items 1–4, 5–6, 7–9, 10–15). 13 of 15 items amended; no finding rejected.
- 2 GUARD — the phase category saturates on the THIRD match (`decompose.go:363` is `2, 6`, not
  cap/2): the doc and table now say so; the ISSUES range corrected to 787–793 (786 is C-13's).
- 3 GUARD — the agreeing fake had `rep` true under an `xterm*` TERM, which `xtermCaps` drops
  (`terminal.go:1055`) → a capabilities mismatch; now `rep: false`. The DECRQM reply byte string
  was not a DECRPM (`…$y`, not `…R$y`); fixed.
- 4 GUARD — the assertion string is `xtermCaps(TERM=(unset))` (`orUnset`); the "only section
  flagged" tests inherit item 3's fix.
- 5 REGRESSION → guard folded: the 2s bound now spans the WHOLE of `Close` (stop + join + flush,
  abandoned on timeout) — the draft joined the writer unboundedly first; `Close` recast as
  flush-and-park (the writer restarts on the next Record) so a shared instance can be closed by
  any holder; `TestStoreLoadIgnoresStalePersistTempFile`, `benchreadiness_test.go:447`,
  `mechanisms/library_test.go`'s 13 stores and `library_bypass_test.go:40` added to the guard; the
  failure-notice test kept synchronous.
- 6 REGRESSION → recast (keeps the ratified intent): three sites build a store on one
  `LibraryDir` (`construct.go:375`, `rebind.go:222`, `BuildMechanisms` for routed children at
  `delegation.go:497`) and only the first was reachable — now `library.Open(dir)` is one shared
  instance per process and directory, `deriveDeps` uses it, the Agent holds and flushes it at
  `Close`; the no-bite child test replaced by rebind and routed-catalogue tests that bite.
- 7 GUARD — `prewarm_windows.go:32-35` is a second unsynchronised reader (added to Files, same
  lock + check); a latched `closed` must not skip `journal.Retire()`, which `session.go:189-191`
  designs to converge on repeat — Close is repeatable, only the token close latches.
- 8 REGRESSION → recast: the draft judged the label AFTER `ClearTree`, where apogee's own work
  reads unlabelled (`TestWindowsForeignPriorLabelIsRestoredOnTeardown` would go red); now judged
  BEFORE the clear, hand-off entries judged once and marked `Judged` on the journal, the OS read
  kept out of the pure `restorablePriors` (cross-OS tables), the doc sentence moved to
  `walk_windows.go` (`doc.go` has no recovery paragraph).
- 9 GUARD — `internal/probe` has no Windows-tagged test; the job comment no longer claims the
  console read-back is covered. (The executor confirms `GOOS=windows go vet` passes today and
  `make check` carries no Windows vet yet.)
- 10–13 GUARD — `bc` is not on the executor (`awk` sums instead); twelve unlisted top-level
  declarations (the `applySettingSpy` family, `fakeSwitcher`, `fakeStamper`, `stubPresenter`,
  `fakeKnown`, …) are now named; the shared-use rule greps all of `cmd/apogee/*_test.go`
  (`captureStderr`, `assertFiringScratchDir` have users outside `wire_test.go`);
  `TestMechanismIDs*` files beside `mechanismIDs` in `wire_tools.go:262`, matching item 14.
- 14 REGRESSION → guard folded with an explicit supersession: ADR 0016 §"whole set or nothing"
  (`:118-125`) would skip a user's saved set WHOLE over the retired ID — the item now amends ADR
  0016 (a RETIRED, inert-by-construction ID is dropped with a notice; unknown IDs still skip the
  whole set) and §6's "pruned 16"; `retired.go` needs `doc.go`'s file map
  (`TestDocMapNamesEveryFile`); the notice is a pure helper printed only pre-TUI, since
  `mechanismIDs` also runs on the live apply path and per delegate.
- 15 GUARD — the `golangci-lint` count is 2 (title + preserved body line).
- **Re-check round (items 5, 6, 8):** 5 GUARD — the bench file is the repo-root
  `benchreadiness_test.go` (package `apogee_test`), added with its own Acceptance line; the
  cleanup-Close rule extended to every recording store (`store_test.go`, all 17 in
  `library_test.go`, `readlistfamilies_test.go`); `persistDebounce` / `closeFlushTimeout` are
  test seams. 6 GUARD — the degrade notice STAYS in `deriveDeps` (ADR 0015 `:156-167`,
  `construct.go:335-343`); `Open` returns `Load`'s error on the constructing call only; the
  child-close test uses the debounce seam instead of racing a timer. 8 REGRESSION → owner call
  (2026-08-29): fold the guard — a restore that FAILS and is retried on a later run would read
  the path unlabelled after the first pass's `ClearTree`, so every entry judged Low is written
  back `Judged: true` BEFORE the clear, not only hand-off entries; ISSUES range corrected to
  794–800. No finding rejected in either round.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Per-item Acceptance is targeted; `make check` runs once, at the closeout.
- No item changes `VERSION`, a CHANGELOG release heading, or a tag.
- Each item's sidecar CHANGELOG entry names the `ISSUES.md` entry it closes (its bold title, or the
  S-n label) so the closed trail is greppable. The item that closes an `ISSUES.md` entry removes
  that entry in the same commit; item 15 removes the two section headings once they are empty.
- Windows-tagged tests (`//go:build windows`) cannot run on the Linux executor. Items 7 and 8 prove
  their Windows half by (a) `GOOS=windows go vet ./internal/platform/...` on the executor and (b)
  the CI job item 9 adds, which the closeout's push exercises. Their Linux-runnable halves (pure
  seams, table tests) are what the executor's Acceptance runs.

**Out of scope:**
- Recalibrating the decompose classifier (C-04's only behaviour-changing fix) — apogee-sim evidence.
- Everything else under `ISSUES.md` "Parked / deferred work" and "Improvements / Ideas".
- The Windows Auto `%TEMP%` residue, code signing, and the four irreducible test-driver claims.
- Teaching `internal/probe`'s terminal probe a screen read-back on non-Windows hosts (the scripted
  terminal of items 3–4 runs with `Fd: 0`, so ECH/ICH stay "unverified" there, as in production).
- A `response_format` / grammar capability on the provider wire (deleted, not wired — item 14).

---

## 1. `Provider.Suggest`'s doc says what it actually guarantees — ✅ DONE (2026-08-29)

NOTES (2026-08-29): the now-empty "### Residuals deferred out of the 2026-08-28 code-audit fixes run" heading and its Status paragraph were left in `ISSUES.md` for item 15, per the plan's standing requirement that item 15 removes the two section headings once they are empty.

**What.** `internal/skills/provider.go:126-128`: the comment claims `Suggest` "reads the SAME
snapshot List does". It does not — every accessor takes its own `p.current()` load (`:105`), and
`Report` (`:120-124`) exists precisely because `List` + `Skipped` can straddle a `Reload`. Reword
the comment to the guarantee that holds: `Suggest` ranks the snapshot in force at the moment it is
called, and a suggestion is always resolvable by the loop through `ResolveSkills` against whatever
catalog the last `Reload` installed — per call, not paired with a preceding `List`. No paired
accessor is added (no caller needs one; the ISSUES entry says so). Remove the `ISSUES.md` entry
**`Provider.Suggest`'s doc still claims it reads "the SAME snapshot List does"** (lines 80–88).

**Files:** `internal/skills/provider.go`, `ISSUES.md`

**Tests.** None — a comment. `go vet ./internal/skills/` proves the file still builds.

**Acceptance.** `go vet ./internal/skills/ && go test ./internal/skills/ -count=1`;
`grep -c 'SAME snapshot List does' internal/skills/provider.go` → `0`;
`grep -c "Provider.Suggest" ISSUES.md` → `0`.

**Commit.** `docs(skills): Provider.Suggest's doc states the per-call snapshot guarantee it keeps`

## 2. The decompose phrase cap's saturation is pinned as the calibration it is (C-04) — ✅ DONE (2026-08-29)

NOTES (2026-08-29): the audit-example assertion (score 14, `decomposeComplex`) is a subtest of `TestDecomposePhraseCategorySaturatesOnTheSecondMatch` rather than a second test function, since the item's Tests line names that one test; its own doc comment carries the "this is the pinned calibration, changing it is an apogee-sim recalibration" statement alongside the function-level one.

NOTES (2026-08-29): the C-04 bullet was ISSUES.md lines 777-783 at the working tree (the plan's 787-793 counted from the base commit, before item 1 removed the `Provider.Suggest` residual); the preceding C-13 bullet and the following C-20+F-08 bullet both survive intact.

**What.** Closes audit finding C-04 as by-design (ratified call). `internal/mechanisms/decompose.go`
`decomposeCountPhraseMatches` (`:384-396`) and its four call sites (`:351-366`): delegation
(`4, 8`), conditional (`3, 6`) and review (`2, 4`) saturate on the SECOND matching phrase, phase
markers (`2, 6`) on the THIRD — so a category contributes "absent / one or two signals /
saturated" and never an open count — the apogee-sim `countPhraseMatches` @pin calibration. (The
audit's "every caller passes `perMatch = cap/2`" is its own miscount: the phase site does not.) Extend the function's doc comment to say exactly that, why
(a category is evidence of a KIND of structure, not a tally — three "delegate" spellings in one
prompt are one signal), and that the audit's "sum then cap" rewrite is the same function. No code
change to the scoring; no threshold moves; the Bypass floor is untouched.

Add a table test in `internal/mechanisms/decompose_test.go` pinning the calibration:
`decomposeCountPhraseMatches` over 0 / 1 / 2 / 3 / 4 matching phrases for the delegation category
(`perMatch 4, cap 8`) → `0, 4, 8, 8, 8` and for the phase category (`perMatch 2, cap 6`) →
`0, 2, 4, 6, 6`; and `decomposeAssessComplexity` on the audit's own example —
a short prompt with two delegation phrases and two conditional phrases — asserting the score is
`8 + 6 = 14` and the level `decomposeComplex`, with the test's doc comment stating that this IS the
pinned behaviour and that changing it is a recalibration for apogee-sim. Remove the `ISSUES.md`
entry **C-04 — the decompose scoring cap saturates** (the bold title is the anchor: the bullet
runs from the title line through "…Not independently verified." — lines 787–793 at the base
commit; the preceding C-13 bullet ends at 786 and must survive).

**Files:** `internal/mechanisms/decompose.go`, `internal/mechanisms/decompose_test.go`, `ISSUES.md`

**Tests.** `TestDecomposePhraseCategorySaturatesOnTheSecondMatch` (the two tables above).

**Acceptance.** `go test ./internal/mechanisms/ -run 'TestDecompose' -count=1`;
`grep -c 'C-04' ISSUES.md` → `0`.

**Commit.** `test(mechanisms): pin the decompose phrase-cap saturation as the sim calibration (C-04)`

## 3. A scripted terminal answers the probe; an agreeing terminal measures clean (C-15, half 1) — ✅ DONE (2026-08-29)

NOTES (2026-08-29): the "unverified — no screen read-back on this OS" cell the item calls the tabs section's third row is `Rows[3]` — the fourth listed, after the two tab-stop rows and the DECST8C row; the test asserts it through a small `observedFor` helper that finds the row by its "tab over written cells" label, so a row inserted ahead of it cannot silently move the assertion.

NOTES (2026-08-29): `terminal_fake_test.go` is 337 lines, of which 230 are code — the item's "~250 lines" budget is met on code lines, with the balance the doc prose every file in this package carries.

NOTES (2026-08-29): `TestFakeTerminalAnswersCPRAndDECRQM` gained a third case beyond the item's two — a CSI written in two halves is answered only once it completes — because the "handles a trailing partial sequence by holding it until the next write" rule the item requires of the fake is otherwise unexercised: the probe formats every sequence in one write.

**What.** `internal/probe/terminal.go` `GatherTerminal` (`:290-331`) is tested only on its two
abort paths (`terminal_test.go:244-300`); every section — `measureModes`, `measureGlyphs` ×2,
`measureTabs`, `measureWrap`, `measureCapabilities` — is unexercised (package coverage 50.4%). Add
a scripted terminal test double in a new `internal/probe/terminal_fake_test.go`: a `fakeTerminal`
whose `Out` side parses what the probe writes and whose `Read` side returns the replies a real
terminal would. It is a small VT model, not a full emulator — it tracks only the cursor and the
things the probe measures:

- **Cursor state:** row/col, 1-based; `Width`/`Height` from the inputs. It honours CUP (`CSI r;cH`),
  CHA (`CSI nG`, when `caps.cha`), VPA (`CSI nd`, when `caps.vpa`), EL (`CSI 2K`, no move), ED
  (`CSI 2J`, no move), and ignores the private modes `?1049`, `?25`, and DECST8C (`CSI ?5W`) except
  as below. Printable text advances the column by `widthOf(text)`, a per-fake func; `\t` advances
  to the next tab stop of the fake's `stops` slice (default: every 8 from column 9; a stop past
  `Width` clamps to `Width`).
- **Wrap:** with `wrap: deferred` (default) a character written at column `Width` leaves the cursor
  at `Width` with a pending flag; the next printable clears the flag, moves to `(row+1, 1)` and
  then advances. With `wrap: immediate` the character at column `Width` moves the cursor to
  `(row+1, 1)` at once.
- **DSR-CPR** (`CSI 6n`): queues `ESC[row;colR`. **DECRQM** (`CSI ?m$p`): queues the DECRPM
  reply `ESC[?<m>;<v>$y` — final byte `y` with the `$` intermediate, exactly what `scanModeReport`
  (`terminal.go:1005`) matches — where `v` comes from the fake's `modes map[int]int` (absent → `modeNotRecognized`, i.e. 0;
  `answersDECRQM: false` queues nothing). **SM/RM `?2027h/l`:** with `honours2027: true` sets
  `modes[2027]` to `modeSet`/`modeReset`; with `false` leaves it as found (the "acknowledges but
  does not take" terminal). **DECST8C:** with `resetsStops: true` sets `stops` to every-8.
- **REP** (`CSI nb`): with `caps.rep` advances the column by `n` (the last printable's width is
  1 in every probe use). **ECH/ICH:** no cursor effect (matches the real thing; the read-back is
  `Fd`-gated and the fake runs with `Fd: 0`).
- **Read:** returns the queued reply bytes and clears the queue; a `chunk int` knob > 0 returns at
  most `chunk` bytes per call (item 4 uses it). With nothing queued it returns `nil` at once
  (the probe's timeout is what a real silent terminal costs; the fake never sleeps).
- **Recording:** every byte written is kept in `written` so a test can assert the screen was
  restored (`showCursor + leaveAltScreen` last) and mode 2027 was put back.

The fake exposes `inputs(term string) TerminalInputs` (Out = the fake, Read = its Read,
`Width: 120, Height: 29, Timeout: time.Millisecond`, `Fd: 0`). Its parser reuses `nextCSI`
(`terminal.go:932`) — the package's own CSI lifter — rather than a second one, and handles a
trailing partial sequence by holding it until the next write. Keep the fake under ~250 lines;
it has one reason to change (the probe asks a new question).

Then add `TestGatherTerminalMeasuresAnAgreeingTerminal` in `terminal_test.go`: a fake with
`widthOf = ansi.WcWidth.StringWidth` while `modes[2027] == modeReset`, and
`ansi.GraphemeWidth.StringWidth` once `2027` is set (honours2027: true), `modes[2026] = modeReset`,
every-8 stops, deferred wrap, `cha` and `vpa` true, **`rep` false**, `TERM = "xterm-256color"`.
`rep` must be false: `xtermCaps` drops `capREP` for every `xterm*` TERM (`terminal.go:1055`, pinned
by `TestTerminalXtermCapsMirrorsTheRenderer` at `terminal_test.go:163`), and the capabilities
section flags a capability the terminal HAS that the painter does not use — an agreeing fake is
one that offers exactly what the painter expects. Assert:
`report.Aborted == ""`; exactly 6 sections in the order modes, glyphs(off), glyphs(on), tabs, wrap,
capabilities; `report.Mismatch() == false`; each section's `Summary` is the exact OK wording from
`terminal.go` (`"mode 2027 reads back as set after CSI ?2027h"`, `"every glyph advances by the
width the painter's wcwidth table predicts"` / `"… grapheme table predicts"`, `"tab stops are every 8
columns and a tab moves without erasing"`, `"the terminal holds a pending wrap at the last column —
the semantics the renderer emits against"`, `"the painter's capability set matches what the terminal
actually does"`); the tabs section's third row reads `unverified — no screen read-back on this OS`;
`written` ends with `showCursor + leaveAltScreen`; and mode 2027 was restored to reset (the last
`?2027` request written is `l`, because `restoreMode2027` puts a found-reset mode back).

**Files:** `internal/probe/terminal_fake_test.go`, `internal/probe/terminal_test.go`

**Tests.** `TestGatherTerminalMeasuresAnAgreeingTerminal` (above) plus a fake self-test
`TestFakeTerminalAnswersCPRAndDECRQM` (write `CSI 3;7H` then `CSI 6n` → Read yields
`ESC[3;7R`; `CSI ?2027$p` → `ESC[?2027;2$y` — the same byte string the DECRQM bullet above
specifies).

**Acceptance.** `go test ./internal/probe/ -run 'TestGatherTerminal|TestFakeTerminal' -count=1 -race`;
`go test ./internal/probe/ -cover -count=1` reports ≥ 70% (from 50.4%).

**Commit.** `test(probe): a scripted terminal answers the probe and an agreeing one measures clean (C-15)`

## 4. A diverging terminal is caught, section by section; a split reply is waited for (C-15, half 2) — ✅ DONE (2026-08-29)

NOTES (2026-08-29): `internal/probe/terminal_fake_test.go` is on the item's Files list but needed no change — item 3 already gave the fake every knob this item names (`chunk`, `resetsStops`, and `widthOf` as an overridable func field), which the item's Tests line anticipates ("if item 3 did not already add them"). The per-glyph width override is therefore installed by the glyph test as a closure over the fake rather than as a new field.

NOTES (2026-08-29): the capabilities inverse ("TERM grants what the terminal lacks → no mismatch") is a second block inside `TestGatherTerminalFlagsACapabilityThePainterDoesNotKnow` rather than a ninth function, which is what makes the item's "the eight above" count hold; the DECST8C-moved complementary is its own function, as the item lists it.

NOTES (2026-08-29): three shared test seams were added beside `observedFor` because the "only this section is flagged" assertion the item requires appears in five tests: a `modesSection…capsSection` index const block naming GatherTerminal's six sections in append order, `onlyMismatch(t, report, index)` (asserts the run completed, six sections came back, and exactly the section at index diverged, returning it), and `flaggedLabels(section, col)` (names the flagged rows by one column, so a test asserts WHICH rows carry the finding).

**What.** Depends on item 3. Each section's MISMATCH branch is what the probe exists for, and none
is exercised. Add to `internal/probe/terminal_test.go`, each on its own fake so one divergence
cannot mask another:

Every "everything else agreeing" fake below is item 3's agreeing fake (`rep` false under an
`xterm*` TERM), so the "only section flagged" assertions hold.

- `TestGatherTerminalFlagsAnImmediateWrap` — `wrap: immediate`, everything else agreeing:
  `Mismatch()` true; the wrap section is the only section with `Mismatch`, its `Summary` contains
  `"WRAPPED IMMEDIATELY"`, and its first row's `Flagged` is true.
- `TestGatherTerminalFlagsAModeThatDoesNotTake` — `modes[2027] = modeReset`, `honours2027: false`:
  the modes section is flagged with the Summary beginning `"the terminal answers DECRQM for mode
  2027 but CSI ?2027h does not take"`; the glyph(on) section's title says the terminal reports
  `reset` and measures against wcwidth (no glyph mismatch).
- `TestGatherTerminalFlagsStopsDECST8CDoesNotMove` — stops every 4 and `resetsStops: false`: the
  tabs section is flagged, Summary `"the terminal's tab stops are not the every-8 model the renderer
  moves against"`, and the DECST8C row says `no change`. And the complementary
  `TestGatherTerminalReportsStopsDECST8CMoved` — stops every 4, `resetsStops: true`: no mismatch,
  DECST8C row says `moved the stops`, the after-row equals `everyEightStops(120, n)`.
- `TestGatherTerminalFlagsACapabilityThePainterDoesNotKnow` — `TERM: ""` (xtermCaps grants
  nothing), `caps.cha` true: the capabilities section is flagged on the CHA row and the Summary
  contains `"xtermCaps(TERM=(unset))"` — `orUnset` (`terminal.go:280-285`) renders the empty TERM as
  `(unset)`, parentheses included; VPA and REP rows are flagged too when supported. And the
  inverse: `TERM: "xterm-256color"` with `caps.cha/vpa/rep` all false → no capability mismatch (the
  probe flags only what the terminal HAS that the painter does not use).
- `TestGatherTerminalFlagsAGlyphTheTableMisses` — `honours2027: true` but `widthOf` measures the
  two VS16 glyphs (`⚠️`, `ℹ️`) as 1 column under grapheme mode: only the glyph(on) section is
  flagged, on exactly those two rows.
- `TestGatherTerminalSurvivesRepliesSplitAcrossReads` — the agreeing fake with `chunk: 1`
  (one byte per Read): the report is identical (section count, `Mismatch() == false`, every
  Summary) to the unchunked run — `await` (`terminal.go:357`) must hold a half-arrived reply.
- `TestGatherTerminalRestoresAModeItFoundSet` — `modes[2027] = modeSet` at start: the last
  `?2027` request written is `h` (the probe leaves a found-set mode set), and the probe's glyph
  sweep still ran the OFF pass first.

Remove the `ISSUES.md` entry **C-15 — `GatherTerminal`'s measurement engine is untested past its
two abort paths** (lines 804–807) and the "Test debt:" heading above it if nothing else remains
under it.

**Files:** `internal/probe/terminal_test.go`, `internal/probe/terminal_fake_test.go`, `ISSUES.md`

**Tests.** The eight above. The fake may gain knobs (`chunk`, `resetsStops`, a per-glyph width
override) if item 3 did not already add them.

**Acceptance.** `go test ./internal/probe/ -run 'TestGatherTerminal' -count=1 -race`;
`go test ./internal/probe/ -cover -count=1` reports ≥ 80%; `grep -c 'C-15' ISSUES.md` → `0`.

**Commit.** `test(probe): every terminal-probe section is proven to catch its divergence (C-15)`

## 5. The library store writes off the caller's path: an async coalescing writer (C-13, store half) — ✅ DONE (2026-08-29)

NOTES (2026-08-29): the snapshot is taken under the full write lock (`mu.Lock`), not the `RLock` the item names — clearing `dirty` is a write, and the item requires the snapshot and the flag to move in the same critical section.

NOTES (2026-08-29): `TestStorePersistFailureNoticeCarriesOnePrefix` also became `t.Parallel()`: it no longer swaps the process-global `os.Stderr` (the reason it was sequential), and it sinks the writer's notice through the new `notify` seam so the async path prints nothing into the test's stderr.

NOTES (2026-08-29): `TestStoreCloseBoundsAHungWriter` joins the abandoned writer in its cleanup (the write seam signals a `resumed` channel after it is released). Without that join the abandoned goroutine's read of the package-level `persistDebounce` is unordered against the next sequential test's move of the seam, and `-race` flags the global.

NOTES (2026-08-29): the seams are unexported package vars, exactly as the item names them (`persistDebounce`, `closeFlushTimeout`), so item 6's `internal/agent` child-close test cannot set them from another package — item 6 will need its own hook (or an exported one added there).

NOTES (2026-08-29): production shutdown does not flush the store yet — no Driver calls `Store.Close`; item 6 (the engine half) is what holds the store on the Agent and flushes it at `Agent.Close`, and it also removes the C-13 `ISSUES.md` entry, which this item deliberately leaves in place.

**What.** Closes audit finding C-13 (ratified: async coalescing writer). `internal/library/store.go`
`Record` (`:138-176`) and `RecordSuccess` (`:181-192`) call `persist()` (`:273-297`: sort +
`MarshalIndent` + `atomicWrite`) under `s.mu.Lock()`. Under ADR 0039 fan-out every sub-agent's
post-response hook serialises behind a full file rewrite, and a hung filesystem hangs the loop.
Restructure:

- `Record` / `RecordSuccess` mutate the map under the lock, set `s.dirty = true`, and signal the
  writer (`s.wake`, a 1-buffered `chan struct{}`, non-blocking send). They return at once.
- **One writer goroutine**, started lazily by the first mutation (`sync.Once`) — a store that is
  only `Load`ed or `Query`ed never spawns it. It loops: wait on `wake` or `s.stop`; on wake, sleep
  the debounce (`persistDebounce = 200 * time.Millisecond`, a const) so a burst of Records
  coalesces into one write; then `snapshot()` under `RLock` (the sorted `[]Entry` VALUE copy —
  never the `*Entry` pointers, which the next Record mutates) with `dirty` cleared under the same
  lock; then encode + `atomicWrite` OUTSIDE any lock. A write failure is still the soft stderr
  notice `persist` prints today (same wording, same one-prefix rule `TestStorePersistFailureNoticeCarriesOnePrefix`
  pins) and re-marks `dirty` so the next flush retries.
- `Flush() error` — synchronous: if dirty, snapshot + write NOW on the caller's goroutine (through
  the same `writeSnapshot` the goroutine uses, serialised by a `writeMu` so the two never interleave
  rename order), returning the write error instead of printing it. Tests use it.
- `Close() error` — **flush and park**, idempotent, safe to call repeatedly and safe to reuse
  after: it asks the writer to stop, joins it, and flushes — ALL of that under ONE deadline,
  `closeFlushTimeout = 2 * time.Second`, run on a helper goroutine `Close` waits on with a
  `select`; past the deadline `Close` returns `errFlushTimedOut` ("apogee: library store: flush did
  not finish in 2s; the last observations are not on disk") and ABANDONS the helper (a writer
  parked inside `atomicWrite` on a hung filesystem is never joined — today `Close` does not exist
  and shutdown touches no disk, and after this item it still cannot hang). The flush must never
  wait on `writeMu` unboundedly: it is the helper goroutine, not `Close`'s caller, that takes it.
  After `Close` the store is parked, not dead: a later `Record` marks dirty and RESTARTS the writer
  (the lazy start is a "not running" state, not a `sync.Once`), and the next `Close` flushes again —
  so a store instance shared across sessions or catalogues (item 6) loses nothing to being closed
  early. `Store` satisfies `io.Closer`.
- `persist()` is deleted; the "under the caller-held write lock" comments go with it. The
  package doc (`doc.go`) states the write model in two sentences: observations reach disk within
  the debounce window or at `Close`; a process that exits within 200ms of its last observation
  without `Close` loses that observation, which is the accepted cost of a best-effort store.
- Standards that shape this: one deep module — the write model is entirely inside `Store`; callers
  see `Record`/`Flush`/`Close` and nothing about goroutines. Test through the public surface
  (`Flush`, `Close`, `Load`), never by reaching into `dirty`.

**Regression guard.** Every existing `store_test.go` test that reads `library.json` straight after
`Record` (`TestStoreRecordRoundTrip`, `TestStoreWritesStayInsideInjectedDir`,
`TestStorePersistLeavesNoTempFile`, `TestStorePersistReplacesRatherThanTruncates`,
`TestStorePersistFailureNoticeCarriesOnePrefix`, `TestStoreRecordSanitizesContent`,
`TestStoreRecordStripsFormatCharacters`, and `TestStoreLoadIgnoresStalePersistTempFile` —
`store_test.go:297-340`, whose `Load` at `:320` follows the Records at `:303-304` and whose second
`Load` at `:334` follows the `RecordSuccess` at `:329`) gains a `Flush()` before each such read.
`TestStorePersistFailureNoticeCarriesOnePrefix` stays SYNCHRONOUS: it asserts the one-prefix rule
on `Flush()`'s returned error, never by racing the writer goroutine's `os.Stderr` write against
`captureStderr`'s restore (`store_test.go:358-371` — `-race` would flag it). The stderr notice the
goroutine prints goes through one unexported `notify func(error)` on the Store (default: print to
`os.Stderr`) so a test can install a sink before the first Record if it needs the async path.
`internal/agent/library_bypass_test.go`: the seed store gets a `Close()` immediately after its two
Records (`:36-37`) — its FIRST disk read is the `os.ReadFile` at `:40`, before the second
`NewStore` at `:50`. `internal/mechanisms/library_test.go` constructs 13 stores that are never
flushed: give each a `t.Cleanup(func() { _ = store.Close() })` via one small helper in that file, so
no writer outlives its `t.TempDir()`. `internal/agent/benchreadiness_test.go:447-449` stats
`library.json` a few ms after the mechanisms arm's last Record and before its deferred `Close`
(`:373`): make that stat POLL (up to 2s, 10ms steps) for the file — the write is asynchronous by
design now, and that file may not import `internal/library` (its header rule, `:10-16`), so a
`Flush` is not available to it. That file is the repo-ROOT `benchreadiness_test.go` (package
`apogee_test`, `TestBenchReadinessContract`) — not under `internal/agent`.
- **Test seams (binding):** `persistDebounce` and `closeFlushTimeout` are both package-level
  `time.Duration` variables (not consts) so a test can shorten them; every timing assertion in
  items 5 and 6 ("Record returns before the file exists", the bench stat's poll, the child-close
  test) sets the debounce seam, never sleeps against the 200ms default.
- **Every recording store in a test closes on cleanup:** the `t.Cleanup(func() { _ = store.Close() })`
  rule applies to `internal/library/store_test.go` (the Records at `:54,128,198,226,262,302,418,463,494`),
  to all 17 `library.NewStore` calls in `internal/mechanisms/library_test.go`
  (`:48,80,93,132,158,192,214,236,267,288,305,344,414,465,533,560,561`) and to
  `internal/mechanisms/readlistfamilies_test.go:48` (its `PostResponse` Records) — a writer that
  fires after `t.TempDir` is removed recreates the tree and fails the cleanup's `RemoveAll`.

**Files:** `internal/library/store.go`, `internal/library/doc.go`, `internal/library/store_test.go`,
`internal/agent/library_bypass_test.go`, `benchreadiness_test.go` (repo root, package `apogee_test`),
`internal/mechanisms/library_test.go`, `internal/mechanisms/readlistfamilies_test.go`

**Tests.** `TestStoreRecordDoesNotTouchTheDiskOnTheCallersPath` (an `atomicWrite` seam or a
read-only dir: `Record` returns before the file exists; `Flush` makes it exist);
`TestStoreCoalescesABurstIntoOneWrite` (50 Records, then `Flush`; count the temp-file renames via a
counting seam or by observing `library.json`'s mtime/inode changes ≤ 2); `TestStoreCloseFlushesAndIsIdempotent`;
`TestStoreCloseBoundsAHungWriter` (a write seam that blocks on a channel; `Close` returns
`errFlushTimedOut` within ~2.5s; use a shortened timeout seam so the test runs in milliseconds);
`TestStoreFlushRetriesAfterAFailedWrite`; `TestStoreRecordAfterCloseRestartsTheWriterAndTheNextCloseFlushes`;
the existing tests amended per the guard. All under `-race`.

**Acceptance.** `go test ./internal/library/ ./internal/agent/ ./internal/mechanisms/ -count=1 -race`;
`go test . -run TestBenchReadinessContract -count=1 -race`;
`grep -c 'func (s \*Store) persist()' internal/library/store.go` → `0`.

**Commit.** `fix(library): the store writes through a coalescing writer, never on the caller's path (C-13)`

## 6. One library store per process and directory; the Agent flushes it at Close (C-13, engine half) — ✅ DONE (2026-08-29)

NOTES (2026-08-29): Driver enumeration, as the item requires — the TUI root reaches `Agent.Close` through `lateEngine.Close` (`cmd/apogee/wire_engine.go:488`), called by `rootWiring.close` (`cmd/apogee/wire.go:227`), the single deferred teardown `runRoot` registers; `internal/run.Once` (`run.go:230`) defers `_ = a.Close()` on the line after construction, covering headless, daemon Firings and an in-session Schedule. Both were already correct, so no `Close` call was added anywhere.

NOTES (2026-08-29): the four engine-side tests live in a new `internal/agent/library_lifecycle_test.go` rather than in `agent_test.go` / the construct/rebind test files the item's Files line offers, because they are one concern cluster spanning construction, Rebind, `BuildMechanisms` and a spawn — and the package already files Library work that way (`library_bypass_test.go`, `library_corrupt_store_test.go`). No production file was added, so `TestDocMapNamesEveryFile` is unaffected.

NOTES (2026-08-29): `TestChildAgentDoesNotCloseTheStore` does NOT set item 5's `persistDebounce` seam — item 5's own notes record that the seam is an unexported package var no other package can reach, and exporting a test-only setter from production code would be the worse trade. The test instead plants a regular FILE where the store's directory would go, which makes every flush fail loudly (`MkdirAll` cannot create over it): a delegate's `Close` that flushed would return that error and one that did not returns nil, and freeing the path lets the session's `Close` publish the observation. That is fully deterministic rather than merely faster than a 200 ms race. All four tests were verified to bite: disabling the flush fails all four, dropping the `depth > 0` guard fails the child test, and swapping `library.Open` back for `library.NewStore` fails the three sharing assertions.

NOTES (2026-08-29): `internal/library/doc.go` gained one sentence beyond the item's Files list: the package doc's process-local paragraph said nothing about instance identity WITHIN a process, which is exactly what this item establishes, and it is the first thing a reader of the package sees.

NOTES (2026-08-29): a `Rebind` that drops the `library` arm leaves `a.library` nil, exactly as the item's text specifies ("nil when no arm needs it"), so the session's `Close` no longer flushes the store an earlier arm opened. It costs nothing in practice — with no arm armed nothing records, and the writer goroutine still publishes what was already dirty within its debounce window — and the pre-item behaviour flushed nothing at any time.

**What.** Depends on item 5. Recast at the regression check (2026-08-29): the draft assumed one
store per process reachable for `Close`; the tree builds THREE on the same `LibraryDir` — `New`
(`internal/agent/construct.go:375`, via `buildEnabledMechanisms` → `deriveDeps`), every `Rebind`
(`internal/agent/rebind.go:222` → the same `buildEnabledMechanisms`), and the host's routed
sub-agent catalogue (`cmd/apogee/delegation.go:497` → `apogee.BuildMechanisms` →
`construct.go:317`), whose store nothing can ever reach. With item 5's writer, three instances on
one `library.json` race on the rename and the last writer's snapshot silently drops the others'
observations. Binding design, keeping the ratified intent (a caller never touches the disk; the
Agent's `Close` flushes):

- **`internal/library` gains `Open(dir string) *Store`** — the per-process, per-directory shared
  instance: a package-level `map[string]*Store` under a mutex keyed by `filepath.Clean(dir)`; the
  first `Open` constructs (`NewStore` + `Load`) and returns `(*Store, error)` where the error is
  `Load`'s soft error ON THAT FIRST CALL ONLY; later `Open`s return the same pointer and a nil
  error. The degrade NOTICE stays where ADR 0015 `:156-167` and `construct.go:335-343` record it —
  in `deriveDeps` — which prints `Open`'s error exactly as `construct.go:377-383` does today; since
  only the constructing call returns it, the notice prints once per process without moving. `NewStore` stays public for a PRIVATE
  store (tests, a bench Driver seeding a fixture); `Open` is what the engine uses. Item 5's
  flush-and-park `Close` is what makes a shared instance safe: any holder may `Close` it, the
  writer restarts on the next `Record`, and nothing is lost.
- **`deriveDeps` (`construct.go:345`) calls `library.Open(cfg.LibraryDir)`** for a `needs.Library`
  arm. That ONE change makes `New`, `Rebind` and `BuildMechanisms` share the instance by
  construction — no signature changes, no `existing` parameter, `rebind.go` and the `apogee`
  facade untouched. `internal/mechanisms/library.go:126-137` (a child's `ForSubAgent` shares the
  STORE) already documents the sharing this extends to catalogues; cite it in `Open`'s doc.
- **The Agent holds the store it derives** — `buildEnabledMechanisms` returns the `Deps` it
  derived (`(mechanisms.Deps, error)`; `New` and `Rebind` keep `deps.Library` on `a.library`, nil
  when no arm needs it; `BuildMechanisms` discards it). **Files** therefore include `rebind.go` (the
  call site's new return shape) and `construct.go:320` (`BuildMechanisms`'s call).
- **`Agent.Close` (`agent.go:419-422`) flushes it**: after `closeConsoles()` (consoles are what the
  observations describe) and before `closeOwnedUpstream` (then the socket goes back); it returns
  `errors.Join` of the store's and the upstream's errors. A child Agent at depth ≥ 1 does NOT
  close the store (the parent's `Close` does; a child closing early would be harmless under
  flush-and-park, but the ownership rule is simpler to state and test). A routed child's registry
  (`delegation.go:497`) holds the same instance, so the session Agent's `Close` flushes its
  observations too.

Enumerate every Driver that constructs an Agent and confirm each reaches `Agent.Close` on its
orderly exit — the TUI root (`cmd/apogee/wire_engine.go:32`), `internal/run` `Once`
(`run.go:230`: headless / daemon Firings / an in-session Schedule) — and record each in a dated
NOTES line; a Driver found never to call `Close` gets the call added in this item (it is part of
"the Agent closes what it opened", not new scope). Remove the `ISSUES.md` entry **C-13 — the
library store persists under its lock** (lines 782–786, title through "Not independently
verified.").

**Files:** `internal/library/store.go` (`Open`), `internal/library/store_test.go`,
`internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/rebind.go`,
`internal/agent/agent_test.go` (or the construct/rebind test file the package uses), `ISSUES.md`

**Tests.** `TestOpenReturnsOneStorePerDirectory` (`internal/library`: two `Open`s of one dir are
the same pointer; a different spelling of the same dir — trailing slash — too; a different dir is
not). `TestCloseFlushesTheLibraryStore` (an Agent with a `library` arm on a temp `LibraryDir`;
drive one `Record` through the store the Agent holds — via the hook the library Mechanism fires, or
`library.Open(dir)` from the test since it IS the same instance; `agent.Close()`; `library.json`
exists and decodes to the entry — bites: pre-item the write is asynchronous and `Close` does not
flush). `TestRebindKeepsTheSessionsLibraryStore` (enable `library`, `Rebind` to another model,
`Record` through the rebound registry's store, `Close`: the entry is on disk — bites: pre-item the
rebound registry's store is a second instance nothing closes).
`TestRoutedCatalogueSharesTheSessionsStore` (`BuildMechanisms(cfg, {library})` plus `New(cfg)` on
the same `LibraryDir`; a Record through each; the Agent's `Close`; ONE file holding both entries —
bites: pre-item two instances each rewrite the whole file from their own memory, so the last
writer's file lacks the other's entry). `TestChildAgentDoesNotCloseTheStore` (with item 5's
`persistDebounce` seam set to a minute: a depth-1 child's `Close` leaves `library.json` absent and
the shared instance still recording; the parent's `Close` writes it — deterministic, never a race
against the 200ms default).

**Acceptance.** `go test ./internal/library/ ./internal/agent/ -run 'TestOpen|TestClose|TestRebindKeeps|TestRoutedCatalogue|TestChildAgent|TestLibrary' -count=1 -race`;
`go build ./...`; `grep -c 'C-13' ISSUES.md` → `0`.

**Commit.** `fix(agent): one library store per directory, flushed by the Agent's Close (C-13)`

## 7. `tokenConfiner`: one snapshot per Confine, fail-closed after Close (C-20) — ✅ DONE (2026-08-29)

NOTES (2026-08-29): `TestWindowsCloseIsRepeatable` implements the item's convergence claim as two subtests, and the handed-off entry is created by the FIRST `Close` rather than "planted between them": `Journal.Retire` reverts the IN-MEMORY record (`session.go:206-224`), so a journal file written to disk between the two `Close`es is unreachable by that same journal — only `Recover` / a fresh construction reads files. Subtest one asserts the item's plain repeat (the journal file is removed by the first `Close`, the second returns nil, the tree stays unlabelled); subtest two plants a live sibling journal claiming the root so the first `Close` hands the foreign prior off, then ends the sibling and asserts the SECOND `Close` on the same backend restores the prior verbatim and retires the journal — the convergence `session.go:189-191` promises, which a latched `closed` must not short-circuit.

NOTES (2026-08-29): `TestWindowsPrewarmAfterCloseLabelsNothing` pins the OUTCOME, not the new `closed` branch on its own — `Close` also zeroes `caps` and the token, so the pre-existing guard already refuses a post-`Close` prewarm. The `closed` check and the `RLock` are what close the window where a prewarm overlaps a shutdown IN FLIGHT, which the race test covers; this one keeps the settled state honest (no notice, no label, no journal).

NOTES (2026-08-29): the four new tests are `//go:build windows` and cannot run on the Linux executor. Proven here by `GOOS=windows go vet ./internal/platform/...` (they compile and vet clean) and `go test ./internal/platform/... -count=1 -race` (the cross-platform suite still passes); their execution is item 9's `windows-latest` CI job.

**What.** Closes audit finding C-20 — a regression class, not a regression: at
`internal/platform/confiner_windows.go` `Confine` (`:196-211`) reads `c.token` twice (`:197` and
`:209`) with no lock, while `Close` (`:217-225`) zeroes `c.token` and `c.caps`. The struct doc
(`:78-84`) states the backend "keeps no lock" because the journal has its own. On bubbletea's
abnormal exit (SIGINT, closed console) the composition root's deferred `Close` runs while a tool
goroutine is mid-`Confine`: the first read passes, `Close` zeroes, the second read stores
`Token = 0` — and `CreateProcess` starts the child UNCONFINED while the caller marks it
`confined=true`. Fix, in this file only:

- `tokenConfiner` gains `mu sync.RWMutex` and `closed bool`. `Confine` takes `mu.RLock()` for its
  whole body — the label walk included — so a `Close` in flight waits for the Confine to finish
  and the token handle is never closed under a `CreateProcess` that is about to use it. Inside,
  ONE read: `if c.closed || !c.caps.FSWrite || c.token == 0 { return ErrConfinementUnavailable… }`,
  with a distinct message for the closed case: `"%w: the Windows token backend has been closed —
  the session is shutting down"`; the token is copied to a local and that local is what
  `SysProcAttr.Token` receives.
- `Close` takes `mu.Lock()`, sets `closed`, calls `journal.Retire()`, closes and zeroes the token
  (once — the handle close latches), zeroes the caps. It is safe to call repeatedly:
  `journal.Retire()` is called on EVERY `Close`, because `winlabel/session.go:189-191` designs a
  repeated `Retire` to CONVERGE (a handed-off entry is discharged by a later call), so a latched
  `closed` must never short-circuit it — only the token close and the caps zeroing latch. The
  second `Close` returns `Retire`'s error, which is nil once the journal is gone.
- `internal/platform/prewarm_windows.go:32-35` is a SECOND reader of `tc.caps` / `tc.token` that
  calls `tc.labelBox` with no lock (the startup label prewarm): give it the same `mu.RLock()` and
  the same `closed || !caps.FSWrite || token == 0` check before `labelBox`, so a prewarm racing a
  shutdown never labels after `Close`.
- `Capabilities()` reads under `RLock`. `labelBox` / `resolveBoxRoots` are called with the read
  lock held (they take no lock of their own; the journal's lock nests inside — document the order:
  `tokenConfiner.mu` outside, `Journal.mu` inside, never the reverse).
- Rewrite the struct doc paragraph (`:78-84`) and ADR 0020's contract note: the backend keeps ONE
  lock, for the lifecycle (token + closed), and the journal keeps its own for the label record —
  add a dated "Amendment (2026-08-29)" paragraph at the end of ADR 0020 stating the closed-state
  rule: after `Close`, `Confine` refuses with `ErrConfinementUnavailable`, which contract §4
  demotes to a forced Gate — a command can fail to start during shutdown; it can never start
  unconfined.
- The console spawn (`internal/console/process.go:111`, `context.Background()`) needs no change:
  `consolePrepare` (`internal/tools/console_open.go:204-219`) calls `Confine` inside `Prepare`,
  and the refusal now propagates as `ErrConfinementUnavailable`, which `console_open.go:161-166`
  already fails closed on. State that in the ADR amendment.

**Files:** `internal/platform/confiner_windows.go`, `internal/platform/prewarm_windows.go`,
`internal/platform/confiner_windows_test.go`, `docs/adr/0020-*.md`

**Tests** (`//go:build windows`, run by item 9's CI job; on the executor they compile under
`GOOS=windows go vet`): `TestWindowsConfineRacesCloseAndNeverStartsUnconfined` — under `-race`,
one goroutine loops `Confine` on fresh `exec.Cmd`s for ~200 iterations while another calls `Close`
midway; every iteration asserts `err != nil || cmd.SysProcAttr.Token != 0`, and after `Close` every
`Confine` returns an error wrapping `domain.ErrConfinementUnavailable` whose text contains
`"has been closed"`; `TestWindowsCloseIsRepeatable` (two `Close`es; the journal file is removed
by the first, the second returns nil, and a handed-off entry planted between them is discharged by
the second — the convergence `session.go:189-191` promises); `TestWindowsCapabilitiesAfterCloseAreEmpty`;
`TestWindowsPrewarmAfterCloseLabelsNothing`.

**Acceptance.** `GOOS=windows go vet ./internal/platform/...` on the executor (the tagged files and
tests compile); `go test ./internal/platform/... -count=1 -race` on the executor (the
cross-platform suite still passes); `grep -n 'Amendment (2026-08-29)' docs/adr/0020-*.md` → one hit.

**Commit.** `fix(platform): the Windows token backend refuses after Close instead of starting an unconfined child (C-20)`

## 8. A journal's prior label is restored only where apogee's Low label still stands (F-08)

**What.** Closes security-audit finding F-08 (paired with C-20 in `ISSUES.md`). Recast at the
regression check (2026-08-29) — the draft judged the label AFTER the clear loop, where every prior
path reads as unlabelled. `internal/platform/winlabel/walk_windows.go` `revertJournal`
(`:288-301`) applies every `PriorSDDL` a journal names with `SetSDDL` unconditionally, and
`Recover` (`:327-352`) does so for any journal whose PID is dead — so a planted or corrupted
journal under `~/.apogee` makes the next `NewConfiner` WRITE labels onto arbitrary paths.
`IsLowLabel` (`sddl.go:46`) vouches only the record side (`journal.go:104-112` refuses to record a
Low prior). Add the read-side check, with the ORDER `revertJournal` already has in mind:

- **Judge before clearing.** `revertJournal` first reads the CURRENT label of every path in
  `priors` (`ReadSDDL`), THEN runs `ClearTree` over the roots, THEN restores — because the clear
  loop is what turns a path apogee labelled into an unlabelled one, and a judgement taken after it
  cannot tell apogee's work from a planted entry. A prior is restored only if its pre-clear label
  `IsLowLabel` — apogee's own mark is what makes the path ours to revert. A path whose pre-clear
  label is not Low (someone changed it, or it was never ours) is skipped and its entry DROPPED
  from the journal (nothing of ours is on it, so nothing remains to revert; keeping it would
  re-attempt forever). A path whose label cannot be read (`ReadSDDL` error other than not-exist)
  keeps its entry, as today's failure path does; a not-exist path is dropped, as today.
- **Every prior is judged once, and the verdict is on disk before anything is cleared.** Two
  paths re-visit a path AFTER `ClearTree` has already unlabelled it: a hand-off
  (`restorablePriors`, `retire.go:127-160`, restored on a later pass once the live sibling
  retires) and a RETRY (a restore that fails at `Close` — a locked file, an unparseable
  `PriorSDDL` — keeps the journal so the next run retries, `session.go:206-210`, `retire.go:10-14`,
  `walk_windows.go:312-314`, ADR 0020 §2). In both, a later read shows no Low label and a naive
  check would drop the foreign prior for good. So the judgement is recorded on the entry: `Entry`
  (`journal.go`) gains `Judged bool` (JSON `judged,omitempty`; absent in an older journal = not
  yet judged, so a journal written by v0.18.6 is still judged on its first pass). The Windows
  revert (`revertSparingLiveSiblings`, `walk_windows.go:261`, which holds `own`, the journal path)
  judges every unjudged entry with a `PriorSDDL` — restore set and hand-off set alike — BEFORE
  the clear loop, drops the non-Low ones, marks EVERY survivor `Judged`, and WRITES THE JOURNAL
  BACK with those flags before `ClearTree` runs; a later pass (hand-off or retry) restores a
  `Judged` entry without re-reading. `retire`'s own write-back (`retire.go:38-41`) carries the
  flag through unchanged.
- **`restorablePriors` and `retire` stay pure.** They are table-tested cross-OS
  (`retire_test.go:149-258` asserts the restore map) and `ReadSDDL` always errors off Windows
  (`walk_other.go:41`), so the OS read lives ONLY in the Windows `revertJournal`, which already
  receives the map they return. The pure seam this item adds is
  `priorRestorable(current string, readErr error) (restore, drop bool)` in `retire.go` —
  `(true, false)` when `readErr == nil && IsLowLabel(current)`; `(false, true)` when
  `readErr == nil` and not Low, or `os.IsNotExist(readErr)`; `(false, false)` otherwise — which
  `revertJournal` consults per entry.
- The rule is documented on `revertJournal` and `Recover` in `walk_windows.go` (the package doc
  `doc.go` has no recovery paragraph to anchor it): "a prior is restored only onto a path that
  still carried the Low label this session (or a dead one) wrote, judged before the clear".

Remove the `ISSUES.md` entry **C-20 + F-08 — the Windows pair** (lines 794–800; 793 is C-04's
last line, removed by item 2) — both halves are
now fixed (item 7 + this item) — and the "Design discussion before code" heading above it if only
the C-13 entry (removed in item 6) and C-04 (item 2) were under it.

**Files:** `internal/platform/winlabel/walk_windows.go`, `internal/platform/winlabel/retire.go`,
`internal/platform/winlabel/journal.go`, `internal/platform/winlabel/retire_test.go`,
`internal/platform/winlabel/journal_test.go`, `internal/platform/confiner_windows_test.go`,
`ISSUES.md`

**Regression guard.** `TestWindowsForeignPriorLabelIsRestoredOnTeardown`
(`confiner_windows_test.go:470`) and the shared-root hand-off test (`:1271`) restore a foreign
prior AFTER apogee's Low label was applied and cleared — with the judgement taken before the clear
they stay green; a judgement taken after it would drop both. `retire_test.go`'s
`restorablePriors` tables are untouched (the seam is pure and unchanged in signature).

**Tests.** Cross-OS: `TestPriorRestorableTable` in `retire_test.go` (the three outcomes above, with
a real Low SDDL string from `sddl_test.go`'s fixtures); `TestJournalRoundTripsJudged` in
`journal_test.go` (the flag survives `WriteJournal` → `ReadJournal`; an entry without it decodes
`Judged == false`). Windows-tagged (item 9's CI job runs them):
`TestWindowsPlantedJournalDoesNotRelabelAForeignPath` in `confiner_windows_test.go` — write a
journal under a temp home naming a temp file with a fabricated non-Low `PriorSDDL` and a dead PID;
the file carries its default (unlabelled) SDDL; `winlabel.Recover(home)`; the file's SDDL is
byte-identical to before, and the journal is retired (the entry was dropped, not re-attempted).
`TestWindowsRecoverStillRestoresOurOwnPrior` — the same, but the file was first labelled Low by
`LabelTree` over a foreign prior: the prior IS restored (the existing revert behaviour, now proven
to survive the check). `TestWindowsHandedOffPriorIsJudgedOnce` — a live sibling claims the root;
the entry is handed off with `Judged: true`; after the sibling retires, the later pass restores it
although the path then reads unlabelled. `TestWindowsFailedRestoreIsRetriedNotDropped` — a
prior whose `SetSDDL` fails on the first pass (the file held open with a deny-WRITE_DAC share, or
an unparseable `PriorSDDL` fixed up before the retry): the journal survives with `Judged: true`,
and `Recover` on the next pass restores it although the path reads unlabelled.

**Acceptance.** `go test ./internal/platform/winlabel/ -count=1 -race` (the cross-OS tables);
`GOOS=windows go vet ./internal/platform/...`; `grep -c 'F-08' ISSUES.md` → `0`.

**Commit.** `fix(winlabel): a prior label is restored only where apogee's Low label stood before the clear (F-08)`

## 9. CI runs the Windows-tagged platform and probe tests on `windows-latest`

**What.** Depends on items 7 and 8. `.github/workflows/ci.yml` has one `check` job on
`ubuntu-latest` and a `cross` job that only compiles the six targets (`:74-97`); every
`//go:build windows` test in `internal/platform` (25 tests + the two new ones), `winlabel` and
`internal/probe` has never run in CI (`internal/probe` has no Windows-tagged test today —
`terminal_windows.go`'s console read-back stays untested by this plan; the leg is there so the
package's untagged tests also prove themselves on the Windows toolchain, and so a future tagged
test has a home). Add a job
`test-windows` (`name: windows tests`) on `windows-latest`: the same pinned `actions/checkout`
(`persist-credentials: false`) and `actions/setup-go` (`go-version: '1.26.x'`) steps, then
`go test -race -count=1 ./internal/platform/... ./internal/probe/...` (the runner's MinGW gcc
satisfies `-race`'s cgo need; if `-race` fails to link on the runner, drop `-race` for this job and
say so in a comment — the C-20 race test then relies on its own iteration count). The job comment
states why these two trees and not `./...`: the rest of the suite is Linux-shaped (landlock
battery, PTY drivers) and the Windows half of the product is the confinement backend
(`internal/platform` carries `winlabel`'s walk), whose tests label the runner's own disk —
throwaway by construction. The comment claims nothing about `internal/probe` coverage. `scripts/check-pins.sh` must still
pass (reuse the exact pinned SHAs already in the file; add no new action). `docs/manual/building.md`
gains one line under the CI paragraph (find it with `grep -n 'CI' docs/manual/building.md`; if the
manual has no CI paragraph, `docs/design/test-drivers.md`'s "CI" mention at `:328` gets the line):
"Windows-tagged tests run on a `windows-latest` job; `make check` on a Linux or macOS box compiles
them (`GOOS=windows go vet`) but cannot run them."

**Files:** `.github/workflows/ci.yml`, `docs/manual/building.md` (or `docs/design/test-drivers.md`
per the rule above), `Makefile`

Also `Makefile`: the `check` target gains `GOOS=windows go vet ./internal/platform/... ./internal/probe/...`
after its `go vet ./...` line, so a Linux `make check` catches a Windows-tagged test that no longer
compiles — the executor-side floor the plan's standing requirement names.

**Tests.** `make actionlint` (the pinned actionlint validates the new job); `./scripts/check-pins.sh`.

**Acceptance.** `make actionlint && ./scripts/check-pins.sh`; `grep -n 'windows-latest' .github/workflows/ci.yml`
→ one hit; `GOOS=windows go vet ./internal/platform/... ./internal/probe/...` passes;
`make -n check | grep -c 'GOOS=windows'` → `1`.

**Commit.** `ci: the Windows-tagged platform and probe tests run on a windows-latest job`

## 10. `wire_test.go` split, part 1: the settings-apply tests move to `wire_settings_test.go` (S-1)

**What.** `cmd/apogee/wire_test.go` is 6,090 lines and 108 tests for a composition root ADR 0043
split by seam into `wire_boot.go`, `wire_engine.go`, `wire_server.go`, `wire_session.go`,
`wire_settings.go`, `wire_tools.go`, `wire_verbs.go`, `wire_mcp.go`, `wire_live.go`,
`wire_present.go`, `wire_firing.go`. Items 10–13 move every test to the `_test.go` of the seam it
exercises — **mechanical moves only**: a test function moves with its doc comment, unchanged, and a
helper moves with its users. No test is renamed, rewritten, merged or dropped; imports are
re-derived per file (`goimports`-clean). The implementer moves by line range with `sed -n` /
`sed -i` and never reads the whole file.

This item creates `cmd/apogee/wire_settings_test.go` and moves into it every test whose subject is
`applySetting` / the settings registry / the live-apply seam — by name (line numbers at the base
commit): `TestApplySettingToolsDisabledSwapsTheSet` (1380), `TestApplySettingURLSafetyHostsSwapTheSet`
(1437), every `TestApplySetting*` from `TestApplySettingDrivesTheRightEngineSeam` (3392) through
`TestApplySettingServersRidesTheRebindForTheBoundEntrysResponseReserve` (5555) and
`TestApplySettingServersDoesNotRebindForAReserveEditThatMovesNothing` (5640),
`TestApplySettingSavesTheTopLevelResponseReserveWithoutMovingTheSession` (5719),
`TestSettingsTableIsInRegistryOrder` (3674), `TestEveryEditableSettingKeyHasAnApply` (3710),
`TestLiveSettingsOptionsFollowEveryApply` (3820), `TestMCPReconnectRebuildsWithTheEndpointTheSessionIsOn`
(4538), `TestPresentPortEditRebindsTheDocServer` (4713), `TestRunRootWiresTheLiveApplySeam` (4803),
`TestLateEngineRemembersSettingsMovedBeforeTheBind` (4857), `TestLateEngineBindRefusesAProfileItCannotParse`
(4901). Helpers used only by these move too: `fullyComposedApplier` (3732), `clobberOptions` (3988),
`writeSettingsFixture` (4232), `newMCPFixture` + the `mcpFixture` type (4453), `toolNames` (4470),
`freePort` (4776), `writeSkillFixture` (4790), and the settings doubles and tables the moved tests
declare: `applySettingSpy` and its seven methods (3343–3383), `startupOnlyContract` (3587),
`settingKeysWithNoMemberToReach` (3598), `settingKeysAppliedByTheRenderer` (3656), `rebindProbe` /
`rebindCall` / `rebind` (4047–4060), `fakeMCPSession` (4424), `mcpFixtureTool` (4434),
`mcpServersFixture` (4480). **Shared-use rule (items 10–13):** every `cmd/apogee/*_test.go` is ONE
package, so a helper's users are found with `grep -n '<helper>(' cmd/apogee/*_test.go` — never
`wire_test.go` alone; a helper with users in two or more files (after the move) goes to
`cmd/apogee/wire_helpers_test.go` (create it in this item if needed; items 11–13 add to it).
Known cross-file cases: `captureStderr` (a user at `confinement_e2e_test.go:153`) and
`assertFiringScratchDir` (`daemonfire_test.go:172`, `headless_test.go:721`, `schedule_test.go:343`)
go to the helpers file.

**Files:** `cmd/apogee/wire_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_helpers_test.go`

**Tests.** The moved tests themselves. Invariant: the number of `^func Test` across
`cmd/apogee/*_test.go` is unchanged.

**Acceptance.** `gofmt -l cmd/apogee` → empty; `go vet ./cmd/apogee/`;
`go test ./cmd/apogee/ -count=1`; `git grep -h -c '^func Test' HEAD -- 'cmd/apogee/*_test.go' | awk '{n+=$1} END{print n}'`
equals `grep -h -c '^func Test' cmd/apogee/*_test.go | awk '{n+=$1} END{print n}'` (`bc` is not
on the executor; `awk` is); `grep -h '^func Test' cmd/apogee/*_test.go | sort | uniq -d` → empty.

**Commit.** `refactor(cmd): the settings-apply tests move to wire_settings_test.go (S-1, 1/4)`

## 11. `wire_test.go` split, part 2: the server / launcher / rebind tests move to `wire_server_test.go` (S-1)

**What.** Depends on item 10 (the line numbers below shift after it; the NAMES are the anchors).
Create `cmd/apogee/wire_server_test.go` and move: `TestRebindRecomposesTheToolSetUnderTheProfileRoster`,
`TestRebindReportsARefusedRosterSwapAsANotice`, `TestRebindSpecForSelectsPerModelBindings`,
`TestRebindInputsOverlayTheBoundUpstream`, `TestRebindResolutionKeysOnTheBoundEndpoint`,
`TestRunRootBindsADeterminedStartupBeforeLaunch`, `TestRunRootStartsPreboundWithoutAnEngine`,
`TestBindServerConstructsOnceAndFlipsBothSeams`, `TestLateEngineAppliesPreBindSettingsOnBind`,
every `TestLoadProfile*`, every `TestUnloadAndStop*`, `TestLaunchProfilesSeamProjectsRows`,
`TestActuationResultKeepsTheStepsBesideTheError`, `TestRunRootWiresTheLauncherSeamsForTheWholeSession`,
`TestSwitchServerFollowsTheEntrysLauncher`, `TestBindServerInstallsTheEntrysLauncher`,
`TestMoveCarriesTheEntrysWindowAndReplyCap`, `TestMoveCarriesTheEntrysResponseReserveShare`,
`TestStartupBindHonoursTheEntrysContextWindow`, `TestServerBindHandsTheEntrysBoundsToTheEngine`,
`TestServerBindHandsTheEntrysResponseReserveToTheEngine`, `TestBindServerResolvesTheWorkingWindow`.
Helpers: `rosterSwitchWiring`, `liveSetHas`, `launcherWiringFixture`, `twoServerConfig`,
`wildcardBoundConfig`, and the doubles `fakeSwitcher` (2476–2488) and `fakeStamper` (2490–2496)
move with them (or to `wire_helpers_test.go` under item 10's shared-use rule).

**Files:** `cmd/apogee/wire_test.go`, `cmd/apogee/wire_server_test.go`, `cmd/apogee/wire_helpers_test.go`

**Tests.** The moved tests. Same count invariant as item 10.

**Acceptance.** As item 10 (`gofmt -l`, `go vet`, `go test ./cmd/apogee/ -count=1`, the two `awk`
count checks).

**Commit.** `refactor(cmd): the server, launcher and rebind tests move to wire_server_test.go (S-1, 2/4)`

## 12. `wire_test.go` split, part 3: the session tests move to `wire_session_test.go` (S-1)

**What.** Depends on item 11. Create `cmd/apogee/wire_session_test.go` and move: every
`TestSessionHost*`, every `TestResolveResume*`, `TestResolveContinuePicksWorkspaceNewest`,
`TestBuildAgentResumeRoundTrip`, `TestBuildAgentResumeFutureVersion`, `TestGCScratchDirsRemovesOldKeepsFresh`,
`TestSessionHostScratchFollowsTheActiveSession`, `TestSessionHostWithoutScratchRootIsInert`.
Helpers: `saveAt`, `assertFiringScratchDir` (shared-use rule applies).

**Files:** `cmd/apogee/wire_test.go`, `cmd/apogee/wire_session_test.go`, `cmd/apogee/wire_helpers_test.go`

**Tests.** The moved tests. Same count invariant.

**Acceptance.** As item 10.

**Commit.** `refactor(cmd): the session tests move to wire_session_test.go (S-1, 3/4)`

## 13. `wire_test.go` split, part 4: boot, engine and tools tests take the remainder; `wire_test.go` is deleted (S-1)

**What.** Depends on item 12. What remains in `wire_test.go` is the boot / engine / tools set:
`TestShouldPrewarmLabelWalk`, `TestCaptureStderrRestoresOnGoexit` (+ `captureStderr`),
every remaining `TestRunRoot*` (`ThreadsContextWindow`, `SystemPromptResolutionFails`,
`ThreadsContextFiles`, `ThreadsSpinnerOptions`, `ResolvesTheColorScheme`, `ConfinementStartupNotices`,
`InstallsPresenter`), `TestPresentationRungs`, `TestParseMode`, `TestResolveRootsOverride`,
`TestResolveRootsDefaults`, `TestRecallHostBindsWorkspace`, `TestBuildAgentNew`, `TestBootConfigCarriesTheDelegateStepCap`,
`TestEveryDriverHandsTheRosterRungsToTheConfig`, `stubPresenter` (1500–1504) →
`cmd/apogee/wire_boot_test.go`; every `TestRegistryWithMCP*` + `assertRegistryOffers`, and every
`TestMechanismIDs*` + `fakeKnown` (2367) → `cmd/apogee/wire_tools_test.go` (`mechanismIDs` lives
in `wire_tools.go:262`, so its tests sit beside it — item 14 edits that resolver there);
`TestFriendlyConstructErr` + `validCfg` → `cmd/apogee/wire_engine_test.go`. The catch-all is
binding for EVERY top-level declaration still in the file — funcs, types, vars, consts, not only
"helpers": each goes with its users, or to `wire_helpers_test.go` when they span files. Then
`wire_test.go` holds nothing but the package clause and imports: `git rm` it. Remove the
`ISSUES.md` line **S-1** (`:812-813`) — and, since item 14 removes S-2, leave the "Architecture
pass, not fixes" heading for item 15.

**Files:** `cmd/apogee/wire_test.go`, `cmd/apogee/wire_boot_test.go`, `cmd/apogee/wire_tools_test.go`,
`cmd/apogee/wire_engine_test.go`, `cmd/apogee/wire_helpers_test.go`, `ISSUES.md`

**Tests.** The moved tests. Same count invariant; additionally `test -e cmd/apogee/wire_test.go`
fails.

**Acceptance.** As item 10, plus `! test -e cmd/apogee/wire_test.go`; `grep -c 'S-1' ISSUES.md`
→ `0`.

**Commit.** `refactor(cmd): wire_test.go is split by seam; the drawer is gone (S-1, 4/4)`

## 14. The grammar Mechanism and `Deps.GrammarConstraint` are retired; a config naming it is tolerated (S-2)

**What.** Ratified: delete, not wire. `internal/mechanisms/grammar.go` is reachable only through
`Deps.GrammarConstraint` (`catalogue.go:42-54`), which `deriveDeps` (`internal/agent/construct.go:344`)
never populates, over a provider wire that carries no `response_format`; it has no-op'd on every
backend since the port. Remove:

- `internal/mechanisms/grammar.go`, `grammar_test.go`; the `GrammarConstraint` field and its doc
  in `catalogue.go`; the `grammar` row from the catalogue's registration table and every
  `"grammar"` in `catalogue_test.go` (`:171`, `:214`); the `catalogue.go:100` doc sentence that
  cites it as the "left inert" example (LookPath remains the example).
- `internal/agent/construct.go:344` comment (drop the GrammarConstraint clause).
- `internal/validated/shipped.json`: drop `"grammar"` from the gemma set (16 → 15 IDs);
  `shipped_test.go:46-52` pin drops it and the comment's "exact 16 IDs" becomes 15; the entry's
  `evidence.note` gains ` — grammar removed 2026-08-29 (retired: inert on every backend, never fired
  in the campaign)`; do NOT change `entered`.
- `docs/design/mechanism-catalogue.md`: the `grammar` rows in Tables A (`:126`), B (`:184`) and
  the wave table (`:286`) become a one-line "retired 2026-08-29 — see CHANGELOG" note or are
  deleted with a footnote; the gemma row (`:504`) says "the pruned 15 … (`grammar` retired
  2026-08-29)"; the `:101` and `:229` mentions drop `grammar` from their lists.
- ADR 0015 `:56`: "`GrammarConstraint` remains the inert false seam" → a dated bracketed note
  "[retired 2026-08-29 — the seam was never populated and was removed with the grammar Mechanism]".

**Regression guard (binding).** A user's `config.yaml` `mechanisms:` map, a per-server
`sub-agents:` posture, or a saved Validated-set record that still names `grammar` worked at
v0.18.6 and must not refuse now.
- `internal/mechanisms` gains `retired.go`: `RetiredIDs() []domain.MechanismID` (today: `grammar`)
  and `IsRetired(id)`. `internal/mechanisms/doc.go` names `retired.go` in its file map and drops
  the `grammar.go` sentence at `doc.go:36` — `TestDocMapNamesEveryFile`
  (`internal/mechanisms/docmap_test.go:11`) goes red the moment an unmapped `.go` file lands.
- `mechanismIDs` (`cmd/apogee/wire_tools.go:262`) DROPS a retired ID silently and errors on an
  unknown one as today. It prints nothing: it also runs on the live apply path
  (`wire_settings.go:1191` `reloadMechanisms`) and per delegate (`delegation.go:487`), where a
  stderr line would be painted into the alt screen. The NOTICE is a separate pure helper,
  `retiredMechanismNotices(enabled map[string]bool) []string`, returning one line per retired ID
  named — `apogee: mechanism "grammar" was retired in v0.18.7 and is ignored; remove it from
  mechanisms:` — printed to stderr ONLY by the pre-TUI startup caller (`wire_live.go:158`) and
  folded into the apply's existing notice string on the settings path. A retired ID in
  `mechanisms.disabled` is ignored silently everywhere.
- **ADR 0016 is superseded explicitly for retired IDs.** `docs/adr/0016-validated-mechanism-sets.md:118-125`
  rules that an entry naming an ID this binary does not know is skipped WHOLE ("never a partial
  application"). A saved user record naming `grammar` would therefore lose its whole set. Add a
  dated amendment there: a RETIRED ID — one that was inert by construction when retired, so the
  set's evidence is unchanged without it — is dropped from the entry with a notice
  (`resolveValidatedSet`'s `notices` slice, `cmd/apogee/validatedsets.go:155`) and the rest of the
  set applies; an UNKNOWN (non-retired) ID still skips the whole entry as ruled.
  `internal/validated`'s ID check (`validatedsets.go:133` → `validated.Validate`) consults
  `mechanisms.RetiredIDs()`. Amend §6 (`:64-66`, "the pruned 16") to 15 in the same commit.
- Tests: `TestMechanismIDsRetiredIDIsDropped` (config `grammar: true` → no error, the ID absent
  from the result, nothing on stderr), `TestRetiredMechanismNoticesNameEachRetiredID` (the exact
  line above), `TestResolveValidatedSetDropsARetiredIDWithANotice` (a user record naming
  `grammar` + 15 known IDs → the 15 apply, one notice), and the existing whole-skip test for an
  unknown ID still passes. `/settings`' mechanism rows: if the settings screen lists `KnownIDs()`,
  the retired ID is simply absent (verify by `grep -rn 'KnownIDs' internal/tui cmd/apogee`).

**Files:** `internal/mechanisms/grammar.go`, `internal/mechanisms/grammar_test.go`,
`internal/mechanisms/catalogue.go`, `internal/mechanisms/catalogue_test.go`,
`internal/mechanisms/retired.go` (new), `internal/mechanisms/retired_test.go` (new),
`internal/mechanisms/doc.go`, `internal/agent/construct.go`, `internal/validated/shipped.json`,
`internal/validated/shipped_test.go`, `internal/validated/validated.go` (the ID check),
`internal/validated/validated_test.go`, `cmd/apogee/wire_tools.go`, `cmd/apogee/wire_tools_test.go`,
`cmd/apogee/wire_live.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/validatedsets.go`,
`cmd/apogee/validatedsets_test.go`, `docs/design/mechanism-catalogue.md`, `docs/adr/0015-*.md`,
`docs/adr/0016-validated-mechanism-sets.md`, `ISSUES.md`

**Tests.** The four tolerance tests above; `TestShipped_GemmaEntryVerbatim` amended;
`TestDocMapNamesEveryFile` green;
`TestKnownIDs…` in `catalogue_test.go` no longer lists `grammar`; `go build ./...` (nothing else
references the symbol — `grep -rn 'GrammarConstraint\|"grammar"' --include=*.go .` → only
`retired.go` and its tests). Remove the `ISSUES.md` line **S-2** (`:814-815`).

**Acceptance.** `go build ./... && go test ./internal/mechanisms/ ./internal/validated/ ./internal/agent/ ./cmd/apogee/ -count=1`;
`grep -rn 'GrammarConstraint' --include=*.go . | wc -l` → `0`; `grep -c '"grammar"' internal/validated/shipped.json`
→ `0`; `grep -n 'retired' docs/adr/0016-validated-mechanism-sets.md` → at least one hit;
`grep -c 'S-2' ISSUES.md` → `0`.

**Commit.** `refactor(mechanisms): retire the grammar Mechanism and its never-populated seam; a config naming it is tolerated (S-2)`

## 15. `ISSUES.md`: the emptied audit-residue sections are removed

**What.** Depends on items 1–14. `ISSUES.md` §"Audit residue (2026-08-25 refocus / security /
code audits)" now holds only its preamble, the "Signal the audits could not produce" paragraph and
the "Worth watching" note; §"Residuals deferred out of the 2026-08-28 code-audit fixes run" holds
only its status line. Remove both sections whole (heading through the `---` rule), and re-home the
two surviving paragraphs: "Signal the audits could not produce" (no lint / govulncheck / CVE signal
on the audit host) becomes a bullet under "Improvements / Ideas" titled **Run `golangci-lint` and
`govulncheck` in `make check` and CI** (it is a real, open improvement); "Worth watching" (the
stock gemma set arms `filehint`, C-08's reachability) is dropped — C-08 was fixed in plan
`2026-08-26 - 02` item 5 and the note has nothing left to watch. Check that no other section
referenced the removed anchors (`grep -n 'Audit residue\|code-audit fixes run' ISSUES.md docs/`).

**Files:** `ISSUES.md`

**Tests.** None — a register edit.

**Acceptance.** `grep -c 'Audit residue' ISSUES.md` → `0`; `grep -c 'code-audit fixes run' ISSUES.md`
→ `0`; `grep -c 'golangci-lint' ISSUES.md` → `2` (the bullet title and its preserved body line); `grep -c 'C-13\|C-04\|C-15\|C-20\|F-08\|S-1\|S-2' ISSUES.md`
→ `0`.

**Commit.** `docs(issues): the audit-residue sections are closed; the lint/vuln-scan gap stays as an idea`

---

## Suggested version bump

Patch (`v0.18.6` → `v0.18.7`): every item is a fix, a test or an internal refactor; the one
user-visible change is the retired `grammar` Mechanism ID (tolerated with a notice, so no config
breaks). Item 14's notice text names `v0.18.7` — if the owner cuts a different version, the
implementer of item 14 uses that string (a dated NOTES line records it). Not performed by this
plan; the owner decides.
