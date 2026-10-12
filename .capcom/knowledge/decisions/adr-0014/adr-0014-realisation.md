---
title: "ADR 0014 realisation of 2026-07-05: the decisions locked at implementation, and where the letter was refined"
---

`guided_decomposition` shipped item-by-item behind the Mechanism catalogue (default-off, D1) on
the same day this ADR was ratified. The build refined the Decision's letter in a handful of
places without touching its semantics; recorded here so the ADR stays the ground truth.

- **Queue delivery is one re-derived deferred directive per Turn** — §3's "cursor over honest
  history" made literal. The remaining-items state carries **no** mechanism-struct field: each
  post-response Turn re-derives the remainder from honest history — the model's own enumeration
  message and the `sub_agent` **calls** in the conversation (never the child *results*, so a
  report capped by `tool_result_cap` leaves the cursor exact) — and re-defers a single directive
  string over the existing deferred FIFO (`conversationJSON.Deferred`). Snapshot/resume-safety and
  suppression-clean abandonment both fall out for free; on suppression at most one already-queued
  directive still drains via the loop's shared defer plumbing (loop plumbing, not a Mechanism fire
  — documented, not fought).
- **The stacking relations landed as declared:** `IncompatibleWith: [decompose]` (two Mechanisms
  steering the same symptom through different means must not co-fire) and `Requires:
  [tool_result_cap]` validated at the **registry** level (`MechanismRegistry.ValidateRequirements`
  → `ErrMissingRequirement`), so a library embedder gets the same enable-time gate as the CLI, not
  a cmd-only check. Validation order is incompatibilities-before-requirements.
- **The enumeration text is never trimmed** (§2/§3 honesty): the intercept only *appends* the
  synthesized `sub_agent` call via `Response.AppendToolCall`; `SetText` is not used, so the list
  stays verbatim in history.
