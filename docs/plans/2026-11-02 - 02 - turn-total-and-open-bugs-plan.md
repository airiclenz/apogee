# Turn total duration and the open bugs

**Goal:** Show the running Exchange's total duration beside the status line's elapsed clock in one reshaped clock format, and fix the four open bugs: unpinned Firings never binding the served model, any `reasoning` object picking the OpenRouter dialect, a watcher missing a workflow-moved session-seat note, and the Firing namer naming against a dead Sub-agents primary.

**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 98becd60

**Sources:**
- `.capcom/issues/turn-total-duration.md`, `.capcom/issues/headless-unbound-model.md`, `.capcom/issues/reasoning-object-dialect-tell.md`, `.capcom/issues/watched-workflow-switch-unnoted.md`, `.capcom/issues/firing-routed-relatch-gap.md`
- ADR 0024 decision 6, ADR 0031, ADR 0055 decision 3, ADR 0060 (`capcom know show "ADR NNNN" --with-children`)
- Glossary: `Exchange`, `Turn`, `Heartbeat`, `Thinking effort`
- `docs/plans/archived/2026-11-02 - 01 - fallback-followups-plan.md` items 2 and 8

**Ratified design calls** (owner, 2026-11-02):
- **Total readout span:** `[total …]` counts the Exchange (glossary term; the issue's "Turn" means it), read from the top-level activity slot, never the shown child slot.
- **Narrow status line:** `[total …]` gives way first, then the `quiet` qualifier, then the phrase truncates with `…` as today.
- **Splash-shaped `reasoning` object:** an object carrying `thinks`, `efforts` or `default` selects `EffortDialectKwargs` with its `efforts`/`default` read; `thinks: false` means unsupported. `EffortDialectReasoning` is chosen only when `supported_efforts` or `default_effort` is present. A bare `{}` (or any other shape) is no tell and falls through to the `/props` template tell. Recorded as ADR 0060 amendment `adr-0060-amend-1102`.
- **headless-unbound-model:** included in this plan though its working-tree status reads `active`.

**Regression check (2026-11-02, 98becd60):**
- 1: guard folded; yields to ADR 0055 decision 3
- 2: guard folded; writer decision applied: OpenRouter keys win a mixed object; `thinks: false` is final and does not fall through to the `/props` template; only a no-tell object falls through
- 3: guard folded; the amendment states item 2's precedence (writer decision)
- 5: guard folded (writer decision applied)
- 6: guard folded
- 7: guard folded (writer decision applied); supersedes layout.md's "the quiet qualifier … is the first thing the slot gives up" and statusLeftWithin's matching comment

**Standing requirements:**
- skills: coding-standards
- This box (Pi 5): never `go test -race` over a whole heavy package or pattern; run tests narrowed with `-run`, prefixed `GOMEMLIMIT=2GiB`, one test run at a time; pass this to every sub-agent.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).

**Out of scope:**
- The TUI's `delegationWiring.fallover` keeping a stale namer binding on a nil fallback target.
- Changing Splash itself; any effort dialect beyond the four that exist.
- A new repaint timer (the total rides the spinner tick).

## 1. Firings bind the served model when none is configured — ✅ DONE (2026-11-02)

NOTES (2026-11-02): firingConfig returns the no-active-model refusal as a plain error (sentinel errFiringNoActiveModel, failure appended by firingNoActiveModel) which raise already wraps as errNotStarted{Stage: stageCompose}, rather than returning an errNotStarted from firingConfig itself — the observable result (typed never-started refusal, headless exit 2) is the one the Goal names, without a double wrap.
NOTES (2026-11-02): cmd/apogee/wire_config_test.go needed no edit — projectedDrivers' "the Firing composer" goes through firingBeat, which now carries ActiveModel "beat-model".

**Closes:** headless-unbound-model

**What:** fix — `apogee headless`, daemon and session-raised Firings on an entry with no `model:` (and no `--model`/`APOGEE_MODEL`) exit 2 with the engine's "no model is bound — Rebind …" error, contradicting `apogee --help` (`cmd/apogee/root.go`, "With no model set anywhere, apogee asks the server for its active model").

