# Mechanism Catalogue — Phase 4 (ratified against the pinned apogee-sim source)

**Status:** ✅ **Ratified 2026-07-04** (Phase-4 plan item 1). This is the **authoritative
map** for wave composition: every catalogued Mechanism apogee will register, its hook point,
descriptor, ordering/incompatibility constraints, the port wave (plan items 5–14), and a
port-or-drop verdict. Where a wave item's *expected* member set disagrees with a row below,
**this catalogue wins** (plan D7) — an implementer must not silently invent a Mechanism this
file does not list, nor ship one it marks DROP.

**Pinned port source:** `github.com/airiclenz/apogee-sim` @
**`d22086701ff9ba8e5565f9587945d6d97434b646`** (`chore: rename project apogee -> apogee-sim`).
Every `path:line` in the *Sim source* column is against that commit. apogee-sim is read-only
reference material — no item modifies it and no apogee code imports it (ADR 0001). The pin is
recorded here (not in a lockfile) because the dependency direction is bench → apogee: apogee
never builds against the sim.

**Evidence method (grounded, honest):** the sim's own
`docs/catalogue.md` (@ the pin) is the canonical prose mirror of its Mechanism layer and
carries **inline A/B figures** (per-Mechanism help-rates, Fisher p-values, baseline turn
counts). Those are cited in the *Prior evidence* column. The raw **trace archive is absent
from this machine** — the sim writes it to `$APOGEE_SIM_HOME`/`~/.apogee-sim/traces`
(`internal/sim/trace_archive.go:108`), which does not exist here — so no row is grounded on a
re-run of the traces; the hook-point mapping is grounded on the **signature survey**
(`docs/design/hook-mutation-api.md`, three-slice survey) plus the sim catalogue's recorded
figures. Every quantitative claim below is quoted from the sim catalogue, not re-measured.

**Vocabulary:** apogee's own (`internal/domain/mechanism.go`) — hook points `pre-request` /
`post-response` / `pre-tool-exec` / `post-tool-result` / `history-rewrite`; Capability
`off-ramp` / `proactive-nudge` / `response-repair`; SuppressionPolicy `strikes-3` / `exempt`.
Bypass keeps only `off-ramp` (ADR 0006 / D5); every catalogued Mechanism ships default-off
until bench-proven (D1).

---

## Ratified catalogue decisions (specific to this map — do not re-litigate)

- **C1 — Exempt is narrowed to true off-ramps.** apogee marks **only** `tool_use_enforcer`
  and `empty_response_recovery` as `SuppressExempt` (plan item 1 spec; ADR 0006 ties exempt to
  the off-ramp Capability). The sim additionally exempts `error_enrichment` and
  `feed_forward_correction` from Adaptive Suppression (`descriptor.go:136-147`); apogee does
  **not** carry that exemption forward — `error_enrichment` becomes a `strikes-3`
  response-repair Mechanism, and `feed_forward_correction` is not a standalone Mechanism at all
  (see C5). Rationale: in apogee, exempt ⇒ survives Bypass; a repair that is not a recovery
  guarantee should not run in the naked-model floor.
- **C2 — The three sim read-loop variants consolidate into one apogee `read_loop`.** The sim
  splits the detector into `read_loop_detector`, `greenfield_read_loop_detector`, and
  `successful_read_loop_detector` (`descriptor.go:89-109`) purely so each variant carries an
  **independent suppression counter**; the three are declared **pairwise-incompatible** — only
  one fires per request, dispatched by `readLoopCandidate` on the greenfield signal. apogee
  folds them into a single `read_loop` Mechanism whose internal branch selection reproduces
  `readLoopCandidate`; the pairwise-incompatibility collapses to branch selection. This matches
  the plan's canonical `read_loop` naming. (Per-variant strike independence is a self-regulation
  detail for item 3, not a catalogue split.)
- **C3 — `compress` is not a Mechanism; it splits three ways (D6).** Tool-result **capping**
  → the `tool_result_cap` pre-request Mechanism (item 9). Generative **Compaction** →
  structural `internal/context/` (item 9, on by default, survives Bypass — **not** a
  Mechanism). History **truncation** → the `truncate_history` history-rewrite Mechanism (item 7).
  The sim's external-client-compaction sniffing (`compress` pre-compressed-content detection) is
  **DROPPED** — apogee owns the loop, there is no external client (broad plan §4).
