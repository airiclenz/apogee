# The solvable-now register — eight beads, and the fourth Driver's silence

**Goal:** close the eight open beads that need nothing this machine does not have — one defect, the
`internal/notice` consolidation, three untested contracts, two doc residues and the demo rig's awk
claim — plus the `/schedule` context-file gap the defect's investigation turned up. No new config
key, no new verb, no version change.

**Date:** 2026-09-05 · **Status:** unexecuted · **Base:** `7cc9bc6c` · **sized for:** ~200k-context host

**Sources**
- issue register (`bd`) → `apogee-m67`, `apogee-v00.3`, `apogee-v00.4`, `apogee-v00.5`, `apogee-v00.6`, `apogee-v00.7`, `apogee-y9g`, `apogee-at8`
- `docs/plans/archived/2026-09-03 - 01 - driver-parity-and-residues-plan.md` — items 8/9/10/13/14/15 and their ratified calls; the residues here were deferred out of it
- ADRs `0031` (Driver parity, benchable-all-the-way-up), `0033` (the scheduler is a library, its Outcome is runner-agnostic), `0043` §4 (a package's `doc.go`), `0024` (a `context-window:` pin outranks an observation)
- `internal/context/budget.go:46-47` (the Budget is the single authority on the split) · `graphics/demo/type.sh:19-23` (the three-awk claim)

**Ratified design calls** (owner, 2026-09-05)
- **Scope:** these eight beads only; every other open bead stays parked (bench arms, owner-run OS passes, code-signing, the undo surface, the grill-gated decisions).
- **Unknown window:** an unattended run SAYS its window is unknown, in the TUI's existing sentence; the context-files composer and the oversize gate are untouched, and no new wording enters the product.
- **Prune seam:** the clock read is hoisted out of the `MaxAge` branch — a genuine `MaxCount`-only sweep, not a 100-year `MaxAge`.
- **`/schedule`:** the context-file ANOMALIES only; the composition notices stay dropped, so the 2026-09-03 call and `cmd/apogee/schedule.go:124-125` both stand.
- **awk:** `type.sh` and `gen.sh` honour `${AWK:-awk}`, and the verified result is recorded.
- **Daemon noise:** the daemon logs the unknown-window line at most once per process, latched exactly as the unconfined-Auto warning is (owner, 2026-09-05).

**Derived calls** (writer, 2026-09-05)
- **`gcSessions`:** the nil guard is DROPPED, not documented — all three production callers build the store unconditionally (`wire_live.go:220`, `daemonfire.go:137`, `headless.go:522`), and the guard's own comment names a Driver that no longer exists.
- **Anomaly carrier:** `schedule.Outcome` gains `ContextAnomalies []string`; the TUI renders them as Firing-block body lines beside the fault and record lines. It is the only path to that surface without a second seam, and `Outcome` is already how both Drivers report a Firing.
- **Composer names:** `notice.ServerOffline(endpoint, failure)` and `notice.WindowUnknown`; the TUI's `unknownWindowNote` becomes a const bound to the latter, so no TUI call site or test moves.

**Standing requirements:** `skills: coding-standards`. Any authorized deviation lands as a dated NOTES line under its item.

**Out of scope:** the bench arms (`apogee-304.*`) · the owner-run Windows/macOS passes (`apogee-2uh.*`, `apogee-m3p`) · code-signing (`apogee-3p1`) · an `apogee undo` verb or a persisted journal (`apogee-kk0.7`) · every grill- or demand-gated parked bead (`apogee-fsx`, `apogee-1bf`, `apogee-09k`, `apogee-3b3`, `apogee-9d8`, `apogee-rms`, `apogee-54f`, `apogee-2sj`, `apogee-96u`, `apogee-37s`, `apogee-p5j`, `apogee-8wy`) · binding an unpinned Firing's observed window (ratified against, plan `2026-09-03 - 01` item 10) · rendering `/schedule`'s composition notices · any version or release change.

**Regression check (2026-09-05, `7cc9bc6c`):**
- **1** — guard folded: the binding-comment grep is widened to `server offline\|upstreamBlockNote`, because the two pins' own comments say the sentence is composed twice and the narrower grep reaches neither.
- **2** — recast: the local `notice` shadow is renamed so the else branch compiles, the daemon latches the line once per process, and the two empty-output assertions are re-aimed. Yields to the ratified daemon-journal rule (`cmd/apogee/daemonfire_test.go:584-587`), which the latch preserves — the item does not supersede it.
- **3** — guard folded: the slice field makes `schedule.Outcome` non-comparable, so seven `==`/`!=` sites convert to `reflect.DeepEqual`; the item now depends on item 2.
- **4** — guard folded: test (b) compares with `reflect.DeepEqual`, and the dependency is item 3 (which lands that import), not item 2.
- **8** — guard folded: `AWK_BIN` goes with the other `readonly`s and the header comment is NOT extended, because both scripts print `--help` by fixed line range.
- **1** (re-check) — the stale "Two non-test files" count is corrected: three after this item, four once item 2 adds `window.go`. The conclusion is unchanged — still far under ADR 0043 §4's ~10-file bar, so no `docmap_test.go` and no file map.
- **2** (re-check) — guard folded: the else is gated on `beat.Answered` too, because both Drivers emit their notices before the offline gate and a failed beat's zero `Resolution` empties `hintNotice`; the test's own `notice` shadow (`wire_firing_test.go:953`) is renamed in the same commit; and the daemon's unstripped-voice comment (`daemonfire.go:334-335`) names the latched line as its one exception.

## 1. One composer for the server-offline refusal, and `internal/notice` gets its `doc.go` — ✅ DONE (2026-09-06)

NOTES (2026-09-06): the widened `server offline\|upstreamBlockNote` grep also hits
`internal/tui/command.go:103` and the failure message inside `headless_test.go`'s pin. Neither was
edited: the first merely names the method that still exists (it binds no copy), and the second is
inside the assertion block the item requires to stay byte-identical.

**What.** Closes `apogee-v00.3` and `apogee-v00.4`. The sentence a Driver prints when the startup
beat never answered is spelled out three times — `internal/tui/heartbeat.go:690`,
`cmd/apogee/headless.go:495`, `cmd/apogee/daemonfire.go:360` — and only comments bind them. Add
`internal/notice/serveroffline.go` with `func ServerOffline(endpoint, failure string) string`,
returning `"cannot send — server offline (" + endpoint + ")"` and, when `failure` is non-empty,
`+ ": " + failure`. It is BYTE-IDENTICAL to all three of today's literals (verified: em dash
U+2014). Concatenation, never `fmt.Sprintf` — the package's house style. The three call sites keep
their own guards, their own endpoint and failure sources and their own delivery (transcript note /
`notStarted` / `schedule.Outcome{}` + error); only the wording moves. The TUI's sibling
`"cannot send — still connecting to "` has one copy and does NOT move.
Move the package doc from the head of `contextfiles.go` into a new `internal/notice/doc.go`, in
`internal/format/doc.go`'s charter shape, ending with the dependency line (`internal/domain` and
`internal/format`, nothing else). After this item `internal/notice` holds THREE non-test files
(`contextfiles.go`, `doc.go`, `serveroffline.go`), and four once item 2 adds `window.go` — still far
under ADR 0043 §4's ~10-file bar, so no `docmap_test.go` and no file map.

