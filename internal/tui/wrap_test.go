package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/charmbracelet/x/ansi"
)

// railedWidth floors a deeply-nested block's usable width at one column so the wrapper never
// divides by zero, even when the rail gutters consume more than the whole terminal width.
func TestRailedWidthFloors(t *testing.T) {
	if got := railedWidth(80, 0); got != 80 {
		t.Errorf("railedWidth(80, 0) = %d; want 80 (depth 0 takes no gutter)", got)
	}
	if got := railedWidth(3, 5); got != 1 {
		t.Errorf("railedWidth(3, 5) = %d; want 1 (floored, not negative)", got)
	}
}

// ----------------------------------------------------------------------------
// The absolute width cap (layout.md:159-160)
// ----------------------------------------------------------------------------

// wrapText holds the cap itself rather than trusting the wrap it delegates to. x/ansi's
// breakpoint branch (x/ansi@v0.11.7/wrap.go:406-419) has neither of the already-full-line checks
// its default branch has, so a run of the wrapper's own breakpoints kept growing a word onto a
// full line: ansi.Wrap("| --- | --- | --- |", 3, "") returned a five-cell first line, and the
// same input at limit 8 an eleven-cell one. That input is not exotic — a markdown delimiter row
// reaches the transcript verbatim whenever a table is too narrow to lay out, and any hyphenated
// word carries the same run in miniature.
//
// The cap is measured in the width authority's measure, since that is the measure the frame is
// painted in, so the whole table runs under both of them.
func TestWrapTextHoldsTheWidthCap(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in string }{
		{"delimiter row", "| --- | --- | --- |"},
		{"hyphen run", "----"},
		{"pipe row", "| alpha | beta | gamma |"},
		{"hyphenated prose", "a deeply nested sub-agent message that must wrap hard"},
		{"mixed pipe and hyphen runs", "|-|--|---|-- a-b-c |"},
		{"leading indent", "    indented -- text"},
		{"cjk", "日本語のテキスト"},
		{"vs16", "a " + vs16Warning + " b " + strings.Repeat(vs16Warning, 3) + " c"},
		{"cjk and vs16", "警告 " + vs16Warning + " 日本語-テキスト | " + vs16Warning},
		{"empty", ""},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			for limit := 1; limit <= 8; limit++ {
				t.Run(pm.name+"/"+c.name+"/limit "+strconv.Itoa(limit), func(t *testing.T) {
					t.Parallel()
					th := newTheme(scheme.Default())
					th.measure = widthAuthority{method: pm.method}

					lines := wrapText(th, c.in, limit)
					if len(lines) == 0 {
						t.Fatalf("wrapText returned no lines at all")
					}
					for i, ln := range lines {
						w := th.measure.Width(ln)
						if w <= limit {
							continue
						}
						// The one thing a break cannot divide is a single grapheme wider than the
						// limit — a CJK glyph at limit 1 — and it gets a line to itself.
						if cluster, _ := ansi.FirstGraphemeCluster(ln, pm.method); cluster == ln {
							continue
						}
						t.Errorf("line %d %q is %d cells wide, over the %d cap: %q", i, ln, w, limit, lines)
					}
					// Capping moves where the lines break and nothing else: every non-space
					// character the wrap produced is still there, in order. (A break may swallow
					// the space it happens at, which is the wrapper's own behaviour.)
					capped := withoutSpaces(strings.Join(lines, ""))
					uncapped := withoutSpaces(strings.ReplaceAll(ansi.Wrap(c.in, limit, ""), "\n", ""))
					if capped != uncapped {
						t.Errorf("capping changed the content to %q, want the wrap's own %q", capped, uncapped)
					}
				})
			}
		}
	}
}

// withoutSpaces drops every space from s — the part of a wrapped line that must survive a
// re-break, since a break may consume the space it falls on.
func withoutSpaces(s string) string { return strings.ReplaceAll(s, " ", "") }

// ----------------------------------------------------------------------------
// The wrap is taken in the painter's measure (ADR 0030)
// ----------------------------------------------------------------------------

