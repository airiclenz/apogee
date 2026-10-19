---
title: ADR 0082 decision
---

1. **A silent stream is cut at the body layer, and the cut is a transient fault.** `Stream`
   (`internal/provider/stream.go`) bounds how long an upstream may stay SILENT — waiting for its
   response headers, or between two body reads — and cuts it when the window elapses, surfacing
   the cut as a terminal `DeltaError` marked `Retryable` (`errStreamIdle`). It is the third
   transient read class beside a connection dropped mid-chunk (`io.ErrUnexpectedEOF`) and a
   network timeout (`isTransientReadError`), because a stalled proxy or a dead generation shows up
   as silence long before any 504 does. **Bytes of any kind are activity**: a reasoning delta, an
   SSE keep-alive comment, a chunk the decoder skips — each resets the clock, so a slow
   generation is never cut, only a stalled one. The window is `stream-idle-timeout:` (a file-only
   key, a duration string in `delegate-timeout:`'s posture; `0` disables), default **`10m`**, and
   it rides every dial the engine makes (`dialOptions` — construction, a `/server` switch, a
   routed spawn) through `provider.WithStreamIdleTimeout`, so the session's Client and a child's
   carry it alike. The engine's `Config.StreamIdleTimeout` reads ZERO as off — an embedder's zero
   Config waits for as long as the server takes and never meets the provider Client's own default;
   the host folds the key's default in.

   Ten minutes, not lower, because a reasoning model thinks silently for minutes and a hosted
   provider's routine call on the day this was observed took 3–8 minutes end to end: a default
   that cuts a healthy generation would make the model perform worse than the bare loop, the one
   thing the floor forbids. Ten minutes, not off, because the observed stall was 520 seconds
   before the proxy said anything, and a proxy that says nothing holds a delegate for
   `delegate-timeout:`. This default **supersedes** the `[0.18.0]` record: a local server whose
   prefill exceeds ten minutes sets `stream-idle-timeout: 0` or a longer window.

   > **Amended 2026-10-12 (plan `2026-10-12 - 00` item 4).** "The bare loop" is **Bypass mode**
   > (ADR 0006), and the idle cut is structural
   > configuration that stays on under Bypass, so it is part of that baseline. Read "worse than the
   > bare loop" here and in the *shorter default* alternative below as "worse than the loop without
   > the cut": the same admission test ADR 0071 Decision 1, clause (c) puts to a Floor guard.

   > **Amended 2026-10-19 (plan `2026-10-19 - 00` items 6–9).** The window still rides every dial
   > the engine makes; its value is now the **bound server's**. A `servers:` entry may state its own
   > `stream-idle-timeout:` (the same parser): absent inherits the top-level key, `0` turns the cut
   > off for that server, a positive length is that server's window
   > (`config.ResolveStreamIdleTimeout`). The host resolves it at each bind — the startup bind, a
   > `/server` switch (an entry without the key falls back to the top-level value as the session
   > holds it at the switch), a routed spawn (its target entry's) and a Firing — and the one-run
   > `--endpoint` server takes the top-level value. Naming calls and discovery probes keep the
   > provider Client's own default. "Bytes of any kind are activity" is unchanged: a keep-alive
   > comment resets a per-server window as it resets the top-level one, so a lower value still
   > catches only a server gone wholly silent.

2. **A Turn re-streams a transient fault up to a per-Turn budget, holding off longer each time.**
   `re-stream-budget:` (a file-only key, an int; `0` = never re-stream), default **3**, replaces the
   one-re-stream latch: a Turn whose request faults transiently — an in-band 429/5xx or
   `provider_unavailable` inside an HTTP 200, a body cut mid-stream, a network timeout, the idle
   cut of decision 1 — re-sends the SAME request while its count (`turnRun.restreamsSpent`) is
   under the budget (`Agent.restreamBudget`), waiting out a hold-off that **doubles** from the
   existing one-second base before each re-send (`restreamHoldoffFor`: 1 s, then 2 s, then 4 s).
   Each re-send emits the `StreamResetEvent` an `Outcome{Retry}` emits, so a streaming Driver
   discards the partial reply; a re-stream that lands is SILENT, exactly as a recovered overflow
   fold is; the fault that finds the budget spent, of any class, surfaces as every fault always
   did, and a cancel during a hold-off is routed as the cancel it is. The budget is the same at
   every depth — a child's Turn gets what depth 0 gets — and the compaction summary's re-stream
   shares it on a counter of its own (`compact.go`), because a fold is not the Turn it runs
   beside. `Config.RestreamBudget` is a pointer so that the zero Config keeps a re-stream: `nil`
   is the default of three, a pointer to `0` never re-streams (contrast decision 1, whose zero
   disables). `capRetrySpent` — ADR 0046's retry of a capped reasoning-only reply — stays a latch
   of its own beside the counter; no remedy's budget pays for another's.

   The neighbour that stays is ADR 0018's one-fold-per-Turn rule: a second overflow proves that
   folding is not the remedy, whereas a second transient fault proves nothing about the third —
   the condition behind it (a provider being swapped out, a server shedding load, a proxy
   stalling) is measured in seconds to minutes, which is what the doubling ladder waits for. The
   two keys compose into a stated worst case: a server gone silent for good costs a Turn up to
   (budget + 1) × `stream-idle-timeout:` plus the 7 s of hold-offs before the Turn fails — just
   over 40 minutes at the defaults — where before it cost the delegation.

3. **A faulted delegate is retained exactly as a capped one is.** This extends the P6 cap
   retention of ADR 0013's 2026-09-18 amendment to the child whose Run ends `Faulted` — its Turn's
   budget spent, or a fault that was never transient — **with the parent's ctx still live**
   (`ctx.Err() == nil`). On the way out, `finishAtFault` (`internal/agent/agent.go`) writes the
   engine fold of the conversation as it stands at the fault (`foldForParent`, the same summary
   call the cap path makes, booked `Maintenance` with the `DelegateFold` flag, no Turn) and the
   parent retains the child in `retainedDelegates` under its Delegation name — task, roster,
   `output_path`, fold, closing text, outcome — for the rest of the parent's Exchange, so
   `sub_agent` with `continue: "<name>"` re-spawns from the fold rather than from nothing. The fold
   is the ONLY model call: there is no wrap-up Turn, because a faulted delegate has no upstream to
   spend one on, and the child's last narration stands as its closing text. Three edges keep the
   cap path's contracts: the fold runs under one `stream-idle-timeout` of its own — the summary
   call is itself a stream that may stall — so the error result lands at most one idle window
   late, and a fold that exceeds it or fails retains the cap path's marker
   (`[engine summary unavailable — <cause>]`); a child that completed NO Turn is retained without
   a fold request, its fold the marker saying so, because the continuation task renders the fold
   under its head unconditionally; and a **cancel still unwinds the whole delegation and retains
   nothing** (`runSubAgent` D2) — a top-level Run is exempt too, its faults being the human's to
   read. (Amended 2026-09-30: a cancel no longer unwinds the delegation.
   ADR 0088 D2 stops a running child as
   `^x` does — folded, given a partial result and retained under its name — and keeps a finished
   one's report; `internal/agent/subagent.go`.)

   The result the parent reads is still the ERROR result: its head still says
   `sub-agent faulted before finishing the delegated task: …` and why, and its body now carries,
   in the slots the cap path already ships, the **draft note** — `[draft output at <path> written
   before the fault]`, only when the call named an `output_path` and the child wrote that file
   during its Run (a pre/post stat around the Run, so a file that predates the spawn earns no
   note; never in Plan mode, where nothing was writable) — and, last, the **continue line**
   `[to continue this delegate: sub_agent with continue: "<name>"]`. ADR 0039 decision 4's
   independence of failures is unchanged — siblings still run to completion and the parent's next
   primary call sees all N results — and its sentence is amended by a dated note; ADR 0013's
   amendment gains one saying retention is the capped OR faulted child's.

4. **This is floor, engine-side, Bypass-neutral.** Decision 1 lives in the provider Client;
   decisions 2 and 3 in the loop and the sub-agent orchestrator; no Driver carries a line of it
   (ADR 0031 door 1, the wire-silent engine, stands: the loop reads `Retryable` and never learns
   what class of silence tripped it). The Approver seam is untouched (door 2), nothing connects
   (door 3), and both keys are config surface a bench Driver sets as an operator does (door 4).
   Nothing here is a Reaction: no key arms it, Bypass does not switch it off, and the only new
   model-facing text is two body notes on a result that was already an error.
