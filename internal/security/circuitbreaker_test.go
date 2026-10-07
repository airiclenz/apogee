package security

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func breakerCall(tool, arg string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"x": arg})
	return domain.ToolCall{ID: "c", Tool: tool, Arguments: args}
}

func TestCircuitBreaker_TripsAfterNIdenticalFailures(t *testing.T) {
	t.Parallel()
	b := NewCircuitBreaker(3)
	call := breakerCall("terminal", "boom")

	for i := 1; i <= 2; i++ {
		if tripped := b.Record(call, true); tripped {
			t.Fatalf("breaker tripped early on failure #%d", i)
		}
		if b.Tripped(call) {
			t.Fatalf("Tripped() true after only %d failures (threshold 3)", i)
		}
	}

	if tripped := b.Record(call, true); !tripped {
		t.Fatal("breaker did not report the trip edge on the 3rd identical failure")
	}
	if !b.Tripped(call) {
		t.Fatal("Tripped() false after the breaker reported a trip")
	}
}

func TestCircuitBreaker_TripReportedOnce(t *testing.T) {
	t.Parallel()
	b := NewCircuitBreaker(2)
	call := breakerCall("terminal", "boom")

	b.Record(call, true)
	if !b.Record(call, true) {
		t.Fatal("expected trip edge on 2nd failure")
	}
	if b.Record(call, true) {
		t.Fatal("trip edge reported more than once for the same signature")
	}
}

func TestCircuitBreaker_SuccessResetsStreak(t *testing.T) {
	t.Parallel()
	b := NewCircuitBreaker(3)
	call := breakerCall("terminal", "boom")

	b.Record(call, true)
	b.Record(call, true)
	b.Record(call, false) // a success clears the streak
	if b.Tripped(call) {
		t.Fatal("a success did not clear the failure streak")
	}
	// Two more failures should still not trip (streak restarted).
	b.Record(call, true)
	if tripped := b.Record(call, true); tripped {
		t.Fatal("breaker tripped on only 2 failures after a reset")
	}
}

// TestCircuitBreaker_SuccessClearsATrippedSignature pins that a success of the tripped
// signature itself — recorded by a caller that does not consult Tripped first — re-arms it
// like any other recorded call, and the trip edge comes back only after a fresh run of
// Threshold back-to-back failures.
func TestCircuitBreaker_SuccessClearsATrippedSignature(t *testing.T) {
	t.Parallel()
	b := NewCircuitBreaker(3)
	call := breakerCall("terminal", "boom")

	for i := 0; i < 3; i++ {
		b.Record(call, true)
	}
	if !b.Tripped(call) {
		t.Fatal("setup: breaker not tripped after 3 identical failures")
	}

	b.Record(call, false) // the call finally succeeded

	if b.Tripped(call) {
		t.Fatal("a success of the tripped call left it tripped")
	}
	// The streak restarted too: the trip edge comes back only after a fresh run of 3,
	// never on the first failure of the new streak.
	for i := 1; i <= 2; i++ {
		if b.Record(call, true) {
			t.Fatalf("trip edge reported on failure #%d of the streak that followed the success", i)
		}
	}
	if !b.Record(call, true) {
		t.Fatal("breaker did not trip again on the 3rd failure after recovering")
	}
}

// TestCircuitBreaker_DistinctCallsIndependent pins that distinct signatures never add into
// one streak: a failure of another call resets the streak instead of extending it, and
// tripping one signature leaves the other untouched.
func TestCircuitBreaker_DistinctCallsIndependent(t *testing.T) {
	t.Parallel()
	b := NewCircuitBreaker(2)
	a := breakerCall("terminal", "alpha")
	c := breakerCall("terminal", "charlie")

	b.Record(a, true)
	b.Record(c, true)
	if b.Tripped(a) || b.Tripped(c) {
		t.Fatal("distinct signatures should not share a streak")
	}
	if b.Record(a, true) {
		t.Fatal("alpha tripped on a failure that followed charlie — its earlier failure was not back to back")
	}
	if !b.Record(a, true) {
		t.Fatal("signature alpha should trip on its 2nd back-to-back failure")
	}
	if b.Tripped(c) {
		t.Fatal("tripping alpha must not trip charlie")
	}
}

