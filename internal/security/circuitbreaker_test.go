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
		t.Fatal("a success after the trip left the signature tripped — the call would be blocked forever")
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
	if !b.Record(a, true) {
		t.Fatal("signature alpha should trip on its own 2nd failure")
	}
	if b.Tripped(c) {
		t.Fatal("tripping alpha must not trip charlie")
	}
}

// TestCircuitBreaker_ConcurrentUse exercises the "safe for concurrent use" guarantee the
// type's doc comment makes: goroutines interleave Record and Tripped over a signature they
// all share and one each of them alone drives. Under -race the run itself is the assertion
// for the shared signature (whose final state is deliberately not deterministic); the
// private signatures carry the deterministic one.
func TestCircuitBreaker_ConcurrentUse(t *testing.T) {
	t.Parallel()
	const goroutines = 8
	b := NewCircuitBreaker(3)
	shared := breakerCall("terminal", "shared")
	own := func(g int) domain.ToolCall { return breakerCall("terminal", fmt.Sprintf("own-%d", g)) }

	edges := make([]int, goroutines) // edges[g] is written by goroutine g only
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			mine := own(g)
			for i := 0; i < b.Threshold(); i++ {
				b.Record(shared, g%2 == 0) // half the goroutines fail the shared signature, half succeed
				b.Tripped(shared)
				if b.Record(mine, true) {
					edges[g]++
				}
				b.Tripped(mine)
			}
		}(g)
	}
	wg.Wait()

	for g := 0; g < goroutines; g++ {
		if edges[g] != 1 {
			t.Errorf("signature own-%d saw %d trip edges, want exactly 1", g, edges[g])
		}
		if !b.Tripped(own(g)) {
			t.Errorf("signature own-%d not tripped after %d identical failures", g, b.Threshold())
		}
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
