# Code Review — apogee (whole repository) — 2026-11-10

**Scope:** The whole repository: cmd/, internal/ (agent, config, floor, gitexec, platform, security, tools, tui, and the rest), scripts, build and CI files. The project's own machine checks (go vet clean; race-enabled suite green) were taken as read.
**Mission:** apogee is a terminal AI coding agent for smaller, locally hosted LLMs (and large ones too) that runs a full tool-use loop over OpenAI-compatible or Anthropic servers and must never make a model perform worse than the bare loop.
**Files reviewed:** 1524

## Executive Summary

The most important finding is that a hostile PDF can kill the whole process: the PDF extractor's `recover` cannot catch Go's fatal stack overflow, and nothing bounds the nesting depth that the third-party parser recurses on. Beyond that the code is in good health: nothing Critical, and the verified defects are narrow. Two themes recur. First, fixed deadlines on slow-but-healthy work (self-update download, snapshot git calls, the provider's 5xx retry, the subprocess drain) contradict the stated "a slow machine makes apogee slower, never broken" invariant. Second, several trust gates have small holes: the adoption prompt prints unsanitised text, the git hardening denylist and the Auto fence both have gaps, and the commit secrets guard can scan a clipped diff and pass it.

## Intent & Architecture Findings

### Medium — Provider gives up on a loading local server after about 0.6 s `[Correctness]`

- **Where:** `internal/provider/client.go:73-74,577-594,623-629`
- **What:** A 5xx is retried twice (200 ms, 400 ms) and then becomes a non-retryable `DeltaError`. llama.cpp answers 503 "Loading model" with no `Retry-After` for the whole load.
- **Why it matters:** A prompt sent just after starting or swapping a local server on a Pi-class box fails after 0.6 s with "upstream HTTP 503", though the server is healthy and only slow. This contradicts the slow-box invariant. *(independently verified)*
- **Fix:** For a 503 without `Retry-After`, back off on a longer schedule bounded by a generous backstop (as `maxRetryAfter` already does), or mark the final 503 retryable so the loop re-streams it.

### Medium — `apogee update` fails on a slow but progressing download `[Intent & Structure + Correctness]`

- **Where:** `internal/update/stage.go:36,141`
- **What:** `downloadTimeout = 2 * time.Minute` is set as `http.Client.Timeout`, which bounds the whole transfer including the body read.
- **Why it matters:** A tens-of-MB archive over a throttled link or SBC (under about 200 KB/s) times out mid-transfer; the user can only retry. *(independently verified)*
- **Fix:** Drop the whole-request timeout. Bound stalls instead (response-header timeout plus a read deadline that resets on each body read) and keep a much larger backstop and the existing size ceilings.

### Medium — Fixed 60 s deadline on every snapshot git call `[Intent & Structure + Correctness]`

- **Where:** `internal/snapshot/store.go:25` (used at :171, :479, :482, :559, :615)
- **What:** `snapshotTimeout = 60s` applies to every git invocation, including the whole-tree `git add -A` in `stageAndWrite` and blob reads in `Content`.
- **Why it matters:** On a slow machine with a large workspace the first capture exceeds 60 s, `Capture` fails with a deadline error, and the Exchange has no snapshot-backed undo. *(independently verified)*
- **Fix:** Use an idle or no-output backstop with a much larger ceiling, or scale the budget with the work; at minimum make it generous and configurable.

### Medium — A command that ran is reported as "could not run" when a detached child holds stderr `[Intent & Structure]`

- **Where:** `internal/userexec/userexec.go:157`
- **What:** `exec.ErrWaitDelay` is neither nil, an `*ExitError` nor `TimedOut`, so it falls through to `could not run X: exec: WaitDelay expired before I/O complete`. `internal/subprocess` already treats the same condition as a wedged drain, not a failure to run.
- **Why it matters:** A Reaction `run:` notifier or an `api-key-cmd:` tool that starts a background helper keeping stderr open reads as failed (a key command as a failed key) although it exited normally. *(independently verified)*
- **Fix:** Treat `errors.Is(runErr, exec.ErrWaitDelay)` as a ran-to-status result (exit code 0, keep captured stderr), mirroring `subprocess`.

### Medium — The adoption pin does not cover what an adopted launcher actually executes `[Correctness]`

- **Where:** `internal/config/execpin.go:146-186`
- **What:** The pin covers only regular files literally named by an argv element or env value. An adopted MCP server or reaction whose launcher (`npm`, `make`, `node .`, `python -m`) reads unnamed manifests, directories or imports is not covered.
- **Why it matters:** The repo can change what runs after adoption without a re-proposal, which undercuts the ADR 0096 promise that the human rules on the bytes that will run. *(independently verified)*
- **Fix:** Document the limit prominently in the adoption prompt, and pin the launcher's well-known manifests (`package.json`, `Makefile`, `go.mod`, `pyproject.toml`) or the working directory's tree hash for those launchers.

### Medium — A failed rebind is never retried, leaving the session on a stale model `[Correctness + Intent & Structure]`

- **Where:** `internal/tui/heartbeat.go:330`
- **What:** `observeBinding` records `observedModel`/`observedWindow` before `applyRebind` runs. If `Server.Rebind` errors, later beats see no change and never re-attempt.
- **Why it matters:** One transient failure leaves apogee talking to a model the server no longer serves, which is the failure the heartbeat exists to prevent; the `lastRebindFailed` dedupe is reachable only on a flip-flop. *(independently verified)*
- **Fix:** On rebind error, restore the pre-capture observed values so the next beat re-detects the change, keeping `lastRebindFailed` as the once-per-failure note guard.

## Critical & High Findings

### High — A hostile PDF can crash the whole process with a stack overflow `[Security]`

- **Where:** `internal/doctext/pdf.go:201-231`
- **What:** `ExtractPDF`'s `recover` cannot intercept Go's fatal `stack overflow`. The parser (`readObject`/`readArray`/`readDict`) recurses once per `[` or `<<` with no depth limit, and `pdfMaxNesting` bounds only the preflight's own lexer.
- **Why it matters:** A PDF of a few million `[` (a prompt-injected repo file, a download, an `@file` reference, up to the 10 MiB ceiling) kills the TUI, headless run or daemon and loses the in-flight turn. The package comment's "nothing escapes this function" is false for this class. *(independently verified)*
- **Fix:** In `preflightPDF`, refuse any document whose raw bytes outside inflated streams nest brackets or dicts deeper than a small bound (for example 64). Also bound the decoded content streams the walk interprets.

## Medium Findings

### Medium — The commit secrets guard can scan a clipped diff and pass `[Correctness]`

- **Where:** `internal/gitexec/gitexec.go:458`
- **What:** `queryDiagnosed` maps `TimedOut` and `DrainWedged` to `ErrIncomplete` but ignores `res.Truncated`, so stdout cut at the 256 KiB `MaxSubprocessOutputBytes` cap returns as a complete, successful answer.
- **Why it matters:** `internal/agent/secretsguard.go:181` runs `git diff --cached` through `host.Query`. A staged diff over 256 KiB (a lockfile sorted ahead of a file with a secret) means the secret after the cap is never scanned, so the guard fails open. `probeGit` in the same file already treats truncation as incomplete. *(independently verified)*
- **Fix:** When `res.Truncated`, return an `incompleteError` ("git <sub>: output exceeded <cap> bytes") so callers treat it as no trustworthy answer.

### Medium — Git command-config denylist omits keys that make git run a program `[Security]`

- **Where:** `internal/gitexec/gitexec.go:490` (list at `internal/security/shellwrites.go:992`)
- **What:** The repo-local denylist misses `remote.<n>.uploadpack`/`receivepack`, `protocol.ext.allow` with `ext::` URLs or `url.<x>.insteadOf`, and `submodule.<n>.update=!cmd`.
- **Why it matters:** A planted `.git/config` entry (by a hostile checkout or a prompt-injected model) is executed unconfined and ungated by apogee's own fetch, pull and push. *(independently verified)*
- **Fix:** Add `remote\..*\.(uploadpack|receivepack|vcs)`, `protocol\..*allow`, `submodule\..*\.update` and `url\..*\.insteadof` to the denylist, or pass `-c protocol.ext.allow=never` in `hardeningOptions` and set `GIT_ALLOW_PROTOCOL=file:git:http:https:ssh`.

### Medium — `apogee project adopt` prints repo-supplied rule text raw into the terminal `[Security]`

- **Where:** `cmd/apogee/project_cmd.go:257` (also :278 and `heldName`)
- **What:** An Allow rule's `proposal.Text` from the repo's `.apogee/config.yaml` is printed with no escape or control-character stripping; the TUI pane and the undo listing already strip.
- **Why it matters:** A hostile repo can use `\r` or cursor-move sequences so the question shows a harmless prefix while the bytes pinned by `y` are a different, dangerous one. This is the trust gate the command exists to make. Needs a deliberately hostile config and a user who answers `y`. *(independently verified, severity lowered from High)*
- **Fix:** Print `strconv.Quote(sanitize.StripEscapesToLine(proposal.Text))` in `printProjectQuestion` and `heldName`, and consider rejecting control characters in rule text at load (`internal/config/allow.go`).

### Medium — A hostile MCP server can receive the preregistered client secret `[Security]`

- **Where:** `internal/mcpauth/login.go:134`
- **What:** The `client-secret-env` secret is sent to `prm.AuthorizationServers[0]` taken from the MCP server's own metadata, with no pinning of the authorization server.
- **Why it matters:** A hostile MCP server names its own token endpoint and receives the client secret in the code exchange and on every refresh. *(independently verified)*
- **Fix:** Require the authorization server to match an operator-configured issuer or token endpoint when a client secret is used, and refuse to send the secret otherwise.

### Medium — The Auto write fence is escapable through reachable local services `[Security]`

- **Where:** `internal/platform/landlock_linux.go:236` (also `namespace_linux.go:768-777`, `seatbelt.go:1032-1075`)
- **What:** Landlock before ABI 9 and bwrap's read-only bind leave `connect(2)` to path-addressed sockets open; seatbelt leaves `mach-lookup` open; `DBUS_SESSION_BUS_ADDRESS` and `XDG_RUNTIME_DIR` are inherited. The residual is disclosed only as a network-egress gap.
- **Why it matters:** A confined child can ask systemd (`systemd-run --user`), dockerd or launchd to spawn a process outside the fence, writing anywhere the user can (`~/.bashrc`, `~/.ssh/authorized_keys`). This contradicts Auto's "fences writes to the workspace" promise. *(independently verified)*
- **Fix:** On bwrap overlay `--tmpfs /run/user` and `--tmpfs /var/run`; on seatbelt deny `mach-lookup` for launchd and AppleScript services; in every backend drop `DBUS_SESSION_BUS_ADDRESS` and `DOCKER_HOST` from the confined environment; disclose any remaining gap in the Auto banner.

### Medium — A file in `confine-writable-paths` breaks every confined call on the landlock backend `[Correctness]`

- **Where:** `internal/platform/landlock_linux.go:368`
- **What:** Every writable root, including a regular-file entry, is passed to `allowWriteBeneath` with the directory-wide access mask; `landlock_add_rule` returns `EINVAL` for directory-only rights on a non-directory.
- **Why it matters:** One entry such as `~/.gitconfig` makes `applyLandlock` fail, so every confined shell call fails on landlock hosts while the bwrap and seatbelt backends accept the same box. *(independently verified)*
- **Fix:** `fstat` the opened root and use the file-applicable mask (`deviceAccessMaskForABI`) when it is not a directory.

### Medium — `run_tests` turns a file path into a package pattern that matches nothing `[Correctness + Intent & Structure]`

- **Where:** `internal/tools/run_tests.go:394`
- **What:** `path: "internal/foo/foo_test.go"` becomes `go test ./internal/foo/foo_test.go/...`, because `subtree()` returns the file itself.
- **Why it matters:** The tool description advertises file paths and pytest handles them. For Go the run fails with a misleading error or, if go exits 0 with "no packages to test", reports PASS for tests that never ran, which a model reads as a green suite. *(independently verified, severity lowered from High)*
- **Fix:** For the Go runner, map a file subtree to its directory before `goPackagePattern`, or refuse a file path for runners that cannot take one.

### Medium — The syntax check false-positives on valid JSX, PHP and C# `[Correctness + Intent & Structure]`

- **Where:** `internal/syntaxcheck/syntaxcheck.go:301`
- **What:** `checkBrackets` opens a string on every quote or backtick, and `inString`/`escaped` persist across lines with no JSX text model. `<p>Don't do this</p>`, PHP `# don't` comments and C# verbatim strings swallow brackets or report "unclosed string literal".
- **Why it matters:** A valid edit triggers a needless correction Turn, which the Bypass floor forbids. *(independently verified, severity lowered from High)*
- **Fix:** For `.jsx`/`.tsx`, do not open a `'` string in JSX text; reset single- and double-quote string state at end of line for languages where such strings cannot span lines, keeping multi-line only for backtick (JS/TS) and triple-quote (Python).

### Medium — gpt-oss tool-call headers are not recognised as Harmony `[Intent & Structure + Correctness]`

- **Where:** `internal/processing/harmony.go:25-35`
- **What:** The `harmonyMessage` and `harmonyOpen` patterns allow only `[^<]*` between the channel name and `<|message|>`, so `<|channel|>commentary to=functions.x <|constrain|>json<|message|>{...}<|call|>` never matches.
- **Why it matters:** `StripHarmony` returns the raw control tokens and JSON arguments as visible text and `IsHarmonyThinking` returns false while it streams, for a gpt-oss model behind a server that leaves Harmony unparsed. *(independently verified)*
- **Fix:** Allow `(?:[^<]|<\|constrain\|>)*` in both patterns and add a test for the `constrain` header.

### Medium — `diagnostics` reports a clean file in a nested Go module as a vet failure `[Correctness]`

- **Where:** `internal/tools/diagnostics.go:338-345`
- **What:** `goVetSpec` runs `go vet <abs package dir>` with `Dir` at the workspace root and `GOWORK=off`; a file under a nested `go.mod` fails with "directory is outside main module", and `runGoVet` reports that as findings.
- **Why it matters:** The model is told a clean file has vet errors and may churn on a phantom problem. *(independently verified)*
- **Fix:** Run with `Dir: filepath.Dir(abs)` (still inside the fence) and vet `.`, or locate the nearest `go.mod`.

### Medium — Data race on the turn counter from background workflow children `[Concurrency]`

- **Where:** `internal/agent/construct.go:654-662`
- **What:** `wireTap.observe` reads the plain `int` `a.turns.index` from the goroutine streaming a background workflow child's request, while the conversation goroutine increments it (`turn.go:250/258/332`).
- **Why it matters:** With the Inspector on and a background fan_out, recipe or sub_agent running while the user chats, this is an unsynchronised read and write, with stale or torn Turn stamps and a `-race` report. *(independently verified)*
- **Fix:** Stamp from a safely readable snapshot (an `atomic.Int64` mirror of `turns.index`, or the emitting child's own base) instead of the top-level Agent's field.

### Medium — The document CSP may stop browsers rendering served PDFs (uncertain) `[Correctness + Intent & Structure]`

- **Where:** `internal/present/server.go:81` (applied at :372)
- **What:** `documentCSP` (`default-src 'none'` plus a bare `sandbox`) is applied to every served document including `.pdf`.
- **Why it matters:** Chromium-family browsers' built-in PDF viewer may refuse to render a sandboxed PDF, defeating the rung that exists to serve PDFs. Whether it does is browser behaviour that could not be checked. *(uncertain: serving a PDF with this header and opening it in Chrome would settle it)*
- **Fix:** Serve `.pdf` with a CSP the viewer accepts (no `sandbox` directive), or confirm in Chrome and Firefox and document the result.

### Medium — The model fingerprint digest changes when near-tied tokens swap rank `[Correctness]`

- **Where:** `internal/probe/modelfingerprint.go:117-123`
- **What:** `candidateDigest` is documented as a digest of the candidate-token set but hashes the candidates joined in server (rank) order.
- **Why it matters:** A rank swap among near-tied top tokens changes the `lp-` digest and `BehaviorSignature`, so `Save.Changed` reports a model change that did not happen. *(independently verified)*
- **Fix:** Sort the candidate tokens before hashing so the digest depends on the set, not the order.

## Recommended Action Order

1. Bound PDF nesting in `preflightPDF` (the only High; remote-triggerable process crash).
2. Fix `queryDiagnosed` to treat truncation as incomplete (small, and it closes a fail-open in the secrets guard).
3. Close the trust-gate holes: sanitise the adopt prompt, pin the MCP authorization server, extend the git denylist. All are small and local.
4. Replace the fixed deadlines (provider 5xx, update download, snapshot git, userexec drain) with condition waits or generous backstops, in one pass, since they share the same invariant.
5. Quick correctness wins: `run_tests` file paths, the nested-module vet, the Harmony header pattern, the landlock file-root mask, the heartbeat rebind retry, the fingerprint digest sort.
6. Fix the `wireTap` turn-counter race with an atomic mirror.
7. Needs design discussion first: the Auto fence's local-service escape (bwrap and seatbelt changes plus banner disclosure) and the adoption pin's coverage of launcher-read files.
8. Confirm the PDF CSP behaviour in a browser, then decide.

## What Looked Good

The machine checks were clean (`go vet`, and a race-enabled suite over every package), and independent verification refuted the one claimed defect in the daemon reload diff. The Floor guards, the hardened git read path, the session store and the Bubble Tea state handling produced no verified defects. Most verified problems are narrow edge cases rather than structural flaws.
