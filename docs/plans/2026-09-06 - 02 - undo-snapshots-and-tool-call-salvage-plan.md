# Plan — persistent undo snapshots and the tool-call salvage guard

**Goal:** close gaps 1 and 2 of the contender assessment: a seventh Floor guard that runs a tool call a native-profile model wrote as JSON in its text instead of on the wire, and an undo that survives relaunch, covers `terminal`/`python_exec`/MCP writes, offers `/redo`, and gives headless and daemon runs a revert verb (closes bead apogee-kk0.7).
**Date:** 2026-09-06 · **Status:** unexecuted · **Base:** `5359e90e` · **Sized for:** ~200k-context host.

**Sources:** `docs/handoffs/2026-09-06 - 02 - contender-gap-assessment.md` (gaps 1, 2); ADR 0071 (admission test (a)(b)(c), lines 52-56); ADR 0051; ADR 0042 D2; ADR 0022 §8; `CONTEXT.md:657-675` (Undo journal), `:698-729` (Floor guard); `docs/design/archived/mechanism-catalogue.md:377-450`; `internal/floor/doc.go`; `internal/undo/doc.go`.

**Ratified design calls** (owner, 2026-09-06, via AskUserQuestion):
- **Undo store:** a session-owned git object database under `~/.apogee/snapshots/<session-id>/` (bare `GIT_DIR`, the workspace as work-tree, private index); git absent → today's in-memory funnel journal stays and `/undo` names the reason.
- **Revert scope:** diff-scoped, skip-and-report — only paths that differ between the Exchange's pre and post snapshots; ADR 0051 D5 stays.
- **Redo:** `/redo` with the same two-step confirm, generation stamp and idle-only rule; the next Exchange that writes clears the redo stack.
- **Unattended revert:** a top-level `apogee undo <session-id> [confirm]` verb; headless/daemon written-files reports end with the exact command.
- **Salvage shapes:** a JSON object with `name` plus `arguments`/`parameters`/`input`, in a fenced block, inside `<tool_call>…</tool_call>` tags, or as the whole trimmed content; every match in document order becomes a call. No XML dialects.
- **Salvage notice:** debug view only, per ADR 0071's rendering rule, with `FloorGuardEvent.Detail` set.
- **Row home:** beads only; `IDEAS.md` untouched.
- **Undo at /new:** reopen under the new session id; a journal.json holds one session's Exchanges.
- **Writer calls (2026-09-06):** config keys `tool-call-salvage` and `undo-snapshots` follow the `tool-call-repair` naming; the salvage guard runs first in the post-response chain and strips the salvaged block from the text (`loop.go:641-645` precedent); out-of-workspace approved writes (ADR 0051 D6) stay funnel-journaled and per-process.

