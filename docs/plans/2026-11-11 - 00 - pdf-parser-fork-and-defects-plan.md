# PDF parser fork, OAuth issuer pin, and seven small defects

**Goal:** Bring the PDF parser in-tree and bound it at the root, so no PDF can overflow the stack or hang `ExtractPDF`; pin a preregistered MCP client secret to an operator-named issuer; and fix seven self-contained defects from the 2026-11-10 audit and its follow-up run.
**Date:** 2026-11-11
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a91f113a
**Closes:** pdf-parser-unbounded-nesting (once items 2 and 3 are verified)

**Sources:**
- the issue files named on each item's `**Closes:**` line (`.capcom/issues/<id>.md`)
- `docs/reviews/code-audit-2026-11-10.md`; `docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md`
- the parser at the version `go.mod` pins for `github.com/ledongthuc/pdf` (module cache)
- ADR 0095 (D3, D9), ADR 0104 (D9, D11), ADR 0024, ADR 0010, ADR 0031; `docs/design/mcp-client.md`

**Ratified design calls (owner, 2026-11-11):**
- **PDF parser:** forked in-tree as `internal/doctext/pdfparse` and fixed at the root (recursion bound, end-of-input termination). Supersedes the 2026-09-21 call "no fork of `ledongthuc/pdf`" (`docs/plans/archived/2026-09-21 - 01 - audit-local-fixes-plan.md`).
- **OAuth issuer:** a new `issuer:` key; a server entry with `client-secret-env:` and no `issuer:` is refused at config load.

**Planner calls (2026-11-11):**
- **Preflight stays:** the raw-byte nesting preflight in `internal/doctext/pdf.go` is kept as a second layer; this plan removes none of it.
- **Truncated streams:** end of input closes an open array or hex string, as `readDict` already does; no refusal.
- **Git messages:** all four `queryDiagnosed` messages name the bare subcommand (`git add: …`).
- **Probe records:** the battery version is bumped by one, so a record of the old digest is rejected as stale, never compared.
- **File roots:** landlock grants a non-directory writable root in-place write and truncate only; no cross-backend probe row.
- **Rebind retry:** every beat retries while a failure is marked; no backoff (the beat is 60 s, the seam reads local files).
- **Issuer match:** exact string after `TrimSpace`; `issuer:` is legal on any `auth: oauth` entry and pins the authorization server whenever set.
- **Issuer rollout:** the key lands consumed-but-optional (item 11), then required (item 12).