**Regression guard.** Every comment that binds the copies is rewritten to point at the composer,
by RULE not by list: `grep -rn 'server offline\|upstreamBlockNote' --include='*.go' internal/ cmd/`.
The grep is widened past the literal because the TUI's doc block at `heartbeat.go:669-677` names
headless but not the daemon, and the two pins' own comments (`cmd/apogee/headless_test.go:762-764`,
`cmd/apogee/daemonfire_test.go:439-441`) say the sentence is composed twice and "Neither is derived
from the other" — which this item falsifies, and which the narrower `server offline` grep never
reaches. Those two test files are comment-only edits: the pins themselves stay byte-identical.

**Files:** `internal/notice/doc.go`, `internal/notice/serveroffline.go`, `internal/notice/serveroffline_test.go`, `internal/notice/contextfiles.go`, `internal/tui/heartbeat.go`, `cmd/apogee/headless.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire_test.go`

**Tests.** New `internal/notice/serveroffline_test.go`, `package notice_test`, matching
`contextfiles_test.go`'s prose-comment style: both shapes pinned by exact equality. The three
existing pins keep spelling the sentence out and must pass UNCHANGED —
`TestSubmitBlockedOfflineKeepsInput` (`internal/tui/heartbeat_test.go:424`),
`TestHeadlessRefusesAServerThatAnsweredNothing` (`cmd/apogee/headless_test.go:765`),
`TestDaemonFireRefusesOnlyAServerThatAnsweredNothing` (`cmd/apogee/daemonfire_test.go:442`). In the
latter two files only the binding comment above the test moves.

