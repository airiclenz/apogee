# Test-suite stability, speed, memory, and Pi-vs-CI divergence — findings

**Date:** 2026-10-12 · **Commit:** `7f22d7df` (v0.25.1) · **Handoff:** `docs/handoffs/2026-10-12 - 02 - test-suite-stability-speed-memory-investigation.md`

Diagnosis only. Nothing in the code, CI or issue register has changed. The levers in §8 are proposals for discussion before a plan is written.

## Sources

- **CI census.** All 40 failed `ci.yml` runs from 2026-09-08 to 2026-10-12: 57 failed jobs, each job log grepped for failing tests.
- **Infrastructure survey.** `scripts/test-shards.sh`, `Makefile`, `.github/workflows/ci.yml`, `internal/tuitest`, `.test-timings`.
- **Windows audit.** Every test that the `windows tests` job compiles.
- **History.** About 45 flake and timing commits, the six archived flake plans and the closed flake issues, each classified by root cause.
- **One measured Pi run.** `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`, sampling the RSS of every test, compile and link process once a second.

## 1. Headline

1. **Most of the red in the window is history, and structural fixes ended it.**
   - The 5 s-deadline e2e cluster (scratch dir, symlink, fan-out) ran 09-15..09-22. It stopped once the slow-box-deadlines plan replaced fixed 5 s waits with the 60 s `tuitest.DefaultTimeout` and priced budgets.
   - The 10-08..10-11 load cluster (settings-list, smoke, live state) and the internal/tui OOM both predate the lanes rework (`7c65df39`, 2026-10-11 17:18). Every sharded Linux failure in the window ran the old `APOGEE_TEST_SHARDS: 2` model.
   - Since lanes landed, the Linux job is green 3 of 3. That is too small a sample to call it fixed.
2. **The Windows "flakes" are mostly not flakes.** The Windows job takes about 1 minute whether it passes or fails, so timing played no part in most failures. Of its 10 failed runs:
   - 4 were deterministic CRLF goldens (09-17).
   - 3 were confiner journal or label bugs (09-23 ×2 and 09-27, the last a 17-test cascade).
   - 2 were a deterministic Unix path in a Windows test (10-11, fixed by `96d436a9`).
   - 1 was a real timing flake, but in the test, not the product (10-12, ConPTY close-before-drain, §3).

   Windows breaks repeatedly because **no Windows test runs before a push**. `make check` does cross-vet the 5 Windows trees (`GOOS=windows go vet`), but every escaped failure was a runtime one — line endings, label state, a path probed — which vet cannot see.
3. **The recurring root-cause class is the wall-clock budget, and no structure prevents it.**
   - About 25 point fixes went in over 15+ commits.
   - The tests still hold about 110 literal-second deadlines outside the TUI kit, 85 `time.Sleep` calls and 107 `time.After` calls.
   - No lint or guard test bans any of them.
   - `APOGEE_TEST_SLOW` scales no budget. It changes process concurrency only.
4. **The suite no longer threatens the Pi's memory.**
   - Measured peaks: 654 MB in one process and 1.7 GB across all processes at once. At least 3.4 GB of RAM stayed free.
   - The pressure at the start of this session came from outside the suite: 1.9 GB of stale Claude Code session directories in the tmpfs `/tmp` had filled the 2 GB zram swap.
5. **Speed is the growing problem.**
   - The Linux CI job's median grew from about 5.5 min (early September) to about 13 min (early October).
   - The cmd/apogee shard time climbed from 130–240 s to over 1,200 s.
   - The Pi suite takes 16 min, and its critical path is two internal/tui render-sweep tests that cannot be split.

## 2. CI failure census (2026-09-08..10-12)

Of 40 failed runs: 30 were Linux tests, 10 Windows tests, 3 govulncheck, 1 every job (toolchain), 1 cross-compile (runner never acquired), and 1 lint. Some runs had more than one failed job.

