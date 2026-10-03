---
Status: accepted; the "Auto sub-agent still requires Confinement" bullet superseded by ADR 0012, the audit guardrail by ADR 0013 (2026-10-02 amendment)
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

## Amendment (2026-10-03) — an Auto sub-agent inherits the parent's blast radius; the audit guardrail is gone

Two lines of the Decision no longer describe the engine. The rest stands.

- **"An Auto sub-agent still requires Confinement"** is superseded by
  [ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md). Auto no
  longer requires OS confinement mode-wide: confinement attaches to blast radius, and Auto is tuned
  by the `confine-to-workspace` key. A sub-agent inherits the parent's live `confine-to-workspace`
  flag at spawn (`newChildAgentOn` sets `childCfg.ConfineToWorkspace = a.ConfineToWorkspace()`) and
  the parent's Confiner verbatim. An Auto child is therefore fenced exactly as its parent is:
  confined where the parent is confined, gated where confinement is unavailable, and unfenced only
  when the parent's effective `confine-to-workspace` is false (global key, `unconfined-hosts` match,
  or a session `/confine off`). It is never looser than the parent.
- **"audit"** in the guardrail list is superseded by
  [ADR 0013](0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md)'s
  2026-10-02 amendment: the in-process audit ring (`security.AuditLog`, `Guards.Audit`) is deleted.
  A sub-agent's calls still pass the parent's Approval and the shared read-only dangerous-action
  floor. Their decisions are emitted as `domain.AuditEvent`s on the parent's `EventSink`, told
  apart by depth and spawn ids. A sub-agent has no audit trail of its own.

The ≤-parent decision stands, and so does the tool-subset rule: `defaultSubAgentTools` builds the
child's registry from the parent registry's own names via `Subset`, and the call's `tools` argument
(`requestedChildTools`) can only narrow it. A privilege expansion stays structurally impossible.
