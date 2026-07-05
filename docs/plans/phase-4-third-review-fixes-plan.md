# Plan — Phase-4 third-review fixes (second-review-fixes wave findings)

**Date:** 2026-07-05
**Status:** ready to run.
**How to run:** `implement-plan docs/plans/phase-4-third-review-fixes-plan.md with skills: coding-standards`
(the broad plan's standing requirement #1 makes `coding-standards` mandatory for every item).
**Source of direction:** the 2026-07-05 four-lens review of the phase-4 second-review fixes wave
(`docs/code-review-2026-07-05.md`) — 2 High / 7 Medium findings, no Critical;
`docs/plans/archived/phase-4-detail-plan.md` (D1–D8), `docs/plans/archived/phase-4-review-fixes-plan.md`
(R1–R5), and `docs/plans/archived/phase-4-second-review-fixes-plan.md` (S1–S6) still bind;
`docs/design/mechanism-catalogue.md` (composition); the pinned sim @
`d22086701ff9ba8e5565f9587945d6d97434b646` at `~/Repos/Airic/apogee-sim` (behaviour).
**Verify gate (every item):** `make check` (gofmt-clean, vet, build, race tests, ADR-0010
invariant) plus the item's own test commands. "Gates green" means exactly this.

**Precedence (points at ground truth, never at an artifact of this plan):** for *behaviour*,
the pinned sim source wins; for *composition/descriptors*, catalogue Table A wins; the review
report is the findings record — if the code you find contradicts a finding's premise (e.g.
already fixed), re-verify against the code and say so in NOTES rather than "fixing" a
non-defect. Every `file:line` below was verified 2026-07-05 against `183eabb`; earlier items
of this plan may shift them — re-locate by symbol name before editing.

**Deviation trail:** any authorized deviation from an item's text lands as a dated NOTES
line under that item — no silent divergence. Two of this review's nine findings exist
because a prior NOTES claim was falsified or an edge went untested; the trail is the point.

**Before running:** commit `docs/code-review-2026-07-05.md` and this plan file first
(suggested: `docs: add the phase-4 third-review fixes plan and file the review report`) —
the implement-plan preflight stops on a dirty tree.

---

## Design record (from the 2026-07-05 review; running this plan ratifies F1–F4 — flag STATUS QUESTION rather than deviate)

- **F1 — A skipped fold never latches saturation.** `apogeectx.Compact` returns
  `Result{Skipped: true}` *without folding* when too few messages sit past the protected
  prefix (`internal/context/compact.go:57`, `minCompactTail`). A skip proves nothing about
  whether folding can help, and re-checking at the next Exchange opening costs no upstream
  call — so the S2 saturation latch (`compactSat`) may be set only after a fold that
  actually RAN and still left the history over its allocation. This is what S2's own text
  already says ("a fold that does NOT bring the history estimate back under…"); the code
  discarding `Result` is the defect, not a ratified behaviour.
- **F2 — `committedLen` is a maintained boundary, not a snapshot length.** Item 10 froze
  `committedLen` at the first `AppendSupersededAssistant` so post-response scanners'
  `View()` sees only committed history. The empty-superseded path breaks it: nothing is
  appended, so the retry correction (`InjectContext`) lands *below* the boundary — the
  insert-before-last-user branch evicts the real user ask from `View()`, and the
  system-prepend branch shifts every index while the boundary stays frozen (both
  empirically reproduced). Ratified fix — boundary maintenance, NOT placement rerouting:
  when `committedLen >= 0`, (a) an `insertMessage` at an index `< committedLen` increments
  `committedLen`, and (b) `appendOrCreateSystem`'s *prepend* branch increments
  `committedLen`. The model-facing request (`State()`) stays byte-identical — no
  sim-placement question is reopened. Accepted residual, recorded here: a request-scoped
  correction remains *visible* inside `View()` (as a user message before the last user
  message, or as system-message content) — it no longer evicts or shifts committed
  history, and the post-response scanners key on assistant/tool messages and the LAST user
  message, to which a mid-history user message is inert. If the implementer finds a
  scanner that keys on mid-history user messages or on system content, STATUS QUESTION
  instead of extending the design.
