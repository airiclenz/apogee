# Undeclared tool arguments are named in the result — plan

**Goal:** a native tool call carrying an argument key its schema does not declare still runs, and its result ends with one line naming the ignored key(s) and the declared ones. A call with only declared keys returns a byte-identical result to today. MCP and host-registered tools are untouched.
**Date:** 2026-11-22
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** d26b34a5
**Closes:** undeclared-args-ignored

**Sources:**
- `.capcom/issues/undeclared-args-ignored.md` (Direction, Acceptance)
- ADR 0071 Decision 1 (`capcom know show "ADR 0071" --with-children`); ADR 0076 D2, D9
- `docs/design/tool-surface-findings.md` — the `grep` arguments record (2026-09-15) is the precedent
- `docs/design/test-drivers.md` §Writing a new e2e test; `capcom know show adr-0062-amend-1118`

**Ratified design calls** (owner, 2026-11-22):
- **Class:** a structural tool-result fact — on for every model, inside Bypass, no config key; not an eighth Floor guard (no retry, no correction) and not a Reaction (fires at no Moment, asks nothing). ADR 0071 1(c): a declared-only call is byte-identical; an extra-key call still runs in the same Turn and gains one true line where the bare loop reports a false success. No bench arm.
- **Placement:** the last line of the tool's own `Content`, appended once in the engine on the leaf path right after the tool runs (before the tree-mutation warning, audit, reactions and the clamp), so the session record and transcript carry it too.
- **Wording:** `note: ignored unknown argument "line" — read_file takes path, start_line, end_line, max_lines, locate`; plural `arguments "a", "b"`; a tool with no declared properties reads `— <tool> takes no arguments`.
- **Scope:** tools built in `internal/tools` (those embedding `toolSpec`) only; `finish` opts out (it refuses undeclared receipt fields itself); MCP and host tools never match.

**Regression check (2026-11-22, d26b34a5):**
- 1: guard folded (unchanged rows compare by `reflect.DeepEqual` — `ReadSpan` holds a slice; ignored keys deduped by exact spelling).

