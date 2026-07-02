package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The pure Resolution resolver (D1–D6; confinement-execution-contract §4)
// ----------------------------------------------------------------------------
//
// These tests are the exhaustive table for resolve(): every guard-outcome × mode × tool-class
// × caps × confine-to-workspace × approver-present combination resolves to one verdict, with
// its reason / force / cacheKey / box / fallback shape pinned. They reuse the fake tools from
// dispatch_test.go / statemachine_test.go (fakeTool, subprocTool, externalTool,
// thirdPartyWriter) — resolve() reads only its input, so the tools need only classify
// correctly. writeTargetInWorkspace / fsConfineAvailable / approverPresent are explicit input
// bools (resolve does NO I/O), so the whole table is hermetic.

// proceed is the always-on guardrail's "no guard fired" verdict — the ladder runs from here.
var proceed = security.PreCheck{Outcome: security.GuardProceed, Audit: security.AuditAllowed}

// ----------------------------------------------------------------------------
// The autonomy-ladder × blast-radius table (guard cleared, Approver present)
// ----------------------------------------------------------------------------

// TestResolve_LadderTable pins every §4 ladder cell: (mode, tool-class, confine-to-workspace,
// caps, write-target-in-workspace) → verdict, with the gate reason / cacheKey and the confine
// box + fallback shape asserted. The guard has cleared (Proceed) and an Approver is present,
// so this is the pure ladder — the overlays get their own tests below.
func TestResolve_LadderTable(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()

	ro := fakeTool{name: "read_file", readOnly: true}
	wsw := tools.NewWriteFile(ws) // carries the workspaceScopedWriter marker
	net := externalTool{name: "web-fetch", kind: domain.EffectNetwork}
	mcp := externalTool{name: "github", kind: domain.EffectMCP}
	sub := &subprocTool{name: "terminal"}
	tpw := thirdPartyWriter{name: "weird"}

	const badMode = domain.Mode("bogus") // an off-ladder mode ⇒ Ask-Before default

	cases := []struct {
		name       string
		tool       domain.Tool
		mode       domain.Mode
		confine    bool
		fsConfine  bool
		writeIn    bool
		wantKind   resolutionKind
		wantReason string // gate/refuse reason ("" = do not assert)
		wantAudit  security.AuditDecision
	}{
		// read-only — runs in every mode, independent of confine/caps.
		{"RO/plan", ro, domain.ModePlan, true, true, true, resolveRun, "", security.AuditAllowed},
		{"RO/ask-before", ro, domain.ModeAskBefore, true, true, true, resolveRun, "", security.AuditAllowed},
		{"RO/allow-edits", ro, domain.ModeAllowEdits, true, true, true, resolveRun, "", security.AuditAllowed},
		{"RO/auto-confine", ro, domain.ModeAuto, true, true, true, resolveRun, "", security.AuditAllowed},
		{"RO/auto-noconfine", ro, domain.ModeAuto, false, true, true, resolveRun, "", security.AuditAllowed},
		{"RO/unknown-mode", ro, badMode, true, true, true, resolveRun, "", security.AuditAllowed},

		// workspace-scoped write — in/out split; NO Confine ever.
		{"WS/plan", wsw, domain.ModePlan, true, true, true, resolveRefuse, planRefusalReason, ""},
		{"WS/ask-before", wsw, domain.ModeAskBefore, true, true, true, resolveGate, "out-of-workspace write", security.AuditAllowed},
		{"WS-in/allow-edits", wsw, domain.ModeAllowEdits, true, true, true, resolveRun, "", security.AuditAllowed},
		{"WS-out/allow-edits", wsw, domain.ModeAllowEdits, true, true, false, resolveGate, "out-of-workspace write", security.AuditAllowed},
		{"WS-in/auto-confine", wsw, domain.ModeAuto, true, true, true, resolveRun, "", security.AuditAllowed},
		{"WS-out/auto-confine", wsw, domain.ModeAuto, true, true, false, resolveGate, "out-of-workspace write", security.AuditAllowed},
		{"WS/auto-noconfine", wsw, domain.ModeAuto, false, true, false, resolveRun, "", security.AuditAllowed},
		{"WS/unknown-mode", wsw, badMode, true, true, false, resolveGate, "out-of-workspace write", security.AuditAllowed},

		// subprocess — confine when caps suffice, else gate ("confine if you can, gate if you can't").
		{"subproc/plan", sub, domain.ModePlan, true, true, true, resolveRefuse, planRefusalReason, ""},
		{"subproc/ask-before", sub, domain.ModeAskBefore, true, true, true, resolveGate, "subprocess execution (confinement unavailable on this host)", security.AuditAllowed},
		{"subproc/allow-edits", sub, domain.ModeAllowEdits, true, true, true, resolveGate, "subprocess execution (confinement unavailable on this host)", security.AuditAllowed},
		{"subproc/auto-confine-caps-suff", sub, domain.ModeAuto, true, true, true, resolveConfine, "", security.AuditAllowed},
		{"subproc/auto-confine-caps-insuff", sub, domain.ModeAuto, true, false, true, resolveGate, "subprocess execution (confinement unavailable on this host)", security.AuditAllowed},
		{"subproc/auto-noconfine", sub, domain.ModeAuto, false, true, true, resolveRun, "", security.AuditAllowed},
		{"subproc/unknown-mode", sub, badMode, true, false, true, resolveGate, "subprocess execution (confinement unavailable on this host)", security.AuditAllowed},

		// native network — auto-runs url-filtered in Auto; gates on the lower rungs.
		{"net/plan", net, domain.ModePlan, true, true, true, resolveRefuse, planRefusalReason, ""},
		{"net/ask-before", net, domain.ModeAskBefore, true, true, true, resolveGate, "network reach", security.AuditAllowed},
		{"net/allow-edits", net, domain.ModeAllowEdits, true, true, true, resolveGate, "network reach", security.AuditAllowed},
		{"net/auto-confine", net, domain.ModeAuto, true, true, true, resolveRun, "", security.AuditAllowed},
		{"net/auto-noconfine", net, domain.ModeAuto, false, true, true, resolveRun, "", security.AuditAllowed},
		{"net/unknown-mode", net, badMode, true, true, true, resolveGate, "network reach", security.AuditAllowed},

		// MCP — gates in Auto (unfenceable server), gates on the lower rungs.
		{"mcp/plan", mcp, domain.ModePlan, true, true, true, resolveRefuse, planRefusalReason, ""},
		{"mcp/ask-before", mcp, domain.ModeAskBefore, true, true, true, resolveGate, "unconfinable MCP tool", security.AuditAllowed},
		{"mcp/allow-edits", mcp, domain.ModeAllowEdits, true, true, true, resolveGate, "unconfinable MCP tool", security.AuditAllowed},
		{"mcp/auto-confine", mcp, domain.ModeAuto, true, true, true, resolveGate, "unconfinable MCP tool", security.AuditAllowed},
		{"mcp/auto-noconfine", mcp, domain.ModeAuto, false, true, true, resolveRun, "", security.AuditAllowed},
		{"mcp/unknown-mode", mcp, badMode, true, true, true, resolveGate, "unconfinable MCP tool", security.AuditAllowed},

		// third-party in-process writer — gates in every non-Plan mode (Apogee can't vouch for it).
		{"3p/plan", tpw, domain.ModePlan, true, true, true, resolveRefuse, planRefusalReason, ""},
		{"3p/ask-before", tpw, domain.ModeAskBefore, true, true, true, resolveGate, "write", security.AuditAllowed},
		{"3p/allow-edits", tpw, domain.ModeAllowEdits, true, true, true, resolveGate, "write", security.AuditAllowed},
		{"3p/auto-confine", tpw, domain.ModeAuto, true, true, true, resolveGate, "write", security.AuditAllowed},
		{"3p/auto-noconfine", tpw, domain.ModeAuto, false, true, true, resolveRun, "", security.AuditAllowed},
		{"3p/unknown-mode", tpw, badMode, true, true, true, resolveGate, "write", security.AuditAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := resolutionInput{
				mode:                   tc.mode,
				call:                   domain.ToolCall{ID: "c1", Tool: tc.tool.Name()},
				tool:                   tc.tool,
				guard:                  proceed,
				confineToWorkspace:     tc.confine,
				fsConfineAvailable:     tc.fsConfine,
				writeTargetInWorkspace: tc.writeIn,
				approverPresent:        true,
				box:                    domain.ConfinementBox{WorkspaceRoot: ws},
			}
			got := resolve(in)

			if got.kind != tc.wantKind {
				t.Fatalf("kind = %s, want %s", got.kind, tc.wantKind)
			}
			if tc.wantReason != "" && got.reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.reason, tc.wantReason)
			}
			if got.force {
				t.Errorf("force = true; no ladder verdict is forced without a Tier-2 guard")
			}
			if got.auditDecision != tc.wantAudit {
				t.Errorf("auditDecision = %q, want %q", got.auditDecision, tc.wantAudit)
			}

			switch got.kind {
			case resolveGate:
				if got.cacheKey != tc.tool.Name() {
					t.Errorf("gate cacheKey = %q, want the tool name %q", got.cacheKey, tc.tool.Name())
				}
				if got.fallback != nil {
					t.Errorf("a Gate must carry no fallback (fallback is Confine-only)")
				}
			case resolveConfine:
				if got.box.WorkspaceRoot != ws {
					t.Errorf("confine box WorkspaceRoot = %q, want %q", got.box.WorkspaceRoot, ws)
				}
				assertConfineFallback(t, got, true /* approverPresent */)
			}
		})
	}
}

