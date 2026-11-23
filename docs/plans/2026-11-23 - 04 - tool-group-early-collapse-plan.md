# Tool group stays open through a tool burst — plan

**Goal:** under `ui.tools-auto-collapse`, an untouched tool group stays open while consecutive Turns keep adding tool calls to it, and folds only when it grows past the size rule or when something that will not join it follows: a reply or narration, a sub-agent group, a note, a user message, or the end of the run. A new Turn starting is no longer by itself a reason to fold.

**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** aec2f503
**Closes:** tool-group-early-collapse

**Sources:**
- `.capcom/issues/tool-group-early-collapse.md`
- ADR 0035 amendments: `capcom know show adr-0035-amend-1118`, `adr-0035-amend-1120`, `adr-0035-amend-1120b`, `adr-0035-amend-1120c`
- `layout.md` "Which fold holds an umbrella"; `docs/layout/tool-layout.md` "Fold states and interaction"
- `docs/design/test-drivers.md` §Writing a new e2e test, §Waiting

**Ratified design calls:**
- **Threshold:** the size trigger is `ui.tools-fold-over` (type rows, not calls); no new setting (owner, 2026-11-23, in the issue).
- **Fold moment for a reply:** the group folds when the run's next streamed reply/narration text first turns non-blank — not when it commits — and the fold is sticky (a stream reset does not reopen it) (owner, 2026-11-23).
- **Reasoning never folds:** reasoning/thinking output is not a follower (owner, 2026-11-23, in the issue).
- **Size rule kept:** a live untouched umbrella past `ui.tools-fold-over` folds by the existing size rule (follows `ui.tools-open`, default false = folded); auto-collapse adds no size fold of its own. Sub-agent and Skill groups have no type rows, so they fold only on a follower (owner, 2026-11-23).

**Standing requirements:**
- skills: coding-standards
- Driven tests use `internal/tuitest` + `internal/stubllm`, never an `httptest` upstream closure (AGENTS.md).
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- New settings keys, or any change to `ui.tools-open` / `ui.tools-fold-over` semantics.
- The touched-group rules, the wait-on-user open, and auto-collapse off — unchanged.
- Engine, `domain` events, session record: none change (TUI view state only, ADR 0031).

**Regression check (2026-11-23, aec2f503):**
- 1: guard folded (writer decision: prose rule extends to `cmd/apogee/`, bite rests on the unit tests), plus `turnBoundary` kept, `streamBuf` non-blank flag, reset-joined marked block, Files widened.
- 2: guard folded (new turns ahead of every wrap-up, keyed on their own seeded files via `launchTUIIn`).
- 3: guard folded (prose rule widened to every next-Turn/step/boundary settle sentence; glossary `group-summary.md` and the shipped `config.yaml` comment added).

## 1. Settle a tool block on a follower, not on the next Turn's start — ✅ DONE (2026-11-23)

NOTES (2026-11-23): internal/tui/autocollapse_test.go is on the item's Files line but needed no change — none of its tests or comments depend on the Turn-boundary settle (its tests pass unchanged).
NOTES (2026-11-23): the rewritten autocollapse_turnboundary_test.go splits the cases into five tests (TestAutoCollapseKeepsABurstOpenAcrossTurns, TestAutoCollapseFoldsOnTheReplyText, TestAutoCollapseSettlesOnlyTheStreamingRunsBlock, TestAutoCollapseKeepsATextFoldedBlockFolded, TestAutoCollapseTurnBoundaryAndTouch); turnBoundary is kept for TestGroupOfOneGrowsIntoTypeRows.
NOTES (2026-11-23): consequential edit — cmd/apogee/e2e_toolsautocollapse_test.go: the autoCollapseHoldWindow comment described a fold seen during the hold; it now describes the open header the renamed awaitOpenWhileHeld waits for.

