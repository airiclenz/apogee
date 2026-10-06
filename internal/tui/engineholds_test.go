package tui

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// engineAnswers is one row's answers to every question the engine hold set is asked.
type engineAnswers struct {
	runsIdleOnly    bool // commandRunnable for an idle-only verb (/clear)
	runsReport      bool // commandRunnable for a reporting verb (/version)
	runsDeferred    bool
	rebinds         bool
	beatCounts      bool
	editsConfig     bool
	launchesBg      bool
	isQuiescent     bool
	resumesWorkflow bool
	wakes           bool
}

// atRest is the answer set of a Model nothing holds: every question says yes.
var atRest = engineAnswers{
	runsIdleOnly: true, runsReport: true, runsDeferred: true, rebinds: true, beatCounts: true,
	editsConfig: true, launchesBg: true, isQuiescent: true, resumesWorkflow: true, wakes: true,
}

// TestEngineHolds_Questions pins the holds × questions table: each row puts the Model under one
// hold (or one non-hold fact, or an overlap), and every named question on the snapshot
// [Model.engineHolds] takes must answer as the row says — and, where the Model already asks that
// question itself, exactly as the Model's own gate does today.
func TestEngineHolds_Questions(t *testing.T) {
	t.Parallel()

	idleOnly := parseInput("/clear", nil)
	report := parseInput("/version", nil)
	tests := []struct {
		name  string
		setup func(*Model)
		want  engineAnswers
	}{
		{
			name:  "at rest",
			setup: func(*Model) {},
			want:  atRest,
		},
		{
			name:  "worker running",
			setup: func(m *Model) { m.state = stateRunning },
			want:  engineAnswers{runsReport: true},
		},
		{
			name:  "worker blocked on an approval",
			setup: func(m *Model) { m.state = stateAwaitingApproval },
			want:  engineAnswers{runsReport: true},
		},
		{
			name:  "errored counts idle for quiescent, not for wake or resume",
			setup: func(m *Model) { m.state = stateErrored },
			want:  atRestExcept(func(a *engineAnswers) { a.resumesWorkflow, a.wakes = false, false }),
		},
		{
			name:  "actuation in flight",
			setup: func(m *Model) { m.actuation.inFlight = true },
			want: atRestExcept(func(a *engineAnswers) {
				a.rebinds, a.beatCounts, a.editsConfig, a.isQuiescent = false, false, false, false
			}),
		},
		{
			name:  "bg launch is quiescent",
			setup: func(m *Model) { m.holds.hold(holdBgLaunch) },
			want: atRestExcept(func(a *engineAnswers) {
				a.runsIdleOnly, a.runsDeferred, a.rebinds, a.launchesBg = false, false, false, false
				a.resumesWorkflow, a.wakes = false, false
			}),
		},
		{
			name:  "session loading",
			setup: func(m *Model) { m.holds.hold(holdSessionLoad) },
			want:  atRestExcept(func(a *engineAnswers) { a.resumesWorkflow, a.wakes = false, false }),
		},
		{
			name:  "record write in flight",
			setup: func(m *Model) { m.writeBusy = true },
			want:  atRestExcept(func(a *engineAnswers) { a.resumesWorkflow, a.wakes = false, false }),
		},
		{
			name:  "record write queued",
			setup: func(m *Model) { m.pendingWrites = []recordWrite{{}} },
			want:  atRestExcept(func(a *engineAnswers) { a.resumesWorkflow, a.wakes = false, false }),
		},
		{
			name:  "quitting",
			setup: func(m *Model) { m.quitting = true },
			want: atRestExcept(func(a *engineAnswers) {
				a.runsDeferred, a.resumesWorkflow, a.wakes = false, false, false
			}),
		},
		{
			name:  "prebound",
			setup: func(m *Model) { m.opts.Prebound.Reason = domain.PreboundFirstBoot },
			want:  atRestExcept(func(a *engineAnswers) { a.resumesWorkflow, a.wakes = false, false }),
		},
		{
			name:  "interjection held",
			setup: func(m *Model) { m.pendingInterjections = []queuedInterjection{{}} },
			want:  atRestExcept(func(a *engineAnswers) { a.isQuiescent, a.wakes = false, false }),
		},
		{
			name:  "command queued",
			setup: func(m *Model) { m.deferredCommands = []parsedInput{idleOnly} },
			want:  atRestExcept(func(a *engineAnswers) { a.isQuiescent = false }),
		},
		{
			name: "boundary confirm open",
			setup: func(m *Model) {
				m.picker = picker{open: true, kind: pickerWorkflowBoundary}
			},
			want: atRestExcept(func(a *engineAnswers) { a.runsDeferred, a.wakes = false, false }),
		},
		{
			name:  "modal pane open",
			setup: func(m *Model) { m.sessionBrowser.open = true },
			want:  atRestExcept(func(a *engineAnswers) { a.wakes = false }),
		},
		{
			name: "worker and actuation at once",
			setup: func(m *Model) {
				m.state = stateRunning
				m.actuation.inFlight = true
			},
			want: engineAnswers{runsReport: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m Model
			tt.setup(&m)

			holds := m.engineHolds()
			got := engineAnswers{
				runsIdleOnly:    holds.commandRunnable(idleOnly),
				runsReport:      holds.commandRunnable(report),
				runsDeferred:    holds.canRunDeferred(),
				rebinds:         holds.canRebind(),
				beatCounts:      holds.beatMayCount(),
				editsConfig:     holds.canEditConfigExternally(),
				launchesBg:      holds.canLaunchBg(),
				isQuiescent:     holds.quiescent(),
				resumesWorkflow: holds.canResumeWorkflows(),
				wakes:           holds.canWake(),
			}
			today := engineAnswers{
				runsIdleOnly:    m.commandRunnable(idleOnly),
				runsReport:      m.commandRunnable(report),
				isQuiescent:     m.quiescent(),
				resumesWorkflow: m.canResumeWorkflows(),
				wakes:           m.canWake(),
			}

			if got != tt.want {
				t.Errorf("answers = %+v, want %+v", got, tt.want)
			}
			if mine := pickModelGates(got); mine != today {
				t.Errorf("hold-set answers %+v disagree with the Model's own gates %+v", mine, today)
			}
		})
	}
}

