# Provider failure honesty — in-band errors, empty-reply guard, Retry-After

- **Goal:** an upstream failure can never masquerade as a successful empty reply. OpenAI-compatible
  aggregators (OpenRouter observed, 2026-08-06) deliver provider errors *in-band*: HTTP 200 whose
  SSE stream or JSON body carries `{"error": {...}}` and no usable choices. Apogee's wire structs
  ignore that member, so the loop commits a blank assistant turn with no error shown. Fix the
  provider layer to parse and surface these, guard the loop against committing empty replies, and
  make 429 retries honor `Retry-After`.
- **Date:** 2026-08-06 · **Status:** unexecuted
- **Authoritative sources:**
  - Pinned code: commit `2e4346d` — `internal/provider/client.go`, `internal/provider/stream.go`,
    `internal/provider/wirejson.go`, `internal/agent/loop.go`. Line anchors below are as of this
    commit; locate by symbol if drifted.
  - OpenRouter error contract: https://openrouter.ai/docs/api-reference/errors — error shape
    `{"error": {"message": string, "code": number, "metadata": object}}`; mid-stream errors arrive
    as an SSE `data:` event on an HTTP-200 response.
  - Evidence: owner session `~/.apogee/sessions/20260806T092047Z-7a2a47cb.json` — one turn committed
    `{"role":"assistant","content":""}` silently (in-band failure on HTTP 200); the retry surfaced a
    real `HTTP 429 … temporarily rate-limited upstream … "limit_source":"upstream_provider_shared_pool"`.
  - Precedence: if an item's prose disagrees with the pinned code or the OpenRouter error contract,
    the pinned code and contract win; note the deviation (see deviation rule below).
- **Ratified design calls** (owner via AskUserQuestion, 2026-08-06):
  1. **Empty reply → fail the turn.** After post-response hooks have had their chance, a reply with
     no visible text and no tool calls emits a visible error and is NOT committed — mirror the
     existing stream-fault path. An enabled `empty_response_recovery` Mechanism retains first claim
     (the guard runs after the hook loop resolves).
  2. **In-band errors surface immediately** — terminal error, no hidden re-requests, on both the
     streaming and non-streaming paths. Transport-level retries stay where they are (real HTTP
     429/5xx).
  3. **Retry-After honored up to a 30s cap** (context-cancellable wait). A header above the cap
     gives up immediately and surfaces the error — never sit blocked on a long ban.
  4. **429 backoff base becomes 1s** (1s/2s waits) when no Retry-After header is present; transport
     faults and 5xx keep the 200ms base.
- **Standing requirements:**
  - skills: coding-standards
  - Run `make check` before every commit (repo rule).
  - Version policy: no VERSION/CHANGELOG-release-heading/tag changes — see the closing suggestion.
  - Deviation rule: any authorized deviation from item text lands as a dated NOTES line under the
    item in this file.
- **Out of scope:** mapping a choice-level `finish_reason: "error"` (no error object) to a fault;
  retrying in-band retryable codes (design call 2); TUI changes (the existing error-entry rendering
  is the display path); any change to the `empty_response_recovery` Mechanism or to mechanism
  enablement in config; a wire-level debug log; provider-specific (non-OpenAI-schema) error shapes.

Rationale note — layer choice: error honesty is engine/provider correctness and therefore must live
below the Mechanisms layer. The Bypass invariant (AGENTS.md) forbids correctness that only holds
with a Mechanism enabled; after this plan the provider reports faults honestly in every mode, and
`empty_response_recovery` remains purely a model-shepherding off-ramp.

## 1. Parse in-band errors on the streaming path — ✅ DONE (2026-08-07)

