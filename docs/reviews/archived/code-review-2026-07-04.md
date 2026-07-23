# Code Review — Phase 4 implementation vs. plan — 2026-07-04

**Scope:** the full Phase-4 diff (`b4ad0bb..HEAD`, 96 files, ~16.2k insertions) checked against `docs/plans/archived/phase-4-detail-plan.md` (items 1–16, D1–D8), the R1–R5 review-fixes record, the ratified mechanism catalogue, and the pinned apogee-sim source at `d220867` (present on disk and verified at the pin).
**Mission:** port the apogee-sim Mechanisms into the loop as catalogued, config-gated (default-off), self-regulating behaviours, with structural Budget/Compaction, a learning Library, and a provable bench contract — truthfully to the plan and its ratified NOTES deviations.
**Files reviewed:** 96 changed files; `make check` independently re-run — green (gofmt, vet, race tests, ADR-0010 invariant, 6-target cross-build).

## Executive Summary

The implementation follows the plan truthfully to an unusual degree: all 16 items are real in code, the ratified NOTES deviations were verified in both directions (each recorded deviation exists, and each claim the NOTES make holds — with one exception below), the substrate fixes from the earlier wave-1 review are all still in place, tests are mostly *stronger* than the plan mandated, and the acceptance gate passes. Against that baseline, four High-severity issues stand out. The single falsified NOTES claim: the item-11 history-aware family never received the apogee-tool-name mapping that items 10 and 12 recorded as required, so its write detection is blind to apogee's own edit tools and actively misguides the model after an edit. The other three: auto-compaction can fire mid-Exchange (the exact request shape the item-9 NOTES ruled out, plus a stale `exchangeStart` corrupting Esc-abort); a pinned `model:` config silently zeroes the context window, turning the entire default-on Budget/auto-compact machinery inert; and the Library's observe path persists raw model-emitted text that a later session re-injects unsanitised into the system prompt. Everything else is documentation accuracy, deliberate-trail gaps, and edge cases — the kind of list a 16k-line phase should be glad to have.

Note on exposure: most mechanisms ship default-off and bench-gated (D1), so of the High findings only the auto-compact pair (H2, H5) touches users running today's defaults; H1/H3/H4 matter the moment mechanisms are enabled — i.e. exactly when the bench campaign starts. Fix them before benching, or the A/B data measures the bugs.

## Intent & Architecture Findings

### High — Item-11 family's write detection is blind to apogee's own edit tools; falsifies an item-11 NOTES claim `[Plan fidelity]`

