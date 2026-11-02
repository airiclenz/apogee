package agent

import (
	"context"
	"iter"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// replyResponder answers every request with one fixed reply, so a test can tell from the content
// alone which Upstream a request reached.
type replyResponder struct {
	reply string
}

func (r *replyResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return streamReply(r.reply)
}

// TestSharedUpstreamGenerationAdvancesOnReplace pins the holder's counter: zero at construction,
// one more on every replace, and read under the same lock as the Responder it describes. A nil
// holder answers no Upstream at generation zero rather than panicking.
func TestSharedUpstreamGenerationAdvancesOnReplace(t *testing.T) {
	t.Parallel()

	first, second, third := &replyResponder{reply: "1"}, &replyResponder{reply: "2"}, &replyResponder{reply: "3"}
	holder := newSharedUpstream(first)
	if up, gen := holder.current(); up != provider.Responder(first) || gen != 0 {
		t.Fatalf("fresh holder = (%v, %d), want (first, 0)", up, gen)
	}

	for i, next := range []*replyResponder{second, third} {
		want := uint64(i + 1)
		if got := holder.replace(next); got != want {
			t.Errorf("replace #%d returned generation %d, want %d", i+1, got, want)
		}
		if up, gen := holder.current(); up != provider.Responder(next) || gen != want {
			t.Errorf("after replace #%d holder = (%v, %d), want (%q, %d)", i+1, up, gen, next.reply, want)
		}
	}

	var none *sharedUpstream
	if up, gen := none.current(); up != nil || gen != 0 {
		t.Errorf("nil holder = (%v, %d), want (nil, 0)", up, gen)
	}
	if none.responder() != nil {
		t.Error("nil holder answers a Responder, want none")
	}
}

// TestSharedUpstreamUnroutedChildStreamsFromReplacement is the point of the holder: a child built
// over its parent's client BEFORE the parent's holder is replaced streams its next request from the
// replacement, because it reads the same cell. A routed child, on a holder of its own, does not.
func TestSharedUpstreamUnroutedChildStreamsFromReplacement(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	unrouted := spawn(t, parent)
	parent.SetDelegationTarget(routedTarget())
	routed := spawn(t, parent)
	routedBefore := routed.seating.upstream.responder()

	replacement := &replyResponder{reply: "from the replacement"}
	parent.seating.upstream.replace(replacement)

	if unrouted.seating.upstream != parent.seating.upstream {
		t.Fatal("unrouted child holds a holder of its own, want its parent's shared by handle")
	}
	got := unrouted.collectCompletion(context.Background(), provider.Request{Model: parent.cfg.Model}, nil)
	if got.failed || got.content != replacement.reply {
		t.Errorf("unrouted child streamed (content %q, failed %v), want %q from the replacement",
			got.content, got.failed, replacement.reply)
	}

	if routed.seating.upstream == parent.seating.upstream {
		t.Fatal("routed child shares its parent's holder, want one of its own")
	}
	if up := routed.seating.upstream.responder(); up != routedBefore || up == provider.Responder(replacement) {
		t.Errorf("routed child Upstream = %T after the parent's replace, want the client its spawn dialled", up)
	}
}

// TestSharedUpstreamUnroutedChildCloseLeavesParentClient holds ownership where it was: sharing the
// holder hands the child no right to tear the session's client down, so its Close closes nothing
// and the parent's later Close closes it exactly once.
func TestSharedUpstreamUnroutedChildCloseLeavesParentClient(t *testing.T) {
	t.Parallel()

	client := &closingResponder{}
	parent, err := newAgent(baseConfig(&recordingSink{}), client)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	parent.seating.ownsUpstream = true // what New does for a session that dialled its own client

	child := spawn(t, parent)
	if child.seating.upstream != parent.seating.upstream {
		t.Fatal("unrouted child holds a holder of its own, want its parent's shared by handle")
	}
	if err := child.Close(); err != nil {
		t.Fatalf("child Close: %v", err)
	}
	if client.closes != 0 {
		t.Errorf("unrouted child's Close closed the session's client %d times, want 0", client.closes)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("parent Close: %v", err)
	}
	if client.closes != 1 {
		t.Errorf("parent's Close closed its client %d times, want 1", client.closes)
	}
}

// TestSharedUpstreamSwitchLeavesBackgroundHost pins what a server switch moves: SwitchUpstream
// installs a FRESH holder on the switching Agent, so a background workflow's host, which copied the
// old holder, stays on the client it started with — and the old holder itself is not replaced, but
// retired, so a fault a background child hits on it never moves the seat (consultFallback).
func TestSharedUpstreamSwitchLeavesBackgroundHost(t *testing.T) {
	t.Parallel()

	old := &replyResponder{reply: "old server"}
	a, err := newAgent(baseConfig(&recordingSink{}), old)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	oldHolder := a.seating.upstream
	host := a.backgroundHost()

	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://new.local:1234"}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if a.seating.upstream == oldHolder {
		t.Fatal("SwitchUpstream replaced into the old holder, want a fresh one on the switching Agent")
	}
	if _, ok := a.seating.upstream.responder().(*provider.Client); !ok {
		t.Errorf("switched Agent Upstream = %T, want the freshly dialled *provider.Client", a.seating.upstream.responder())
	}
	if host.seating.upstream.responder() != provider.Responder(old) {
		t.Errorf("background host Upstream = %T after the switch, want the client it started with", host.seating.upstream.responder())
	}
	if up, gen := oldHolder.current(); up != provider.Responder(old) || gen != 0 {
		t.Errorf("old holder = (%T, %d) after the switch, want (the old client, 0) untouched", up, gen)
	}
	if !oldHolder.isRetired() || !host.seating.upstream.isRetired() {
		t.Error("the holder the switch left behind is not retired, want a background reader's fault to abandon")
	}
	if a.seating.upstream.isRetired() {
		t.Error("the switching Agent's fresh holder is retired, want it live")
	}
}

