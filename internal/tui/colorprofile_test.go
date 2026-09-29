package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// The prompt block's half rows under the terminal's colour profile
// ----------------------------------------------------------------------------

// hasPadRow reports whether any of rows is a prompt block's half row: a run of ▄ or of ▀ and nothing
// else once the styling and the blanks around it are gone. A bare glyph test would not do — the
// start-up box's logo is drawn with the same quadrant glyphs.
func hasPadRow(rows []string) bool {
	for _, row := range rows {
		row = strings.TrimSpace(strip(row))
		if row != "" && (strings.Trim(row, "▄") == "" || strings.Trim(row, "▀") == "") {
			return true
		}
	}
	return false
}

// withPrompt returns m with an answer, a sent prompt and another answer in its transcript,
// repainted through Update's own settle — the transcript write moves the generation, so the next
// message's frameKey compare repaints. The message is a stale flash tick: it changes nothing else.
func withPrompt(t *testing.T, m Model) Model {
	t.Helper()
	m.transcript.commitAssistant("the answer before", runRef{})
	m.transcript.addUser("the prompt", nil)
	m.transcript.commitAssistant("the answer after", runRef{})
	return step(t, m, flashClearMsg{gen: m.flashGen - 1})
}

// promptNeighbours returns the transcript rows directly above and below the prompt's own row, with
// the scroll-bar column and trailing blanks trimmed away.
func promptNeighbours(t *testing.T, m Model) (above, below string) {
	t.Helper()
	rows := transcriptRows(t, m)
	for i, row := range rows {
		if !strings.Contains(row, "the prompt") {
			continue
		}
		if i == 0 || i == len(rows)-1 {
			t.Fatalf("the prompt sits on the transcript's edge row %d:\n%s", i, strings.Join(rows, "\n"))
		}
		return strings.TrimSpace(rows[i-1]), strings.TrimSpace(rows[i+1])
	}
	t.Fatalf("no transcript row carries the prompt:\n%s", strings.Join(rows, "\n"))
	return "", ""
}

// assertUnpadded fails when the transcript carries a half row, or when the prompt is not framed by
// the blank separators the half rows would otherwise stand in for.
func assertUnpadded(t *testing.T, m Model) {
	t.Helper()
	if m.th.padPrompts {
		t.Fatal("the theme still pads prompt blocks")
	}
	if rows := transcriptRows(t, m); hasPadRow(rows) {
		t.Fatalf("the transcript carries a half row on a colourless profile:\n%s", strings.Join(rows, "\n"))
	}
	if above, below := promptNeighbours(t, m); above != "" || below != "" {
		t.Errorf("the prompt is framed by %q above and %q below; want the blank separators", above, below)
	}
}

// A terminal with no colour cannot draw a half row in the block's gray — the ▄ and ▀ would print as
// bare glyphs — so an Ascii profile paints the prompt block without them and the blank separators
// they stand in for come back.
func TestAsciiProfileDropsPromptPadding(t *testing.T) {
	t.Parallel()
	for _, p := range []colorprofile.Profile{colorprofile.Ascii, colorprofile.NoTTY} {
		t.Run(p.String(), func(t *testing.T) {
			t.Parallel()
			m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
			m = step(t, m, tea.ColorProfileMsg{Profile: p})
			m = withPrompt(t, step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}))
			assertUnpadded(t, m)
		})
	}
}

// Every profile that carries colour keeps the half rows — the profile unknown included, since it
// says nothing about the terminal's colour.
func TestTrueColorProfileKeepsPromptPadding(t *testing.T) {
	t.Parallel()
	for _, p := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.Unknown} {
		t.Run(p.String(), func(t *testing.T) {
			t.Parallel()
			m := withPrompt(t, step(t, newTestModel(t), tea.ColorProfileMsg{Profile: p}))
			if !m.th.padPrompts {
				t.Fatal("the theme stopped padding prompt blocks on a colour profile")
			}
			above, below := promptNeighbours(t, m)
			if !strings.HasPrefix(above, "▄") || !strings.HasPrefix(below, "▀") {
				t.Errorf("the prompt is framed by %q above and %q below; want the ▄ and ▀ half rows", above, below)
			}
		})
	}
}

