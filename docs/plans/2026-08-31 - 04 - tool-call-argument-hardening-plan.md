# Tool-call argument hardening

**Goal:** A malformed tool call is refused with a retry signal instead of silently running as a
different call, and streamed parallel tool calls are accumulated by their wire index instead of by
arrival order.

**Date:** 2026-08-31 · **Status:** unexecuted · **Sized for:** ~200k-context host · **Base:** `07daba40`

**Sources**

- `internal/domain/tools.go:237-284` — `FoldArgumentKey`, `CollidingArgumentKeys`, and the pinned
  exact-duplicate last-wins line at `:277`.
- `internal/agent/dispatch.go:262`, `:401`, `:424-455` — the colliding-keys refusal, called from
  both the serial path and `prepareDelegation`.
- `internal/provider/stream.go:138-307` — `parseSSE`, `accumulateToolCalls`, `sseToolCall`.
- `internal/tui/toolargs.go:110-165` — `duplicateKeyNote` and the last-wins prose.
- Incident: session `~/.apogee/sessions/20260831T112434Z-538f0700.json`, message 63. The `sub_agent`
  call `call_9de8031a8cd8453fa2a56ceb` arrived as
  `{"name":…,"task":A,"max_steps":1,"max_steps":1,"task":B}`; last-wins ran it as `task:B`,
  `max_steps:1`, and the delegation died after one Turn with no visible text.

**Ratified design calls** (owner, 2026-08-31)

- **Duplicate policy:** refuse a repeated argument key only when its values DIFFER; byte-identical repeats stay last-wins.
- **Accumulator:** key fragments by wire `index` when present, fall back to today's id/current rule when absent; a repeated id continues the open call; every call is emitted at stream end in index order.
- **Scope:** these two fixes only — a `duplicateKeyNote` for registered tools' transcript blocks and a sub-agent `max_steps` floor were both denied.

**Regression check (2026-08-31, `07daba40`):**

- 1: guard folded — `internal/domain/doc.go`'s package map must name `RepeatedArgumentKeys` and its
  second check; `internal/domain/doc.go` added to **Files:**.
- 2: guard folded — the prose grep keeps test prose and the rule covers docs as well as code;
  `internal/agent/resolution.go`, `internal/agent/resolution_test.go` and
  `docs/design/confinement-execution-contract.md` added to **Files:**; the item yields to
  `docs/design/confinement-execution-contract.md:634-635`, whose CacheKey last-wins reading stands
  for byte-identical repeats.
- 3: recast — an index-bearing fragment opens a call only when it also carries an id or a name, so
  the provider never manufactures a nameless, id-less call; the item yields to
  `internal/agent/loop.go:608-622` + `CHANGELOG.md:1494-1500`, whose deliberate drop-and-report
  stands unchanged; the guard's single comment site is replaced by the rule plus its grep and
  `internal/provider/client.go` is added to **Files:**.
- 3 (re-check of the recast): guards folded — the comment rule widens to cover WHEN a call is
  emitted (`internal/provider/stream.go:23` states the timing the item reverses; the item's grep
  already surfaces it); the in-band-error test gains a second id-bearing fragment, without which it
  is green at `07daba40` and guards nothing (the only flush before the fault is the next-id flush at
  `stream.go:257`); the dropped index-only fragment asserts no delta of any kind — in particular no
  `DeltaError` — instead of an `ErrorEvent`, which is `domain.ErrorEvent` (`internal/agent/loop.go:635`)
  and unobservable from `internal/provider`, which imports no `internal/domain`. **Files:** unchanged.

**Standing requirements**

- `skills: coding-standards`

**Out of scope**

- Extending `duplicateKeyNote` beyond the approval prompt and the unregistered-tool fallback.
- Any `max_steps` floor or `sub_agent` schema change.
- The non-streaming `Respond` path — it decodes whole calls and never accumulates.
- Any VERSION or CHANGELOG release-heading change.

## 1. `domain.RepeatedArgumentKeys` — the key given two answers — ✅ DONE (2026-08-31)

NOTES (2026-08-31): consequential edit — internal/domain/tools_test.go: the section header comment "The argument-key fold (FoldArgumentKey, CollidingArgumentKeys)" now names RepeatedArgumentKeys too, made necessary by adding the new test under it.