// assertConfineFallback pins D4: every Confine carries exactly one bounded runtime-demote
// fallback — a FORCED gate (re-run unconfined on allow) when an Approver is present, a Refuse
// otherwise — and the fallback never nests (its own fallback is nil).
func assertConfineFallback(t *testing.T, got resolution, approverPresent bool) {
	t.Helper()
	if got.fallback == nil {
		t.Fatal("Confine verdict carries no fallback (D4 requires the runtime-demote contingency)")
	}
	fb := got.fallback
	if fb.fallback != nil {
		t.Error("fallback nests another fallback; the demote must be a single bounded step")
	}
	if approverPresent {
		if fb.kind != resolveGate {
			t.Fatalf("fallback kind = %s, want gate (Approver present)", fb.kind)
		}
		if !fb.force {
			t.Error("the runtime-demote fallback gate must be forced (not pre-allowable)")
		}
		if fb.reason != confineDemoteGateReason {
			t.Errorf("fallback reason = %q, want %q", fb.reason, confineDemoteGateReason)
		}
		return
	}
	if fb.kind != resolveRefuse {
		t.Fatalf("fallback kind = %s, want refuse (no Approver)", fb.kind)
	}
	if fb.reason != confineDemoteRefuseReason {
		t.Errorf("fallback reason = %q, want %q", fb.reason, confineDemoteRefuseReason)
	}
}