// Enforcing the cap in the authority's measure is only half of it: the BREAK has to be chosen in
// that measure too, or the wrapper is deciding where lines end by a ruler the terminal is not
// using. wrapText used to call the package-level ansi.Wrap, which is hard-wired to
// ansi.GraphemeWidth whatever the painter does — the last site ADR 0030 left unconverted. On the
// painter's DEFAULT WcWidth that reads a VARIATION SELECTOR-16 cluster as two cells where the
// terminal paints one, so the wrap broke a cell early on every line carrying one and the surface
// came out shorter than the width it was given.
//
// Under GraphemeWidth the authority's wrap IS ansi.Wrap, so the grapheme half of this table also
// pins that the conversion changed nothing where the two measures agree; only the WcWidth half
// fails on the pre-fix code.
func TestWrapTextBreaksInThePaintersMeasure(t *testing.T) {
	t.Parallel()

	warns := vs16Warning + " " + vs16Warning + " " + vs16Warning

	cases := []struct {
		name         string
		in           string
		limit        int
		wc, grapheme []string
	}{
		{
			// Five painted cells at limit 5 — one line the painter fills exactly. GraphemeWidth
			// calls the same string eight cells and breaks it.
			name:     "VS16 clusters that fit the painted limit",
			in:       warns,
			limit:    5,
			wc:       []string{warns},
			grapheme: []string{vs16Warning + " " + vs16Warning, vs16Warning},
		},
		{
			name:     "VS16 clusters that fit a narrow painted limit",
			in:       vs16Warning + " " + vs16Warning,
			limit:    3,
			wc:       []string{vs16Warning + " " + vs16Warning},
			grapheme: []string{vs16Warning, vs16Warning},
		},
		{
			// The wrap only moves where the measures disagree: plain prose breaks identically.
			name:     "ascii prose the measures agree about",
			in:       "the quick brown fox jumps",
			limit:    10,
			wc:       []string{"the quick", "brown fox", "jumps"},
			grapheme: []string{"the quick", "brown fox", "jumps"},
		},
		{
			// Wide glyphs are two cells to BOTH measures, so they break the same way as well.
			name:     "cjk the measures agree about",
			in:       "日本語のテキスト",
			limit:    6,
			wc:       []string{"日本語", "のテキ", "スト"},
			grapheme: []string{"日本語", "のテキ", "スト"},
		},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			t.Run(pm.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}

				want := c.wc
				if pm.method == ansi.GraphemeWidth {
					want = c.grapheme
				}
				if got := wrapText(th, c.in, c.limit); !reflect.DeepEqual(got, want) {
					t.Errorf("wrapText(%q, %d) = %q, want %q", c.in, c.limit, got, want)
				}
			})
		}
	}
}

// ----------------------------------------------------------------------------
// The row-capped clip (clipWrap)
// ----------------------------------------------------------------------------

// clipWrap is hangingWrap with a row budget, and the budget is the only difference: text that fits
// inside it comes back as hangingWrap's own lines — same breaks, same hanging indent, same styling —
// and reports no clip. That is what lets a caller reach for it unconditionally instead of measuring
// the text first and branching, which is how the collapsed and expanded paints of one block would
// come to disagree about where a line breaks.
func TestClipWrapLeavesFittingTextAlone(t *testing.T) {
	t.Parallel()

	const width = 40
	marker := branchMarker(true)

	cases := []struct {
		name, text string
		maxRows    int
		rows       int // what the fixture wraps to — a budget it must stay under
	}{
		{name: "one row inside a two-row budget", text: "go build ./...", maxRows: 2, rows: 1},
		{name: "empty text", text: "", maxRows: 2, rows: 1},
		{name: "exactly the budget", text: strings.Repeat("ab ", 20), maxRows: 2, rows: 2},
		{name: "cjk", text: "日本語のテキスト", maxRows: 2, rows: 1},
		{name: "tab bearing", text: "a\tb\tc", maxRows: 1, rows: 1},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			t.Run(pm.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}

				want := hangingWrap(th, th.toolDetail, marker, c.text, width)
				if len(want) != c.rows {
					t.Fatalf("the fixture wraps to %d rows, not the %d the case assumes: %q",
						len(want), c.rows, mapStrip(want))
				}

				got, clipped := clipWrap(th, th.toolDetail, marker, c.text, width, c.maxRows)
				if clipped {
					t.Errorf("reported a clip on %d rows inside a %d-row budget", len(want), c.maxRows)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("clipped output %q differs from hangingWrap's %q", mapStrip(got), mapStrip(want))
				}
			})
		}
	}
}

