---
title: ADR 0005 decision
---

**A sub-agent's privileges are always ≤ its parent's.** Concretely:

- it inherits the parent's **Agent mode** (or a stricter one), never a more permissive one;
- every sub-agent tool call passes the **same Safety guardrails** (Approval in Ask-Before,
  path-safety, arg-guard, audit);
- an **Auto** sub-agent still requires **Confinement**;
- its **tool set is a subset** of the parent's available tools — the parent may *restrict*
  (e.g. a research sub-agent gets a read-mostly set) but never *expand* it.

**No sub-agent can do what its parent could not.** Do not replicate apogee-code's
gate-less sub-agent orchestrator.
