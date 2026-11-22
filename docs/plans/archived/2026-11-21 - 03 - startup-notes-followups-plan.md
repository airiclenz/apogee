# Start-up notes follow-ups — server-move notes routed, first-contact notes not saved

**Goal:** every server-move note reaches the transcript through the start-up chrome with a row in the notice-kind table, and a first-contact heartbeat note is shown but never saved to the session record, so a resumed session stops collecting one copy per launch.

**Date:** 2026-11-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 38fb10b7

**Sources:**
- `.capcom/issues/server-switch-notes-unrouted.md`, `.capcom/issues/first-contact-notes-repeat.md`
- `docs/plans/archived/2026-11-21 - 02 - startup-notes-plan.md` (the chrome and kind table these items extend)
- ADR 0022 addendum 0801 (`capcom know show adr-0022 --with-children`): a note re-derived at startup or resume is ephemeral; a server switch is earned and stays persisted
- `.capcom/knowledge/glossary/glossary-identity/session.md`, `.capcom/knowledge/glossary/glossary-identity/session-log.md`

**Ratified design calls** (owner, 2026-11-21):
- **First-contact repeats:** first-contact beat notes become ephemeral; no dedupe against the record.
- **Server-move classes:** the `switching server: …` note and the server-fallback note are routine; "the display could not follow" and "could not record the server choice" are loud.
- **Cold-start offline repeat:** out of scope; filed as `offline-notes-repeat`.
- **Watch mode:** a watcher no longer sees the owner's first-contact notes; accepted (owner, 2026-11-21).

