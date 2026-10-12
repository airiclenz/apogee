---
title: ADR 0039 considered options
---

- **Discovery overrides the config key ("fallback" shape)** — *rejected*: the repo's idiom
  is the opposite (`context-window`: set = a pin the heartbeat never overrides), and a
  wrong server-advertised number would be uncorrectable from config.
- **All depths share one slot budget** — *rejected for v1*: requires release-while-
  delegating accounting to dodge the self-deadlock; real concurrency machinery for a case
  (nested fan-out) small models barely exercise. Depth-0-only needs none of it.
- **Fail-fast sibling cancellation** — *rejected*: destroys completed work, hides surviving
  reports the model could have used, and adds cancellation paths; errors-are-results
  already composes.
- **Per-child event sinks** — *rejected*: every Driver (TUI, bench, `run.Once`, the future
  daemon) would need multiplexing; one additive `EventBase` field serves them all.
- **A separate mechanism batch knob** — *rejected*: two width numbers to explain and keep
  consistent; the bench can still tune by pinning `parallel-agents` on the bench server's
  entry.
- **Launcher-profile `Parallel` as a discovery source** — *rejected*: redundant with the
  live server's `/props`, and a second source can disagree with the first.
