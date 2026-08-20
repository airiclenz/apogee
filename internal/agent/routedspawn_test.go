package agent

// The ROUTED spawn (ADR 0045, plan item 4): what newChildAgent builds when a Delegation target is
// latched. subagent_test.go covers the fallback — no target, the child inherits the parent's
// Upstream and posture verbatim — so these tests own the other half: the dial facts, window,
// profile and posture a routed child takes from the target instead, and the boundary between the
// two (a latch swap reaches the spawns AFTER it, never the children already built).
//
// They drive newChildAgent directly rather than through a delegation, because the question is what
// the CONSTRUCTION produces; the loop-level behaviour of a nested Agent is already covered.

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// routedTarget is the usable Sub-agent server these tests route to: another box, another model,
// a smaller window, its own fan-out width, and a delimited-thinking profile the parent does not
// have (so "the child parses in the grunt model's dialect" is observable).
func routedTarget() *DelegationTarget {
	return &DelegationTarget{
		Endpoint:       "http://grunt.local:1111",
		APIKey:         "grunt-key",
		Model:          "cheap-4b",
		ContextWindow:  32768,
		ParallelAgents: 3,
		Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{
			Style: domain.ThinkingDelimited,
			Start: "<think>",
			End:   "</think>",
		}},
	}
}

// routingParent builds a parent Agent with one tool, a scripted responder, and the session's own
// per-model bindings — the orchestrator side of the split, so every routed value below is visibly
// NOT the one the child would have inherited.
func routingParent(t *testing.T) *Agent {
	t.Helper()
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "w"})
	cfg.Endpoint = "http://session.local:9999"
	cfg.APIKey = "session-key"
	cfg.Model = "smart-70b"
	cfg.Context.MaxContextTokens = 131072
	a, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// spawn is the one-line child construction these tests repeat.
func spawn(t *testing.T, parent *Agent) *Agent {
	t.Helper()
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	return child
}

// TestRoutedSpawnBuildsFromTheTarget is the item's core acceptance: with a target latched the child
// is built against the SUB-AGENT server — its endpoint, key, model and window — on a provider client
// of its own rather than the parent's responder, and its parse seam speaks that model's dialect.
func TestRoutedSpawnBuildsFromTheTarget(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	target := routedTarget()
	parent.SetDelegationTarget(target)

	child := spawn(t, parent)

	if child.cfg.Endpoint != target.Endpoint || child.cfg.APIKey != target.APIKey {
		t.Errorf("routed child dial facts = %q/%q, want the target's %q/%q",
			child.cfg.Endpoint, child.cfg.APIKey, target.Endpoint, target.APIKey)
	}
	if child.cfg.Model != target.Model {
		t.Errorf("routed child model = %q, want the target's %q", child.cfg.Model, target.Model)
	}
	if child.cfg.Context.MaxContextTokens != target.ContextWindow {
		t.Errorf("routed child window = %d, want the target's %d",
			child.cfg.Context.MaxContextTokens, target.ContextWindow)
	}
	// The child dials the Sub-agent server itself: a client built from the target, never the
	// session's responder. The parent's own Upstream is untouched by the routing.
	if child.upstream == nil {
		t.Fatal("routed child has no Upstream")
	}
	if child.upstream == parent.upstream {
		t.Error("routed child reuses the parent's Upstream responder, want its own client on the target")
	}
	if _, ok := parent.upstream.(*scriptedResponder); !ok {
		t.Errorf("parent Upstream = %T after a routed spawn, want its own scriptedResponder untouched", parent.upstream)
	}
	// The profile rides the routing (ADR 0044/0045): the child's stripper is the target model's,
	// so a delimited thinking channel leaves the visible content in the CHILD even though the
	// parent has no thinking profile at all.
	if child.cfg.Profile.Thinking.Style != domain.ThinkingDelimited {
		t.Errorf("routed child profile thinking style = %q, want the target's delimited", child.cfg.Profile.Thinking.Style)
	}
	visible, reasoning := child.stripper.Strip("<think>plan it</think>the answer")
	if visible != "the answer" || reasoning != "plan it" {
		t.Errorf("routed child Strip = (%q, %q), want (%q, %q) — the target's profile is not in its parse seam",
			visible, reasoning, "the answer", "plan it")
	}
	if v, r := parent.stripper.Strip("<think>plan it</think>the answer"); v != "<think>plan it</think>the answer" || r != "" {
		t.Errorf("parent Strip = (%q, %q), want the raw text — routing must not touch the session's own seam", v, r)
	}
}