**Regression guard.** Give the shared `firingBeat` (`cmd/apogee/wire_firing_test.go`) an `ActiveModel` (e.g. `"beat-model"`), so every unpinned test through it (the `TestFiringConfig*` users, `TestFiringSeatChoiceToolsReadTheRunsScratchDir`, `TestFiringHooksFenceARepoRun`, and `projectedDrivers`' "the Firing composer" in `cmd/apogee/wire_config_test.go`) binds rather than refuses; pin `Model:` on the primary entries of `TestFiringConfigCarriesItsPrimaryObservation` (its 401 row keeps Reachable false, Answered true, Failure carried) and `TestFiringConfigBeatsTheSubAgentServerOnItsOwnEndpoint`. When `beat.Failure != ""`, the no-active-model sentence appends it (as `notice.ServerOffline` does) so a 401/429 is named. Rule: rewrite every comment that calls an empty Firing model legitimate or "whatever is serving" to say the beat's served model is bound — `grep -n 'whatever is serving' cmd/apogee/*.go` (wire_firing.go `bindFiringConfig`, daemonfire.go, daemonfire_test.go). ADR 0055 decision 3 stands and the item yields to it: the Firing still never actuates a launcher, it names the model the beat saw serving; `docs/manual/daemon.md` ("sends to whatever that server is already serving") stays true.

**Goal:** a Firing whose configured model is empty binds the active model its composition beat observed — `run_started.model` names it, and system prompt, Model profile, window, namer and fallback all key on it; a beat that observed no model refuses before submit with a clear sentence as an `errNotStarted` (headless exit 2), never the Rebind error.

**Approach (assumed at the header base):** in `firingConfig` (`cmd/apogee/wire_firing.go`), right after the one `observe(...)` beat and before the window, namer (`newFiringNamer`), `newFiringFallback` and `resolveFiringRouting` read `spec.Model`: when `spec.Model == ""` and `beat.ActiveModel != ""`, re-run `bindFiringConfig` with `in.model = beat.ActiveModel` and the already-resolved `apiKey`/`keys` (so `api-key-cmd` runs once — precedent `measureContextCost`, `cmd/apogee/probecontext.go`), and take its cfg, spec and notices. When both are empty and the beat answered, return `errNotStarted{Stage: stageCompose}` wrapping a new sentinel worded like `errProbeContextLiveNeedsModel` ("the server named no active model; set `model:` on the entry or pass --model"). An unanswered beat keeps today's offline refusal in `raise`. In `scheduleWiring.fire` (`cmd/apogee/schedule.go`) set the synthetic beat's `ActiveModel` to the bound model. Extends `firingConfig`; no new module.

**Files:** cmd/apogee/wire_firing.go, cmd/apogee/schedule.go, cmd/apogee/daemonfire.go, cmd/apogee/headless_test.go, cmd/apogee/daemonfire_test.go, cmd/apogee/wire_firing_test.go, cmd/apogee/wire_config_test.go
**Read first:** cmd/apogee/wire_firing.go — firingConfig, bindFiringConfig, errNotStarted; cmd/apogee/schedule.go — scheduleWiring.fire; cmd/apogee/wire_firing_test.go — firingBeat; cmd/apogee/wire_config_test.go — projectedDrivers;
cmd/apogee/daemonfire_test.go — TestDaemonFireSendsToALauncherFrontedServerAsItStands; internal/heartbeat/heartbeat.go — Beat.ActiveModel

**Tests:** headless real-run test on an unpinned home against `stubllm` advertising one model (`runner: run.Once`) — the run completes and `run_started.model` equals the advertised id; daemon `fire` on the same entry binds it (update `TestDaemonFireSendsToALauncherFrontedServerAsItStands`, which pins `""`); a beat answering with no model refuses exit 2 with the new sentence; `raiseInputs` entry gains a `Model` so `TestRaise*` keep their intent; rows of `TestHeadlessRunsAgainstAServerThatAnsweredAtAll` pin a model where they test something else; `firingBeat` gains an `ActiveModel` and `TestFiringConfigCarriesItsPrimaryObservation` / `TestFiringConfigBeatsTheSubAgentServerOnItsOwnEndpoint` pin `Model:` (per the guard); a beat that answered a 401 with no model refuses with the sentence plus its failure; a `firingConfig` row (unpinned entry, beat `ActiveModel: "served"`) asserts `cfg.Model`, the system prompt and Model profile for that id, the namer's session binding model and the window.

**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestHeadless|TestDaemonFire|TestFiringConfig|TestFiringSeatChoice|TestFiringHooks|TestProjectConfig|TestRaise|TestSchedule' ./cmd/apogee/`

**Commit:** `fix(headless): Firings bind the served model when none is configured`

## 2. Only an OpenRouter-shaped reasoning object picks the reasoning dialect — ✅ DONE (2026-11-02)

NOTES (2026-11-02): re-derived from the assumption that `modelReasoning` is extended with Splash's fields — it is replaced by two shape structs (`openRouterReasoning`, `splashReasoning`) decoded in separate passes after a key-presence classification (`carriesAny`), so a Splash key of an unexpected type cannot cost an OpenRouter object its tell; `mandatory` moved into `reasoningMandatory` (still its own best-effort pass, OpenRouter shape only).
NOTES (2026-11-02): `decodeReasoning`/`reasoningFor`/`effortSupport`/`toModelInfo`/`discoverModels` now also return whether the entry told anything, and `Discover` falls through to the `/props` template only when it did not (was: when the answer was unsupported) — this is what makes `thinks: false` final; a key whose value is JSON null counts as absent.

**Closes:** reasoning-object-dialect-tell

**What:** fix — `modelsResponse.effortSupport` (`internal/provider/discovery.go`) treats any non-null `reasoning` object as OpenRouter's tell, so Splash's `{"thinks":true,"efforts":[…],"default":"medium"}` gets `reasoning:{effort}` which Splash ignores, and its `efforts`/`default` are never read. Rule per the ratified call.

**Regression guard.** `reasoning:{}` stops being a tell, so the two driven tests that script `&stubllm.ModelReasoning{}` and want `EffortDialectReasoning` — `TestFiringConfigDefaultsItsSeams` (`cmd/apogee/wire_firing_test.go`) and `TestHeadlessSendsTheServersEffortDialect` (`cmd/apogee/headless_test.go`) — script `&stubllm.ModelReasoning{SupportedEfforts: []string{"low", "high"}}` and their comments drop "a `reasoning` object is the tell". Rule for the comments: every Go comment saying a `reasoning` object's presence is the tell or picks OpenRouter's dialect — `grep -rn 'presence\|reasoning. object' internal/provider internal/stubllm cmd/apogee/*_test.go` (filter hits by eye); known sites besides the Approach's: `DiscoveredModel.EffortSupport` and `effortSupport`'s doc (discovery.go), stubllm `DiscoveredModel`/`ModelReasoning` (script.go) and `modelEntry` (wire.go).
Precedence when shapes mix: an object carrying `supported_efforts` or `default_effort` is OpenRouter-shaped and picks `EffortDialectReasoning` whatever Splash keys it also carries. Otherwise an explicit `thinks: false` is the server's own answer for that model: the answer is the zero EffortSupport and `Discover` does NOT fall through to the `/props` template tell for it (a test row pins `thinks:false` plus a kwargs `/props` template → unsupported). Only a no-tell object (`{}` or unrecognised keys) falls through.

**Goal:** discovery returns `EffortDialectReasoning` only for an object with `supported_efforts` or `default_effort`; a Splash-shaped object (`thinks`/`efforts`/`default`) returns `{Supported: true, Dialect: EffortDialectKwargs, Efforts, Default}`, or the zero answer when `thinks` is `false`; any other object, `{}` included, is no tell, so `Discover` falls through to `effortFromTemplate`.

**Approach (assumed at the header base):** extend `modelReasoning` with Splash's `Thinks *bool` (`thinks`), `Efforts` (`efforts`), `Default` (`default`); `decodeReasoning` classifies the shape (keep the best-effort second unmarshal for `mandatory`); `effortSupport` maps the class to the dialect. Keep the `:` variant base-slug retry. `forceEffortDialect` needs no change (forcing `kwargs` on Splash now keeps its efforts). Update the comments naming "mere presence is the tell": `modelReasoning`, the `EffortSupport` type doc, `Discover`, and `EffortDialectReasoning` in `internal/provider/wire.go`.

**Files:** internal/provider/discovery.go, internal/provider/wire.go, internal/provider/discovery_test.go, cmd/apogee/wire_firing_test.go, cmd/apogee/headless_test.go, internal/stubllm/script.go, internal/stubllm/wire.go
**Read first:** internal/provider/discovery.go — decodeReasoning, modelReasoning, modelsResponse.effortSupport, Discover; internal/provider/discovery_test.go — TestDiscover_EffortSupport, TestDiscover_ForcedEffortDialect;
cmd/apogee/wire_firing_test.go — TestFiringConfigDefaultsItsSeams; cmd/apogee/headless_test.go — TestHeadlessSendsTheServersEffortDialect

**Tests:** in `TestDiscover_EffortSupport` change the "bare reasoning object still means supported" row to no tell (with and without a kwargs `/props` template); add rows for a Splash shape (kwargs, `[low medium xhigh]`, default `medium`), `thinks:false` (unsupported), `thinks:false` plus a kwargs `/props` template (unsupported — no fall-through), an object carrying both `supported_efforts` and Splash keys (`EffortDialectReasoning`), and the existing OpenRouter fixtures unchanged; a `TestDiscover_ForcedEffortDialect` row forcing `kwargs` on a Splash shape keeps its efforts; `TestFiringConfigDefaultsItsSeams` and `TestHeadlessSendsTheServersEffortDialect` script `SupportedEfforts` and keep wanting `EffortDialectReasoning`.

**Acceptance:**
- `go build ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDiscover|TestEffortDialectFor|TestBuildBody' ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFiringConfigDefaultsItsSeams|TestHeadlessSendsTheServersEffortDialect' ./cmd/apogee/`

**Commit:** `fix(discovery): only an OpenRouter-shaped reasoning object picks the reasoning dialect`

## 3. Record the reasoning-object rule in ADR 0060 and the docs — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the amendment Note carries `refs: [internal/provider/discovery.go]` (set with `capcom know add --ref`), which the adr-0043-amend-1010 template does not; `capcom check` passes with it.
NOTES (2026-11-02): docs/manual/configuration.md also names Splash beside llama.cpp among the template-driven servers that take `chat_template_kwargs` ("Three different wires" paragraph), so the dialect list agrees with the new shape-rule sentence above it.

**What:** docs for item 2. Depends on item 2.

**Regression guard.** The prose guard's grep widens to `grep -rn 'reasoning. object\|/v1/models. entry\|mere presence' docs/manual docs/design .capcom/knowledge/glossary`; known sites add `docs/design/test-drivers.md` (discovery `models:` bullet, "whose mere presence is the effort tell — `reasoning: {}` scripts a dial with no vocabulary") and `docs/manual/configuration.md` ("a `/v1/models` entry naming that model's own levels"); `internal/config/defaults/config.yaml` leaves the known-site list (its `effort-dialect` comment carries no such sentence). The amendment states item 2's precedence: OpenRouter keys (`supported_efforts`/`default_effort`) win a mixed object; an explicit `thinks: false` is final, with no fall-through to the `/props` template; only a no-tell object falls through.

**Goal:** ADR 0060 carries a child Note `adr-0060-amend-1102` stating the shape rule of item 2 (amends D1 and D2; why: a shared field name is not proof of OpenRouter's dialect; source: this plan), named on the record's Status line and in its reading-order list; every user-facing description of the `/v1/models` reasoning tell states the shape rule.

**Approach (assumed at the header base):** `capcom know add --id adr-0060-amend-1102 --parent adr-0060`, modelled on `.capcom/knowledge/decisions/adr-0043/adr-0043-amend-1010.md`; edit `.capcom/knowledge/decisions/adr-0060.md` Status line and reading-order list. Rule for the prose guard: every doc or glossary sentence saying a `reasoning` object (or its presence) selects OpenRouter's dialect — find with `grep -rn 'reasoning. object\|reasoning` object' docs/manual .capcom/knowledge/glossary internal/config/defaults/config.yaml`; known sites `docs/manual/configuration.md` (effort section), `.capcom/knowledge/glossary/glossary-identity/thinking-effort.md`, `internal/config/defaults/config.yaml` (`effort-dialect` comment).

**Files:** .capcom/knowledge/decisions/adr-0060.md, .capcom/knowledge/decisions/adr-0060/adr-0060-amend-1102.md, docs/manual/configuration.md, .capcom/knowledge/glossary/glossary-identity/thinking-effort.md, docs/design/test-drivers.md
**Read first:** .capcom/knowledge/decisions/adr-0060.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0060/adr-0060-decision.md — decisions 1 and 2; .capcom/knowledge/decisions/adr-0043/adr-0043-amend-1010.md — amendment template;
docs/manual/configuration.md — thinking-effort section ("Apogee works out for itself", "Three different wires"); .capcom/knowledge/glossary/glossary-identity/thinking-effort.md — detected-passively sentence; docs/design/test-drivers.md — discovery `models:` bullet

**Tests:** none new (docs only).

**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0060" | grep -n 1102`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/config/`

**Commit:** `docs(adr): ADR 0060 amendment — the reasoning object's shape picks the dialect`

## 4. A watcher notes a workflow-moved session-seat switch — ✅ DONE (2026-11-02)

NOTES (2026-11-02): added a second test, TestFallbackBackgroundSubAgentsSeatSwitchIsUnnoted (live and watched subtests), to pin the Goal's "a workflow-owned Sub-agents-seat switch stays unnoted on both paths" clause, which the plan's Tests line did not cover.

**Closes:** watched-workflow-switch-unnoted

**What:** fix — since fallback-followups item 2 (37ca5e97) a background workflow child can move the session seat; the live `eventMsg` owned branch (`internal/tui/model.go`) notes it, but `foldWatchedEvent` (`internal/tui/watch.go`) routes every workflow-owned event to `foldBackgroundEvent` and returns, so a watcher's transcript gets no "server fallback:" note.

**Goal:** `foldWatchedEvent` adds exactly one `fallbackNote` for a workflow-owned `UpstreamSwitchedEvent` on `UpstreamSeatSession`, follows nothing (`followFallback` never called, `opts` unchanged), and a workflow-owned Sub-agents-seat switch stays unnoted on both paths as today.

**Approach (assumed at the header base):** extract the owned-branch check of the `eventMsg` case into one small `Model` method (e.g. `noteWorkflowSessionSwitch(e) (domain.UpstreamSwitchedEvent, bool)`) that adds the note and reports the switch; the live path then calls `followFallback`, the watcher path does not. Both call sites share one reason to change.

**Files:** internal/tui/watch.go, internal/tui/model.go, internal/tui/fallback_test.go
**Read first:** internal/tui/model.go — Model.Update eventMsg case (workflows.owns branch); internal/tui/watch.go — foldWatchedEvent; internal/tui/heartbeat.go — Model.followFallback; internal/tui/fold.go — fallbackNote;
internal/tui/workflow.go — foldBackgroundEvent; internal/tui/fallback_test.go — TestFallbackBackgroundSessionSeatSwitchIsFollowed, TestFallbackWatchedSwitchIsNotedAndNotFollowed, sessionSwitch

**Tests:** sibling of `TestFallbackWatchedSwitchIsNotedAndNotFollowed` modelled on `TestFallbackBackgroundSessionSeatSwitchIsFollowed`: start the background workflow via `foldWatchedEvent(bgPhase(domain.WorkflowStarted))`, fold `sessionSwitch(1)` with `bgChildBase("run-bg-1")`, assert no follow calls, `noteTexts(m)[before:]` equals exactly `server fallback: test-host (test-model) failed (status 503); continuing on remote (remote-model) after waiting 2s` once, `HostAlias` unchanged.

**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback' ./internal/tui/`

**Commit:** `fix(tui): a watcher notes a workflow-moved session-seat switch`

## 5. The Firing namer follows the re-latched Sub-agents target — ✅ DONE (2026-11-02)

NOTES (2026-11-02): delegationLatchSlot.push records the pushed target even before an Agent has bound the slot (the landing says the primary is down); the Agent setter is still called only once bound.

**Closes:** firing-routed-relatch-gap

**What:** fix — residual of fallback-followups item 8: `newFiringNamer` (`cmd/apogee/naming.go`) closes over the fixed initial routed target, so after a Sub-agents-seat fallover a later unnamed child still asks the dead primary for its name. Depends on item 1 (same file).

**Regression guard.** Item 5 depends on item 1; the slot-aware namer is built after item 1's re-bind and reads the re-bound spec.Model and apiKey. Rule: rewrite every comment calling the Firing namer's Upstreams constant or unmovable — `grep -n 'constant' cmd/apogee/naming.go cmd/apogee/wire_firing.go` (naming.go's routed-reader comment and `newFiringNamer` doc, firingConfig's namer block) — and name the namer as a second reader in the `delegationLatchSlot` and `newFiringSeatFallback` docs.

**Goal:** after a Firing's Sub-agents-seat fallover, the delegation namer names against the fallback's routed binding; a nil fallback target sends naming to the session binding (routed reader `ok=false`); with no latch slot (`in.delegationLatch == nil`) the routed reader answers the routed target firingConfig resolved, for the whole run.

**Approach (assumed at the header base):** `delegationLatchSlot` (`cmd/apogee/wire_firing.go`) also records the last pushed target (atomic) and whether one was pushed; `firingConfig` passes the slot to `newFiringNamer`, whose `routed` reader returns the pushed target when present, else the initial `routed`. Extends the slot; no new type.

**Files:** cmd/apogee/wire_firing.go, cmd/apogee/naming.go, cmd/apogee/naming_test.go, cmd/apogee/e2e_fallback_test.go
**Read first:** cmd/apogee/naming.go — newFiringNamer, delegationNamer.upstream; cmd/apogee/wire_firing.go — delegationLatchSlot.push, delegationLatchSlot.bind, newFiringSeatFallback, firingConfig;
cmd/apogee/e2e_fallback_test.go — TestHeadlessRoutedFallbackRelatchesLaterSpawns, requestsCarrying

**Tests:** headless sibling of `TestHeadlessRoutedFallbackRelatchesLaterSpawns` whose second `sub_agent` call has no `name`: assert `grunt.chats == 1` (fails today with 2) and `requestsCarrying(spare, secondTask) == 2`; keep the first child named. Unit test on the namer: pushed target wins, nil push → session, nil slot → initial target.

**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestHeadlessRoutedFallback|TestDelegationNamer|TestFiringNamer' ./cmd/apogee/`

**Commit:** `fix(headless): the Firing namer follows the re-latched Sub-agents target`

## 6. One value-filled clock format

**What:** first half of turn-total-duration. `formatElapsed` (`internal/tui/activity.go`) supersedes its deliberate no-hour `#s` / `#m ##s` form.

**Regression guard.** The prose guard's grep widens to `grep -rnE '[0-9]+m( [0-9]{2}s)?\b' internal layout.md docs/layout docs/manual` (filter hits by eye); known sites add the shipped default config comment `· 21m 03s` (`internal/config/defaults/config.yaml`, stall-after comment), `docs/manual/configuration.md`'s `thinking · quiet · 12m`, and the comment examples in `internal/tui/model.go` (two `2m 59s` / `21m 03s` sites), `internal/tui/doc.go` and `internal/tui/activity.go`.

**Goal:** `formatElapsed` renders only the spots that carry a value, always with a trailing `s`: `15s`, `01:00s`, `03:15s`, `59:59s`, `01:02:15s`, `100:00:00s`; negatives clamp to `0s`; sub-seconds truncate. Every status-line test and doc example uses that shape.

**Approach (assumed at the header base):** rewrite `formatElapsed` and its doc comment; keep the clamp (cited by `relativeTime` in `internal/tui/sessions.go`). Rule for the prose guard: every test string, regexp and doc example showing the old `Nm NNs` clock — find with `grep -rnE '[0-9]+m [0-9]{2}s' internal/tui layout.md docs/layout docs/manual`; known sites `TestFormatElapsed`, `TestStatusPhraseMergesConcurrentDelegates`, `elapsedPattern` in `internal/tui/model_test.go`, `TestStatusLineQuietSuffix*`, `layout.md` (`21m 03s`), `docs/manual/commands.md` (`1m 12s`).

**Files:** internal/tui/activity.go, internal/tui/activity_test.go, internal/tui/model.go, internal/tui/doc.go, internal/tui/model_test.go, layout.md, docs/manual/commands.md, docs/manual/configuration.md, internal/config/defaults/config.yaml
**Read first:** internal/tui/activity.go — formatElapsed; internal/tui/activity_test.go — TestFormatElapsed, TestStatusPhraseMergesConcurrentDelegates; internal/tui/model_test.go — elapsedPattern, TestStatusLineQuietSuffix;
internal/tui/model.go — runningPhrase; internal/tui/sessions.go — relativeTime; internal/config/defaults/config.yaml — stall-after comment

**Tests:** `TestFormatElapsed` table covering every Goal example.

**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFormatElapsed|TestStatusPhrase|TestModelStatusLineActivity|TestStatusLineQuietSuffix|TestActivity' ./internal/tui/`

**Commit:** `feat(tui): one value-filled elapsed clock format`

## 7. Show the Exchange total beside the elapsed clock

**Closes:** turn-total-duration

**What:** second half of turn-total-duration. Depends on item 6.

**Regression guard.** The total gives way to every right-slot occupant (cancel/ctrl+c/flash hints, `esc×2 cancel`, breadcrumb hint): in statusLine, when the gap is < 1 or a hint would fall back to its short form, recompose the left slot without the total before dropping or shortening the right slot. Left-slot give-way chain: stalled row quiet+total → quiet → plain → truncate; unstalled row plain+total → plain → truncate. Render no total while the root slot's since is zero (as activity.elapsed guards its own clock). Tests use a new helper `backdateExchange(m, at)` that sets `m.acts.at(runRef{}).since`; `backdateActivity` stays unchanged. The ratified narrow-line call supersedes layout.md's "the quiet qualifier … is the first thing the slot gives up" and the matching statusLeftWithin comment: the prose guard covers every statement of the slot's give-way order, found with `grep -rnE 'first thing the slot gives|FIRST thing the slot gives' layout.md docs internal/tui`. Item 7 depends on item 6 and its doc examples use item 6's clock shape.

**Goal:** in the running state only, the status line reads `<phrase> · <clock> [total <formatElapsed(since Exchange start)>]` then the existing ` · N tok/s`, styled `m.th.statusBar`; the total keeps counting across activity changes, delegate fan-out and run views; idle, asking and every other state render byte-for-byte as before; on a narrow line the total drops first, then `quiet`, then the phrase truncates; no new timer.

**Approach (assumed at the header base):** the Exchange origin is `m.acts.at(runRef{}).since` (stamped by `enterRunning`, kept by `moveActivity`, cleared by `finishWorker`/`watchIdle`). Add a `totalSuffix(now)` beside `throughputSuffix` (`internal/tui/model.go`), composed in `statusLeftWithin`'s `stateRunning` case — not inside `runningPhrase`, whose `· 0s` tests stay valid. Candidate variants tried in order: quiet+total, quiet, plain, then truncation. Repaints ride `spinnerTickMsg`. Docs: `docs/manual/commands.md` status-line section, `layout.md` running-line examples, `docs/layout/prompt-box-layout.md` sketch.

**Files:** internal/tui/model.go, internal/tui/model_test.go, internal/tui/activity_test.go, docs/manual/commands.md, layout.md, docs/layout/prompt-box-layout.md
**Read first:** internal/tui/model.go — statusLeftWithin, statusLine, statusLeftBeside, escStopHint, throughputSuffix, runningPhrase; internal/tui/activity.go — runActivities.at, activity.elapsed;
internal/tui/activity_test.go — backdateActivity

**Tests:** backdate the Exchange (`backdateExchange`, a new helper beside `backdateActivity` in `internal/tui/activity_test.go`) and a child slot (`backdateActivity`) separately: total reads the root while the clock reads the shown slot; total survives `moveActivity` to a new kind; idle and asking `statusText` unchanged; narrow-width drop order (total gone, `quiet` kept); an unstalled row drops the total before truncating; at a width where an armed `press esc again to cancel` fits beside the plain phrase the hint stays full and the total is gone (likewise ctrl+c, flash, `esc×2 cancel`, breadcrumb hint); a running state whose root slot's since is zero renders no total; `statusText` contains `[total 03:15s]` at 3m15s.

**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestModelStatusLine|TestStatusLine|TestStatusPhrase' ./internal/tui/`

**Commit:** `feat(tui): show the Exchange's total duration beside the elapsed clock`
