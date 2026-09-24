package agent

// Coverage for the Dialer seam: every provider dial the engine makes — the session's client (New,
// Resume), the replacement a switch binds (SwitchUpstream) and the client a routed spawn builds for
// its child — crosses the ONE Dialer the constructor was given, and a child inherits it so a
// grandchild's dial crosses the same one. The fake Dialer below is the double the rest of the
// package's dial-only tests stand on instead of an httptest server: it records what was dialled and
// answers with whichever in-process Responder the test chose.

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/provider"
)

// dialRecord is what the fake Dialer noted about one dial: the three facts a constructor, a switch
// or a routed spawn has to hand the seam TOGETHER — the wire target, the model bound on it and the
// key it carries — and what the Options asked the client for, read back off a Client built from
// those Options (provider.Option is opaque; Client.Wire and Client.StreamIdleTimeout are its
// accessors): the wire it speaks, the silence bound its streams run under, and the server identity
// its attempt measurements carry (Client.ServerIdentity — the entry's name and its endpoint, redacted).
type dialRecord struct {
	endpoint, model, apiKey string
	wire                    provider.Wire
	idle                    time.Duration
	server, idEndpoint      string
}

// fakeDialer is the test double behind WithDialer. It records every dial in order and answers each
// from answer, keyed by the endpoint dialled — so one fake can serve the session's endpoint with the
// parent's script and the Sub-agent server's with the grunt's. Its mutex is real: a fan-out's routed
// children are dialled from several goroutines at once.
type fakeDialer struct {
	mu     sync.Mutex
	dials  []dialRecord
	answer func(endpoint string) provider.Responder
}

// dialerAnswering builds a fake Dialer whose every dial is answered by answer(endpoint).
func dialerAnswering(answer func(endpoint string) provider.Responder) *fakeDialer {
	return &fakeDialer{answer: answer}
}

// dialerTo builds a fake Dialer that answers EVERY dial with up, whatever the endpoint — the shape
// for a test that routes to one Sub-agent server and never dials anything else through it.
func dialerTo(up provider.Responder) *fakeDialer {
	return dialerAnswering(func(string) provider.Responder { return up })
}

// dial is the Dialer the fake is installed as (WithDialer(d.dial)). The provider Options arm no
// client — an in-process Responder has none — but they are applied to a throwaway one so the wire,
// the idle bound and the server identity they carry are recorded: that is the only way to observe
// what a real dial would have spoken, how long it would have let the server stay silent, and which
// server its attempt measurements would have named.
func (d *fakeDialer) dial(endpoint, model, apiKey string, opts ...provider.Option) provider.Responder {
	client := provider.NewClient("", "", opts...)
	server, idEndpoint, _ := client.ServerIdentity()
	d.mu.Lock()
	d.dials = append(d.dials, dialRecord{
		endpoint: endpoint, model: model, apiKey: apiKey,
		wire: client.Wire(), idle: client.StreamIdleTimeout(),
		server: server, idEndpoint: idEndpoint,
	})
	d.mu.Unlock()
	return d.answer(endpoint)
}

// dialled returns every dial the fake answered, in order, under its lock.
func (d *fakeDialer) dialled() []dialRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.dials)
}

