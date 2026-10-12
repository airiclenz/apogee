---
title: ADR 0017 context
---

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
fixes plan ([`docs/plans/archived/post-v1.3.0-review-fixes-plan.md`](../../../../docs/plans/archived/post-v1.3.0-review-fixes-plan.md),
ratified 2026-07-07) patched each site tactically and is this decision's behaviour contract.

The 2026-07-06 architecture review (`docs/reviews/architecture-review-20260706-205911.html`)
then found the gap three times independently: the engine explorer found the seven ad-hoc
boundary computations, the mechanisms explorer found Exchange-scoped state faked with
request-scoped markers, and the 07-06 code review traced all three of its High findings to
exactly this gap. Its "Give the Exchange a home" candidate (Strong) is what this ADR ratifies,
as concentrated in the deepening plan's D1–D3
(`docs/plans/architecture-deepening-plan.md`).
