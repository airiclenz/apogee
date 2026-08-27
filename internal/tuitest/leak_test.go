package tuitest

import (
	"strings"
	"testing"
	"time"
)

// parkedForTest blocks in a frame this package owns, which is exactly what a leaked driver
// goroutine looks like from the outside.
func parkedForTest(started chan<- struct{}, stop <-chan struct{}) {
	close(started)
	<-stop
}

// TestLeakedGoroutinesSeesAParkedOne — and stops seeing it once it finishes. The two halves are
// one test on purpose: a check that only ever reports a leak is as useless as one that never does.
// It does not run in parallel: it counts goroutines, and a neighbour's screen would be counted too.
func TestLeakedGoroutinesSeesAParkedOne(t *testing.T) {
	before := len(leakedGoroutines())

	started, stop := make(chan struct{}), make(chan struct{})
	go parkedForTest(started, stop)
	<-started
	if got := len(leakedGoroutines()); got <= before {
		t.Errorf("leakedGoroutines() = %d with a goroutine parked in a tuitest frame, want more than %d", got, before)
	}
	close(stop)

	deadline := time.Now().Add(2 * time.Second)
	for len(leakedGoroutines()) > before {
		if !time.Now().Before(deadline) {
			t.Fatalf("the finished goroutine is still reported as leaked")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestLeakedGoroutinesIgnoresItsOwnChecker: the goroutine walking the stacks is standing in a
// tuitest frame itself, and a check that reported itself would fail every test that used it.
func TestLeakedGoroutinesIgnoresItsOwnChecker(t *testing.T) {
	for _, stack := range leakedGoroutines() {
		if strings.Contains(stack, checkerFrame) {
			t.Errorf("leakedGoroutines() reported the goroutine doing the looking:\n%s", stack)
		}
	}
}

// TestCheckLeaksPassesWhenTheScreenIsClosed is the positive case, run the way a driver test runs
// it: CheckLeaks first, the screen after, and the cleanup order does the rest. If the answer pump
// outlived the subtest this fails.
func TestCheckLeaksPassesWhenTheScreenIsClosed(t *testing.T) {
	t.Run("a screen opened and closed", func(t *testing.T) {
		CheckLeaks(t)
		s := NewScreen(20, 5)
		t.Cleanup(s.Close)
		if _, err := s.Write([]byte("hello")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	})
}
