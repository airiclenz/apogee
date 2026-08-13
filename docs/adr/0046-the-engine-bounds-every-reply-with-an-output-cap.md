---
Status: accepted
---

# The engine bounds every reply with an output-token cap

## Context

The engine already decides how big a reply is allowed to be and then never says so. `Allocate`
(`internal/context/budget.go`) splits the discovered window into a `ResponseReserve` — 20% by
default — and the working room the prompt's parts draw from, and `budget()`
(`internal/agent/loop.go`) hands that split to every Turn. But neither `domain.NewRequest` site in
the loop sets any sampling, so `MaxTokens` reaches the provider nil, the server is told
`n_predict: -1`, and the reply's only real ceiling is the context wall. The reserve and the request
disagree: one reserves room for a reply of a stated size, the other asks for a reply of unbounded
size.

That gap is invisible while models answer in a few hundred tokens and expensive the moment one does
not. On 2026-08-12 a `/security-audit` run on qwen3.6-35B-A3B (per-slot `n_ctx` 98,304) put one
sub-agent request at 20,653 reasoning tokens over ~50 minutes at ~6.2 tok/s, still generating, with
~81 further minutes of window to burn before the wall stopped it. At that wall the reply —
reasoning only, no visible text — would have failed through `reviewedOutcome` as `upstream returned
an empty reply (finish: length)`: a message that names neither a ceiling nor the twenty thousand
tokens spent reaching it, and that calls the largest reply of the session empty. A thinking model
makes the unbounded request a routine hazard rather than a corner case, and no Mechanism was ever
going to close it: `SamplingParams` (`internal/domain/hooks.go`) is a pre-request HOOK's surface,
and nothing in the engine populates it.

This record ratifies the calls the owner made that day. It supersedes nothing.

## Decision

**Every turn states the ceiling the engine already budgets for: the request carries a
`MaxTokens` derived from the Budget's own `ResponseReserve`, overridable per server, and a reply
cut off at that ceiling fails with a message that names it.** Concretely:

**1 — The cap is engine behaviour, and it holds in Bypass.** It is not a Mechanism and never
becomes one. The reasoning is `reviewedOutcome`'s, verbatim in kind: failure honesty — and, here,
resource honesty — is provider/engine correctness, not a Mechanism's job, so the guard has to fire
where no Mechanism is present to catch it. The cap steers nothing about how the model works the
problem; it states a bound the engine had already chosen, which is why the
[ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md) /
[ADR 0009](0009-the-ab-decision-rule.md) bench gate is not owed: Bypass with the cap and Bypass
without it differ only in whether a runaway ends at the reserve or at the wall.

**2 — The pin is a per-server `max-output-tokens:` key, in the established three-state idiom.**
The ceiling is a property of the slot, like the window it is derived from, so it belongs on the
`servers:` entry beside `context-window:`
([ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md) decision 3) and
`parallel-agents:`
([ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)),
with the three states those keys already have: absent (or 0, which yaml cannot tell from absent) ⇒
the engine derives the cap from the reply budget; N ≥ 1 ⇒ that ceiling whatever the window says;
negative ⇒ refused by `ValidateServers`, on the reasoning those two checks already state — a
negative number is the one value with nothing to mean. It is legal on any entry because it
describes the server, and a delegation routed to the Sub-agent server derives from THAT entry's
numbers, not the parent's. No new global key: [ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)'s
single-definition claim stays intact.

**3 — The default is the Budget's `ResponseReserve`, clamped to [4096, 32768].** Not a fixed
constant and not a fresh fraction: the reserve is the number the engine has already held back for
this reply, so deriving the cap from it is what makes the request and the budget agree rather than
two independent guesses about the same thing. The clamp is what keeps that derivation sane at both
ends — a small window would otherwise reserve a cap too small to answer in, and a very large one a
cap so generous it stops bounding anything. An unknown window (a zero `Allocation`) takes the
FLOOR, 4096, never "no cap": `Allocation`'s contract is explicit that a consumer must not read
unknown as unbounded, and it names the session that got wedged by doing so (audit 2026-08-01). A
cloud endpoint that advertises no window is exactly that case, and the pin from decision 2 is its
escape hatch — the one way such a server is allowed to answer at length.

**4 — A reply cut off at the cap fails with its own message.** When a reply has no visible text and
no tool calls AND the finish reason is `length`, the error names the cap and what was spent instead
of routing through `emptyReplyErrFmt`. The reader is being told something different from "the
Upstream returned nothing": the model DID answer, at length, and the engine's own ceiling stopped
it — so the fix is a bigger `max-output-tokens:` or a shorter task, not a retry. The turn still
fails, from the same source, with the same outcome: a branch and a message, not a control-flow
change. No partial-reply salvage (reasoning is not an answer, which is the existing guard's own
reasoning) and no retry Mechanism (a retry re-runs the same request into the same ceiling).

## Considered and rejected

- **A fixed constant default** (say 8192 everywhere): simpler to read, but it disagrees with the
  reserve on every window that is not the one it was tuned for — the same two-numbers-for-one-thing
  defect this record closes.
- **A top-level `max-output-tokens:` key**: the ceiling would then describe the session rather than
  the slot, and a delegation routed to a different server would carry the wrong one.
- **Leaving it to a Mechanism** (or to `SamplingParams` and a hook): it would be absent in Bypass,
  which is where an unbudgeted session most needs the floor.
- **Retrying or continuing a reply that hit the cap**: burns the same tokens again for the same
  outcome; the honest failure is the cheaper answer and the one the operator can act on.
- **Salvaging the visible text of a cut-off reply**: a reasoning-only reply has none, and half an
  answer committed as a Turn is the blank-assistant-message failure wearing better clothes.
- **Capping the auxiliaries too**: `internal/title/title.go` and `internal/agent/compact.go`
  already bound themselves at 4096 and are not turns; nothing about them is unbudgeted.

## Consequences

- `ServerEntry` grows `max-output-tokens:`; `ValidateServers` grows the negative refusal, in the
  message shape its two siblings use.
- The agent's context configuration and the Delegation target both carry the pin, so a child on the
  flagged server derives from that server's numbers (ADR 0045 decision 3's pin, one field wider).
- The loop gains `maxOutputTokens()` beside `budget()`, applied at both `domain.NewRequest` sites
  BEFORE the pre-request hooks run — which is what makes `SamplingParams`'s "a nil field leaves the
  loop's value untouched" contract true of `MaxTokens` for the first time: a hook that sets it
  still wins.
- The north star ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md))
  gains a guarantee rather than a constraint: because the cap is engine behaviour, every Driver —
  TUI, bench, a future daemon — inherits it, and no Driver has to bound replies itself. A bench
  Driver sets the pin through the same config surface an operator does.
- `Temperature` stays unset (the server's default). The emergency fold and its retry
  ([ADR 0018](0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md)),
  `defaultReserveFraction`, and the `Allocate` split are all untouched — this record adds a
  statement of the reserve on the wire, not a new allocation policy.
