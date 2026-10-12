---
title: ADR 0011 context
---

Phase 2 makes the agent a product: an interactive terminal UI you hold a coding
conversation in ([phase-2 detail plan](../../../../docs/plans/archived/phase-2-detail-plan.md)). The TUI is the
first *interactive* consumer of the Phase-1 public surface; the bench (P1.7) was the first
programmatic one, and they consume the **same** surface (ADR 0001).
Five forces collide, and wiring them without a deadlock or a data race is the real work of
the phase:

- the **Agent is single-goroutine** and not safe for concurrent use (`agent.go`: "drive
  one Agent from one goroutine; observe it only via its EventSink");
- **`Step` blocks** — network I/O, streaming, and a synchronous `Approver` that may block
  on a human (ADR 0007);
- **`EventSink.Emit` is called synchronously on the Step goroutine**, in Turn order, and
  "must not block the loop for long";
- **Bubble Tea owns a single-threaded `Update` loop** — all model mutation happens there,
  one `Msg` at a time;
- **the human's approval and stop live in that `Update` loop**, but the `Approver` that
  needs them runs on the Step goroutine.

The constraint the broad plan sets is absolute: *"No agent logic in the TUI — it only
renders events and sends user input"* (and supplies the Approval delegate + cancellation).
So the threading model has to be settled **before** any pane is drawn, or every view fights
it. This ADR records that model — the same way ADR 0007
recorded the loop's control-flow calls — so each pane (P2.2–P2.4) is mechanical.