// TestUnroutedSpawnInheritsTheParent is the fallback in the same shape: a nil latch leaves every one
// of those values the parent's, which is what every delegation did before routing existed.
func TestUnroutedSpawnInheritsTheParent(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)

	child := spawn(t, parent)

	if child.cfg.Endpoint != parent.cfg.Endpoint || child.cfg.APIKey != parent.cfg.APIKey {
		t.Errorf("unrouted child dial facts = %q/%q, want the parent's %q/%q",
			child.cfg.Endpoint, child.cfg.APIKey, parent.cfg.Endpoint, parent.cfg.APIKey)
	}
	if child.cfg.Model != parent.cfg.Model {
		t.Errorf("unrouted child model = %q, want the parent's %q", child.cfg.Model, parent.cfg.Model)
	}
	if child.cfg.Context.MaxContextTokens != parent.cfg.Context.MaxContextTokens {
		t.Errorf("unrouted child window = %d, want the parent's %d",
			child.cfg.Context.MaxContextTokens, parent.cfg.Context.MaxContextTokens)
	}
	if child.upstream != parent.upstream {
		t.Error("unrouted child does not share the parent's Upstream responder, want today's path verbatim")
	}
}

// TestSpawnStampsItsOwnModelOnItsReadings pins what makes routing VISIBLE to a Driver (ADR 0045 §7):
// every reading an agent emits names the model that produced it, so a routed child's fill arrives
// stamped with the grunt model while the session's own arrives stamped with the session's. Without
// it a surface painting a delegation's fill has no way to say the work happened somewhere else.
func TestSpawnStampsItsOwnModelOnItsReadings(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	parent.SetDelegationTarget(routedTarget())

	routed := spawn(t, parent)
	if got := reading(routed).Model; got != "cheap-4b" {
		t.Errorf("routed child reading names %q, want the target's %q", got, "cheap-4b")
	}
	if got := reading(parent).Model; got != "smart-70b" {
		t.Errorf("session reading names %q, want the session's own %q", got, "smart-70b")
	}

	parent.SetDelegationTarget(nil)
	unrouted := spawn(t, parent)
	if got := reading(unrouted).Model; got != "smart-70b" {
		t.Errorf("unrouted child reading names %q, want the parent's %q — a fallback run is not news",
			got, "smart-70b")
	}
}

// reading is one usage event as the agent under test would emit it — the same two bindings the loop
// stamps (agent.go), so a test asks what a Driver would receive rather than what a call site typed.
func reading(a *Agent) domain.UsageEvent {
	return a.usage.record(a.base(1), a.cfg.Model, a.cfg.Context.MaxContextTokens, 10, 5, 15)
}

// TestSpawnStampsItsOwnWindowOnItsReadings is the model stamp's twin, and the reason a Driver can
// paint a routed fill honestly: a routed child works against the Delegation target's window
// (ADR 0045), so its readings must carry THAT number — a 7k fill on an 8k grunt server is `7k/8k`,
// not `7k/128k` against the session's. An unrouted child names the parent's window, which is the
// one it actually inherited.
func TestSpawnStampsItsOwnWindowOnItsReadings(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	parent.SetDelegationTarget(routedTarget())

	routed := spawn(t, parent)
	if got := reading(routed).ContextWindow; got != 32768 {
		t.Errorf("routed child reading names window %d, want the target's %d", got, 32768)
	}
	if got := reading(parent).ContextWindow; got != 131072 {
		t.Errorf("session reading names window %d, want the session's own %d", got, 131072)
	}

	parent.SetDelegationTarget(nil)
	unrouted := spawn(t, parent)
	if got := reading(unrouted).ContextWindow; got != 131072 {
		t.Errorf("unrouted child reading names window %d, want the parent's %d — inherited verbatim",
			got, 131072)
	}
}

