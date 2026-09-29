# Code Review — Changes since the 2026-09-26 audit (d7ab52f1..HEAD) — 2026-09-29

**Scope:** the 328 files changed since commit d7ab52f1 (the 2026-09-26 audit), heaviest in internal/tui, internal/agent, internal/tools, cmd/apogee and internal/workflow. Some review passes over parts of the agent, daemon, security, tools, workflow and cmd/apogee areas did not complete, so coverage there is thinner. The project's test suite did not finish on this host and no race or coverage run was made; `go vet ./...` was clean. golangci-lint and govulncheck were not run.
**Mission:** apogee is a terminal AI coding agent for smaller, locally hosted LLMs, with the hard invariant that nothing it puts in front of a model may make that model perform worse than the bare loop.
**Files reviewed:** 328

## Executive Summary

The most important finding is in the Floor read cache: it treats any earlier successful read of a file, including a ranged read or one already pruned to a `[pruned:` stub, as a full copy in the conversation. A later full read is then capped to one line, so the model never sees the file. This breaks the "never worse than the bare loop" invariant on a routine small-model pattern. Overall the code is carefully built and most changes match their documented intent; the real defects cluster in the new recipe and workflow surface, where a symlink in a repo skill can pull host files into a child agent's brief, and in lifecycle edges where state is drained or recorded on one exit path without the compensating restore. A second cluster is guard and doc contradictions, such as the scratch-dir exemption swallowing `..` traversal and the merged-stdout denial kill re-opening a closed ADR 0056 incident. No critical issues were found.

## Intent & Architecture Findings

### Medium — Read cache counts a ranged or capped earlier read as a full copy `[Intent & Structure + Correctness]`

- **Where:** `internal/floor/readcache.go:77-91`
- **What:** `priorSuccessfulReadUnchanged` records `lastSuccessfulRead` for any successful read of the path without inspecting the earlier call's `start_line`, `end_line` or `max_lines`. `CacheRead` then caps a later bare read to `max_lines:1`.
- **Why it matters:** the model reads `a.go` lines 1-40, later asks for the whole file, and receives one header line while the guard's own doc promises the existing copy "stays the source of truth". The model can recover with an explicit range, but the guard violates the invariant that Floor guards never make a model worse than the bare loop. Tests cover only a ranged pending call, not a ranged prior read. *(independently verified)*
- **Fix:** count a prior read as a cached copy only when its arguments carry none of `start_line`, `end_line` or `max_lines` (reuse the key list in `capReadArguments`), and skip a prior read this guard itself capped. Add a test with a ranged first read.

### Medium — Repo skill recipes can read host files through symlinks `[Intent & Structure + Security]`

- **Where:** `internal/skills/catalog.go:227-238`, `internal/agent/workflowspawn.go:330-347`, `internal/agent/recipe.go:510`
- **What:** `skillFiles` hands the recipe engine `os.DirFS(dir)` over a disk skill's folder. `os.DirFS` follows symlinks, unlike skill discovery, which is fenced through `os.Root`. `readPrompt` guards only with `fs.ValidPath`, and `stageSkillFile` copies scripts through the same unfenced FS.
- **Why it matters:** a cloned repo ships `.apogee/skills/x/SKILL.md` with a `prompt:` stage whose file is a symlink to `~/.ssh/id_rsa`. When the recipe runs, the target's bytes become a child agent's brief, sent to the upstream model, with no approval and no read-tool fence. `parse.go` states the rule that an untrusted skill must not get the engine to read a host file into a child's brief. The recipe must be started and the target readable by the user, hence Medium. *(independently verified)*
- **Fix:** for a non-shipped Dir, build `Files` from `os.OpenRoot(dir).FS()` so escaping symlinks are refused, and apply the same fence to `stageSkillFile`. Add a symlink test for `readPrompt`.

### Medium — Scratch-dir exemption masks `..` traversal out of the writable area `[Intent & Structure + Correctness + Security]`

