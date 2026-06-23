// Package agent is the embeddable agent loop: it builds requests, calls the
// Upstream, parses responses, dispatches tools, and applies Mechanisms at the
// loop's hook points. It owns the Turn/Step state machine, the serializable
// conversation state, and typed Event emission, and holds no ambient process or
// filesystem state — every state root is injected via Config. Sub-agent
// orchestration (privileges ≤ parent) and the Plan / Ask-Before / Auto modes live
// here too.
//
// In the ADR-0010 layout it is the engine layer: it imports internal/domain for the
// public types and internal/provider for the Responder seam, and never imports the
// root apogee package. The root facade re-exports the Agent handle and forwards New /
// Resume here. See ADR 0001 (embeddable, steppable loop) and ADR 0007 (Step/Turn and
// the quiescent boundary). The real loop body, sub-agents, and modes land in Phase 1;
// P0.6 seeded the single-Turn slice this package now hosts.
package agent
