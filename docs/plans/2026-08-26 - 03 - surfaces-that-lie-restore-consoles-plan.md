# Surfaces that lie about state, session restore, and Console ownership

**Goal:** every surface that states a fact about the running session reads that fact from where
it lives — the live engine, the typed tool outcome, the walked diff, the verdict — and a session
boundary (restore) leaves nothing of the outgoing conversation running or miscounted.

**Date:** 2026-08-26 · **Status:** unexecuted · **sized for:** ~200k-context host

**Evidence (security audit 2026-08-25 §3.3/§3.6, code audit 2026-08-25 §7, refocus corrections
§3, `ISSUES.md` open defects):** on a stock install `/settings` reports the BOOT value of
`confine-to-workspace` and `mode` (`cmd/apogee/wire_options.go:334` hands the pane a
`config.Options` captured at wire time) while `/confine off` and Shift+Tab have moved the engine —
and ⏎ on the row it marks "(current)" re-applies the stale rung (F-14, F-31). `apogee headless`
exits 0 and the daemon logs `completed` on a Turn the engine ABANDONED, because
`domain.StepResult.Faulted` never reaches `run.Result` (`internal/run/run.go:235-243`, F-15).
`git_status`' outcome slot says "0 changed" whenever ANY listed path contains "Working tree
clean" (`internal/tui/toolregistry.go:757`, F-20); `git_diff_range`'s diffstat skips every changed
line whose content begins with `--`/`++` (`toolregistry.go:836`, F-24); a config-file save
reconfigures the live session with no transcript line (`settingswatcher.go:280-297`, F-23). A
`/sessions` restore leaves the outgoing conversation's Consoles running (`RestoreSession`,
`internal/agent/agent.go:921`, sweeps nothing — F-38 = `ISSUES.md:52`) and stamps the outgoing
session's token sums onto the reopened record (`fold.go:125` `m.usage = totals` latest-wins, C-16).
`lookupConsole` never checks `Owner` (`internal/tools/console_common.go:69`, F-37) and ownership is
keyed on the model-supplied call id (`subagent.go:380`, F-43). `commitIsPublished` prefix-matches
`origin/` on `%D` decoration (`internal/tools/git.go:620`, C-05); `probe model` counts
`tool_calls:[{}]` as native tool-call evidence (`internal/probe/battery.go:169`, C-18).

**Authoritative sources:**
- `internal/agent/agent.go:315-360` (`usageTally`, `record`), `:414-433` (`Close`,
  `closeConsoles`), `:577-625` (`Mode`, `SetMode`, `ConfineToWorkspace`, `SetConfineToWorkspace`),
  `:892-929` (`ClearContext`, `RestoreSession`), `:300` (`lastFault`), `internal/agent/loop.go:543`
  (`emitLoopFault`), `internal/agent/subagent.go:98-187` (`runSubAgent`), `:241-424`
  (`newChildAgent`; `child.callID = spawnCallID` at `:380`, `child.consoles = a.consoles` at
  `:422`), `internal/agent/dispatch.go:770` (`WithSpawnCallID` stamp).
- `internal/domain/config.go:656-664` (`StepResult.Faulted`), `internal/domain/ask.go:171-188`
  (`WithSpawnCallID` / `SpawnCallIDFromContext`), `internal/domain/toolsummary.go:46` (the sealed
  `ToolSummary` sum; `toolsummary_test.go:37` `want = 7`), `apogee.go:344` (variant re-exports).
- `internal/run/run.go:66-90` (`Result`), `:235-243` (the `Result` literal), `internal/run/doc.go:47`.
- `cmd/apogee/headless.go:38-44` (exit codes), `:70` (`exitCodeFor`), `:407` / `:447`
  (`headlessSummary`); `cmd/apogee/daemon.go:604-633` (`notify`, `daemonOutcome`);
  `cmd/apogee/daemonfire.go:237` and `cmd/apogee/schedule.go:149` (`schedule.Outcome` literals);
  `internal/schedule/schedule.go:76-89` (`Outcome`), `:124-132` (`EventCompleted`/`EventFailed`);
  `internal/tui/schedule.go:556` (`firingStats`); `docs/manual/headless.md:50-56`,
  `docs/manual/daemon.md:96-103`.
- `cmd/apogee/wire_options.go:30-110` (`options`), `:181-186` (the `Settings:` literal), `:320-334`
  (`settingsHost`, `Rows`); `cmd/apogee/wire_engine.go:59-64` (`lateEngine`), `:275` (`SetMode`),
  `:417` (`ConfineToWorkspace`); `cmd/apogee/settingsrows.go:100-130` (`settingsRows`).
- `internal/tui/settings.go:167` (`settingEdit`), `:279-286` (`settingsEditMarker`), `:562` (the
  enum sub-list opens on `settingsCurrentValue`), `:971` (`settingsPersistedValue`), `:1078-1096`
  (`settingsCurrentValue`, the `server` carve-out), `:1380-1391` (`settingsValueCell`);
  `internal/tui/settingsapply.go:155` (`settingsApplied`), `:186-201` (`settingsApplyLive`, the
  `opts.Mode` mirror at `:197`), `:352` (`settingKeyMode`), `:377` (`recordSettingEdit`);
  `internal/tui/settingswatcher.go:205-218` (`applyReloaded`), `:242` (`configWatchStalledNote`),
  `:280-297` (`foldConfigChanged`); `internal/tui/model.go:1324-1330` (Shift+Tab mirror),
  `:2622-2660` (`footerContent`), `:2797` (`modeMarker`); `internal/tui/confine.go:30` (`runConfine`);
  `internal/tui/tui.go:563-660` (the `Engine` seam; `ConfineToWorkspace()` at `:624`), `:675`
  (`Options.Mode`), `:814` / `:1343` (`Confinement`, `ConfinementInfo`), `:1315` (`ResumedSession`),
  `:1411` (`SettingRow`).
- `internal/tui/fold.go:67-94` (`usageTotals`, `usageReading`), `:110-125` (`foldStats`);
  `internal/tui/usage.go:185-206` (`usageRows`), `:312` (`usageSum`); `internal/tui/model.go:576`
  (`replayResumed`), `internal/tui/sessions.go:575-579` (`resumeLoaded`),
  `internal/tui/sessionsave.go:51`, `internal/tui/commandrun.go:159-165` (the `/new` accounting stance).
- `internal/tui/toolregistry.go:362` (the `git_status` row), `:751-773` (`gitStatusSection`,
  `changedFilesStat`), `:617` (`consoleStatusMarker`), `:629` (`exitMarkerPhrase`), `:828-847`
  (`diffLinesStat`), `:1267-1286` (`outputDetail`, `consoleDetail`); `internal/tools/git.go:551-560`
  (the amend guard), `:617-627` (`commitIsPublished`), `:938-982` (`renderGitStatus`,
  `writeStatusSection`); `internal/tui/diffbody.go:135-137`, `:175` (`diffRegionCutter`), `:419-447`
  (`gitDiffFileSections`, `gitDiffWalk`), `:519-536` (`takeHunkLine`).
- `internal/tui/toolview.go:89-149` (`branchSummary`, `namedSummary`, `quotedSummary`,
  `typedSummary`), `:117-120` (the verdict wordings), `:588-610` (`summaryOnly`, `promotedOutput`),
  `:875-887` (`runAggregate`), `:898` (`failedCalls`), `:1197-1207` (`applyStat`);
  `internal/tui/toolleader.go:196` (the one `summaryStyle` call), `:282-291` (`summaryStyle`),
  `:307-315` (`failedSummary`); `internal/tui/subagentblock.go:29` (`subAgentSpan`), `:57`
  (`subAgentFramed`), `:190` / `:210` (`subAgentPromptRows`), `:325` (`subAgentFinished`),
  `:409-433` (`renderSubAgentMemberRows`), `:450-467` (`expandedSubAgentView`,
  `collapsedSubAgentView`), `:496` (`subAgentSummary`); `internal/tui/render.go:358` (the framed
  branch), `:541-546` (the lone `entryToolCall` block).
