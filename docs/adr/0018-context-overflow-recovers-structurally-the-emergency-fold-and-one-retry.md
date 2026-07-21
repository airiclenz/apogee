---
Status: accepted
---

# Context overflow recovers structurally: the emergency fold and one retry

## Context

A `/refocus` run against a 32k-window model died mid-task on the llama.cpp 400 —
`request (57546 tokens) exceeds the available context size (32768 tokens)`. The wire
classification was already right (`isContextOverflow` → `DeltaContextOverflow`), but the loop
folded that delta into the same terminal outcome as any stream fault: one `ErrorEvent`, then
`abandonTurn` — the Exchange over, the task lost, the history intact but useless.

Nothing proactive could reach the failure. Automatic Compaction is Exchange-boundary-only (the S2
`inExchange` gate), and a doc-heavy Exchange — assistant → whole-file read → assistant → … —
grows past the window exactly where the boundary trigger cannot fire. The only reducer that could
act mid-Exchange was `tool_result_cap`: a default-off, Bypass-disabled, self-regulation-
withdrawable Mechanism. And no loop-level bound existed on a single tool result entering history
at all — dispatch appended verbatim, `read_file`'s only ceiling being a 10 MiB refusal, so one
`read_file` of a large file could put more text into history than the whole window holds.

That left the guarantee resting on a config opt-in, which
[ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md) forbids: the structural reducers
(Budget, Compaction) are load-bearing parts of the agent, must stay on in the Bypass baseline, and
that baseline must be *functional* — a floor that quits at the first stumble passes the hard
constraint trivially. Overflow recovery therefore belongs with Budget and Compaction, not in the
catalogue.

## Decision

**1. Overflow protection is STRUCTURAL.** Every gate on the recovery path consults
`cfg.Context.CompactionEnabled` and never `cfg.Bypass`, so recovery runs under `--bypass` exactly
as Budget and automatic Compaction do (ADR 0006, D6). The single opt-out is the file-only
`auto-compact: false`: the emergency fold IS an automatic fold, so a user who chose to manage the
window themselves keeps the pre-recovery abandon behaviour, unchanged and unsurprising. The
shipped config template stays behaviour-neutral — nothing here is a new default to enable.

**2. A wire overflow is its own Turn outcome, and giving up is byte-identical to before.**
`turnOverflowed` joins `turnOK` / `turnCancelled` / `turnFailed`. `respondAndReview` deliberately
*withholds* the `ErrorEvent` for it and carries the sanitized message to the caller, because an
overflow is the one Upstream fault the loop can act on and a recovered Turn must stay quiet. The
caller owns the decision, so it owns the event: when recovery is refused or spent, `step()` emits
that carried message verbatim — same `Source: "loop"`, same text, same ordering, same
`abandonTurn` — so a give-up is indistinguishable from what shipped before this ADR.

**3. The emergency fold is the ONE fold allowed to run MID-EXCHANGE.** It amends S2's
Exchange-boundary-only rule *for this path alone*: the estimate-driven trigger
(`shouldAutoCompact`) and the on-demand `/compact` (which refuses mid-Exchange with
`ErrInputPending`) both stay boundary-only. The asymmetry is the point — their caller can wait for
the next opening, while a Turn whose request the server just rejected cannot: deferring there
means abandoning the Exchange, which is the failure this recovery exists to prevent. Running
mid-Exchange is safe because of the fold's own shape: `context.Compact` keeps the protected prefix
(leading system messages + the first user message) and *Replaces* everything after it with a
single assistant summary, so no half-answered tool call survives to be orphaned. Because that
rewrite can drop the open Exchange's opening user message, the fold re-anchors the cached
`exchangeStart` to the bridge's index — the recorded fallback of
[ADR 0017](0017-the-exchange-is-a-derived-domain-working-value.md) §2, mirroring the S2 repair
`step()` performs after a mid-Exchange `truncate_history` shrink.

**4. The folded conversation ends with a user-role bridge.** On success the fold appends one
constant message (`overflowBridge`) telling the model that the conversation above was compacted
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
Budget is uncalibrated.** Before a request is sent, `requestExceedsWindow` estimates it with the
measure the whole engine shares (`domain.PromptChars` through `Budget.EstimateTokens`) against the
FULL working room (`ContextLimit − ResponseReserve`), never a softer fraction: a fold is a lossy
rewrite of the user's history, so it must fire only when the estimate says the request cannot fit
at all, never as a comfort margin (the ~60%-of-working-room History allocation stays the boundary
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
Mechanism.** A tool result whose estimated tokens exceed the ENTIRE History allocation is clamped
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

## Consequences

- A mid-Exchange overflow is survivable: the history folds, the same Turn re-sends, and the task
  continues from the summary. What the user loses is verbatim history, not the task.
- **`tool_result_cap` is no longer "the only reducer able to act mid-Exchange"** — the claim in
  `CONTEXT.md` is corrected, and the same premise sentence in
  [ADR 0014](0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md) §4 is
  amended by this decision. ADR 0014's *decision* stands unchanged: `guided_decomposition` still
  `Requires` `tool_result_cap`, because the emergency fold is reactive, lossy, and capped at one
  per Turn — a rescue, not a way to keep accumulation down.
- The reducer taxonomy grows a second automatic Compaction trigger (`internal/context/doc.go`,
  `CONTEXT.md`): estimate-driven at the Exchange boundary, overflow-driven mid-Exchange. The
  generative fold itself is reused unchanged — one `context.Compact`, two triggers.
- `auto-compact: false` now opts out of overflow recovery as well as boundary folding. That is
  stated in the shipped config template's `auto-compact` comment, because the key's blast radius
  grew.
- The public API is untouched — no new Event variant, no facade change, `internal/provider`
  unmodified (its classification was already correct). The whole change is behaviour-only: a minor
  bump at most (ADR 0001 §consequences).
- The clamp rewrites the result *in the conversation*, so the raw text of a pathological result
  never reaches history, a snapshot, or the rendered transcript — the accepted price of a floor
  that must hold for every later reducer; the marker tells the model to re-read the omitted range
  with `start_line`/`end_line`.
- The floor's shared rendering is **line-based** (20 head + 20 tail lines, the same shape and
  marker `tool_result_cap` renders), so it bounds LINES, not History characters: a result of ~40
  very long lines can still exceed the History allocation after clamping, and one the head/tail
  form cannot shrink at all is passed through whole rather than grown. That is the prescribed
  rendering discipline both reducers share, not a defect — the emergency fold and the predictive
  guard remain the backstop for what the clamp cannot bound.
- Recovery is quiet by construction, so operationally a fold shows up only as the context gauge
  dropping on the next `UsageEvent`. The behaviour is pinned by hermetic `internal/agent` tests
  (overflow→success, overflow→overflow, opted-out, tool-continuation, cancel-during-fold); a live
  proof against a real ~32k-window profile is the implementing plan's closing gate.
