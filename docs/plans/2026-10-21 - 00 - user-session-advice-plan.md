# User session advice — plan

**Goal:** A user or adopted repo `advise:` entry (argv or webhook) on `session-started` reaches the model through the shipped session-start slot on every Driver, so `advise: ["capcom", "prime"]` briefs a session as Claude Code's SessionStart hook does. A new `compaction-finished` notice lets the same entry re-brief the model after every fold. The lane's three open defects are fixed on the way.
**Date:** 2026-10-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** ae50b5bc
**Closes:** session-start-advise; session-advise-admission-arm; compaction-rebrief

**Sources:**
- ADR 0076 (`capcom know show "ADR 0076"`): D1, D5, D6, D7, D8; amendments `adr-0076-amend-1019`, `adr-0076-amend-1021`, `adr-0076-amend-1021b`
- ADR 0018 D4, D6 + `adr-0018-amend-1021`; ADR 0104 (adopted repo reactions); ADR 0031 (Driver sufficiency)
- Glossary: Moment, Reaction, Advice span, Compaction, Session boundary, Bypass mode, Firing
- Archived plan `docs/plans/archived/2026-10-19 - 00 - session-started-reactions-plan.md` (built the slot and lane)
- Issues `session-start-advise`, `compaction-rebrief`, `session-advise-timeout-replay`, `session-window-stays-open`, `session-window-wake-test`

**Ratified design calls** (owner, 2026-10-21):
- **Admission:** an admission arm gates only what apogee ships on; a user cell opens on a slot that is role-safe, prefix-preserving, fenced, capped, ephemeral and off under Bypass (`adr-0076-amend-1021`). User `advise:` on `session-started` is admitted; the "pending admission" refusal is retired.
- **Lane defects:** the three open session-start lane issues are fixed in this plan.
- **Compaction:** a new notice `compaction-finished`, fired once after every fold that ran (estimate, emergency, `/compact`), top-level agent only, payload `fold: auto | emergency | manual`, env `APOGEE_REACTION_FOLD`; published through `ObserveRunner`, no new Event variant (`adr-0076-amend-1021b`).
- **Compaction slot:** advise runs start when the fold begins and are joined after the `Replace`; the answer rides fenced ahead of the next user-role message's text (the opening, or the overflow bridge mid-Exchange) — never the summary, never the system prompt.
- **Stale span:** an attaching `compaction-finished` span strips the same reaction id's earlier spans from the protected prefix's first user message; other reactions' spans, and all spans on a failed run, stay.
- **Doc example:** the manual shows `advise: ["capcom", "prime"]` as the worked example beside a generic one.

**Standing requirements:**
- skills: coding-standards
- Pi box: never `go test -race` over a package pattern; one package + `-run`, `GOMEMLIMIT=2GiB`; never two test runs at once. Pass this to every sub-agent.
- Bubble Tea v2 names only (`tea.KeyPressMsg`).
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The tail slot for `pre-request` / `post-response` / `history-rewrite` (issue `non-tool-advise-slot`).
- Stamping `schedule` on the sync lane's seam document (pre-existing for post-tool-result too).
- capcom's own `init` support for writing the apogee entry (capcom repo).
- An apogee-sim arm for either slot (welcome evidence, not a gate).

**Regression check (2026-10-21, ae50b5bc):**
- 1: recast — every run reported at the join is dropped there, not only a timed-out one
- 2: guard folded
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 6: guard folded; supersedes the "an Agent holding no Runner" doc comments on `sessionWindow` (internal/agent/agent.go) and `announceSessionStart` (internal/agent/construct.go)
- 7: guard folded
- 8: guard folded
- 9: guard folded
- 10: guard folded
- 11: guard folded
- 12: guard folded
- 13: guard folded
- 14: guard folded
- 1 (re-check): guard folded — the abort-puts-back comments (loop.go join site) name only runs that delivered advice

## 1. A timed-out session-start run is never joined again — ✅ DONE (2026-10-21)

NOTES (2026-10-21): consequential edit — internal/agent/construct.go: made necessary by restoreDelivered now restoring only the runs that answered (exchangeAborted's doc comment said the opening's session-start advice is put back wholesale)
NOTES (2026-10-21): a run Bypass skipped at the join is neither reported nor attached, so it still counts as taken into the Exchange and an abort restores it, as before