// TestRoutedSpawnWithoutATargetWindowKeepsTheParents covers the one target field that may name
// NOTHING: a flagged entry with no `context-window:` pin, on a server whose beat observed no
// per-slot window, hands the engine a target with window 0. Taking that 0 would build the child
// windowless — Budget and automatic Compaction inactive, readings stamped 0 — so the parent's
// window stands instead, and the STAMP says so too: a Driver reading it paints the routed fill
// against a real limit rather than falling back to the session's window for a child in a different
// one. A target that does name a window still overrides, which is the routed case proper.
func TestRoutedSpawnWithoutATargetWindowKeepsTheParents(t *testing.T) {
	t.Parallel()

	const parentWindow = 131072 // routingParent's own, so "kept" and "replaced" are distinguishable

	cases := []struct {
		name         string
		targetWindow int
		want         int
	}{
		{name: "no pin and nothing observed keeps the parent's", targetWindow: 0, want: parentWindow},
		{name: "a negative window is no window either", targetWindow: -1, want: parentWindow},
		{name: "a named window still overrides", targetWindow: 32768, want: 32768},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parent := routingParent(t)
			target := routedTarget()
			target.ContextWindow = tc.targetWindow
			parent.SetDelegationTarget(target)

			child := spawn(t, parent)

			if got := child.cfg.Context.MaxContextTokens; got != tc.want {
				t.Errorf("routed child window = %d, want %d", got, tc.want)
			}
			if got := reading(child).ContextWindow; got != tc.want {
				t.Errorf("routed child reading names window %d, want the effective %d — a 0 stamp sends a Driver to the session's window",
					got, tc.want)
			}
			// The window guard is about the window alone: the rest of the routing still comes from
			// the target, so a windowless entry is a routed delegation and not a fallback.
			if child.cfg.Model != target.Model || child.cfg.Endpoint != target.Endpoint {
				t.Errorf("routed child = %q on %q, want the target's %q on %q",
					child.cfg.Model, child.cfg.Endpoint, target.Model, target.Endpoint)
			}
		})
	}
}

// TestRoutedSpawnResponseReserveShare covers how the window settled above is SPLIT, the one routed
// field whose absence is read as silence rather than as an answer. A target stating a share replaces
// the parent's, because that share describes how the SUB-AGENT server's window is worth dividing; a
// target stating none — an entry with no `response-reserve:` key, which the host carries through as
// a plain 0 (delegation.go, rank-free by design) — leaves the parent's resolved share standing,
// which already is the run's top-level key when nobody overrode it. Taking that 0 as an answer would
// hand every unstated routed child the engine's built-in fifth in place of the share the operator
// actually configured, and a fraction — unlike a token count — stays meaningful against any window,
// so there is nothing in the inherited number describing the wrong server.
func TestRoutedSpawnResponseReserveShare(t *testing.T) {
	t.Parallel()

	// The parent's own share, so "kept" and "replaced" are distinguishable, and neither is the
	// built-in 0.20 that a dropped share would fall to.
	const parentShare = 0.5

	cases := []struct {
		name        string
		targetShare float64
		want        float64
	}{
		{name: "a target stating a share splits its own window by it", targetShare: 0.35, want: 0.35},
		{name: "a target stating none leaves the parent's share standing", targetShare: 0, want: parentShare},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parent := routingParent(t)
			// Written onto the built parent because the share has no setter of its own: its only live
			// door is the atomic rebind commit (rebind.go), which is not what this test is asking about.
			parent.cfg.Context.ResponseReserveFraction = parentShare
			target := routedTarget()
			target.ResponseReserveFraction = tc.targetShare
			parent.SetDelegationTarget(target)

			child := spawn(t, parent)

			if got := child.cfg.Context.ResponseReserveFraction; got != tc.want {
				t.Errorf("routed child reserve share = %v, want %v", got, tc.want)
			}
			// And what that share MEANS for the child: the reserve is held back out of the target's
			// window, not out of the session's, so the two halves of a routed budget agree.
			wantReserve := int(tc.want * float64(target.ContextWindow))
			if got := child.budget().ResponseReserve; got != wantReserve {
				t.Errorf("routed child holds %d tokens back for the reply, want %d — %v of the target's "+
					"%d window", got, wantReserve, tc.want, target.ContextWindow)
			}
			// The parent divides its own window exactly as it did: routing changes what a SPAWN is
			// built with, never the session's own split.
			if got := parent.cfg.Context.ResponseReserveFraction; got != parentShare {
				t.Errorf("parent reserve share = %v after a routed spawn, want its own %v untouched",
					got, parentShare)
			}
		})
	}
}

