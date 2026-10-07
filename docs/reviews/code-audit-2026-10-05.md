# Code Review — whole repository (apogee) — 2026-10-05

> Note (2026-10-07): every finding below is closed. Action-order steps 1–4 landed through
> `docs/plans/archived/2026-10-05 - 00 - audit-urgent-fixes-plan.md`, step 5's three model-facing
> findings through `docs/plans/archived/2026-10-06 - 01 - model-facing-contracts-plan.md`, and every
> other finding through `docs/plans/archived/2026-10-06 - 00 - audit-remaining-fixes-plan.md`;
> `CHANGELOG.md` carries each fix.
> Read the report as the record of what was found.
>
> Note (2026-10-07): there is no `git_add` or `git_diff` tool. The git-config finding's exposure is
> any git tool that runs through `internal/gitexec` — `git_status`, `git_diff_range`, `git_log`,
> `git_show`, `git_branch` and `git_commit` (`internal/tools/git.go`).

**Scope:** the whole repository: the Go module (root facade, `cmd/`, `internal/*`), the demo graphics, the `.github`, `.beads`, `.codex` and `.agents` tooling directories, and `scripts/`. The project's linters (`go vet`, `golangci-lint`) report zero issues. The test suite was only partly run, so coverage figures are not part of this report.
**Mission:** apogee is a terminal AI coding agent for smaller, locally hosted LLMs (and better with big ones): one embeddable Go engine behind the TUI, headless runner, daemon and bench, with the hard rule that nothing it adds may make a model perform worse than the bare loop.
**Files reviewed:** 1346

## Executive Summary

The code is in good shape overall. The layering holds, the Floor guards and confinement backends match their documentation on most paths, and the security reviewers found no exploitable hole in the core fences. The most important finding is a likely TUI deadlock: `/clear` or a session switch while a background workflow is running makes the Update loop and the workflow goroutine wait for each other. The weakest area is the places where code and documentation promise different things. The circuit breaker is documented as "consecutive" but trips on interleaved failures. The malformed-tool-call sentinel is documented as a loop contract that nothing implements. The custom-regex instruction example breaks its own manual example. The model-facing parts of these gaps bear directly on the "never worse than the bare loop" invariant. A second cluster is trust-boundary reads that bypass the `os.Root` fence: the workflow runner follows symlinks out of the workspace, and a few shell-write and git-config checks fail open on edge cases.

## Intent & Architecture Findings

### High — Malformed tool call discards every sibling call and the model is never told `[Intent & Structure]`

- **Where:** `internal/processing/toolcall.go:12-15`, `internal/agent/loop.go:529-535`
- **What:** The doc on `ErrMalformedToolCall` says the loop matches it with `errors.Is` and degrades the bad call to a tool-error result (ADR 0007). No caller matches it. `ParseNativeToolCalls` is all-or-nothing, so the loop logs one error, sets `nativeCalls = nil` and treats the Turn as a final reply with no tools.
- **Why it matters:** A reply with three tool calls where one has cut-off JSON (a common small-model failure) loses all three, and the model gets no error result explaining why. A bare loop would at least show the model the error, so this works against the hard invariant.
- **Fix:** Parse per call. Answer the bad call with an error tool result naming it, and dispatch the valid siblings. If that is not wanted, delete the sentinel and its doc claim and revisit the ADR 0007 wording.

### High — Custom-regex tool-call instructions teach the model a call the pattern cannot match `[Intent & Structure + Correctness]`

- **Where:** `internal/processing/instructions.go:158-185`
- **What:** `extractRegexDelimiters` copies the literal text around the first two named groups and only strips backslashes. The manual's own example pattern renders as `<tool_call>s*bashs*{"command": "ls -la"}s*</tool_call>`. Only the JS `(?<name>` spelling is recognised, although the manual also accepts `(?P<name>`, and the group matcher stops at the first `)`.
- **Why it matters:** A model that copies the instruction emits a call the user's pattern does not parse, so a documented custom-regex profile never gets a tool call. This breaks the stated promise that what the model is told and what is parsed cannot drift.
- **Fix:** Build the example from the pattern: translate or drop regex escapes and quantifiers, and fall back to the generic `<tool_call>` example when a delimiter still holds metacharacters. Accept `(?P<` as well.

