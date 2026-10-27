package agent

import (
	"slices"
	"testing"
)

// TestHeldLedger_Operations walks the ledger through each operation from a known state and checks
// both halves after it: what is pending and what is delivered.
func TestHeldLedger_Operations(t *testing.T) {
	t.Parallel()
	never := func(string) bool { return false }
	always := func(string) bool { return true }
	only := func(keep ...string) func(string) bool {
		return func(entry string) bool { return slices.Contains(keep, entry) }
	}

	cases := []struct {
		name          string
		apply         func(l *heldLedger[string])
		wantPending   []string
		wantDelivered []string
	}{
		{
			name:        "the zero value is empty",
			apply:       func(*heldLedger[string]) {},
			wantPending: nil,
		},
		{
			name: "hold appends behind what is pending",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				l.hold("c")
			},
			wantPending: []string{"a", "b", "c"},
		},
		{
			name: "a take delivers only what the predicate accepts and drops the rest of the read set",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b", "c")
				l.take([]string{"a", "b", "c"}, only("a", "c"))
			},
			wantPending:   nil,
			wantDelivered: []string{"a", "c"},
		},
		{
			name: "a subset take leaves what lies outside the read set pending",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				read := l.pending()
				l.hold("c")
				l.take(read, always)
			},
			wantPending:   []string{"c"},
			wantDelivered: []string{"a", "b"},
		},
		{
			name: "a take that delivers nothing empties the read set out of the ledger",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				l.take(l.pending(), never)
			},
			wantPending:   nil,
			wantDelivered: nil,
		},
		{
			name: "takes deliver in the order they are taken",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				l.take([]string{"b"}, always)
				l.take([]string{"a"}, always)
			},
			wantDelivered: []string{"b", "a"},
		},
		{
			name: "putBack prepends ahead of what was held since",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				taken := l.pending()
				l.take(taken, never)
				l.hold("c")
				l.putBack(taken...)
			},
			wantPending: []string{"a", "b", "c"},
		},
		{
			name: "markDelivered delivers without touching pending",
			apply: func(l *heldLedger[string]) {
				l.hold("a")
				l.markDelivered("x", "y")
			},
			wantPending:   []string{"a"},
			wantDelivered: []string{"x", "y"},
		},
		{
			name: "restore puts delivered back ahead of pending",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				l.take(l.pending(), always)
				l.hold("c")
				l.markDelivered("d")
				l.restore()
			},
			wantPending: []string{"a", "b", "d", "c"},
		},
		{
			name: "forget drops delivered and keeps pending",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				l.take([]string{"a"}, always)
				l.forget()
			},
			wantPending: []string{"b"},
		},
		{
			name: "a restore after forget holds nothing again",
			apply: func(l *heldLedger[string]) {
				l.hold("a")
				l.take(l.pending(), always)
				l.forget()
				l.restore()
			},
			wantPending: nil,
		},
		{
			name: "reset empties both halves",
			apply: func(l *heldLedger[string]) {
				l.hold("a", "b")
				l.take([]string{"a"}, always)
				l.reset()
			},
			wantPending: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var l heldLedger[string]
			tc.apply(&l)
			if got := l.pending(); !slices.Equal(got, tc.wantPending) {
				t.Errorf("pending = %q, want %q", got, tc.wantPending)
			}
			if !slices.Equal(l.delivered, tc.wantDelivered) {
				t.Errorf("delivered = %q, want %q", l.delivered, tc.wantDelivered)
			}
		})
	}
}

// TestHeldLedger_PendingIsACopy — the snapshot pending returns is the caller's own: writing to it
// changes nothing held.
func TestHeldLedger_PendingIsACopy(t *testing.T) {
	t.Parallel()
	var l heldLedger[string]
	l.hold("a", "b")
	snapshot := l.pending()
	snapshot[0] = "changed"
	if got := l.pending(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("pending after writing to a snapshot = %q, want [a b]", got)
	}
}

// TestHeldLedger_PutBackKeepsTheCallersSlice — putBack copies the entries it prepends, so a later
// hold cannot write through into the slice the taker still holds.
func TestHeldLedger_PutBackKeepsTheCallersSlice(t *testing.T) {
	t.Parallel()
	var l heldLedger[string]
	taken := make([]string, 1, 4)
	taken[0] = "a"
	l.putBack(taken...)
	l.hold("b")
	if got := taken[:cap(taken)][1]; got != "" {
		t.Errorf("the taker's slice gained %q past its length", got)
	}
	if got := l.pending(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("pending = %q, want [a b]", got)
	}
}