**Regression check (2026-09-06, 5359e90e):** Line references were verified at 5359e90e; the hooks plan landed after it (458a7b19..a2863f5a) and shifts internal/agent, internal/config, cmd/apogee and CONTEXT.md lines — an implementer re-verifies every cited range against its tree before editing.
- 1: new in round 3 — the hooks-plan gate.
- 2: SAFE.
- 3: recast (rounds 1-2).
- 4: guard folded (rounds 1-2).
- 5: guard folded.
- 6: recast (rounds 1-2); round 3: guard folded (residue rule), yields to ADR 0059:24-25.
- 7: split off in round 2; round 3: guard folded (structs stay in tools, teardown seam, cmdline/`exitCodeOf` move).
- 8: recast (rounds 1-2); round 3: guard folded (`Run` = `RunGitQuery` + env, message const, box).
- 9: recast; round 3: guard folded (`--ignore-errors`, blob-only `ListBlobs`, `--object-format=sha1`).
- 10: recast; round 3: guard folded (redo cleared at materialisation, `post` kept without a snapshot).
- 11: guard folded.
- 12: recast; round 3: guard folded (`onClose` seam, fourth row) + decision (`undo-snapshots` in both settings tests).
- 13: recast — supersedes `agent.go:1097-1100` via ADR 0074; round 3: guard folded (sweep beside `gcSessions`, `pendingJournal`, empty `ConfigDir`, cite `:264`).
- 14: guard folded.
- 15: guard folded.
- 16: recast; round 3: guard folded (four files, wider pattern, cites, TUI hits are item 14's).

**Standing requirements:** `skills: coding-standards`; any authorized deviation from item text lands as a dated NOTES line under the item; item 1 gates every item; items 2-5 are independent of items 6-16, which are DEPENDS-chained as stated.

**Out of scope:** the hooks plan (`docs/plans/2026-09-06 - 01 - hooks-plan.md`) and ADR 0073; XML function-call dialects; a pure-Go snapshot store; persisting out-of-workspace pre-images; snapshots of paths the workspace's own `.gitignore` excludes; an undo tool for the model; `IDEAS.md`; any version bump.

## 1. Verify the hooks plan is archived — ✅ DONE (2026-09-07)

NOTES (2026-09-07): gate verified — `docs/plans/archived/2026-09-06 - 01 - hooks-plan.md` exists, `docs/plans/2026-09-06 - 01 - hooks-plan.md` is gone, and `git status --porcelain -- internal/agent internal/config cmd/apogee internal/tui` is empty (the whole tree is clean at 8c3dc9ae). The hooks plan's last commit a2863f5a is an ancestor of HEAD, so its edits have landed rather than being in flight.

NOTES (2026-09-07): the plan header's Base is 5359e90e but HEAD is 8c3dc9ae — every later item must re-verify its cited line ranges against this tree, exactly as the plan's regression-check note requires.

**What:** this plan touches `internal/agent`, `internal/config`, `cmd/apogee/wire*.go` and `internal/tui`, the files the hooks plan `docs/plans/2026-09-06 - 01 - hooks-plan.md` is landing in concurrently (commits 458a7b19..a2863f5a and a dirty tree at write time); no item of this plan runs until that plan has been archived under `docs/plans/archived/` and the working tree is clean of its edits.
**Files:** none.
**Tests:** none.
**Acceptance:** `test -f "docs/plans/archived/2026-09-06 - 01 - hooks-plan.md" && ! test -f "docs/plans/2026-09-06 - 01 - hooks-plan.md" && test -z "$(git status --porcelain -- internal/agent internal/config cmd/apogee internal/tui)"`
**Commit:** none (a gate, no change).

## 2. `floor.SalvageToolCall` — the pure guard — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the `six`-count sentence in `internal/floor/doc.go` was left as it stands — item 4's regression guard names `internal/floor/doc.go:18` as its own site; this item touched doc.go only to add salvage.go to the file map, as its text says.

NOTES (2026-09-07): the guard cannot import `internal/processing` (doc.go's one-direction rule), so "shaped as `processing.ParseNativeToolCalls` shapes a native call" is reproduced locally in `normalizeSalvagedArguments` — object taken as written, string decoded, empty normalised to `{}` — rather than by calling it.

**What:** add `internal/floor/salvage.go`: `func SalvageToolCall(resp *domain.Response, offered []string) (calls []domain.ToolCall, text string, fired bool)`. Fires only when `len(resp.ToolCalls()) == 0` and `resp.Text()` holds at least one JSON object whose `name` is exactly one of `offered` and which carries `arguments` / `parameters` / `input` (an object, or a string holding JSON), in any of the three ratified containers. Every match becomes a call in document order, shaped as `processing.ParseNativeToolCalls` shapes a native call; IDs left empty. `text` is the response text with each matched block removed and trimmed. Unoffered names, prose mentions and malformed JSON never fire. Pure function (`internal/floor/doc.go:6-15`); add the file to `doc.go`'s file map. Depends on item 1.
**Files:** `internal/floor/{salvage.go,salvage_test.go,doc.go}`.
**Tests:** per firing case (fence, tag, whole-content, two calls in order, `parameters` key, string-encoded arguments); a no-op table (wire call present, unknown name, prose mention, bad JSON, empty text); text stripping.
**Acceptance:** `go build ./... && go test ./internal/floor/`
**Commit:** `feat(floor): SalvageToolCall reads a fenced JSON tool call out of a wire-less reply`

## 3. Wire the salvage guard into the loop, gated by `tool-call-salvage` — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the plan cites `cmd/apogee/wire_settings.go:957` among the floor's Go-comment counts; at this tree (`:988`) that sentence counts `settingsApplier`'s own members, not Floor guards, so it was left alone. It is stale on its own terms — the struct has thirteen members, not six — but that predates this item and is not a floor count.

NOTES (2026-09-07): the plan's `Files:` list did not name `cmd/apogee/{wire_boot_test.go,wire_engine_test.go,wire_firing_test.go,wire_helpers_test.go}`; each carries a comment counting the Floor guards as six and, in two cases, an `Options` fixture described as "the whole floor". Folded in under the item's own "every sentence that counts the guards is updated" rule (the enumeration is a floor, not a ceiling).

NOTES (2026-09-07): consequential edit — cmd/apogee/wire_boot_test.go: made necessary by the seventh Floor-guard key

NOTES (2026-09-07): consequential edit — cmd/apogee/wire_engine_test.go: made necessary by the seventh Floor-guard key

NOTES (2026-09-07): consequential edit — cmd/apogee/wire_firing_test.go: made necessary by the seventh Floor-guard key

NOTES (2026-09-07): consequential edit — cmd/apogee/wire_helpers_test.go: made necessary by the seventh Floor-guard key

NOTES (2026-09-07): consequential edit — internal/processing/factory_test.go: made necessary by the new exported `processing.IsNative`

NOTES (2026-09-07): salvaged-call IDs are `text_call_<turn>_<n>` with n 0-based, matching the 0-based Turn in the loop's existing `text_call_<turn>` spelling; the plan fixed the shape but not the base.

NOTES (2026-09-07): the TUI already renders `FloorGuardEvent.Detail` in the hidden debug view (transcript.addFloorGuard), so the ratified "debug view only, with Detail set" notice needed no Driver change.

**What:** Recast at the regression check (2026-09-06). `domain.FloorConfig` (`internal/domain/config.go:376`) gains `DisableToolCallSalvage bool`; register `tool-call-salvage` (`KindBool`, default `true`, Editable, Desc `Floor guard: run a tool call the model wrote as JSON in its text instead of on the wire.`) beside `tool-call-repair` (`internal/config/registry.go:409-444`) and its config/options/apply plumbing. In `internal/agent/floorguards.go` add `guardToolCallSalvage` / `guardActionSalvage`; the guard runs FIRST in `runPostResponseGuards` (`:69`) and does not return: on fire it calls `resp.SetText(text)` and `resp.AppendToolCall(call)` per call (IDs `text_call_<turn>_<n>`) and emits the event. Gate: `processing.IsNative(ToolCallParser) bool` (new; `nativeTextParser`, `factory.go:52-64`). `emitFloorGuard` (`:164`) gains `detail string`; salvage detail = `salvaged <name>[, <name>…] from content`. Rewrite the order comment (`:58-64`). Add the key line to `docs/manual/configuration.md:52-76` and a `tool-call-salvage: true` stanza beside `read-cache` in `internal/config/defaults/config.yaml:579`. Depends on item 2.
**Regression guard.** The guard salvages against the request's menu — `resp.View().Tools()` (internal/domain/hooks.go:312), the tools actually offered on that request — not the registry, and is skipped when `a.wrapUp`. `detail` is threaded through every existing `emitFloorGuard` call site (six: floorguards.go:74,80,86,92,137,157). `TestApplyConfigFloorGuardKeys` (internal/config/config_test.go:1598) gains the key. The Go-comment counts of the floor in this item's own files are updated here: `internal/domain/config.go:373`, `internal/agent/floorguards.go:35-36`, `cmd/apogee/wire_settings.go:214,279,739-764,957,1315,1802-1822`, `internal/config/defaults/config.yaml:541,547`.
**Files:** `internal/domain/config.go`, `internal/config/{registry.go,config.go,config_test.go,options.go,defaults/config.yaml}`, `internal/agent/{floorguards.go,floorguards_test.go}`, `internal/processing/factory.go`, `cmd/apogee/{wire_settings.go,wire_settings_test.go,settingsrows_test.go}`, `docs/manual/configuration.md`, `cmd/apogee/testdata/stubllm/` (new fixture), `cmd/apogee/` (new stubllm-driven test).
**Tests:** a fenced `read_file` call dispatches and the committed message carries `ToolCalls` with the fence stripped; markdown-fenced profile, `DisableToolCallSalvage`, an unoffered tool and a wrap-up Turn all end the Exchange as today; stubllm end-to-end: the second request carries `tool_calls`; `/settings` rows fixture; registry/options/floor-key/editable-key/manual tests green.
**Acceptance:** `go build ./... && go test ./internal/floor/ ./internal/agent/ ./internal/processing/ ./internal/config/ && go test ./cmd/apogee/ -run 'SettingsRows|Salvage|EveryEditableSettingKey|ManualDocumentsEverySettingsKey'`
**Commit:** `feat(agent): tool-call salvage floor guard runs a JSON call a native-profile model left in its text`

## 4. Document the seventh Floor guard — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the plan's `README.md:241` cite is stale — at BASE and at HEAD that line is the Documentation table's Configuration row ("the floor guards"), which carries no count and no enumeration and needs no edit. README's only guard enumeration is the "Why apogee" floor bullet the regression guard names as `README.md:35`; the seventh one-liner and the count went there.

NOTES (2026-09-07): the item's regression guard says "The Go-comment sites belong to item 3", but item 3's own site list did not name `internal/floor/doc.go` and its commit left `doc.go:18`'s "keeps all six ON" standing. The file is in this item's `Files:` list and inside its acceptance grep, so the count was fixed here.

NOTES (2026-09-07): `docs/manual/configuration.md:78` ("every row it once carried either became one of the six guards above or retired outright") counts *catalogue rows*, and tool-call salvage was never one — it is a new guard, not a promotion. Rewritten to "one of the guards above" rather than bumped to seven, so the sentence stays true about what the catalogue became.

NOTES (2026-09-07): ADR 0071's body counts ("the six that pass it", "Six catalogued rows pass the test", "Six config keys", "holds six `Disable…` booleans", "A stock install runs six Floor guards") were left as written and corrected by the appended amendment, following the repo's ADR convention that a decision's text stands as ratified and an amendment restates the changed fact (ADR 0002's 2026-08-20 amendment is the precedent). The amendment states explicitly that Decision 5's "six" reads as seven.

**What:** append an amendment to ADR 0071 admitting tool-call salvage under test (a)(b)(c) with the chain rule (first, non-short-circuiting), and record that the archived catalogue's "deliberately … parses none from content" (`mechanism-catalogue.md:429-449`) describes the native profile's parse seam, not the floor. Add the seventh one-liner to the Floor guard entry (`CONTEXT.md:707-724`), the Session row in `docs/manual/commands.md:407-411`, and `README.md:241`; the `configuration.md` key line landed in item 3. Depends on item 3.
**Regression guard.** Rule: every sentence that counts the guards or their keys is updated. Check: `grep -rniw 'six' README.md CONTEXT.md docs/manual internal/floor/doc.go` — floor sites at BASE: `CONTEXT.md:5,704,726`, `docs/manual/configuration.md:12,52,68-69,77,194,796`, `docs/manual/daemon.md:107`, `docs/manual/commands.md:407`, `README.md:35`, `internal/floor/doc.go:18`. The Go-comment sites belong to item 3.
**Files:** `docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md`, `CONTEXT.md`, `docs/manual/{configuration.md,commands.md,daemon.md}`, `README.md`, `internal/floor/doc.go`.
**Tests:** the grep below returns only non-floor hits; `go build ./...`.
**Acceptance:** `grep -rniw 'six' README.md CONTEXT.md docs/manual internal/floor/doc.go | grep -viE 'targets?|archives|builds|spinner|snake|seconds|combinations|nested structures|catalogue rows|rows every model'; test $? -eq 1 && go build ./... && go test ./internal/floor/ -run DocMap`
**Commit:** `docs(floor): admit the tool-call salvage guard (ADR 0071 amendment, CONTEXT, manual)`

## 5. Probe reports a salvageable reply — ✅ DONE (2026-09-07)

**What:** in `probeNativeToolCall` (`internal/probe/battery.go:177-203`), when the reply carried no `tool_calls` but `floor.SalvageToolCall` over its content (offered = the canary tool name) fires, the Finding's `Detail` becomes `the reply carried no tool_calls entry, but its content carried a JSON call for <name> — the tool-call salvage guard runs it`; otherwise `:197` stays. Detail-only: no new `Capability`, no `BatteryVersion` change (`battery.go:22-24`). Depends on item 2.
**Regression guard.** `floor.SalvageToolCall` takes `*domain.Response`; the probe wraps its `provider.RawResponse` via `domain.NewResponse(resp.Content, "", nil, "", nil)`.
**Files:** `internal/probe/{battery.go,battery_test.go}`.
**Tests:** a fenced canary call yields the new Detail; a plain-text reply the old one; the fingerprint is byte-identical before and after.
**Acceptance:** `go build ./... && go test ./internal/probe/`
**Commit:** `feat(probe): name a salvageable JSON tool call in the native tool-call finding`

## 6. ADR 0074 — undo becomes a per-Exchange snapshot pair in a session-owned object database — ✅ DONE (2026-09-07)

NOTES (2026-09-07): ADR 0051's marking follows ADR 0004's precedent — a frontmatter `Status:` line plus a top blockquote naming decision 3, decision 8 and the rejected git-based revert as superseded and decisions 1, 2, 4-7 as kept; the blockquote names that rejected alternative by its title rather than by the plan's `0051:136` citation, because the blockquote itself shifts 0051's line numbers.

NOTES (2026-09-07): `CONTEXT.md`'s Undo journal body was rewritten to exactly its previous fourteen lines so the `_Avoid_` block still starts at line 672 (the item's acceptance reads `672,675`); with "snapshot" dropped and nothing added in its place the block is now three lines, 672-674, and keeps "undo history", "undo stack" and "rollback".

