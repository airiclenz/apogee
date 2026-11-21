# Start-up notes — one class table, one router

**Goal:** every start-up and heartbeat note has a `NoticeKind` whose class (loud/routine) and transcript wording are declared once on the TUI Driver side, and one `startupChrome` module decides each note's destination (session log, own line persisted, own line ephemeral). Behaviour-neutral: every note lands where it lands at the base.
**Date:** 2026-11-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** ec353573

**Regression check (2026-11-21, ec353573):**
- 1: guard folded (writer decision; zero kind pinned through the table lookup)
- 2: guard folded
- 4: recast (writer decision: the chrome is a `sessionView` field; boundary sites in commandrun.go and sessions.go)
- 5: guard folded
- 6: guard folded
- 7: guard folded
- 5: guard folded (writer decision: the leftover-field check greps `noteRebindRoutine|firstBeat|quietSeed:|intent\.quietSeed`, not the bare `quietSeed` `rebindNote`'s parameter keeps)
- 1 (re-check): guard folded (writer decision: the zero kind's row is classLoud, sourceBoundary, declared once in item 1)
- 4 (re-check): guard folded (writer decision: the chrome's zero value is closed; launch-side callers pass contactEvent with non-beat routine kinds in the seeded tests; the enterRunning subtest stays model-driven; kindColorScheme/kindGitRefusal rows pinned; launch-side routing check added to Acceptance)

**Sources:**
- `.capcom/issues/startup-notice-class-scattered.md`, `.capcom/issues/startup-note-routing-scattered.md`
- `docs/reviews/architecture-review-2026-11-21.html` (#2, #8)
- `capcom know show "ADR 0112" --with-children` (amend-1112 §2), `capcom know show "ADR 0024" --with-children` (amend-1111), ADR 0011
- `internal/notice/doc.go`; `layout.md` §"The start-up box"; `docs/manual/commands.md` §"The start-up session log"; glossary Note `session-log`

**Ratified design calls:**
- **Scope:** triage cluster 1 only (Airic, 2026-11-21).
- **Persistence:** destinations stay exactly as at the base; first-contact beat notes on their own line stay persisted. The resume pile-up is filed as `first-contact-notes-repeat`, not fixed here (Airic, 2026-11-21).
- **Zero kind:** a `Notice` whose `Kind` is the zero value is loud (Airic, 2026-11-21).

**Standing requirements:**
- skills: coding-standards
- stderr, `apogee headless` and `apogee daemon` output stay byte-identical (ADR 0112 amend-1112 §2).
- The class and wording table lives in `internal/tui`, never in `internal/notice` or `internal/domain`; `notice.ContextNotice.Anomaly` stays (daemon and schedule read it).
- `applyRebind` alone sets `lastRebindFailed`; a refusal is noted once per target (ADR 0024 amend-1111).
- Every new `internal/tui` file is named in `internal/tui/doc.go` in the same item (`TestDocMapNamesEveryFile`).
- Narrow test runs only: one package, `-run` filter. Never a multi-package `go test`.

**Out of scope:**
- Making first-contact notes ephemeral or deduplicated (`first-contact-notes-repeat`).
- Any change to `internal/notice`, `internal/probe` wording, headless or daemon routing.
- The other triage clusters.

## 1. Notice kinds for the TUI's start-up notices — ✅ DONE (2026-11-21)
NOTES (2026-11-21): context-file kinds are sourceAmbient (re-derived from the workspace at every session boundary), start-up notices sourceBoundary, rebind notices sourceBeat; the zero kind is spelled kindUnset, the lookup is kindRow/classOf, and an undeclared kind falls back to the zero kind's row (loud).
NOTES (2026-11-21): test Notice literals that set Loud: true now set a loud Kind instead of Loud (the field is no longer set in any internal/tui test); TestNoticeKindClasses also fails when the table gains a kind the test does not pin.

**What:**
**Goal:** `internal/tui` declares `type NoticeKind` with one table `noticeKinds` mapping each kind to a class (`classRoutine`/`classLoud`) and a source (`sourceBoundary`, `sourceAmbient`, `sourceBeat`); `tui.Notice` carries a `Kind`; context-file notes and start-up notices take their loudness from the table; a unit test pins each declared kind's class.
**Approach (assumed at the header base):** new file `internal/tui/noticekind.go`. Exported kinds for what cmd/apogee produces: `NoticeAutoUnconfined`, `NoticeAutoDegraded`, `NoticeFenceResidual`, `NoticeShareGroupConfig`, `NoticeMCPTruncated`, `NoticeMCPSkipped`, `NoticeModelBinding`, `NoticeTrustedHint`, `NoticeRosterUnapplied`; unexported `kindContextLoaded`, `kindContextAnomaly`. Zero value `NoticeKind(0)` is reserved and classed loud. Add `Kind NoticeKind` to `Notice` (tui.go) beside the existing `Loud` field; a transitional `func (n Notice) loud() bool` returns the table class when `Kind != 0`, else `n.Loud` (the field is deleted in item 2). `noteStartupNotices`, `noteContextFiles` (maps `Anomaly` → `kindContextAnomaly`) and the `applyRebind` notice loop read loudness through the table. TUI tests building `Notice{…, Loud: true}` switch to a loud Kind.
**Regression guard.** Every `Notice{` literal in internal/tui test files (rule: `grep -n 'Notice{' internal/tui/*_test.go`, shared helpers such as seededStartupNote/withSeededStartupNote included) sets an explicit Kind in this item — a loud kind where it set `Loud: true`, a routine kind otherwise — so item 2's zero-kind-is-loud flip changes no test outcome; the three rebind-produced kinds NoticeModelBinding, NoticeTrustedHint and NoticeRosterUnapplied are `sourceBeat` (ModelBinding and TrustedHint routine, RosterUnapplied loud), and item 5 uses the same sources.
`TestNoticeKindClasses` pins the zero kind through the table/class lookup (e.g. a `classOf(NoticeKind)` helper or `noticeKinds[0]`), never through the transitional `Notice.loud()`, which returns `n.Loud` when `Kind == 0`; item 2 adds the `Notice{}.Loud() == true` assertion once the fallback goes.
The zero-value NoticeKind's table row is classLoud with sourceBoundary, declared once in this item, so a Notice whose producer set no Kind lands as an ephemeral own line, like a loud start-up notice at the base, never a persisted one; TestNoticeKindClasses pins that row.
**Files:** internal/tui/noticekind.go, internal/tui/noticekind_test.go, internal/tui/tui.go, internal/tui/doc.go, internal/tui/model.go, internal/tui/heartbeat.go, internal/tui/model_test.go, internal/tui/heartbeat_test.go, internal/tui/sessionlog_test.go
**Read first:** internal/tui/tui.go — Notice; internal/tui/model.go — noteStartupNotices, noteContextFiles; internal/tui/heartbeat.go — applyRebind; internal/notice/contextfiles.go — ContextFileNotices;
internal/tui/heartbeat_test.go — TestSessionLogLeavesTheFirstBeatsLoudNotesOnTheirOwnLine; internal/tui/model_test.go — TestStartupNoticesPostAsEphemeralNotes; internal/tui/sessionlog_test.go — TestSessionLogLeavesAnAnomalyOnItsOwnLine
**Tests:** `TestNoticeKindClasses` (table: each declared kind → class and source, the three rebind-produced kinds `sourceBeat`; zero kind → loud and `sourceBoundary`, read through the table lookup). Existing start-up and session-log tests pass unchanged in outcome.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestNoticeKind|TestStartupNotices|TestSessionLog|TestRebindNoticesSurfaceAsNotes|TestDocMapNamesEveryFile' ./internal/tui/`
**Commit:** `refactor(tui): a start-up notice's class comes from one NoticeKind table`

## 2. cmd/apogee producers name a Kind; the Loud field goes — ✅ DONE (2026-11-21)
NOTES (2026-11-21): consequential edit — internal/tui/heartbeat.go: made necessary by renaming Notice.loud() to the exported Notice.Loud()
NOTES (2026-11-21): consequential edit — internal/tui/model.go: made necessary by renaming Notice.loud() to the exported Notice.Loud()
NOTES (2026-11-21): consequential edit — internal/tui/noticekind_test.go: made necessary by dropping the Loud field and fallback; adds the `Notice{}.Loud() == true` assertion item 1's section hands to item 2

**What:**
**Goal:** every `tui.Notice` built in cmd/apogee sets a `Kind` from item 1's exported constants and no `Loud`; `tui.Notice` has no `Loud` field; `func (n Notice) Loud() bool` (exported, table-backed) answers the class; stderr text is byte-identical.
**Approach (assumed at the header base):** the nine sites: `announceConfinement` (wire_boot.go: unconfined, degraded, residual), the share-group warning (wire_live.go), `announceTruncatedMCP` and `announceSkippedMCP` (mcp_cmd.go), `rootWiring.rebind` (wire_verbs.go: binding notices, hint notice, roster unapplied). Rename item 1's transitional `loud()` to `Loud()` and drop the fallback. cmd/apogee tests asserting `.Loud` read `.Loud()`; add one assertion that every notice `announce` collects has a non-zero Kind.
**Regression guard.** Dropping the fallback makes every zero-Kind `Notice` loud; item 1 has already given every `Notice{` literal in internal/tui/*_test.go an explicit Kind (the routine ones included: model_test.go:8979, :9032; heartbeat_test.go:1459, :1486 `withSeededStartupNote`, :1588), so no internal/tui test outcome changes here. The internal/tui Acceptance run adds `TestRebindNoticesSurfaceAsNotes|TestSessionLog` to prove it.
**Files:** cmd/apogee/wire_boot.go, cmd/apogee/wire_live.go, cmd/apogee/wire_verbs.go, cmd/apogee/mcp_cmd.go, cmd/apogee/wire_boot_test.go, cmd/apogee/mcp_cmd_test.go, cmd/apogee/wire_server_test.go, internal/tui/tui.go
**Read first:** cmd/apogee/wire_verbs.go — rootWiring.rebind, hintNotice; cmd/apogee/wire_boot.go — announceConfinement, rootWiring.announce; cmd/apogee/mcp_cmd.go — announceTruncatedMCP, announceSkippedMCP;
cmd/apogee/wire_live.go — sessionStoreOptions shareWarning announce; cmd/apogee/wire_boot_test.go — TestRunRootConfinementStartupNotices
**Tests:** existing cmd/apogee start-up tests, converted; a new assertion in `TestRunRootConfinementStartupNotices` that each collected notice has a Kind.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/ ./internal/tui/`
- `go test -race -count=1 -run 'TestRunRootConfinementStartupNotices|TestStartupSkipsARepoServer|TestStartupAnnouncesAnMCPToolListCutShort|TestRebindReportsARefusedRosterSwapAsANotice' ./cmd/apogee/`
- `go test -race -count=1 -run 'TestNoticeKind|TestStartupNotices|TestRebindNoticesSurfaceAsNotes|TestSessionLog' ./internal/tui/`
Depends on item 1.
**Commit:** `refactor(apogee): start-up notices name their NoticeKind`

## 3. The TUI owns a notice's transcript wording — ✅ DONE (2026-11-21)
NOTES (2026-11-21): consequential edit — internal/tui/doc.go: made necessary by noticekind.go now holding the residual kind's transcript wording (Model.transcriptNotes) beside the class table; its file-map line names it.
NOTES (2026-11-21): TestStartupNoticesPlaceTheirLogWordingOneNoteEach is renamed TestStartupNoticesWordTheResidualFromTheConfinementFacts (still under the Acceptance's TestStartupNotices filter); it also pins the zero-ConfinementInfo fallback to Text. assertResidualLogWording keeps its name and now takes tui.Options, asserting the Kind, the byte-identical stderr text, and that Options.Confinement carries the backend and caps the TUI words the notes from.

**What:**
**Goal:** `tui.Notice` has no `Log` field; the TUI derives each kind's transcript wording itself — `NoticeFenceResidual` renders the `probe.ResidualNotes` lines for `Options.Confinement`, every other kind its `Text`; the session log shows the same lines as at the base.
**Approach (assumed at the header base):** a Model method `transcriptNotes(n Notice) []string` in noticekind.go replaces `Notice.transcriptNotes`; for `NoticeFenceResidual` it calls `probe.ResidualNotes(m.opts.Confinement.Backend, m.opts.Confinement.Caps, domain.ModeAuto, true)` (the kind attests the gate fired) and falls back to `Text` when that returns nothing. `probe.ResidualNotes` stays the composer (no probe change). `announceConfinement` stops setting `Log:`; `assertResidualLogWording` becomes a Kind assertion plus the byte-identical stderr check. `foldHardBreaks` stays as a session-log invariant (layout.md names it); rewrite its comment so it no longer cites the residual's stderr shape.
**Files:** internal/tui/noticekind.go, internal/tui/tui.go, internal/tui/model.go, internal/tui/transcript.go, internal/tui/model_test.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_boot_test.go
**Read first:** internal/tui/tui.go — Notice.transcriptNotes, ConfinementInfo; cmd/apogee/wire_options.go — Confinement: tui.ConfinementInfo{Backend: probe.BackendName, Caps}; internal/probe/confinement.go — ResidualNotes;
cmd/apogee/wire_boot.go — announceConfinement; internal/tui/transcript.go — foldHardBreaks; cmd/apogee/wire_boot_test.go — assertResidualLogWording; internal/tui/model_test.go — TestStartupNoticesPlaceTheirLogWordingOneNoteEach
**Tests:** rewrite `TestStartupNoticesPlaceTheirLogWordingOneNoteEach` as a residual-kind wording test that compares against `probe.ResidualNotes` output; `TestSessionLogFoldsTheResidualNotice` unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestStartupNotices|TestSessionLogFoldsTheResidualNotice|TestNoticeKind' ./internal/tui/`
- `go test -race -count=1 -run 'TestRunRootConfinementStartupNotices|TestE2EAutoDegradationJourney' ./cmd/apogee/`
Depends on item 2.
**Commit:** `refactor(tui): the TUI words a start-up notice for the transcript`

## 4. The start-up chrome module: destination table and launch-side routing — ✅ DONE (2026-11-21)
NOTES (2026-11-21): re-derived from the plan's `destination(rule kindRule, …)` signature — item 1 named the row type `noticeKindRow`, so destination takes a `noticeKindRow`; the chrome also carries `place(t *transcript, kind, contact, text)`, the router `m.note` delegates to, which is what lets the converted sessionlog tests run on a bare transcript + startupChrome.
NOTES (2026-11-21): the open/closed state is the chrome's `isLogOpen` field (zero value closed) with `open()`/`close()` methods; tests read `m.chrome.isLogOpen`. noteRebindRoutine (heartbeat.go) now reads `m.chrome.isLogOpen` and calls `addLogNote`, unchanged in effect — item 5 routes it through the chrome.
NOTES (2026-11-21): the model_test.go StartupNotices literals (TestStartupNoticesPostAsEphemeralNotes, TestStartupNoticesWordTheResidualFromTheConfinementFacts) already carry NoticeFenceResidual, a non-beat routine kind, so they are left as they are; model_test.go is not in FILES. Only withSeededStartupNote (heartbeat_test.go) moves to kindContextLoaded.
NOTES (2026-11-21): TestSessionLogAbsentWithoutRoutineNotes in its bare form asserts no log entry is drawn; the painted-frame header check it carried as a Model test is dropped, since with no entry there is nothing to paint. TestStartupChromePlace and TestStartupChromeZeroValueIsClosed are added beside TestStartupChromeDestination.

**What:** Recast at the regression check (2026-11-21).
**Goal:** one module `startupChrome` (internal/tui/startupchrome.go) owns the session log's open/closed state and the pure rule `destination(rule kindRule, c contact, logOpen bool) dest` → `destLog | destLinePersisted | destLineEphemeral`; every launch-side note (context files, colour-scheme warnings, start-up notices, git refusal) is placed through it; the transcript keeps no log-open flag and places what it is told.
**Approach (assumed at the header base):** the rule, binding: `destLog` when class routine, log open, and (source boundary or ambient, or source beat with `c == contactFirst`); otherwise `destLinePersisted` for source beat and `destLineEphemeral` for boundary/ambient. `type contact uint8` (`contactEvent`, `contactFirst`, `contactSettle`) is declared here, used by item 5. `startupChrome` is a plain value (ADR 0011) held in `sessionView` (see the guard). `m.note(kind, c, text)` is the one entry; add kinds `kindColorScheme` (loud, ambient), `kindGitRefusal` (routine, ambient). Remove `transcript.sessionLogOpen`, `openSessionLog`, `closeSessionLog`; `addSessionLogNote` becomes an unconditional `addLogNote`. `enterRunning` calls `m.chrome` close. `noteContextFiles`, `noteColorSchemeWarnings`, `noteStartupNotices`, `noteGitRefusal` go through `m.note`; `noteRoutine` is deleted. Every `transcript.reset` path must leave the log as at the base (open at a boundary).
**Regression guard.** `startupChrome` is a field of `sessionView` (the per-session state every boundary resets), not a bare Model field, so `TestSessionViewHoldsEveryFieldTheBoundariesReset` (internal/tui/watch_test.go) stays green — add it to this item's Acceptance run and to items 5 and 6; this item also lists and edits the boundary call sites in internal/tui/commandrun.go and internal/tui/sessions.go that call the removed `openSessionLog` (re-pointed to the chrome; item 6 later folds them into `beginSession`).
The three `m.transcript.sessionLogOpen` reads in heartbeat_test.go (:1537, :1616, :1637) and heartbeat.go:474's `addSessionLogNote` call move to the chrome in this item. `TestSessionLogOpensAtBoundaryWithoutContextFiles` stays model-driven (clearSession/newSession/restoreSession from a closed chrome) and asserts the chrome reopened; only the reset-only subtest moves to the bare form. Every comment naming a removed symbol is rewritten (entrykind.go:75, :137; render.go:1292; paintcache.go:112; heartbeat.go:583).
The chrome's zero value means the session log is CLOSED (an `open`-style bool, false = closed), so a fresh `sessionView` literal (e.g. clearWatchView in internal/tui/watch.go) is closed exactly as at the base, and only a boundary opens it.
The launch-side callers (noteContextFiles, noteColorSchemeWarnings, noteStartupNotices, noteGitRefusal) pass `contactEvent`; the routine StartupNotices literals in tests (withSeededStartupNote, heartbeat_test.go:1486; model_test.go:8979, :9032) carry a non-beat routine kind (`kindContextLoaded` fits seededStartupNote's "context: AGENTS.md" text), never NoticeModelBinding or NoticeTrustedHint, which `sourceBeat` would put on a persisted own line.
`TestNoticeKindClasses` gains the rows kindColorScheme (loud, ambient) and kindGitRefusal (routine, ambient). The "enterRunning closes it" subtest stays model-driven (sendLine, then assert `m.chrome` closed and a later `m.note` of a routine kind lands on its own line); only "reset clears it" moves to the bare form.
**Files:** internal/tui/startupchrome.go, internal/tui/startupchrome_test.go, internal/tui/doc.go, internal/tui/noticekind.go, internal/tui/transcript.go, internal/tui/model.go, internal/tui/gitstatus.go, internal/tui/sessionview.go, internal/tui/commandrun.go, internal/tui/sessions.go, internal/tui/heartbeat.go, internal/tui/entrykind.go, internal/tui/render.go, internal/tui/paintcache.go, internal/tui/sessionlog_test.go, internal/tui/heartbeat_test.go, internal/tui/noticekind_test.go, internal/tui/model_test.go
**Read first:** internal/tui/transcript.go — openSessionLog, closeSessionLog, addSessionLogNote, reset; internal/tui/model.go — newModel, noteContextFiles, noteRoutine, noteStartupNotices, noteColorSchemeWarnings, enterRunning; internal/tui/heartbeat.go — noteRebindRoutine, applyRebind;
internal/tui/commandrun.go — resetSessionView; internal/tui/sessions.go — resumeLoaded; internal/tui/watch.go — clearWatchView, swapSessionView;
internal/tui/watch_test.go — TestSessionViewHoldsEveryFieldTheBoundariesReset, fieldsWritten; internal/tui/sessionlog_test.go — storedSessionLogNotes, TestSessionLogOpensAtBoundaryWithoutContextFiles, TestSessionLogClosedByEnterRunningAndClearedByReset
**Tests:** `TestStartupChromeDestination` — table over class × source × contact × logOpen → dest. Convert the sessionlog_test.go routing tests (anomaly on its own line, absent without routine notes, the "reset clears it" subtest) to a bare `transcript` + `startupChrome`, except that `TestSessionLogOpensAtBoundaryWithoutContextFiles` and the "enterRunning closes it" subtest stay model-driven (the latter asserts `m.chrome` closed after sendLine and a later routine `m.note` on its own line); `TestNoticeKindClasses` gains the kindColorScheme and kindGitRefusal rows; painter/codec tests stay on transcript.
**Acceptance:**
- `go vet ./internal/tui/`
- `! grep -rn 'sessionLogOpen\|openSessionLog\|closeSessionLog\|addSessionLogNote\|noteRoutine\b' internal/tui/`
- `! awk '/^func \(m \*Model\) note(ContextFiles|ColorSchemeWarnings|StartupNotices|GitRefusal)\(/,/^}/' internal/tui/model.go internal/tui/gitstatus.go | grep -n 'transcript\.add'`
- `go test -race -count=1 -run 'TestStartupChrome|TestSessionLog|TestGitStatusRefusal|TestStartupNotices|TestNoticeKind|TestDocMapNamesEveryFile|TestSessionViewHoldsEveryFieldTheBoundariesReset' ./internal/tui/`
Depends on item 3.
**Commit:** `refactor(tui): one start-up chrome decides where a launch note lands`

## 5. Heartbeat notes route through the start-up chrome — ✅ DONE (2026-11-21)
NOTES (2026-11-21): a beat after first contact (and the post-/server seed) builds its intent with contactEvent, not contactSettle: rebindNote's quiet argument is `c != contactEvent`, so only first contact and a fallback settle stay quiet, as at the base. The contactEvent/contactFirst/contactSettle comments in startupchrome.go are rewritten to match (and no longer cite rebindIntent.firstBeat).
NOTES (2026-11-21): the eight heartbeat kinds are appended after kindGitRefusal in noticekind.go and pinned in TestNoticeKindClasses (consequential: its length check against noticeKinds requires every new row).
NOTES (2026-11-21): consequential edit — internal/tui/noticekind_test.go: made necessary by the eight new noticeKinds rows (TestNoticeKindClasses counts the table).
NOTES (2026-11-21): consequential edit — internal/tui/model.go: made necessary by m.note now taking heartbeat notes too (its doc said "every launch-side note").
NOTES (2026-11-21): TestSessionLogClosedByAPromptLeavesTheFirstBeatsNoteItsOwn and TestSessionLogLeavesTheFirstBeatsLoudNotesOnTheirOwnLine are folded into a heartbeat-kind table inside TestStartupChromeDestination (first beat's routine note persisted once the log is closed; refused rebind, unapplied roster, unknown window, cleared effort persisted while open); the first-beat-into-log, fallback-settle and /model-pick + post-/server-seed tests stay as wiring smoke tests.
NOTES (2026-11-21): the mandatoryEffortNote and foldRoutingNotice doc comments named addNote/addEphemeralNote as their destination; rewritten to name the chrome. The serverSwitchNote and "server fallback: could not follow" notes (heartbeat.go) are not in the item's enumerated list and still call transcript.addNote directly.
NOTES (2026-11-21): internal/tui/transcript_test.go, listed in Files, needed no change (rebindNote's signature is unchanged). The open issue first-contact-notes-repeat still names noteRebindRoutine in its body; issues are not edited by this run.
NOTES (2026-11-21): per the item's regression guard, a rebind notice with a zero Kind now takes the zero row (loud, sourceBoundary: ephemeral own line) where the base persisted it via Notice.Loud(); every cmd/apogee rebind producer names a Kind since item 2, so no shipped note moves.

**What:**
**Goal:** every heartbeat and rebind note (binding change, refused rebind, unknown window, effort cleared, mandatory effort, offline, online, routing) is placed by `m.note` with a kind from the table; `rebindIntent` carries one `contact` value instead of the `quietSeed`/`firstBeat` pair; `noteRebindRoutine` is gone; every destination equals the base's.
**Approach (assumed at the header base):** kinds: `kindRebindChange` (routine, beat), `kindRebindFailed`, `kindWindowUnknown`, `kindEffortCleared`, `kindOffline`, `kindOnline` (loud, beat), `kindRouting` (loud, ambient), `kindMandatoryEffort` (routine, beat). `observeBinding` sets `contactFirst`, `followFallback` sets `contactSettle`; `rebindNote`'s quiet argument is `c != contactEvent` (its signature and wording unchanged). The `firstContact` computation in `foldBeat` (reads `switched`, `everOnline`, `failures`) is untouched. `pendingRebind` stores `rebindIntent` by value — `contact` stays a plain value.
**Regression guard.** The `result.Notices` loop in `applyRebind` (heartbeat.go:429-435) calls `m.note(n.Kind, intent.contact, n.Text)`; NoticeModelBinding, NoticeTrustedHint and NoticeRosterUnapplied are `sourceBeat` (item 1's rows), and a zero Kind on that path takes item 1's zero row (loud, `sourceBoundary`: own line, ephemeral). `contactEvent` is the zero value of `contact` (declared first): the `/model` pick at picker.go:1397 builds its intent with no contact, and the `rebindIntent{model, window}` DeepEqual literals (engineholds_test.go:244, heartbeat_test.go:1166, actuation_test.go:525, command_test.go:1274) rely on it. Keep the /model-pick and post-/server-seed subtests (heartbeat_test.go:1606) as wiring smoke tests.
The leftover-field check is `! grep -nE 'noteRebindRoutine|firstBeat|quietSeed:|intent\.quietSeed' internal/tui/heartbeat.go` — `rebindNote`'s own parameter may keep the name `quietSeed`, so that bare name is not grepped.
**Files:** internal/tui/heartbeat.go, internal/tui/noticekind.go, internal/tui/startupchrome.go, internal/tui/heartbeat_test.go, internal/tui/startupchrome_test.go, internal/tui/transcript_test.go
**Read first:** internal/tui/heartbeat.go — rebindIntent, observeBinding, applyRebind, noteRebindRoutine, rebindNote, followFallback, foldBeat; internal/tui/picker.go — applyRebind call in the /model pick
**Tests:** fold the five `TestSessionLog*` heartbeat tests into `TestStartupChromeDestination` rows; keep one wiring smoke test for a first beat into the log, one for a fallback settle on its own line, and the /model-pick and post-/server-seed subtests of `TestSessionLogLeavesAModelPickAndAServerSeedOnTheirOwnLine`; `TestSessionLogClosedByAPromptLeavesTheFirstBeatsNoteItsOwn`'s persisted outcome is preserved.
**Acceptance:**
- `go vet ./internal/tui/`
- `! grep -nE 'noteRebindRoutine|firstBeat|quietSeed:|intent\.quietSeed' internal/tui/heartbeat.go`
- `go test -race -count=1 -run 'TestSessionLog|TestRebind|TestFirstContactWindowNote|TestLateSeed|TestUnknownWindowNotedOnBind|TestColdStartFailureIsOfflineImmediately|TestOfflineDebouncesAtIdle|TestBridgeNotifyRoutingLandsAsAnEphemeralNote|TestStartupChrome|TestNoticeKind|TestSessionViewHoldsEveryFieldTheBoundariesReset' ./internal/tui/`
Depends on item 4.
**Commit:** `refactor(tui): heartbeat notes route through the start-up chrome`

## 6. Session boundaries and box restatement are chrome operations — ✅ DONE (2026-11-21)
NOTES (2026-11-21): newModel now goes through beginSession too, so construction runs transcript.reset() on the fresh, empty transcript (it drops nothing; one extra generation bump and an empty paint-cache clear), where the plan's approach said "no reset" for that site.
NOTES (2026-11-21): consequential edit — internal/tui/transcript.go: made necessary by beginSession (the addStartup and reset doc comments named newModel/startNewSession as the seeding callers).
NOTES (2026-11-21): consequential edit — internal/tui/doc.go: made necessary by beginSession/restateBox (the package map's startupchrome.go line now names the two box operations).

**What:**
**Goal:** a session boundary (launch, `/clear`-style reset, session restore) is one call `m.beginSession()` that resets the transcript, adds the start-up box and opens the log; restating the box at a rebind is one call `m.restateBox()`; no call site outside startupchrome.go composes reset → addStartup → open by hand or calls `refreshStartup(newStartupView(…))` directly.
**Approach (assumed at the header base):** the three boundary sites: `newModel` (no reset; before `replayResumed`), `resetSessionView` (commandrun.go), the restore fold in sessions.go (before `replayScrollback`). The five box-restatement callers: `applyRebind`, `foldServerSwitch`, `followFallback` (heartbeat.go), prebound.go, update_check.go.
**Regression guard.** The chrome's log state is a `sessionView` field (item 4), so `TestSessionViewHoldsEveryFieldTheBoundariesReset` (watch_test.go:1809) stays green with `beginSession` inside `resetSessionView` and `resumeLoaded`. The Acceptance grep catches `addStartup(newStartupView` too (commandrun.go:369, sessions.go:958, model.go:876). The restatement callers' own tests (`TestUpdateCheck*`, `TestPreboundChoiceBindsRecordsAndEndsTheState`) join the run. The boundary-reopens-the-log proof is its own Model test, `TestStartupChromeBoundaryReopensTheLog`, driving clearSession/newSession/restoreSession (contextfiles_test.go:55-73) from a closed chrome — not a row of the pure `TestStartupChromeDestination`.
**Files:** internal/tui/startupchrome.go, internal/tui/startupchrome_test.go, internal/tui/model.go, internal/tui/commandrun.go, internal/tui/sessions.go, internal/tui/heartbeat.go, internal/tui/prebound.go, internal/tui/update_check.go
**Read first:** internal/tui/commandrun.go — resetSessionView; internal/tui/sessions.go — resumeLoaded; internal/tui/model.go — newModel, replayResumed; internal/tui/heartbeat.go — applyRebind;
internal/tui/watch_test.go — TestSessionViewHoldsEveryFieldTheBoundariesReset, fieldsWritten; internal/tui/contextfiles_test.go — clearSession
**Tests:** existing boundary tests pass unchanged; add `TestStartupChromeBoundaryReopensTheLog`, a Model test proving each boundary reopens a closed log.
**Acceptance:**
- `go vet ./internal/tui/ && ! grep -n 'refreshStartup(newStartupView\|addStartup(newStartupView' internal/tui/*.go | grep -v startupchrome.go`
- `go test -race -count=1 -run 'TestNewModelSeedsStartupBox|TestClearResetsToStartupBox|TestNewStartupViewMatchesSeed|TestSessionLog|TestStartupChrome|TestLateSeed|TestRestore|TestUpdateCheck|TestPrebound|TestSessionViewHoldsEveryFieldTheBoundariesReset' ./internal/tui/`
Depends on item 5.
**Commit:** `refactor(tui): a session boundary and a box restatement are one chrome call each`

## 7. Docs name every loud kind, pinned by a drift test

**What:**
**Goal:** layout.md §"The start-up box", docs/manual/commands.md §"The start-up session log" and the glossary Note `session-log` list every loud kind in `noticeKinds` (including the MCP tool list cut short and standing content over its Budget share) and name the git refusal as routine; a test fails when a loud kind is added without a docs phrase or a docs phrase loses its kind; the docs say the list is defined in `internal/tui/noticekind.go`.
**Approach (assumed at the header base):** a test-only map in noticekind_test.go from each loud kind to the phrases each doc uses; `TestLoudKindsNamedInDocs` reads layout.md, commands.md and the glossary Note (precedent: `internal/tools/manual_drift_test.go`) and asserts every loud kind in `noticeKinds` has its phrases present in each. Edit the glossary Note in place (`.capcom/knowledge/glossary/glossary-identity/session-log.md`, then `capcom check`). Refresh `internal/tui/doc.go` narration for noticekind.go and startupchrome.go.
**Regression guard.** Match against whitespace-normalised text (`strings.Join(strings.Fields(doc), " ")`): the docs are hard-wrapped. The map is keyed per kind per doc with `[]string` values — the docs word several kinds differently today, and `kindContextAnomaly` covers two texts (internal/notice/contextfiles.go:41, :58); it skips the reserved zero kind and asserts every key is `classLoud`. The test also reads the glossary Note and asserts `internal/tui/noticekind.go` appears in each of the three docs.
**Files:** layout.md, docs/manual/commands.md, .capcom/knowledge/glossary/glossary-identity/session-log.md, internal/tui/noticekind_test.go, internal/tui/doc.go
**Read first:** layout.md — §"The start-up box" ("The session log beneath it"); docs/manual/commands.md — §"The start-up session log"; .capcom/knowledge/glossary/glossary-identity/session-log.md;
internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName; internal/notice/contextfiles.go — ContextNotice.Anomaly;
cmd/apogee/mcp_cmd.go — announceTruncatedMCP, announceSkippedMCP; cmd/apogee/wire_verbs.go — rootWiring.rebind
**Tests:** `TestLoudKindsNamedInDocs`.
**Acceptance:**
- `go test -race -count=1 -run 'TestNoticeKind|TestLoudKindsNamedInDocs|TestDocMapNamesEveryFile' ./internal/tui/`
- `capcom check`
Depends on item 6.
**Closes:** startup-notice-class-scattered; startup-note-routing-scattered
**Commit:** `docs(tui): the loud start-up notice kinds are listed from one table`