// Text past the budget comes back as exactly the budget's rows, the last of them ending in the
// continuation tail, and every row inside the width it was given — layout.md's absolute cap, held
// in the measure the frame is painted in (ADR 0030). Fitting the tail is the whole point: appending
// it to a row the wrap had already filled would overrun the width and the viewport would fold the
// row in two, spending the very row the budget was saving.
//
// The rows above the last are hangingWrap's own, untouched, so a clip only ever cuts at its seam.
func TestClipWrapHoldsItsRowBudget(t *testing.T) {
	t.Parallel()

	longRun := "cd . && head -3 go.mod && echo \"---\" && wc -l $(find . -name '*.go' | grep -v dist) 2>/dev/null | tail -1"
	marker := branchMarker(true)
	indent := strings.Repeat(" ", len([]rune(marker))) // the marker is four one-cell runes

	cases := []struct{ name, text string }{
		{"long command", longRun + " && " + longRun},
		{"breakpoint run", strings.Repeat("a-b-c-d ", 40)},
		{"cjk", strings.Repeat("日本語のテキスト", 16)},
		{"tab bearing", strings.Repeat("col\tvalue\t", 20)},
		{"vs16", strings.Repeat(vs16Warning+" warning ", 24)},
	}

	for _, pm := range paintMethods {
		for _, c := range cases {
			for _, width := range []int{20, 40, 80} {
				for maxRows := 1; maxRows <= 2; maxRows++ {
					name := pm.name + "/" + c.name + "/width " + strconv.Itoa(width) + "/rows " + strconv.Itoa(maxRows)
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						th := newTheme(scheme.Default())
						th.measure = widthAuthority{method: pm.method}

						full := hangingWrap(th, th.toolDetail, marker, c.text, width)
						if len(full) <= maxRows {
							t.Fatalf("the fixture wraps to %d rows and never reaches the %d-row budget", len(full), maxRows)
						}

						got, clipped := clipWrap(th, th.toolDetail, marker, c.text, width, maxRows)
						if !clipped {
							t.Errorf("reported no clip while dropping %d of %d rows", len(full)-maxRows, len(full))
						}
						if len(got) != maxRows {
							t.Fatalf("kept %d rows, want the budget's %d: %q", len(got), maxRows, mapStrip(got))
						}
						last := strip(got[maxRows-1])
						if !strings.HasSuffix(last, clipTail) {
							t.Errorf("the last kept row %q does not end in the continuation tail %q", last, clipTail)
						}
						for i, ln := range got {
							if w := th.measure.Width(ln); w > width {
								t.Errorf("row %d %q is %d cells, over the %d cap", i, strip(ln), w, width)
							}
							if strings.Contains(strip(ln), "\t") {
								t.Errorf("row %d still carries a tab for a style to rewrite: %q", i, strip(ln))
							}
							prefix := indent
							if i == 0 {
								prefix = marker
							}
							if !strings.HasPrefix(strip(ln), prefix) {
								t.Errorf("row %d %q does not hang under %q", i, strip(ln), prefix)
							}
						}
						// Only the seam row is re-cut; everything above it is the wrap's own.
						for i := 0; i < maxRows-1; i++ {
							if got[i] != full[i] {
								t.Errorf("row %d %q is not hangingWrap's own %q", i, strip(got[i]), strip(full[i]))
							}
						}
					})
				}
			}
		}
	}
}

