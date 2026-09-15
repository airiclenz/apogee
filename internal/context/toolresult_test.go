package context

import (
	"strconv"
	"strings"
	"testing"
)

// TestElideMiddle_KeepsWholeLinesAtBothEnds pins the byte-sized elision: the head ends on a full
// line inside its budget, the tail starts on one, the shared marker sits between them, nothing
// else of the middle survives and the whole rendering stays within maxBytes.
func TestElideMiddle_KeepsWholeLinesAtBothEnds(t *testing.T) {
	t.Parallel()

	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, strings.Repeat("x", 9)+" "+strconv.Itoa(i))
	}
	content := strings.Join(lines, "\n")
	maxBytes := 100 + len(toolResultElisionMarker) + 50

	got := ElideMiddle(content, maxBytes, 100)

	if len(got) > maxBytes {
		t.Errorf("rendering is %d bytes, want at most %d", len(got), maxBytes)
	}

	if !strings.HasPrefix(got, "xxxxxxxxx 1\nxxxxxxxxx 2\nxxxxxxxxx 3\nxxxxxxxxx 4\nxxxxxxxxx 5\nxxxxxxxxx 6\nxxxxxxxxx 7\nxxxxxxxxx 8\n"+toolResultElisionMarker) {
		t.Errorf("head = %q, want the first eight whole lines then the marker", got[:min(len(got), 200)])
	}
	if !strings.HasSuffix(got, toolResultElisionMarker+"xxxxxxxxx 97\nxxxxxxxxx 98\nxxxxxxxxx 99\nxxxxxxxxx 100") {
		t.Errorf("tail = %q, want the marker then the last four whole lines — the tail spends what the head gave up", got[max(0, len(got)-200):])
	}
	if strings.Contains(got, "xxxxxxxxx 50") {
		t.Errorf("result still carries a middle line: %q", got)
	}
}

// TestElideMiddle_NoLineBreakCutsAtWholeRunes pins the fallback for a budget with no line break in
// it: the cut lands on a byte boundary that never tears a multi-byte rune apart — the head's odd
// fifth byte is given up, and the tail spends the byte it freed.
func TestElideMiddle_NoLineBreakCutsAtWholeRunes(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("é", 100) // 200 bytes, two per rune

	got := ElideMiddle(content, 5+len(toolResultElisionMarker)+5, 5)

	if want := "éé" + toolResultElisionMarker + "ééé"; got != want {
		t.Errorf("ElideMiddle = %q, want %q", got, want)
	}
}
