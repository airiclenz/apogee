package agent

// The reaction-time subprocess permit (confinement-execution-contract §10). A Reaction runs
// outside the per-call Resolution, so the ladder's answer to "may this fire spawn a process?"
// reaches it as a domain.SubprocessPermit on the context. These tests drive a real Turn and read
// the permit back through a post-response Reaction, which is the only Moment the engine installs
// one for.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// permitProbe records the SubprocessPermit its fire ctx carried — the observable end of
// hookExecutionCtx. A pointer so the capture survives the fire.
type permitProbe struct {
	fired   bool
	granted bool
	permit  domain.SubprocessPermit
}

// reaction arms the probe as a post-response Reaction on Config.Reactions.
func (p *permitProbe) reaction() domain.Reaction {
	return domain.Reaction{
		ID:     "permit_probe",
		Origin: domain.OriginEngine,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPostResponse},
		Handler: domain.PostResponseFunc(func(ctx context.Context, _ *domain.Response) (domain.Outcome, error) {
			p.fired = true
			p.permit, p.granted = domain.SubprocessPermitFromContext(ctx)
			return domain.Outcome{}, nil
		}),
	}
}

// permitConfig builds a Config in mode with the given fake Confiner and confine-to-workspace flag,
// plus the three box fields resolutionInput reads, so a granted permit's box is assertable.
func permitConfig(mode domain.Mode, conf domain.Confiner, confine bool) domain.Config {
	cfg := baseConfig(&recordingSink{})
	cfg.Mode = mode
	cfg.Confiner = conf
	cfg.ConfineToWorkspace = confine
	cfg.WorkspaceDir = "/work/space"
	cfg.ConfineWritablePaths = []string{"/work/space/out"}
	cfg.ConfineNetworkAllow = []string{"example.test"}
	return cfg
}

// runTurnWithPermitProbe drives one full Turn with a post-response probe registered, optionally
// under a parent-mode view (tighten != nil makes the Agent behave as a sub-agent), and returns the
// probe once it has fired.
func runTurnWithPermitProbe(t *testing.T, cfg domain.Config, tighten func() domain.Mode) *permitProbe {
	t.Helper()

	probe := &permitProbe{}
	cfg.Reactions = []domain.Reaction{probe.reaction()}

	a, err := newAgent(cfg, echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.liveMode = tighten
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if !probe.fired {
		t.Fatal("the post-response probe never fired; the permit assertion would be vacuous")
	}
	return probe
}

// TestHookSubprocessPermitLadder walks every row of hookExecutionCtx's table: only Auto grants a
// permit at all, confine-to-workspace decides whether it carries a box, and a Confiner that cannot
// enforce filesystem confinement gates the hook-time subprocess surface exactly as it gates a
// subprocess tool's.
func TestHookSubprocessPermitLadder(t *testing.T) {
	t.Parallel()

	capable := func() *fakeConfiner { return &fakeConfiner{caps: capsBoth()} }
	incapable := func() *fakeConfiner { return &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: false}} }

	tests := []struct {
		name        string
		mode        domain.Mode
		conf        *fakeConfiner
		confine     bool
		wantGranted bool
		wantBox     bool
	}{
		{name: "plan grants nothing", mode: domain.ModePlan, conf: capable(), confine: true},
		{name: "ask-before grants nothing", mode: domain.ModeAskBefore, conf: capable(), confine: true},
		{name: "allow-edits grants nothing", mode: domain.ModeAllowEdits, conf: capable(), confine: true},
		{
			name: "auto with confine off grants an unfenced permit", mode: domain.ModeAuto,
			conf: capable(), confine: false, wantGranted: true,
		},
		{
			name: "auto with confine on grants a confined permit", mode: domain.ModeAuto,
			conf: capable(), confine: true, wantGranted: true, wantBox: true,
		},
		{
			name: "auto with confine on and no fs caps grants nothing", mode: domain.ModeAuto,
			conf: incapable(), confine: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			probe := runTurnWithPermitProbe(t, permitConfig(tc.mode, tc.conf, tc.confine), nil)

			if probe.granted != tc.wantGranted {
				t.Fatalf("permit granted = %v, want %v", probe.granted, tc.wantGranted)
			}
			if !tc.wantGranted {
				return
			}
			if !tc.wantBox {
				if probe.permit.Confinement != nil {
					t.Fatalf("permit carried Confinement %+v, want nil (unfenced)", probe.permit.Confinement)
				}
				return
			}
			assertPermitBox(t, probe.permit, tc.conf)
		})
	}
}

// assertPermitBox checks a granted permit carries the injected Confiner and the box built from the
// Config's three confinement fields.
func assertPermitBox(t *testing.T, permit domain.SubprocessPermit, conf domain.Confiner) {
	t.Helper()

	if permit.Confinement == nil {
		t.Fatal("permit carried no Confinement, want the workspace box")
	}
	if permit.Confinement.Confiner != conf {
		t.Errorf("permit Confiner = %v, want the injected fake", permit.Confinement.Confiner)
	}
	box := permit.Confinement.Box
	if box.WorkspaceRoot != "/work/space" {
		t.Errorf("box.WorkspaceRoot = %q, want %q", box.WorkspaceRoot, "/work/space")
	}
	if len(box.WritablePaths) != 1 || box.WritablePaths[0] != "/work/space/out" {
		t.Errorf("box.WritablePaths = %v, want [/work/space/out]", box.WritablePaths)
	}
	if len(box.NetworkAllow) != 1 || box.NetworkAllow[0] != "example.test" {
		t.Errorf("box.NetworkAllow = %v, want [example.test]", box.NetworkAllow)
	}
}

// TestHookSubprocessPermitReadsEffectiveMode proves the gate composes with the parent's mode
// (ADR 0013): a sub-agent spawned into Auto whose parent has since tightened to Plan gets NO
// permit, so a hook cannot outlive the tightening the tool ladder already honours.
func TestHookSubprocessPermitReadsEffectiveMode(t *testing.T) {
	t.Parallel()

	cfg := permitConfig(domain.ModeAuto, &fakeConfiner{caps: capsBoth()}, true)
	parentInPlan := func() domain.Mode { return domain.ModePlan }

	probe := runTurnWithPermitProbe(t, cfg, parentInPlan)

	if probe.granted {
		t.Errorf("a sub-agent under a Plan-mode parent was granted %+v, want no permit", probe.permit)
	}
}