### Medium — Circuit breaker trips on interleaved failures, then refuses the call forever `[Intent & Structure + Correctness]`

- **Where:** `internal/security/circuitbreaker.go:62-79`, `internal/security/guard.go:123`
- **What:** The breaker is documented as tripping on "consecutive identical failing calls", but it keeps one streak per call signature for the Agent's lifetime. Only a success of the same signature clears it. A tripped signature is refused before execution, so it can never succeed and never clear.
- **Why it matters:** `go build ./...` fails, the model edits a file, the build fails again, and so on. The third failure trips the signature, and the verify-after-edit loop that small models depend on is refused for the rest of the session. This is worse than the bare loop. *(independently verified; severity lowered from High because only one signature is affected and the model can vary arguments)*
- **Fix:** Make the streak truly consecutive (any other recorded call resets it), and let a tripped signature re-arm after a different call succeeds or at a Turn boundary. Review the semantics together with the tool-loop-breaker Floor guard (ADR 0071).

### Medium — Tree-mutation floor misses files that were already dirty `[Intent & Structure + Correctness]`

- **Where:** `internal/agent/treesnapshot.go:128`
- **What:** The floor is documented as naming "the workspace files the command changed", but it diffs whole `git status --porcelain` lines. A file already modified (` M a.go`) and rewritten again by `sed -i` or a formatter keeps the same status line, so it is never reported. A new file inside an already-untracked directory collapses to one `?? dir/` line, with the same result.
- **Why it matters:** The common flow is "edit tool dirties a file, then a shell command rewrites it", which is exactly the unreported case. The floor exists for the incident where a subprocess silently clobbered a tracked file.
- **Fix:** Snapshot a per-path content identity (a hash or `git diff` hash per dirty path, `-uall` for untracked) and diff on that. Otherwise narrow the stated contract to "files whose status changed".

### Medium — A session payload with no `conversation` key half-restores `[Intent & Structure + Correctness]`

- **Where:** `internal/agent/state.go:267`, `internal/agent/state.go:312-318`
- **What:** `decodeState` substitutes an empty conversation only for a zero-length payload. A valid JSON payload that omits or nulls `conversation` (for example `{}` or `{"turnIndex":3}`) leaves `st.Conversation` nil. `restoreState` then skips the conversation but still resets the task list, Turn counters, `exchangeStart` and retained set.
- **Why it matters:** The outgoing conversation stays in memory under the incoming session's counters and no error is returned. The `/sessions` flow would then save the old history into the new session's file. This is the "half-restore with no error" the code's own docs say cannot happen.
- **Fix:** In `decodeState`, treat a nil `st.Conversation` like the empty-payload case (`domain.NewConversation(nil)`) so every payload shape ends in one state.

### Medium — Run errors from subprocesses are swallowed, so a start failure looks like a signal kill `[Intent & Structure + Correctness]`

- **Where:** `internal/subprocess/subprocess.go:343-357`, `internal/subprocess/subprocess.go:430-435`
- **What:** `run` drops `runErr` except for the `ErrWaitDelay` check. A start failure (missing program, bad working directory, permission denied, argument list too long) reaches the model as `ExitCode -1` with empty output and no cause. `RunSubprocessTo` promises a failing writer surfaces "through the exit code and the diagnostics", but a child that exits 0 after a copy error yields a clean-looking run with truncated output.
- **Why it matters:** The model cannot tell a missing binary from a kill, so it cannot route around the problem. Truncated payloads look successful.
- **Fix:** Carry a non-nil `runErr` that is neither an `ExitError` nor `ErrWaitDelay` onto the result (an `Err` field or appended text) and force a non-zero exit code.

### Medium — Undo's staleness guard is checked by each caller, outside the journal lock `[Intent & Structure + Concurrency]`

- **Where:** `internal/undo/journal.go:520`, callers `internal/agent/agent.go:1399-1403` and `cmd/apogee/undo.go:235`
- **What:** `Redo(generation)` enforces the generation check inside the journal's own lock, but `Revert()` takes no generation. Each caller compares `Generation()` and then calls `Revert()` in a separate lock hold.
- **Why it matters:** A sub-agent `Record` landing between the compare and the pop makes the revert undo a group the human never previewed, which is exactly what the guard exists to refuse (ADR 0051 call 7).
- **Fix:** Give `Revert` a `generation` parameter and compare it under the lock in `takeTop`, as `takeRedoTop` does. Remove the two caller copies.