**What:**
Recast at the regression check (2026-10-21).
**Goal:** A session-start advise run reported at a join — timed out, errored or panicked — is never reported again nor attached later, whether or not that Exchange aborts.
**Approach (assumed at the header base):** in `internal/agent/sessionadvise.go`, `joinSessionAdvice` reports a deadline miss but `take` still moves the run into `delivered`, and `restoreDelivered` puts it back ahead of `pending` on an abort. Every run reported at the join (timed out, errored or panicked) is dropped there, so `restoreDelivered` restores only runs that delivered an answer.
**Regression guard.** every run reported at the join — timed out, errored or panicked — is dropped there; restoreDelivered restores only runs that delivered an answer (the Goal widens to "a run reported at a join is never reported again nor attached later"), and the test also drives an errored run through an abort.
Every comment saying an abort puts the session-start runs back — the join-site comment in `internal/agent/loop.go` (step opening block) included; find them with `grep -n 'abort.*back\|restoreDelivered' internal/agent/{loop,construct,sessionadvise}.go` — names only runs that delivered advice.
**Files:** internal/agent/sessionadvise.go, internal/agent/sessionadvise_test.go, internal/agent/loop.go
**Read first:** internal/agent/sessionadvise.go — joinSessionAdvice, sessionAdviseLane.take, sessionAdviseLane.restoreDelivered, waitForRun; internal/agent/construct.go — exchangeAborted;
internal/agent/loop.go — step opening block (joinSessionAdvice call, ctx.Err cancel exit);
internal/agent/sessionadvise_test.go — TestSessionAdvise_ATimeoutAddsNothingAndIsReported, TestSessionAdvise_AnAbortedFirstExchangeKeepsTheAdvice
**Tests:** a `TestSessionAdvise_*` test drives a timeout, an aborted Exchange (`exchangeAborted`) and the next opening; pins one timeout report and no fenced advice in the next request, including when the handler finishes in between; a second drives an errored run through an abort and pins one failure report and nothing attached at the next opening.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionAdvise' ./internal/agent/`
**Closes:** session-advise-timeout-replay
**Commit:** `fix(agent): a timed-out session-start advise run never replays after an abort`

## 2. The session-started window shuts on every resumed conversation — ✅ DONE (2026-10-21)

NOTES (2026-10-21): re-derived from "the Step path" living in agent.go — both the exported Step and a top-level Run (internal/run drives Run) continue through the unexported step() in internal/agent/loop.go, so the shut sits at the top of step() rather than in Step alone; agent.go carries only the sessionWindow / Agent.window doc updates, and background.go (Wake) needed no change.
NOTES (2026-10-21): the resume test covers both resumed shapes — a snapshot restored mid-Exchange (InExchange) and one with its input still queued (PendingInput) — as subtests; the Wake test pins existing behaviour (it passes on the base tree), the resume test fails there.

**What:**
**Goal:** A session resumed mid-Exchange that continues through `Step` shuts its pre-first-prompt window, so a `session-started` entry armed afterwards waits for the next boundary; a test pins that a `Wake` that queues held background notes shuts the window too.
**Approach (assumed at the header base):** the agent's `sessionWindow` (`internal/agent/agent.go`) is shut by `Submit` and `Wake` (`background.go`); the `Step` path that continues a restored mid-Exchange conversation does not shut it. Shut it where that path resumes the conversation. The wake test holds background notes, Wakes the Agent so they queue, then arms a `session-started` entry by a live `SetReactions` swap.
**Regression guard.** The existing window tests live in `internal/agent/reactions_test.go` as `TestSessionWindow_*` and a shut moved into `Step` can break them, so the acceptance regex is `TestSessionStart|TestSessionWindow`. The new tests reuse `windowAgent` / `armObserve` / `sessionStartedEntry` (reactions_test.go) and build the mid-Exchange snapshot with `cutFixtureSession(agentState{InExchange, PendingInput})` (state_test.go).
**Files:** internal/agent/agent.go, internal/agent/background.go, internal/agent/sessionstart_test.go
**Read first:** internal/agent/agent.go — Submit, Step, RestoreSession, sessionWindow.shut; internal/agent/background.go — Wake;
internal/agent/reactions_test.go — windowAgent, TestSessionWindow_AnEntryArmedAfterTheFirstPromptWaitsForTheNextBoundary; internal/agent/state_test.go — cutFixtureSession
**Tests:** resume-and-`Step` then live-arm: no fire for the current session; hold-notes-`Wake` then live-arm: no fire; every `TestSessionWindow_*` stays green.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionStart|TestSessionWindow' ./internal/agent/`
**Closes:** session-window-stays-open; session-window-wake-test
**Commit:** `fix(agent): a resumed mid-Exchange session shuts its session-started window`

## 3. `advise:` on `session-started` is admitted — ✅ DONE (2026-10-21)

NOTES (2026-10-21): the config load case is a new test, TestLoadFileConfigAdmitsSessionStartedAdvice (argv `run:`+`advise:` entry plus a webhook `advise:` entry on session-started, both lanes checked), rather than a row in the refusal table
NOTES (2026-10-21): the ArgvHandler / WebhookHandler doc comments now say an advise answer at session-started becomes the session-start advice rather than the fenced trailer; TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler's doc comment widened and rewrapped the same way
NOTES (2026-10-21): docs/manual/reactions.md (on: key row line 87, and the "session-started is not one of them yet" paragraph near line 364) still quotes the retired pending-admission refusal and the old advise sentence — left to item 13, which owns that prose and whose Goal requires no sentence naming the admission refusal
NOTES (2026-10-21): pre-existing — the same test's doc comment says "a webhook stays observe-only", which ADR 0076 D2 / apogee-1d8 made stale (webhooks serve all three user classes); left as is

