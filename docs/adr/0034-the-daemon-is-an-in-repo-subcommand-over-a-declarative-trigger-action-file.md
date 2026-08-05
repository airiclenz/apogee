---
Status: accepted
---

# The daemon is an in-repo subcommand over a declarative trigger-action file

## Context

[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
anticipates a platform daemon but defers its packaging; [ADR 0033](0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)
built the libraries it composes (`internal/run`, `internal/schedule`), scoped TUI Schedules to
die with the TUI, and named durable schedules as "the future daemon's value-add over this same
library". A 2026-08-05 grill settled the daemon's shape — and, because a second apogee process
makes cross-process contention on a single-slot local server real for the first time, the shape
of the **slot broker** that resolves it.

One force redirected the broker's supply side out of this repo: llama-launcher's identity is a
**process manager, not a request router** (its ADR-0002 — one-shot, zero resident memory, no
proxying), so slot brokerage cannot land in the launcher CLI. Its repo already holds the
template for the exception: the MCP adapter (its ADR-0008), an optional resident *sibling
binary* beside an untouched CLI. The supply side follows that template as an optional
**Gateway** sibling binary, recorded in llama-launcher's ADR-0013; this record covers apogee's
side and cross-references it.

## Decision

**1. Packaging: `apogee daemon` is an in-repo subcommand.** One repo, one binary, beside
`probe` and the deferred `headless`. `internal/run` and `internal/schedule` stay unexported;
the facade-export question ADR 0033 closed stays closed; the single-static-binary promise
holds.

**2. Topology: separate runtimes, no IPC.** The daemon and the TUI are independent processes
that share libraries and stores, never a protocol. The TUI's session-scoped `/schedule` is
unchanged; neither process depends on the other running. Ephemeral-while-open and durable are
different promises, and two surfaces stating them separately is honest. IPC can be added later
without unwinding anything decided here.

**3. Control plane: a declarative file with live reload.** `~/.apogee/daemon/schedules.yaml`
is the source of truth. The daemon watches it: parse, validate, then atomically swap the
schedule set; an invalid file keeps the previous set running and logs the rejection. CLI sugar
(`apogee daemon add …`) may edit the same file; nothing speaks to the daemon over a socket.
The daemon's full standing state stays inspectable with `cat` and diffable with git.

**4. Schema: a trigger+action envelope from day one.** Each entry is a `name`, an `on:` block
(v1 supports only `cycle:`) and a `run:` block (v1 supports only `prompt:`, with `mode:` and
`workspace:`). Webhook triggers (`on: webhook:`) and workflow actions (`run: workflow:`)
arrive later as new keys inside the same envelope — additive, no migration of a file users
hand-edit. This is the door the north star's workflow layer enters through: ADR 0033 already
made the scheduler runner-agnostic behind an injected seam, so an action type beyond
"run a prompt" is a new seam implementation, not a scheduler change.

```yaml
schedules:
  - name: nightly-audit
    on:
      cycle: 24h
    run:
      prompt: "/code-audit internal/tui"
      mode: plan
      workspace: ~/repos/apogee
```

**5. Firing posture: ADR 0033 wholesale; MCP deferred.** Plan or Auto only (Auto gated by the
same eligibility ladder), the Approver is the fail-safe denier, Asker and Presenter are `nil`,
no MCP. The no-MCP reasoning differs from the TUI's (the daemon has no live connections to
fork; it *could* establish fresh per-Firing connections), so `mcp:` in the `run:` block is a
deliberate deferral, not a structural bar — it waits on its own design pass for the
denier-vs-gating interaction: under current rules, Auto-mode MCP gates through Approval, which
a denier fails, so every gated call would die anyway; only Plan-mode read-only MCP would flow.

**6. Records: the shared sessions store.** Daemon Firings save as ordinary records in
`~/.apogee/sessions`. The store is already collision-safe across concurrent processes
(random-suffixed ids, atomic temp-file-plus-rename saves, fresh directory reads on List), and
ADR 0033 already put schedule id and name on browsable `Meta` — so the TUI's `/sessions`
browser labels daemon Firings with zero new plumbing. That browser **is** the v1 observability
story. Flood control is deferred and additive: a per-schedule `keep: N` retention key in the
envelope, and browser grouping/filtering as display work.

**7. Lifecycle: foreground process, lock file, shipped service units.** `apogee daemon` runs
in the foreground, logs to stdout, and exits on SIGTERM/interrupt after a bounded grace for an
in-flight Firing. A lock file under `~/.apogee/daemon/` refuses a second instance — two
daemons watching one file would double-fire every schedule. Supervision belongs to the OS:
`apogee daemon install` generates the unit for the host's supervisor (systemd user unit,
launchd plist, Windows Task Scheduler). Shipping the units in v1 is a deliberate scope choice
for a turnkey daemon, accepting three OS-specific surfaces into v1 testing.

**8. v1 triggers: cycle only.** The webhook wave comes later with its own design pass, in two
named steps: first a **payload-discarded** webhook (localhost-bound listener, bearer token,
fires a named entry, body dropped — no model-visible injection, so no Mechanism obligation),
then payload injection, which is model-visible content and therefore lands as a catalogued
Mechanism per ADR 0031 invariant 4, never as daemon plumbing.