## Critical & High Findings

### High — `/clear` or a session switch deadlocks the TUI while a background workflow runs `[Concurrency]`

- **Where:** `internal/agent/background.go:594-614`, `internal/tui/commandrun.go:202`, `internal/tui/sessions.go:593`
- **What:** `stopAllBackground` waits on each run's `done` channel on the caller's goroutine. `ClearContext` and `RestoreSession` call it from the Bubble Tea `Update` goroutine. `done` closes only after the run goroutine emits its final event through the sink, and that event goes through `tea.Program.Send`, which blocks until `Update` takes it.
- **Why it matters:** One background workflow running, the human runs `/clear` (or switches session) and answers `y` to "stop running workflows?". `Update` waits for the run goroutine and the run goroutine waits for `Update`, so the TUI hangs for good. *(independently verified)*
- **Fix:** Do not wait on the caller's goroutine. Cancel the runs and return, then drop notes and launch state from the goroutine that sees the last `done`, or hand the wait to a `tea.Cmd`. Add a driven `/clear` + `y` test with a running stub workflow.

## Medium Findings

### Medium — Workflow runner reads follow symlinks out of the workspace `[Security]`

- **Where:** `internal/agent/launch.go:210`, `internal/workflow/items.go:300`, `internal/workflow/items.go:277-294`, `internal/agent/workflowspawn.go:407`
- **What:** The runner reads `lines:`, `split:` and context files through `os.DirFS(WorkspaceDir)`, which follows symlinks. `read_file` and `@file` refs use the `os.Root`-pinned `security.SafeOpen`, which refuses escaping symlinks. The directory walk skips symlinks, but the `lines:` read, a symlinked walk root and a symlinked recipe `prompt:` file do not.
- **Why it matters:** A hostile repository commits `notes.txt -> ~/.aws/credentials`. A prompt-injected model calls `fan_out` with `lines: notes.txt`. The call is not approval-gated, and each non-blank line becomes an item prompt sent to the model endpoint. *(independently verified; severity lowered from High because it needs a committed symlink plus a model-issued fan_out and exposes only non-blank lines)*
- **Fix:** Back the runner's workspace with an `os.Root`-pinned FS (`os.OpenRoot(dir).FS()`), or open these files through `security.SafeOpen`, and refuse non-regular or escaping entries. Apply the same refusal to the walk root and to recipe prompt files.

### Medium — A message typed during a session load or `/bg` launch starts a second goroutine on the Agent `[Concurrency]`

- **Where:** `internal/tui/model.go:2099-2147`, `internal/tui/commandrun.go:120`, `internal/tui/sessions.go:593`
- **What:** The plain-message send path checks `prebound`, `actuation.inFlight`, `blockedUpstream` and `eng.InExchange()` but not `holdBgLaunch` or `holdSessionLoad`. Only commands consult the hold set.
- **Why it matters:** Pressing Enter on a message while a `/sessions` load or `/bg` launch is in flight starts a worker that Steps the single-goroutine Agent beside the Cmd goroutine, and `resumeLoaded` can then reset the transcript under a running Step. `RestoreSession` usually refuses the swap, but the `/bg` overlap and the unsynchronised read remain. *(independently verified; severity lowered from High because the window is narrow)*
- **Fix:** Make `submit` (and the flush, continue and compact openers) refuse while the hold set is active, with a "still loading" note that keeps the typed line. Have `resumeLoaded` bail out if `m.busy()`. Add a driven test that sends a message between `acceptBrowser` and `sessionLoadedMsg`.

### Medium — Landlock helper does not pin its OS thread before restricting and exec `[Concurrency]`

- **Where:** `internal/platform/landlock_linux.go:302`, `internal/platform/landlock_linux.go:376-384`
- **What:** `ApplyLandlockAndExec` sets `NO_NEW_PRIVS` and calls `landlock_restrict_self` as raw syscalls that bind to the calling thread only, then `syscall.Exec`. There is no `runtime.LockOSThread()` anywhere in the repo.
- **Why it matters:** If the goroutine is rescheduled onto another thread between the restriction and the exec, the command runs from an unrestricted thread and escapes the landlock domain in Auto mode. The window is small, so it would show up as a rare flake. *(independently verified; severity lowered from High because the helper is short-lived and near-idle)*
- **Fix:** Call `runtime.LockOSThread()` as the first statement of `ApplyLandlockAndExec` and never unlock it.

