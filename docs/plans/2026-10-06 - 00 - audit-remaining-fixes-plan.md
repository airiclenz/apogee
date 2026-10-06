# Audit remaining fixes — 2026-10-05 code audit, second half

**Goal:** Close the 2026-10-05 audit's Medium findings left after the urgent-fixes plan, other than the three model-facing contract gaps of its action-order step 5. Each item is one defect fix with a test that fails on the pre-item tree.
**Date:** 2026-10-06
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 91103e58

**Sources:**
- `docs/reviews/code-audit-2026-10-05.md` (findings and suggested fixes)
- `docs/plans/archived/2026-10-05 - 00 - audit-urgent-fixes-plan.md` (the first half, done)

**Ratified design calls** (owner, 2026-10-06):
- **Tree-mutation floor:** diff a per-path content identity, never whole porcelain lines.
- **Markdown-fenced values:** string-typed params stay verbatim; a param whose tool-menu schema type is integer/number/boolean/array/object is JSON-decoded (reopened 2026-10-06); one leading and one trailing line break dropped.
- **Anthropic unparsable tool-call arguments:** encode that call's input as `{}`; the call and its result stay in history.
- **Duplicate workflow items:** de-duplicate at expansion, first occurrence kept, the dropped count reported.
- **MCP tool names:** sanitise to `^[a-zA-Z0-9_-]{1,64}$` and map the model-facing name back to the remote name.
- **Stale merge report:** delete `report.md` before spawning a merge child that is not resumed; the folder layout is unchanged.
- **Shell-write heredoc bodies:** scanned only when the leader is a shell interpreter (sh, bash, dash, zsh, ksh), tokenised and judged as a substitution body is; every other leader's heredoc stays payload. Supersedes ADR 0049's "a heredoc body is payload" for those leaders.

**Standing requirements:**
- skills: coding-standards
- Run tests narrowed to one package and, for `internal/tui`, one `-run` pattern.

**Out of scope:**
- Audit action-order step 5: circuit breaker semantics, `ErrMalformedToolCall` sentinel, custom-regex instruction example (separate grill session).
- `custom_regex.go` value coercion (`coerceArgs`), which belongs with the step-5 custom-regex work.
- Every finding the urgent-fixes plan already closed.

