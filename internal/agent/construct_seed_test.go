package agent

// The construction seed: New and Resume finish the Agent from one seed — the sync lane
// (Config.Sync), the undo journal, the delegation seat and the delegation target — applied in one
// fixed order before the construction boundary's session-started announcement. A seed SetReactions
// would refuse fails construction with SetReactions' own error, wrapped as a SeedRefusedError.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/undo"
)

// seedConstructor is one of the two public constructors, over the echo responder, with opts.
type seedConstructor struct {
	name  string
	build func(t *testing.T, cfg domain.Config, opts ...Option) (*Agent, error)
}

// seedConstructors returns New and Resume (over an empty snapshot), each dialling responder.
func seedConstructors(responder *scriptedUpstream) []seedConstructor {
	dial := WithDialer(dialerTo(responder).dial)
	return []seedConstructor{
		{"New", func(_ *testing.T, cfg domain.Config, opts ...Option) (*Agent, error) {
			return New(cfg, append([]Option{dial}, opts...)...)
		}},
		{"Resume", func(t *testing.T, cfg domain.Config, opts ...Option) (*Agent, error) {
			return Resume(cfg, emptySession(t), append([]Option{dial}, opts...)...)
		}},
	}
}

// mustSeed constructs through c and fails the test on a refusal, closing the Agent at cleanup.
func mustSeed(t *testing.T, c seedConstructor, cfg domain.Config, opts ...Option) *Agent {
	t.Helper()
	a, err := c.build(t, cfg, opts...)
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// syncIDs lists the ids of a's live sync lane, in order.
func syncIDs(a *Agent) []string {
	var ids []string
	for _, r := range a.Generation().Sync {
		ids = append(ids, r.ID)
	}
	return ids
}

// TestConstructionSeed_ArmsTheSyncLane — a seeded sync lane is the live Generation's from the first
// moment, through either constructor, exactly as a SetReactions swap would have armed it.
func TestConstructionSeed_ArmsTheSyncLane(t *testing.T) {
	t.Parallel()

	for _, c := range seedConstructors(echoResponder(t, "reply")) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig(&recordingSink{})
			cfg.Sync = []domain.Reaction{goGate("warden", domain.GateDecision{Verdict: domain.GateAllow})}

			a := mustSeed(t, c, cfg)

			if got := syncIDs(a); !slices.Equal(got, []string{"warden"}) {
				t.Errorf("Generation().Sync = %v, want [warden]", got)
			}
		})
	}
}

// TestConstructionSeed_TheTargetStatesTheFarWidthOverASeededSeat — the seat's door forgets the far
// width and the target's states it, so the seed applies the seat first: a seed carrying both leaves
// the target's width as the one the model is told, not the session server's.
func TestConstructionSeed_TheTargetStatesTheFarWidthOverASeededSeat(t *testing.T) {
	t.Parallel()

	for _, c := range seedConstructors(echoResponder(t, "reply")) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig(&recordingSink{})
			cfg.ParallelAgents = 2
			seat := &DelegationSeat{Name: "grunt"}
			target := &DelegationTarget{Endpoint: "http://grunt.local:1111", Model: "cheap-4b", ParallelAgents: 5}

			a := mustSeed(t, c, cfg, WithDelegationTarget(target), WithDelegationSeat(seat))

			if got := a.statedDelegationWidth(); got != 5 {
				t.Errorf("statedDelegationWidth() = %d, want the target's 5", got)
			}
			if got := a.subAgentsSeat(); got != seat {
				t.Errorf("subAgentsSeat() = %+v, want the seeded seat", got)
			}
			if got := a.delegationTarget(); got != target {
				t.Errorf("delegationTarget() = %+v, want the seeded target", got)
			}
		})
	}
}

// TestConstructionSeed_InstallsTheUndoJournal — a seeded journal is the one the Agent records into,
// with its note; a nil one leaves construction's in-memory journal and no note.
func TestConstructionSeed_InstallsTheUndoJournal(t *testing.T) {
	t.Parallel()

	for _, c := range seedConstructors(echoResponder(t, "reply")) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			journal := undo.New()

			a := mustSeed(t, c, baseConfig(&recordingSink{}), WithUndoJournal(journal, "git not found"))

			if a.journal != journal {
				t.Error("the Agent records into a journal other than the seeded one")
			}
			if got := a.UndoNote(); got != "git not found" {
				t.Errorf("UndoNote() = %q, want %q", got, "git not found")
			}

			bare := mustSeed(t, c, baseConfig(&recordingSink{}), WithUndoJournal(nil, "ignored"))

			if bare.journal == nil || bare.UndoNote() != "" {
				t.Errorf("a nil seeded journal: journal=%v note=%q, want construction's journal and no note",
					bare.journal, bare.UndoNote())
			}
		})
	}
}

