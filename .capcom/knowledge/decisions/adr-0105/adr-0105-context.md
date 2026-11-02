---
title: ADR 0105 context
---

Seqcom runs apogee unattended in headless mode on a local model, with a cloud model configured
beside it (request `apogee-midrun-fallback`, run 20261031T1223Z, apogee v0.29.1; issue
`midrun-model-fallback`). When the local server dies partway through a run, the run loses most
of its time window. Nothing hands the session to the second configured server.

What the code had when the grill opened (2026-11-02, base 81b692b1):

- **ADR 0082 rides out a transient fault under a budget.** A silent stream is cut, a cut or
  dropped stream is re-streamed within a per-Turn budget, and once that budget is spent the Turn
  is abandoned. Headless exits 3.
- **ADR 0028 binds a session to one server at a time and moves it only at idle.**
  `SwitchUpstream` unbinds the model, and the new server's first beat binds it again. It sits in
  the idle-only class, so no request is ever re-pointed at another server underneath itself.
  `/server` and `/model` are the human doors.
- **ADR 0085 named fallback pools as future work.** Its `adr-0085-future` Note accepts them and
  asks for their own ADR, written against three settled records. ADR 0028 binds a session to one
  server, so a pool must either be limited to equivalent models or define mid-Turn rehoming
  explicitly. ADR 0047 rejects a fallback chain across key sources (not servers). ADR 0082's
  budget is the trigger. A pool must also keep the engine wire-silent (ADR 0031).
- **A Delegation seat already "falls back", in a different sense.** ADR 0045 decision 4 and ADR
  0069 send a routed spawn to the session's own Upstream when the Sub-agent server is unusable at
  spawn time. The result carries one note line (`SeatFallbackNote`). That fallback picks the seat
  before the child starts. It never moves a running child, and it never reads a list of servers.
- **ADR 0018 recovers a context overflow by folding** the conversation and retrying once, so a
  smaller window on another server already has a structural answer.

The owner ratified the calls below in a grill on 2026-11-02.
