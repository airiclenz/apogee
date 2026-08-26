package sanitize

import (
	"strings"
	"testing"
)

// The two ANSI sequences the strip exists for, spelled once so the tables below read as text.
const (
	escOSC52 = "\x1b]52;c;cGFyaQ==\x07" // an OSC 52 clipboard write
	escCSI   = "\x1b[2J\x1b[H"          // a CSI screen clear and cursor home
)

// The sanitizer's whole job, pinned character by character. A control character in untrusted text
// is an instruction to the terminal rather than a character in the text — ESC opens an ANSI
// sequence, BEL rings the bell and closes an OSC 52 clipboard payload, CR rewinds the line so what
// follows overwrites what the reader already saw, and NUL or DEL takes string length while
// occupying no display cell — and stripping ESC alone left every one of the others to arrive
// intact. The two that a wrapped body is railed BY, the newline and the tab, are the class's only
// survivors.
func TestStripEscapesDropsControlCharacters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain text passes through untouched", "just a note", "just a note"},
		{"ESC opens an ANSI sequence", "safe\x1b[31mred", "safe[31mred"},
		{"BEL rings the bell", "safe\x07text", "safetext"},
		{"CR rewinds the line", "shown\rhidden", "shownhidden"},
		{"CRLF leaves the newline behind", "first\r\nsecond", "first\nsecond"},
		{"an OSC 52 clipboard write is left inert", "safe " + escOSC52 + " text", "safe ]52;c;cGFyaQ== text"},
		{"a CSI screen game goes with it", "safe" + escCSI + "text", "safe[2J[Htext"},
		{"NUL, backspace and the rest of C0 go too", "a\x00b\x08c\x1fd", "abcd"},
		{"DEL goes with them", "a\x7fb", "ab"},
		{"the newline and the tab are the body's own", "para\n\nnext\tcolumn", "para\n\nnext\tcolumn"},
		{"non-ASCII text is not control text", "héllo — 世界 ✓", "héllo — 世界 ✓"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripEscapes(tc.in)

			if got != tc.want {
				t.Errorf("StripEscapes(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
					t.Errorf("StripEscapes(%q) left %#U behind: %q", tc.in, r, got)
				}
			}
			if again := StripEscapes(got); again != got {
				t.Errorf("StripEscapes is not idempotent: %q became %q", got, again) // every seam may strip twice
			}
		})
	}
}

// The bidi half of the same sanitizer, pinned rune by rune. A bidirectional formatting character
// reorders the glyphs around it without touching a byte an executor reads, so on a decision surface
// it is the same hazard as the CR above: the row says one thing and the tool runs another. The set
// is deliberately narrow — the bidi controls, not all of unicode.Cf — so the survivors below are
// the point of the test as much as the casualties are: U+200D ZWJ holds an emoji sequence together
// and U+00AD is a soft hyphen, and a later "consistency" change to blanket-drop Cf must break a
// test rather than a person's prose.
func TestStripEscapesDropsBidiControls(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"RLO reorders the row it sits in", "run \u202esafe.sh", "run safe.sh"},
		{"LRO goes with it", "run \u202dsafe.sh", "run safe.sh"},
		{"the embeddings and their pop go too", "a\u202ab\u202bc\u202cd", "abcd"},
		{"the isolates go", "a\u2066b\u2067c\u2068d\u2069e", "abcde"},
		{"the marks go", "a\u200eb\u200fc", "abc"},
		{"a whole reversed tail is dropped, not reordered", "echo hello\u202edlrow", "echo hellodlrow"},
		{"ZWJ survives: it holds an emoji sequence together", "\U0001f469\u200d\U0001f4bb ok", "\U0001f469\u200d\U0001f4bb ok"},
		{"a soft hyphen survives: it is the user's own prose", "in\u00adcremental", "in\u00adcremental"},
		{"a zero-width space survives", "a\u200bb", "a\u200bb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripEscapes(tc.in)

			if got != tc.want {
				t.Errorf("StripEscapes(%q) = %q; want %q", tc.in, got, tc.want)
			}
			if strings.ContainsFunc(got, BidiControl) {
				t.Errorf("StripEscapes(%q) left a bidi control behind: %q", tc.in, got)
			}
			if again := StripEscapes(got); again != got {
				t.Errorf("StripEscapes is not idempotent: %q became %q", got, again)
			}
		})
	}
}

