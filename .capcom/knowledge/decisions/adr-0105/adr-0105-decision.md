---
title: ADR 0105 decision
---

**A `servers:` entry may name fallback servers. When its server fails mid-Turn and stays down
past a grace window, the seat rehomes to the first live fallback and resends the failed request.
The move is one-way and per seat, and it is reported as an `upstream_switched` Event.**

1. **Opt-in, per entry.** A `servers:` entry may carry `fallback:`, an ordered list of other
   entries' names, and `fallback-after:`, a duration in `delegate-timeout:`'s posture (default
   `2m`). An `--endpoint` / `APOGEE_ENDPOINT` one-run entry has neither. With no `fallback:`,
   behaviour is unchanged byte for byte: the same requests, the same events, the same exit codes.
   There is no headless flag for it; config is the only spelling.
2. **Bad config is refused at load.** A `fallback:` name that no entry carries, an entry naming
   itself, or a name listed twice is a config error naming the entry. Headless exits 2. The TUI
   and the daemon report it as the config error it is.
3. **The trigger is an upstream fault that would abandon the Turn today, and nothing else.** That
   means a transport or connection failure, a timeout or idle cut, HTTP 5xx, 429 or 529 (the
   Anthropic `overloaded_error` counts as 529), or a cut stream once ADR 0082's re-stream budget is
   spent. The trigger comes after that budget, never instead of it. Any other 4xx (400, 401, 403,
   413, …), a context overflow (ADR 0018's fold), and an empty or capped reply never trigger. A
   different server would not fix those, and the bare loop's answer to them stands.
4. **A grace window comes first.** Once triggered, apogee probes the primary's heartbeat for up to
   `fallback-after:`. If the primary recovers, the failed request is resent on it and no switch
   happens. At most one recovered resend is spent per Turn: a second fault in the same Turn skips
   the grace and walks the list, so a flapping primary cannot hold a Turn indefinitely. A cancel
   during the grace ends the Turn as a cancel. The shared grace runs on a context detached from any
   one agent, so cancelling the agent that started it does not strand the others waiting on it.
5. **The mid-Turn rehome.** The partial response is discarded, and a `stream_reset` precedes the
   resend. The seat is rehomed as ADR 0028 decision 3 rehomes it: a fresh client from an
   `UpstreamSpec`, the model unbound, and one beat on the fallback binding its model, window and
   per-model facts through a `RebindSpec`. Then the failed request is resent whole. A fallback with
   a smaller window is handled by ADR 0018's fold, as any overflow is. Prompt-cache warmth is lost.
   That is accepted: the alternative is an abandoned Turn. This is the one switch allowed
   mid-Turn; `/server` and `/model` stay idle-only (`adr-0028-amend-1102`).
6. **One-way, per seat.** There are two seats. The **session** seat is the session's own Upstream:
   the parent and every unrouted child. The **Sub-agents** seat holds the children routed to
   `sub-agents-server`. The first agent on a seat to trigger runs the grace and the switch,
   single-flight. Every other agent on that seat moves with it, and the seat emits one event. A
   seat never switches back by itself. A routed child follows its own entry's `fallback:` list,
   not the session entry's.
7. **The list walk.** The failed entry's list is walked in order, one beat each, with no grace per
   fallback. A fallback's own `fallback:` list is ignored: no chaining. If every fallback is down,
   the Turn is abandoned as it is today (headless exit 3).
8. **The engine stays wire-silent** (ADR 0031). The engine is handed an injected resolver per seat
   and the `UpstreamSpec` / `RebindSpec` it answers with. It never reads config. The Driver builds
   the resolver from the entry, and the resolver owns the single-flight and the fallen-over state.
   A bench or any other Driver can supply its own. This is the "behind the `provider.Responder`
   seam" placement `adr-0085-future` asked for.
9. **Reporting.** A new `upstream_switched` Event carries the from server and model, the to server
   and model, the fault, the time waited, and the seat (`"session"` or `"sub-agents"`). It is
   additive within event-line `v:2` (`adr-0075-amend-1102`) and is written to the Event journal
   (ADR 0103). The TUI writes a transcript note, headless a stderr line, the daemon a log line.
   Every reported string is sanitized. Exit codes are unchanged.
10. **The TUI switches automatically, and the human doors move back.** The heartbeat follows the
    bound entry after a fallover. `/server` moves the session back, and `/sub-agents-server` moves
    the Sub-agents seat back. Either installs a fresh resolver, so a seat a human moved is never
    left fallen over. A fallover calls no `SetDelegationSeat`: ADR 0069's line moves on the human
    doors only.
11. **Resume starts on the configured primary.** A fallover writes no `server:` key (ADR 0028
    decision 5 as amended by ADR 0036 records only a human switch). A resumed session starts on
    the entry config names. A session-seat switch sets the session record's model to the
    fallback's, so the record states what the session actually ran.
12. **Not a Reaction, and no bench arm.** It acts only where the bare loop has already failed and
    would abandon the Turn, so it cannot make a model perform worse than that loop. It is
    structural configuration, on under Bypass when configured (ADR 0006, ADR 0071 Decision 1
    clause (c)).

**Settled against the records `adr-0085-future` named.** ADR 0028: mid-Turn rehoming is defined
explicitly (this record's decision 5) rather than limiting a list to equivalent models. ADR 0047: that record
rejects a fallback chain across key sources; this record chains servers, each resolving its own
key through its own source, so there is no conflict. ADR 0082: its spent budget is the trigger
(decision 3). ADR 0085 decision 1 ("apogee never routes") stands: a fallover chooses nothing by
request content, cost or load. It follows an owner-written list, only after the seat has failed.

**How this differs from a Delegation seat's fallback.** ADR 0045 decision 4 and ADR 0069 send a
routed spawn to the session's Upstream when the Sub-agent server is unusable *at spawn*, and the
result says so in `SeatFallbackNote`. A Server fallback moves a seat *mid-Turn* onto an entry its
config names. Both stand, and `adr-0045-amend-1102` says how they meet.

**Out of scope:** switching back to the primary by itself, fallback on model-quality signals, a
bench arm, and a headless flag for `fallback:`.
