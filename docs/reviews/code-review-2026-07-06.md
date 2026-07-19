# Code Review — v1.2.0 → HEAD (guided decomposition + mechanism enable surface) — 2026-07-06

> **Fixed (2026-07-06):** the findings below were addressed by
> `docs/plans/post-v1.3.0-review-fixes-plan.md` (items 1–11 implemented and committed; item 12
> is this close-out). The 5 High / 10 Medium findings landed as the guided-decomposition
> substrate hardening (majority-marked enumerations, delegation-anchored cursor, consume-once
> matching, off-script re-defer, once-per-Exchange gate, line-anchored role-scoped markers,
> Exchange-scoped deferred actions, `truncate_history` incompatibility) plus the enable-surface
> tests and the read/list spelling-family consolidation. The residual structural re-shaping the
> findings flagged for `/improve-codebase-architecture` is carried by
> `docs/plans/architecture-deepening-plan.md`.

**Scope:** `git diff v1.2.0...HEAD` — the guided-decomposition Mechanism (ADR 0014), the public mechanism enable surface (ADR 0015), and the phase-4 third-review-fixes wave. 65 changed Go files, ~5,500 insertions, plus docs.
**Mission:** Apogee is a terminal coding agent for small local LLMs whose gated, self-regulating Mechanisms must never make the model perform worse than Bypass mode.
**Files reviewed:** 65 Go files (source + tests), read against the current working tree; ADRs 0014/0015 (incl. Realisation sections) and CONTEXT.md as ground truth.

**Baseline evidence:** `go build ./...`, `go vet ./...`, and the full test suite pass on every package. The security audit of the diff found no exploitable issue at or above the Medium floor. The three fixes mandated by the previous review were verified correct in the code (see *What Looked Good*).

## Executive Summary

The ADR 0015 enable surface landed clean end to end — the YAML→ID-list collapse, the single engine-side build path, registry validation in the stated order, sub-agent registry inheritance, and the bench-readiness contract shedding its internal imports all match the ratified design, and the third-review-fixes wave checks out. The problems are concentrated in one place: **guided decomposition's post-response half**. Its idempotency and queue cursor live entirely in request-scoped marker strings plus a deliberately lenient list parser, and under exactly the mid-Exchange conditions the Mechanism exists for, that substrate fails three independently triggerable ways — the gate re-steers completed work into a loop, the remainder cursor anchors on the wrong message, and one off-script tool call silently drops the queue. Each was found independently by two review passes. A fourth High: a queued fan-out directive survives Exchange boundaries after a fault or abort and hijacks the user's next, unrelated ask. The Mechanism ships default-off, so no default user is affected today — but the overnight aggregate bench campaign is the stated next step, and the `guided_decomposition + tool_result_cap` arm would measure this looping behaviour, not the design. Fix the marker/cursor substrate before benching that arm.

## Intent & Architecture Findings

### High — The gate re-steers once the fan-out's markers vanish, looping the decomposition `[Intent + Correctness]` (found independently twice)

- **Where:** `internal/mechanisms/guided_decomposition.go:132-153` (gate), `:217-227` (steer), `:294-298` (case-2 zero decision); `internal/agent/compact.go:126` (no mid-Exchange relief)
- **What:** Both no-double-steer markers are request-scoped only — the steer rides an `InjectContext` copy, the directive rides the deferred drain — and neither ever enters committed history. When the remainder empties (or a turn passes without re-deferral), the next request carries no marker while signal B (`HistoryExceedsAllocation`) is still true: child reports have grown history past its allocation and auto-compaction cannot fire mid-Exchange. The steer re-fires on the intended synthesis turn; the lenient parser (any 2–12 non-blank lines) then intercepts the model's final answer as a fresh "enumeration" and delegates its first line as a bogus subtask — a new fan-out of completed work, repeatable until context overflow. Self-regulation cannot stop it: each successful `sub_agent` dispatch is judged productive, clearing strikes every turn. The agent-level test fixture knows — `internal/agent/guided_decomposition_test.go:45-54` deliberately sizes the window so "signal B never re-steers mid-Exchange"; that guard exists only in the fixture, not in production code.
- **Why it matters:** This is the hard-constraint failure class the mission forbids — the Mechanism makes the model strictly worse than Bypass — and it fires near-deterministically at the end of any signal-B fan-out. It would also corrupt the imminent bench campaign's `guided_decomposition` arm.
- **Fix:** Give the gate durable fan-out evidence from *committed* history: stay quiet whenever the current Exchange contains an in-bounds enumeration with dispatched `sub_agent` calls (the `guidedDecompositionEnumeration` + `guidedDecompositionDispatchedTasks` machinery already reads exactly this), or commit a marker at enumeration time. A clean "once per Exchange" scoping is a candidate for `/improve-codebase-architecture`.

