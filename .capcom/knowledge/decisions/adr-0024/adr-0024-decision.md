---
title: ADR 0024 decision
---

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

**2. The cadence is a named constant, and the chain re-arms from the LANDED beat.** (Amended
2026-09-30: the cadence is now `Interval = 2 * provider.DiscoveryTimeout`, about 60 s —
`internal/heartbeat/heartbeat.go` records that it supersedes the ten seconds below. The
constant-not-a-key rule stands.) `Interval = 10
* time.Second` is a const in `internal/heartbeat`, deliberately **not** a config key: the owner
fixed ten seconds, and a knob nobody turns is surface to maintain. The first beat fires immediately
from `Model.Init` — startup discovery **is** that beat now — and the `beatMsg` fold schedules the
next tick ten seconds later, which then re-issues the beat command.

Timing the wait from the *landing* rather than from a fixed clock is what makes overlap impossible:
an observation and its wait are strictly sequential whatever the server's latency (and
`Discover`'s own five-second timeout keeps a beat strictly shorter than the interval anyway; since
2026-09-22 that timeout is `provider.DiscoveryTimeout`, 30 s, and the interval is derived as twice
it, so the property now holds by construction). The
chain reuses the spinner's generation counter verbatim (`internal/tui/spinner.go`), so a message
from a retired chain is inert rather than a second chain.

**3. Startup is fully asynchronous: cold start, late seed and mid-session switch are ONE code
path.** The synchronous startup discovery is deleted — `RunE` goes `seedDefaultConfig →
applyConfig →
runRoot` and the TUI paints instantly *(amended 2026-10-29 — the names today: `config.SeedDefaultConfig
→ config.ApplyConfigOffer →
runRootWith`, `cmd/apogee/root.go`)*. `Config.Model` may therefore start **empty**:
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
latest-wins `pendingRebind` applied in the exchange-terminal fold. (Amended 2026-09-30: the
exchange-terminal fold is one of four release points — the worker's end, a launcher verb's
completion, a `/bg` launch landing and a background prompt's pane closing — and each runs the
one release transition `Model.releaseEngine` (`internal/tui/engineholds.go`), which applies the
stash only when no worker, actuation or `/bg` launch hold still stands. Those holds overlap, so
with two at once — a wake can run during an actuation — the stash lands at the second release,
not the first. The boundary is still the synchronization; it is now the last hold's boundary.)

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
ADR 0044**: the profile is resolved per
model, so it no longer stands across a rebind — it rides one, as `RebindSpec.Profile`, applied
atomically with the other per-model bindings. Everything else this paragraph lists is unchanged.)*

**5. Two seams, split by job: the heartbeat OBSERVES, the rebind APPLIES, and the binary owns
everything in between.** `tui.Options` gains `Heartbeat func(context.Context) heartbeat.Beat` and
`Rebind func(model string, contextWindow int) (RebindResult, error)`, both nil-able. *(Amended
2026-10-29: since 2026-08-20 (ADR 0054) both are methods of one `tui.ServerHost` interface on
`Options.Server` — `Beat(ctx) heartbeat.Beat` and
`Rebind(model, contextWindow, effortDialect) (RebindResult, error)`, the dialect added by ADR 0060 —
and the degrades below read `Server: nil` or `ServerActs.CanObserve` / `CanRebind` false where they
say `Heartbeat: nil` / `Rebind: nil`. The split of jobs is unchanged.)* The renderer
decides only **when** — at idle, or at the exchange-terminal boundary — because the *what* is
config the composition root owns: the per-model system prompt (ADR 0023), the validated set
(ADR 0016), the manual mechanisms list, and the window pin. `internal/tui` imports
`internal/heartbeat` for the `Beat` value and the cadence and **never** `internal/provider`
(ADR 0010); the thin-renderer contract
(ADR 0011) is unchanged.