**What:**
**Goal:** A `reactions:` entry with `advise:` (argv or webhook) and `on: [session-started]` loads; an entry spelling both `run:` and `advise:` on `[session-started]` loads; the advise refusal for any other non-advise Moment names all three Moments it takes: `advise: reacts at post-tool-result, file-changed or session-started; %q is none of them`.
**Approach (assumed at the header base):** delete the `MomentSessionStarted` "pending admission" branch in `Reaction.Validate` (`internal/domain/reaction.go`) and widen the generic advise sentence. Update the `entryReactions` doc comment in `internal/config/reactions.go` ("the only combined entry that validates …"). Rewrite every test pinning either sentence (`internal/domain/reaction_test.go`, `internal/config/reactions_test.go`) to the new behaviour: session-started accepted, the widened sentence byte-pinned.
**Regression guard.** Every Go comment naming advise's Moments follows, not only `entryReactions`: find them by `grep -rn 'post-tool-result or file-changed' --include=*.go internal cmd` (today it also hits the `ArgvHandler` / `WebhookHandler` doc comments in internal/domain/reaction.go); `cmd/apogee/probecontext.go`'s hit belongs to item 5.
**Files:** internal/domain/reaction.go, internal/domain/reaction_test.go, internal/config/reactions.go, internal/config/reactions_test.go
**Read first:** internal/domain/reaction.go — Reaction.Validate, ArgvHandler, WebhookHandler; internal/domain/reaction_test.go — TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler;
internal/config/reactions.go — reactionConfig.entryReactions; internal/config/reactions_test.go — TestLoadFileConfigRefusesMalformedReactions; internal/agent/sessionadvise.go — startSessionAdvice, callSessionAdvice
**Tests:** domain table: argv and webhook advise on session-started validate; config load: `run:`+`advise:` on `[session-started]` yields one observe and one sync reaction; the widened refusal byte-pinned.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReaction|Validate' ./internal/domain/` and `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Reaction' ./internal/config/`; `grep -rn 'post-tool-result or file-changed' --include=*.go internal cmd` finds only `cmd/apogee/probecontext.go` (item 5's).
**Commit:** `feat(reactions): admit advise: on session-started`

## 4. A user's session-start advice is booked with its text

**What:**
Depends on items 1 and 3.
**Goal:** A user argv or webhook advise on `session-started` lands fenced ahead of the first opening's text, redacted of configured secrets, capped with the marker, fails open with one reported failure, is skipped under Bypass, takes its own `timeout:` else the 10s advise default, and books a `ReactionFiredEvent` (action `advise`) whose Detail is the capped advice text (no fence header), byte-equal to what a post-tool-result firing books.
**Approach (assumed at the header base):** `callSessionAdvice` already dispatches `ArgvHandler` to `runSyncArgv` and `WebhookHandler` to `runSyncWebhook`; `bookFiring` at the join passes `run.out.Detail`, never the advice text. Fall back to the advice text as `fireLeg` in `internal/agent/reactions.go` does (share that fallback, do not restate it).
**Regression guard.** Sharing `fireLeg`'s `if out.Detail == "" { out.Detail = adv.text }` fallback edits `internal/agent/reactions.go`, so it is in **Files**. The booked Detail is `adv.text` — `CapAdvice`'d, no fence header (pinned byte-exact as `"token=[redacted]\n"` in advise_argv_test.go); no test asserts a fence header in it. The new tests are named `TestSessionAdvise*` so the acceptance regex reaches them.
**Files:** internal/agent/sessionadvise.go, internal/agent/reactions.go, internal/agent/sessionadvise_user_test.go
**Read first:** internal/agent/sessionadvise.go — attachSessionAdvice, callSessionAdvice; internal/agent/reactions.go — fireLeg, adviceOf, bookFiring;
internal/agent/syncexec.go — syncTimeout, runSyncArgv; internal/agent/advise_argv_test.go — TestAdviseArgvRedactsAConfiguredSecretFromTheTrailer
**Tests:** `TestSessionAdvise*` twins of `advise_argv_test.go` / `advise_webhook_test.go` (redaction, cap, fail-open, deadline, Bypass) driven at `session-started` through the lane; one pins the booked Detail byte-equal to the capped text with no fence header; one pins `APOGEE_REACTION_START` and the payload `start` the argv sees.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionAdvise' ./internal/agent/`
**Commit:** `feat(agent): a user session-start advise reaches the model and books its text`

## 5. Firings and the probe arm user reactions before the boundary

