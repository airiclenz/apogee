# Review findings follow-through plan

**Goal:** Close the drift and open cards found by the 2026-09-23 refocus follow-through: give `git_show` a tool card, fix stale plan paths and ADR/CONTEXT wording, fix three small engine/host defects, and land the still-open architecture-review cards #8, #9, #10, #14, #16 and #17.
**Date:** 2026-09-24
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 78430eb7

**Regression check (2026-09-24, 78430eb7):**
- 1: guard folded (a comment rule covers every summary-count and coverage comment; git_show is the eleventh summary-bearing tool)
- 3: guard folded (all three ADR 0004 sites in apogee.go; the ADR 0003 and 0002 index lines)
- 4: guard folded (ADR 0039 Decision 1(b), not D2(b); the latched-target width wording; a one-line absence grep)
- 5: guard folded (domain doc.go file map and DocMap test; every StripCwdLine/cwdLinePrefix comment names domain)
- 6: guard folded (construct.go; every call-class statement in internal/agent; SetDelegationSeat's second write)
- 7: guard folded (cmd/apogee/doc.go's wire_session.go entry)
- 8: guard folded (every named file must match)
- 9: guard folded (ok=false refuses, a nil-Confinement permit spawns unfenced; existing tests install the permit; SyncArgv|Gate in -run)
- 11: guard folded (a tools-local readScope constructor and its six callers; filtered absence grep)
- 12: guard folded (decision: HostToolsOf's cfg reads, the Sources line; Config-field-only grep, skills comments, CONTEXT.md:1120, the construction-site carve-out)
- 13: guard folded (observed through the orientation's width clause; both ADR notes, grepped)
- 14: guard folded (hold after construction, refuse only on HeldError); supersedes cmd/apogee/undo.go:44 "that gap is recorded, not closed here"
- 15: recast (decision: file closures are unvalidated typed copies; KindBool rows incl. present.command-on-model-documents; bypass env/flag stay hand-written until 16); yields to ADR 0043 amendment 2026-09-20; re-check: guard folded (init panic only on a field row with a hand-written fromFile; Goal names KindBool rows except context-files.enable; present.* bool Set keeps landIn; doc.go file map + DocMap in -run); yields to ADR 0043 amendment 2026-09-16
- 16: guard folded (decision: the file pass stays an unvalidated typed copy except the five Set-landing rows); yields to ADR 0043 amendment 2026-09-16 and ADR 0040 call 8
- 17: guard folded (config_test.go's two keyAccessors loops; amendment rule, incl. ADR 0036)
- 18: guard folded (Copy copies only its own sub-field; setFloorGuard through row.Copy)
- 19: guard folded (rename by rule; the method set is checked)
- 20: guard folded (the multi-line twins in the absence grep; highlightSettingsField presence)
- 21: guard folded (settingsKey stashes the span; settings-screen-layout.md)
- 22: recast (decision: Live owns id, title, createdAt, parentID; Retitle; NewLive/Begin fire no onMove)
- 23: guard folded (no identity state of its own; adoption observed from behaviour; sessionHost keeps now; widened -run and SessionHost diff)

**Sources:**
- `docs/reviews/architecture-review-2026-09-20.html` (cards #8, #9, #10, #14, #15, #16, #17)
- ADR 0022, 0030 §6, 0031, 0035, 0043, 0069, 0071, 0076, 0083
- `CONTEXT.md` (Retired terms; Parallel agents; the Scratch-dir paragraph on read mounts)

**Ratified design calls (owner, 2026-09-24):**
- **Scope:** all of #8 (incl. 8.3), #9, #10 (10.1–10.4), #14 (14.1–14.2), #16 (16.1–16.3), #17 (a, c, docs); #15 only its Bind-order fix.
- **Declined:** #15 replay list (ADR 0083 §2 accepted residue); #17 record-write queue, facade options, probe/terminal.go split; #14 ReadOnly fold.
- **git_show card:** `read_file` parity — label "Git Show", target `path @ ref`, stat `N lines`, no file content in the body.
- **ADR statuses:** existing forms plus a pointer note in ADR 0076 Consequences; no new "retired" form.
- **#14.2:** allowed over the 8-file cap (mechanical rename; a split needs a temporary double field).
- **#10 bad file values:** keep each row's current "stated" predicate — no user-visible change, no #10.5.
- **SubprocessPermit:** enforced in `tools.RunHookSubprocess`.
- **#16 swallowed hold refusal:** keep today's behaviour; tracked as `apogee-refused-hold-followers-move`.
- **Internal calls (accepted as recommended):** #8 `promptSel`→`fieldSel`, prompt stays out of the shared geometry, glyph helpers on `lineEditor`; #9 separate `parseArgs`/`run` fields plus a test that every row runs; #10 unexported typed field on `Key`, per-row file closures, one `Key.Copy` + `applyMirror`; #14 `domain.ReadMounts` + tools alias, keep `HostTools` and the `Roots` name; #15 typed pending fields stay; #16 `session.Live` in `internal/session`, Firing hold in `run.Once`; `StripCwdLine` moves to `internal/domain`.

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 (`tea.KeyPressMsg`, `msg.String()`); never hold a no-copy type by value on the Model (ADR 0011).
- One package per `go test` line; `make test`/`make check` only at closeout.

**Out of scope:** Windows ARM confiner failures (`apogee-windows-confiner-test-failures`); the PID-only journal decision in `SECURITY.md`; removing the arm layout belt calls (`apogee-arm-layout-calls-residue`); withholding followers after a refused hold (`apogee-refused-hold-followers-move`); `internal/refs/refs_test.go`'s quoted-path fixture (deliberate).

## 1. git_show gets its own tool card — ✅ DONE (2026-09-24)

NOTES (2026-09-24): TestToolRegistryCoversEveryBuiltInTool carries no ask_user/load_skill exemption on its reverse walk: tools.KnownToolNames already includes the host-delegate tools by construction, so the allowance the plan described would have been dead code (the test passes without it, and fails on a renamed git_show key in both directions).

NOTES (2026-09-24): the shared target helper is fileReadTarget(args, ref); readFileTarget and gitShowTarget both call it, so read_file's target is byte-identical to before.

NOTES (2026-09-24): consequential edit — internal/tui/doc.go: made necessary by git_show becoming a summary-bearing card (its "nine of the ten … the tenth, git_status" count now names eleven, with git_show the second repository-bound tool).

NOTES (2026-09-24): the readSpanStat and readFileBody doc comments in toolregistry.go now also name git_show, and toolsummary_pin_test.go's TestToolSummariesRenderThroughThePresenter comment says "nine of the eleven", naming git_show as the second repository-bound tool. Both follow from the same rule.

**What:**
**Goal:** every name in `tools.KnownToolNames()` has a row in the TUI `toolRegistry`, pinned by a test; `git_show` presents as "Git Show" with target `<path>[:a–b] @ <ref>[ · locate "…"]`, stat `N lines` from its `domain.ReadSpan`, and the `read_file` body (located lines only); the registry's coverage comments are true.
**Approach (assumed at the header base):** add a `"git_show"` row to `toolRegistry` in `internal/tui/toolregistry.go` modelled on `read_file`'s (`firstLineDetail`, `readSpanStat`, `readFileBody`); a `gitShowTarget` reuses `readFileTarget`'s span/locate head (factor a shared helper) and inserts `" @ "+ref`. No `wireDropped`. Fix the comment claiming full built-in coverage and the "ten tools that report a typed summary" count. Add `TestToolRegistryCoversEveryBuiltInTool` (walks `tools.KnownToolNames()`; reverse direction too, allowing the constant-keyed `ask_user`/`load_skill`), a `git_show` case in `TestPresentToolCall`, and `git_show` in `TestToolSummaryPinUsesRegisteredToolNames`'s list. In `docs/layout/tool-layout.md` add the `git_show` row after `git_log`, and amend the dated "five diff-bodied rows" note to "(six since `write_file` joined, 2026-09-04)" without rewriting it.
**Regression guard.** Fix by rule, not list: every comment in `internal/tui` that counts the summary-bearing tools or claims full registry coverage (`grep -rn 'ten tools\|ten summary\|the one summary-bearing\|full built-in' internal/tui/` — `toolregistry.go`, `toolpresent_test.go:44`, `toolsummary_pin_test.go:175-177`) states the set with `git_show` in it: `GitShow.Execute` returns `okSummary`, so `git_show` is the eleventh summary-bearing tool.
**Closes:** apogee-git-show-no-presenter
**Files:** internal/tui/toolregistry.go, internal/tui/toolregistry_test.go, internal/tui/toolpresent_test.go, internal/tui/toolsummary_pin_test.go, docs/layout/tool-layout.md
**Read first:** internal/tui/toolregistry.go — toolRegistry, readFileTarget, qualifiedTarget, readSpanStat, readFileBody; internal/tools/registry.go — KnownToolNames, builtinToolsWith;
internal/tools/git.go — gitShowSpec, gitShowArgs, GitShow.Execute; internal/tui/toolpresent_test.go — TestPresentToolCall, detailsText;
internal/tui/toolsummary_pin_test.go — TestToolSummaryPinUsesRegisteredToolNames; docs/layout/tool-layout.md — "Display details per tool"
**Tests:** new `TestToolRegistryCoversEveryBuiltInTool`; `TestPresentToolCall` git_show case (`a.go:1–5 @ HEAD~1`, stat `5 lines`).
**Acceptance:**
- `go test -race -count=1 -run 'TestToolRegistryCoversEveryBuiltInTool|TestPresentToolCall|TestToolSummaryPin' ./internal/tui/`
- `go vet ./internal/tui/`
**Commit:** `fix(tui): git_show gets its own tool card, and the registry is pinned to the tool set`

## 2. Code and CI stop naming moved plans — ✅ DONE (2026-09-24)

**What:**
**Goal:** no tracked non-Markdown file outside `docs/` names a `docs/plans/<file>` (full name or `"YYYY-MM-DD - NN"` prefix) that exists only under `docs/plans/archived/`, except the deliberate quoted-path fixture in `internal/refs/refs_test.go`.
**Approach (assumed at the header base):** mechanical comment rewrite `docs/plans/` → `docs/plans/archived/` at every such site (rule, not list: `git grep -nE 'docs/plans/[^a]' -- ':!docs' ':!*.md' ':!internal/refs/refs_test.go'`). Known sites include `.github/workflows/ci.yml`, `Makefile`, `internal/tui/model.go`, `internal/tui/smoke_live_test.go`, many `internal/tui` files citing `"2026-08-11 - 01"`, `"2026-08-19 - 05"`, `"2026-08-11 - 00"`, `"2026-08-31 - 05"` and others, and `internal/agent/advise_argv_test.go` (points into the sibling `../apogee-sim` repo; its plan now sits under that repo's `docs/plans/archived/`). The ambiguous prefix `2026-08-06 - 04` (in `environ_windows.go`, `syncoutput.go`) is spelled out in full as the windows-tui-ghosting plan. Comments only — no code changes.
**Files:** .github/workflows/ci.yml, Makefile, internal/agent/advise_argv_test.go, internal/tui/*.go (comment lines only), and any other file the rule's grep finds
**Read first:** .github/workflows/ci.yml — header comment; Makefile — header comment; internal/agent/advise_argv_test.go — armSentence; internal/tui/model.go — windows-tui-ghosting comment;
internal/tui/environ_windows.go — noCaps comment; internal/tui/syncoutput.go — THE MEASUREMENT comment; internal/tui/toolregistry.go — toolPresenter.failure;
internal/refs/refs_test.go — quoted-path fixture (excluded)
**Tests:** none (comments only).
**Acceptance:**
- `test -z "$(git grep -nE 'docs/plans/[^a]' -- ':!docs' ':!*.md' ':!internal/refs/refs_test.go' ':!.beads')"`
- `gofmt -l internal/ cmd/` prints nothing; `go vet ./internal/tui/` ; `go vet ./internal/agent/`
**Commit:** `docs: code and CI comments point at archived plans`

## 3. ADR statuses name what retired them — ✅ DONE (2026-09-24)

**What:**
**Goal:** ADR 0003 and 0015 read `Status: superseded by ADR 0076`; ADR 0014 reads `Status: superseded by ADR 0071`; ADR 0002 reads `Status: accepted; the Mechanism-catalogue half superseded by ADR 0076`; each carries a one-line dated pointer note under its header; ADR 0076's Consequences name all four; the facade index in `apogee.go` cites the ADR that governs Auto-mode confinement, not superseded ADR 0004.
**Approach (assumed at the header base):** copy the status forms already used (0004, 0016, 0009). ADR 0076 D1 deleted the registry, `Config.EnableMechanisms` and the catalogue; ADR 0071 D5 amended 0014 "by removal". In `apogee.go` replace the "ADR 0004 Auto mode requires Confinement" citation with ADR 0012 (read 0012 first to confirm its title).
**Regression guard.** Re-point every `ADR 0004` in `apogee.go` to ADR 0012 — three sites (`grep -n 'ADR 0004' apogee.go`: the package-doc index, `New`, `Confiner`) — so the item's own `! grep -n 'ADR 0004' apogee.go` passes. In the same edit re-point or annotate the index's ADR 0003 line (for example "ADR 0076 (supersedes 0003)"), and check the ADR 0002 line against its new split status.
**Files:** docs/adr/0002-*.md, docs/adr/0003-*.md, docs/adr/0014-*.md, docs/adr/0015-*.md, docs/adr/0076-*.md, apogee.go
**Read first:** apogee.go — package doc index, New, Confiner; docs/adr/0004-*.md — status + superseded blockquote; docs/adr/0016-*.md — status form;
docs/adr/0071-*.md — Amends header, Decision 5 paragraph on ADR 0014/0015; docs/adr/0076-*.md — Decision D1, Consequences; docs/adr/0012-*.md — title, Supersedes
**Tests:** none.
**Acceptance:**
- `grep -n '^Status:' docs/adr/0002-*.md docs/adr/0003-*.md docs/adr/0014-*.md docs/adr/0015-*.md`
- `grep -c 'ADR 000[23]\|ADR 001[45]' docs/adr/0076-*.md` ≥ 1; `! grep -n 'ADR 0004' apogee.go`; `go vet .`
**Commit:** `docs(adr): mark ADRs 0002, 0003, 0014 and 0015 as superseded`

## 4. CONTEXT.md stops describing retired behaviour as live — ✅ DONE (2026-09-24)

NOTES (2026-09-24): the Parallel agents wording says `min(width, remaining)` rather than the approach's `min(cap, remaining)`, since the batch follows the stated width (which is the target's cap while latched, 1 on a sub-agent), not the session cap alone.

**What:**
**Goal:** no live `CONTEXT.md` entry describes guided decomposition or a lab-only Mechanism as current: the Parallel agents entry states the cap as the width the engine reports to Reactions (`LoopView.ParallelAgents()`), the loop entry's "formerly lab-only Mechanisms" sentence no longer names an unported Mechanism, and the Task list entry's contrast links Retired terms.
**Approach (assumed at the header base):** Parallel agents — replace "the width of a guided decomposition **batch** (`min(cap, remaining)` delegations per Turn)" with: "the width the engine states to Reactions (`LoopView.ParallelAgents()`: the cap at depth 0, 1 deeper down), so an engine-origin Reaction that synthesizes delegations batches `min(cap, remaining)` of them per Turn by the number dispatch will honour. No shipped Reaction does." The loop — "…lets a behaviour that once had to live outside the loop (a tool-result correction, say) be a first-class seam Reaction." Task list — "…rather than a Reaction steering the model's plan (the retired guided decomposition — see [Retired terms](#retired-terms))." Add a dated one-line note to ADR 0039 D2(b) pointing at the Reaction-view reading. Leave `_Avoid_` lines, Retired terms entries and ADR links as they are.
**Regression guard.** The dated ADR 0039 note goes on Decision 1(b) ("*Mechanism:* guided decomposition dispatches…"), not D2(b) — Decision 2 has no (b); optionally annotate the Decision headline too. The Parallel agents wording must not call `LoopView.ParallelAgents()` "the cap at depth 0": word it as the width dispatch uses — the Sub-agent server's cap while a Delegation target is latched (`delegationCap`), else this cap, and 1 deeper down — or point at the Fan-out ceiling entry's wording. The absence check greps one physical line (`decomposition \*\*batch`), because the old phrase wraps across CONTEXT.md:296-297.
**Files:** CONTEXT.md, docs/adr/0039-*.md
**Read first:** CONTEXT.md — Parallel agents, The loop, Task list, Retired terms; internal/domain/hooks.go — LoopView.ParallelAgents, Request.SetParallelAgents;
internal/agent/dispatch.go — delegationWidth, delegationCap; internal/agent/loop.go — newProjection; docs/adr/0039-*.md — Decision 1
**Tests:** none.
**Acceptance:**
- `! grep -n 'decomposition \*\*batch' CONTEXT.md`; `! grep -n 'formerly lab-only Mechanisms' CONTEXT.md`
- `grep -n 'ParallelAgents()' CONTEXT.md`
**Commit:** `docs(context): parallel agents, the loop and the task list stop naming retired behaviour`

## 5. StripCwdLine moves to internal/domain — ✅ DONE (2026-09-24)

NOTES (2026-09-24): the prefix constant is exported as domain.CwdLinePrefix, since internal/tools' subprocessToolResult still writes it from outside the package.

**What:**
**Goal:** `internal/tui` imports nothing from `internal/tools` (`go list -deps ./internal/tui` shows no `internal/tools`), the cwd-line strip lives in `internal/domain` with its prefix, and `internal/tui/doc.go`'s "does not import internal/tools" sentence is true.
**Approach (assumed at the header base):** move `StripCwdLine` and its prefix constant from `internal/tools/terminal.go` to `internal/domain` (new file or the neighbouring tool-result file); `tools` keeps using it via `domain`. Update callers `internal/tui/toolregistry.go`, `cmd/apogee/headless.go`, and tests `internal/tools/terminal_test.go`, `internal/tools/python_exec_test.go`, `internal/tui/toolpresent_test.go`, `cmd/apogee/headless_test.go`. No behaviour change. Depends on item 1 (shared file).
**Regression guard.** `internal/domain/doc.go`'s file map names `cwdline.go` in its "Tools and confinement" paragraph (beside `tools.go`/`tooledit.go`/`toolsummary.go`), or `TestDocMapNamesEveryFile` goes red. Every comment naming `StripCwdLine` or `cwdLinePrefix` names `domain` (sites include `toolpresent_test.go:709,822`, `toolregistry.go:756`, `headless.go:324`, `headless_test.go:3781`, `terminal.go:251`): `git grep -n 'tools\.StripCwdLine\|internal/tools, StripCwdLine\|cwdLinePrefix' -- '*.go' ':!internal/domain'` is empty.
**Files:** internal/domain/cwdline.go, internal/domain/cwdline_test.go, internal/domain/doc.go, internal/tools/terminal.go, internal/tools/terminal_test.go, internal/tools/python_exec_test.go, internal/tui/toolregistry.go, internal/tui/toolpresent_test.go, cmd/apogee/headless.go, cmd/apogee/headless_test.go
**Read first:** internal/tools/terminal.go — cwdLinePrefix, StripCwdLine, subprocessToolResult; internal/tui/toolregistry.go — subprocessFailure, subprocessDetail; cmd/apogee/headless.go — narrationSink.resultLine;
internal/tools/terminal_test.go — TestStripCwdLine; internal/tools/python_exec_test.go — TestPythonExec_WorkspaceDoesNotShadowTheStdlib; internal/domain/doc.go — file map "Tools and confinement";
internal/domain/docmap_test.go — TestDocMapNamesEveryFile; internal/tui/doc.go — "does not import internal/tools" sentence
**Tests:** move the strip's unit tests to `internal/domain`.
**Acceptance:**
- `! go list -deps ./internal/tui | grep -x 'github.com/airiclenz/apogee/internal/tools'`
- `test -z "$(git grep -n 'tools\.StripCwdLine\|internal/tools, StripCwdLine\|cwdLinePrefix' -- '*.go' ':!internal/domain')"`; `go test -race -count=1 -run 'DocMap' ./internal/domain/`
- `go test -race -count=1 -run 'CwdLine|Strip' ./internal/domain/`; `go test -race -count=1 -run 'CwdLine|Terminal|PythonExec' ./internal/tools/`; `go test -race -count=1 -run 'ToolRegistry|PresentToolCall' ./internal/tui/`; `go test -race -count=1 -run 'Headless' ./cmd/apogee/`
**Commit:** `refactor(domain): the cwd-line strip lives in domain, so tui no longer imports tools`

## 6. Stale engine and TUI comments state the code — ✅ DONE (2026-09-24)

NOTES (2026-09-24): consequential edit — internal/agent/setlive_test.go: made necessary by dropping the ordinal counts from the anytime-safe class (the test comment called SetParallelAgents "the fifth anytime-safe setter"; now "another")
NOTES (2026-09-24): by the regression guard's rule, agent.go also drops the "fifth" field ordinal on parallelAgentsMu, restates genMu as the one member covering a compound value (parallelAgentsMu now guards two fields, parallelAgents and farWidth), and restates effortMu's "the one member with NO cfg seed" (the Delegation latch and the seat have none either)

**What:**
**Goal:** these comments state the shipped code: `internal/tui/doc.go`'s "an arm mutates; Update's tail lays out" names the remaining belt calls as residue (tracked by `apogee-arm-layout-calls-residue`); `internal/agent/subagent.go`'s "nothing is written to the child after it is built" says `newChildAgentOn` writes nothing after construction while `runSubAgent` writes the call-derived roster, output target, baseline and step cap (ADR 0083 §3); `internal/agent/agent.go`'s anytime-safe setter list is complete (SetMode, SetConfineToWorkspace, SetScratchDir, SetReactions, SetCompactionEnabled, SetPruneToolResults, SetContextFiles, SetParallelAgents, SetEffortOverride, SetDelegationTarget, SetDelegationSeat) with `SetJournal` and `SwitchUpstream` on the idle-only list, and it says each setter's doc states its class; `internal/tui/sessionsave.go` and ADR 0022's "C7 deliberately still open" settle as "the fold owns ordering, the store owns atomicity".
**Approach (assumed at the header base):** comment edits only. First run `grep -rn "does not import internal/tools\|anytime-goroutine-safe\|C7" internal/*/*_test.go` for pinned wording. Verify the setter classes against each setter's own doc before listing.
**Regression guard.** Fix by rule: every comment in `internal/agent` claiming a child is not written after construction (`grep -rn 'nothing is written\|written to it afterwards' internal/agent/` — `subagent.go` and `construct.go:39` `newDelegateAgent`), and every call-class statement in the package (`grep -rn 'anytime-safe\|anytime-goroutine-safe\|modeMu class' internal/agent/` — `agent.go:51,144,198,222`, `delegationtarget.go:169`, `interject.go:26`). The class sentence no longer says each member swaps ONE live field: `SetDelegationSeat` also clears the far width (`forgetFarWidth`), and `agent.go:198`'s "a sixth live field" count is restated.
**Files:** internal/tui/doc.go, internal/agent/subagent.go, internal/agent/construct.go, internal/agent/agent.go, internal/agent/delegationtarget.go, internal/agent/interject.go, internal/tui/sessionsave.go, docs/adr/0022-*.md
**Read first:** internal/agent/agent.go — Agent type doc (call classes), SetScratchDir, SetJournal, SetEffortOverride, isDelegate; internal/agent/delegationseat.go — SetDelegationSeat; internal/agent/rebind.go — SwitchUpstream;
internal/agent/subagent.go — runSubAgent, newChildAgentOn; internal/agent/construct.go — newDelegateAgent; internal/tui/doc.go — "an arm mutates" invariant;
internal/tui/sessionsave.go — recordWriteKind fold comment; docs/adr/0022-*.md — C7 paragraph
**Tests:** none.
**Acceptance:**
- `go vet ./internal/agent/`; `go vet ./internal/tui/`
- `grep -n 'SetDelegationSeat' internal/agent/agent.go | head -3`
**Commit:** `docs: engine and TUI comments state the setters, the child writes and the save fold as shipped`

## 7. The recall store serves the TUI directly — ✅ DONE (2026-09-24)

NOTES (2026-09-24): consequential edit — cmd/apogee/wire.go: made necessary by deleting recallHost (its file-map entry called wire_session.go the "session-persistence and prompt-recall hosts"; re-worded like doc.go's)
NOTES (2026-09-24): ADR 0043 line 78 still lists "session/recall hosts" among cmd/apogee's seams; left as written, since it records the split decision as taken rather than describing the live tree

**What:**
**Goal:** `cmd/apogee` has no `recallHost` adapter; a `*recall.Store` bound to its workspace satisfies `tui.RecallHost` (asserted in `cmd/apogee/wire_options.go`), with no behaviour change.
**Approach (assumed at the header base):** `recall.New(dir, workspace)` binds the workspace; `AppendPrompt`/`LoadPrompts` become `*recall.Store` methods with the TUI's signatures; `recall` does not import `tui`. The digest-collision tests build two stores over one dir. Delete `recallHost` from `wire_session.go`; update the one caller in `wire_options.go` and the `tui.RecallHost` doc comment.
**Regression guard.** `cmd/apogee/doc.go`'s file map stops calling `wire_session.go` a prompt-recall host: re-word its `wire_session.go` entry (the recall host is now `*recall.Store`, asserted in `wire_options.go`).
**Files:** internal/recall/store.go, internal/recall/doc.go, internal/recall/store_test.go, cmd/apogee/wire_session.go, cmd/apogee/wire_options.go, cmd/apogee/wire_boot_test.go, cmd/apogee/doc.go, internal/tui/tui.go
**Read first:** cmd/apogee/wire_session.go — recallHost, newRecallHost; cmd/apogee/wire_options.go — Options.Recall wiring; internal/recall/store.go — Store, New, Append, Load, path, normalize;
internal/recall/store_test.go — TestLoadFiltersForeignWorkspaceRecords, TestTwoWorkspacesTwoFiles, TestAppendLoadRoundTrip; cmd/apogee/wire_boot_test.go — TestRecallHostBindsWorkspace;
internal/tui/tui.go — RecallHost, Options.Recall; internal/tui/recall_test.go — fakeRecallHost
**Tests:** recall store tests for the bound workspace; `TestRecallHostBindsWorkspace` in cmd/apogee.
**Acceptance:**
- `! grep -n 'recallHost' cmd/apogee/*.go`
- `go test -race -count=1 ./internal/recall/`; `go test -race -count=1 -run 'Recall' ./cmd/apogee/`; `go test -race -count=1 -run 'Recall' ./internal/tui/`
**Commit:** `refactor(recall): the recall store is the TUI's recall host; the adapter goes`

## 8. Per-call context carriers are documented in one table — ✅ DONE (2026-09-24)

NOTES (2026-09-24): the §11 table names `tools.RunHookSubprocess` as the `subprocessPermitCtxKey` reader. That is the enforcement item 9 adds in the same wave (the ratified call "SubprocessPermit: enforced in tools.RunHookSubprocess"). At the time of writing there is no production `SubprocessPermitFromContext` reader, so the row is accurate only once item 9 lands.

**What:**
**Goal:** `docs/design/confinement-execution-contract.md` has a §11 "Per-call context carriers" table listing every production `context.WithValue` key (key, installer, reader, lifetime), and the installers in `internal/agent/dispatch.go` and `internal/domain/doc.go` point at it.
**Approach (assumed at the header base):** find the keys with `git grep -n 'context.WithValue' -- 'internal/*.go' ':!*_test.go'` (12 production keys; stubllm's test key excluded). Keep them as separate keys — a typed CallContext is rejected (domain would import undo/console/tasklist; lifetimes differ). Docs and comments only.
**Regression guard.** The Acceptance checks every named file, not any one: the contract table and both installer pointers (`dispatch.go`, `domain/doc.go`) must each match `Per-call context carriers`.
**Files:** docs/design/confinement-execution-contract.md, internal/agent/dispatch.go, internal/domain/doc.go
**Read first:** internal/domain/confinement.go — WithConfinement, WithoutConfinement, WithSubprocessPermit, WithWriteEscapePermit; internal/domain/ask.go — WithSubAgentTask, WithSubAgentName, WithSubAgentDepth, WithSpawnCallID, WithConsoleOwner; internal/domain/promptslot.go — WithPromptSlot;
internal/agent/dispatch.go — syncPermitCtx, writeEscapeCtx, executeRun, the child ctx block installing undo/console/tasklist; internal/agent/loop.go — WithPromptSlot install;
internal/undo/context.go, internal/tasklist/context.go, internal/console/context.go — With*; docs/design/confinement-execution-contract.md — §10.x (append §11)
**Tests:** none.
**Acceptance:**
- `test -z "$(grep -L 'Per-call context carriers' docs/design/confinement-execution-contract.md internal/agent/dispatch.go internal/domain/doc.go)"`
- `go vet ./internal/agent/`; `go vet ./internal/domain/`
**Commit:** `docs(contract): one table of the per-call context carriers`

## 9. A hook subprocess spawns only under a permit — ✅ DONE (2026-09-24)

NOTES (2026-09-24): the refusal is an unexported sentinel `errNoSubprocessPermit` in internal/tools/exec_common.go rather than a new domain error, so the item stays inside its three named files; the agent path passes the error through verbatim.
NOTES (2026-09-24): §10 of docs/design/confinement-execution-contract.md (in-flight under item 8) still reads true ("the seam keeps the may-not-spawn default") but does not yet name RunHookSubprocess as the enforcement site; that file was left alone.

**What:** Defect: `domain.SubprocessPermitFromContext` has no production reader, so the contract "a hook that reads false must not spawn" is unenforced (not exploitable today: the one caller installs the permit just before spawning).
**Goal:** `tools.RunHookSubprocess` refuses to spawn, with an error naming the missing permit, when the context carries no permit or a false one; the sync-exec path, which installs the permit, still spawns.
**Approach (assumed at the header base):** at the top of `RunHookSubprocess` (`internal/tools/exec_common.go`) read `domain.SubprocessPermitFromContext`; refuse before any process starts. Update the contract comment in `internal/domain/confinement.go` to say it is enforced there.
**Regression guard.** Refuse only when `SubprocessPermitFromContext` reports `ok=false`; there is no "false" permit — a present permit with nil `Confinement` is the unfenced grant `syncPermitCtx` mints with confine-to-workspace off, and it still spawns unfenced. Each existing `RunHookSubprocess` test in `exec_common_test.go` (seven pass a bare `context.Background()`) installs `domain.WithSubprocessPermit(ctx, domain.SubprocessPermit{})` before calling.
**Closes:** apogee-subprocess-permit-unread
**Files:** internal/tools/exec_common.go, internal/tools/exec_common_test.go, internal/domain/confinement.go
**Read first:** internal/tools/exec_common.go — RunHookSubprocess, confinementBox; internal/tools/exec_common_test.go — TestRunHookSubprocessScrubsApogeeCredentials, TestRunHookSubprocessRefusesAProgramInsideTheWorkspace, plantExecutable;
internal/domain/confinement.go — SubprocessPermit, WithSubprocessPermit, SubprocessPermitFromContext; internal/agent/dispatch.go — syncPermitCtx; internal/agent/syncexec.go — runSyncArgv;
internal/agent/syncexec_test.go — TestSyncArgvRunsUnfencedAndReturnsStdout, TestSyncArgvRunsConfinedWhenTheBoxCanBeBuilt; internal/agent/hookpermit_test.go — TestHookSubprocessPermitLadder
**Tests:** new: no permit → refused, no child spawned; permit → spawns; a present permit with nil `Confinement` → spawns unfenced. Existing `RunHookSubprocess` tests install the unfenced permit; existing `TestSyncArgv*` and gate tests stay green.
**Acceptance:**
- `go test -race -count=1 -run 'HookSubprocess|Permit' ./internal/tools/`
- `go test -race -count=1 -run 'SyncArgv|Advise|Gate|Hook' ./internal/agent/`
**Commit:** `fix(tools): a hook subprocess spawns only under a subprocess permit`

## 10. Each slash command's behaviour lives on its row — ✅ DONE (2026-09-24)

NOTES (2026-09-24): a fifth adapter, actuationVerb(verb), serves /unload-model and /stop-server (both called startServerActuation with a constant, which none of the four named adapters can express); it carries the two arms' reasoning in its doc.
NOTES (2026-09-24): consequential edit — internal/tui/autotitle.go: made necessary by removing the /rename arm (its idle-only reasoning moved onto runRename's doc)
NOTES (2026-09-24): consequential edit — internal/tui/colorscheme.go: made necessary by removing the /color-scheme arm (its reasoning moved onto runColorScheme's doc)
NOTES (2026-09-24): consequential edit — internal/tui/fork.go: made necessary by removing the /fork arm (its idle-only reasoning moved onto runFork's doc)
NOTES (2026-09-24): consequential edit — internal/tui/picker.go: made necessary by removing the /model, /server and /sub-agents-server arms (their reasoning moved onto the three runX docs)
NOTES (2026-09-24): consequential edit — internal/tui/skillscmd.go: made necessary by removing the /skills arm (its reasoning moved onto runSkillsCommand's doc)
NOTES (2026-09-24): consequential edit — internal/tui/undo.go: made necessary by removing the /undo and /redo arms (their reasoning moved onto runUndo/runRedo's docs; the file header's "commandrun.go says why" pointer now names them)
NOTES (2026-09-24): consequential edit — internal/tui/effort.go: made necessary by removing the /effort arm (its mid-Exchange reasoning moved onto runEffortCommand's doc; the file header's "commandrun.go says why" pointer now names runEffortCommand)

**What:**
**Goal:** `commandSpec` rows carry a `run` field; `runCommand` keeps its three gates (parse error, actuation latch, upstream block) and has no per-verb switch; `TestEveryCommandRowRuns` asserts `run != nil` for every row.
**Approach (assumed at the header base):** adapters `bareVerb`, `tokenVerb`, `restVerb`, `typedVerb[T]`; lift the inline arms (continue, compact, version, help) into `runContinue`, `runCompact`, `runVersion`, `runHelp`, keeping their side-effect order (continue: `m.detached=false` → `addUser` → `startExchange`; compact: `m.layout()` → `startCompact`). `parseArgs` and `run` stay separate fields. Fill `commandSpecs` in `init()` (a var initializer is an init cycle via `help`→`helpNote`→`commandSpecs`); the doc comment warns package vars not to read it at declaration. Never copy `run` onto `parsedInput` (it sits on the Model, ADR 0011). Move arm-only reasoning onto the `runX` docs; update `doc.go`'s "switchboard" prose.
**Files:** internal/tui/command.go, internal/tui/commandrun.go, internal/tui/command_test.go, internal/tui/doc.go
**Read first:** internal/tui/commandrun.go — Model.runCommand; internal/tui/command.go — commandSpec, commandSpecs, verbGrammar, verbArgsOf, parseInput, commandByName; internal/tui/help.go — helpNote;
internal/tui/picker.go — init (pickerOfferings: the house init() pattern); internal/tui/actuation.go — actuationBlocked;
internal/tui/command_test.go — TestCommandTableDrivesParserAndMenu, TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs; internal/tui/doc.go — runCommand "switchboard" paragraph
**Tests:** new `TestEveryCommandRowRuns`; existing `TestCommandTableDrivesParserAndMenu`, `TestHelpNoteListsEveryVerb`, `TestTheActuationLatchRefusesExactlyTheServerAndExchangeVerbs`, `TestEffort*`.
**Acceptance:**
- `! grep -n 'switch parsed.command' internal/tui/*.go`
- `go test -race -count=1 -run 'Command|Help|Effort|Undo|Redo|Confine|ColorScheme|Skills|Schedule|Fork|Compact|Continue|Rename|Actuation|DocMap|Seam' ./internal/tui/`
**Commit:** `refactor(tui): each slash command's behaviour lives on its command row`

## 11. One ReadMounts value on the tools side — ✅ DONE (2026-09-24)

NOTES (2026-09-24): consequential edit — internal/domain/doc.go: made necessary by the new internal/domain/readmounts.go (the package file map, checked by TestDocMapNamesEveryFile)
NOTES (2026-09-24): consequential edit — internal/domain/config.go: made necessary by removing HostTools.ExtraReadRoots (the skillFilesLine comment named `tools.HostTools.ExtraReadRoots`; now `tools.HostTools.ReadMounts.Roots`); comment only, Config's three fields are untouched until item 12
NOTES (2026-09-24): readScope is built with the literal `readScope{root: root, mounts: mounts}` at the six constructors (the plan offered that or a newReadScope helper); the four-clause contract that lived on HostTools.ExtraReadRoots/ScratchReadRoot/VirtualReadRoots now lives once on domain.ReadMounts

**What:**
**Goal:** `domain.ReadMounts{Roots, Scratch, Virtual}` (funcs, live) holds the one contract doc; `tools.ReadMounts` is an alias of it; `HostTools` carries one `ReadMounts` field; `readScope` holds `{root, mounts}`; the host-field drift tests check every sub-field.
**Approach (assumed at the header base):** new `internal/domain/readmounts.go`. Replace `HostTools.ExtraReadRoots`/`ScratchReadRoot`/`VirtualReadRoots` with `ReadMounts`; delete `readMounts()`. The funcs stay funcs (scratch follows session rotation; roots/virtual follow skill settings). `TestHostToolsOfFillsEveryHostField` and `TestHostToolsForFillsEveryHostField` check each sub-field (a struct-level zero check passes with one func set). `Config` keeps its three fields until item 12.
**Regression guard.** Go refuses a method on an alias of a non-local type, so `ReadMounts.scope` (path_read.go:202) becomes a tools-local constructor (`newReadScope(root, mounts)` or the literal `readScope{root, mounts}`), and its six callers (`read_file.go`, `list_dir.go`, `grep.go`, `find_files.go`, `file_ops.go`, `present_document.go`) move with it. `HostToolsOf` keeps reading the three `Config` fields into the one `HostTools.ReadMounts` (item 12 drops them), so the absence grep filters the `cfg.`/`Config.` field reads and `registry_test.go`'s `domain.Config` literal.
**Files:** internal/domain/readmounts.go, internal/tools/path_read.go, internal/tools/path_virtual.go, internal/tools/registry.go, internal/tools/doc.go, internal/tools/read_file.go, internal/tools/list_dir.go, internal/tools/grep.go, internal/tools/find_files.go, internal/tools/file_ops.go, internal/tools/present_document.go, internal/tools/path_read_test.go, internal/tools/registry_test.go, cmd/apogee/wire_tools_test.go
**Read first:** internal/tools/path_read.go — ReadMounts, ReadMounts.scope, readScope, readScope.extraRoots; internal/tools/registry.go — HostTools, HostToolsOf, builtinToolsWith, HostTools.readMounts; internal/tools/path_virtual.go — readScope virtual lookup (s.virtual);
internal/tools/read_file.go — NewReadFile (also NewListDir, NewGrep, NewFindFiles, NewCopyFile, NewPresentDocument); internal/tools/registry_test.go — TestHostToolsOfFillsEveryHostField;
cmd/apogee/wire_tools_test.go — TestHostToolsForFillsEveryHostField, TestRegistryWithMCPThreadsExtraReadRoots; internal/tools/path_read_test.go — readScope{root, extra/scratch} literals
**Tests:** existing read-scope and host-field tests.
**Acceptance:**
- `! grep -n 'ExtraReadRoots\|ScratchReadRoot\|VirtualReadRoots' internal/tools/*.go | grep -v '\(cfg\|Config\)\.\(ExtraReadRoots\|ScratchReadRoot\|VirtualReadRoots\)\|ReadRoots\?: *func'`
- `go test -race -count=1 -run 'ReadScope|Virtual|HostTools|ExtraRoot|DeclareReadOnly' ./internal/tools/`; `go test -race -count=1 -run 'HostToolsFor|RegistryWithMCPThreads' ./cmd/apogee/`
**Commit:** `refactor(tools): the read mounts are one domain.ReadMounts value`

## 12. Config carries the one ReadMounts — ✅ DONE (2026-09-24)

NOTES (2026-09-24): the header Sources line the regression guard asks to fix already reads "the Scratch-dir paragraph on read mounts" at this tree — nothing to change there
NOTES (2026-09-24): domain.Config's three long field docs collapse into one ReadMounts field doc that keeps only the Config-specific clauses (default-tool-set only, the engine never defaults it, the host owns the real-path trust call, why Scratch is a func, and the sub-fields-only rule); the shared four-clause contract stays on domain.ReadMounts (item 11). The now-unused "io/fs" import leaves config.go
NOTES (2026-09-24): the Firing-shows-all-three-mounts test lives in TestFiringConfigSetsEveryUnattendedField (Roots and Virtual non-nil, Scratch answers the record's scratch dir); TestEveryDriverCarriesTheProjectedConfig keeps comparing Roots and Virtual through assertCarriesProjection — Scratch is not the projection's, the session sets it later in wireSession
NOTES (2026-09-24): gofmt realigned HostToolsOf's whole keyed literal in internal/tools/registry.go once the multi-line ReadMounts literal became one line (alignment only)
NOTES (2026-09-24): consequential edit — internal/agent/loop.go: made necessary by removing Config.ExtraReadRoots (standingMeasured's comment named it)
NOTES (2026-09-24): consequential edit — cmd/apogee/wire_engine.go: made necessary by removing Config.ScratchReadRoot (lateEngine.ScratchDir's comment named it)
NOTES (2026-09-24): consequential edit — cmd/apogee/toolchain_roots.go: made necessary by removing Config.ExtraReadRoots (package comment named it)
NOTES (2026-09-24): internal/skills/load.go:75's "(VirtualReadRoots)" names the skills Provider method, not the Config field, and stays; load.go:199 was updated

**What:**
**Goal:** `domain.Config` carries `ReadMounts domain.ReadMounts` and none of `ExtraReadRoots`, `ScratchReadRoot`, `VirtualReadRoots`; the facade exposes an alias; `CONTEXT.md`'s read-mounts entry names the one value. Depends on item 11. Allowed over the file cap (owner call).
**Approach (assumed at the header base):** `gofmt -r` selector rename across `internal/agent` (incl. `orientation.go`), `cmd/apogee` and the facade `apogee.go`. Always assign sub-fields, never the whole value (sites in `wire_live.go`, `probecontext.go`, `wire_firing.go` set parts at different times; a whole-value assign wipes the others).
**Regression guard.** Files gains internal/tools/registry.go and internal/tools/registry_test.go — item 12 is the one that drops HostToolsOf's three cfg.* mount reads for cfg.ReadMounts and updates its drift test; item 11 leaves HostToolsOf reading the three Config fields into the one HostTools.ReadMounts. Also fix the header Sources line: "CONTEXT.md (Retired terms; Parallel agents; Read mounts)" becomes "CONTEXT.md (Retired terms; Parallel agents; the Scratch-dir paragraph on read mounts)". The absence grep targets the Config fields only: `skills.Provider.VirtualReadRoots` and the `Test*` names stay, and the `internal/skills` comments naming the Config fields (`load.go`, `provider.go`, `provider_test.go`) are updated. CONTEXT.md's read-mount site is the Scratch-dir paragraph (CONTEXT.md:1120). `gofmt -r` does not rewrite keyed-literal keys: the construction site (`projectConfig` in `wire_config.go`) and the keyed literals in `internal/agent/extrareadroots_test.go` are hand-edited into one whole `ReadMounts{…}` value; the sub-fields-only rule binds post-construction assigns.
**Files:** internal/domain/config.go, internal/agent/*.go (mount selectors), cmd/apogee/*.go (mount selectors), apogee.go, CONTEXT.md, internal/tools/registry.go, internal/tools/registry_test.go, internal/skills/load.go, internal/skills/provider.go, internal/skills/provider_test.go (comments only)
**Read first:** internal/domain/config.go — Config.ExtraReadRoots, ScratchReadRoot, VirtualReadRoots; cmd/apogee/wire_config.go — projectConfig (ExtraReadRoots/VirtualReadRoots keys); internal/tools/registry.go — HostToolsOf;
cmd/apogee/wire_live.go — w.cfg.ScratchReadRoot assign; cmd/apogee/wire_firing.go — firingConfig scratch assign; cmd/apogee/probecontext.go — scratch assign;
internal/agent/orientation.go — library roots line; apogee.go — Config alias block
**Tests:** `TestEveryDriverCarriesTheProjectedConfig` plus a Firing test must show all three mounts set.
**Acceptance:**
- `! git grep -nE '\b(ExtraReadRoots|ScratchReadRoot)\b|Config\.VirtualReadRoots|cfg\.VirtualReadRoots|^\s*VirtualReadRoots:' -- '*.go'`
- `! grep -n 'ScratchReadRoot\|ExtraReadRoots' CONTEXT.md`; `grep -n 'ReadMounts *= *domain.ReadMounts' apogee.go`
- `go test -race -count=1 -run 'HostTools' ./internal/tools/`
- `go test -race -count=1 -run 'ExtraReadRoots|ReadMounts|Skill|Orientation_' ./internal/agent/`; `go test -race -count=1 -run 'RegistryWithMCPThreads|EveryDriverCarriesTheProjectedConfig|HostToolsFor|ReadRoots|Firing' ./cmd/apogee/`
**Commit:** `refactor: Config carries one ReadMounts from the host to the read tools`

## 13. A late-bound Agent keeps the far delegation width — ✅ DONE (2026-09-24)

NOTES (2026-09-24): internal/run/run.go (run.Once, item 14's file, in flight) applies SetDelegationTarget before SetDelegationSeat too — the same order defect on the headless Firing path, where no heartbeat ever re-states the target; not edited here (outside this item's Files and owned by a sibling in flight).

**What:** Defect: `lateEngine.Bind` replays the pending delegation target before the pending seat; `SetDelegationSeat` calls `forgetFarWidth`, so a late-bound Agent loses the far width until the next heartbeat beat.
**Goal:** `Bind` replays the seat before the target, with a comment stating the order; a late-bound Agent reports the pending target's far width immediately after `Bind`. ADR 0083 §2 gains a dated note that the typed pending fields are kept (card #15 declined).
**Approach (assumed at the header base):** swap the two replay branches in `lateEngine.Bind` (`cmd/apogee/wire_engine.go`). Add a dated note to ADR 0069 D6.
**Regression guard.** The test observes the far width through the system message, since `statedDelegationWidth` is unexported and `validCfg` rosters no `sub_agent`: bind a Config whose roster holds `sub_agent` against a capturing stub upstream, Step once, and assert the orientation's "up to N run at once" (`delegationWidthClause`) equals the pending target's `ParallelAgents` (distinct from the session width) — red with today's order. Goal and Approach agree on both dated notes: ADR 0083 §2 (the typed pending fields are kept, card #15 declined) and ADR 0069 D6 (the Bind order).
**Closes:** apogee-bind-forgets-far-width
**Files:** cmd/apogee/wire_engine.go, cmd/apogee/wire_engine_test.go, docs/adr/0069-*.md, docs/adr/0083-*.md
**Read first:** cmd/apogee/wire_engine.go — lateEngine.Bind, pendingDelegation, pendingSeat, lateEngine.SetDelegationSeat, lateEngine.SetDelegationTarget; internal/agent/delegationseat.go — SetDelegationSeat (forgetFarWidth); internal/agent/delegationtarget.go — SetDelegationTarget (stateFarWidth);
internal/agent/agent.go — statedDelegationWidth, forgetFarWidth; internal/agent/orientation.go — delegationBounds, delegationWidthClause; cmd/apogee/delegation.go — the seat-then-nil-target pushes on the human doors;
cmd/apogee/wire_engine_test.go — TestLateEngineRemembersTheDelegationSeatUntilTheBind; cmd/apogee/wire_helpers_test.go — validCfg
**Tests:** new `TestLateEngineBindKeepsTheFarWidthOfThePendingTarget`.
**Acceptance:**
- `go test -race -count=1 -run 'LateEngine|DelegationSeat|DelegationTarget' ./cmd/apogee/`
- `grep -n '2026-09-24' docs/adr/0069-*.md`; `grep -n '2026-09-24' docs/adr/0083-*.md`
**Commit:** `fix(cmd/apogee): Bind replays the seat before the target, so the far width survives`

## 14. A running Firing holds its session record — ✅ DONE (2026-09-24)

NOTES (2026-09-24): consequential edit — docs/manual/headless.md: made necessary by the Firing hold (the manual said an unattended headless or daemon run takes no hold of its own)
NOTES (2026-09-24): the hold lives in a small helper, holdRecord(spec), called from Once right after the sync-lane arm and before the undo journal opens; a refused hold is returned wrapped as "apogee: hold the firing's record: …" with the *session.HeldError reachable through errors.As
NOTES (2026-09-24): cmd/apogee acceptance was run in a scratch worktree at HEAD plus this item's files, because the shared tree's internal/config did not build mid-wave (item 15's in-flight edits); `go test -race -count=1 -run 'Undo' ./cmd/apogee/` passed there, as did -run 'Headless|Daemon|Firing|Schedule|Session' and the whole internal/run package

**What:** Defect: `apogee undo <id>` takes the session hold, but a running Firing holds nothing, so the verb can rewrite a live Firing's journal.
**Goal:** while `run.Once` runs with both a store and a record id, `store.Hold(spec.RecordID)` is held and released on return; `apogee undo <id>` against that record is refused with `*session.HeldError` naming the id; a run with no store or id takes no hold and touches no disk (ADR 0022).
**Approach (assumed at the header base):** hold in `run.Once` (`internal/run/run.go`), not in `raise` (a hold before the gates leaves a lock on every refused daemon tick). Dated notes on ADR 0022 and ADR 0074.
**Regression guard.** Take the hold after `agent.New`/`SetReactions`, so a construction refusal leaves no `<id>.lock`. Refuse the run only on `*session.HeldError`; any other `Hold` error (e.g. `ErrInvalidID` for `../escape`) runs unheld, so Save reports as today and `TestOnceReportsARecordIDThatCannotNameAFile` stays green. This supersedes `cmd/apogee/undo.go:44` ("Headless and daemon runs take no hold of their own … that gap is recorded, not closed here"): rewrite every comment saying an unattended run or Firing takes no hold (`grep -rn 'take no hold\|takes no hold\|Firing in flight' --include=*.go`).
**Closes:** apogee-firing-holds-no-record
**Files:** internal/run/run.go, internal/run/run_test.go, internal/run/doc.go, cmd/apogee/undo.go, cmd/apogee/undo_test.go, docs/adr/0022-*.md, docs/adr/0074-*.md
**Read first:** internal/run/run.go — Once, openRecordJournal; internal/session/store.go — Store.Hold, Store.hold, HeldError; cmd/apogee/undo.go — runUndoVerb (doc paragraph on unattended runs);
cmd/apogee/undo_test.go — TestUndoVerbRefusesAHeldSession, runUndoCmd; internal/run/run_test.go — TestOnceReportsARecordIDThatCannotNameAFile, TestOnceWithNoStoreKeepsTheInMemoryJournal, dirNames;
internal/stubllm/server.go — Server.Release (Turn await gate); cmd/apogee/wire_firing.go — raise
**Tests:** new: undo mid-Firing is refused with `*session.HeldError`; a store-less run creates no lock file; existing `TestOnceReportsARecordIDThatCannotNameAFile` stays green (an unnameable id runs unheld).
**Acceptance:**
- `go test -race -count=1 -run 'Once|Hold' ./internal/run/`; `go test -race -count=1 -run 'Undo' ./cmd/apogee/`
**Commit:** `fix(run): a running Firing holds its session record`

## 15. Config key descriptor, proven on the bool rows — ✅ DONE (2026-09-24)

NOTES (2026-09-24): re-derived from the binder name `bindRows` at the header base — the binder was `bindSetters` (registry.go), renamed to `bindRows` as the plan's regression guard directs; it derives each field row's Read, landing and file projection and panics on a field row that also hand-writes Read or Set.
NOTES (2026-09-24): the Approach's "file closures ... closing over the bound Set" is overridden by the item's own regression guard: the derived file projection is an unvalidated typed copy under the row's stated predicate (the file func answering nil), never Set; the row's default is parsed from `Key.Default` once at init.
NOTES (2026-09-24): the derived file projection is stored on an unexported `Key.fromFile` (beside the unexported `Key.field`), which `accessorsOver` reads — the shape item 17 extends to the hand-written rows.
NOTES (2026-09-24): config_test.go, registry_test.go were listed in Files but needed no change: TestKeyAccessorsBindDescribedKeys, TestEveryConfigKeyReachesTheOptions, TestRegistrySetIsTheInverseOfRead, TestRegistryDefaultsReadBackFromAnEmptyFile, TestRegistryIsBijectionWithFileConfig and TestUIRowsLandThroughUIPrefsSet pass unchanged over the derived rows.

**What:** Recast at the regression check (2026-09-24).
**Goal:** `internal/config/keyfield.go` defines an unexported `fieldSpec` interface and a generic `scalarField[T]`; every `KindBool` row of `KeyRegistry` except `context-files.enable` (`present.command-on-model-documents` included) carries a `field` from which Read, Set and its file value derive; those rows have no hand-written `fromFile` (`bypass` keeps its hand-written `fromEnv`/`fromFlag` until item 16), and a field row whose hand-written entry carries a `fromFile`, or a row with neither, panics at init.
**Approach (assumed at the header base):** `bindRows` derives the accessors (file closures built inside it, closing over the bound Set). Transition: `keyAccessors = accessorsOver(KeyRegistry, handWritten)`. Keep each row's current "stated" predicate — no behaviour change (owner call).
**Regression guard.** Same file-closure rule as item 16's guard binds item 15's bool rows: the derived file closure is an unvalidated typed copy under the row's stated predicate, never the bound Set (only the five Set-landing rows named in item 16 land through Set). Item 15 also owns present.command-on-model-documents, whose on-disk type is a plain bool (not *bool): its descriptor covers that on-disk shape. The row set is the `KindBool` rows except `context-files.enable` (no field, no Set). `bypass` reads APOGEE_BYPASS and --bypass, so its `keyAccessors` entry keeps the hand-written `fromEnv`/`fromFlag` until item 16 derives them. The binder at base is `bindSetters` (registry.go:1387): "`bindRows`" means `bindSetters`, renamed. `ui.*` rows' Set stays `landUI` through `UIPrefs.Set`: the item yields to ADR 0043's 2026-09-20 amendment (no third parser).
**Regression guard (re-check).** The init panic fires only for a field row whose hand-written `keyAccessors` entry carries a `fromFile`; a field row may keep a hand-written `fromEnv`/`fromFlag` (`bypass`, config.go:926-936, until item 16). The derived Set of `present.auto-open` and `present.command-on-model-documents` keeps `landIn(presentOf, PresentSettings.Validate, …)` (registry.go:660,679), as `ui.*` keeps `landUI` — the item yields to ADR 0043's 2026-09-16 amendment (a block-mapped key re-runs the block's validator). `internal/config/doc.go`'s file map gains a line naming `keyfield.go` (`TestDocMapNamesEveryFile`).
**Files:** internal/config/keyfield.go, internal/config/keyfield_test.go, internal/config/registry.go, internal/config/config.go, internal/config/config_test.go, internal/config/registry_test.go, internal/config/doc.go
**Read first:** internal/config/config.go — keyAccessor, keyAccessors, applyFile, applyEnv, setThroughRow, fileUI, filePresent, uiConfig.toUIPrefs; internal/config/registry.go — Key, bindSetters, admit, land, landIn, landUI;
internal/config/config_test.go — TestKeyAccessorsBindDescribedKeys; internal/config/registry_test.go — TestUIRowsLandThroughUIPrefsSet, TestRegistrySetIsTheInverseOfRead, TestRegistryIsBijectionWithFileConfig;
internal/config/doc.go — file map; internal/domain/uiprefs.go — UIPrefs.Set, UIPrefs.Validate
**Tests:** new keyfield tests (a bool block row's file value is a typed copy: `LoadFileConfig` still accepts `ui: {spinner: twirl, spinner-color: false}`; a field row with a hand-written `fromFile` panics at init while `bypass`'s `fromEnv`/`fromFlag` carry does not; with `Present.Port=70000`, `present.auto-open`'s Set("false") still refuses); `TestDocMapNamesEveryFile`, `TestKeyAccessorsBindDescribedKeys`, `TestEveryConfigKeyReachesTheOptions`, `TestRegistrySetIsTheInverseOfRead`, `TestRegistryDefaultsReadBackFromAnEmptyFile`, `TestRegistryIsBijectionWithFileConfig`.
**Acceptance:**
- `go test -race -count=1 -run 'Registry|KeyAccessors|EveryConfigKey|ApplyConfig|Bypass|ContextFillNotice|Field|DocMap' ./internal/config/`
**Commit:** `refactor(config): a key's typed field derives its accessors, starting with the bool rows`

## 16. Scalar rows, env and flag derive from the descriptor

**What:**
**Goal:** string, int, float and duration rows carry a `field`; env sources set through the bound Set; the flag copy is an unvalidated typed copy; `setThroughRow` is gone; the env error lead `apogee: invalid APOGEE_X %q:` and `--mode`'s refusal timing and wording are unchanged. Depends on item 15.
**Approach (assumed at the header base):** each scalar row keeps its own "stated" predicate (delegate-max-steps/fanout-rounds/max-tokens fall back on negatives; delegate-max-depth ≤ 0 → default; cursor-shape "" when absent). The `everyKeyFileConfig` fixture still covers every row.
**Regression guard** (keeps the owner's "no behaviour change" call): a row's derived file closure lands an UNVALIDATED typed copy under the row's own "stated" predicate — it never goes through the bound Set, except for the five rows whose file pass lands through Set at base (sub-agents-choice, delegate-timeout, stream-idle-timeout, re-stream-budget, cursor-shape). So `mode: fast` in the file with `--mode plan` or APOGEE_MODE=plan still starts; web-search-endpoint and ui.color-scheme gain no file-pass refusal (ADR 0040 call 8 keeps the scheme load forgiving); the present.port, ui.spinner and sessions.* refusals stay in ResolveOptions, not LoadFileConfig. Only the APOGEE_* env pass lands through row.Set (ADR 0043 2026-09-16 amendment). New tests: file `mode: fast` + --mode plan resolves; LoadFileConfig accepts a bad ui.spinner and a bad present.port as today.
**Files:** internal/config/keyfield.go, internal/config/keyfield_test.go, internal/config/registry.go, internal/config/config.go, internal/config/config_test.go, internal/config/registry_test.go
**Read first:** internal/config/config.go — keyAccessors, setThroughRow, applyEnv, sansPrefix, applyFlags, ResolveOptions, LoadFileConfig; internal/config/registry.go — validateSettingMode, validateSearchEndpoint, validateColorSchemeName, validatePresentPort, parseSessionsMaxAge;
cmd/apogee/wire.go — runRootWith; cmd/apogee/probeconfig.go — configReport; cmd/apogee/docs_env_test.go — TestDocsEnvBadValuesNameTheVariableAndTheValue;
internal/config/config_test.go — resolveSources, everyKeyFileConfig, TestApplyConfigSessionsRetention
**Tests:** existing config tests; `TestDocsEnv*` in cmd/apogee; new: file `mode: fast` + `--mode plan` resolves; `LoadFileConfig` accepts a bad `ui.spinner` and a bad `present.port` as today.
**Acceptance:**
- `! grep -n 'setThroughRow' internal/config/*.go`
- `go test -race -count=1 -run 'Registry|KeyAccessors|EveryConfigKey|ApplyConfig|Env|Flag|Override|Mode|Delegate|CursorShape|SubAgents|LoadFileConfig' ./internal/config/`; `go test -race -count=1 -run 'DocsEnv' ./cmd/apogee/`
**Commit:** `refactor(config): scalar keys, env and flag sources derive from the key's field`

## 17. List rows move onto the registry and keyAccessor retires

**What:**
**Goal:** `internal/config` has no `keyAccessor`/`keyAccessors`; hand-written rows (servers, sub-agents-server, system-prompt ×4, context-files ×2, unconfined-hosts, mcp-servers, reactions, model-profiles) carry their `fromFile` on the row; `applyFile`, `applyEnv`, `applyFlags` and `overrideSources` range over `KeyRegistry` with the first-refusal order unchanged; ADR 0035 and ADR 0043 carry dated amendments. Depends on item 16.
**Approach (assumed at the header base):** list rows get `field`s; fold `TestKeyAccessorsBindDescribedKeys` into `TestRegistryRowInvariants`. The bijection and Set/Read inverse tests stay (ADR 0035 D4).
**Regression guard.** `config_test.go` ranges `keyAccessors` twice (the multi-source fence loop and `TestKeyAccessorsBindDescribedKeys`): both loops move onto `KeyRegistry` rows when the test folds into `TestRegistryRowInvariants`. Amend by rule: every live doc naming `keyAccessors` or `setThroughRow` as today's table (`grep -rln 'keyAccessor\|setThroughRow' docs/adr docs/design docs/manual CONTEXT.md` — ADR 0036:131 besides 0035/0043) gets a dated amendment; plans, reviews and CHANGELOG mentions stay as history.
**Files:** internal/config/keyfield.go, internal/config/registry.go, internal/config/config.go, internal/config/config_test.go, internal/config/registry_test.go, docs/adr/0035-*.md, docs/adr/0036-*.md, docs/adr/0043-*.md
**Read first:** internal/config/config.go — keyAccessors, fileSystemPrompt, fileContextFiles, applyFile, applyEnv, applyFlags, overrideSources; internal/config/reactions.go — projectReactions;
internal/config/config_test.go — TestKeyAccessorsBindDescribedKeys, resolveSources; internal/config/registry_test.go — TestRegistryRowInvariants, TestRegistryIsBijectionWithFileConfig; internal/config/unknownkeys_test.go — applyFile caller;
docs/adr/0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md — keyAccessors note
**Tests:** whole config package.
**Acceptance:**
- `! grep -rn 'keyAccessor' internal/config`
- `go test -race -count=1 ./internal/config/`
**Commit:** `refactor(config): every key's file semantics live on its registry row`

## 18. Settings applies mirror through Key.Copy

**What:**
**Goal:** `config.Key` exports `Copy(dst, src *Options)`; the pure holder-mirror `applyX` functions in `cmd/apogee/wire_settings.go` are one `applyMirror`; `floorGuardFields` derives from the registry rows. Depends on item 16.
**Approach (assumed at the header base):** `settingsTable` stays (config cannot import the seams, ADR 0031/0043). Do not turn `floorGuardFields` into a keyed FloorConfig (denied, ADR 0076 D11).
**Regression guard.** `Copy` on a block row copies only its own sub-field, never the block: `applyInspector` mirrors `UI.Inspector` alone, and `landSetting`'s zero-based `UI` would otherwise overwrite the holder's whole block. `setFloorGuard(key, on bool)` lands through `row.Copy` from the landed Options (e.g. `setFloorGuard(key, landed)`), and `TestFloorGuardTableMatchesTheConfigKeys` builds its positive Options with `row.Set("true")` rather than a `*bool` handed back by `floorGuardFields`.
**Files:** internal/config/keyfield.go, internal/config/keyfield_test.go, internal/config/registry.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_settings_test.go
**Read first:** cmd/apogee/wire_settings.go — applyInspector, applyDelegateMaxSteps, applyUndoSnapshots, applyServerStats, applyFloorGuard, floorGuardFields, liveSettings.setFloorGuard, landSetting;
cmd/apogee/wire_settings_test.go — TestFloorGuardTableMatchesTheConfigKeys; internal/config/reactions.go — FloorGuardKeys
**Tests:** new `Copy` test in `internal/config/keyfield_test.go` pinning that sibling `ui.*`, `present.*` and `sessions.*` fields stay untouched; existing settings-apply tests.
**Acceptance:**
- `go test -race -count=1 -run 'Apply|LiveSettings|Setting|FloorGuard' ./cmd/apogee/`; `go test -race -count=1 -run 'Registry|Copy' ./internal/config/`
**Commit:** `refactor(cmd/apogee): pure setting applies mirror through Key.Copy`

## 19. One selection protocol for the prompt and settings fields

**What:**
**Goal:** the selection type is named `fieldSel` and owns `seat`, `extend`, `span()`, `nonEmpty()` and `taken(value)`; `handleMouseRelease` copies through one helper for both surfaces; behaviour unchanged.
**Approach (assumed at the header base):** rename `promptSel`→`fieldSel` (production and test literals); the prompt keeps its textarea-walk caret (ADR 0030 §6) and does not join a geometry interface. `writeSystemClipboard` is a package-var seam swapped only by serial tests.
**Regression guard.** Rename by rule, not list: every `promptSel` in `internal/tui/*.go`, comments included — also `ask.go`, `prompteditor.go`, `inputaccent_test.go` and `settings_test.go`. The Goal's method set (`seat`, `extend`, `span`, `nonEmpty`, `taken`) is checked by the Acceptance, so a rename-only change does not pass.
**Files:** internal/tui/mouse.go, internal/tui/model.go, internal/tui/lineeditor.go, internal/tui/settings.go, internal/tui/ask.go, internal/tui/prompteditor.go, internal/tui/mouse_test.go, internal/tui/inputaccent_test.go, internal/tui/settings_test.go
**Read first:** internal/tui/mouse.go — promptSel, handleMouseRelease, copyFlash, highlightInput; internal/tui/model.go — handleKey (sel stash + selection-delete branch); internal/tui/lineeditor.go — deleteSelection, caretToRune;
internal/tui/settings.go — settingsPane.sel, settingsKey, settingsFieldMsg; internal/tui/prompteditor.go — promptEditor.sel; internal/tui/ask.go;
internal/tui/mouse_test.go — TestSelectionDeleteKeys, TestSettingsDragSelectsAndCopies; internal/tui/settings_test.go — TestSettingsPasteDropsTheFieldSelection
**Tests:** existing selection and drag tests.
**Acceptance:**
- `! grep -n 'promptSel' internal/tui/*.go`
- `test "$(cat internal/tui/*.go | grep -cE 'func \([a-z]+ \*?fieldSel\) (seat|extend|span|nonEmpty|taken)\(')" -eq 5`
- `go test -race -count=1 -run 'TestSelectionText|TestDragSelectsAndCopies|TestBareClickReleaseDoesNotCopy|TestSelectionDelete|TestButtonlessMotionIsTheRelease|TestSettingsDragSelectsAndCopies|TestSettingsTextDragSelectsAcrossLines|TestPromptAndTranscriptSelectionsAreExclusive' ./internal/tui/`
**Commit:** `refactor(tui): one selection protocol serves the prompt and the settings fields`

## 20. One pointer geometry for the settings fields

**What:**
**Goal:** the settings value row and multi-line field share one paint geometry, one caret-at, one click branch, one motion branch and one `highlightSettingsField`; the caret-glyph offset shift lives once on `lineEditor`; none of `settingsCaretAt`, `settingsEditCells`, `highlightSettingsEdit`, `handleSettingsTextClick`, `handleSettingsTextMotion` remains; a table test proves the shaded cells are the cells a click places the caret in. Depends on item 19.
**Approach (assumed at the header base):** `settingsFieldPaint(place, origin)` returns painted sub-rows (y, start offset, runes) plus text x. Click reads the pre-click frame, motion the live model; highlight works in pane coordinates, pointer in screen; a scrolled-out value row emits no sub-row. Claim semantics unchanged (off-row click in the value buffer swallowed; multi-line chrome click unclaimed; off-field motion claimed).
**Regression guard.** The Acceptance checks the whole Goal: the multi-line twins `highlightSettingsText` and `settingsTextCaretAt` are gone too, across `internal/tui/*.go`; `highlightSettingsField` exists; and the caret-glyph offset shift is one method on `lineEditor`, called by both fields.
**Files:** internal/tui/mouse.go, internal/tui/settings.go, internal/tui/lineeditor.go, internal/tui/lineeditor_test.go, internal/tui/doc.go, internal/tui/mouse_test.go
**Read first:** internal/tui/mouse.go — handleSettingsClick, handleSettingsMotion, settingsCaretAt, settingsEditCells, settingsTextPaint, settingsTextGeometry, settingsTextCaretAt, highlightSettingsEdit, highlightSettingsText, settingsValueX;
internal/tui/settings.go — renderSettings, renderSettingsText, settingsEditText, settingsTextLines, settingsTextSpec; internal/tui/lineeditor.go — textWithCaret, caretRune; internal/tui/doc.go — mouse.go paragraph ([Model.settingsTextPaint]);
internal/tui/mouse_test.go — TestSettingsTextClickSeatsTheCaretInTheProse, TestTranscriptDragOutlivesASettingsHighlight, settingsTextEditModel; internal/tui/settings_test.go — settingsEditModel, settingsStringRow
**Tests:** new table over {value row, multi-line} × {ascii, double-width, span before/after caret}.
**Acceptance:**
- `! grep -nE 'func \(m Model\) (settingsCaretAt|settingsEditCells|highlightSettingsEdit|highlightSettingsText|settingsTextCaretAt|handleSettingsTextClick|handleSettingsTextMotion)\(' internal/tui/*.go`
- `grep -n 'func (m Model) highlightSettingsField' internal/tui/*.go`
- `go test -race -count=1 -run 'TestSettingsClick|TestSettingsDrag|TestSettingsText|TestSettingsEntryDropsTheHighlight|TestTranscriptDragOutlivesASettingsHighlight|TestSettingsPaneValueFieldEditsAtTheCaret|TestSettingsPaneTextEditorPaintsTheCaret|TestSettingsWheel|Field' ./internal/tui/`
**Commit:** `refactor(tui): the settings fields share one pointer geometry`

## 21. Backspace and Delete remove a settings-field selection

**What:**
**Goal:** in a `/settings` value or multi-line field with a non-empty selection, Backspace and Delete remove the selected text (as in the prompt box), leaving the caret at the selection start; with no selection they behave as today; `docs/manual/commands.md`'s claim that the field selects "exactly as … in the prompt box" holds. Depends on item 20.
**Approach (assumed at the header base):** mirror the prompt's selection-delete branch (in the model's key handling) in the settings field's key path, through `fieldSel.taken`.
**Regression guard.** `settingsKey` zeroes `m.settings.sel` at its chokepoint before `step.key` runs, so it stashes the span first (as `handleKey` does, model.go:1597-1598) and runs the Backspace/Delete branch there, for `settingsValueBuffer` and `settingsTextEditor`, ahead of `step.key`; the step-table signature is unchanged. `docs/layout/settings-screen-layout.md:136` ("drags a selection exactly as in the prompt box") gains the same Backspace/Delete sentence as commands.md.
**Files:** internal/tui/settings.go, internal/tui/settings_test.go, docs/manual/commands.md, docs/layout/settings-screen-layout.md
**Read first:** internal/tui/settings.go — settingsKey, settingsSteps, settingsEditKey, settingsBufferKey, settingsTextKey; internal/tui/model.go — handleKey selection-delete branch; internal/tui/lineeditor.go — deleteSelection;
internal/tui/mouse_test.go — TestSelectionDeleteKeys, TestSettingsDragSelectsAndCopies; internal/tui/settings_test.go — settingsEditModel, settingsStringRow, settingsTextRow;
docs/manual/commands.md — /settings "A buffer is a real field"
**Tests:** new: select a span in each settings field kind, Backspace → text removed; Delete → same; no selection → one rune removed.
**Acceptance:**
- `go test -race -count=1 -run 'TestSettings.*Select|TestSelectionDelete' ./internal/tui/`
**Commit:** `feat(tui): Backspace and Delete remove a selection in a settings field`

## 22. session.Live owns session identity

**What:** Recast at the regression check (2026-09-24).
**Goal:** `internal/session/live.go` defines `Live` with `NewLive(store, now, resumed, onMove...)`, `Begin`, `Rotate`, `Park`, `Activate`, `DropParked`, `ID`, `ActiveID`, `Close`, unit-tested on its own; `internal/session` still imports no snapshot, undo or tui package.
**Approach (assumed at the header base):** port the identity rules of `cmd/apogee` `sessionHost` (mint, rotate, hold, parked hold, `holdsLocked`'s own-lock check) intact. Followers are `onMove` callbacks run outside Live's mutex. No hold at mint (a run that never saves touches no disk, ADR 0022). A swallowed hold refusal still moves followers (owner call; `apogee-refused-hold-followers-move`). No `resolveResume` or sweeps (ADR 0083 §5).
**Regression guard.** session.Live owns the whole session identity, not only id plus hold: id, title, createdAt and parentID. `Begin` returns that identity; `Retitle(id, title)` renames the active record in place (TestSessionHostRenameActiveSticks keeps passing through it); `Rotate` clears parentID (TestRotateClearsParentID); Save reads the identity from Live. Item 23's goal then also covers: sessionHost holds no title/createdAt/parentID state of its own. `NewLive` and `Begin` run no `onMove`; only `Rotate` and `Activate` do (construction pushes nothing — wire_session_test.go:774, 1052-1056 — and boot seeding stays with `SessionScratchDir` plus `openSessionJournal`).
**Files:** internal/session/live.go, internal/session/live_test.go, internal/session/doc.go
**Read first:** cmd/apogee/wire_session.go — sessionHost, activeSession, newSessionHost, holdLocked, holdsLocked, releasePendingLocked, Save, Rotate, Load, Activate, Delete, Rename, Fork, SessionID;
internal/session/store.go — Store.Hold, NewID, HeldError; internal/session/doc.go — "A session has one live instance" paragraph;
cmd/apogee/wire_session_test.go — assertHeld, TestSessionHostScratchFollowsTheActiveSession, TestSessionHostJournalFollowsTheActiveSession, TestRotateClearsParentID, TestSessionHostRenameActiveSticks
**Tests:** new `TestLive*` covering mint, rotate (clears parentID), park/activate, drop, close, own-lock, `Retitle`, and that `NewLive`/`Begin` fire no `onMove`.
**Acceptance:**
- `go test -race -count=1 -run 'TestLive' ./internal/session/`
- `! go list -deps ./internal/session | grep -E 'internal/(snapshot|undo|tui)$'`
**Commit:** `feat(session): Live owns a session's identity, rotation and hold`

## 23. sessionHost runs on session.Live

**What:**
**Goal:** `cmd/apogee`'s `sessionHost` delegates identity to `session.Live`; it holds no `pendingRelease` or `heldID` state of its own; `tui.SessionHost` is unchanged; `followScratch`/`followJournal` are `onMove` callbacks. Depends on item 22 and item 7 (shared file).
**Approach (assumed at the header base):** `SessionID` is read at boot before any Save (`openSessionJournal`) — keep that order. Wire the followers where `wire_live.go` builds the host.
**Regression guard.** Per item 22's decision, `sessionHost` also holds no title/createdAt/parentID state of its own. `TestSessionHostLoadParksTheHoldForActivate` stops reading `host.heldID`/`pendingID`/`pendingRelease`: it observes adoption from behaviour (e.g. after `Activate(first)`, `host.Delete(first)` is refused because this process holds it live) or through a read-only accessor on item 22's `Live`. `sessionHost` keeps its own `now` for Save's stamps (or `saveAt` passes the clock in), so `saveAt`'s callers still stamp their fixed time.
**Files:** cmd/apogee/wire_session.go, cmd/apogee/wire_live.go, cmd/apogee/wire_session_test.go
**Read first:** cmd/apogee/wire_session.go — sessionHost, newSessionHost, Save, Rotate, Load, Activate, Delete, Rename, Fork, SessionScratchDir, SessionID, followScratch, followJournal;
cmd/apogee/wire_live.go — newSessionHost call site, openSessionJournal; cmd/apogee/wire.go — rootWiring.host, Close;
cmd/apogee/wire_session_test.go — saveAt, assertHeld, TestSessionHostLoadParksTheHoldForActivate, TestSessionHostScratchFollowsTheActiveSession, TestSessionHostJournalFollowsTheActiveSession; cmd/apogee/headless_test.go — TestHeadlessNoSaveStillAppliesTheRetentionPolicy
**Tests:** existing `wire_session_test.go`, with `TestSessionHostLoadParksTheHoldForActivate` rewritten to observe adoption from behaviour; `saveAt`'s callers (`TestGCSessionsAppliesTheConfiguredPolicy`, `TestWireSessionSweepsAfterResolvingContinue`, `TestGCSnapshotDirsSweepsWhatNothingCanReach`, `TestHeadlessNoSaveStillAppliesTheRetentionPolicy`) stay green.
**Acceptance:**
- `! grep -n 'pendingRelease\|heldID' cmd/apogee/wire_session.go`
- `go test -race -count=1 -run 'TestSessionHost|TestResolveResume|TestResolveContinue|TestForkStamps|TestActivateCarries|TestResumeCarries|TestRotateClears|TestGCSessions|TestWireSessionSweeps|TestGCSnapshotDirs|TestHeadlessNoSaveStillAppliesTheRetentionPolicy|TestBuildAgentResume' ./cmd/apogee/`
- `grep -n 'session.NewLive(' cmd/apogee/wire_session.go cmd/apogee/wire_live.go`
- `diff <(git show 78430eb7:internal/tui/tui.go | sed -n '/^type SessionHost interface/,/^}/p') <(sed -n '/^type SessionHost interface/,/^}/p' internal/tui/tui.go)`
**Commit:** `refactor(cmd/apogee): the session host runs on session.Live`
