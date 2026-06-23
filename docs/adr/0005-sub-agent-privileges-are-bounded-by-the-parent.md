---
Status: accepted
---

# Sub-agent privileges are bounded by the parent

## Context

Apogee can spawn a **sub-agent** — a nested, focused agent loop for a delegated sub-task,
itself an instance of the Embeddable agent
([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)).
apogee-code's `SubAgentOrchestrator` constructs this loop with **no `ApprovalManager` and
no agent mode** — so a literal port would let a sub-agent execute tools *outside* its
parent's safety gates. Post-[ADR 0004](0004-auto-mode-requires-os-level-confinement.md)
that is a privilege-escalation hole: a Plan- or Ask-Before-mode session could spawn a
sub-agent that writes files or runs commands unconfined.

## Decision

**A sub-agent's privileges are always ≤ its parent's.** Concretely:

- it inherits the parent's **Agent mode** (or a stricter one), never a more permissive one;
- every sub-agent tool call passes the **same Safety guardrails** (Approval in Ask-Before,
  path-safety, arg-guard, audit);
- an **Auto** sub-agent still requires **Confinement**;
- its **tool set is a subset** of the parent's available tools — the parent may *restrict*
  (e.g. a research sub-agent gets a read-mostly set) but never *expand* it.

**No sub-agent can do what its parent could not.** Do not replicate apogee-code's
gate-less sub-agent orchestrator.

## Consequences

- The Go sub-agent orchestrator **must** be constructed with the parent's mode, approval
  delegate, confiner, and guardrails threaded in — this is a required signature change from
  the TS source, not an optional one.
- Default sub-agent tool set = the parent's set; callers narrow it per task. (A default the
  catalogue/tooling work can revisit.)
- Sub-agent events nest into the parent's event stream so the TUI and bench observe them.
