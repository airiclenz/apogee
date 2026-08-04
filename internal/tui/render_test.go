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
		"✦ Sub-Agent",
		"  ┕ first",
		"",
		"│ ⤷ sub-agent",
		"│",
		"│ ✦ first child",
		"", // the first run closes here…
		"✦ Sub-Agent",
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

// A batch of reads folds into one block: a single ✦ Read File header, ┝ ┝ ┕ rails, and every
// target padded to the widest one so the detail column lines up — the shape layout.md sketches.
func TestRenderGroupsConsecutiveSameLabelCalls(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	readCall(tr, "c2", "TODO.md", 1, 408, 0)
	readCall(tr, "c3", "ISSUES.md", 1, 8, 0)

	want := strings.Join([]string{
		"✦ Read File",
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
		"│ ✦ Read File",
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
		"✦ Edit File",
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
		"✦ Read File",
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
		"✦ Read File",
		"  ┝ README.md 1 - 154",
		"  ┕ TODO.md   1 - 408",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("re-rendered group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A lone call renders in the very shape a group does — label-only header, target leading the
// branch — so the block does not reshape when a second call joins it. That is the whole point of
// the uniform layout, and the ┕-with-no-padding is what "a group of one pads to itself" means.
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
		"✦ Read File",
		"  ┝ main.go               1 - 154",
		"  ┕ a-much-longer-name.go 1 - 9",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grown block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A body-only call keeps the same header and branch: nothing rides beside the target, and the
// body lays out beneath it at the branch marker's width — those lines are not ┝/┕ branches of
// their own, because only calls are (layout.md's Run sketch).
func TestRenderMultiDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})

	want := strings.Join([]string{
		"✦ Run",
		"  ┕ go test ./...",
		"    ok   apogee/internal/tui   0.412s",
		"    … +2 more lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("multi-detail block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A diff call is the summary-and-body shape layout.md sketches: the diffstat rides the branch
// beside the path and the coloured body hangs beneath it. The body keeps its red/green
// colouring, which — together with having a body at all — is why it can never fold into a group.
func TestRenderDiffDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
		Content: "- a removed line\n+ an added line",
		Summary: domain.DiffStat{Added: 1, Removed: 1}}})

	want := strings.Join([]string{
		"✦ View Diff",
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
// coloured, so the branch reads like every other tool's summary.
func TestRenderDiffMatchesLayoutSketch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "- a code line that has been removed\n- a second removed line\n+ a new code line\n+ a second new line",
		Summary: domain.DiffStat{Added: 2, Removed: 2},
	}})

	want := strings.Join([]string{
		"✦ View Diff",
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
	if got, want := tr.renderLines(th, 80)[1], th.toolDetail.Render("  ┕ main.go +2 -2"); got != want {
		t.Errorf("diffstat branch = %q; want the plain toolDetail style %q", got, want)
	}
}

// A diff whose body is capped still names the whole change on its branch: the diffstat counts
// every line, the body stops at diffDetailCap with its remainder count.
func TestRenderDiffStatSurvivesTheBodyCap(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: strings.TrimSuffix(strings.Repeat("+ added\n", diffDetailCap+5), "\n"),
		Summary: domain.DiffStat{Added: diffDetailCap + 5},
	}})

	lines := strings.Split(renderPlain(tr, 80), "\n")
	if got, want := lines[1], "  ┕ main.go +25 -0"; got != want {
		t.Errorf("capped diff branch = %q, want %q (the stat spans the whole diff)", got, want)
	}
	if got, want := lines[len(lines)-1], "    … +5 more lines"; got != want {
		t.Errorf("capped diff body ends %q, want %q", got, want)
	}
}

