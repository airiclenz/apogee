# Plan — Upstream Heartbeat: live model/context/server state, async startup, full rebind

**Date:** 2026-07-27
**Status:** complete (grilled with the owner 2026-07-27 — ten decisions recorded below; ground verified against the working tree same day)
**Source:** ISSUES.md — "model-, context and server information need to be updated on a timer of 10 sec … start apogee without a running server … model/server selection stays possible later."
**Track:** rides **v0.9.0** (current `VERSION` v0.8.6; minor — additive public surface, one behavioral change: startup no longer blocks or hard-fails on discovery).
**Public API:** additive (ADR 0010): `Agent.Rebind(RebindSpec) error` + `apogee.RebindSpec` alias on the root facade. New internal package `internal/heartbeat`. `tui.Options` gains two nil-able seam fields. `provider.Client` gains `SetModel`. Deleted: `provider.ServerManager` (dead code, item 6).
**Standing requirement:** `/coding-standards` is forwarded to the implementer and verifier sub-agents.

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Items run in order 1 → 7. The tree is coherent and green after every item; you may stop after any completed item. (3 needs 1; 4 needs 3, and 2 only conceptually — its seam is a func field, fake-testable; 5 needs 2 + 4 and is the first user-visible commit; 6 any time after 1; 7 last.)

**Deviations leave a trail.** Any deviation gets a dated `NOTES (YYYY-MM-DD):` paragraph directly under the item heading.

**Authoritative sources**, in precedence order:
1. This plan (encodes the owner decisions).
2. ADR 0011 (thin renderer; the two legal engine-call classes), ADR 0010 (package layout), ADR 0023 (per-model system prompt), ADR 0016 (validated sets), ADR 0021 (probe ≠ monitor).
3. CONTEXT.md domain language ("Upstream", "Probe"; the new noun "Heartbeat" lands in item 7).
4. The code as it stands.

---

## Owner decisions (grill, 2026-07-27)

1. **The bug is staleness.** The wrong context size / gauge is the frozen-at-startup display (B4 below), observed after background model switches via llama-launcher.
2. **Offline UX: block + clear status.** Distinct offline state in the footer; a submit while offline is rejected immediately with a clear transcript note and **the typed input is preserved**. Everything else (scrollback, `/clear`, `/sessions`, Shift+Tab…) stays usable. Never kills an in-flight exchange.
3. **Model change → transcript notice**: `model changed: X → Y, context 32k → 16k` (window clause only when it moved), plus the display/gauge update.
4. **Scope: seam + data layer.** Fetch the `/v1/models` list into TUI state every beat (for the future picker); build **no** model-picker or server-switch UI now, but make both easy to add.
5. **Engine depth: FULL REBIND NOW.** A model change re-resolves: outgoing request model id, per-model system prompt (ADR 0023), validated set (ADR 0016), mechanisms registry, compaction budget / `MaxContextTokens`. This deliberately pulls forward part of the `/server` blocker (TODO.md "[P1] Server / model switching").
6. **Cold start, `model:` unset, server down:** the first successful beat completes startup discovery late (**late seed = the same rebind code path**, from empty).
7. **Noun: "Heartbeat."** New concept, distinct from Probe (CONTEXT.md: probe "diagnoses, it does not monitor"). ADR + CONTEXT.md entry + type names.
8. **Startup fully async.** Delete the synchronous startup discovery (today: up to 5 s stall, hard fail when `model:` unset + server down). TUI paints instantly; the first beat fires immediately from `Init`. One code path for cold start / late seed / refresh.
9. **`context-window:` pin wins.** A pinned window is never overridden by the heartbeat (display or budget) — "leave unset to discover" stays the documented semantics. The heartbeat still refreshes model + reachability under a pin.
10. **`model:` pin is a hint.** Passed as the discovery hint (fixes B1); honored while the server serves that id; when the server's loaded model changes or the pin vanishes from `/v1/models`, rebind follows observed reality + notice.

---

## The ground (verified 2026-07-27 against the working tree)

**Discovery.** `provider.Client.Discover` (`internal/provider/discovery.go:53`, 5 s timeout at `:14`) probes `GET /v1/models` (fatal) then llama.cpp `GET /props` (best-effort; runtime per-slot `n_ctx` overrides the advertised window, `:62-64,103-151`). `ModelInfo{AvailableModels, ActiveModel, ContextWindow, RuntimeContextWindow}`; `toModelInfo(hint)` at `:168` picks the hint if present, else `models[0]`. Endpoints: `/v1/chat/completions`, `/v1/models`, `/props` only (`client.go:15-18`); no `/health`.

**Startup today.** `cmd/apogee/root.go:171-191`: no-model path → `resolveModel` (`config.go:1037`) is **fatal** on discovery failure; pinned-model path → `resolveContextWindow` (`config.go:1066`), non-fatal, window stays 0. A `context-window:` pin (`config.go:519`, file-only) suppresses probing entirely. Both production discovery callers pass an **empty** model hint (`wire.go:52`). Discovery blocks the first paint for up to 5 s.

**Frozen display (root cause).** `wire.go:320-326` builds static `tui.Options{Model, Endpoint, HostAlias, ContextWindow, …}` → copied into the TUI `Model` at construction; nothing ever writes it again (the only precedent mutation is `m.opts.Mode` on Shift+Tab, `model.go:470-476`). Footer `footerContent` (`model.go:1373`), startup box `newStartupView` (`:1398`, re-seeded from `m.opts` on `/clear` at `:695`), gauge denominator `contextGauge` (`:1563`), the engine budget (`wire.go:209` → `agent/loop.go:774`), and session metadata (`sessionHost.model`, `wire.go:318/549`) all keep launch-time values forever.