| Test (family) | Pkg | Plat | Runs | Dates | Class | Status |
|---|---|---|---|---|---|---|
| E2EAnnouncedScratchDir… / …ThroughASymlink / FanOutCeiling / PresentOpens | cmd/apogee | linux | 10 / 7 / 3 / 1 | 09-15..09-22 | fixed 5 s budget under -race | ended by slow-box-deadlines |
| ExtractPDF_BoundsAReferenceCycle | doctext | linux | 5 | 09-14..15 | fixed 10 s ceiling | ceiling removed (`76323133`) |
| ContextCostReport goldens | probe | win | 4 | 09-17 | CRLF | `.gitattributes` pin |
| filewatch Coalesces / TwoWatchers / NeverSettles | filewatch | linux | 4 | 09-18..10-01 | budget + product clock bugs | last fix was a **skip** (`7929556d`) |
| SubAgentView / ApprovalForcesALook (`✦` vs blank) | cmd/apogee | linux | 3 / 2 | 09-14..10-11 | golden captured at the wrong blink phase | `liveStarRedaction` (`04100ac9`) |
| HostileWraps / WidthSurvives / LiveState / SmokeInProcess / StreamRepaint / HooksFire / OutcomeSlots | cmd/apogee | linux | 3 / 3 / 2 / 2 / 2 / 1 / 1 | 10-08..10-11 | runner load: keys outran repaints, 1 m deadlines | predates lanes; none since |
| Windows confiner (ForeignPrior…, PlantedJournal…, 17-test cascade) | platform | win | 3 | 09-23, 09-27 | journal per pid, labels left behind | fixed in `ea51204c` and after |
| ApprovalKeysAreArmedAfterPaint | cmd/apogee | linux | 1 (+2 local) | 09-23 | arming paints nothing | 3 fixes, latest `ee8926ca` |
| SmokeRunsTheHostsZipArchive | cmd/release | win | 2 | 10-11 | deterministic (Unix path) | fixed `96d436a9` |
| StartPseudoConsoleRunsTheCommand | platform | win | 1 | 10-12 | close-before-drain in the test | **open** (§3) |
| Schedule* race cluster (15 tests) | cmd/apogee | linux | 1 | 09-11 | real DATA RACE (seam swap) | seams guard |
| Singletons: TeaSinkEmitsEventsInOrder, DriverQuitReturnsTheRunError (goroutine outlived 1 m), heartbeat `server_error`, LandlockAllowsDevNull out-of-box write | various | linux | 1 each | — | drain, leak, unknown, possible thread migration | not investigated |

The non-test failures were:
- **govulncheck (10-10):** real advisories against go1.26.8 and x/net v0.55.0, cleared by the Go and x/net bumps.
- **35dfc396 (10-11):** `go.mod` asked for 1.26.9 while setup-go served 1.26.8 with `GOTOOLCHAIN=local`. `8f2b128f` fixed it.
- **e96c8706 (10-05):** no hosted runner was acquired.

None of these is a flake.

No log in the window shows `panic:`, `test timed out`, `signal: killed` or an OOM.

## 3. Windows

**`TestStartPseudoConsoleRunsTheCommand`: the hypothesis is confirmed.**
- The test drains, waits, and then calls `pty.Close()` (`internal/platform/conpty_windows_test.go:45`).
- `Close` is `errors.Join(c.Release(), c.CloseOutput())` (`conpty_windows.go:372-377`). `CloseOutput` cancels the reader's in-flight `ReadFile` (`CancelIoEx`) before conhost's final flush arrives.
- The type comment documents this exact hazard (`conpty_windows.go:68-71`, `:367-371`), and production `reap` uses the safe order: Release, then join the reader, then CloseOutput (`internal/console/process_windows.go:159-168`).
- The failure output fits: `"\x1b[?9001h\x1b[?1004h"` is only conhost's start-up preamble.
- The fix is Release → `output.wait` → CloseOutput, plus a `t.Cleanup(Close)` for the Fatal paths. The test is then almost a one-run copy of `TestPseudoConsoleReleaseKeepsTheFinalFrame`; folding its exit-code assertion into that test is the alternative.
- No other content-asserting ConPTY test has this shape. `TestPseudoConsoleCloseClosesTheOutput` closes first on purpose and asserts no content.

**The raw multi-package invocation amplifies the flake but does not cause it.**
- `go test -race ./internal/platform/... ./internal/console/... …` runs up to 4 packages at once, each at `-parallel 4`, under a 10 m alarm.
- `internal/platform` and `internal/console` each run a 50-iteration cmd.exe+conhost loop side by side. Under that load conhost's render thread slows, which widens every final-frame window.
- The same load eats into console's 5 s `closeJoinTimeout` and `registry_test.go`'s 10 s bound, and raises the odds that the confiner tests' `deadPID` is recycled.

