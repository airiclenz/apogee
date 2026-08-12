---
Status: accepted
---

# The heartbeat observes the Upstream; rebind applies the change at the boundary

## Context

Apogee learned what model it was talking to **exactly once**, at startup. `runRoot` called
`provider.Client.Discover` before the first paint, wrote the answer into `tui.Options{Model,
ContextWindow, …}` and into `apogee.Config`, and nothing ever wrote either again — the only
precedent mutation of `Options` was `Mode` on Shift+Tab. Everything downstream inherited that
one-shot answer for the life of the session: the footer's `model ✦ ctx`, the start-up box, the
context-usage gauge's denominator, the Budget and the automatic Compaction trigger
([ADR 0018](0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md)), the
per-model system prompt ([ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md)),
the validated Mechanism set ([ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md)),
and the model id stamped on every saved session record
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)).

That is wrong in the way the owner actually works. A local llama.cpp server is restarted with a
different model, or a different `-c`, from a launcher **beside** apogee — and apogee kept showing
the old name over the old window while sending requests whose configured model id the provider
client silently preferred over the request's. The reported symptom was "the context size is wrong
and the gauge is wrong"; the cause was that both were frozen at launch.

Three more defects lived in the same seam. Startup **blocked** the first paint for up to five
seconds on discovery and **hard-failed** when `model:` was unset and the server was down, so
apogee could not be started before its server — the ordinary order of events on a workstation. The
pinned-model path probed with an **empty** model hint, so on a multi-model server it adopted
`models[0]`'s window rather than the pinned model's. And the gauge clamped its bar but not its
percentage text, printing `41k 137%`.

Nothing in the codebase watched the Upstream over time. `apogee probe`
([ADR 0021](0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md))
is the diagnosis command and CONTEXT.md is explicit that it "diagnoses, it does not monitor". A
dead `provider.ServerManager` carried a `Reachable` health probe and a local-process launcher, but
was never constructed in production. The engine had no way to change a per-model binding at all:
`agent.upstream` and `cfg.Model` were fixed at construction, and `errMissingModel` refused a
model-less one.

This ADR records the design the owner ratified in the 2026-07-27 grill, implemented by
`docs/plans/2026-07-27 - 00 - upstream-heartbeat-plan.md`. It supersedes nothing; it extends
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)'s idle-only engine-call
class by one member and gives ADR 0021's parked "who monitors, then?" question its answer.

## Decision

**1. The Heartbeat is a new noun, not a widening of Probe.** `internal/heartbeat` observes the
Upstream every `Interval` and reports a `Beat` — reachability, the active model, the runtime
context window, and the full `/v1/models` offering. A probe *diagnoses once, on demand, for a
human to read*; a beat *monitors continuously, unasked, for the program to consume*. Two jobs, two
nouns: neither is spelled "health check" or "poller", and `apogee probe` is untouched by this work
(its hint-less resolution stays documented behaviour and its output stays byte-identical).

**A beat is never an error.** An unreachable server sets `Reachable: false` and explains itself in
`Failure` — the same posture `probe.Discover` already takes — because "the server stopped
answering" is precisely the observation the caller needs in order to say so, not a failure of the
observation.

**2. The cadence is a named constant, and the chain re-arms from the LANDED beat.** `Interval = 10
* time.Second` is a const in `internal/heartbeat`, deliberately **not** a config key: the owner
fixed ten seconds, and a knob nobody turns is surface to maintain. The first beat fires immediately
from `Model.Init` — startup discovery **is** that beat now — and the `beatMsg` fold schedules the
next tick ten seconds later, which then re-issues the beat command.

Timing the wait from the *landing* rather than from a fixed clock is what makes overlap impossible:
an observation and its wait are strictly sequential whatever the server's latency (and
`Discover`'s own five-second timeout keeps a beat strictly shorter than the interval anyway). The
chain reuses the spinner's generation counter verbatim (`internal/tui/spinner.go`), so a message
from a retired chain is inert rather than a second chain.

