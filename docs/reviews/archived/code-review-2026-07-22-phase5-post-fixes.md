# Code Review — Phase 5 + review fixes — 2026-07-22

**Scope:** The full Phase 5 delivery on `main` (`git diff 6ecc10a...HEAD`): Windows confinement
(low-integrity token + disk-label box + crash-recovery journal, ADR 0020), Job-Object
process-tree teardown, platform Shell/Path widening, `apogee probe` host + model halves
(ADR 0021 incl. Amendment), the proxy-retirement record, and the 13 follow-up review fixes.
**Mission:** Apogee is a terminal coding agent for small local LLMs; this delivery makes Auto
mode OS-confined on Windows and adds host/model diagnosis without running an agent.
**Files reviewed:** 72 code files (~11k added lines) plus the touched docs.

## Executive Summary

No Critical findings, and the confinement core is faithful to its spec: journal-before-label,
the fail-closed refusal ladder, the report/session constructor split, the Job-Object release
lifecycle, and the quoting rules all check out — several verified natively against
`CommandLineToArgvW` and a real `cmd.exe`. Two High findings remain. First, `probe model`'s
auto-apply claim still diverges from what startup actually does — the exact defect class the
review fix `fb9dec2` targeted — in two more rungs of the identity ladder (an unpinned model and
a weights-file label), so the report can print "now AUTO-APPLIES" for a session that will apply
nothing. Second, the foreign-prior label *restore* path — the reason the journal machinery
exists — has no end-to-end test on any OS. The Mediums cluster in the label-journal lifecycle's
edge symmetry: descendant-level cases where a label can land unjournalled, or a journal can be
retired (or stranded forever) out of step with what is actually on disk.

## Intent & Architecture Findings

### High — `probe model`'s auto-apply claim diverges from startup's identity ladder `[Correctness + Intent]`

- **Where:** `cmd/apogee/probemodel.go:202-244` (`autoApplyKeys`), `internal/probe/model.go:244-262` (`effectLine`)
- **What:** `autoApplyKeys` evaluates `validated.Match(m.Fingerprint.Label, ConfidenceMedium, …)`
  directly, while startup (`resolveValidatedSet` → `library.ResolveFingerprintFrom`) runs the
  full ladder from `opts.model`. Two realistic divergences:
  1. **Unpinned model.** With no `--model`, the probe falls back to the *discovered* active
     model (`probemodel.go:97-104`), but startup resolves from `opts.model` — empty in that same
     flow — so `ResolveFingerprintFrom` returns the zero fingerprint and no set ever applies
     (`cmd/apogee/wire.go:242`, `internal/agent/loop.go:160-165`). The report still prints
     "Validated set X now AUTO-APPLIES for this model … it was previously only offered."
  2. **Weights-file label.** When the advertised label is a locally reachable weight file — the
     case `internal/library/fingerprint.go:22` documents as routine for local servers — startup
     resolves `sha256:…` at **High** and never consults the probe record, yet the effect line
     claims the model "now resolves at medium confidence." The zero-fingerprint branch even
     knows this rung exists (`model.go:197`).
- **Why it matters:** This is precisely the false-promotion claim class review fix `fb9dec2`
  ("probe model's auto-apply claim passes catalogue validation") set out to eliminate; the user
  acts on a promotion the next session never delivers, or spends battery tokens for a record
  startup will never read.
- **Fix:** In `autoApplyKeys`/`recordProbeFingerprint`, compute both the counterfactual and
  with-record answers through `library.ResolveFingerprintFrom` with the same inputs startup
  uses, and emit a `Suppressed` reason when they differ: "no model is pinned — set `model:` for
  the record to take effect" / "identity resolves at the weights tier here; the record is
  inert." This belongs to the same twin-ladder consolidation already recorded in `TODO.md` for
  `/improve-codebase-architecture` — that follow-on is now carrying a live defect, not just
  duplication, and should be scheduled rather than parked.

### Medium — `--no-save` effect line denies a surviving earlier record `[Intent]`

- **Where:** `internal/probe/model.go:245-247`
- **What:** The no-record branch claims "with no record stored, this model's identity stays at
  the label tier (low confidence)" even when an earlier *saved* record still exists and still
  resolves the model at Medium.
- **Why it matters:** The wrong claim fires in exactly the drift-check scenario `--no-save`
  exists for: probe, save, later re-run with `--no-save` — the report then denies the promotion
  the next startup will in fact deliver. The report's own standard ("each claim is narrowed
  until it is true of the reader's machine") is violated.
