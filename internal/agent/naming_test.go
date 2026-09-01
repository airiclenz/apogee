package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// The out-of-band delegation namer (ADR 0068 — an unnamed delegation is named while it runs)
// ----------------------------------------------------------------------------
//
// A delegation the model named keeps that name: the namer is never even called for it. One the
// model left unnamed gets ONE completion on the child's own Upstream, concurrently with the run
// and bounded by its lifetime, and the name that comes back is announced as a SubAgentNamedEvent
// stamped with the CHILD's identity. Every failure is silent — an error, nothing usable in the
// reply, a reply that came back too late — and leaves the run wearing the task's first line.

// lockedSink captures every emitted Event under a lock, which the plain recordingSink does not
// need and this suite does: the naming goroutine emits CONCURRENTLY with the run it names, so the
// single-goroutine Agent contract recordingSink relies on does not hold here. It also closes
// renamed on the first SubAgentNamedEvent, which is how a test synchronises on "the rename has
// landed" without polling.
type lockedSink struct {
	mu      sync.Mutex
	events  []domain.Event
	once    sync.Once
	renamed chan struct{}
}

func newLockedSink() *lockedSink {
	return &lockedSink{renamed: make(chan struct{})}
}

func (s *lockedSink) Emit(e domain.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	if _, ok := e.(domain.SubAgentNamedEvent); ok {
		s.once.Do(func() { close(s.renamed) })
	}
}

// namings returns every rename the sink saw, in emission order.
func (s *lockedSink) namings() []domain.SubAgentNamedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var named []domain.SubAgentNamedEvent
	for _, e := range s.events {
		if ne, ok := e.(domain.SubAgentNamedEvent); ok {
			named = append(named, ne)
		}
	}
	return named
}

// awaitRename blocks until a rename has been emitted, or gives up. It is the gate a test hangs on
// the child's own goroutine when the claim is about what happens AFTER the rename; the timeout
// turns a namer that never fired into a failed assertion rather than a hung suite.
func (s *lockedSink) awaitRename() {
	select {
	case <-s.renamed:
	case <-time.After(5 * time.Second):
	}
}

// stubNamer is the host stand-in for the naming seam: it records every request it was handed and
// answers with a canned reply. block makes it wait for its context instead of answering at once —
// the "the reply came back after the run finished" case, which is exactly what a cancelled naming
// context means.
type stubNamer struct {
	mu    sync.Mutex
	seen  []domain.DelegationNaming
	reply string
	err   error
	block bool
}

func (n *stubNamer) NameDelegation(ctx context.Context, req domain.DelegationNaming) (string, error) {
	n.mu.Lock()
	n.seen = append(n.seen, req)
	n.mu.Unlock()
	if n.block {
		<-ctx.Done()
	}
	return n.reply, n.err
}

func (n *stubNamer) calls() []domain.DelegationNaming {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]domain.DelegationNaming(nil), n.seen...)
}

// namingParent builds a parent wired with the sub_agent tool and the given namer, scripted to
// delegate namingChildTask under the optional name and then finish. The child answers once and
// stops; gate runs on the CHILD's goroutine before that answer streams, which is where a test that
// asserts the rename ITSELF holds the run open until the name has landed. A run this short would
// otherwise finish first and see its own name dropped as late, which is the contract working
// rather than a bug: naming is bounded by the lifetime of the thing it names.
func namingParent(t *testing.T, sink domain.EventSink, namer domain.DelegationNamer, name string, gate func(context.Context)) *Agent {
	t.Helper()
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	if namer != nil {
		cfg.Namer = namer
	}
	a, err := newAgent(cfg, newRoutedResponder().
		route(namingParentInput, nil, namedDelegationScript("c1", namingChildTask, name)).
		route(namingChildTask, gate, contentScript("child done")).
		route(namingParentInput, nil, contentScript("parent done")))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

const (
	namingParentInput = "delegate the audit"
	namingChildTask   = "audit the config loader"
)

// runNamingParent drives one delegation to its end and fails the test on anything but a clean
// Exchange, so every assertion below is about naming rather than about the run.
func runNamingParent(t *testing.T, a *Agent) {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: namingParentInput}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}
}

