# Advise slot for pre-request and post-response — plan

- **Goal:** build ADR 0076 D6's second advise slot: a user `advise:` Reaction on `pre-request` or `post-response`
  lands as a fenced, capped, request-only advice block at the request tail, placed role-safely by the tail's
  role, off under Bypass.
- **Date:** 2026-11-09
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** 1eaef158
- **Sources:** ADR 0076 (`capcom know show "ADR 0076" --with-children`: D3, D6, `adr-0076-amend-1019`,
  `adr-0076-amend-1021`, `adr-0076-amend-1029`); ADR 0006; ADR 0071; glossary `Advise`, `Moment`, `advice-span`,
  `reaction-surface`, `Bypass`; `docs/manual/reactions.md`; issue on the header `**Closes:**` line.
- **Closes:** non-tool-advise-slot
- **Ratified design calls** (owner, 2026-11-09):
  - **Moments:** `pre-request` and `post-response` take `advise:`; `history-rewrite` keeps refusing it, and the
    refusal sentence points to `pre-request`.
  - **Placement (role-safe by tail):** tool tail → trailing fence inside that tool message; user tail → leading
    fence inside that user message (as session-started advice does); assistant tail → one new user message
    holding only the fence. Never a fold into the system message (the `InjectContext` adjacency D6 closes).
  - **Lifetime:** request copy only (the `NoteOnTail` precedent): never in the conversation, snapshot,
    record or Defer queue; gone on the next request.
  - **Ordering:** the advice fence sits ahead of the engine-note rows on the same tail; the wrap-up directive stays last.
  - **Firing:** synchronous through the Reaction cascade, fail-open, `domain.DefaultAdviseTimeout`; re-runs on a
    refold (pre-request runs per request). post-response advice is held on the Agent, Exchange-scoped, and lands
    on the next request's tail; dropped when the Exchange ends.
  - **Config:** no new key; writing an `advise:` entry is the opt-in, `bypass:` the reference arm (amend-1021).
  - **Fence and cap:** `RenderAdvice` unchanged (header names the Moment); per-entry `AdviceCap` via `adviceOf`.
  - **Visibility:** `ReactionFiredEvent` (action advise) → `/advice`; never the transcript.
  - **Delegates:** inherited like post-tool-result advice unless `TopLevelOnly`.
