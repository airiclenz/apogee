package agent

// The session-started notice (ADR 0076 amendment of 2026-10-19): every top-level Session boundary
// announces exactly one start to the observe Runner — construction "new" (fresh and resumed starts
// alike), ClearContext "clear", RestoreSession "resume", or "fork" after MarkFork — a refused
// boundary and a delegate announce none, a Driver that defers the construction boundary announces it
// once, and a boundary over a wedged Runner still returns.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/reactions"
	"github.com/airiclenz/apogee/internal/testtime"
)

// startRecordingRunner is a Config.ObserveRunner that keeps every session-started boundary it is
// told of, in order, and every narrowed firing of newly armed entries (armedCall). Its swap door
// takes any list.
type startRecordingRunner struct {
	mu     sync.Mutex
	starts []domain.SessionStart
	armed  []armedCall
}

// armedCall is one SessionStartedFor call: the boundary's start and the entry ids it fired.
type armedCall struct {
	start domain.SessionStart
	ids   []string
}

func (r *startRecordingRunner) Replace([]domain.Reaction) error { return nil }

func (r *startRecordingRunner) SessionStarted(start domain.SessionStart) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, start)
}

func (r *startRecordingRunner) SessionStartedFor(start domain.SessionStart, ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.armed = append(r.armed, armedCall{start: start, ids: slices.Clone(ids)})
}

// armedCalls returns a copy of the narrowed firings made so far.
func (r *startRecordingRunner) armedCalls() []armedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.armed)
}

// recorded returns a copy of the starts announced so far.
func (r *startRecordingRunner) recorded() []domain.SessionStart {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.starts)
}

// emptySession is a restorable snapshot of an empty conversation.
func emptySession(t *testing.T) domain.Session {
	t.Helper()
	return cutFixtureSession(t, agentState{Conversation: domain.NewConversation(nil)})
}