**Engine immutability.** `agent.upstream` and `cfg.Model` are fixed at construction (`internal/agent/agent.go:30`); the only goroutine-safe mutators are `SetMode`/`SetConfineToWorkspace`. `errMissingModel` (`construct.go:24,236`) forbids a model-less construction; no test pins it. **The provider client's configured model wins over the request's** (`client.go:234-238`) — mutating `cfg.Model` alone would not change the wire model. All scattered window/cfg readers (`loop.go`, `compact.go`, mechanisms via the per-call `loopView`→`Budget` rebuild) execute **only inside `Step`/`Compact` on the worker goroutine**.

**TUI patterns.** No polling exists anywhere (confirmed); `Init` returns only `m.input.Focus()` (`model.go:210`). The canonical self-rescheduling tick is the spinner's generation-counter chain (`spinner.go:369-382`, fold `model.go:365-374`). Msg types live in `messages.go` with a `var _ tea.Msg` compile-assert block. Injected seams precedent: `Engine`, `SkillCatalog`, `SessionHost` (`tui.go:19-120`). `internal/tui` must not import `internal/provider` (ADR 0010). The Model is value-copied — no self-pointer types by value (`TestModelNoBuilderByValue`). Fail-once posture precedent: `saveBusy`/`saveFailing` (`model.go:56-59,962-1066`). Usage source: server `usage` → `foldStats` sets `m.ctxUsed` from `TotalTokens` (`fold.go:47-77`).

**Bug inventory and disposition.**

| # | Bug | Disposition |
|---|---|---|
| B1 | Pinned-model startup probes with empty hint → wrong model's window on multi-model servers (`wire.go:52`) | **Fixed** (item 1: Monitor carries the config hint) |
| B2 | `/props` `n_ctx` is per-slot; `total_slots` never read | Documented in ADR 0024, not changed |
| B3 | Pinned `context-window:` silently suppresses probing | **By design** per decision 9; docs sharpened (item 5) |
| B4 | `Options.Model/ContextWindow` frozen at construction — the reported staleness | **Fixed** (items 3–5) |
| B5 | Gauge `%` text unclamped while the bar clamps (`model.go:1590`) — prints "41k 137%" | **Fixed** (item 3) |
| B6 | Gauge uses `TotalTokens`, engine `Budget.Used` uses prompt-side estimate | Explicit non-goal; documented |
| B7 | `formatTokens` truncation (131072→"131k"); narrow startup box drops the context row | Cosmetic, untouched |
| B8 | Startup blocks up to 5 s on discovery | **Fixed by design** (decision 8, item 5) |

Dead code: `provider.ServerManager` (`server.go`) — never constructed in production (verified); deleted in item 6.

---

## Decisions taken (mechanical — grounded, with rationale)