**Regression check (2026-10-06, 91103e58):** draft items 12–18 are now 13–19 (item 11 split; new item 12).
- 2: guard folded
- 3: guard folded
- 4: guard folded; supersedes the RedoRevert doc's caller-side asymmetry (internal/agent/agent.go)
- 5: guard folded; supersedes the "opened beside the launch" comments (internal/tui)
- 6: guard folded (decision); yields to `SubprocessResult.DenialStopped` (internal/subprocess/subprocess.go)
- 7: guard folded; yields to ADR 0056 D4
- 8: recast
- 9: recast; supersedes ADR 0049 "a heredoc body is payload"
- 10: guard folded; yields to ADR 0049 "Nothing is synthesised"
- 11: recast (split: schema-typed decode moved to item 12)
- 12: new, split from item 11
- 13: guard folded
- 14: guard folded; supersedes the non-object encode-error docs (internal/provider/wire_anthropic.go)
- 15: recast; yields to internal/agent/wire.go's "double-tell" rule
- 16: guard folded; yields to `bothListsRefusal` (internal/config/configmigrate.go)
- 17: guard folded
- 18: recast (pick `file:` dedupe added); guard folded; supersedes the ItemSpec doc and docs/manual/workflows.md `lines` row
- 19: guard folded
- round 2 — 8: recast (decision: Goal and Approach rewritten from the guard)
- round 2 — 9: recast (decision: Goal and Approach rewritten from the guard)
- round 2 — 11: recast (decision: JSON-valid values keep tryParseValue until item 12); supersedes the MarkdownFencedParser port doc and fenceClose doc (internal/processing/markdown_fenced.go)
- round 2 — 12: guard folded (decode unless the schema admits string; folded key lookup; replaces item 11's JSON coercion)
- round 2 — 15: guard folded (decision: round-trip vector); supersedes the formatMessage null-content doc (internal/provider/wire_openai.go)
- round 2 — 18: guard folded (verbatim entries, doc-sweep grep, re-run refusal in CHANGELOG)
- round 3 — 8: guard folded (decision: uniq/xxd option values and `--` stop, one --output emission, git diff --output rows, --output scan stops at `--`; uniq/xxd/tree stay in readLeaders, its doc rewritten)
- round 3 — 9: guard folded (decision: body scanned only as the interpreter's stdin script; per-host delimiter binding; doc-sweep grep)
- round 3 — 11: guard folded (decision: one nested-fence close rule; indented and glued closes; info-string scope, bare-opener limit)
- round 3 — 15: guard folded (decision: adds processing.RenderToolCall; fold keyed on the profile format; encode assertion in wire_openai_test.go)

## 1. A session payload with no conversation restores to one state — ✅ DONE (2026-10-06)

NOTES (2026-10-06): the test also covers `{"conversation":null,"turnIndex":2}`, because the Goal names a null key as well as an absent one; the now-unreachable `st.Conversation != nil` guards in restoreState, CutSession and checkRestoredStructure were left in place (out of scope, harmless).

**What:** Fixes audit Medium "A session payload with no `conversation` key half-restores".
**Goal:** `decodeState` yields a non-nil empty conversation for every payload whose `conversation` key is absent or null, so `restoreState` always swaps the conversation together with the counters.
**Approach (assumed at the header base):** In `internal/agent/state.go` `decodeState`, after unmarshalling, set `st.Conversation = domain.NewConversation(nil)` when it is nil, mirroring the zero-length-payload branch.
**Files:** internal/agent/state.go, internal/agent/state_test.go
**Read first:** internal/agent/state.go — decodeState, restoreState, CutSession, checkRestoredStructure; internal/agent/state_test.go — TestRestore_AnEmptyPayloadClearsTheLiveSession, newSnapshotAgent, cutFixtureSession;
internal/agent/restoresession_test.go — TestRestoreSession_RejectsCorruptPayloadUntouched
**Tests:** `TestDecodeStateMissingConversation` — payloads `{}` and `{"turnIndex":3}` restore over a non-empty conversation and leave it empty, with no error.
**Acceptance:** `go build ./internal/agent/ && go test -count=1 -run 'TestDecodeState' ./internal/agent/`
**Commit:** `fix(agent): restore an empty conversation when the payload carries none`

## 2. Scheduler.Add cannot race Close on the WaitGroup — ✅ DONE (2026-10-06)

NOTES (2026-10-06): tick's `wg.Add` (after its unlock, on the counted loop goroutine) left untouched as the item's regression guard binds; the Goal's "every `wg.Add`" wording is read through that guard.
NOTES (2026-10-06): TestSchedulerAddCloseRace does fail on the pre-item tree under -race (race reported on the WaitGroup), beyond the plan's "regression guard only" expectation.

**What:** Fixes audit Medium "`Scheduler.Add` can race `Close` on the WaitGroup".
**Regression guard.** The Goal binds `Scheduler.Add` only: Add takes its WaitGroup slot while `s.mu` is held and `closed` is false; `tick`'s `wg.Add` after its unlock (safe on the counted loop goroutine) stays untouched. "No `EventCreated` after `Close` returns" is the observable half. No seam sits between Add's unlock and its `wg.Add`, so the race test guards against regressions and is never claimed red on the pre-item tree, unless a test-only hook between unlock and `wg.Add` makes it so.
**Goal:** Every `wg.Add` in `internal/schedule` happens while `s.mu` is held and `closed` is false, and no loop goroutine starts after `Close` returned.
**Approach (assumed at the header base):** In `Scheduler.Add`, move `s.wg.Add(1)` inside the `!closed` branch before `s.mu.Unlock()`; the goroutine start may stay after the unlock.
**Files:** internal/schedule/schedule.go, internal/schedule/schedule_test.go
**Read first:** internal/schedule/schedule.go — Scheduler.Add, Scheduler.Close, Scheduler.tick, Scheduler.loop; internal/schedule/schedule_test.go — start, TestCloseIsIdempotentAndJoinsEveryGoroutine;
internal/schedule/harness_test.go — newFakeClock, newRecorder
**Tests:** `TestSchedulerAddCloseRace` — concurrent `Add`/`Close` loop; after `Close` returns, no `EventCreated` arrives. A regression guard, not a red-before test.
**Acceptance:** `go build ./internal/schedule/ && go test -race -count=1 -run 'TestSchedulerAddCloseRace' ./internal/schedule/`
**Commit:** `fix(schedule): take the WaitGroup slot under the lock in Add`

## 3. stripThinking searches and slices the same string — ✅ DONE (2026-10-06)

**What:** Fixes audit Medium "`stripThinking` slices with an index taken from a lower-cased copy".
**Regression guard.** The new cases live in a new `TestStripThinking`, a table over `stripThinking` directly, so the Acceptance run matches them. The Goal reads "inside a leading block, before its close tag": only a leading block is stripped (title.go doc), so a text whose leading `İ` fails the `<think>` prefix is returned whole, as today. Go's `ToLower` shrinks `İ` to 1 byte and grows `Ⱥ` to 3; the table covers both directions.
**Goal:** `stripThinking` never panics and never returns a garbled title for input whose lower-casing changes byte length (`İ`, `Ⱥ`) before or inside the thinking block.
**Approach (assumed at the header base):** In `internal/title/title.go` `stripThinking`, find `</think>` case-insensitively without changing offsets (an ASCII-only fold over `trimmed` itself, or `regexp (?i)`), and slice `trimmed` with that index.
**Files:** internal/title/title.go, internal/title/title_test.go
**Read first:** internal/title/title.go — stripThinking, thinkOpen, thinkClose, SanitizeTo; internal/title/title_test.go — TestSanitize
**Tests:** `TestStripThinking` — table cases with `İ`/`Ⱥ` runs inside a leading block before `</THINK>` and `</think>`; the title is the text after the tag.
**Acceptance:** `go build ./internal/title/ && go test -count=1 -run 'TestStripThinking' ./internal/title/`
**Commit:** `fix(title): match the thinking close tag without shifting offsets`

## 4. Undo's staleness guard runs under the journal lock — ✅ DONE (2026-10-06)

NOTES (2026-10-06): `Journal.Revert(generation)` checks emptiness before the stamp, as `takeRedoTop` does, so `Agent.UndoRevert` on an empty journal now answers `undo.ErrNothingToUndo` whatever the stamp (it answered `ErrStaleGeneration` for a mismatched stamp before); `applyUndoVerb` and the TUI already answered "nothing to undo" first.
NOTES (2026-10-06): the stale branch of `applyUndoVerb` moved into a new helper `undoVerbMoved` (cmd/apogee/undo.go), which re-Previews after the refusal and prints `undoVerbMovedLead` plus the fresh preview; the error returned is the journal's own `ErrStaleGeneration`-wrapping refusal, same message format as before.
NOTES (2026-10-06): test callers were updated mechanically to `X.Revert(X.Generation())`.

**What:** Fixes audit Medium "Undo's staleness guard is checked by each caller, outside the journal lock".
**Regression guard.** In `applyUndoVerb`, an `errors.Is(err, undo.ErrStaleGeneration)` from `journal.Revert(generation)` re-Previews and prints `undoVerbMovedLead` plus the fresh preview, as today. Every `.Revert()` test caller is updated, the five outside internal/undo/journal_test.go included. This supersedes the RedoRevert doc's caller-side asymmetry (internal/agent/agent.go, "the one shape it does not share with [Agent.UndoRevert]"): every comment that puts the undo stamp compare in a caller or contrasts Redo with Revert on it is rewritten — `grep -rn -i 'generation' internal/undo/journal.go internal/undo/redo.go internal/agent/agent.go cmd/apogee/undo.go`.
**Goal:** `Journal.Revert` takes the generation the caller previewed and refuses under the journal's own lock when it no longer matches, exactly as `Redo(generation)` does; no caller compares `Generation()` before calling `Revert`.
**Approach (assumed at the header base):** Give `Revert` a `generation` parameter compared in `takeTop` as `takeRedoTop` compares it, returning the same stale error `Redo` returns. Update both production callers: `internal/agent/agent.go` (the method wrapping `a.journal.Revert()`) and `cmd/apogee/undo.go`; delete their separate generation compares. Update every test caller (grep `\.Revert(`).
**Files:** internal/undo/journal.go, internal/undo/journal_test.go, internal/agent/agent.go, cmd/apogee/undo.go, internal/undo/redo.go, internal/undo/snapshot_test.go, internal/undo/persist_test.go, internal/agent/undo_group_test.go, internal/tools/undo_journal_test.go, internal/snapshot/journal_test.go
**Read first:** internal/undo/journal.go — Journal.Revert, takeTop; internal/undo/redo.go — takeRedoTop; internal/agent/agent.go — Agent.UndoRevert, Agent.RedoRevert;
cmd/apogee/undo.go — applyUndoVerb, undoVerbMovedLead; cmd/apogee/undo_test.go — TestUndoVerbRefusesAStaleGeneration
**Tests:** `TestRevertStaleGeneration` — a `Record` between preview and `Revert(gen)` is refused and the journal is unchanged; `TestUndoVerbRefusesAStaleGeneration` stays green (the moved-lead line and fresh preview still print).
**Acceptance:** `go build ./... && go vet ./internal/undo/ ./internal/agent/ ./internal/tools/ ./internal/snapshot/ ./cmd/apogee/ && go test -count=1 -run 'Revert|TestUndoVerb' ./internal/undo/ ./internal/agent/ ./cmd/apogee/`
**Commit:** `fix(undo): check the revert generation under the journal lock`

## 5. A typed message waits while a session load or /bg launch holds — ✅ DONE (2026-10-06)

NOTES (2026-10-06): consequential edit — internal/tui/interject.go: made necessary by the Goal's "flush opener". flushInterjections, the flush that runs automatically when an Exchange completes, asks loadHoldNote too. While a load or launch holds, the staged rows stay held for Enter. No current path reaches it during a hold. The item's Files list does not name interject.go.
NOTES (2026-10-06): /continue and /compact are refused inside runContinue/runCompact, as the regression guard asks. A refused /continue or /compact line therefore leaves the input box empty but stays in recall (↑), the way the existing actuation and upstream gates in runCommand already behave. Only the plain-message and held-queue send paths keep the typed line in the box. Under a /bg launch the two verbs are still queued (commandRunnable), and holdsTakingAgent is left as it was.
NOTES (2026-10-06): resumeLoaded, when a record lands while m.busy(), now adds a note (resumeBusyNote) as well as skipping the restore, so the dropped load is not silent. Added TestResumeLoadedWhileBusy, which the plan's Tests line does not list.
NOTES (2026-10-06): The rewritten comments include two more lines than the ones the plan names, because they also matched the plan's `beside the launch` grep: the commandrun.go runBg doc and the TestBgLaunchStashesARebindUntilItLands doc in command_test.go. The grep now has no hits. The forced-overlap tests (TestBgLaunchKeepsARebindStashedPastAnExchangeEnd, TestReleaseEngine_FoldBgStarted "under a worker") are kept, with comments saying they pin finishWorker's and releaseEngine's own deferral.

**What:** Fixes audit Medium "A message typed during a session load or `/bg` launch starts a second goroutine on the Agent".
**Regression guard.** `submit`, `runContinue` and `runCompact` gate on an explicit `holdBgLaunch|holdSessionLoad` check and start no worker (/compact opens no Exchange); `holdsTakingAgent` is left as is — widening it instead must update TestEngineHolds_Questions' "session loading" row and drain `runDeferredCommands` in the `sessionLoadedMsg` fold. The note names its hold (a session load vs. a background launch). This supersedes the comments recording a message opening an Exchange beside a /bg launch (commandrun.go foldBgStarted doc, command_test.go, engineholds_test.go): each is rewritten — `grep -rn 'beside the launch\|opened meanwhile' internal/tui`.
**Goal:** While `holdBgLaunch` or `holdSessionLoad` is active, the plain-message send path and the flush, continue and compact openers start no Exchange, keep the typed line in the input, and show a one-line note that the session is still loading; `resumeLoaded` does nothing while `m.busy()`.
**Approach (assumed at the header base):** In `internal/tui/model.go` `submit` (and the openers it shares a guard with), consult the same hold set commands consult before starting a worker. In `internal/tui/sessions.go` `resumeLoaded`, return early when `m.busy()`.
**Files:** internal/tui/model.go, internal/tui/sessions.go, internal/tui/sessions_test.go, internal/tui/commandrun.go, internal/tui/command_test.go, internal/tui/engineholds_test.go
**Read first:** internal/tui/model.go — Model.submit; internal/tui/engineholds.go — holdsTakingAgent, engineHolds.commandRunnable; internal/tui/commandrun.go — runContinue, runCompact, foldBgStarted;
internal/tui/sessions.go — acceptBrowser, resumeLoaded
**Tests:** driven tests: `TestSendDuringSessionLoad` sends a message between `acceptBrowser` and `sessionLoadedMsg` — no Exchange starts, the line stays in the input, the session-load note shows; `TestSendDuringBgLaunch` does the same under a /bg launch, the note naming the launch; `TestContinueDuringSessionLoad` — /continue during a load starts no worker.
**Acceptance:** `go build ./internal/tui/ && go test -count=1 -run 'TestSendDuring|TestContinueDuring' ./internal/tui/`
**Commit:** `fix(tui): hold a typed message while a session load or /bg launch runs`

## 6. Subprocess start and copy failures reach the result — ✅ DONE (2026-10-06)

NOTES (2026-10-06): The start-failure cause line also names the working directory when the spec sets one. exec blames a missing directory on the program ("fork/exec <program>: no such file or directory"), so without the directory the model would be told the wrong thing. Copy failures and other post-start failures get their own wording (runFailureLine).
NOTES (2026-10-06): TestRunSubprocessLateCancelKeepsACleanExit runs on Linux only (it skips elsewhere). It reads /proc to hold Contain until the child is a zombie, and its teardown constructor stubs cmd.Cancel so the run's deadline is counted as delivered after the clean exit. This test passes on the pre-item tree too, by design: it pins the guard. The start-failure and copy-failure tests fail on the pre-item tree.

**What:** Fixes audit Medium "Run errors from subprocesses are swallowed, so a start failure looks like a signal kill".
**Regression guard.** Adopt the reviewer's guard as binding — leave runErr untouched (no forced -1, no cause line) whenever runCtx.Err() != nil (timeout and denial-watch paths, already reported by TimedOut/DenialStopped); the new rule applies only to start failures and output-copy errors on an uncancelled run; the item yields to the DenialStopped contract at subprocess.go (a run that finished cleanly keeps its success result) and adds a test for a cancel landing after a clean exit keeping ExitCode 0
**Goal:** A run whose `cmd.Run`/`Wait` error is neither an `*exec.ExitError` nor `exec.ErrWaitDelay` yields a result with a non-zero exit code and the error's text in the diagnostics the model sees; a child exiting 0 after an output copy error is reported non-zero with the copy error named.
**Approach (assumed at the header base):** In `internal/subprocess/subprocess.go` `run`, keep `runErr` and append a one-line cause to the result's diagnostics; force the exit code to `-1` when it reads 0. Check what `RunSubprocessTo`'s doc promises and make the code match it.
**Files:** internal/subprocess/subprocess.go, internal/subprocess/subprocess_test.go
**Read first:** internal/subprocess/subprocess.go — run, exitCodeOf, SubprocessResult.DenialStopped, RunSubprocessTo; internal/platform/teardown_unix.go — NewProcessTeardown;
internal/tools/terminal.go — subprocessToolResult; internal/subprocess/teardown_test.go — installFakeTeardown; internal/subprocess/subprocess_test.go — TestRunSubprocessReportsAWedgedDrain
**Tests:** a missing program and a bad working directory each yield a non-zero exit code plus the cause text; a failing writer with a child exiting 0 yields a non-zero exit code; a ctx cancel landing after a child already exited 0 keeps ExitCode 0 and adds no cause line.
**Acceptance:** `go build ./internal/subprocess/ && go test -count=1 -run 'TestRun' ./internal/subprocess/`
**Commit:** `fix(subprocess): surface start and copy failures on the result`

## 7. The tree-mutation floor names files rewritten while already dirty — ✅ DONE (2026-10-06)

NOTES (2026-10-06): the probe now runs `rev-parse --is-inside-work-tree --show-toplevel` (still one git run per Agent) because porcelain paths are relative to the repository top, not to a workspace root below it; the funnel test pins the new probe argv as well as `status --porcelain -uall -z`.
NOTES (2026-10-06): status listing is NUL-separated (`-z`) so the path→file mapping survives quoted names; a rename/copy record's source field is consumed. Status runs through `gitexec.Host.RunTo` into a sink that refuses output past `subprocess.MaxSubprocessOutputBytes` (overrun → check skipped); scriptedGit in the test gained a SpawnTo.
NOTES (2026-10-06): bounds — per-file hash cap 2 MiB, whole-snapshot budget 32 MiB, deadline one `treeSnapshotTimeout` per snapshot (status run included); past the cap/budget a path is not hashed and compares by its Lstat identity (type|size|mtime) rather than abandoning the snapshot, and identities compare by digest only when both sides hashed. Regular files open via `security.SafeOpen` (O_NONBLOCK, fenced to the repo top); symlinks identify by target string; directories (nested repos) by type only.
NOTES (2026-10-06): consequential edit — internal/agent/dispatch.go: made necessary by beforeCall returning a treeSnapshot instead of a string (the `preTree` zero-value declaration in executeTool).
NOTES (2026-10-06): consequential edit — docs/adr/0056-terminal-fail-fast-and-session-scratch.md: made necessary by D4 describing the snapshot as `git status --porcelain` text; appended an "Amended 2026-10-06" sentence.

**What:** Fixes audit Medium "Tree-mutation floor misses files that were already dirty". Ratified call: per-path content identity.
**Regression guard.** The item updates both pinned tests: `TestTreeSnapshot_GitRunsThroughTheFunnel`'s argv pin moves to the new argv, and `TestTreeSnapshot_DiffHelpers` is rewritten against the new diff function (or kept on a retained line helper). Output past `subprocess.MaxSubprocessOutputBytes` is detected and the check skipped silently (e.g. `RunTo` into a bounded writer that errors over the cap), or plain `--porcelain` stays and only new or changed untracked-directory lines expand. Hashing Lstats each path, hashes only regular files (a symlink hashes its target string), and holds a per-file byte cap and a whole-snapshot budget inside `treeSnapshotTimeout`, skipping silently past it; the Goal's "every path" means every path within that bound. The item yields to ADR 0056 D4 (docs/adr/0056-terminal-fail-fast-and-session-scratch.md, "must never break or slow a call") and treesnapshot.go's robustness contract: the bounds keep both.
**Goal:** The floor reports every workspace path whose content changed across a shell command: a path already modified before the command, and a new file inside an already-untracked directory, included.
**Approach (assumed at the header base):** In `internal/agent/treesnapshot.go`, snapshot `git status --porcelain -uall` plus a content hash per dirty or untracked path (bounded the way the current snapshot is bounded), and diff on path → (status, hash). Keep the output wording the floor already emits.
**Files:** internal/agent/treesnapshot.go, internal/agent/treesnapshot_test.go
**Read first:** internal/agent/treesnapshot.go — treeSnapshotter.beforeCall, mutationWarning, porcelainDiffPaths; internal/agent/dispatch.go — executeTool; internal/gitexec/gitexec.go — Host.Run, Host.RunTo;
internal/agent/treesnapshot_test.go — TestTreeSnapshot_GitRunsThroughTheFunnel, TestTreeSnapshot_DiffHelpers
**Tests:** a pre-modified tracked file rewritten by the command is reported; a new file in an untracked dir is reported; an untouched dirty file is not; an untracked symlink to `/dev/zero` does not stall the call; `TestTreeSnapshot_GitRunsThroughTheFunnel` and `TestTreeSnapshot_DiffHelpers` are updated to the new argv and diff function.
**Acceptance:** `go build ./internal/agent/ && go test -count=1 -run 'TreeSnapshot' ./internal/agent/`
**Commit:** `fix(agent): diff tree snapshots on per-path content identity`

## 8. Shell-write guard: writing leaders and output options — ✅ DONE (2026-10-06)

NOTES (2026-10-06): consequential edit — internal/security/doc.go: made necessary by read leaders now contributing an output option's or output verb's file (the package doc said the view drops what a read leader names)
NOTES (2026-10-06): consequential edit — docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md: made necessary by read leaders now contributing an output option's or output verb's file (the ADR said a read leader contributes nothing); sentence amended, dated
NOTES (2026-10-06): the hard-refuse half of the Tests line is pinned by a new TestShellWriteViewRefusesOutputWrites in shellwrites_test.go (guard-level, via DefaultDangerousActionGuard) rather than by rows in dangerous_test.go, which the item's Files line does not list
NOTES (2026-10-06): xxd value options also accept their long spellings (-cols, -groupsize, -len, -offset, -seek, -name), which xxd's own parser treats as the same options; uniq short bundles ending in f/s/w consume the next word as getopt does
NOTES (2026-10-06): pre-existing debt, not changed: sed's `e` command and `s///e` flag execute a shell command yet sed without -i stays a read leader; GNU long-option abbreviations (`--out=`) and tree bundles (`-ao file`) are not read as output options; a `-f` sed script file is not seen

**What:** Recast at the regression check (2026-10-06). Fixes part of audit Medium "Shell-write guard misses writes to `.git/hooks`": ordinary verbs judged writeless.
**Regression guard.** uniq/xxd/tree stay in `readLeaders`, with cases in `operandTargets` ahead of `readLeaders[leader]` (like `ddTargets`) and the map doc's "on any option" claim rewritten to name them. uniq/xxd contribute their 2nd positional operand, skipping the value word of xxd -c/-g/-l/-o/-s/-n and uniq -f/-s/-w/--skip-fields/--skip-chars/--check-chars, option parsing stopped at `--`; tree contributes only its -o value. A bare `-o <word>` writes only for tree. The generic `--output=`/`--output <word>` value is read only on branches that do not return `valueOperands` today (read leaders, sed without -i, find without a writing predicate, `gitTargets`' read-verb branch, dd, the new handlers), so `frobnicate --output=.git/config` is emitted once; that scan stops at `--` and skips echo/printf and a grep/rg `-e` value. A sed target is only the filename after a w/W command or an s///w flag, never sed's file operands.
**Goal:** The 2nd positional operand of `uniq` and `xxd` (`uniq in out`, `xxd -r in out`), `tree`'s `-o` value, the filename after a `sed` `w`/`W` command or an `s///w` flag, and every leader's `--output=file`/`--output file` (`git diff --output=…` included) are write targets, so each aimed at `.git/hooks/*` or `.git/config` is hard-refused as `write-git-control-plane`.
**Approach (assumed at the header base):** In `internal/security/shellwrites.go`: the guard's `operandTargets` handlers, a generic `--output` reader on the branches it names, the `sed` write target parsed from its script.
**Files:** internal/security/shellwrites.go, internal/security/shellwrites_test.go
**Read first:** internal/security/shellwrites.go — operandTargets, readLeaders, ddTargets, gitTargets, hasInPlaceFlag, valueOperands; internal/security/shellwrites_test.go — TestWriteTargetsOf;
internal/security/dangerous_test.go — TestShellWriteViewJudgesWhatTheCommandWrites
**Tests:** an adversarial table with each write spelling above against `.git/hooks/pre-commit`, the `--output` rows built on `git diff --output=.git/hooks/pre-commit`, `git log --output .git/hooks/pre-commit` and one read leader; still allowed: `uniq in`, `tree`, `tree .git/hooks`, `xxd .git/config`, `uniq .git/config`, `xxd -l 64 .git/config`, `uniq -f 1 .git/config`, `ls -o .git/hooks`, `grep -o x .git/config`, `grep -rn -- --output .git/hooks`, `sed -n '/worktree/p' .git/config`; the `frobnicate --output=.git/config` row keeps its single `.git/config` want.
**Acceptance:** `go build ./internal/security/ && go test -count=1 -run 'ShellWrite|WriteTargets' ./internal/security/`
**Commit:** `fix(security): treat output-writing verbs and options as shell writes`

## 9. Shell-write guard: heredoc bodies fed to an interpreter — ✅ DONE (2026-10-06)

NOTES (2026-10-06): the wrapper-and-assignment stripping operandTargets did inline is extracted into stripWrappers so readsStdinScript strips the same way; operandTargets' behaviour is unchanged
NOTES (2026-10-06): the interpreter's stdin test also lets `-o`/`+o`/`-O`/`+O` and `--rcfile`/`--init-file`/`--emulate` consume their value word, and treats a lone `-` or `--` as the end of options (a word after it is a script file), so `bash -o pipefail <<EOF` is still read as a stdin script
NOTES (2026-10-06): the guard-level allowed/refused rows of the Tests line are pinned by a new TestShellWriteViewReadsInterpreterHeredocs in shellwrites_test.go; the per-delimiter binding and the wrapped/`-s` forms by new TestWriteTargetsOf rows
NOTES (2026-10-06): pre-existing debt, not changed: an unquoted-delimiter heredoc body's `$(…)` runs in the OUTER shell for any host, yet a non-interpreter body is still skipped whole; a heredoc on a brace group or subshell (`{ bash; } <<EOF`) binds to no interpreter and stays payload; `cat <<EOF | bash` stays payload by the ratified host-leader rule

**What:** Recast at the regression check (2026-10-06). Fixes part of audit Medium "Shell-write guard misses writes to `.git/hooks`": heredoc bodies skipped as payload. Depends on item 8.
**Regression guard.** Ratified design call (owner, 2026-10-06): a heredoc body is scanned only when its host command's leader — after wrapper and assignment stripping and `path.Base`, as in `operandTargets` — is sh/bash/dash/zsh/ksh AND the body is the interpreter's script on stdin (no `-c`, no script-file operand: options only, or `-s`). The body goes through `splitSimpleCommands` and each resulting command is judged by its redirects and `operandTargets`, as `nest` does, so reads in it stay allowed. Bind each delimiter to its own host command: `tk.heredocs` holds bare delimiters and `endCommand` has already flushed the host when `skipHeredocBodies` runs. Every other heredoc stays payload. Supersedes ADR 0049's "a heredoc body is payload" for those leaders: amend the ADR with a dated note and rewrite every hit of `grep -rniE 'here-?doc' internal/security docs/adr docs/manual` that calls a heredoc body payload without the interpreter exception (the `writeTargetsOf` doc, ADR 0049 "The shell write view"; the test row "a heredoc body is payload" stays true for its cat leader).
**Goal:** A heredoc body that is a shell interpreter's (`sh`, `bash`, `dash`, `zsh`, `ksh`) script on stdin is scanned as shell for write targets, so `bash <<EOF\nchmod +x .git/hooks/pre-commit\nEOF` and `sh <<EOF\necho x > .git/hooks/pre-commit\nEOF` are refused.
**Approach (assumed at the header base):** In `internal/security/shellwrites.go`, record with each delimiter whether its host is an interpreter reading stdin; `skipHeredocBodies` judges such a body as the guard states.
**Files:** internal/security/shellwrites.go, internal/security/shellwrites_test.go, docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md
**Read first:** internal/security/shellwrites.go — shellTokenizer.scan, endWord, endCommand, skipHeredocBodies, nest, operandTargets; internal/security/shellwrites_test.go — TestWriteTargetsOf;
docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md — "The shell write view"
**Tests:** the two heredoc spellings above refused; allowed: `bash <<EOF\ncat .git/config\nEOF`, `cat <<EOF > notes.md` with a body naming `.git/hooks`, `git commit -F - <<'EOF'` and `tee -a notes.md <<'EOF'` with bodies naming `.git/hooks`, `bash ./install.sh <<EOF\n.git/hooks\nEOF`, `bash -c 'cat > notes.md' <<'EOF'\nsee .git/hooks/x\nEOF`.
**Acceptance:** `go build ./internal/security/ && go test -count=1 -run 'ShellWrite|WriteTargets' ./internal/security/`
**Commit:** `fix(security): scan interpreter heredoc bodies for shell writes`

## 10. Shell-write guard: cd joins later relative targets — ✅ DONE (2026-10-06)

NOTES (2026-10-06): a `cd` in a pipeline stage (`cd .git | …`) or a background job (`cd .git & …`) is still treated as moving the later commands — the shell runs those in a subshell, so the view over-joins there (stricter, never laxer); left as is.
NOTES (2026-10-06): a later operand only partly built from a substitution (`rm x$(y)`) keeps its literal remainder and is joined; only the `cd` operand itself carries the per-word substitution mark.

**What:** Fixes the last part of audit Medium "Shell-write guard misses writes to `.git/hooks`". Depends on item 9.
**Regression guard.** The existing TestWriteTargetsOf row `cd .git/hooks && rm -rf pre-commit` (want ".git/hooks pre-commit") is updated to the prefixed target. A tracked `cd` is dropped at the close of a `( … )` subshell and never carried out of a `$( … )`/backtick body, so `(cd .git && ls); echo x > config.yaml` and `g=$(cd .git && pwd); echo x > config.yaml` stay allowed. The item yields to ADR 0049's "Nothing is synthesised" (docs/adr/0049-…md, "The shell write view"): the prefix only joins a literal `cd` operand with a later literal operand the line names; no path is inferred from a verb.
**Goal:** Within one command line, `cd .git && echo x > hooks/pre-commit` (and `;`/`||` joins, `cd ./.git/hooks && …`) resolves the later relative operand against the tracked `cd` target and is refused.
**Approach (assumed at the header base):** In `internal/security/shellwrites.go`, track a literal `cd <dir>` across the segments of one command and prefix later relative write targets with it; a non-literal `cd` target leaves later operands unprefixed.
**Files:** internal/security/shellwrites.go, internal/security/shellwrites_test.go
**Read first:** internal/security/shellwrites.go — writeTargetsOf, splitSimpleCommands, shellTokenizer.scan, nest, operandTargets; internal/security/shellwrites_test.go — TestWriteTargetsOf;
internal/security/dangerous.go — Inspect, shellWriteText
**Tests:** the spellings above refused; `cd docs && echo x > notes.md`, `(cd .git && ls); echo x > config.yaml` and `g=$(cd .git && pwd); echo x > config.yaml` allowed; the `cd .git/hooks && rm -rf pre-commit` row's want updated to the prefixed form.
**Acceptance:** `go build ./internal/security/ && go test -count=1 -run 'ShellWrite|WriteTargets' ./internal/security/`
**Commit:** `fix(security): resolve relative shell writes against a preceding cd`

## 11. Markdown-fenced tool calls keep argument values verbatim — ✅ DONE (2026-10-06)

**What:** Recast at the regression check (2026-10-06). Fixes audit Medium "Markdown-fenced tool-call format truncates and mangles argument values". Ratified call: verbatim strings.
**Regression guard.** Processing-only; schema-typed decode is item 12's. Close rule: walking the lines after `blockStart`, a line whose TrimSpace is ```<info> opens a nested fence, one whose TrimSpace is exactly ``` closes the innermost open nested fence, else closes the tool block (a doubled close leaves its stray ``` in the stripped text, as today); with no such close line, today's mid-line ``` close (`src/main.ts```) is the fallback. The block's lines lose the opener line's indentation (CommonMark) and the block-level TrimSpace in strictParse/fallbackParse is dropped (trim only ahead of the name line / first marker). Value coercion: a value whose trimmed form is valid JSON keeps `tryParseValue`; every other value is the verbatim string less one leading and one trailing line break (no TrimSpace). A nested fence opened bare (```\nx\n```) cannot be told from trailing prose: the documented limit. Supersedes the `MarkdownFencedParser` type doc ("faithful port ... identical") and the `fenceClose` doc (internal/processing/markdown_fenced.go); both rewritten.
**Goal:** A markdown-fenced call whose argument holds its own fence opened with an info string (```bash) keeps the whole value, coerced as the guard states, and typed params (`read_file` `start_line`, `ask_user` `choices`) keep decoding after item 11 alone.
**Approach (assumed at the header base):** In `internal/processing/markdown_fenced.go`, `fenceClose` (so `strictParse`/`strictStrip`) takes the guard's close rule and the block its de-indent; `parseBlock` encodes non-JSON values with a new verbatim-string helper in `args.go`. `tryParseValue` stays for `custom_regex.go` (out of scope).
**Files:** internal/processing/markdown_fenced.go, internal/processing/args.go, internal/processing/markdown_fenced_test.go
**Read first:** internal/processing/markdown_fenced.go — fenceClose, strictParse, strictStrip, fallbackParse, parseBlock; internal/processing/args.go — tryParseValue;
internal/processing/markdown_fenced_test.go — TestMarkdownFenced_PortedOracleVectors; internal/agent/profile_test.go — TestProfile_MarkdownFencedCallParsedAndStripped
**Tests:** a `write_file` README holding a ```bash block; prose after the tool block holding its own ```bash block leaves the value unchanged and stays in the stripped text; the "double opening fence" vector asserts exact "Hello world"; an indented (list-item) block and a glued close (`src/main.ts```) yield the unindented value; a bare-opened nested fence pins the limit; a last argument keeps its trailing text less one line break; a `package.json` value keeps today's `tryParseValue` result; a Python `new_string` keeps its indentation; `TestMarkdownFenced_TypedParamsKeepDecoding` — fenced `read_file` `start_line` / `ask_user` `choices` still yield a number and an array.
**Acceptance:** `go build ./internal/processing/ && go test -count=1 -run 'MarkdownFenced' ./internal/processing/`
**Commit:** `fix(processing): keep markdown-fenced argument values verbatim`

