---
title: ADR 0017 decision
---

**1. The boundary is derived, never cached — one derivation, homed in `internal/domain`.**
The Exchange becomes a domain working value: an `ExchangeView` value plus a
`CurrentExchange(...)` constructor over a minimal unexported `Len()/At(i)` read surface,
satisfied by both `Conversation` (the engine's committed history) and the unexported
`conversationView` (the hooks' request view) — so the loop and the Mechanisms consume the
same boundary definition: the index of the last `RoleUser` message, the current Exchange being
the messages strictly after it. Per ADR 0010's lowest-layer rule the derivation is pure logic
on domain types, so `internal/domain` is its home; `InjectContext` and `LastUser` route
through it (or its shared core) with their public behaviour unchanged.

> **Note (2026-10-29, knowledge inventory).** Since 2026-07-27 (Interjection, commit 15971da7) the
> opening is the last `RoleUser` message that is **not** an Interjection —
> `domain.lastExchangeOpening` (`internal/domain/exchange.go`), which `CurrentExchange` and
> `Request.InjectContext` route through, so a mid-Exchange user message moves no boundary. The
> hooks' `LastUser` deliberately stays the plain role scan over the shared `lastMatchIndex` core: it
> reports the most recent user message, interjected or not.

**2. The engine derives the boundary; the cached field and its repair math go.** With one
derivation available, `a.exchangeStart` is redundant while an Exchange is open:
`AbortExchange`'s rollback target and the S2 repair after a mid-Exchange history rewrite are
both subsumed by re-deriving from the post-rewrite conversation — correct by construction,
no arithmetic to keep honest. `a.inExchange` **stays**: open-vs-closed is genuine state, not
derivable (a closed Exchange still has a last user message), and it is serialized. The
snapshot schema follows: `ExchangeStart` stops being written and is **ignored on read** — the
tolerant decode keeps old snapshots resumable, preserving ADR 0007's quiescent-boundary
snapshot/resume contract; `InExchange` continues to round-trip. This rests on one
precondition: **no history rewrite may drop the open Exchange's opening user message**
(`truncate_history` keeps the tail; auto-compaction is Exchange-boundary-only). Should a
future rewrite be able to violate it, the fallback is to keep the cached field and shrink
this decision to routing its readers through one helper.

**Realisation (2026-07-19) — the fallback was taken.** The deepening plan's item-4
verification found the precondition violated by the EXISTING `truncate_history`: its keep-tail
cut counts assistant boundaries, so an open Exchange already holding `keepLastTurns` (4) or
more assistant messages has its opening user message dropped and replaced by the user-role gap
note — the last-`RoleUser` derivation then anchors at the note, and an abort re-derived from
it would wrongly drop the note (pinned by
`TestExchangeStartRepairedAfterMidExchangeTruncation`, `internal/agent`). Per this section's
own fallback: the cached `exchangeStart` and its S2 repair stay authoritative for the rollback
boundary, its readers route through the single `Agent.exchangeBoundary()` helper, and the
snapshot `ExchangeStart` keeps being written and read — it is load-bearing, not legacy. §1
(the domain derivation for hooks and Mechanisms) and §3 (`closeExchange`) stand unchanged;
swapping the cache for the derivation would need a rewrite contract that preserves the opening
user message, which reopens this ADR.

**3. Exchange end has one engine-side owner.** The fixes plan's F6 placed deferral clearing at
three Exchange-end sites. Those three ends concentrate into one private `closeExchange` on the
Agent — flip `inExchange`, clear the deferred queue, one docstring owning the "a deferral dies
with its Exchange" invariant — called from `completeTurn`'s `StatusExchangeComplete` branch,
`abandonTurn`, and `AbortExchange`. `cancelTurn` stays distinct by design: the Exchange
remains open there, so it truncates-then-restores the deferred queue exactly as F6 specifies.
Pure concentration — same observable behaviour, one place to read it.

> **Note (2026-10-29, knowledge inventory).** The code has since moved; the invariant has not.
> `closeExchange` lives on the unexported `turnLifecycle` (`internal/agent/turn.go`), not on the
> Agent, and the three exit helpers `completeTurn` / `abandonTurn` / `cancelTurn` became one table,
> `turnLifecycle.end` (2026-07-24, commit 3f1808b6). Its callers are `end`'s `endExchangeDone`,
> `endAbandoned` and `endStepCapped` rows plus `abort` (`AbortExchange`) and `settle`
> (`SettleExchange`); `cancelTurn`'s truncate-then-restore is the `endCancelled` row, and
> `endSettled` likewise leaves the Exchange open.

> **Amended 2026-09-19 (`apogee-2un`, Stage A).** The caller list gains a fourth end:
> `turnLifecycle.settle`, the engine half of `Agent.SettleExchange`. Where `AbortExchange` rolls
> the whole Exchange back to `exchangeStart` (the explicit throw-away — `/clear` on an interrupted
> session), `SettleExchange` closes a cancelled Exchange *keeping* the Turns that finished before
> the stop: it marks the cut as an ephemeral `[engine — cancelled]` note on the Exchange's last
> tool result, then calls the same `closeExchange` — so the "a deferral dies with its Exchange"
> invariant holds for a settled Exchange exactly as for an aborted one. An Exchange with no
> finished Turn has no tool result to carry the note and falls through to `abort`. The TUI's
> cancel and loop-error folds and a fresh message on a restored interrupted session settle; only
> `/clear` still aborts. Plan `2026-09-19 - 01`.

**4. `ExchangeView` is not exported at the root.** Its consumers are internal — Mechanisms are
curated (ADR 0002), not an
open extension point that external code builds against. The public
`LoopView` / `ConversationView` interfaces gain **no** methods: they are unsealed and
externally implementable (the `LoopView` docstring itself anticipates test fakes), so an added
method is a breaking change; the domain constructor over the existing read surface is the
semver-safe shape. Export becomes a later, deliberate minor bump when an external consumer
exists — not before.