**What:** Add `func RepeatedArgumentKeys(raw json.RawMessage) ([]string, error)` to
`internal/domain/tools.go`, beside `CollidingArgumentKeys`. It reports the quoted spelling of every
key that appears more than once within one object carrying at least two DIFFERING values —
`{"task":"a","task":"b"}` → `["\"task\""]` — sorted and deduped across the whole value, descending
into nested objects and objects inside arrays exactly as `CollidingArgumentKeys` does. Values are
compared by running each occurrence's raw bytes through `json.Compact` and comparing with
`bytes.Equal`: whitespace-insensitive, spelling-honest, so `1` and `1.0` count as DIFFERING
(deliberate — two spellings the executor could round differently must not be declared the same
answer). Errors are the same three `CollidingArgumentKeys` reports (not an object, unparsable,
trailing content) with the same `argument object: ` prefix, so a caller can tell "nothing repeats"
from "nothing could be read". A fold collision alone is NOT this rule's business and returns no
group. Binding: `CollidingArgumentKeys` is left byte-identical — do NOT refactor the two into a
shared walker; the new walk captures each member's value as `json.RawMessage` (`decoder.Decode`
into one) while the collision walk only needs names, so they carry different state and change for
different reasons. Amend the `:277` doc-comment sentence ("A key repeated with the SAME spelling …
is not a collision") to point at the new function for the differing case.

**Regression guard.** `internal/domain/doc.go:65-68` maps tools.go's argument-key surface as
`FoldArgumentKey`, `CollidingArgumentKeys` and "the check that refuses an object naming one
parameter under two spellings" — singular. Amend that sentence to name `RepeatedArgumentKeys` and
its second check; `TestDocMapNamesEveryFile` checks file names only, so nothing goes red — it rots.

**Files:** `internal/domain/tools.go`, `internal/domain/tools_test.go`, `internal/domain/doc.go`

**Tests:** A table in `internal/domain/tools_test.go` beside the existing collision table:
`{"task":"a","task":"b"}` → `"task"`; `{"a":1,"a":1}` → none; `{"a":1,"a":1.0}` → `"a"`;
`{"a":[1, 2],"a":[1,2]}` → none (whitespace only); `{"o":{"p":1,"p":2}}` → `"p"`;
`{"o":[{"p":1,"p":2}]}` → `"p"`; `{"a":1,"A":2}` → none; two repeated keys → both, sorted; a bare
array, `{`, and `{"a":1} x` → error. The existing `CollidingArgumentKeys` table stays unchanged and
green.

**Acceptance:** `go build ./... && go test ./internal/domain/`

**Commit:** `feat(domain): RepeatedArgumentKeys reports a key given two answers`

## 2. Dispatch refuses a call that answers one parameter twice

**What:** Depends on item 1. A call whose arguments answer one parameter twice is refused before
resolution, exactly as a fold collision is — the defect the incident above exercised: apogee ran a
call the model did not write and gave it no signal to retry. In `internal/agent/dispatch.go`, add
`repeatedArgumentKeysResult(call domain.ToolCall) (domain.ToolResult, bool)` beside
`collidingArgumentKeysResult`, in the same shape (groups and err from
`domain.RepeatedArgumentKeys`; `err != nil || len(groups) == 0` ⇒ not refused, malformed arguments
left to the tool's own decode). Call it at BOTH sites the colliding check is called —
`resolveAndExecute` (`:401`) and `prepareDelegation` (`:262`) — immediately AFTER the colliding
check, so the fan-out path and the serial path answer such a call identically and the existing
refusal keeps precedence and its wording untouched. The refusal text is pinned by package
constants beside the colliding pair: prefix `invalid arguments: repeated with different values: `,
advice ` — spell each argument once`, joined with `", "` between groups, so a repeated `task`
refuses with exactly
`invalid arguments: repeated with different values: "task" — spell each argument once`.

**Regression guard.** Prose across the tree asserts that the executor RUNS an exact duplicate
last-wins; for the differing case that is now false on the dispatched path. Rule: every comment or
doc line stating the exact-duplicate last-wins contract must also say dispatch refuses the
differing case before it runs — test prose states the contract too, and the rule covers docs as
well as code, so the grep drops its `| grep -v _test` filter. Find them with
`grep -rn 'last-wins\|last wins\|duplicate key' internal/ docs/`. `internal/agent/resolution.go:651`
and `docs/design/confinement-execution-contract.md:634-635` state the duplicated-key-collapses-to-last
contract, and `internal/agent/resolution_test.go:970` ("a duplicated key takes the value the
executor runs") carries the very premise this item refuses. Archived plans under
`docs/plans/archived/` are history and are never amended. That CacheKey digest reading is recorded
as intended and the item yields to it — the digest layer is unchanged; the line gains the
differing-case refusal, nothing more. The `duplicateKeyNote` behaviour itself STAYS —
`internal/tui/toolargs.go:127-133` keeps it as the pane's by-construction reading for a Driver that
skips `resolveAndExecute` — only its prose gains the refusal.

**Files:** `internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`, `internal/tui/toolargs.go`, `internal/tui/doc.go`, `internal/tools/tools.go`, `internal/agent/resolution.go`, `internal/agent/resolution_test.go`, `docs/design/confinement-execution-contract.md`

**Tests:** `TestDispatch_RepeatedArgumentKeysAreRefusedBeforeResolution` and
`TestFanOut_RepeatedArgumentKeysAreRefusedLikeASerialCall`, mirroring `dispatch_test.go:1281` and
`:1371`: the tool never executes and the result is an error. At least one of them asserts the
LITERAL emitted string `invalid arguments: repeated with different values: "task" — spell each
argument once`, not a value rebuilt from the constants. Plus the negative: a call carrying
`{"path":"a","path":"a"}` still RUNS, reaching the tool with the last value. Existing colliding-key
tests unchanged, and `internal/agent/resolution_test.go:970` keeps its assertion green — the digest
layer is untouched; only its subtest name and comment gain the dispatch refusal.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/tui/ ./internal/tools/`

**Commit:** `fix(agent): a call that answers one parameter twice is refused`

## 3. Streamed tool calls accumulate by their wire index

**What:** Recast at the regression check (2026-08-31). `internal/provider/stream.go` accumulates
streamed tool-call fragments with no notion of `index` at all — the rule is "a fragment with an id
starts a new call (flushing the previous), an id-less fragment appends to the open one". A server that interleaves parallel calls by index, or
repeats the id on every fragment, therefore has its calls mis-joined or split. Give `sseToolCall`
an `Index *int` field (a POINTER: index 0 is legal and an absent index must not read as one) and
replace the single `*ToolCall` accumulator with an ordered set of open calls held in a small struct
value on `parseSSE`'s stack:

- a fragment with an index addresses the call at that index, appending its argument text; it may
  OPEN that call (taking the fragment's id and name) only when it also carries an id or a name. An
  index-bearing fragment carrying neither addresses the call already open at that index and falls to
  the most-recently-addressed rule below when none is open — it never opens one, so the provider
  never manufactures a nameless, id-less call;
- a fragment with no index but an id continues the open call of that id when one exists, else opens
  a new call keyed by the id (today a repeated id splits one call into many);
- a fragment with neither appends to the most recently addressed open call — today's `current` rule,
  kept for servers that send neither;
- an id or name arriving later fills in on the call it addresses when that call carries none.

Emission changes with it: nothing is yielded mid-stream. Every open call is yielded in ascending
index order — calls opened without an index keep arrival order after the indexed ones —
immediately BEFORE the terminal `DeltaDone`, on both the `[DONE]` path and the
stream-ended-without-`[DONE]` path, so the `DeltaToolCall`* → `DeltaDone` ordering every consumer
sees today is preserved. `maxToolCallBytes` now bounds the SUM of accumulated argument bytes across
all open calls rather than one call, so a broken server cannot multiply the bound by opening calls;
its error text is unchanged.

**Regression guard.** Comments in `internal/provider/` state the rule this item reverses: amend
every comment naming the flush-on-next-id rule, WHEN a call is emitted, or a per-call
`maxToolCallBytes`. Find them with
`grep -n 'flush\|current\|maxToolCallBytes\|accumulat' internal/provider/stream.go internal/provider/client.go`;
`internal/provider/stream.go:23` states the emission timing this item reverses ("emitted once its
argument fragments are joined"), and `internal/provider/client.go:22` and `:39-40` both state the
per-call cap. The in-band error path
(`chunk.Error`) drops what has not been flushed; with buffering that is now the whole reply rather
than only a call in progress. That is what its comment already intends ("the reply is faulted, not
partly usable") — amend the comment to say every call is dropped, and pin it with the test below.
The item yields to `internal/agent/loop.go:608-622` and `CHANGELOG.md:1494-1500`: the deliberate
drop-and-report of a nameless, id-less native call stands unchanged, and the accumulator never
manufactures such a call for it to report.

**Files:** `internal/provider/stream.go`, `internal/provider/stream_test.go`, `internal/provider/client.go`

**Tests:** In `internal/provider/stream_test.go`, driving raw SSE through `sseServer` as the
existing tests do: two calls opened with id+index 0 and id+index 1 and their argument fragments
interleaved `0,1,0,1` yield two complete calls with their own arguments, in index order (this is
the shape the current code mis-joins); a server repeating the id on every fragment with no index
yields ONE call with the joined arguments; `roundTripSSE` (no index, id-less continuations) stays
byte-identical; the delta sequence puts every `DeltaToolCall` before `DeltaDone`; a stream ending
without `[DONE]` still flushes open calls; an index-bearing fragment carrying neither id nor name,
arriving when nothing is open, is dropped silently exactly as today — no delta of any kind, in
particular no `DeltaError` (`internal/provider` imports no `internal/domain`, so a test here cannot
observe an `ErrorEvent`); an in-band error arriving after one call completed AND a second id-bearing
fragment opened — the second fragment is what flushes the first call at `07daba40`, so the assertion
bites — yields no `DeltaToolCall` and one `DeltaError`; the size cap trips on the sum across two
open calls.
`TestStream_RoundTrip`, `TestStream_EarlyBreakIsClean`, `TestStream_DropsMalformedEvent` and the
`TestWireObserver_*` tests stay green unchanged.

**Acceptance:** `go build ./... && go test ./internal/provider/ && go test ./cmd/apogee/ -run TestE2EDelegationStepCap`

**Commit:** `fix(provider): streamed tool calls accumulate by their wire index`

## Suggested version bump

Not performed by this plan. When these land, `VERSION` (`v0.19.4`) plausibly warrants a micro bump
with a `[Unreleased]` CHANGELOG entry for the refusal and the accumulator — the owner's call.