- **Bounds (tuning surface, the bench's to move):** the steer asks for at most **7** subtasks
  (`guidedDecompositionMaxSubtasks`); the intercept declines the WHOLE enumeration — a benign
  no-op, never a truncation — on a parsed list of fewer than **2** or more than **12** items
  (`guidedDecompositionMinSubtasks` / `guidedDecompositionMaxAcceptedSubtasks`, the accept window
  deliberately wider than the steer's ask so a slight overshoot still fans out).

Authorized implementation deviations (each recorded in the plan's per-item NOTES, mirrored here):

- **Depth reaches hooks through a `Request.SetDepth` engine seam,** not a new `NewRequest`
  parameter — a parameter would have forced ~18 test-caller edits outside the item's confined
  diff. `SetDepth` is loop setup and does **not** bump the revision. `hookrun` already composed an
  in-place response mutation with a returned `ActionDefer` correctly, so §2's mutate-and-defer
  needed no hookrun change.
- **The no-double-steer gate uses TWO markers, not one:** `guidedDecompositionSteerMarker` on the
  enumeration steer this half injects, and `guidedDecompositionDirectiveMarker` on the
  remaining-items directive (a forward contract — `buildRequest` drains and injects the directive
  *before* the pre-request hooks run, so an in-flight fan-out is visible to the gate, which stays
  quiet). Signal-B history-token estimation counts message content only, not tool-call arguments
  (the existing chars→token idiom).
- **The follow-through is additionally guarded on the directive marker** being present before it
  re-derives — a refinement of §2's case-2 text, consistent with the cursor-over-honest-history
  model: a spurious model `sub_agent` call with no fan-out in flight is an explicit no-op rather
  than relying on an empty-remainder derivation.
- **The config template's commented `mechanisms:` example carries the STACK** (both
  `guided_decomposition: true` AND `tool_result_cap: true`), never the lone key — a lone
  `guided_decomposition` would be an erroring half-stack if uncommented (`ErrMissingRequirement`),
  so the two-line example is valid-if-uncommented.

These refine the letter; the Decision is unchanged. The Mechanism ships default-off — the
ADR 0009 non-inferiority gate for the `guided_decomposition + tool_result_cap` stack, and any
constant tuning, remain the bench's call.

### Addendum (2026-07-06) — substrate hardening: committed-evidence, majority-marked, delegation-anchored, consume-once, line-anchored

A four-lens review of the 2026-07-05 build (`docs/code-review-2026-07-06.md`) found the
idempotency and cursor machinery leaned on **request-scoped markers** in ways §2/§3/§5 do not
actually license. The 2026-07-06 fixes (plan items 1–6) close five gaps without touching the
Decision — they make the substrate honest about which history a decision reads:

- **What the marker-only idempotency missed.** The steer rides an `InjectContext` copy and the
  directive rides the deferred drain, so **both markers vanish from the next request** the moment
  nothing is re-deferred (the synthesis Turn). Because signal B still reads oversized there, the
  gate re-steered and looped the decomposition. Alongside it: the remainder cursor's **first-match
  lenient anchor** could latch a prior-Exchange answer or a compaction summary; **prefix-match
  consumption** let a longer nested item absorb a shorter item's dispatch (and a duplicate be
  double-counted); **one off-script tool call** mid-fan-out drained the directive and dropped the
  queue; the parser's **plain-line leniency** accepted prose (a clarifying question, a refusal) as
  an enumeration; and the marker scan was a **bare substring match over every role**, so an
  assistant echo or an `@file` line could masquerade as an injection.
- **§5's gate is now once-per-Exchange on committed evidence (F1).** The pre-request gate stays
  quiet for the rest of an Exchange once any assistant message after the last user ask carries a
  `sub_agent` call — read from committed history, not the request-scoped markers, which the
  synthesis Turn no longer carries. The markers remain only as the same-request double-steer guard.
- **§2 accepts an enumeration only on a majority of explicit markers (F4).** A steered reply is
  treated as an enumeration only when the parsed list is in-bounds (2..12) **and** a strict majority
  of its lines carried an explicit ordered/bullet marker. A compliant numbered list passes; prose is
  declined whole. This is the review's escalation of §2's plain-line leniency — the enabling root
  cause of the hijacks — from "warrants revisiting" to a rule.
- **§3's cursor anchors on the delegation-bearing enumeration in the current Exchange (F3), and
  consumes dispatched tasks by exact match, once each (item 3).** The remainder derives only from
  the messages after the last user ask, anchoring on the first assistant message that **both**
  parses in-bounds **and** carries a `sub_agent` call — the pair that uniquely identifies the real
  enumeration. A dispatched task removes an item only when it equals the item or the item plus the
  appended report-hygiene ask, and each dispatch consumes at most one occurrence.
- **§2's case-2 re-defers across an off-script tool Turn (F2).** When a directive is steering and the
  model calls at least one tool that is not `sub_agent`, the shrunken directive is re-deferred
  (the off-script call consumes no item, so the remainder is intact) rather than draining away. A
  no-tool final answer still ends the Exchange and is never re-deferred (item 7 expires any residue
  at the Exchange boundary).
- **Marker detection is line-anchored and role-scoped (F5).** A marker counts only in a `RoleUser`
  or `RoleSystem` message and only where it starts a line — the exact shape the loop's
  `InjectContext` writes an injection in (a whole message, or appended to the system prompt after a
  `"\n\n"` separator). An assistant echo, a tool result, or an `@file` line carrying the phrase
  mid-line no longer matches. The marker **strings** are unchanged (the loop-level tests' wire
  contract).

The Decision (§1–§5) is unchanged; these are Realisation refinements that make the honest-history
reading the Decision already assumed actually hold across a full Exchange.

- **2026-07-06 (post-v1.3.0 review-fixes item 8, F7):** `guided_decomposition` is additionally
  declared `IncompatibleWith: [truncate_history]` — a mid-Exchange truncation longer than its keep
  window can drop the enumeration message the cursor re-derives the remainder from, destroying the
  fan-out mid-flight. The one-sided declaration on the `guided_decomposition` descriptor suffices
  (`detectIncompatibility` is symmetric in effect), so co-enabling the two is refused at startup with
  `ErrIncompatibleMechanisms`.
