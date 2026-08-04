package tui

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The session's name on the top rule (session-name-on-the-top-rule plan, item 2)
// ----------------------------------------------------------------------------
//
// Three things are pinned here, and only one of them is cosmetic. The shape —
// `▔▔▔▔ the name ▔▔▔▔`, the two spaces always present — is the owner's ratified spelling. The second
// is geometry: the row is squared to the window in CELLS (ADR 0030), so every case asserts that the
// three pieces sum to exactly w whatever the name is, and the pieces are asserted explicitly rather
// than re-derived, which is the only way a centering or clipping change shows up as a failure. The
// third is a security property: the name reaching this seam is untrusted (a model's reply to the
// naming call, a pasted /rename, a record's Meta.Title read back off disk), and here a control
// character breaks the row's measure as well as the trust, so no case may leave one behind.

// assertSessionRuleRow fails when the three pieces are not a rule row: the runs must be nothing but
// ▔, the label must be a name bracketed by its two spaces, the whole row must measure exactly w, and
// no rune of it may be a control character.
func assertSessionRuleRow(t *testing.T, measure widthAuthority, lead, label, trail string, w int) {
	t.Helper()
	if got := measure.Width(lead + label + trail); got != w {
		t.Errorf("row measures %d cells, want exactly %d — every row is squared to the window", got, w)
	}
	for _, run := range []struct {
		what string
		s    string
	}{{"lead", lead}, {"trail", trail}} {
		if strings.Trim(run.s, sessionRuleRune) != "" {
			t.Errorf("%s run = %q, want nothing but %s runes", run.what, run.s, sessionRuleRune)
		}
	}
	if label != "" {
		if !strings.HasPrefix(label, " ") || !strings.HasSuffix(label, " ") {
			t.Errorf("label = %q, want the name bracketed by one space on each side", label)
		}
		if strings.Contains(strings.Trim(label, " "), sessionRuleRune) {
			t.Errorf("label = %q, want the name only — the rule runes belong to the runs", label)
		}
	}
	for _, r := range lead + label + trail {
		if unicode.IsControl(r) {
			t.Errorf("row carries control rune %U: %q", r, lead+label+trail)
		}
	}
}

// TestSessionRuleLayout pins the pure geometry: what a name becomes on the rule at a given width.
func TestSessionRuleLayout(t *testing.T) {
	t.Parallel()

	// The painter's default measure (ansi.WcWidth), which is what the layout runs under until a
	// terminal answers mode 2027 — so the CJK case below is clipped in the measure that actually
	// reaches most terminals.
	measure := newWidthAuthority()
	rule := func(n int) string { return strings.Repeat(sessionRuleRune, n) }

	cases := []struct {
		what      string
		in        string
		w         int
		wantLead  string
		wantLabel string
		wantTrail string
	}{
		{
			// No placeholder: the rule is a rule that CARRIES a name when there is one.
			what:     "an unnamed session gets an unbroken rule",
			in:       "",
			w:        20,
			wantLead: rule(20),
		},
		{
			what:      "a plain short name sits between two runs",
			in:        "name",
			w:         20,
			wantLead:  rule(7),
			wantLabel: " name ",
			wantTrail: rule(7),
		},
		{
			// An odd fill leaves the extra column to the RIGHT, so the name sits one column left of
			// dead centre. Stated in sessionRuleLayout's doc comment, pinned here.
			what:      "an odd fill puts the extra column in the trail run",
			in:        "name",
			w:         21,
			wantLead:  rule(7),
			wantLabel: " name ",
			wantTrail: rule(8),
		},
		{
			// The case a fixed cap would get wrong: 40 cells of name in a 100-cell window is shown
			// WHOLE, and carries no ellipsis. It is the runs that flex, not the name that shrinks.
			what:      "a name that fits is never touched",
			in:        strings.Repeat("n", 40),
			w:         100,
			wantLead:  rule(29),
			wantLabel: " " + strings.Repeat("n", 40) + " ",
			wantTrail: rule(29),
		},
		{
			// The clip boundary from both sides. At exactly w-8 the name is whole and each run is the
			// three-cell minimum; one cell past it the name is cut to w-8 with the … counted INSIDE
			// that, so the ellipsis is chrome the room does not pay for and the runs stay at three.
			what:      "a name exactly at the room the rule leaves is whole",
			in:        strings.Repeat("n", 12),
			w:         20,
			wantLead:  rule(sessionRuleMinSegment),
			wantLabel: " " + strings.Repeat("n", 12) + " ",
			wantTrail: rule(sessionRuleMinSegment),
		},
		{
			what:      "a name past that room is clipped to it, ellipsis included",
			in:        strings.Repeat("n", 13),
			w:         20,
			wantLead:  rule(sessionRuleMinSegment),
			wantLabel: " " + strings.Repeat("n", 11) + "… ",
			wantTrail: rule(sessionRuleMinSegment),
		},
		{
			// ADR 0030's point, and the case a rune-based clip gets wrong: nine wide glyphs are 18
			// CELLS, so only five of them survive a 12-cell room — a rune clip would have kept eleven
			// and overrun the window by six columns.
			what:      "a CJK name is clipped in cells, not runes",
			in:        "日本語日本語日本語",
			w:         20,
			wantLead:  rule(sessionRuleMinSegment),
			wantLabel: " 日本語日本… ",
			wantTrail: rule(4),
		},
		{
			// Untrusted twice over: an OSC payload, a bare BEL, a bare ESC and a CSI colour all leave
			// the name entirely (title.StripEscapes), and what is left is one row of text.
			what:      "escape sequences and control characters do not survive",
			in:        "the\x1b]2;pwned\x07 wave\x1b[31m\x07\x1b",
			w:         30,
			wantLead:  rule(10),
			wantLabel: " the wave ",
			wantTrail: rule(10),
		},
		{
			// Whitespace controls are exempt from the strip and collapse here instead, so a pasted
			// multi-line name occupies ONE row rather than smuggling a newline into the frame.
			what:      "a tab and a newline collapse to single spaces",
			in:        "two\tlines\nhere",
			w:         30,
			wantLead:  rule(7),
			wantLabel: " two lines here ",
			wantTrail: rule(7),
		},
		{
			// The strip is allowed to empty a name, and an emptied name is no name: the row refuses
			// rather than showing a lone pair of spaces.
			what:     "a name that is nothing but controls leaves a plain rule",
			in:       "\x07\x1b[31m\x1b]2;x\x07",
			w:        20,
			wantLead: rule(20),
		},
		{
			what:     "a name that is nothing but whitespace leaves a plain rule",
			in:       "  \t \n ",
			w:        20,
			wantLead: rule(20),
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()

			lead, label, trail := sessionRuleLayout(measure, c.in, c.w)

			if lead != c.wantLead || label != c.wantLabel || trail != c.wantTrail {
				t.Errorf("layout(%q, %d) = (%q, %q, %q), want (%q, %q, %q)",
					c.in, c.w, lead, label, trail, c.wantLead, c.wantLabel, c.wantTrail)
			}
			if strings.Contains(label, "…") && !strings.Contains(c.wantLabel, "…") {
				t.Errorf("label = %q carries an ellipsis a name that fits must never earn", label)
			}
			assertSessionRuleRow(t, measure, lead, label, trail, c.w)
		})
	}
}

