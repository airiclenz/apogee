# Plan — Post-v1.3.0 review fixes (guided-decomposition substrate + enable-surface hardening)

**Date:** 2026-07-06
**Status:** ready to run.
**How to run:** `implement-plan docs/plans/post-v1.3.0-review-fixes-plan.md with skills: coding-standards`
**Source of direction:** the 2026-07-06 four-lens review of `v1.2.0...HEAD`
(`docs/code-review-2026-07-06.md`) — 5 High / 10 Medium findings, no Critical. Binding ground
truth: ADR 0014 (`docs/adr/0014-...md`, incl. its Realisation section) and ADR 0015 for intent;
`docs/design/mechanism-catalogue.md` Table A for composition/descriptors; the pinned sim @
`d22086701ff9ba8e5565f9587945d6d97434b646` at `~/Repos/Airic/apogee-sim` for ported-set behaviour
(item 11 only). The review report is the findings record — if the code contradicts a finding's
premise (already fixed, line moved), re-verify against the code and say so in NOTES rather than
"fixing" a non-defect.
**Verify gate (every item):** `make check` (gofmt-clean, vet, build, race tests, ADR-0010
invariant) plus the item's own test commands. "Gates green" means exactly this.
**CHANGELOG:** each item adds its line under an `## [Unreleased]` section (create it above
`[1.3.0]` on first need, following the file's format).

**Precedence (points at ground truth, never at an artifact of this plan):** for guided
decomposition's *intent*, ADR 0014's Decision section wins; the Design record below (F1–F8)
*refines its Realisation* and is ratified by running this plan — where an item's letter and an
F-decision seem to conflict, or where either seems to conflict with ADR 0014's Decision, flag
STATUS QUESTION rather than deviate. Every `file:line` below was verified 2026-07-06 against
`2a33c26`; earlier items of this plan may shift them — re-locate by symbol name before editing.

**Deviation trail:** any authorized deviation from an item's text lands as a dated NOTES line
under that item — no silent divergence.

**Before running:** commit `docs/code-review-2026-07-06.md` and this plan file first (suggested:
`docs: add the post-v1.3.0 review fixes plan and file the review report`) — the implement-plan
preflight stops on a dirty tree.

---

## Design record (from the 2026-07-06 review; running this plan ratifies F1–F8 — flag STATUS QUESTION rather than deviate)

Shared context for F1–F5: the mid-Exchange conversation shape is
`[…, user ask, assistant(calls), tool results, assistant(calls), …]` — tool results are
`RoleTool`, and every request-scoped injection (`InjectContext`) lands *before* the last user
message or in the system message, never after it. So **"the current Exchange" = the messages
strictly after the last `RoleUser` message in the view**, and that boundary is stable across
injections. F1 and F3 both build on this; if the implementer finds a message shape that violates
it (e.g. a user-role message that can appear mid-Exchange), STATUS QUESTION.

- **F1 — The gate is once-per-Exchange, on committed evidence, not markers.** The pre-request
  gate stays quiet for the remainder of an Exchange once a fan-out has begun in it, judged from
  the conversation itself: quiet whenever any assistant message in the current Exchange carries a
  `sub_agent` tool call, or qualifies as the enumeration anchor (F3). Markers stay only as the
  same-request double-steer guard. Rationale: both markers are request-scoped (the steer rides an
  `InjectContext` copy, the directive rides the deferred drain) and vanish from the next request
  the moment nothing is re-deferred — while signal B stays true after a fan-out (child reports
  grew history; auto-compaction cannot run mid-Exchange), so the steer re-fires on the synthesis
  turn and loops the decomposition. A model that delegated unprompted this Exchange also silences
  the gate — it is already delegating; a steer adds nothing. A new Exchange (new last user
  message) naturally re-arms the gate.
- **F2 — Off-script tool Turns re-defer the directive; final answers never do.** In PostResponse,
  when the directive marker is present, the response carries at least one tool call, none of them
  `sub_agent`, and the re-derived remainder is non-empty → re-defer the shrunken directive
  (`ActionDefer`, a normal act under R4). When the response carries NO tool calls, do NOT
  re-defer regardless of remainder — a no-tool response ends the Exchange, and a directive
  deferred there would cross into the next Exchange (the exact defect F6 closes); the model
  closing a fan-out with its own answer is the accepted §5 fail-soft.
- **F3 — The enumeration anchor requires the synthesized delegation.**
  `guidedDecompositionEnumeration` anchors on the FIRST assistant message *within the current
  Exchange* that BOTH parses in-bounds (under F4) AND carries at least one `sub_agent` tool call.
  The real enumeration uniquely combines both — the intercept appends the first delegation onto
  the enumeration response, so it commits as list + call (`assistantMessage(resp, calls)`).
  Prior-Exchange answers, mid-Exchange narration, and compaction summaries (multi-line
  `RoleAssistant`) carry no `sub_agent` call and can no longer shadow it.
- **F4 — An enumeration needs explicit list markers on a strict majority of its lines.**
  `guidedDecompositionParseList` learns whether each kept line carried an explicit ordered/bullet
  marker (`guidedDecompositionStripMarker` already computes this — it must start *returning* it);
  a reply is treated as an enumeration only when the parsed items are in-bounds AND strictly more
  than half of them carried explicit markers. Unmarked lines still become items when the majority
  test passes (small-model tolerance). The steer asks for a numbered list, so a compliant reply
  passes trivially; a clarifying question, refusal, or prose answer does not. This amends
  ADR 0014 §2's plain-line leniency — the review's "warrants revisiting" escalation: the leniency
  is the enabling root cause of the three High hijacks.
- **F5 — Marker detection is line-anchored and role-scoped.** Markers match only in `RoleUser`
  and `RoleSystem` messages (the only places the loop's `InjectContext` puts injections), and
  only at the start of a line. Both injected texts already begin with their marker phrase.
  Assistant echoes and tool results never match; `@file` content collides only in the
  line-start-inside-a-user-message case, and with F1 carrying idempotency the residual blast
  radius of a collision is one benign no-op or one self-regulated misfire. Marker *strings* stay
  unchanged (they are the loop-level tests' wire contract). If the system-append injection branch
  does not place injections at a line start, STATUS QUESTION.
- **F6 — Deferred Response Actions are Exchange-scoped.** The deferred queue is cleared whenever
  an Exchange ends: `completeTurn`'s `StatusExchangeComplete` branch, `abandonTurn`, and
  `AbortExchange`. `cancelTurn` (Exchange stays open; the Turn is re-attempted) first truncates
  the queue back to its pre-hooks length — dropping what the cancelled Turn's own post-response
  hooks deferred — and then `restoreDeferred` re-queues the drained injections, so a re-attempt
  or snapshot never carries two contradictory directives. Rationale: a deferral is a decision
  about the next request of the SAME conversation flow; the wave-1 repairs retry in place, and no
  catalogued Mechanism legitimately defers across Exchanges. If the implementer finds one that
  does, STATUS QUESTION.
- **F7 — `guided_decomposition` is incompatible with `truncate_history`.** truncate_history is
  currently a legal peer that keeps only the last N exchanges and runs mid-Exchange — a fan-out
  longer than its window drops the enumeration message and destroys the cursor. One-sided
  declaration suffices: `detectIncompatibility` checks each descriptor's `IncompatibleWith`
  against every registered ID (`internal/domain/registry.go:191-209`).
- **F8 — Spelling families, not one merged set.** The read/list tool-name sets consolidate as
  single-source *spelling families* (the read trio `read_file/readFile/open_file`; the five list
  spellings `list_files/listFiles/list_dir/listDir/list_directory`) hoisted beside
  `wave4WriteTools`; each mechanism's set composes from the families while keeping its documented
  per-source *membership* (which tool concepts belong in the set stays local — the sets serve
  different purposes and several carry `@pin` rationales). Ratified gap fixes (each a behaviour
  change with a test): `cotReadOnlyTools` gains `list_directory`; `libraryListTools` gains
  `listFiles`/`listDir`; `fileHintListTools` gains `listDir`; `toolFilterAnalysisKeep` gains
  `listFiles`/`listDir` (its own comment's mixed-menu rationale). Search/exec spellings stay out
  of scope. If the pinned sim contradicts a gap fix, NOTES — not silent deviation.

---

## 1. Parser strictness: a majority-marked list is an enumeration; prose is not — ✅ DONE (2026-07-06)

**Finding:** review "The lenient list parser cannot tell prose from an enumeration" (Medium,
Intent — warrants revisiting ADR 0014 §2) plus "Accept-window boundaries (exactly 2 and exactly
12) are untested" (Medium, Tests). Ground truth: ADR 0014 §2/§5 and its Realisation bounds
(steer asks ≤7; accept window 2..12, decline-whole); the parser at
`internal/mechanisms/guided_decomposition.go:466-478` (`guidedDecompositionParseList`), the
marker stripper at `:485-509` (returns the line verbatim when no marker), the case-1 intercept at
`:279-289`, the bounds check at `:336-338`, the constants at `:38-41`.

**What:** implement F4. `guidedDecompositionStripMarker` returns `(item string, marked bool)`;
`guidedDecompositionParseList` (or a sibling returning richer info) reports items plus the count
of explicitly-marked lines; the case-1 intercept (and only it — the bounds helper keeps its
single job) accepts an enumeration only when the list is in-bounds AND strictly more than half
the items were explicitly marked. Blank-line and code-fence skipping is unchanged; unmarked lines
still become items when the majority passes. Update the parser's and PreRequest/PostResponse doc
comments where they describe the plain-line leniency. Do NOT amend ADR 0014 here — item 6 owns
the consolidated dated addendum.

**Tests:** in `internal/mechanisms/guided_decomposition_test.go`: a fully-numbered list of
exactly 2 and of exactly 12 items → intercepted (one `sub_agent` call synthesized, remainder
deferred); 1 and 13 → declined (existing cases keep passing); an empty/whitespace reply →
declined; a 3-line unmarked prose reply (e.g. a clarifying question over multiple lines) under an
outstanding steer → declined whole (no synthesized call, zero decision); a mixed list (majority
marked, minority plain) → accepted with every line kept as an item; a 4-line reply with exactly
half marked → declined (strict majority).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): guided decomposition accepts only majority-marked enumerations`.

---

## 2. Anchor the remainder cursor on the delegation-bearing enumeration in the current Exchange — ✅ DONE (2026-07-06)

**NOTES (2026-07-06):** the existing follow-through/shrink fixtures (`guidedFanOutHistory` and the
inline histories in `TestGuidedDecompositionDerivesFromCallsNotCappedResults` and the exhausted-remainder
no-op) modelled the drained directive as a trailing `RoleUser` message — production-unfaithful (per F1,
`InjectContext` puts it in the system message when history ends in a tool result), and it would fall
outside the new after-last-user window. They were corrected to the directive-in-system shape (original
ask stays the last `RoleUser`); the assertions are unchanged, so the tests "keep passing" as required.

**Finding:** review "The remainder cursor anchors on the wrong 'enumeration'" (High, Intent +
Correctness, found independently twice). Ground truth: ADR 0014 Realisation ("each post-response
Turn re-derives the remainder from honest history — the model's own enumeration message and the
`sub_agent` calls"); the anchor at `internal/mechanisms/guided_decomposition.go:394-410`
(first-match over ALL assistant messages, lenient parse) and its now-false docstring claim
("scanning first-match reliably anchors on the original list"); the shared-context note above F1
(current Exchange = messages after the last `RoleUser` message); compaction summaries are
multi-line `RoleAssistant` messages (`internal/context/compact.go:107-109`).

**What:** implement F3. `guidedDecompositionEnumeration` scans only messages after the last
`RoleUser` message and returns the FIRST assistant message that both parses in-bounds (item 1's
parser) and carries at least one `sub_agent` tool call (`m.ToolCalls`). Rewrite the docstring to
state the real invariant (the enumeration message uniquely commits as list + synthesized call).
`guidedDecompositionDispatchedTasks` scopes to the same current-Exchange window so a previous
Exchange's fan-out cannot consume this one's items — keep the calls-never-results reading.

**Tests:** `internal/mechanisms/guided_decomposition_test.go` (view-driven, per the existing
patterns): a conversation whose PRIOR Exchange contains a 3-line assistant answer (in-bounds,
no calls) followed by a current-Exchange enumeration-with-call → the remainder derives from the
enumeration, not the old answer; a compaction-summary-shaped multi-line assistant message earlier
in history → ignored; an enumeration in the previous Exchange only → `guidedDecompositionRemainder`
returns nil (no cross-Exchange resumption); the existing follow-through/shrink tests keep passing.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): anchor the decomposition cursor on the delegation-bearing enumeration`.
**Depends on:** item 1.

---

## 3. Consume-once, exact-match dispatched-task accounting — ✅ DONE (2026-07-06)

**Finding:** review "Prefix matching marks duplicate or prefix-nested items as dispatched"
(Medium, Correctness). Ground truth: ADR 0014 §3 (every enumerated item gets its serialized
delegation); the matcher at `internal/mechanisms/guided_decomposition.go:452-459`
(`strings.HasPrefix(task, item)`), the task shape at `:343-346`
(`item + " " + guidedDecompositionReportHygiene`), the remainder loop at `:375-388`.

**What:** an enumeration item is consumed only by an exact match against a dispatched task —
equal to `item` or to `item + " " + guidedDecompositionReportHygiene` — and each dispatched task
consumes at most ONE item occurrence (count-aware: two identical items need two dispatches).
Preserve the §5 tolerance: an off-script model task that matches no item still leaves the
remainder intact. Update the `guidedDecompositionTaskDispatched` docstring (or replace the helper)
to name the consume-once rule.

**Tests:** duplicate items ("Add tests" twice) → one dispatch removes exactly one occurrence, the
remainder still carries the other; prefix-nested items ("Add tests", "Add tests for the CLI") →
dispatching the longer one leaves the shorter in the remainder; an off-script task matching
nothing → remainder unchanged; the existing shrink tests keep passing.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): consume-once exact matching for dispatched decomposition subtasks`.
**Depends on:** item 2.

---

## 4. Re-defer the directive on off-script tool Turns — ✅ DONE (2026-07-06)

**NOTES (2026-07-06):** rather than adding a literally separate off-script `if`, the existing
follow-through branch's guard was widened from `guidedDecompositionHasSubAgentCall(calls)` to
`len(calls) > 0`. Behaviour is identical to F2's four-condition off-script branch (a sub_agent call
was already `len(calls) > 0`; an off-script call contributes no dispatched task so it re-defers the
remainder intact; a no-tool response still fails the guard and stays the accepted no-op), with no
duplicated remainder-derivation. `guidedDecompositionHasSubAgentCall` remains in use by
`guidedDecompositionEnumeration`.

**Finding:** review "One off-script tool call mid-fan-out silently drops the queue" (High,
Intent + Correctness). Ground truth: ADR 0014 Realisation ("re-derives the remainder … and
re-defers a single directive string over the existing deferred FIFO"); the follow-through case at
`internal/mechanisms/guided_decomposition.go:294-298` (re-defers only on
`guidedDecompositionHasSubAgentCall`); the drain that already consumed the directive at
`internal/agent/loop.go:629-643` (`buildRequest` → `TakeDeferred`).

**What:** implement F2. In PostResponse, add the off-script branch: directive marker present,
`len(calls) > 0`, no `sub_agent` among them, re-derived remainder non-empty → re-defer the
directive built from that remainder (`ActionDefer`). The no-tool-call final-answer path stays a
no-op by design (F2's second half — Exchange ends; item 7 expires the queue as backstop). Update
the PostResponse doc comment's case list: the off-script drop is no longer filed under "benign
no-op".

**Tests:** loop-level in `internal/agent/guided_decomposition_test.go` (scripted-model harness):
mid-fan-out the model emits one `read_file` Turn instead of delegating → the NEXT request still
carries the directive marker with the remainder intact, and the fan-out completes; a fan-out the
model ends early with a no-tool final answer → the directive is NOT re-deferred and the Exchange
completes on that answer. Mechanism-level: the new branch fires (non-zero decision) only when all
four conditions hold — same shape minus the marker, minus tool calls, or with an empty remainder
each yields a zero decision.

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `internal/agent` (tests) +
CHANGELOG. Commit:
`fix(mechanisms): re-defer the decomposition directive across off-script tool turns`.
**Depends on:** items 2, 3.

---

## 5. Once-per-Exchange gate on committed fan-out evidence — ✅ DONE (2026-07-06)

**NOTES (2026-07-06):** the committed-evidence check landed as a single subsuming predicate
(`guidedDecompositionFanOutBegun`: any current-Exchange assistant message carries a `sub_agent`
call) rather than a literal "`sub_agent` call OR item-2 anchor" disjunction. The item-2 anchor
(`guidedDecompositionEnumeration`) requires a `sub_agent` call, so it is a strict subset of the
first clause — the single predicate silences exactly the same set of Exchanges, with no redundant
anchor re-parse. Behaviour is identical to F1's literal text.

**Finding:** review "The gate re-steers once the fan-out's markers vanish, looping the
decomposition" (High, Intent + Correctness, found independently twice — the top finding). Ground
truth: ADR 0014 §2 ("synthesis … the model's natural next Turn" — nothing sanctions a re-steer
there) and §5 (the gate stays quiet while a fan-out is in flight); the gate at
`internal/mechanisms/guided_decomposition.go:132-153`, the marker-only outstanding check at
`:167-183`; the fixture-only guard at `internal/agent/guided_decomposition_test.go:45-54`
(`gdWindow` deliberately sized so "signal B never re-steers mid-Exchange"); mid-Exchange
auto-compaction is impossible (`internal/agent/compact.go` — the `inExchange` early return), so
signal B stays true after a fan-out.

**What:** implement F1. Add a committed-evidence check to the gate: quiet when any assistant
message in the current Exchange (after the last `RoleUser` message) carries a `sub_agent` tool
call or qualifies as the item-2 anchor. Keep the marker-based outstanding check as the
same-request guard (the drained directive and an injected steer are request-scoped and invisible
to the committed-evidence scan). Update the PreRequest doc comment's precondition list.

**Tests:** loop-level: a window sized so signal B goes TRUE mid-fan-out (history over its
allocation while children report) → exactly ONE steer is injected for the whole Exchange, the
fan-out completes serially, and the final synthesis answer is NOT intercepted into a new fan-out
(no second enumeration, no `sub_agent` call appended to it); a model that calls `sub_agent`
unprompted early in an Exchange → the gate never steers that Exchange; a NEW Exchange (next user
ask) with signal A tripping → the gate fires again (re-armed). Replace the `gdWindow` fixture
comment: the suite must no longer depend on sizing the window to dodge the re-steer — that
scenario is now a covered production behaviour.

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `internal/agent` (tests) +
CHANGELOG. Commit:
`fix(mechanisms): guided decomposition steers at most once per exchange`.
**Depends on:** item 2.

---

## 6. Line-anchored, role-scoped marker detection — and the owning ADR 0014 addendum — ✅ DONE (2026-07-06)

**NOTES (2026-07-06):** `appendOrCreateSystem` (`internal/domain/hooks.go`) already newline-separates
appended injections (`Content += "\n\n" + text`) and a freshly created system message places the text
at the start, so the line-start invariant holds without a change — no fix to `internal/domain` was
needed (diff stayed in `internal/mechanisms`, `docs/adr`, CHANGELOG). Separately, the existing
`TestGuidedDecompositionNoDoubleSteer` fixture placed its marker MID-LINE in a user message
("earlier: <marker> ..."), which F5 now (correctly) no longer treats as outstanding; it was adapted to
carry the marker at a LINE START (production-faithful to a real injection), assertions unchanged — the
mid-line non-match is covered by the new `TestGuidedDecompositionMarkersLineAnchoredRoleScoped`.

**Finding:** review "Marker detection is a bare substring scan over every message and role"
(Medium, Intent + Correctness, found independently twice; the security audit confirmed
quality-only impact). Ground truth: the marker constants at
`internal/mechanisms/guided_decomposition.go:59-62`, the scans at `:172-183` and `:308-321`
(`strings.Contains` over all roles); both injected texts begin with their marker phrase (`:70-77`,
`:353-366`); the loop-level tests treat the marker strings as the Mechanism's wire contract
(`internal/agent/guided_decomposition_test.go:38-44`) — the strings must not change.

**What:** implement F5. Marker matching becomes: only `RoleUser` and `RoleSystem` messages, and
only where the marker starts a line of the content. Apply to both
`guidedDecompositionOutstanding` and `guidedDecompositionMarkerPresent`. Verify the system-append
injection branch (`appendOrCreateSystem` in `internal/domain/hooks.go`) separates appended
injections with a newline so the line-start invariant holds — STATUS QUESTION if it does not.

**(b) Owning doc amendment (this item owns it; nothing else touches ADR 0014's Realisation):**
append one dated addendum block to ADR 0014's Realisation section recording the 2026-07-06
refinements F1–F5 plus item 3's consume-once matching — what the 2026-07-05 build's
marker-only idempotency missed (the request-scoped markers vanish at the synthesis turn; the
first-match lenient anchor; the prefix-match consumption; the off-script drop) and the
committed-evidence / majority-marked / delegation-anchored / consume-once / line-anchored rules
that now realise §2/§3/§5. The Decision itself is unchanged.

**Tests:** an assistant message echoing the directive phrase mid-reply → neither
`guidedDecompositionOutstanding` nor the follow-through case treats it as a marker (gate still
free to fire in a later Exchange; no bogus follow-through); a user message whose `@file`-style
content carries the phrase mid-line → no match; the drained directive (user-role injection
starting with the marker) and the injected steer still match (existing loop-level tests keep
passing unchanged — the strings are untouched).

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `internal/domain` (only if
the newline verification requires a fix), `docs/adr/0014-...md` + CHANGELOG. Commit:
`fix(mechanisms): line-anchored role-scoped decomposition markers; ADR 0014 realisation addendum`.
**Depends on:** items 1–5.

---

## 7. Deferred Response Actions are Exchange-scoped — ✅ DONE (2026-07-06)

**NOTES (2026-07-06):** clearing the queue at `completeTurn`'s `StatusExchangeComplete` branch (item
7(a), literal) reverses the pre-F6 behaviour a pre-existing loop test asserted — a post-response
`ActionDefer` on a no-tool FINAL answer used to ride into the NEXT Exchange's request. That is exactly
the cross-Exchange leakage F2 names as "the exact defect F6 closes", so
`TestStep_DeferredCorrectionSurvivesSnapshot` was repurposed to
`TestStep_DeferredCorrectionExpiresAtExchangeEnd` (asserting the correction is cleared at the Exchange
boundary and never rides the next Exchange). Within-Exchange defer delivery across snapshot/resume is
still covered by `TestGuidedDecomposition_SnapshotMidFanOutRoundTripsDirective`. Diff stays within the
item's `internal/agent` scope.

**Finding:** review "A stale fan-out directive survives into the next Exchange after a fault or
abort" (High, Correctness — verified directly in the main review pass) plus "cancelTurn restores
the old directive but keeps the one deferred by the cancelled turn itself" (Medium, Correctness).
Ground truth: CONTEXT.md "Deferred Response Action" (a decision consumed by the *next request* of
the same flow); the fault path at `internal/agent/loop.go:291-306` (`turnFailed` →
`restoreDeferred` + `abandonTurn`), Exchange ends at `completeTurn` (`:564-577`) and
`abandonTurn` (`:584-595`); `cancelTurn` at `:608-618`; `restoreDeferred` at `:620-626`;
`AbortExchange` at `internal/agent/agent.go:163-170` (drops messages, never touches the queue);
the queue at `internal/domain/hooks.go:745` (`Defer`) / `:759` (`TakeDeferred`); post-response
hooks defer via the `ActionDefer` dispatch in `internal/domain/hookrun.go` (re-locate by symbol).

**What:** implement F6.
**(a)** Add a clear operation on the conversation's deferred queue (e.g. `ClearDeferred` on
`domain.Conversation`) and call it wherever an Exchange ends: `completeTurn`'s
`StatusExchangeComplete` branch, `abandonTurn`, and `AbortExchange`.
**(b)** `cancelTurn`: capture the queue length before the post-response hooks run (thread it
alongside the existing `rollback`/`deferred` values), truncate the queue back to that length
before `restoreDeferred` — the cancelled Turn's own deferrals die with the Turn; the drained
injections are restored exactly once.
**(c) Owning doc amendment:** one sentence in CONTEXT.md's "Deferred Response Action vs
Request-prep Hint" entry recording the Exchange-scoped lifetime (cleared at Exchange end, rolled
back with a cancelled Turn).

**Tests:** loop-level: fault (terminal `DeltaError`) mid-fan-out, then a new Submit → the new
Exchange's first request carries NO directive marker and answers the new ask; Esc-path
`AbortExchange` mid-fan-out, then Submit → same; cancel during a `sub_agent` dispatch, then
re-Step → the re-attempted request carries exactly ONE directive (the restored drained one, not
two contradictory copies), and a snapshot taken at the cancelled boundary round-trips with the
same single directive; a normal fan-out is unaffected (the existing end-to-end acceptance keeps
passing — the directive there is consumed by the drain each Turn, never alive at Exchange end
except through item 4's no-re-defer final-answer path, which this clears).

**Acceptance:** gates green; diff confined to `internal/agent`, `internal/domain`, `CONTEXT.md` +
CHANGELOG. Commit:
`fix(agent,domain): expire deferred response actions at the exchange boundary`.
**Depends on:** item 4 (its final-answer no-re-defer path is this item's test surface).

---

## 8. Declare `guided_decomposition` incompatible with `truncate_history` — ✅ DONE (2026-07-06)

**NOTES (2026-07-06):** `guided_decomposition` has NO row in `docs/design/mechanism-catalogue.md`
Table A (Tables A/B/C catalogue the sim-*ported* Mechanisms; guided_decomposition is a new ADR 0014
Mechanism, never sim-ported), so 8(b)'s "matching Table A cell" was applied to the counterpart that
IS in Table A — the `truncate_history` row's Ordering/incompatibility cell now records
`IncompatibleWith guided_decomposition`. The relation is thereby reflected in Table A as intended;
no new guided_decomposition row was added (out of scope for this item).

**Finding:** review "Co-enabling truncate_history destroys the fan-out cursor mid-flight"
(Medium, Correctness + Tests). Ground truth: the descriptor at
`internal/mechanisms/guided_decomposition.go:98-104` (`IncompatibleWith: [decompose]`);
`truncateHistoryID` at `internal/mechanisms/truncate_history.go:20`; one-sided declaration
suffices per `detectIncompatibility` (`internal/domain/registry.go:191-209`); composition ground
truth is `docs/design/mechanism-catalogue.md` Table A; the stacking relations are ADR 0014 locked
decisions (Realisation, "The stacking relations landed as declared").

**What:** add `truncateHistoryID` to `guidedDecompositionDescriptor.IncompatibleWith`, with a
comment naming the reason (a mid-Exchange truncation can drop the enumeration message the cursor
re-derives from). **(b)** Update the matching Table A cell in
`docs/design/mechanism-catalogue.md`. **(c)** Append a dated one-line note to ADR 0014's
Realisation recording the added relation (this item owns that line; item 6's addendum covers
F1–F5 and must not — coordinate wording, not ownership).

**Tests:** `internal/mechanisms/catalogue_test.go` (or registry-level): enabling
`guided_decomposition + tool_result_cap + truncate_history` is refused with
`ErrIncompatibleMechanisms` naming the pair; the valid stack
`guided_decomposition + tool_result_cap` still constructs; `CataloguedMechanisms()` reflects the
new relation (descriptor row and instance stay matching — the existing static-row test should
catch a mismatch; extend it if it does not).

**Acceptance:** gates green; diff confined to `internal/mechanisms`,
`docs/design/mechanism-catalogue.md`, `docs/adr/0014-...md` + CHANGELOG. Commit:
`fix(mechanisms): declare guided_decomposition incompatible with truncate_history`.

---

## 9. Test: sub-agent spawn under the production `EnableMechanisms` arm

**Finding:** review "No test exercises sub-agent spawn under the production `EnableMechanisms`
arm" (High, Tests). Ground truth: ADR 0015 Realisation ("a spawned sub-agent inherits the
parent's already-built registry … and clears `EnableMechanisms`"); the inheritance at
`internal/agent/subagent.go:118-119`; the existing harnesses in
`internal/agent/enable_mechanisms_test.go` (arms via `EnableMechanisms`, never dispatches
`sub_agent`) and `internal/agent/guided_decomposition_test.go` (dispatches `sub_agent`, arms via
a pre-built registry — `gdConfig`).

**What (test-only):** one loop-level test in `internal/agent`: a parent whose Config arms
`EnableMechanisms = {"guided_decomposition", "tool_result_cap"}` (registry left nil so the engine
builds it) with the `sub_agent` recursion point registered; the scripted model performs one
delegation. Assert: the spawn returns no error (reverting the `EnableMechanisms` clear at
`subagent.go:119` must make this test fail with the duplicate-ID rejection — verify the failure
mode once while writing, then leave the assertion on the success path); the child's events nest
at `Depth == 1`; the child fires a catalogued Mechanism from the shared registry (e.g. assert a
child-side mechanism fire or the parent-observed audit/event trail shows the child ran with the
inherited stack). Mirror the same arm through `Resume` if the harness makes it cheap (the ADR
names `New`/`Resume` as one path); otherwise a NOTES line saying why not.

**Acceptance:** gates green; diff confined to `internal/agent` test files + CHANGELOG (one line).
Commit: `test(agent): sub-agent spawn inherits the EnableMechanisms-built registry`.

---

## 10. Test: enable-surface edges — corrupt-store degrade and the descriptor clone contract

**Finding:** review "Corrupt-store degrade branch at construction is untested" +
"`CataloguedMechanisms()`'s documented clone contract is untested" (both Medium, Tests). Ground
truth: ADR 0015 Realisation ("a corrupt or absent store degrades to an empty store with wire.go's
exact `os.Stderr` notice"); the branch at `internal/agent/loop.go:129-160`
(`buildEnabledMechanisms` — the degrade notice inside the `libraryMechanismID` block);
`Store.Load` returns nil for an absent file (so existing temp-dir tests only cover the happy
path); the clone at `internal/mechanisms/catalogue.go` (`cloneDescriptor`, `slices.Clone`) and
the public query `apogee.go:305-313`.

**What (test-only):**
**(a)** `internal/agent`: seed `LibraryDir/library.json` with garbage bytes, arm
`EnableMechanisms = ["library"]` → construction succeeds, the stderr notice appears once
(capture `os.Stderr` non-parallel, the `wire_test.go` precedent), and the `library` Mechanism
runs over an empty store (no injection fires from the corrupt content).
**(b)** root `apogee_test.go`: take `CataloguedMechanisms()`, mutate a returned descriptor's
`Requires` (and `IncompatibleWith`) slice element, query again → the fresh result is unchanged
(the static catalogue was not reached).

**Acceptance:** gates green; diff confined to `internal/agent` and root test files + CHANGELOG
(one line). Commit:
`test(agent,apogee): pin the corrupt-store degrade and descriptor clone contracts`.

---

## 11. Read/list spelling families — consolidate, fix the diverged gaps, repoint stale wiring comments

**Finding:** review "Read/list tool-name sets: five hand-maintained copies, already diverged"
(Medium, Structure — this drift class shipped defects in the two previous review rounds) plus
"Stale wiring pointers after the ADR 0015 wire.go collapse" (Medium, Structure). Ground truth:
the write-side precedent `wave4WriteTools` (`internal/mechanisms/decompose.go:115` — the single
source `isFileMutatingTool`/`isWriteTool` read, `robustness.go:94-98`); the sets:
`readToolNames` (`offramps.go:47-52`), `cotReadOnlyTools`/`cotReadTools` (`cot.go:77-89`),
`libraryListTools`/`libraryReadTools` (`library.go:74-80`), `listToolNames`
(`historyhints.go:32-35` — the complete five-spelling list family), `toolFilterAnalysisKeep`
(`toolfilter.go:50-54`), `fileHintListTools`/`fileHintReadTools` (`filehint.go:46-49`);
membership rationales are the sets' own `@pin` comments against the pinned sim @ `d220867`; the
stale comments at `internal/mechanisms/catalogue.go:20-32` (`Deps.Library` "wire.go constructs
and Loads"), `:46-52` (`GrammarConstraint` "wire.go leaves this false") and
`internal/mechanisms/library.go:22` — the single build path is now `buildEnabledMechanisms`
(`internal/agent/loop.go:129-160`); the parked TODO.md entry (~`:151-162`) records the read-trio
consolidation design.

**What:** implement F8.
**(a)** Hoist two spelling families next to `wave4WriteTools` (same file or a sibling detection
file): the read trio and the five list spellings. Compose every set above from the families —
per-source membership stays local and documented (do NOT merge sets with different purposes; do
NOT touch search/exec spellings).
**(b)** The four ratified gap fixes land as part of the composition: `cotReadOnlyTools` +
`list_directory`; `libraryListTools` + `listFiles`/`listDir`; `fileHintListTools` + `listDir`;
`toolFilterAnalysisKeep` + `listFiles`/`listDir`. Check each against the pinned sim source its
comment names; a contradiction → NOTES, not silent deviation.
**(c)** Repoint the three stale wiring comments at the engine's build path.

**Tests:** one table-driven test per gap fix pinning the newly covered spelling through the
mechanism's observable behaviour (the `write_detection_test.go` precedent): a `list_directory`
turn keeps cot's read-only streak alive; a `listFiles`/`listDir` turn triggers the library
shallow-exploration observation; a `listDir` listing opens a filehint opportunity; a
`listFiles`/`listDir` tool survives toolfilter's analysis-keep. Each test must FAIL if its gap
fix is reverted (the mutation discipline of the third-review wave — verify once while writing,
state so in NOTES only if a site proves structurally untestable, the offramps `:98` precedent).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`refactor(mechanisms): single-source read/list tool spelling families; fix the diverged sets`.

---

## 12. Docs close-out (owning item for the residue)

**What:** the cross-cutting residue with exactly one owner (items above carry their own
code-adjacent CHANGELOG lines and doc amendments in their commits).
**(a) CHANGELOG:** sanity-check every item 1–11 landed its line under `## [Unreleased]`; add any
missing one-liner; do not create a version heading (tagging is a release decision, not this
plan's).
**(b) TODO.md:** update the parked read-tool-set consolidation entry (~`:151-162`): the spelling
families landed in item 11 — either close the entry or narrow it to whatever structural
re-shaping remains for `/improve-codebase-architecture` (per item 11's actual NOTES).
**(c) ISSUES.md:** if it tracks any finding this plan fixed, close it; otherwise leave untouched.
**(d) Review report:** append a short dated "fixed by `docs/plans/post-v1.3.0-review-fixes-plan.md`"
note at the top of `docs/code-review-2026-07-06.md` so the findings record points forward.

**Acceptance:** gates green (docs-only otherwise); `git status` clean after commit. Commit:
`docs: close out the post-v1.3.0 review fixes`.
**Depends on:** items 1–11.

---

## Explicitly NOT in this plan

- **Any relaxation of the guided-decomposition gate or bounds** — the steer/accept constants,
  the Depth==0 gate, and the measured-signals-only rule are ADR 0014 locked decisions; this plan
  hardens the substrate under them, it does not retune them (tuning is the bench's).
- **The bench A/B campaign and any default-ON flip** — still bench-evidence-gated (ADR 0009).
  This plan is the prerequisite the review named: bench the `guided_decomposition +
  tool_result_cap` stack only after items 1–7 land.
- **Structural re-shaping of the mechanism package** (a shared detection module beyond the two
  spelling families, unifying the marker machinery into a framework) — flagged for a future
  `/improve-codebase-architecture` pass; item 11 deliberately stops at families + gap fixes.
  *(2026-07-06: that pass has run — the follow-up is `docs/plans/architecture-deepening-plan.md`,
  BLOCKED on this plan completing first; it treats this plan's tests as its behaviour contract.)*
- **Mid-Exchange auto-compaction at quiescent Turn boundaries** — the parked TODO.md/ADR 0014
  consequence stands; F1 closes the re-steer without it.
- **The suppression-abandonment executable proof** — the review dropped it as uncertain and
  un-cross-validated; the design makes abandonment emergent, and item 7's Exchange-boundary
  clearing narrows the residual drain window further.
- Re-running the full review — the closeout backstop is `make check` + this plan's per-item
  tests.
