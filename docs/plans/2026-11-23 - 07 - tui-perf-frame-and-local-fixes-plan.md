# TUI performance — frame floor and local fixes — plan

**Goal:** restore the 2026-11-23 audit benchmarks as kept code, then land the six local, output-preserving fixes the audit filed under `tui-perf-responsiveness`: no Model heap copy per frame, no footer transcript walk per frame, a memoised /thinking pane, the `@file` walk off the Update goroutine, a memoised side panel and copy-free sub-agent paint keys. Drawn output stays byte-identical everywhere.

**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 47d9e204

**Sources:**
- `docs/reviews/tui-perf-audit-2026-11-23.md` (Profile, Suggested plan items, Recommended Action Order)
- `.capcom/issues/tui-perf-responsiveness/` — open-panes-heap-copies, footer-spend-walks-transcript, thinking-pane-rebuilds-rows, at-file-walk-blocks, panel-sections-every-frame, delegation-key-copies-span
- ADR 0011 and `internal/tui/doc.go` (value-copied Model, pointer-held memos); `capcom know show adr-0062-amend-1118` (tuitest + stubllm rule); `docs/design/test-drivers.md` §Waiting
- Follow-on plan: `2026-11-23 - 08 - tui-perf-repaint-plan.md` (repaint path; depends on this plan)

**Ratified design calls:**
- **Split:** the audit's issues go into two plans, this one (local fixes) first (owner, 2026-11-23).
- **@file listing:** stale-then-refresh — a key filters what the cache holds now (nothing on first use: no menu), and the open menu is refreshed when the background walk lands; no new visual state (owner, 2026-11-23).
- **Footer spend:** a by-value memo of the transcript half refreshed on Update's tail, keyed on `transcript.generation`; workflow spend and the resumed fallback are added at read time (writer, 2026-11-23).
- **Report panes:** `reportState` becomes a direct switch so the Model does not escape; `paneSpec` keeps its by-value signatures (writer, 2026-11-23).
- **Side panel:** memo per section (changed-files, section-list parse), not the whole frame; sub-agent, usage and MCP sections repaint every frame (clock/gauge-driven) (writer, 2026-11-23).
- **Thinking wrap memo:** per line, handling the front cut at `thinkingRecordCap`; a new line-level counter beside the record-level `wraps` (writer, 2026-11-23).

**Regression check (2026-11-23, 47d9e204):**
- 2: guard folded (reportpane_test.go compile, panic default, non-parallel alloc test; supersedes the reportRows state-column comments)
- 3: guard folded (writer decision: generation-trusted memo, touch() at direct test writes); yields to usage.go:274-275
- 4: guard folded (composed memo keyed on computed headings; supersedes the thinkingpane.go "HEADING never" note)
- 5: guard folded (writer decision: nil-safe in-flight state, walk warmed through a Cmd-running helper)
- 6: recast (writer decision: body-only-when-expanded clause dropped, memo cleared on theme switch, render-path parses only, memo on Model)
- 7: guard folded (blockKey record path and the resolvedBlock buffer hand-back kept)
- 3 (re-check): guard folded (writer decision: touch() after the usage_test.go direct writes as workflowblock_test.go does; zero-value memo invalid)
- 5 (re-check): guard folded (writer decision: fileRegion flag gates the filesListedMsg re-derive; root-mismatched listing filters empty; install only for the workspace root)
- 6 (re-check): guard folded (tuitest + stubllm e2e for rows following writes; issue bullet reworded to the changed-files-only repaint)

**Standing requirements:**
- skills: coding-standards
- Every fix keeps the drawn frame byte-identical; the cold reference is a nil paint cache or a fresh build.
- Tests that need a model script `internal/stubllm` (never an `httptest` upstream); any wait goes through `internal/testtime` with a generous backstop; no fixed clock in shipped code (slow-box rule).
- Run one test process at a time, narrowed to one package and `-run` pattern; benchmarks without `-race`, one at a time. Record before/after benchmark numbers in each item's commit body.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).

**Out of scope:**
- Everything in plan 08: keystroke/stream/drag repaints, the viewport width pass, View composition, the reserve recut, resize leaf costs.
- Audit findings not filed as issues (blink repaint, preview markdown memo, debounce ticks, long-draft geometry, tool-group shape, cursor-move repaint).
- Any visual or interaction change.