// TestSeatBindingInheritance pins the seat each spawn door assigns whole (seatBinding): an unrouted
// child and a background workflow's host inherit the parent's seat — its holder, its resolver and the
// generation the parent has APPLIED, owning nothing — while a routed child sits on a Sub-agents
// holder of its own at generation zero, owns the client it dialled and consults its target's
// resolver, never the session's.
func TestSeatBindingInheritance(t *testing.T) {
	t.Parallel()
	const parentApplied = 3 // a generation the parent has applied, so inheriting it differs from starting at zero
	targetResolver := &stubResolver{}
	cases := []struct {
		name   string
		target *DelegationTarget
		spawn  func(t *testing.T, r *seatRig) *Agent
		routed bool
	}{
		{
			name:  "unrouted child",
			spawn: func(t *testing.T, r *seatRig) *Agent { return r.child(t, "unrouted") },
		},
		{
			name:  "background host",
			spawn: func(t *testing.T, r *seatRig) *Agent { return r.parent.backgroundHost() },
		},
		{
			name:   "routed child",
			target: &DelegationTarget{Endpoint: seatGruntEndpoint, Model: "grunt-model", Fallback: targetResolver},
			spawn:  func(t *testing.T, r *seatRig) *Agent { return r.child(t, "routed") },
			routed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newSeatRig(t, nil)
			r.parent.seating.appliedGen = parentApplied
			if tc.target != nil {
				r.parent.SetDelegationTarget(tc.target)
			}

			got := tc.spawn(t, r).seating

			want := seatBinding{upstream: r.parent.seating.upstream, fallback: r.resolver, appliedGen: parentApplied}
			if tc.routed {
				if got.upstream == r.parent.seating.upstream || got.upstream.seatOf() != domain.UpstreamSeatSubAgents {
					t.Fatalf("routed seat on %q, shared with the parent = %v; want a Sub-agents holder of its own",
						got.upstream.seatOf(), got.upstream == r.parent.seating.upstream)
				}
				want = seatBinding{upstream: got.upstream, fallback: targetResolver, ownsUpstream: true}
			}
			if got != want {
				t.Errorf("seat = %+v, want %+v", got, want)
			}
		})
	}
}

// TestSeatBindingSwitchKeepsTheResolver pins the switched seat: a `/server` moves the session onto a
// fresh holder it owns at generation zero and KEEPS the seat's resolver, so a fault on the new
// server still falls over — one session-seat switch onto the spare — rather than abandoning.
func TestSeatBindingSwitchKeepsTheResolver(t *testing.T) {
	t.Parallel()
	r := newSeatRig(t, nil)
	before := r.parent.seating.upstream
	r.switchAway(t)
	seat := r.parent.seating
	if seat.upstream == before || seat.fallback != r.resolver || seat.appliedGen != 0 || !seat.ownsUpstream {
		t.Fatalf("switched seat = %+v, want a fresh owned holder at generation 0 with the session's resolver", seat)
	}
	if err := r.parent.Rebind(RebindSpec{Model: testModel, MaxContextTokens: 1_000_000}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	res := stepOnce(t, r.parent, "ask the new server")

	if res.Faulted {
		t.Fatalf("result = %+v, want the Exchange finished on the fallback", res)
	}
	if n := len(r.resolver.seen()); n != 1 {
		t.Errorf("resolver reports = %d, want 1 — the fault on the new holder consults the kept resolver", n)
	}
	events := r.switchedEvents()
	if len(events) != 1 || events[0].Seat != domain.UpstreamSeatSession || events[0].ToServer != "spare" {
		t.Errorf("events = %+v, want one session-seat switch onto the spare", events)
	}
}