// TestConstructionSeed_ASessionStartedAdviseEntryRunsAtTheBoundary — the seeded sync lane is armed
// BEFORE construction announces the session, so its session-started advise entry is one of the
// construction boundary's own runs: fenced once ahead of the first prompt, booked once.
func TestConstructionSeed_ASessionStartedAdviseEntryRunsAtTheBoundary(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	for _, tc := range []struct {
		name       string
		withRunner bool
	}{
		{name: "with an observe Runner", withRunner: true},
		{name: "without an observe Runner", withRunner: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingSink{}
			responder := echoResponder(t, "ok")
			cfg := baseConfig(sink)
			if tc.withRunner {
				cfg.ObserveRunner = &startRecordingRunner{}
			}
			cfg.Sync = []domain.Reaction{liveAdviceEntry("ci")}

			a := mustSeed(t, seedConstructors(responder)[0], cfg)
			runExchange(t, a, "first prompt")

			first := lastUserContent(t, responder.requests()[0])
			if !strings.HasPrefix(first, userFenceFor("ci")) || strings.Count(first, userFenceFor("ci")) != 1 ||
				!strings.Contains(first, sessionAdviceText) || !strings.HasSuffix(first, "\n\nfirst prompt") {
				t.Errorf("first user message = %q, want the seeded advice fenced once ahead of the prompt", first)
			}
			if fired := sessionAdviseFirings(sink, "ci"); len(fired) != 1 {
				t.Errorf("booked %d firings for the seeded entry, want 1: %+v", len(fired), fired)
			}
		})
	}
}

// TestConstructionSeed_ScopesTheSyncLaneToTheWorkspace — an entry whose `workspace:` names another
// root is dropped from the seeded lane, as SetReactions drops it from a swapped-in one; an unscoped
// entry stays.
func TestConstructionSeed_ScopesTheSyncLaneToTheWorkspace(t *testing.T) {
	t.Parallel()

	elsewhere := goGate("elsewhere", domain.GateDecision{Verdict: domain.GateAllow})
	elsewhere.Workspace = t.TempDir()
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = t.TempDir()
	cfg.Sync = []domain.Reaction{goGate("here", domain.GateDecision{Verdict: domain.GateAllow}), elsewhere}

	a := mustSeed(t, seedConstructors(echoResponder(t, "reply"))[0], cfg)

	if got := syncIDs(a); !slices.Equal(got, []string{"here"}) {
		t.Errorf("Generation().Sync = %v, want [here] — the entry scoped to another root dropped", got)
	}
}

// TestConstructionSeed_ARefusedSeedFailsWithSetReactionsError — a seeded lane SetReactions would
// refuse fails construction with that same sentence, as a SeedRefusedError that unwraps to it.
// Validation runs on the UNSCOPED lane, so a reserved id is refused even when its `workspace:`
// names another root and scoping would have dropped it.
func TestConstructionSeed_ARefusedSeedFailsWithSetReactionsError(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	reserved := goGate(FloorGuardKeys()[0], domain.GateDecision{Verdict: domain.GateAllow})
	reservedElsewhere := reserved
	reservedElsewhere.Workspace = t.TempDir()
	observer := goGate("watcher", domain.GateDecision{Verdict: domain.GateAllow})
	observer.Class = domain.ClassObserve

	for _, tc := range []struct {
		name string
		sync []domain.Reaction
	}{
		{"a reserved id", []domain.Reaction{reserved}},
		{"a reserved id scoped to another root", []domain.Reaction{reservedElsewhere}},
		{"an observe entry in the sync lane", []domain.Reaction{observer}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig(&recordingSink{})
			cfg.WorkspaceDir = workspace

			reference, err := newAgent(cfg, echoResponder(t, "reply"))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			t.Cleanup(func() { _ = reference.Close() })
			want := reference.SetReactions(syncGen(reference, tc.sync...))
			if want == nil {
				t.Fatal("SetReactions accepted the lane; the fixture must be one it refuses")
			}

			for _, c := range seedConstructors(echoResponder(t, "reply")) {
				seeded := cfg
				seeded.Sync = tc.sync
				a, err := c.build(t, seeded)
				if a != nil {
					_ = a.Close()
					t.Fatalf("%s built an Agent over a refused seed", c.name)
				}
				var refused *SeedRefusedError
				if !errors.As(err, &refused) {
					t.Fatalf("%s = %v, want a *SeedRefusedError", c.name, err)
				}
				if got := err.Error(); got != want.Error() {
					t.Errorf("%s = %q, want SetReactions' %q", c.name, got, want.Error())
				}
				if inner := errors.Unwrap(err); inner == nil || inner.Error() != want.Error() {
					t.Errorf("%s unwraps to %v, want SetReactions' %v", c.name, inner, want)
				}
				if !errors.Is(err, domain.ErrInvalidReaction) {
					t.Errorf("%s = %v, want it to wrap ErrInvalidReaction", c.name, err)
				}
			}
		})
	}
}

// TestConstructionSeed_AChildIsSpawnedWithNoSync — a delegate takes the parent's sync lane on
// Config.Reactions (inheritedReactions), never as a seed of its own, so the entry is armed on the
// child exactly once.
func TestConstructionSeed_AChildIsSpawnedWithNoSync(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Sync = []domain.Reaction{goGate("warden", domain.GateDecision{Verdict: domain.GateAllow})}
	parent := mustSeed(t, seedConstructors(echoResponder(t, "reply"))[0], cfg)

	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })

	if len(child.cfg.Sync) != 0 || len(child.Generation().Sync) != 0 {
		t.Errorf("child Sync = %v (live %v), want none", child.cfg.Sync, child.Generation().Sync)
	}
	var inherited []string
	for _, r := range child.cfg.Reactions {
		inherited = append(inherited, r.ID)
	}
	if !slices.Equal(inherited, []string{"warden"}) {
		t.Errorf("child Config.Reactions = %v, want [warden] once", inherited)
	}
}
