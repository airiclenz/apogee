# Plan — Give the Turn a lifecycle owner (depth-review candidate 01)

**Date:** 2026-07-24
**Status:** **DRAFT — grounded, not yet grilled.** Drafted from
`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md` (candidate 01, the review's
top recommendation). Every claim below was re-verified at file level this session; the design
decisions are the drafter's calls and are listed as the grill agenda. If the owner ratifies
them (or after a grill session amends them), `/implement-plan` executes item-by-item.
**Track:** architecture deepening. `internal/agent` only — no public Go API change, no
session-schema change (`agentState` JSON stays byte-compatible), no ADR conflict. Every code
item is **behavior-preserving**: the existing test suite is the harness and must stay green
untouched except where an item explicitly says it adds tests.

**Design decisions taken in this draft (the grill agenda — challenge these, not the goal):**

1. The owner is an unexported same-package module: type `turnLifecycle` in
   `internal/agent/turn.go`, held as `Agent.turns`, holding `*domain.Conversation` and
   `*selfRegulator` by reference (wired in `newAgent` after the Agent literal, so `&a.conv`
   exists). Not a new package — the Turn is the loop's concept (ADR 0007) and the tracker is
   unexported.
2. The three exit helpers become **one entry point** `end(t, how)` driven by a `turnEnd`
   enum — the "exits as a table" shape, mirroring `resolve()`'s outcome-driven single function
   (`internal/agent/resolution.go`, the in-package yardstick) — not three prettier methods.
3. The one-fold-per-Turn latch changes encoding: the `maxOverflowRecoveries` attempt-counter
   arithmetic is retired for an explicit `turnRun.foldSpent` bool. ADR 0018's *rule* (one fold,
   one retry, then give up) is untouched; only its in-code carrier changes. The ADR itself
   needs no amendment — it specifies the behavior, not the counter.