// TestCollapsedPaintTruncatesRetainedBodies pins the relocation itself: the entry KEEPS every
// body line it was given and the collapsed paint is the only thing that truncates, synthesizing
// the "… +N more lines" remainder the outcome builders used to bake in (layout.md, "Collapsed
// and expanded blocks" — truncation is a render-time act on retained facts). Both flavours are
// asserted, because the cap is read off the body's own line kinds: a diff body paints
// diffDetailCap lines, any other multi-line body paints its first, and a body already inside its
// cap paints whole with no marker at all.
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
			name: "free-form output paints its first line and counts the rest",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
			},
			wantKept:  4,
			wantPaint: []string{"    ok   a", "    … +3 more lines"},
		},
		{
			name: "a diff body paints diffDetailCap lines and counts the rest",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: diffLines(diffDetailCap + 3), Summary: domain.DiffStat{Added: diffDetailCap + 3}}})
			},
			wantKept: diffDetailCap + 3,
			wantPaint: append(strings.Split(strings.TrimSuffix(strings.Repeat("    + added\n", diffDetailCap), "\n"), "\n"),
				"    … +3 more lines"),
		},
		{
			name: "a body inside its cap paints whole, with no marker",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: "- old line\n+ new line", Summary: domain.DiffStat{Added: 1, Removed: 1}}})
			},
			wantKept:  2,
			wantPaint: []string{"    - old line", "    + new line"},
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
			name: "free-form output expands from its first line to all of them",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
			},
			wantCollapsed: []string{"    ok   a", "    … +3 more lines"},
			wantExpanded:  []string{"    ok   a", "    ok   b", "    ok   c", "    PASS"},
		},
		{
			name: "a diff body expands past diffDetailCap",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: diffContent(diffDetailCap + 3), Summary: domain.DiffStat{Added: diffDetailCap + 3}}})
			},
			wantCollapsed: append(paintedDiff(diffDetailCap), "    … +3 more lines"),
			wantExpanded:  paintedDiff(diffDetailCap + 3),
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