// ----------------------------------------------------------------------------
// Overlay: the tighten-only guardrail floor (Tier-1 refuse, Tier-2 force)
// ----------------------------------------------------------------------------

// TestResolve_GuardTier1Refuses proves a guard hard-refuse (Tier-1 dangerous action or a
// tripped circuit-breaker) refuses in EVERY mode and for every class — before the ladder —
// carrying the guard's rendered reason and pass-through audit decision.
func TestResolve_GuardTier1Refuses(t *testing.T) {
	t.Parallel()

	guards := []struct {
		name       string
		guard      security.PreCheck
		wantReason string
		wantAudit  security.AuditDecision
	}{
		{
			"tier-1 dangerous",
			security.PreCheck{Outcome: security.GuardRefuse, Reason: "rm -rf /", Audit: security.AuditDangerousRefused},
			"refused by the dangerous-action guard: rm -rf /",
			security.AuditDangerousRefused,
		},
		{
			"circuit tripped",
			security.PreCheck{Outcome: security.GuardRefuse, Reason: "identical failing call", Audit: security.AuditCircuitTripped},
			"circuit-breaker open: this tool call has failed repeatedly with identical arguments and is refused",
			security.AuditCircuitTripped,
		},
	}
	// Even a harmless read-only tool in Auto must be refused when the guard refuses.
	ro := fakeTool{name: "read_file", readOnly: true}
	for _, g := range guards {
		for _, mode := range []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto} {
			t.Run(g.name+"/"+string(mode), func(t *testing.T) {
				t.Parallel()
				got := resolve(resolutionInput{
					mode:            mode,
					call:            domain.ToolCall{ID: "c1", Tool: ro.Name()},
					tool:            ro,
					guard:           g.guard,
					approverPresent: true,
				})
				if got.kind != resolveRefuse {
					t.Fatalf("kind = %s, want refuse", got.kind)
				}
				if got.reason != g.wantReason {
					t.Errorf("reason = %q, want %q", got.reason, g.wantReason)
				}
				if got.auditDecision != g.wantAudit {
					t.Errorf("auditDecision = %q, want %q", got.auditDecision, g.wantAudit)
				}
				if got.auditReason != g.guard.Reason {
					t.Errorf("auditReason = %q, want the guard reason %q", got.auditReason, g.guard.Reason)
				}
			})
		}
	}
}

