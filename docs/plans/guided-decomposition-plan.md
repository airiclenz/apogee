# Plan — Guided decomposition (`guided_decomposition`, ADR 0014)

**Date:** 2026-07-05
**Status:** ready to implement — not started.
**Track:** post-`v1.0.0` Mechanism-catalogue extension (the same track as the phase-4 waves).
Purely **additive**; no freeze break. Default **off** (D1) — this plan ends with the Mechanism
implemented, tested, and enable-able; flipping it on is the bench's call, not this plan's.

**Authoritative sources (precedence):** if any item text disagrees with
[ADR 0014](../adr/0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md)
or the `CONTEXT.md` entries it cites (Guided decomposition, Mechanism descriptor,
Post-response decision), **the ADR and CONTEXT.md win** — they are the ratified ground truth
from the 2026-07-05 grill. The one place this plan *refines* the ADR's letter (the delivery
plumbing of the remaining-items directive, item 4) is recorded below as a locked decision and
stays within the ADR's stated semantics (a Deferred Response Action whose ground truth is
honest history).

## Where things stand (grounded, verified 2026-07-05)

- **Descriptor** (`internal/domain/mechanism.go:118-124`): `MechanismDescriptor{ID,
  Capability, Suppression, IncompatibleWith}`. **No `Requires` field yet** — CONTEXT.md
  already documents it (grill 2026-07-05); the code catches up in item 1. Incompatibility is
  validated by `Registry.ValidateIncompatibilities()` from `newAgent`
  (`internal/agent/loop.go:54-58`); config keys are validated (enabled AND disabled) against
  `mechanisms.KnownIDs()` in `buildMechanismRegistry` (`cmd/apogee/wire.go:263-305`); the
  `mechanisms:` block is file-only.
- **Hooks** (`internal/domain/hooks.go`): `PreRequestHook.PreRequest(ctx, *Request) error`
  (mutators: `AppendToSystem`, `InjectContext`, `SetTools`, `SetMessageContent`);
  `PostResponseHook.PostResponse(ctx, *Response) (PostResponseDecision, error)` with
  `ActionRetry`/`ActionIntercept`/`ActionDefer` + `Inject`. Intercept mutations apply
  in-place; **`Response` today has only `SetText` and `SetToolCallArguments`** — there is no
  way to *add* a tool call to a response that has none. Item 2 closes that.
- **LoopView** (`internal/domain/hooks.go:224-237`): `Conversation()`, `Tools()`, `Budget()`
  (full struct incl. `History`, `FileContext`, `CharsPerToken`), `Turn()`, `Fired(id)`.
  **`Depth` is NOT exposed to hooks** (it is a private `Agent` field, `agent.go:79`). Item 2
  closes that too.
- **Deferred FIFO** (`internal/domain/hooks.go:604-611, 716-737`): `Conversation.Defer(s)` /
  `TakeDeferred()` — **drains ALL pending entries into the next request** and is serialized
  through snapshot (`conversationJSON.Deferred`). Consequence: the remaining-items queue
  cannot ride as N entries consumed one-per-Turn; it rides as **one directive string per
  Turn, re-derived and re-deferred each post-response** (locked decision 1 below).
- **Sub-agent** (`internal/tools/sub_agent.go`): `SubAgentToolName = "sub_agent"`, args
  schema `{task: string}` (required). Dispatch recognizes it as the recursion point
  (`internal/agent/subagent.go`, ADR 0013); `toolMenu()` (`loop.go:704-708`) withholds it at
  the depth bound. `ToolCall{ID, Tool, Arguments json.RawMessage}`; loop-synthesized call IDs
  follow the `text_call_<turn>` style (`loop.go:348`).
- **Self-regulation is free**: `hookrun.go` records acted fires (revision bump or non-zero
  decision, R4) and `selfreg.go` runs judgment/strikes/suppression with no per-Mechanism
  code. Suppression is checked at every hook dispatch (`hookrun.go:51,91,135,206,247`).
