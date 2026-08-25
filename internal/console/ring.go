package console

import (
	"sync"
	"time"
)

// ringCapacity is how much unread output one Console holds: 1 MiB. Past it the OLDEST bytes go,
// because a long-running Console's interesting output is its tail — the prompt it is sitting at,
// the error it just printed — not the scrollback the model already had a chance to read.
const ringCapacity = 1 << 20

// ring is a Console's output buffer: a fixed-size circular byte buffer holding what the process
// has written but nobody has read yet, plus a count of how many bytes were dropped to make room.
// The process's reader goroutine is its only writer (through Write, which satisfies io.Writer so
// the confinement denial watch can sit in front of it) and a tool call is its only reader.
//
// It is drain-on-read: Read hands back everything unread and empties the buffer, so "unread"
// means "since the previous Read" and no cursor has to be tracked. Read can wait for new bytes,
// which is what lets a Console tool collect output for a window of time without polling; close
// releases every waiter at once, so a process that exits never leaves a caller parked.
type ring struct {
	mu sync.Mutex
	// buf is the circular storage; start is the index of the oldest unread byte and length how
	// many unread bytes follow it, wrapping at len(buf).
	buf    []byte
	start  int
	length int
	// dropped counts bytes discarded since the last Read, which the reader reports so the model
	// learns its output has a hole rather than silently reading a spliced stream.
	dropped int
	// closed marks the process's output as finished; a closed ring still hands out what it holds.
	closed bool
	// wake is closed and replaced whenever bytes arrive or the ring closes — a broadcast a
	// timed select can wait on, which sync.Cond cannot offer.
	wake chan struct{}
}

// newRing returns an empty ring holding at most capacity unread bytes.
func newRing(capacity int) *ring {
	return &ring{buf: make([]byte, capacity), wake: make(chan struct{})}
}

// Write stores p as unread output, dropping the oldest bytes when the buffer is full, and wakes
// anyone waiting in Read. It never fails and never blocks: a Console that nobody reads must not
// wedge the process writing into it.
func (r *ring) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	capacity := len(r.buf)
	if len(p) >= capacity {
		// The write alone overflows the ring: everything unread and all but the last
		// capacity bytes of p are dropped.
		r.dropped += r.length + len(p) - capacity
		copy(r.buf, p[len(p)-capacity:])
		r.start, r.length = 0, capacity
		r.broadcastLocked()
		return len(p), nil
	}
	if overflow := r.length + len(p) - capacity; overflow > 0 {
		r.start = (r.start + overflow) % capacity
		r.length -= overflow
		r.dropped += overflow
	}
	end := (r.start + r.length) % capacity
	copied := copy(r.buf[end:], p)
	if copied < len(p) {
		copy(r.buf, p[copied:])
	}
	r.length += len(p)
	r.broadcastLocked()
	return len(p), nil
}

// Read returns the output written since the previous Read together with how many bytes were
// dropped over the same span, and empties the buffer.
//
// With wait <= 0 it reports what is buffered right now. With wait > 0 and nothing buffered it
// blocks until the first new bytes arrive — returning as soon as SOME output does, not waiting
// out the whole window — or until the deadline passes, or until close reports the process's
// output finished, whichever comes first.
func (r *ring) Read(wait time.Duration) ([]byte, int) {
	deadline := time.Now().Add(wait)
	for {
		r.mu.Lock()
		if r.length > 0 || r.closed || wait <= 0 {
			unread, dropped := r.drainLocked()
			r.mu.Unlock()
			return unread, dropped
		}
		wake := r.wake
		r.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			r.mu.Lock()
			unread, dropped := r.drainLocked()
			r.mu.Unlock()
			return unread, dropped
		}
		timer := time.NewTimer(remaining)
		select {
		case <-wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// close marks the output finished and releases every waiter. It is idempotent: the reader
// goroutine closes the ring when the process's output ends, and Close does so again on teardown.
func (r *ring) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.broadcastLocked()
}

// drainLocked hands back the unread bytes and the dropped count, resetting both. The caller
// holds r.mu.
func (r *ring) drainLocked() ([]byte, int) {
	dropped := r.dropped
	r.dropped = 0
	if r.length == 0 {
		return nil, dropped
	}
	unread := make([]byte, r.length)
	copied := copy(unread, r.buf[r.start:])
	if copied < r.length {
		copy(unread[copied:], r.buf[:r.length-copied])
	}
	r.start, r.length = 0, 0
	return unread, dropped
}

// broadcastLocked releases every Read parked on the current wake channel and installs a fresh
// one for the next wait. The caller holds r.mu.
func (r *ring) broadcastLocked() {
	close(r.wake)
	r.wake = make(chan struct{})
}
