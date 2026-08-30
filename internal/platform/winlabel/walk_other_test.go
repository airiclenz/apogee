//go:build !windows

package winlabel

import (
	"errors"
	"os"
	"testing"
)

// The non-Windows stubs report UNSUPPORTED rather than succeeding quietly. This is the guard
// on the one way walk_other.go could turn into a lie: a stub that returned nil would let a
// Linux test — today's or a future one's — pass over a mandatory label that was never written,
// and the package's whole claim to be table-testable off Windows rests on the decisions being
// exercised while the OS calls are honestly absent.
func TestNonWindowsStubsReportUnsupported(t *testing.T) {
	t.Parallel()

	_, readErr := ReadSDDL(`C:\work`)
	tests := []struct {
		name string
		err  error
	}{
		{name: "LabelTree", err: LabelTree(`C:\work`, Open(""))},
		{name: "ClearTree", err: ClearTree(`C:\work`)},
		{name: "ReadSDDL", err: readErr},
		{name: "SetSDDL", err: SetSDDL(`C:\work`, lowSDDL)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err == nil {
				t.Fatalf("%s = nil on a host with no mandatory-label facility; a silent success would look like a label that landed", tt.name)
			}
			if !errors.Is(tt.err, errors.ErrUnsupported) {
				t.Errorf("%s = %v; want an errors.ErrUnsupported condition — the facility is absent, not misconfigured", tt.name, tt.err)
			}
		})
	}
}

// ProcessAlive spares nothing off Windows, which is the safe direction: a spared root leaves a
// label in place, and nothing on this host can have applied one.
func TestNonWindowsProcessAliveSparesNothing(t *testing.T) {
	t.Parallel()

	if ProcessAlive(os.Getpid()) {
		t.Error("ProcessAlive reported this very process alive off Windows; recovery would then spare roots forever")
	}
}

// Recover is a no-op rather than a stub that reports: it returns nothing, it is called from a
// constructor, and there are no labels on this host's disk to finish reverting.
func TestNonWindowsRecoverDoesNothing(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := JournalPath(home, 4242)
	if err := WriteJournal(path, Record{PID: 4242, Entries: []Entry{{Path: `C:\work`, Root: true}}}); err != nil {
		t.Fatalf("plant a journal: %v", err)
	}

	Recover(home)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("stat the planted journal after Recover: %v; nothing here may retire a journal it cannot revert", err)
	}
}

// Off Windows the journal's revert seam is EMPTY, and Retire is the same single unguarded
// method it is everywhere else. That pairing is the whole point of the seam: one Retire, one
// doc comment, one contract, instead of two build-tagged copies that can drift apart.
func TestNonWindowsJournalHasNoRevertSeam(t *testing.T) {
	t.Parallel()

	if osRevert() != nil {
		t.Error("osRevert returned a revert off Windows; there is no label walk here for it to drive")
	}
	if Open(t.TempDir()).revert != nil {
		t.Error("Open left a revert on the journal off Windows; Retire must see the empty seam")
	}
}