- **C4 — The "completion nudges" are the `cot` family; they land in item 12.** The broad
  plan's wave-1 "completion nudges" are the three tracked Mechanisms the sim's `cot` Transform
  emits — `stall_nudge`, `list_nudge`, `tool_use_directive` (`catalogue.md` §cot). They did not
  land in items 5/6 (whose scopes are the validate/syntax/autofix and off-ramp sets), so per
  plan item 12's explicit pick-up clause they are assigned to **item 12** alongside `decompose`
  (`cot` is not a fourth member — the sim's `cot` Transform *is* these three nudges; Table C
  records the SPLIT. Prose amended 2026-07-04, review-fixes item 6). In apogee they are three
  plain pre-request `proactive-nudge` Mechanisms; `stall_nudge` ⊥ `list_nudge` (contradictory
  directives on the same surface).
- **C5 — `feed_forward_correction` is folded, not ported.** In the sim it is the exempt
  Mechanism that *delivers* a streaming deferred correction on the next request
  (`response_validator.go`, `session_state.go:StoreCorrection`). apogee expresses exactly this
  as `validate`/`syntax` returning `ActionDefer{Inject}` held in conversation state
  (`hook-mutation-api.md` §4.1; `PostResponseDecision.Inject` survives snapshot/resume). No
  standalone `feed_forward_correction` Mechanism.
  **Amended 2026-07-04 (R1, `docs/plans/phase-4-review-fixes-plan.md` — owner-ratified):**
  the delivery expression is `ActionRetry{Inject}` — retry-in-place — not `ActionDefer`. The
  sim deferred only because its streaming proxy had already sent the response downstream and
  could not unsay it; the apogee loop owns the stream and can reset it (`StreamResetEvent`
  was built for exactly this). On ActionRetry the loop re-streams the corrected request **in
  the same Turn**, appending to the in-flight request — request-scoped, never committed to
  history — the superseded assistant message (text + tool calls, when non-empty) and then
  the `Inject` text as a role-safe user correction, mirroring the sim's own retry builders
  (`retryWithCorrection` `response_validator.go:366-391`, `retryForToolUse`
  `tooluse_enforcer.go:59-83`, `retryForEmptyResponseWithStrategy`
  `empty_recovery.go:131-176`), bounded by the loop's `maxPostResponseRetries = 3` (at the
  cap the last response passes through). Table A `validate`'s "short-circuits cascade on
  fail" becomes literally true via the retry semantics (ActionRetry short-circuits the
  post-response cascade). `ActionDefer` keeps its next-request semantics and remains
  available, but wave 1 no longer uses it. The fold's *substance* stands: still no
  standalone `feed_forward_correction` Mechanism.
- **C6 — `intent` is a shared helper, dropped as a Mechanism.** `internal/intent/intent.go` is
  an intent classifier (`HasActionIntent` / `HasAnalysisIntent` / `LastUserMessage`) consumed by
  `cot`, `decompose`, `tool_use_enforcer`, `empty_response_recovery`, and `library`. It fires no
  hook, has no descriptor, and is not in the sim `Mechanism` enum. It ports **inline** with its
  consumers, never as its own catalogue row.
- **C7 — `codeinfo` is DROPPED (pre-decided).** Broad plan §2 deprioritized it (modest
  measured effect, superseded by shell-out diagnostics); the sim's own A/B shows its specific
  signal is not significant (see its row). Not ported to any wave.