// A grouped run paints the same in both states, and no special case in the painter makes that
// true: a groupable call carries no body by definition, so there is nothing for the expanded
// paint to reveal (layout.md — the group is the degenerate case of the two-state rule).
func TestExpandedGroupPaintsIdentically(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "main.go", 1, 154, 0)
	readCall(tr, "c2", "util.go", 1, 42, 0)

	collapsed := renderPlain(tr, 80)
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the group's head entry toggled")
	}

	if got := renderPlain(tr, 80); got != collapsed {
		t.Errorf("expanded group repainted:\n--- got ---\n%s\n--- want (unchanged) ---\n%s", got, collapsed)
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
			build:  func(tr *transcript) { tr.addUser("alpha\nbravo\ncharlie\ndelta", nil, nil) },
			want:   []string{"❯ alpha", "  bravo", "  charlie"},
			marker: "see more (+1 line)…",
		},
		{
			name:   "a long prompt counts every row it hides",
			build:  func(tr *transcript) { tr.addUser("a\nb\nc\nd\ne\nf\ng\nh\ni\nj", nil, nil) },
			want:   []string{"❯ a", "  b", "  c"},
			marker: "see more (+7 lines)…",
		},
		{
			name:  "exactly three rows is not over the threshold",
			build: func(tr *transcript) { tr.addUser("alpha\nbravo\ncharlie", nil, nil) },
			want:  []string{"❯ alpha", "  bravo", "  charlie"},
		},
		{
			name:  "a short prompt paints as it always has",
			build: func(tr *transcript) { tr.addUser("alpha", nil, nil) },
			want:  []string{"❯ alpha"},
		},
		{
			// width 40 less the 20-column marker, its 2-column gap and the 1-column right margin
			// leaves 17 for the row, ellipsis included — the whole of what "truncated to leave a
			// gap" means, with the margin paid for out of the content and never out of the marker.
			name:   "the third row is truncated to make room for the marker",
			build:  func(tr *transcript) { tr.addUser("alpha\nbravo\n"+long, nil, nil) },
			want:   []string{"❯ alpha", "  bravo", "  " + strings.Repeat("x", 14) + "…"},
			marker: "see more (+5 lines)…",
		},
		{
			name:   "an interjection collapses by the same rule",
			build:  func(tr *transcript) { tr.addInterjected("alpha\nbravo\ncharlie\ndelta", nil, nil) },
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
	tr.addUser("alpha\nbravo\ncharlie\ndelta\necho", nil, nil)

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
	tr.addUser("alpha\nbravo", nil, nil)

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
	tr.addUser("the quick brown fox jumps over the lazy dog and keeps on running", nil, nil)

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

// TestCollapsedPromptKeepsItsChipRow holds the chip row out of the collapse in both states: it is
// the record of what the model was actually given, so it is never counted among the collapsed rows
// and never hidden — and the see-less marker closes the whole block, chips included.
func TestCollapsedPromptKeepsItsChipRow(t *testing.T) {
	const width = 44
	tr := &transcript{}
	tr.addUser("alpha\nbravo\ncharlie\ndelta", []string{"coding-standards"}, nil)

	rows := promptRows(t, tr, width)
	if len(rows) != promptCollapsedRows+1 {
		t.Fatalf("collapsed block painted %d rows; want %d body rows plus the chip row:\n%s",
			len(rows), promptCollapsedRows, strings.Join(rows, "\n"))
	}
	splitMarker(t, rows[promptCollapsedRows-1], promptSeeMore(1), width)
	if want := glyphSkill + " coding-standards"; !strings.Contains(rows[promptCollapsedRows], want) {
		t.Errorf("the chip row is not the collapsed block's last row: %q", rows[promptCollapsedRows])
	}

	if !tr.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
	}
	rows = promptRows(t, tr, width)
	if len(rows) != 6 { // four body rows, the chip row, then the see-less row
		t.Fatalf("expanded block painted %d rows; want 6:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if want := glyphSkill + " coding-standards"; !strings.Contains(rows[4], want) {
		t.Errorf("the chip row moved: %q", rows[4])
	}
	splitMarker(t, rows[5], promptSeeLess, width)
}

// TestPromptMarkerCarriesTheHighlightStyle pins the marker's look: it is painted in the theme's own
// promptToggle role and not in the prompt body's, which is what sets the toggle off from what the
// human wrote. A loose contains against the theme's own render (the toolLabel precedent), with the
// two guards for the opposite failures — a role that paints nothing at all, and one that paints
// exactly what the body does.
func TestPromptMarkerCarriesTheHighlightStyle(t *testing.T) {
	th := newTheme()
	tr := &transcript{}
	tr.addUser("alpha\nbravo\ncharlie\ndelta", nil, nil)

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

// TestRenderMarksHeaderAndMarkerLines pins the whole target rule in one table: the painter marks a
// block's header lines and its synthesized remainder marker, each carrying the index of the entry
// a click there toggles, and it marks NOTHING when the collapsed paint hides nothing. Every case
// asserts the complete set of marks, so a line that quietly became clickable fails here.
func TestRenderMarksHeaderAndMarkerLines(t *testing.T) {
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
			// ❯ run the tests | (spacer) | ✦ Run | ┕ go test ./... | ok   a | … +3 more lines
			name:  "a truncated body marks its header and its remainder marker",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.addUser("run the tests", nil, nil)
				run(tr, "c1", "go test ./...", "ok   a\nok   b\nok   c\nPASS", 0)
			},
			want: []blockMark{
				{line: 2, kind: targetHeader, entry: 1, text: "✦ Run"},
				{line: 5, kind: targetMarker, entry: 1, text: "    … +3 more lines"},
			},
		},
		{
			// The state does not decide the target: an expanded block keeps its header marked —
			// that is the click that collapses it again — and has no marker left to mark.
			name:  "an expanded block keeps its header and loses its marker",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.addUser("run the tests", nil, nil)
				run(tr, "c1", "go test ./...", "ok   a\nok   b\nok   c\nPASS", 0)
				if !tr.toggleExpanded(1) {
					t.Fatal("toggleExpanded(1) = false; want the tool-call entry expanded")
				}
			},
			want: []blockMark{{line: 2, kind: targetHeader, entry: 1, text: "✦ Run"}},
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
			// The targetless shape hides nothing however long its body is — an unregistered tool's
			// verbatim arguments ARE its branches — so nothing about it is clickable.
			name:  "a targetless block hides nothing and marks nothing",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "weird_tool", Arguments: []byte(`{"a":1,"b":2,"c":3}`)}})
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
				{line: 1, kind: targetHeader, entry: 0, text: "  Python"},
				{line: 5, kind: targetMarker, entry: 0, text: "    … +2"},
				{line: 6, kind: targetMarker, entry: 0, text: "    more"},
				{line: 7, kind: targetMarker, entry: 0, text: "    lines"},
			},
		},
		{
			// Two blocks of the same shape: each header and marker names its OWN head entry, which
			// is the whole of what the index is for.
			name:  "each block's marks carry its own entry index",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				run(tr, "c1", "go build ./...", "a\nb\nc", 0)
				run(tr, "c2", "go vet ./...", "x\ny", 0)
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Run"},
				{line: 3, kind: targetMarker, entry: 0, text: "    … +2 more lines"},
				{line: 5, kind: targetHeader, entry: 1, text: "✦ Run"},
				{line: 8, kind: targetMarker, entry: 1, text: "    … +1 more line"},
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
			want: []blockMark{{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent"}},
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
			want: []blockMark{{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent"}},
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
				{line: 2, kind: targetHeader, entry: 0, text: "│ ✦ Run"},
				{line: 5, kind: targetMarker, entry: 0, text: "│     … +2 more lines"},
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

// TestPromptBlockIsOneClickSurface pins the prompt's half of the target rule (D8): a block with two
// shapes to move between is a click surface WHOLE — every row it paints, the marker row, the chip
// row and the see-less row among them — and a block with one shape is no target on any row. Each
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
			build: func(_ *testing.T, tr *transcript) { tr.addUser(huge, nil, nil) },
			want:  targetHeader,
		},
		{
			// The chip row is not part of what collapses, but it IS part of what a click lands on:
			// the toggle surface is the block, not the body.
			name:  "the chip row is marked with the rest of the block",
			build: func(_ *testing.T, tr *transcript) { tr.addUser(huge, []string{"coding-standards"}, nil) },
			want:  targetHeader,
		},
		{
			// State-independent, for the tool block's reason: this is the click that closes it again.
			name: "an expanded prompt keeps its marks, see-less row included",
			build: func(t *testing.T, tr *transcript) {
				tr.addUser(huge, nil, nil)
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
				}
			},
			want: targetHeader,
		},
		{
			name:  "an interjection is a click surface by the same rule",
			build: func(_ *testing.T, tr *transcript) { tr.addInterjected(huge, nil, nil) },
			want:  targetHeader,
		},
		{
			name:  "an under-threshold prompt is no target at all",
			build: func(_ *testing.T, tr *transcript) { tr.addUser("alpha\nbravo\ncharlie", nil, nil) },
			want:  targetNone,
		},
		{
			name:  "a chip row with no body hides nothing and marks nothing",
			build: func(_ *testing.T, tr *transcript) { tr.addUser("", []string{"coding-standards"}, nil) },
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

// TestBlockMarksAgreeWithTheMouseMapping walks the seam the toggle will use: the row a header is
// PAINTED on is the row the mouse resolves to that header's content line, and the mark stashed
// beside those lines names a tool-call entry. One accounting, so a click can never toggle a block
// other than the one under the cursor — the map's whole reason for being built by the painter.
func TestBlockMarksAgreeWithTheMouseMapping(t *testing.T) {
	m := newTestModel(t)
	m.transcript.reset() // drop the seeded start-up box: the block under test opens at line 0
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
	m.refreshViewport()

	if len(m.lineTargets) != len(m.lines) {
		t.Fatalf("stashed targets and lines out of lockstep: %d targets for %d lines",
			len(m.lineTargets), len(m.lines))
	}
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

		line, _, ok := m.pointTranscriptRow(2, screenRow(t, m, marked))
		if !ok {
			t.Fatalf("the mouse maps nothing to the row line %d is painted on", marked)
		}
		if line != marked {
			t.Errorf("a click on line %d's row resolved to content line %d", marked, line)
		}
		if entry := m.lineTargets[line].entry; m.transcript.entries[entry].kind != entryToolCall {
			t.Errorf("line %d is marked %v but names entry %d, a %v", line, want, entry,
				m.transcript.entries[entry].kind)
		}
	}
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
// ✦ or ✧ by the frame's blink phase, and a block with everything it was waiting for paints ✦ at
// BOTH phases — the phase alone never moves a settled star. Each case asserts the header at both
// phases, so a block that blinked when it should not have fails here just as loudly as one that did
// not blink when it should.
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
			settled: "✦ Run", flipped: "✧ Run",
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
			settled: "✦ Run", flipped: "✧ Run",
		},
		{
			// A group has ONE header for many calls, so its star answers for all of them: a batch
			// whose first read landed and whose second has not is still working.
			name: "a group blinks while any of its calls is open",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				openRead(tr, "c2", "util.go", 0)
			},
			settled: "✦ Read File", flipped: "✧ Read File",
		},
		{
			name: "a group whose calls have all landed settles",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			settled: "✦ Read File", flipped: "✦ Read File",
		},
		{
			// A run is live until its REPORT lands, whatever the span has already finished.
			name: "a sub-agent run blinks while its report is out",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
			},
			settled: "✦ Sub-Agent", flipped: "✧ Sub-Agent",
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
			settled: "✦ Sub-Agent", flipped: "✧ Sub-Agent",
		},
		{
			name: "a finished run settles",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
			},
			settled: "✦ Sub-Agent", flipped: "✦ Sub-Agent",
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
// answer's first line is visible over the `… +N more lines` remainder — whether the answer rode the
// branch or leads the body, which is the whole point of following the outcome's two-halves grammar —
// and expanded, the block shows the answer whole with the prompt, the stats and the record pointer
// beneath it. It is one transcript toggled rather than two fixtures, because that is the claim:
// nothing about the entry changes but the flag the painter reads.
func TestFiringBlockCollapsesToTheAnswersFirstLine(t *testing.T) {
	cases := []struct {
		name                        string
		answer                      string
		wantCollapsed, wantExpanded []string
	}{
		{
			name:   "a multi-line answer leads the body",
			answer: "found 3 stale entries\nremoved them",
			wantCollapsed: []string{
				"⟳ Schedule",
				"  ┕ nightly tidy",
				"    found 3 stale entries",
				"    … +4 more lines",
			},
			wantExpanded: []string{
				"⟳ Schedule",
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
				"⟳ Schedule",
				"  ┕ nightly tidy the log is clean",
				"    prompt: check the log",
				"    … +2 more lines",
			},
			wantExpanded: []string{
				"⟳ Schedule",
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
	}{
		{"a Firing still running", open},
		{"a Firing that returned", firingBlock("the log is clean")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const want = "⟳ Schedule"
			if got := headerStar(t, tc.tr, false); got != want {
				t.Errorf("header at the settled phase = %q, want %q", got, want)
			}
			if got := headerStar(t, tc.tr, true); got != want {
				t.Errorf("header at the flipped phase = %q, want %q", got, want)
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
		"✦ Run",
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
// the header stands alone and its verbatim pretty-printed arguments are themselves the ┝/┕
// branches — nothing about what the model asked for is hidden.
func TestRenderNoTargetStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1}`)}})

	want := strings.Join([]string{
		"✦ mcp_thing",
		"  ┝ {",
		`  ┝   "a": 1`,
		"  ┕ }",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("targetless block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A targetless call has no branch line for its summary to ride, so the outcome closes the branch
// list instead of vanishing: an unregistered tool's arguments, then the "error: …" it earned.
func TestRenderNoTargetKeepsItsSummary(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "no such server", IsError: true}})

	want := strings.Join([]string{
		"✦ mcp_thing",
		"  ┝ {",
		`  ┝   "a": 1`,
		"  ┝ }",
		"  ┕ error: no such server",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("targetless error block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Anything between two same-label calls ends the run, and so does a call that cannot fit one
// aligned branch line. Each case pins the whole scrollback, so a break shows as the separate
// blocks it must produce.
func TestRenderGroupBreakers(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			name: "a multi-detail call between two reads",
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
				"✦ Run",
				"  ┕ go test",
				"    ok",
				"    … +2 more lines",
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
}

// ----------------------------------------------------------------------------
// The whole-transcript layout golden (tool-call layout item 5)
// ----------------------------------------------------------------------------

// TestTranscriptLayoutGolden pins the whole rendered scrollback of one realistic mixed session —
// a user prompt, narration the model padded with a trailing "\n\n", a batch of reads, a Run whose
// output hangs beneath its command, a diff whose "+2 -2" rides its branch over a coloured body, an
// approval note, and a sub-agent read — as an exact line sequence, blank lines included. It is the
// backstop across the layout changes rather than a test of any one of them: the blank-line hygiene
// shows as the single separator row between every block — empty at the top level, the │ rail
// gutter inside the sub-agent run — the bracketless bold-orange label as the
// header text, the grouping as the one aligned Read File block, and the uniform shape as the fact
// that every header here — grouped, standalone, railed — is a label and nothing else, with the
// target always leading a branch and the outcome split into the summary beside it and the body
// beneath. A regression in any of them changes this golden, and the golden doubles as the living
// example of what layout.md sketches.
func TestTranscriptLayoutGolden(t *testing.T) {
	tr := &transcript{}
	tr.addUser("read the docs, then run the tests", nil, nil)
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
	tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
	readCall(tr, "c6", "main.go", 1, 154, 1)

	want := strings.Join([]string{
		"❯ read the docs, then run the tests",
		"",
		"✦ Reading the docs first.",
		"",
		"✦ Read File",
		"  ┝ README.md 1 - 154",
		"  ┝ TODO.md   1 - 408",
		"  ┕ ISSUES.md 1 - 8",
		"",
		"✦ Run",
		"  ┕ go test ./...",
		"    ok   apogee/internal/tui     0.412s",
		"    … +2 more lines",
		"",
		"✦ View Diff",
		"  ┕ main.go +2 -2",
		"      func main() {",
		"    -     fmt.Println(\"old\")",
		"    -     return",
		"    +     fmt.Println(\"new\")",
		"    +     os.Exit(0)",
		"      }",
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