## 12. Text-format tool-call arguments decode against the tool menu's schema types — ✅ DONE (2026-10-06)

NOTES (2026-10-06): re-derived from "item 11's JSON branch lives in parseBlock": the branch is in internal/processing/args.go verbatimValue (parseBlock only calls it), so verbatimValue now always returns the verbatim string and internal/processing/markdown_fenced.go is unchanged.
NOTES (2026-10-06): item 11's TestMarkdownFenced_TypedParamsKeepDecoding is renamed TestMarkdownFenced_TypedParamsStayVerbatim, and TestMarkdownFenced_JSONValueKeepsTryParseValue is renamed TestMarkdownFenced_JSONValueStaysVerbatim. Both now expect the verbatim string, and the old names would describe behaviour this item removes.
NOTES (2026-10-06): the decode runs for every recovered text-format call, custom-regex included, as the Goal says. custom_regex.go's own coercion (tryParseValue for the "raw" fallback) is untouched (out of scope). A property schema that names no type, does not parse, or matches two properties under the folded key counts as admitting a string, so its value stays verbatim. An object with duplicate keys is re-encoded with the duplicates kept, so dispatch's repeated-key refusal still sees them.

**What:** Split from item 11 at the regression check (2026-10-06): the typed half of audit Medium "Markdown-fenced tool-call format truncates and mangles argument values". Ratified call: schema-typed decode. Depends on item 11.
**Regression guard.** Decode when the property schema does not ADMIT string (its `type`, a type list, or `anyOf`/`oneOf` members — MCP schemas pass verbatim, `normaliseSchema`, so FastMCP's `{"anyOf":[{"type":"integer"},{"type":"null"}]}` must decode); keep verbatim only where string is admitted. Look up the schema property by `domain.FoldArgumentKey(name)` against folded property names, so `START_LINE` reaches `start_line` as the tool's case-insensitive decode does. The item replaces item 11's JSON coercion in `parseBlock`: every fenced value becomes the verbatim string, and string-typed params stay verbatim even when they look like JSON.
**Goal:** A recovered text-format call's argument whose tool-menu schema type is integer/number/boolean/array/object reaches the tool JSON-decoded; a string-typed argument stays the verbatim string item 11 produced, `123` included.
**Approach (assumed at the header base):** In `internal/processing/markdown_fenced.go` `parseBlock`, drop item 11's JSON branch so every fenced value is the verbatim string. In `internal/agent/loop.go` `assembleResponse`, which holds `view.Tools()`, decode each JSON-string argument of a recovered text-format call whose schema property does not admit string, with a helper in `internal/processing/args.go`; a value that does not decode stays the string so the tool's own decode error names it. Params whose schema admits string, already-typed values and native calls are untouched.
**Files:** internal/agent/loop.go, internal/agent/loop_test.go, internal/processing/args.go, internal/processing/markdown_fenced.go, internal/processing/markdown_fenced_test.go
**Read first:** internal/agent/loop.go — assembleResponse; internal/domain/hookview.go — loopView.Tools; internal/domain/tools.go — FoldArgumentKey; internal/tools/tools.go — decodeArgs, decodeToolArgs;
internal/mcp/tool.go — normaliseSchema; internal/tools/read_file.go, list_dir.go, ask_user.go — schema literals; internal/agent/profile_test.go — newProfileAgent
**Tests:** `TestAssembleResponseDecodesSchemaTypedArgs` — a fenced `read_file` with `start_line`/`max_lines`, `list_dir` with `recursive`, and `ask_user` with `choices` decode to number, boolean and array; a string param holding `123` stays a string; a string `content` holding a `package.json` stays a string; an `anyOf` integer/null param decodes to a number; a `START_LINE` spelling decodes through the folded key. Item 11's `TestMarkdownFenced_TypedParamsKeepDecoding` is rewritten to the verbatim-string want.
**Acceptance:** `go build ./internal/agent/ ./internal/processing/ && go test -count=1 -run 'SchemaTyped' ./internal/agent/ && go test -count=1 -run 'MarkdownFenced' ./internal/processing/`
**Commit:** `fix(agent): decode schema-typed text-format arguments against the tool menu`

## 13. MCP tool names fit the provider pattern

**What:** Fixes audit Medium "MCP tool names are passed to the model without checking the provider name pattern". Ratified call: sanitise and map back.
**Regression guard.** A short stable hash of the full qualified name is appended whenever sanitising changed the name (a per-name rule, no cross-server state), not only past 64 characters, so `files.read`/`files_read` on one server and aliases `a.b`/`a_b` across servers stay distinct; the Approach states this rule. docs/design/mcp-client.md's "Tool naming" bullet states the sanitise + hash rule and that dispatch keeps the remote name.
**Goal:** Every MCP tool name offered to the model matches `^[a-zA-Z0-9_-]{1,64}$`, distinct remote tools never share a model-facing name, and a call by the model-facing name reaches the original remote name.
**Approach (assumed at the header base):** In `internal/mcp`, after `qualifyToolName`, replace characters outside the pattern with `_`; when longer than 64, truncate and append a short stable hash of the full qualified name. `listServerTools` keeps the remote name on the tool so dispatch sends it.
**Files:** internal/mcp/tool.go, internal/mcp/client.go, internal/mcp/tool_test.go, docs/design/mcp-client.md
**Read first:** internal/mcp/tool.go — qualifyToolName, newServerTool, serverTool.Execute; internal/mcp/client.go — listServerTools, validateServers; internal/mcp/tool_test.go — TestQualifyToolName;
cmd/apogee/wire_tools.go — registryWithMCP; docs/design/mcp-client.md — Tool naming
**Tests:** `files.read` and an 80-character qualified name each sanitise to a valid name and dispatch with the remote name; two names that sanitise alike stay distinct; a two-server case (aliases `a.b` / `a_b`, same tool) in a `…ToolName…` test gets distinct names.
**Acceptance:** `go build ./internal/mcp/ && go test -count=1 -run 'ToolName' ./internal/mcp/`
**Commit:** `fix(mcp): sanitise qualified tool names to the provider pattern`

## 14. Anthropic wire encodes unparsable tool-call arguments as {}

**What:** Fixes the Anthropic half of audit Medium "Provider wires can fail an entire request on one odd history entry". Ratified call: `{}`.
**Regression guard.** `TestAnthropicCodecEncodeRejectsNonObjectArguments` is rewritten into the new pin: encode succeeds, `tc_bad`'s input is `{}`, its tool_result unchanged. This supersedes the documented encode error (internal/provider/wire_anthropic.go, `assistantBlocks` / `anthropicToolInput` docs): every comment in internal/provider/wire_anthropic*.go stating the non-object encode error is rewritten — `grep -n 'encode error\|not a JSON object' internal/provider/wire_anthropic*.go`.
**Goal:** Encoding a history whose tool call carries arguments that are not a JSON object succeeds, with that call's `input` as `{}` and its paired result unchanged.
**Approach (assumed at the header base):** In `internal/provider/wire_anthropic.go`, where the encoder returns an error for non-object arguments, substitute `{}` instead.
**Files:** internal/provider/wire_anthropic.go, internal/provider/wire_anthropic_test.go
**Read first:** internal/provider/wire_anthropic.go — anthropicToolInput, assistantBlocks; internal/provider/wire_anthropic_test.go — TestAnthropicCodecEncodeRejectsNonObjectArguments, anthropicCodec.encode;
internal/provider/wire_anthropic_stream.go — anthropicStream.deltaBlock
**Tests:** a history with truncated `partial_json` arguments encodes; the call's input is `{}`; the next request round-trips; `TestAnthropicCodecEncodeRejectsNonObjectArguments` rewritten into that pin.
**Acceptance:** `go build ./internal/provider/ && go test -count=1 -run 'Anthropic' ./internal/provider/`
**Commit:** `fix(provider): encode unparsable Anthropic tool input as an empty object`

## 15. OpenAI wire renders tool calls as text when no tools are offered

**What:** Recast at the regression check (2026-10-06). Fixes the OpenAI half of audit Medium "Provider wires can fail an entire request on one odd history entry".
**Regression guard.** Ratified design call (owner, 2026-10-06; the compaction premise is false). For a non-native profile, `toProviderRequest` (internal/agent/wire.go) folds each past call into Content in the profile's own format and clears ToolCalls (wire.go's "never double-tell" rule); the fold is keyed on the profile format (`!processing.IsNative(a.textParser)`), not on the `block != ""` branch, so an empty-menu fenced wrap-up Turn folds too. The item adds the missing exported `RenderToolCall(profile, call)` in internal/processing/render.go — fenced via `MarkdownFencedConfig.withDefaults` (string values verbatim plus one trailing line break, others as JSON text, inverting items 11/12), custom-regex via `extractRegexDelimiters` with the `<tool_call>name(args)</tool_call>` fallback. For a native profile on a tool-less Turn, the OpenAI codec renders calls as text as the Anthropic codec does, asserted in internal/provider/wire_openai_test.go (`buildBody` is unexported); the agent test asserts only the fold. Pin one round-trip vector (fold render → items 11/12 parse → identical arguments). Supersedes the `formatMessage` doc "content is null when an assistant message carries only tool calls" (internal/provider/wire_openai.go) for tool-less requests; rewritten.
**Goal:** No assistant tool call goes out as `content: null` with its calls dropped: a prompted-format profile's past calls ride in Content in its own format; a native request offering no tools encodes calls and results as text.
**Approach (assumed at the header base):** As the guard states: `RenderToolCall`, the fold in `toProviderRequest` (independent of the instruction block), and the tool-less text render in `internal/provider/wire_openai.go` after `anthropicToolCallText`.
**Files:** internal/processing/render.go, internal/processing/render_test.go, internal/agent/wire.go, internal/agent/wire_test.go, internal/provider/wire_openai.go, internal/provider/wire_openai_test.go
**Read first:** internal/agent/wire.go — toProviderRequest, toolInstructions; internal/provider/wire_openai.go — formatMessage, buildBody; internal/provider/wire_anthropic.go — anthropicToolCallText;
internal/processing/instructions.go — InstructionsFor, extractRegexDelimiters; internal/processing/factory.go — IsNative
**Tests:** `TestRenderToolCall` (render_test.go) — fenced, custom-regex and fallback renders; `TestProviderRequestFoldsPastCallsIntoContent` (internal/agent/wire_test.go) — recovered history calls ride in fenced Content with no ToolCalls, an empty-menu row included, plus the round-trip row; in internal/provider/wire_openai_test.go, `TestOpenAICodecBodyIsUnchanged`'s "no tools degrades the tool result, logprobs and kwargs effort" row's want rewritten to the non-null text-content form (the native tool-less encode assertion).
**Acceptance:** `go build ./internal/processing/ ./internal/provider/ ./internal/agent/ && go test -count=1 -run 'RenderToolCall' ./internal/processing/ && go test -count=1 -run 'OpenAI' ./internal/provider/ && go test -count=1 -run 'TestProviderRequestFoldsPastCalls' ./internal/agent/`
**Commit:** `fix(provider): render tool calls as text on a tool-less OpenAI request`

