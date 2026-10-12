---
title: Firing
---

**Firing**:
One run of a **Schedule**'s prompt: a **fresh** agent constructed through the **Embeddable
agent**'s public API, the prompt submitted, the loop driven to the quiescent boundary, and the
result saved as an ordinary **Session record** marked with its Schedule's identity (id and name
on browsable `Meta`) so the `/sessions` browser can label it. A Firing *is* a **headless run** —
the same act as the `apogee headless` runner, over the shared core (`internal/run`)
both use. It carries nothing over from the previous Firing (fresh context; no summary is
injected — model-visible content would be an **advise** Reaction to bench, not a scheduler
feature), its Approver is a **fail-safe denier** (a gated action fails visibly and nothing waits
for a human), its **Asker** and **Presenter** are `nil`, and it saves **once, at completion** — a
deliberate scoping of ADR 0022's per-Turn cadence to a bounded, unattended run. Firings of one Schedule are
strictly serial: a tick landing while one is in flight is **skipped**, never queued. See
ADR 0033.
_Avoid_: "background task" / "detached process" (a Firing is a Session in this process, and it
ends), "job run", "cron run".