### High — One off-script tool call mid-fan-out silently drops the queue `[Intent + Correctness]`

- **Where:** `internal/mechanisms/guided_decomposition.go:294-298`; `internal/agent/loop.go:634-644` (`buildRequest` drains the directive into the request)
- **What:** The follow-through re-defers the remaining-items directive only when this turn's response carries a `sub_agent` call. But the directive was already drained out of `conv.deferred` into this request; if the model interleaves a `read_file`/`list_dir` turn before delegating — routine behaviour for small models — nothing is re-deferred. The remaining subtasks are never dispatched, the close-out instruction never reappears, and (worse) the marker-less next request lets the re-steer finding above fire from scratch. This contradicts the stated contract: "remainder re-derived each Turn from honest history … and re-deferred as a single directive" — the code re-derives only on delegation turns.
- **Why it matters:** A single exploratory tool call mid-fan-out — not an error, not suppression — silently abandons the decomposition the user was promised.
- **Fix:** When the directive marker is present in the request and the derived remainder is non-empty, re-defer the directive even on a non-`sub_agent` tool turn (keep the shrink path for delegation turns).

### High — The remainder cursor anchors on the wrong "enumeration" `[Intent + Correctness]` (found independently twice)

- **Where:** `internal/mechanisms/guided_decomposition.go:390-410` (anchor), `:466-478` (parser); `internal/context/compact.go:107-109` (summaries are multi-line assistant messages)
- **What:** `guidedDecompositionEnumeration` takes the *first* assistant message whose content parses in-bounds, and the parser accepts any 2–12 non-blank plain lines. The docstring's justification ("later follow-through replies are one-line delegations, so first-match reliably anchors on the original list") only considers messages *after* the enumeration — but signal B fires mid-Exchange by definition, and signal A fires on any later Exchange opening with an oversized user message, so earlier multi-line assistant messages (an ordinary prior answer, mid-Exchange narration, even an auto-compaction summary) shadow the real enumeration. Follow-through directives then list lines of an old reply as "remaining subtasks": the remainder never shrinks and the model is told each turn to delegate garbage.
- **Why it matters:** Under exactly the trigger conditions the ADR added the mechanism for, the cursor is unreliable — delegated garbage plus an unshrinkable remainder is another worse-than-Bypass path.
- **Fix:** Anchor on the assistant message that itself carries a `sub_agent` tool call — the enumeration commits uniquely as an in-bounds list *plus* the synthesized delegation (`assistantMessage(resp, calls)`) — preferring the latest match; or require explicit ordered-list markers on the anchor path.

### Medium — The lenient list parser cannot tell prose from an enumeration `[Intent]` — warrants revisiting ADR 0014 §2

