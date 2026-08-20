package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The width authority — one display-width measure, and it is the painter's
// ----------------------------------------------------------------------------
//
// Two measures of "how many terminal columns does this string occupy" are in play in any
// bubbletea program, and they disagree.
//
// The LAYOUT side measures grapheme clusters: lipgloss.Width is a per-line max over
// ansi.StringWidth, and every package-level ansi helper this package reaches for — Cut,
// Truncate, Wrap, Wordwrap, Hardwrap — is hard-wired to ansi.GraphemeWidth. That measure
// promotes any cluster carrying VARIATION SELECTOR-16 (U+FE0F) to two cells, so "⚠️" is two.
//
// The PAINT side is the screen buffer bubbletea draws the composed view into, and it starts at
// ansi.WcWidth (the zero value of ansi.Method; ultraviolet/buffer.go:612-617). That measure
// takes the first non-zero-width rune of the cluster — U+FE0F is zero-width and U+26A0 is
// neutral — so the same "⚠️" is ONE cell. bubbletea moves the painter to ansi.GraphemeWidth
// only when a terminal answers its start-up mode-2027 (Unicode core) query
// (bubbletea/v2@v2.0.7/tea.go:786-798), and it does not even ask on Apple Terminal or over SSH.
//
// A frame laid out in one measure and painted in the other is off by a column per such
// grapheme, and the drift is not cosmetic: the scroll bar lands one column left of where every
// other row put it, a reported mouse column names a different rune than the one under the
// pointer, and a row composed to exactly the block width overruns the absolute cap layout.md
// states.
//
// [widthAuthority] is this package's answer: ONE measure for the whole TUI, and it is whatever
// the painter is actually using. It starts where the painter starts (ansi.WcWidth) and adopts
// ansi.GraphemeWidth at the same message the painter does ([widthAuthority.observe]), so
// measurement agrees with paint on a terminal that answers mode 2027 and on one that never
// does. Measuring in a method the painter did not choose is the defect; following the painter
// is the whole design.
//
// It is a plain value — one ansi.Method, no pointer, no sync type — so it is safe inside the
// value-copied Model (ADR 0011; doc.go). That is what lets it ride on [theme], which is
// already threaded as the first parameter through every free renderer function, and so reach
// the measurement sites without a signature change at each one.
type widthAuthority struct {
	// method is the measure every operation below dispatches on. Its zero value is
	// ansi.WcWidth — which is also the painter's starting measure — so a widthAuthority that
	// was never initialised is already the honest default rather than a wrong one.
	method ansi.Method
}

// newWidthAuthority returns the authority a session starts with: the measure the painter uses
// before any terminal has said otherwise. [widthAuthority.observe] is the only thing that moves
// it from there.
func newWidthAuthority() widthAuthority {
	return widthAuthority{method: ansi.WcWidth}
}

// Method reports the measure the authority currently dispatches on — the same value the
// painter's screen buffer holds. Production code should call the operations below rather than
// branch on this; it exists so a test can paint a frame in exactly the measure the layout used.
func (w widthAuthority) Method() ansi.Method { return w.method }

// observe folds a terminal mode report into the authority and returns the authority to use from
// here on. It mutates nothing: the receiver is a value, like the Model that carries it.
//
// It mirrors bubbletea's own reaction to the very same message, deliberately and exactly. On a
// mode-2027 (Unicode core) report whose setting is anything other than "not recognized" or
// "permanently reset", bubbletea calls renderer.setWidthMethod(ansi.GraphemeWidth) and then
// hands the message on to the model's Update (bubbletea/v2@v2.0.7/tea.go:786-798, which does
// not `continue`) — which is how this package gets to learn what the painter just decided. Any
// other report leaves the measure alone. The switch is one-way, as the renderer's is:
// bubbletea never moves the painter back to WcWidth, so neither does this.
func (w widthAuthority) observe(report tea.ModeReportMsg) widthAuthority {
	if report.Mode != ansi.ModeUnicodeCore {
		return w
	}
	switch report.Value {
	case ansi.ModeSet, ansi.ModeReset, ansi.ModePermanentlySet:
		w.method = ansi.GraphemeWidth
	}
	return w
}