**What:**
Depends on item 4.
**Goal:** A headless run, a daemon run and a `/schedule` Firing carry a user session-start advise answer in their first request; `apogee probe context --live` measures it at Turn 1, and its doc comment no longer says user advise cannot precede Turn 1.
**Approach (assumed at the header base):** `run.Once` (`internal/run/run.go`) builds the Agent with `agent.New` — which announces the boundary and starts the lane — and only then calls `SetReactions(lanes)`. Arm first: construct with `Config.DeferSessionStart`, call `SetReactions`, then `AnnounceSessionStart` (the order `lateEngine.Bind` uses). Same for `newProbeAgent` / `measureTurn1` in `cmd/apogee/probecontext.go`.
**Regression guard.** Lanes never go into construction: `Config.Reactions` is the engine's own set a Driver must not write (`run.Spec.Generation`'s doc), and lanes armed there leave `Generation().Sync` empty (`TestProbeContextLiveArmsTheSyncLaneOnTheFirstAgentOnly` goes red); `Once` and `measureTurn1` set `cfg.DeferSessionStart`, call `SetReactions`, then `a.AnnounceSessionStart()`; the probe's offline estimate path is left as is. expected fences on a user-role message are built with AdviceSpan Leading: true (with `Moment: domain.MomentSessionStarted` and the Turn the join stamps); the Bypass twin asserts no `[advice` anywhere in the first request. The order test extends `recordingObserveRunner` with one ordered log (`"replace"`, `"started"`) and asserts Replace precedes SessionStarted; the tests reading `lists`/`starts` keep passing.
**Files:** internal/run/run.go, internal/run/run_test.go, cmd/apogee/probecontext.go, cmd/apogee/probecontext_test.go, cmd/apogee/e2e_reactions_test.go
**Read first:** internal/run/run.go — Once; internal/agent/construct.go — AnnounceSessionStart; cmd/apogee/probecontext.go — newProbeAgent, measureTurn1; internal/run/run_test.go — recordingObserveRunner;
cmd/apogee/e2e_reactions_test.go — TestE2EAdviseLandsAsTheTrailer, TestE2EAdviseIsOffUnderBypass; cmd/apogee/probecontext_test.go — TestProbeContextLiveArmsTheSyncLaneOnTheFirstAgentOnly
**Tests:** headless e2e twin of `TestE2EAdviseLandsAsTheTrailer`: an argv advise on `session-started` prints a fixed line; the stub upstream's first request's last user message starts with `domain.RenderAdvice`'s fence of it (span `Leading: true`, `Moment: domain.MomentSessionStarted`); `TestE2EAdviseIsOffUnderBypass` twin asserting no `[advice` in the first request; a run.Once unit test pins order (reactions armed before the announce) through `recordingObserveRunner`'s ordered log.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/run/` and `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EAdvise|ProbeContext' ./cmd/apogee/`
**Commit:** `feat(run): Firings arm user reactions before the session-started boundary`

## 6. An advise entry armed before the first prompt starts for this session

**What:**
Depends on items 2 and 4.
**Goal:** A `session-started` advise entry armed live before the session's first prompt — a repo entry adopted in the pane, in `/settings` or by `apogee project adopt`, or a global entry added to `config.yaml` — starts for the current session with its `start` and is joined at the first opening; armed after the first prompt it waits for the next boundary; re-arming an entry that already ran this session starts nothing. Holds with or without an `ObserveRunner`.
**Approach (assumed at the header base):** `SetReactions` → `fireNewlyArmed` covers observe only (`ObserveRunner.SessionStartedFor`). Extend the same path: newly armed advise ids on `session-started` are appended to the current boundary's runs through a new `sessionAdviseLane` method beside `replace` (extends `sessionadvise.go`; no new module), reusing the per-boundary fired-id set. `announceSessionStart` opens `sessionWindow` only when an `ObserveRunner` is present: open it whenever the boundary is announced.
**Regression guard.** The boundary seeds the window's fired set with the advise ids `startSessionAdvice` starts as well as the observe ids (`opened` takes both lanes), so an unchanged `SetReactions` before the first prompt (a `/settings` toggle after `lateEngine.Bind`) starts no second run; the new lane method installs a cancel when the lane has none, so the next boundary or Close ends the run. Acceptance adds `TestSessionWindow`. Supersedes the doc comments that call the window shut on "an Agent holding no Runner" (`sessionWindow`, internal/agent/agent.go) and say such an Agent "has no one to tell" (`announceSessionStart`, internal/agent/construct.go): revise both.
**Files:** internal/agent/agent.go, internal/agent/construct.go, internal/agent/sessionadvise.go, internal/agent/sessionadvise_test.go, cmd/apogee/wire_settings_test.go
**Read first:** internal/agent/agent.go — SetReactions, fireNewlyArmed, sessionWindow.opened; internal/agent/construct.go — announceSessionStart; internal/agent/sessionadvise.go — startSessionAdvice, sessionAdviseLane.replace;
cmd/apogee/wire_engine.go — lateEngine.Bind; cmd/apogee/wire_settings_test.go — TestSessionStartedAdoptedBeforeTheFirstPromptFiresForTheSession
**Tests:** agent: live-arm before first prompt → fenced at the opening; after → nothing; re-arm → nothing; no-ObserveRunner config → still fenced; an entry armed at the boundary, then an unchanged `SetReactions` before the first prompt → one run and one fence. cmd: twin of `TestSessionStartedAdoptedBeforeTheFirstPromptFiresForTheSession` for an adopted repo advise entry.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionAdvise|TestSessionStart|TestSessionWindow' ./internal/agent/` and `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionStarted' ./cmd/apogee/`
**Commit:** `feat(agent): an advise entry armed before the first prompt briefs that session`

## 7. `/advice` shows session-start advice; the TUI journey is driven

**What:**
Depends on item 4.
**Goal:** The `/advice` pane shows a session-start firing under its run and Turn with its fenced text and the `<reaction> (user origin) @ session-started` heading; a driven TUI session with an argv advise on `session-started` sends the fence in its first request; the pane's doc comments no longer say advice lands only inside a tool result.
**Approach (assumed at the header base):** `foldAdvice` (`internal/tui/advicepane.go`) already folds any advise firing; it showed empty text only because of item 4's Detail gap. Fix the comments there and in `internal/tui/doc.go`; add the pane test and a TUI e2e beside `cmd/apogee/e2e_session_started_test.go` (`launchTUIConfigured`).
**Regression guard.** The TUI's auto-title call fires in parallel with the first Exchange (`auto-title: true` by default), so the e2e picks the Turn request by content (its last user message holds the submitted prompt) or sets `auto-title: false` in `extraConfig`, and is named `TestE2ESessionStarted…`. expected fences on a user-role message are built with AdviceSpan Leading: true. Every TUI spec sentence placing advice on a tool result follows, layout.md's `/advice` popup included: `grep -rnE 'on a tool result|inside the tool result|tool-result seam' internal/tui layout.md docs/layout`.
**Files:** internal/tui/advicepane.go, internal/tui/advicepane_test.go, internal/tui/doc.go, cmd/apogee/e2e_session_started_test.go, layout.md
**Read first:** internal/tui/advicepane.go — foldAdvice, adviceFiringRow; internal/tui/advicepane_test.go — TestAdviceBoardFoldsAdviseFiringsInOrder; cmd/apogee/e2e_session_started_test.go — TestE2ESessionStartedClearDoesNotHang;
cmd/apogee/e2e_support_test.go — launchTUIConfigured; internal/tui/autotitle.go — titleCmd; internal/tui/doc.go — advicepane.go paragraph; layout.md — The `/advice` popup
**Tests:** pane fold of a session-started `ReactionFiredEvent`; TUI e2e `TestE2ESessionStarted…` first-Turn-request fence (span `Leading: true`), the Turn request chosen by content or auto-title off.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Advice' ./internal/tui/` and `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2ESessionStarted' ./cmd/apogee/`; the grep above finds no sentence saying advice lands only on a tool result.
**Commit:** `feat(tui): /advice shows session-start advice`

