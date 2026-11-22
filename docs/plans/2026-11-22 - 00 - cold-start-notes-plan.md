# Cold-start notes — a superseded refusal is not re-placed, a cold-start outage is not saved

**Goal:** a transcript swap re-places only the first-contact lines that still hold, and the notes a cold start re-derives while its server is down — the `server offline — …` note and the notes of the first beat that lands after it — are shown but never saved to the session record, so a session resumed N times with its server down carries none of them.

**Date:** 2026-11-22
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0fce30b8

**Regression check (2026-11-22, 0fce30b8):**
- 2: guard folded — the offline note is `server offline — <failure>` (`offlineNote`) everywhere and tests match it on the `server offline` prefix (writer's decision); the record test launches unbound so its landing writes `connected: …`; the prose guard greps all non-test `internal/tui/*.go` plus the manual and glossary, and names `transcript.go`; `server back online` stays out of the stash.

**Sources:**
- `.capcom/issues/first-contact-lines-stale.md`, `.capcom/issues/offline-notes-repeat.md`
- `docs/plans/archived/2026-11-21 - 03 - startup-notes-followups-plan.md` (the stash and the ephemeral first-contact rule these items extend)
- ADR 0022 addendum 0801 (`capcom know show adr-0022 --with-children`): a note re-derived at startup or resume is ephemeral
- ADR 0024 amendment 1111 (`capcom know show adr-0024-amend-1111`): a failed rebind is retried; its refusal is noted once per distinct target
- `.capcom/knowledge/glossary/glossary-identity/session.md`, `.capcom/knowledge/glossary/glossary-identity/session-log.md`

**Ratified design calls** (owner, 2026-11-22):
- **Superseded refusal:** a rebind refusal noted at any later contact drops the stashed first-contact refusal line; it is not replaced by the new one.
- **First landing after a cold-start failure:** its notes are display-only, keep their own lines, and are worded in full; a mid-session outage's notes stay saved.
- **Offline line after a swap:** the cold-start `server offline — …` line is not stashed or re-placed; the footer's `✦ offline` marker carries the state.

**Standing requirements:**
- skills: coding-standards
- Narrow test runs only: one package, a `-run` filter, `GOMEMLIMIT=2GiB`; never a multi-package or whole-package `-race` run, never two runs at once.
- stderr, headless and daemon output stay byte-identical; note texts do not change.
- Any authorised deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The `connected: …` a retry writes after a refused first contact (ADR 0024 amendment 1111) — it stays a persisted line.
- Any change to boundary or ambient note destinations, or to the loud/routine class of a kind.
- Deduplicating notes already stored in existing session records.

## 1. A later refusal drops the stashed first-contact refusal

**What:**
**Goal:** fixes `first-contact-lines-stale` (a defect, not a regression). After a first-contact rebind refusal for target A and a later refusal for target B at any other contact, a `/sessions` restore or `/fork` re-places no refusal line; every other stashed first-contact line (an unknown window, an unapplied roster) is still re-placed once. A repeat refusal for the same target as the stashed one leaves the stash as it is.
**Approach (assumed at the header base):** `heartbeatState.firstContactLines` (`internal/tui/heartbeat.go`) holds texts only; make each entry carry its `NoticeKind` and text (a small private struct; only ever replaced or appended to a clipped copy, ADR 0011). `Model.noteBinding` records the kind. In `Model.applyRebind`'s error branch, after the `lastRebindFailed == intent.model` early return and before the refusal is noted, when `intent.contact != contactFirst` filter the stash to drop entries of kind `kindRebindFailed` (`slices.DeleteFunc` on a clone). `Model.replaceFirstContactLines` (`internal/tui/model.go`) places each entry's text exactly as today. Rewrite the `firstContactLines` field comment to name the new supersession rule. Extends the existing stash; no new module.
Producers and consumers of the stash: `Model.noteBinding` (producer); `Model.replaceFirstContactLines`, the success-path clear in `Model.applyRebind`, and the wholesale reset in `Model.foldServerSwitch` (consumers). Each keeps its behaviour apart from the new drop.
**Files:** internal/tui/heartbeat.go, internal/tui/model.go, internal/tui/model_test.go
**Read first:** internal/tui/heartbeat.go — Model.applyRebind, Model.noteBinding, heartbeatState.firstContactLines, Model.observeBinding; internal/tui/model.go — Model.replaceFirstContactLines; internal/tui/sessions.go — Model.resumeLoaded;
internal/tui/model_test.go — TestRestoreRePlacesFirstContactLines; internal/tui/heartbeat_test.go — fakeRebind, upBeat, countNotes
**Closes:** first-contact-lines-stale
**Tests:**
- New `TestRestoreDropsASupersededFirstContactRefusal` in `internal/tui/model_test.go`: a `fakeRebind` refusing every target; first beat `upBeat("model-a", …)`, a later `upBeat("model-b", …)`; `restoreSession`; `countNotes` of A's `rebindFailNote` text is 0. Second case: the later beat names `model-a` again — A's line is re-placed once.
- `TestRestoreRePlacesFirstContactLines` passes unchanged.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRestoreDropsASupersededFirstContactRefusal|TestRestoreRePlacesFirstContactLines|TestFirstContactNotesStayOutOfAResumedRecord|TestRebind' ./internal/tui/`
- The new test fails against the pre-item tree.

**Commit:** `fix(tui): a later rebind refusal drops the stashed first-contact refusal`

## 2. A cold-start outage and its first landing are display-only

Depends on item 1.

**What:**
**Goal:** fixes `offline-notes-repeat` (a defect, not a regression). The `server offline — …` note a session raises before any beat has landed (and not after a `/server` switch) is an ephemeral line of its own. Every note the first landed beat after it raises — `connected: …`, `server back online`, an unknown window, a refused rebind, a roster note, an effort note — is an ephemeral line of its own, worded in full (never the quiet first-contact seed); every one of them but `server back online` joins the stash under item 1's rules. An offline note after a beat has landed, or after a `/server` switch, and every note of the beats after the first landing stay persisted lines. Launch → resume → launch with the server down leaves no `server offline — …` in the record and one in the view.
**Approach (assumed at the header base):** add a `contact` value in `internal/tui/startupchrome.go`, `contactColdRecovery` — the first landed beat after a cold-start failure. `Model.foldBeat` captures, before its resets, `coldFailed := !m.hb.everOnline && m.hb.failures > 0 && !m.hb.switched` beside the existing `firstContact`, and stamps the intent's contact through `Model.observeBinding` (pass the contact, not the bool); its `kindOnline` note uses the same contact. `Model.foldBeatFailure` raises the offline note with `contactFirst` when `!m.hb.everOnline && !m.hb.switched`, else `contactEvent`. `destination`: only `contactFirst` joins the open log (`contactColdRecovery` never does); a `sourceBeat` note is persisted only for `contactEvent` and `contactSettle`. In `Model.applyRebind`, `quiet` is true only for `contactFirst` and `contactSettle`; the success-path stash clear and item 1's refusal drop treat `contactColdRecovery` like any later contact (clear first, then its own lines stash). `Model.noteBinding` stashes a line for `contactFirst` or `contactColdRecovery`. The offline note does not go through `noteBinding` and is never stashed (ratified). Extends the chrome and the heartbeat fold; no new module.
Prose guard: rewrite every comment and doc sentence that lists which beat notes are ephemeral or says an offline / `connected:` note is persisted. Find them with `grep -rn -iE 'ephemeral|persisted|not saved|first.contact|first answers|offline' internal/tui/*.go .capcom/knowledge/glossary/glossary-identity/session.md .capcom/knowledge/glossary/glossary-identity/session-log.md docs/manual/sessions.md layout.md` (non-test files; `transcript.addEphemeralNote`'s comment and the manual's first-answer-notes paragraph are among them); the `session.md` ephemeral list gains the cold-start offline note and the first landing's notes.
**Regression guard.** the offline note's shipped wording is `server offline — <failure>` (`offlineNote`, internal/tui/heartbeat.go; bare `server offline` with no failure) — replace every `offline: …` mention in the plan (header Goal, the ratified "Offline line after a swap" call, item 2's Goal, Approach and Tests) with `server offline — …`, and tests match the note on the `server offline` prefix, never `offline:`.
Also binding: the record test launches from `unbound(testOpts)` through `wireRebind`, so its landing writes `connected: …` (a bound `test-model` launch writes none); the prose grep covers every non-test `internal/tui/*.go` file plus the glossary and manual files with `not saved` and `first answers` in its pattern; `server back online` (raised through `m.note`, never `noteBinding`) is ephemeral but not stashed, like the offline line.
**Files:** internal/tui/startupchrome.go, internal/tui/startupchrome_test.go, internal/tui/heartbeat.go, internal/tui/heartbeat_test.go, internal/tui/model_test.go, internal/tui/transcript.go, .capcom/knowledge/glossary/glossary-identity/session.md, docs/manual/sessions.md
**Read first:** internal/tui/heartbeat.go — Model.foldBeat, Model.foldBeatFailure, Model.applyRebind, Model.noteBinding, offlineNote; internal/tui/startupchrome.go — contact, destination;
internal/tui/heartbeat_test.go — unbound, downBeat; internal/tui/model_test.go — TestFirstContactNotesStayOutOfAResumedRecord (launch/saved helpers)
**Closes:** offline-notes-repeat
**Tests:**
- `TestStartupChromeDestination` rows for `contactColdRecovery`: routine and loud `sourceBeat` kinds, log open and closed → `destLineEphemeral`.
- New `TestColdStartOfflineStaysOutOfAResumedRecord` in `internal/tui/model_test.go`: launch a resumed session (`opts.Resumed` on `unbound(testOpts)`, built through `wireRebind`), fold `downBeat`, snapshot via `snapshotPayload`; relaunch from that blob, fold `downBeat` again; assert the blob carries no `server offline` text (match `offlineNote(failure)` / the `server offline` prefix, never `offline:`) and the view shows it once. Then fold `upBeat`: `connected: …` shown once, ephemeral, absent from the record.
- A mid-session outage (`upBeat`, then `downBeat` × `offlineFailureThreshold`, then `upBeat`) still saves the offline note and, if the model moved, the change note; a `/server` switch's cold failure still saves its offline note.
- `TestColdStartFailureIsOfflineImmediately`, `TestRebindFirstContactFailureRecovers` and `TestE2EColdStartHeartbeat` pass, adjusted only where they pin persistence.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestColdStartOfflineStaysOutOfAResumedRecord|TestStartupChromeDestination|TestColdStart|TestOffline|TestRebind|TestFirstContact|TestRestoreRePlacesFirstContactLines|TestRestoreDropsASupersededFirstContactRefusal|TestE2EColdStartHeartbeat' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestLoudKindsNamedInDocs|TestNoticeKindClasses' ./internal/tui/`
- The new record test fails against the pre-item tree.

**Commit:** `fix(tui): a cold-start outage and its first landing are not saved to the session record`