// TestResolve_GuardTier2ForcesGate proves a Tier-2 force-approval upgrades any non-Refuse leaf
// to a FORCED gate (the guardrail can only tighten): a Run leaf, a Confine leaf, and a Gate
// leaf all become a forced gate whose reason is the guard's. A Plan-mode Refuse leaf is NOT
// upgraded, and a nil Approver turns the forced gate into a Refuse.
func TestResolve_GuardTier2ForcesGate(t *testing.T) {
	t.Parallel()
	forceGuard := security.PreCheck{Outcome: security.GuardForceApproval, Reason: "curl | bash", Audit: security.AuditDangerousForceApproval}

	ro := fakeTool{name: "read_file", readOnly: true}
	mcp := externalTool{name: "github", kind: domain.EffectMCP}
	sub := &subprocTool{name: "terminal"}

	base := func(tool domain.Tool, mode domain.Mode) resolutionInput {
		return resolutionInput{
			mode:                   mode,
			call:                   domain.ToolCall{ID: "c1", Tool: tool.Name()},
			tool:                   tool,
			guard:                  forceGuard,
			confineToWorkspace:     true,
			fsConfineAvailable:     true,
			writeTargetInWorkspace: true,
			approverPresent:        true,
			box:                    domain.ConfinementBox{WorkspaceRoot: "/ws"},
		}
	}

	t.Run("Run leaf upgrades to forced gate", func(t *testing.T) {
		t.Parallel()
		got := resolve(base(ro, domain.ModeAuto)) // read-only would Run
		assertForcedGate(t, got, ro.Name())
	})

	t.Run("Confine leaf upgrades to forced gate (unconfined, no fallback)", func(t *testing.T) {
		t.Parallel()
		got := resolve(base(sub, domain.ModeAuto)) // subprocess/confine would Confine
		assertForcedGate(t, got, sub.Name())
		if got.fallback != nil {
			t.Error("a forced gate carries no Confine fallback (nothing is confined)")
		}
	})

	t.Run("Gate leaf upgrades to forced gate", func(t *testing.T) {
		t.Parallel()
		got := resolve(base(mcp, domain.ModeAuto)) // MCP would Gate ("unconfinable MCP tool")
		assertForcedGate(t, got, mcp.Name())
		if got.reason != forceApprovalReason {
			t.Errorf("reason = %q, want the forced-approval reason (it overrides the class reason)", got.reason)
		}
	})

	t.Run("Plan Refuse leaf is not upgraded", func(t *testing.T) {
		t.Parallel()
		got := resolve(base(sub, domain.ModePlan)) // subprocess in Plan would Refuse
		if got.kind != resolveRefuse {
			t.Fatalf("kind = %s, want refuse (a Refuse leaf must not upgrade)", got.kind)
		}
		if got.reason != planRefusalReason {
			t.Errorf("reason = %q, want %q", got.reason, planRefusalReason)
		}
		if got.auditDecision != "" {
			t.Errorf("auditDecision = %q, want empty (a Plan-mode write refusal is not audited)", got.auditDecision)
		}
	})

	t.Run("forced gate with nil Approver refuses", func(t *testing.T) {
		t.Parallel()
		in := base(ro, domain.ModeAuto)
		in.approverPresent = false
		got := resolve(in)
		if got.kind != resolveRefuse {
			t.Fatalf("kind = %s, want refuse (forced gate + nil Approver)", got.kind)
		}
		if got.reason != noApproverReason {
			t.Errorf("reason = %q, want %q", got.reason, noApproverReason)
		}
		if got.auditDecision != security.AuditDangerousForceApproval {
			t.Errorf("auditDecision = %q, want the pass-through Tier-2 decision", got.auditDecision)
		}
	})
}

// assertForcedGate pins the shape of a Tier-2 forced gate: a gate with force set, the
// forced-approval reason, the tool-name cache key, and the pass-through Tier-2 audit decision.
func assertForcedGate(t *testing.T, got resolution, toolName string) {
	t.Helper()
	if got.kind != resolveGate {
		t.Fatalf("kind = %s, want gate", got.kind)
	}
	if !got.force {
		t.Error("force = false; a Tier-2 gate must skip the allow-for-session cache")
	}
	if got.reason != forceApprovalReason {
		t.Errorf("reason = %q, want %q", got.reason, forceApprovalReason)
	}
	if got.cacheKey != toolName {
		t.Errorf("cacheKey = %q, want the tool name %q", got.cacheKey, toolName)
	}
	if got.auditDecision != security.AuditDangerousForceApproval {
		t.Errorf("auditDecision = %q, want the pass-through Tier-2 decision", got.auditDecision)
	}
}