**Standing requirements:**
- skills: coding-standards
- Machine rule (Raspberry Pi 5, 8 GB): never `go test -race`/`-cover` over a whole heavy package or a pattern; one package + one `-run` with `GOMEMLIMIT=2GiB`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`; never two test runs at once. Forward to every sub-agent.
- A test that needs a model scripts `internal/stubllm`; never an `httptest` upstream closure.
- Bubble Tea v2 names only (`tea.KeyPressMsg`), should any TUI code be touched (none is planned).

**Out of scope:**
- MCP server tools (their servers validate their own schemas) and host/embedder-registered tools.
- `sub_agent` and `fan_out`: delegation calls go through `runDelegation` / `runWorkflowCall` and never reach the leaf path; their ignored keys stay silent.
- Refusing, retrying or correcting a call with extra keys; nested (non-top-level) keys.
- Any change to `internal/floor` (the repair guard) or the Floor-guard count.

## 1. Name a call's ignored arguments in internal/tools — ✅ DONE (2026-11-22)

NOTES (2026-11-22): consequential edit — internal/tools/doc.go: made necessary by the new internal/tools/ignoredargs.go (the package file map, enforced by TestDocMapNamesEveryFile)
NOTES (2026-11-22): test table also carries a "not a JSON object" row (`[1]`, unchanged), which the Goal names but the Tests list did not

**What:**
**Goal:** `internal/tools` exports one function that, given a tool, its call and the result `Execute` returned, returns the result with a final `note:` line naming every top-level argument key the tool's schema does not declare — and returns the result unchanged (same struct value) when there is none, when the arguments are not a JSON object, or when the tool did not opt in. Every tool embedding `toolSpec` opts in; `finish` opts out.
**Approach (assumed at the header base):**
- New file `internal/tools/ignoredargs.go`: `func NoteIgnoredArguments(tool domain.Tool, call domain.ToolCall, result domain.ToolResult) domain.ToolResult`. Extends `internal/tools`; no new package. One deep function; no per-tool code.
- Opt-in is an unexported interface `interface{ ignoresUndeclaredArguments() bool }`: `toolSpec` gains `func (toolSpec) ignoresUndeclaredArguments() bool { return true }` (in `tools.go`, beside `decodeArgs`, whose stdlib decode is the reason); `finish`'s tool type shadows it with `false`. MCP `serverTool` and host tools cannot implement it.
- Raw keys: walk `call.Arguments` top-level member names with a `json.Decoder` in wire order (empty/whitespace arguments → no keys, as `decodeArgs` treats them). Declared: the schema's `properties` member names in schema order, read the same way from `tool.Schema()` at call time. A raw key is declared when `domain.FoldArgumentKey(key)` equals the fold of some property (stdlib JSON binds case-insensitively, so `Path` is not flagged).
- Line: `note: ignored unknown argument %q — <tool> takes <a, b, c>`; ≥2 keys `arguments "a", "b"` (each `%q`, so a key with a line break cannot forge a row); no properties `— <tool> takes no arguments`. Joined with one `\n` (none when `Content` is empty or already ends in `\n`). `IsError` results get it too: the fact is still true and can explain the failure.
- Rewrite the `grepCapNudge` comment in `internal/tools/grep.go` (rule: every comment stating that `decodeArgs` silently drops unknown fields — `grep -rn "drops unknown\|silently ignores" internal/`) to say an unknown key is now named back as ignored, keeping its reason for not naming a knob grep does not honour.
**Regression guard.** `read_file`'s `Summary` is `domain.ReadSpan`, which holds `LocatedOn []int`, so `==` on two such `ToolResult`s panics ("comparing uncomparable type"): every unchanged-result row (declared-only, empty arguments, `finish`, fake tool) compares with `reflect.DeepEqual`. A key repeated under one spelling with one value (`{"line":3,"line":3}`) is not refused and reaches the tool, so ignored keys are deduped by exact spelling (first occurrence keeps wire order) before singular/plural is chosen.
**Files:** internal/tools/ignoredargs.go; internal/tools/ignoredargs_test.go; internal/tools/tools.go; internal/tools/finish.go; internal/tools/grep.go
**Read first:** internal/tools/tools.go — toolSpec, decodeArgs, decodeToolArgs; internal/domain/tools.go — FoldArgumentKey, CollidingArgumentKeys, RepeatedArgumentKeys, ToolResult; internal/tools/finish.go — Finish, NewFinish, FinishSchema;
internal/tools/read_file.go — readFileSpec, readFileArgs, ReadFile.Execute; internal/domain/toolsummary.go — ReadSpan; internal/tools/git.go — gitStatusSpec, gitShowArgs (embeds readFileArgs);
internal/tools/grep.go — grepCapNudge; internal/tools/readonly_subprocess.go — readOnlySubprocess (unexported-marker precedent)
**Tests:** `TestNoteIgnoredArguments` — table over real built-ins run through `Execute` then the function: `read_file` with `line` (still reads; result ends with the exact literal `note: ignored unknown argument "line" — read_file takes path, start_line, end_line, max_lines, locate`, the declared list taken from the issue and checked against the live schema); `read_file` declared-only (result `reflect.DeepEqual` the bare `Execute` result; every unchanged row compares this way); two ignored keys (plural, wire order); a same-spelling repeat `"line":3,"line":3` (one `"line"`, singular); `Path` spelling (no note); empty arguments; `git_status` with a key (`takes no arguments`); a decode-failure error result (note appended); `finish` and a fake non-`toolSpec` tool (unchanged).
**Acceptance:**
- `go build ./... && go vet ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNoteIgnoredArguments' ./internal/tools/`
**Commit:** `feat(tools): name a call's ignored arguments in its result`

## 2. Append the note on the engine's leaf tool path