**The other Windows risks are all "possible", none confirmed:**
- `TestProcessCloseIsBoundedOnWindows` (fixed 5 s bound).
- `TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows`: production joins the reader for only 5 s, so under heavy load it can still cut the frame. This is a product edge, not only a test one.
- `TestRegistryOpenRunsAConfinedConsoleOnWindows` (10 s, while its siblings use 30 s).
- The confiner `deadPID` reuse.
- Journal `os.Rename` against Defender or the indexer.

**Coverage.** Every test in `internal/platform` and `internal/console` that touches ConPTY, the confiner or winlabel is unique Windows coverage. The probe, auditsplit, cmd/release and ring tests duplicate Linux logic and prove only the Windows paths, quoting and line endings.

**Running `scripts/test-shards.sh` on Windows is possible but not worth it now.**
- The script has no package-scope input.
- Its `go test -c -o …/N.test` output lacks `.exe`.
- It relies on MSYS path conversion, and its signal attribution means nothing on Windows.
- The 7 packages total about 19 s of test time.

A `-p 1` split for the two ConPTY packages buys the same isolation for one flag.

## 4. Root-cause classes: what recurs after point fixes

| Class | Point fixes | Recurs? | Structural measure today | Gap |
|---|---|---|---|---|
| A wall-clock budget | ~25 sites / 15+ commits | yes, constantly | `tuitest.DefaultTimeout` (60 s) and priced budgets (`settled`, `repaintBudget`, `typingAllowance`, …) | covers only the TUI kit (~18 test lines use it); ~110 literal deadlines and ~25 package-local constants elsewhere; no env scale; no lint |
| B poll-until-match / arming | ~14 (pane arming alone: 7 commits in 6 weeks) | yes | cursor-answer counting (`PaneArmAnswers`, `CursorMark`), `PromptSlot.Waiting()` | `WaitQuiet(settled)` at 216 sites still treats "quiet" as "ready", which fails wherever readiness paints nothing |
| F load / concurrency shape | 9 reworks of `test-shards.sh` in 5 weeks | yes, until 10-11 | lanes, short chunks, timings seed | the seed is refreshed by hand (it went stale once); CI packs by Pi timings |
| C drain / shutdown ordering | 7 | yes (pty, ConPTY, console, Windows) | product `reap` order, kit teardown join | the tests themselves can still call `Close` first |
| E CRLF / golden | 6 per-path `.gitattributes` pins | yes: each new byte-compared path breaks Windows once | path pins | no repo-wide `* text=auto eol=lf` |
| D shared seam / global state | 2 | **no** | `seams_guard_test.go`, `TestNoPackageLevelExecSeam` | — the one class a guard fully retired |
| G OOM | 1 | not since 10-11 | lanes, chunks, `TestModelFitsTheStack` | — |

**About a third of the flake investigations ended in a real product defect.** Several of those had first been papered over by a widened wait:
- undo-snapshot-in-box (P1), first masked by `d7645741`
- the approval, ask_user and adoption pane arming (one P1)
- `tui.Model` crossing 128 KiB, which heap-copied on every call
- the stale Esc-disarm tick
- the filewatch clock bugs
- the winlabel Recover deadlock
- the ConPTY final frame
- the headless stderr race

**These were left open and never filed or done:**
- The esc×2 window is measured at fold time, not at the keypress. Under load, esc×2 can fail to stop a run, and `stopRun` re-pressing hides it.
- `demorig-take-start-eperm` was closed with a 3× retry and "root cause not pinned".
- `ci-e2e-timeout-flakes` set its acceptance as "N consecutive green main pushes" but was closed on its plan items instead.
- `TestE2EDefaultWireStaysChatCompletions` fails about 1 in 3 on the owner's windows/arm64 box.

## 5. Speed

**Pi (measured):**
- 975 s wall: 28 processes over 3 lanes, about 18 s of build first.
- Work by package: cmd/apogee 1,280 s in 13 chunks, internal/tui 911 s in 9, the other packages 594 s in 6 batches.

**Slowest tests:**