- **Where:** `internal/mechanisms/guided_decomposition.go:466-478` (`guidedDecompositionParseList`), `:279-289` (case-1 intercept)
- **What:** Under an outstanding steer, *any* no-tool-call reply of 2–12 non-blank lines is accepted as the enumeration — a clarifying question, a refusal, a direct answer — its first line shipped to a sub-agent as a "task" and the rest deferred as "remaining subtasks". The plain-line leniency is a documented ADR 0014 §2 decision, but it is the enabling root cause of all three High hijacks above, so it meets the actively-causing-defects bar: it warrants revisiting.
- **Why it matters:** Every misfire is a hard-constraint violation in miniature; the ADR's own §5 rejected verb-sniffing for exactly this false-positive class.
- **Fix:** Require a majority of parsed lines to carry explicit list markers before treating a reply as an enumeration — `guidedDecompositionStripMarker` already knows whether it stripped one.

## Critical & High Findings

### High — A stale fan-out directive survives into the next Exchange after a fault or abort `[Correctness]` (verified in the main review pass)

- **Where:** `internal/agent/loop.go:303-306` (`turnFailed` → `restoreDeferred` + `abandonTurn`); `internal/agent/agent.go:163-170` (`AbortExchange` never clears `conv.deferred`)
- **What:** On a terminal stream error or context overflow — likeliest mid-fan-out, when history is over budget — the drained directive is restored into `conv.deferred` and the Exchange ends with it queued. The TUI's Esc path (`cancelTurn` → `AbortExchange`) leaves it queued too. The user's next message drains the stale "Remaining decomposition subtasks (N left): Delegate EXACTLY the next subtask now…" directive into the new ask's *first* request; on the fault path the old enumeration is still in history, so the stale fan-out resumes wholesale, deferring the user's new question. The ADR's documented acceptance covers only the single post-suppression drain, not Exchange-crossing.
- **Why it matters:** The mechanism hijacks an unrelated user ask after any mid-fan-out fault — user-visible, and invisible to the suite.
- **Fix:** Clear `conv.deferred` in `AbortExchange`; drop rather than restore drained deferrals in `abandonTurn` (the Exchange they belonged to is dead); or tag deferrals with their Exchange and expire them at Exchange end.

### High — No test exercises sub-agent spawn under the production `EnableMechanisms` arm `[Tests]`

- **Where:** `internal/agent/subagent.go:118-119`
- **What:** `newChildAgent` hands the child the parent's already-built registry and clears `EnableMechanisms` precisely because rebuilding the IDs into the shared registry would trip duplicate-ID rejection and fail **every** spawn. Nothing exercises this: the guided-decomposition end-to-end tests arm via a pre-built `cfg.Mechanisms`, `enable_mechanisms_test.go` never dispatches `sub_agent`, and `benchreadiness_test.go`'s scripted model only calls `list_dir`. Revert line 119 and the whole suite stays green while every production delegation (the CLI arms via `EnableMechanisms`) fails at spawn — a mechanisms-on-only failure, i.e. the worse-than-Bypass class, on the exact path guided decomposition's fan-out depends on.
- **Why it matters:** The production arming path and the delegation path only meet in untested code.
- **Fix:** One test: parent with `EnableMechanisms = {"guided_decomposition","tool_result_cap"}` and `sub_agent` registered completes one delegation — spawn returns no error, child events nest at Depth 1, the child fires catalogued Mechanisms from the shared registry.

## Medium Findings

### Medium — Marker detection is a bare substring scan over every message and role `[Intent + Correctness]` (found independently twice; the security audit confirmed no security impact)

- **Where:** `internal/mechanisms/guided_decomposition.go:59-62, 172-183, 308-321`
- **What:** The markers are plain English phrases ("Decomposition planning…", "Remaining decomposition subtasks…") matched with `strings.Contains` over all roles. A repo planning doc read via `read_file`, an `@file` reference, or the model echoing the directive text (small models routinely echo instructions) either makes `guidedDecompositionOutstanding` permanently true — silently disabling the mechanism for the session — or satisfies the case-1/case-2 marker checks in later, unrelated Exchanges, misfiring via the anchor-drift path. Quality impact only; the synthesized call still goes through full dispatch gating.
- **Fix:** Scope the marker scan to messages the loop itself injected (system + injected user roles), and/or use low-collision sentinel strings.