**Acceptance.**
- `go build ./... && go vet ./internal/notice/ ./internal/tui/ ./cmd/apogee/`
- `go test ./internal/notice/... ./internal/tui/... ./cmd/apogee/...`
- `test "$(grep -rn 'cannot send — server offline (' --include='*.go' internal/ cmd/ | grep -v _test.go | wc -l)" -eq 1`

commit: `refactor(notice): one composer for the server-offline refusal`

## 2. An unattended run says when its context window is unknown — ✅ DONE (2026-09-06)

NOTES (2026-09-06): consequential edit — internal/notice/doc.go: made necessary by adding
internal/notice/window.go — the package doc's "Two families today" enumeration named
ContextFileNotices and ServerOffline only, and is now three with WindowUnknown's own entry.
NOTES (2026-09-06): the table row `an advertised model is unremarkable`
(cmd/apogee/wire_firing_test.go) is renamed to `an advertised model is unremarkable but its
unknown window is not` — that row IS the plan's new advertised-and-unpinned case, and the old
name asserted the opposite of what it now pins. Two further rows were added beside it, both
named in the item's guard: a pinned+advertised Firing that says nothing about the window, and
the offline case (a beat that never answered).

**What.** Recast at the regression check (2026-09-05). Fixes `apogee-m67`. An unpinned headless or
daemon run derives its Budget from configuration alone, so `ContextFilesReport.SystemShare` is 0
(`internal/agent/contextfiles.go:281`
← `internal/context/budget.go:72-74`) and the oversize warning, gated on `SystemShare > 0`
(`internal/domain/contextfile.go:42`), can never fire there however far the standing content grows.
Depends on item 1. The report is NOT re-taken and the Firing does NOT bind the observed window —
that is ratified against (plan `2026-09-03 - 01` item 10, pinned by
`TestFiringConfigSaysWhenTheModelIsNotAdvertised`, `cmd/apogee/wire_firing_test.go:970-974`).
Instead the run says so: move `unknownWindowNote` (`internal/tui/heartbeat.go:501-502`) into
`internal/notice` as exported `WindowUnknown`, leaving `const unknownWindowNote = notice.WindowUnknown`
behind so no TUI call site or test moves. In `firingConfig` (`cmd/apogee/wire_firing.go:214-217`)
the hint append becomes: append `hintNotice(...)` when it is non-empty, ELSE append
`notice.WindowUnknown` when `spec.MaxContextTokens == 0`. That `else` is the whole no-double-say
rule — `hintNotice`'s default branch already carries its own unknown-window clause, and it is
reached exactly when `bound <= 0`. Headless prints the line on stderr and the daemon logs it
through channels both already read (`headless.go:468-470`, `daemonfire.go:336-338`).

