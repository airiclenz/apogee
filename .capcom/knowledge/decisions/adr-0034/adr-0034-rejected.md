---
title: ADR 0034 considered options
---

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