**Regression check (2026-11-11, a91f113a):**
- 1: guard folded (Acceptance uses `go mod tidy -diff`; the `pdfparse.` type name in an unreadable-PDF cause is the one accepted wording change)
- 2: guard folded (writer's decision: `ErrTooDeep` refuses the whole document past every per-page recover; depth counts restored by `defer`)
- 3: guard folded (writer's decision: the hang helper's deadline is a few seconds from `testtime.Fixed`)
- 4: guard folded
- 6: guard folded
- 7: guard folded
- 8: guard folded; the marker wording corrected and the stale beat comment added (writer's decision)
- 10: recast — the empty-`Issuer` refusal leaves this item for item 12; guard folded; re-check: the issuer mismatch is a typed error outside `Login`'s redaction (writer's decision; supersedes `Login`'s origin-only doc sentence for that error)
- 11: recast — split at the re-check (writer's decision): `issuer:` lands parsed, pinned and consumed, but optional; guard folded
- 12: recast — split from item 11 at the re-check (writer's decision): `issuer:` required beside `client-secret-env:`, with the empty-`Issuer` refusal in `mcpauth`; guard folded
- 13: guard folded (item 12 before the re-check renumbered the plan)
- 14: guard folded (item 13 before the renumbering)

**Standing requirements:**
- skills: coding-standards
- A deviation from item text lands as a dated NOTES line under the item.
- In `cmd/apogee` and `internal/tui`, run one test by `-run`, never the whole package.
- Items 10–13 reach `main` together, in the run's one fast-forward: after item 12 the manual's own `client-secret-env:` example is a config apogee refuses until item 13 documents `issuer:`.

**Out of scope:**
- `fence-escape-local-spawner` (ADR-first plan of its own), `served-pdf-csp-sandbox` (needs a manual browser check).
- `context-fill-bench-arm`, `lsp-symbol-navigation`, `serve-http-driver`, `user-view-reaction-cell`.
- Removing or simplifying the PDF preflight; upstreaming the parser patches.

## 1. Bring the PDF parser in-tree as `internal/doctext/pdfparse`

**What:**
**Goal:** `internal/doctext` imports the parser from `internal/doctext/pdfparse`; `go.mod` no longer requires `github.com/ledongthuc/pdf`; extraction behaviour is unchanged.
**Approach (assumed at the header base):** copy the root package of `github.com/ledongthuc/pdf` at the version `go.mod` pins (`ascii85.go`, `lex.go`, `name.go`, `page.go`, `ps.go`, `read.go`, `text.go`, `LICENSE`) verbatim into `internal/doctext/pdfparse/`; never `examples/` or `pdfpasswd/`. The only edits: the package clause (`package pdfparse`), gofmt, and a new `doc.go` naming the upstream module, the copied version and the licence, and stating that local patches are listed there. `internal/doctext/pdf.go` and `pdf_test.go` import the new path under the alias they already use. Remove the requirement with `go mod tidy`. Exclude the directory from golangci-lint in `.golangci.yml` (`linters.exclusions.paths`); gofmt and `go vet` stay clean over it — a vet finding in copied code is fixed minimally and listed in `doc.go`. Reword every comment in `internal/doctext` that names `ledongthuc` or says the parser is not forked (`grep -n "ledongthuc\|fork" internal/doctext/*.go`).
**Regression guard.** Acceptance checks tidiness with `go mod tidy -diff` — it writes nothing and exits non-zero only when tidy would still change `go.mod` or `go.sum` — never with `git diff --exit-code go.mod go.sum`: the item itself edits both files and the verifier runs Acceptance on the uncommitted tree. "Unchanged" has one accepted exception, and no other: a parser panic formatted with `%T` (`lex.go` `readDict`, `read.go` `resolve`) reaches the model through `failureFor` naming the type `pdfparse.dict` where it reads `pdf.dict` today; no test pins the old spelling.
**Files:** internal/doctext/pdfparse/ascii85.go, internal/doctext/pdfparse/lex.go, internal/doctext/pdfparse/name.go, internal/doctext/pdfparse/page.go, internal/doctext/pdfparse/ps.go, internal/doctext/pdfparse/read.go, internal/doctext/pdfparse/text.go, internal/doctext/pdfparse/doc.go, internal/doctext/pdfparse/LICENSE, internal/doctext/pdf.go, internal/doctext/pdf_test.go, internal/doctext/doc.go, go.mod, go.sum, .golangci.yml
**Read first:** internal/doctext/pdf.go — import block, ExtractPDF, walkPages, pageContentNesting, budgetedReaderAt.failureFor; internal/doctext/pdf_test.go — import block, TestExtractPDF_EncryptedFixtureOpensWithTheEmptyPassword;
  go.mod — require block, retract block; .golangci.yml — linters; Makefile — check, lint;
  $GOMODCACHE/github.com/ledongthuc/pdf@v0.0.0-20250511090121-5959a4027728/read.go — package comment, Open, Reader.resolve; same dir lex.go — buffer.readDict, buffer.errorf
**Tests:** none new; the existing `internal/doctext` suite is the no-behaviour-change proof.
**Acceptance:**
- `go build ./... && go vet ./internal/doctext/...`
- `test -z "$(gofmt -l internal/doctext)"`
- `! grep -q ledongthuc go.mod && ! grep -rq '"github.com/ledongthuc/pdf"' --include=*.go .`
- `go mod tidy -diff`
- `go test -race -count=1 ./internal/doctext/...`
**Commit:** `refactor(doctext): bring the PDF parser in-tree as pdfparse`

## 2. The parser refuses to recurse past a fixed depth

Depends on item 1.

**What:**
**Goal:** no PDF makes `pdfparse` recurse deeper than `pdfparse.MaxDepth`; `ExtractPDF` answers such a document with the `pdfNestingCause` refusal and the process survives — for an xref offset forged into an untyped stream body of nested `[`, for a `/ToUnicode` CMap and an `/ObjStm` whose `/DecodeParms` or `/Predictor` is an indirect reference hiding nested `[`, and for a chain of `N G obj N G obj …` headers.
**Approach (assumed at the header base):** fix for `pdf-forged-offset-nesting` and `pdf-indirect-predictor-unscanned` (the remaining ways past the bound `pdf-parser-unbounded-nesting` asked for). In `pdfparse`, carry a depth count through `buffer.readObject` / `readArray` / `readDict` (the `obj` wrapper included) and through `Reader.resolve`'s re-entry; past the bound, panic with an exported sentinel `ErrTooDeep`, the way the parser already reports bad input. `MaxDepth` is 256 and a test pins `pdfparse.MaxDepth > pdfMaxNesting`, so every document the preflight passes still parses. Both `recover` sites in `internal/doctext/pdf.go` (`ExtractPDF`, the page walk) map `ErrTooDeep` to `pdfUnreadableFormat` wrapping `pdfNestingCause`. List the patch in `pdfparse/doc.go`. Reword every doc sentence in `internal/doctext/pdf.go` that states a nesting limit this closes (`grep -n "forged\|not followed\|unpredicted" internal/doctext/pdf.go`: `ExtractPDF`, `readPDFPredictor`).
**Regression guard.** The `ErrTooDeep` refusal must reach `ExtractPDF` as the `pdfNestingCause` failure for the whole document — `Page.GetPlainText`'s per-page recover (and any other recover inside pdfparse) re-panics or otherwise propagates `ErrTooDeep` instead of swallowing it into a per-page failure — and any depth count kept on a `Reader` or `buffer` is restored by `defer`, so one failed page never leaves it raised for the next; a test covers a too-deep page followed by a normal page. `pageContentNesting` returns `fmt.Sprintf(pdfNestingCause, pdfMaxNesting)` unwrapped — `walkPages` already wraps what it returns — and only `ExtractPDF`'s recover, through `failureFor`, applies `pdfUnreadableFormat`, so the model never reads that prefix twice. The reword rule is every sentence in `internal/doctext` saying the parser's recursion is unbounded or a nesting path is unscanned: `grep -n "no limit of its own\|no bound of its own\|forged\|not followed\|unpredicted" internal/doctext/*.go` — it adds `pdfMaxNesting`'s comment and the doc of `TestExtractPDF_RefusesDeepNestingBeforeTheParserRecurses`.
**Files:** internal/doctext/pdfparse/lex.go, internal/doctext/pdfparse/read.go, internal/doctext/pdfparse/page.go, internal/doctext/pdfparse/doc.go, internal/doctext/pdfparse/depth_test.go, internal/doctext/pdf.go, internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdfparse/lex.go — buffer.readObject, readArray, readDict; internal/doctext/pdfparse/read.go — Reader.resolve; internal/doctext/pdfparse/page.go — Page.GetPlainText, Font.charmapEncoding, readCmap; internal/doctext/pdfparse/ps.go — Interpret;
  internal/doctext/pdf.go — ExtractPDF, walkPages, pageContentNesting, pageAccumulator.record, budgetedReaderAt.failureFor, pdfMaxNesting, readPDFPredictor;
  internal/doctext/pdf_test.go — TestExtractPDF_RefusesDeepNestingBeforeTheParserRecurses, TestExtractPDF_ReadsBracketsThatAreNotDeepSyntax, nestingPDF, hostilePDF, xrefStreamPDF, deflatedStream, pngUpRows
**Tests:** rows in `TestExtractPDF_RefusesDeepNestingBeforeTheParserRecurses` for the four shapes (helpers `hostilePDF`, `xrefStreamPDF`, `nestingPDF`, `deflatedStream`, `pngUpRows`), asserting `fmt.Sprintf(pdfNestingCause, pdfMaxNesting)` in the failure; `TestExtractPDF_ReadsBracketsThatAreNotDeepSyntax` stays green; a `pdfparse` unit test of the bound and the `MaxDepth > pdfMaxNesting` pin; a too-deep page followed by a normal page — the whole document is refused with that cause, and the depth count is back where it started for the normal page. Fixtures nest a few hundred levels past `MaxDepth`, never millions: against the pre-item tree they then fail the assertion instead of overflowing the stack.
**Acceptance:** `go test -race -count=1 ./internal/doctext/...`
**Closes:** pdf-forged-offset-nesting; pdf-indirect-predictor-unscanned
**Commit:** `fix(doctext): bound the PDF parser's recursion depth`

## 3. Every parser read loop ends at end of input

Depends on item 2.

**What:**
**Goal:** a content stream, CMap or object stream that ends inside an unclosed `[`, inside `[ <<` or `<< /K [`, inside an unterminated hex string (`<41`), inside `[ (abc`, or an `/ObjStm` whose `/N` exceeds its body, makes `ExtractPDF` return — text or the unreadable-PDF failure — without hanging and without unbounded allocation.
**Approach (assumed at the header base):** fix for `pdf-unclosed-array-hang`: under `allowEOF`, `buffer.readArray` appends the `io.EOF` token forever, `readHexString` spins, and the ObjStm branch of `Reader.resolve` loops on an exhausted body. End of input closes the open array or hex string — the leniency `readDict` already has — so a truncated stream yields the text read so far; the ObjStm loop stops at end of input. List the patch in `pdfparse/doc.go`. Reword the doc sentences in `internal/doctext/pdf.go` that describe these loops as bounded only by the read budget (`grep -n "loop finite\|abandons" internal/doctext/pdf.go`: `budgetedReaderAt`, `pdfNestingProgram`).
**Regression guard.** The hang helper's deadline is a few seconds taken from `internal/testtime.Fixed` (not scaled up by the slow-box factor), because against the pre-item tree `readArray` allocates on every spin; on expiry the helper panics; the post-fix parse of these tiny fixtures finishes in milliseconds on any box. The reword rule runs over the package — `grep -n "loop finite\|abandons\|loops the parser\|hanging the parser" internal/doctext/*.go` — and rewords only sentences about the parser's end-of-input loops: the comment on the "a ToUnicode CMap behind a PNG Up predictor" row in `pdf_test.go` is one, the `pdfPhantomRun` sentence in `ExtractPDF`'s doc is not.
**Files:** internal/doctext/pdfparse/lex.go, internal/doctext/pdfparse/read.go, internal/doctext/pdfparse/doc.go, internal/doctext/pdfparse/eof_test.go, internal/doctext/pdf.go, internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdfparse/lex.go — buffer.readArray, readHexString, readToken, readByte, readDict, newBuffer; internal/doctext/pdfparse/read.go — Reader.resolve (ObjStm branch); internal/doctext/pdfparse/ps.go — Interpret; internal/doctext/pdfparse/page.go — Page.GetPlainText;
  internal/doctext/pdf.go — budgetedReaderAt, pdfNestingProgram, walkPages; internal/doctext/pdf_test.go — nestingPDF, plainStream, deflatedStream, contentStream;
  internal/testtime/testtime.go — Budget, Fixed; internal/testtime/guard_test.go — TestNoFixedTimingInTests
**Tests:** a table in `internal/doctext/pdf_test.go` with one row per shape above, for a page content stream and a `/ToUnicode` CMap, plus the ObjStm rows; `pdfparse` unit tests of `readArray` and `readHexString` at end of input. Each hang fixture runs through one helper that parses in a goroutine under a deadline of a few seconds from `internal/testtime.Fixed` and, on expiry, panics — never `t.Fatal`, which would leave the looping goroutine allocating while the rest of the package runs.
**Acceptance:** `go test -race -count=1 ./internal/doctext/...`
**Closes:** pdf-unclosed-array-hang
**Commit:** `fix(doctext): end the PDF parser's read loops at end of input`

## 4. A git failure names its subcommand, not a leading option

**What:**
**Goal:** a git call whose arguments lead with global options reports its subcommand: the snapshot store's `-c core.excludesFile=<path> add …` that times out reads `git add: timed out after …`, and the same holds for the drain-wedged, output-exceeded and exit messages.
**Approach (assumed at the header base):** fix for `gitexec-timeout-names-flag`: `Host.queryDiagnosed` in `internal/gitexec/gitexec.go` formats four messages with `args[0]`. Add one unexported `subcommand(args []string) string` in that file — skip `-c` and `-C` with their value and any other leading `-` argument, fall back to `args[0]` — and use it in all four. Producers: `queryDiagnosed` only (`probeGit` names its own words and is left alone). Consumers of the text: the snapshot store (`internal/snapshot/store.go`) and `internal/auditsplit/plan.go` surface it unparsed.
**Regression guard.** The `subcommand` table lives in a new untagged in-package file, `internal/gitexec/subcommand_internal_test.go` (`package gitexec`): `gitexec_test.go` is `package gitexec_test` and the one in-package test file is build-tagged unix-only. The `fakeHost` test stays in `gitexec_test.go` and is a table over `TimedOut`, `DrainWedged`, `ExitCode != 0` and `Truncated`, each driven with the store's real spelling `-c core.excludesFile= add -A --ignore-errors` (an empty value, no path), so no message can keep `args[0]` and stay green.
**Files:** internal/gitexec/gitexec.go, internal/gitexec/gitexec_test.go, internal/gitexec/subcommand_internal_test.go
**Read first:** internal/gitexec/gitexec.go — Host.queryDiagnosed, incompleteError, Host.RunDiagnosed, Host.probeGit (left alone); internal/gitexec/gitexec_test.go — fakeHost, cleanRepository, outsideGit, TestHost_DataEntryPointsReportATruncatedStdoutAsIncomplete;
  internal/gitexec/fileprint_internal_test.go — the in-package test precedent; internal/snapshot/store.go — Store.stageAndWrite, Store.streamWith, the `--literal-pathspecs rm` call; internal/auditsplit/plan.go — gitListArgs
**Tests:** a table test of `subcommand` (`-c k=v add`, `--literal-pathspecs rm`, `-C dir status`, `diff`, options only) in `subcommand_internal_test.go`; a table driven through `fakeHost` over `TimedOut`, `DrainWedged`, a non-zero `ExitCode` and `Truncated`, each with the store's exact argument spelling (`-c core.excludesFile= add -A --ignore-errors`), asserting every message begins `git add:` and the timeout one `git add: timed out after`; the existing `git diff: output exceeded` pin stays green.
**Acceptance:** `go test -race -count=1 ./internal/gitexec/ && go test -race -count=1 ./internal/snapshot/`
**Closes:** gitexec-timeout-names-flag
**Commit:** `fix(gitexec): name the subcommand in a git failure that leads with an option`

## 5. The loading notice's bound is tied to the provider's backstop

**What:**
**Goal:** a test fails when the provider's loading backstop and the bound `UpstreamLoadingEvent.Notice()` names disagree; the notice still reads `waiting up to 10m` byte for byte.
**Approach (assumed at the header base):** closes `loading-notice-bound-unpinned`. In `internal/domain/events.go` replace the string constant `upstreamLoadingBound` with an exported `UpstreamLoadingBound time.Duration` that `Notice()` formats through an unexported helper (a whole-minute duration prints as `10m`, never `10m0s`). In `internal/provider/client.go` export the backstop as `LoadingBackstop`; the provider stays domain-free (ADR 0010) and does not import the domain value. A test in `internal/agent` (which imports both) asserts `provider.LoadingBackstop == domain.UpstreamLoadingBound`.
**Files:** internal/domain/events.go, internal/domain/events_test.go, internal/provider/client.go, internal/provider/reliability_test.go, internal/agent/fallback_test.go
**Read first:** internal/domain/events.go — upstreamLoadingBound, UpstreamLoadingEvent.Notice; internal/provider/client.go — loadingBackstop const, Client.loadingBackstop field, NewClient, awaitLoaded; internal/provider/reliability_test.go — loadingPacing (sets the field, not the const);
  internal/agent/fallback_test.go — TestLoadingWaitEmitsOneNoticePerCall, loadingEvents; internal/domain/events_test.go — TestUpstreamSwitchedEventNoticeNamesBothServers (Notice test precedent);
  internal/tui/fold_test.go, internal/run/transcript_test.go, cmd/apogee/headless_test.go, cmd/apogee/daemonfire_test.go — the `waiting up to 10m` pins that must stay green
**Tests:** `internal/domain`: `Notice()` equals the exact current text; the formatter over 10m, 90s and 1h. `internal/agent`: the equality test.
**Acceptance:**
- `go test -race -count=1 ./internal/domain/`
- `go test -race -count=1 -run 'Loading' ./internal/provider/`
- `go test -race -count=1 -run 'Loading' ./internal/agent/`
- `grep -rn "waiting up to 10m" docs/manual/ | grep -q .`
**Closes:** loading-notice-bound-unpinned
**Commit:** `refactor(domain): derive the loading notice's bound from a duration tied to the provider backstop`

## 6. The probe's candidate digest ignores rank order

**What:**
**Goal:** `BehaviorSignature` is identical for two batteries whose `Candidates` hold the same tokens in different orders; the first `apogee probe model` after this change reports no changed model against a record the previous battery wrote.
**Approach (assumed at the header base):** fix for `candidate-digest-rank-order`: `candidateDigest` in `internal/probe/modelfingerprint.go` hashes the tokens joined in `top_logprobs` order. Hash a sorted COPY (`Battery.Candidates` keeps its order — the report counts it). Its doc states what still moves the digest: a token entering or leaving the top-k. Raise `BatteryVersion` (`internal/probe/battery.go`) by one: `probeRecordDefect` already rejects a record of another battery version, so `recordProbeFingerprint` (`cmd/apogee/probemodel.go`) never compares an old digest. Follow the signature's version segment in every test that pins it (`grep -rn 'probe:1:\|battery v1' internal/probe cmd/apogee`).
**Regression guard.** The `cmd/apogee` test seeds its old record as raw JSON at `probe.ProbeRecordPath`, as `TestProbeModelWarnsAboutAnOldFormatRecord` does, with the literals `"version":2,"battery-version":1` — never through `SaveProbeRecord`, which restamps `rec.BatteryVersion` so the record loads as current and the run prints "changed", and never a computed `BatteryVersion-1`, which is 0 on the pre-item tree, where the bite check then cannot fire.
**Files:** internal/probe/modelfingerprint.go, internal/probe/modelfingerprint_test.go, internal/probe/battery.go, internal/probe/battery_test.go, internal/probe/model_test.go, cmd/apogee/probemodel_test.go
**Read first:** internal/probe/modelfingerprint.go — candidateDigest, BehaviorSignature; internal/probe/battery.go — BatteryVersion, Battery.Candidates, probeCandidates; internal/probe/proberecord.go — probeRecordDefect, SaveProbeRecord, LoadProbeRecord; cmd/apogee/probemodel.go — recordProbeFingerprint;
  cmd/apogee/probemodel_test.go — TestProbeModelWarnsAboutAnOldFormatRecord, TestProbeModelReportsAChangedModelBehindTheLabel, runProbeModel, modelUpstream; internal/probe/battery_test.go — runBattery, script, TestBatteryWithoutLogprobs;
  internal/probe/model_test.go — TestModelReportStatesTheFindings; internal/probe/model.go — Model.Report (`capability battery v%d`)
**Tests:** two rank orders give one signature; a token swapped out of the set gives another; in `cmd/apogee`, a stored record of the previous battery version — raw JSON carrying the literal `"battery-version":1` — yields the stale-record warning and no "changed" line.
**Acceptance:**
- `go test -race -count=1 ./internal/probe/`
- `go test -race -count=1 -run 'TestProbeModel' ./cmd/apogee/`
**Closes:** candidate-digest-rank-order
**Commit:** `fix(probe): hash the candidate tokens as a set, not in rank order`

## 7. Landlock accepts a regular file as a writable root

**What:**
**Goal:** a box whose `WritablePaths` holds a regular file confines successfully on the landlock backend: a write to that file succeeds, a write to a sibling in the same directory is denied.
**Approach (assumed at the header base):** fix for `landlock-file-root-einval`: `applyLandlock` in `internal/platform/landlock_linux.go` gives every root `accessMaskForABI(abi)`, whose directory-only rights make `landlock_add_rule` return EINVAL on a non-directory fd. `allowWriteBeneath` fstats its `O_PATH` fd and picks the mask itself — a directory takes the full mask, anything else the file-applicable one (`deviceAccessMaskForABI`: WRITE_FILE, plus TRUNCATE where the ABI has it); the `/dev/null` exemption goes through the same path. A file root allows in-place write and truncate only; rename-over and unlink stay denied. Bring the prose in line: the `allowWriteBeneath` doc, the `canonicalWritableRoot` comment in `confine_posix.go`, the `ConfineWritablePaths` doc in `internal/domain/config.go`, and `docs/design/confinement-execution-contract.md` — every sentence saying a writable root is a directory (`grep -rn "root is a directory\|writable root" internal/platform internal/domain docs/design`).
**Regression guard.** `allowWriteBeneath` takes its mask from one pure helper — the mask for an ABI and is-directory — which it calls after its fstat; `TestDeviceAccessMaskStaysRuleApplicable` ranges over that helper for both fd types and every ABI (today the mask is a parameter and the test ranges over `deviceAccessMaskForABI` alone, so there is no chooser to pin). The prose rule is every comment or doc saying the roots carry the directory mask or that the file-applicable mask is `/dev/null`'s alone: `grep -rn "root is a directory\|writable root\|file-applicable\|roots' mask\|directory mask" internal/platform docs/design` — it adds the `/dev/null` comment in `applyLandlock`, `deviceAccessMaskForABI`'s doc, `TestDeviceAccessMaskForABI`'s comment and the contract's "must not reuse the roots' mask"; the What's grep has no hit in `internal/domain`.
**Files:** internal/platform/landlock_linux.go, internal/platform/landlock_linux_test.go, internal/platform/confine_posix.go, internal/domain/config.go, docs/design/confinement-execution-contract.md
**Read first:** internal/platform/landlock_linux.go — allowWriteBeneath, applyLandlock, deviceAccessMaskForABI, accessMaskForABI; internal/platform/landlock_linux_test.go — TestLandlockAllowsDevNullThroughTheFence, runConfinedShellLine, newTestConfiner, TestDeviceAccessMaskStaysRuleApplicable;
  internal/platform/confine_posix.go — canonicalWritableRoot; internal/domain/config.go — Config.ConfineWritablePaths; internal/domain/confinement.go — ConfinementBox;
  docs/design/confinement-execution-contract.md — "The /dev/null device exemption"
**Tests:** `TestLandlockFileRoot…`, modelled on `TestLandlockAllowsDevNullThroughTheFence` (`newTestConfiner`, `runConfinedShellLine`, skip on `!c.Capabilities().FSWrite`): the file root is writable, a sibling is denied (run error and `os.Stat` not-exist); extend `TestDeviceAccessMaskStaysRuleApplicable` to range over the pure mask helper for both fd types and every ABI.
**Acceptance:** `go test -race -count=1 -run 'TestLandlock|TestDeviceAccessMask' ./internal/platform/ && go vet ./internal/platform/ ./internal/domain/`
**Closes:** landlock-file-root-einval
**Commit:** `fix(platform): give a non-directory landlock root file-applicable rights`

## 8. A failed heartbeat rebind is retried on the next beat

**What:**
**Goal:** a rebind that fails once and succeeds on a later identical beat ends bound to the new model; a permanently failing rebind writes its refusal note exactly once per target; a failed first-contact bind recovers on a later beat without user action.
**Approach (assumed at the header base):** fix for `rebind-failure-never-retried`: `observeBinding` in `internal/tui/heartbeat.go` records the observation before `applyRebind` runs, so an identical next beat sees no change. Make its change test also true while `m.hb.lastRebindFailed != ""`; `applyRebind` is the one place that SETS the `lastRebindFailed` marker for all four callers, and keeps its once-per-target note; `followFallback` also clears it and `foldServerSwitch` resets the heartbeat state wholesale, and both stay as they are. The item also corrects the stale "every ten seconds" comment in `internal/tui/heartbeat.go` to the real 60 s beat (`heartbeat.Interval`). No backoff. A recovery after a failed first contact prints the ordinary `connected: …` note. `heartbeatState` stays plain values (the Model is copied by value). Update the fail-once wording in `internal/tui/doc.go` and record the retry as a dated amendment to ADR 0024 (`capcom know add --id adr-0024-amend-<MMDD> --parent adr-0024`), named on the record's Status line and in its reading-order list.
**Regression guard.** A retry that fires only because the marker is set, on a beat with `ContextWindow == 0`, keeps `observedWindow` and carries that last known window on the intent — a windowless beat names no window (`TestZeroWindowBeatIsNotAChange`), and overwriting the observation with 0 would have a successful retry bind and announce an unknown window. The new tests take the `TestRebind` prefix Acceptance selects by: `TestRebindRetriesAfterFailure` and `TestRebindFirstContactFailureRecovers`. The prose rule is every comment or Note saying a beat is measured only against the last observation, or that Rebind runs only on a change: `grep -rni "last observation\|fail-once\|once per distinct target\|observed the upstream serving" internal/tui cmd/apogee .capcom/knowledge/glossary` — it adds the `ServerHost.Rebind` contract in `internal/tui/tui.go`, beyond `doc.go` and ADR 0024.
**Files:** internal/tui/heartbeat.go, internal/tui/heartbeat_test.go, internal/tui/tui.go, internal/tui/doc.go, .capcom/knowledge/decisions/adr-0024.md, .capcom/knowledge/decisions/adr-0024/
**Read first:** internal/tui/heartbeat.go — observeBinding, applyRebind, heartbeatState.lastRebindFailed, applyPendingRebind, followFallback, rebindNote; internal/tui/heartbeat_test.go — TestRebindFailureNotedOnce, TestZeroWindowBeatIsNotAChange, fakeRebind, wireRebind, unbound, upBeat, foldBeatMsg, countNotes;
  internal/tui/picker.go — bindPickedModel; internal/tui/tui.go — ServerHost.Rebind; cmd/apogee/wire_verbs.go — rootWiring.rebind; cmd/apogee/wire_settings.go — rebindSpecFor, liveSettings.observe;
  internal/tui/doc.go — package doc (heartbeat/rebind section); .capcom/knowledge/decisions/adr-0024.md — Status line, reading order
**Tests:** `TestRebindFailureNotedOnce` flips to "the seam is asked again on an identical beat, still one note"; new `TestRebindRetriesAfterFailure` (fail, then succeed) and `TestRebindFirstContactFailureRecovers` (first contact, `unbound(opts)`); a `TestRebind…` case — fail, then a window-0 beat — asserting the seam was asked for the last known window; fakes `fakeRebind`, `wireRebind`, `upBeat`, `foldBeatMsg`, `countNotes`.
**Acceptance:**
- `go test -race -count=1 -run 'TestRebind' ./internal/tui/`
- `capcom check && capcom know show "ADR 0024" --with-children | grep -qi retr`
**Closes:** rebind-failure-never-retried
**Commit:** `fix(tui): retry a failed heartbeat rebind on the next beat`

## 9. A Windows test pins the narrowed remove classifier

**What:** test-only. Add `TestWindowsRemoveSharingErrorClassifier` to `internal/session/journal_windows_test.go`: a table over bare and `*os.PathError`-wrapped errnos asserting `isRemoveSharingError` is true for `windows.ERROR_SHARING_VIOLATION` and false for `windows.ERROR_ACCESS_DENIED` and for nil. The name must start with `TestWindows`: the Windows CI job selects tests by `-run 'TestWindows|TestAtomicWriteRetries'`.
**Files:** internal/session/journal_windows_test.go
**Read first:** internal/session/rename_windows.go — isRemoveSharingError, isRenameSharingError; internal/session/journal_windows_test.go — TestWindowsRemoveJournalOfAReadOnlyJournal, TestWindowsRemoveJournalUnderAWatcher;
  internal/session/rename_other.go — isRemoveSharingError (non-Windows stub); internal/session/store.go — the RemoveJournal caller of isRemoveSharingError;
  .github/workflows/ci.yml — the `-run 'TestWindows|TestAtomicWriteRetries'` session step
**Tests:** the test above; it runs only on Windows CI.
**Acceptance:** `GOOS=windows go vet ./internal/session/ && grep -q "func TestWindowsRemoveSharingErrorClassifier" internal/session/journal_windows_test.go`
**Closes:** remove-classifier-narrowing-untested
**Commit:** `test(session): pin isRemoveSharingError refusing ERROR_ACCESS_DENIED`

## 10. mcpauth sends a client secret only to the pinned issuer

**What:** Recast at the regression check (2026-11-11).
**Goal:** with an issuer configured, a login whose authorization server is not that issuer fails naming both and sends no token request; a refresh whose stored record names another issuer (or none) fails as login-required and sends no token request; a public client and Dynamic Client Registration with no issuer behave as before.
**Approach (assumed at the header base):** first half of the fix for `oauth-secret-unpinned-issuer`. `LoginConfig` and the handler's config in `internal/mcpauth` gain `Issuer string`. In `loginFlow.run` (`login.go`), when `Issuer` is set: select it when the protected-resource metadata lists it at any position, else — and when the fallback origin differs from it — fail before any authorization-server metadata fetch; compare exact strings after `TrimSpace` (the SDK already requires the metadata's `issuer` to equal the URL fetched). In `NewHandler` / `refreshLocked` (`handler.go`), beside `clientMatches`, refuse when `Issuer` is set and `record.Issuer` differs, through `loginRequired`.
**Regression guard.** At item 10's commit an empty `Issuer` means no pin and every existing login and refresh behaves exactly as before, so the commit is safe on its own; the refusal of a client secret with an empty `Issuer` is item 12's. The issuer mismatch is a typed error in `internal/mcpauth` that `Login` returns OUTSIDE its `security.NewOriginRedactor(cfg.Endpoint).RedactErr` wrap; its text names the configured issuer and the offered authorization server(s) as scheme+host+path only — never query, fragment or userinfo — so two same-host issuers stay distinguishable; the doc sentence on `Login` that says errors carry origins only is reworded to name this one exception. That supersedes, for this error alone, `Login`'s doc in `internal/mcpauth/login.go` ("Error text names the server and never carries more of the endpoint than its origin."). A test covers the same-host pair (endpoint `https://h/mcp`, `issuer: https://h/realms/a`, metadata listing `https://h/realms/b`). `serveResource` hard-codes one authorization server, so that row and "issuer listed second" both need a `fakeOAuth` field for the servers it lists.
**Files:** internal/mcpauth/login.go, internal/mcpauth/login_test.go, internal/mcpauth/handler.go, internal/mcpauth/handler_test.go
**Read first:** internal/mcpauth/login.go — Login, loginFlow.run, resourceMetadata, authServerMetadata, LoginConfig; internal/mcpauth/handler.go — NewHandler, clientMatches, refreshLocked, loginRequired, HandlerConfig; internal/security/urlscrub.go — NewOriginRedactor, RedactErr;
  internal/mcpauth/login_test.go — fakeOAuth, serveResource, seen, loginConfig; internal/mcpauth/handler_test.go — storedRecord, assertLoginRequired, refreshes, TestHandlerRefreshRefusesARecordOfAnotherClient; internal/mcpauth/store.go — Record;
  internal/security/urlscrub_test.go — TestOriginRedactor_CutsTheEndpointToItsOrigin
**Tests:** `TestLoginRefusesAnotherIssuer` (`fakeOAuth`'s two origins; `seen().tokenForm` shows no secret left); a same-host issuer pair whose error text carries both issuers' paths, and a listed server carrying a query and userinfo whose error text carries neither; issuer listed second is selected (all through a new `fakeOAuth` field for the listed authorization servers); refresh with a mismatched and with an empty stored issuer → `assertLoginRequired`, zero token requests; public-client and DCR tests unchanged.
**Acceptance:** `go test -race -count=1 ./internal/mcpauth/`
**Commit:** `fix(mcpauth): send a client secret only to the configured issuer`

## 11. `issuer:` is a server config key that pins the authorization server

Depends on item 10.

**What:** Recast at the regression check (2026-11-11).
**Goal:** an MCP server entry accepts `issuer:`; the configured issuer reaches both the login and the connection's refresh handler; the adoption pane shows the key; with the key absent everything behaves as before.
**Approach (assumed at the header base):** the config half of the fix for `oauth-secret-unpinned-issuer`. `ServerConfig` (`internal/mcp/transport.go`) gains `Issuer`: add it to `Equal`, to `ValidateAuth` (legal only with `auth: oauth`; an absolute `http`/`https` URL with a host and no userinfo, query or fragment; no normalisation; NOT yet required) and to `newOAuthHandler`. `mcpServerConfig` / `toServerConfig` (`internal/config/config.go`) parse it; `pinFieldOrder` / `execPin` (`internal/config/execpin.go`) pin it, which is what draws it in the adoption pane. `cmd/apogee/mcp_cmd.go` passes it into `LoginConfig`. Producers of the value: the config layers; consumers: `ValidateAuth`, `Equal`, the exec pin, `newOAuthHandler`, the login command — a test at each.
**Regression guard.** The key lands consumed but optional: with `issuer:` absent every config, login and refresh behaves as before, and no existing fixture or refusal row changes here. No `issuer:` refusal echoes the configured value (`ValidateAuth`: "The error names the keys, never a value"). `comparedFields` goes 14 to 15, and the yaml field, `execPin` and `pinFieldOrder` move together or `TestServerExecPinCoversEveryField` is red. The wiring is tested, not assumed: `writeMCPConfig` writes each of `client-id:`, `client-secret-env:` and `issuer:` only when set, so its other callers' files are unchanged; a login test that sets the secret variable uses `t.Setenv` and is not parallel, as `TestMCPCmdLoginHonoursTheAPOGEE_CONFIGHome` is; a wrong `issuer:` fails with `fake.exchanges.Load() == 0` at `fakeOAuthMCP` — before the wiring that login succeeds; and `TestConnect_OAuthServerRefusals` gains a per-row seed hook and issuer (or the row a test of its own), so a stored record of another issuer is `mcpauth.ErrLoginRequired` with `o.seen == 0`.
**Files:** internal/mcp/transport.go, internal/mcp/transport_test.go, internal/config/config.go, internal/config/config_test.go, internal/config/execpin.go, internal/config/execpin_test.go, cmd/apogee/mcp_cmd.go, cmd/apogee/mcp_cmd_test.go
**Read first:** internal/mcp/transport.go — ValidateAuth, ServerConfig, Equal, newOAuthHandler; cmd/apogee/mcp_cmd.go — loginMCPServer, mcpClientSecret; internal/config/config.go — mcpServerConfig, toServerConfig, fileMCPServers; internal/config/execpin.go — pinFieldOrder, execPin;
  internal/mcp/transport_test.go — TestServerConfigValidateAuth, TestServerConfigEqual_CoversEveryField, TestConnect_OAuthServerRefusals, oauthHost, oauthServerConfig; internal/config/config_test.go — TestApplyConfigMCPServerAuth; internal/config/execpin_test.go — TestServerExecPinCoversEveryField;
  cmd/apogee/mcp_cmd_test.go — writeMCPConfig, fakeOAuthMCP, runMCPCommand, TestMCPCmdLoginHonoursTheAPOGEE_CONFIGHome
**Tests:** `TestServerConfigValidateAuth` shape rows (issuer without `auth: oauth`; malformed URL; a query or fragment; userinfo — `https://user:pass@host/…` is refused, the error naming the key and never the value); `TestServerConfigEqual` and `TestServerConfigEqual_CoversEveryField`; `TestApplyConfigMCPServerAuth` parses the key; `TestServerExecPinCoversEveryField` green. In `cmd/apogee`: a `TestMCP…` login test over `writeMCPConfig` (now writing `client-id:`, `client-secret-env:`, `issuer:`) where a wrong `issuer:` fails and `fakeOAuthMCP` sees no token request. In `internal/mcp`: a `TestConnect_OAuthServerRefusals` row — a stored record of another issuer is `mcpauth.ErrLoginRequired`.
**Acceptance:**
- `go test -race -count=1 ./internal/mcp/`
- `go test -race -count=1 ./internal/config/`
- `go build ./cmd/apogee/ && go test -race -count=1 -run 'MCP|WireConfig' ./cmd/apogee/`
**Commit:** `feat(mcp): add issuer: to pin an OAuth server's authorization server`

## 12. `client-secret-env:` without `issuer:` is refused

Depends on item 11.

**What:** Recast at the regression check (2026-11-11).
**Goal:** an MCP server entry with `client-secret-env:` and no `issuer:` fails config validation on every layer with an error naming both keys; `mcpauth` refuses a client secret with an empty `Issuer` at login and at handler construction.
**Approach (assumed at the header base):** fix for `oauth-secret-unpinned-issuer`. `ValidateAuth` (`internal/mcp/transport.go`) requires `issuer:` beside `client-secret-env:`; the layers that call it — `fileMCPServers`, `checkRepoServer`, `validateServers` — refuse through it. In `internal/mcpauth`, `Login` and `NewHandler` refuse a client secret with an empty `Issuer`.
**Regression guard.** The required-`issuer:` check sits after every existing `client-id:` / `client-secret-env:` check, so the refusal rows carrying a secret and no issuer keep their pinned wording (`client-secret-env: needs client-id:`) and stay as written; only accepted-shape fixtures gain `issuer:`. The `ValidateAuth` rule and the `mcpauth` refusal share this commit: apart, a secret without issuer loads and fails only at login. That refusal turns red `TestLoginWithAPreregisteredClientDoesNotRegister` and every row of `TestHandlerRefreshAuthenticatesTheClientAsRegistered` (a secret and no issuer): each sets `Issuer`, `storedRecord` gains the matching `Issuer`, and the "public client" row still proves no `client_secret` leaves. "Every layer" reads per layer: the global file is refused at load, a Project config entry is dropped with `repoServerRefusedNotice` naming both keys while the rest of the layer loads, and an embedder Host is refused by `validateServers` — with a repo-layer row beside the `auth:` one in `internal/config/execlayer_test.go`.
**Files:** internal/mcp/transport.go, internal/mcp/transport_test.go, internal/config/config_test.go, internal/config/execlayer_test.go, cmd/apogee/wire_config_test.go, cmd/apogee/mcp_cmd_test.go, internal/mcpauth/login.go, internal/mcpauth/login_test.go, internal/mcpauth/handler.go, internal/mcpauth/handler_test.go
**Read first:** internal/mcp/transport.go — ValidateAuth; internal/config/execlayer.go — checkRepoServer, repoMCPServers, repoServerConfig; internal/mcp/client.go — validateServers;
  internal/mcp/transport_test.go — TestServerConfigValidateAuth; internal/config/config_test.go — TestApplyConfigMCPServerAuth; internal/mcpauth/login.go — Login, LoginConfig; internal/mcpauth/handler.go — NewHandler, HandlerConfig;
  internal/mcpauth/handler_test.go — storedRecord, assertLoginRequired
**Tests:** `TestServerConfigValidateAuth` row — secret without issuer asserts the exact error text the code emits; per-layer rows: `TestApplyConfigMCPServerAuth` (global file), a repo-layer row in `execlayer_test.go` — the entry is dropped with the notice naming both keys — and a `validateServers` row in `internal/mcp`; existing accepted fixtures with `client-secret-env:` gain an `issuer:` (`internal/mcp`, `internal/config`, `cmd/apogee/wire_config_test.go`, `cmd/apogee/mcp_cmd_test.go`). In `internal/mcpauth`: a client secret with an empty `Issuer` is refused by `Login` and by `NewHandler`; `TestLoginWithAPreregisteredClientDoesNotRegister` and `TestHandlerRefreshAuthenticatesTheClientAsRegistered` set `Issuer`, over a `storedRecord` carrying the matching one.
**Acceptance:**
- `go test -race -count=1 ./internal/mcpauth/`
- `go test -race -count=1 ./internal/mcp/`
- `go test -race -count=1 ./internal/config/`
- `go build ./cmd/apogee/ && go test -race -count=1 -run 'MCP|WireConfig' ./cmd/apogee/`
**Commit:** `fix(mcp): refuse client-secret-env: without issuer:`

## 13. Document the issuer pin and amend ADR 0095 and ADR 0104

Depends on item 12.

**What:**
**Goal:** the manual, the shipped default config, the MCP design contract, the adoption-pane layout spec, the glossary and the two decision records all describe `issuer:` and the rule that a client secret goes only to the pinned issuer.
**Approach (assumed at the header base):** document the key beside `client-secret-env:` in `docs/manual/configuration.md` and in the commented example in `internal/config/defaults/config.yaml`, including the load-time refusal and what an existing config must add. Update `docs/design/mcp-client.md` (the trust statement for the authorization server) and the adoption-pane field list and example in `docs/layout/user-questions-layout.md`. Update the glossary Note `mcp-client`. Add dated amendments `adr-0095-amend-<MMDD>` (D3/D9: the client secret) and `adr-0104-amend-<MMDD>` (the pinned field list), each named on its record's Status line and reading-order list.
**Regression guard.** Acceptance greps each of the four files on its own — one `grep -q` over four files exits 0 as soon as any one matches — and reads the glossary Note, `capcom know show "MCP client" | grep -q "issuer:"`; the `go test ./internal/config/` line proves nothing here, the default config's example being all comment. The doc rule is every sentence that lists a server's OAuth keys, `ValidateAuth`'s refusals, the adoption pane's server fields, or says who names the authorization server: `grep -rn 'client-secret-env\|client-id\|authorization server' docs/manual docs/design/*.md docs/layout internal/config/defaults .capcom/knowledge/glossary` — it adds `configuration.md`'s adoption-pane sentence and its http entry's key list, `mcp-client.md`'s `ValidateAuth` "one rule", and the layout spec's "the `endpoint:` is where those values go".
**Files:** docs/manual/configuration.md, internal/config/defaults/config.yaml, docs/design/mcp-client.md, docs/layout/user-questions-layout.md, .capcom/knowledge/glossary/, .capcom/knowledge/decisions/adr-0095.md, .capcom/knowledge/decisions/adr-0095/, .capcom/knowledge/decisions/adr-0104.md, .capcom/knowledge/decisions/adr-0104/
**Read first:** docs/manual/configuration.md — "External MCP servers" `auth: oauth` example and refusal paragraph, the adoption-pane paragraph; docs/design/mcp-client.md — the "`auth: oauth` server's bearer rides the origin pin" bullet;
  docs/layout/user-questions-layout.md — Adoption pane "For an MCP server entry" bullet, the HTTP server body example; internal/config/defaults/config.yaml — the commented `mcp-servers:` example; .capcom/knowledge/glossary/glossary-safety/mcp-client.md — the `auth: oauth` passage;
  .capcom/knowledge/decisions/adr-0095/adr-0095-decision.md — D3, D9; .capcom/knowledge/decisions/adr-0104/adr-0104-decision.md — decisions 2, 11; .capcom/knowledge/decisions/adr-0104.md — Status line, reading order
**Tests:** none new; the config package's tests load the default config.
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0095" --with-children | grep -q "issuer"` and the same for `"ADR 0104"`
- `for f in docs/manual/configuration.md internal/config/defaults/config.yaml docs/design/mcp-client.md docs/layout/user-questions-layout.md; do grep -q "issuer:" "$f" || exit 1; done`
- `capcom know show "MCP client" | grep -q "issuer:"`
- `go test -race -count=1 ./internal/config/`
**Closes:** oauth-secret-unpinned-issuer
**Commit:** `docs(mcp): document issuer: and amend ADR 0095 and ADR 0104`

## 14. Require an x/net release that clears GO-2026-6866 and GO-2026-6860

Depends on item 1 (both edit `go.mod`).

**What:**
**Goal:** `go.mod` requires `golang.org/x/net` v0.62.0 or later, the `retract [v1.0.0, v1.8.0]` block is intact, and govulncheck's verbose output lists neither GO-2026-6866 nor GO-2026-6860.
**Approach (assumed at the header base):** `go get golang.org/x/net@v0.62.0 && go mod tidy` (needs the network; it also raises `x/sys`, `x/text`, `x/term`). Touch nothing else in `go.mod`.
**Regression guard.** The run also raises `golang.org/x/sync` (v0.23.0 → v0.24.0, which x/text v0.43.0 requires), so the rule replaces the list and "Touch nothing else": the `go.mod` diff changes only `golang.org/x/*` version lines; the `go` line, the retract block and every other requirement stay byte-identical.
**Files:** go.mod, go.sum
**Read first:** go.mod — require block (golang.org/x/net, x/sys, x/text), indirect block (x/sync, x/term), go directive, retract block; go.sum — golang.org/x lines;
  Makefile — GOVULNCHECK_VERSION, vulncheck, check; .capcom/issues/xnet-module-advisories.md — Acceptance
**Tests:** none new; the importers' suites (`internal/mdns`, `internal/security`, `internal/mcp`) are the proof.
**Acceptance:**
- `go build ./... && grep -q 'retract \[v1.0.0, v1.8.0\]' go.mod`
- `make vulncheck`
- `go run golang.org/x/vuln/cmd/govulncheck@"$(sed -n 's/^GOVULNCHECK_VERSION *[:?]*= *//p' Makefile)" -show verbose ./... | grep -c 'GO-2026-6866\|GO-2026-6860' | grep -qx 0`
- `go test -race -count=1 ./internal/mdns/`, then `./internal/security/`, then `./internal/mcp/`
**Closes:** xnet-module-advisories
**Commit:** `build: require golang.org/x/net v0.62.0 to clear GO-2026-6866 and GO-2026-6860`