NOTES (2026-09-07): the rewritten term states the decided design (workspace-tree coverage, per-session persistence, `/redo`) ahead of items 9-16 implementing it, as the item's text directs; `internal/agent/agent.go`'s `ClearContext` comment still states the superseded "journal survives /clear" rule and is item 13's to change.

**What:** Recast at the regression check (2026-09-06). Write `docs/adr/0074-undo-is-a-per-exchange-snapshot-pair-in-a-session-owned-git-object-database.md` (accepted) with the header's ratified undo calls as decisions, plus: nothing is ever written inside the workspace; capture points (pre = lazily before the first write-capable tool call of a depth-0 Exchange, post = at Exchange close when a pre exists; a no-diff Exchange leaves no step); `journal.json` beside the objects (ordinals, generation, pre/post tree ids, workspace), out-of-workspace pre-images per-process (D6); resume opens the store only when the workspace matches; `undo-snapshots: false` behaves as git absent; GC = the scratch sweep's age rule plus removal with the session record; state class = persisted host state keyed by session id, outside the session record (ADR 0022 §8 holds). Supersedes ADR 0051 D3, D8 and its rejected git alternative (`0051:136`), keeps D1, D2, D4-D7; mark those lines in 0051's status. Rewrite the Undo journal term (`CONTEXT.md:657-675`): coverage is the workspace tree, persistence per session, redo exists; drop "snapshot" from `_Avoid_` (:672-675), keep "rollback". Depends on item 1.
**Regression guard.** ADR 0074 also decides: (i) at Rotate (`/new`, `/clear`) and Activate (`/sessions` resume) the journal is reopened under the new session id, the previous session's Exchanges revertible by resuming it or via `apogee undo <old-id>` — supersedes the journal surviving `/clear` at `internal/agent/agent.go:1097-1100`; (ii) coverage never narrows: every funnel-journaled path keeps its full pre-image, the snapshot diff only ADDS paths the funnel never saw; (iii) `Capture` ignores the operator's global `core.excludesFile`; (iv) the residue RULE: any path git's add pipeline transforms or refuses — filter drivers (git-lfs pointers), nested or commit-less repositories, workspace `.gitignore` matches — is outside the snapshot; the funnel pre-image stays authoritative. ADR 0059:24-25 ("per process, dies with it") is the Console's class, not superseded: this item yields to it.
**Files:** the ADR above, `docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md`, `CONTEXT.md`.
**Tests:** none (docs).
**Acceptance:** `ADR="docs/adr/0074-undo-is-a-per-exchange-snapshot-pair-in-a-session-owned-git-object-database.md"; test -f "$ADR" && grep -q "0074" docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md && grep -q "excludesFile" "$ADR" && grep -qi "nested repositor" "$ADR" && grep -qi "lfs" "$ADR" && grep -qi "one session's Exchanges" "$ADR" && ! sed -n '672,675p' CONTEXT.md | grep -q snapshot`
**Commit:** `docs(adr): 0074 — undo is a per-Exchange snapshot pair in a session-owned git object database`

## 7. Extract the subprocess core into `internal/subprocess` — ✅ DONE (2026-09-07)

NOTES (2026-09-07): `setRawCommandLine` and `exitCodeOf` moved as the item says but stay UNEXPORTED in `internal/subprocess` — nothing outside the package calls either, and the item's "exported under the same names" clause names only `runSubprocess`/`subprocessSpec`/`subprocessResult`/`cappedBuffer` and the teardown seam.

NOTES (2026-09-07): the streaming variant is `RunSubprocessTo(ctx, spec, io.Writer)` rather than a spec field, so a caller cannot silently ask for uncapped output by setting a struct field; it implies split-stdout (CombinedOutput holds the diagnostics alone, Stdout is empty) and a nil writer discards.

NOTES (2026-09-07): five core tests moved from `internal/tools/exec_common_test.go` into `internal/subprocess/subprocess_test.go` (nil-Confiner fail-closed, wedged drain, confined flag, both denial-watch cases). `TestRunSubprocessReapsTheProcessGroupOnACleanExit` stayed in `internal/tools`: it needs `waitForPIDFile`/`pidAlive`/`killPID`, which live in test files the item's Files list does not cover, and it still exercises the core through the wrapper.

NOTES (2026-09-07): `internal/tools`' `maxSubprocessOutputBytes` is kept under its own name as required, defined as `= subprocess.MaxSubprocessOutputBytes` so the Console family's separate truncation is measured against the one ceiling rather than a second copy of the number.

NOTES (2026-09-07): consequential edit — internal/tools/doc.go: made necessary by the move of exec_cmdline_{unix,other}.go and the teardown seam out of the package

NOTES (2026-09-07): consequential edit — internal/tools/terminal.go: made necessary by the rename of exec_cmdline_other.go to internal/subprocess/cmdline_other.go

NOTES (2026-09-07): consequential edit — internal/platform/platform.go: made necessary by the rename of exec_cmdline_other.go to internal/subprocess/cmdline_other.go

NOTES (2026-09-07): consequential edit — internal/platform/confinetest/lines_windows.go: made necessary by the rename of exec_cmdline_other.go to internal/subprocess/cmdline_other.go

