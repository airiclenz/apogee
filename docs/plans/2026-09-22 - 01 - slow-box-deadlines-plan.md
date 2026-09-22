# Slow-box deadlines: budgets sized for the hardware apogee is built for

**Goal:** Close the seven-bead `slow-box` cluster: every deadline that calls a healthy-but-slow
thing dead. Production budgets are resized and, where a check degrades, it says so instead of
returning the same value as a clean run; the test rows either assert the thing directly or carry a
budget a loaded box can meet.

**Date:** 2026-09-22
**Status:** unexecuted
**sized for:** ~200k-context host
**skills:** coding-standards
**base:** `0fb289d5`

**Regression check (2026-09-22, 0fb289d5):** 1: guard folded — supersedes ADR 0024's ten-second cadence
decision. 2: guard folded — supersedes ADR 0080 decision 6 and ADR 0056 decision 4. 3: guard folded
(decision) — supersedes the "the backend has nothing to say" clause in `internal/domain/confinement.go`'s
`Unavailable` doc. 4: guard folded; the dependency on item 3 is dropped. 5: guard folded (decision);
depends on item 6. 6: guard folded — supersedes the 600 s clamp sentence in `docs/manual/reactions.md`.
7: guard folded. 8: guard folded (decision). 9: guard folded (decision). 10: guard folded — yields to
`internal/doctext/pdf_test.go`'s recorded no-parallel rule for allocation-measured cases. 11: guard
folded (decision). 12: guard folded. 13: recast — `holdKey` returns its count and `repaintBudget` stays
under `tuitest.DefaultTimeout`. 14: guard folded — yields to `docs/adr/0062`:125 and the historical
record (`docs/adr/`, `docs/plans/archived/`, `docs/reviews/`, `CHANGELOG.md`), left as written; depends
on items 12 and 13. 13 (second round): recast again — the hold keeps a press COUNT as its bound
(`holdKeyMax` raised, `clearPrompt` derived from the count `holdKey` returns), `repaintBudget = 40 *
settled` (10 s), `waitForFrameChange` and `waitForScroll` named as the other full-budget spenders, and
the **Goal** restated so the item's own Acceptance checks it; supersedes this block's earlier 13 line.

## Authoritative sources

- The seven beads: `bd show apogee-kws apogee-c8l apogee-3mh apogee-v0e8 apogee-5y94 apogee-7fmu apogee-61g`.
- `docs/handoffs/2026-09-22 - 00 - ci-and-test-failures-triage.md` — the triage this cluster follows.
- `internal/tuitest/wait.go` — `DefaultTimeout`'s doc comment states the house principle for a
  poll-and-return budget: generous costs a healthy run nothing.
- `docs/manual/building.md` (Testing), `docs/design/test-drivers.md`, `scripts/test-shards.sh`.

## Ratified design calls

- **Probe budget (owner, 2026-09-22):** raise `discoveryTimeout` and DERIVE `heartbeat.Interval`
  from it, so "a beat is strictly shorter than the interval" holds by construction. No config key.
- **Degraded checks (owner, 2026-09-22):** fail loud. A secrets scan that cannot complete is
  distinguished from a clean one and forces the approval look; the bwrap verdict gains a typed
  timed-out cause and the startup notice names the reason.
- **Execution budgets (owner, 2026-09-22):** `run_tests` gains an optional `timeout_seconds`
  exactly as `terminal` declares it; the subprocess ceiling is raised; `apogee probe model` gains
  `--timeout`, documented as bounding one HTTP attempt.
- **PromptSlot (owner, 2026-09-22):** add a waiter observer to `domain.PromptSlot` and let the six
  sleeping tests poll it.
- **apogee-61g (owner, 2026-09-22):** scale `holdKey` and the `scrollTranscript` budgets, state the
  supported invocation where agents read it, and refresh the stale numbers.

## Standing requirements

- skills: coding-standards
- Every production budget changed here keeps its reason in the comment beside it.

## Out of scope

- `docs/plans/2026-09-22 - 00 - audit-design-findings-plan.md` and its beads — another session owns
  that plan and this branch's tree; touch none of its files.
- The `APOGEE_TEST_SLOW` concurrency knob itself: it picks process counts, never a wait.
- Any version identifier, release heading or `CHANGELOG.md` edit (item sidecars carry those).
- `internal/tuitest`'s already-cured constants (commit `31f44f75`) and the fan-out test it fixed.

## 1. The discovery probe is sized for a saturated local server, and the beat interval derives from it — ✅ DONE (2026-09-22)

