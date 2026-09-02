# Open defects — tok/s, cancelled-reply order, routing-notice flap

**Goal:** close the three open `[ ]` defects in `ISSUES.md` "Open defects": the absurd `tok/s`
reading, the next prompt landing above a cancelled partial reply, and the alternating
`sub-agents: … unavailable` / `routing to …` notices.
**Date:** 2026-09-02 · **Status:** unexecuted · **Base:** `cd2bd71f`
**Sized for:** ~200k-context host.

**Sources:** `ISSUES.md:28,32,34-40`; `internal/tui/fold.go:132-172`; `internal/tui/model.go:1872-1879`;
`internal/tui/transcript.go:615-627,1160-1172`; `cmd/apogee/delegation.go:439-470,499-522,748-765`;
`internal/heartbeat/heartbeat.go:50-70,145-150`; `internal/provider/discovery.go:250-252`;
`internal/tui/heartbeat.go:123-130,634-648` (ADR 0024 D7); ADR 0045 §4; ADR 0042.

**Ratified design calls** (owner, 2026-09-02):
- **tok/s:** fix the measurement, no display clamp — the clock starts on the first depth-0 output event (reasoning OR visible token); a window under 250 ms yields no reading (suffix hidden).
- **Cancel:** the streamed partial is committed as a plain assistant entry placed before the `· cancelled` note and persisted in the session record, so a resume shows the screen's order.
- **Routing debounce:** the NOTICE is what waits — two consecutive unusable beats flip a routed server's notice to unavailable, and the first usable beat flips it back. The engine push and the fallback are UNCHANGED: every unusable beat still pushes nil under the lock, so a spawn in that window still falls back to the session server. Beats keep landing mid-Exchange (ADR 0045 never-idle-gated rule stands); cold start still announces on the first unusable beat; notices stay change-only.
- **Probe:** an HTTP 429 on the routed server's `/v1/models` is neither failure nor success — the last verdict is kept. Timeouts, 5xx and connection errors count toward the threshold; 401/403 stay unusable.

