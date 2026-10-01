# Exec host, guarded HTTP client and budget-note latch

**Goal:** Deepen per architecture review 2026-09-30 candidates #4, #7, #8: git, vet, the snapshot store and mcp stdio run through one host value instead of package globals; one security-owned guarded HTTP client serves the network tools and mcp; the budget notices' latch becomes the note's own presence.
**Date:** 2026-09-30
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 704514db
**Run order:** after plan 2026-09-30 - 00 and - 02 have closed out; never concurrently with them.
**Sources:** `docs/reviews/architecture-review-2026-09-30.html` (#4, #7, #8); ADR 0008, 0012 (2026-07-26 amendment), 0042, 0077; `docs/plans/archived/2026-09-20 - 01 - test-seams-stubllm-discovery-and-exec-host-plan.md` (execHost unexported, no HostTools field — stands).
**Ratified design calls** (user, 2026-09-30):
- **Engine git:** the tree snapshotter and secrets guard hold an unexported, injectable `gitexec.Host` defaulting to the real OS; no new public `apogee.Config` field.
- **SSRF behaviour:** byte-identical per adapter (pre-flight, dial control, proxy pinning, redirects, transport numbers, refusal wording); the two URL scrubbers keep their distinct algorithms.
**Standing requirements:**
- skills: coding-standards
- Acceptance commands run under the checkout's local test-running rules (`AGENTS.local.md`, if present).
- Windows-tagged tests (`git_windows_test.go`, `terminal_windows_test.go`) do not compile here: keep `gitexec.SafeEnv(root)` and `defaultExecHost()` stable.
**Out of scope:** `subprocess.NewProcessTeardown` and `platform.ProcessWaitDelay` globals (tunable, six users; follow-up); keystore runner (#15); ADR 0008 text; any version identifier. The endpoint pre-flight stays per adapter and three host types remain (deliberate, recorded in item 11).
**Not covered by plans 00–02:** review 2026-09-30 candidates #9, #11, #12, #13, #14, #16, #18, #20 (except its bead) and #17 (plan 00 item 10 only reuses `domain.RecipeLaunch`).
**Regression check:** three rounds (2026-09-30 ×2, 2026-10-01) at 704514db; reports in docs/skill-runs/implement-plan/2026-09-30_-_01_-_exec-host-guarded-client-budget-latch-plan/.

## 1. gitexec: a Host value resolves, runs and scopes env — ✅ DONE (2026-10-01)

**Files:** `internal/gitexec/gitexec.go`, `internal/gitexec/gitexec_test.go`, `internal/gitexec/doc.go`
**Read first:** `internal/gitexec/gitexec.go` — Resolve, Run, queryDiagnosed, probeGit, probeCommandConfig; `internal/security/execsafety.go` — ResolveProgram (nil look → exec.LookPath);
`internal/subprocess/subprocess.go` — RunSubprocess; `internal/tools/exec_host_test.go` — TestNoPackageLevelExecSeamBites (lookY gitexec.LookFunc)
**What:**
**Goal:** `gitexec.Host{Look, Spawn, SpawnTo, Env}` carries `Resolve/Capture/CaptureUnchecked/Run/RunTo/RunDiagnosed/Query/Program/SafeEnv` as methods; every nil field falls back to the real OS, so `OS()` (the zero `Host{}`) is the real OS; a fake Host scripts git outcomes with no real git; the package funcs are thin wrappers over a Host.
**Approach (assumed at the header base):** Fields take the signatures they default to: `Look func(string) (string, error)` (nil → passed as nil to `security.ResolveProgram`, never bypassing the fence), `Spawn`/`SpawnTo` those of `subprocess.RunSubprocess`/`RunSubprocessTo`, `Env platform.Host` (nil → `platform.Current()`).
`probeGit` and `queryDiagnosed` spawn through the Host. Until item 2 the package funcs wrap `Host{Look: <look, else LookPath>, Env: host}`, so `LookPath`, `host` and `withFakeGit` keep working.
Kept: `SafeEnv(root)`'s signature, `LookFunc` (the `TestNoPackageLevelExecSeam` bite fixture), `commandConfigProbes` (host-agnostic key).
**Regression guard.** The spawn fields are `Spawn`/`SpawnTo`: Go rejects a field and a method of one name, and the methods keep `Run`/`RunTo` (items 5 and 11 grep them). The config-probe memo ignores the host, hence the fresh `t.TempDir()` root per scripted config answer.
**Tests:** `TestHost_*` with a fake Spawn (git absent via a fake Look, planted git refused, exit non-zero), each on a fresh `t.TempDir()` root per scripted config answer; `TestHost_ZeroValueIsTheOS` (`Host{}.SafeEnv(root)` equals the base `SafeEnv(root)`).
**Acceptance:** `go test ./internal/gitexec -run 'TestCapture|TestRun|TestCommandConfigRefusal|TestHost'`
**Commit:** `refactor(gitexec): resolve, run and scope env through a Host value`

## 2. gitexec: the package vars go; engine git takes a passed Host — ✅ DONE (2026-10-01)

NOTES (2026-10-01): Program/Resolve keep their `look LookFunc` parameter (the git tools still pass their execHost look until item 3); they wrap a small `lookingHost(look)` — OS() with Look set — so a nil look reaches security.ResolveProgram as nil (exec.LookPath behind the fence, as before).
NOTES (2026-10-01): withFakeGit became `fakeGitHost(path) gitexec.Host` and swapEngineGitLook became `engineGitHost(path) gitexec.Host` (OS() with a fake Look; the real fence and real spawn of the fake-git script stay); the converted tests call the Host's methods. TestRunGitQuery_RefusesAPlantedGit and _NonZeroExitIsAnError now take t.Parallel() since no process-wide var is swapped any more.

**Depends on:** item 1.
**Files:** `internal/gitexec/gitexec.go`, `internal/gitexec/gitexec_test.go`, `internal/tools/exec_host.go`, `internal/tools/exec_host_test.go`, `internal/tools/git.go`, `internal/tools/git_test.go`, `internal/agent/treesnapshot.go`, `internal/snapshot/store.go`
**Read first:** `internal/gitexec/gitexec.go` — LookPath, host; `internal/tools/git.go` — RunGitQuery; `internal/tools/git_test.go` — swapEngineGitLook; `internal/tools/exec_host.go` — execHost;
`internal/snapshot/store.go` — Available; `internal/agent/treesnapshot.go` — treeSnapshotter.git; `internal/gitexec/gitexec_test.go` — withFakeGit
**What:**
**Goal:** the package vars `LookPath` and `host` are gone and the package wrappers run through `OS()`; `tools.RunGitQuery` takes a `gitexec.Host`; `execHost.git()` hands the tools' facilities to gitexec; `withFakeGit`, `swapEngineGitLook` and every test that used them script git through a fake Host.
**Approach (assumed at the header base):** `RunGitQuery(ctx, h gitexec.Host, root, timeout, args...)` calls `h.Run`; `treesnapshot.go` passes `gitexec.OS()`.
`execHost.git() gitexec.Host` maps `h.look` → `Look`, `h.run` → `Spawn`, `h.shell` → `Env`, `SpawnTo` nil (execHost has no runTo). `snapshot.Available` resolves through `gitexec.OS()` (the same `exec.LookPath` as today).
`withFakeGit` and `swapEngineGitLook` are deleted. `TestExecHost_GitMapsItsFacilities` asserts the mapping (`SpawnTo` nil), so `git()` has a user before item 3 (the `unused` linter).
**Regression guard.** Deleting `var LookPath` breaks `snapshot/store.go`'s `Available`, `withFakeGit` and `swapEngineGitLook` in the same commit — all three are in Files.
**Tests:** `TestExecHost_GitMapsItsFacilities`; `withFakeGit` users on a fake Host, each on a fresh `t.TempDir()` root per scripted config answer; the three `TestRunGitQuery_*` inject a fake Host.
**Acceptance:** `go test ./internal/gitexec -run 'TestCapture|TestRun|TestCommandConfigRefusal|TestHost'`; `go test ./internal/tools -run 'TestBuiltinToolsShareOneExecHost|TestNoPackageLevelExecSeam|TestRunGitQuery|TestExecHost_GitMapsItsFacilities'`; `go test ./internal/snapshot`; `! grep -nE '^var (LookPath|host)\b' internal/gitexec/gitexec.go`
**Commit:** `refactor(gitexec): drop the package lookup and host vars; engine git takes a Host`

## 3. tools: git family and vet run through the execHost — ✅ DONE (2026-10-01)

NOTES (2026-10-01): gitRead, gitWrite, stageGitPaths, runGit and runGitUnchecked take the execHost and go through h.git() (Host.Program / Host.Capture / Host.CaptureUnchecked); runGoVet calls h.run. runSubprocess now serves RunHookSubprocess only. gitexec.go untouched — its package-level Program/Resolve(look) wrappers stay for item 5.
NOTES (2026-10-01): consequential edit — internal/tools/exec_host.go: made necessary by the git tools now resolving AND launching through execHost (type doc said "take their git lookup from it"; shellArgv doc said argv[0] is "never handed to runSubprocess").
NOTES (2026-10-01): new tests TestDiagnostics_VetRunsThroughTheHost (fails at base: the recorder sees no spec, the real launcher ran the fake go) and TestGitStatus_LaunchesThroughTheHostRun (scripted h.run: empty answers for the probe's split-stdout specs, a scripted failure for the command; asserts the final spec's argv and dir). TestGit_GracefulWhenAbsent kept unchanged on fakeLookHost.
NOTES (2026-10-01): pre-existing doc drift left alone — internal/tools/terminal.go:140,392, docs/design/confinement-execution-contract.md:1389 and ADR 0056:44 still speak of the `runSubprocess` funnel for the execution tools, which already launched through execHost.run before this run.

**Depends on:** item 2.
**Files:** `internal/tools/git.go`, `internal/tools/git_stage.go`, `internal/tools/file_ops.go`, `internal/tools/delete_file.go`, `internal/tools/diagnostics.go`, `internal/tools/git_test.go`, `internal/tools/git_stage_test.go`, `internal/tools/diagnostics_test.go`, `internal/tools/exec_common.go`, `internal/tools/doc.go`
**Read first:** `internal/tools/git.go` — gitRead, gitWrite, runGit; `internal/tools/git_stage.go` — stageGitPaths; `internal/tools/diagnostics.go` — runGoVet;
`internal/tools/exec_common.go` — runSubprocess; `internal/tools/exec_host_test.go` — capturedRunHost; `internal/tools/git_test.go` — TestGit_GracefulWhenAbsent
**What:** fix — `runGoVet` receives an execHost and ignores it (calls package `runSubprocess`), so a fake host's look hands back a path that is really executed.
**Goal:** every git and vet launch in `tools` reaches the execHost the tool was built with; `capturedRunHost` records the exact git and vet specs.
**Approach (assumed at the header base):** `gitRead`, `gitWrite`, `stageGitPaths`, `runGit`, `runGitUnchecked` take `execHost` (via item 2's `h.git()`) instead of a `look`; `runGoVet` calls `h.run`. `runSubprocess` stays for `RunHookSubprocess` only.
**Regression guard.** Comments naming `runSubprocess`'s in-package callers or a git `look` parameter match the tree — `grep -n 'runSubprocess\|look gitexec\|host\.look\|execHost look' internal/tools/*.go` (at base `exec_common.go`, `doc.go`).
The absent-git test is `TestGit_GracefulWhenAbsent` (no `TestGit_AbsentGitIsReported` exists). The scripted-outcome test uses its own `h.run`, not `capturedRunHost` (an empty success, last spec only).
**Tests:** `TestDiagnostics_VetRunsThroughTheHost` (fails at base); `TestGit_GracefulWhenAbsent` via `fakeLookHost` (kept); one scripted git outcome via a test-set `h.run`, asserting the final spec's argv.
**Acceptance:** `go test ./internal/tools -run 'Git|Stage|DeleteFile|MoveFile|Diagnostics|TestBuiltinToolsShareOneExecHost|TestNoPackageLevelExecSeam'`
**Commit:** `fix(tools): git and go vet run through the tool's exec host`

## 4. agent: engine git through an injectable gitexec.Host — ✅ DONE (2026-10-01)

NOTES (2026-10-01): two unexported fields, both defaulted to gitexec.OS() in newAgent's shared literal: treeSnapshotter.host (passed by newTreeSnapshotter(root, host); the field is `host` because the snapshotter already has a `git` method) and Agent.gitHost for the secrets guard; scanStagedSecrets/newShadowIndex/runShadowGit take the Host and call host.Resolve / host.Query. background.go's backgroundHost literal is untouched (zero Host is the OS). gofmt re-aligned the trailing comments of the neighbouring tokens/prompts/tasks lines in construct.go.
NOTES (2026-10-01): tests — driveToolCallWith(t, cfg, sink, setup, ...) added (driveToolCall delegates with nil); withEngineGit(host) sets both fields; scriptedGit fake Host answers gitexec's config probe as a clean repo and matches the command after the hardening `-c` pairs (gitCommand). TestTreeSnapshot_GitRunsThroughTheFunnel and _PlantedGitTurnsTheFloorOff no longer swap PATH (the planted test now takes t.Parallel); writeFakeGit, writeSleepingGit and countShadowGit are deleted (countingGit wraps the real launcher); the incomplete-scan cases use a wedged Spawn (blocks until the budget's ctx expires) and a Spawn failing `diff` with exit 128; TestCommitSecretsSkipsInPlanMode and _HonoursStricterTextVerdict now take t.Parallel. lowerCommitSecretsTimeout and t.Setenv(APOGEE_API_KEY) stay, so those tests remain serial.

**Depends on:** item 3.
**Files:** `internal/agent/treesnapshot.go`, `internal/agent/secretsguard.go`, `internal/agent/treesnapshot_test.go`, `internal/agent/secretsguard_test.go`, `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/guardrails_test.go`
**Read first:** `internal/agent/secretsguard.go` — shadowGitQuery, scanStagedSecrets; `internal/agent/treesnapshot.go` — treeSnapshotter.git; `internal/agent/construct.go` — newAgent literal (tree:);
`internal/agent/secretsguard_test.go` — countShadowGit, lowerCommitSecretsTimeout; `internal/agent/guardrails_test.go` — driveToolCall; `internal/agent/background.go` — backgroundHost literal
**What:**
**Goal:** the tree snapshotter and the secrets guard run git only through an unexported `gitexec.Host` field defaulting to `gitexec.OS()`; the `shadowGitQuery` package var is gone; tests inject a fake Host.
**Approach (assumed at the header base):** `tools.RunGitQuery` already takes a `gitexec.Host` (item 2); `treeSnapshotter.git` (sole caller, built by `newTreeSnapshotter` in `construct.go`) passes its field instead of `gitexec.OS()`.
**Regression guard.** The secrets guard has no struct (`Agent` methods plus the free `scanStagedSecrets`), so its Host field is on `Agent`, defaulted in `newAgent`'s shared literal (`construct.go`) so delegates get it.
Secrets tests inject via a `driveToolCall` variant taking a `func(*Agent)` setup hook; the fake Spawn matches `diff` in `spec.Argv` after the hardening `-c` options. `backgroundHost` (`background.go`) and test Agent literals omitting the field stay safe: a zero `gitexec.Host` is the OS.
No git-seam swaps (`shadowGitQuery`, PATH); `lowerCommitSecretsTimeout` and `t.Setenv(APOGEE_API_KEY)` stay, so those tests remain non-parallel.
**Tests:** `TestTreeSnapshot_*` and secrets-guard tests on a fake Host, no git-seam swaps (`lowerCommitSecretsTimeout` and `t.Setenv(APOGEE_API_KEY)` stay).
**Acceptance:** `go test ./internal/agent -run 'TreeSnapshot|Secrets'`; `go test ./internal/tools -run 'RunGitQuery'`; `! grep -n 'shadowGitQuery' internal/agent/*.go`
**Commit:** `refactor(agent): engine git runs through an injectable gitexec host`

## 5. snapshot: the store holds its Host; gitexec's OS wrappers go — ✅ DONE (2026-10-01)

NOTES (2026-10-01): signatures take the host right after ctx — Available(host), Open(ctx, host, dir, workspace), OpenJournal(ctx, host, home, sessionID, workspace, enabled), OpenStored(ctx, host, home, sessionID); Store keeps it in an unexported `git` field. run.go, wire_live.go and undo.go pass gitexec.OS().
NOTES (2026-10-01): with Program/Resolve gone, the unexported lookingHost helper was dead and is deleted; LookFunc lost its last production use, so Host.Look is now typed LookFunc (same underlying type; internal/tools/exec_host_test.go's fixture still names it). The deleted wrappers' doc comments moved onto the Host methods, replacing the "is the package-level X" one-liners.
NOTES (2026-10-01): new test TestAStoreOnAHostWithNoGitIsUnavailableAndNeverSpawns (store_test.go) runs Available and Open on a fake Host whose lookup finds no git, in parallel with no PATH edit. The two PATH-clearing journal tests (TestOpenJournalReportsAnAbsentGit, TestOpenStoredReportsAnAbsentGit) were left as they are, passing gitexec.OS().
NOTES (2026-10-01): consequential edit — internal/snapshot/doc.go: made necessary by removing gitexec.RunTo (the reads now stream through the Store's Host.RunTo)
NOTES (2026-10-01): consequential edit — internal/tools/git.go: made necessary by removing gitexec.Capture (two comments now name gitexec.Host.Capture)
NOTES (2026-10-01): consequential edit — internal/agent/dispatch.go: made necessary by removing gitexec.Resolve (comment now names gitexec.Host.Resolve)

**Depends on:** item 4.
**Files:** `internal/snapshot/store.go`, `internal/snapshot/journal.go`, `internal/snapshot/store_test.go`, `internal/run/run.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/undo.go`, `cmd/apogee/undo_test.go`, `internal/gitexec/gitexec.go`, `internal/snapshot/journal_test.go`, `internal/run/run_test.go`, `internal/agent/undo_group_test.go`, `cmd/apogee/wire_live_test.go`, `internal/gitexec/gitexec_test.go`
**Read first:** `internal/snapshot/store.go` — Available, Open, Store; `internal/snapshot/journal.go` — OpenJournal, OpenStored; `internal/run/run.go` — openRecordJournal;
`cmd/apogee/wire_live.go` — rootWiring.openSessionJournal; `cmd/apogee/undo.go` — runUndoVerb
**What:**
**Goal:** `snapshot.Store` holds a `gitexec.Host` passed to `Available`, `Open`, `OpenJournal`, `OpenStored` by their callers; no package-level `gitexec.Run/RunTo/RunDiagnosed/Query/Resolve/Program/Capture/CaptureUnchecked` wrapper remains.
**Approach (assumed at the header base):** Callers (`run.go`, `wire_live.go`, `undo.go`) pass `gitexec.OS()`; test callers pass it or a fake. `SafeEnv(root)` stays. Reword `store.go`'s "(gitexec.Run)" comment (above `RunTo`'s caller) to the Store's Host method.
**Regression guard.** Every test caller is updated (`journal_test.go`, `run_test.go`, `undo_group_test.go`'s `snapshotAgent`, `wire_live_test.go`'s `requireSnapshotStore`); `gitexec_test.go`'s real-git `TestCommandConfigRefusal_JudgesTheRepositoryTheRunActuallyReaches` and its `Capture` callers use `gitexec.OS()`.
**Tests:** existing snapshot/journal/undo tests; one store test on a fake Host (git absent → unavailable); `gitexec_test.go` callers on `gitexec.OS()`.
**Acceptance:** `go test ./internal/snapshot`; `go test ./internal/run -run 'Snapshot|Journal|Undo|Store'`; `go test ./cmd/apogee -run 'Undo|Rotate'`; `go test ./internal/agent -run 'TestSnapshotsCoverAWriteTheFunnelNeverSaw|TestReadOnlyCallTakesNoPreImage|TestExchangeEndClosesTheGroupOnEveryRowThatEndsOne|TestChildExchangeEndLeavesTheParentGroupOpen'`; `go test ./internal/gitexec -run 'TestCommandConfigRefusal|TestCapture'`; `! grep -rnE 'gitexec\.(Run|RunTo|RunDiagnosed|Query|Resolve|Program|Capture|CaptureUnchecked)\(' --include=*.go internal cmd`; `! grep -nE '^func (Run|RunTo|RunDiagnosed|Query|Resolve|Program|Capture|CaptureUnchecked)\(' internal/gitexec/gitexec.go`
**Commit:** `refactor(snapshot): the store runs git through the host it was opened with`

## 6. security: one guarded HTTP client; network tools adopt it — ✅ DONE (2026-10-01)

NOTES (2026-10-01): `security.URLGuard.GuardedClient(ctx, target, GuardedClientOptions{Proxy, Timeout, Policy})` carries both floor policies now — `DialFloor` (tools: blanket SafeDialControl, proxy pinned) and `DialPinDestination` (destination + proxy pinned, PinnedDialControl's union, for item 7's mcp adoption) — tested by a pin-destination row; no transport wrapper/origin pin (item 7's).
NOTES (2026-10-01): typed refusals are `security.ErrProxyUnusable` (wraps ErrURLBlocked) and `*security.PinError{Hosts, Err}`; tools' `newHTTPClient` stays as the thin adapter that calls the builder and rewords both into the base text, so `TestNewHTTPClient_UnusableOrUnpinnableProxyRefusesTheCall` is unchanged; the typed-error assertions live in `TestGuardedClient_BuildsTheVettedDestinationsClient` (one table: no-proxy, proxy, pin-destination, unusable, unpinnable, redirect, field set incl. ForceAttemptHTTP2).
NOTES (2026-10-01): `TestNetworkTool_ProxyRefusalWordingsAreExact` was written first and passed at the unmodified base before the builder moved; network_funnel_test.go only gets a comment pointer to security.GuardedClient. mcp's `newGuardedHTTPClient` comment ("the two builders are deliberately NOT consolidated") is left for item 7, which replaces that builder.

**Files:** `internal/security/httpclient.go`, `internal/security/httpclient_test.go`, `internal/security/doc.go`, `internal/tools/network.go`, `internal/tools/network_test.go`, `internal/tools/network_funnel_test.go`
**Read first:** `internal/tools/network.go` — networkTool.do, newHTTPClient, blockedMessage; `internal/security/ssrf.go` — PinnedDialControl, SafeDialControl;
`internal/tools/network_test.go` — TestNewHTTPClient_UnusableOrUnpinnableProxyRefusesTheCall; `internal/tools/network_funnel_test.go` — TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses;
`internal/mcp/transport.go` — newGuardedHTTPClient (the transport field set to match)
**What:**
**Goal:** `internal/security` builds "a client for this vetted destination" from proxy resolution on (proxy + pinned/blanket dial control → fixed transport → no redirects), parameterised by per-call timeout and floor policy (tools: floor on, `SafeDialControl` / proxy pin); the pre-flight stays with each adapter; `tools` builds its client only through the builder, and its unusable-proxy and unpinnable-proxy refusals are byte-identical to the base tree's, pinned by exact-string tests.
**Approach (assumed at the header base):** First, at base, add `TestNetworkTool_ProxyRefusalWordingsAreExact` (`network_test.go`: drive `networkTool.do`, assert the whole message for both proxy refusals). Then move `newHTTPClient`'s recipe from proxy resolution on beside `URLGuard`.
`networkTool.do` keeps normalisation, the pre-flight `CheckContext` under the one M-5 budget (`rctx`), its `ctx.Err()`-vs-`blockedMessage` split for pre-flight and builder errors, and `CloseIdleConnections`; mcp's `checkEndpoint` (also used alone by `Admit`) stays mcp's.
Builder errors are typed (`ProxyUnusable`, pin failure with host); tools words them `%w: the configured egress proxy is not a usable URL` (never the proxy value) and `egress proxy HOST could not be pinned: %w`.
**Regression guard.** The transport field set of both base builders stays: `Proxy`, `DialContext` (10s dialer + control), `ForceAttemptHTTP2: true`, `MaxIdleConns` 10, `IdleConnTimeout` 30s, `TLSHandshakeTimeout` 10s, `ExpectContinueTimeout` 1s.
Only the typed-error half of `TestNewHTTPClient_UnusableOrUnpinnableProxyRefusesTheCall` moves to security (named to match `GuardedClient`); a tools-side `TestNewHTTPClient_*` keeps both wordings, `ErrURLBlocked` and no `hunter2`. Session lifetime, origin pin and inner wrapper are item 7's.
**Tests:** `TestNetworkTool_ProxyRefusalWordingsAreExact` (written first, green at base); a security table over proxy/no-proxy/unusable/unpinnable/redirect plus a `ForceAttemptHTTP2` row; the tools-side proxy-wording test; `TestWebFetch_*`, `TestNetworkTools_FailureMessagesDoNotLeakKey`, `TestNetworkFunnel_*` green.
**Acceptance:** `go test ./internal/security -run 'GuardedClient|DocMap|PinnedDialControl|SafeDialControl'`; `go test ./internal/tools -run 'WebFetch|HTTPRequest|WebSearch|NetworkTools|BlockedMessage|NetworkFunnel|RedactSubstring|NewHTTPClient|ProxyRefusalWordings'`
**Commit:** `refactor(security): own the guarded HTTP client; network tools use it`

## 7. mcp: the HTTP transports use the security client; proxyForRequest goes — ✅ DONE (2026-10-01)

NOTES (2026-10-01): `TestVetEndpoint_RefusalWordingsAreExact` (4 rows) and `TestOriginPin_RefusalWordingIsExact` were written first and passed at the unmodified tree (serial `proxyForRequest` swap); after the move they inject `Host.Proxy` and run `t.Parallel`.
NOTES (2026-10-01): security gains `GuardedClientOptions.OriginRefusal` (non-nil turns the origin pin on; the caller's error is returned verbatim) and `GuardedClientOptions.WrapTransport` (the layer between pin and dial, where mcp puts `boundedBodyTransport`), plus `ErrNoOrigin`, `CanonicalOrigin` and `OriginPinTransport{Origin, Refusal, Next}`; option names are this item's choice.
NOTES (2026-10-01): mcp maps GuardedClient refusals in a new `endpointRefusal` helper (ErrProxyUnusable / ErrNoOrigin / *PinError → pinErr.Err); its default branch (unreachable today) reuses the "endpoint blocked by url-safety" wording.
NOTES (2026-10-01): added `TestGuardedClient_PinsRequestsToTheDestinationsOrigin` in security (same-origin through the wrapped layer, cross-origin refused with the caller's error, hostless destination → ErrNoOrigin); `TestCanonicalOrigin_ComparesSchemeHostAndPort` moved there unchanged but for the exported name.
NOTES (2026-10-01): test helper `endpointClient` gained a `Host` parameter (all callers pass `Host{}` except the proxied test); the three proxy tests now inject `Host.Proxy` and run `t.Parallel`.
NOTES (2026-10-01): consequential edit — internal/mcp/doc.go: trust-boundary paragraph named `originPinTransport`, now `security.OriginPinTransport`.
NOTES (2026-10-01): consequential edit — docs/design/mcp-client.md: named `originPinTransport`; also records the shared GuardedClient recipe and the `Host` / `ConnectWith` surface.

**Depends on:** item 6.
**Files:** `internal/mcp/transport.go`, `internal/mcp/client.go`, `internal/mcp/transport_test.go`, `internal/mcp/doc.go`, `internal/security/httpclient.go`, `internal/security/httpclient_test.go`, `docs/design/mcp-client.md`, `internal/mcp/mcp_test.go`
**Read first:** `internal/mcp/transport.go` — vetEndpoint, newGuardedHTTPClient, originPinTransport.RoundTrip, canonicalOrigin, proxyForRequest; `internal/mcp/client.go` — Connect;
`internal/mcp/transport_test.go` — beneathOriginPin, TestVetEndpoint_TheEgressProxyComesFromTheEnvironment
**What:**
**Goal:** mcp's SSE and streamable transports get their client from the security builder (floor policy "pin destination" at dial time, session lifetime, origin pin outermost, `boundedBodyTransport` below it); an `mcp.Host{Proxy}` passed via `ConnectWith` replaces the `proxyForRequest` var; `Connect` wraps it with the real host; mcp's four HTTP refusal wordings are byte-identical to the base tree's, pinned by exact-string tests; `newGuardedHTTPClient` no longer exists.
**Approach (assumed at the header base):** First, at base, add `TestVetEndpoint_RefusalWordingsAreExact` (rows: unusable proxy; unpinnable proxy; `http://:8080/mcp` under `guard.DisableIPFloor()`; the same endpoint floor on, reading `mcp: server %q endpoint blocked by url-safety: security: url blocked by url-safety: no host to pin`) and `TestOriginPin_RefusalWordingIsExact` (a cross-origin request through the guarded client).
Preserved: `mcp: server %q: %w: the configured egress proxy is not a usable URL`, `mcp: server %q endpoint blocked by url-safety: %w`, `mcp: server %q: %w: the endpoint has no origin to pin` (`vetEndpoint`), `mcp: server %q: %w: a request left the configured endpoint's origin` (`originPinTransport.RoundTrip`).
`originPinTransport` and `canonicalOrigin` move to security (`security.CanonicalOrigin`); mcp passes its wording in. Builder order stays proxy resolution → `PinnedDialControl` → `CanonicalOrigin`; an origin-less endpoint is a typed error `vetEndpoint` words as `the endpoint has no origin to pin`, so the floor-on row keeps the pin wording.
Only the dial-time endpoint pin is the floor-policy parameter (ADR 0012 2026-07-26 amendment); the floor-off pre-flight stays mcp's `checkEndpoint` (`DisableIPFloor` + host lists); `Admit` checks with no client, no DNS; no `Client.Timeout`.
Delete `newGuardedHTTPClient` and its "deliberately NOT consolidated" comment; reword `transport_test.go`'s "reproduced field-for-field from the native funnel" comment (`TestGuardedClient_DoesNotFollowRedirects`) to the shared builder.
**Regression guard.** The pin-failure `%w` wraps the inner `PinnedDialControl` error, never security's typed wrapper text; the other three wrap `security.ErrURLBlocked` as at base. The security origin pin exposes its inner transport as an exported `Next` field; `beneathOriginPin` and the bounded-body subtest are rewritten against it.
A nil `Host.Proxy` means `http.ProxyFromEnvironment`. net/http memoises the proxy environment, so every row of both wording tests injects its own resolver (at base a serial `proxyForRequest` swap returning nil, nil; then `Host.Proxy`) and `TestOriginPin_RefusalWordingIsExact` is `t.Parallel` or injects.
The three proxy tests inject `Host.Proxy`; only `TestVetEndpoint_TheEgressProxyComesFromTheEnvironment` reads the real environment (serial, passes `Host{}`), and its "every other proxy test here swaps the seam" comment is reworded to `Host.Proxy`.
**Tests:** `TestVetEndpoint_RefusalWordingsAreExact` and `TestOriginPin_RefusalWordingIsExact` (written first, green at base); all `TestGuardedClient_*`, `TestVetEndpoint_*`, `TestBuildTransport_*`, `TestConnect_SSE*` green; `TestCanonicalOrigin_*` moves to security with `canonicalOrigin`; every `buildTransport` call in `mcp_test.go` updated.
**Acceptance:** `go test ./internal/mcp -run 'TestGuardedClient|TestVetEndpoint|TestOriginPin|TestBuildTransport|TestCanonicalOrigin|TestConnect_SSE'`; `go test ./internal/security -run 'GuardedClient|Origin'`; `! grep -n 'proxyForRequest' internal/mcp/*.go`; `! grep -n newGuardedHTTPClient internal/mcp/*.go`
**Commit:** `refactor(mcp): build the HTTP transport client through security`

## 8. mcp: stdio gets its host from the composition root — ✅ DONE (2026-10-01)

NOTES (2026-10-01): wire_live.go builds the real host in a small `liveMCPHost()` helper (Proxy: http.ProxyFromEnvironment, Shell: platform.Current(), NewTeardown: platform.NewProcessTeardown) so both Connect sites and wire_settings_test.go's "production recipe" closure make the identical `mcp.ConnectWith(…, liveMCPHost(), …)` call; `mcp.Connect` stays as the zero-host convenience the package's own tests use.
NOTES (2026-10-01): zero-field defaults resolve in `Host.withStdioDefaults()`, applied at the top of buildStdioTransport, so tests calling buildTransport with `Host{}` keep working; stdioTransport.Connect's own non-positive fallback is kept for hand-built transports.

**Depends on:** items 5, 7.
**Files:** `internal/mcp/transport.go`, `internal/mcp/client.go`, `internal/mcp/mcp_test.go`, `cmd/apogee/wire_live.go`, `docs/design/mcp-client.md`, `cmd/apogee/wire_settings_test.go`
**Read first:** `internal/mcp/transport.go` — buildStdioTransport, newStdioTeardown, stdioHost, stdioTerminateDuration; `internal/mcp/client.go` — connectOne;
`cmd/apogee/wire_live.go` — rootWiring.wireSession (mcp.Connect, the newLiveMCP closure); `cmd/apogee/wire_settings_test.go` — TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce; `internal/mcp/mcp_test.go` — recordStdioTeardowns
**What:**
**Goal:** `mcp.Host` also carries `Shell platform.Host`, `NewTeardown` and `TerminateDuration`; stdio transports use it; the `newStdioTeardown`, `stdioHost` and `stdioTerminateDuration` package vars are gone; `wire_live.go` passes the host.
**Approach (assumed at the header base):** Extend item 7's `mcp.Host`; `connectOne` → `buildTransport` → `buildStdioTransport` thread it; `wire_live.go`'s two `Connect` sites call `ConnectWith` with the real platform host.
**Regression guard.** Zero `mcp.Host` fields default to the real ones (`Shell`→`platform.Current()`, `NewTeardown`→`platform.NewProcessTeardown`, `TerminateDuration`≤0→5s), so a test setting only `TerminateDuration` cannot nil-panic.
`recordStdioTeardowns` and its two callers pass a Host whose `NewTeardown` wraps `platform.NewProcessTeardown`; `wire_settings_test.go`'s "production recipe (wire_live.go)" closure makes the same `ConnectWith` call as `wire_live.go`.
**Tests:** `TestClose_BoundsTheDrainOfAWedgedStdioServer`, `TestBuildStdioTransport_CancelArmsTheCmdsTeardown`, and `recordStdioTeardowns`' callers `TestConnect_StdioContainPrecedesTheHandshake`, `TestConnect_FailedStdioHandshakeReapsAContainedTree` inject via Host instead of swapping vars.
**Acceptance:** `go test ./internal/mcp -run 'TestClose_BoundsTheDrain|TestBuildStdioTransport|TestConnect'`; `go test ./cmd/apogee -run 'MCP|Mcp'`; `! grep -nE 'newStdioTeardown|stdioHost|stdioTerminateDuration' internal/mcp/*.go`
**Commit:** `refactor(mcp): stdio servers run through a host from the composition root`

## 9. security: the network tools' URL scrubbers live beside the client — ✅ DONE (2026-10-01)

**Depends on:** item 6.
**Files:** `internal/security/urlscrub.go`, `internal/security/urlscrub_test.go`, `internal/security/doc.go`, `internal/tools/network.go`, `internal/tools/web_search.go`, `internal/tools/network_funnel_test.go`, `internal/tools/doc.go`
**Read first:** `internal/tools/network.go` — safeHost, scrubURLError, redactRequestURL, redactSubstring; `internal/tools/web_search.go` — WebSearch.Execute (endpointHost, scrubURLError);
`internal/tools/network_funnel_test.go` — TestRedactSubstring_StripsTheQuotedFormToo; `internal/tools/web_search_redaction_test.go` — secretKey; `internal/tools/doc.go` — the redact.go paragraph naming redactRequestURL
**What:**
**Goal:** `redactSubstring` (substring plus `%q` form), `scrubURLError` and `safeHost` live in `internal/security` (`urlscrub.go`) as `RedactSubstring`, `ScrubURLError` and `SafeHost` with behaviour unchanged, and `redactRequestURL` moves there unexported beside `ScrubURLError`, its only caller; `tools` calls them.
**Approach (assumed at the header base):** Move the exact-URL substring algorithm as it is. No merge with mcp's origin redactor (item 10).
**Regression guard.** Comments naming `redactRequestURL`, `redactSubstring`, `scrubURLError` or `safeHost` by package or file are updated (at base `tools/doc.go`) — `grep -rn 'redactRequestURL\|redactSubstring\|scrubURLError\|safeHost' internal/`. The moved `TestRedactSubstring_StripsTheQuotedFormToo` declares its own secret constant (`secretKey` is tools-only).
**Tests:** `TestRedactSubstring_*` moved to security; `TestNetworkTools_FailureMessagesDoNotLeakKey` and `TestWebSearch_*` green.
**Acceptance:** `go test ./internal/security -run 'Scrub|Redact|DocMap'`; `go test ./internal/tools -run 'Redact|BlockedMessage|FailureMessages|WebSearch|DocMap'`
**Commit:** `refactor(security): the network tools' URL scrubbers live beside the guarded client`

## 10. security: mcp's origin redactor lives beside the client — ✅ DONE (2026-10-01)

NOTES (2026-10-01): realURLErrorText moved to the security test rather than copied — after the move mcp has no caller left, so a copy would be dead code; refusedAddr is copied (mcp's connect test still uses it).
NOTES (2026-10-01): redactedError (the chain-keeping error RedactErr returns) moved with the redactor into security/urlscrub.go; the moved test gained an "endpoint with no host is the identity" subtest for NewOriginRedactor's nil return, which the mcp-side stdio subtest no longer reaches.
NOTES (2026-10-01): tool.go's serverTool field comments realigned by gofmt after the redactor field's type name grew.

**Depends on:** items 7, 9.
**Files:** `internal/security/urlscrub.go`, `internal/security/urlscrub_test.go`, `internal/security/doc.go`, `internal/mcp/transport.go`, `internal/mcp/tool.go`, `internal/mcp/client.go`, `internal/mcp/transport_test.go`, `internal/mcp/doc.go`
**Read first:** `internal/mcp/transport.go` — endpointRedactor, newEndpointRedactor; `internal/mcp/client.go` — connectOne, listServerTools; `internal/mcp/tool.go` — serverTool.redactor;
`internal/mcp/transport_test.go` — TestEndpointRedactor_CutsTheEndpointToItsOrigin, refusedAddr, realURLErrorText
**What:**
**Goal:** the origin endpoint redactor lives in `internal/security` as `OriginRedactor`, built by `NewOriginRedactor(endpoint string)`, with behaviour unchanged; mcp keeps a thin `newEndpointRedactor(cfg)` returning `*security.OriginRedactor` (nil for stdio).
**Approach (assumed at the header base):** Move the origin-regex algorithm as it is; `serverTool.redactor` and `listServerTools` take `*security.OriginRedactor`. No merge with the exact-URL scrubber (item 9).
**Regression guard.** The constructor takes the endpoint string (security cannot import `mcp.ServerConfig`); the "stdio and nil are the identity" subtest stays in mcp as `TestEndpointRedactor_StdioAndNilAreTheIdentity`; `refusedAddr`/`realURLErrorText` are copied into the security test. Comments naming `endpointRedactor` by package or file are updated — `grep -rn 'endpointRedactor' internal/`.
**Tests:** `TestOriginRedactor_CutsTheEndpointToItsOrigin` in security; `TestEndpointRedactor_StdioAndNilAreTheIdentity`, `TestConnect_RedactsTheEndpointFromARefusedConnect`, `TestExecute_RedactsTheEndpointWhenTheServerDies` green in mcp.
**Acceptance:** `go test ./internal/security -run 'Redact|DocMap'`; `go test ./internal/mcp -run 'Redact'`
**Commit:** `refactor(security): mcp's origin redactor lives beside the guarded client`

## 11. docs: exec host and guarded client — ✅ DONE (2026-10-01)

NOTES (2026-10-01): internal/snapshot/doc.go, internal/agent/dispatch.go and internal/tools/git.go needed no edit — items 3, 4 and 5 had already brought their comments onto gitexec.Host (the removed-function grep was clean before this item).
NOTES (2026-10-01): ADR 0012 (c)'s "reproduced the funnel's builder field-for-field" reworded to past-tense history ("was then built by a copy of the network funnel's client recipe") so the guard grep stays clean; the decision text is untouched and the dated note carries the consolidation.
NOTES (2026-10-01): pre-existing — internal/gitexec/doc.go says all five entry points "take an env the caller appends", but Host.Capture takes none (only CaptureUnchecked does); left as is.

**Depends on:** items 5, 8, 10.
**Files:** `docs/adr/0042-external-programs-are-optional-enhancements-never-prerequisites.md`, `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`, `docs/design/confinement-execution-contract.md`, `internal/tools/doc.go`, `internal/gitexec/doc.go`, `internal/snapshot/doc.go`, `internal/agent/dispatch.go`, `internal/tools/git.go`, `internal/tools/run_tests.go`
**Read first:** `internal/gitexec/doc.go` — package doc links [Resolve] [Program] [Capture] [Run] [Query] [RunTo] [RunDiagnosed]; `internal/tools/git.go` — RunGitQuery, runGit (doc comments);
`internal/tools/run_tests.go` — RunTests.Execute (env comment over subprocessEnv); ADR 0012 — Amendment (2026-07-26) (c);
ADR 0042 — Consequences (resolution seam); `internal/snapshot/doc.go` — Reads paragraph; `internal/agent/dispatch.go` — executeTool floorCtx comment
**What:**
**Goal:** every doc naming a removed global or the two-builder client shape matches the tree: gitexec runs through a passed Host; the guarded client is security's; ADR 0012's dated note records that only the dial-time endpoint pin became the client's floor-policy parameter while the floor-off pre-flight (`checkEndpoint`'s `DisableIPFloor`) stays in mcp; `run_tests.go`'s env comment names plain `subprocessEnv` without claiming terminal's and python_exec's environment.
**Approach (assumed at the header base):** Dated notes (`> **Note 2026-09-30 …**`) in ADR 0042 and 0012, never rewriting decisions; ADR 0008 untouched.
Sweep rule: `grep -rnE 'gitexec\.LookPath|swapEngineGitLook|withFakeGit|shadowGitQuery|stdioHost|newStdioTeardown|stdioTerminateDuration|proxyForRequest|newGuardedHTTPClient|builds the same shape|NOT consolidated' internal docs --include='*.go' --include='*.md'` — fix every site outside `docs/plans`, `docs/reviews`, archives.
`run_tests.go` uses plain `subprocessEnv` (review 2026-09-30 in-passing defect); terminal and python_exec use `subprocessEnvScopedPath`.
**Regression guard.** Removed-function grep: `grep -rnE 'gitexec\.(Run|RunTo|RunDiagnosed|Query|Resolve|Program|Capture|CaptureUnchecked)\b' internal docs CONTEXT.md` (at base `snapshot/doc.go`, `agent/dispatch.go`, `tools/git.go`), plus `gitexec/doc.go`'s `[Resolve]` `[Program]` `[Run]` `[Query]` `[RunTo]` `[RunDiagnosed]` links.
Docs describing gitexec's package-var lookup seam or mcp's own client builder are fixed too: `grep -rniE 'package var|funnel.s builder|field-for-field' docs/adr docs/design internal/*/doc.go` (at base ADR 0042, ADR 0012).
The ADR 0012 note records review #7's narrowing as deliberate, not deferred: the pre-flight (`checkEndpoint`, `DisableIPFloor` included) and URL/origin redaction stay per adapter, only the dial-time pin moves into the security client, three host types remain.
**Tests:** none (docs).
**Acceptance:** the sweep grep and the removed-function grep return no site outside `docs/plans`, `docs/reviews` and archived files; `! grep -n 'environment terminal and python_exec run in' internal/tools/run_tests.go`; `go test ./internal/tools -run DocMap`
**Commit:** `docs: record the exec host and guarded client consolidation`

## 12. agent: a budget note's latch is its own presence

**Files:** `internal/agent/stepnotice.go`, `internal/agent/agent.go`, `internal/agent/prune.go`, `internal/agent/compact.go`, `internal/agent/construct.go`, `internal/agent/stepnotice_test.go`, `internal/agent/turn.go`
**Read first:** `internal/agent/stepnotice.go` — stepBudgetNotice, tokenBudgetNotice, rearmNotices; `internal/domain/hooks.go` — Conversation.HasEngineNote; `internal/agent/construct.go` — buildAgent observer comment;
`internal/agent/turn.go` — settle, observer field doc; `internal/agent/stepnotice_test.go` — TestStepNoticeReArmsAfterARollback
**What:**
**Goal:** the step and token budget notices fire iff depth ≥ 1, cap > 0, the threshold is reached and `a.conv.HasEngineNote(topic)` is false; `stepNoticeAt`, `stepNoticeLive`, `tokenNoticeAt`, `tokenNoticeLive`, `rearmStepNotice`, `rearmTokenNotice` and `rearmNotices` no longer exist.
**Approach (assumed at the header base):** Check the threshold first so the scan runs only past it; delete the prune (`autoPrune`) and fold (`foldFor`) re-arm calls; `turnRolledBack` calls `rearmFillNotice` directly (the fill ladder is a rung, not a note — unchanged). No reachable behaviour change: an abort never drops a noted result at depth ≥ 1 (settle aborts only an Exchange with no tool result past exchangeStart).
**Regression guard.** `turn.go`'s observer field doc and `endCancelled` comment ("observer.turnRolledBack → rearmNotices") are reworded to `rearmFillNotice`. Separate-latch prose is reworded too — `grep -rnE "(step|token)-budget (notice|note)s?'s? latch|budget notices' latch" internal` (at base `turn.go`, `construct.go`'s `buildAgent`, `compact.go`, `prune.go`).
**Tests:** rewrite the field-peeking lines only: `…ReArmsAfterARollback` drops the noted message via `a.conv.DropRange`; `…IsToldAgainAfterAPruneStubbedIt` asserts behaviour, not `*NoticeLive`; once-per-life, many-call-Turn and fold tests unchanged.
**Acceptance:** `go test ./internal/agent -run 'TestStepNotice|TestTokenNotice|TestContextFillNotice|TestAutoPrune|TestTurnLifecycle'`; `go test ./internal/domain -run 'EngineNote'`; `! grep -nE 'NoticeLive|NoticeAt|rearmStepNotice|rearmTokenNotice|rearmNotices' internal/agent/*.go`
**Commit:** `refactor(agent): a budget note's latch is the note's own presence`

## 13. docs: no comment names a parallel budget latch

**Depends on:** item 12.
**Files:** `docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md`, `internal/agent/fillnotice.go`, `internal/agent/dispatch.go`, `internal/domain/hooks.go`, `CONTEXT.md`, `internal/agent/stepnotice.go`, `internal/agent/construct.go`
**Read first:** `docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md` — token-budget twin addendum ("same latch and re-arm seams"); `CONTEXT.md` — token-budget notice paragraph;
`internal/agent/stepnotice.go` — package header comment; `internal/domain/hooks.go` — Conversation.HasEngineNote doc; `internal/agent/construct.go` — buildAgent observer comment
**What:**
**Goal:** no comment or ADR text describes a step/token notice latch or re-arm seam separate from the note's presence.
**Approach (assumed at the header base):** A dated note in ADR 0077 on the "same latch and re-arm seams" sentence; reword `HasEngineNote`'s doc and any `fillnotice.go`/`dispatch.go` comment naming the latch (`turn.go` is item 12's). Sweep rule: `grep -rnE "\b(step|token)Notice(At|Live)\b|rearm(Step|Token)Notice\b|rearmNotices\b|same latch and re-arm|(step|token)-budget (notice|note)s?'s? latch|budget notices' latch" internal docs/adr CONTEXT.md`.
**Regression guard.** A bare `NoticeAt|same latch` hits sites that must stay (`tui/contextfiles_test.go`, `agent/seat_test.go`, `tui/doc.go`, `tui/command.go`, `tui/model_test.go`, ADR 0029). `CONTEXT.md`'s paragraph and the `stepnotice.go` header are reworded here; the annotated ADR 0077 sentence is the grep's one allowed hit; a `construct.go` hit left after item 12 is reworded here.
**Tests:** none (comments/docs).
**Acceptance:** the sweep grep returns nothing outside archived material and the annotated ADR 0077 sentence; `go vet ./internal/agent ./internal/domain`
**Commit:** `docs: the budget notices' latch is the note itself`