### Medium — Co-enabling `truncate_history` destroys the fan-out cursor mid-flight `[Correctness + Tests]`

- **Where:** `internal/mechanisms/guided_decomposition.go:376-378`; `internal/mechanisms/truncate_history.go:28`
- **What:** `truncate_history` is a legal peer (no stacking relation declared), keeps only the last 4 assistant-anchored exchanges, and runs mid-Exchange. A fan-out longer than that drops the enumeration message; `guidedDecompositionRemainder` returns nil, the remaining subtasks are silently discarded without the close-out instruction — and with no directive re-deferred, the re-steer finding fires into a fresh enumeration. The bench's full-stack arms would co-enable them.
- **Fix:** Declare `IncompatibleWith: [truncate_history]` on the descriptor (matches the existing stacking vocabulary), or protect the enumeration message from the cut.

### Medium — `cancelTurn` restores the old directive but keeps the one deferred by the cancelled turn itself `[Correctness]`

- **Where:** `internal/agent/loop.go:608-618`; `internal/domain/hookrun.go:187-190` (post-response hooks `Defer` before dispatch)
- **What:** Post-response hooks defer directive *k+1* during the turn; a cancel during a slow `sub_agent` dispatch rolls back the messages and restores directive *k* — leaving the queue holding both contradictory directives ("3 left" and "2 left", each demanding "delegate EXACTLY the next subtask"). The re-attempted request, and any snapshot taken at that boundary, carries both.
- **Fix:** Snapshot `len(conv.deferred)` before the post-response hooks run and truncate back to it in `cancelTurn` before `restoreDeferred`.

### Medium — Prefix matching marks duplicate or prefix-nested items as dispatched `[Correctness]`

- **Where:** `internal/mechanisms/guided_decomposition.go:452-459`
- **What:** Dispatched-task matching uses `strings.HasPrefix(task, item)` where tasks are `item + " " + hygiene`. An enumeration with duplicate items, or where item *i* is a textual prefix of item *j* ("Add tests" / "Add tests for the CLI"), drops item *i* from the remainder once *j* is dispatched — it is never delegated. Reachable whenever the model delegates out of order (tolerated by design) or enumerates near-duplicates.
- **Fix:** Match exactly against `item` or `item + " " + guidedDecompositionReportHygiene` and consume at most one dispatched task per item.

### Medium — Read/list tool-name sets: five hand-maintained copies, already diverged `[Structure]`

- **Where:** `internal/mechanisms/offramps.go:47-52`, `cot.go:77-89`, `library.go:74-80`, `historyhints.go:32-35`, `toolfilter.go:50-54`
- **What:** The write side genuinely consolidated in this diff (`wave4WriteTools` behind `isFileMutatingTool`/`isWriteTool` — verified at all call sites), but the read/list twins were left as five copies that have already diverged: `library.go`'s list set is missing the camelCase spellings (`listFiles`, `listDir` — the shallow-exploration observation misses models the greenfield detector catches), `cot.go`'s is missing `list_directory` (such a turn wrongly ends the read-only streak, suppressing the stall nudge), and `toolfilter.go` carries a fourth variant. This exact drift class produced shipped defects in the two previous review rounds.
- **Fix:** Hoist the read/list sets beside `wave4WriteTools` as the single source — a candidate for `/improve-codebase-architecture`.

### Medium — Stale wiring pointers after the ADR 0015 wire.go collapse `[Structure]`

- **Where:** `internal/mechanisms/catalogue.go:20-32, 46-52`; `internal/mechanisms/library.go:22`
- **What:** Comments still say `cmd/apogee/wire.go` constructs and injects `Deps` (the Library store, the grammar seam). It no longer builds any `mechanisms.Deps`; the single build path is `buildEnabledMechanisms` in `internal/agent/loop.go:129-160`. These pointers misdirect maintenance of a load-bearing wiring path.
- **Fix:** Repoint the three references at the engine's build path.

### Medium — Corrupt-store degrade branch at construction is untested `[Tests]`

