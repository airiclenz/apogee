package agent

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// One tool classification: the Plan menu and the resolution ladder agree
// ----------------------------------------------------------------------------
//
// The Plan tool menu (loop.go's toolMenu) and the Plan row of the autonomy ladder
// (resolution.go's resolveLadder) once keyed on DIFFERENT facts — the menu on the bare
// ReadOnly() self-declaration, the ladder on the blast-radius class — so Plan offered
// git_diff_range and diagnostics and then refused the call (contract §4 fn 2). Both now key on
// planAdmits. These tests pin the agreement over the WHOLE registry rather than over the pair
// that drifted, so a future tool cannot re-open the gap: a new class, or a new built-in that
// declares itself read-only while carrying a marker, fails here the moment it is registered.

// stubAsker / stubPresenter are the host delegates the default registry needs before it
// registers ask_user and present_document. They are never called: these tests build the menu
// and resolve verdicts, they execute nothing.
type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

type stubPresenter struct{}

func (stubPresenter) Present(context.Context, domain.PresentRequest) (domain.PresentOutcome, error) {
	return domain.PresentOutcome{}, nil
}

// planMenuTools is the registry the agreement table runs over: every shipped built-in (both
// host delegates supplied, so ask_user and present_document are in it too) plus fakes for the
// classes no built-in occupies — a third-party network tool, an MCP tool, a third-party
// in-process writer — and the two RO-declaring-marker-carrying shapes a host could register.
// Together they span all seven toolClass values.
func planMenuTools(ws string) []domain.Tool {
	all := tools.DefaultToolsWithHost(ws, tools.HostTools{
		Asker:     stubAsker{},
		Presenter: stubPresenter{},
	})
	return append(all,
		externalTool{name: "third_party_net", kind: domain.EffectNetwork},
		externalTool{name: "github__create_issue", kind: domain.EffectMCP},
		thirdPartyWriter{name: "third_party_write"},
		// The two shapes the old declaration-based filter mis-offered, as a host could
		// register them: a read-only DECLARATION over an unfakeable marker.
		externalTool{name: "ro_declared_net", kind: domain.EffectNetwork, readOnly: true},
		&subprocTool{name: "ro_declared_subproc", readOnly: true},
	)
}

// planMenuAgent builds an Agent in mode over the given tools. It never runs a Turn — the
// responder is only there because construction requires one.
func planMenuAgent(t *testing.T, mode domain.Mode, toolset []domain.Tool) *Agent {
	t.Helper()
	cfg := configWithTools(&recordingSink{}, toolset...)
	cfg.Mode = mode
	cfg.Approver = &fakeApprover{decision: domain.ApprovalAllow}
	// Auto refuses to construct without fs-write confinement (ADR 0012), so every mode gets a
	// fully-capable fake Confiner — the menu itself never consults it.
	cfg.Confiner = &fakeConfiner{caps: capsBoth()}
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// offeredNames is the set of tool names a menu contains.
func offeredNames(menu []domain.ToolDef) map[string]bool {
	offered := make(map[string]bool, len(menu))
	for _, def := range menu {
		offered[def.Name] = true
	}
	return offered
}

// TestPlanToolMenuAgreesWithTheLadder is the invariant this item exists for: for EVERY
// registered tool, Plan offers it in the menu exactly when the Plan ladder would actually run
// it. Offered-but-refused is the drift that shipped (git_diff_range, diagnostics); refused-but-
// hidden would be the opposite hole — a tool Plan can run that the model is never shown.
//
// The verdict side calls the real resolve() rather than re-deriving anything, so the two sides
// of the assertion cannot drift together.
func TestPlanToolMenuAgreesWithTheLadder(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	toolset := planMenuTools(ws)
	offered := offeredNames(planMenuAgent(t, domain.ModePlan, toolset).toolMenu())

	for _, tool := range toolset {
		t.Run(tool.Name(), func(t *testing.T) {
			t.Parallel()
			got := resolve(resolutionInput{
				mode:                   domain.ModePlan,
				call:                   domain.ToolCall{ID: "c1", Tool: tool.Name()},
				tool:                   tool,
				guard:                  proceed,
				confineToWorkspace:     true,
				fsConfineAvailable:     true,
				writeTargetInWorkspace: true,
				approverPresent:        true,
				box:                    domain.ConfinementBox{WorkspaceRoot: ws},
			})
			// Anything but a refusal is a call Plan carries out — sub_agent Delegates (D3/ADR
			// 0013), a read-only-classed leaf Runs.
			runnable := got.kind != resolveRefuse
			if offered[tool.Name()] != runnable {
				t.Errorf("Plan menu offers %s = %t, but the ladder resolves it to %s (want the menu to follow the ladder)",
					tool.Name(), offered[tool.Name()], got.kind)
			}
		})
	}
}

// TestPlanToolMenuDropsTheDriftedPair names the two shipped built-ins the resolution explicitly
// moves: git_diff_range and diagnostics declare ReadOnly() and launch an OS subprocess, so their
// class is subproc and Plan neither offers nor runs them. The named assertion sits beside the
// property test because the pair IS the documented drift (contract §4 fn 2) — a regression here
// should read as itself, not as one row of a table.
func TestPlanToolMenuDropsTheDriftedPair(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	toolset := planMenuTools(ws)
	offered := offeredNames(planMenuAgent(t, domain.ModePlan, toolset).toolMenu())

	for _, name := range []string{"git_diff_range", "diagnostics", "ro_declared_subproc", "ro_declared_net"} {
		if offered[name] {
			t.Errorf("Plan menu offers %s; it declares ReadOnly() but carries a marker, so the ladder refuses it", name)
		}
	}
	// The floor is intact: the tools no marker claims are still there, and so is the recursion
	// point (a Plan sub-agent inherits Plan).
	for _, name := range []string{"read_file", "list_dir", "grep", "view_diff", "open_file", "ask_user", "present_document", tools.SubAgentToolName} {
		if !offered[name] {
			t.Errorf("Plan menu is missing %s; Plan runs it, so the model must be shown it", name)
		}
	}
	// And the write half is still hidden, as it always was.
	for _, name := range []string{"write_file", "edit_existing_file", "terminal", "web_fetch"} {
		if offered[name] {
			t.Errorf("Plan menu offers %s, which Plan refuses", name)
		}
	}
}

// TestToolMenuOutsidePlanIsUnfiltered pins that the change is Plan-only: every other rung still
// shows the model the whole registry, and the gating happens per call at the Approval seam.
func TestToolMenuOutsidePlanIsUnfiltered(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	toolset := planMenuTools(ws)

	for _, mode := range []domain.Mode{domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			menu := planMenuAgent(t, mode, toolset).toolMenu()
			if len(menu) != len(toolset) {
				t.Fatalf("%s menu has %d tools, want all %d", mode, len(menu), len(toolset))
			}
			for i, tool := range toolset {
				if menu[i].Name != tool.Name() {
					t.Errorf("%s menu[%d] = %s, want %s (registration order is load-bearing)", mode, i, menu[i].Name, tool.Name())
				}
			}
		})
	}
}
