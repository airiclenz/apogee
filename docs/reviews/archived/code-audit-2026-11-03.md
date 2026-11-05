# Code Review — whole repository — 2026-11-03

**Scope:** Whole repository (Go source under `cmd/` and `internal/`, `scripts/`, `Makefile`, CI and editor config). The project's own tooling gave a partial result: `go vet` was clean and the full race-enabled test suite passed. `golangci-lint` and `govulncheck` were not installed, and coverage was skipped because of memory limits on the review machine. Every claim that reached the High level, and most Medium claims, was independently re-checked against the code.
**Mission:** apogee is a terminal AI coding agent for smaller, locally hosted models (and larger ones), running a full tool-use loop with OS-confined Auto mode and an embeddable engine that several Drivers share.
**Files reviewed:** 1487

## Executive Summary

The most important finding is in the Linux `bwrap` confinement backend. It runs without `--unshare-pid` while mounting the host `/proc`, so a confined same-uid command can write outside the box through `/proc/<apogee-pid>/root/...` and, with the default `ptrace_scope=0`, can ptrace or write the memory of apogee itself. This defeats the Auto-mode promise on the Raspberry Pi class of machines the project targets. The rest of the codebase is in good shape: the dispatch, gate, sub-agent privilege inheritance and session paths read as consistent and fail-closed. No finding above High survived verification, and several initial High ratings were lowered after independent checks. Two patterns cut across the Medium findings. Git and shell text is parsed in ways that miss unusual input (spaces or non-ASCII in paths, line continuations, option values), which weakens the secret-file and git-control-plane guards. Several fixed deadlines and one-shot caches still break the "slow machine makes apogee slower, never broken" rule.

## Intent & Architecture Findings

### Medium — `Config.ConfineToWorkspace` is documented as defaulting to true but a zero Config runs Auto unconfined `[Intent & Structure]`

- **Where:** `internal/domain/config.go:157-163` (read at `internal/agent/construct.go:116`, branch at `internal/agent/dispatch.go:1154`)
- **What:** The field is a plain `bool` whose zero value is false, and the engine seeds its live flag straight from it. The dispatch treats false as the "I am the sandbox" unfenced Auto row. A bare `Config{Mode: ModeAuto, Confiner: x}` therefore runs unconfined, the opposite of the documented default and unlike the Floor guard fields, where the zero value keeps the guard on.
- **Why it matters:** The shipped Drivers (TUI, daemon, probe) set the field explicitly, so only a Driver that omits it is exposed: the bench, headless or a library embedder. Such a Driver gets a fail-open Auto mode without noticing. *(independently verified)*
- **Fix:** Invert the field to an `Unconfined` or `Disable...` bool (or a `*bool`) so the zero Config fences. Keep the host's `confine-to-workspace:` key as the negation seam, and check each Driver's Config literal for an explicit assignment.

### Medium — The read-cache Floor guard fights the tool-result-cap guard, capping a needed full re-read to one line `[Intent & Structure]`

- **Where:** `internal/floor/readcache.go:96-100` (with `internal/floor/resultcap.go:41-49`, `internal/context/toolresult.go:37`)
- **What:** `CapToolResults` elides a large earlier read result in the projected request to 20 head and 20 tail lines. `priorSuccessfulReadUnchanged` only checks for a prune stub on the stored result, and the conversation still holds the full text, so the elided copy counts as "already in the conversation". A later bare re-read of that file is capped to `max_lines=1`.
- **Why it matters:** With a small context window, the model sees 40 lines, asks for the whole file and gets a one-line header. That is worse than the bare loop and breaches the guard's own "cannot regress Bypass" claim. The elision marker tells the model to use `start_line`/`end_line`, so it recovers with a wasted turn. *(independently verified)*
- **Fix:** Make the cache decline when the cap would elide the earlier copy. Compare against `capMaxChars(view.Budget())`, or have both guards share one "is this copy still whole in the request" predicate. Alternatively cap the re-read to the file's real first N lines instead of 1.

### Medium — The git work-tree probe latches a negative answer forever on a slow box `[Intent & Structure + Correctness + Concurrency]`

