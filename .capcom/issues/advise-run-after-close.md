---
title: SetReactions after Close starts a session-start advise run nothing ends
status: done
type: bug
priority: 2
created: 2026-10-21T12:09:42Z
closed: 2026-10-21T15:10:28Z
reason: "Resolved by plan 2026-10-21 - 01 - advise-run-after-close-plan (archived at e8ab6979): Close shuts the session window (817dc5c0) and session-advise started waits are bounded via testtime (d0f0c572)."
---

Deferred from the user-session-advice plan run (item 6, commit 39bd22c2).

`Agent.Close` (internal/agent/agent.go:862) stops the session-start and compaction advise lanes but never shuts the session window (`sessionWindow.shut`, internal/agent/agent.go:2223). A `SetReactions` (internal/agent/agent.go:1646) that lands after `Close` can still pass `sessionWindow.admit` (internal/agent/agent.go:2203) and reach `extendSessionAdvice` (internal/agent/sessionadvise.go:223), whose `sessionAdviseLane.extend` (internal/agent/sessionadvise.go:106) installs a fresh context and cancel because `stop` left `cancel` nil — so it starts an advise run that nothing ever ends.

Separately, the item-6 test `TestSessionAdvise_ALiveArmedRunEndsAtTheNextBoundary` waits on a bare `<-started` with no `internal/testtime` bound (internal/agent/sessionadvise_test.go:812); the review named `TestSessionStartedAdviseAdoptedBeforeTheFirstPromptBriefsTheSession` (cmd/apogee/wire_settings_test.go:4391) as the test in question. A hung run hangs the test until the package timeout instead of failing it.

## Acceptance

- `Close` shuts the session window (or the lane refuses `extend` once stopped for good), so a `SetReactions` after `Close` starts no advise run; a test arms a `session-started` advise entry after `Close` and asserts no run starts.
- The bare `<-started` receive is bounded through `internal/testtime` (`Budget` / `Eventually`) and fails with a message instead of hanging.

## Log

- 2026-10-29 Airic Lenz: 2026-10-21 (docs/plans/archived/2026-10-21 - 01 - advise-run-after-close-plan.md item 1): Close now shuts the session window before it stops the session-start lane, and that one window gates both lanes — so after Close a live SetReactions starts no session-started advise run AND hands no observe session-started entry to the ObserveRunner. Deliberate: no production Driver swaps after Close (cmd/apogee/wire.go closes the hooks before the engine). A Driver that ever reorders shutdown so a swap can land after Close loses those observe firings too.
