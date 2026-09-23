# Input box and pane performance plan

**Goal:** Typing and pasting into the prompt box stay responsive on long drafts, and the `/thinking` pane and the sub-agent run view stay responsive with large content. Every per-keystroke and per-frame cost that scales with content size is cut to linear-or-cached.
**Date:** 2026-09-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 69d9ae67
**Closes:** IDEAS.md: skill recommendations needs to run less often; IDEAS.md: Sub-Agent / Thinking panels are partly extremely slow

**Sources:**
- `docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md` (band, spend-at-send)
- `docs/adr/` 0011 (value-copied Model, pointer-held caches), 0030 (one width authority), 0039, 0053, 0063 (run view)
- `layout.md` — "The /thinking popup"; `internal/tui/doc.go` (tick-chain invariant); `internal/tui/paintcache.go`
- Measurements (2026-09-23, Pi arm64): 10k-char draft ≈ 99 ms/key (textarea wrap ≈ 41 %, skill band ≈ 21 %, apogee row counting ≈ 17 %); 40k bracketed paste ≈ 6.7 s (`seatCaret` ≈ 84 %); `/thinking` pane at the 64×64 KB cap ≈ 1.6 s per reasoning chunk; one 64 KB single-line record wraps in 357 ms / 128 MB; run view with 800 blocks ≈ 43 ms per child token, 28 MB heap on all-cache-hit.

**Ratified design calls:**
- **Skill band refresh:** debounced — re-ranked once ~150 ms after edits stop; Enter spends the hints on screen (ADR 0061 §3 as written); ADR 0061's per-keystroke consequence is amended (owner, 2026-09-23).
- **Scope:** transcript-wide repaint fixes (theme heap escape, per-block widget measure) are in scope (owner, 2026-09-23).
- **99-line newline cap:** lifted — typed newlines work up to the textarea's 10000-line limit, as a paste already does (owner, 2026-09-23).

**Regression check (2026-09-23, 69d9ae67):**
- 1: recast — memo capacity sized to the draft, 99-line newline cap lifted, Goal restated.
- 2: guard folded — working seat route named, Acceptance widened; depends on item 1.
- 3: recast — yields to ADR 0030 rule 6 (uniseg ruler kept, incremental per grapheme cluster); Acceptance widened.
- 4: guard folded — tick armed only when the band would rank, invoked skills filtered at once (yields to ADR 0061 §3), Acceptance widened.
- 5: guard folded — numeric allocation bound, non-UTF-8 corpus line.
- 6: guard folded — cache validated on text, not length; heading composed fresh.
- 7: guard folded — supersedes the `reportFullWindow` doc comment; depends on item 6.
- 8: recast — frameKey memo dropped (yields to `frameOverlays`/`withFrameSpans` per-value guarantees), Goal scoped; depends on items 6 and 7.
- 9: guard folded — buffer keyed on event kind too; split escapes stripped whole; extends ADR 0011's token coalescing.
- 10: guard folded — `in` and the per-block `ins` kept off the hit path; both `resolveBlock` and `resolveGroup`.
- 11: guard folded — yields to ADR 0030 §6 amendment (widget measure stored per line); survives `railed`/`retargeted`.
- 1 (re-check): guard folded — Files widened to `prompteditor.go` and `model.go`, where the prompt's edits reach the widget (`handleKey`, `foldPaste`, `foldWidgetMsg`).
- 3 (re-check): guard folded — Files widened to `prompteditor.go`, where the row count reaches `Model.inputRows` (`promptEditor.rows`).
- 8 (re-check): guard folded — clamp correctness rests on the height query agreeing with render, pinned per `paneSpecs` row; supersedes the `Model.View` "same rows by construction" comment.
- 1 (re-check, completion): guard folded — one `MaxHeight`-sizing helper runs before every textarea `Update`, capacity capped at `min(10000, …)`, non-parallel `HeapInuse` test, newline test driven through `Model.Update` on a `SetValue` draft.
- 3 (re-check, completion): guard folded — width-factor bite check replaces the length ratio (Goal: O(runes), independent of width); memo pointer-held on `promptEditor`.
- 8 (re-check, completion): guard folded — per-`paneSpec` render counter, `panes_test.go` nil check for the height field, comment-rewrite sweep in `model.go`.

**Standing requirements:**
- skills: coding-standards
- Performance tests are deterministic: call counts, allocation counts/bytes, or output equality — never wall-clock thresholds. Each item also adds or extends a `Benchmark…` for its path.
- Rendered output is byte-identical before and after every item unless the item says otherwise.
- Caches live behind pointers (ADR 0011); a returned slice is fresh or documented read-only.

**Out of scope:**
- bubbles `viewport.SetContentLines`' own `maxLineWidth` pass (upstream-owned).
- Pastes delivered as per-rune keystrokes (no bracketed paste): per-key costs shrink here, the O(n) key count stays.
- Other IDEAS.md entries (sidepanel, copy regression, sub-agent gauge, sub-agent control).