- **Where:** `internal/agent/treesnapshot.go:134` (probe at `:130-150`, timeout at `:55`)
- **What:** `active()` runs under `sync.Once` with the first call's context and a fixed 2 s timeout. On any error it leaves `isRepo` false, and that answer is cached for the life of the Agent.
- **Why it matters:** On a throttled Pi with a cold disk cache, or if the user presses Esc during the first `run_command`, `git rev-parse` can fail once. The tracked-file mutation warning, documented as always on in every mode, is then silently off for the whole session. This contradicts the "slower, never broken" rule.
- **Fix:** Cache only a definitive answer (a clean "not a work tree" or git absent). On cancellation, deadline or transient error leave the probe unresolved so the next call retries. Use a mutex-guarded unknown/yes/no state instead of `sync.Once`, probe under a context detached from the call's cancel, and size the budget for the slowest supported hardware.

### Medium — `go vet` diagnostics run under a fixed 30 s deadline and report a timeout as a code problem `[Intent & Structure + Correctness]`

- **Where:** `internal/tools/diagnostics.go:55,284-318`
- **What:** `runGoVet` never checks `res.TimedOut` or `res.DrainWedged`. A killed vet has exit code -1 and empty output, so it is reported as "go vet reported problems (exit code -1)" together with the claim that the package directory was checked.
- **Why it matters:** On a slow box with a cold build cache, vetting a large package routinely exceeds 30 s. The model is told its clean code has problems and may "fix" code that is fine. *(independently verified)*
- **Fix:** Handle `TimedOut` and `DrainWedged` as an ok result ("go vet skipped: timed out after N s") next to the syntax verdict, and drop the "checked" line on that path. Also size the budget for slow hardware, as `run_tests.go:304` already does with a computed budget.

## Critical & High Findings

### High — bwrap backend exposes the host process table, so a confined command can write outside the box and attack apogee `[Security]`

- **Where:** `internal/platform/namespace_linux.go:348` (documented at `:71`, `:341`; contract at `docs/design/confinement-execution-contract.md:232`)
- **What:** bwrap runs without `--unshare-pid` while `--proc /proc` shows the host process table, and the child keeps apogee's uid. The confined command can open `/proc/<apogee-pid>/root/<path>` (a magic link into the host mount namespace) and write outside the box. With `kernel.yama.ptrace_scope=0` (the Debian and Raspberry Pi OS default) it can also ptrace or write `/proc/<pid>/mem` of apogee. Landlock avoids the ptrace half because its hook scopes to the domain; this backend has no equivalent.
- **Why it matters:** A prompt-injected or hostile model issuing a shell command in Auto mode on the bwrap rung (Pi OS, containers without landlock) can write arbitrary files and inject code into the unconfined parent, which defeats the workspace-write-only fence. The design document records the trade-off without naming this consequence. *(independently verified)*
- **Fix:** Add `--unshare-pid` and handle teardown another way (a `Setsid` process-group kill plus `--die-with-parent`). If the trade-off is kept, at least disclose it as a Residual. To confirm, run `echo x > /proc/<apogee-pid>/root/tmp/escape-probe` inside a confined run and check the host.

## Medium Findings

### Medium — Streamed parallel tool calls numbered `index: 0` with distinct ids fold into one call with invalid arguments `[Correctness]`

- **Where:** `internal/provider/stream.go:560` (`address`, `atIndex` at `:580`, `fold` at `:528`)
- **What:** A fragment carrying a wire index is merged into the call already at that index even when the fragment carries a different, non-empty id. A server that gives every parallel call `index: 0` but its own id (seen from some OpenAI-compatible shims) is folded into one call. The first id and name win and the arguments concatenate into `{"a":1}{"b":2}`.
- **Why it matters:** The loop sees one call with invalid JSON, and the other call id never gets a tool result. The trigger needs a non-conforming server, so the impact is limited to those endpoints. *(independently verified)*
- **Fix:** In `address`, when an index match exists but the fragment's non-empty id differs from the entry's non-empty id, open a new call instead of merging. Keep the empty-id continuation path as is, and add a replay test with two `index:0` chunks and different ids.

### Medium — Release gate misreads any commit that touches a path containing a space `[Intent & Structure + Correctness]`