**Regression guard.** The sentence is announced text and must stay byte-identical to the TUI's:
one spelling, in `internal/notice`. `notice.ContextFileNotices` is NOT touched — a "share unknown"
line there would reach the TUI's pre-bind startup notice as well.
Five binding changes. (i) COMPILE: `notice` is already a local string variable at
`cmd/apogee/wire_firing.go:215`, shadowing the package name — rename it in the same statement
(`if hint := hintNotice(spec.Model, beat.Resolution, beat.ContextWindow, spec.MaxContextTokens); hint != "" { notices = append(notices, hint) } else if spec.MaxContextTokens == 0 && beat.Answered { notices = append(notices, notice.WindowUnknown) }`),
which frees the package name for the else branch; `wire_verbs.go:58`'s twin local is untouched. The
SAME shadow sits in the test: `cmd/apogee/wire_firing_test.go:953`'s scan loop is `for _, notice :=
range notices`, and it is renamed to `n` in the same commit — a row asserting `notice.WindowUnknown`
inside that loop does not compile once the file imports `internal/notice`.
(ii) DAEMON LATCH: the daemon logs `notice.WindowUnknown` at most once per process, recognised by
exact-string compare against the exported const and latched on `daemonWiring` the way the
unconfined-Auto warning already is (`warnedUnconfined` / `latchUnconfinedWarning`,
`cmd/apogee/daemonfire.go:95-96,197-212`, pinned by `TestDaemonFireWarnsOnceOnUnconfinedAuto`);
every OTHER composition notice keeps logging per Firing as today. Headless is one run per process
and always says it. The ratified journal rule recorded at `cmd/apogee/daemonfire_test.go:584-587`
therefore STANDS and its comment is not rewritten — the item does not supersede it. A daemon test
pins the latch: a second Firing in the same process logs the line no more. (iii) The two
empty-output assertions are named in the item and re-aimed rather than left to break:
`cmd/apogee/daemonfire_test.go:632` ("a clean run says nothing at all") asserts the absence of the
context-file and written-files lines instead of an empty buffer, and
`cmd/apogee/wire_firing_test.go:766` ("no key names no seat and asks nothing") asserts that no
`sub-agents:` notice is present instead of `len(notices) == 0`. (iv) OFFLINE ORDER: the else is gated
on the beat as well — `else if spec.MaxContextTokens == 0 && beat.Answered`. Both Drivers emit the
notices BEFORE their offline gate (`headless.go:468` vs `:494`, `daemonfire.go:336` vs `:359`), and a
beat that never answered carries a zero `beat.Resolution`, so `hintNotice` returns ""
(`cmd/apogee/upstream.go:577-579`): ungated, an unpinned run against a dead endpoint would say the
window is unknown AHEAD of "cannot send — server offline", and in the daemon that refused Firing
would burn the once-per-process latch, leaving the first Firing that really runs silent. (v) The
daemon's own rule-comment at `cmd/apogee/daemonfire.go:334-335` — the notices are "Printed in the
composer's own voice, unstripped, as runHeadless prints the same lines" — names the latched line as
its ONE exception; the rule itself stands for every other notice.

**Files:** `internal/notice/window.go`, `internal/notice/window_test.go`, `internal/tui/heartbeat.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire_test.go`

**Tests.** New rows on `TestFiringConfigSaysWhenTheModelIsNotAdvertised`: an unpinned Firing with an
ADVERTISED model carries the exact sentence in `notices` and still leaves
`cfg.Context.MaxContextTokens == 0`; a pinned Firing carries no such line; an unadvertised unpinned
Firing says it ONCE, in the hint. Journey tests, each feeding the exact emitted string: a headless
run prints it on stderr (`cmd/apogee/headless_test.go`, beside
`TestHeadlessPrintsTheContextFileNotices`), a daemon Firing logs it (`cmd/apogee/daemonfire_test.go`,
beside `TestDaemonFireLogsTheCompositionsNotices`) and a SECOND Firing in the same process logs it no
more — the latch, shaped on `TestDaemonFireWarnsOnceOnUnconfinedAuto`. The two empty-output
assertions named in the guard are re-aimed in the same commit: `cmd/apogee/daemonfire_test.go:632`
and `cmd/apogee/wire_firing_test.go:766`, and the scan loop's own `notice` shadow
(`cmd/apogee/wire_firing_test.go:953`) is renamed to `n` there. One further row pins the offline
case: an unpinned Firing whose beat did NOT answer carries no unknown-window line, so the Driver's
refusal stays the only sentence that run gets.

**Acceptance.**
- `go test ./internal/notice/... ./internal/tui/... ./cmd/apogee/...`
- `go test ./cmd/apogee/ -run 'TestFiringConfig|TestHeadless|TestDaemonFire' -count=1`

commit: `fix(firing): an unattended run says when its context window is unknown`

## 3. A `/schedule` Firing reports the context files it could not read — ✅ DONE (2026-09-06)

NOTES (2026-09-06): consequential edit — layout.md: made necessary by the new Firing-block body line — the spec's enumeration of that block's body (stats, fault line, record pointer) was complete and became false.
NOTES (2026-09-06): the fill goes through a new `contextAnomalies(run.Result.ContextFiles)` helper beside `firingSpend` in cmd/apogee/schedule.go rather than an inline loop, so the Outcome stays one composite literal — the same shape `firingSpend` was factored out for.

**What.** Closes the fourth-Driver gap the item-2 investigation found: `scheduleWiring.fire` drops
`res.ContextFiles` entirely (`cmd/apogee/schedule.go:176-186` maps only the Outcome's own fields),
so an unreadable context file in a Firing raised from a session is invisible on every surface.
`schedule.Outcome` gains `ContextAnomalies []string` (`internal/schedule/schedule.go:78-110`),
documented in that type's voice: RAW text a surface escape-strips at its own render seam, and the
library reads none of it (ADR 0033, runner-agnostic). `scheduleWiring.fire` fills it from
`notice.ContextFileNotices(res.ContextFiles)` keeping `Anomaly` entries only — the daemon's ratified
rule at `daemonfire.go:393-397` — and does not strip here. The TUI appends one body line per entry
where the fault and record lines are built (`internal/tui/schedule.go:518-526`), so they land in the
Firing block beside `firingFaultLine` and `firingRecordLine` and are stripped at that block's own
sanitize seam. Depends on item 2 (shared `cmd/apogee/daemonfire_test.go`).

**Regression guard.** The composition notices stay dropped: `cmd/apogee/schedule.go:127` keeps its
`_`, and the comment at `:124-125` ("a Firing's narration is the session record it leaves behind")
is left standing — plan `2026-09-03 - 01`'s ratified *Firing notices* call is NOT superseded here.
Only `res.ContextFiles` moves.
A slice field makes `schedule.Outcome` NON-COMPARABLE, so the seven live `==`/`!=` comparisons of it
(`internal/schedule/schedule_test.go:543,579,636,669,704` and `cmd/apogee/daemonfire_test.go:347,516`)
convert to `reflect.DeepEqual`, both files gaining a `reflect` import. This item lands the
`daemonfire_test.go` conversion and import that item 4 then reuses.

**Files:** `internal/schedule/schedule.go`, `internal/schedule/schedule_test.go`, `cmd/apogee/schedule.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/daemonfire_test.go`, `internal/tui/schedule.go`, `internal/tui/schedule_test.go`

**Tests.** `cmd/apogee/schedule_test.go`: a Firing whose stubbed `run.Result` carries an unreadable
note and a loaded file yields exactly the anomaly text in `Outcome.ContextAnomalies` and nothing
else; a clean report yields none. `internal/tui/schedule_test.go`: the Firing block renders one body
line per anomaly, with the exact composed string, and strips escapes in it. The seven `Outcome`
comparisons named in the guard become `reflect.DeepEqual` and must keep asserting exactly what they
assert today — no assertion is weakened to get the tree compiling.

**Acceptance.**
- `go build ./... && go test ./internal/schedule/... ./internal/tui/... ./cmd/apogee/...`
- `go test ./cmd/apogee/ -run 'TestSchedule' -count=1`

commit: `feat(schedule): a Firing reports the context files it could not read`

## 4. The daemon's failed-Firing narration is pinned on both Result shapes — ✅ DONE (2026-09-06)

NOTES (2026-09-06): subtest (b) asserts the absence of the two RESULT narrations (the context-file lines and the `changed — ` block) rather than an empty log — the composition's own notices, including the unknown-window sentence, precede the run and are what `TestDaemonFireLogsTheCompositionsNotices` and the neighbouring "a clean run says nothing at all" subtest already pin; the subtest's comment says so.

**What.** Closes `apogee-y9g`, whose text is partly stale: `fd27a732` already pinned the
`res.Wrote`-on-failure half (`cmd/apogee/daemonfire_test.go:680-694`). Depends on item 3. What is
still unpinned, and what the comments at `daemonfire.go:383-388` and `:421-423` overclaim: the
anomalies are "reported whether the run answered or failed" only for a failure that still produced
a Result. A run that fails with a ZERO `run.Result` logs nothing at all — `ContextFileNotices` of an
empty report is nil, `writtenFilesLines(nil)` is nil — returns a zero `schedule.Outcome{}`, and
because `res.SessionID` is empty returns the bare error with no `(partial run saved as %s)`
wrapping. Pin all three facts and amend both comments to say "a failure that still produced a
Result" rather than "whether the run answered or failed". No production seam is needed: `runOnce` is
a package var (`cmd/apogee/headless.go:95`), the harness swaps it (`daemonfire_test.go:62-69`),
`stubRunner` carries both `res` and `err`, and `harness.raise` returns the error instead of fataling.

**Regression guard.** The shared `report` fixture at `daemonfire_test.go:591-598` yields three
notices and `:609-611` fatals if the count moves — a new subtest builds its OWN smaller
`domain.ContextFilesReport` rather than reusing it.
The dependency is on item 3, not item 2 — item 3 lands the `reflect` import and the
`reflect.DeepEqual` conversions in `cmd/apogee/daemonfire_test.go` that this item's new subtests
reuse: the slice field item 3 adds makes `schedule.Outcome` non-comparable, so test (b) cannot
write `out == schedule.Outcome{}`.

**Files:** `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`

**Tests.** Two subtests beside `TestDaemonFireLogsContextFileAnomaliesAlone`: (a) `runOnce` fails
after a Result carrying an unreadable-context-file note and a written path — both the exact anomaly
line and the `changed —` block are logged, and the error is wrapped `(partial run saved as …)`;
(b) `runOnce` fails with a zero `run.Result` — nothing is logged, `reflect.DeepEqual(out,
schedule.Outcome{})` holds (written as `if !reflect.DeepEqual(…)`, reusing the `reflect` import item
3 adds), and the error is returned unwrapped.

**Acceptance.**
- `go test ./cmd/apogee/ -run 'TestDaemonFire' -count=1 -race`

commit: `test(daemon): pin the failed-Firing narration on both Result shapes`

## 5. `Store.Prune`'s partial-failure contract is pinned on the count-only sweep — ✅ DONE (2026-09-06)

NOTES (2026-09-06): survivor set compared with `slices.Equal` (already imported, and the style the
neighbouring `storedIDs` assertion at store_test.go:929 uses) rather than a new `reflect` import.
NOTES (2026-09-06): non-vacuity checked — with the hoist reverted the new test fails ("Prune
returned no error"); restored before finishing.

**What.** Closes `apogee-v00.5`. `Store.Prune` reads `s.now()` only inside `if r.MaxAge > 0`
(`internal/session/store.go:355-358`), so the injected clock — the only seam between `scan()` and
the delete loop, and the one `TestPruneReportsFirstErrorAndKeepsSweeping` uses to unlink a candidate
mid-sweep — never fires on a `MaxCount`-only sweep, leaving "first error, keep sweeping" pinned on
the age path alone. Hoist the read: `now := s.now().UTC()` above the branch, `cutoff = now.Add(-r.MaxAge)`
inside it, with a comment saying the read is deliberately unconditional so the seam reaches both
paths (otherwise it reads as dead work). Behaviour is unchanged; `s.now` is never nil (there are no
`session.Store{…}` composite literals in the repo).

**Regression guard.** `os.Remove` failing is the ONLY way to produce `firstErr` — `Delete`'s
`validateID` cannot fail for a candidate, since `decodeRecord` already validated the id and the loop
skips `f.stem != f.meta.ID`. Do not reach for a chmod: this box runs as root with `CAP_DAC_OVERRIDE`
and an unwritable directory does not stop the unlink.

**Files:** `internal/session/store.go`, `internal/session/store_test.go`

**Tests.** New `TestPruneReportsFirstErrorOnACountOnlySweep` beside the age-path pin
(`store_test.go:1008`), reusing `pruneStore`/`pruneAges` and the same unlink-from-`now` technique on
`Retention{MaxCount: 1}`: the first failed delete is returned and names the vanished id, the sweep
continues past it, and the survivor set is the one the budget allows.

**Acceptance.**
- `go test ./internal/session/... -count=1 -race`
- `go test ./cmd/apogee/ -run 'TestGCSessions|TestWireSessionSweeps' -count=1`

commit: `test(session): pin Prune's partial-failure contract on the count-only sweep`