> **Note 2026-10-02 (production files; test-only import allowed).** The "never `internal/provider`"
> rule binds `internal/tui`'s production (non-test) files; `TestTUIImportsNoProvider`
> (`internal/tui/imports_guard_test.go`) enforces it over every non-test `.go` file, build-tagged
> ones included. The live tests (`live_test.go`, `smoke_live_test.go`) and the e2e harness
> (`e2e_test.go`) keep a test-only `internal/provider` import, which the rule allows. Effort
> capabilities reach the renderer as `domain.EffortSupport` / `domain.EffortDialect`, converted
> outside `internal/tui` (`internal/heartbeat` for the Beat, `cmd/apogee` for the rebind dialect).

The seams degrade independently and honestly. `Heartbeat: nil` arms no chain, folds nothing and
blocks nothing — the pre-heartbeat renderer exactly. `Rebind: nil` is a **display-frozen**
heartbeat: offline state and the model list still live, no binding ever moves. `RebindResult`
reports what was actually **bound**, never merely what was observed, and the display adopts *that*
— so the footer can never advertise a model the wire is not using.

**6. `context-window:` is a PIN; a configured `model` is a HINT.** A configured window is never overridden by
the heartbeat, in the display or in the Budget — "leave unset to discover" stays the documented
semantics, and the pin is the escape hatch for a server that misreports its window. A configured
model is passed as the **discovery hint** (which fixes the multi-model wrong-window defect at its
root) and is honoured while the server serves that id; ~~once it vanishes from `/v1/models` the
binding follows observed reality, with a transcript notice. A pin is a hint about reality, never a
claim that overrides it.~~
*(The struck clause is **amended 2026-09-23 by
ADR 0085** decision 3, recording
the behaviour shipped since b6e51496: a configured `model` is the active model verbatim whether
or not `/v1/models` lists it — an exact or base-slug match (the part before the first `:`)
supplies only the window, an unlisted id runs as configured with the window unknown and a
transcript notice — and it is never replaced by another advertised model. Only an empty `model`
binds the first advertised one. The `context-window:` pin above is unchanged.)*
*(**Amended 2026-10-08:** every Driver binds the window the same way — the `context-window:`
pin, else the window the server was observed to report — and a Firing is no exception. A
headless or daemon Firing binds the window its one composition beat observed when nothing pins
one, and a `/schedule` Firing binds the window the session's own beats last named; a beat that
never answered, or named no window, binds none and the window stays unknown. This supersedes
the earlier rule that an unpinned Firing bound no window whatever its server advertised — kept
so a run on a `--parallel 8` box would not bind its per-slot window — under which a run
against a server advertising a million tokens was managed by the unknown-window ceiling
(ADR 0018)
as a 3072-token window. The per-slot window is the window one conversation actually gets
(Consequences below), and the pin remains the answer for a server that misreports it.)*

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
- **Otherwise the crossing waits for two consecutive idle failures** (~15–25 s at the cadence of the
  day; ~90–150 s since 2026-09-22, when the discovery timeout became 30 s and the interval twice it
  — `offlineFailureThreshold`, `internal/tui/heartbeat.go`), so one five-second
  discovery timeout under load cannot flicker the footer.

Each crossing is noted **exactly once**, both ways (the `saveFailing` fail-once posture). While
there is nothing to send to — offline, or no model bound yet — the three paths that would open an
Exchange (a message, `/continue`, `/compact`) are refused with a transcript note naming the
endpoint and the failure, **and the typed message stays in the box**. Everything else stays live:
scrollback, `/clear`, `/sessions`, `/version`, `/confine`, Shift+Tab. An in-flight Exchange is
never killed by a heartbeat.

*Cross-reference (2026-09-02):* ADR 0045 §4
mirrors this debounce for the Sub-agent routing notice — two consecutive unusable beats before the
server is called unavailable — but debounces the NOTICE only: routing beats are never idle-gated.

**8. The beat carries the whole `/v1/models` offering, and nothing renders it.** Each beat stashes
`AvailableModels` into TUI state. That is the deliberate data layer for the future `/model` picker
and `/server` switch: the seams (`hb.models`, `RebindSpec`, `Agent.Rebind`) are prepared and
proven by this work, and the UI is explicitly not built here. *(Note 2026-10-29: it has been since —
the `/model` picker (2026-07-28, `internal/tui/picker.go`) derives its rows from `hb.models`, and
`/server` lists `servers:` entries through `ServerHost.List`.)*
