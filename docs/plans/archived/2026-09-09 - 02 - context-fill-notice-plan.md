# Context-fill notice — the first engine advise Reaction

**Goal.** Ship `context-fill-notice`: an engine-origin `advise` Reaction on `post-tool-result` that tells the model, as the advise trailer on the closing tool result, how far its conversation has climbed toward the automatic Compaction line, at fixed rungs 50 / 75 / 90, with tokens used and the window; a child agent's 90 rung adds one wrap-up sentence. Off by default behind one top-level boolean, off under Bypass, silent when no window is known. It rides the advise slot stage 3 shipped (`803c865e`, `2d95419f`: `Message.WithAdvice`, `RenderAdvice`, the record strip) — nothing new in the slot.

**Date:** 2026-09-09, re-written 2026-09-13 · **Status:** unexecuted · **sized for:** ~200k-context host · **Base:** `a1ce9252` · **Bead:** `apogee-4kb` (its dependency `apogee-rxj`'s plan is archived; the slot is in the tree)

**Sources.** `docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md` (every decision) · `docs/adr/0076-…` D2/D6/D7/D9/D10 · `CONTEXT.md` §Reactions and Moments (**Context-fill notice**, **Reaction**, **Floor guard**) and §Context and history (**Budget**) · `internal/domain/advice.go` (the shipped slot: `AdviceSpan`, `RenderAdvice`, `CapAdvice`) · `internal/domain/budget.go` (`HistoryExceedsAllocation`, the compare to match) · `internal/agent/compact.go:261-283` (trigger and unknown-window posture) · `internal/agent/reactions.go:295,310-313,451-475` (Bypass guard, advice collection, `isAdvice`/`adviceOf`) · `internal/agent/builtins.go` (builtin idiom) · `docs/design/test-drivers.md`.

**Ratified design calls** (owner, 2026-09-09 grill; do not re-ask):
- **Cell:** engine-origin `advise`, `post-tool-result`, inherited by children, off under Bypass, **default off**; not a Floor guard.
- **Key:** top-level file-only boolean `context-fill-notice: false`, registered after `read-cache` (Session section), live-editable in `/settings`; the manual entry says it is not a Floor guard and why it defaults off. ADR 0076 D10 amended (recorded).
- **Scale:** percent of the compaction line (the Budget's History allocation) through the trigger's own estimate; never the window. Unknown window (`History <= 0`) ⇒ silent, no substitution.
- **Line:** `context: N% of the way to automatic compaction — Xk tokens used of a Yk window`; tokens = the same estimate the percent is computed from; window = `Budget.Window` (`Budget.ContextLimit` when `Window <= 0`).
- **Rungs:** fixed 50 / 75 / 90, at most one notice per tool result (the highest rung reached, reporting the actual percent), re-armed when the fill drops back under a fired rung. 50/75 fact only; 90 adds one engine-authored wrap-up sentence for a child (`Depth() > 0`) only.
- **Slot (writer, from the tree 2026-09-13):** the notice is a builtin of class `advise` whose handler returns `Outcome{Inject: text}`; `fireLeg`'s `adviceOf` fences it as `[advice — reaction context-fill-notice (engine origin) at post-tool-result, turn N]` … `[end advice — context-fill-notice]`, caps at `AdviceCap`, and `appendToolResult` lands it through `Message.WithAdvice`; the record strip (`Message.MarshalJSON` → `recordContent`) already keeps it out of a resumed session.
- **Switch plumbing (writer, from the tree):** the boolean is `domain.Config.ContextFillNotice` and a fifth `Generation` member after `Sync`, mirroring `Bypass` — never a `FloorConfig` field; `bypassSkips` also skips a **builtin** of class `advise` (Floor guards are `shape (view)` and stay).
- **Events:** the existing `ReactionFiredEvent` keyed `context-fill-notice`, action `notice`, `Detail` = `rung 75 (78%)` — `fireLeg` copies the advice text into `Detail` only when the handler left it empty; no new headless line kind.

**Standing requirements.**
- `skills: coding-standards`.
- Wording ships as prompt assets under `internal/agent/prompts/` via `mustPrompt`, never Go literals; every new `.go` file is named in its package's `doc.go` (docmap gates).
- Authorized deviations land as a dated `NOTES (YYYY-MM-DD):` line under the item; CHANGELOG entries travel in sidecars.

**Out of scope.** Changes to the shipped advise slot (fence text, cap, record strip) · a decode-time strip of a hand-edited snapshot · the apogee-sim bench arm that could flip the default on (`apogee-sbj`) · `/settings` "what the model saw this Turn" ledger view · mid-Exchange auto-compaction for the main loop (`apogee-1bf`) · any `TopLevelOnly` semantics for builtins · VERSION and CHANGELOG headings.

**Regression check (2026-09-13, a1ce9252):** 1: yields to ADR 0017 §4 — `ConversationView` gains no method; a free `ConversationChars(conv)` in budget.go carries the measure; 1: What recast to match · 2: guard folded (writer's decision on the bind-time replay) · 3: guard folded (`agent.go` in Files; `32.8k`/`8.2k` pinned; the trigger boundary observed through one result crossing 90 and 100 together) · 5: guard folded (both Driver-built `Generation` literals threaded here) · 7: guard folded (fixtures written into the workspace at test time; no `testdata/` files) · 8: guard folded (link targets the `### context-fill-notice` heading). Items 4 and 6 SAFE.

## 1. The fill measure: `Budget.HistoryFill` and `ConversationChars` — ✅ DONE (2026-09-13)

**What:** In `internal/domain/budget.go`, `func (b Budget) HistoryFill(chars int) float64` returns `EstimateTokens(chars) / History` and **0** when `History <= 0` or `CharsPerToken <= 0` (inert on an unknown window — the same clauses `HistoryExceedsFraction` has). In the same file, `func ConversationChars(conv ConversationView) int` sums `Content` and each `ToolCall`'s `Tool` + `Arguments` through `Range` — the same number as `PromptChars(msgs, nil)`; `ConversationView` (`internal/domain/hooks.go:353-366`) gains no method and `hookview.go` / `domaintest.FakeLoopView` are untouched. The invariant to pin: for any conv over msgs, `HistoryExceedsAllocation(msgs) == (HistoryFill(ConversationChars(conv)) > 1.0)` — the notice and the fold read one scale.

**Regression guard.** Yields to ADR 0017 §4 (`docs/adr/0017-the-exchange-is-a-derived-domain-working-value.md:102-106`): the public `LoopView` / `ConversationView` interfaces "gain **no** methods" — unsealed, externally implementable, an added method is a breaking change — so the measure is a free function over the read surface.

**Files:** `internal/domain/budget.go`, `internal/domain/budget_test.go`

**Tests:** table over `{History, CharsPerToken, chars}` including the zero-History and zero-ratio rows (fill 0); the equivalence property above at chars one under, at, and one over the History boundary (`EstimateTokens` ceils, so the equality must hold exactly at the boundary too); `ConversationChars(conv)` equals `PromptChars(msgs, nil)` over a `NewRequest(…).View().Conversation()` built from messages with tool calls and results (`domaintest.FakeLoopView.Conversation`'s shape, `domaintest.go:165`).

**Acceptance:**
```
go build ./... && go vet ./internal/domain/
go test ./internal/domain/ -run 'HistoryFill|HistoryExceeds|PromptChars|ConversationChars'
```

**Commit:** `feat(domain): Budget.HistoryFill and ConversationChars read the compaction trigger's scale`

## 2. The switch: `Config.ContextFillNotice`, a fifth `Generation` member, Bypass for a builtin advise — ✅ DONE (2026-09-13)

NOTES (2026-09-13): the notice-only rebuild proof is a sibling test (`TestSetReactionsRebuildsTheLadderWhenOnlyTheNoticeSwitchMoves`) rather than an extension of `TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves`, whose name would have become false; that test's doc comment now names both enable-set inputs
NOTES (2026-09-13): consequential edit — CONTEXT.md: made necessary by the fifth `Generation` member — the `Generation` entry's `{Floor, Bypass, Observe, Sync}` member list and the "agent takes Floor, Bypass and Sync" sentence now name `ContextFillNotice`
NOTES (2026-09-13): the child inheritance test lives in `subagent_test.go` (`TestSubAgent_ChildInheritsTheLiveNoticeSwitch`) in the shape of `TestFloorGuard_ChildInheritsTheLiveFloor`, flipping the switch ON then OFF through `SetReactions`; `gofmt` re-aligned three neighbouring trailing comments in `construct.go` when the `gen:` literal grew

**What:** `domain.Config` (`internal/domain/config.go`, beside `Bypass` `:68`) gains `ContextFillNotice bool` (doc: ADR 0077 D1/D2, not a Floor guard); `domain.Generation` (`internal/domain/reaction.go:526-539`) gains `ContextFillNotice bool` as its fifth member after `Sync` (`Validate` inspects only the lanes — unchanged). `Agent.SetReactions` (`internal/agent/agent.go:1054-1061`) rebuilds the builtin ladder when `gen.Floor` **or** `gen.ContextFillNotice` moved and copies the new member beside Floor/Bypass/Sync; `construct.go:89` seeds it from `cfg` and `:113` passes it. `buildBuiltins(gates domain.FloorConfig)` becomes `buildBuiltins(gates domain.FloorConfig, notice bool)` — item 3 appends the reaction; here the parameter is threaded and unused. `fireLeg`'s guard at `reactions.go:295` (`!builtin && a.bypassSkips(r.spec)`) becomes "skip when `bypassSkips` says so, where a builtin is skipped only if its class is `advise`" — the seven `shape (view)` guards keep firing under Bypass exactly as today. `newChildAgentOn` (`subagent.go:523-525`) forwards `childCfg.ContextFillNotice = gen.ContextFillNotice` from the LIVE Generation beside `Bypass`/`Floor`, so a switch flipped through `SetReactions` reaches a child spawned after it. Rewrite the "never switched off by Bypass" wording in `builtins.go:13-28` and the `isAdvice` doc ("A builtin never satisfies it", `reactions.go:451`) to name the one builtin advise exception (ADR 0077); `bypassSkips`' own doc says a builtin of class advise is skipped too.

**Regression guard.** the bind-time Generation replay zeroing the seed before item 6 lands is accepted — the key does not exist until item 5, so the seed is false in every commit before item 6 anyway (writer's decision, no NOTES line needed)

**Files:** `internal/domain/config.go`, `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/builtins.go`, `internal/agent/reactions.go`, `internal/agent/reactions_test.go`, `internal/agent/setlive_test.go`, `internal/agent/subagent.go`, `internal/agent/subagent_test.go`

**Tests:** `SetReactions` with only `ContextFillNotice` changed rebuilds the ladder and with nothing changed does not (extend `TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves`, `reactions_test.go:682`, with a spy or the builtin count); a fake **builtin** of class `advise` on the builtin leg is skipped under Bypass while a Floor guard is not (a new `TestFireBypassMatrix` case beside `:364`'s "builtin shape-view never skipped"); the child test flips the switch ON via `SetReactions` (construction `cfg` off) and asserts a child spawned after carries it, then OFF and asserts it does not (shape: `TestSetReactionsReachesAChildSpawnedAfterTheSwap`, `setreactions_test.go:117`); every existing Floor/Bypass fieldwise comparison (`setlive_test.go:155`, `floorguards_test.go:233`) stays green with the fifth member zero.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/ ./internal/agent/
go test ./internal/domain/ ./internal/agent/ -run 'Generation|SetReactions|Bypass|Child|Builtin'
```

**Commit:** `feat(agent): Generation carries the context-fill-notice switch and Bypass reaches a builtin advise reaction`

## 3. The `context-fill-notice` reaction — ✅ DONE (2026-09-13)
NOTES (2026-09-13): the driven tests script `toolCallScript` (no usage report) rather than `usageToolCallScript`, so the chars→token ratio stays at the uncalibrated 4.0 and the rung percentages are deterministic — a usage report would calibrate the ratio against the whole prompt and move the line under the pinned numbers.
NOTES (2026-09-13): `TestSetReactionsRebuildsTheLadderWhenOnlyTheNoticeSwitchMoves` (reactions_test.go, item 2) asserted the switch-on ladder is seven "the notice is not yet in the tree" — updated to eight with the notice last, the state this item makes true; the switch-off seven-guards test is unchanged.
NOTES (2026-09-13): the child wrap-up sentence is joined to the fact line with a newline inside the one fence (the plan says "append" without naming the join), so the fact line is byte-identical on main and child.
NOTES (2026-09-13): the re-arm is the plan's literal algorithm — a drop re-arms only the rungs the fill fell UNDER: after a fold that lands the next result at 52%, the 50 rung stays "fired" and 75 is next; the fold test therefore lands one under-50 result before the 50 rung fires again.
NOTES (2026-09-13): fix round — the line above is superseded: a fold that ran (`Agent.fold`, the one call `Compact`, `autoCompact` and `emergencyFold` share), `/clear` and `RestoreSession` now re-arm the whole ladder (`rearmFillNotice`), so the first post-fold result fires whichever rung its fill reaches (50 at 50–74, 75 and never 50 at 75–89), pinned by `TestContextFillNoticeFirstPostFoldResultFiresItsOwnRung`; the handler's silent drop branch now covers only the estimate moving (ratio recalibration) between folds. ADR 0077 D4, CONTEXT.md, the manual and the CHANGELOG entry restated.
NOTES (2026-09-13): fix round 2 — `AbortExchange` (Esc in the TUI) drops the tool result a notice rode on yet kept the rung marked fired; it now calls the same `rearmFillNotice` the fold, `/clear` and `RestoreSession` paths use, pinned by `TestContextFillNoticeReArmsAfterAnAbortedExchange` (a fired 50, an abort, a result back at 50–74% fires 50 again once); the handler's "only grows between folds" comment restated; the CHANGELOG entry extended in place to name the abort path. The cancelled-Turn rollback (`turn.go` endCancelled row, re-attempted on resume by a Step-driven host) still keeps the ladder — deferred.
NOTES (2026-09-13): fix round 3 — the cancelled-Turn rollback now re-arms too: `turnLifecycle` gained an `onRollback func()` seam (the `onClose` shape), fired by end()'s endCancelled row after its `DropRange` and wired in construct.go to the same `rearmFillNotice` (idempotent, so a rollback re-attempted on resume re-arms harmlessly); pinned by `TestContextFillNoticeReArmsAfterACancelledTurnRollsBack` (a fired 50, a cancel mid-Turn that rolls the result back, the resumed re-attempt back at 50–74% fires 50 again once); the `fillRung` field, `rearmFillNotice` doc, handler comments and the CHANGELOG entry restated in place.

**What:** Depends on items 1–2. New `internal/agent/fillnotice.go`: `const contextFillNoticeID = "context-fill-notice"`, `actionNotice = "notice"`, `var fillRungs = [3]int{50, 75, 90}`; `Agent` field `fillRung int` (highest rung fired on the current climb, 0 = none); handler `(a *Agent) contextFillNotice(ctx, view domain.LoopView, call domain.ToolCall, result *domain.ToolResultEdit) (domain.Outcome, error)` as a `domain.PostToolResultFunc`: `chars := domain.ConversationChars(view.Conversation()) + len(result.Content())`, `fill := view.Budget().HistoryFill(chars)`; fill 0 ⇒ zero Outcome (unknown window, silent). `pct := int(fill * 100)`; `reached` = highest rung `<= pct`; if `reached < a.fillRung` set `a.fillRung = reached` (re-arm) and return; if `reached > a.fillRung`: set `a.fillRung = reached`, render the fact line from `prompts/context-fill-notice.txt` (`fmt` verbs: `%d` pct, `%s` tokens, `%s` window; tokens = `view.Budget().EstimateTokens(chars)`, window = `Budget.Window`, or `Budget.ContextLimit` when `Window <= 0` — never `0`; both through one `formatTokens(n int) string` in this file — `999` → `999`, `15400` → `15.4k`, `131072` → `131k`), append the child sentence from `prompts/context-fill-wrap-up.txt` when `reached == 90 && view.Depth() > 0`, and return `Outcome{Inject: text, Detail: fmt.Sprintf("rung %d (%d%%)", reached, pct)}` — the slot fences and lands it. `reactions.go:312` (`out.Detail = adv.text`) becomes conditional on `out.Detail == ""` so a handler-set Detail survives (every user `advise:` entry leaves it empty — unchanged). `buildBuiltins` appends the reaction **after** the seven guards when `notice` is true, through a class-taking sibling of `engineBuiltin` (`builtins.go:85` hardcodes `ClassShapeView`) with `Class: domain.ClassAdvise`. `armReactions`' reserved set (`reactions.go:744-766`) becomes `guardIDs` plus `contextFillNoticeID`; `guardIDs` stays seven. Fact line asset: `context: %d%% of the way to automatic compaction — %s tokens used of a %s window`. Wrap-up asset: `Your context is nearly full. Stop working now: make your next reply the final report — what you finished, what is left, and where — so your parent can carry on.`

**Regression guard.** `TestBuiltinReactionsAreTheSevenFloorGuardsWhenEveryGuardIsOn` (`reactions_test.go:582`) asserts every builtin is `ClassShapeView`: it stays as the switch-off case (seven, all shape-view); a sibling asserts the switch-on ladder is eight with the last `ClassAdvise`, `OriginEngine`, `On == {post-tool-result}`. A `working-window` with no advertised window (`loop.go`, grep `WorkingWindow`) yields `History > 0` with `Budget.Window == 0` — pinned by the `MaxContextTokens = 0, WorkingWindow = 32768` row. `fillRung` lives in `type Agent struct` (`agent.go:57`, beside `compactSat`/`compactFailed` `:348-349`) — `agent.go` is in Files. `formatTokens` keeps one decimal below 100k, so 32768 renders `32.8k` and 8192 `8.2k`; no row expects `32k`. Rung 90 fires once per climb reporting the fill at that moment, and later results or the final text-only reply (no post-tool-result Moment) can carry the history past 100 with no further notice — so the trigger boundary is observed only through one result that crosses 90 and 100 together. Item 1 yields to ADR 0017 §4: `chars` is `domain.ConversationChars(view.Conversation()) + len(result.Content())` — the view has no `PromptChars()` method.

**Files:** `internal/agent/fillnotice.go`, `internal/agent/fillnotice_test.go`, `internal/agent/prompts/context-fill-notice.txt`, `internal/agent/prompts/context-fill-wrap-up.txt`, `internal/agent/agent.go`, `internal/agent/builtins.go`, `internal/agent/reactions.go`, `internal/agent/reactions_test.go`, `internal/agent/doc.go`

**Tests:** drive an agent (`usageToolCallScript` responders, `usagetally_test.go:28`; `cfg.Context.MaxContextTokens = 8192`, switch on) through tool results sized to cross 50, then 75, then 90: exactly three notices, each once, as the trailer on the closing tool result (`msg.Advice` carries one `AdviceSpan{Reaction: "context-fill-notice", Origin: engine, Moment: post-tool-result}`, content ends with the rendered line inside the shipped fence); a single result jumping 40→80 fires one notice at rung 75 reporting the actual percent; after a fold (`scriptedCompactResponder`, `CompactionEnabled`) the fill drops and the next climb fires 50 again; `MaxContextTokens = 0` ⇒ no notice and no firing; `MaxContextTokens = 0, WorkingWindow = 32768` ⇒ the window reads `32.8k` (the 8192 window of the main driven test reads `8.2k`); child (`Depth() > 0`) 90 carries the wrap-up sentence, main does not; switch off ⇒ no builtin registered; Bypass on ⇒ no notice; `ReactionFiredEvent{Reaction: "context-fill-notice", Action: "notice", Detail: "rung 50 (52%)"}` on the sink; one tool result that crosses 90 and 100 together fires exactly one notice reporting a percent `>= 100`, and `historyExceedsAllocation` (`compact.go:277`) answers true at the next Turn boundary; `formatTokens` table; a snapshot → `RestoreSession` round-trip carries no notice text (`TestAdviseNeverSurvivesASnapshotResume` shape, `advise_test.go:249`).

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'FillNotice|ContextFill|Builtin|Advise|Prompt|DocMap'
```

**Commit:** `feat(agent): the context-fill-notice reaction — three rungs of the compaction line on the closing tool result`

## 4. Forgery closure: a context file cannot carry the advice fence — ✅ DONE (2026-09-13)

NOTES (2026-09-13): consequential edit — CONTEXT.md: made necessary by the context-file guard now fencing the advice fence — the **Advice span** entry's "nothing out-of-process can forge an engine header" sentence gained the context-file half of that posture, matching how the Delegate report block and Task list entries record theirs.

**What:** `internal/domain/advice.go` exports the fence's fixed prefixes as consts (`AdviceFencePrefix = "[advice — reaction "`, `AdviceFenceClosePrefix = "[end advice — "`) and `RenderAdvice` builds from them (byte-identical output — `TestAdviceRenderIsByteExact` stays untouched). `forgesStandingStructure` (`internal/agent/contextfiles.go:187-194`) refuses a context file whose trimmed line starts with either prefix, exactly as it refuses `delegateReportFence`.

**Files:** `internal/domain/advice.go`, `internal/agent/contextfiles.go`, `internal/agent/contextfiles_test.go`

**Tests:** a context file containing a line `[advice — reaction lint (user origin) at post-tool-result, turn 1]` and one containing `[end advice — lint]` are each refused with the same outcome as the delegate-report forgery test (`contextfiles_test.go:530-546`); a file mentioning `advice` in prose is still admitted; `go test ./internal/domain/ -run Advice` unchanged and green.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/ ./internal/agent/
go test ./internal/domain/ -run Advice && go test ./internal/agent/ -run 'Forge|ContextFile'
```

**Commit:** `fix(agent): a context file cannot forge the advice fence`

## 5. The `context-fill-notice:` config key — ✅ DONE (2026-09-13)

**What:** Depends on item 2. `internal/config/config.go`: `fileConfig` gains `ContextFillNotice *bool \`yaml:"context-fill-notice"\`` in its **own** comment block directly after the Floor block (`:1317-1339` stays "the seven"); `keyAccessors` gains a row in the default-**false** shape of `remember-model` (`:749-756`); `internal/config/options.go` gains `ContextFillNotice bool` after `ReadCache` (`:297`), outside the Floor comment. `internal/config/registry.go`: row `{Path: "context-fill-notice", Kind: KindBool, Default: "false", Editable: false, Desc: "Tell the model how close it is to automatic compaction (50/75/90) on its tool results. Not a Floor guard: off until bench evidence turns it on.", Read: …}` inserted **directly after `read-cache`** (`:445-450`, before `delegate-max-steps`) so it lands in the Session run (`settingsrows.go:81-92`); item 6 flips `Editable: true` with the applier. `internal/config/reactions.go`: a `reactions:` entry with `id: context-fill-notice` is refused in `entryReactions` (`:86-89`) with `that is the built-in engine reaction context-fill-notice: — set the top-level key, not a reactions: entry` (a second literal beside `floorGuardKeys`, which stays seven). `internal/config/defaults/config.yaml`: after `tool-call-salvage: true` (`:675`) a short second-person paragraph (what it tells the model, that it is not a Floor guard, why it ships off, how to turn it on) then the active line `context-fill-notice: false`; the `reactions:` header's refusal sentence (`:471`) names the notice's id too. Wire the value into the `apogee.Config` literals beside `Floor: floorFromOptions(...)` at `cmd/apogee/wire_boot.go:361` and `wire_firing.go:381` (headless composes through `firingConfig`); item 6 owns the live path. Docs gate: `docs/manual/configuration.md` gains the `### context-fill-notice` subsection after the Floor prose (`:67-75`) — the yaml one-liner, what the model receives (the line and the rungs, the child wrap-up at 90), that it is **not** a Floor guard, why it defaults off (ADR 0077), that Bypass turns it off, that it is silent until a window is known; `cmd/apogee/settingsrows_test.go` gains the `want` map entry and the `fabricatedSettings()` field.

**Regression guard.** `TestRegistryIsBijectionWithFileConfig`, `TestKeyAccessorsBindDescribedKeys` and `TestManualDocumentsEverySettingsKey` demand field, accessor, row and manual mention in one commit — all land here. `TestSettingsTableIsInRegistryOrder` is not touched: no `settingsTable` row exists until item 6, and `TestEveryEditableSettingKeyHasAnApply` sees `Editable: false`. At this commit a TUI session with `context-fill-notice: true` would otherwise run WITHOUT the notice while the manual subsection landing here announces the key as working: the bind-time seed `apogee.Generation{Floor: w.cfg.Floor, Bypass: w.cfg.Bypass, …}` (`cmd/apogee/wire_live.go:204-209`) and the settings host's literal (`wire_settings.go:292-297`) carry no `ContextFillNotice`, and `lateEngine.SetReactions` (`wire_engine.go:483-497`, item 2 copies the member) replays it over the `wire_boot.go:361` seed as false. BINDING: thread `ContextFillNotice: w.cfg.ContextFillNotice` (`wire_live.go`) and `ContextFillNotice: opts.ContextFillNotice` (`wire_settings.go:292-297`) in this item; item 6's BINDING sentence is then already satisfied and item 6 keeps the table row, applier and `optionsLocked` (headless/daemon compose through `firingConfig` and need nothing more).

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/options.go`, `internal/config/registry.go`, `internal/config/registry_test.go`, `internal/config/reactions.go`, `internal/config/reactions_test.go`, `internal/config/defaults/config.yaml`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_boot_test.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/settingsrows_test.go`, `docs/manual/configuration.md`

**Tests:** `TestEveryConfigKeyReachesTheOptions` `want` map + `everyKeyFileConfig` (non-default = `true`) updated; `TestApplyConfigContextFillNotice` in the shape of `TestApplyConfigRememberModel` (absent ⇒ false, `true` ⇒ true, `false` ⇒ false); `TestApplyConfigFloorGuardKeys` unchanged; registry bijection/invariant/projection tests pass; the refusal sentence pinned whole in `reactions_test.go` beside the Floor one (`:283`), `len(floorGuardKeys) == 7` still asserted (`:330`); the embedded template parses with the key false; boot and firing wiring tests (`TestBootConfigCarriesTheFloorGuardKeys`, `TestFiringConfigCarriesTheFloorGuardKeys`) assert `Config.ContextFillNotice` follows the option; `TestManualDocumentsEverySettingsKey`, `TestSettingsRowsFormatEffectiveValues`, `TestSettingsRowsCarryTheirSection` (Session) pass; the settings host seeded from `config.Options{ContextFillNotice: true}` holds a `gen` with the member set (a case beside `TestLiveSettingsGenerationClonesBothLanes`, `wire_settings_test.go:3295`).

**Acceptance:**
```
go build ./... && go vet ./internal/config/ ./cmd/apogee/
go test ./internal/config/ && go test ./cmd/apogee/ -run 'Boot|Firing|SettingsRows|Manual|Docs'
```

**Commit:** `feat(config): the context-fill-notice key — file-only, default off, refused as a reactions: id`

NOTES (2026-09-09): for the one commit between items 5 and 6 the row is `Editable: false` and so `externallyEdited` (`cmd/apogee/settingsrows.go`): `/settings` paints it with the `⏎ opens $EDITOR` pointer until item 6 flips it.
NOTES (2026-09-13): the manual subsection sits directly after the "What runs above the floor" paragraph (the Floor prose's closing bridge), and that paragraph's "and nothing else in this release" was amended to name the notice — the sentence became false once an engine builtin ran above the floor.
NOTES (2026-09-13): the refused id is a second literal `contextFillNoticeKey` (a const beside `floorGuardKeys`, which stays seven); `TestFloorGuardKeysAreRegistryKeys` also pins it as a registry key kept out of the seven.
NOTES (2026-09-13): the new boot/firing/settings-host tests are separate `…CarriesTheContextFillNotice` cases (both values of the switch) rather than edits to the Floor tests, whose one-key-off literals stay exact.

## 6. Live toggle in `/settings` and the settings rows — ✅ DONE (2026-09-13)

NOTES (2026-09-13): `cmd/apogee/wire_live.go` unchanged — item 5 already threaded `ContextFillNotice` through both Driver-built `apogee.Generation` literals (`wire_live.go:204-209`, `wire_settings.go:292-297`); the BINDING requirement was verified in place rather than re-applied.
NOTES (2026-09-13): the boot test (`TestWireSessionBindsTheContextFillNoticeOntoTheAgent`) reads the Agent `wireSession`'s own startup bind constructs instead of calling `Bind` a second time — `wireSession` binds the startup server through the real `serverBinder`, so a second `Bind` is refused as "already has a server".
NOTES (2026-09-13): consequential edit — docs/manual/commands.md: made necessary by the row turning editable; the `/settings` paragraph that enumerates the Session section's live bool rows now names `context-fill-notice` beside the seven Floor guards.
NOTES (2026-09-13): consequential edit — docs/manual/configuration.md: made necessary by the row turning editable; the key's entry said "file-only" alone and now says the `/settings` row switches it live.

**What:** Depends on item 5. `cmd/apogee/wire_settings.go`: a `setContextFillNotice(on bool) apogee.Generation` beside `setFloorGuard` (`:780-802`, which stays a seven-key switch) returning `s.generationLocked()` with the fifth member set; an `applyContextFillNotice` mirroring `applyFloorGuard` (`:1908-1914`); its `settingsTable` row `{key: "context-fill-notice", reaches: reachesTheEngineAndTheHolder, apply: applyContextFillNotice}` sits directly after the `read-cache` arm (`:1435-1439`) and before `delegate-max-steps` — `TestSettingsTableIsInRegistryOrder` (`wire_settings_test.go:633`) fatals otherwise; the live-options projection `optionsLocked` (`:896-925`) copies the member back to `next.ContextFillNotice`; the registry row flips to `Editable: true` in this same commit (`TestEveryEditableSettingKeyHasAnApply`, `:669`). BINDING: thread `ContextFillNotice: w.cfg.ContextFillNotice` / `opts.ContextFillNotice` through both Driver-built `apogee.Generation` literals — `cmd/apogee/wire_live.go:204-209` and `wire_settings.go:292-297` — because the TUI applies a Generation at Bind that would otherwise overwrite the construct-time seed with false. `lateEngine.SetReactions` (`wire_engine.go:483-497`) compares only `Observe` for the Runner and hands the whole Generation to the agent — unchanged. `layout.md` enumerates no keys — nothing to edit.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_engine_test.go`, `cmd/apogee/wire_boot_test.go`, `internal/config/registry.go`

**Tests:** `TestContextFillNoticeRowAppliesOneGeneration` in the shape of `TestBypassRowAppliesOneGeneration` — toggling the row swaps one Generation whose Floor, Bypass and Sync are unchanged; `TestEveryEditableSettingKeyHasAnApply`, `TestApplySettingRefusesEveryKeyItCannotReach`, `TestSettingsTableIsInRegistryOrder` pass; `TestLiveSettingsOptionsFollowEveryApply` (`:906`) gains the key; a boot test in the `wire_engine_test.go` / `wire_boot_test.go` shape: a home with `context-fill-notice: true` yields an engine Generation whose `ContextFillNotice` is true after Bind; the `t16-settings-rows` golden is untouched (it captures the pane top) — never `-update` it in this item.

**Acceptance:**
```
go build ./... && go vet ./cmd/apogee/
go test ./cmd/apogee/ -run 'Settings|Generation|Apply|Rows|Boot|Engine'
```

**Commit:** `feat(settings): context-fill-notice is a live on/off row in the Session section`

## 7. Announced-surface journey: the exact line the model sees — ✅ DONE (2026-09-13)

NOTES (2026-09-13): the journeys run through `headlessFillNotice`, a twin of `headlessHooksIn` in the new file that seeds the workspace between creating it and running — the plan's BINDING sibling helper — rather than a changed signature on the shared helper; its doc comment states the one reason it is a twin, in the shape `headlessHooksAgainst` already uses.
NOTES (2026-09-13): the rendered line is matched by a regexp (`5\d%`, `\d+\.\dk tokens`) and the fence around it is pinned exactly via `domain.RenderAdvice` over the matched line plus `HasSuffix`, so the test asserts the exact shipped trailer without hand-computing the run's own overhead chars; a probe run measured the crossing at `rung 50 (55%)` with the second read near 37%.
NOTES (2026-09-13): the script's second and third read turns are told apart by `last_message: '^\[File: fill-N\.txt,'` beside `tool_result: read_file`, because three bare `tool_result` matchers would all take the first.

**What:** Depends on items 3, 5, 6. A stubllm-driven e2e in `cmd/apogee/e2e_fillnotice_test.go` (pattern: the advise journeys in `cmd/apogee/e2e_reactions_test.go:1025-1151` — `headlessHooksIn`, `toolResultHandedOn`, `adviseFirings`; `docs/design/test-drivers.md` §The script format): fixture home with `context-window: 8192` and `context-fill-notice: true` in the `extraConfig` block; a script `cmd/apogee/testdata/stubllm/fill-notice.yaml` whose Turns call `read_file` on three **distinct** fixture files sized so the third result crosses 50% of the History allocation, with no `usage:` report in the script; assert over `stub.Requests()` that the tool-result message of the crossing Turn ends with the exact shipped trailer — `[advice — reaction context-fill-notice (engine origin) at post-tool-result, turn N]`, the rendered line `context: 5x% of the way to automatic compaction — …k tokens used of a 8.2k window`, `[end advice — context-fill-notice]` — and that no earlier request carries `[advice — reaction context-fill-notice`. A headless `--format json` twin asserts one `reaction_fired` line with `"reaction":"context-fill-notice","action":"notice"` and a `detail` starting `rung 50 (`. A third case with the key absent asserts no such fence in any request. Fixture sizes are computed in the test from `apogeectx.Allocate(8192, 0, 0).History` at `DefaultCharsPerToken` (4 chars/token), not hand-pinned.

**Regression guard.** The `read-cache` Floor guard (`internal/floor/readcache.go`, on by default and under Bypass) caps a re-read of one already-read, unwritten file to `max_lines: 1`, so one fixture read three times never crosses 50% — hence three distinct files. The ratio `Calibrate` blends toward every reported `usage.prompt` (`internal/context/budget.go:148-158`), so the script reports no `usage:` at all — a non-positive report leaves the ratio at `DefaultCharsPerToken` 4.0 — and the fixtures are sized at 4 chars/token. Static fixtures under `cmd/apogee/testdata/` can never be read by the run: `headlessHooksIn` roots it in `e2eWorkspace(t)` — a temp dir seeded with `a.txt` only (`cmd/apogee/e2e_support_test.go:516-527`) — and `read_file` refuses a path under no root with the workspace escape message (`internal/tools/read_file.go:59-66`); nor could static files carry sizes computed from `Allocate`. BINDING: write the three files into the workspace at test time (sizes from `apogeectx.Allocate(8192, 0, 0).History × DefaultCharsPerToken`) and script the `read_file` paths relative to the workspace — `headlessHooksIn` creates the workspace and runs in one call (`e2e_reactions_test.go:1337-1372`), so the test seeds through a sibling helper that takes a workspace-seeding step before the run.

**Files:** `cmd/apogee/e2e_fillnotice_test.go`, `cmd/apogee/testdata/stubllm/fill-notice.yaml`

**Tests:** the three cases above; run under the same gates and budgets as `e2e_reactions_test.go` (`docs/design/test-drivers.md` §Gates and budgets).

**Acceptance:**
```
go build ./... && go vet ./cmd/apogee/
go test ./cmd/apogee/ -run 'FillNotice'
```

**Commit:** `test(e2e): the context-fill notice reaches the model as the announced line, once per rung`

## 8. Manual — ✅ DONE (2026-09-13)

**What:** Depends on item 5. `docs/manual/configuration.md:77-79`: rewrite "the `reactions:` list, and nothing else in this release" to name the notice as the one engine advise builtin above the floor, linking the item-5 subsection. `docs/manual/commands.md:420-424`: the Session-section row list gains `context-fill-notice` with the phrase that it is not one of the seven. `docs/manual/reactions.md`: beside `:19` ("A Floor guard is the engine's own `shape (view)` Reaction") add that the engine's own `advise` Reaction is `context-fill-notice` (configuration.md), and in the `bypass:` paragraph (`:300-309`) say the engine notice is off under Bypass too. `docs/manual/headless.md:169`: the `reaction_fired` cell becomes "an engine builtin (a Floor guard or the context-fill notice) or armed Reaction". Rule for the sweep: every manual sentence that says the engine ships only Floor guards as builtins, or that nothing but `reactions:` runs above the floor, names the notice — `grep -rn "nothing else in this release\|only.*Floor guards\|the seven" docs/manual/` finds the candidates; amend each that makes the claim.

**Regression guard.** item 8's link to the notice subsection targets the `### context-fill-notice` heading item 5 adds to docs/manual/configuration.md

**Files:** `docs/manual/configuration.md`, `docs/manual/commands.md`, `docs/manual/reactions.md`, `docs/manual/headless.md`

**Tests:** `TestManualDocumentsEverySettingsKey` (`cmd/apogee/docs_settings_test.go:38`) and `TestManualListsEveryEventLineKind` pass; the grep below proves the "nothing else" claim is gone.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'Docs|Manual'
grep -n "nothing else in this release" docs/manual/configuration.md | wc -l   # 0
```

**Commit:** `docs(manual): context-fill-notice — the key, the line the model sees, and why it ships off`

NOTES (2026-09-13): `docs/manual/configuration.md` and `docs/manual/commands.md` needed no edit — items 5 and 6 already landed the "nothing else in this release" rewrite (naming the notice and linking `#context-fill-notice`) and the Session-row sentence ("not a Floor guard and starts `off`"); this item's sweep confirmed both and touched only the two remaining files.
NOTES (2026-09-13): the acceptance grep's one surviving hit, `docs/manual/daemon.md:108` ("the only live surface. The seven Floor guards are on for every firing"), is a false positive — it states the Floor is on, not that nothing else runs — and the daemon threads `context-fill-notice:` through `cmd/apogee/wire_firing.go:384`, so the sentence stays true; left as is.
