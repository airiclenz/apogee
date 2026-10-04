# Plan — Console prompt reason, --endpoint vision advice, Fork spend

**Goal:** The approval prompt for a POSIX send to an unconfined Console names the Console and the reopen fix. A vision refusal on an `--endpoint` run gives advice the user can act on. A forked session starts at zero spend, delegate spend included.
**Date:** 2026-10-04
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** f5497a19

**Regression check (2026-10-04, f5497a19):**
- 1: guard folded (executeTool signature, stale-comment rule, `Confine`-named test; supersedes CHANGELOG.md [Unreleased] apogee-zwvj "The Approval prompt is unchanged.")
- 2: guard folded (decision: constants copied from item 1's tree; line-broken "stays generic" check; CHANGELOG apogee-zwvj sentence)
- 3: recast (decision: flag rides serverBinding via UpstreamSpec and DelegationTarget, not RebindSpec alone; daemon case reads APOGEE_ENDPOINT); fixture and test guards folded
- 3 (round 2): recast (decision: the flag is a serverBinding field present on UpstreamSpec and as a present false on DelegationTarget, absent from RebindSpec so every beat's Rebind keeps it like Vision; no RebindSpec field; Approach/Files/Tests/guard reconciled; bindingCells/cellsOf/bindingParent carry the flag; `Ephemeral`-named move test); yields to serverbinding.go:57-59
- 4: guard folded (non-vision test double, spelled constant, lateEngine "" fallback, named-server flash change stated; daemon case accepted, superseding options.go:117-122 ServerFlagBound for it)
- 5: guard folded (commands.md in the Acceptance grep)
- 6: guard folded (test built on newForkModel + delegate with priced Usage; docs as additions; transcriptbridge.go comment; fill and DelegateUsage asserts)

**Sources:**
- `docs/handoffs/2026-10-03 - 00 - open items outside the refocus follow-ups plan.md` ("Follow-ups found while planning" 1–3)
- `docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md` (2026-10-04 amendment), `docs/design/confinement-execution-contract.md`
- `docs/adr/0093-usage-is-priced-per-call-at-the-bound-server.md` (decision 6 + 2026-10-04 amendment)
- beads `apogee-console-prompt-generic-reason`, `apogee-endpoint-vision-advice`, `apogee-fork-inherits-delegate-spend`

**Ratified design calls (owner, 2026-10-04):**
- **Console prompt:** Reason `send to console N, which was opened unconfined`; Fix `deny it — the agent is told to close the console and reopen it fenced`; no `/confine off` advice. Windows and every real host failure keep today's wording.
- **--endpoint vision:** advice only, no new flag. The refusal tells the user to add a `servers:` entry with `vision: true` and start with `--server <name>`.
- **Fork spend:** strip per-head spend from the copied prefix at the cut; keep context fill. A copied Workflow finish note loses its spend line.

**Standing requirements:**
- skills: coding-standards
- Machine limits (`AGENTS.local.md`): never `go test -race` over a package pattern or a whole heavy package; one package per run, narrowed with `-run`, prefixed `GOMEMLIMIT=2GiB`; never two test runs at once. Pass this to every sub-agent.

**Out of scope:**
- A `--vision` flag or `APOGEE_VISION` env.
- Resume reaching headless/daemon drivers (`domain.Usage.Unpriced()` single consumer).
- Any change to Windows Console confinement.

## 1. Console send carries its own approval reason

**What:**
**Goal:** Under a confinement box on POSIX, a `console_send` to a Console opened unconfined raises an approval request whose Reason is `send to console N, which was opened unconfined` and whose Remedy is `deny it — the agent is told to close the console and reopen it fenced`. Every other route into the confine fallback keeps `confineDemoteGateReason` / `confineUnavailableRemedy` byte for byte. The refusal/deny text the model receives is unchanged. Fixes `apogee-console-prompt-generic-reason`: the prompt advised `/confine off` on a host that can confine.
**Approach (assumed at the header base):**
- Add an exported error type in `internal/domain/errors.go` (e.g. `ConfineDemoteError{Detail, Reason, Remedy}`) whose `Error()` yields exactly today's `unfencedSendError` text and whose `Unwrap()` returns `ErrConfinementUnavailable`, so `errors.Is` and `withConfineFailureDetail` keep working.
- `unfencedSendError` in `internal/tools/console_send.go` returns it on the POSIX branch only (`ConsoleConfines()` true); the Windows branch stays a plain `%w` wrap.
- `executeTool` in `internal/agent/dispatch.go` reduces the error to a string today; carry the typed error (or its Reason/Remedy) out to `executeConfineFallback`. When present and the fallback is `resolveGate`, call `approve` with its Reason/Remedy; keep `force` and `cacheKey`. Refuse/deny paths keep `withConfineFailureDetail`.
- `confineFallback` in `internal/agent/resolution.go` is unchanged; update the comments on `executeConfineFallback`, `confineFallback` and `confineUnavailableRemedy` that say the prompt "stays generic".
**Regression guard.** Keep `executeTool`'s two-value signature (a thin wrapper carries the typed error to `executeConfine` only); if it changes instead, `treesnapshot_test.go:101,457` and `undo_group_test.go:266` change with it. Comment sites are a rule, not a list: every comment saying the demote prompt is generic or naming which gates carry a Remedy — `grep -rn -E 'stays generic|confinement-unavailable (gates|pair)|two confinement-unavailable' internal/` (today also `domain/approval.go:41`, `resolution.go:64,137`, `tui/approval.go:426`, `resolution_test.go:309`). The new dispatch test's name contains `Confine` so Acceptance runs it. Supersedes CHANGELOG.md [Unreleased] apogee-zwvj "The Approval prompt is unchanged." (amended by item 2).
**Files:** internal/domain/errors.go, internal/domain/approval.go, internal/tools/console_send.go, internal/tools/console_send_test.go, internal/agent/dispatch.go, internal/agent/dispatch_test.go, internal/agent/resolution.go, internal/agent/resolution_test.go, internal/tui/approval.go, internal/agent/treesnapshot_test.go and internal/agent/undo_group_test.go (only if `executeTool`'s signature changes)
**Read first:** internal/agent/dispatch.go — executeConfine, executeConfineFallback, executeTool, approve; internal/tools/console_send.go — unfencedSendError; internal/agent/resolution.go — confineDemoteGateReason, confineUnavailableRemedy; internal/agent/dispatch_test.go — unfencedTargetTool
**Closes:** apogee-console-prompt-generic-reason
**Tests:**
- `internal/tools/console_send_test.go`: the POSIX unconfined-send error `errors.As` to the new type with the exact Reason/Remedy above and `errors.Is(err, ErrConfinementUnavailable)`; its `Error()` still contains "close it" / "reopen it"; the Windows-wording case is not the new type.
- `internal/agent/dispatch_test.go`: extend `unfencedTargetTool` to return the typed error; assert the approval request's Reason/Remedy equal the error's, and that a plain-wrap tool (`confinePropagatingTool`) still gets `confineDemoteGateReason` / `confineUnavailableRemedy`. Name the new test with `Confine` in it (e.g. `TestDispatch_ConfineFallbackPromptCarriesTheToolReason`).
- `internal/agent/resolution_test.go`: the `:309` comment follows the comment rule above.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/tools/ ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ConsoleSend' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Confine' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Resolve' ./internal/agent/`
**Commit:** `fix(agent): name the Console and the reopen fix in its unconfined-send approval prompt`

## 2. Console prompt — docs follow the code

**What:**
**Goal:** Every doc line that describes the confine-fallback approval prompt states that a POSIX send to an unconfined Console shows a Console-specific Reason and Fix, and that every other demote keeps the generic wording. No doc says the Console prompt "stays generic". Depends on item 1.
**Approach (assumed at the header base):**
- `docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md`: a dated 2026-10-04 note under its amendment superseding "the Approval prompt stays generic".
- `docs/design/confinement-execution-contract.md`: the passage saying Remedy is set on "exactly the two gates" and that the demote "carries the same wording" gains the Console case.
- `docs/manual/configuration.md`: the Console paragraph says what the prompt shows.
- Rule for finding sites: every doc line quoting `confinement unavailable on this host`, `/confine off`, or "stays generic" next to Console — `grep -rn -E 'confinement unavailable|stays generic|confine off' docs/ CONTEXT.md`.
**Regression guard.** every doc quote of the Console prompt and the Acceptance grep copy item 1's landed Reason/Remedy constants verbatim, read from the tree at run time, never retyped from the plan. The contract's sites include the bold lead "on two cells only a `Remedy`" (:774) and the D4 paragraph ("A `Confine` carries a bounded runtime `fallback`"), whose "stays / generic" is split across a line break — find it with `perl -0ne 'print "$ARGV\n" if /stays\s+generic/' docs/adr/*.md docs/design/*.md docs/manual/*.md CONTEXT.md`. Amend CHANGELOG.md [Unreleased] apogee-zwvj's "The Approval prompt is unchanged." so the release notes do not contradict item 1 — an edit of an existing line, so CHANGELOG.md stays off **Files:** (house lint); this item's new entry still travels in its sidecar.
**Files:** docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md, docs/design/confinement-execution-contract.md, docs/manual/configuration.md
**Read first:** docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md — Amendment (2026-10-04); docs/design/confinement-execution-contract.md — Gate Reason/CacheKey/Remedy paragraph, D4 runtime fallback paragraph; docs/manual/configuration.md — The Console family ("On Windows, through ConPTY" paragraph); CHANGELOG.md — [Unreleased] apogee-zwvj entry; CONTEXT.md — Console entry
**Tests:** none (docs only).
**Acceptance:**
- `grep -rn 'stays generic' docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md` shows the line only with the superseding note beside it.
- `perl -0ne 'print "$ARGV\n" if /stays\s+generic/' docs/adr/*.md docs/design/*.md docs/manual/*.md CONTEXT.md` prints only the ADR 0059 path.
- `grep -n 'reopen it fenced' docs/manual/configuration.md docs/design/confinement-execution-contract.md` matches in both files.
- `grep -n 'The Approval prompt is unchanged' CHANGELOG.md` matches nothing.
**Commit:** `docs(confine): describe the Console-specific approval prompt`

## 3. The bound server knows it came from --endpoint

**What:** Recast at the regression check (2026-10-04).
**Goal:** `domain.Config` carries a bool marking the bound server as an ephemeral `--endpoint` entry. It is true after the startup bind of an `--endpoint` run, after a `/server` switch to the synthesized `--endpoint` row, in a headless firing started with `--endpoint`, and in a daemon firing whose endpoint comes from APOGEE_ENDPOINT. It is false for every configured `servers:` entry, including after a `/server` switch away from the ephemeral one.
**Approach (assumed at the header base):**
- `config.ServerEntry` gains `Ephemeral bool` with `yaml:"-"`. Set it in `resolveStartupEntry`'s endpoint branch and on the synthesized row in `upstreamChoices` (`cmd/apogee/upstream.go`).
- `domain.Config` (`internal/domain/config.go`) gains a sibling of `ServerName`/`Vision` (e.g. `ServerEphemeral`), and `serverBinding` (`internal/agent/serverbinding.go`) a matching `*bool` field that `applyTo` projects exactly as `Vision`. It is a field of the server binding: `agent.UpstreamSpec` (`internal/agent/rebind.go`) gains it and `UpstreamSpec.binding` states it present (from the bound entry — the switch spec in `cmd/apogee/upstream.go` sets it); `DelegationTarget.binding` states it present and always false (targets are configured entries; no `DelegationTarget` field); `RebindSpec.binding` leaves it ABSENT, so every beat's `Rebind` keeps it exactly as `Vision` is kept. There is no `agent.RebindSpec` field for it and no edit to `Rebind`. Set it wherever `ServerName` is set from an entry: `cmd/apogee/wire_server.go` (startup bind) and `cmd/apogee/wire_firing.go`.
**Regression guard.** The ephemeral flag is a field of the server binding carried on `UpstreamSpec` (present, from the bound entry) and `DelegationTarget` (present, always false), and is ABSENT from `RebindSpec`, so every beat's `Rebind` keeps it, exactly as `Vision` is kept; there is no `agent.RebindSpec` field for it and no edit to `RebindSpec`/`Rebind` in `internal/agent/rebind.go` for it (the file changes only for the `UpstreamSpec` field). Yields to `internal/agent/serverbinding.go:57-59` (server facts never ride `RebindSpec`, which would reset them on every rebind).
**Files:** internal/config/config.go, internal/config/config_test.go, internal/domain/config.go, cmd/apogee/wire_server.go, cmd/apogee/wire_server_test.go, cmd/apogee/upstream.go, cmd/apogee/upstream_test.go, cmd/apogee/wire_firing.go, cmd/apogee/request_extra_test.go, internal/agent/rebind.go (`UpstreamSpec` field only), internal/agent/serverbinding.go, internal/agent/serverbinding_test.go
**Read first:** internal/agent/serverbinding.go — serverBinding.applyTo, UpstreamSpec.binding, DelegationTarget.binding; internal/config/config.go — resolveStartupEntry; cmd/apogee/upstream.go — upstreamChoices, sessionMover.move; cmd/apogee/wire_server.go — serverBinder.bind; cmd/apogee/wire_firing.go — bindFiringConfig
**Tests:**
- `internal/config/config_test.go`: extend `TestApplyConfigEphemeralEntryIsUnnamed` (or a sibling) — the `--endpoint` startup entry has `Ephemeral` true; a configured `--server` entry false. `TestApplyConfigHoldsTheStartupEntry`'s endpoint-override `want` gains `Ephemeral: true`.
- `cmd/apogee/upstream_test.go`: the synthesized row in `upstreamChoices` is ephemeral (`TestUpstreamChoicesAssembly`'s `ephemeralRow` gains `Ephemeral: true`); a new `TestMoveCarriesTheEphemeralFlag` (the name contains `Ephemeral` so the Acceptance `-run` selects it) switches to that row and back through `sessionMover.move` with `fakeSwitcher` (`wire_helpers_test.go`), asserting the recorded `UpstreamSpec` flag true then false.
- `internal/agent/serverbinding_test.go`: add the flag to `bindingCells`/`cellsOf` and set it true in `bindingParent`, so a row that never states it shows the parent's true; `TestServerBindingApplyTo` rows for the flag — `UpstreamSpec` present, `DelegationTarget` present-false (flips the parent's true), `RebindSpec` absent (keeps the parent's true).
- `cmd/apogee/wire_server_test.go` and `cmd/apogee/request_extra_test.go`: `Ephemeral`-named siblings of `TestServerBindHandsTheEntrysBoundsToTheEngine` (recording build) and `TestFiringConfigCarriesTheEntrysRequestExtra` cover the startup bind and the firing.
**Acceptance:**
- `go build ./... && go vet ./internal/config/ ./cmd/apogee/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Ephemeral|HoldsTheStartupEntry' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'UpstreamChoices|SwitchServer|Ephemeral' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Rebind|ServerBindingApplyTo' ./internal/agent/`
**Commit:** `feat(config): mark the bound server as an --endpoint entry`

## 4. Vision refusal gives --endpoint advice, from one source

**What:**
**Goal:** On a bound `--endpoint` server without vision, every image refusal — Submit/Interject gate, image `@ref`, and the TUI ctrl+v flash — reads `server "<name>" does not accept images: an --endpoint server cannot turn vision on — add a servers: entry for it with vision: true and start with --server <name>`. Configured servers keep today's named and unnamed wordings byte for byte. The TUI flash takes its wording from the engine, not a restated constant. Depends on item 3. Fixes `apogee-endpoint-vision-advice`.
**Approach (assumed at the header base):**
- `(*Agent).visionRefusal` in `internal/agent/loop.go` gains a third format constant chosen when the bound config's ephemeral flag is true; its two callers (`refImageRefusal`, `checkInputImages`) are unchanged.
- Widen the TUI's `visionReporter` interface with `VisionRefusal() string`, served by `Agent` (`internal/agent/agent.go`) and forwarded by `lateEngine` (`cmd/apogee/wire_engine.go`). `internal/tui/clipboard.go` uses it and falls back to `noVisionNote` only when the engine is unbound.
**Regression guard.** The constant is `server %q does not accept images: an --endpoint server cannot turn vision on — add a servers: entry for it with vision: true and start with --server <name>`, the trailing `<name>` literal. `lateEngine.VisionRefusal()` returns "" while unbound; `clipboard.go` reads "" as the cue for `noVisionNote`. The ctrl+v flash on a named configured server changes from `noVisionNote` to the engine's named wording — intended; this item's changelog sidecar entry says so. An APOGEE_ENDPOINT-started `apogee daemon` Firing gets this wording though that command has neither flag — accepted, superseding for this case `internal/config/options.go:117-122` (ServerFlagBound).
**Files:** internal/agent/loop.go, internal/agent/loop_test.go, internal/agent/agent.go, cmd/apogee/wire_engine.go, cmd/apogee/wire_engine_test.go, internal/tui/clipboard.go, internal/tui/prompteditor_test.go
**Read first:** internal/agent/loop.go — visionRefusal, visionRefusalFormat, unnamedVisionRefusal; internal/tui/clipboard.go — visionReporter, foldClipboardImage, noVisionNote; cmd/apogee/wire_engine.go — lateEngine.Vision; internal/tui/prompteditor_test.go — visionEngine
**Closes:** apogee-endpoint-vision-advice
**Tests:**
- `internal/agent/loop_test.go`: add an ephemeral case to `TestImageVisionRefusal_NamesNoBlankServer` driving both the submit and the `@ref` path, asserting the exact string above; existing named/unnamed cases unchanged.
- `internal/tui/prompteditor_test.go`: add a non-vision double (`Vision()` false, `VisionRefusal()` a distinct sentinel) and have `TestPasteCtrlVImageOnANonVisionServerIsRefused` assert the flash equals it; keep a bare-`fakeEngine` case pinning the `noVisionNote` fallback. `visionEngine` gains `VisionRefusal()` so it still satisfies the widened `visionReporter`.
- `cmd/apogee/wire_engine_test.go`: extend `TestLateEngineVisionFollowsTheBoundServer` — `VisionRefusal()` is "" while unbound and the engine's wording once bound.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Image|Vision' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'PasteCtrlV' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'LateEngineVision' ./cmd/apogee/`
**Commit:** `fix(agent): give actionable vision advice on an --endpoint run`

## 5. Vision advice — docs follow the code

**What:**
**Goal:** The docs that quote the vision refusal or describe `--endpoint` state that an `--endpoint` server cannot turn vision on and quote the item-4 wording for that case. Depends on item 4.
**Approach (assumed at the header base):**
- `docs/manual/configuration.md`: the "Images — `vision:`" section and the `--endpoint` override section.
- `docs/manual/commands.md`: the image `@ref` and ctrl+v passages.
- `CONTEXT.md`: the Image input entry.
- Rule for finding sites: every doc line quoting `does not accept images` or describing `--endpoint` alongside vision — `grep -rn -E 'does not accept images|vision' docs/manual/ CONTEXT.md`.
**Regression guard.** `docs/manual/commands.md` (the image `@ref` passage, :88) quotes the refusal, so the Acceptance grep checks it too.
**Files:** docs/manual/configuration.md, docs/manual/commands.md, CONTEXT.md
**Read first:** docs/manual/configuration.md — "Images — `vision:`" section, "An override runs one session elsewhere" paragraph; docs/manual/commands.md — image `@` reference passage, `⌃v` attach passage; CONTEXT.md — Image input entry; CHANGELOG.md — Unreleased unnamed-vision-refusal entry
**Tests:** none (docs only).
**Acceptance:**
- `grep -n 'an --endpoint server cannot turn vision on' docs/manual/configuration.md docs/manual/commands.md CONTEXT.md` matches in all three.
**Commit:** `docs(vision): explain the --endpoint vision refusal`

## 6. A fork starts at zero spend

**What:**
**Goal:** After `/fork`, the forked session's `/usage` delegate and session rows, footer spend and first saved `Meta.DelegateUsage` count only calls the fork itself made; the parent's pre-cut sub-agent and Workflow spend is in neither the fork's transcript record nor its sums. Copied run cards keep their context fill. The parent's live transcript and saved record are unchanged. Fixes `apogee-fork-inherits-delegate-spend` (also an ADR 0093 decision 6 breach when the parent's label differs).
**Approach (assumed at the header base):**
- In `forkAt` (`internal/tui/fork.go`), clear `usage` on every entry of the prefix from `transcript.prefixThrough` before `entriesToRecords`. Clear it on copies, never on entries the parent's transcript still holds. That one slice feeds both `SessionHost.Fork` and `forkRecord`, so they keep agreeing. Keep `ctxUsed`, `ctxLimit`, `ctxModel`.
- A copied background-Workflow finish note then fails `carriesWorkflowSpend` and renders as a plain note with its text (ratified).
- Doc sites: every line stating what a fork carries or starts at — `grep -n -i -E 'fork' CONTEXT.md docs/manual/*.md | grep -i -E 'spend|usage|cost|zero'`.
**Regression guard.** The test builds on `newForkModel` (`fork_test.go`) — `usageModel` has no session host, so `/fork` never forks — and seeds the head with `delegate` (`usage_test.go`) using a Usage with `PricedCalls>0` and `CostMicros>0`, placed before the next depth-0 prompt so it lands inside `prefixThrough`'s cut. The doc grep matches nothing today: the doc work is additions — one sentence in the `sessions.md` `/fork` bullet (:71-76), one beside CONTEXT.md's `Meta.ParentID` fork sentence (:613). Comments: every one claiming the fork prefix/blob equals the parent's (`transcriptbridge.go:47-49` today), found with `grep -n -i 'fork\|prefixThrough' internal/tui/{transcriptbridge,transcript,fork}.go`.
**Files:** internal/tui/fork.go, internal/tui/fork_test.go, internal/tui/transcriptbridge.go, CONTEXT.md, docs/manual/sessions.md
**Read first:** internal/tui/fork.go — forkAt; internal/tui/transcript.go — prefixThrough, carriesWorkflowSpend; internal/tui/usage.go — delegateUsageTotal, spendText; internal/tui/fork_test.go — newForkModel; internal/tui/seam_test.go — fakeSessionHost.Fork; internal/tui/usage_test.go — delegate
**Closes:** apogee-fork-inherits-delegate-spend
**Tests:** `internal/tui/fork_test.go` — on `newForkModel`, seed a priced sub_agent head with `delegate` (`usage_test.go`; `PricedCalls>0`, `CostMicros>0`) before the next depth-0 prompt, run the fork: the records passed to `fakeSessionHost.Fork` carry zero `Usage*` and keep `CtxUsed`/`CtxLimit`/`CtxModel`; after the fork folds in, `delegateUsageTotal()` and the footer spend are zero and the child's first Save carries `delegateUsage` zero (`savedCall`, `seam_test.go`); the parent model's `delegateUsageTotal()` is unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Fork' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Usage' ./internal/tui/`
**Commit:** `fix(tui): start a fork at zero spend, delegate spend included`
