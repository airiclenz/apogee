---
title: A serial wave never shows the ctrl+g sends-now hint
status: done
type: bug
priority: 3
plan: 2026-10-05 - 01 - subagent-wave-and-background-leftovers-plan
aliases: [apogee-serial-wave-hint-hidden]
created: 2026-10-05T16:15:27Z
closed: 2026-10-05T18:03:13Z
reason: resolved by plan 2026-10-05 - 01 - subagent-wave-and-background-leftovers (archived at 7af6fde3), item 5
---

With parallel-agents: 1 the TUI draws no row for a group member that has not started, so it never sees a queued member and the 'after the wave · ctrl+g sends now' hint shows only for pooled groups. The esc stop hint has the same limit. A fix likely needs the engine to announce queued members (a domain event), weighed against ADR 0031's wire-silent engine and ADR 0025 (amended 2026-10-05). Code: internal/tui/interject.go, bridge.go (pendingNow), model.go; e2e cmd/apogee/e2e_subagent_preempt_test.go. Found in the subagent-wave-and-background plan run (item 5, a8374732); see docs/handoffs/2026-10-05 - 00 - subagent-wave-and-background-leftovers.md.
