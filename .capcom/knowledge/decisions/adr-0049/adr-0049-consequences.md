---
title: ADR 0049 consequences
---

- A gate is honest everywhere: "Allow" means the disclosed action happens — write, patch, copy,
  move, delete — and an error after a yes now signals a real fault, not a policy contradiction.
- The permit is engine-internal and wire-silent; drivers and the bench see only the verdicts they
  always saw (ADR 0031 intact). Sub-agents inherit the behaviour through the tree's Approver, and
  a Firing's fail-safe denier still refuses every Gate, so unattended escapes remain impossible.
- The fence's one rule — the box bounds writes, the Gate bounds escapes — is what the parked
  *configurable tool × mode security matrix* design builds on; its tighten-only law gains a
  concrete floor to tighten from.
- The realisation is tracked by `docs/plans/2026-08-14 - 01 - approved-escape-write-plan.md`;
  until it lands, contract §4's gap note stays flagged (decided, unbuilt) and the `ISSUES.md`
  defect stays open, marked planned.
