# Audit defects: crash, secrets, adoption, slow-box timeouts

**Goal:** Fix the self-contained defects the 2026-11-10 code audit filed: a PDF that kills the process, a secrets scan that passes a clipped diff, raw rule bytes in `apogee project adopt`, four slow-box deadlines, and five model-facing correctness bugs. Each item closes one capcom issue.
**Date:** 2026-11-10
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a5c4f2db

**Sources:**
- `docs/reviews/code-audit-2026-11-10.md`
- the issue files named on each item's `**Closes:**` line (`.capcom/issues/<id>.md`)
- AGENTS.md "A slow machine makes apogee slower, never broken"; ADR 0031 (Driver sufficiency), ADR 0051 (undo snapshots), ADR 0071 / 0076 (Bypass, Floor)

**Ratified design calls (owner, 2026-11-10):**
- **Fence escape:** `fence-escape-local-spawner` gets its own ADR-first plan; not in this one.
- **Secrets scan:** a truncated git stdout is `ErrIncomplete` everywhere, AND the commit scan streams the staged diff uncapped into the scanner.
- **Allow rule text:** shown through the adoption pane's sanitised spelling, AND config load refuses Allow rule text holding C0 controls, DEL or Unicode bidi controls, on every layer.
- **Loading server (ratified at the regression check, 2026-11-10):** the patient wait (1 s backoff doubling to an 8 s cap, 10-minute backstop, ctx-cancellable) fires ONLY on llama-server's loading reply (a 503 whose body is the "Loading model" / unavailable_error reply); every other 503 and 5xx keeps the existing short ladder. The wait is skipped when the bound server entry names a non-empty `fallback:` list (ADR 0105 decision 3-4: the `fallback-after:` grace governs) — never "a fallback resolver is installed", which the TUI always is (wire_live.go) — and when maxRetries is 0. Every Driver shows a one-time "still loading" status line.
- **JSX apostrophes (ratified at the regression check, 2026-11-10):** in .jsx/.tsx only a word-internal `'` (between letters: Don't, it's) is inert; there is no after-tag-closing-`>` clause. The rule keys on the .jsx/.tsx extension threaded from `Check`, never on the language name (Language returns "javascript"/"typescript" for .js/.ts too).

**Planner calls (2026-11-10):**
- **Update download:** no total timeout; 60 s response-header bound and 60 s body-idle bound.
- **Snapshot git:** cheap reads keep 60 s; `add -A` / `write-tree` get a 30-minute wedge backstop.
- **Wire tap Turn:** a background child's WireEvent carries the top-level Turn index current when it is observed; documented on `WireEvent`.

**Regression check (2026-11-10, a5c4f2db):**
- 1: guard folded
- 2: guard folded (lands back to back with item 3)
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 7: recast; yields to ADR 0105 decision 3-4 (`fallback-after:` grace)
- 8: guard folded; follows ADR 0075 decision 4 as amended (adr-0075-amend-1102)
- 9: guard folded
- 10: guard folded
- 11: guard folded
- 12: guard folded; yields to the `goVetPins` / `runGoVet` doc (a dependency-resolution failure stays a vet finding)
- 13: guard folded
- 14: recast
- 15: guard folded
- 16: guard folded
- 7 (round 2): recast — owns the per-call wait-start provider Delta item 8 consumes; skip predicate is the bound entry's non-empty `fallback:` list; Goal restated to the loading reply; error body peeked and restored; supersedes the client.go Client "Retry policy" and WithMaxRetries doc comments for the loading reply
- 8 (round 2): guard folded — consumes item 7's wait Delta; the provider entry leaves its Files
- 14 (round 2): recast — JSX: word-internal `'` only, keyed on .jsx/.tsx; supersedes the hasHashComments "PHP is deliberately absent" comment (syntaxcheck.go)

**Standing requirements:**
- skills: coding-standards
- Tests run one package and named tests (`go test -race -count=1 -run '<names>' ./<pkg>/`); never a package pattern.
- A deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `fence-escape-local-spawner`, `oauth-secret-unpinned-issuer`, `served-pdf-csp-sandbox` (needs a browser), `landlock-file-root-einval`, `rebind-failure-never-retried`, `candidate-digest-rank-order`.
- The parked architecture-review issues (2026-11-10 #1–#10).
- Any version bump.

## 1. Refuse deeply nested PDFs before the parser recurses — ✅ DONE (2026-11-10)

NOTES (2026-11-10): page content streams are depth-scanned at the walk through the parser's own `Value.Reader` (`pageContentNesting`, the plan's stated alternative), not in the preflight. This also covers a page dictionary compressed into an object stream, an indirect /Length, /Filter or /DecodeParms, and a /Contents array object, none of which the raw scan sees. The preflight (`refuseDeepNesting`) scans the object bytes, every /ObjStm, every /CMap, and every stream a /ToUnicode names, including names found inside a decoded object stream.
NOTES (2026-11-10): the depth scan is iterative, but it keeps a bounded stack of at most 64 typed frames rather than a bare counter. A counter in which every closer decrements can be bypassed with `[ >> [ >> …` and `<< /A >> /B << /A >> …`, because the parser reads a `>>` in an array, or in a dictionary's value position, as null. The scan models those positions, and a frame closes only where the parser closes one.
NOTES (2026-11-10): the item went beyond its literal approach in two places. The decode for the depth scan models the parser's PNG Up predictor (/Predictor 12), because without it a predicted stream hides its `[` bytes from the scan. In the program mode (content/CMap) the scan also stops at the first byte the lexer refuses, as Interpret does, so an inline image's binary is never counted.
NOTES (2026-11-10): "the package comment" in the Goal is read as ExtractPDF's doc comment, which carried the "nothing escapes this function" claim. That comment now states the guarantee, and doc.go is untouched.
NOTES (2026-11-10): known limits of the guarantee, stated in ExtractPDF's doc. A /Predictor or /DecodeParms given by reference reads as unpredicted. Each decoded stream is held in memory for its scan, up to the 64 MiB inflate budget.
NOTES (2026-11-10): pre-existing debt the work exposed: `/Columns +99999999` (with a sign) gets past the pdfMaxPredictorColumns guard, because declaredIntegers reads digits only, and the parser's pngUpReader then allocates rows of that width.
NOTES (2026-11-10): retry fix: a syntax stream (an /ObjStm, a /CMap or a /ToUnicode target) whose /Filter is a reference, or an array holding a reference among its names, is now depth-scanned through the plain chains too (no filter and ASCII85Decode alone, up to skipEnd) as well as the inflating unread chains (`pdfUnreadSyntaxChains`, `pdfFilterReferenced`). A stream with an unreadable dictionary gains the ASCII85Decode-alone chain the same way. Pinned by three new cases: a /ToUnicode CMap whose /Filter 7 0 R is null, one whose /Filter [7 0 R] is /ASCII85Decode, and an /ObjStm whose /Filter 7 0 R is null. A throwaway probe confirmed each one passes the preflight without the fix. readPDFPredictor's 161-character doc line is rewrapped.
NOTES (2026-11-10): known limit: a referenced /Filter that resolves to a chain of several plain filters (ASCII85Decode twice) is not modelled.

