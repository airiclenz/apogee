# Engine architecture deepening — implementation plan

**Goal:** Land the engine-architecture review of 2026-08-20: fix all twelve defects it tripped over, consolidate the five hand-written hook runners behind one generic runner with revision-bearing tool-stage working values (candidate 1 + its ordering companion), cut the `internal/config` → `internal/tui` link (candidate 8), make config-key resolution registry-driven so Settings and Layer dissolve (candidate 2, resolution-and-projections scope), and fold the six config-write pipelines into one verified transaction (candidate 9).

- **Date:** 2026-08-20
- **Status:** not started
- **Sized for:** ~200k-context host

## Authoritative sources

- `docs/reviews/2026-08-20 - 00 - engine-architecture-review.html` — the review; candidates 1, 2, 8, 9 and the "Defects tripped over" list.
- ADR 0002 (tools open / mechanisms curated), ADR 0003 (constraint-declared mechanism registry, incl. the 2026-07-25 amendment), ADR 0010 (package layout, thin root facade), ADR 0035 (one key per deliberate edit), ADR 0043 (files split by concern; config package).
- Evidence verified in the working tree on 2026-08-20 (commit range around v0.15.8). Line numbers cited below are from that day; anchor on symbol names, not line numbers.
- Where the review's prose and an item's ratified text disagree, the item text wins; where item text disagrees with observed behavior contracts, preserve behavior and record a dated NOTES line under the item.

## Ratified design calls

1. **Scope** — candidates 1, 2, 8, 9 plus all twelve listed defects; every other candidate and all "Worth exploring"/"Speculative" entries stay out. (User, 2026-08-20.)
2. **UI vocabulary home** — SpinnerStyle (+parser), PreboundStart/PreboundReason, and the cursor-shape name vocabulary move to `internal/domain`, following ADR 0043's ParseMode precedent. No new package. (User, 2026-08-20.)
3. **Config registry scope** — registry rows gain fromFile/overlay/read/structure accessors; resolution and the display tables (`settingValues`, `settingTexts`, `settingStructures`) become loops over rows; `applySettingFor`, its `unreachable` twin, and tui's `settingsApplyLocal` stay as they are. (User, 2026-08-20.)
4. **Tool-stage working values** — new hook-facing edit wrappers (`ToolCallEdit`, `ToolResultEdit`) with bumping mutators and `Revision()`; the plain `ToolCall`/`ToolResult` structs stay unchanged everywhere else (provider, tools, processing untouched). (User, 2026-08-20.)
5. **Config.SessionsDir** — deleted, together with its two write sites. (User, 2026-08-20.)
6. **Autofix subprocess** — routed through the existing `internal/tools` funnel via a narrow exported entry point; `internal/mechanisms` already imports `internal/tools` in production code, no cycle. Full tools/Mechanisms spawner unification stays out of scope. (Plan author, 2026-08-20.)
7. **Ordered(at) freeze** — the per-hook-point orders are precomputed when the registry's `Validate*` gates run (both `newAgent` and rebind run them); `Ordered` returns the cached order, `Add`/`AddExperimental` clear the cache, and an unvalidated registry (bench callers) falls back to computing on the fly. Public API shape unchanged. (Plan author, 2026-08-20.)
8. **tui keeps aliases** — after the vocabulary move, `internal/tui` re-exports the moved types as type aliases so its ~99k lines of call sites do not change. (Plan author, 2026-08-20.)
9. **Client teardown ownership** — `provider` clients gain `Close` (connection teardown); an Agent closes only clients it owns (built at construction, rebind, or routed spawn); a child sharing the parent's client never closes it; `SwitchUpstream` closes the client it replaces. (Plan author, 2026-08-20.)

## Standing requirements

- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see the closing note).
- Consolidation items preserve each hook point's / writer's externally observable contract unless the item names the change.
- A fixed defect that has an `ISSUES.md` entry gets that entry removed in the same item (resolved items leave ISSUES.md; the CHANGELOG entry records the fix).
- A new file in a package that keeps a doc.go file map gets its map entry in the same item (ADR 0043; docmap tests enforce this).
- The ADR-0010 invariant holds throughout: nothing under `internal/` imports the root module path.

## Out of scope

