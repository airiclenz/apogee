package agent

// Coverage for Agent.SwitchUpstream (ADR 0024) — the engine half of a server switch. It proves
// the four properties the host's /server verb rests on: the wire target moves as ONE fresh
// provider client (endpoint and key together), the model is left UNBOUND so the new server's
// first observed model binds through the ordinary Rebind, the two token bounds the new server
// pins move WITH it (ADR 0045's window, ADR 0046's reply cap), and everything that is session
// state rather than a per-server fact survives the move. The white-box package placement is what
// lets these inject a fake Responder through newAgent and read the swapped bindings directly.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// modelBindingResponder is echoResponder plus the optional SetModel binder Rebind reaches for,
// and a count of the requests it answered — so a test can prove a Rebind and an Exchange AFTER a
// switch never reach the responder the switch retired.
type modelBindingResponder struct {
	reply    string
	bound    []string
	requests int
}

func (r *modelBindingResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	r.requests++
	return streamReply(r.reply)
}

func (r *modelBindingResponder) SetModel(model string) { r.bound = append(r.bound, model) }

// recordedRequest is what the fake Upstream below noted about one chat request: the model id on
// the wire and the Authorization header — the two facts a switch has to have moved together.
type recordedRequest struct {
	model string
	auth  string
}

// recordedUpstream is an OpenAI-compatible httptest server that records every chat request and
// answers with one canned SSE reply. Its mutex is real: the handler runs on net/http's goroutine
// while the test reads from its own.
type recordedUpstream struct {
	*httptest.Server

	mu   sync.Mutex
	seen []recordedRequest
}

func newRecordedUpstream(t *testing.T, reply string) *recordedUpstream {
	t.Helper()
	up := &recordedUpstream{}
	up.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		up.mu.Lock()
		up.seen = append(up.seen, recordedRequest{model: body.Model, auth: r.Header.Get("Authorization")})
		up.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", reply)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(up.Close)
	return up
}

func (u *recordedUpstream) requests() []recordedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.seen)
}

// TestSwitchUpstreamUnbindsTheModelAndKeepsTheSession: a switch moves the endpoint and the key,
// leaves NO model bound (Submit refuses with errNoModelBound until the new server's first
// Rebind), resets the per-model estimator and the compaction latch, and costs the user nothing
// of their conversation — the same "session state is not a per-server fact" posture Rebind takes.
func TestSwitchUpstreamUnbindsTheModelAndKeepsTheSession(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.APIKey = "old-key"
	responder := &captureAllResponder{scripts: [][]provider.Delta{contentScript("kept")}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "remember this")
	before := a.conv.Messages()
	estimator := a.tokens
	a.compactSat = true    // the latch a switch must clear: it was judged against the old window
	a.compactFailed = true // and the stand-down latch beside it: that fold faulted on the old server

	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://elsewhere.invalid:9999", APIKey: "new-key"}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}

	if a.cfg.Endpoint != "http://elsewhere.invalid:9999" {
		t.Errorf("endpoint = %q after the switch, want the new one", a.cfg.Endpoint)
	}
	if a.cfg.APIKey != "new-key" {
		t.Errorf("api key = %q after the switch, want the new server's", a.cfg.APIKey)
	}
	if a.cfg.Model != "" {
		t.Errorf("model = %q after the switch, want it UNBOUND", a.cfg.Model)
	}
	if err := a.Submit(domain.UserInput{Text: "too early"}); !errors.Is(err, errNoModelBound) {
		t.Errorf("Submit in the unbound gap err = %v, want errNoModelBound", err)
	}
	if a.tokens == estimator {
		t.Error("the token estimator survived the switch; its calibration described the old model")
	}
	if a.compactSat {
		t.Error("the compaction saturation latch survived the switch")
	}
	if a.compactFailed {
		t.Error("the compaction stand-down latch survived the switch; it judged the retired server")
	}

	after := a.conv.Messages()
	if len(after) != len(before) {
		t.Fatalf("conversation length = %d after the switch, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].Role != before[i].Role || after[i].Content != before[i].Content {
			t.Errorf("message %d = %+v after the switch, want %+v", i, after[i], before[i])
		}
	}
}