**3. Startup is fully asynchronous: cold start, late seed and mid-session switch are ONE code
path.** The synchronous startup discovery is deleted — `RunE` goes `seedDefaultConfig →
applyConfig → runRoot` and the TUI paints instantly. `Config.Model` may therefore start **empty**:
`errMissingModel` is dropped from construction and replaced by `errNoModelBound`, a *request-time*
gate on `Submit`, because a model-less Agent is a legitimate object (it can clear context, restore
a session, switch mode) right up to the point where a request would have to name a model.

The first successful beat completes discovery **late**, through exactly the same rebind that a
mid-session model switch takes. There is no separate seeding path to keep in step with the
switching path — only different words in the transcript (`connected: X` versus `model changed: X →
Y`).

**4. Rebind is idle-only, and the boundary IS the synchronization.** `Agent.Rebind(RebindSpec)`
swaps *all* the per-model bindings together — wire model id, system-prompt template,
`MaxContextTokens`, and a freshly built Mechanism registry — and joins ADR 0011's idle-only
engine-call class beside `ClearContext` / `RestoreSession` / `AbortExchange`: it refuses
mid-Exchange with `ErrInputPending`, and the host stashes a change observed mid-Exchange as a
latest-wins `pendingRebind` applied in the exchange-terminal fold.

That discipline is *deliberately chosen over a lock*. Every un-mutexed `cfg` reader in the loop —
`buildRequest`, `budget()`, `Compact`, the per-call `loopView`→`Budget` rebuild the Mechanisms see
— runs inside `Step`/`Compact` on the worker goroutine, and the worker's terminal `Msg` travelling
through the Bubble Tea channel establishes happens-before in both directions. So the hot loop
needs **no new mutex and no changed read**, and `-race` stays green. The two surfaces that are
genuinely concurrent get real guards instead: `provider.Client.SetModel` takes a mutex (the Client
documents concurrent use), and `sessionHost.SetModel` uses the `mu` its `Save` already runs under.

**Validate-then-commit.** Rebind builds the new registry and passes it through the ordering,
incompatibility and requirement gates *before* mutating anything, so a spec the new model cannot
satisfy leaves every binding — and the whole conversation — exactly as it was. What stands across
a rebind: the conversation and Turn counters, the autonomy mode, session approvals, the
confinement flag, the resolved tools, and ~~the **model profile** (`model-profile` is global, not
per-model)~~. What resets: the token estimator (its chars→token calibration described the old model)
and the compaction saturation latch (it was judged against the old window).
*(The struck clause is **superseded 2026-08-11 by
[ADR 0044](0044-model-profiles-are-per-model-and-mostly-shipped.md)**: the profile is resolved per
model, so it no longer stands across a rebind — it rides one, as `RebindSpec.Profile`, applied
atomically with the other per-model bindings. Everything else this paragraph lists is unchanged.)*

**5. Two seams, split by job: the heartbeat OBSERVES, the rebind APPLIES, and the binary owns
everything in between.** `tui.Options` gains `Heartbeat func(context.Context) heartbeat.Beat` and
`Rebind func(model string, contextWindow int) (RebindResult, error)`, both nil-able. The renderer
decides only **when** — at idle, or at the exchange-terminal boundary — because the *what* is
config the composition root owns: the per-model system prompt (ADR 0023), the validated set
(ADR 0016), the manual mechanisms list, and the window pin. `internal/tui` imports
`internal/heartbeat` for the `Beat` value and the cadence and **never** `internal/provider`
([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)); the thin-renderer contract
(ADR 0011) is unchanged.

The seams degrade independently and honestly. `Heartbeat: nil` arms no chain, folds nothing and
blocks nothing — the pre-heartbeat renderer exactly. `Rebind: nil` is a **display-frozen**
heartbeat: offline state and the model list still live, no binding ever moves. `RebindResult`
reports what was actually **bound**, never merely what was observed, and the display adopts *that*
— so the footer can never advertise a model the wire is not using.