## 1. Restore the audit benchmarks as kept code — ✅ DONE (2026-11-23)

NOTES (2026-11-23): copied verbatim from the audit's .txt copy; only the TEMPORARY header was replaced by a doc comment (report path, one-benchmark-at-a-time run line). Nothing failed to compile at HEAD. Baseline 1x numbers (Pi 5): Blocks view N=48/300/900 ≈ 2.0/2.4/2.5 ms, 708 KB, 5263 allocs; repaint 5.0/14.6/40.7 ms; Type N=0/48/300 3.6/6.2/16.5 ms; Stream ≈ 4.0 / 16.4 ms per delta (session=0/300).

**What:** test-only. Copy `docs/skill-runs/code-audit/2026-11-23/profile/zz_perfaudit_bench_test.go.txt` to `internal/tui/perfaudit_bench_test.go`, drop its "TEMPORARY" header comment, and add a doc comment naming the audit report and how to run one benchmark at a time. Keep the helper names (`paSession`, `paModel`, `paStep`, `paViewString`, …) and the benchmark names (`BenchmarkPerfAuditStream`, `…Blocks`, `…Fold`, `…Scroll`, `…Type`, `…Idle`) unchanged — later items and plan 08 cite them. If the source file is absent, rebuild the same benchmarks from the report's Profile section (resumed session of N tool blocks through `--resume`'s `ResumedSession`, 120×40). Fix only what fails to compile at HEAD.
**Files:** internal/tui/perfaudit_bench_test.go
**Read first:** docs/skill-runs/code-audit/2026-11-23/profile/zz_perfaudit_bench_test.go.txt — paSession, paModel, paStep, BenchmarkPerfAuditBlocks; internal/tui/model_test.go — newTestModel, testOpts; internal/tui/tui.go — ResumedSession; internal/testtime/guard_test.go — TestNoFixedTimingInTests
**Tests:** the file compiles and each benchmark runs one iteration.
**Acceptance:**
- `go vet ./internal/tui/`
- `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchtime 1x ./internal/tui/`
- `go test -run '^$' -bench 'PerfAuditType' -benchtime 1x ./internal/tui/`
**Commit:** `test(tui): keep the 2026-11-23 perf audit benchmarks`

## 2. No Model heap copy when asking which panes are open — ✅ DONE (2026-11-23)

NOTES (2026-11-23): benchmark `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem ./internal/tui/` (Pi 5, no -race) before → after: N=48 2.36 ms 751244 B/op 5273 allocs → 1.84 ms 170713 B/op 5233 allocs; N=300 2.22 ms 728153 B/op 5267 allocs → 1.93 ms 166452 B/op 5232 allocs; N=900 2.52 ms 715735 B/op 5262 allocs → 2.30 ms 163547 B/op 5229 allocs (B/op −77%). `-gcflags=-m` "moved to heap: m" 14 → 2; the two left (toolargs.go:50, mouse.go:1292) predate this item and are outside report/pane predicates.

NOTES (2026-11-23): TestOpenPanesAllocatesNothing measures "no Model heap copy per View" with the runtime/metrics allocs-by-size histogram, counting only the Model's own size class (24576–27264 B; Model is 24800 B). Counting every class ≥ the Model's size failed under -race, where the race runtime's sync.Pool drops add ~4 large (>32 KB) allocations per View. Old code: 5 allocs per openPanes, ~20 Model-class allocations per View; new code: 0 and 0. The test fails loudly if the Model ever grows into the large-object bucket, where the histogram cannot isolate it.

NOTES (2026-11-23): folded mouse.go popupPaneHit's generic `pre.openPanes().has(pane)` into `paneSpecs[pane].open(pre)` (same boolean, one pane's predicate), beyond the two has(paneDropdown) sites the item named; the shared dropdown helper is `Model.dropdownOpen` (panes.go), also used as the dropdown row's open predicate.

NOTES (2026-11-23): consequential edit — internal/tui/doc.go: made necessary by moving report state lookup out of reportRows into the reportState switch (the file map said "each kind one row of [reportRows]").

