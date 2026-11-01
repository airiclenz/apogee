# view_diff orientation header and headless run budgets — plan

**Goal:** `view_diff` output names which side is the current file and which is the proposed `newContent`, and the TUI renders it unchanged apart from that header. `apogee headless` gains opt-in `--max-turns` / `--max-wall-time` budgets that stop the run at a Turn boundary, save the record and exit with a code of their own.
**Date:** 2026-11-01
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 92a57987

**Sources:**
- `.capcom/issues/viewdiff-orientation-header.md`, `.capcom/issues/headless-run-budgets.md`
- ADR 0075 (D5 one `run_finished` per exit, D10 additive growth in `v` 2, D14 goldens), ADR 0033 (CLI owns exit codes over `internal/run`), ADR 0031 (Driver-neutral engine), ADR 0007 (Step, quiescent boundary), ADR 0088 (settle on cancel)
- Glossary: `Turn`, `Step`, `Exchange`, `Step cap`

**Regression check (2026-11-01, 92a57987):**
- 1: guard folded (the `wantBody` table is `TestPresentToolCallOutcomeSplit`'s; the comment grep widened)
- 2: guard folded (decision: control-character path written `%q`; header on rendered diffs only; TUI mocks unchanged; standalone presenter test)
- 4: guard folded (decision: only headless reads the keys; widened Acceptance; template, config tests, negative flag, manual counts)
- 5: guard folded (Event-line Writer only, journal frame stays `{}`; member last)
- 6: guard folded (decision: budgets copied only on headless's own path; non-nil `exitError.err`; clock through `deps.runner`)

**Ratified design calls** (Airic Lenz, 2026-11-01):
- **view_diff header:** two lines, `--- current: <path>` then `+++ proposed: newContent`, where `<path>` is the `path` argument as passed. The over-budget one-liner names both sides too. "No changes detected" gets no header.
- **Budget safe point:** both budgets are checked only between Turns. A running Turn always finishes and nothing is rolled back; an outer supervisor's kill remains the backstop for one overlong Turn.
- **JSON member:** `run_finished` gains `budget_stop`: `null` on every other exit, `{"budget":"max-turns"|"max-wall-time","limit":"<value>"}` on a budget stop.
- **Key names:** config keys `headless-max-turns` / `headless-max-wall-time`, env `APOGEE_HEADLESS_MAX_TURNS` / `APOGEE_HEADLESS_MAX_WALL_TIME`, flags `--max-turns` / `--max-wall-time`.
- **Exit code:** a budget stop exits `4`. A run whose final Turn ends the Exchange inside the budget is a normal completion (exit 0), even if that Turn reached the limit exactly.

**Standing requirements:**
- skills: coding-standards
- Durations in tests go through `internal/testtime`; `WaitQuiet` never stands in for readiness.
- Bubble Tea v2 names only (`tea.KeyPressMsg`).

**Out of scope:**
- Telling the model its remaining budget (model-facing; off until bench evidence).
- Any default budget; budgets for the TUI or `apogee daemon`.
- A `serve` Driver, mid-run model fallback.

## 1. TUI skips the view_diff orientation header — ✅ DONE (2026-11-01)

NOTES (2026-11-01): the `view_diff` slot's `firstLineDetail` floor (used only when a result carries no DiffStat) still reads the raw first line, so a headed result with no stat would show `--- current: <path>` there; the tool always reports a DiffStat beside a rendered diff, so it is left as is.

**What:**
**Goal:** a `view_diff` result whose first two lines are `--- current: <path>` and `+++ proposed: newContent` renders the same body and regions as the same diff without them: the header is not painted as a removed/added or context row, and region recovery still succeeds. A body without the header renders exactly as before.
**Approach (assumed at the header base):** in `internal/tui/diffbody.go`, strip a leading header pair (both lines, exact prefixes `--- current: ` and `+++ proposed: `, first two lines only) before `viewDiffRegions` → `taggedDiffRegions` and before the `viewDiffBody` → `diffBody` floor. Content lines always carry a two-cell tag, so only those two leading lines can match. Define the two prefixes once in this package, as private constants. Reword the comments in `diffbody.go` and the `diffFileRegions` / `viewDiffRegions` docs in `internal/tui/toolview.go` that say unifiedLineDiff emits no file header or that view_diff never names its file: every comment that states either fact (`grep -n "file header\|never says which" internal/tui/*.go`). view_diff still paints no file row.
**Regression guard.** The `wantBody` table is `TestPresentToolCallOutcomeSplit`'s (case "view_diff is both"), which calls `presentToolCall`, not `viewDiffCard`: add the header case there, and keep the `viewDiffCard` cases in `TestViewDiffRecoversItsRegions`. The comment rule is every comment saying view_diff's output names no file or carries no file header — `grep -n "file header\|never says which\|names no file\|never names it" internal/tui/*.go`, which also finds `RegionFiles` in `toolview.go` and `diffbody.go`'s "its output never names it"; the git-diff hits in `toolregistry.go` are out of scope.
**Files:** internal/tui/diffbody.go, internal/tui/toolview.go, internal/tui/toolpresent_test.go
**Read first:** internal/tui/diffbody.go — viewDiffBody, diffBody, viewDiffRegions, taggedDiffRegions, diffTagOf; internal/tui/toolview.go — diffFileRegions, RegionFiles; internal/tui/toolpresent_test.go — viewDiffCard, TestViewDiffRecoversItsRegions, TestPresentToolCallOutcomeSplit

**Tests:** in `toolpresent_test.go`, add header-carrying cases to `TestViewDiffRecoversItsRegions` (via `viewDiffCard`) and to `TestPresentToolCallOutcomeSplit`'s `wantBody` table. Assert the same rows and regions as the headerless fixture, and assert no row contains `current:` or `proposed:`. Add a case where only one header line is present: it renders as today.

**Acceptance:**
- `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestViewDiff|TestDiffBody|TestDiffStatSpansTheWholeDiff|TestPresentToolCallOutcomeSplit' ./internal/tui/`

**Commit:** `feat(tui): skip the view_diff orientation header in its body and regions`

## 2. view_diff names its current and proposed sides — ✅ DONE (2026-11-01)

NOTES (2026-11-01): the header goes only on a rendered diff via a new shared predicate `withinDiffBudget` (used by both `unifiedLineDiff` and `Execute`), rather than `Execute` re-spelling the cell-budget comparison; `unifiedLineDiff`'s signature and output are unchanged.
NOTES (2026-11-01): added `TestViewDiff_HeaderNamesThePathAsPassed` in diff_test.go (plain, newline, CR and tab paths) alongside the planned tests; `TestViewDiffUntaggedBodyRendersPlain`'s fixture sentence in internal/tui keeps the old over-budget wording — it pins any untagged line, so it was left as is.

**What:** fixes `viewdiff-orientation-header` (a model passing the before-text as `newContent` gets a mirrored diff that nothing in the output names). Depends on item 1.
**Goal:** a non-empty `view_diff` diff opens with `--- current: <path>` and `+++ proposed: newContent`. The tool description states that `- ` lines are the file's current content and `+ ` lines are the proposed `newContent`, in the header's words. The over-budget result stays one line and names both sides. "No changes detected" is unchanged. `docs/manual/commands.md` names the header where it describes view_diff's output; the TUI layout specs say it paints no row.
**Approach (assumed at the header base):** in `ViewDiff.Execute` (`internal/tools/diff.go`), prepend the header to the `unifiedLineDiff` output when it is non-empty. Leave `unifiedLineDiff` itself unchanged, because `regions_test.go` reads its text. Reword the `overBudgetDiff` sentence so `+%d` reads as lines in the proposed newContent and `-%d` as lines of the current file. Extend `viewDiffSpec`'s description. Refresh `internal/agent/testdata/contextcost.golden` with `go test ./internal/agent -run TestContextCostGolden -update` and check that only the tool-menu row moved. Update the docs as the regression guard says.
**Regression guard.** the header's <path> is the `path` argument verbatim when it holds no control character; a path holding a newline, CR or other control character is written Go-quoted (%q) so the header is always exactly two lines, and a test pins that case through item 1's strip.
Prepend the header only to a rendered diff, never to the over-budget sentence, which `unifiedLineDiff` also returns non-empty: `Execute` re-checks `int64(lineCount(a))*int64(lineCount(b)) > maxDiffTableCells`, or `unifiedLineDiff` returns a rendered bool; the over-budget sentence names both sides inline.
Do not add the header to the TUI paint mocks (`layout.md` Diff Preview, `tool-layout.md`, `split-diff-layout.md`), which item 1 makes true as they stand; reword `split-diff-layout.md`'s File sections bullet ("a body that names no file (… `view_diff`)") to say view_diff's orientation header is skipped and paints no row.
The presenter test is a standalone `TestViewDiff…` func in `toolsummary_pin_test.go` (`writePinFile`, its own `t.TempDir` root), never a case of `TestToolSummariesRenderThroughThePresenter`, whose `len(cases) != 11` check it would break.
**Files:** internal/tools/diff.go, internal/tools/diff_test.go, internal/agent/testdata/contextcost.golden, internal/tui/toolsummary_pin_test.go, docs/layout/split-diff-layout.md, docs/manual/commands.md
**Read first:** internal/tools/diff.go — ViewDiff.Execute, unifiedLineDiff, overBudgetDiff, viewDiffSpec; internal/tools/diff_test.go — TestViewDiff_ReportsDiffStat, countDiffTags, TestViewDiff_DegradesOverBudgetToDiffstat; internal/tui/toolsummary_pin_test.go — TestToolSummariesRenderThroughThePresenter, writePinFile; docs/layout/split-diff-layout.md — File sections bullet

**Tests:**
- `diff_test.go`: `TestViewDiff_ReportsChanges` asserts that the first two lines are the exact header with the passed path.
- `TestViewDiff_NewFileDiffsAgainstEmpty` expects the header before `+ one`.
- `TestViewDiff_DegradesOverBudgetToDiffstat` keeps its no-newline assertion and checks that the sentence names both sides.
- `TestViewDiff_ReportsDiffStat`: each `wantContent` gains the two header lines, and `countDiffTags` (or its call) skips them before comparing with the stat.
- `diff_test.go` asserts that `viewDiffSpec`'s description names `current` and `proposed newContent`.
- `TestViewDiff_NoChanges*` are unchanged.
- In `toolsummary_pin_test.go`, a standalone `TestViewDiff…` func runs the real `NewViewDiff` output through the presenter. It asserts that regions are recovered and that no rendered row shows the header, so the journey is driven with the exact emitted string; a second case passes an absent path holding a newline (`"a\nb.txt"`) and asserts the `%q` header is still two lines and still stripped.

**Acceptance:**
- `go vet ./internal/tools/ ./internal/tui/`
- `go test -race -count=1 -run 'TestViewDiff|TestUnifiedLineDiff|TestOverBudgetDiff' ./internal/tools/`
- `go test -race -count=1 -run 'TestContextCostGolden' ./internal/agent/`
- `go test -race -count=1 -run 'TestViewDiff|ToolSummaryPin' ./internal/tui/`
- `grep -n 'proposed: newContent' docs/manual/commands.md`

**Closes:** viewdiff-orientation-header
**Commit:** `fix(tools): name the current and proposed sides in view_diff output`

## 3. internal/run stops a run at a Turn budget — ✅ DONE (2026-11-01)

NOTES (2026-11-01): besides the one budget struct (`run.Budget{MaxTurns, MaxWallTime}` on `Spec.Budget`), the stop is reported as a second small exported type, `Result.BudgetStop *BudgetStop{Budget, Limit string}`, with exported name constants `BudgetMaxTurns` ("max-turns") / `BudgetMaxWallTime` ("max-wall-time"), shaped to match item 5's `{"budget":…,"limit":…}` member; the wall-time limit is spelled by `time.Duration.String()` (e.g. `90m` reads `1h30m0s`), so item 6 copies the fields straight through.
NOTES (2026-11-01): the zero Budget keeps calling `Agent.Run` untouched; a set Budget drives `Agent.Step` (equivalent for a top-level Agent: Run's step cap, time/token bounds, fault fold and mailbox drain are delegate-only) and checks Turns before wall time, reading `Spec.Now` only when a wall-time budget is set; on a hit `Agent.SettleExchange()` closes the Exchange (its cancelled-note is ephemeral, so the saved record carries no marker).

**What:**
**Goal:** `run.Spec` carries an optional Turn budget and an optional wall-time budget (zero means none). When a Turn completes and the Exchange is not finished, `run.Once` checks both budgets. If either is reached, it ends the Exchange without starting another Turn, keeps every finished Turn, saves the record as it does today, and reports in `run.Result` which budget stopped it and its limit. A run that finishes its Exchange within budget reports no budget stop. No budget set means behaviour is identical to today.
**Approach (assumed at the header base):** `run.Once` calls `a.Run(ctx)`, and `Agent.Run` loops `a.step(ctx)` until the status is not `StatusTurnComplete`. When a budget is set, drive `a.Step(ctx)` in a loop instead and check after each `StatusTurnComplete`. Count Turns with `step.TurnIndex+1`, the same count as `Result.Turns`. Measure wall time from `run.Once` entry on the `Spec.Now` clock seam. On a hit, close the Exchange with `Agent.SettleExchange()` and fall through to the existing `Snapshot` / `Store.Save` path. Budgets are Driver-neutral fields on `run.Spec`, and nothing headless-specific goes into `internal/run`. The budget type is one small exported struct, owned by `internal/run`.
**Files:** internal/run/run.go, internal/run/run_test.go
**Read first:** internal/run/run.go — Once, Spec.Now, Result.Turns, Result.Faulted; internal/agent/agent.go — Agent.Run, Agent.Step, Agent.emitTurn, Agent.SettleExchange; internal/agent/turn.go — turnLifecycle.settle; internal/run/harness_test.go — planSpec, finalScript

**Tests:** `run_test.go`, over a scripted stubllm upstream that keeps calling tools:
- a Turn budget of 2 stops after 2 Turns, the record is saved with 2 Turns, and `Result` names `max-turns` / `2`;
- a wall-time budget, using an injected `Spec.Now` that advances per call, stops at the next Turn boundary;
- an Exchange that finishes on exactly the budgeted Turn reports no budget stop;
- zero budgets match today's result.

**Acceptance:**
- `go vet ./internal/run/`
- `go test -race -count=1 -run 'TestOnce' ./internal/run/`

**Commit:** `feat(run): stop a run at a Turn or wall-time budget between Turns`

## 4. Config keys, env vars and flags for the headless budgets

**What:**
**Goal:** `headless-max-turns` (a whole count) and `headless-max-wall-time` (a Go duration such as `90m`) exist as config keys. They resolve flag (`--max-turns` / `--max-wall-time` on `apogee headless`) over env (`APOGEE_HEADLESS_MAX_TURNS` / `APOGEE_HEADLESS_MAX_WALL_TIME`) over `config.yaml`. Unset or `0` means no budget. A malformed or negative value is refused before the run starts. Both are documented in `docs/manual/configuration.md`, the keys in their table and the env vars in "Environment overrides", whose count word grows by two.
**Approach (assumed at the header base):** add two `config.KeyRegistry` rows (`internal/config/registry.go`) with `EnvVar` and `FlagName` set. Follow `delegate-max-steps` (`KindInt`, `*WholeCount` on disk) and `delegate-timeout` (`checkedField` with a duration parser, `0` = off) as precedents. Add the `fileConfig` / `Options` fields and env constants in `internal/config/config.go`. Register the two flags in `newHeadlessCommandWith` (`cmd/apogee/headless.go`), bound into `config.Options`, so `ApplyConfigOffer` resolves them. The run does not read them yet (item 6).
**Regression guard.** the two keys resolve into config.Options like every key, but only headless reads them — no other Driver (TUI, daemon, /schedule via raise) passes them on; widen the cmd/apogee Acceptance to also run, by name, every settings/config-template test regression-2.md names as affected by new registry rows.
Files also take `internal/config/defaults/config.yaml` (both keys as commented examples, `# headless-max-turns: 0`, in `KeyRegistry` order) and `internal/config/config_test.go` (two precedence cases in `TestMultiSourceKeysReadTheRegistry`, both Options names in `TestEveryConfigKeyReachesTheOptions`'s want set, non-default values in `everyKeyFileConfig`), plus the `owns` map in `registry_test.go`.
State one choice in the commit: `Editable` with a startup-only apply case in `cmd/apogee/wire_settings.go` (Desc ending `startupOnlyContract`), or `Editable: false` with no Validate hook. A negative resolved value is refused after `ApplyConfigOffer` (not started, exit 2), since `applyFlags` copies a flag unjudged.
Every manual sentence that counts or lists the flag/env-carrying keys follows (`grep -n 'Three keys\|Three of them\|file-only' docs/manual/configuration.md`, lines 11–13 and 486 among them).
**Files:** internal/config/registry.go, internal/config/config.go, internal/config/registry_test.go, internal/config/config_test.go, internal/config/defaults/config.yaml, cmd/apogee/headless.go, cmd/apogee/headless_test.go, cmd/apogee/wire_settings.go (if Editable), docs/manual/configuration.md
**Read first:** internal/config/registry.go — KeyRegistry, bindRows; internal/config/config.go — applyEnv, applyFlags, ApplyConfigOffer; internal/config/config_test.go — TestMultiSourceKeysReadTheRegistry, everyKeyFileConfig; internal/config/defaults_test.go — TestTemplateMentionsEveryRegistryKey, TestRegistryFollowsTheTemplateOrder;
cmd/apogee/wire_settings_test.go — TestEveryEditableSettingKeyHasAnApply, TestApplySettingAcceptsTheStartupOnlyKeys; cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride, numberWords; cmd/apogee/headless.go — newHeadlessCommandWith

**Tests:**
- Config tests: each layer wins in order; `0` is no budget; `-1`, `abc` and `5x` are refused with the key named.
- `TestManualDocumentsEverySettingsKey` and `TestManualListsEveryEnvironmentOverride` pass with the new rows.
- `TestHeadlessRefusesANegativeBudget` (`headless_test.go`): `apogee headless --max-turns -1` and `--max-wall-time -5m` exit 2 before the run starts.

**Acceptance:**
- `go vet ./internal/config/ ./cmd/apogee/`
- `go test -race -count=1 ./internal/config/`
- `go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestManualListsEveryEnvironmentOverride|TestEveryEditableSettingKeyHasAnApply|TestApplySettingAcceptsTheStartupOnlyKeys|TestHeadlessRefusesANegativeBudget' ./cmd/apogee/`

**Commit:** `feat(config): add the headless-max-turns and headless-max-wall-time keys`

## 5. run_finished carries budget_stop

**What:**
**Goal:** every `run_finished` Event line has a `budget_stop` member, `null` unless the run stopped at a budget, when it is `{"budget":"<name>","limit":"<value>"}`. The line version stays `2`. The three eventline goldens and the `docs/manual/headless.md` sentence that lists every `run_finished` member include it.
**Approach (assumed at the header base):** add the field to `eventjson.RunFinished` (`internal/eventjson/writer.go`) with a small struct type, and to the decoder in `internal/eventjson/decode.go` if it decodes `run_finished`. `runFinishedFrame` (`cmd/apogee/headless.go`) leaves it nil until item 6. Regenerate `cmd/apogee/testdata/eventlines/{run,not-started,not-started-after-sink}.jsonl` the way `TestE2EEventLinesGolden` documents, and confirm the only diff is the new member.
**Regression guard.** `budget_stop` belongs to the Event-line Writer's closing frame (`eventjson.RunFinished`) only; the Event journal's `run_finished` (`journalFinished`, ADR 0103) stays an empty object, as `journal_test.go` pins. The member sits last, after `turn1_cached_prompt_tokens`, so each golden's diff is one tail member.
**Files:** internal/eventjson/writer.go, internal/eventjson/decode.go, internal/eventjson/*_test.go, cmd/apogee/testdata/eventlines/run.jsonl, cmd/apogee/testdata/eventlines/not-started.jsonl, cmd/apogee/testdata/eventlines/not-started-after-sink.jsonl, docs/manual/headless.md
**Read first:** internal/eventjson/writer.go — RunFinished, Writer.RunFinished; internal/eventjson/writer_test.go — the pinned run_finished line; internal/eventjson/decode_test.go — roundTripFinished, TestDecodeRoundTripsEveryKind; internal/eventjson/journal.go — journalFinished; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden; cmd/apogee/headless.go — runFinishedFrame

**Tests:**
- `writer_test.go`'s pinned `run_finished` byte line gains `"budget_stop":null` at its tail; the journal's `{}` pin is unchanged.
- eventjson round-trip of a frame with and without `budget_stop`.
- `TestE2EEventLinesGolden` passes on the regenerated goldens.

**Acceptance:**
- `go vet ./internal/eventjson/`
- `go test -race -count=1 ./internal/eventjson/`
- `go test -race -count=1 -run 'TestE2EEventLinesGolden|TestManualListsEveryEventLineKind' ./cmd/apogee/`

**Commit:** `feat(eventjson): add the budget_stop member to run_finished`

## 6. apogee headless stops at its budget with exit 4

**What:** depends on items 3, 4 and 5.
**Goal:** with `--max-turns` or `--max-wall-time` set (by any layer), a run that reaches the budget stops at the next Turn boundary with its record saved, and prints one stderr line naming the budget and its limit (e.g. `apogee: run stopped at its max-turns budget (20)`). Record-save reporting follows the existing partial-run path, including `--no-save`. The run exits `4`. Under `--format json`, `run_finished` carries `exit_code: 4` and the `budget_stop` member. `apogee headless --help` and the exit table in `docs/manual/headless.md` list `4` with the other codes, and the manual documents the two budgets.
**Approach (assumed at the header base):** pass the resolved `config.Options` budgets into `run.Spec` on headless's own path (the regression guard says where). Add `exitBudgetStop = 4` beside `exitRunFailed` / `exitNotStarted` / `exitRunFaulted` (`cmd/apogee/headless.go`). Return an `exitError{code: exitBudgetStop}` carrying the stderr sentence as its `err` when `run.Result` reports a budget stop. Fill `budget_stop` in `runFinishedFrame`. Add a `4 the run ...` phrase to the command's `Long` and `exitBudgetStop` to `headlessExitCodes` in `headless_help_test.go`. A budget stop ranks below a fault: a run whose final Turn was abandoned still exits 3.
**Regression guard.** the budgets are copied into run.Spec only at headless's own raise/run.Spec construction in runHeadlessBody — never in config.Options resolution or the shared raise path — so the TUI, daemon and /schedule firings never carry a budget even when the keys are set; a test pins that a daemon/TUI-style firing ignores a set key if a cheap seam exists, else the item's What states the scoping as binding text.
`raise` (`wire_firing.go`) builds `run.Spec` for every Driver, so headless's copy lives in the runner `runHeadlessBody` hands it (`firingInputs.runner`); the cheap seam exists (`wire_firing_test.go` calls `raise` directly), so a test pins it.
The stop's `exitError.err` is non-nil — `exitError.Error()` dereferences it, and `main` and `runFinishedFrame` both call it — and ends with `partialRunSuffix` like the fault branch; `main` prints it once, so it is not also `PrintErrln`'d.
`headlessDeps` holds only `runner` and `confiner`: the wall-time test injects its clock as `deps.runner = func(ctx, s) { s.Now = fake; return run.Once(ctx, s) }`.
**Files:** cmd/apogee/headless.go, cmd/apogee/headless_test.go, cmd/apogee/headless_help_test.go, cmd/apogee/wire_firing_test.go, docs/manual/headless.md
**Read first:** cmd/apogee/wire_firing.go — raise, firingInputs; cmd/apogee/headless.go — runHeadlessBody, exitError, exitCodeFor, runFinishedFrame, newHeadlessCommandWith (Long); cmd/apogee/main.go — main; cmd/apogee/headless_help_test.go — headlessExitCodes;
cmd/apogee/headless_test.go — headlessRun, headlessBeatServer, TestHeadlessExitCodes, TestHeadlessFormatJSONFramesEveryExit

**Tests:** `headless_test.go` drives a real `run.Once` over `headlessBeatServer` / stubllm with a tool-calling script.
- `--max-turns 2` exits 4, its `err.Error()` is the stderr line with the exact budget and value (`headlessRun` never runs `main`), and the record is saved.
- `APOGEE_HEADLESS_MAX_TURNS` without the flag does the same.
- `--format json` gives a frame with `budget_stop` `{"budget":"max-turns","limit":"2"}`.
- `--max-wall-time` uses a clock injected through a wrapping `deps.runner`.
- `TestRaiseCarriesNoBudgetFromOptions` (`wire_firing_test.go`): `raise` with both keys set in `opts` and a capturing runner hands a Spec with no budget.
- With no budget, `TestHeadlessExitCodes` and `TestHeadlessFormatJSONFramesEveryExit` are unchanged, apart from the new case rows.

**Acceptance:**
- `go vet ./cmd/apogee/`
- `go test -race -count=1 -run 'TestHeadless|TestRaiseCarriesNoBudgetFromOptions' ./cmd/apogee/`

**Closes:** headless-run-budgets
**Commit:** `feat(headless): stop at a user-set turn or wall-time budget with exit 4`
