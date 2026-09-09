# Context-fill notice — the first engine advise Reaction

**Goal.** Ship `context-fill-notice`: an engine-origin `advise` Reaction on `post-tool-result` that tells the model, as a fenced trailer on the closing tool result, how far its conversation has climbed toward the automatic Compaction line, at fixed rungs 50 / 75 / 90, with tokens used and the window; a child agent's 90 rung adds one wrap-up sentence. Off by default behind one top-level boolean, off under Bypass, silent when no window is known. It builds ADR 0076 D6's advise slot (provenance fence, ledger, strip on resume), which stage 3 (`apogee-rxj`) reuses unchanged.

**Date:** 2026-09-09 · **Status:** unexecuted · **sized for:** ~200k-context host · **Base:** `7b23f9da` · **Bead:** `apogee-4kb`

**Sources.** `docs/adr/0077-the-context-fill-notice-is-the-first-engine-advise-reaction.md` (every decision) · `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md` D2/D6/D7/D9/D10 · `CONTEXT.md` §Reactions and Moments (**Context-fill notice**, **Reaction**, **Floor guard**) and §Context and history (**Budget**) · `internal/domain/budget.go` (`HistoryExceedsAllocation`, the compare to match) · `internal/agent/compact.go:222-282` (trigger and unknown-window posture) · `internal/agent/builtins.go` (builtin idiom) · `docs/design/test-drivers.md`.

