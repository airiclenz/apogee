package tui

import (
	"strings"

	"github.com/airiclenz/apogee/internal/title"
)

// ----------------------------------------------------------------------------
// The terminal window's title (session-name-window-title plan, item 2)
// ----------------------------------------------------------------------------
//
// The frame names the window it is drawn in: `✭ <name>`, the star first and always, one space, then
// the session's name clipped to windowTitleRunes. Every row of the frame is already spoken for
// (layout.md), and the session's name is an IDENTITY fact rather than a live one, so it belongs on
// the chrome the terminal owns rather than on a row the transcript would have to pay for.
//
// How it reaches the terminal: Bubble Tea carries the string on the frame (tea.View.WindowTitle) and
// the renderer emits it as OSC 2 (`ESC ] 2 ; <title> BEL`) — but only when the string CHANGES, so
// View may set it every frame at no cost, and apogee itself never writes an escape sequence. On exit
// the renderer blanks the title rather than restoring it, which an interactive shell re-stamps at its
// next prompt. Terminals that ignore OSC 2 (and the ones whose tab label is configured to something
// else, VS Code most notably) are simply unaffected: nothing here is load-bearing for the frame.
//
// The trust posture, which is the reason this file exists as a seam rather than a string concat: the
// name is untrusted TWICE over. It can be a model's reply to the naming call, and it can be a stored
// record's Meta.Title read back off disk, which nothing sanitizes on the way in. Inside an OSC
// payload a BEL is not cosmetic — it TERMINATES the sequence, so a name carrying one would end the
// escape early and hand everything after it to the terminal to execute. That is why formatWindowTitle
// runs the name through internal/title's strong strip (title.StripEscapes: whole escape sequences and
// every non-whitespace control character) and not through this package's own stripEscapes, which
// drops the ESC byte alone and would let a bare BEL through. One definition of "a title carries no
// control character", used by both of its readers.
//
// Owner-ratified decisions (2026-08-03), which the constants below spell out: the star leads always;
// the name is clipped to 30 runes with the star and its space as chrome on top; a session that has
// said nothing yet is `✭ apogee`, so an apogee window is identifiable before it has a name; and the
// title carries NO status decoration — no spinner, no working marker. It says which session this
// window is, and nothing that changes every frame.

// windowTitleRunes bounds the NAME the window title carries; the star and its space are chrome on
// top of it. RUNES, not cells: the title never enters the cell buffer — the terminal lays it out in
// its own title bar, where apogee has no measure to take — so ADR 0030's m.th.measure rule does not
// reach here, and the rune-counting clipRunes is the right cap.
const windowTitleRunes = 30

// windowTitleMark leads every title apogee sets, named or not, so a window that belongs to this
// program says so at a glance.
const windowTitleMark = "✭ "

// windowTitleUnnamed is what a session that has said nothing yet is called — the program's own name,
// since there is no work to name it after.
const windowTitleUnnamed = "apogee"

// windowTitle is the name this window wears on the frame being built. Three answers, in the order a
// session acquires them: the name a naming call or a rename decided (sessionName, autotitle.go), the
// heuristic the first Save stamps on the record, or the program's name.
//
// The heuristic branch is what names a window from the human's opening request the INSTANT it is
// sent, hours before any naming call can answer — and it is deliberately re-derived per frame rather
// than cached: firstUserText walks to the first user entry and stops, which is far cheaper than the
// frame it rides on, while a cached copy would be one more thing /clear could leave stale.
//
// The gate on that branch is "the transcript has a first user text", not "sessionTitle returned
// something": sessionTitle answers a dated `Session <date>` fallback for a session that has said
// nothing, and a window reading `✭ Session 2026-08-03` would name the calendar instead of the
// session. A session that HAS spoken keeps whatever sessionTitle makes of it, dated fallback
// included — that is the title its record carries, and the window agreeing with the browser is the
// point.
func (m Model) windowTitle() string {
	if m.sessionName != "" {
		return formatWindowTitle(m.sessionName)
	}
	if first := m.transcript.firstUserText(); first != "" {
		return formatWindowTitle(sessionTitle(first))
	}
	return formatWindowTitle(windowTitleUnnamed)
}

// formatWindowTitle renders one name as the title the terminal is handed: stripped of escape
// sequences and control characters (title.StripEscapes — the seam's whole security posture, see the
// note above), whitespace runs collapsed to single spaces so a pasted multi-line name occupies one
// title rather than smuggling a newline into the payload, clipped to windowTitleRunes, and led by
// the star.
//
// A name that nothing survives falls back to windowTitleUnnamed instead of leaving the star standing
// alone: the strip and the collapse are both allowed to empty a string — an all-control name does
// exactly that — and a window that went from naming the session to naming nothing would read as a
// bug rather than as the refusal it is.
func formatWindowTitle(name string) string {
	clean := strings.Join(strings.Fields(title.StripEscapes(name)), " ")
	if clean == "" {
		clean = windowTitleUnnamed
	}
	return windowTitleMark + clipRunes(clean, windowTitleRunes)
}
