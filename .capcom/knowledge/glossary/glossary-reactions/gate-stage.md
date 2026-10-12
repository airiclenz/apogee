---
title: "Gate stage: the Approver stage where armed gate Reactions decide whether a call may run"
---

**Gate stage**:
The stage of the **Approver** at which the armed **gate** **Reactions**
answer *may this call run* — `applyGates` in `internal/agent`, run immediately after `resolve` at
both of its sites and before the approval cache, in every **Agent mode** and
under **Bypass mode**
(ADR 0076 D2, D9). It
is a stage of the Approver, not a leg of the `pre-tool-exec` cascade: the cascade reshapes a pending
call, a gate decides its fate, and it folds into the ladder's own verdict one way only — the first
`deny` refuses the call with the engine-authored `tool call denied by reaction <id>`, the first
`ask` raises it to the human whatever the mode said (`reaction <id> asks: <reason>`; the reason
reaches the person, never the model), and `allow` means nothing, the ladder's verdict standing,
because a script that could pre-approve a call would be a second, unaudited autonomy ladder
(ADR 0049
§4). Every unreadable answer — empty or unparseable stdout, a non-zero exit, a timeout, a refused
permit — is an `ask`, the one place the sync lane is not fail-open. On a delegation an `ask` leaves
the verdict untouched and is booked as `deferred to the child's calls`: nothing executes at the
recursion point, and the inherited gate asks on the child's own tool calls.
_Avoid_: "gate hook", "pre-approval" (a gate can only tighten), "gate leg" (it is no part of the
seam cascade).
