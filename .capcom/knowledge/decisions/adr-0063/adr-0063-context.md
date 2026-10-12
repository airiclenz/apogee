---
title: ADR 0063 context
---

A delegation is the one place in apogee where the human is a spectator. Two entries in `IDEAS.md`
say it from opposite ends. `IDEAS.md:14`: expanding a sub-agent should open it *full screen*,
jumping to its latest line, with a way back up — and opening it that way is what would finally
"allow the user to send prompts to the sub agent. This is not currently possible". `IDEAS.md:18`:
with several children live, the activity line above the prompt box "flickers back and forth between
the different sub agents", because there is exactly one activity slot and every child writes it.

Three structural facts stand behind those complaints.

**1 — The inline expanded shape nests a run inside its parent.** Expanding a framed delegation opens
`expandedSubAgentView` (`internal/tui/subagentblock.go:548`): a rail drawn *inside* the parent
transcript, the child's rows one depth deeper than the parent's, sharing the scroll position and the
sticky header with everything around it. It also paints the delegation's tool-result body above the
child's prompt rows, which is the open double-print defect `ISSUES.md` records as this is written —
the report printed twice, once badly formatted at the top and once properly at the end.

**2 — A running child cannot be addressed at all.**
ADR 0025 gave the human a way into a
running Exchange — `Agent.Interject`, committed at a between-Steps boundary — but it is a *top-level*
contract, and it is realised by the Driver drives-`Step`-itself pattern: `Run`'s own doc says it
"owns the between-Steps boundaries it loops over, so there is no seam to interject at"
(`internal/agent/agent.go:531-533`), and directs an embedder who wants mid-Exchange delivery to drive
`Step`. A delegated child cannot take that advice. Per
ADR 0013
D5 a sub-agent runs atomically inside the parent's Turn, driven by `Run` from inside the parent's
tool dispatch — so `Run` *is* the child's driver and no host holds its Step loop. "Drive `Step`
yourself" is not an option anyone has.

**3 — The identity already exists.**
ADR 0039 D5
put the spawning call-ID on `EventBase` so a Driver can tell one child's stream from another's. That
ID is exactly the handle a user-facing address needs; nothing new has to be invented to name a child.

Two invariants bound the answer.
ADR 0031
requires the engine to stay sufficient for **any** Driver, so reaching a child must be an engine
capability, never a TUI-only shortcut around it.
ADR 0035 settled that a
full-height surface in apogee is a pane inside apogee's own frame, and rejected the alternate-screen
takeover that "drops apogee's status line" and puts the user's scrollback out of reach.

The shape below was ratified with the owner on 2026-08-30 and is realised item-by-item by
`docs/plans/2026-08-30 - 01 - sub-agent-run-view-plan.md`.