- **Rebind synchronizes by boundary, not by lock.** All un-mutexed `cfg` readers run only inside `Step`/`Compact` on the worker goroutine. The TUI already has an ADR-0011-blessed class of idle-only engine calls (`ClearContext`, `RestoreSession`) where the worker's terminal Msg through the Bubble Tea channel establishes happens-before both ways. `Agent.Rebind` joins exactly that class: it refuses mid-exchange; the TUI stashes a mid-exchange change as `pendingRebind` and applies it in the exchange-terminal fold. No new mutex on `cfg`, no locking in `budget()`, `-race` stays green with zero hot-loop changes. The two genuinely concurrent surfaces get real guards: `provider.Client.SetModel` (mutex — the Client documents concurrent use) and `sessionHost.SetModel` (its existing `mu` — `Save` runs on a Cmd goroutine).
- **Tick topology: re-arm from the landed beat.** First beat fires immediately from `Init`; the `beatMsg` fold schedules the next tick 10 s later; the tick re-issues the beat Cmd. 10 s *after completion* ⇒ overlap impossible (and `Discover`'s 5 s timeout keeps a beat strictly shorter than the interval anyway). Generation counter (spinner pattern) makes stale ticks inert. `heartbeat.Interval = 10 * time.Second` is a named const, **no config key** (owner fixed 10 s; one field later if ever wanted).
- **Offline debounce.** A failed beat while an exchange is in flight is **ignored** (a live stream is stronger evidence than a timed-out `/v1/models` on a saturated single-slot server). A failure before any success flips offline immediately (a cold start says so honestly). After a success, offline only after **2 consecutive idle failures** (~15–25 s) — one 5 s discovery timeout under load must not flicker the footer. Transitions note once each way (the `saveFailing` fail-once posture).
- **Validate-then-commit rebind.** `Rebind` builds the fresh mechanisms registry and validates everything before mutating anything; any error leaves the old bindings fully intact.
- **`apogee probe` is untouched.** Its hint-less resolution is documented behavior (`internal/probe/discovery.go:31-33`); probe output stays byte-identical.
- **`model-profile` is global** (not per-model) — explicitly unchanged by rebind; stated in its doc comment.
- **`ServerManager` is deleted, not reused.** Its liveness half is superseded by the heartbeat (which observes more); its launch half belongs to the future local-server start/stop TODO item and should be rebuilt over `heartbeat.Beat` then. Two reachability concepts would violate the sharp-nouns rule.

---

## 1. `internal/heartbeat` — the Beat observation and the discovery-backed Monitor — ✅ DONE (2026-07-27)

**What.** New package `internal/heartbeat` (files `heartbeat.go`, `heartbeat_test.go`, ~80 lines production code). It owns the domain noun, the cadence, and the production beat source over `provider.Client.Discover` — constructed **with the config model hint** (the B1 fix). Package doc states the probe/heartbeat distinction (ADR 0021 / CONTEXT.md: probe diagnoses once on demand; the heartbeat observes continuously).

```go
// Interval is the monitor's cadence. Named const, not config — the owner fixed 10 s.
// Discover's own 5 s timeout keeps a beat strictly shorter than the interval, and the
// TUI re-arms only from a landed beat, so beats can never overlap.
const Interval = 10 * time.Second

// ModelSummary is one advertised model (/v1/models), kept in TUI state for the future picker.
type ModelSummary struct {
    ID, DisplayName string
    ContextWindow   int
}

// Beat is one observation of the upstream. Never an error: an unreachable server is a
// finding, not a failure (the probe.Discover posture).
type Beat struct {
    Reachable       bool
    Failure         string // why unreachable; "" when Reachable
    ActiveModel     string
    ContextWindow   int // /props runtime window overriding /v1/models (provider.Discover)
    AvailableModels []ModelSummary
}

// Monitor is the production beat source. modelHint is the config-pinned model id ("" unpinned):
// Discover resolves the pin's model AND window while served (B1 fix), falling back to the
// server's first model when it vanishes — decision 10's observed-reality fallback.
type Monitor struct{ client *provider.Client }
func NewMonitor(endpoint, modelHint string) *Monitor
func (m *Monitor) Beat(ctx context.Context) Beat
```

**Tests** (`internal/heartbeat/heartbeat_test.go`; copy the httptest helpers' shape from `internal/provider/discovery_test.go:14-50`):
- `TestBeatCarriesDiscovery` — two-path httptest server: Beat reports Reachable, active model, the `/props`-overridden window, the full model list.
- `TestBeatHintPinsActiveWindow` — two-model payload + hint on the second: `ActiveModel`/`ContextWindow` are the hinted model's, not `models[0]`'s (pins the B1 fix).
- `TestBeatHintVanishedFallsBack` — hint absent from the payload: `ActiveModel` is the server's first model (decision 10).
- `TestBeatUnreachableIsObservation` — closed listener: `Reachable: false`, non-empty `Failure`, no error, no panic.

**Acceptance.** The green gate. `grep -rn "internal/heartbeat" internal/tui` empty (the seam lands in item 3); package imports `provider`, nothing imports it yet.

**commit.** `feat(heartbeat): the Beat observation and the discovery-backed Monitor`

---

## 2. Engine full rebind — `Agent.Rebind`, `Client.SetModel`, deferred model binding — ✅ DONE (2026-07-27)

NOTES (2026-07-27): three deviations from the item's literal text.
1. `errMissingModel` **was** pinned by a test, contrary to the item's "no test pins it":
   `apogee_test.go`'s `TestNew_RequiresMinimumConfig/missing_Model`. That table row is deleted and
   replaced by `TestNew_ModelMayBeBoundLater` (empty `Model` constructs → `Submit` refuses → `Rebind`
   → `Submit` flows), which pins the new contract through the public facade.
2. The mid-Exchange refusal returns `domain.ErrInputPending` (the item named no error): `Rebind`
   joins the `ClearContext`/`RestoreSession` idle-only class, and that is the error that class uses.
3. Two additions the item implied but did not list: `_ apogee.RebindSpec` in `example_test.go`'s
   completeness guard (that file's own contract — every alias must be named there), and
   `TestRebindRefusesUnbuildableSpecs` covering the two refusals the implementation notes mandate
   (`spec.Model == ""`, pre-built `Config.Mechanisms`), which the test list did not name.

**What.** The engine half plus the construction relaxation async startup needs. In `internal/provider/client.go`: add `modelMu sync.RWMutex` + `SetModel(string)`; route the two `c.model` reads (`buildBody` `:234`, `discoverModels` `:91`) through a locked accessor so the Client's documented concurrent-use contract stays literally true. In `internal/agent` (new `rebind.go` + touches to `agent.go`, `construct.go`):

```go
// RebindSpec carries the per-model bindings the composition root re-resolved for an
// observed model change (ADR 0024). Computed WHOLE by the caller — per-model system
// prompt (ADR 0023) and validated set (ADR 0016) resolution live in cmd/apogee, not here.
type RebindSpec struct {
    Model            string               // required
    SystemPrompt     string               // re-selected template; "" ⇒ none
    MaxContextTokens int                  // the BOUND window (context-window: pin already applied); 0 ⇒ unknown
    EnableMechanisms []domain.MechanismID // re-resolved set for the new model
}

// Rebind swaps the Agent's per-model bindings at a quiescent boundary. Like
// ClearContext/RestoreSession it is idle-only (refuses mid-exchange) — boundary
// discipline, not a lock, keeps the loop's un-mutexed cfg reads race-free (ADR 0024).
// Conversation, mode, approvals, confinement, model-profile/parsers, and tools stand.
func (a *Agent) Rebind(spec RebindSpec) error
```

Implementation notes — validate-then-commit, nothing mutated on any error path: refuse `inExchange`; refuse `spec.Model == ""`; refuse `a.cfg.Mechanisms != nil` (an embedder-supplied pre-built registry cannot be rebuilt — honest error, never hit by the TUI wiring, which only sets `EnableMechanisms`); `prompt.Validate(spec.SystemPrompt)`; build a **fresh** registry via the existing `buildEnabledMechanisms` (`construct.go:116`) over a cfg copy carrying `spec.Model` (so `deriveDeps`' fingerprint re-keys, `construct.go:165`) and run the three `Validate*` gates. Commit: `a.cfg.Model/.SystemPrompt/.Context.MaxContextTokens/.EnableMechanisms`, `a.registry = fresh`, optional-interface `a.upstream.(interface{ SetModel(string) })` (fake responders in tests simply don't implement it), `a.tokens = apogeectx.NewTokenEstimator()` (fresh calibration), `a.compactSat = false` (the saturation latch was judged against the old window). Construction relaxation: drop `errMissingModel` from `validateConfig` (`construct.go:24,236`; no test pins it); add `errNoModelBound` guarding `Submit` (`agent.go:127`) so a model-less request never reaches the wire; update `Config.Model` doc in `internal/domain/config.go` ("may be empty when the host late-binds via Rebind"). Root facade: `type RebindSpec = agent.RebindSpec` in `apogee.go`.

**Tests** (new `internal/agent/rebind_test.go`, fake-responder style of existing agent tests, all under `-race`; plus one in `internal/provider/client_test.go`):
- `TestRebindSwapsRequestBindings` — after Rebind, the next captured request carries the new model id, new system message, and `Budget.ContextLimit` = new window.
- `TestRebindRefusedMidExchange` — `inExchange` ⇒ error, every binding unchanged.
- `TestRebindRebuildsMechanismsForNewModel` — new `EnableMechanisms` swaps the registry; a gate failure leaves OLD registry and cfg intact.
- `TestRebindKeepsConversation` — history identical across a rebind.
- `TestRebindBetweenExchangesRaceClean` — two full exchanges with Rebind between, across a channel handoff (mirrors the terminal-fold boundary); exists to run under `-race`.
- `TestNewAllowsEmptyModelSubmitRefuses` — `New` with `Model: ""` succeeds; `Submit` returns `errNoModelBound`.
- `TestClientSetModelSwapsWireModel` — after `SetModel`, the next request body's `model` field carries the new id (pins `client.go:234-238`).

**Acceptance.** `go test -race -run 'Rebind|NoModelBound|SetModel' ./internal/agent ./internal/provider` green; `grep -n errMissingModel internal/agent/construct.go` empty.

**commit.** `feat(agent): full engine rebind at the quiescent boundary + deferred model binding`

---

## 3. TUI heartbeat loop — tick chain, offline state, submit block, display fixes — ✅ DONE (2026-07-27)

NOTES (2026-07-27): three deviations/additions from the item's literal text.
1. The acceptance line "all pre-existing `internal/tui` tests pass **unmodified**" could not hold
   literally: `TestDisplayModel` (`model_test.go`) PINNED the `displayModel("") == "."` bug the item
   fixes, with the comment "never reached in practice". That one table row is deleted (the case is
   now covered by the item's own `TestDisplayModelEmpty`) and the test's doc comment points at it.
   Every other pre-existing test in the package passes untouched.
2. The `runCommand` gate is one check above the switch, keyed on the two Exchange-opening verbs
   (`continue`, `compact`), rather than a copy inside each case — same behaviour, one wording of the
   refusal. `submit`'s check sits after the blank-message guard, so an empty ⏎ while offline stays
   the silent no-op it already was.
3. Two additions the item implied but did not list: the `beatMsg` fold repaints only when the fold
   actually noted a transition (a repaint on every beat would drop a live drag-selection every ten
   seconds — `refreshViewport` clears `transcriptSel`), and `internal/tui/doc.go` gains the
   heartbeat paragraph the package's file-by-file narration convention requires.

**What.** The TUI half, deliberately rebind-free (bindings don't move yet, so the footer never shows an observed-but-unbound model — the tree stays honest). `tui.Options` gains `Heartbeat func(context.Context) heartbeat.Beat` (nil ⇒ unwired: no chain starts, every existing test unaffected) — the `SkillCatalog`-style narrow seam; `internal/tui` imports `internal/heartbeat`, never `internal/provider`. `Model` gains a plain-value `hb heartbeatState{gen, failures int; offline, everOnline bool; lastFailure string; models []heartbeat.ModelSummary; observedModel string; observedWindow int}` (value-copy-safe). `messages.go` gains `heartbeatTickMsg{gen int}` + `beatMsg{gen int; beat heartbeat.Beat}` and the compile-assert entries. Tick topology: `newModel` arms `hb.gen = 1` when wired; `Init` (`model.go:210`) becomes `tea.Batch(m.input.Focus(), m.beatCmd())`; the `beatMsg` fold schedules `tea.Tick(heartbeat.Interval, …)`; the tick re-issues `beatCmd`; both drop stale generations (spinner chain shape, `spinner.go:369-382`). The beat Cmd captures `m.parent` so shutdown cancels an in-flight beat. Offline policy per the debounce decision above; transitions note once each way: `server offline — <failure>` / `server back online`. Submit block: `blockedUpstream()` = `opts.Heartbeat != nil && (m.hb.offline || m.opts.Model == "")`, checked at the top of `submit()`'s message path and `runCommand`'s `continue`/`compact` cases — transcript note (`cannot send — server offline (<endpoint>): <failure>`, or `…still connecting to <endpoint>` pre-bind), **typed input preserved** (no editor reset); `/clear`, `/sessions`, `/version`, `/confine`, Shift+Tab stay live. Display: `footerContent` (`model.go:1373`) appends a styled `offline` segment when offline and shows `connecting…` in place of model ✦ ctx while `Model == ""`; successful beats stash `AvailableModels` into `hb.models` (decision 4's data layer — no UI). Two cheap fixes ride along: `displayModel("")` returns `""` (today `filepath.Base("")` = `"."`, live on every cold start), and **B5**: `contextUsage.view` (`model.go:1590`) clamps the `%` text to 100 to match the already-clamped bar.

**Tests** (new `internal/tui/heartbeat_test.go`; `newTestModel`/`step` harness `model_test.go:29-80`; chain tests copy `TestSpinnerTickChainGeneration`, `spinner_test.go:609+`):
- `TestInitFiresImmediateBeat` — fake Heartbeat: Init's Cmd yields `beatMsg{gen:1}`, fake called once.
- `TestBeatChainReArmsAndStaleGenIsInert` — live-gen `beatMsg` returns non-nil Cmd; stale-gen `beatMsg`/`heartbeatTickMsg` changes nothing, schedules nothing.
- `TestHeartbeatUnwiredIsInert` — nil seam: Init returns only the focus Cmd (protects every existing test).
- `TestColdStartFailureIsOfflineImmediately` — first-ever failed beat ⇒ offline + one note.
- `TestOfflineDebouncesAtIdle` — online → one idle failure stays silently online; the second flips offline with exactly one note.
- `TestBusyFailureNeverFlipsOffline` — failed beat in `stateRunning` leaves state and counter untouched.
- `TestRecoveryNotesOnce` — offline → success ⇒ online + one note; further successes add nothing.
- `TestSubmitBlockedOfflineKeepsInput` — offline submit: no worker Cmd, `m.input` value preserved, note added; `/clear` still works.
- `TestSubmitBlockedBeforeFirstBind` — `Model: ""` + wired heartbeat blocks with the connecting note.
- `TestFooterShowsOfflineAndConnecting` — footer substrings via `plain(view)` for both states.
- `TestGaugePercentClamped` — `contextUsage{Used: 45000, Limit: 32768}` renders `100%`, not `137%` (B5).
- `TestDisplayModelEmpty` — `displayModel("")` == `""`.

**Acceptance.** All pre-existing `internal/tui` tests pass **unmodified** (nil-seam inertness proven, not assumed).

**commit.** `feat(tui): heartbeat tick chain, offline state with idle-only debounce, and the upstream submit block`

---

## 4. TUI rebind orchestration — late seed, deferred apply, transcript notice — ✅ DONE (2026-07-27)

NOTES (2026-07-27): five deviations/additions from the item's literal text, all in the WORDING half.
1. A beat that crosses back online AND rebinds writes only the bind note — the item-3 `server back
   online` line is suppressed for that one fold. This item's acceptance demands it ("offline note,
   connected note, (nothing), changed note" for the script `offline → model A → model A → model B`),
   and "connected: A" already implies the server answered. Item 3's recovery note is untouched in
   every other case (`TestRecoveryNotesOnce` passes unmodified — it wires no Rebind).
2. The window-only change needed a third wording the item did not give (it named only the late-seed
   and model-change lines while asking for a "window-only notice"): `context window changed: 32k →
   16k`. A rebind whose BOUND values did not move — the pinned-window case, where the server resized
   and the pin outranked it — writes no note at all, since none of it is visible to the human.
3. The failure note has a second form for the late seed: `still bound to X` cannot be said when
   nothing is bound, so the pre-bind refusal reads `could not bind Y: <err> — no model is bound yet`.
   The bound case is the item's literal sentence.
4. Model ids in all four notes are rendered through `displayModel` (the footer's and start-up box's
   own rendering), so a note and the chrome beside it can never name one model two ways.
5. Four tests beyond the item's list: `TestBeatScriptNarratesEachChangeOnce` (the acceptance script,
   asserted as the exact note sequence), `TestUnknownWindowNotedOnBind` and
   `TestRebindNoticesSurfaceAsNotes` (the two note paths the item mandates but did not list a test
   for), and `internal/tui/doc.go` gains the rebind paragraph the package's narration convention
   requires.

**What.** The apply half. `tui.Options` gains the second seam:

```go
// Rebind re-resolves and applies the per-model bindings for an observed model/window
// change — the binary owns per-model config resolution (ADR 0023/0016) and the engine
// mutators; the TUI owns only WHEN (idle, or deferred to the exchange-terminal fold).
// Returns what was actually BOUND (the context-window: pin may override the observed
// window) plus per-session notices. nil ⇒ display-frozen heartbeat (bindings never move).
Rebind func(model string, contextWindow int) (RebindResult, error)

type RebindResult struct {
    Model         string
    ContextWindow int      // bound window (pin wins); 0 ⇒ unknown
    Notices       []string // validated-set lines etc., surfaced as transcript notes
}
```

Trigger (successful `beatMsg` folds only): `changed := beat.ActiveModel != m.hb.observedModel || (beat.ContextWindow > 0 && beat.ContextWindow != m.hb.observedWindow)` — compared against the last **observed** values, recorded when the intent is captured, so a pinned window never re-triggers every 10 s and the pin needs no TUI-side knowledge. Changed and `!m.busy()` ⇒ apply now; busy ⇒ stash `pendingRebind` (latest-wins), applied in `finishWorker` (`model.go:853`), the same boundary where `AbortExchange`/`saveAtIdle` already sit. `applyRebind` calls `opts.Rebind`; on success: update `m.opts.Model`/`m.opts.ContextWindow` (the Shift+Tab `m.opts.Mode` precedent), refresh the seeded startup box in place via a new `transcript.refreshStartup(newStartupView(m.opts))` (its values are frozen at seed time — without this a late seed leaves stale placeholders until `/clear`), and note: **late seed** (old model `""`, decision 6 — same code path, different words): `connected: <model>, context 32k`; **change** (decision 3): `model changed: X → Y, context 32k → 16k` (window clause only when moved); plus `RebindResult.Notices`; plus, when the bound window is 0, the relocated honesty line `context window unknown — automatic compaction and the Budget are inactive; set context-window: in config.yaml` (replaces the startup stderr notice item 5 deletes). On error: `model change detected (X → Y) but rebind failed: <err> — still bound to X`, noted once per distinct target (`lastRebindFailed string` guard — no 10-second spam). `ctxUsed` is left alone (the conversation survives the switch; an over-window fill renders clamped at 100% until the next usage event or compaction).

**Tests** (extend `internal/tui/heartbeat_test.go`, same harness):
- `TestLateSeedBindsThroughRebind` — `Model: ""` + beat with a model ⇒ fake Rebind called with `(model, window)`; `m.opts` adopts the result; "connected:" note; footer AND startup box show the model.
- `TestModelChangeRebindsWithNotice` — bound X, beat Y ⇒ `model changed: … X → Y` with both windows; footer shows Y.
- `TestWindowOnlyChangeRebinds` — same model, new non-zero window ⇒ rebind fires, window-only notice.
- `TestZeroWindowBeatIsNotAChange` — beat with `ContextWindow: 0`, same model ⇒ nothing.
- `TestRebindDeferredWhileBusy` — change during `stateRunning` ⇒ no call; `exchangeDoneMsg` ⇒ applied in the fold; two differing busy-time beats apply only the latest.
- `TestPinnedWindowResultSticks` — fake Rebind returns the pinned window; display shows the pin; identical subsequent beats trigger no further calls.
- `TestRebindFailureNotedOnce` — failing Rebind notes once; identical beats silent; a different target notes again.
- `TestRebindNilIsDisplayFrozen` — `Rebind: nil` + model change ⇒ no panic, bindings and footer unchanged.

**Acceptance.** With a fake beat script `offline → model A → model A → model B`, the transcript reads exactly: offline note, connected note, (nothing), changed note — no duplicates.

**commit.** `feat(tui): full rebind orchestration — late seed, deferred apply, and the model-change notice`

---

## 5. Startup surgery + composition-root wiring — the feature goes live — ✅ DONE (2026-07-27)

NOTES (2026-07-27): four deviations/additions from the item's literal text.
1. Deleting `contextWindowNotice` broke a test the item did not name: `TestRunRootThreadsContextWindow`
   (`wire_test.go`) observed `opts.contextWindow → Config.Context.MaxContextTokens` THROUGH that
   stderr notice, and the Agent exposes no accessor for the field. It is retimed rather than deleted:
   it now pins the two surfaces that still read the value — `tui.Options.ContextWindow` and the wired
   rebind closure, where the pin must outrank the observation — and its doc comment points at
   `TestUnknownWindowNotedOnBind` (item 4) for the notice half.
2. Two doc comments named `contextWindowNotice` as the "pure so it is table-testable" precedent
   (`shouldPrewarmLabelWalk`, `appliedNotice`); both now name `probe.DegradedNotice` instead, so the
   deletion leaves no dangling reference.
3. `README.md`'s context-window paragraph claimed the window is "discovered from the server at
   startup" and that apogee "says so once at startup" — both false after this item, and README is
   enumerated by neither this item nor item 7. The three sentences are corrected here (live
   discovery, `context-window:` as a pin, the notice now in the transcript).
4. `TestRebindSpecForSelectsPerModelBindings` asserts the enable list through a per-case check func
   rather than a literal slice: the applying case is the shipped gemma entry, whose exact membership
   is pinned by `validated`'s own tests and would be a brittle copy here.

**What.** Remove the synchronous startup discovery and wire both seams. `cmd/apogee/root.go:171-191`: delete the `resolveModel`/`resolveContextWindow` calls and the "discovered model" stderr line — `RunE` goes `seedDefaultConfig → applyConfig → runRoot`; the TUI paints instantly. `cmd/apogee/config.go:1013-1077`: delete `discoveredUpstream`, `modelDiscoverer`, `resolveModel`, `resolveContextWindow` (+ their tests); the `context-window:` pin layering (`config.go:519`) is untouched, and since discovery no longer pre-fills `opts.contextWindow`, **`opts.contextWindow > 0` now ⇔ pinned** — the fact the rebind closure relies on. `cmd/apogee/wire.go`: delete `discoverUpstreamModel` (`:51`) and `contextWindowNotice` (`:64` + call at `:231` — the honesty line moved into the TUI, item 4, where it fires at the right moment instead of wrongly on every cold start); `resolveSystemPrompt` (`:169`) now runs with a possibly-empty model (selects the global template; the per-model entry lands via the first beat's rebind, seconds later); keep `resolveValidatedSet` as-is (empty model ⇒ no identity ⇒ no set — correct until bound) but **hoist the manual `mechanismIDs` list into a local** before the validated-set overwrite (`:293-295`) so the rebind closure can re-run the "manual list suppresses the set" rule per new model. Wire:

```go
mon := heartbeat.NewMonitor(opts.endpoint, opts.model) // opts.model is now ONLY the config pin — decision 10, and the B1-fixing hint
pinnedWindow := opts.contextWindow                     // > 0 ⇔ context-window: pin (decision 9)
rebind := func(model string, window int) (tui.RebindResult, error) {
    spec, notices, err := rebindSpecFor(opts, roots, manualIDs, model, window, pinnedWindow) // pure-ish, table-testable
    if err != nil { return tui.RebindResult{}, err }
    if err := agent.Rebind(spec); err != nil { return tui.RebindResult{}, err }
    host.SetModel(model)
    return tui.RebindResult{Model: spec.Model, ContextWindow: spec.MaxContextTokens, Notices: notices}, nil
}
// tui.Options: Heartbeat: mon.Beat, Rebind: rebind — Model/ContextWindow now honestly "" / pin-or-0 at launch
```

`rebindSpecFor` re-runs `resolveSystemPrompt` (keyed on the new model) and `resolveValidatedSet` (opts copy with the new model), applies the pin (`pinnedWindow > 0 ⇒ MaxContextTokens = pinnedWindow`, else the observed window), never touches `Profile` (`model-profile` is global — say so in its doc comment). `sessionHost` gains `SetModel(string)`; `Save` reads `h.model` under the existing `mu` (`wire.go:489-553`) so session metadata follows the rebind (the resumed-session seed at `:318` keeps working). `cmd/apogee/defaults/config.yaml`: update the `# model:` comment ("a hint — apogee follows the model the server actually serves; the heartbeat rebinds on a switch") and `# context-window:` ("a pin — the 10-second heartbeat never overrides it; leave unset to discover, live"). `apogee probe` (`probe.go`/`probemodel.go`) untouched.

**Tests** (`cmd/apogee/root_test.go` / `wire_test.go`, replacing the deleted discovery-path tests; one cross-layer e2e in `internal/tui`):
- `TestRootStartsWithNoModelAndNoServer` — no model, unreachable endpoint: the fake launcher IS invoked with `Options.Model == ""`, no error (the old fatal-path test inverted — decision 8's headline).
- `TestRootMakesNoStartupProbe` — httptest endpoint with a request counter: zero requests before `launch` is called.
- `TestRebindSpecForSelectsPerModelBindings` — table: per-model `system-prompt-models` entry selected; validated set applied when the manual list is empty and suppressed when not; `pinnedWindow` wins over observed; unpinned adopts observed.
- `TestSessionHostSetModelStampsSaves` — after `SetModel("new")`, the next `Save`'s `Meta.Model` is `"new"`.
- `TestE2EColdStartHeartbeat` (`internal/tui/e2e_test.go` style, new func) — real `agent.New` with `Model: ""`, real `heartbeat.Monitor` against a flip-able httptest server (404 everything → then models + a scripted exchange): Init's beat ⇒ offline + submit blocked; flip up; next beat ⇒ late-seed rebind (wire-shaped closure inline) ⇒ footer bound; submit ⇒ a real exchange completes. The deliverable proof of decisions 2, 6, 8 in one test.

**Acceptance.** `time ./apogee --endpoint http://127.0.0.1:1` (nothing listening) paints the TUI in < 1 s with `connecting…`/offline footer and a blocked-submit note — versus today's up-to-5 s stall and hard fail.

**commit.** `feat(cmd): fully async startup — wire the heartbeat and the per-model rebind closure`

---

## 6. Remove the dead `ServerManager` — ✅ DONE (2026-07-27)

NOTES (2026-07-27): one addition beyond the item's literal text. `internal/provider/doc.go`'s
package doc advertised "a local server-process manager" as part of what the package owns — false
the moment `server.go` goes. The clause is dropped and replaced by a pointer to
`internal/heartbeat` as the thing that watches the Upstream over time. (`docs/design/technical-design.md`
and `TODO.md` also name `ServerManager`; both are item 7's enumerated territory and are left alone.)

**What.** Delete `internal/provider/server.go` + `server_test.go` (zero references outside its own file+test, verified 2026-07-27). Rationale recorded under "Decisions taken". The technical-design "Provider / Upstream" row's `ServerManager` mention is amended in item 7.

**Tests.** None added; the deletion is the change.

**Acceptance.** `grep -rn ServerManager --include='*.go' .` empty; cross-build matrix green.

**commit.** `chore(provider): remove the dead ServerManager — the heartbeat is the reachability observer`

---

## 7. Docs, decision record, and release bookkeeping — ✅ DONE (2026-07-27)

**NOTES (2026-07-27):** the `TODO.md` half of this item is already done — an owner-requested
TODO cleanup rewrote the file the same day: the stale "upstream is immutable" note is gone and
**[P1] Server / model switching** is already marked IN FLIGHT with the item 1–3 / 4–7 split and
the non-goals. The quoted `:53-55` / `:92-95` line numbers no longer apply. When this item runs,
only *finalize* that entry's wording against what actually shipped (drop "IN FLIGHT", record what
remains); every other target of this item (ADR 0024, CONTEXT.md, ISSUES.md, CHANGELOG.md,
technical-design.md) is untouched and still to do.

**NOTES (2026-07-27, implementation):** four deviations from the item's literal text.
1. `ISSUES.md` was rewritten by the owner mid-plan into an `A`/`P`/`X` status legend (Activated /
   Planned / Executed), so "check off the 10-second-timer item" is realized as `[P]` → `[X]` plus the
   shipped paragraph the item asks for (ADR 0024 pointer, B5/B1 fixed, B6 open by design). The
   owner's wording of the entry itself is preserved verbatim.
2. The `TODO.md` half is as the note above predicted: the "upstream is immutable" line was already
   gone, so only the `[P1] Server / model switching` entry's wording was finalized (IN FLIGHT
   dropped, shipped/remaining split recorded), plus a shipped-ledger line in the same entry's
   "Shipped since parking" list.
3. `docs/design/technical-design.md`: besides the §5 Provider row and the new Heartbeat row the item
   names, the §8 backlog's completed "Provider/Upstream client" line (`:314`) — the doc's only other
   `ServerManager` mention — gains a one-clause dated pointer, so item 6's deletion leaves no claim
   in this file that the type still exists. The Provider row's *Undesigned* cell was corrected in the
   same edit (llama.cpp `/props` discovery shipped 2026-06-28; PID-file orphan adoption re-tagged to
   the parked local-server work).
4. `VERSION` is deliberately **not** bumped: it is not one of this item's enumerated targets, the
   owner moved it concurrently (v0.8.6 → v0.8.7), and the `[Unreleased]` block names the additive
   surface as a **minor** bump rather than hard-coding `v0.9.0` — the release header carries the
   number when the release is cut.

**What.** ADR **0024** `docs/adr/0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md`: the 10 s monitor as a concept distinct from probe (ADR 0021); the beat-result-re-arms tick topology; the idle-only/deferred-apply concurrency argument (why no locks on `cfg` — the boundary IS the synchronization, extending ADR 0011's idle-only class); pin-wins (decision 9); model-as-hint (decision 10); the busy-failure/2-idle-failures debounce; the display-only-vs-rebind split of the two Options seams; deliberate non-goals (B6; B2 per-slot `n_ctx` documented, not re-derived). `CONTEXT.md`: a **Heartbeat** noun entry near Probe (`CONTEXT.md:608-644`) — probe diagnoses once, the heartbeat monitors continuously, rebind is its apply; _Avoid_: "health check", "poller", "probe" for this — plus a cross-reference from the Probe entry. `TODO.md`: rewrite the stale "upstream is immutable" note (`:53-55`); mark **[P1] Server / model switching** (`:92-95`) partially landed (heartbeat + `Agent.Rebind` + model-list data layer shipped; `/server` + `/model` picker UI and local-server start/stop remain, now unblocked). `ISSUES.md`: check off the 10-second-timer item with a pointer to ADR 0024; note the gauge half is B5-fixed and B6 stays open by design. `CHANGELOG.md` `[Unreleased]`: the Heartbeat block (async startup, offline UX, live rebind + notice, `/v1/models` data layer, B5 fix, ServerManager removal) — rides **v0.9.0**. `docs/design/technical-design.md` §5: new **Heartbeat / upstream monitor** row (pointing at ADR 0024); amend the Provider row.

**Tests.** None (docs); `make check` still runs.

**Acceptance.** `ls docs/adr | tail -1` → 0024; `grep -n "Heartbeat" CONTEXT.md TODO.md CHANGELOG.md docs/design/technical-design.md` all hit.

**commit.** `docs: ADR 0024 — the heartbeat observes upstream; rebind applies at the boundary`

---

## Explicitly NOT in this plan

- **Model-picker / `/model` / `/server` UI** — future; `hb.models`, `RebindSpec`, and `Agent.Rebind` are its prepared seams.
- **Server switch (endpoint change)** — `Rebind` deliberately never touches `Endpoint`/`upstream` construction; `errMissingEndpoint` stands. A future endpoint switch swaps the Monitor + client behind the same two seams.
- **B6** — gauge counts `TotalTokens`, engine `Budget.Used` counts prompt-side estimate; documented divergence, untouched.
- **B2** — `/props` per-slot `n_ctx` semantics documented in ADR 0024, not re-derived; `total_slots` still unread.
- **B7** — `formatTokens` truncation and the narrow startup box dropping the context row; cosmetic.
- **Heartbeat interval config key** — named const; one field later if wanted.
- **Local llama.cpp start/stop** — remaining P1 scope; rebuild over `heartbeat.Beat` when it comes.
- **Typing/steering while busy, static cursor, `@file` with spaces** (other ISSUES lines) — unrelated, untouched.

## Critical files

- `internal/heartbeat/heartbeat.go` (new) — Beat, Monitor, Interval.
- `internal/agent/rebind.go` (new), `internal/agent/agent.go`, `internal/agent/construct.go` — Rebind, Submit guard, relaxed validation.
- `internal/provider/client.go` — `SetModel` + locked model reads.
- `internal/tui/tui.go` — the two Options seams; `internal/tui/model.go` — tick chain, folds, offline state, submit block, footer/gauge, `finishWorker` apply; `internal/tui/messages.go`, `internal/tui/transcript.go` (`refreshStartup`).
- `cmd/apogee/root.go`, `cmd/apogee/config.go`, `cmd/apogee/wire.go` — startup surgery, `rebindSpecFor`, wiring, `sessionHost.SetModel`; `cmd/apogee/defaults/config.yaml` — comment updates.
- `docs/adr/0024-…`, `CONTEXT.md`, `TODO.md`, `ISSUES.md`, `CHANGELOG.md`, `docs/design/technical-design.md`.

## Verification (whole plan)

Manual live run against the llama-launcher host (`http://192.168.64.1:1111`, MCP at `:7331/mcp` for server control):

1. Stop the server. `apogee --endpoint http://192.168.64.1:1111` with `model:` unset — TUI paints instantly; footer shows `<host> ✦ connecting…` then the offline state after the first beat; Enter on a typed message → "cannot send — server offline…" note, **input still in the box**; `/sessions`, `/clear`, Shift+Tab all work.
2. Start the server with model A (llama-launcher MCP). Within ≤ 10 s: `connected: A, context 32k`; footer/startup box show A ✦ 32k; Enter on the preserved message → a normal exchange runs.
3. While generating, saturate the server briefly — the footer must NOT flicker offline (busy-failure rule).
4. Switch models via llama-launcher (A → B, different `-c`). Within ≤ 10 s at idle: `model changed: A → B, context 32k → 16k`; footer + gauge denominator update; validated-set notice if B matches; the next request on the wire carries B (check llama-launcher `tail_log`). Repeat the switch mid-generation: the notice lands only after the exchange finishes.
5. Set `context-window: 8192`, restart, switch models again — the window stays 8192 and the notice omits the window clause; the model still rebinds.
6. Kill the server at idle: one grace beat, then offline after the second failure (~20–25 s), submits blocked; restart → `server back online`, submits flow. `apogee probe` output before/after the whole feature is byte-identical against the same server.

Automated: the per-item green gate after every item; `TestE2EColdStartHeartbeat` is the end-to-end proof in CI.
