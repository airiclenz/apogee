# Mechanism retirement wave — the floor guards become engine behaviour, the nudge catalogue retires

**Goal:** the six catalogued Mechanisms that every model benefits from — `tool_use_enforcer`,
`empty_response_recovery`, `validate`, `tool_loop_interceptor`, `tool_result_cap`,
`cached_content_intercept` — become **Floor guards**: plain engine behaviour in a new `internal/floor`
policy package, on under Bypass, each behind one file-only config key. The other fourteen rows retire
through the retired roll (the `grammar` precedent) and their source files are deleted. The hook API,
registry, `mechanisms:` key, `/settings` row and `--bypass` stay as the bench's lab surface; the shipped
catalogue and the gemma Validated set end empty.

**Date:** 2026-09-02 · **Status:** unexecuted · **Base:** `8c8d66bf` ·
**sized for:** ~200k-context host

**Sources**
- `docs/handoffs/2026-09-02 - 00 - harness-over-mechanisms-parked-items.md` (parked item 1)
- `docs/adr/0006-…`, `0009-…`, `0014-…`, `0015-…`, `0016-…` (2026-08-29 amendment), `0070-…` (Option C)
- `docs/design/mechanism-catalogue.md` (Table A/B, the `cached_content_intercept` evidence row `:191`)
- `internal/mechanisms/retired.go` (the roll), `internal/agent/prune.go` + `internal/context` (the policy/wiring split to copy)
- `docs/plans/archived/2026-09-02 - 00 - structural-feedback-and-pruning-plan.md` (item 7: the `prune-tool-results` key's full path)

**Ratified design calls** (owner, 2026-09-02)
- **Scope:** parked item 1 only; items 2, 3, 5 of the handoff stay parked; item 4 is plan `2026-09-02 - 07`.
- **Dispositions:** PROMOTE the six above; RETIRE `stall_nudge`, `list_nudge`, `tool_use_directive`, `decompose`, `guided_decomposition`, `filehint`, `read_loop`, `read_repeat`, `truncate_history`, `library`, `toolfilter`, `error_enrichment`, `syntax`, `autofix`.
- **Promotion shape:** engine code plus one structural config key each; no catalogue row, ID, descriptor or strikes.
- **Machinery:** hook API, registry, `EnableMechanisms`, `mechanisms:`, `/settings` row, `--bypass` STAY as the lab surface.
- **Validated sets:** the gemma entry retires (empty shipped roster); the `validated` package and its surfaces stay.
- **Term:** **Floor guard**; the six are tool-use enforcer, empty-response recovery, tool-call repair, tool-loop breaker, tool-result cap, read cache.
- **Config keys:** six top-level file-only bools, default `true`: `tool-use-enforcer`, `empty-response-recovery`, `tool-call-repair`, `tool-loop-breaker`, `tool-result-cap`, `read-cache`. An old `mechanisms:` entry naming a promoted ID earns a notice naming the new key; no silent mapping.
- **Package:** pure policy in `internal/floor`; thin call sites in `internal/agent`.

**Derived calls** (writer, 2026-09-02)
- `read_repeat` retires as the twin of the promoted read cache (mutually `IncompatibleWith`; only the intercept has evidence).
- `domain.FloorConfig` holds six `Disable…` bools: the ZERO value keeps every guard on, so an embedder's bare `Config` keeps the floor (ADR 0070's empty-list semantics survive).
- The roll becomes per-ID `{ID, Release, Successor}`; `Successor` is the config key for a promoted ID. Release string for this wave: `v0.20.0` (see the closing note).
- Guards run BEFORE the lab hooks at each seam. Post-response order: tool-loop breaker → tool-call repair → empty-response recovery → tool-use enforcer; the first retry wins and shares `maxPostResponseRetries`.
- Guard firings are `domain.FloorGuardEvent`s whose `Guard` field is the config key; `MechanismFiredEvent` stays for lab hooks.
- Retired rows are DELETED with their tests and assets (`grammar` precedent). Behaviour moves verbatim: no guard's decision logic is redesigned in this plan.
- `~/.apogee/library` on disk is never touched; `internal/library`'s fingerprint and probe-record halves stay (they serve Validated sets and `probe model`).

**Standing requirements:** `skills: coding-standards`.

