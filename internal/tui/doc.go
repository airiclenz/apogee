// Package tui is the Bubble Tea terminal UI: a thin renderer over the agent's
// typed Events that supplies the Approval delegate. It holds no agent logic — it
// only renders Events and sends user input (broad plan §4; phase-2 detail plan §1).
//
// It depends on the engine only through the narrow [Engine] interface and on the
// public types through internal/domain; it never imports the root module path, so the
// ADR-0010 invariant "internal/* never imports root" holds (phase-2 detail plan §3 C5).
//
// Phase 2 build order: P2.0 landed the seam boundary (the [Engine] interface, [Options],
// and the [Run] entry point). P2.1 landed the concurrency seam — the worker-goroutine
// engine driver ([startExchange]/[driveExchange]), the Event→Msg bridge ([teaSink]), and
// the approval rendezvous ([uiApprover]), all late-bound to the running program through
// the [Bridge] (phase-2 detail plan §3 C1–C5; ADR 0011) and proven under -race against a
// stub program. P2.2 lands the Bubble Tea skeleton that drives them: the [Model] with its
// four-state machine, the input box, the transcript viewport, and the status line, with
// [Run] now building the [tea.Program] and binding the [Bridge] to it. The Charm v2 stack
// (bubbletea/bubbles/lipgloss, all on the charm.land path) is taken over the v1 fallback.
// The rich event fold (P2.3) and the Approval UI keys (P2.4) build on this skeleton.
package tui
