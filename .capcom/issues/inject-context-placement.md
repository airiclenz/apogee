---
title: "`Request.InjectContext` placement — a `domain` data type encodes chat-template role-safety policy"
status: parked
type: decision
priority: 4
aliases: [apogee-p5j]
created: 2026-09-03T18:40:20Z
---

**Status:** parked 2026-07-26 — carried out of the 2026-07-24 architecture-deepening review so that
review's ledger could close honestly. It is the review's own verdict that parks it, and the verdict
stands as written: ***Speculative* — it reopens an ADR-0010 line; flagged, NOT recommended without a
grill; the current placement is defensible.** So this is **not a code TODO**: no code change, no ADR
amendment, and no "obvious" move happens here until an owner grill (`grill-with-docs`) settles the
question. It is not a live gap either — no wrong output, no bug, no broken invariant; the ladder
below is doing its job. Full card:
[`docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.md`](docs/reviews/archived/2026-07-24%20-%2000%20-%20architecture-deepening-review.md)
(the smaller-deepenings list, last entry).

**The question, in one line.** `Request.InjectContext` — a method on a `domain` **data type** —
encodes *chat-template role-safety policy*, while the engine / `internal/context` layer is where
role-alternation is otherwise owned. Which layer should own the placement decision?

**What the code does today** (`internal/domain/hooks.go:504–529`) — recorded so the grill starts from
behaviour rather than re-reading it. `InjectContext(text)` picks the landing spot with a three-branch
ladder:

1. history ends in a **tool result** → append to the system prompt instead (a user message after a
   tool result breaks strict chat templates);
2. history ends in an **assistant** message → append at the end (the retry-exchange shape: the
   correction answers the superseded assistant message it follows, R1);
3. otherwise → insert **before** the last Exchange's opening (`lastExchangeOpening`,
   interjection-aware — drifted since this entry was written, which recorded "before the last user
   message"; re-verified 2026-08-13) — plus the F2 boundary maintenance, since an
   insert below the frozen `committedLen` shifts every committed message right by one and the
   boundary must advance with it;

with no user message present at all, it appends at the end. Sibling for contrast: `AppendToSystem`
(same file, `:481`) is the marker-idempotent system-prompt inject; `InjectContext` carries **no**
marker of its own.

**The tension.** Strict-template alternation is otherwise the engine/context layer's business:
`internal/context/compact.go` (`:52`, `:105`, `:113`) shapes the folded history and the summarizer
call to keep clean alternation, and `internal/agent/compact.go:193` asserts alternation holds after
`Conversation.Replace`. Chat-template policy therefore has two homes — a `domain` value type and the
engine's reducers — which is the whole of the observation.

**What it reopens, i.e. why this is not a free move.**

- **An ADR-0010 public-surface line.** ADR 0010's canonical placement rule puts every public type in
  `internal/domain` with the root re-exporting it — `apogee.go:416` is `type Request = domain.Request`
  — and ADR 0010's *Stability* consequence states the public surface **is** the set of root aliases
  (ADR 0001 §18). `InjectContext` is a method on that aliased type and is a documented member of the
  hook mutation API (`docs/design/archived/hook-mutation-api.md` §3, the `Request` pre-request surface, and
  its §7 traceability table — a P0.1 draft, so read its line references as stale). Moving it
  is a **breaking public-surface change**, not a refactor — and ADR 0010's own lowest-layer rule
  ("a type lives at the lowest layer that can define it without importing upward", and pure logic
  intrinsic to those types lives with them) is simultaneously the strongest argument that the current
  home is *correct*, not accidental.
- **An ADR-0017 pairing.** ADR 0017 §1 explicitly routes `InjectContext` (and
  `conversationView.LastUser`) through the single Exchange-boundary derivation "with their public
  behaviour unchanged" — `internal/domain/exchange.go`'s `lastRoleIndex` is THE boundary core. And
  `exchange.go`'s header states the Exchange boundary is stable *because* `InjectContext` places
  injections before the last user message or in the system message, **never after it**. The placement
  policy and the boundary derivation are load-bearing for each other; any move must say what happens
  to that pairing.

**Blast radius, so it is known before the grill, not during.** Two non-test callers:
`internal/agent/loop.go:497` (the retry correction) and `:922` (the deferred-correction drain).
Nothing else calls it, and nothing else keys idempotency or boundary logic on the roles
`InjectContext` writes — the ladder's behaviour is depended on by the loop alone.

**What a grill has to settle (the branch points, so nothing is re-derived):**

- Is the three-branch ladder **policy** (belongs with whoever owns chat-template shape) or **pure
  logic intrinsic to the `Request` value** (ADR 0010's lowest-layer rule — the argument for today's
  home)? Everything else follows from this one.
- If it is policy: what is the public replacement, given `Request` is a root alias and the hook API
  documents the method? A seam the engine injects, versus keeping the method and moving only the
  *decision* behind it, are different-sized breaks.
- What happens to the ADR-0017 pairing — does the "never after the last user message" property stay
  guaranteed by construction, or does it degrade to a convention the boundary derivation merely hopes
  for?
- Is the honest outcome instead to **close** the question — an ADR 0010 (and/or 0017) note naming
  `domain` the deliberate owner of role-safety placement, so it stops being re-flagged at every
  architecture pass? A review that says "defensible" twice is evidence for this branch.
