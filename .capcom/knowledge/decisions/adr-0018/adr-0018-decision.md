---
title: ADR 0018 decision
---

**1. Overflow protection is STRUCTURAL.** Every gate on the recovery path consults
`cfg.Context.CompactionEnabled` and never `cfg.Bypass`, so recovery runs under `--bypass` exactly
as Budget and automatic Compaction do (ADR 0006, D6). The single opt-out is the file-only
`auto-compact: false`: the emergency fold IS an automatic fold, so a user who chose to manage the
window themselves keeps the pre-recovery abandon behaviour, unchanged and unsurprising. The
shipped config template stays behaviour-neutral — nothing here is a new default to enable.

**2. A wire overflow is its own Turn outcome, and giving up is byte-identical to before.**
*(Amended 2026-08-02 — see the Amendment section below: byte-identical still holds whenever a
window is known; with NO window known the give-up appends the remedy that ends the wedge.)*
`turnOverflowed` joins `turnOK` / `turnCancelled` / `turnFailed`. `respondAndReview` deliberately
*withholds* the `ErrorEvent` for it and carries the sanitized message to the caller, because an
overflow is the one Upstream fault the loop can act on and a recovered Turn must stay quiet. The
caller owns the decision, so it owns the event: when recovery is refused or spent, `step()` emits
that carried message verbatim — same `Source: "loop"`, same text, same ordering, same
`abandonTurn` — so a give-up is indistinguishable from what shipped before this ADR.

**3. On the MAIN agent, the emergency fold is the ONE fold allowed to run MID-EXCHANGE.**
*(Amended 2026-08-26 — on a CHILD agent the estimate-driven trigger runs mid-Exchange too, at
quiescent Turn boundaries; see the 2026-08-26 Amendment section below.)* It amends S2's
Exchange-boundary-only rule *for this path alone*: on the main agent the estimate-driven trigger
(`shouldAutoCompact`) and the on-demand `/compact` (which refuses mid-Exchange with
`ErrInputPending`) both stay boundary-only. The asymmetry is the point — their caller can wait for
the next opening, while a Turn whose request the server just rejected cannot: deferring there
means abandoning the Exchange, which is the failure this recovery exists to prevent. Running
mid-Exchange is safe because of the fold's own shape: `context.Compact` keeps the protected prefix
(leading system messages + the first user message) and *Replaces* everything after it with a
single assistant summary, so no half-answered tool call survives to be orphaned. Because that
rewrite can drop the open Exchange's opening user message, the fold re-anchors the cached
`exchangeStart` to the bridge's index — the recorded fallback of
ADR 0017 §2, mirroring the S2 repair
`step()` performs after a mid-Exchange `truncate_history` shrink.

**4. The folded conversation ends with a user-role bridge.**
*(Amended 2026-08-28 — the bridge belongs to every fold that runs MID-EXCHANGE, so a child's
estimate-driven fold appends the same one; see the 2026-08-26 Amendment section below.)* On success
the fold appends one constant message (`overflowBridge`) telling the model that the conversation above was compacted
because the previous request exceeded the window, and to continue from the summary. Its ROLE is
the load-bearing half — the conversation would otherwise end in an assistant message, which a
strict chat template refuses and an instruct model reads as "keep writing that summary" — leaving
`… first-user | assistant-summary | user-bridge`: strict alternation holds and no dangling tool
calls survive, so any chat template accepts the retried request. Its TEXT is the other half: the
model is told in-band why its history shrank, so it resumes the task instead of re-asking for
context it will never get back.

**5. One recovery fold per Turn — predictive or reactive, whichever fires first.** The bound is
`maxOverflowRecoveries = 1`, spent through the respond phase's own attempt counter rather than a
separate flag, so the predictive guard (decision 7) and the reactive retry share one budget: a
predictive fold enters the respond loop with the counter already at the cap. A second overflow
means folding is not the answer here — the protected prefix alone is over the window, or the
server rejects even a minimal prompt — and the Turn gives up per decision 2. A fold that refuses
(opted out, nothing past the protected prefix to shed, or the summary call itself faulted) is the
same give-up; a summary-call fault additionally surfaces one `ErrorEvent` from source
`"compaction"`, mirroring `autoCompact`, and leaves the conversation untouched.

**6. Quiet on success, and a cancel after the fold KEEPS the fold.** No new Event variant and no
TUI affordance: the `Replace` and the retried request's `UsageEvent` are the visible effect,
exactly as for an automatic fold (the context gauge simply drops). And because the fold is history
maintenance rather than part of the Turn's attempt, the cancellation rollback boundary moves
*past* it — `cancelTurn` can never roll back into a pre-fold index, the same way a cancel after
`autoCompact` keeps that fold.