// TestRoutedSpawnBypassPosture drives ADR 0045 §2's replace-or-inherit rule on the Bypass half: a
// present flag replaces the parent's LIVE value in both directions, an absent one inherits it.
func TestRoutedSpawnBypassPosture(t *testing.T) {
	t.Parallel()

	on, off := true, false
	cases := []struct {
		name         string
		parentBypass bool
		targetBypass *bool
		want         bool
	}{
		{name: "present true replaces a parent that is off", parentBypass: false, targetBypass: &on, want: true},
		{name: "present false replaces a parent that is on", parentBypass: true, targetBypass: &off, want: false},
		{name: "absent inherits the parent's live on", parentBypass: true, targetBypass: nil, want: true},
		{name: "absent inherits the parent's live off", parentBypass: false, targetBypass: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parent := routingParent(t)
			// SetBypass, not the construction seed: the rule is about the parent's LIVE flag.
			parent.SetBypass(tc.parentBypass)
			target := routedTarget()
			target.Bypass = tc.targetBypass
			parent.SetDelegationTarget(target)

			child := spawn(t, parent)

			if got := child.bypassEnabled(); got != tc.want {
				t.Errorf("routed child Bypass = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRoutedSpawnMechanismsPosture is the catalogue half of the same rule: a factory on the target
// REPLACES the inherited catalogue whole and is called once per child (a fresh registry each, the
// live-state isolation a concurrent fan-out needs), while no factory leaves ForSubAgent's inherited
// copy standing.
func TestRoutedSpawnMechanismsPosture(t *testing.T) {
	t.Parallel()

	// The parent's catalogue carries one experimental hook, so "inherited" and "replaced" are
	// distinguishable by what the child's registry holds.
	newParent := func(t *testing.T) *Agent {
		t.Helper()
		cfg := configWithTools(&recordingSink{}, fakeTool{name: "w"})
		cfg.Mechanisms = domain.NewMechanismRegistry()
		fired := false
		if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &fired}); err != nil {
			t.Fatalf("AddExperimental: %v", err)
		}
		a, err := newAgent(cfg, &scriptedResponder{})
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		return a
	}

	t.Run("factory present replaces the inherited catalogue", func(t *testing.T) {
		t.Parallel()

		parent := newParent(t)
		var built []*domain.MechanismRegistry
		target := routedTarget()
		target.Mechanisms = func() *domain.MechanismRegistry {
			r := domain.NewMechanismRegistry() // the Sub-agent server's own posture: nothing armed
			built = append(built, r)
			return r
		}
		parent.SetDelegationTarget(target)

		first := spawn(t, parent)
		second := spawn(t, parent)

		if len(built) != 2 {
			t.Fatalf("factory calls = %d, want one per child (2)", len(built))
		}
		if built[0] == built[1] {
			t.Error("both children got the SAME registry, want a fresh one each (siblings run at once)")
		}
		if first.registry != built[0] || second.registry != built[1] {
			t.Error("children do not run the registries the factory built")
		}
		if hooks := first.registry.Experimental(domain.HookPreRequest); len(hooks) != 0 {
			t.Errorf("routed child pre-request hooks = %d, want 0 — the target's catalogue replaces the parent's whole", len(hooks))
		}
		if hooks := parent.registry.Experimental(domain.HookPreRequest); len(hooks) != 1 {
			t.Errorf("parent pre-request hooks = %d after routed spawns, want its own 1 untouched", len(hooks))
		}
	})

	t.Run("factory absent inherits through ForSubAgent", func(t *testing.T) {
		t.Parallel()

		parent := newParent(t)
		target := routedTarget()
		target.Mechanisms = nil
		parent.SetDelegationTarget(target)

		child := spawn(t, parent)

		if child.registry == parent.registry {
			t.Error("routed child shares the parent's registry by pointer, want ForSubAgent's own container")
		}
		if hooks := child.registry.Experimental(domain.HookPreRequest); len(hooks) != 1 {
			t.Errorf("routed child pre-request hooks = %d, want the parent's 1 inherited through ForSubAgent", len(hooks))
		}
	})
}

// TestLatchSwapReachesOnlyLaterSpawns is the never-idle-gated contract seen from the spawn side: a
// beat that lands between two delegations routes the second and leaves the first — already built and
// possibly already running — exactly where it was. Clearing the latch is the same story in reverse.
func TestLatchSwapReachesOnlyLaterSpawns(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)

	before := spawn(t, parent) // no target yet: the fallback

	parent.SetDelegationTarget(routedTarget())
	during := spawn(t, parent)

	parent.SetDelegationTarget(nil) // the Sub-agent server went away
	after := spawn(t, parent)

	if before.cfg.Model != parent.cfg.Model || before.upstream != parent.upstream {
		t.Errorf("child spawned BEFORE the target was latched = model %q, want the parent's %q on the parent's Upstream",
			before.cfg.Model, parent.cfg.Model)
	}
	if during.cfg.Model != "cheap-4b" || during.upstream == parent.upstream {
		t.Errorf("child spawned WHILE the target was latched = model %q on the parent's Upstream (%v), want the routed cheap-4b on its own",
			during.cfg.Model, during.upstream == parent.upstream)
	}
	if after.cfg.Model != parent.cfg.Model || after.upstream != parent.upstream {
		t.Errorf("child spawned AFTER the clearing push = model %q, want the parent's %q on the parent's Upstream",
			after.cfg.Model, parent.cfg.Model)
	}
}

// TestRoutedSpawnClosesItsOwnClient: the client a routed spawn DIALS is the child's own, so the
// child is the one that tears it down — nothing else holds it, and the parent's Close must not be
// made responsible for a connection to a server the session never spoke to. Ownership and the
// teardown it authorises are both asserted: the flag alone would not prove Close reaches the wire.
func TestRoutedSpawnClosesItsOwnClient(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	parent.SetDelegationTarget(routedTarget())

	child := spawn(t, parent)

	if _, ok := child.upstream.(*provider.Client); !ok {
		t.Fatalf("routed child Upstream = %T, want the client the spawn dialled", child.upstream)
	}
	if !child.ownsUpstream {
		t.Fatal("routed child does not own the client it dialled — nothing would ever close it")
	}
	if parent.ownsUpstream {
		t.Error("routing gave the parent ownership of a client it did not build")
	}

	// The real client's Close is invisible from here (it reaps idle sockets), so stand a counting
	// closer in its place to watch the teardown the ownership flag authorises actually fire.
	dialled := &closingResponder{}
	child.upstream = dialled
	if err := child.Close(); err != nil {
		t.Fatalf("routed child Close: %v", err)
	}
	if dialled.closes != 1 {
		t.Errorf("routed child Close closed its client %d times, want exactly 1", dialled.closes)
	}
}