## 1. Prompt textarea keeps its wrap cache on long drafts — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the retained-heap test edits one 1500-rune line with 3000 keys alternating a typed rune at the line's start and a forward delete, not 3000 appends to a 10k-char line — keeps every key a new same-length version so the heap measures memo capacity alone and the test runs in ~9 s on the Pi instead of minutes; measured ~1 MB fitted vs ~25 MB at MaxHeight 0, bound 2400 KB.
NOTES (2026-09-23): longDraft numbers each line — the bubbles memo is keyed on line content, so identical lines share one entry and never thrash; measured allocs 400-vs-40 lines ≈ 9.5× fitted vs ≈ 32× at MaxHeight 99.
NOTES (2026-09-23): Acceptance run without -race: ThreadSanitizer is unsupported on this Pi kernel (47-bit VMA); `go test -count=1 -run 'LineEditor|Prompt' ./internal/tui/` passes.
NOTES (2026-09-23): MaxHeight is also the widget's visible-height clamp; it is never below the old 99, so on terminals over ~100 rows a draft of more than 49 lines may now grow the box past 99 rows, still bounded by Model.draftRowsCeiling (the Goal's "no new cap on visible height").

**What:**
Recast at the regression check (2026-09-23).
**Goal:** the wrap memo is not evicted within one keypress's pass for drafts up to `maxLines`, and its retained heap stays bounded; no new cap on draft lines or visible height is introduced.
**Approach (assumed at the header base):** bubbles `textarea.Model.Update` recreates its memo cache at capacity `MaxHeight` whenever the two differ; apogee leaves `MaxHeight` at the default 99, so any draft over 99 lines thrashes and every `cursorLineNumber` rewraps all lines above the caret. Set the textarea's `MaxHeight` in `newLineEditor` (`internal/tui/lineeditor.go`) so the cache keeps a capacity covering `maxLines` — `MaxHeight = 0` (no clamp; `atContentLimit` then never blocks) or an explicit large value — and confirm apogee's own box-height sizing (`SetHeight` callers) still bounds the visible rows.
**Regression guard.** Size the memo capacity to the draft, not to `maxLines`: on growth set `MaxHeight = max(99, next power of two ≥ 2×LineCount())`, rebuilding the memo only when a doubling is crossed (this replaces the Approach's `MaxHeight = 0` / large-value option, which retains every stale line version — ~242 MB after 3000 keys on one 10k-char line). The 99-logical-line refusal of typed newlines (alt+enter / ctrl+j, via textarea `atContentLimit`) is LIFTED in both the prompt box and the `/settings` text editor (same `newLineEditor`), matching what a paste already allows. One `lineEditor` helper sets `MaxHeight` from `LineCount()` and runs right before EVERY textarea `Update` — `lineEditor.editKey`/`editMsg`, `Model.handleKey`'s fall-through, `Model.foldPaste`, `Model.foldWidgetMsg` — since the prompt's edits bypass `editKey`. The growth formula is capped: `MaxHeight = min(10000, max(99, next power of two ≥ 2×LineCount()))`, so `atContentLimit` refuses the 10001st typed line (textarea `maxLines` is 10000, `splitLine` has no limit, and a later `SetValue` silently truncates past it). The retained-heap test runs without `t.Parallel`, reads `HeapInuse` after `runtime.GC()` before and after, and asserts a bound with ≥ 10× margin (~4 MB at capacity 99 vs ~240 MB at `MaxHeight` 0).
**Files:** internal/tui/lineeditor.go, internal/tui/lineeditor_test.go, internal/tui/prompteditor.go, internal/tui/model.go
**Read first:** internal/tui/lineeditor.go — newLineEditor, lineEditor.editKey; internal/tui/model.go — Model.handleKey fall-through (m.input.Update); internal/tui/prompteditor.go — Model.foldPaste, Model.foldWidgetMsg; internal/tui/recall.go — recall SetValue; bubbles/v2@v2.1.0/textarea/textarea.go — Model.Update (cache recreate), atContentLimit
**Tests:** an allocation-ratio test: one keypress at the end of a 400-logical-line draft allocates ≤ 15× the same keypress on a 40-line draft (10× line ratio; ~31× at base), or ≥ 3× fewer allocs than the same 400-line keypress on a textarea left at `MaxHeight` 99; a retained-heap bound test, not parallel: N keys typed into one long line keep `HeapInuse` (after `runtime.GC()`) within a ≥ 10×-margin bound (fails with `MaxHeight` 0); a test that sends alt+enter / ctrl+j (never plain enter, which submits) through `Model.Update` on a 150-line draft installed by `SetValue` (the recall route) and asserts a new line appears, plus typed runes land; a test that types newlines past 10000 lines and asserts the draft stops at 10000; `BenchmarkPromptKeyLongDraft`.
**Acceptance:** `go build ./... && go test -race -count=1 -run 'LineEditor|Prompt' ./internal/tui/`
**Commit:** `perf(tui): the prompt textarea keeps its wrap cache on drafts over 99 lines`

## 2. Caret seating is linear in the draft — ✅ DONE (2026-09-23)

NOTES (2026-09-23): consequential edit — docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md: made necessary by seatCaret no longer walking (the ADR said `seatCaret` "is now the one walk"); a dated parenthetical records the change, the decision text stays.
NOTES (2026-09-23): the walk-equivalence table covers every row the value has (and every column plus one either side); for a row OUTSIDE the value the old walk ran to the last line's end before the column landed, leaving a larger scroll than needed, while the direct seat clamps the row and scrolls the least. No caller names such a row (stepLine and offsetToLineCol clamp, reseatInput re-seats in place), so no observed behaviour changes.
NOTES (2026-09-23): the paste allocation ratio (40k vs 4k) is ~19× after the change against ~422× before; the test allows 40×. The part above linear is the widget's 99-slot wrap memo: foldPaste sizes it to the draft BEFORE the paste, so a 900-line paste misses the memo on each full pass while a 90-line one fits. Not caused by this item.
NOTES (2026-09-23): `-race` cannot run on this host (ThreadSanitizer: unsupported VMA range, 47-bit VMA); the Acceptance was run without `-race`, and `make test` with APOGEE_TEST_RACE=0.

**What:**
Depends on item 1.
**Goal:** seating the caret after a layout change (`reseatInput` → `lineEditor.seatCaret`) costs O(draft length), not O(lines²); the caret lands on the same (row, col) as before.
**Approach (assumed at the header base):** `seatCaret` walks from line 0 with `CursorEnd` + `CursorDown` per logical line, each `CursorDown` rewrapping everything above. Seat the caret directly (the textarea's `SetCursor`/row-setting API, or a single `MoveToEnd`-style jump followed by column set) without per-line cursor moves.
**Regression guard.** bubbles v2.1.0 textarea has no `SetCursor` or row setter (`row` is unexported; only `SetCursorColumn` is public), and `MoveToEnd`+`CursorUp` walks are as quadratic as today's. The seat route is: split the value at the target offset, then `SetValue(tail); MoveToBegin(); InsertString(head); SetHeight(Height())`. Its seat stays cheap only once item 1's memo is sized to the draft — hence the dependency.
**Files:** internal/tui/lineeditor.go, internal/tui/lineeditor_test.go
**Read first:** internal/tui/lineeditor.go — seatCaret, reseatInput, caretToOffset; internal/tui/prompteditor.go — Model.foldPaste; internal/tui/prompteditor_test.go — TestPromptEditorCaretToOffsetCrossesWrappedRows, TestPromptEditorReseatCaretReachesEveryVisualRow; internal/tui/mouse_test.go — TestClickBelowPhantomWrappedLineSeatsCaret; internal/tui/skill_test.go — TestAcceptSkillRowSeatsTheCaretOnAWrappedDraft
**Tests:** table test pinning the caret position after `seatCaret` for start/middle/end targets on multi-line and soft-wrapped drafts (same results as base) — identical Line/Column/ScrollYOffset/View to base's walk across every (row, col) target, the phantom-trailing-sub-line draft included; allocation-ratio test for a 40k-char paste folded via `foldPaste` vs a 4k paste (fails at base); `BenchmarkPromptPaste`.
**Acceptance:** `go test -race -count=1 -run 'LineEditor|Paste|Seat|CaretToOffset|ClickPositions|LineBreaks' ./internal/tui/`
**Commit:** `perf(tui): seat the prompt caret directly instead of walking every line`

## 3. Prompt row counting is linear and computed once per frame — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the Acceptance command's `-race` cannot run on this box (ThreadSanitizer aborts with "FATAL: Found 47 - Supported 48", the Pi's 47-bit VMA); the same selection passes without `-race`, as does the whole `./internal/tui/` package.

**What:**
Recast at the regression check (2026-09-23).
**Goal:** `inputContentRows`/`wrapRowStarts` cost O(draft runes) per call, independent of width, and one Update+View measures the draft's rows at most once per (value, width).
**Approach (assumed at the header base):** `wrapRowStarts` (`internal/tui/inputaccent.go`) calls `runesWidth` → `uniseg.StringWidth(string(row))` on the growing row for every rune; accumulate the width per rune instead (via `th.measure`, ADR 0030). `Model.inputRows` (via `promptEditor.rows` in `layout`), `settle`→`freshenTranscriptClamp` and `Model.hiddenDraftRows` in `View` each recount; memoise the result on (value, width) in a pointer-held cache.
**Regression guard.** Keep `runesWidth`'s ruler (`uniseg.StringWidth`) — ADR 0030 rule 6: the widget is the oracle, never `th.measure` — replacing the Approach's per-rune `th.measure` accumulation (the item yields to ADR 0030 rule 6, `docs/adr/0030-…` lines 103-106). Make the measure incremental per grapheme cluster: cache the row's width between group placements and re-measure only the word's trailing cluster as a rune joins, so a VS16 emoji still counts 2 cells. The bite check pins the width factor, not the length ratio (base is already ≈ 10× on a 10× length at a terminal width): on one 64 KB line, `TotalAlloc` at width 400 must be ≤ ~2× the width-40 figure (base ≈ 10×). The (value, width) memo is held by pointer on `promptEditor`, set in `newPromptEditor` beside `files: &fileCache{}` — never package state, which parallel tests would race on — nil-tolerant for literal Models, and `Model.hiddenDraftRows` routes through it.
**Files:** internal/tui/inputaccent.go, internal/tui/inputaccent_test.go, internal/tui/model.go, internal/tui/prompteditor.go
**Read first:** internal/tui/inputaccent.go — wrapRowStarts, inputContentRows, runesWidth; internal/tui/prompteditor.go — promptEditor.rows, newPromptEditor; internal/tui/model.go — Model.inputRows, Model.hiddenDraftRows; internal/tui/chromelayout_test.go — TestInputContentRowsMirrorsTheWidget
**Tests:** equality test: new row starts equal the base algorithm's over a corpus (ASCII, wide CJK, emoji, VS16, ZWJ sequences, combining marks, a space followed by a combining mark, tabs, soft-wrap boundaries); a deterministic width-factor test: `TotalAlloc` of `wrapRowStarts` over one 64 KB line at width 400 ≤ ~2× the same line at width 40 (≈ 10× at base); a literal Model with no memo still counts rows correctly; a counting test that one keypress + View measures once, counted as `inputContentRows` memo misses (not `wrapRowStarts` calls — the View accent pass `inputCellSpans` calls it on its own); `BenchmarkInputContentRows`.
**Acceptance:** `go test -race -count=1 -run 'InputAccent|InputRows|WrapRow|InputContentRows|HiddenDraft|PromptScroll' ./internal/tui/`
**Commit:** `perf(tui): count prompt rows in one linear pass per frame`

## 4. The skill band re-ranks once per pause, not per keystroke — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the debounce is armed inside `recomputeAutocomplete` (its Cmd now batches the reload with the tick), so every edit-path caller — including `prompteditor.go`'s paste and `interject.go`'s restore — picks it up with no call-site change; the tick is registered in `doc.go`'s suggestion-band paragraph (a one-shot generation tick, the ctrlCResetMsg/flashClearMsg shape), not in the spinner/heartbeat chain notes.
NOTES (2026-09-23): a current tick landing while an overlay is open (only the Tab-opened suggestion menu can be, since the edit path already cleared the row for "/" and "@") leaves the row untouched so Esc brings the band back; `spendSkillHints` also bumps the generation so a pre-send tick lands inert.
NOTES (2026-09-23): CONTEXT.md carries no per-keystroke wording for Suggestion, so it is unchanged; ADR 0061's body lines ("free to change with every keystroke", "re-ranked per keystroke") are left as written and superseded by the dated amendment.
NOTES (2026-09-23): `-race` cannot run on this host (ThreadSanitizer: unsupported VMA range on the Pi kernel); the Acceptance command was run without `-race` and passed, as did the whole `./internal/tui/` package.

**What:**
**Goal:** a burst of edits (typing or a paste) runs `skills.Catalog.Suggest` once, ~150 ms after the last edit; a stale re-rank never paints; Enter spends the hints on screen at send time.
**Approach (assumed at the header base):** `Model.recomputeAutocomplete` (`autocomplete.go`) calls `recomputeSkillHints` (`suggestband.go`) on every edit. Split it: the edit path bumps a plain-value generation field and returns a `tea.Tick(150ms)` carrying it (the `ctrlCResetMsg{gen}` / `flashClearMsg{gen}` pattern); the tick handler re-ranks only when its gen is current. `spendSkillHints` keeps reading `m.skillHints` unchanged. Band clearing on send, `/clear` and an emptied draft stays immediate. Register the tick in `doc.go`'s tick-chain notes. Amend ADR 0061 with a dated amendment (Consequences' "re-ranked per keystroke") and fix the `suggestband.go` comment claiming no debounce/Cmd; update any CONTEXT.md "Suggestion" wording that says per keystroke.
**Regression guard.** `typeDraft` ends by delivering the tick at the model's current gen, so every caller gets the band. Arm the tick only when `recomputeSkillHints`' own guard would rank (knob on, catalog wired, no overlay open) — typing with the knob off still returns a nil Cmd — and clear `skillHints` immediately on overlay-open and knob-off. On the edit path, filter out of `m.skillHints` any id `refs.SkillRefs` finds in the draft (or rank synchronously in `spliceCompletion`), so the item yields to ADR 0061 §3's "a skill already invoked in the draft is never suggested at all". Prose rule: every comment or doc line saying the band is re-derived/re-ranked on the edit path or per keystroke — `grep -rn -i 'keystroke\|edit path\|re-derived on\|debounce' internal/tui/*.go docs/adr/0061-*.md CONTEXT.md | grep -i 'band\|hint\|skill\|suggest'`.
**Files:** internal/tui/suggestband.go, internal/tui/autocomplete.go, internal/tui/model.go, internal/tui/doc.go, internal/tui/suggestband_test.go, docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md, CONTEXT.md
**Read first:** internal/tui/autocomplete.go — Model.recomputeAutocomplete, Model.spliceCompletion; internal/tui/suggestband.go — Model.recomputeSkillHints, Model.spendSkillHints; internal/tui/model.go — ctrlCResetMsg arm, skillHints field comment; internal/tui/suggestband_test.go — typeDraft, gatedSuggest; internal/tui/keyclaim_test.go — TestTabAtIdleWithHintsReachesTheFramesOwnVerb; internal/tui/skill_test.go — TestSlashMenuReloadNilSafe
**Tests:** with `gatedSuggest`'s counter: 50 keypresses then the tick → exactly 1 `Suggest`; a paste → 1; a tick with an old gen → 0 and no repaint; Enter before the tick spends what is shown; emptied draft clears the band without waiting; a Tab-accepted skill leaves the band at once, before any tick; typing with the knob off returns a nil Cmd; opening an overlay clears the band at once. Update existing band tests to deliver the tick (via `typeDraft`); `TestTabAtIdleWithHintsReachesTheFramesOwnVerb`, `TestHintRowIsNearestTheInputBox` and `TestSlashMenuReloadNilSafe` pass unchanged in intent.
**Acceptance:** `go test -race -count=1 -run 'Band|Suggest|SkillHint|Hint|Tab|Autocomplete|SlashMenu' ./internal/tui/ ./internal/skills/`
**Commit:** `perf(tui): debounce the skill band so a burst of edits ranks once`

## 5. Readable wrapping is linear in line length — ✅ DONE (2026-09-23)

NOTES (2026-09-23): cutReadable now takes and returns a []rune (rest is a subslice); a new appendReadableSegment holds the per-segment loop and keeps a fitting segment's original bytes, and readableRow spells each row in one allocation — measured 364 KB / 711 allocs for a 64 KB no-space line (≈5.6×, under the 8× bound).
NOTES (2026-09-23): the Acceptance command's -race flag cannot run on this host (ThreadSanitizer: unsupported VMA range, 47-bit arm64); ran the same selection without -race.

**What:**
**Goal:** `wrapReadable`/`cutReadable` (`internal/tui/inspector.go`) wrap a line of L runes in O(L) time and allocation, with output identical to base.
**Approach (assumed at the header base):** each cut converts the remaining segment to `[]rune` and back to string; convert once and cut by rune/byte offsets.
**Regression guard.** The allocation bound is numeric: ≤ 8× input bytes for the one-`[]rune` design (a probed pass allocates ~6.7×), or name a byte-offset walk (`utf8.DecodeRuneInString`, substring rows) if the test is to hold near 2×. Advice Detail keeps invalid UTF-8 (`sanitize.strip` returns unrewritten text as is), and base turns a long line's invalid bytes into U+FFFD on every cut row: keep that U+FFFD result on cut rows, or say here that the output changes.
**Files:** internal/tui/inspector.go, internal/tui/inspector_test.go
**Read first:** internal/tui/inspector.go — wrapReadable, cutReadable, readablePassages.close, readablePassages.row; internal/tui/thinkingpane.go — thinkingRows; internal/tui/advicepane.go — adviceRows; internal/tui/inspector_test.go — TestReadableWrapsALongPassage; internal/sanitize/sanitize.go — strip
**Tests:** equality against the base algorithm over a corpus (long single line, wide runes, spaces at the column, no break opportunity, a non-UTF-8 line longer than the column); a bytes-allocated bound for one 64 KB single-line record (base: ~128 MB; bound ≤ 8× the input bytes); `BenchmarkWrapReadable`.
**Acceptance:** `go test -race -count=1 -run 'Readable|Inspector' ./internal/tui/`
**Commit:** `perf(tui): wrap readable text in one pass instead of re-slicing the remainder`

## 6. The /thinking pane wraps each record once per column — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the memo is a validation cache, not an invalidated one — entries are keyed on (run, turn, ordinal among same run+turn in the scoped list) and served only when the stored text == the record's text; a column or scope (viewedRun) change clears it and each render keeps only the entries it used. So push/drop/commitAt/commitAll carry no explicit invalidation (the assumed approach's "mutators invalidate what they touch"); the per-mutator table test pins that only text-changing mutators cost a wrap.
NOTES (2026-09-23): the memo pointer lives on thinkingBoard (field `rows *thinkingRowCache`, off thinkingRecord) and is allocated in newModel (model.go); thinkingBoardWith boards carry nil and render uncached.
NOTES (2026-09-23): consequential edit — internal/tui/model.go: made necessary by the memo pointer, which newModel must allocate.
NOTES (2026-09-23): `-race` cannot run on this box (ThreadSanitizer: unsupported VMA range, 47-bit VMA on the Pi kernel); the Acceptance command was run without `-race`. BenchmarkThinkingPaneRender at the 64×64 KB cap: ~5.5 ms/op on the Pi (base measured ≈ 1.6 s per chunk).

**What:**
**Goal:** a `/thinking` render re-wraps only records whose text changed since the last render at that column and scope; committed records are never re-wrapped while the column and scope hold; output equals base.
**Approach (assumed at the header base):** `thinkingRows` (`thinkingpane.go`) re-wraps the whole `thinkingBoard` (`thinking.go`) per call. Hold a pointer-held per-record row cache keyed on record identity + text length (or a per-record revision bumped by `append`), the wrap column and the scope (`viewedRun`); `push`, `drop`, `commitAt`, `commitAll` invalidate what they touch. Honour `layout.md` "The /thinking popup" (no elision, caps unchanged, follows the tail, recomposed on resize).
**Regression guard.** Do not key on text length: at `thinkingRecordCap` `keepLastBytes` holds an ASCII record at exactly 64 KB, so later chunks leave the length unchanged and the pane would freeze. Validate each entry on the text itself (store the string and compare with `==`) or on a revision every append bumps; keep the column in the key. Keep the key off `thinkingRecord` (a revision field turns `thinking_test.go`'s `slices.Equal` checks red — else list those test edits); make the cache nil-receiver-safe (`paintcache.go` precedent) so a `thinkingBoardWith` board renders uncached, and name the file that allocates the pointer (`model.go` if it lives on `Model`). Cache only each record's wrapped text rows; compose the heading (`runLabel`, which falls back until the run's head entry lands) fresh on every render.
**Files:** internal/tui/thinking.go, internal/tui/thinkingpane.go, internal/tui/thinking_test.go, internal/tui/thinkingpane_test.go
**Read first:** internal/tui/thinkingpane.go — thinkingRows, scopedThinking, thinkingWrapColumn; internal/tui/thinking.go — thinkingBoard.append, keepLastBytes, commitAt; internal/tui/runview.go — Model.runLabel; internal/tui/paintcache.go — paintCache (nil-safe pointer precedent); internal/tui/thinkingpane_test.go — thinkingBoardWith
**Tests:** a wrap-call counter: second render with an unchanged board wraps 0 records; one reasoning chunk wraps 1; a chunk landing on a record already at `thinkingRecordCap` re-wraps it and the pane shows the new tail; a width change or run-view scope change rewraps all; a heading shows the fallback label and then the real one once the head entry lands; a hand-built board renders uncached; per-mutator invalidation table; `TestThinkingPaneLosesNoText` and the follow test still pass; `BenchmarkThinkingPaneRender` at the 64×64 KB cap.
**Acceptance:** `go test -race -count=1 -run 'Thinking' ./internal/tui/`
**Commit:** `perf(tui): the /thinking pane wraps each record once per column`

## 7. A report pane lays its rows out once per render — ✅ DONE (2026-09-23)

NOTES (2026-09-23): consequential edit — internal/tui/thinkingpane_test.go: made necessary by the plan's "extend BenchmarkThinkingPaneRender", which item 6 created there (not in popup_test.go/reportpane_test.go); it now opens the pane with the bar on and times `renderReport(thinkingReport)` per chunk.
NOTES (2026-09-23): the window arithmetic is extracted as `popupRowSeat` (returns `popupRowSeating`), shared by `popupRowLinesAt` (which now takes pre-laid-out blocks) and `reportFullWindow`; a `wrapRows` spec in `reportFullWindow` still asks the painter (no report sets it today). The layout counter is a package `atomic.Int64` (`popupLayouts`) read by a non-parallel test, following the lineeditor_test.go HeapInuse precedent.
NOTES (2026-09-23): "single-column popups measure no per-row width" is met by `layoutPopupColumn`, which measures cells only until one is wider than zero (to keep the "" collapse); there is no measure-call counter, the test pins byte equality with the measured layout instead. `-race` cannot run on this host (ThreadSanitizer: unsupported VMA range, 47-bit arm64), so the Acceptance run was without `-race`.

**What:**
Depends on item 6.
**Goal:** one `renderReport` runs `layoutPopupRows` over the rows at most once, and single-column popups measure no per-row width; rendered output equals base.
**Approach (assumed at the header base):** `reportSpec` → `reportFullWindow` runs a full `renderPopupPlaced` just to size the window, `renderPopup` runs it again, and with the scrollbar on `popupRowLines` calls `popupRowLinesAt` twice — 4 layouts per render (`reportpane.go`, `popup.go`). Size the window from row counts, lay out once and reuse it for the scrollbar pass. In `popupColumnWidths`/`layoutPopupRow`, skip width measurement when there is one column (multi-column lists keep measuring all rows, per `popup.go`'s no-shift contract).
**Regression guard.** Short-circuit the one-column case only in `layoutPopupRows` (the non-wrap path), keeping `TrimRight(expandTabs(cell))` and the "" collapse; `popupColumnWidths` keeps measuring (a 0 width makes `popupLastColumn` -1 and blanks wrapped ask answers). Reuse the first composition for the scrollbar pass only when `!spec.wrapRows`; a wrapping spec keeps its second composition at `rowInner`. Derive the window through the same `popupRowPads`/`popupRowWindowFrom` arithmetic `popupRowLinesAt` uses (one shared helper), pad handback included — this supersedes the `reportFullWindow` doc comment (`reportpane.go`, "Asking costs one composition"), which is rewritten to match. Extends `BenchmarkThinkingPaneRender`, which item 6 creates.
**Files:** internal/tui/popup.go, internal/tui/reportpane.go, internal/tui/popup_test.go, internal/tui/reportpane_test.go
**Read first:** internal/tui/reportpane.go — reportFullWindow, renderReport, reportWindow; internal/tui/popup.go — popupRowLines, popupRowLinesAt, popupRowBlocks, layoutPopupRows, popupColumnWidths, popupRowPads; internal/tui/ask.go — askPrompt spec (wrapRows)
**Tests:** a layout counter shows 1 layout per `renderReport` (4 at base) for a non-wrapping spec; golden equality of `/thinking` and `/inspect` panes across overflow/no-overflow and scrollbar on/off; the window-from-counts equals the painter's across grants, a floor-grant case (maxRows ≤ pads+1) included; wrapped ask answers and `/settings` rows still paint; multi-column no-shift test still passes; extend `BenchmarkThinkingPaneRender`.
**Acceptance:** `go test -race -count=1 -run 'Popup|Report|Thinking|Inspect|Ask|Settings' ./internal/tui/`
**Commit:** `perf(tui): lay a report pane's rows out once per render`

## 8. Open panes render once per frame — ✅ DONE (2026-09-23)

NOTES (2026-09-23): re-derived from the assumption that the height-only query could sit beside render in panes.go/reportpane.go alone: every non-report pane composes its popupSpec inline in its own renderer, so the spec step was split out of approvalPromptPlaced (approval.go → approvalPromptSpec), askPromptPlaced (ask.go → askPromptSpec), renderListPlaced (listsurface.go → listSpec/listHeight, plus filteredListContent) and renderSettingsEnum (settings.go → settingsEnumContent; renderSettingsSubList became settingsSubListContent, its only caller being the enum), and popup.go gained popupHeight — the painter's own line arithmetic (elisionSplit, popupBodyPad's rule, popupRowSeat, popupHeading/popupTitleLine) asked for counts; popupHeading now takes the body's line count instead of its lines.
NOTES (2026-09-23): consequential edit — internal/tui/mouse.go: made necessary by transcriptRows no longer composing the overlays (the file-head comment said it "composes the frame").
NOTES (2026-09-23): reportWindow (the scroll keys' and wheel's window) now reads the seat from row counts through a new reportRowSeat shared with reportFullWindow instead of calling renderPopupPlaced — otherwise a pgdown on a report still rendered it twice per Update+View; a wrapRows spec still asks the painter, as before.
NOTES (2026-09-23): the render counter (paneRenders, panes.go) is incremented in frameOverlays, the table's one render walk, and counts closed panes' "" answers too, so the test's ceiling is ≤1 per paneSpecs row plus exactly 1 for each pane open after the Update. The height-equals-render test is named TestOverlayHeightQueryMatchesItsRender (not TestPane…) so the item's Acceptance regex selects it; it sweeps widths 2–140, heights 4–44 and the bar on/off over 16 fixtures (every pane, every /settings paint, ask with and without choices, empty browser/report). Mutating popupHeight's elision or empty-offering arithmetic makes it fail.
NOTES (2026-09-23): BenchmarkThinkingPaneUpdateAndView added (paintcache_test.go): /thinking at the record cap, one reasoning-chunk Update + View ≈ 18 ms/op on this Pi. `-race` cannot run on this host (ThreadSanitizer: unsupported VMA range), so Acceptance ran without it; the whole internal/tui package and golangci-lint (pinned version) pass.

**What:**
Recast at the regression check (2026-09-23).
Depends on items 6 and 7.
**Goal:** one non-pointer Update (engine Event, keypress) plus View renders each open overlay pane at most once; `settle`'s transcript clamp obtains pane heights without rendering pane content.
**Approach (assumed at the header base):** `settleFrame` → `settle` → `freshenTranscriptClamp` → `transcriptWidgetRows` → `frameOverlays()` renders every open pane after each Update; `View` and `layout()` call `frameOverlays()` again. Give panes a height-only query for the clamp, and memoise `frameOverlays()` per frame key behind the existing `Model.painted`/`frameKey` machinery (`paintcache.go`).
**Regression guard.** Drop the `frameKey` memo entirely — `frameKey` names none of the overlay inputs, and the item yields to `model.go`'s `frameOverlays` "pure function of the Model value" and `withFrameSpans` per-value guarantees. The height-only query replaces `frameOverlays` in `transcriptRows`/`transcriptWidgetRows`, so `settle` renders nothing and View renders once. The query lives beside `render` in the pane table (`panes.go`) and returns 0 wherever render returns "" (narrow width, unseated pane). Its cost win relies on item 6's cache (the query still composes `thinkingRows`). The clamp's correctness rests on the height query agreeing with render, pinned by the height-equals-rendered test across every pane in the pane table; this supersedes the `Model.View` comment in `internal/tui/model.go` that the clamp and the click map address "the same rows by construction" (`contentLineAt` now takes the query). The render counter counts per `paneSpecs` row: each open pane renders ≤ 1 per Update+View, not only `/thinking`. `TestEveryFramePaneHasASpec` nil-checks the new height field, so a row left nil fails there, not in `transcriptRows` on the first frame. Every comment saying `transcriptRows`, `layout` or the clamp composes or renders the overlays is rewritten to say it measures them — find them with `grep -n 'overlay composition\|overlays measure\|measures them to say\|composed once here\|measured by the same derivation' internal/tui/model.go`.
**Files:** internal/tui/model.go, internal/tui/panes.go, internal/tui/reportpane.go, internal/tui/paintcache_test.go, internal/tui/panes_test.go
**Read first:** internal/tui/model.go — Model.transcriptRows, Model.transcriptWidgetRows, Model.frameOverlays, Model.contentLineAt; internal/tui/panes.go — paneSpec, paneSpecs init; internal/tui/model_test.go — TestPaneHeightChangeReachesLayout; internal/tui/panes_test.go — TestEveryFramePaneHasASpec
**Tests:** an overlay-render counter per `paneSpecs` row: with each pane open, one non-pointer Update+View renders it at most once (`/thinking` on a reasoning chunk: ≥3 at base); `TestEveryFramePaneHasASpec` gains a nil check for the height field; pane heights from the height query equal the rendered heights across every pane in the pane table (`paneSpecs`), narrow-width and unseated cases (height 0) included — the clamp's correctness rests on this query agreeing with render, and this test is what pins it; frame output equals base; `TestPaneHeightChangeReachesLayout` and `TestANewPaneClaimingAKeyStillReachesLayout` still pass.
**Acceptance:** `go test -race -count=1 -run 'Paint|Overlay|Settle|Thinking|ReachesLayout|EveryFramePaneHasASpec' ./internal/tui/`
**Commit:** `perf(tui): render each open pane once per frame`

## 9. Reasoning deltas coalesce like tokens

**What:**
**Goal:** consecutive `ReasoningEvent`s for the same EventBase (Depth, Turn, CallID) that queue up in the TUI sink reach `Update` as one message carrying their concatenated text in order; events for different runs/blocks, and any interleaved non-reasoning event, keep their order and are never merged across.
**Approach (assumed at the header base):** `teaSink.Emit` (`internal/tui/sink.go`) merges only `TokenEvent`s; extend the same merge to `ReasoningEvent`.
**Regression guard.** Reasoning and visible tokens of one Turn share an EventBase, so the buffer carries the event kind as well as the EventBase and a change of kind flushes. The merge strips a split escape whole (as the token path already does over sink-merged tokens) — output changes from base only there. ADR 0011 (lines 53-54) names only `TokenEvent` coalescing; the item extends it (coalescing, never dropping), and every comment calling the sink's buffer "token-coalescing" or "coalesces adjacent tokens" (`grep -n -i coalesc internal/tui/*.go`: sink.go, worker.go, model.go, tui.go, transcript.go) is updated.
**Files:** internal/tui/sink.go, internal/tui/sink_test.go, internal/tui/worker.go, internal/tui/model.go, internal/tui/tui.go, internal/tui/transcript.go
**Read first:** internal/tui/sink.go — teaSink.Emit, emitToken, flushLocked, closeWindow; internal/tui/sink_test.go — TestTeaSinkCoalescesOnlyWithinOneStream, TestTeaSinkFlushesPendingBeforeEveryOtherVariant; internal/tui/thinking.go — thinkingBoard.append; internal/agent/loop.go — inline reasoning emit beside the TokenEvent emit
**Tests:** a burst of reasoning deltas → one merged event with the joined text; interleaved token/reasoning/tool events under an identical EventBase keep order and never merge across kinds; different run ids never merge; the thinking board's final record equals the unmerged sequence's for chunks whose escapes and runes are whole; one pinned split-escape case asserts the merged result.
**Acceptance:** `go test -race -count=1 -run 'Sink|Reasoning|Thinking' ./internal/tui/`
**Commit:** `perf(tui): coalesce queued reasoning deltas like tokens`

## 10. A cache-hit transcript block allocates nothing large

**What:**
**Goal:** repainting a transcript (or run view) whose blocks all hit the paint cache allocates under 1 KB per block.
**Approach (assumed at the header base):** `resolveBlock` (`render.go`) builds a `draw` closure capturing `th theme` (~32 KB) and `in paintInput` by value, so both escape to the heap on every call, hit or miss. Capture pointers, or build the closure only on a miss; confirm with `go build -gcflags=-m ./internal/tui/` that `th` no longer moves to heap there.
**Regression guard.** Passing `th` by pointer alone leaves ~2.1 KB per block (probed): to hold `<1 KB`, also stop `in` escaping and build the per-block `ins` slice only inside the miss path — else restate the bound as a measured ceiling ("no theme-sized allocation per block"). Multi-entry blocks (Tools umbrella, delegation group, sub-agent run row) materialise `len(ins)×832 B` per hit through `root.inputs` → `paintInputs`: have the key's span facts read the entries without materialising, or scope the bound to single-entry blocks and state the multi-entry cost. Name both `resolveBlock` and `resolveGroup`; `-gcflags=-m` shows no "moved to heap: th" in either.
**Files:** internal/tui/render.go, internal/tui/paintcache.go, internal/tui/paintcache_test.go
**Read first:** internal/tui/render.go — resolveBlock, resolveGroup, renderView, paintRoot.inputs; internal/tui/paintcache.go — paintBlock, blockKey, paintInputs, paintInput; internal/tui/paintcache_test.go — BenchmarkRenderViewStreaming, TestPaintCacheKeysOnTheRoot
**Tests:** a bytes-allocated bound for an all-hit `renderView` over 800 blocks (base ≈ 28 MB), with a multi-entry (collapsed run) fixture held to whichever bound the guard picks; `TestPaintCacheKeysOnTheRoot` and `BenchmarkRenderViewStreaming` still pass; add a run-view sub-benchmark.
**Acceptance:** `go test -race -count=1 -run 'Paint|Render|RunView' ./internal/tui/`
**Commit:** `perf(tui): stop the theme escaping to the heap on every cached block`

## 11. Widget cells are measured once per painted block

**What:**
Depends on item 10.
**Goal:** `reserveWidgetCells`' width result for a block is computed when the block is painted and reused on every cache hit; the reserved cells equal base.
**Approach (assumed at the header base):** `reserveWidgetCells` re-measures every transcript line with `th.measure` per repaint; store the per-block result in `blockPaint` and sum/fold it on hits. Keep one width authority (ADR 0030) — the stored value comes from the same measure the viewport widget mirrors.
**Regression guard.** `reserveWidgetCells` measures with `ansi.StringWidth` (via `overWidgetWidth`), never `th.measure` — the item yields to ADR 0030 §6 amendment 2026-08-30. Store per-line widget widths (limit-independent; the paint key's `transcriptWidth` maps two viewport widths to one), never a verdict tied to one limit, and compare them against the limit in `reserveWidgetCells`; keep the byte-length short-circuit. A line with no stored measure (separators, breadcrumb, rooted prompt, streaming preview, a hand-built `renderedTranscript`) is measured as base does. The per-block measure added to `blockPaint` must survive `blockPaint.railed` / `retargeted`, which rebuild the literal (`render.go`).
**Files:** internal/tui/render.go, internal/tui/paintcache.go, internal/tui/paintcache_test.go
**Read first:** internal/tui/render.go — reserveWidgetCells, overWidgetWidth, splitAtWidgetWidth, renderView, blockPaint.railed, blockPaint.retargeted; internal/tui/model.go — transcriptWidth; internal/tui/model_test.go — TestReserveWidgetCellsMovesTargetsAndSpansWithTheRows, TestPainterReservesTheCellsTheWidgetMeasuresOver
**Tests:** a measure counter on `overWidgetWidth` / `ansi.StringWidth` calls, over a fixture whose lines exceed the limit in bytes: an all-hit repaint measures 0 cached lines; one changed block measures only its lines (uncached lines excluded); a railed or retargeted block keeps its stored measure; reserved cells equal base across wide/narrow and the painter measure `th.measure` WcWidth vs GraphemeWidth; `TestReserveWidgetCellsMovesTargetsAndSpansWithTheRows` and `TestPainterReservesTheCellsTheWidgetMeasuresOver` still pass.
**Acceptance:** `go test -race -count=1 -run 'Paint|Render|Widget' ./internal/tui/`
**Commit:** `perf(tui): measure a block's widget cells once when it is painted`