// TestSessionRuleLayoutWidths sweeps the widths a window can actually be, including the ones a
// naive implementation panics on (strings.Repeat on a negative count) and the degradation boundary:
// below sessionRuleMinName cells of room a name is an ellipsis with a hint, so the row gives up the
// name and stays a rule. w = 13 leaves five cells of room and degrades; w = 14 leaves six and
// carries a six-cell name between two three-cell runs.
func TestSessionRuleLayoutWidths(t *testing.T) {
	t.Parallel()

	measure := newWidthAuthority()
	const name = "the hairline wave"

	for _, w := range []int{0, 1, 5, 13, 14, 200} {
		lead, label, trail := sessionRuleLayout(measure, name, w)
		assertSessionRuleRow(t, measure, lead, label, trail, w)

		switch {
		case w < 14:
			if label != "" || lead != strings.Repeat(sessionRuleRune, w) {
				t.Errorf("w = %d gave (%q, %q, %q), want the whole row as an unbroken rule",
					w, lead, label, trail)
			}
		case w == 14:
			wantLead := strings.Repeat(sessionRuleRune, sessionRuleMinSegment)
			if lead != wantLead || trail != wantLead {
				t.Errorf("w = 14 runs = (%q, %q), want %d cells each side",
					lead, trail, sessionRuleMinSegment)
			}
			if got := measure.Width(strings.Trim(label, " ")); got != sessionRuleMinName {
				t.Errorf("w = 14 name = %q (%d cells), want the %d-cell floor",
					label, got, sessionRuleMinName)
			}
		default:
			if strings.Trim(label, " ") != name {
				t.Errorf("w = %d name = %q, want the whole %q — 200 cells has room to spare",
					w, label, name)
			}
		}
	}
}

// TestTopRuleCarriesSessionName pins the composed row on the Model: which name it wears at each
// stage a session acquires one, and that naming it never costs the row its width or its edges.
func TestTopRuleCarriesSessionName(t *testing.T) {
	t.Parallel()

	t.Run("an unnamed session", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)

		if got, want := ansi.Strip(m.topRule()), strings.Repeat(sessionRuleRune, m.width); got != want {
			t.Errorf("top rule = %q, want the plain %d-cell rule: nothing has named this session", got, m.width)
		}
	})

	t.Run("a named session", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)
		const name = "the hairline wave"
		m.nameSession(name)

		row := ansi.Strip(m.topRule())
		if !strings.Contains(row, name) {
			t.Errorf("top rule = %q, want it to carry the session's name %q", row, name)
		}
		if got := m.th.measure.Width(row); got != m.width {
			t.Errorf("named rule measures %d cells, want the full %d", got, m.width)
		}
		if !strings.HasPrefix(row, sessionRuleRune) || !strings.HasSuffix(row, sessionRuleRune) {
			t.Errorf("top rule = %q, want it to open and close on the rule's own %s", row, sessionRuleRune)
		}
	})

	t.Run("a first prompt names the rule before any naming call answers", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)
		const prompt = "fix the broken parser in tokenizer.go"
		m.transcript.addUser(prompt, nil)

		if row := ansi.Strip(m.topRule()); !strings.Contains(row, prompt) {
			t.Errorf("top rule = %q, want the heuristic name %q derived from the opening request",
				row, prompt)
		}
	})

	t.Run("clear returns the row to a plain rule", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)
		m.transcript.addUser("fix the broken parser in tokenizer.go", nil)
		m.nameSession("the hairline wave")

		m, _ = sendPrompt(t, m, "/clear")

		if got, want := ansi.Strip(m.topRule()), strings.Repeat(sessionRuleRune, m.width); got != want {
			t.Errorf("top rule = %q after /clear, want the plain rule: the session it opens is unnamed", got)
		}
	})
}