func TestCircuitBreaker_InterleavedFailuresNeverTrip(t *testing.T) {
	t.Parallel()
	b := NewCircuitBreaker(3)
	a := breakerCall("terminal", "alpha")
	c := breakerCall("terminal", "bravo")

	for i := 0; i < 3; i++ {
		if b.Record(a, true) {
			t.Fatalf("alpha tripped on interleaved failure #%d", i+1)
		}
		if b.Record(c, true) {
			t.Fatalf("bravo tripped on interleaved failure #%d", i+1)
		}
	}
	if b.Tripped(a) || b.Tripped(c) {
		t.Fatal("interleaved failures tripped the breaker — only back-to-back identical failures may")
	}
}

func TestCircuitBreaker_DifferentCallReArms(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		otherFailed bool
	}{
		{name: "other call succeeds", otherFailed: false},
		{name: "other call fails", otherFailed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := NewCircuitBreaker(3)
			a := breakerCall("terminal", "alpha")
			other := breakerCall("terminal", "bravo")

			for i := 0; i < 3; i++ {
				b.Record(a, true)
			}
			if !b.Tripped(a) {
				t.Fatal("setup: alpha not tripped after 3 back-to-back failures")
			}

			b.Record(other, tc.otherFailed)

			if b.Tripped(a) {
				t.Fatal("alpha still tripped after another call was recorded")
			}
			for i := 1; i <= 2; i++ {
				if b.Record(a, true) {
					t.Fatalf("alpha tripped again on failure #%d after re-arming, want a fresh run of 3", i)
				}
			}
			if !b.Record(a, true) {
				t.Fatal("alpha did not trip on the 3rd fresh back-to-back failure")
			}
		})
	}
}

// TestCircuitBreaker_ConcurrentUse exercises the "safe for concurrent use" guarantee the
// type's doc comment makes: goroutines interleave Record and Tripped over a signature they
// all share and one each of them alone drives. Under -race the run itself is the assertion
// for the concurrent phase (whose final state is deliberately not deterministic — every
// signature resets the others' streak); a final single-goroutine run carries the
// deterministic one.
func TestCircuitBreaker_ConcurrentUse(t *testing.T) {
	t.Parallel()
	const goroutines = 8
	b := NewCircuitBreaker(3)
	shared := breakerCall("terminal", "shared")
	own := func(g int) domain.ToolCall { return breakerCall("terminal", fmt.Sprintf("own-%d", g)) }

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			mine := own(g)
			for i := 0; i < b.Threshold(); i++ {
				b.Record(shared, g%2 == 0) // half the goroutines fail the shared signature, half succeed
				b.Tripped(shared)
				b.Record(mine, true)
				b.Tripped(mine)
			}
		}(g)
	}
	wg.Wait()

	final := breakerCall("terminal", "final")
	for i := 1; i < b.Threshold(); i++ {
		if b.Record(final, true) {
			t.Fatalf("final signature tripped early on failure #%d", i)
		}
	}
	if !b.Record(final, true) {
		t.Fatalf("final signature did not report the trip edge on failure #%d", b.Threshold())
	}
	if !b.Tripped(final) {
		t.Fatal("final signature not tripped after the trip edge")
	}
}

func TestNewCircuitBreaker_DefaultThreshold(t *testing.T) {
	t.Parallel()
	if got := NewCircuitBreaker(0).Threshold(); got != DefaultCircuitBreakerThreshold {
		t.Fatalf("threshold for 0 = %d, want default %d", got, DefaultCircuitBreakerThreshold)
	}
	if got := NewCircuitBreaker(-5).Threshold(); got != DefaultCircuitBreakerThreshold {
		t.Fatalf("threshold for negative = %d, want default %d", got, DefaultCircuitBreakerThreshold)
	}
}