**What:**
**Goal:** a `View` of an unchanged model with no pane open performs no heap copy of the Model; pane predicates return the same booleans as before.
**Approach (assumed at the header base):** in `internal/tui/reportpane.go`, replace the function-valued `reportRow.state` field used by `(*Model).reportState` with a direct `switch r` returning `&m.usagePane`, `&m.inspector`, `&m.thinkingPane`, `&m.advicePane`, `&m.mcpPane`; add `func (m Model) reportOpen(r) bool` and use it as the report rows' `open` in `reportPaneRow` (`panes.go`). Keep `paneSpec`'s `func(Model)` signatures (a `*Model` through an indirect call re-creates the escape). Callers that ask about one pane use that pane's own predicate: `draftRowsCeiling` and the other `openPanes().has(panePrompt)` sites (`model.go`, `approval.go`) use `m.promptOpen()`; the `has(paneDropdown)` sites (`autocomplete.go`, `mouse.go`) use the dropdown row's predicate via one shared helper. `inputScrollbarShown` returns false before computing the ceiling when the draft's content rows are ≤ `minInputRows`. Confirm with `go build -gcflags=-m ./internal/tui/ 2>&1 | grep -E 'reportState|reportOpen|moved to heap: m'`.
**Regression guard.** Removing `reportRow.state` breaks the test package's compile at `TestReportKindsResolveDistinctly` (`reportpane_test.go:61`, `row.state == nil`): drop that check there, and give the `reportState` switch a `default` that panics on an undeclared kind, so a fifth report with no case still fails loudly as the "unfilled row" check did. `TestOpenPanesAllocatesNothing` does not call `t.Parallel()` (`testing.AllocsPerRun` refuses a parallel test), like `TestLineEditorKeypressAllocsDoNotRewrapTheDraft`. Supersedes the state-column design recorded at `reportpane.go:70-76`, `:97-99`, `:178-184` and `reportpane_test.go:43-49`: reword every comment that names `reportRows`' state column or "switches over" it (`grep -n 'reportRows\|switches over' internal/tui/*.go`).
**Files:** internal/tui/reportpane.go, internal/tui/panes.go, internal/tui/model.go, internal/tui/approval.go, internal/tui/autocomplete.go, internal/tui/mouse.go, internal/tui/panes_test.go, internal/tui/reportpane_test.go
**Read first:** internal/tui/reportpane.go — reportRows, (*Model).reportState; internal/tui/panes.go — reportPaneRow, promptOpen; internal/tui/model.go — openPanes, inputScrollbarShown; internal/tui/reportpane_test.go — TestReportKindsResolveDistinctly; internal/tui/lineeditor_test.go — keypressAllocs
**Closes:** open-panes-heap-copies
**Tests:** `TestOpenPanesAllocatesNothing` (`testing.AllocsPerRun` on `openPanes` and on `View` deltas of an unchanged model with no pane open — `openPanes` 0 allocs; not parallel); `TestReportKindsResolveDistinctly` green without its `row.state` check; existing pane open/close tests and `TestModelFitsTheStack` stay green.
**Acceptance:**
- `go test -race -count=1 -run 'TestOpenPanes|TestModelFitsTheStack|TestReport|TestPane' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EPopup' ./cmd/apogee/`
- `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem ./internal/tui/` — B/op roughly halves (record before/after)
**Commit:** `perf(tui): ask about open panes without heap-copying the Model`

## 3. Footer spend without a transcript walk per frame — ✅ DONE (2026-11-23)

NOTES (2026-11-23): benchmark `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem -count=3 ./internal/tui/` (Pi 5, no -race) before → after: N=48 1.84 ms 172 KB/op 5233 allocs → 1.83 ms 172 KB/op 5233 allocs; N=300 1.92 ms 168 KB/op 5232 allocs → 1.81 ms 166 KB/op 5232 allocs; N=900 2.31 ms 164 KB/op 5229 allocs → 1.77 ms 164 KB/op 5229 allocs — N=900 is no longer above N=48.

NOTES (2026-11-23): the memo field (`delegateSpend delegateSpendMemo`) sits on `sessionView` (internal/tui/sessionview.go, embedded in Model) rather than directly on Model: `swapSessionView` (watch.go) swaps the whole view, transcript included, so a Model-level memo could survive a swap and match the incoming transcript's generation by coincidence; beside the transcript it travels with the view it sums.

