# Reaction core — a greenfield design for one hook/mechanism/floor-guard surface

**Date:** 2026-09-07 · **Status:** ✅ **Decided** — grilled the same day; §3 and §8 resolved in
[ADR 0076](../adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md) (day-one user
cells observe + advise + gate; user shape(view) and `mcp:` reserved; Bypass = advise + shape off).
Implementation plan not yet written. · **Companion:**
[hook-talkback-findings.md](hook-talkback-findings.md) (research; the doctrine map) · **Open bead:**
`apogee-575`.

> **How to read this.** The owner asked: *imagine a green field — how would a system be designed
> that merges today's Hooks, Mechanisms and Floor guards into one thing, that can run external
> actions, report back to the model, or alter the context — and what would that buy?* The ADRs are
> treated here as **ideas to weigh, not rules to obey**. Where an existing decision is kept, it is
> kept on merit and the reason is given; where it is dropped, the reason is given too. The point is
> to see whether apogee should refactor towards this shape.

---

## 1. Today: one abstraction in three coats

| Subsystem | Origin | Timing | May return | Config idiom |
|---|---|---|---|---|
| Floor guard (7) | engine Go | in-loop, sync | edits request / response / call | 7 flat `*bool` keys |
| Mechanism (0 shipped) | lab Go | in-loop, sync | retry / inject / edit | `mechanisms:` map + retired roll |
| Hook (5 events) | user argv / webhook | post-hoc, async | nothing | `hooks:` list |

They mutate the same working values at the same seams. What is triplicated:

- three vocabularies — `hooks.Event`, `domain.HookPoint`, the guard-key consts — each with its own
  list helper and error wording;
- two firing events with the same shape, `MechanismFiredEvent` and `FloorGuardEvent`;
- a guards-then-hooks double ladder at all five seam call sites, returning the same
  `(retry, inject)` tuple through one `applyRetry`;
- three config idioms and three live-swap idioms (`SetBypass`, `SetFloor`, `Runner.Replace`);
- a lab layer — `Deps`, `register`, `SwapCatalogue`, the retired roll, self-regulation — that
  governs zero shipped rows.

Roughly 5.5k production and 6.1k test lines across the three packages and their `internal/agent`
wiring. About 1.5k of that, the policy in `internal/floor`, is the value; the rest is plumbing.

## 2. The design

### 2.1 Moment — one vocabulary

Every point the loop passes is a **Moment**. Two kinds:

- **seam** — in-loop, synchronous, the payload is an editable working value:
  `pre-request`, `post-response`, `pre-tool-exec`, `post-tool-result`, `history-rewrite`.
- **notice** — post-hoc, the payload is sealed: `exchange-finished`, `turn-finished`,
  `file-changed`, `approval-requested`, `approval-decided`, `error`, … (additive).

Every seam publishes a notice when it closes, so observing a seam costs nothing extra.

### 2.2 Reaction — one type

```
Reaction { id, origin, class, on: [moments], handler }
```

- `origin` — `engine` (a builtin, or a Go reaction the bench registers in-process) | `user`
  (configured).
- `class` — what the reaction may do; see the matrix in §3.
- `handler` — Go func | argv | webhook | `mcp:` server/tool.

### 2.3 Two lanes, one dispatcher

- **Sync lane** runs on the loop goroutine at a seam, ordered engine → user, each reaction under a
  recover boundary and a deadline. Outcomes fold to `(retry, inject, edit)`.
- **Async lane** is today's Runner: one bounded queue per reaction, drop-newest under overload, never
  waited on.

The class picks the lane. `EventSink.Emit` stays return-less and the tree-wide serialising mutex is
untouched: a reaction that must answer runs *before* the moment is published, never on the stream.
This split is physics, not doctrine — a reaction that returns must block the loop; one that does not
must not.

### 2.4 One config shape, three layers

```
embedded defaults  →  ~/.apogee/config.yaml  →  <workspace>/.apogee/config.yaml
```