- **Standing requirements:**
  - skills: coding-standards
  - Tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`; never a package
    pattern, never two test runs at once; pass this rule to every sub-agent.
  - Handler output is untrusted: redacted (`tools.RedactSecrets`) and fenced by the span, never by handler text.
- **Out of scope:** `history-rewrite` advise; an aggregate per-request cap; a new Moment; a bench arm (apogee-sim
  evidence welcome, not required); engine-shipped pre-request content.
- **Regression check (2026-11-09, 1eaef158):** items reordered by decision so the admission never lands before
  its routes — the former item 3 is now 2, the former 4 is now 3, the former 2 is now 4; the ids below are the new
  numbers.
  - 1: guard folded.
  - 2: recast (decision: lands before the admission, adds the exported request projection); guard folded.
  - 3: recast (decision: lands before the admission, adds the exported response projection); guard folded.
  - 4: recast (decision: depends on items 2 and 3); guard folded.
  - 5: guard folded.
  - 6: guard folded.
  - 7: guard folded.
  - Re-check round (same base):
  - 2: guard folded (application point inside `fireCascade` at `MomentPreRequest`; delegate case moved to item 4;
    Emit-time observer snapshot; refold case; reactions.go comment sweep).
  - 3: guard folded (decision: shares item 2's application point, ahead of that request's own pre-request
    advice; `t.deferred` drain/restore pattern; cancel drops the rolled-back Turn's own advice; ordering case).
  - 4: guard folded (decision: `docs/manual/reactions.md` `on:` row changes here; stale reorder sentences dropped;
    delegate case from item 2).

## 1. `Request.AdviseTail`: place a fenced advice block on the request tail — ✅ DONE (2026-11-09)

NOTES (2026-11-09): per DECISION, loop-break's resultKey keys on Message.ContentBeforeRequestAdvice (new exported domain method in request.go): Content up to the first standing trailing advice row at pre-request/post-response, so a request-tail fence no longer parts the A-B-A-B results; post-tool-result trailers still key as before. Pinned by TestToolLoopBreakOnAnAlternatingRepeatUnderTailAdvice in internal/floor/loopbreak_test.go (fails with the old m.Content key).
NOTES (2026-11-09): consequential edit — internal/domain/advice.go: made necessary by AdviseTail's mid-ledger insert ahead of an engine note — AdviceSpan doc said "a ledger is append-ordered"; now "kept in offset order", and names the request-tail fence among trailing/leading spans.
NOTES (2026-11-09): an assistant tail's advice-only user message carries a leading-span fence (Content = fence + blank line), so the ledger's fenceStands holds; a system-only or empty request is refused (returns false).

**What:** **Goal:** `internal/domain` offers one request-copy mutator that places a rendered advice fence on the
request's last message by the tail's role (tool → trailing, user → leading, assistant → a new user message),
ahead of any engine-note rows on that tail, records its `AdviceSpan` with correct offsets, and never touches an
earlier message or the conversation the request was built from.
**Regression guard.** Assistant tail: freeze `committedLen` at `len(messages)` before appending the advice-only
message (as `AppendSupersededAssistant` does), so `View()` never shows it and the tool-use, empty-reply and
loop-break guards keep their LastUser/Exchange. Tool tail with engine notes: insert the span in offset order
(before the first engine-note row), shift later rows' `Offset` and copy the ledger, keeping `advice.go`'s
"append-ordered, first trailing row is the earliest cut" true. The fence changes the tool tail's `resultKey`
(`computeToolResultKey`), so loop-break's A-B-A-B rule cannot match while a pre-request advise is armed: name
this as accepted (post-tool-result advise already perturbs the key), or key on Content up to the span's `Offset`
(then add internal/floor/loopbreak.go to Files).
**Approach (assumed at the header base):** the twin of `Request.NoteOnTail` (`internal/domain/request.go`),
reusing `RenderAdvice`, the leading-fence offset shift of `withLeadingAdvice` and the engine-note markers
(`EngineNoteFencePrefix`); no new module.
**Files:** internal/domain/request.go; internal/domain/request_test.go
**Read first:** internal/domain/request.go — Request.NoteOnTail, Request.View, AppendSupersededAssistant;
internal/domain/advice.go — RenderAdvice, withLeadingAdvice, ContentWithoutAdvice, fenceStands;
internal/floor/loopbreak.go — computeToolResultKey
**Tests:** one case per tail role; a tool tail already carrying a wrap-up note keeps the note last, its
`ContentWithoutAdvice` equals the bare tool output and every ledger row passes `fenceStands`; an assistant tail's
`View().Conversation().LastUser()` is not the advice-only message; the source conversation's messages are
unchanged (deep compare); a second request built from the same conversation has no fence.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAdviseTail' ./internal/domain/`
**Commit:** `feat(domain): place a fenced advice block on the request tail by role`

## 2. Fire pre-request advice into the outgoing request — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the advice lands in fireCascade (new adviseRequest helper) after the armed leg and before the deferred SeamClosedEvent, so build, refold and rehome are covered with no change to internal/agent/loop.go, which leaves FILES; fireCascade returns an empty advise slot at pre-request because the slot is already on the request.
NOTES (2026-11-09): the exported request projection is reactions.ProjectRequest (returns any, nil for a nil request), now also what projectSeamValue's pre-request case calls; the sync-lane document carries it under SeamPayload.Value, so seampayload.go's Value doc names that firing beside the five seam-closing notices.
NOTES (2026-11-09): consequential edit — internal/agent/syncexec.go: made necessary by routing pre-request advise through adviseUser — the file-top comment named the advise slot at post-tool-result as runSyncArgv's only advise caller.
NOTES (2026-11-09): adviseUser's default-case wrongHandler now names doc.Event instead of a hard-coded post-tool-result, since the route serves two Moments.
NOTES (2026-11-09): the plan's tests live as six TestPreRequestAdvice* functions in internal/agent/reactions_test.go (tail-only across a tool-call Exchange incl. conv/snapshot/booking, the pre-request-finished observer via an Emit-time snapshotting sink, the stdin document, refold, Bypass, timeout); the entries are appended straight onto a.armed because Reaction.Validate still refuses a user pre-request advise until item 4.