| Test | s |
|---|---|
| tui `TestOverlayHeightQueryMatchesItsRender` | 141 |
| tui `TestInputBoxWindowFollowsTheCaret` | 136 |
| tui `TestFrameNeverExceedsTheTerminalHeight` | 76 |
| tui `TestLineEditorLongLineKeepsTheMemoHeapBounded` | 60 |
| `TestE2EDelegationStepCap` | 59 |
| `TestE2EOutcomeSlotsCarryTheToolsVerdict` | 50 |
| tools `TestNoPackageLevelExecSeamBites` | 49 |
| config `TestSpliceScalarSettingRoundTripsEveryEditableKey` | 43 |

- The two largest tui tests are each a chunk of their own, and their parents cannot be split at the `-run` grain. Together they are about 30% of one lane's wall time.

**CI:**
- The Linux job's median grew from about 5.5 to about 13 min in a month, and the cmd/apogee shard time grew about 5×.
- The suite grew (939 → 941+ cmd tests, more e2e). The rest of the growth is unmeasured.

**Speed levers, by certainty:**

| Lever | Expected effect | Evidence |
|---|---|---|
| Split the two 140 s tui sweep tables, or pack at subtest grain (`-run '^Parent$/^sub$'`) | shortens the Pi critical path by ~100 s; spreads the load evenly | measured |
| Drop `WaitQuiet(settled)` where a readiness signal exists | ≥54 s of fixed 250 ms waits (216 sites) | counted |
| `repaintBudget` spent in full per transcript walk / `waitForFrameChange` | ~50–70 s | counted; `test-drivers.md` still says "half-second per walk" (stale) |
| `e2e-typed-prompts`: paste instead of key-by-key | 100–470 s of the ~1,390 s E2E time (190 submits, ~5,250 bytes, 20–90 ms per key) | estimated, unprofiled; possibly the largest single lever |
| Build the e2e apogee binary once in the script instead of in each chunk's `TestMain` | 13 → 1 link per run | counted |
| `snapshot-frame-cache` | single-digit % of cmd/apogee, plus less contention on the emulator mutex | estimated; needs a profile |
| CI cache: setup-go saves GOCACHE only on a key miss, and the key is shared by all ubuntu jobs, so the saved cache probably lacks race objects | unknown; confirm in the run logs | inferred |

## 6. Memory (measured on the Pi, slow mode, 3 lanes)

| Measure | Value |
|---|---|
| Peak RSS, internal/tui chunk (`1.test`) | 503–654 MB |
| Peak RSS, cmd/apogee chunk (`0.test`) | 418–509 MB |
| Peak RSS, batches (agent, tools, config, provider) | 433–516 MB |
| Peak sum of all test, compile and link processes at once | 1,667 MB |
| Minimum MemAvailable during the run | 3,468 MB |
| Minimum SwapFree | 1,432 MB |
| Safety stop triggered | no |

- The internal/tui chunks are about 25% heavier than the cmd/apogee chunks. That is consistent with `tui-model-frame-size`, but this run does not isolate it.
- `GOMEMLIMIT` is not needed in CI (16 GB), and on the Pi it is not what keeps the suite safe now. The lanes and chunks bound memory; `GOMEMLIMIT` does not bound race shadow memory anyway (`building.md`).
- **A Pi-environment finding, not a repo one.** Before the run, `/tmp` (tmpfs, 4 GB) held 1.9 GB of stale Claude Code session directories (83 deleted), and zram swap was 2.0/2.0 GB full. That leaves about 2 GB less headroom than the suite needs, which is the plausible source of "the Pi behaves differently today".
- **Leaked test dirs.** `/tmp` holds stray `t.TempDir` dirs (`TestPresenter*`, `TestPasteNonAttachablePath*`, `TestMCPLoginFetch_*`). None of those tests calls `os.MkdirTemp`, and the dirs date from the 10-08 and 10-11 OOM-kill sessions, so they are most likely from processes killed before cleanup. Not a code defect.

## 7. Pi vs CI divergence