// TestDelegationNaming_FiresOnlyForAnUnnamedChild is the trigger and the silence in one table: the
// namer is asked exactly once for a delegation the model left unnamed and never for one it named,
// and only a reply with something usable in it produces the announcement. Every other outcome —
// an error, a reply that sanitises to nothing, no namer at all — leaves the stream exactly as it
// was before naming existed.
func TestDelegationNaming_FiresOnlyForAnUnnamedChild(t *testing.T) {
	tests := []struct {
		label     string
		given     string
		namer     *stubNamer
		wantCalls int
		wantName  string
	}{
		{
			label:     "the model named it — the namer is never asked",
			given:     "repo-scout",
			namer:     &stubNamer{reply: "Generated Name"},
			wantCalls: 0,
		},
		{
			label:     "unnamed — one call, and the sanitised reply is announced",
			given:     "",
			namer:     &stubNamer{reply: "  Config Loader Audit.  "},
			wantCalls: 1,
			wantName:  "Config Loader Audit",
		},
		{
			label:     "the namer failed — silent, no event",
			given:     "",
			namer:     &stubNamer{err: errors.New("the sub-agent server is down")},
			wantCalls: 1,
		},
		{
			label:     "nothing usable came back — silent, no event",
			given:     "",
			namer:     &stubNamer{reply: "   \n  "},
			wantCalls: 1,
		},
		{
			label: "no namer at all — nothing fires",
			given: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			sink := newLockedSink()
			var namer domain.DelegationNamer
			if tc.namer != nil {
				namer = tc.namer
			}
			// Only the row that EXPECTS a rename holds the child open for it. The rows that
			// expect none must not wait: they assert the namer was asked (which the join before
			// the child closes guarantees) and that nothing was announced.
			var gate func(context.Context)
			if tc.wantName != "" {
				gate = func(context.Context) { sink.awaitRename() }
			}
			runNamingParent(t, namingParent(t, sink, namer, tc.given, gate))

			if tc.namer != nil {
				calls := tc.namer.calls()
				if len(calls) != tc.wantCalls {
					t.Fatalf("the namer was asked %d times, want %d", len(calls), tc.wantCalls)
				}
				if tc.wantCalls > 0 {
					if calls[0].Task != namingChildTask {
						t.Errorf("DelegationNaming.Task = %q, want the delegated task %q", calls[0].Task, namingChildTask)
					}
					if calls[0].Routed {
						t.Errorf("DelegationNaming.Routed = true for an UNROUTED spawn — the child speaks over the parent's own Upstream")
					}
				}
			}

			named := sink.namings()
			if tc.wantName == "" {
				if len(named) != 0 {
					t.Fatalf("SubAgentNamedEvents = %d, want none — every naming failure is silent", len(named))
				}
				return
			}
			if len(named) != 1 {
				t.Fatalf("SubAgentNamedEvents = %d, want exactly one rename", len(named))
			}
			if named[0].Name != tc.wantName {
				t.Errorf("SubAgentNamedEvent.Name = %q, want the sanitised reply %q", named[0].Name, tc.wantName)
			}
			// The stamp is the CHILD's run identity, exactly as its lifecycle events carry: that
			// is what lets a reader rename one member of a fan-out without threading anything
			// through.
			if named[0].Depth != 1 {
				t.Errorf("SubAgentNamedEvent.Depth = %d, want 1 — the run being renamed is one level down", named[0].Depth)
			}
			if named[0].CallID != "c1" {
				t.Errorf("SubAgentNamedEvent.CallID = %q, want the spawning call's id %q", named[0].CallID, "c1")
			}
		})
	}
}

