---
title: ADR 0039 context
---

Every sub-agent today runs strictly serially: dispatch executes a reply's tool calls in
order (ADR 0013
runs the nested `Agent` inline on the caller's goroutine), and guided decomposition
deliberately dispatches **one child per Turn**
(ADR 0014 §3).
Meanwhile a llama.cpp server started with `--parallel N` holds N idle slots Apogee never
uses, and a cloud endpoint can take many concurrent requests. A model that emits three
`sub_agent` calls in one reply has already chosen N-in-one-Turn; running them one after
another is pure wall-clock waste the model did not ask for.

The grill (2026-08-07) surfaced the constraints that force the shape:

- **The server's parallelism is discoverable but ignored.** llama.cpp's `/props` reports
  `total_slots`; Apogee already fetches `/props` every discovery pass and heartbeat
  (ADR 0024)
  and reads only `n_ctx`. Cloud servers report nothing.
- **`/props` `n_ctx` is the PER-SLOT share** (ADR 0024): raising `--parallel` shrinks every
  agent's window. Concurrency and window size trade off at the server, not in Apogee.
- **An unbounded shared slot budget deadlocks.** With one budget across depths, N children
  can hold all N slots while each blocks waiting for a slot for its own children — a
  textbook semaphore deadlock; escaping it needs release-while-delegating accounting.
- **Interleaved child events are indistinguishable.** Events carry only `Depth`
  (`internal/domain/events.go`); serialized execution made depth sufficient. The code
  already flags the gap: `internal/run/run.go` notes concurrent fan-out would need a run
  identity on `UsageEvent`.
- **The hard invariant binds** (ADR 0006,
  ADR 0009): any change to guided decomposition's dispatch
  shape must re-pass the bench gate before defaulting on.