// TestSessionStart_EachBoundaryAnnouncesItsStartOnce drives each boundary over a recording Runner
// and reads exactly one announcement per boundary, carrying the boundary's own start.
func TestSessionStart_EachBoundaryAnnouncesItsStartOnce(t *testing.T) {
	t.Parallel()

	construct := func(t *testing.T, cfg domain.Config) *Agent {
		t.Helper()
		a, err := New(cfg, WithDialer(dialerTo(echoResponder(t, "reply")).dial))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = a.Close() })
		return a
	}
	for _, tc := range []struct {
		name     string
		boundary func(t *testing.T, cfg domain.Config)
		want     []domain.SessionStart
	}{
		{"a fresh construction", func(t *testing.T, cfg domain.Config) { construct(t, cfg) },
			[]domain.SessionStart{domain.SessionStartNew}},
		{"a resumed construction", func(t *testing.T, cfg domain.Config) {
			a, err := Resume(cfg, emptySession(t), WithDialer(dialerTo(echoResponder(t, "reply")).dial))
			if err != nil {
				t.Fatalf("Resume: %v", err)
			}
			t.Cleanup(func() { _ = a.Close() })
		}, []domain.SessionStart{domain.SessionStartNew}},
		{"a clear", func(t *testing.T, cfg domain.Config) {
			if err := construct(t, cfg).ClearContext(); err != nil {
				t.Fatalf("ClearContext: %v", err)
			}
		}, []domain.SessionStart{domain.SessionStartNew, domain.SessionStartClear}},
		{"a live restore", func(t *testing.T, cfg domain.Config) {
			if err := construct(t, cfg).RestoreSession(emptySession(t)); err != nil {
				t.Fatalf("RestoreSession: %v", err)
			}
		}, []domain.SessionStart{domain.SessionStartNew, domain.SessionStartResume}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &startRecordingRunner{}
			cfg := baseConfig(&recordingSink{})
			cfg.ObserveRunner = runner

			tc.boundary(t, cfg)

			if got := runner.recorded(); !slices.Equal(got, tc.want) {
				t.Errorf("announced %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSessionStart_ARefusedBoundaryAnnouncesNothing — a restore of a snapshot newer than this build
// starts no session, so it announces none.
func TestSessionStart_ARefusedBoundaryAnnouncesNothing(t *testing.T) {
	t.Parallel()

	runner := &startRecordingRunner{}
	cfg := baseConfig(&recordingSink{})
	cfg.ObserveRunner = runner
	a, err := newAgent(cfg, echoResponder(t, "reply"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	future := emptySession(t)
	future.Version = domain.SessionVersion + 1
	if err := a.RestoreSession(future); !errors.Is(err, domain.ErrSessionVersion) {
		t.Fatalf("RestoreSession of a future snapshot = %v, want ErrSessionVersion", err)
	}

	if got := runner.recorded(); len(got) != 0 {
		t.Errorf("a refused restore announced %v, want nothing", got)
	}
}

// TestSessionStart_ADeferredConstructionIsTheDriversToAnnounce — under Config.DeferSessionStart New
// announces nothing, the Driver's AnnounceSessionStart announces "new" once, and a second call
// announces nothing more; without the deferral the Driver's call finds the boundary announced.
func TestSessionStart_ADeferredConstructionIsTheDriversToAnnounce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		deferred bool
	}{
		{"deferred to the Driver", true},
		{"announced by New", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &startRecordingRunner{}
			cfg := baseConfig(&recordingSink{})
			cfg.ObserveRunner = runner
			cfg.DeferSessionStart = tc.deferred
			a, err := New(cfg, WithDialer(dialerTo(echoResponder(t, "reply")).dial))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = a.Close() })
			if got := runner.recorded(); tc.deferred && len(got) != 0 {
				t.Fatalf("a deferred construction announced %v before the Driver did", got)
			}

			a.AnnounceSessionStart()
			a.AnnounceSessionStart()

			if got, want := runner.recorded(), []domain.SessionStart{domain.SessionStartNew}; !slices.Equal(got, want) {
				t.Errorf("announced %v, want the construction boundary once: %v", got, want)
			}
		})
	}
}

// TestSessionStart_ADelegateAnnouncesNone — a delegation runs a child Agent, which starts no
// session: the only announcement over a whole delegating Exchange is the construction's.
func TestSessionStart_ADelegateAnnouncesNone(t *testing.T) {
	t.Parallel()

	runner := &startRecordingRunner{}
	cfg := subAgentConfig(&recordingSink{}, domain.ModeAskBefore)
	cfg.ObserveRunner = runner
	responder := scriptedResponder(t,
		subAgentCallTurn("c1", "summarise the repo"),
		contentTurn("the repo is a Go TUI agent"),
		contentTurn("done — delegated and summarised"),
	)
	a, err := New(cfg, WithDialer(dialerTo(responder).dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	runExchange(t, a, "please research")

	if got, want := runner.recorded(), []domain.SessionStart{domain.SessionStartNew}; !slices.Equal(got, want) {
		t.Errorf("announced %v over a delegating Exchange, want only the construction's %v", got, want)
	}
}

// gatedObserveExec holds every observe firing until release is closed (or its context ends).
type gatedObserveExec struct {
	release chan struct{}
}

func (e gatedObserveExec) Run(ctx context.Context, _ domain.Reaction, _ domain.SeamPayload) error {
	select {
	case <-e.release:
	case <-ctx.Done():
	}
	return nil
}

// TestSessionStart_ABoundaryOverAWedgedRunnerReturns — every TUI boundary runs on the Update
// goroutine, so a boundary must return whatever the Runner's session-started entry is doing: here
// the entry is wedged and its queue overflows, and every clear still returns at once.
func TestSessionStart_ABoundaryOverAWedgedRunnerReturns(t *testing.T) {
	t.Parallel()

	list := []domain.Reaction{{
		ID:      "primer",
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []domain.Moment{domain.MomentSessionStarted},
		Handler: domain.ArgvHandler{Argv: []string{"true"}},
		Timeout: time.Minute,
	}}
	exec := gatedObserveExec{release: make(chan struct{})}
	runner, err := reactions.New(list, reactions.Options{Workspace: t.TempDir(), Exec: exec})
	if err != nil {
		t.Fatalf("reactions.New: %v", err)
	}
	cfg := baseConfig(&recordingSink{})
	cfg.ObserveRunner = runner
	cfg.Observe = list
	a, err := New(cfg, WithDialer(dialerTo(echoResponder(t, "reply")).dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	const clears = 200 // well past the Runner's per-entry queue
	done := make(chan error, 1)
	go func() {
		for i := 0; i < clears; i++ {
			if err := a.ClearContext(); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ClearContext: %v", err)
		}
	case <-time.After(testtime.Budget(5 * time.Second)):
		t.Fatal("a clear did not return while the Runner's session-started entry was wedged")
	}

	close(exec.release)
	ctx, cancel := context.WithTimeout(context.Background(), testtime.Budget(5*time.Second))
	defer cancel()
	if err := runner.Close(ctx); err != nil {
		t.Fatalf("runner.Close: %v", err)
	}
}

// TestSessionStart_AForkRestoreAnnouncesFork — MarkFork makes the next boundary a fork: the restore
// it marks announces "fork", the boundary consumes the mark, so a later restore is a plain "resume",
// and a clear that meets the mark drops it rather than announcing a fork.
func TestSessionStart_AForkRestoreAnnouncesFork(t *testing.T) {
	t.Parallel()

	restore := func(t *testing.T, a *Agent) {
		t.Helper()
		if err := a.RestoreSession(emptySession(t)); err != nil {
			t.Fatalf("RestoreSession: %v", err)
		}
	}
	for _, tc := range []struct {
		name       string
		boundaries func(t *testing.T, a *Agent)
		want       []domain.SessionStart
	}{
		{"a marked restore, then a plain one", func(t *testing.T, a *Agent) {
			a.MarkFork()
			restore(t, a)
			restore(t, a)
		}, []domain.SessionStart{domain.SessionStartFork, domain.SessionStartResume}},
		{"a clear that meets the mark, then a restore", func(t *testing.T, a *Agent) {
			a.MarkFork()
			if err := a.ClearContext(); err != nil {
				t.Fatalf("ClearContext: %v", err)
			}
			restore(t, a)
		}, []domain.SessionStart{domain.SessionStartClear, domain.SessionStartResume}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &startRecordingRunner{}
			cfg := baseConfig(&recordingSink{})
			cfg.ObserveRunner = runner
			a, err := newAgent(cfg, echoResponder(t, "reply"))
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			t.Cleanup(func() { _ = a.Close() })

			tc.boundaries(t, a)

			if got := runner.recorded(); !slices.Equal(got, tc.want) {
				t.Errorf("announced %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSessionStartedResumeAfterRefusedForkRestore — a fork restore the engine refuses (a snapshot
// newer than this build) announces nothing and still consumes the mark, so the `/sessions` resume
// after it announces "resume", not "fork".
func TestSessionStartedResumeAfterRefusedForkRestore(t *testing.T) {
	t.Parallel()

	runner := &startRecordingRunner{}
	cfg := baseConfig(&recordingSink{})
	cfg.ObserveRunner = runner
	a, err := newAgent(cfg, echoResponder(t, "reply"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	future := emptySession(t)
	future.Version = domain.SessionVersion + 1
	a.MarkFork()
	if err := a.RestoreSession(future); !errors.Is(err, domain.ErrSessionVersion) {
		t.Fatalf("the fork's RestoreSession of a future snapshot = %v, want ErrSessionVersion", err)
	}
	if err := a.RestoreSession(emptySession(t)); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	if got, want := runner.recorded(), []domain.SessionStart{domain.SessionStartResume}; !slices.Equal(got, want) {
		t.Errorf("announced %v, want the refused fork to announce nothing and the resume after it %v", got, want)
	}
}
