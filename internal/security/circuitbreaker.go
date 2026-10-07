package security

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Circuit-breaker (halt a runaway repeated-tool / tool-loop — D6)
// ----------------------------------------------------------------------------

// DefaultCircuitBreakerThreshold is the number of back-to-back identical *failing* calls
// after which the breaker trips. A small model stuck calling the same tool with the same
// arguments and getting the same error is the loop this catches; three identical failures
// in a row is a clear runaway while still tolerating a transient retry or two.
const DefaultCircuitBreakerThreshold = 3

// CircuitBreaker trips a (tool, arguments) signature once it fails Threshold times back
// to back — with no call of any other signature recorded between those failures — so a
// model stuck in a tool-loop is halted with a surfaced ErrorEvent rather than spinning
// forever. The streak belongs to the last recorded signature only: recording any other
// signature, failed or not, zeroes the streak and re-arms a tripped signature, and a
// success of the same signature zeroes it too. Failures interleaved with other calls
// therefore never add up to a trip, and a tripped call is allowed again as soon as another
// call runs. Only executed calls are recorded: a refused call (this breaker, the
// dangerous-action guard, the mode, an approval denial) never reaches Record, so a refusal
// changes nothing. It is safe for concurrent use (the executor and any observer may touch
// it), though the loop drives one Agent from one goroutine and records a reply's calls in
// the order they were emitted.
type CircuitBreaker struct {
	threshold int

	mu      sync.Mutex
	last    string // signature of the last recorded call ("" before the first Record)
	streak  int    // back-to-back failures of last
	tripped bool   // last has tripped (so a trip is reported once and Tripped refuses it)
}

// NewCircuitBreaker returns a breaker that trips after threshold back-to-back identical
// failing calls. A threshold <= 0 falls back to DefaultCircuitBreakerThreshold.
func NewCircuitBreaker(threshold int) *CircuitBreaker {
	if threshold <= 0 {
		threshold = DefaultCircuitBreakerThreshold
	}
	return &CircuitBreaker{threshold: threshold}
}

// Tripped reports whether the breaker is already open for call's signature — checked
// BEFORE executing, so a tripped signature short-circuits without running the tool again.
// Only the last recorded signature can be open: any other recorded call re-arms it.
func (b *CircuitBreaker) Tripped(call domain.ToolCall) bool {
	sig := signature(call)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tripped && b.last == sig
}

// Record updates the breaker with the outcome of executing call. A call whose signature
// differs from the last recorded one first zeroes the streak and re-arms a tripped
// signature; then a failing call extends the streak and a succeeding call zeroes it. It
// returns true at the moment the streak first reaches the threshold (the trip edge), so the
// caller surfaces a single ErrorEvent; the tripped signature is caught by Tripped from then
// on, until another call is recorded.
func (b *CircuitBreaker) Record(call domain.ToolCall, failed bool) bool {
	sig := signature(call)
	b.mu.Lock()
	defer b.mu.Unlock()

	if sig != b.last {
		b.last = sig
		b.streak = 0
		b.tripped = false
	}

	if !failed {
		b.streak = 0
		b.tripped = false
		return false
	}

	b.streak++
	if b.streak >= b.threshold && !b.tripped {
		b.tripped = true
		return true
	}
	return false
}

// Threshold reports the configured trip threshold.
func (b *CircuitBreaker) Threshold() int { return b.threshold }

// signature derives a stable key for a tool call from its tool name and exact argument
// bytes — two calls are "identical" for breaker purposes iff both match. The arguments
// are hashed so the key stays bounded regardless of argument size.
func signature(call domain.ToolCall) string {
	h := sha256.New()
	h.Write([]byte(call.Tool))
	h.Write([]byte{0})
	h.Write(call.Arguments)
	return call.Tool + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}