// TestDelegationNaming_TheRunningChildWearsTheNewName is the other half of the announcement: the
// event says what the run is now called, and the run itself must AGREE — the child registered
// under the spawning call id reports the generated name from the moment the event fires, while it
// is still running. Asserted from the child's own goroutine, which is the only place a running
// delegation can be observed.
func TestDelegationNaming_TheRunningChildWearsTheNewName(t *testing.T) {
	sink := newLockedSink()
	namer := &stubNamer{reply: "Config Loader Audit"}

	var (
		parent *Agent
		seen   string
		found  bool
	)
	// The gate runs on the CHILD's goroutine before its second reply streams, so by then the child
	// is registered and — once the rename has landed — must already answer to the new name.
	gate := func(context.Context) {
		sink.awaitRename()
		if child, ok := parent.children.lookup("c1"); ok {
			found, seen = true, child.displayName()
		}
	}

	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "touch_thing", result: "touched"})
	cfg.Namer = namer
	up := newRoutedResponder().
		route(namingParentInput, nil, namedDelegationScript("c1", namingChildTask, "")).
		route(namingChildTask, nil, toolCallScript("t1", "touch_thing", `{}`)).
		route(namingChildTask, gate, contentScript("child done")).
		route(namingParentInput, nil, contentScript("parent done"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	parent = a
	runNamingParent(t, a)

	if !found {
		t.Fatal("the child was not registered when its second Turn ran — the rename could not be observed")
	}
	if seen != "Config Loader Audit" {
		t.Errorf("the running child's displayName() = %q, want the generated name %q", seen, "Config Loader Audit")
	}
}

// TestDelegationNaming_ALateReplyIsDropped pins the lifetime bound: a namer that answers only once
// its context is cancelled is answering about a run that has already been read and reported under
// the name it wore, so its name is thrown away — no rename, no event, and no write to a child the
// delegation has finished closing.
func TestDelegationNaming_ALateReplyIsDropped(t *testing.T) {
	sink := newLockedSink()
	namer := &stubNamer{reply: "Too Late", block: true}

	runNamingParent(t, namingParent(t, sink, namer, "", nil))

	if calls := namer.calls(); len(calls) != 1 {
		t.Fatalf("the namer was asked %d times, want the child's one call", len(calls))
	}
	if named := sink.namings(); len(named) != 0 {
		t.Fatalf("SubAgentNamedEvents = %d, want none — a name that arrives after the run is dropped", len(named))
	}
}

// TestDelegationNaming_ARoutedChildIsNamedAsRouted pins the one fact the engine states about WHERE
// the naming call belongs: a child that dialled the Sub-agent server itself is reported as Routed,
// so the host puts the naming completion on the machine already warm for this run rather than on
// the orchestrator's.
func TestDelegationNaming_ARoutedChildIsNamedAsRouted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"child done\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	sink := newLockedSink()
	namer := &stubNamer{reply: "Config Loader Audit"}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.Namer = namer
	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
		namedDelegationScript("c1", namingChildTask, ""),
		contentScript("parent done"),
	}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// The routed spawn's whole difference: the child builds its own client on the target and owns
	// it (routedspawn_test.go's routingParent shape), which is the fact Routed reports.
	target := routedTarget()
	target.Endpoint, target.APIKey = srv.URL, ""
	a.SetDelegationTarget(target)

	runNamingParent(t, a)

	calls := namer.calls()
	if len(calls) != 1 {
		t.Fatalf("the namer was asked %d times, want the child's one call", len(calls))
	}
	if !calls[0].Routed {
		t.Error("DelegationNaming.Routed = false for a ROUTED spawn — the child dialled the Sub-agent server itself")
	}
	if calls[0].Task != namingChildTask {
		t.Errorf("DelegationNaming.Task = %q, want the delegated task %q", calls[0].Task, namingChildTask)
	}
}