- **F3 — The Library sanitizer strips all of Cc, Cf, Co, Cs.** `unicode.IsControl` covers
  only Cc (nothing above U+00FF), so bidi overrides (U+202A–202E, U+2066–2069), zero-width
  characters (U+200B/C/D), BOM (U+FEFF) and soft hyphen survive today. Extend the existing
  strip branch with `unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs)` (a
  graphic-plus-space whitelist is an acceptable alternative if it preserves the existing
  fold-newlines-then-collapse-runs semantics exactly).
- **F4 — Example-call "parameter names" are schema-intersected.** The S4 "names, not
  values" mitigation recorded the *keys of the model's arguments object* — free-form
  model-controlled strings (`validateArguments` only checks required params are present,
  so junk keys ride along on clean observations). Ratified: intersect the recorded keys
  with the tool's declared schema `properties`; a key not in the schema is dropped. When
  no schema properties are derivable (absent/unparsable schema), skip the example
  observation entirely — "prefer not to record under uncertainty", mirroring the
  Library's confidence-gated injection. This amends S4; item 3 owns the dated amendment
  note.

---

## 1. Auto-compaction: gate the saturation latch on a fold that ran — ✅ DONE (2026-07-05)

**Finding:** review "A skipped fold latches the S2 saturation latch, permanently disabling
auto-compaction" (High, found independently twice). Ground truth: S2
(`docs/plans/archived/phase-4-second-review-fixes-plan.md:49-56` — "a fold that does NOT
bring the history estimate back under the allocation"); `internal/context/compact.go:40,57`
(the `Skipped` no-op); the latch at `internal/agent/compact.go:73-94`, the clear path at
`:117-119`.

**What:** implement F1 in `internal/agent/compact.go`. `autoCompact` captures the result —
`res, err := apogeectx.Compact(…)` — and, after the existing error handling, returns before
the saturation check when `res.Skipped` (nothing folded ⇒ nothing proved; the trigger
re-checks at the next Exchange opening for free). Update the function's doc comment
(`compact.go:60-66,80-84`) so "a successful fold that STILL leaves the history over its
allocation" is literally the only latch path; adjust the ErrorEvent wording if needed so it
stays true in the only case that now reaches it (a completed fold leaves prefix + summary +
protected tail; the current "protected prefix … alone exceeds it" text is acceptable if the
implementer verifies that is the only remaining over-allocation shape post-fold — otherwise
name "the protected prefix and compaction summary").

**Tests:** in `internal/agent/autocompact_guard_test.go`: an Exchange-boundary conversation
of `prefix + one oversized assistant answer` (over-allocation, but ≤1 message past the
protected prefix so `Compact` skips) → no `ErrorEvent`, no latch — proven by a later
boundary where a foldable multi-message tail exists → the fold RUNS (summarizer called);
the existing saturation tests (one ErrorEvent, zero summarizer calls while latched, re-arm
on drop below allocation) keep passing.

**Acceptance:** gates green; diff confined to `internal/agent` + CHANGELOG. Commit:
`fix(agent): saturate auto-compaction only after a fold that ran`.

---

## 2. `committedLen` boundary maintenance for the empty-superseded retry — ✅ DONE (2026-07-05)

**Finding:** review "`committedLen` retry-view bound is defeated when the superseded
response is empty" (High, empirically reproduced in both shapes). Ground truth: item 10's
invariant (`docs/plans/archived/phase-4-second-review-fixes-plan.md:401-448` — corrections
must not masquerade as committed history); the freeze-before-short-circuit at
`internal/domain/hooks.go:410-416`; `InjectContext`'s placement branches at `:376-393`;
`appendOrCreateSystem` at `:458-470`; `View()`'s bound at `:330-337`; the retry seam at
`internal/agent/loop.go:315-320`. The sim comparison for View semantics is the item-10
NOTES' finding: the sim's detectors ran against the ORIGINAL committed request on every
retry iteration.

**What:** implement F2 in `internal/domain/hooks.go`. When `committedLen >= 0`:
`InjectContext`'s insert-before-last-user branch (`insertMessage` at an index
`< committedLen`) increments `committedLen`; `appendOrCreateSystem`'s prepend branch
(`:469`) increments `committedLen`. No other placement changes — `State()` must remain
byte-identical for every shape (assert this in the tests). Leave a short comment at the
increment sites naming the invariant: the boundary tracks the same logical message across
request-scoped structural mutations. The append-to-existing-system branch (`:461-466`)
changes no indices and stays untouched (its content-visibility residual is accepted in F2).

