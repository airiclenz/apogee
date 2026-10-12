---
title: Delegation seat
---

**Delegation seat**:
One of the **two** places a delegation may run — the session's own server, or the **Sub-agent
server** — and, with root `sub-agents-choice: model`, something the top-level model picks per
call through the `sub_agent` tool's optional `run_on` (`session` | `sub-agents-server`). Absent,
the root `sub-agents-server:` key's rule stands, so the default gate value (`fixed`) is today's
behavior with the parameter not even in the schema. The choice exists at **depth 0 only** — a
child's own tool carries no `run_on`, and a delegation keeps the seat it landed on
(ADR 0039's
depth-0 rule, ADR 0045's identity-once-there rule). The seat carries everything the seat means:
`run_on: session` in a routed session runs unrouted with the parent's posture, window and
**effort dialect**, while `sub-agents-server` is routing exactly as ADR 0045/0066 already build
it. The model chooses from session-constant facts the **Orientation block**'s Delegations line
states for BOTH seats symmetrically — entry name, the entry's `description:`, its `model:` pin,
the bound session model — never from availability, which is why an ask the far seat cannot honour
**falls back** to the session server and says so in ONE appended note line, the last line of the
result body ahead of a steered child's trailer
(ADR 0063 D3 keeps the final line).
Ratified 2026-09-01
(ADR 0069).
_Avoid_: "target" (the **Delegation target** is the latched spec of ONE seat, not the pair),
"routing choice" (routing is what one of the two seats is), "worker pool".
