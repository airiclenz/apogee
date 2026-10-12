---
title: ADR 0005 context
---

Apogee can spawn a **sub-agent** — a nested, focused agent loop for a delegated sub-task,
itself an instance of the Embeddable agent
(ADR 0001).
apogee-code's `SubAgentOrchestrator` constructs this loop with **no `ApprovalManager` and
no agent mode** — so a literal port would let a sub-agent execute tools *outside* its
parent's safety gates. Post-ADR 0004
that is a privilege-escalation hole: a Plan- or Ask-Before-mode session could spawn a
sub-agent that writes files or runs commands unconfined.