- **Where:** `cmd/release/gate.go:129` (also `:545`)
- **What:** `touchesOnlyPeelablePaths` splits `git diff-tree --name-only` output with `strings.Fields`. The house plan and handoff files are named `YYYY-MM-DD - NN - name.md`, so they are torn into fragments such as `-` and `00` that match no CI-skip pattern. A git C-quoted non-ASCII path also keeps its leading quote.
- **Why it matters:** The peel stops at a prose-only commit that CI never ran on, and `checkGate`, `status` and `publish` report "ci.yml has no successful run" for a release that is clear. The gate has no bypass, so it fails closed with a misleading message, but it can block a legitimate release. No test covers a spaced path. *(independently verified)*
- **Fix:** Read paths NUL-delimited (`git diff-tree -r -z --name-only`, split on `\x00`, which also disables C-quoting) and add a gate test with a spaced path.

### Medium — Commit-secrets pre-check misses secret-named files whose path git C-quotes `[Correctness]`

- **Where:** `internal/agent/secretsguard.go:185-189` (consumer at `internal/security/secrets.go:106`)
- **What:** Staged paths come from `git diff --cached --name-only` with neither `-z` nor `core.quotepath=false`. Git wraps any path with a non-ASCII byte, control character, `"` or `\` in quotes, so `données/.env` becomes `"donn\303\251es/.env"` and the base name becomes `.env"`. The secret-bearing file-name class (`.env`, `*.pem`, `id_rsa`) then never matches.
- **Why it matters:** A `git_commit` that stages `données/.env` or `日本語/server.pem` under default git config skips the forced approval for the file name. Only a file whose contents also match no pattern slips through, so the practical gap is narrow but it is in a security gate.
- **Fix:** List names with `--name-only -z` and split on NUL (or pass `-c core.quotepath=false` and also unquote control characters). Make the diff header parse in `addedFilePath` tolerate the same quoting.

### Medium — Staged-secret scan skips any added line that begins with `++ ` `[Security]`

- **Where:** `internal/security/secrets.go:113`
- **What:** `SecretFindings` treats every diff line starting with `+++ ` as a file header and skips it. An added line whose content begins `++ ` appears in the diff as `+++ ...`, so it is never scanned and it also resets the current path, mis-attributing later findings.
- **Why it matters:** A prompt-injected or careless model can commit a file with a secret on a line like `++ AKIA...` or `++ -----BEGIN PRIVATE KEY-----`. The `commit-secrets` forced look (ADR 0080) never fires and the commit proceeds under Auto.
- **Fix:** Treat `+++ ` as a header only when the previous line was a `--- ` line (or a `diff --git`/`index` header state). Otherwise scan it as content (`line[1:]`).

### Medium — Shell tokenizer mishandles line continuations and wrapper option values, blinding the git-control-plane write rules `[Intent & Structure + Correctness + Security]`

