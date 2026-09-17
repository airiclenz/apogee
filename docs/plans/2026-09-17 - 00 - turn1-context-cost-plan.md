# Plan: Turn-1 context cost — a first-class engine report

**Goal:** Measure and surface what apogee itself puts in front of the model at Turn 1 — the standing system content and the tool menu — as a per-piece estimate and, once measured, the Turn-1 `prompt_tokens`. Surface it in `apogee probe context`, the headless frames and the facade so the bench can chart it, and pin the injected bytes with a golden so growth is deliberate.
**Date:** 2026-09-17
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base commit:** 6e025093

**Sources:**
- `internal/agent/standingblocks.go`, `internal/agent/loop.go` (`buildRequest`, `standingSystem`, `toolMenu`), `internal/agent/wire.go` (`toProviderRequest`, `toolInstructions`)
- `internal/domain/contextfile.go` (`ContextFilesReport`), `internal/agent/contextfiles.go` (`ContextFilesReport()`)
- `internal/context/budget.go` (`TokenEstimator`), `internal/domain/budget.go` (`PromptChars`)
- `internal/eventjson/writer.go` (`RunStarted`, `RunFinished`, `ContextFiles`), `internal/run/run.go` (`eventTap.noteUsage`, `Result`)
- `cmd/apogee/probe.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/wire_config.go` (`projectConfig`), `cmd/apogee/wire_firing.go` (`firingConfig`)
- ADR 0021 (probe free/paid split), ADR 0061 D2 (skills not standing), ADR 0075 (headless Event lines, v:2 additive), ADR 0076 (Bypass)

**Ratified design calls (owner, 2026-09-17):**
- **Scope:** everything apogee injects — prompt, orientation, delegate report, task list, context files, tool menu, tool-instruction block (non-native profiles). Excludes the user's message. One total plus a per-piece breakdown.
- **Source:** real provider `prompt_tokens` when measured; offline chars/token estimate otherwise, always labelled `~`.
- **Surfaces:** `apogee probe context`, headless `--format json` frames, facade `Agent.ContextCost()`.
- **Tripwire:** a golden test pins BYTES per piece for a fixed fixture with workspace paths normalised; tokens are derived, never pinned.
- **Probe send:** estimate by default; `--live` sends one fixed one-word Turn-1 request to the configured endpoint (paid only on the flag, ADR 0021).
- **Reactions delta:** measured only — no dry-run of the pre-request cascade. Offline: one column plus a note when advise/shape Reactions are armed. `--live` with Reactions armed sends twice (as configured, then Bypass) and prints both measured columns. Probe gains no `--bypass` flag.
- **Headless:** `run_finished.context_cost` (estimate, taken idle after construction) + `run_finished.turn1_prompt_tokens` / `turn1_cached_prompt_tokens` (measured); `run_started` untouched. Additive under `v:2`; text mode gains one line. (Supersedes the run_started call — owner, 2026-09-17, after the regression check found run_started precedes Agent construction.)
- **Name:** CONTEXT.md term **Context cost**; `domain.ContextCost`, `Agent.ContextCost()`, facade alias `apogee.ContextCost`.
- **Old field:** `ContextFilesReport.StandingTokens` and `run_finished.context_files.standing_tokens` stay, derived from the new report.
- **ADR:** one short ADR — "Context cost is a first-class engine report".

