---
Status: accepted
Amends: ADR 0039 decision 4 (a faulted child's result carries a continue line and a draft note, and the child is retained); ADR 0013's 2026-09-18 amendment (retention is the capped OR faulted child's); ADR 0046 decision 4's 2026-09-19 note (the `restreamSpent` latch is a per-Turn budget)
---

# A silent stream is cut, and a transient fault is ridden out under a budget

## Context

On 2026-09-20 a `/code-audit` run on a hosted server (`openrouter-ds4`, deepseek-v4-flash routed
through one provider, every model call taking 3–8 minutes) lost a delegate the way apogee had
always been able to lose one. Delegate `L1 internal-floor-2` had spent 55 minutes, 34 tool calls,
13 Turns and ~2.3M prompt tokens when its next streaming call sat **520 seconds** with no bytes
and the wire finally returned an in-band chunk — `{"choices":[],"error":{"code":504,"message":
"Upstream idle timeout exceeded"}}`. apogee surfaced `loop: apogee: upstream in-band error 504
…`, the child faulted, the parent read `sub-agent faulted before finishing the delegated task:
…` and nothing else, and the child's conversation was discarded. The only thing that survived
was the draft findings file the skill prompt had told the delegate to write early (bead
`apogee-60x`).

Three settled rules, each right when it was made, combined into that loss:

- **`Stream` had no idle timeout.** The 2026-08 record under `[0.18.0]` in `CHANGELOG.md` ("The
  caller's context is documented and pinned as the stream's only deadline") pinned the caller's
  ctx as the body read's ONLY deadline, for the sake of a local model whose first token can take
  minutes on a large prompt. So a stalled proxy held the call until the proxy's own timeout —
  or, where the proxy never sends one, until Esc or `delegate-timeout:` (two hours by default),
  on a child nobody is watching.
- **A transient fault was re-streamed once.** `[0.14.0]` ("A transient upstream blip mid-stream no
  longer kills the exchange") gave a Turn one re-send after a fixed one-second wait, on a latch
  (`restreamSpent`) that a second fault of any class found spent. One second is a stutter; a
  provider being swapped out behind an aggregator, or a server shedding load, outlasts it.
- **Retention was the capped child's only.** ADR 0013's 2026-09-18 amendment retains a child that
  ends at a bound (`delegate-max-steps`, `delegate-max-tokens`, `delegate-timeout`) — folded,
  reported, continuable — and ADR 0039 decision 4 had a fault be that child's tool result and
  nothing more ("Failures are independent: a child's error, breaker trip, or denied approval
  becomes that child's tool result"). A bound is an outcome the engine chose; a fault the
  upstream chose. The hour of tool rounds before it is worth the same either way.

Two facts bound the shape. The engine is wire-silent ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
an idle cut is the provider Client's to make and the loop only ever reads `Delta.Retryable`, so
the whole change is `internal/provider` plus `internal/agent`, and no Driver learns anything. And
the floor invariant binds the model-facing side: the only text a model reads that it did not read
before is the continue line and the draft note, in the result slots the cap path already ships —
Bypass-neutral by construction, no bench gate owed.

This record ratifies the owner's calls of 2026-09-20 (plan `2026-09-20 - 04`).

## Decision

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
   read.

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

## Considered and rejected

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
- **A per-server `request-extra:` passthrough** (pinning a provider behind an aggregator so the
  stall does not recur): filed as `apogee-glh`, a separate concern.
- **Retrying at the HTTP layer instead** (`client.send`'s constants): the stall is past the headers,
  where the HTTP client's retries cannot reach; the body layer is the only place that sees it.

## Consequences

- `internal/provider`: `Stream` gains the idle cut (`errStreamIdle`, `WithStreamIdleTimeout`,
  `Client.StreamIdleTimeout`), `isTransientReadError` gains the third class. `internal/config`:
  `stream-idle-timeout:` and `re-stream-budget:` registry rows with their validators;
  `domain.Config` gains `StreamIdleTimeout` and `RestreamBudget`. `internal/agent`:
  `restreamBudget`, `restreamHoldoffFor`, `holdOffRestream`, `turnRun.restreamsSpent`;
  `finishAtFault`, `draftOutputSurvives`, `draftOutputNoteFormat`; the compaction summary's
  counter. Each was its own plan item under `docs/plans/2026-09-20 - 04`.
- `docs/manual/configuration.md` states both keys and the combined worst case; `CONTEXT.md`
  names the budget in *Turns and stepping* and the faulted retention in the P6 paragraph. ADR
  0013, ADR 0039 and ADR 0046 carry dated notes pointing here; the `[0.18.0]` and `[0.14.0]`
  `CHANGELOG.md` records are superseded as history.
- A `continue` after a fault composes over ONE fold: a child that faults again is retained anew
  under the same name, over the original task, never a fold of a fold — the cap path's rule,
  unchanged.
- Bead `apogee-60x` closes on this record.