**Tests:** loop-level through the scripted harness (`internal/agent/retryview_test.go`
patterns), with `empty_response_recovery` enabled: (a) Exchange-opening turn, model returns
an empty response, retry fires → during the retry's post-response pass the view's last user
message is the REAL user ask, not the recovery nudge, and no committed message is missing
from `View()`; (b) tool-continuation turn (request carries no domain-level system message),
empty response, retry fires → `View()` still ends at the newest tool result
(`ResultFor` on the latest call returns it; no dangling assistant tool-call tail); (c) a
second consecutive empty-retry in the same Turn keeps the boundary correct (multi-retry
accumulation); (d) the existing non-empty retryview tests and the R1 retry-exchange tests
(`retryexchange_test.go`) stay green, and `State()` for shapes (a) and (b) is byte-identical
to the pre-fix construction (the appendage + correction still reach the model unchanged).

**Acceptance:** gates green; diff confined to `internal/domain`, `internal/agent` (tests) +
CHANGELOG. Commit:
`fix(domain): maintain the retry-view boundary across below-boundary injections`.

---

## 3. Library S4 hardening: format-character strip + schema-filtered param names

**Finding:** review "SanitizeContent lets Unicode format characters through" (Medium,
Security) + "Recorded 'parameter names' are free-form model-chosen JSON keys" (Medium,
Security, warrants revisiting S4). Ground truth: S4
(`docs/plans/archived/phase-4-second-review-fixes-plan.md:62-69`);
`internal/library/store.go:334` (`SanitizeContent`);
`internal/mechanisms/library.go:292-310` (`observeSuccessfulComplexToolCalls`) and
`:312-330` (`libraryArgParamNames`); the tool schemas already flow in as the
`tools []domain.ToolDef` parameter.

