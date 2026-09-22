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

// The sanitizer's whole job, pinned sequence by sequence and character by character. A control
// character in untrusted text is an instruction to the terminal rather than a character in the
// text — ESC opens an ANSI sequence, BEL rings the bell and closes an OSC 52 clipboard payload, CR
// rewinds the line so what follows overwrites what the reader already saw, and NUL or DEL takes
// string length while occupying no display cell. A sequence goes WHOLE: its ESC, its parameters and
// its final byte together, so what the reader sees is the text and not the sequence's tail. The two
// that a wrapped body is railed BY, the newline and the tab, are the class's only survivors — an
// ESC ahead of one of them goes alone, and a sequence nothing terminates stops at the newline.
func TestStripEscapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain text passes through untouched", "just a note", "just a note"},
		{"a CSI colour goes whole", "safe\x1b[31mred\x1b[0m", "safered"},
		{"a CSI screen game goes whole", "safe" + escCSI + "text", "safetext"},
		{"an OSC 52 clipboard write goes whole, BEL-terminated", "safe " + escOSC52 + " text", "safe  text"},
		{"an OSC goes whole, ST-terminated", "safe \x1b]8;;http://evil\x1b\\link text", "safe link text"},
		{"a bare ESC takes its follower with it", "a\x1bcb", "ab"},
		{"an ESC ahead of a kept newline goes alone", "a\x1b\nb", "a\nb"},
		{"an ESC ahead of a kept tab goes alone", "a\x1b\tb", "a\tb"},
		{"an ESC ahead of another ESC goes alone: the second opens its own sequence", "a\x1b\x1b[31mb", "ab"},
		{"an unterminated CSI is swallowed to the end of its line and the next line survives", "a\x1b[31\nb", "a\nb"},
		{"an unterminated OSC is swallowed to the end of its line too", "a\x1b]52;c;xyz\nb", "a\nb"},
		{"an unterminated sequence at the end of the text takes the rest", "a\x1b[31", "a"},
		{"a C1 CSI (U+009B) is dropped as a character", "a\u009b31mb", "a31mb"},
		{"the rest of C1 goes with it", "a\u0080b\u009fc", "abc"},
		{"BEL rings the bell", "safe\x07text", "safetext"},
		{"a lone CR rewinds the line", "shown\rhidden", "shownhidden"},
		{"a lone VT goes", "a\vb", "ab"},
		{"a lone FF goes", "a\fb", "ab"},
		{"a lone NEL goes", "a\u0085b", "ab"},
		{"CRLF leaves the newline behind", "first\r\nsecond", "first\nsecond"},
		{"NUL, backspace and the rest of C0 go too", "a\x00b\x08c\x1fd", "abcd"},
		{"DEL goes with them", "a\x7fb", "ab"},
		{"the newline and the tab are the body's own", "para\n\nnext\tcolumn", "para\n\nnext\tcolumn"},
		{"ZWJ and the soft hyphen survive: they are the user's own prose", "\U0001f469\u200d\U0001f4bb in\u00adcremental", "\U0001f469\u200d\U0001f4bb in\u00adcremental"},
		{"non-ASCII text is not control text", "héllo — 世界 ✓", "héllo — 世界 ✓"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripEscapes(tc.in)

			if got != tc.want {
				t.Errorf("StripEscapes(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) {
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

// The allocation the seam is cheap because of: the strip returns its input unchanged when it
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
		{"a CSI and a BEL go with them", "safe\x1b[31m\x07red", "safered"},
		{"an ESC ahead of a folded newline goes alone", "a\x1b\nb", "a b"},
		{"an unterminated CSI stops at the newline, which folds", "a\x1b[31\nb", "a b"},
		{"ordinary text is untouched", "just a label", "just a label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := StripEscapesToLine(tc.in)

			if got != tc.want {
				t.Errorf("StripEscapesToLine(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if r < 0x20 || (r >= 0x7f && r <= 0x9f) || BidiControl(r) {
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

	want := []string{"safe", "run safe.sh", "plain"}
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

// TestClampRunes pins the one clamp for a fixed rune budget: text that fits comes back as it went
// in (the string itself, not a copy), text over the budget is cut on a rune boundary so a multibyte
// character is never split, and the clamp neither trims nor marks the cut — a caller that wants
// an ellipsis compares the result with the input and adds its own.
func TestClampRunes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"fits", "short", 10, "short"},
		{"exactly n", "12345", 5, "12345"},
		{"over n is cut", "1234567", 5, "12345"},
		{"cut on a rune boundary", "héllo wörld", 4, "héll"},
		{"multibyte counts one rune per character", "日本語テキスト", 3, "日本語"},
		{"not trimmed", "  padded  ", 20, "  padded  "},
		{"no ellipsis", "abcdef", 3, "abc"},
		{"empty stays empty", "", 5, ""},
		{"zero budget cuts everything", "abc", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ClampRunes(tc.in, tc.n); got != tc.want {
				t.Errorf("ClampRunes(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

// TestFirstLine pins the one form every single-line display paints a delegation in: the text ahead
// of the first newline, trimmed. The \r\n case is the one worth spelling out — the cut is made on
// the \n, so the \r is left behind for the trim rather than surviving into a rendered row.
func TestFirstLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a single line is its own first line", "repo-scout", "repo-scout"},
		{"padding is trimmed", "   repo-scout\t ", "repo-scout"},
		{"multi-line keeps the first", "repo-scout\nand then some prose", "repo-scout"},
		{"a padded multi-line first is trimmed", "  repo-scout  \n more prose\n", "repo-scout"},
		{"\\r\\n leaves no \\r behind", "repo-scout\r\nprose", "repo-scout"},
		{"a leading blank line is absent", "\nrepo-scout", ""},
		{"whitespace only is absent", "   \n  ", ""},
		{"empty is empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := FirstLine(tc.in); got != tc.want {
				t.Errorf("FirstLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