**Regression check (2026-09-02, 8c8d66bf):** five independent reviewers read every item against the
tree. Plan-wide, folded into items 4-8 and 12-17: before deleting a file, grep its package-level
identifiers for surviving callers (`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each
still-called identifier into a surviving file in the same commit; a symbol grep, never a closed file
list, defines the item's fix-up scope. `internal/mechanisms/historyhints_test.go` and
`writedetection_test.go` are edited by whichever item deletes the row a case names; the FINAL shape of
both (and of `historyhints.go`) is item 17's. Per item:
- 1: guard folded (ADR 0071 decision (1) carries the no-strikes-suppression statement).
- 2: guard folded (`RetiredRelease` call sites; the `shipped_test.go` drift tripwire relaxes here).
- 3: guard folded (`toolSet` copied; only the portable off-ramp test case moves; `Config.Floor` is top-level).
- 4: guard folded (moved range corrected, `toolNames` rehomed, symbol rule replaces the file list, `retired_test.go` exemplar synthetic).
- 5: guard folded (`readToolNames` rehomed, symbol rule for the two IDs, move ranges corrected).
- 6: guard folded (`OffRampFloor` symbol rule; the pinned `mechanisms` Desc string).
- 7: guard folded (surviving `IncompatibleWith` edges; the test pins arguments; the fan-out seam is a no-op for reads).
- 8: guard folded (`toolResultCapID` spelled literally, agent tests recast, `tool_result_cap` symbol rule incl. `compact.go:226-232`).
- 9: guard folded (the event needs a `Hook` or a hookless string; `internal/run` replaces daemon/inspector; root alias added).
- 10: guard folded (`wantDefaults()` joins the golden list; the `cmd/apogee` split is stated out loud).
- 11: guard folded (the six rows reach the holder too; `FloorConfig` gains a root alias) — yields to ADR 0031's engine-sufficiency invariant.
- 12: guard folded (`listSpellings`/`hasWrittenFiles` rehomed, `decomposeID` edges dropped, the write-tool pin survives; `compact.go:275` is item 13's).
- 13: guard folded (`gd*` helpers rehomed, the clone contract stays in `internal/mechanisms`, the prose sites listed).
- 14: guard folded (`firstUserContent` moves to item 15, `make lint` replaces `go vet`, one `readlistfamilies_test.go` case survives to item 17).
- 15: guard folded (`historyhints_test.go` joins Files; the `truncate_history` rule is scoped).
- 16: guard folded (`construct_test.go` joins Files; `TestHostToolsCarriesSecretEnvVars` stays).
- 17: guard folded (`dirPerm`/`filePerm` rehomed, `LibraryDir` symbol rule, `benchreadiness_test.go:74` drops `library` here; Depends gains item 4).
- 18: recast (a retired-entry key list keeps a config carrying the offered alias starting) — upholds ADR 0016's 2026-08-29 amendment.
- 19: guard folded (both zero-length bails relax here; `./internal/agent/` joins Acceptance).
- 20: guard folded (the ADR link repoint is in scope, README/ISSUES leave Files, the rule excludes `plans/` and reaches the code tree).
- 21: guard folded (`:1396-1416` corrected, **History truncation** named, term-level alternatives, the three live Library sentences).
- 22: guard folded (greps narrowed off `~/Library` and bolded `**21**`, the syntax-trailer sentence stays, `ManualLists` joins Acceptance).
- 23: guard folded (the `Request.InjectContext` entry joins the REWORD list; the guided-decomposition coverage clause goes).

**Regression check — second pass (2026-09-03, 8c8d66bf):** a further independent reviewer re-read the
amended item 18 against the tree. The regression it found — the retired-key check running BEFORE the
entry lookup, which would have cost a user's own `gemma-4-e4b-it-qat` entry its start — was put to the
owner, and the guards below were ratified (owner, 2026-09-03, via question). Every other item stands as
the first pass left it.
- 18: guard folded (the key check moves after the entry lookup, so a live user-dir entry under a
  retired key still applies; all four `TestResolveValidatedSet_*` gemma fixtures, `wire_server_test.go`'s
  two rebind tests and `wire_live_test.go:321-330` recast over a synthetic user-dir entry; the first-run
  `config.yaml` alias examples de-name the shipped key; Acceptance widens to `-run 'Validated|Probe|Rebind'`;
  the `shipped_test.go` rewrite preserves item 2's drift tripwire) — still upholds ADR 0016's 2026-08-29
  amendment.

**Out of scope:** removing the hook API, registry, `mechanisms:`, `--bypass` or the `validated` surface · handoff items 2, 3, 5 · the system-message order sentences plan `2026-09-02 - 07` owns (`CONTEXT.md:778`, `:1192`, `docs/manual/configuration.md`'s order sentence) · rewriting ADRs 0006/0009/0014/0015/0016/0070 (records; ADR 0071 states what supersedes them — a relative-link repoint is not a rewrite and is IN scope, item 20) · deleting user data · a version bump.

---

## 1. ADR 0071 — Floor guards are engine behaviour; the nudge catalogue retires — ✅ DONE (2026-09-03)

NOTES (2026-09-03): retry — the previous attempt left the ADR untracked in the tree; kept it and applied the DECISION to the Context section's time-framing clause ("Four years of catalogue and two years of ports later" → "Two months of catalogue and four port waves later", matching the catalogue's 2026-07-04 ratification).

**What.** Write `docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md`
(`Status: accepted`, `Supersedes: ADR 0009 for structural behaviour; ADR 0070 Option C`,
`Amends: ADR 0006, ADR 0014, ADR 0015 D1, ADR 0016 (2026-08-29 amendment)`). Decisions, one section
each: (1) the Floor-guard test — "changes only what the model sees after its own failure or shapes the
request without steering it; needs no per-model proof; cannot regress Bypass" — and the six that pass
it; (2) the per-Mechanism A/B gate (ADR 0009) binds only lab rows; (3) a row may retire on a ratified
verdict, not only when inert (supersedes ADR 0016's "inert by construction"); the gemma set's evidence
is void without its members, so the entry retires; (4) the machinery stays as ADR 0001's lab surface,
the catalogue is frozen (no ports, no new rows), Bypass now switches off lab rows only; (5) the six
config keys and the zero-value-on `FloorConfig`; (6) rejected: keep a `structural` Capability; delete
the layer; engine code without keys. Cite the handoff's evidence paragraph and `catalogue.md:191`.

**Regression guard.** ADR 0071 decision (1) states that Floor guards carry NO strikes-3 suppression
and no Turn-Budget throttle — the per-Turn `maxPostResponseRetries` bound is the only limiter —
superseding the SuppressStrikesThree posture the two repair rows had (`selfreg.go:239-250`).

**Files:** `docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md`

**Tests.** None (docs-only).

**Acceptance.** `test -f docs/adr/0071-*.md && grep -c "^## " docs/adr/0071-*.md` ≥ 6;
`grep -n "Supersedes\|Amends" docs/adr/0071-*.md` shows both lines.

**Commit:** `docs(adr): ADR 0071 — floor guards are engine behaviour, the nudge catalogue retires`

## 2. The retired roll carries a release and a successor per ID — ✅ DONE (2026-09-03)

NOTES (2026-09-03): consequential edit — internal/mechanisms/doc.go: made necessary by the new promoted-row notice, whose file-map half-line said ResolveEnabled hands back notices only for rows "the block still turns on".

NOTES (2026-09-03): the retired-and-false notice is worded exactly as the plan spells it, including the literal quotes around `"<id>: false"`; `retired_test.go` gains a `withRoll` fixture (sequential, package-global swap, captureStderr precedent) so the wording is pinned over a synthetic promoted row rather than over whichever IDs are rolled today.

NOTES (2026-09-03): `cmd/apogee/validatedsets_test.go` gains one assertion pinning grammar's own `v0.18.7` in the shed notice — the per-ID lookup's only user-visible proof at this item.

**What.** `internal/mechanisms/retired.go`: replace `retiredIDs []MechanismID` + `const RetiredRelease`
(`:25`, `:43`) with `var retired = []retiredRow{{ID: "grammar", Release: "v0.18.7"}}` where
`retiredRow{ID domain.MechanismID; Release, Successor string}`. Keep `RetiredIDs()`/`IsRetired()`;
add `RetiredRelease(id) string` and `Successor(id) string`. `ResolveEnabled` notices (`:154-158`):
unchanged text for a plain retired ID set `true`; for an ID with a `Successor` emit
`apogee: mechanism %q is the %q floor guard since %s and is on by default; remove it from mechanisms:`
when set `true`, and — new: today retired-and-`false` is silent —
`apogee: mechanism %q is the %q floor guard since %s; "%s: false" under mechanisms: no longer turns it off — set %s: false at the top level`
when set `false`. Callers of the const: `cmd/apogee/validatedsets.go:204-208` → `RetiredRelease(id)`.
Update `retired.go:11-24`'s joining rule to "a ratified verdict (ADR 0071) or inert by construction".

**Regression guard.** `cmd/apogee/wire_helpers_test.go:29` reads the `mechanisms.RetiredRelease`
CONST (feeding `daemon_test.go:416`, `wire_live_test.go:304`, `headless_test.go:471`): point its
helper at `mechanisms.RetiredRelease(domain.MechanismID(id))` — rule: `grep -rn "RetiredRelease"
internal/ cmd/` reads only call sites. This item also relaxes the drift tripwire
`internal/validated/shipped_test.go:15-37` to run `DropRetired(e, mechanisms.RetiredIDs())` before
`Validate`, so a row removed WITHOUT a roll entry still fails there while a rolled ID passes; add the
file to Files and a test asserting an un-rolled removal still trips it.

**Files:** `internal/mechanisms/retired.go`, `internal/mechanisms/retired_test.go`, `cmd/apogee/validatedsets.go`, `cmd/apogee/validatedsets_test.go`, `cmd/apogee/wire_helpers_test.go`, `internal/validated/shipped_test.go`

**Tests.** `retired_test.go`: a roll fixture with a `Successor` row drives `ResolveEnabled` with `true`
and with `false` and asserts both exact notice strings; `grammar` keeps its `v0.18.7` text.
`shipped_test.go`: a shipped entry naming a ROLLED id validates, one naming an id removed with no roll
entry still fails `TestShipped_PinnedAgainstCatalogue`.

**Acceptance.** `go test ./internal/mechanisms/ -run 'Retired|ResolveEnabled' && go test ./internal/validated/ && go test ./cmd/apogee/ -run 'Validated'`

**Commit:** `refactor(mechanisms): retired roll carries a release and a successor per ID`

## 3. `internal/floor` — the shared substrate and `domain.FloorConfig` — ✅ DONE (2026-09-03)

NOTES (2026-09-03): the copied helpers are re-homed by concern rather than under their source file names — `offramps.go` splits into `toolnames.go` (the spelling families, `toolSet`, `isReadTool`, `isFileMutatingTool`) and `conversation.go` (the history scans), `robustness.go`'s three correction helpers become `correction.go`, and `historyhints.go`'s five become `readerror.go`. `offramps.go` was not reused as a name because item 6's rule requires `off-ramp|OffRamp` to read zero across `internal/`; `intent.go`'s header comment lost its catalogue-row/off-ramp framing for the same reason. Content is verbatim apart from those comment rewordings.

NOTES (2026-09-03): the item's copy list left most of the copied helpers with no caller at all in the new package, which golangci-lint's `unused` (`default: standard`) reports as dead code until items 4/5/7/8 wire them — so the copied tests (`TestToolCallPath`, `intent_test.go`, `TestResultIsReadErrorPrefersTheCommittedMarker`) are joined by coverage for the rest: the narration scans, `wroteRecently`'s window, `hasRecentProgress`, `normalizePath`, the anchored signal match, `buildCorrectionMessage`/`hasIssues`, `toolSet`, and every embedded prompt asset. `make lint` is clean.

NOTES (2026-09-03): `robustness_test.go`'s `TestWriteDetectionSemanticsSplitOnApogeeWriteTools` asserts over `isWriteTool` too, which is NOT copied (it is the narrower content-repair predicate belonging to the lab rows), so only its `isFileMutatingTool` half moved, folded into `TestToolNamePredicates` beside `isReadTool`.

NOTES (2026-09-03): the floor tests import `internal/domain/domaintest`, domain's own test adapter under `internal/domain/` — the one-deep-module rule's "only internal/domain and internal/context" is unbroken in non-test code, which imports `internal/domain` alone (`internal/context` is not needed until item 8).

**What.** New package `internal/floor` (with `doc.go` + `docmap_test.go`, the `internal/agent`
convention). COPY — the originals stay until their retire items delete them — the helpers the six
guards need: from `offramps.go` the whole file (`isReadTool`, `toolCallPath`, `assistantMessageCount`,
`wroteRecently`, `previousAssistantWasTextOnly`, `hasEverUsedTools`, `hasRecentProgress`); from
`robustness.go` `robustnessIssue`, `hasIssues`, `buildCorrectionMessage`, `isFileMutatingTool`; from
`historyhints.go` `normalizePath`, `readErrorSignals`, `contentMatchesAny`, `resultIsReadError`,
`firstLineMatchesAny`; `intent.go` whole; the two spelling families `readSpellings` and
`wave4WriteTools` from `decompose.go`; `promptFS`/`mustPrompt` (`toolloop.go:126-142`) with the nine
surviving assets (`completion-check-nudge.txt` + the eight `toolloop.go:151-168` names) under
`internal/floor/prompts/`. Copy their unit tests (`offramps_test.go`'s portable cases, `intent_test.go`,
`readerror_test.go:16`, the `robustness_test.go` cases for the copied funcs). Add
`domain.FloorConfig{DisableToolUseEnforcer, DisableEmptyResponseRecovery, DisableToolCallRepair,
DisableToolLoopBreaker, DisableToolResultCap, DisableReadCache bool}` and `Config.Floor` in
`internal/domain/config.go` (beside `:407-415`); zero value = all on (Derived calls). One deep
module: `internal/floor` imports only `internal/domain` and `internal/context`.

**Regression guard.** Copy `toolSet` (`decompose.go:162`) beside the two spelling families —
`offramps.go:46` is `var readToolNames = toolSet(readSpellings)` and `internal/floor` will not compile
without it. Copy `offramps_test.go`'s pure-helper cases only (`TestToolCallPath`, `:91`); the
descriptor/registration cases (`:46`, `:69`) assert over `emptyResponseRecoveryDescriptor` /
`toolUseEnforcerDescriptor` and `Descriptors()` and stay in `internal/mechanisms` until item 5 deletes
the rows. `Config.Floor` goes in the TOP-LEVEL `Config` struct (`internal/domain/config.go:19-358`),
after `Delegation` (`:354-357`); `:407-415` is inside `ContextConfig` (`:371-416`) and would spell
`Config.Context.Floor`, which item 4's seeding and item 11's fold cannot name.

**Files:** `internal/floor/*` (new), `internal/domain/config.go`, `internal/domain/config_test.go`

**Tests.** The copied tests pass in the new package; `TestFloorConfigZeroValueKeepsEveryGuardOn`.

**Acceptance.** `go build ./... && go test ./internal/floor/ ./internal/domain/`

**Commit:** `feat(floor): shared guard substrate and the zero-value-on FloorConfig`

## 4. Post-response guards: tool-call repair and tool-loop breaker — ✅ DONE (2026-09-03)

NOTES (2026-09-03): consequential edit — internal/agent/agent.go: made necessary by the live gate, whose `floorMu`/`floor` fields can only be declared in the Agent struct; the three comments enumerating the four settings-swappable live fields become five. The methods (`SetFloor`, `floorConfig`) are in floorguards.go as the item says.

NOTES (2026-09-03): consequential edit — internal/floor/doc.go: made necessary by the two new files, which docmap_test.go requires the package map to name.

NOTES (2026-09-03): consequential edit — internal/mechanisms/doc.go: made necessary by deleting validate.go/toolloop.go and adding prompts.go, which docmap_test.go enumerates.

NOTES (2026-09-03): consequential edit — internal/tui/fold_test.go: made necessary by `domain.FloorGuardEvent`; `TestFoldEventCoversEveryEventVariant` parses events.go and fails on a variant with no row, so the row is added saying the fold does explicitly nothing with it (item 9 owns the debug-view render).

NOTES (2026-09-03): consequential edit — internal/domain/hooks.go, internal/mechanisms/historyhints.go: made necessary by the retirement; both comments named `tool_loop_interceptor` as a live row.

NOTES (2026-09-03): the item's regression guard (a) named only `toolNames` as a surviving caller, but `library.go:200` calls `validateToolCalls` (and through it `validateCall`/`validateArguments`/`toolKnown`/`requiredParams`) to observe the same issues it records as corrections — so those five moved into `robustness.go` beside `toolNames` rather than being deleted, and `internal/floor/repair.go` carries its own copy. The two halves diverge only when `library` retires at item 17.

NOTES (2026-09-03): the eight loop-directive prompt assets were deleted with `toolloop.go` (item 3 copied them into `internal/floor/prompts/`); `toolloop_test.go`'s package-wide asset roster test `TestEmbeddedDirectivePromptsLoad` moved into `prompts_test.go` beside the embed rather than dying with the row.

NOTES (2026-09-03): the symbol rule "`grep -rn 'validateID\|toolLoopInterceptorID' internal/ --include=*.go` reads zero" is met for the CATALOGUE-row consts; the grep still hits `internal/session/store.go`'s unrelated `validateID` filename-stem validator and `internal/config/configwrite_mechanism_test.go`'s local `validateID = "validate"` YAML-key const (a splice fixture that never touches the catalogue), neither of which names a Mechanism.

NOTES (2026-09-03): the item's "tests that name them" list is a floor, not a ceiling. Beyond it: `internal/agent/{rebind,guardrails,statemachine,setconfine,selfreg,autocompact_guard,console,restoresession,subagent}_test.go`, `cmd/apogee/{validatedsets,wire_live}_test.go` and the `guard`/`egress` stubllm scripts all had to be re-armed. Most are the SAME two interactions the guards now always-on introduce (see the follow-up): a test that scripted an identical repeat to exercise something else now varies its arguments, and one whose subject IS the identical repeat (the circuit-breaker, the allow-for-session cache) sets the guard's own `Floor.Disable…` opt-out with a comment saying why. `driveToolCall` (guardrails_test.go) disables the repair guard for the whole disposition family: several of those drives push a deliberately bare `{}` or a tool a mode withdrew from the menu.

NOTES (2026-09-03): `internal/agent/wave1delivery_test.go`'s `TestWave1_ValidateRetryCarriesCorrectionThenDispatchesFixed` moved into `floorguards_test.go` as the guard's own Bypass proof rather than being edited in place; `TestWave1_ValidateFailShortCircuitsCascade` became `TestWave1_RepairGuardShortCircuitsTheCascade` (the guard now short-circuits the whole hook cascade, not just its tail), and `retryview_test.go`'s two committedLen proofs were recast over the guards.

NOTES (2026-09-03): `internal/mechanisms/retired_test.go`'s live-row exemplar is `liveExemplarID` ("live_exemplar"), registered through `registerIn` into a test-private table, with `exemplarKnown()` as the known-ID list those `ResolveEnabled` cases validate against — the permanent shape the guard asked for. A new `TestPromotedRowsCarryTheirFloorGuardKey` pins the REAL roll: both IDs, `v0.20.0`, and their successor keys, plus the notice each earns.

NOTES (2026-09-03): `internal/validated/shipped_test.go` needed no edit — item 2's `DropRetired` relaxation absorbs the two new roll entries. `cmd/apogee/validatedsets_test.go`'s gemma fixtures now derive their expected size from the shed set instead of the literal 15 (item 18 recasts them over a synthetic user-dir entry), and `wire_live_test.go`'s shipped-set fold sheds the retired members before `BuildMechanisms`.

**What.** Depends on 2, 3. Move `validate.go:51-159` into `internal/floor/repair.go` as
`ToolCallRepair(resp *domain.Response) (correction string, ok bool)` and `toolloop.go:64-251` (minus the
embed, now in item 3) into `internal/floor/loopbreak.go` as `ToolLoopBreak(resp) (directive string, ok bool)`;
tests move with them. Engine: new `internal/agent/floorguards.go` with the live gate (`floorMu`,
`floor domain.FloorConfig`, `SetFloor`, `floorConfig()`, seeded from `cfg.Floor` in `construct.go`, inherited
by children beside `subagent.go:527`) and `runPostResponseGuards(turn, resp) (retry bool, inject string)`
called in `respondAndReview` immediately before `runPostResponseHooks` (`loop.go:447`); extract the retry
application (`loop.go:453-470`) into one helper both paths use, sharing the `attempt` counter. Each
firing emits `domain.FloorGuardEvent{Guard: "tool-call-repair"|"tool-loop-breaker", Action: "retry"}`
(type added in `internal/domain`, rendered in item 9). Then RETIRE the two rows: delete `validate.go`,
`toolloop.go` and their tests, move the embed to `internal/mechanisms/prompts.go` for the rows still
using it, add both IDs to the roll with `Release: "v0.20.0"` and their `Successor` keys, drop the
`Before: [validateID]`/`Before: [syntaxID, autofixID]` edges and `validateID` const uses. Fix the tests
that name them: `catalogue_test.go:171,:414`, `internal/agent/wave1delivery_test.go`,
`retryview_test.go:45,:62`, `enable_mechanisms_test.go:78`, `cmd/apogee/wire_tools_test.go:283`,
`cmd/apogee/validatedsets_test.go:122,:133` (synthetic descriptors), `settingsrows_test.go:57`.
Add the two files' half-lines to `internal/agent/doc.go`. Both guards run WITHOUT strikes-3
suppression and without a Turn-Budget throttle (ADR 0071 decision 1): the per-Turn
`maxPostResponseRetries` bound is the only limiter.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. (a) The moved range is `validate.go:51-159`; `toolNames` moves into
`robustness.go` (it dies with the enforcer at item 5). (b) The closed "tests that name them" list
becomes the RULE "`grep -rn 'validateID\|toolLoopInterceptorID' internal/ --include=*.go` reads zero"
with `readrepeat.go`, `autofix.go`, `syntax.go`, `robustness_test.go`, `historyhints_test.go`,
`writedetection_test.go` added to Files — the surviving rows' ordering edges naming the two consts are
dropped in place. (c) `internal/mechanisms/retired_test.go` joins Files and its live-row exemplar
`"validate"` becomes a synthetic row registered through `registerIn` into a test-private catalogue
(permanent — the shipped catalogue ends empty); `grammar` stays the plain-retired case. (d) The two
guards run without strikes suppression, as What now states (ADR 0071 decision 1).

**Files:** `internal/floor/repair.go`, `internal/floor/loopbreak.go` (+tests), `internal/agent/floorguards.go`, `internal/agent/loop.go`, `internal/agent/construct.go`, `internal/agent/subagent.go`, `internal/agent/doc.go`, `internal/domain/events.go`, `internal/mechanisms/{validate.go,toolloop.go,prompts.go,retired.go,retired_test.go,catalogue_test.go,robustness.go,robustness_test.go,readrepeat.go,autofix.go,syntax.go,historyhints_test.go,writedetection_test.go}`, the listed tests

**Tests.** Scripted-responder tests in `internal/agent`: an unknown tool call is corrected and retried
under `Bypass: true`; an identical repeat Turn draws the loop directive; `Floor.DisableToolCallRepair`
lets the bad call through; the event carries the key name. Retired-ID notice test for `validate`.

**Acceptance.** `go build ./... && go test ./internal/floor/ ./internal/agent/ ./internal/mechanisms/ && go test ./cmd/apogee/ -run 'Tools|Validated|SettingsRows'`

**Commit:** `feat(floor): tool-call repair and tool-loop breaker are floor guards`

## 5. Post-response guards: tool-use enforcer and empty-response recovery — ✅ DONE (2026-09-03)

NOTES (2026-09-03): the item's regression guard named only `readToolNames` as a surviving offramps.go caller, but `library.go:271` calls `shouldEnforceToolUse` (tooluseenforcer.go) and through it `wroteRecently`, `assistantMessageCount`, `previousAssistantWasTextOnly` and `hasEverUsedTools` — so those five moved into `library.go` (its sole caller, and the file that retires them at item 17) rather than being deleted, on item 4's `validateToolCalls` precedent: `internal/floor` carries its own copy and the two halves diverge only when `library` retires. `hasRecentProgress` had no surviving caller and went with the row.

NOTES (2026-09-03): `offramps_test.go` could not simply be deleted — its fixtures `offrampResponse`/`readCall`/`userMsg`/`assistantCall` and `TestToolCallPath` are used by `cachedcontent_test.go`, `errorenrich_test.go`, `readloop_test.go`, `readrepeat_test.go`, `library_test.go` and `writedetection_test.go`. They moved into `historyhints_test.go` beside the helpers they exercise, and `offrampResponse` was renamed `historyResponse` (item 6's rule requires `off-ramp|OffRamp` to read zero across `internal/`, and the name names a concept this item retires); `assistantText` had no surviving caller and went with the row.

NOTES (2026-09-03): `internal/mechanisms/prompts/completion-check-nudge.txt` was deleted with the row (item 3 copied it into `internal/floor/prompts/`) and its `prompts_test.go` roster entry with it — the grammar precedent for a retired row's assets.

NOTES (2026-09-03): `TestEmptyResponseRecoveryTreatsRecentEditAsProgress` (the 2026-08-10 edit-tool write-detection pin) moved from `writedetection_test.go` into `internal/floor/emptyreply_test.go` recast over `RecoverEmpty`, its subject having moved; the file's NOTE paragraph about `wroteRecently`'s untestable branch was repointed at the new homes.

NOTES (2026-09-03): three `internal/mechanisms/retired_test.go` cases (`TestOffRampFloorIsTheCapOffRampRows`, `TestResolveEnabledDefaultsToTheOffRampFloor`, `TestResolveEnabledExplicitFalseRemovesOneOffRamp`) asserted the catalogue CARRIES CapOffRamp rows and failed the moment the second one retired, so they were recast over the now-empty floor (the machinery itself is untouched — item 6 removes it). `TestBuildEnabledMechanismsFloorsAFreshRegistry` (`internal/agent/construct_test.go`) failed the same way and was recast the same way; `enable_mechanisms_test.go`'s `NilAndEmptyBuildTheOffRampFloor` still passes vacuously and is left to item 6.

NOTES (2026-09-03): the item's Acceptance excludes `cmd/apogee`, but two of its tests failed on the emptied floor — `TestListMechanismsAppliesTheOffRampFloor` (`wire_options_test.go`) and `TestSettingsRowsFormatEffectiveValues`'s `"4 mechanisms"` pin. Rather than commit a red package between items 5 and 6, both assertions were made honest about the empty floor with no OffRampFloor machinery touched; item 6 owns their final shape.

NOTES (2026-09-03): the recast of `enable_mechanisms_test.go` needed a surviving catalogued row that fires through a scripted loop — `syntax`, driven by a well-formed `write_file` call with unbalanced Go content (well-formed so the tool-call repair guard ahead of it stands down). `rebind_test.go` swapped to `autofix`. `wave1delivery_test.go`'s four off-ramp tests moved into `floorguards_test.go` recast over the guards (item 4's precedent), keeping the Bypass + tripped-Turn-Budget pin as ADR 0071 decision 1's dispatch-level proof.

NOTES (2026-09-03): consequential edits — internal/agent/construct.go, internal/agent/loop_test.go, internal/agent/emptyreply_test.go, internal/domain/domaintest/domaintest.go, internal/mechanisms/{doc.go,prompts.go,intent.go,intent_test.go,robustness.go,retired.go,catalogue_test.go}: made necessary by the retirement; each named one of the two rows, `offramps.go`, or the off-ramp floor as live.

NOTES (2026-09-03): `internal/validated/shipped_test.go:95,:98` still spell both IDs — it pins the SHIPPED gemma JSON verbatim, which item 18 retires; item 2's `DropRetired` relaxation absorbs the two new roll entries, so the file needed no edit here.

**What.** Depends on 4. Move `tooluseenforcer.go:52-107` → `internal/floor/tooluse.go`
(`EnforceToolUse(resp) (correction string, ok bool)`) and `emptyresponse.go:51-99` →
`internal/floor/emptyreply.go` (`RecoverEmpty(resp) (nudge string, ok bool)`); tests move. Extend
`runPostResponseGuards` in the ratified order; `Guard` names `"empty-response-recovery"` /
`"tool-use-enforcer"`. Restate `loop.go:518-522` (`reviewedOutcome`'s "first claim" doc) in terms of the
guard. RETIRE the two rows: delete both files and tests, delete `offramps.go` (its helpers now live in
`internal/floor`; the retired rows still needing `isReadTool`/`toolCallPath` get them from
`historyhints.go` — move those two there), the two ID consts, roll entries with `Successor`. Fix
`internal/agent/enable_mechanisms_test.go:33`, `writedetection_test.go:186`, `catalogue_test.go:171`.
Do NOT touch `OffRampFloor` and its callers here — item 6 removes them; until then `OffRampFloor`
returns `nil`, which is the correct value.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit: `readToolNames` (`offramps.go:46`, with the `toolSet` call it needs)
goes to `historyhints.go` beside `isReadTool`/`toolCallPath` — `readloop.go:143,:165` and
`readrepeat.go:89` still call it and retire only at items 14/15. `internal/mechanisms/offramps_test.go`
is DELETED with the rows (`:48`, `:72` name the two ID consts). The three-file list becomes the RULE
"`grep -rn '\"tool_use_enforcer\"\|\"empty_response_recovery\"' internal/ cmd/ --include=*_test.go`
reads zero, each case recast over a synthetic registered row" — it reaches `rebind_test.go:114,:161,:181`,
`retryview_test.go:160,:203,:266,:331`, `wave1delivery_test.go:241,:273,:287,:323,:355-423`,
`construct_test.go:488` and `enable_mechanisms_test.go:126,:138,:192,:199,:211,:218` as well as `:33`.
The move ranges are `tooluseenforcer.go:52-107` (the file is 107 lines) and `emptyresponse.go:51-99`
(`completionCheckNudge` at `:60` included; `isEmptyResponse` runs to `:99`).

**Files:** `internal/floor/tooluse.go`, `internal/floor/emptyreply.go` (+tests), `internal/agent/floorguards.go`, `internal/agent/loop.go`, `internal/mechanisms/{tooluseenforcer.go,emptyresponse.go,offramps.go,offramps_test.go,historyhints.go,retired.go,catalogue_test.go,writedetection_test.go}`, `internal/agent/{enable_mechanisms_test.go,rebind_test.go,retryview_test.go,wave1delivery_test.go,construct_test.go}`

**Tests.** Scripted-responder tests: a prose reply to an action request is retried into a tool call
under Bypass; an empty reply draws the completion-check nudge then `reviewedOutcome` faults after the
retries; `Floor.DisableToolUseEnforcer` leaves prose alone. `setlive_test.go`'s synthetic off-ramp case
still passes (lab rows keep the Capability).

**Acceptance.** `go build ./... && go test ./internal/floor/ ./internal/agent/ ./internal/mechanisms/`

**Commit:** `feat(floor): tool-use enforcer and empty-response recovery are floor guards`

## 6. The off-ramp floor plumbing goes — ✅ DONE (2026-09-03)

NOTES (2026-09-03): the item's literal `grep -rn "off-ramp\|OffRamp" internal/ cmd/ --include=*.go` outside `internal/domain/mechanism.go` CANNOT read zero — `domain.CapOffRamp` stays for lab rows, as the item itself says, and is referenced from `internal/agent/hookrun.go:55` (the Bypass gate) plus a dozen lab-row tests (`mechanism_dispatch_test.go`, `setlive_test.go`, `hookrun_test.go`, `selfreg*.go`). What was applied instead is the regression guard's own operative symbol rule — `grep -rn 'OffRampFloor' internal/ cmd/ --include=*.go` reads zero, met — plus every sentence claiming a live off-ramp FLOOR or default-on off-ramps, wherever it sits; the Capability-semantics sentences (Bypass exemption, `SuppressExempt`) describe machinery that stays and were left alone.

NOTES (2026-09-03): consequential edit — internal/domain/config.go: made necessary by construct.go's empty list building nothing; the `EnableMechanisms` doc said "Empty/nil arms the OFF-RAMP FLOOR and nothing else".

NOTES (2026-09-03): consequential edit — cmd/apogee/delegation.go: made necessary by the same change; `subAgentCatalogue`'s replace-whole doc said an all-false map "replaces whatever the parent arms with the OFF-RAMP FLOOR alone".

NOTES (2026-09-03): consequential edit — cmd/apogee/validatedsets.go: made necessary by dropping the union; three comments (`:22`, `:157`, `:194`) named "the D1 floor — structure plus the default-on off-ramps" as the posture a non-matching model runs.

NOTES (2026-09-03): consequential edit — internal/config/defaults/config.yaml: made necessary by the same; the shipped first-run template's `mechanisms:` header said "Each Mechanism bar the two off-ramps ships OFF by default" and carried a THE TWO OFF-RAMPS ARE THE EXCEPTION paragraph. Only that block was rewritten — the `:856-863` example is item 10's.

NOTES (2026-09-03): consequential edit — internal/config/configwrite_mechanism_test.go, cmd/apogee/settingsrows_test.go, internal/agent/emptyreply_test.go: made necessary by the same; each carried one sentence stating the floor or the default-on off-ramps as live.

NOTES (2026-09-03): `internal/config/registry_test.go` is in the item's Files but needed no edit — the package has no test that names `enabledCount`, and `go test ./internal/config/` is green with the floor term gone.

NOTES (2026-09-03): tests whose SUBJECT was the removed machinery were recast rather than edited in place. `TestBuildEnabledMechanismsFloorsAFreshRegistry` → `TestBuildEnabledMechanismsEmptyListBuildsNothing` (the name the item's Tests line gives it); `TestEnableMechanisms_NilAndEmptyBuildTheOffRampFloor` → `…BuildNothing`; `TestListMechanismsAppliesTheOffRampFloor` → `TestListMechanismsReadsTheBlock`; `TestWithOffRampFloorDeduplicatesAShippedSet` → `TestAShippedValidatedSetBuildsAsTheEnableList` (its function is deleted, but its live claim — a shipped set, retired members shed, builds through the engine's own `BuildMechanisms` — survives at the seam and is what the recast pins). `TestOffRampFloorIsEmptyWithNoCapOffRampRow` was DELETED with the machinery it asserted over, along with the `floorPlus` and `countID` helpers left with no caller.

NOTES (2026-09-03): `internal/agent/wave1delivery_test.go`'s package header enumerated six delivery cases; items 4 and 5 moved five of them into `floorguards_test.go` and left the header describing tests the file no longer holds. Removing the off-ramp clause my own change falsified would have left the comment half-corrected, so the whole header was made honest in the same edit.

**What.** Depends on 5. With no `CapOffRamp` row, remove `mechanisms.OffRampFloor` (`retired.go:45-72`)
and the union in `ResolveEnabled` (`:144-148`); the empty-list floor in `agent.buildEnabledMechanisms`
(`construct.go:284-292`) becomes "empty list builds nothing"; `withOffRampFloor` (`cmd/apogee/wire_live.go:454-462`)
and its two call sites (`:199`, `wire_settings.go:2066-2068`); the floor term in `enabledCount`
(`internal/config/registry.go:948-961`); `ListMechanisms`'s floor (`cmd/apogee/wire_options.go:225`).
Tests: `retired_test.go:73-155`, `internal/agent/construct_test.go:488`, `cmd/apogee/delegation_test.go:517`,
`wire_options_test.go:118-135` (use a synthetic registered row or assert the empty list), `registry`'s
`enabledCount` tests. Every in-code sentence naming "the two off-ramps" or "off-ramp floor" in these
files is restated as "no catalogued row is on by default; the Floor guards are `Config.Floor`" —
rule: `grep -rn "off-ramp\|OffRamp" internal/ cmd/ --include=*.go` outside `internal/domain/mechanism.go`
(the Capability stays for lab rows) reads zero after this item.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. Replace the closed file/line list with the symbol rule
"`grep -rn 'OffRampFloor' internal/ cmd/ --include=*.go` reads zero": it reaches the sites the list
missed — `cmd/apogee/headless_test.go:481`, `cmd/apogee/wire_live_test.go:309,:321,:331,:335`,
`internal/agent/enable_mechanisms_test.go:143-168`, `internal/agent/construct_test.go:493,:495,:543`,
`internal/mechanisms/retired_test.go:65,:189,:220,:221` and the `internal/config/options.go:327`
comment. `internal/config/registry.go:605`'s `mechanisms` Desc ("…the two off-ramps default on, every
other one defaults off.") is pinned VERBATIM by `internal/tui/settings_test.go:3639`, so the new Desc
string lands there in the same commit and `go test ./internal/tui/ -run Settings` joins Acceptance.

**Files:** `internal/mechanisms/retired.go`, `internal/mechanisms/retired_test.go`, `internal/agent/construct.go`, `internal/agent/construct_test.go`, `internal/agent/enable_mechanisms_test.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_live_test.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_options.go`, `cmd/apogee/wire_options_test.go`, `cmd/apogee/delegation_test.go`, `cmd/apogee/headless_test.go`, `internal/config/registry.go`, `internal/config/registry_test.go`, `internal/config/options.go`, `internal/tui/settings_test.go`

**Tests.** `TestBuildEnabledMechanismsEmptyListBuildsNothing`; `ResolveEnabled(nil, known)` returns nil;
the `mechanisms` row's Desc is re-pinned in `internal/tui/settings_test.go`.

**Acceptance.** `go build ./... && go test ./internal/mechanisms/ ./internal/agent/ ./internal/config/ && go test ./internal/tui/ -run Settings && go test ./cmd/apogee/ -run 'Options|Delegation|Live|Settings'`

**Commit:** `refactor: remove the off-ramp floor plumbing now that the guards are engine behaviour`

## 7. Pre-tool-exec guard: read cache — ✅ DONE (2026-09-03)

NOTES (2026-09-03): the guard is spelled `CacheRead(view, edit) (ok bool)` exactly as the item asks, and the seam wraps the pending `*domain.ToolCall` in a `domain.NewToolCallEdit` — the same wrapper the hooks mutate through — so the guard's write reaches the call the loop owns. The item's `:362` call is kept for seam parity with a comment saying it is a no-op for reads, as the regression guard directs.

NOTES (2026-09-03): the three helpers `priorSuccessfulReadUnchanged`, `capReadArguments` and `toolDeclaresMaxLines` were private to `cachedcontent.go` — the symbol grep found no surviving caller for any of them, so they moved wholesale rather than being rehomed. `cachedContentReadCap` became `readCacheLines`; every other line of decision logic is verbatim.

NOTES (2026-09-03): the four scripted-responder tests insert an intervening read of ANOTHER file between the two reads of the same one. Two identical consecutive read Turns would trip the tool-loop breaker, which runs first at the post-response seam and would end the Turn before the pre-tool-exec seam is ever reached — so the read cache is exercised over a realistic three-read sequence rather than by switching a sibling guard off.

NOTES (2026-09-03): `TestCachedContentLeavesEditedSinceUntouched` moved from `writedetection_test.go` into `internal/floor/readcache_test.go` recast over `CacheRead`, its subject having moved (item 5's precedent for `TestEmptyResponseRecoveryTreatsRecentEditAsProgress`); the pin it carries — `isFileMutatingTool` counting `edit_existing_file` as a write-since — survives intact.

NOTES (2026-09-03): consequential edit — internal/floor/doc.go: made necessary by the new readcache.go, which docmap_test.go requires the package map to name.

NOTES (2026-09-03): consequential edit — internal/mechanisms/doc.go: made necessary by deleting cachedcontent.go, which the package map enumerates. The map's count line still read "Eighteen files carrying twenty catalogue rows" — stale since items 4 and 5 deleted four row files without touching it — so it was corrected to the true count (thirteen files, fifteen rows) rather than left half-corrected at a number my own deletion made further wrong.

NOTES (2026-09-03): consequential edits — internal/mechanisms/{historyhints.go,robustness.go,decompose_test.go,catalogue_test.go}, internal/domain/{tooledit.go,registry.go}: made necessary by the retirement; each named `cached_content_intercept` as a live catalogue row (the history family's header, the `isFileMutatingTool` users list, the write-detection pin's rationale, the ported-waves comment, `SetArguments`'s worked example, and `detectIncompatibility`'s two-rows-at-different-hook-points example, whose pair became read_loop/read_repeat).

NOTES (2026-09-03): consequential edit — internal/mechanisms/retired_test.go: `TestPromotedRowsCarryTheirFloorGuardKey` enumerates the promoted rows on the real roll, so the new one joins it. Item 5's two promoted rows (`tool_use_enforcer`, `empty_response_recovery`) are still absent from that table — missing coverage on already-shipped work, not a defect of this item.

NOTES (2026-09-03): `internal/agent/mechanism_dispatch_test.go:294-295` and `internal/domain/registry_ordered_test.go:216-237` still spell `cached_content_intercept`, but as SYNTHETIC ids registered into test-private tables that never touch the catalogue — no edit needed. `internal/validated/shipped_test.go:95` pins the shipped gemma JSON verbatim (item 18's), and item 2's `DropRetired` relaxation absorbs the new roll entry, so it needed none either.

**What.** Depends on 3. Move `cachedcontent.go:77-177` → `internal/floor/readcache.go` as
`CacheRead(view domain.LoopView, edit *domain.ToolCallEdit) (ok bool)` (same signature shape the hook
used; verbatim decision logic incl. `priorSuccessfulReadUnchanged`, `capReadArguments`,
`toolDeclaresMaxLines`); tests move. Engine: `runPreToolExecGuards(turn, call)` in `floorguards.go`,
called immediately before `runPreToolExecHooks` at BOTH `dispatch.go:257` and `:362`; gated by
`DisableReadCache`; emits `FloorGuardEvent{Guard: "read-cache", Action: "intercept"}`. RETIRE the row:
delete `cachedcontent.go` + tests, the ID const, roll entry with `Successor: "read-cache"`; fix
`catalogue_test.go:171`, `writedetection_test.go:90`. The `IncompatibleWith: [read_loop, read_repeat]`
edge disappears with the row.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `readloop.go:55` and
`readrepeat.go:49` name `cachedContentInterceptID` in their `IncompatibleWith` and
`historyhints_test.go:31,:58-59` table it: drop `cached_content_intercept` from both edges and both
test tables HERE, or `go build ./...` — this item's own acceptance — fails. The guard never answers a
read from history: it appends `max_lines:1` (`cachedcontent.go:77-96`) and only when the tool's schema
declares that field (`:159-176`), so the test pins the ARGUMENTS instead. `dispatch.go:52-60` routes
leaves to `dispatchSerially` and only DELEGATIONS to `dispatchFanOut`→`prepareDelegation` (`:359`), so
no read call ever reaches `:362`: keep the `:362` guard call for seam parity and say it is a no-op for
reads.

**Files:** `internal/floor/readcache.go` (+test), `internal/agent/floorguards.go`, `internal/agent/dispatch.go`, `internal/mechanisms/{cachedcontent.go,cachedcontent_test.go,readloop.go,readrepeat.go,retired.go,catalogue_test.go,writedetection_test.go,historyhints_test.go}`

**Tests.** Scripted-responder test on the serial path: the second unchanged read of the same path
carries `max_lines:1` on a tool whose schema declares the field; a read after a write is not capped; a
tool whose schema omits `max_lines` is left untouched; `DisableReadCache` leaves the arguments
untouched.

**Acceptance.** `go build ./... && go test ./internal/floor/ ./internal/agent/ ./internal/mechanisms/`

**Commit:** `feat(floor): the read cache is a floor guard at both dispatch seams`

## 8. Pre-request guard: tool-result cap — ✅ DONE (2026-09-03)

NOTES (2026-09-03): DEVIATION from the item's regression guard, which directs spelling the literal `"tool_result_cap"` in `guideddecomposition.go:115` so the surviving `Requires` edge keeps compiling. Applying it literally would have left the last live declarer of `Requires` naming a RETIRED ID, and `ValidateRequirements` runs over the built registry — so `mechanisms: {guided_decomposition: true}` (with or without the tolerated `tool_result_cap: true`, which `ResolveEnabled` drops) would fail construction with `ErrMissingRequirement`, an ID apogee accepted at v0.20.0 and would refuse today. That is the regression class AGENTS.md forbids shipping, so the edge was DROPPED instead and the relation restated in prose ("the peer is a Floor guard now, on in every arm"). `cmd/apogee/guided_decomposition_test.go` gains a case pinning that a block still naming the retired key boots.

NOTES (2026-09-03): the guard's `Requires` drop moved four proofs off the catalogue, since no catalogued row declares `Requires` any more (item 13 plans the same move for what it deletes). `TestEnableMechanisms_HalfStackFailsRequirement` (internal/agent) and `TestEnableErrors_MatchableThroughRoot` (apogee_test.go) now inject a synthetic requiring row through `Config.Mechanisms` — `newAgent`/`New` merge it before the gates, so `ErrMissingRequirement` is still proved at construction; `mechanism_dispatch_test.go` gains the `requiresMech` fixture beside the existing `incompatMech`. `BuildMechanisms` and `Rebind` build into a FRESH registry and never read `Config.Mechanisms`, so their refusal cases (`TestBuildMechanisms_RefusesWhatConstructionRefuses`, `TestRebindRebuildsMechanismsForNewModel`, `benchreadiness_test.go`'s arm) were recast over the incompatibility gate, which a catalogue pair can still trip. `example_test.go`'s two examples were rewritten over `IncompatibleWith` for the same reason, and `apogee_test.go`'s clone-contract test now exercises `IncompatibleWith` alone (both fields clone through the same `slices.Clone`; item 13 moves the two-field assertion to a synthetic row in `internal/mechanisms`).

NOTES (2026-09-03): the item's "re-arm the sub-agent inheritance test on a surviving lab row" is met with `syntax`: the child now writes a Go file with an unbalanced payload, which the inherited row retries in place at Depth 1. The old proof (the cap firing inside the child) is no proof of inheritance at all now — the guard is on for every agent at every Depth.

NOTES (2026-09-03): `internal/agent/toolresultfloor_test.go`'s division-of-labour test drives `runPreRequestGuards` over a stock Config instead of arming the row, and gains `TestToolResultCapOptOutSendsTheResultWhole`; the scripted Bypass proof the item's Tests line asks for is `TestFloorGuard_ToolResultCapTrimsAnOlderResultUnderBypass` in `floorguards_test.go`, table-driven over the opt-out and reading the CAPTURED WIRE REQUEST so "the request is trimmed, the conversation is not" is pinned end to end. Its two tool Turns carry different arguments so the tool-loop breaker ahead of the seam stands down.

NOTES (2026-09-03): consequential edits — internal/floor/doc.go (docmap_test.go requires the new resultcap.go in the package map); internal/mechanisms/doc.go (the map enumerates the deleted toolresultcap.go, and its count line — already stale at "Thirteen files carrying fifteen catalogue rows" since prompts.go joined the paragraph — was corrected to the true twelve/fourteen rather than left at a number my deletion made further wrong, item 7's precedent); internal/agent/{dispatch.go,compact.go}, internal/context/{doc.go,prune.go,toolresult.go}, internal/config/configwrite_mechanism.go, internal/agent/autocompact_guard_test.go — each named `tool_result_cap` as a live, config-gated, Bypass-disabled Mechanism, restated as the Floor guard (the two `dispatch.go` ranges the item names as clamp-vs-guard: both structural, the clamp editing the conversation and the guard the projected request).

NOTES (2026-09-03): while correcting the `internal/mechanisms/doc.go` prompt-loader half-line my own deletion touched, its list of asset loaders still named `emptyresponse.go`, deleted at item 5 — dropped in the same edit rather than left naming a file that is not there.

NOTES (2026-09-03): the item's rule "`grep -rn "tool_result_cap" internal/ cmd/ --include=*.go` reads zero outside `retired.go`" is met for all NON-test code. Test files still spell it where it is correct to: as prose naming the retired ID (`guideddecomposition_test.go`, `catalogue_test.go`, `apogee_test.go`, `benchreadiness_test.go`, `cmd/apogee/guided_decomposition_test.go` — the last also as the live retired-key-still-boots fixture), as SYNTHETIC ids in test-private tables that never touch the catalogue (`internal/domain/registry_ordered_test.go`, `internal/tui/settings_test.go`'s `newMechanismLog`), and in `internal/validated/shipped_test.go:97`, which pins the shipped gemma JSON verbatim (item 18's) and needs no edit because item 2's `DropRetired` relaxation absorbs the new roll entry.

NOTES (2026-09-03): `ISSUES.md`, `README.md` and `docs/manual/{commands,configuration,probe}.md` are dirty in the working tree from a CONCURRENT documentation pass, not from this item — they are deliberately absent from FILES and were left untouched.

**What.** Depends on 3. Move `toolresultcap.go:74-126` → `internal/floor/resultcap.go` as
`CapToolResults(req *domain.Request) (capped int)` keeping the `0.4` fraction, `capMaxChars`,
`mostRecentToolCallTurn` and `apogeectx.TruncateToolResult`; tests move. Engine: `runPreRequestGuards`
in `floorguards.go`, called immediately before `runPreRequestHooks` at BOTH `loop.go:151` and `:212`;
gated by `DisableToolResultCap`; emits `FloorGuardEvent{Guard: "tool-result-cap", Action: "cap"}` when
`capped > 0`. Restate `dispatch.go:1320-1326` and `:1353-1372` — they contrast the structural clamp with
"the Mechanism" — as clamp-vs-guard (both structural: the clamp edits the conversation, the guard the
projected request). RETIRE the row: delete `toolresultcap.go` + tests, drop the `After: decomposeID`
edge, roll entry with `Successor: "tool-result-cap"`; fix `catalogue_test.go:171,:211-266` (the
ordering-seeds test loses its last surviving ID — rewrite it over synthetic rows in item 19; here only
remove `tool_result_cap`), `ISSUES.md` stays for item 23.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `guideddecomposition.go:115` declares
`Requires: []domain.MechanismID{toolResultCapID}` (read by `guideddecomposition_test.go:63,:64,:91,:107`)
and that row retires only at item 13, so spell the requirement as the literal `"tool_result_cap"` in
this commit. `internal/agent/toolresultfloor_test.go:156-200` and
`enable_mechanisms_subagent_test.go:27,:95,:158` construct agents with
`EnableMechanisms: [… "tool_result_cap"]` and would now draw `ErrUnknownMechanism`: rewrite the floor
test over `runPreRequestGuards` and re-arm the sub-agent inheritance test on a surviving lab row or a
registered experimental hook. The two prose ranges become the RULE
"`grep -rn \"tool_result_cap\" internal/ cmd/ --include=*.go` reads zero outside `retired.go` after
this item" — which reaches `dispatch.go:1334-1340` as well; `internal/agent/compact.go:226-232` (the
`tool_result_cap` sentence) is this item's, restated as the tool-result-cap guard.

**Files:** `internal/floor/resultcap.go` (+test), `internal/agent/floorguards.go`, `internal/agent/loop.go`, `internal/agent/dispatch.go`, `internal/agent/compact.go`, `internal/agent/toolresultfloor_test.go`, `internal/agent/enable_mechanisms_subagent_test.go`, `internal/mechanisms/{toolresultcap.go,toolresultcap_test.go,guideddecomposition.go,guideddecomposition_test.go,retired.go,catalogue_test.go}`

**Tests.** Scripted test: an oversized result in an earlier Turn is capped in the outgoing request under
Bypass while the conversation keeps the full text; the freshest result is never touched;
`DisableToolResultCap` sends it whole.

**Acceptance.** `go build ./... && go test ./internal/floor/ ./internal/agent/ ./internal/mechanisms/ ./internal/context/`

**Commit:** `feat(floor): the tool-result cap is a floor guard on the request projection`

## 9. `FloorGuardEvent` reaches every Driver — ✅ DONE (2026-09-03)

NOTES (2026-09-03): the item's What words the debug line `guard <key> @ <hook>: <action>`; its regression guard offers the hookless `guard <key>: <action>` as the alternative to adding a `Hook HookPoint` field, and that is what landed. A Hook field would have had to be filled at the three seams in `internal/agent/floorguards.go` — a file this item's Files list deliberately omits — and the event's own doc now says why it carries none: a guard is not registered at a hook, and the Guard key already names the seam. A non-empty `Detail` renders as a trailing ` (<detail>)`; no guard fills one today.

NOTES (2026-09-03): consequential edit — internal/tui/fold_test.go: made necessary by the new debug-view render; item 4's row read "FloorGuardEvent is inert in the transcript", which my own change made false — it is inert OUTSIDE the debug view, exactly as the MechanismFiredEvent row beside it says of itself. The assertion (a default fold contributes nothing) is unchanged and still passes.

NOTES (2026-09-03): `internal/run/transcript_test.go` and `cmd/apogee/headless_test.go` join Files — the item's Tests line and Binding claim a SILENCE at both surfaces, and neither had a test pinning it (`internal/run` had no Mechanism-firing case at all). `internal/run` gains `TestTranscriptFoldIgnoresAGuardFiring` (a guard firing and a Mechanism firing fold zero entries); `TestPruneNoticeSinkForwardsEveryEvent` gains a `FloorGuardEvent` that is forwarded and prints no stderr line.

NOTES (2026-09-03): `cmd/apogee/daemon*.go` and `internal/tui/inspector*.go` were re-checked against the item's own grep and carry no `MechanismFiredEvent`/`PruneEvent` switch, so they stay out of Files as the regression guard says.

NOTES (2026-09-03): `ISSUES.md`, `README.md`, `docs/manual/{commands,configuration,probe}.md` and the untracked `docs/plans/2026-09-03 - 01 - …` are dirty from a CONCURRENT pass, not this item — deliberately absent from FILES and left untouched.

**What.** Depends on 4. `domain.FloorGuardEvent{EventBase; Guard, Action, Detail string}` (added in
item 4) is rendered wherever `MechanismFiredEvent` is today and wherever `PruneEvent` is: rule —
`grep -rn "MechanismFiredEvent\|PruneEvent" internal/ cmd/ --include=*.go` lists every consumer (TUI
transcript `internal/tui/transcript.go:916,:941-942,:1802-1810` hidden debug view, headless, daemon,
inspector/e2e fixtures); each gains the guard case. TUI wording in the debug view:
`guard <key> @ <hook>: <action>`. Binding: no visible transcript line outside the debug view (a guard
firing is not user news, unlike a prune pass).

**Regression guard.** `FloorGuardEvent{EventBase; Guard, Action, Detail}` carries no hook —
`EventBase` is Depth/Turn/CallID (`internal/domain/events.go:37-55`) — so either give the event a
`Hook HookPoint` field as `MechanismFiredEvent` has (`events.go:246`) or pin `guard <key>: <action>`.
Word the render rule as "wherever `MechanismFiredEvent` is rendered": `PruneEvent` also reaches
`cmd/apogee/headless.go:117-124` (a visible stderr line) and `internal/run/transcript.go:91-97` (a
session-record Note), both of which contradict this item's own Binding — headless and `internal/run`
render a guard firing as NOTHING, exactly as they already do for a Mechanism firing
(`internal/run/transcript.go:75-78`). `cmd/apogee/daemon*.go` and `internal/tui/inspector*.go` carry no
such switch at all: they leave Files, `internal/run/transcript.go` (the one consumer the grep finds and
the Files omit) joins it, and `go test ./internal/run/` joins Acceptance. Add
`FloorGuardEvent = domain.FloorGuardEvent` to the root alias block (`apogee.go:209-223`) so an external
Driver can name the new variant.

**Files:** `internal/domain/events.go`, `internal/tui/transcript.go`, `internal/tui/transcript_test.go`, `cmd/apogee/headless.go`, `internal/run/transcript.go`, `apogee.go`

**Tests.** A transcript test renders one `FloorGuardEvent` in the debug view with the exact string
above; `internal/run`'s fold contributes nothing for it, as it already does for a Mechanism firing.

**Acceptance.** `go build ./... && go test ./internal/tui/ -run 'Transcript' && go test ./internal/run/ && go test ./cmd/apogee/ -run 'Headless'`

**Commit:** `feat(events): floor-guard firings reach every Driver as FloorGuardEvent`

## 10. The six config keys through `internal/config` — ✅ DONE (2026-09-03)

NOTES (2026-09-03): consequential edit — cmd/apogee/testdata/frames/t16-settings-rows.txt: made necessary by the six new registry rows; `empty-response-recovery` is now the longest key path, so the /settings pane's name column widens by one character in every row of the recorded frame (regenerated with `go test ./cmd/apogee -run TestE2ELiveStateFollowsTheRunningSession -update`).

NOTES (2026-09-03): the item's Files list `internal/config/registry_test.go`, `configwrite_scalar_test.go` and `defaults_test.go`, but none needed an edit and none was touched — all three are registry sweeps or default-mirrors rather than golden lists: `TestRegistryRowInvariants`/`TestRegistryRowsProjectEveryValue` walk `KeyRegistry`, `TestSpliceScalarSettingRoundTripsEveryEditableKey` walks the editable rows (it now covers all six, verified by name), and `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt` compares against `wantDefaults()`, which the six live `<key>: true` template lines already agree with.

NOTES (2026-09-03): the item's yaml scope was `defaults/config.yaml:856-863` (the `mechanisms:` example naming retired IDs); the same block's RECOMMENDED paragraph three paragraphs above recommended `tool_result_cap` — retired by item 8, so the shipped first-run template was telling a reader to write an ID that is now a startup error. Folded in on the "enumeration is a floor, not a ceiling" rule and rewritten to point at the `tool-result-cap` guard key instead. `config.yaml:836`'s catalogue-path pointer was left alone (item 20's).

NOTES (2026-09-03): the new floor-guard comment block could not word the sentence "see the mechanisms: block further down" as written — a comment line beginning `# mechanisms:` is a second commented spelling of that key and trips `TestSaveMechanismSettingSeedsAnAbsentConfigAndLandsBelowTheExample` ("it comments mechanisms: out in two places"). Reworded to "the catalogue block further down".

NOTES (2026-09-03): the item's stated 10/11 split holds — `go test ./internal/config/` is green and `TestEveryEditableSettingKeyHasAnApply` (six new subtests) and `TestSettingsRowsFormatEffectiveValues`'s length pin are red in `cmd/apogee` until item 11 lands the applies.

**What.** Depends on 3. Add six `KindBool` registry rows (`internal/config/registry.go`, after
`prune-tool-results` at `:404-409`, `Default: "true"`, `Editable: true`, each `Desc` one sentence
naming the guard), `fileConfig` `*bool` fields + `keyAccessors` rows (`config.go` beside `:678-683`,
`:1251-1256`), `Options` fields (`options.go` beside `:265-270`), and the commented block + live
`<key>: true` lines in `internal/config/defaults/config.yaml` beside `:526-538`. Rewrite the
`mechanisms:` example at `defaults/config.yaml:856-863` (it names retired IDs) into a two-line lab-surface
note. Golden tests: `config_test.go:510-528` (Options names), `:483-496`, `registry_test.go:24-35`,
`:204-230`, `:258-274`, `configwrite_scalar_test.go:243-270`. Keys map to `domain.FloorConfig`'s
`Disable…` fields by negation at the ONE composition seam (items 11); `internal/config` stays positive.

**Regression guard.** `wantDefaults()` (`internal/config/config_test.go:299-310`) is the hand-written
"every key at its built-in default" Options, asserted at `:279`, `:4797` and
`internal/config/defaults_test.go:136`: it joins the golden list with six `<Guard>: true` entries, and
`defaults_test.go` stays in Files. The six new `Editable: true` rows also fail
`cmd/apogee/wire_settings_test.go:485` (`TestEveryEditableSettingKeyHasAnApply`) and
`settingsrows_test.go:426` (`len(want) != len(config.KeyRegistry)`), which item 11 repairs — the split
is stated out loud: this item's targeted acceptance (`go test ./internal/config/`) is green, the two
`cmd/apogee` tests go green at item 11, and `make check` runs at the pair's close, not between them.

**Files:** `internal/config/registry.go`, `internal/config/config.go`, `internal/config/options.go`, `internal/config/defaults/config.yaml`, `internal/config/config_test.go`, `internal/config/registry_test.go`, `internal/config/configwrite_scalar_test.go`, `internal/config/defaults_test.go`

**Tests.** Each key resolves `true` when absent and `false` when written; the round-trip splice test
covers all six.

**Acceptance.** `go test ./internal/config/`

**Commit:** `feat(config): six floor-guard keys, on by default`

## 11. The keys reach the engine and `/settings` live-apply

**What.** Depends on 6, 10. Fold `Options` → `Config.Floor` in both composition roots
(`cmd/apogee/wire_boot.go:273-294`, `wire_firing.go:304-306`); live holder fields, seed, setters and
`optionsLocked` mirror in `wire_settings.go` (`:212, :275, :710-716, :776`); one apply row per key
(`:1231-1247` pattern) all calling ONE engine seam `SetFloor(domain.FloorConfig)` on `settingsEngine`
(`wire.go:301`) with `pendingFloor` replay in `wire_engine.go:166,:363-372`. Tests: `settingsrows_test.go`
golden map `:370-430` (+6 rows, length), `wire_settings_test.go:442-500`, `wire_helpers_test.go:88` spy.
Restate `root.go:117-118` (`--bypass` help): "run with the lab Mechanisms off; Floor guards and
structural reducers stay on (ADR 0071)". `/settings` shows the six as ordinary bool rows in the Session
section (position gives the section; no table edit).

**Regression guard.** One `SetFloor(domain.FloorConfig)` seam needs all six values, but an apply row
knows only its own key and `reachesTheEngine` (`wire_settings.go:1702`) lets `a.live` be nil (tests
build `settingsApplier{engine: spy}`, `wire_settings_test.go:1276`) — a nil holder would send a zero
`FloorConfig` and re-enable the other five, so the item's "other five unchanged" test could not pass.
Mark the six rows `reachesTheEngineAndTheHolder` (`:1707`), or give `settingsEngine` six single-bool
setters on the `SetPruneToolResults` pattern (`wire.go:301`). `settingsEngine` names every engine-facing
type through the root aliases and `apogee.go:106-112` aliases `ContextConfig`/`DelegationConfig` but not
`FloorConfig`: add `type FloorConfig = domain.FloorConfig` there and list `apogee.go` — this item YIELDS
to ADR 0031's engine-sufficient-for-any-Driver invariant
(`docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md`),
which an embedder unable to write a `Config.Floor` literal would breach.

**Files:** `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire.go`, `cmd/apogee/wire_engine.go`, `cmd/apogee/root.go`, `cmd/apogee/settingsrows_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_helpers_test.go`, `apogee.go`

**Tests.** Flipping `read-cache` off in `/settings` reaches `SetFloor` with `DisableReadCache: true`
and the other five unchanged; a firing composed with `tool-result-cap: false` builds a `Config.Floor`
with only that flag set; the `--bypass` help text is pinned.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Settings|Firing|Boot|Root|Help'`

**Commit:** `feat(cmd): floor-guard keys reach the engine and apply live from /settings`

## 12. Retire the three completion nudges and `decompose`

**What.** Depends on 2. Delete `cot.go`, `cot_test.go`, `decompose.go`, `decompose_test.go`, the five
assets `cot-*.txt`, `decompose-*.txt`; move `readSpellings`/`wave4WriteTools` (and `toolSet` if it lives
there) from `decompose.go` into `historyhints.go` so `isFileMutatingTool`/`isReadTool` keep working for
the rows still present; roll entries for `stall_nudge`, `list_nudge`, `tool_use_directive`, `decompose`
(`Release: "v0.20.0"`, no successor). Fix: `prompts_test.go:19-26` (drop the five cases; the test keeps
`library_injection` until item 17), `catalogue_test.go:171,:211-266`, `readlistfamilies_test.go:23`,
`writedetection_test.go`'s decompose uses, `benchreadiness_test.go:74,:409,:416` (arm rewritten in
item 19 — here replace the four IDs with an experimental hook so the file compiles and passes),
`cmd/apogee/schedule_test.go:158`. Update `doc.go`.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `listSpellings` (`decompose.go:156`) and
`hasWrittenFiles` (`:176`) move into `historyhints.go` beside `readSpellings`/`wave4WriteTools`/`toolSet`
— `historyhints.go:33`, `library.go:89`, `filehint.go:64,:74,:111` and `toolfilter.go:60` still call
them. Deleting the `decomposeID` const also breaks `toolfilter.go:24` (`Before: [decomposeID]`, the file
dies at item 14) and `guideddecomposition.go:114` (`IncompatibleWith`, item 13): drop both edges here and
fix `toolfilter_test.go:51` and `guideddecomposition_test.go:60-61`.
`decompose_test.go:353` (`TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin`) is the pin that caught
the 2026-08-10 copy_file/move_file/delete_file gap and `wave4WriteTools` SURVIVES (`robustness.go:99`,
`historyhints.go:127`, plus item 3's copy): move that test with `workspaceWritingBuiltins` /
`writeCapableNonFileBuiltins` into a surviving `internal/mechanisms` test file — it imports
`internal/tools`, which item 3's one-deep-module rule bars from `internal/floor`.
`internal/agent/compact.go:275` leaves this item's Files (item 13 owns it).

**Files:** `internal/mechanisms/{cot.go,cot_test.go,decompose.go,decompose_test.go,historyhints.go,retired.go,prompts_test.go,catalogue_test.go,readlistfamilies_test.go,writedetection_test.go,toolfilter.go,toolfilter_test.go,guideddecomposition.go,guideddecomposition_test.go,doc.go}`, `internal/mechanisms/prompts/{cot-*,decompose-*}.txt`, `benchreadiness_test.go`, `cmd/apogee/schedule_test.go`

**Tests.** `ResolveEnabled({"decompose": true})` yields the retired notice with `v0.20.0`; the package's
remaining tests pass.

**Acceptance.** `go build ./... && go test ./internal/mechanisms/ && go test . -run BenchReadiness`

**Commit:** `refactor(mechanisms): retire stall_nudge, list_nudge, tool_use_directive and decompose`

## 13. Retire `guided_decomposition`

**What.** Depends on 12. Delete `guideddecomposition.go` (+`_test.go`), `internal/agent/guided_decomposition_test.go`,
`cmd/apogee/guided_decomposition_test.go`; roll entry. `Requires`/`IncompatibleWith` and
`ErrMissingRequirement` STAY in `domain`/`catalogue.go` (lab API) — tests that exercised them through
this row move to synthetic rows: `apogee_test.go:222-315` (the `Requires`/clone assertions over a
registered test row via the public query), `example_test.go:208-251` (rewrite both examples over
`CataloguedMechanisms()` without a named ID; expected output adjusted), `enable_mechanisms_test.go:67`,
`benchreadiness_test.go:571` (drop the case; item 19 owns the file's shape). ADR 0014 stays as the record;
`internal/agent/compact.go:275` and any `internal/` comment naming guided decomposition is restated —
rule: `grep -rn "guided.decomposition\|guided_decomposition" internal/ cmd/ *.go` reads zero.
`doc.go` updated.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `internal/agent/guided_decomposition_test.go`
defines `gdConfig`, `gdRequestContains` and `gdDirectiveMarker`, still used by
`internal/agent/deferred_exchange_scope_test.go:30,:42,:84,:88,:99,:143,:147,:160,:207,:210`, and
`rebind_test.go:190` / `enable_mechanisms_subagent_test.go:27` arm the row by ID: add all three files and
RECAST the F6 Deferred-Action Exchange-scope proofs over a synthetic deferring hook rather than deleting
them — `ActionDefer` stays lab API. `apogee_test.go:255-300` needs a CATALOGUED descriptor with both
`Requires` and `IncompatibleWith` non-empty, and `CataloguedMechanisms()` = `mechanisms.Descriptors()`
(`apogee.go:456`) never returns a registry row, so the clone-contract assertion stays in
`internal/mechanisms` over a `registerIn` synthetic row and the root keeps only what a
`Config.Mechanisms` registry can prove (`ErrMissingRequirement` via the `apogee_test.go:210` builder).
The rule's own sites join Files — `internal/title/title.go:739`, `internal/tui/toolregistry.go:1150`,
`internal/agent/delegationtarget.go:81`, `internal/agent/dispatch.go:212`,
`internal/domain/hooks.go:317,:325,:722` — and the grep joins Acceptance.
`internal/agent/compact.go:275` is this item's only compact.go site.

**Files:** `internal/mechanisms/{guideddecomposition.go,guideddecomposition_test.go,retired.go,catalogue_test.go,doc.go}`, `internal/agent/{guided_decomposition_test.go,deferred_exchange_scope_test.go,rebind_test.go,enable_mechanisms_subagent_test.go,enable_mechanisms_test.go,delegationtarget.go,dispatch.go,compact.go}`, `cmd/apogee/guided_decomposition_test.go`, `apogee_test.go`, `example_test.go`, `benchreadiness_test.go`, `internal/title/title.go`, `internal/tui/toolregistry.go`, `internal/domain/hooks.go`

**Tests.** The synthetic-row `Requires` test fails construction with `ErrMissingRequirement` exactly as
before; the recast Exchange-scope proofs pass over the synthetic deferring hook; `go vet` clean.

**Acceptance.** `go build ./... && go test ./internal/mechanisms/ ./internal/agent/ ./internal/tui/ ./internal/title/ ./internal/domain/ . && go test ./cmd/apogee/ -run 'Guided|Delegation'`; `grep -rn "guided.decomposition\|guided_decomposition" internal/ cmd/ *.go` reads zero

**Commit:** `refactor(mechanisms): retire guided_decomposition`

## 14. Retire `filehint`, `read_loop` and `toolfilter`

**What.** Depends on 12. Delete `filehint.go`, `readloop.go`, `toolfilter.go` (+tests),
`readlistfamilies_test.go` bar `TestLibraryObserveShallowExplorationOnCamelCaseList` (item 17 deletes that one with the `library` row), the `read_loop` cases in
`readerror_test.go:93,:130,:148` and `writedetection_test.go:126,:248`; roll entries. Prune
`historyhints.go` of helpers with no remaining caller (`listToolNames`/`isListTool`, `deriveWriteTarget`
and its regexes, `requestContains`, `pathInList`, `writtenPaths`) and
`historyscan.go`'s `readAttemptCounts`; keep what items 15–17 still call (verify by build). `doc.go`.
`intent.go` stays until item 17 (library still calls it). `filehint.go:14`'s `library` import goes with it.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `firstUserContent` (`historyhints.go:110`)
is therefore NOT pruned here: `readrepeat.go:121` still calls it and `read_repeat` retires at item 15,
whose prune list it joins. `go vet` has no unused-identifier check — dead package-level funcs are found
only by golangci-lint's `unused` (`.golangci.yml`, `default: standard`) — so the leftover sweep is
`make lint` (`golangci-lint run ./internal/mechanisms/`), not `go vet`. Keep
`TestLibraryObserveShallowExplorationOnCamelCaseList` (`readlistfamilies_test.go:43`), the only pin on
`libraryListTools`' listFiles/listDir spellings: it needs just `observeResponse`/`closeOnCleanup`/`libFP`
from `library_test.go` and dies with the row at item 17.

**Files:** `internal/mechanisms/{filehint.go,filehint_test.go,readloop.go,readloop_test.go,toolfilter.go,toolfilter_test.go,readlistfamilies_test.go,readerror_test.go,writedetection_test.go,historyhints.go,historyhints_test.go,historyscan.go,historyscan_test.go,retired.go,catalogue_test.go,doc.go}`

**Tests.** Retired notices for the three IDs; `make lint` reports no unused code in
`internal/mechanisms`.

**Acceptance.** `go build ./... && make lint && go test ./internal/mechanisms/`

**Commit:** `refactor(mechanisms): retire filehint, read_loop and toolfilter`

## 15. Retire `truncate_history`, `error_enrichment` and `read_repeat`

**What.** Depends on 14. Delete `truncatehistory.go`, `errorenrich.go`, `readrepeat.go` (+tests),
`historyscan.go`'s `recentSuccessfulReadPaths` and, if nothing else remains, the file; `generalErrorSignals`;
roll entries. Fix `internal/agent/autocompact_guard_test.go:266` (builds `truncate_history` — use a
synthetic `HistoryRewriter`), `writedetection_test.go:39,:57,:74,:109`, `catalogue_test.go:171`.
Prose rule: every comment naming `truncate_history` as the history-rewrite example is restated as
"a lab `HistoryRewriter`" — `grep -rn "truncate_history" internal/ --include=*.go` (`turn.go:225,:248`,
`compact.go:366`, `agent.go:710`, `domain/mechanism.go:66`) reads zero. The `error_enrichment`
CHANGELOG-only follow-on from plan 00 needs no code. `doc.go`.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `internal/mechanisms/historyhints_test.go`
names `errorEnrichmentID` and `readRepeatID` (`:27`, `:29`, `:57`, `:59`, `:80`, `:101`, `:109`), so it
joins this item's Files: `TestHistoryFamilyDescriptorsNonExempt`, `TestReReadFamilyPairwiseIncompatible`
and `TestHistoryFamilyCompatibleMembersCoRegister` lose every subject and go with the rows.
`firstUserContent` (`historyhints.go:110`) joins this item's prune list, its last caller
(`readrepeat.go:121`) dying here. The `truncate_history` rule is scoped to NON-TEST `internal/` files
plus `internal/agent/state_test.go:156`; `internal/tui/transcript_test.go:1067,:1079` and
`internal/config/config_test.go:3184,:3192` are synthetic fixtures that stay exactly as written.

**Files:** `internal/mechanisms/{truncatehistory.go,truncatehistory_test.go,errorenrich.go,errorenrich_test.go,readrepeat.go,readrepeat_test.go,historyscan.go,historyscan_test.go,historyhints.go,historyhints_test.go,writedetection_test.go,catalogue_test.go,retired.go,doc.go}`, `internal/agent/autocompact_guard_test.go`, `internal/agent/state_test.go`, `internal/agent/turn.go`, `internal/agent/compact.go`, `internal/agent/agent.go`, `internal/domain/mechanism.go`

**Tests.** The auto-compact guard test passes with the synthetic rewriter; retired notices for the three IDs.

**Acceptance.** `go build ./... && go test ./internal/mechanisms/ ./internal/agent/ ./internal/domain/`

**Commit:** `refactor(mechanisms): retire truncate_history, error_enrichment and read_repeat`

## 16. Retire `syntax` and `autofix`

**What.** Depends on 15. Delete `syntax.go`, `autofix.go` (+tests), `syntaxID`/`autofixID`
(`robustness.go:32-33`) and the write-payload helpers only they used (`writeToolNames`/`isWriteTool`,
`writePathContent`, `replaceContentArg`); roll entries. `internal/syntaxcheck` STAYS (the structural
trailer from plan 00 uses it); `Deps.WritableBox`/`SecretEnvVars`/`LookPath` (`catalogue.go:20-73`) were
`autofix`'s — remove them and the unconditional lines in `deriveDeps` (`construct.go:398-404`);
`hookExecutionCtx`'s subprocess permit (`hookrun.go:223`) stays for lab hooks. Fix
`internal/agent/wave1delivery_test.go:198,:222` (synthetic response-repair rows), `writedetection_test.go:143,:157`,
`catalogue_test.go:171`. Manual sentence `docs/manual/configuration.md:64-66` ("the `syntax` Mechanism is
only the RETRY half") is item 22's. `doc.go`.

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. `internal/agent/construct_test.go` joins Files:
`:133` reads `deriveDeps(...).SecretEnvVars` and `:161-170` `deriveDeps(...).WritableBox`, both dying
with the fields — delete those two tests, and KEEP `TestHostToolsCarriesSecretEnvVars` (`:73-98`), which
pins the tools' route, not the Mechanism's. `internal/mechanisms/historyhints_test.go:101,:109`
(`TestPostResponseCascadeOrder`) names `syntaxID` and `autofixID`: item 15's guard takes that file with
the history rows, and whatever of it survives is finished here.

**Files:** `internal/mechanisms/{syntax.go,syntax_test.go,autofix.go,autofix_test.go,robustness.go,robustness_test.go,catalogue.go,catalogue_test.go,writedetection_test.go,historyhints_test.go,retired.go,doc.go}`, `internal/agent/construct.go`, `internal/agent/construct_test.go`, `internal/agent/wave1delivery_test.go`

**Tests.** Retired notices; the syntax trailer's tests in `internal/tools` still pass (untouched package).

**Acceptance.** `go build ./... && go test ./internal/mechanisms/ ./internal/agent/ ./internal/syntaxcheck/ ./internal/tools/`

**Commit:** `refactor(mechanisms): retire syntax and autofix`

## 17. Retire `library` and the store it owned

**What.** Depends on 4, 16. Delete `internal/mechanisms/library.go` (+test), the three `library-*.txt`
assets, `intent.go` (+test; its last caller), `prompts_test.go` (no cases left) and, once no row
embeds anything, `internal/mechanisms/prompts.go` and the `prompts/` dir; roll entry. Remove
`Deps.Library`/`Deps.Fingerprint`, `DepNeeds`, `row.needs`, `DepsNeeded` (`catalogue.go:20-33,:75-116,:173-183`);
`deriveDeps`'s library block (`construct.go:405-427`) and `Deps` return; `Agent.library`/`closeLibrary`
(`agent.go:282-289,:505-510`); `Config.LibraryDir` (`domain/config.go:165`, `wire_boot.go:187`,
`wire_firing.go:230`, `stateRoots.library` `wire.go:409`, tests `wire_boot_test.go:879`, `wire_firing_test.go:30`);
`rebind.go:195,:255` comments. Delete `internal/library/store.go`, `entry.go` (+tests) — `fingerprint.go`,
`proberecord.go`, `doc.go` stay (Validated sets, `probe model`); update `internal/library/doc.go`. Tests:
`internal/agent/library_{bypass,lifecycle,corrupt_store}_test.go` deleted, `cmd/apogee/delegation_test.go:458-481`,
`benchreadiness_test.go:440`. `hookForSubAgent`/`SubAgentScoped` stay (lab API).

**Regression guard.** Before deleting a file, grep its package-level identifiers for surviving callers
(`grep -rn '<ident>' internal/ cmd/ --include=*.go`) and move each still-called identifier into a
surviving file in the same commit. (a) `dirPerm`/`filePerm`
(`internal/library/store.go:41-42`) move into `proberecord.go` — its SURVIVING `SaveProbeRecord`
(`:107`, `:115`) is their only remaining consumer — in the same commit that deletes `store.go`, and
`go build ./internal/library/` joins Acceptance. (b) `internal/agent/enable_mechanisms_test.go` joins
Files (it builds the `library` row at `:227-259`, `:264-270` and reads `LibraryDir` at `:230-269`), and
the `Config.LibraryDir` line list is replaced by the RULE
"`grep -rln 'LibraryDir\|\"library\"' --include=*.go .` reads zero outside the deleted files".
(c) `"library"` leaves `benchreadiness_test.go:74`'s `enabledMechanisms` HERE, where the row dies,
rather than waiting for item 19. (d) Depends on 4 and 16 — item 4 is what CREATES
`internal/mechanisms/prompts.go`, which this item deletes.

**Files:** `internal/mechanisms/{library.go,library_test.go,intent.go,intent_test.go,prompts_test.go,prompts.go,readlistfamilies_test.go,catalogue.go,catalogue_test.go,historyhints.go,historyhints_test.go,writedetection_test.go,retired.go,doc.go}`, `internal/mechanisms/prompts/`, `internal/library/{store.go,entry.go,proberecord.go,doc.go}` (+tests), `internal/agent/{construct.go,agent.go,rebind.go,enable_mechanisms_test.go}`, `internal/agent/library_*_test.go`, `internal/domain/config.go`, `cmd/apogee/{wire.go,wire_boot.go,wire_firing.go,delegation_test.go,wire_boot_test.go,wire_firing_test.go}`, `benchreadiness_test.go`

**Tests.** `mechanisms.Build` takes no `Deps` a caller must construct; a config with
`mechanisms: {library: true}` starts with the retired notice; `probe model` and Validated-set identity
tests untouched and green; `SaveProbeRecord` still writes with the rehomed `dirPerm`/`filePerm`.

**Acceptance.** `go build ./... && go build ./internal/library/ && go vet ./... && go test ./internal/mechanisms/ ./internal/library/ ./internal/agent/ . && go test ./cmd/apogee/ -run 'Delegation|Boot|Firing|Probe|Validated'`

**Commit:** `refactor: retire the library Mechanism and the store only it used`

## 18. The gemma Validated set retires; an all-retired entry no longer "applies"

**What.** Recast at the regression check (2026-09-02). Depends on 17. `internal/validated/shipped.json` → `[]` with a trailing note in
`docs/design/archived/mechanism-catalogue.md` (item 20). `shipped_test.go:16-79` → asserts the roster is
empty and that every ID the retired entry named (`autofix … validate`, the 15) is on `mechanisms.RetiredIDs()`.
`DropRetired` (`validate.go:20-44`) that leaves an EMPTY set must not reach `setApplied`
(`checkEntry`'s "empty set" runs only at decode): add `setRetired` to `startupSetDecision`
(`cmd/apogee/validatedsets.go:128-150`) with ONE notice
`apogee: validated-set entry %q names only retired mechanisms and no longer applies; remove it`
instead of per-member lines; `retiredSetMemberNotice` (`:204-208`) keeps its text for partial drops.
`probemodel.go:300-319` returns no effect for `setRetired`. `docs/manual/probe.md:40` and the manual's
Validated-set prose are item 22's.

**Regression guard.** `internal/validated` gains `RetiredEntryKeys = []string{"gemma-4-e4b-it-qat"}`
(a Go const beside `Shipped()`), and `Match` consults it only AFTER the entry lookup misses
(`match.go:74` alias target and `:81` direct label): a retired key with no entry yields
`Decision{Kind: KindRetired}` (new kind) instead of `*DanglingAliasError`/`KindNone`; a live user-dir
entry under that key still applies unchanged (owner, 2026-09-03, ratified via question).
`startupSetDecision` maps `KindRetired` to `setRetired` with the single notice `apogee: validated-set
entry %q was retired in v0.20.0 and no longer applies; remove the alias` — so a config carrying the
alias line apogee itself offered (`validatedsets_test.go:59`) still starts. All four
`TestResolveValidatedSet_*` gemma fixtures (`cmd/apogee/validatedsets_test.go:46`, `:64`, `:87`, `:117`)
are recast over a synthetic user-dir entry via the `writeUserEntry` helper (`:250`), as are
`cmd/apogee/wire_server_test.go`'s `TestRebindSpecForSelectsPerModelBindings` (`:185-200`) and
`TestRebindResolutionKeysOnTheBoundEndpoint` (`:314`, `:351-357`); `cmd/apogee/wire_live_test.go:321-330`'s
silent `t.Skip` on an empty roster likewise becomes a recast over a synthetic entry, and
`internal/config/defaults/config.yaml:865-878`'s two alias examples become a generic placeholder key
naming no shipped entry. This item's `shipped_test.go` rewrite preserves item 2's DropRetired-relaxed
drift tripwire and its un-rolled-removal case, adding only the empty-roster and retired-key pins.
This UPHOLDS ADR 0016's 2026-08-29 amendment (`internal/validated/validate.go:9-15` — a curation change
of ours never costs the user a start) rather than reversing it.

**Files:** `internal/validated/shipped.json`, `internal/validated/shipped.go`, `internal/validated/shipped_test.go`, `internal/validated/match.go`, `internal/validated/match_test.go`, `internal/validated/validate.go`, `internal/validated/validate_test.go`, `cmd/apogee/validatedsets.go`, `cmd/apogee/validatedsets_test.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/probemodel_test.go`, `cmd/apogee/wire_server_test.go`, `cmd/apogee/wire_live_test.go`, `internal/config/defaults/config.yaml`

**Tests.** A user-dir entry naming only retired IDs yields exactly the one notice and enables nothing;
an entry mixing one lab ID and one retired ID keeps the per-member notice and applies the rest;
`probe model` prints no effect line for a fully retired match; a config whose `validated-sets` alias
names `gemma-4-e4b-it-qat` STARTS, drawing the single retired-entry notice and no
`*DanglingAliasError`; a user-dir entry keyed `gemma-4-e4b-it-qat` still applies unchanged; the rebind
fixtures (`wire_server_test.go`, `wire_live_test.go`) resolve their spec from a synthetic user-dir
entry rather than the shipped roster, and the first-run `config.yaml` template's alias examples name no
shipped entry.

**Acceptance.** `go test ./internal/validated/ && go test ./cmd/apogee/ -run 'Validated|Probe|Rebind'`

**Commit:** `feat(validated): the gemma set retires; an all-retired entry no longer applies`

## 19. Empty-catalogue invariants: tests, the doc map and the `/settings` list

**What.** Depends on 8, 13, 17, 18. The shipped catalogue is empty; every test that indexed it or
counted it is restated: `catalogue_test.go` (`:157-195` → "the production catalogue is empty and every
former ID is on the roll"; `:197-268` ordering seeds over synthetic `registerIn` rows; `:295-308` stays),
`benchreadiness_test.go` (`:74` arm and `:650-681` leave-one-out → one registered experimental hook is
the ON arm; leave-one-out drops with a comment naming ADR 0071; `:571-576` keeps the unknown-ID case),
`cmd/apogee/naming_test.go:429,:504`, `wire_firing_test.go:85,:572` (`KnownIDs()[0]` panics — use `nil`
manual sets), `internal/agent/enable_mechanisms_test.go:54` (`(none)` tail), `internal/tui/settings.go:1933-1934`
and `settings_test.go:3837` ("twenty-one") comments. `/settings` `mechanisms` sub-list with zero rows
renders one dim line `no catalogued Mechanisms in this build — the Floor guards are the Session keys`
(`internal/tui/settings.go:1941-1950`). `internal/mechanisms/doc.go` rewritten: the package is the lab
registry — `catalogue.go`, `retired.go`, `historyhints.go` (if any survives), `doc.go`.

**Regression guard.** The dim empty-list line is UNREACHABLE as the item describes it:
`internal/tui/settings.go:561-563` refuses to OPEN the sub-list when `len(m.settingsMechanisms(rows)) == 0`
and `settingsMechanismTarget` (`:1050-1052`) returns `ok=false` on zero toggles, so the render dispatch
at `:1676` never calls `renderSettingsMechanisms`. Relax BOTH zero-length bails as part of this item so
an empty catalogue opens and paints; the key router's `ok=false` fallback (`:422-431`) then stays for the
genuinely-unwired seam only. Add `./internal/agent/` to the Acceptance's `go test` list — this item edits
`internal/agent/enable_mechanisms_test.go` and claims `TestDocMapNamesEveryFile` green there.

**Files:** `internal/mechanisms/catalogue_test.go`, `internal/mechanisms/doc.go`, `benchreadiness_test.go`, `cmd/apogee/naming_test.go`, `cmd/apogee/wire_firing_test.go`, `internal/agent/enable_mechanisms_test.go`, `internal/tui/settings.go`, `internal/tui/settings_test.go`

**Tests.** `TestProductionCatalogueIsEmpty` (also asserts `KnownIDs()` returns an empty, non-nil slice);
the `/settings` empty-list line is pinned by exact string; `TestDocMapNamesEveryFile` green in
`internal/mechanisms`, `internal/floor`, `internal/agent`.

**Acceptance.** `go build ./... && go vet ./... && go test ./internal/mechanisms/ ./internal/floor/ ./internal/agent/ ./internal/tui/ . && go test ./cmd/apogee/ -run 'Naming|Firing'`

**Commit:** `test: pin the empty shipped catalogue and the /settings empty-list line`

## 20. Archive the catalogue with verdicts

**What.** Depends on 1. `git mv docs/design/mechanism-catalogue.md docs/design/archived/mechanism-catalogue.md`;
prepend the archive header the two siblings use (archived 2026-09-02, successor: ADR 0071 and the
**Floor guard** entry in `CONTEXT.md`); add a **Verdicts (2026-09-02)** table above Table A — one row per
Table A ID: `PROMOTED → <key>` (six), `RETIRED — <one-line reason>` (fourteen), `correct_tool_result`
"frozen with the catalogue", `grammar` "retired 2026-08-29"; append the gemma entry's retirement to
§Validated sets. Every live pointer to the old path moves — rule: `grep -rln "design/mechanism-catalogue.md" AGENTS.md CONTEXT.md docs/ --include=*.md --exclude-dir=archived --exclude-dir=plans` reads zero, plus the
code-tree pointers `internal/config/defaults/config.yaml:836` and `internal/agent/selfreg.go:48,:52`.
`docs/design/tool-surface-findings.md` untouched (different register).

**Regression guard.** (a) Relative-link repoints are EXEMPT from the header's "rewriting ADRs"
out-of-scope clause: `docs/adr/0070-off-ramp-mechanisms-ship-on-by-default.md:10` joins Files as a
link-only edit. (b) `README.md` and `ISSUES.md` leave Files — README carries no such pointer
(`README.md:203` is the manual-index row for `configuration.md`), and `ISSUES.md:119` sits inside the
block item 23 deletes. (c) `AGENTS.md:10` names "mechanism catalogue" in PROSE only, so it is a site the
path rule cannot certify and carries its own check `grep -n "mechanism catalogue" AGENTS.md`. (d) The
rule becomes `grep -rln "design/mechanism-catalogue.md" AGENTS.md CONTEXT.md docs/ --include=*.md
--exclude-dir=archived --exclude-dir=plans` reads zero (the plan document itself matches otherwise), and
`internal/config/defaults/config.yaml:836` plus `internal/agent/selfreg.go:48,:52` join Files as the
code-tree pointers — `config.yaml`'s is written into `~/.apogee/config.yaml` on first run.

**Files:** `docs/design/archived/mechanism-catalogue.md`, `AGENTS.md`, `CONTEXT.md` (pointer lines only), `docs/adr/0070-off-ramp-mechanisms-ship-on-by-default.md` (link only), `internal/config/defaults/config.yaml`, `internal/agent/selfreg.go`, any file the grep names

**Tests.** None (docs-only).

**Acceptance.** `test ! -f docs/design/mechanism-catalogue.md && grep -c "PROMOTED\|RETIRED" docs/design/archived/mechanism-catalogue.md` ≥ 20; the grep rule above reads zero; `grep -n "mechanism catalogue" AGENTS.md` reads the reworded prose line.

**Commit:** `docs(design): archive the Mechanism catalogue with the wave's verdicts`

## 21. `CONTEXT.md` — Floor guards enter the language; the nudges leave it

**What.** Depends on 1. Add a **Floor guard** entry under "Safety and autonomy" beside **Bypass mode**
(`:672-684`): the definition from ADR 0071, the six guards with their keys, "on in every arm, not a
Mechanism, emits `FloorGuardEvent`". Rewrite **Bypass mode** (`:674-676`: switches off lab rows only;
the Library sentence goes), **Mechanism** (`:999-1010`: the catalogue is frozen and empty in the shipped
build; the term names the lab surface), **Off-ramp (Exempt Mechanism)** (`:1101-1112` → a one-line
retired-terms pointer to Floor guard), **Library** (`:1114-1127` → Retired terms), **Tool-result
capping** (`:1292-1302` → the guard), **History truncation** (`:1390-1394` → Retired terms; item 15
retires the `truncate_history` it describes), **Guided decomposition / decompose** (`:1396-1416` → Retired
terms), **Validated set** (`:1625-1642`: shipped roster empty), the placement rule (`:1174-1176`: add
"floor-wide and failure-shaped → Floor guard"). Rule for the rest: every sentence naming a retired ID or
"default-off until bench-proven" — `grep -n "stall_nudge\|list_nudge\|tool_use_directive\|decompose\|filehint\|read_loop\|read_repeat\|truncate_history\|toolfilter\|error_enrichment\|autofix\|D1" CONTEXT.md` — is restated or moved to **Retired terms** (`:1654-1673`). Leave `:778` and `:1192` (plan 07's).

**Regression guard.** **Guided decomposition** starts at `CONTEXT.md:1396`, not `:1393`: `:1393-1394`
is the TAIL of **History truncation** (its last definition line plus its `_Avoid_` line), so the range
moved is `:1396-1416` and **History truncation** (`:1390-1394`) is named in its own right. The literal-ID
grep finds no prose site, so the rule gains term-level alternatives —
`History truncation|off-ramp|Off-ramp|Library|syntax` — reaching **History truncation** (which never
spells `truncate_history`), the **Mechanism descriptor** entry's `Capability` line (`:1054`), and
`library`/`syntax`, all absent from the ID alternation. Three live sentences keep treating **Library** as
current after its entry moves — `:1575` and `:1577` (the `ModelFingerprint` ladder) and `:1629`
(`[Library](#self-regulation)` inside **Validated set**, an entry this item already rewrites): restate
them as the FINGERPRINT's own best-available ladder, which the derived call keeps.

**Files:** `CONTEXT.md`

**Tests.** None (docs-only).

**Acceptance.** `grep -n "^\*\*Floor guard\*\*\|Floor guard" CONTEXT.md | head -3` non-empty; the retired-ID grep above, with the term-level alternatives, hits only inside **Retired terms**.

**Commit:** `docs(context): Floor guards enter the language; the nudge Mechanisms retire from it`

## 22. README, AGENTS.md and the manual

**What.** Depends on 1. `AGENTS.md:3` → "Its hard invariant: nothing apogee puts in front of a model
may make that model perform worse than the bare loop — the **Floor guards** and structural reducers
that ship on for every model are held to it, and any model-facing behaviour above them (the
**Mechanism** lab surface; **Bypass mode** is its off-switch) ships off until bench evidence turns it
on." `README.md:29-36` and `:169-173` restated the same way (guards named, "every mechanism is
A/B-tested" → the lab rule). `docs/manual/configuration.md`: `:52-89` becomes a **Floor guards** section
(six keys, default on, on under `--bypass`, the promoted-ID notice) followed by a short `mechanisms:` lab
note (unknown ID still errors; retired IDs noticed); `:64-66`, `:87` ("21") removed; `:169-180` Bypass
paragraph restated. `docs/manual/commands.md:282-289` (the pane's list is empty in a shipped build; the
six guards are Session rows), `headless.md:42`, `daemon.md:80-81`, `probe.md:40` (Library observations
gone), `docs/manual/README.md:11`. Rule: `grep -rn "off-ramp\|counts \*\*21\*\*\|Library observ\|learning \*Library\*" README.md AGENTS.md docs/manual/` reads zero outside the retired-ID notice examples.

**Regression guard.** Every "reads zero" grep in this item is scoped to the files in Files, excludes the
plan document, and matches the DOMAIN term only — `\bLibrary\b` as the store term, never `~/Library` or
other third-party text; the implementer edits no correct text to satisfy a grep. Concretely: the
`Library` alternative narrows to `Library observ\|learning \*Library\*`, because `docs/manual/daemon.md:130`
is macOS's `~/Library/LaunchAgents/com.airiclenz.apogee.daemon.plist`, a correct install path; and
`21 mechanisms` becomes `counts \*\*21\*\*`, because `docs/manual/configuration.md:87` bolds the digits and
the old alternative silently certifies the very line this item removes. KEEP
`configuration.md:64-66`'s always-on syntax-trailer sentence ("every write tool already appends its own
in-process syntax verdict … always on and not configurable"), dropping only its `syntax`-mechanism
clause — the trailer survives the wave (`internal/tools/syntaxtrailer.go`, `CONTEXT.md:1338-1340`).
Acceptance runs `-run 'Docs|ManualLists'`: `-run Docs` alone never reaches
`TestManualListsEveryEnvironmentOverride` (`cmd/apogee/docs_env_test.go:66`), the only test that parses
the `## Environment overrides` section (`configuration.md:142-181`) holding the `:169-180` paragraph this
item rewrites.

**Files:** `AGENTS.md`, `README.md`, `docs/manual/configuration.md`, `docs/manual/commands.md`, `docs/manual/headless.md`, `docs/manual/daemon.md`, `docs/manual/probe.md`, `docs/manual/README.md`

**Tests.** `cmd/apogee/docs_env_test.go` stays green (no env name changes), `TestManualListsEveryEnvironmentOverride` included.

**Acceptance.** `go test ./cmd/apogee/ -run 'Docs|ManualLists'`; the grep rule reads zero over the files in Files.

**Commit:** `docs: Floor guards in README, AGENTS.md and the manual; the nudge catalogue leaves them`

## 23. `ISSUES.md` — the entries the wave closes or reframes

**What.** Depends on 17, 21. REMOVE (closed; the closeout records them in `CHANGELOG.md`): "Phase-4
mechanism catalogue — deliberately dropped / folded / deferred" (`:115-137`, verdicts now in the archived
catalogue), "A door left open by the Mechanism-registration collapse" (`:209-227`, `Deps` is gone), "A
marker phrase in the standing system content suppresses that Mechanism's directive" (`:459-482`, no
shipped row injects). REWORD in place: "Mid-Exchange auto-compaction" `:387-392` (`tool_result_cap` →
the tool-result-cap guard, always on), "Adaptive prompt complexity" `:426-436` ("a Mechanism by
definition" → "a lab row or, if floor-wide, a Floor guard needing an ADR 0071 verdict"), "schedule
tool" `:596-599`, "B1 auto-attach" `:715-720` (the Mechanism-in-ADR-0003-sense clause → lab row),
"`Request.InjectContext` placement" (`:224`).
Untouched: the headless `--bypass` gap (`:799-803`; Bypass still exists). Convention check: no closed
narration remains — `grep -n "retired\|Floor guard" ISSUES.md` shows only the reworded live entries.

**Regression guard.** `### Request.InjectContext placement` (`ISSUES.md:224`) is a LIVE entry the wave
falsifies and joins the REWORD list: its blast radius (`:284-289`) cites `internal/mechanisms/readloop.go:89`,
`filehint.go:111` and `guideddecomposition.go:159`, plus `guideddecomposition.go:175,:196,:373,:516`, all
deleted by items 13 and 14 — "Five non-test callers" becomes two. Extend the Mid-Exchange auto-compaction
reword to the same paragraph's next clause (`:381-383`): "guided decomposition covers it with a descriptor
`Requires` on `tool_result_cap`" is false after item 13, and the convention grep (`retired|Floor guard`)
cannot find it — drop the coverage clause.

**Files:** `ISSUES.md`

**Tests.** None (docs-only).

**Acceptance.** `grep -c "Phase-4 mechanism catalogue\|door left open\|marker phrase" ISSUES.md` = 0; `grep -c "guided decomposition covers it" ISSUES.md` = 0.

**Commit:** `docs(issues): close the entries the Mechanism retirement wave resolves`

---

**Suggested version bump:** minor (`v0.20.0`) — six default-on behaviours change for every session (they
now run under Bypass), fourteen config-addressable IDs retire, six keys appear. The roll's `Release`
string for this wave is written as `v0.20.0`; if the owner cuts a different number, the bump commit
corrects the string.