- **Where:** `internal/security/shellwrites.go:506-510` and `:578-581` (continuations); `:1335-1339,1429-1434,939-940` (`stripWrappers`)
- **What:** A backslash-newline is kept as a literal newline word instead of being removed, as `sh` does. In `a && \<NL> cat .git/HEAD` the newline becomes the command leader and the real leader becomes an operand. That causes false write-refusals on reads such as `cd .git && \<NL> ls hooks`, and it hides a real write when the continuation sits inside a path (`rm -rf .git/\<NL>hooks`). Separately, `stripWrappers` drops wrapper option words without knowing which take a value, so `env -u echo rm -f .git/hooks/pre-commit` takes `echo` as a read leader and never emits the `rm` target.
- **Why it matters:** Multi-line `cmd && \` is a style models emit constantly, so the first defect produces false refusals in common use and a missed write in rarer forms. The wrapper defect lets a hostile model slip a hook write past `write-git-control-plane`. The package doc calls this a footgun guard rather than obfuscation-proof, which keeps the second part at Medium.
- **Fix:** In `scan` and `scanDoubleQuoted`, consume a backslash-newline (and `\r\n`) and emit nothing. In `stripWrappers`, consume the known value-taking options per wrapper (`env -u/-C/-S`, `sudo -u/-g/-h/-p/-C/-D/-R/-T`, `nice -n`, `xargs -I/-n/-s/-P/-d/-E/-a/-L`), or fail closed by returning all remaining words as operands. Check with `writeTargetsOf("cd .git && \\\n ls hooks")`.

### Medium — `git config core.fsmonitor` is not treated as a git-control-plane write `[Security]`

- **Where:** `internal/security/shellwrites.go:875-880` (and `:1270,1280`)
- **What:** `GitCommandConfigName` adds `core.hookspath` to the write view but omits `core.fsmonitor` and `alias.*`, on the grounds that apogee's own git argv neutralises them. That protects only apogee's git tools. `git config core.fsmonitor ./.cache/x` appends no `.git/config` target, so `write-git-control-plane` never fires.
- **Why it matters:** A prompt-injected model can plant a script in the workspace and register it as the fsmonitor. It then runs unconfined the next time the user's shell prompt, editor or `git status` touches the repo. The `alias.*` part is weaker because aliases cannot shadow builtins; `fsmonitor` is the real gap. *(independently verified)*
- **Fix:** Add `core\.fsmonitor` (and `alias\..*`, `interactive\.difffilter`, `include(if\..*)?\.path`) to the write-view-only pattern beside `core\.hookspath`, so the write view is a true superset. Add a rule-engine test of `git config core.fsmonitor ./x`.

### Medium — `SafeReadFile` and the copy source open block on a planted FIFO and read unbounded files `[Correctness + Security]`

- **Where:** `internal/security/safeio.go:259` (also `:270`, `:337`)
- **What:** `SafeReadFile` opens with a blocking read and has no regular-file gate or size bound. `openCopySource` opens the source with a blocking open before it checks `IsRegular()`. `SafeOpen` was already hardened with `O_NONBLOCK` against exactly this planted-FIFO case in the 2026-09-20 audit. Callers on model-chosen paths include `tools/write_target.go:121`, `tools/diagnostics.go:191`, `undo/journal.go:770` and `copy_file`.
- **Why it matters:** A confined subprocess or the model itself can `mkfifo x`, then a write, edit, diagnostics or `copy_file` call on `x` blocks forever and wedges the Turn. A multi-GB planted file causes the same through unbounded allocation on a small-RAM box.
- **Fix:** Route `SafeReadFile` through `SafeOpen` (non-blocking open, fstat, refuse non-regular with `ErrNotRegular`) and read through an `io.LimitReader` with a generous cap and a clear "too large" error. Open the copy source with `sr.OpenFile(rel, os.O_RDONLY|openNonblock, 0)`.

### Medium — Hostile PDF can stall a Turn with quadratic reference scanning `[Security]`

- **Where:** `internal/doctext/pdf.go:1294-1307`
- **What:** `decodedReferences` re-scans a referenced array object in full for every `/Contents` or `/ToUnicode` site that names it, with no de-duplication and no `ctx` check. One object holding K references plus M cheap sites costs M*K steps in `preflightPDF`, before any read budget applies.
- **Why it matters:** A hostile repo file, downloaded document or prompt-injected path can hang the Turn and ignore cancellation. The stall is bounded by file size, not unbounded. *(independently verified)*
- **Fix:** Skip the member scan when `named[number]` is already set, and check `ctx.Err()` in the site loop. Time `ExtractPDF` on a crafted file with ~300k sites to confirm.

### Medium — Anthropic stream accepts unlimited `thinking` blocks, allowing memory exhaustion `[Security]`

- **Where:** `internal/provider/wire_anthropic_stream.go:207` (layout append at `:212`)
- **What:** `openReasoning` stores each non-redacted `thinking` block without charging anything against `maxReplyTextBytes` or counting it against a cap. Text, tool bytes and tool calls are all capped, and only `redacted_thinking` is charged. The stream has no total-byte or duration bound because the idle timer resets on any byte.
- **Why it matters:** A hostile or compromised endpoint, or a middlebox on a cleartext link, can stream endless `content_block_start` events of type `thinking` with distinct indices and never close them. Each ~70-byte event leaves resident state, so agent memory grows until out-of-memory. On a Pi-class box that is well under a minute at LAN speed. The attacker must control the configured `wire: anthropic` endpoint.
- **Fix:** Cap open reasoning blocks (reuse `maxOpenToolCalls` as the bound on `len(s.reasoning)` plus total blocks per reply) and charge a fixed per-block cost to `s.charge`, ending the stream with the existing FaultLocal error.

### Medium — MCP model-facing names can collide when an alias or tool name contains `__` `[Correctness]`

- **Where:** `internal/mcp/tool.go:84-127`
- **What:** Names are built as `<alias>__<tool>` and altered only when out of pattern or over 64 characters. Alias `a` with tool `b__c` and alias `a__b` with tool `c` both yield `a__b__c`, and the hash disambiguation never applies.
- **Why it matters:** One tool shadows the other in the flat registry. Snake_case tool names containing `__` are common, so an odd alias choice or a hostile server can trigger it. *(independently verified)*
- **Fix:** Reject `__` in aliases in `validateServers`, or hash whenever the alias or tool contains the separator. Confirm by registering two such servers and listing the registry.

### Medium — Any 4xx from the MCP token endpoint is reported as "login required" `[Intent & Structure + Correctness]`

- **Where:** `internal/mcpauth/handler.go:274`
- **What:** Every 4xx, including 408 and 429 and 400/401 from a misconfigured client secret, is returned as `ErrLoginRequired`, which tells the user to run `apogee mcp login <name>`.
- **Why it matters:** A rate-limited or timed-out refresh leaves the refresh token valid. The connection fails instead of being retried, and a caller that treats `ErrLoginRequired` as terminal will not retry.
- **Fix:** Restrict `ErrLoginRequired` to 400/401 (ideally `invalid_grant` or `invalid_client`) and return a plain retryable error for 408, 429 and other 4xx.

### Medium — Windows label journal records an entry in memory before the flush, so a retry can label a path with no journal entry on disk `[Correctness]`

- **Where:** `internal/platform/winlabel/session.go:128`
- **What:** `Journal.record` assigns `j.rec.Entries` before `flush()` and does not roll back on a flush failure. The next confined command retries `LabelTree`, finds the entry present, returns `changed=false` without flushing, and proceeds to set the Low label.
- **Why it matters:** After a transient journal write failure (rename refused by AV or an indexer, disk briefly full), a crash leaves Low labels, and a destroyed foreign label, with no record for Recover or Residue. That breaks the journal-before-label rule of ADR 0020 §2. It needs a flush failure, a retry and a crash before the next flush. *(independently verified)*
- **Fix:** On a flush error restore the previous entries (copy the slice before `recordEntry`, since promotion mutates in place), or keep a `dirty` flag and always re-flush while dirty before any label write.

### Medium — systemd unit quoting does not escape `%` and `$` `[Intent & Structure + Correctness]`

- **Where:** `cmd/apogee/daemoninstall.go:426-432`
- **What:** `systemdQuote` claims to spell one argument the way systemd's ExecStart parser reads it and triggers quoting on `$` and `%`, but only escapes `\` and `"`. systemd expands `%` specifiers and `$VAR` even inside double quotes.
- **Why it matters:** A binary or `--config` path containing `%` or `$` yields a unit that starts a different path or fails, while `install` reports success.
- **Fix:** Add `%` to `%%` and `$` to `$$` in the replacer.

