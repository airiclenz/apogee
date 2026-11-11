---
title: "confinetest row #15's accept wait is a raw 2s duration, not a testtime one"
status: done
type: bug
priority: 3
labels: [testing]
created: 2026-11-11T14:10:24Z
closed: 2026-11-11T18:50:19Z
reason: "Resolved by plan '2026-11-11 - 02 - fence-test-gaps-and-landlock-udp-plan' item 1 (archived at c48693ef): row #15's unix and row #13's UDP probe windows are testtime.Fixed, WaitDelay drains testtime.Budget."
---

Row #15 (`unix_connect_outside_box`, internal/platform/confinetest/confinetest.go:276) bounds the parent listener's accept with `unixProbeWait = 2 * time.Second` (internal/platform/confinetest/confinetest.go:353), set raw at confinetest.go:307. On the silent arm that window is a "nothing happens within" wait that always expires in full. docs/design/test-drivers.md §Waiting says every duration in a test says what it is — `testtime.Fixed` for a semantic window, `testtime.Budget` for a deadline — and `TestNoFixedTimingInTests` (internal/testtime/guard_test.go) does not parse confinetest.go, a non-`_test.go` kit file, so nothing flags it. Deferred out of the closeout of `docs/plans/archived/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md` (item 7).

## Acceptance

- Row #15's accept wait is written through internal/testtime (`Fixed` or `Budget`, whichever the window is) per docs/design/test-drivers.md §Waiting.
