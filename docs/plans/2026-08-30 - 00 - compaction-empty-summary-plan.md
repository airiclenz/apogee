# Compaction empty-summary fix — implementation plan

**Goal:** the summarizer call that automatic Compaction runs must never come back empty because
a thinking model spent its whole output cap on reasoning, and a fold that fails must never be
retried with the same inputs. On 2026-08-29/30 a delegate on Qwen3.8-27B looped for ~9 h on
`compaction: apogee: compaction produced an empty summary` — one ~40-minute summary call
(54k-token prompt, 4096 tokens of reasoning, no content) per Turn boundary, seven times.

**Date:** 2026-08-30 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:** `internal/agent/compact.go` (`compactCompleter.Complete` :379-420,
`autoCompact` :106-175, `shouldAutoCompact` :188-218, `emergencyFold` :319-341); `internal/agent/wire.go:16-50`
(`toProviderRequest`, effort applied from `resolvedEffort()`); `internal/title/title.go:42-60` (the
naming call's `EffortOff` precedent and its documented capped-reasoning failure); `internal/agent/loop.go:460-575`
(`cappedReplyErrFmt`, `emptyReplyFault` — ADR 0046 wording precedent); `internal/context/compact.go`
(reducer contract: conv untouched on error); ADR 0018, ADR 0046, ADR 0050; pinned tree `4e184a16`.

**Ratified design calls** (owner, 2026-08-30):
- **Scope:** all four items below are in.
- **Summarizer effort:** the compaction request asks `EffortOff`, overriding the session's effort; `compactMaxTokens` stays 4096.
- **Failure latch:** after an automatic fold fails, the estimate-driven trigger stands down for the rest of the Exchange (a child: for the rest of the delegation; the main agent: re-arms at the next Exchange opening). The emergency fold keeps its single shot; `/compact` ignores the latch.
- **Truncated summary:** finish `length` WITH content is accepted; the summary message ends with a truncation marker line.

**Regression check (2026-08-30, 4e184a16):**
- 1: recast — the `EffortOff` override is gated on a positively detected Kwargs/Reasoning dialect; yields to ADR 0050:19-20 (a caller that asks for nothing changes nothing on the wire — stands, not superseded).
- 3: guard folded — test 3(a) stays under `requestExceedsWindow`'s doubled uncalibrated room so `summaryCalls == 1` counts the estimate-driven trigger alone.
- 4: guard folded — the `compactMaxTokens` comment is reworded; yields to ADR 0046:91-94 (the auxiliaries' exemption is named in the What).
- Second round (2026-08-30, 4e184a16), final:
- 1: guard folded — `newChildAgent` copies the parent's live `effortDialect` onto the child (a rebound parent's delegate speaks the rebound server's dialect, so the override fires for the incident's delegate); Tests reworded to the helpers that exist (the "compacting a conversation" substring, `domain.EffortDialectKwargs` seed) and gain the rebound-child case.
- 3: guard folded — the stand-down suffix is appended only when the failed fold ran with `a.turns.inExchange` true (a child's Turn-boundary fold); a main-agent fold failing at an Exchange opening keeps the plain error text.

**Standing requirements:** `skills: coding-standards`. Every change to `compactCompleter` keeps
`context.Compact`'s guarantee (conv untouched on any error). The provider package holds no domain
import (ADR 0010) — effort crosses at `toProviderRequest`'s boundary only.

**Out of scope:** the step cap and its default (`delegate-max-steps`, shipped 2026-08-26);
mid-Exchange auto-compaction for the MAIN loop (ISSUES.md, parked); making `compactMaxTokens`
a config/profile knob; the llama.cpp prefix-cache miss on the summary prompt; VERSION/CHANGELOG
(entries travel in item sidecars).

## 1. The summarizer asks for no reasoning (`EffortOff`) — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the plan's test (c) names `SwitchUpstream` for the rebind; `UpstreamSpec` carries no `EffortDialect` field and `SwitchUpstream` swaps in a real `provider.Client` (which would drop the test's fake responder), so the test rebinds through `Agent.Rebind`/`RebindSpec` — the one seam that actually moves `a.effortDialect` (rebind.go:266) — which is what the guard's premise ("a rebound parent's delegate speaks the rebound server's dialect") describes.
NOTES (2026-08-30): the three new tests were confirmed non-vacuous by temporarily reverting both production edits — case (a) and case (c) fail without them; case (b) is the byte-identical-wire anchor and passes either way by design.

**What:** Recast at the regression check (2026-08-30). `compactCompleter.Complete` (`internal/agent/compact.go`) builds the provider request
via `c.a.toProviderRequest(req)` and then sets `ThinkingEffort` to `provider.EffortOff` before
streaming — the session's effort override and the profile's level never reach the summary call,
exactly as `internal/title` does for the naming call. The `EffortDialect` stays whatever the
Agent carries (the dialect is the server's, ADR 0060); a server with no dialect emits nothing,
as today. Update the comment block above `compactCompleter` to state the intent and its limit
(the intent lands only on a template that honours it — title.go's wording).

**Regression guard.** The EffortOff override fires ONLY when the Agent's effort dialect is positively detected as one whose "off" rung means off — `a.effortDialect == provider.EffortDialectKwargs || a.effortDialect == provider.EffortDialectReasoning`; on EffortDialectNone, EffortDialectOpenAI and EffortDialectOff the summary request carries `resolvedEffort()` exactly as today (a caller that asked for nothing keeps changing nothing on the wire — ADR 0050:19-20 stands, not superseded). The incident server (llama.cpp) is detected as Kwargs through the /props probe, so it is covered. The comment states the limit; the item's CHANGELOG sidecar entry names it. Tests: one case per branch — Kwargs → EffortOff on the summarizer request; None → the session's resolved effort (or none) on the summarizer request, byte-identical to today

**Regression guard.** `newChildAgent` (`internal/agent/subagent.go`) copies the PARENT's live `effortDialect` field onto the child (today the child takes the startup `cfg.EffortDialect`, so after a rebind a child speaks the wrong server's dialect and the override would not fire for exactly the delegate that hit the incident); `internal/agent/subagent.go` joins Files; Tests gain one case: after the parent is rebound to a Kwargs-dialect spec, a child's summarizer request carries `EffortOff`.

**Files:** `internal/agent/compact.go`, `internal/agent/subagent.go`, `internal/agent/compact_test.go`

**Tests:** in `internal/agent/compact_test.go`, a responder that records the summarizer's
`provider.Request` (identify it by the `"compacting a conversation"` substring in the first
message, as `scriptedCompactResponder` does — `summaryInstruction` is unexported in
`internal/context`), one case per dialect branch: (a) an Agent seeded with
`cfg.EffortDialect = domain.EffortDialectKwargs` (the `agent_test.go:139` shape; assert
`provider.EffortDialectKwargs` on the projected request):
`Agent.Compact` on a foldable conversation sends `ThinkingEffort == provider.EffortOff`; with
`SetEffortOverride(domain.EffortHigh)` set on the Agent the summarizer request STILL carries
`EffortOff` while the next main-turn request carries the override (the existing effort tests'
assertion shape); (b) an Agent with `provider.EffortDialectNone`: the summarizer request carries
the session's `resolvedEffort()` (none when nothing is configured; the override when
`SetEffortOverride` is set) — byte-identical to today's request; (c) a parent seeded with
`domain.EffortDialectNone`, rebound via `SwitchUpstream` to a spec whose `EffortDialect` is
`provider.EffortDialectKwargs`, then a child (`newChildAgent`) folding: the child's summarizer
request carries `EffortOff`.

**Acceptance:** `go build ./... && go test ./internal/agent/ -run 'Compact|Effort'`

Commit: `fix(agent): the compaction summarizer asks for no reasoning, whatever the session's effort`

## 2. A capped, reasoning-only summary faults honestly; inline thinking is stripped

**What:** fix for the 2026-08-29 empty-summary fault (`internal/agent/compact.go`,
`compactCompleter.Complete`): collect `DeltaThinking` text and the `DeltaDone` finish reason
alongside content. After the stream, run the content through `c.a.stripper.Strip` (the same
inline-thinking stripper the loop applies, `loop.go:587`) and join the lifted reasoning to the
Upstream thinking (`joinThinking`). Then, when the visible text is blank (`strings.TrimSpace`)
AND the finish reason is `length`, return an error whose text is EXACTLY
`compaction summary hit its output cap (4096 tokens) with no visible text to show for it%s — the summarizer asked for no reasoning; this server's template did not honour that`
where `%s` is `, after roughly N tokens of reasoning` via `a.tokens.EstimateTokens(len(thinking))`
when thinking is non-empty, else empty — the `emptyReplyFault` shape, ADR 0046. The cap named is
`compactMaxTokens`. Any other blank reply returns `""` so `context.Compact` keeps returning
`errEmptySummary`. Binding standards: one new named constant for the format (`cappedSummaryErrFmt`
next to `cappedReplyErrFmt` in loop.go is acceptable, or in compact.go); no retry, no salvage.

**Files:** `internal/agent/compact.go`, `internal/agent/compact_test.go`

**Tests:** (a) responder yields `DeltaThinking{"…"}` then `DeltaDone{FinishReason:"length"}` with
no content → `Agent.Compact` returns an error containing the exact string above with the
`, after roughly` clause, and `conv.Len()` is unchanged; (b) same with no thinking delta → the
message has no reasoning clause; (c) blank content with finish `stop` → `errEmptySummary` text
unchanged (existing `TestCompactEmptySummaryLeavesConvUntouched` in `internal/context` stays);
(d) on a Profile with `Thinking: domain.ThinkingDelimited, Start "<think>", End "</think>"`
(`profile_test.go:154` shape) a summary reply `"<think>plan</think>Summary text"` folds to a
summary message containing `Summary text` and no `<think>`.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/context/ -run 'Compact|Summary'`

Commit: `fix(agent): a reasoning-only capped summary names the cap and the spend; inline thinking never enters the summary`

## 3. A failed automatic fold stands down for the rest of the Exchange

**What:** fix for the 2026-08-29 retry runaway (a child agent re-ran the identical failing
summary call at every Turn boundary). Add `compactFailed bool` to `Agent` (`internal/agent/agent.go`,
beside `compactSat`, with a comment in the same voice). `autoCompact` sets it on any non-cancel
error (the branch that emits the "compaction" ErrorEvent); `shouldAutoCompact` returns false while
it is set, checked AFTER the enabled/inExchange gates and BEFORE the allocation compare (so the
compare's `compactSat` clearing is unaffected). `turnLifecycle.openExchange` (`internal/agent/turn.go`)
clears it — a new Exchange is a fresh latch, as it is for the step-cap count — so the main agent
re-arms at its next Exchange opening and a child, whose Exchange never re-opens, stands down for
the delegation. `emergencyFold` and `Agent.Compact` (on demand) do NOT consult the latch. The
ErrorEvent text gains the suffix ` — automatic folding stands down for the rest of this exchange`
appended to the error's text (one event, then silence). Update `autoCompact`'s and
`shouldAutoCompact`'s doc comments and the CONTEXT.md **Compaction** paragraph (one sentence).

**Regression guard.** test 3(a) keeps its four over-budget Turns under requestExceedsWindow's doubled uncalibrated room (the existing 25k-char result + "ok" reply shape in autocompact_guard_test.go) so the latch-exempt emergencyFold never adds a summary call and `summaryCalls == 1` is the estimate-driven trigger's count alone; state that in the Tests text.

**Regression guard.** The ErrorEvent suffix ` — automatic folding stands down for the rest of this exchange` is appended ONLY when the failed fold ran with `a.turns.inExchange` true (a child's Turn-boundary fold); a main-agent fold that fails at an Exchange opening keeps the plain error text, because `openExchange` clears the latch immediately after and no stand-down happens there — regression-1.md's G: for item 3 is folded on these terms.

**Files:** `internal/agent/agent.go`, `internal/agent/compact.go`, `internal/agent/turn.go`, `internal/agent/autocompact_guard_test.go`, `CONTEXT.md`

**Tests:** in `internal/agent/autocompact_guard_test.go` with `scriptedCompactResponder` (a
`summaryReply` of `""` makes every fold fail with `errEmptySummary`): (a) a child
(`midExchangeCompaction = true`, the `:366` shape) over-budget for 4 tool-call Turns gets
`summaryCalls == 1` and `countCompactionErrors == 1`, and that one error ends with the suffix
above — the four Turns keep the existing 25k-char result + `"ok"` reply shape so the request stays
under `requestExceedsWindow`'s doubled uncalibrated room (`loop.go:995-1008`, ×2 at `:947`) and the
latch-exempt `emergencyFold` never adds a summary call: `summaryCalls == 1` is the estimate-driven
trigger's count alone; (b) the main agent: a failed fold at one Exchange opening, then a second Submit → a
second summary call is made (re-armed), and neither error's text carries the suffix (the fold
ran with `a.turns.inExchange` false); (c) after a failed auto-fold, an overflow
(`DeltaContextOverflow`, the `overflowResponder` shape in `compact_test.go`) still runs
`emergencyFold` once (its summary call is counted); (d) `Agent.Compact` on demand runs while
the latch is set.

**Acceptance:** `go build ./... && go test ./internal/agent/ -run 'Compact|AutoCompact|Fold'`

Commit: `fix(agent): a failed automatic fold is never retried within the same exchange`

## 4. A summary cut at the output cap is kept, with a truncation marker

**What:** in `compactCompleter.Complete` (`internal/agent/compact.go`), when the visible text is
non-blank AND the finish reason is `length`, return the text with `"\n\n"` and the marker
appended; the marker is a new embedded asset `internal/agent/prompts/summary-truncated.txt`
loaded with the existing `mustPrompt`, text EXACTLY:
`[This summary was cut off at the compaction output cap; the most recent state and next steps may be missing — re-derive them from the tools before continuing.]`
`context.Compact` then folds as usual (the marker rides inside the summary message, after
`summaryMessagePrefix`). Depends on item 2 (the finish reason and visible text it collects).

**Regression guard.** Reword the `compactMaxTokens` comment (`internal/agent/compact.go:14-16`, "bounded but
not truncated") to say a summary cut at the cap is kept with the truncation marker. This item yields to
ADR 0046: its rejection of salvaging a cut-off reply (`0046:91-92`) binds Turns; the summary is an auxiliary
under the `:93-94` exemption — name that exemption in the comment.

**Files:** `internal/agent/compact.go`, `internal/agent/prompts/summary-truncated.txt`, `internal/agent/compact_test.go`

**Tests:** responder yields content `"partial summary"` then `DeltaDone{FinishReason:"length"}` →
after `Agent.Compact` the conversation is prefix + one assistant message whose content starts
with `summaryMessagePrefix` (`internal/context`'s exported behaviour: assert via the message
text starting `Summary of the conversation so far:`) and ends with the exact marker line;
finish `stop` → no marker. `TestEmbeddedPromptsLoadWithoutTrailingNewline`'s agent-side
counterpart (if one exists in `internal/agent`) covers the new asset; otherwise add the
one-line assertion that the loaded marker has no trailing newline.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/context/ -run 'Compact|Prompt'`

Commit: `feat(agent): a summary cut at the output cap is kept and marked as truncated`

**Suggested version bump:** patch (`v0.18.10`) — two user-visible fixes (the 9-hour delegate
runaway and the honest fault text) and one small behaviour addition; the owner decides.
