---
title: A tool group auto-collapses when the next tool call arrives, not when tool work ends
status: done
type: bug
priority: 2
labels: [tui]
plan: 2026-11-23 - 04 - tool-group-early-collapse-plan.md
created: 2026-11-23T08:19:52Z
closed: 2026-11-23T14:16:45Z
reason: "Resolved by plan '2026-11-23 - 04 - tool-group-early-collapse-plan' (archived at 7f49e4ea): tool group settles on a follower, not the next Turn's start; driven test; ADR 0035 amendment and docs."
---

With `ui.tools-auto-collapse` on (the default), a tool group folds as soon as the next Turn of its run starts past it. In a run of consecutive tool-call Turns that means only the first call is ever seen expanded: the group folds while the model is thinking about its second call, and every later call joins a folded group.

## Cause

This is the Turn-boundary rule in `transcript.blockSettled` (`internal/tui/transcript.go`): `transcript.closeTurn` marks the run's last entry `turnClosed`, and a block with a `turnClosed` member is settled, and stays settled while the next Turn's calls extend it. The rule is deliberate (2026-11-20, `adr-0035-amend-1120b` / `adr-0035-amend-1120c`; `layout.md` "Which fold holds an umbrella"), so this issue changes a recorded decision, not a slip in the code.

## Wanted

An untouched tool group stays open while tool calls keep joining it. It folds only when one of these happens:

- the group grows past `ui.tools-fold-over`, or
- the run moves on to something that will not end up in the group: a reply or narration, a sub-agent group, a note, a user message, or the end of the run.

A new Turn starting is not by itself a reason to fold: if that Turn's output is another tool call, it joins the open group.

## Threshold

The collapse threshold is `ui.tools-fold-over` (default 5 type rows, the size past which an umbrella is large); the owner confirmed this on 2026-11-23. No new setting is added. Counting type rows, not calls, is intended: ten `Read` calls are one row and do not trip it, because the threshold is about visual cleanliness, not call count.

## Why

A reader watching a run sees only the first call of a burst open; every later call lands in an already folded group, so the live tool work is hidden while it happens. Auto-collapse is meant to fold finished history, not the burst in progress.

## Acceptance

- With `ui.tools-auto-collapse` on, an untouched tool group stays open while consecutive Turns keep adding tool calls to it and nothing else is placed after it.
- The group folds once it grows past `ui.tools-fold-over`, or once something that does not join the group follows it: a reply or narration, a sub-agent group, a note, a user message, or the end of the run.
- A group the reader touched, a member waiting on the user, and `ui.tools-auto-collapse` off behave as today.
- Driven `internal/tuitest` + `internal/stubllm` test: a multi-Turn tool burst shows the second and later calls expanded until the follower arrives; `autocollapse_turnboundary_test.go` is updated to the new rule.
- ADR 0035 gains a dated amendment; `layout.md`, `docs/layout/tool-layout.md` and `docs/manual/configuration.md` state the new rule.