// ----------------------------------------------------------------------------
// The sub_agent recursion point (Delegate; D3 / ADR 0013)
// ----------------------------------------------------------------------------

// TestResolve_SubAgentDelegate proves the sub_agent call resolves to Delegate (not a leaf
// tool), that a Tier-2 force-approval is DELIBERATELY ignored for a Delegate (D3), that the
// depth bound refuses defensively, and that a Tier-1 guard still refuses the delegation itself.
func TestResolve_SubAgentDelegate(t *testing.T) {
	t.Parallel()
	subCall := domain.ToolCall{ID: "c1", Tool: tools.SubAgentToolName}
	// resolve() reaches Delegate before it touches in.tool, so the tool object is irrelevant.
	subTool := fakeTool{name: tools.SubAgentToolName}

	t.Run("delegates under a cleared guard", func(t *testing.T) {
		t.Parallel()
		got := resolve(resolutionInput{
			mode: domain.ModeAuto, call: subCall, tool: subTool, guard: proceed, approverPresent: true,
		})
		if got.kind != resolveDelegate {
			t.Fatalf("kind = %s, want delegate", got.kind)
		}
		if got.auditDecision != security.AuditAllowed {
			t.Errorf("auditDecision = %q, want allowed", got.auditDecision)
		}
	})

	t.Run("Tier-2 force-approval does NOT gate a Delegate (D3)", func(t *testing.T) {
		t.Parallel()
		got := resolve(resolutionInput{
			mode:            domain.ModeAuto,
			call:            subCall,
			tool:            subTool,
			guard:           security.PreCheck{Outcome: security.GuardForceApproval, Reason: "x", Audit: security.AuditDangerousForceApproval},
			approverPresent: true,
		})
		if got.kind != resolveDelegate {
			t.Fatalf("kind = %s, want delegate (Tier-2 must not gate delegation)", got.kind)
		}
		if got.force {
			t.Error("a Delegate is never forced — nothing executes at the delegation point")
		}
		if got.auditDecision != security.AuditDangerousForceApproval {
			t.Errorf("auditDecision = %q, want the pass-through Tier-2 decision", got.auditDecision)
		}
	})

	t.Run("depth bound refuses defensively", func(t *testing.T) {
		t.Parallel()
		got := resolve(resolutionInput{
			mode: domain.ModeAuto, call: subCall, tool: subTool, guard: proceed, approverPresent: true, atDepthBound: true,
		})
		if got.kind != resolveRefuse {
			t.Fatalf("kind = %s, want refuse at the depth bound", got.kind)
		}
		if got.reason == "" {
			t.Error("depth-bound refusal must carry a reason")
		}
	})

	t.Run("Tier-1 guard refuses the delegation itself", func(t *testing.T) {
		t.Parallel()
		got := resolve(resolutionInput{
			mode:            domain.ModeAuto,
			call:            subCall,
			tool:            subTool,
			guard:           security.PreCheck{Outcome: security.GuardRefuse, Reason: "dangerous task", Audit: security.AuditDangerousRefused},
			approverPresent: true,
		})
		if got.kind != resolveRefuse {
			t.Fatalf("kind = %s, want refuse (a Tier-1 guard refuses the sub_agent call)", got.kind)
		}
		if got.auditDecision != security.AuditDangerousRefused {
			t.Errorf("auditDecision = %q, want dangerous-refused", got.auditDecision)
		}
	})
}

// ----------------------------------------------------------------------------
// Unknown tool, nil-Approver, and the Confine fallback shapes
// ----------------------------------------------------------------------------

