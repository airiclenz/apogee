# Session-log wrapping and tool-grouping gaps — plan

**Goal:** Session-log notes read as one paragraph each, wrapped only at the window width. `git_status`, a no-argument `run_tests` and every unregistered (MCP) tool call get a target, so they group and count in the Tools umbrella like any other call.
**Date:** 2026-11-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 53db5d85

**Regression check (2026-11-12, 53db5d85):**
- 1: guard folded
- 2: guard folded; writer decision applied (`Notice.Log` read only from `Options.StartupNotices`); supersedes `adr-0112-amend-1112` §1 "verbatim" through a dated amendment
- 3: guard folded
- 5: guard folded; supersedes in part `layout.md:876-879` (the target row is shortened; the body stays quoted)
- 6: guard folded (no decode change); supersedes `layout.md:826-827` (an unknown tool's label is its raw name)

**Sources:**
- `.capcom/issues/session-log-hard-breaks.md`, `.capcom/issues/git-status-never-groups.md`, `.capcom/issues/run-tests-never-groups.md`, `.capcom/issues/mcp-tools-never-group.md`
- `layout.md` (start-up box / session log; "What stays standalone"), `docs/layout/tool-layout.md`
- ADR 0112 (residual disclosure; amended 2026-11-12: a residual inside the session log counts as disclosed)

**Ratified design calls (owner, 2026-11-12):**
- **Residual note shape:** one session-log note per residual: `<backend> cannot fence <token> — <consequence>`.
- **Prefix:** session-log notes drop the `apogee: ` prefix; stderr keeps its text byte-identical.
- **Hard breaks:** no session-log note carries a hard line break; any `\n` (with the indent around it) folds to one space at the sink.
- **git_status target:** the fixed words `working tree`.
- **run_tests target:** `whole project` when no `path`; still qualified `· <filter>` when a filter is given.
- **Unregistered tool target:** the first string argument's first line, in the order `argumentDetails` lists arguments, bounded; no string argument → the tool's raw name.
- **MCP label:** a name holding `__` shows as `<server> · <tool>` (split at the first `__`); the raw name stays the key (`toolView.name`).

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 (`charm.land/bubbletea/v2`); `make test` is the suite runner, per-package `go test -race -count=1 -run … ./internal/tui/` for targeted runs.
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Recorded sessions: a stored empty Target (`session.ToolView.Target`) replays as stored; no re-derivation on decode.
- A malformed call missing a required argument stays targetless (`TestRenderGroupBreakers` "a call with no target").
- The side-panel session log (`session-log-side-panel`).
- `probe.DegradedNotice` and every other loud notice: they keep their own lines.

## 1. Session-log notes fold hard breaks into one paragraph — ✅ DONE (2026-11-12)

NOTES (2026-11-12): added a separate test `TestSessionLogFoldsTheResidualNotice` for the plan's "Add a case" (the residual text exactly as `probe.ResidualNotice` emits it) rather than a row inside the recast paint test; both drive the sink through a shared `storedSessionLogNotes` helper that asserts no stored note holds a `\n`.

**What:**
**Goal:** every note stored in the session log is a single line of text with no `\n`; a note given with embedded breaks and indents reaches the log with each break-plus-surrounding-whitespace replaced by one space, and the expanded log paints it through `hangingWrap` only.
**Approach (assumed at the header base):** in `transcript.addSessionLogNote` (`internal/tui/transcript.go`), after the escape strip, fold whitespace runs that contain a newline into one space and trim the ends. One small unexported helper in `internal/tui`; no new package. Update the `renderSessionLog` doc comment (`internal/tui/render.go`) — it states a note of several lines keeps them. Update the prose and sketch in `layout.md` ("The session log beneath it (2026-11-12)") and `docs/layout/tool-layout.md` (the third header-alone fold) that say a note of several lines keeps its lines: a note is one paragraph, soft-wrapped and hung at its text column.
**Regression guard.** The fold lives at the sink, so the recast test drives it: each note goes through `tr.openSessionLog(); tr.addSessionLogNote(…)` and the paint reads `tr.entries[tr.sessionLogIndex()].notes` (a hand-built `paintInput{notes: …}` never reaches the fold); it asserts the stored note holds no `"\n"` before it checks the narrow-width rows. The doc sweep is a rule, not a list: every prose that says a session-log note keeps its lines, found by `grep -rn "several lines\|second line\|its lines" layout.md docs/layout internal/tui/*.go` (the test comment at `sessionlog_test.go:147` is one).
**Files:** internal/tui/transcript.go, internal/tui/render.go, internal/tui/sessionlog_test.go, layout.md, docs/layout/tool-layout.md
**Read first:** internal/tui/transcript.go — transcript.addSessionLogNote, transcript.openSessionLog, transcript.sessionLogIndex; internal/tui/render.go — renderSessionLog; internal/tui/wrap.go — hangingWrap;
internal/tui/sessionlog_test.go — TestSessionLogPaintsHeaderAloneThenEveryNote; layout.md — "The session log beneath it (2026-11-12)"; docs/layout/tool-layout.md — session log header-alone bullet
**Tests:** recast `TestSessionLogPaintsHeaderAloneThenEveryNote` (`internal/tui/sessionlog_test.go`) to send each note through `openSessionLog` / `addSessionLogNote` and paint the stored notes: the stored note holds no `\n`, and a note `"head:\n  a — b\n  c — d"` paints as one paragraph; at a narrow width every continuation row starts at the note's text column, none deeper. Add a case: the residual text exactly as `probe.ResidualNotice` emits it (header + two `"  token — consequence"` lines) folds to one line.
**Acceptance:**
- `go test -race -count=1 -run 'TestSessionLog' ./internal/tui/`
- `go vet ./internal/tui/`
**Commit:** `fix(tui): fold hard line breaks in session-log notes into one paragraph`

## 2. Residual notice gets its own session-log wording, one note per residual — ✅ DONE (2026-11-12)

NOTES (2026-11-12): the shared gate is a new unexported `probe.disclosedResiduals` (Auto ∧ confine ∧ FSWrite, then `writeClassResiduals`), read by both `ResidualNotice` and `ResidualNotes`; `tui.Notice` gains an unexported `transcriptNotes()` method that `noteStartupNotices` reads, so the Log-or-Text choice lives in one place.

NOTES (2026-11-12): the ADR 0112 amendment is a dated `> **Amended 2026-11-12 (…)**` blockquote under §1 of `adr-0112-amend-1112` (the plan's Files list names no new child Note), named on `adr-0112.md`'s Status line.

NOTES (2026-11-12): the `TestRunRootConfinementStartupNotices` Log assertion lives in a new helper `assertResidualLogWording` in `wire_boot_test.go`, which also checks that stderr carries `probe.ResidualNotice`'s text byte for byte.

**What:** fixes the defect in `session-log-hard-breaks`: the start-up residual notice reached the session log as its multi-line stderr text.
**Goal:** on an Auto + confine start with write-class residuals, the session log holds one note per residual, worded `<backend> cannot fence <token> — <consequence>` with no `apogee: ` prefix; stderr still prints `probe.ResidualNotice`'s text byte-identical.
**Approach (assumed at the header base):** add `probe.ResidualNotes(backendName, caps, mode, confineToWorkspace) []string` beside `ResidualNotice` in `internal/probe/confinement.go`, with the same gate and the same `writeClassResiduals` / `residualConsequence` (shared, never restated). Give `tui.Notice` (`internal/tui/tui.go`) an optional `Log []string`: the transcript wording, one session-log note each; empty means "use Text". `announce` (`cmd/apogee/wire_boot.go`) keeps writing `Text` to stderr; `Model.noteStartupNotices` (`internal/tui/model.go`) adds each `Log` entry as its own routine note (or its own ephemeral note when Loud) and falls back to `Text` when `Log` is empty. The residual call site in wire_boot passes both. headless.go and daemon.go are untouched (stderr only).
**Regression guard.** `Log []string` makes `tui.Notice` non-comparable: rewrite the `slices.Contains(result.Notices, tui.Notice{Text: …})` check in `TestRebindRecomposesTheToolSetUnderTheProfileRoster` (`cmd/apogee/wire_server_test.go:91`) as `slices.ContainsFunc(result.Notices, func(n tui.Notice) bool { return n.Text == "…" })`. Reword every prose saying the TUI repeats a startup notice verbatim / word for word, found by `grep -rn "verbatim" cmd/apogee/wire*.go internal/tui/tui.go internal/tui/model.go docs/manual/configuration.md` (`configuration.md` "Auto mode's blast radius", `wire_boot.go` announce, `tui.go` Notice, `model.go` noteStartupNotices, `wire.go`). Name the new model test `TestStartupNotices…` and put the `Log` assertion inside `TestRunRootConfinementStartupNotices` (`wire_boot_test.go:482`), its prefix built from `probe.BackendName(fakeConfiner{})`, so the Acceptance runs both. This supersedes `adr-0112-amend-1112` §1 ("one keypress … from view, verbatim"): record a dated amendment to ADR 0112 naming the per-residual notes, named on the record's Status line. tui.Notice is also the element of RebindResult.Notices (read in internal/tui/heartbeat.go); the new Notice.Log field is read only from Options.StartupNotices, and its doc comment says so — a rebind Notice that sets Log has it ignored, and no rebind producer sets it
**Files:** internal/probe/confinement.go, internal/probe/confinement_test.go, internal/tui/tui.go, internal/tui/model.go, internal/tui/model_test.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_boot_test.go, cmd/apogee/wire_server_test.go, cmd/apogee/wire.go, docs/manual/configuration.md, .capcom/knowledge/decisions/adr-0112.md, .capcom/knowledge/decisions/adr-0112/adr-0112-amend-1112.md
**Read first:** cmd/apogee/wire_boot.go — rootWiring.announceConfinement, rootWiring.announce; internal/probe/confinement.go — ResidualNotice, writeClassResiduals, residualConsequence;
internal/tui/model.go — Model.noteStartupNotices; internal/tui/tui.go — Notice; cmd/apogee/wire_boot_test.go — TestRunRootConfinementStartupNotices
**Tests:** table test `TestResidualNotes` (both tokens, truncate(2), unknown token, network-only → nil, non-Auto → nil); existing `TestResidualNotice*` unchanged and passing. A model test `TestStartupNotices…`: a routine Notice with `Text` multi-line and `Log` of two entries yields a session log of exactly those two notes. Inside `TestRunRootConfinementStartupNotices`: the announced residual Notice carries `Log` entries starting `probe.BackendName(fakeConfiner{}) + " cannot fence "` while stderr matches `ResidualNotice`. `TestRebindRecomposesTheToolSetUnderTheProfileRoster` compiles and passes with the `ContainsFunc` check.
**Acceptance:**
- `go test -race -count=1 -run 'TestResidualNotice|TestResidualNotes' ./internal/probe/`
- `go test -race -count=1 -run 'TestStartupNotices|TestSessionLog' ./internal/tui/`
- `go test -race -count=1 -run 'TestRunRootConfinementStartupNotices|TestHeadlessAutoDisclosesTheFenceResidual|TestDaemonDisclosesTheFenceResidualAtStartup' ./cmd/apogee/`
**Closes:** session-log-hard-breaks
**Commit:** `fix(notices): word the fence residuals as one session-log note each`

## 3. git_status targets the working tree and groups — ✅ DONE (2026-11-12)

NOTES (2026-11-12): the target is a named extractor `workingTreeTarget` returning the constant `workingTreeTargetText` ("working tree"), placed with the other extractor helpers in toolregistry.go.
NOTES (2026-11-12): TestTargetlessBlockStillCapsAndToggles now uses a git_diff_range call sent without its refs (a registered call with no target) and asserts the empty Target up front; the new driven test is TestGitStatusGroupsAndCounts (toolblock_test.go), pinning the umbrella collapsed, with git_status' type row open, and with its member open.
NOTES (2026-11-12): TestToolStat and toolsummary_pin_test.go asserted nothing of the targetless shape, so neither changed (toolsummary_pin_test.go stays unedited); TestGitStatusReportSurvivesItsTypedSummary gained a Target == "working tree" assertion.
NOTES (2026-11-12): tool-layout.md gains a dated 2026-11-12 note under the table beside the row change, in the shape of its earlier dated row notes.

**What:** fixes `git-status-never-groups`: `groupable` (`internal/tui/toolbranch.go`) requires a Target and `git_status` had none.
**Goal:** a `git_status` call's Target is `working tree`; a run of read_file, grep, git_status and git_log calls renders as one Tools umbrella of four member rows counting all four; an expanded git_status member still shows the branch and the staged / unstaged / untracked report.
**Approach (assumed at the header base):** set `target` on the `git_status` row of `toolRegistry` (`internal/tui/toolregistry.go`) to a constant extractor returning `working tree`; fix its comment and the registry comments that cite git_status as carrying no target. `stat` and `body` unchanged. Update the git_status row of the table in `docs/layout/tool-layout.md` (target column `working tree`).
**Regression guard.** Fix every comment or doc row that calls git_status targetless, not only the registry comments, found by `grep -rnE 'git_status|Git Status' internal/tui docs/layout layout.md` and reading each hit: `toolregistry.go:469-471` and `:565-566` wrap across lines, so a one-line grep misses them; `toolregistry_test.go:731` and `toolblock_test.go:807` are hits too.
**Files:** internal/tui/toolregistry.go, internal/tui/toolregistry_test.go, internal/tui/toolblock_test.go, internal/tui/toolpresent_test.go, internal/tui/toolsummary_pin_test.go, docs/layout/tool-layout.md
**Read first:** internal/tui/toolregistry.go — toolRegistry["git_status"], toolPresenter.target; internal/tui/toolbranch.go — groupable; internal/tui/transcript.go — sameLabelRun, toolSuperGroup;
internal/tui/toolblock_test.go — TestTargetlessBlockStillCapsAndToggles; internal/tui/toolpresent_test.go — TestGitStatusReportSurvivesItsTypedSummary, TestToolStat
**Tests:** `TestTargetlessBlockStillCapsAndToggles` moves to another targetless example (an orphan result or a registered call with no target) so the targetless shape stays covered. New driven test: the four-call run groups, umbrella count 4, git_status member collapsed and expanded. Re-pin `TestToolStat` / `TestGitStatusReportSurvivesItsTypedSummary` where they assert the targetless shape.
**Acceptance:**
- `go test -race -count=1 -run 'GitStatus|Targetless|TestToolStat|TestToolSummaryPin|Group' ./internal/tui/`
**Closes:** git-status-never-groups
**Commit:** `fix(tui): give git_status a working-tree target so it groups`

## 4. run_tests with no path targets the whole project

**What:** fixes `run-tests-never-groups`: `runTestsTarget` returns `""` with neither `path` nor `filter`. Depends on item 3 (same file).
**Goal:** `runTestsTarget` returns `whole project` with no args, `whole project · <filter>` with only a filter, `<path>` with only a path, `<path> · <filter>` with both; a no-argument run_tests groups and counts in a Tools umbrella.
**Approach (assumed at the header base):** in `runTestsTarget` (`internal/tui/toolregistry.go`) default the path half to `whole project` before `qualifiedTarget`. Fix the registry comment that says it renders an empty target for a bare "running tests". Check `toolActivityVerb` / the live status line for any rule keyed on run_tests' empty target, and keep its wording. Update the run_tests row in `docs/layout/tool-layout.md`.
**Files:** internal/tui/toolregistry.go, internal/tui/toolpresent_test.go, internal/tui/activity_test.go, docs/layout/tool-layout.md
**Read first:** internal/tui/toolregistry.go — runTestsTarget, qualifiedTarget, toolRegistry["run_tests"] comment; internal/tui/activity.go — toolActivityVerb (reads Verb only, so the status line never sees the target);
internal/tui/activity_test.go — TestToolActivityVerb; internal/tui/toolshape_test.go — TestRenderGroupBreakers; docs/layout/tool-layout.md — run_tests table row; cmd/sessionexpect/judge.go — entryContains (fixture-only, untouched by the presenter)
**Tests:** table test on `runTestsTarget` (four cases above); a group test with a no-arg run_tests member; `TestToolActivityVerb` re-pinned only if its run_tests case reads the target.
**Acceptance:**
- `go test -race -count=1 -run 'RunTests|TestToolActivityVerb|Group' ./internal/tui/`
- `go test -race -count=1 ./cmd/sessionexpect/`
**Closes:** run-tests-never-groups
**Commit:** `fix(tui): target a bare run_tests call at the whole project`

## 5. Unregistered tool calls get a target and group

**What:** fixes part of `mcp-tools-never-group`: `presentToolCall` (`internal/tui/toolview.go`) builds an unregistered tool's view with no Target.
**Goal:** an unregistered tool's call has Target = the first string argument's first line in `argumentDetails` order, bounded like other targets, or the raw tool name when it has no string argument; two such calls between a read_file and a grep render as one Tools umbrella of four member rows; expanding a member still lists every argument.
**Approach (assumed at the header base):** in the `!ok` branch of `presentToolCall`, set `Target` from the arguments via one helper that walks arguments in the same key order `argumentDetails` uses (share the ordering, never restate it). Keep `Details` as the argument list. Update `layout.md` prose that names an unregistered tool's labelled arguments as the targetless example ("What stays standalone", "A call with no target", the collapse-budget note on an MCP argument blob, "Hiding a targetless call's branches"): unregistered tools now group; the targetless examples left are a registered call missing its target and a stray `result`.
**Regression guard.** The target is the first string argument whose first line is non-blank; with none, the raw name. The target row is shortened like every target (`finishDisplay` → `shortenPaths`) while the body stays quoted verbatim; this supersedes in part `layout.md:876-879` ("an unregistered tool's argument values … quoted, never respelled"), reworded in the sweep, beside `layout.md:907-910` ("What stays standalone"). The prose sweep is a rule: every site naming an unregistered / unknown tool as targetless or as its arguments-as-branches, found by `grep -rn 'unregistered\|unknown tool\|raw-name\|raw name' layout.md docs/layout internal/tui/*.go` (`layout.md:873-880` included).
**Files:** internal/tui/toolview.go, internal/tui/toolshape_test.go, internal/tui/toolpresent_test.go, internal/tui/render_test.go, internal/tui/workspacepath_test.go, internal/tui/blocktarget_test.go, layout.md, internal/tui/toolregistry.go, internal/tui/toolbranch.go, internal/tui/blockstate.go, internal/tui/doc.go, internal/tui/toolargs.go, internal/tui/workspacepath.go
**Read first:** internal/tui/toolview.go — presentToolCall, finishDisplay, shortenPaths; internal/tui/toolargs.go — orderedArgs, argumentDetails; internal/tui/toolbranch.go — groupable;
internal/tui/toolpresent_test.go — TestPresentToolCall; internal/tui/workspacepath_test.go — TestUnregisteredToolCardQuotesItsArgumentsVerbatim
**Tests:** tests using `mcp_thing` / `mcp_search` as the targetless example (`TestRenderNoTargetStandalone`, `TestRenderNoTargetKeepsItsSummary`, `TestTargetlessBlocksCollapseToTheBudget`, `TestEveryToolShapeCollapsesInsideTheRowBudget`, `TestRenderMarksTheWholeBlock`) move to a registered call with no target. Re-pin both `TestPresentToolCall` cases that flip — "unknown tool" → target `frobnicate`, "malformed args" → target `weird` — and `TestRenderMarksTheWholeBlock`'s "a targetless block inside the cap marks nothing" (`weird_tool` `"go"` becomes a toggle target); re-pin `TestUnregisteredCallLabelsItsArguments`, `TestTranscriptLayoutGolden`, and `TestUnregisteredToolCardQuotesItsArgumentsVerbatim`, which also asserts the target row is shortened while the body stays verbatim. New: a driven group test with two MCP calls, a no-argument MCP call that still groups, and an empty first value (`{"cursor":"","query":"x"}` → target `x`).
**Acceptance:**
- `go test -race -count=1 -run 'Unregistered|NoTarget|Targetless|TestPresentToolCall|TestTranscriptLayoutGolden|TestRenderMarksTheWholeBlock|Group' ./internal/tui/`
**Commit:** `fix(tui): give unregistered tool calls a target so they group`

## 6. MCP tool label splits server and tool

**What:** completes `mcp-tools-never-group`. Depends on item 5.
**Goal:** an unregistered call whose raw name holds `__` shows the label `<server> · <tool>` (split at the first `__`); a name without `__` shows unchanged; `toolView.name` and every lookup keyed on it keep the raw name.
**Approach (assumed at the header base):** in the `!ok` branch of `presentToolCall`, derive `Label` from the raw name; `Verb` stays `running <raw name>`. The separator is a local constant in `internal/tui` with a comment naming `internal/mcp`'s `toolNameSeparator` (the TUI does not import internal/mcp). Check `fromWireToolView` (`internal/tui/transcriptbridge.go`): a replayed unregistered view gets the same label rule.
**Regression guard.** No decode change (in line with Out of scope "no re-derivation on decode"): a dynamic tool's record keeps the label it was written with — for a new record already the live `<server> · <tool>` — so `TestTranscriptCodecReDerivesARelabelledToolsLabel` stays green unchanged; only the `transcriptbridge.go:637-642` comment ("keeps … the raw name the live path gave it") is reworded to the label the live path gave it. Split only when both halves are non-empty, else keep the raw name (`__init__`, `x__` unchanged; a real alias never holds `__`, per `ServerConfig.ValidateName`). This supersedes `layout.md:826-827` (an unknown tool's label is its raw name): reword it so an MCP tool's label is `<server> · <tool>` and any other unregistered tool keeps its raw name.
**Files:** internal/tui/toolview.go, internal/tui/transcriptbridge.go, internal/tui/toolpresent_test.go, internal/tui/transcriptbridge_test.go, layout.md
**Read first:** internal/tui/toolview.go — presentToolCall; internal/tui/transcriptbridge.go — fromWireToolView, toWireToolView; internal/tui/transcriptbridge_test.go — TestTranscriptCodecReDerivesARelabelledToolsLabel, TestTranscriptCodecRoundTrip;
internal/mcp/tool.go — toolNameSeparator; internal/mcp/transport.go — ServerConfig.ValidateName; layout.md — tool-label paragraph
**Tests:** table test on the label rule (`github__search_issues` → `github · search_issues`, `plain_tool` → unchanged, `a__b__c` → `a · b__c`, `__init__` → unchanged, `x__` → unchanged); a codec round-trip asserts the replayed label matches the live one; `TestTranscriptCodecReDerivesARelabelledToolsLabel` stays green unchanged.
**Acceptance:**
- `go test -race -count=1 -run 'TestPresentToolCall|Unregistered|TestTranscriptCodec|McpLabel|MCPLabel' ./internal/tui/`
**Closes:** mcp-tools-never-group
**Commit:** `feat(tui): label MCP tool calls as server · tool`
