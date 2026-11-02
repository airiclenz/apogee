package agent

import (
	"context"
	"iter"
	"testing"

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
	routedBefore := routed.upstream.responder()

	replacement := &replyResponder{reply: "from the replacement"}
	parent.upstream.replace(replacement)

	if unrouted.upstream != parent.upstream {
		t.Fatal("unrouted child holds a holder of its own, want its parent's shared by handle")
	}
	got := unrouted.collectCompletion(context.Background(), provider.Request{Model: parent.cfg.Model}, nil)
	if got.failed || got.content != replacement.reply {
		t.Errorf("unrouted child streamed (content %q, failed %v), want %q from the replacement",
			got.content, got.failed, replacement.reply)
	}

	if routed.upstream == parent.upstream {
		t.Fatal("routed child shares its parent's holder, want one of its own")
	}
	if up := routed.upstream.responder(); up != routedBefore || up == provider.Responder(replacement) {
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
	parent.ownsUpstream = true // what New does for a session that dialled its own client

	child := spawn(t, parent)
	if child.upstream != parent.upstream {
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
	oldHolder := a.upstream
	host := a.backgroundHost()

	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://new.local:1234"}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if a.upstream == oldHolder {
		t.Fatal("SwitchUpstream replaced into the old holder, want a fresh one on the switching Agent")
	}
	if _, ok := a.upstream.responder().(*provider.Client); !ok {
		t.Errorf("switched Agent Upstream = %T, want the freshly dialled *provider.Client", a.upstream.responder())
	}
	if host.upstream.responder() != provider.Responder(old) {
		t.Errorf("background host Upstream = %T after the switch, want the client it started with", host.upstream.responder())
	}
	if up, gen := oldHolder.current(); up != provider.Responder(old) || gen != 0 {
		t.Errorf("old holder = (%T, %d) after the switch, want (the old client, 0) untouched", up, gen)
	}
	if !oldHolder.isRetired() || !host.upstream.isRetired() {
		t.Error("the holder the switch left behind is not retired, want a background reader's fault to abandon")
	}
	if a.upstream.isRetired() {
		t.Error("the switching Agent's fresh holder is retired, want it live")
	}
}
