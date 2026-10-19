# sub_agent shared context files + per-server stream-idle-timeout — plan

**Goal:** a `sub_agent` call can name `context` files that the engine reads once, through the read_file scope, and renders as one fenced prefix ahead of the child's task, behind a root gate that is off by default. A `servers:` entry can state its own `stream-idle-timeout`, which overrides the root for that server.
**Date:** 2026-10-19
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a926417f

**Regression check (2026-10-19, a926417f):**
- 1: recast (writer decision: `Context` is `json.RawMessage`); guard folded (`context` ArgRole only with the option).
- 2: recast (writer decision: Files, manual entry, nested-child gate); guards folded (firings, HostTools fill pins, config drift gates, `workflow-wake` row shape).
- 3: guard folded (path-prefixed refusals, own binary refusal, virtual mounts by `mount:rel` only).
- 4: recast (writer decision: lazy decode, background refusal, child-bounded render, wider `-run`).
- 5: guard folded (writer decision: extend item 2's entry; Acceptance runs `TestManualDocumentsEverySettingsKey`; config.yaml-only wording).
- 6: guard folded (ValidateServers prefix, whitespace-only as absent, unknownkeys_test.go).
- 7: recast (writer decision: pointer window, DeepEqual; RebindSpec dropped; comment rule); yields to ADR 0082 Decision 1.
- 8: guard folded (writer decision: inherits item 7's shape; DeepEqual sites, callers, liveSettings accessor, window kept out of `Client()`/`Monitor()`).
- 9: guard folded (writer decision: live-edit timing; file-only wording and registry row both stand).
- Re-check (second round):
- 2: guards folded (t16-settings-rows golden regenerated; firingSeatTools takes seat choice from the key, firing test asserts no `run_on`; row takes no Class; `owns` entry).
- 7: amends ADR 0082 Decision 1 (writer decision; replaces the round-1 "yields to" call); guards folded (spec-comparison test moved to item 8; widened comment grep in Acceptance).
- 8: guard folded from item 7 (the equal-windows spec-comparison test lands here).
- 9: recast (writer decision: dated ADR 0082 D1 amendment note, Status line, `capcom check`).

**Sources:**
- `.capcom/issues/sub-agent-shared-files.md`, `docs/reviews/archived/session-mining-2026-09-14.md` (item 12 / quant §5)
- `.capcom/issues/per-server-idle-timeout.md`
- ADR 0023 §6 (system-message order: unchanged, the prefix is user-role), ADR 0087 D8 (new model-facing surface ships off), ADR 0082 (keepalive bytes reset the idle timer), ADR 0096 (the `servers` row is ClassGlobalOnly)

**Ratified design calls** (owner, 2026-10-19):
- **Arg name:** `context` (the same name fan_out uses), not `files`.
- **Gate:** a root key `sub-agents-context: off|on`. It is file-only, defaults to `off` and is shaped like `sub-agents-choice`. When off, the plain schema stays byte-identical and a stray `context` arg is ignored rather than refused. Nested children follow the same gate.
- **Bad file:** paths resolve under the read_file scope rules (workspace plus `domain.ReadMounts`). If any path is missing, out of scope, binary or an image, the whole call is refused before a child exists, with a model-visible error that names the path.
- **Per-server idle key:** `stream-idle-timeout` inside a `servers:` entry, using the same parser. Absent inherits the root, `0` turns the cut off for that server, and a positive value is that server's window. `/server` moves and routed children take the target entry's value, falling back to the root. Keepalive comments still reset the timer.
- **ADR 0082 D1:** amended — the per-server window rides every dial; its value is the bound server's (writer, from the ratified per-server call, 2026-10-19).

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi. Never run `go test -race`/`-cover` over a whole heavy package or a pattern. Narrow to one package and a `-run` filter, prefixed `GOMEMLIMIT=2GiB`. Never run two test runs at once. Pass these rules on to every sub-agent.
- Bubble Tea v2 names only (`tea.KeyPressMsg`).
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Turning `sub-agents-context` on by default. That needs a bench arm in apogee-sim.
- A system-part rendering of the context prefix (that would be an ADR 0023 addendum).
- Glob patterns, `{{SKILL_DIR}}` expansion inside `context` paths, and re-injecting `context` on a `continue` call that does not restate it.
- Changing keepalive/idle-reset semantics, and a `/settings` row for per-entry keys.

## 1. sub_agent schema learns the gated `context` argument — ✅ DONE (2026-10-19)

NOTES (2026-10-19): `SubAgentArgs.Context` carries `json:"context,omitempty"` (the item names only the `json.RawMessage` type) so a SubAgentArgs that engine tests build by marshalling the struct never grows a `"context":null` key; decode behaviour is unchanged.
NOTES (2026-10-19): no CHANGELOG entry — the argument is unreachable until item 2 wires the `sub-agents-context` gate; the user-facing entry belongs with the gate and the render (items 2/4).

**What:** Recast at the regression check (2026-10-19).
**Goal:** `NewSubAgentWith` with the context option set publishes a `context` property (array of strings, described as the files the child reads before its task). Without the option, the schema is byte-identical to `wantPlainSubAgentSchema`. `SubAgentArgs` carries `context` undecoded (`json.RawMessage`). `ArgRoles()` declares `context` as `domain.ArgRoleReadSource` only when the context option is set.
**Approach (assumed at the header base):** in `internal/tools/sub_agent.go`, add a `Context bool` field to `SubAgentOptions`, a `subAgentContextProperty` and a third `%s` hole in `subAgentSchemaTemplate`, filled the same way `run_on`/`background` are. Add `OffersContext()` beside `OffersSeatChoice`/`OffersBackground`, and `Context json.RawMessage` on `SubAgentArgs`. The description wording must not imply the files are re-read by the child.
**Regression guard.** `SubAgentArgs.Context` is `json.RawMessage`, not `[]string`, so every existing SubAgentArgs decoder (runDelegate, dispatch.go askedSeat/stepCapFor, delegationLabel, workflowcall.go backgroundSubAgentResult, cmd/apogee/headless.go narrator) keeps accepting any `context` shape with the gate off; add a test that a string- and an object-valued `context` still decode.
`ArgRoles()` declares `context` only when `t.context` (`OffersContext()`) is set: `TestBuiltinToolArgRolesNameSchemaKeys` walks `builtinTools(…, HostTools{})`, whose sub_agent is the plain variant, so an unconditional entry names a key that schema lacks; gating it also keeps a stray `context` on the plain variant fully inspected by the write rules (dangerous.go), as today.
**Files:** internal/tools/sub_agent.go, internal/tools/sub_agent_test.go, internal/tools/argroles_test.go
**Read first:** internal/tools/sub_agent.go — subAgentSchemaTemplate, SubAgentOptions, NewSubAgentWith, SubAgent.ArgRoles, SubAgentArgs; internal/tools/sub_agent_test.go — TestSubAgentSchema_EachGateAddsOnlyItsProperty, wantPlainSubAgentSchema; internal/tools/argroles_test.go — TestBuiltinToolArgRolesNameSchemaKeys
**Tests:** extend `TestSubAgentSchema_EachGateAddsOnlyItsProperty` for the context gate. Add an args-parse test for `context`, including a string- and an object-valued `context` that still decode. Add an ArgRoles test: `context` is declared with the option and absent without it. `TestBuiltinToolArgRolesNameSchemaKeys` stays green. The plain-schema byte pin stays unchanged.
**Acceptance:**
- `go vet ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSubAgent|TestBuiltinToolArgRoles' ./internal/tools/`
**Commit:** `feat(tools): sub_agent schema carries a gated context argument`

## 2. Root key `sub-agents-context` gates the argument — ✅ DONE (2026-10-19)

NOTES (2026-10-19): tools.HostToolsOf and registryWithMCP take the gate as a new positional bool (`seatChoice, subAgentContext, offersBackground`), next to seatChoice. The engine's own roster (construct.go defaultRoster) passes false.
NOTES (2026-10-19): withoutSeatChoice now rebuilds a child's sub_agent as `NewSubAgentWith(SubAgentOptions{Context: <parent's>})`, so run_on and background are stripped and context is kept. A roster whose sub_agent publishes only `context` comes back unchanged.
NOTES (2026-10-19): the settingsrows_test fixture sets SubAgentsContext true (pinned "on"), so the row is shown to report the value rather than the default.
NOTES (2026-10-19): consequential edit — internal/agent/loop.go: made necessary by withoutSeatChoice keeping `context` (the comment no longer says a child's sub_agent is the "plain variant")
NOTES (2026-10-19): consequential edit — internal/agent/orientation.go: made necessary by withoutSeatChoice keeping `context` (the comment no longer says a child's sub_agent is the "plain variant")
NOTES (2026-10-19): consequential edit — cmd/apogee/probecontext.go: made necessary by firingSeatTools now also building under `sub-agents-context: on` (its doc comments named the seat gate alone)

**What:** Recast at the regression check (2026-10-19).
**Goal:** `sub-agents-context: on` in the global config publishes `context` on every sub_agent tool set (nested children and Firings included), and `off`/absent publishes the plain schema. An invalid value is refused with the key named. The key is file-only and ClassGlobalOnly, with default `off`. The same commit adds the key's entry to `docs/manual/configuration.md`.
**Approach (assumed at the header base):** add a registry row in `internal/config/registry.go` modelled on the `workflow-wake` row (not Editable, no Validate), plus the `fileConfig` field and the `Options` field. Then thread it through `tools.HostToolsOf`/`HostTools` into `builtinToolsWith`, and on to `NewSubAgentWith(SubAgentOptions{Context: …})`. Hold the flag per tool set in `cmd/apogee/wire_tools.go` the same way `seatChoice` is held. Children keep the flag: `withoutSeatChoice` strips only seat choice and background, never `context` (see the guard).
**Regression guard.** correct Files to internal/tools/registry.go (no builtin.go); item 2 also adds the `sub-agents-context` key's entry to docs/manual/configuration.md in the same commit so TestManualDocumentsEverySettingsKey stays green (add that file and that test to Files/Acceptance; item 5 then extends the prose); "nested children follow the same gate" is binding: `withoutSeatChoice`/`defaultSubAgentTools` must rebuild the child's sub_agent with the context option preserved (only seat choice/background stripped), with a test that a nested child offers `context` when the key is on — add the file holding those funcs to Files.
Firings: `firingSeatTools` (cmd/apogee/wire_firing.go) builds a registry when seat choice OR context is on, passing the flag, and stays nil otherwise (headless_test.go pins that). A new `HostTools` field: pass true in both fill pins (`TestHostToolsOfFillsEveryHostField`, `TestHostToolsForFillsEveryHostField`), extend `TestHostToolsOfLeavesSeatChoiceToTheCaller`, and update every `HostToolsOf`/`registryWithMCP` caller (construct.go, workflow_test.go, registry_test.go, wire_live.go's toolSetSpec and rebuild closure).
Config drift gates: a commented `# sub-agents-context: off` in internal/config/defaults/config.yaml beside `sub-agents-choice`, in registry order; config_test.go's `TestEveryConfigKeyReachesTheOptions` want map and `everyKeyFileConfig`; settingsrows_test.go's `TestSettingsRowsFormatEffectiveValues` pin.
The row copies `workflow-wake`, not `sub-agents-choice`: an editable key with no applier fails `TestEveryEditableSettingKeyHasAnApply`, and `TestRegistryValidateHooksSitOnEditableKeys` forbids a Validate hook on a non-editable row, so the refusal is `checkedField`'s parse at the file pass.
Re-check (2026-10-19): the row sits beside `sub-agents-choice` on the settings pane's first screen, so `cmd/apogee/testdata/frames/t16-settings-rows.txt` is regenerated in this commit (`go test ./cmd/apogee -run TestE2ELiveStateFollowsTheRunningSession -update`).
`firingSeatTools` passes seatChoice `opts.SubAgentsChoice == config.SubAgentsChoiceModel` to `registryWithMCP` (not `true`), its "only the seat gate" doc (and the firing test's message) is restated, and the firing test also asserts no `run_on`.
The row takes no `Class` (the ClassGlobalOnly zero value) — not `workflow-wake`'s `ClassProjectParam`, which `TestRegistryKeyClassesArePinned` would reject; the drift gates add the key→field entry to `TestRegistrySetIsTheInverseOfRead`'s `owns` map.
**Files:** internal/config/registry.go, internal/config/config.go, internal/config/options.go, internal/config/registry_test.go, internal/config/config_test.go, internal/config/defaults/config.yaml, internal/tools/registry.go, internal/tools/registry_test.go, internal/tools/workflow_test.go, internal/agent/subagent.go, internal/agent/subagent_test.go, internal/agent/construct.go, cmd/apogee/wire_tools.go, cmd/apogee/wire_tools_test.go, cmd/apogee/wire_live.go, cmd/apogee/wire_firing.go, cmd/apogee/wire_firing_test.go, cmd/apogee/settingsrows_test.go, cmd/apogee/testdata/frames/t16-settings-rows.txt, docs/manual/configuration.md
**Read first:** internal/config/registry.go — workflow-wake row; internal/config/registry_test.go — TestRegistryKeyClassesArePinned, TestRegistrySetIsTheInverseOfRead;
internal/agent/subagent.go — withoutSeatChoice; internal/tools/registry.go — HostToolsOf; cmd/apogee/wire_tools.go — registryWithMCP; cmd/apogee/wire_firing.go — firingSeatTools;
cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateFollowsTheRunningSession
**Tests:** a registry/parse test for on/off/invalid, and a wire test showing that the built sub_agent tool offers context only when the key is on. A nested-child roster test: seat choice on and context on ⇒ the child publishes `context` and no `run_on`. A firing test: context on with seat choice `fixed` ⇒ the firing's sub_agent offers `context` and no `run_on`. The regenerated `t16-settings-rows` golden shows the new row. The two fill pins pass the new field as true; the config drift gates and `TestManualDocumentsEverySettingsKey` stay green.
**Acceptance:**
- `go vet ./internal/config/ ./internal/tools/ ./internal/agent/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SubAgentsContext|Registry|Template|EveryConfigKey' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'HostToolsOf|TestBuiltinTool' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SubAgentsContext|SeatChoice' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SubAgentsContext|TestManualDocumentsEverySettingsKey|HostToolsFor|SettingsRows|EditableSettingKey|TestFiringConfig|TestE2ELiveStateFollowsTheRunningSession' ./cmd/apogee/`
**Commit:** `feat(config): sub-agents-context root key gates the sub_agent context argument`
Depends on item 1.

## 3. Exported scoped text read for engine-side context files — ✅ DONE (2026-10-19)

NOTES (2026-10-19): consequential edit — internal/tools/doc.go: made necessary by the new exported ReadScopedText in path_read.go (the package map's path_read.go entry now names it).
NOTES (2026-10-19): signature is `ReadScopedText(ctx, root string, mounts ReadMounts, path string) (string, error)`; ctx bounds PDF extraction. Image refusal is by extension (raster set incl. .ico; SVG stays text) and runs before any read; binary/PDF handling is rendered in ReadScopedText itself rather than through readableText, per the item's regression guard.

**What:**
**Goal:** `internal/tools` exports one function that reads a path under the read_file scope rules: relative paths go against the workspace, absolute paths under `domain.ReadMounts` Roots/Scratch, or a `mount:path` reference into Virtual. It returns the text, or an error that names the path for a missing, out-of-scope, binary or image file. The function is bounded by `maxFileReadBytes`.
**Approach (assumed at the header base):** wrap the existing `readScope.readBounded` and `readableText` (`internal/tools/path_read.go`, `read_file.go`). This is one deep entry point, not a second resolver: read_file and the new export share the scope code. PDFs stay readable via doctext, as read_file allows. Image extensions are refused.
**Regression guard.** The export prefixes every failure with the caller's path: `readOpenedBounded`'s "file too large: N bytes (max M)" and an `ErrRootInaccessible` text name no file. It renders the binary refusal itself via `looksBinary`, never `readableText`'s `read_file:`-prefixed text. Virtual mounts are reached only by a `mount:rel` reference (`virtualLocate`/`virtualMountRef`), never by an absolute path.
**Files:** internal/tools/path_read.go, internal/tools/path_read_test.go
**Read first:** internal/tools/path_read.go — readScope.readBounded, readOpenedBounded, looksBinary, escapeOrMessage; internal/tools/read_file.go — readableText; internal/tools/path_virtual.go — readScope.virtualLocate;
internal/tools/path_read_test.go — scopeFixture, TestReadScopeReadBounded
**Tests:** a workspace-relative read, an absolute read under a mount Root, a `mount:rel` read into a Virtual mount, a refusal for an out-of-scope path, a refusal for a binary file (NUL sniff, no `read_file:` prefix), a refusal for a missing file, a refusal for an oversize file that names the path, and a refusal for an image.
**Acceptance:**
- `go vet ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ReadScoped|PathRead' ./internal/tools/`
**Commit:** `feat(tools): export the read_file scope as a bounded text read`

## 4. runDelegate renders `context` once ahead of the child's task — ✅ DONE (2026-10-19)

NOTES (2026-10-19): internal/agent/filerefs_test.go (listed in Files) needed no edit — the extracted renderer (refBlock) keeps FileRefs' output byte-identical, and the existing FileRefs tests pass unchanged under the Acceptance -run.
NOTES (2026-10-19): backgroundSubAgentResult now takes the dispatch ctx (workflowCallResult passes it) so the pre-start context read can bound a PDF extraction; the background child re-reads the files when its item starts on the blocking path, so a file removed in between refuses that item instead.
NOTES (2026-10-19): a PDF named in `context` renders as its extracted text with no "(PDF, N pages)" header annotation — tools.ReadScopedText returns no page count; an Agent with no workspace refuses every context path by name.

**What:** Recast at the regression check (2026-10-19).
**Goal:** when the context gate is on, a sub_agent call with `context: [a, b]` produces a child whose first user message holds each file once, fenced in the same "Referenced file `x`:" form FileRefs uses, in argument order, with duplicate paths dropped, followed by the task. Any unreadable path refuses the call before a child is created, with a tool error naming that path. When the gate is off, `context` is ignored. The child's system message is unchanged (ADR 0023 §6).
**Approach (assumed at the header base):** in `internal/agent/subagent.go` `(*Agent).runDelegate`, read every path through item 3's export (with `a.cfg.WorkspaceDir` and `a.cfg.ReadMounts`) after decode and before `newChildAgentOn`. Refuse through the same give-back path that seat and roster refusals use. Extract the per-ref fence and clamp out of `resolveFileRefs` in `internal/agent/loop.go` into one renderer called by both FileRefs and this path, bounded by the child's `sub.refBound`/`sub.clampRef` across the context files, after `newChildAgentOn`. The prefix goes into the single `sub.Submit(domain.UserInput{Text: …})`. On `continue`, only that call's own `context` is rendered, and nothing is stored on `retainedDelegate`. Use `publishesSeatChoice` as the precedent for gate-off ignoring.
**Regression guard.** apply reviewer 2's guards as binding — runDelegate unmarshals the RawMessage to []string only when the gate is on and refuses a bad shape naming `context`; the same read/refusal runs in workflowcall.go backgroundSubAgentResult before startBackground (add workflowcall.go and a background refusal test); read and refuse before newChildAgentOn, but render with the child's sub.refBound/sub.clampRef after newChildAgentOn and before Submit, keeping the task handed to newChildAgentOn bare; widen Acceptance -run to 'SubAgent.*Context|FileRef|ResolveRefs|ResolveSkillRefs|Interject'.
Reviewer 2's guards also bind a gate-off test where a string- and an object-valued `context` still run the child. The bare task matters because it feeds `a.task` → `ApprovalRequest.SubAgentTask` (dispatch.go), `WithSubAgentTask` and the namer.
**Files:** internal/agent/subagent.go, internal/agent/loop.go, internal/agent/workflowcall.go, internal/agent/subagent_test.go, internal/agent/workflowcall_test.go, internal/agent/filerefs_test.go
**Read first:** internal/agent/subagent.go — runDelegate, newChildAgentOn, publishesSeatChoice; internal/agent/loop.go — resolveFileRefs, refBound, clampRef;
internal/agent/workflowcall.go — backgroundSubAgentResult; internal/agent/subagent_test.go — subAgentCallTurn
**Tests:**
- With a scripted upstream (`subAgentCallTurn`, `upstream.requests()`), the child's first request carries both files once, before the task.
- The refusal names the exact path the model sent, and no child request is made.
- A `context` of the wrong shape with the gate on is refused naming `context`.
- A `background: true` call with a bad path is refused before `startBackground`: no "started in the background" answer, no workflow folder.
- With the gate off, the child gets the task alone, and a string- or object-valued `context` still runs the child.
- A duplicate path is rendered once.
- The existing FileRefs tests still pass.
**Acceptance:**
- `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SubAgent.*Context|FileRef|ResolveRefs|ResolveSkillRefs|Interject' ./internal/agent/`
**Commit:** `feat(agent): sub_agent context files are rendered once ahead of the child's task`
Depends on items 1 and 3.

## 5. Manual documents `sub-agents-context` and the `context` argument — ✅ DONE (2026-10-19)

NOTES (2026-10-19): the plan's "no `/settings` row" does not match the tree — item 2's row exists (read-only, `· ⏎ opens $EDITOR`, pinned by t16-settings-rows); the manual says the key is set in config.yaml only, has no live switch, and its row opens the editor.
NOTES (2026-10-19): no manual key list names `sub-agents-choice` beyond configuration.md's own section, so no list gained the key on that rule; instead the start-only key lists (configuration.md "Sixteen" → "Seventeen", commands.md's read-only-rows parenthetical) gained `sub-agents-context`, since the key is read once at start like `workflow-wake`.

**What:**
**Goal:** `docs/manual/configuration.md` extends the `sub-agents-context` entry item 2 added (values, `off` default, set in config.yaml only, the reason it ships off) and, in its delegation section, the sub_agent `context` argument (read scope, refusal rule, rendered once before the task, not re-injected on `continue`). Every manual list of root keys that names `sub-agents-choice` also names `sub-agents-context`.
**Approach (assumed at the header base):** extend the "Letting the model pick the seat" neighbourhood. Find every key list with `grep -rn 'sub-agents-choice' docs/manual/ README.md`.
**Regression guard.** item 5 extends the `sub-agents-context` entry item 2 added rather than adding a second one.
The key stays back-ticked or inside a fenced example (`manualDocumentsSetting`). Item 2's row is not Editable, so the manual says the key is set in config.yaml only, with no `/settings` row, unlike the `sub-agents-choice` paragraph.
**Files:** docs/manual/configuration.md, docs/manual/commands.md
**Read first:** docs/manual/configuration.md — "Letting the model pick the seat", "Delegated sub-agents — what one may spend"; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey, manualDocumentsSetting;
internal/config/registry.go — sub-agents-choice row; docs/manual/workflows.md — fan_out argument table (`context` row)
**Tests:** none new. `TestManualDocumentsEverySettingsKey` must still pass.
**Acceptance:**
- `grep -rn 'sub-agents-context' docs/manual/configuration.md`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey' ./cmd/apogee/`
**Closes:** sub-agent-shared-files
**Commit:** `docs(manual): document sub-agents-context and the sub_agent context argument`
Depends on items 2 and 4.

## 6. `servers:` entry carries an optional `stream-idle-timeout` — ✅ DONE (2026-10-19)

NOTES (2026-10-19): the refusal's reason is ParseStreamIdleTimeout's message with its leading "apogee: " trimmed, so the prefix reads `apogee: servers: entry N ("<name>"): stream-idle-timeout: invalid stream-idle-timeout ...` without a second "apogee:" mid-line; the whitespace-only value is refused in ValidateServers (as api-key-env: is) AND the resolver treats it as absent, both pinned by tests.
NOTES (2026-10-19): gofmt realigned the ServerEntry struct's field tags because the new field name is the longest; no field changed beyond the added `StreamIdleTimeout string` (`yaml:"stream-idle-timeout,omitempty"`).

**What:**
**Goal:** `config.ServerEntry` accepts `stream-idle-timeout`, validated by `ParseStreamIdleTimeout`. A bad value is refused with `ValidateServers`' prefix, `servers: entry N ("<name>"): stream-idle-timeout:`, followed by `ParseStreamIdleTimeout`'s reason. `config.ResolveStreamIdleTimeout(entry, root)` returns the entry's value when it is stated (`0` included, meaning off), and the root otherwise. `ServerEntry` stays comparable, and a written entry omits the key when it is absent.
**Approach (assumed at the header base):** store the raw text as `StreamIdleTimeout string` with `yaml:"stream-idle-timeout,omitempty"`, where `""` means absent. No pointer is used, so `cloneOptions` and `!=` comparisons need no change. Validate in `ValidateServers`; no `UnmarshalYAML` branch, since a scalar string never fails decode there. Put the resolver beside `ResolveContextWindow`/`ResolveResponseReserve`.
**Regression guard.** The refusal is spelled as `ValidateServers` spells its prefix (`apogee: servers: entry %d (%q):`), then `stream-idle-timeout:` and the parser's reason. `ResolveStreamIdleTimeout` treats `strings.TrimSpace(entry) == ""` as absent, because `ParseStreamIdleTimeout` trims `" "` to `""` and answers the 10m default. Either `ValidateServers` refuses a whitespace-only value, as it does `api-key-env:`, or a test pins the whitespace case.
**Files:** internal/config/config.go, internal/config/config_test.go, internal/config/configwrite_server_test.go, internal/config/unknownkeys_test.go
**Read first:** internal/config/config.go — ServerEntry, ValidateServers, ResolveContextWindow, ResolveResponseReserve; internal/config/registry.go — ParseStreamIdleTimeout; internal/config/unknownkeys.go — serverEntryType, walkUnknownKeys;
internal/config/configwrite.go — SaveServerEntrySetting
**Tests:**
- The resolver: absent gives the root, whitespace-only gives the root, `0` gives 0, `90s` gives 90s.
- A refusal for an invalid or negative value names the entry and the key.
- An entry without the key round-trips through the writer.
- The unknown-key walk accepts the key (unknownkeys_test.go).
**Acceptance:**
- `go vet ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'StreamIdle|ValidateServers|ServerEntry' ./internal/config/`
**Commit:** `feat(config): a servers entry may state its own stream-idle-timeout`

## 7. Engine bindings carry a per-server stream-idle window — ✅ DONE (2026-10-19)

NOTES (2026-10-19): the fold bounds in agent.go (finishAtFault) and subagent.go (foldStoppedChild) already read the Config's StreamIdleTimeout, so they needed no edit — applying the window on the binding is what moves them; the third test ("an unstated window keeps the Config's value") is a table test over the binding (UpstreamSpec, DelegationTarget, and RebindSpec, which states none), beside the existing unstated-window dial test.
NOTES (2026-10-19): domain/config.go keeps its "host folds in the `stream-idle-timeout:` key" sentence unchanged; the per-entry override is item 8's host behaviour to document.

**What:** Recast at the regression check (2026-10-19).
**Goal:** a `serverBinding` can state a stream-idle window. Applied to a `domain.Config`, it sets `StreamIdleTimeout`, so the dial (`dialOptions` → `provider.WithStreamIdleTimeout`), the fold bound in `agent.go` and the child fold bound in `subagent.go` all read the bound server's window. `UpstreamSpec` and `DelegationTarget` each carry it; `RebindSpec` does not, because a model change states no dial facts. No comment in `internal/agent` or `internal/domain` calls the stream-idle bound a session fact that is the same on every dial.
**Approach (assumed at the header base):** add a pointer field to `serverBinding` (`internal/agent/serverbinding.go`), set in `applyTo`, and fields on the spec types (`rebind.go`, `delegationtarget.go`) feeding their `binding()` methods. An absent field keeps the Config's value, which matches the existing pointer-field semantics. The host (item 8) always states the resolved value.
**Regression guard.** binding shape for items 7 and 8: an optional pointer window (`*time.Duration`) on UpstreamSpec/DelegationTarget/serverBinding (absent keeps the Config's value), a plain resolved value on cmd/apogee upstreamBinding, and spec comparisons that use reflect.DeepEqual (or an equivalent value compare) so a pointer field never makes equal specs compare unequal.
`RebindSpec` gains no window: `Agent.Rebind` never dials (only `SetModel` on the existing client), so a window there would move the fold bound while the Client kept the old one. `UpstreamSpec`'s doc ("Every field here is a plain value … applied as written", rebind.go) is edited to name the pointer exception, and the field gets its own doc. Every comment that calls the bound a session fact is restated (`grep -rn 'StreamIdleTimeout\|idle bound\|silence bound\|carry it alike\|session.s, not a server' internal/agent internal/domain`: construct.go dialOptions incl. :582-583, dialer_test.go, domain/config.go).
Re-check (2026-10-19): no engine code compares `UpstreamSpec`/`DelegationTarget`, so the equal-windows spec-comparison test moves to item 8, where the comparisons switch to reflect.DeepEqual; item 7 keeps the three dial/fold tests.
ADR 0082 Decision 1 is amended, not yielded to — the window still rides every dial through `dialOptions`, but its value is the bound server's (`servers:` entry's `stream-idle-timeout`, else the root), so a routed child's window may differ from the session's. The amendment note on the record lands with item 9.
**Files:** internal/agent/serverbinding.go, internal/agent/rebind.go, internal/agent/delegationtarget.go, internal/agent/construct.go, internal/agent/dialer_test.go, internal/agent/subagent_test.go, internal/domain/config.go
**Read first:** internal/agent/serverbinding.go — serverBinding, applyTo; internal/agent/rebind.go — UpstreamSpec; internal/agent/construct.go — dialOptions;
internal/agent/agent.go — finishAtFault; internal/agent/subagent.go — foldStoppedChild; internal/agent/delegationtarget.go — DelegationTarget; internal/agent/dialer_test.go — dialRecord
**Tests:**
- A `SwitchUpstream` that states 30s re-dials with a 30s window (dialer_test.go's `dialRecord.idle`).
- A delegation target that states a window bounds the child.
- An unstated window keeps the Config's value.
**Acceptance:**
- `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'StreamIdle|SwitchUpstream|Dialer|DelegationTarget' ./internal/agent/`
- `grep -rn 'StreamIdleTimeout\|idle bound\|silence bound\|carry it alike\|session.s, not a server' internal/agent internal/domain` — no hit claims the bound is a session fact, the same on every dial
**Commit:** `feat(agent): server bindings carry their stream-idle window`

## 8. Host resolves the per-server window on bind, move and delegation

**What:**
**Goal:** the window `ResolveStreamIdleTimeout(entry, root)` gives is the one that bounds every upstream apogee binds:
- the startup bind;
- a `/server` move, where moving from an entry with an override to one without falls back to the root;
- a routed delegation target;
- Firings.

The ephemeral `--endpoint` entry gets the root.
**Approach (assumed at the header base):** add the field to `upstreamBinding` (`cmd/apogee/upstream.go`). Fill it in `bindingOfEntry`/`bindingOfTarget`/`bindingOfConfig`/`fillDial`, and pass it through `upstreamSpec` and `delegationTarget` (`cmd/apogee/dial.go`) and `resolveDelegationTarget` (`cmd/apogee/delegation.go`), always stating the resolved value. Follow the `request-extra`/`vision` plumbing into `fillDial`, `upstreamSpec` and `delegationTarget` only. The call sites are the same ones `ResolveContextWindow` uses (`wire_server.go`, `upstream.go`, `wire_firing.go`).
**Regression guard.** inherits item 7's shape; the host always states the resolved value as a non-nil pointer.
`upstreamBinding`'s field is a plain `time.Duration`. Spec comparisons switch to `reflect.DeepEqual`: wire_server_test.go's `agent.specs[i] != wantSpec` sites (move and load-profile tests) and dial_test.go's `spec != want`. `bindingOfEntry`'s callers probe.go, probemodel.go and delegation.go, and `resolveDelegationTarget`'s calls in delegation_test.go, follow any new root parameter; alternatively callers set the resolved value after construction.
A move's root comes from a new `liveSettings` accessor (wire_settings.go), so a `/settings` edit of the root lands at the next move, as working-window and response-reserve do. The window stays out of `upstreamBinding.Client()` and `Monitor()`, which would give naming and probe Clients `WithStreamIdleTimeout(0)`.
**Files:** cmd/apogee/upstream.go, cmd/apogee/dial.go, cmd/apogee/delegation.go, cmd/apogee/wire_server.go, cmd/apogee/wire_firing.go, cmd/apogee/wire_settings.go, cmd/apogee/probe.go, cmd/apogee/probemodel.go, cmd/apogee/dial_test.go, cmd/apogee/wire_server_test.go, cmd/apogee/delegation_test.go
**Read first:** cmd/apogee/dial.go — bindingOfEntry, fillDial, upstreamSpec, delegationTarget, Client; cmd/apogee/upstream.go — sessionMover.move; cmd/apogee/delegation.go — resolveDelegationTarget;
cmd/apogee/wire_settings.go — liveSettings.pin
**Tests:**
- The entry override reaches the bound engine Config.
- A move to an entry without an override restores the root.
- A routed child gets its target entry's window.
- `--endpoint` gets the root.
- A Client built by `upstreamBinding.Client()` keeps the provider's default cut.
- Two specs with equal windows held at different addresses compare equal under the spec comparison (the reflect.DeepEqual sites in wire_server_test.go and dial_test.go).
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'StreamIdle|Dial_|Move|LoadProfile|DelegationTarget' ./cmd/apogee/`
**Commit:** `feat(apogee): a servers entry's stream-idle-timeout bounds its upstream`
Depends on items 6 and 7.

## 9. Manual documents the per-server `stream-idle-timeout`

**What:** Recast at the regression check (2026-10-19).
**Goal:** the `servers:` key list in `docs/manual/configuration.md` names `stream-idle-timeout` with its three states. The root key's section says an entry can override it, and says plainly that keepalive comments still reset the timer, so a lower value does not catch a keepalive-only stream.
**Approach (assumed at the header base):** edit the "The context window and the reply stream" section and the `servers:` prose list. Leave the root key's "file-only key" sentence and the startup-only key lists in `configuration.md` and `commands.md` as they stand.
**Regression guard.** the manual also states that a live edit of the bound entry's `stream-idle-timeout` takes effect at the next `/server` move or restart, not mid-session.
The "(a file-only key)" sentence and the registry row's `Editable: true` both stand: in the manual "file-only" means no flag or env (configuration.md line 13), and the start-only key lists (configuration.md "Sixteen" list, commands.md) already name the key. Neither is edited.
Item 9 also records ADR 0082 Decision 1's amendment (the per-server window rides every dial; its value is the bound server's) as a dated (2026-10-19) amendment note on `.capcom/knowledge/decisions/adr-0082/adr-0082-decision.md` Decision 1, and names that own amendment on `adr-0082.md`'s Status line (the house form, e.g. ADR 0029); its `Amends:` line names other records and stays.
**Files:** docs/manual/configuration.md, docs/manual/commands.md, .capcom/knowledge/decisions/adr-0082/adr-0082-decision.md, .capcom/knowledge/decisions/adr-0082.md
**Read first:** docs/manual/configuration.md — "The context window and the reply stream" section, servers: optional-key prose, file-only definition (line 13), start-only key list; docs/manual/commands.md — /settings start-only key list;
internal/config/registry.go — stream-idle-timeout row; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none new.
**Acceptance:**
- `grep -n 'stream-idle-timeout' docs/manual/configuration.md`
- `grep -nE 'overrid.*stream-idle-timeout|stream-idle-timeout.*overrid' docs/manual/configuration.md` finds the per-entry override sentence
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Drift|Manual' ./internal/tools/`
- `capcom check`
**Closes:** per-server-idle-timeout
**Commit:** `docs(manual): document the per-server stream-idle-timeout`
Depends on item 8 and item 5 (both edit `configuration.md`).