- **Fix:** `recordProbeFingerprint` already loads the previous record (`probemodel.go:166`);
  carry "a usable previous record exists" into `SaveOutcome` and word the no-save branch as
  "the record from \<date\> continues to apply."

### Medium — the promised v1-record warning is silently dropped `[Intent]`

- **Where:** `cmd/apogee/probemodel.go:166` (vs. the Long text at `:69-71`)
- **What:** `probe model --help` promises pre-amendment v1 records "are skipped with a warning,"
  but `recordProbeFingerprint` does `prev, _, ok := library.LoadProbeRecord(...)`, discarding
  the returned warning. Its comment claims "LoadProbeRecord already reports why" — it doesn't;
  it returns the warning for the caller to surface (only startup's `probedLabel` prints it,
  `internal/library/fingerprint.go:106`).
- **Why it matters:** A user with a v1 record follows the help's own instruction to re-run
  `probe model` and sees no warning and no `changed:` line — the documented migration story
  silently fails to explain itself.
- **Fix:** Surface the returned warning (thread it through `SaveOutcome` or `cmd.PrintErrln` at
  the call site) and correct the comment.

### Medium — `probe model --workspace` is an inert flag `[Intent]`

- **Where:** `cmd/apogee/probemodel.go:132-133`
- **What:** The flag feeds only `resolveRoots(opts.configDir, opts.workspace)`, whose
  `roots.workspace` the model probe never reads; nothing in the battery, record, or report
  depends on it.
- **Why it matters:** It contradicts the sibling command's own declared rule (`probe.go:46`:
  flags are "the subset of the root's that CHANGES what is reported") and misleads users into
  thinking the probe is workspace-scoped.
- **Fix:** Drop the flag from `probeModelCommand` (or actually report the workspace).

## Critical & High Findings

### High — the foreign-prior label restore path is untested end-to-end `[Tests]`