4. The three `restoreDeferred`-immediately-before-abandon calls are deleted as dead motion
   (the abandon path's `closeExchange` clears the whole queue two lines later — F6). Pinned by
   a new test, not silently dropped.
5. The `loop.go` file split (the review's "related smaller" item) is folded into this plan as
   the final code item — it touches the same file and is one mechanical commit.

## Why

The Turn — begin / overflow-recover / end — is the loop's central concept and has no module.
The rules live in three exit helpers that each touch a **different subset** of
{tracker, turnIndex, rollback, deferred queue, inExchange}, related only by 15-line defensive
comments; the overflow recovery ritual is copy-pasted; the per-Turn working values travel as
five positional locals re-derived at three sites. Concretely (all verified 2026-07-24):

- **Three exits, three different state subsets** (`internal/agent/loop.go`):
  `completeTurn` (L763: judge → maybe close Exchange → advance), `abandonTurn` (L781:
  discard → close Exchange → advance), `cancelTurn` (L807: discard → roll back conversation →
  truncate-then-restore deferred → **no** advance, `inExchange` deliberately untouched), plus
  `closeExchange` (L754) as the shared Exchange-end owner. Which subset each touches and why
  is comment-lore, not structure.
- **The fold-and-rebuild ritual is copy-pasted** at the predictive guard (L338–357) and the
  reactive recovery (L402–431): `restoreDeferred → emergencyFold → ctx.Err() check →
  re-derive {rollback, req, deferred, deferredFloor}`. The re-derive trio
  (`rollback = conv.Len(); req, deferred = buildRequest; deferredFloor = DeferredLen()`)
  appears **3×** (L317–323, L355–357, L429–431). *(The review doc said "`buildRequest`
  reconstructed 7×" and "`exchangeStart` written in 5 places"; verified counts are 3 call
  sites in `step()` and 4 write sites — the correction changes nothing material.)*
- **`emergencyFold` leaks its contract onto the caller** (`compact.go` L205): it returns a
  bare `bool` and delegates "check `ctx.Err()` yourself to tell a cancel from a decline"
  to both call sites, each of which then re-derives the four stale locals by hand.
- **`exchangeStart` has 4 write sites** and one reader seam: `step()`'s Exchange opening
  (loop.go L281), the S2 shrink repair (L310 — an inline `min(max(...))` clamp pinned only by
  an integration test), `emergencyFold`'s bridge re-anchor (compact.go L226), and
  `restoreState` (state.go L80). The clamp arithmetic is not unit-testable where it lives.
- **Test friction:** Turn/Exchange/self-reg state is only assertable by reaching through the
  Agent — **163 `a.conv` + 22 `a.tracker` reach-ins** across the package's tests (verified by
  grep), each needing a scripted `step()` run through a fake responder just to get the state
  into position.

The deepening: a small `turnLifecycle` module owning the state tuple and the exits, a
`turnRun` value owning the per-Turn working set, and one fold-and-rebuild function returning
fresh working values. `step()` reads as a loop again; the exit rules become one table-tested
function; the clamp becomes a unit-testable method. Highest leverage + locality of the
review's seven candidates, and the target shape already exists in the same package —
`resolve()` is the yardstick.

## Where things stand (grounded, verified 2026-07-24)

- **The lifecycle fields** (`internal/agent/agent.go` L81–90): `conv`, `pendingInput`,
  `inExchange`, `exchangeStart`, `turnIndex` (plus `compacting`/`compactSat`, which stay with
  Compaction). Non-loop readers/writers that must be re-pointed when fields move: `Submit`
  (L123), `AbortExchange` (L174–182), `exchangeBoundary` (L196), `ClearContext` (L280),
  `Compact` (compact.go L43), `shouldAutoCompact` (L130), `emergencyFold` (L225–227),
  `compactCompleter.Complete` (L261, reads `turnIndex`), and `encodeState`/`restoreState`
  (state.go L54–57, L78–81).
- **Serialization contract** (state.go): `agentState` marshals
  `{conversation, turnIndex, inExchange, exchangeStart, pendingInput}`. The JSON schema is
  version-gated (`domain.SessionVersion`) and **must not change**. The tracker is per-Session
  and deliberately not serialized.
- **`cancelTurn`'s five positional parameters** (`turn, rollback, deferred, deferredFloor,
  start`) are exactly the per-Turn working set `step()` threads as locals — the `turnRun`
  fields.
- **Dead motion at the abandon sites:** loop.go L363, L395, and L433 call
  `restoreDeferred(deferred)` and then `abandonTurn`, whose `closeExchange` →
  `conv.ClearDeferred()` empties the queue regardless (the F6 comment at L788–790
  acknowledges the restore is expired on the spot). No observer runs between restore and
  clear, so deleting the three restores is behavior-identical.
- **The predictive/reactive asymmetry that the fold function must preserve:** on a declined
  fold the predictive path **proceeds unfolded** and *must* re-derive (its `restoreDeferred`
  refilled the queue; re-running `buildRequest` re-drains it — skipping the re-derive would
  double-deliver corrections next Turn), while the reactive path **gives up**. On a cancelled
  fold both paths route to the cancel exit with the *pre-fold* working values, whose
  truncate-then-restore leaves the corrections queued exactly once (loop.go L341–345).
- **Existing pins that must stay green untouched:** `overflowrecovery_test.go`,
  `emergencyfold_test.go`, `predictiveguard_test.go`, `autocompact_guard_test.go`,
  `statemachine_test.go`, `TestExchangeStartRepairedAfterMidExchangeTruncation`, and the
  snapshot/resume round-trip tests.
- **`loop.go` is 1172 LOC** — the package's largest file — carrying four separable clusters:
  construction (`newAgent` … `validateConfig`, ~L57–248), the loop proper, the exits, and the
  domain→wire translation (`toProviderRequest` … `toProviderSampling`, ~L1058–1165).
- **CONTEXT.md** already defines **Turn**, **Exchange**, and **Step** (§"Turns and stepping")
  and records the cached-boundary exception (ADR 0017 §2). No existing term names the Turn's
  *exits*; item 6 adds the minimal wording.

## 1. `turn.go` — introduce `turnLifecycle` and relocate the lifecycle fields — ✅ DONE (2026-07-24)

**NOTES (2026-07-24):** Applied the mechanical test reach-in renames (`a.turnIndex`→`a.turns.index`,
`a.inExchange`→`a.turns.inExchange`, `a.exchangeStart`→`a.turns.exchangeStart`) across 5 test files
(`autocompact_guard_test.go`, `minilang_test.go`, `emergencyfold_test.go`,
`overflowrecovery_test.go`, `predictiveguard_test.go`) exactly as this item's Acceptance authorizes —
the field relocation does not compile otherwise. No test logic, assertion, or expectation changed,
so this is consistent with the plan-wide "existing test suite stays green unedited" (which governs
behavioral test edits). Also added `TestAgentState_EncodesStableKeyNames` to `state_test.go`: only
`exchangeStart`'s JSON key was previously pinned (via `TestSnapshot_RoundTripsExchangeBoundaryForAbort`),
`turnIndex`/`inExchange` key names were not — the item's conditional "add one small marshal-and-assert-keys
test" therefore applied.

**What:** Create `internal/agent/turn.go` with the owner type, and move the three lifecycle
fields off `Agent` into it — a mechanical relocation, no semantics change:

```go
// turnLifecycle owns the loop's Turn/Exchange lifecycle state — where the loop stands
// between quiescent boundaries (ADR 0007) — and, from item 2 on, the exits that mutate it.
// It coordinates the two collaborators the exits touch together: the conversation (rollback,
// deferred queue) and the self-regulator (judge vs discard). Same-package, unexported: the
// Turn is the loop's concept, not a public seam.
type turnLifecycle struct {
	conv    *domain.Conversation
	tracker *selfRegulator

	index         int  // 0-based index of the next Turn (was Agent.turnIndex)
	inExchange    bool // true between Submit and the Step that completes the Exchange
	exchangeStart int  // cached rollback boundary of the open Exchange (ADR 0017 §2's recorded fallback)
}
```

`newAgent` constructs the Agent literal first, then wires `a.turns =
&turnLifecycle{conv: &a.conv, tracker: a.tracker}` (the pointer targets the field, so
`restoreState`'s value-assign to `a.conv` stays visible through it). Re-point every
reader/writer listed in *Where things stand* to `a.turns.index` / `a.turns.inExchange` /
`a.turns.exchangeStart` (same-package field access; accessor methods are not needed yet).
`pendingInput` **stays on Agent** — it is input queueing (Submit's half), not Turn lifecycle.
Carry each field's doc comment with it, including `exchangeStart`'s ADR 0017 §2 reference.
`encodeState`/`restoreState` read/write through `a.turns`; the `agentState` struct and its
JSON tags are untouched.

**Tests:** none new — this item is mechanical. The full suite is the harness; the
snapshot/resume round-trip tests prove the JSON payload is unchanged. If no existing test
pins the encoded key names (`turnIndex`, `inExchange`, `exchangeStart`), add one small
marshal-and-assert-keys test in `state_test.go` so a future field move cannot silently
rename the schema.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` clean with zero edits to
existing tests beyond the mechanical `a.turnIndex` → `a.turns.index` (etc.) reach-in renames.
Commit: `refactor(agent): move the Turn/Exchange lifecycle state onto a turnLifecycle owner`.

## 2. The exit table — `turnRun` + one `end()` replacing the three exit helpers — ✅ DONE (2026-07-24)

**NOTES (2026-07-24):** Implemented as written — `turnRun`/`turnEnd`/`end()` in `turn.go`,
`closeExchange`+`restoreDeferred` moved onto the owner, the three exit helpers deleted, and the
three dead `restoreDeferred`-before-abandon calls removed (verified dead: no observer runs between
restore and the abandon path's `closeExchange`→`ClearDeferred`; `revision` bumps are runtime-only,
unserialized). Minor consequential edits beyond the two named files, all forced by deleting the
symbols: (a) two dangling comment references to `cancelTurn` updated — `AbortExchange`'s doc in
`agent.go` and `emergencyFold`'s cancel comment in `compact.go` — now point at end()'s `endCancelled`
row; (b) the comments at the three deleted-restore sites were rewritten so they no longer describe
the removed re-queue (they now note the abandoned Exchange clears the queue via F6); (c)
`restoreDeferred`'s doc narrowed "(cancelled or abandoned)" → "(cancelled)" since the abandon
callers are gone. Left OUT OF SCOPE (internal/agent-only track): `domain/hooks.go`'s `ClearDeferred`
doc still names `completeTurn`/`abandonTurn` as example call sites — cross-package doc rot for a
later docs pass, not touched here.

**What:** In `turn.go`, add the per-Turn working value and the single exit entry point; in
`loop.go`, delete `completeTurn`, `abandonTurn`, `cancelTurn`, and move `closeExchange` and
`restoreDeferred` onto the owner.

```go
// turnRun is the working state of one Turn attempt — the values step() used to thread as
// five positional locals (and cancelTurn took as five positional parameters). It is created
// at the top of step() and dies at the Turn's exit; nothing in it is serialized.
type turnRun struct {
	turn          int       // this Turn's index (StepResult.TurnIndex)
	start         time.Time // Turn start (StepResult.Elapsed)
	rollback      int       // conversation boundary a cancel restores to
	req           *domain.Request
	deferred      []string  // corrections buildRequest drained; a cancel re-queues them
	deferredFloor int       // queue length after the drain — the cancel truncation floor (F6)
}

// turnEnd names the four ways a Turn exits. One row per exit; end() is the whole table.
type turnEnd int

const (
	endTurnDone     turnEnd = iota // judged · advance · Exchange stays open   · StatusTurnComplete
	endExchangeDone                // judged · advance · Exchange closes       · StatusExchangeComplete
	endAbandoned                   // discarded · advance · Exchange closes    · StatusExchangeComplete
	endCancelled                   // discarded · roll back + restore deferred · no advance · Exchange stays open · StatusCancelled
)

func (l *turnLifecycle) end(t *turnRun, how turnEnd) domain.StepResult
```

`end()` implements each dimension exactly once — judge (`tracker.endTurn`) vs discard
(`tracker.discardTurn`); the cancel row's `conv.DropRange(t.rollback, conv.Len())` +
`conv.TruncateDeferred(t.deferredFloor)` + restore of `t.deferred`; `closeExchange()` on the
two Exchange-ending rows; `index++` on every row **except** `endCancelled` — and returns the
`StepResult`. The load-bearing comments migrate with their rules, condensed onto the rows
they explain: R3's judge-vs-discard rationale, F6's deferral-dies-with-its-Exchange, and
`cancelTurn`'s "inExchange deliberately untouched / no advance ⇒ resume re-attempts +
Submit stays rejected" block (loop.go L795–806), which is the single most load-bearing
comment in the file and must survive verbatim in substance.

`step()` builds `t := &turnRun{turn: a.turns.index, start: time.Now()}` at the top, fills
`rollback`/`req`/`deferred`/`deferredFloor` at the same points the locals are assigned
today, and every `return a.xxxTurn(...)` becomes `return a.turns.end(t, endXxx), nil`.
`AbortExchange` keeps calling `closeExchange` (now `a.turns.closeExchange()`).

Delete the three dead `restoreDeferred`-before-abandon calls (loop.go L363, L395, L433 —
see *Where things stand*); the abandon row's queue-clear is the whole effect either way.

**Tests:** new `turn_test.go`: a table test driving `end()` **directly** — construct a
`turnLifecycle` over a scripted `domain.Conversation` (committed messages + a populated
deferred queue) and a `selfRegulator` with a pending fire — asserting per row: returned
`Status`/`TurnIndex`/`Elapsed > 0`, index advanced or held, `inExchange` after, conversation
length (rollback applied or not), deferred queue contents (cleared / truncated-then-restored
exactly once / untouched on `endTurnDone`), and tracker effect (judged: pending set rotated;
discarded: turn scratch cleared, pending set intact — readable directly, same package). Plus
one pin for the dead-restore deletion: an abandoned Turn leaves the deferred queue empty.
This is the deepening's testability payoff: these assertions previously required scripted
`step()` runs through a fake responder.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` clean; existing suite
untouched and green; `completeTurn`/`abandonTurn`/`cancelTurn` no longer exist. Commit:
`refactor(agent): unify the three Turn exits behind one table-tested end()`.

## 3. `refold` — collapse the fold-and-rebuild ritual; retire the recovery counter — ✅ DONE (2026-07-24)

**What:** In `loop.go` (beside `step()`), one function owning the ritual both overflow paths
copy today, and a named helper for the re-derive trio:

```go
// armRequest (re)derives the Turn's request-scoped working values from the current
// conversation: the rollback boundary, the request (draining the deferred queue), and the
// queue's post-drain floor. Called once when the Turn first builds its request, and by
// refold after the conversation is rewritten.
func (a *Agent) armRequest(t *turnRun)

// foldOutcome classifies refold's result for the caller's routing.
type foldOutcome int

const (
	foldFolded    foldOutcome = iota // history rewritten; t re-derived; t.foldSpent latched
	foldDeclined                     // nothing folded (opted out / nothing to shed / summary fault); t re-derived unchanged
	foldCancelled                    // ctx cancelled mid-summary; t untouched — route to end(t, endCancelled)
)

// refold re-queues t's drained corrections, runs the emergency fold, and re-derives t's
// working values from the (possibly folded) conversation — the ritual both overflow paths
// previously copied, including the ctx check emergencyFold delegates to its caller.
func (a *Agent) refold(ctx context.Context, t *turnRun) foldOutcome
```

Semantics locked to today's, per *Where things stand*: re-derive on **both** `foldFolded`
and `foldDeclined` (load-bearing for the predictive path's unfolded proceed; harmless for
the reactive path's give-up); leave `t` untouched on `foldCancelled` so the cancel exit's
truncate-then-restore re-queues the corrections exactly once. `emergencyFold` itself is
unchanged and becomes `refold`-only — its "check ctx.Err() yourself" contract now has one
caller, inside the module that documents it.

Add `foldSpent bool` to `turnRun` (latched by `refold` on `foldFolded`), retire
`maxOverflowRecoveries` and the `recoveries` local, and move the one-fold-per-Turn doc
comment (loop.go L34–40) onto the field. The predictive guard becomes a `switch` on
`refold`; the respond loop drops its `attempt` arithmetic (`for attempt := recoveries;;`
→ `for {}`) and gives up on `outcome != turnOverflowed || t.foldSpent`. The post-response
retry counter (`maxPostResponseRetries`, inside `respondAndReview`) is a **different**
counter and is untouched.

**Tests:** existing `overflowrecovery_test.go` / `emergencyfold_test.go` /
`predictiveguard_test.go` are the behavior pins and must pass unedited — they cover fold
succeeds/declines/cancels on both paths, the one-fold budget shared between them, and the
second-overflow give-up. Add a focused `refold` unit in `turn_test.go` (or
`overflowrecovery_test.go`): outcome mapping and `t`-mutation per outcome — cancelled leaves
`t` (and the just-restored queue) intact; declined re-derives with an unchanged
conversation; folded re-derives against the folded conversation and latches `foldSpent`.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` clean, existing overflow
tests green unedited; the ritual appears exactly once. Commit:
`refactor(agent): collapse the overflow fold-and-rebuild ritual into refold`.

## 4. Lift the Exchange-boundary mutations onto the owner

**What:** The four `exchangeStart` write sites become three intention-named methods on
`turnLifecycle` (the fourth, `restoreState`, already writes through the owner since item 1):

```go
// openExchange marks the boundary a new Exchange opens at — before its first user message —
// and flips inExchange. Called once per Exchange (Submit is refused mid-Exchange).
func (l *turnLifecycle) openExchange()               // exchangeStart = conv.Len(); inExchange = true

// reanchorAfterShrink repairs the cached boundary after a mid-Exchange history rewrite
// dropped `dropped` messages (S2): shift down by the delta, clamped to
// [conv.PrefixEnd()+1, conv.Len()]. A grow or an out-of-Exchange rewrite is a no-op.
func (l *turnLifecycle) reanchorAfterShrink(dropped int)

// anchorAtBridge re-anchors the boundary to the just-appended overflow bridge after a
// mid-Exchange emergency fold (ADR 0018), so AbortExchange rolls back to the folded
// prefix + summary rather than into the protected prefix. No-op outside an Exchange.
func (l *turnLifecycle) anchorAtBridge()             // if inExchange { exchangeStart = conv.Len() - 1 }
```

`step()`'s opening block calls `a.turns.openExchange()` where it sets `exchangeStart` today
(the user-message `Append` follows; `inExchange` moves into the call — no reader runs
between, so the reorder is inert). The S2 repair site keeps computing
`dropped := beforeRewrite - a.conv.Len()` and delegates guard + clamp to
`reanchorAfterShrink`. `emergencyFold`'s re-anchor becomes `a.turns.anchorAtBridge()`.
`Agent.exchangeBoundary()` stays the one public-side reader seam (ADR 0017 §2) and forwards
to the owner. Each site's defensive comment (the S2 rationale at loop.go L298–308, the
bridge rationale at compact.go L191–199) migrates onto the method it now describes.

