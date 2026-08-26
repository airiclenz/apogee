package agent

import (
	"crypto/sha256"
	"encoding/hex"
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
// forced-Gate demote, D4 — which a Tier-2 force keeps, along with a Confine leaf's box, when it
// upgrades that leaf to a forced Gate). resolve() DECIDES; dispatch EXECUTES — the executor follows the
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
// table that consumes it, and it is the ONLY classification in the engine: the Plan-mode tool
// menu (loop.go) keys on it through planAdmits rather than re-deriving one of its own, so the
// menu can never offer a tool the ladder then refuses (2026-08-02).

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
	// confineUnavailableRemedy is the Approval remedy the two confinement-unavailable gates carry
	// — the ladder cell that could not fence the subprocess and the runtime demote whose box
	// failed to establish. Same cause, same way out, so both name it in the same words — mirroring
	// the escape `/confine status` already offers (internal/tui/confine.go), condensed to one line
	// because an Approval prompt has room for a sentence, not a paragraph. It is a bare sentence: the
	// "Fix: " label a Driver paints in front of it is presentation, not engine (ADR 0031).
	confineUnavailableRemedy = "/confine off runs commands unconfined this session (disposable machines only)"
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

	// remedy is the optional one-line route out of the condition that forced a Gate, carried to
	// the Approval prompt beside the reason. The two confinement-unavailable gates set it, and so
	// does a Tier-2 forced gate whose rule carries a Hint — the guard's own way out is the route
	// out of the condition that forced this look. A gate the autonomy rung itself asked for has
	// nothing to fix and leaves it empty. Gate only.
	remedy string

	// hint is the guard's MODEL-facing way out on a Tier-2 forced gate: the matched rule's Hint,
	// empty for a rule that offers none and for every gate the guard did not force. It is the
	// same sentence remedy carries, aimed at the other reader — remedy reaches the human on the
	// Approval prompt, hint reaches the model on the deny result, so a denied look tells the
	// model where to go instead of leaving it to loop on rewrites of a call it cannot satisfy
	// (guardRefusalMessage does this for a Tier-1 refusal; this is the Tier-2 half). Gate only.
	hint string

	// force marks a Gate that must SKIP the allow-for-session cache (a Tier-2 force-approval
	// or a runtime-demote fallback). Gate only.
	force bool

	// cacheKey is the allow-for-session cache key for a Gate: the tool name plus a digest of
	// the call's arguments for most classes, so one allow authorises the call the human read
	// and not every later call of that tool; but the SERVER grain "mcp-server:<alias>" for an
	// MCP tool, so approving one of a server's tools clears its siblings for the Session
	// (ADR 0012's server-grain promise). Empty when the arguments do not decode — an
	// unrememberable decision. Gate only.
	cacheKey string

	// writeEscapeTarget is the ONE resolved absolute path this verdict authorises a write to
	// OUTSIDE the workspace fence, which the executor mints onto the execution context as a
	// domain.WriteEscapePermit before the tool runs (ADR 0049). It is set only for a
	// workspace-scoped writer whose target resolves out of the workspace root, and only on the
	// two kinds that actually execute one:
	//
	//   - a GATE, whose allow IS the authorisation — the ladder's out-of-workspace row and a
	//     dangerous-action forced look alike, because approval is final (ADR 0049 Q4): what the
	//     human was shown is what the yes runs;
	//   - a RUN, which is how the Auto · confine-to-workspace:false cell and a target inside the
	//     box's declared writable paths land — both already answered for by the ladder, so
	//     nothing further is asked of the human.
	//
	// Empty everywhere else, and a Refuse never carries one: a call the ladder refused, and a
	// gate the human denied, never reach a permit. Run / Gate only.
	writeEscapeTarget string

	// box is the confinement box a Confine subprocess runs inside. Confine, a Run with
	// confineChildren, and a forced Gate upgraded from a Confine (confineOnAllow).
	box domain.ConfinementBox

	// confineChildren says this Run executes with the Confinement handle installed: a Run whose
	// in-process tool may spawn a subprocess of apogee's own (the workspace-scoped writers' git
	// staging, tools/git_stage.go) executes with the box installed, so that child confines as a
	// git tool's would. The demote logic (D4) stays Confine-only — an unconfinable child here is
	// the tool's own best-effort skip, never a demote, because the file operation has already
	// happened by the time it runs. Run only, and only in the cell where a subprocess would be
	// Confined (applyOverlays).
	confineChildren bool

	// confineOnAllow says this Gate's allow-continuation executes as a Confine rather than as a
	// bare Run. Gate only, and only on the forced Gate a Tier-2 dangerous action upgraded a
	// Confine leaf into: approval decides WHETHER the call runs, confinement decides WHERE, and
	// the guard is tighten-only (ADR 0012), so a forced look never loosens the fence the ladder
	// chose. Such a gate carries the leaf's box and its D4 fallback too.
	confineOnAllow bool

	// fallback is the precomputed runtime-demote contingency for a Confine (D4): a forced
	// Gate (re-run unconfined on allow) or, with no Approver, a Refuse. Structurally bounded
	// — a fallback's own fallback is always nil. Carried by a Confine and by the forced Gate a
	// Tier-2 force upgraded a Confine leaf into (confineOnAllow).
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
	// workspace-scoped writer's target resolves IN-FENCE — inside the workspace root OR inside
	// one of the confinement box's declared writable paths. The UNION is the fence at
	// classification time (ADR 0049): a session that declared a writable path outside the
	// workspace is not made to gate the very path it declared writable. A target that is in the
	// union but outside the root is in-fence here and still needs the escape below to land.
	writeTargetInWorkspace bool
	// writeEscapeTarget is the resolved absolute path a workspace-scoped writer's call would
	// land on when that path lies OUTSIDE the workspace root — in the union or not — and "" for
	// every other call (an in-workspace write, a writer whose call has no inspectable target, a
	// tool that is not one of Apogee's own writers). It is the classified writeTarget.Real, taken
	// from the SAME single resolution writeTargetInWorkspace is derived from, so the fence, the
	// disclosure the pane rendered and the permit the executor mints can never name three
	// different paths.
	writeEscapeTarget string
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
// classifyTool decides which one a tool takes; the classes themselves are just the list, and
// the load-bearing part is the CHECK ORDER there — every unfakeable marker is consulted before
// the self-declared read-only floor.
type toolClass int

const (
	classReadOnly          toolClass = iota // IsReadOnly and NO other marker (the terminal floor)
	classWorkspaceWrite                     // workspaceScopedWriter marker (Apogee's own write)
	classNetwork                            // network + urlFilteredNetworker marker (Apogee's own)
	classThirdPartyNetwork                  // network, no url-filter marker (unfiltered URLs — gates)
	classMCP                                // ExternalEffectTool, kind mcp
	classSubprocess                         // SubprocessTool (shell/exec; OS-confinable)
	classThirdPartyWrite                    // write-capable, none of the above (can't vouch for scoping)
)

// classifyTool maps a tool onto its blast-radius class. The check order IS the invariant: every
// marker that is unfakeable by construction is consulted first — Apogee's own workspace-scoped
// writer, then the external-effect kinds (the network kind splitting on the url-filter marker),
// then the subprocess marker — and classReadOnly is the TERMINAL FLOOR, reached only by a tool
// that no marker claimed. A tool that carries neither a marker nor a read-only declaration is a
// third-party in-process writer.
//
// ReadOnly() is a bare SELF-DECLARATION, so it can never outrank a structural fact about what
// the tool does (ADR 0012 Amendment 2026-07-25(a): classification keys on the marker). A tool
// declaring itself read-only that also launches an OS subprocess (git_diff_range, diagnostics)
// is classified by the subprocess it launches and is confined/gated accordingly; one declaring
// itself read-only that also reaches the network takes a network class and is url-filtered or
// gated. Otherwise a call could be both unsupervised and unbounded — the one thing ADR 0012's
// core invariant forbids. The declaration keeps its own job: it decides the floor for the tools
// no marker claims (read_file, grep, view_diff, list_dir, ask_user) and it is what
// self-regulation's read/write tally reads (selfreg.go). It is NO LONGER what Plan mode's menu
// filter reads (2026-08-02): the menu keys on this class through planAdmits, because a filter
// on the bare declaration offered git_diff_range and diagnostics in Plan and the ladder below
// then refused them.
//
// The network kind splits on the url-filter marker: an EffectNetwork tool that routes through
// internal/tools' network funnel is classNetwork (Apogee vouches that every outbound URL passed
// the host's URLGuard, so it auto-runs in Auto); one WITHOUT the marker is
// classThirdPartyNetwork — its URLs are unfiltered, so it gates instead of reaching the network
// unattended (ADR 0012 Amendment 2026-07-25, the network analogue of classThirdPartyWrite).
func classifyTool(tool domain.Tool) toolClass {
	if tools.IsWorkspaceScopedWriter(tool) {
		return classWorkspaceWrite
	}
	if ext, ok := tool.(domain.ExternalEffectTool); ok {
		if ext.ExternalEffect() == domain.EffectNetwork {
			if tools.IsURLFilteredNetworker(tool) {
				return classNetwork
			}
			return classThirdPartyNetwork
		}
		return classMCP
	}
	if domain.IsSubprocessTool(tool) {
		return classSubprocess
	}
	if domain.IsReadOnly(tool) {
		return classReadOnly
	}
	return classThirdPartyWrite
}

// planAdmits reports whether Plan mode admits tool — the ONE fact the Plan row of the ladder
// (resolveLadder) and the Plan tool-menu filter (loop.go's toolMenu) both key on, so the menu
// can never offer a tool the ladder then refuses.
//
// It is the blast-radius CLASS, never the bare ReadOnly() self-declaration: a tool that declares
// itself read-only while carrying an unfakeable marker — git_diff_range and diagnostics declare
// it and launch an OS subprocess — is classSubprocess, so Plan neither offers nor runs it
// (contract §4 fn 2, resolved 2026-08-02; previously the menu read the declaration and offered
// exactly that pair, which the ladder refused on the call).
//
// The sub_agent recursion point is NOT a leaf tool and never reaches this predicate: resolve()
// Delegates it before the ladder (D3/ADR 0013), and toolMenu keeps it in the Plan menu for the
// same reason — a Plan sub-agent inherits Plan, so its children are read-only too.
func planAdmits(tool domain.Tool) bool {
	return classifyTool(tool) == classReadOnly
}

// resolveLadder ports dispose()/disposeAuto() verbatim: the autonomy-ladder × tool-class ×
// confine-to-workspace × backend-caps table, producing the BARE leaf verdict (kind only,
// plus the box for a Confine). The leaf overlays — guard Tier-2, nil-Approver, gate
// reason/cacheKey, Confine fallback — are applied afterward by applyOverlays.
func resolveLadder(in resolutionInput) resolution {
	class := classifyTool(in.tool)

	switch in.mode {
	case domain.ModePlan:
		// Plan runs the read-only floor and nothing else. The menu filter (loop.go) keys on the
		// SAME predicate, so a refusal here is now defensive only: it catches a host-registered
		// tool the model called without it being on the menu, never a tool Plan itself offered.
		if planAdmits(in.tool) {
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

// resolveLadderAuto ports disposeAuto(): the Auto-mode leaf, tuned by confine-to-workspace
// (the load-bearing column). The one deliberate departure from the P3 table is
// classThirdPartyNetwork, which gates where the undivided network class auto-ran (ADR 0012
// Amendment 2026-07-25 — tighten-only).
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
	case classThirdPartyNetwork:
		// Unfiltered network reach — Apogee cannot vouch for its URLs, so it gates (the
		// network analogue of classThirdPartyWrite).
		return resolution{kind: resolveGate}
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
// a Tier-2 force-approval upgrades any non-Refuse leaf to a forced Gate — carrying the matched
// rule's Hint to both of its readers (remedy for the human's prompt, hint for a denied model),
// and a Confine leaf keeps its box and its D4 fallback through that upgrade, so an approved
// forced look still runs confined; a Gate is finished
// (nil-Approver ⇒ Refuse, else its class reason + cache key, plus the write-escape target its
// allow would authorise); a Confine is finished (box + runtime-demote fallback); a Run / Refuse
// carries the guard's audit metadata where today's trail records it, and a Run also carries the
// write-escape target the ladder already answered for — plus, in the confining Auto cell, the box
// a workspace-scoped writer's own git child runs inside.
func applyOverlays(in resolutionInput, leaf resolution) resolution {
	// A Tier-2 dangerous action forces the Approver even where the ladder would not (the
	// guardrail can only tighten — ADR 0012). A Refuse leaf stays refused.
	//
	// The rule's Hint rides the upgrade in both directions: as the prompt's remedy line, so the
	// human reads the sanctioned route beside the question, and as the gate's model-facing hint,
	// so a DENIED look answers the model with that route instead of a bare refusal (the Tier-1
	// half of this is guardRefusalMessage). A rule without a Hint leaves both empty — today's
	// prompt, unchanged.
	//
	// A CONFINE leaf keeps its box and its D4 fallback across the upgrade — both taken from the
	// input, exactly as finishConfine would have taken them (the bare ladder leaf carries neither
	// yet): approval decides
	// WHETHER the call runs, confinement decides WHERE, and the guard is tighten-only, so a
	// forced look must never loosen the fence the ladder had already chosen. The allow-
	// continuation therefore executes as the Confine would have (confineOnAllow), demote
	// contingency included. A Run or Gate leaf had no fence to keep, so its upgrade is the
	// bare forced gate.
	if in.guard.Outcome == security.GuardForceApproval && leaf.kind != resolveRefuse {
		if leaf.kind == resolveConfine {
			leaf = resolution{
				kind:           resolveGate,
				force:          true,
				reason:         forceApprovalReason,
				remedy:         in.guard.Hint,
				hint:           in.guard.Hint,
				box:            in.box,
				confineOnAllow: true,
				fallback:       confineFallback(in),
			}
		} else {
			leaf = resolution{
				kind:   resolveGate,
				force:  true,
				reason: forceApprovalReason,
				remedy: in.guard.Hint,
				hint:   in.guard.Hint,
			}
		}
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
		// A Run the ladder produced for an out-of-workspace target is one the ladder itself
		// authorised — the "I am the sandbox" cell, or a target inside the box's declared
		// writable paths. It executes with the permit, which is the only way those two cells
		// stop being nullified by a fence pinned to the workspace root (ADR 0049 Q3/Q7).
		leaf.writeEscapeTarget = in.writeEscapeTarget
		// A workspace-scoped writer's Run in the very cell where a SUBPROCESS call would be
		// Confined carries the box, so the git child its staging spawns (tools/git_stage.go)
		// is fenced exactly as a git tool's child would be. Auto + confine-to-workspace + caps
		// is the whole condition, because that is the cell — and the only cell — whose answer
		// for a subprocess is "the OS fences it".
		//
		// The lower rungs deliberately get nothing: in Allow-Edits and in the gate cell the
		// child's bound is its HARDENED ARGV — apogee's own fixed `git add -A -- :(literal)<path>`
		// with the repository's command-config refused, not a model-chosen program — which is
		// the blast radius classWorkspaceWrite already declares, so ADR 0012 D5's "no Confine in
		// the lower modes" is kept intact.
		if classifyTool(in.tool) == classWorkspaceWrite && in.mode == domain.ModeAuto &&
			in.confineToWorkspace && in.fsConfineAvailable {
			leaf.box = in.box
			leaf.confineChildren = true
		}
		return leaf
	}
}

// finishGate completes a Gate leaf. A gate with no Approver configured cannot actually
// consult a human, so it refuses rather than run unapproved (D5) — a Gate always means the
// Approver is consulted. Otherwise it takes its allow-for-session cache key (gateCacheKey: the
// tool name plus its argument digest, or the MCP server grain) and, unless a forced reason was
// already set, its
// blast-radius class reason and the remedy that goes with it (gateReason yields the pair, so a
// gate can never end up blaming one condition and prescribing the fix for another).
//
// It only ADDS to the gate it is handed, so a forced Gate upgraded from a Confine keeps the box,
// confineOnAllow and fallback applyOverlays put on it, and a forced gate of any leaf keeps the
// remedy and hint its rule's Hint supplied (the class remedy is reached only through the same
// empty-reason branch the class reason is). The nil-Approver branch is the one exception, and
// deliberately so: it builds a fresh Refuse, and a refused call confines nothing.
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
	// What this gate's allow would authorise beyond the fence — empty for every gate but an
	// out-of-workspace write. It is attached AFTER the Tier-2 upgrade above deliberately: a
	// forced look is still a look, and the human's yes to the path the pane disclosed runs
	// (ADR 0049 Q4). It rides an allow the session REMEMBERED too — the cache key carries the
	// call's canonical arguments, so a remembered yes answers the same call, and the target
	// stamped is always this call's own fresh resolution rather than the one that was cached.
	gate.writeEscapeTarget = in.writeEscapeTarget
	if gate.reason == "" {
		gate.reason, gate.remedy = gateReason(in)
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

// mcpServerAlias reports the server alias an MCP tool was qualified with, and whether this tool
// exposes one at all. It is the ONE reading of the serverAliaser marker: the allow-for-session key
// an MCP gate carries (gateCacheKey) and the disclosure the Approval makes about that key's grain
// (dispatch.go's mcpServerGrant) are then two statements about a single fact, and cannot end up
// describing the same grant differently.
//
// ok is false for every non-MCP tool and for an MCP tool that exposes no alias — the tighten-only
// degradation to a tool-grain key, which reaches no further than the call the human read, so there
// is nothing wider to disclose. The alias itself may be EMPTY with ok TRUE: the single unnamed
// server is one grain like any other.
func mcpServerAlias(tool domain.Tool) (alias string, ok bool) {
	if classifyTool(tool) != classMCP {
		return "", false
	}
	aliaser, exposed := tool.(serverAliaser)
	if !exposed {
		return "", false
	}
	return aliaser.ServerAlias(), true
}

// gateArgumentsSeparator sits between a gate cache key's tool name and its argument digest. It
// is a byte no tool name carries, and the digest that follows it is fixed-length, so two
// argument-grain keys are equal only when both the tool and the arguments are.
const gateArgumentsSeparator = "\x00"

// gateCacheKey is the allow-for-session cache key a Gate carries — the identity an "allow for
// this session" is remembered under, and therefore the blast radius of that one answer.
//
// For an MCP tool it is the SERVER grain "mcp-server:<alias>", so approving one of a server's
// tools clears its siblings for the Session (ADR 0012); the "mcp-server:" prefix keeps that
// grain collision-proof against ordinary tool names, and the empty-alias (single unnamed server)
// case is still one grain. An MCP tool that does not expose its alias degrades to the tool name,
// a tighten-only fallback.
//
// Every other class keys on the tool name PLUS a digest of the call's arguments, because the
// tool name alone made one answer stand for every later call of that tool: an "allow for
// session" on `terminal` pre-cleared every shell command for the rest of the Session, in this
// Agent and — the memory being the whole tree's (approvalcache.go) — in its parent and siblings
// too. With the arguments in the key, the answer authorises the call the human actually read.
// The deliberate cost is ergonomic: allowing `npm test` no longer clears `npm run build`.
func gateCacheKey(tool domain.Tool, call domain.ToolCall) string {
	if classifyTool(tool) == classMCP {
		if alias, exposed := mcpServerAlias(tool); exposed {
			return mcpServerCacheKeyPrefix + alias
		}
		return call.Tool
	}
	digest, ok := argumentsDigest(call)
	if !ok {
		return ""
	}
	return call.Tool + gateArgumentsSeparator + digest
}

// argumentsDigest hashes what the executor will actually run: the call's arguments in the
// canonical spelling tools.CanonicalArgs defines (keys sorted, a duplicated key collapsed to the
// last — the one stdlib JSON hands the executor, and the one the approval pane now shows), bound
// to the tool name so no two tools can share a digest.
//
// It reports ok=false when the arguments do not decode, and the caller then emits the EMPTY key.
// That is the conservative direction and it needs no further handling: the memory refuses an
// empty key on both sides (approvalCache.Allowed / .Allow), so a call whose arguments cannot be
// pinned down is asked about every time rather than remembered under a key that might not
// describe it.
func argumentsDigest(call domain.ToolCall) (string, bool) {
	canonical, err := tools.CanonicalArgs(call.Arguments)
	if err != nil {
		return "", false
	}
	material := make([]byte, 0, len(call.Tool)+1+len(canonical))
	material = append(material, call.Tool...)
	material = append(material, 0)
	material = append(material, canonical...)
	sum := sha256.Sum256(material)
	return hex.EncodeToString(sum[:]), true
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
// fallback never carries its own fallback — the demote is a single, bounded step. The gate
// carries the same remedy as the caps-insufficient ladder cell: the two prompts differ only in
// WHEN the host's incapacity was discovered, and the way out of both is the same one command.
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
		remedy:        confineUnavailableRemedy,
		cacheKey:      in.call.Tool,
		auditDecision: in.guard.Audit,
		auditReason:   in.guard.Reason,
	}
}

// gateReason maps a gated tool onto the human-facing why for the Approval prompt, and the
// optional remedy that goes with it. It reproduces the P3 approvalReason() mapping, plus the
// third-party-network reason the vouched-for/unvouched network split added (ADR 0012 Amendment
// 2026-07-25). Six of the seven classes are a bare statement of the reach being authorised, so
// the class alone decides them; the subprocess class also reads the ladder CELL, because only
// one of its cells is a confinement failure (see subprocessGateReason).
//
// The remedy is empty for every class but that one cell: a gate the autonomy rung itself asked
// for has no condition to lift, and offering a fix for it would be an invitation to widen the
// blast radius the user chose. Reason and remedy leave together so a prompt cannot name one
// cause and prescribe another's fix.
func gateReason(in resolutionInput) (reason, remedy string) {
	switch classifyTool(in.tool) {
	case classNetwork:
		return "network reach", ""
	case classThirdPartyNetwork:
		return "unfiltered network reach", ""
	case classMCP:
		return "unconfinable MCP tool", ""
	case classSubprocess:
		return subprocessGateReason(in)
	case classWorkspaceWrite:
		return "out-of-workspace write", ""
	default:
		return "write", ""
	}
}

// subprocessGateReason words a gated subprocess call by the ladder cell it was gated in, and
// hands back the remedy for that cell in the same breath — ONE cell predicate, written once,
// yielding both, so the diagnosis and the fix can never drift apart. In
// Auto with confinement asked for, a gate means the backend could not give the fence ("confine
// if you can, gate if you can't"), so the host's incapacity IS the reason and naming it is what
// points the user at /confine. Every other rung gates the subprocess surface as a MODE
// decision, capable host or not — claiming otherwise blamed a working seatbelt/landlock backend
// for an approval the rung itself asked for, and sent the user to /confine status to be told the
// opposite (dated 2026-08-11). The three-term test spells the cell out: confineToWorkspace ==
// false in Auto cannot reach a subprocess gate today (resolveLadderAuto auto-runs it), so that
// term documents the cell rather than carrying live logic.
func subprocessGateReason(in resolutionInput) (reason, remedy string) {
	if in.mode == domain.ModeAuto && in.confineToWorkspace && !in.fsConfineAvailable {
		return "subprocess execution (confinement unavailable on this host)", confineUnavailableRemedy
	}
	return "subprocess execution", ""
}