## 16. The legacy config fold accepts an existing reactions list

**What:** Fixes audit Medium "Legacy config fold refuses to run when a `reactions:` list already exists".
**Regression guard.** `bothListsRefusal` and `TestMigrateLegacyConfigRefusesBothLists` stay: the item yields to `bothListsRefusal` (internal/config/configmigrate.go: two lists under two names is a hand migration in progress). The "reactions plus `hooks:`" test and Goal clause are dropped — with `hooks:` present the existing reactions are always null, so the expected list is the folded hooks. Tests cover reactions-only plus `mechanisms:` and plus `validated-sets:`.
**Goal:** Folding a config that has a hand-written `reactions:` block and no `hooks:` (with a leftover `mechanisms:` key or `validated-sets:` block) succeeds and keeps those reactions; with `hooks:` present, the check compares against existing reactions plus the folded hooks.
**Approach (assumed at the header base):** In `internal/config/configmigrate.go` `foldLegacyReactions`, build the expected list as `toReactions(before.Reactions)` plus the folded hooks, and pass that to `verifyReactionsFold`.
**Files:** internal/config/configmigrate.go, internal/config/configmigrate_test.go
**Read first:** internal/config/configmigrate.go — migrateLegacyConfig, foldLegacyReactions, verifyReactionsFold, bothListsRefusal; internal/config/reactions.go — toReactions; internal/config/configedit.go — verifiedEdit;
internal/config/configmigrate_test.go — TestMigrateLegacyConfigRefusesBothLists, TestVerifyReactionsFoldRefusesEachWayTheEditCouldBeWrong
**Tests:** reactions-only plus `mechanisms:` folds clean; reactions-only plus `validated-sets:` folds clean; a file with both lists is still refused (`TestMigrateLegacyConfigRefusesBothLists` unchanged).
**Acceptance:** `go build ./internal/config/ && go test -count=1 -run 'Fold|RefusesBothLists' ./internal/config/`
**Commit:** `fix(config): compare the reactions fold against existing reactions`