- `internal/tools/console_common.go:69-95` (`lookupConsole`, `openConsoleIDs`),
  `internal/tools/console_open.go:150-152` (`Owner:` stamp), `internal/console/registry.go:43`/`:86`
  (`Owner`), `:139` (`Open`), `:167` (`Get`), `:177` (`OpenIDs`), `:192` (`Close`), `:211`
  (`CloseOwnedBy`), `:233` (`CloseAll`).
- `internal/probe/battery.go:155-176` (`probeNativeToolCall`), `:218-260` (`probeMultiStepChain`);
  `internal/provider/wirejson.go:187-199` (`toRawResponse`); `internal/probe/battery_test.go:77`,
  `:127` (`script.toolReply`, `chatReply`).
- ADR 0059 (Console = live host state; §1 lifetime, §6 delegation-scoped ownership), ADR 0012
  (`confine-to-workspace`, the `/confine` interlock), ADR 0039 (call-ID events), ADR 0033
  (Firing / scheduler, runner-agnostic library), ADR 0011 (value-copied `Model`), ADR 0037 /
  ADR 0041 (settings apply path, config watcher), ADR 0052 (Edit regions), ADR 0031 (Driver seams).
- `CONTEXT.md` **Console** (`:617`), **Driver** (`:46`), **Firing** (`:71`), **Turn** (`:371`),
  **Session** (`:187`); `docs/manual/configuration.md:36-43` (the watched-config paragraph),
  `:613` (§ Auto mode's blast radius); `docs/manual/commands.md:115-182` (§ The settings screen);
  `docs/manual/sessions.md:52-54`; `ISSUES.md:27`, `:35`, `:52`, `:844`.

**Ratified design calls (owner, 2026-08-26, via AskUserQuestion):**
1. Consoles: a session restore CLOSES every Console of the outgoing conversation
   (`consoles.CloseAll()`, like `ClearContext`, ADR 0059 §1) — it does not adopt them.
2. F-15: `run.Result` gains a new `Faulted` field (plus the fault's reason) and `apogee headless`
   exits with a distinct, documented exit code; the daemon reports the Firing's outcome as
   `faulted`. `FinalText` is still delivered.
3. F-23: on every successful config re-read the transcript gets one notice naming the keys that
   landed; watcher-sourced journal entries are marked apart from in-pane edits; there is no
   confirmation gate — what applies is unchanged.
4. Confinement state: the `/settings` confinement and mode rows read the LIVE engine, and the
   footer gains a short confinement word beside the mode word.
5. C-16: the TUI adds a per-resume OFFSET to the engine's cumulative reading so a resumed record's
   `Meta.Usage` accumulates rather than being replaced, and `RestoreSession` resets the engine's
   usage tally.
6. (Author, no user-visible alternative) `git_status` carries its counts as a typed
   `domain.ToolSummary` variant rather than the slot re-parsing prose (the seam the audit named,
   and the one the other counted slots already use); `git_diff_range`'s diffstat is read off the
   same walked regions its body is painted from; a summary's failure verdict becomes a field the
   presenter sets, never a re-reading of composed text.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see closing note).
- Every item's Acceptance is targeted; `make check` runs once at closeout.
- The Bubble Tea `Model` is value-copied on every Update (ADR 0011): no `strings.Builder` or other
  no-copy type by value anywhere it reaches; new `Model` state is plain values or fresh slices.
- Mechanisms must never make any model perform worse than Bypass mode; nothing here touches a
  Mechanism.
- `ISSUES.md` is open-only: an item that closes an entry there REMOVES it and adds the closing line
  under `CHANGELOG.md` `[Unreleased]` (`### Fixed` for a defect, `### Added` for a new surface).

**Out of scope:**
- The `/new` boundary's accounting stance (`commandrun.go:159-165`: the fresh session inherits the
  closed one's spend) — recorded as a deferred defect there, not changed here.
- Provider-side dropping of malformed `tool_calls` entries at `toRawResponse` (it would change
  the agent loop's contract, which rejects a nameless call with an error the model reads,
  `internal/processing/toolcall.go:55-57`); the probe validates its own evidence (item 14).
- F-30 (newline in filenames), F-32 (headless bidi strip), F-22 (mode-row escalation wording),
  F-16 — the untrusted-text and approval-integrity plans (`docs/plans/2026-08-26 - 01/02`).
- The three "(grill)" sub-agent ideas at `ISSUES.md:28-31`; the tool×mode matrix; adopting
  Consoles across a restore (rejected by ratified call 1).
- Routing a faulted Firing to `schedule.EventFailed` (rejected: the Firing DID return; see item 4).

---

## 1. `RestoreSession` closes every Console and resets the usage tally — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `internal/agent/console_test.go` is one file beyond the item's Files list. `TestSnapshotCarriesNoConsoleState` asserted the pre-amendment lifetime ("a restore neither adds nor removes one", `assertOpenIDs` after the restore), so the item's own acceptance (`go test ./internal/agent/`) could not pass without it. Its "never serialized" purpose is unchanged: the snapshot-key assertion stands and the `Alive()` check moved to just after `Snapshot()`, which is what it was really proving; the post-restore assertion now expects no open Console.

**What:** the engine half of the session boundary. `internal/agent/agent.go` `RestoreSession`
(`:921-929`) becomes a boundary for the Consoles and the tally exactly as `ClearContext` (`:892`)
is for the Consoles:
- After `restoreSnapshot` succeeds and BEFORE `reloadContextFiles`, call `a.consoles.CloseAll()`
  and set `a.usage = usageTally{}`. Order matters: a REFUSED restore (mid-Exchange,
  `ErrSessionVersion`, decode error) must leave the Consoles running and the tally standing, so
  both resets sit after the swap, never before it. The doc comment's sentence "It does NOT touch
  the allow-for-session approval cache, the autonomy mode, or the confinement flag" stays; add
  one sentence naming the two things it DOES reset and why (the Console ids live in the history
  the swap drops — ADR 0059 §1; the tally belongs to the conversation that just left, and a Driver
  restoring a record seeds its own accounting from that record — item 2).
- `internal/tui/tui.go:587-592` (the `Engine.RestoreSession` doc comment): add "closes every
  Console of the outgoing conversation and resets the engine's cumulative usage reading".
- `docs/adr/0059-…md` §1 (`:22`): the lifetime sentence "lives until an explicit close, `/new`, or
  engine exit" becomes "…an explicit close, `/new`, a session restore (`/sessions`), or engine
  exit". `CONTEXT.md` **Console** (`:617-627`): the same list gains "a session restore".
  `docs/manual/sessions.md:52-54` (the "deliberately not part of a saved session" paragraph): add
  "and any Console the previous conversation left open is closed at the switch".
- `ISSUES.md:52-66` ("Resuming a stored session leaves the outgoing conversation's Consoles
  running"): remove the whole entry. `CHANGELOG.md` `[Unreleased]` `### Fixed`: one entry saying a
  `/sessions` restore now closes the outgoing conversation's Consoles (ADR 0059 §1 amended) and
  resets the engine's usage reading.

Binding standards: the reset lives in `RestoreSession` only — `restoreSnapshot` stays a pure
decode-and-swap so its refusal tests keep meaning what they say; no new goroutine; the registry's
`CloseAll` is already nil-safe, so a test Agent built with no registry needs nothing extra.

**Files:** `internal/agent/agent.go`, `internal/agent/restoresession_test.go`,
`internal/tui/tui.go`, `docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md`,
`CONTEXT.md`, `docs/manual/sessions.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** `restoresession_test.go` — `TestRestoreSession_ClosesEveryConsoleOfTheOutgoingSession`
(open two Consoles through the Agent's registry with the test helper `internal/agent/console_test.go`
already uses, restore a valid snapshot, assert `OpenIDs()` is empty);
`TestRestoreSession_ResetsTheUsageTally` (drive one scripted Turn so `record` has counted, restore,
then drive another and assert the next `UsageEvent`'s `CumulativeCalls == 1`);
`TestRestoreSession_RefusalLeavesConsolesAndTallyStanding` (mid-Exchange refusal and a
future-version snapshot: both Consoles still open, cumulative count unchanged).

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/tui/ && grep -c "session restore" docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md CONTEXT.md && ! grep -q "Resuming a stored session leaves" ISSUES.md`

**Commit:** `fix(agent): RestoreSession closes the outgoing conversation's Consoles and resets the usage tally`

---

## 2. A resumed session's accounting accumulates through a per-resume offset — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the item's Tests line asks the browser subtest of `TestSessionUsageTotalsSurviveTheRecord` to assert the next save carries the record's totals plus the folded event's. That replaces its previous final assertion (`saved == want`) rather than adding to it — the same save cannot be both — so the round-trip check now pins base+reading; the "reopened at exactly the stored totals" fact it used to carry is still asserted, unchanged, immediately after the resume in the same subtest and in the startup-resume subtest beside it.

Depends on item 1.

**What:** the TUI half of C-16. The engine's cumulative reading restarts at zero on every resume
(a fresh Agent on `--resume`; the reset of item 1 on a browser restore), so the view keeps a base
and folds the engine's reading ON TOP of it:
- `internal/tui/model.go`: `Model` gains `usageBase usageTotals` — the main-agent accounting the
  record carried when this session was (re)opened, zero on a fresh launch. Plain value, ADR 0011.
- `internal/tui/model.go:576` (`replayResumed`) and `internal/tui/sessions.go:575` (`resumeLoaded`):
  set `m.usageBase = usageTotals(r.Usage)` / `usageTotals(msg.rec.Meta.Usage)` and
  `m.usage = m.usageBase` (the two lines that assign `m.usage` today become these two).
- `internal/tui/fold.go:125`: `m.usage = usageSum(m.usageBase, totals)` in place of
  `m.usage = totals`; the comment says the reading is the engine's own sum since the session was
  opened and the base is what the record carried into it. `usageSum` (`usage.go:312`) is the one
  addition used; nothing else recomputes.
- `internal/tui/usage.go:12-24` (the package comment's "survives … a resumed session" claim): one
  clause naming the base. `sessionsave.go:51` is untouched — `m.usage` is already the sum the
  record should carry.
- `CHANGELOG.md` `[Unreleased]` `### Fixed`: a resumed session's `/usage` main row and the record's
  stored totals now accumulate across resumes instead of collapsing to the first post-resume
  reading.

Binding standards: the offset is the renderer's (it is the Driver that seeded the view from the
record — the engine never sees a record); `usageTotals` stays comparable so the tests keep using `!=`.

**Files:** `internal/tui/model.go`, `internal/tui/sessions.go`, `internal/tui/fold.go`,
`internal/tui/usage.go`, `internal/tui/usage_test.go`, `internal/tui/sessions_test.go`,
`CHANGELOG.md`

**Tests:** `usage_test.go` — `TestUsageAccumulatesOverAResumedReading`: a `newModel` with
`Resumed.Usage = {Calls:40, Total:500000, Prompt:480000, Completion:20000}`, fold a depth-0
`UsageEvent{CumulativeCalls:1, CumulativeTotalTokens:5300, …}`, assert `m.usage ==
{Calls:41, Total:505300, …}` and `snapshotPayload(...).usage` carries the same; a second event with
`CumulativeCalls:2` yields 42 (the base is added once, not per event); a fresh model (no resume)
folds to exactly the event's reading. `sessions_test.go` — extend
`TestSessionUsageTotalsSurviveTheRecord`'s browser subtest (`:2108`) with one folded `UsageEvent`
after the resume and assert the next save's `Usage` is the record's plus the event's, not the
event's alone.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Usage|SessionUsage'`

**Commit:** `fix(tui): fold the engine's usage reading onto the resumed record's totals instead of replacing them`

---

## 3. `run.Result` carries the abandoned-Turn fault; `apogee headless` exits 3 on it — ✅ DONE (2026-08-26)

**What:**
- `internal/agent/agent.go`: add `func (a *Agent) LastFault() string` — the text `emitLoopFault`
  (`loop.go:543`) recorded, "" when no loop-level fault has been surfaced; a boundary-only read
  like `InExchange` (`:934`), documented as such.
- `internal/run/run.go` `Result` (`:66-90`): add, beside `Denied`,
  `Faulted bool` — "the Firing's final Turn was ABANDONED (`domain.StepResult.Faulted`): the
  Exchange reached its boundary, so `Err` is nil and `FinalText` is whatever the run last said,
  but that text is not the answer to the prompt" — and `Fault string`, the reason (the Agent's
  `LastFault()`, which is the `ErrorEvent` text the sink already saw). The `Result` literal
  (`:235-243`) sets `Faulted: step.Faulted, Fault: a.LastFault()`. `Err` stays nil on a faulted
  run: a fault is not a loop error and `doc.go:47-50` already says so — extend that paragraph to
  name the two new fields.
- `cmd/apogee/headless.go:38-44`: a third code, `exitRunFaulted = 3` — "the run started and
  reached its boundary, but the engine abandoned its final Turn: whatever is on stdout is the
  run's last words, not its answer". `runHeadless` (`:376-418`): after the answer, the fill and
  usage lines and the summary are printed exactly as today, a faulted `res` returns
  `exitError{code: exitRunFaulted, err: fmt.Errorf("apogee headless: the run's final turn was
  abandoned — %s", res.Fault)}` (with the `(partial run saved as %s)` suffix when `SessionID` is
  set, the wording `:410-416` uses). `headlessSummary` (`:447`) appends ` · faulted` to the stats
  cell on a faulted result, so the one line a script greps says so. The `runErr != nil` branch
  stays ahead of it and unchanged (a run that errored is exit 1 whether or not it also faulted).
- `docs/manual/headless.md:50-56`: the exit table gains a `3` row — "the run started and reached
  its boundary, but its final turn was abandoned (a model or upstream fault the loop could not
  recover) — stdout holds the run's last text, not an answer; the record is saved" — and the
  paragraph before it says the summary line carries `faulted` in that case.
- `CHANGELOG.md` `[Unreleased]` `### Added`: `run.Result.Faulted`/`Fault` and headless exit 3.

Binding standards: the fault crosses the seam as DATA on `Result` (ADR 0010/0031 — no Driver reads
the event stream to learn it); the exit code is decided in `runHeadless` alone, next to the two it
already decides; `exitCodeFor` is untouched.

**Files:** `internal/agent/agent.go`, `internal/run/run.go`, `internal/run/doc.go`,
`internal/run/run_test.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`,
`docs/manual/headless.md`, `CHANGELOG.md`

**Tests:** `run_test.go` — `TestOnceReportsAnAbandonedFinalTurn`: a scripted Upstream whose reply
makes the loop abandon the Turn (the empty-reply fault `internal/agent/emptyreply_test.go:80`
pins, reused through the same scripted responder shape) yields `Faulted == true`, non-empty
`Fault`, `Err == nil`, and a saved record; a clean run yields `Faulted == false`, `Fault == ""`.
`headless_test.go` — a faulted `run.Result` (through the package's `runOnce` seam the existing
tests script) exits `exitRunFaulted`, prints the answer text on stdout, the summary on stderr
contains `faulted`, and the error names the partial record; a clean result still exits 0; an
errored result still exits `exitRunFailed` even with `Faulted` set.

**Acceptance:** `go build ./... && go test ./internal/run/ ./internal/agent/ -run 'Once|LastFault' && go test ./cmd/apogee/ -run 'Headless' && grep -q '^| `3` |' docs/manual/headless.md`

**Commit:** `feat(run): Result reports an abandoned final Turn; apogee headless exits 3 on it`

---

## 4. A faulted Firing reads `faulted` in the daemon log and the TUI's firing block — ✅ DONE (2026-08-26)

NOTES (2026-08-26): also updated `layout.md`'s firing-block paragraph (the stats-line and body-line
enumeration it pins) — the item's Files list omitted it, but it is the TUI rendering spec this
change alters.
NOTES (2026-08-26): the daemon's fault clause runs through `oneLine` and the TUI's through
`flattenField` — the fault is raw upstream text and both surfaces are one-row-per-fact, so a
wrapped fault cannot forge a second log line or an unauthored body row.

Depends on item 3.

**What:**
- `internal/schedule/schedule.go` `Outcome` (`:76-89`): add `Faulted bool` and `Fault string`,
  documented as pass-through like every other field (the library reads none of them — ADR 0033).
  No new `EventKind`: the Firing returned, so it is `EventCompleted` carrying a faulted Outcome.
- `cmd/apogee/daemonfire.go:237` and `cmd/apogee/schedule.go:149`: both `schedule.Outcome`
  literals copy `Faulted: res.Faulted, Fault: res.Fault` (these are the only two sites —
  `grep -n "schedule.Outcome{" cmd/apogee/` finds exactly them plus the zero-value returns).
- `cmd/apogee/daemon.go` `notify` (`:611-613`): the `EventCompleted` arm logs
  `faulted   %s in %s — %s` when `ev.Outcome.Faulted`, else the `completed` line as today;
  `daemonOutcome` (`:627`) prefixes the work clause with `final turn abandoned (<Fault>); ` on a
  faulted Outcome, so the line reads `faulted   nightly-audit in 7m41s — final turn abandoned
  (upstream returned an empty reply); 9 turns, 0 denied, saved as 2026…`. The verb column keeps
  its 9-character padding (`completed`/`faulted  `).
- `internal/tui/schedule.go` `firingStats` (`:556-561`): append a `faulted` cell after the denied
  cell when `ev.Outcome.Faulted`; the firing block's body gains one detail line
  `final turn abandoned — <Fault>` (escape-stripped through the same seam the prompt lines use).
- `docs/manual/daemon.md:96-103`: one sentence after the sample log — a Firing whose final Turn
  the engine abandoned logs `faulted` in place of `completed`, names the reason, and is saved like
  any other; the daemon still exits 0.
- `CHANGELOG.md` `[Unreleased]` `### Added`: the daemon's `faulted` line and the firing block's cell.

**Files:** `internal/schedule/schedule.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/schedule.go`,
`cmd/apogee/daemon.go`, `cmd/apogee/daemon_test.go`, `internal/tui/schedule.go`,
`internal/tui/schedule_test.go`, `docs/manual/daemon.md`, `CHANGELOG.md`

**Tests:** `daemon_test.go` — a pinned-wording case in `TestDaemonNotifyLinesArePinned` for an
`EventCompleted` with `Outcome{Faulted: true, Fault: "…"}` (the `faulted` verb, the reason, the
counts, the record id); `TestDaemonNotifyRendersEveryLibraryKind` passes unchanged; the
`daemonfire` mapping test asserts the two new fields cross. `schedule_test.go` (tui) — a firing
block on a faulted Outcome carries the `faulted` cell and the reason line; a clean one carries
neither.

**Acceptance:** `go build ./... && go test ./internal/schedule/ ./cmd/apogee/ -run 'Daemon|Firing|Schedule' && go test ./internal/tui/ -run 'Firing|Schedule' && grep -q 'faulted' docs/manual/daemon.md`

**Commit:** `feat(daemon): a Firing whose final Turn was abandoned logs faulted, in the daemon and the firing block`

---

## 5. Console ownership is keyed by an engine-minted owner key, not the model's call id — ✅ DONE (2026-08-26)

NOTES (2026-08-26): no ISSUES.md entry existed for F-43, so nothing was removed there.

**What:** F-43. The key that decides whose Consoles a delegation may reap — and, after item 6,
drive — must be minted by the engine: a model-supplied tool-call id can collide across siblings
(a text-format parser numbering per Turn), and a collision reaps another delegation's shells.
The call id stays what it is for EVENTS (`EventBase.CallID`, ADR 0039) — only the Console
ownership key moves.
- `internal/console/registry.go`: `Registry` gains `MintOwner() string` — under `r.mu`, a
  monotonic counter rendered `"run-<n>"`, never "" and never reused within a registry. The
  registry mints because it is the thing that compares the key (`CloseOwnedBy :211`), so the
  namespace has exactly one author. `Owner` doc comments (`:43`, `:86`) now say "the engine-minted
  owner key of the delegation that opened this Console (Registry.MintOwner), empty for the
  top-level agent" — the top level keeps "" (the `CloseOwnedBy` contract at `:213-215` stands).
- `internal/domain/ask.go`: beside `WithSpawnCallID` (`:179`), add `WithConsoleOwner(ctx, key)`
  / `ConsoleOwnerFromContext(ctx) string` with the same "" = top level convention. It is a
  separate key on purpose: `SpawnCallID` is display/attribution identity (`present_document.go:121`
  still reads it); the owner key is privilege identity, and the two must be able to differ.
- `internal/agent/agent.go`: `Agent` gains `consoleOwner string` beside `callID` (`:271`);
  `closeConsoles` (`:428`) reaps `CloseOwnedBy(a.consoleOwner)`. `internal/agent/subagent.go`
  `newChildAgent` (`:380`): `child.consoleOwner = a.consoles.MintOwner()` (nil-registry-safe: a
  nil `*Registry` mints "" — document it on the method — and a top-level-shaped key on a child is
  harmless because no registry exists to hold a Console). `internal/agent/dispatch.go:770`: stamp
  `ctx = domain.WithConsoleOwner(ctx, a.consoleOwner)` beside the spawn-call-id stamp.
- `internal/tools/console_open.go:152`: `Owner: domain.ConsoleOwnerFromContext(ctx)`.
- `subagent.go:416-421` comment ("a Console records the call id that opened it") is rewritten to
  name the minted key. `CHANGELOG.md` `[Unreleased]` `### Fixed`: ownership no longer rides the
  model-supplied call id.

Binding standards: the key is minted where it is checked (`internal/console`), threaded through
the domain context seam like its sibling, and read by the tool — no package below `domain` learns
about agents; no global counter (a registry is the engine's, and tests build many).

**Files:** `internal/console/registry.go`, `internal/console/registry_test.go`,
`internal/domain/ask.go`, `internal/agent/agent.go`, `internal/agent/subagent.go`,
`internal/agent/dispatch.go`, `internal/agent/console_test.go`, `internal/tools/console_open.go`,
`internal/tools/console_open_test.go`, `CHANGELOG.md`

**Tests:** `registry_test.go` — `MintOwner` returns distinct non-empty keys across 100 calls and ""
on a nil registry. `console_open_test.go:141` — the top-level `Owner` stays ""; a ctx carrying
`WithConsoleOwner(ctx, "run-7")` opens a Console with `Owner == "run-7"` while `WithSpawnCallID`
alone leaves it "". `console_test.go` (agent) — two sibling delegations whose `sub_agent` calls
carry the SAME call id each open a Console; the first child's end closes only its own (the other's
id is still in `OpenIDs()`); a depth-2 grandchild gets a key distinct from its parent's.

**Acceptance:** `go build ./... && go test ./internal/console/ ./internal/domain/ ./internal/tools/ -run 'Console|Owner|Mint' && go test ./internal/agent/ -run 'Console'`

**Commit:** `fix(agent): key Console ownership on an engine-minted owner key instead of the model's call id`

---

## 6. A Console is addressable only by the run that opened it — ✅ DONE (2026-08-26)

Depends on item 5.

**What:** F-37. `internal/tools/console_common.go` `lookupConsole` (`:69-79`): read
`owner := domain.ConsoleOwnerFromContext(ctx)`; a Console found by id whose `Owner != owner` is
refused EXACTLY as an unknown id is — same `no console %d (open consoles: %s)` wording — so a
delegation learns nothing about a sibling's shells, not even that the id exists. `openConsoleIDs`
(`:83`) lists only the caller's own: `Registry` gains `OpenIDsOwnedBy(owner string) []int`
(ascending, like `OpenIDs`), and the refusal is worded from it. `console_open`'s cap refusal
(`registry.go:146-148`, "close one of …") keeps naming ALL open ids: the cap is engine-wide (ADR
0059 §6) and a delegation that cannot open needs to know the engine is full, not that it owns
nothing. `console_send`, `console_read`, `console_close` all go through `lookupConsole`, so the
one change covers the three (verify with `grep -n "lookupConsole(" internal/tools/`).
- `docs/adr/0059-…md` §6 (`:72-77`): add one sentence — "A Console is addressable by the run that
  opened it and by no other: a sibling or a parent delegation naming its id is refused as if the
  id did not exist." This is the contract F-37 found unwritten; it lands with the code.
- `CHANGELOG.md` `[Unreleased]` `### Fixed`.

Binding standards: the check lives in the ONE lookup the three tools share; the registry gains a
query, not a policy — `Get` stays owner-blind so `CloseOwnedBy`/`CloseAll` are unaffected.

**Files:** `internal/tools/console_common.go`, `internal/console/registry.go`,
`internal/console/registry_test.go`, `internal/tools/console_send_test.go`,
`internal/tools/console_read_test.go`, `internal/tools/console_close_test.go`,
`docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md`, `CHANGELOG.md`

**Tests:** one table-driven test per tool file (send/read/close): a Console opened under owner
`"run-1"` is driven fine under a `"run-1"` ctx, refused under `"run-2"` and under the top-level ""
with the unknown-id wording whose open-list names only the caller's Consoles; the top-level
agent's own Console (Owner "") is refused to `"run-1"`. `registry_test.go` — `OpenIDsOwnedBy`
returns the owner's ids ascending and nothing for an owner with none.

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'Console' && go test ./internal/console/ && grep -q 'addressable by the run that opened it' docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md`

**Commit:** `fix(tools): a Console answers only to the run that opened it; lookupConsole checks Owner`

---

## 7. `/settings` mode and confinement rows read the live engine — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the plan names the new interface `liveSettings`; `cmd/apogee` already has a `liveSettings` struct (wire.go:171, the live session-state holder), so it is spelled `runningSettings` — same shape, same file, same two methods.

NOTES (2026-08-26): `internal/tui/seam_test.go` is edited beyond the plan's Files list — `fakeEngine` gained a `mode` field its `SetMode` sets plus a `liveMode()` reader (the counterpart of its existing `confine` field), which is what lets the TUI tests' fake host model the binary's live overlay.

NOTES (2026-08-26): two existing tests (`TestSettingsPaneModeEditAppliesLiveAndMarksNothing`, `TestSettingsPaneResetOfModeAppliesTheDefaultLive`) held a static mode row and asserted the journalled value in its cell; they now run on the live-overlay host helper and assert the same strings off the host's own rows.

**What:** F-14 + F-31. The rows are projected in the BINARY (`settingsRows`, `settingsrows.go:100`)
from the boot `config.Options`; the two keys whose live value the engine holds are overlaid there,
and the renderer's three reads of a row's "current" value stop preferring a stale journal entry
for `mode`.
- `cmd/apogee/wire_engine.go`: `lateEngine` gains `Mode() apogee.Mode` — the `mode` field
  (`:62`) under `mu`, the mirror `SetMode` (`:275`) already keeps — beside `ConfineToWorkspace()`
  (`:417`).
- `cmd/apogee/wire_options.go`: `settingsHost` (`:320-329`) gains `live liveSettings` where
  `type liveSettings interface{ Mode() apogee.Mode; ConfineToWorkspace() bool }` (declared in
  `settingsrows.go`); the literal at `:181-186` passes `w.engine`. `Rows()` (`:334`) becomes
  `overlayLiveSettings(settingsRows(h.opts), h.live)` — a pure function in `settingsrows.go` that
  replaces `Value` on the `mode` row with `string(live.Mode())` and on the
  `confine-to-workspace` row with `strconv.FormatBool(live.ConfineToWorkspace())`; every other
  row is untouched, and a nil `live` overlays nothing (a test host without an engine).
- `internal/tui/settings.go`: `settingsCurrentValue` (`:1086`) gains the `mode` carve-out beside
  `server`'s — `if row.Path == settingKeyMode { return row.Value }` — so the enum sub-list opens on
  and marks "(current)" the rung the engine is RUNNING, and ⏎ on that row re-applies the live
  rung (a no-op) rather than a stale one. `settingsValueCell` (`:1386`): for `settingKeyMode`
  the cell shows `row.Value` (live) — with `settingsEditMarker` appended when the journal holds an
  edit for it — instead of the journaled value; `settingsPersistedValue` (`:971`) is unchanged for
  every key (a `mode` toggle still reads the journal for the reset path). The `confine-to-workspace`
  row is not `Editable` (its `EditPointer` is `/confine`), so no journal ever touches it and the
  overlaid `Value` is the whole answer.
- The `settingsEditMarker` doc comment (`:279-286`) gains: "on the `mode` row the value beside it
  is always the LIVE rung — Shift+Tab moves it without a journal entry — and the marker only says
  the session wrote the key once".
- `docs/manual/commands.md` § The settings screen (`:115-182`): one sentence — the `mode` and
  `confine-to-workspace` rows show what the session is running right now, so a Shift+Tab or a
  `/confine off` shows up on the next open.
- `ISSUES.md:27` ("display somewhere id apogee is confined or not"): NOT removed here — item 8
  closes it. `CHANGELOG.md` `[Unreleased]` `### Fixed`: the two rows report the live engine.

Binding standards: the renderer keeps holding no schema and no engine mutator (ADR 0037 decision
2) — the overlay is the binary's, as `Rows()` already is; the renderer's carve-out reads `row.Value`,
never `m.eng`; no new `Model` state.

**Files:** `cmd/apogee/wire_engine.go`, `cmd/apogee/wire_options.go`, `cmd/apogee/settingsrows.go`,
`cmd/apogee/settingsrows_test.go`, `internal/tui/settings.go`, `internal/tui/settings_test.go`,
`docs/manual/commands.md`, `CHANGELOG.md`

**Tests:** `settingsrows_test.go` — `TestSettingsRowsOverlayTheLiveModeAndConfinement`: boot opts
say `mode: plan`, `confine-to-workspace: true`; a fake `liveSettings` answering `auto`/`false`
yields rows reading `auto` and `false`, every other row byte-identical to the un-overlaid set; a
nil live changes nothing. `settings_test.go` — after a Shift+Tab (`keyShiftTab` through `Update`),
the mode row's value cell and `settingsCurrentValue` read the new rung with no journal entry and
no marker; after an in-pane `mode` write followed by a Shift+Tab, the cell reads the Shift+Tab
rung WITH the marker; ⏎ on the "(current)" row applies the live rung (the fake `SettingsHost`
records the value it was asked to apply).

**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'SettingsRows' && go test ./internal/tui/ -run 'Settings'`

**Commit:** `fix(settings): the mode and confine-to-workspace rows read the live engine, not the boot snapshot`

---

## 8. The footer says whether Auto is confined — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the item's binding standard asks that `TestFooterModeMarkerLeadsWithTheModeSymbol` pass unchanged; it cannot — its Auto case builds a fake engine whose blast radius is off, so that rung's marker now legitimately ends `⏵⏵ auto · unconfined` and the flat-suffix assertion had to grow a per-case `tail`. The two assertions the standard is about are untouched and still pass as written: the symbol leads (`modeMarker`), and the symbol+word are ONE styled run in the mode's colour.

NOTES (2026-08-26): `layout.md` is edited though it is not in the item's Files list — it is the TUI layout/rendering spec and enumerates the mode marker's content paragraph by paragraph, so a footer element missing from it would make the spec lie. One new paragraph, beside the marker's symbol paragraph.

NOTES (2026-08-26): the `ISSUES.md` line removed reads "display somewhere **if** apogee is confined or not" — the acceptance command's `grep` spells it "id"; the line is gone either way.

**What:** the "display confined" idea (`ISSUES.md:27`) plus ratified call 4's footer half. The mode
marker at the footer's right edge (`model.go:2644-2645`, `modeMarker :2797`) gains a confinement
word, shown ONLY in Auto — the one rung where the box exists (ADR 0012: confinement attaches to
Auto's blast radius; every other rung gates subprocess calls through Approval):
- `internal/tui/model.go`: a pure `confinementWord(info ConfinementInfo, mode domain.Mode,
  confine bool) string` beside `modeMarker`: `""` unless `mode == domain.ModeAuto`; then
  `"confined"` when `confine && info.Caps.FSWrite`; `"unconfined"` when `!confine` (the user's
  "I am the sandbox"); `"gated"` when `confine && !info.Caps.FSWrite` (the backend cannot fence,
  so every subprocess call falls back to Approval — the vocabulary `probe.DegradedNotice`
  already uses: "auto mode is gating terminal commands"). `footerContent` renders the marker as
  `modeMarker(mode) + " · " + word` when the word is non-empty; `confined`/`gated` take the mode's
  own colour through the same single `Render`, `unconfined` takes `th.errorFg` on the footer's
  black field (the `offline` treatment at `:2651-2653`) — it is the one state where Auto runs with
  the user's full privileges. The whole marker still drops as a unit when the window is too
  narrow (`:2647-2652`). The word reads `m.eng.ConfineToWorkspace()` (live, goroutine-safe — the
  same read `runConfine :33` makes) and `m.opts.Confinement` (`tui.go:814`); no new `Model` state.
- `internal/tui/tui.go:1336-1345` (`ConfinementInfo` doc): add "…and the footer's confinement word".
- `docs/manual/configuration.md` § Auto mode's blast radius (`:613`): one paragraph — in Auto the
  footer's mode marker carries `confined`, `unconfined` or `gated`, what each means, and that
  `/confine` is where it changes.
- `ISSUES.md:27`: remove the line. `CHANGELOG.md` `[Unreleased]` `### Added`.

Binding standards: pure, table-tested word function; the footer stays ONE line (layout.md);
`TestFooterModeMarkerLeadsWithTheModeSymbol` (`mode_test.go:86`) must pass unchanged — the symbol
still leads.

**Files:** `internal/tui/model.go`, `internal/tui/tui.go`, `internal/tui/mode_test.go`,
`internal/tui/model_test.go`, `docs/manual/configuration.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** `mode_test.go` — `TestConfinementWordFollowsModeFlagAndBackend`: the six cells
(Auto × {flag on + FSWrite, flag on + no FSWrite, flag off}, and Allow-Edits/Ask/Plan with any
flag ⇒ ""). `model_test.go` — `footerContent(120)` with a fake engine answering
`ConfineToWorkspace() == true` and `Confinement.Caps.FSWrite == true` in Auto ends in
`auto · confined`; after `/confine off` the same footer ends in `auto · unconfined`; in Plan no
word appears; the narrow-window branch drops the whole marker (existing assertion shape at
`model_test.go:3403`).

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Footer|ConfinementWord|Confine' && ! grep -q "display somewhere id apogee is confined" ISSUES.md`

**Commit:** `feat(tui): the footer's mode marker says confined, unconfined or gated in Auto`

---

## 9. A config re-read names the keys it applied; watcher-sourced edits are marked; the manual names all three excluded keys — ✅ DONE (2026-08-26)

NOTES (2026-08-26): three amendments beyond the item's literal line references, each to keep a surface from lying about the change. (a) `docs/manual/configuration.md`'s earlier sentence in the same paragraph — "every key that came back different is applied exactly as an in-pane edit is, and its row repaints wearing the same ` *`" — now names the ` ~`; leaving it would have contradicted the marker this item introduces two sentences later. (b) `settingsValueCell`'s own doc comment (`settings.go`) had the same stale ` *`-only claim and was amended to name the pair. (c) The note's path list is built by a small `appliedPaths` helper beside `foldConfigChanged` rather than inline, so the "composed from the `applied` slice, never from the pane's rows" rule is visible at the seam. Not changed: ADR 0041's line 155 still says a watcher apply journals a ` *` marker; the plan's ratified call 3 supersedes it and the item's Files list names no ADR, so the record is left as written.

**What:** F-23 + R-3.
- `internal/tui/settings.go` `settingEdit` (`:167`): add `watched bool` — the edit landed from a
  file save this program had nothing to do with (the watcher), as opposed to an in-pane commit or
  the pane's own editor round trip. `settingsValueCell` (`:1386-1391`): a watched edit carries
  `settingsWatchMarker = " ~"` instead of `settingsEditMarker`; the marker constants sit together
  and the `settingsEditMarker` comment (`:279-286`) is amended — ` *` means "this session wrote
  it through this surface", ` ~` means "a save on disk moved it under this session". The journal
  keeps one entry per key (`recordSettingEdit :377`), so the LAST source wins the marker.
- `internal/tui/settingswatcher.go` `applyReloaded` (`:205`): gains a `watched bool` parameter it
  stamps onto every `settingEdit` it records; `foldSettingsEdit` (`:175`) passes `false`,
  `foldConfigChanged` (`:280`) passes `true`. `foldConfigChanged`, after the applies and only when
  `len(applied) > 0`, adds ONE transcript note:
  `configWatchAppliedNote = "config changed on disk — applied: "` + the applied paths joined by
  `", "` (registry order, i.e. the order `ReloadConfig` returned them). A re-read that found
  nothing changed (apogee's own write coming back, ADR 0041 decision 8) says nothing. A key that
  landed but whose apply REFUSED still appears in the list — the row carries the refusal
  (`settingsApplyFailedNote`) and the note says what the file moved. Nothing about what applies
  changes (ratified call 3).
- `docs/manual/commands.md:145-159`: beside the ` *` sentence, one sentence for ` ~` and the
  transcript line a saved file produces.
- `docs/manual/configuration.md:40-41`: replace the sentence "`server:` is the one key a re-read
  never moves: it names where the *next* session starts (see [The servers you run models
  on](#the-servers-you-run-models-on))." with, verbatim: "`server:` is the one ordinary key a
  re-read never moves: it names where the *next* session starts (see [The servers you run models
  on](#the-servers-you-run-models-on)). The confinement pair — `confine-to-workspace:` and
  `unconfined-hosts:` — is left alone by a re-read as well; that interlock stays single-homed in
  `/confine` (ADR 0012)." Then one more sentence: a re-read that applied anything says so in the
  transcript, naming the keys.
- `CHANGELOG.md` `[Unreleased]` `### Added`: the notice and the ` ~` marker; the manual correction
  rides the same entry.

Binding standards: one apply loop, two triggers (ADR 0041 decision 6) — the flag is a parameter
of that loop, not a second loop; the note is composed from the `applied` slice the seam returned,
never from the pane's rows; the journal slice is rebuilt fresh as `recordSettingEdit` already does
(ADR 0011).

**Files:** `internal/tui/settings.go`, `internal/tui/settingswatcher.go`,
`internal/tui/settings_test.go`, `docs/manual/commands.md`, `docs/manual/configuration.md`,
`CHANGELOG.md`

**Tests:** `settings_test.go` — extend `TestConfigWatchAppliesASavedFileWithNoKeyPress` (`:2767`):
a `ReloadConfig` returning two applied keys leaves one transcript note reading
`config changed on disk — applied: ui.spinner, auto-title` and both rows carry ` ~`; a re-read
returning nothing adds no note; an in-pane write after a watched edit of the same key flips the
marker back to ` *`; `TestConfigWatchNotesAFileThatKeepsFailingToParseExactlyOnce` unchanged.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'ConfigWatch|Settings' && grep -q 'one ordinary key a re-read never moves' docs/manual/configuration.md && ! grep -q 'is the one key a re-read never moves' docs/manual/configuration.md`

**Commit:** `feat(settings): a config re-read names the keys it applied and marks watcher-sourced edits`

---

## 10. `git_status` reports its counts as a typed summary; the slot stops parsing prose

**What:** F-20 (ratified call 6).
- `internal/domain/toolsummary.go`: an eighth variant, `ChangedFiles{Staged, Unstaged, Untracked
  int}` — "git_status' outcome: how many paths each section holds, the FULL counts even where the
  printed list was capped; all zero on a clean tree" — with its `isToolSummary()` marker.
  `toolsummary_test.go:37`: `want = 8` and the variant list gains it. `apogee.go:344`: the alias
  `type ChangedFiles = domain.ChangedFiles` beside `DiffStat` (the root re-exports every variant —
  the doc block at `toolsummary.go:20-24` says so).
- `internal/tools/git.go` `git_status` `Execute` (`:797-840` region; the call that builds the
  `okResult` from `renderGitStatus`): attach `Summary: domain.ChangedFiles{len(rep.staged),
  len(rep.unstaged), len(rep.untracked)}` — the same lengths `writeStatusSection` (`:969`) prints
  in its headers. The prose is unchanged.
- `internal/tui/toolregistry.go` `changedFilesStat` (`:756-773`): read `res.Summary.(domain.
  ChangedFiles)`; sum the three; `countedStat(total, "changed")`. No summary ⇒ `ok == false`
  (the prose floor, as every other typed slot degrades). Delete `gitStatusSection` (`:751`) and the
  `Contains("Working tree clean")` test; the doc comment names the variant.
- `CHANGELOG.md` `[Unreleased]` `### Fixed`.

Binding standards: the count crosses as data (the `ToolSummary` doc's whole argument: "Text
written for a model is not an interface"); no wire form (session replays store the rendered view,
`toolsummary.go:31-38`); the variant is comparable so `hookrun`'s `toolResultChanged` keeps using
`==`.

**Files:** `internal/domain/toolsummary.go`, `internal/domain/toolsummary_test.go`, `apogee.go`,
`internal/tools/git.go`, `internal/tools/git_test.go`, `internal/tui/toolregistry.go`,
`internal/tui/toolpresent_test.go`, `CHANGELOG.md`

**Tests:** `git_test.go` — `TestGitStatus_CleanTree` (`:711`) and
`TestGitStatus_ReportsStagedUnstagedAndUntracked` (`:726`) assert the `Summary` (`{0,0,0}` and the
three counts); `TestGitStatus_CapsEachList` (`:690`) asserts the summary carries the FULL count
past the cap. `toolpresent_test.go` — `changedFilesStat` on a result whose prose lists a path
named `Working tree clean.md` under `Untracked (1):` with `Summary{0,0,1}` words `1 changed`; a
result with no summary keeps the prose floor (`ok == false`); `TestToolSummaryVariantsAreSealed`
passes at 8.

**Acceptance:** `go build ./... && go test ./internal/domain/ ./internal/tools/ -run 'ToolSummary|GitStatus' && go test ./internal/tui/ -run 'ChangedFiles|GitStatus'`

**Commit:** `fix(tools): git_status carries its counts as a typed ChangedFiles summary; the slot stops parsing prose`

---

## 11. `git_diff_range`'s diffstat is counted off the walked regions

**What:** F-24. `internal/tui/toolregistry.go` `diffLinesStat` (`:832-847`) counts from the same
walk the body is painted from: call `gitDiffFileSections(res.Content)` (`diffbody.go:419`); when it
returns sections, `added`/`removed` are the sums of `len(region.Inserted)` / `len(region.Removed)`
over every region of every section (`diffbody.go:135-137`: the regions hold changed lines only, so
they add up to the diffstat). When the walk refuses (nil — a binary section, a rename header, a
`--stat` call, `diffbody.go:427-433`), fall back to a header-AWARE line count: a `---`/`+++` line
is a file header only while OUTSIDE a hunk — track `inHunk` exactly as `gitDiffWalk` does (a
`diff --git` line clears it, an `@@` line sets it) — so a removed line whose content starts with
`--` inside a hunk counts as removed. Both paths keep the existing "no tagged lines ⇒ prose floor"
answer. The doc comment says which count is which and why the fallback exists.
`CHANGELOG.md` `[Unreleased]` `### Fixed`.

Binding standards: one reader of git's grammar for positions AND counts (the walk); the fallback
is the old loop with the one missing state bit, not a second parser.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolpresent_test.go`, `CHANGELOG.md`

**Tests:** `toolpresent_test.go` — a two-file unified diff where one removed line reads `--flag`
and one added line reads `++i` inside hunks words `+3 −2` (today: `+2 −1`); a `--stat`-shaped body
keeps the prose floor; a diff whose second file is `Binary files … differ` (walk refuses) still
counts the first file's `--`-prefixed removed line through the fallback; the region and the
fallback answers agree on `TestGitDiffRangeRecoversARegionPerFileSection`'s (`:2656`) fixture.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'DiffLines|GitDiffRange'`

**Commit:** `fix(tui): git_diff_range's diffstat counts the walked regions, so --/++ content lines are not skipped`

---

## 12. The failure verdict is a field the presenter sets, never a re-reading of composed text

**What:** F-28 + F-29 (ratified call 6). `internal/tui/toolview.go` `branchSummary` (`:89`) gains
`failed bool` — "the block's own verdict that this call failed; set only by a summary the block
WORDED (a named summary in the failure vocabulary, a run aggregate counting errors, a delegation
whose report was a failure), never by a quoted line".
- Producers: `namedSummary` (`:125`) sets `failed = failedSummary(line.Text)` (the existing text
  reader becomes the ONE place named wording is judged); `runAggregate` (`:882-883`) sets it on
  the `plural(n, errorNoun)` summary; `quotedSummary` (`:131`) and `typedSummary` (`:144`) never
  set it. `subAgentSummary` (`subagentblock.go:496-517`) — the composed `"N tool calls · fill ·
  gist"` line, quoted — sets `failed = failedSummary(head.tool.Summary.Text)`: the verdict the
  head's OWN summary carries (`subAgentFinished :326` reads the same fact), carried onto the
  composed reading rather than re-derived from it. This is F-28: a failed delegation's row now
  paints red collapsed and expanded, lone or grouped.
- Consumers: `summaryStyle` (`toolleader.go:287`) reads `s.failed` instead of
  `failedSummary(s.Text)` — F-29: a tool's own one-line output promoted into the slot
  (`promotedOutput :604`, quoted) can no longer wear the failure tone by spelling `error: …`.
  `failedCalls` (`toolview.go:898`) reads `tv.Summary.failed`; `subAgentFinished`
  (`subagentblock.go:326`) reads `head.tool.Summary.failed`. `failedSummary` itself stays, called
  from `namedSummary` and `subAgentSummary` only (verify with `grep -n "failedSummary(" internal/tui/`).
- The `toolleader.go:296-306` comment ("It asks the TEXT because that is where the fact is") is
  rewritten: the text is read ONCE at the seam that words it, and the field is what every reader
  asks. `CHANGELOG.md` `[Unreleased]` `### Fixed` (two lines: the red delegation, the quoted line).

Binding standards: the verdict is decided at the presenter's seam and carried as data
(`branchSummary` is already the type that says whose text the slot holds); no consumer re-reads
text; the session codec stores rendered views, so no wire change (`transcriptcodec.go` untouched —
confirm the replayed view re-derives its summary through `namedSummary`/`quotedSummary`; if it
builds a `branchSummary` literal directly, that literal goes through `namedSummary`).

**Files:** `internal/tui/toolview.go`, `internal/tui/toolleader.go`, `internal/tui/subagentblock.go`,
`internal/tui/toolbranch_test.go`, `internal/tui/subagentblock_test.go`, `CHANGELOG.md`

**Tests:** `subagentblock_test.go` — `TestFailedDelegationPaintsItsSlotRed`: a head whose result is
`error: sub-agent failed: …` renders its collapsed row's slot in `th.errorText` both as a lone run
and as a grouped member, wears no ✓, and its expanded row is red too; a delegation whose report
merely CONTAINS "error:" past the first cell is not red. `toolbranch_test.go` — a terminal call
whose one-line output is `error: not really` promoted into the slot renders in the ordinary slot
colour and counts 0 in `failedCalls`; a `summaryOnly("error: boom")` renders red and counts 1;
`runAggregate` of three views with two failures words `2 errors` with `failed == true`.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Failed|SubAgent|Aggregate|Summary'`

**Commit:** `fix(tui): the failure verdict rides branchSummary — failed delegations paint red, quoted output never does`

---

## 13. A delegation that never ran shows the prompt it carried when expanded

**What:** `ISSUES.md:844-862`. A delegation that is over with nothing behind it (refused at the
depth bound, failed by a hook, a construct error — `runSubAgent :100-116`) is an ordinary tool
block (`subAgentFramed :57` is right not to frame it). The prompt then lives in the BODY:
- `internal/tui/subagentblock.go`: `unframedSubAgentView(head paintInput) toolView` returns
  `head.tool` with `Details` = one lead line `task: ` + the task's first line, the remaining task
  lines as plain detail lines (clipped per line as `outputBody` clips), a blank line, then the
  existing `Details` (the refusal the result carried). It is applied ONLY when
  `head.headsRun() && head.expanded && !subAgentFramed(head, span)` — a collapsed never-ran
  delegation keeps its one row (the task's first line already rides the header as the name
  fallback), and a framed one keeps painting the prompt inside the frame (`:190`, `:433`).
- Both rendering paths call it: `internal/tui/render.go:541-546` (the lone `entryToolCall` block:
  substitute the view when the predicate holds — `subAgentSpan` is already computed at `:358`) and
  `renderSubAgentMemberRows` (`subagentblock.go:409`) in its `!spanned` branch before
  `renderGroupMember`. One rule, two callers — the ISSUES entry's own requirement.
- `ISSUES.md:844-862`: remove the entry. `CHANGELOG.md` `[Unreleased]` `### Fixed`.

Binding standards: paint-time only (no entry field changes, no codec change); the predicate is
asked through `subAgentFramed`, never restated.

**Files:** `internal/tui/subagentblock.go`, `internal/tui/render.go`,
`internal/tui/subagentblock_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** `subagentblock_test.go` — a depth-bound-refused delegation (result `error: sub-agent
depth limit reached…`, no span) expanded as a lone block shows `task: <first line>` above the
refusal; the same head as a grouped member shows the same rows; collapsed it shows one row; a
delegation WITH a span is unchanged (the existing sketch tests at `:58` stay byte-identical).

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'SubAgent|Unframed' && ! grep -q 'A delegation that never ran shows no prompt' ISSUES.md`

**Commit:** `fix(tui): an expanded delegation that never ran shows the prompt it carried`

---

## 14. The amend guard asks git which remote branches contain HEAD

**What:** C-05. `internal/tools/git.go:551-560`: replace the `log -1 --format=%D` read and
`commitIsPublished` (`:617-627`) with `runGit(ctx, gitPath, t.root, gitTimeout, "branch", "-r",
"--contains", "HEAD")`; HEAD is published when the command exits 0 and its output has at least
one non-blank line (a pure `remoteBranchesListed(output string) bool` keeps the parse table-
testable). This answers the two shapes the audit reproduced: a local branch BEHIND its remote
(HEAD is contained in `origin/main` though `%D` decorates only the tip) and a remote not named
`origin`. A non-zero exit (no remotes, a detached state git cannot answer) reads as unpublished —
the guard's existing degrade, kept explicit in the comment. The tool spec's "blocked on published
commits" sentence (`:487`) is now true; the error wording at `:559` is unchanged.
`CHANGELOG.md` `[Unreleased]` `### Fixed`.

**Files:** `internal/tools/git.go`, `internal/tools/git_test.go`, `CHANGELOG.md`

**Tests:** `git_test.go` (using `gitRepo(t)` at `:30`, a `git init --bare` remote added under the
name `upstream`, and pushes through `exec.Command` like the helper's own setup) —
`TestGitCommit_AmendRefusedWhenHEADIsBehindItsRemote`: push two commits, `reset --hard HEAD~1`,
amend ⇒ error result "cannot amend a commit that has been pushed"; `TestGitCommit_AmendRefusedOnANonOriginRemote`:
one commit pushed to `upstream/main`, amend ⇒ refused; `TestGitCommit_AmendAllowedOnAnUnpushedCommit`:
a commit past the pushed tip amends fine; `TestGitCommit_AmendAllowedWithNoRemote`: a repo with no
remote amends fine; a table test for `remoteBranchesListed` ("", "\n", "  upstream/main\n").

**Acceptance:** `go build ./... && go test ./internal/tools/ -run 'GitCommit'`

**Commit:** `fix(tools): git_commit's amend guard asks git branch -r --contains HEAD instead of parsing decoration`

---

## 15. `probe model` counts only a well-formed native tool call as evidence

**What:** C-18. `internal/probe/battery.go`: a package helper
`wellFormedToolCalls(calls []provider.ToolCall) []provider.ToolCall` keeps an entry only when
`Function.Name != ""` AND `ID != ""` (the shape the follow-up request needs: `probeMultiStepChain`
echoes `first.ToolCalls` back as the assistant turn and keys the tool message on `call.ID`,
`:235-238` — an id-less call would send a `tool_call_id` `omitempty` drops).
`probeNativeToolCall` (`:169`) and both gates in `probeMultiStepChain` (`:230`, `:247`) test the
FILTERED list; the chain's assistant echo and `call`/`next` read from it too. A reply that carried
entries but none well-formed gets its own detail:
`"the reply carried %d tool_calls entries, none with a name and an id — "` + `firstWords`, so the
report says what the server did rather than "no tool_calls entry". `Observed` stays false in that
case, so the tier, the suggested `tool-call-format` and the fingerprint follow. `provider` is
untouched (out of scope above). `CHANGELOG.md` `[Unreleased]` `### Fixed`.

**Files:** `internal/probe/battery.go`, `internal/probe/battery_test.go`, `CHANGELOG.md`

**Tests:** `battery_test.go` — the `script` gains `malformedTools string` spliced by `toolReply`
and `chainReply` in place of the well-formed entry; three rows — `[{}]`, `[null]`, and
`[{"function":{"name":"probe_echo","arguments":"{}"}}]` (no id) — each yield
`Observed(CapNativeToolCall) == false`, `Observed(CapMultiStepChain) == false`, the new detail
wording, `Complete()` unchanged in meaning, `Tier != TierStructured`, and no request whose
assistant turn carries an empty-name call (assert on the recorded request bodies); the existing
`TestBatteryAllPass` fixture (`id` + `name` present) still observes both.

**Acceptance:** `go build ./... && go test ./internal/probe/ -run 'Battery'`

**Commit:** `fix(probe): a tool_calls entry without a name and an id is not native tool-call evidence`

---

## 16. A Console status word ends a LINE, not a word

**What:** `ISSUES.md:35-48`. `internal/tui/toolregistry.go:617`: `consoleStatusMarker` becomes
`(?:^|\n)(alive|exited with code (-?\d+)|killed)\s*$` — the leading `\n?` was optional, so a
program whose last output line ENDED in "alive" had that word read as the verdict and
`exitMarkerPhrase` (`:629`) cut it off the body. The two capture groups keep their positions
(`exitMarkerPhrase`'s shape). The doc comment (`:613-616`) already claims line anchoring; it now
holds. `exitCodeMarker` (`:611`) is untouched — its brackets keep it off prose. Remove the
ISSUES entry; `CHANGELOG.md` `[Unreleased]` `### Fixed`.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolpresent_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** `toolpresent_test.go` — a table over `consoleDetail`/`consoleStatusStat`: body
`"the dev server is alive\nalive"` words `alive` and keeps the whole first line; body
`"the dev server is alive"` (no status line) has NO status phrase and an intact body; body
`"exited with code 3"` alone (the status IS the first line) still matches through the `^`
alternative; `"…\nkilled  "` matches with trailing whitespace.

**Acceptance:** `go build ./... && go test ./internal/tui/ -run 'Console' && ! grep -q 'A Console status word inside' ISSUES.md`

**Commit:** `fix(tui): consoleStatusMarker anchors the status word to a line start`

---

**Suggested version bump (not performed):** minor — `0.18.0`. Items 3 and 4 add public fields on
`run.Result` and `schedule.Outcome` and a documented headless exit code; item 10 adds a
`ToolSummary` variant (additive, minor per ADR 0001's amended consequences); items 5 and 6 change
the Console family's ownership contract (ADR 0059 §6 amended); items 8 and 9 add user-visible
surfaces. The bump is the owner's call, after the run.