**9. Contention: accepted in v1; the slot broker is the fix.** The daemon sends to the
configured endpoint unconditionally; the server queues; interactive latency may dip during a
Firing. No courtesy heuristics (probing server business couples the daemon to one server's
introspection and becomes dead code when the broker lands). The broker splits along the
settled request-side/server-side line: the **supply side** — routing by model, on-demand
load/swap, queue-during-swap, priority (interactive above background), an interactive
stickiness window against background eviction, side-by-side loading under declared memory
budgets — is the launcher-repo **Gateway** (llama-launcher ADR-0013). Apogee's processes then
point their upstream at the gateway endpoint by config alone; the daemon marks its Firings'
requests background-priority; no engine change, and model swaps behind one stable endpoint
mean no rehoming (ADRs 0024/0028 untouched).

**10. The demand side: fingerprint-keyed capability records.** Apogee's half of the broker is
a future request-side resolver mapping a requirement set ("tools + 32k context") to a model
name. Its source of truth is decided now so nothing paints it out: a per-model capability
record keyed by **fingerprint** (ADR 0016's identity system), seeded from config declarations,
overwritten by probe model-battery measurements (ADR 0021) when one runs. Declarations alone
drift; measurements alone are cold-start-hostile; seeded-then-verified is the composite. The
resolver ships with its first consumer (capability-keyed schedules or workflow nodes), not
before — daemon v1 entries name models explicitly or inherit the configured default.

## Considered options

- **Separate binary or separate module for the daemon** — rejected: a second artifact weakens
  the single-binary promise for modest process-identity gain, and an external module forces
  `internal/run`/`internal/schedule` through the facade-export question ADR 0033 closed.
- **The TUI as a daemon client (durable `/schedule` over IPC), or the daemon running all
  agents with the TUI as a pure client** — rejected: the first forces a versioned IPC protocol
  on day one and couples the TUI to daemon liveness; the second is a different product that
  would supersede ADR 0011's in-process worker.
- **Control socket verbs or an HTTP admin API for schedule CRUD** — rejected: the socket *is*
  the deferred IPC surface plus a compatibility contract; the HTTP API drags auth, versioning
  and a wire schema into v1 of a single-user local tool.
- **Flat v1 schema** — rejected: the first webhook or workflow key becomes a schema break in a
  hand-edited file; the envelope costs one level of nesting now.
- **Per-schedule MCP in v1** (including a Plan-only variant) — rejected for scope: it ships
  the connection-lifecycle and denier-vs-gating questions unexamined; the envelope makes
  `mcp:` additive when that pass happens.
- **A separate daemon session store** — rejected: it isolates flood at the cost of the only
  no-IPC observability window; retention and filtering solve flood at the right layers.
- **Self-daemonizing start/stop verbs** — rejected: reimplements per-OS supervision that
  systemd/launchd/Task Scheduler already do better.
- **Courtesy-probe deferral for v1 contention** — rejected: a half-broker built on coupling,
  thrown away when the gateway lands. (A `window:` key on `on:` — schedules declaring when
  they may fire — was noted as genuinely useful future schema, but as scheduler policy, not a
  contention fix.)
- **Broker in the daemon, or cross-process file leases** — rejected: broker-in-daemon builds
  the deferred IPC now, makes the TUI carry a fallback path, and puts server supply management
  on apogee's side of the settled request-side line; file leases have no queue, no fairness,
  and stale-lock failure modes — and still nothing to actuate a model swap.
- **Declarations-only or probe-only capability metadata** — rejected: declarations drift and
  fail unattended (the worst place); probe-only makes every new profile unroutable until a
  battery run.

## Consequences

- ADR 0031's open-questions list shortens: daemon packaging is answered here (in-repo
  subcommand). **Durable-across-restart approvals stay open** — the daemon launches with the
  deny-and-record posture, and the cross-session approval inbox that would unlock Ask-Before
  schedules remains its own future design.
- The daemon becomes the third Driver, and the sequencing recorded in ADRs 0031/0033 stands:
  `apogee headless` (a thin CLI over `internal/run`) precedes it as the tripwire that keeps
  every capability off the TUI-only path.
- `schedules.yaml` becomes apogee's second declarative config surface, with validate-and-swap
  semantics; its envelope is the extension point through which triggers and workflow actions
  later arrive without migration.
- Daemon Firings appear in the TUI's `/sessions` browser labeled by schedule; short-cycle
  schedules will eventually need `keep: N` and browser filtering, both additive.
- `apogee daemon install` puts three OS-specific supervision surfaces into v1 scope — a
  recorded turnkey-over-lean choice.
- No wire surface exists on the engine or between apogee processes. The first apogee wire
  surface will be the webhook listener of the trigger wave, composed by the daemon Driver
  exactly as ADR 0031 invariant 1 prescribes.
- Cross-repo: the Gateway's behavior (priority queue, stickiness window, memory budgets,
  single-host v1 with federation later) is llama-launcher ADR-0013's to own; apogee consumes
  it as an ordinary OpenAI-compatible endpoint plus a request-level priority hint.
- `CONTEXT.md` gains **Daemon** as a canonical term; **Gateway** is a llama-launcher term,
  referenced from here, not duplicated.