NOTES (2026-11-23): the shared predicate is `delegateSpendOf(*entry)` (usage.go), asked by both `delegateUsageHeads` (now an index walk) and the memo's `sumTranscriptDelegateSpend`; `-gcflags=-m` shows it and the entry methods it calls inlined, so the walk copies no entry. The memo is refreshed in `settle` after its `!m.ready` guard (an unsized model reads the walk); `settle`'s doc comment gained a sentence saying so.

NOTES (2026-11-23): besides the two named usage_test.go sites, the `delegate` test helper (usage_test.go) now calls `m.transcript.touch()` after its direct `head.usage` write, per the item's rule for test sites writing transcript fields directly. `TestViewDoesNotWalkForFooterSpend` proves the no-walk claim behaviourally (a head's usage rewritten without touch leaves the footer unchanged; after touch + an Update it moves) instead of with a call counter in shipped code.

**What:**
**Goal:** a `View` of an unchanged model walks no transcript entries for the footer's spend segment, and the footer's bytes are unchanged; the segment still moves when a sub-agent's usage lands.
**Approach (assumed at the header base):** add a by-value field on `Model` (e.g. `delegateSpend struct{ gen uint64; sum domain.Usage }`) refreshed in `settle` when `gen != m.transcript.generation`, by a walk over `&m.transcript.entries[i]` (no entry copy, no slices) summing the run heads and the transcript workflow entries `delegateUsageHeads` selects today. `delegateUsageTotal` (`usage.go`) reads the memo when its `gen` matches, else falls back to the walk; it then adds `m.workflows.spendEntries()` and applies the `Calls <= 0` → `m.delegateUsage` fallback to the combined total exactly as today. `delegateUsageHeads` stays for any non-sum caller, rewritten as an index walk.
**Regression guard.** The memo is trusted only when its gen equals transcript.generation, else the walk runs. Every write of an entry's usage must go through a path that calls touch(); test sites that write transcript fields directly (usage_test.go, the sites the report names) call touch() after the write — add usage_test.go edits to this item's scope. The new Model field is safe as a zero value. Yields to `usage.go:274-275` ("It is the one walk both readers of a delegate's spend take"): the memo walk and `delegateUsageHeads` select entries through one shared predicate, so the footer and the /usage pane cannot count different runs. Add `m.transcript.touch()` after the direct `m.transcript.entries = entries` assignments in usage_test.go (TestUsageRestoredDelegateKeepsItsCachedShare, …KeepsItsCost), as workflowblock_test.go does; make the zero-value memo invalid (a valid flag, or store gen+1) so a model that never settled never serves an empty sum.
**Files:** internal/tui/usage.go, internal/tui/model.go, internal/tui/usage_test.go
**Read first:** internal/tui/usage.go — delegateUsageHeads, delegateUsageTotal; internal/tui/model.go — settle, footerLeftText; internal/tui/transcript.go — touch, applyUsage; internal/tui/workflow.go — spendEntries; internal/tui/usage_test.go — TestUsageRestoredDelegateKeepsItsCost
**Closes:** footer-spend-walks-transcript
**Tests:** `TestFooterSpendMemoMatchesTheWalk` (memo vs walk over applyUsage, workflow spend, resumed fallback, /clear, and a never-settled zero-value model that must walk, not serve an empty sum); `TestViewDoesNotWalkForFooterSpend` (a call counter or AllocsPerRun on `footerContent` of a settled model); `TestUsageRestoredDelegateKeepsItsCachedShare` and `TestUsageRestoredDelegateKeepsItsCost` call `m.transcript.touch()` after their direct `m.transcript.entries` writes (`usage_test.go:579`, `:627`) and stay green; `TestE2EUsageReportsCachedTokensAndDelegateSpend` stays green.
**Acceptance:**
- `go test -race -count=1 -run 'TestFooterSpend|TestViewDoesNotWalkForFooterSpend|TestDelegateUsage|TestFooter|TestUsage' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EUsage' ./cmd/apogee/`
- `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem ./internal/tui/` — N=900 no longer above N=48 by the walk's share
**Commit:** `perf(tui): memoise the footer's sub-agent spend by transcript generation`

## 4. /thinking pane: per-line wrap memo and one composed build per frame — ✅ DONE (2026-11-23)