Later layers override by key; one resolver; `/settings` shows which layer each value came from.

```yaml
reactions:
  - id: notify                                   # user origin; enabled: false parks it
    on: [exchange-finished]
    run: ["notify-send", "apogee done"]          # observe lane
  - id: lint-after-edit
    on: [file-changed]
    advise: ["golangci-lint", "run", "{path}"]   # sync lane: stdout → model, confined, capped, fenced
    timeout: 10s
  - id: no-force-push
    on: [pre-tool-exec]
    gate: ["./scripts/guard.sh"]                 # sync lane: allow / deny / ask
```

`run:` = observe, `advise:` = returns text, `gate:` = returns a decision; an entry may carry more
than one. The old `hooks:` list and the `mechanisms:` map migrate through one table — the retired
roll already models exactly this mapping.

> **Amended 2026-09-08 (stage-2 grill; ADR 0076 amendment A1, A2, A7).** `reactions:` is the
> **user-origin surface only**, and stage 2 ships the **global file only** — the repo layer, the
> three-layer resolver and the adoption pin below become stage 2b, grilled on bead `apogee-089`.
> The seven Floor booleans stay canonical and do not migrate (ADR 0076 D11: a Floor guard is by
> definition one top-level file-only boolean); a builtin id inside `reactions:` is a load-time
> validation error naming the boolean instead, never a second spelling. Engine origin is code — the
> bench arms a Go reaction in-process through the facade (ADR 0076 D1), not through this key. The
> entry schema is `id:` / `on:` / `run:` (polymorphic — a sequence is argv, a mapping is a webhook)
> / `workspace:` / `timeout:` / `enabled:`; `advise:` and `gate:` are rejected at load until stage 3.
> This supersedes D10's "builtins appear by id with `enabled:`" and the original wording here.

**Which keys a repo layer may set.** The rule to keep is not "global only" but *a clone cannot run a
command before the user has seen it*:

| Key class | Repo layer | Why |
|---|---|---|
| Parameters — `enabled`, `timeout`, `on:`, model profile, floor toggles, `workspace:` | overrides freely | introduces no execution |
| Execution — `run:` / `advise:` / `gate:` / `mcp:` | **proposed, not live** | the rival CVEs, and the model-writes-its-own-reaction hole |

A proposed reaction goes live only when the user adopts it (`apogee reactions adopt`, or a
`/settings` row). Adoption pins the entry's hash; an edit to the command re-proposes. Two backstops
specific to apogee: the repo config path is on the tool deny list for writes, and the hash pin
holds even if that deny is switched off — because `write_file` can otherwise author a reaction that
runs on the next reload, bypassing the workspace exec fence in one hop.

### 2.5 Provenance ledger

Every span a reaction injects records `{reaction, origin, moment, turn}`. This is what makes the
advise class survivable, and it answers the costs the findings document lists as apogee-specific:

- the fence header is derived from provenance (nothing out-of-process can forge an engine header);
- injected text lands in **one stable slot**, never the system prompt, so a local server's prefix
  cache survives the Turn;
- spans are `ephemeral` and dropped on resume, so a replay never re-reads a stale SHA or timestamp;
- the bench attributes effect to a reaction id, which is what makes any reaction benchable;
- `/settings` can show "what the model saw this Turn".

## 3. The policy matrix

The `Reaction surface` entry in `CONTEXT.md` (four exclusive rungs) becomes an executable matrix.
Exclusivity survives as "one reaction, one cell".

| origin ↓ / class → | observe | advise | gate | shape (view) | shape (work) |
|---|---|---|---|---|---|
| engine (builtin or bench-armed) | ✓ | ✓ | ✓ | ✓ | ✓ |
| user | ✓ | ✓ | ✓ | ✓ | ✗ |

- **observe** — no return. Today's Hook.
- **advise** — returns text that becomes model-visible context, fenced, capped, fail-open (no advice
  is just no advice). Tier C in the findings document's terms.
