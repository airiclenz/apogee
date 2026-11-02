package context

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
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

// TestElideToBytes_BoundsContentPastTheMarker pins the guarded helper's ordinary case: content
// within the bound passes unchanged, and content past it renders head, marker and tail within the
// bound, as valid UTF-8, whether or not it has a line break to cut at.
func TestElideToBytes_BoundsContentPastTheMarker(t *testing.T) {
	t.Parallel()

	maxBytes := len(toolResultElisionMarker) + 64
	if got := ElideToBytes("short", maxBytes); got != "short" {
		t.Errorf("ElideToBytes(within bound) = %q, want the content unchanged", got)
	}

	for name, content := range map[string]string{
		"single line": "HEAD-" + strings.Repeat("é", 2000) + "-TAIL",
		"many lines":  strings.Repeat("a line of text\n", 500),
	} {
		got := ElideToBytes(content, maxBytes)
		if len(got) > maxBytes {
			t.Errorf("%s: rendering is %d bytes, want at most %d", name, len(got), maxBytes)
		}
		if !strings.Contains(got, toolResultElisionMarker) {
			t.Errorf("%s: rendering %q carries no elision marker", name, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: rendering %q is not valid UTF-8", name, got)
		}
	}
}

// TestElideToBytes_NoRoomForTheMarkerCutsABareHead pins the guard: a bound smaller than the
// marker, zero or negative never panics and yields a bare head of at most max(bound, 0) bytes,
// backed off to a whole UTF-8 sequence, with no marker it could not pay for.
func TestElideToBytes_NoRoomForTheMarkerCutsABareHead(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("é", 1000) // 2000 bytes, two per rune
	for _, bound := range []int{len(toolResultElisionMarker) - 1, len(toolResultElisionMarker), 7, 1, 0, -1, -500} {
		got := ElideToBytes(content, bound)
		if len(got) > max(bound, 0) {
			t.Errorf("bound %d: head is %d bytes, want at most %d", bound, len(got), max(bound, 0))
		}
		if !strings.HasPrefix(content, got) {
			t.Errorf("bound %d: %q is not a head of the content", bound, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("bound %d: head %q is not valid UTF-8", bound, got)
		}
		if strings.Contains(got, strings.TrimSpace(toolResultElisionMarker)) {
			t.Errorf("bound %d: head %q carries the marker though the bound cannot hold it", bound, got)
		}
	}
	if got := ElideToBytes(content, 7); got != "ééé" {
		t.Errorf("ElideToBytes(bound 7) = %q, want the three whole runes the bound holds", got)
	}
}
