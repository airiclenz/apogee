package workflow

import (
	"errors"
	"testing"
)

func TestEndingOf_ReadsHowARunEnded(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage merge: disk full")
	earlyErr := errors.New("invalid plan")
	cases := []struct {
		name   string
		result Result
		err    error
		want   RunEnding
	}{
		{"a run that ran to done", Result{ID: "wf-1", Phase: PhaseDone}, nil, RunEnding{Phase: PhaseDone}},
		{"a run a cancel stopped", Result{ID: "wf-1", Phase: PhaseStopped}, nil, RunEnding{Phase: PhaseStopped}},
		{"a run a stage error ended", Result{ID: "wf-1", Phase: PhaseFailed}, stageErr, RunEnding{Phase: PhaseFailed, Err: stageErr}},
		{"a run that failed before its folder opened", Result{}, earlyErr, RunEnding{Phase: PhaseFailed, Err: earlyErr}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := EndingOf(tt.result, tt.err)

			if got != tt.want {
				t.Errorf("EndingOf = %+v, want %+v", got, tt.want)
			}
			if got.NeverStarted {
				t.Error("NeverStarted = true, want false: Run took the run up")
			}
		})
	}
}

func TestUnstartedEnding_MarksARunThatNeverStarted(t *testing.T) {
	t.Parallel()

	settleErr := errors.New("status.json: permission denied")
	cases := []struct {
		name  string
		phase Phase
		err   error
	}{
		{"a queued first run, marked stopped", PhaseStopped, nil},
		{"a queued re-run, its done folder kept", PhaseDone, nil},
		{"a folder that could not be settled", "", settleErr},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := UnstartedEnding(tt.phase, tt.err)

			want := RunEnding{Phase: tt.phase, Err: tt.err, NeverStarted: true}
			if got != want {
				t.Errorf("UnstartedEnding = %+v, want %+v", got, want)
			}
		})
	}
}