NOTES (2026-11-23): benchmarks (arm64 Pi 5, -count=3, no -race) — BenchmarkThinkingPaneUpdateAndView before 15.15–15.79 ms/op, 8.41–8.43 MB/op, 130,750 allocs/op; after 10.38–10.46 ms/op, 6.39 MB/op, 46,892 allocs/op. BenchmarkThinkingPaneRender before 10.97–11.13 ms/op, 6.05 MB/op, 88,220 allocs/op; after 8.05–8.23 ms/op, 5.68 MB/op, 43,925 allocs/op. Put these in the commit body.

NOTES (2026-11-23): the composed-list memo is validated on the computed headings plus every record's text (the column and scope reset it), never on a generation; the per-line reuse is decided by line equality, with `lineShift` only choosing the alignment (verified by HasPrefix on the bytes), so a wrong alignment costs wraps, never rows. Composed rows now share one backing cell array (each `popupRow` a capped one-cell slice of it) instead of one allocation per row — same rows, fewer allocations.

NOTES (2026-11-23): the remaining per-chunk cost (~89% of allocations) is the painter's `popupRowBlocks` over the whole row list in `renderPopupPlaced` — View composition, plan 08 territory, not touched here.

NOTES (2026-11-23): thinking_test.go (listed in Files) needed no change — TestThinkingBoardMutatorsInvalidateOnlyWhatTheyTouch passes unchanged with `wraps` keeping its record-level meaning.