## Recommended Action Order

1. Fix the `bwrap` confinement gap first (add `--unshare-pid` or disclose it): it is the only High and it undermines the central Auto-mode promise.
2. Close the cluster of small secret-guard bypasses together, since they share a cause (unusual text in git or shell input): `secretsguard.go` path quoting, the `+++ ` header skip in `secrets.go`, the `core.fsmonitor` gap and the tokenizer defects in `shellwrites.go`. Use NUL-delimited git output for both `secretsguard.go` and the release gate.
3. Make `SafeReadFile` and the copy source non-blocking and size-bounded, then cap the Anthropic reasoning blocks. These are denial-of-service fixes with small diffs.
4. Fix the slow-box defects as a group: the git work-tree probe latch, the `go vet` deadline, and the MCP and release-gate fail-closed paths. Handle a timeout as a distinct outcome everywhere.
5. Fix the correctness bugs with clear one-line fixes: the tool-call fragment joiner, the MCP name collision, the 4xx handling in `mcpauth`, the systemd quoting and the `winlabel` journal rollback.
6. Resolve the two interaction defects (read cache against result cap) and the `ConfineToWorkspace` default. The second needs a design call on the inverted field name. Treat the PDF scan fix as a quick win.

## What Looked Good

The agent dispatch, gate, launch, reaction cascade, resolution ladder and sub-agent privilege inheritance read as consistent and fail-closed. The `.claude` and `.vscode` configuration, the git status and mdns packages, and the Windows and macOS confinement code showed no problems beyond the findings above. Most subsystems carry detailed doc comments tied to decision records, which made intent easy to check. The race-enabled test suite passed in full and `go vet` was clean.