- **gate** — returns allow / deny / ask at `pre-tool-exec`, implemented as a stage of the existing
  Approver, not at the Mechanism seam. Deny text stays engine-authored. A user script saying No is
  the same act as a human saying No, so it does not touch the floor invariant.
- **shape (view)** — edits what the model *sees*: `post-tool-result`, `pre-request`,
  `history-rewrite`. Strip ANSI, trim test output, redact. Today's Floor guards live here.
- **shape (work)** — edits what the model *does*: tool-call arguments at `pre-tool-exec`. Engine only.
  Denied to users on evidence (pi's ordering hazard: once a later reaction can mutate arguments an
  earlier gate approved, no gate in the system is sound).
- **continue** is not a class. Every rival that shipped continuation control wedged and shipped a
  loop breaker late. Not building it is the cheapest safety decision available.

`--bypass` under this design = builtins only (every user reaction and every bench-armed reaction
off). The bench's control arm and the shipped default remain the same agent.

## 4. What it buys

### Simpler code

- 3 vocabularies → 1; 2 firing events → 1 `ReactionFiredEvent`; 5 double-ladder sites → 1 call each;
  3 config idioms → 1; 3 live-swaps → 1 generation swap.
- The dead lab machinery (`Deps`, `register`, `SwapCatalogue`) goes. Runtime self-regulation (strikes,
  Turn Budget) leaves the core: no shipped row ever needed it, and the bench arms a Go reaction
  through the facade without a catalogue.
- Estimate: ~5.5k → ~3.5k production lines. `internal/floor` policy stays byte-for-byte. One test
  harness (Moment payload in, outcome out) replaces three.

### New possibilities

1. The owner's three modes — event → action; event → action → response to the model; both — are one
   config entry with `run:` and/or `advise:`.
2. Observation at any seam without veto, for free.
3. Per-project reactions with the project's own linter, test runner and formatter, via the repo
   layer; the lint-after-edit case finally has a natural home.
4. Script-authored bench arms (an `advise` reaction with an argv handler) lower the bar to *try* an
   intervention; Go stays the shipped form.
5. Per-repo model profile and floor tuning; per-profile reaction rosters on the `tools.disabled`
   axis.
6. "Repo proposes, user adopts" doubles as onboarding: clone, see the proposed reactions, accept the
   ones you want.
7. The provenance ledger is a first-class engine fact no rival has, and the only honest answer to
   prefix-cache instability, replay staleness and header forgery.

## 5. What it costs

- **Doctrine.** One ADR would replace ADR 0071 D4/D6 (catalogue and lab layer) and ADR 0073
  D2/D4/D5/D7 (observe-only, no pre-event, global-only, never-wait) together, and the
  `Reaction surface` entry in `CONTEXT.md` is rewritten as the matrix. ADR 0031 invariant 4 is
  *satisfied by construction* rather than superseded: every handler registers on one core, so any
  reaction is bench-drivable in-process and the ledger attributes its effect. Content quality carries
  the standing obligation ADR 0064 §6 already places on the system prompt.
- **Abstraction risk.** A generic dispatcher over seven guards, zero mechanisms and five events could
  be over-built. Bound it: one interface, one matrix, no plugin loader, no `continue` class.
- **Churn.** Most of the ~6k test lines rewrite.
- **The advise costs are unchanged** from the findings document §4 (injection channel, token cost,
  staleness, prefix cache, forgery adjacency); the ledger is the tool that addresses them, not a
  proof that they are gone.

## 6. What is kept from the ADRs, and why

Kept on merit, not authority:

- The bench as the admission process for anything that ships **on** (ADR 0009/0071 idea).
- The guards' ratified order — salvage first, no short-circuit (ADR 0071 amendment).
- Fail-open for advice, hard timeouts, output caps, no shell, program resolution fenced against the
  workspace (ADR 0073 D6; the exec contract §10).
