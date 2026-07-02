package agent

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The per-call Resolution — the one verdict dispatch executes (D1–D6;
// confinement-execution-contract §4; CONTEXT.md → Resolution)
// ----------------------------------------------------------------------------
//
// A resolution is the COMPLETE, per-call verdict — every rule that decides a tool call's
// fate, computed in full BEFORE anything executes: the tighten-only guardrail floor, the
// autonomy-ladder × blast-radius table, the confinement-capability check, and the
// precomputed contingency for what can only be discovered at run time (the Confine ⇒
// forced-Gate demote, D4). resolve() DECIDES; dispatch EXECUTES — the executor follows the
// plan and never re-derives it. This supersedes the Phase-3 "per-call disposition", which
// named only the ladder-table stage that runs after the guard clears.
//
// resolve is a HERMETICALLY PURE free function (D6): it does NO I/O and reads only its
// resolutionInput. The one I/O-tainted fact — whether a workspace-scoped writer's target is
// in the workspace (EvalRealPath touches disk) — is precomputed by dispatch and passed as a
// bool. This keeps the whole autonomy-ladder decision in one table-testable place.
//
// resolve() is the single decision point: dispatch (dispatch.go) is a thin executor that
// gathers the facts resolutionInput carries, calls resolve() once, and mechanically carries
// out the verdict — it holds no ladder, guard-tier, or demote decision of its own. The
// tool-classification the ladder keys on (classifyTool / toolClass) lives here too, beside the
// table that consumes it.

// Model-facing refusal text and human-facing Approval reasons carried on a resolution. They
// reproduce today's exact strings (dispatch.go / disposition.go) so the rewire in item 2 is
// behaviour-preserving.
const (
	// planRefusalReason is returned to the model when Plan mode refuses a write tool the
	// menu should already have hidden (a defensive refusal).
	planRefusalReason = "plan mode: write tools are not permitted"
	// forceApprovalReason is the Approval prompt reason for a gate a Tier-2 dangerous action
	// forced (a per-call speed-bump, not a pre-allowable convenience).
	forceApprovalReason = "dangerous-action guard forced approval"
	// noApproverReason is the refusal when a gate is required but no Approver is configured:
	// a Gate always means the Approver is actually consulted, so a nil Approver refuses
	// rather than running unapproved (D5).
	noApproverReason = "approval required but no Approver configured"
	// confineDemoteGateReason is the Approval reason for the runtime-demote fallback gate: a
	// Confine whose box could not be established at run time re-runs unconfined on allow (D4).
	confineDemoteGateReason = "subprocess execution (confinement unavailable on this host)"
	// confineDemoteRefuseReason is the runtime-demote fallback when no Approver is configured:
	// the subprocess could not be confined and no human could authorise the unconfined run.
	confineDemoteRefuseReason = "subprocess could not be confined and approval was not granted"
)

// resolutionKind is the class of verdict resolve() computes for one tool call. It is the
// single discriminator dispatch switches on to execute the call (D2).
type resolutionKind int

const (
	// resolveRun executes the call directly — no Approval, no Confine.
	resolveRun resolutionKind = iota
	// resolveConfine executes the call's subprocess inside Confiner.Confine (box carried).
	resolveConfine
	// resolveGate routes the call through Approval (allow-for-session caches apply unless
	// force is set).
	resolveGate
	// resolveRefuse refuses the call outright with a model-facing reason.
	resolveRefuse
	// resolveDelegate drives the sub_agent recursion point (a nested Agent), not a leaf tool.
	resolveDelegate
)

// String renders the kind for readable test and audit output.
func (k resolutionKind) String() string {
	switch k {
	case resolveRun:
		return "run"
	case resolveConfine:
		return "confine"
	case resolveGate:
		return "gate"
	case resolveRefuse:
		return "refuse"
	case resolveDelegate:
		return "delegate"
	default:
		return fmt.Sprintf("resolutionKind(%d)", int(k))
	}
}

// resolution is the complete verdict for one tool call. Beyond its kind it carries only the
// fields the relevant kind needs (D4/D5), plus the audit metadata the executor records so
// the trail stays byte-identical to today.
type resolution struct {
	// kind is the verdict class dispatch executes.
	kind resolutionKind

	// reason is the model-facing refusal text (Refuse) or the human-facing Approval prompt
	// reason (Gate). Empty for Run / Confine / Delegate.
	reason string

	// force marks a Gate that must SKIP the allow-for-session cache (a Tier-2 force-approval
	// or a runtime-demote fallback). Gate only.
	force bool

	// cacheKey is the allow-for-session cache key for a Gate: the tool name for most classes,
	// but the SERVER grain "mcp-server:<alias>" for an MCP tool, so approving one of a server's
	// tools clears its siblings for the Session (ADR 0012's server-grain promise). Gate only.
	cacheKey string

	// box is the confinement box a Confine subprocess runs inside. Confine only.
	box domain.ConfinementBox

	// fallback is the precomputed runtime-demote contingency for a Confine (D4): a forced
	// Gate (re-run unconfined on allow) or, with no Approver, a Refuse. Structurally bounded
	// — a fallback's own fallback is always nil. Confine only.
	fallback *resolution

	// auditDecision and auditReason are what the executor records for this verdict, so its
	// recordExecuted / recordBlocked calls stay byte-identical (D8). An EMPTY auditDecision
	// means the verdict is NOT audit-recorded — today's quirk for the unknown-tool and
	// Plan-mode-write refusals (a guard refusal, a gate deny, and every executed call ARE
	// recorded, with the guard's pass-through decision).
	auditDecision security.AuditDecision
	auditReason   string
}

// resolutionInput is the complete set of facts resolve() decides from. Dispatch gathers
// these (running the registry lookup, the guardrails, the caps probe, and the one on-disk
// write-target check) and hands them over; resolve() itself does NO I/O (D6).
type resolutionInput struct {
	// mode is the EFFECTIVE autonomy mode for this call (a sub-agent's is already tightened
	// to min(spawn, parent-live) — ADR 0013).
	mode domain.Mode
	// call is the parsed tool call.
	call domain.ToolCall
	// tool is the resolved tool, or nil for an unknown tool (a registry miss).
	tool domain.Tool
	// guard is the always-on guardrail verdict (dangerous-action floor + circuit-breaker),
	// run before the ladder and tighten-only (ADR 0012).
	guard security.PreCheck
	// confineToWorkspace is the confine-to-workspace flag (the load-bearing Auto column).
	confineToWorkspace bool
	// fsConfineAvailable reports whether the injected Confiner can enforce fs confinement on
	// this host (Capabilities().FSWrite) — the caps gate before choosing to confine.
	fsConfineAvailable bool
	// writeTargetInWorkspace is precomputed by dispatch (EvalRealPath is I/O): whether a
	// workspace-scoped writer's target resolves inside the workspace root.
	writeTargetInWorkspace bool
	// atDepthBound is true when spawning a sub-agent here would reach maxSubAgentDepth.
	atDepthBound bool
	// approverPresent reports whether an Approver is configured (a gate with none refuses).
	approverPresent bool
	// box is the prebuilt confinement box a Confine verdict carries.
	box domain.ConfinementBox
}

// resolve computes the complete Resolution for one tool call. It is pure given its input and
// applies the rules in a fixed, load-bearing order (confinement-execution-contract §4):
//
//  1. A guard hard-refuse (Tier-1 dangerous action or a tripped circuit-breaker) refuses in
//     every mode — the tighten-only floor runs before the ladder.
//  2. The sub_agent recursion point is Delegated, not run as a leaf. A Tier-2 force-approval
//     is DELIBERATELY not applied to a Delegate (D3/ADR 0013): nothing executes at
//     delegation, so the shared read-only floor re-fires on the child's own dangerous call.
//     At the depth bound the delegation is refused defensively (mirrors runSubAgent).
//  3. An unknown tool refuses (not audit-recorded today, D8).
//  4. The autonomy-ladder × blast-radius table produces the leaf verdict, then the leaf
//     overlays apply: a Tier-2 force upgrades a non-Refuse leaf to a forced Gate; a Gate with
//     no Approver refuses; a Gate gets its class reason + cache key; a Confine gets its box
//     and its precomputed runtime-demote fallback.
func resolve(in resolutionInput) resolution {
	// 1. Guard hard-refuse (Tier-1 / tripped breaker).
	if in.guard.Outcome == security.GuardRefuse {
		return resolution{
			kind:          resolveRefuse,
			reason:        guardRefusalMessage(in.guard),
			auditDecision: in.guard.Audit,
			auditReason:   in.guard.Reason,
		}
	}

	// 2. The sub_agent recursion point (Tier-2 is intentionally NOT applied here — D3).
	if isSubAgentCall(in.call) {
		if in.atDepthBound {
			return resolution{
				kind: resolveRefuse,
				reason: fmt.Sprintf(
					"sub-agent depth limit reached (max %d): cannot spawn a deeper sub-agent", maxSubAgentDepth),
				auditDecision: in.guard.Audit,
				auditReason:   in.guard.Reason,
			}
		}
		return resolution{
			kind:          resolveDelegate,
			auditDecision: in.guard.Audit,
			auditReason:   in.guard.Reason,
		}
	}

	// 3. Unknown tool — refuse, NOT audit-recorded (D8).
	if in.tool == nil {
		return resolution{
			kind:   resolveRefuse,
			reason: fmt.Sprintf("unknown tool %q", in.call.Tool),
		}
	}

	// 4. The ladder table, then the leaf overlays.
	return applyOverlays(in, resolveLadder(in))
}

// toolClass is the blast-radius class the ladder keys on (confinement-execution-contract §4).
// The order is significant: read-only wins (a read never gates), then the unfakeable
// workspace-scoped-writer marker, then the external-effect kinds, then the subprocess marker,
// and finally a write-capable tool Apogee cannot vouch for (3p-write).
type toolClass int

const (
	classReadOnly        toolClass = iota // IsReadOnly
	classWorkspaceWrite                   // workspaceScopedWriter marker (Apogee's own write)
	classNetwork                          // ExternalEffectTool, kind network
	classMCP                              // ExternalEffectTool, kind mcp
	classSubprocess                       // SubprocessTool (shell/exec; OS-confinable)
	classThirdPartyWrite                  // write-capable, none of the above (can't vouch for scoping)
)

// classifyTool maps a tool onto its blast-radius class. The classes are checked in a fixed
// priority so a tool implementing several markers resolves deterministically: read-only first
// (harmless), then Apogee's own writer, then the external kinds, then the confinable subprocess
// surface, else a third-party in-process writer.
func classifyTool(tool domain.Tool) toolClass {
	if domain.IsReadOnly(tool) {
		return classReadOnly
	}
	if tools.IsWorkspaceScopedWriter(tool) {
		return classWorkspaceWrite
	}
	if ext, ok := tool.(domain.ExternalEffectTool); ok {
		if ext.ExternalEffect() == domain.EffectNetwork {
			return classNetwork
		}
		return classMCP
	}
	if domain.IsSubprocessTool(tool) {
		return classSubprocess
	}
	return classThirdPartyWrite
}

// resolveLadder ports dispose()/disposeAuto() verbatim: the autonomy-ladder × tool-class ×
// confine-to-workspace × backend-caps table, producing the BARE leaf verdict (kind only,
// plus the box for a Confine). The leaf overlays — guard Tier-2, nil-Approver, gate
// reason/cacheKey, Confine fallback — are applied afterward by applyOverlays.
func resolveLadder(in resolutionInput) resolution {
	class := classifyTool(in.tool)

	switch in.mode {
	case domain.ModePlan:
		// Plan filters the menu to read-only tools; anything else is refused defensively.
		if class == classReadOnly {
			return resolution{kind: resolveRun}
		}
		return resolution{kind: resolveRefuse, reason: planRefusalReason}

	case domain.ModeAllowEdits:
		// Apogee's own in-workspace writes auto-approve; everything unbounded (and any
		// out-of-workspace write) gates. NO Confine is ever invoked here (ADR 0012 D5).
		if class == classReadOnly {
			return resolution{kind: resolveRun}
		}
		if class == classWorkspaceWrite && in.writeTargetInWorkspace {
			return resolution{kind: resolveRun}
		}
		return resolution{kind: resolveGate}

	case domain.ModeAuto:
		return resolveLadderAuto(in, class)

	default:
		// An empty / unknown mode is Ask-Before — gate every write/exec/external, run only
		// harmless reads.
		if class == classReadOnly {
			return resolution{kind: resolveRun}
		}
		return resolution{kind: resolveGate}
	}
}

// resolveLadderAuto ports disposeAuto() verbatim: the Auto-mode leaf, tuned by
// confine-to-workspace (the load-bearing column).
func resolveLadderAuto(in resolutionInput, class toolClass) resolution {
	if !in.confineToWorkspace {
		// "I am the sandbox" (VM-only): everything auto-runs unfenced. The dangerous-action
		// floor already ran (and may have forced approval / refused) before this point.
		return resolution{kind: resolveRun}
	}

	// confine-to-workspace = true (the default).
	switch class {
	case classReadOnly:
		return resolution{kind: resolveRun}
	case classWorkspaceWrite:
		// An in-workspace Apogee write runs path-safety-bounded; an out-of-workspace one gates.
		if in.writeTargetInWorkspace {
			return resolution{kind: resolveRun}
		}
		return resolution{kind: resolveGate}
	case classNetwork:
		// Native network tools auto-run url-filtered — the network is open (ADR 0012).
		return resolution{kind: resolveRun}
	case classMCP:
		// MCP executes in a server Apogee cannot fence: gate (server-grain allow-for-session).
		return resolution{kind: resolveGate}
	case classSubprocess:
		// Confine if the backend can, else gate ("confine if you can, gate if you can't").
		if in.fsConfineAvailable {
			return resolution{kind: resolveConfine}
		}
		return resolution{kind: resolveGate}
	default: // classThirdPartyWrite
		// A write-capable tool Apogee cannot vouch for: gate.
		return resolution{kind: resolveGate}
	}
}

// applyOverlays folds the leaf-verdict overlays onto the bare ladder verdict, in order (D5):
// a Tier-2 force-approval upgrades any non-Refuse leaf to a forced Gate; a Gate is finished
// (nil-Approver ⇒ Refuse, else its class reason + cache key); a Confine is finished (box +
// runtime-demote fallback); a Run / Refuse carries the guard's audit metadata where today's
// trail records it.
func applyOverlays(in resolutionInput, leaf resolution) resolution {
	// A Tier-2 dangerous action forces the Approver even where the ladder would not (the
	// guardrail can only tighten — ADR 0012). A Refuse leaf stays refused.
	if in.guard.Outcome == security.GuardForceApproval && leaf.kind != resolveRefuse {
		leaf = resolution{kind: resolveGate, force: true, reason: forceApprovalReason}
	}

	switch leaf.kind {
	case resolveGate:
		return finishGate(in, leaf)
	case resolveConfine:
		return finishConfine(in, leaf)
	case resolveRefuse:
		// A Plan-mode write refusal carries its model-facing reason already and is NOT
		// audit-recorded today (D8), so it needs no audit metadata.
		return leaf
	default: // resolveRun
		leaf.auditDecision = in.guard.Audit
		leaf.auditReason = in.guard.Reason
		return leaf
	}
}

// finishGate completes a Gate leaf. A gate with no Approver configured cannot actually
// consult a human, so it refuses rather than run unapproved (D5) — a Gate always means the
// Approver is consulted. Otherwise it takes its allow-for-session cache key (gateCacheKey: the
// tool name, or the MCP server grain) and, unless a forced reason was already set, its
// blast-radius class reason.
func finishGate(in resolutionInput, gate resolution) resolution {
	if !in.approverPresent {
		return resolution{
			kind:          resolveRefuse,
			reason:        noApproverReason,
			auditDecision: in.guard.Audit,
			auditReason:   in.guard.Reason,
		}
	}
	gate.cacheKey = gateCacheKey(in.tool, in.call)
	if gate.reason == "" {
		gate.reason = gateReason(in.tool)
	}
	gate.auditDecision = in.guard.Audit
	gate.auditReason = in.guard.Reason
	return gate
}

// mcpServerCacheKeyPrefix namespaces an MCP gate's server-grain allow-for-session key so it can
// never collide with an ordinary tool-name key (ADR 0012's server-grain promise).
const mcpServerCacheKeyPrefix = "mcp-server:"

// serverAliaser is the optional interface an MCP tool implements to expose the server alias it
// was qualified with. It lets resolve() key an MCP gate's allow-for-session cache at SERVER
// grain WITHOUT internal/agent importing internal/mcp — the surfaced serverTool (internal/mcp)
// satisfies it structurally. An MCP tool that does not implement it degrades to the per-tool
// key (a safe, tighten-only fallback).
type serverAliaser interface {
	ServerAlias() string
}

// gateCacheKey is the allow-for-session cache key a Gate carries. For an MCP tool it is the
// SERVER grain "mcp-server:<alias>", so approving one of a server's tools clears its siblings
// for the Session (ADR 0012); the "mcp-server:" prefix keeps that grain collision-proof against
// ordinary tool names, and the empty-alias (single unnamed server) case is still one grain.
// Every other class — and an MCP tool that does not expose its alias — keys on the tool name,
// today's tighter per-tool grain, so the change never loosens a non-MCP gate.
func gateCacheKey(tool domain.Tool, call domain.ToolCall) string {
	if classifyTool(tool) == classMCP {
		if sa, ok := tool.(serverAliaser); ok {
			return mcpServerCacheKeyPrefix + sa.ServerAlias()
		}
	}
	return call.Tool
}

// finishConfine completes a Confine leaf: it attaches the prebuilt box and the precomputed
// runtime-demote fallback (D4), and carries the guard's audit metadata for the executed run.
func finishConfine(in resolutionInput, confine resolution) resolution {
	confine.box = in.box
	confine.auditDecision = in.guard.Audit
	confine.auditReason = in.guard.Reason
	confine.fallback = confineFallback(in)
	return confine
}

// confineFallback builds the one bounded runtime-demote contingency every Confine carries
// (D4): if the box cannot be established at run time, the call demotes to a FORCED gate whose
// allow-continuation is a re-run UNCONFINED; with no Approver it refuses instead. The
// fallback never carries its own fallback — the demote is a single, bounded step.
func confineFallback(in resolutionInput) *resolution {
	if !in.approverPresent {
		return &resolution{
			kind:          resolveRefuse,
			reason:        confineDemoteRefuseReason,
			auditDecision: in.guard.Audit,
			auditReason:   in.guard.Reason,
		}
	}
	return &resolution{
		kind:          resolveGate,
		force:         true,
		reason:        confineDemoteGateReason,
		cacheKey:      in.call.Tool,
		auditDecision: in.guard.Audit,
		auditReason:   in.guard.Reason,
	}
}

// gateReason maps a gated tool onto the human-facing why for the Approval prompt, derived
// from its blast-radius class so the human sees what kind of reach they are authorising. It
// reproduces today's approvalReason() mapping exactly.
func gateReason(tool domain.Tool) string {
	switch classifyTool(tool) {
	case classNetwork:
		return "network reach"
	case classMCP:
		return "unconfinable MCP tool"
	case classSubprocess:
		return "subprocess execution (confinement unavailable on this host)"
	case classWorkspaceWrite:
		return "out-of-workspace write"
	default:
		return "write"
	}
}