// The allocation the seam is cheap because of: strings.Map returns its input unchanged when it
// rewrites nothing, which is the overwhelmingly common case — ordinary text carrying no control
// character at all — and is what lets a producer that also strips cost nothing. A regression here
// would be silent, since the output stays correct either way.
func TestStripEscapesDoesNotAllocateWhenNothingIsRewritten(t *testing.T) {
	// Deliberately not parallel: testing.AllocsPerRun panics when the test that calls it is.

	const clean = "an ordinary answer\nwith a second line\tand a tab, plus 世界"

	if allocs := testing.AllocsPerRun(100, func() { _ = StripEscapes(clean) }); allocs != 0 {
		t.Errorf("StripEscapes allocated %v times on text with nothing to rewrite; want 0", allocs)
	}
}

// The one-line form's difference from the body form, pinned: the two controls a body is railed by
// are exactly what forges a second line or a false column on a row printed beside something else,
// so they fold to a space — one rune for one rune, so a later clip counts what the row will hold -
// while everything else goes where StripEscapes sends it, the bidi set included.
func TestStripEscapesToLineFoldsBreaks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"a newline and a tab fold to one space each", "a\nb\tc", "a b c"},
		{"CRLF folds to one space: the CR is dropped, the newline folded", "first\r\nsecond", "first second"},
		{"the bidi set goes, exactly as it does in a body", "run \u202esafe.sh", "run safe.sh"},
		{"ESC and BEL go with them", "safe\x1b[31m\x07red", "safe[31mred"},
		{"ordinary text is untouched", "just a label", "just a label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripEscapesToLine(tc.in)

			if got != tc.want {
				t.Errorf("StripEscapesToLine(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if r < 0x20 || r == 0x7f || BidiControl(r) {
					t.Errorf("StripEscapesToLine(%q) left %#U behind: %q", tc.in, r, got)
				}
			}
		})
	}
}

// The batch form is the single form over a slice, and its nil is load-bearing: a caller handing an
// absent list of choices must get an absent list back, not an empty one.
func TestStripEscapesAllStripsEveryElement(t *testing.T) {
	t.Parallel()

	if got := StripEscapesAll(nil); got != nil {
		t.Errorf("StripEscapesAll(nil) = %#v; want nil", got)
	}

	in := []string{"safe\x1b[31m", "run \u202esafe.sh", "plain"}
	got := StripEscapesAll(in)

	want := []string{"safe[31m", "run safe.sh", "plain"}
	if len(got) != len(want) {
		t.Fatalf("StripEscapesAll returned %d entries; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("StripEscapesAll[%d] = %q; want %q", i, got[i], want[i])
		}
	}
	if in[0] != "safe\x1b[31m" {
		t.Errorf("StripEscapesAll rewrote its input: %q", in[0])
	}
}

// The set itself, membership tested rather than described: exactly eleven code points in the range
// the bidi characters live in, and nothing else. This is the test a later "let's just drop all of
// unicode.Cf" change has to argue with — U+200B, U+200C and U+200D sit inside the swept range and
// must NOT be members.
func TestBidiControlIsExactlyTheElevenCodePoints(t *testing.T) {
	t.Parallel()

	members := map[rune]bool{
		'\u200e': true, '\u200f': true, // LRM, RLM
		'\u202a': true, '\u202b': true, '\u202c': true, '\u202d': true, '\u202e': true, // LRE, RLE, PDF, LRO, RLO
		'\u2066': true, '\u2067': true, '\u2068': true, '\u2069': true, // LRI, RLI, FSI, PDI
	}
	if len(members) != 11 {
		t.Fatalf("the expected set holds %d code points; the bidi set is eleven", len(members))
	}

	for r := rune(0x2000); r < 0x2070; r++ {
		if got := BidiControl(r); got != members[r] {
			t.Errorf("BidiControl(%#U) = %v; want %v", r, got, members[r])
		}
	}
	for _, r := range []rune{'a', '\n', 0x00, 0x7f, '\u00ad', '\u200b', '\u200d', '\ufeff'} {
		if BidiControl(r) {
			t.Errorf("BidiControl(%#U) = true; the set is the bidi controls only", r)
		}
	}
}
