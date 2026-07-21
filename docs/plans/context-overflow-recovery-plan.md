# Plan — Context-overflow recovery: the emergency fold and one retry

**Date:** 2026-07-21
**Status:** NOT STARTED
**Track:** post-`v1.5.0`, structural (`internal/agent` + docs). No new Event variant, no facade
change — the public API is untouched, so this is a **minor** bump at most (CHANGELOG rule,
ADR 0001 §consequences).

**Authoritative sources (precedence):** if any item text disagrees with ADR 0006 (Bypass is the
Mechanisms-off floor; structural reducers stay on), ADR 0007 (a fault degrades the Turn to a
clean boundary), ADR 0017 (the Exchange boundary is cached, not re-derived), or
`internal/context/doc.go` (the reducer taxonomy), **the ADR and `doc.go` win**. Decision 1 below
is owner-ratified (2026-07-21); decisions 2–6 are proposed by this plan — implement them as
written and flag `STATUS QUESTION` rather than silently deviate. Item 5 is **NEEDS-DESIGN-CALL**:
consult the owner before starting it.

## Why

Running `/refocus` against a 32k-context model died mid-task:

```
loop: apogee: context window exceeded: {"error":{…"request (57546 tokens) exceeds the
available context size (32768 tokens)"…,"n_ctx":32768}}
```

The wire classification is already right — `isContextOverflow`
(`internal/provider/client.go:307`) turns the llama.cpp 400 into `DeltaContextOverflow`
(`internal/provider/stream.go:84-91`) — but the loop folds it into the same terminal outcome as
any stream fault (`internal/agent/loop.go:509-512`: `out.failed = true`), surfaces one
`ErrorEvent`, and **abandons the Exchange** (`loop.go:309-311` → `abandonTurn`, `:639-651`).
Nothing recovers, nothing retries, and the proactive machinery cannot help mid-task:

1. Automatic Compaction is **Exchange-boundary-only** — `shouldAutoCompact` refuses while
   `inExchange` (`internal/agent/compact.go:121-128`, the S2 rule). A doc-heavy Exchange
   (assistant → whole-file read → assistant → …) grows past the window where the fold cannot
   reach.
2. The only mid-Exchange reducer, the `tool_result_cap` Mechanism, is **default-off** and
   bypass-disabled (`internal/mechanisms/tool_result_cap.go:65-69`). It is now enabled in the
   owner's `~/.apogee/config.yaml` (2026-07-21) and recommended in the defaults template, but a
   config-dependent Mechanism cannot be the *guarantee*.
3. No loop-level bound exists on a single tool result entering history
   (`internal/agent/dispatch.go:411-415` appends verbatim; `read_file` has only a 10 MiB refusal
   bound).

**Owner-ratified decision 1 (2026-07-21):** overflow protection becomes **STRUCTURAL** — part of
the loop like Budget and automatic Compaction (ADR 0006 floor), not a config opt-in. The shipped
config template stays behaviour-neutral (`TestEmbeddedDefaultConfigIsNeutral`,
`cmd/apogee/defaults_test.go:57`, stands unchanged).

The core of this plan: **when a request overflows, fold the history and retry once** (reactive
guarantee, items 1–3), and **when the calibrated estimate already says the request cannot fit,
fold before sending** (predictive, item 4). Both reuse the existing generative fold
(`internal/context.Compact`) whose summary call is already overflow-safe near n_ctx
(`renderBudgetedTranscript`, `internal/context/compact.go:150-189`, budgeted by
`compactTranscriptChars`, `internal/agent/compact.go:156-166`).

## Where things stand (grounded, verified 2026-07-21)

- **Classification:** `ErrContextOverflow` (`internal/provider/client.go:30`);
  `isContextOverflow` (`client.go:307-321`) matches the llama.cpp body; streaming path
  `statusDelta` → `DeltaContextOverflow` (`internal/provider/stream.go:84-91`). The loop always
  streams, so the streaming path is the one that matters.
