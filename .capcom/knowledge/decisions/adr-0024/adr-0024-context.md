---
title: ADR 0024 context
---

Apogee learned what model it was talking to **exactly once**, at startup. `runRoot` called
`provider.Client.Discover` before the first paint, wrote the answer into `tui.Options{Model,
ContextWindow, …}` and into `apogee.Config`, and nothing ever wrote either again — the only
precedent mutation of `Options` was `Mode` on Shift+Tab. Everything downstream inherited that
one-shot answer for the life of the session: the footer's `model ✦ ctx`, the start-up box, the
context-usage gauge's denominator, the Budget and the automatic Compaction trigger
(ADR 0018), the
per-model system prompt (ADR 0023),
the validated Mechanism set (ADR 0016),
and the model id stamped on every saved session record
(ADR 0022).

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
(ADR 0021)
is the diagnosis command and CONTEXT.md is explicit that it "diagnoses, it does not monitor". A
dead `provider.ServerManager` carried a `Reachable` health probe and a local-process launcher, but
was never constructed in production. The engine had no way to change a per-model binding at all:
`agent.upstream` and `cfg.Model` were fixed at construction, and `errMissingModel` refused a
model-less one.

This ADR records the design the owner ratified in the 2026-07-27 grill, implemented by
`docs/plans/2026-07-27 - 00 - upstream-heartbeat-plan.md`. It supersedes nothing; it extends
ADR 0011's idle-only engine-call
class by one member and gives ADR 0021's parked "who monitors, then?" question its answer.