**Regression check (2026-09-02, cd2bd71f):**
- 1: guard folded — `TestFoldStatsSkipsAMaintenanceReadingForTheGaugeAndClock` and the depth-0 `ReasoningEvent` variant row become edited tests; the sub-floor test measures over `-10*time.Second`.
- 3: recast — owner call 2026-09-02 drops the in-Exchange suspension; the debounce is the counter alone; yields to ADR 0045 line 60 and `cmd/apogee/delegation.go:414-417` (never idle-gated — beats land mid-Exchange).
- 3: recast again (re-check round) — owner call 2026-09-02 debounces the NOTICE, not the latch: every unusable beat still pushes nil under the lock and only `stateChange` is gated, so `CONTEXT.md:244` and ADR 0045 §4:67 ("an unusable target is not an error: the spawn falls back to the parent's Upstream") stand untouched, nothing is superseded and `CONTEXT.md` enters no Files line. `TestDelegationWiringObservePushesWhatTheBeatResolvedTo` is left unedited as a pin on the unconditional push; `TestDelegationTwoUnusableBeatsUnroute` now asserts per beat (no notice + nil push after the first down, the `unavailable` line after the second), which is what gives it bite against Base.
- 4: guard folded — a provider error type wraps the `*StatusError` so the discovery text carries one `apogee:` prefix. Re-check round: kept as amended, with one sentence added to **What** naming the distinction from item 3 — a 429 means the server answered, so the beat is not landed and the latch keeps the last verdict, while a timeout/5xx/connection error still pushes nil at once and only its notice is debounced.
- 5: guard folded (writer's decision) — the ADR 0045 §4 amendment names the line-60 sentence and states the threshold does not idle-gate beats; "suspended during an Exchange" dropped. Re-check round: the amendment covers the NOTICE rule only and must also state that the engine push and the "unusable ⇒ the spawn falls back" rule (§4, `CONTEXT.md:244`) are NOT changed by the debounce. `ISSUES.md` is uncommitted at Base `cd2bd71f` (there the entries sit at `:28`, `:30` as `[ ]`, `:32-38`, with no `reposnding` entry), so item 5's `ISSUES.md` locators (`:28`, `:32`, `:34-40`, the `[P]` gauge line) describe the working tree, not the Base.

**Standing requirements:** `skills: coding-standards`. A deviation from item text lands as a dated NOTES line under the item. Never edit `VERSION` / `CHANGELOG.md` release headings.

**Out of scope:** the `[P]` gauge-background defect (plan `2026-09-02 - 01`); a display ceiling on `tok/s`; the session server's own heartbeat classification of 429 (`internal/tui/heartbeat.go`); lengthening `discoveryTimeout`; a distinct entry kind or styling for a cancelled partial; sub-agent (depth ≥ 1) pending buffers at cancel.

---

## 1. tok/s: time the whole generation and refuse a sub-floor window — ✅ DONE (2026-09-02)

**What.** Fixes `ISSUES.md:28` (defect: `'1514218 tok/s'`). In `foldStats` (`internal/tui/fold.go:132-172`):
- Start `genStart` on the first depth-0 `domain.ReasoningEvent` as well as the first depth-0 `TokenEvent` (`fold.go:135-137`): the server's `completion_tokens` counts reasoning, so the clock must too.
- Replace the `secs > 0` guard (`fold.go:167`) with a named constant `throughputWindowFloor = 250 * time.Millisecond`: an elapsed window below it sets `m.tokPerSec = 0` (the suffix at `model.go:3176-3181` then hides itself) instead of dividing. No clamp in `throughputSuffix`.
- Update the `liveStats` doc (`model.go:475-478`) and the `throughputSuffix` comment to say the reading is completion tokens over the window from the first output event, and that sub-floor windows read as unmeasured.
- Remove the `time.Sleep(2 * time.Millisecond)` workaround in `internal/tui/model_test.go:304-305`; drive the clock by setting `m.genStart` directly (`time.Now().Add(-time.Second)` for a measured window, `time.Now()` for a sub-floor one) — no sleeps.

**Regression guard.** Three tests are edited, not left "green": (a) `TestFoldStatsSkipsAMaintenanceReadingForTheGaugeAndClock` (`internal/tui/fold_test.go:499-532`) folds its token and its final `mainUsage` microseconds apart, so the floor reads 0 where `:530` asserts `> 0` — set `m.genStart = time.Now().Add(-time.Second)` before the `:526` fold (the `:520` non-zero check still holds); (b) the variant-table row "ReasoningEvent is activity plus retention" (`fold_test.go:82-89`) gets `wantStats: statsFold{genStarted: true}` plus a new Depth-1 `ReasoningEvent` row with none, the way the `TokenEvent` rows at `:64-77` state it — the runner compares `statsOf(m)` at `:356`; (c) `TestFoldStatsSubFloorWindowReadsUnmeasured` measures 100 tokens over `-10*time.Second` and expects `[9, 11]` — over `-time.Second` a ≥112 ms stall (a `-race` run) reads 89.x and fails.

**Files:** `internal/tui/fold.go`, `internal/tui/model.go`, `internal/tui/fold_test.go`, `internal/tui/model_test.go`

**Tests** (`internal/tui`):
- `TestFoldStatsReasoningStartsTheGenerationClock` — a depth-0 `ReasoningEvent` sets `genStarted`; a depth-1 one does not.
- `TestFoldStatsSubFloorWindowReadsUnmeasured` — `genStart = time.Now()` then a `UsageEvent{CompletionTokens: 100}` leaves `tokPerSec == 0` and the suffix empty; `genStart = time.Now().Add(-10*time.Second)` gives `tokPerSec` in `[9, 11]`.
- Variant table (`fold_test.go:82-89`): the depth-0 `ReasoningEvent` row states `wantStats: statsFold{genStarted: true}`; a new Depth-1 `ReasoningEvent` row states none.
- `TestFoldStatsSkipsAMaintenanceReadingForTheGaugeAndClock` (`fold_test.go:499-532`) is edited: `m.genStart = time.Now().Add(-time.Second)` before its final `mainUsage` fold, then its `tokPerSec > 0` assertion holds under the floor.
- Existing `model_test.go:303-317`, `model_test.go:342-350` stay green without the sleep.

**Acceptance:**
```
go build ./... && go test ./internal/tui/ -run 'FoldStats|Throughput|Stats' -count=1
```
**Commit:** `fix(tui): time tok/s from the first output event and hide sub-floor windows`

---

## 2. Cancel: commit the streamed partial before the cancelled note — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's whitespace-only case is covered in `model_test.go` as a subtest of
`TestCancelCommitsThePartialBeforeTheNote`; `transcript_test.go` carries the transcript-level
sibling `TestCommitCancelledRecoversAParkedTopLevelPartial` (a delegate streaming at depth 1 parks
the parent's buffer, and the cancel must still commit it) — the case only the transcript level can
reach, and what puts that file on the Files list.
NOTES (2026-09-02): consequential edit — cmd/apogee/e2e_stream_test.go: made necessary by the
commit; the pre-existing comment at the cancel's idle wait said the note "is written ABOVE the text
that had arrived", which the fix makes false, so it was reworded alongside the `:167-176` comment
the item names.

**What.** Fixes `ISSUES.md:32` (defect: the next prompt renders above the cancelled partial). In `Model.foldCancelled` (`internal/tui/model.go:1872-1879`), before `addNote("cancelled")`: take the top-level pending buffer — `text := trimBlankLines(m.transcript.takePending(runRef{}))` — and when non-empty `place(entry{kind: entryAssistant, text: text})`, the depth-0 twin of `closeRun` (`transcript.go:1160-1172`). Put the two lines in a `transcript.commitCancelled()` method beside `closeRun` so the buffer's join stays in `transcript.go`. Consequences that need no further code: the note then lands at the tail behind the entry (`internal/tui/doc.go:68-72`), the next `addUser` appends after both, and `encodeTranscript` (`transcriptbridge.go:42-56`) persists the entry with the note after it. Rewrite the `foldCancelled` doc comment (`model.go:1870-1871`) to state the partial is committed, not left as a preview.

Binding: plain `entryAssistant`, depth 0, no new kind, no marker text; the `· cancelled` note below it is the marking. Depth ≥ 1 buffers are untouched (out of scope).

**Files:** `internal/tui/model.go`, `internal/tui/transcript.go`, `internal/tui/transcript_test.go`, `internal/tui/model_test.go`, `cmd/apogee/e2e_stream_test.go`

**Tests:**
- `internal/tui`: `TestCancelCommitsThePartialBeforeTheNote` — fold depth-0 tokens `"Item 1.\nItem 2."`, fold `cancelledMsg{}`, then `addUser("next")`: entries end `[assistant "Item 1.\nItem 2.", note "cancelled", user "next"]`, `streaming == false`, `pending` empty. A whitespace-only buffer commits nothing.
- `cmd/apogee/e2e_stream_test.go` `TestE2EStreamCancelKeepsWhatArrived` (`:132-204`): replace the "record holds exactly one answer" assertion (`:181-192`) and its comment (`:167-176`) — the record now holds TWO reply entries, the first containing `Item 1.` and no line ≥ `streamLines`, the second exactly `Nothing else to add.`; the frame after the second reply shows the `cancelled` note BELOW the last kept stream row and the `Anything else?` prompt below the note (assert row order on the frame text).

**Acceptance:**
```
go build ./... && go test ./internal/tui/ -run 'Cancel' -count=1 && go test ./cmd/apogee/ -run 'TestE2EStreamCancel' -count=1
```
**Commit:** `fix(tui): commit a cancelled partial reply before the cancelled note`

---

## 3. Routing notices: debounce the unusable verdict like the session heartbeat — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the existing `TestDelegationNoticesOnlyOnARoutingStateChange` (up, up, down, down, up) keeps its exact notice list as the item says; only its two per-beat trailing comments moved to name which `down` now does the flip.

**What.** Recast at the regression check (2026-09-02). Fixes `ISSUES.md:34-40` (defect: notices alternate every beat). `delegationWiring` (`cmd/apogee/delegation.go:133`) gains `failures int`. New constant `delegationFailureThreshold = 2` with a comment citing ADR 0024 D7 and `offlineFailureThreshold`. What the counter gates is the NOTICE alone, never the latch: on every beat `land` still performs the engine push `d.engine.SetDelegationTarget(target)` under the lock exactly as today — nil on an unusable beat — so a spawn in that window still falls back to the session server with `seatFallbackNote` (`internal/agent/subagent.go:596`). In `land` (`:439`), for an UNUSABLE beat only — `target == nil && keyErr == nil && d.missingNotice == ""`:
- `failures++`; while `d.routed && failures < delegationFailureThreshold` → skip the `stateChange` call and leave `d.routed` / `d.stated` unmoved, so no notice goes out (the nil push still lands).
- at or above the threshold `stateChange` runs, flips `d.routed` to false and emits the `unavailable` line once.
A usable beat resets `failures = 0` and lands as today. Beats keep landing mid-Exchange: no `inExchange` field, no `InExchange()` on `delegationSetter`, no engine read from the beat goroutine. Cold start (`!d.stated`) is unchanged: the first unusable beat is announced (ADR 0042, `TestDelegationNoticesTheFirstStateEvenWhenItIsUnusable`). Key refusals and the missing-entry notice bypass the counter (facts about config, not the network). The generation check stays first.

**Regression guard.** Owner call 2026-09-02 (re-check round) — DEBOUNCE THE NOTICE, NOT THE LATCH. On an unusable beat (`target == nil && keyErr == nil && d.missingNotice == ""`) `land` always performs the engine push `d.engine.SetDelegationTarget(nil)` under the lock exactly as today, so a spawn in that window still falls back to the session server with `seatFallbackNote` and `CONTEXT.md:244` + ADR 0045 §4 (`docs/adr/0045-…:67`, "an unusable target is not an error: the spawn falls back to the parent's Upstream") stand UNTOUCHED — no supersession, `CONTEXT.md` stays out of every Files line. What the counter gates is ONLY the notice: `failures++`, and while `d.routed && failures < delegationFailureThreshold` skip the `stateChange` call and leave `d.routed` / `d.stated` unmoved, so no notice goes out; at or above the threshold `stateChange` runs and flips `d.routed` to false, emitting `unavailable` once. A usable beat resets `failures = 0` and lands as today. Cold start (`!d.stated`) still announces the first unusable beat. Key refusals and the missing-entry notice bypass the counter. This supersedes the first round's guard, and the header's "Routing debounce" line now reads that the NOTICE is what waits for the second consecutive unusable beat while the engine push and the fallback are unchanged.

The in-Exchange half of that ratified call stays dropped: no `inExchange` field, no `InExchange()` on `delegationSetter`, no engine read from the beat goroutine, and `wire_helpers_test.go` leaves the Files line; `TestDelegationUnusableBeatsAreIgnoredInExchange` is removed from Tests. The item yields to the documented decision at ADR 0045 line 60 ("never idle-gated — beats land mid-Exchange") and `cmd/apogee/delegation.go:414-417`: that rule stays TRUE under this item, and item 5's amendment must say so explicitly.

**Files:** `cmd/apogee/delegation.go`, `cmd/apogee/delegation_test.go`, `cmd/apogee/e2e_seat_test.go`

**Tests** (`cmd/apogee`, beside `TestDelegationNoticesOnlyOnARoutingStateChange` `:613`):
- `TestDelegationOneUnusableBeatDoesNotUnroute` — kept, with its intent restated as *does not NOTICE*: up, down, up → after the single down beat no new notice has gone out AND the engine spy still received that beat's nil push; the final notice list is the single `routing to` line (+ dialect advice).
- `TestDelegationTwoUnusableBeatsUnroute` — asserted PER BEAT rather than over an accumulated list, which is what gives it bite against Base (where the notice fires on the first down): up; after the first down beat → no new notice and the engine received the nil push; after the second down beat → exactly the `unavailable` line; then up → `routing to` once.
- `TestDelegationWiringObservePushesWhatTheBeatResolvedTo` (`delegation_test.go:415-435`) is NOT edited: the nil push still lands after one down beat, so the test is unaffected and must stay green as a pin on the unconditional push.
- Existing `:613` sequence (up, up, down, down, up) keeps its exact notice list with the second `down` doing the flip.
- `TestE2ESeatDelegationsLineSurvivesATargetDownBeat` (`e2e_seat_test.go:215-232`): widen the wait to `tuitest.Within(3*heartbeat.Interval)` and say why (two beats must fail).

**Acceptance:**
```
go build ./... && go test ./cmd/apogee/ -run 'TestDelegation' -count=1 && go test ./cmd/apogee/ -run 'TestE2ESeat' -count=1
```
**Commit:** `fix(delegation): debounce the sub-agents unavailable notice over two beats`

---

## 4. Probe: an HTTP 429 keeps the last routing verdict — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the re-run grep for `model discovery: upstream HTTP` / `discovery: upstream` across `*_test.go` found no pinned copy — only the plan document and two archived plans mention the old text, so no pin needed fixing.

**What.** Depends on item 3. `provider.Discover`'s non-200 branch (`internal/provider/discovery.go:250-252`) returns `fmt.Errorf("apogee: model discovery: %w", &StatusError{Code: resp.StatusCode})` so callers branch with `errors.As`. `heartbeat.Beat` (`internal/heartbeat/heartbeat.go:52`) gains `Throttled bool`, set by `Monitor.Beat` (`:145-150`) when `errors.As(err, &se) && se.Code == http.StatusTooManyRequests`; `Reachable` stays false and `Failure` keeps the text. In `delegationWiring.observe` (`delegation.go:404-407`) a `Throttled` beat is not landed at all — no push, no notice, `failures` untouched — with a comment stating the call: a rate-limited list is silence, not a verdict. A cold-start 429 therefore says nothing until a non-429 beat lands. This differs in KIND from item 3's debounce: a 429 means the server ANSWERED, so the beat is not landed at all and the routed target stays latched (the last verdict), whereas a timeout, 5xx or connection error still pushes nil immediately under item 3's rule and only its NOTICE is debounced. The session heartbeat (`internal/tui/heartbeat.go`) is NOT changed (out of scope).

The wrapped error's text changes from `apogee: model discovery: upstream HTTP 429` to the `StatusError` rendering; grep `model discovery: upstream HTTP` and `discovery: upstream` across `*_test.go` found no pinned copy at write time — re-run the grep and fix any pin found.

**Regression guard.** `StatusError.Error()` already renders `apogee: upstream HTTP 429 Too Many Requests` (`internal/provider/client.go:89-91,104-112`), so `fmt.Errorf("apogee: model discovery: %w", &StatusError{…})` would print `apogee: model discovery: apogee: upstream HTTP 429 …` wherever the failure text surfaces (the offline note at `internal/tui/heartbeat.go:142`, the probe report at `internal/probe/host.go:178`). Return a small provider error type instead — `Error()` is `apogee: model discovery: upstream HTTP <code> <text>`, `Unwrap()` returns the `*StatusError` — so `errors.As` works without the second `apogee:`.

**Files:** `internal/provider/discovery.go`, `internal/provider/discovery_test.go`, `internal/heartbeat/heartbeat.go`, `internal/heartbeat/heartbeat_test.go`, `cmd/apogee/delegation.go`, `cmd/apogee/delegation_test.go`

**Tests:**
- `internal/provider`: `TestDiscoverNon200IsAStatusError` — an `httptest` server answering 429 yields an error `errors.As`-able to `*StatusError` with `Code == 429`, whose `Error()` is exactly `apogee: model discovery: upstream HTTP 429 Too Many Requests` (one `apogee:` prefix).
- `internal/heartbeat`: `TestBeatMarksA429Throttled` — 429 → `Reachable false, Throttled true`; 503 → `Throttled false`.
- `cmd/apogee`: `TestDelegationThrottledBeatKeepsTheLastVerdict` — up, throttled×3 → no notice and the target stays latched; down, down → `unavailable`; throttled → still nothing; up → `routing to`.

**Acceptance:**
```
go build ./... && go test ./internal/provider/ ./internal/heartbeat/ -count=1 && go test ./cmd/apogee/ -run 'TestDelegation' -count=1
```
**Commit:** `fix(heartbeat): a 429 on the routed server's model list keeps the last routing verdict`

---

## 5. Register and ADR: close the three entries, amend ADR 0045 §4 — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's "leaving the `[P]` gauge line" describes a line that no longer
exists in the working tree — the Open defects section's remaining `[ ]` load_skill entry was
left untouched instead.
NOTES (2026-09-02): docs-only item, so the sanity check is the item's own acceptance command
rather than a compile.

**What.** Depends on items 1–4. Remove the three `[ ]` lines from `ISSUES.md` "Open defects" (`:28`, `:32`, `:34-40`), leaving the `[P]` gauge line. Amend ADR 0045 §4 (`docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md`) with a dated paragraph: the unusable NOTICE is debounced over `delegationFailureThreshold` consecutive beats, mirroring ADR 0024 D7, and a 429 is silence; the notice remains one per state change. Add a one-line cross-reference under ADR 0024 D7 pointing at the amendment. Docs-only; no code.

**Regression guard.** The ADR 0045 §4 amendment covers the NOTICE rule ONLY. It names the line-60 sentence ("never idle-gated — beats land mid-Exchange") and states the threshold does not idle-gate beats (they still land mid-Exchange; only the NOTICE of the flip to unavailable waits for the second consecutive unusable beat); "suspended during an Exchange" is dropped from this item's wording. It must additionally state that the engine push and the "an unusable target is not an error: the spawn falls back to the parent's Upstream" rule (§4 itself, `CONTEXT.md:244`) are NOT changed by the debounce — every unusable beat still pushes nil, so a spawn in the debounce window still falls back. `ISSUES.md` is uncommitted at Base `cd2bd71f`, so this item's `ISSUES.md` locators describe the working tree (see the header's regression-check block).

**Files:** `ISSUES.md`, `docs/adr/0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md`, `docs/adr/0024-*.md`

**Tests:** none (docs).

**Acceptance:**
```
! grep -n "1514218 tok/s\|reposnding\|OR-deepseek unavailable" ISSUES.md && grep -n "delegationFailureThreshold" docs/adr/0045-*.md
```
**Commit:** `docs(adr): record the routing-notice debounce; close three register defects`

---

**Suggested version bump:** micro (three user-visible defect fixes) — the owner decides.