// TestResolve_UnknownTool proves an unknown tool (nil resolved tool) refuses with the exact
// model-facing message and — matching today's quirk — carries NO audit decision. A guard
// hard-refuse on the same call still wins (ordering: guard before the unknown-tool check).
func TestResolve_UnknownTool(t *testing.T) {
	t.Parallel()

	t.Run("nil tool refuses, not audited", func(t *testing.T) {
		t.Parallel()
		got := resolve(resolutionInput{
			mode:            domain.ModeAuto,
			call:            domain.ToolCall{ID: "c1", Tool: "mystery"},
			tool:            nil,
			guard:           proceed,
			approverPresent: true,
		})
		if got.kind != resolveRefuse {
			t.Fatalf("kind = %s, want refuse", got.kind)
		}
		if got.reason != `unknown tool "mystery"` {
			t.Errorf("reason = %q, want %q", got.reason, `unknown tool "mystery"`)
		}
		if got.auditDecision != "" {
			t.Errorf("auditDecision = %q, want empty (an unknown-tool refusal is not audited)", got.auditDecision)
		}
	})

	t.Run("a guard refuse wins over the unknown-tool check", func(t *testing.T) {
		t.Parallel()
		got := resolve(resolutionInput{
			mode:            domain.ModeAuto,
			call:            domain.ToolCall{ID: "c1", Tool: "mystery"},
			tool:            nil,
			guard:           security.PreCheck{Outcome: security.GuardRefuse, Reason: "rm -rf /", Audit: security.AuditDangerousRefused},
			approverPresent: true,
		})
		if got.kind != resolveRefuse {
			t.Fatalf("kind = %s, want refuse", got.kind)
		}
		if got.auditDecision != security.AuditDangerousRefused {
			t.Errorf("auditDecision = %q, want the guard's decision (a guard refuse IS audited)", got.auditDecision)
		}
	})
}

// TestResolve_NilApproverGateRefuses proves every gate-producing verdict becomes a Refuse when
// no Approver is configured (D5): a Gate always means the Approver is actually consulted, so a
// nil Approver refuses rather than run unapproved — and that refusal IS audited (pass-through).
func TestResolve_NilApproverGateRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		tool      domain.Tool
		mode      domain.Mode
		confine   bool
		fsConfine bool
		writeIn   bool
	}{
		{"ask-before write", tools.NewWriteFile(t.TempDir()), domain.ModeAskBefore, true, true, true},
		{"auto mcp", externalTool{name: "github", kind: domain.EffectMCP}, domain.ModeAuto, true, true, true},
		{"auto out-of-workspace write", tools.NewWriteFile(t.TempDir()), domain.ModeAuto, true, true, false},
		{"auto subproc caps-insufficient", &subprocTool{name: "terminal"}, domain.ModeAuto, true, false, true},
		{"auto third-party write", thirdPartyWriter{name: "weird"}, domain.ModeAuto, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolve(resolutionInput{
				mode:                   tc.mode,
				call:                   domain.ToolCall{ID: "c1", Tool: tc.tool.Name()},
				tool:                   tc.tool,
				guard:                  proceed,
				confineToWorkspace:     tc.confine,
				fsConfineAvailable:     tc.fsConfine,
				writeTargetInWorkspace: tc.writeIn,
				approverPresent:        false,
			})
			if got.kind != resolveRefuse {
				t.Fatalf("kind = %s, want refuse (gate + nil Approver)", got.kind)
			}
			if got.reason != noApproverReason {
				t.Errorf("reason = %q, want %q", got.reason, noApproverReason)
			}
			if got.auditDecision != security.AuditAllowed {
				t.Errorf("auditDecision = %q, want the pass-through decision (a nil-Approver refusal is audited)", got.auditDecision)
			}
		})
	}
}

// TestResolve_ConfineFallbackShape pins D4 directly on the Confine verdict: the primary verdict
// is still Confine whether or not an Approver is present (a Confine runs without Approval), but
// its precomputed fallback is a forced gate with an Approver and a Refuse without one — and it
// never nests.
func TestResolve_ConfineFallbackShape(t *testing.T) {
	t.Parallel()
	sub := &subprocTool{name: "terminal"}
	base := func(approver bool) resolutionInput {
		return resolutionInput{
			mode:               domain.ModeAuto,
			call:               domain.ToolCall{ID: "c1", Tool: sub.Name()},
			tool:               sub,
			guard:              proceed,
			confineToWorkspace: true,
			fsConfineAvailable: true,
			approverPresent:    approver,
			box:                domain.ConfinementBox{WorkspaceRoot: "/ws"},
		}
	}

	t.Run("Approver present → forced-gate fallback", func(t *testing.T) {
		t.Parallel()
		got := resolve(base(true))
		if got.kind != resolveConfine {
			t.Fatalf("kind = %s, want confine", got.kind)
		}
		if got.box.WorkspaceRoot != "/ws" {
			t.Errorf("confine box not carried: %+v", got.box)
		}
		assertConfineFallback(t, got, true)
	})

	t.Run("no Approver → refuse fallback, still Confine primary", func(t *testing.T) {
		t.Parallel()
		got := resolve(base(false))
		if got.kind != resolveConfine {
			t.Fatalf("kind = %s, want confine (a Confine runs without Approval)", got.kind)
		}
		assertConfineFallback(t, got, false)
	})
}