// atRestExcept returns the at-rest answers with edit applied — a row stated as the few answers its
// hold turns to no.
func atRestExcept(edit func(*engineAnswers)) engineAnswers {
	answers := atRest
	edit(&answers)
	return answers
}

// pickModelGates keeps only the answers the Model already gives through its own methods, so the
// hold-set answers can be compared with today's gates field for field.
func pickModelGates(a engineAnswers) engineAnswers {
	return engineAnswers{
		runsIdleOnly:    a.runsIdleOnly,
		runsReport:      a.runsReport,
		isQuiescent:     a.isQuiescent,
		resumesWorkflow: a.resumesWorkflow,
		wakes:           a.wakes,
	}
}

// ----------------------------------------------------------------------------
// The release transition
// ----------------------------------------------------------------------------

// releaseRebind is the observation every releaseEngine test stashes under a hold and expects bound
// at the release that leaves no hold standing.
var releaseRebind = rebindCall{model: "other-model", window: 16384}

// stashRebind folds a beat advertising releaseRebind into a Model some hold keeps from rebinding, so
// observeBinding stashes it rather than applying it.
func stashRebind(t *testing.T, m Model, rb *fakeRebind) Model {
	t.Helper()
	m = foldBeatMsg(t, m, upBeat(releaseRebind.model, releaseRebind.window))
	if len(rb.calls) != 0 || m.hb.pendingRebind == nil {
		t.Fatalf("rebind calls %+v, pending %+v; want the observation stashed under the hold", rb.calls, m.hb.pendingRebind)
	}
	return m
}

// wantRebound asserts the stash was applied exactly once and cleared.
func wantRebound(t *testing.T, m Model, rb *fakeRebind) {
	t.Helper()
	if want := []rebindCall{releaseRebind}; !reflect.DeepEqual(rb.calls, want) {
		t.Errorf("rebind calls = %+v, want the stash applied exactly once (%+v)", rb.calls, want)
	}
	if m.hb.pendingRebind != nil {
		t.Errorf("pendingRebind = %+v, want it cleared by the apply", m.hb.pendingRebind)
	}
}

// wantWithheld asserts the stash still stands with nothing driven into the engine.
func wantWithheld(t *testing.T, m Model, rb *fakeRebind) {
	t.Helper()
	if len(rb.calls) != 0 {
		t.Errorf("rebind calls = %+v, want none while another hold still stands", rb.calls)
	}
	if want := (rebindIntent{model: releaseRebind.model, window: releaseRebind.window}); m.hb.pendingRebind == nil || !reflect.DeepEqual(*m.hb.pendingRebind, want) {
		t.Errorf("pendingRebind = %+v, want the observation still stashed (%+v)", m.hb.pendingRebind, want)
	}
}

