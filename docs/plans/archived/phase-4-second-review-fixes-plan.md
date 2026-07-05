# Plan — Phase-4 second-review fixes (full-phase review findings)

**Date:** 2026-07-04
**Status:** ready to run.
**How to run:** `implement-plan docs/plans/phase-4-second-review-fixes-plan.md with skills: coding-standards`
(the broad plan's standing requirement #1 makes `coding-standards` mandatory for every item).
**Source of direction:** the 2026-07-04 six-lens review of the complete Phase 4
(`docs/code-review-2026-07-04.md`, filed there by item 12) — 5 High / 9 Medium findings,
no Critical; `docs/plans/archived/phase-4-detail-plan.md` (D1–D8) and
`docs/plans/archived/phase-4-review-fixes-plan.md` (R1–R5) still bind;
`docs/design/mechanism-catalogue.md` (composition); the pinned sim @
`d22086701ff9ba8e5565f9587945d6d97434b646` at `~/Repos/Airic/apogee-sim` (behaviour, D7).
**Verify gate (every item):** `make check` (gofmt-clean, vet, build, race tests, ADR-0010
invariant) plus the item's own test commands. "Gates green" means exactly this.

**Precedence (points at ground truth, never at an artifact of this plan):** for *behaviour*,
the pinned sim source wins; for *composition/descriptors*, catalogue Table A wins; the review
report is the findings record — if the code you find contradicts a finding's premise (e.g.
already fixed), re-verify against the code and say so in NOTES rather than "fixing" a
non-defect. Every `file:line` below was verified 2026-07-04 against `bce9c40`; earlier items
of this plan may shift them — re-locate by symbol name before editing.

**Deviation trail:** any authorized deviation from an item's text lands as a dated NOTES
line under that item — no silent divergence (this is what made Phase 4 reviewable; the one
place it slipped is now item 1).

**Before running:** commit this plan file and `code-review-2026-07-04.md` first (suggested:
`docs: add the phase-4 second-review fixes plan and file the review report`) — the
implement-plan preflight stops on a dirty tree.

---

## Design record (from the 2026-07-04 review; running this plan ratifies S1–S5 — flag STATUS QUESTION rather than deviate)

