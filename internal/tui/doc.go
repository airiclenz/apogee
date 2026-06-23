// Package tui is the Bubble Tea terminal UI: a thin renderer over the agent's
// typed Events that supplies the Approval delegate. It holds no agent logic — it
// only renders Events and sends user input (broad plan §4; phase-2 detail plan §1).
//
// It depends on the engine only through the narrow [Engine] interface and on the
// public types through internal/domain; it never imports the root module path, so the
// ADR-0010 invariant "internal/* never imports root" holds (phase-2 detail plan §3 C5).
//
// Phase 2 build order: P2.0 lands the seam boundary defined here (the [Engine]
// interface, [Options], and the [Run] entry point). The concurrency seam — the
// worker-goroutine engine driver, the Event→Msg bridge, and the approval rendezvous
// (phase-2 detail plan §3 C1–C5) — plus the Bubble Tea model/update/view land in
// P2.1–P2.4.
package tui