**What:** Recast at the regression check (2026-11-09). **Goal:** each request whose pre-request cascade yields
user advice carries it through `Request.AdviseTail`, fenced and capped, after every pre-request Go handler and
engine note; the advice is booked as a `ReactionFiredEvent` (action advise); Bypass skips it; a refold re-runs it;
a handler error or timeout fails open. Depends on item 1.
**Regression guard.** the pre-request item (new 2) adds an exported request projection in
internal/reactions/payload.go and the post-response item (new 3) an exported response projection there — both
list internal/reactions/payload.go on their Files line. Its tests build the Reaction specs directly in agent
tests, never through load-time validation (item 4 admits the Moment). Apply `AdviseTail` inside the pre-request
pass — inside `fireCascade` when `m == MomentPreRequest`, after the armed leg and before return (payload `t.req`),
so all three fire sites (build, refold, rehome) and the deferred `SeamClosedEvent` are covered at once; not in
`loop.go` after `stampRequestTail` (that runs inside `buildRequest`, before the fire) — so a `pre-request-finished`
subscriber sees the request as sent (`docs/manual/reactions.md` `pre-request-finished` row); if that cannot hold,
state the gap and amend that row and the `domain.SeamClosedEvent` Value doc in this item. The delegate case moves
to item 4: a child arms through `newChildAgentOn` → `armReactions` → `Reaction.Validate`, which refuses a user
pre-request advise until item 4; this item's cases inject specs straight into `a.armed`. Sweep every comment in
internal/agent/reactions.go that lists the Moments the advise slot speaks at
(`grep -n 'post-tool-result seam alone\|but post-tool-result\|closing tool result has\|session-started, or' internal/agent/reactions.go`);
if the projection rides `SeamPayload.Value`, amend internal/domain/seampayload.go's "The five seam-closing notices" doc.
**Approach (assumed at the header base):** in `internal/agent`, `seamPayload`'s `MomentPreRequest` routes
Argv/Webhook/MCP handlers to `adviseUser` with the documented pre-request JSON (`internal/reactions/payload.go`
`requestValue`); `isAdvice` (`reactions.go`) admits pre-request so engine Go advise there is no longer dropped;
`fireLeg` collects advice via `adviceOf` and `bookFiring`; `fireCascade` applies it to `t.req` at
`MomentPreRequest`, after the armed leg and before its deferred `SeamClosedEvent` (all three fire sites, refold included).
**Files:** internal/agent/reactions.go; internal/agent/loop.go; internal/agent/reactions_test.go; internal/reactions/payload.go; internal/domain/seampayload.go (only if the projection rides `SeamPayload.Value`)
**Read first:** internal/agent/reactions.go — fireCascade, seamPayload, adviceOf, adviseUser; internal/agent/loop.go —
refold; internal/reactions/payload.go — requestValue; internal/agent/reactions_test.go — ladderAgent;
internal/agent/harness_test.go — recordingSink
**Tests:** scripted provider: the captured request's tail carries the fence (tool and user tail); the next
request lacks it; `conv` and the record are unchanged; a `pre-request-finished` observer sees the fence — through a
sink that snapshots `req.State().Messages` inside `Emit` (`recordingSink` keeps the `*Request` pointer, which would
show a fence added after the emit); a scripted overflow folds and the rebuilt request carries exactly one fence
and a second firing is booked; Bypass → no fence, no booking; handler timeout → request sent without fence.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPreRequestAdvice' ./internal/agent/`
**Commit:** `feat(agent): land pre-request advise on the outgoing request`

## 3. Hold post-response advice for the next request — ✅ DONE (2026-11-09)