**What:** implement F3 and F4.
**(a)** `internal/library/store.go`: extend `SanitizeContent`'s strip to Cf/Co/Cs per F3.
No change to the fold/collapse order or the no-length-cap decision (both ratified in the
second-review plan's item-4 NOTES).
**(b)** `internal/mechanisms/library.go`: `observeSuccessfulComplexToolCalls` resolves the
called tool's schema from `tools`, and the recorded params become the sorted intersection
of the arguments' keys with the schema's declared `properties`; no derivable properties ⇒
no example observation for that call (F4). The complexity gate (5+ params) applies to the
FILTERED set, so junk keys cannot promote a simple call to "complex" — verify and state in
NOTES whether the existing gate already sits after the filter point.
**(c) Owning doc amendment:** append a dated amendment note under S4 in
`docs/plans/archived/phase-4-second-review-fixes-plan.md` recording F3/F4 (what the
2026-07-04 mitigation missed, what now closes it).

**Tests:** `internal/library/store_test.go`: content carrying U+202E, U+200B, U+FEFF,
U+00AD (and a plain Cc control as regression) → stored entry contains none of them;
`internal/mechanisms/library_test.go`: a pre-seeded store whose entry carries bidi/zero-width
characters → the rendered injection block is clean (render-path defence); a valid tool call
carrying an extra non-schema key with directive text → the recorded example lists only
schema-declared names and the directive string appears nowhere in the store or the built
system prompt; a call whose tool schema is absent/unparsable → no example observation
recorded; existing round-trip/inject tests stay green.

**Acceptance:** gates green; diff confined to `internal/library`, `internal/mechanisms`,
the archived-plan amendment + CHANGELOG. Commit:
`fix(library,mechanisms): strip format characters and schema-filter example param names`.

---

## 4. `resolveContextWindow`: no redundant probe on the no-model path

**Finding:** review "Falsified NOTES claim: `resolveContextWindow` also probes on the
no-model path" (Medium — behaviourally benign, but the deviation trail's honesty is the
point). Ground truth: the item-3 NOTES claim
(`docs/plans/archived/phase-4-second-review-fixes-plan.md:176-183` — "fires the single
extra probe only for a pinned model with no key"); the guards at
`cmd/apogee/config.go:571-583`; the call site at `cmd/apogee/root.go:129`; the trigger:
model discovery ran and the server advertised no window (`got.contextWindow == 0`).

**What:** make the claim true. Gate the `resolveContextWindow` call (or the helper itself)
so it never probes when model discovery already ran this startup — e.g. `resolveModel`
reports whether it probed (the discovered-id return is already `""` for a pinned model) and
`root.go` skips the call in that case, regardless of whether the advertised window was 0.
The pinned-model path is unchanged (still probes when no `context-window` key is set;
failure still non-fatal). **Owning doc amendment:** append a dated correction line to the
archived plan's item-3 NOTES ("the no-redundant-probe claim was false for servers
advertising no window until 2026-07-05; fixed by phase-4-third-review-fixes item 4").

**Tests:** `cmd/apogee/config_test.go` — a counting stub discoverer: no-model path where
the probe advertises `contextWindow: 0` → exactly ONE probe for the whole startup sequence
and the loud-zero notice still appears once; pinned-model path unchanged (existing
`TestResolveContextWindow*` tests keep passing).

**Acceptance:** gates green; diff confined to `cmd/apogee`, the archived-plan amendment +
CHANGELOG. Commit:
`fix(config): skip the redundant window probe on the no-model path`.

---

## 5. Test: `context-window` key precedence and `ContextConfig` threading

**Finding:** review "`context-window` key precedence on the no-model path is
mutation-proven unguarded" (Medium, Tests). Ground truth: the item-3 Tests mandate
("`context-window` set → no window probe, value threaded to ContextConfig",
`docs/plans/archived/phase-4-second-review-fixes-plan.md:208-212`); the unguarded branch at
`cmd/apogee/config.go:556-558` (`if opts.contextWindow == 0` in `resolveModel`'s no-model
path); the threading at `cmd/apogee/wire.go:136`
(`MaxContextTokens: opts.contextWindow`).

**What (test-only):** `cmd/apogee/config_test.go`: no-model path with
`contextWindow: 16384` pre-set (the key) and a stub discoverer advertising `131072` →
`resolveModel` succeeds, the discovered model id is kept, and `opts.contextWindow == 16384`
(the key wins over the advertisement). Threading: one assertion that the config constructed
from `opts` carries `MaxContextTokens == opts.contextWindow` (place it where `wire_test.go`
already exercises the opts→config seam). While there, correct the
`TestApplyConfigContextWindow` comment (`config_test.go:236-252`) if it still claims
"end-to-end" coverage it does not provide.

**Acceptance:** gates green; diff confined to `cmd/apogee` test files + CHANGELOG (one
line). Commit:
`test(config): pin context-window precedence over discovery and the ContextConfig threading`.

---

## 6. Test: cached-content schema-gate conservative fallbacks

**Finding:** review "`toolDeclaresMaxLines` conservative fallbacks are unpinned" (Medium,
Tests — mutation-proven silent). Ground truth: the item-6 invariant (never hand a strict
tool an undeclared argument; no mutation ⇒ no fire, R4);
`internal/mechanisms/cachedcontent.go:159-177`; the realistic absent-tool case: toolfilter
narrowing removes the pending tool from `view.Tools()`.

**What (test-only):** extend `internal/mechanisms/cachedcontent_test.go` with a redundant
re-read where the pending tool is (a) absent from the tool menu entirely, (b) present with
`Schema: nil`/empty, and (c) present with malformed schema JSON → in all three: arguments
byte-identical (the existing `string(got.Arguments)` check) and no fire booked.

**Acceptance:** gates green; diff confined to `internal/mechanisms` test files + CHANGELOG
(one line). Commit:
`test(mechanisms): pin the cached-content schema-gate conservative fallbacks`.

---

## 7. Test: edit-tool coverage at the remaining `isFileMutatingTool` sites

**Finding:** review "Four `isFileMutatingTool` call sites switched with no edit-tool test
coverage" (Medium, Tests). Ground truth: S1's semantic (b) ("this call mutated a file");
the sites at `internal/mechanisms/offramps.go:98,149`, `internal/mechanisms/toolloop.go:170`,
`internal/mechanisms/historyhints.go:106`; the existing edit-tool test patterns in
`internal/mechanisms/write_detection_test.go` (which covers read_repeat, cachedcontent,
error_enrichment, read_loop, syntax, autofix — not these four).

**What (test-only):** extend `write_detection_test.go` (or siblings where the harness fits)
so each of the four sites is exercised with `edit_existing_file` (and, where the table is
cheap to widen, a find-and-replace tool — derive canonical names from `internal/tools`
`Name()` methods, per the S1 precedent): the enforcement off-ramps' recent-progress checks
treat a recent edit as progress (off-ramp stays inert — mirror of the existing `write_file`
cases at both `offramps.go` sites); `toolloop.go:170`'s write branch counts an edit tool;
`historyhints.go:106`'s written-path collection excludes an edit-tool-written path from its
suggestion.

**Acceptance:** gates green; diff confined to `internal/mechanisms` test files + CHANGELOG
(one line). Commit:
`test(mechanisms): edit-tool coverage for the off-ramp, tool-loop and hint write sites`.

---

## 8. Docs close-out (owning item for the residue)

**What:** the cross-cutting residue with exactly one owner (items above carry their own
code-adjacent CHANGELOG lines and archived-plan amendments in their commits).
**(a) CHANGELOG:** confirm `git tag -l` still shows no `v1.2.0` (if one appeared mid-run,
STATUS QUESTION — entries then need an Unreleased section); sanity-check every item 1–7
landed its `[1.2.0]` line; add any missing one-liner.
**(b) TODO.md:** add a parked entry for the read-tool-set consolidation the review flagged
(three byte-identical hand-maintained sets: `readToolNames` `offramps.go:47`,
`fileHintReadTools` `filehint.go:46`, `libraryReadTools` `library.go:75`; drift class has
shipped defects in two consecutive review rounds; candidate for
`/improve-codebase-architecture`, deliberately NOT fixed in this plan). Follow TODO.md's
existing entry format (enough design to avoid re-derivation).
**(c) ISSUES.md:** if it tracks any finding this plan fixed, close it; otherwise leave it
untouched.

**Acceptance:** gates green (docs-only otherwise); `git status` clean after commit. Commit:
`docs: close out the phase-4 third-review fixes`.

**Depends on:** items 1–7.

---

## Explicitly NOT in this plan

- The read-tool-set consolidation itself — recorded in TODO.md by item 8; it is structural
  re-shaping for a future `/improve-codebase-architecture` pass, not a fix.
- Relocating the Library observe hook behind the approval gate (S4's accepted stance is
  unchanged; F3/F4 harden the sanitisation defence, they do not move it).
- Any change to what the model *sees* on a retry (F2 deliberately keeps `State()`
  byte-identical); any reopening of the item-10 sim-placement question.
- The bench A/B campaign, default flips, apogee-sim changes, and every deliberate Phase-4
  deferral (R2 retry-ladder refinements etc.) — still bench-pending.
- Re-running the full review — the closeout backstop is `make check` + this plan's
  per-item tests.
