package workflow

import (
	"testing"
	"time"
)

// settleClock is the moment the settle tests' Runner reads, apart from storeClock, which stamps the
// folder as it is created.
var settleClock = storeClock.Add(time.Hour)

// newSettleRunner returns a Runner over a fresh store whose clock reads settleClock, and the id of
// a workflow folder in it recorded in phase prior.
func newSettleRunner(t *testing.T, prior Phase) (*Runner, string) {
	t.Helper()
	runner := &Runner{Store: newTestStore(t), Now: func() time.Time { return settleClock }}
	status, err := runner.Store.Create(samplePlan("audit"), "hash", storeClock)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	status.Phase = prior
	if err := runner.Store.WriteStatus(status); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	return runner, status.ID
}

func TestSettleUnstarted_RecordsStoppedUnlessTheFolderIsFinished(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		prior       Phase
		want        Phase
		wantUpdated time.Time
	}{
		{"a pending folder is stopped", PhasePending, PhaseStopped, settleClock},
		{"a running folder is stopped", PhaseRunning, PhaseStopped, settleClock},
		{"a stopped folder is stopped again", PhaseStopped, PhaseStopped, settleClock},
		{"a done folder is kept", PhaseDone, PhaseDone, storeClock},
		{"a failed folder is kept", PhaseFailed, PhaseFailed, storeClock},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner, id := newSettleRunner(t, tt.prior)

			got, err := runner.SettleUnstarted(id)

			if err != nil {
				t.Fatalf("SettleUnstarted: %v", err)
			}
			if got != tt.want {
				t.Errorf("returned phase = %q, want %q", got, tt.want)
			}
			status, err := runner.Store.ReadStatus(id)
			if err != nil {
				t.Fatalf("ReadStatus: %v", err)
			}
			if status.Phase != tt.want {
				t.Errorf("recorded phase = %q, want %q", status.Phase, tt.want)
			}
			if !status.Updated.Equal(tt.wantUpdated) {
				t.Errorf("updated = %v, want %v", status.Updated, tt.wantUpdated)
			}
		})
	}
}

func TestSettleUnstarted_AMissingFolderIsAnError(t *testing.T) {
	t.Parallel()
	runner := &Runner{Store: newTestStore(t), Now: func() time.Time { return settleClock }}

	got, err := runner.SettleUnstarted("2026-09-27-audit")

	if err == nil {
		t.Fatalf("SettleUnstarted of a missing folder = %q, nil; want an error", got)
	}
}

func TestSettleUnstarted_AFailedStartMarksOnlyAPendingFolder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		prior       Phase
		want        Phase
		wantUpdated time.Time
	}{
		{"a pending folder is failed", PhasePending, PhaseFailed, settleClock},
		{"a running folder is kept", PhaseRunning, PhaseRunning, storeClock},
		{"a stopped folder is kept", PhaseStopped, PhaseStopped, storeClock},
		{"a done folder is kept", PhaseDone, PhaseDone, storeClock},
		{"a failed folder is kept", PhaseFailed, PhaseFailed, storeClock},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner, id := newSettleRunner(t, tt.prior)

			got, err := runner.SettleFailedStart(id)

			if err != nil {
				t.Fatalf("SettleFailedStart: %v", err)
			}
			if got != tt.want {
				t.Errorf("returned phase = %q, want %q", got, tt.want)
			}
			status, err := runner.Store.ReadStatus(id)
			if err != nil {
				t.Fatalf("ReadStatus: %v", err)
			}
			if status.Phase != tt.want {
				t.Errorf("recorded phase = %q, want %q", status.Phase, tt.want)
			}
			if !status.Updated.Equal(tt.wantUpdated) {
				t.Errorf("updated = %v, want %v", status.Updated, tt.wantUpdated)
			}
		})
	}

	t.Run("a missing folder is an error", func(t *testing.T) {
		t.Parallel()
		runner := &Runner{Store: newTestStore(t), Now: func() time.Time { return settleClock }}
		if got, err := runner.SettleFailedStart("2026-09-27-audit"); err == nil {
			t.Fatalf("SettleFailedStart of a missing folder = %q, nil; want an error", got)
		}
	})
}