NOTES (2026-11-09): re-derived from "hold collected advice on an Agent field cleared at Exchange end (Agent.exchangeClosed, construct.go)": the held advice lives on turnLifecycle (turn.go, heldAdvice with holdAdvice/takeAdvice/restoreAdvice) beside the deferred queue it mirrors, because that type owns both exits the plan names — closeExchange clears it on every Exchange end (abort and settle included) and end()'s endCancelled row drops the Turn's own advice and holds the taken copy again — which an Agent field could only reach through the argument-less observer; agent.go and construct.go are unchanged and leave FILES. turnLifecycle.restore also clears it (a restored snapshot replaces the conversation it was about).
NOTES (2026-11-09): the advice the request took reaches the pre-request cascade through a new carried parameter on fireCascade, passed by a new firePreRequest(ctx, t) that all three loop.go pre-request fire sites (build, refold, rehome) now call; routeAdvice settles both request-tail slots inside the cascade (post-response holds, pre-request fences carried then its own via slices.Concat), and drops carried under a live Bypass.
NOTES (2026-11-09): the exported response projection is reactions.ProjectResponse(domain.PostResponseMoment), now also what projectSeamValue's post-response case calls; the sync-lane document carries it under SeamPayload.Value.
NOTES (2026-11-09): consequential edit — internal/agent/syncexec.go: made necessary by routing post-response advise through adviseUser — the file-top comment listed the advise slot's Moments as post-tool-result and pre-request.
NOTES (2026-11-09): consequential edit — internal/domain/seampayload.go: made necessary by the post-response projection riding SeamPayload.Value — the Value doc named only the pre-request sync-lane advise firing beside the five notices.
NOTES (2026-11-09): tests are six TestPostResponseAdvice* functions in internal/agent/reactions_test.go (next-request-only across a tool-call Exchange incl. snapshot, conv and the next Exchange; the stdin document; refold re-application; a cancelled retry's own advice dropped while the taken Turn-0 advice is held again; ordering ahead of pre-request advice with DeferredLen 0; Bypass), plus two TestFireBooksTheActionTheOutcomeNames cases (advise-class Retry+Inject books retry, injects once, holds nothing; advise-class Inject books advise and holds one span); entries are appended onto a.armed since Reaction.Validate refuses a user post-response advise until item 4.

**What:** Recast at the regression check (2026-11-09). **Goal:** user advice from a `post-response` Reaction
lands, through `Request.AdviseTail`, on the next request of the same Exchange only, ahead of that request's own
pre-request advice; it never enters the conversation, snapshot or Defer queue and is dropped when the Exchange
ends. Depends on item 2.
**Regression guard.** Per the regression-check decision this item adds an exported response projection in
internal/reactions/payload.go (beside item 2's request projection), and its tests build the Reaction specs
directly in agent tests, never through load-time validation. Held post-response advice lands at the same
application point as item 2's pre-request advice, inside the pre-request pass (`fireCascade` at
`MomentPreRequest`), ahead of that request's own pre-request advice, so pre-request-finished observers and the
ordering rule see both. Hold it with the `t.deferred` pattern in full: `armRequest` drains the Agent's held advice
into `turnRun`; `refold`/`rearmAfterRehome` put it back before re-arming; `endCancelled` first drops the advice
that Turn's own post-response held (a cancel after `applyRetry` rolls back the response it was about, mirroring
`TruncateDeferred(t.deferredFloor)`), then restores the drained copy; the Exchange end (`Agent.exchangeClosed`,
construct.go) clears the Agent copy. Never clear it at `endTurnDone` — that runs before Turn N+1 builds its
request. At post-response `isAdvice` also requires `!out.Retry`, so an engine-origin ClassAdvise
`{Retry, Inject}` injects once and keeps its `retry` label.
**Approach (assumed at the header base):** route `MomentPostResponse` in `seamPayload` to `adviseUser` with the
documented post-response JSON (`responseValue`); hold collected advice on an Agent field cleared at Exchange end
and drained into `turnRun` at `armRequest`; apply it in `fireCascade` at `MomentPreRequest`, as item 2 does.
**Files:** internal/agent/reactions.go; internal/agent/loop.go; internal/agent/turn.go; internal/agent/agent.go; internal/agent/construct.go; internal/agent/reactions_test.go; internal/reactions/payload.go
**Read first:** internal/agent/loop.go — armRequest, refold, applyRetry; internal/agent/turn.go — turnLifecycle.end,
turnRun; internal/agent/construct.go — exchangeClosed; internal/agent/reactions.go — isAdvice;
internal/reactions/payload.go — responseValue
**Tests:** post-response advice on Turn N appears on Turn N+1's request only; a refold before that request is
answered re-applies it; an Exchange ending on that response drops it (next Exchange's first request has none);
snapshot after Turn N carries none; a cancel after a retried re-stream drops that Turn's own advice; with both a
post-response and a pre-request advise armed, Turn N+1's tail carries the post-response fence first and
`conv.DeferredLen()` stays 0; Bypass skips; a Retry+Inject ClassAdvise case in
`TestFireBooksTheActionTheOutcomeNames` books `retry` and injects once.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPostResponseAdvice|TestFireBooksTheActionTheOutcomeNames' ./internal/agent/`
**Commit:** `feat(agent): carry post-response advise to the next request`

## 4. Admit `advise:` at pre-request and post-response

**What:** Recast at the regression check (2026-11-09). **Goal:** a user Reaction with `advise:` on `pre-request`
or `post-response` validates and loads; `history-rewrite` and every other non-advise Moment still refuse it with
a sentence listing the six advise Moments and pointing `history-rewrite` users to `pre-request`. Depends on
items 2 and 3.
**Regression guard.** the admission must never land before the routes exist (between them a user pre-request
advise fails seamPayload's PreRequestFunc assertion and abandons every Turn): it lands after items 2 and 3. Flip the two refusal cases that go red in `reaction_test.go` ("an argv handler advising on another
seam is refused", "an mcp handler advising at pre-request is refused") to validating cases and add
`history-rewrite` refusal cases (argv and mcp) pinning the new sentence; update every comment listing the advise
Moments — `ArgvHandler`, `WebhookHandler`, `MCPHandler` in `reaction.go` and the
`TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler` comment (`grep -n 'compaction-finished' internal/domain/reaction*.go`).
The refusal sentence quoted in docs/manual/reactions.md (the `on:` row) changes in this item's own commit, so
Files names docs/manual/reactions.md for that one line; item 7 keeps the rest of the manual sweep. Carries the
delegate case moved from item 2: a delegate inherits a user pre-request advise once `Reaction.Validate` admits it.
**Approach (assumed at the header base):** widen `Reaction.Validate` (`internal/domain/reaction.go`) and its
refusal sentence; update the tests pinning that sentence in `internal/domain/reaction_test.go` and
`internal/config/reactions_test.go`.
**Files:** internal/domain/reaction.go; internal/domain/reaction_test.go; internal/config/reactions_test.go; internal/agent/reactions_test.go; docs/manual/reactions.md (the `on:` row's refusal sentence only)
**Read first:** internal/domain/reaction.go — Reaction.Validate, ArgvHandler, WebhookHandler, MCPHandler;
internal/domain/reaction_test.go — TestReactionValidateAppliesThePerClassRulesToAnAsyncHandler;
internal/config/reactions_test.go — TestLoadFileConfigRefusesMalformedReactions, TestLoadFileConfigAdmitsSessionStartedAdvice; internal/agent/reactions.go — seamPayload
**Tests:** validate accepts both Moments; refuses `history-rewrite` with the new sentence (pinned exactly); the
two named refusal cases now validate; a config case beside `TestLoadFileConfigAdmitsSessionStartedAdvice` admits
a pre-request and a post-response `advise:` entry; `TestPreRequestAdviceReachesADelegate`: a delegate's request
carries the parent's pre-request fence (not under `TopLevelOnly`); the manual's `on:` row quotes the new sentence.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReaction' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPreRequestAdviceReachesADelegate' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLoadFileConfig(RefusesMalformedReactions|Admits)' ./internal/config/`
**Commit:** `feat(domain): admit advise reactions at pre-request and post-response`

## 5. Count pre-request advise in the context probe

**What:** **Goal:** the context-cost probe counts a configured pre-request advise entry in the Turn-1 request
weight, as it counts session-started advice. Depends on items 2 and 4.
**Regression guard.** `internal/probe.ContextCost` only renders the numbers it is handed and
`armedDirectiveReactions` already counts every ClassAdvise, so the test lives in `cmd/apogee/probecontext_test.go`
as the twin of `TestProbeContextLiveMeasuresSessionStartAdviceAtTurn1`, and contextcost.go/_test.go leave Files
unless a row really changes. Reword the `newProbeAgent` comment (probecontext.go) to name pre-request beside
session-started ("only one on session-started rides the Turn-1 request" is false after item 2).
**Approach (assumed at the header base):** `internal/probe/contextcost.go` and `cmd/apogee/probecontext.go`.
**Files:** cmd/apogee/probecontext.go; cmd/apogee/probecontext_test.go
**Read first:** cmd/apogee/probecontext.go — measureTurn1, armedDirectiveReactions, newProbeAgent;
cmd/apogee/probecontext_test.go — TestProbeContextLiveMeasuresSessionStartAdviceAtTurn1, probeContextLiveStub,
runProbeContext; internal/probe/contextcost.go — ContextCost.Report
**Tests:** `TestProbeContextLiveMeasuresPreRequestAdvice`: with a pre-request advise entry, `--live`'s
as-configured Turn-1 request tail carries the pre-request fence and the Bypass request none.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestProbeContextLiveMeasuresPreRequestAdvice' ./cmd/apogee/`
- `go vet ./cmd/apogee/`
**Commit:** `feat(probe): count pre-request advise in the Turn-1 context cost`

## 6. E2E: a user pre-request advise reaches the wire on both wires

**What:** test-only. An e2e test in `cmd/apogee` configures a `reactions:` entry with `on: pre-request` and an
`advise:` argv script, sends a prompt against the stub upstream on the OpenAI-compatible wire and on
`wire: anthropic`, and asserts the captured request body's last message carries the pre-request fence exactly as
`RenderAdvice` emits it, that no two consecutive user messages reach the Anthropic body, and that the second
request carries no Turn-0 fence. Depends on items 2, 3 and 4.
**Regression guard.** The first Turn is 0 (`turnLifecycle.index` is 0-based): build the expected fences with
`domain.RenderAdvice` — `AdviceSpan{Moment: pre-request, Turn: 0, Leading: true}` on request 1's user tail;
request 2 has no Turn-0 fence and a Turn-1 trailing fence on its tool tail. stubllm decodes Anthropic
tool_result blocks to role `tool`, so assert user/user adjacency over `Request.Body`'s raw `messages[].role`;
`headlessHooksArgs` cannot set `wire:` (it sits inside `servers:`), so build that home from `wireHome` plus the
reactions block.
**Files:** cmd/apogee/e2e_advise_test.go
**Read first:** cmd/apogee/e2e_reactions_test.go — TestE2EAdviseSessionStartLeadsTheFirstRequest, adviseConfigOn,
headlessHooksIn; cmd/apogee/e2e_wire_anthropic_test.go — wireHome, assertRequestsOnWire; internal/stubllm/log.go —
Request.Body; internal/domain/advice.go — RenderAdvice, withLeadingAdvice
**Tests:** the new e2e test.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EPreRequestAdvise' ./cmd/apogee/`
**Commit:** `test(apogee): pre-request advise reaches both wires request-only`

## 7. Docs: the second advise slot

**What:** **Goal:** the manual, glossary and ADR 0076 describe advise at `pre-request` and `post-response`, the
per-tail placement and its request-only lifetime. Depends on item 6.
**Regression guard.** The sweep rule is "every doc or comment that lists where advise fires", found with
`grep -rn -E 'session-started|compaction-finished|post-tool-result|tool result' docs/manual README.md .capcom/knowledge/glossary internal cmd --include='*.md' --include='*.go'`
and read hit by hit; it replaces the single-line grep below, which misses wrapped sites (README.md Reactions row,
probe.md, reactions.md's Moment overview, configuration.md, glossary reaction-surface and advice-span). The
edited pages are pinned by the Floor-guard count drift tests, so Acceptance runs them too.
**Approach (assumed at the header base):** dated amendment `adr-0076-amend-1109` records that D6's "fenced
user-role message at the tail" is fixed per tail role, request-only, and that the Rejected row "a dedicated tail
message for every advise" holds (only an assistant tail gets a new message); named on ADR 0076's Status line and
reading order. Rule for the sweep: every doc site stating which Moments take `advise:` —
`grep -rn 'advise' docs/manual/ README.md .capcom/knowledge/glossary/ | grep -i 'moment\|on:\|post-tool-result'`
— covering at least `docs/manual/reactions.md` (Moment list, `on:` row, seam payloads), `configuration.md`,
`commands.md` (`/advice`), `probe.md`, `README.md`, glossary `advice-span`, `moment`, `reaction-surface`, and the
`internal/tui/advicepane.go` comments.
**Files:** docs/manual/reactions.md; docs/manual/configuration.md; docs/manual/commands.md; docs/manual/probe.md; README.md; internal/tui/advicepane.go; .capcom/knowledge/decisions/adr-0076.md; .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1109.md; .capcom/knowledge/glossary/ (advice-span, moment, reaction-surface Notes)
**Read first:** docs/manual/reactions.md — Moment list, seam payload table; .capcom/knowledge/decisions/adr-0076.md —
Status line, reading-order list; .capcom/knowledge/decisions/adr-0076/adr-0076-decision.md — D6;
adr-0076/adr-0076-rejected.md — "A dedicated tail message for every advise"; internal/config/reactions_test.go — TestManualStatesTheFloorGuardCount
**Tests:** `TestManualStatesTheMomentCount` still passes (no Moment added); the Floor-guard count drift tests
still pass.
**Acceptance:**
- `capcom check`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualStatesTheMomentCount' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualStatesTheFloorGuardCount|TestReadmeStatesTheFloorGuardCount' ./internal/config/`
**Commit:** `docs: describe advise at pre-request and post-response`
