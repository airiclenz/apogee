---
title: gitDiffTimeout is shorter than gitTimeout, inverting its own comment
status: done
type: bug
priority: 3
labels: [slow-box]
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-lak9]
created: 2026-09-22T16:03:47Z
closed: 2026-09-23T06:55:59Z
reason: "Item 5: gitDiffTimeout 30s >= gitTimeout, pinned by test; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

internal/tools/git.go:57 gitDiffTimeout = 10 * time.Second against :53 gitTimeout = 15 * time.Second. The comment at :55-56 says a diff-range "can be larger" than an ordinary status/log, yet the value is SHORTER, and timeoutFor at :192-198 confirms diffs get the smaller of the two. Either the comment or the number is wrong; on a large diff on slow storage it is the number.

Found by the slow-box deadline sweep, 2026-09-22.

## Acceptance

The diff budget is at least the ordinary git budget, and the comment and the value agree.