## 17. Config saves write through a symlinked config.yaml

**What:** Fixes audit Medium "Config saves replace a symlinked `config.yaml` with a regular file".
**Regression guard.** A symlink whose target sits in a read-only directory (e.g. a home-manager link into /nix/store) saves today; the item states and tests its outcome — refuse with an error naming the resolved target, or fall back to today's replace-the-link. The backup clause is out of the item: `backUpConfig` (configmigrate.go) already follows the link. The new tests are `TestWriteConfigAtomically…` in the new configsplice_test.go and skip when `os.Symlink` fails.
**Goal:** Saving a config whose path is a symlink leaves the symlink in place and updates its target atomically; the backup reads the target too.
**Approach (assumed at the header base):** In `internal/config/configsplice.go` `writeConfigAtomically`, resolve `filepath.EvalSymlinks(path)` first, create the temp file beside the resolved path, rename onto it, and stat the resolved path for the backup.
**Files:** internal/config/configsplice.go, internal/config/configsplice_test.go
**Read first:** internal/config/configsplice.go — writeConfigAtomically, lockConfig; internal/config/configmigrate.go — rewriteLegacyConfig, backUpConfig; internal/config/configedit_test.go — onlyFileIn, configAndItsLock;
internal/filewatch/filewatch.go — Watcher.sample; internal/agent/launch_test.go — mustSymlink
**Tests:** a symlinked `config.yaml` saved via `writeConfigAtomically` stays a symlink and its target holds the new bytes; a symlink whose target directory is read-only gets the chosen outcome; both `TestWriteConfigAtomically…`, skipped when `os.Symlink` fails.
**Acceptance:** `go build ./internal/config/ && go test -count=1 -run 'WriteConfigAtomically' ./internal/config/`
**Commit:** `fix(config): save through a symlinked config file`

