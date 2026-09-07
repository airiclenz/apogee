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
// diagnostics and then refused the call (contract §4 fn 2). Both now key on
// planAdmits. These tests pin the agreement over the WHOLE registry rather than over the tools
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
// Together they span all eight toolClass values — the RO-subproc one is occupied by the
// shipped git read trio, whose marker is unexported and therefore unfakeable here.
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
// it. Offered-but-refused is the drift that shipped (diagnostics); refused-but-
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

// TestPlanToolMenuDropsDiagnosticsAndTheUnmarkedFakes names what the resolution explicitly
// moves off the Plan menu: diagnostics declares ReadOnly() and launches an OS subprocess Apogee
// cannot vouch for, and so do the two host-registered fakes, so their class is subproc / 3p-net
// and Plan neither offers nor runs them. The named assertion sits beside the property test
// because this IS the documented drift (contract §4 fn 2) — a regression here should read as
// itself, not as one row of a table.
func TestPlanToolMenuDropsDiagnosticsAndTheUnmarkedFakes(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	toolset := planMenuTools(ws)
	offered := offeredNames(planMenuAgent(t, domain.ModePlan, toolset).toolMenu())

	for _, name := range []string{"diagnostics", "ro_declared_subproc", "ro_declared_net"} {
		if offered[name] {
			t.Errorf("Plan menu offers %s; it declares ReadOnly() but carries a marker, so the ladder refuses it", name)
		}
	}
	// The floor is intact: the tools no marker claims are still there, and so is the recursion
	// point (a Plan sub-agent inherits Plan).
	for _, name := range []string{"read_file", "list_dir", "grep", "view_diff", "ask_user", "present_document", tools.SubAgentToolName} {
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

// TestPlanToolMenuOffersTheGitReadTrio is the other half of the same named assertion: the three
// hardened git read tools ARE on the Plan menu, by exact name, because they carry the
// readOnlySubprocess marker and the Plan ladder runs them (contract §4 amendment 2026-09-06).
// The write-side git tools and the two other subprocess surfaces stay off it, so the assertion
// pins the boundary rather than just the addition — a marker minted one tool too widely fails
// here by name.
func TestPlanToolMenuOffersTheGitReadTrio(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	toolset := planMenuTools(ws)
	offered := offeredNames(planMenuAgent(t, domain.ModePlan, toolset).toolMenu())

	for _, name := range []string{"git_status", "git_log", "git_diff_range"} {
		if !offered[name] {
			t.Errorf("Plan menu is missing %s; it is read-only by construction and Plan runs it", name)
		}
	}
	for _, name := range []string{"diagnostics", "git_branch", "git_commit", "terminal"} {
		if offered[name] {
			t.Errorf("Plan menu offers %s; only the hardened git READ trio crosses into Plan", name)
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

// TestPlanAdmitsTheReadOnlyHalfOfTheConsoleFamily pins the split ADR 0059 §2 draws through the
// Console family: console_read and console_close take the read-only floor, so Plan admits them
// with no engine code of their own — a model planning can watch, and stop, a program an earlier
// mode left running — while console_open and console_send carry the Subprocess marker and Plan
// refuses them however read-only they might one day declare themselves.
//
// It asserts planAdmits directly rather than through the registry, because the family is
// registered default-off: the whole-registry agreement test above never sees it.
func TestPlanAdmitsTheReadOnlyHalfOfTheConsoleFamily(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	tests := []struct {
		tool     domain.Tool
		admitted bool
	}{
		{tools.NewConsoleRead(), true},
		{tools.NewConsoleClose(), true},
		{tools.NewConsoleOpen(ws, nil), false},
		{tools.NewConsoleSend(), false},
	}
	for _, tt := range tests {
		t.Run(tt.tool.Name(), func(t *testing.T) {
			t.Parallel()
			if got := planAdmits(tt.tool); got != tt.admitted {
				t.Errorf("planAdmits(%s) = %v, want %v", tt.tool.Name(), got, tt.admitted)
			}
		})
	}
}