// TestSwitchUpstreamSwapsTheProviderClient: the switch really re-points the wire. After it, a
// Rebind binds the new server's model on a NEW client — the request lands on the new endpoint
// carrying the new model id and the new key — while the retired responder sees neither the
// SetModel nor the request. This is the "a new Client, not a mutated one" contract in action.
func TestSwitchUpstreamSwapsTheProviderClient(t *testing.T) {
	upstream := newRecordedUpstream(t, "from the new server")

	cfg := baseConfig(&recordingSink{})
	cfg.APIKey = "old-key"
	retired := &modelBindingResponder{reply: "from the old server"}

	a, err := newAgent(cfg, retired)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "before the switch")
	answered := retired.requests

	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: upstream.URL, APIKey: "new-key"}); err != nil {
		t.Fatalf("SwitchUpstream: %v", err)
	}
	// The new server's first observed model, applied through the ONE binding path.
	if err := a.Rebind(RebindSpec{Model: "new-model", MaxContextTokens: 16384}); err != nil {
		t.Fatalf("Rebind after the switch: %v", err)
	}
	runExchange(t, a, "after the switch")

	if len(retired.bound) != 0 {
		t.Errorf("the retired responder was rebound to %v; the client did not actually swap", retired.bound)
	}
	if retired.requests != answered {
		t.Errorf("the retired responder answered %d requests, want %d — the wire still points at it", retired.requests, answered)
	}

	got := upstream.requests()
	if len(got) != 1 {
		t.Fatalf("the new upstream saw %d requests, want 1", len(got))
	}
	if got[0].model != "new-model" {
		t.Errorf("new upstream request model = %q, want %q", got[0].model, "new-model")
	}
	if got[0].auth != "Bearer new-key" {
		t.Errorf("new upstream Authorization = %q, want the new server's key", got[0].auth)
	}
}

// TestSwitchUpstreamRefusedMidExchange: an Exchange left open by a cancel refuses the switch with
// ErrInputPending (the Rebind / ClearContext / RestoreSession idle-only class), and every binding
// — endpoint, key, model, and the client itself — stands.
func TestSwitchUpstreamRefusedMidExchange(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.APIKey = "old-key"
	responder := blockingResponder{started: make(chan struct{})}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "slow"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	if _, err := a.Step(ctx); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !a.InExchange() {
		t.Fatal("the cancelled Exchange is not open; the refusal below would prove nothing")
	}

	err = a.SwitchUpstream(UpstreamSpec{Endpoint: "http://elsewhere.invalid:9999", APIKey: "new-key"})
	if !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("SwitchUpstream mid-Exchange err = %v, want ErrInputPending", err)
	}
	assertUpstreamUnmoved(t, a, "old-key")
}

// TestSwitchUpstreamRefusesAnEmptyEndpoint: the construction requirement did not vanish, it moved
// — a spec naming no endpoint is refused with the same errMissingEndpoint sentinel, and nothing
// is committed.
func TestSwitchUpstreamRefusesAnEmptyEndpoint(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.APIKey = "old-key"

	a, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.SwitchUpstream(UpstreamSpec{APIKey: "new-key"}); !errors.Is(err, errMissingEndpoint) {
		t.Errorf("SwitchUpstream with no endpoint err = %v, want errMissingEndpoint", err)
	}
	assertUpstreamUnmoved(t, a, "old-key")
}

// TestSwitchUpstreamCarriesTheNewServersTokenBounds: a mid-session move takes the new server's own
// two bounds — the `context-window:` its entry pins (ADR 0045) and its `max-output-tokens:` (ADR
// 0046) — because both describe the SLOT rather than the conversation, and the retired server's
// numbers describe a machine this session no longer talks to.
//
// The three moves are the three states an entry can be in: both pinned, neither pinned, and a window
// with no cap — where the engine derives the cap from the reply room that very window reserves,
// which is the whole reason the window has to move first.
func TestSwitchUpstreamCarriesTheNewServersTokenBounds(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 8192
	cfg.Context.MaxOutputTokens = 2048

	a, err := newAgent(cfg, &modelBindingResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// Onto an entry that pins both: the Budget measures against the new window and the wire states
	// the new ceiling, neither of them the number the session launched with.
	if err := a.SwitchUpstream(UpstreamSpec{
		Endpoint: "http://big.invalid:9999", MaxContextTokens: 131072, MaxOutputTokens: 16384,
	}); err != nil {
		t.Fatalf("SwitchUpstream onto the pinned entry: %v", err)
	}
	if got := a.budget().ContextLimit; got != 131072 {
		t.Errorf("budget window = %d after the switch, want the new entry's pinned 131072", got)
	}
	if got := a.maxOutputTokens(); got != 16384 {
		t.Errorf("reply cap = %d after the switch, want the new entry's pinned 16384", got)
	}

	// Onto an entry that pins neither: the previous server's numbers do NOT follow. The window falls
	// to unknown until that server's first beat binds one — the state a session before its first beat
	// is in — and the cap falls to the clamp floor, which is what an unknown window's zero reserve
	// derives (never "unbounded", internal/context.Allocation).
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://bare.invalid:9999"}); err != nil {
		t.Fatalf("SwitchUpstream onto the unpinned entry: %v", err)
	}
	if got := a.budget().ContextLimit; got != 0 {
		t.Errorf("budget window = %d after the unpinned switch, want 0 — the retired pin must not follow", got)
	}
	if got := a.maxOutputTokens(); got != minOutputTokenCap {
		t.Errorf("reply cap = %d after the unpinned switch, want the derived floor %d", got, minOutputTokenCap)
	}

	// And onto an entry that pins only the window: the cap is DERIVED from that window's own reply
	// reserve (65,536 × 0.20 = 13,107, which sits between the two clamp ends), so the request and the
	// Budget cannot disagree about the room the reply has.
	if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://windowed.invalid:9999", MaxContextTokens: 65536}); err != nil {
		t.Fatalf("SwitchUpstream onto the window-only entry: %v", err)
	}
	reserve := a.budget().ResponseReserve
	if reserve <= minOutputTokenCap || reserve >= maxOutputTokenCap {
		t.Fatalf("the window-only reserve = %d; the case needs one strictly inside [%d, %d]",
			reserve, minOutputTokenCap, maxOutputTokenCap)
	}
	if got := a.maxOutputTokens(); got != reserve {
		t.Errorf("reply cap = %d after the window-only switch, want the new window's own reserve %d", got, reserve)
	}
}

