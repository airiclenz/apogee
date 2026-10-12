---
title: ADR 0082 considered and rejected
---

- **Leaving the idle timeout off by default** (the `[0.18.0]` posture): the persona it protected —
  a local model with a long prefill — can set `0` or a longer window and lose nothing; the persona
  a default protects is a delegate nobody is watching, which cannot set anything. Off-by-default
  makes the observed loss the shipped behaviour.
- **A shorter default (one or two minutes)**: reasoning models think silently for minutes, and a
  hosted call that takes 3–8 minutes was the ordinary case on the day; a cut that fires on a
  healthy generation degrades the model below the bare loop, which the floor forbids.
- **Counting only visible content as activity**: a reasoning delta is the model working; a
  keep-alive comment is the proxy saying the connection is alive. Either being "silence" would
  cut exactly the replies that take longest and matter most.
- **A wall-clock re-stream budget instead of a count**: the count composes with the idle window
  into a stated worst case; a clock would double-count the window it already bounds.
- **A fixed hold-off repeated N times**: three stutters in a row wait out nothing a single one did
  not; the doubling ladder gives a condition that outlasts one wait a longer one before the next.
- **Retaining the faulted child's raw messages, or a snapshot, for `continue`**: the bead's
  option B raw variant. A second retention shape beside the fold, a memory cost per retained
  child, and a continuation that re-reads a whole history instead of a summary authored for
  exactly that purpose; the fold is the shape the continuation already reads.
- **A wrap-up Turn on a fault**: a faulted delegate has no upstream to spend a Turn on, and a
  wrap-up that itself faults would delay the error result by another budget's worth of windows.
- **Retaining a cancelled child**: ADR 0013 §5 and `runSubAgent` D2 — Esc unwinds the whole
  delegation; a finer cut inside the pool is `apogee-2un` Stage B and needs its own grill.
  (Amended 2026-09-30: adopted since — ADR 0088 D2 closed Stage B and retains a cancelled child.)
- **A per-server `request-extra:` passthrough** (pinning a provider behind an aggregator so the
  stall does not recur): filed as `apogee-glh`, a separate concern.
- **Retrying at the HTTP layer instead** (`client.send`'s constants): the stall is past the headers,
  where the HTTP client's retries cannot reach; the body layer is the only place that sees it.
