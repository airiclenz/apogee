package tuitest

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// A resize is the one moment a driver hands the emulator a size the RENDERER has not seen yet.
// Everything already on the wire was laid out for the old terminal, and it keeps arriving after
// the buffer has changed shape — so the write path, not just [Screen.Resize], is what has to
// survive a shrink. These tests pin that on the smallest thing that fails without it: a scroll
// region for a taller screen followed by a delete-line inside it.

// oldSizeFrame is one repaint from a renderer that still believes the terminal is rows tall: the
// scroll region for that height, the cursor at its top, and a delete-line inside it. On a buffer
// that has since shrunk, the region is the part that lies and the delete-line is the part that
// indexes it.
func oldSizeFrame(rows int) []byte {
	return fmt.Appendf(nil, "\x1b[1;%dr\x1b[H\x1b[M", rows)
}

// TestScreenResizeSurvivesAStaleScrollRegion is the deterministic half: shrink, then hand the
// screen the frame the renderer had already computed for the old height. Without the margin clamp
// x/vt sets a bottom margin past the end of the resized buffer and the delete-line panics with an
// index out of range; the screen must instead keep painting inside the height it actually has.
func TestScreenResizeSurvivesAStaleScrollRegion(t *testing.T) {
	t.Parallel()

	const (
		tall  = 30
		short = 20
	)
	s := NewScreen(100, tall)
	defer s.Close()

	mustWrite(t, s, oldSizeFrame(tall))
	s.Resize(100, short)
	mustWrite(t, s, oldSizeFrame(tall))

	// The clamped region is still a working region, not a dead one: home is inside it, and text
	// written there lands on the first row of a screen that is now short rows tall.
	mustWrite(t, s, []byte("\x1b[Hafter the shrink"))

	frame := s.Snapshot()
	if frame.Height() != short {
		t.Errorf("frame height = %d, want %d", frame.Height(), short)
	}
	if got := strings.TrimRight(frame.Row(0), " "); got != "after the shrink" {
		t.Errorf("row 0 = %q, want %q", got, "after the shrink")
	}
}

// TestScreenResizeHeightShrinkWhileOutputStreams is the racing half, and the shape plan item 9 hit:
// the shrink lands while a renderer is painting frames for the old size. The write and the resize
// share the screen's mutex, so the interleaving is at whole-write granularity — exactly one stale
// frame between the resize and the renderer catching up — which is the case that used to take the
// test binary down from inside the writing goroutine.
func TestScreenResizeHeightShrinkWhileOutputStreams(t *testing.T) {
	t.Parallel()

	const (
		tall  = 30
		short = 12
		turns = 50
	)
	s := NewScreen(100, tall)
	defer s.Close()

	frame := oldSizeFrame(tall)
	done := make(chan struct{})
	var painting sync.WaitGroup
	painting.Add(1)
	go func() {
		defer painting.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			if _, err := s.Write(frame); err != nil {
				return
			}
		}
	}()

	for range turns {
		painted := s.BytesWritten()
		s.Resize(100, short)
		// Wait for at least one frame laid out for the tall terminal to land on the short one.
		// That is the window the panic lived in, and without the wait the two resizes can bracket
		// it entirely — the test would then pass against the unfixed code.
		deadline := time.Now().Add(5 * time.Second)
		for s.BytesWritten() < painted+int64(len(frame)) {
			if time.Now().After(deadline) {
				close(done)
				painting.Wait()
				t.Fatal("the painting goroutine stopped writing")
			}
			runtime.Gosched()
		}
		s.Resize(100, tall)
	}
	close(done)
	painting.Wait()

	if got := s.Snapshot().Height(); got != tall {
		t.Errorf("frame height = %d, want %d", got, tall)
	}
}

// TestScreenResizeWidthShrinkSurvivesStaleMargins is the same defect one axis over: a renderer that
// has turned on left/right margins (DECLRMM) and asks for a right margin the narrowed screen no
// longer has. bubbletea does not use them, but [Screen] is a terminal for whatever paints into it.
func TestScreenResizeWidthShrinkSurvivesStaleMargins(t *testing.T) {
	t.Parallel()

	const (
		wide   = 100
		narrow = 40
	)
	s := NewScreen(wide, 30)
	defer s.Close()

	// DECLRMM on, then margins for the full old width, a home and a delete-line inside them: the
	// line operations are the ones that index a margin straight into the buffer, so they are the
	// ones a stale right margin walks off the end of.
	mustWrite(t, s, fmt.Appendf(nil, "\x1b[?69h\x1b[1;%ds\x1b[H\x1b[M", wide))
	s.Resize(narrow, 30)
	mustWrite(t, s, fmt.Appendf(nil, "\x1b[1;%ds\x1b[H\x1b[M", wide))

	if got := s.Snapshot().Width(); got != narrow {
		t.Errorf("frame width = %d, want %d", got, narrow)
	}
}

// mustWrite paints p into s and fails the test on the error a closed screen returns.
func mustWrite(t *testing.T, s *Screen, p []byte) {
	t.Helper()
	if _, err := s.Write(p); err != nil {
		t.Fatalf("write %q to the screen: %v", p, err)
	}
}
