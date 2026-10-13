package session

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// errRenameBusy stands in for the sharing violation a Windows reader causes: the error the fake
// predicate calls retryable.
var errRenameBusy = errors.New("destination held open")

// isRenameBusy is the fake retryable predicate: errRenameBusy, wrapped or bare, and nothing else.
func isRenameBusy(err error) bool { return errors.Is(err, errRenameBusy) }

// A retryable failure followed by success is retried, and the rename that lands returns nil after
// exactly two calls — the reader let go and the save went through.
func TestRenameWithRetryRetriesARetryableError(t *testing.T) {
	t.Parallel()
	calls := 0
	rename := func(from, to string) error {
		calls++
		if calls == 1 {
			return errRenameBusy
		}
		return nil
	}
	if err := renameWithRetry(rename, isRenameBusy, testtime.Budget(2*time.Second), "a.tmp", "a.json"); err != nil {
		t.Fatalf("renameWithRetry = %v, want nil once the second attempt succeeds", err)
	}
	if calls != 2 {
		t.Errorf("rename calls = %d, want 2", calls)
	}
}

// A retryable failure that never clears is retried until the budget is spent, and the error that
// surfaces is the LAST one the rename gave, not the first.
func TestRenameWithRetryReturnsTheLastErrorPastTheBudget(t *testing.T) {
	t.Parallel()
	calls := 0
	rename := func(from, to string) error {
		calls++
		return fmt.Errorf("attempt %d: %w", calls, errRenameBusy)
	}
	err := renameWithRetry(rename, isRenameBusy, testtime.Fixed(30*time.Millisecond), "a.tmp", "a.json")
	if err == nil {
		t.Fatal("renameWithRetry = nil, want the last retryable error once the budget is spent")
	}
	if calls < 2 {
		t.Fatalf("rename calls = %d, want the retryable error retried before the budget ran out", calls)
	}
	if want := fmt.Sprintf("attempt %d: %v", calls, errRenameBusy); err.Error() != want {
		t.Errorf("renameWithRetry = %q, want the last attempt's error %q", err, want)
	}
}

// An error the predicate does not call retryable surfaces at once: one call, that error.
func TestRenameWithRetryReturnsANonRetryableErrorAtOnce(t *testing.T) {
	t.Parallel()
	errFinal := errors.New("no such directory")
	calls := 0
	rename := func(from, to string) error {
		calls++
		return errFinal
	}
	if err := renameWithRetry(rename, isRenameBusy, testtime.Budget(2*time.Second), "a.tmp", "a.json"); !errors.Is(err, errFinal) {
		t.Fatalf("renameWithRetry = %v, want %v", err, errFinal)
	}
	if calls != 1 {
		t.Errorf("rename calls = %d, want 1", calls)
	}
}