## 18. Workflow sources de-duplicate items

**What:** Recast at the regression check (2026-10-06). Fixes the collision half of audit Medium "Workflow items can collide on one folder". Ratified call: de-duplicate.
**Regression guard.** Also de-duplicate pick `file:` entries (stages.go pickEntries -> nonBlankLines), first occurrence kept, so a fan_out over a pick file cannot collide on one folder; add a pick-file duplicate test. Entries are compared by their exact unit string and the first is kept verbatim — trim nothing new (lines and pick-file entries are already trimmed by `nonBlankLines`), so only a source that repeats an entry changes its PlanHash — before `batchItems` — equal units give an equal key within a stage (`Expand` cannot compute `ItemKey`). `Expand`'s callers keyscheme_test.go and items_fifo_unix_test.go are updated, or its signature kept with the count exposed separately. `endStage` joins the dedupe note with `redidNote` instead of replacing it. A pre-fix folder whose source repeated entries has a new PlanHash: the item's changelog sidecar entry says a re-run of such a folder is refused and asks for a fresh launch (`ErrFolderMoved`, `rerunMovedFormat`), while a resume or re-issue starts a new folder. This supersedes the `ItemSpec` doc (runner.go, "a fanout list is not deduplicated") and docs/manual/workflows.md's `lines` row; rule: every comment or doc that describes a list/lines/pick-file source's entries or lists what a stage Note carries is updated — find them with `grep -rn 'non-blank lines\|redid\|not deduplicated' internal/workflow docs/manual`.
**Goal:** `list:` and `lines:` sources with repeated entries expand to unique items (first occurrence kept), no two items share an `items/<key>/` folder, and the run's output states how many duplicates were dropped.
**Approach (assumed at the header base):** In `internal/workflow/items.go` `Expand`, drop entries whose item key repeats and return the dropped count to the runner, which reports it in the run's output.
**Files:** internal/workflow/items.go, internal/workflow/runner.go, internal/workflow/items_test.go, internal/workflow/stages.go, internal/workflow/stages_test.go, internal/workflow/runner_test.go, internal/workflow/keyscheme_test.go, internal/workflow/items_fifo_unix_test.go, internal/workflow/format.go, internal/workflow/store.go, internal/workflow/plan.go, docs/manual/workflows.md
**Read first:** internal/workflow/items.go — Expand, batchItems, nonBlankLines; internal/workflow/runner.go — endStage, redidNote, ItemSpec; internal/workflow/stages.go — pickEntries;
internal/workflow/recipe_stages_test.go — TestPickFromAFileInTheWorkflowFolder
**Tests:** a `lines:` file with a repeated line expands once and reports one dropped; distinct lines are unaffected; a `list:` entry `"a.go "` is kept verbatim (no new trim); `TestRunNotesDedupedItems` (runner_test.go) — the fanout's Note/Format line states the dropped count beside a redid note; `TestPickFileDedupesEntries` (stages_test.go) — a pick file with a repeated entry yields it once.
**Acceptance:** `go build ./internal/workflow/ && go test -count=1 -run 'Expand|Dedup' ./internal/workflow/`
**Commit:** `fix(workflow): de-duplicate repeated items before they share a folder`