## 8. The `compaction-finished` notice in the vocabulary

**What:**
**Goal:** `domain.Moment` includes the notice `compaction-finished`, appended at the end of the notice list; `run:` and `advise:` entries may subscribe to it, `gate:` refuses it as any notice; the facade re-exports `apogee.MomentCompactionFinished`; every public count follows (thirteen notices, eighteen Moments) and `docs/manual/reactions.md`'s notices table gains its row (fold kinds, top-level only).
**Approach (assumed at the header base):** mirror the archived plan's item 2 for `session-started`: const beside the notice consts in `internal/domain/reaction.go`, `allNotices` (append last), `Reaction.Validate`'s advise Moments (item 3's sentence gains it: `post-tool-result, file-changed, session-started or compaction-finished`), `internal/reactions/hooks.go` `ParseEvent` listing, `apogee.go`. Find count sites by rule: `grep -rniE '\b(seven|twelve|seventeen|twelfth)\b' docs/manual internal/config/defaults .capcom/knowledge/glossary apogee.go internal/domain internal/reactions` (glossary-retired is historical; every hit reviewed — reactions.md's "Seven report something the session did" becomes eight).
**Regression guard.** Acceptance builds the facade and greps the row (`go build ./...`; `grep -n 'compaction-finished' docs/manual/reactions.md apogee.go`). Stale advise-Moment comments are re-found after item 3: `grep -rnE 'post-tool-result or file-changed|only combined entry' internal/` (the `entryReactions` comment in internal/config/reactions.go). Tests name `TestSeamsAndNoticesReportTheVocabularyAsACopy`, `TestMomentIsSeam`'s table, `TestEventsIsTheWholeVocabularyInOrder`, `TestParseEventErrorTextIsByteIdentical` and hooks_test.go's "twelve" comments. item 8 also updates every in-repo link to the renamed notices heading anchor (`#the-twelve-notices`; find by `grep -rn 'the-twelve-notices' docs README.md`)
**Files:** internal/domain/reaction.go, internal/domain/reaction_test.go, internal/config/reactions.go, internal/config/reactions_test.go, internal/reactions/hooks.go, internal/reactions/hooks_test.go, apogee.go, docs/manual/reactions.md, docs/manual/README.md, docs/manual/configuration.md, internal/config/defaults/config.yaml, .capcom/knowledge/glossary/, every file `grep -rn 'the-twelve-notices' docs README.md` names
**Read first:** internal/domain/reaction.go — allNotices, Reaction.Validate; internal/domain/reaction_test.go — TestSeamsAndNoticesReportTheVocabularyAsACopy, TestManualStatesTheMomentCount;
internal/reactions/hooks.go — ParseEvent, eventList; apogee.go — MomentSessionStarted; docs/manual/reactions.md — "The twelve notices"
**Tests:** `TestMomentValuesArePinnedLiterals`, `TestManualStatesTheMomentCount`, `TestSeamsAndNoticesReportTheVocabularyAsACopy`, `TestMomentIsSeam`, `TestEventsIsTheWholeVocabularyInOrder`, `TestParseEventErrorTextIsByteIdentical` and the hooks vocabulary pins follow; validation table rows for run/advise/gate on it.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/ ./internal/reactions/` (two light packages, one run) and `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Reaction|Manual' ./internal/config/`; `go build ./...`; `grep -n 'compaction-finished' docs/manual/reactions.md apogee.go` finds the row and the const; `grep -rn 'the-twelve-notices' docs README.md` finds nothing.
**Commit:** `feat(reactions): compaction-finished joins the Moment vocabulary`

