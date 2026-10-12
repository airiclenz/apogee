---
title: ADR 0082 context
---

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

Two facts bound the shape. The engine is wire-silent (ADR 0031):
an idle cut is the provider Client's to make and the loop only ever reads `Delta.Retryable`, so
the whole change is `internal/provider` plus `internal/agent`, and no Driver learns anything. And
the floor invariant binds the model-facing side: the only text a model reads that it did not read
before is the continue line and the draft note, in the result slots the cap path already ships —
Bypass-neutral by construction, no bench gate owed.

This record ratifies the owner's calls of 2026-09-20 (plan `2026-09-20 - 04`).
