---
title: ADR 0073 rejected
---

- **Plugins** — out-of-process tools are what MCP is (ADR 0031 invariant 3); in-process tools are
  already open to Go embedders (ADR 0002);
  a dynamic loader is a large trust surface for a gap MCP covers.
- **Tool-call policy or veto hooks** — a user-authored guard at the pre-tool-exec seam is a second
  Mechanism surface, which ADR 0071 closed.
- **Post-edit lint fed back to the model** — the retired `autofix`/`syntax` rows; a benched
  Mechanism if it returns, never a Hook.
- **Driver-side feeds instead of engine variants** (a `Boundary(StepResult)` call and an Approver
  wrapper) — each Driver would have to remember both, and the one that forgot would silently lose
  those Hooks.
- **A distinct term ("Notifier")** — cleaner in the glossary, opaque to every external reader.