NOTES (2026-09-22): consequential edit — internal/provider/client.go: made necessary by exporting discoveryTimeout as provider.DiscoveryTimeout (the discoveryDeadline field comment and WithDiscoveryTimeout's "default 5s" doc both named the old symbol/value).

NOTES (2026-09-22): consequential edit — internal/config/config.go: made necessary by the derived cadence (two comments stated the heartbeat asks "every ten seconds").

NOTES (2026-09-22): consequential edit — internal/config/defaults/config.yaml: made necessary by the derived cadence (four user-facing template comments stated the ten-second heartbeat).

NOTES (2026-09-22): consequential edit — cmd/apogee/root.go: made necessary by the raised probe budget (the comment quoted a five-second startup stall).

NOTES (2026-09-22): consequential edit — cmd/apogee/wire_verbs.go: made necessary by the raised probe budget ("two five-second discoveries").

NOTES (2026-09-22): consequential edit — cmd/apogee/delegation.go: made necessary by the derived cadence (three comments quoting ten seconds / two five-second discoveries; the "two discoveries in series would be exactly heartbeat.Interval" claim is now restated from the 2× relation, which still holds exactly).

NOTES (2026-09-22): consequential edit — cmd/apogee/wire_boot.go: made necessary by the derived cadence ("wrong ten seconds later").

NOTES (2026-09-22): consequential edit — cmd/apogee/upstream.go: made necessary by the derived cadence ("a ten-second cadence").

NOTES (2026-09-22): consequential edit — internal/tui/heartbeat_test.go: made necessary by the derived cadence (six comments and one failure message quoting ten seconds).

NOTES (2026-09-22): consequential edit — docs/manual/configuration.md: made necessary by the derived cadence (three user-facing sentences quoting the ten-second heartbeat).

NOTES (2026-09-22): consequential edit — CONTEXT.md: made necessary by the derived cadence (the Heartbeat entry stated "every ten seconds"; it now states the derivation and "about a minute").

NOTES (2026-09-22): docs/adr/0024 and docs/adr/0028 are left as written — they are the historical record, and the item's guard asks only that the supersession be NAMED where the constants are restated, which internal/heartbeat/heartbeat.go and internal/tui/heartbeat.go now do.

NOTES (2026-09-22): the guard grep's remaining hits are unrelated to this item's constants (the spinner colour lap, the shutdown grace, reaction kill grace, and several test-local budgets) and were left alone.

NOTES (2026-09-22): the longer cadence has a real suite cost: `TestE2ESeatDelegationsLineSurvivesATargetDownBeat` waits for two real failed beats and now takes ~122 s (it passes). Its wait was re-anchored to `2*heartbeat.Interval + tuitest.DefaultTimeout` as the item directs, so the allowance is added to the pair rather than multiplied by the interval.

**What.**
**Goal:** a discovery probe against a local server that answers slowly does not fail, and the
heartbeat interval is computed from the probe budget rather than stated independently, so no pair of
constants can drift into overlapping beats.
**Approach (assumed at the header base):** `internal/provider/discovery.go` holds unexported
`discoveryTimeout = 5 * time.Second`, consumed only by `Client.Discover` as the fallback for the
`discoveryDeadline` field that `WithDiscoveryTimeout` sets. Export it as `provider.DiscoveryTimeout`
at 30s, keeping the option and the field. `internal/heartbeat/heartbeat.go` states
`Interval = 10 * time.Second` with a comment naming the "beat strictly shorter than the interval"
invariant; redefine it as `2 * provider.DiscoveryTimeout` (heartbeat already imports provider) and
rewrite the comment to say the interval is derived, not fixed. `internal/tui/heartbeat.go`'s
`offlineFailureThreshold` comment names "~15–25 s" for the debounce window — restate it from the new
constants. Closes `apogee-kws`, whose acceptance is that a saturated local server is not painted
offline and not refused a send. The consequence to state in the comment: an absent server is now
reported offline later.

**Regression guard.** The rule: every comment, doc sentence and ADR line quoting the ten-second cadence
or the five-second discovery bound is restated from the derived constants — find them with
`grep -rn "ten second\|ten-second\|five-second\|five seconds" internal/ cmd/ docs/manual/ docs/adr/ CONTEXT.md`.
This supersedes `docs/adr/0024`'s recorded ten-second decision and its "~15–25 s" debounce line; name it
where the constants are restated. `cmd/apogee/e2e_seat_test.go`'s `tuitest.Within(3*heartbeat.Interval)`
wait and its cadence comment are re-anchored (e.g. `2*heartbeat.Interval + tuitest.DefaultTimeout`) so
the multiplier does not compound the new interval.

**Files:** `internal/provider/discovery.go`, `internal/provider/discovery_test.go`, `internal/heartbeat/heartbeat.go`, `internal/heartbeat/heartbeat_test.go`, `internal/tui/heartbeat.go`, `cmd/apogee/e2e_seat_test.go`

**Read first:** internal/provider/discovery.go — discoveryTimeout, Client.Discover;
internal/heartbeat/heartbeat.go — Interval; internal/tui/heartbeat.go — offlineFailureThreshold;
internal/provider/client.go — discoveryDeadline, WithDiscoveryTimeout; cmd/apogee/e2e_seat_test.go —
TestE2ESeatDelegationsLineSurvivesATargetDownBeat

**Tests.** A test pinning `heartbeat.Interval > provider.DiscoveryTimeout` (the invariant, not the
numbers). No existing case asserts the five-second default — `discoveryTimeout` is read only at
`discovery.go:155` — so the two `discovery_test.go` comments naming it are restated from the exported
constant instead, never against a literal.

**Acceptance.**
```
go build ./... && go test ./internal/provider/... ./internal/heartbeat/... ./internal/tui/...
grep -n "DiscoveryTimeout" internal/heartbeat/heartbeat.go
```

**Commit:** `fix(provider): the discovery probe is sized for a slow local server and the beat interval derives from it`

## 2. A secrets pre-check that cannot finish forces the approval look — ✅ DONE (2026-09-22)

NOTES (2026-09-22): consequential edit — docs/manual/configuration.md: made necessary by the third outcome (the manual's commit-secrets paragraph stated the look is forced by a hit alone, which is now incomplete; the added sentences quote no budget number so they cannot drift).

NOTES (2026-09-22): the timeout classification is falsifiable, not incidental — with the `errors.Is(err, errScanTimedOut)` branch removed from `newShadowIndex`, `TestCommitSecretsIncompleteScanForcesApproval/the_budget_cuts_the_scan_short` fails with "approver consulted 0 times" (checked, then restored).

**What.** Closes `apogee-c8l`, the silent degradation of a security control.
**Goal:** a `git_commit` whose staged-secret pre-check could not complete never proceeds
unprompted, and "could not complete" is distinguishable in the code from "scanned clean".
**Approach (assumed at the header base):** `internal/agent/secretsguard.go` bounds every shadow-index
git call through `runShadowGit` with `commitSecretsTimeout = 2 * time.Second`, and
`commitSecretsCheck` returns the identical `(security.PreCheck{}, false)` for a timeout, a git error
and a clean scan alike; `tightenForStagedSecrets` then hands back the untightened text verdict.
Raise the budget to 30s, and give `commitSecretsCheck` a third outcome: a scan that resolved a
repository but could not finish (a `context.DeadlineExceeded` from `runShadowGit`, or a git failure
after the repo resolved) returns a `PreCheck` whose `Outcome` is `GuardForceApproval` with a reason
naming the incomplete scan, so the Approver sees it exactly once. A root that is no repository at
all keeps today's skip — there is nothing to scan. Binding: one deep helper decides the outcome;
callers of `tightenForStagedSecrets` are unchanged.

**Regression guard.** `gitexec` renders a timed-out run as a plain `git …: timed out after …` error,
never a wrapped `context.DeadlineExceeded`, so `runShadowGit` classifies on
`errors.Is(runCtx.Err(), context.DeadlineExceeded)` after the call. One
`context.WithTimeout(ctx, commitSecretsTimeout)` taken in `commitSecretsCheck` and shared by all four
shadow runs makes 30 s the whole check's ceiling rather than each run's, and the file's "a wedged git can
never hold a commit's resolution" comment is restated to say so. This supersedes ADR 0080 decision 6 and
ADR 0056 decision 4 (silent skip on any git failure); name them.

**Files:** `internal/agent/secretsguard.go`, `internal/agent/secretsguard_test.go`

**Read first:** internal/agent/secretsguard.go — commitSecretsTimeout, commitSecretsCheck,
runShadowGit, tightenForStagedSecrets; internal/gitexec/gitexec.go — Query (the res.TimedOut branch);
internal/agent/secretsguard_test.go — countShadowGit, TestCommitSecretsGitFailureSkips;
internal/security — GuardForceApproval

**Tests.** A new case driving the real `gitexec.Query` against a stub git that sleeps past the budget —
not a `shadowGitQuery` returning `context.DeadlineExceeded`, which production never produces — asserting
the verdict forces approval and that the reason names the incomplete scan with the exact string the
Approver receives. The existing `TestCommitSecretsGitFailureSkips` (a non-repo root) must still skip.

**Acceptance.**
```
go test ./internal/agent/... -run 'CommitSecrets|Guardrails_CommitSecrets'
```

**Commit:** `fix(agent): a staged-secret scan that cannot finish forces the approval look`

## 3. The namespace probe is sized for a cold start and a timeout is a typed cause — ✅ DONE (2026-09-22)

NOTES (2026-09-22): consequential edit — internal/platform/seatbelt.go: made necessary by the cause rule, which the item keys on the caps rather than on a backend list ("every site returning caps with FSWrite false sets a cause", found with the item's own `grep -rn "ConfinementCaps{"`). An absent sandbox-exec is the last such production site outside the item's Files and now carries CauseBackendAbsent; the backend probes presence only, so it has no refusal and no timeout to report.

NOTES (2026-09-22): consequential edit — docs/design/confinement-execution-contract.md: made necessary by the new field. §5 documents each caps field in turn (`Unavailable`, `Residuals`); the `Cause` paragraph now sits between them, records that an empty `Unavailable` no longer implies an absent reason, and states the two-rung join rule. Nothing already written there became false — no live document quoted the ten-second probe budget.

NOTES (2026-09-22): `namespaceProbeTimeout` is a package var rather than a const, as the item's Tests section directs, so the timeout case can lower it; production never assigns it and the two tests that reach `probeNamespace` are both non-parallel.

NOTES (2026-09-22): the timeout classification is falsifiable, not incidental — with `domain.CauseProbeTimedOut` swapped for `CauseLaunchRefused` in `probeNamespace`, `TestNamespaceProbeReasonNamesTheCause/a_launcher_that_outlives_the_budget_times_out` fails with `cause = "launch-refused", want "probe-timed-out"` (checked, then restored).

NOTES (2026-09-22): the timeout case's stub launcher runs `exec sleep 30`, not `sleep 30`. A surviving grandchild holds the captured stderr pipe, so `Wait` would pay the whole `ProcessWaitDelay` drain: the subtest cost 5.07 s before the `exec` and 0.05 s after it.

NOTES (2026-09-22): `-race` cannot run on this host — the kernel's VMA range (47 bits) is outside ThreadSanitizer's supported 48, so every `-race` invocation aborts before the first test. The item's own Acceptance does not use it; the unraced run is what was verified here.

**What.** The platform half of `apogee-3mh`.
**Goal:** a bwrap probe on a cold, loaded box completes rather than downgrading the session's
sandbox, and a caller can tell a timed-out probe from an absent backend without matching on prose.
**Approach (assumed at the header base):** `internal/platform/namespace_linux.go` bounds one real
bwrap launch with `namespaceProbeTimeout = 10 * time.Second` inside `probeNamespace`, called once
from `NewNamespaceConfiner`; on expiry it yields the sentence `"bwrap timed out"`, which reaches
callers only as the free-text `domain.ConfinementCaps.Unavailable`. Raise the budget to 60s (a probe
that answers returns immediately, so the budget is spent only on a failure — the
`tuitest.DefaultTimeout` reasoning, applied to production). Add a typed cause alongside the
sentence: a `domain.ConfinementCause` string enum on `ConfinementCaps` with a value for a
timed-out probe, one for an absent backend and one for a refused launch, set wherever `Unavailable`
is set. Leave the sentences themselves unchanged.

**Regression guard.** Files gain `internal/platform/confiner_linux.go`,
`internal/platform/landlock_linux.go` and `internal/platform/confiner_linux_test.go`. The rule is keyed
on the caps, not on the field: every site returning caps with `FSWrite` false sets a cause — including
`internal/platform/platform.go`'s `denyConfiner` and `internal/platform/confiner_windows.go`'s latch,
which take an unknown-or-absent-backend cause — found with `grep -rn "ConfinementCaps{" internal/ cmd/`.
The two-backend join in `internal/platform/confiner_linux.go` carries the cause of the rung the caps
value is returned from (namespace), never a zero. Tests: the `neither_carries_both_reasons` want and
`TestNewConfinerOnThisHost`'s default branch in `confiner_linux_test.go` both gain the expected cause,
and `neitherHostReason` stays the unchanged sentence. This item explicitly supersedes the clause in
`internal/domain/confinement.go`'s `Unavailable` field doc that the field is empty when "the backend has
nothing to say": a cause is now set even where the sentence is empty, and that doc line is restated in
the same edit.

**Files:** `internal/domain/confinement.go`, `internal/domain/confinement_test.go`, `internal/platform/namespace_linux.go`, `internal/platform/namespace_linux_test.go`, `internal/platform/confiner_linux.go`, `internal/platform/landlock_linux.go`, `internal/platform/confiner_linux_test.go`, `internal/platform/platform.go`, `internal/platform/confiner_windows.go`

**Read first:** internal/platform/namespace_linux.go — probeNamespace, newNamespaceConfiner;
internal/platform/confiner_linux.go — selectLinuxConfiner; internal/platform/landlock_linux.go —
unavailableReason; internal/platform/confiner_linux_test.go — TestSelectLinuxConfiner,
TestNewConfinerOnThisHost; internal/platform/platform.go — denyConfiner.Capabilities

**Tests.** `TestNamespaceProbeReasonNamesTheCause`'s existing `stubLauncher` seam drives a launcher
that outlives a shortened probe budget (make the budget a package var the test can lower), asserting
both the sentence and the typed cause; a domain test that a fenceable caps value carries no cause. In
`confiner_linux_test.go` the `neither_carries_both_reasons` want and `TestNewConfinerOnThisHost`'s
default branch both gain the expected cause, with `neitherHostReason` left as the unchanged sentence.

**Acceptance.**
```
go build ./... && go test ./internal/domain/... ./internal/platform/...
```

**Commit:** `fix(platform): the namespace probe is sized for a cold start and a timeout is a typed cause`

## 4. The degraded-confinement notice names why the backend cannot fence — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the reason goes on its own `  why: <sentence>` line rather than inside the existing sentence — the fallback line is already 81 columns and a reason spliced into it would wrap unpredictably on a narrow terminal; `why:` is the label `probe.CapabilityLine` already uses for the same field, so the surfaces stay one vocabulary.

NOTES (2026-09-23): no live document had to change. `docs/design/confinement-execution-contract.md`:867 and `docs/adr/0081`:143 already state that "the startup notice, `apogee probe host` and `/confine`" all speak the `Unavailable` sentence — written as if this item had landed; the startup notice was the one of the three that did not, and now does, so both sentences become true rather than false. The only other copies of the notice text are in `docs/plans/archived/`, which is the historical record and left as written.

NOTES (2026-09-23): the new case is falsifiable, not incidental — with the `caps.Unavailable != ""` branch in `DegradedNotice` disabled, `TestDegradedNoticeNamesTheReason` fails on both reason subtests (checked, then restored).

**What.** The disclosure half of `apogee-3mh`. It reads only `caps.Unavailable`, which exists unchanged
at the base, so it and item 3 may land in either order.
**Goal:** a user whose session lost confinement learns the reason from the startup notice itself,
without having to run `/confine status`.
**Approach (assumed at the header base):** `internal/probe/confinement.go`'s `DegradedNotice` names
only the backend; `CapabilityLine` is the one surface carrying `caps.Unavailable` (as
`" · why: …"`), and `cmd/apogee/wire_boot.go`'s `announceConfinement` prints the notice. Fold the
reason into `DegradedNotice`'s text so the sentence names the backend AND why it cannot fence,
wording it once in `internal/probe` — that package is the stated single home for this wording across
its three surfaces, so `CapabilityLine` and `/confine status` keep reading the same field.

**Regression guard.** `caps.Unavailable` is empty in exactly the cell `DegradedNotice` fires in for two
shipped backends — `internal/platform/platform.go`'s `denyConfiner` (every OS without a real backend)
and `confiner_windows.go`'s latched token backend — so an empty reason keeps today's sentence verbatim
rather than emitting a dangling clause, and that cell is a test case of its own.

**Files:** `internal/probe/confinement.go`, `internal/probe/confinement_test.go`

**Read first:** internal/probe/confinement.go — DegradedNotice, CapabilityLine;
internal/probe/confinement_test.go — TestDegradedNotice; cmd/apogee/wire_boot.go — announceConfinement;
cmd/apogee/confinement_e2e_test.go — TestE2EAutoDegradationJourneyOnAnIncapableHost;
cmd/apogee/headless_test.go — TestHeadlessAutoDegradedCellIsARefusalNotANotice

**Tests.** A `DegradedNotice` test feeding caps whose `Unavailable` is the timed-out sentence and
asserting the emitted notice contains it verbatim — the exact string the program prints, taken from
the emitting code, not a fixture-internal spelling. An absent-backend case asserts the same shape, and a
third case with `FSWrite:false, Unavailable:""` asserts today's notice unchanged.

**Acceptance.**
```
go test ./internal/probe/... ./internal/tui/... -run 'Degraded|Capability|Confine'
go test ./cmd/apogee/... -run 'ConfinementJourney|AutoDegrad|Degraded'
```

**Commit:** `fix(probe): the degraded-confinement notice names why the backend cannot fence`

## 5. run_tests takes a timeout the model can raise

**What.** The `run_tests` half of `apogee-v0e8`. Depends on item 6, the raised ceiling, which lands
first.
**Goal:** a test suite that needs longer than the default can be run through `run_tests` by asking
for more time, and the timeout message names the budget that was actually applied.
**Approach (assumed at the header base):** `internal/tools/run_tests.go` hard-codes
`runTestsTimeout = 300 * time.Second` into the `subprocess.SubprocessSpec` it builds in `Execute`,
and `condenseTestOutput` renders `" — timed out after %s"` from the same constant. Raise the default
to 900s and add an optional `timeout_seconds` integer to `runTestsSpec`'s schema and to
`runTestsArgs`, declared in the exact shape `internal/tools/terminal.go` already uses (schema
property with the default and ceiling stated in its description; a plain `int` field; the value
handed straight to `Timeout:` with no tool-local clamp, since `internal/subprocess`'s `run` is the
one place default-and-ceiling logic lives). `condenseTestOutput` must render the budget the run
actually used, not the package default. No tool is added or removed, so the registry counts are
untouched.

**Regression guard.** A zero or absent `timeout_seconds` falls back to `runTestsTimeout`; only a
positive value is handed through — terminal's shape for the schema, not for the fallback. The timeout
line and the advertised ceiling are rendered from `min(applied, subprocess.MaxSubprocessTimeout)`, never
from the requested value.

**Files:** `internal/tools/run_tests.go`, `internal/tools/run_tests_test.go`

**Read first:** internal/tools/run_tests.go — runTestsTimeout, runTestsSpec, runTestsArgs,
RunTests.Execute, condenseTestOutput; internal/subprocess/subprocess.go — MaxSubprocessTimeout, run;
internal/tools/terminal.go — terminalSpec, terminalArgs.TimeoutSeconds; internal/tools/exec_common.go —
execHost

**Tests.** A case asserting the schema advertises `timeout_seconds` with the ceiling it states rendered
from `subprocess.MaxSubprocessTimeout`; a case asserting a requested value reaches the spec's `Timeout`;
a case asserting an absent or zero value falls back to `runTestsTimeout`, not to
`subprocess.DefaultSubprocessTimeout`; a case asserting the timeout line quotes the applied budget —
the exact string the model receives, e.g. the `FAIL (go test) — timed out after …` line.

**Acceptance.**
```
go test ./internal/tools/... -run 'RunTests|Registry|Roster'
```

**Commit:** `fix(tools): run_tests takes a timeout the model can raise`

## 6. The subprocess ceiling is reachable on slow hardware

**What.** The ceiling half of `apogee-v0e8`.
**Goal:** a caller-named subprocess timeout long enough for a cold toolchain build on throttled
hardware is honoured rather than silently reduced.
**Approach (assumed at the header base):** `internal/subprocess/subprocess.go` states
`DefaultSubprocessTimeout = 120 * time.Second` and `MaxSubprocessTimeout = 600 * time.Second`, and
`run` clamps `spec.Timeout` between them with no record that a clamp happened. Raise
`MaxSubprocessTimeout` to 3600s, keeping the default where it is (it is the value `terminal` and
`python_exec` document), and restate the ceiling's comment in terms of the hardware it must admit.
The silent clamp itself is the codebase's established idiom (`http_request` clamps the same way) and
stays.

**Regression guard.** The ceiling is quoted in prose. The rule: every description, comment or manual
sentence stating the old maximum is restated — find them with
`grep -rn "600\|max 600\|MaxSubprocessTimeout" internal/tools/ internal/subprocess/ docs/`. Three of
them are model- or user-facing and are in **Files:** for it: `terminal.go`'s and `python_exec.go`'s
`timeout_seconds` descriptions, derived from the constants rather than restated by hand, and
`docs/manual/reactions.md`'s clamp sentence, whose recorded 600 s ceiling this item supersedes.

**Files:** `internal/subprocess/subprocess.go`, `internal/subprocess/subprocess_test.go`, `internal/tools/terminal.go`, `internal/tools/python_exec.go`, `docs/manual/reactions.md`

**Read first:** internal/subprocess/subprocess.go — MaxSubprocessTimeout, DefaultSubprocessTimeout,
run; internal/tools/terminal.go — terminalSpec schema; internal/tools/python_exec.go — pythonExecSpec
schema; internal/tools/exec_common.go — RunHookSubprocess; docs/manual/reactions.md — the advise/gate
ceilings paragraph

**Tests.** No clamp case exists today and the effective timeout is a local in `run`, so extract the
clamp as an in-package `effectiveTimeout(time.Duration) time.Duration` that `run` calls and write both
cases against it: a request above the old ceiling and below the new one is honoured unclamped, and one
above the new ceiling is clamped to the constant, never to a literal.

**Acceptance.**
```
go build ./... && go test ./internal/subprocess/... ./internal/tools/... -run 'Subprocess|Terminal|Python'
```

**Commit:** `fix(subprocess): the timeout ceiling is reachable on slow hardware`

## 7. apogee probe model takes a timeout, and its comment stops calling a slow model hung

**What.** Closes `apogee-5y94`.
**Goal:** a battery against a CPU-quantised local model completes, and the documented meaning of the
probe's timeout matches what the code bounds.
**Approach (assumed at the header base):** `cmd/apogee/probemodel.go` builds its client with
`provider.WithRequestTimeout(batteryRequestTimeout)` where `batteryRequestTimeout = 60 * time.Second`,
and the constant's comment claims a call still running after a minute is a hung server. That comment
is wrong twice over: a 30B-class model on CPU ordinarily exceeds it, and `WithRequestTimeout` bounds
one HTTP ATTEMPT, which the client's retries can multiply. Raise the default to 300s, add a
`--timeout` flag bound to a local `time.Duration` via `flags.DurationVar` following the `noSave`
local-variable pattern (not a `config.Options` field — no other probe command needs it), thread it
into `WithRequestTimeout`, and rewrite the comment to say it bounds one attempt and that retries can
multiply it. `docs/manual/probe.md` gains the flag in the paragraph listing `--endpoint`, `--model`
and `--config`, with one sentence on the per-attempt meaning.

**Regression guard.** The repo root is the library package `apogee`, not a main, so `go run .` fails at
every commit — the acceptance drives `go run ./cmd/apogee`. `Client.requestTimeout` is unexported with
no accessor beside `StreamIdleTimeout()`, and the client is built inside the RunE closure, so the
default case asserts the registered flag's own `DefValue` rather than a round trip that passes
identically against the old 60 s default.

**Files:** `cmd/apogee/probemodel.go`, `cmd/apogee/probemodel_test.go`, `docs/manual/probe.md`

**Read first:** cmd/apogee/probemodel.go — batteryRequestTimeout, probeModelCommand;
cmd/apogee/probemodel_test.go — runProbeModel, newProbeCommand, modelUpstream;
internal/provider/client.go — WithRequestTimeout, Client.requestTimeout, Client.attemptContext;
docs/manual/probe.md — the `probe model` flag paragraph

**Tests.** A case asserting `--timeout` reaches the client's request timeout, driving the command as a
user does against a slow httptest upstream with the flag string exactly as registered; a case asserting
the registered flag's own `DefValue` is the new default.

**Acceptance.**
```
go test ./cmd/apogee/... -run 'ProbeModel'
go run ./cmd/apogee probe model --help
```

**Commit:** `fix(probe): apogee probe model takes a timeout sized for CPU inference`

## 8. PromptSlot can tell a test that a caller is waiting

**What.** The production half of `apogee-7fmu`'s sleep family.
**Goal:** a test can learn that a second caller is blocked acquiring the prompt slot, without timing
it.
**Approach (assumed at the header base):** `internal/domain/promptslot.go`'s `PromptSlot` is a
cap-1 `chan struct{}` whose `Acquire` is a bare `select` over the send and `ctx.Done()`; nothing
observes a blocked caller. Add an atomic waiter count incremented immediately before entering the
select and decremented on either exit, exposed as one exported accessor (`Waiting() int`). Binding:
the accessor is the whole seam — no callback plumbing, no test-only build tag — and callers poll it
against a generous ceiling, which is the house pattern for a poll-and-return budget. The six
sleeping call sites are item 9; this item converts only `internal/domain/promptslot_test.go`'s own
site.

**Regression guard.** The waiter count increments before the select, so it also counts a caller that
acquires without ever blocking — say so where the accessor is described.

**Files:** `internal/domain/promptslot.go`, `internal/domain/promptslot_test.go`

**Read first:** internal/domain/promptslot.go — PromptSlot, NewPromptSlot, Acquire, Release;
internal/domain/promptslot_test.go — TestPromptSlot_QueuedCallerAnswersItsOwnCancellation;
internal/agent/construct.go — queuedApprover.Approve, PromptSlotFor; internal/tools/ask_user.go —
queuedAsker.Ask

**Tests.** A test that a caller blocked in `Acquire` is counted and that the count returns to zero
when it acquires or its context is cancelled; `TestPromptSlot_QueuedCallerAnswersItsOwnCancellation`
polls `Waiting()` in place of its `time.Sleep(20 * time.Millisecond)`.

**Acceptance.**
```
go test -race ./internal/domain/... -run 'PromptSlot'
```

**Commit:** `test(domain): PromptSlot reports a waiting caller instead of a test timing one`

## 9. The queued-caller tests block on the waiter count, not on a sleep

**What.** The test half of `apogee-7fmu`'s sleep family. Depends on item 8.
**Goal:** none of the queued-caller tests asserts another goroutine has reached a blocking point by
having slept.
**Approach (assumed at the header base):** five sites sleep 20ms to mean "the second caller is now
genuinely waiting": `internal/agent/promptslot_test.go` (two), `internal/agent/approvalqueue_test.go`,
`internal/agent/approvalcache_test.go` and `internal/tools/askqueue_test.go`. Each exercises a
production type (`queuedApprover`, `queuedAsker`) that acquires the shared `domain.PromptSlot`, so
each replaces its sleep with a poll of item 8's accessor against a generous ceiling, failing with a
message naming what never happened.

**Regression guard.** The rule: every test in these packages that sleeps to mean "a caller is now
queued" is converted, not only the five listed — find the rest with
`grep -rn "genuinely waiting\|time.Sleep(20" internal/agent internal/tools internal/domain`, after which
only `terminal_test.go`'s pidAlive poll may remain. The polls are written against a slot a first caller
demonstrably holds, never merely against `Waiting() > 0`; name the opposing constant whose bite the
widened budget must still preserve, so the widening does not silently retire the guard. Three sites call
the seam on `context.Background()` and so reach the seam's own private slot in-package —
`q.(*queuedApprover).slot.Waiting()`, `q.(*queuedAsker).slot.Waiting()` — while the two
`promptslot_test.go` sites poll their own `slot`.

**Files:** `internal/agent/promptslot_test.go`, `internal/agent/approvalqueue_test.go`, `internal/agent/approvalcache_test.go`, `internal/tools/askqueue_test.go`

**Read first:** internal/agent/construct.go — queuedApprover.slot, PromptSlotFor;
internal/tools/ask_user.go — queuedAsker.slot; internal/agent/approvalqueue_test.go —
TestQueuedApprover_QueuedRequestAnswersItsOwnCancellation; internal/agent/approvalcache_test.go —
TestApprovalSeam_TwinCoalescesWhileItWaits; internal/tools/askqueue_test.go —
TestQueuedAsker_QueuedQuestionAnswersItsOwnCancellation

**Tests.** The converted tests are the tests; each must still fail when the production coalescing or
ordering it asserts is broken.

**Acceptance.**
```
go test -race -count=2 ./internal/agent/... ./internal/tools/... -run 'PromptSlot|Approval|Queued|AskUser'
grep -rn "time.Sleep(20" internal/agent internal/tools
```

**Commit:** `test(agent): the queued-caller tests block on the waiter count, not on a sleep`

## 10. Three wall-clock proxies assert the thing they stand for

**What.** The "measure it, do not time it" rows of `apogee-7fmu`.
**Goal:** none of these three tests proves its claim by a wall-clock reading a loaded box can break.
**Approach (assumed at the header base):**
`internal/tools/network_funnel_test.go`'s one-budget case bounds a real resolver swap plus an
httptest dial with `slack = 250 * time.Millisecond` over a 600ms budget — 250ms of margin under
`-race`. Its sibling in `TestNetworkFunnel_TimeoutResolution` makes the same kind of claim with a
multiplicative ceiling anchored to `budget` plus an independent watchdog; copy that shape, scaling
the case's own `budget` and lookup constants so the shared-budget and per-phase outcomes are
separated by a multiple rather than by a fixed slack.
`internal/doctext/pdf_test.go`'s `TestExtractPDF_RefusesAnAbsurdXrefSize` times the refusal at 100ms
to mean "decided before the parser allocates"; `TestExtractPDF_RefusesAnInflateBomb` in the same file
already measures exactly that with a `runtime.MemStats` delta against an allocation ceiling — adopt
that, and drop the clock.
`internal/provider/reliability_test.go`'s `TestRespond_HonorsRetryAfterHeader` bounds two real
httptest round trips by `elapsed >= retry429BaseDelay` with no margin; assert instead the gap the
backoff would have introduced — the interval the server itself observes between the two requests —
against the same constant.

**Regression guard.** Name the opposing constant whose bite the widened budget must still preserve (the
per-phase `lookup+budget` outcome the funnel case must stay below): the ceiling sits strictly between
`budget` and `lookup+budget` — e.g. 1.5×budget with lookup ≥ 0.8×budget — and the room comes from
scaling `budget` and `lookup` up in absolute terms, never from raising the multiple. The PDF conversion
yields to `pdf_test.go`'s own recorded rule that an allocation-measured case does not run in parallel: it
drops `t.Parallel()` from `TestExtractPDF_RefusesAnAbsurdXrefSize` and names a ceiling loose enough for
the parse of `minimal.pdf`.

**Files:** `internal/tools/network_funnel_test.go`, `internal/doctext/pdf_test.go`, `internal/provider/reliability_test.go`

**Read first:** internal/tools/network_funnel_test.go —
TestNetworkFunnel_OneBudgetCoversResolveAndRequest, TestNetworkFunnel_TimeoutResolution;
internal/doctext/pdf_test.go — TestExtractPDF_RefusesAnAbsurdXrefSize,
TestExtractPDF_RefusesAnInflateBomb; internal/provider/reliability_test.go —
TestRespond_HonorsRetryAfterHeader; internal/provider/client.go — retry429BaseDelay

**Tests.** The three rewritten cases; each must still fail against the defect it guards (a per-phase
budget, a materialised stream, a header-less backoff).

**Acceptance.**
```
go test -race ./internal/tools/... -run 'NetworkFunnel'
go test -race ./internal/doctext/... -run 'ExtractPDF'
go test -race ./internal/provider/... -run 'RetryAfter|Respond'
```

**Commit:** `test: three wall-clock proxies assert the thing they stand for`

## 11. The real-process and settle-window margins fit a loaded box

**What.** The remaining `apogee-7fmu` rows, where a real process or a real settle window is the
thing being timed and a budget is the honest instrument.
**Goal:** each margin is generous enough that a loaded box meets it, and each says in its comment
what it is a margin for.
**Approach (assumed at the header base):** `internal/userexec/userexec_test.go`'s deadline table
allows `time.Second` (and `WaitGrace + time.Second`) around a `/bin/sh` cold start and a
process-group kill under `-race`, and `internal/reactions/command_test.go` mirrors it one layer up;
widen both allowances substantially while keeping them anchored to `userexec.WaitGrace` so the claim
("killed at the deadline, drained within the grace") is unchanged.
`internal/filewatch/filewatch_unix_test.go` sleeps `testSettle / 3` to land a second write inside a
150ms settle window driven by a real `SIGSTOP`/`SIGCONT` stall; widen `testSettle` itself in
`internal/filewatch/filewatch_test.go` so the fraction has real room, leaving the test's design
intact.

**Files:** `internal/userexec/userexec_test.go`, `internal/reactions/command_test.go`, `internal/filewatch/filewatch_test.go`, `internal/filewatch/filewatch_unix_test.go`

**Read first:** internal/userexec/userexec_test.go —
TestRunReportsTheDeadlineRatherThanWaitingOnASleep; internal/userexec/userexec.go — WaitGrace;
internal/reactions/command_test.go — TestCommandExecutorReportsTheDeadlineRatherThanWaitingOnASleep;
internal/filewatch/filewatch_test.go — testSettle, testDeadline, testQuiet, awaitChange;
internal/filewatch/filewatch_unix_test.go — TestWatchSettlesOnTheClockNotTheTick

**Regression guard.** `testSettle` is shared. The rule: every test in `internal/filewatch` reading
`testSettle` still asserts what it asserted — find them with `grep -rn "testSettle" internal/filewatch`
and re-run the whole package, not only the unix file. Name the opposing constant whose bite the widened
budget must still preserve — the deadline the `sleep 5` table kills against, and `testSettle`'s own
window: both `within` bounds stay strictly below the script's own sleep (at most `WaitGrace + 2s`) unless
the same edit lengthens both scripts to `sleep 30`, and `testDeadline` — `awaitChange`'s whole window,
which names no `testSettle` and so the grep cannot find — rises with it (e.g.
`testDeadline = 10 * testSettle`, `testQuiet ≥ testSettle`).

**Tests.** The existing cases, unchanged in what they assert.

**Acceptance.**
```
go test -race -count=2 ./internal/userexec/... ./internal/reactions/... ./internal/filewatch/...
```

**Commit:** `test: the real-process and settle-window margins fit a loaded box`

## 12. The e2e budgets outside the cured kit follow the kit's principle

**What.** The `cmd/apogee` constants of `apogee-7fmu` that commit `31f44f75` did not reach.
**Goal:** no e2e budget is centred on one machine's measurement.
**Approach (assumed at the header base):** `cmd/apogee/e2e_stream_test.go`'s
`streamReplyWait = 15 * time.Second` is documented as arithmetic from a measurement on one box —
restate it as `tuitest.DefaultTimeout` plus an allowance for the stub's own playback, the shape
`submit` and `awaitReply` already use. `cmd/apogee/e2e_newcomer_test.go`'s `newcomerBudget` (15m
whole) and `newcomerStepBudget` (60s per step) bound a live judge model and a docker exec; widen
both and say in the comment what hardware the step budget admits.

**Regression guard.** `newcomerBudget` is the ctx every judge call and every `docker exec` hangs off,
and its expiry is a `t.Fatalf`, not the reader's report. The relation — `newcomerBudget` ≥
`newcomerMaxSteps` × `newcomerStepBudget` plus the judge's own round trips, which today's 15 m against
20 × 60 s already fails — is stated in the item and in the comment, and the whole-exercise budget is
widened from the step budget rather than independently.

**Files:** `cmd/apogee/e2e_stream_test.go`, `cmd/apogee/e2e_newcomer_test.go`

**Read first:** cmd/apogee/e2e_stream_test.go — streamReplyWait,
TestE2EStreamCommitsCompleteAndInOrder; cmd/apogee/e2e_smoke_test.go — submit, awaitReply,
toolTurnAllowance; cmd/apogee/e2e_newcomer_test.go — newcomerBudget, newcomerStepBudget,
newcomerMaxSteps, driveNewcomer; internal/tuitest/wait.go — DefaultTimeout

**Tests.** The existing e2e cases; the newcomer test is gated on its live/docker environment and
runs only where that is present.

**Acceptance.**
```
go test ./cmd/apogee/... -run 'TestE2EStream'
go vet ./cmd/apogee/...
```

**Commit:** `test(e2e): the budgets outside the cured kit follow the kit's principle`

## 13. holdKey and the transcript walk scale with the work they drive

**What.** Recast at the regression check (2026-09-22). Closes the code half of `apogee-61g`: the two
`cmd/apogee` budgets that are still flat literals after commit `31f44f75`.
**Goal:** the hold and the transcript walk bound themselves by budgets derived from `holdKeyMax` and
`settled`, and both named tests pass three times in a row under `-race`.
**Approach (assumed at the header base):** `cmd/apogee/e2e_approval_test.go`'s `holdKey` sends at
most `holdKeyMax = 12` presses at a local 5ms gap — a ≤60ms window that must outlast a pane paint
under load; keep the press count as the hold's bound and raise `holdKeyMax` so the hold survives a
loaded box, keeping the gap; `holdKey` returns the count it actually sent, and `clearPrompt` — the
second consumer of `holdKeyMax`, which presses one backspace more than the hold sent and whose doc
says the two are the same number — derives its backspaces from that returned count in the same edit.
`promptly = 2 * time.Second` in the same file bounds a whole
TUI round trip after a deliberate keypress; widen it and restate its comment as a liveness bound, not a responsiveness
measurement. `cmd/apogee/e2e_support_test.go`'s `scrollTranscript` walks pages under
`repaintBudget = 3 * time.Second` and a literal `WaitQuiet(60 * time.Millisecond)`; raise the first to
`40 * settled` (10 s) — the package's own constant, strictly greater than today's 3 s and well under
`tuitest.DefaultTimeout` — and replace the literal with that same constant, so a slow page cannot make
the walk conclude early and read a truncated transcript. `cmd/apogee/e2e_hostile_test.go`'s
`waitForFrameChange` and `cmd/apogee/e2e_stream_test.go`'s `waitForScroll` each spend the whole
`repaintBudget` once per walk to conclude "no movement", so the raise lengthens them too; neither needs
an edit — their docs already cite `repaintBudget` — and the added suite time is a deliberate cost.

**Regression guard.** The hold keeps a press COUNT as its bound — the owner reversed the earlier call
after this round (owner, 2026-09-22). `holdKeyMax` stays the bound and is raised; `holdKey` returns the
count it actually sent and `clearPrompt` derives its backspaces from that returned count instead of from
`holdKeyMax + 1`. No `tuitest.DefaultTimeout`-anchored hold window: the count is what keeps presses out
of `approvalArmDelay`'s 100 ms arming window in `internal/tui/approval.go`, and
`cmd/apogee/e2e_approval_test.go`'s own comment records that bound as deliberate. `repaintBudget` is
stated as `40 * settled` (10 s) — strictly greater than today's 3 s and well under
`tuitest.DefaultTimeout`; name `cmd/apogee/e2e_hostile_test.go`'s `waitForFrameChange` and
`cmd/apogee/e2e_stream_test.go`'s `waitForScroll` in the **What** as the two other full-budget spenders
the raise lengthens (no edit to them needed — their docs already cite `repaintBudget`), so the added
suite time is a deliberate cost. Restate the **Goal** so the item's own Acceptance can check it: the
hold and the transcript walk bound themselves by budgets derived from `holdKeyMax` and `settled`, and
both named tests pass three times in a row under `-race`.

**Files:** `cmd/apogee/e2e_approval_test.go`, `cmd/apogee/e2e_support_test.go`

**Read first:** cmd/apogee/e2e_approval_test.go — holdKey, holdKeyMax, clearPrompt, TestE2EApprovalKeysAreArmedAfterPaint;
internal/tui/approval.go — approvalArmDelay; cmd/apogee/e2e_support_test.go — repaintBudget, awaitRepaint, scrollTranscript;
cmd/apogee/e2e_smoke_test.go — settled; cmd/apogee/e2e_hostile_test.go — waitForFrameChange; cmd/apogee/e2e_stream_test.go — waitForScroll

**Tests.** `TestE2EApprovalKeysAreArmedAfterPaint` and `TestE2EOutcomeSlotsCarryTheToolsVerdict`,
both run repeatedly to show the flake is gone.

**Acceptance.**
```
go test -race -count=3 -timeout 20m ./cmd/apogee/ -run 'TestE2EApprovalKeysAreArmedAfterPaint|TestE2EOutcomeSlotsCarryTheToolsVerdict'
```

**Commit:** `test(e2e): holdKey and the transcript walk scale with the work they drive`

## 14. The docs state the supported way to run the suite, and stop quoting the old budgets

**What.** The documentation half of `apogee-61g`, and the one owning item for every stale budget
number this cluster's predecessor left behind. Depends on items 12 and 13 — both change numbers this
item then restates from the code.
**Goal:** an agent reading the repo's own instructions learns that `make test` is the supported way
to run the suite and that a raw multi-package `go test` is not, and no document or script comment
quotes a budget the code no longer holds.
**Approach (assumed at the header base):** `AGENTS.md` names the three `APOGEE_TEST_*` knobs and
points at `docs/manual/building.md`; neither warns against the raw multi-package shape that produced
this bead's three failures, and `docs/manual/building.md` recommends a raw full-`./...` run for
bisecting without distinguishing it from the partial, unraced shape. State the supported invocation
in both. Separately, commit `31f44f75` raised the kit's constants and left their old values quoted
in prose.

**Regression guard.** The rule: every sentence in the repo's LIVE docs and script comments that quotes a
test-kit budget is restated from the code, not only the ones listed here — find them with
`grep -rn "5 s\|5s wait\|2 s\|leak grace\|DefaultTimeout" docs/manual/ docs/design/ scripts/ AGENTS.md README.md CONTEXT.md`
and check each against `internal/tuitest/wait.go` and `internal/tuitest/leak.go`. `docs/adr/`,
`docs/plans/archived/`, `docs/reviews/` and `CHANGELOG.md` are the historical record and are left as
written — the item yields to `docs/adr/0062-test-drivers-are-drivers.md`:125 ("the whole e2e set budgeted
at ~15 s under `-race`"), already reconciled in prose at `docs/design/test-drivers.md:1053`. `AGENTS.md`
is concurrently modified by the `2026-09-22 - 00` session, so the item re-reads it at run time and edits
only its own testing sentences, never reverting that session's edits.

**Files:** `AGENTS.md`, `docs/manual/building.md`, `docs/design/test-drivers.md`, `scripts/test-shards.sh`

**Read first:** AGENTS.md — the `make check` / APOGEE_TEST_* bullet; docs/manual/building.md — the
Testing section; docs/design/test-drivers.md — "Waiting", "Gates and budgets";
scripts/test-shards.sh — the shard-budget comments; internal/tuitest/wait.go — DefaultTimeout;
internal/tuitest/leak.go — leakGrace

**Tests.** None (documentation and a shell comment). The repo's existing docs gates must still pass.

**Acceptance.**
```
go test ./cmd/apogee/... -run 'TestManualDocuments|TestManualLists'
sh -n scripts/test-shards.sh
```

**Commit:** `docs(testing): the supported way to run the suite, and budgets quoted from the code`