- **`decompose` already exists** (`internal/mechanisms/decompose.go`, phase-4 item 12): a
  *prompt-level* focus/step-directive nudge family ported from apogee-sim — same symptom
  (task too big), different means (wording, not delegation). It is NOT Guided decomposition
  and must not co-fire with it (locked decision 2).
- **Mechanism test conventions**: `shaperRequest()` helper, descriptor/ordering/catalogue
  table tests, revision-based act detection (`internal/mechanisms/decompose_test.go`,
  `toolfilter_test.go`); declared pre-request ordering edges must be seeded in
  `TestPreRequestOrderingSeeds`.

### Decisions locked with the owner (2026-07-05)

1. **Queue delivery = one re-derived directive per Turn over the existing deferred FIFO.**
   The remaining-items state is *derived from honest history each post-response* (the
   enumeration is the model's own visible message; dispatched `sub_agent` calls are in the
   conversation) and delivered as a single `Conversation.Defer` string for the next request.
   This is the ADR's "Deferred Response Action as a cursor over honest history" made literal:
   no mechanism-struct state (snapshot/resume-safe for free — the pending directive rides
   `conversationJSON.Deferred`), and suppression abandons cleanly (at most one already-queued
   directive still delivers via the loop's existing drain semantics — that trailing inject is
   loop plumbing shared by every defer user, not a Mechanism fire; document, don't fight it).
2. **`IncompatibleWith: [decompose]`.** Two Mechanisms steering the same symptom through
   different means must not stack (the phase-4 `stall_nudge ⊥ list_nudge` precedent:
   IncompatibleWith is the startup gate that replaces runtime cross-checks).
3. **`Requires: [tool_result_cap]`** per ADR 0014 §4 — validated at the registry level (not
   cmd-only) so library embedders get the same guarantee.
4. **The enumeration text is never trimmed.** The intercept only APPENDS the synthesized
   tool call; `SetText` is not used — the list stays verbatim in history (ADR §2/§3 honesty).
5. **Bounds:** the steer asks for at most **7** subtasks; the intercept declines (benign
   no-op, no truncation) on a parsed list of fewer than 2 or more than **12** items.
   Constants in the mechanism file; tuning is the bench's job.

---

## 1. Domain: the `Requires` stacking relation + registry validation — ✅ DONE (2026-07-05)

**What:** add `Requires []MechanismID` to `MechanismDescriptor`
(`internal/domain/mechanism.go:118-124`) and a `Registry.ValidateRequirements()` that mirrors
`ValidateIncompatibilities()`: every enabled Mechanism's `Requires` must all be present in
the registry, else a config error naming both IDs and the reason ("X requires Y — enable
both or neither; they are benched as a stack"). Call it from `newAgent` alongside the
incompatibility check (`internal/agent/loop.go:54-58`) so CLI and library embedders share
the gate. `buildMechanismRegistry` needs no change (the registry check surfaces at the same
startup boundary). Semantics are **enable-time only** (ADR 0014 §4): live suppression of a
required peer mid-Session is accepted and NOT re-checked.

**Authoritative source:** ADR 0014 §4; CONTEXT.md "Mechanism descriptor" (already documents
`Requires` — no glossary edit in this item).

**Tests:** table tests with `fakeMechanism` (the `catalogue_test.go:90-97` pattern): enabled
A requiring absent B → error; A+B enabled → pass; empty `Requires` → pass; requirement chain
(A→B→C) validates transitively by iteration, not recursion; error text names both IDs.
Existing descriptor tests stay green (zero-value `Requires` is nil).

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/agent` +
docs/CHANGELOG. Commit:
`feat(domain): the Requires stacking relation on the mechanism descriptor`.

---

## 2. Domain: hook-visible seams — `LoopView.Depth()` and `Response.AppendToolCall` — ✅ DONE (2026-07-05)

NOTES (2026-07-05): Depth is threaded via a new engine-seam `Request.SetDepth(int)` (called
from `buildRequest` and the `loopView` helper) rather than as a new `NewRequest` parameter —
adding a parameter would have forced edits to ~18 `domain.NewRequest` callers in
`internal/mechanisms`/`internal/domain` tests, blowing the item's "diff confined to
internal/domain, internal/agent" boundary. `SetDepth` is loop setup, so it does NOT bump the
revision. hookrun already composes an in-place mutation with a returned `ActionDefer` correctly
(`applyPostResponse` applies the mutation then routes the defer) — no hookrun change was needed.
NOTES (2026-07-05): adding `Depth()` to the `LoopView` interface required the mandatory
compile fix of one test fake outside the confined-diff set — `fakeView` in
`internal/mechanisms/robustness_test.go` gained `Depth() int` (per the item body's "update
every test fake implementing LoopView").

**What:** the two seams ADR 0014's gate and intercept need that hooks cannot reach today.
(a) **`Depth() int` on `LoopView`** (`internal/domain/hooks.go:224-237`), implemented by the
agent's `loopView` from `Agent.depth` (`agent.go:79`), threaded wherever the view is built
(`loop.go:566`); update every test fake implementing `LoopView`. Top-level agents report 0.
(b) **`Response.AppendToolCall(call ToolCall)`** — appends a synthesized call to the parsed
response, bumps the revision (acted-fire detection, R4), and the loop must then treat it
exactly like a model-emitted call: recorded on the assistant message in the conversation and
dispatched with a full per-call Resolution (the ADR 0013 recursion point untouched). Also pin
the decision-composition semantic the intercept relies on: **an in-place response mutation
combined with a returned `ActionDefer`** must both take effect (mutation already applied;
`hookrun.go:174-194` routes the defer) — if the current routing precludes it, fix hookrun in
this item, not item 4.

**Authoritative source:** ADR 0014 §2 (dispatch through the recursion point; honest history);
ADR 0013 §1 (per-call Resolution).

**Tests:** domain: `AppendToolCall` bumps revision, appended call visible via `ToolCalls()`;
`Depth()` on fakes. Agent-level: a stub post-response hook appends a `sub_agent`-shaped call
→ the loop dispatches it, the assistant message in the conversation carries it, and a
Resolution is computed for it (mirror `resolution_test.go` style); a stub hook that mutates
AND returns `ActionDefer` → both the mutation and the deferred inject land; sub-agent events
still nest at `Depth == 1` (existing tests stay green).

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/agent` +
docs/CHANGELOG. Commit:
`feat(domain): expose loop depth to hooks and allow post-response tool-call synthesis`.

---

## 3. Mechanism: skeleton, gate, and the enumeration steer (pre-request half) — ✅ DONE (2026-07-05)

NOTES (2026-07-05): the "no double-steer while a fan-out is in flight" gate (item-3 requirement)
is implemented with TWO fixed markers, not one: `guidedDecompositionSteerMarker` (in the
enumeration steer this half injects) and `guidedDecompositionDirectiveMarker` (a forward
contract — item 4 MUST embed this constant in its remaining-items deferred directive so the gate
recognises an in-flight fan-out and stays quiet). Both constants live in
`guided_decomposition.go`; the gate scans the conversation for either. Signal-B history-token
estimation counts `msg.Content` only (the existing `library.go` chars→token idiom), not ToolCall
arguments. The steer text is a package `var` built via `fmt.Sprintf` from
`guidedDecompositionMaxSubtasks` (7) so the bound is single-sourced (locked decision 5).

**What:** new `internal/mechanisms/guided_decomposition.go` (+ `_test.go`), registered via
`init()` in the catalogue. Descriptor: `{ID: guided_decomposition, Capability:
CapProactiveNudge, Suppression: SuppressStrikesThree, IncompatibleWith: [decompose],
Requires: [tool_result_cap]}` (locked decisions 2–3). Declared ordering: **`After
toolfilter`** — the sub_agent-presence check must read the *final* tool menu (`toolfilter`
mutates `SetTools`); seed the edge in `TestPreRequestOrderingSeeds`.

`PreRequest` gate — ALL of (ADR 0014 §5, measured signals only, no verb heuristics):
- `req.View().Budget().ContextLimit > 0` (unknown window → never fire);
- `req.View().Depth() == 0` (item 2 seam);
- `sub_agent` present in `req.View().Tools()`;
- signal A **or** B, computed with the `tool_result_cap` chars→tokens idiom
  (`chars / Budget.CharsPerToken`):
  - **A (Turn-1 fact):** the conversation's last message is the fresh user message and its
    estimated tokens exceed `Budget.FileContext` (the resolved `@file` blocks live inside it
    — `loop.go:191` — so the message embodies the resolved file context);
  - **B (mid-Exchange fact):** estimated history tokens exceed `Budget.History` AND the last
    assistant message carried tool calls (the model is mid-work; the auto-compact signal with
    no mid-Exchange consumer, ADR 0014 §5).

When gated in and no enumeration is outstanding (no marker in history), `InjectContext` the
enumeration steer, idempotent via a fixed marker string: reply with ONLY a numbered list of
at most 7 (locked decision 5) independent, self-contained subtasks — no other text, no tool
calls. When a fan-out is already in flight (a deferred directive from item 4 is doing the
steering), this half stays quiet — no double-steer. Exact prompt wording is the
implementer's, requirements above are not.

**Authoritative source:** ADR 0014 §1 (descriptor), §5 (gate); CONTEXT.md "Guided
decomposition".

**Tests:** descriptor/ordering/catalogue-Build table tests (the `decompose_test.go` shape);
revision-based act detection; gate table — zero Budget, Depth>0 (fake view), menu without
`sub_agent`, under-threshold A and B → no fire (revision unchanged); each signal alone over
threshold → fires; marker already in history → no re-inject; both hook interfaces
type-assert (`PreRequestHook` now, `PostResponseHook` after item 4 — adjust there).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit: `feat(mechanisms): guided_decomposition gate and enumeration steer`.

**Depends on:** items 1, 2.

---

## 4. Mechanism: the intercept + serialized follow-through (post-response half) — ✅ DONE (2026-07-05)

NOTES (2026-07-05): the follow-through case is additionally guarded on the `guidedDecompositionDirectiveMarker`
being present in the conversation (alongside "response carries a `sub_agent` call") before it re-derives —
a refinement of the literal case-2 text consistent with locked decision 1 ("a deferred directive is doing
the steering"). It is provably non-rejecting for a live fan-out (a non-empty remainder implies the prior
Turn deferred, so `buildRequest` injects the directive into this Turn's request), and it makes a spurious
model `sub_agent` call when no fan-out is in flight an explicit no-op rather than relying on the empty-
remainder derivation. Case 1 keys the list on `resp.Text()` (the enumeration is not yet committed at that
post-response point) per the item's own "text parses as a list" wording; case 2 reads the enumeration from
committed history.

**What:** `PostResponse` on the same struct. Three cases, evaluated against honest history
(the enumeration message and prior `sub_agent` calls read from
`resp.View().Conversation()` — never from mechanism-struct state, locked decision 1):

- **Enumeration response** (steer marker outstanding in the request, response has no tool
  calls, text parses as a 2–12-item list — numbered/bulleted/plain lines): synthesize the
  FIRST delegation via `resp.AppendToolCall` (`Tool: sub_agent`, `Arguments:
  {"task": <item 1 + compact-report hygiene sentence>}`, ID in the loop's synthesized-call
  style), leave `resp` text verbatim (locked decision 4), and return `ActionDefer` with the
  remaining-items directive for the next request: the remaining subtasks verbatim, an
  instruction to delegate exactly the next one via `sub_agent` (carrying the same
  compact-report hygiene ask, ADR §4), and to synthesize from all reports once none remain.
- **Fan-out follow-through** (remainder derived non-empty, response carries a `sub_agent`
  call the model emitted itself): re-derive the remainder MINUS this call's task and return
  `ActionDefer` with the updated directive. Matching is by enumeration-item text prefix
  against dispatched task args; an unmatchable model-authored task simply doesn't shrink the
  remainder (the model went off-script — tolerated, judged by self-regulation, ADR §5).
- **Anything else** (no marker, list out of bounds → decline whole, response already has
  other tool calls, remainder empty): inspect-only no-op — benign, zero revision, no
  decision (ADR §2 fail-soft).

Suppression needs NO code: hooks stop being dispatched (`hookrun.go` gates), the un-consumed
directive ground-truth lives in history, and at most one already-deferred directive drains
via loop plumbing (locked decision 1 — document this edge in the doc comment).

**Authoritative source:** ADR 0014 §2 (steer/intercept shape), §3 (serialized, one per Turn,
cursor over honest history), §4 (report hygiene), §5 (fail-soft, self-regulation).

**Tests:** list-parse table (numbered, bulleted, plain, fenced noise, 1 item → decline, 13
items → decline whole); enumeration case appends exactly one valid `sub_agent` call
(unmarshal args against `tools.SubAgentArgs`) + defers the remainder + text untouched;
follow-through shrinks the remainder and re-defers; off-script task leaves remainder intact;
remainder-empty and no-marker cases are zero-revision no-ops; derive-from-history works when
older child *results* were capped (the calls, not results, are the cursor's ground truth).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit:
`feat(mechanisms): guided_decomposition intercept and serialized delegation follow-through`.

**Depends on:** items 2, 3.

---

## 5. Wire-up proof + loop-level acceptance (fan-out end-to-end) — ✅ DONE (2026-07-05)

NOTES (2026-07-05): the config template DOES carry a commented `mechanisms:` example block, so the
commented key was added — but as the STACK (both `guided_decomposition: true` AND its Required peer
`tool_result_cap: true`) rather than the lone key: a commented `guided_decomposition` on its own would
be an erroring half-stack if uncommented (item-1 `ErrMissingRequirement`), so the two-line example is
valid-if-uncommented. The decompose-incompatibility config test enables `tool_result_cap` alongside so
that ONLY the incompatibility can surface (validation order is incompatibilities-before-requirements),
matching the item's "with `decompose` also enabled" as an addition to the booting stack. No production
code changes were needed — item 2's mutate-and-defer composition and the injected-steer visibility
through `req.View()` carried the wire-up as-is; this item is tests + the template comment only.

**What:** prove the whole stack through the real loop, no mechanism internals mocked.
Config: enabling `guided_decomposition` without `tool_result_cap` → the item-1 startup error
(exact text asserted); with both → boots; with `decompose` also enabled → the
incompatibility error. If the embedded config template (`cmd/apogee/defaults/config.yaml`)
carries a commented `mechanisms:` block, add the commented key; otherwise nothing (verify,
don't invent). Loop-level acceptance in `internal/agent` (the scripted-upstream +
fake-sandbox style of the existing agent/benchreadiness tests): an oversized user message →
Turn 1 request carries the enumeration steer; scripted 3-item list response → the appended
`sub_agent` call dispatches a REAL nested child (scripted child exchange) whose events nest
at `Depth == 1`; next request carries the deferred directive; scripted second delegation →
remainder shrinks; after the last report a scripted no-tool synthesis ends the Exchange with
all three reports in honest history. Plus: a snapshot taken at the quiescent boundary
mid-fan-out and resumed carries the pending directive (`conversationJSON.Deferred`
round-trip); Bypass mode runs the identical script with zero guided_decomposition activity
(ADR 0014 §1 control arm); cancel during a child rolls back only that parent Turn (existing
ADR 0013 §5 semantics stay green).

**Authoritative source:** ADR 0014 (whole Decision); ADR 0013 §5; ADR 0007 (boundaries).

**Tests:** as above — this item IS tests plus at most the config-template comment.

**Acceptance:** gates green (`go test ./...` incl. `-race` on `internal/agent`); diff
confined to `cmd/apogee`, `internal/agent` (tests) + docs/CHANGELOG. Commit:
`test(agent): guided_decomposition end-to-end fan-out acceptance`.

**Depends on:** items 1–4.

---

## 6. Docs close-out (the one owning item for every cross-cutting doc edit) — ✅ DONE (2026-07-05)

NOTES (2026-07-05): items 1–5 had already placed their five bullets under one shared
`### Guided decomposition (guided_decomposition, default-off)` heading, so the CHANGELOG (a) was
reconciled by ADDING a sixth "Docs close-out" bullet under that same heading (mirroring the
Phase-4 "closed out" CHANGELOG precedent) rather than rewriting the existing five — the single
coherent entry is preserved. (b)/(c)/(d) landed as written. The item-2 NOTES about the mandatory
`fakeView` compile-fix were treated as a mechanical test change, not an architectural deviation,
so they were NOT mirrored into the ADR Realisation note (which records design refinements only).

**What:** (a) CHANGELOG roll-up for the feature (items 1–5 each added their line; this item
reconciles them under one heading). (b) CONTEXT.md: extend the Guided decomposition entry's
_Avoid_ list with the one hazard the grill missed — the shipped **`decompose` Mechanism** (a
prompt-level focus/step nudge family, phase-4 item 12): "not the `decompose` Mechanism (a
prompt-shaping nudge; steers wording, not delegation — the two are declared incompatible)".
(c) ADR 0014: append a dated **Realisation** note recording locked decisions 1–5 (delivery
via one re-derived deferred directive per Turn; `IncompatibleWith: [decompose]`; registry-
level Requires validation; verbatim enumeration text; the 7/12 bounds) — refinements, not
changes, in the house style of ADR 0013's post-v1 realisation. (d) sweep the items' NOTES
lines for any authorized deviations and mirror them into the ADR note.

**Authoritative source:** the plan-author convention (every cross-cutting doc amendment has
exactly one owning item); ADR realisation-note precedent (ADR 0013, ADR 0007).

**Tests:** none (docs). Verify: links resolve; `grep -n "decompose" CONTEXT.md` shows the
disambiguation; CHANGELOG builds one coherent entry.

**Acceptance:** diff confined to docs (CHANGELOG.md, CONTEXT.md, docs/adr/0014). Commit:
`docs: guided decomposition close-out — decompose disambiguation and ADR realisation`.

**Depends on:** items 1–5.

---

## Explicitly NOT in this plan

- **The bench campaign and the default-on flip.** The Mechanism ships default-off; the
  ADR 0009 non-inferiority gate for the `guided_decomposition + tool_result_cap` stack is
  apogee-sim work, and only its evidence flips the default.
- **Depth-1 relaxation** (ADR 0014 §5 — additive later, evidence-gated).
- **Mid-Exchange auto-compaction** (parked in TODO.md 2026-07-05 — its own future grill).
- **Constant tuning** (subtask bounds, thresholds' slack): initial values are locked
  decision 5; moving them is bench evidence, not code review taste.
- **TUI affordances** for fan-out progress (a queue/chip display) — additive, post-this-plan.

## Critical files

**New:** `internal/mechanisms/guided_decomposition.go` (+ `_test.go`),
`docs/plans/guided-decomposition-plan.md` (this file).
**Modified:** `internal/domain/mechanism.go` (Requires), `internal/domain/hooks.go`
(`LoopView.Depth`, `Response.AppendToolCall`), `internal/agent/loop.go` +
`internal/agent/hookrun.go` (validation call, view threading, mutate+defer semantics),
`internal/agent` tests (acceptance), `cmd/apogee/defaults/config.yaml` (comment, if the
block exists), `CHANGELOG.md`, `CONTEXT.md` (decompose disambiguation),
`docs/adr/0014-...md` (realisation note).
