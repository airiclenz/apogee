# Denial label and Windows confiner tests plan

**Goal:** Stop the confinement denial labels from asserting that a write was blocked when the denied operation may have been something else, and make the two Windows confiner tests that fail on the owner's windows/arm64 box check their own precondition so they skip (with the reason) where the host cannot produce the denial, and stay red where it can.
**Date:** 2026-09-30
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** bfad66a1

**Sources:**
- beads `apogee-denial-label-blames-writes`, `apogee-windows-confiner-test-failures`
- ADR 0056 (terminal fail-fast, denial labels, amended 2026-08-25)
- `docs/design/confinement-execution-contract.md`

**Ratified design calls (owner, 2026-09-30):**
- **Label wording:** one neutral wording for every denial, no errno classification. Likely: `[likely blocked by workspace confinement: the sandbox refused an operation; it allows writes only inside <roots>; setuid programs run without their privileges]`. Stop: `[blocked by workspace confinement: an operation was denied, so the command was stopped; the sandbox allows writes only inside <roots>; setuid programs run without their privileges]`.
- **Windows tests:** self-checking probe — after the blocking DACL is planted, the test attempts the denied label write itself; allowed → `t.Skipf` naming the host facts; refused → the test runs unchanged and a failure stays a failure.
- **Amended by owner, 2026-09-30 (regression check):** both label wordings replace the clause ", and refuses privileged (setuid) programs" with "; setuid programs run without their privileges" — landlock sets PR_SET_NO_NEW_PRIVS, so setuid programs run unprivileged rather than being refused.

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Confirming why `ps` fails under confinement (setuid vs /proc) or documenting it as a limit.
- The ~123 other Windows failures on the owner's box and the flaky `TestE2EDefaultWireStaysChatCompletions`.
- Matching Windows "Access is denied." as a denial signature.
- CHANGELOG entries of past releases that quote the old wording.

**Regression check (2026-09-30, bfad66a1):**
- 1: recast — header label wordings amended by owner (setuid clause); see Ratified design calls.
- 2: guard folded — empty-prior restore spelled "S:" with Fatalf on restore/read failure; Acceptance grep pins the helper call in each test.
- 1: guard folded (re-check) — `ps` denial test pinned as a `subprocessToolResult` case, not end-to-end; doc-comment rule widened to every `internal/tools` comment on the label's shape or write-re-aim rationale.

## 1. Denial labels no longer claim a write was blocked — ✅ DONE (2026-09-30)

NOTES (2026-09-30): docs/design/confinement-execution-contract.md not edited — it quotes both labels only as `[… workspace confinement: …]` with an ellipsis, so no quote of the old wording exists there.
NOTES (2026-09-30): confinementWritableRoots doc comment reworded from "the tail both denial labels end with" to "as both denial labels name them" (roots order and zero-box fallback rationale kept) — the roots are no longer the labels' tail now that the setuid clause follows them.

**What:** Recast at the regression check (2026-09-30). fix for `apogee-denial-label-blames-writes`: a confined `ps` failing with "Operation not permitted" was told "writes are allowed only inside <roots>", pointing the model at a write that never happened.

**Regression guard.** The new `ps: Operation not permitted` test is a `subprocessToolResult` case (`Confined: true`, `DenialStopped: false`, `ExitCode: 1`, `Box` set) beside `TestSubprocessToolResultDenialLabel`, not a `Terminal.Execute` journey — end-to-end the watch trips the stop label (terminal.go:413-414), which lacks "the sandbox refused an operation". The comment rule covers every doc comment in `internal/tools` describing the label's shape or the write-re-aim rationale, including `confinementWritableRoots` ("the tail both denial labels end with", exec_common.go:129) — find with `grep -n "tail both denial labels\|re-aim\|put the file" internal/tools/*.go`.

**Goal:** `confinementDenialLabel` and `confinementDenialStopLabel` render exactly the ratified wordings in the header (roots via `confinementWritableRoots`, unchanged); neither label contains the phrase `writes are allowed only inside` as its leading reason, both still name every writable root, the likely label keeps `likely blocked by workspace confinement`, the stop label keeps `the command was stopped`.

