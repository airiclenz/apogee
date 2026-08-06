# Sub-agent context usage readout — implementation plan

- **Goal:** make a sub-agent's context usage visible — live on its transcript block in the TUI, and as per-run lines in `apogee headless` — closing ISSUES.md line 7 ("Currently I cannot see how much of it's context a sub agent has used").
- **Date:** 2026-08-05
- **Status:** not started
- **Authoritative sources:**
  - `internal/domain/events.go:124-137` — `UsageEvent` (carries `EventBase{Depth, Turn}` + `PromptTokens/CompletionTokens/TotalTokens`; emitted once per Turn at `internal/agent/loop.go:468-473` with the emitting agent's own depth; a sub-agent's usage already reaches every sink at `Depth > 0` and every consumer currently drops it — `internal/tui/fold.go:57-60`, `internal/run/run.go:288`).
  - `layout.md` §"A sub-agent run collapses to its call block" (~592-604), §"The status line's right slot" (~768-793), §"Where the window went" (~832-837), §"The live star" (~574-581) — the rendering spec this plan amends (item 6) and otherwise must not violate.
  - ADR 0011 (TUI is a thin renderer over the event stream), ADR 0013 (sub-agent orchestrator/depth), ADR 0014 (delegation is serialized — the headless bracketing in item 4 rests on it), ADR 0031 (Driver parity: derive everything from the shared events).
  - Fact established 2026-08-05 (scouted): a sub-agent inherits the parent's `Config` verbatim (`internal/agent/subagent.go:129`), so its context window **is** the parent's window — no separate denominator exists. CONTEXT.md's sub-agent entry ("a reduced context Budget", CONTEXT.md:103-118) has drifted from the code; item 6 corrects it.