- **The conflation to undo:** `streamResponse`'s
  `case provider.DeltaError, provider.DeltaContextOverflow:` sets only `out.failed` + `errMsg`
  (`loop.go:509-512`); the `reply` struct (`loop.go:446-453`) records no overflow bit;
  `respondAndReview` emits the `ErrorEvent` and returns `turnFailed` (`loop.go:362-365`).
- **The fold:** `apogeectx.Compact(ctx, compactCompleter{a}, &a.conv, a.compactTranscriptChars())`
  keeps the protected prefix (leading system messages + first user message, `conv.PrefixEnd()`)
  and Replaces the rest with ONE assistant summary (`internal/context/compact.go:53-83`);
  no-ops (`Result.Skipped`) when fewer than `minCompactTail = 2` messages sit past the prefix
  (`:35`); mutates conv **only on success**. `compactCompleter` is a silent single completion
  that treats `DeltaContextOverflow` as a plain failure (`internal/agent/compact.go:190`) — keep
  that: a summary call has no recovery of its own (recursion guard).
- **Loop bookkeeping that a mid-Exchange fold touches:** `exchangeStart` is a cached boundary
  (ADR 0017 §2 recorded fallback; field at `internal/agent/agent.go:76`), already repaired after
  a mid-Exchange `truncate_history` shrink (`loop.go:283-285` — the precedent to mirror);
  `rollback` is captured pre-request (`loop.go:291`) and is **stale after any fold**;
  `buildRequest` drains the deferred queue (`loop.go:696-706`) and the fault paths re-queue it
  (`restoreDeferred`, e.g. `loop.go:310`); pre-request hooks run per request (`loop.go:298-303`).
- **Gates and guards to reuse:** `a.compacting` re-entrancy guard and `compactSat` saturation
  latch (`agent.go:74-75`); `cfg.Context.CompactionEnabled` (the `auto-compact` key, structural,
  on by default); `a.budget()` (`loop.go:812-824`) exposes `ContextLimit`, `ResponseReserve`,
  `CharsPerToken`, `History` on `domain.Budget` (`internal/domain/hooks.go:199-215`);
  `Budget.EstimateTokens` is zero/inert while uncalibrated or unbudgeted
  (`internal/domain/budget.go:16-21`); `domain.PromptChars` (`budget.go:47`) is the one measure,
  calibrated against server usage each Turn (`loop.go:497-498`).
- **On-demand `/compact` stays boundary-only** (`Agent.Compact` refuses mid-Exchange with
  `ErrInputPending`, `compact.go:42-48`) — a human can wait for the boundary; only the engine's
  overflow recovery earns the mid-Exchange exception.

## Decisions this plan implements

1. **Structural** (owner-ratified 2026-07-21): recovery runs under `--bypass` like Budget and
   Compaction; the template stays neutral; `tool_result_cap` remains the A/B-able tuning valve.
2. **Recovery shape:** full fold to protected prefix + assistant summary, then a **user-role
   bridge message** telling the model to continue from the summary. Role alternation stays
   strict-template-legal (prefix ends in the first user message → assistant summary → user
   bridge) and no dangling tool calls survive (the fold replaced them).
3. **One recovery fold per Turn**, predictive or reactive, whichever fires first. A second
   overflow on the retried request gives up **exactly like today**: the same sanitized
   `ErrorEvent` (source `"loop"`) + `abandonTurn`.
4. **`auto-compact: false` opts out of recovery too.** The emergency fold IS an automatic fold;
   a user who set `auto-compact: false` chose to manage the window themselves — for them,
   today's abandon behaviour stands unchanged.