## 6. `gcSessions` loses its unreachable nil-store guard — ✅ DONE (2026-09-06)

NOTES (2026-09-06): the regression grep confirmed the only nil caller was
`wire_session_test.go:681`; the "no store directory" subtest keeps its missing-root
assertion unchanged.

**What.** Closes `apogee-v00.6`. Item 16 of plan `2026-09-03 - 01` made headless build the sweep
store unconditionally, so all three production callers hand `gcSessions` a non-nil store
(`cmd/apogee/wire_live.go:220`, `cmd/apogee/daemonfire.go:137`, `cmd/apogee/headless.go:522`) and
`session.NewStore` can never return nil. The guard survives only because one test calls it directly.
Per the derived call: DROP it. Delete the `if store == nil { return }` block at
`cmd/apogee/wire.go:500-502` and its only caller-of-nil, `cmd/apogee/wire_session_test.go:681`. The
function doc at `:489-498` does not mention the nil case, so it needs no edit — but the inline
comment being deleted ("a Driver with no store") is the claim that is no longer true.

**Regression guard.** Nothing else in the repo passes nil: verify with
`grep -rn 'gcSessions(' --include='*.go' .` before deleting, and keep the surrounding subtest's
first assertion (a sweep over a missing sessions root creates nothing) intact.