**Regression check (2026-11-21, 38fb10b7):**
- 1: guard folded (unlisted `noteWorkflowSessionSwitch` fallback note and `warn` callers; kind named for any unrecorded choice; empty warning places nothing; happy-path bite as a mutation).
- 2: guard folded (writer decision: a stashed first-contact intent keeps `contactFirst`, its notes land ephemeral, per ADR 0022 addendum 0801).
- 2 (second pass): guard folded (writer decision: Watch mode loses the owner's first-contact notes on purpose, said in What and the session.md ephemeral-list sentence, no code change; "and from forks" kept and pinned by an `entriesToRecords(prefixThrough(...))` assertion).

**Standing requirements:**
- skills: coding-standards
- Narrow test runs only: one package, a `-run` filter, `GOMEMLIMIT=2GiB`; never a multi-package or whole-package `-race` run.
- stderr, headless and daemon output stay byte-identical; only the TUI transcript and session record change, and only in item 2.
- Any authorised deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The cold-start `offline` / `connected: …` repeat (`offline-notes-repeat`).
- Any change to the destination of boundary or ambient notes, or to stderr notice text.
- Deduplicating notes already stored in existing session records.

## 1. Server-move notes route through the start-up chrome — ✅ DONE (2026-11-21)

NOTES (2026-11-21): `choiceRecord.warn` returns `(string, bool)` (the plan's alternative form); each of the seven callers places the text with `<model>.note(kindChoiceUnrecorded, contactEvent, w)` only when ok, so a move with no warning places no empty entry.

NOTES (2026-11-21): new kinds named `kindServerSwitch`, `kindServerFallback`, `kindFallbackUnfollowed`, `kindChoiceUnrecorded`. All three docs and `loudKindPhrases` use one phrase per loud kind: "a server fallback the display could not follow" and "a choice that could not be saved to config.yaml".

NOTES (2026-11-21): `TestServerSwitchHappyPath`'s new not-ephemeral assertion was checked by mutation: with `kindServerSwitch` declared `sourceAmbient` it fails ("last entry … ephemeral true"), and it passes once restored.

**What:**
**Goal:** every note `Model.foldServerSwitch`, `Model.followFallback`, `choiceRecord.warn` and the server-fallback note in `foldEvent` write reaches the transcript through `Model.note` with a row in `noticeKinds`; none of them calls `transcript.addNote` directly. Each note keeps its exact text and its destination today: its own persisted line, with the session log open or closed.
**Approach (assumed at the header base):** add four kinds to `noticeKinds` (`internal/tui/noticekind.go`), all `source: sourceBeat`, raised with `contactEvent`: a server switch (`serverSwitchNote`, routine), a server fallback (`fallbackNote` in `internal/tui/fold.go`, routine), a follow refusal ("server fallback: the display could not follow …" in `followFallback`, loud) and an unrecorded server choice (`choiceRecord.warn` in `internal/tui/prebound.go`, loud). Widen the `sourceBeat` doc comment to "a heartbeat rebind or a server move the session made". `destination` in `internal/tui/startupchrome.go` does not change: a `sourceBeat` row with `contactEvent` already resolves to `destLinePersisted`. `choiceRecord.warn` stops taking `*transcript` and returns the warning text (empty when there is none), which its callers pass to `m.note`. Extends the existing table and chrome; no new module.
Name both loud kinds in the three loud lists: `layout.md` §"The start-up box", `docs/manual/commands.md` (the "A notice that needs you is never folded away" paragraph) and `.capcom/knowledge/glossary/glossary-identity/session-log.md`, and add their phrases to `loudKindPhrases`.
**Regression guard.** `Model.noteWorkflowSessionSwitch` (`internal/tui/model.go`, reached from model.go's Update and `foldWatchedEvent` in watch.go) writes `fallbackNote` too: route it through `m.note` with the same server-fallback kind and `contactEvent`, as `foldEvent`'s is.
Every `choiceRecord.warn` caller (all seven, `grep -n '\.warn(' internal/tui/*.go`: heartbeat.go, prebound.go, picker.go ×4 — `switchToServer`, the sub-agents pick, `bindPickedModel` ×2 — and actuation.go `recordLoadedProfile`) passes the text to the note on its own Model copy (`next.note` for the `next.transcript` caller), and places it ONLY when non-empty (or `warn` returns `(string, bool)`): `m.note("")` commits an empty entry, so "nothing at all when there is none" must hold.
The unrecorded kind covers every failed choice write (server, model, sub-agents server, launch profile): name it for that (e.g. `kindChoiceUnrecorded`), and word `loudKindPhrases` and the three docs' phrases as "a choice that could not be saved to config.yaml", not the server alone.
`TestServerSwitchHappyPath`'s not-ephemeral assertion passes on the pre-item tree; its bite is a mutation (declare the switch kind `sourceAmbient`/`sourceBoundary` → the entry stops being a persisted line and the test fails). The pre-item bite is the new `TestNoticeKindClasses` rows.
**Files:** internal/tui/noticekind.go, internal/tui/noticekind_test.go, internal/tui/heartbeat.go, internal/tui/fold.go, internal/tui/prebound.go, internal/tui/model.go, internal/tui/picker.go, internal/tui/actuation.go, internal/tui/startupchrome_test.go, internal/tui/picker_test.go, layout.md, docs/manual/commands.md, .capcom/knowledge/glossary/glossary-identity/session-log.md
**Read first:** internal/tui/prebound.go — choiceRecord.warn, recordServerChoice, Model.foldServerBind; internal/tui/heartbeat.go — Model.foldServerSwitch, Model.followFallback, serverSwitchNote; internal/tui/fold.go — Model.foldEventAgainst, fallbackNote; internal/tui/model.go — Model.note, Model.noteWorkflowSessionSwitch;
internal/tui/picker.go — Model.switchToServer, Model.bindPickedModel, recordSubAgentsChoice; internal/tui/actuation.go — Model.recordLoadedProfile; internal/tui/startupchrome.go — destination, startupChrome.place;
internal/tui/noticekind_test.go — TestNoticeKindClasses, loudKindPhrases, TestLoudKindsNamedInDocs
**Closes:** server-switch-notes-unrouted
**Tests:**
- `TestNoticeKindClasses` pins the four new rows.
- `TestStartupChromeDestination` gains a row per new kind: `contactEvent`, `destLinePersisted` with the log open and with it closed.
- `TestServerSwitchHappyPath` also asserts that the switch note's entry is not ephemeral (bite by mutation: a switch kind declared `sourceAmbient`/`sourceBoundary` fails it).
- `TestServerSwitchHappyPath` and the sub-agents exact-slice notes test (`picker_test.go`) stay green: a move with no warning places no empty note.
- The `noteWorkflowSessionSwitch` fallback note is placed through `m.note` (covered by the Acceptance grep).
- `TestLoudKindsNamedInDocs` covers both loud kinds.
**Acceptance:**
- `go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNoticeKind|TestLoudKindsNamedInDocs|TestStartupChrome|TestServerSwitch|TestFallback|TestProfileLoad' ./internal/tui/`
- `! grep -nE 'addNote\((serverSwitchNote|fallbackNote|"server fallback)' internal/tui/*.go`
- `capcom check`

**Commit:** `refactor(tui): server-move notes route through the start-up chrome`

## 2. First-contact beat notes are not saved to the session record — ✅ DONE (2026-11-21)

**What:**
**Goal:** fixes `first-contact-notes-repeat` (a defect, not a regression). A heartbeat note raised with `contactFirst` is an ephemeral transcript entry: shown exactly as today, but absent from the session record and from forks. A beat note raised with `contactEvent` or `contactSettle` is persisted as before. Launching, resuming and launching again leaves no copy of a first-contact note in the record, and exactly one in the view. No loud note ever goes to the session log.
**Approach (assumed at the header base):** in `startupChrome`'s `destination` (`internal/tui/startupchrome.go`), keep the routine-into-open-log branch unchanged. After it, a `sourceBeat` note with `contactFirst` resolves to `destLineEphemeral`, and every other `sourceBeat` note to `destLinePersisted`. Boundary and ambient rows are untouched, and item 1's server-move kinds (`contactEvent`) stay persisted. The rule lives in `destination` alone; no caller adds an ephemeral flag of its own. A first-contact intent stashed in `pendingRebind` while the model is busy keeps `contactFirst` when `applyPendingRebind` applies it after the first Exchange, so its notes land ephemeral too — intended. Watch mode is a record reader: it rebuilds a live session's view from the owner's record (`watch.go` `recordHead` → `decodeTranscript`), so a watcher no longer sees the owner's first-contact notes (unknown window, refused rebind, unapplied roster) — accepted on purpose; no code change.
Prose guard: rewrite every comment and doc sentence that says a beat note's own line is persisted, or that lists what a transcript keeps out of the record. Find them with `grep -rn -iE 'persisted|ephemeral|addEphemeralNote' internal/tui/startupchrome*.go internal/tui/heartbeat.go internal/tui/transcript.go .capcom/knowledge/glossary/glossary-identity/session.md docs/manual/sessions.md layout.md`. This includes the stale `effortClearedNote` doc ("the note's one destination is addNote"). The rewritten `session.md` ephemeral-list sentence also says a watcher does not see the owner's first-contact notes.
**Regression guard.** A first-contact intent stashed in pendingRebind while the model is busy keeps contactFirst when applyPendingRebind applies it after the first Exchange, so its notes (e.g. "model changed: A → B") land ephemeral too — intended, since they are re-derived at every launch (ADR 0022 addendum 0801); state this in the item's What and pin it with one TestStartupChromeDestination or heartbeat test row. Watch mode loses the owner's first-contact notes on purpose (it reads the record); say so in What and in the session.md ephemeral-list sentence; no code change. "And from forks" stays in the Goal: `TestFirstContactNotesStayOutOfAResumedRecord` asserts that `entriesToRecords` over `m.transcript.prefixThrough(...)` (the `fork.go` `forkAt` path) holds neither first-contact note.
**Files:** internal/tui/startupchrome.go, internal/tui/startupchrome_test.go, internal/tui/heartbeat.go, internal/tui/heartbeat_test.go, internal/tui/transcript.go, internal/tui/model_test.go, .capcom/knowledge/glossary/glossary-identity/session.md, docs/manual/sessions.md
**Read first:** internal/tui/startupchrome.go — destination, startupChrome.place; internal/tui/heartbeat.go — Model.foldBeat (firstContact), Model.observeBinding, Model.applyRebind, rebindIntent.contact; internal/tui/startupchrome_test.go — TestStartupChromeDestination;
internal/tui/heartbeat_test.go — wireRebind, fakeRebind, unbound, foldBeatMsg, noteTexts, countNotes; internal/tui/model_test.go — TestNewModelReplaysResumedScrollback; internal/tui/sessionsave.go — Model.snapshotPayload;
internal/tui/transcriptbridge.go — entriesToRecords, encodeTranscript; internal/tui/transcript.go — transcript.addEphemeralNote
**Closes:** first-contact-notes-repeat
**Depends on:** item 1
**Tests:**
- In `TestStartupChromeDestination`, the first-contact beat rows flip to `destLineEphemeral`: loud ones with the log open and closed, routine ones with it closed.
- New `TestFirstContactNotesStayOutOfAResumedRecord` (model_test.go), modelled on `TestNewModelReplaysResumedScrollback`:
  - Resume a seeded blob, then fold a first beat that raises the unknown-window note and an unapplied-roster notice (`fakeRebind`). The blob from `snapshotPayload`, decoded, holds neither note, and `noteTexts` shows each once. `entriesToRecords(m.transcript.prefixThrough(...))` — the fork path (`fork.go` `forkAt`) — holds neither note either.
  - Relaunch from that blob and fold the first beat again: the record still holds none, and the view holds one of each.
  - A later beat that changes the model (`contactEvent`) is in the record.
- New `TestRebindDeferredFirstContactStaysEphemeral` (heartbeat_test.go), modelled on `TestRebindDeferredWhileBusy`: a worker owns the engine before the first beat lands, the first beat changes the model and is stashed in `pendingRebind`; after `exchangeDoneMsg` the applied "model changed: …" note is shown once and is an ephemeral entry.
**Acceptance:**
- `go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStartupChrome|TestFirstContactNotesStayOutOfAResumedRecord|TestNewModelReplaysResumedScrollback|TestUnknownWindowNotedOnBind|TestRebind|TestSessionLog|TestDelayedConnectNotesOnce' ./internal/tui/`
- `capcom check`

**Commit:** `fix(tui): first-contact beat notes are not saved to the session record`
