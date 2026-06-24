package security

import (
	"encoding/json"
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

func TestNewCircuitBreaker_DefaultThreshold(t *testing.T) {
	t.Parallel()
	if got := NewCircuitBreaker(0).Threshold(); got != DefaultCircuitBreakerThreshold {
		t.Fatalf("threshold for 0 = %d, want default %d", got, DefaultCircuitBreakerThreshold)
	}
	if got := NewCircuitBreaker(-5).Threshold(); got != DefaultCircuitBreakerThreshold {
		t.Fatalf("threshold for negative = %d, want default %d", got, DefaultCircuitBreakerThreshold)
	}
}