**Files:** `cmd/apogee/wire.go`, `cmd/apogee/wire_session_test.go`

**Tests.** No new test — the change removes an unreachable branch and the test line that reached it.
`TestGCSessionsAppliesTheConfiguredPolicy`'s other four cases must pass unchanged.

**Acceptance.**
- `go build ./... && go vet ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestGCSessions|TestWireSession|TestDaemon|TestHeadless' -count=1`

commit: `refactor(wire): drop gcSessions' unreachable nil-store guard`

## 7. `follows()`'s doc names its siblings instead of counting them

**What.** Closes `apogee-v00.7`. `internal/tui/reportpane.go:129` says the switch panics "for the
reason the three resolvers above are", but only `pane()` (`:88`) precedes `follows()` (`:132`) —
`reportState` (`:149`) and `reportContent` (`:181`) sit below it. Reword positionally-neutrally
("its three siblings"), since `reportKind`'s own doc (`:70-76`) already enumerates all four by name
and stays the authority.

**Regression guard.** State the RULE, not this one line: every comment in `reportpane.go` that
locates a sibling by POSITION rather than by name gets the same treatment — find them with
`grep -n 'above\|below' internal/tui/reportpane.go` and fix each that names a function's position.
No test or doc pins the current wording (`grep -rn 'three resolvers' .` returns this line alone).

