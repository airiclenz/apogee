---
title: ADR 0046 decision
---

**Every turn states the ceiling the engine already budgets for: the request carries a
`MaxTokens` derived from the Budget's own `ResponseReserve`, overridable per server, and a reply
cut off at that ceiling fails with a message that names it.** Concretely:

**1 — The cap is engine behaviour, and it holds in Bypass.** It is not a Mechanism and never
becomes one. The reasoning is `reviewedOutcome`'s, verbatim in kind: failure honesty — and, here,
resource honesty — is provider/engine correctness, not a Mechanism's job, so the guard has to fire
where no Mechanism is present to catch it. The cap steers nothing about how the model works the
problem; it states a bound the engine had already chosen, which is why the
ADR 0006 /
ADR 0009 bench gate is not owed: Bypass with the cap and Bypass
without it differ only in whether a runaway ends at the reserve or at the wall.

**2 — The pin is a per-server `max-output-tokens:` key, in the established three-state idiom.**
The ceiling is a property of the slot, like the window it is derived from, so it belongs on the
`servers:` entry beside `context-window:`
(ADR 0045 decision 3) and
`parallel-agents:`
(ADR 0039),
with the three states those keys already have: absent (or 0, which yaml cannot tell from absent) ⇒
the engine derives the cap from the reply budget; N ≥ 1 ⇒ that ceiling whatever the window says;
negative ⇒ refused by `ValidateServers`, on the reasoning those two checks already state — a
negative number is the one value with nothing to mean. It is legal on any entry because it
describes the server, and a delegation routed to the Sub-agent server derives from THAT entry's
numbers, not the parent's. No new global key: ADR 0036's
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
reasoning); the original text also ruled out any retry on the claim that one re-runs the same
request into the same ceiling — struck 2026-09-19, see the two notes below.

> **Superseded in part (2026-09-15, plan `2026-09-14 - 03` item 13).** The struck claim that a retry
> "re-runs the same request into the same ceiling" — and the fault text's matching last clause, "a
> retry meets the same ceiling" — is false for a reasoning model: its spend under the cap varies
> from pass to pass, and the same request has been seen answering on its second run (`30a3b2df`).
> The fault was changed to end "a retry may succeed on a reasoning model", leaving the choice to
> the reader. The no-retry half of the decision stood at that date — the raised-cap retry was parked
> on `apogee-tfp` — and the capped-summary fault of `compact.go` kept its no-retry wording while
> naming the cap the summariser request was actually sent with. *Amended 2026-09-19:* the parked
> retry has since landed (next note); the fault's "a retry may succeed" clause went with it.

> **Superseded in part (2026-09-19, plan `2026-09-19 - 01` item 2, `apogee-tfp`).** The engine now
> runs ONE retry of its own, in the loop and under Bypass, for exactly the reply this decision
> describes when it also carries reasoning: finish reason `length`, no tool call, no visible text,
> reasoning present. `respondAndReview` re-sends the identical request once with `MaxTokens` at
> twice the cap the capped request was sent with — the loop's derived cap, or a pre-request
> Reaction's override, whichever the request carried — unclamped, since the clamp bounds the
> engine's derivation and not a remedy for a reply already seen to fail under it; it emits a
> `StreamResetEvent` first so a streaming Driver discards the superseded reasoning, and it spends a
> latch of its own (`turnRun.capRetrySpent`, beside the transient re-stream's `restreamSpent`) so no
> other remedy's budget pays for it. It runs before the post-response Moment: a reasoning-only
> reply is not yet a reply the cascade should judge. A second cut-off faults as before, and the fault
> now ends `— retried once at N tokens and hit the cap again`, N being the raised cap. A capped
> reply with NO reasoning is not retried (nothing about it says a second pass would spend
> differently) and keeps the plain remedy; `cappedDelegateReplyErrFmt` (a truncated delegate answer)
> and compaction's capped-summary fault stay no-retry. The "Retrying or continuing a reply that hit
> the cap" rejection below is struck to the same extent.
>
> **Amended (2026-09-20, plan `2026-09-20 - 04` item 3; recorded by ADR 0082).**
> The transient re-stream's latch named above, `restreamSpent`, is history: a Turn now re-streams
> a transient fault up to a per-Turn budget (`turnRun.restreamsSpent` against
> `Agent.restreamBudget`, three by default, the `re-stream-budget:` key) with a doubling hold-off
> before each re-send. `capRetrySpent` stays a latch of its own beside that counter; the
> separation of budgets this note records is unchanged.
