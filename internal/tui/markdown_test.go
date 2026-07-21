package tui

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Markdown rendering (chat-text formatting pass)
// ----------------------------------------------------------------------------
//
// The assertions are written against the VISIBLE text (ansi.Strip) and the visible width
// (lipgloss.Width), so they hold whatever colour profile the test environment resolves — the
// parser must consume its markers and keep the content and layout correct regardless of whether
// ANSI is emitted. The few "is it actually styled?" checks are guarded by colorActive so they are
// skipped (not failed) on a no-colour profile.

// strip returns the visible text of a rendered line with its ANSI styling removed.
func strip(s string) string { return ansi.Strip(s) }

// colorActive reports whether this environment's lipgloss profile emits ANSI escapes, so the
// style-presence assertions run only where colour is actually produced.
func colorActive(th theme) bool { return strings.Contains(th.mdCode.Render("x"), "\x1b") }

func TestRenderInlineBold(t *testing.T) {
	th := newTheme()
	got := renderInline(th, "a **bold** b")
	if v := strip(got); v != "a bold b" {
		t.Errorf("visible = %q; want %q (the ** markers consumed)", v, "a bold b")
	}
	if strings.Contains(got, "**") {
		t.Errorf("output still carries the literal ** markers: %q", got)
	}
	if colorActive(th) && !strings.Contains(got, "\x1b") {
		t.Errorf("bold span emitted no styling: %q", got)
	}
}

func TestRenderInlineCode(t *testing.T) {
	th := newTheme()
	got := renderInline(th, "run `go test` now")
	if v := strip(got); v != "run go test now" {
		t.Errorf("visible = %q; want %q (the backticks consumed)", v, "run go test now")
	}
	if strings.Contains(got, "`") {
		t.Errorf("output still carries a literal backtick: %q", got)
	}
}

// A code span wins over bold: ** inside `…` stays literal (CommonMark code-span precedence).
func TestRenderInlineCodeBeatsBold(t *testing.T) {
	th := newTheme()
	got := renderInline(th, "`**x**`")
	if v := strip(got); v != "**x**" {
		t.Errorf("visible = %q; want %q (bold not parsed inside a code span)", v, "**x**")
	}
}

// An unterminated marker (mid-stream) is left literal — never a leaked escape or eaten text.
func TestRenderInlineUnterminated(t *testing.T) {
	th := newTheme()
	for _, in := range []string{"a **bold start", "a `code start", "trailing *"} {
		got := renderInline(th, in)
		if v := strip(got); v != in {
			t.Errorf("renderInline(%q) visible = %q; want it left literal", in, v)
		}
		if strings.Contains(got, "\x1b") {
			t.Errorf("renderInline(%q) styled an unterminated span: %q", in, got)
		}
	}
}

func TestHeadingStripsMarkers(t *testing.T) {
	th := newTheme()
	for _, lvl := range []string{"#", "##", "###", "####", "#####", "######"} {
		out := renderMarkdownBody(th, lvl+" Title", 40)
		if len(out) != 1 {
			t.Fatalf("%q heading: got %d lines, want 1", lvl, len(out))
		}
		if v := strip(out[0]); v != "Title" {
			t.Errorf("%q heading visible = %q; want %q", lvl, v, "Title")
		}
		if strings.Contains(out[0], "#") {
			t.Errorf("%q heading still carries a literal #: %q", lvl, out[0])
		}
		if colorActive(th) && !strings.Contains(out[0], "\x1b") {
			t.Errorf("%q heading emitted no styling: %q", lvl, out[0])
		}
	}
}

// Seven #s (or a # with no following space) is not a heading; it renders as a plain paragraph.
func TestNotAHeading(t *testing.T) {
	th := newTheme()
	for _, in := range []string{"####### TooDeep", "#NoSpace"} {
		out := renderMarkdownBody(th, in, 40)
		if v := strip(out[0]); v != in {
			t.Errorf("renderMarkdownBody(%q) visible = %q; want it unchanged (not a heading)", in, v)
		}
	}
}

func TestBulletList(t *testing.T) {
	th := newTheme()
	for _, b := range []string{"-", "*", "+"} {
		out := renderMarkdownBody(th, b+" one\n"+b+" two", 40)
		if len(out) != 2 {
			t.Fatalf("bullet %q: got %d lines, want 2", b, len(out))
		}
		if v := strip(out[0]); v != "• one" {
			t.Errorf("bullet %q line 0 visible = %q; want %q", b, v, "• one")
		}
		if v := strip(out[1]); v != "• two" {
			t.Errorf("bullet %q line 1 visible = %q; want %q", b, v, "• two")
		}
	}
}

func TestNumberedList(t *testing.T) {
	th := newTheme()
	out := renderMarkdownBody(th, "1. first\n2) second", 40)
	if v := strip(out[0]); v != "1. first" {
		t.Errorf("numbered line 0 visible = %q; want %q", v, "1. first")
	}
	if v := strip(out[1]); v != "2) second" {
		t.Errorf("numbered line 1 visible = %q; want %q", v, "2) second")
	}
}

// A wrapped list item hangs under its text: the continuation line is indented to the marker width.
func TestListHangingIndent(t *testing.T) {
	th := newTheme()
	out := renderMarkdownBody(th, "- "+strings.Repeat("word ", 8), 20)
	if len(out) < 2 {
		t.Fatalf("expected the long item to wrap, got %d line(s)", len(out))
	}
	if v := strip(out[0]); !strings.HasPrefix(v, "• ") {
		t.Errorf("first line = %q; want it to lead with the bullet", v)
	}
	if v := strip(out[1]); !strings.HasPrefix(v, "  ") {
		t.Errorf("continuation = %q; want a two-column hanging indent under the text", v)
	}
}

