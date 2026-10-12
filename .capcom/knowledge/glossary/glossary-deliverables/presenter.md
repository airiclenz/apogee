---
title: Presenter
---

**Presenter**:
The **host-supplied delegate** a presentation routes through (`domain.Presenter`, on `Config`
beside `Approver`/`Asker`/`Confiner`) — the sibling of the **Asker**: the same host-decides shape,
for showing a document rather than asking a question, and carrying no allow/deny semantics. A
`nil` Presenter means `present_document` is simply **not registered** (a headless host supplies
none, so the model is never offered an affordance nobody can honour). It is **not** an
`ExternalEffectTool` — the user's own display is not a non-forkable remote to stub — and it holds
**no live state across a Turn**: the tool keeps a delegate reference, the host owns the
mechanisms (ADR 0008).
_Avoid_: "opener" (that is one mechanism *inside* the ladder, not the delegate), "viewer",
"renderer".