- **Ratified design calls** (owner, 2026-08-05, via AskUserQuestion in the planning session):
  1. **Surface:** the usage readout rides the sub-agent block's collapsed **summary line** as a `·`-separated cell **between the count and the gist**: `N tool calls · 12k/32k · <gist>`. It ticks live as the child's turns land and the final figure persists on the finished block. No status-row/right-slot changes.
  2. **Spelling:** `<used>/<limit>` in the gauge's integer-k token spelling (`formatTokens`), e.g. `12k/32k`. No percent, no bar.
  3. **Headless:** included in this wave, at **per-run grain** — one stderr line per sub-agent run (final reading + the task's first line), printed **before** the existing summary line. The summary line itself is unchanged.
  4. **Persistence:** the figure survives session resume via an additive transcript wire-entry field (stays within `transcriptVersion = 1`).
- **Author-resolved mechanical calls** (plan author, 2026-08-05 — binding for implementers):
  - **Semantics = fill, not spend:** the reading is the **latest** `UsageEvent` total for that run (`TotalTokens`, falling back to `PromptTokens + CompletionTokens` — the same preference `fold.go:61-65` and `run.go:290-294` already use). Never cumulative across turns, never transitive across nesting: each agent has its own window, so a nested run's reading belongs to its own block/line only.
  - **Denominator frozen at fold time:** store *both* used and limit on the transcript entry when the usage folds, capturing the limit from the same number the gauge reads (`m.opts.ContextWindow`). A later heartbeat rebind must not rewrite a finished run's history.
  - **Self-hiding:** the cell (TUI) or line (headless) is omitted whenever used or limit is 0 — the gauge's own precedent (`model.go:3848-3850`); a fill only means something beside its limit.
  - **Attribution (TUI):** a `UsageEvent` with `Depth = N > 0` attaches to the most recent still-open sub-agent run whose head entry sits at depth `N-1` (the same family of heuristic `subAgentGist`/`subAgentSpan` already use, `render.go:336, 418`). No open run at that depth → drop the event, as today.
  - **Attribution (headless):** bracket by the delegating `sub_agent` `ToolCallEvent`/`ToolResultEvent` pair per depth; safe because delegation is serialized (ADR 0014). Runs report in finish order, flat (no indentation for nested runs); zero-reading runs are skipped.
- **Standing requirements:**
  - skills: coding-standards
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:**
  - Any status-row / right-slot change (the chrome keeps exactly one gauge; layout.md ~832-837 stands).
  - Sub-agent naming (TODO.md:25) — the headless task label is the raw task text's first line, not a name.
  - Implementing a genuinely reduced child Budget (item 6 only corrects CONTEXT.md's wording to match the code).
  - Concurrent sub-agent fan-out identity (TODO.md:437-448) — bracketing relies on serialized delegation; if fan-out ever lands, `UsageEvent` needs an identity field then.
  - Bench driver output; persisting per-sub-agent usage into the session record `Meta` for headless (the TUI's transcript codec is the only persistence in this wave).

## 1. Fold depth>0 usage onto its owning sub-agent transcript run — ✅ DONE (2026-08-06)

NOTES (2026-08-06): mechanism chosen under the item's "implementer's choice" clause — the usage fold is a sibling method `transcript.applyUsage(e, window)` called from `foldEvent` (fold.go) rather than a `UsageEvent` case inside `apply`: `apply(e)` has 153 call sites across the package's tests, so threading the window through its signature was not viable, and a hidden window field on the transcript would have to be kept in sync with a rebindable `m.opts.ContextWindow`. `foldStats` keeps its `Depth != 0` break for the gauge (comment amended); the stale `apply` doc comment now describes the new routing. Entry fields are `ctxUsed` / `ctxLimit`.

**What:** In `internal/tui`, stop discarding sub-agent usage and attach it to the owning transcript entry.

- `fold.go` (`foldStats`, ~47-77): the `Depth != 0` gate stays for the **gauge** (`m.ctxUsed` remains top-level-only), but a `Depth > 0` `UsageEvent` now routes to the transcript instead of breaking.
- `transcript.go`: `apply` (~418-441) gains a `UsageEvent` case for `Depth > 0`: find the most recent still-open sub-agent run (head entry is a `sub_agent` tool call, not done, at depth `e.Depth-1`) and set two new entry fields — the used reading (latest-total semantics per the header) and the limit captured from `m.opts.ContextWindow` at fold time (thread the window into `apply` or set the fields from `foldEvent`; the mechanism is the implementer's choice). No matching open run → no-op. Update the stale comment at `transcript.go:411` ("UsageEvent is not a transcript-rendered variant") to describe the new routing.
- No rendering yet — this item is state only (item 2 paints it).

**Tests:**
- `fold_test.go`: update the fold-table row for `UsageEvent` (~108-109, "moves the gauge and nothing else" is no longer true); `TestFoldEventCoversEveryEventVariant` needs no new variant, only the amended row.
- `model_test.go` `TestUsageEventDrivesGaugeAndThroughput` (~288): the existing assertion that a `Depth: 1` usage event does NOT move the top-level gauge (~317-324) is **kept** — extend it to assert the reading landed on the open sub-agent entry instead.
- New transcript tests: attribution to the most recent open run at the right depth; a second sequential run at the same depth gets its own reading while the first run's figure stays frozen after `done`; a nested (depth-2) run's usage lands on the nested head, not the outer one; a `Depth > 0` usage event with no open sub-agent run changes nothing.

**Acceptance:** `go test ./internal/tui/` passes; `go test ./internal/tui/ -run 'TestUsageEventDrivesGaugeAndThroughput|TestFoldEventCoversEveryEventVariant'` passes.

**Commit:** `feat(tui): fold sub-agent usage onto its transcript run`

## 2. Render the usage cell on the sub-agent summary line — ✅ DONE (2026-08-06)

Depends on item 1.

NOTES (2026-08-06): two additions beyond the item's named files, both documentation of the code this item changed. `render.go` gained a small `subAgentFill(head entry) string` helper beside `subAgentSummary` (the omit-when-zero rule stated once, where the codec and the gauge precedent can be cited); `internal/tui/doc.go` (~56) had its one clause about the collapsed run's summary slot amended to name the fill cell and its non-transitivity — the sentence described the very composition this item changed. `paintKey` gained the reading as two int fields (`ctxUsed`/`ctxLimit`) rather than one composite, matching the entry's own pair. `TestCollapsedRunSaysItsGistOnce` keeps its 2-line pin unweakened and its fixture now carries a reading, so the pin also covers "the cell rides the existing summary line".

**What:** Paint the stored reading as the summary line's middle cell, and make the paint cache honest about it.

- `render.go` `subAgentSummary` (~396-408): compose `N tool calls · <used>/<limit> · <gist>` — the cell sits between the count and the gist so a long gist clips, not the figure; spelled with the existing `formatTokens` (`model.go:3893`); omitted entirely (along with its `·`) when used or limit is 0, degrading to today's `N tool calls · <gist>`. With no gist the line reads `N tool calls · 12k/32k`.
- The collapsed run stays exactly two rows (header + branch) — the cell rides the existing summary line; `TestCollapsedRunSaysItsGistOnce` must keep passing unweakened.
- `paintcache.go`: the reading joins the paint key (`paintKey`/`blockKey`, ~65-86, 201-213) so a bare `UsageEvent` — which adds no entry and flips no `done`/`expanded` flag — still invalidates the block. Without this the block serves a stale figure; the cache is a validation cache (`paintcache.go:57-61`).
- Live ticking needs no new clock: `eventMsg` already refreshes the viewport on every folded event (`model.go:529-532`), honoring layout.md's "the transcript carries no timer of its own".

**Tests:**
- Golden updates: `TestSubAgentRunCollapsesToItsCallBlock` (`transcript_test.go:913`) gains the cell; `TestSubAgentSummaryTempi` (`transcript_test.go:1007`) gains with-usage variants of its tempi (live phrase, count-alone, report gist) plus the no-usage degradation; `TestCollapsedRunSaysItsGistOnce` (`transcript_test.go:968`) still pins exactly 2 lines.
- `TestSubAgentCountIsTransitive` (`transcript_test.go:1067`): extend to pin that the usage cell is NOT transitive — a nested run's reading never appears on the outer run's line.
- `paintcache_test.go`: extend `TestPaintCacheMatchesAColdRenderThroughEveryMutation` (~147) with a usage-reading mutation; `TestPaintCacheRepaintsWhenTheKeyMoves` (~88) covers the new key field.

**Acceptance:** `go test ./internal/tui/` passes; `go test ./internal/tui/ -run 'TestSubAgent|TestCollapsedRun|TestPaintCache'` passes.

**Commit:** `feat(tui): sub-agent summary line shows context usage`

## 3. Persist the reading across session resume — ✅ DONE (2026-08-06)

Depends on item 1.

**What:** Carry the two entry fields through the transcript codec so a reopened session shows each finished sub-agent block's final figure.

- `transcriptcodec.go` (~34, 55-75): additive `wireEntry` fields for the used reading and its limit, staying within `transcriptVersion = 1` (`omitempty`, like the codec's existing optional fields); encode from and restore to the entry fields item 1 added.
- Old sessions without the fields decode to zero → the cell self-hides (item 2's rule); no migration.

**Tests:** codec round-trip test covering a sub-agent entry with a reading (both fields survive); a wire entry without the fields decodes to zeros; existing codec tests unbroken.

**Acceptance:** `go test ./internal/tui/ -run 'Codec|Wire|Transcript'` passes; `go test ./internal/tui/` passes.

**Commit:** `feat(tui): persist sub-agent context usage across resume`

## 4. internal/run: Result carries per-sub-agent usage

Independent of items 1–3 (shares only the event contract).

**What:** Teach the headless driver's `eventTap` to bracket sub-agent runs and report each run's final reading.

- `internal/run/run.go` `eventTap` (~262-308): add cases for `ToolCallEvent`/`ToolResultEvent` where the tool is `sub_agent` (`tools.SubAgentToolName`): the call event at depth `d` opens a run bracket for child depth `d+1`, retaining the task arg's first line (the args are on the event — the TUI's `firstLineArg("task")` presentation proves it); a `Depth > 0` `UsageEvent` updates the open bracket at its depth with latest-total semantics; the result event closes the bracket and appends a finished run record. One open bracket per depth suffices — delegation is serialized (ADR 0014); if an event arrives with no matching bracket, drop it defensively. The existing `Depth == 0` behavior of `t.total`/`t.final` is untouched.
- `Result` (~53-79) gains an additive field, e.g. `SubAgents []SubAgentUsage` with `{Used, Limit int; Task string}`, in finish order; `Limit` is the run's window — `spec.Config.Context.MaxContextTokens` (the child inherits it verbatim). Runs with a zero reading are skipped.
- Update the tap's doc paragraph (~272-274, "Both readings are top-level only") and `internal/run/doc.go`'s v1-scope section (~33-48) to state the new per-sub-agent reporting.

**Tests (in `internal/run`):**
- New test in the style of `TestOnceIgnoresASubAgentsAnswer` (`run_test.go:366`, harness `harness_test.go:249`): a scripted run whose sub-agent emits usage at depth 1 yields one `SubAgents` entry with the task line and the latest reading, while the top-level fill (`Meta.CtxUsed`) is unaffected.
- Pin the previously-untested top-level filter: a depth-1 `UsageEvent` never moves `tap.fill()`.
- Nested case: a depth-2 run produces its own entry; nothing accrues to the depth-1 entry.

**Acceptance:** `go test ./internal/run/` passes.

**Commit:** `feat(run): result carries per-sub-agent context usage`

## 5. cmd/apogee: headless prints per-sub-agent lines

Depends on item 4.

**What:** Surface `Result.SubAgents` on stderr, one line per run, before the summary line.

- `cmd/apogee/headless.go` (~364-367): before printing `headlessSummary(res)`, print one stderr line per `SubAgents` entry in order: `sub-agent: <used>/<limit> · <task first line>` (the ratified preview's shape), the task clipped to 80 runes. Token figures use the gauge's integer-k spelling via a small local helper in `cmd/apogee` mirroring the TUI's `formatTokens` (the TUI's is package-internal; a ~10-line local twin is the intended shape, pinned by test). No entries → no lines; `headlessSummary` itself is unchanged; stdout stays answer-only.
- Doc surfaces owned by this item: the command's `Long` help text (~111-128, the output-split paragraph); `README.md` §"Running one prompt — apogee headless" (~643-680) — a sentence describing the per-run lines in the output description; `docs/design/technical-design.md:208` — the `CLI / headless / probe` row gains the per-sub-agent stderr lines (its "Only `Result.FinalText` reaches stdout" claim stays true).

**Tests (in `cmd/apogee/headless_test.go`):**
- Extend the `stubRunner` result with `SubAgents` entries: stderr contains the per-run lines in order and before the summary; stdout remains exactly the answer.
- Empty `SubAgents` → stderr has no `sub-agent:` line (extend an existing routing subtest).
- The never-started-run guards (keyed on `"turns:"`) keep passing; add the same guard for `"sub-agent:"` in one of them.
- Pin the local token-spelling helper against the TUI's spelling (e.g. 18432 → `18k`, sub-1000 bare).

**Acceptance:** `go test ./cmd/apogee/` passes.

**Commit:** `feat(cli): headless prints per-sub-agent context lines`

## 6. Docs: spec amendments, drift fix, changelog, issue closed

Depends on items 1–5. This item owns every cross-cutting doc amendment.

**What:**
- `layout.md` §"A sub-agent run collapses to its call block" (~592-604): the summary-line grammar becomes `N tool calls · <used>/<window> · ` + gist; state that the cell appears only once a reading exists, ticks per child Turn, freezes on the final reading, and — unlike the count — is **not** transitive because each agent fills its own window.
- `layout.md` §"Where the window went" (~832-837): one clarifying sentence — the chrome still has exactly one gauge; a sub-agent run states its own fill on its own block, which is transcript content, not chrome.
- `CONTEXT.md` sub-agent entry (~103-118): correct the "reduced context Budget" drift — a sub-agent inherits the parent's context window verbatim today — and add that its context fill is visible on its run block (TUI) and per-run stderr lines (headless).
- `CHANGELOG.md`: entries for the TUI readout and the headless lines under the current unreleased section. **No version identifier changes** (VERSION, release headings, tags — per standing policy).
- `ISSUES.md` line 7: marker `[P]` → `[X]`.

**Tests:** none (docs only).

**Acceptance:** `grep -F 'tool calls · <used>/<window>' layout.md` (or the exact grammar as written) succeeds; `grep -n 'sub-agent' CHANGELOG.md` shows the new entries; `grep -n '\[X\].*context.*sub agent' ISSUES.md` succeeds; `make check` passes as the whole-plan backstop.

**Commit:** `docs: record the sub-agent context usage readout`

---

**Suggested version bump:** minor (a user-visible TUI feature plus a headless output extension) — the owner decides whether and when; no item in this plan touches a version identifier.