**Approach (assumed at the header base):** edit the two string builders in `internal/tools/exec_common.go` and their doc comments (the comments must stop saying the model's next act is to re-aim a write — say the roots are one fact the model can act on when the denied operation was a write). Update `TestConfinementDenialLabelsNameTheWritableRoots` in `internal/tools/exec_common_test.go` to pin the new wording. Add one journey test in `internal/tools/terminal_test.go` beside `TestSubprocessToolResultDenialLabel`: a confined failed result whose output is `ps: Operation not permitted` renders a label that contains `the sandbox refused an operation` and does not contain `writes are allowed only inside` directly after the colon. Update the quoted label in ADR 0056 decision 2 with a dated amendment note, and any quote in `docs/design/confinement-execution-contract.md`. Prose guard rule: every non-archived, non-CHANGELOG site quoting the old wording — find with `grep -rn "writes are allowed only inside" --include=*.go --include=*.md . | grep -v "docs/plans/archived\|CHANGELOG.md\|skill-runs"`.

**Files:** internal/tools/exec_common.go; internal/tools/exec_common_test.go; internal/tools/terminal_test.go; docs/adr/0056-terminal-fail-fast-and-session-scratch.md; docs/design/confinement-execution-contract.md
**Read first:** internal/tools/exec_common.go — confinementDenialLabel, confinementDenialStopLabel, confinementWritableRoots; internal/tools/terminal.go — subprocessToolResult;
internal/tools/exec_common_test.go — TestConfinementDenialLabelsNameTheWritableRoots; internal/tools/terminal_test.go — TestSubprocessToolResultDenialLabel, TestTerminal_ConfinementDenialLabelEndToEnd;
docs/adr/0056-terminal-fail-fast-and-session-scratch.md — decision 2

**Closes:** apogee-denial-label-blames-writes

**Tests:**
- `go test -count=1 -run 'TestConfinementDenialLabels|TestSubprocessToolResultDenial|TestTerminal_ConfinementDenialLabelEndToEnd|TestTerminal_MergedStreamDenialStopsTheScript|DenialStop' ./internal/tools/`

**Acceptance:**
- `go build ./... && go vet ./internal/tools/`
- the Tests command passes
- `grep -rn "writes are allowed only inside" --include=*.go --include=*.md . | grep -v "docs/plans/archived\|CHANGELOG.md\|skill-runs"` prints no line where the phrase directly follows `confinement: ` or `stopped; `

**Commit:** `fix(tools): word confinement denial labels without asserting a blocked write`

## 2. Windows label-denial tests check their own precondition

**What:** `TestWindowsUnclearableDescendantKeepsTheJournal` and `TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry` in `internal/platform/confiner_windows_test.go` fail on the owner's windows/arm64 box (already at `ea51204c`); both assume the planted DACL `D:P(A;;0x170080;;;OW)` makes the label write fail, which that host apparently grants. Test-only change; CI (windows-latest) must keep running both tests in full.

**Regression guard.** A prior read as "" (no label, as a fresh temp dir has) must be restored with the clear spelling `"S:"` (winlabel's `clearSDDL` is unexported), never `SetSDDL(path, "")`, which fails in `labelHandle.setSDDL` (no SACL) and would leave the Low label on the root. If the prior read or the restore fails, the helper calls `t.Fatalf` — never `t.Skipf`.

**Goal:** both tests, right after planting the DACL, call one shared helper that attempts the exact label operation the test expects to be refused (the clear for the descendant test, the Low-label write for the root test) against the obstructed path; when that attempt succeeds, the helper restores the path's prior label and calls `t.Skipf` with a message naming the path, the attempted operation, whether the token is elevated, and which of SeTakeOwnershipPrivilege / SeRestorePrivilege / SeSecurityPrivilege are enabled; when it is refused, the test proceeds unchanged.

**Approach (assumed at the header base):** add an unexported helper (e.g. `requireLabelWriteDenied`) beside `setFileDACL`/`restoreFileDACL`, using `winlabel.SetSDDL` / `winlabel.ReadSDDL` and `winlabel.IsLowLabel`, and `windows.GetCurrentProcessToken` for elevation and privilege facts (a helper that reads them, failing soft to "unknown"). Do not change product code. After the change, add a beads note so the owner can confirm on the box: `bd comments add apogee-windows-confiner-test-failures "Probe landed; on the windows/arm64 box run: go test -count=1 -run 'TestWindowsUnclearableDescendantKeepsTheJournal|TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry' -v ./internal/platform/ — a SKIP names the host fact; a FAIL is a real fence bug."` (use the tracker's actual comment command if that spelling differs). The ticket stays open until the owner reports.

**Files:** internal/platform/confiner_windows_test.go
**Read first:** internal/platform/confiner_windows_test.go — TestWindowsUnclearableDescendantKeepsTheJournal, TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry, setFileDACL; internal/platform/winlabel/walk_windows.go — SetSDDL, ReadSDDL, labelHandle.setSDDL;
internal/platform/winlabel/sddl.go — clearSDDL; golang.org/x/sys/windows — GetCurrentProcessToken

**Tests:**
- `GOOS=windows GOARCH=arm64 go vet ./internal/platform/`
- `GOOS=windows GOARCH=amd64 go test -c -o /dev/null ./internal/platform/`

**Acceptance:**
- both Tests commands succeed on Linux
- `grep -n "requireLabelWriteDenied(" internal/platform/confiner_windows_test.go` shows the definition plus one call inside each of the two named tests, after its `setFileDACL` line
- `bd show apogee-windows-confiner-test-failures` shows the note and status still open

**Commit:** `test(platform): skip the label-denial tests where the host grants the label write`
