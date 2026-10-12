---
title: ADR 0092 considered and rejected
---

- **An append-only prefix** (owner, 2026-10-02). Freezing the system prompt and every earlier
  message once sent, and moving the changing parts to the end of the request, would keep every
  signature valid. It would change how the task list, mode, orientation, tail notes, capping and
  pruning reach the model on every wire, to serve one wire's replay. Rejected.
- **Letting the server decide.** Sending the blocks and recovering from the 400, or using a
  server-side beta field to discard stale thinking, spends a round trip or ties apogee to a beta.
  The client already knows the prefix changed.
- **Dropping all thinking once anything changes.** Simpler, but it throws away valid replays on
  every unchanged prefix, which is the common case inside a tool loop.
