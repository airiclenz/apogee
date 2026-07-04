# Plan — Phase-4 wave-1 review fixes (items 2–6 findings)

**Date:** 2026-07-04
**Status:** ready to run.
**How to run:** `implement-plan docs/plans/phase-4-review-fixes-plan.md with skills: coding-standards`
**Source of direction:** the 2026-07-04 four-agent review of Phase-4 items 1–6
(commits `9e328d0`..`af0647d`), owner-ratified decisions below;
`docs/plans/phase-4-detail-plan.md` (spec text for items 3/5/6, D1–D8);
`docs/design/mechanism-catalogue.md` (the ratified map — amended by this plan where
the pinned sim source contradicts a cell); the pinned sim @
`d22086701ff9ba8e5565f9587945d6d97434b646`.
**Verify gate (every item):** `make check` plus the item's own test commands.

**Working-tree caveat:** `ISSUES.md` carries an uncommitted owner edit (re-opened TUI
issues). It is NOT part of this plan — never stage, commit, or revert it. Per-item
commits stage their own files explicitly; "gates green" does not require a clean tree,
only that nothing of this plan's leaks outside its commit.

---

## Design record (owner-ratified 2026-07-04 — do not re-litigate)

- **R1 — Retry-in-place is the correction path; C5 is amended.** The owner ratified
  (2026-07-04, superseding catalogue C5's ActionDefer expression): a post-response
  Mechanism delivers its correction by `ActionRetry` + `Inject`, and the loop re-streams
  the corrected request **in the same Turn**. The loop seam: on ActionRetry it appends to
  the in-flight request — request-scoped, never committed to history — (a) the superseded
  assistant message (text + tool calls) when non-empty, then (b) the `Inject` text as a
  role-safe user correction. This mirrors the sim's own retry builders exactly
  (`retryWithCorrection` `response_validator.go:366-391`, `retryForToolUse`
  `tooluse_enforcer.go:59-83`, `retryForEmptyResponseWithStrategy`
  `empty_recovery.go:131-176` — all copy the request, append assistant msg where present
  + user correction, re-call). C5's *substance* stands: `feed_forward_correction` is still
  folded, no standalone Mechanism; only the delivery expression changes. `ActionDefer`
  remains available (it keeps its next-request semantics) but wave 1 no longer uses it.
  Bounded by the existing `maxPostResponseRetries = 3` (`loop.go:32`) — at the cap the
  last response passes through.
- **R2 — Accepted, recorded divergences from the sim's retry ladder** (bench-pending
  refinements, NOT ported now): per-attempt temperature escalation (0.9→1.1), context
  trimming (`maxRetryContextMessages`), the empty-recovery *system* directive
  (`injectSystemDirective`), keep-original-if-retry-worse best-of, the sim's per-session
  throttle counters (2-cap/cooldown for empty recovery, 3/session for the enforcer —
  the shared loop cap of 3 substitutes for both), and `ensureNonEmptyResponse`'s
  `"[no response]"` placeholder at the cap. Each is named in the catalogue rows / NOTES
  (item 6) so the trail is deliberate.
- **R3 — The four proxy signals judge the NEXT Turn, three-way.** Per plan item 3's spec
  and CONTEXT "Self-regulation": fires recorded in Turn N are judged by Turn N+1's
  outcome. Outcome is three-way — **productive** (a novel file read, or a successful
  write/action), **harmful** (a tool-result error, or an empty final response), **neutral**
  (neither — e.g. a substantive text-only answer). Productive wins when signals mix.
  Strikes and the Turn-Budget streak advance **only on harmful** Turns; a neutral Turn
  freezes both (no strike, no streak advance, no clear); a productive Turn is the existing
  global clear-path. Consequence (the point of the fix): a pure Q&A session neither
  strikes mechanisms nor trips the Turn Budget.
- **R4 — Fired = acted.** A catalogued Mechanism is booked (recordFire +
  `MechanismFiredEvent` + judgment set) only when its invocation **acted**: it returned a
  non-zero post-response Action, or it mutated its working value (request / response /
  conversation / tool call / tool result). An inspect-and-do-nothing invocation is not a
  fire — matching the sim's `FiredCounts` (interventions, not invocations).
  `LoopView.Fired` therefore counts actions. Experimental hooks keep today's
  always-booked behaviour under the synthetic ID (bench observability).
