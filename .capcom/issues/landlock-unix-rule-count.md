---
title: landlock RESOLVE_UNIX planner adds one rule per /tmp entry when the runtime dir is under /tmp
status: done
type: bug
priority: 3
labels: [security]
created: 2026-11-11T14:10:30Z
closed: 2026-11-11T18:50:19Z
reason: "Resolved by plan '2026-11-11 - 02 - fence-test-gaps-and-landlock-udp-plan' item 3 (archived at c48693ef): TestLandlockResolveUnixPlanBusyTmp pins the rule count on a 10 000-entry /tmp, BenchmarkPlanResolveUnixRules measures it, and planResolveUnixRules' doc states the bound and why it is harmless."
---

`planResolveUnixRules` (internal/platform/landlock_linux.go:565) grants RESOLVE_UNIX to every entry of each ancestor of a closed path, descending into `/tmp` when the user runtime dir lives there (doc comment at landlock_linux.go:547-563). A closed runtime dir under /tmp therefore makes the confined child add one landlock rule per /tmp entry at apply time — potentially many on a busy /tmp — with the cost and any failure landing on every confined call. Deferred out of the closeout of `docs/plans/archived/2026-11-11 - 01 - fence-escape-and-open-defects-plan.md` (item 9).

## Acceptance

- The rule count a busy /tmp produces is measured or bounded, and either shown harmless by a test or capped/handled so a confined call cannot fail or slow unboundedly from it.
