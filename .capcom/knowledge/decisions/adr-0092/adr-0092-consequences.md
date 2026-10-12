---
title: ADR 0092 consequences
---

- On the preserved-thinking models a request no longer 400s because an earlier reply's prefix
  changed. The cost is that the model loses the thinking of that reply and of every later one in
  the request.
- Reasoning entries gain a digest field. Entries saved before this change still decode, but on the
  preserved-thinking models their blocks are not replayed.
- CHANGELOG.md's apogee-4kl engine-half entry ("so they survive a session save and resume, and go
  back upstream") is narrowed: on these models a resumed session replays thinking only where the
  resumed prefix matches.
- `CONTEXT.md`'s Thinking channel entry is qualified to match.
- Implementation and its tests are plan `docs/plans/2026-10-02 - 02 - refocus-findings-plan.md`
  item 5.
