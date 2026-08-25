package console

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestRingReadDrainsWhatWasWrittenSinceTheLastRead pins the drain-on-read contract: a Read hands
// back everything written since the previous one and leaves the buffer empty.
func TestRingReadDrainsWhatWasWrittenSinceTheLastRead(t *testing.T) {
	t.Parallel()

	r := newRing(64)
	writeString(t, r, "first")
	writeString(t, r, "-second")

	unread, dropped := r.Read(0)
	if string(unread) != "first-second" {
		t.Fatalf("first read = %q, want %q", unread, "first-second")
	}
	if dropped != 0 {
		t.Fatalf("first read dropped %d bytes, want 0", dropped)
	}

	if unread, _ := r.Read(0); len(unread) != 0 {
		t.Fatalf("second read = %q, want nothing left", unread)
	}

	writeString(t, r, "third")
	if unread, _ := r.Read(0); string(unread) != "third" {
		t.Fatalf("third read = %q, want %q", unread, "third")
	}
}

// TestRingOverflowDropsOldestBytesAndCountsThem covers the overflow rule in both shapes: a write
// that tips the buffer over its capacity, and a single write bigger than the whole buffer.
func TestRingOverflowDropsOldestBytesAndCountsThem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		capacity    int
		writes      []string
		wantUnread  string
		wantDropped int
	}{
		{
			name:        "no overflow keeps everything",
			capacity:    8,
			writes:      []string{"abc", "def"},
			wantUnread:  "abcdef",
			wantDropped: 0,
		},
		{
			name:        "overflow drops the oldest bytes",
			capacity:    8,
			writes:      []string{"abcdef", "ghij"},
			wantUnread:  "cdefghij",
			wantDropped: 2,
		},
		{
			name:        "a write larger than the ring keeps only its tail",
			capacity:    4,
			writes:      []string{"xy", "abcdefg"},
			wantUnread:  "defg",
			wantDropped: 5,
		},
		{
			name:        "wrapped writes reassemble in order",
			capacity:    6,
			writes:      []string{"abcd", "efgh"},
			wantUnread:  "cdefgh",
			wantDropped: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			r := newRing(test.capacity)
			for _, write := range test.writes {
				writeString(t, r, write)
			}
			unread, dropped := r.Read(0)
			if string(unread) != test.wantUnread {
				t.Errorf("unread = %q, want %q", unread, test.wantUnread)
			}
			if dropped != test.wantDropped {
				t.Errorf("dropped = %d, want %d", dropped, test.wantDropped)
			}
			// The dropped count is per span, so the next read starts clean.
			if _, dropped := r.Read(0); dropped != 0 {
				t.Errorf("dropped after draining = %d, want 0", dropped)
			}
		})
	}
}

// TestRingReadReturnsAsSoonAsNewBytesArrive proves a waiting Read is released by the first write
// rather than sitting out its whole window — the property that lets a tool collect output for up
// to N milliseconds without polling.
func TestRingReadReturnsAsSoonAsNewBytesArrive(t *testing.T) {
	t.Parallel()

	r := newRing(64)
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = r.Write([]byte("late"))
	}()

	start := time.Now()
	unread, _ := r.Read(5 * time.Second)
	elapsed := time.Since(start)

	if string(unread) != "late" {
		t.Fatalf("read = %q, want %q", unread, "late")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("read waited %v for bytes that arrived after 20ms", elapsed)
	}
}

// TestRingReadReturnsWhenTheRingCloses proves a process whose output ends never leaves a caller
// parked for the rest of its window.
func TestRingReadReturnsWhenTheRingCloses(t *testing.T) {
	t.Parallel()

	r := newRing(64)
	go func() {
		time.Sleep(20 * time.Millisecond)
		r.close()
	}()

	start := time.Now()
	unread, _ := r.Read(5 * time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("read waited %v after the ring closed", elapsed)
	}
	if len(unread) != 0 {
		t.Fatalf("read = %q, want nothing", unread)
	}

	// A closed ring still hands out what it holds, and closing twice is a no-op.
	writeString(t, r, "tail")
	r.close()
	if unread, _ := r.Read(time.Second); string(unread) != "tail" {
		t.Fatalf("read after close = %q, want %q", unread, "tail")
	}
}

// TestRingReadWithoutWaitReportsWhatIsBufferedNow pins the wait <= 0 case: it never blocks, even
// on an empty ring nobody will ever write to.
func TestRingReadWithoutWaitReportsWhatIsBufferedNow(t *testing.T) {
	t.Parallel()

	r := newRing(64)
	start := time.Now()
	if unread, _ := r.Read(0); len(unread) != 0 {
		t.Fatalf("read = %q, want nothing", unread)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("read(0) on an empty ring took %v, want no wait", elapsed)
	}
}

// TestRingWriteNeverBlocksAWriterNobodyReads is the property that keeps a Console the model
// forgot about from wedging the process writing into it.
func TestRingWriteNeverBlocksAWriterNobodyReads(t *testing.T) {
	t.Parallel()

	r := newRing(1024)
	chunk := bytes.Repeat([]byte("x"), 512)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			_, _ = r.Write(chunk)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("writes blocked with nobody reading")
	}

	unread, dropped := r.Read(0)
	if len(unread) != 1024 {
		t.Errorf("unread = %d bytes, want the full ring (1024)", len(unread))
	}
	if want := 100*512 - 1024; dropped != want {
		t.Errorf("dropped = %d, want %d", dropped, want)
	}
	if strings.Trim(string(unread), "x") != "" {
		t.Errorf("unread carries bytes nobody wrote")
	}
}

// writeString writes s to r and fails the test if the ring did not take all of it.
func writeString(t *testing.T, r *ring, s string) {
	t.Helper()
	written, err := r.Write([]byte(s))
	if err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
	if written != len(s) {
		t.Fatalf("write %q reported %d bytes, want %d", s, written, len(s))
	}
}