### Medium — bwrap-confined children keep the controlling terminal and can inject keystrokes `[Security]`

- **Where:** `internal/platform/namespace_linux.go:336-344`, `internal/platform/teardown_unix.go:45`
- **What:** The bwrap argv omits `--new-session` (deliberately, and a test pins it), and teardown only sets `Setpgid`, not `Setsid`. With `--dev /dev`, a confined child can open `/dev/tty` and use `ioctl(TIOCSTI)`.
- **Why it matters:** On kernels where legacy TIOCSTI is enabled, a hostile command can type into apogee's input, including answering an approval prompt. bwrap is the fallback backend, but the gap is not named in the docs. *(independently verified; severity lowered from High because it needs the TUI to hold a tty and TIOCSTI enabled)*
- **Fix:** Drop the controlling terminal for non-Console confined runs (`--new-session` or `Setsid`), keeping the tty only for the pty Console path. Document or test the choice.

### Medium — Git command-config probe fails open on truncation, timeout or wedged drain `[Intent & Structure + Correctness]`

- **Where:** `internal/gitexec/gitexec.go:625-628`, `internal/gitexec/gitexec.go:683-685`, `internal/gitexec/gitexec.go:706-720`
- **What:** The refusal that blocks commands when the repository's own config holds a command-valued key (`filter.*.clean`, `core.pager`) reads `git config --local --list`, whose output is capped at 256 KiB. The code skips any non-zero exit and never checks `TimedOut` or `DrainWedged`. A listing cut at the cap, or a rev-parse that times out, looks like "no command keys", and the real git command runs un-probed.
- **Why it matters:** A repository shipped with a large `.git/config` that puts a command key after the cap makes the next `git_add` or `git_diff` run the attacker's program. *(independently verified; severity lowered from High because it needs a hostile config over 256 KiB or a 15 s hang)*
- **Fix:** Treat a truncated listing, a timeout or a wedged drain as a refusal or error, never as a pass. Accept only a clean non-zero exit as "scope does not apply", and never memoise an incomplete answer. Add tests for the truncated and timed-out probe.

### Medium — Shell-write guard misses writes to `.git/hooks` through heredocs and ordinary verbs `[Correctness + Security]`

- **Where:** `internal/security/shellwrites.go:298-314`, `internal/security/shellwrites.go:367-377`, `internal/security/shellwrites.go:459-492`
- **What:** The `write-git-control-plane` hard-refuse judges a command only by what `writeTargetsOf` says it writes. Heredoc bodies fed to an interpreter (`bash <<EOF ... chmod +x .git/hooks/pre-commit`) are skipped as payload. `readLeaders` lists `uniq`, `xxd` and `tree` as writeless, but `uniq in out`, `xxd -r in out` and `tree -o file` write a file. `sed -n 'w file'` and `git diff --output=file` are also judged writeless, and `cd .git && echo x > hooks/pre-commit` never joins the two parts.
- **Why it matters:** This is the only guard on `.git/hooks` and `.git/config` inside the workspace, which the confiner leaves writable. A hostile model can plant a payload with `write_file` and copy it over an executable hook, gaining code execution at the user's next `git commit`. The guard is documented as a footgun guard, not a boundary, but these are ordinary verbs, not obfuscation.
- **Fix:** Drop `uniq`, `xxd` and `tree` from `readLeaders`. Treat `sed` `w`/`W` commands and any `--output`/`-o` option as writes. Add heredoc body words to the operands of non-read leaders. Track `cd` targets and prefix later relative operands.

### Medium — Markdown-fenced tool-call format truncates and mangles argument values `[Correctness]`