**What:** Depends on item 1.
**Goal:** every leaf tool call the engine runs returns its result through `tools.NoteIgnoredArguments` exactly once, so the next request the model receives carries the note for a built-in called with an undeclared key — under Bypass too — and is byte-identical for a declared-only call.
**Approach (assumed at the header base):** in `internal/agent/dispatch.go` `executeToolDemotable`, on the `err == nil` branch right after `a.runTool(ctx, tool, call)` and before `appendTreeMutationWarning`, set `res = tools.NoteIgnoredArguments(tool, call, res)`. The Go-error branch is untouched. One call site; no Reaction, no config key, no event.
**Changelog (closeout, `[Unreleased]`):** `- A built-in tool called with an argument it does not declare still runs, and its result now ends with a line naming the ignored argument and the ones it takes (\`note: ignored unknown argument "line" — read_file takes path, start_line, end_line, max_lines, locate\`); it was silently dropped before (\`undeclared-args-ignored\`).`
**Files:** internal/agent/dispatch.go; internal/agent/ignoredargs_test.go
**Read first:** internal/agent/dispatch.go — executeToolDemotable, runTool, prepareCall, runCall, appendToolResult; internal/agent/treesnapshot.go — appendTreeMutationWarning; internal/agent/recipe.go — runScriptCall, recipeScripts.RunScript (engine-built terminal call, declared "command" only);
internal/agent/extrareadroots_test.go — TestMissingMountRootAnswersNotFound; internal/agent/harness_test.go — scriptedResponder, scriptedUpstream.last, baseConfig, toolCallTurn; internal/agent/loop_test.go — assertWireArguments; internal/stubllm/log.go — Request, Message
**Tests:** new `internal/agent/ignoredargs_test.go`, scripted through `internal/stubllm` on the pattern of `TestMissingMountRootAnswersNotFound` (`extrareadroots_test.go`) and asserting on the next request the stub receives (`upstream.last().Messages`, as `assertWireArguments` does): `TestIgnoredArgumentNoteReachesModel` (`read_file` with `line` → tool message ends with the exact note; file content present); `TestDeclaredArgumentsResultUnchanged` (declared-only call → tool message equals the file's read with no `note:` line); `TestIgnoredArgumentNoteUnderBypass` (same as the first with Bypass on).
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestIgnoredArgumentNote|TestDeclaredArgumentsResultUnchanged' ./internal/agent/`
**Commit:** `feat(agent): append the ignored-arguments note on the leaf tool path`

## 3. Record the change in the tool-surface findings

**What:** Depends on item 2.
**Goal:** `docs/design/tool-surface-findings.md` carries a dated bold-lead record of the ignored-arguments note: what was added, the session evidence (session 6e088546: `read_file` called with an invented `line:` 31 times, every call an unranged read), its class (structural tool-result fact, on under Bypass, no key — the ratified call and its ADR 0071 1(c) reasoning), its scope (built-ins only; MCP, host tools, `finish`, `sub_agent`, `fan_out` excluded) and that, like every addition, it is pruned on bench evidence.
**Approach (assumed at the header base):** add one paragraph after the `**\`grep\` arguments added 2026-09-15 …**` record, in its style: `**Ignored-arguments note added 2026-11-22 (plan \`2026-11-22 - 00\`, issue \`undeclared-args-ignored\`):**`.
**Files:** docs/design/tool-surface-findings.md
**Read first:** docs/design/tool-surface-findings.md — the "`grep` arguments added 2026-09-15" record (insert after it, before "Denied, with reasons"), "Engine-level notes (not tools)", method lesson 2; .capcom/issues/undeclared-args-ignored.md — session 6e088546 evidence, Direction; internal/tools/manual_drift_test.go — TestManualListsEveryKnownToolName, TestReadmeStatesTheToolCounts
**Tests:** none (docs-only).
**Acceptance:**
- `grep -n 'Ignored-arguments note added 2026-11-22' docs/design/tool-surface-findings.md`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualListsEveryKnownToolName|TestReadmeStatesTheToolCounts' ./internal/tools/`
**Commit:** `docs(design): record the ignored-arguments note in the tool-surface findings`
