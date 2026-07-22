# Handoff — Phase 5 is complete and archived; next is the Linux closeout pass

**Date:** 2026-07-22 · **Branch:** `main` (clean, 13 commits ahead of the last merge, unpushed)
**Execution machine for this session:** the owner's **Windows** box (windows/arm64, build 26200, go1.26.5)

## What happened

The merge plan's **Phase 5 — cross-platform hardening & retirement** was executed end-to-end with
`/implement-plan` + `/coding-standards`. A Windows Update interrupted the run mid-way; this session
picked it up, finished it, cleared two verifier follow-ups, and archived the plan.

The plan — every work item, its acceptance criteria, and the dated `NOTES (2026-07-22):` deviation
trail under each item — is the record. **Read it rather than re-deriving anything:**

    docs/plans/archived/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md

Those NOTES are unusually load-bearing: most items deviated from their literal text under an
authorized precedence rule, and the NOTES (not the item text) describe what actually shipped.

Commits, oldest first: `440572a`, `5405707`, `123d631`, `836c670`, `cebb16d`, `393ea1b`, `8fb2620`,
`eccc3a1`, `0d32866`, `a7c8eaa`, `6eaabc2`.

Design authorities produced or amended: **ADR 0020** (Windows confinement), **ADR 0021** + its dated
Amendment (the two halves of `apogee probe`), `docs/design/confinement-execution-contract.md` §2.2 /
§2.4 / §9.

## State when the interrupt hit, and what this session did about it

- **Item 8 was implemented but uncommitted** — the implementer had finished (including live
  evidence in its NOTES); the verifier never ran. Verified and committed as `393ea1b`.
- **`TestTerminal_WindowsCleanRunLeavesADetachedProcessAlive` was flaking** (item 7's territory,
  already committed). Root cause was real, not cosmetic: the test recorded the `cmd.exe` PID while
  its `ping.exe` grandchild held the temp dir, so `t.TempDir()` cleanup raced the survivor. Fixed
  test-only in `eccc3a1`; the negative control (breaking `release()`'s `KILL_ON_JOB_CLOSE` clear)
  was independently re-run and still fails 3/3, so the test still proves what item 7 needs it to.
- **Two verifier follow-ups cleared** in `a7c8eaa`:
  1. Four Go comments still claimed Windows used AppContainer or kept `denyConfiner` "until
     Phase 5" (`internal/platform/platform.go`, `seatbelt.go`, `seatbelt_notdarwin_test.go`,
     `internal/domain/confinement.go`).
  2. **A truthfulness bug:** `apogee probe host` read label residue from the resolved config root
     while the confiner writes its journal under the default `~/.apogee`, so under a non-default
     `--config` the `labels:` line silently reported nothing. The *reader* was moved onto the
     journal's own home (`confinementJournalHome()`); the writer and `NewConfiner()`'s no-arg shape
     are unchanged, and `--config` is still not threaded in. Proven end-to-end by the verifier with
     a live foreign PID and both the default and non-default `--config` cases.

## What is NOT done — this is the next session's work

`TODO.md` § **"Phase-5 verification leftovers — the owner-run passes this machine cannot perform"**
(around line 439) is the authority. Summarised:

1. **Closeout Linux pass — the important one.** `make check` on the Linux devbox. The linux-tagged
   landlock tests cannot run on Windows at all, so no confinement work in this phase has been
   proven on Linux beyond `GOOS=linux go vet ./...`. The plan originally declared this as gating
   item 10; item 10 shipped anyway and the requirement was carried into TODO.md rather than buried
   in the archived plan (owner's decision this session).
2. Live Auto-confined deliverable run on Windows, if an LLM endpoint is reachable there.
3. Degradation notice on a **below-floor** Windows host (< build 17763) — recorded UNTESTED in
   ADR 0020's consequences; no such host exists here.
4. macOS cross-binary smoke: `--help` plus a trivial session.
5. **An open owner call, not a task:** item 8 measured the Windows disk-label pass at ~1 ms/object
   (a synthetic 5,051-object tree took 5.2 s to label, 2.2 s to revert), paid once per box per
   session. A workspace with a large `.git`/`node_modules` will make the first `Confine` visibly
   block. ADR 0020 accepted the mutation but did not quantify it. Pruning the walk (startup notice?
   exclude ignored trees?) changes ratified box semantics — **do not decide this without the
   owner.**

## Environment traps on the Windows machine (they will bite you)

- **`make` is absent.** Run the `go` commands the Makefile wraps: `go build ./...`, `go vet ./...`,
  `$env:GOOS='linux'|'darwin'|'windows'; go vet ./...` (then `$env:GOOS=''`), the six cross targets,
  `./apogee.exe --help`.
- **`gofmt -l .` flags every file in the repo** — the checkout is `core.autocrlf=true`. Check
  formatting over LF copies of changed files only.
- **`-race` is unavailable on windows/arm64.**
- **Three test failures are the baseline here, not defects.** Do not "fix" them:
  `TestSaveHostAcknowledgement_PreservesTheFileMode` (POSIX file modes),
  `TestAutofixRepairsBrokenContentWhenFormatterImproves`, `TestFoldActivityClockRunsPerPhrase`.
  Any *other* failure is real. On the Linux devbox this baseline will differ — expect the first
  two to behave differently there and judge them fresh.

## Known gaps a fresh agent should not mistake for finished work

- The below-floor Windows path (deny backend + degradation notice under build 17763) is **written
  but never executed**. ADR 0020's consequences say so explicitly.
- `internal/probe`'s residue render now has tests, but the concurrency between `Capabilities()` /
  `Confine` reads and `Close`'s mutex-held writes on `tokenConfiner` is **unproven** — `-race`
  cannot run on this host. It is unreachable in the shipped wiring (`Close` runs at shutdown), so
  it was accepted, not fixed. Worth a `-race` run on the Linux/macOS side if the type ever grows a
  second caller.
- Phase 5 is the merge plan's last open phase. After the Linux pass, the merge plan itself
  (`docs/plans/implementation-plan-apogee-merge.md`) is complete — check whether it should be
  archived too.

## Suggested skills

- **`/implement-plan`** — only if new plan work is queued. The Phase-5 plan is archived; do not
  re-run it against the archived file.
- **`/coding-standards`** — mandatory for any new or modified Go in this repo (merge-plan Standing
  Requirement 1). Forward it to any sub-agent that writes code.
- **`/code-review`** — a good use of the Linux session: the Phase-5 diff (`440572a..6eaabc2`) is
  large, touches syscall-level code on three OSes, and has only ever been reviewed item-by-item by
  its own verifiers.
- **`/refocus`** — if you are arriving cold and want the whole-repo picture before touching
  anything.
- **`/archive-handoffs`** — this document supersedes several in `docs/handoffs/archived/`; run it
  once this one is itself superseded.
