---
title: ADR 0028 considered options
---

- **Let `SwitchUpstream` mutate the existing client's endpoint** — rejected, and it is the
  provider's own documented line rather than a preference: `Client.SetModel` covers the model
  precisely because the endpoint is not a mutable property of a bound client. Mutating endpoint and
  key separately would also open a window in which a request could carry one server's URL and
  another's bearer token.
- **Keep the old binding "until corrected" after a switch** — rejected as dishonest, and it is the
  same rejection ADR 0024 made for "refresh the display only": for up to one Interval the footer,
  the start-up box and the Budget would describe a server the wire is no longer pointed at, and the
  gap is exactly when a human is watching. Unbound says the true thing, and `connecting…` is a
  state the cold start already teaches.
- **Tear down and reconstruct the Agent for a new endpoint** — rejected. The conversation, the Turn
  counters, session approvals, the mode and the confinement state all survive a switch *by design*;
  reconstruction would destroy them to change one URL, and it is the same posture Rebind already
  takes for a model.
- **Swap the `Options` func values TUI-side** (build a new Monitor and rebind closure in the
  renderer) — rejected: it would put upstream lifecycle — client construction, key handling,
  Monitor ownership — inside the thin renderer, which ADR 0011 and ADR 0010 keep deliberately
  ignorant of `internal/provider`. The holder keeps the seams' signatures and moves the ownership
  nowhere.
- **Suppress the flap-back TUI-side** instead of moving the discovery hint — rejected: it treats
  the symptom, leaves discovery answering a question the session has moved past, and needs a second
  rule for the heartbeat-driven case where no human picked anything.
- **A `--save` form for `/server`, and a `server:` startup key naming an entry** — rejected for
  now, not on principle. Session-scoped is the smaller, reversible claim and matches
  `/confine off`; both are additive later, and neither is needed to make the switch useful.
  *(Amended 2026-08-05 by ADR 0036:
  the `server:` key is ratified and a switch records into it automatically; the `--save` form stays
  rejected — a human who picked a server has already stated the intent a flag would ask them to
  repeat, and `/confine off`'s asymmetry is about loosening a safety fence, not about which machine
  serves the session.)*
- **Persist the switched endpoint in the session record** so `--resume` returns to it — rejected
  with the same reasoning: a session record describes a conversation, and which machine served it
  is not part of what is being resumed.
- **Make `/server` require a reachable current server** (the offline degrade `/model` takes) —
  rejected: switching away is the single most useful thing to do *while* the current server is
  unreachable. Where the session can go is config, not an observation.
- **A callback field on the picker state instead of a kind enum** — rejected mechanically: the
  Model is copied by value on every `Update` (ADR 0011), and the state stays plain values so that
  rule needs no new exception.
- **Keep first contact quiet after a switch too** (one rule, no `switched` flag) — rejected: the
  quiet seed's whole justification is a start-up box the human can still see. After a switch they
  cannot, and they asked — an unacknowledged act is worse than one redundant line.
