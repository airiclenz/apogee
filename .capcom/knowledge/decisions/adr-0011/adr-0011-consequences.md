---
title: ADR 0011 consequences
---

- **The seam is provable without a terminal.** The bridge depends on a narrow
  `programSender interface { Send(tea.Msg) }`, so a stub program stands in for
  `*tea.Program`; the worker drives a fake `Engine`. The whole seam — sink ordering, the
  approval rendezvous and its cancel-no-leak guarantee, the worker's terminal Msg, and
  concurrent Emit + Approve + cancel — is tested under `-race` before any pane exists (P2.1),
  which is the point of converging the threading model first.
- **Each pane is mechanical.** P2.2–P2.4 only fold the C1–C4 messages
  (`eventMsg`/`approvalReqMsg`/`exchangeDoneMsg`/`cancelledMsg`/`errMsg`) and keypresses into
  the model — no new threading decisions, no agent logic.
- **This is the template every future interactive consumer copies.** A worker-goroutine
  engine + a `p.Send` event bridge + a buffered approval rendezvous + a ctx cancel is the
  load-bearing structure of the product binary; sub-agent (`Depth > 0`) rendering in Phase 3
  and any later interactive surface inherit it.
- **`tea.Msg` is not `any`.** In Bubble Tea v2 (`charm.land/bubbletea/v2`), `Msg` is an alias
  for a method-less `ultraviolet.Event`, so the seam references `tea.Msg` (not `any`) for the
  `programSender` interface; any struct is still a valid Msg, so the seam message types stay
  plain values. (See the build note below — the module path moved at v2.0.7.)