**What:**
**Goal:** under auto-collapse a finished, untouched foldable block (umbrella, group of one, Sub-agent or Skill group) is settled only when (a) a later entry of its own run follows it, (b) its run is over, or (c) its run has streamed non-blank reply/narration text since the block's last member — sticky once set. A `TurnEvent` with `StatusTurnComplete` settles nothing. A block the reader touched, a member waiting on the user, and auto-collapse off behave as at the header base. This fixes issue `tool-group-early-collapse`: the Turn-boundary settle folded a burst after its first call.

**Regression guard.** item 1's prose rule also covers cmd/apogee/ — every comment in cmd/apogee/e2e_toolsautocollapse_test.go and cmd/apogee/testdata/stubllm/toolsautocollapse.yaml that states the Turn-boundary settle is rewritten in item 1 (add the yaml to its Files line, and extend its grep to cmd/apogee/); item 1's bite rests on its unit tests, not on the flipped e2e tests.
Rewrite only sentences stating the settle: "Turn boundary" also hits correct prose (`sink.go:47`, `fold.go:247`, `model.go:2746`); the `markRunning` comment (`model.go:2620`) and `doc.go:846` state it and are rewritten here. Keep `turnBoundary` defined in the rewritten `autocollapse_turnboundary_test.go` — `TestGroupOfOneGrowsIntoTypeRows` (`groupofone_test.go:166`) calls it.
Carry a `nonBlank` flag on `streamBuf`, set in `append`/`appendBuf` so it survives park/unpark, and test only the arriving escape-stripped chunk while the buffer is still blank — never `strings.TrimSpace` over the joined buffer per `TokenEvent` (O(n²) on the streaming hot path).
Cases (6) and (3)'s sticky extension build the marked block with a joining member as tokens → `StreamResetEvent` → call (→ `ApprovalRequested`): non-blank text commits as narration at the next call (`finalizeNarration`) and splits the block, so a reset is the only path where a call joins a block the text marked.

**Approach (assumed at the header base):** in `internal/tui/transcript.go`, `transcript.apply`'s `domain.TurnEvent` case stops calling `transcript.closeTurn`; the session-only mark `entry.turnClosed` is re-purposed (rename it, e.g. `entry.passed`, and `closeTurn` to e.g. `passBlock`) and set from `transcript.appendToken` instead: on the chunk that first makes the arriving run's buffer non-blank (after `unpark`), mark the run's last own entry (`lastOwnEntry`) when it ends a foldable block (`toolBlockAt(...).foldable()`). The `lastOwnEntry` walk runs once per streamed reply, on that blank→non-blank transition, never per token. `transcript.blockSettled` keeps its two rules (mark-and-not-waiting, then committed follower / run over) unchanged in shape. Rewrite every doc comment that states the Turn-boundary settle (`blockSettled`, `blockFoldBy`, `blockFolded`, `closeTurn`, `entry.turnClosed`, `apply`'s header comment, `foldBySettled`) to the new rule.

Rewrite `internal/tui/autocollapse_turnboundary_test.go` to the new rule, driven through the Model's `foldEvent` as today: (1) two Turns each issuing one call, separated only by a Turn boundary, form one open umbrella showing both calls; (2) a boundary followed by reasoning only leaves the block open; (3) a boundary followed by non-blank `TokenEvent` text folds it, and a `StreamResetEvent` after that keeps it folded; (4) blank-only tokens then a call: the call joins the open block; (5) a delegated child's text folds only the child's block, never the parent's; (6) a member held for approval keeps a marked block open. Every comment elsewhere naming `closeTurn`, `turnClosed` or the Turn-boundary settle is updated (`grep -rn "closeTurn\|turnClosed\|next Turn starts\|Turn boundary\|Turn-boundary" internal/tui/ cmd/apogee/`).

The two driven tests that pin the old timing flip in the same item: in `cmd/apogee/e2e_toolsautocollapse_test.go`, `TestE2EToolsAutoCollapseFoldsAGroupWhenTheNextTurnStarts` and `TestE2EToolsAutoCollapseRefoldsALoneCall` assert the group stands OPEN (`▼`) while the wrap-up request is held, and folded after the wrap-up lands (rename the first to match; replace `awaitFoldedWhileHeld` with an open-while-held helper).