func TestFencedCodeBlock(t *testing.T) {
	th := newTheme()
	out := renderMarkdownBody(th, "```go\nfmt.Println()\n```", 40)
	if len(out) != 1 {
		t.Fatalf("got %d lines, want 1 (fence lines dropped)", len(out))
	}
	if v := strip(out[0]); v != "  fmt.Println()" {
		t.Errorf("code line visible = %q; want %q (indented, no fence)", v, "  fmt.Println()")
	}
	for _, ln := range out {
		if strings.Contains(ln, "```") {
			t.Errorf("a fence marker leaked into the output: %q", ln)
		}
	}
	if colorActive(th) && !strings.Contains(out[0], "\x1b") {
		t.Errorf("code block emitted no styling: %q", out[0])
	}
}

// An unterminated fence (still streaming) renders the body it has, with no leaked fence marker.
func TestFencedCodeBlockUnterminated(t *testing.T) {
	th := newTheme()
	out := renderMarkdownBody(th, "```\ncode line", 40)
	if v := strip(out[0]); v != "  code line" {
		t.Errorf("visible = %q; want %q", v, "  code line")
	}
	for _, ln := range out {
		if strings.Contains(ln, "```") {
			t.Errorf("a fence marker leaked: %q", ln)
		}
	}
}

// A run of two or more blank lines collapses to a single blank row (the paragraph break stays,
// the padding goes); one blank line is left exactly as it was.
func TestCollapsesBlankRuns(t *testing.T) {
	th := newTheme()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single blank kept", "first\n\nsecond", []string{"first", "", "second"}},
		{"triple blank collapses", "first\n\n\n\nsecond", []string{"first", "", "second"}},
		{"whitespace-only lines count as blank", "first\n \n\t\nsecond", []string{"first", " ", "second"}},
		{"leading run collapses", "\n\n\nfirst", []string{"", "first"}},
		{"all blank is one blank line", "\n\n\n", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderMarkdownBody(th, tc.in, 40)
			if len(out) != len(tc.want) {
				t.Fatalf("renderMarkdownBody(%q) = %#v; want %#v", tc.in, visible(out), tc.want)
			}
			for i := range out {
				if v := strip(out[i]); v != tc.want[i] {
					t.Errorf("line %d visible = %q; want %q", i, v, tc.want[i])
				}
			}
		})
	}
}

// Blank lines inside a fenced code block are code, not padding: they survive verbatim, and a
// blank line immediately after the closing fence is not swallowed by one inside it.
func TestFencedCodeBlockKeepsBlankLines(t *testing.T) {
	th := newTheme()
	out := renderMarkdownBody(th, "```go\na()\n\n\nb()\n```\n\ntail", 40)
	want := []string{"  a()", "  ", "  ", "  b()", "", "tail"}
	if len(out) != len(want) {
		t.Fatalf("got %#v; want %#v (fence interior verbatim)", visible(out), want)
	}
	for i := range out {
		if v := strip(out[i]); v != want[i] {
			t.Errorf("line %d visible = %q; want %q", i, v, want[i])
		}
	}
}

// visible is the ANSI-stripped text of every rendered line — the failure-message form of a block.
func visible(lines []string) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = strip(ln)
	}
	return out
}

// Plain text with no markup is returned byte-identical (the no-regression property that keeps
// existing assistant-text assertions green).
func TestPlainTextUnchanged(t *testing.T) {
	th := newTheme()
	const in = "just plain assistant text"
	out := renderMarkdownBody(th, in, 80)
	if len(out) != 1 || out[0] != in {
		t.Errorf("renderMarkdownBody(%q) = %#v; want a single unchanged line", in, out)
	}
}

// Every rendered line stays within the content width, even when a bold span crosses a soft-wrap
// boundary — the guarantee that baked-in ANSI never perturbs the wrap arithmetic.
func TestWidthNeverExceeds(t *testing.T) {
	th := newTheme()
	const width = 20
	body := "**" + strings.Repeat("alpha ", 10) + "**bold tail"
	for _, ln := range renderMarkdownBody(th, body, width) {
		if w := lipgloss.Width(ln); w > width {
			t.Errorf("line %q has visible width %d > %d", strip(ln), w, width)
		}
	}
}

func TestEmptyMessageRendersOneLine(t *testing.T) {
	th := newTheme()
	if out := renderMarkdownBody(th, "", 40); len(out) != 1 || strip(out[0]) != "" {
		t.Errorf("empty message = %#v; want a single empty line (so its marker shows)", out)
	}
}

func TestWithMarker(t *testing.T) {
	got := withMarker(glyphAssistant+" ", []string{"first", "second"})
	if got[0] != glyphAssistant+" first" {
		t.Errorf("line 0 = %q; want the marker prepended", got[0])
	}
	if got[1] != "  second" {
		t.Errorf("line 1 = %q; want a two-column hanging indent", got[1])
	}
	if one := withMarker(glyphAssistant+" ", nil); len(one) != 1 || one[0] != glyphAssistant+" " {
		t.Errorf("empty body = %#v; want a single marker-only line", one)
	}
}
