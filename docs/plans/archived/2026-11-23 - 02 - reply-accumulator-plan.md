# One reply accumulator ends every stream — plan

**Goal:** an unexported `replyAccumulator` in `internal/provider` owns the scan loop, the text and tool-call caps, the malformed count, the pending finish/usage/model and every terminal path (terminator, bare EOF, read fault, in-band error, consumer break); the OpenAI and Anthropic codecs keep only their dialect. No Delta, error text or ordering changes on either wire.
**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1f9f5369
**Closes:** stream-ending-duplicated

**Sources:**
- `.capcom/issues/stream-ending-duplicated.md`; `docs/reviews/archived/architecture-review-2026-10-14.html` (#12); item 7 of `docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md`
- ADR 0078 (one Client; wire traits inside the unexported `wireCodec`), ADR 0083 §6 (stop-reason mapping stays in the codec), ADR 0082 (the idle cut is at the body; the codec never sees it): `capcom know show "ADR NNNN" --with-children`
- The `wireCodec.parseSSE(body, sent, toolFragment, yield)` contract (`internal/provider/client.go`, called from `stream.go`)

**Writer's calls** (2026-11-23):
- **Pure refactor:** every behaviour difference between the wires at the header base is kept (list under Standing requirements); unifying any of them is a separate issue.
- **Epilogue hook:** the codec's epilogue is a func given at construction, run by `finish` after the malformed-only check and before the tool-call flush.
- **Tests:** new tests drive `parseSSE` over `strings.NewReader`; no new `httptest` upstream (ADR 0062 amendment 1118). The existing `sseServer` tests are left as they are.
- **`openToolCalls` stays in `stream.go`.**
- **Interface (regression check):** epilogue given at construction, `patchOpenCalls`, `failLocal` — see item 2.

**Standing requirements:**
- skills: coding-standards
- Machine rule (Raspberry Pi 5, 8 GB): never `go test -race`/`-cover` over a whole heavy package or a pattern; one package + one `-run` with `GOMEMLIMIT=2GiB`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`; never two test runs at once. Forward to every sub-agent.
- Kept differences: (D1) OpenAI charges a chunk's content and chosen thinking together before yielding either, Anthropic per fragment; (D2) Anthropic's charged bytes include signature, redacted data and the per-block reasoning cost, and "carried nothing" is `charged bytes == 0 && no open tool call`; (D3) Anthropic patches empty tool arguments to `"{}"` and yields its layout block after the malformed-only check, before the tool calls; (D4) an Anthropic `error` event with no `error` member counts as malformed; (D5) served model: first non-empty on OpenAI, last `message_start` on Anthropic; (D6) `data: [DONE]` is a counted malformed chunk on Anthropic, `message_stop` an ignored chunk on OpenAI; (D7) cap faults carry neither `MalformedChunks` nor the note; (D8) OpenAI's order within a chunk: error, model, usage, cap, thinking, content, tool folds, finish reason.
- Byte-identical: `apogee: read stream: %v` + `malformedChunksNote`, `malformedOnlyErrFmt`, `toolCallCountTripped`, `toolCallBytesTripped`, `reasoningBlockCountTripped`, the text-cap sentence, `inBandErrPrefix`.
- An authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Migrating the `sseServer`/`httptest` tests in `stream_test.go`.
- `idleBody`, the wire-capture tee, `attemptRecorder`, `Agent.collectCompletion`.
- Any change to a cap's value.

**Regression check (2026-11-23, 1f9f5369):**
- 1: guard folded — D8 pinned by what a Delta shows; both twins take an `io.Reader` and a `yield`; two rows added.
- 2: guard folded (writer's decision) — epilogue at construction, `patchOpenCalls`, `failLocal`; `text` with `n == 0` yields nothing.
- 3: guard folded — Acceptance greps the codec file; the stale-comment rule; the codec keeps first-non-empty model itself.
- 4: guard folded — literal-only grep; the two codec-local faults go through `failLocal`; Acceptance checks the Goal.

## 1. Pin every terminal path on both wires — ✅ DONE (2026-11-23)

**What:** Test-only. A new `internal/provider/terminal_paths_test.go` drives both codecs' `parseSSE` over `strings.NewReader` (and an erroring reader for read faults) with one table per terminal path and one row per wire, asserting the full Delta sequence — kinds, order, and exact `Err` text, `Fault`, `Retryable`, `MalformedChunks`, `FinishReason`, `Usage`, `Model`. It must pass at the header base and stay unedited through items 3 and 4.
Rows, both wires unless noted: terminator ends cleanly; bare EOF with a pending finish and usage keeps both; bare EOF flushes open tool calls; malformed-only at the terminator and at bare EOF; malformed chunk then content (Done carries the count); read error mid-stream (text, the malformed note, `Retryable`, `Fault`); in-band error drops accumulated calls and stamps `MalformedChunks`; text cap (exact sentence, `FaultLocal`, no `MalformedChunks`); tool-argument byte cap summed across calls (`toolCallBytesTripped` exact); tool-call count cap and exactly-at-the-cap flush; consumer returns false (no further yield); the other wire's terminator (D6). Wire-specific rows for D1, D2 (a reply that only opened an empty thinking block is not "carried nothing"), D3 (empty arguments → `"{}"`; layout block position), D4, D5, D8 (see the guard). Added at the regression check: an empty Anthropic `text_delta`/`thinking_delta` yields nothing; an Anthropic malformed payload, then the reasoning-block cap, carries `MalformedChunks` 0.
**Regression guard.** D8 is pinned only by what a Delta shows (a cap fault carries no `Usage` or `FinishReason`): a choice-less chunk still records model and usage on the Done; thinking is yielded before content; a chunk whose tool fold trips still yielded its content first; the cap-crossing chunk yields neither fragment. Both wires' helpers in the new file take an `io.Reader` and a `yield` (read-fault and consumer-break rows) and call `client.codec.parseSSE` directly, as `TestAnthropicParseSSE_ConsumerBreakStopsTheRead` does.
**Files:** internal/provider/terminal_paths_test.go
**Read first:** internal/provider/wire_anthropic_stream_test.go — parseAnthropicSSESent, TestAnthropicParseSSE_Edges, TestAnthropicParseSSE_ConsumerBreakStopsTheRead; internal/provider/wire_openai.go — openaiCodec.parseSSE; internal/provider/wire_anthropic_stream.go — anthropicCodec.parseSSE, anthropicStream.finish; internal/provider/stream.go — Delta, readFault
**Tests:** the file itself; `parseAnthropicSSE` is reused for string-body rows only; an `io.Reader`-taking helper per wire is added in the new file.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTerminalPaths' ./internal/provider/`
**Commit:** `test(provider): pin every stream ending on both wires`

## 2. The reply accumulator — ✅ DONE (2026-11-23)

**What:**
**Goal:** `internal/provider/reply_accumulator.go` defines the unexported `replyAccumulator`, unit-tested on its own; no codec uses it yet and no other file's behaviour changes.
**Approach (assumed at the header base):** binding interface (one deep module; codecs call these and nothing else of it):
- `newReplyAccumulator(toolFragment func(), yield func(Delta) bool, epilogue func() bool)` — `epilogue` nil = none.
- `run(body io.Reader, event func(data string) (ended bool))` — `newSSEScanner`, the `data:` filter, the read-fault Delta (`apogee: read stream: %v` + note, `isTransientReadError`, `readFault`, `MalformedChunks`), and bare EOF → `finish` (which runs the epilogue).
- `skipMalformed()`; `charge(n int) (ended bool)` (text cap only); `emit(d Delta) (ended bool)` (yield, no charge); `text(d Delta, n int) (ended bool)` (`n == 0`: no charge, no yield, not ended); `foldCall(frag sseToolCall) (ended bool)` (`openToolCalls`, both tool caps).
- `setFinish(reason string)`, `setUsage(u Usage)`, `setModel(id string)` — plain setters; the codec applies its own rule (D5, merge versus replace).
- `fail(d Delta) (ended bool)` — stamps `MalformedChunks`; in-band errors only. `failLocal(d Delta) (ended bool)` — a codec-local terminal fault, yielded unstamped.
- `patchOpenCalls(fn func(args string) string)` — rewrites the accumulated arguments of every open tool call; for the epilogue.
- `finish()` — malformed-only check on charged bytes and open calls (D2), then the epilogue, then the tool-call flush, then one `DeltaDone` (default finish reason `"stop"`).
- After any terminal Delta or a false `yield`, every method is a no-op returning `ended == true`; at most one terminal Delta is ever yielded.
- `internal/provider/doc.go` names the accumulator and what stays in a codec.
**Regression guard.** the binding interface changes in three places so it can carry what the Anthropic codec does at the header base — (a) the codec's epilogue is given once at construction, `newReplyAccumulator(toolFragment func(), yield func(Delta) bool, epilogue func() bool)` (nil = none), and `finish()` takes no argument, so the terminator AND the bare-EOF end inside `run` both run it, after the malformed-only check and before the tool-call flush; (b) a method `patchOpenCalls(fn func(args string) string)` lets the epilogue rewrite the accumulated arguments of every open tool call before the flush (Anthropic's empty → "{}" patch, D3) without the codec reaching into `openToolCalls`; (c) `fail(d Delta)` stamps `MalformedChunks` and is for in-band errors only, and a second `failLocal(d Delta) (ended bool)` yields a codec-local terminal fault unstamped (Anthropic's `reasoningBlockCountTripped` and "encode thinking block" faults, exactly as unstamped as they are at the base). Items 3 and 4 and item 2's Tests follow this interface wherever they name the old one (item 4: the epilogue is passed at construction; the two codec-local faults go through `failLocal`; item 2 Tests add: bare EOF inside `run` runs the epilogue; `patchOpenCalls` rewrites before the flush; `failLocal` carries no `MalformedChunks`). Also folded from the report: `text` with `n == 0` charges nothing, yields nothing and does not end, as `anthropicStream.text` does at the base.
**Files:** internal/provider/reply_accumulator.go, internal/provider/reply_accumulator_test.go, internal/provider/doc.go
**Read first:** internal/provider/wire_anthropic_stream.go — anthropicStream.charge, text, fail, finish, yieldLayout; internal/provider/wire_openai.go — openaiCodec.parseSSE (finish, finishPending closures); internal/provider/stream.go — openToolCalls.fold, openToolCalls.flush; internal/provider/docmap_test.go — TestDocMapNamesEveryFile (reply_accumulator.go must be a Files entry)
**Tests:** `reply_accumulator_test.go`, driving the type directly: each terminal path yields exactly one terminal Delta; methods after the end are no-ops; `charge` at the limit passes and one byte over trips; cap faults carry no `MalformedChunks` (D7) while `fail` and the read fault do; `finish` runs the epilogue after the malformed-only check and before the flush; an epilogue returning false stops before Done; default and set finish reason; setters overwrite; bare EOF inside `run` runs the epilogue; `patchOpenCalls` rewrites before the flush; `failLocal` carries no `MalformedChunks`; `text` with `n == 0` yields nothing.
**Acceptance:** `go build ./... && go vet ./internal/provider/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReplyAccumulator|TestTerminalPaths' ./internal/provider/`
**Commit:** `refactor(provider): add the reply accumulator`

## 3. The OpenAI codec ends through the accumulator — ✅ DONE (2026-11-23)

NOTES (2026-11-23): internal/provider/client.go is named in the item's Files but was left unchanged — its parseSSE contract comment says nothing about who owns the scanner, the read fault or the terminal path, so nothing in it became false.
NOTES (2026-11-23): comments in stream.go and doc.go that named "both codecs"/"the codec's scanner" as the reader were reworded wire-neutrally ("the scan loop", "every reply") so they hold both now, while the Anthropic codec still runs its own scan loop, and after item 4; the "both wires" comments on openToolCalls (tripped, open, flush) and wire_anthropic_stream.go were left as they are — still true.

Depends on items 1 and 2.

**What:**
**Goal:** `openaiCodec.parseSSE` (`internal/provider/wire_openai.go`) contains no scan loop, cap check, read-fault construction, malformed-only check or Done construction of its own; `TestTerminalPaths` passes unedited.
**Approach (assumed at the header base):**
- The closures `finish`/`finishPending`, the inline text-cap block and the read-error block are replaced by accumulator calls; `malformed`, `pendingFinish`, `pendingUsage`, the model variable and the `openToolCalls` value become accumulator state.
- Stays in the codec: `sseChunk`/`sseDelta` decoding, `reasoningChannel.thinking()`, the `[DONE]` sentinel, raw `finish_reason`, `usageJSON.usage()`, `sent.carriesEffort`, first-non-empty model (D5), and the D8 order. D1: one `charge(len(content)+len(thinking))`, then `emit` for each.
- Comments in `stream.go` that describe the OpenAI epilogue follow the code.
**Regression guard.** The codec passes a nil epilogue. `setModel` has no getter: the codec keeps its own "model settled" flag and calls `setModel` only while it is unset (D5; item 1's D5 row is the check). Every comment in `internal/provider` that says a codec owns the scanner, the read fault or the terminal path — `grep -n 'scanner\|both wires\|both codecs\|terminal path\|parser reads' internal/provider/*.go` — follows the code (items 3 and 4). The Acceptance grep counts comments: `parseSSE`'s doc comment names `maxReplyTextBytes` at the base and is reworded.
**Files:** internal/provider/wire_openai.go, internal/provider/stream.go, internal/provider/client.go, internal/provider/doc.go
**Read first:** internal/provider/wire_openai.go — openaiCodec.parseSSE, sseDone; internal/provider/stream.go — sseChunk, joinDataPayloads, openToolCalls, inBandErrorDelta, Stream (the comment above parseSSE's call); internal/provider/attempt.go — attemptRecorder.wrap
**Tests:** `TestTerminalPaths` (OpenAI rows) and the existing `TestStream_*` terminal tests pass unedited — `TestStream_KeepsFinishAndUsageWithoutDone`, `TestStream_FlushesOpenCallsWithoutDone`, `TestStream_OnlyMalformedChunksIsAFault`, `TestStream_ReplyTextIsCapped`, `TestStream_ThinkingCountsTowardTheCap`, `TestStream_ToolCallSizeCapSumsAcrossCalls`, `TestStream_InBandErrorDropsEveryAccumulatedCall`, `TestStream_MidStreamEOFIsRetryable`, `TestStream_EarlyBreakIsClean`, `TestStream_FaultClass`.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTerminalPaths|TestReplyAccumulator' ./internal/provider/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStream_' ./internal/provider/ && test -z "$(git diff --name-only HEAD~1 -- internal/provider/terminal_paths_test.go)" && test -z "$(grep -n 'newSSEScanner\|maxReplyTextBytes\|malformedOnlyErrFmt\|isTransientReadError\|DeltaDone' internal/provider/wire_openai.go)"`
**Commit:** `refactor(provider): the openai codec ends through the reply accumulator`

## 4. The Anthropic codec ends through the accumulator — ✅ DONE (2026-11-23)

NOTES (2026-11-23): internal/provider/stream.go is named in the item's Files but was left unchanged — item 3 already reworded its scanner/read-fault comments wire-neutrally ("the scan loop", "every reply"), and the stale-comment grep (`scanner\|both wires\|both codecs\|terminal path\|parser reads`) finds nothing in it that this item made false.
NOTES (2026-11-23): `anthropicStream.fail` is kept as the codec's in-band-error dialect method (the no-member `error` event calls `skipMalformed`, D4; otherwise it renders through `inBandErrorDelta` and ends through the accumulator's `fail`); `yieldLayout` is kept and called from the new `anthropicStream.epilogue`, which is the func passed at construction.
NOTES (2026-11-23): `sseDataPrefix` stays declared in wire_anthropic_stream.go although its only reader is now reply_accumulator.go — moving it is a content move the item does not ask for.

Depends on items 1 and 2.

**What:**
**Goal:** `anthropicStream` (`internal/provider/wire_anthropic_stream.go`) contains no scan loop, text-cap check, read-fault construction, malformed-only check or Done construction of its own; `TestTerminalPaths` passes unedited; `grep -n '"apogee: read stream' internal/provider/*.go` finds the string literal in non-test code only in `reply_accumulator.go`.
**Approach (assumed at the header base):**
- `charge`, `text`, `foldCall`, `fail` and `finish` on `anthropicStream` become calls on the accumulator (`fail` for the in-band error only); `textBytes`, `malformed`, `stopReason`, the usage and model fields and `ended` become accumulator state.
- Stays in the codec: event and delta type constants, block-index addressing, the reasoning map and its place, signature and redacted handling (charged through `charge`, D2), `maxReasoningBlocks` and `reasoningBlockCountTripped` (through `failLocal`), `anthropicReplyLayout`, `anthropicFinishReason` at receipt, `mergeUsage` (the merged value handed to `setUsage`), `prefixDigest`, unconditional model assignment (D5), the no-member `error` event calling `skipMalformed` (D4).
- The epilogue passed at construction: the layout `DeltaThinkingBlock` (skipped while a reasoning block is open), then the `"{}"` patch of empty tool arguments through `patchOpenCalls` (D3).
**Regression guard.** Both codec-local faults — `reasoningBlockCountTripped` and `apogee: encode thinking block: %v` — end through `failLocal`, unstamped as at the base (item 1's "malformed payload, then the reasoning-block cap" row is the check). The read-stream grep matches the string literal only: `stream.go`'s comments quote the sentence in backticks. The Acceptance grep counts comments: the file's header comment names `maxReplyTextBytes` at the base and is reworded; item 3's stale-comment rule applies here too.
**Files:** internal/provider/wire_anthropic_stream.go, internal/provider/stream.go
**Read first:** internal/provider/wire_anthropic_stream.go — anthropicCodec.parseSSE, anthropicStream, openReasoning, stopBlock, yieldLayout, finish, and the file's header comment; internal/provider/wire_anthropic.go — anthropicToolArguments; internal/provider/stream.go — errStreamIdle comment
**Tests:** `TestTerminalPaths` (Anthropic rows) and the existing `TestAnthropicParseSSE_*` tests pass unedited — `_Edges`, `_OpenToolCallCountIsCapped`, `_OpenToolCallCountAtTheCapStillFlushes`, `_InBandError`, `_ConsumerBreakStopsTheRead`, `_ReplyTextIsCapped`, `_ReasoningBlockCountIsCapped`, `_ReasoningBlocksChargeTheTextCap` — with `TestStream_IdleTimeoutCutsTheAnthropicWire` and `TestStream_FaultClass`.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTerminalPaths|TestReplyAccumulator|TestAnthropicParseSSE' ./internal/provider/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStream_FaultClass|TestStream_IdleTimeout' ./internal/provider/ && test "$(grep -l '"apogee: read stream' internal/provider/*.go | grep -v _test.go)" = "internal/provider/reply_accumulator.go" && test -z "$(git diff --name-only HEAD~2 -- internal/provider/terminal_paths_test.go)" && test -z "$(grep -n 'newSSEScanner\|maxReplyTextBytes\|malformedOnlyErrFmt\|DeltaDone' internal/provider/wire_anthropic_stream.go)"`
**Commit:** `refactor(provider): the anthropic codec ends through the reply accumulator`