**Regression check (2026-09-17, 6e025093):**
- 1: guard folded (frontmatter `---` / `Status: accepted` / `---`, date in the body)
- 2: guard folded (wire-faithful rows: seeded gate, `tool instructions` replaces `tool menu` off-native, `doc.go` map lines); yields to `internal/domain/contextfile.go:25-27` / `internal/agent/contextfiles.go:271-272` on `StandingTokens` (whole standing string, not a per-row sum)
- 3: guard folded (`SetScratchDir(t.TempDir())` before rendering; `tuitest.GoldenText` + its `-update`, no second flag); names the superseded source (`docs/design/test-drivers.md:419-423`, ADR 0062 call 13 — ADR 0079 carries the second supersession)
- 4: recast (the estimate rides `run_finished.context_cost`; `run_started` untouched)
- 5: guard folded (stubllm HTTP server via `--endpoint`, never `stubllm.InProcess`; offline probe never Steps; `doc.go` map lines)
- 6: guard folded (same stub-server pattern as 5)
- 7: guard folded (manual describes `run_finished.context_cost`)
- Re-check round (2026-09-17, 6e025093) — the regression-1/-2 `G:` lines the first pass left unfolded, verified against the tree and folded, plus regression-3:
- 1 (re-check): guard folded (the What names the second supersession of ADR 0062 call 13 — the Goldens rule at `docs/design/test-drivers.md:419-423`, superseded once by ADR 0075 §14 — that item 3's guard relies on)
- 2 (re-check): What made consistent with the yield (`ContextFilesReport()` is not rewritten; `StandingTokens` stays the whole-standing-string estimate; `ContextCost()` is a sibling producer over the same `standingBlock` renders; the test asserts the standing rows' bytes plus the `"\n\n"` joiners equal `len(a.standingSystem())`)
- 4 (re-check): recast (H2 retitled to `run_finished`; the header's **Headless:** call replaced, superseding the run_started call; regression-3's guard folded — `RunFinished.ContextCost ContextCost` as a value beside `ContextFiles`, no `omitempty`, no `RunStarted.` spelling; `"bytes"`/`"tokens"` redactions on the eventlines goldens with `context_cost.tokens > 0` asserted semantically; all three goldens plus `TestWriterEnvelopeOrderAndNulls`' pinned `run_finished` line refreshed; the text line self-hides on empty `Rows` and lists the rows present; `Turn1Usage` from the event's per-call fields, never `Cumulative`)
- 5 (re-check): guard folded (compose under the resolved startup mode, mode printed in the header, `--live` relies on item 6's ctx cancel not on Plan; `firingConfig` is never called — it dials the beat, creates the scratch dir and may dial the sub-agents server — the helper stops before beat, mkdir and routing, `ScratchDir` set to the would-be path uncreated; docmap acceptance line added)
- 6 (re-check): guard folded (the mechanism is the sink's synchronous ctx cancel on the first Depth-0 UsageEvent, before any dispatch — Plan does not stop read-only tools; the deny-all Approver is belt-and-braces; the `reactions:` sync lane is armed on the first Agent as `run.Once` does, unarmed under Bypass on the second)
- 7 (re-check): guard folded (`docs/manual/probe.md` has no `##` headings — a paragraph after the `probe config` block, before **When a frame comes out wrong**; "the same number a headless run in the same mode reports on `run_finished.context_cost`"; `newProbeCommand`'s doc-comment subject count bumped)

**Standing requirements:**
- skills: coding-standards
- Deviations from item text land as a dated NOTES line under the item.
- No `--bypass` on probe; no new headless line kind (frames grow, `eventjson.Kinds()` is unchanged).

**Out of scope:**
- A tokenizer; a config budget ceiling / startup warning on the number; a bypass column on an idle estimate.
- Skill bodies and `@file` refs (per-message, not Turn-1 standing cost — ADR 0061 D2).
- TUI surfaces (`/usage`, status line) — a later plan once the bench has used the number.
- apogee-sim changes (external module).

## 1. ADR 0079 and the CONTEXT.md term

**What:** Write `docs/adr/0079-context-cost-is-a-first-class-engine-report.md` in the house ADR format (status Accepted, date 2026-09-17): the ratified calls above as decisions, with the estimate-vs-measured distinction, the bytes-pinned golden, the paid-only-on-`--live` rule and the additive headless keys. The ADR also names the second supersession of ADR 0062 call 13 — the goldens-for-rendering-surfaces-only rule at `docs/design/test-drivers.md:419-423` (restated at `internal/tuitest/golden.go:14-17`), superseded once by ADR 0075 §14 — for item 3's bytes-per-piece golden, which is the supersession item 3's guard relies on. Add a **Context cost** entry to `CONTEXT.md` next to **Context files** / **System prompt** (read that section first for the house voice): "the tokens apogee itself puts in front of the model before the user's message — the standing system content plus the tool menu — as a per-piece estimate and, once measured, the Turn-1 `prompt_tokens`; the Reactions' directives are part of it only when measured". Cross-link the ADR from the term.

**Regression guard.** ADR uses the house frontmatter (`---` / `Status: accepted` / `---`), the date 2026-09-17 in the body. It names the second supersession of ADR 0062 call 13 (`docs/design/test-drivers.md:419-423`) that item 3's golden relies on; `docs/design/test-drivers.md` itself is not edited by this plan.

**Files:** `docs/adr/0079-context-cost-is-a-first-class-engine-report.md`, `CONTEXT.md`
**Read first:** docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md — frontmatter shape (`Status:` line only, no date field); CONTEXT.md — **System prompt**, **Context files** entries (Context and history section); docs/design/test-drivers.md — Goldens section (the ADR 0062 rendering-surfaces-only rule item 3 needs 0079 to supersede); apogee_alias_internal_test.go — TestEveryDomainEventVariantIsAliased (the only facade guard; ContextCost is not an Event, so it is untouched)

**Tests:** none (docs).

**Acceptance:**
- `test -f "docs/adr/0079-context-cost-is-a-first-class-engine-report.md"`
- `grep -n "Context cost" CONTEXT.md`
- `go test ./... -run 'TestADR|TestContext' -count=1` (any doc-index guard that pins ADR numbering or CONTEXT.md structure still passes)

**Commit:** `docs(adr): 0079 — context cost is a first-class engine report`

## 2. `domain.ContextCost` and `Agent.ContextCost()`

**What:** Add `internal/domain/contextcost.go`: `type ContextCostRow struct{ Name string; Bytes int; Tokens int }` and `type ContextCost struct{ Rows []ContextCostRow; Bytes, Tokens int; Calibrated bool }` with a pure `Total()`-free design — `Bytes`/`Tokens` are filled by the producer, `Calibrated` says whether `Tokens` came from a calibrated estimator. Row names are the `standingBlocks()` names in wire order (`prompt`, `orientation`, `delegate report`, `task list`, `context files`) followed by `tool menu` (the `domain.PromptChars`-style sum of every `ToolDef` Name+Description+Schema) and `tool instructions` (the rendered non-native block, absent on a native profile). Empty rows are omitted. Add `(*Agent).ContextCost() domain.ContextCost` in `internal/agent/contextcost.go`: idle-only read like `ContextFilesReport`, rendering each block through its `standingBlock.render`, the menu through `a.toolMenu()`, the instruction block through `a.toolInstructions` only when the profile is non-native; tokens via `a.budget().EstimateTokens(bytes)` per row, `Calibrated` from the estimator (add a `Calibrated() bool` accessor to `context.TokenEstimator` if none exists). `ContextFilesReport()` is NOT rewritten and `StandingTokens` stays the whole-standing-string estimate (`budget.EstimateTokens(len(standingSystem()))`, joins included); `ContextCost()` is a sibling producer over the same `standingBlock` renders, so the two agree by construction, and the item's test asserts that the standing rows' bytes plus the `"\n\n"` joiners between them equal `len(a.standingSystem())`. Export `type ContextCost = domain.ContextCost` and `ContextCostRow` on the facade in `apogee.go` beside `ContextFilesReport`.

Binding standards: no new render paths — `ContextCost()` reads the same `standingBlock` table `standingSystem()` and `ContextFilesReport()` read, so a new standing row is counted automatically and neither producer renders a block the other does not.

**Regression guard.** Rows are wire-faithful — ride-along rows (orientation, delegate report, task list) are counted only when a configured row (prompt or context files) seeds, exactly as `standingSystem()` gates them; on a non-native profile the `tool instructions` row REPLACES the `tool menu` row (tools are nil on the wire), never both; every new non-test .go file gets its package `doc.go` map line, listed in Files. On `StandingTokens` the item yields to the documented decision at `internal/domain/contextfile.go:25-27` and `internal/agent/contextfiles.go:271-272`: it stays the estimate over the WHOLE standing system content exactly as seeded (`budget.EstimateTokens(len(standingSystem()))`, joins included), not a per-row sum — per-row `Tokens` are derived for display and never summed into it.

**Files:** `internal/domain/contextcost.go`, `internal/domain/doc.go`, `internal/agent/contextcost.go`, `internal/agent/doc.go`, `internal/agent/contextcost_test.go`, `internal/context/budget.go`, `apogee.go`
**Read first:** internal/agent/loop.go — standingSystem, buildRequest, toolMenu, budget; internal/agent/standingblocks.go — standingBlock, standingBlocks; internal/agent/wire.go — toProviderRequest, toolInstructions; internal/agent/contextfiles.go — ContextFilesReport, contextBlocks; internal/context/budget.go — TokenEstimator, Calibrate, Used (calibration is detected as Used > 0 today, loop.go uncalibratedRoomMargin); internal/domain/budget.go — Budget.EstimateTokens, PromptChars; internal/agent/contextfiles_test.go — contextConfig, TestContextFilesReportMeasuresStandingContent; internal/agent/harness_test.go — baseConfig, scriptedResponder (stubllm.Turn.Usage{Prompt} drives Calibrate at loop.go:726)

**Tests:** `internal/agent/contextcost_test.go` over the `harness_test.go` helpers (`baseConfig`, `scriptedResponder`): rows appear in wire order and only when non-empty; a WorkspaceDir-only, prompt-less Agent (`use-default-prompt: false`, no context file) reports no standing rows at all while the tool rows stay; `tool menu` bytes equal the sum over `a.toolMenu()`; on a non-native profile `tool instructions` replaces `tool menu` (never both), on a native profile the reverse; `Calibrated` flips after one `DeltaDone` usage; the standing rows' bytes plus the `"\n\n"` joiners between them equal `len(a.standingSystem())` on a seeded Agent; `ContextFilesReport().StandingTokens` is unchanged for every existing case (`TestDocMapNamesEveryFile` green in `internal/domain` and `internal/agent`). Existing `contextfiles_test.go` and `standingblocks_test.go` unchanged and green.

**Acceptance:**
- `go build ./... && go vet ./internal/agent ./internal/domain ./internal/context`
- `go test ./internal/agent -run 'TestContextCost|TestContextFilesReport|TestStandingBlocks' -count=1`
- `go test . -run TestBenchReadiness -count=1`

**Commit:** `feat(agent): ContextCost reports the Turn-1 standing content and tool menu per piece`

## 3. Golden tripwire on injected bytes

**What:** Depends on item 2. Add `internal/agent/contextcost_golden_test.go` + `internal/agent/testdata/contextcost.golden`: build an Agent from `baseConfig` with the embedded default prompt (`config.DefaultSystemPrompt()`), the default roster (`tools.NewDefaultRegistryWithHost` as `defaultRoster` builds it), a fixture workspace under `testdata/contextcost-ws/` holding one `AGENTS.md`, native profile, depth 0, Plan mode off. Render `ContextCost()` and compare `Name` + `Bytes` per row against the golden as `name<TAB>bytes` lines. Normalise before comparing: the fixture's absolute workspace and scratch paths are substituted by the literals `<ws>` and `<scratch>` in the rendered prompt and orientation text before their bytes are counted (count the normalised text, not the live one), so the golden is machine-independent. `-update` flag rewrites the golden. The failure message names the row that moved and says the golden is updated deliberately with `go test ./internal/agent -run TestContextCostGolden -update`.

**Regression guard.** Call `SetScratchDir(t.TempDir())` on the fixture Agent before rendering, so the scratch bullet renders and is normalised to `<scratch>` like the workspace (the fixture otherwise carries no scratch dir and `ScratchDir()` is `""`, which would make the substitution insert `<scratch>` between every byte). Compare and `-update` through `tuitest.GoldenText` (`internal/tuitest/golden.go` already registers the package-wide `-update` flag; a second `flag.Bool("update")` collides at init), wrapping the diff with the row-naming message. This bytes-per-piece golden is a second supersession of the goldens-for-rendering-surfaces-only rule (`docs/design/test-drivers.md:419-423`, ADR 0062 call 13, restated at `internal/tuitest/golden.go:14-17`; superseded once by ADR 0075 §14) — ADR 0079 (item 1) is where that supersession is named.

**Files:** `internal/agent/contextcost_golden_test.go`, `internal/agent/testdata/contextcost.golden`, `internal/agent/testdata/contextcost-ws/AGENTS.md`
**Read first:** internal/agent/contextfiles_test.go — contextConfig; internal/agent/harness_test.go — baseConfig, echoResponder; internal/agent/agent.go — SetScratchDir, ScratchDir, Mode; internal/agent/orientation.go — orientationBlock, prompts/orientation.txt; internal/config/defaults.go — DefaultSystemPrompt (defaults/prompt.txt renders {{workspace}}, {{datetime}} date-only, {{mode}} — byte-stable); internal/agent/construct.go — resolveTools, defaultRoster; internal/tuitest/golden.go — GoldenText, compareGolden, updateGolden

**Tests:** the golden test itself (compare and `-update` via `tuitest.GoldenText`, fixture Agent with `SetScratchDir(t.TempDir())`); a negative check that appending one byte to the fixture `AGENTS.md` in a temp copy fails the compare.

**Acceptance:**
- `go test ./internal/agent -run TestContextCostGolden -count=1`
- `go test ./internal/agent -run TestContextCostGolden -count=1 -update && git diff --exit-code internal/agent/testdata/contextcost.golden` (a fresh update is a no-op; `-update` is `tuitest`'s flag)

**Commit:** `test(agent): golden pins the bytes apogee injects at Turn 1 per piece`

## 4. Headless: the context cost and the measured Turn-1 tokens on run_finished

**What:** Recast at the regression check (2026-09-17). Depends on item 2. In `internal/eventjson/writer.go` add `ContextCost` (mirrors `domain.ContextCost`: `rows[]{name, bytes, tokens}`, `bytes`, `tokens`, `calibrated`) as `RunFinished.ContextCost ContextCost` (`json:"context_cost"`, a value beside `ContextFiles` so the not-started frames carry it zero-valued like `context_files` does), and `RunFinished.Turn1PromptTokens int` (`json:"turn1_prompt_tokens"`) + `Turn1CachedPromptTokens int` (`json:"turn1_cached_prompt_tokens"`). In `internal/run/run.go`: `Result` gains `ContextCost domain.ContextCost` (taken idle after construction, before the first Step) and `Turn1Usage domain.Usage` — `eventTap.noteUsage` records the first Depth-0 `UsageEvent` with `Turn == 0` and `Maintenance == false`, from the event's own per-call fields (`ev.PromptTokens`, `ev.CachedPromptTokens`), never `ev.Cumulative`. `cmd/apogee/headless.go` fills the `run_finished` frame; text mode prints one line after the existing `usage:` line: `context cost: ~N tokens (prompt A · orientation B · context files C · tool menu D)` when unmeasured — the parenthesis lists only the rows present, and no line at all when `Rows` is empty — and `context cost: N tokens measured at turn 1 (estimate ~M)` when measured. Update `docs/manual/headless.md` "The two frames" to list the three keys and "`v` is `2`" stays true (additive). Refresh all three eventlines goldens (`run.jsonl`, `not-started.jsonl`, `not-started-after-sink.jsonl`) and the pinned `run_finished` line in `TestWriterEnvelopeOrderAndNulls`.

Enumerated consumers of `RunStarted`/`RunFinished`: `cmd/apogee/headless.go`, `cmd/apogee/e2e_eventlines_test.go`, `cmd/apogee/headless_test.go`, `internal/eventjson/writer_test.go`, `docs/manual/headless.md`.

**Regression guard.** Ratified design call (owner, 2026-09-17, supersedes the run_started call): the estimate rides `run_finished.context_cost` beside `turn1_prompt_tokens` / `turn1_cached_prompt_tokens`; `run_started` is untouched; `Result.ContextCost` is taken idle after construction, before the first Step. The wire member is `RunFinished.ContextCost ContextCost` (`json:"context_cost"`, a value beside `ContextFiles` so the not-started frames carry it zero-valued like `context_files` does — `internal/eventjson/writer.go` RunFinished); no `omitempty`, no `RunStarted.` spelling anywhere in the item. `context_cost.rows[].bytes`/`tokens` and the totals spell the run's workspace and scratch-dir path lengths (the reason `standing_tokens` is redacted, `cmd/apogee/e2e_eventlines_test.go:236-246`): in TestE2EEventLinesGolden add `tuitest.Redact(`"bytes":\d+`, `"bytes":"<bytes>"`)` and `tuitest.Redact(`"tokens":\d+`, `"tokens":"<tokens>"`)` beside the `standing_tokens` redaction (quote-prefixed, so `prompt_tokens` keys are untouched) and assert `context_cost.tokens > 0` semantically, as the standing_tokens assertion does. `turn1_prompt_tokens`/`turn1_cached_prompt_tokens` are non-omitempty, so all THREE goldens and the whole-line pin in `TestWriterEnvelopeOrderAndNulls` (`internal/eventjson/writer_test.go:102-106`) are refreshed. The text line follows the self-hiding rule `headlessUsageLine` applies (`Calls <= 0` ⇒ no line, `cmd/apogee/headless.go:1315-1318`): no `context cost:` line when `ContextCost.Rows` is empty (every `stubRunner`-driven test in `cmd/apogee/headless_test.go`), and the per-piece parenthesis lists only the rows present (a prompt-less run has no `prompt` column). `Turn1Usage` is filled from the event's own per-call fields (`ev.PromptTokens`, `ev.CachedPromptTokens`), never `ev.Cumulative`, so a predictive emergencyFold at Turn 0 cannot fold the summarizer's prompt into the Turn-1 figure.

**Files:** `internal/eventjson/writer.go`, `internal/eventjson/writer_test.go`, `internal/run/run.go`, `internal/run/run_test.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/e2e_eventlines_test.go`, `cmd/apogee/testdata/eventlines/run.jsonl`, `cmd/apogee/testdata/eventlines/not-started.jsonl`, `cmd/apogee/testdata/eventlines/not-started-after-sink.jsonl`, `docs/manual/headless.md`
**Read first:** cmd/apogee/headless.go — narrate, runFinishedFrame, contextFilesFrame, headlessUsageLines; cmd/apogee/wire_firing.go — raise, firingConfig; internal/run/run.go — Once, Result, eventTap.noteUsage; internal/eventjson/writer.go — RunStarted, RunFinished, envelope; internal/eventjson/writer_test.go — TestWriterEnvelopeOrderAndNulls; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden, eventLinesRedactions, eventLinesHome, headlessEventLines; cmd/apogee/headless_test.go — stubRunner, headlessRun; internal/run/run_test.go — planSpec, TestOnceReportsWhatTheFiringSpent

**Tests:** `internal/run`: `TestOnceReportsContextCostAndTurn1Usage` over stubllm with a scripted `Usage{Prompt: 1234, Cached: 100}` on Turn 1 and a different figure on Turn 2 — `Turn1Usage.PromptTokens == 1234`, unaffected by Turn 2 and by a Turn-0 maintenance call's cumulative figure; `ContextCost.Rows` non-empty. `cmd/apogee`: `TestHeadlessFormatJSONCarriesContextCost` asserts `run_finished.context_cost.tokens > 0` and `run_finished.turn1_prompt_tokens == 1234`, and that `run_started` carries no `context_cost` member; `TestE2EEventLinesGolden` gains the `"bytes"`/`"tokens"` redactions and the semantic `context_cost.tokens > 0` assertion; text-mode test pins the exact `context cost:` line spelling from `headlessUsageLines` on a Result with rows, and that a `stubRunner` Result with empty `Rows` prints no `context cost:` line; `TestWriterEnvelopeOrderAndNulls`' pinned `run_finished` line updated; `TestManualListsEveryEventLineKind` still green (no new kind); all three eventlines goldens updated.

**Acceptance:**
- `go build ./... && go vet ./internal/eventjson ./internal/run ./cmd/apogee`
- `go test ./internal/eventjson ./internal/run -count=1`
- `go test ./cmd/apogee -run 'TestHeadless|TestE2EEventLines|TestManualListsEveryEventLineKind|TestE2EUsage' -count=1`

**Commit:** `feat(headless): run_finished carries context_cost and the measured Turn-1 prompt tokens`

## 5. `apogee probe context` — the offline estimate table

**What:** Depends on item 2. Add `probe context` in `cmd/apogee/probecontext.go`, registered in `newProbeCommand()` beside `model`/`terminal`/`config`. Flags: `--workspace`, `--config`, `--endpoint`, `--model` (the same `config.Options` + `config.ApplyConfig` + `resolveRoots` pattern as `probeHostCommand`/`probeModelCommand`). Composition: the Config comes from `projectConfig(opts, roots, confiner, mode, skillProvider)` under the resolved startup mode (see the guard), a deny-all Approver and a no-op Confiner — reuse what `firingConfig` composes rather than a third assembly, through a shared helper extracted in `wire_firing.go` (see the guard: `firingConfig` itself is never called). Construct the Agent with the ordinary `provider.NewClient` bound to the configured endpoint (it does not dial at construction), call `ContextCost()`, `Close()` — never Step, so nothing is sent. Rendering lives in `internal/probe/contextcost.go`: `type ContextCost struct{ Estimate domain.ContextCost; Armed int; ... }` with `Report() string` producing:

```
Context cost — what apogee puts in front of the model at Turn 1 (mode auto; estimate, ~4.0 chars/token)
  prompt              1,043 B   ~261
  orientation           612 B   ~153
  context files       4,201 B  ~1050
  tool menu          11,512 B  ~2878
  total              17,368 B  ~4342
```

A trailing line `N advise/shape Reactions armed — their directives are measured with --live` when `len(cfg.Reactions)` arms any `ClassAdvise`/`ClassShape` Reaction; nothing when none. Plain text, `sanitize.StripEscapes`, `cmd.OutOrStdout()` — no JSON mode (matches every probe subcommand).

**Regression guard.** Tests stub the upstream the way the existing cmd/apogee e2e tests do (a stubllm HTTP server bound via `--endpoint`, never `stubllm.InProcess` from cmd/apogee); the offline probe constructs the Agent with the ordinary provider client bound to the configured endpoint and never Steps, so nothing is dialled — the test asserts the stub server saw zero requests; new .go files get their `doc.go` map line, listed in Files. Compose the estimate under the resolved startup mode (`opts.Mode`, or a `--mode` flag defaulting to it) — Plan filters the menu (`toolMenu` via `planOffers`, `internal/agent/loop.go`) and adds a Plan bullet to the orientation (`internal/agent/orientation.go`), so a Plan-composed table would not match a `headless --mode auto` run — and print the mode in the table header; for `--live` rely on the ctx cancel from the UsageEvent sink (item 6), not on Plan, to keep tools from running. `firingConfig` is never called: it dials the beat (`discoverBeat`), creates `~/.apogee/scratch/<id>` (`ensureScratchDir`) and may dial the sub-agents server (`resolveFiringRouting`). Extract the helper as `projectConfig` + `rebindSpecFor` + key resolve, stopping before the beat, the scratch mkdir and routing; set `ScratchDir` to the would-be path uncreated (a session's orientation names it); `provider.NewClient` does not dial at construction.

**Files:** `cmd/apogee/probecontext.go`, `cmd/apogee/probecontext_test.go`, `cmd/apogee/probe.go`, `cmd/apogee/doc.go`, `cmd/apogee/wire_firing.go`, `internal/probe/contextcost.go`, `internal/probe/contextcost_test.go`, `internal/probe/doc.go`
**Read first:** cmd/apogee/probe.go — newProbeCommand, probeHostCommand; cmd/apogee/probemodel.go — probeModelCommand; cmd/apogee/wire_firing.go — firingConfig, firingInputs, discoverBeat, ensureScratchDir; cmd/apogee/wire_config.go — projectConfig; internal/agent/loop.go — toolMenu, planOffers, standingSystem; internal/agent/orientation.go — orientationBlock; cmd/apogee/probe_test.go — TestSubcommandsRegistersProbe; internal/probe/doc.go — file map, cmd/apogee/doc.go — file map

**Tests:** `internal/probe`: `Report()` golden for a fixed `domain.ContextCost` (column alignment, `~` prefix, thousands separators, the armed line present/absent). `cmd/apogee`: `TestProbeContextPrintsTheEstimate` drives `apogee probe context --workspace <fixture> --endpoint <stub.URL>` (a `stubllm.New` HTTP server, the `eventLinesHome` pattern) through the cobra command and asserts the `prompt`, `tool menu` and `total` rows are present and the stub server saw zero requests; `TestProbeContextNamesArmedReactions` with a `reactions:` config; `TestProbeContextComposesUnderTheStartupMode` — the header names the mode, and `--mode plan` prints a smaller `tool menu` row than `auto`; `TestProbeContextNeitherDialsNorWrites` — after the command no scratch dir exists under the temp config home and the stub saw zero requests (no beat taken); `TestDocMapNamesEveryFile` green in `internal/probe` and `cmd/apogee`.

**Acceptance:**
- `go build ./... && go vet ./cmd/apogee ./internal/probe`
- `go test ./internal/probe -run TestContextCost -count=1`
- `go test ./cmd/apogee -run 'TestProbeContext|TestProbe' -count=1`
- `go test ./internal/probe ./cmd/apogee -run TestDocMapNamesEveryFile -count=1`

**Commit:** `feat(probe): apogee probe context prints the Turn-1 context-cost estimate per piece`

## 6. `apogee probe context --live` — the measured columns

**What:** Depends on items 4 and 5. Add `--live` to `probe context`: dials the configured endpoint exactly as `probeModelCommand` does (`provider.NewClient`, `--endpoint`/`--model` overlay), Submits the fixed prompt `Reply with the single word OK.` with `MaxTokens` capped at 8, Steps until the first Depth-0 `UsageEvent` (Turn 0), then cancels and Closes — a tool call the model might attempt is never executed because the sink cancels the ctx synchronously on that first UsageEvent, before any dispatch (see the guard; the deny-all Approver is belt-and-braces only). Report gains a `measured` column: `prompt_tokens` (and `cached` when non-zero) on the `total` row, and every estimate row re-rendered through the now-calibrated estimator (`Calibrated == true`), header `~N chars/token, calibrated`. When advise/shape Reactions are armed: the first Agent arms the `reactions:` sync lane exactly as `run.Once` does (see the guard); a second run on a fresh Agent with `Config.Bypass = true` (the Agent's `SetReactions`/`Generation` path, mirroring `liveSettings.setBypass`), producing a `bypass` measured column beside `as configured`; the delta line `Reactions add N tokens at Turn 1`. Without `--live` the behaviour of item 5 is unchanged byte-for-byte. Nothing is written (no fingerprint, no session).

**Regression guard.** Same stub-server pattern as 5 (`stubllm.New(t, script)` + `--endpoint stub.URL`, `stub.Requests()` / `stub.LastMessage` for the request count and last user message). The mechanism that keeps tools from running is stated, not Plan: Plan runs read-only tools without the Approver (`resolveLadder`, `internal/agent/resolution.go`, `planAdmits`) and permits scratch writes once `ScratchDir` is set; what stops dispatch is the ctx cancel landing before `respondAndReview`'s check (`internal/agent/loop.go:409`), which follows `Calibrate` + the usage Emit inside `streamResponse` (`:726-734`) — the sink cancels the ctx synchronously on the first Depth-0 UsageEvent, and the Turn is cancelled before any dispatch; the deny-all Approver stays as belt-and-braces only. The "as configured" run measures no Reaction delta unless the probe arms the `reactions:` sync lane itself: arm `domain.SplitLanes(opts.Reactions)`'s sync half on the first Agent exactly as `run.Once` does (`internal/run/run.go:298-311` — Generation read-edit-hand-back: `gen := a.Generation(); gen.Sync = sync; a.SetReactions(gen)`), and leave it unarmed under Bypass on the second.

**Files:** `cmd/apogee/probecontext.go`, `cmd/apogee/probecontext_test.go`, `internal/probe/contextcost.go`, `internal/probe/contextcost_test.go`
**Read first:** internal/agent/loop.go — streamResponse, respondAndReview; internal/agent/resolution.go — resolveLadder, planAdmits; internal/run/run.go — Once (sync-lane arming); cmd/apogee/wire_settings.go — liveSettings.setBypass, generationOf; internal/context/budget.go — TokenEstimator.Calibrate, CharsPerToken; internal/stubllm/log.go — Requests, LastMessage, AssertConsumed; cmd/apogee/e2e_eventlines_test.go — eventLinesHome; cmd/apogee/probemodel.go — probeModelCommand

**Tests:** `cmd/apogee`: `TestProbeContextLiveMeasuresTurn1` over a `stubllm.New` HTTP server bound via `--endpoint` (item 5's pattern) with `Usage{Prompt: 777}` — the report carries `777` on the `total` row and the stub saw exactly one request (`stub.Requests()`) whose last user message (`stub.LastMessage`) is `Reply with the single word OK.`; `TestProbeContextLiveSendsTwiceWhenReactionsArmed` — two requests, the first carrying the advise directive (the sync lane armed as `run.Once` arms it), the second with it absent, both columns printed; `TestProbeContextLiveNeverRunsATool` — a script whose Turn 1 answers with a tool call and `Usage` attached: exactly one request, the tool never executed (the ctx cancel lands before dispatch); `TestProbeContextLiveNeverWrites` — no session or fingerprint file appears under the temp config home. `internal/probe`: `Report()` golden for the measured single- and two-column shapes.

**Acceptance:**
- `go build ./... && go vet ./cmd/apogee ./internal/probe`
- `go test ./internal/probe -run TestContextCost -count=1`
- `go test ./cmd/apogee -run 'TestProbeContext' -count=1`

**Commit:** `feat(probe): --live measures the Turn-1 prompt tokens and the armed-Reactions delta`

## 7. Manual and index

**What:** Depends on items 5 and 6. `docs/manual/probe.md`: a `probe context` paragraph (the page has no `##` headings — one H1 and prose subjects) placed after the `probe config` block and before **When a frame comes out wrong** — what the table means, the `~` estimate vs measured distinction, `--live` costs one request (two when Reactions are armed) per ADR 0021, the sample output from item 5, and that the number is the same number a headless run in the same mode reports on `run_finished.context_cost`. `cmd/apogee/probe.go`: bump the `newProbeCommand` doc comment's subject count. `docs/manual/README.md`: the probe row mentions `context`. `docs/manual/configuration.md`: the `APOGEE_BYPASS` paragraph gains one sentence — on a stock install the Turn-1 request is identical with and without Bypass, and `apogee probe context --live` shows the delta once Reactions are armed. Any doc guard that pins the manual's probe subcommand list to `newProbeCommand()` (grep `cmd/apogee/docs_*_test.go` for `probe`) is updated in the same item.

**Regression guard.** The manual describes `run_finished.context_cost`, not `run_started`, and words the equivalence "the same number a headless run in the same mode reports on `run_finished.context_cost`" (item 5 composes under the resolved startup mode, so the mode is what makes the two agree). `docs/manual/probe.md` has no `##` headings (one H1, prose subjects): the `probe context` paragraph goes after the `probe config` block and before "**When a frame comes out wrong**"; the `newProbeCommand` doc comment's subject count (`cmd/apogee/probe.go`) is bumped.

**Files:** `docs/manual/probe.md`, `docs/manual/README.md`, `docs/manual/configuration.md`, `cmd/apogee/probe.go`, `cmd/apogee/docs_probe_test.go` (only if such a guard exists)
**Read first:** docs/manual/probe.md — probe config block, "When a frame comes out wrong"; docs/manual/README.md — probe row; docs/manual/configuration.md — APOGEE_BYPASS paragraph; cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; cmd/apogee/docs_settings_test.go — manualPage; cmd/apogee/probe.go — newProbeCommand

**Tests:** `go test ./cmd/apogee -run 'TestManual|TestDocs' -count=1` green.

**Acceptance:**
- `grep -n "probe context" docs/manual/probe.md docs/manual/README.md`
- `go test ./cmd/apogee -run 'TestManual|TestDocs' -count=1`

**Commit:** `docs(manual): probe context and the Turn-1 context cost`