**6. `context-window:` is a PIN; a configured `model` is a HINT.** A configured window is never overridden by
the heartbeat, in the display or in the Budget — "leave unset to discover" stays the documented
semantics, and the pin is the escape hatch for a server that misreports its window. A configured
model is passed as the **discovery hint** (which fixes the multi-model wrong-window defect at its
root) and is honoured while the server serves that id; once it vanishes from `/v1/models` the
binding follows observed reality, with a transcript notice. A pin is a hint about reality, never a
claim that overrides it.

The renderer needs **no knowledge of either**. A landed beat is measured against the last
**observation**, not against the current binding, and the observation is recorded the moment the
intent is captured — so a pinned window that the server keeps contradicting every ten seconds is
recognised as "nothing new" rather than as a fresh change, and a rebind whose bound values did not
move writes no note at all.

**7. Offline is debounced by the Exchange, not by a timer; and going offline BLOCKS rather than
degrades.** Three rules, in order:

- **A failed beat while an Exchange is in flight is ignored** — counter, state and words untouched.
  A streaming reply is stronger evidence that the server is there than a timed-out `/v1/models` on
  the single-slot server busy producing that very stream.
- **A failure before any beat has ever landed is believed at once.** A cold start against a
  stopped server should say so immediately, not after a debounce it has no evidence for.
- **Otherwise the crossing waits for two consecutive idle failures** (~15–25 s), so one five-second
  discovery timeout under load cannot flicker the footer.

Each crossing is noted **exactly once**, both ways (the `saveFailing` fail-once posture). While
there is nothing to send to — offline, or no model bound yet — the three paths that would open an
Exchange (a message, `/continue`, `/compact`) are refused with a transcript note naming the
endpoint and the failure, **and the typed message stays in the box**. Everything else stays live:
scrollback, `/clear`, `/sessions`, `/version`, `/confine`, Shift+Tab. An in-flight Exchange is
never killed by a heartbeat.

**8. The beat carries the whole `/v1/models` offering, and nothing renders it.** Each beat stashes
`AvailableModels` into TUI state. That is the deliberate data layer for the future `/model` picker
and `/server` switch: the seams (`hb.models`, `RebindSpec`, `Agent.Rebind`) are prepared and
proven by this work, and the UI is explicitly not built here.

## Considered options

- **Guard `cfg` with a mutex and rebind whenever the beat lands** — rejected. It would put a lock
  in the loop's hottest read paths (`budget()` runs per Mechanism call) to serve an event that
  happens at most once every ten seconds, and it would still not be *correct*: a model swapped
  mid-Turn would change the system prompt and the Mechanism set between two requests of the same
  Turn. The boundary discipline is both cheaper and stronger.
- **Cancel the in-flight Exchange when the model changes** — rejected by the owner's decision that
  a heartbeat never kills work in progress. A deferred apply costs at most one Exchange's worth of
  staleness and loses nothing.
- **Refuse a mid-Exchange change outright** (make the user re-trigger it) — rejected: the change
  is upstream reality, not a request; there is nothing for the human to re-trigger, and the
  transcript would carry a refusal for something that did happen.
- **A fixed `time.Ticker` independent of the beats** — rejected: a slow server would let a second
  beat start while the first was still running, which is exactly the overlap the landed-beat re-arm
  makes structurally impossible.
- **A `heartbeat-interval:` config key** — rejected with the other non-knobs. Ten seconds is the
  owner's decision; a field can be added later without changing anything decided here.
- **Refresh the display only, without rebinding the engine** — rejected as dishonest: the footer
  would name a model the requests are not using, and the Budget would keep measuring against a
  window that no longer exists. If the display follows the server, the bindings must follow it too.
- **Reuse `provider.ServerManager` for reachability** — rejected, and the type is deleted. Its
  liveness half is superseded by the heartbeat, which observes strictly more (the model and the
  window, not just "up"); its launch half belongs to the parked local-server start/stop work and
  should be rebuilt over `heartbeat.Beat` when that lands. Two reachability concepts would violate
  the sharp-nouns rule for no gain.
