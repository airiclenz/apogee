# winlabel journal trust and the landlock UDP proof — implementation plan

**Goal:** A Windows confinement journal entry acts only on the exact object it labelled, and its owner reads as alive only while the process that wrote it is running, so a recycled PID doesn't count. A Low child can never reach the journal directory. CI proves that confinetest row #13's landlock UDP arm runs, where today it may silently skip.
**Date:** 2026-09-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** ea51204c
**Closes:** apogee-winlabel-journal-secret

**Regression check (2026-09-23, ea51204c):**
- 1: guard folded (a failed identity read journals zero and never refuses; one open per path)
- 2: guard folded (a missing path still drops; the handoff keeps identity); yields to retire.go `priorRestorable` / walk_windows.go `revertJournal` "a vanished path is a completed revert"
- 3: guard folded (the judgePriors case lives in walk_windows_test.go)
- 4: guard folded (decision: newTokenConfiner and ResidueIn/siblingJournals are not liveness consumers; rewriteJournalOwner, walk_other_test.go, bite test)
- 5: guard folded (decision: owns the winguard.go protected-root comments; journal dir threaded as a parameter; names the resolved root)
- 6: guard folded (decision: owns the contract §9 / SECURITY.md protected-root prose; positive greps; grep-found sites)
- 7: guard folded (a sibling of ProbeNetwork, a pure skip-or-fail verdict, `make actionlint`)