// foldModeReport folds one terminal mode report into the model: the width authority observes it,
// the diag log records what it made of the measure, and a measure that MOVED re-lays the frame out.
//
// This is the Update loop's only reader of the message. Exactly one of the mode queries bubbletea
// sends at start-up concerns the layout: mode 2027 (Unicode core). bubbletea acts on that answer by
// switching the PAINTER to ansi.GraphemeWidth and then passes the same message on to Update
// (bubbletea/v2@v2.0.7/tea.go:786-798), so this fold is the only place apogee can learn which
// measure its own output is about to be painted in. The width authority follows it — measuring in a
// method the painter did not choose is precisely the defect it exists to close. Nothing else in the
// program reads this message; before the authority there was no arm for it and it fell through to
// the input widget, which ignores it.
func (m Model) foldModeReport(msg tea.ModeReportMsg) (tea.Model, tea.Cmd) {
	before := m.th.measure
	m.th.measure = m.th.measure.observe(msg)
	// What the report MADE of the measure, beside the report itself — Update's observation point
	// records the mode number and value, and this is the consequence a rendering bug is
	// actually argued from (diagnostics.go). Change-suppressed, so only the one report that
	// moves it writes a line.
	m.diag.record(diagWidthMethod, widthMethodName(m.th.measure.Method()))
	if m.th.measure != before && m.ready {
		m.layout() // the measure moved: re-wrap and repaint everything against the new one
	}
	return m, nil
}

// The measurement operations. Each one is the authority's stand-in for the package-level ansi
// function of the same name, with the same signature and the same semantics — the only
// difference being that the method is the authority's rather than a hard-wired
// ansi.GraphemeWidth. Keeping the signatures identical is deliberate: converting a call site is
// then a rename, not a rewrite, and nothing about the wrapping or cutting behaviour can drift
// away from the library while the seam is being adopted.

// Width reports how many terminal columns s occupies when painted. ANSI escape sequences count
// for nothing and wide glyphs count for two. It is the replacement for both lipgloss.Width and
// ansi.StringWidth, which measure the same thing under two spellings and neither of which can
// follow the painter.
func (w widthAuthority) Width(s string) int {
	return w.method.StringWidth(s)
}

// Truncate cuts s down to length columns, appending tail when it had to cut. It replaces
// ansi.Truncate.
func (w widthAuthority) Truncate(s string, length int, tail string) string {
	return w.method.Truncate(s, length, tail)
}

// Cut returns the columns [left, right) of s, keeping the styling of the parts it slices
// through intact. It replaces ansi.Cut.
//
// It composes the two truncations itself rather than calling ansi.Method.Cut, which is the one
// operation here that cannot be delegated: x/ansi@v0.11.7's cut binds its LEFT truncation to
// TruncateWc — a right-truncation — on the WcWidth branch (truncate.go:35-39, where the grapheme
// branch correctly uses TruncateLeft), so ansi.Method.Cut(s, left, right) under WcWidth spends
// `left` as a WIDTH rather than as an offset and returns the first `left` columns:
// Cut("abcdef", 2, 5) hands back "ab" where the span is "cde". WcWidth is the painter's DEFAULT,
// and a mouse selection is exactly a cut with a non-zero left, so the mistake would land on every
// terminal that does not answer mode 2027. TestWidthAuthorityCutsFromTheLeft pins the correct
// behaviour; if a dependency bump fixes it upstream, this can go back to delegating.
func (w widthAuthority) Cut(s string, left, right int) string {
	if right <= left {
		return ""
	}
	cut := w.method.Truncate(s, right, "")
	if left <= 0 {
		return cut
	}
	return w.method.TruncateLeft(cut, left, "")
}

// Wrap wraps s to limit columns, breaking at spaces and at breakpoints and hard-breaking a word
// too long to fit. It replaces ansi.Wrap.
func (w widthAuthority) Wrap(s string, limit int, breakpoints string) string {
	return w.method.Wrap(s, limit, breakpoints)
}

// Wordwrap wraps s to limit columns at word boundaries only, leaving a word longer than the
// limit over-long. It replaces ansi.Wordwrap.
func (w widthAuthority) Wordwrap(s string, limit int, breakpoints string) string {
	return w.method.Wordwrap(s, limit, breakpoints)
}

// Hardwrap breaks s at exactly limit columns regardless of word boundaries. It replaces
// ansi.Hardwrap.
func (w widthAuthority) Hardwrap(s string, limit int, preserveSpace bool) string {
	return w.method.Hardwrap(s, limit, preserveSpace)
}