| Dimension | Pi | CI linux | CI windows |
|---|---|---|---|
| Arch / kernel | arm64, 16K pages, custom 48-bit-VA race kernel | amd64, 4K pages | amd64 |
| Cores / RAM | 4 (3–5× slower per core) / 8 GB + 2 GB zram | 4 vCPU / 16 GB | 4 vCPU / 16 GB |
| Invocation | `make test` (shards) | `make test` (shards) | raw `go test -race` over 5 trees |
| Lanes | 3 | **5 on 4 vCPU** (oversubscribed) | `-p` 4 packages at once |
| `-parallel` chunk / batch | 1 / 1 | 1 / 2 | 4 per package |
| `-timeout` | 30m | 30m | **10m** |
| GOMEMLIMIT | 2 GiB (local settings) | unset | unset |
| Packing timings | warm local | the seed, harvested **on the Pi**, so CI chunks last ~25–40 s rather than 120 s | none |
| Windows code exercised | vet only (5 trees, `make check`) | never | yes |

- **`APOGEE_TEST_SLOW=1` changes almost nothing on the Pi.** nproc 4 already gives 3 lanes at chunk `-parallel 1`, so SLOW only lowers the batch `-parallel` from 2 to 1. No Go code reads it, so no test that "needs SLOW" exists in a budget sense. The only tests that needed a slow box needed wider budgets (class A), and those were fixed point by point.
- **Both sides can run the same configuration.** The lane count already scales from nproc, so CI could drop `APOGEE_TEST_LANES: 5` and take the default 3. Whether that costs CI wall time needs one run to tell.

## 8. Ranked levers

Ranked by expected effect on how often the gate goes red, then on time.

| # | Lever | Retires | Effect | Cost |
|---|---|---|---|---|
| 1 | Fix `TestStartPseudoConsoleRunsTheCommand` (Release → drain → CloseOutput) and run the two ConPTY packages with `-p 1` in the Windows job | the open Windows flake; the load amplifier | removes the only current red | small |
| 2 | Widen `make check`'s Windows vet from 5 trees to `./...` (14 s measured) | Windows compile/vet breaks outside the 5 trees | small: runtime bugs still need the runner | small |
| 3 | Repo-wide `.gitattributes` `* text=auto eol=lf` with binary exceptions | class E | ends per-path pins | small, one-time diff check |
| 4 | One test-budget helper (for example `internal/testbudget`: `Short`, `Long`, `Scale` read from `APOGEE_TEST_SLOW` or a `APOGEE_TEST_TIME_SCALE`), with `tuitest.DefaultTimeout` derived from it, plus a lint (forbidigo or a guard test) banning `time.Sleep` and literal-duration `time.After`/`WithTimeout` in `_test.go` outside the helper and named, justified exceptions | class A, structurally, as the seams guard did for class D | the largest recurring class | medium: ~150 sites to migrate, which can be done package by package behind the guard's allowlist |
| 5 | Retire `WaitQuiet(settled)` as a readiness proxy wherever a readiness signal exists; give every arming surface a paint-free readiness mark | class B and ≥54 s | the arming saga's root | medium |
| 6 | Split or subtest-pack the two 140 s tui sweep tests | Pi critical path | ~100 s per Pi run | small |
| 7 | `e2e-typed-prompts`, after a profile | E2E time | up to several minutes | medium |
| 8 | Build the e2e binary once per run, not per chunk | 12 redundant links | tens of seconds | small |
| 9 | CI parity: drop `APOGEE_TEST_LANES: 5` (take the nproc default); confirm what the race GOCACHE holds | oversubscription; cache misses | one run to measure | small |
| 10 | A flake tally script (`scripts/ci-flakes`): failing test × platform × date × sha from `gh api` job logs; the census from this investigation is its prototype | the "is it a flake?" guesswork | evidence for every future decision | small |
| 11 | File the never-filed items: the esc×2 fold-time window, demorig EPERM root cause, `TestE2EDefaultWireStaysChatCompletions` on windows/arm64 | latent product bugs | — | filing only |

**Gate design: do not auto-retry.** A third of flake hunts found real product bugs (y72, the arming bugs, the Model stack regression), and a retry would have hidden every one of them. It would also contradict "Regressions are never deferred". Make flakes cheap to see instead (lever 10), and judge any closure by N consecutive green `main` runs, not by its plan items.

**Not recommended now:**
- `scripts/test-shards.sh` on Windows (§3).
- `GOMEMLIMIT` in CI or in the script (§6).
- Treating the Linux job as unstable before more post-lanes runs exist (§1).
