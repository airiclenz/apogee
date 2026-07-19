# Plan — Architecture deepening (the Exchange seam, the Mechanism-author surface, the tool-definition module)

**Date:** 2026-07-06
**Status:** READY — the blocking `post-v1.3.0-review-fixes-plan.md` completed and was archived
(`docs/plans/archived/`, 2026-07-07). That plan's tests are this plan's behaviour contract.
**How to run:** `implement-plan docs/plans/architecture-deepening-plan.md with skills: coding-standards`
**Source of direction:** the 2026-07-06 architecture review
(`docs/architecture-review-20260706-205911.html`) — five candidates from a three-explorer pass;
this plan implements the two `Strong` candidates ("Give the Exchange a home", "Deepen the
Mechanism author's interface") and the two `Worth exploring` ones (tool-definition module,
Mechanism test-surface adapter). The review is a findings record, not ground truth — if the code
contradicts a finding's premise (already fixed, symbol moved), re-verify against the code and say
so in NOTES rather than "fixing" a non-defect.
**Binding ground truth:** ADR 0010 (layering: `internal/*` never imports root; a type lives at
the lowest layer that can define it; domain owns pure logic on domain types), ADR 0014 incl. its
Realisation and the fixes plan's ratified F1–F8 refinements (esp. F1 committed-evidence gating and
F6 Exchange-scoped deferrals — this plan concentrates those decisions, it must not alter them),
ADR 0007 (quiescent boundary / snapshot-resume), ADR 0002 (tools open, Mechanisms curated),
CONTEXT.md for every term.
**Verify gate (every item):** `make check` (gofmt-clean, vet, build, race tests, ADR-0010
invariant) plus the item's own test commands. "Gates green" means exactly this.
**CHANGELOG:** each item adds its line under `## [Unreleased]` (create it above the newest
release heading on first need, following the file's format). Items 4–6 note the public-surface
effect explicitly (new `Budget` methods are additive → minor; everything else is internal).

**Behaviour contract (the refactor discipline):** every item below is behaviour-preserving
unless its text names an authorized delta. "Behaviour-preserving" is proved by the existing
suite: the fixes plan's tests and the pre-existing package tests must pass **unchanged** (test
edits allowed only where an item authorizes them, each with a NOTES line). A refactor item that
finds itself needing to change an unauthorized assertion has discovered a behaviour change —
STATUS QUESTION, do not proceed.

**Precedence (points at ground truth, never at an artifact of this plan):** ADRs and CONTEXT.md
win over the Design record below; the Design record (D1–D7) wins over an item's prose; where any
two seem to conflict, flag STATUS QUESTION rather than deviate. Every `file:line` below was
verified 2026-07-06 against `2a33c26` — but the fixes plan runs first and WILL shift lines, and
earlier items of this plan shift them further: **always re-locate by symbol name before
editing**; treat the fixes plan's F1–F8 (ratified once that plan ran) as the shape of the code
you will actually find at the named symbols.

**Deviation trail:** any authorized deviation from an item's text lands as a dated NOTES line
under that item — no silent divergence.

**Before running:** confirm the fixes plan is fully checked off, the tree is clean, and this plan
file plus the architecture review HTML are committed (suggested:
`docs: add the architecture deepening plan and file the 2026-07-06 architecture review`).

---

## Design record (from the 2026-07-06 architecture review; running this plan ratifies D1–D7 — flag STATUS QUESTION rather than deviate)

- **D1 — The Exchange becomes a domain working value with ONE boundary derivation.** The
  current Exchange is derived, not cached: its opening is **the index of the last `RoleUser`
  message** ("current Exchange = messages strictly after it", the fixes plan's shared F-context —
  stable across `InjectContext` insertions, which land before that message or in the system
  message). The engine's cached `a.exchangeStart` is the same number by construction
  (`loop.go:255` sets it to `conv.Len()` immediately before appending the opening user message),
  so one derivation can serve both the engine (committed conversation) and the hooks (request
  view). Home: `internal/domain` — a new `ExchangeView` value + `CurrentExchange(...)`
  constructor over a minimal `Len()/At(i)` read surface (satisfied by both `Conversation` and the
  unexported `conversationView`). **Not** aliased at the root: its consumers are internal
  (Mechanisms are curated, ADR 0002); export is a later, deliberate minor bump when an external
  consumer exists. The public `LoopView` / `ConversationView` **interfaces gain no methods** —
  they are unsealed and externally implementable (the `LoopView` docstring itself anticipates
  test fakes), so an added method is a breaking change; the domain constructor over the existing
  read surface is the semver-safe shape.