- **Where:** `internal/mechanisms/robustness.go:70-78` (shared `writeToolNames`), consumed at `cachedcontent.go:114`, `readrepeat.go:122`, `errorenrich.go:116`, `readloop.go:189`
- **What:** the shared write-tool set carries only the sim's spellings (`write_file`, `writeFile`, `write_to_file`, `create_file`, `edit_file`, `editFile`, `replace_in_file`). Apogee's real menu edits via `edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace` (`internal/tools/file_edit.go:38`, `find_replace.go:53,171`) — none recognised. Items 10 and 12 explicitly recorded in NOTES that mechanism tool-name sets must carry apogee's spellings (and their code does); item 11 got no such bullet and no such mapping. The item-11 NOTES claim that `cached_content_intercept` "was strengthened … with a write-since check" is falsified in effect — the check never sees apogee's primary edit tools.
- **Why it matters:** read `a.go` → `edit_existing_file` → verify-read: `cached_content_intercept` caps the verify-read to `max_lines: 1` (hiding the model's own edit), and `read_repeat` fires a retry telling the model *not* to re-read a file whose in-context copy is now stale. Repeated `edit_existing_file` failures are never enriched by `error_enrichment`, and `read_loop`'s write-decrement misses edits. Two of these actively harm the model — the opposite of the hard constraint's intent — and would poison the bench A/B for the whole wave-3 family.
- **Fix:** point the item-11 family at `wave4WriteTools` (or extend one shared apogee write set in `robustness.go`); decide whether the family's read set (`offramps.go:42`) should also carry `open_file` like the cot/filehint/library sets do; add the NOTES bullet either way.

### High — Auto-compaction fires mid-Exchange, producing the request shape the item-9 NOTES ruled out; also leaves `exchangeStart` stale `[Plan fidelity + Correctness — found independently twice]`

- **Where:** `internal/agent/compact.go:64-88` (`autoCompact`/`shouldAutoCompact` — no `inExchange` guard), `internal/agent/loop.go:179` (called at top of every Turn), `internal/agent/agent.go:166` (`AbortExchange` uses the stale index)
- **What:** the trigger gates only on `CompactionEnabled` + `HistoryExceedsAllocation`. On a tool-continuation Turn (mid-Exchange, no pending input) the fold replaces everything past the protected prefix with a single assistant summary — the very next request ends in an assistant message, the shape the item-9 NOTES ratified the top-of-step placement *specifically to avoid*, and the shape the on-demand `/compact` guard (`compact.go:43`, `ErrInputPending`) exists to prevent. Mid-tool-loop is also the *likeliest* overflow point (one big `read_file` result). Separately, the fold reindexes the conversation but `exchangeStart` is never adjusted, so a later Esc-abort (`AbortExchange` → `DropRange`) either silently no-ops (leaving an aborted tail ending in tool results — the user-after-tool-result shape the code itself documents as template-breaking) or truncates at an arbitrary point. `truncate_history`'s mid-Exchange `DropRange`/`Insert` shifts the same index. All four autocompact tests trigger only at Exchange openings.
- **Why it matters:** auto-compact is **on by default** — this is one of the few Phase-4 behaviours live for every user today. Strict role-alternating chat templates hard-error (Exchange dies); lenient ones continue from a summary that just swallowed the tool results the model asked for.
- **Fix:** add `a.inExchange` to the `shouldAutoCompact` skip (tool_result_cap is the mid-Exchange relief valve, per D6's own division of labour), and clamp/shift `exchangeStart` whenever the conversation is structurally rewritten. Alternatively, ratify mid-Exchange folding in NOTES with explicit continuation-shape handling — but the current state contradicts the recorded rationale.

### Medium — Plan-mandated "Bypass leaves the store byte-for-byte untouched" test was silently dropped `[Plan fidelity + Tests — found independently twice]`

- **Where:** plan item 14 test clause (`phase-4-detail-plan.md:636`) vs `benchreadiness_test.go:463-465` and `internal/mechanisms/library_test.go`
- **What:** coverage substitutes weaker proxies: the Bypass arm's LibraryDir asserted *empty* (proves no file created), plus the generic dispatch-gate matrix. No test seeds a populated `library.json`, drives a Bypass session, and compares bytes. Behaviour is correct today (`Store.Load` never writes — verified), but the wire path *does* `Load()` under Bypass+enabled (`cmd/apogee/wire.go:299-301`), so a future Load-time normalise/evict-and-persist or shutdown flush would violate the stated Bypass invariant with no test failing. Item 14's NOTES record five other deviations but not this substitution.
- **Fix:** add the pre-seeded byte-comparison test (a few lines in `library_test.go` or the bench test), or record the substitution in the item-14 NOTES.

### Medium — CHANGELOG `[1.2.0]` misstates `LibraryDir` as a new Config field `[Plan fidelity]`

- **What:** `CHANGELOG.md:16-17` lists `LibraryDir` among new fields; `git show v1.1.0:internal/domain/config.go` already carries it — Phase 4 only made it consumed. Item 16 explicitly required sanity-checking the claim against the diff since `v1.1.0`. The no-breaking-change claim itself holds (root facade only gains `ErrIncompatibleMechanisms`).
- **Fix:** reword to "the now-consumed `LibraryDir` root" before the owner cuts `v1.2.0`.

### Medium — Catalogue contradicts itself on the pre-request ordering seeds; the sim's order holds only by alphabetical accident `[Plan fidelity]` *(uncertain: which section is the ratified surface)*

- **Where:** `docs/design/mechanism-catalogue.md:211` (§Ordering: "the `cot` nudges and `library` inject before `toolfilter`"; `tool_result_cap` "runs last among pre-request shapers") vs Table A's "none" cells and the code (`library.go:124`, `cot.go:104-176` declare no edges)
- **What:** the implementation follows Table A, and today `"library" < "toolfilter"` alphabetically, so the D4 tiebreak happens to reproduce the sim's order — but nothing declares it, so any future edge or ID rename could silently invert the sim-seeded order with no test failing. (`tool_result_cap` actually sorts *before* `toolfilter` by tiebreak, contradicting §Ordering's "runs last" today.)
- **Fix:** either declare the edges or amend §Ordering with a dated note that the seeds were subsumed by Table A's ratified "none" verdicts — needs owner ratification per D7's amendment rule.

### Medium — Item-10 NOTES claim "plus the sim spellings, for mixed MCP menus" is incomplete `[Plan fidelity]`

- **Where:** `internal/mechanisms/toolfilter.go:46-49` (omits `readFile`), `filehint.go:44` (omits `listFiles`) vs sim `toolfilter.go:59`, `file_hint_detector.go:59`
- **What:** two camelCase sim spellings were dropped from the keep/listing sets. On a mixed MCP menu, analysis-intent filtering can drop a `readFile` tool from the menu, and a `listFiles` call never opens a filehint opportunity.
- **Fix:** add the two spellings, or amend the NOTES to record the narrowing.

## Critical & High Findings

### High — `read_repeat` blocks the verify-read after a same-turn read-then-write `[Correctness — reproduced]`

- **Where:** `internal/mechanisms/readrepeat.go:110-137`
- **What:** within one assistant message, calls are scanned in order and the write-exclusion check runs *before* the write is seen — `[read_file(a.go), write_file(a.go)]` leaves `a.go` in "recent successful reads". (Independent of, and additive to, the tool-name-set issue above: this misfires even for the sim-spelled `write_file`.)
- **Why it matters:** the next verification `read_file(a.go)` triggers an `ActionRetry`: "You already read \"a.go\" … Do not read them again" — steering the model to act on the pre-write, stale in-context copy. Read-then-edit-then-verify is the bread-and-butter agentic shape.
- **Fix:** two-pass per assistant message — collect write paths first, then apply the exclusion when building `reads`.

### High — Library store persists model-controlled text and re-injects it unsanitised into future system prompts `[Security]`

- **Where:** `internal/mechanisms/library.go:283-302` (observe records raw `tc.Arguments`, 200-char cap only), `:425-436` + `:126-155` (`libraryBuildInjectionBlock` → `AppendToSystem`), persisted via `internal/library/store.go:263-283`
- **What:** a hostile repository prompt-injects the local model; the model emits a valid call to any ≥5-param tool; `observeSuccessfulComplexToolCalls` records the raw arguments verbatim as entry `Content`. Two observations later the entry passes the Bayesian gate, and a *future session* concatenates it into the system prompt with no sanitisation or untrusted-data framing. Observe runs at post-response — *before* the approval gate — so the user rejecting the call doesn't stop the recording. Observe is not confidence-gated, so a store poisoned under a label-only setup starts injecting the moment the user switches to a weights-file model id.
- **Why it matters:** hostile-repo → persistent on-disk state → attacker text in the system prompt of unrelated future sessions. Today it requires `library` enabled (default-off) plus a high-confidence fingerprint, which is why this is High rather than Critical — but the longitudinal Library experiment the handoff commissions is exactly the configuration that arms it.
- **Fix:** treat stored `Content` as untrusted on both ends — strip control chars/newlines at `Record`; render the injected block inside an explicitly-untrusted fence ("learned observations, not instructions"); prefer recording a structural fingerprint of the call rather than verbatim arguments.

### High — Pinning `model:` silently disables the entire Budget machinery `[Correctness — independently verified]`

- **Where:** `cmd/apogee/config.go:518-531` (`resolveModel` early-returns when a model is configured; `opts.contextWindow` is set only inside the discovery path), `cmd/apogee/wire.go:123` (`MaxContextTokens: opts.contextWindow` → 0), read-only thereafter (`compact.go:98`, `loop.go:664`; `domain/config.go:99`'s "0 ⇒ discover from the model" comment has no runtime implementation)
- **What:** any user who sets `model:` in config.yaml (the seeded starter config suggests exactly this), `--model`, or `APOGEE_MODEL` gets a zero context window: zero Allocation, `HistoryExceedsAllocation` never trips, `tool_result_cap`'s ceiling is 0, and the default-on automatic Compaction that Phase 4 ships as its structural overflow guard never fires. Long sessions die on context overflow — for the most common configuration.
- **Fix:** run window discovery even when the model is pinned (use the configured id, still probe `/v1/models` for `n_ctx`), and/or add a `context-window` config key; either way make `MaxContextTokens == 0` loud rather than silently inert.

## Medium Findings

### Medium — Retry-in-place superseded messages masquerade as committed history to post-response scanners `[Correctness]` *(uncertain: confirm against the sim whether its detectors saw retry exchanges too)*

- **Where:** `internal/agent/loop.go:287-304` (next attempt's `Response.View()` builds from the appended in-flight request), `internal/mechanisms/historyhints.go:71-77`, `toolloop.go:105-113`
- **What:** the superseded assistant message (R1 appendage, never committed) is indistinguishable from real history in `resp.View()`. Concretely: `read_repeat` counts never-executed reads from the superseded attempt as successful (missing result ⇒ "not an error") and can veto a legitimate first read on the retry; `tool_loop_interceptor` compares the retried response against the superseded attempt instead of the last committed turn, escalating a validate-rejected repeat to "STOP. You are in a loop" instead of the precise correction.
- **Fix:** record the pre-retry conversation length (or mark the appendage) and have post-response views exclude request-scoped retry messages.

### Medium — Auto-compact thrashes when the protected prefix alone exceeds the History allocation `[Correctness]`

- **Where:** `internal/agent/compact.go:83-88`; fold floor at `internal/context/compact.go:35`
- **What:** the trigger never checks that the previous fold got the estimate under budget. A large first user message (pasted log, `@file`; roughly ≥ 0.48 × window × 4 chars) keeps the estimate over budget after every fold, so every step that accumulates ≥2 post-prefix messages pays a full summarisation call and re-collapses just-made tool results — perpetual latency plus per-turn amnesia, while the request still overflows.
- **Fix:** after a fold, suppress the trigger until history has grown past the post-fold estimate; if the prefix alone exceeds the allocation, surface one `ErrorEvent` and stand down.

### Medium — `truncate_history` books a phantom acted-fire when re-run on an ungrown history `[Plan fidelity — R4 violated in effect]`

- **Where:** `internal/mechanisms/truncate_history.go:87-89,116`; booking at `internal/agent/hookrun.go:63-65`
- **What:** after a truncation, a re-run (abandoned or cancelled-then-resumed Turn — no new assistant boundary) drops the gap note and re-inserts an identical one. Content is byte-identical but `Revision` bumps twice, so the R4 bracket books a fire + `MechanismFiredEvent` for a do-nothing pass — feeding strikes-3 judgment and bench attribution with noise. The drop-the-middle content itself is sim-faithful; only the booking is wrong.
- **Fix:** no-op when the drop range is exactly the previously inserted note (`tailStart == prefixEnd+1 && conv.At(prefixEnd).Content == m.gapNote`).

### Medium — Library entries expire on `CreatedAt` even while actively reinforced `[Correctness]` *(uncertain: check whether the sim also expires on creation time)*

- **Where:** `internal/library/store.go:141-146` (re-observation never refreshes `CreatedAt`), `internal/library/entry.go:70-75` (TTL 168h keyed solely on `CreatedAt`)
- **What:** a failure pattern observed daily for a week — by then high-observation, injection-qualified evidence — is silently dropped at day 7 and learning restarts. "The model grew out of it" is supposed to be expressed via `Successes`, not the clock.
- **Fix:** refresh `CreatedAt` (or key expiry on `LastUsed`) when an entry is re-observed — after checking the pinned sim for parity; if the sim does the same, record it as an accepted port quirk instead.

### Medium — `cached_content_intercept`'s `max_lines` cap assumes every read tool tolerates unknown arguments `[Plan fidelity]` *(uncertain: depends on third-party MCP server strictness)*

- **Where:** `internal/mechanisms/cachedcontent.go:128-143`
- **What:** the cap unconditionally adds `"max_lines": 1` to any recognised read call without consulting the tool's schema. Apogee's own `read_file` declares the field, but an MCP `readFile` behind `additionalProperties: false` would reject the mutated arguments — turning a redundant-but-working read into a tool error. The code comment's "benign no-op" claim is unverifiable for third-party tools.
- **Fix:** gate the cap on the pending tool's schema actually declaring `max_lines` (available via `view.Tools()`), which also makes the recorded claim literally true.

## Recommended Action Order

1. **Fix the item-11 tool-name set and the `read_repeat` scan order together** (High ×2, same family, same test files): swap `writeToolNames` for an apogee-complete set, two-pass the same-turn write exclusion. Small diffs, and they must land before any wave-3 bench run — otherwise the A/B measures the bugs, not the mechanisms.
2. **Guard auto-compact with `inExchange` and repair `exchangeStart` on structural rewrites**, and add the post-fold thrash guard while in that function (High + Medium, same file). This is the only High touching users on today's defaults.
3. **Restore window discovery for pinned models** (High): the bench campaign and every documented setup pins a model; without this the structural context machinery Phase 4 ships is inert exactly where it's needed.
4. **Sanitise the Library observe→inject path** (High) before the longitudinal Library experiment the handoff commissions — that experiment is the configuration that arms the ladder.
5. Quick wins: the pre-seeded byte-for-byte Bypass store test, the `truncate_history` phantom-fire no-op, the CHANGELOG `LibraryDir` reword (before the owner tags v1.2.0), the two dropped sim spellings.
6. Owner-ratification items (D7 amendment rule — don't self-amend): the catalogue §Ordering vs Table A contradiction, and the two sim-parity questions (retry-appendage visibility to scanners; TTL-on-CreatedAt).

## What Looked Good

The dispatch substrate is genuinely solid: deterministic topo-sort with tiebreak, duplicate/reserved-ID refusal, the five-point Bypass matrix, R4 acted-fire bracketing via revision counters, and the R3 next-Turn three-way judgment all match their ratified specs exactly, and the earlier review-fixes (R1–R5) are all still in place at dispatch, delivery, and judgment level. The test suite deserves specific praise — loop-level where the plan demanded loop-level, a property-style truncation test against a transliterated sim oracle, a counting-LookPath stub proving construction-only probing, and a bench-readiness contract test whose assertions are all real. The Library store hygiene (no `$HOME` reach, versioned envelope, soft-degrade, bounded eviction, injected-root asserts) and the item-13/14 port fidelity (verified line-level against the pinned sim) are exemplary. The NOTES discipline itself — recording every deviation with rationale — is what made this review's "truthfully follows the plan" question answerable at all; the one place it slipped (item 11's tool-name mapping) is the review's top finding.