// The profile answer can land after the first sized frame is on screen. It moves no transcript
// entry, so the repaint rides the theme's own term in the frame key alone: the frame comes back
// unpadded with the transcript's write counter where it stood.
func TestLateAsciiProfileDropsPromptPadding(t *testing.T) {
	t.Parallel()
	m := withPrompt(t, newTestModel(t))
	if above, _ := promptNeighbours(t, m); !strings.HasPrefix(above, "▄") {
		t.Fatalf("before the profile answer the prompt has %q above it; want the ▄ half row", above)
	}
	generation := m.transcript.generation

	m = step(t, m, tea.ColorProfileMsg{Profile: colorprofile.Ascii})

	if m.transcript.generation != generation {
		t.Errorf("the profile answer wrote to the transcript: generation %d, want %d", m.transcript.generation, generation)
	}
	assertUnpadded(t, m)
}

// A colour-scheme switch rebuilds the theme from scratch, and a fresh theme pads. The switch gives a
// colourless terminal no colour to paint the half rows in, so it carries the profile's answer across
// the rebuild the way it carries the width authority.
func TestColorSchemeSwitchKeepsPadChoice(t *testing.T) {
	t.Parallel()
	m := settingsSchemeModel(t, &settingsWriteLog{}, []string{"dark", "light"},
		func(string) (scheme.Scheme, []string) { return stubScheme("#123456"), nil })
	m.transcript.addUser("the prompt", nil)
	m = step(t, m, tea.ColorProfileMsg{Profile: colorprofile.Ascii})

	// Open the sub-list, walk to the second scheme, commit it.
	switched := step(t, step(t, step(t, m, keyEnter()), keyDown()), keyEnter())

	if got := hexOf(switched.th.errorFg); got != "#123456" {
		t.Fatalf("the model's error tone = %s, want the switched scheme's #123456 — the theme was not rebuilt", got)
	}
	if switched.th.padPrompts {
		t.Fatal("the scheme switch turned the prompt padding back on under an Ascii profile")
	}
	view := switched.transcript.renderView(switched.th, 80, false, breadcrumbHint)
	if hasPadRow(view.lines) {
		t.Errorf("the switched theme paints a half row:\n%s", strip(strings.Join(view.lines, "\n")))
	}
}

// The paint cache names the padding: the same prompt block painted padded and then unpadded through
// one live cache yields two different paints, each the one a cold render produces — never the
// memoised paint of the other state.
func TestPaintCacheSeparatesPadStates(t *testing.T) {
	t.Parallel()
	tr := warmed(&transcript{})
	tr.addUser("the prompt", nil)

	padded := newTheme(scheme.Default())
	unpadded := padded
	unpadded.padPrompts = false

	first := tr.renderView(padded, 80, false, breadcrumbHint)
	second := tr.renderView(unpadded, 80, false, breadcrumbHint)

	if strings.Join(first.lines, "\n") == strings.Join(second.lines, "\n") {
		t.Fatalf("both pad states painted the same block:\n%s", strip(strings.Join(first.lines, "\n")))
	}
	if !hasPadRow(first.lines) {
		t.Errorf("the padded paint carries no half row")
	}
	sameRender(t, "padded", first, coldRender(tr, padded, 80, false))
	sameRender(t, "unpadded", second, coldRender(tr, unpadded, 80, false))
	if key := func(th theme) paintKey {
		return blockKey(shapeEntry, []paintInput{{kind: entryUser}}, th, 80, false, false, paintRoot{}, umbrellaFold{})
	}; key(padded) == key(unpadded) {
		t.Error("blockKey names the same key for both pad states")
	}
}