Relocations carried from the survey (plan item 1): `cached_content_intercept` → `pre-tool-exec`;
`error_enrichment` → `post-tool-result`. `grammar` and `filehint` are pre-request (explicit
assignment, hook-mutation-api §8 #7).

---

## Table A — identity & dispatch

One row per catalogued Mechanism (DROP/FOLD/SPLIT rows follow in Table C). "Sim canonical ID"
is apogee-sim's own snake_case spelling (D4); the apogee ID equals it except where C2/C3/C4
consolidate or rename.

| apogee ID | Sim canonical ID | Sim source (@pin) | Hook point | Capability | Suppression | Ordering / incompatibility |
|---|---|---|---|---|---|---|
| `tool_use_enforcer` | `tool_use_enforcer` | `internal/proxy/tooluse_enforcer.go`; desc `descriptor.go:57` | post-response | off-ramp | exempt | none — the sim's 3/Session enforcer counter is **not ported** (R2, review-fixes plan); the shared loop retry cap of 3 (`maxPostResponseRetries`) substitutes |
| `empty_response_recovery` | `empty_response_recovery` | `internal/proxy/empty_recovery.go`; desc `descriptor.go:83` | post-response | off-ramp | exempt | none — the sim's 2-retry cap + per-Turn cooldown are **not ported** (R2, review-fixes plan); the shared loop retry cap of 3 (`maxPostResponseRetries`) substitutes |
| `read_repeat` | `read_repeat_interceptor` | `internal/proxy/read_repeat_interceptor.go`; desc `descriptor.go:117` | post-response | response-repair | strikes-3 | Before `validate` in cascade (was 'After' — corrected to match §Ordering + sim `response_analysis.go` `detectRepeatReads` @L54 before validate @L71; owner-ratified 2026-07-04); IncompatibleWith `cached_content_intercept` |
| `tool_loop_interceptor` | `tool_loop_interceptor` | `internal/proxy/tool_loop_interceptor.go`; desc `descriptor.go:124` | post-response | response-repair | strikes-3 | Before `validate` in cascade (fires on 2nd identical turn; 30s cooldown) |
| `validate` | `feed_forward_correction`¹ | `internal/validate/{validate,bridge}.go`; `internal/proxy/response_validator.go` | post-response | response-repair | strikes-3 | Before `syntax`,`autofix` (short-circuits cascade on fail) |
| `syntax` | (untracked analyzer) | `internal/syntax/{syntax,go_check,generic_check}.go` | post-response | response-repair | strikes-3 | After `validate`,`autofix` (corrects the post-repair remainder); own per-Session syntax-fail counter |
| `autofix` | (untracked analyzer) | `internal/autofix/{autofix,formatters}.go` | post-response | response-repair | strikes-3 | After `validate`; Before `syntax` (repair precedes correction — sim `response_analysis.go:72-88`; in-process gofmt always; external formatters LookPath-cached at construction) |
| `correct_tool_result` | `correct_tool_result` (lab-only) | `internal/sim/intervention.go:22,94` | post-tool-result | response-repair | strikes-3 | none — **DEFERRED (owner-ratified 2026-07-04): not ported — no production trigger in the source; the bench experiments via an experimental post-tool-result hook (see Table B)** |
| `truncate_history` | `truncate_history` (lab-only) | `internal/sim/intervention.go:23,99` | history-rewrite | proactive-nudge² | strikes-3 | IncompatibleWith `guided_decomposition` (F7 — a mid-Exchange truncation longer than its keep window can drop the enumeration message the fan-out cursor re-derives from; post-v1.3.0 review-fixes item 8, declared one-sided on the `guided_decomposition` descriptor, 2026-07-06); otherwise none — cut only at `AssistantBoundaries()`, never `PrefixEnd()` |
| `tool_result_cap` | `context_compression` (cap half) | `internal/compress/compress.go` (`capToolResults` `:428/431`) | pre-request | proactive-nudge² | strikes-3 | After `decompose` — runs last among pre-request shapers (ratified 2026-07-04, review-fixes item 11 / option A; was "none" — §Ordering seed now declared, D7 amendment rule); protects the most-recent Turn; per-result 40%-budget cap |
| `toolfilter` | `tool_filtering` | `internal/toolfilter/toolfilter.go:33,70` | pre-request | proactive-nudge | strikes-3³ | Before `decompose` (trim menu before user-msg rewrite) |
| `filehint` | `file_hint` | `internal/filehint/filehint.go`; `internal/proxy/file_hint_detector.go`; desc `descriptor.go:130` | pre-request | proactive-nudge | strikes-3 | none (greenfield-suppressed internally) |
| `grammar` | `grammar` | `internal/grammar/grammar.go`; `internal/proxy/proxy.go:625` | pre-request | proactive-nudge | strikes-3³ | none — backend-capability gated (D3; see Table C) |
| `error_enrichment` | `error_enrichment` | `internal/proxy/error_enrichment.go`; desc `descriptor.go:136` | post-tool-result | response-repair | strikes-3 (C1) | none (classifies read-vs-write from originating call) |
| `read_loop` | `read_loop_detector` (+ greenfield/successful) | `internal/proxy/read_loop_detector.go`; desc `descriptor.go:89-109` | pre-request | proactive-nudge | strikes-3 | IncompatibleWith `cached_content_intercept`, `read_repeat` (C2 folds the 3 sim variants) |
| `cached_content_intercept` | `cached_content_intercept` | `internal/proxy/cached_content_intercept.go`; desc `descriptor.go:110` | pre-tool-exec | proactive-nudge | strikes-3 | IncompatibleWith `read_loop`, `read_repeat` (relocated from request-prep) |
| `decompose` | `prompt_decomposition` | `internal/decompose/decompose.go:89`; desc `descriptor.go:148` | pre-request | proactive-nudge | strikes-3 | After `toolfilter`; muted when `read_loop` has Fired (D2 — `Fired` query or ordering edge) |
| `stall_nudge` | `stall_nudge` | `internal/cot/cot.go`; desc `descriptor.go:63` | pre-request | proactive-nudge | strikes-3 | Before `toolfilter` (ratified 2026-07-04, review-fixes item 11 / option A; was "none" — §Ordering seed now declared, D7 amendment rule); IncompatibleWith `list_nudge`; 4-nudge cap |
| `list_nudge` | `list_nudge` | `internal/cot/cot.go`; desc `descriptor.go:70` | pre-request | proactive-nudge | strikes-3 | Before `toolfilter` (ratified 2026-07-04, review-fixes item 11 / option A; was "none" — §Ordering seed now declared, D7 amendment rule); IncompatibleWith `stall_nudge`; 3-nudge cap |
| `tool_use_directive` | `tool_use_directive` | `internal/cot/cot.go`; desc `descriptor.go:77` | pre-request | proactive-nudge | strikes-3 | Before `toolfilter` (ratified 2026-07-04, review-fixes item 11 / option A; was "none" — §Ordering seed now declared, D7 amendment rule); fires only before first tool use |
| `library` | `library_injection` + observer | `internal/library/{transform,observer,store}.go` | pre-request (inject); **post-response (observe)** — hook point decided 2026-07-04, item 14 | proactive-nudge | strikes-3⁴ | Before `toolfilter` (inject) — ratified 2026-07-04, review-fixes item 11 / option A; was "none" — §Ordering seed now declared, D7 amendment rule; confidence gates injection; fully inert in Bypass (inject **and** observe) |

¹ The sim tracks `validate` indirectly: validation itself is untracked, but its **streaming
deferred correction** is the exempt `feed_forward_correction` Mechanism. apogee folds that path
into `validate`'s `ActionRetry{Inject}` retry-in-place delivery (C5 as amended 2026-07-04), so
the apogee `validate` Mechanism subsumes both.
² Context-shapers (`truncate_history`, `tool_result_cap`) are neither off-ramps nor
response-repairs; classified `proactive-nudge` so Bypass disables them (D5) while the structural
Budget + Compaction stay on (D6). Not a nudge to the model in the literal sense — the label
carries the Bypass semantics only.
³ Untracked in the sim (structurally gated there); apogee makes them catalogued `strikes-3`
Mechanisms so they self-regulate uniformly. Noted per-row so the divergence is explicit.
⁴ The sim does not per-fire-track `library` — Bayesian score-gating is its throttle (ADR 0009
sim). apogee registers it as a catalogued Mechanism (D1 default-off, Bypass-inert); its
injection gate remains confidence-driven, with `strikes-3` as the uniform self-regulation
backstop.

**Library observe hook point — decided 2026-07-04 (item 14):** the observer half is a
**post-response** hook. The sim's observer runs on the completed request-response cycle; apogee's
post-response hook is that point, giving the observer the response's tool calls, the tool menu, and
the conversation via `resp.View()`. The single `library` catalogue row is realized as ONE Mechanism
implementing BOTH hooks (`PreRequest` inject + `PostResponse` observe) — splitting it would need a
second catalogue ID this map does not list (D7). The observer is a pure reader: it returns the zero
decision and never mutates the response, so it books no fire (R4) and never short-circuits the
post-response cascade. Injection is gated on the fingerprint confidence tier (≥ medium — a
low-confidence metadata label does not inject); observe records on any identified model.

---

## Table B — port decision, wave, evidence, bench status

| apogee ID | Port wave (item) | Verdict + one-line rationale | Prior evidence (sim @pin) | Bench validation |
|---|---|---|---|---|
| `tool_use_enforcer` | Wave 1 — **item 6** | PORT — recovery guarantee; without it a text-only turn has no off-ramp | `catalogue.md` §tool_use_enforcer (3-retry cap; de-exempt siblings recorded) | pending (leave-one-out, ADR 0009) |
| `empty_response_recovery` | Wave 1 — **item 6** | PORT — recovery guarantee; without it an empty turn ends the conversation | `catalogue.md` §empty_response_recovery (escalating-temp retries) | pending |
| `validate` | Wave 1 — **item 5** | PORT — tool-call validation; carried most of the measured win | `catalogue.md` response cascade (validate→syntax→autofix short-circuit) | pending |
| `syntax` | Wave 1 — **item 5** | PORT — write-content syntax check (Go parser + generic) | `catalogue.md` §syntax (per-Session fail counter) | pending |
| `autofix` | Wave 1 — **item 5** | PORT — formatter write-back; gofmt in-process, others optional (§3a) | `catalogue.md` §autofix (LookPath-cached formatter table) | pending |
| `correct_tool_result` | — (deferred from item 7) | **DEFER (owner-ratified 2026-07-04)** — no production trigger exists to port; the shipped post-tool-result hook + mutation API is the lab surface (the bench plays the sim's operator via an experimental hook); a bench-discovered trigger motivates a NEW plan item | lab-only intervention, operator-supplied correction — "a finding that motivates a new production surface, not a 1:1 port" (`intervention.go:12-15`) | deferred (bench discovery precedes any port) |
| `truncate_history` | Wave 2 — **item 7** | PORT — cheap A/B alternative to Compaction; off by default (D1) | lab-only intervention; drop-the-middle (`intervention.go:99-178`) + static gap-note insertion (`:180-181`) | pending |
| `tool_result_cap` | Context — **item 9** | PORT — surviving half of `compress`; 40%-budget per-result cap | `catalogue.md` §compress (40% cap, most-recent-turn protected) | pending |
| `toolfilter` | Wave 3 — **item 10** | PORT — tool-menu narrowing (30+ tools or observed hallucination) | `catalogue.md` §filter (structurally gated) | pending |
| `filehint` | Wave 3 — **item 10** | PORT — role-safe workspace hint; TF-IDF-ish scoring | `catalogue.md` §file_hint (greenfield-suppressed) | pending |
| `grammar` | Wave 3 — **item 10** | PORT (capability-gated) — `response_format` only when the backend needs it | `catalogue.md`+ADR 0007 sim: fires only on llama.cpp **without** native tool-calls | pending (may no-op on all current apogee backends) |
| `error_enrichment` | Wave 3 — **item 11** | PORT — read-vs-write error clarification; relocated to post-tool-result | `catalogue.md` §error_enrichment (exempt in sim; C1 narrows) | pending |
| `read_loop` | Wave 3 — **item 11** | PORT — failed-re-read detector; 3 sim variants consolidated (C2) | `catalogue.md` §read_loop/§greenfield/§successful (threshold 1 vs 2 by greenfield) | pending |
| `cached_content_intercept` | Wave 3 — **item 11** | PORT — redundant successful-re-read interceptor; relocated to pre-tool-exec | `catalogue.md` §cached_content_intercept: **help_rate 0.73** (11/4/1), repeated_tool_call 0.91→0.15/run (gpt-oss-20b); inert-but-correct on gemma | pending |
| `tool_loop_interceptor` | Wave 3 — **item 11** | PORT — identical-repeat-turn detector; **inventory-missed, found in checkout** | `catalogue.md` §tool_loop_interceptor (atomic decision, 30s cooldown) | pending |
| `decompose` | Wave 4 — **item 12** | PORT — one-step focus + history-collapse; read-loop coupling → `Fired`/ordering | `catalogue.md` §decompose (mute-on-read-loop, stop at completedSteps) | pending |
| `stall_nudge` | Wave 4 — **item 12** | PORT — completion nudge (C4); read-only stall → proceed-with-writes | `catalogue.md` §stall_nudge (threshold 4, cap 4; 11-fire/0%-compliance baseline motivation) | pending |
| `list_nudge` | Wave 4 — **item 12** | PORT — completion nudge (C4); list-without-read → read | `catalogue.md` §list_nudge (threshold 2, cap 3) | pending |
| `tool_use_directive` | Wave 4 — **item 12** | PORT — completion nudge (C4); action-intent + no tool use → use tools | `catalogue.md` §tool_use_directive (de-exempted 2026-05-23) | pending |
| `library` | Library — **item 14** | PORT — cross-session learning; observe + confidence-gated inject | `catalogue.md` §library + ADR 0009 sim (Bayesian `(obs-succ+1)/(obs+2)`, gate 0.5/2 obs) | pending (longitudinal: improves-over-sessions AND never-below-baseline) |

---

## Table C — DROP / FOLD / SPLIT (non-ported inventory members, for the deliberate trail)

| Inventory member | Sim canonical ID | Disposition | Rationale + evidence |
|---|---|---|---|
| `codeinfo` | `codeinfo` (untracked) | **DROP** | Broad plan §2 deprioritized (modest effect, superseded by shell-out diagnostics). Sim A/B (gpt-oss-20b-MXFP4, `propagate-lookup-rename`, N=75/arm): full pipeline good-rate 54.7% vs 32.0% (+22.7pp, Fisher p=0.008) is multi-stage; the codeinfo-specific missed-call-site shape 37→30 is **not significant** (OR 0.69, p=0.32). Not ported. |
| `intent` | — (helper) | **FOLD (helper)** | Shared intent classifier (`intent.go`), no hook/descriptor; ports inline with `cot`/`decompose`/`tool_use_enforcer`/`empty_response_recovery`/`library` (C6). |
| `feed_forward_correction` | `feed_forward_correction` | **FOLD into `validate`** | The streaming deferred-correction delivery path; apogee expresses it as `ActionRetry{Inject}` — retry-in-place, appended to the in-flight request (C5 as amended 2026-07-04; hook-mutation-api §4.1). No standalone Mechanism. |
| `compress` | `context_compression` | **SPLIT (D6)** | → `tool_result_cap` (item 9, Mechanism) · generative Compaction (item 9, structural `context/`, on in Bypass, **not** a Mechanism) · `truncate_history` (item 7, Mechanism). External-client-compaction sniffing **DROPPED** (no external client — broad plan §4). |
| `cot` | `cot` (Transform, untracked) | **SPLIT → `stall_nudge` / `list_nudge` / `tool_use_directive` (C4)** | The sim's `cot` Transform is not itself a tracked Mechanism — it emits the three tracked completion nudges (`internal/cot/cot.go`; desc `descriptor.go:63/70/77`). They port as three plain pre-request `proactive-nudge` Mechanisms in item 12. (Row added 2026-07-04, review-fixes item 6 — the SPLIT was decided in C4 but missing from this table.) |
| `read_loop_detector`, `greenfield_read_loop_detector`, `successful_read_loop_detector` | same | **CONSOLIDATE → `read_loop`** | Three sim variants exist only to give each an independent suppression counter and are pairwise-incompatible (one fires per request). Folded into one apogee `read_loop` with internal branch selection (C2). |

---

## Ordering carried from the sim (source for item-2 constraint edges)

The catalogue records the sim's declared orders so item 2's deterministic topo-sort
(stable tiebreak by canonical ID, D4) has a grounded seed. These are *declared* Before/After
edges, not the total order (the loop computes that).

**Ratified 2026-07-04 (review-fixes item 11 / option A):** the pre-request seeds below are now
*live* code edges (`OrderingConstraints`) — the `cot` nudges (`stall_nudge`, `list_nudge`,
`tool_use_directive`) and `library` declare `Before toolfilter`, and `tool_result_cap` declares
`After decompose` (so it sorts last among the shapers). The matching Table A cells, which read
"none" through item 12, were amended per D7 to record these edges, so §Ordering, Table A, and the
code now agree; the resulting order is pinned by `TestPreRequestOrderingSeeds`
(`internal/mechanisms/catalogue_test.go`).

- **Pre-request pipeline (sim Transform order, `catalogue.md` §Pipeline ordering):**
  `cot` → `library` → `codeinfo`(dropped) → `filter` → `decompose` → `compress`(split).
  apogee edges: the `cot` nudges and `library` inject before `toolfilter`; `toolfilter` before
  `decompose`; `tool_result_cap` runs last among pre-request shapers (it trims after context is
  assembled). `filehint`/`grammar`/`read_loop` are request-prep injectors with no hard order
  against the transforms beyond the incompatibility edges in Table A.
- **Post-response cascade (sim, `catalogue.md` §Response-side detection cascade):**
  `read_repeat` → `tool_loop_interceptor` → `validate` → (if valid) `autofix` → `syntax`:
  the sim repairs before it corrects — detect → `tryAutoFix` → correct-the-remainder
  (`internal/proxy/response_analysis.go:72-88` @pin) — so `autofix` precedes `syntax` or the
  correction stage re-corrects issues a formatter would have fixed (amended 2026-07-04,
  review-fixes item 3). `validate` short-circuits `autofix`/`syntax` on failure. The two
  text-side off-ramps (`tool_use_enforcer`, `empty_response_recovery`) run separately on
  text-only/empty responses.
- **Cross-mechanism coupling:** `decompose` mutes when a `read_loop` variant has fired this
  Session (sim `RequestMeta.FiredCounts` peek, `decompose.go` gate) → apogee `LoopView.Fired`
  query or a declared ordering edge (D2). `stall_nudge` ⊥ `list_nudge` (contradictory
  directives). The read-loop / re-read family (`read_loop`, `read_repeat`,
  `cached_content_intercept`) is pairwise-exclusive on the same wasted-read symptom.

Self-regulation constants the sim ships (for item 3, not this item): Adaptive Suppression =
3 consecutive not-helped → suppress for the Session; Turn Budget = 8 consecutive non-productive
Turns → suppress all non-exempt; productive-Turn clear-path (default `zero`).

---

## Open question surfaced for a later wave (not blocking item 1) — RESOLVED

- **`correct_tool_result` production trigger (item 7).** In the sim this is a **lab-only**
  intervention (`intervention.go:12-13`: "lab-only kinds with no production counterpart") —
  the operator supplies the correction; nothing detects a correctable tool result on its own.
  The `PostToolResult(ctx, call, result, view)` signature already carries the originating call
  and view it would need, but the **gating logic does not exist in the source**.
  **RESOLVED 2026-07-04 (owner-ratified): DEFER.** Not ported — inventing gating would ship
  behavior with no sim evidence (D7 as amended, phase-4-detail-plan). The shipped
  post-tool-result hook + mutation API already gives the bench the lab surface the sim's
  operator had; a bench-discovered trigger motivates a new plan item and a fresh Table B
  verdict. Table A/B rows + ledger amended to match; detail-plan item 7 now ships
  `truncate_history` only.

---

## Ledger (shipped-in-item · bench validation)

Every ported row's **bench validation is `pending`** — no per-Mechanism default flips to ON in
this plan (D1/D8); flips are one-line follow-ups gated on the bench A/B campaign (see the
`docs/handoffs/2026-07-04 - 00 - phase-4-complete-bench-campaign-next.md` handoff). "Shipped in
item N" was filled as each wave landed; **the ledger is closed here (item 16, 2026-07-04)** —
every ported Mechanism carries its shipping item and a **pending** bench validation, and every
DROP / FOLD / SPLIT / DEFER row carries its verdict. Nothing remains porting-undecided.

| apogee ID | Shipped in item | Bench validation |
|---|---|---|
| `validate`, `syntax`, `autofix` | 5 | pending |
| `tool_use_enforcer`, `empty_response_recovery` | 6 | pending |
| `truncate_history` | 7 | pending |
| `correct_tool_result` | — (DEFER, owner-ratified 2026-07-04) | n/a until a bench-discovered trigger |
| `tool_result_cap` | 9 | pending |
| `toolfilter`, `filehint`, `grammar` | 10 | pending |
| `error_enrichment`, `read_loop`, `read_repeat`, `tool_loop_interceptor`, `cached_content_intercept` | 11 | pending |
| `decompose`, `stall_nudge`, `list_nudge`, `tool_use_directive` | 12 | pending |
| `library` | 14 | pending |
| `codeinfo` | — (DROP, C7) | n/a |
| `intent` | — (FOLD helper, C6) | n/a |
| `feed_forward_correction` | — (FOLD into `validate`, C5) | n/a |
| `compress` | — (SPLIT, C3) | n/a |
| `cot` | — (SPLIT → `stall_nudge`/`list_nudge`/`tool_use_directive`, C4) | n/a |

### Bench campaign evidence (L9 — keyed by campaign ID + model)

The porting ledger above closed 2026-07-04 (item 16); this subsection is the **append-only
evidence stream** it awaits. Per L9 (apogee-sim
`docs/plans/leave-one-out-campaign-plan.md`), completed bench campaigns land here as
**ledger entries only** — no default flips, no catalogue deletions; curation decisions wait
for the Screen + Confirmation pair. Bundles (manifest, `runs.jsonl`, traces, `report.md`,
`analysis.json`) live in `~/.apogee-sim/campaigns/<campaign ID>/`.

#### `gemma-4-e4b-it-qat-20260706` · gemma-4-e4b-it-qat — **inferior** (aggregate A/B)

- **Design:** full-stack aggregate — candidate (17 mechanisms) vs Bypass floor (ADR 0006);
  14 tasks × 2 arms × 5 reps = 140/140 recorded, 0 infra_failed (2026-07-06).
- **Gate (ADR 0009):** candidate **not** non-inferior to Bypass within δ = 0.4048
  (split-half AA-null 95th percentile); one-sided Wilcoxon signed-rank W+ = 66.0,
  p = 0.2087, N = 14 paired tasks.
- **Secondaries — every one favors Bypass:** mean grade 2.400 vs 2.757; gate pass 33/70 vs
  45/70; compile 70% vs 87%; tests 147P/53F vs 188P/22F; lint-clean 8/70 vs 21/70.
- **Reading:** the full stack **hurts** this small model. The campaign is aggregate-only —
  it attributes nothing to individual mechanisms, so every Table B `pending` stands.
  Attribution is the job of the leave-one-out Screen `gemma-4-e4b-it-qat-20260708`
  (1,190 runs, launched 2026-07-08, in flight as of this entry), then a Confirmation
  campaign on the pruned set.

#### `qwen25-coder-14b-20260707` · qwen25-coder-14b — **no-evidence** (aggregate A/B)

- **Design:** same aggregate design; 140/140 recorded, 0 infra_failed, 3 h 30 m
  (2026-07-07).
- **Gate (ADR 0009):** **no-evidence** — every task produced the same letter grade in all
  10 of its runs (both arms, all reps) ⇒ zero non-zero paired diffs ⇒ min attainable
  one-sided p = 1.0000 (δ = 0.0000 AA null). No-evidence still **fails** the gate
  (inconclusive ≠ pass). Secondary aggregates byte-identical between arms (mean 1.786,
  gate 15/70, compile 71%, tests 95P/65F, lint 10/70).
- **Not a rig bug — wiring verified before believing it:** manifest arms differ only by
  `Bypass: false/true` (correct bypass-floor design); candidate-vs-bypass trace files
  differ for every pair hashed — different transcripts converging on the same outcome
  buckets.
- **Zero-variance caveat (binds any future capable-model claim):** the letter-grade
  instrument shows **zero within-task variance** on this model despite temp 0.7 —
  confirmed twice (this aggregate, then the 34-run Screen smoke
  `qwen25-coder-14b-20260707-smoke`: all 17 arms identical grades per task). The
  instrument cannot measure "helps" on capable models; any such claim first needs a finer
  instrument or harder corpus.
- **Efficiency observation (recorded, unmined — no graded claim):** many candidate-arm
  runs finished in 6–25 s vs minutes for Bypass with identical grades (walls in
  `~/campaign-run.log`; traces preserved). Plausibly `cached_content_intercept`. If mined
  and confirmed, this is same-outcome-less-compute "helps where it can" evidence — for
  now it is an observation, not a verdict.
- **Reading:** the stack is **outcome-neutral** on this capable coder. No per-mechanism
  attribution; every Table B `pending` stands.

Not entered: `qwythos-9b-20260707` (16/140, model abandoned mid-campaign — think-block
death spirals; L9 admits completed campaigns only, so its record stays in the bundle and
the 2026-07-08 handoff) and `qwen25-coder-14b-20260707-smoke` (rig-acceptance smoke for
plan item 7, cited above only as the zero-variance replication — not mechanism evidence).

Working hypothesis these two entries jointly support (hypothesis, not a verdict): the
stack is outcome-neutral on capable models and harmful on weak ones — "gets out of the
way" is the binding constraint, and the gemma Screen + Confirmation pair is the critical
path to naming the harm.
