package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// TestDelegationOffer_ZeroAgentAndNilRosterOfferNothing pins the safe answer: an Agent with no
// roster — the zero Agent, or one whose roster holds neither delegation tool, or a foreign tool
// under either name — offers nothing, so no seat is read, no `context` file is opened and no call
// runs in the background.
func TestDelegationOffer_ZeroAgentAndNilRosterOfferNothing(t *testing.T) {
	t.Parallel()

	foreign := domain.NewToolRegistry()
	_ = foreign.Register(fakeTool{name: tools.SubAgentToolName})
	_ = foreign.Register(fakeTool{name: tools.FanOutToolName})

	for _, tc := range []struct {
		name  string
		agent *Agent
	}{
		{name: "the zero Agent", agent: &Agent{}},
		{name: "a nil roster with a scratch dir and workspace", agent: topLevelAgent(nil, "/w")},
		{name: "an empty roster", agent: topLevelAgent(domain.NewToolRegistry(), "/w")},
		{name: "foreign tools under both names", agent: topLevelAgent(foreign, "/w")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.agent.delegationOffer(); got != (delegationOffer{}) {
				t.Errorf("delegationOffer() = %+v, want the zero offer", got)
			}
		})
	}
}

// TestDelegationOffer_JoinsThePublishedOfferWithTheLiveFacts pins each field against the roster's
// published arguments and the live facts beside them: a delegate keeps the seat and context it was
// offered but never a background, and a sub_agent background needs a scratch dir and a workspace
// where fan_out's does not.
func TestDelegationOffer_JoinsThePublishedOfferWithTheLiveFacts(t *testing.T) {
	t.Parallel()

	everything := domain.NewToolRegistry()
	_ = everything.Register(tools.NewSubAgentWith(tools.SubAgentOptions{SeatChoice: true, Background: true, Context: true}))
	_ = everything.Register(tools.NewFanOutWith(tools.FanOutOptions{SeatChoice: true, Background: true}))

	delegate := topLevelAgent(everything, "/w")
	delegate.depth = 1
	noScratch := topLevelAgent(everything, "/w")
	noScratch.SetScratchDir("")

	for _, tc := range []struct {
		name  string
		agent *Agent
		want  delegationOffer
	}{
		{
			name:  "the top-level Agent with a scratch dir and workspace",
			agent: topLevelAgent(everything, "/w"),
			want:  delegationOffer{seatChoice: true, context: true, background: true, fanOutSeatChoice: true, fanOutBackground: true},
		},
		{
			name:  "a delegate",
			agent: delegate,
			want:  delegationOffer{seatChoice: true, context: true, fanOutSeatChoice: true},
		},
		{
			name:  "no scratch dir",
			agent: noScratch,
			want:  delegationOffer{seatChoice: true, context: true, fanOutSeatChoice: true, fanOutBackground: true},
		},
		{
			name:  "no workspace",
			agent: topLevelAgent(everything, ""),
			want:  delegationOffer{seatChoice: true, context: true, fanOutSeatChoice: true, fanOutBackground: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.agent.delegationOffer(); got != tc.want {
				t.Errorf("delegationOffer() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestDelegationOffer_LiveFactsGateBackground pins the sub_agent background gate on a real Agent
// (ADR 0094 D1, D7): only the top-level Agent of a session that can keep the workflow — a scratch
// dir and a workspace — whose sub_agent published the switch may run a call in the background.
func TestDelegationOffer_LiveFactsGateBackground(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		shape func(*domain.Config)
		depth int
		want  bool
	}{
		{"the switch published", func(*domain.Config) {}, 0, true},
		{"a delegate", func(*domain.Config) {}, 1, false},
		{"no scratch dir", func(cfg *domain.Config) { cfg.ScratchDir = "" }, 0, false},
		{"no workspace", func(cfg *domain.Config) { cfg.WorkspaceDir = "" }, 0, false},
		{"the plain sub_agent", func(cfg *domain.Config) {
			reg := domain.NewToolRegistry()
			_ = reg.Register(tools.NewSubAgent())
			_ = reg.Register(tools.NewWorkflow())
			cfg.Tools = reg
		}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := backgroundSubAgentConfig(t, &recordingSink{})
			tc.shape(&cfg)
			a, err := newAgent(cfg, &workflowResponder{})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.depth = tc.depth

			if got := a.delegationOffer().background; got != tc.want {
				t.Errorf("delegationOffer().background = %t, want %t", got, tc.want)
			}
		})
	}
}

// topLevelAgent is a depth-0 Agent over roster with a scratch dir and the given workspace — the
// fields delegationOffer reads, and nothing else.
func topLevelAgent(roster *domain.ToolRegistry, workspace string) *Agent {
	a := &Agent{tools: roster, cfg: domain.Config{WorkspaceDir: workspace}}
	a.SetScratchDir("/scratch")
	return a
}