**Tests:** table-test `reanchorAfterShrink`'s clamp directly in `turn_test.go` — shift
within span, floor clamp at `PrefixEnd()+1`, ceiling clamp at `Len()`, zero/negative
`dropped` no-op, not-in-Exchange no-op — the arithmetic that today is pinned only through
the `TestExchangeStartRepairedAfterMidExchangeTruncation` integration test, which stays as
the end-to-end pin. One direct case each for `openExchange` and `anchorAtBridge`.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` clean; no direct
`exchangeStart =` writes remain outside `turn.go` + `restoreState`. Commit:
`refactor(agent): lift the Exchange-boundary mutations onto the lifecycle owner`.

## 5. Split `loop.go` — construction and wire translation move out

**What:** After items 1–4, `loop.go` still opens with the construction cluster and closes
with the domain→wire translation cluster — two deep, self-contained groups trapped in the
god-file (1172 LOC pre-plan). Move, verbatim:

- → **`construct.go`**: `newAgent`, `libraryMechanismID`, `buildEnabledMechanisms`,
  `resolveTools`, `hostTools`, `resumeAgent`, `validateConfig`, and the `errMissing*` vars
  (construction-only). `errHookPanicked` stays beside its users (the hook-runner path).
- → **`wire.go`**: `toProviderRequest`, `toolInstructions`, `injectSystemInstructions`,
  `toProviderToolCalls`, `toProviderTools`, `toProviderSampling` — the ADR 0010 translation
  boundary, with its cluster comment.

`loop.go` keeps the loop proper: `step`, `respondAndReview`, `assembleResponse`,
`streamResponse` + delta emitters, `parseToolCalls`, `assistantMessage`, `buildRequest`,
`armRequest`/`refold`, the guards, and the ref/skill resolvers. Move-only — no signature,
comment, or import-graph change beyond what the move forces.

**Tests:** none new (pure relocation); the suite is the harness.

**Acceptance:** `go build ./... && go vet ./... && go test ./...` clean; `gofmt -l` empty;
diff is verifiably move-only (deletions in `loop.go` match insertions in the two new files).
Commit: `refactor(agent): split loop.go construction and wire-translation clusters into their own files`.

## 6. Docs — CONTEXT.md wording and the CHANGELOG entry

**What:** Two small edits:

- **CONTEXT.md §"Turns and stepping", the `Turn` entry:** append one sentence naming the new
  structure so the term stays navigable: the Turn's lifecycle — its opening, its one
  permitted overflow fold, and its four exits (complete / Exchange-complete / abandoned /
  cancelled) — is owned by the loop's `turnLifecycle` module (`internal/agent/turn.go`).
  No new glossary term: "Turn" already names the concept; the entry just gains its code
  anchor, exactly as the `Exchange` entry anchors `ExchangeView`.
- **CHANGELOG `[Unreleased]` › Changed:** one entry — internal refactor, no behavior or API
  change: the agent loop's Turn lifecycle (exits, overflow fold-and-rebuild, Exchange
  boundary maintenance) is consolidated into a table-tested `turnLifecycle` module, and
  `loop.go`'s construction and wire-translation clusters moved to `construct.go`/`wire.go`.

**Tests:** none (docs).

**Acceptance:** CONTEXT.md's Turn entry names the owner; CHANGELOG reads true against the
landed code. Commit: `docs(context,changelog): record the Turn lifecycle owner`.