- **Where:** `internal/agent/loop.go:140-146`
- **What:** The Load-error → stderr-notice → empty-store branch is never executed by any test: `Load` returns nil for an *absent* file, so the existing temp-dir cases only take the happy path. A regression to fail-hard would brick startup for anyone with a corrupt `library.json` and `library` enabled, with the suite green.
- **Fix:** Seed `LibraryDir/library.json` with garbage, set `EnableMechanisms = ["library"]`, assert construction succeeds and the Mechanism runs over an empty store.

### Medium — Accept-window boundaries (exactly 2 and exactly 12) are untested `[Tests]`

- **Where:** `internal/mechanisms/guided_decomposition.go:336-338`
- **What:** Tests pin decline at 1 and 13 and accept at 3, so mutating `>= 2` to `>= 3` (or `<= 12` to `<= 11`) survives the suite. A 2-item enumeration is the likeliest small-model output; it silently declining is a silent mechanism-off against an ADR-locked bound.
- **Fix:** Add exactly-2 and exactly-12 accepted cases (one call synthesized, remainder deferred) and an empty-enumeration decline.

### Medium — `CataloguedMechanisms()`'s documented clone contract is untested `[Tests]`

- **Where:** `internal/mechanisms/catalogue.go:104-108`; `apogee.go:305-313`
- **What:** The API documents that a caller may mutate the result freely. Nothing tests it: dropping `slices.Clone` would let an embedder's leave-one-out planning (the exact idiom the bench-readiness test and the public example model) corrupt the process-global descriptor table in a long-lived bench process, invisibly.
- **Fix:** One-line test — mutate a returned descriptor's relation slice, assert a fresh query is unchanged.

## Recommended Action Order

1. **Rework guided decomposition's idempotency/cursor substrate before the bench campaign** — the three intent Highs and the parser Medium share one root cause and one fix wave: durable fan-out evidence from committed history (the re-steer loop), re-deferral on off-script turns (the dropped queue), a `sub_agent`-call-bearing anchor (the cursor drift), and a stricter enumeration recognizer (revisit ADR 0014 §2's plain-line leniency; the marker-scoping Medium largely falls out of this too). Benching the `guided_decomposition + tool_result_cap` arm before this fix would measure the loop, not the design.
2. **Expire deferrals at Exchange boundaries** — clear on abort, drop on abandon, truncate on cancel (the Exchange-crossing High plus the cancelTurn Medium). Small, contained agent-loop change.
3. **Add the `EnableMechanisms` → sub-agent spawn test** — one test protecting the production arming path.
4. **Declare `guided_decomposition` incompatible with `truncate_history`** — one descriptor row plus a registry test.
5. **Cheap test adds:** corrupt-store degrade, accept-window boundaries, descriptor clone.
6. **Hygiene, no urgency:** consume-once dispatched-task matching; consolidate the read/list tool sets and repoint the stale wiring comments (the tool-set consolidation is a `/improve-codebase-architecture` candidate).

## What Looked Good

The ADR 0015 enable surface is structurally sound end to end: the wire.go YAML→ID-list collapse, the single engine-side build path, descriptor/constructor twin registration, validation in the ratified order (incompatibilities before requirements), child-agent registry inheritance, and the bench-readiness contract genuinely shedding its `internal/mechanisms` and `internal/library` imports — with no dead exported additions. The security posture was preserved and in one place improved: synthesized `sub_agent` calls dispatch through the full Resolution/Approval/depth-bound path identically to native calls, sub-agent privilege can only narrow, and the library store gained content sanitization closing a store→system-prompt injection channel. All three fixes from the previous review verified correct (the saturation latch now gates on a fold that ran; the `committedLen` retry-view bound holds on the empty-superseded path; the write-detection consolidation preserved each call site's semantics). Test discipline remains strong — non-vacuous, loop-driven tests with fire-count and Bypass-equality assertions — and build, vet, and the full suite pass everywhere.
