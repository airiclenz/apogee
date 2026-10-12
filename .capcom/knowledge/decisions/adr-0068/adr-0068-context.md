---
title: ADR 0068 context
---

One delegation is shown to a human in five places — the collapsed call block, the
`✦ Sub-Agent (N)` umbrella's member rows, a **Run view**'s breadcrumb
(ADR 0063), the `apogee headless`
`sub-agent:` line, and the **Session record** it is saved into — and every one of them asks the
same question: what is this run called? The `sub_agent` tool takes an OPTIONAL `name`, normalised
at the recursion point into the one line every display can paint (`delegationName`,
`internal/agent/subagent.go`), and when the call leaves it out every display falls back to the
delegated task's first line. That fallback is what a wall of delegations actually reads as: three
rows all opening `You are a repo scout. Read the...` are indistinguishable at the width a status
line has, and the one party that could have told them apart — the model that wrote all three
tasks — is the party that declined to name them.

Session records met this exact problem on 2026-07-31 and settled it
(ADR 0022's addendum): a
cosmetic completion, off to the side of the conversation, renames a record shortly after it is
born; `auto-title:` gates it, it fails silently, and the heuristic first-line title stands
underneath it the whole time. Naming a delegation is that same act one level down — the same
trigger (something was born unnamed), the same cost (one small completion), the same failure
posture (the fallback was already good enough to ship with). Re-deciding it from scratch would
have been re-deciding a settled question.

What is *not* the same is where it lives. The session title's call rides a nil-able func seam on
`tui.Options` and never enters the engine, which is correct because a Session is the TUI's object.
A delegation is the ENGINE's object: the engine spawns it, holds the name it was given, decides
what the parent's tool result says, and is the only party that knows when the child started and
when it stopped. And every **Driver** has the same want — headless prints the name, a **Firing**
saves it — so a naming that lived in `tui.Options` would name delegations in one Driver and leave
the other two reading task first lines. The seam has to sit where the run does.

A grill on 2026-09-01 settled the arrangement, and this record is that ratification.
