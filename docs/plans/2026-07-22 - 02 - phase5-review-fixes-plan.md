# Implementation plan — Phase 5 review fixes (journal lifecycle, probe ordering, terminal gate)

**Date:** 2026-07-22. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session **on the owner's Windows machine** (native toolchain, NOT WSL — most items carry
windows-tagged tests that must run natively), forwarding skills: `coding-standards` (mandatory for
all new Go). One sub-agent per numbered work item, verifier before commit, mark items done with a
✅ in the item heading of this file. A final `make check` on the Linux devbox is required before
item 14 closes the plan (linux-tagged landlock tests cannot run on Windows).

**Scope source:** `docs/reviews/code-review-2026-07-22.md` — the Phase 5 code review (4 High,
10 Medium). Item text below is self-contained; the review is the finding record, not an authority.
**Precedence for design questions:** ADR 0012, [ADR 0020](../adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md),
[ADR 0021](../adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
(incl. the 2026-07-22 Amendment) and `docs/design/confinement-execution-contract.md` govern
everything confinement- and probe-shaped. If an artifact produced by an earlier item of THIS plan
disagrees with those sources, the sources win — stop and consult, don't propagate.

## Why

The Phase 5 review found the code faithful to its ADRs on the happy path but concentrated its
defects in the Windows label-journal lifecycle — the failure and cleanup paths that have never run
natively. The journal exists so the one disk mutation apogee performs (Low mandatory labels on the
box roots, ADR 0020 §2) is always revertible; today it is deleted even when the revert fails, can
record apogee's own label as the user's prior state (teardown then *restores* the label,
self-perpetuatingly), is skipped entirely when the user profile is unresolvable, and is written
non-atomically. `apogee probe host` heals-or-destroys the residue it exists to report before
reading it. Separately: the terminal tool rejects valid cmd.exe lines through a POSIX parser, Job
Object handles leak on the two routine early-exit paths, and `probe model` can claim an auto-apply
that startup will refuse. These land **before** the still-pending owner-run live-enforcement
proofs — the journal defects are exactly the kind a live run bakes onto the real disk.

## Ground truth (verified 2026-07-22 — anchors, not vibes)

- **Build-tag split (decides where each item's tests run):** `internal/platform/winconfine.go` is
  UNTAGGED — journal read/write/list, residue wording, guardrails, `windowsFloorBuild` (`:28`) are
  Linux-table-testable. `internal/platform/confiner_windows.go` is `//go:build windows` (token,
  label walks, recovery). `internal/tools/exec_common.go` + `exec_teardown.go` untagged;
  `exec_pgroup_other.go` is windows-tagged despite the name.
- Journal deleted regardless of revert outcome: `confiner_windows.go:289–297` (`restoreLabels` —
  `os.Remove` unconditional), `:356–369` (`recoverLabelJournals` — `_ = revertLabelJournal(j);
  _ = os.Remove(path)`). The composition root swallows the only surviving error:
  `cmd/apogee/wire.go:136–138` (`defer func() { _ = closer.Close() }()`).
- Own-label self-poisoning: `labelBox` (`confiner_windows.go:217–235`) appends a journal entry per
  root with no per-path dedupe and no own-SDDL recognition; `labelTree` (`:271–276`) journals any
  pre-existing label it finds; `priorLabels()` (`winconfine.go:101–109`) is a map build — last
  entry wins, empty priors skipped — so a re-journalled root whose "prior" is apogee's own Low
  label gets that label RESTORED by `revertLabelJournal` (`confiner_windows.go:319–323`).
- Journal-less labelling: `confinementJournalHome()` returns `""` when `os.UserHomeDir()` fails
  (`winconfine.go:251–257`); `newTokenConfiner` then keeps `journalPath == ""`
  (`confiner_windows.go:121–131`) and `flushJournal` silently no-ops (`:300–306`) while `Confine`
  still labels the disk.
- Non-atomic journal + invisible corruption: `writeLabelJournal` is truncate-in-place
  `os.WriteFile` (`winconfine.go:270–282`), rewritten on every pre-labelled descendant found
  during the walk; an unreadable journal is `continue`d by BOTH `recoverLabelJournals`
  (`confiner_windows.go:359–362` — left on disk, never reverted) and `confinementResidue`
  (`winconfine.go:334–347` — never reported).
- Probe ordering: in the `probe.Inputs` literal, `Confiner: platform.NewConfiner()`
  (`cmd/apogee/probe.go:79`) is evaluated before `Residue: platform.ConfinementResidue()` (`:91`);
  on Windows the constructor's recovery reverts labels and deletes dead journals first, so the
  residue line ADR 0020 §2 promises ("`apogee probe host` reports an outstanding journal") can
  never fire for a dead run. Three surfaces pin the host report read-only: ADR 0021 §1, README
  ("nothing is written"), and the command's own Long text (`probe.go:31`).
- Terminal gate: `internal/tools/terminal.go:74–80` runs `shlex.Split` (POSIX) on every command
  unconditionally; on Windows the line then goes to cmd.exe via `shellHost` (schema at
  `terminal.go:22` says "POSIX sh on Unix, cmd on Windows"). `echo don't panic` and
  `dir "C:\Program Files\"` are valid cmd lines rejected today. The stale "shlex-validated" claim
  lives in `docs/design/technical-design.md` §5 (P3.8 row).
- Job Object leak: `setProcessGroupTeardown` creates the job handle at call time
  (`exec_pgroup_other.go:45–54`), BEFORE `Confine` runs (`exec_common.go:125–138`); a `Confine`
  error returns before `runWithTeardown` is called, and `runWithTeardown` returns on a
  `cmd.Start()` error before its `defer td.release()` is installed (`exec_teardown.go:74–79`).
  `release` is already idempotent via the `InvalidHandle` guard.
- Probe/startup twin ladders: `autoApplyKeys` (`cmd/apogee/probemodel.go:200`) mirrors
  `resolveValidatedSet` but omits the `validated.Validate(entry, mechanisms.Descriptors())` step
  (`cmd/apogee/validatedsets.go:83`). Every `resolveValidatedSet` test threads an EMPTY
  `t.TempDir()` probe dir — no test proves a stored record flips a set from offered to applied
  (`cmd/apogee/validatedsets.go:42–46`, `internal/agent/loop.go:161–165` are the two call sites
  that gained the Medium rung).
- Untested refusal gates: `probemodel.go:98–110` (discovery failure; `errProbeModelNeedsLabel` on
  an empty `/v1/models`). Vacuous test: `internal/probe/model_test.go:60–71`
  (`TestGatherModelWritesNothing` asserts an unrelated temp dir is empty; `GatherModel` takes no
  path). Quote table gaps: `internal/platform/host_test.go:78–87`; the `%VAR%` non-guarantee is
  comment-only at `internal/platform/host.go:328–332`.
- Floor gate has no seam: inline `windows.RtlGetNtVersionNumbers()` read at
  `confiner_windows.go:112`; the windows-tagged test (`confiner_windows_test.go:71–85`) can only
  observe the branch its host is on.
- Dead surface: `Path.ExecExt` — definition `internal/platform/platform.go:63–67` +
  `host.go:96–97`; repo-wide grep finds zero production callers (tests only).
- `ConfineWritablePaths` has one reader (`internal/agent/dispatch.go:121–125`) and NO writer;
  ADR 0020 §2 calls a labelled/box-local `%TEMP%` a "hard prerequisite" for toolchain work under
  the Windows fence. Not implemented anywhere; acknowledged only in a deferred cell of
  technical-design §5.
- **Windows-host environment facts (from the Phase 5 run, still current):** `make` is absent —
  run the underlying commands (`go vet ./...`, `go build ./...`, `go test -count=1 ./...`, six
  cross targets). The checkout is `core.autocrlf=true`, so `gofmt -l .` flags every file — check
  gofmt against LF copies of changed files only. Four test failures are pre-existing on that host
  (`TestSaveHostAcknowledgement_PreservesTheFileMode`, `TestAutofix…`, two
  `TestDiagnostics_…GoVet…`, `TestFoldActivityClockRunsPerPhrase`) — not caused by, and not to be
  fixed by, this plan.

## Settled design (do not re-litigate in work items)

- **ADR 0020's shape is fixed.** The facility stays the restricted Low token; the box stays
  labels-on-disk; `domain.Confiner` does NOT grow a method (teardown stays the optional
  `io.Closer` asserted at the composition root); the journal home stays
  `confinementJournalHome()` (`~/.apogee`), deliberately independent of `--config`
  (`winconfine.go:245–250` — the rationale stands).
- **Fail closed, with an undo record — always.** No code path may apply a label without a
  persisted journal entry first, and no code path may delete a journal whose labels are not
  verifiably reverted. When in doubt: refuse to confine (`ErrConfinementUnavailable` → the
  dispatch demotes to Approval; never unconfined).
- **Restoring toward LESS privilege is the safe direction.** Where a prior state is ambiguous
  (own-label collision, item 2), prefer clearing to unlabelled (implicitly Medium) over restoring
  a Low label — ADR 0020's own manual remedy states an explicit Medium label is behaviourally
  identical to no label.
- **The probe host report's read-only pledge (ADR 0021 §1) is binding.** Item 5 decides its one
  Windows exception explicitly and amends the docs in the same item; no other item may quietly
  add probe-path writes.
- **The `%TEMP%`/writable-paths design is NOT built in this plan.** Item 12 records it; building
  it is its own future design session (ADR 0020 §2 + contract §7 are the sources).
- **Follow existing idiom religiously** — comment density and `doc.go` conventions are
  load-bearing. ADR 0010: `internal/*` depends only on `internal/domain` downward. Windows
  semantics stay table-testable off-Windows via injected seams (fold flags, env funcs, fakes);
  native runs are additional proof, never a replacement.

## Work items

Each item is one sub-agent's task: read the named files first, implement, test, `go vet` + run the
package tests, then mark the item done here. Any authorized deviation from item text lands as a
dated `NOTES (YYYY-MM-DD):` line under the item. Review finding IDs refer to
`docs/reviews/code-review-2026-07-22.md`.

## 1. The journal survives a failed revert; the composition root surfaces the Close error — ✅ DONE (2026-07-22)

NOTES (2026-07-22): the stderr line is worded by a new exported pure helper
`platform.ConfinementTeardownNotice(err)` in `winconfine.go` (the `probe.DegradedNotice` /
`ConfinementResidue` idiom) rather than inline in `wire.go`, so the wording stays table-testable;
it quotes the `icacls` remedy through a new shared const `windowsLabelRemedy` that
`windowsResidueNotice` now also uses — the rendered wording of the residue notice is byte-identical.
The journal PATH reaches that line through `restoreLabels`, which wraps a failed revert in an error
naming `c.journalPath`. `restoreLabels` also keeps the IN-MEMORY journal (not just the file) on
failure, so the object still describes what is outstanding.

**What:** (Review: High "journal destroyed on failed revert".) In
`internal/platform/confiner_windows.go`, `restoreLabels` and `recoverLabelJournals` remove the
journal file ONLY when `revertLabelJournal` returned nil; on error the file stays untouched so
the next `NewConfiner()` retries and `ConfinementResidue()` reports it. Extract the
revert-then-conditionally-remove decision into an UNTAGGED helper in `winconfine.go` taking the
revert as a `func(labelJournal) error` so the retention rule is Linux-table-testable (the
`internal/present` seam pattern). In `cmd/apogee/wire.go:136–138`, stop discarding the deferred
`Close()` error: print one line to stderr naming the journal path and the `icacls` manual remedy
(reuse `windowsResidueNotice`'s wording style; do not invent a second phrasing).
**Tests:** Untagged: table test on the extracted helper — failing revert ⇒ file kept, nil revert
⇒ file removed. Windows-tagged: existing revert/recovery tests still green.
**Acceptance:** `go test ./internal/platform/...` green on Linux AND natively on the Windows
host; a forced revert failure demonstrably leaves the journal file on disk.
**Commit:** `fix(confine): keep the label journal when a revert fails`

## 2. Never journal apogee's own label as prior state; dedupe entries per path — ✅ DONE (2026-07-22)

NOTES (2026-07-22): three deviations, all widening the item's rule in the Settled-design
direction ("restoring toward LESS privilege is the safe direction"). (a) The own-label guard
(`isLowLabelSDDL`) recognises ANY mandatory-label ACE naming the LOW level — `LW` or
`S-1-16-4096` — not only the two constants' verbatim spelling: the same label read back from the
OS carries descriptor flags (`S:AI(…)`) and, on a path that inherited it from a labelled root,
the `ID` ACE flag, so string equality would recognise apogee's own label in one spelling only. A
genuinely foreign Low prior is therefore also cleared rather than restored, which is exactly the
ambiguity the Settled design resolves that way. (b) An entry left naming neither a root to walk
nor a prior to put back is not recorded at all, instead of being appended with an empty prior:
re-walking an already-labelled tree would otherwise append (and re-flush) one useless entry per
file — O(n²) journal writes. (c) `Root` is sticky: a path first journalled as a labelled
descendant and later handed in as a box root is promoted, because teardown walks roots and
first-prior-wins alone would leave that tree labelled. The fold is `foldLabelPath`
(`strings.ToUpper`) — the whole-path form of `hostRules.sameComponent`'s case-folding and the
same fold `windowsProtectedRoots` already dedupes with.

**What:** (Review: High "journal records apogee's own Low label as prior".) In
`labelBox`/`labelTree` (`confiner_windows.go`): (a) skip appending an entry for a path already
present in `c.journal.Entries` — first-recorded prior wins — comparing case-folded with the same
fold the rule table uses; fold the `labelled` memo map keys identically; (b) never record a
`PriorSDDL` equal to the backend's own `windowsDirLabelSDDL`/`windowsFileLabelSDDL` spelling —
record the entry with an empty prior instead (revert then clears to unlabelled; see Settled
design: less privilege is the safe direction). Put the append-or-skip decision in an untagged
pure helper in `winconfine.go` (entries, path, prior, fold ⇒ decision) so both triggers — the
re-Confine-after-partial-pass and the concurrent-session read of a transient Low label — are
table-testable on Linux.
**Tests:** Untagged: table rows for duplicate path (case-varied), own-dir-SDDL prior,
own-file-SDDL prior, genuine foreign prior (kept verbatim). Windows-tagged: after a first
`Confine`, force `labelled` reset (fresh backend, same box, same journal semantics) and
re-`Confine`; assert the journal contains no entry whose `PriorSDDL` is the backend's own label
and `Close()` leaves the root unlabelled.
**Acceptance:** the self-perpetuating-residue scenario is impossible by construction: no journal
written by this build can instruct a revert to apply a Low label that apogee itself minted.
**Commit:** `fix(confine): never record apogee's own Low label as prior state`

## 3. No journal home ⇒ refuse to confine — ✅ DONE (2026-07-22)

NOTES (2026-07-22): no existing windows-tagged test constructed with `""`, so none needed
updating — the new refusal test is the only test change. `flushJournal`'s `journalPath == ""`
early return is left in place as a belt (labelBox now refuses first, so it can no longer
accompany a disk mutation); only its doc comment says so.

**What:** (Review: Medium "labelling proceeds journal-less".) In `labelBox`
(`confiner_windows.go`), return `ErrConfinementUnavailable` (with a message naming the missing
user profile) when `c.journalPath == ""`, BEFORE any label read or write. Update
`newTokenConfiner`'s doc comment (`:118–120`): the `home == ""` carve-out now means "Confine
refuses; construction and Capabilities are unaffected" — caps stay `{FSWrite: true, …}` because
the FACILITY is present; the refusal is the routine per-run kind contract §4 demotes to a Gate.
Update any existing windows-tagged test that constructs with `""` and expected labelling.
**Tests:** Windows-tagged: `newTokenConfiner("")` + `Confine` on a valid box ⇒
`errors.Is(err, domain.ErrConfinementUnavailable)` AND `readLabelSDDL(root) == ""`.
**Acceptance:** the invariant "the journal is written before any label" has no remaining
bypass: with no writable journal location, nothing is ever labelled.
**Commit:** `fix(confine): refuse to label when no journal can be written`

## 4. Atomic journal writes; unreadable journals are reportable residue; flush-failure proven fail-closed — ✅ DONE (2026-07-22)

NOTES (2026-07-22): three points beyond the item's literal text. (a) The temp file is flushed
(`Sync`) before the rename, so the guarantee covers a machine crash and not only a killed
process; a flush error refuses the box, which is the fail-closed direction. The temp file is
named `writing-*.tmp`, matching neither `labelJournalPrefix` nor `labelJournalSuffix`, so debris
a crash leaves behind can never be listed, read or reported as a journal. (b) An unreadable
journal is reported even though its owner cannot be identified — it might in principle be this
process's own file — because journals are now published atomically, so a live session's own
journal is never mid-write and an undecodable one is a genuine finding whoever wrote it. (c) The
unreadable finding carries its OWN trailer rather than extending the existing one: "a new session
reverts them automatically" is false for a journal no run can decode, so the manual remedy is
stated as the only one. The labels-half wording is byte-identical to before and a test pins it,
because `internal/probe` renders it verbatim.

**What:** (Review: Medium "truncate-in-place / invisible corruption" + High test gap "fail-closed
flush untested".) In `winconfine.go`: `writeLabelJournal` writes to a temp file in the journal
directory and `os.Rename`s into place. `confinementResidue` stops skipping an unreadable journal:
it reports it ("journal present but unreadable: <path>", extending `windowsResidueNotice` — keep
the function pure and the wording table-testable). `recoverLabelJournals`
(`confiner_windows.go:359–362`) continues to leave unreadable journals on disk (already true) —
now they are at least visible.
**Tests:** Untagged: residue wording rows for the unreadable case; write-then-read round-trip
through the rename path; a garbage journal file surfaces in `confinementResidue` output.
Windows-tagged (the review's missing fail-closed proof): pre-create `home/confinement` as a FILE
so `MkdirAll` fails ⇒ `Confine` returns `ErrConfinementUnavailable` AND `readLabelSDDL(root) ==
""` — proving both the flush-failure refusal and the journal-before-first-label ordering.
**Acceptance:** a crash mid-flush can no longer produce a half-written journal that recovery and
residue both ignore; the fail-closed flush path is pinned by a test.
**Commit:** `fix(confine): atomic journal writes and unreadable-journal residue`

## 5. DESIGN-CALL — `probe` reads residue before constructing the backend; reconcile the read-only pledge — ✅ DONE (2026-07-22)

NOTES (2026-07-22): the owner chose the recommended option — probe-path construction performs NO
recovery, the read-only pledge stays absolute, and no exception is added to ADR 0021, the README
or the command's Long text. Four deviations from the item's literal text. (a) The variant is
reachable from `cmd/apogee`, so the SELECTOR is exported — `platform.NewReportConfiner()`, defined
in all four `confiner_*.go` files (`NewConfiner()` verbatim on the three OSes whose construction
touches no disk); the recovery-free CONSTRUCTION it selects is the unexported
`newTokenConfinerWithoutRecovery`, which `newTokenConfiner` now wraps with the recovery pass.
(b) The Windows floor check moved into a shared `selectWindowsConfiner(build)` so the two
selectors cannot disagree about which hosts get the token backend — item 10's seam extraction
lands there instead of in `NewConfiner` itself. (c) The doc amendment landed in ADR 0020 §2/§3 and
`confinement-execution-contract.md` §9.2, not in ADR 0021: 0021's pledge is unchanged, and the
claims that had gone stale ("construction must not touch the disk", cited against
`platform.NewConfiner()`) live in the other two. (d) The "windows-tagged probe host path" proof is
an UNTAGGED `cmd/apogee` test (`TestProbeReportsConfinementResidueWithoutHealingIt`) — the probe
path is there, it redirects `%USERPROFILE%`/`$HOME` to plant a dead-PID journal, and it runs
natively on Windows, where it fails if the report constructor is swapped back (verified). A
windows-tagged constructor-level test pins the same rule at the backend seam.

**What:** (Review: High "`probe host` heals/destroys the residue it promises to report".) The
mechanical half is settled: in `cmd/apogee/probe.go`, capture
`residue := platform.ConfinementResidue()` into a local BEFORE `platform.NewConfiner()` is
evaluated, for both the bare `probe` and `probe host` paths. The design half needs the owner:
**Q:** on Windows, does probe-path construction keep performing crash recovery (then ADR 0021 §1,
README's "nothing is written", and `probe.go`'s Long text all gain the explicit recovery
exception, worded as ADR 0020 §2's remedy), or does the probe construct the backend WITHOUT
recovery (an unexported construction variant; the read-only pledge stays absolute; the session
constructor still heals on the next real run)? *Recommendation: the recovery-free probe variant —
it keeps three user-facing surfaces truthful, keeps the residue line meaningful (report, don't
heal), and after item 1 nothing is lost: the journal survives until a session run reverts it.*
Whichever way: the false comment at `probe.go:85–90` ("the host half stays read-only") is
corrected in this item, and the doc amendment (ADR 0021 §1 exception OR no exception) lands here
too — exactly one owning item.
**Tests:** Untagged: with a fake home containing a dead-PID journal, the report contains the
residue line (drive through `confinementResidue`/report assembly at whatever seam is cleanest).
Windows-tagged: plant a dead journal, run the probe host path; assert the output carries the
`labels:` residue line and — under the recommended option — the journal file still exists
afterwards.
**Acceptance:** on a host with an outstanding dead journal, `apogee probe` REPORTS it; ADR/README/
Long text and the code agree on whether the probe may write; no surface claims read-only while
recovery runs.
**Commit:** `fix(probe): report confinement residue before the backend can heal it`

## 6. The terminal pre-flight matches the target shell — ✅ DONE (2026-07-22)

NOTES (2026-07-22): the branch is the established raw-command-line convention —
`shellHost.CommandLine(line) == ""` means the platform hands the shell a real argv (POSIX sh),
non-empty means the line is delivered verbatim to cmd.exe (`exec_cmdline_*.go`) — so no Host
predicate was added and there is no second OS switch; `Execute` computes the raw line ONCE and
uses it for both the gate and `subprocessSpec.cmdline`. No cmd-side balanced-quote check: no
malformed-input class survives honestly (a trailing backslash, a caret, `%VAR%` and an
unbalanced quote are all legal to cmd). Two deviations. (a) "Existing terminal tests unchanged"
could not hold for `TestTerminal_EmptyAndUnparseableCommand`: it is UNTAGGED and asserted the
POSIX rejection on every host, so it failed natively on Windows once the gate stopped firing
there. Its unparseable half now asserts per shell family (POSIX ⇒ still rejected byte-identically;
cmd ⇒ no pre-flight rejection), which makes it a proof of the fix rather than a casualty; every
other terminal test is untouched. (b) The untagged "injected Windows rules" test injects only the
raw-command-line convention (a `platform.Host` wrapper overriding `CommandLine`) rather than a
whole Windows rule set: `windowsRules()` is unexported in `internal/platform`, and a full Windows
host would also swap the argv for `cmd /c`, which no Linux runner can execute — leaving the argv
real is what lets the two lines genuinely reach spec construction and the shell. That test is
deliberately not `t.Parallel()` because it substitutes the package-level `shellHost`.

**What:** (Review: High "POSIX splitter gates cmd.exe lines".) In
`internal/tools/terminal.go:74–80`, apply the `shlex.Split` gate only when the platform shell is
POSIX sh. For cmd.exe, skip the pre-flight entirely and let cmd report its own errors (recommended
— cmd has no stable quoting grammar worth pre-parsing; a balanced-double-quote check is acceptable
if the implementer finds a concrete malformed-input class it catches honestly). Derive the branch
from the existing `shellHost` (a Host predicate or the established raw-command-line convention —
read `exec_cmdline_*.go` first; do not add a second OS switch). Update the stale "shlex-validated"
wording in `docs/design/technical-design.md` §5 (P3.8 row) in this item.
**Tests:** Untagged, via injected Windows rules: `echo don't panic` and `dir "C:\Program Files\"`
pass the gate and reach spec construction; under POSIX rules both still fail exactly as today;
existing terminal tests unchanged. Windows-tagged: one end-to-end row in
`terminal_windows_test.go` running a quoted-path command natively.
**Acceptance:** ordinary cmd.exe lines with apostrophes or trailing quoted backslashes execute on
Windows; POSIX behaviour byte-identical.
**Commit:** `fix(tools): stop gating cmd.exe lines behind a POSIX parser`

## 7. The Job Object is released on the confine-failure and start-failure paths — ✅ DONE (2026-07-22)

NOTES (2026-07-22): `runSubprocess` is confirmed `runWithTeardown`'s only caller. Two deviations.
(a) The fake `processTeardown` implements the existing interface, but there was no INJECTION
point — `setProcessGroupTeardown` is a per-build-tag function — so the untagged test needed a
package-var seam, `newProcessTeardown = setProcessGroupTeardown` in `exec_teardown.go`, the same
idiom as `shellHost`; the tests that substitute it are deliberately not `t.Parallel()`. (b) A
third row was added beyond the item's two: a clean run must `contain` once and `release` EXACTLY
once — that count is what proves the redundant `defer td.release()` was deleted rather than
duplicated, which no early-exit row can see. All three rows were mutation-checked against the
pre-fix placement (both early-exit rows fail with `release called 0 times`).

**What:** (Review: Medium "handle leaks on the two routine early-exit paths".) In
`internal/tools/exec_common.go`, `defer teardown.release()` immediately after
`setProcessGroupTeardown(cmd)` — ownership moves to `runSubprocess` — and delete the now-redundant
`defer td.release()` inside `runWithTeardown` (`exec_teardown.go:74–79`; confirm `runSubprocess`
is its only caller first). `release` stays idempotent via the existing `InvalidHandle` guard.
**Tests:** Untagged: through a fake `processTeardown` at the existing interface, assert `release`
is invoked when (a) `Confine` fails (inject a failing confiner via `domain.ConfinementFromContext`
— the seam exists) and (b) `cmd.Start()` fails (nonexistent binary). Windows-tagged:
`exec_pgroup_other_test.go` suite unchanged and green natively.
**Acceptance:** no path through `runSubprocess` exits without `release` having run.
**Commit:** `fix(tools): release the Job Object on confine- and start-failure paths`

## 8. `probe model`'s auto-apply claim passes catalogue validation; startup promotion proven — ✅ DONE (2026-07-22)

NOTES (2026-07-22): three points of detail. (a) The `suppressed` line mirrors
`resolveValidatedSet`'s warning by carrying the catalogue's own reason verbatim (`validated.Validate`'s
error), not the warning's literal `apogee: skipping validated-set entry …` prefix — `Suppressed` is
rendered mid-sentence by `probe.Model.effectLine` ("this model now resolves at medium confidence,
but …"), where a second `apogee:` prefix would read as a stray notice. The rendered line is "…but
the next session start skips validated-set entry "k": <reason>; it is not applied". (b) Test (a) is
end-to-end through `runProbeModel` (which is what stores the record) AND asserts at the
`autoApplyKeys` seam; it was mutation-checked against the pre-fix code, where the report claims
`AUTO-APPLIES`. (c) Test (b) needs `opts.endpoint` set beside `baseOpts(gemmaKey)` — a probe record
is keyed on endpoint + label, so `resolveValidatedSet` cannot find it from the model id alone.
Both tests live in `probemodel_test.go`, next to the promotion tests they complete.

**What:** (Review: Medium "diverged duplicate of the startup ladder" + Medium test gap "startup
half untested".) In `cmd/apogee/probemodel.go` (`autoApplyKeys`, `:200`), add the missing
`validated.Validate(withRecord.Entry, mechanisms.Descriptors())` step with the same outcome
startup gives it: an entry that fails validation is NOT claimed as auto-applying — the report
names the skip in the `suppressed` wording, mirroring `resolveValidatedSet`'s warning
(`validatedsets.go:83–86`), so probe and startup can never disagree about the same entry. Record
the twin-ladder consolidation (one shared "would this entry apply" function) as a dated `TODO.md`
follow-on flagged for `/improve-codebase-architecture` — do NOT refactor it here.
**Tests:** Untagged, in `cmd/apogee`: (a) a user-local set entry naming a nonexistent mechanism ID
plus a stored probe record ⇒ `autoApplyKeys` claims nothing and the report names the invalid
entry; (b) the missing startup-side proof: `SaveProbeRecord` for (endpoint, shipped-set key), then
`resolveValidatedSet(baseOpts(key), userDir, probeDir)` asserts the set now APPLIES where the
existing `DirectLowMatchOffers` case only offers.
**Acceptance:** for any entry, `probe model`'s claim and the next session start's behaviour agree;
the record-present startup path is pinned by a test.
**Commit:** `fix(cli): probe model's auto-apply claim passes catalogue validation`

## 9. The pre-spend refusal gates are tested; the vacuous writes-nothing test is fixed — ✅ DONE (2026-07-22)

NOTES (2026-07-22): one deviation and one finding. (a) `{"data":[]}` does NOT reach
`errProbeModelNeedsLabel`: `provider.Discover` rejects an empty model list itself
(`discovery.go:92–94`, "server returned no models"), so the refusal that fires on that payload is
the discovery branch and the test row asserts THAT wording. Both rows (empty list, HTTP 500) were
mutation-checked against a build with the `derr != nil` return deleted — both then fail, reporting
the label refusal instead, so the discovery gate is genuinely pinned. (b) Consequently
`errProbeModelNeedsLabel` (`probemodel.go:105–110`) is unreachable through `Discover` today:
`toModelInfo` drops id-less entries, and a list that ends up empty is an error, so no `/v1/models`
payload can yield a nil error with an empty `ActiveModel`. It is a defensive gate no test can
exercise without a production seam, which this tests-only item may not add — left as is and
recorded here. `TestGatherModelWritesNothing` was DELETED (the item's recommended outcome): it
asserted an unrelated `t.TempDir()` was empty while `GatherModel` takes no path, and the honest
contract is covered by `TestProbeModelNoSaveWritesNothing` in `cmd/apogee`.

**What:** (Review: Mediums "refusal gates untested" + "`TestGatherModelWritesNothing` asserts
nothing".) Tests only, no production change. Cover `probemodel.go:98–110`: an `httptest` server
whose `/v1/models` returns `{"data":[]}` ⇒ running `probe model` without `--model` fails with the
"advertises no active model" error, records ZERO `/chat/completions` hits, and leaves the config
home untouched; plus the discovery-failure branch (server returning 500). Delete
`TestGatherModelWritesNothing` (`internal/probe/model_test.go:60–71`) — its contract is already
honestly covered by the cmd-level `TestProbeModelNoSaveWritesNothing` — or rewrite it to assert
against a redirected `HOME`/CWD; deletion with a one-line NOTES justification is the recommended
outcome.
**Tests:** are the item. **Acceptance:** `go test ./internal/probe/... ./cmd/apogee/...` green;
the two refusal branches show as covered paths (a failing mutation of either branch is caught).
**Commit:** `test(probe): cover the pre-spend refusal gates`

## 10. The below-floor selection gets a seam and a table test — ✅ DONE (2026-07-22)

NOTES (2026-07-22): the caller is `selectWindowsConfiner` (`confiner_windows.go:131`), not
`NewConfiner` — item 5 moved the floor check there so the session and report selectors cannot
disagree, and its NOTES already assign this item's extraction to that spot. The production diff is
the one line the acceptance criterion describes: the inline `buildNumber < windowsFloorBuild`
becomes `belowWindowsFloor(buildNumber)`. The table rows use the literal builds the item names
(17762/17763/26200) rather than expressions over `windowsFloorBuild`, so the floor's VALUE is
pinned too and a silent edit of the constant fails the test.

**What:** (Review: Medium "floor gate has no seam".) Extract the decision at
`confiner_windows.go:112` into an untagged pure predicate in `winconfine.go` — e.g.
`belowWindowsFloor(build uint32) bool` beside `windowsFloorBuild` — and have `NewConfiner` call
it. No behaviour change.
**Tests:** Untagged table: 17762 ⇒ true, 17763 ⇒ false, 26200 ⇒ false. Windows-tagged: existing
selection tests unchanged.
**Acceptance:** the deny-vs-token selection logic is provable on every OS; `NewConfiner` diff is
two lines.
**Commit:** `test(confine): pin the Windows build-floor selection`

## 11. Adversarial quoting rows; delete the caller-less `ExecExt` — ✅ DONE (2026-07-22)

NOTES (2026-07-22): the item said "add rows"; the rows could not be written truthfully without
first fixing what they describe, so this item also changes `windowsQuote`. Measured natively on the
owner's Windows host (`CommandLineToArgvW` round-trip AND end-to-end through a real `cmd /c`),
across 27 adversarial values:

- The shipped `windowsQuote` doubled only a TRAILING backslash run, so a run touching an embedded
  quote was eaten: `a\"b` reached the child as `a"b`, `a\\"b` as `a\"b`, `say "hi"\\` as
  `say "hi\\`. A first attempt at this item wrote those three outcomes into the table as the
  expected answers, with a rationale comment inverting the rule. The user's ratified decision
  (2026-07-22) is to fix the quoting instead and assert the correct round-trip.
- Doubling every backslash run that touches a quote is necessary but NOT sufficient, and this is
  the deviation from the decision's literal wording: measured, it fixes `a\"b` and `a\\"b` and
  leaves `say "hi"\\` still arriving as `say "hi\\`. The cause is the `""` escape itself —
  `CommandLineToArgvW` decodes `""` inside a quoted argument to a literal quote but LEAVES quoted
  mode, so every later space splits the argument. The standard MSVCRT form the decision names
  (`2n+1` backslashes, then `\"`) round-trips all 27 values through `CommandLineToArgvW`.
- That form alone would have introduced a worse defect than the one being fixed: `\"` is still a
  quote to `cmd`, which toggles out of its own quoted region, so a value carrying both a quote and
  an `&` or `>` reached `cmd` as a live command separator or redirect (measured: `a"b & c"d` ran a
  second command). `windowsQuote` therefore caret-escapes a value that contains a quote — every
  metacharacter including the quotes, so `cmd` never enters quote mode at all. Output for a
  quote-free value (every production and test caller quotes filesystem paths) is byte-identical to
  before; only the values that were broken change.
- The table rows are backed by `TestWindowsQuoteRoundTripsThroughCmd` (new, windows-tagged), which
  re-runs the same values through a real `cmd /c` into a sentinel child — the `TestHelperProcess`
  idiom `landlock_linux_test.go` already uses, which needed a windows-tagged `TestMain`. Verified
  by negative control: single-escaping the backslash run fails the two backslash rows, and removing
  the caret branch fails the two metacharacter rows. `%PATH%` is pinned twice — as the documented
  non-guarantee row, and natively as `TestWindowsQuoteDoesNotNeutraliseEnvironmentExpansion`.
- The `ExecExt` deletion also removes the now-unreachable `hostRules.execExt` field and its two
  initialisers, and rewords the `Path`/`hostRules`/`posixRules`/`windowsRules` doc comments — the
  interface method alone would have left a field no code can read.
- Docs corrected because they now assert something false, not as drive-by edits: the `[Unreleased]`
  CHANGELOG platform paragraph and `technical-design.md` §5's P5.6 cell (both the retired `ExecExt`
  and the quoting claim), `Shell.Quote`'s interface comment, and the stale `Path{ExecExt, Contains}`
  at `implementation-plan-apogee-merge.md:449` named in the decision. Item 13 still owns the
  roll-up CHANGELOG entry.

**What:** (Review: Mediums "windowsQuote lacks adversarial rows" + "`ExecExt` is dead surface".)
Two mechanical strokes in `internal/platform`: (a) extend the `windowsQuote` table
(`host_test.go:78–87`) with: a backslash run immediately before an embedded quote (`a\"b`),
multiple trailing backslashes, cmd metacharacters (`&`, `|`, `^`, `>`) inside quotes, and a row
PINNING the documented non-guarantee that `%VAR%` is not neutralised (promoting the comment at
`host.go:328–332` from prose to a test row stating the intent); (b) delete `Path.ExecExt`
(`platform.go:63–67`, `host.go:96–97`) and its test rows — zero production callers; it returns
with its first real caller (the plan-item-6 acceptance rule the Phase 5 run itself set).
**Tests:** are stroke (a); stroke (b) is a deletion whose proof is the build.
**Acceptance:** `go build ./... && go vet ./... && go test ./internal/platform/...` green on
Linux and natively on Windows; grep for `ExecExt` finds nothing.
**Commit:** `test(platform): adversarial quoting rows; drop the caller-less ExecExt`

## 12. Record the confined-`%TEMP%`/writable-paths gap where users and planners will find it — ✅ DONE (2026-07-22)

NOTES (2026-07-22): the README caveat is three sentences rather than the item's literal one — the
bare claim ("workspace-scoped writes only") reads as a scoping nicety without the reason a Low
process cannot write to an unmarked directory at all, which is what makes `go build` / `pip` /
`npm` fail outright rather than degrade. It is appended to the existing "**On Windows the fence is
a token…**" paragraph, after its "two things worth knowing" list, and names `TODO.md` as the
follow-on's home. No other doc touched.

**What:** (Review: Medium "no `%TEMP%` story, `ConfineWritablePaths` has no writer" — docs only,
per Settled design.) Add a named, dated `TODO.md` follow-on: *"Windows Auto: box-local `%TEMP%` /
toolchain caches"* — carrying the design question (box-local `%TEMP%` via `ScopeEnv` on the
confined path vs labelling the user's cache/temp trees), the anchors (ADR 0020 §2 "hard
prerequisite" paragraph, contract §7, `internal/agent/dispatch.go:121–125` as the sole
`ConfineWritablePaths` reader with no writer), and the consequence (confined `go build` / `pip` /
`npm` fail inside the fence today). Add one caveat sentence to README's Windows Auto material:
today's Windows fence covers workspace-scoped writes; toolchain temp/cache work under Auto is a
recorded follow-on. Keep both wordings matter-of-fact — ADR 0020's "must not be soft-pedalled"
applies.
**Tests:** none (docs). **Acceptance:** TODO.md carries the follow-on with anchors; README no
longer implies confined toolchain builds work on Windows; no other doc touched.
**Commit:** `docs: record the confined-%TEMP% prerequisite as a follow-on`

## 13. Roll-up: CHANGELOG, review cross-reference, full gates on both machines — ✅ DONE (2026-07-22)

NOTES (2026-07-22): one deviation, and it is the item's second host. **The Linux devbox pass was
not run** — the devbox is a separate machine and this Windows host has no WSL, no container
runtime and no sanctioned network path to it, so `make check` there remains an **outstanding
owner action**, recorded under *Phase-5 verification leftovers* in `TODO.md`. Everything reachable
from the Windows host was run and
is green: `go vet ./...`, `go build ./...`, `go test -count=1 ./...` (only the three pre-existing
host failures — `TestSaveHostAcknowledgement_PreservesTheFileMode`,
`TestAutofixRepairsBrokenContentWhenFormatterImproves`, `TestFoldActivityClockRunsPerPhrase`; the
two `TestDiagnostics_…GoVet…` failures Ground truth predicted did NOT reproduce), all six cross
targets, the ADR-0010 import check, `apogee --help`, and `gofmt -l` over LF copies of every Go file
this plan touched. As the closest available proxy for the unreachable host, `GOOS=linux go vet
./...` and `GOOS=darwin go vet ./...` are clean — that type-checks and vets the landlock- and
seatbelt-tagged code **and their test files**, so the Linux gap is execution only, not compilation.

NOTES (2026-07-22): the owner **ratified closing this item without the devbox pass**. The Linux
devbox is unreachable from this host, and the accepted proxy for its `make check` is the native
Windows gate above plus `GOOS=linux`/`GOOS=darwin go vet ./...` and the six cross-compile targets —
a proxy that proves compilation and vetting of the landlock-tagged code, not its execution. That
is a knowingly accepted gap, not a green light: the outstanding devbox `make check` is tracked as
a dated owner action in `TODO.md` (*Phase-5 verification leftovers* → "Closeout Linux pass"), and
this item's literal "both hosts green" acceptance is therefore met on one host only. The gate was
re-run at close-out and is unchanged: vet/build clean, the same three pre-existing host test
failures and no others, six cross targets OK, ADR-0010 import check clean, `apogee --help` exit 0.

**What:** (Closes the plan — run last.) CHANGELOG `[Unreleased]`: one Fixed block summarizing the
review fixes (journal lifecycle: survives failed revert, own-label guard, journal-or-refuse,
atomic writes; probe residue ordering + the item-5 decision; cmd.exe pre-flight; Job Object
release; probe/startup auto-apply parity), citing `docs/reviews/code-review-2026-07-22.md`.
Verify every item above is marked done with its NOTES trail. Full gate NATIVELY on the Windows
host (`go vet ./...`, `go build ./...`, `go test -count=1 ./...` — modulo the four pre-existing
failures listed in Ground truth — plus all six cross targets); then `make check` on the Linux
devbox (landlock-tagged tests) before this item is marked done.
**Tests:** the gates are the tests. **Acceptance:** both hosts green; CHANGELOG entry present;
plan file fully marked; plan is ready for the owner's archive pass.
**Commit:** `docs: roll up the Phase 5 review fixes`

---

## Explicitly NOT in this plan

- Building the `%TEMP%`/writable-paths box construction (item 12 records it; own design session).
- Consolidating the probe/startup validated-set twin ladders (item 8 records it for
  `/improve-codebase-architecture`).
- The uncertain `Confine`-vs-`Close` race noted in the review (not cross-validated; revisit only
  if a `-race` run on the Windows host with an overlapping cancel reproduces it).
- The owner-run live-enforcement checklist from Phase 5 (live Auto session on Windows,
  below-floor host, macOS cross-binary smoke) — unchanged, still owner-run, and deliberately
  scheduled AFTER this plan lands.