- Review candidates 3 (write fence), 4 (Binding), 5 (Config projection), 6 (live session settings), 7 (tool vocabulary owner), 10 (probe fake terminal) — unchanged.
- All "Worth exploring" entries (execute preamble, validated.Resolve, presentation ladder, idle-only guard, hooks.go split, full subprocess-spawner unification, processing double switch, RetireConfiner) — unchanged.
- All "Speculative" entries — unchanged.
- `applySettingFor`, `settingsApplier.unreachable`, `settingsApplyLocal` and their drift guards — unchanged (ratified call 3).
- Any version bump or release act.

---

## 1. Delete the dead Config.SessionsDir field — ✅ DONE (2026-08-20)

NOTES (2026-08-20): the item named four files, but `SessionsDir` was also written by two more test sites the plan did not list (`cmd/apogee/schedule_test.go` x2, `benchreadiness_test.go` x4); they had to go or the packages would not compile.
NOTES (2026-08-20): prose corrected as the item allows — `internal/session/store.go` now says "the sessions root its caller resolves", `cmd/apogee/wire_live.go` and one `benchreadiness_test.go` comment say "sessions root" instead of naming the deleted field.

**Source:** review defect d; ratified call 5.

**What:** Remove `SessionsDir` from `domain.Config` (`internal/domain/config.go`, near line 109) and its only two writes (`cmd/apogee/headless.go` ~380, `cmd/apogee/wire_boot.go` ~166). The session store already takes its root as an explicit argument; nothing reads the field in production. Update `cmd/apogee/headless_test.go` (the one test read) and any doc comment that mentions the field (`internal/session/store.go`, `cmd/apogee/wire_live.go` mention it in prose only — correct the prose if it claims the field is used).

**Files:** internal/domain/config.go, cmd/apogee/headless.go, cmd/apogee/wire_boot.go, cmd/apogee/headless_test.go

**Tests:** existing suites; no new tests — the change is a deletion.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/domain/ ./cmd/apogee/`

**Commit:** `refactor(domain): drop the dead Config.SessionsDir field`

## 2. Correct the stale "not re-exported" claim in hooks.go — ✅ DONE (2026-08-20)

NOTES (2026-08-20): The item's text names `Defer` / `TakeDeferred` among the "named constructors" that are facade-absent. They are methods on `*Conversation` (hooks.go:861, :875), so the `apogee.Conversation` alias carries them — as it carries `Request.State` and `Conversation.Messages`. Writing them as facade-absent would have replaced one false claim with another, so the new comment names only the three package-level constructors as facade-absent and says explicitly that the method half of the seam travels with the aliases. Verified: `apogee.go` re-exports none of `NewRequest` / `NewResponse` / `NewConversation`.

**Source:** review defect e.

**What:** The comment at `internal/domain/hooks.go:22-27` claims Request/Response/Conversation are "deliberately NOT re-exported by the root facade" — false since `apogee.go` aliased them (`Request` :479, `Response` :485, `Conversation` :497; aliases carry the full method set). Rewrite the comment to state the truth: the types ARE reachable through the facade aliases; only the named constructors (`NewRequest`, `NewResponse`, `NewConversation`, `Defer`, `TakeDeferred`) are facade-absent. Do not change any code or alias — the hooks.go split is out of scope; this item makes the written claim honest.

**Files:** internal/domain/hooks.go

**Tests:** none (comment-only).

**Acceptance:** `go build ./... && go vet ./internal/domain/`

**Commit:** `docs(domain): correct the stale not-re-exported claim in hooks.go`

## 3. buildFrom clones descriptors like Descriptors() already does

**Source:** review defect j.

**What:** `internal/mechanisms/catalogue.go` `buildFrom` (return near line 208) hands out `r.descriptor` by shallow value, so `IncompatibleWith`/`Requires` slice headers alias the static catalogue map — while `Descriptors()` routes through `cloneDescriptor` and documents that callers cannot mutate the catalogue. Route `buildFrom` through `cloneDescriptor` too (and clone `Ordering`'s slices if they share backing the same way).

**Files:** internal/mechanisms/catalogue.go, internal/mechanisms/catalogue_test.go

**Tests:** one test that mutates a built `RegisteredMechanism`'s descriptor slices and asserts the catalogue's row is unchanged.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/mechanisms/`

**Commit:** `fix(mechanisms): clone catalogue descriptors in buildFrom`

## 4. Align the Tool-summary carrier counts and the run Events comment

**Source:** review defect l.

