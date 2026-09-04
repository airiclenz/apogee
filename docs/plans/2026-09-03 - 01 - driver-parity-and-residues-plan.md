# Driver parity — what only the TUI can reach, and the 2026-09-02 residues

**Goal:** close the eight `Driver-parity gaps` beads and the four residue beads in the issue register (`bd`),
so a headless run and a daemon Firing narrate, refuse and report what an interactive session
already does. No new config key, no new persistence format, no new cobra verb.

**Date:** 2026-09-03 · **Status:** unexecuted · **Base:** `25611103` · **sized for:** ~200k-context host

**Sources**
- issue register (`bd`) → epic `apogee-kk0` *Driver-parity gaps — behaviour only the TUI can reach (architecture review 2026-08-30)* with children `apogee-kk0.1`–`apogee-kk0.8`; epic `apogee-1ov` *Issues-register sweep — residue (2026-09-02)* with children `apogee-1ov.1`–`apogee-1ov.3`; `apogee-4w7` *`TestReportKindsResolveDistinctly` does not walk `reportKind.follows`*. All three were migrated out of `ISSUES.md` on 2026-09-03, when that file was deleted.
- `docs/reviews/architecture-review-2026-08-30.html` — the findings behind the parity section
- ADRs `0012` (blast radius, the unconfined-Auto loosen), `0031` (benchable-all-the-way-up), `0022` (session records), `0046` (the engine's output cap)
- `docs/handoffs/2026-09-03 - 00 - parked-items-verified-and-next-plan.md`

**Ratified design calls** (owner, 2026-09-03)
- **Context files:** `run.Result` carries the report; headless prints all three TUI sentences on stderr; the daemon logs the anomalies only (unreadable / oversize).
- **Offline gate:** one shared pre-send beat on the unattended path; a failed beat refuses before submit — headless exits 2 with the TUI's wording, the daemon records a failed Outcome.
- **Undo:** an end-of-run written-files report on `run.Result`; the journal stays memory-only, no `apogee undo` verb.
- **Firing notices:** the daemon logs them through `log.line`; the TUI `/schedule` Driver keeps dropping them.
- **Daemon boot:** the unconfined-Auto warning and the Windows pre-warm both run per Firing, latched once per process (per workspace for the pre-warm).
- **Bypass reach:** `--server` and `--bypass` land on `headless` only; no `bypass:` key on a schedule entry.

**Derived calls** (writer, 2026-09-03)
- **One composer:** `formatBytes` moves to `internal/format` as exported `Bytes` (stdlib-only, that package's charter intact) and the three context-file SENTENCES move to a new leaf package `internal/notice`, which may import `internal/domain` and `internal/format`; `domain` is NOT the home — `internal/domain/contextfile.go`'s doc reserves formatting for the host. (Split at the regression check, 2026-09-03.)
- **Beat carrier:** `firingRouting` gains the beat; on the unattended path (headless, the daemon) the pinned-entry case now takes one HTTP call it did not take before, which IS the liveness gate — a TUI-raised Firing passes its own beat through item 7's seam and still spends none.
- **Anomaly split:** the composer returns `[]notice.ContextNotice{Text, Anomaly}` so one caller prints all and another keeps the anomalies.
- **Exit code:** a pre-send refusal is `notStarted` (exit 2, `cmd/apogee/headless.go:46`) — no new code.

**Standing requirements:** `skills: coding-standards`. Any authorized deviation lands as a dated NOTES line under its item.

**Out of scope:** the bench and its Task Pool · a persisted undo journal or an `apogee undo` verb · `/schedule`'s dropped notices · any new config key · the TUI tool-row error-detail defect · the `Test drivers — residue` (`apogee-n6z`, closed) and `Tool-surface findings` (`apogee-304`) beads.

**Regression check (2026-09-03, `25611103`):**
- 1: guard folded — the clean-tree pathspec widens to every path plan `2026-09-02 - 08` owns.
- 2: recast, guard extended at the re-check — yields to `internal/format/doc.go:27-29`, whose `stdlib-only` clause the acceptance now pins; the item amends `doc.go:2-4` to name the byte-size family.
- 3: guard folded — supersedes `cmd/apogee/headless.go:487-489`, whose "ZERO Result" clause becomes "zero Turns".
- 4: guard folded — the notice loop sits ABOVE the never-started exit at `cmd/apogee/headless.go:496-497`.
- 5: guard folded — the pre-warm needs a swappable seam to be observable; the `FSWrite: false` case never reaches the gate.
- 6: guard folded — supersedes `internal/config/config.go:3204-3212` and its test comment; the remedy assertion is ADDED, not updated.
- 7: recast, and recast again at the re-check — `firingInputs` gains a `beat` seam (nil ⇒ `discoverBeat`) so a TUI-raised Firing keeps its own observation, and `heartbeat.Beat` gains `Answered bool`; the four unlisted `cmd/apogee` files and both discovery packages join **Files:**.
- 8: recast at the re-check — the refusal is `!beat.Answered` alone (a transport-level failure); the "404/401 on the model list" condition is withdrawn as unimplementable, and a 429 or a listless endpoint still proceeds.
- 9: recast, and narrowed again at the re-check — the condition is `!beat.Answered`, exactly item 8's; the refusal stays `fire`'s error (`EventFailed`), never `Faulted: true`; yields to `internal/schedule/schedule.go:91-95`.
- 10: recast, guard extended at the re-check — `rebindSpecFor` keeps its hard-coded `0`, so the Firing's window clause is the pin or nothing (`unknown` unpinned), never the TUI's observed-window sentence.
- 11: guard folded — this item OWNS the daemon-log plumb and gains `cmd/apogee/daemon.go`.
- 12: guard folded — depends on item 11's plumb; the test asserts the latch, not the off-Windows no-op call.
- 13: recast, guard extended at the re-check — a paths-only `Journal.Wrote()` / `Agent.WroteFiles()` replaces `UndoPreview`; item 3 moves `run.Result` and `run_test.go` first, so both sites are found by anchor text.
- 14: recast, guard extended at the re-check — a plain path list under a `changed —` header (deletes and move sources are journaled too), never `undoPathLine`'s revert verbs; gains `docs/manual/daemon.md`; the headless block is placed by anchor text after item 4's.
- 15: guard folded — the injected clock, not a chmod, is the failing-delete seam.
- 16: guard folded — gains `cmd/apogee/wire.go`; the stale nil-store comment is this item's to correct.
- 17: guard folded — the notice fires on `sse` and `streamable-http` only; the sink is `internal/config/config.go:3004-3008`.
- 18: guard folded — gains `internal/tui/reportpane.go`; its walk comment is stale once `follows` is covered.
- 19: guard folded — the undo-surface bullet MOVES to `## Parked / deferred work`, its revert half still open.

---

## 1. Verify plan `2026-09-02 - 08` is archived — ✅ DONE (2026-09-04)

NOTES (2026-09-04): gate passed — `docs/plans/archived/2026-09-02 - 08 - mechanism-retirement-wave-plan.md` exists; the scoped `git status --short` over `cmd/apogee/ internal/run/ internal/mechanisms/ internal/floor/ docs/manual/ CONTEXT.md internal/agent/ internal/tui/ internal/config/ internal/domain/ internal/validated/ apogee.go` printed nothing (the whole tree is clean, `git status --porcelain` empty); `go build ./...` exit 0.

NOTES (2026-09-04): HEAD is `0309e144`, three commits past the plan's stated base `25611103` (`200434ea` version bump, `de022767` this plan's document, `0309e144` an AGENTS.md convention). None of them touch the files plan `2026-09-02 - 08` owns, so the gate's premise holds; this plan's own document is now tracked, so the pathspec scoping the item describes is no longer load-bearing.

**What.** Plan `docs/plans/2026-09-02 - 08 - mechanism-retirement-wave-plan.md` was in flight at
this plan's base and owns `cmd/apogee/{wire_boot,wire,wire_firing,root,headless}.go`,
`internal/run/transcript.go`, `docs/manual/{headless,daemon}.md` and `CONTEXT.md` —
every file this plan builds on. Confirm it is under `docs/plans/archived/` and that nothing it owns
is uncommitted. On failure STOP and report; never re-derive its work here. The clean-tree check is
SCOPED to those paths — this plan's own document is untracked until the owner commits it.

**Regression guard.** Plan `2026-09-02 - 08` also owns `CONTEXT.md`, `internal/agent/`,
`internal/tui/`, `internal/config/`, `internal/domain/`, `internal/validated/` and `apogee.go` (its
**Files:** lines `:257`, `:483`, `:759`, `:796`, `:861`, `:928`), so the clean-tree pathspec must name
those too — otherwise a half-landed plan 08 passes this gate and item 19 closes beads against a
half-landed tree.

**Files:** none (verification only).

**Tests.** None.

**Acceptance.** `test -f "docs/plans/archived/2026-09-02 - 08 - mechanism-retirement-wave-plan.md"`;
`git status --short -- cmd/apogee/ internal/run/ internal/mechanisms/ internal/floor/ docs/manual/
CONTEXT.md internal/agent/ internal/tui/ internal/config/ internal/domain/
internal/validated/ apogee.go` prints nothing; `go build ./...` succeeds.

**Commit:** none — this item gates, it does not change the tree.

---

## 2. One composer for the context-files notices — ✅ DONE (2026-09-04)

NOTES (2026-09-04): `internal/format/doc.go` gained a short paragraph beside the amended opening
sentence, saying Bytes shares neither the unknown-sentinel nor the 1000-ceiling rule — the opening
amendment alone would have left the ladder's rule reading as package-wide over a helper that spells
"1023 B". The `stdlib-only` charter clause at `:27-29` is untouched.

NOTES (2026-09-04): added `TestContextFilesNoticeStripsEscapes` to
`internal/tui/contextfiles_test.go` beyond the item's Tests list, pinning the widening the item
states — the host strips EVERY composed notice, so the loaded line is stripped too, not only the
unreadable one. `TestContextFilesNoticeNamesEveryFile` is unchanged and green.

**What.** Recast at the regression check (2026-09-03). TWO moves, not one. `formatBytes`
(`internal/tui/model.go:3616`, one caller) becomes exported `Bytes` in `internal/format`, doc comment
intact — stdlib-only, so that package's charter holds — and is deleted from the TUI, while
`internal/format/doc.go:2-4` is amended to name the byte-size family beside the token one. The
SENTENCE composer lands in a NEW leaf package `internal/notice`: `type
ContextNotice struct { Text string; Anomaly bool }` and `func ContextFileNotices(r
domain.ContextFilesReport) []ContextNotice`, importing `internal/domain` and `internal/format`. It
emits, in TODAY's order: the loaded line first when any file loaded, then one `Anomaly: true` notice
per unreadable file, then the oversize warning as `Anomaly: true` when `r.Oversize()`. It returns NO
notices at all when `len(r.Files) == 0`. The strings are the TUI's CURRENT wording, moved verbatim,
not rephrased — `"context: " + name + " unreadable — " + err`, `"context: " + strings.Join(loaded,
", ")` with each entry `name + " (" + format.Bytes(f.Bytes) + ")"`, and `"standing system content ~"
+ TokensFine(StandingTokens) + " tokens exceeds its Budget share (~" + TokensFine(SystemShare) + ")
— trim context files, the task list or the system prompt"`. Escape stripping stays at the call sites.
`noteContextFiles` (`internal/tui/model.go:768`) becomes a loop over the notices, each still
`m.transcript.addEphemeralNote(...)`, keeping its `len(report.Files) == 0` early return and applying
`stripEscapes` to EVERY composed notice text. One deep module: no other file re-spells these
sentences.

**Regression guard.** The item YIELDS to `internal/format/doc.go:27-29` ("stdlib-only and depends on
nothing else in Apogee"), which is NOT superseded — that is why the composer lands in
`internal/notice` and only `Bytes` lands in `internal/format`. Ordering is TODAY's, not the draft's:
the loaded line FIRST, then one Anomaly notice per unreadable file, then the oversize warning as
Anomaly, so `TestContextFilesNoticeNamesEveryFile` (`internal/tui/contextfiles_test.go:95-108`,
`slices.Equal` over the notes) stays green UNCHANGED. `ContextFileNotices` returns no notices at all
when `len(r.Files) == 0`, so the oversize warning stays silent for a session with no context files
exactly as `internal/tui/model.go:755` states, and headless obeys the same rule; the TUI call site
ALSO keeps its `len(report.Files) == 0` early return, and it applies `stripEscapes` to every composed
notice text, not only the unreadable line — `model.go:777` strips the name on both branches today.
That charter clause — `:27-29` — is what the acceptance PINS, not the whole file: `doc.go:2-4`'s "owns
exactly one family — token and context-window sizes" goes false the moment `Bytes` lands, so this item
amends that opening sentence rather than leave the package doc lying about its own contents.

**Files:** `internal/notice/contextfiles.go`, `internal/notice/contextfiles_test.go`,
`internal/format/bytes.go`, `internal/format/bytes_test.go`, `internal/format/doc.go`,
`internal/tui/model.go`, `internal/tui/contextfiles_test.go`

**Tests.** A report with a loaded file, an unreadable one and an oversize standing block yields three
notices in the order above, exactly two of them `Anomaly: true`; an empty report yields none — the
oversize warning included; a report whose files all loaded and fits the budget yields exactly one,
`Anomaly: false`; `Bytes` spells `900` → `"900 B"`, `3174` → `"3.1 KiB"`, `2*1024*1024` → `"2.0 MiB"`.

**Acceptance.** `go test ./internal/notice/ ./internal/format/ ./internal/tui/`;
`go vet ./internal/notice/ ./internal/format/`;
`grep -c "func formatBytes" internal/tui/model.go` is `0`;
`grep -c "func Bytes" internal/format/bytes.go` is ≥ 1;
`grep -c "stdlib-only" internal/format/doc.go` is ≥ 1 (the charter clause survives the edit).

**Commit:** `refactor(format): the context-files notices get one composer`

---

## 3. `run.Result` carries the context-files report — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the submit-failure test drives Submit's refusal through an unbound model (`Config.Model: ""`) — construction succeeds, Submit returns `errNoModelBound` — since that is the package's only reachable Submit failure without a new double.

**What.** `internal/run/run.go`: `Result` (`:82-129`) gains `ContextFiles
domain.ContextFilesReport` — documented as measured at session construction, the same boundary the
TUI measures at. In `Once` (`:228`), after `agent.New` succeeds (`:259`) and before `Submit`, call
`a.ContextFilesReport()` and hold it; assign it into the `Result` literal (`:293-303`) AND into the
`Result` returned on the submit-failure exit (`:284`), so a run that never started still reports
what it loaded. The construct-failure and mode-gate exits (`:230`, `:261`) keep returning the zero
`Result` — there is no agent to ask. This item adds NO rendering: the field's consumers are items
4 and 11. `schedule.Outcome` is deliberately unchanged — the daemon renders from the `Result`.

**Regression guard.** `Result` is consumed by `cmd/apogee/headless.go` (`:475`),
`cmd/apogee/daemonfire.go` (`:242`) and `cmd/apogee/schedule.go` (`:156`), and built by the
`stubRunner` fakes in `cmd/apogee/headless_test.go:33` and the daemon harnesses. A new field is
additive — no fake needs a change to compile — so this item must leave all three consumers and
every fake untouched; if any needs an edit, the field's shape is wrong. Populating the
submit-failure exit SUPERSEDES `cmd/apogee/headless.go:487-489`, which makes "it returns the ZERO
Result from each of its three pre-run exits" the structural marker behind the exit-2 gate: rewrite
that clause to name ZERO TURNS — the field the gate at `:496` actually reads — and correct the same
wording where it is echoed at `cmd/apogee/headless_test.go:1503`.

**Files:** `internal/run/run.go`, `internal/run/run_test.go`, `cmd/apogee/headless.go`,
`cmd/apogee/headless_test.go`

**Tests.** A run whose workspace holds a readable context file reports it on
`Result.ContextFiles.Files`; a run with no context files reports an empty `Files`; a run whose
submit fails still reports the loaded files. Drive them through the package's existing
`internal/stubllm` + `harness_test.go` upstream, not a new double.

**Acceptance.** `go test ./internal/run/`; `go build ./...`;
`grep -c "ContextFiles" internal/run/run.go` is ≥ 2.

**Commit:** `feat(run): the run result carries the session's context-files report`

---

## 4. Headless prints the context-files notices — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the manual sentence names context files in plain text rather than linking them — `docs/manual/configuration.md` has no `#context-files` anchor to link to, and adding one is another item's file.

**What.** Depends on items 2 and 3. In `runHeadless` (`cmd/apogee/headless.go:274`), immediately
after `runOnce` returns (`headless.go:482`) and BEFORE the never-started early return at
`:496-497`, print every notice from `notice.ContextFileNotices(res.ContextFiles)` with
`cmd.PrintErrln`, one per line, each passed through `sanitize.StripEscapes` exactly as the answer is
at `:514`. All notices print, anomaly or
not — the ratified parity call. They go to stderr, so the stdout contract (`the answer alone`,
pinned by `TestHeadlessAnswerLandsOnTheProcessStdout`) is untouched. Order: context notices, then
the existing sub-agent lines (`:520`), usage lines (`:527`) and summary (`:530`).
`docs/manual/headless.md` gains one sentence in the stderr paragraph naming what these lines say.

**Regression guard.** The context-notice loop runs BEFORE the never-started exit at
`cmd/apogee/headless.go:496-497`, so a run that never started still reports what it loaded — that is
the only placement realizing item 3's stated purpose. `TestHeadlessOutputRouting`
(`cmd/apogee/headless_test.go:800`) and
`TestHeadlessAnswerLandsOnTheProcessStdout` (`:1349`) pin the stream split; both must still pass
with a `stubRunner` result that carries context files. The existing `stubRunner` returns a
zero-valued `ContextFiles`, which yields NO lines — so every unrelated headless test keeps its
current stderr byte-for-byte.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `docs/manual/headless.md`

**Tests.** A `stubRunner` result carrying one loaded file, one unreadable file and an oversize
standing block puts all three lines on stderr, with the EXACT strings `notice.ContextFileNotices`
composes (assert against the composer's output, never a hand-typed copy) and nothing on stdout but
the answer; a zero-valued report adds no stderr line at all; a run that never started — the
submit-failure exit item 3 populates — still prints its context notices ahead of the exit-2 refusal.

**Acceptance.** `go test ./cmd/apogee/ -run 'Headless'`; `go vet ./cmd/apogee/`;
`grep -c "ContextFileNotices" cmd/apogee/headless.go` is ≥ 1.

**Commit:** `feat(headless): the run reports what the context files contributed`

---

## 5. Headless pre-warms the Windows label walk — ✅ DONE (2026-09-04)

NOTES (2026-09-04): took the item's FIRST offered option — a package-level `prewarmLabelWalk` seam beside `runOnce`, swapped by the test — rather than dropping the claim to the grep acceptance, because the item's own Tests line prescribes the swapped seam.

NOTES (2026-09-04): the "no extra stderr line off Windows" half is asserted as an EQUALITY between the same invocation run with the real seam and with a do-nothing seam, not as an empty stderr — a clean confined-Auto stub run already prints its own `turns: 1 · denied: 0` summary line. The subtest skips on Windows, where the walk is real work.

NOTES (2026-09-04): pre-existing, not caused by this item — `TestRegistryWithMCPThreadsExtraReadRoots` (`cmd/apogee/wire_tools_test.go:59`) fails on this macOS host at the plan's own base commit `0309e144` as well as at HEAD: the test's `t.TempDir()` mount root is `/var/folders/...`, which resolves through the `/var` → `/private/var` symlink and is refused as outside the workspace root. Verified in a throwaway worktree at `0309e144`; the whole `go test ./cmd/apogee/` package is red for that one reason both before and after this item.

**What.** `cmd/apogee/headless.go`, inside the `mode == domain.ModeAuto` branch and immediately
after the `probe.ResidualNotice` block (`:390-393`): call `platform.PrewarmLabelWalk(confiner,
roots.workspace, cmd.ErrOrStderr())` when `shouldPrewarmLabelWalk(mode, opts.ConfineToWorkspace,
confiner.Capabilities().FSWrite)` — the same gate and the same order `announceConfinement`
(`cmd/apogee/wire_boot.go:349-351`) uses, so the two boot paths agree. Off Windows
`PrewarmLabelWalk` is an empty no-op (`internal/platform/prewarm_other.go:19`), so this changes no
byte of headless output on this machine's platform. Note the one deliberate difference from the
TUI: the notice goes to the command's stderr writer, not raw `os.Stderr`, because headless tests
capture through the cobra writers.

**Regression guard.** `shouldPrewarmLabelWalk` is reused, never re-implemented — a second copy of
the gate is how the two paths drift. `TestShouldPrewarmLabelWalk`
(`cmd/apogee/wire_boot_test.go:30`) stays the gate's only unit test; this item adds no second one.
`PrewarmLabelWalk` is an empty no-op off Windows (`internal/platform/prewarm_other.go:19`) and is
called directly, with no seam, so a test asserting the run "reaches the pre-warm" passes identically
against the pre-item tree: either add a package-level `prewarmLabelWalk` var beside
`runOnce`/`newConfiner` (`cmd/apogee/headless_test.go:86-89` swaps those already) and assert the swap
recorded the call, or drop that claim and let the grep acceptance stand. The `FSWrite: false` case
never reaches this gate — `probe.AutoUnattendedBlocked` refuses `--mode auto` under a confining config
when `AutoEligible()` is false (`cmd/apogee/headless.go:364-369`, `internal/domain/confinement.go:72`)
— so it stays with `TestShouldPrewarmLabelWalk` and is not written into the headless test.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`

**Tests.** With a `fakeConfiner` reporting `FSWrite: true` under `--mode auto` and a confining
config, the headless run records the pre-warm through the swapped `prewarmLabelWalk` seam and still
emits no extra stderr line off Windows (the no-op). Assert through the existing `headlessRunOn`
helper (`cmd/apogee/headless_test.go:84`). The `FSWrite: false` case is NOT written here — that
invocation exits 2 at the Auto refusal before the gate — and stays with `TestShouldPrewarmLabelWalk`.

**Acceptance.** `go test ./cmd/apogee/ -run 'Headless'`; `go build ./...`;
`grep -c "PrewarmLabelWalk" cmd/apogee/headless.go` is ≥ 1.

**Commit:** `feat(headless): the confined Auto run pre-warms the Windows label walk`

---

## 6. `headless` registers `--server` and `--bypass` — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the item quotes `--bypass`'s help string as "run with Mechanisms off; structural context reducers stay on (ADR 0006)", but its own instruction is to copy `cmd/apogee/root.go`'s strings VERBATIM so the two commands describe the flag identically; root now reads "run with the lab Mechanisms off; Floor guards and structural reducers stay on (ADR 0071)" after the retirement wave. Took root's live string — the verbatim rule wins over the item's stale quotation.

NOTES (2026-09-04): the remedy assertion is added per-case, not to all three, because the "nothing configured" refusal (`no servers are configured`) offers no name-shaped remedy at all — only the two cases a server NAME would fix carry `startupServerRemedy`. The unwanted direction (`APOGEE_SERVER` absent) is asserted on all three.

NOTES (2026-09-04): the manual's `:24` flag list gained `--server` and `--bypass` and the `:27` clause was rewritten to name the flag; the same sentence also now states the `--server` > `APOGEE_SERVER` > `server:` order and `--endpoint`'s override, since removing the "there being no `--server` flag" clause left the precedence unstated.

NOTES (2026-09-04): pre-existing, not caused by this item — `TestE2EPresentOpensOnlyTheAllowedFormats`, `TestE2ESmokeInProcess`, `TestFiringConfigDefaultsItsSeams` and `TestRegistryWithMCPThreadsExtraReadRoots` fail on this macOS host at HEAD (`08014b76`) as well as with this item's changes, all on the `/var` → `/private/var` symlink resolving a `t.TempDir()` root outside the workspace. Verified by stashing this item's diff and re-running the four.

**What.** `cmd/apogee/headless.go:237-247` gains two flags, with `cmd/apogee/root.go:101-135`'s
help strings copied verbatim so the two commands describe them identically:
`flags.StringVar(&opts.StartupServer, "server", "", "name of the servers: entry to start on
(default: the last one /server switched to)")` and `flags.BoolVar(&opts.Bypass, "bypass", false,
"run with Mechanisms off; structural context reducers stay on (ADR 0006)")`. Set
`opts.ServerFlagBound = true` beside the `--server` registration, with root's comment
(`root.go:107-109`) adapted — that field flips `startupServerRemedy`
(`internal/config/config.go:3211`) from `"or set APOGEE_SERVER=<name>"` to `"or pass --server
<name>"`, the announced surface this item changes. `--endpoint` keeps overriding `--server` through
the existing `resolveStartupEntry` short-circuit — no new mutual-exclusion rule.
`docs/manual/headless.md:27` currently says the resolution happens through
`APOGEE_SERVER` or `server:` "there being no `--server` flag on this command": rewrite that clause
to name the flag, and add `--bypass` to the flag list at `:24`.

**Regression guard.** The remedy wording is pinned in `internal/config/config_test.go:1141-1147`,
whose two directions both stay valid. `cmd/apogee/headless_test.go:1573` and `:1607` assert only "no
servers are configured" / "no startup server is chosen" / `names "the-old-name"` — neither spells
`APOGEE_SERVER` — so the remedy assertion is ADDED to `TestHeadlessRefusesEveryUndeterminedStartup`
(want `or pass --server <name>`, unwanted `APOGEE_SERVER`), not updated; that test is the journey, do
not add a parallel one. This item SUPERSEDES two documented statements that `apogee headless` carries
no `--server`: `startupServerRemedy`'s doc (`internal/config/config.go:3204-3212`) and the comment at
`internal/config/config_test.go:1125-1129` — rewrite both so `apogee probe` is the only flag-less
printer named.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `docs/manual/headless.md`,
`internal/config/config.go`, `internal/config/config_test.go`

**Tests.** `apogee headless --server <name>` starts on that `servers:` entry; the startup refusal
on an unresolvable choice now names `--server <name>` (assert the exact remedy string from
`startupServerRemedy`, not a paraphrase); `--bypass` reaches the engine as `apogee.Config.Bypass`
true (assert on the `run.Spec` the `stubRunner` captured).

**Acceptance.** `go test ./cmd/apogee/ -run 'Headless'`; `go test ./internal/config/`;
`grep -c '"bypass"' cmd/apogee/headless.go` is ≥ 1;
`grep -c "no \`--server\` flag" docs/manual/headless.md` is `0`.

**Commit:** `feat(headless): --server and --bypass reach the unattended run`

---

## 7. One beat per Firing, carried on `firingRouting` — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the pinned-entry rows of `TestHeadlessInstallsTheParallelAgentsCap` and `TestHeadlessSendsTheServersEffortDialect`, and the two `probed`/`dialects.called` assertions in `TestFiringConfigSetsEveryUnattendedField`, asserted the OLD mechanism ("a pin skips the round trip"). They are inverted: the beat is now asserted to be taken on every row, exactly once, with the pinned values still winning.

NOTES (2026-09-04): `firingInputs.width` and `.dialect` are replaced by the single `beat` seam rather than re-signed — the two answers come off one observation, and keeping two seams would have let a Driver hand over a width and a dialect that describe different moments. `scheduleWiring.fire` accordingly hands over one `heartbeat.Beat` carrying `w.width()` and `w.live.observedDialect()` with an empty `Failure` (and `Reachable`/`Answered` true: this session is talking to that server).

NOTES (2026-09-04): `stubSlots` and `stubDialect` are deleted and the existing `stubBeat` serves both beat seams; it gains `calls` and `endpoints` so "exactly one beat" and "the Sub-agent seam saw the SUB-AGENT endpoint" are assertable. `wire_firing_test.go` gains a `firingBeat` helper and passes it on the composition tests that are about something else, because an unconditional beat would otherwise dial `box.example` for real.

NOTES (2026-09-04): `wire_firing_test.go` imports `internal/provider` under the alias `apiprovider` — two tests in that file hold a `skills.Provider` in a variable named `provider`, which shadows the package name inside them.

NOTES (2026-09-04): pre-existing, unrelated to this item — `TestFiringConfigDefaultsItsSeams`, `TestE2EPresentOpensOnlyTheAllowedFormats`, `TestE2ESmokeInProcess`, `TestRegistryWithMCPThreadsExtraReadRoots` and eight `ExtraReadRoots` tests in `internal/agent` and `internal/tools` fail identically on this machine at HEAD (macOS resolves `t.TempDir()`'s `/var/...` to `/private/var/...`; the fixtures compare unresolved paths). Verified by running the full suite in a clean worktree at HEAD before this item's changes.

**What.** Recast at the regression check (2026-09-03). `cmd/apogee/wire_firing.go`: `firingConfig`
(`:97`) takes exactly ONE beat through a NAMED, swappable seam — `var discoverBeat = func(ctx,
endpoint, model, apiKey string) heartbeat.Beat` — and shares it with the two seams that beat the SAME
endpoint, model and key: `discoverSlots` (`wire_firing.go:192`) and `discoverDialect` (`:214`), which
are re-signed to take the beat or deleted outright. `discoverDelegationBeat` (`:434`) keeps opening
its OWN, because it beats the SUB-AGENT server's endpoint, key and model. Today the two folded seams
are SKIPPED when the entry pins `parallel-agents:` / `effort-dialect:` (`:193-196`, `:209-215`); after
this item the beat is unconditional — the pins still win over its values, but the call happens,
because that call IS the liveness gate items 8 and 9 consume. `firingRouting` gains `Beat
heartbeat.Beat` and `Reachable bool` (`Beat.Failure == ""`), written by `firingConfig` onto the value
RETURNED by `resolveFiringRouting` (after `:319`), never inside it; widen `firingRouting`'s doc
comment (`:351-373`) to say it also carries the Firing's observation of its PRIMARY server. Every
`discoverBeat` swap site is fixed HERE: `cmd/apogee/wire_firing_test.go:294`,
`cmd/apogee/headless_test.go:575` and `:635`, `cmd/apogee/daemonfire_test.go:47`,
`cmd/apogee/daemon_test.go:81`. No caller behaviour changes in this item — nothing yet reads
`Reachable`.

**Regression guard.** `discoverDelegationBeat` must NOT be fed the primary's beat:
`resolveDelegationTarget` (`cmd/apogee/delegation.go:856-870`) reads the SUB-AGENT server's
`Reachable`/`ActiveModel`/`ContextWindow`/`TotalSlots`, so a shared beat would resolve a target
against the wrong box and route delegations to a dead grunt server instead of degrading to the
Upstream with a notice. `Reachable` is written OUTSIDE `resolveFiringRouting` because that function
returns a bare `firingRouting{}` on the default no-`sub-agents-server:` path (`:407`) and on three
failure paths (`:412`, `:420`, `:429`) — a field set inside it reads false for every ordinary Firing,
and items 8 and 9 would then refuse every run. Every existing test that swaps a folded seam must
still compile and pass; a pinned-entry test asserting "no discovery call" is asserting the old
mechanism and is updated with a NOTES line saying so. Two further conditions bind, from the re-check
(2026-09-03). (1) `firingInputs` (`wire_firing.go:20`) gains a `beat` seam: nil means "take the real
beat through `discoverBeat`", which is what headless and the daemon pass, while `scheduleWiring.fire`
passes a Beat carrying `w.width()` and `w.live.observedDialect()` with an empty `Failure`, so a
TUI-raised Firing spends no round trip and keeps the session's own observation —
`cmd/apogee/schedule.go:136-139` and `cmd/apogee/wire_firing.go:62-73` record that as design call 4
and it STANDS. (2) The reachability discriminator is a NEW field, not a status match: `heartbeat.Beat`
gains `Answered bool`, true when the server returned ANY HTTP response — 4xx, 5xx and 429 included —
and false only for a transport-level failure (dial, timeout, DNS, TLS), supplied by
`internal/provider`'s discovery. That REPLACES the "404/401 on the model list" condition items 8 and 9
carried, which is unimplementable: `heartbeat.Beat` carries no status code, and
`internal/heartbeat/heartbeat.go:54-65` rules out string-matching `Failure`.

**Files:** `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/headless.go`,
`cmd/apogee/headless_test.go`, `cmd/apogee/schedule.go`, `cmd/apogee/schedule_test.go`,
`cmd/apogee/naming_test.go`, `cmd/apogee/daemonfire_test.go`, `cmd/apogee/daemon_test.go`,
`internal/heartbeat/heartbeat.go`, `internal/heartbeat/heartbeat_test.go`,
`internal/provider/discovery.go`, `internal/provider/discovery_test.go`

**Tests.** A Firing with both keys pinned still takes exactly one beat and still uses the pinned
values; an unreachable server yields `Reachable: false` with the failure text preserved on the
routing; a reachable one yields `Reachable: true`; a Firing with a `sub-agents-server:` entry still
beats THAT entry's endpoint through `discoverDelegationBeat`, not the shared primary beat; `Answered`
is false for a refused dial and true for BOTH a 429 and a 404 on the model list; a Firing raised
through `scheduleWiring.fire` takes NO beat of its own — it carries the session's width and dialect
with an empty `Failure`.

**Acceptance.** `go test ./cmd/apogee/ -run 'Firing|Headless|Daemon'`; `go build ./...`;
`grep -c "Reachable" cmd/apogee/wire_firing.go` is ≥ 1;
`grep -c "discoverBeat" cmd/apogee/wire_firing.go` is ≥ 1.

**Commit:** `refactor(firing): one shared beat per firing, carried on the routing`

---

## 8. Headless refuses an unreachable server before submit — ✅ DONE (2026-09-04)

NOTES (2026-09-04): the refusal travels as the returned `notStarted` error rather than a direct
write to the command's stderr writer — `newHeadlessCommand` sets `SilenceErrors`, so `main` is what
prints the error on stderr, and a second write here would print the sentence twice. The test
therefore asserts the exact sentence on the returned error (and stdout empty, the stub uncalled,
exit 2), which is the same text the operator reads.

NOTES (2026-09-04): consequential edit — internal/tui/heartbeat.go: made necessary by the new
headless offline gate — the item requires a comment at each site naming the other, so
`upstreamBlockNote`'s doc comment now names `cmd/apogee/headless.go`. Comment text only.

NOTES (2026-09-04): consequential edit — cmd/apogee/keymigrate_test.go: made necessary by the new
gate — two headless tests there build the cobra command directly (not through `headlessRunOn`) and
so took the production beat against an address nothing listens on, which the gate now refuses.
They call the new `swapAnsweringBeat(t)` helper; no assertion changed.

NOTES (2026-09-04): the test harness in `headless_test.go` gained `swapBeat`/`swapAnsweringBeat` and
`headlessRunOn` now installs an ANSWERED beat when a test dictated none. Without it every headless
test would assert against a refusal, and the suite would depend on whether the developer happens to
have a server on the test endpoint. The two tests in that file that dictated their own beat were
moved onto `swapBeat` so the harness cannot clobber them.

NOTES (2026-09-04): docs/manual/headless.md gained the refusal and its exit-2 row — the item changed
user-facing behaviour and the manual's exit table said only "usage, configuration, a refused mode".

NOTES (2026-09-04): three failures in `go test ./cmd/apogee/` are PRE-EXISTING at the base commit
(`TestE2ESmokeInProcess`, `TestFiringConfigDefaultsItsSeams`,
`TestRegistryWithMCPThreadsExtraReadRoots`) and one
(`TestFiringConfigSaysWhenTheModelIsNotAdvertised/a_variant_slug_...`) comes from another item's
in-flight changes to `cmd/apogee/wire_firing.go` present in the working tree. None is this item's.

**What.** Recast at the regression check (2026-09-03). Depends on item 7. In `runHeadless`, after
`firingConfig` returns (`headless.go:428`) and its notices are printed (`:440-442`), refuse when the
beat did not answer at all — `!routing.Beat.Answered`, item 7's field, false ONLY for a
transport-level failure (dial, timeout, DNS, TLS): return `notStarted(...)` — exit 2, the existing
never-started code (`headless.go:46`) — carrying the TUI's own wording from `upstreamBlockNote` (`internal/tui/heartbeat.go:672`): `cannot send — server
offline (<endpoint>)`, with `: <failure>` appended when the beat reported one. The endpoint spelled
is the resolved one the Firing would have used. The refusal happens BEFORE `runOnce`, so no session
record is written and no token is spent — that is the whole point of the gate.

**Regression guard.** Refuse ONLY when the beat did not answer — `!routing.Beat.Answered`, item 7's
field, false only for a transport-level failure; every server that returned ANY HTTP response keeps
today's proceed-and-degrade. A 429 on `GET /v1/models` and a completions-only endpoint that serves no
list at all both ANSWER, and both answer completions today
(`internal/heartbeat/heartbeat.go:151-163`, `internal/provider/discovery.go:145-152`);
`internal/heartbeat/heartbeat.go:59-64` and `cmd/apogee/delegation.go:426-433` record that a throttled
beat is SILENCE, not a verdict, so `Throttled` needs no case of its own. This item CONSUMES item 7's
`Answered` field and adds none of its own: it touches neither `internal/heartbeat` nor
`internal/provider`. It depends on item 7's `discoverBeat` seam and the test swaps it in
`cmd/apogee/headless_test.go`. The wording is shared with the TUI but the composition is NOT hoisted: the
TUI's note is a Model method over `m.hb`/`m.opts`, and hoisting it is a bigger change than this
item. Instead the item's test asserts the exact sentence, and a comment at each site names the
other, so a future edit finds both. State that in the code comment, not only here.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`

**Tests.** With item 7's `discoverBeat` swapped in `cmd/apogee/headless_test.go` to a beat that never
answered (`Answered: false`), `apogee headless "hi"` exits 2, prints the refusal on stderr with the
endpoint in it, prints NOTHING on stdout, and never calls the `stubRunner` (assert the stub's call
count is 0); a beat that ANSWERED — a 429, and a server that serves no model list at all — PROCEEDS,
as does a healthy one.

**Acceptance.** `go test ./cmd/apogee/ -run 'Headless'`;
`grep -c "server offline" cmd/apogee/headless.go` is ≥ 1.

**Commit:** `feat(headless): an unreachable server refuses the run before it starts`

---

## 9. The daemon refuses an unreachable Firing

**What.** Recast at the regression check (2026-09-03). Depends on item 7. In
`cmd/apogee/daemonfire.go`, after `firingConfig` (`:224`), a Firing whose beat did not answer —
`!routing.Beat.Answered`, item 7's field, a transport-level failure — does not run: `fire`
returns the refusal as its ERROR, carrying the same `cannot send — server offline (<endpoint>)`
sentence item 8 uses (with the beat's failure appended when present). It therefore lands as
`schedule.EventFailed` and renders through the existing `failed <name> after <elapsed> — <err>` line
(`cmd/apogee/daemon.go:649`). It records NO `schedule.Outcome` and is never `Faulted: true`. The
schedule's own retry/next-fire behaviour is untouched: a refused Firing is a failed Firing, nothing
more. `docs/manual/daemon.md`'s *What a
firing leaves behind* section gains one sentence naming the refusal.

**Regression guard.** Same refusal condition as item 8 — `!routing.Beat.Answered` alone — because a
429 on `GET /v1/models` and a completions-only endpoint both ANSWER, and both answer completions today
(`internal/heartbeat/heartbeat.go:151-163`); `Throttled` needs no case of its own, and this item adds
no field of its own — it CONSUMES item 7's `Answered`. The refusal is `fire`'s ERROR so it lands as
`EventFailed`, which is what this item's own "a refused Firing is a failed Firing" means; it is NEVER
`Faulted: true`: `internal/schedule/schedule.go:91-95` reserves `Faulted` for a Firing that RETURNED
with its Exchange at a boundary, and `daemonOutcome` (`cmd/apogee/daemon.go:680-690`) leads with why
a final Turn was abandoned — a run with no Turn has none. It renders through the `EventFailed` line
already pinned by `TestDaemonNotifyLinesArePinned` (`cmd/apogee/daemon_test.go:1026`), not a second
path.

**Files:** `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`, `docs/manual/daemon.md`

**Tests.** With item 7's `discoverBeat` swapped to a beat that never answered (`Answered: false`),
`fire` returns an ERROR carrying the refusal sentence, records no Outcome, and never calls `runOnce`;
the daemon renders it through the existing `EventFailed` line; a beat that ANSWERED — a 429, and a
server that serves no model list — runs the Firing as before, as does a healthy one.

**Acceptance.** `go test ./cmd/apogee/ -run 'Daemon'`;
`grep -c "server offline" cmd/apogee/daemonfire.go` is ≥ 1.

**Commit:** `feat(daemon): an unreachable server refuses the firing`

---

## 10. The "model not advertised" hint reaches the unattended path

**What.** Recast at the regression check (2026-09-03). Depends on item 7. `hintNotice`
(`cmd/apogee/upstream.go:574`) has exactly one production
caller today, the TUI rebind seam (`cmd/apogee/wire_verbs.go:58`). In `firingConfig`, after
`rebindSpecFor` (`wire_firing.go:150`), compose `hintNotice(model, grade, observedWindow,
boundWindow)` from the shared beat's resolution grade and observed window — the same two values
`wire_verbs.go:118` feeds `hintObserver.observe` — and append a non-empty result to the `notices`
slice `firingConfig` already returns. That slice is what headless prints on stderr (`:440-442`) and
what item 11 gives the daemon, so both Drivers gain the hint by consuming an existing channel.
`rebindSpecFor` KEEPS its hard-coded observed window `0` (`wire_firing.go:150`): the observed window
reaches `hintNotice` ALONE, so the Firing states its own bound window — the pin or nothing — while
nothing changes about what a Firing actually binds.

**Regression guard.** Replacing that `0` would make `bound = window`
(`cmd/apogee/wire_settings.go:2071-2074`) for every UNPINNED Firing, so `Context.MaxContextTokens`
(`wire_firing.go:290`) would become the observed PER-SLOT window and a run on a `--parallel 8 -c
65536` box would start pruning a prompt it sends whole today. `cmd/apogee/wire_firing.go:141-143`
records that an unpinned run leaves the Budget inactive as the honest degrade, and that decision
stands: the item's own second branch is now its ONLY one, and the tree keeps one path, not two. The
Firing's window clause is therefore the pin or nothing: with the `0` kept, `bound`
(`cmd/apogee/wire_settings.go:2071-2074`) is the pin and `window` is always zero, so `hintNotice`'s
base-slug case (`cmd/apogee/upstream.go:580`, which needs `window > 0 && bound == window`) can never
fire on this path — an UNPINNED Firing gets `(context window unknown — Budget and auto-compaction
inactive)` (`upstream.go:584-585`) and only an entry pinning `context-window:` gets `(context window:
<pin>)` (`:582-583`). The TUI's clause names the OBSERVED window (`cmd/apogee/wire_verbs.go:58` hands
it `spec.MaxContextTokens`), so the two Drivers' sentences differ BY DESIGN and no edit may "align"
them.

**Files:** `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`,
`cmd/apogee/upstream_test.go`

**Tests.** A Firing binding a model the beat's server never advertised puts `hintNotice`'s exact
sentence on the returned notices, ending `(context window unknown — Budget and auto-compaction
inactive)` when the entry pins no `context-window:` and `(context window: <pin>)` when it does; an
advertised model adds nothing; an unpinned Firing's bound window is unchanged by this item —
`rebindSpecFor` still passes `0`.

**Acceptance.** `go test ./cmd/apogee/ -run 'Firing|Hint|Headless'`;
`grep -c "hintNotice" cmd/apogee/wire_firing.go` is ≥ 1.

**Commit:** `feat(firing): an unadvertised model says so on every driver`

---

## 11. The daemon narrates its Firing

**What.** Depends on items 3 and 10. `cmd/apogee/daemonfire.go:224` stops blanking the notices:
`cfg, routing, notices, err := firingConfig(...)` and each notice is emitted through the daemon log
(`log.line("%s", n)`, the shape `cmd/apogee/daemon.go:232` already uses for retired-mechanism
notices). The comment at `daemonfire.go:222-223` — "the rebind notices are dropped … a Firing's
narration is the session record it leaves behind" — is a documented decision this item REVERSES:
rewrite it to say the daemon log carries the narration and the session record still carries the
run. After the run returns, log the ANOMALY notices only from
`notice.ContextFileNotices(res.ContextFiles)` (`Anomaly: true` — unreadable files and the oversize
warning); the loaded-files line stays off the daemon log by ratified call. The daemon log needs
reaching from the firing path: pass the existing `*daemonLog` down rather than building a second
one. `docs/manual/daemon.md`'s *What a firing leaves behind* gains one sentence naming both.

**Regression guard.** This item OWNS the daemon-log plumb: `newDaemonWiring`
(`cmd/apogee/daemonfire.go:95`) takes the existing `*daemonLog` built at `cmd/apogee/daemon.go:184`
and handed over at its one production call site (`cmd/apogee/daemon.go:211`); its five test call sites
are `cmd/apogee/daemonfire_test.go:54` and `cmd/apogee/daemon_test.go:628/668/687/853`. Items 12 and
14 use THAT plumb and add no second one. `cmd/apogee/schedule.go:126` (the TUI `/schedule` Driver) also blanks that
return and MUST keep doing so — the ratified call is daemon-only. Leave its `_` and its comment
untouched; a "make both consistent" edit is out of scope.

**Files:** `cmd/apogee/daemonfire.go`, `cmd/apogee/daemon.go`, `cmd/apogee/daemonfire_test.go`,
`cmd/apogee/daemon_test.go`, `docs/manual/daemon.md`

**Tests.** A Firing whose config resolution produced notices logs each verbatim through the daemon
log; a run whose result carries an unreadable context file logs that line and NOT the loaded-files
line; a clean run logs neither.

**Acceptance.** `go test ./cmd/apogee/ -run 'Daemon'`;
`grep -c "ContextFileNotices" cmd/apogee/daemonfire.go` is ≥ 1;
`grep -c "notices are dropped" cmd/apogee/daemonfire.go` is `0`.

**Commit:** `feat(daemon): the firing's notices and context-file anomalies reach the log`

---

## 12. The daemon warns on unconfined Auto and pre-warms, latched per process

**What.** Depends on item 11. In `cmd/apogee/daemonfire.go`, at the top of a Firing once its mode and
roots are known (`:194` resolves roots, `:232` reads `f.Mode`): when `f.Mode == domain.ModeAuto &&
!opts.ConfineToWorkspace`, emit `unconfinedAutoWarning` (`cmd/apogee/wire_boot.go:34`, reused
verbatim — never re-spelled) through the daemon log, LATCHED so it appears once per daemon process
however many Auto Firings run. Then, gated by `shouldPrewarmLabelWalk(f.Mode,
opts.ConfineToWorkspace, wiring.confiner.Capabilities().FSWrite)`, call
`platform.PrewarmLabelWalk(wiring.confiner, roots.workspace, w)`, latched per workspace path (a
`map[string]struct{}` on the wiring, guarded — Firings can overlap). `w` is the `*daemonLog` item 11
plumbs onto the wiring, wrapped as an `io.Writer`: both notices leave by ONE seam, this item adds no
plumb of its own, and `cmd/apogee/daemon.go` is therefore NOT in its Files.
Both latches live on the daemon wiring, not in package state, so a second daemon in one test
process warns on its own.

**Regression guard.** The daemon already prints `probe.ResidualNotice` at boot with mode hard-coded
to `domain.ModeAuto` (`cmd/apogee/daemon.go:241-244`); that is the DEGRADED cell and stays where it
is. This item adds the UNCONFINED cell only, and it belongs per Firing because a schedule entry
carries its own mode — do not move either notice to the other's site. `platform.PrewarmLabelWalk` is
an empty function off Windows (`internal/platform/prewarm_other.go:19`), so on the suite's hosts the
call emits nothing: the test asserts the LATCH (`len(wiring.prewarmed) == 1` after two Firings on one
workspace) plus `shouldPrewarmLabelWalk`'s verdict — the shape `cmd/apogee/wire_boot_test.go:30`
already uses for the gate — never "reaches the pre-warm".

**Files:** `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`

**Tests.** Two Auto Firings under `confine-to-workspace: false` log the warning exactly once, with
the exact `unconfinedAutoWarning` text; a `plan` Firing under the same config logs it never; a
confined Auto Firing on an `FSWrite: true` fake confiner latches the pre-warm once per distinct
workspace — `len(wiring.prewarmed)` is 1 after two Firings on one workspace and 2 after a second one.

**Acceptance.** `go test ./cmd/apogee/ -run 'Daemon'`;
`grep -c "unconfinedAutoWarning" cmd/apogee/daemonfire.go` is ≥ 1;
`grep -c "shouldPrewarmLabelWalk" cmd/apogee/daemonfire.go` is ≥ 1.

**Commit:** `feat(daemon): an unconfined auto firing says so, once, and pre-warms its workspace`

---

## 13. `run.Result` carries the written-files preview

**What.** Recast at the regression check (2026-09-03). `Agent.UndoPreview` is the WRONG source:
`Journal.Preview()` returns only the TOP un-undone group — one exchange, not the run — computes its
changes by reading and hashing the filesystem (`internal/undo/journal.go:246`, `:352`), and spells
REVERT verbs. Add a paths-only, whole-run view instead: `func (j *Journal) Wrote() []string`
returning the distinct paths recorded across ALL groups in first-write order, with no filesystem
access and no hashing; expose it as `Agent.WroteFiles() []string` (`internal/agent/agent.go`, beside
`UndoPreview` at `:825`). Then `internal/run/run.go`: `Result` carries `Wrote []string`, read in
`Once` after `a.Run(ctx)` returns and BEFORE the deferred close fires, faulted runs included — a
faulted Auto run is exactly the one whose writes a human needs to see. An empty slice means "this run
wrote nothing recorded". The journal stays memory-only — nothing is persisted, no verb is added — so
what this field buys is a human-readable account of what the Firing changed, rendered by item 14.

**Regression guard.** The list is a REPORT, never a handle: paths only, no `Generation` and no
`undo.Change`, so nothing in the tree can treat it as revertible. It also costs no filesystem work —
`Journal.Preview`'s `classify` (`internal/undo/journal.go:307-321`) reads every recorded path back
and SHA-256s it, work only a human's `/undo` pays today and which no `run.Once` — headless, daemon,
`/schedule`, the bench (ADR 0031) — may start paying. `Journal.Preview` itself is untouched: the TUI
keeps it. Item 3 lands the first `run.Result` field and edits `internal/run/run_test.go` before this
item does, so this item locates BOTH by anchor text — the `Result` struct's closing brace and the
`Once` assignment site — never by the line numbers cited here, which item 3 will have moved.

**Files:** `internal/undo/journal.go`, `internal/undo/journal_test.go`, `internal/agent/agent.go`,
`internal/run/run.go`, `internal/run/run_test.go`

**Tests.** `Journal.Wrote` returns every recorded path across two groups, de-duplicated, in
first-write order, and touches no file (a journal whose recorded paths have since been deleted still
reports them); a run whose model writes two files reports both paths on `Result.Wrote`; a read-only
run reports an empty `Wrote`; a faulted run that wrote one file still reports it.

**Acceptance.** `go test ./internal/undo/ ./internal/agent/ ./internal/run/`; `go build ./...`;
`grep -c "WroteFiles" internal/run/run.go` is ≥ 1;
`grep -c "func (j \*Journal) Wrote" internal/undo/journal.go` is `1`.

**Commit:** `feat(run): the run result carries the files the run wrote`

---

## 14. Headless and the daemon report the files a Firing wrote

**What.** Recast at the regression check (2026-09-03). Depends on items 11 and 13. Headless
(`cmd/apogee/headless.go`), after the context-file notices of item 4 and before the sub-agent lines:
when `res.Wrote` names any path, print the header `changed — <n> file(s) this run:` and then one
INDENTED path per entry on stderr — a plain path list, never the TUI's `undoPathLine` shape
(`internal/tui/undo.go:135`), whose verb column describes undoing rather than writing. Nothing at all
when the slice is empty. The daemon (`cmd/apogee/daemonfire.go`) logs the same block through the
`*daemonLog` item 11 plumbs, after the outcome is recorded, and `docs/manual/daemon.md`'s *What a
firing leaves behind* gains one sentence naming that block, beside item 11's narration sentence.
Neither Driver offers to revert: the journal died with the process and the lines say what happened,
not what can be undone.

**Regression guard.** No `undo.Change.Action` is rendered: on a preview, a file the run CREATED
classifies `ActionDelete` and one it MODIFIED classifies `ActionRestore`
(`internal/undo/journal.go:317-319`), so borrowing that column would print `delete /ws/new.go` under a
header claiming the run wrote it, and a file edited outside the write funnel would print a `skip …
changed since the agent wrote it` line contradicting this item's own "neither Driver offers to
revert". This block is stderr-only on headless, like every other narration — the
stdout contract stays the answer alone, and `TestHeadlessAnswerLandsOnTheProcessStdout`
(`cmd/apogee/headless_test.go:1349`) is the test that proves it. Do not route any part of it to
`cmd.OutOrStdout()`. The header says "changed", not "wrote", because the write funnel journals a
`delete_file` target and a `move_file` SOURCE with `postExists: false`
(`internal/tools/delete_file.go:104`, `internal/tools/file_ops.go:305` →
`internal/tools/path_safety.go:157-158`; `internal/undo/doc.go`'s "a move is simply two records"), so
item 13's paths-only list carries paths the run REMOVED and a `wrote —` header over them would be a
lie. Item 4 adds its own stderr block to `runHeadless` before this item does, so the insertion point
is found by anchor text — after item 4's context-notice loop, before the sub-agent lines — never by
the line numbers cited here, which item 4 will have moved.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire.go`,
`cmd/apogee/daemonfire_test.go`, `docs/manual/headless.md`, `docs/manual/daemon.md`

**Tests.** A `stubRunner` result carrying two written paths puts the header and two indented path
lines on headless stderr and nothing on stdout; an empty `Wrote` adds no line; the daemon logs the
same block for the same result.

**Acceptance.** `go test ./cmd/apogee/ -run 'Headless|Daemon'`;
`grep -c "changed — " cmd/apogee/headless.go` is ≥ 1.

**Commit:** `feat(drivers): an unattended run reports the files it wrote`

---

## 15. `Store.Prune`'s partial-failure contract gets a test

**What.** `internal/session/store.go:334-341` promises that one failed delete does not abort the
sweep — the first error comes back beside the count of what did go — and the loop (`:370-393`)
implements it, but no test drives a failing delete. `Store`'s only injected seam is `now` (`:161`), and that is
enough: `s.now()` runs BETWEEN `scan()` and the delete loop (`internal/session/store.go:357`), so a
`now` that unlinks ONE of the candidate files makes exactly that `Delete` fail with ENOENT while the
rest succeed — first error non-nil AND `removed` > 0, with no chmod and so no root/Windows skip.
`pruneStore` (`internal/session/store_test.go:822-825`) already sets `now`. Add the test beside the
existing prune tests (`internal/session/store_test.go:865-1000`), reusing `pruneStore` and `pruneNow`
(`:817`), under a `MaxAge` policy so the clock is read. No production change.

**Regression guard.** The whole point is the contract's SECOND half: assert BOTH that the returned
error is non-nil AND that the count reflects the records that did go — a test that only checks the
error passes against a loop that aborts on the first failure. An `os.Chmod(dir, 0o555)` fixture
cannot serve it: the store is FLAT and `Delete` unlinks `<dir>/<id>.json`
(`internal/session/store.go:319`), so an unwritable directory fails EVERY candidate and `removed` is
0 — the very half this test exists to assert.

**Files:** `internal/session/store_test.go`

**Tests.** As described: a candidate unlinked from under the sweep by the injected clock yields a
non-nil first error AND a non-zero removed count, with the other expired records gone from disk. No
skip guard — nothing here depends on the process's privileges.

**Acceptance.** `go test ./internal/session/ -run 'Prune'`; `go vet ./internal/session/`.

**Commit:** `test(session): the prune sweep survives a delete that fails`

---

## 16. `--no-save` still applies the retention policy

**What.** `apogee headless --no-save` leaves `store` nil (`cmd/apogee/headless.go:444-450`) and
`gcSessions` returns immediately on a nil store (`cmd/apogee/wire.go:500-505`), so the host that
only ever runs `--no-save` — the case the sweep was placed on this path to cover
(`headless.go:452-457`) — never applies `sessions.max-age` / `max-count`. Fix in headless, not in
`gcSessions`: build a `session.NewStore(roots.sessions)` for the SWEEP unconditionally, and pass
the record-writing store (nil under `--no-save`) to `runOnce` as today. `gcSessions` keeps its
nil-store guard and its silent best-effort posture (`wire.go:490-499`) — both stay true; it simply
stops being handed nil here. The sweep still runs after the run, still prints nothing, still keeps
the run's own record id where one exists.

**Regression guard.** `session.NewStore` must not create or touch the sessions directory as a side
effect of the `--no-save` sweep beyond what a normal sweep does — `--no-save` promises no RECORD,
not an untouched directory, but a run that creates an empty sessions tree where none existed is a
behaviour change worth pinning: assert the directory state a `--no-save` run leaves behind. Once the
sweep store is built unconditionally, no caller passes nil (`cmd/apogee/headless.go:457`,
`cmd/apogee/daemonfire.go:115`, `cmd/apogee/wire_live.go:223`), and `gcSessions`'s own comment
"headless --no-save: no store, nothing to sweep" (`cmd/apogee/wire.go:502`) names the exact case this
item reverses: restate it as "a Driver with no store". The same sentence is echoed in a test comment
at `cmd/apogee/wire_session_test.go:681`, and this item owns both corrections.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/wire.go`,
`cmd/apogee/wire_session_test.go`

**Tests.** A `--no-save` run against a sessions directory holding records older than a configured
`max-age` removes them and writes no record of its own; the same run with retention unset removes
nothing; a saving run is unchanged.

**Acceptance.** `go test ./cmd/apogee/ -run 'Headless'`;
`grep -c "gcSessions" cmd/apogee/headless.go` is ≥ 1.

**Commit:** `fix(headless): --no-save still applies the session retention policy`

---

## 17. `env-allowlist:` on a non-stdio MCP server gets a notice

**What.** `env-allowlist:` is meaningful to the stdio launch only
(`internal/mcp/transport.go:61-62`, `:172-176`); an `sse` or `streamable-http` entry that sets it
(`internal/config/config.go:2273-2276`) is silently ignored. Add a validation notice on the
existing `ApplyConfig` notice sink (`internal/config/config.go:3004-3008`, beside `rosterNotices`)
— the same channel
`unknownToolNotice` (`:957`) and `rosterConflictNotice` (`:975`) ride, printed by root
(`cmd/apogee/root.go:82`), headless (`headless.go:283`) and probe (`probe.go:75`). Wording follows
those two: `apogee: mcp-servers.<name> sets env-allowlist:, which only a stdio server reads —
this <transport> server inherits nothing from it; drop the key or switch the transport to stdio`.
One notice per offending entry, in file order. It is a notice, never a refusal — the entry still
loads. This item adds NO validation for `command:` / `args:` on http transports; that silence is
untouched.

**Regression guard.** The predicate is the two NAMED transports (`mcp.TransportSSE`,
`mcp.TransportStreamableHTTP`), never "not stdio": an entry that omits `transport:` but sets
`command:` + `env-allowlist:` loads today with no notice (`internal/config/config.go:606-610`) and is
refused only at connect (`internal/mcp/transport.go:118-119`), so a non-stdio predicate would print
the sentence with an EMPTY transport word. The notice fires on the ABSENT/PRESENT distinction the pointer encodes: a
non-stdio entry with `env-allowlist: []` (explicitly empty) is still a set key and still notices;
an entry that omits the key never does. A `nil`-vs-empty confusion here makes the notice fire for
every http server in every config.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`

**Tests.** An `sse` entry with a populated allowlist produces the exact sentence naming that
entry; the same entry with `env-allowlist: []` also produces it; an `sse` entry without the key
produces none; a `stdio` entry with the key produces none.

**Acceptance.** `go test ./internal/config/ -run 'MCP|Notice|Config'`; `go vet ./internal/config/`;
`grep -c "only a stdio server reads" internal/config/config.go` is ≥ 1.

**Commit:** `feat(config): an env-allowlist on a non-stdio mcp server says it does nothing`

---

## 18. `TestReportKindsResolveDistinctly` walks `follows`

**What.** The guard at `internal/tui/reportpane_test.go:44-70` walks every declared `reportKind`
through `pane()`, `reportState()` and `reportContent()`, but not through the module's fourth
kind-switch, `reportKind.follows()` (`internal/tui/reportpane.go:132-141`), whose panicking default
is therefore reached by no test — a fourth report added later inherits the walk's guard for three
resolvers out of four and panics at first paint instead of failing the build. Extend the SAME walk:
call `r.follows()` for every kind so the panicking default is exercised by construction. Unlike the
other three resolvers, `follows` is a bool and cannot be checked for distinctness — two kinds
legitimately share an answer (`inspectReport` and `thinkingReport` both follow) — so the assertion
is that the call does not panic, plus a pin of each declared kind's current answer (`usageReport`
false, the other two true) so a silent flip is caught. One production file changes, in PROSE only:
`internal/tui/reportpane.go:74-76` tells the next reader that the walk covers "the first three" while
`follows` "is reached only through the reports the follow tests open, one kind at a time" — false the
moment the walk gains `follows()` — so reword that clause to say the walk covers all four resolvers.
No code change.

**Regression guard.** `internal/tui/reportpane.go:74-76` records the current division of the
module's guards and is SUPERSEDED here: the reword is this item's, not a later sweep's. The point is
that the walk needs no hand-written list: it runs from
`reportKind(0)` to `reportKinds`, so a fourth kind is covered the day it is declared. Do not add a
second test with an enumerated list of the three kinds — that is the shape this guard exists to
avoid.

**Files:** `internal/tui/reportpane_test.go`, `internal/tui/reportpane.go`

**Tests.** The extended walk itself; plus the per-kind answer pin described above.

**Acceptance.** `go test ./internal/tui/ -run 'Report'`; `go vet ./internal/tui/`;
`grep -c "follows()" internal/tui/reportpane_test.go` is ≥ 1.

**Commit:** `test(tui): the report-kind walk covers the follow answer`

---

## 19. The closed entries leave the issue register

**What.** Depends on every item above. The issue register (`bd`) holds OPEN work: a resolved item is
CLOSED there, and its record lives in `CHANGELOG.md` under `[Unreleased]` — closing a bead does not
write a changelog entry, so do both. Close the `Driver-parity gaps` epic `apogee-kk0` together with
its children `apogee-kk0.1`–`apogee-kk0.6` and `apogee-kk0.8`; the `Issues-register sweep — residue
(2026-09-02)` epic `apogee-1ov` together with its children `apogee-1ov.1`–`apogee-1ov.3`; and
`apogee-4w7` (`TestReportKindsResolveDistinctly` does not walk `reportKind.follows`). Give every
`bd close` a `--reason` naming this plan. No "done" narration is written anywhere else. If any bead
in those trees was NOT delivered by items 2–18, it stays OPEN — say so in a dated NOTES line under
this item.

One is already known to be in that class: **An Auto Firing has no undo surface** (`apogee-kk0.7`).
Its VISIBILITY half is closed by items 13-14; its revert half ("nothing a human can revert") is NOT
— those items state that neither Driver offers to revert and keep the journal memory-only. That bead
stays OPEN, its description rewritten to the revert half alone, and it is detached from the closing
epic so the epic closes cleanly. The destination is decided HERE, not left to the implementer:
`bd update apogee-kk0.7 --parent "" -p 3`, top-level parked work.

**Regression guard.** Beads are addressed by ID, never by list position. `bd show <id>` each one
before closing it and confirm the title matches the entry this plan claims to close — the register
was migrated out of the deleted `ISSUES.md` on 2026-09-03, and a stale ID would close the wrong
work. Closing an epic does not close its children: close each child explicitly.

**Files:** none — register state, plus the `CHANGELOG.md` `[Unreleased]` entry the closeout writes.

**Tests.** None.

**Acceptance.** For each of `apogee-kk0`, `apogee-kk0.1`, `apogee-kk0.2`, `apogee-kk0.3`,
`apogee-kk0.4`, `apogee-kk0.5`, `apogee-kk0.6`, `apogee-kk0.8`, `apogee-1ov`, `apogee-1ov.1`,
`apogee-1ov.2`, `apogee-1ov.3` and `apogee-4w7`, `bd show <id> --json | jq -r '.[0].status'` prints
`closed`; `bd show apogee-kk0.7 --json | jq -r '.[0].status'` prints `open` and
`jq -r '.[0].parent // "none"'` prints `none`.

**Commit:** `chore(issues): the parity gaps and the 2026-09-02 residues close in the register`