- **A `/health` endpoint check instead of `/v1/models`** — rejected: apogee speaks exactly three
  endpoints (`/v1/chat/completions`, `/v1/models`, `/props`), not every OpenAI-compatible server
  serves `/health`, and "answers with a usable model list" is the reachability that actually
  matters — a server that is up but serving nothing apogee can call is not reachable in any useful
  sense.
- **Keep synchronous startup discovery with a shorter timeout** — rejected: it trades one defect
  (a five-second stall) for another (a hard failure when the server is not up yet) and keeps the
  two code paths, seeded and switched, that the async design collapses into one.
- **Teach the renderer about the `context-window:` pin** so it can tell a real change from a
  suppressed one — rejected: it would push config knowledge into the thin renderer. Comparing
  against the last observation gets the same result with no knowledge at all.
- **Unbind the model when the server goes offline** — rejected. `Rebind` *binds*; an unreachable
  server is an observation to report, not a reason to tear a session's bindings down. The
  conversation, its history and its window survive an outage intact.

## Consequences

- **The public Go surface gains `Agent.Rebind(RebindSpec)` and the `apogee.RebindSpec` alias** —
  additive (ADR 0010), so a **minor** bump. An embedder that never calls it is byte-identical to
  before, with one relaxation: `Config.Model` may now be empty at construction, and a `Submit`
  against an unbound Agent fails with `errNoModelBound` instead of `New` failing. An embedder who
  supplies a **pre-built** `Config.Mechanisms` registry cannot rebind (Rebind cannot rebuild hooks
  it did not build) and gets an honest error; the TUI wiring only ever sets `EnableMechanisms`, so
  it never meets that.
- **Startup no longer blocks or hard-fails on discovery** — the one behavioural change in this
  work. `apogee` against a dead endpoint now paints in well under a second and says `connecting…`,
  where it used to stall up to five seconds and, with `model:` unset, refuse to start at all.
- **`opts.contextWindow > 0` now means "pinned", exactly.** Nothing pre-fills it any more, which is
  what lets the rebind closure implement the pin rule with a single comparison.
- **A mid-Exchange model switch is narrated late.** The notice and the display move when the
  Exchange closes, not when the switch happens — the visible cost of never interrupting work.
- **`ctxUsed` survives a rebind unchanged.** The conversation is not reset by a model switch, so a
  fill measured against a larger window renders **clamped at 100%** against a smaller one until the
  next usage event or a compaction re-measures it. The gauge's percentage text now clamps with its
  bar, so this reads as "full", not as `137%`.
- **The gauge and the engine Budget still count different things** — the gauge uses the server's
  reported `TotalTokens`, the Budget a prompt-side estimate. That divergence is **documented and
  deliberately untouched** here; it long predates the heartbeat and is not a staleness bug.
- **llama.cpp's `/props` `n_ctx` is PER SLOT** — and `total_slots` was not read (**amended
  2026-08-08:** it is now, riding the same Beat as the discovery source for the **Parallel agents**
  cap,
  [ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
  decision 2; the per-slot arithmetic below is unchanged, and is exactly why that cap and this
  window trade are documented as one thing). On a server started
  with `--parallel N` the runtime window a beat reports is one slot's share, which is the window a
  single conversation actually gets — the honest number for the Budget. The multi-slot arithmetic
  is recorded here rather than re-derived; if a server ever reports these two in a way that makes
  the per-slot value misleading, the `context-window:` pin is the answer until it is.
- **The picker work is now cheap and unblocked.** `TODO.md`'s "[P1] Server / model switching" loses
  its blocker: what remains is UI over `hb.models` plus, for a server switch, a new Monitor and
  client behind the same two seams — `Rebind` deliberately never touches `Endpoint`, and
  `errMissingEndpoint` stands.
- **`apogee probe` is unchanged and its output stays byte-identical**, which is the regression
  boundary between the diagnosis command and the monitor.
- **CONTEXT.md gains one term — *Heartbeat*** — worded to match this ADR and cross-referenced from
  *Probe*, so the two nouns cannot blur.
