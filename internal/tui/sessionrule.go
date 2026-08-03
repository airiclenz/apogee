package tui

import (
	"strings"

	"github.com/airiclenz/apogee/internal/title"
)

// ----------------------------------------------------------------------------
// The session's name on the top rule (session-name-on-the-top-rule plan, item 2)
// ----------------------------------------------------------------------------
//
// The frame says which session it is on a row apogee actually paints. The ▔ hairline that already
// caps the bottom chrome (topRule, model.go) carries the session's name centered on it —
// `▔▔▔▔ the name ▔▔▔▔` — in place of the rule runes it would otherwise draw there. It costs the
// frame no row: the rule already existed and already spanned the window, so the name rides a row
// the transcript had already paid for.
//
// This is the route that replaced the terminal window's title, which was withdrawn the same day
// because it reached no tab on any terminal the owner runs. The substantive difference is the
// MEASURE. A title bar is not a cell buffer apogee measures, so that seam clipped in runes; this
// row is painted into the cell buffer, so everything here is measured with the painter's own
// measure (m.th.measure, ADR 0030) and every arithmetic below is in CELLS.
//
// The trust posture, unchanged from that seam and for the same reason: the name is untrusted TWICE
// over — a model's reply to the naming call, and a stored record's Meta.Title read back off disk,
// which nothing sanitizes on the way in. It goes through internal/title's strong strip
// (title.StripEscapes: whole escape sequences AND every non-whitespace control character) and then
// through strings.Fields/Join, so a pasted multi-line name occupies one row rather than smuggling a
// newline into the frame. Here a control character is a LAYOUT bug as much as a security one: it
// breaks the row's measure, and every row of this frame is squared to the window.
//
// Owner-ratified decisions (2026-08-03), which the constants below spell out: there is NO fixed cap
// on the name — the only cap is the room the rule leaves, so a name is shown whole whenever it fits
// and is clipped only when it would not; an unnamed session gets an unbroken rule rather than an
// `apogee` placeholder, because the frame is already unmistakably apogee's from every other row;
// and the rule carries NO status decoration — no spinner, no clock, no working marker. It states an
// IDENTITY, and everything that changes every frame already has a home in the chrome below it.

// sessionRuleMinSegment is the ▔ run that stays on each side of the name, and with the space beside
// it fixes the name's room at w - 2*(sessionRuleMinSegment+1). Three cells and a space each side is
// what makes the row read as a rule CARRYING a name rather than as a caption with two stubs.
const sessionRuleMinSegment = 3

// sessionRuleMinName is the narrowest name worth showing: below six cells a name is not a name, it
// is an ellipsis with a hint, so a window that cannot spare that much gets a plain rule instead.
const sessionRuleMinName = 6

// sessionRuleRune is the rule's own glyph — the upper one-eighth block the top hairline is drawn
// from (topRule), which the name is set into rather than drawn beside.
const sessionRuleRune = "▔"

// sessionRuleName is the name the top rule wears, or "" for a session that has none. Two answers,
// in the order a session acquires them: the name a naming call or a rename decided (sessionName,
// autotitle.go), or the heuristic title derived from the first thing the human asked.
//
// The heuristic branch is what names the rule from the opening request the INSTANT it is sent,
// hours before any naming call can answer — and it is deliberately re-derived per frame rather than
// cached: firstUserText walks to the first user entry and stops, which is far cheaper than the
// frame it rides on, while a cached copy would be one more thing /clear could leave stale.
//
// The gate on that branch is "the transcript has a first user text", not "sessionTitle returned
// something": sessionTitle answers a dated `Session <date>` fallback for a session that has said
// nothing, and a rule naming the calendar would be worse than a rule naming nothing. A session that
// HAS spoken keeps whatever sessionTitle makes of it, dated fallback included — that is the title
// its record carries, and the rule agreeing with the browser is the point.
func (m Model) sessionRuleName() string {
	if m.sessionName != "" {
		return m.sessionName
	}
	if first := m.transcript.firstUserText(); first != "" {
		return sessionTitle(first)
	}
	return ""
}

// sessionRuleLayout splits one rule row of width w into its three pieces: the ▔ run before the
// label, the label itself (the two spaces included), and the ▔ run after it. An unnamed session —
// or a window too narrow to show a name honestly — returns the whole row as lead, an empty label
// and an empty trail. It is pure, so the geometry is testable without a theme, and it is the one
// place the row's arithmetic lives.
//
// The two spaces are always present so the name never touches a rule rune. The name is stripped and
// its whitespace runs collapsed (the trust posture above), then clipped — with an ellipsis, and in
// CELLS — to the room the rule leaves and to nothing else: w - 2*(sessionRuleMinSegment+1), which
// is w - 8. A name that fits is therefore never touched, however long it is.
//
// Centering leaves the extra column to the RIGHT: fill is what the label does not spend, the lead
// run takes fill/2 and the trail run the remainder, so on an odd fill the name sits one column left
// of dead centre. A reader who counts columns would otherwise take that for a bug.
//
// The three-cell guarantee falls straight out of the clip and needs no second check: a name at
// exactly the clip leaves fill == 2*sessionRuleMinSegment, which is three cells to each run, and
// every shorter name leaves more.
func sessionRuleLayout(measure widthAuthority, name string, w int) (lead, label, trail string) {
	if w <= 0 {
		// Reachable before the first WindowSizeMsg, and strings.Repeat panics on a negative count.
		return "", "", ""
	}
	clean := strings.Join(strings.Fields(title.StripEscapes(name)), " ")
	room := w - 2*(sessionRuleMinSegment+1)
	if clean == "" || room < sessionRuleMinName {
		return strings.Repeat(sessionRuleRune, w), "", ""
	}
	clean = measure.Truncate(clean, room, "…")
	fill := w - measure.Width(clean) - 2
	lead = strings.Repeat(sessionRuleRune, fill/2)
	trail = strings.Repeat(sessionRuleRune, fill-fill/2)
	return lead, " " + clean + " ", trail
}