- **Where:** `internal/security/dangerous.go:414-446`
- **What:** `maskExempt`/`exemptTokenTail` masks the whole shell token starting at the exempt scratch dir, `..` segments included. `rm -rf <scratch>/../../..` or `echo x > <scratch>/../../../.ssh/authorized_keys` collapse to `<exempt>` and never reach the hard-refuse rules or the `~/.apogee` forced look.
- **Why it matters:** the exemption is justified because the confinement already declares that dir writable, but a lexical `..` reaches paths that are not writable. A plain path spelling, steered by a hostile repo, skill or page, skips the Tier-1/Tier-2 floor. Auto mode is still boxed by the Confiner; in ask-before and allow-edits the human is the only remaining gate.
- **Fix:** in `exemptTokenTail`/`maskSpelling`, refuse to mask a token containing a `..` segment, end the masked token at the first `..`, or path-clean it before masking. Add a table test of `<scratch>/../../.ssh` through `Inspect` expecting `TierHardRefuse`.

### Medium — Merged-stdout denial watch re-opens the incident ADR 0056 D2 closed `[Intent & Structure]`

- **Where:** `internal/platform/denialkill.go:143` (armed at `internal/subprocess/subprocess.go:322`, gated by `internal/tools/terminal.go:45`)
- **What:** the 2026-09-26 merged-stdout watch arms on any command containing `2>&1`, `&>`, `|&` or `>&2`, and uses `denialLinePattern`, which matches a stdout line ending in `permission denied`. The ADR 0056 D2 amendment had stopped watching stdout for exactly that reason.
- **Why it matters:** models append `2>&1` to almost every command. `cat build.log 2>&1` or `grep -r denied . 2>&1` over data with a line ending in `Permission denied` gets killed mid-output and labelled a confinement denial, misleading the model about the cause. This warrants revisiting ADR 0056 D2 (2026-09-26 amendment).
- **Fix:** on the merged-stdout watch, arm the kill only when the same denial also appeared on the process's own stderr or a write was actually refused; otherwise keep the label-only judgement and leave the kill to stderr.

### Medium — Editing a recipe prompt file does not change the resume key `[Intent & Structure]`

