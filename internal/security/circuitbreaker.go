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

// DefaultCircuitBreakerThreshold is the number of consecutive identical *failing* calls
// after which the breaker trips. A small model stuck calling the same tool with the same
// arguments and getting the same error is the loop this catches; three identical failures
// is a clear runaway while still tolerating a transient retry or two.
const DefaultCircuitBreakerThreshold = 3

// CircuitBreaker tracks consecutive identical failing tool calls and trips once a single
// (tool, arguments) signature fails Threshold times in a row, so a model stuck in a
// tool-loop is halted with a surfaced ErrorEvent rather than spinning forever. A
// succeeding call clears its signature's streak. It is safe for concurrent use (the
// executor and any observer may touch it), though the loop drives one Agent from one
// goroutine.
type CircuitBreaker struct {
	threshold int

	mu       sync.Mutex
	failures map[string]int  // signature -> consecutive failure count
	tripped  map[string]bool // signatures that have already tripped (so a trip is reported once)
}

// NewCircuitBreaker returns a breaker that trips after threshold consecutive identical
// failing calls. A threshold <= 0 falls back to DefaultCircuitBreakerThreshold.
func NewCircuitBreaker(threshold int) *CircuitBreaker {
	if threshold <= 0 {
		threshold = DefaultCircuitBreakerThreshold
	}
	return &CircuitBreaker{
		threshold: threshold,
		failures:  make(map[string]int),
		tripped:   make(map[string]bool),
	}
}

// Tripped reports whether the breaker is already open for call's signature — checked
// BEFORE executing, so a tripped signature short-circuits without running the tool again.
func (b *CircuitBreaker) Tripped(call domain.ToolCall) bool {
	sig := signature(call)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tripped[sig]
}

// Record updates the breaker with the outcome of executing call. A failing call
// increments its signature's streak; a succeeding call clears it. It returns true at the
// moment the streak first reaches the threshold (the trip edge), so the caller surfaces a
// single ErrorEvent; subsequent calls on the same tripped signature are caught by Tripped
// instead.
func (b *CircuitBreaker) Record(call domain.ToolCall, failed bool) bool {
	sig := signature(call)
	b.mu.Lock()
	defer b.mu.Unlock()

	if !failed {
		delete(b.failures, sig)
		delete(b.tripped, sig)
		return false
	}

	b.failures[sig]++
	if b.failures[sig] >= b.threshold && !b.tripped[sig] {
		b.tripped[sig] = true
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
