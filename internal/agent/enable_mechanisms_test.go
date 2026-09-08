package agent

// Construction-path coverage for Config.Reactions (ADR 0076 stage 1, recast off the retired
// enable-list arm): the engine validates every armed Reaction at New and at Resume,
// fails construction on an ill-formed or shadowed entry, and arms nothing at all when the list is
// nil or empty — observed through the loop's own effects (a construction error, a firing) rather
// than the Agent's internals. Config.Reactions is the seam every Driver and the bench drive, so
// these prove the real arm-and-validate path end to end.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestReactions_IllFormedEntryFailsConstruction: an entry that cannot fire — here one whose On
// list names a Moment its handler does not serve — fails New with a matchable ErrInvalidReaction
// rather than being armed and silently doing nothing.
func TestReactions_IllFormedEntryFailsConstruction(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Reactions = []domain.Reaction{{
		ID:     "wrong_seam",
		Origin: domain.OriginEngine,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPostResponse},
		Handler: domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
			return domain.Outcome{}, nil
		}),
	}}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if !errors.Is(err, domain.ErrInvalidReaction) {
		t.Errorf("newAgent err = %v, want it to wrap domain.ErrInvalidReaction", err)
	}
}

// TestReactions_ShadowedIDRejectionCarriesOnePrefix: an arm-path rejection is RETURNED to the
// host, and cmd/apogee/main.go prints a returned error verbatim — so it has to read as ONE
// "apogee: "-prefixed line naming the ID that failed. The rejection driven here is the one a bench
// hits first: an entry reusing an engine builtin's ID, which would make every attribution keyed on
// that ID ambiguous.
func TestReactions_ShadowedIDRejectionCarriesOnePrefix(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	fired := false
	cfg.Reactions = []domain.Reaction{firingReaction("tool-loop-breaker", &fired)}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err == nil {
		t.Fatal("newAgent accepted a Reaction shadowing a builtin's ID; want a refusal")
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "apogee: ") {
		t.Errorf("newAgent err = %q; want it to start with %q", msg, "apogee: ")
	}
	if got := strings.Count(msg, "apogee: "); got != 1 {
		t.Errorf("newAgent err = %q; want exactly one %q prefix, got %d", msg, "apogee: ", got)
	}
	if !strings.Contains(msg, `"tool-loop-breaker"`) {
		t.Errorf("newAgent err = %q; want it to name the reaction that failed", msg)
	}
}

// TestReactions_SurviveConstructionAndFire: what the host arms on Config.Reactions stands — the
// engine's own builtins are added BESIDE it, never in place of it (locked decision 2, carried onto
// the Reaction core) — and the armed entry fires through the real loop.
func TestReactions_SurviveConstructionAndFire(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})
	fired := false
	cfg.Reactions = []domain.Reaction{firingReaction("provided_probe", &fired)}

	a, err := newAgent(cfg, echoResponder{reply: "done"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "update the config file")

	if !fired {
		t.Error("the armed Reaction did not fire; construction dropped what the host armed")
	}
}

// TestReactions_NilAndEmptyArmNothing: a nil and an empty list both arm NOTHING beside the
// engine's builtins. Every user-facing Reaction is default-off (ADR 0076 D1), so an embedder that
// hands New a Config with no Reactions gets the Floor guards and nothing else, and its recovery
// guarantees from Config.Floor.
//
// It is read off what construction armed rather than off fired events: what the list ARMS is the
// claim, and a Reaction that never triggers on a well-behaved reply would make an event-based
// assertion say nothing at all.
func TestReactions_NilAndEmptyArmNothing(t *testing.T) {
	cases := map[string][]domain.Reaction{
		"nil":   nil,
		"empty": {},
	}
	for name, reactions := range cases {
		t.Run(name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Reactions = reactions

			a, err := newAgent(cfg, echoResponder{reply: "hi"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			if got := len(a.armed); got != 0 {
				t.Errorf("armed Reactions = %d, want nothing armed beside the builtins", got)
			}
		})
	}
}

// TestReactions_ResumeArmsIdentically: Resume arms the list the same way New does — Reactions are
// Config, not session state — so a resumed Agent walks the same arm path and refuses the same
// list. The pin is the refusal: a Config whose Reactions shadow a builtin's ID fails resumeAgent
// exactly as it fails newAgent, which it could only do by re-arming from Config rather than
// restoring from the snapshot.
func TestReactions_ResumeArmsIdentically(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})

	a, err := newAgent(cfg, echoResponder{reply: "done"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "update the config file")
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	cfg2 := configWithTools(&recordingSink{}, fakeTool{name: "write_file", result: "ok"})
	shadowFired := false
	cfg2.Reactions = []domain.Reaction{firingReaction("tool-loop-breaker", &shadowFired)}
	if _, err := resumeAgent(cfg2, snap, echoResponder{reply: "unreached"}); !errors.Is(err, domain.ErrInvalidReaction) {
		t.Errorf("resumeAgent err = %v, want ErrInvalidReaction; Reactions must be re-armed from Config, not session state", err)
	}
}

// TestBuildMechanisms_ArmsTheSameSetWithoutAnAgent: the host-facing half of the same build (ADR
// 0045). A Delegation target's Mechanisms posture is composed by the HOST, which needs the registry
// rather than an Agent, so BuildMechanisms hands one back off the very path New walks. The shipped
// catalogue is empty since v0.20.0 (ADR 0071), so every id list resolves to an empty registry — and
// the registry comes back fresh and unowned either way; a child takes a copy through ForSubAgent.
func TestBuildMechanisms_ArmsTheSameSetWithoutAnAgent(t *testing.T) {
	cfg := baseConfig(&recordingSink{})

	registry, err := BuildMechanisms(cfg, nil)
	if err != nil {
		t.Fatalf("BuildMechanisms with no ids: %v, want an empty registry", err)
	}
	if got := len(registry.Ordered(domain.HookPreRequest)); got != 0 {
		t.Errorf("armed pre-request rows = %d, want 0 — the shipped catalogue is empty", got)
	}
	if sub := registry.ForSubAgent(); sub == registry {
		t.Error("ForSubAgent handed back the same container; a child must never share the built one")
	}
}

// TestBuildMechanisms_RefusesWhatConstructionRefuses: the error is the construction error, raised
// where the host can still name the config that asked for it — an unknown ID wrapping
// ErrUnknownMechanism, exactly as New refuses the same list. BuildMechanisms builds into a FRESH
// registry and never reads the host's own, so the refusal it is checked on has to be one the
// shipped catalogue can still trip: the incompatibility gate no longer qualifies, its last two
// declarers having been promoted to Floor guards and retired outright in v0.20.0 (ADR 0071). The
// gate itself is pinned over synthetic rows in internal/domain.
func TestBuildMechanisms_RefusesWhatConstructionRefuses(t *testing.T) {
	cfg := baseConfig(&recordingSink{})

	_, err := BuildMechanisms(cfg, []domain.MechanismID{"no_such_mechanism"})
	if !errors.Is(err, domain.ErrUnknownMechanism) {
		t.Errorf("BuildMechanisms with an unknown ID = %v, want ErrUnknownMechanism", err)
	}
}