## 9. A fold fires `compaction-finished` through the ObserveRunner

**What:**
Depends on item 8.
**Goal:** Every fold that ran on the top-level agent (estimate-driven, predictive or reactive emergency, `/compact`) fires `compaction-finished` once, after its `Replace`, on every Driver; a faulted, cancelled or no-op fold and any delegate fold fire none. Its seam document carries `fold` (`auto | emergency | manual`) and its command env `APOGEE_REACTION_FOLD`. `apogee.ObserveRunner` gains `CompactionFinished(apogee.CompactionFold)`, enqueue-only like `SessionStarted`.
**Approach (assumed at the header base):** all folds go through `Agent.foldFor` (`internal/agent/compact.go`) and its `foldTable`; fire where `rearmFillNotice` is called after a fold that ran, gated on `!isDelegate`. Payload type and constants beside `SessionStart` in `internal/domain/seampayload.go` (`Env()` emits the variable). Implement the method on `internal/reactions.Runner` beside `SessionStarted`; update every `ObserveRunner` implementer and test double (`grep -rn 'SessionStartedFor(' --include=*.go`).
**Regression guard.** The interface change reaches three test doubles — `startRecordingRunner` (internal/agent/sessionstart_test.go), `recordingObserveRunner` (internal/run/run_test.go), `recordingRunner` (cmd/apogee/wire_engine_test.go) — and the twin firing builder sits beside `sessionStartedFiring` in internal/reactions/match.go; acceptance vets `./internal/run/` and `./cmd/apogee/`. The fold's Turn passes through the door (`CompactionFinished(fold, turn)` or a payload struct) so the seam document's `turn` is the fold's Turn; stamping turn 0 instead must be stated in the item and the manual as session-started's paragraph does.
**Files:** internal/domain/reaction.go, internal/domain/seampayload.go, internal/domain/seampayload_test.go, internal/agent/compact.go, internal/agent/compact_test.go, internal/agent/sessionstart_test.go, internal/reactions/runner.go, internal/reactions/runner_test.go, internal/reactions/match.go, internal/run/run_test.go, cmd/apogee/wire_engine_test.go, apogee.go
**Read first:** internal/agent/compact.go — foldFor, foldTable, rearmFillNotice; internal/reactions/runner.go — SessionStarted; internal/reactions/match.go — sessionStartedFiring, firingOf;
internal/domain/reaction.go — ObserveRunner; internal/domain/seampayload.go — SeamPayload
**Tests:** agent: each fold kind fires once with its `fold` and its Turn; failed fold and delegate fold fire none (fake ObserveRunner); runner: argv entry on the notice sees `APOGEE_REACTION_FOLD`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Compact|Fold' ./internal/agent/` and `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/reactions/ ./internal/domain/`; `go vet ./internal/run/ ./cmd/apogee/`
**Commit:** `feat(agent): a fold fires the compaction-finished notice`

## 10. Strip one reaction's advice spans from a message

**What:**
**Goal:** `domain` offers stripping the advice spans of one named reaction from a conversation message while keeping every other reaction's spans and their ledger rows consistent; the provenance ledger drops exactly the stripped rows.
**Approach (assumed at the header base):** extends the advice ledger in `internal/domain/advice.go` (beside `ContentWithoutAdvice` and `AdviseUserMessage`); no new module. Range-strip by the ledger's `{Reaction, Offset}` rows as the existing strip does, shifting later offsets.
**Regression guard.** Cut `Content[Offset:End]` only where `fenceStands`, and shift both `Offset` and `End` of every row past the cut (mirroring `withLeadingAdvice`), so the remaining leading rows still tile from 0 for `leadingEnd`. Offer it as a `Conversation` method beside `AdviseUserMessage` (message index, reaction id) that bumps the revision only when it strips — `SetMessageContent` drops the ledger, so item 11 has no other ledger-preserving write. Name the test so `-run 'Advice|Advise'` selects it.
**Files:** internal/domain/advice.go, internal/domain/advice_test.go
**Read first:** internal/domain/advice.go — Message.withLeadingAdvice, Message.leadingEnd, Message.fenceStands, Message.ContentWithoutAdvice, Conversation.AdviseUserMessage;
internal/domain/conversation.go — Conversation.SetMessageContent; internal/domain/advice_test.go — sessionStartSpan, TestConversationAdviseUserMessage
**Tests:** two reactions' leading spans on one message: strip one → the other's text, `Offset` and `End` intact and `ContentWithoutAdvice` equals the bare prompt; strip absent id → no change and no revision bump.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Advice|Advise' ./internal/domain/`
**Commit:** `feat(domain): strip one reaction's advice spans from a message`

