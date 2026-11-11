---
title: The Inspector's wire tap reads the top-level Agent's turn counter and Config from background workflow goroutines
status: done
type: bug
priority: 2
created: 2026-11-10T19:36:31Z
closed: 2026-11-11T06:36:34Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md ("Data race on the turn counter from background workflow children"). Verified VALID (widened) 2026-11-10 at a5c4f2db.

**Where:** `internal/agent/construct.go` `wireTap.observe` (:654-662), bound to the top-level Agent by `tap.bind(a)` (`agent.go:997/1021`, `construct.go:351`, `rebind.go:419/498`).

**What:** one tap serves one provider client. A background workflow's host (`backgroundHost`, `background.go:684`) inherits the session seating, so its unrouted children (fan_out, recipe, background sub_agent not routed to a Delegation target) send over the top-level client, whose observer runs on the child's streaming goroutine (`provider/stream.go:216`). It reads `a.turns.index` and `a.cfg.Events` from the top-level Agent. Meanwhile the conversation goroutine increments `turns.index` (`turn.go:250/258/332`), and the idle-only mutators background workflows are designed to tolerate replace `a.cfg` whole (`rebind.go:229/442/647`). Neither is guarded. Foreground fan_out does not race (the parent is blocked in dispatch).

**Why it matters:** with the Inspector on and a background workflow running while the user chats: unsynchronised reads/writes, WireEvents with stale Turn stamps, `-race` reports. `a.cfg` carries interfaces and strings, so a torn read is not benign by construction.

**Suggested fix:** snapshot what `observe` needs when the tap is bound (sink, EventBase identity). For the Turn, read a race-safe mirror (`atomic.Int64` written beside `turns.index`), or give the background host's children a tap bound to the host/child, as a routed spawn already gets. Add a single-test `-race` test covering Inspector + background workflow + Stepping and rebinding.

## Acceptance

- A single-test -race run with the Inspector on, a background workflow streaming over the session client and the conversation Stepping and rebinding reports no race.
- WireEvents from background children carry a Turn stamp that is defined and documented.