NOTES (2026-09-07): consequential edit — internal/platform/teardown.go: made necessary by the teardown owner moving from internal/tools' runSubprocess to internal/subprocess' run

**What:** a pure move of `runSubprocess` (`internal/tools/exec_common.go:309`), `subprocessSpec` (`:35`), `subprocessResult` (`:177`), `cappedBuffer` (`:549`) and the teardown logic (`platform.RunWithTeardown`, `:405`) into a new `internal/subprocess`, exported under the same names; it imports `security`, `domain`, `platform` and stdlib only (cycle-free). `internal/tools` keeps its unexported names as thin wrappers so every tool and test is untouched. One new capability only: a streaming variant that writes stdout uncapped to an `io.Writer` (same spec, confinement handoff and teardown; stderr still capped). Depends on item 6.
**Regression guard.** `internal/tools` KEEPS `subprocessSpec`, `subprocessResult`, `cappedBuffer` and `maxSubprocessOutputBytes` with today's fields (tests and `console_common.go:247-251` build them by field name); `runSubprocess` converts at the seam, no type alias. `subprocess` exports the teardown seam `var NewProcessTeardown` (`exec_common.go:282`; `exec_teardown_test.go` moves whole), and the move includes `setRawCommandLine` (`exec_cmdline_{unix,other}.go`) and `exitCodeOf` (`:530`).
**Files:** `internal/subprocess/{doc.go,subprocess.go,subprocess_test.go,teardown_test.go,cmdline_unix.go,cmdline_other.go}`, `internal/tools/{exec_common.go,exec_common_test.go,exec_teardown_test.go,exec_cmdline_unix.go,exec_cmdline_other.go}`.
**Tests:** the moved tests run under `internal/subprocess` (the teardown test swaps `subprocess.NewProcessTeardown`); the capped variant truncates a >256 KiB stdout exactly as today and the streaming variant delivers the same stdout to its writer byte-identical; `internal/tools` suite unchanged.
**Acceptance:** `go build ./... && go test ./internal/subprocess/ ./internal/tools/`
**Commit:** `refactor(subprocess): one subprocess core shared by the tools and the git runner`

## 8. Extract the hardened git runner into `internal/gitexec` — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the item's `Run` gained a sibling `Query` (Run minus the resolution) so `tools.RunGitQuery` can keep resolving through its own `lookGit` var — the regression guard's requirement that `withFakeGit` keep working — while `Run`/`RunTo` resolve through the exported `gitexec.LookPath` seam.

NOTES (2026-09-07): `gitexec` carries its own `probeTimeout` (15s, the old `gitTimeout` value) for the command-config probe; `tools`' `gitTimeout`/`gitDiffTimeout` are untouched, since they bound the git TOOLS' calls and stay tool-side.

NOTES (2026-09-07): moved hardening tests = `TestRunGit_AppliesHardeningToEveryInvocation` and `TestRunGit_MemoisesTheFilterDriverProbePerRoot` (now `TestCapture_*` in `internal/gitexec`). The three `TestRunGitQuery_*` stayed in `internal/tools`: `tools.RunGitQuery` is still the exported engine entry with its own lookup seam, so they are wrapper coverage rather than runner coverage, and `gitexec` got its own `Run` equivalents.

NOTES (2026-09-07): consequential edit — internal/tools/doc.go: made necessary by the funnel moving out of git.go, whose package-map role line said it held the hardened funnel.

NOTES (2026-09-07): consequential edit — docs/design/confinement-execution-contract.md: made necessary by the move, the §2.4 line cited `gitHardeningOptions`, a symbol this item removed from internal/tools.

NOTES (2026-09-07): fix-retry — deleted the dead `gitUnavailableMessage` const from internal/tools/git.go (was left behind unreferenced after the move); `gitexec.UnavailableMessage` is now the single copy, and `git_test.go:209` pins the sentence through the wrapper's behaviour rather than the const.

**What:** Recast at the regression check (2026-09-06). Move the program-resolution and hardened-run core of `internal/tools/git.go` (`:66-410`: `lookGit`, `gitProgram`, `resolveGit`, `runGit`, `gitRunSpec`, `safeEnvKeys`/`safeGitEnv`, the hardening options/env/args, the repo-local command-config refusal, `RunGitQuery`) into `internal/gitexec`, built on `internal/subprocess`; `gitexec` imports `security`, `domain`, `platform`, `subprocess` and stdlib. Add `gitexec.Run(ctx, dir string, env []string, timeout time.Duration, args ...string) (string, error)` — `RunGitQuery` with `env` appended to the hardening env, nothing removed — and `gitexec.RunTo(ctx, dir, env, timeout, w io.Writer, args...)`, its uncapped streaming sibling on item 7's streaming variant. `internal/tools/git.go` keeps `RunGitQuery` and every tool-facing symbol as thin wrappers so `internal/agent/treesnapshot.go:116` and every existing test are untouched. Depends on item 7.
**Regression guard.** `Run` passes `env` to `probeCommandConfig` (`git.go:343`) and the memo key (`commandConfigProbes`, `:318-335`, today `gitPath+root`) includes the effective `GIT_DIR` (or the env). `gitexec` exports the look-up seam (`gitexec.LookPath` var, or `Resolve(ctx, root, look)`) and `tools.resolveGit` passes `tools.lookGit` through, so `withFakeGit` (`git_test.go:23-32`) keeps working. `Run` = `RunGitQuery` (`git.go:254-284`; it screens nothing but `len(args)==0`) plus `env` after `gitHardeningEnv` (`:227-228`). `gitexec` carries `gitUnavailableMessage` (`git.go:1236`, pinned by `git_test.go:209`) verbatim — tools re-uses that const — and reads the box via `domain.ConfinementFromContext` (`confinementBox`, `path_safety.go:40`, stays in tools).
**Files:** `internal/gitexec/{doc.go,gitexec.go,gitexec_test.go}`, `internal/tools/{git.go,git_test.go}`.
**Tests:** the moved hardening tests under `internal/gitexec`; `Run` refuses a writable-path git as `RunGitQuery` does and passes the extra env; a `credential.helper` in the workspace's own `.git/config` refuses the tool run, never a `GIT_DIR` run of the same root; `RunTo` streams >256 KiB untruncated; the no-git / planted-git cases (`git_test.go:243,262,433,708`, `delete_file_test.go:222`) unchanged.
**Acceptance:** `go build ./... && go test ./internal/gitexec/ ./internal/tools/ ./internal/agent/ -run 'Git|Tree|Snapshot'`
**Commit:** `refactor(gitexec): one hardened git runner shared by the tools and the snapshot store`

## 9. `internal/snapshot` — the session-owned object store — ✅ DONE (2026-09-07)

NOTES (2026-09-07): `Content` asks existence with `ls-tree -r -z <tree> -- ':(literal)<path>'` before `cat-file blob`, rather than reading the blob and reading a failure as absence — `ls-tree` exits zero whether or not the path is in the tree, so a real failure (no git, a corrupt store, a timeout) stays an error instead of quietly becoming "this file did not exist", which a revert would act on by deleting. `:(literal)` stops a path holding glob characters from being read as a pattern. Two git calls per path, on an interactive revert only.

NOTES (2026-09-07): `Capture` deliberately does not treat `git add`'s non-zero exit as fatal — with `--ignore-errors` it is the NORMAL outcome for a workspace holding a commit-less nested repository (verified: exit 1, everything else staged) — but the add error is folded into the failure message when `write-tree` also fails, so it is never swallowed silently.