- **Where:** `internal/processing/markdown_fenced.go:135-150`, `internal/processing/args.go:12-17`
- **What:** `fenceClose` closes the block at the first ``` not followed by `tool`, so an argument that contains its own code fence ends the block early. `tryParseValue` trims every value, turns anything that is valid JSON (`123`, `true`, `{"a":1}`) into a non-string, and strips leading indentation and the trailing newline from strings.
- **Why it matters:** Writing a README with a fenced block dispatches `write_file` with cut-off content. Writing a `package.json` sends an object instead of a string. A Python `new_string` loses its indentation. *(independently verified; severity lowered from High because only the opt-in markdown-fenced and custom-regex formats are affected)*
- **Fix:** Close the block only on a line that is exactly ``` and prefer the last such line after the last argument marker. Keep values verbatim (drop one leading and one trailing line break), and coerce to a non-string only when the target tool's schema says so.

### Medium — MCP tool names are passed to the model without checking the provider name pattern `[Correctness]`

- **Where:** `internal/mcp/tool.go:79-84`, `internal/mcp/client.go:280-288`
- **What:** `qualifyToolName` only prepends `<alias>__`. MCP allows dots in tool names (`files.read`), and a long alias plus a long tool name can pass 64 characters. Strict providers require `^[a-zA-Z0-9_-]{1,64}$`.
- **Why it matters:** One such tool makes the provider reject every request that carries the tool menu, so the whole session breaks, not just that tool. *(independently verified)*
- **Fix:** In `listServerTools`, sanitise or skip tools whose qualified name fails the pattern, and keep a map from the model-facing name back to `remoteName`.

### Medium — Provider wires can fail an entire request on one odd history entry `[Correctness]`

- **Where:** `internal/provider/wire_anthropic.go:411-414`, `internal/provider/wire_anthropic.go:761-767`, `internal/provider/wire_openai.go:196-205`
- **What:** The Anthropic encoder returns an error for any tool-call argument string that is not a JSON object, and a reply cut off by `max_tokens` leaves truncated `partial_json` in the committed history. The OpenAI encoder sends a tool-call-only assistant message as `content: null` with `tool_calls` dropped when the request offers no tools, which is what the compaction summariser does.
- **Why it matters:** On Anthropic, one truncated call makes every later request fail. On the OpenAI wire, compaction of any history with a tool-call-only turn is rejected by validating servers such as llama.cpp. *(independently verified; the llama.cpp rejection is from known server behaviour, not tested here)*
- **Fix:** For unparsable arguments send `{}` or drop the call and its result instead of failing the encode. When no tools are offered, render tool calls as text as the Anthropic codec already does.

### Medium — Legacy config fold refuses to run when a `reactions:` list already exists `[Correctness]`

- **Where:** `internal/config/configmigrate.go:1174`, `internal/config/configmigrate.go:1323`
- **What:** `foldLegacyReactions` builds its expected list from `hooks:` only. With no `hooks:`, that list is nil, and `verifyReactionsFold` compares it with the user's existing non-empty `reactions:`. They differ, so the edit fails with "the folded reactions: block does not fire what the hooks: block fired".
- **Why it matters:** A config with a hand-written `reactions:` block plus a leftover `mechanisms:` key or `validated-sets:` block turns into a `reactionsRefusal`, and startup aborts. The refusal's own advice (move the entries by hand) leads into this state.
- **Fix:** When there are no `hooks:` entries, compute the expected list from `toReactions(before.Reactions)`. In general compare against the resolved existing reactions plus the folded hooks.

### Medium — Config saves replace a symlinked `config.yaml` with a regular file `[Correctness]`

- **Where:** `internal/config/configsplice.go:208`, `internal/config/configsplice.go:229`
- **What:** `writeConfigAtomically` stats the path (following the link) but renames its temp file over the path itself, replacing the symlink.
- **Why it matters:** If `~/.apogee/config.yaml` is a symlink into a dotfiles repo, every `/settings` save silently detaches it. The managed file stays stale and the next dotfiles sync overwrites the edit.
- **Fix:** Call `filepath.EvalSymlinks(path)` at the top, create the temp file beside the resolved path and rename onto it. Do the same for the backup stat.

### Medium — Workflow items can collide on one folder, and a stale `report.md` passes for a new report `[Correctness + Concurrency]`