- No in-process plugin loader for users (ADR 0073 rejection): the trust surface is not worth it
  while argv, webhook and `mcp:` cover the cases.
- The floor invariant itself — "nothing apogee puts in front of a model may make it perform worse
  than the bare loop" — which is the product premise, not an ADR.

Dropped, with the reason:

- "No veto, ever" → `gate` as an Approver stage (the objection was a second *Mechanism* surface;
  this is not one).
- "User may not shape" → split by moment: view yes, work no.
- "Global config only" → three layers; execution keys need adoption.
- Runtime self-regulation for lab rows → gone from the core; a bench concern.
- The `lab` origin as a row of its own → engine-origin, registered in-process.

## 7. Verdict, and a staged route

**Refactor, staged.** Best long-term shape, and the mechanism retirement wave (ADR 0071) already did
half the collapse.

1. **Core** — Moment, Reaction, single ladder, `ReactionFiredEvent`. Pure refactor,
   behaviour-identical; a bench identity arm proves it.
2. **Config** — `reactions:` with the migration table, three layers, adoption pin.
3. **User cells** — `advise`, `gate`, `shape (view)` for user origin, each behind the grill that
   bead `apogee-575` asks for. Under this design that grill shrinks to: which cells on day one, one
   fence, one bench arm.

Rejected alternative: a config-only merge over the three existing runtimes (the position in the
handoff). Cheaper now, but it adds a fourth idiom on top of three and keeps every duplication.

## 8. Open questions for the grill

1. Day-one user cells: `observe` + `advise`, or all four?
2. The stable injection slot: one block before the newest user/tool message, or a dedicated message?
3. Deadline defaults per class (`advise` and `gate` block the loop).
4. Whether `mcp:` handlers need their own approval posture or inherit the MCP tool's.
5. Migration UX for the seven floor booleans and the `hooks:` list — silent, or a one-time notice.

## 9. Seed material for the plan

Recorded 2026-09-07 from a code survey of `v0.20.10` so the plan-writer starts from the map. Line
numbers drift; names do not.

### 9.1 What collapses into what

| Today | File | Becomes |
|---|---|---|
| `hooks.Event` (5 consts), `eventList()` | `internal/hooks/hooks.go` | `Moment` notices |
| `domain.HookPoint` (5 consts) | `internal/domain/mechanism.go` | `Moment` seams |
| guard-key consts (7) + actions (4) | `internal/agent/floorguards.go` | builtin `Reaction` ids + `ReactionFiredEvent.Action` |
| `MechanismFiredEvent`, `FloorGuardEvent` | `internal/domain/events.go` | one `ReactionFiredEvent` |
| `runHooks[H]` + 5 adapters | `internal/agent/hookrun.go` | the sync-lane dispatcher |
| `runPostResponseGuards`, `runPreToolExecGuards`, `runPreRequestGuards` | `internal/agent/floorguards.go` | engine-origin reactions on the same dispatcher |
| double ladder at 5 sites | `loop.go` (pre-request ×2, post-response), `dispatch.go` (pre-tool-exec ×2, post-tool-result ×2) | one `Fire(moment, payload)` per site |
| `hooks.Runner` (queue, matcher, executors) | `internal/hooks/runner.go`, `match.go`, `command.go`, `webhook.go` | the async lane, unchanged in substance |
| `SetBypass`/`bypassMu`, `SetFloor`/`floorMu`, `Runner.Replace` | `agent.go`, `floorguards.go`, `runner.go` | one generation swap of the reaction set |
| `mechanisms.Deps`, `register`, `catalogue`, `SwapCatalogue`, `Build`, `Descriptors` | `internal/mechanisms/catalogue.go` | deleted |
| `selfreg.go` (strikes, Turn Budget), `skipUnderBypass` | `internal/agent/selfreg.go`, `hookrun.go` | deleted from the core; `--bypass` = builtins only |
| `retired.go` roll with `Successor` | `internal/mechanisms/retired.go` | the config migration table |
| `hooks:` list, `mechanisms:` map | `internal/config/hooks.go`, `config.go`, `configwrite_mechanism.go`, `cmd/apogee/wire_settings.go` | one `reactions:` list, one resolver (amended 2026-09-08: the 7 floor `*bool` keys and the `floorFromOptions` negation seam STAY — ADR 0076 D11; the layers are stage 2b) |
| `/settings` rows: hooks (read-only), mechanisms (read-only) | `internal/config/registry.go` | one read-only structured `reactions` row (amended 2026-09-08: the 7 floor rows stay as they are — ADR 0076 D11, open bead `apogee-tbs`; no per-entry toggle table until stage 2b) |

