---
Status: accepted
---

# The Exchange is a derived domain working value

## Context

The Exchange is a first-class CONTEXT.md term — one user input through to the final no-tool
response, the user-facing unit of a conversation — with no module. Its boundary is re-derived
ad-hoc in seven places across three packages (sites as of the 2026-07-06 review; re-locate by
symbol):

1. `internal/agent/loop.go` — `step()`'s Exchange opening caches `a.exchangeStart =
   a.conv.Len()` immediately before appending the opening user message;
2. `internal/agent/loop.go` — the S2 repair after a mid-Exchange history rewrite re-anchors the
   cache with `min(max(exchangeStart-dropped, PrefixEnd()+1), Len())` arithmetic;
3. `internal/agent/agent.go` — `AbortExchange` rolls back with `DropRange(a.exchangeStart,
   Len())`;
4. `internal/agent/compact.go` — the auto-compaction gate's Exchange-boundary-only check;
5. `internal/domain/hooks.go` — `InjectContext` places injections via `lastIndex(msgs,
   RoleUser)`;
6. `internal/domain/hookview.go` — `conversationView.LastUser` spells the same scan again;
7. `internal/mechanisms/guided_decomposition.go` — the current-Exchange scans the fixes plan's
   F1/F3 added ("the messages after the last `RoleUser` message", spelled per-Mechanism).

Two facts make one shared derivation possible. First, the mid-Exchange conversation shape is
`[…, user ask, assistant(calls), tool results, assistant, …]`, and every request-scoped
injection (`InjectContext`) lands *before* the last user message or in the system message —
so **"the current Exchange" = the messages strictly after the last `RoleUser` message in the
view**, stable across injections (the fixes plan's shared F-context). Second, the engine's
cached `a.exchangeStart` is the same number by construction: it is set to `conv.Len()`
immediately before the opening user message is appended.

The cost of not having the module is on the record. The post-v1.3.0 review's three High
findings — the F1 gate re-fire loop (request-scoped markers standing in for Exchange-scoped
state), the F3 enumeration-anchor shadowing (a scan not scoped to the current Exchange), and
the F6 cross-Exchange deferral leak (Exchange-lifetime state held at Session scope) — share
one root cause: Exchange-scoped state and boundaries with no Exchange module to own them. The
fixes plan ([`docs/plans/archived/post-v1.3.0-review-fixes-plan.md`](../plans/archived/post-v1.3.0-review-fixes-plan.md),
ratified 2026-07-07) patched each site tactically and is this decision's behaviour contract.

The 2026-07-06 architecture review (`docs/reviews/architecture-review-20260706-205911.html`)
then found the gap three times independently: the engine explorer found the seven ad-hoc
boundary computations, the mechanisms explorer found Exchange-scoped state faked with
request-scoped markers, and the 07-06 code review traced all three of its High findings to
exactly this gap. Its "Give the Exchange a home" candidate (Strong) is what this ADR ratifies,
as concentrated in the deepening plan's D1–D3
(`docs/plans/architecture-deepening-plan.md`).

## Decision

**1. The boundary is derived, never cached — one derivation, homed in `internal/domain`.**
The Exchange becomes a domain working value: an `ExchangeView` value plus a
`CurrentExchange(...)` constructor over a minimal unexported `Len()/At(i)` read surface,
satisfied by both `Conversation` (the engine's committed history) and the unexported
`conversationView` (the hooks' request view) — so the loop and the Mechanisms consume the
same boundary definition: the index of the last `RoleUser` message, the current Exchange being
the messages strictly after it. Per ADR 0010's lowest-layer rule the derivation is pure logic
on domain types, so `internal/domain` is its home; `InjectContext` and `LastUser` route
through it (or its shared core) with their public behaviour unchanged.

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

**4. `ExchangeView` is not exported at the root.** Its consumers are internal — Mechanisms are
curated ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)), not an
open extension point that external code builds against. The public
`LoopView` / `ConversationView` interfaces gain **no** methods: they are unsealed and
externally implementable (the `LoopView` docstring itself anticipates test fakes), so an added
method is a breaking change; the domain constructor over the existing read surface is the
semver-safe shape. Export becomes a later, deliberate minor bump when an external consumer
exists — not before.

## Consequences

- Hooks and engine share one boundary definition; the two derivations cannot drift, and
  boundary bugs concentrate in one module that is testable directly at the domain level
  rather than only through loop runs.
- The [ADR 0014](0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md)
  "re-derive from honest history" posture (its Realisation, as amended by the fixes plan's
  committed-evidence gating) is implemented in one place, not per-Mechanism.
- ~~The S2 repair arithmetic and the snapshot `ExchangeStart` plumbing disappear from
  `internal/agent`~~ — superseded by the §2 realisation note (2026-07-19): the fallback was
  taken, so the repair and the round-tripping `ExchangeStart` stay, concentrated behind
  `Agent.exchangeBoundary()`. Resumability
  ([ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)) and the layering
  ([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)) are unchanged — the
  value lives at the lowest layer that can define it.
- The §2 precondition's failure is a named, test-pinned fact the engine documents at the
  repair arithmetic and at `exchangeBoundary()`; a rewrite contract that preserves the open
  Exchange's opening user message would reopen this ADR before the cache could go.
- Implementation lands via `docs/plans/architecture-deepening-plan.md` items 3–4; the fixes
  plan's tests are the behaviour contract that proves the refactor preserved semantics.