- **S1 — Two write-tool predicates, not one extended set.** `isWriteTool`/`writeToolNames`
  (`internal/mechanisms/robustness.go:70-82`) is consumed by TWO different semantics today:
  (a) wave-1 *content repair* (`syntax.go:70`, `autofix.go:130`) — "this call carries a full
  file payload whose content can be syntax-checked/formatted"; (b) everything else
  (`cachedcontent.go:114`, `readrepeat.go:122`, `readloop.go:103,189`, `errorenrich.go:116,154`,
  `toolloop.go:170`, `historyhints.go:103`, `offramps.go:92,143`) — "this call mutated a file /
  was a write action". Semantic (b) must include apogee's own edit tools
  (`edit_existing_file` and the find-and-replace tools — derive the exact canonical names from
  the `Name()` methods in `internal/tools/`, do not trust any doc's spelling); semantic (a)
  must NOT be extended — edit-tool payloads are fragments (old/new strings), not files, and
  extending (a) would make autofix/syntax act on shapes the sim never checked. So: a new
  `isFileMutatingTool` predicate (superset; reuse or subsume `wave4WriteTools`,
  `decompose.go:110`, which is already the apogee-complete precedent from item-12's NOTES)
  for the (b) call sites; `isWriteTool` stays sim-set and content-repair-only.
- **S2 — Auto-compaction is Exchange-boundary-only; a failed fold saturates.** The automatic
  trigger gains an `inExchange` guard (the item-9 NOTES rationale — "folding … would leave the
  request ending in an assistant message" — applies with full force mid-Exchange;
  `tool_result_cap` is the mid-Exchange relief valve per D6). A fold that does NOT bring the
  history estimate back under the allocation emits exactly one `ErrorEvent` (Source
  "compaction") and stands down until the estimate drops below the allocation again.
  `exchangeStart` is repaired after any mid-Exchange history rewrite (delta-shift, floored
  just past the protected prefix/gap note).
- **S3 — The context window is discovered for pinned models too.** `resolveModel`'s early
  return on a configured model must not skip window discovery (the discovery failure is a
  one-line notice, never fatal — a pinned-model user can start offline today and keeps that).
  A new file-only `context-window` key (tokens, int) overrides discovery. A resolved window
  of 0 with compaction enabled prints one startup notice naming the consequence and the key.
- **S4 — Library store content is untrusted data.** Sanitize (strip control chars, fold
  newlines to spaces, collapse runs) at `Record` time AND at injection-render time (defends
  pre-existing stores); the complex-call "example" observation records the tool name and
  sorted parameter NAMES only — never argument values (kills the hostile-repo →
  store → future-system-prompt payload channel while keeping the shape-teaching value); the
  injected block opens with an explicit data-not-instructions frame. Accepted and recorded:
  observe still runs at post-response, before the approval gate — sanitisation, not
  relocation, is the defence.
  - **Amendment (2026-07-05, phase-4-third-review-fixes item 3, findings F3/F4):** the
    2026-07-04 sanitizer stripped only Cc via `unicode.IsControl`, so Cf/Co/Cs — bidi
    overrides (U+202A–202E, U+2066–2069), zero-width characters (U+200B/C/D), the BOM
    (U+FEFF) and soft hyphen (U+00AD) — survived; `SanitizeContent`'s strip now also covers
    `unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs)` (F3). The "parameter NAMES" mitigation
    recorded the raw keys of the model's arguments object — free-form model-controlled strings
    (`validateArguments` only checks that required params are present, so junk keys ride along
    on a clean call); the recorded names are now the intersection of the argument keys with the
    tool schema's declared `properties`, and a call whose tool schema yields no derivable
    properties records no example at all (F4). The 5+-param complexity gate already reads the
    schema property count (never the argument keys), so junk keys never could promote a simple
    call to "complex" — that half of the mitigation held.
- **S5 — Sim-parity rule for the two `(uncertain)` findings** (items 9 and 10): read the
  pinned sim first; if the sim behaves identically, the finding is a *ported quirk* — record
  it (code comment + dated NOTES here), change no behaviour; if the sim differs, implement
  the item's specified fix. Either way the item produces a commit (code or comment/NOTES).
- **S6 — The catalogue §Ordering contradiction is an owner design call** (item 11) — Table A
  cells are ratified surface (D7's amendment rule), so neither declaring new edges nor
  amending §Ordering happens without the owner choosing.

---

## 1. History-family write detection + `read_repeat` same-turn scan order — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** Implemented S1 by *reusing* `wave4WriteTools` (decompose.go) as the single
superset — `isFileMutatingTool` is `func(name) bool { return wave4WriteTools[name] }`. Beyond the two
named doc comments (robustness.go, historyhints.go) I also added one clarifying sentence to the
`wave4WriteTools` comment noting it now doubles as isFileMutatingTool's source. `open_file` DOES place
file content into the conversation (open_file.go renderOpenFile, read-only), so it was added to
`readToolNames` per the item's "does" branch. No behaviour change to `syntax`/`autofix` (kept on the
sim-only `isWriteTool`).

**Findings:** review "Item-11 family's write detection is blind to apogee's own edit tools"
(High, falsified NOTES claim) + "`read_repeat` blocks the verify-read after a same-turn
read-then-write" (High, reproduced). Ground truth: apogee's real tool names from
`internal/tools/` (`file_edit.go`, `find_replace.go`); sim sets at
`~/Repos/Airic/apogee-sim/internal/toolsets/toolsets.go` @pin.

**What:** implement S1. Add `isFileMutatingTool` (sim write set ∪ apogee's edit tools —
verify names against `internal/tools`; `wave4WriteTools` at `decompose.go:110-116` is the
existing apogee-complete set: reuse it or define the union in one place and point both at
it). Switch every semantic-(b) call site listed in S1 to the new predicate; leave
`syntax.go`/`autofix.go` on `isWriteTool` untouched. Update the shared-set doc comments
(`robustness.go:67-82`, `historyhints.go:23-24`) to name the two semantics.
Read-set alignment: check `internal/tools/open_file.go` — if its result places file content
into the conversation (like `read_file`), add `open_file` to the family's `readToolNames`
(`offramps.go:40-48`), matching the cot/filehint/library precedent (`cot.go:75`,
`filehint.go:45`, `library.go:75`); if it does not, leave the set and say so in NOTES.
`read_repeat` scan order: in `readrepeat.go:110-137`, per assistant message collect write
paths in a FIRST pass over its ToolCalls, then build `reads` excluding written paths — so a
read superseded by a same-turn write never lands in "recent successful reads" (this fixes
the sim-spelling case too, independent of the set swap).

**Tests:** (all loop-level where the existing harness supports it, else table-driven on the
mechanism) read `a.go` → `edit_existing_file(a.go)` → verify-read: `cached_content_intercept`
does not cap, `read_repeat` does not fire; same-turn `[read_file(a.go), write_file(a.go)]`
then verify-read → `read_repeat` does not fire (the reproduced C-02 case, sim spellings);
repeated `edit_existing_file` failures on one path → `error_enrichment` enriches;
`read_loop`'s write-decrement counts an edit tool; `syntax`/`autofix` still ignore
edit-tool calls (regression pin for S1's non-extension).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): apogee-complete write detection for the history family and same-turn read-then-write ordering`.

---

## 2. Auto-compaction: Exchange-boundary guard, `exchangeStart` repair, fold saturation — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** (c) The `exchangeStart` clamp is applied only on a SHRINK (`dropped > 0`),
not unconditionally when `inExchange`. The plan's literal "afterwards, when `a.inExchange`, clamp(…)"
corrupts `exchangeStart` on an Exchange-opening Turn: there the just-appended user message sits at
`PrefixEnd()`, so a zero-drop clamp with floor `PrefixEnd()+1` has lo > hi (`min(max(…))` would then
force `exchangeStart` past the user message). Guarding on a real shrink also avoids mis-shifting a
hypothetical growing rewrite (no registered history-rewriter grows). Behaviour is identical to the
intended repair for the truncate case the item targets. (b) Saturation is tracked as a boolean latch
(`compactSat`) re-checked against the live estimate rather than caching a stale token number — same
semantics, no drift.

**Findings:** review "Auto-compaction fires mid-Exchange … also leaves `exchangeStart`
stale" (High, found independently twice) + "Auto-compact thrashes when the protected prefix
alone exceeds the History allocation" (Medium). Ground truth: the item-9 NOTES' own ratified
rationale (`phase-4-detail-plan.md:411-415`); the `/compact` guard at
`internal/agent/compact.go:43`.

**What:** implement S2 in `internal/agent/compact.go` / `loop.go` / `agent.go`.
**(a) Guard:** `shouldAutoCompact` (`compact.go:83-88`) additionally requires
`!a.inExchange` — a mid-Exchange over-budget turn skips the fold (tool_result_cap is the
relief valve); the fold then fires at the next Exchange opening exactly as the existing
top-of-`step()` placement (`loop.go:179`) already provides.
**(b) Saturation:** track the post-fold history estimate on the agent; when a fold completes
and the estimate still exceeds the History allocation, emit one `ErrorEvent` (Source
"compaction", message naming the oversized protected prefix) and suppress further automatic
folds until the estimate drops below the allocation (growth alone must not re-trigger while
saturated). On-demand `/compact` ignores saturation entirely.
**(c) `exchangeStart` repair:** history-rewrite hooks (`truncate_history`) can shrink the
conversation mid-Exchange. Around the history-rewrite dispatch in `step()` capture
`before := conv.Len()`; afterwards, when `a.inExchange`,
`a.exchangeStart = clamp(a.exchangeStart - (before - conv.Len()), conv.PrefixEnd()+1, conv.Len())`
— the floor lands just past the protected prefix + gap note, which is correct because after
a truncation everything from there to Len is current-Exchange tail (see the review's
analysis); leave a short comment stating that invariant. `AbortExchange`
(`agent.go:166`) itself stays unchanged.

**Tests:** mid-Exchange over-budget tool-continuation turn → no fold, no summarizer call;
same conversation folds at the next Exchange opening; oversized first user message (prefix
alone > allocation) → exactly one fold attempt + one `ErrorEvent`, zero summarizer calls on
subsequent steps, saturation clears once the allocation is no longer exceeded (e.g. after
`/compact` of a grown tail or a larger window); Esc-abort after a mid-Exchange truncation →
conversation ends exactly at the gap note (no orphaned tool results, no over-drop); all four
existing autocompact tests keep passing.

**Acceptance:** gates green; diff confined to `internal/agent` + CHANGELOG. Commit:
`fix(agent): exchange-boundary auto-compaction with exchangeStart repair and fold saturation`.

---

## 3. Context-window discovery for pinned models + `context-window` key — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** (a) "Split window resolution out of `resolveModel`" is done as an ADDITIVE
split, not a full extraction: `resolveModel` still sets the window in its no-model path (that one
probe returns both id and window — extracting it would force a second probe on the zero-config
startup and break the existing `resolveModel` window assertion), now guarded by `if opts.contextWindow
== 0` so a `context-window:` key wins. The new `resolveContextWindow` owns the PINNED path and
self-guards on `opts.contextWindow > 0 || endpoint == ""`, so it fires the single extra probe only
for a pinned model with no key — no redundant probe on the common no-model path. Both the discovery
notice and the loud-zero notice name the `context-window` key.

**Finding:** review "Pinning `model:` silently disables the entire Budget machinery" (High,
independently verified). Ground truth: `cmd/apogee/config.go:518-531` (`resolveModel` early
return), `cmd/apogee/wire.go:123` (`MaxContextTokens: opts.contextWindow`),
`internal/domain/config.go:99` (the "0 ⇒ discover from the model" comment nothing implements).

**What:** implement S3 in `cmd/apogee`.
**(a)** Split window resolution out of `resolveModel`: when a model IS configured, an
endpoint is known, and no `context-window` key is set, still call the discoverer for the
window; keep the pinned model id regardless of what discovery reports; on discovery failure
print a one-line notice and continue with 0 (never fatal — preserves offline startup for
pinned models; the existing no-model path keeps its current fatal semantics).
**(b)** New file-only config key `context-window` (int, tokens; like `auto-compact` — no
flag/env), threaded fileConfig → layer → settings → `opts.contextWindow`; when > 0 it wins
and the window probe is skipped.
**(c)** Loud zero: at wire time, `MaxContextTokens == 0 && CompactionEnabled` → one startup
notice ("context window unknown — automatic compaction and the Budget are inactive; set
`context-window:` in config.yaml or let discovery run"). Fix the stale
`internal/domain/config.go:99` comment to describe reality (0 ⇒ unknown; the CLI discovers
or the key supplies it).

**Docs (same commit):** README Configuration section + `cmd/apogee/defaults/config.yaml`
gain the `context-window` key with a one-line explanation.

**Tests:** pinned model + stub discoverer → `opts.contextWindow` populated and the pinned id
kept; pinned model + failing discoverer → non-fatal, notice text, window 0; `context-window`
set → no window probe, value threaded to `ContextConfig`; config round-trip + file-only
enforcement (no flag/env); zero-window-with-compaction notice emitted, suppressed when the
key is set or discovery succeeds.

**Acceptance:** gates green; diff confined to `cmd/apogee`, `internal/domain` (comment only)
+ README/CHANGELOG. Commit:
`fix(config): discover the context window for pinned models and add the context-window key`.

---

## 4. Library observe/inject content hygiene — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** (a) "keep the existing length cap" — `Store.Record` never had a content-length
cap (the only 200-char cap lived in `observeSuccessfulComplexToolCalls`, which (b) replaces with
parameter names, naturally short), so `SanitizeContent` applies NO length cap: adding an arbitrary one
would both invent a magic number and break `TestLibraryInjectBudgetCap` (its ~700-char note must stay
over the 200-token budget to be dropped). The inject-side token budget remains the size bound, stated
in the helper doc. The helper is EXPORTED (`library.SanitizeContent`) because (c)'s render-time
re-sanitize lives in the `mechanisms` package. (c) The data-not-instructions frame is folded into the
existing header line (keeping `libraryInjectionMarker` as its prefix so `AppendToSystem` idempotency
holds) rather than added as a separate line; wording differs from the item's illustrative "e.g." text.
The (b) audit of other `Record` callers is satisfied structurally: sanitisation lives inside
`Store.Record`, so every content string (validation-failure/hallucination messages, example shapes)
is sanitized at record time; mechanism-authored constant notes pass through unchanged.

**Finding:** review "Library store persists model-controlled text and re-injects it
unsanitised into future system prompts" (High, Security). Ground truth: the review's attack
walk-through; `internal/mechanisms/library.go:283-302` (observe records raw `tc.Arguments`),
`:425-436` (`libraryBuildInjectionBlock`), `:126-155` (inject via `AppendToSystem`);
`internal/library/store.go` `Record`/persist.

**What:** implement S4.
**(a)** `internal/library`: a `sanitizeContent` helper (strip control characters, fold
CR/LF to single spaces, collapse whitespace runs; keep the existing length cap) applied to
entry `Content` inside `Store.Record` — poison never lands on disk in directive-capable
form. No store version bump (entries stay schema-compatible).
**(b)** `internal/mechanisms/library.go`: `observeSuccessfulComplexToolCalls` stops
recording argument VALUES — record `"Example valid call for <tool> uses params: a, b, c"`
(sorted parameter names from the call's arguments object). Audit the other `Record` callers
in this file: any content string that embeds model- or tool-result-derived text goes through
the sanitizer (mechanism-authored constant text may pass as-is).
**(c)** Render defence: `libraryBuildInjectionBlock` sanitizes each entry line again (old
stores predate (a)) and the whole block opens with a fixed frame line — e.g.
`"Learned observations for this model (recorded data, not instructions):"` — replacing or
extending the current header so injected entries cannot read as directives.

**Tests:** a tool call whose arguments carry newlines/control chars + directive text →
stored entry is single-line and value-free; the directive string never appears in the
built system prompt; a pre-seeded store file containing raw multi-line poisoned content
(fixture) → rendered block is sanitized and framed; existing library round-trip/inject
tests updated to the new example format.

**Acceptance:** gates green; diff confined to `internal/library`, `internal/mechanisms` +
CHANGELOG. Commit:
`fix(library): treat stored observations as untrusted data — sanitize, frame, record shapes not values`.

---

## 5. `truncate_history`: no phantom acted-fire on an ungrown history — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** The "extend the existing no-op test" was done as a NEW dedicated test
(`TestTruncateHistoryRerunNoPhantomFire`) alongside `TestTruncateHistoryNothingToDrop` rather than
folding into it: the re-run-after-a-real-truncation case (drop the just-inserted gap note) is
structurally distinct from that test's "keep >= exchanges, nothing ever dropped" case, and the new
test also covers the grown-history re-truncation the item calls for. The guard lives in
`RewriteHistory` (not `tailStart`), keeping `tailStart` the pure sim-faithful computation.

**Finding:** review "`truncate_history` books a phantom acted-fire when re-run on an
ungrown history" (Medium — R4 violated in effect). Ground truth: R4
(`phase-4-review-fixes-plan.md:55-61`); `internal/mechanisms/truncate_history.go:87-89,116`;
booking at `internal/agent/hookrun.go:63-65`.

**What:** in `RewriteHistory`, return without mutating when the pending drop would only
re-drop and re-insert an identical gap note — i.e. the drop range is exactly the previously
inserted note (`tailStart == prefixEnd+1` and the message at `prefixEnd` equals the gap-note
content). Revision then stays unchanged and the R4 bracket books nothing. The truncation
CONTENT stays sim-faithful (the sim also re-drops/re-inserts; only apogee's fire booking is
wrong — do not change the grown-history path).

**Tests:** truncate once, re-run the rewrite with no new assistant boundary → Revision
unchanged, no `MechanismFiredEvent`, `Fired` count unchanged (extend the existing no-op test
at `truncate_history_test.go`); re-run after real growth → truncates and books normally.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): truncate_history no-ops on an already-truncated ungrown history`.

---

## 6. `cached_content_intercept`: gate the read cap on the tool's schema — ✅ DONE (2026-07-04)

**Finding:** review "`max_lines` cap assumes every read tool tolerates unknown arguments"
(Medium, uncertain-for-third-party-tools). Ground truth:
`internal/mechanisms/cachedcontent.go:128-143`; the tool schemas reachable via the hook's
view (`view.Tools()`).

**What:** before mutating the pending call's arguments, look up the pending tool in
`view.Tools()` and apply the `max_lines` cap ONLY when the tool's parameter schema declares
a `max_lines` property; otherwise the mechanism inspects but does not act (no mutation ⇒ no
fire, R4). This makes the code comment's "benign no-op" claim literally true — a strict MCP
server (`additionalProperties: false`) never receives an argument it rejects.

**Tests:** apogee's `read_file` (declares `max_lines`) → capped as today; an MCP-style read
tool named `readFile` whose schema lacks `max_lines` → arguments untouched, no fire booked;
existing cachedcontent tests keep passing.

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): gate the cached-content read cap on the tool schema declaring max_lines`.

---

## 7. Item-10 sets: carry the dropped camelCase sim spellings — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** Added the two named spellings — `readFile` to `toolFilterAnalysisKeep` and
`listFiles` to `fileHintListTools` — and reworded both sets' doc comments (the toolfilter one had
falsely claimed the sim spellings were mapped away). Scan result for the "add or NOTE" clause: the
remaining sim-spelling sets are already complete against the pin — `fileHintReadTools` carries both
`read_file`/`readFile` (sim `file_hint_detector.go:63`), and `fileHintWriteTools` carries all seven
`toolsets.WriteTools` entries (sim `toolsets.go:9-16`). No other spelling was dropped, so nothing
else to add.

**Finding:** review "Item-10 NOTES claim 'plus the sim spellings' is incomplete" (Medium).
Ground truth: sim `internal/toolfilter/toolfilter.go:59` (keeps `readFile` on analysis
intent) and `internal/proxy/file_hint_detector.go:59` (`listFiles` is a listing) @pin.

**What:** add `readFile` to the toolfilter analysis-keep set
(`internal/mechanisms/toolfilter.go:46-49`) and `listFiles` to filehint's listing set
(`internal/mechanisms/filehint.go:44`), completing the item-10 NOTES claim. Scan both files
for any other sim spelling the NOTES promised and a set dropped; add or NOTE.

**Tests:** analysis-intent narrowing keeps a `readFile` tool on the menu; a `listFiles` call
opens a filehint opportunity (mirrors the existing `list_dir` case).

**Acceptance:** gates green; diff confined to `internal/mechanisms` + CHANGELOG. Commit:
`fix(mechanisms): carry the camelCase sim spellings in the toolfilter/filehint sets`.

---

## 8. Test: Bypass leaves a pre-seeded Library store byte-for-byte untouched — ✅ DONE (2026-07-04)

**Finding:** review "Plan-mandated 'Bypass … byte-for-byte' test was silently dropped"
(Medium, found independently twice). Ground truth: the item-14 mandate
(`phase-4-detail-plan.md:636`); the wire path DOES `Load()` under Bypass+enabled
(`cmd/apogee/wire.go:299-301`), so the invariant deserves its literal test.

**What (test-only):** seed a temp `LibraryDir` with a populated, valid `library.json`
(write it through a `library.Store` in setup so the format stays canonical); build a
registry-backed agent with `library` enabled AND `Bypass: true`; drive a full scripted
Exchange (the `internal/mechanisms/library_test.go` or `wave1delivery_test.go` harness
patterns); assert the store file's bytes are identical before/after (and its mtime-agnostic
content, i.e. compare `os.ReadFile` output). Place it wherever the existing library
loop-level tests live.

**Acceptance:** gates green; diff confined to test files + CHANGELOG (one line). Commit:
`test(mechanisms): bypass leaves a pre-seeded library store byte-for-byte untouched`.

---

## 9. Sim-parity check: Library entry expiry vs reinforcement — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** Sim-parity outcome = *ported quirk, no behaviour change*. The pinned sim keys
expiry on `CreatedAt` (`entry.go:48` `time.Since(e.CreatedAt) > TTL`) and its `Store.Record` match path
(`store.go:161-166`) bumps `Observations`/`Confidence`/`LastUsed`/`Content` but never touches
`CreatedAt`; a full grep of the sim's `internal/library` finds `CreatedAt` written only once, at entry
creation. apogee already behaves identically (`Record` match path bumps `Observations`/`LastUsed`/
`Content`, `Expired` keys on `CreatedAt`), so a re-observed entry expires at `CreatedAt + TTL`
regardless of reinforcement — this is sim-faithful, not a defect. Per S5: no code behaviour change;
added a sim-faithful-port comment on `Entry.Expired` (`internal/library/entry.go`). No CHANGELOG entry
(Acceptance scopes CHANGELOG to the fix case only). Commit: the `docs(library):` option.

**Finding:** review "Library entries expire on `CreatedAt` even while actively reinforced"
(Medium, *(uncertain — check sim parity)*). Ground truth: the pinned sim's store
(`~/Repos/Airic/apogee-sim/internal/library/store.go` and its entry TTL handling @pin);
apogee `internal/library/store.go:141-146`, `entry.go:70-75`.

**What:** apply S5. Read the sim's expiry/re-observation semantics at the pin. If the sim
also keys expiry on creation time without refresh → no behaviour change: add a short code
comment on `Expired` naming it a sim-faithful port choice and a dated NOTES line here.
If the sim refreshes lifetime on re-observation (or keys on last-use) → port that exactly:
refresh the relevant timestamp in `Record`'s match path so an entry observed within its TTL
window does not expire while being reinforced; keep eviction semantics otherwise identical.

**Tests (only in the fix case):** an entry re-observed inside the TTL window survives past
the original `CreatedAt + TTL`; an entry NOT re-observed still expires.

**Acceptance:** gates green; diff confined to `internal/library` (+ CHANGELOG in the fix
case). Commit (pick by outcome):
`fix(library): refresh entry lifetime on re-observation (sim parity)` /
`docs(library): record creation-time expiry as a sim-faithful port choice`.

---

## 10. Sim-parity check: retry-appendage visibility to post-response scanners — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** Sim-parity outcome = *FIX (the sim differs)*. The sim's retry builders
(`retryWithReadRepeatHint`/`retryWithToolLoopDirective`/`retryWithCorrection`, `read_repeat_interceptor.go`/
`tool_loop_interceptor.go`/`response_validator.go` @pin) COPY the request and append the superseded
attempt + hint only to the throwaway copy marshalled upstream; `forwardWithValidation`'s retry loop
re-runs `analyzeToolCalls(req, …)` with the ORIGINAL unmutated `req.Messages` on every iteration, so
`detectRepeatReads`/`detectToolCallLoop` never see the appendage. apogee mutated `req` in place, so
`req.View()` leaked the superseded read/call to `read_repeat`/`tool_loop_interceptor`. Fix: internal
`Request.committedLen` (unexported, no public surface), frozen at the FIRST `AppendSupersededAssistant`
and NOT advanced per retry (the sim uses the original committed request for ALL retry iterations, so
the scanner view stays pinned across accumulated corrections — a clarification of "record the pre-append
length" for the multi-retry case); `View()` bounds the scanner history to it while `State()` still sends
the full appendage to the model. Tests live in `internal/agent` (the loop-level scripted harness, within
the item's `internal/agent` scope) rather than `internal/mechanisms`.

**Finding:** review "Retry-in-place superseded messages masquerade as committed history to
post-response scanners" (Medium, *(uncertain — confirm against the sim)*). Ground truth: the
sim's retry builders and detector inputs @pin
(`~/Repos/Airic/apogee-sim/internal/proxy/response_validator.go:366-391`,
`response_analysis.go` — what request/history the detectors saw during a retry cycle);
apogee `internal/agent/loop.go:287-304` (retry views build from the appended in-flight
request), `internal/mechanisms/historyhints.go:71-77`, `toolloop.go:105-113`.

**What:** apply S5. Determine whether the sim's detectors, on a retry cycle, also saw the
augmented retry request (superseded assistant + correction) as history. If yes → ported
quirk: record it (comment at the loop's retry-append seam + dated NOTES here), no code
change. If no → fix: when `respondAndReview` appends the superseded exchange, record the
pre-append request length (an internal field on `Request` set via the existing
retry-exchange seam — NOT a new hook-mutation primitive and NOT a public surface; if a
public surface turns out to be required, STATUS QUESTION instead of shipping one); the
post-response view construction excludes the request-scoped appendage so scanners see only
committed history plus the response under review. Concrete misfires to kill (they become
the tests): `read_repeat` counting never-executed superseded reads as successful;
`tool_loop_interceptor` comparing the retried response against the superseded attempt
instead of the last committed turn.

**Tests (only in the fix case, loop-level through the scripted harness):** a validate-retry
cycle where the superseded attempt contained a read call → `read_repeat` does not treat that
path as already-read on the retry; a model repeating its validate-rejected calls → gets the
validate correction, not the tool-loop "STOP" escalation; the R1 retry-exchange tests
(`retryexchange_test.go`) stay green — the appendage still reaches the MODEL, only the
mechanism views change.

**Acceptance:** gates green; diff confined to `internal/agent`, `internal/domain`,
`internal/mechanisms` (tests) + CHANGELOG (fix case). Commit (pick by outcome):
`fix(agent): exclude the retry-in-place appendage from post-response history views` /
`docs(agent): record retry-appendage visibility as a sim-faithful port choice`.

---

## 11. Catalogue ordering seeds vs Table A — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** OPTION A implemented per the owner's design call. Edges added: `Before toolfilter`
on `stall_nudge`/`list_nudge`/`tool_use_directive` (cot.go) and `library` (library.go); `After decompose`
on `tool_result_cap` (tool_result_cap.go). Table A cells amended with dated D7 ratification notes, plus a
dated reconciliation note in §Ordering. The resulting-order pinning test lives in
`internal/mechanisms/catalogue_test.go` (`TestPreRequestOrderingSeeds`) rather than the item's named
`registry_ordered_test.go`/`mechanism_dispatch_test.go`: both of those use STUB mechanisms (domain
white-box topo-sort / agent synthetic dispatch) and cannot pin the REAL catalogue's edges, which is the
whole point of option A (rename-proof); catalogue_test.go already builds the real Mechanisms. Two existing
descriptor tests (tool_result_cap_test.go, library_test.go) that asserted the old "none" ordering were
updated to the new edges. Bench-readiness `[toolfilter decompose experimental]` expectation re-verified
unchanged (library is enabled there but inject is confidence-gated, so it does not fire; toolfilter still
sorts strictly before decompose).

**Finding:** review "Catalogue contradicts itself on the pre-request ordering seeds; the
sim's order holds only by alphabetical accident" (Medium, S6). Ground truth: catalogue
§"Ordering carried from the sim" (`docs/design/mechanism-catalogue.md:209-214`) vs Table A's
ratified "none" cells (`:122-123,131-134`) and the code (no declared edges on `library`, the
cot nudges, or `tool_result_cap`).

**Design call — Q for the owner:** §Ordering says the cot nudges and `library` inject before
`toolfilter`, and `tool_result_cap` "runs last among pre-request shapers"; Table A and the
code declare none of these edges, so today's order is the D4 ID tiebreak (which happens to
match the sim for library/cot — and does NOT for `tool_result_cap`, which sorts before
`toolfilter`). Two resolutions:
**(A) Declare the edges** — `stall_nudge`/`list_nudge`/`tool_use_directive` and `library`
gain `Before toolfilter`; `tool_result_cap` gains `After decompose` (pushing it last among
current shapers); amend the Table A cells with a dated ratification note (D7 amendment
rule). Rename-proof and sim-faithful; recommended.
**(B) Amend §Ordering** — record with a dated note that the seeds were subsumed by Table A's
ratified "none" verdicts and the order deliberately rests on the tiebreak; add a regression
test pinning the current full pre-request `Ordered()` sequence so a future rename/edge
change fails loudly.

**What:** implement exactly the chosen option (code edges + catalogue amendment for A;
catalogue amendment + pinning test for B). Either way `registry_ordered_test.go` /
`mechanism_dispatch_test.go` coverage is extended to the resulting order, and the
bench-readiness test's `[toolfilter decompose experimental]` expectation is re-verified
(option A must not change it — the new edges only ORDER existing fires, `tool_result_cap`
and the nudges are not armed in that test).

**Acceptance:** gates green; diff confined to `internal/mechanisms`, `internal/domain`
(tests), `docs/design/mechanism-catalogue.md` + CHANGELOG. Commit (by option):
`fix(mechanisms): declare the sim-seeded pre-request ordering edges` /
`docs(design): ratify tiebreak ordering — §Ordering seeds subsumed by Table A`.

---

## 12. Docs close-out (owning item for every cross-cutting doc amendment) — ✅ DONE (2026-07-04)

**NOTES (2026-07-04):** (b) The preflight filed the review as a handoff at
`docs/handoffs/2026-07-04 - 01 - code-review.md` (committed `58887a7`), not at repo root as
`code-review-2026-07-04.md`. So the literal `git mv code-review-2026-07-04.md docs/` is moot; item 12
instead `git mv`s that handoff to `docs/code-review-2026-07-04.md` — the docs/-top-level architecture-
review precedent (`docs/architecture-review-20260629-110828.html`) the item cites, a stable home the
archive-handoffs skill will not relocate — and repoints the plan's header pointer there. (a) All items
1–11 that warranted a CHANGELOG line have one under `[1.2.0]`; item 9 shipped as `docs(library):` with
no CHANGELOG entry per its own NOTES, so none was added. `git tag -l` shows only v1.0.0/v1.1.0 — no
`[Unreleased]` split needed. (c) ISSUES.md tracks none of this plan's findings — left untouched.

**What:** the residue with exactly one owner (items above carry their own code-adjacent
CHANGELOG lines in their commits).
**(a) CHANGELOG:** confirm `git tag -l` still shows no `v1.2.0` (if one appeared mid-run,
STATUS QUESTION — entries then need an Unreleased section instead). Fix the `[1.2.0]`
misstatement at `CHANGELOG.md:16-17`: `LibraryDir` is not a new Config field (it predates
`v1.1.0`) — reword to the "now-consumed `LibraryDir` root". Sanity-check every item 1–11
landed its CHANGELOG line under `[1.2.0]`; add any missing one-liner.
**(b) File the review:** `git mv code-review-2026-07-04.md docs/` (the
architecture-review precedent) and make sure this plan's header pointer still resolves.
**(c) Consistency:** the item-1 fix falsifiable-claim trail — append a dated correction
line to the archived `docs/plans/archived/phase-4-detail-plan.md` item-11 NOTES ("write-since
check was inert for apogee's edit tools until 2026-07-04, fixed by
phase-4-second-review-fixes item 1") so the historical record stays honest; ISSUES.md — if it
tracks any finding this plan fixed, close it, otherwise leave it untouched.

**Acceptance:** gates green (docs-only otherwise); `git status` clean after commit. Commit:
`docs: close out the phase-4 second-review fixes`.

**Depends on:** items 1–11.

---

## Explicitly NOT in this plan

- The bench A/B campaign, any default flip, any apogee-sim change (D1/D8 unchanged).
- Relocating the Library observe hook behind the approval gate (S4 accepts sanitisation as
  the defence; a relocation is a design change for a future plan if the bench motivates it).
- The R2 retry-ladder refinements and every other deliberate Phase-4 deferral — still
  bench-pending, still recorded in the catalogue.
- Re-running the full six-lens review — the closeout backstop is `make check` + this plan's
  per-item tests.
