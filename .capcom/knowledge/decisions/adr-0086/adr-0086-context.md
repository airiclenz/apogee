---
title: ADR 0086 context
---

`IDEAS.md` → "Sub-Agents" item 3 asks that "a master agent … stop its sub agents and …
communicate with its sub agents". The handoff
`docs/handoffs/2026-09-24 - 00 - sub-agent-control-handoff.md` surveyed the ground at 69565337, and
the owner's 2026-09-25 grilling session settled it.

A delegation blocks its parent: `runSubAgent` runs the child to its Exchange boundary inside the
parent's Turn (ADR 0013 §5), and a fan-out joins every child before the parent continues (ADR 0039
D4). The parent therefore cannot act while a child runs, so "the parent stops a child" is
impossible in the current model rather than merely unbuilt. Two things exist that come close. The
human can message a running child (ADR 0063, `InterjectChild`), and the parent can `continue:` a
**named** delegation — but only one that was capped or faulted, only for the rest of the Exchange
(`loop.go` clears the retention as the next opens), and only once per retained entry. The one stop
there is `esc×2` at the top level, which cancels the whole Turn and rolls it back with no partial
result.
