package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Markdown rendering for assistant chat text
// ----------------------------------------------------------------------------
//
// Assistant messages arrive as markdown source; this file turns the small, common subset the
// transcript needs into styled physical lines: **bold**, # headings (bold white), `inline code`
// and ``` fenced blocks ``` (orange), and bullet/numbered lists. It is a deliberately spare,
// lipgloss-only renderer (no syntax highlighting, no external dependency) — pure and
// table-testable, matching toolpresent.go's posture; render.go owns the marker and depth framing.
//
// Two properties keep it safe inside the existing line-oriented renderer. The styling is baked
// into the text as ANSI before wrapping, and ansi.Wrap/Hardwrap are SGR-aware (they re-emit a
// style across a soft-wrap boundary), so word-wrap arithmetic is unperturbed (render.go:204);
// and lipgloss.Width strips ANSI, so the sticky-header scroll math (wrappedOffset) still measures
// visible columns. Unterminated markup (a ` or ** or fence still streaming in) degrades to
// literal text, so a partially-streamed message never leaks an escape or breaks the layout.

// renderMarkdownBody renders an assistant message's markdown into styled physical lines at the
// content width — the columns inside the ✦ marker gutter; the caller re-attaches the marker via
// withMarker. It walks the text block by block: fenced code blocks, ATX headings, bullet/numbered
// list items, then plain paragraphs. An empty message yields one empty line so its marker still
// shows (matching the old wrapText("") behaviour for a just-opened assistant buffer).
func renderMarkdownBody(th theme, text string, width int) []string {
	if width < 1 {
		width = 1
	}
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case isFence(line):
			// Collect the body until the closing fence or EOF (an unterminated fence — mid-stream
			// — still renders what has arrived). The fence lines themselves are dropped.
			var code []string
			j := i + 1
			for ; j < len(lines) && !isFence(lines[j]); j++ {
				code = append(code, lines[j])
			}
			out = append(out, renderCodeBlock(th, code, width)...)
			if j < len(lines) {
				i = j // skip the closing fence
			} else {
				i = j - 1 // EOF: there is no closing fence to skip
			}
		case isHeading(line):
			out = append(out, renderHeadingLine(th, headingText(line), width)...)
		default:
			if li, ok := matchListItem(line); ok {
				out = append(out, renderListItem(th, li, width)...)
				continue
			}
			out = append(out, renderParagraphLine(th, line, width)...)
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// renderParagraphLine styles one ordinary line's inline spans and word-wraps it to width,
// preserving the line's own break. A blank line stays blank (one empty line).
func renderParagraphLine(th theme, line string, width int) []string {
	return wrapText(renderInline(th, line), width)
}

// renderHeadingLine renders a heading: its text bold white, word-wrapped to width. Inline markup
// inside a heading is not re-parsed — headings rarely carry it, and a nested span's reset would
// cut the heading's own SGR run short.
func renderHeadingLine(th theme, text string, width int) []string {
	wrapped := wrapText(text, width)
	out := make([]string, len(wrapped))
	for i, seg := range wrapped {
		out[i] = th.mdHeading.Render(seg)
	}
	return out
}

// renderListItem renders a list item: its marker (bullet glyph or source number) plus the
// inline-styled item text, word-wrapped with a hanging indent so continuation lines align under
// the text (hangingPrefixes leaves the marker unstyled and is ANSI-aware for the styled text).
func renderListItem(th theme, li listItem, width int) []string {
	return hangingPrefixes(li.indent+li.marker, renderInline(th, li.text), width)
}

// renderCodeBlock renders a fenced code block's body: each source line indented two columns and
// styled in the code colour, hard-wrapped (not reflowed) so the code's own line structure
// survives. A blank source line stays blank.
func renderCodeBlock(th theme, code []string, width int) []string {
	const indent = "  "
	cw := max(1, width-len(indent))
	var out []string
	for _, cl := range code {
		for _, seg := range strings.Split(ansi.Hardwrap(cl, cw, true), "\n") {
			out = append(out, indent+th.mdCodeBlock.Render(seg))
		}
	}
	return out
}

// renderInline styles the inline spans in one logical line: `code` (orange) and **bold**. Code
// spans win over bold (no bold is parsed inside a code span), matching CommonMark's code-span
// precedence. An unterminated ` or ** is emitted literally so a mid-stream line renders cleanly.
func renderInline(th theme, s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString(th.mdCode.Render(s[i+1 : i+1+end]))
				i += 1 + end + 1
				continue
			}
			b.WriteByte(s[i])
			i++
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				b.WriteString(th.mdBold.Render(s[i+2 : i+2+end]))
				i += 2 + end + 2
				continue
			}
			b.WriteString("**")
			i += 2
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// withMarker re-attaches a leading marker to an already-rendered block: the marker leads the
// first line and a same-width blank indent leads the rest, so a multi-line assistant body stays
// aligned under its ✦ marker (the hanging indent of layout.md). It is hangingPrefixes for lines
// that are already wrapped and styled.
func withMarker(marker string, lines []string) []string {
	if len(lines) == 0 {
		lines = []string{""}
	}
	indent := strings.Repeat(" ", lipgloss.Width(marker))
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i == 0 {
			out[i] = marker + ln
		} else {
			out[i] = indent + ln
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// Block matchers
// ----------------------------------------------------------------------------

// isFence reports whether line opens or closes a fenced code block: ``` (optionally followed by a
// language tag) after any leading spaces.
func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), "```")
}

// isHeading reports whether line is an ATX heading: one to six # then a space.
func isHeading(line string) bool {
	t := strings.TrimLeft(line, " ")
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && n < len(t) && t[n] == ' '
}

// headingText returns a heading line's text with the # markers (and surrounding space) stripped.
func headingText(line string) string {
	return strings.TrimSpace(strings.TrimLeft(line, " #"))
}

// listItem is a parsed list line: any leading indentation, the marker to show ("• " for a bullet
// or the source number plus its "." / ")" for an ordered item), and the item's text.
type listItem struct {
	indent string
	marker string
	text   string
}

// matchListItem reports whether line is a bullet (- * +) or numbered (1. / 1)) item and returns
// its rendered marker and text. Leading whitespace is preserved so nested items keep their indent.
func matchListItem(line string) (listItem, bool) {
	rest := strings.TrimLeft(line, " ")
	indent := strings.Repeat(" ", len(line)-len(rest))
	// Bullet: a single - * or + followed by a space.
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && rest[1] == ' ' {
		return listItem{indent: indent, marker: glyphBullet + " ", text: strings.TrimLeft(rest[2:], " ")}, true
	}
	// Numbered: one or more digits, then . or ), then a space.
	d := 0
	for d < len(rest) && rest[d] >= '0' && rest[d] <= '9' {
		d++
	}
	if d > 0 && d+1 < len(rest) && (rest[d] == '.' || rest[d] == ')') && rest[d+1] == ' ' {
		return listItem{indent: indent, marker: rest[:d+1] + " ", text: strings.TrimLeft(rest[d+2:], " ")}, true
	}
	return listItem{}, false
}