**Files:** internal/tui/transcript.go, internal/tui/autocollapse_turnboundary_test.go, internal/tui/fold_test.go, internal/tui/autocollapse_test.go, internal/tui/model.go, internal/tui/doc.go, cmd/apogee/e2e_toolsautocollapse_test.go, cmd/apogee/testdata/stubllm/toolsautocollapse.yaml
**Read first:** internal/tui/transcript.go — appendToken, closeTurn, blockSettled, streamBuf, finalizeNarration; internal/tui/blockshape.go — toolBlockAt;
internal/tui/autocollapse_turnboundary_test.go — turnBoundary; cmd/apogee/e2e_toolsautocollapse_test.go — awaitFoldedWhileHeld

**Tests:** `go test -race -count=1 -run 'TestAutoCollapse|TestFold|TestGroupOfOne' ./internal/tui/`; `go test -race -count=1 -run 'TestE2EToolsAutoCollapse' ./cmd/apogee/`

**Acceptance:**
- `go build ./...`
- `go test -race -count=1 -run 'TestAutoCollapse|TestFold' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EToolsAutoCollapse|TestE2EToolsFold' ./cmd/apogee/`
- `grep -rn "closeTurn\|turnClosed" internal/ cmd/` prints nothing.

**Commit:** `fix(tui): a tool group settles on a follower, not on the next Turn's start`

## 2. Driven test: a multi-Turn tool burst stays open until its follower

Depends on item 1.

**What:**
**Goal:** a driven `internal/tuitest` + `internal/stubllm` test shows, under default config, a burst of tool calls across three consecutive Turns standing as one OPEN umbrella — every call's type row visible under a `▼` header — while the next Turn's request is held on a gate, and the umbrella folded to its Group summary under `▶` once the wrap-up reply lands.

**Regression guard.** A request takes the first matching `when:` turn, and every read/list/grep result in `e2eWorkspace` is already claimed by an existing wrap-up (`tool_result: read_file`/`list_dir`/`grep`, the `zebra` hold). Put the new turns AHEAD of every existing wrap-up and key each on result text only it produces — e.g. reads of files of its own, seeded in a workspace built for the test and launched with `launchTUIIn`, matched by path.

**Approach (assumed at the header base):** extend `cmd/apogee/testdata/stubllm/toolsautocollapse.yaml` with a new prompt whose script issues one call per Turn over three Turns (two different tool kinds, so the umbrella has two type rows and stays under `ui.tools-fold-over`), then a wrap-up Turn held on a new gate. Add the test to `cmd/apogee/e2e_toolsautocollapse_test.go`, reusing `heldRequests`, `umbrellaHeaderMatch`, `launchTUIIn`, `submit`, `waitIdle`, `quitCleanly`; register the gate's `stub.Release` in `t.Cleanup` after launch, as `TestE2EToolsAutoCollapseFoldsAGroupWhenTheNextTurnStarts` does. Waits follow `docs/design/test-drivers.md` §Waiting (no `WaitQuiet` for readiness; durations through `internal/testtime`).

**Files:** cmd/apogee/e2e_toolsautocollapse_test.go, cmd/apogee/testdata/stubllm/toolsautocollapse.yaml
**Read first:** cmd/apogee/testdata/stubllm/toolsautocollapse.yaml — the zebra/wings held wrap-ups, the tool_result wrap-ups; cmd/apogee/e2e_toolsautocollapse_test.go — heldRequests, TestE2EToolsAutoCollapseFoldsAGroupWhenTheNextTurnStarts;
cmd/apogee/e2e_support_test.go — launchTUIIn, e2eWorkspace; docs/design/test-drivers.md — §Matching, §Waiting

**Tests:** `go test -race -count=1 -run 'TestE2EToolsAutoCollapse' ./cmd/apogee/`

