---
title: ADR 0034 decision
---

**1. Packaging: `apogee daemon` is an in-repo subcommand.** One repo, one binary, beside
`probe` and the deferred `headless`. `internal/run` and `internal/schedule` stay unexported;
the facade-export question ADR 0033 closed stays closed; the single-static-binary promise
holds.

**2. Topology: separate runtimes, no IPC.** The daemon and the TUI are independent processes
that share libraries and stores, never a protocol. The TUI's session-scoped `/schedule` is
unchanged; neither process depends on the other running. Ephemeral-while-open and durable are
different promises, and two surfaces stating them separately is honest. IPC can be added later
without unwinding anything decided here.
(Amended 2026-10-17, ADR 0103 D6: a TUI watching a daemon Firing tails that Firing's Event
journal. A tailed file is not IPC: the daemon never learns a watcher exists, nothing flows back,
and neither process depends on the other.)

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

> **Amended 2026-09-27 (ADR 0087 D10).** The reserved `run: workflow:` key arrives as
> `run: workflow: {recipe, inputs}`: an entry names a Recipe and the scope to run it over, and the
> daemon starts that Workflow through the same engine library the TUI and headless `--recipe` use.
>
> **Amended 2026-08-22 (ADR 0055 D1–D3).** The `run:` block also takes `server:`, which names a
> `servers:` entry in `config.yaml` (absent: the host's startup default), and `model:`, a per-request
> model selection that is refused on a launcher-fronted entry. The daemon never actuates the launcher.
> Decision 10's "name models explicitly or inherit the configured default" reads through this.

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
Mechanism per ADR 0031 invariant 4, never as daemon plumbing. (Amended 2026-09-30: the Mechanism
catalogue is gone — ADR 0071
froze it and ADR 0076 deleted it —
so there is no catalogued route to land in. ADR 0031 invariant 4 is the rule that still binds
payload injection; the daemon ships no webhook trigger yet.)

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
