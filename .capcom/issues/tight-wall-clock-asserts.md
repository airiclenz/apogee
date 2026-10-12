---
title: Six tight wall-clock assertions in unit tests can fail on a loaded box
status: done
type: bug
priority: 2
labels: [slow-box]
aliases: [apogee-7fmu]
created: 2026-09-22T16:03:47Z
closed: 2026-09-23T06:02:34Z
reason: Closed
---

The slow-box sweep of 2026-09-22 cured the TUI test kit (internal/tuitest, cmd/apogee e2e helpers). These are the wall-clock assertions left OUTSIDE it, each a hard upper bound or a fixed sleep that a loaded machine breaks. Several want a different assertion rather than a bigger number.

- internal/tools/network_funnel_test.go:982 — slack = 250ms on an 850ms ceiling around a real resolver swap, an httptest dial and a goroutine handoff under -race. The sibling at :910 (elapsed > 20*budget) makes the same claim safely; copy that shape.
- internal/doctext/pdf_test.go:486 — elapsed > 100ms proxying "the refusal is decided before the parser allocates". Measure the allocation (the file already does this in TestExtractPDF_RefusesAnInflateBomb), do not time it.
- internal/provider/reliability_test.go:192 — elapsed >= retry429BaseDelay (1s) bounds two real httptest round trips with no margin.
- internal/agent/promptslot_test.go:79 and :124, internal/agent/approvalqueue_test.go:224, internal/agent/approvalcache_test.go:156, internal/domain/promptslot_test.go:59, internal/tools/askqueue_test.go:71 — time.Sleep(20ms) "long enough for the second caller to be genuinely waiting". A fixed sleep asserting another goroutine has reached a blocking point is the assumption a loaded machine breaks; in the queue tests it can invert the expected ordering. Needs a synchronisation point, not a longer sleep.
- internal/filewatch/filewatch_unix_test.go:51 — Sleep(testSettle/3) to land a second write INSIDE a 150ms settle window; a descheduled goroutine misses it and the coalescing claim is not exercised.
- internal/reactions/command_test.go:129-130 and internal/userexec/userexec_test.go:129,137 — ~800ms of real margin around two /bin/sh cold starts and a process-group kill under -race.
- cmd/apogee/e2e_stream_test.go:70 streamReplyWait = 15s, derived from a measurement on one box; cmd/apogee/e2e_approval_test.go:413 promptly = 2s around a whole TUI round trip; cmd/apogee/e2e_newcomer_test.go:38,40 (15m whole / 60s per step, with a local judge model and a docker exec).

Requirement behind all of them: the suite must pass on a slow CI runner or a slow developer machine. See docs/handoffs/2026-09-22 - 00 - ci-and-test-failures-triage.md.

## Acceptance

Each row either asserts the thing directly (allocation, a synchronisation point) or carries a budget generous enough for a loaded box; none is re-centred on one machine's measurement.

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af. Every row the description enumerates is accounted for — by plan items 8-13, not only by item 10. Row by row:

  1. internal/tools/network_funnel_test.go (was :982, the 250ms slack on an 850ms ceiling) — f623600a. The fixed slack is gone: the constants scaled (budget 2s, lookup 1.6s = 0.8*budget) and the ceiling is now a MULTIPLE of the budget, 'budget + budget/2', which sits strictly between the shared-budget outcome (~budget) and the per-phase one (lookup+budget). A floor assertion 'elapsed < lookup' was added so the case cannot pass without exercising the pre-flight. The comment states that room for a loaded box comes from scaling both constants, not from widening the slack.

  2. internal/doctext/pdf_test.go (was :486, elapsed > 100ms) — f623600a. No longer timed. TestExtractPDF_RefusesAnOversizedXrefTable now measures runtime.MemStats TotalAlloc across the refusal against an 8 MiB ceiling, the same shape the inflate-bomb case already used, and is serial (no t.Parallel) so no sibling's allocations land in the delta. The comment records that the wall clock only ever stood in for the allocation.

  3. internal/provider/reliability_test.go (was :192, elapsed >= retry429BaseDelay) — f623600a. Now asserts the gap the SERVER saw between the two request arrivals, 'arrivals[1].Sub(arrivals[0]) >= retry429BaseDelay', which excludes client-side round-trip overhead entirely instead of bounding it with no margin.

  4. The six time.Sleep(20ms) 'long enough for the second caller to be genuinely waiting' sites — cddb5bbc and 32b984a5 (plan items 8 and 9). domain.PromptSlot gained Waiting(), counting callers inside Acquire, and all six sites now block on the waiter count instead: waitForPromptSlotWaiters (internal/domain/promptslot_test.go:59), waitForQueuedCallers (internal/agent/promptslot_test.go:79 and :124, approvalqueue_test.go:227, approvalcache_test.go:159), waitForQueuedAskers (internal/tools/askqueue_test.go:74). The seam itself is pinned by TestPromptSlot_WaitingCountsACallerUntilItIsAdmitted and TestPromptSlot_WaitingDropsWhenAQueuedCallerIsCancelled. The only time.Sleep left in approvalqueue_test.go is the fake approver's deliberate hold window, which is the thing being simulated rather than a synchronisation assumption.

  5. internal/filewatch/filewatch_unix_test.go (Sleep(testSettle/3) into a 150ms settle window) — 3a1198d2. testSettle 150ms -> 500ms, so the fraction the test has to land inside is ~167ms rather than 50ms; the stalling child's margin also derives from testSettle ('testSettle + testSettle/2') so it stays proportional. filewatch_test.go records why the constants relate as they do.

  6. internal/reactions/command_test.go and internal/userexec/userexec_test.go — 3a1198d2. The ~1s margins became a named 'const margin = 5 * time.Second' in both tables, and the scripts sleep 30 instead of 5 so the margin stays well under the thing it must not reach. Both comments state that the margin is the instrument, never the claim.

  7. The e2e budgets outside the cured kit — 7ba67ced and 383c4f15. e2e_stream_test.go: streamReplyWait is no longer a 15s measurement but is DERIVED, 'tuitest.DefaultTimeout + runes*streamRuneAllowance', shared by the in-process test, its pty twin and the judge so the twins cannot drift. e2e_approval_test.go: 'promptly' is now 20 * settled (5s) off the kit's own unit instead of a raw 2s. e2e_newcomer_test.go: newcomerStepBudget 60s -> 3 minutes, admitting an apt-get, a domestic-link download or a build from source on a four-core ARM box, and newcomerBudget is derived from it ('newcomerMaxSteps * (step + judge allowance)') rather than chosen beside it — the note records that the old 15m could not fit even its own 20 x 60s of steps.

  Packages re-run green unraced on this box (doctext, provider, filewatch, userexec, reactions; the cmd/apogee rows under apogee-61g).

  Acceptance met: each row either asserts the thing directly (an allocation, a waiter count, a server-side gap) or carries a budget derived from a kit constant or a multiple of the thing it bounds; none is re-centred on one machine's measurement.