- **Where:** `internal/workflow/items.go:116`, `internal/workflow/runner.go:529`, `internal/workflow/stages.go:181`, `internal/workflow/stages.go:346-353`
- **What:** `list:` and `lines:` sources are not de-duplicated and `Validate` does not reject duplicates. Two items with the same label and units get the same key and the same `items/<key>/` folder, and `runItems` runs them concurrently. Separately, every merge stage writes the same `report.md`, and `reportMissing` only checks that the file exists, so an earlier stage's or round's report passes.
- **Why it matters:** A repeated line in a `lines:` file at Width 2 has two children writing one detail file and the work is paid for twice. A merge child that claims a report it never wrote has its failure hidden by the old file, and the parent reads stale content as the new report.
- **Fix:** Reject duplicates in `Validate` and `Expand`, or fold the item index into the key. Use a per-stage and per-round report path, or delete `report.md` before spawning a merge child that is not resumed.

### Medium — `Scheduler.Add` can race `Close` on the WaitGroup `[Intent & Structure + Correctness + Concurrency]`

- **Where:** `internal/schedule/schedule.go:348-354`
- **What:** `Add` releases `s.mu` and only then calls `s.wg.Add(1)` and starts the loop goroutine. A concurrent `Close` can take the lock, set `closed`, run `wg.Wait()` at count zero and return in that gap.
- **Why it matters:** The late `wg.Add(1)` is a `WaitGroup` misuse, and the loop goroutine outlives `Close` and can emit `EventCreated` after the Driver was told everything had stopped. The TUI adding a schedule while quit calls `Close` is enough.
- **Fix:** Call `s.wg.Add(1)` before `s.mu.Unlock()`, inside the `!closed` branch. Add a `-race` test that runs `Add` and `Close` concurrently.

### Medium — `stripThinking` slices with an index taken from a lower-cased copy `[Correctness]`

- **Where:** `internal/title/title.go:459-468`
- **What:** It finds `</think>` with `strings.Index` on `strings.ToLower(trimmed)`, then slices `trimmed` with that index. `ToLower` changes byte length for some characters (`İ` shrinks, `Ⱥ` grows).
- **Why it matters:** A thinking model that reasons about such text gets a garbled title like `ink>` stored as the session name, or a slice past the end of the string, which panics.
- **Fix:** Search and slice the same string, using a case-insensitive match that does not change offsets (for example an ASCII-only fold or `regexp (?i)`).

## Recommended Action Order

1. Fix the background-workflow deadlock in `stopAllBackground`. It can hang the TUI for good.
2. Close the symlink read gap in the workflow runner (`os.Root`-pinned FS). This is a read-fence bypass in the threat model, and one change covers four call sites.
3. Fix the two quick confinement items: `runtime.LockOSThread()` in the landlock helper, and `--new-session` (or `Setsid`) for bwrap non-Console runs.
4. Make the gitexec command-config probe fail closed on truncation, timeout and wedged drain.
5. Repair the model-facing contract gaps that bear on the non-inferiority invariant: the circuit breaker's "consecutive" semantics, the malformed-tool-call path, and the custom-regex instruction example. These need a short design decision first, so raise the circuit breaker and sentinel together with the Floor guard ADRs (0071, 0007).
6. Tighten the shell-write guard (`uniq`/`xxd`/`tree`, heredoc bodies, `--output`) and add the missing adversarial tests.
7. Quick wins: the `decodeState` nil-conversation guard, `wg.Add` ordering in `Scheduler.Add`, `stripThinking` offsets, the TUI send-path hold check, and passing the `generation` into `Revert`.
8. Then the wire and config fixes: MCP name sanitising, Anthropic/OpenAI encode fallbacks, the legacy-fold comparison and the symlink-safe config write.

No finding was marked a candidate for `/improve-codebase-architecture` as a primary fix. The two nearest are the diverged `Revert`/`Redo` generation checks and the duplicated recording proxies in `internal/stubllm`.

## What Looked Good

The package boundaries and layering are clean. The path fence, resolved-IP SSRF floor, staged-secret scan, exec fence and the doc-count pin tests held up under review. Floor guards (`internal/floor`), `internal/domain`, `internal/profiles`, `internal/scheme`, `internal/sanitize` and the demo graphics produced no findings above the floor. The driven end-to-end and escape-probe test strategy is thorough, and most of the gaps found are in the edges (failure arms and concurrency contracts) rather than the main paths.
