---
title: A model-facing schedule tool — daemon-era, not v1
status: parked
type: feature
priority: 4
aliases: [apogee-54f]
created: 2026-09-03T18:40:52Z
---

**Status:** parked 2026-08-04 (assessment at the `/schedule` close-out,
[ADR 0033](docs/adr/0033-the-scheduler-is-a-library-and-the-tui-is-its-first-driver-surface.md)).
The question: should the model be able to create Schedules itself, via a tool, instead of telling
the human to run `/schedule`?

**Why not now — two costs, one thin benefit.** The benefit is a keystroke: a TUI-hosted Schedule
dies with the process, so the model would only ever set up something short-lived the human can
create with one command. Against that: (1) **catalogue dilution** — every schema costs context and
tool-selection accuracy for the 4B–35B target class, tools are not per-model curated the way a
lab Mechanism row is, and a tool relevant in one session in fifty is exactly what the never-worse
invariant says to refuse; (2) **authorization inversion** — ADR 0033 decision 3 makes the *human's*
creation choice the mode authorization, and a model-created Auto schedule is the model granting its
future self unattended runs; inside a Firing it is self-replication (a firing scheduling more
firings).

**The shape when built (so it is not re-derived).** Layering is *not* the blocker — the precedent
is `ask_user`: a driver-supplied delegate seam, nil = tool not registered, and
[ADR 0002](docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)'s open
registry means the driver that owns the scheduler registers the tool without touching the engine.
Creation routes through the **Approver** as a gated (non-ReadOnly) action, which buys the key
property for free: a Firing's fail-safe denier refuses it, so self-replication is structurally
impossible with no special-casing, and Ask-Before preserves decision 3's human authorization at the
gate. The residual hole is Auto (gated-but-eligible runs unsupervised — the exact bad case):
"create an Auto schedule" belongs in the footgun-guard's dangerous tier, or the tool caps
model-creatable schedules at Plan. Sub-agents don't inherit it (ADR 0005).

**The trigger to pick it up: a real need.** Durable schedules that survive quit are where a model
setting up its own monitoring has real value, and the companion feature is the cross-session
approval surface ADR 0033 already named when it rejected Ask-Before schedules. Build the tool once,
at that layer; the TUI still never has to register it. The daemon's shape has since been settled —
[ADR 0034](docs/adr/0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md),
an in-repo subcommand over a declarative trigger-action file — and `apogee daemon` itself shipped
on 2026-08-22, which made this tool possible, not needed. Re-parked 2026-08-22 (owner call): this
entry stays parked until a real need for a model-facing schedule tool appears.
**Not the vehicle:** MCP — schedules are
in-process driver state, Firings run without MCP, and ADR 0031 forbids first-party connectors.