- **Where:** `internal/workflow/runner.go:734-743` (with `internal/workflow/store.go:505`)
- **What:** `stageKeyBrief` folds only the `prompt:` path string, not the file's contents, into every item key, and `PlanHash` covers the same path. `ItemKey` promises that a changed context file gives a new key so stale work is redone, but a recipe's prompt file is the brief.
- **Why it matters:** run a recipe, edit its prompt file, re-run on the same scope in the same session: the keys are unchanged, every finished ok/partial item is skipped as "resumed", and old receipts for the old prompt are returned.
- **Fix:** give the Runner a way to read prompt files (the recipe's `Files` fs.FS) and fold their contents into `stageKeyBrief`.

### Medium — A blocked or partial verify child still counts as confirmed or refuted `[Intent & Structure]`

- **Where:** `internal/workflow/stages.go:305-316`
- **What:** `verdictOf` reads only `Fields["verdict"]` and never checks the receipt status, although the type doc says a verify child that ended blocked counts as unclear. `ReceiptSpec.Check` requires declared fields only on an ok receipt.
- **Why it matters:** a verify child that finishes `blocked` or `partial` yet fills `verdict` is folded into the source item's tally as confirmed or refuted, so a claim can be dropped or kept on the strength of an unfinished check.
- **Fix:** return `VerdictUnclear` unless `Receipt.Status == StatusOK`.

### Medium — Console tools ignore cancel during their wait window `[Intent & Structure]`

- **Where:** `internal/tools/console_common.go:139-210` (and `console_open.go:188`)
- **What:** `consoleTail`, `collectConsoleWindow` and `awaitConsoleExit` take a ctx only for `confinementBox`. `Console.Read(wait)` has no ctx and `awaitConsoleExit` sleep-polls to the deadline.
- **Why it matters:** pressing Esc during `console_send` with `wait_ms` up to 30 000 (or `console_open` up to 10 000) on a quiet program is not observed until the window ends, contradicting ADR 0088 (cancel settles promptly) and the package doc that the Go error return is reserved for ctx cancellation. It was not confirmed whether the agent abandons the tool goroutine on cancel, which would soften the impact. *(independently verified)*
- **Fix:** pass ctx into `collectConsoleWindow`/`consoleTail` and make the read a select on `ctx.Done()` plus a timer, returning `ctx.Err()`.

### Medium — `^r` re-run bypasses the actuation latch that guards `/bg` `[Intent & Structure]`

- **Where:** `internal/tui/workflows.go:346`
- **What:** `^r` in a workflow detail takes the `bgLaunching` latch after checking only `m.busy()` and `m.bgLaunching`, not `m.actuation.inFlight`. `/bg` is refused under the actuation latch because its off-loop Agent read races a completing `/load-model` move; `^r` reaches the same launch (`RerunFailed`) through a door that skips the check.
- **Why it matters:** `/load-model` in flight plus `^r` lets the launch snapshot read the Agent off the loop while the actuation may re-point it, a data race. *(independently verified)*
- **Fix:** refuse `^r` when `m.actuation.inFlight`, ideally through the same predicate `actuationBlocked` reads.

## Critical & High Findings

### High — Read cache caps a re-read of a file whose earlier result was pruned to a stub `[Correctness]`

- **Where:** `internal/floor/readcache.go:88`
- **What:** `priorSuccessfulReadUnchanged` treats a prior read as the copy in the conversation without checking the copy still exists. `autoPrune` rewrites stale tool results into `[pruned: N lines — re-run the call if you need it]` stubs, and `resultIsReadError` reports such a result as a success.
- **Why it matters:** in a long session the model reads `a.go`, the result is pruned, and the model follows the stub's own advice and re-runs the read. `CacheRead` caps it to `max_lines:1`, so the file content is in neither the stub nor the re-read and the model loops or edits blind. *(independently verified)*
- **Fix:** skip a prior read whose result content starts with the prune stub prefix (`[pruned:`) when recording `lastSuccessfulRead`, exporting the prefix check from `internal/context` if needed. Add a test.

## Medium Findings

### Medium — MCP size cap is per line, so one hostile server can exhaust memory `[Security]`

- **Where:** `internal/mcp/transport.go:258` (and `:489`; bound in `bounded.go:52-80`)
- **What:** the 4 MiB cap is per line, not per message. One JSON value spread over endless short lines (`[\n1,\n1,...` or newline whitespace inside an object) passes every line check while the SDK's stdio decoder buffers the whole value, and the HTTP JSON path calls `ReadAll` on the body.
- **Why it matters:** a hostile or compromised MCP server, which the threat model includes, can grow apogee's memory without limit; on an 8 GB box that freezes the host.
- **Fix:** add a cumulative cap to `lineBoundedReader` for non-SSE bodies and stdio (bytes since the last complete JSON value or blank-line-delimited SSE event), or wrap plain `application/json` bodies in an `io.LimitReader` of `maxMCPMessageBytes`.

### Medium — Memoised git config probe can be defeated by a same-length rewrite `[Security]`

- **Where:** `internal/gitexec/gitexec.go:482-485`
- **What:** the probe cache is validated by size, mtime, mode and inode only. A confined shell that can write `.git/config` can pad it, wait for one clean probe, rewrite it in place with an equal-length `[core] pager=...` or `filter.x.clean`, and restore mtime with `touch -r`. The next git tool call serves the stale clean answer and runs the repo-named program unconfined on the host.
- **Why it matters:** an escape from confinement through the git tools, needing a shell write that evades the write guard and a fiddly same-length payload. The verifier kept this as uncertain. *(independently verified)*
- **Fix:** also compare ctime (`syscall.Stat_t` Ctim, which userland cannot forge) or hash the small config files, and re-probe on any mismatch.

### Medium — Re-run queued behind another workflow is marked stopped, blocking later retries `[Correctness]`

- **Where:** `internal/agent/background.go:786`
- **What:** `markStopped` unconditionally rewrites a queued run's `status.json` to `PhaseStopped`, but a queued run launched by `RerunFailed` sits on a finished folder (Phase `done`, with blocked items).
- **Why it matters:** `RerunFailed(id)` while another background workflow holds the server queues the re-run; then `StopWorkflow(id)`, `ClearContext`, `RestoreSession` or `Close` flips the folder from done to stopped. The next `RerunFailed(id)` is refused ("has not finished (stopped)") and the blocked items can no longer be retried.
- **Fix:** have `markStopped` leave a folder whose phase was already `done` untouched, and stamp `stopped` only on folders created or mid-run.

### Medium — A finished background workflow's note is lost when the opening Exchange is aborted `[Correctness]`

- **Where:** `internal/agent/loop.go:116`
- **What:** the opening Turn drains the held finish notes (`a.background.takeNotes()`) into the opening user message, but nothing puts them back when that Exchange is scrapped; `exchangeAborted` restores only `retained`. Only the wake path calls `putBack`.
- **Why it matters:** with `workflow-wake: off` (or while idle) a workflow ends, the user sends a message, then aborts the Exchange: the note is gone from the manager and from history and the model is never told the workflow finished.
- **Fix:** record the notes taken at open and `putBack` them from `exchangeAborted`; apply the same to `TakeWorkflowNotes` interjections dropped by an abort.

### Medium — Scheduled `run: workflow:` firings never apply the recipe-failure judgement `[Correctness]`

- **Where:** `cmd/apogee/daemonfire.go:475` (compare `cmd/apogee/headless.go:1170`, `wire_firing.go:893`)
- **What:** headless applies `recipeWorkflowFailure` to `run.Result.Workflow`; the daemon's `fire` maps the result through `firingOutcome`, which drops it.
- **Why it matters:** a nightly recipe whose workflow ends stopped, failed, or with every item blocked is logged and notified as a successful firing with no fault, while the same recipe under headless exits 1. Two Drivers judge the same outcome differently, and an unattended run is where nobody reads result lines.
- **Fix:** hoist `recipeWorkflowFailure` to a shared helper in `wire_firing.go` and, in `fire` after `raise` returns nil, return `out` with that error (with `partialRunSuffix`) so the schedule lands it as failed.

### Medium — Delegate reports with many repeated lines are rejected as degenerate `[Correctness]`

- **Where:** `internal/agent/subagent.go:408-419`
- **What:** `degenerateRepeat` counts any repeated trimmed line, so 50 or more identical lines such as `}`, `)`, `end` or `---` trip it. A roughly 500-line Go file has that many closing braces.
- **Why it matters:** a delegate asked to return code, a diff or a listing gets "sub-agent reply is degenerate", only the first 20 lines are kept, and the round is retained as an error, discarding the real finding.
- **Fix:** count only lines with real content (skip lines of pure punctuation or under about 4 runes), or require a long consecutive run of the same line, which is the actual loop signature.

### Medium — `apply_patch` edits land mid-line and glue text onto unterminated files `[Correctness]`

- **Where:** `internal/tools/file_edit.go:245` and `:249`
- **What:** `applyPatch` finds a hunk by raw substring `strings.Index` with no line-boundary check and takes the first match; and the pure-insertion branch appends `strings.Join(hunk.newLines, "\n")` with no separator and no trailing newline.
- **Why it matters:** a hunk whose `-x = 1` line is a prefix of `x = 10` edits to `x = 20` and reports success; a hunk repeated earlier in the file hits the first copy; an Add File hunk appended to a file without a trailing newline glues its first line onto the old last line. All are silent corruptions of user files.
- **Fix:** require the match to start at a line start and end at a line end (or EOF) and refuse a needle matching more than once, as `find_replace` does; insert a `"\n"` separator when the result is non-empty and lacks one, and end appended text with a newline.

### Medium — Outside click on the workflow boundary confirm strands queued commands `[Correctness]`

- **Where:** `internal/tui/mouse.go:1573-1576`
- **What:** `handlePickerClick` dismisses any picker on an outside click with `m.picker = picker{}`, but the boundary confirm's esc route calls `answerBoundary(boundaryCancel)`, which also runs `runDeferredCommands()` and the held flush.
- **Why it matters:** with `/clear`, a `/sessions` switch or `/fork` confirm open while a background workflow runs, clicking outside the pane cancels the boundary but the queued commands never drain and the held flush is dropped until some later idle or completion event.
- **Fix:** in `handlePickerClick`, when `m.boundaryConfirmOpen()` and the click is outside the rect, return `m.answerBoundary(boundaryCancel)` instead of zeroing the picker.

### Medium — A fanout `out:` without `{item}` makes concurrent children share one file `[Concurrency]`

- **Where:** `internal/workflow/runner.go:754` (with `validate.go:39`)
- **What:** `Validate` only checks that `out` sits on a fanout stage; nothing requires `{item}`, so `outputPath` gives every item the same path.
- **Why it matters:** a recipe with `out: results/report.md` on a fanout at Width > 1 has children overwrite one file, silently losing detail output while every receipt points at the same path. *(independently verified)*
- **Fix:** require `{item}` in a fanout `out:` in `Validate`, or fall back to a per-key path.

### Medium — Windows disk recipe skills cannot find their stage prompts `[Intent & Structure]`

- **Where:** `internal/skills/load.go:294,510` with `internal/agent/recipe.go:343`
- **What:** on Windows the stage prompt is stored via `filepath.Join` (backslashes), but the agent strips the prefix `recipe.Dir+"/"`, which never matches, leaving an absolute path that `fs.FS` rejects.
- **Why it matters:** every user or workspace recipe skill with a `prompt:` stage fails on Windows; shipped skills are unaffected. No Windows test pins it. *(independently verified)*
- **Fix:** keep the recipe prompt folder-relative (join only for display), or compare with `filepath.ToSlash` on both sides.

### Medium — Hostile SKILL.md frontmatter causes a quadratic stall on every skill load `[Security]`

- **Where:** `internal/skills/parse.go:479`
- **What:** the lenient frontmatter fold does `values[openKey] = strings.TrimSpace(values[openKey] + " " + line)` per continuation line, copying the whole accumulated value, and clamps only afterwards (`parse.go:224-225`).
- **Why it matters:** a 1 MiB SKILL.md whose frontmatter fails strict YAML (unterminated quote) followed by roughly 500k one-character lines costs on the order of 2.5e11 bytes copied, tens of seconds on a Pi-class CPU; up to 32 such files fit the catalog cap and `.apogee/skills` is scanned on every Load/Reload. A malicious repo stalls startup and reload.
- **Fix:** stop appending once the value exceeds `maxDescriptionLen` runes and cap `items[openKey]` at `maxTriggers` entries, or accumulate in a `strings.Builder` with that ceiling.

### Medium — MCP HTTP transport pinning, redirect and body-bound lane has no behavioural test `[Critical-Path Tests]`

- **Where:** `internal/mcp/transport.go:356` (pinning at `:367-380`, no-redirect policy at `:463`, body bound at `:483`)
- **What:** `mcp_test.go` only checks that a denied endpoint is refused pre-flight and Admit partitioning; nothing uses `httptest`, a redirect, a proxy, or an oversize SSE/JSON body.
- **Why it matters:** this is the url-safety/SSRF critical path. Dropping the proxy host from `pinned`, following redirects, or unwrapping the bounded body would pass every current test.
- **Fix:** add loopback `httptest` tests asserting (a) a 302 to another private address is not followed, (b) with `proxyForRequest` stubbed the dial is pinned to the proxy address and other addresses are refused, (c) a line over `maxMCPMessageBytes` makes the body read error.

## Recommended Action Order

1. Fix both read-cache defects together (`internal/floor/readcache.go`): they share one function and one test file, and they are the only findings that break the never-worse-than-bare-loop invariant.
2. Close the symlink read in recipe files (`os.Root`-backed `Files` for disk skills) and add the `readPrompt` symlink test; then the `..` handling in `maskExempt`. Both are quick and are guard/containment gaps.
3. Bound MCP messages cumulatively, then add the HTTP transport tests that would have caught it; harden the git config probe fingerprint with ctime.
4. Quick lifecycle fixes: `markStopped` on a done folder, `putBack` of drained notes on abort, `^r` behind the actuation predicate, the outside-click boundary cancel, `verdictOf` status check, and the Console ctx wait.
5. Tool correctness: line-boundary matching and newline handling in `applyPatch`; `degenerateRepeat` content floor.
6. Workflow key and output paths: fold prompt-file contents into `stageKeyBrief`; require `{item}` in fanout `out:`.
7. Share the recipe-failure judgement with the daemon, and fix the Windows prompt path and the frontmatter fold.
8. Needs design discussion first: the merged-stdout denial kill against ADR 0056 D2. Decide whether the kill stays stderr-only before changing code.

## What Looked Good

The agent loop, resolution ladder, cancel-settling paths, sub-agent frame and config sidecar lock read as carefully built, with no crash or data-loss path in the core traces. Skill discovery is well fenced (`os.Root` anchors, count, byte, depth caps), the per-tool declared argument roles and pipeline-aware remote-pipe-to-shell rule match ADR 0012 and 0049, and the root facade, `internal/config`, `internal/refs`, `internal/run`, `internal/schedule` and `internal/present` changes raised nothing at Medium or above. Background workflows run on a detached launch-time snapshot, so the Agent-field races first suspected there do not exist.
