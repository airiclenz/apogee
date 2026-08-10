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
//
// # The files, one line each
//
// Eighteen files: the handle and its lifecycle, the loop proper, the tool path, and the
// mid-session doors a host opens without tearing the session down.
//
// The handle. agent.go is the Agent type and the surface a Driver holds — New, Resume, Close,
// Submit, Step, Run, AbortExchange, Snapshot, and the setters for mode, Bypass, compaction,
// context files and the parallel-agent cap. construct.go is newAgent: the validation order
// every construction path shares — required fields, then the ordering-cycle, incompatibility
// and requirements gates, then the Auto/Confinement gate (ADR 0012). state.go is agentState,
// the v1 payload inside domain's Session envelope, with the encode and the restore that
// rebuilds a quiescent loop from it.
//
// The loop. loop.go is the Step body: arming and building the request, the standing system
// content, file and skill reference resolution, the streamed upstream call, response assembly
// and tool-call parsing, and the post-response retry cap. turn.go is turnLifecycle, the
// Turn/Exchange state between quiescent boundaries (ADR 0007) and the exits that mutate the
// conversation and the self-regulator together. hookrun.go fires the hooks at each point —
// catalogued Mechanisms first in the registry's total order, then the bench's experimental
// hooks — each under one recover boundary and attributed by MechanismID. wire.go is the
// translation onto the provider seam: the domain request drained into a domain-free
// provider.Request (ADR 0010). compact.go is conversation compaction — the explicit Compact,
// the auto-compaction trigger and its allocation arithmetic, the emergency fold, and the
// summarizer's own fixed sampling. selfreg.go is per-Session self-regulation: the proxy-signal
// safety net that withdraws a Mechanism hurting the model, deliberately weaker than the
// bench's A/B gate and reset on Resume.
//
// The tool path. resolution.go computes the per-call Resolution — the complete verdict before
// anything executes: the guardrail floor, the ladder-by-blast-radius table, the confinement
// capability check, and the precomputed fallback for what only run time can reveal.
// dispatch.go executes it: the serial and depth-0 fan-out paths, the run/gate/confine/delegate/
// refuse arms, Approval, result clamping, and the audit records. subagent.go is the sub-agent
// orchestrator — a nested Agent whose privileges are the parent's verbatim or stricter, with a
// tool set that is a subset and never an expansion (ADR 0013).
//
// The mid-session doors. interject.go commits the human's remark into the OPEN Exchange at a
// between-Steps boundary. rebind.go swaps every per-model binding together when the Upstream's
// loaded model changes, and moves the session to another server (ADR 0024). setprofile.go is
// the separate, explicit door for changing the model PROFILE, which Rebind deliberately leaves
// alone (ADR 0037). swaptools.go is the single door for handing the engine a freshly built
// tool registry, so no second registry-mutation path has to exist. contextfiles.go owns the
// workspace context files' discovery half — the session-scoped cache, its loader, and the
// construction-time name gate.
//
// And doc.go this map.
package agent
