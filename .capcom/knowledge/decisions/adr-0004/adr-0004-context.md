---
title: ADR 0004 context
---

Apogee has three agent modes: Plan (read-only), Ask-Before (human approves each tool
call), and Auto (runs tool calls without per-call approval). Auto is the one production
scenario that resembles the bench's *unsupervised* runs — there is no human gate — so it
is the one place OS-level confinement actually buys safety. apogee-code shipped no OS
confinement at all (approval was the only gate); apogee-sim's `RealSandbox` proved out the
confinement tech (seatbelt on macOS, namespace network-deny + write-confinement on Linux)
but only for the bench.

We chose to be safe from the start rather than treat confinement as a later optional
enhancement. The refinement this ADR settles: confinement is **not one thing** — a backend
may confine filesystem writes but not network egress (or vice-versa), and different tools
need different confinement — so "is it confined?" is the wrong question to gate on.