**Authoritative sources**
- `docs/reviews/code-audit-2026-09-20.md` — High "a forgeable confinement journal …"
- `docs/plans/archived/2026-09-22 - 00 - audit-design-findings-plan.md` — items 9–11 and their ratified calls (bounded carry, `maxPriorCarries`)
- `docs/design/confinement-execution-contract.md` — §5, §6.2 (row #13)
- `SECURITY.md` — "What counts", "Out of scope"
- beads `apogee-winlabel-journal-secret`, `apogee-ifrv`

**Ratified design calls** (owner, 2026-09-23)
- **Journal binding:** no secret and no HMAC. Each journal `Entry` records the labelled object's file identity (volume serial + file index) and is re-checked before any `ClearTree`/`SetSDDL`. A per-run secret was rejected because a same-user Medium process can forge anything it can read, which SECURITY.md already places out of scope.
- **Journal fence:** a box root that is, or contains, `winlabel.JournalDir(home)` is refused on Windows.
- **Unverifiable entries:** a legacy entry (no identity field) keeps today's label-read rules. On an identity mismatch the entry is never cleared or restored: its prior is carried under item 11's bounded life, then dropped.
- **Liveness:** `Record` gains the owner's process creation time (FILETIME from `GetProcessTimes`). The owner is alive only if its PID runs AND the creation time matches. A legacy record (creation time 0) falls back to the PID-only check.
- **UDP arm proof:** new env `APOGEE_REQUIRE_LANDLOCK_NET=1` turns every skip on row #13's landlock path into a failure. The tests log the probed ABI. A CI step runs the test with `-v` and requires the subtest's PASS line.

**Standing requirements**
- skills: coding-standards
- `winlabel` stays a leaf (stdlib + `x/sys/windows` only; `deps_test.go`).
- JSON tags already in the journal never change. New fields are additive with `omitempty`, and every literal `Record{…}` rewrite threads them through.
- A Windows-only change is compile-checked with `GOOS=windows go test -c -o /dev/null <pkg>` when the executor is not on Windows. On a Windows host it also runs natively.

**Out of scope**
- A per-run secret, an HMAC, or any write into the user's workspace (marker file, ADS).
- Landlock UDP/UNIX-socket *rights* (no shipped kernel), and an ABI threshold for them.
- A `version` field on the journal Record.
- Closing `apogee-ifrv`: it closes on the first green CI run of item 7's step, which the owner confirms after pushing.

---

## 1. A journal entry records the identity of the object it labelled — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the identity helper is a per-journal stat seam (`Journal.stat`, fixed at `Open` from `osStat()`), not a standalone `fileIdentity(path)`. The Windows `statHandle` returns the link count and identity from one open, `hardLinkCount` (still used by `ClearTree`) wraps it, and the non-Windows `osStat` stub returns a zero `fileStat` with `errNoLabelFacility`. The seam follows the `revert` field's pattern, so the failed-read test can fail one path's read without a package global. The untagged `withIdentity(entry, st, err)` makes the zero-on-failure rule testable on any OS.
NOTES (2026-09-23): retry fix: the legacy JSON literal in `TestJournalWrittenByAnOlderApogeeHasNoFileIdentity` now doubles each backslash (`C:\\work`, `C:\\work\\vendor\\lib.dll`). The single-backslash form was invalid JSON (`\w`, `\v`, `\l`). The host has no Go on PATH, so verification used go1.26.6 windows/arm64 unpacked into the session scratchpad, with GOPATH/GOCACHE also there and GOTOOLCHAIN=local. All three Acceptance commands pass natively on Windows, the six identity tests PASS under `-v`, and `GOOS=linux go vet` and a `GOOS=darwin` test compile of the package are clean. gofmt is clean once the checkout's CRLF endings are stripped.

**What:**
**Goal:** every `winlabel.Entry` the session journal writes carries the labelled object's volume serial and file index in new additive JSON fields. A journal written by an older apogee decodes with those fields at zero.
**Approach (assumed at the header base):** add `Entry` fields, for example `Volume uint32 "vol,omitempty"` and `FileIndex uint64 "fid,omitempty"`, in `journal.go`. Fill them where the session journals a root or a prior (`session.go`, the path through `Journal.flush` and `LabelTree`) from a leaf helper `fileIdentity(path)`. That helper is Windows-only and reuses the `GetFileInformationByHandle` open that `hardLinkCount` already does; its non-Windows stub returns zero. The identity is taken before the label write, from the same handle-open semantics (no reparse follow beyond what `hardLinkCount` does).
**Regression guard.** A failed identity read journals a zero identity (judged by the legacy label-read rule) and never refuses the box or aborts the walk — `LabelTree` labels such a root today (walk_windows.go:77-90). Keep one open per path: take the identity only for the root and for `shouldJournal` descendants, or return it from the same handle `hardLinkCount` already opens; never a second standalone `CreateFile` per descendant.
**Files:** internal/platform/winlabel/journal.go, internal/platform/winlabel/session.go, internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/walk_other.go, internal/platform/winlabel/journal_test.go, internal/platform/winlabel/walk_windows_test.go
**Read first:** internal/platform/winlabel/walk_windows.go — LabelTree, hardLinkCount, descendantDecision; internal/platform/winlabel/journal.go — Entry, recordEntry, unwindEntry;
internal/platform/winlabel/session.go — Journal.record, Journal.flush; internal/platform/confiner_windows.go — resolveBoxRoot
**Tests:** a journal round-trip keeps the identity fields; a legacy JSON literal (no `vol`/`fid`) decodes to zero; on Windows, labelling a temp dir journals an identity equal to a fresh `GetFileInformationByHandle` of that dir; a failed identity read journals zero and the root is still labelled.
**Acceptance:**
- `go test -count=1 ./internal/platform/winlabel/`
- `GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/`
- `GOOS=windows go vet ./internal/platform/...`
**Commit:** `feat(winlabel): journal the file identity of every labelled object`

## 2. Recover acts only on an object whose identity still matches — ✅ DONE (2026-09-23)

NOTES (2026-09-23): an identity that exists but cannot be READ (a stat error other than not-exist) is refused the same way as a mismatch: the root is skipped and the prior carried. The plan left this case open. Declining costs nothing destructive and never acts on an unverified object; a not-exist error falls through to the label rules as the regression guard requires.
NOTES (2026-09-23): the identity check in judgeEntries also runs on a prior an earlier pass already vouched for (Judged=true). On a mismatch its Judged flag is withdrawn and the prior carried, which is how "a mismatched prior is never in the restore map" holds across a retry. restorablePriors itself is unchanged: it already hands off every unjudged prior.
NOTES (2026-09-23): identityRefuses tests not-exist with errors.Is(err, fs.ErrNotExist), not os.IsNotExist, because statHandle wraps its error with %w, which os.IsNotExist does not unwrap.

**What:** fixes the audit High "a forgeable confinement journal …": today a planted or stale claim drives `ClearTree`/`SetSDDL` on whatever now sits at its path. Depends on item 1.
**Goal:** during recovery or retire, an entry with a non-zero identity whose path exists but now names a different object is neither cleared nor restored; its prior is carried under the existing bounded life (`maxPriorCarries`) and then dropped. A missing path drops as today. An entry with a zero identity is judged exactly as before. `isVolumeRoot` still applies to every entry.
**Approach (assumed at the header base):** check the identity in `judgeEntries` (walk_windows.go) next to `rootClearable`/`priorRestorable`. A mismatch makes the root not clearable and routes the prior through `carryPrior`, never into `restorablePriors`' restore map. `revertibleRoots` must not clear a mismatched root even when `RootJudged` is set. The persisted verdict skips the label read but never the identity check, just as it never skips `isVolumeRoot`.
**Regression guard.** A MISSING path keeps today's rule and drops (`priorDrop`); the identity mismatch applies only to a path that exists and names a different object. The item yields to "a vanished path is a completed revert" (retire.go `priorRestorable`, walk_windows.go `revertJournal`). `handoffSparedRoots` carries the original entry's identity fields (e.g. `revertibleRoots` passes entries, not paths), never a fresh `Entry{Path: root, Root: true}` (retire.go:484).
**Files:** internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/retire.go, internal/platform/winlabel/walk_windows_test.go, internal/platform/winlabel/retire_test.go
**Read first:** internal/platform/winlabel/retire.go — judgeEntries, priorRestorable, rootClearable, revertibleRoots, restorablePriors, handoffSparedRoots, carryPrior;
internal/platform/winlabel/walk_windows.go — judgePriors
**Tests:** on Windows: journal a labelled dir, then delete and recreate it at the same path and label the new dir Low (`SetSDDL` with `lowSDDL`, or `RootJudged=true`) so the pre-item code would clear it; Recover leaves the new dir's SACL untouched. The carry is observed on a PRIOR entry whose recreated path carries a foreign non-Low label, not on the root (a refused root is skipped, never handed back). The same test with the original dir intact still clears it. A legacy (zero-identity) entry is still cleared; a missing prior path still drops. Pure-logic tests in retire_test.go: a `RootJudged` mismatch is never in the revertible set, a mismatched prior is never in the restore map, and a `handoffSparedRoots` table case keeps the spared root's identity.
**Acceptance:**
- `go test -count=1 ./internal/platform/winlabel/`
- `GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/`
**Commit:** `fix(winlabel): recover acts only on an object whose identity still matches its journal entry`

## 3. A journal Record carries its owner's process creation time — ✅ DONE (2026-09-23)

NOTES (2026-09-23): Started is stamped in Journal.flush only (not also in Open): flush is the one writer of this process's own record, and a failed creation-time read stamps 0 ("not recorded") and never refuses the write.
NOTES (2026-09-23): `go test ./internal/platform/` fails TestWindowsUnclearableDescendantKeepsTheJournal and TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry on this host with and without this item's changes (checked on a stash of the base tree). They look host-policy dependent (the denial each one relies on is not enforced here). They sit outside this item's acceptance and were not caused by it.

**What:**
**Goal:** every `winlabel.Record` the running process writes carries its own creation time (FILETIME, as a uint64) in a new additive field, and it survives every rewrite of the file. A legacy record decodes with it at 0.
**Approach (assumed at the header base):** add `Started uint64 "started,omitempty"` to `Record`. Set it in `Journal.flush` (session.go) beside `rec.PID = os.Getpid()`, from a leaf helper `processStarted(pid) (uint64, bool)` built on `windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` + `windows.GetProcessTimes`; the non-Windows stub returns 0. Thread `Started` through every literal `Record{PID: r.PID, …}` rewrite (`retire` in retire.go, `judgePriors` in walk_windows.go); grep `Record{` across the package for any others.
**Regression guard.** The `judgePriors` rewrite case needs a real Low SACL and `judgePriors` is Windows-tagged (walk_windows.go:347), so it lives in walk_windows_test.go using `lowLabelledDir`; the `retire()` case stays in the untagged retire_test.go.
**Files:** internal/platform/winlabel/journal.go, internal/platform/winlabel/session.go, internal/platform/winlabel/retire.go, internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/walk_other.go, internal/platform/winlabel/journal_test.go, internal/platform/winlabel/retire_test.go, internal/platform/winlabel/walk_windows_test.go
**Read first:** internal/platform/winlabel/session.go — Journal.flush, Open, Journal.Retire; internal/platform/winlabel/retire.go — retire; internal/platform/winlabel/walk_windows.go — judgePriors, ProcessAlive;
internal/platform/winlabel/journal.go — Record; internal/platform/winlabel/walk_windows_test.go — lowLabelledDir
**Tests:** a journal flushed by this process carries a non-zero `Started` equal to `processStarted(os.Getpid())` (Windows); a `retire` rewrite (retire_test.go) and a `judgePriors` rewrite (walk_windows_test.go, `lowLabelledDir`) both keep `Started`; a legacy literal decodes to 0.
**Acceptance:**
- `go test -count=1 ./internal/platform/winlabel/`
- `GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/`
**Commit:** `feat(winlabel): journal the owner's process creation time`

## 4. Journal-owner liveness is pinned to PID and creation time

**What:** fixes the audit's PID-only liveness: a recycled PID reads as alive, so its journal is never recovered and its roots are spared forever. Depends on item 3.
**Goal:** a journal owner counts as alive only when a process with its PID is running and, if the record's `Started` is non-zero, that process's creation time equals `Started`. A record with `Started == 0` keeps the PID-only check. Every liveness consumer in winlabel and its callers uses this rule.
**Approach (assumed at the header base):** reshape the injected `alive func(int) bool` into one that takes the record's PID and start time (e.g. `func(pid int, started uint64) bool`). `ProcessAlive` becomes that check, built on item 3's `processStarted`. Producers and consumers to update: `ProcessAlive` (walk_windows.go), its stub (walk_other.go), `recoveryLiveness`, `recoverSweep`, `revertibleRoots`, and `internal/platform/confiner_windows_test.go`'s `winlabel.ProcessAlive` call.
**Regression guard.** newTokenConfiner (internal/platform/confiner_windows.go) is not a liveness consumer — it only calls winlabel.Recover(home); drop it from the producers/consumers list and from Files unless the tree shows otherwise; ResidueIn/siblingJournals judge no liveness — drop them from the consumer list too. `rewriteJournalOwner` (confiner_windows_test.go) zeroes `Started` or sets it to the new owner's creation time, or `TestWindowsRecoveryLeavesALiveProcessAlone` goes red after item 3. `walk_other_test.go:48` calls `ProcessAlive(os.Getpid())` and moves to the new signature. The wrong-`Started` bite is tested at `ProcessAlive(os.Getpid(), wrong)` directly or via a sibling naming `liveChildPID`, never through Recover on self (`recoveryLiveness` already reads self dead).
**Files:** internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/walk_other.go, internal/platform/winlabel/retire.go, internal/platform/winlabel/retire_test.go, internal/platform/winlabel/walk_windows_test.go, internal/platform/winlabel/walk_other_test.go, internal/platform/confiner_windows_test.go
**Read first:** internal/platform/winlabel/walk_windows.go — ProcessAlive, revertSparingLiveSiblings; internal/platform/winlabel/retire.go — recoveryLiveness, recoverSweep, revertibleRoots;
internal/platform/winlabel/walk_other_test.go — TestNonWindowsProcessAliveSparesNothing; internal/platform/confiner_windows_test.go — rewriteJournalOwner, liveChildPID, TestWindowsRecoveryLeavesALiveProcessAlone
**Tests:** `ProcessAlive(os.Getpid(), wrong)` reads dead (or: a sibling journal naming a `liveChildPID` with a wrong `Started` is spared before the item and cleared after); the correct `Started` reads alive; `Started == 0` with a live PID reads alive (legacy). `rewriteJournalOwner` rewrites `Started` with the PID and `TestWindowsRecoveryLeavesALiveProcessAlone` stays green; `TestNonWindowsProcessAliveSparesNothing` compiles against the new signature. Pure-logic retire tests drive the new `alive` signature.
**Acceptance:**
- `go test -count=1 ./internal/platform/winlabel/ ./internal/platform/`
- `GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/`
- `GOOS=windows go test -c -o /dev/null ./internal/platform/`
**Commit:** `fix(winlabel): a journal owner is alive only while its PID and creation time both match`

## 5. A Windows box root may not contain the confinement journal directory

**What:**
**Goal:** on Windows, confining a box whose writable root is, or is an ancestor of, `winlabel.JournalDir(home)` is refused through the same protected-roots refusal (and message shape) that `windowsProtectedRoots` already produces. The refusal names the resolved root.
**Approach (assumed at the header base):** add `JournalDir(Home())` (resolved to its absolute, cleaned form) to `windowsProtectedRoots` in internal/platform/winguard.go, so the existing ancestor/equality check covers it. The refusal is announced to the user, so the test drives it with the exact path string the user would configure (e.g. the home directory itself and `~/.apogee`), not only the resolved journal path.
**Regression guard.** Item 5 owns the code comments in internal/platform/winguard.go that enumerate the protected roots (update them to name the journal dir). `windowsProtectedRoots` stays pure: the journal dir is a parameter (`windowsProtectedRoots(lookup, userHome, journalDir)`), passed by `newTokenConfinerWithoutRecovery` as `winlabel.JournalDir(home)` only when `home != ""`, resolved through `rules.finalPath` when it exists (lexical form otherwise). This overrides the Approach's in-function `JournalDir(Home())` and its "exact path the user would configure" test. New tests are named to match `Protected`, test at the `windowsBoxRoots` level with already-resolved paths, and count the bite only on `~/.apogee` and `~/.apogee/confinement` (the home dir is refused today via USERPROFILE).
**Files:** internal/platform/winguard.go, internal/platform/winguard_test.go, internal/platform/confiner_windows.go
**Read first:** internal/platform/winguard.go — windowsProtectedRoots, windowsBoxRoots, windowsLabelGuardrail; internal/platform/confiner_windows.go — newTokenConfinerWithoutRecovery, labelBox, resolveBoxRoots;
internal/platform/winlabel/journal.go — JournalDir; internal/platform/winguard_test.go — TestWindowsProtectedRootsFromEnvironment
**Tests:** `TestWindowsProtectedRootsFromEnvironment`'s want list gains the journal dir, plus an off-Windows table case (an empty journal dir adds nothing); at the `windowsBoxRoots` level with resolved paths, a box rooted at `~/.apogee` and at `~/.apogee/confinement` is refused with the protected-root message (the home-dir case stays but carries no bite); a sibling such as `~/work` is not.
**Acceptance:**
- `go test -count=1 -run 'Protected|Winguard' ./internal/platform/`
- `GOOS=windows go test -c -o /dev/null ./internal/platform/`
**Commit:** `fix(platform): a Windows box may not label the confinement journal directory`

## 6. The docs state what the journal trusts and what it no longer trusts

**What:** Depends on items 2, 4 and 5.
**Goal:** SECURITY.md, the winlabel package doc and the confinement execution contract say that journal entries are bound to file identity, that liveness uses PID + creation time, that `~/.apogee/confinement` is a protected root, and that a same-user Medium process forging a journal is out of scope. No prose anywhere still says journal liveness is PID-only or that a claim is trusted verbatim.
**Approach (assumed at the header base):** rule: every comment or doc sentence describing journal liveness or journal-claim trust is updated. Find them with `grep -rnE "PID-only|pid only|ProcessAlive|liveness|trusted verbatim|prior_sddl" --include=*.go --include=*.md internal/platform docs/design SECURITY.md`. The CHANGELOG entry travels in the sidecar.
**Regression guard.** Item 6 owns the prose enumerations of protected roots in docs/design/confinement-execution-contract.md (§9) and SECURITY.md — add the journal dir there. The rule's grep excludes `docs/design/archived`, and every non-archived site it finds is in scope (retire.go, walk_windows.go, session.go and doc.go comments included); where items 2 and 4 already rewrote a comment, this item only verifies it. `PID-only` appears nowhere at the base, so the acceptance greps are positive.
**Files:** SECURITY.md, internal/platform/winlabel/doc.go, docs/design/confinement-execution-contract.md, plus every site the rule's grep finds (excluding docs/design/archived)
**Read first:** internal/platform/winlabel/doc.go — package doc; docs/design/confinement-execution-contract.md — §9 Guardrails / Interrupted cleanup bullets; SECURITY.md — What counts, Out of scope; internal/platform/winlabel/retire.go — recoveryLiveness;
internal/platform/winlabel/walk_windows.go — ProcessAlive, revertSparingLiveSiblings; docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md — Guardrails bullet
**Tests:** docs-only; no Go test.
**Acceptance:**
- `grep -n "file identity" SECURITY.md internal/platform/winlabel/doc.go docs/design/confinement-execution-contract.md`
- `grep -n "creation time" SECURITY.md internal/platform/winlabel/doc.go docs/design/confinement-execution-contract.md`
- `grep -n "confinement" docs/design/confinement-execution-contract.md` shows the §9 Guardrails bullet naming the journal dir, and `grep -n "apogee/confinement" SECURITY.md` prints a line
**Commit:** `docs(winlabel): journal trust is file identity plus creation-time liveness`

## 7. APOGEE_REQUIRE_LANDLOCK_NET makes a skipped UDP arm fail, and CI requires it

**What:** context for bead `apogee-ifrv`, which closes on the first green CI run of this step (owner-confirmed).
**Goal:** with `APOGEE_REQUIRE_LANDLOCK_NET=1`, `TestLandlockProbeNetwork` fails rather than skips when landlock is absent, the ABI is below 4, or bash is missing for the UDP row. Every run logs the probed landlock ABI. The CI `check` job runs the test with that env and `-v`, and fails unless `--- PASS: TestLandlockProbeNetwork/udp_egress_under_network_deny` appears. `docs/manual/building.md` documents the variable.
**Approach (assumed at the header base):** read the env in `internal/platform/landlock_linux_test.go` (`TestLandlockProbeNetwork`), and thread a "required" flag into `confinetest.ProbeNetwork` (a field on its options, or a helper it calls) so its `NetworkEgress==false` skip and the row's no-bash skip become `t.Fatalf` with the same wording. Log the ABI with `t.Logf`. Add a step to `.github/workflows/ci.yml`'s `check` job after `make test`: `APOGEE_REQUIRE_LANDLOCK_NET=1 go test -count=1 -v -run 'TestLandlockProbeNetwork' ./internal/platform/ | tee landlock-net.log`, then a `grep -q -- '--- PASS: TestLandlockProbeNetwork/udp_egress_under_network_deny' landlock-net.log`, with `set -o pipefail`.
**Regression guard.** `ProbeNetwork(t, c, sh)` keeps its signature and behaviour (callers in namespace_linux_test.go, confiner_windows_test.go, seatbelt_darwin_test.go); add a sibling (e.g. `ProbeNetworkRequired(t, c, sh)`, or `ProbeNetwork` delegating to `probeNetwork(t, c, sh, required)`). The env is read only in `TestLandlockProbeNetwork`, never inside confinetest. The skip-or-fail decision is a pure, table-tested function (e.g. `skipOrFail(required bool, reason string)`); the `t.Skip`/`t.Fatalf` call is a one-liner over it.
**Files:** internal/platform/landlock_linux_test.go, internal/platform/confinetest/confinetest.go, .github/workflows/ci.yml, docs/manual/building.md
**Read first:** internal/platform/confinetest/confinetest.go — ProbeNetwork, udpSendLine, runDatagramProbe; internal/platform/landlock_linux_test.go — TestLandlockProbeNetwork, newTestConfiner; internal/platform/landlock_linux.go — landlockConfiner.abi;
.github/workflows/ci.yml — check job ("go test (race, sharded)" step); docs/manual/building.md — APOGEE_TEST_* env paragraph
**Tests:** a table test of the pure skip-or-fail verdict (required → fail, not required → skip) on any OS; on a host without landlock ABI ≥ 4, the env set turns the skip into a failure; unset, it still skips.
**Acceptance:**
- `go test -count=1 ./internal/platform/confinetest/ ./internal/platform/`
- `GOOS=linux go vet ./internal/platform/...`
- `make actionlint`
- `grep -n APOGEE_REQUIRE_LANDLOCK_NET docs/manual/building.md .github/workflows/ci.yml`
**Commit:** `test(confinetest): APOGEE_REQUIRE_LANDLOCK_NET makes CI prove the landlock UDP arm runs`