**What:** In `internal/provider/wirejson.go`, add a `wireError` struct for the OpenAI/OpenRouter
in-band error member: `Message string \`json:"message"\``, `Code json.RawMessage \`json:"code"\``
(servers send a number or a string — parse tolerantly to an int via a small helper
`(wireError).intCode() int` returning 0 when non-numeric), and
`Metadata json.RawMessage \`json:"metadata"\``. In `internal/provider/stream.go`, add
`Error *wireError \`json:"error"\`` to `sseChunk` (~line 198). In `parseSSE` (~line 98), directly
after a chunk unmarshals: when `chunk.Error != nil`, stop the stream with a terminal delta and
`return` — do not fall through to the `len(chunk.Choices) == 0 → continue` skip (~line 133) and do
not reach the implicit-`[DONE]` `DeltaDone` (~line 162), which is the silent-empty bug. Classify:
`intCode() == 400 && isContextOverflow(message)` → `DeltaContextOverflow`; anything else →
`DeltaError`. The error text is `apogee: upstream in-band error <code>: <text>` where `<text>` is
the raw SSE `data` payload passed through `c.sanitize` (API-key redaction + length cap, client.go
~line 340) — the metadata (e.g. OpenRouter's `metadata.raw` provider detail) rides along inside the
payload. A flushed-but-unfinished tool call is dropped (the reply is faulted; do not emit a partial
`DeltaToolCall` before the terminal error).

**Tests** (`internal/provider/stream_test.go` or `reliability_test.go`, table-driven like the
existing SSE tests):
- Error-only stream — no choice chunks, then `data: {"error":{"message":"Provider returned
  error","code":429,"metadata":{"raw":"…rate-limited…"}}}` — yields exactly one terminal
  `DeltaError` (no `DeltaDone`); its `Err` contains `429` and the message. This is the regression
  test for the observed silent turn.
- Mid-stream error after a content delta → the content delta then a terminal `DeltaError`, no
  `DeltaDone`.
- In-band `code: 400` with body matching an overflow marker (e.g. "maximum context length") →
  `DeltaContextOverflow`.
- String code (`"code":"rate_limit_exceeded"`) → still a terminal `DeltaError` (code renders 0 or
  the raw string; assert the message text is present).
- API key appearing in the error payload → `[REDACTED]` in `Err`.
- Existing stream tests unchanged and green (no `error` member ⇒ byte-identical behavior).

**Acceptance:** `go build ./...` && `go test ./internal/provider/` && `make check`

**Commit:** `fix(provider): surface in-band upstream errors on streamed 200 replies`

## 2. Parse in-band errors on the non-streaming path

Depends on item 1 (reuses `wireError`).

**What:** In `internal/provider/wirejson.go`, add `Error *wireError \`json:"error"\`` to
`chatCompletionResponse` (~line 66). In `Respond` (`internal/provider/client.go` ~line 157), after
decoding: when `decoded.Error != nil`, return an error instead of `decoded.toRawResponse()` (which
maps zero choices to a silent zero `RawResponse`, wirejson.go ~line 100 — leave that mapping as-is
for genuinely choice-less successes). Classification mirrors item 1 and the existing `statusError`
(~line 257): `intCode() == 400 && isContextOverflow(message)` → wrapped `ErrContextOverflow`;
otherwise `&StatusError{Code: intCode(), Body: <sanitized text>}` so existing `errors.As` callers
branch uniformly (a non-numeric code yields `Code: 0` — acceptable; the Body carries the truth).
The Body is the error member re-rendered (message plus metadata when present), passed through
`c.sanitize`.

**Tests** (`internal/provider/client_test.go` or `reliability_test.go`):
- 200 body `{"error":{"message":"…","code":429,"metadata":{…}}}`, no choices → `Respond` returns a
  `*StatusError` with `Code == 429` and the message in `Body`; the `RawResponse` is not consulted.
- 200 body in-band `code: 400` + overflow marker → `errors.Is(err, ErrContextOverflow)`.
- String code → error still surfaces (`Code == 0`, message present).
- 200 body with choices AND no error member → unchanged success (existing tests stay green).

**Acceptance:** `go build ./...` && `go test ./internal/provider/` && `make check`

**Commit:** `fix(provider): surface in-band upstream errors in non-streaming replies`

## 3. Retry-After-aware backoff and a 1s base for 429

**What:** In `internal/provider/client.go`: add a pure helper `parseRetryAfter(h string) (time.Duration, bool)`
accepting delta-seconds and HTTP-date forms (per RFC 9110; invalid/absent → false). In `send`
(~line 189), on a retryable status (`isRetryableStatus`, ~line 376) with retry budget remaining:
read `Retry-After` from the response before `drain`. Header present and ≤ 30s (new constant
`maxRetryAfter = 30 * time.Second`) → wait exactly that duration (context-cancellable, reuse the
timer/select shape of `backoff`, ~line 242) instead of the exponential backoff. Header present and
> 30s → give up immediately: return the response as the final answer (it becomes the surfaced
`statusError`), consuming no further attempts. No header → exponential backoff as today, except a
429 uses a new `retry429BaseDelay = 1 * time.Second` (design call 4); transport faults and 5xx keep
`defaultRetryBaseDelay` (200ms, ~line 29). Keep the existing behavior that a caller-cancelled
context aborts without retrying. Update the `Client`/option doc comments to describe the policy.

**Tests** (`internal/provider/client_test.go` / `reliability_test.go`):
- Unit-test `parseRetryAfter`: `"2"` → 2s; an HTTP-date ~2s ahead → ≈2s; `"garbage"`/empty → false;
  negative/past date → 0s, true (retry immediately).
- 429 with `Retry-After: 0` then 200 → succeeds on attempt 2 with elapsed well under the 1s 429
  base (proves the header path, fast test).
- 429 with `Retry-After: 3600` → surfaces `*StatusError` `Code == 429` immediately (single request
  observed at the server, small elapsed time).
- Context cancelled during a Retry-After wait → returns `ctx.Err()` promptly.
- Delay-selection logic (429-without-header → 1s base; 500 → 200ms base) unit-tested via the
  smallest seam the implementer finds reasonable — a sleep-free test; do not add a 1s+ sleep to the
  suite.

**Acceptance:** `go build ./...` && `go test ./internal/provider/` && `make check`

**Commit:** `feat(provider): honor Retry-After and slow 429 backoff`

## 4. Empty-reply guard in the loop

**What:** In `internal/agent/loop.go` `respondAndReview` (~line 316): when the hook loop resolves
to returning a response (`turnOK` paths, ~lines 343 and 362) whose reply is empty — no visible text
(`strings.TrimSpace(resp.Text()) == ""`) and no tool calls — do not return it as `turnOK`. Instead
mirror the plain-fault path (~lines 322–327) exactly: emit an `ErrorEvent` (`Source: "loop"`, text
like `upstream returned an empty reply (finish: <reason>)` including the finish reason for
diagnosis) and return `nil, turnFailed, ""` — the blank assistant message is never committed
(design call 1). Placement is load-bearing: the guard runs only after `runPostResponseHooks` (~line
339) has resolved, so an enabled `empty_response_recovery` Mechanism keeps first claim and a hook
retry that produced content passes untouched; a thinking-only reply (reasoning but no visible text,
no tool calls) counts as empty — it is a non-answer to the user; document that in the guard's
comment. The guard is engine-level and fires in Bypass too (see rationale note). Cancellation is
already handled earlier (~line 319) and never reaches the guard.

**Tests** (`internal/agent/`, using the package's existing scripted-upstream harness):
- Upstream yields a bare `DeltaDone` (no content, no tool calls) → an `ErrorEvent` is emitted, no
  assistant message is committed to history, the turn resolves as failed. Regression test for the
  observed silent turn (with items 1–2 in place this covers genuinely-empty 200s).
- A post-response hook that retries and whose second attempt streams content → guard does not fire;
  the content commits normally.
- Thinking-only reply (reasoning delta, then done) → guard fires.
- Existing agent tests stay green — in particular the `empty_response_recovery` mechanism tests
  (`internal/mechanisms/empty_response_test.go`) and the retry-exchange tests, which prove hook
  precedence is intact.

**Acceptance:** `go build ./...` && `go test ./internal/agent/ ./internal/mechanisms/` && `make check`

**Commit:** `fix(agent): fail the turn on an empty upstream reply`

## 5. CHANGELOG entry

Depends on items 1–4 (describes the landed behavior).

**What:** Single owning item for the cross-cutting doc amendment: add one entry block to
`CHANGELOG.md` under the current unreleased/in-progress heading (create an `## Unreleased` heading
only if none exists; never touch `VERSION`, release headings, or tags) covering the three behavior
changes: in-band upstream errors on HTTP 200 now surface as errors instead of silent empty replies
(streaming + non-streaming); an empty upstream reply now fails the turn visibly instead of
committing a blank assistant message; 429 retries honor `Retry-After` (≤30s, longer gives up
immediately) and use a 1s backoff base without the header.

**Tests:** none (docs-only).

**Acceptance:** `git diff --stat` touches only `CHANGELOG.md`; entry present under a non-release
heading; `make check` still passes.

**Commit:** `docs(changelog): record provider failure-honesty fixes`

---

**Suggested version bump** (not performed — owner's call): patch, `v0.11.8` — user-visible fixes to
failure surfacing plus a small retry-policy change; nothing breaking, no new feature surface.