**What:** Ground truth: nine built-in tools attach a typed Summary (seven `okSummary` call sites; the three edit tools share `regions.go`'s). Fix the two wrong claims: `internal/tui/toolregistry.go` (~177, says "six", omits the three edit tools) lists all nine; `docs/adr/0002-...md` (~39, says "seven") gets a short dated amendment note stating the count grew to nine — do not rewrite the ADR's body (amendment style per ADR 0003's precedent). `internal/tools/doc.go`'s "NINE" is correct — leave it. Separately, `internal/run/run.go` (~28-29) claims `Spec.Config.Events` is "replaced" by Once, while the code and the package doc both say it is wrapped by `eventTap` (nil ⇒ discard); fix the struct-field comment to say wrapped, matching `doc.go` and the tap code.

**Files:** internal/tui/toolregistry.go, docs/adr/0002-tools-are-an-open-extension-point-mechanisms-are-curated.md, internal/run/run.go

**Tests:** none (doc-only).

**Acceptance:** `go build ./... && go vet ./internal/run/ ./internal/tui/`

**Commit:** `docs: align Tool-summary carrier counts and the run Events wrapping claim`

## 5. wave4WriteTools learns the three 2026-08-10 write tools

**Source:** review defect a.

**What:** Add `copy_file`, `move_file`, `delete_file` to the `wave4WriteTools` map in `internal/mechanisms/decompose.go` (~122). Per its own doc comment the map is the single source for `isFileMutatingTool` (robustness.go), `hasWrittenFiles`, `isGreenfieldContext`, and `detectReadLoopPaths` — the whole history family currently treats the three as non-writes.

**Files:** internal/mechanisms/decompose.go, internal/mechanisms/decompose_test.go, internal/mechanisms/robustness_test.go

**Tests:** extend the existing tables so the three tools count as file-mutating; add one drift pin: `internal/mechanisms` imports `internal/tools` in production code, so assert that every registered built-in whose execution mutates workspace files appears in `wave4WriteTools` (enumerate from the tools package's registered names; keep the foreign spellings additive).

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/mechanisms/`

**Commit:** `fix(mechanisms): count copy_file, move_file and delete_file as write tools`

## 6. Cover the decompose prose fallback and the readloop greenfield trigger

**Source:** review defect k. Files overlap item 5 — runs after it.

**What:** Two production branches have zero coverage: `decomposeExtractStep`'s no-numbered-steps path (`decompose.go` ~480-490: first-action-sentence extraction plus its two bail-outs) and `listResultEmpty` (`readloop.go` ~126-133, the only gate on `isGreenfieldContext`'s list-tool arm). Add tests only — no production change.

**Files:** internal/mechanisms/decompose_test.go, internal/mechanisms/readloop_test.go

**Tests:** a prose-only prompt table driving `decomposeExtractStep` through the sentence fallback and both bail-outs; a greenfield table where a `list_dir`-class call with an empty result keeps greenfield true and a non-empty one flips it.

**Acceptance:** `go test -race -count=1 ./internal/mechanisms/ && go test -cover -run 'Decompose|Greenfield|ReadLoop' ./internal/mechanisms/`

**Commit:** `test(mechanisms): cover the decompose prose fallback and readloop greenfield trigger`

## 7. The autofix formatter runs through the subprocess funnel

**Source:** review defect b; ratified call 6.

**What:** `internal/mechanisms/autofix.go` `runExternalFormatter` (~288) is the only `exec.Command*` call in the package and bypasses every funnel protection: it inherits `os.Environ()` (including `APOGEE_API_KEY`), has no process-group/Job-Object teardown, no output cap, no timeout clamp. Export one narrow entry from `internal/tools` wrapping the existing unexported `runSubprocess` (`exec_common.go` ~201) — keep the funnel itself unexported; the wrapper takes what autofix needs (ctx, argv, dir, timeout, stdin) and applies env scrub (`subprocessEnv`), `newProcessTeardown`, the confinement handoff failing closed on a nil Confiner, the output cap, and the timeout clamp. `runExternalFormatter` calls it and keeps only its own wording and stdin/stdout handling.

**Files:** internal/tools/exec_common.go, internal/mechanisms/autofix.go, internal/mechanisms/autofix_test.go, internal/tools/exec_common_test.go

**Tests:** a funnel-level test asserting the child env of the exported entry excludes `APOGEE_API_KEY`; an autofix test asserting the formatter path goes through the exported entry (e.g. env-scrub observable via a script that echoes the variable).

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tools/ ./internal/mechanisms/`

**Commit:** `fix(mechanisms): route the autofix formatter through the tools subprocess funnel`

## 8. Memoise the git filter-driver probe

**Source:** review defect c.

**What:** `runGit` (`internal/tools/git.go` ~166) calls `repoLocalFilterDrivers` unconditionally, spawning two probe subprocesses per call; one `git_commit` costs 12 git subprocesses. Memoise the probe per `(gitPath, root)` for the process lifetime (a `sync.Map` or equivalent). Document the accepted staleness: a filter driver added to the repo config mid-session is not re-probed until restart.

**Files:** internal/tools/git.go, internal/tools/git_test.go

**Tests:** a test that counts probe invocations across repeated `runGit` calls on one root (two roots stay independent).

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tools/`

**Commit:** `perf(tools): memoise the git filter-driver probe per repo root`

## 9. One ConfinementBox constructor

**Source:** review defect f.

**What:** The box is hand-assembled from the same `Config` fields at three sites: `internal/agent/construct.go` ~345 (two fields — deliberately no `NetworkAllow`, per its own comment), `internal/agent/dispatch.go` ~418 (`resolutionInput`) and ~458 (`hookExecutionCtx`) (all three fields). Give `domain.Config` one constructor for the full box (e.g. `func (c Config) ConfinementBox() ConfinementBox` beside the type in `internal/domain/confinement.go`); the two dispatch sites call it. The construct.go site calls it too and then explicitly clears `NetworkAllow`, keeping its deliberate divergence visible in one line next to its existing comment — a fourth future site that forgets a field can no longer create a silent confinement hole.

**Files:** internal/domain/confinement.go, internal/agent/construct.go, internal/agent/dispatch.go, internal/domain/confinement_test.go

**Tests:** a constructor test pinning all three fields; existing agent suites cover the call sites.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/domain/ ./internal/agent/`

**Commit:** `refactor(domain): build ConfinementBox through one constructor`

## 10. Agent.Close and SwitchUpstream tear down provider clients

**Source:** review defect g; ratified call 9.

**What:** `Agent.Close` (`internal/agent/agent.go` ~295) is a stale no-op whose comment predates real clients: `SwitchUpstream` (`internal/agent/rebind.go` ~313) overwrites `a.upstream` without teardown, and routed spawns (`internal/agent/subagent.go` ~274) build clients nothing closes. Give the provider client a `Close` method (tear down its transport's idle connections; `internal/provider` currently exposes none). Track ownership on the Agent: it owns a client it built (construction, rebind, routed spawn) and does not own one shared from a parent. `Agent.Close` closes the owned current client; `SwitchUpstream` closes the owned client it replaces. Update Close's doc comment to the new contract.

**Files:** internal/provider/client.go, internal/agent/agent.go, internal/agent/rebind.go, internal/agent/subagent.go, internal/agent/rebind_test.go, internal/agent/subagent_test.go

**Tests:** SwitchUpstream closes the replaced owned client; a child sharing the parent's client never closes it on Close; a routed-spawn child closes its own on Close.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/provider/ ./internal/agent/`

**Commit:** `fix(agent): close provider clients on Close and SwitchUpstream`

## 11. Drop the dead HistoryExceedsAllocation shim; one PromptChars spelling

**Source:** review defect h.

**What:** `internal/context/budget.go` exports `HistoryExceedsAllocation` (~183) with zero production callers — production uses the `domain.Budget` method directly (`internal/agent/compact.go` ~203). Delete the shim; move or retarget its tests at the domain method; fix `internal/context/doc.go`'s claim (~23) that it is the automatic trigger. In `internal/agent/loop.go`, both spellings of PromptChars appear (`apogeectx.PromptChars` ~627, `domain.PromptChars` ~888/890): unify on `domain.PromptChars`. Keep the `context.PromptChars` re-export only if other production callers exist; otherwise delete it and its "original home" comment.

**Files:** internal/context/budget.go, internal/context/budget_test.go, internal/context/doc.go, internal/agent/loop.go

**Tests:** existing budget tests retargeted; no new behavior.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/context/ ./internal/agent/`

**Commit:** `refactor(context): drop the unused HistoryExceedsAllocation shim and unify PromptChars`

## 12. Model-profile validation covers all four axes

**Source:** review defect i.

**What:** `validateModelProfiles` (`internal/config/config.go` ~1838) checks only `Thinking.Effort`; a typo'd `tool-call-format` sails through `toModelProfile`'s bare cast and surfaces as a failed Rebind at the first Beat, naming no config key. Validate all four axes at config load, each error naming its full key path in the style of the existing effort message (`model-profiles.<pattern>.<axis>`): `tool-call-format` against the domain's valid formats (`FormatNative`/`FormatMarkdownFenced`/`FormatCustomRegex`); `tool-call-pattern` must be present and compile as a regex when the format is custom-regex (and is refused with a named key when set under a format that ignores it, if that is the domain contract — check `domain.ToolCallFormat`'s docs and preserve the permissive case with a NOTES line if it is deliberate); `thinking.style` against the domain's style vocabulary; `thinking.effort` as today.

**Files:** internal/config/config.go, internal/config/config_test.go

**Tests:** a table over the four axes: one invalid value each ⇒ an error naming the key path; a fully valid profile passes.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/`

**Commit:** `fix(config): validate every model-profile axis at load`

## 13. Revision-bearing edit values for the tool-stage hooks

**Source:** review candidate 1 (first half); ratified call 4. Depends on item 3 only in spirit (same package tests) — no semantic dependency.

**What:** `ToolCall`/`ToolResult` (`internal/domain/tools.go` ~174-194) carry no revision counter, so the two tool-stage runners hand-roll acted probes (`callSnapshot`/`callChanged`, `toolResultChanged` — the DeepEqual probe that shipped the 558cd073 panic). Add hook-facing working values in `internal/domain` (new file, e.g. `tooledit.go`): `ToolCallEdit` and `ToolResultEdit` wrap pointers to the structs, expose read accessors plus bumping mutators for every hook-mutable field, and `Revision() int` — the same convention `Request`/`Response`/`Conversation` already follow. Change the two hook interfaces (`PreToolExecHook`, `PostToolResultHook` in `internal/domain/mechanism.go`) to take the edit values; update the two catalogue implementations (`internal/mechanisms/cachedcontent.go`, `errorenrich.go`), the test fixtures (`fivePointProbe` in `internal/agent/mechanism_dispatch_test.go`, `hookrun_test.go`), and any facade alias of the interfaces in `apogee.go`. In `hookrun.go`, the two tool-stage runners construct the wrappers and use `Revision()` for their acted probes — delete `callSnapshot`, `callChanged`, `toolResultChanged` and the HOTFIX comment (~315-318) whose promise this item fulfils. The plain structs stay unchanged for provider/tools/processing.

**Files:** internal/domain/tooledit.go, internal/domain/mechanism.go, internal/domain/tools.go, internal/agent/hookrun.go, internal/mechanisms/cachedcontent.go, internal/mechanisms/errorenrich.go, internal/agent/hookrun_test.go, internal/agent/mechanism_dispatch_test.go, apogee.go

**Tests:** keep `TestPostToolResultActedProbe` green on the new probe; add the same acted/no-op pin for pre-tool-exec; a `ToolResultEdit` mutator test covering a Summary-carrying result (the uncomparable case that panicked in production).

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/domain/ ./internal/agent/ ./internal/mechanisms/`

**Commit:** `refactor(domain): revision-bearing edit values for the tool-stage hooks`

## 14. One generic hook runner with five adapters

**Source:** review candidate 1 (second half). Depends on item 13.

**What:** Collapse the ten loops in `internal/agent/hookrun.go` (five runners × catalogued + experimental) into one generic `runHooks` implementing the shared protocol — ordered → bypass gate → revision bracket → fire (recover boundary) → book — with five thin per-point adapters. Preserve every asymmetry as adapter behavior, explicitly: post-response alone installs the subprocess-permit ctx (`hookExecutionCtx`), short-circuits on ActionRetry and routes ActionDefer; post-tool-result returns no error and contains panics without propagating; history-rewrite builds no LoopView while the tool stages build one per runner call; experimental fires are always booked, catalogued fires only when the acted probe trips. Call sites in `loop.go`/`dispatch.go` keep their signatures.

**Files:** internal/agent/hookrun.go, internal/agent/hookrun_test.go

**Tests:** the 25-cell table the review calls for — every hook point × {catalogued acts ⇒ booked, catalogued no-op ⇒ not booked, experimental ⇒ always booked, panicking hook ⇒ contained per that point's contract, bypass ⇒ skipped} — replacing the current 2-of-5 acted-probe coverage. Keep the existing per-point behavior suites (permit, synthesis, retry) green.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/`

**Commit:** `refactor(agent): one generic hook runner behind five adapters`

## 15. Precompute the per-hook-point mechanism order

**Source:** review candidate 1 companion; ratified call 7. Depends on item 13 (shared file `internal/domain/mechanism.go`).

**What:** `Ordered(at)` (`internal/domain/mechanism.go` ~351) re-runs filtering plus `topoSort` on every call — ~23 sorts per Turn with a full catalogue — although the registry is declared read-only once the engine has it (mechanism.go ~206-213). Precompute the order for all five hook points when the `Validate*` gates run (both `newAgent` and rebind's fresh-registry path run them); `Ordered` returns the cached slice (return a clone, or document the returned slice as read-only — match the package's existing convention); `Add`/`AddExperimental` clear the cache; an unvalidated registry (bench/public callers via the `apogee.MechanismRegistry` alias) computes on the fly exactly as today. No public API change.

**Files:** internal/domain/mechanism.go, internal/domain/registry.go, internal/domain/registry_ordered_test.go, internal/domain/mechanism_test.go

**Tests:** cached and uncached paths return identical orders (reuse `TestOrdered_DeterministicUnderShuffle`'s fixtures); Add after validation invalidates; existing ordering suites stay green.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/domain/ ./internal/agent/`

**Commit:** `perf(domain): precompute per-hook-point mechanism order at validation`

## 16. UI vocabulary moves into internal/domain

**Source:** review candidate 8 (first half); ratified calls 2, 8.

**What:** Move into `internal/domain` (new file, e.g. `uivocab.go`): `SpinnerStyle` with its three constants, name list, default, and `ParseSpinnerStyle` (from `internal/tui/spinner.go`); `PreboundReason` with its three constants and the `PreboundStart` struct (from `internal/tui/tui.go` ~1179-1203); and the cursor-shape NAME vocabulary — the canonical name list, a `ValidCursorShapeName`-style validator, and the default name (from `internal/tui/prompteditor.go` ~119-129). The tea-typed mapping stays in tui: `ParseCursorShape` keeps returning `tea.CursorShape` but derives its accepted names from the domain list (domain must not import bubbletea — it stays a leaf). tui re-exports the moved types as aliases (`type SpinnerStyle = domain.SpinnerStyle`, etc.) so no other tui call site changes. `internal/config` is not touched in this item.

**Files:** internal/domain/uivocab.go, internal/domain/doc.go, internal/tui/spinner.go, internal/tui/tui.go, internal/tui/prompteditor.go, internal/domain/uivocab_test.go

**Tests:** parser/validator round-trip tables in domain; tui suites stay green through the aliases.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/domain/ ./internal/tui/ && go list -deps ./internal/domain | grep -q bubbletea && exit 1 || true`

**Commit:** `refactor(domain): home the spinner, prebound and cursor-shape vocabulary`

## 17. internal/config stops importing internal/tui

**Source:** review candidate 8 (second half). Depends on item 16.

**What:** Retarget the three import sites — `internal/config/registry.go` (~16), `config.go` (~24), `options.go` (~11) — from `internal/tui` to the domain vocabulary: `UISettings.Spinner`/`Options.Prebound` field types, `ParseSpinnerStyle` calls, the `Prebound*` constants, and the two registry validators (`validateSpinnerName`, `validateCursorShapeName` — the latter uses the domain name validator instead of discarding `tui.ParseCursorShape`'s result). Update `cmd/apogee` accordingly (`wire_options.go` keeps calling `tui.ParseCursorShape` — the binary may import tui) and retarget `TestRegistryEnumValuesMatchParseSites` in `internal/config/registry_test.go` at the domain parsers. Add a dated amendment note to ADR 0043: the sibling import was legal but defeated the ADR's stated motivation; config now reaches the vocabulary through domain. Acceptance pins the decoupling.

**Files:** internal/config/registry.go, internal/config/config.go, internal/config/options.go, internal/config/registry_test.go, internal/config/config_test.go, cmd/apogee/wire_options.go, docs/adr/0043-files-split-by-concern-and-config-gets-a-package.md

**Tests:** existing config suites; the retargeted parse-site pin.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/ ./cmd/apogee/ && go list -deps ./internal/config | grep -q 'internal/tui' && exit 1 || true`

**Commit:** `refactor(config): reach UI vocabulary through domain, not tui`

## 18. Every registry row carries fromFile and overlay accessors

**Source:** review candidate 2; ratified call 3. Depends on item 17 (same files).

**What:** Today `multiSourceKey` (`internal/config/config.go` ~648) gives 3 of 38 keys accessor-driven resolution; the other 35 ride the hand-written copy chain. Extend the accessor table to all 38 registry rows: each row gains `fromFile` (project the typed value plus presence out of `fileConfig`) and `overlay` (apply a present value onto `Settings`) closures, written against the CURRENT Layer/Settings shapes — the existing 3 rows fold into the same table, `fromEnv`/`fromFlag` stay on the rows that have an EnvVar/FlagName. No resolution code changes yet; this item is the table plus its completeness guard.

**Files:** internal/config/config.go, internal/config/registry.go, internal/config/config_test.go, internal/config/registry_test.go

**Tests:** a completeness guard: every `KeyRegistry` row has both accessors (extend `TestRegistryIsBijectionWithFileConfig`'s reflection walk or add a sibling); spot tables proving accessor output matches the hand-written chain for representative kinds (bool, string-list, structured, enum).

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/`

**Commit:** `refactor(config): give every registry row fromFile and overlay accessors`

## 19. Resolution becomes a loop over registry rows

**Source:** review candidate 2. Depends on item 18.

**What:** Rewrite `fileConfig.layer()` (~1896-1991), `ResolveSettings`'s hand-written overlay block (~747-796), and `ApplyConfig`'s flat copy block (~2344-2379) as loops over the row accessors from item 18; delete the per-field hand-written copies. Behavior is identical — the ~40 existing per-key tests in `config_test.go` are the net. `Settings` and `Layer` still exist as carrier types after this item; validation in `ApplyConfig` (Present, UI, cursor-shape, SystemPrompt, ContextFiles, Servers, model profiles) stays where it is.

**Files:** internal/config/config.go, internal/config/config_test.go

**Tests:** existing suites unchanged and green — that is the point; add none unless a gap is exposed.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/ ./cmd/apogee/`

**Commit:** `refactor(config): resolve every key through the registry accessor loop`

## 20. Settings and Layer dissolve into Options

**Source:** review candidate 2. Depends on item 19.

**What:** With resolution loop-driven, delete the `Settings` (~42-213) and `Layer` (~502-627) carrier structs: the overlay accessors retarget `Options` directly, precedence stays defaults → file → env → flag, and presence keeps riding `fileConfig`'s pointer/zero-guard semantics (the accessor signature change from item 18's interim shape is expected). `ResolveSettings` becomes (or is replaced by) a resolve-to-Options entry; update every consumer of the deleted types (`internal/config`'s own files — `keyresolve.go`, `configwatch.go`, `defaults.go` as applicable — and `cmd/apogee` wire files). The nested `UISettings`/`PresentSettings` groupings survive only if `Options` already carries them; otherwise their fields land flat as `ApplyConfig` mapped them. Expect a deletions-heavy diff (~335 lines of carrier code go away).

**Files:** internal/config/config.go, internal/config/options.go, internal/config/keyresolve.go, internal/config/configwatch.go, internal/config/config_test.go, internal/config/keyresolve_test.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_settings.go

**Tests:** existing suites retargeted at the Options-returning entry; the multi-source end-to-end test (`TestResolveSettingsMultiSourceKeysReadTheRegistry`) keeps asserting env/flag precedence.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/ ./cmd/apogee/`

**Commit:** `refactor(config): dissolve Settings and Layer into Options`

## 21. The settings display tables become registry projections

**Source:** review candidate 2; ratified call 3. Depends on item 20.

**What:** Give each registry row `read func(Options) string` and (for the seven structured rows) `structure func(Options) any` accessors; `settingValues` (38 closures) and `settingTexts` in `cmd/apogee/settingsrows.go` and `settingStructures` (7 closures) in `cmd/apogee/settingsedit.go` become loops over the rows. Their per-key drift guards (`TestSettingValuesCoverEveryRegistryKey`, `TestSettingTextsCoverEveryTextKey`, `TestSettingStructuresCoverEveryStructuredKey`) become structurally unnecessary — replace them with one completeness guard on the rows (every row has `read`; every structured row has `structure`). `applySettingFor`, `unreachable`, and `settingsApplyLocal` stay untouched (ratified call 3), as do their guards.

**Files:** internal/config/registry.go, internal/config/registry_test.go, cmd/apogee/settingsrows.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsrows_test.go, cmd/apogee/settingsedit_test.go

**Tests:** the row completeness guard; `TestSettingsRowsFormatEffectiveValues`'s per-key expectations stay as the formatting net.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/ ./cmd/apogee/`

**Commit:** `refactor(config): project the settings display tables from registry rows`

## 22. One config-write transaction; the scalar and mechanism writers adopt it

**Source:** review candidate 9. Independent of items 18-21 (disjoint files).

**What:** Behind each of the six config-write pipelines sits the same ~40-line transaction: validate → `ReadConfigForWrite` (read/seed) → locate → splice → re-parse → shape predicate + `sameApartFrom` → `writeConfigAtomically`. Introduce one transaction function in a new `internal/config/configedit.go` — e.g. `edit(path string, splice func(...) error, verify func(before, after fileConfig, raw []byte) error) error` — owning read, seed, parse, splice, re-parse, verification ordering, and the atomic mode-preserving write. Provide the per-container-shape `changedOnlyAt` predicates (scalar path, list item, map key) beside it. Convert the scalar writer (`SaveConfigSetting`/`ResetConfigSetting`, gate `verifiedSplice`) and the mechanism writer (`SaveMechanismSetting`, gate `verifiedMechanismSplice`) to locate + splice + verify triples over the transaction; their template-citing comments (e.g. "It is verifiedEntrySplice's shape…") go away with the duplication.

**Files:** internal/config/configedit.go, internal/config/configwrite_scalar.go, internal/config/configwrite_mechanism.go, internal/config/doc.go, internal/config/configwrite_scalar_test.go, internal/config/configwrite_mechanism_test.go

**Tests:** both writers' existing suites green unchanged — the conversion is behavior-preserving.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/`

**Commit:** `refactor(config): one verified config-write transaction; scalar and mechanism writers adopt it`

## 23. The entry, host-acknowledgement and legacy-fold writers adopt the transaction

**Source:** review candidate 9. Depends on item 22.

**What:** Convert the remaining four pipelines to the item-22 transaction: the key-source entry writers (`saveEntryEdit`, gate `verifiedEntrySplice` — keep its extra `ValidateServers` call as that writer's verify step), the server-entry setting writer (`SaveServerEntrySetting`, which borrows `verifiedEntrySplice` with a caller noun), the host-acknowledgement writer (`saveHostAcknowledgement`, whose verify is inline around `hostsAppended`), and the legacy fold (`foldLegacyKeys`, gate `verifyFold` — keep `backUpConfig` as that caller's pre-step). Each keeps only its locate + splice + verify triple and its own wording.

**Files:** internal/config/configwrite.go, internal/config/configwrite_keysource.go, internal/config/configmigrate.go, internal/config/configwrite_test.go, internal/config/configwrite_keysource_test.go, internal/config/configwrite_server_test.go, internal/config/configmigrate_test.go

**Tests:** all four writers' existing suites green unchanged.

**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/`

**Commit:** `refactor(config): entry, host-ack and fold writers ride the write transaction`

## 24. One property suite for the write transaction

**Source:** review candidate 9. Depends on items 22 and 23.

**What:** The pipeline properties — atomic write, file-mode preservation, re-parse refusal (a splice that breaks the YAML never lands), out-of-scope-change refusal (`sameApartFrom`) — are currently asserted per writer across ~2,957 test lines. Write one property suite against `edit` itself in a new `configedit_test.go`, then delete the per-writer duplicates of exactly those pipeline properties from the six writer test files. Keep every writer-specific behavior test (locate errors, rendering, noun wording, validation) — only the duplicated transaction properties go.

**Files:** internal/config/configedit_test.go, internal/config/configwrite_scalar_test.go, internal/config/configwrite_mechanism_test.go, internal/config/configwrite_keysource_test.go, internal/config/configwrite_test.go, internal/config/configmigrate_test.go

**Tests:** this item is tests; net coverage of the transaction must not drop (compare `go test -cover ./internal/config/` before/after in the verify step).

**Acceptance:** `go test -race -count=1 ./internal/config/ && go test -cover ./internal/config/`

**Commit:** `test(config): one property suite for the config-write transaction`

---

## Suggested version bump

Minor (v0.16.0): a wave of engine-internal consolidation plus twelve defect fixes, including two live behavioral fixes (write-tool classification, subprocess env scrub) — no breaking user-facing changes. Whether and when to bump is the user's call; no item in this plan changes a version identifier.
