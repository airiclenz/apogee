// Package tui is the Bubble Tea terminal UI: a thin renderer over the agent's
// typed Events that supplies the Approval delegate. It holds no agent logic — it
// only renders Events and sends user input (broad plan §4; phase-2 detail plan §1).
//
// It depends on the engine only through the narrow [Engine] interface and on the
// public types through internal/domain; it never imports the root module path, so the
// ADR-0010 invariant "internal/* never imports root" holds (phase-2 detail plan §3 C5).
//
// Phase 2 build order: P2.0 landed the seam boundary (the [Engine] interface, [Options],
// and the [Run] entry point). P2.1 lands the concurrency seam — the worker-goroutine
// engine driver ([startExchange]/[driveExchange]), the Event→Msg bridge ([teaSink]), and
// the approval rendezvous ([uiApprover]), all late-bound to the running program through
// the [Bridge] (phase-2 detail plan §3 C1–C5; ADR 0011) and proven under -race against a
// stub program. The Bubble Tea model/update/view that drives them land in P2.2–P2.4.
package tui