- **D2 — The engine derives the boundary; the cached field and its repair math go.** With D1,
  `a.exchangeStart` is redundant while an Exchange is open: `AbortExchange`'s rollback target and
  the S2 repair after a mid-Exchange history rewrite (`loop.go:281-283` — the
  `min(max(exchangeStart-dropped, PrefixEnd()+1), Len())` arithmetic) are both subsumed by
  re-deriving "index of the last `RoleUser` message" from the post-rewrite conversation.
  `a.inExchange` **stays** — open-vs-closed is genuine state, not derivable (a closed Exchange
  still has a last user message), and it is serialized. The snapshot schema: `ExchangeStart`
  stops being written and is **ignored on read** (tolerant decode keeps old snapshots resumable;
  `InExchange` continues to round-trip). Precondition this rests on: no history rewrite may drop
  the open Exchange's opening user message (truncate_history keeps the tail; compaction is
  Exchange-boundary-only) — the implementer verifies this against the registered history-rewrite
  Mechanisms and files STATUS QUESTION if any rewrite can violate it, in which case the cached
  field stays and D2 shrinks to routing its readers through one helper.
- **D3 — Exchange end has one engine-side owner.** The fixes plan's F6 placed deferral clearing
  at three Exchange-end sites plus the cancel rollback. This plan concentrates the three ends
  into one private `closeExchange` on the Agent — flip `inExchange`, clear the deferred queue,
  one docstring owning the "a deferral dies with its Exchange" invariant — called from
  `completeTurn`'s `StatusExchangeComplete` branch, `abandonTurn`, and `AbortExchange`.
  `cancelTurn` stays distinct by design (the Exchange remains open; it truncates-then-restores
  per F6(b)). Pure concentration: same observable behaviour, one place to read it.
