# Implementation plan — Phase 5 second review fixes (journal edge symmetry, probe claim parity)

**Date:** 2026-07-22. **Status: PLAN — not started.** Execute with `/implement-plan` in a fresh
session **on the owner's Windows machine** (native toolchain, NOT WSL — most items carry
windows-tagged tests that must run natively), forwarding skills: `coding-standards` (mandatory
for all new Go). One sub-agent per numbered work item, verifier before commit, items marked done
with a ✅ in the item heading of this file.

**Scope source:** `docs/reviews/code-review-2026-07-22-phase5-post-fixes.md` — the post-fixes
Phase 5 review (2 High, 12 Medium). Item text below is self-contained; the review is the finding
record, not an authority. **Precedence for design questions:** ADR 0012,
[ADR 0020](../adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md),
[ADR 0021](../adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
(incl. the 2026-07-22 Amendment) and `docs/design/confinement-execution-contract.md` govern
everything confinement- and probe-shaped. If an artifact produced by an earlier item of THIS plan
disagrees with those sources, the sources win — stop and consult, don't propagate.

## Why

The first review-fixes pass hardened the label journal's *happy-path* lifecycle (journal before
label, keep on failed revert, atomic writes, refuse without a home). This second review finds the
remaining defects at the lifecycle's **edges**: a descendant whose prior label cannot be read is
labelled with no journal entry (a foreign label destroyed with no record); teardown retires the
journal without verifying the revert below the root; a prior-labelled file the agent deleted makes
`Close` fail every session forever; a failed root label write strands a phantom journal entry that
alarms forever; and two concurrent sessions on one workspace un-fence each other. Separately, the
review's strongest cross-validated finding: `probe model`'s auto-apply claim STILL diverges from
startup — the defect class commit `fb9dec2` fixed for catalogue validation recurs at two more
rungs of the identity ladder (unpinned model, weights-file label). And the single most important
missing test: nothing anywhere proves a foreign prior label is actually restored end-to-end — the
one behaviour the journal machinery exists to deliver.

## Ground truth (verified 2026-07-22 — anchors, not vibes)