**Acceptance:**
- `go test -race -count=1 -run 'TestE2EToolsAutoCollapse' ./cmd/apogee/`
- `go vet ./cmd/apogee/`

**Commit:** `test(tui): a multi-Turn tool burst stays open until its follower`

## 3. Record the rule: ADR 0035 amendment and the layout and manual docs

Depends on item 1.

**What:**
**Goal:** ADR 0035 carries a dated amendment `adr-0035-amend-1123` stating the follower settle and the four ratified calls above, named on ADR 0035's Status line and in its reading-order list; `layout.md`, `docs/layout/tool-layout.md`, `docs/manual/configuration.md` and the `ToolsAutoCollapse` field comment in `internal/domain/uiprefs.go` state the new rule; no shipped doc still says a block folds when the next Turn starts.

**Regression guard.** The prose rule is every sentence that names the next Turn/step starting, or a boundary, as a reason a block settles or stays folded (e.g. `tool-layout.md` "next Turn of its own run has started", "its run's next Turn started", "the next Turn started or something after it"; `configuration.md` "the model has started its next step"); find them with `grep -rn -i "next Turn\|next step\|boundary" layout.md docs/layout docs/manual internal/domain internal/config/defaults .capcom/knowledge/glossary`, dropping the headless-budget hits (`headless.md`, `configuration.md`'s budget text, domain events/config/reaction).
The glossary Note `.capcom/knowledge/glossary/glossary-context/group-summary.md` ("settled from the moment the next Turn of its run starts, `adr-0035-amend-1120b`") is repointed to the follower rule citing `adr-0035-amend-1123`, and the shipped `internal/config/defaults/config.yaml` `tools-auto-collapse` comment ("the conversation moved past it or its Turn over") is kept aligned with the new `uiprefs.go` wording, as the 1120c docs pass did (CHANGELOG); `registry.go`'s Desc says only "settled" and is unchanged.

**Approach (assumed at the header base):** `capcom know add --id adr-0035-amend-1123 --parent adr-0035`, in the shape of `adr-0035-amend-1120b` (Why now / What this supersedes / the rule / "TUI view state only" closing). It supersedes `adr-0035-amend-1120b` (a) and (b)'s sticky Turn-boundary settle and `adr-0035-amend-1120c` (c)'s "the Turn boundary keeps it folded while the next Turn's call joins it"; the wait-on-user open and touched rules stand. Update the ADR 0035 Status line and reading-order list (`capcom know show adr-0035`). Prose rule: every doc sentence stating the Turn-boundary settle is rewritten — find them with `grep -rn -i "next Turn\|next step\|boundary" layout.md docs/layout docs/manual internal/domain internal/config/defaults .capcom/knowledge/glossary`, dropping the headless-budget hits (historical text — CHANGELOG, older ADR amendments, archived plans — stays as written). Run `capcom check` after the Note edits.

**Files:** .capcom/knowledge/decisions/adr-0035.md, .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1123.md, layout.md, docs/layout/tool-layout.md, docs/manual/configuration.md, internal/domain/uiprefs.go, internal/config/defaults/config.yaml, .capcom/knowledge/glossary/glossary-context/group-summary.md
**Read first:** .capcom/knowledge/decisions/adr-0035.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120b.md — (a), (b); .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120c.md — (c) touched group grows;
docs/layout/tool-layout.md — "Fold states and interaction" (1)/(2); layout.md — "Which fold holds an umbrella" paragraph; docs/manual/configuration.md — `ui.tools-auto-collapse` paragraph

**Tests:** `go test -count=1 ./internal/domain/`

**Acceptance:**
- `capcom know show adr-0035-amend-1123`
- `capcom check`
- `go test -count=1 ./internal/domain/`
- `grep -rn "next Turn starts\|next Turn of its run" layout.md docs/layout docs/manual internal/domain` prints nothing.

**Commit:** `docs(adr): a tool group folds on a follower, not on the next Turn's start`