- **Where:** `internal/platform/confiner_windows.go:384-401` (`revertLabelJournal`), fed by
  `:340-347` (`labelTree`'s foreign-prior journaling)
- **What:** The journal machinery exists so a path carrying a pre-existing explicit label (a
  foreign Medium/High SACL inside the box) gets that descriptor put back *verbatim* on
  teardown. Every pure piece is unit-tested, but no test on any OS seeds a real foreign label
  inside a box, runs `labelBox` + `Close`, and asserts the label comes back. Delete the
  `priorLabels` loop, or swap the clear/restore order so the walk wipes the just-restored
  prior, and the whole suite stays green.
- **Why it matters:** A silent regression here strips a security label from the user's disk and
  then deletes the only record of it (the journal is removed after a "successful" revert) — the
  exact data-loss class the journal was built to prevent.
- **Fix:** Windows-tagged test: `setLabelSDDL(child, "S:(ML;;NW;;;ME)")` on a file inside a
  fresh workspace; `labelBox`; assert the journal entry carries the Medium prior; `Close()`;
  assert `readLabelSDDL(child)` returns the Medium descriptor (not `""`) while the root and
  other files are clear.

## Medium Findings

### Medium — a descendant whose prior label cannot be read is labelled unjournalled `[Correctness]`

- **Where:** `internal/platform/confiner_windows.go:340-352` (`labelTree`)
- **What:** `if prior, err := readLabelSDDL(path); err == nil && prior != ""` skips journalling
  on a read *error*, then falls through to `_ = setLabelSDDL(path, sddl)`.
- **Why it matters:** A descendant carrying a foreign explicit label whose read fails (e.g.
  WRITE_OWNER granted but READ_CONTROL denied) gets apogee's Low label with no journalled
  prior; teardown then clears it to unlabelled — a foreign security label destroyed with no
  record, violating "no label lands without a journal entry first" at the descendant level.
- **Fix:** On a read error, return without labelling that path — the same tolerated-failure
  rung an unlabellable descendant already takes.

### Medium — teardown retires the journal without verifying the revert below the root `[Correctness]`

- **Where:** `internal/platform/confiner_windows.go:413-425` (`clearLabelTree`),
  `internal/platform/winconfine.go:438-449` (`retireLabelJournal`)
- **What:** The `WalkDir` callback swallows every failure (`if walkErr != nil || path == root
  { return nil }`; `_ = setLabelSDDL(path, windowsClearLabelSDDL)`), so `clearLabelTree`
  returns nil even when descendants keep their Low labels; `retireLabelJournal` then deletes
  the journal. Recovery has the identical hole.
- **Why it matters:** A subtree unreadable at teardown (a directory whose DACL the confined
  child rewrote — it owns in-box objects) strands Low labels on disk with no residue report and
  no retry, against the stated invariant "a journal is deleted only after its labels are
  verifiably reverted."
- **Fix:** Count descendant clear failures and walk errors (tolerating only `IsNotExist`) and
  return an error so the journal survives for the next attempt.

### Medium — a deleted prior-labelled path makes the journal permanently un-retirable `[Intent + Correctness]`

- **Where:** `internal/platform/confiner_windows.go:395-399` (`revertLabelJournal`'s
  prior-restore loop)
- **What:** Unlike `clearLabelTree` directly above (which filters `os.IsNotExist` with the
  comment "a path that has since been deleted is not an error"), the prior-restore loop treats
  any `setLabelSDDL` error — including a vanished path — as a revert failure.
- **Why it matters:** A file that carried a foreign label and was then deleted or renamed by
  the agent (routine workspace activity) makes `Close` fail every session, keeps the journal
  forever, retries-and-fails silently at every startup, and turns `ConfinementResidue`'s "a new
  session reverts them automatically" into a false promise. The only remedy is manually
  deleting a journal describing labels that no longer exist.
- **Fix:** Skip `os.IsNotExist` errors in the prior-restore loop, matching `clearLabelTree`'s
  posture.

### Medium — a failed root label write leaves a phantom journal entry forever `[Correctness]`

- **Where:** `internal/platform/confiner_windows.go:276-286` (`journalLabel` → `labelTree`)
- **What:** The root's journal entry is (correctly) flushed before `setLabelSDDL(root, …)`; if
  that write then fails (label readable but not writable — SMB share, missing WRITE_OWNER),
  the box is refused but the entry stays. `Close`'s `clearLabelTree(root)` fails for the same
  reason (not `IsNotExist`), so the journal is kept forever.
- **Why it matters:** Every session end prints the "could not revert every mandatory label"
  warning and `apogee probe` reports Low-label residue for a disk that carries no label at all —
  a persistent false alarm that trains the user to ignore the real one.
- **Fix:** When the root label write fails and the just-added entry recorded no foreign prior,
  remove the entry and re-flush — nothing was mutated, so unwinding is honest.

### Medium — two concurrent sessions on one workspace: the first `Close` un-fences the survivor `[Correctness]`

- **Where:** `internal/platform/confiner_windows.go:363-371` (memo) + `Close`/`clearLabelTree`
- **What:** Session B reads A's Low label as the prior and correctly journals no restore; when
  A exits first, its teardown clears the shared root's labels while B still runs. B's
  `c.labelled` memo says the root is done, so it never re-labels.
- **Why it matters:** Every subsequent confined command in B is denied writes inside its own
  box for the rest of the session — fail-closed, but a broken tool in a plausible two-terminal
  setup.
- **Fix:** Before clearing a root at teardown/recovery, skip roots that appear in another
  *live* process's journal — the liveness check already exists (`processAlive`).

### Medium — a directory genuinely named like a short name is refused as a box root `[Correctness]`

- **Where:** `internal/platform/host.go:186-193` (`split`),
  `internal/platform/platform_windows.go:38-53` (`longPathName`)
- **What:** For a real directory literally named e.g. `demo~1`, `GetLongPathName` returns the
  same string (it *is* the long name), `hasShortName` remains true, and `split` returns
  `ok=false` — so `Contains` fails and `windowsLabelGuardrail` refuses the box with
  `ErrConfinementUnavailable` even though the resolver answered authoritatively.
- **Why it matters:** A perfectly resolvable workspace is forced into Gate mode with a
  misleading "cannot compare" refusal.
- **Fix:** Treat a successful resolution whose result equals the input as "this is the long
  form" (have the resolver signal success distinctly) instead of re-running the shape test.

### Medium — (uncertain) the lexical guardrail can be defeated by a reparse-point or trailing-dot box root `[Security]`

- **Where:** `internal/platform/confiner_windows.go:317`, `:407` (root labelling),
  `internal/platform/winconfine.go:314-330` (`windowsLabelGuardrail`)
- **What:** The guardrail and `Contains` are purely lexical: a box root spelled `C:\Windows.`
  (trailing dot) or a directory junction targeting `C:\Windows` passes the protected-location
  check, and `SetNamedSecurityInfo` then follows OS canonicalization/the reparse point and
  labels the protected target Low — making it writable by the confined child. Descendant
  reparse points are correctly skipped; only the root escapes the skip.
- **Why it matters:** The stated invariant "refuse to label protected locations" is silently
  defeated by these spellings. Marked uncertain-severity because the box roots come from
  trusted config (`WorkspaceDir ∪ ConfineWritablePaths`), not from the model or server, and the
  Low child cannot create the precondition — so the fence's actual adversary cannot reach it.
- **Fix:** `Lstat` the root and refuse (`ErrConfinementUnavailable`) if it is a reparse point;
  evaluate the guardrail against the resolved path (`GetFinalPathNameByHandle`); normalize
  trailing dots/spaces per component in `split`.

### Medium — session recovery's preservation of an undecodable journal is untested `[Tests]`

- **Where:** `internal/platform/confiner_windows.go:441-453` (`recoverLabelJournals`)
- **What:** The code promises recovery never destroys an unreadable journal (it is the only
  trace of whatever it described), but no test constructs a *session* backend over a home
  containing a corrupt `labels-N.json` and asserts the file survives. Existing tests cover the
  reporting half only.
- **Why it matters:** A plausible "clean up what we can't decode" refactor would pass every
  test while making the unreadable-journal residue line permanently unreachable.
- **Fix:** Seed garbage at `labelJournalPath(home, 909)`, run `newTokenConfiner(home)`, assert
  the file still exists and `confinementResidue(home)` still names it.

### Medium — `autoApplyKeys`'s session-off-switch branches are untested `[Tests]`

- **Where:** `cmd/apogee/probemodel.go:204-211`, `:221-224`
- **What:** The `opts.bypass`, `!opts.validatedSetsEnable`, and explicit-`mechanisms:` branches
  that suppress the promotion claim have no test at the seam that computes them — only the
  catalogue-defect branch is pinned. Deleting `case opts.bypass:` passes the suite while
  `probe model` prints "now AUTO-APPLIES" on a machine whose next bypassed session applies
  nothing.
- **Why it matters:** Probe/startup parity is the invariant the report machinery exists to
  uphold; these are its untested branches (distinct from the recorded twin-ladder follow-on).
- **Fix:** A table over the three off-switches calling `autoApplyKeys` directly, asserting
  `keys == nil`, `promoted == false`, and the expected `suppressed` substring. Fold into the
  High finding's fix — the same function is being reworked.

## Recommended Action Order

1. **Write the foreign-label restore test first** (High, Tests) — it pins the journal
   machinery's reason for existing before any of the lifecycle fixes below touch that code.
2. **Fix the probe/startup ladder divergence + add the off-switch tests** (High + its test
   Medium) — one locus (`autoApplyKeys`/`effectLine`); route both claims through
   `ResolveFingerprintFrom`. Consider pulling the recorded twin-ladder consolidation forward,
   since it now carries a live defect.
3. **Journal lifecycle symmetry sweep** — the four Mediums in `confiner_windows.go` (unread
   prior → don't label; verify revert below the root; `IsNotExist` in the prior-restore loop;
   unwind the entry on a failed root write) are small, same-file, same-invariant fixes best
   landed together with the new test from step 1 as the safety net.
4. **Concurrent-session label ownership** — skip live-journal roots at teardown; needs a short
   design note (it touches recovery too) but the liveness primitive exists.
5. **Quick wins:** the tilde-named-directory refusal; the root reparse-point `Lstat` refusal;
   drop the inert `--workspace` flag; surface the v1-record warning; fix the `--no-save`
   effect line.
6. The undecodable-journal recovery test closes the list.

## What Looked Good

The fail-closed spine of the Windows backend is genuinely well built: journal-before-label,
first-prior-wins with the own-label guard, atomic journal writes, the report/session
constructor split, and the refusal ladder all match ADR 0020 and the execution contract, and
the touched docs (README, CONTEXT.md, ADRs, contract §9) check out against the shipped code —
no stale claims found. The quoting work (`windowsArgvQuote` + caret escaping) is verified
natively against both `CommandLineToArgvW` and a real `cmd.exe` child, the Job-Object teardown
releases on every path with mutation-checked tests, and the overall test posture — injected
seams making Windows semantics table-testable everywhere, negative-control verification of the
load-bearing tests — is unusually strong. The security review specifically cleared the
high-value escape paths: probe-record forgery cannot lower any safety floor, descendant
junctions/hardlinks are skipped or blocked by the integrity fence, and Job-Object breakaway is
denied.