Stays byte-for-byte: the policy functions in `internal/floor` (`SalvageToolCall`, `ToolLoopBreak`,
`ToolCallRepair`, `RecoverEmpty`, `EnforceToolUse`, `CacheRead`, `CapToolResults`) and the working
values in `internal/domain/hooks.go`, `tooledit.go`, `hookview.go`. `tools.RunHookSubprocess` and
the `SubprocessPermit` remain the sanctioned door for a sync-lane subprocess.

Install sites to rewire: `cmd/apogee/wire_boot.go` (TUI sink chain
`eventjson.Writer → hooks.Runner → bridge.Sink()`), `cmd/apogee/wire_firing.go` (Firings),
`cmd/apogee/wire_live.go` (`EnableMechanisms`), `cmd/apogee/wire_settings.go` (live reload).

### 9.2 Type sketches

```go
// internal/domain
type Moment string            // "pre-request", "post-response", …, "exchange-finished", …
type Origin string            // "engine", "user"
type Class string             // "observe", "advise", "gate", "shape-view", "shape-work"

type Reaction struct {
    ID      string
    Origin  Origin
    Class   Class
    On      []Moment
    Handler Handler           // GoHandler | ArgvHandler | WebhookHandler | MCPHandler
    Timeout time.Duration
}

// One outcome shape for every seam; the zero value means "did nothing".
type Outcome struct {
    Retry  bool              // re-stream the Turn (post-response only)
    Inject string            // advise text, fenced by the dispatcher with provenance
    Gate   GateDecision      // allow | deny | ask (pre-tool-exec only)
    Edited bool              // a shape reaction moved the working value's Revision()
}

type ReactionFiredEvent struct {
    EventBase
    Reaction string; Origin Origin; Moment Moment; Action string; Detail string
}

// internal/agent — the only dispatcher.
func (a *Agent) fire(ctx context.Context, m domain.Moment, payload any) (domain.Outcome, error)
```

The Go handler for a seam takes the payload the seam already owns today (`*Request`,
`*Response`, `*ToolCallEdit`, `*ToolResultEdit`, `*Conversation`). The matrix in §3 is a table the
config loader consults at resolve time, not a runtime check.

### 9.3 Acceptance for "behaviour-identical" (stage 1)

- Every test in `internal/floor`, `internal/agent`, `internal/hooks` passes with only the
  event-type renames changed.
- `go test ./...` goldens in `internal/tuitest` unchanged except the debug-view lines that render
  the firing event.
- Bench identity arm: the same task set under `v0.20.10` and under stage 1, same model, produces the
  same firing sequence per Turn (reaction id + moment + action) — compared from `ReactionFiredEvent`
  against a mapping of the old two events.
- `--bypass` on a stock install still switches off nothing.
- `hooks:` and the seven floor keys still load, with a one-time notice naming the `reactions:` form.

### 9.4 Plan boundaries

- **Plan A (stage 1):** 9.1 rows one to eleven, the type sketches, 9.3. No config change beyond
  the migration notice; no user cells beyond `observe`.
- **Plan B (stage 2, gated on A):** `reactions:` config, three layers, adoption pin, `/settings`
  table, the user cells the grill ratified.