**Files:** `internal/tui/reportpane.go`

**Tests.** None — comment-only. `TestReportKindsResolveDistinctly`
(`internal/tui/reportpane_test.go:39`) remains the behavioural guard the comment alludes to.

**Acceptance.**
- `go build ./... && go vet ./internal/tui/`
- `test "$(grep -c 'three resolvers above' internal/tui/reportpane.go)" -eq 0`

commit: `docs(tui): follows() names its siblings instead of counting them`

## 8. The demo rig's awk is selectable, and the three awks are checked

**What.** Closes `apogee-at8`. `graphics/demo/type.sh:19-23` claims the generator is byte-stable
across BSD awk, gawk and mawk, but every awk call is the bare `awk`, so the claim could only be read,
never run. Give both scripts one override — `readonly AWK_BIN="${AWK:-awk}"` — and use it at every
awk call site: `type.sh:116` (the `--check` classifier), `type.sh:182` (the pooled-mean gate),
`type.sh:273` (the generator) and `gen.sh:100` (the tape guard). `type.sh --check` re-invokes itself
through `bash "${BASH_SOURCE[0]}"`, so an `AWK=…` prefix propagates without further work. Record the
verification in `graphics/demo/README.md` — the one-line recipe and the result measured 2026-09-05 on
mawk 1.3.4, GNU Awk 5.2.1 and one-true-awk 2023-11-27: `--check` passes and both the generated blocks
and the whole generated hero tape are byte-identical under all three. Install line for a fresh box:
`apt-get install -y gawk original-awk` (`original-awk` IS the one-true-awk; both are already present
on this machine).

**Regression guard.** No golden total is re-baselined and no band, seed, pause odd or typed string
is edited — the generated bytes must not move. The override defaults to `awk`, so `record.sh` and
`gen.sh` behave exactly as today when `AWK` is unset.
`readonly AWK_BIN="${AWK:-awk}"` goes with the other `readonly`s below the header comment, and the
header comment is NOT extended: `type.sh:81` and `gen.sh:41` print `--help` by fixed line range
(`sed -n '2,7p'` and `sed -n '2,5p'`), so a new header line would silently change announced help
text. The `AWK` knob is documented in `graphics/demo/README.md` only.

**Files:** `graphics/demo/type.sh`, `graphics/demo/gen.sh`, `graphics/demo/README.md`

**Tests.** Shell, not Go: the acceptance commands below ARE the test — the rig has no Go test.

**Acceptance.**
- `for a in mawk gawk original-awk; do AWK=$a graphics/demo/type.sh --check || exit 1; done`
- `for a in mawk gawk original-awk; do AWK=$a graphics/demo/gen.sh graphics/demo/tapes/hero.tape "/tmp/hero.$a.tape" || exit 1; done && cmp /tmp/hero.mawk.tape /tmp/hero.gawk.tape && cmp /tmp/hero.mawk.tape /tmp/hero.original-awk.tape`
- `graphics/demo/type.sh --check` (no `AWK` set — the default path still works)

commit: `test(demo): the typing rig's awk is selectable, and the three agree`