**7. The predictive guard fires only on "cannot fit", and is DAMPED — not gated — while the
Budget is uncalibrated.** *(Amended 2026-08-02 (second) — the working room below is the KNOWN-window
threshold and the whole-request measure is the KNOWN-window measure; with no window the guard
compares the transcript alone against the same conservative ceiling the fold renders against,
instead of standing down. See the second Amendment section.)* *(Amended 2026-08-26 — the Budget
now splits the advertised `Window` from the working `ContextLimit`, and the field the threshold
below names is `Window`; see the 2026-08-26 Amendment section.)* Before a request is sent,
`requestExceedsWindow` estimates it with the measure the whole engine shares (`domain.PromptChars`
through `Budget.EstimateTokens`) against the FULL HARD room — the ADVERTISED window less the reply
reserve (`Window − ResponseReserve`) — never a softer fraction: a fold is a lossy rewrite of the
user's history, so it must fire only when the estimate says the request cannot fit at all, never
as a comfort margin (the ~60%-of-working-room History allocation stays the boundary
trigger's business). While no server usage has been reported yet (`Budget.Used == 0` — Turn 1,
every sub-agent, and the first Turn after a resume, where the estimator is deliberately not
serialized while the restored history may already sit near the window) the threshold is
`uncalibratedRoomMargin` (= 2) times that room. Rationale is an asymmetry: an under-estimate costs
nothing (the wire overflow still routes to the reactive path) while an over-estimate spends an
irreversible lossy fold plus a summary call on a request that would have fit. Two is
`maxCharsPerToken / DefaultCharsPerToken` (8.0/4.0), so a false positive is impossible anywhere
inside the estimator's own clamp band while every pathological case still fires with room to spare
(a 10 MiB read is ~25x over). The guard earns its place twice: it saves the round-trip on a
predictable overflow, and it is the ONLY protection against a server whose 400 body the provider
cannot classify as an overflow — there the stream yields a plain `DeltaError` and the reactive
path never fires. Damping rather than gating is what keeps that cover on the Turns most at risk.

**8. A structural floor on a single oversized tool result lives in the LOOP, not in the
Mechanism.** *(Amended 2026-08-02 — the `window - 4608` transcript budget below is the KNOWN-window
arithmetic; an unknown window now budgets a flat default instead of rendering everything. See the
Amendment section. Amended again the same day — with no window the floor itself measures against
that same flat default rather than being disabled, so the two meet exactly; see the second
Amendment.)* A tool result whose estimated tokens exceed the ENTIRE History allocation is clamped
to a head/tail-plus-marker elision as it enters the conversation (`appendToolResult` — the one
seam every result crosses, so no route bypasses it). A result bigger than everything History may
hold can never survive any reducer, and it can doom the Turn outright: the emergency fold's own
summary call keeps the most recent message unconditionally, so a fresh giant result IS that
message and overflows the fold that was supposed to rescue the Turn. The floor therefore sits
BELOW the fold's transcript budget at every window an agent can realistically run in, which is the
property that keeps the fold survivable. That ordering is arithmetic between two independent
constants, not an invariant: History is ~60% of the working room (~48% of the window at the default
20% reserve) while the fold budgets its transcript at `window - compactMaxTokens -
compactPromptOverheadTokens` (= `window - 4608`), so the floor sits below it only while
`0.6 x (window - reserve) < window - 4608` — windows above ~8.9k tokens at the default reserve
(≥ 8865 with the integer arithmetic). Below that crossover the floor is the looser of the two and
the survivability property lapses; that band sits far under the ~32k target window and is too small
to run a coding Turn in, so it is stated here rather than defended in code. Placing
it in the loop rather than in `tool_result_cap` is ADR 0006's requirement, not a convenience: a
structural reducer must stay on in the baseline and the baseline must be functional, whereas the
Mechanism is default-off, Bypass-disabled, withdrawable by self-regulation, and caps only the
turns BEFORE the most recent tool call — precisely never the result that overflows.

**9. `tool_result_cap` remains the tunable A/B-able valve above the floor.** Its tighter
40%-of-working-room nudge shapes the ordinary case and, when enabled, fires first and leaves the
floor a no-op. The two ceilings are deliberately far apart: the floor is pathological-only.

Rejected alternatives: shipping recovery as a Mechanism (a config-dependent, withdrawable
guarantee — ADR 0006); a softer predictive threshold or a comfort margin (a fold is lossy);
more than one fold per Turn (a second overflow proves folding is not the remedy); gating the
predictive guard on first calibration (it would leave Turn 1, every sub-agent, and the first Turn
after a resume unprotected — exactly where the restored history sits nearest the window); a new
Event variant or TUI affordance for a successful recovery (revisit only if live use proves the
quiet fold confusing).
