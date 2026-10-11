package tuitest

import "testing"

// cpr is one cursor-position report as the emulator writes it into the program's input.
var cpr = []byte("\x1b[1;1R")

// TestCursorAnswersCountFromTheMark pins the anchor a mark-anchored wait counts from: answers
// pumped before the mark do not count, answers pumped after it do, and a key sent after it does not
// move it — which is the whole difference from the last-key count, which that same key restarts.
func TestCursorAnswersCountFromTheMark(t *testing.T) {
	t.Parallel()

	var c cursorAnswers
	c.pump(cpr)
	c.pump(cpr)
	mark := c.mark()
	if got := c.sinceMark(mark); got != 0 {
		t.Fatalf("sinceMark right after the mark = %d; want 0 (answers before it must not count)", got)
	}

	c.pump(cpr)
	c.keySent()
	c.pump(cpr)
	if got := c.sinceMark(mark); got != 2 {
		t.Errorf("sinceMark = %d after two answers behind the mark; want 2", got)
	}
	if got := c.sinceKey(); got != 1 {
		t.Errorf("sinceKey = %d after one answer behind the last key; want 1", got)
	}
}

// TestCursorAnswersCountOneSplitReportOnce pins that a report split across two pumped chunks is
// counted once, when its final byte goes through — so a mark taken between the halves counts it.
func TestCursorAnswersCountOneSplitReportOnce(t *testing.T) {
	t.Parallel()

	var c cursorAnswers
	c.pump(cpr[:3])
	mark := c.mark()
	c.pump(cpr[3:])
	if got := c.sinceMark(mark); got != 1 {
		t.Errorf("sinceMark = %d for a report finished behind the mark; want 1", got)
	}
}
