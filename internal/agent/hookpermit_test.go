package agent

// The reaction-time subprocess permit (confinement-execution-contract §10). A Reaction runs
// outside the per-call Resolution, so the only authorisation a fire can spawn under is a
// domain.SubprocessPermit on its context — and absence means refusal (§10.2). These tests drive a
// real Turn and read the permit back through a post-response Reaction to pin the row the engine
// no longer has: since 2026-09-15 no cascade installs a permit at any Moment, in any mode, so the
// post-response ctx carries none even under Auto. The one permit the engine mints is the sync
// lane's (syncPermitCtx, exercised by TestSyncArgv* in syncexec_test.go).

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// permitProbe records the SubprocessPermit its fire ctx carried — the observable end of whatever
// the cascade installed ahead of its handlers. A pointer so the capture survives the fire.
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
// plus the three box fields resolutionInput reads, so a granted permit's box would be assertable.
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

// TestHookSubprocessPermitLadder walks every row the retired post-response permit table had —
// every mode, confine-to-workspace on and off, a Confiner with and without filesystem caps, and a
// sub-agent whose parent sits in Plan — and asserts the same thing on each: the ctx a post-response
// handler receives carries NO SubprocessPermit. Auto is the row that used to grant one; it grants
// nothing now, because no shipped Reaction spawns at that Moment and the refusal default is the
// only posture a permit-less seam can have (§10.2).
func TestHookSubprocessPermitLadder(t *testing.T) {
	t.Parallel()

	capable := func() *fakeConfiner { return &fakeConfiner{caps: capsBoth()} }
	incapable := func() *fakeConfiner { return &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: false}} }
	parentInPlan := func() domain.Mode { return domain.ModePlan }

	tests := []struct {
		name    string
		mode    domain.Mode
		conf    *fakeConfiner
		confine bool
		tighten func() domain.Mode
	}{
		{name: "plan", mode: domain.ModePlan, conf: capable(), confine: true},
		{name: "ask-before", mode: domain.ModeAskBefore, conf: capable(), confine: true},
		{name: "allow-edits", mode: domain.ModeAllowEdits, conf: capable(), confine: true},
		{name: "auto with confine off", mode: domain.ModeAuto, conf: capable(), confine: false},
		{name: "auto with confine on", mode: domain.ModeAuto, conf: capable(), confine: true},
		{name: "auto with confine on and no fs caps", mode: domain.ModeAuto, conf: incapable(), confine: true},
		{name: "auto under a plan-mode parent", mode: domain.ModeAuto, conf: capable(), confine: true, tighten: parentInPlan},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			probe := runTurnWithPermitProbe(t, permitConfig(tc.mode, tc.conf, tc.confine), tc.tighten)

			if probe.granted {
				t.Fatalf("post-response handler was granted %+v, want no permit in any mode", probe.permit)
			}
		})
	}
}
