# Plan — Phase 4: merge the apogee-sim Mechanisms into the loop

**Date:** 2026-07-04
**Status:** items 1–6 done (2026-07-04, including the review-fixes pass — see the
addendum under "Where things stand"); ready to resume at item 7.
**How to run:** `implement-plan docs/plans/phase-4-detail-plan.md with skills: coding-standards`
(the broad plan's standing requirement #1 makes `coding-standards` mandatory for every item).
**Source of direction:** `docs/plans/implementation-plan-apogee-merge.md` §4 "Phase 4" +
§6 decisions 7/10/11/12/13/14; ADR 0002/0003/0006/0009; CONTEXT.md "Mechanism and hook
points" / "Self-regulation" / "Context and history".
**Verify gate (every item):** `make check` (gofmt-clean, vet, build, race tests, ADR-0010
invariant) plus the item's own test commands. "Gates green" below means exactly this.

**Port source:** the apogee-sim checkout at `~/Repos/Airic/apogee-sim` — **the owner places
it there before the run** (confirmed 2026-07-04); it is NOT on this machine yet. Item 1
verifies it and pins the commit. apogee-sim is read-only reference material: no item ever
modifies it, and no apogee code may import it (the dependency points the other way,
ADR 0001).

---

## Where things stand (grounded, verified 2026-07-04)

Everything below was verified against the working tree at `b4ad0bb`; do not re-derive.

- **The five hook points are designed, public, and wired — for experimental hooks only.**
  Interfaces: `internal/domain/mechanism.go:34-65` (`PreRequestHook`, `PostResponseHook`,
  `PreToolExecHook`, `PostToolResultHook`, `HistoryRewriter`), re-exported at
  `apogee.go:258-264`. Invocation: `internal/agent/hookrun.go` — history-rewrite at
  `loop.go:190`, pre-request at `loop.go:201`, post-response at `loop.go:267`,
  pre-tool-exec at `dispatch.go:44`, post-tool-result at `dispatch.go:56`. Each `run*Hooks`
  iterates **only** `registry.Experimental(hp)`; a Mechanism added via
  `MechanismRegistry.Add` is validated (hook-interface check, ordering-cycle check via
  `ValidateOrdering`, `mechanism.go:166/197`) but **never dispatched**.
- **Registry/descriptor types exist:** `MechanismDescriptor` (ID, `Capability`,
  `SuppressionPolicy`, `IncompatibleWith`) at `mechanism.go:105-111`;
  `OrderingConstraints{Before, After}` at `mechanism.go:138-141`; `MechanismFiredEvent`
  is an Event variant already emitted for experimental fires (`hookrun.go:159`).
- **`LoopView.Fired(id MechanismID) int` is declared** (`internal/domain/hooks.go:214-217`)
  but nothing tracks fires — the self-regulation substrate is greenfield.
- **`Config.Bypass` exists end-to-end but gates nothing:** `domain/config.go:21`, file key
  `bypass` (`cmd/apogee/config.go:156`), flag>env>file resolution — no consumer in the loop.
- **`internal/mechanisms/` is a 9-line `doc.go` scaffold.** The catalogue is entirely
  greenfield.
- **Hook working-value surface is complete** (applied 2026-06-23 from the three-slice
  apogee-sim survey, `docs/design/hook-mutation-api.md`): `Request.AppendToSystem/
  InjectContext/SetMessageContent/SetTools/SetExtra`, `Response.SetToolCallArguments/
  SetText` + `PostResponseDecision{Action, Inject}`, `Conversation.PrefixEnd/
  AssistantBoundaries/SetMessageContent/DropRange/Insert/Replace/Defer/TakeDeferred`,
  `LoopView`/`ConversationView` pairing helpers (`CallByID`, `ResultFor`, `LastUser`).
  That doc's §7 operation→mechanism traceability table is the port map.
- **internal/context:** generative `Compact` shipped (protected prefix, `Replace`
  write-back; on-demand `/compact` in the TUI). **Missing:** Budget allocator,
  tool-result capping, history truncation, real token accounting — the loop uses a
  trivial `defaultCharsPerToken = 4.0` estimate (`loop.go:27`); server-reported usage
  only rides `UsageEvent`.
- **Library: not implemented.** Only `Config.LibraryDir` (injected state root,
  `domain/config.go:59`) exists. No store, no `ModelFingerprint`.
- **Bench contract seam exists:** `New/Resume/Submit/Step/Run/Snapshot/Close`,
  `AddExperimental`, isolated state roots. Session snapshot/resume works
  (`internal/session/store.go`).
- **Tool results are appended uncapped** (`dispatch.go:407`); individual tools self-cap
  their own output only.

**Addendum (2026-07-04, after items 1–6 + the review-fixes pass):** the bullets above are
the pre-run baseline (`b4ad0bb`) — kept for orientation, superseded wherever items 1–6
landed; re-verify any file:line ref before relying on it (e.g. `defaultCharsPerToken` is
now `loop.go:28`). Current substrate for items 7–16: catalogued Mechanisms dispatch in
deterministic order under the Bypass gate (item 2); self-regulation judges fires against
the NEXT Turn's three-way outcome and books only acted fires (item 3 as amended — R3/R4);
the `mechanisms:` config block + constructor table are live and hardened (item 4 +
duplicate/reserved-ID refusal, loud unknown-key errors); wave 1 shipped — `validate`,
`autofix`, `syntax` (cascade order validate → autofix → syntax) plus both off-ramps —
delivering corrections by retry-in-place (R1: `ActionRetry{Inject}` re-streams in the same
Turn; catalogue C5 as amended). The review-fixes plan and its owner-ratified design record
**R1–R5** are archived at `docs/plans/archived/phase-4-review-fixes-plan.md`; R1–R5 bind
items 7–16 exactly like D1–D8.

- **D1 — Default-off until bench-proven.** Every catalogued Mechanism ships
  **config-gated and disabled by default**. A mechanism's default flips ON only after its
  bench A/B (run in apogee-sim, ADR 0009 gate) passes — that flip is a later one-line
  change, not part of this plan. Rationale: the broad plan's "A/B-validate via the bench
  before keeping it on" + owner confirmation 2026-07-04 ("bench-proven mechanisms belong
  in apogee"). The bench enables mechanisms explicitly via `Config`, so shipped defaults
  don't constrain it.
- **D2 — Suppression is loop/registry-managed** (hook-mutation-api §8 #2): the loop simply
  does not call a suppressed Mechanism. Mechanisms never receive suppression state;
  cross-mechanism coupling is a `LoopView.Fired(id)` query or a declared
  ordering/incompatibility constraint.
- **D3 — Dependencies are construction-injected** (§8 #3): the Library store, backend
  capability probes, formatter availability are given to a Mechanism when it is built
  (wire-up), never passed per-call. Hook signatures stay about conversation state.
- **D4 — Canonical IDs and deterministic order.** Mechanism IDs are apogee-sim's
  snake_case IDs (`toolfilter` → `tool_filter` style: use the sim's own canonical spelling,
  recorded per-row in the catalogue). Dispatch order per hook point is a **deterministic
  topo-sort** of declared constraints with a **stable tiebreak by canonical ID** (broad
  plan §4). Catalogued Mechanisms run **before** experimental hooks (which keep
  registration order) — the bench observes/perturbs the configured behaviour, not the
  other way round.
- **D5 — Bypass = run only `Capability == off-ramp`.** In Bypass, every catalogued
  non-off-ramp Mechanism is skipped at dispatch (proactive-nudge + response-repair off,
  decision 13) and the Library is fully inert (no inject, **no observe/write**).
  **Structural context machinery is NOT a Mechanism and stays on in Bypass** (decision
  12: Budget + Compaction are load-bearing — a naked model just overflows its window).
- **D6 — The four-way context split** (decision 10): **Budget** (structural, `context/`) ·
  **tool-result capping** (a pre-request *Mechanism* — D1 applies) · **Compaction**
  (structural, `context/`, generative, the default reducer — gains its automatic trigger
  here) · **history truncation** (a history-rewrite *Mechanism*, off by default, the cheap
  A/B alternative to Compaction).
- **D7 — The catalogue doc (item 1) is authoritative for wave composition.** Wave items
  below name their *expected* members from the broad plan + survey; if the ratified
  catalogue disagrees (a mechanism relocated, dropped, or renamed), follow the catalogue
  and say so in NOTES. An implementer must not silently invent a mechanism the catalogue
  doesn't list.
  **Amended 2026-07-04 (review-pass lesson):** the catalogue's authority covers
  *composition only* (which mechanisms, which wave, port-or-drop). For *behavior*, the
  ground truth is the pinned sim source — a catalogue cell that contradicts the pinned
  source is a defect to surface (report QUESTION, or amend the catalogue with owner
  ratification and a dated note), never a license to diverge from the source silently.
  Precedence rules must point at ground truth, not at an artifact an earlier item
  produced: item 1's original C5 cell contradicted the sim's retry builders and steered
  items 5–6 into the ActionDefer divergence the review-fixes plan had to unwind.
- **D8 — Out of scope for this plan:** the bench A/B campaign itself, trace-driven
  attribution runs, per-mechanism default flips, the behavioral-probe fingerprint
  (`apogee probe`, Phase 5), Windows, and any change inside apogee-sim. The plan ends with
  apogee *benchable*: every catalogued Mechanism registered, gated, self-regulating, and
  provably drivable from an external module.

---

## 1. Pin the apogee-sim source and ratify the Mechanism catalogue — ✅ DONE (2026-07-04)

**What:** confirm the checkout at `~/Repos/Airic/apogee-sim` exists (missing → STATUS
BLOCKED asking the owner to place it; do not clone on your own). Record
`git -C ~/Repos/Airic/apogee-sim rev-parse HEAD` as the pinned port commit. Then write
**`docs/design/mechanism-catalogue.md`**: one row per mechanism from the broad plan's §2
inventory plus the lab-only interventions — expected set: `library`, `decompose`,
`toolfilter`, `cot`, `filehint`, `intent`, `grammar`, `syntax`, `autofix`, `validate`,
`codeinfo`, `compress` (split per D6), `cached_content_intercept`, `error_enrichment`,
`read_loop`, `read_repeat`, `tool_use_enforcer`, `empty_response_recovery`,
`correct_tool_result`, `truncate_history` — plus anything found in the checkout the
inventory missed. Columns: canonical ID · source package/files (verified against the real
checkout — where `docs/design/hook-mutation-api.md`'s file:line refs drifted, correct them
here, not there) · target hook point (relocations: `cached_content_intercept`→
pre-tool-exec, `error_enrichment`→post-tool-result; `grammar`/`filehint` need explicit
assignment per hook-mutation-api §8 #7) · `Capability` · `SuppressionPolicy` (exempt:
`tool_use_enforcer`, `empty_response_recovery`) · ordering/incompatibility constraints
(e.g. the decompose↔read-loop `FiredCounts` coupling becomes a `Fired` query or an
ordering edge) · port wave (map to items 5–14) · port-or-drop verdict with one-line
rationale (`codeinfo` is pre-decided DROP — deprioritized in the broad plan §2; decide
`cot`/`intent`/`grammar` from source + whatever eval evidence the checkout carries) ·
prior-evidence pointer (sim eval results/traces if present in the checkout; if the trace
archive is absent, record that the mapping is grounded on the signature survey) · a
**"bench validation: pending"** status column. Also locate the broad plan's "completion
nudges" in the sim source and assign them a wave.

**Docs (same commit):** the new catalogue doc; a pointer line added to
`docs/design/hook-mutation-api.md`'s header ("catalogue ratified → mechanism-catalogue.md").

**Acceptance:** the catalogue has a row for every inventory mechanism with no empty
cells; the pinned commit hash is recorded; gates green (docs-only change otherwise).
Commit: `docs(design): ratify the Phase-4 mechanism catalogue against the pinned apogee-sim source`.

---

## 2. Dispatch catalogued Mechanisms: deterministic order + the Bypass gate — ✅ DONE (2026-07-04)

**What:** make `MechanismRegistry.Add` mean something. Registry side: an
`Ordered(at HookPoint) []Mechanism` accessor — topo-sort of `OrderingConstraints` with
stable tiebreak by canonical ID (D4); construction-time validation grows an
`IncompatibleWith` check (registering two mutually-incompatible mechanisms → loud
`New`-time error, same posture as `ValidateOrdering`). Loop side: each `run*Hooks` in
`internal/agent/hookrun.go` dispatches catalogued mechanisms first (in `Ordered` order),
then experimental hooks (registration order, unchanged), under the same recover boundary
and `MechanismFiredEvent` attribution. Bypass gate (D5): when `cfg.Bypass`, skip every
catalogued Mechanism whose `Capability != off-ramp`; experimental hooks are NOT
Bypass-gated (they are the bench's own instruments).

**Tests:** order determinism (shuffled registration → identical `Ordered` output);
tiebreak by ID; incompatibility → construction error; Bypass matrix (off-ramp fires,
nudge/repair skipped, experimental unaffected); fired events carry the mechanism ID;
a panicking catalogued mechanism is contained exactly like a panicking experimental hook.

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/agent`,
root re-exports if needed, + docs. Commit:
`feat(agent): dispatch catalogued mechanisms in deterministic order with the Bypass gate`.

---

## 3. Self-regulation: effectiveness tracking, Adaptive Suppression, Turn Budget — ✅ DONE (2026-07-04)

**What:** the per-Session tracker behind `LoopView.Fired` and the two withdrawal rules
(CONTEXT "Self-regulation"; decision 12 — these are proxy-signal heuristics, explicitly
weaker than the bench gate). Effectiveness tracking: record each catalogued fire; judge
the *next* Turn better/worse for it on proxy signals (a new file read, a file written, a
tool error, an empty/no-op response). **Adaptive Suppression** (per-Mechanism): judged
not-helpful N consecutive times (default 3) in a Session → suppressed for the rest of it,
with a clear-path that re-opens on a productive Turn. **Turn Budget** (global): after M
consecutive non-productive Turns, all non-exempt Mechanisms are suppressed until
productive activity resumes. `SuppressionPolicy == exempt` bypasses both. The loop
consults the tracker at dispatch (D2 — a suppressed mechanism is simply not called);
`LoopView.Fired` now answers from the tracker. Tracker state is per-Session and
**resets on Resume** (fresh suppression state can only cause re-tries; record this
accepted v1 posture in the doc comment).

**Tests:** strikes-then-suppressed; clear-path re-opens; exempt never suppressed; Turn
Budget trips globally and clears; `Fired` counts visible to a test hook; reset-on-resume.

**Acceptance:** gates green; diff confined to `internal/agent` (or a new
`internal/agent` sub-file — NOT a new public surface) + `internal/domain` + docs. Commit:
`feat(agent): per-session effectiveness tracking with adaptive suppression and the turn budget`.

**NOTES (2026-07-04 review):** deviations found — judgment was same-Turn on two proxy
signals (novel read / write) and fires were booked per invocation, not per intervention;
fixed by `phase-4-review-fixes` item 4 (R3: next-Turn three-way judgment on four signals,
with cancelled-Turn read-novelty rollback; R4: acted-fires only). Accepted per R5:
cancelled-Turn `fireCounts` may over-report toward the decompose coupling; the reserved
experimental ID moved to `internal/domain`.

---

## 4. Config surface: the `mechanisms:` block + wire-up seam — ✅ DONE (2026-07-04)

**What:** a file-only `mechanisms:` config block (like `mcp-servers` / `model-profile` —
no flag/env), mapping canonical ID → `enabled: true|false`. All defaults **off** (D1).
An unknown ID is a loud startup error listing the known catalogue. Thread it
fileConfig → layer → settings → `domain.Config` (`cmd/apogee/config.go:151/70/31/108`).
Wire-up: `internal/mechanisms` gains a constructor table
(`Build(id, deps) (domain.Mechanism, error)`) that `cmd/apogee/wire.go` drives for each
enabled ID, injecting a `deps` struct (D3: Library store — nil until item 13 — formatter
availability, backend capabilities); built mechanisms are `registry.Add`ed before `New`.
The table starts empty-but-tested (a fake row) — waves 5–14 fill it.

**Tests:** config round-trip (enable one → registered; default → none registered);
unknown ID error; Bypass + enabled interplay (enabled but Bypass ⇒ not dispatched, per
item 2's gate).

**Docs (same commit):** README config section + example `config.yaml` block.

**Acceptance:** gates green; diff confined to `cmd/apogee`, `internal/mechanisms`,
`internal/domain` (if a config type is needed) + docs. Commit:
`feat(config): file-only mechanisms block wiring the catalogue constructor table`.

---

## 5. Wave 1 — response robustness: `validate`, `syntax`, `autofix` — ✅ DONE (2026-07-04)

**What:** port the measured-win robustness stages (broad plan: "these carried most of the
win") from the pinned sim source as post-response Mechanisms per the catalogue.
`validate`: tool-call validation (unknown tool name against `LoopView.Tools()`, malformed
arguments) → `ActionRetry` with the sim's correction message; when the stream can't be
retried in place → `ActionDefer` with the correction as `Inject` (the loop already
persists it via `Conversation.Defer`). `syntax`: argument-syntax repair per the sim's
rules. `autofix`: formatter pass writing back via `Response.SetToolCallArguments` —
**in-process gofmt always; goimports/black/prettier/rustfmt only when detected on PATH,
gracefully absent** (standing requirement #2; availability injected per D3). Descriptors
per the catalogue (Capability response-repair, strikes-3). Register all three in item 4's
table.

**Tests:** table-driven against the scripted-responder harness
(`internal/agent/harness_test.go` fakes): bad call → retry with correction; streaming →
deferred inject lands in the next request; autofix formats a Go payload in-process;
missing external formatter degrades silently; suppression kicks in after strikes.

**Acceptance:** gates green; diff confined to `internal/mechanisms` (+ small
`internal/agent` seams if the port exposes a gap — name it in NOTES) + docs/CHANGELOG.
Commit: `feat(mechanisms): port the validate/syntax/autofix response-robustness wave`.

**NOTES (2026-07-04 review):** deviations found — corrections delivered by `ActionDefer`
(next request) instead of the sim's in-cycle retry; `autofix` probed formatters at fire
time (D3 violation), beautified unconditionally, and ran after `syntax`. Fixed by
`phase-4-review-fixes` items 1–3 (R1 retry-in-place per amended catalogue C5;
construction-probed formatter table; issue-count-gated repair; cascade reordered to
validate → autofix → syntax).

---

## 6. Wave 1 — off-ramps: `empty_response_recovery`, `tool_use_enforcer` — ✅ DONE (2026-07-04)

**What:** the two recovery guarantees, ported per the catalogue as post-response
Mechanisms with `Capability: off-ramp`, `SuppressionPolicy: exempt` — they run even in
Bypass (D5) because without them a failed Turn has no way out (CONTEXT "Off-ramp").
`empty_response_recovery`: empty text + no tool calls → bounded corrective retry (the
sim's nudge text; a hard attempt cap so an always-empty model still terminates).
`tool_use_enforcer`: the model narrates instead of acting → corrective retry/defer per
the sim's trigger conditions. Exempt-from-suppression ≠ exempt-from-validation (decision
13) — their bench leave-one-out stays pending like everyone else's.

**Tests:** empty response recovered once then passed through at the cap; enforcer fires
only on its trigger; both fire under Bypass; both ignore Adaptive Suppression and the
Turn Budget.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit: `feat(mechanisms): port the empty-response-recovery and tool-use-enforcer off-ramps`.

**NOTES (2026-07-04 review):** deviations found — the enforcer's correction sat deferred
until the next user Submit (the sim re-calls in-cycle) and empty recovery retried bare,
without the sim's nudge; fixed by `phase-4-review-fixes` items 1–2 (R1 retry-in-place
carrying the superseded narration + correction / the first-attempt completion-check nudge).
The sim's retry-ladder refinements (attempt-2 nudge ladder, system directive, temperature
escalation, per-Session throttle counters) stay un-ported — accepted per R2, recorded on
the catalogue's off-ramp rows.

---

## 7. Wave 2 — loop-native: `truncate_history` (`correct_tool_result` deferred)

**Design call — RESOLVED (owner-ratified 2026-07-04):** `correct_tool_result` is
**DEFERRED, not ported.** The pinned sim defines no production trigger — it is a lab-only
intervention with an operator-supplied correction ("a successful one is a finding that
motivates a new production surface, not a 1:1 port", `intervention.go:12-15`) — and
inventing gating logic would ship behavior with no sim evidence (D7 as amended). The loop
already ships the lab surface the sim's operator had: an experimental post-tool-result
hook can replace a result via the mutation API, so the bench plays the operator without a
catalogued Mechanism. A bench-discovered trigger motivates a NEW plan item. The catalogue
(Table A/B rows, resolved open-question note, ledger) was amended with this ratification
on 2026-07-04 — no catalogue work remains in this item.

**What:** `truncate_history` (history-rewrite): drop-the-middle keeping the last N
exchanges, cutting only at `AssistantBoundaries()` (tool results stay adjacent to their
call), never touching `PrefixEnd()`, inserting the static gap-note message
(`Conversation.DropRange` + `Insert` — the operations were designed for exactly this,
hook-mutation-api §6). Off by default like everything (D1) — it is the cheap A/B
alternative to Compaction, validated bench-side later.

**Tests:** truncation respects prefix + boundaries (property-style over generated
histories); gap note inserted once.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit: `feat(mechanisms): port the truncate-history rewrite; correct_tool_result deferred`.

---

## 8. Budget allocator + honest token accounting (structural, `internal/context`)

**What:** the Budget from CONTEXT ("the single authority on how much room each part
gets"): allocate the model's context window (provider-discovered `n_ctx`) across system
prompt / conversation history / file context / response reserve. Estimation: keep a
chars-per-token heuristic but **calibrate it against server-reported usage** — when a
`UsageEvent` arrives, snap `Used` to the reported prompt tokens and recompute the ratio
from actual chars-sent (bounded to a sane range). Replace the loop's
`defaultCharsPerToken = 4.0` trivial estimate so `LoopView.Budget()` becomes honest.
Structural — NOT a Mechanism, stays on in Bypass (D5/D6). This also closes the TODO.md
deferral "automatic budget-driven trigger needs the Budget allocator" *prerequisite*
(the trigger itself is item 9).

**Tests:** allocation arithmetic (reserve honoured, parts sum ≤ window); calibration
converges toward reported usage across simulated turns; `Budget()` view reflects it;
no behaviour change to requests themselves (allocation is advisory until item 9 consumes
it).

**Acceptance:** gates green; diff confined to `internal/context`, `internal/agent`,
`internal/domain` + docs/CHANGELOG. Commit:
`feat(context): budget allocator with usage-calibrated token accounting`.

**Depends on:** nothing in waves 5–7 (can run right after item 4 if resuming out of order).

---

## 9. Tool-result capping (Mechanism) + the automatic Compaction trigger

**What:** the two Budget consumers. **Tool-result capping** — the surviving half of the
sim's `compress`, per the catalogue: a pre-request Mechanism that truncates any single
tool-result message exceeding its Budget fraction, head/tail preserved with an elision
marker, the most recent Turn always protected (CONTEXT "Tool-result capping");
implemented once, via `Request.SetMessageContent` (in-place edit — no wholesale replace
at this hook, hook-mutation-api finding §1.4). D1 applies: default off.
**Automatic Compaction** — the existing generative `Compact` finally becomes "the
default reducer" (CONTEXT): when the Budget's history allocation is exceeded at a
quiescent boundary, run the same compaction the TUI's `/compact` drives (protected
prefix, `Replace` write-back, events visible), non-reentrant, before the next request is
built. Structural, so **on by default** with a file-only `auto-compact: false` opt-out —
this is deliberately NOT under D1 (it is not a Mechanism; Bypass keeps it, D5). Closes
the TODO.md "automatic budget-driven trigger" deferral for real. Retire nothing from the
sim's `compress` beyond what the catalogue says (external-client sniffing was already
decided dead, broad plan §4).

**Tests:** capping trims only over-budget results, preserves head+tail+marker, spares the
newest Turn; auto-compact fires at the threshold, not before; non-reentrant; opt-out key
respected; `/compact` on-demand path unchanged.

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `internal/context`,
`internal/agent`, `cmd/apogee` (config key) + docs/CHANGELOG; TODO.md deferral note
updated in the same commit. Commit:
`feat(context): tool-result capping mechanism and the budget-driven automatic compaction trigger`.

**Depends on:** item 8.

---

## 10. Wave 3 — tool-menu & request shapers: `toolfilter`, `filehint`, `grammar`

**What:** port per the catalogue (including its port-or-drop verdicts — `grammar` in
particular may be dropped or gated on backend capability, D3-injected). `toolfilter`:
pre-request `SetTools` menu narrowing per the sim's relevance rules. `filehint`:
pre-request role-safe `InjectContext` of workspace file hints (the shared inject
primitive already encodes the ends-in-tool-result rule). `grammar` (if ported): `SetExtra`
`response_format` when the backend supports it. Descriptors + registration per the
catalogue.

**Tests:** menu narrowed deterministically and restored next turn (tools are re-set per
request, never mutated globally); hint injected role-safely (ends-in-tool-result case);
idempotency markers prevent double-inject; capability-gated grammar no-ops without
support.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit: `feat(mechanisms): port the toolfilter/filehint request-shaper wave`.

---

## 11. Wave 3 — history-aware hint family: `error_enrichment`, `read_loop`, `read_repeat`, `cached_content_intercept`

**What:** the cross-turn aggregators, at their **relocated** hook points per the
catalogue: `error_enrichment` at post-tool-result (classifies read-vs-write errors from
the originating call — classification stays mechanism-internal, `ToolResult.IsError` is
authoritative); `read_loop` / `read_repeat` detection via the `ConversationView` pairing
helpers (`CallByID`/`ResultFor` — the pairing logic they each hand-rolled in the sim);
`cached_content_intercept` at pre-tool-exec (a redundant re-read of an unchanged path is
intercepted using the `LoopView` history scan). Hint injection goes through the role-safe
primitives with idempotency markers. Descriptors, ordering edges (the decompose coupling
lands in item 12), and any drop verdicts per the catalogue.

**Tests:** ≥2 same-file errors → one enriched hint (marker-deduped); read-loop detected
across turns; redundant read intercepted, novel read untouched; all four suppress
normally (non-exempt).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit: `feat(mechanisms): port the history-aware error/read-loop hint family`.

---

## 12. Wave 4 — `decompose` (+ `cot` / `intent` per the catalogue)

**What:** the task-decomposition family, last of the request shapers because it carries
the known cross-mechanism coupling. `decompose`: pre-request focus/step directives via
`AppendToSystem`(marker) + `InjectContext`, history-collapse of older user messages via
`SetMessageContent`; its read-loop coupling (the sim's `meta.FiredCounts` peek) becomes a
`LoopView.Fired` query or a declared ordering edge — whichever the catalogue ratified
(D2). `Fired` now counts **acted** fires (R4) — the same semantics as the sim's
`FiredCounts`; R5 accepts that counts retained from a cancelled Turn may over-report
toward this coupling. `cot` and `intent`: port or drop exactly per the catalogue's verdict; if ported
they are plain pre-request shapers using the same primitives. This item also picks up
whatever the catalogue assigned as the broad plan's "completion nudges" if they didn't
land in wave 1.

**Tests:** directives injected once (markers); collapse leaves the protected prefix +
latest exchange intact; the coupling actually gates (decompose defers to a fired
read-loop, or the ordering holds).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + docs/CHANGELOG.
Commit: `feat(mechanisms): port the decompose wave and close the request-shaper catalogue`.

---

## 13. `ModelFingerprint` + the Library store

**What:** the learning substrate (CONTEXT "Library"; no Mechanism yet — that is item 14).
`ModelFingerprint`: a confidence-tagged identity — **weights-hash (high)** when the GGUF
file is reachable, **metadata label (low)** otherwise; the **behavioral-probe (medium)**
tier is Phase 5 (`apogee probe`) — design the seam (an enum slot + resolver interface),
do not build the probe (D8). The store: file-backed under `Config.LibraryDir` (the
injected root — `wire.go` supplies the production default; **never** an ambient
`~/.apogee` reach from the library code itself, ADR 0001), holding per-fingerprint
observations with Bayesian confidence counts, load/persist with versioning like
`domain.Session`, process-local (no cross-process locking claims in v1 — document it).
Bench isolation (decision 11) falls out of the injected root.

**Tests:** fingerprint tiers + confidence tags; store round-trip; observation
confidence updates; corrupt/missing store degrades to empty-with-soft-error (matches the
skills-catalog posture); everything stays inside the injected dir (no `$HOME` writes —
assert).

**Acceptance:** gates green; diff confined to a new `internal/library` (+
`internal/domain` for the fingerprint type, `cmd/apogee/wire.go`) + docs/CHANGELOG.
Commit: `feat(library): confidence-tagged model fingerprint and the file-backed store`.

---

## 14. Library Mechanisms: observe + inject

**What:** the Library's two loop-facing halves, per the catalogue and the sim's
`library` package: an **observe** side recording completed-Turn outcomes into the store,
and a **pre-request inject** Mechanism (`AppendToSystem` with marker) that injects
qualifying observations — **confidence gates injection** ("prefer not to inject under
uncertainty": low-confidence fingerprints don't inject). Both are catalogued Mechanisms
(D1: default off) and both go fully inert in Bypass — no inject AND no observe/write
(decision 13; the dispatch gate of item 2 covers this since neither is an off-ramp). The
store is construction-injected (D3, item 4's `deps`). The longitudinal validation
(improves-over-sessions AND never-below-baseline) is bench-side — record it as pending in
the catalogue ledger.

**Tests:** observe writes keyed on the fingerprint; inject only above the confidence
gate, marker-deduped; Bypass ⇒ store file untouched byte-for-byte; isolated roots don't
cross-contaminate (two agents, two dirs).

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `internal/library`,
`cmd/apogee` + docs/CHANGELOG. Commit:
`feat(mechanisms): library observe/inject with confidence-gated injection`.

**Depends on:** item 13.

---

## 15. Bench-readiness proof (the ADR 0001 contract, exercised in-repo)

**What:** a permanent regression proving apogee is drivable exactly the way apogee-sim
will drive it — an integration test (root-package consumer style, like `example_test.go`)
that: constructs **two Agents from one scripted responder** — a mechanisms-on arm
(several waves enabled via `Config`) and a **Bypass arm** — against **isolated temp state
roots** (LibraryDir, SessionsDir); registers an **experimental hook at each of the five
hook points**; `Step`s both to quiescent boundaries; `Snapshot`s and `Resume`s a fork of
each; then asserts: deterministic mechanism order is visible in the
`MechanismFiredEvent` stream (drive the arms so the enabled mechanisms actually **act** —
per R4 an inspect-only invocation books no fired event); the Bypass arm fired no
non-exempt Mechanism yet all five experimental hooks ran; no state bled between arms or forks (Library/session files stay
inside each injected root; nothing written outside them); resumed forks diverge
independently. This is the executable definition of "benchable" — if a future change
breaks the bench contract, this test breaks first.

**Tests:** the item IS the test. Verify: `go test -race -count=1 ./...` including the new
test, plus `make check`.

**Acceptance:** gates green; diff confined to the test file(s) + docs. Commit:
`test: bench-readiness proof of the embeddable two-arm contract`.

**Depends on:** items 2–6 minimum (needs real mechanisms to arm); run it after 14 in
sequence.

---

## 16. Docs close-out + v1.2.0 roll-up

**What:** the release bookkeeping. Roll the accumulated Unreleased CHANGELOG entries into
a `[1.2.0]` section (additive minor — new Config fields, new Events usage, no breaking
change; sanity-check that claim against the diff since `v1.1.0`). Update `TODO.md`: the
auto-compact deferral is closed (item 9); note anything the catalogue dropped
(`codeinfo` et al.) so the deferral trail stays deliberate. CONTEXT.md drift check: the
Mechanism/self-regulation/context vocabulary should already match what was built — fix
any drift *in the code's doc comments or the glossary, whichever is wrong*; include the
review pass's ratified vocabulary (retry-in-place corrections, next-Turn three-way
judgment, acted fires — R1/R3/R4). Finish the
catalogue doc's ledger (every row: shipped-in-item N, bench validation **pending**).
Write a short handoff `docs/handoffs/<date> - 00 - phase-4-complete-bench-campaign-next.md`:
what the bench (apogee-sim) must now build/run — import apogee, two-arm + leave-one-out
A/Bs per ADR 0009, longitudinal Library experiment — and that per-mechanism default
flips happen on wins. **No tag, no push** — cutting `v1.2.0` stays owner-run.

**Acceptance:** gates green; `git status` clean after commit; CHANGELOG/TODO/catalogue/
handoff consistent with the ledger. Commit:
`docs: close out Phase 4 — roll up v1.2.0 notes and hand off the bench campaign`.

---

## Explicitly NOT in this plan

- **The bench A/B campaign** (two-arm, leave-one-out, longitudinal Library) — that is
  apogee-sim work in the apogee-sim repo, after this plan lands (see item 16's handoff).
- **Per-mechanism default flips to ON** — one-line follow-ups gated on bench wins (D1).
- **The behavioral-probe fingerprint / `apogee probe`** — Phase 5 (the seam ships in 13).
- **Windows Confiner, cross-platform matrix** — Phase 5.
- **Any modification to apogee-sim**, any apogee import of apogee-sim, any `sim`/`bench`
  subcommand (ADR 0001 — rejected options stay rejected).
- **The tool×mode security matrix, url-safety config keys, server switching, session UI**
  — separate parked tracks (TODO.md).