// The degenerate widths, where the block is narrower than the mark it has to make. A clipped row
// says "…" or it says nothing at all, so the tail is the one thing the cap yields to — and a budget
// of no rows keeps nothing while still reporting the clip, rather than handing back a row the caller
// has no room to paint.
func TestClipWrapSurvivesNarrowWidths(t *testing.T) {
	t.Parallel()

	for _, pm := range paintMethods {
		t.Run(pm.name, func(t *testing.T) {
			t.Parallel()
			th := newTheme(scheme.Default())
			th.measure = widthAuthority{method: pm.method}
			floor := th.measure.Width(clipTail)

			for width := 0; width <= 6; width++ {
				got, clipped := clipWrap(th, th.toolDetail, branchMarker(true), "a long target that cannot fit", width, 1)
				if !clipped {
					t.Errorf("width %d: reported no clip", width)
				}
				if len(got) != 1 {
					t.Fatalf("width %d: kept %d rows, want 1: %q", width, len(got), mapStrip(got))
				}
				if w := th.measure.Width(got[0]); w > max(width, floor) {
					t.Errorf("width %d: row %q is %d cells, over the %d cap", width, strip(got[0]), w, max(width, floor))
				}
			}

			if got, clipped := clipWrap(th, th.toolDetail, branchMarker(true), "anything", 40, 0); got != nil || !clipped {
				t.Errorf("a zero-row budget returned %q (clipped %v), want no rows and a reported clip", mapStrip(got), clipped)
			}
		})
	}
}

// wrapText is the one wrap in the package, so moving it moves every wrapped surface at once —
// transcript prose and table cells among them. Each is asserted through its own production entry
// point rather than through wrapText again, so a surface that grew a wrap of its own would show up
// here.
//
// The pop-up body is the third surface ISSUES.md named and it is deliberately NOT here: its wrap does
// move with the others, but the pane it lands in is composed by lipgloss — Style.Width pads, and
// past its width WRAPS, in GraphemeWidth whatever the painter is doing — so a VS16 line the
// authority calls short enough is folded back into two pane rows. That fold is the pane's, not the
// wrap's: it is reachable today through pop-up ROWS, which never touch wrapText at all, and it is
// the lipgloss pane's own deliberate behaviour rather than a residue tracked anywhere.
func TestWrappedSurfacesBreakInThePaintersMeasure(t *testing.T) {
	t.Parallel()

	// Five painted cells, eight grapheme cells: one line to the painter's default measure, two to
	// the other one, at a width of 5 on every surface.
	const width = 5
	warns := vs16Warning + " " + vs16Warning + " " + vs16Warning

	surfaces := []struct {
		name  string
		lines func(th theme) []string
	}{
		{
			name:  "transcript prose",
			lines: func(th theme) []string { return renderMarkdownBody(th, warns, width) },
		},
		{
			name:  "table cell",
			lines: func(th theme) []string { return wrapTableCell(th, warns, width) },
		},
	}

	for _, pm := range paintMethods {
		for _, s := range surfaces {
			t.Run(pm.name+"/"+s.name, func(t *testing.T) {
				t.Parallel()
				th := newTheme(scheme.Default())
				th.measure = widthAuthority{method: pm.method}

				want := 1
				if pm.method == ansi.GraphemeWidth {
					want = 2
				}
				lines := s.lines(th)
				if len(lines) != want {
					t.Fatalf("%s wrapped %q at width %d into %d lines, want %d: %q",
						s.name, warns, width, len(lines), want, lines)
				}
				// Whatever the break, no line may exceed the width it was given in the measure
				// the frame is painted in — layout.md's absolute cap.
				for i, ln := range lines {
					if w := th.measure.Width(strip(ln)); w > width {
						t.Errorf("%s line %d %q is %d cells, over the %d cap", s.name, i, strip(ln), w, width)
					}
				}
			})
		}
	}
}

// ----------------------------------------------------------------------------
// The sub-agent rail through the inter-block spacers
// ----------------------------------------------------------------------------