NOTES (2026-11-23): the working tree also shows modified files this item never touched (.capcom/issues/*, docs/plans/archived/*, internal/agent|floor testdata, internal/prompt/prompt_test.go); they appeared mid-run from elsewhere and were left exactly as found — not part of this item.

**What:**
**Goal:** one reasoning chunk re-wraps only the lines it changed (including a front cut at the record cap), and the pane's composed rows are built once per frame state; rows equal a cold build.
**Approach (assumed at the header base):** in `thinkingpane.go`, `thinkingRowCache` keeps per record the wrapped rows of each `\n`-line (`wrapThinkingText` wraps lines independently). On a grown text, keep the rows of unchanged complete lines and re-wrap only the trailing partial line; when `keepLastBytes` cut the front, re-wrap only the first (cut) line and the trailing one. Add a line-level counter (`lineWraps`) beside the record-level `wraps`; existing tests pinning `wraps` keep their meaning. Memoise the composed `rows`/`kinds` in the cache, keyed on every record's text, the column, the scope and the run-label inputs, so `reportHeight` and `renderReport` share one build; a run head landing must still change the label (`TestThinkingHeadingFollowsTheRunHead`).
**Regression guard.** Validate the composed-rows memo on the computed heading strings (`runLabel` per record) plus every record's text, the column and the scope — never on `transcript.generation` alone: the memo is pointer-held and shared by every Model copy (`thinking.go:93-101`), so a copy whose transcript changed without `touch()`, or that forked at the same generation, would be served a stale heading. Supersedes `thinkingpane.go:14-18`, `:149-153` ("The HEADING never does: it is composed fresh on every call") and the `thinking.go:93-97` memo note: reword every comment that says the memo holds text rows only (`grep -n 'HEADING never\|text rows only\|wrapped text' internal/tui/thinking*.go`).
**Files:** internal/tui/thinkingpane.go, internal/tui/thinking.go, internal/tui/thinkingpane_test.go, internal/tui/thinking_test.go
**Read first:** internal/tui/thinkingpane.go — thinkingRows, wrapThinkingText, thinkingRowCache.wrapRecords; internal/tui/thinking.go — keepLastBytes; internal/tui/runview.go — runLabel; internal/tui/reportpane.go — renderReport, reportHeight; internal/tui/thinkingpane_test.go — TestThinkingHeadingFollowsTheRunHead
**Closes:** thinking-pane-rebuilds-rows
**Tests:** `TestThinkingRowsRewrapOnlyTheChangedLine` (append + at-cap front cut, `lineWraps` deltas, rows == cold `wrapThinkingText`); `TestThinkingRowsComposeOncePerFrame`; existing `TestThinkingRows*`, `TestThinkingBoardMutatorsInvalidateOnlyWhatTheyTouch`, `TestThinkingHeadingFollowsTheRunHead` green.
**Acceptance:**
- `go test -race -count=1 -run 'TestThinking' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EThinking' ./cmd/apogee/`
- `go test -run '^$' -bench 'ThinkingPaneUpdateAndView' -benchmem ./internal/tui/` — below 15.87 ms / 9.86 MB per chunk (record before/after)
**Commit:** `perf(tui): wrap /thinking rows per line and compose them once per frame`

## 5. The @file listing walks the disk off the Update goroutine — ✅ DONE (2026-11-23)

NOTES (2026-11-23): re-derived from "fileRegion beside skillRegion on the Model" — skillRegion lives on promptEditor (internal/tui/prompteditor.go), so fileRegion sits there and promptEditor.reset clears it with skillRegion.

NOTES (2026-11-23): deviation — suggest does not report whether a walk is due; it only filters the held listing (root-mismatched → empty). The due check and the in-flight mark live in fileCache.refreshCmd(root, now), which recomputeAutocomplete calls while the caret is in a file token; install(msg, workspace, now) clears the in-flight mark and installs only a listing of the workspace root. The in-flight mark is the root being walked, not a bool.

NOTES (2026-11-23): consequential edit — internal/tui/watch.go: made necessary by the new filesListedMsg fold case (TestWatchIsolatesRoutesEveryFoldedMsg requires every folded Msg routed; it routes as the view's, since the input box and file cache are the Model's, not the parked session's).

NOTES (2026-11-23): consequential edit — internal/tui/doc.go: made necessary by filecache.go's walk moving to a Cmd goroutine (the file map's filecache.go line now says so).

NOTES (2026-11-23): no kept benchmark covers the @ path (perfaudit_bench_test.go has none), so there are no before/after numbers for this item; the win is that the first @ keystroke's Update no longer includes an os.Root walk of up to 4096 files.

**What:**
**Goal:** `walkWorkspaceFiles` never runs on the Update goroutine; typing in an `@` token shows the same file rows once the listing lands; a burst of keys while a walk is in flight starts one walk.
**Approach (assumed at the header base):** follow the `skillRescanCmd` / `foldSkillsReloaded` / `reselectRow` pattern in `autocomplete.go`. `fileCache` (`filecache.go`, shared by pointer via `promptEditor.files`) gains an in-flight flag and a `walk func(root string, limit int) []string` field (nil = `walkWorkspaceFiles`) as the test seam. `suggest` never walks: it filters the held listing (stale or empty) and reports whether a walk is due. `recomputeAutocomplete` returns a Cmd running the walk off-loop that yields a `filesListedMsg{root, files}`; the Model's Update arm installs it (only Update writes the cache — `holds()` reads it on the render path) and, when the `@` menu is still live, re-derives it with `reselectRow`. `foldSkillsReloaded`'s direct `computeAutocomplete` call must not walk. Per the ratified call: stale-then-refresh; first use shows no menu until the walk lands.
**Regression guard.** The fileCache in-flight state and walk seam are nil-safe (tests build bare Model literals). step() drops Cmds, so tests warm or complete a walk through a helper that runs the returned Cmd and feeds filesListedMsg back, never by an @ keystroke under step() alone. Add a fileRegion flag beside skillRegion, set in recomputeAutocomplete while the caret is in a file token and cleared by dismissAutocomplete; the filesListedMsg arm re-derives the menu only while fileRegion && state.live() (foldSkillsReloaded's guard) — so the first walk opens the menu and an esc-dismissed menu stays closed. A held listing whose c.root != root filters as empty (holds()'s rule; TestFileCacheInvalidatesOnRootChange keeps its meaning), and the filesListedMsg arm installs only when msg.root == m.opts.Workspace.
**Files:** internal/tui/filecache.go, internal/tui/autocomplete.go, internal/tui/model.go, internal/tui/filecache_test.go, internal/tui/minilang_test.go, internal/tui/inputaccent_test.go, internal/tui/autocomplete_test.go, internal/tui/interject_test.go, internal/tui/mouse_test.go, cmd/apogee/e2e_atfile_test.go
**Read first:** internal/tui/filecache.go — fileCache, suggest, holds; internal/tui/autocomplete.go — recomputeAutocomplete, foldSkillsReloaded, skillRescanCmd, dismissAutocomplete; internal/tui/mouse_test.go — fileDropdownOpts
**Closes:** at-file-walk-blocks
**Tests:** `TestFileWalkNeverRunsInUpdate` (counting `walk` seam: `@` key → 0 walks, a Cmd; running it → 1 walk, `filesListedMsg`); `TestFileWalkBurstStartsOneWalk`; `TestFileWalkLandingOpensOnlyALiveFileMenu` (first walk landing opens the menu; an esc-dismissed menu stays closed when the listing lands; a `filesListedMsg` for another root installs nothing); `TestFileCacheInvalidatesOnRootChange` keeps its meaning (a root-mismatched held listing filters as empty); `TestE2EAtFileMenuListsWorkspaceFiles` (tuitest + stubllm, types `@`, waits via `internal/testtime` for the rows); existing tests that reach the `@` region over a temp-dir workspace with an unseeded cache (`filecache_test.go`, `minilang_test.go`, `inputaccent_test.go`, `autocomplete_test.go`, `interject_test.go`, `mouse_test.go`'s `fileDropdownOpts`) warm it through the walk-running helper and stay green.
**Acceptance:**
- `go test -race -count=1 -run 'TestFileWalk|TestFileCache|TestAutocomplete' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EAtFile' ./cmd/apogee/`
**Commit:** `perf(tui): list @file candidates off the Update goroutine`

## 6. Side panel: memoised changed-files section and section parse

**What:** Recast at the regression check (2026-11-23).
**Goal:** a `View` whose panel inputs did not change performs no `ChangedFiles` walk and no changed-files repaint; panel rows still update when a write lands; output byte-identical.
**Approach (assumed at the header base):** first add `BenchmarkSidePanelView` (~300 edit calls over ~150 paths, panel shown at 160 columns, panel on vs off) and record the before number. Then add a pointer-held memo (allocated in `newModel`, per ADR 0011) for the changed-files section keyed on the `*sidepanel.State` pointer (held in the memo so its address stays live), scope, a counter bumped whenever `m.panelDisk.gone` is replaced, content width, theme identity (glyphs, measure, padPrompts), spinner style and the ws root; memo the `ParseSidePanelSections` / `ParseSidePanelFolded` results by their source strings. Sub-agent, usage and MCP sections stay painted every frame.
**Regression guard.** Drop the "paintPanelRows paints a section's body only when it is expanded" clause (the changed-files memo removes the cost; expansion is decided from len(body) after the paint). Do not key the memo on scheme: clear the panel memo wherever a theme switch clears the paint cache (applyColorScheme in internal/tui/settingsapply.go, tellGlyphs in internal/tui/uiprefapply.go) and add both files to Files. Memo only the render-path parses (panelSectionsIn, panelFoldedSections); withSectionMode and panelReportNote keep a fresh parse (they write entries in place). Hold the memo pointer on Model, not sessionView, and make every memo method nil-safe (clearWatchView builds a sessionView literal; tests build bare Model literals). The ChangedFiles call-counter test does not call t.Parallel, like TestPanelSectionsPaintOncePerView. The issue's Acceptance needs rows shown following a write through `internal/tuitest` + `internal/stubllm`: add a cmd/apogee e2e (stubllm scripts a write_file call, 160 cols, `sidePanelAutoConfig`; WaitFor the path under "Changed files", a second write adds a row). Tasks, sub-agents, usage and MCP still repaint every frame (`sidepanel.go:662-688`): reword the issue's "no section repaint" bullet with `capcom update panel-sections-every-frame` to "no ChangedFiles walk and no changed-files repaint", naming tasks with sub-agents/usage/MCP as repainted each frame (ratified header call).
**Files:** internal/tui/sidepanel.go, internal/tui/panelframe.go, internal/tui/model.go, internal/tui/settingsapply.go, internal/tui/uiprefapply.go, internal/tui/sidepanel_test.go, cmd/apogee/e2e_sidepanel_files_test.go, .capcom/issues/tui-perf-responsiveness/panel-sections-every-frame.md (via `capcom update`)
**Read first:** internal/tui/sidepanel.go — panelSectionIn, changedFileRows, panelSectionsIn, panelFoldedSections; internal/tui/settingsapply.go — applyColorScheme; internal/tui/uiprefapply.go — tellGlyphs;
internal/tui/panelframe_test.go — TestPanelSectionsPaintOncePerView; cmd/apogee/e2e_sidepanel_drag_test.go — sidePanelAutoConfig
**Closes:** panel-sections-every-frame
**Tests:** `BenchmarkSidePanelView`; `TestPanelChangedFilesMemoHitsOnAnUnchangedFrame` (counter: no `ChangedFiles` call across two Views; not parallel); `TestPanelChangedFilesFollowWrites` (memo vs cold paint after a write, a disk-gone change, a width change, a theme switch through `applyColorScheme` and `tellGlyphs`); `TestE2ESidePanelChangedFilesFollowWrites` (tuitest + stubllm, `cmd/apogee/e2e_sidepanel_files_test.go`: two scripted write_file calls, each path appears under "Changed files"); `TestE2ESidePanelDragResizesAndPersists` and `TestE2ESidePanelDragResizesWhileWatching` green.
**Acceptance:**
- `go test -race -count=1 -run 'TestPanel|TestSidePanel' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2ESidePanel' ./cmd/apogee/` (includes `TestE2ESidePanelChangedFilesFollowWrites`)
- `capcom show panel-sections-every-frame --section acceptance` shows the reworded changed-files-only bullet
- `go test -run '^$' -bench 'SidePanelView' -benchmem ./internal/tui/` (record before/after)
**Commit:** `perf(tui): memoise the side panel's changed-files section`

## 7. Sub-agent paint keys without copying hidden entries

**What:**
**Goal:** building a block's paint key does not copy entries a collapsed Sub-agent run or group covers; keys, hits and misses are unchanged and frames byte-identical.
**Approach (assumed at the header base):** first add `BenchmarkRenderViewDelegationsAllHit` (a Sub-agents group of 4 delegates × 300 nested calls, built with `delegationCall` / `delegatedRead`; all-hit repaint) and record the before number. In `render.go`, compute the key's per-entry terms (`ins[0]` kind/depth re-based to the root, span length, `spanFlags`, `spanFills`, `anyOpenCall`, `subAgentReported`) by a pointer walk over `t.entries[head:end]`; `entry.painted` takes `*entry`; `appendInputs` runs only inside the `draw` closure (a cache miss). The shared `records` buffer stays (draw runs synchronously in `paintBlock`).
**Regression guard.** Keep `blockKey(shape, ins, …)` as the record-path reference (`TestDelegationKeysMatchTheRecordPath` compares against it; `colorprofile_test.go:179` calls it) and keep a buffer hand-back on `resolvedBlock` (`records = block.ins` in `renderView` and `paintStage`, `render.go:482`/`:833`, and `blockshape_test.go:297-298`'s `paintWalk`); change both walks, `renderView` and `paintStage`. List `blockshape_test.go` and `colorprofile_test.go` in the commit if either changes.
**Files:** internal/tui/render.go, internal/tui/paintcache.go, internal/tui/blockstate.go, internal/tui/render_test.go; internal/tui/blockshape_test.go and internal/tui/colorprofile_test.go only if either changes
**Read first:** internal/tui/render.go — paintStage, resolveBlock, paintRoot.appendInputs; internal/tui/paintcache.go — blockKey, entry.painted; internal/tui/blockshape_test.go — paintWalk; internal/tui/render_test.go — delegationCall; internal/tui/paintcache_test.go — TestPaintCacheMatchesAColdRenderThroughEveryMutation
**Closes:** delegation-key-copies-span
**Tests:** `BenchmarkRenderViewDelegationsAllHit` (allocs/op flat in elided entries); `TestDelegationKeysMatchTheRecordPath` (old vs new key over collapsed run, group, live run); `TestPaintCacheMatchesAColdRenderThroughEveryMutation` green.
**Acceptance:**
- `go test -race -count=1 -run 'TestDelegationKeys|TestPaintCache|TestRenderView' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2ESubAgentView' ./cmd/apogee/`
- `go test -run '^$' -bench 'RenderViewDelegationsAllHit' -benchmem ./internal/tui/` (record before/after)
**Commit:** `perf(tui): build sub-agent paint keys without copying hidden entries`