NOTES (2026-09-07): added beyond the item's literal surface, both a few lines: `ParseTree(string) (Tree, error)` (item 11 persists tree ids as strings in `journal.json` and item 10's `Snapshotter` interface is string-typed, so the ids have to be validated on the way back in), and an `Open` refusal for a store directory inside the workspace (ADR 0074 decision 1's "nothing is ever written inside the workspace" made unlosable at the seam). Both are covered by tests.

NOTES (2026-09-07): `snapshotTimeout` is 60s — the item asks for "generous" without a figure; the floor's 2s is the wrong scale here because a capture that gives up early loses the only way back from an Exchange.

NOTES (2026-09-07): no package map test (`docmap.Check`) — three files, well under the ~10-file rule the convention uses, matching `internal/gitexec` and `internal/subprocess`.

**What:** Recast at the regression check (2026-09-06). New package with `type Store`: `Open(ctx, dir, workspace string) (*Store, error)` creates `dir` (0700) as a bare repository whose work-tree is `workspace` and whose index lives inside `dir`, writing nothing under `workspace`; `Capture(ctx) (Tree, error)` stages the whole work-tree (`add -A`, the workspace's own `.gitignore` respected, a nested `.git` never entered) and returns `write-tree`'s id; `Diff(ctx, a, b Tree) ([]string, error)` = `diff-tree -r -z --name-only` (added and removed included); `ListBlobs(ctx, tree Tree) (map[string]string, error)` = `ls-tree -r -z` (path → blob id); `Content(tree Tree, path string) (data []byte, exists bool, err error)` reads `<tree>:<path>` (no ctx — a bounded `snapshotTimeout` applies internally); `Remove(dir) error`; `Available() bool`. Every git call goes through `gitexec.Run` (`RunTo` for `Content`/`Diff`/`ListBlobs`) with `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE` set and a generous `snapshotTimeout`. `Tree` is a validated 40/64-hex string type. Restoration stays in `internal/undo`. Depends on item 8.
**Regression guard.** `Content`, `Diff` and `ListBlobs` never go through the 256 KiB-capped capture; `diff-tree`/`ls-tree` run with `-z` and split on NUL, blobs addressed as `<tree>:<path>`; `Capture` passes `-c core.excludesFile=` (empty) so only the work-tree's own `.gitignore` files exclude. `Capture` runs `add -A --ignore-errors`, exit 1 = staged what it could (`write-tree` decides; a commit-less nested repository fails a plain `add -A`, exit 128). `ListBlobs` keeps `blob` entries only (a nested repository is `160000 commit`; `120000` symlinks read as link-target bytes). `Open` passes `--object-format=sha1` (HOME is on `safeEnvKeys`, `git.go:67`, so `init.defaultObjectFormat` reaches it).
**Files:** `internal/snapshot/{doc.go,store.go,store_test.go}`.
**Tests:** (git-gated, `requireGit` pattern from `internal/agent/treesnapshot_test.go:50`) capture → edit/add/delete/ignored-path → capture: `Diff` names exactly the three unignored paths, `Content` returns pre bytes and `exists=false` for the added path; a >256 KiB blob round-trips; `café.txt` unescaped in `Diff`/`ListBlobs` and readable via `Content`; a path excluded only by a temp-`HOME` `core.excludesFile` is captured; `ListBlobs` ids equal `sha1("blob <len>\0"+data)` even under a temp `HOME` with `init.defaultObjectFormat=sha256`; a commit-less nested repository is skipped, the rest captured; no `.git` under the workspace; a workspace that IS a repository keeps `git status --porcelain` identical; `Open` on an existing dir reopens.
**Acceptance:** `go build ./... && go test ./internal/snapshot/`
**Commit:** `feat(snapshot): session-owned git object store capturing whole-tree pre/post images`

## 10. Snapshot-backed groups and the redo stack in `internal/undo` — ✅ DONE (2026-09-07)

NOTES (2026-09-07): added `WithWorkspace(root)` beside the item's `WithSnapshotter(s)` — a tree spells its paths relative to the work-tree, so without the root the journal cannot turn a diff path into the absolute address `Preview` discloses and `Revert` writes; a snapshotter given without a workspace is ignored, leaving the supported funnel-only journal rather than a half-wired one.

NOTES (2026-09-07): the item's Files list named `journal.go` alone for the source; the new code landed in new files `internal/undo/snapshot.go` (Snapshotter seam, capture points, diff-only paths) and `internal/undo/redo.go` (the redo stack) under the run's standing `coding-standards` requirement, with `journal.go` still at 690 lines. No existing content was moved between files, and `doc.go`'s package map names both new files. The new tests are in `snapshot_test.go` for the same reason; `journal_test.go` needed no change and passes untouched, which is the item's `New()` test.

NOTES (2026-09-07): `Close` advances the generation stamp when a non-empty diff materialises the group, so ADR 0051 decision 7's staleness guard also moves for an Exchange that wrote only outside the funnel; the item specified only the redo-stack clearing at that point.

NOTES (2026-09-07): the item fixed the undo skip wordings but not the redo ones; the mirror wordings are "changed since the undo restored it", "deleted since the undo restored it" and "recreated since the undo removed it".

**What:** Recast at the regression check (2026-09-06). `internal/undo` gains `type Snapshotter interface { Capture(ctx) (string, error); Diff(ctx, a, b string) ([]string, error); ListBlobs(ctx, tree string) (map[string]string, error); Content(tree, path string) ([]byte, bool, error) }` (`undo` never imports `snapshot`). `New(opts ...Option)` with `WithSnapshotter(s)` (`New()` = today); `WithIndexPath(p)` is item 11's. A group gains `pre`/`post` tree ids, their per-path blob ids and `touched []string`. `MarkPre(ctx) error` captures the pre tree once per open group (no-op without a Snapshotter); `Close(ctx) error` captures post when a pre exists, stores both trees' blob ids via `ListBlobs`, and drops the group only when the diff and the funnel entries are both empty. `Record` keeps today's full pre-image entry for every funnel-journaled path, in-root included; the diff only ADDS paths the funnel never saw, and a funnel entry wins over a diff path. `Preview`/`Revert` stay ctx-free: funnel entries plus diff-only paths (restore via `Content(pre, path)` when present in pre, delete otherwise); conflict rule: git blob id of the current bytes (`sha1("blob <len>\0"+data)`, sha256 for a sha256 store) ≠ stored post id → skip `changed since the agent wrote it`; absent → `deleted since the agent wrote it`; writes stay on `security.SafeWriteFile`/`SafeRemove`. A reverted group moves to a redo stack; `RedoPreview() (Step, bool)` and `Redo(generation) (Report, error)` mirror them with pre/post swapped; `ErrNothingToRedo`; a materialised group clears the redo stack (guard). `Wrote()` = touched ∪ diff ∪ escapes. Depends on item 9.
**Regression guard.** A group whose `MarkPre` never ran or whose `Capture` failed is a funnel-only group exactly as today and `Close` keeps it. `Preview()`, `Revert()`, `RedoPreview()`, `Redo()` keep ctx-free signatures so `Agent.UndoPreview/UndoRevert` (agent.go:819,842), `tui.Engine` (tui.go:723,732), `lateEngine` (wire_engine.go:516,527) and `internal/tools/undo_journal_test.go` compile untouched. The redo stack is cleared where the group materialises — first `Record`, or `Close` with a non-empty diff — never in `BeginGroup` (`loop.go:98-99` calls it on every Exchange open, writes or not). A funnel entry keeps only `postHash` (`journal.go:131-143`): entries keep `post` whole when their group has no snapshot (funnel-only groups, every escape), a snapshot-backed group reads post bytes via `Content(post, path)`, and `Redo` skips a path with neither as `no post-image recorded`.
**Files:** `internal/undo/{journal.go,journal_test.go,doc.go}`.
**Tests:** in-package fake Snapshotter (tree → path → bytes, `ListBlobs` computing blob ids); a three-Exchange revert walks back one at a time; a funnel-written in-root path the diff omits reverts from its pre-image; a funnel-only group (no `pre`) is kept by `Close` and reverts as today; edit-after-write skips with the exact reason; redo restores post bytes and skips a path edited after the undo; a materialised group clears redo while a `BeginGroup` with no write between undo and redo leaves it; escapes revert AND redo with and without a Snapshotter; `New()` passes the existing suite; race clean.
**Acceptance:** `go build ./... && go test -race ./internal/undo/ ./internal/tools/ -run 'Undo|Journal|Revert|Redo'`
**Commit:** `feat(undo): snapshot-backed Exchange groups with diff-scoped revert and a redo stack`

## 11. Persist the journal index beside the objects — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the item's file list is a floor — `snapshot.go` (`Close`) and `redo.go` (`Redo`) also changed, because "the journal saves itself after every `Close`, `Revert` and `Redo`" cannot be wired from `journal.go` alone; each ends with the shared `persist()` and returns its error without disturbing the in-memory state.

NOTES (2026-09-07): added beyond the item's literal surface, all covered by tests: `Load` MATERIALISES each record through the Snapshotter it is handed (`Diff` + `ListBlobs` per group, under `context.Background()` since `Load` has no ctx and the store bounds its own git calls) — without it a loaded group carries tree ids but no diff or blob ids, so `/undo` (item 14) and `apogee undo` (item 15) would preview an empty step; `GroupRecord.Ordinal` is derived from position on the way out and CHECKED against position on the way in, so a truncated or hand-edited index is an error naming the path rather than a stack numbered differently to the one the human confirms; `Load` refuses records with no snapshotter to read them with, rather than dropping steps a later `Save` would then overwrite; an unknown `Version` is refused the same way.

NOTES (2026-09-07): `group` gained an unexported `generation` field, stamped at `Close`, to give `GroupRecord.Generation` something to carry — nothing in a step reads it and it does not change when a group moves between the stacks.

NOTES (2026-09-07): `doc.go`'s "What this package deliberately is NOT" opened with "It is memory, not storage … a resumed session cannot revert an earlier process's writes"; that claim is what this item falsifies, so it was replaced with a Persistence paragraph (index of tree ids, workspace check, what stays in memory) and the ADR 0022 §8 point kept — the store is host state keyed by session id, outside the session record.

**What:** `internal/undo` gains `journal.json` persistence: `type Index struct { Version int; Workspace string; Generation uint64; Groups, Redo []GroupRecord }` with `GroupRecord{Ordinal, Generation uint64; Pre, Post string}`; `Journal.Save(path) error` (atomic temp+rename, 0600) and `Load(path, snap Snapshotter, workspace string) (*Journal, error)` (missing file = empty journal; workspace mismatch = `ErrWorkspaceMismatch`, the caller falls back to a fresh in-memory journal). Escape entries are never persisted (ADR 0074). The journal saves itself after every `Close`, `Revert` and `Redo` when `WithIndexPath` was given; a save failure is returned, never panics, and leaves the in-memory state intact. Depends on item 10.
**Regression guard.** `Save` skips (and `Load` never materialises) a group whose pre and post tree ids are equal — an Exchange whose only writes were approved out-of-workspace paths (D6) is a per-process, funnel-only group that must not survive as an empty step; state it in `persist.go`. `Load(path, snap, workspace)` returns a journal already carrying `WithIndexPath(path)`.
**Files:** `internal/undo/{persist.go,persist_test.go,journal.go,doc.go}`.
**Tests:** round-trip preserves ordinals, generation and both stacks; a pre==post group is absent from the file and not counted by `Ordinal`; a loaded journal saves back to the same path after `Close`; its next generation continues from the file; mismatch error; corrupt file → error naming the path; atomic save (no partial file after a simulated failure).
**Acceptance:** `go build ./... && go test ./internal/undo/`
**Commit:** `feat(undo): journal.json persists the snapshot index across relaunches`

## 12. Engine wiring — capture points, resume, `undo-snapshots` — ✅ DONE (2026-09-07)

NOTES (2026-09-07): `snapshot.OpenJournal` carries two reasons beyond the item's three — `no apogee home` (an empty home or session id, which item 13 requires by that exact wording) and `no workspace` — because neither call can name a store to open, and both are fallbacks with a reason rather than errors, which is the item's own rule for everything that is not a broken index.

NOTES (2026-09-07): exported `snapshot.Dir(home, sessionID)` beside `OpenJournal`: the store path convention is spelled once so the open, item 13's GC sweep and its session delete cannot drift apart. Not in the item's literal surface; it is the path `OpenJournal` itself composes.

NOTES (2026-09-07): consequential edit — internal/snapshot/doc.go: made necessary by internal/snapshot/journal.go (the package now imports internal/undo, which the "imports nothing else of apogee's" sentence denied, and the Files map needed the new file).

NOTES (2026-09-07): consequential edit — internal/config/config_test.go: made necessary by the `undo-snapshots` registry row (the fixtures that enumerate every key's default, every Options field a key owns, and a file stating every key).

NOTES (2026-09-07): the `undo-snapshots` row in the `/settings` dispatcher table sits between `delegate-max-steps` and `remember-model` rather than beside `ui.inspector`, because TestSettingsTableIsInRegistryOrder pins the table to `config.KeyRegistry` order.

NOTES (2026-09-07): `internal/config/defaults/config.yaml` gained no `undo-snapshots:` stanza — the item did not list the file and no test requires one; the shipped template therefore documents `tool-call-salvage` but not this key.

**What:** Recast at the regression check (2026-09-06). `snapshot.OpenJournal(ctx, home, sessionID, workspace string, enabled bool) (*undo.Journal, string, error)` returns a snapshot-backed journal loaded from `~/.apogee/snapshots/<sessionID>/journal.json`, or `undo.New()` plus a reason (`git not found`, `undo-snapshots is off`, `workspace mismatch`). `Agent.SetJournal(j *undo.Journal, note string)` injects it — legal before the first Submit and at a quiescent boundary (`ClearContext`/`RestoreSession`), mirrored on `apogee.New/Resume` (apogee.go:58-62); `construct.go:126` keeps `undo.New()` as default — and `UndoNote() string` surfaces the reason through the Engine seam. Capture points: in `executeTool` (`dispatch.go:1032`), before `runTool`, when `!domain.IsReadOnly(tool)` call `a.journal.MarkPre(floorCtx)` (`:1107`); a delegated child shares the parent's journal pointer (ADR 0051 D1). At Exchange close — `turnLifecycle.closeExchange` (`turn.go:187`), the one owner of Exchange end (endExchangeDone, endAbandoned, endStepCapped, AbortExchange), never endCancelled, gated on `a.depth == 0` as BeginGroup is at `loop.go:98` — call `a.journal.Close(ctx)`; a capture error is emitted as `domain.ErrorEvent{Source: "undo"}` through `a.cfg.Events` and never fails the Exchange. `Agent.RedoPreview`/`RedoRevert` beside `UndoPreview`/`UndoRevert` (`agent.go:815-834`). Register `undo-snapshots` (`KindBool`, default `true`, Editable, Desc `Snapshot the workspace around each exchange so /undo survives a relaunch and covers subprocess and MCP writes; takes effect at the next start.`) with its `domain` field, config/options rows (`config.go` :697/:1338 pattern), a startup-only apply row (the `ui.inspector` shape, wire_settings_test.go:451) and the `docs/manual/configuration.md` line (item 16 keeps `:1599`). Depends on item 11.
**Regression guard.** `closeExchange` (`turn.go:187`), `end` (`:83`) and `AbortExchange` (`agent.go:707`) carry no ctx, Agent or turn: `turnLifecycle` gains `onClose func()`, set at `construct.go:137`, implemented on `Agent` with a bounded background ctx and `l.index` for the `ErrorEvent`; `closeExchange` stays the single caller, its fourth row endStepCapped (`turn.go:166`) inert for a child by the depth-0 gate. `undo-snapshots` is added to the `settingKeysWithNoMemberToReach` list (cmd/apogee/wire_settings_test.go:436 at BASE) and to `TestEveryEditableSettingKeyHasAnApply` (:549) in this item.
**Files:** `internal/snapshot/{journal.go,journal_test.go}`, `internal/agent/{construct.go,dispatch.go,loop.go,turn.go,agent.go,undo_group_test.go,undo_surface_test.go}`, `apogee.go`, `internal/domain/config.go`, `internal/config/{registry.go,config.go,options.go}`, `cmd/apogee/{wire_settings.go,wire_settings_test.go,settingsrows_test.go}`, `docs/manual/configuration.md`.
**Tests:** (git-gated) `mutatingSubprocessTool` (`treesnapshot_test.go:26`) via `executeFake` writes a file the funnel never saw → `UndoPreview` lists it, `UndoRevert` restores it; `AbortExchange` and a step-capped Exchange still close the group; a cancelled Turn leaves it open; a child's Exchange end never closes the parent's group and a later parent write reverts with it; a read-only call never captures; a capture error arrives as `ErrorEvent{Source: "undo"}` and the Exchange completes; `OpenJournal` reasons; settings rows fixture; registry/editable-key/manual tests green.
**Acceptance:** `go build ./... && go test -race ./internal/agent/ -run 'Undo|Redo|Snapshot|Tree|Abort' && go test ./internal/snapshot/ ./internal/config/ && go test ./cmd/apogee/ -run 'SettingsRows|EveryEditableSettingKey|ManualDocumentsEverySettingsKey'`
**Commit:** `feat(agent): snapshot every writing Exchange at the tool choke point and expose redo`

## 13. Driver wiring — TUI session, headless and daemon Firings, GC, delete — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the item's Files list names `cmd/apogee/wire_test.go`, which does not exist in this tree — wire.go's tests live in wire_session_test.go (gcScratchDirs, gcSessions) and wire_live_test.go, so the new sweep, host and journal-open tests went there beside their siblings.

NOTES (2026-09-07): the snapshot sweep is called ONCE, beside `gcSessions` in wire_live.go, with both of its rules rather than splitting the age rule back to wire.go:108 — the guard's "the age rule alone MAY stay at :108" is permission, and one call site keeps the two rules and the `keep` list in one place. `gcSnapshotDirs` itself is defined beside `gcScratchDirs` in wire.go as the item directs.

NOTES (2026-09-07): a session store that cannot be LISTED disables the record-missing rule rather than deleting on a guess (every store would otherwise read as record-less); the age rule still runs. Not stated by the item, and the honest reading of a best-effort sweep.

NOTES (2026-09-07): `stateRoots.snapshots` spells `<home>/snapshots` while `snapshot.Dir` composes the same path in internal/snapshot; TestSnapshotRootMatchesStoreDir pins the two together so the swept root and the opened store cannot drift.

NOTES (2026-09-07): consequential edit — internal/agent/agent.go: made necessary by the Driver re-opening the journal at every session boundary — `ClearContext`'s "the journal survives /clear" paragraph and the `journal` field's "never serialized … a resumed session starts with an empty one" claim were both made false by this item (the first is the supersession the item's text names, per item 6's own NOTES line).

NOTES (2026-09-07): consequential edit — internal/run/doc.go: made necessary by `run.Once` keying its undo store on `Spec.RecordID` — the doc named the scratch dir as the only thing a caller keys on that id.

**What:** Recast at the regression check (2026-09-06). `cmd/apogee/wire.go`: once the session id is known, open the journal with `snapshot.OpenJournal` using `resolveRoots`' home (`:388`) and hand it to the Agent via `SetJournal`; `stateRoots` gains `snapshots`; `gcSnapshotDirs` beside `gcScratchDirs` (`:453`), swept where `:108` sweeps scratch: remove a snapshot dir older than `scratchMaxAge`, or whose session record is missing AND whose mtime is older than 24h, passing the active/resumed id as `keep` as `gcSessions` does (`wire.go:487-500`). On Rotate (`/new`, `/clear`, `wire_session.go:158-167`) and Activate (`/sessions` resume, `:185-192`) the journal is reopened under the new id through a `journalMoved` seam beside `followScratch` (`:167,:191`) and swapped on the Agent via `SetJournal`. `internal/run/run.go` `Once` opens the journal the same way for the session id it records under (home from `cfg.ConfigDir`, `wire_firing.go:264`; flag from the domain field). `newSessionHost` (`wire_session.go:82-84`) gains a `snapshotsRoot`; `sessionHost.Delete` (`:225`) removes the snapshot dir via `snapshot.Remove`. `lateEngine` (`wire_engine.go:516-527`) gains `RedoPreview`/`RedoRevert` returning `undo.ErrNothingToRedo` while unbound. Depends on item 12.
**Regression guard.** An `OpenJournal` error never fails a start or Firing — fall back to `undo.New()` with the error text as the reason. Supersedes the journal-survives-`/clear` note at `internal/agent/agent.go:1097-1100` (ADR 0074, item 6). The record-missing rule runs beside `gcSessions` (`wire_live.go:220`, store and `keepSessions` in hand; `wire.go:108` predates the store at `wire_live.go:203`), the age rule alone may stay at `:108`. `lateEngine` keeps a `pendingJournal` mirroring `pendingScratch` (`wire_engine.go:102-106`), applied at bind as `agent.SetScratchDir(*s)` is at `:182`. `run.Once` with an empty `cfg.ConfigDir` (`domain/config.go:164-168`) yields `undo.New()` with the reason `no apogee home`.
**Files:** `cmd/apogee/{wire.go,wire_test.go,wire_live.go,wire_engine.go,wire_session.go,wire_session_test.go,wire_firing.go,wire_firing_test.go}`, `internal/run/{run.go,run_test.go}`.
**Tests:** (git-gated) a `run.Once` whose scripted tool writes a file leaves one group in `<home>/snapshots/<id>/journal.json`; GC removes an aged dir and a record-less dir older than 24h, keeps a younger record-less dir and the `keep` id; Rotate reopens under the new id, the old `journal.json` untouched; `sessionHost.Delete` removes the dir; a home without git yields the in-memory journal and a reason; a corrupt `journal.json` yields `undo.New()` with the error as the reason and the start succeeds; a `/clear` before bind lands the moved journal at bind; an empty `ConfigDir` yields the `no apogee home` reason.
**Acceptance:** `go build ./... && go test ./internal/run/ && go test ./cmd/apogee/ -run 'Snapshot|Scratch|Wire|Delete|Session|Firing'`
**Commit:** `feat(cmd): open the session's undo snapshots for the TUI, headless and daemon Drivers`

## 14. `/undo` reads the persistent journal; `/redo` joins it — ✅ DONE (2026-09-07)

NOTES (2026-09-07): `PreviewLines`/`ReportLines` head their listing with a verb-free line (`exchange 3:`, `exchange 2: 1 restored, …`) and the Driver supplies the verb and the applying line (`revertNote` in internal/tui/undo.go); `/undo`'s old head clause "the most recent one that wrote files" was dropped, because it is false of a redo step and of item 15's `apogee undo <session-id>`, which share this listing.

NOTES (2026-09-07): `NothingLines` parenthesises the engine's reason verbatim — `… for this session (git not found)` — rather than the item's illustrative `(undo snapshots off: git not found)`: snapshot.OpenJournal's reasons already read as the cause, and its `undo-snapshots is off` would otherwise be doubled by the prefix.

NOTES (2026-09-07): `parseUndo` and the new `parseRedo` are two lines over one shared `parseRevert(verb, usage, args)`, and `/redo` reuses `undoAction` rather than declaring a second identical type — the two verbs read one grammar, and nothing existing was renamed.

NOTES (2026-09-07): consequential edit — internal/undo/doc.go: made necessary by adding internal/undo/notes.go (the package map enumerates every non-test file).

NOTES (2026-09-07): the pure note-builder table tests moved to internal/undo/notes_test.go with the builders, as the item directed; the TUI's verb heading and confirm hint stay covered by the `/undo` and `/redo` routing tests, which read them off `plain(m.View())`.

NOTES (2026-09-07): `internal/tui/tui.go:720` still says the journal is "memory, not storage" in `UndoPreview`'s doc comment — left as is, because item 16 names that exact line among the code-prose fixes it owns.

**What:** move the pure note builders in `internal/tui/undo.go` (`:112-136`) into `internal/undo/notes.go` as Driver-neutral `PreviewLines(Step) []string` / `ReportLines(Report) []string` / `NothingLines(reason string) []string`, called from the TUI. Add `/redo` with the grammar of `/undo` (`command.go:594-635`): bare = preview, `confirm` applies, idle-only, generation stamp (`model.go:159` gains `redoGeneration` or the stamp is shared — one field, documented), summary `put back what the last /undo removed (bare = preview)`, usage `usage: /redo | /redo confirm`. Rewrite `undoNothingNote` (`undo.go:147-150`): it names the engine's `UndoNote` reason when one exists (`nothing to undo — no agent file writes are recorded for this session (undo snapshots off: git not found)`), else the plain first line. The `Engine` interface (`tui.go:723-732`) gains `RedoPreview`/`RedoRevert`/`UndoNote`. Depends on item 13.
**Regression guard.** `fakeEngine` (`internal/tui/seam_test.go:115`) gains the three methods with scripted redo step/report/err/note fields beside `undoStep`/`undoReport` (`:142-146`). `/redo` is routed in the idle-only switch at `internal/tui/commandrun.go:465-470` with a `case "redo"` beside `"undo"` calling `runRedo`. `UndoNote() string` is a METHOD on the interface, on `Agent` (item 12) and on `lateEngine` (`wire_engine.go:516`), so `var _ tui.Engine = (*apogee.Agent)(nil)` (`wire.go:69`, `wire_engine.go:120`) keeps compiling.
**Files:** `internal/undo/{notes.go,notes_test.go}`, `internal/tui/{undo.go,undo_test.go,command.go,command_test.go,commandrun.go,model.go,tui.go,seam_test.go,doc.go}`, `cmd/apogee/wire_engine.go`.
**Tests:** `runUndoLine`-style key-path tests for `/redo`, `/redo confirm`, stale generation and nothing-to-redo via `fakeEngine`; the nothing-note with and without a reason on `plain(m.View())`; the note builders' table tests move with them.
**Acceptance:** `go build ./... && go vet ./cmd/apogee/ && go test ./internal/undo/ ./internal/tui/ -run 'Undo|Redo|Command|NoBuilder'`
**Commit:** `feat(tui): /redo, and /undo speaks for the persistent journal`

## 15. `apogee undo <session-id> [confirm]` and the Firing report line

**What:** new top-level cobra verb `undo` in `cmd/apogee/undo.go`: `apogee undo <session-id>` previews, `… confirm` applies; it validates the id as a single clean path component, opens `~/.apogee/snapshots/<id>/` via `snapshot.OpenJournal` with the workspace read from `journal.json`, prints `undo.PreviewLines`/`ReportLines`, and exits non-zero with `nothing to undo for session <id>` / `undo snapshots unavailable: <reason>` otherwise; `--workspace` is refused. `run.Result` (`internal/run/run.go:82`) gains `UndoNote string` (the reason `run.Once` opened the journal with; empty = snapshot-backed). The two `writtenFilesLines` callers (`headless.go:609`, `daemonfire.go:457`) append `  undo with: apogee undo <session-id>` when `res.SessionID != "" && res.UndoNote == ""`. Fix for bead apogee-kk0.7. Depends on item 14.
**Regression guard.** `writtenFilesLines(paths []string)` (`headless.go:708`) keeps its signature — `daemonfire_test.go:773` calls it with `[]string`. The verb registers `--config` exactly as `headless.go:253` does and resolves the home through `resolveRoots` (`wire.go:388`), never bare `config.ApogeeHome("")` (`internal/config/config.go:3380`, no env fallback).
**Files:** `cmd/apogee/{undo.go,undo_test.go,headless.go,headless_test.go,daemonfire.go,daemonfire_test.go}`, `internal/run/run.go`, `cmd/apogee/root.go` (grep `AddCommand`).
**Tests:** (git-gated, temp home via `--config`) a headless run that writes a file prints the `undo with:` line, one with a non-empty `UndoNote` prints none; the daemon report carries the same line; the test lifts that line, runs the verb it names, and the file is restored; preview then confirm; invalid id refused; unknown id → `nothing to undo`; no git → the reason.
**Acceptance:** `go build ./... && go test ./cmd/apogee/ -run 'Undo|Headless|Daemon'`
**Commit:** `feat(cmd): apogee undo <session-id> reverts a headless or daemon Firing`

## 16. Manual, README and index prose for persistent undo

**What:** Recast at the regression check (2026-09-06). Rewrite the `/undo` section (`docs/manual/commands.md:298-320`) and its table row (`:41`) for snapshots, coverage (workspace tree incl. subprocess/MCP writes; `.gitignore`d paths and out-of-workspace approved writes are the residue), `/redo`, persistence and the git-absent fallback; fix `docs/manual/configuration.md:1599` (the key line landed in item 12); replace `docs/manual/headless.md:80-82` with the `undo with:` line and the verb; update `README.md:55,189`, `docs/manual/README.md:9`; update the code prose stating the old limit (`internal/undo/doc.go:33-42`, `internal/agent/dispatch.go:1075-1081`, `internal/agent/loop.go:88-97`, `internal/run/run.go:140-144`, `cmd/apogee/headless.go:697-703`, `internal/tui/doc.go:186-190`, `internal/tui/tui.go:720`, `cmd/apogee/daemonfire.go:451`, `cmd/apogee/daemonfire_test.go:760`, `cmd/apogee/headless_test.go:1627`). Depends on item 15.
**Regression guard.** Rule: every sentence stating the journal is per-process or memory-only goes; the rule and the Acceptance share one scope (`README.md CONTEXT.md docs/manual internal/ cmd/`) and one journal-scoped pattern (below); ADRs and archived plans are historical and stay. Also covered: `docs/manual/daemon.md:81-82` (`lives only as`), `internal/agent/state.go:60`, `agent.go:264`, `internal/undo/journal.go:153`; the `internal/tui/undo.go:149` and `undo_test.go:195,289` hits are item 14's `undoNothingNote` rewrite — this Acceptance is green only after it.
**Files:** `docs/manual/{commands.md,configuration.md,headless.md,daemon.md,README.md}`, `README.md`, `internal/undo/{doc.go,journal.go}`, `internal/agent/{dispatch.go,loop.go,state.go,agent.go}`, `internal/run/run.go`, `cmd/apogee/{headless.go,headless_test.go,daemonfire.go,daemonfire_test.go}`, `internal/tui/{doc.go,tui.go}`.
**Tests:** the grep below; `go build ./... && go vet ./cmd/apogee/ ./internal/tui/` (comment/string-only code edits).
**Acceptance:** `grep -rniE "journal[^\n]*(died|dies|lives) (with|only as long as) the (process|run)|memory, not storage|persists no undo|journal is memory|died with the run|starts with an empty (journal|one)|per process and in memory|starts empty each run|lives only as" README.md CONTEXT.md docs/manual internal/ cmd/; test $? -eq 1 && go build ./...`
**Commit:** `docs(undo): the manual, README and code prose describe persistent snapshot undo`