// The one separator row between two blocks is railed at the JOIN of their depths — the shallower
// of the two — and that single rule is the whole of the continuous rail: a spacer inside a run
// carries the gutter, a spacer that crosses a run boundary does not. A climb-out is no exception
// here: the ┊ replaces the spacer only where another grouped sub-agent follows the expanded one
// (railJoin, pinned by TestSubAgentCloserOnlyWhenAnotherGroupedMemberFollows), and none of these
// cases is a group. Each case pins the entire rendered scrollback, so a separator that gained or
// lost a rail shows as the row it is.
func TestRenderSpacerRailsAtTheJoinDepth(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			// The narration between the two calls is what keeps them two BLOCKS: adjacent calls of
			// different labels fold under one umbrella otherwise (toolSuperGroup), and one block has
			// no spacer inside it to rail.
			name: "two different-label blocks inside one run",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 1)
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "now the tests"})
				tr.apply(domain.ToolCallEvent{
					EventBase: domain.EventBase{Depth: 1},
					Call:      domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)},
				})
			},
			want: []string{
				"│ ✦ Read",
				"│   ┕ a.go ⋯ 5 lines",
				"│", // both sides sit at depth 1: the rail runs straight through
				"│ ✦ now the tests",
				"│",
				"│ ✦ Terminal",
				"│   ┕ go test ⋯",
			},
		},
		{
			name: "a climb-out to the top level ends the rail",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child"})
				tr.apply(domain.MessageEvent{Text: "back to parent"})
			},
			want: []string{
				"│ ✦ child",
				"", // the join is the shallower of the two: the rail simply stops here
				"✦ back to parent",
			},
		},
		{
			name: "a 0 to 2 descent opens both rails at once",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{Text: "delegating"})
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "deep"})
			},
			want: []string{
				"✦ delegating",
				"", // join 0: nothing announces the descent, so the spacer is the flat one
				"│ │ ✦ deep",
			},
		},
		{
			name: "a 2 to 1 climb-out keeps the outer rail",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "deep"})
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "shallower"})
			},
			want: []string{
				"│ │ ✦ deep",
				"│", // only the rail both sides reach survives the climb; no member follows, so no ┊
				"│ ✦ shallower",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			got, want := renderPlain(tr, 80), strings.Join(tc.want, "\n")

			if got != want {
				t.Errorf("spacer rail mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// The issue's core case: two sub-agent calls back to back are never visually connected. The first
// run's frame is CLOSED by its ┊ before the second call's own header row opens a frame of its own,
// so the two rails meet nowhere — the closer is what makes the boundary legible now that no label
// stands between them.
//
// Both runs are EXPANDED first, because a collapsed run elides its span whole (layout.md) and a run
// with no rail on screen cannot say anything about how two rails meet. The rule under test is the
// expanded paint's, and this pins it unchanged.
func TestRenderConsecutiveSubAgentRunsAreNotConnected(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"first"}`)}})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "first child"})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s2", Tool: "sub_agent", Arguments: []byte(`{"task":"second"}`)}})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "second child"})
	for _, head := range []int{0, 2} {
		if !tr.setExpanded(head, true) {
			t.Fatalf("setExpanded(%d, true) = false; want the sub-agent head expanded", head)
		}
	}

	want := strings.Join([]string{
		"✦ Sub-Agent (2)", // the two adjacent delegations are rows of one list (subAgentGroup)
		// A member is always a toggle target (its span), so its row always wears the state. Open, it
		// is the ┌─┶ header of its own frame — the ┕ the last row would have closed the list with
		// included, since the frame it opens is what that row now says.
		// Each open head keeps the run's own <tool-top-level-details>, exactly as its shut row
		// would: these two delegates have streamed words and called nothing yet.
		leaderEdgeRow("┌─┶ first ⋯ 0 tool calls", glyphExpanded),
		"│",
		"│ first", // each span opens with the prompt its own delegation was handed
		"│",
		"│ ✦ first child",
		"┊", // the first run closes here…
		leaderEdgeRow("┌─┶ second ⋯ 0 tool calls", glyphExpanded),
		"│", // …and the second opens a frame of its own, touching nothing of the first
		"│ second",
		"│",
		"│ ✦ second child",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("consecutive sub-agent runs mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A railed spacer is a real rail: it carries the subRail role's styling (an unstyled gutter would
// read as stray punctuation rather than as the frame continuing) and it ends on the glyph — the
// gutter's trailing space is trimmed, so the row never leaves a styled blank hanging off its right.
func TestRenderSpacerRailIsStyledAndUntrailed(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := feed(
		domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "one"},
		domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "two"},
	)

	spacer := tr.renderLines(th, 80)[1] // the label → block separator inside the run

	if want := th.subRail.Render(glyphSubRail); spacer != want {
		t.Errorf("railed spacer = %q; want the subRail-styled gutter %q", spacer, want)
	}
	if plain := ansi.Strip(spacer); plain != strings.TrimRight(plain, " ") {
		t.Errorf("railed spacer %q carries trailing whitespace", plain)
	}
}

// The sub-agent frame — rail, corner and closer alike, all the subRail role — is painted in the
// scheme's `tool-header` role, the same gold toolLabel carries, so a nested run reads as one
// coloured frame rather than as dim chrome. The assertion compares against the palette's own render
// rather than a lipgloss byte-golden; the guard below it catches the opposite failure, a subRail
// role that paints nothing at all and would leave the rail unstyled.
func TestSubRailPaintedInToolHeaderGold(t *testing.T) {
	th := newTheme(scheme.Default())

	rail := th.subRail.Render(glyphSubRail)

	if want := lipgloss.NewStyle().Foreground(lipgloss.Color(scheme.Default().ToolHeader)).Render(glyphSubRail); rail != want {
		t.Errorf("rail = %q; want the `tool-header` role's gold %q the tool header carries", rail, want)
	}
	if rail == glyphSubRail {
		t.Fatal("the subRail role renders no escape sequence; the rail and label would be unstyled")
	}
}

// Design call 2 splits the delegation frame's header row between two voices, and the split is the
// whole point of it: ┌ and ┊ are the RAIL — its top end and its close — and wear the gold the │
// between them does, while the arm and the tee reaching across to the branch are that row's own
// chrome and stay in the tone every other ┝/┕ takes. A frame painted in one voice throughout would
// read as a box drawn around the run rather than as a rail hanging off it.
func TestSubAgentFrameSplitsRailGoldFromBranchTone(t *testing.T) {
	th := newTheme(scheme.Default())

	marker := paintRowMarker(th, subAgentOpenMarker, true)

	gold, branch := th.subRail.Render(glyphRailCorner), detailTone(th, true).Render("─"+glyphRailTee+" ")
	if want := gold + branch; marker != want {
		t.Errorf("┌─┶ marker = %q; want the corner in rail gold and the arm in the branch tone %q",
			marker, want)
	}
	if got, want := ansi.Strip(marker), subAgentOpenMarker; got != want {
		t.Errorf("painting the marker changed its cells to %q; want %q", got, want)
	}
	if plain := branchMarker(false); paintRowMarker(th, plain, true) != detailTone(th, true).Render(plain) {
		t.Error("an ordinary ┝ marker no longer paints in one tone; only the frame's corner is split off")
	}
}

// The done ✓ is the `success` role and nothing else — errorText's green counterpart, so the two
// verdicts a row can carry are read as one pair — and it lands on the row it belongs to, after the
// delegation's name and ahead of the leaders.
func TestSubAgentDoneMarkPaintedInTheSuccessRole(t *testing.T) {
	th := newTheme(scheme.Default())

	row := leaderRow(th, toolView{Target: "survey", finished: true}, branchMarker(true), 60, false, noRemainder)

	styled := th.successMark.Render(glyphDone)
	if styled == glyphDone {
		t.Fatal("the successMark role renders no escape sequence; the ✓ would be unstyled")
	}
	if want := lipgloss.NewStyle().Foreground(lipgloss.Color(scheme.Default().Success)).Render(glyphDone); styled != want {
		t.Errorf("✓ style = %q; want the scheme's `success` green %q", styled, want)
	}
	if !strings.Contains(row, styled) {
		t.Errorf("row %q does not carry the ✓ in its own role", row)
	}
	if got, want := ansi.Strip(row), "  ┕ survey ✓ "; !strings.HasPrefix(got, want) {
		t.Errorf("row text = %q; want it to open %q (the mark follows the name, ahead of the leaders)", got, want)
	}
	plain := leaderRow(th, toolView{Target: "survey"}, branchMarker(true), 60, false, noRemainder)
	if strings.Contains(ansi.Strip(plain), glyphDone) {
		t.Errorf("an unfinished delegation's row carries a ✓: %q", ansi.Strip(plain))
	}
}