5. **Quiet on success** — the `autoCompact` precedent (no event on a successful fold; the
   retried request's `UsageEvent` re-measures the gauge). No new Event variant.
6. **A cancel after the fold keeps the fold.** The fold is history maintenance, not part of the
   Turn's attempt — the same as a cancel after `autoCompact` keeps that fold. `cancelTurn` must
   therefore roll back to a **post-fold** boundary, never a pre-fold index.

---

## 1. Loop seam: overflow becomes its own turn outcome — ✅ DONE (2026-07-21)

NOTES (2026-07-21): the message is carried as a third return from `respondAndReview`
(`(*domain.Response, turnOutcome, string)`, non-empty only on `turnOverflowed`) rather than a
turn-scoped Agent field — the authorized implementer's choice, keeping no hidden state across
Turns. Beyond the item's literal text, `step()` also gained an interim `case turnOverflowed:` arm
(emit the carried message as `ErrorEvent` source `"loop"` → `restoreDeferred` → `abandonTurn`):
without it the new outcome would fall through to `resp.ToolCalls()` on a nil response. It is
today's give-up behaviour verbatim and is exactly the arm item 3 wraps in the retry loop.

**What:** in `internal/agent/loop.go`:

- `reply` (`:446-453`) gains `overflow bool`. Split the stream case: keep
  `case provider.DeltaError, provider.DeltaContextOverflow:` setting `failed`/`errMsg`, and set
  `out.overflow = delta.Kind == provider.DeltaContextOverflow`.
- `turnOutcome` (`:337-343`) gains `turnOverflowed`.
- `respondAndReview` (`:356-365`): on `reply.failed && reply.overflow` return
  `nil, turnOverflowed` **without emitting the `ErrorEvent`** — surfacing now belongs to the
  give-up path (item 3), which must emit the **identical** sanitized message. Carry the message
  to step() (a turn-scoped field on the Agent, or an extra return — implementer's choice; the
  constraint is that the give-up `ErrorEvent` is byte-identical to today's). A plain
  `DeltaError` keeps today's behaviour exactly (emit + `turnFailed`).
- `compactCompleter.Complete` (`internal/agent/compact.go:186-201`) is untouched: a summary
  call's overflow stays a plain failure.

**Tests:** in `internal/agent` with a fake upstream: a stream ending in
`DeltaContextOverflow` yields `turnOverflowed` and **no** `ErrorEvent` from
`respondAndReview`; `DeltaError` still yields `turnFailed` plus the event. Existing
`reliability`/`stream` tests stay green.

**Acceptance:** `go build ./... && go test ./internal/agent/... ./internal/provider/...` green.
Diff confined to `internal/agent`. Commit:
`feat(agent): classify context overflow as its own turn outcome`.

---

## 2. The emergency fold: mid-Exchange-legal, role-safe — ✅ DONE (2026-07-21)

**What:** in `internal/agent/compact.go`, a new `emergencyFold(ctx, turn) bool` (true ⇒ the
conversation was folded and the Turn may retry):

- **Gates, in order:** `cfg.Context.CompactionEnabled` (decision 4 — false ⇒ return false, no
  upstream call); the `a.compacting` re-entrancy guard (reuse it exactly as `autoCompact` does).
  No `inExchange` gate — running mid-Exchange is the point. No `--bypass` gate (structural,
  ADR 0006).
- **Fold:** `apogeectx.Compact(ctx, compactCompleter{a}, &a.conv, a.compactTranscriptChars())` —
  the same call `autoCompact` runs. `Result.Skipped` (tail < `minCompactTail`) ⇒ return false:
  there is nothing to fold, recovery is impossible. An error with `ctx.Err() != nil` ⇒ return
  false quietly (the caller's ctx check routes to the cancel path); any other error ⇒ one
  `ErrorEvent` source `"compaction"` (mirroring `autoCompact`, `compact.go:81`) and false.
- **Bridge:** on success append a user-role message (a package const, e.g. `overflowBridge`):
  "The conversation above was compacted because the previous request exceeded the model's
  context window. Continue the task from the summary." Document the invariant: the conversation
  now ends `…first-user | assistant-summary | user-bridge` — strict alternation holds and no
  dangling tool calls survive, so any chat template accepts the retried request.
- **Exchange re-anchor:** when `a.inExchange`, set `a.exchangeStart = a.conv.Len()-1` (the
  bridge's index) so `AbortExchange` rolls back to the clean prefix+summary boundary — cite the
  S2 repair precedent (`loop.go:283-285`) and ADR 0017 §2 in the doc comment.
- **`compactSat` is untouched** by this path: the saturation latch governs the estimate-driven
  trigger; the overflow-driven path is bounded by item 3's one-fold-per-Turn latch instead.

**Tests:** `internal/agent` (fake completer): folds while `inExchange`; result shape is
prefix + summary + bridge with `exchangeStart` re-anchored; `Skipped` tail ⇒ false and conv
untouched; `CompactionEnabled=false` ⇒ false and **no upstream call**; completer error ⇒ false,
conv untouched, one `ErrorEvent`; alternation + no-dangling-tool-calls asserted on the folded
conversation.

**Acceptance:** `go test ./internal/agent/...` green; diff confined to
`internal/agent/compact.go` + tests. Commit:
`feat(agent): an emergency fold that may run mid-Exchange`.

---

## 3. Recovery orchestration in step(): fold and retry once

**Depends on items 1–2.** This is the guarantee.

**What:** in `step()` (`internal/agent/loop.go:305-312`), bound the respond phase with one
recovery attempt (a small loop or an explicit second attempt — implementer's choice; a
`for attempt := 0; attempt < 2; attempt++` shape reads best):

- On `turnOverflowed`, first attempt only: `a.restoreDeferred(deferred)` (the drained
  injections must ride the retried request), then `a.emergencyFold(ctx, turn)`.
  - Fold refused/failed: **give up exactly like today** — emit the stashed sanitized message as
    `ErrorEvent` source `"loop"`, `restoreDeferred` semantics and `abandonTurn` as in the
    current `turnFailed` arm (`loop.go:309-311`). (Careful not to double-restore: restore
    happens once per give-up, as today.)
  - Fold succeeded: recompute **everything the fold invalidated** — `rollback = a.conv.Len()`
    (decision 6), rebuild `req, deferred = a.buildRequest(turn)`, recompute `deferredFloor`,
    re-run `runPreRequestHooks` on the rebuilt request (its failure path behaves as at
    `loop.go:298-303`), then `respondAndReview` again.
- A second `turnOverflowed` (attempt 2) routes to the same give-up arm. `turnCancelled` at any
  point routes to `cancelTurn` with the **current** (post-fold, if folded) `rollback`.
- Audit every local captured before the retry for staleness — `rollback`, `deferred`,
  `deferredFloor` are the known three; `exchangeStart` is item 2's job.

**Tests:** `internal/agent`, fake upstream scripted per-request:

- overflow then success ⇒ the Turn **completes**: history is prefix + summary + bridge +
  assistant reply; NO `ErrorEvent` anywhere on the recovered path; status
  `StatusTurnComplete`/`StatusExchangeComplete` as the reply dictates.
- overflow then overflow ⇒ exactly one `ErrorEvent`, message byte-identical to today's, and
  `StatusExchangeComplete` via `abandonTurn` — pin today's observable shape.
- overflow with `CompactionEnabled=false` ⇒ today's behaviour unchanged (one `ErrorEvent`,
  abandon, no upstream retry).
- overflow on a **tool-continuation** Turn (mid-Exchange, tool results in history) ⇒ recovery
  works and the retried request's message sequence is template-legal (no orphaned tool result,
  no two consecutive same-role messages).
- cancel during the fold ⇒ `StatusCancelled`, no corruption; snapshot round-trips (reuse the
  existing snapshot/resume test discipline).

**Acceptance:** `go test ./internal/agent/...` green (whole suite — the compaction, snapshot,
and reliability tests are the regression net). Commit:
`feat(agent): recover an overflowed turn with one fold and retry`.

---

## 4. Predictive pre-request guard: fold before a request that cannot fit

**Depends on items 2–3** (same fold, same one-per-Turn latch).

**What:** in `step()`, after `buildRequest` (`loop.go:293`) and before `runPreRequestHooks`:
with `b := a.budget()`, compute `est := b.EstimateTokens(domain.PromptChars(msgs, tools))` over
the request's projected messages + tool menu. If `est > b.ContextLimit - b.ResponseReserve`
(only possible when the window is known AND the estimator is calibrated — `EstimateTokens`
returns 0 otherwise, `internal/domain/budget.go:16-21`), run the same
`restoreDeferred → emergencyFold → rebuild` sequence as item 3, consuming the Turn's one fold.
The reactive path (item 3) remains as the backstop when the estimate was wrong.

**Why it earns its place:** it saves the wasted wire round-trip on a predictable overflow, and
it is the **only** protection against a server whose 400 body `isContextOverflow` cannot
classify (there the stream yields plain `DeltaError` and the reactive path never fires).

**Threshold note:** use the full `ContextLimit - ResponseReserve` working room, not a softer
fraction — a fold rewrites history, so it must fire only when the estimate says the request
*cannot* fit, never as a comfort margin. `HistoryExceedsAllocation` (the ~60% History slice)
remains the boundary trigger's business, not this guard's.

**Tests:** fake upstream + a pre-calibrated estimator (drive one scripted `UsageEvent` first or
set the ratio via the estimator's seam): an over-window request folds BEFORE any upstream call
(the fake records only the post-fold request); an exactly-fitting request does not fold; an
uncalibrated/unbudgeted Agent never predictively folds.

**Acceptance:** `go test ./internal/agent/...` green. Commit:
`feat(agent): predictively fold before a request that cannot fit`.

---

## 5. Structural floor on a single oversized tool result — **NEEDS-DESIGN-CALL**

**Consult the owner before starting this item** (implement-plan: stop and ask). It sharpens the
ADR 0002/0006 line between curated Mechanisms and structure, and the owner may prefer to rely
on items 2–4 plus the `tool_result_cap` Mechanism alone.

**Proposal:** in `appendToolResult` (`internal/agent/dispatch.go:411-415`), clamp a single tool
result whose estimated tokens exceed the **entire History allocation** (`a.budget().History`;
inert when the window is unknown) to a head/tail elision with a marker — the same rendering
discipline as the Mechanism's `truncateToolResult`
(`internal/mechanisms/tool_result_cap.go:143`), hoisted to a shared home rather than
duplicated. Rationale: a result bigger than everything History may hold can never survive *any*
reducer — appending it whole buys nothing and can doom the Turn before `tool_result_cap` (if
enabled) or the emergency fold get a say. The floor is deliberately generous (fires only on
pathological results); the Mechanism keeps its tighter 40%-of-budget nudge
(`toolResultBudgetFraction`, `tool_result_cap.go:26`) as the A/B-able behaviour, and when
enabled it fires first, making the floor a no-op.

**Tests:** a result over the History allocation is clamped with the marker; under it passes
verbatim; unknown window passes verbatim; with `tool_result_cap` enabled the Mechanism's
tighter cap is what the request carries.

**Acceptance:** `go test ./internal/agent/... ./internal/mechanisms/...` green. Commit:
`feat(agent): a structural floor on a single oversized tool result`.

---

## 6. Docs close-out (the one owning item for every doc edit)

**Depends on items 1–4 (and 5 if ratified).**

**What:**

- **New ADR 0018** — "Context overflow recovers structurally: the emergency fold and one
  retry." Record: the overflow-driven fold may run mid-Exchange, **amending S2's
  Exchange-boundary-only rule for this path only** (the estimate-driven `autoCompact` trigger
  and the on-demand `/compact` both stay boundary-only, and why the asymmetry is right — the
  human can wait, a dying Turn cannot); the bridge shape and its alternation argument;
  one-fold-per-Turn; the `auto-compact: false` opt-out; quiet-on-success; the predictive
  threshold; `tool_result_cap`'s continued role as the tunable valve.
- `internal/context/doc.go` — add the emergency fold to the reducer taxonomy.
- `CONTEXT.md` (Budget/Compaction/reducer definitions, ~lines 377-434) — same addition, and
  **correct "tool_result_cap — the only reducer able to act mid-Exchange"**, which item 3 makes
  false.
- `internal/agent/compact.go` — the `autoCompact` doc comment's S2 paragraph names the
  emergency path as the second, overflow-driven exception.
- `docs/design/technical-design.md` — the context-reducers row (~:196) and §8 backlog item 8
  gain the recovery; strike any "overflow abandons the turn" claim if present.
- `CHANGELOG.md` — `[Unreleased]` **Added**: structural context-overflow recovery (fold +
  one retry, predictive guard); note it is behaviour-only (no API/facade change).
- `cmd/apogee/defaults/config.yaml` — one sentence in the `auto-compact` comment: it also
  powers overflow recovery, and `auto-compact: false` opts out of both. (The template stays
  fully commented — `TestEmbeddedDefaultConfigIsNeutral` must stay green.)

**Tests:** none (docs); `go build ./... && go test ./...` as the final whole-plan gate.

**Acceptance:** docs-only diff; grep finds no stale "only reducer able to act mid-Exchange"
claim. Commit: `docs: record structural context-overflow recovery (ADR 0018)`.

---

## Plan-wide gates

`go build ./...`, `go vet ./...`, `go test ./...` after every item. End-of-plan live proof
against the host endpoint (`http://192.168.64.1:1111`, a ~32k profile via llama-launcher):

1. Run `/refocus` (or any doc-heavy task) in a real repo and confirm the session **survives**
   where it previously died — either no overflow reaches the wire (predictive/`tool_result_cap`)
   or the transcript shows the fold's effect (context gauge drops on the next `UsageEvent`) and
   the task continues from the summary.
2. Confirm the give-up path still reads exactly as before by forcing it
   (`auto-compact: false` + a deliberately oversized paste).

## Explicitly NOT in this plan

- **Enabling `tool_result_cap` in the shipped template** — owner decision 2026-07-21: the
  template stays behaviour-neutral (and an explicit `mechanisms:` block would suppress
  Validated-set auto-application, ADR 0016). The owner's own config carries it; the template
  documents the recommendation.
- **Estimator template-markup accounting.** `PromptChars` deliberately omits chat-template
  markup on *both* sides of the calibrated ratio so the offset cancels
  (`internal/domain/budget.go:41-46`); the predictive guard is simply inert until first
  calibration, and the reactive path covers Turn 1.
- **Token-aware trimming of `@file` refs and skill blocks at input time** — the deferred
  context-builder concern (TDD §8 #8; `maxRefFileBytes` comment, `loop.go:708-711`).
- **A new Event variant / TUI affordance for recovery** (quiet-on-success; revisit only if live
  use proves confusing).
- **Re-litigating ADR 0016's whole-set-or-nothing rule** or enabling `guided_decomposition`.

## Critical files

**Modified:** `internal/agent/loop.go` (reply/outcome seam, orchestration, predictive guard),
`internal/agent/compact.go` (`emergencyFold` + doc comments), `internal/agent/dispatch.go`
(item 5 only), tests across `internal/agent/`; docs: `docs/adr/0018-…`, `internal/context/doc.go`,
`CONTEXT.md`, `docs/design/technical-design.md`, `CHANGELOG.md`,
`cmd/apogee/defaults/config.yaml` (comment only).
**Untouched by design:** `internal/provider` (classification already correct),
`internal/context/compact.go` (the fold is reused as-is), the TUI, the facade (`apogee.go`).
