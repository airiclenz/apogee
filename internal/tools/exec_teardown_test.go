package tools

import "testing"

// TestPlanTreeKill covers the one §2.4 teardown decision both backends share, on every OS:
// what a cancelled run must kill. The syscalls differ per platform; this table does not.
func TestPlanTreeKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		started  bool
		treeHeld bool
		want     treeKillAction
	}{
		{
			name:    "a process that never started is not killed at all",
			started: false, treeHeld: false,
			want: treeKillNothing,
		},
		{
			name:    "a container held before the process started is still nothing to kill",
			started: false, treeHeld: true,
			want: treeKillNothing,
		},
		{
			name:    "a held tree is reaped whole — the contract's intent",
			started: true, treeHeld: true,
			want: treeKillTree,
		},
		{
			name:    "an unheld tree degrades to the leader rather than nothing",
			started: true, treeHeld: false,
			want: treeKillLeader,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := planTreeKill(tt.started, tt.treeHeld); got != tt.want {
				t.Errorf("planTreeKill(%v, %v) = %v, want %v", tt.started, tt.treeHeld, got, tt.want)
			}
		})
	}
}

// TestNoTeardownIsInert pins the POSIX implementation: the process group needs no post-start
// step and owns no handle, so both hooks must be safe to call — including with a cmd that
// never started.
func TestNoTeardownIsInert(t *testing.T) {
	t.Parallel()

	var td processTeardown = noTeardown{}
	td.contain(nil)
	td.release()
	td.release()
}
