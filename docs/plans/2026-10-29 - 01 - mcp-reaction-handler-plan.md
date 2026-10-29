# The mcp: Reaction handler

**Goal:** A user Reaction's `run:`, `advise:` or `gate:` key may name a tool on a connected MCP server, `{mcp: {server:, tool:, args:, payload-arg:}}`, called at the Moment without the Approver because the user authored it. Drivers that dial no MCP server leave such an entry inert with one notice. This settles ADR 0076 D3's `mcp:` reservation.
**Date:** 2026-10-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 594a7a08
**Closes:** mcp-handler-posture
**Sources:**
- Issue `mcp-handler-posture` (`capcom show mcp-handler-posture`); ADR 0076 D3, D6, D8 and `adr-0076-amend-1021` (structural bar); ADR 0104 (adopted repo entries, repo servers); ADR 0031 (wire-silent engine)
- `docs/design/mcp-client.md`; `docs/manual/reactions.md`

**Ratified design calls:**
- **Case** (owner, 2026-10-29): `advise:` at `session-started` calling a llama-launcher status tool; an observe entry is the second test.
- **Trust** (owner): authorship is the approval — the call never reaches the Approver.
- **Classes** (owner): `run:`, `advise:`, `gate:` all accept `mcp:`; gate rules unchanged (never says yes; failure escalates to ask).
- **Drivers without MCP** (owner): headless, daemon and bench Firings leave the entry inert with one notice per entry; never a load error.
- **Gate without MCP** (owner, 2026-10-29): in a Driver with no resolver, advise and observe mcp: entries are dropped before arming; an mcp: gate stays armed and escalates to ask without calling anything, so a Firing's deny-all Approver refuses every call it gates.
- **Confine** (owner): runs with `confine-to-workspace` on; it spawns no process; an advise answer is secret-redacted.
- **Shape** (owner): a mapping with `mcp:` is MCP, one with `url:` a webhook; `args:` static; `payload-arg:` puts the SeamPayload JSON object under that argument name; naming a key `args:` already holds is refused at load.
- **Results** (owner): text blocks joined = the answer, read as argv stdout; isError, timeout, server not connected, tool missing, pin-skipped server = handler failure.
- **Engine seam** (owner): `domain.MCPHandler` is pure data; the Driver injects a resolver; the engine never imports `internal/mcp`.
- **Repo scope** (owner): an adopted repo entry may name only a server its own Project config declares and does not shadow; a global entry may name any server.
- **Record** (owner): `adr-0076-amend-1029`. ADR 0034 D5 is left untouched (writer: it governs the daemon schedule's `run:`, not Reaction handlers).
- **Resolver shape** (writer): `domain.MCPResolver func(server, tool string, project bool) (MCPCall, error)`, `MCPCall func(ctx, args json.RawMessage) (string, error)`; the answer is the joined text with no `(mcp tool returned no content)` placeholder, so an empty answer stays silent advice.
- **Notice cadence** (writer): a resolution failure (no server, no tool, wrong layer, pin-skipped) reports once per entry per Agent; a call failure (isError, timeout) reports every time, as argv does.
- **Inert-notice wording** (writer, 2026-10-29): 'MCP servers are not connected in this run' — true for headless, daemon, probe and a TUI /schedule Firing alike.

**Standing requirements:**
- skills: coding-standards
- Test-run rules for this machine (pass them to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never `-race` or `-cover` over a whole package. Never two test runs at once.
- Every user- and model-facing string not named in an item stays byte-identical.

**Out of scope:**
- `non-tool-advise-slot` (the pre-request tail slot); `user-view-reaction-cell`.
- Dialling MCP servers in headless or daemon; `mcp:` in the daemon schedule's `run:` (ADR 0034 D5).
- Per-tool MCP trust or annotation use for model calls.

**Interim window:** from item 3 to item 8 an `mcp:` entry loads before the lanes handle it; the run branch is fast-forwarded whole, so no shipped tree carries the gap.

**Regression check (2026-10-29, 594a7a08):**
- 2: guard folded
- 3: guard folded; the interim window recorded in the header (writer decision)
- 4: guard folded
- 5: guard folded
- 6: recast (writer decision); guards folded
- 7: guard folded (writer decision); yields to `internal/agent/sessionadvise.go`'s boundary rule
- 8: guard folded (writer decision)
- 9: recast (writer decision); guards folded
- 10: guard folded
- 6 (re-check): guards folded — Goal reworded to the no-report nil-resolver rule; sync-lane comment sweep; supersedes askGate's doc (`internal/agent/gate.go:129-135`)
- 9 (re-check): recast (writer decision: one shared no-resolver filter for every Firing path and `apogee probe context`; new notice wording); guards folded — Goal names both notices, wireSession-driven TUI test, notices deduped per (id, text), schedule_test.go and mcp_cmd_test.go in Files

## 1. Record the decision as adr-0076-amend-1029 — ✅ DONE (2026-10-29)

**What:**
**Goal:** `capcom know show adr-0076-amend-1029` prints the decision this plan builds; ADR 0076's Status line and reading-order list name it; `non-tool-advise-slot` carries a log line saying it is not unblocked by this work.
**Approach (assumed at the header base):** `capcom know add --id adr-0076-amend-1029 --parent adr-0076` with a title "ADR 0076 amendment of 2026-10-29: the mcp: handler" and one bullet per Ratified design call above (owner-decided ones first), stating that D3's "`mcp:` handlers are reserved" sentence and the rejected-list row "`mcp:` handlers now, inheriting the tool's posture" are settled by authorship trust, not by inheritance. Edit `.capcom/knowledge/decisions/adr-0076.md`: append the amendment to the Status line (`decision 3's mcp: reservation settled by adr-0076-amend-1029`) and to the reading-order list after `adr-0076-amend-1027`. Decision text stays as written. Then `capcom log non-tool-advise-slot "2026-10-29: mcp-handler-posture settled (adr-0076-amend-1029) on existing slots; an mcp: handler rides any slot once it exists, so this issue gains no dependency."` Run `capcom check`.
**Files:** .capcom/knowledge/decisions/adr-0076.md; .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1029.md; .capcom/issues/non-tool-advise-slot.md
**Read first:** .capcom/knowledge/decisions/adr-0076.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0076/adr-0076-decision.md — D3 `mcp:` reservation;
.capcom/knowledge/decisions/adr-0076/adr-0076-rejected.md — "`mcp:` handlers now" row; .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1027.md — amendment form;
.capcom/issues/non-tool-advise-slot.md — Log
**Tests:** none (knowledge only).
**Acceptance:** `capcom know show adr-0076-amend-1029`; `capcom know show "ADR 0076" | grep -c amend-1029` ≥ 2; `capcom check` reports no problems.
**Commit:** `docs(decisions): adr-0076-amend-1029 settles the mcp: handler posture`

## 2. domain.MCPHandler and the resolver types

**What:**
**Goal:** `domain.MCPHandler{Server, Tool string; Args map[string]any; PayloadArg string}` is a sealed `Handler` kind serving observe, advise and gate with the same Moment rules as `ArgvHandler`; `Validate` refuses a blank server or tool and a `PayloadArg` that is also a key of `Args`; `domain.Config` carries an optional `MCP domain.MCPResolver`; the root facade re-exports `MCPHandler`, `MCPResolver`, `MCPCall`.
**Approach (assumed at the header base):** In `internal/domain/reaction.go` add the type with `seam()` returning `""`, include it in `isAsyncHandler` and every async-kind rule in `Validate`, extend `validateHandlerRunnable` (errors keyed by `handlerKey(class)`, e.g. `advise: mcp server is blank`), and rewrite the `Handler` doc comment that says "An MCP handler is the kind still to join them" and `ErrInvalidReaction`'s kind list. The class-check error that names "a command or webhook" stays byte-identical for argv and webhook. Add `MCPResolver`/`MCPCall` (header Resolver shape) beside `BindProjectRun` in `internal/domain/config.go`. In `apogee.go` re-export the three names and fix the stale "a webhook serves observe alone" comment.
**Regression guard.** Add `_ apogee.MCPHandler`, `_ apogee.MCPResolver`, `_ apogee.MCPCall` to `example_test.go`'s alias var block (the ADR 0010 completeness guard), so `go vet .` fails without the re-exports.
**Files:** internal/domain/reaction.go; internal/domain/reaction_test.go; internal/domain/config.go; apogee.go; example_test.go
**Read first:** internal/domain/reaction.go — Handler, isAsyncHandler, Reaction.Validate, validateHandlerRunnable, handlerKey; internal/domain/config.go — Config.BindProjectRun;
apogee.go — ArgvHandler/WebhookHandler alias block; example_test.go — completeness-guard var block
**Tests:** table rows in `reaction_test.go`: MCP advise at `session-started` valid; MCP gate at `pre-tool-exec` valid; MCP advise at `pre-request` refused; blank server/tool refused; payload-arg colliding with an args key refused.
**Acceptance:** `go build ./... && go vet ./internal/domain/ . && GOMEMLIMIT=2GiB go test -count=1 -run 'Reaction|Validate' ./internal/domain/`
**Commit:** `feat(domain): an MCPHandler kind and the resolver a Driver injects`

## 3. Parse the mcp: mapping in reactions:

Depends on item 2.

**What:**
**Goal:** a `run:`/`advise:`/`gate:` mapping whose only key is `mcp:` loads as `domain.MCPHandler`; `mcp:` must be a mapping of `server`, `tool`, `args` (mapping), `payload-arg` (string), any other key a shape error; the shape error reads `%s: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`; an MCP entry contributes no names to `ReactionEnvNames`.
**Approach (assumed at the header base):** In `internal/config/reactions.go` branch in `handlerFor` before `webhookFromMapping` on the `mcp` key (a mapping with both `mcp` and `url` is a shape error); new `mcpFromMapping`; update `shapeError`'s text and the `ReactionEnvNames` switch. The exec pin (`execPin`) pins the decoded mapping whole; confirm `pinnedFiles` does not treat `server`/`tool` strings as files it must hash, and exclude the `mcp` mapping from that scan if it does.
**Regression guard.** Write "an MCP repo entry's pin is stable across loads" through the layered load (`loadLayeredOffer`: the entry is Proposed with the same Fingerprint on two loads), never against `reactionConfig.execPin` alone — `execPin` pins any decoded value unparsed and `textsOf` reads a mapping as no file, so such a test passes at base. `execpin.go` needs no edit.
**Files:** internal/config/reactions.go; internal/config/reactions_test.go; internal/config/execlayer_test.go
**Read first:** internal/config/reactions.go — handlerFor, webhookFromMapping, shapeError, ReactionEnvNames; internal/config/execpin.go — reactionConfig.execPin, textsOf;
internal/config/execlayer_test.go — TestRepoReactionProposalCarriesItsActionShapes; internal/config/reactions_test.go — TestLoadFileConfigRefusesMalformedReactions
**Tests:** parse rows for each class; unknown key under `mcp:`; `mcp`+`url` together; the shape-error table updated to the new text; an MCP repo entry's pin is stable across loads, through `loadLayeredOffer` (Proposed, same Fingerprint on two loads).
**Acceptance:** `go vet ./internal/config/ && GOMEMLIMIT=2GiB go test -count=1 -run 'Reaction|Shape|Pin' ./internal/config/`
**Commit:** `feat(config): reactions: accepts an mcp: handler mapping`

## 4. A repo entry may name only its own Project config's servers

Depends on item 3.

**What:**
**Goal:** an adopted-or-pending repo `reactions:` entry whose `mcp.server` is not a server its own Project config declares (declared and not dropped as shadowing a global name) is refused at load with the notice `apogee: repo reaction %q names mcp server %q, which this Project config does not declare; it is not loaded` (form of the neighbouring `repoReactionRefusedNotice`); a global entry naming any alias loads.
**Approach (assumed at the header base):** in `internal/config/execlayer.go` `pinRepoReactions` (or `repoArmed`), with `layer.servers` in scope after `pinRepoServers`, check each repo entry's `MCPHandler.Server`; add the notice constant beside `repoReactionShadowedNotice`.
**Regression guard.** Run the server check inside `reactionConfig.repoArmed` on `mapped`, before the parked (`enabled: false`) early return, so a parked entry is checked too; the allowed set is `layer.servers` less `pinRepoServers`' `globalNames`, passed into `pinRepoReactions`. Emit the refusal through `repoReactionRefusedNotice` with reason `it names mcp server %q, which this Project config does not declare` (this replaces the Goal's literal), and pin that text in the test.
**Files:** internal/config/execlayer.go; internal/config/execlayer_test.go
**Read first:** internal/config/execlayer.go — pinRepoServers, pinRepoReactions, reactionConfig.repoArmed, repoReactionRefusedNotice;
internal/config/execlayer_test.go — TestRepoReactionRefusedAloneKeepsTheRest, TestRepoMCPServerNameCollisionKeepsTheGlobal, loadLayeredOffer, layerFixture
**Tests:** repo entry naming its own server loads; naming a global-only server refused with the exact notice (`repoReactionRefusedNotice` with the guard's reason); a parked (`enabled: false`) entry naming a global-only server refused; naming a repo server that shadows a global name refused; global entry naming a repo server loads.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 -run 'Repo|Layer' ./internal/config/`
**Commit:** `feat(config): a repo mcp: reaction reaches only its own Project servers`

## 5. internal/mcp builds the resolver

Depends on item 2.

**What:**
**Goal:** `mcp.Resolver(tools func() []domain.Tool) domain.MCPResolver` resolves `(server, tool, project)` to an `MCPCall` over the live tool list by server alias and the server's own tool name, refusing a project entry whose match is not `ProjectServer()`; the call returns the text blocks joined with `\n` (empty when the server returns none), and isError, a transport error or a timeout as a Go error carrying the server's text.
**Approach (assumed at the header base):** in `internal/mcp/tool.go` add `RemoteName()` on `serverTool`; factor `Execute`'s call-and-render path so the reaction call shares the transport and the 2 MiB clip but skips the no-content placeholder; resolution errors name the alias and tool (`mcp server %q is not connected`, `mcp server %q has no tool %q`). The `tools` func is read per call so a reconnect is seen.
**Regression guard.** `Resolver` resolves over an exported optional interface (`ServerAlias`, `RemoteName`, `ProjectServer` plus the text-call method) that `serverTool` satisfies, never over the unexported `serverTool`, so a `cmd/apogee` stub tool can stand in (item 9). `Execute`'s error-result wordings (`mcp: call timed out after %s`, `mcp: call failed: `) stay byte-identical.
**Files:** internal/mcp/tool.go; internal/mcp/tool_test.go
**Read first:** internal/mcp/tool.go — serverTool, Execute, renderContent, clipText, ServerAlias, ProjectServer; internal/mcp/tool_test.go — fakeCaller;
cmd/apogee/wire_helpers_test.go — mcpFixtureTool
**Tests:** resolve by alias+remote name when the model name is truncated/hashed; unknown server; unknown tool; project entry against a global server refused; empty content → `""`; isError → error with text; a stub tool that is not a `serverTool` but implements the exported interface resolves; `TestExecute_CallTimeoutIsErrorResult` still passes unchanged.
**Acceptance:** `go vet ./internal/mcp/ && GOMEMLIMIT=2GiB go test -count=1 -run 'Resolver|RemoteName|Tool' ./internal/mcp/`
**Commit:** `feat(mcp): a resolver for mcp: reaction handlers`

## 6. Sync lane: runSyncMCP and the gate

Depends on item 2.

**What:**
Recast at the regression check (2026-10-29).
**Goal:** a gate entry with an `MCPHandler` is answered by its MCP call through `gateAnswer` with argv's parse and escalation rules; the call runs under `syncTimeout`, takes no SubprocessPermit, never reaches the Approver, sends `Args` plus the stamped seam document under `PayloadArg`; a project entry passes `checkProjectPin` first; with `cfg.MCP` nil the gate escalates to ask with no report; a resolution failure from a non-nil resolver reports once per entry per Agent; a call failure every time via `reportReaction`.
**Approach (assumed at the header base):** in `internal/agent/syncexec.go` add `runSyncMCP(ctx, r, payload)` beside `runSyncWebhook`, reusing `syncDocument` for the payload bytes and a per-Agent once-set keyed by reaction id for resolution failures; wire the `MCPHandler` case into `gateAnswer` in `internal/agent/gate.go`.
**Regression guard.** with cfg.MCP nil, runSyncMCP returns a handler failure that the gate escalates to ask WITHOUT any report (item 9's Firing notice owns it); the once-per-entry-per-Agent report applies only to resolution failures from a non-nil resolver; replace the test "resolver nil → ask, one report across two calls" with "resolver nil → ask, no report".
Also binding: the signature is `runSyncMCP(ctx, turn, r, doc)`, matching `runSyncArgv`/`runSyncWebhook`; it never reports itself but returns a typed resolution error, and each report site (`askGate`, `adviseUser`, `attachSessionAdvice`) checks the once-set for that type before `reportReaction`. The once-set is nil-safe and mutex-guarded (lazily allocated under its own mutex, or a `sync.Map`), because `backgroundHost` (`background.go`) builds an Agent without `New`.
Also binding: this supersedes askGate's doc that every unanswered gate is reported (`internal/agent/gate.go:129-135`); update every comment in `internal/agent/gate.go` and `syncexec.go` that lists the sync lane's handler kinds or says a failed gate is always reported — find them with `grep -n 'three handler shapes\|is reported once\|webhook POSTed\|out-of-process kinds' internal/agent/gate.go internal/agent/syncexec.go`.
**Files:** internal/agent/syncexec.go; internal/agent/syncexec_test.go; internal/agent/gate.go; internal/agent/gate_test.go; internal/agent/construct.go; internal/agent/background.go
**Read first:** internal/agent/gate.go — gateAnswer, askGate; internal/agent/syncexec.go — runSyncWebhook, syncDocument, checkProjectPin;
internal/agent/background.go — backgroundHost; internal/agent/subagent.go — childCfg (MCP inherited by copy); internal/agent/gate_test.go — gateAgent
**Tests:** gate deny/ask/allow from a stub `MCPCall`; resolver nil → ask, no report; a non-nil resolver's resolution failure → ask, one report across two calls; a background-host gate with a resolution failure reports without panicking; call error → ask, reported each time; args merged with payload under payload-arg; timeout honoured; project pin moved → refused like argv.
**Acceptance:** `go vet ./internal/agent/ && GOMEMLIMIT=2GiB go test -count=1 -run 'Gate|SyncMCP' ./internal/agent/`
**Commit:** `feat(agent): a gate reaction may answer through an MCP tool`

## 7. Advise sites call runSyncMCP

Depends on item 6.

**What:**
**Goal:** an `MCPHandler` advise entry at `post-tool-result`, `file-changed`, `session-started` and `compaction-finished` lands its answer exactly as argv's would — capped, fenced, secret-redacted, fail-open, off under Bypass, on the provenance ledger.
**Approach (assumed at the header base):** add the `MCPHandler` case to `adviseUser` and the post-tool-result `invoke` closure in `internal/agent/reactions.go` and to `callSessionAdvice` in `internal/agent/sessionadvise.go`, each calling `runSyncMCP` and then the existing `tools.RedactSecrets` path.
**Regression guard.** an advise entry with cfg.MCP nil fails open with no report (backstop; Drivers filter such entries in item 9).
Also binding: `callSessionAdvice` only returns the error — it runs on the Session boundary's background goroutine, which never reports (`sessionadvise.go` header; this item yields to that rule) — and `attachSessionAdvice` and `adviseUser` are the sites that apply item 6's once-set to its typed resolution error.
**Files:** internal/agent/reactions.go; internal/agent/sessionadvise.go; internal/agent/advise_test.go; internal/agent/sessionadvise_user_test.go
**Read first:** internal/agent/reactions.go — adviseUser, seamPayload (MomentPostToolResult invoke switch); internal/agent/sessionadvise.go — callSessionAdvice, attachSessionAdvice, sessionAdviseSpecs;
internal/agent/sessionadvise_user_test.go — reportLines, TestSessionAdvise_AUserHandlerFailsOpen; internal/agent/advise_webhook_test.go — TestAdviseWebhookIsSkippedUnderBypass
**Tests:** session-started MCP advise lands fenced in the first user message (the case); post-tool-result trailer; empty answer → no span; a secret in the answer redacted; Bypass on → no call made; cfg.MCP nil → no span and no report; a resolution failure from a non-nil resolver reported once across two firings, at the join.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 -run 'Advise|SessionAdvice' ./internal/agent/`
**Commit:** `feat(agent): advise reactions may answer through an MCP tool`

## 8. Async lane: observe entries call MCP

Depends on items 2 and 5.

**What:**
**Goal:** the observe Runner's default executor runs an `MCPHandler` entry through an injected `domain.MCPResolver` under the entry's timeout, payload as in item 6, answer discarded; with no resolver the run is a failure reported once (the worker's existing repeat suppression).
**Approach (assumed at the header base):** in `internal/reactions/exec.go` give `DefaultExecutor` the resolver (a late-bound slot like the `projectRunSlot` precedent if the TUI builds the Runner before MCP connects) and add the case in `defaultExecutor.Run`, whose "neither a command nor a webhook" default stays for unknown kinds.
**Regression guard.** a nil resolver in the executor is a plain run error with no extra report path (backstop only; item 9 filters observe mcp: entries before arming); item 8 also updates every cmd/apogee caller of reactions.DefaultExecutor (wire_boot.go, wire_firing.go) to pass a nil resolver so `go build ./...` stays green between items 8 and 9.
Also binding: the widened `DefaultExecutor` moves its test callers too (`command_test.go`, `webhook_test.go`, `runner_test.go`). The executor calls `resolver(server, tool, r.Project)`, and for `r.Project` admits `r.Pin` first — a nil Pin refused — as argv's project path does (`commandExecutor.runProject` → `RunProjectReaction`) and item 6 does.
**Files:** internal/reactions/exec.go; internal/reactions/runner_test.go; internal/reactions/command.go; internal/reactions/command_test.go; internal/reactions/webhook_test.go; cmd/apogee/wire_boot.go; cmd/apogee/wire_firing.go
**Read first:** internal/reactions/exec.go — DefaultExecutor, defaultExecutor.Run; internal/reactions/runner.go — runOne (lastFailure repeat suppression); internal/reactions/command.go — commandExecutor.runProject;
internal/reactions/runner_test.go — fakeExecutor, TestRunnerReportsAFailureOnceUntilTheHookSucceeds; cmd/apogee/wire_firing.go — projectRunSlot; cmd/apogee/wire_boot.go — projectRuns slot at the Runner build
**Tests:** observe MCP entry fires with payload-arg; nil resolver → one failure line across repeats; timeout; a project entry resolves with `project` true and is refused with a nil Pin.
**Acceptance:** `go build ./... && go vet ./internal/reactions/ && GOMEMLIMIT=2GiB go test -count=1 -run 'Exec|Runner|MCP' ./internal/reactions/`
**Commit:** `feat(reactions): observe reactions may call an MCP tool`

## 9. The TUI injects the resolver; Firing Drivers leave mcp: inert

Depends on items 5, 6 and 8.

**What:**
Recast at the regression check (2026-10-29).
**Goal:** in the TUI, `domain.Config.MCP` and the observe executor resolve through the live MCP set, so a reconnect is seen without a restart; every Driver path that arms reactions without a resolver (headless, daemon, the TUI's `/schedule` Firing, `apogee probe context`) runs one shared filter that reports through `in.report` (the probe prints it), once per MCP entry at setup: an advise/observe entry is dropped before arming with `reaction %q: its mcp: handler is inert here — MCP servers are not connected in this run`; a gate entry stays armed with `reaction %q: its mcp: gate cannot run here — MCP servers are not connected in this run, so every call it gates is refused`.
**Approach (assumed at the header base):** in `cmd/apogee`, build `mcp.Resolver(w.mcpSet.tools)` from `liveMCP` and set it where `BindProjectRun` is set (`wire_boot.go`) and on the Runner's executor; in `wire_firing.go` `firingConfig`/`firingHooks` report the inert entries through the Firing's notice path.
**Regression guard.** Item 9 is the single owner of the no-resolver notice: at Firing setup, once per mcp: entry — advise/observe: `reaction %q: its mcp: handler is inert here — MCP servers are not connected in this run`; gate: `reaction %q: its mcp: gate cannot run here — MCP servers are not connected in this run, so every call it gates is refused`. Item 9 filters advise/observe mcp: entries out of the Generation before arming (Sync and Observe) and keeps gate entries; tests assert both exact notices once each, no MCP call, a gated call refused, and an advise/observe entry absent from the armed set.
Also binding: the filter runs at the top of `raise` (`wire_firing.go`), before both `LanesOf` reads (`raise`, `firingConfig`), by assignment `in.opts.Reactions = …` (never a composite literal — `TestNoWiringSiteWritesConfigReactions`); its notices go through `in.report`, which reaches all three Drivers including the TUI's `/schedule` Firing (`schedule.go` drops `raise`'s notices slice). In the TUI, set `w.cfg.MCP` and the item-8 executor slot in `wireSession` right after `newLiveMCP` and before `serverBinder{cfg: w.cfg}` copies the Config, via `func() []apogee.Tool { return w.mcpSet.tools() }` — never a method value taken in `resolveConfig`, which binds a nil `*liveMCP`. The TUI test resolves through item 5's exported interface, served by the real serverTool `newPingMCPServer` dials (below).
Also binding (writer, 2026-10-29): the no-resolver filter is ONE shared helper, used by every Driver path that arms reactions without a resolver — the Firing path (raise/firingHooks, headless, daemon, the TUI's /schedule Firing) AND `apogee probe context` (cmd/apogee/probecontext.go measureContextCost → measureTurn1 / armedDirectiveReactions), which excludes filtered advise/observe mcp: entries from its armed set and prints the same notice once per entry. The helper dedupes notices by (reaction id, notice text), since one entry with both `run:` and `advise:` as mcp: maps to two Reactions with one ID (`internal/config/reactions.go` entryReactionsIn). The TUI test drives `startupWiring(t, deps, newPingMCPServer(t, "llm"))` + `w.wireSession` (mcp_cmd_test.go) — a real serverTool answering "pong" at session-started — then `w.mcpSet.reconnect` for the reconnect; a hand-made Config never stands in for wireSession.
**Files:** cmd/apogee/wire_boot.go; cmd/apogee/wire_mcp.go; cmd/apogee/wire_live.go; cmd/apogee/wire_firing.go; cmd/apogee/wire_firing_test.go; cmd/apogee/wire_engine_test.go; cmd/apogee/wire_helpers_test.go; cmd/apogee/schedule_test.go; cmd/apogee/mcp_cmd_test.go; cmd/apogee/probecontext.go; cmd/apogee/probecontext_test.go
**Read first:** cmd/apogee/wire_firing.go — raise, firingConfig, firingHooks; cmd/apogee/wire_live.go — wireSession, newLiveMCP; cmd/apogee/wire_mcp.go — liveMCP.reconnect;
cmd/apogee/schedule.go — scheduleWiring.fire; cmd/apogee/mcp_cmd_test.go — startupWiring, newPingMCPServer; cmd/apogee/wire_firing_test.go — TestNoWiringSiteWritesConfigReactions
**Tests:** a wireSession-wired TUI (`startupWiring` + `newPingMCPServer`) answers a session-started MCP advise with "pong" and sees a `w.mcpSet.reconnect`; a Firing with an advise and an observe mcp: entry emits the inert notice once each, makes no MCP call, and neither is in the armed set (the stub runner's `spec.Generation` and the Runner list); a Firing with an mcp: gate emits the gate notice once and the gated call is refused; a `/schedule` Firing's notice reaches `in.report` (`newScheduleHarness`, schedule_test.go); one entry with both `run:` and `advise:` as mcp: emits the inert notice once; `apogee probe context` with an mcp: advise entry prints the inert notice once and does not count it armed (a `TestProbeContext…MCP…` test in probecontext_test.go, so the acceptance pattern runs it).
**Acceptance:** `go build ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -count=1 -run 'Firing|MCP|Resolver' ./cmd/apogee/`
**Commit:** `feat(apogee): wire the mcp: reaction resolver into the TUI`

## 10. Docs and glossary speak the mcp: handler

Depends on items 3, 4 and 9.

**What:**
**Goal:** every user-facing page and glossary Note that lists Reaction handler shapes names three — argv, webhook, MCP tool — and none calls `mcp:` reserved; the manual shows the session-started llama-launcher example, the trust rule, the Driver limit and the repo-scope rule.
**Approach (assumed at the header base):** `docs/manual/reactions.md`: intro "two shapes", the key table's shape-error sentence (item 3's exact text), a new `## Calling an MCP tool` section after `## Sending a webhook`, and lines in `## Where an advise: or gate: command runs` and `## Where a failure shows`. `docs/manual/configuration.md` §Reactions "same two shapes". Glossary `glossary-reactions/reaction.md` handler kinds and loader sentence, `glossary-identity/hook.md`. `docs/design/reaction-core-greenfield.md`: a dated note at each `mcp:` reservation pointing at `adr-0076-amend-1029`. Rule: every site found by `grep -rn 'two shapes\|mcp: handler\|mcp:.*reserved\|argv list or a webhook' docs .capcom/knowledge/glossary`, excluding `docs/plans/archived`, `docs/reviews`, `CHANGELOG.md`.
**Regression guard.** The rule also takes every line of `README.md`, `docs/manual` and `.capcom/knowledge/glossary` matched by `grep -rn 'webhook'` that enumerates a reaction action's shapes (the "command(s) or (POST a) webhook(s)" and "argv or webhook" wordings: `README.md`, `docs/manual/README.md`, `docs/manual/reactions.md`, `docs/manual/configuration.md`'s adoption-pane line); skip `.capcom/knowledge/glossary/glossary-identity/run-view.md`'s unrelated "two shapes". Keep "the eighteen Moments" in `docs/manual/README.md`.
**Files:** README.md; docs/manual/README.md; docs/manual/reactions.md; docs/manual/configuration.md; .capcom/knowledge/glossary/glossary-reactions/reaction.md; .capcom/knowledge/glossary/glossary-identity/hook.md; docs/design/reaction-core-greenfield.md
**Read first:** docs/manual/reactions.md — An entry (key table run:/gate:/advise: rows), Sending a webhook; docs/manual/configuration.md — Reactions section, adoption-pane paragraph;
.capcom/knowledge/glossary/glossary-reactions/reaction.md — handler kinds ("eight kinds"); docs/manual/README.md — Reactions row; internal/domain/reaction_test.go — manualMomentCount;
docs/design/reaction-core-greenfield.md — mcp: reserved notes
**Tests:** the docs drift tests that read these pages.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 -run 'Manual|Doc' ./internal/config/`; `GOMEMLIMIT=2GiB go test -count=1 -run Manual ./internal/domain/`; `capcom check`; the grep above shows no "reserved" `mcp:` claim outside history.
**Commit:** `docs(reactions): the mcp: handler shape`