**What:**
**Goal:** `doctext.ExtractPDF` returns a model-facing unreadable-PDF failure, and the process survives, for a PDF whose object bytes or any decoded content stream nest `[` / `<<` deeper than 64; the package comment states what is guaranteed.
**Approach (assumed at the header base):** in `preflightPDF` scan bytes outside stream bodies for `[`/`<<` depth (skipping strings and comments as the preflight lexer does) and refuse past 64 with `pdfUnreadableFormat`; apply the same depth scan to every content stream the preflight's inflation measurement decodes. `pdfMaxNesting` becomes that refusal bound, not a lexer give-up. Fix the "nothing escapes this function" wording to the guarantee. Fix for `pdf-parser-unbounded-nesting`: `github.com/ledongthuc/pdf` recurses unbounded and a Go stack overflow is not recoverable.
**Regression guard.** Depth-scan only the streams the parser reads as PDF syntax — those a `/Contents` or `/ToUnicode` names (`decodedReferences`), plus `/ObjStm` and `/CMap` — never `/XRef` rows or other charged-but-binary streams (an untyped FontFile2 full of `0x5B` bytes must still extract), or scan each page's `/Contents` through the parser's `Value.Reader` before `GetPlainText`. Scan a selected stream whatever its chain: one with no inflating chain over its raw (or ASCII85-decoded) body, up to `skipEnd`. Keep the `readDictionary`/`readArray` give-up at `pdfMaxNesting` (the preflight lexer runs before any depth refusal) and write the new depth scan as an iterative counter; the constant may be shared.
**Files:** internal/doctext/pdf.go, internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdf.go — preflightPDF, chargesInflation, decodedReferences, pdfDecodeChains, withoutStreamBodies, pdfLexer.readDictionary;
  ledongthuc/pdf ps.go — Interpret; internal/doctext/pdf_test.go — hostilePDF
**Closes:** pdf-parser-unbounded-nesting
**Tests:** a trailer with ~5 MB of `[` and a FlateDecode page stream inflating to deep `[` / `<<` each return the failure; an unfiltered `/Contents` stream of deep `[` returns the failure; a big binary untyped stream full of `[` still extracts; a normal fixture still extracts.
**Acceptance:** `go test -race -count=1 -run 'PDF' ./internal/doctext/`
**Commit:** `fix(doctext): refuse deeply nested PDFs before the parser recurses`

## 2. A truncated git stdout is an incomplete answer — ✅ DONE (2026-11-10)