// An Exchange's end applies a rebind stashed under its worker, and withholds it while a /bg launch
// still reads the Agent off the loop.
func TestReleaseEngine_FinishWorker(t *testing.T) {
	t.Parallel()
	t.Run("alone", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		startStubWorker(t, &m)
		m = stashRebind(t, m, rb)

		m = step(t, m, exchangeDoneMsg{})

		wantRebound(t, m, rb)
	})
	t.Run("under a /bg launch", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m.holds.hold(holdBgLaunch)
		startStubWorker(t, &m)
		m = stashRebind(t, m, rb)

		m = step(t, m, exchangeDoneMsg{})

		wantWithheld(t, m, rb)
	})
}

// A launcher verb's completion applies a rebind stashed under its latch, and withholds it while a
// /bg launch still reads the Agent off the loop.
func TestReleaseEngine_FoldActuationDone(t *testing.T) {
	t.Parallel()
	t.Run("alone", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m, cmd := startLoad(t, m, "alpha")
		m = stashRebind(t, m, rb)

		m, _ = driveActuation(t, m, cmd)

		wantRebound(t, m, rb)
	})
	t.Run("under a /bg launch", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m, cmd := startLoad(t, m, "alpha")
		m.holds.hold(holdBgLaunch)
		m = stashRebind(t, m, rb)

		m, _ = driveActuation(t, m, cmd)

		wantWithheld(t, m, rb)
	})
}

// A /bg launch landing applies a rebind stashed under it, and withholds it while a worker still drives
// the engine. A typed message no longer opens one under the launch (submit refuses it), so the worker
// is forced here to pin releaseEngine's own deferral.
func TestReleaseEngine_FoldBgStarted(t *testing.T) {
	t.Parallel()
	t.Run("alone", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m.holds.hold(holdBgLaunch)
		m = stashRebind(t, m, rb)

		m = step(t, m, bgStartedMsg{id: testWorkflowID})

		wantRebound(t, m, rb)
	})
	t.Run("under a worker", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m.holds.hold(holdBgLaunch)
		startStubWorker(t, &m)
		m = stashRebind(t, m, rb)

		m = step(t, m, bgStartedMsg{id: testWorkflowID})

		wantWithheld(t, m, rb)
	})
}

// A background prompt's pane closing applies a rebind stashed while it stood, and withholds it while
// a /bg launch still reads the Agent off the loop.
func TestReleaseEngine_CloseWorkflowPrompt(t *testing.T) {
	t.Parallel()
	t.Run("alone", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m, _ = m.openWorkflowPrompt(bgApproval(7))
		m = stashRebind(t, m, rb)

		m, _ = m.dismissWorkflowPrompt()

		wantRebound(t, m, rb)
	})
	t.Run("under a /bg launch", func(t *testing.T) {
		t.Parallel()
		m, rb := wireLauncher(t, newLauncher())
		m.holds.hold(holdBgLaunch)
		m, _ = m.openWorkflowPrompt(bgApproval(7))
		m = stashRebind(t, m, rb)

		m, _ = m.dismissWorkflowPrompt()

		wantWithheld(t, m, rb)
	})
}

// A worker and an actuation at once — a wake can run during a launcher verb — with the worker's
// Exchange ending first: the rebind waits out the actuation and lands at its completion.
func TestReleaseEngine_WorkerThenActuation(t *testing.T) {
	t.Parallel()
	m, rb := wireLauncher(t, newLauncher())
	m, cmd := startLoad(t, m, "alpha")
	startStubWorker(t, &m)
	m = stashRebind(t, m, rb)

	m = step(t, m, exchangeDoneMsg{})
	wantWithheld(t, m, rb)

	m, _ = driveActuation(t, m, cmd)
	wantRebound(t, m, rb)
}

// The same overlap with the actuation completing first: the rebind waits out the worker and lands at
// its Exchange's end.
func TestReleaseEngine_ActuationThenWorker(t *testing.T) {
	t.Parallel()
	m, rb := wireLauncher(t, newLauncher())
	m, cmd := startLoad(t, m, "alpha")
	startStubWorker(t, &m)
	m = stashRebind(t, m, rb)

	m, _ = driveActuation(t, m, cmd)
	wantWithheld(t, m, rb)

	m = step(t, m, exchangeDoneMsg{})
	wantRebound(t, m, rb)
}
