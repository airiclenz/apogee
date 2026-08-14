package tui

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The one-time start-up box (version-command-and-startup-box plan, item 3)
// ----------------------------------------------------------------------------

// lineWithLogoAnd reports whether any rendered (ANSI-stripped) line carries both a distinctive
// logo fragment and the given substring — i.e. whether the logo and that text share a physical row.
// It is the mechanical side-by-side / stacked discriminator the two start-up-box tests pivot on.
func lineWithLogoAnd(lines []string, sub string) bool {
	t := false
	for _, ln := range lines {
		p := ansi.Strip(ln)
		if strings.Contains(p, "▗▄▄▖▗▄▄") && strings.Contains(p, sub) {
			t = true
		}
	}
	return t
}

// When there is horizontal room the start-up box uses the WIDE layout: the logo on the left and a
// right-aligned host / model / context / version block on the right, inside a rounded card that
// reuses the prompt box's border glyphs but drops its black fill. The assertions are the layout's
// acceptance made mechanical: (a) the logo art is present, (b) all FOUR session facts (incl. the new
// context) with their dim labels are present, (c) the logo and the info block share physical rows
// (side by side — the wide layout, not stacked), (d) the widest info row is flush against the right
// padding (the block is right-aligned), (e) the rounded corners match the prompt box, (f) the card
// carries none of the black-background SGR the input box emits, and (g) every line spans the full
// content width, top and bottom closing on the rounded corner at that edge.
func TestRenderStartupBox(t *testing.T) {
	th := newTheme(scheme.Default())
	v := startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    "test-host:1111", // the widest value → its row is the one flushed right
		Model:   "gpt-oss-20b",
		Context: "32k",
		Version: "v9.9.9-test",
	}
	const width = 80 // ample room: inner 76 ≥ logo 36 + gap 4 + info 23, so the wide layout engages
	lines := renderStartupBox(th, v, width)
	raw := strings.Join(lines, "\n")
	plain := ansi.Strip(raw)

	// (a) a distinctive fragment of the block-art wordmark survives into the card.
	if !strings.Contains(plain, "▗▄▄▖▗▄▄") {
		t.Errorf("startup box does not carry the logo art:\n%s", plain)
	}
	// (b) all four session facts, each with its dim label, are present.
	for _, want := range []string{"host", v.Host, "model", v.Model, "context", v.Context, "version", v.Version} {
		if !strings.Contains(plain, want) {
			t.Errorf("startup box missing %q:\n%s", want, plain)
		}
	}
	// (c) the logo and the info block sit on the same rows — the wide (side-by-side) layout, not the
	// stacked fallback where the logo lines stand alone above the facts.
	if !lineWithLogoAnd(lines, "host") {
		t.Errorf("wide startup box does not place the info block beside the logo:\n%s", plain)
	}
	// (d) the info block is right-aligned: the widest row (host) ends flush against the right padding,
	// so its line closes on the value then the one-column padding and the border.
	if want := v.Host + " │"; !strings.Contains(plain, want) {
		t.Errorf("wide startup box is not right-aligned — no line ends %q (flush to the right padding):\n%s", want, plain)
	}
	// (e) the rounded corners match the prompt box's RoundedBorder glyphs.
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(plain, corner) {
			t.Errorf("startup box missing rounded corner %q:\n%s", corner, plain)
		}
	}
	// (f) none of the surface-background SGR the input box paints. Extract it from a real
	// Background(surface) render, so the check tracks whatever colour profile lipgloss uses rather
	// than a hard-coded escape.
	probe := lipgloss.NewStyle().Background(lipgloss.Color(scheme.Default().Surface)).Render("x")
	if !strings.Contains(probe, "\x1b") {
		t.Fatal("the black-background probe rendered no escape; the colour profile hides the SGR this test relies on")
	}
	blackBG := probe[:strings.IndexByte(probe, 'm')+1] // the leading SGR, up to and including its 'm'
	if strings.Contains(raw, blackBG) {
		t.Errorf("startup box carries the input box's black-background SGR %q — it must be transparent", blackBG)
	}
	// (g) every rendered line — border runes included — is exactly the content width it was handed,
	// so the right border aligns to the same column the rest of the transcript's content ends at.
	// The top and bottom rows close on the rounded corner at that edge.
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("startup box line %d is %d cols, want the full content width %d: %q", i, w, width, ansi.Strip(ln))
		}
	}
	if top := ansi.Strip(lines[0]); !strings.HasSuffix(top, "╮") {
		t.Errorf("top row does not close on ╮ at the content edge: %q", top)
	}
	if bot := ansi.Strip(lines[len(lines)-1]); !strings.HasSuffix(bot, "╯") {
		t.Errorf("bottom row does not close on ╯ at the content edge: %q", bot)
	}
}

// When the width cannot fit the logo, a gap, and the info block side by side, the start-up box falls
// back to the STACKED layout — the card's original shape: the logo, a blank line, then host / model /
// version below it, and (by owner decision) NO context row. The assertions: (a) the three fallback
// facts are present, (b) the context fact is absent (it lives only in the wide layout), (c) the logo
// and the facts are on SEPARATE rows (stacked, not side by side), (d) the card still spans the full
// content width with rounded corners.
func TestRenderStartupBoxStackedFallback(t *testing.T) {
	th := newTheme(scheme.Default())
	v := startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    "test-host:1111",
		Model:   "gpt-oss-20b",
		Context: "32k",
		Version: "v9.9.9-test",
	}
	const width = 50 // inner 46 < logo 36 + gap 4 + info 23 → the wide layout does not fit, so stacked
	lines := renderStartupBox(th, v, width)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	// (a) the three stacked facts, each with its dim label, are present.
	for _, want := range []string{"host", v.Host, "model", v.Model, "version", v.Version} {
		if !strings.Contains(plain, want) {
			t.Errorf("stacked startup box missing %q:\n%s", want, plain)
		}
	}
	// (b) the context row does not appear in the fallback (context is wide-layout-only).
	for _, absent := range []string{"context", v.Context} {
		if strings.Contains(plain, absent) {
			t.Errorf("stacked startup box carries %q — context belongs only to the wide layout:\n%s", absent, plain)
		}
	}
	// (c) the logo and the facts are stacked, not side by side: no line carries both the logo and a
	// fact label.
	if lineWithLogoAnd(lines, "host") {
		t.Errorf("stacked startup box put a fact beside the logo — expected the stacked layout:\n%s", plain)
	}
	// (d) full-width card with rounded corners.
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("stacked startup box line %d is %d cols, want the full content width %d: %q", i, w, width, ansi.Strip(ln))
		}
	}
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(plain, corner) {
			t.Errorf("stacked startup box missing rounded corner %q:\n%s", corner, plain)
		}
	}
}