- **D4 — Token arithmetic gets one implementation, on `Budget`.** Three chars→token conversions
  exist today and already disagree: `context.TokenEstimator.EstimateTokens`
  (`int(math.Ceil(chars/ratio))`, non-positive ratio → `DefaultCharsPerToken` fallback),
  `guidedDecompositionEstimateTokens` (`int(chars/ratio)` truncation, non-positive ratio → 0,
  deliberately inert), `libraryCapToBudget` (non-positive ratio → its own default constant).
  The single implementation lands as methods on the **`Budget` struct** (a struct, root-aliased —
  added methods are additive, minor): `Budget.EstimateTokens(chars int) int` (ceil rounding — the
  estimator's calibrated ground truth; returns 0 when `CharsPerToken <= 0`, the zero-value-Budget
  guard that preserves guided decomposition's inertness) and
  `Budget.HistoryExceedsAllocation(msgs []Message) bool` (the `context.HistoryExceedsAllocation`
  compare against `Budget.History`). The pure character measure (`context.PromptChars`) moves to
  `internal/domain` per ADR 0010's lowest-layer rule (it reads only `domain.Message`/`ToolDef`);
  `internal/context` keeps `TokenEstimator` (calibration is context's) and delegates its math to
  the domain implementation so the two paths cannot drift. **Authorized behavioural delta:**
  guided decomposition's signal thresholds move from truncation to ceil rounding — at most one
  token of difference per comparison; boundary-exact test fixtures may be adjusted (NOTES each).
  Caller-side fallbacks that are semantics, not arithmetic (library's default-ratio substitution)
  stay at the caller.
- **D5 — History-scan helpers are a mechanisms-internal module beside the spelling families.**
  The repeated `conv.Range(...)` scan idioms (readloop's greenfield/read-loop path counting,
  readrepeat's recent-successful-reads, filehint's equivalents) consolidate into package-level
  helpers in `internal/mechanisms`, in the same file (or a sibling) as the F8 spelling families
  they compose with. Same F8 spirit: the *scan* is shared, per-Mechanism *membership and
  thresholds* stay local. Internal only — no domain or public-surface change.
- **D6 — The hook seam gets its second adapter: a shared test-support package.** New
  `internal/domain/domaintest` (the `internal/platform/confinetest` precedent): a fluent
  `ConversationBuilder` (`.User(text)`, `.AssistantText(text)`, `.AssistantCalls(calls…)`,
  `.ToolResult(callID, content)`, `.Messages()`), canned `ToolCall` builders, and a
  `FakeLoopView` implementing `domain.LoopView` with settable budget / depth / fired counts
  (internal implementers of the public interface carry no semver cost). One implementation:
  the existing package-local helpers in `internal/mechanisms` (`readCall` / `userMsg` /
  `assistantText` / `assistantCall` in `offramps_test.go:24-39`) become thin delegates so no
  test rewrites are forced; new tests use `domaintest` directly.
- **D7 — Tools keep raw JSON schemas; the ritual around them collapses.** No schema *generation*
  (a reflection generator would trade a visible, reviewable schema string for magic). The
  repetition that goes: every tool's three metadata methods (`Name`/`Description`/`Schema`) fold
  into an embedded spec value, and the decode-and-error preamble folds into one generic helper
  (`decodeArgs` + `errorResult` already exist — they gain a typed wrapper that returns the
  arg-error `ToolResult` shape in one place). `domain.Tool` and every tool's observable behaviour
  (names, schemas, results, error strings) are unchanged — the existing per-tool tests prove it
  by passing untouched.

---

## 1. ADR 0017 + CONTEXT.md — the Exchange working value and the deferral lifecycle, on the record — ✅ DONE (2026-07-19)

**Finding:** review candidate 1 ("Give the Exchange a home", Strong): the Exchange is a
first-class CONTEXT.md term with no module — its boundary is re-derived ad-hoc in seven places
(`internal/agent/loop.go:255,281-283`, `internal/agent/agent.go:167` (`AbortExchange`),
`internal/agent/compact.go:126`, `internal/domain/hooks.go` (`InjectContext`'s
`lastIndex(RoleUser)`), `internal/domain/hookview.go:50` (`LastUser`),
`internal/mechanisms/guided_decomposition.go` (the current-Exchange scans F1/F3 added)), and
Exchange-lifetime state was request-scoped or Session-scoped until F6. Ground truth for the
decisions being recorded: D1–D3 above; ADR 0014 Realisation as amended by the fixes plan
(committed-evidence gating; the deferral clearing sites); CONTEXT.md "Exchange" and "Deferred
Response Action vs Request-prep Hint" (the latter already carries F6's Exchange-scoped-lifetime
sentence from fixes item 7(c)).

**What (docs only — this item owns every cross-cutting doc amendment of items 2–4):**
**(a)** Author `docs/adr/0017-the-exchange-is-a-derived-domain-working-value.md` (0016 has
since been taken by the validated-sets ADR) (Status:
accepted): Context (the seven derivation sites; the F1/F3/F6 defect cluster whose root cause was
Exchange-scoped state without an Exchange module; the review's independent triple finding),
Decision (D1's derived-not-cached boundary and its `internal/domain` home; D2's engine
derivation replacing the cached field + repair math, `inExchange` retained, snapshot
`ExchangeStart` ignored-on-read; D3's single `closeExchange` owner; the explicit **non**-export
of `ExchangeView` at the root until an external consumer exists), Consequences (hooks and engine
share one boundary definition; the ADR 0014 "re-derive from honest history" posture is
implemented in one place, not per-Mechanism).
**(b)** CONTEXT.md: extend the **Exchange** entry with two sentences naming the code home (the
Exchange is derived from the conversation — the messages strictly after the last user message —
as a domain working value consumed by the loop and by Mechanisms; its boundary is never cached
state). Do not touch the "Deferred Response Action" entry — F6's sentence from the fixes plan
already covers the lifetime; if it is missing (fixes item 7(c) deviated), STATUS QUESTION.

**Acceptance:** gates green (docs-only); the ADR cross-links ADR 0007/0010/0014 and the fixes
plan. Commit: `docs: ratify ADR 0017 — the exchange is a derived domain working value`.

---

## 2. `internal/domain/domaintest` — the hook seam's test adapter — ✅ DONE (2026-07-19)

**Finding:** review candidate 4 ("A test-surface adapter for Mechanisms", Worth exploring): the
hook interface is the test surface, but its test adapter does not exist — conversation fixtures
are hand-built per test file (`internal/mechanisms/offramps_test.go:24-39` defines the
package-shared `readCall`/`userMsg`/`assistantText`/`assistantCall`; `internal/agent`'s
harnesses build their own), and there is no fake `LoopView`. Ground truth: D6;
`internal/platform/confinetest` as the house test-support-package precedent; the `LoopView`
docstring (`internal/domain/hooks.go:218-243` — "a view built without a depth (a test fake …)
reports 0") already anticipating fakes.

**What:** create `internal/domain/domaintest` per D6: `ConversationBuilder` (fluent, returning
`[]domain.Message`), canned call/result builders (at minimum the `read_file` call shape
`offramps_test.go` uses), and `FakeLoopView` (implements `domain.LoopView`; zero value usable;
setters/fields for `Budget`, `Depth`, `Fired` counts, and the conversation). Convert the four
`internal/mechanisms` helpers into thin delegates to `domaintest` (signatures unchanged — no
existing test rewrites). Do **not** sweep other test files onto the builder; later items and
future tests adopt it naturally.

**Tests:** `domaintest` gets its own small suite: the builder produces the exact message shapes
the delegating helpers produced before (assert equality against literal `domain.Message`
values); `FakeLoopView` satisfies `domain.LoopView` (compile-time assertion) and reports set
values. The full `internal/mechanisms` suite passes unchanged (the delegation is invisible).

**Acceptance:** gates green; diff confined to `internal/domain/domaintest` (new),
`internal/mechanisms` test files (delegation only) + CHANGELOG. Commit:
`test(domain): domaintest conversation builder and fake loop view — the hook seam's second adapter`.

---

## 3. `domain.ExchangeView` — one boundary derivation, no callers yet — ✅ DONE (2026-07-19)

NOTES (2026-07-19): both `*Conversation` and `conversationView` already satisfied the
`Len()/At(i)` interface, so the "trivial adapter" clause was moot for them; a `messageSlice`
adapter was added instead so `lastIndex` (and through it `InjectContext`/`LastUser`) routes into
the one core, `lastRoleIndex`. Tests are package-internal (`package domain`) because the
property pin needs the unexported `conversationView`; `domaintest` is therefore unused — package
`domain` importing it would cycle.

**Finding:** review candidate 1, the domain half: the boundary derivation exists in at least
four spellings (`lastIndex(r.messages, RoleUser)` in `InjectContext`
(`internal/domain/hooks.go:408` region), `conversationView.LastUser`
(`internal/domain/hookview.go:49-54`), the engine's cached `exchangeStart`, and guided
decomposition's post-F1/F3 current-Exchange scans). Ground truth: D1; ADR 0017 (item 1); the
fixes plan's shared F-context (boundary = last `RoleUser` message, stable across injections —
its "if a message shape violates this, STATUS QUESTION" caveat applies here verbatim).

**What:** in `internal/domain`, add the `ExchangeView` working value and constructor per D1:
`CurrentExchange(c messageReader) ExchangeView` over a minimal `Len()/At(i)` interface
(unexported; satisfied by `*Conversation` and `conversationView` — add the trivial adapter for
whichever does not already satisfy it). Surface (keep it minimal; extend only when a caller
exists): `Found() bool` (a user message exists), `UserIndex() int` (the opening user message),
`After() []Message` (copies — the messages strictly after it), and `RangeAfter(fn)` (the
allocation-free walk). Reuse the existing `lastIndex` logic — after this item there is exactly
one implementation of the derivation inside `domain` (`InjectContext` and `LastUser` route
through it or its shared core; their public behaviour is unchanged). No root alias (D1). No
engine or mechanisms callers yet — this item is pure, additive, and independently testable.

**Tests:** table-driven in `internal/domain`: empty conversation → `Found()==false`; single
user message → `UserIndex()==0`, empty `After()`; the mid-Exchange shape from the F-context
(`user, assistant(calls), tool results, assistant`) → `After()` returns exactly the three
trailing messages; multiple Exchanges in history → the LAST user message anchors; an injected
user-role message inserted before the last user message (the `InjectContext` shape) does NOT
move the boundary. Property pin: for every fixture, `UserIndex()` equals what
`conversationView.LastUser` reports (the two derivations may not drift).

**Acceptance:** gates green; diff confined to `internal/domain` + CHANGELOG (internal — no
public-surface line). Commit:
`feat(domain): ExchangeView — the exchange boundary derived in one place`.
**Depends on:** items 1 (the ADR names the shape), 2 (tests may use domaintest).

---

## 4. The engine consumes the derivation: drop the cached boundary, concentrate Exchange end — ✅ DONE (2026-07-19)

NOTES (2026-07-19): the 4(a) invariant verification FAILED — `truncate_history` DOES drop the
open Exchange's opening user message whenever the open Exchange already holds >=
keepLastTurns(4) assistant messages (the keep-tail cut lands inside the Exchange; the user-role
gap note then anchors the last-`RoleUser` derivation, which would over-drop the note on abort —
pinned by `TestExchangeStartRepairedAfterMidExchangeTruncation`,
`internal/agent/autocompact_guard_test.go`). Per D2's pre-registered fallback (owner decision,
option (a)): the cached `exchangeStart` field and the S2 repair KEPT; D2 shrunk to routing the
field's readers (`AbortExchange`, `encodeState`) through the one `exchangeBoundary()` helper;
4(b) did not proceed (`ExchangeStart` keeps round-tripping — it is load-bearing, and the new
`TestSnapshot_RoundTripsExchangeBoundaryForAbort` pins the round-trip + post-resume abort);
4(c) landed as specified. The item's "old repair math" abort test already exists as the pinning
guard test above, so no duplicate was added. Diff extends beyond `internal/agent` + CHANGELOG:
ADR 0017 §2 got a dated realisation note recording the taken fallback, and CONTEXT.md's
Exchange entry no longer claims the engine holds no cached boundary.

**Finding:** review candidate 1, the engine half: `a.exchangeStart` is a cached copy of a
derivable number, and keeping it correct costs the S2 repair arithmetic
(`internal/agent/loop.go:281-283`) plus snapshot plumbing (`internal/agent/state.go:41-42,53-54,
77-78`); Exchange end is three call sites each re-stating the F6 invariant. Ground truth: D2,
D3; ADR 0017; ADR 0007 (snapshot/resume at the quiescent boundary — resumability must be
preserved); the engine sites: `step()`'s opening (`loop.go:250-264`), the S2 repair
(`loop.go:281-283`), `completeTurn` / `abandonTurn` / `cancelTurn` (`loop.go`, re-locate by
symbol — the fixes plan's item 7 added the deferral clearing/truncation there), `AbortExchange`
(`internal/agent/agent.go:163-170`), the compaction gate (`internal/agent/compact.go:126`), and
the state schema (`internal/agent/state.go`).

**What:**
**(a)** Per D2: replace reads of `a.exchangeStart` with the item-3 derivation over `a.conv`
(`AbortExchange`'s `DropRange` target; anything else `grep -n exchangeStart internal/agent`
finds), delete the field, the `step()` assignment, and the S2 repair block (the derivation is
correct by construction after a rewrite — record in the deleted block's place a one-line comment
naming the invariant it rests on: no history rewrite drops the open Exchange's opening user
message). **First** verify that invariant against every registered history-rewrite Mechanism
(truncate_history's keep-tail window; compaction's Exchange-boundary-only gate) — if any rewrite
can violate it, STATUS QUESTION per D2's fallback.
**(b)** State schema: stop writing `ExchangeStart`; keep the struct field with a comment
(ignored on read — old snapshots stay resumable); `InExchange` unchanged.
**(c)** Per D3: introduce `closeExchange` and route `completeTurn`'s `StatusExchangeComplete`
branch, `abandonTurn`, and `AbortExchange` through it (flag flip + deferral clear + the owning
docstring). `cancelTurn` untouched beyond what fixes item 7(b) landed.

**Tests:** the existing `statemachine_test.go`, `autocompact*_test.go`, and the fixes plan's
item-7 Exchange-boundary tests pass **unchanged** — they are the behaviour contract for exactly
this refactor (white-box assertions that name the deleted `exchangeStart` field are the one
authorized edit class: re-express them against the derivation, NOTES each). Add: a mid-Exchange
`truncate_history` rewrite followed by `AbortExchange` rolls back to the same boundary the old
repair math produced (pin the S2 scenario the deleted arithmetic served — fixture shape from the
S2 comment); a snapshot written by the OLD schema (fixture JSON with `exchangeStart` set)
resumes with the Exchange open and aborts correctly.

**Acceptance:** gates green; diff confined to `internal/agent` + CHANGELOG. Commit:
`refactor(agent): derive the exchange boundary; closeExchange owns exchange end`.
**Depends on:** item 3.

---

## 5. Guided decomposition reads the Exchange through the seam

**Finding:** review candidate 1, the Mechanism half: after the fixes plan, guided
decomposition's gate and cursor are correct but still privately re-derive the current Exchange
("messages after the last `RoleUser`") inside `guidedDecompositionEnumeration`,
`guidedDecompositionDispatchedTasks`, and the F1 committed-evidence check (re-locate all by
symbol — fixes items 2 and 5 reshaped them). Ground truth: D1; ADR 0014 Realisation as amended
(committed evidence, delegation-bearing anchor); the fixes plan's tests for items 1–5 (the
behaviour contract — they must pass unchanged).

**What:** migrate the three sites onto `domain.CurrentExchange` (via the hook's
`ConversationView` — the minimal read interface from item 3 must accept it; if the view
plumbing needs a small adapter, it lives in `internal/mechanisms`, not `domain`). Delete the
Mechanism-local boundary derivation. Marker handling, parsing, and every threshold stay exactly
as the fixes plan left them.

**Tests:** the entire `guided_decomposition` suite (mechanism-level and the loop-level
`internal/agent/guided_decomposition_test.go`) passes **unchanged** — zero new behaviour. One
new test: the enumeration anchor and the dispatched-task window agree with
`domain.CurrentExchange` on a fixture with two Exchanges of history (the derivations may not
drift now that they share an implementation — assert via the helper's outputs, not internals).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`refactor(mechanisms): guided decomposition derives the exchange through domain.CurrentExchange`.
**Depends on:** items 3, 4.

---

## 6. One token-arithmetic implementation, on `Budget`

**Finding:** review candidate 2 ("Deepen the Mechanism author's interface", Strong), the
arithmetic half: three divergent chars→token conversions
(`internal/context/budget.go:118-124` (`TokenEstimator.EstimateTokens`, ceil + default-ratio
fallback), `internal/mechanisms/guided_decomposition.go:244-248` (truncation + inert-on-zero),
`internal/mechanisms/library.go:446-449` (caller default substitution)), a fourth site doing the
inverse (`internal/mechanisms/tool_result_cap.go:117-123`), and the canonical allocation check
(`internal/context/budget.go:175-179` (`HistoryExceedsAllocation`)) unreachable from a hook —
the engine wraps it privately (`internal/agent/compact.go:143`,
`internal/agent/loop.go:745-761` (`Agent.budget`)). Ground truth: D4; ADR 0010 (pure logic on
domain types belongs in domain); CONTEXT.md "Budget" ("the single authority"); the estimator's
calibration semantics (`internal/context/budget.go:88-143`).

**What:**
**(a)** Move the pure math to `internal/domain`: `PromptChars` (verbatim relocation from
`internal/context/budget.go:151-164`; context re-exports or callers repoint — pick the smaller
diff, NOTES which) and the two `Budget` methods per D4: `EstimateTokens(chars int) int` (ceil;
0 when `CharsPerToken <= 0`) and `HistoryExceedsAllocation(msgs []Message) bool`
(`History > 0 && EstimateTokens(PromptChars(msgs, nil)) > History` — mirror the existing
non-positive-budget never-trips rule).
**(b)** `internal/context` delegates: `TokenEstimator.EstimateTokens` keeps its default-ratio
fallback then calls the shared rounding core; `context.HistoryExceedsAllocation` builds on the
domain compare. One implementation of ceil-division exists afterwards.
**(c)** Migrate the callers: `guidedDecompositionEstimateTokens` and both signal checks
(`guidedDecompositionFreshUserOversized`, `guidedDecompositionMidExchangeOversized`) onto
`Budget.EstimateTokens` / a direct comparison (delete the local helper);
`libraryCapToBudget` keeps its default-substitution then uses the shared math;
`capMaxChars` computes its tokens→chars inverse FROM the same constants (document the inversion
against `Budget.EstimateTokens` in its comment — do not force an awkward shared shape, NOTES if
left as-is); `Agent.historyExceedsAllocation` (`internal/agent/compact.go:143`) routes through
the same single compare so the compaction trigger and any hook reading
`Budget.HistoryExceedsAllocation` can never disagree.
**Authorized delta (D4):** guided decomposition's thresholds move truncation→ceil (≤1 token per
comparison); adjust only boundary-exact fixtures, NOTES each.

**Tests:** `internal/domain`: table-driven `EstimateTokens` (zero/negative ratio → 0; exact
divisors; ceil boundaries) and `HistoryExceedsAllocation` (zero allocation never trips; the
existing context-level cases transliterated). Equality pin: for a grid of (chars, ratio),
`TokenEstimator.EstimateTokens` == `Budget{CharsPerToken: ratio}.EstimateTokens` whenever ratio
> 0 (the delegation cannot drift). The autocompact suites and guided-decomposition signal tests
pass unchanged except authorized boundary fixtures.

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/context`,
`internal/agent`, `internal/mechanisms` + CHANGELOG (public: two additive `Budget` methods —
minor). Commit:
`feat(domain): Budget.EstimateTokens and HistoryExceedsAllocation — one token arithmetic`.
**Depends on:** nothing in this plan (independent of items 3–5; still after the fixes plan).

---

## 7. History-scan helpers beside the spelling families

**Finding:** review candidate 2, the scan half: each history-inspecting Mechanism hand-rolls
the same `conv.Range(...)` walks — `internal/mechanisms/readloop.go:99-121`
(`isGreenfieldContext`), `:140-173` (`detectReadLoopPaths`), `:178-205`
(`detectSuccessfulReadLoopPaths`), `internal/mechanisms/readrepeat.go:118-155`
(`recentSuccessfulReads`), plus filehint's equivalents (re-locate by symbol) — differing subtly
in role/window/success handling. Ground truth: D5; the F8 spelling families the fixes plan's
item 11 landed (the helpers compose with them — same file or a sibling; re-locate the families
by the `wave4WriteTools` anchor); each Mechanism's existing tests as the behaviour contract.

**What:** extract the shared scan shapes as package-level helpers in `internal/mechanisms`
beside the families — the review's shapes: successful-read paths over a recent window,
read-attempt path counting (successes and failures separately), written paths since an index.
Parameterize by tool-name set (the families) and window so per-Mechanism membership and
thresholds stay at the call site (D5). Migrate readloop, readrepeat, and filehint onto them;
delete the private copies. A subtle per-Mechanism difference the helper cannot express without
contortion stays local with a comment naming the difference (NOTES it) — the goal is one copy of
each *shared* shape, not forced uniformity.

**Tests:** the three Mechanisms' suites pass **unchanged** (the contract). New table-driven
tests for each helper (use `domaintest`): mixed success/error results, window edges, tool-name
spellings from the families, calls-without-results.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`refactor(mechanisms): shared history-scan helpers beside the spelling families`.
**Depends on:** item 2 (`domaintest` for the new tests).

---

## 8. The tool-definition module: fold the per-tool ritual

**Finding:** review candidate 3 ("A tool-definition module", Worth exploring): ~19 built-in
tools each hand-roll the same ritual — a package-var schema string, an args struct, three
metadata methods, and a decode-and-error preamble (`internal/tools/read_file.go:13-60` is the
canonical shape; `decodeArgs`/`errorResult` at `internal/tools/tools.go:30-42` are the only
shared pieces). Safety threading is already clean (not in scope). Ground truth: D7; ADR 0002
(tools are an open extension point — the built-ins' idiom is what a third-party author copies);
`domain.Tool` / `domain.ReadOnlyTool` (unchanged); every existing per-tool test (the behaviour
contract).

**What:** per D7, in `internal/tools`: **(a)** an embeddable spec value carrying
name/description/schema and providing the three metadata methods; **(b)** a generic decode
helper wrapping `decodeArgs` that returns the standard arg-error `ToolResult` in one place
(today's per-tool error strings must survive verbatim where tests pin them — where they differ
only in incidental wording across tools, keep each tool's current string rather than unify,
NOTES any that beg unification for a later pass). **(c)** Migrate the full built-in suite
(including `internal/mcp`'s `serverTool` only if it shares the ritual — check; NOTES if left).
Raw JSON schema strings stay (D7 — no generation).

**Tests:** the entire `internal/tools` suite passes **unchanged** — that is the proof the
refactor preserved names, schemas, results, and error strings. One new test pins the spec
embedding (a tool built from a spec reports exactly the spec's name/description/schema bytes).

**Acceptance:** gates green; diff confined to `internal/tools` (and `internal/mcp` only per the
check above) + CHANGELOG. Commit:
`refactor(tools): embedded tool spec and typed arg decoding fold the per-tool ritual`.
**Depends on:** nothing in this plan (fully independent; ordered last of the code items because
it is the lowest-risk, most mechanical).

---

## 9. Docs close-out (owning item for the residue)

**What:** the cross-cutting residue with exactly one owner:
**(a) CHANGELOG:** sanity-check items 1–8 landed their lines under `## [Unreleased]`; the
`Budget` methods' line says additive/minor explicitly; no version heading (tagging is a release
decision).
**(b) TODO.md:** the read-tool-set consolidation entry the fixes plan's item 12(b) narrowed "to
whatever structural re-shaping remains" — items 6–7 are that re-shaping; close or re-narrow the
entry per what actually remains.
**(c) Architecture review report:** append a short dated "planned as
`docs/plans/architecture-deepening-plan.md`; candidates 1–4 implemented, candidate 5 declined"
note at the top of `docs/architecture-review-20260706-205911.html` (a visible HTML comment or a
small banner div — match the report's style).
**(d) ISSUES.md:** close anything this plan fixed; otherwise untouched.

**Acceptance:** gates green (docs-only otherwise); `git status` clean after commit. Commit:
`docs: close out the architecture deepening plan`.
**Depends on:** items 1–8.

---

## Explicitly NOT in this plan

- **Review candidate 5 (slicing `@file`/`/skill` resolution out of the loop)** — Speculative in
  the review and stays out: one caller, one adapter (a hypothetical seam), and the loop's
  remaining size is genuine Turn-sequencing depth. Revisit only if the resolver grows (richer
  skill layering); the review card records the reasoning.
- **Exporting `ExchangeView` (or any new symbol) at the root facade** — no external consumer
  exists; export is a deliberate later minor bump (D1), not a side effect of this plan.
- **Adding methods to the public `LoopView` / `ConversationView` interfaces** — breaking for
  external implementers (D1's semver analysis); every new query in this plan lands on the
  `Budget` struct, as a domain constructor, or mechanisms-internal.
- **Any Mechanism behaviour retune** — gates, thresholds, markers, and stacking relations stay
  exactly as ADR 0014 + the fixes plan left them, except D4's named ≤1-token rounding delta.
- **A Mechanism marker/idempotency framework** — F1 moved idempotency onto committed evidence;
  the residual marker use is one Mechanism's same-request guard. A shared marker store is
  speculative until a second Mechanism needs one (the deletion test currently fails to
  concentrate anything real); re-surface it at the next architecture pass if that happens.
- **A schema *generator* for tools** — D7 keeps schema strings visible and reviewable.
- **Wholesale test migration onto `domaintest`** — existing tests move only where an item
  touches them anyway; the delegation in item 2 keeps one implementation without churn.
- **The bench A/B campaign and any default-ON flip** — still bench-evidence-gated (ADR 0009),
  and still sequenced after the fixes plan per its own close-out.
