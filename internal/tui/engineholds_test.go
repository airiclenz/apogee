package tui

import (
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
			setup: func(m *Model) { m.bgLaunching = true },
			want: atRestExcept(func(a *engineAnswers) {
				a.runsIdleOnly, a.runsDeferred, a.rebinds, a.launchesBg = false, false, false, false
				a.resumesWorkflow, a.wakes = false, false
			}),
		},
		{
			name:  "session loading",
			setup: func(m *Model) { m.sessionLoading = true },
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