- **R5 — Known accepted leftovers (record, don't churn):** `fireCounts` retained from a
  cancelled (rolled-back) Turn may over-report toward the decompose coupling (benign —
  Fired is a heuristic input); the trivial `defaultCharsPerToken` stays until plan item 8;
  the reserved experimental ID moves to `internal/domain` so the registry can refuse it.

---

## 1. Loop seam: ActionRetry carries the corrective exchange — ✅ DONE (2026-07-04)

**What:** implement R1 in `internal/domain` + `internal/agent`.
Domain: `PostResponseDecision.Inject` doc changes to "the correction text — injected into
the retried request for ActionRetry, or the next request for ActionDefer"
(`mechanism.go:73-78`; drop "ActionRetry carries nothing" at `:72`). `Request` gains the
loop-side seam to append the superseded assistant message (suggested:
`AppendSupersededAssistant(text string, calls []ToolCall)` — doc-commented as the loop's
retry-exchange seam, not a hook-mutation primitive; skip entirely when text is empty and
calls are nil). Agent: `runPostResponseHooks`/`applyPostResponse` (`hookrun.go:116-169`)
surface the retrying decision's `Inject`; `respondAndReview` (`loop.go:252-286`), on
`retry && attempt < maxPostResponseRetries`, appends the superseded assistant message
(from the reviewed `resp`: `resp.Text()`, `resp.ToolCalls()`) then
`req.InjectContext(inject)` when non-empty, before `continue` — so the re-stream carries
the exchange the sim's retry builders carried. Corrections accumulate across attempts
(bounded by the cap) — matching the sim's escalating re-asks; note it in the doc comment.

**Docs (same commit):** amend `docs/design/mechanism-catalogue.md` C5 with the R1
ratification (date + rationale: the loop, unlike the sim's proxy, owns the stream and can
reset it — `StreamResetEvent` was built for this; Table A `validate` row's "short-circuits
cascade on fail" becomes true via retry semantics). Add a pointer line to
`docs/design/hook-mutation-api.md` §4.1 ("C5 amended → retry-in-place, see catalogue").

**Tests:** with a request-capturing fake responder: ActionRetry{Inject} from an
experimental hook → the second provider request contains the superseded assistant message
(content + tool calls) and the user-role correction; empty superseded response → only the
correction appended; Inject == "" → today's bare re-stream; corrections accumulate across
two retries; at the cap the last response passes through (no further append);
ActionDefer's next-request path is byte-unchanged (existing tests keep passing).

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/agent`,
docs + CHANGELOG. Commit:
`feat(agent): retry-in-place corrective exchange for post-response mechanisms`.

---

## 2. Wave-1 delivery switch: validate, syntax, enforcer, empty-recovery ride the seam

**What:** switch the four shipped Mechanisms to R1 delivery, per the pinned sim.
`validate` (`internal/mechanisms/validate.go:60`): `ActionDefer` → `ActionRetry` with the
same `buildCorrectionMessage` inject — the catalogue's "short-circuits cascade on fail"
now holds (retry short-circuits `runPostResponseHooks`). `syntax` (`syntax.go:58`): same
switch (its correction covers post-autofix remainders once item 3 reorders the cascade).
`tool_use_enforcer` (`tool_use_enforcer.go:54-57`): `ActionDefer` → `ActionRetry{Inject:
buildToolUseCorrection(...)}` — fixes the review finding that the correction sat until the
next user Submit while the sim re-called in-cycle (`response_validator.go:188-222`); the
retried request carries the narration (item 1's superseded-assistant append), exactly the
sim's `retryForToolUse` shape. `empty_response_recovery` (`empty_response.go:56`): return
`ActionRetry{Inject: completionCheckNudge}` — port the sim's first-attempt nudge text
verbatim (`empty_recovery.go:21`); the attempt-2 context-aware nudge ladder + system
directive + temp escalation stay un-ported per R2 (update the `offramps.go:16-31` comment
block to cite R1/R2 instead of "cannot express today"). Update the enshrining test
assertion (`empty_response_test.go:25`). Rewrite the stale delivery rationale comments
(`robustness.go:16-23`, mechanism doc comments citing C5-as-defer).

**Tests:** through the scripted-responder harness (`internal/agent` fakes — loop-level,
per plan item 5/6's original test spec): bad tool call → retried request contains the
correction → scripted second response dispatches fixed; validate-fail short-circuits the
cascade (fired events show no `syntax`/`autofix` in the failing pass); narration ×2 with
action intent → enforcer retry carries narration + correction; empty response → retried
request carries the nudge; always-empty scripted responder → terminates at the cap, empty
final passes through; both off-ramps still fire under Bypass and through a tripped Turn
Budget **at dispatch level** (registry-built, not descriptor-only — closes the review's
test-gap).

**Acceptance:** gates green; diff confined to `internal/mechanisms`,
`internal/agent` (tests), docs/CHANGELOG. Commit:
`fix(mechanisms): deliver wave-1 corrections by retry-in-place per amended C5`.

**Depends on:** item 1.

---

## 3. autofix: construction-injected availability + sim repair semantics + cascade order

**What:** three review findings on `internal/mechanisms/autofix.go`, all grounded in the
pinned sim (`internal/autofix/{autofix,formatters}.go`).
**(a) D3 compliance:** `Deps` (`catalogue.go`) gains `LookPath func(string) (string,
error)` (nil ⇒ `exec.LookPath`); `newAutofix` probes goimports/black/prettier/rustfmt
**once at construction** and caches resolved paths (the sim's "LookPath-cached formatter
table"); fires never probe — delete the package-var-at-fire-time path
(`autofix.go:24,41,111,147`). `cmd/apogee/wire.go` passes the production default.
**(b) Sim semantics:** autofix acts only on syntax-broken write content and keeps output
only when it *reduces* the issue count (sim `tryAutoFix`/`AttemptFix`): per write call —
`checkSyntax` issues == 0 ⇒ skip (no unconditional beautification); issues > 0 ⇒ format,
re-check, write back via `SetToolCallArguments` only if the count decreased. Restore the
sim's `sanitizePath` NUL/control-char guard alongside the kept `-` prefix guard.
**(c) Cascade order:** swap to validate → **autofix** → **syntax** — the sim runs
detect → `tryAutoFix` → correct-the-remainder (`response_analysis.go:72-88`), so repair
must precede the correction stage or syntax over-corrects issues autofix would have fixed
(the review's double-correction finding). Update `Ordering()` on both mechanisms and the
catalogue Table A ordering cells + the "Post-response cascade" section (same commit,
with a one-line rationale citing the sim lines).

**Tests:** counting LookPath stub → probed at construction, **zero** lookups at fire
time; clean Go payload untouched (no beautification); broken-then-fixable payload
formatted and written back; format-that-doesn't-reduce-issues discarded; missing external
formatter degrades silently (construction with not-found stub); order determinism test
updated to validate→autofix→syntax.

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `cmd/apogee`,
docs/CHANGELOG. Commit:
`fix(mechanisms): autofix construction-time formatter table and sim-faithful repair gating`.

---

## 4. Self-regulation: next-turn judgment, four signals, acted fires

**What:** implement R3 + R4 in `internal/agent/selfreg.go` / `hookrun.go` / `loop.go` /
`dispatch.go` + `internal/domain`.
**Next-turn judging (R3):** `selfRegulator` gains a `pendingJudgment` set — `endTurn`
first judges *pending* (the previous Turn's fires) against THIS Turn's outcome, then
shifts `firedThisTurn` → pending. `discardTurn` (cancel/abandon) clears the per-Turn
scratch but leaves pending in place (the re-attempt's outcome judges it). Update the
`dispatch.go:56-59` comment (the fires-judged-against-state-they-saw rationale is
obsolete once judgment is next-Turn).
**Four signals, three-way outcome (R3):** `noteToolProductivity` records a tool-result
error as a harmful signal instead of returning early (`selfreg.go:164-167`); `step()`'s
final no-tool path notes an empty (whitespace-only, no-calls) response as harmful;
outcome = productive if (novel read ∨ successful write), else harmful if (tool error ∨
empty response), else neutral. Strikes + streak advance only on harmful; neutral freezes;
productive clears (unchanged). Rewrite the selfreg doc-comment block to the new model.
**Cancelled-Turn read rollback:** track this Turn's novel-read keys in per-Turn scratch;
`cancelTurn`'s discard also removes them from `seenReads`, so the mandated re-attempt
regains its novelty credit (the review's re-attempt-penalty bug).
**Acted fires (R4):** `Request`/`Response`/`Conversation` gain an internal revision
counter bumped by each mutator (`AppendToSystem` only when it injected;
`InjectContext`/`SetMessageContent`/`SetTools`/`SetExtra`/`SetSampling`;
`SetText`/`SetToolCallArguments`; the Conversation mutators) with a `Revision() int`
accessor; `hookrun.go` brackets each **catalogued** fire — acted ⇔ revision changed, or
(post-response) decision.Action ≠ zero, or (pre-tool-exec / post-tool-result) the
call/result snapshot differs — and books recordFire + `MechanismFiredEvent` only when
acted. Experimental hooks unchanged. Update `LoopView.Fired`'s doc (counts actions) and
`hookrun.go`'s header comment.
**Reserved ID:** move the `"experimental"` constant to `internal/domain`
(`loop.go:21` consumes it); `MechanismRegistry.Add` refuses a Mechanism carrying it
(item 5 adds the duplicate check in the same method).

**Tests:** fire in Turn N judged by Turn N+1 (harmful N+1 strikes N's fires; N's own
outcome does not); neutral Turn freezes strikes AND streak; tool-error Turn is harmful;
empty-response Turn is harmful; a pure text-Q&A session (echo responder, many exchanges)
strikes nothing and never trips the budget; cancelled-Turn re-attempt regains read
novelty; no-op invocation (inspect-only mechanism) → no MechanismFiredEvent, no strike,
Fired == 0; acting mechanism booked; **loop-level end-to-end**: scripted responder driving
real tool calls → productive Turn clears strikes/budget through
`dispatchTools`→`noteToolProductivity`→`endTurn`; erroring-tool script ×8 → budget trips
and a non-exempt mechanism is withdrawn at dispatch while an exempt one still fires
(closes the review's echo-responder-only gap); reset-on-Resume unchanged.

**Acceptance:** gates green; diff confined to `internal/agent`, `internal/domain`,
docs/CHANGELOG. Commit:
`fix(agent): next-turn judgment on the four proxy signals with acted-fire booking`.

---

## 5. Registry + config hardening

**What:** the three loud-failure holes.
**Duplicate ID:** `MechanismRegistry.Add` (`domain/mechanism.go:166-172`) refuses a
`MechanismID` already registered (today `topoSort`'s `byID` map silently drops one —
`registry.go:126-132`); loud error naming the ID.
**Reserved ID:** `Add` refuses the experimental sentinel (item 4 moves the constant).
**Config keys:** `buildMechanismRegistry` (`cmd/apogee/wire.go:227-250`) validates
**every** `mechanisms:` key against the known catalogue — a typo'd `false` entry is
today silently accepted (`wire_test.go:303` bakes it in) while README/config.yaml promise
a loud error. Validate disabled keys by name (via the catalogue's known-IDs surface;
do NOT construct disabled mechanisms), keep the existing build-path error for enabled
ones; error text lists the known catalogue as today.
**New-time incompatibility e2e:** a test proving `New`/`newAgent` surfaces
`ErrIncompatibleMechanisms` at construction (the review found only method-level
coverage). **Bypass dispatch matrix breadth:** parametrize the existing
`mechanism_dispatch_test.go` Bypass/order coverage across all five hook points (today
`HookPreRequest` only).

**Tests:** duplicate Add errors; reserved-ID Add errors; `{"typo": false}` → startup
error listing the catalogue; `{"typo": true}` unchanged; disabled-valid keys still build
nothing; New-time incompatibility; the five-point Bypass matrix.

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/agent`
(tests), `cmd/apogee`, docs/CHANGELOG (README already promises the behaviour — no README
change needed). Commit:
`fix(domain,config): reject duplicate and reserved mechanism IDs, validate disabled config keys`.

---

## 6. Docs close-out: catalogue amendments + the NOTES trail

**What:** the record-keeping the reviews found missing (items 1–5 above carry their own
code-adjacent doc edits in their commits; this item is the residue).
**Catalogue (`docs/design/mechanism-catalogue.md`):** add the missing `cot` row to
Table C + Ledger (SPLIT → `stall_nudge`/`list_nudge`/`tool_use_directive` per C4) and fix
C4's prose ("alongside `decompose` and `cot`" — cot IS the three nudges, not a fourth
member); fix the `library` row's hook-point cell ("pre-request (inject) + observe" —
"observe" is not a hook point; reword to "pre-request (inject); observer half's hook
point decided in item 14"); correct the imprecise refs (`capToolResults` ~`:458` →
`compress.go:428/431`; gap-note insertion `intervention.go:99-178` → `:180-181`); record
the R2 throttle divergence on the two off-ramp rows (shared loop cap 3 substitutes the
sim's 2-cap/cooldown and 3/session counters).
**NOTES trail (`docs/plans/phase-4-detail-plan.md`):** append a short `NOTES
(2026-07-04 review)` line under items 3, 5, and 6 naming the deviations found and their
resolution ("fixed by phase-4-review-fixes items 1–5" / "accepted per R2/R5") — the
deviation record D7 and the acceptance clauses required.
**CHANGELOG:** one Unreleased entry summarizing the review pass (behaviour changes:
retry-in-place corrections, autofix repair gating, next-turn judgment, acted fires,
duplicate/unknown-ID hardening).

**Acceptance:** gates green (docs-only otherwise); the ISSUES.md caveat above still
holds. Commit:
`docs: record the Phase-4 wave-1 review findings, amend the catalogue, close the NOTES trail`.

---

## Explicitly NOT in this plan

- Items 7–16 of the Phase-4 detail plan (they proceed after this lands, on the corrected
  substrate).
- The R2 divergences (retry ladder refinements, best-of retry, `"[no response]"`
  sanitizing, per-mechanism throttle counters) — bench-pending; the catalogue records them.
- Any default flip, any bench work, any apogee-sim change (D1/D8 unchanged).
- The `Deps.Library any` narrowing (item 13's job) and the config→dispatch cross-boundary
  integration test (plan item 15's bench-readiness proof covers it).
