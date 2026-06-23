// Package agent is the embeddable agent loop: it builds requests, calls the
// Upstream, parses responses, dispatches tools, and applies Mechanisms at the
// loop's hook points. It owns the Turn/Step state machine and typed Event
// emission, and holds no ambient process or filesystem state — every state root
// is injected via Config. Sub-agent orchestration (privileges ≤ parent) and the
// Plan / Ask-Before / Auto modes live here too.
//
// See ADR 0001 (embeddable, steppable loop) and ADR 0007 (Step/Turn and the
// quiescent boundary). Phase-0 (P0.6) seeds only the Responder provider seam
// (responder.go) — root-type-free so the root facade imports it one-way; the real
// loop, sub-agents, and modes port here in Phase 1.
package agent