## 11. Compaction advice re-briefs the model after a fold

**What:**
Depends on items 4, 9 and 10.
**Goal:** An advise entry on `compaction-finished` starts when a top-level fold begins and is joined after its `Replace`; its answer lands fenced ahead of the next user-role message's text — the opening for a fold between Exchanges, the overflow bridge for a fold mid-Exchange — never in the summary or system prompt; when it attaches, the same reaction id's spans in the protected first user message are stripped. Fail-open, capped, redacted, Bypass-off, booked as an `advise` firing; a fold that faults or is cancelled discards its runs.
**Approach (assumed at the header base):** extend the session-start lane in `internal/agent/sessionadvise.go` to a lane keyed by Moment (one module; `startSessionAdvice`'s filter takes the Moment) rather than a parallel lane. Start runs in `foldFor` before `apogeectx.Compact`; join on the loop goroutine after the `Replace` (opening: the existing join after `turns.open`; mid-Exchange: after `refold` appends `overflowBridge`, before `armRequest`, waiting only for what is left of each deadline). Strip the prefix message with item 10's method first, then attach with `conv.AdviseUserMessage`.
**Regression guard.** builds on item 9's post-Replace hook in foldFor rather than re-placing it; the mid-Exchange join sits in refold (internal/agent/loop.go) between foldFor (which appends the overflow bridge) and armRequest; expected fences use AdviceSpan Leading: true.
Run state stays separate per Moment: a fold replaces only compaction runs (a resume's first-opening `autoCompact` keeps the pending session-start runs); a Session boundary and `Close` discard both; `exchangeClosed` / `exchangeAborted` forget/restore both. A fold that faults, is cancelled or is `Skipped` discards its runs. Strip before attaching (or skip the strip when the target is the prefix message); at one join an earlier same-id span in the target message is dropped too, so one entry on both Moments lands one fence.
**Files:** internal/agent/sessionadvise.go, internal/agent/sessionadvise_test.go, internal/agent/compact.go, internal/agent/loop.go, internal/agent/construct.go, internal/agent/agent.go, internal/agent/compactadvise_test.go
**Read first:** internal/agent/sessionadvise.go — sessionAdviseLane, startSessionAdvice, joinSessionAdvice; internal/agent/compact.go — foldFor; internal/agent/loop.go — step, refold;
internal/agent/construct.go — exchangeClosed, exchangeAborted
**Tests:** opening fold → fence in the opening; emergency fold → fence in the bridge, bridge's fixed text intact after it; stale same-id span gone from the first user message, other id kept; timeout/fail → nothing attached, stale span kept; Bypass → nothing; delegate fold → no run; skipped fold → its runs discarded; resume that folds at its first opening keeps the session-start runs; fold → `/clear` → prompt attaches the fresh span unstripped; one entry on `[session-started, compaction-finished]` joined at one opening → one fence. Expected spans `Leading: true`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'CompactAdvise|TestSessionAdvise|Fold' ./internal/agent/`
**Commit:** `feat(agent): compaction-finished advice re-briefs the model after a fold`

## 12. Headless journey: re-brief after an emergency fold

**What:**
Depends on items 5 and 11.
**Goal:** A headless run whose stub upstream overflows mid-Exchange sends, after the emergency fold, a request whose overflow bridge carries the fenced answer of an argv advise on `compaction-finished`, and whose first user message no longer carries that reaction's session-start span; `reaction_fired` lines show both firings.
**Approach (assumed at the header base):** extend `cmd/apogee/e2e_reactions_test.go` with an overflow stub script (see the Regression guard), one entry `on: [session-started, compaction-finished]`.
**Regression guard.** No emergency-fold e2e pattern exists in cmd/apogee, and internal/agent's `overflowTurn` / `overflowBody` / `summaryTurn` are unimportable test code: restate them in a Go `stubllm.Script` (not the shared reactions.yaml) — a `list_dir` tool-call turn (so `minCompactTail` = 2 does not skip the fold), an HTTP 400 turn whose body carries an `isContextOverflow` marker, a summary turn matched `When: &stubllm.Match{System: "compacting a conversation"}`, then the answer. expected fences use AdviceSpan Leading: true.
**Files:** cmd/apogee/e2e_reactions_test.go
**Read first:** cmd/apogee/e2e_reactions_test.go — adviseConfig, TestE2EAdviseLandsAsTheTrailer, headlessHooksArgs; internal/agent/compact.go — foldFor;
internal/agent/compact_test.go — overflowTurn, overflowBody; internal/agent/autocompact_test.go — summaryTurn; internal/context/compact.go — minCompactTail
**Tests:** as Goal; the bridge's and the session-start's expected spans use `Leading: true`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EAdvise' ./cmd/apogee/`
**Commit:** `test(apogee): headless re-brief after an emergency fold`

## 13. Manual: session-start and compaction advice

**What:**
Depends on items 3 and 11.
**Goal:** `docs/manual/reactions.md` and `docs/manual/configuration.md` state that `advise:` takes `post-tool-result`, `file-changed`, `session-started` and `compaction-finished`; describe where each lands (session start: fenced in the next user message ahead of the prompt; compaction: the next user-role message after a fold, replacing that reaction's stale briefing), the timing (background start, joined at the opening/after the fold), the seam document's `start` and `fold`, failure reporting, adoption before the first prompt, and the cost (one fenced block per boundary/fold in context); carry the worked example `advise: ["capcom", "prime"]` with `on: [session-started, compaction-finished]` beside a generic script. No sentence names the admission refusal any more.
**Approach (assumed at the header base):** reactions.md: intro paragraphs, the "An entry" example and the `on:` and `advise:` key rows, the multi-key paragraph after the key table, "The payload", "The seam document", "Running a command", "Advising the model", "Where a failure shows", the adoption intro. configuration.md: the reactions paragraph and its YAML example. Find stale prose by rule: `grep -nE 'post-tool-result.? or .?file-changed|tool result|call.s result|after a tool call|admission|APOGEE_REACTION_START|\`start\`' docs/manual/reactions.md docs/manual/configuration.md` — every hit reviewed.
**Regression guard.** item 13 also owns the run:-lane seam-document `fold` payload row and the `APOGEE_REACTION_FOLD` entry in reactions.md's environment-variable list (item 9 writes no docs). The stale-prose rule is the widened grep above (it reaches the "An entry" YAML comment, configuration.md's "appended to that call's result" and the `start` payload/env sites). Acceptance runs one package per test run and adds the configuration.md drift gates `TestManualDocumentsEverySettingsKey` and `TestManualListsEveryEnvironmentOverride` (cmd/apogee).
**Files:** docs/manual/reactions.md, docs/manual/configuration.md
**Read first:** docs/manual/reactions.md — "An entry" key table, "The payload", "Running a command" env list, "Advising the model", "Where a failure shows"; docs/manual/configuration.md — "Reactions — `reactions:`" paragraph;
cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (docs); drift tests must still pass.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Manual' ./internal/domain/`, then `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Manual' ./internal/config/`, then `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManual' ./cmd/apogee/`; the grep above finds no stale sentence.
**Commit:** `docs(manual): advise at session start and after a fold`

## 14. Front door, other manual pages and the glossary

**What:**
Depends on item 13.
**Goal:** `README.md`'s Reactions bullet, `docs/manual/README.md`'s Reactions row, `docs/manual/commands.md`'s `/advice` row, `docs/manual/probe.md`'s `--live` paragraph and `docs/manual/headless.md`'s `reaction_fired` event row agree with items 3–11; glossary Notes Reaction, Moment, Advice span and Compaction say advise fires at four Moments and that a fold fires `compaction-finished`.
**Approach (assumed at the header base):** rule: every sentence saying where or when advise fires, found by `grep -rnE 'post-tool-result.? or .?file-changed|tool result|call that just finished|two Moments|admission' README.md docs/manual .capcom/knowledge/glossary` (glossary-retired and CHANGELOG are historical; each hit reviewed); edit Notes in place, then `capcom check`.
**Regression guard.** docs/manual/README.md's Reactions row holds the sentence `TestManualStatesTheMomentCount` pins by first match of `the (\w+) Moments`, so advise's Moments are spelled there without "the <N> Moments", and acceptance runs that test and `TestReadmeStatesTheFloorGuardCount` (separate runs). The acceptance grep narrows to `awaits its admission arm|pending admission` — reaction-surface.md's "admission arm" line is current doctrine. headless.md's site is the `reaction_fired` row: edit it only if it misstates advise firings at the new Moments, else leave headless.md untouched.
**Files:** README.md, docs/manual/README.md, docs/manual/commands.md, docs/manual/probe.md, docs/manual/headless.md, .capcom/knowledge/glossary/
**Read first:** docs/manual/README.md — Reactions row; internal/domain/reaction_test.go — TestManualStatesTheMomentCount; README.md — Reactions bullet, docs-table Reactions row; internal/config/reactions_test.go — TestReadmeStatesTheFloorGuardCount;
docs/manual/commands.md — `/advice` row; .capcom/knowledge/glossary/glossary-reactions — moment.md, reaction-surface.md
**Tests:** none (docs).
**Acceptance:** `capcom check` reports no problems; `grep -rnE 'awaits its admission arm|pending admission|post-tool-result or file-changed' README.md docs/manual .capcom/knowledge/glossary` finds only historical lines; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Manual' ./cmd/apogee/`, then `GOMEMLIMIT=2GiB go test -race -count=1 -run TestManualStatesTheMomentCount ./internal/domain/`, then `GOMEMLIMIT=2GiB go test -race -count=1 -run TestReadmeStatesTheFloorGuardCount ./internal/config/`
**Commit:** `docs: session-start and compaction advice across the front door and glossary`
