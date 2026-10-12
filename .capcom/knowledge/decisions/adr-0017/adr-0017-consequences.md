---
title: ADR 0017 consequences
---

- Hooks and engine share one boundary definition; the two derivations cannot drift, and
  boundary bugs concentrate in one module that is testable directly at the domain level
  rather than only through loop runs.
- The ADR 0014
  "re-derive from honest history" posture (its Realisation, as amended by the fixes plan's
  committed-evidence gating) is implemented in one place, not per-Mechanism.
- ~~The S2 repair arithmetic and the snapshot `ExchangeStart` plumbing disappear from
  `internal/agent`~~ — superseded by the §2 realisation note (2026-07-19): the fallback was
  taken, so the repair and the round-tripping `ExchangeStart` stay, concentrated behind
  `Agent.exchangeBoundary()`. Resumability
  (ADR 0007) and the layering
  (ADR 0010) are unchanged — the
  value lives at the lowest layer that can define it.
- The §2 precondition's failure is a named, test-pinned fact the engine documents at the
  repair arithmetic and at `exchangeBoundary()`; a rewrite contract that preserves the open
  Exchange's opening user message would reopen this ADR before the cache could go.
- Implementation lands via `docs/plans/architecture-deepening-plan.md` items 3–4; the fixes
  plan's tests are the behaviour contract that proves the refactor preserved semantics.