## 19. A merge child never inherits a stale report.md

**What:** Fixes the report half of audit Medium "Workflow items can collide on one folder, and a stale `report.md` passes". Ratified call: delete before spawn. Depends on item 18.
**Regression guard.** Whenever `runMerge` deletes report.md, or a merge ends failed, `result.Report` is cleared, so a repeat round or a second merge never announces a deleted report (format.go, agent/background.go). The delete hooks into runner.go: once per non-Resumed job before the attempt loop in `runItem`, never per continuation (a capped merge's next round continues the same report.md). It removes the absolute `reportPath` under the Store folder (`os.Remove`, `fs.ErrNotExist` ignored) or goes through a Store method — never `Runner.Workspace`, the user's read-only fs.FS.
**Goal:** A merge child that is not resumed starts with no `report.md` in the folder, so `reportMissing` fails when the child wrote none.
**Approach (assumed at the header base):** In `internal/workflow/stages.go`, before spawning a non-resumed merge child, remove `report.md` (`reportName`) through the workflow's workspace FS; a missing file is not an error.
**Files:** internal/workflow/stages.go, internal/workflow/stages_test.go, internal/workflow/runner.go
**Read first:** internal/workflow/stages.go — runMerge, reportMissing, runRepeat; internal/workflow/runner.go — prepareItems, runItem, itemDraft;
internal/workflow/format.go — Format; internal/agent/background.go — finishReportPrefix
**Tests:** a second merge round whose child writes no report fails `reportMissing`; `TestRepeatMergeClearsAStaleReport` — a repeat over a merge whose later round writes no report leaves `result.Report` empty; a resumed child keeps its report.
**Acceptance:** `go build ./internal/workflow/ && go test -count=1 -run 'Merge' ./internal/workflow/`
**Commit:** `fix(workflow): clear report.md before a fresh merge child runs`