// TestSwitchUpstreamCarriesTheNewServersResponseReserveShare: the third statement a move makes about
// the server it lands on — how that server's window is SPLIT for the reply, the share the caller
// resolved from the new entry's `response-reserve:` over the top-level key. It follows the two bounds
// above for their reason, and a spec stating none puts the split back to the engine's own built-in
// share rather than dividing the new server's window the retired server's way.
func TestSwitchUpstreamCarriesTheNewServersResponseReserveShare(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 8192

	a, err := newAgent(cfg, &modelBindingResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// Onto an entry stating half its window: the Budget holds exactly that back, whatever the run's
	// own top-level share says.
	if err := a.SwitchUpstream(UpstreamSpec{
		Endpoint: "http://halved.invalid:9999", MaxContextTokens: 100000, ResponseReserveFraction: 0.5,
	}); err != nil {
		t.Fatalf("SwitchUpstream onto the entry stating a share: %v", err)
	}
	if got := a.budget().ResponseReserve; got != 50000 {
		t.Errorf("reply reserve = %d after the switch, want the new entry's half of 100,000", got)
	}

	// Onto an entry stating none: the retired server's half does NOT follow — the engine's own
	// built-in fifth divides the window again, which is what "nobody said" means here.
	if err := a.SwitchUpstream(UpstreamSpec{
		Endpoint: "http://bare.invalid:9999", MaxContextTokens: 100000,
	}); err != nil {
		t.Fatalf("SwitchUpstream onto the entry stating none: %v", err)
	}
	if got := a.budget().ResponseReserve; got != 20000 {
		t.Errorf("reply reserve = %d after the unstated switch, want the built-in fifth of 100,000 — "+
			"the retired entry's share must not follow", got)
	}
}

// assertUpstreamUnmoved checks that a refused SwitchUpstream left every binding it would have
// committed exactly as baseConfig set it — including the injected fake Responder, which a
// committed switch would have replaced with a real provider client.
func assertUpstreamUnmoved(t *testing.T, a *Agent, apiKey string) {
	t.Helper()
	if a.cfg.Endpoint != "http://localhost:0" {
		t.Errorf("endpoint = %q after the refusal, want it unchanged", a.cfg.Endpoint)
	}
	if a.cfg.APIKey != apiKey {
		t.Errorf("api key = %q after the refusal, want %q", a.cfg.APIKey, apiKey)
	}
	if a.cfg.Model != "test-model" {
		t.Errorf("model = %q after the refusal, want it still bound", a.cfg.Model)
	}
	if _, swapped := a.upstream.(*provider.Client); swapped {
		t.Error("a refused SwitchUpstream still swapped in a real provider client")
	}
}

// TestSwitchUpstreamClosesTheClientItReplaces: a switch is the moment the retired client becomes
// unreachable, so the session tears it down — but only when it OWNS it. A session speaking over a
// client somebody else built (a shared one, ownsUpstream false) leaves it running, and the client
// the switch dials is owned in its turn, so the next switch retires it the same way.
func TestSwitchUpstreamClosesTheClientItReplaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		owns       bool
		wantCloses int
	}{
		{name: "owned client is torn down", owns: true, wantCloses: 1},
		{name: "shared client is left running", owns: false, wantCloses: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			retired := &closingResponder{}
			a, err := newAgent(baseConfig(&recordingSink{}), retired)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.ownsUpstream = tc.owns

			if err := a.SwitchUpstream(UpstreamSpec{Endpoint: "http://new.local:1234"}); err != nil {
				t.Fatalf("SwitchUpstream: %v", err)
			}
			if retired.closes != tc.wantCloses {
				t.Errorf("retired client closed %d times, want %d", retired.closes, tc.wantCloses)
			}
			if !a.ownsUpstream {
				t.Error("the session does not own the client the switch dialled — nothing would ever close it")
			}
			if _, ok := a.upstream.(*provider.Client); !ok {
				t.Errorf("Upstream = %T after the switch, want the fresh *provider.Client", a.upstream)
			}
		})
	}
}
