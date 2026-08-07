package tui

import (
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Sub-agent framing reflow safety (P3.14)
// ----------------------------------------------------------------------------

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

// A Depth > 0 block renders at a tiny and a zero width without panicking (the acceptance's
// "reflow at small sizes doesn't panic"), the framed text is still produced, and — the part that
// makes this more than a smoke test — every line it draws obeys layout.md's absolute cap. The cap
// has a floor: a Depth-2 block cannot be drawn in fewer columns than its two rail gutters, its
// marker and one column of text, so below that the bound is the floor rather than the width. The
// fixture's hyphen ("sub-agent") is the point — it is exactly the breakpoint run the wrapper used
// to grow past the limit.
func TestSubAgentReflowAtSmallWidths(t *testing.T) {
	th := newTheme()
	const depth = 2
	floor := depth*railWidth + th.measure.Width(glyphAssistant+" ") + 1

	for _, width := range []int{0, 1, 2, 3, 6, 12, 40} {
		tr := feed(domain.MessageEvent{
			EventBase: domain.EventBase{Depth: depth},
			Text:      "a deeply nested sub-agent message that must wrap hard",
		})
		lines := tr.renderLines(th, width) // must not panic at any width
		if len(lines) == 0 {
			t.Errorf("width %d produced no lines", width)
		}
		bound := max(width, floor)
		for i, ln := range lines {
			if w := th.measure.Width(ln); w > bound {
				t.Errorf("width %d: line %d %q is %d cells wide, over the %d cap", width, i, strip(ln), w, bound)
			}
		}
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
					th := newTheme()
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
				th := newTheme()
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
				th := newTheme()
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
						th := newTheme()
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
			th := newTheme()
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
// The pop-up body is the third surface TODO.md named and it is deliberately NOT here: its wrap does
// move with the others, but the pane it lands in is composed by lipgloss — Style.Width pads, and
// past its width WRAPS, in GraphemeWidth whatever the painter is doing — so a VS16 line the
// authority calls short enough is folded back into two pane rows. That fold is the pane's, not the
// wrap's: it is reachable today through pop-up ROWS, which never touch wrapText at all, and it is
// tracked in TODO.md with the rest of the ADR 0030 residue.
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
				th := newTheme()
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
			th := newTheme()
			th.measure = widthAuthority{method: pm.method}

			paint := renderUserBlock(th, glyphUser+" ", text, nil, width, true)
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
// The sub-agent rail through the inter-block spacers
// ----------------------------------------------------------------------------

// The one separator row between two blocks is railed at the JOIN of their depths — the shallower
// of the two — and that single rule is the whole of the continuous rail: a spacer inside a run
// carries the gutter, a spacer that crosses a run boundary does not. Each case pins the entire
// rendered scrollback, so a separator that gained or lost a rail shows as the row it is.
func TestRenderSpacerRailsAtTheJoinDepth(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			name: "two different-label blocks inside one run",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 1)
				tr.apply(domain.ToolCallEvent{
					EventBase: domain.EventBase{Depth: 1},
					Call:      domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)},
				})
			},
			want: []string{
				"│ ⤷ sub-agent",
				"│",
				"│ ✦ Read File",
				"│   ┕ a.go 1 - 5",
				"│", // both sides sit at depth 1: the rail runs straight through
				"│ ✦ Run",
				"│   ┕ go test",
			},
		},
		{
			name: "a climb-out to the top level ends the rail",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child"})
				tr.apply(domain.MessageEvent{Text: "back to parent"})
			},
			want: []string{
				"│ ⤷ sub-agent",
				"│",
				"│ ✦ child",
				"", // join 0: the rail ends on the run's last line, not on the spacer
				"✦ back to parent",
			},
		},
		{
			name: "a 0 to 2 descent joins the stacked labels at depth 1",
			build: func(tr *transcript) {
				tr.apply(domain.MessageEvent{Text: "delegating"})
				tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "deep"})
			},
			want: []string{
				"✦ delegating",
				"",
				"│ ⤷ sub-agent",
				"│", // the outer rail alone: the inner run has not opened yet
				"│ │ ⤷ sub-agent",
				"│ │",
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
				"│ ⤷ sub-agent",
				"│",
				"│ │ ⤷ sub-agent",
				"│ │",
				"│ │ ✦ deep",
				"│", // the inner run closed; the outer one is still open
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

// The issue's core case: two sub-agent calls back to back are never visually connected. The
// second call's own tool-call block sits at the parent's depth between the two runs, so the join
// dips to 0, the rail breaks, and a fresh ⤷ sub-agent label opens the second run.
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
		"✦ Sub-Agent ▼", // a run's head is always a toggle target (its span), so it always wears the state
		"  ┕ first",
		"",
		"│ ⤷ sub-agent",
		"│",
		"│ ✦ first child",
		"", // the first run closes here…
		"✦ Sub-Agent ▼",
		"  ┕ second",
		"", // …and the second call is fenced off from it on both sides
		"│ ⤷ sub-agent",
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
	th := newTheme()
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

// The sub-agent frame — rail and ⤷ label alike, both the subRail role — is painted in colCode,
// the same tool-header orange toolLabel carries, so a nested run reads as one coloured frame
// rather than as dim chrome. The assertion compares against the palette's own render rather than
// a lipgloss byte-golden; the guard below it catches the opposite failure, a subRail role that
// paints nothing at all and would leave the rail unstyled.
func TestSubRailPaintedInToolHeaderOrange(t *testing.T) {
	th := newTheme()

	rail := th.subRail.Render(glyphSubRail)

	if want := lipgloss.NewStyle().Foreground(colCode).Render(glyphSubRail); rail != want {
		t.Errorf("rail = %q; want the colCode orange %q the tool header carries", rail, want)
	}
	if rail == glyphSubRail {
		t.Fatal("the subRail role renders no escape sequence; the rail and label would be unstyled")
	}
}

// ----------------------------------------------------------------------------
// The tool header's label styling
// ----------------------------------------------------------------------------

// A tool header shows the label alone — no brackets and, now that the block shape is uniform, no
// target either — and that label carries the bold-orange style, baked in before the wrap so the
// visible text is unaffected. The style assertion is a loose contains against the theme's own
// render rather than a byte-exact golden, so a lipgloss change cannot false-fail it; the guard
// below it catches the opposite failure, a toolLabel role that paints nothing at all.
func TestToolHeaderLabelStyled(t *testing.T) {
	th := newTheme()
	block := renderToolBlock(th, []toolView{{Label: "Read File", Target: "main.go"}}, 80, blockState{}).lines
	head := block[0]

	if got, want := ansi.Strip(head), "✦ Read File"; got != want {
		t.Errorf("header text = %q; want %q (no brackets, and never a target)", got, want)
	}
	if got, want := ansi.Strip(block[1]), "  ┕ main.go"; got != want {
		t.Errorf("branch text = %q; want %q (the target leads the branch)", got, want)
	}
	styled := th.toolLabel.Render("Read File")
	if styled == "Read File" {
		t.Fatal("the toolLabel role renders no escape sequence; the header would be unstyled")
	}
	if !strings.Contains(head, styled) {
		t.Errorf("header %q does not carry the styled label %q", head, styled)
	}
}

// ----------------------------------------------------------------------------
// Grouped same-label tool calls (tool-call layout item 4)
// ----------------------------------------------------------------------------

// readCall folds a read_file call and its result into tr, so a grouping test reads as the
// batch of reads it is meant to render. The result carries BOTH halves the real tool reports:
// the "showing lines from-to" prose the model reads, and the domain.ReadSpan the view renders
// its branch line from.
func readCall(tr *transcript, id, path string, from, to, depth int) {
	base := domain.EventBase{Depth: depth}
	tr.apply(domain.ToolCallEvent{
		EventBase: base,
		Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
	})
	tr.apply(domain.ToolResultEvent{
		EventBase: base,
		Result: domain.ToolResult{
			CallID: id,
			Content: "[File: " + path + ", " + strconv.Itoa(to) + " lines total, showing lines " +
				strconv.Itoa(from) + "-" + strconv.Itoa(to) + "]\n…",
			Summary: domain.ReadSpan{Start: from, End: to, Total: to},
		},
	})
}

// A batch of reads folds into one block: a single ✦ Read File header carrying the member count,
// ┝ ┝ ┕ rails, and every target padded to the widest one so the detail column lines up — the shape
// docs/layout/tool-layout.md sketches.
func TestRenderGroupsConsecutiveSameLabelCalls(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	readCall(tr, "c2", "TODO.md", 1, 408, 0)
	readCall(tr, "c3", "ISSUES.md", 1, 8, 0)

	want := strings.Join([]string{
		"✦ Read File (3)",
		"  ┝ README.md 1 - 154",
		"  ┝ TODO.md   1 - 408",
		"  ┕ ISSUES.md 1 - 8",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grouped block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A grouped run inside a sub-agent is framed like any other block: the ⤷ label opens the run once
// and every line of the group — header, branches, and the separator between the label and the
// block alike — carries the │ rail gutter, so the run reads as one continuous frame.
func TestRenderGroupsInsideSubAgent(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "a.go", 1, 5, 1)
	readCall(tr, "c2", "bb.go", 1, 9, 1)

	want := strings.Join([]string{
		"│ ⤷ sub-agent",
		"│",
		"│ ✦ Read File (2)",
		"│   ┝ a.go  1 - 5",
		"│   ┕ bb.go 1 - 9",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("railed group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Two different tools that share a friendly label group together — the reader groups by what the
// header says, not by tool id.
func TestRenderGroupsDifferentToolsSharingALabel(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "single_find_and_replace", Arguments: []byte(`{"path":"a.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "replaced text in a.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "multi_find_and_replace", Arguments: []byte(`{"path":"bb.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "applied 2 replacements to bb.go"}})

	want := strings.Join([]string{
		"✦ Edit File (2)",
		"  ┝ a.go  replaced text in a.go",
		"  ┕ bb.go applied 2 replacements to bb.go",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("shared-label group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A member whose result has not landed shows its bare padded target and nothing after it; when the
// result folds in, the whole block repaints with that member's detail in the aligned column.
func TestRenderGroupWithInFlightMember(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file", Arguments: []byte(`{"path":"TODO.md"}`)}})

	want := strings.Join([]string{
		"✦ Read File (2)",
		"  ┝ README.md 1 - 154",
		"  ┕ TODO.md",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2",
		Content: "[File: TODO.md, 408 lines total, showing lines 1-408]\n…",
		Summary: domain.ReadSpan{Start: 1, End: 408, Total: 408}}})
	want = strings.Join([]string{
		"✦ Read File (2)",
		"  ┝ README.md 1 - 154",
		"  ┕ TODO.md   1 - 408",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("re-rendered group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A lone call renders in the shape a group does — a label header, target leading the branch — and
// counts nothing: the "(N)" is a group's arithmetic and a block of one has none to state. The
// ┕-with-no-padding is what "a group of one pads to itself" means, and a second call joins by
// adding a line rather than by moving the first one's target.
func TestRenderSingleCallSharesTheGroupShape(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "main.go", 1, 154, 0)

	want := strings.Join([]string{
		"✦ Read File",
		"  ┕ main.go 1 - 154",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("single-call block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// …and a second call joins it by adding a line, not by moving the first one's target.
	readCall(tr, "c2", "a-much-longer-name.go", 1, 9, 0)
	want = strings.Join([]string{
		"✦ Read File (2)",
		"  ┝ main.go               1 - 154",
		"  ┕ a-much-longer-name.go 1 - 9",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grown block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A body-only call keeps the same header and branch: nothing rides beside the target, and the
// body lays out beneath it at the branch marker's width — those lines are not ┝/┕ branches of
// their own, because only calls are (layout.md's Run sketch). COLLAPSED, none of them lays out at
// all: the collapsed block's rows go to its target and the marker counts the body whole
// (collapsedBodyRows), which is the shape the sketch draws.
func TestRenderMultiDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})

	want := strings.Join([]string{
		"✦ Run ▶", // a hidden body is something to reveal, and the header says so
		"  ┕ go test ./...",
		"    +3 more lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("multi-detail block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A diff call is the summary-and-body shape layout.md sketches: the diffstat rides the branch
// beside the path and the coloured body hangs beneath it. The body keeps its red/green
// colouring, which — together with having a body at all — is why it can never fold into a group.
// Asserted expanded, because a collapsed diff paints no body line at all (collapsedBodyRows) and
// there would be no colour to see.
func TestRenderDiffDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
		Content: "- a removed line\n+ an added line",
		Summary: domain.DiffStat{Added: 1, Removed: 1}}})
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the diff block expanded")
	}

	want := strings.Join([]string{
		"✦ View Diff ▼",
		"  ┕ main.go +1 -1",
		"    - a removed line",
		"    + an added line",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	th := newTheme()
	lines := tr.renderLines(th, 80)
	if got, want := lines[2], th.diffRemoved.Render("    - a removed line"); got != want {
		t.Errorf("removed line = %q; want the diffRemoved style %q", got, want)
	}
	if got, want := lines[3], th.diffAdded.Render("    + an added line"); got != want {
		t.Errorf("added line = %q; want the diffAdded style %q", got, want)
	}
}

// The layout.md sketch, rendered: a two-line change shows "+2 -2" on the branch beside the path
// with the diff body beneath it, and the diffstat line itself stays plain — only the body is
// coloured, so the branch reads like every other tool's summary. The sketch is the EXPANDED
// shape: a collapsed diff hides its body whole like every other block (collapsedBodyRows), so its
// hunks are what a click reveals.
func TestRenderDiffMatchesLayoutSketch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "- a code line that has been removed\n- a second removed line\n+ a new code line\n+ a second new line",
		Summary: domain.DiffStat{Added: 2, Removed: 2},
	}})
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the diff block expanded")
	}

	want := strings.Join([]string{
		"✦ View Diff ▼",
		"  ┕ main.go +2 -2",
		"    - a code line that has been removed",
		"    - a second removed line",
		"    + a new code line",
		"    + a second new line",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff sketch mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	th := newTheme()
	if got, want := tr.renderLines(th, 80)[1], th.toolDetailBright.Render("  ┕ main.go +2 -2"); got != want {
		t.Errorf("diffstat branch = %q; want the plain detail tone of an OPEN block %q", got, want)
	}
}

// A diff whose body is hidden still names the whole change on its branch: the diffstat counts
// every line, and the marker beneath counts every line the collapsed paint withheld.
func TestRenderDiffStatSurvivesTheBodyCap(t *testing.T) {
	const longDiff = 25 // well past the collapsed budget, so the stat and the paint cannot agree by luck
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: strings.TrimSuffix(strings.Repeat("+ added\n", longDiff), "\n"),
		Summary: domain.DiffStat{Added: longDiff},
	}})

	lines := strings.Split(renderPlain(tr, 80), "\n")
	if got, want := lines[1], "  ┕ main.go +"+strconv.Itoa(longDiff)+" -0"; got != want {
		t.Errorf("capped diff branch = %q, want %q (the stat spans the whole diff)", got, want)
	}
	if got, want := lines[len(lines)-1], "    +"+strconv.Itoa(longDiff)+" more lines"; got != want {
		t.Errorf("capped diff body ends %q, want %q", got, want)
	}
}

// TestCollapsedPaintTruncatesRetainedBodies pins the relocation itself: the entry KEEPS every
// body line it was given and the collapsed paint is the only thing that withholds them,
// synthesizing the "+N more lines" remainder the outcome builders used to bake in (layout.md,
// "Collapsed and expanded blocks" — truncation is a render-time act on retained facts). One budget
// answers for every body kind: a command's output and a diff alike paint NO body line collapsed
// (collapsedBodyRows) and the marker counts the body whole, down to a body of one line — there is
// no length at which a collapsed block starts previewing its output.
func TestCollapsedPaintTruncatesRetainedBodies(t *testing.T) {
	diffLines := func(n int) string {
		return strings.TrimSuffix(strings.Repeat("+ added\n", n), "\n")
	}
	cases := []struct {
		name      string
		build     func(tr *transcript)
		wantKept  int      // body lines the entry retains
		wantPaint []string // the body the collapsed block paints, marker included
	}{
		{
			name: "free-form output paints no line and counts them all",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
			},
			wantKept:  4,
			wantPaint: []string{"    +4 more lines"},
		},
		{
			name: "a diff body spends the same budget and is counted the same way",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: diffLines(4), Summary: domain.DiffStat{Added: 4}}})
			},
			wantKept:  4,
			wantPaint: []string{"    +4 more lines"},
		},
		{
			name: "a body of one line is hidden and counted like any other",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: "+ new line", Summary: domain.DiffStat{Added: 1}}})
			},
			wantKept:  1,
			wantPaint: []string{"    +1 more line"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)
			if got := tr.entries[0].tool.Details.len(); got != tc.wantKept {
				t.Errorf("retained body = %d lines, want the whole %d", got, tc.wantKept)
			}
			// The block is a header, a branch line, then its body: everything past the branch is
			// what the collapsed paint made of the retained lines.
			lines := strings.Split(renderPlain(tr, 80), "\n")
			if got, want := strings.Join(lines[2:], "\n"), strings.Join(tc.wantPaint, "\n"); got != want {
				t.Errorf("collapsed body mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestExpandedBlockPaintsItsWholeBody pins what the expanded state is FOR: the block paints every
// body line the entry retained and grows no remainder marker, and collapsing it again paints
// exactly the compact shape back. The round trip runs over one transcript rather than two
// fixtures, because that is the claim — nothing about the entry changes but the flag the painter
// reads (layout.md, "Collapsed and expanded blocks").
func TestExpandedBlockPaintsItsWholeBody(t *testing.T) {
	diffContent := func(n int) string { return strings.TrimSuffix(strings.Repeat("+ added\n", n), "\n") }
	paintedDiff := func(n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, "    + added")
		}
		return out
	}
	cases := []struct {
		name          string
		build         func(tr *transcript)
		wantCollapsed []string
		wantExpanded  []string
	}{
		{
			name: "free-form output expands from nothing to all of it",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
			},
			wantCollapsed: []string{"    +4 more lines"},
			wantExpanded:  []string{"    ok   a", "    ok   b", "    ok   c", "    PASS"},
		},
		{
			name: "a diff body expands from its marker to its hunks",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: diffContent(4), Summary: domain.DiffStat{Added: 4}}})
			},
			wantCollapsed: []string{"    +4 more lines"},
			wantExpanded:  paintedDiff(4),
		},
		{
			// The written lines are the body from the moment the call is announced, so this one
			// spends and expands past the budget with no result involved at all.
			name: "a write's own lines are hidden collapsed and expand whole",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "write_file",
					Arguments: []byte(`{"path":"notes.txt","content":"alpha\nbeta\ngamma\ndelta"}`)}})
			},
			wantCollapsed: []string{"    +4 more lines"},
			wantExpanded:  []string{"    + alpha", "    + beta", "    + gamma", "    + delta"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)
			// The block is a header, a branch line, then its body: everything past the branch is
			// what the block's state made of the retained lines.
			body := func() string {
				lines := strings.Split(renderPlain(tr, 80), "\n")
				return strings.Join(lines[2:], "\n")
			}

			if got, want := body(), strings.Join(tc.wantCollapsed, "\n"); got != want {
				t.Errorf("default paint mismatch (collapsed is the default):\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the tool-call entry toggled")
			}
			if got, want := body(), strings.Join(tc.wantExpanded, "\n"); got != want {
				t.Errorf("expanded paint mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if strings.Contains(body(), "more line") {
				t.Errorf("the expanded body kept a remainder marker:\n%s", body())
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false on the way back; want the entry toggled")
			}
			if got, want := body(), strings.Join(tc.wantCollapsed, "\n"); got != want {
				t.Errorf("re-collapsed paint mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestExpandedBlockLiftsItsDetailTone is design call 9 in the paint: a block's own text is dim while
// it is collapsed and a step brighter once it is open (theme.go's colFaintBright), so the block a
// reader opened stands out of the scrollback of closed ones around it. It holds for both shapes that
// have a state — the single block and the group member, which are painted by different functions
// (renderToolBranch, renderExpandedMember) and could drift apart.
//
// The tones are asserted as the theme's own roles rather than as SGR bytes, and the guard above the
// subtests fails the day the two roles resolve to the same colour: a contrast step that quietly went
// away would satisfy every equality beneath it.
func TestExpandedBlockLiftsItsDetailTone(t *testing.T) {
	th := newTheme()
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	if th.toolDetail.Render("x") == th.toolDetailBright.Render("x") {
		t.Fatal("the collapsed and the open detail tone paint identically; there is no contrast step to assert")
	}

	t.Run("a single block", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal",
			Arguments: []byte(`{"command":"go test ./..."}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nPASS"}})

		// Row 0 is the header and row 2 the remainder marker — chrome with roles of their own — so
		// row 1, the branch line, is the whole of what the collapsed paint says about the call.
		collapsed := tr.renderLines(th, 80)
		if want := th.toolDetail.Render(strip(collapsed[1])); collapsed[1] != want {
			t.Errorf("collapsed branch = %q; want the dim tone %q", collapsed[1], want)
		}

		if !tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = false; want the block opened")
		}
		// Open, everything below the header is the call's own: the branch line and every body row.
		for i, row := range tr.renderLines(th, 80)[1:] {
			if want := th.toolDetailBright.Render(strip(row)); row != want {
				t.Errorf("open row %d = %q; want the brighter tone %q", i+1, row, want)
			}
		}
	})

	t.Run("a group member", func(t *testing.T) {
		// Both members carry a MULTI-line body: a one-line output rides the branch as the call's
		// summary instead, which would leave the member with nothing to open.
		tr := runGroup(0, [2]string{"go build ./...", "ok\nbuilt"}, [2]string{"go vet ./...", "clean\ndone"})
		if !tr.setExpanded(1, true) {
			t.Fatal("setExpanded(1, true) = false; want the second member opened")
		}
		rows := tr.renderLines(th, 80)

		// A member row is not one style run — its ▶/▼ and, open, its gutter are chrome painted
		// beside the text — so the tone is asserted on the text the member is carrying.
		if want := th.toolDetail.Render("  ┝ go build ./..."); !strings.Contains(rows[1], want) {
			t.Errorf("the closed member = %q; want its row in the dim tone %q", rows[1], want)
		}
		if want := th.toolDetailBright.Render("go vet ./..."); !strings.Contains(rows[2], want) {
			t.Errorf("the open member's first row = %q; want its target in the brighter tone %q", rows[2], want)
		}
		if want := th.toolDetailBright.Render("clean"); !strings.Contains(rows[3], want) {
			t.Errorf("the open member's body = %q; want it in the brighter tone %q", rows[3], want)
		}
		if want := th.toolDetail.Render(memberGutter); !strings.Contains(rows[3], want) {
			t.Errorf("the open member's body = %q; want the gutter beside it still chrome %q", rows[3], want)
		}
	})
}

// …and the tone step is the PLAIN detail's alone: a diff line is red or green because of which way
// it went, and layering an emphasis step onto that would give the same colour two meanings. The two
// states are asked of the two painters that draw a targetless branch list — the collapsed one under
// the row budget (clipDetails) and the open one (renderDetails) — over the same line, so the
// comparison is of paint rather than of the style table. The plain case is the control: the same
// pair of painters must NOT agree there, or the diff assertion would hold by the tone step having
// gone missing altogether.
func TestDiffLinesKeepTheirColourInBothBlockStates(t *testing.T) {
	th := newTheme()
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	cases := []struct {
		name     string
		kind     detailKind
		wantSame bool
		style    lipgloss.Style
	}{
		{name: "an added line keeps its green", kind: detailDiffAdded, wantSame: true, style: th.diffAdded},
		{name: "a removed line keeps its red", kind: detailDiffRemoved, wantSame: true, style: th.diffRemoved},
		{name: "a plain line takes the state's tone", kind: detailPlain, wantSame: false, style: th.toolDetail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := []detailLine{{Kind: tc.kind, Text: "+ added"}}
			closed, _ := clipDetails(th, line, 40)
			open := renderDetails(th, line, 40)
			if len(closed) != 1 || len(open) != 1 {
				t.Fatalf("the painters spent %d and %d rows on one line; want one each", len(closed), len(open))
			}
			if same := closed[0] == open[0]; same != tc.wantSame {
				t.Errorf("closed = %q, open = %q; want the two paints same=%v", closed[0], open[0], tc.wantSame)
			}
			if want := tc.style.Render(strip(closed[0])); closed[0] != want {
				t.Errorf("closed paint = %q; want the kind's own style %q", closed[0], want)
			}
		})
	}
}

// TestCollapsedBlockStandsAtMostFourRows is the cap itself, asked of the case that used to break
// it: a 400-character command soft-wrapped over five rows before the row budget existed, and the
// block it led stood seven rows tall in a scrollback of them. Now the block is its header, two rows
// of target with the clip's " …" saying the target goes on, and the marker counting the body — four
// rows, whatever the target's length and whatever the body's (docs/layout/tool-layout.md).
//
// The width bound is asserted on every row rather than assumed from the wrap: the clip re-cuts the
// row it ends, and a tail appended past the column would fold that row in two and spend a fifth row
// the budget does not have (clipWrap).
func TestCollapsedBlockStandsAtMostFourRows(t *testing.T) {
	const width = 80
	command := strings.Repeat("cd . && head -3 go.mod && ", 16)[:400]

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok\nPASS\ndone"}})

	lines := strings.Split(renderPlain(tr, width), "\n")
	if len(lines) != 4 {
		t.Fatalf("the collapsed block stands %d rows tall, want the budget's 4:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if want := "✦ Run " + glyphCollapsed; lines[0] != want {
		t.Errorf("header = %q, want %q", lines[0], want)
	}
	if !strings.HasSuffix(lines[2], clipTail) {
		t.Errorf("the last target row = %q, want it cut short with %q", lines[2], clipTail)
	}
	if want := "    +3 more lines"; lines[3] != want {
		t.Errorf("marker row = %q, want %q", lines[3], want)
	}
	th := newTheme()
	for i, ln := range lines {
		if w := th.measure.Width(ln); w > width {
			t.Errorf("row %d paints %d columns, past the %d-column block: %q", i, w, width, ln)
		}
	}
}

// …and the target's own clip is enough to make a block a toggle target: a call with NO body at all
// still hides something once its target is cut, so it wears the indicator, is marked for the click,
// and expands to the target whole. That is what makes the toggle-target rule WIDTH-dependent — the
// same call at a wide enough block hides nothing and is no target — and why the indicator and the
// mark are one predicate asked with the width the frame is painted at
// (blockHidesWhenCollapsed).
func TestClippedTargetAloneMakesABlockAToggleTarget(t *testing.T) {
	const width = 60
	path := "internal/" + strings.Repeat("deeply-nested-package/", 6) + "main.go"

	tr := &transcript{}
	readCall(tr, "c1", path, 1, 154, 0)
	if got := tr.entries[0].tool.Details.len(); got != 0 {
		t.Fatalf("the fixture's call carries %d body lines, want the bodiless case", got)
	}

	collapsed := strings.Split(renderPlain(tr, width), "\n")
	if len(collapsed) != 3 || !strings.HasSuffix(collapsed[2], clipTail) {
		t.Errorf("collapsed paint = %d rows ending %q, want 3 rows ending in the clip tail:\n%s",
			len(collapsed), collapsed[len(collapsed)-1], strings.Join(collapsed, "\n"))
	}
	// And it is a target WHOLE: the header and both clipped target rows are one click surface, so
	// the reader opens the path by clicking the path (renderToolBlock).
	marks := blockMarks(t, tr, width)
	if len(marks) != len(collapsed) {
		t.Fatalf("collapsed block marked %d of its %d rows, want every one:\n%+v",
			len(marks), len(collapsed), marks)
	}
	if marks[0].text != "✦ Read File "+glyphCollapsed {
		t.Errorf("header = %q, want the label wearing the collapsed indicator", marks[0].text)
	}
	for i, mark := range marks {
		if mark.line != i || mark.kind != targetHeader || mark.entry != 0 {
			t.Errorf("mark %d = %+v; want line %d, a toggle naming entry 0", i, mark, i)
		}
	}

	if !tr.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the clipped block expanded")
	}
	expanded := renderPlain(tr, width)
	if strings.Contains(expanded, clipTail) {
		t.Errorf("the expanded paint still carries a clip tail:\n%s", expanded)
	}
	if flat := strings.Join(strings.Fields(expanded), ""); !strings.Contains(flat, strings.ReplaceAll(path, " ", "")) {
		t.Errorf("the expanded paint does not carry the whole target:\n%s", expanded)
	}

	// The same call in a block wide enough to seat its target hides nothing and is no target at
	// all — the predicate follows the width, which is the half a fixed-width test cannot show.
	if got := blockMarks(t, tr, 200); got != nil {
		t.Errorf("marks at a width the target fits = %+v, want none", got)
	}
}

// groupMemberLine composes a collapsed member row the way the painter lays it out: the row's own
// text, then the ▶ flush against the block's right edge. A golden line then reads as the text it
// carries rather than as a hand-counted run of spaces, and the count moves with the width.
func groupMemberLine(t *testing.T, text string, width int) string {
	t.Helper()
	return memberEdgeRow(t, text, glyphCollapsed, width)
}

// memberEdgeRow is that arithmetic for any right-aligned mark a member row carries — the ▶ of a
// collapsed one, the ▼ an open one wears on its first row, the see-less marker closing it. One
// definition of "flush against the block's right edge", so a golden that moves because the edge
// moved fails everywhere at once instead of in one place.
func memberEdgeRow(t *testing.T, text, mark string, width int) string {
	t.Helper()
	th := newTheme()
	pad := width - th.measure.Width(text) - th.measure.Width(mark)
	if pad < 0 {
		t.Fatalf("member row %q plus %q does not fit width %d", text, mark, width)
	}
	return text + strings.Repeat(" ", pad) + mark
}

// runGroup folds a batch of same-label terminal calls, each with its output, into a fresh
// transcript at depth — the sketch's "✦ Run (3)" fixture (docs/layout/tool-layout.md). Each call is
// a {command, output} pair. They carry bodies deliberately: that is what gives every member
// something to reveal and so a state of its own to be opened.
func runGroup(depth int, calls ...[2]string) *transcript {
	tr := &transcript{}
	base := domain.EventBase{Depth: depth}
	for i, c := range calls {
		id := "c" + strconv.Itoa(i+1)
		tr.apply(domain.ToolCallEvent{EventBase: base, Call: domain.ToolCall{ID: id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c[0] + `"}`)}})
		tr.apply(domain.ToolResultEvent{EventBase: base,
			Result: domain.ToolResult{CallID: id, Content: c[1]}})
	}
	return tr
}

// summaryColumn is the display CELL a row's summary opens in — the column the eye reads it in,
// which a byte offset is not: a branch marker and a clip tail are three bytes each and one cell
// each, so two rows carrying different glyphs land at different byte offsets in the same column.
func summaryColumn(t *testing.T, row, summary string) int {
	t.Helper()
	at := strings.Index(row, summary)
	if at < 0 {
		t.Fatalf("row %q carries no summary %q", row, summary)
	}
	return newTheme().measure.Width(row[:at])
}

// TestRenderGroupsBodyCarryingCalls is the grouping scope's new half (design call 3): a call that
// carries a body groups exactly as a bodiless one does, and pays for it with a member row held to
// ONE line. Three Runs with output are the sketch's own case
// (docs/layout/tool-layout.md): one header counting them, one row each, and every ▶ flush against
// the block's right edge, whatever the commands beneath it are doing.
func TestRenderGroupsBodyCarryingCalls(t *testing.T) {
	tr := &transcript{}
	for _, c := range []struct{ id, command, output string }{
		{"c1", "go build ./...", "ok\nbuilt"},
		{"c2", "go vet ./...", "clean\nno findings\ndone"},
		{"c3", "go test ./...", "ok\nPASS"},
	} {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: c.id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c.command + `"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: c.id, Content: c.output}})
	}

	want := strings.Join([]string{
		"✦ Run (3)",
		groupMemberLine(t, "  ┝ go build ./...", 80),
		groupMemberLine(t, "  ┝ go vet ./...", 80),
		groupMemberLine(t, "  ┕ go test ./...", 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("body-carrying group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The count is the group's own arithmetic and is painted as such: it rides the header in the faint
// indicator tone rather than in the label's bold orange, so a reader scanning the orange down the
// left edge does not read "(3)" as part of the tool's name (design call 6). The header wears no
// state indicator and takes no click — the members own their state.
func TestGroupHeaderCountIsFaintAndInert(t *testing.T) {
	th := newTheme()
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	tr := &transcript{}
	readCall(tr, "c1", "main.go", 1, 154, 0)
	readCall(tr, "c2", "util.go", 1, 42, 0)

	header := tr.renderLines(th, 80)[0]
	if want := th.toolIndicator.Render("(2)"); !strings.Contains(header, want) {
		t.Errorf("header %q does not carry the faint-styled count %q", header, want)
	}
	if styled := th.toolLabel.Render("Read File (2)"); strings.Contains(header, styled) {
		t.Errorf("header %q paints the count in the label's own style", header)
	}
	if strings.ContainsAny(strip(header), glyphCollapsed+glyphExpanded) {
		t.Errorf("group header %q wears a state indicator; the members own their state", strip(header))
	}
	if got := blockMarks(t, tr, 80); got != nil {
		t.Errorf("group marks = %+v, want none — a group header is not a click target", got)
	}
}

// A member's summary is never traded away for more of its target: the outcome's cells are reserved
// first and the target takes what is left, ending in the clip tail. The indicator field is reserved
// on every member, so the ▶ of the member that has something to reveal lands in the same column the
// bare rows leave empty.
func TestGroupMemberKeepsItsSummaryAndClipsTheTarget(t *testing.T) {
	const width = 60
	long := "cd . && " + strings.Repeat("echo one-more-fragment && ", 6) + "true"

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal",
		Arguments: []byte(`{"command":"` + long + `"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal",
		Arguments: []byte(`{"command":"pwd"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "/repo"}})

	rows := strings.Split(renderPlain(tr, width), "\n")
	if len(rows) != 3 {
		t.Fatalf("group painted %d rows, want 3 — one header and one row per member:\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	if !strings.HasSuffix(rows[1], glyphCollapsed) || !strings.Contains(rows[1], clipTail) {
		t.Errorf("clipped member = %q; want the target cut with %q and the ▶ at the edge",
			rows[1], clipTail)
	}
	if got := newTheme().measure.Width(rows[1]); got != width {
		t.Errorf("clipped member measures %d cells, want the full %d", got, width)
	}
	if strings.HasSuffix(rows[2], glyphCollapsed) {
		t.Errorf("short member = %q; it hides nothing and must wear no indicator", rows[2])
	}
	// The summaries open in the SAME column though one target was cut and the other padded: the
	// column is the block's, capped once against its widest summary (groupTargetCells). Measured in
	// display cells, not bytes — both rows carry multi-byte glyphs the other does not.
	a, b := summaryColumn(t, rows[1], "abc1234"), summaryColumn(t, rows[2], "/repo")
	if a != b {
		t.Errorf("summaries open at columns %d and %d; want one shared column:\n%s\n%s",
			a, b, rows[1], rows[2])
	}
}

// TestExpandedGroupMemberPaintsTheSketchShape is the open member, whole, against the sketch's
// "middle one expanded" (docs/layout/tool-layout.md): the branch marker and the full target with ▼
// where the ▶ was, the body under a │ gutter, and a right-aligned see-less row closing it — with
// the siblings still one row each, untouched. It is one golden because the shape is the point: a
// gutter that lost its space or a see-less row that drifted off the edge is the failure this catches.
func TestExpandedGroupMemberPaintsTheSketchShape(t *testing.T) {
	const width = 80
	tr := runGroup(0,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"},
		[2]string{"go test ./...", "ok\nPASS"})
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	want := strings.Join([]string{
		"✦ Run (3)",
		groupMemberLine(t, "  ┝ go build ./...", width),
		memberEdgeRow(t, "  ┝ go vet ./...", glyphExpanded, width),
		"  │ clean",
		"  │ no findings",
		"  │ done",
		memberEdgeRow(t, "  │ ", promptSeeLess, width),
		groupMemberLine(t, "  ┕ go test ./...", width),
	}, "\n")
	got := renderPlain(tr, width)
	if got != want {
		t.Errorf("open-member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "more line") {
		t.Errorf("the open member grew a remainder marker; it hides nothing:\n%s", got)
	}
}

// Every row an open member paints belongs to that member and says so: the marks name entry 1 down
// the whole of it — first row, body and see-less row alike — while the siblings' single rows name
// entries 0 and 2 and the group header names nothing at all. This is the click surface the mouse
// then resolves against (mouse.go, toggleBlockAt).
func TestGroupMemberMarksNameTheirOwnCalls(t *testing.T) {
	tr := runGroup(0,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"},
		[2]string{"go test ./...", "ok\nPASS"})
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	// Line 0 is the header and carries no mark, so the marks start on line 1 and run to the end:
	// one row for the first member, six for the open one, one for the last.
	marks := blockMarks(t, tr, 80)
	want := []int{0, 1, 1, 1, 1, 1, 2} // the entry each marked line names, in paint order
	if len(marks) != len(want) {
		t.Fatalf("group painted %d marked rows, want %d:\n%+v", len(marks), len(want), marks)
	}
	for i, mark := range marks {
		if mark.line != i+1 || mark.kind != targetHeader || mark.entry != want[i] {
			t.Errorf("mark %d = %+v; want line %d, a toggle naming entry %d", i, mark, i+1, want[i])
		}
	}
}

// An open member nested inside a sub-agent run wears TWO vertical bars on the same row, and they
// must not read as one: the run's rail is the label orange and the member's gutter is the detail
// tone (design call 8). Painted in the same style, a member's body would look like a section of the
// delegate's frame rather than the output of one call inside it.
func TestExpandedMemberGutterIsNotTheSubAgentRail(t *testing.T) {
	th := newTheme()
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	tr := runGroup(1,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"})
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	var row string
	for _, ln := range tr.renderLines(th, 80) {
		if strings.Contains(strip(ln), "│ built") {
			row = ln
		}
	}
	if row == "" {
		t.Fatalf("no body row under the member gutter in:\n%s", strings.Join(tr.renderLines(th, 80), "\n"))
	}
	if !strings.Contains(row, th.toolDetail.Render(memberGutter)) {
		t.Errorf("row %q does not carry the gutter in the detail tone", row)
	}
	if strings.Contains(row, th.subRail.Render(memberGutter)) {
		t.Errorf("row %q paints the member gutter in the sub-agent rail's style", row)
	}
	if !strings.HasPrefix(row, th.subRail.Render(glyphSubRail+" ")) {
		t.Errorf("row %q lost the run's own rail; the fixture no longer nests the group", row)
	}
}

// ----------------------------------------------------------------------------
// The answered Ask User block: an ordinary body-bearing block (layout.md, "Collapsed and
// expanded blocks")
// ----------------------------------------------------------------------------

// askUserCall folds an ask_user call and, where answer is non-empty, the human's reply — the two
// halves the answered block's record is built from (the question and choices are the CALL's,
// the ticks are the RESULT's). An empty answer leaves the question pending, which is the state the
// popup owns and the block says nothing about.
func askUserCall(tr *transcript, id, args, answer string) {
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "ask_user", Arguments: []byte(args)}})
	if answer != "" {
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: answer}})
	}
}

// TestAnsweredAskUserBlockPaintsTheRecord walks the answered question through both block states:
// the record is a body like any other, so the collapsed block withholds it whole behind the
// remainder marker and the expanded one paints it, with the answer riding the branch throughout. No
// painter rule is new here — that is the claim. Once the presenter hands the block a body, the
// machinery already in place gives the exchange its permanent shape.
func TestAnsweredAskUserBlockPaintsTheRecord(t *testing.T) {
	tr := &transcript{}
	askUserCall(tr, "c1", `{"question":"Which mode?","choices":["Plan","Ask before","Auto"]}`, "Ask before")

	collapsed := strings.Join([]string{
		"✦ Ask User ▶",
		"  ┕ Which mode? Ask before",
		"    +4 more lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != collapsed {
		t.Errorf("collapsed record mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the answered question expanded")
	}
	expanded := strings.Join([]string{
		"✦ Ask User ▼",
		"  ┕ Which mode? Ask before",
		"    Which mode?",
		"    [ ] Plan",
		"    [x] Ask before",
		"    [ ] Auto",
	}, "\n")
	if got := renderPlain(tr, 80); got != expanded {
		t.Errorf("expanded record mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, expanded)
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false on the way back; want the block collapsed again")
	}
	if got := renderPlain(tr, 80); got != collapsed {
		t.Errorf("re-collapsed record mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}
}

// …and because the collapsed paint now hides something, the block becomes a toggle target by the
// one predicate that decides both the affordance and the click (blockHidesWhenCollapsed): every row
// it paints is marked, the header wearing the ▶/▼ indicator, the remainder marker keeping its own
// open-only kind, and expanding takes the marker away while the block — the answers it now shows
// included — keeps the click that closes it again. A question still on the screen hides nothing and
// is no target at all.
func TestAnsweredAskUserBlockIsAToggleTarget(t *testing.T) {
	const question = `{"question":"Which mode?","choices":["Plan","Ask before","Auto"]}`

	t.Run("an answered question marks its rows and its marker", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", question, "Ask before")

		want := []blockMark{
			{line: 0, kind: targetHeader, entry: 0, text: "✦ Ask User ▶"},
			{line: 1, kind: targetHeader, entry: 0, text: "  ┕ Which mode? Ask before"},
			{line: 2, kind: targetMarker, entry: 0, text: "    +4 more lines"},
		}
		if got := blockMarks(t, tr, 80); !reflect.DeepEqual(got, want) {
			t.Errorf("collapsed marks mismatch:\n--- got ---\n%+v\n--- want ---\n%+v", got, want)
		}

		if !tr.toggleExpanded(0) {
			t.Fatal("toggleExpanded(0) = false; want the answered question expanded")
		}
		want = []blockMark{
			{line: 0, kind: targetHeader, entry: 0, text: "✦ Ask User ▼"},
			{line: 1, kind: targetHeader, entry: 0, text: "  ┕ Which mode? Ask before"},
			{line: 2, kind: targetHeader, entry: 0, text: "    Which mode?"},
			{line: 3, kind: targetHeader, entry: 0, text: "    [ ] Plan"},
			{line: 4, kind: targetHeader, entry: 0, text: "    [x] Ask before"},
			{line: 5, kind: targetHeader, entry: 0, text: "    [ ] Auto"},
		}
		if got := blockMarks(t, tr, 80); !reflect.DeepEqual(got, want) {
			t.Errorf("expanded marks mismatch:\n--- got ---\n%+v\n--- want ---\n%+v", got, want)
		}
	})

	t.Run("a pending question is no target", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", question, "")

		if got := blockMarks(t, tr, 80); got != nil {
			t.Errorf("pending question marks = %+v, want none — the popup is its live view", got)
		}
	})
}

// The record breaks the grouping a question used to fold into, and now SAYS so: the presenter marks
// an answered record solo (askUserAnswerRecord), so consecutive answered questions each keep a block
// of their own with the room the exchange needs. It used to be kept apart by the body it carries,
// back when grouping admitted bodiless calls only; a Run and its output group now, so the exclusion
// had to become a statement rather than a side effect. Pending questions still group — nothing has
// been answered, so there is no record to stand alone — which is what keeps this a rule about
// records and not a rule about Ask User.
func TestAnsweredAskUserBlocksNeverGroup(t *testing.T) {
	t.Run("answered questions stand alone", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", `{"question":"Ship it?","choices":["Yes","No"]}`, "Yes")
		askUserCall(tr, "c2", `{"question":"Tag it?","choices":["Yes","No"]}`, "No")

		if !tr.entries[0].tool.solo {
			t.Error("the answered record is not marked solo; the split would rest on its body again")
		}

		want := strings.Join([]string{
			"✦ Ask User ▶",
			"  ┕ Ship it? Yes",
			"    +3 more lines",
			"",
			"✦ Ask User ▶",
			"  ┕ Tag it? No",
			"    +3 more lines",
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("answered questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// The mark is a verdict the presenter reached when the result landed, and decode never re-runs a
	// presenter — so it rides the wire (wireToolView.Solo). Without it a resumed session would fold
	// two records into one group, which is the scrollback changing shape across a restart.
	t.Run("a replayed record still stands alone", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", `{"question":"Ship it?","choices":["Yes","No"]}`, "Yes")
		askUserCall(tr, "c2", `{"question":"Tag it?","choices":["Yes","No"]}`, "No")
		before := renderPlain(tr, 80)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		entries, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if got := renderPlain(&transcript{entries: entries}, 80); got != before {
			t.Errorf("replayed records mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, before)
		}
	})

	t.Run("pending questions still group", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", `{"question":"Ship it?","choices":["Yes","No"]}`, "")
		askUserCall(tr, "c2", `{"question":"Tag it?","choices":["Yes","No"]}`, "")

		want := strings.Join([]string{
			"✦ Ask User (2)",
			"  ┝ Ship it?",
			"  ┕ Tag it?",
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("pending questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// A sub-agent call heads a run rather than joining one: what hangs beneath its block is a whole
// delegation (renderSubAgentRun), so two delegations in a row are two blocks and never one
// "✦ Sub-Agent (2)" counting them. The painter's span rule cannot carry that on its own — it fires
// only for a head with nested entries behind it, and a delegation refused at the depth bound
// (executeRefuse, internal/agent) leaves a head with NO span, which is a call of the right shape
// sitting beside another. The presenter states the fact instead (presentToolCall marks the call
// solo), so the rule holds for the empty run as well as the full one.
func TestSpanlessSubAgentHeadsNeverGroup(t *testing.T) {
	const refusal = "sub-agent depth limit reached (max 2): cannot spawn a deeper sub-agent"
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"first"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "s1", Content: refusal, IsError: true}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s2", Tool: "sub_agent", Arguments: []byte(`{"task":"second"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "s2", Content: refusal, IsError: true}})

	for i := range tr.entries {
		if span := subAgentSpan(tr.entries, i); span != 0 {
			t.Fatalf("entry %d heads a span of %d; the premise here is a head with none", i, span)
		}
	}
	if run := toolCallRun(tr.entries, 0); run != nil {
		t.Fatalf("toolCallRun over the two refused delegations = %d views, want none — a solo call heads no run", len(run))
	}

	want := strings.Join([]string{
		"✦ Sub-Agent",
		"  ┕ first error: sub-agent depth limit reached (max 2): cannot spawn a deeper",
		"    sub-agent",
		"",
		"✦ Sub-Agent",
		"  ┕ second error: sub-agent depth limit reached (max 2): cannot spawn a deeper",
		"    sub-agent",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("refused delegations mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
	lines := tr.renderLines(newTheme(), width)
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
	th := newTheme()
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
	th := newTheme()
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
	th := newTheme()
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
	th := newTheme()
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
	th := newTheme()
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
	th := newTheme()
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

// ----------------------------------------------------------------------------
// The click surface: which rendered lines toggle a block (layout.md, "Collapsed and expanded
// blocks")
// ----------------------------------------------------------------------------

// blockMark is one line the painter marked as a block's click surface: where the line sits, what
// it is, whose block a click there toggles, and the text painted on it. The text is carried so a
// failure names the line that moved rather than reporting a bare index mismatch — and so the
// assertion pins the mark to the shape, not merely to a number.
type blockMark struct {
	line  int
	kind  targetKind
	entry int
	text  string
}

// blockMarks renders tr and returns its marked lines in order. It first asserts the map's standing
// invariant — exactly one target per rendered line — which is what makes an index into the lines
// safe to use as an index into the targets on the mouse path (model.go).
func blockMarks(t *testing.T, tr *transcript, width int) []blockMark {
	t.Helper()
	rendered := tr.renderView(newTheme(), width, false)
	if len(rendered.targets) != len(rendered.lines) {
		t.Fatalf("targets and lines out of lockstep: %d targets for %d lines",
			len(rendered.targets), len(rendered.lines))
	}
	var marks []blockMark
	for i, target := range rendered.targets {
		if target.kind == targetNone {
			continue
		}
		marks = append(marks, blockMark{
			line:  i,
			kind:  target.kind,
			entry: target.entry,
			text:  strings.TrimRight(ansiPattern.ReplaceAllString(rendered.lines[i], ""), " "),
		})
	}
	return marks
}

// TestRenderMarksTheWholeBlockAndItsMarker pins the whole target rule in one table: a single tool
// block that hides something is a click surface WHOLE — every row it paints, its header, its target
// rows and (open) its body, each carrying the index of the entry a click there toggles — its
// synthesized remainder marker apart, which keeps its own open-only kind. A block that hides nothing
// marks no row at all. Every case asserts the complete set of marks, so a line that quietly became
// clickable, or quietly stopped being, fails here.
//
// It pins the AFFORDANCE against the same rule, because each mark carries its line's text: a marked
// block wears the ▶/▼ state indicator on its header and an unmarked one wears none, so the visible
// hint and the click target cannot drift apart — a block that grew an indicator without becoming
// clickable, or became clickable without growing one, fails here too.
func TestRenderMarksTheWholeBlockAndItsMarker(t *testing.T) {
	// run folds a terminal call and its multi-line output — the block with a body, and therefore
	// the block with something to reveal.
	run := func(tr *transcript, id, command, output string, depth int) {
		base := domain.EventBase{Depth: depth}
		tr.apply(domain.ToolCallEvent{EventBase: base,
			Call: domain.ToolCall{ID: id, Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
		tr.apply(domain.ToolResultEvent{EventBase: base,
			Result: domain.ToolResult{CallID: id, Content: output}})
	}
	cases := []struct {
		name  string
		width int
		build func(t *testing.T, tr *transcript)
		want  []blockMark
	}{
		{
			// ❯ run the tests | (spacer) | ✦ Run | ┕ go test ./... | +4 more lines — the header and
			// the branch line beneath it are one surface, the marker its own open-only kind.
			name:  "a hidden body marks the block's rows and its remainder marker",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.addUser("run the tests", nil)
				run(tr, "c1", "go test ./...", "ok   a\nok   b\nok   c\nPASS", 0)
			},
			want: []blockMark{
				{line: 2, kind: targetHeader, entry: 1, text: "✦ Run ▶"},
				{line: 3, kind: targetHeader, entry: 1, text: "  ┕ go test ./..."},
				{line: 4, kind: targetMarker, entry: 1, text: "    +4 more lines"},
			},
		},
		{
			// The state does not decide the target: an expanded block keeps every row marked — that
			// is the click that collapses it again, wherever in the output the pointer happens to be
			// — and has no marker left to mark.
			name:  "an expanded block marks its body too and loses its marker",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.addUser("run the tests", nil)
				run(tr, "c1", "go test ./...", "ok   a\nok   b\nok   c\nPASS", 0)
				if !tr.toggleExpanded(1) {
					t.Fatal("toggleExpanded(1) = false; want the tool-call entry expanded")
				}
			},
			want: []blockMark{
				{line: 2, kind: targetHeader, entry: 1, text: "✦ Run ▼"},
				{line: 3, kind: targetHeader, entry: 1, text: "  ┕ go test ./..."},
				{line: 4, kind: targetHeader, entry: 1, text: "    ok   a"},
				{line: 5, kind: targetHeader, entry: 1, text: "    ok   b"},
				{line: 6, kind: targetHeader, entry: 1, text: "    ok   c"},
				{line: 7, kind: targetHeader, entry: 1, text: "    PASS"},
			},
		},
		{
			// The other half of the rule, on the shape that has a body row to offer: a short call
			// with no body hides nothing at this width, so not one of its rows is a click target and
			// a click anywhere on it keeps its selection meaning.
			name:  "a block that hides nothing marks no row at all",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
			},
			want: nil,
		},
		{
			// A group's calls carry no bodies (that is what made them groupable), so the block
			// hides nothing and its header keeps a click's selection meaning.
			name:  "a body-less group is no target at all",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			want: nil,
		},
		{
			// The targetless shape is capped like every other: an unregistered tool's verbatim
			// arguments ARE its branches, and a blob that overflows the cap makes the header a
			// target with a marker beneath it, exactly as a body would.
			name:  "a targetless block over the cap marks its header and its marker",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "weird_tool", Arguments: []byte(`{"a":1,"b":2,"c":3}`)}})
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ weird_tool ▶"},
				{line: 1, kind: targetHeader, entry: 0, text: "  ┝ a:"},
				{line: 2, kind: targetHeader, entry: 0, text: "  ┕   1"},
				{line: 3, kind: targetMarker, entry: 0, text: "    +4 more lines"},
			},
		},
		{
			// The cap is what decides, not the shape: a targetless call whose whole branch list
			// fits hides nothing and keeps a click's selection meaning.
			name:  "a targetless block inside the cap marks nothing",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "weird_tool", Arguments: []byte(`"go"`)}})
			},
			want: nil,
		},
		{
			// Narrow enough that both the header and the marker wrap: the click lands on the
			// header, not on its first row, so EVERY physical line of each is marked.
			name:  "a wrapped header and a wrapped marker mark all their physical lines",
			width: 11,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "python_exec", Arguments: []byte(`{"code":"print(1)"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "1\n2\n3"}})
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Run"},
				{line: 1, kind: targetHeader, entry: 0, text: "  Python ▶"},
				{line: 2, kind: targetHeader, entry: 0, text: "  ┕ print(1"},
				{line: 3, kind: targetHeader, entry: 0, text: "    )"},
				{line: 4, kind: targetMarker, entry: 0, text: "    +3 more"},
				{line: 5, kind: targetMarker, entry: 0, text: "    lines"},
			},
		},
		{
			// Two blocks of the same shape: each header and marker names its OWN head entry, which
			// is the whole of what the index is for. The approval note between them is what keeps
			// them two blocks — two same-label calls are one group now, however much body they
			// carry (groupable) — and it makes the second block's index 2, which a mark that
			// counted blocks rather than entries would get wrong.
			name:  "each block's marks carry its own entry index",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				run(tr, "c1", "go build ./...", "a\nb\nc", 0)
				tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
				run(tr, "c2", "go vet ./...", "x\ny", 0)
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Run ▶"},
				{line: 1, kind: targetHeader, entry: 0, text: "  ┕ go build ./..."},
				{line: 2, kind: targetMarker, entry: 0, text: "    +3 more lines"},
				{line: 6, kind: targetHeader, entry: 2, text: "✦ Run ▶"},
				{line: 7, kind: targetHeader, entry: 2, text: "  ┕ go vet ./..."},
				{line: 8, kind: targetMarker, entry: 2, text: "    +2 more lines"},
			},
		},
		{
			// A sub-agent run's head is a target for its SPAN alone. This one is still working, so
			// it has no body to truncate and nothing among its views hides anything — what a click
			// there reveals is the elided run behind it, and only the span rule knows that.
			name:  "a working sub-agent head is a target for its elided span",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent ▶"},
				{line: 1, kind: targetHeader, entry: 0, text: "  ┕ survey the tests 1 tool call"},
			},
		},
		{
			// Expanded, the run's head keeps its mark — that is the click that closes it again —
			// even though its own one-line report hides nothing. The span it reveals carries no
			// marks of its own here, the read inside it having nothing to reveal either.
			name:  "an expanded sub-agent head stays clickable",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the run's head expanded")
				}
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent ▼"},
				{line: 1, kind: targetHeader, entry: 0, text: "  ┕ survey the tests survey complete"},
			},
		},
		{
			// A railed sub-agent block is marked exactly like a flat one — the rail prefixes lines
			// and adds none — and the ⤷ descent label the run opens with is no target.
			name:  "a nested block keeps its marks behind the rail",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				run(tr, "c1", "go test", "a\nb\nc", 1)
			},
			want: []blockMark{
				{line: 2, kind: targetHeader, entry: 0, text: "│ ✦ Run ▶"},
				{line: 3, kind: targetHeader, entry: 0, text: "│   ┕ go test"},
				{line: 4, kind: targetMarker, entry: 0, text: "│     +3 more lines"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(t, tr)

			if got := blockMarks(t, tr, tc.width); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("marked lines mismatch:\n--- got ---\n%+v\n--- want ---\n%+v", got, tc.want)
			}
		})
	}
}

// TestHeaderIndicatorFollowsTheBlockState pins the indicator's OTHER half: unlike the click mark,
// which is state-independent by design, the glyph says which way the click will go — ▶ while the
// block is collapsed, ▼ while it is expanded — and it follows the state back and forth on one
// transcript rather than across two fixtures, because that is the claim: nothing about the entry
// changes but the flag the painter reads. The block kinds that reach the indicator by three
// different routes are each here: a hidden body (blockHidesWhenCollapsed), a sub-agent run's elided
// span (blockState.elides) and a Firing wearing the borrowed shape under its own glyph. A CLIPPED
// TARGET is the fourth route into the same predicate and has a test of its own, the width being
// what it turns on (TestClippedTargetAloneMakesABlockAToggleTarget).
func TestHeaderIndicatorFollowsTheBlockState(t *testing.T) {
	cases := []struct {
		name                        string
		build                       func() *transcript
		wantCollapsed, wantExpanded string
	}{
		{
			name: "a hidden body",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok\na\nb"}})
				return tr
			},
			wantCollapsed: "✦ Run ▶", wantExpanded: "✦ Run ▼",
		},
		{
			name: "a sub-agent run's elided span",
			build: func() *transcript {
				tr := &transcript{}
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
				return tr
			},
			wantCollapsed: "✦ Sub-Agent ▶", wantExpanded: "✦ Sub-Agent ▼",
		},
		{
			name:          "a Firing under its own glyph",
			build:         func() *transcript { return firingBlock("found 3 stale entries\nremoved them") },
			wantCollapsed: "⟳ Schedule ▶", wantExpanded: "⟳ Schedule ▼",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := tc.build()

			if got := headerStar(t, tr, false); got != tc.wantCollapsed {
				t.Errorf("collapsed header = %q, want %q", got, tc.wantCollapsed)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the block expanded")
			}
			if got := headerStar(t, tr, false); got != tc.wantExpanded {
				t.Errorf("expanded header = %q, want %q", got, tc.wantExpanded)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false on the way back; want the block collapsed again")
			}
			if got := headerStar(t, tr, false); got != tc.wantCollapsed {
				t.Errorf("re-collapsed header = %q, want %q", got, tc.wantCollapsed)
			}
		})
	}
}

// The indicator is painted apart from the label: the detail tone, never toolLabel's bold orange, so
// the affordance reads as chrome beside the tool's name rather than as the last letter of it. The
// assertion is against the theme's own roles rather than a lipgloss byte-golden, and the second
// guard catches the opposite failure — an indicator styled into the label's run.
func TestHeaderIndicatorIsStyledApartFromTheLabel(t *testing.T) {
	th := newTheme()
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok\na\nb"}})

	header := tr.renderLines(th, 80)[0]

	if want := th.toolIndicator.Render(glyphCollapsed); !strings.Contains(header, want) {
		t.Errorf("header %q does not carry the detail-toned indicator %q", header, want)
	}
	if styledIntoTheLabel := th.toolLabel.Render("Run " + glyphCollapsed); strings.Contains(header, styledIntoTheLabel) {
		t.Errorf("header %q paints the indicator inside the label's own run", header)
	}
}

// The synthesized "+N more lines" marker carries its OWN style role rather than the body's: it is
// apogee's line, not the tool's, and a body line that happens to open with "…" must not be able to
// look like one. The two roles are asserted to differ as well as to be applied, so a marker style
// that quietly became detailStyle's twin fails here rather than passing silently.
func TestRemainderMarkerCarriesItsOwnStyle(t *testing.T) {
	th := newTheme()
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nPASS"}})

	rendered := tr.renderView(th, 80, false)
	var marker string
	for i, target := range rendered.targets {
		if target.kind == targetMarker {
			marker = rendered.lines[i]
		}
	}
	if marker == "" {
		t.Fatal("the block painted no remainder marker to check")
	}

	plain := ansi.Strip(marker)
	if want := th.toolMarker.Render(plain); marker != want {
		t.Errorf("marker line = %q; want the marker role's paint %q", marker, want)
	}
	if asABodyLine := th.toolDetail.Render(plain); marker == asABodyLine {
		t.Errorf("marker line %q is painted exactly as a body line, so the two cannot be told apart", marker)
	}
}

// TestPromptBlockIsOneClickSurface pins the prompt's half of the target rule (D8): a block with two
// shapes to move between is a click surface WHOLE — every row it paints, the marker row and the
// see-less row among them — and a block with one shape is no target on any row. Each
// case renders a transcript holding that block alone, so "every row of the block" and "every
// rendered line" are the same set and a row that quietly changed its mind fails here.
func TestPromptBlockIsOneClickSurface(t *testing.T) {
	const width = 40
	const huge = "alpha\nbravo\ncharlie\ndelta" // four wrapped rows: one past promptCollapsedRows
	cases := []struct {
		name  string
		build func(t *testing.T, tr *transcript)
		want  targetKind
	}{
		{
			name:  "an over-threshold prompt marks every row it paints",
			build: func(_ *testing.T, tr *transcript) { tr.addUser(huge, nil) },
			want:  targetHeader,
		},
		{
			// State-independent, for the tool block's reason: this is the click that closes it again.
			name: "an expanded prompt keeps its marks, see-less row included",
			build: func(t *testing.T, tr *transcript) {
				tr.addUser(huge, nil)
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
				}
			},
			want: targetHeader,
		},
		{
			name:  "an interjection is a click surface by the same rule",
			build: func(_ *testing.T, tr *transcript) { tr.addInterjected(huge, nil) },
			want:  targetHeader,
		},
		{
			name:  "an under-threshold prompt is no target at all",
			build: func(_ *testing.T, tr *transcript) { tr.addUser("alpha\nbravo\ncharlie", nil) },
			want:  targetNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(t, tr)

			rendered := tr.renderView(newTheme(), width, false)
			if len(rendered.targets) != len(rendered.lines) {
				t.Fatalf("targets and lines out of lockstep: %d targets for %d lines",
					len(rendered.targets), len(rendered.lines))
			}
			if len(rendered.lines) == 0 {
				t.Fatal("the block painted nothing at all")
			}
			for i, target := range rendered.targets {
				if target.kind != tc.want {
					t.Errorf("row %d (%q) is marked %v; want %v", i, strip(rendered.lines[i]), target.kind, tc.want)
				}
				if tc.want != targetNone && target.entry != 0 {
					t.Errorf("row %d names entry %d; want the block's own head entry 0", i, target.entry)
				}
			}
		})
	}
}

// TestBlockMarksAgreeWithTheMouseMapping walks the seam the toggle uses: the row a mark is PAINTED
// on is the row the mouse resolves to that mark's content line, and the entry it names is the one
// whose state a click there flips. One accounting, so a click can never toggle a block other than
// the one under the cursor — the map's whole reason for being built by the painter.
//
// A grouped block is the case where "the one under the cursor" stops being the block: its members
// each own a state, so the marks have to name the MEMBER's entry rather than the run's head, and a
// mapping that quietly fell back to the head would open the wrong call.
func TestBlockMarksAgreeWithTheMouseMapping(t *testing.T) {
	// lockstep is the map's standing invariant, asserted before any index into it is used.
	lockstep := func(t *testing.T, m Model) {
		t.Helper()
		if len(m.lineTargets) != len(m.lines) {
			t.Fatalf("stashed targets and lines out of lockstep: %d targets for %d lines",
				len(m.lineTargets), len(m.lines))
		}
	}
	// resolves asserts that the mouse maps the row line is painted on back to line itself.
	resolves := func(t *testing.T, m Model, line int) {
		t.Helper()
		got, _, ok := m.pointTranscriptRow(2, screenRow(t, m, line))
		if !ok {
			t.Fatalf("the mouse maps nothing to the row line %d is painted on", line)
		}
		if got != line {
			t.Errorf("a click on line %d's row resolved to content line %d", line, got)
		}
	}

	t.Run("a single block's header and marker", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset() // drop the seeded start-up box: the block under test opens at line 0
		m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
		m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
		m.refreshViewport()
		lockstep(t, m)

		for _, want := range []targetKind{targetHeader, targetMarker} {
			marked := -1
			for i, target := range m.lineTargets {
				if target.kind == want {
					marked = i
					break
				}
			}
			if marked < 0 {
				t.Fatalf("no line marked %v in the stashed map", want)
			}
			resolves(t, m, marked)
			if entry := m.lineTargets[marked].entry; m.transcript.entries[entry].kind != entryToolCall {
				t.Errorf("line %d is marked %v but names entry %d, a %v", marked, want, entry,
					m.transcript.entries[entry].kind)
			}
		}
	})

	t.Run("a group's member rows name their own calls", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset()
		for i, c := range [][2]string{
			{"go build ./...", "ok\nbuilt"},
			{"go vet ./...", "clean\nno findings"},
			{"go test ./...", "ok\nPASS"},
		} {
			id := "c" + strconv.Itoa(i+1)
			m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "terminal",
				Arguments: []byte(`{"command":"` + c[0] + `"}`)}})
			m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: c[1]}})
		}
		m.refreshViewport()
		lockstep(t, m)

		// One header and one row per member: the marks are the three member rows, in order, each
		// naming its own entry — and the entry the mouse's own lookup lands on is that same one.
		var marked []int
		for i, target := range m.lineTargets {
			if target.kind != targetNone {
				marked = append(marked, i)
			}
		}
		if len(marked) != 3 {
			t.Fatalf("group marked %d rows, want one per member:\n%s", len(marked), strings.Join(m.lines, "\n"))
		}
		for member, line := range marked {
			resolves(t, m, line)
			if got := m.lineTargets[line].entry; got != member {
				t.Errorf("member %d's row (line %d) names entry %d, not its own call", member, line, got)
			}
		}
	})
}

// ----------------------------------------------------------------------------
// The live star: which blocks blink their header glyph (layout.md, "The live star")
// ----------------------------------------------------------------------------

// headerStar renders tr at one blink phase and returns its first rendered line with the styling
// stripped — the block header the star leads. The phase is the renderer's parameter rather than
// anything the transcript holds, so a test names it outright instead of driving a clock.
func headerStar(t *testing.T, tr *transcript, blink bool) string {
	t.Helper()
	lines := tr.renderView(newTheme(), 80, blink).lines
	if len(lines) == 0 {
		t.Fatal("the transcript rendered nothing at all")
	}
	return strings.TrimRight(ansiPattern.ReplaceAllString(lines[0], ""), " ")
}

// TestLiveBlockHeaderStarBlinks is the rule in one table: a block still holding an open call paints
// ✦ or a bare cell by the frame's blink phase, and a block with everything it was waiting for paints
// ✦ at BOTH phases — the phase alone never moves a settled star. Each case asserts the header at
// both phases, so a block that blinked when it should not have fails here just as loudly as one that
// did not blink when it should. The blinked-out phase keeps the star's column, so its expectation is
// the header led by two leading spaces rather than one glyph short.
func TestLiveBlockHeaderStarBlinks(t *testing.T) {
	openRead := func(tr *transcript, id, path string, depth int) {
		tr.apply(domain.ToolCallEvent{
			EventBase: domain.EventBase{Depth: depth},
			Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
		})
	}
	openRun := func(tr *transcript, id, command string) {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: id, Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
	}
	cases := []struct {
		name             string
		build            func(t *testing.T, tr *transcript)
		settled, flipped string
	}{
		{
			name:    "a call still awaiting its result blinks",
			build:   func(_ *testing.T, tr *transcript) { openRun(tr, "c1", "go test ./...") },
			settled: "✦ Run", flipped: "  Run",
		},
		{
			name: "a landed result settles the star",
			build: func(_ *testing.T, tr *transcript) {
				openRun(tr, "c1", "go test ./...")
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "PASS"}})
			},
			settled: "✦ Run", flipped: "✦ Run",
		},
		{
			// The state is none of the star's business: expanding a block shows more of it, it does
			// not make the work behind it land.
			name: "an expanded live block blinks like any other",
			build: func(t *testing.T, tr *transcript) {
				openRun(tr, "c1", "go test ./...")
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the in-flight call expanded")
				}
			},
			settled: "✦ Run", flipped: "  Run",
		},
		{
			// A group has ONE header for many calls, so its star answers for all of them: a batch
			// whose first read landed and whose second has not is still working.
			name: "a group blinks while any of its calls is open",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				openRead(tr, "c2", "util.go", 0)
			},
			settled: "✦ Read File (2)", flipped: "  Read File (2)",
		},
		{
			name: "a group whose calls have all landed settles",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			settled: "✦ Read File (2)", flipped: "✦ Read File (2)",
		},
		{
			// A run is live until its REPORT lands, whatever the span has already finished.
			name: "a sub-agent run blinks while its report is out",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
			},
			settled: "✦ Sub-Agent ▶", flipped: "  Sub-Agent ▶",
		},
		{
			// The mirror case, and the reason the rule asks the span as well as the head: the report
			// landed over a call that never got its result, so work is still standing behind the
			// star — and behind a COLLAPSED run nothing else on screen says so.
			name: "a reported run whose span still holds an open call keeps blinking",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				openRead(tr, "c1", "a.go", 1)
				subAgentReport(tr, "s1", "survey complete", 0)
			},
			settled: "✦ Sub-Agent ▶", flipped: "  Sub-Agent ▶",
		},
		{
			name: "a finished run settles",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
			},
			settled: "✦ Sub-Agent ▶", flipped: "✦ Sub-Agent ▶",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(t, tr)

			if got := headerStar(t, tr, false); got != tc.settled {
				t.Errorf("header at the settled phase = %q, want %q", got, tc.settled)
			}
			if got := headerStar(t, tr, true); got != tc.flipped {
				t.Errorf("header at the flipped phase = %q, want %q", got, tc.flipped)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The firing block: the tool block's shape under a static ⟳ (layout.md, "The firing block")
// ----------------------------------------------------------------------------

// firingBlock folds one Schedule's whole Firing into a fresh transcript — announced by its
// EventFired, closed by the Event that ends the run — so what these tests paint is the block the
// surface's own fold builds (schedule.go) rather than a hand-dressed view.
func firingBlock(answer string) *transcript {
	tr := &transcript{}
	tr.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Prompt: "check the log",
	})
	tr.enrichFiring(schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Elapsed: 4 * time.Second,
		Outcome: schedule.Outcome{
			RecordID: "s1", Title: "nightly tidy — 14:05", FinalText: answer, Turns: 2,
		},
	})
	return tr
}

// The two states a Firing's reader cares about, in the shape layout.md gives them: collapsed, the
// block is its header, its branch and the `+N more lines` remainder counting everything beneath —
// what rode the BRANCH still shows, which is the whole point of following the outcome's two-halves
// grammar — and expanded, the block shows the answer whole with the prompt, the stats and the record
// pointer beneath it. It is one transcript toggled rather than two fixtures, because that is the
// claim: nothing about the entry changes but the flag the painter reads.
func TestFiringBlockCollapsesToItsRemainderMarker(t *testing.T) {
	cases := []struct {
		name                        string
		answer                      string
		wantCollapsed, wantExpanded []string
	}{
		{
			name:   "a multi-line answer leads the body",
			answer: "found 3 stale entries\nremoved them",
			wantCollapsed: []string{
				"⟳ Schedule ▶",
				"  ┕ nightly tidy",
				"    +5 more lines",
			},
			wantExpanded: []string{
				"⟳ Schedule ▼",
				"  ┕ nightly tidy",
				"    found 3 stale entries",
				"    removed them",
				"    prompt: check the log",
				"    2 turns · 4s",
				`    saved as "nightly tidy — 14:05" — find it in /sessions`,
			},
		},
		{
			name:   "a one-line answer rides the branch beside the Schedule's name",
			answer: "the log is clean",
			wantCollapsed: []string{
				"⟳ Schedule ▶",
				"  ┕ nightly tidy the log is clean",
				"    +3 more lines",
			},
			wantExpanded: []string{
				"⟳ Schedule ▼",
				"  ┕ nightly tidy the log is clean",
				"    prompt: check the log",
				"    2 turns · 4s",
				`    saved as "nightly tidy — 14:05" — find it in /sessions`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := firingBlock(tc.answer)

			if got, want := renderPlain(tr, 80), strings.Join(tc.wantCollapsed, "\n"); got != want {
				t.Errorf("default paint mismatch (collapsed is the default):\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the firing block toggled")
			}
			if got, want := renderPlain(tr, 80), strings.Join(tc.wantExpanded, "\n"); got != want {
				t.Errorf("expanded paint mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// The ⟳ is STATIC (layout.md, "The firing block"): the spinner belongs to the worker driving this
// session's Exchange and the session is idle while a Firing runs, so the header paints the same at
// both blink phases — most of all while the Firing is still going, which is the one frame a star
// would have blinked in.
func TestFiringBlockHeaderNeverBlinks(t *testing.T) {
	open := &transcript{}
	open.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy", Prompt: "check the log",
	})
	for _, tc := range []struct {
		name string
		tr   *transcript
		want string
	}{
		// The state indicator is orthogonal to the star and follows the ordinary toggle-target
		// rule: a collapsed block paints no body line at all, so the running Firing's one-line
		// prompt is as much hidden as the returned one's whole record and both wear the ▶ a click
		// acts on.
		{"a Firing still running", open, "⟳ Schedule ▶"},
		{"a Firing that returned", firingBlock("the log is clean"), "⟳ Schedule ▶"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := headerStar(t, tc.tr, false); got != tc.want {
				t.Errorf("header at the settled phase = %q, want %q", got, tc.want)
			}
			if got := headerStar(t, tc.tr, true); got != tc.want {
				t.Errorf("header at the flipped phase = %q, want %q", got, tc.want)
			}
		})
	}
}

// The block borrows the tool block's SHAPE and none of its meaning (ADR 0033), so neither derivation
// that folds entries into a bigger block may admit it: a Firing between two reads breaks their group
// instead of joining it, and no sub-agent span opens behind one. Both are pinned with a block
// DRESSED as exactly what each rule looks for — the reads' own label in the groupable shape, the
// sub-agent tool name over deeper entries — so a rule that stopped checking the entry kind fails
// here rather than quietly regrouping the transcript.
func TestFiringBlockJoinsNoToolGrouping(t *testing.T) {
	fired := schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy", Prompt: "check the log",
	}

	t.Run("it breaks a run of same-label calls", func(t *testing.T) {
		tr := &transcript{}
		readCall(tr, "c1", "main.go", 1, 154, 0)
		tr.addFiring(fired)
		readCall(tr, "c2", "util.go", 1, 42, 0)
		tr.entries[1].tool.Label = tr.entries[0].tool.Label
		tr.entries[1].tool.Details = toolBody{}

		if run := toolCallRun(tr.entries, 0); len(run) != 1 {
			t.Errorf("toolCallRun over the first read = %d views, want 1 — a Firing breaks the run", len(run))
		}
		if run := toolCallRun(tr.entries, 1); run != nil {
			t.Errorf("toolCallRun at the firing block = %v, want nil — it heads no group of its own", run)
		}
	})

	t.Run("it opens no sub-agent span", func(t *testing.T) {
		tr := &transcript{}
		tr.addFiring(fired)
		tr.entries[0].tool.name = subAgentToolName
		readCall(tr, "c1", "a.go", 1, 5, 1)

		if got := subAgentSpan(tr.entries, 0); got != 0 {
			t.Errorf("subAgentSpan at the firing block = %d, want 0 — no run hides behind a Firing", got)
		}
	})
}

// A command whose output is a single line puts that line where every other one-line outcome goes:
// on the branch, beside the command. Nothing hangs beneath — a one-line result is a summary, not a
// body, and only a command with more to say than one line reshapes into the Run block above.
func TestRenderOneLineOutputRidesTheBranch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234\n"}})

	want := strings.Join([]string{
		"✦ Run",
		"  ┕ git rev-parse HEAD abc1234",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("one-line Run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// …and because a one-line result leaves the branch line free of a body, consecutive one-line
// commands still fold into one block with their outputs aligned past the widest command — the
// grouping a body would (correctly) break.
func TestRenderGroupsOneLineOutputCalls(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"pwd"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "/workspace/repos/apogee"}})

	want := strings.Join([]string{
		"✦ Run (2)",
		"  ┝ git rev-parse HEAD abc1234",
		"  ┕ pwd                /workspace/repos/apogee",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("one-line Run group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A call whose result has not landed shows the bare target on its branch and nothing after it —
// the same line it will keep once the detail arrives beside it.
func TestRenderInFlightStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}})

	want := strings.Join([]string{
		"✦ Read File",
		"  ┕ main.go",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The one shape with no target line: an unregistered tool has nothing to lead a branch with, so
// the header stands alone and its LABELLED arguments — one `name:` line with the value's own lines
// beneath it, the same rendering the approval prompt shows — are themselves the ┝/┕ branches.
// Collapsed, that branch list is capped like any other block's body and the remainder
// marker counts what is behind it; expanded, every line the model sent is back — the approval
// popup is where a human approves an action, the transcript block is the record (layout.md,
// "Collapsed and expanded blocks").
func TestRenderNoTargetStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1,"b":2}`)}})

	want := strings.Join([]string{
		"✦ mcp_thing ▶",
		"  ┝ a:",
		"  ┕   1",
		"    +2 more lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("collapsed targetless block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the targetless block to expand")
	}
	want = strings.Join([]string{
		"✦ mcp_thing ▼",
		"  ┝ a:",
		"  ┝   1",
		"  ┝ b:",
		"  ┕   2",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("expanded targetless block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A targetless call has no branch line for its summary to ride, so the outcome closes the branch
// list instead of vanishing: an unregistered tool's arguments, then the "error: …" it earned. The
// summary is part of that list, so the collapsed cap counts it like any other branch line.
func TestRenderNoTargetKeepsItsSummary(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "no such server", IsError: true}})
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the targetless block to expand")
	}

	want := strings.Join([]string{
		"✦ mcp_thing ▼",
		"  ┝ a:",
		"  ┝   1",
		"  ┕ error: no such server",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("targetless error block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestTargetlessBlocksCollapseToTheBudget pins the reversal of layout.md's old never-hide rule
// across all three targetless shapes: the unregistered/MCP argument dump, a registered call whose
// target argument never arrived, and a stray result. Each collapses to the house budget with a
// remainder marker and a ▶ header — two branch rows and the marker, whatever it is hiding, which is
// the whole of the ask — and each expands to every line it retained. The 60-line blob is the case
// the old rule made 61 permanent rows.
func TestTargetlessBlocksCollapseToTheBudget(t *testing.T) {
	blob := func(lines int) []byte {
		items := make([]string, lines)
		for i := range items {
			items[i] = strconv.Quote("arg" + strconv.Itoa(i))
		}
		return []byte("[" + strings.Join(items, ",") + "]")
	}
	cases := []struct {
		name          string
		build         func(tr *transcript)
		wantCollapsed []string
		wantExpanded  int // physical lines, header included
	}{
		{
			name: "an unregistered tool's 60-line argument blob",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "mcp_search", Arguments: blob(58)}})
			},
			wantCollapsed: []string{"✦ mcp_search ▶", "  ┝ [", `  ┕   "arg0",`, "    +58 more lines"},
			wantExpanded:  61,
		},
		{
			name: "a registered call whose target argument is missing",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "terminal", Arguments: []byte(`{"cmd":"go test"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
					CallID: "c1", Content: "one\ntwo\nthree\nfour"}})
			},
			wantCollapsed: []string{"✦ Run ▶", "  ┝ one", "  ┕ two", "    +2 more lines"},
			wantExpanded:  5,
		},
		{
			name: "a stray result that matched no call",
			build: func(tr *transcript) {
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
					CallID: "gone", Content: "one\ntwo\nthree"}})
			},
			wantCollapsed: []string{"✦ result ▶", "  ┝ one", "  ┕ two", "    +1 more line"},
			wantExpanded:  4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			want := strings.Join(tc.wantCollapsed, "\n")
			if got := renderPlain(tr, 80); got != want {
				t.Errorf("collapsed block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the targetless block to own a block state")
			}
			got := strings.Split(renderPlain(tr, 80), "\n")
			if len(got) != tc.wantExpanded {
				t.Errorf("expanded block is %d lines, want %d:\n%s", len(got), tc.wantExpanded, strings.Join(got, "\n"))
			}
			if !strings.HasSuffix(got[0], " "+glyphExpanded) {
				t.Errorf("expanded header = %q, want it to wear %q", got[0], glyphExpanded)
			}
			for _, ln := range got {
				if strings.Contains(ln, "more line") {
					t.Errorf("an expanded block kept a remainder marker: %q", ln)
				}
			}
		})
	}
}

// TestEveryToolShapeCollapsesInsideTheRowBudget is the UNIFORM cap read across every shape that
// wears the tool block, at a width narrow enough that each one's content soft-wraps if nothing stops
// it: a targeted call with a long target and a long body, a targetless argument blob, a stray
// result, a scheduled Firing and a collapsed sub-agent run. Collapsed, none of them may stand taller
// than its header plus three content rows or wider than the column it was painted in — that is the
// whole of the budget, and the point of it is that a reader can predict a block's height without
// knowing which tool filled it (docs/layout/tool-layout.md).
//
// It asserts the SHAPE rather than the text, which the per-shape tests above pin line by line: what
// would regress here is a path that still soft-wraps unbounded, and that shows as a row count.
func TestEveryToolShapeCollapsesInsideTheRowBudget(t *testing.T) {
	const width = 60
	long := strings.Repeat("go test ./internal/tui/ -run TestSomethingLong ", 9)
	body := "line one is itself long enough to wrap at sixty columns without help\ntwo\nthree\nfour"
	cases := []struct {
		name  string
		build func() *transcript
	}{
		{
			name: "a targeted call with a long target and a long body",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":` + strconv.Quote(long) + `}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: body}})
				return tr
			},
		},
		{
			name: "a targetless call whose one verbatim argument line overflows the row",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "mcp_search", Arguments: []byte(strconv.Quote(long))}})
				return tr
			},
		},
		{
			name: "a targetless argument list past both the cap and the row",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "mcp_search", Arguments: []byte(
						`{"query":` + strconv.Quote(long) + `,"server":"docs","limit":20}`)}})
				return tr
			},
		},
		{
			name: "a stray result that matched no call",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "gone", Content: long + "\n" + body}})
				return tr
			},
		},
		{
			name:  "a scheduled Firing",
			build: func() *transcript { return firingBlock(long + "\n" + body) },
		},
		{
			name: "a collapsed sub-agent run",
			build: func() *transcript {
				tr := &transcript{}
				subAgentCall(tr, "s1", long, 0)
				runCall(tr, "c1", "go build ./...", body, 1)
				subAgentReport(tr, "s1", long+"\n"+body, 0)
				return tr
			},
		},
	}
	th := newTheme()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(renderPlain(tc.build(), width), "\n")
			if len(lines) > 1+collapsedTargetRows+1 {
				t.Errorf("collapsed block is %d rows, want at most %d:\n%s",
					len(lines), 1+collapsedTargetRows+1, strings.Join(lines, "\n"))
			}
			for i, ln := range lines {
				if w := th.measure.Width(ln); w > width {
					t.Errorf("row %d measures %d cells, want at most %d: %q", i, w, width, ln)
				}
			}
			// A block that hides this much says so, whatever its shape: the indicator and the click
			// target are one predicate, so a missing ▶ here is an unreachable second state.
			if !strings.HasSuffix(lines[0], " "+glyphCollapsed) {
				t.Errorf("collapsed header = %q, want it to wear %q", lines[0], glyphCollapsed)
			}
		})
	}
}

// A call the presenter does not recognise paints its arguments the way the approval prompt does:
// one `name:` line per argument with the value's own real lines beneath it — no brace envelope
// around the set, no quoted key names, and a multi-line value showing the lines it will actually
// run rather than one `"…\n…"` blob. The labelling changes what a body SAYS and nothing about how
// a block behaves: it still collapses to the house budget behind a remainder marker and still
// gives every retained line back on toggle.
func TestUnregisteredCallLabelsItsArguments(t *testing.T) {
	cases := []struct {
		name          string
		args          string
		wantCollapsed []string
		wantExpanded  []string
	}{
		{
			name: "a multi-key argument object",
			args: `{"query":"collapse","server":"docs","limit":20}`,
			wantCollapsed: []string{
				"✦ mcp_search ▶",
				"  ┝ query:",
				"  ┕   collapse",
				"    +4 more lines",
			},
			wantExpanded: []string{
				"✦ mcp_search ▼",
				"  ┝ query:",
				"  ┝   collapse",
				"  ┝ server:",
				"  ┝   docs",
				"  ┝ limit:",
				"  ┕   20",
			},
		},
		{
			name: "a multi-line value keeps its own lines",
			args: `{"script":"cd /ws\ngit status\ngit diff"}`,
			wantCollapsed: []string{
				"✦ mcp_search ▶",
				"  ┝ script:",
				"  ┕   cd /ws",
				"    +2 more lines",
			},
			wantExpanded: []string{
				"✦ mcp_search ▼",
				"  ┝ script:",
				"  ┝   cd /ws",
				"  ┝   git status",
				"  ┕   git diff",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
				ID: "c1", Tool: "mcp_search", Arguments: []byte(tc.args)}})

			collapsed := renderPlain(tr, 80)
			if want := strings.Join(tc.wantCollapsed, "\n"); collapsed != want {
				t.Errorf("collapsed block mismatch:\n--- got ---\n%s\n--- want ---\n%s", collapsed, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the unregistered call to own a block state")
			}
			expanded := renderPlain(tr, 80)
			if want := strings.Join(tc.wantExpanded, "\n"); expanded != want {
				t.Errorf("expanded block mismatch:\n--- got ---\n%s\n--- want ---\n%s", expanded, want)
			}
			// The JSON envelope is what the labelling replaces, so neither state may carry a
			// brace of its own or a key still wearing its wire quotes.
			for _, state := range []string{collapsed, expanded} {
				for _, banned := range []string{"{", "}", `"query"`, `"server"`, `"limit"`, `"script"`} {
					if strings.Contains(state, banned) {
						t.Errorf("painted block still carries %q:\n%s", banned, state)
					}
				}
			}
		})
	}
}

// Anything between two same-label calls ends the run, and so does a call with no target to lead an
// aligned member row. A BODY is no longer among the breakers — that is the flip this test carries,
// asserted after the table — so the case that used to stand for it stands for what it actually
// breaks on now: the label. Each case pins the whole scrollback, so a break shows as the separate
// blocks it must produce.
func TestRenderGroupBreakers(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			name: "a differently-labelled call between two reads",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "ok\nPASS\ndone"}})
				readCall(tr, "c3", "b.go", 1, 9, 0)
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"✦ Run ▶",
				"  ┕ go test",
				"    +3 more lines",
				"",
				"✦ Read File",
				"  ┕ b.go 1 - 9",
			},
		},
		{
			name: "an approval note between two reads",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "read_file"}, Decision: domain.ApprovalAllow})
				readCall(tr, "c2", "b.go", 1, 9, 0)
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"· approval allow: read_file",
				"",
				"✦ Read File",
				"  ┕ b.go 1 - 9",
			},
		},
		{
			name: "a deeper sub-agent call",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				readCall(tr, "c2", "b.go", 1, 9, 1)
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"", // the descent's own spacer joins at depth 0: the rail starts at the label
				"│ ⤷ sub-agent",
				"│",
				"│ ✦ Read File",
				"│   ┕ b.go 1 - 9",
			},
		},
		{
			name: "a call with no target",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file"}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2",
					Content: "[File: ?, 1 lines total, showing lines 1-1]",
					Summary: domain.ReadSpan{Start: 1, End: 1, Total: 1}}})
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"✦ Read File",
				"  ┕ 1 - 1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)
			if got, want := renderPlain(tr, 80), strings.Join(tc.want, "\n"); got != want {
				t.Errorf("group not broken:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}

	// The flip: a call carrying output used to end a run of its own label and now joins it, giving
	// up nothing but the rows its body would have taken — which are a click away on the member
	// itself (design call 3).
	t.Run("a call with output joins the run", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go build"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "done"}})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "ok\nPASS\ndone"}})

		if run := toolCallRun(tr.entries, 0); len(run) != 2 {
			t.Fatalf("toolCallRun over the two Runs = %d views, want 2 — a body no longer breaks a run", len(run))
		}
		want := strings.Join([]string{
			"✦ Run (2)",
			"  ┝ go build done",
			groupMemberLine(t, "  ┕ go test", 80),
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("joined group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// ----------------------------------------------------------------------------
// The whole-transcript layout golden (tool-call layout item 5)
// ----------------------------------------------------------------------------

// TestTranscriptLayoutGolden pins the whole rendered scrollback of one realistic mixed session —
// a user prompt, narration the model padded with a trailing "\n\n", a batch of reads, a Run whose
// output hangs beneath its command, a diff whose "+2 -2" rides its branch over a coloured body, two
// edits showing the lines they change beneath their own reports, a write showing the lines it
// writes beneath its own byte count, an
// unregistered tool whose verbatim arguments are its own branches, an
// approval note, and a sub-agent read — as an exact line sequence, blank lines included. It is the
// backstop across the layout changes rather than a test of any one of them: the blank-line hygiene
// shows as the single separator row between every block — empty at the top level, the │ rail
// gutter inside the sub-agent run — the bracketless bold-orange label as the
// header text, the grouping as the two counted blocks — three reads aligned under "Read File (3)",
// and the two consecutive edits under "Edit File (2)", differently tooled and sharing a label, each
// held to one member row with its diff behind its own indicator now that a body no longer breaks a
// run — and the uniform shape as the fact
// that every header here — grouped, standalone, railed — is a label and nothing else, with the
// target always leading a branch and the outcome split into the summary beside it and the body
// beneath. The ▶ on every header that hides something and its absence everywhere else
// is the affordance rule in the same picture: exactly the blocks here that hide something say so,
// the targetless one among them now that it collapses like every other — and in a group it is the
// MEMBER that says it, the counted header wearing none. A regression in any of
// them changes this golden, and the golden doubles as the living example of what layout.md
// sketches.
func TestTranscriptLayoutGolden(t *testing.T) {
	tr := &transcript{}
	tr.addUser("read the docs, then run the tests", nil)
	tr.apply(domain.TokenEvent{Text: "Reading the docs first."})
	tr.apply(domain.TokenEvent{Text: "\n\n"}) // the model's own padding: trimmed at commit
	readCall(tr, "c1", "README.md", 1, 154, 0)
	readCall(tr, "c2", "TODO.md", 1, 408, 0)
	readCall(tr, "c3", "ISSUES.md", 1, 8, 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c4", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c4",
		Content: "ok   apogee/internal/tui     0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c5", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c5",
		Content: "  func main() {\n-     fmt.Println(\"old\")\n-     return\n+     fmt.Println(\"new\")\n+     os.Exit(0)\n  }",
		Summary: domain.DiffStat{Added: 2, Removed: 2},
	}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c6", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"main.go","oldText":"fmt.Println(\"old\")","newText":"fmt.Println(\"new\")"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c6", Content: "replaced text in main.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c7", Tool: "multi_find_and_replace",
		Arguments: []byte(`{"path":"main.go","replacements":[{"oldText":"return","newText":"os.Exit(0)"}]}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c7", Content: "applied 1 replacement to main.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c8", Tool: "write_file",
		Arguments: []byte(`{"path":"notes.md","content":"# Notes\n\nrewrote main.go\n"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c8",
		Content: "wrote 25 bytes to notes.md", Summary: domain.WroteBytes{Bytes: 25}}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c9", Tool: "mcp_search",
		Arguments: []byte(`{"query":"collapse","limit":20}`)}})
	tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
	readCall(tr, "c10", "main.go", 1, 154, 1)

	want := strings.Join([]string{
		"❯ read the docs, then run the tests",
		"",
		"✦ Reading the docs first.",
		"",
		"✦ Read File (3)",
		"  ┝ README.md 1 - 154",
		"  ┝ TODO.md   1 - 408",
		"  ┕ ISSUES.md 1 - 8",
		"",
		"✦ Run ▶",
		"  ┕ go test ./...",
		"    +3 more lines",
		"",
		"✦ View Diff ▶",
		"  ┕ main.go +2 -2",
		"    +6 more lines",
		"",
		"✦ Edit File (2)",
		groupMemberLine(t, "  ┝ main.go replaced text in main.go", 80),
		groupMemberLine(t, "  ┕ main.go applied 1 replacement to main.go", 80),
		"",
		"✦ Write File ▶",
		"  ┕ notes.md +25 bytes",
		"    +3 more lines",
		"",
		"✦ mcp_search ▶",
		"  ┝ query:",
		"  ┕   collapse",
		"    +2 more lines",
		"",
		"· approval allow: terminal",
		"",
		"│ ⤷ sub-agent",
		"│",
		"│ ✦ Read File",
		"│   ┕ main.go 1 - 154",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("transcript layout mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ----------------------------------------------------------------------------
// inputContentRows sizes the prompt box to what the textarea actually draws
// ----------------------------------------------------------------------------

// TestInputContentRows pins the box-sizing count against the textarea's own wrap, including the
// edge that used to under-count: a logical line whose final wrapped segment exactly fills the
// width takes one extra visual row (the widget reserves a trailing row for the caret past a full
// line). Under-counting it left the box a row short at the wrap boundary, stranding the scroll the
// layout re-seat then could not clamp (ISSUES #2).
func TestInputContentRows(t *testing.T) {
	const w = 10
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"empty is one row", "", 1},
		{"short line", "abc", 1},
		{"one under the width", strings.Repeat("a", 9), 1},
		{"exact width gains a trailing row", strings.Repeat("a", 10), 2},
		{"one over the width", strings.Repeat("a", 11), 2},
		{"two full widths", strings.Repeat("a", 20), 3},
		{"two full widths plus one", strings.Repeat("a", 21), 3},
		{"trailing newline adds a row", "abc\n", 2},
		{"two logical lines", "abc\ndef", 2},
		{"each full logical line gets its trailing row", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 4},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 2}, // 5×2 = 10 cells = exact width
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inputContentRows(c.value, w); got != c.want {
				t.Errorf("inputContentRows(%q, %d) = %d, want %d", c.value, w, got, c.want)
			}
		})
	}
}

// A zero or negative width floors to one column rather than dividing by zero, and still returns at
// least one row.
func TestInputContentRowsZeroWidth(t *testing.T) {
	if got := inputContentRows("ab", 0); got < 1 {
		t.Errorf("inputContentRows with zero width = %d, want >= 1 (width floored to one)", got)
	}
}

// widgetContentRows is the row count a REAL textarea draws for value at width, and the effective
// text width it settled on. It is the oracle inputContentRows mirrors, read straight off the widget
// rather than re-derived: DynamicHeight makes the textarea publish its own totalVisualLines as its
// height (bubbles/v2@v2.1.0/textarea/textarea.go:1666-1692), and MaxHeight is cleared so nothing
// clamps that answer. It is the whole-value counterpart of the per-line LineInfo.Height oracle
// TestWrapRowStartsMirrorsTheWidget pins wrapRowStarts to — the same widget, asked the same
// question the box-sizing path asks.
func widgetContentRows(t *testing.T, value string, width int) (rows, effWidth int) {
	t.Helper()
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0 // uncapped: the height it reports is then its raw visual-line total
	ta.MinHeight = 1
	ta.DynamicHeight = true
	ta.SetWidth(width)
	ta.SetValue(value)
	return ta.Height(), ta.Width()
}

// TestInputContentRowsMirrorsTheWidget pins the box-sizing count to the widget itself, which the
// old Wordwrap+Hardwrap approximation was not: it under-counted "hello world" at width 5 (four
// widget rows, it said three) and "a b  c" at width 3 (three, it said two), and over-counted
// "a-b-c-d" at width 3 (three, it said four). The count now delegates to wrapRowStarts, so the box's
// height and the rows the accent pass paints on come off one ruler.
//
// Tabs are deliberately absent: the widget's sanitizer expands them and neither mirror does, the one
// divergence left standing (TODO.md, "The TUI width authority — what it did not convert").
func TestInputContentRowsMirrorsTheWidget(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
	}{
		// The three the follow-up finding named, each a concrete failure of the old count.
		{"word-wrapped prose", "hello world", 5},
		{"a double space between words", "a b  c", 3},
		{"a hyphen run", "a-b-c-d", 3},

		{"empty", "", 10},
		{"short line", "abc", 10},
		{"one under the width", strings.Repeat("a", 9), 10},
		{"exact width", strings.Repeat("a", 10), 10},
		{"one over the width", strings.Repeat("a", 11), 10},
		{"two full widths", strings.Repeat("a", 20), 10},
		{"trailing newline", "abc\n", 10},
		{"two logical lines", "abc\ndef", 10},
		{"two full logical lines", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 10},
		{"a blank line between two", "abc\n\ndef", 10},
		{"a line of nothing but spaces", "     ", 3},
		{"trailing space at a row boundary", "aaa aaa aaa aaax ", 8},
		{"a word longer than the row", "averyveryverylongwordindeed", 6},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 10},
		{"wide runes wrapping mid-word", "日本語のテキスト 絵文字", 7},
		{"an emoji carrying VS16", "warn ⚠️ here", 7},
		{"a VS16 run filling the row", "⚠️⚠️⚠️ end", 6},
		{"VS16 inside a word too wide for the row", "aa⚠️bb⚠️cc", 4},
		{"a realistic draft", "/grill-me check @internal/tui/model.go and /code-adit", 20},
		{"a multi-line draft", "fix the wrap bug\n\nsee @internal/tui/render.go — the mirror under-counts", 24},
		{"one column", "ab cd", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want, w := widgetContentRows(t, c.value, c.width)
			if got := inputContentRows(c.value, w); got != want {
				t.Errorf("inputContentRows(%q, %d) = %d, the widget draws %d rows", c.value, w, got, want)
			}
		})
	}
}

// The same oracle over a spread of generated prompt-shaped drafts, which is what turns "the three
// named cases agree" into "the mirror is faithful": the old count differed from the widget on
// roughly 41% of inputs like these, so any regression to an approximation fails here loudly rather
// than on one lucky fixture. Deterministic — a fixed seed, so a failure is reproducible.
func TestInputContentRowsMirrorsTheWidgetOnGeneratedDrafts(t *testing.T) {
	// The alphabet is chosen for the boundaries the two mirrors disagreed at: spaces (the widget's
	// word/space grouping), hyphens (a breakpoint to ansi.Wordwrap but not to the widget), a wide
	// rune and a VS16 cluster (grapheme-vs-rune measurement), and newlines (logical lines).
	glyphs := []string{"a", "b", "c", " ", " ", "-", "@", "/", "あ", "⚠️", "\n"}
	rng := rand.New(rand.NewSource(20260731))
	for _, width := range []int{1, 2, 3, 5, 8, 13, 40} {
		for i := 0; i < 300; i++ {
			var sb strings.Builder
			for n := rng.Intn(24); n > 0; n-- {
				sb.WriteString(glyphs[rng.Intn(len(glyphs))])
			}
			value := sb.String()
			want, w := widgetContentRows(t, value, width)
			if got := inputContentRows(value, w); got != want {
				t.Fatalf("inputContentRows(%q, %d) = %d, the widget draws %d rows", value, w, got, want)
			}
		}
	}
}

// The clamp above the count still holds now that it can report MORE rows than the old
// approximation: the box never grows past maxInputRows (past it the widget scrolls internally) and
// never shrinks below minInputRows, whatever the mirror returns. inputContentRows itself stays
// unclamped — layout() derives the viewport's height from the clamped box height, not from this.
func TestPromptEditorRowsClampsTheWidgetCount(t *testing.T) {
	const width = 8
	cases := []string{
		"",
		"short",
		strings.Repeat("a", width*maxInputRows*2),   // one very long soft-wrapped line
		strings.Repeat("word ", width*maxInputRows), // many wrapped words
		strings.Repeat("line\n", maxInputRows*3),    // many logical lines
		strings.Repeat("⚠️", width*maxInputRows),    // VS16 clusters, the widest measure gap
		strings.Repeat("a", width) + "\n" + "b",     // an exact width fill plus a line
	}
	for _, value := range cases {
		e := newPromptEditor(defaultCursorShape)
		e.input.SetValue(value)
		raw := inputContentRows(e.input.Value(), width) // what the editor holds, not what was handed to it
		got := e.rows(width)
		if got < minInputRows || got > maxInputRows {
			t.Fatalf("rows(%q) = %d, outside [%d, %d] (unclamped count %d)", value, got, minInputRows, maxInputRows, raw)
		}
		if want := clampInt(raw, minInputRows, maxInputRows); got != want {
			t.Errorf("rows(%q) = %d, want %d (clamp of the unclamped count %d)", value, got, want, raw)
		}
	}
}

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
	th := newTheme()
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
	// (f) none of the black-background SGR the input box paints. Extract it from a real
	// Background(colBlack) render, so the check tracks whatever colour profile lipgloss uses rather
	// than a hard-coded escape.
	probe := lipgloss.NewStyle().Background(colBlack).Render("x")
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
	th := newTheme()
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