NOTES (2026-11-10): the truncation check sits after the exit-status check in queryDiagnosed: a non-zero exit stays the plain exit error (git's own answer) even over a cut stdout; pinned by TestHost_RunReportsANonZeroExitOverATruncatedStdout.
NOTES (2026-11-10): consequential edit — internal/tools/git.go: made necessary by the truncation becoming a failure; RunGitQuery's comment listing every failure kind now names "a stdout past the output cap".
NOTES (2026-11-10): caller audit — Run/Query/RunDiagnosed callers are snapshot store (init, write-tree, rm --cached, add -A: none print stdout near the cap), treesnapshot runProbe (rev-parse; a truncation counts as one transient miss) and secretsguard runShadowGit (any error is the incomplete-scan path, as the plan expects until item 3); none needed a code change.

**What:**
**Goal:** `gitexec.Host` `Query`, `Run` and `RunDiagnosed` never return a capped (truncated) stdout as success: they return an error matching `ErrIncomplete` naming the cap.
**Approach (assumed at the header base):** in `queryDiagnosed` map `res.Truncated` to an `incompleteError` ("git <sub>: output exceeded <cap> bytes"), as `probeGit` does. Grep every caller of `Query`/`Run`/`RunDiagnosed` (treesnapshot, snapshot store, footer) and confirm each handles the new error; fix any that would now misreport. Items 2 and 3 land back to back: item 2 alone makes a >256 KiB staged diff take the commit-secrets Tier-2 (incomplete-scan) look, even in auto, which item 3 lifts — never cut a release between them.
**Regression guard.** Reword every comment that lists what `ErrIncomplete` covers (`ErrIncomplete`, `Host.Run`, internal/gitexec/doc.go; find them with `grep -rn 'ErrIncomplete\|complete answer' internal/`) to add "overran the output cap", and drop the next-call-may-answer / leave-it-uncached promise for that case: a truncated stdout is the same on every retry. Items 2 and 3 land back to back; item 2 alone makes a >256 KiB staged diff take the Tier-2 look, which item 3 lifts.
**Files:** internal/gitexec/gitexec.go, internal/gitexec/gitexec_test.go, internal/gitexec/doc.go
**Read first:** internal/gitexec/gitexec.go — queryDiagnosed, probeGit, incompleteError, ErrIncomplete, Host.Run; internal/gitexec/gitexec_test.go — fakeHost;
  internal/agent/treesnapshot.go — treeSnapshotter.runProbe; internal/snapshot/store.go — Store.settleWarnings
**Tests:** fake launcher setting `Truncated` → `errors.Is(err, ErrIncomplete)` for each of the three entry points.
**Acceptance:** `go test -race -count=1 ./internal/gitexec/`
**Commit:** `fix(gitexec): report a truncated git stdout as incomplete`

## 3. The commit secrets scan reads the whole staged diff — ✅ DONE (2026-11-10)

NOTES (2026-11-10): streaming scanner API is `security.NewSecretScanner()` → `*SecretScanner` (an io.Writer for the diff), `NameListWriter() io.Writer` for the `--name-only -z` listing, and `Findings()`, which ends both streams; a write after `Findings` fails. `SecretFindings(diff, paths)` now runs on that scanner, so its order (path findings first) and its one-finding-per-(path, class) dedup are shared code, not a copy. A clean scan still returns nil.
NOTES (2026-11-10): budget guard: took the first option. `commitSecretsTimeout` stays one 30 s bound on the whole check. Its doc comment says that a diff too large to read within it gets the incomplete-scan approval prompt, and accepts that cost. `TestCommitSecretsIncompleteScanForcesApproval/a_diff_the_budget_cannot_finish_reading` pins it. ADR 0080 is not amended: no stall bound was added.
NOTES (2026-11-10): the two streaming runs go through `Host.RunTo`, which resolves git again through the same fence. The old `nulFields` helper is deleted because nothing calls it now. The listing test wraps the scripted `Spawn` in `cappedSpawn`, which cuts output the way the real capped launcher does. Without that, the scripted listing passes on the old code too. Both new past-the-cap subtests were confirmed to fail on the old implementation.

Depends on item 2.

**What:**
**Goal:** `scanStagedSecrets` finds a secret staged past the first 256 KiB of the diff; when the scan cannot finish it reports incomplete (Tier-2 look), never clean.
**Approach (assumed at the header base):** run `git diff --cached` through `Host.RunTo` into a writer that feeds the secret scanner incrementally (no whole-diff buffer); the `--name-only -z` listing streams the same way. Any capped or incomplete answer surfaces as the existing scan-could-not-finish path. Fix for `staged-diff-scan-clipped`.
**Regression guard.** Give the `failingDiff` host in `TestCommitSecretsIncompleteScanForcesApproval` ("a git failure after the repository resolved") a `SpawnTo` that fails `diff ` (shape: `scriptedGit`, treesnapshot_test.go), or it falls through to the real git and goes red. The incremental feed is a new exported streaming scanner in `internal/security` (name its API); `SecretFindings`'s order (path findings first) and once-per-(path, class) dedup carry over byte for byte. `commitSecretsTimeout` (30 s, the whole check) now bounds the full diff: either state and test that an over-budget diff is the incomplete look (an accepted cost), or make it a stall bound reset per chunk the scanner consumes and amend ADR 0080's `adr-0080-amend-0923` by name.
**Files:** internal/agent/secretsguard.go, internal/agent/secretsguard_test.go, internal/security/secrets.go, internal/security/secrets_test.go
**Read first:** internal/agent/secretsguard.go — scanStagedSecrets, runShadowGit, commitSecretsTimeout; internal/security/secrets.go — SecretFindings, secretContentPatterns;
  internal/gitexec/gitexec.go — Host.RunTo; internal/agent/secretsguard_test.go — TestCommitSecretsIncompleteScanForcesApproval; internal/agent/treesnapshot_test.go — scriptedGit
**Closes:** staged-diff-scan-clipped
**Tests:** a >256 KiB staged diff with a key after the cut is found; a listing past the cap is not reported clean; `TestCommitSecretsIncompleteScanForcesApproval` stays green with `failingDiff` failing `diff ` on `SpawnTo`; the streaming scanner matches `SecretFindings`'s order and dedup; an over-budget diff behaves as the guard chose.
**Acceptance:** `go test -race -count=1 -run 'Secret' ./internal/agent/` then `go test -race -count=1 -run 'Secret' ./internal/security/`
**Commit:** `fix(agent): scan the whole staged diff for secrets`

## 4. `apogee project adopt` shows Allow rules through the sanitised name — ✅ DONE (2026-11-10)

**What:**
**Goal:** the CLI adoption question and the "no longer holds" refusal write no C0, DEL or ESC byte from repo-supplied text, and name an Allow rule with the same spelling as the adoption pane.
**Approach (assumed at the header base):** in `cmd/apogee/project_cmd.go` build the Allow-rule branch of `printProjectQuestion` from the sanitised `tui.AdoptionQuestion` title, and make `heldName` use `tui.AdoptionEntryName` for every kind. Fix for `adopt-cli-raw-rule`.
**Regression guard.** `heldName` returns `tui.AdoptionEntryName(p)` for every kind and appends " as it was offered" only when `p.Handle != ""`, so the "no longer holds `go test`, so it was not adopted." that `TestProjectCmdAdoptRefusesARuleTheFileNoLongerHolds` pins stays green; rewrite `printProjectQuestion`'s doc ("An Allow rule is one line, its kind and text") to the title it now prints. Item 4's tests call `printProjectQuestion` and `heldName` directly (item 5 refuses those bytes at load).
**Files:** cmd/apogee/project_cmd.go, cmd/apogee/project_cmd_test.go
**Read first:** cmd/apogee/project_cmd.go — printProjectQuestion, heldName; internal/tui/adoption.go — AdoptionEntryName, proposalName, flatText;
  internal/tui/adoptionbody.go — AdoptionQuestion; cmd/apogee/project_cmd_test.go — TestProjectCmdAdoptRefusesARuleTheFileNoLongerHolds, TestProjectCmdAdoptAnswersEachProposedRule
**Closes:** adopt-cli-raw-rule
**Tests:** a rule holding `\r` and an ESC sequence through `printProjectQuestion` and `heldName`: no control byte in output; spelling equals `tui.AdoptionEntryName`; `TestProjectCmdAdoptRefusesARuleTheFileNoLongerHolds` stays green.
**Acceptance:** `go test -race -count=1 -run 'Project' ./cmd/apogee/`
**Commit:** `fix(project): print adoption rules through the sanitised name`

## 5. Config load refuses Allow rules holding control characters — ✅ DONE (2026-11-10)

NOTES (2026-11-10): consequential edit — docs/manual/configuration.md: made necessary by the new load refusal (the Allow-rules paragraph listed only the blank-rule refusal)

**What:**
**Goal:** loading any config layer whose Allow rule text holds a C0 control, DEL or a Unicode bidi control fails with an error naming the layer and the rule index; ordinary rules load as before.
**Approach (assumed at the header base):** extend the validation in `internal/config/allow.go` `rules()` (which today refuses only empty entries); one predicate for the refused set. Defence in depth for `adopt-cli-raw-rule` (item 4 closes it).
**Regression guard.** Put the one refused-set predicate in `checkAllowRule` (internal/config/projectwrite.go) too, so `AddProjectAllowRule` / `RemoveProjectAllowRule` refuse what load refuses. `rules()` is always called with `SourceGlobal`, so the error itself adds only the character class and the `allow.<kind>` entry index; the layer is named by the existing wrappers (the global load error, `projectSkippedNotice`'s path — the project case is the "project layer is skipped" notice, as `TestAllowRulesRefuseABlankRule` pins).
**Files:** internal/config/allow.go, internal/config/allow_test.go, internal/config/projectwrite.go, internal/config/projectwrite_test.go
**Read first:** internal/config/allow.go — allowConfig.rules, fileAllowRules; internal/config/projectwrite.go — checkAllowRule, AddProjectAllowRule;
  internal/config/layer.go — checkProjectLayer, projectSkippedNotice; internal/sanitize/sanitize.go — BidiControl; internal/config/allow_test.go — TestAllowRulesRefuseABlankRule
**Tests:** `\r`, ESC, DEL, U+202E each refused; a tab-free ordinary rule and a non-ASCII printable rule accepted; `AddProjectAllowRule` refuses a rule holding U+202E; a project file holding one gets the "project layer is skipped" notice.
**Acceptance:** `go test -race -count=1 -run 'Allow' ./internal/config/`
**Commit:** `fix(config): refuse control characters in Allow rule text`

## 6. A command that exited 0 with a held pipe ran — ✅ DONE (2026-11-10)

**What:**
**Goal:** `userexec.Run` returns nil error and the command's exit code, with captured stdout/stderr kept, when the command exited but a child it spawned still holds a pipe after `WaitGrace`; non-zero exits and the deadline case are unchanged.
**Approach (assumed at the header base):** treat `errors.Is(runErr, exec.ErrWaitDelay)` as ran-to-status, as `internal/subprocess` does with `DrainWedged`; add a `Result` flag noting output may be cut; update the `Run` doc comment. Fix for `held-pipe-misread-unrun` (api-key-cmd, keystore helper, Reaction `run:`).
**Files:** internal/userexec/userexec.go, internal/userexec/userexec_test.go
**Read first:** internal/userexec/userexec.go — Run, Result, WaitGrace; internal/platform/teardown.go — RunWithTeardown;
  internal/userexec/userexec_test.go — TestRunReportsTheDeadlineRatherThanWaitingOnASleep; internal/userexec/userexec_unix_test.go — TestRunKillsAWrappersGrandchildOnTimeout;
  internal/keystore/run.go — userexec.Run caller; internal/reactions/command.go — userexec.Run caller
**Closes:** held-pipe-misread-unrun
**Tests:** script backgrounding `sleep` on inherited stderr then `exit 0` → nil, code 0, stdout kept, within WaitGrace + margin; same with `exit 3` → code 3; deadline → TimedOut.
**Acceptance:** `go test -race -count=1 ./internal/userexec/`
**Commit:** `fix(userexec): a command that exited is not "could not run" when a child holds its pipe`

## 7. The provider waits out a loading server's 503s

**What:** Recast at the regression check (2026-11-10).
**Goal:** a 503 whose body is the llama-server loading reply is retried inside `send` with 1 s backoff doubling to an 8 s cap until it succeeds, ctx is cancelled, or 10 minutes pass; any other 503/5xx, maxRetries 0, or a seat whose bound entry names `fallback:`, retries as at base.
**Approach (assumed at the header base):** in `internal/provider/client.go` `send`, a separate 503 branch beside the `maxRetryAfter` logic; constants named beside `defaultMaxRetries`; a test seam for the clock/backstop. The final 503 after the backstop surfaces as today (`statusDelta`). Each loading-reply wait start is yielded as a new per-call provider Delta kind (once per call, at wait start, via rec/yield in stream.go); item 7 owns that Delta, and item 8 only consumes it. Fix for `loading-server-503-giveup` (slow-box).
**Regression guard.** Ratified design call (owner, 2026-11-10) — replaces the header's "Loading server" call: the patient wait (1 s backoff doubling to an 8 s cap, 10-minute backstop, ctx-cancellable) fires ONLY on llama-server's loading reply (a 503 whose body is the "Loading model" / unavailable_error reply); every other 503 and 5xx keeps the existing short ladder. The wait is skipped when the bound server entry names a non-empty `fallback:` list (ADR 0105 decision 3-4: the `fallback-after:` grace governs; item yields to it) and when maxRetries is 0 (WithMaxRetries(0) = one POST; pin beside TestStatusError_EmptyBodyNamesTheStatus and keep TestTitleGeneratorDoesNotRetry green). On Stream the pre-header idle timer is reset on each loading reply (a reply is not idle), so the 10-minute backstop is the bound; past it the final 503 surfaces through statusDelta as today. Record one attempt (DeltaAttempt) per wait, not one per poll. TestTUIFallbackOnAnEntryWithoutFallbackIsAbandoned stays green unchanged. Item 7 owns the wait signal: the provider emits a per-call Delta when a loading-reply wait starts (the signal item 8 consumes — replace the Approach's "observer hook" wording so items 7 and 8 agree), and the skip predicate is "the bound server entry names a non-empty `fallback:` list", never "a fallback resolver is installed" (the TUI always installs one: wire_live.go). The predicate is carried per call to `send` (a Request field or ctx), answered for the bound entry in cmd/apogee/fallback.go. Spotting the loading reply peeks at most `maxErrorBodyBytes` of the 503 body and restores it (`resp.Body = io.NopCloser(io.MultiReader(peeked, rest))`) on any response `send` returns, so `statusDelta` / `statusError` still quote the "Loading model" text. Supersedes, for the loading reply only, the client.go Client "Retry policy" doc comment and the WithMaxRetries "up to 3 attempts" comment: update both.
**Files:** internal/provider/client.go, internal/provider/client_test.go, internal/provider/stream.go, internal/provider/attempt.go, internal/provider/reliability_test.go, internal/agent/loop.go (carries the predicate per call), cmd/apogee/fallback.go (answers it for the bound entry)
**Read first:** internal/provider/client.go — send, isRetryableStatus, retryDelay, drain, sleepCtx; internal/provider/stream.go — Stream (headersCtx, newIdleTimer, rec.headers), statusDelta; internal/provider/attempt.go — newAttemptRecorder, attemptRecorder.begin, abandon;
  cmd/apogee/wire_live.go — newSessionFallback / engine.fallback; cmd/apogee/fallback.go — sessionFallback.resolver, newSeatResolver; internal/agent/loop.go — consultFallback, streamResponse DeltaAttempt case;
  internal/provider/reliability_test.go — TestRespond_RetriesThenSucceeds, TestStatusError_EmptyBodyNamesTheStatus; cmd/apogee/title.go — namingCall
**Tests:** scripted upstream loading-reply 503×N (past 0.6 s) then 200 completes; a loading reply past the (scaled) backstop faults through `statusDelta` and its message still quotes the "Loading model" body text; cancel mid-wait returns at once; 500 and a non-loading 503 keep 2 retries (`TestRespond_RetriesThenSucceeds` green unchanged); `WithMaxRetries(0)` makes one POST of a loading reply (beside `TestStatusError_EmptyBodyNamesTheStatus`); a seat whose bound entry names a non-empty `fallback:` list does not wait, while a TUI seat on an entry with no `fallback:` (a resolver is installed regardless) does wait; the wait-start Delta is yielded once per call; on Stream a loading reply resets the pre-header idle timer; one `DeltaAttempt` per wait. `TestTitleGeneratorDoesNotRetry` and `TestTUIFallbackOnAnEntryWithoutFallbackIsAbandoned` stay green unchanged.
**Acceptance:** `go test -race -count=1 -run 'Retry|503|StatusError' ./internal/provider/` then `go test -race -count=1 -run 'Loading' ./internal/agent/` then `go test -race -count=1 -run 'Loading|TestTitleGeneratorDoesNotRetry|TestTUIFallbackOnAnEntryWithoutFallbackIsAbandoned' ./cmd/apogee/`
**Commit:** `fix(provider): wait out a local server that is still loading its model`

## 8. Every Driver shows a "server still loading" status

Depends on item 7.

**What:**
**Goal:** when a model call starts waiting on a loading server's 503, the engine emits one event per model call whose Notice reads `<server> is still loading its model (HTTP 503) — waiting up to 10m`, and the TUI and headless output show it.
**Approach (assumed at the header base):** a new `domain` event modelled on `UpstreamSwitchedEvent` (EventBase, Server, Model, Notice); the agent emits it from item 7's wait-start Delta once per model call. Every consumer of `UpstreamSwitchedEvent` gets the new event beside it — rule: `grep -rn UpstreamSwitchedEvent internal cmd` (eventjson encode/decode, TUI notice, headless). Wire-silent engine (ADR 0031) holds: observation only, nothing reaches history.
**Regression guard.** The Notice fires only for item 7's loading-reply wait; wording `<server> is still loading its model (HTTP 503) — waiting up to 10m` holds because item 7 makes the 10-minute backstop the Stream bound. A host that names no server reads `this server is still loading its model …` (as loop.go words an unnamed server). The wait signal travels per call as a provider Delta through Stream's yield (as `DeltaAttempt` reaches loop.go, compact.go, collect.go), emitted with the calling Agent's `base(turn)` — never a Client-level observer, which unrouted children share. The new event reaches every Driver render/encode site (`grep -rn 'UpstreamSwitchedEvent' internal cmd`), excluding the seat-move handlers by name (run.go `noteSwitch`, tui `followFallback` and `noteWorkflowSessionSwitch`, agent/rebind.go), where it does nothing. Follows ADR 0075 decision 4 as amended (adr-0075-amend-1102): the new line kind lands with a dated ADR 0075 amendment naming the new count, and docs/manual/headless.md's "twenty-nine line kinds" becomes thirty.
**Files:** internal/domain/events.go, internal/agent/construct.go, internal/agent/loop.go, internal/agent/compact.go, internal/agent/collect.go, internal/eventjson/encode.go, internal/eventjson/encode_test.go, internal/eventjson/decode.go, internal/eventjson/decode_test.go, internal/tui/ (the UpstreamSwitchedEvent render site), internal/tui/fold_test.go, cmd/apogee/ (headless render site), cmd/apogee/daemonfire.go, internal/run/transcript.go, docs/manual/headless.md, a new ADR 0075 amendment Note under .capcom/knowledge/decisions/adr-0075/
**Read first:** internal/domain/events.go — UpstreamSwitchedEvent.Notice; internal/agent/loop.go — emitAttempt, streamResponse DeltaAttempt case; internal/eventjson/encode.go — Kinds;
  internal/eventjson/decode.go — upstreamSwitchedEventFrom; cmd/apogee/headless.go — narrationSink.narrate; internal/tui/fold.go — fallbackNote; cmd/apogee/daemonfire.go — daemonSwitchLine
**Closes:** loading-server-503-giveup
**Tests:** agent test: scripted loading-reply 503 then 200 emits exactly one event with that Notice; the unnamed-server wording; eventjson round-trip; TUI frame shows the line; `TestEncodeMapsEveryEventVariant`, `TestDecodeRoundTripsEveryKind`, `TestFoldEventCoversEveryEventVariant` and `TestManualListsEveryEventLineKind` green with the new kind.
**Acceptance:** `go test -race -count=1 -run 'Loading|RoundTrip|EveryEventVariant|RoundTripsEveryKind' ./internal/eventjson/` then `go test -race -count=1 -run 'Loading|RoundTrip' ./internal/agent/` then `go test -race -count=1 -run 'Loading|EveryEventVariant' ./internal/tui/` then `go test -race -count=1 -run 'TestManualListsEveryEventLineKind' ./cmd/apogee/`
**Commit:** `feat(agent): tell the user when a server is still loading its model`

## 9. Snapshot git bounds a wedge, not the work

**What:**
**Goal:** a snapshot capture whose `add -A` takes longer than 60 s completes; a wedged git still ends at a backstop; a deadline failure does not chain into a second full `captureFresh`; a capture killed mid-add leaves the next capture working (no stale `index.lock`).
**Approach (assumed at the header base):** in `internal/snapshot/store.go` split `snapshotTimeout` into a 60 s bound for cheap reads (`ls-tree`, `cat-file`, `Content`, `stream`) and a 30-minute wedge backstop for `add -A` / `write-tree` in `stageAndWrite` and `settleWarnings`; `Capture` skips `captureFresh` when the add failed on its deadline; remove a stale `index.lock` in the store's private git dir before an add. Fix for `snapshot-git-fixed-deadline` (slow-box).
**Regression guard.** Removing a stale `index.lock` before an add is safe only because every Capture runs under `undo.Journal.mu` (journalSource is the Store's one capturing caller) plus the session hold (cmd/apogee/undo.go `runUndoVerb`): name that serialiser and amend the `Store` doc ("git's own index lock serialises concurrent captures") and the `Capture` doc in the same commit. Pick the bound by the work, not the helper: work-tree walks (`ls-files --deleted|--others`, `rm --cached` in `dropFromIndex`) take the 30-minute backstop; only `ls-tree` / `cat-file` / `diff-tree` keep 60 s.
**Files:** internal/snapshot/store.go, internal/snapshot/store_test.go
**Read first:** internal/snapshot/store.go — snapshotTimeout, Store.Capture, stageAndWrite, captureFresh, settleWarnings, dropFromIndex, streamWith;
  internal/undo/snapshot.go — Journal.MarkPre
**Tests:** slow-git fake past the read bound completes (an `add -A` and an `ls-files` walk alike); deadline error → no `captureFresh` call; planted `index.lock` → next capture succeeds; never-exiting fake ends at the (scaled) backstop.
**Acceptance:** `go test -race -count=1 ./internal/snapshot/`
**Commit:** `fix(snapshot): bound a wedged git, not a large workspace`

## 10. A failed pre-image capture is not retried on every write call

Depends on item 9.

**What:**
**Goal:** after `Journal.MarkPre` fails in an Exchange, later write-capable tool calls in that Exchange do not re-run the capture; the next Exchange tries again.
**Approach (assumed at the header base):** `internal/undo` `Journal` records the failure per Exchange and `MarkPre` returns the recorded error without a git call until the Exchange ends; caller `internal/agent/dispatch.go` before `runTool` unchanged except surfacing the error once. Closes `snapshot-git-fixed-deadline` with item 9.
**Regression guard.** Record a failure only when the capture's ctx is still live (`ctx.Err() == nil`): a human stop of a delegated child cancels `childCtx` mid-capture, and that failure must not leave the rest of the Exchange funnel-only. Surface the error once: either `MarkPre` returns nil on a replay (documented), or internal/agent/dispatch.go (which emits `ErrorEvent{Source:"undo"}` on every non-nil `MarkPre`) changes, with an undo_group_test.go case pinning one `undo` ErrorEvent for two write calls.
**Files:** internal/undo/snapshot.go, internal/undo/snapshot_test.go; internal/agent/dispatch.go and internal/agent/undo_group_test.go when the guard's dispatch branch is taken
**Read first:** internal/undo/snapshot.go — Journal.MarkPre; internal/undo/journal.go — Journal.openGroup, closeGroup; internal/agent/dispatch.go — journal.MarkPre call site;
  internal/agent/childrun.go — runChild; internal/undo/snapshot_test.go — fakeSnapshotter;
  internal/agent/undo_group_test.go — failingSnapshotter, TestACaptureFailureIsReportedAndNeverFailsTheExchange
**Closes:** snapshot-git-fixed-deadline
**Tests:** failing store → two MarkPre calls in one Exchange hit the store once; a new Exchange calls it again; a cancelled-ctx failure, then a live `MarkPre` in the same Exchange, captures; two write calls yield one `undo` ErrorEvent.
**Acceptance:** `go test -race -count=1 ./internal/undo/` then, when dispatch.go changes, `go test -race -count=1 -run 'Capture' ./internal/agent/`
**Commit:** `fix(undo): do not retry a failed pre-image capture on every write call`

## 11. `apogee update` bounds a stall, not a slow download

**What:**
**Goal:** a release archive download that keeps moving completes however long it takes; one that stops sending for 60 s fails with an error naming the stall; size ceilings and the SHA256SUMS check are unchanged.
**Approach (assumed at the header base):** in `internal/update/stage.go` drop `Client.Timeout` from `newDownloadClient`; set `Transport.ResponseHeaderTimeout` (60 s) with dial/TLS timeouts; `download` wraps the body in an idle-deadline reader reset on each successful read; the idle bound is a test knob. Fix for `update-download-wall-clock` (slow-box).
**Regression guard.** Make the bite structural: assert `newDownloadClient()` has `Timeout == 0` and a non-zero `Transport.ResponseHeaderTimeout`, then trickle the body over several idle bounds with the idle knob lowered (the old 2-minute total has no knob, so a scaled trickle cannot bite). Write the knob only from a test without `t.Parallel` (the `lowerCommitSecretsTimeout` pattern, restored in `t.Cleanup`) — stage_test.go runs 11 parallel `Stage` tests — or pass the bound as a `download` / `newDownloadClient` parameter.
**Files:** internal/update/stage.go, internal/update/stage_test.go
**Read first:** internal/update/stage.go — newDownloadClient, download, Stage, downloadTimeout; internal/update/stage_test.go — newAssetServer, TestStage_PublishedArchive_ReturnsVerifiedBinary;
  internal/testtime/testtime.go — Budget, Eventually
**Closes:** update-download-wall-clock
**Tests:** `newDownloadClient()` has `Timeout == 0` and a non-zero `ResponseHeaderTimeout`; a body trickled over several (lowered) idle bounds completes; mid-body stop fails within the idle bound naming the stall; ceiling and checksum tests still pass.
**Acceptance:** `go test -race -count=1 ./internal/update/`
**Commit:** `fix(update): end a stalled download, not a slow one`

## 12. `diagnostics` vets a file in its own module

**What:**
**Goal:** `diagnostics` on a clean `.go` file in a nested module (root has another module, or no `go.mod`) reports clean; a real vet finding in a nested module is still reported; a module-resolution failure is reported as `go vet skipped: <reason>`, never as findings.
**Approach (assumed at the header base):** in `internal/tools/diagnostics.go` `goVetSpec` / `runGoVet` run vet with `Dir` at the nearest `go.mod` directory inside the workspace (the walk `run_tests` uses), keeping the GOWORK/GOTOOLCHAIN/GOENV pins; fix the test helper comment "a normal workspace is always a module". Fix for `diagnostics-vet-nested-module`.
**Regression guard.** When no `go.mod` sits between the file's directory and the root, keep `Dir=root` as today (a workspace opened in a module's subdirectory vets fine because go walks up); skip only on the go command's own module errors ("go.mod file not found", "does not contain package"), classified from vet's output. The item yields to the `goVetPins` / `runGoVet` doc: a dependency-resolution failure stays a vet finding. `TestDiagnostics_VetRunsThroughTheHost`, `TestDiagnostics_UnfinishedVetIsReportedAsSkipped` and `TestDiagnostics_VetSubprocessEnvironmentIsPinned` stay green (the last's Dir/argv assertion changes only if `goVetSpec`'s argv changes form).
**Files:** internal/tools/diagnostics.go, internal/tools/diagnostics_test.go
**Read first:** internal/tools/diagnostics.go — goVetSpec, runGoVet, goVetPins; internal/tools/run_tests.go — detectTestRunner, markerFileExists;
  internal/tools/diagnostics_test.go — writeGoModule, TestDiagnostics_VetSubprocessEnvironmentIsPinned, TestDiagnostics_VetRunsThroughTheHost
**Closes:** diagnostics-vet-nested-module
**Tests:** `sub/proj/go.mod` under a root module; `checkouts/x/go.mod` with no root module; nested-module real finding still reported; a fake host returning go's "go.mod file not found" / "does not contain package" stderr with exit 1 → ok result, skip note, no "go vet checked" line; the three tests the guard names stay green.
**Acceptance:** `go test -race -count=1 -run 'Diagnostics|GoVet' ./internal/tools/`
**Commit:** `fix(tools): run go vet in the file's own module`

## 13. `run_tests` maps a Go file path to its package

**What:**
**Goal:** `run_tests` with `path` naming a Go file runs `go test` on that file's package directory, never a `<file>/...` pattern.
**Approach (assumed at the header base):** in `internal/tools/run_tests.go` the Go runner (`goTestRunner.args` / `goPackagePattern`) maps a file target from `subtree()` to `./<dir>`; other runners keep the file. Fix for `run-tests-go-file`.
**Regression guard.** A Go file in the marker directory itself (`foo_test.go` at the module root, or `sub/proj/foo_test.go`) maps to `.`, never `goPackagePattern("")` (`./...`); `runner.args` takes only a string, so state how the file/dir distinction reaches it (subtree's start, or a new arg updating all three runners and `TestRunTestsCommandConstructionPerRunner`). If item 12 reuses the nearest-go.mod walk, `goTestRunner.detect` stays unchanged. docs/manual/commands.md "Where run_tests runs" gains one clause: a Go file path runs that file's package.
**Files:** internal/tools/run_tests.go, internal/tools/run_tests_test.go, docs/manual/commands.md
**Read first:** internal/tools/run_tests.go — goTestRunner, goPackagePattern, relativeToMarker, RunTests.subtree, parentDir;
  internal/tools/run_tests_test.go — TestRunTestsRunsFromTheMarkerDirectory, TestRunTestsCommandConstructionPerRunner; docs/manual/commands.md — "Where run_tests runs"
**Closes:** run-tests-go-file
**Tests:** argv for `path=internal/foo/foo_test.go` pins `./internal/foo`; `path=foo_test.go` in the marker directory pins `.`; pytest/jest file targets unchanged; `TestRunTestsCommandConstructionPerRunner` green.
**Acceptance:** `go test -race -count=1 -run 'RunTests' ./internal/tools/`
**Commit:** `fix(tools): run_tests maps a Go file path to its package`

## 14. The syntax trailer stays silent on valid JSX, PHP, C# and Rust

**What:** Recast at the regression check (2026-11-10).
**Goal:** `syntaxcheck.Check` reports valid for: JSX/TSX text with an apostrophe, a PHP `# don't` comment, a C# `@"C:\temp\"` literal, a Rust `'('` char literal; existing true-positive vectors still fire; the package comment no longer speaks of a correction Turn.
**Approach (assumed at the header base):** in `checkBrackets`: no `'` string in JSX text (between `>` and `<`) for .jsx/.tsx; `'`/`"` string state resets at end of line except JS/TS backticks, Python triple quotes, C# raw/verbatim strings; PHP `#` is a comment unless followed by `[`; `\` does not escape the closing quote of C# `@"…"`; Rust `'x'` char literals inert. Fix for `syntax-heuristic-false-positives` (model-facing output on valid code).
**Regression guard.** Drop the general end-of-line reset of `'`/`"` string state — none of the four Goal shapes needs it; keep `"` state across lines for every language and honour backslash-newline. Ratified design call (owner, 2026-11-10) — in .jsx/.tsx only a word-internal `'` (between letters: Don't, it's) is inert; drop the after-tag-closing-`>` clause entirely. Thread the file extension from Check into checkBrackets (Language returns "javascript"/"typescript" for .js/.ts too, so the rule must key on .jsx/.tsx). Pin `if (ch>'9') return;` in a .tsx file as a valid vector, beside the valid vectors `onClick={() => window.open('https://example.com')}` and `fetchJson<User>('https://x')`. PHP: `#` is a comment only when followed by whitespace or EOL (same rule in stripTrailingComment); add `<style>.btn { color: #333; }</style>` in a .php file as a valid vector. Rewrite the hasHashComments comment that records PHP as deliberately absent (syntaxcheck.go, superseded: it is reversed by the `#`-followed-by-whitespace rule). Add valid multi-line vectors for PHP `"…IN (\n 1, 2\n)"`, a Rust multi-line string, a Swift `"""` block and a Java text block. Existing true positives (unclosed string literal, javascript unclosed string holding a slash) stay pinned unchanged. Prose rule: every comment in internal/syntaxcheck naming a correction Turn or retry is fixed (`grep -rn 'retry\|correction' internal/syntaxcheck/`).
**Files:** internal/syntaxcheck/syntaxcheck.go, internal/syntaxcheck/syntaxcheck_test.go, internal/tools/syntaxtrailer_test.go
**Read first:** internal/syntaxcheck/syntaxcheck.go — Check, checkBrackets, hasHashComments, stripTrailingComment, Language; internal/syntaxcheck/syntaxcheck_test.go — TestCheckSyntaxAcceptsValidCode ("rust lifetime annotations are not string literals"), TestCheckSyntaxReportsEachBrokenShape;
  internal/tools/syntaxtrailer.go — syntaxTrailer; internal/tools/syntaxtrailer_test.go — TestSyntaxTrailerSaysNothingWhereThereIsNothingToSay
**Closes:** syntax-heuristic-false-positives
**Tests:** one vector per shape in `syntaxcheck`, plus the guard's valid vectors (the `=>`, `fetchJson<User>` and `if (ch>'9') return;` .tsx lines, the .php `<style>` line, the PHP, Rust, Swift and Java multi-line strings) in `TestCheckSyntaxAcceptsValidCode`; the unclosed-string true positives stay pinned unchanged; `write_file` of each returns no trailer.
**Acceptance:** `go test -race -count=1 ./internal/syntaxcheck/` then `go test -race -count=1 -run 'Syntax' ./internal/tools/` then `! grep -rn 'retry\|correction' internal/syntaxcheck/`
**Commit:** `fix(syntaxcheck): stay silent on valid JSX, PHP, C# and Rust shapes`

## 15. The wire tap reads no Agent state from background goroutines

**What:**
**Goal:** a single `-race` test with the Inspector on, a background workflow streaming over the session client, and the conversation Stepping and rebinding reports no race; `WireEvent` documents the Turn a background child's event carries.
**Approach (assumed at the header base):** `wireTap.bind` (internal/agent/construct.go) snapshots what `observe` needs (sink, EventBase identity) and publishes it through an `atomic.Pointer`, so a rebind's re-bind never races an in-flight `observe`; the Turn comes from an `atomic.Int64` mirror written beside every `turns.index` write (turn.go). Fix for `wire-tap-background-race`.
**Regression guard.** Build the race test's parent with `New(cfg)` over a `newWireUpstream` / stubllm endpoint with `cfg.Inspector=true` and a recipe from `recipeConfig`: `newBackgroundParent` runs over a fake Responder that never calls the wire observer, so a test on it passes on the pre-item tree. Bite check: at the base, `-race` reports `turns.index` (turn.go `turnLifecycle.end` / `restore`) against `observe`.
**Files:** internal/agent/construct.go, internal/agent/turn.go, internal/agent/rebind.go, internal/domain/events.go, internal/agent/wiretap_test.go
**Read first:** internal/agent/construct.go — wireTap.bind, wireTap.observe, armWireCapture; internal/agent/turn.go — turnLifecycle.end, turnLifecycle.restore;
  internal/agent/rebind.go — SwitchUpstream; internal/agent/background_test.go — launchBackground; internal/agent/construct_test.go — newWireUpstream
**Closes:** wire-tap-background-race
**Tests:** new race test as in the Goal, built as the guard says; it reports the race at the base.
**Acceptance:** `go test -race -count=1 -run 'WireTap' ./internal/agent/`
**Commit:** `fix(agent): stop the wire tap racing background workflow children`

## 16. Harmony stripping recognises `<|constrain|>` tool-call headers

**What:**
**Goal:** `StripHarmony` on an analysis plus `commentary to=functions.x <|constrain|>json<|message|>{…}<|call|>` response leaves no control tokens or call JSON in Visible; `IsHarmonyThinking` is true while that message is unterminated; existing vectors pass.
**Approach (assumed at the header base):** in `internal/processing/harmony.go` let `harmonyMessage` and `harmonyOpen` accept `<|constrain|>` between channel and `<|message|>`, e.g. `(?:[^<]|<\|constrain\|>)*`. Fix for `harmony-constrain-header-unparsed`.
**Regression guard.** An analysis + constrained-call reply from a server that does not parse the call now strips to empty Text with no ToolCalls (collect.go), reaching `replyFault`: an empty-response-recovery retry (`floor.RecoverEmpty`), then "upstream returned an empty reply (finish: …)" abandons the Exchange, where at the base it completed with the raw call visible. Name this outcome in the item's changelog sidecar entry and pin it with one agent-level test; optionally word a fault that names an unparsed Harmony tool call instead of "empty reply".
**Files:** internal/processing/harmony.go, internal/processing/harmony_test.go, an internal/agent test file
**Read first:** internal/processing/harmony.go — harmonyMessage, harmonyOpen, StripHarmony, IsHarmonyThinking; internal/agent/collect.go — stripper.Strip seam;
  internal/agent/loop.go — replyFault, emptyReplyErrFmt; internal/floor/emptyreply.go — RecoverEmpty
**Closes:** harmony-constrain-header-unparsed
**Tests:** terminated and unterminated constrain-header vectors for both functions; one agent-level test pins the empty-reply outcome the guard names.
**Acceptance:** `go test -race -count=1 -run 'Harmony' ./internal/processing/` then `go test -race -count=1 -run 'Harmony' ./internal/agent/`
**Commit:** `fix(processing): strip gpt-oss tool-call headers carrying <|constrain|>`