// TestDialerIsUsedForSwitchUpstreamAndRoutedSpawn walks every dial site through one injected
// Dialer: New dials the session's client through it, a routed spawn dials the child's client through
// it and hands the child the same seam (the grandchild's dial proves the inheritance), and a switch
// dials the replacement through it — each dial carrying the endpoint, model, key and wire of the
// seat it binds, and each Agent left speaking over exactly the Responder the fake answered with.
//
// The wire is a per-server fact (ADR 0078), so the three seats deliberately differ: the session
// starts on an anthropic entry, the routed target names NO wire — and the child dials openai, the
// target's own folded answer, never the parent's anthropic — and the switch arrives on an
// anthropic server again, the spec's value replacing whatever the retired client spoke. The
// stream idle bound is the opposite kind of fact — the session's, not a server's — so every dial
// carries cfg.StreamIdleTimeout unchanged: the session's, the routed child's (and grandchild's),
// and the switched client's alike. The server identity attempt measurements carry (ADR 0085) is a
// per-server fact again: each dial names the entry it binds — the session's, the routed target's
// own name and endpoint, the switch's arrived-at server — never the parent's or the departed one's.
func TestDialerIsUsedForSwitchUpstreamAndRoutedSpawn(t *testing.T) {
	t.Parallel()

	const (
		sessionEndpoint  = "http://session.local:9999"
		switchedEndpoint = "http://elsewhere.local:1234"
	)
	session := scriptedResponder(t)
	grunt := echoResponder(t, "grunt done")
	switched := echoResponder(t, "from the new server")
	dialer := dialerAnswering(func(endpoint string) provider.Responder {
		switch endpoint {
		case sessionEndpoint:
			return session
		case switchedEndpoint:
			return switched
		default:
			return grunt
		}
	})

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "w"})
	cfg.Endpoint = sessionEndpoint
	cfg.APIKey = "session-key"
	cfg.Model = "smart-70b"
	cfg.Wire = "anthropic"
	cfg.ServerName = "session-box"
	cfg.StreamIdleTimeout = 45 * time.Second
	cfg.Delegation.MaxDepth = 2 // room for the grandchild below
	a, err := New(cfg, WithDialer(dialer.dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.upstream != session {
		t.Fatalf("New bound %T as the Upstream, want the Responder the Dialer answered the session dial with", a.upstream)
	}
	if !a.ownsUpstream {
		t.Error("New left the dialled client unowned; Close would never tear it down")
	}

	target := routedTarget()
	target.ServerName = "grunt-box"
	a.SetDelegationTarget(target)
	child := spawn(t, a)
	if child.upstream != grunt {
		t.Errorf("routed child Upstream = %T, want the Responder the Dialer answered the target dial with", child.upstream)
	}
	if !child.ownsUpstream {
		t.Error("routed child does not own the client the Dialer handed it")
	}
	// The grandchild is the inheritance proof: its routed dial can only reach the fake through the
	// Dialer the child took from its parent.
	grandchild := spawn(t, child)
	if grandchild.upstream != grunt {
		t.Errorf("routed grandchild Upstream = %T, want the inherited Dialer's answer", grandchild.upstream)
	}

	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: switchedEndpoint, APIKey: "new-key", Wire: "anthropic", ServerName: "elsewhere-box"}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	if a.upstream != switched {
		t.Errorf("Upstream after the switch = %T, want the Responder the Dialer answered the switch with", a.upstream)
	}
	if a.cfg.Wire != "anthropic" {
		t.Errorf("Config.Wire after the switch = %q, want the spec's %q mirrored, so a later dial speaks it", a.cfg.Wire, "anthropic")
	}

	idle := cfg.StreamIdleTimeout
	want := []dialRecord{
		{endpoint: sessionEndpoint, model: "smart-70b", apiKey: "session-key", wire: provider.WireAnthropic, idle: idle, server: "session-box", idEndpoint: sessionEndpoint},
		{endpoint: target.Endpoint, model: target.Model, apiKey: target.APIKey, wire: provider.WireOpenAI, idle: idle, server: "grunt-box", idEndpoint: target.Endpoint}, // the target's own unnamed wire, not the parent's
		{endpoint: target.Endpoint, model: target.Model, apiKey: target.APIKey, wire: provider.WireOpenAI, idle: idle, server: "grunt-box", idEndpoint: target.Endpoint},
		{endpoint: switchedEndpoint, model: "", apiKey: "new-key", wire: provider.WireAnthropic, idle: idle, server: "elsewhere-box", idEndpoint: switchedEndpoint}, // a switch binds NO model (ADR 0024)
	}
	if got := dialer.dialled(); !slices.Equal(got, want) {
		t.Errorf("dials through the seam = %+v, want %+v", got, want)
	}
}

// TestResumeDialsThroughTheDialer: Resume is the second constructor that dials, and it takes the
// same Option — a resumed session's client is the injected Dialer's answer, dialled with the
// Config's own seat facts, the stream idle bound among them.
func TestResumeDialsThroughTheDialer(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.APIKey = "resumed-key"
	cfg.StreamIdleTimeout = 45 * time.Second
	seed, err := newAgent(cfg, echoResponder(t, "hello"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, seed, "hi")
	snap, err := seed.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	resumed := echoResponder(t, "resumed")
	dialer := dialerTo(resumed)
	b, err := Resume(cfg, snap, WithDialer(dialer.dial))
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if b.upstream != resumed {
		t.Errorf("Resume bound %T as the Upstream, want the Dialer's answer", b.upstream)
	}
	want := []dialRecord{{endpoint: cfg.Endpoint, model: cfg.Model, apiKey: "resumed-key", wire: provider.WireOpenAI, idle: cfg.StreamIdleTimeout, idEndpoint: cfg.Endpoint}}
	if got := dialer.dialled(); !slices.Equal(got, want) {
		t.Errorf("dials through the seam = %+v, want %+v", got, want)
	}
	if got := b.conv.Len(); got != seed.conv.Len() {
		t.Errorf("resumed conversation has %d messages, want the snapshot's %d", got, seed.conv.Len())
	}
}

// TestWithDialerNilKeepsTheDefault: a nil Dialer is not "dial nothing" — the option is ignored and
// the real provider client stays the default, so a host that forwards an unset seam gets the wire.
func TestWithDialerNilKeepsTheDefault(t *testing.T) {
	t.Parallel()

	a, err := New(baseConfig(&recordingSink{}), WithDialer(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if _, ok := a.upstream.(*provider.Client); !ok {
		t.Errorf("Upstream = %T under WithDialer(nil), want the default provider client", a.upstream)
	}
	if a.dial == nil {
		t.Error("the Agent holds no Dialer; its next switch or routed spawn would panic")
	}
}
