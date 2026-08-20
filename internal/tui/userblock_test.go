package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/scheme"
)

// The user block pads its own wrapped rows, in the painter's measure, the way promptMarkerRow two
// functions down already pads its. It used to hand them to a lipgloss Width style, which pads —
// and past its width WRAPS — in GraphemeWidth whatever the painter is doing (ADR 0030). Once
// wrapText takes its break in the painter's measure, a prompt line the authority calls exactly the
// block width can measure wider than that to lipgloss, and the style folds it in two: not an
// overflow, but a "\n" smuggled into ONE element of the []string the whole line-oriented renderer
// counts rows with, so the viewport height, the sticky offsets and the userBlocks ranges each count
// one row where the terminal paints two.
func TestUserBlockRowsAreOneSquareLineEach(t *testing.T) {
	t.Parallel()

	const width = 12
	text := strings.Repeat(vs16Warning+" ", 8)

	for _, pm := range paintMethods {
		t.Run(pm.name, func(t *testing.T) {
			t.Parallel()
			th := newTheme(scheme.Default())
			th.measure = widthAuthority{method: pm.method}

			paint := renderUserBlock(th, glyphUser+" ", paintInput{text: text, entryState: entryState{expanded: true}}, width)
			if len(paint.lines) == 0 {
				t.Fatalf("the user block rendered nothing at all")
			}
			for i, ln := range paint.lines {
				if strings.Contains(ln, "\n") {
					t.Errorf("row %d holds %d physical lines in one entry: %q",
						i, strings.Count(ln, "\n")+1, strip(ln))
				}
				if w := th.measure.Width(strip(ln)); w != width {
					t.Errorf("row %d %q is %d cells, want the block's %d", i, strip(ln), w, width)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The collapsed prompt: a huge send paints three rows and a marker (layout.md, "Collapsed and
// expanded blocks")
// ----------------------------------------------------------------------------

// promptRows renders tr at width and returns its lines with the styling stripped and the trailing
// pad KEPT — deliberately not renderPlain, which trims it: a prompt block is painted to the full
// width and its collapse marker is flush against the right edge, so where a row ENDS is half of
// what these tests assert.
func promptRows(t *testing.T, tr *transcript, width int) []string {
	t.Helper()
	lines := tr.renderLines(newTheme(scheme.Default()), width)
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = strip(ln)
	}
	return out
}

// splitMarker asserts the geometry of a prompt row carrying a collapse marker — the row is exactly
// the block's width, it ends with want over a promptMarkerMargin of clear field, and at least
// promptMarkerGap columns separate want from the row's own content — and returns that content,
// trailing pad trimmed, for the caller to assert on.
func splitMarker(t *testing.T, row, want string, width int) string {
	t.Helper()
	th := newTheme(scheme.Default())
	if got := th.measure.Width(row); got != width {
		t.Errorf("row %q is %d columns wide; want the block's full %d", row, got, width)
	}
	margin := strings.Repeat(" ", promptMarkerMargin)
	if !strings.HasSuffix(row, want+margin) {
		t.Fatalf("row %q does not end with the marker %q over its %d-column margin", row, want, promptMarkerMargin)
	}
	content := strings.TrimRight(strings.TrimSuffix(row, want+margin), " ")
	gap := width - promptMarkerMargin - th.measure.Width(content) - th.measure.Width(want)
	if gap < promptMarkerGap {
		t.Errorf("marker %q sits %d columns past the content %q; want at least promptMarkerGap (%d)",
			want, gap, content, promptMarkerGap)
	}
	return content
}

// TestCollapsedPromptPaintsThreeRowsWithAnInlineMarker pins the collapsed shape in one table: a
// prompt whose body wraps past promptCollapsedRows rows paints exactly that many, the last of them
// truncated to leave the right-aligned see-more marker its gap, and the marker counts what is left
// behind — pluralised. A body inside the cap paints whole with no marker at all, which is the
// boundary the trigger turns on, and an interjection collapses by the very same rule.
func TestCollapsedPromptPaintsThreeRowsWithAnInlineMarker(t *testing.T) {
	const width = 40
	// One unbreakable word, wrapped hard: it fills the block's rows edge to edge, which is what
	// makes the third row long enough to be truncated by the marker beside it.
	long := strings.Repeat("x", 200)
	cases := []struct {
		name   string
		build  func(tr *transcript)
		want   []string // every row of the block, trailing pad trimmed, the marker excluded
		marker string   // the marker the last row carries; "" when the block hides nothing
	}{
		{
			name:   "a four-row prompt keeps three rows and counts the fourth",
			build:  func(tr *transcript) { tr.addUser("alpha\nbravo\ncharlie\ndelta", nil) },
			want:   []string{"❯ alpha", "  bravo", "  charlie"},
			marker: "see more (+1 line)…",
		},
		{
			name:   "a long prompt counts every row it hides",
			build:  func(tr *transcript) { tr.addUser("a\nb\nc\nd\ne\nf\ng\nh\ni\nj", nil) },
			want:   []string{"❯ a", "  b", "  c"},
			marker: "see more (+7 lines)…",
		},
		{
			name:  "exactly three rows is not over the threshold",
			build: func(tr *transcript) { tr.addUser("alpha\nbravo\ncharlie", nil) },
			want:  []string{"❯ alpha", "  bravo", "  charlie"},
		},
		{
			name:  "a short prompt paints as it always has",
			build: func(tr *transcript) { tr.addUser("alpha", nil) },
			want:  []string{"❯ alpha"},
		},
		{
			// width 40 less the 20-column marker, its 2-column gap and the 1-column right margin
			// leaves 17 for the row, ellipsis included — the whole of what "truncated to leave a
			// gap" means, with the margin paid for out of the content and never out of the marker.
			name:   "the third row is truncated to make room for the marker",
			build:  func(tr *transcript) { tr.addUser("alpha\nbravo\n"+long, nil) },
			want:   []string{"❯ alpha", "  bravo", "  " + strings.Repeat("x", 14) + "…"},
			marker: "see more (+5 lines)…",
		},
		{
			name:   "an interjection collapses by the same rule",
			build:  func(tr *transcript) { tr.addInterjected("alpha\nbravo\ncharlie\ndelta", nil) },
			want:   []string{"⧖ alpha", "  bravo", "  charlie"},
			marker: "see more (+1 line)…",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			rows := promptRows(t, tr, width)
			if len(rows) != len(tc.want) {
				t.Fatalf("block painted %d rows, want %d:\n%s", len(rows), len(tc.want), strings.Join(rows, "\n"))
			}
			for i, row := range rows {
				got := strings.TrimRight(row, " ")
				if tc.marker != "" && i == len(rows)-1 {
					got = splitMarker(t, row, tc.marker, width)
				}
				if got != tc.want[i] {
					t.Errorf("row %d = %q; want %q", i, got, tc.want[i])
				}
			}
			if tc.marker == "" && strings.Contains(strings.Join(rows, "\n"), "see more") {
				t.Errorf("a block that hides nothing grew a marker:\n%s", strings.Join(rows, "\n"))
			}
		})
	}
}

// TestExpandedPromptPaintsItsWholeBodyAndTrailsSeeLess is what the expanded state is FOR on a
// prompt: every wrapped row paints, no content row is truncated, and the see-less marker takes a
// trailing row of its own — the row a full body leaves no room for it to ride. Collapsing again
// paints exactly the compact shape back, over one transcript, because that is the claim: nothing
// about the entry changes but the flag the painter reads.
func TestExpandedPromptPaintsItsWholeBodyAndTrailsSeeLess(t *testing.T) {
	const width = 40
	tr := &transcript{}
	tr.addUser("alpha\nbravo\ncharlie\ndelta\necho", nil)

	collapsed := promptRows(t, tr, width)
	if len(collapsed) != promptCollapsedRows {
		t.Fatalf("collapsed is not the default: %d rows\n%s", len(collapsed), strings.Join(collapsed, "\n"))
	}

	if !tr.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
	}
	rows := promptRows(t, tr, width)
	want := []string{"❯ alpha", "  bravo", "  charlie", "  delta", "  echo"}
	if len(rows) != len(want)+1 {
		t.Fatalf("expanded block painted %d rows, want the %d body rows plus one see-less row:\n%s",
			len(rows), len(want), strings.Join(rows, "\n"))
	}
	for i, w := range want {
		if got := strings.TrimRight(rows[i], " "); got != w {
			t.Errorf("row %d = %q; want %q", i, got, w)
		}
	}
	if content := splitMarker(t, rows[len(rows)-1], promptSeeLess, width); content != "" {
		t.Errorf("the see-less row carries %q; want the marker alone on a row of its own", content)
	}

	if !tr.setExpanded(0, false) {
		t.Fatal("setExpanded(0, false) = false; want the prompt collapsed again")
	}
	if got := promptRows(t, tr, width); !reflect.DeepEqual(got, collapsed) {
		t.Errorf("collapsing again did not repaint the compact shape:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(collapsed, "\n"))
	}
}

// TestUnderThresholdPromptIgnoresItsExpandedState pins the harmless half of the state gate (item 2):
// every prompt OWNS an expanded state, and one whose body fits inside the row cap paints identically
// either way — holding the flag is not the same as showing it.
func TestUnderThresholdPromptIgnoresItsExpandedState(t *testing.T) {
	tr := &transcript{}
	tr.addUser("alpha\nbravo", nil)

	collapsed := renderPlain(tr, 40)
	if !tr.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the prompt entry to own a block state")
	}
	if got := renderPlain(tr, 40); got != collapsed {
		t.Errorf("an under-threshold prompt repainted when expanded:\n--- got ---\n%s\n--- want (unchanged) ---\n%s",
			got, collapsed)
	}
}

// TestPromptCollapseFollowsThePaintWidth is the trigger's other half: whether a prompt collapses is
// measured at paint time against the width being painted, so one entry — untouched between the two
// renders — paints whole in a wide window and collapses in a narrow one. The hidden count is read
// off the expanded paint at that same narrow width, so the marker's arithmetic is asserted against
// the rows it is counting rather than against a number written down here.
func TestPromptCollapseFollowsThePaintWidth(t *testing.T) {
	const narrow = 24
	tr := &transcript{}
	tr.addUser("the quick brown fox jumps over the lazy dog and keeps on running", nil)

	if wide := promptRows(t, tr, 100); len(wide) != 1 || strings.Contains(wide[0], "see more") {
		t.Fatalf("the prompt did not paint whole at width 100:\n%s", strings.Join(wide, "\n"))
	}

	if !tr.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
	}
	body := len(promptRows(t, tr, narrow)) - 1 // less the trailing see-less row
	if !tr.setExpanded(0, false) {
		t.Fatal("setExpanded(0, false) = false; want the prompt collapsed again")
	}

	rows := promptRows(t, tr, narrow)
	if len(rows) != promptCollapsedRows {
		t.Fatalf("the same prompt painted %d rows at width %d; want %d:\n%s",
			len(rows), narrow, promptCollapsedRows, strings.Join(rows, "\n"))
	}
	splitMarker(t, rows[len(rows)-1], promptSeeMore(body-promptCollapsedRows), narrow)
}

// TestPromptWithSkillsPaintsNoChipRow is the retired chip row's epitaph: a send that invoked a
// skill is exactly its body rows in both states — promptCollapsedRows collapsed, the body plus the
// see-less row expanded — with no trailing ✦ row of any kind. What records the invocation now is
// the token inside the text (TestSentBlockAccentsItsSkillTokens).
func TestPromptWithSkillsPaintsNoChipRow(t *testing.T) {
	const width = 44
	const text = "/review alpha\nbravo\ncharlie\ndelta"
	tr := &transcript{}
	tr.addUser(text, []skillSpan{spanOf(t, text, "/review", 1)})

	rows := promptRows(t, tr, width)
	if len(rows) != promptCollapsedRows {
		t.Fatalf("collapsed block painted %d rows; want exactly its %d body rows:\n%s",
			len(rows), promptCollapsedRows, strings.Join(rows, "\n"))
	}
	splitMarker(t, rows[promptCollapsedRows-1], promptSeeMore(1), width)

	if !tr.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
	}
	rows = promptRows(t, tr, width)
	if len(rows) != 5 { // four body rows, then the see-less row that closes the block
		t.Fatalf("expanded block painted %d rows; want 5:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	splitMarker(t, rows[4], promptSeeLess, width)
	for i, row := range rows {
		if strings.Contains(row, glyphSkill) {
			t.Errorf("row %d still badges the skill: %q", i, row)
		}
	}
}

// spanOf states where want sits in text as the [skillSpan] a send records for it. nth counts from
// one, so a test can name the SECOND invocation of a twice-named skill.
func spanOf(t *testing.T, text, want string, nth int) skillSpan {
	t.Helper()
	at, from := -1, 0
	for n := 0; n < nth; n++ {
		i := strings.Index(text[from:], want)
		if i < 0 {
			t.Fatalf("%q holds fewer than %d occurrences of %q", text, nth, want)
		}
		at = from + i
		from = at + len(want)
	}
	return skillSpan{start: at, end: at + len(want)}
}

// accentRuns returns the glyph runs the skill accent covers in a painted block, in paint order: the
// text between the SGR that opens each accented span and the escape that closes it. shadeCells
// strips what it re-renders, so a run carries no escapes of its own and the runs are exactly the
// cells that lit up — a test can therefore assert the accent's EXTENT and not merely its presence.
//
// EMPTY runs are dropped: shading a row twice makes the cut re-emit the style it found active with
// nothing between it and the next SGR, which paints no cell and is not a second accent.
func accentRuns(block, opener string) []string {
	var out []string
	for rest := block; ; {
		i := strings.Index(rest, opener)
		if i < 0 {
			return out
		}
		rest = rest[i+len(opener):]
		run := rest
		if j := strings.IndexByte(rest, '\x1b'); j >= 0 {
			run, rest = rest[:j], rest[j:]
		} else {
			rest = ""
		}
		if run != "" {
			out = append(out, run)
		}
		if rest == "" {
			return out
		}
	}
}

// TestSentBlockAccentsItsSkillTokens is the chip row's replacement rule: the "/token" the human
// typed is painted in the skill violet where it stands, and nothing else on the row is — the block
// still reads as the sentence that was sent (ISSUES, "sent prompts with skills"; layout.md,
// "Tokens light up when they resolve").
func TestSentBlockAccentsItsSkillTokens(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 44
	const text = "/review this diff"
	tr := &transcript{}
	tr.addUser(text, []skillSpan{spanOf(t, text, "/review", 1)})

	rows := tr.renderLines(th, width)
	if len(rows) != 1 {
		t.Fatalf("the block painted %d rows; want the one its text wraps to:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if got := accentRuns(rows[0], accentOpener(t, th.skillAccent)); !reflect.DeepEqual(got, []string{"/review"}) {
		t.Errorf("the accent covers %q; want the token alone", got)
	}
	if got := strip(rows[0]); !strings.HasPrefix(got, glyphUser+" "+text) {
		t.Errorf("the block's own text changed under the accent: %q", got)
	}
}

// A token invoked twice is painted twice: the SPANS drive the accent, not the de-duped name list,
// so both occurrences light up.
func TestSentBlockAccentsEveryOccurrence(t *testing.T) {
	th := newTheme(scheme.Default())
	const text = "/review this diff and /review that one"
	tr := &transcript{}
	tr.addUser(text, []skillSpan{
		spanOf(t, text, "/review", 1),
		spanOf(t, text, "/review", 2),
	})

	block := strings.Join(tr.renderLines(th, 44), "\n")
	if got := accentRuns(block, accentOpener(t, th.skillAccent)); !reflect.DeepEqual(got, []string{"/review", "/review"}) {
		t.Errorf("the accent covers %q; want both invocations", got)
	}
}

// A token the block had to break across a soft-wrap is accented on BOTH rows — the prompt box's
// own rule for a wrapped token (TestAccentedTokenWrapsAcrossRows), against the transcript's wrap.
func TestAccentedSkillTokenStraddlesASoftWrap(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 12 // the token is wider than the row left of the marker, so the block breaks it
	const text = "/coding-standards"
	tr := &transcript{}
	tr.addUser(text, []skillSpan{spanOf(t, text, text, 1)})

	block := strings.Join(tr.renderLines(th, width), "\n")
	opener := accentOpener(t, th.skillAccent)
	if lit := rowsWithAccent(block, opener); len(lit) != 2 {
		t.Fatalf("a wrapped token lit %d rows, want both halves: %v", len(lit), lit)
	}
	runs := accentRuns(block, opener)
	if got := strings.Join(runs, ""); got != text {
		t.Errorf("the two accented halves are %q, joining to %q; want the whole token", runs, got)
	}
}

// The collapse rules the accent: a token on a row the collapse hid paints nothing, and a token on
// the truncated row carrying the see-more marker stays inside that row's own content — the marker
// is apogee talking, and an accent that reached it would recolour that voice.
func TestCollapsedBlockAccentsOnlyWhatItShows(t *testing.T) {
	th := newTheme(scheme.Default())
	const width = 44

	t.Run("a token on a hidden row paints nothing", func(t *testing.T) {
		const text = "alpha\nbravo\ncharlie\ndelta /review"
		tr := &transcript{}
		tr.addUser(text, []skillSpan{spanOf(t, text, "/review", 1)})

		block := strings.Join(tr.renderLines(th, width), "\n")
		if runs := accentRuns(block, accentOpener(t, th.skillAccent)); len(runs) != 0 {
			t.Errorf("a hidden row's token painted %q", runs)
		}
	})

	t.Run("a token on the marker row paints, and the marker keeps its own colour", func(t *testing.T) {
		const text = "alpha\nbravo\ncharlie /review\ndelta"
		tr := &transcript{}
		tr.addUser(text, []skillSpan{spanOf(t, text, "/review", 1)})

		rows := tr.renderLines(th, width)
		if len(rows) != promptCollapsedRows {
			t.Fatalf("the collapsed block painted %d rows; want %d", len(rows), promptCollapsedRows)
		}
		marker := rows[promptCollapsedRows-1]
		if got := accentRuns(marker, accentOpener(t, th.skillAccent)); !reflect.DeepEqual(got, []string{"/review"}) {
			t.Errorf("the marker row's accent covers %q; want the token alone", got)
		}
		if !strings.Contains(marker, th.promptToggle.Render(promptSeeMore(1))) {
			t.Errorf("the see-more marker lost its own styling to the accent:\n%q", marker)
		}
	})
}

// TestPromptMarkerCarriesTheHighlightStyle pins the marker's look: it is painted in the theme's own
// promptToggle role and not in the prompt body's, which is what sets the toggle off from what the
// human wrote. A loose contains against the theme's own render (the toolLabel precedent), with the
// two guards for the opposite failures — a role that paints nothing at all, and one that paints
// exactly what the body does.
func TestPromptMarkerCarriesTheHighlightStyle(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := &transcript{}
	tr.addUser("alpha\nbravo\ncharlie\ndelta", nil)

	row := tr.renderLines(th, 40)[promptCollapsedRows-1]
	marker := promptSeeMore(1)
	styled := th.promptToggle.Render(marker)
	if styled == marker {
		t.Fatal("the promptToggle role renders no escape sequence; the marker would be unstyled")
	}
	if styled == th.userBlock.Render(marker) {
		t.Error("the promptToggle role paints exactly what the prompt body does; the marker is not set off")
	}
	if !strings.Contains(row, styled) {
		t.Errorf("row %q does not carry the styled marker %q", row, styled)
	}
}