- **Build-tag split (decides where each item's tests run):** `internal/platform/winconfine.go`
  is UNTAGGED — `journalLabelEntry` (`:188`), `windowsLabelGuardrail` (`:314`),
  `confinementJournalHome` (`:354`), `retireLabelJournal` (`:438`, takes the revert as a
  `func(labelJournal) error` — the injected seam), `listLabelJournals` (`:467`).
  `internal/platform/confiner_windows.go` is `//go:build windows`. `internal/platform/host.go`
  is untagged (both rule sets compile everywhere); `platform_windows.go` is windows-tagged.
- **The journal lifecycle as shipped:** `labelBox` reads the root's prior, journals it
  (`Root: true`), then `labelTree` (`confiner_windows.go:276-287`). `labelTree` labels the root
  fail-closed (`:317-318`), then walks: reparse points skipped (`:334-339`); a descendant's
  prior is journalled ONLY when `readLabelSDDL` succeeds AND is non-empty (`:340-347`) — **on a
  read error it falls through and labels anyway** (`:352`), unjournalled. `restoreLabels`
  (`:363-371`) → `retireLabelJournal` → `revertLabelJournal` (`:388-401`): clears every root's
  tree first, then restores priors (`:395-399`) — **the prior-restore loop does not tolerate
  `os.IsNotExist`**, unlike `clearLabelTree`'s root (`:407-410`, comment "a path that has since
  been deleted is not an error"). `clearLabelTree`'s walk **swallows every descendant failure**
  (`:413-425`: `walkErr != nil → return nil`, `_ = setLabelSDDL(...)`), so it returns nil even
  when labels remain — and `retireLabelJournal` then deletes the journal. `recoverLabelJournals`
  (`:441-453`) skips undecodable journals (`continue`) and live PIDs (`processAlive`, `:457`).
- **The phantom-entry path:** `labelBox` flushes the root's journal entry BEFORE `labelTree`
  labels it (`:280-284` — correct order). If `setLabelSDDL(root, …)` then fails (`:317`), the
  box is refused but the entry stays; `clearLabelTree(root)` at Close fails for the same
  non-`IsNotExist` reason, so the journal is never retired and `ConfinementResidue` reports
  residue for a disk carrying no label.
- **The concurrent-session hole:** session B walking a tree A already labelled reads A's Low
  label as the prior; the own-label guard correctly journals no restore (comment at `:341-343`),
  and B's root entry gets an EMPTY prior (own-label recognised), so B's journal still names the
  root `Root: true` — the clear obligation transfers to B naturally. But A's `Close` →
  `clearLabelTree` strips the shared root while B runs; B's `c.labelled` memo says the root is
  done, so B never re-labels and every later confined write in B is denied.
- **Probe claim vs startup:** startup is `resolveValidatedSet` (`cmd/apogee/validatedsets.go:34`)
  → `library.ResolveFingerprintFrom(Sources{ModelID: opts.model, Endpoint, ProbeDir})`
  (`:42-46`); call sites `cmd/apogee/wire.go:242` and `internal/agent/loop.go:161`. The ladder is
  weights-hash (reachable file) → stored probe record → bare label. `probe model`'s claim is
  `autoApplyKeys` (`cmd/apogee/probemodel.go:202-244`): it calls
  `validated.Match(m.Fingerprint.Label, ConfidenceMedium, …)` directly — no ladder. The probed
  label falls back to the server's discovered active model when `--model` is unset
  (`probemodel.go:97-111`), but startup passes `opts.model` — EMPTY in that flow, so
  `ResolveFingerprintFrom` returns the zero fingerprint and `resolveValidatedSet` applies
  nothing. And a `--model` naming a reachable weight file resolves at High
  (`internal/library/fingerprint.go` documents the path case at `:22`), never reading the record.
  Off-switches `opts.bypass` / `!opts.validatedSetsEnable` / `len(opts.mechanisms) > 0`
  (`probemodel.go:206-224`) are handled but UNTESTED at this seam; only the catalogue-defect
  branch is pinned (`TestProbeModelDoesNotClaimAnEntryStartupWillSkip`,
  `probemodel_test.go:286`).
- **Probe report honesty anchors:** `effectLine` (`internal/probe/model.go:244-264`) — the
  no-record branch (`:246-247`) claims "identity stays at the label tier" even when an earlier
  SAVED record survives and still resolves Medium; `recordProbeFingerprint` already loads the
  previous record (`probemodel.go:166`) but discards `LoadProbeRecord`'s returned warning —
  its comment says "LoadProbeRecord already reports why", which is false (the warning is
  returned for the caller; only startup's path prints it) — while the command's Long text
  (`probemodel.go:69-71`) promises v1 records "are skipped with a warning".
  `--workspace` (`probemodel.go:132-133`) feeds `resolveRoots(opts.configDir, opts.workspace)`
  (`:82`) whose `roots.workspace` the model path never reads (only `roots.probe` and
  `roots.validated`); `probe host` DOES read it (`probe.go:95`).
- **Path-rule anchors:** `split`'s short-name branch (`internal/platform/host.go:186-193`)
  rejects when the resolved result still `hasShortName` — but for a directory GENUINELY named
  like a short name (`demo~1`), `GetLongPathName` returns the input unchanged (it IS the long
  name), so an authoritative success is misread as failure and `windowsLabelGuardrail` refuses
  the box. `longPathName` (`platform_windows.go:38-53`) returns the input unchanged BOTH on
  total failure and on nothing-to-expand — the two cases are indistinguishable to `split` today.
  The guardrail and `Contains` are purely lexical: a root spelled `C:\Windows.` (trailing dot)
  or a junction targeting a protected location passes the check, and `SetNamedSecurityInfo`
  follows OS canonicalization/the reparse point. Descendant reparse points are skipped
  (`confiner_windows.go:334-339`); only the ROOT label call (`:317`, and `clearLabelTree`'s
  `:407`) follows one.
- **Windows-host environment facts (unchanged from the prior two plans):** `make` is absent —
  run the underlying commands (`go vet ./...`, `go build ./...`, `go test -count=1 ./...`, six
  cross targets, `--help` exit 0, the ADR-0010 grep). The checkout is `core.autocrlf=true`, so
  check gofmt against LF copies of changed files only. Three test failures are pre-existing on
  this host (`TestSaveHostAcknowledgement_PreservesTheFileMode`,
  `TestAutofixRepairsBrokenContentWhenFormatterImproves`, `TestFoldActivityClockRunsPerPhrase`) —
  not caused by, and not to be fixed by, this plan. The Linux devbox remains unreachable; its
  `make check` is already tracked in `TODO.md` (*Phase-5 verification leftovers*) and this plan
  adds to that debt only via `GOOS=linux|darwin go vet ./...` as the accepted proxy.

## Settled design (do not re-litigate in work items)

- **ADR 0020's shape is fixed.** The facility stays the restricted Low token; the box stays
  labels-on-disk; the journal home stays `confinementJournalHome()` (`~/.apogee`), independent
  of `--config`; teardown stays the optional `io.Closer` asserted at the composition root;
  `domain.Confiner` does NOT grow a method.
- **Fail closed, with an undo record — always.** No code path may apply a label without a
  persisted journal entry first (this plan extends that to the unreadable-prior descendant), and
  no code path may retire a journal whose labels are not verifiably reverted (this plan extends
  that below the root). Where a prior state is ambiguous, clearing to unlabelled (implicitly
  Medium) beats restoring a Low label — less privilege is the safe direction.
- **A deleted path is a completed revert, not a failure.** `clearLabelTree`'s root already says
  so ("the tree is being restored, not reconstructed"); items 3 and 4 apply the same posture to
  the prior-restore loop and the phantom root entry. Tolerating `IsNotExist` is honesty, not
  leniency: a path that no longer exists carries no label to revert.
- **The probe report may never promise an effect startup will not deliver** (ADR 0021 §4). The
  fix direction for item 6 is parity-by-construction — the claim must be computed by the same
  decision startup runs, not by a parallel re-implementation that happens to agree today.
- **The read-only probe pledge (ADR 0021 §1) is binding.** No item here touches the probe host
  path's construction; nothing may quietly add probe-path writes.
- **Item 9's guardrail gap is invariant hygiene, not an emergency.** The box roots come from
  trusted config; the confined Low child cannot create the precondition. Fix it because the
  stated invariant ("refuse to label protected locations") must hold for every spelling, not
  because the fence's adversary can reach it.
- **Windows semantics stay table-testable off-Windows** via injected seams (fold flags, env
  funcs, injected resolvers/reverts — the `internal/present` pattern); native runs are
  additional proof, never a replacement. **Follow existing idiom religiously** — comment density
  and `doc.go` conventions are load-bearing. ADR 0010: `internal/*` depends only on
  `internal/domain` downward. No AI attribution in commits.

## Work items

Each item is one sub-agent's task: read the named files first, implement, test, `go vet` + run
the package tests, then mark the item done here. Any authorized deviation from item text lands as
a dated `NOTES (YYYY-MM-DD):` line under the item. Review finding IDs refer to
`docs/reviews/code-review-2026-07-22-phase5-post-fixes.md`.

## 1. The foreign-prior restore path gets its end-to-end proof — ✅ DONE (2026-07-22)

NOTES (2026-07-22): Both mutation checks demonstrated natively
(`TestWindowsForeignPriorLabelIsRestoredOnTeardown`): (1) with `revertLabelJournal`'s
prior-restore loop deleted the test FAILED ("the foreign Medium label was cleared, not
restored"); (2) with the clear/restore order swapped (restore first, clear second) the test
FAILED with the same assertion — the walk wipes the just-restored prior. Both mutations
reverted; `confiner_windows.go` is byte-identical to HEAD (`git diff` empty).

**What:** (Review: High "foreign-prior restore untested end-to-end" — tests only, and run FIRST:
it is the safety net under items 2–5, which all touch the code it pins.) Windows-tagged, in
`internal/platform/confiner_windows_test.go`: create a fresh box, apply a foreign explicit
Medium label to a file inside it (`setLabelSDDL(child, "S:(ML;;NW;;;ME)")` — the same helper the
backend uses), run the label pass (`labelBox` or `Confine` on a real command spec, whichever the
existing tests' idiom uses), assert the journal entry for that child carries the Medium prior
verbatim, then `Close()` and assert `readLabelSDDL(child)` returns the Medium descriptor — NOT
`""` — while the root and a sibling file read back unlabelled. Verify by negative control
(mutation-check, the house standard): with `revertLabelJournal`'s prior-restore loop deleted the
test must fail, and with the clear/restore order swapped (restore first, clear second) it must
also fail — those are the two silent regressions the review names, and both currently pass the
entire suite.
**Tests:** are the item.
**Acceptance:** `go test ./internal/platform/...` green natively; both mutation checks
demonstrated in the item's NOTES.
**Commit:** `test(confine): pin the foreign-prior label restore end-to-end`

## 2. A descendant whose prior label cannot be read is not labelled — ✅ DONE (2026-07-22)

NOTES (2026-07-22): Mutation check demonstrated on the untagged table: with
`descendantLabelDecision`'s read-error rung reverted to "label anyway" (the pre-fix
behaviour), `TestDescendantLabelDecision`'s two error rows FAIL; reverted, all green. One
finding on the windows-tagged test (`TestWindowsUnreadablePriorDescendantIsNotLabelled`,
implemented as specified and passing natively): on this host `SetNamedSecurityInfo` demands
READ_CONTROL even for the LABEL write, so under the deny-READ_CONTROL DACL the OLD code's
label write would have failed silently too — the tagged test pins the wiring and the
no-label/no-entry outcome, while the skip-vs-attempt distinction is pinned by the untagged
table. The child's DACL grants 0x1d0080
(WRITE_DAC|WRITE_OWNER|DELETE|SYNCHRONIZE|FILE_READ_ATTRIBUTES) via an OWNER_RIGHTS ACE —
CreateFile implicitly requests the last two — and the test restores the DACL through a
WRITE_DAC handle (`SetKernelObjectSecurity`), because the named-object API cannot write back
a DACL it is not allowed to read.

**What:** (Review: Medium "unreadable prior ⇒ labelled unjournalled".) In `labelTree`
(`internal/platform/confiner_windows.go:340-352`), a `readLabelSDDL(path)` ERROR must take the
tolerated-failure rung — skip the path entirely (no label, no journal entry) — instead of
falling through to `setLabelSDDL`. Today a descendant carrying a foreign label whose read fails
(WRITE_OWNER granted, READ_CONTROL denied) is relabelled Low with no journalled prior, and
teardown clears it to unlabelled: a foreign security label destroyed with no record, violating
"no label lands without a journal entry first" at the descendant level. The consequence for the
session is the documented one for tolerated descendants: that one path stays read-only (or in
this case possibly writable-by-prior-label) to the confined child; it must not gate the box.
Extract the three-way decision (readable prior / empty prior / read error ⇒ journal+label /
label / skip) into an untagged pure helper in `winconfine.go` so it is Linux-table-testable —
the `retireLabelJournal` seam pattern.
**Tests:** Untagged: table rows over the extracted decision (error ⇒ skip both; foreign prior ⇒
journal then label; empty prior ⇒ label only). Windows-tagged: a child with a DACL denying
READ_CONTROL to the current user inside a box — after the label pass it carries NO Low label and
the journal has NO entry for it; item 1's restore test still green.
**Acceptance:** no path through `labelTree` can mutate a label it could not first read and
journal.
**Commit:** `fix(confine): never label a descendant whose prior label cannot be read`

## 3. The revert is verified below the root before the journal is retired — ✅ DONE (2026-07-22)

NOTES (2026-07-22): The below-root failure accounting is extracted as the untagged pure
helper `clearTreeOutcome` (`winconfine.go`) — the item named no helper, but that is what
makes the accounting Linux-table-testable (`TestClearTreeOutcome`), matching the
retireLabelJournal seam pattern; `clearLabelTree` counts descendant walk/clear failures
(tolerating only `os.IsNotExist`) and hands them to it. Both mutation checks demonstrated
natively: (a) with the verdict forced to `clearTreeOutcome(root, 0, nil)` (the pre-fix
swallow-everything behaviour), `TestWindowsUnclearableDescendantKeepsTheJournal` FAILED
("Close reported success while a descendant kept its Low label"); (b) with the
`!os.IsNotExist` tolerance removed from the prior-restore loop,
`TestWindowsDeletedPriorLabelledPathDoesNotWedgeTheRevert` FAILED (Close errored on the
vanished path). Both mutations reverted; full platform suite green after each revert.

**What:** (Review: Mediums "journal retired without verifying the revert" + "deleted
prior-labelled path makes the journal permanently un-retirable" — one item, both make
`retireLabelJournal`'s decision honest.) Two strokes in
`internal/platform/confiner_windows.go`:
(a) `clearLabelTree` (`:406-426`) stops swallowing descendant failures: count walk errors and
`setLabelSDDL` failures on descendants — tolerating ONLY `os.IsNotExist` (the "restored, not
reconstructed" posture the root already takes) — and return an error naming the first failure
and the count when any remain, so `retireLabelJournal` keeps the journal and the next
session/recovery retries. Today a subtree unreadable at teardown (a directory whose DACL the
confined child rewrote — it owns in-box objects) strands Low labels on disk with no residue
report, against "a journal is deleted only after its labels are verifiably reverted".
(b) `revertLabelJournal`'s prior-restore loop (`:395-399`) skips `os.IsNotExist` from
`setLabelSDDL`: a prior-labelled file the agent deleted or renamed (routine workspace activity)
currently fails the revert FOREVER — `Close` warns every session, recovery retries and fails
silently every startup, and `ConfinementResidue`'s "a new session reverts them automatically"
becomes a false promise whose only remedy is manually deleting the journal.
**Tests:** Untagged where the failure accounting can be driven through the
`retireLabelJournal`/injected-revert seam. Windows-tagged: (a) a box whose descendant's clear is
forced to fail (deny-WRITE_OWNER DACL on a child) ⇒ `Close` errors AND the journal file
survives; (b) journal a foreign prior for a child, delete the child, `Close` ⇒ nil error, journal
retired, disk clean. Item 1's restore test still green.
**Acceptance:** `retireLabelJournal` deletes a journal only when every label it describes is
verifiably gone or restored; a vanished path no longer wedges the lifecycle.
**Commit:** `fix(confine): verify the revert below the root; tolerate deleted priors`

## 4. A failed root label write unwinds its own journal entry — ✅ DONE (2026-07-22)

NOTES (2026-07-22): The unwind decision is the untagged pure helper `unwindLabelEntry`
(`winconfine.go`, table-tested by `TestUnwindLabelEntry`); the root-write-vs-later-failure
split is `labelTree`'s new `rootLabelled` return, and the unwind fires only for an entry
`journalLabel` newly added — the item's "just-journalled", so an entry predating the attempt
(whose root label may really be on the disk from an earlier partial pass) is never removed.
Mutation check demonstrated natively: with labelBox's unwind call disabled (the pre-fix
behaviour), `TestWindowsFailedRootLabelWriteUnwindsItsJournalEntry` FAILED on all three
fronts — phantom entry in memory, on disk, and `confinementResidue` alarming over the
never-labelled root; mutation reverted, full platform suite green after the revert.

**What:** (Review: Medium "phantom journal entry alarms forever".) In `labelBox`
(`internal/platform/confiner_windows.go:276-287`): when `labelTree(root)` fails on the ROOT
label write (`:317-318` — the box is refused, nothing was mutated) and the just-journalled root
entry recorded NO foreign prior, remove that entry and re-flush before returning the error.
Journal-before-label is correct and stays; but an entry describing a mutation that never
happened turns every later `Close` and recovery into a failing no-op (clearing a label that is
not there fails non-`IsNotExist` on the same unwritable root) and `apogee probe` reports
Low-label residue for a disk carrying no label — a persistent false alarm that trains the user
to ignore the real one. An entry WITH a foreign prior is kept — ambiguity resolves toward
keeping the record (Settled design). Note the interaction with item 3: once clears tolerate
nothing-to-clear only via `IsNotExist`, the unwritable-root case cannot self-heal, which is why
the unwind must happen at the source.
**Tests:** Untagged: the unwind decision (entry has empty prior + root write failed ⇒ remove) as
a pure-helper table if extracted, else through the journal round-trip. Windows-tagged: a root
whose label write fails (deny-WRITE_OWNER DACL) ⇒ `Confine` returns
`ErrConfinementUnavailable`, the journal contains no entry for it (or the file is gone when it
was the only entry), and `ConfinementResidue` reports nothing.
**Acceptance:** a refused box leaves no journal debris; residue is reported only for labels that
exist.
**Commit:** `fix(confine): unwind the journal entry when the root label write fails`

## 5. Teardown never clears a root a live sibling session still uses — ✅ DONE (2026-07-22)

**What:** (Review: Medium "two concurrent sessions un-fence each other".) When session A and
session B confine the same workspace, B correctly journals no restore for A's own-label priors
and B's root entry carries an empty prior — the clear obligation already transfers to B (Ground
truth). But A's `Close` → `clearLabelTree` strips the shared root while B runs, and B's
`c.labelled` memo prevents re-labelling: every later confined write in B is denied for the rest
of the session. Fix at teardown AND recovery: before clearing a root, skip any root that is
named in ANOTHER journal in the same home whose owning PID is alive (`listLabelJournals` +
`readLabelJournal` + `processAlive` all exist — `winconfine.go:467`, `confiner_windows.go:457`).
A skipped root does not fail the revert: the live sibling's journal owns the obligation, so A's
journal may still retire (record that reasoning in the code comment). Put the exclusion decision
(this journal's roots minus roots named by live siblings) in an untagged pure helper in
`winconfine.go` taking the sibling journals and a liveness func — table-testable on Linux.
`recoverLabelJournals` already skips whole LIVE journals; this item extends the same respect to
root OVERLAP between a dead (or closing) journal and a live one.
**Tests:** Untagged: table over the exclusion helper (no siblings ⇒ all roots; live sibling
shares a root ⇒ that root excluded, others kept; dead sibling ⇒ nothing excluded).
Windows-tagged: plant a sibling journal naming the same root with the PID of a spawned
long-lived child process; `Close` the backend ⇒ the root's label survives and the backend's own
journal is retired; kill the child, run recovery ⇒ the root is cleared.
**Acceptance:** in the two-terminal setup, the surviving session's box stays fenced and
writable; nothing is stranded once both sessions end.
**Commit:** `fix(confine): leave a live sibling session's box labels in place`

## 6. DESIGN-CALL — `probe model`'s claim runs startup's own ladder — ✅ DONE (2026-07-22)

NOTES (2026-07-22): Owner picked the consolidation. The shared function is `startupSetDecision`
(`cmd/apogee/validatedsets.go`) — the whole ladder including `library.ResolveFingerprintFrom`;
`resolveValidatedSet` renders/enacts it, `autoApplyKeys` reports it. Two consequences beyond the
item's literal text: (1) `autoApplyKeys` gained a `probeDir` parameter and the claim is now
computed AFTER a successful `SaveProbeRecord` — the with-record answer is startup's ladder run
against the disk as the next session will find it, and the counterfactual is the same call with
the record rung removed (empty probe dir); under `--no-save` or a failed write the
`AutoApply`/`Promoted`/`Suppressed` fields stay empty (previously computed but never rendered —
`effectLine` gates on `Requested && Written`). (2) `TestProbeModelDoesNotClaimAnEntryStartupWillSkip`
is green with assertions unchanged, but its direct `autoApplyKeys` call carries the new
`roots.probe` argument (the record the probe run itself stored supplies the Medium rung).
Mutation checks (a) and (b) demonstrated: keying the claim on the probed label and dropping the
weights rung fails both new tests. `TODO.md`'s "Validated-set twin ladders" entry marked DONE.

**What:** (Review: High "auto-apply claim diverges from startup", cross-validated by two lenses;
plus Medium "off-switch branches untested".) `autoApplyKeys`
(`cmd/apogee/probemodel.go:202-244`) assumes the probed label resolves at Medium; startup
(`resolveValidatedSet` → `library.ResolveFingerprintFrom` with `ModelID: opts.model`) may
disagree at two rungs, and then the report promises an effect the next session never delivers —
the defect class `fb9dec2` was supposed to close:
(a) **Unpinned model.** With no `--model`, the probe discovers the active model and keys the
record on it, but startup's `opts.model` is empty ⇒ zero fingerprint ⇒ NO set applies, while the
report prints "now AUTO-APPLIES". The claim must become a `Suppressed` line: "no `model:` is
pinned in your config, so the next session start cannot resolve this identity — pin `model:
<label>` for the record to take effect" (wording in the report's established mid-sentence
`Suppressed` register, `internal/probe/model.go:249-251`).
(b) **Weights-file label.** A `--model` naming a locally reachable weight file resolves at High
(`sha256:…`) and the record is never read; the report claims a Medium promotion. Suppress:
"identity resolves at the weights tier on this machine, so the behavioral record is inert here".
**Q (owner):** minimal fix vs. pulling forward the twin-ladder consolidation already recorded in
`TODO.md` (one shared "what does startup decide about this entry" function used by BOTH
`resolveValidatedSet` and `autoApplyKeys`). *Recommendation: consolidate now — this is the
second divergence found in the twin ladders in one day; parity-by-construction (Settled design)
is the only durable fix, and the function's inputs (model id, endpoint, probe dir, opts'
off-switches, entries) are already identical on both sides.* Whichever way: the answer must be
COMPUTED via `library.ResolveFingerprintFrom` (or the shared function), not re-derived by
string-shape checks, and `effectLine`'s branches must stay true of the reader's machine.
This item also closes the off-switch test gap: `opts.bypass`, `!opts.validatedSetsEnable`, and
the explicit-`mechanisms:` branch (`probemodel.go:206-224`) each get a row — deleting any one
`case` must fail a test.
**Tests:** Untagged, in `cmd/apogee` beside the existing promotion tests: (a) unpinned-model
flow (endpoint discovery, no `--model`) with a stored record ⇒ no AUTO-APPLIES claim, the
suppressed line names the missing `model:` pin; (b) `--model` pointing at a real temp weight
file ⇒ suppressed as weights-tier; (c) the three off-switch rows asserting `keys == nil`,
`promoted == false`, and the expected substring; (d) the existing
`TestProbeModelDoesNotClaimAnEntryStartupWillSkip` and promotion tests green unchanged.
Mutation-check (a) and (b) against the pre-fix code.
**Acceptance:** for any (model, endpoint, config) triple, `probe model`'s effect line and the
next session start agree — including the two new rungs; no off-switch branch is untested.
**Commit:** `fix(cli): probe model's effect claim runs startup's identity ladder`

## 7. The probe report's remaining honesty gaps: `--no-save` wording, v1 warning, inert flag

**What:** (Review: Mediums "`--no-save` denies a surviving record" + "v1 warning dropped" +
"`--workspace` is inert" — three small strokes, one item, all in the probe-model report path.)
(a) `effectLine`'s no-record branch (`internal/probe/model.go:246-247`) claims "identity stays
at the label tier" even when an earlier SAVED record survives and still resolves the model at
Medium — wrong in exactly the drift-check scenario `--no-save` serves. `recordProbeFingerprint`
already loads the previous record (`probemodel.go:166`); carry "a usable previous record exists
(and its date)" into `probe.SaveOutcome` and word the branch: "none new — the record from
<date> continues to apply; this run recorded nothing".
(b) The Long text promises v1 records "are skipped with a warning" (`probemodel.go:69-71`), but
the only reader on this path discards `LoadProbeRecord`'s returned warning (`:166`) and its
comment misstates the contract ("LoadProbeRecord already reports why" — it returns the reason
for the caller to surface). Surface it (`cmd.PrintErrln` at the call site or threaded through
`SaveOutcome`, matching how startup's rung prints it) and correct the comment.
(c) `--workspace` (`probemodel.go:132-133`) changes nothing the model probe reports or writes —
`roots.workspace` is read only by `probe host` (`probe.go:95`) — contradicting the probe
commands' own flag rule ("the subset of the root's that CHANGES what is reported",
`probe.go:46`). Drop the flag from `probeModelCommand`; verify `resolveRoots(opts.configDir,
"")` stays correct for the model path (it must — `probe`'s default is the current directory and
the model path never reads the result).
**Tests:** Untagged: an effectLine/report row for "no-save with surviving record" (through
`runProbeModel` with a pre-stored record, the item-8-of-the-prior-plan idiom); a v1-format
record on disk ⇒ the warning appears on stderr and the report still renders; `probe model
--workspace x` now fails as an unknown flag (and `--help` no longer lists it).
**Acceptance:** every branch of the record section is true of the reader's machine; the v1
migration story explains itself; no inert flags on `probe model`.
**Commit:** `fix(cli): probe model report matches the disk; drop the inert --workspace flag`

## 8. A directory genuinely named like a short name is containable

**What:** (Review: Medium "authoritative resolution misread as failure".) `split`
(`internal/platform/host.go:186-193`) rejects a path when the resolver's output still has the
8.3 SHAPE — but for a directory literally named `demo~1`, `GetLongPathName` returns its input
because it IS the long name, and that authoritative success is indistinguishable from
`longPathName`'s nothing-resolvable fallback (`platform_windows.go:38-53` returns the input
unchanged in both cases). Consequence: `Contains` reports false and `windowsLabelGuardrail`
refuses a perfectly resolvable workspace into Gate mode. Fix by making the seam signal
authority: change the `longPath` seam to return `(string, ok bool)` — `ok` true when the
resolver ANSWERED (every component expanded or verified, even if unchanged), false when it
could not (no existing ancestor, API failure) — and have `split` trust an ok=true answer without
re-running the shape test. `longPathName` walks to the longest existing prefix already; ok is
false only when nothing at all was resolvable. The rejection comments at `:187-192` are
rewritten to match. POSIX rules are untouched (`longPath` is nil there — keep nil meaning
"reject 8.3 shapes", the pure-rule-set behaviour the untagged tables pin).
**Tests:** Untagged: injected-resolver rows — resolver answers with the same string + ok ⇒
contained; resolver fails ⇒ rejected (today's behaviour); nil resolver ⇒ rejected.
Windows-tagged: `mkdir demo~1` under a temp box root, assert `Contains` and the guardrail
accept it and the label pass runs; a genuinely unresolvable short name still refuses.
**Acceptance:** ADR 0020 §6's "normalise or be rejected" now rejects only what genuinely cannot
be normalised; no resolvable workspace is forced into Gate.
**Commit:** `fix(platform): trust the resolver's authoritative answer for tilde-named paths`

## 9. The label guardrail sees through reparse-point roots and trailing-dot spellings

**What:** (Review: Medium (uncertain) "lexical guardrail defeated by root spellings" — invariant
hygiene per Settled design, not an emergency.) The guardrail (`winconfine.go:314`) and the rule
table are lexical; `SetNamedSecurityInfo` is not: a box root spelled `C:\Windows.` (trailing
dot — OS canonicalization strips it) or a junction targeting a protected location passes
`Contains(root, protected) == false` and is then labelled THROUGH the reparse/canonical form,
labelling the protected target Low. Descendant reparse points are already skipped
(`confiner_windows.go:334-339`); only the root escapes. Three strokes:
(a) in the windows-tagged box-root path, `Lstat` each root and refuse
(`ErrConfinementUnavailable`, the guardrail's wrapping style) when it is a reparse point — a
junction/symlink root is exactly the "cannot honestly compare" class the guardrail already
refuses;
(b) resolve each root to its final form (`GetFinalPathNameByHandle` via an injected seam beside
`longPath`) and evaluate the guardrail against the RESOLVED path;
(c) in the untagged rule table, fold trailing dots and spaces off components in
`sameComponent`/`split` (Windows rules only — POSIX byte-compare untouched), so `C:\Windows.`
compares equal to `C:\Windows`.
Keep (a) and (b) at the backend (windows-tagged) and (c) in the pure rules, so the untagged
tables stay executable everywhere. ADR 0020 §6's consequences gain one sentence naming the
refusal ("a box root that is itself a reparse point is refused"), in this item — it is the
item's own subject, not a roll-up drive-by.
**Tests:** Untagged: `sameComponent`/`Contains` rows for trailing dot/space spellings.
Windows-tagged: `mklink /J` (no admin needed) a junction to a temp "protected" stand-in wired
through the guardrail's protected list ⇒ the box is refused and nothing is labelled; a
trailing-dot spelling of a protected root ⇒ refused; an ordinary root still labels.
**Acceptance:** no spelling of a protected location can be labelled; an honest workspace is
unaffected.
**Commit:** `fix(confine): refuse reparse-point roots; fold trailing dots in the rule table`

## 10. Session recovery provably preserves an undecodable journal

**What:** (Review: Medium "preservation of the unreadable journal untested" — tests only.) The
code promises `recoverLabelJournals` never deletes what it cannot decode
(`confiner_windows.go:437-447` — "deleting it would throw away the only trace"), and
`ConfinementResidue` is that state's only path to a human; but no test constructs a SESSION
backend over a home containing a corrupt journal and asserts the file survives construction. A
plausible "clean up what we can't decode" refactor would pass every existing test while making
the unreadable-journal residue line permanently unreachable.
**Tests:** Windows-tagged: seed garbage at `labelJournalPath(home, 909)` (a dead PID), construct
via `newTokenConfiner(home)` (the RECOVERING constructor — not the report variant the existing
probe test uses), assert the file still exists afterwards and `confinementResidue(home)` still
names it. If the recovery skip decision is instead extracted behind the existing untagged seam,
an untagged table row proving "undecodable ⇒ untouched" is an acceptable equivalent — but the
windows-tagged construction test is preferred (it pins the wiring, not just the rule).
**Acceptance:** the only trace of an unknown mutation cannot be destroyed by any constructor.
**Commit:** `test(confine): recovery leaves an undecodable journal in place`

## 11. Roll-up: CHANGELOG, review cross-reference, full native gate

**What:** (Closes the plan — run last.) CHANGELOG `[Unreleased]`: one Fixed block summarising
this plan (journal edge symmetry: unreadable-prior skip, verified revert, deleted-prior
tolerance, phantom-entry unwind, live-sibling respect; probe claim parity with startup's ladder
+ report honesty; tilde-name containment; guardrail canonicalisation), citing
`docs/reviews/code-review-2026-07-22-phase5-post-fixes.md`. Verify every item above is marked
done with its NOTES trail. Full gate NATIVELY on this host: `go vet ./...`, `go build ./...`,
`go test -count=1 ./...` (modulo the three pre-existing failures in Ground truth), all six cross
targets, `GOOS=linux go vet ./...` + `GOOS=darwin go vet ./...` (the accepted devbox proxy),
`apogee --help` exit 0, the ADR-0010 grep, gofmt over LF copies of changed files. The
outstanding Linux devbox `make check` stays tracked in `TODO.md` (*Phase-5 verification
leftovers*) — this plan widens that entry's file list if any untagged platform test changed.
**Tests:** the gates are the tests.
**Acceptance:** gate green; CHANGELOG entry present; plan fully marked; ready for the owner's
archive pass.
**Commit:** `docs: roll up the Phase 5 second review fixes`

---

## Explicitly NOT in this plan

- **The composition-root Close-wiring test** (review's uncertain, not-cross-validated test gap):
  the crash-recovery pass bounds the damage and the review itself declined to confirm it —
  revisit only if a refactor touches `runRoot`'s teardown defer.
- **The `Confine`-vs-`Close` unsynchronised field reads** noted in passing by the review
  (worst outcome: a failed `Start` at shutdown) — revisit only if a `-race` run on this host
  with an overlapping cancel reproduces it.
- **The confined-`%TEMP%`/writable-paths box construction** — still the recorded `TODO.md`
  follow-on; own design session.
- **The twin-ladder consolidation beyond item 6's decision** — if the owner picks the minimal
  fix in item 6's Q, the consolidation stays a recorded follow-on for
  `/improve-codebase-architecture`.
- **The owner-run live-enforcement checklist** (live Auto session on Windows, below-floor host,
  macOS cross-binary smoke, Linux devbox `make check`) — unchanged, still owner-run.
