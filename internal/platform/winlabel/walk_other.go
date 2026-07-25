//go:build !windows

package winlabel

import (
	"errors"
	"fmt"
)

// The non-Windows half of the OS seam. No production path on a Linux or macOS host ever
// reaches any of it — package platform selects the landlock or seatbelt backend there and
// never constructs a Journal — but the declarations must exist so the package's DECISION half
// (the journal, the fold rules, the retention and revert splits, the notice wording) compiles
// and is table-testable on the machines it is actually developed on, exactly as
// confiner_other.go and seatbelt_notdarwin_test.go do for their own backends.
//
// Each stub reports unsupported rather than succeeding quietly. A stub that returned nil would
// let a future Linux test pass over a label that was never written, which is the one way this
// file could turn into a lie.

// errNoLabelFacility is what every stub here reports: the OS has no mandatory-label facility,
// which is an errors.ErrUnsupported condition and not a failure of this host's configuration.
var errNoLabelFacility = fmt.Errorf("%w: no mandatory label facility on this OS", errors.ErrUnsupported)

// LabelTree is the non-Windows stub of the label walk. See the Windows build for the real
// walk and for the journal-before-label invariant it owns.
func LabelTree(_ string, _ *Journal) error { return errNoLabelFacility }

// ClearTree is the non-Windows stub of the teardown walk.
func ClearTree(_ string) error { return errNoLabelFacility }

// Recover is the non-Windows stub of crash recovery: there are no labels on this host's disk
// to finish reverting, so it does nothing at all.
func Recover(_ string) {}

// ProcessAlive is the non-Windows stub of the liveness probe. It reports false, the answer
// that spares nothing — the safe direction, since a spared root leaves a label in place and
// nothing here can have labelled one.
func ProcessAlive(_ int) bool { return false }

// ReadSDDL is the non-Windows stub of the mandatory-label read.
func ReadSDDL(_ string) (string, error) { return "", errNoLabelFacility }

// SetSDDL is the non-Windows stub of the mandatory-label write.
func SetSDDL(_, _ string) error { return errNoLabelFacility }

// osRevert supplies NO revert off Windows: nothing on this host labels anything, so a journal
// has nothing to put back. Journal.Retire is declared once, unguarded, and treats the nil seam
// as "no disk mutation to undo" — one method, one doc comment, one contract, instead of two
// build-tagged copies that can silently diverge.
func osRevert() revertFunc { return nil }
