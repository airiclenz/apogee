# run_tests subdirectory markers — detect at or above `path`, run from the marker's directory

**Goal:** `run_tests path=<dir>` finds its runner at `path` or the nearest directory above it (up to the workspace root) and runs it from the directory holding the marker. A refusal names every directory it searched. The tool description and the manual say how `path` picks the runner.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 5cedb3a3

**Regression check (2026-11-05, 5cedb3a3):**
- 1: recast — takes the full-log hint's workdir clause from item 2 (writer's decision); guards folded: Acceptance pattern names the real cross-file tests, a bad-`path` case on an unmarked workspace, the file-`path` case pinned on pytest, the root hint spelled.
- 2: recast — owns only the refusal naming the searched directories (writer's decision); guard folded: the one-directory refusal form spelled.
- 3: guard folded — the context-cost golden's "tool menu" row is regenerated and its test runs in Acceptance.
- re-check of the recast items 1 and 2: all items SAFE; their Read first lines refreshed.

**Sources:**
- `.capcom/issues/run-tests-subdir-marker.md` (Seqcom request `run-tests-clone-marker`, run 20261105T0756Z)
- internal/tools/run_tests.go (`RunTests.Execute`, `detectTestRunner`, `testRunners`, `subtree`, `noTestRunnerMessage`, `runTestsSpec`)
- docs/design/confinement-execution-contract.md (`Dir` inside the box); terminal's `resolveWorkdirInRoot` precedent
- docs/plans/archived/2026-08-10 - 00 - tool-surface-improvements-plan.md (ratified call 6: marker precedence go ▸ pytest ▸ npm)

**Ratified design calls** (writer, 2026-11-05, from the issue's Acceptance):
- **Search:** start at `path` (its parent directory when `path` is a file), walk up to the workspace root; the first directory holding any marker wins; inside one directory call 6's precedence holds. Never search below `path`.
- **Run directory:** the runner's `Dir` is the marker directory; the target handed to the runner is `path` relative to that directory ("" when equal).
- **Model-facing directory spelling:** workspace-relative slash paths; the root reads `the workspace root`.
- **Full-log hint:** when the marker directory is not the root, the condensed footer reads ``run `<command>` with terminal (workdir <dir>) for the full log``.

**Standing requirements:**
- skills: coding-standards
- Any deviation from item text lands as a dated NOTES line under the item.
- Raspberry Pi 5 host: never `-race`/`-cover` over a whole heavy package or a pattern; targeted `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`; one test run at a time. Forward to every sub-agent.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.

**Out of scope:**
- New runners or markers; searching below `path`; monorepo "run every project" fan-out.
- Answering Seqcom's request (the owner runs the feedback-reply script after the plan ships).

## 1. Detect the runner at or above `path` and run it from the marker directory — ✅ DONE (2026-11-05)

NOTES (2026-11-05): `detectTestRunner` already returns the list of directories it searched (nearest first, "" for the root), and `TestRunTestsDetectionWalksUpFromStart` tests it; `Execute` discards it with `_` until item 2 passes it to `noTestRunnerMessage`.

NOTES (2026-11-05): the path is now resolved before detection, and the option-shaped-filter check moved after it, because that check still names the detected runner. A call with both a bad path and a bad filter now reports the path first.

NOTES (2026-11-05): the footer's workdir goes through `clipLine`, like the command echo, so a long model-supplied path cannot push the reserved footer past the 8 KB cap.

**What:** Recast at the regression check (2026-11-05). fix — `run_tests` detects its runner only at the workspace root, so a project in a subdirectory (a `checkouts/<id>` clone, a nested module) is refused, and a nested Go module cannot be tested from the outer root.

**Regression guard.** item 1 also carries the full-log hint's workdir clause (``run `<command>` with terminal (workdir <dir>) for the full log`` off the root, today's hint at the root) with its condensed-output test, so no commit tells the model to rerun a nested runner from the root; item 2 keeps only the refusal naming the searched directories, and its title, Goal and Tests drop the hint half. The Acceptance pattern names the two cross-file tests that exercise `run_tests` (`TestNotFoundRefusalsRouteThroughSuggestions`, `TestEveryExecSiteRefusesAProgramInsideTheWorkspace`); no `TestPathSuggest*` exists and `TestExecFence` matches only the confinement-box test. A file `path` is pinned on pytest; go's file-target rendering (`goPackagePattern`) is unchanged and out of scope.

**Goal:** `run_tests path=sub/proj` with `sub/proj/go.mod` runs `go test ./...` with `Dir` = `<root>/sub/proj`; `path=sub/proj/pkg` runs `go test ./pkg/...` from `sub/proj`; a pytest or npm marker at `path` or above behaves the same way; a marker-less `path` under a root `go.mod` runs `go test ./<path>/...` with `Dir` = root; a bad `path` is refused with its path error before any detection. A run from a non-root marker directory ends its condensed output with ``run `<command>` with terminal (workdir <dir>) for the full log``; a root run keeps today's ``[condensed from N bytes of output; run `<command>` with terminal for the full log]``.

**Approach (assumed at the header base):** in `RunTests.Execute` resolve `args.Path` through `t.subtree` before detection. Change each `testRunner.detect` to take the directory to probe (workspace-relative, joined onto the marker names before `markerFileExists` / `readMarkerFile`, still fenced by `t.root`). Replace `detectTestRunner(root)` with a walk that returns the runner, the marker directory and the list of directories searched (consumed by item 2). A file `path` starts the walk at its parent. Rebase the subtree onto the marker directory before calling `runner.args`, and set `SubprocessSpec.Dir` to the marker directory's absolute path. Pass the marker directory into `condenseTestOutput` / `displayCommand` so the footer line carries the workdir clause only off the root. `security.ResolveProgram` keeps judging against `t.root`. Extend `run_tests.go`; no new module.

**Files:** internal/tools/run_tests.go, internal/tools/run_tests_test.go
**Read first:** internal/tools/run_tests.go — RunTests.Execute, detectTestRunner, testRunner.detect, condenseTestOutput, displayCommand; internal/tools/run_tests_test.go — TestRunTestsDetectsRunnerByMarkerPrecedence, writeProject; internal/tools/exec_host_test.go — capturedRunHost

**Tests:** adapt `TestRunTestsDetectsRunnerByMarkerPrecedence` to the new signature; new cases with `writeProject` + `capturedRunHost` + `fakeLook`: nested `go.mod` at `path`, a `path` below the nested module, a file `path` pinned on pytest (the file positional relative to the marker directory, e.g. `tests/test_x.py`), a nested `pytest.ini` two levels below the root reached from a deeper `path`, an npm marker, and a root-only marker (Dir stays root; argv unchanged from today); an unmarked workspace with `path` `nope` refused with `path not found` (today it reads "no test runner detected"); a condensed-output test pins the workdir clause for a nested run and today's hint for a root run.

**Acceptance:**
- `go build ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRunTests|TestNotFoundRefusalsRouteThroughSuggestions|TestEveryExecSiteRefusesAProgramInsideTheWorkspace' ./internal/tools/`

**Commit:** `fix(tools): run_tests detects its runner at or above path and runs from the marker's directory`

## 2. Name the searched directories in the refusal — ✅ DONE (2026-11-05)

NOTES (2026-11-05): each searched directory is passed through `clipLine`, like the full-log hint's workdir, since it derives from the model's `path`.

**What:** Recast at the regression check (2026-11-05). fix — the "no test runner detected" refusal does not say where it looked, so it reads as if nothing was found anywhere. Depends on item 1.

**Regression guard.** see item 1's decision — item 2 owns only the refusal. The one-directory form is spelled: `path=sub` reads `no test runner detected in sub and the workspace root: …`.

**Goal:** with no marker at `path` or above, the refusal reads `no test runner detected in <dir>, <dir>, …, and the workspace root: looked for <markers>. Use terminal to run this project's own test command.` (nearest first; with one directory below the root it reads `no test runner detected in sub and the workspace root: …`; with no `path` it reads `no test runner detected in the workspace root: …`).

**Approach (assumed at the header base):** give `noTestRunnerMessage` the searched list from item 1's walk.

**Files:** internal/tools/run_tests.go, internal/tools/run_tests_test.go
**Read first:** internal/tools/run_tests.go — noTestRunnerMessage, RunTests.Execute, testRunners; internal/tools/run_tests_test.go — TestRunTestsNoMarkerIsAnErrorNamingTheMarkers, writeProject, runTestsCall

**Tests:** extend `TestRunTestsNoMarkerIsAnErrorNamingTheMarkers` to pin the full string for a nested `path` two levels down, for a one-directory `path`, and for no `path`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRunTests' ./internal/tools/`

**Commit:** `fix(tools): run_tests refusal names the directories it searched`

## 3. Say how `path` picks the runner — tool description and manual — ✅ DONE (2026-11-05)

**What:** Depends on item 1.

**Regression guard.** `run_tests` is on the default roster, so the new description and `path` text move the "tool menu" row of internal/agent/testdata/contextcost.golden: regenerate it with `GOMEMLIMIT=2GiB go test -count=1 ./internal/agent -run TestContextCostGolden -update` in this commit (precedent 0988513d, b7d664dc) and run `TestContextCostGolden` in Acceptance.

**Goal:** `runTestsSpec`'s description and its `path` schema text state that the runner is detected at `path` or the nearest directory above it and runs from the directory holding the marker; docs/manual/commands.md, section "Reading a tool's answer — diffs and near misses", carries a `run_tests` paragraph saying the same beside the "How long a command may run" paragraph.

**Approach (assumed at the header base):** edit the `runTestsSpec` description and the `path` property text (built with `fmt.Sprintf`); add the manual paragraph.

**Files:** internal/tools/run_tests.go, internal/tools/run_tests_test.go, docs/manual/commands.md, internal/agent/testdata/contextcost.golden
**Read first:** internal/tools/run_tests.go — runTestsSpec; internal/tools/run_tests_test.go — TestRunTestsSchemaAdvertisesTheTimeoutItApplies; internal/agent/contextcost_golden_test.go — TestContextCostGolden, contextCostGoldenAgent; internal/agent/testdata/contextcost.golden; docs/manual/commands.md — "Reading a tool's answer — diffs and near misses", "How long a command may run"

**Tests:** a spec test asserts the description and the `path` schema text name "nearest" / "above" and "runs from".

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRunTests' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestContextCostGolden ./internal/agent/`
- `grep -n 'run_tests' docs/manual/commands.md`

**Closes:** run-tests-subdir-marker

**Commit:** `docs(tools): run_tests says how path picks the runner`