**Ratified design calls** (owner, 2026-09-09 grill; do not re-ask):
- **Cell:** engine-origin `advise`, `post-tool-result`, inherited by children, off under Bypass, **default off**; not a Floor guard.
- **Key:** top-level file-only boolean `context-fill-notice: false`, registered after `read-cache` (Session section), live-editable in `/settings`; the manual entry says it is not a Floor guard and why it defaults off. ADR 0076 D10 amended (recorded).
- **Scale:** percent of the compaction line (the Budget's History allocation) through the trigger's own estimate; never the window. Unknown window (`History <= 0`) ⇒ silent, no substitution.
- **Line:** `context: N% of the way to automatic compaction — Xk tokens used of a Yk window`; tokens = the same estimate the percent is computed from; window = `Budget.Window`.
- **Rungs:** fixed 50 / 75 / 90, at most one notice per tool result (the highest rung reached, reporting the actual percent), re-armed when the fill drops back under a fired rung. 50/75 fact only; 90 adds one engine-authored wrap-up sentence for a child (`depth > 0`) only.
- **Slot:** D6 built here — `ToolResultEdit.AppendAdvice(prov, text)`, fence derived from provenance `{reaction, origin, moment, turn}`, capped, provenance ledger, spans stripped on resume.
- **Switch plumbing (writer, from the tree):** the boolean is `domain.Config.ContextFillNotice` and a fourth `Generation` member, mirroring `Bypass` — never a `FloorConfig` field; `bypassSkips` also skips a **builtin** of class `advise` (Floor guards are `shape (view)` and stay).
- **Events:** the existing `ReactionFiredEvent` keyed `context-fill-notice`, action `notice`, `Detail` = `rung 75 (78%)`; no new headless line kind (`reaction_fired` already carries id/action/detail).

**Regression check (2026-09-09, 7b23f9da):**
- 1: recast — `StripAdvice` suffix-anchored (writer's decision); guard folded — `internal/domain/doc.go` names `advice.go`.
- 3: guard folded (writer's decision) — the restore test carries a mid-text fence as data, byte-identical after the strip.
- 4: guard folded — the child copies `ContextFillNotice` from the LIVE Generation; yields-to-superseded: the "builtins never consult Bypass" wording (`reactions.go:22-25`, `builtins.go:11-12`) is superseded by ADR 0077 and the ratified switch-plumbing call.
- 5: guard folded — keyed `domain.Provenance` literal; `Budget.ContextLimit` rendered when `Window <= 0`.
- 5: rejected — "`usageToolCallScript` responders — no such helper exists"; it exists at `internal/agent/usagetally_test.go:28`.
- 6: recast (writer's decision) — row lands `Editable: false`, the manual subsection and the settings-rows test edits move here; guard folded — `headless.go` / `delegation.go` build no `Config`, dropped.
- 7: recast (writer's decision) — row flipped `Editable: true` with the applier, `ContextFillNotice` threaded through every Driver-built `Generation`, boot test added.
- 8: guard folded — three distinct fixture files (read-cache), no `usage:` in the script, fixtures sized at 4 chars/token.
- 9: recast (writer's decision) — subsection and yaml one-liner moved to item 6; guards folded — `TestManualDocumentsEverySettingsKey` named, `headless.md:169` cell amended, `reactions.md:53` gets its own wording.
- Round 2 (2026-09-09, 7b23f9da) — 6: guards folded — `wire_boot_test.go` / `wire_firing_test.go` named in Files; Acceptance widened to `SettingsRows|Manual`; the one-commit `Editable: false` window recorded as a NOTES line (writer's decision).
- Round 2 — 7: guard folded — the applier row sits directly after the `read-cache` arm, before `delegate-max-steps`; `wire_options.go` dropped from the `Generation` threading list and Files (writer's decision: it builds no `Generation`).

**Standing requirements.**
- `skills: coding-standards`.
- Per item: `go build ./... && go vet <pkgs>` plus the tests named in Acceptance; the full suite runs once at closeout.
- Wording ships as prompt assets under `internal/agent/prompts/`, never Go literals; every new `.go` file is named in its package's `doc.go` (docmap gates).
- Authorized deviations land as a dated `NOTES (YYYY-MM-DD):` line under the item; CHANGELOG entries travel in sidecars.

**Out of scope.** The user advise cell and `advise:` key (stage 3, `apogee-rxj`) · the apogee-sim bench arm that could flip the default on (`apogee-sbj`) · `/settings` "what the model saw this Turn" ledger view · mid-Exchange auto-compaction for the main loop (`apogee-1bf`) · any `TopLevelOnly` semantics for builtins · VERSION and CHANGELOG headings.

## 1. Advise slot primitives in `domain`

**What:** Recast at the regression check (2026-09-09). New `internal/domain/advice.go`: `type Provenance struct { Reaction string; Origin Origin; Moment Moment; Turn int }`; `func AdviceFence(p Provenance) string` rendering exactly `<apogee-advice reaction="…" origin="…" moment="…" turn="N">` (attribute order fixed, values through `strconv.Quote`-free plain text — ids, origins and moments are already restricted vocabularies); const `adviceFenceClose = "</apogee-advice>"`; const `AdviceCap = 1024` (chars; longer text is cut at the cap with a trailing `…`). `func StripAdvice(content string) string` removes every `<apogee-advice …>…</apogee-advice>` block together with the two newlines that precede it (see below), leaving other text byte-identical; a header with no close tag is left alone. `(*Conversation).StripAdvice() int` applies it to every `RoleTool` message and bumps the revision once when anything changed. In `internal/domain/tooledit.go`, `(*ToolResultEdit).AppendAdvice(p Provenance, text string)` appends `"\n\n" + AdviceFence(p) + "\n" + capped text + "\n" + adviceFenceClose` to the content, bumps the revision, and records `p` on the edit; `(*ToolResultEdit).Advice() []Provenance` returns the recorded spans in append order. Pure domain code: no agent imports, no I/O.

**Regression guard.** `StripAdvice` is SUFFIX-ANCHORED — it strips only a span that terminates the content (the appended form `"\n\n" + fence + "\n" + text + "\n" + adviceFenceClose` as the content's tail), repeating from the tail so stacked spans all go; a fence anywhere else in the content is data and stays byte-identical. The round-trip test adds a `RoleTool` message carrying a literal fence mid-text and asserts it is untouched by both `StripAdvice` and `Conversation.StripAdvice`. `advice.go` is named in `internal/domain/doc.go` (the file map names every file; `docmap_test.go` gates it).

**Files:** `internal/domain/advice.go`, `internal/domain/advice_test.go`, `internal/domain/tooledit.go`, `internal/domain/tooledit_test.go`, `internal/domain/doc.go`

**Tests:** `AdviceFence` renders the pinned header for a sample provenance; `AppendAdvice` bumps the revision, records provenance, caps at `AdviceCap`; `StripAdvice` round-trips: append then strip yields the original content byte-for-byte, two spans on one result both stripped, an unterminated header untouched, a `RoleUser` message carrying the literal fence untouched by `Conversation.StripAdvice`; a `RoleTool` message carrying a literal fence mid-text (not as the tail) byte-identical after both `StripAdvice` and `Conversation.StripAdvice`; `Conversation.StripAdvice` bumps `Revision()` exactly once and returns the span count.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/
go test ./internal/domain/ -run 'Advice|ToolResultEdit'
```

**Commit:** `feat(domain): the advise slot — provenance fence, AppendAdvice on the tool-result edit, StripAdvice`

## 2. The fill measure on `Budget` and `ConversationView`

**What:** In `internal/domain/budget.go`, `func (b Budget) HistoryFill(chars int) float64` returns `EstimateTokens(chars) / History` and **0** when `History <= 0` or `CharsPerToken <= 0` (inert on an unknown window — the same clauses `HistoryExceedsFraction` has). In `internal/domain/hookview.go`, `ConversationView` gains `PromptChars() int` = `PromptChars(msgs, nil)` over the viewed slice (no copy). The invariant to pin: for any msgs, `HistoryExceedsAllocation(msgs) == (HistoryFill(PromptChars(msgs, nil)) > 1.0)` — the notice and the fold read one scale.

**Files:** `internal/domain/budget.go`, `internal/domain/budget_test.go`, `internal/domain/hookview.go`, `internal/domain/hookview_test.go`

**Tests:** table over `{History, CharsPerToken, chars}` including the zero-History and zero-ratio rows (fill 0); the equivalence property above at chars one under, at, and one over the History boundary (rounding: `EstimateTokens` ceils, so the equality must hold exactly at the boundary too); `ConversationView.PromptChars` equals `domain.PromptChars(conv.Messages(), nil)` on a conversation with tool calls and results.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/
go test ./internal/domain/ -run 'HistoryFill|HistoryExceeds|PromptChars'
```

**Commit:** `feat(domain): Budget.HistoryFill and ConversationView.PromptChars read the compaction trigger's scale`

## 3. Provenance ledger, strip on resume, forgery closure in `agent`

**What:** Depends on item 1. `Agent` gains a process-local `advice []domain.Provenance` ledger (never encoded in `agentState`); `firePostToolResult` (`internal/agent/reactions.go:182-187`) appends `edit.Advice()` to it after the cascade, and the firing it books carries the seam's advice count in its `Detail` only when the handler set none. `restoreState` (`internal/agent/state.go:132-172`) calls `conv.StripAdvice()` on the decoded conversation after `dropLeadingSystem`, before the swap into `a.conv`, so a resumed session never re-reads a stale span; the strip count is reported through the existing restore notice path if one exists, else silently. `forgesStandingStructure` (`internal/agent/contextfiles.go:192`) refuses a context file whose trimmed line starts with `<apogee-advice` exactly as it refuses the delegate-report fence. Name `state.go`'s new step in `internal/agent/doc.go` only if the file map lists responsibilities (check `docmap_test.go`).

**Regression guard.** The resume strip inherits item 1's suffix anchoring: the restore test includes a tool result whose CONTENT contains a literal `<apogee-advice …>…</apogee-advice>` block mid-text as data (a read of a test file) and asserts that message survives byte-identical while the appended spans on other results are removed.

**Files:** `internal/agent/agent.go`, `internal/agent/reactions.go`, `internal/agent/state.go`, `internal/agent/state_test.go`, `internal/agent/contextfiles.go`, `internal/agent/contextfiles_test.go`, `internal/agent/reactions_test.go`

**Tests:** encode a conversation with two advice spans on tool results, restore it through `Agent.RestoreSession`, and assert the conversation the next request projects carries no appended `<apogee-advice` span while every other byte is unchanged, including a tool result whose content holds a literal `<apogee-advice …>…</apogee-advice>` block mid-text as data (byte-identical after the restore); `agentState` JSON carries no ledger field; a context file containing an advice fence line is refused with the same outcome as the delegate-report forgery test at `contextfiles_test.go:530-546`; a scripted post-tool-result reaction calling `AppendAdvice` lands its provenance on `a.advice`.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'Restore|Advice|Forge|PostToolResult'
```

**Commit:** `feat(agent): advice spans ride a process-local ledger, are stripped on resume, and cannot be forged from a context file`

## 4. The switch: `Config.ContextFillNotice`, a fourth `Generation` member, Bypass for a builtin advise

**What:** `domain.Config` gains `ContextFillNotice bool` (doc: ADR 0077 D1/D2, not a Floor guard); `domain.Generation` gains `ContextFillNotice bool` as its fourth member (`internal/domain/reaction.go:440-449`; extend `Generation.Validate` and its test only if it inspects members). `Agent.SetReactions` (`internal/agent/agent.go:1038-1045`) rebuilds the builtin ladder when `gen.Floor` **or** `gen.ContextFillNotice` moved and copies the new member; `construct.go:89/113` seeds both from `cfg`. `buildBuiltins(gates domain.FloorConfig)` becomes `buildBuiltins(gates domain.FloorConfig, notice bool)` — item 5 appends the reaction; here the parameter is threaded and unused. `bypassSkips` (`internal/agent/reactions.go:345-356`) is consulted for builtins too: `fireLeg`'s `!builtin &&` guard becomes "skip when `bypassSkips` says so, where a builtin is skipped only if its class is `advise`" — the seven `shape (view)` guards keep firing under Bypass, exactly as today. The child inherits the switch through `childCfg` (verify `newChildAgentOn` copies `Config` wholesale; if it copies fields, add this one).

**Regression guard.** `newChildAgentOn` copies `cfg` wholesale but overrides `Bypass`/`Floor` from the LIVE Generation (`subagent.go:523-525`), so a switch flipped through `SetReactions` would never reach a child spawned after it: add `childCfg.ContextFillNotice = gen.ContextFillNotice` beside `subagent.go:524-525`, and the child test flips the switch via `SetReactions`, not `cfg`, so it bites. The documented rule that "the builtins never consult [Bypass]" (`internal/agent/reactions.go:22-25`, `internal/agent/builtins.go:11-12`) is superseded by ADR 0077 and the header's ratified switch-plumbing call — rewrite both comments here; `TestFireBypassMatrix`'s "a builtin is never skipped" case (`reactions_test.go:372`) stays as the shape-view builtin case and stays green.

**Files:** `internal/domain/config.go`, `internal/domain/reaction.go`, `internal/domain/reaction_test.go`, `internal/agent/agent.go`, `internal/agent/construct.go`, `internal/agent/builtins.go`, `internal/agent/reactions.go`, `internal/agent/reactions_test.go`, `internal/agent/setlive_test.go`, `internal/agent/subagent.go`, `internal/agent/subagent_test.go`

**Tests:** `SetReactions` with only `ContextFillNotice` changed rebuilds the ladder (observable through the builtin count or a spy) and with nothing changed does not; a fake engine builtin of class `advise` is skipped under Bypass while a Floor guard is not (`reactions_test.go` fixtures); every existing `Generation` comparison test (`setlive_test.go:124-152`, `reactions_test.go:682-689`) still passes with the fourth member zero; a child spawned after the parent's switch was flipped ON through `SetReactions` (construction `cfg` off) carries it, and one spawned after it was flipped OFF does not.

**Acceptance:**
```
go build ./... && go vet ./internal/domain/ ./internal/agent/
go test ./internal/domain/ ./internal/agent/ -run 'Generation|SetReactions|Bypass|Child'
```

**Commit:** `feat(agent): Generation carries the context-fill-notice switch and Bypass reaches a builtin advise reaction`

## 5. The `context-fill-notice` reaction

**What:** Depends on items 1–4. New `internal/agent/fillnotice.go`: `const contextFillNoticeID = "context-fill-notice"`, `actionNotice = "notice"`, `var fillRungs = [3]int{50, 75, 90}`; `Agent` field `fillRung int` (highest rung fired on the current climb, 0 = none); handler `(a *Agent) contextFillNotice(ctx, view, call, result) (domain.Outcome, error)` as a `domain.PostToolResultFunc`: `chars := view.Conversation().PromptChars() + len(result.Content())`, `fill := view.Budget().HistoryFill(chars)`; fill 0 ⇒ return zero Outcome (unknown window, silent). `pct := int(fill * 100)`; `reached` = highest rung `<= pct`; if `reached < a.fillRung` set `a.fillRung = reached` (re-arm) and return; if `reached > a.fillRung`: set `a.fillRung = reached`, render the fact line from `prompts/context-fill-notice.txt` (`fmt` verbs: `%d` pct, `%s` tokens, `%s` window; tokens = `view.Budget().EstimateTokens(chars)`, window = `view.Budget().Window`, both through one `formatTokens(n int) string` in this file — `999` → `999`, `15400` → `15.4k`, `131072` → `131k`), append the child sentence from `prompts/context-fill-wrap-up.txt` when `reached == 90 && a.depth > 0`, and `result.AppendAdvice(domain.Provenance{contextFillNoticeID, domain.OriginEngine, domain.MomentPostToolResult, view.Turn()}, text)`; return `Outcome{Detail: fmt.Sprintf("rung %d (%d%%)", reached, pct)}`. `buildBuiltins` appends `engineBuiltin(contextFillNoticeID, actionNotice, domain.MomentPostToolResult, domain.PostToolResultFunc(a.contextFillNotice))` with `Class: domain.ClassAdvise` when `notice` is true, **after** the seven guards. `armReactions`'s reserved set (`reactions.go:470-479`) becomes `guardIDs` plus `contextFillNoticeID`; `guardIDs` stays seven. Fact line asset text: `context: %d%% of the way to automatic compaction — %s tokens used of a %s window`. Wrap-up asset text: `Your context is nearly full. Stop working now: make your next reply the final report — what you finished, what is left, and where — so your parent can carry on.`

**Regression guard.** The `domain.Provenance` literal is written keyed (`domain.Provenance{Reaction: …, Origin: …, Moment: …, Turn: …}`) — an unkeyed literal of an imported struct fails `go vet ./internal/agent/` (composites). A `working-window` with no advertised window (`loop.go:1318-1322`) yields `History > 0` with `Budget.Window == 0`, so the notice fires: render `Budget.ContextLimit` in the window slot when `Budget.Window <= 0` — never "a 0 window" — and pin it with a `MaxContextTokens = 0, WorkingWindow = 32768` row.

**Files:** `internal/agent/fillnotice.go`, `internal/agent/fillnotice_test.go`, `internal/agent/prompts/context-fill-notice.txt`, `internal/agent/prompts/context-fill-wrap-up.txt`, `internal/agent/builtins.go`, `internal/agent/reactions.go`, `internal/agent/reactions_test.go`, `internal/agent/doc.go`

**Tests:** drive an agent (`usageToolCallScript` responders, `cfg.Context.MaxContextTokens = 8192`, switch on) through tool results sized to cross 50, then 75, then 90: exactly three notices, each once, on the closing tool result, with the pinned line rendered from the asset; a single result jumping 40→80 fires one notice at rung 75 reporting the actual percent; after a fold (`scriptedCompactResponder`, `CompactionEnabled`) the fill drops and the next climb fires 50 again; `MaxContextTokens = 0` ⇒ no notice and no firing; `MaxContextTokens = 0, WorkingWindow = 32768` ⇒ the notice fires and its window reads `32k` (`Budget.ContextLimit`), never `0`; child (`depth > 0`) 90 carries the wrap-up sentence, main does not; switch off ⇒ no builtin registered; Bypass on ⇒ no notice; `ReactionFiredEvent{Reaction: "context-fill-notice", Action: "notice", Detail: "rung 50 (52%)"}` observed on the sink; at the boundary where `historyExceedsAllocation` first answers true the last notice's percent was `>= 100` (the trigger-equivalence test); `formatTokens` table.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/
go test ./internal/agent/ -run 'FillNotice|ContextFill|Prompt|DocMap'
```

**Commit:** `feat(agent): the context-fill-notice reaction — three rungs of the compaction line on the closing tool result`

## 6. The `context-fill-notice:` config key

**What:** Recast at the regression check (2026-09-09). Depends on item 4. `internal/config/config.go`: `fileConfig` gains `ContextFillNotice *bool \`yaml:"context-fill-notice"\`` in its **own** comment block after the Floor block (`:1320-1341` stays "the seven"); `keyAccessors` gains a row in the default-**false** shape of `remember-model` (`:752-758`); `internal/config/options.go` gains `ContextFillNotice bool` outside the Floor block. `internal/config/registry.go`: row `{Path: "context-fill-notice", Kind: KindBool, Default: "false", Editable: true, Desc: "Tell the model how close it is to automatic compaction (50/75/90) on its tool results. Not a Floor guard: off until bench evidence turns it on.", Read: …}` inserted **directly after `read-cache`** so it lands in the Session section. `internal/config/reactions.go`: a `reactions:` entry with `id: context-fill-notice` is refused with `reaction %q: that is the built-in engine reaction context-fill-notice: — set the top-level key, not a reactions: entry` (a second literal beside `floorGuardKeys`; `floorGuardKeys` stays seven). `internal/config/defaults/config.yaml`: after the Floor block a short second-person paragraph (what it tells the model, that it is not a Floor guard, why it ships off, how to turn it on) then the active line `context-fill-notice: false`. Wire the value: `cmd/apogee`'s `Options → domain.Config` seam sets `Config.ContextFillNotice` (find via `floorFromOptions` callers: `wire_boot.go:350`, `wire_firing.go:372`, `wire_settings.go:291`) — this item sets the boot and firing sites (headless composes through `firingConfig`); item 7 owns the live path.

**Regression guard.** To keep `go test ./cmd/apogee/` green at this item's commit, item 6 registers the row with `Editable: false` (item 7 flips it), adds the key's manual documentation itself — the yaml one-liner and the `### context-fill-notice` subsection described in item 9 move here, so `TestSettingsDocsCoverEveryKey` passes — and updates `cmd/apogee/settingsrows_test.go` (`want` map entry, `fabricatedSettings()` field) so `TestSettingsRowsFormatEffectiveValues` passes; Files gain `docs/manual/configuration.md` and `cmd/apogee/settingsrows_test.go`. `headless.go` and `delegation.go` build no `domain.Config` (headless composes through `firingConfig`, `headless.go:712` → `wire_firing.go:264-372`; `delegation.go:872-898` returns a `DelegationTarget`), so they are dropped from What and Files: the seams are `wire_boot.go:210-350`, `wire_firing.go:264-372` and the holder seed at `wire_settings.go:290-294` (item 7 owns the last).

**Regression guard (round 2).** Files gain `cmd/apogee/wire_boot_test.go` and `cmd/apogee/wire_firing_test.go` — the boot and firing wiring tests this item's Tests extend (`TestBootConfigCarriesTheFloorGuardKeys`, `wire_boot_test.go:752`; `TestFiringConfigCarriesTheFloorGuardKeys`, `wire_firing_test.go:249`). Acceptance runs `-run 'Boot|Headless|Firing|Delegation|SettingsRows|Manual'` so `TestSettingsRowsFormatEffectiveValues` (`settingsrows_test.go:367`) and `TestManualDocumentsEverySettingsKey` (`docs_settings_test.go:38`) — the two gates the round-1 guard keeps green — are actually selected.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/options.go`, `internal/config/registry.go`, `internal/config/registry_test.go`, `internal/config/reactions.go`, `internal/config/reactions_test.go`, `internal/config/defaults/config.yaml`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_boot_test.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/settingsrows_test.go`, `docs/manual/configuration.md`

**Tests:** `TestEveryConfigKeyReachesTheOptions` `want` map + `everyKeyFileConfig` (non-default = `true`) updated; a `TestApplyConfigContextFillNotice` in the shape of `TestApplyConfigRememberModel` (absent ⇒ false, `true` ⇒ true, `false` ⇒ false); `TestApplyConfigFloorGuardKeys` table unchanged; `TestRegistryIsBijectionWithFileConfig`, `TestRegistryRowInvariants`, `TestRegistryRowsProjectEveryValue` pass; the refusal sentence pinned whole in `reactions_test.go` beside the Floor one, and `len(floorGuardKeys) == 7` still asserted; the embedded template parses with the key false; boot and firing wiring tests assert `Config.ContextFillNotice` follows the option; `TestManualDocumentsEverySettingsKey` (the docs gate, `cmd/apogee/docs_settings_test.go:38`) and `TestSettingsRowsFormatEffectiveValues` pass with the key documented and its row (`Editable: false`) in the `want` map and `fabricatedSettings()`.

**Acceptance:**
```
go build ./... && go vet ./internal/config/ ./cmd/apogee/
go test ./internal/config/ && go test ./cmd/apogee/ -run 'Boot|Headless|Firing|Delegation|SettingsRows|Manual'
```

**Commit:** `feat(config): the context-fill-notice key — file-only, default off, refused as a reactions: id`

NOTES (2026-09-09): accepted at the regression check — for the one commit between items 6 and 7 the row is `Editable: false` and so `externallyEdited` (`cmd/apogee/settingsrows.go:298`): `/settings` paints it with the `⏎ opens $EDITOR` pointer until item 7 flips it `Editable: true` with the applier.

## 7. Live toggle in `/settings` and the settings rows

**What:** Recast at the regression check (2026-09-09). Depends on item 6. `cmd/apogee/wire_settings.go`: a `setContextFillNotice(on bool) domain.Generation` beside `setFloorGuard` (`:766-790`, which stays a seven-key switch) returning the live Generation with the fourth member set; an applier row for `context-fill-notice` with `reaches: reachesTheEngineAndTheHolder` and an `applyContextFillNotice` mirroring `applyFloorGuard` (`:1888-1897`); the live-options projection (`:906-913`) reflects the member back; every site that builds a `Generation` (`wire_settings.go:290`, `wire_live.go:195`) seeds the member from `Config.ContextFillNotice`. `cmd/apogee/settingsrows_test.go` (`want` map entry, `fabricatedSettings()` field) was updated in item 6 at the regression check. Pick nothing in `layout.md` — it enumerates no keys.

**Regression guard.** Flips the registry row to `Editable: true` in the same commit as the applier so `TestEveryEditableSettingKeyHasAnApply` never sees an editable row without an apply; BINDING: thread `ContextFillNotice` through every `Generation` literal the Driver builds — `cmd/apogee/wire_live.go:195-199`, `cmd/apogee/wire_settings.go:290-294` — because the TUI applies a Generation at Bind that would otherwise overwrite the construct-time seed with false; add a boot test asserting a home with `context-fill-notice: true` yields an engine Generation whose `ContextFillNotice` is true after Bind (`wire_engine_test.go` / `wire_boot_test.go` shape); Files gain `internal/config/registry.go`.

**Regression guard (round 2).** The applier row for `context-fill-notice` sits directly after the `read-cache` arm of `settingsTable` (`wire_settings.go:1418-1422`) and before `delegate-max-steps`, matching the registry insertion in item 6 — `TestSettingsTableIsInRegistryOrder` (`wire_settings_test.go:633`) fatals on an entry out of `KeyRegistry` order, so a row appended at the end of the table goes red. `cmd/apogee/wire_options.go` builds no `Generation` (its `:68` `Bypass:` is a `tui.Options` field) — removed from the threading list and Files; the Driver-built `Generation` sites are exactly `cmd/apogee/wire_live.go:195` and `cmd/apogee/wire_settings.go:290`.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_engine_test.go`, `cmd/apogee/wire_boot_test.go`, `internal/config/registry.go`

**Tests:** `TestContextFillNoticeRowAppliesOneGeneration` in the shape of `TestBypassRowAppliesOneGeneration` (`:395`) — toggling the row swaps one Generation whose Floor and Bypass are unchanged; `TestEveryEditableSettingKeyHasAnApply` and `TestApplySettingRefusesEveryKeyItCannotReach` pass; `TestLiveSettingsOptionsFollowEveryApply` covers the key; `TestSettingsRowsFormatEffectiveValues` (`len(want) == len(config.KeyRegistry)`) passes; `TestSettingsRowsCarryTheirSection` places it in Session; a boot test in the `wire_engine_test.go` / `wire_boot_test.go` shape: a home with `context-fill-notice: true` yields an engine Generation whose `ContextFillNotice` is true after Bind; the `t16-settings-rows` golden is untouched (it captures the pane top) — never `-update` it in this item.

**Acceptance:**
```
go build ./... && go vet ./cmd/apogee/
go test ./cmd/apogee/ -run 'Settings|Generation|Apply|Rows'
```

**Commit:** `feat(settings): context-fill-notice is a live on/off row in the Session section`

## 8. Announced-surface journey: the exact line the model sees

**What:** Depends on items 5–7. A stubllm-driven e2e in `cmd/apogee/e2e_fillnotice_test.go` (pattern: `e2e_announced_test.go:63-132`, `docs/design/test-drivers.md` §The script format): fixture home with `context-window: 8192` and `context-fill-notice: true`; a script whose Turns call `read_file` on three distinct fixture files sized so the third result crosses 50% of the History allocation, with no `usage:` report in the script; assert over `stub.Requests()` that the tool-result message of the crossing Turn ends with `<apogee-advice reaction="context-fill-notice" origin="engine" moment="post-tool-result" turn="N">`, the exact rendered line `context: 5x% of the way to automatic compaction — …k tokens used of a 8.2k window`, and `</apogee-advice>`, and that no earlier request carries the fence. A headless `--format json` twin asserts one `reaction_fired` line with `"reaction":"context-fill-notice","action":"notice"`. A third case with the key absent asserts no fence in any request. Fixture sizes are computed in the test from `apogeectx.Allocate(8192, 0, 0).History` at `DefaultCharsPerToken` (4 chars/token), not hand-pinned.

**Regression guard.** The `read-cache` Floor guard (`internal/floor/readcache.go:14-45`, on by default and under Bypass) caps a second and third `read_file` of one already-read, unwritten file to `max_lines: 1`, so one fixture read three times never crosses 50%: the script reads three DISTINCT fixture files (a range- or limit-qualified read would also escape the guard; `read-cache: false` in the fixture home is the alternative). The percent is `Budget.EstimateTokens(PromptChars(conv.Messages()))` over a ratio `Calibrate` blends toward every reported `usage.prompt` (`internal/context/budget.go:148-158`), so the script reports no `usage:` at all — a non-positive report leaves the ratio at `DefaultCharsPerToken` 4.0 (`budget.go:11,149`) — and the fixtures are sized at 4 chars/token.

**Files:** `cmd/apogee/e2e_fillnotice_test.go`, `cmd/apogee/testdata/stubllm/fill-notice.yaml` (name per the existing script directory's convention), three fixture files under `cmd/apogee/testdata/` (named per that directory's convention)

**Tests:** the three cases above; run under the same gates and budgets as `e2e_announced_test.go` (`docs/design/test-drivers.md` §Gates and budgets).

**Acceptance:**
```
go build ./... && go vet ./cmd/apogee/
go test ./cmd/apogee/ -run 'FillNotice'
```

**Commit:** `test(e2e): the context-fill notice reaches the model as the announced line, once per rung`

## 9. Manual

**What:** Recast at the regression check (2026-09-09). Depends on item 6. `docs/manual/configuration.md`: after the Floor paragraph (`:67-75`) a short subsection `### context-fill-notice` — what the model receives (the pinned line and the rungs, the child wrap-up at 90), that it is **not** a Floor guard (it steers, so it lives above the floor), why it defaults off (the hard invariant; ADR 0077), that Bypass turns it off, that it is silent until a window is known, and the one-line yaml; rewrite the sentence at `:77-82` ("the `reactions:` list, and nothing else in this release") to name the notice as the one engine advise builtin. `docs/manual/commands.md:420-424`: the Session-section row list gains `context-fill-notice` with the phrase that it is not one of the seven. `docs/manual/reactions.md:48`: "not yet shipped" becomes "no *user* `advise:` entry ships yet; the engine's own advise reaction is `context-fill-notice` (configuration.md)"; `:53` gets its own wording (see the guard). `docs/manual/headless.md:169`: the `reaction_fired` cell is amended (see the guard).

**Regression guard.** The `### context-fill-notice` subsection and the yaml one-liner moved to item 6; this item keeps the rewrite of the "nothing else in this release" sentence in `docs/manual/configuration.md`, `docs/manual/commands.md` and `docs/manual/reactions.md`; Acceptance unchanged. The docs gate is `TestManualDocumentsEverySettingsKey` (`cmd/apogee/docs_settings_test.go:38`; `TestSettingsDocsCoverEveryKey` does not exist) — name it. `docs/manual/headless.md:169` describes `reaction_fired` as "builtin Floor guard or armed Reaction"; amend that cell to "an engine builtin (a Floor guard or the context-fill notice) or armed Reaction" and list `headless.md` in Files. `docs/manual/reactions.md:53` carries no "not yet shipped" (it reads "Reserved for the classes a later release ships. An entry that spells either is refused today"): give it its own wording — "a user `advise:` entry is reserved for a later release; the engine's own advise reaction is `context-fill-notice`".

**Files:** `docs/manual/configuration.md`, `docs/manual/commands.md`, `docs/manual/reactions.md`, `docs/manual/headless.md`

**Tests:** `TestManualDocumentsEverySettingsKey` (`cmd/apogee/docs_settings_test.go:38`) passes with the new key; `TestManualListsEveryEventLineKind` unchanged; a grep proves no manual sentence still claims the Reaction core ships "nothing else" or that advise ships nothing.

**Acceptance:**
```
go test ./cmd/apogee/ -run 'Docs|Manual'
grep -n "nothing else in this release" docs/manual/configuration.md | wc -l   # 0
grep -n "not yet shipped" docs/manual/reactions.md | wc -l                     # 0
```

**Commit:** `docs(manual): context-fill-notice — the key, the line the model sees, and why it ships off`
