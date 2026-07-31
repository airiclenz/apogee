package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The width authority (width.go)
// ----------------------------------------------------------------------------
//
// The authority's contract is a mirror: it must measure the way the PAINTER measures, at every
// moment. So these tests pin three things — where it starts (the painter's own default), what
// moves it (exactly the mode report bubbletea moves the painter on, and nothing else), and that
// each operation is the ansi helper of the same name with the authority's method substituted
// for the hard-wired GraphemeWidth. vs16Warning — the grapheme the two measures disagree
// about — is declared in paint_test.go.

// The painter starts at ansi.WcWidth (ultraviolet/buffer.go:612-617, the zero value of
// ansi.Method), so the authority must too. The zero value is checked alongside the constructor
// because the authority is a value inside a value-copied Model: a widthAuthority that was never
// initialised has to be the honest default rather than a wrong one.
func TestWidthAuthorityStartsAtThePainterMeasure(t *testing.T) {
	t.Parallel()

	var zero widthAuthority
	if got := zero.Method(); got != ansi.WcWidth {
		t.Errorf("the zero widthAuthority measures with %v, want the painter's ansi.WcWidth", got)
	}
	if got := newWidthAuthority().Method(); got != ansi.WcWidth {
		t.Errorf("newWidthAuthority measures with %v, want the painter's ansi.WcWidth", got)
	}
	if got := newTheme().measure.Method(); got != ansi.WcWidth {
		t.Errorf("a fresh theme measures with %v, want the painter's ansi.WcWidth", got)
	}
}

// observe mirrors bubbletea's own reaction to a mode report, so the cases are bubbletea's:
// mode 2027 answered as anything other than "not recognized" or "permanently reset" moves the
// painter to GraphemeWidth (tea.go:786-798) — including the plain "reset", which says the
// terminal understands the mode and merely has it off, and which bubbletea then switches on.
// Every other report leaves the painter, and so the authority, alone.
func TestWidthAuthorityObservesTheUnicodeCoreReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report tea.ModeReportMsg
		want   ansi.Method
	}{
		{"2027 set", tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet}, ansi.GraphemeWidth},
		{"2027 reset — understood but off, which bubbletea turns on", tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset}, ansi.GraphemeWidth},
		{"2027 permanently set", tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModePermanentlySet}, ansi.GraphemeWidth},
		{"2027 permanently reset", tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModePermanentlyReset}, ansi.WcWidth},
		{"2027 not recognized", tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeNotRecognized}, ansi.WcWidth},
		{"a different mode, set", tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeSet}, ansi.WcWidth},
		{"a different mode, reset", tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeReset}, ansi.WcWidth},
		{"no mode at all", tea.ModeReportMsg{}, ansi.WcWidth},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := newWidthAuthority().observe(tc.report).Method(); got != tc.want {
				t.Errorf("observing %+v measures with %v, want %v", tc.report, got, tc.want)
			}
		})
	}

	t.Run("the switch is one-way, as the renderer's is", func(t *testing.T) {
		t.Parallel()
		w := newWidthAuthority().observe(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
		for _, v := range []ansi.ModeSetting{ansi.ModeNotRecognized, ansi.ModePermanentlyReset} {
			if got := w.observe(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: v}).Method(); got != ansi.GraphemeWidth {
				t.Errorf("a later %v report dropped the measure back to %v; bubbletea never moves the "+
					"painter back, so neither may the authority", v, got)
			}
		}
	})

	t.Run("observe returns rather than mutates", func(t *testing.T) {
		t.Parallel()
		w := newWidthAuthority()
		_ = w.observe(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
		if got := w.Method(); got != ansi.WcWidth {
			t.Errorf("observe mutated its receiver (now %v); it must return the new authority", got)
		}
	})
}

// Each operation is the ansi helper of the same name with the authority's method substituted for
// the hard-wired one: under GraphemeWidth it must equal ansi.X exactly, under WcWidth it must
// equal ansi.XWc exactly. Pinning the whole set against the library this way means an operation
// cannot quietly acquire behaviour of its own while items 3–6 convert call sites onto it.
func TestWidthAuthorityOperationsMatchTheAnsiHelpers(t *testing.T) {
	t.Parallel()

	grapheme := widthAuthority{method: ansi.GraphemeWidth}
	wc := newWidthAuthority()

	samples := []struct{ name, s string }{
		{"empty", ""},
		{"ascii", "the quick brown fox"},
		{"cjk", "日本語のテキスト"},
		{"emoji presentation", "🙂 done"},
		{"vs16", "a" + vs16Warning + "b"},
		{"styled", "\x1b[31mred\x1b[0m text"},
		{"pipes and hyphens", "| --- | --- |"},
	}
	limits := []int{0, 1, 2, 3, 5, 8, 20}

	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			s := sample.s

			if got, want := grapheme.Width(s), ansi.StringWidth(s); got != want {
				t.Errorf("GraphemeWidth Width(%q) = %d, want ansi.StringWidth's %d", s, got, want)
			}
			if got, want := wc.Width(s), ansi.StringWidthWc(s); got != want {
				t.Errorf("WcWidth Width(%q) = %d, want ansi.StringWidthWc's %d", s, got, want)
			}

			for _, n := range limits {
				if got, want := grapheme.Truncate(s, n, "…"), ansi.Truncate(s, n, "…"); got != want {
					t.Errorf("GraphemeWidth Truncate(%q, %d) = %q, want ansi.Truncate's %q", s, n, got, want)
				}
				if got, want := wc.Truncate(s, n, "…"), ansi.TruncateWc(s, n, "…"); got != want {
					t.Errorf("WcWidth Truncate(%q, %d) = %q, want ansi.TruncateWc's %q", s, n, got, want)
				}
				if got, want := grapheme.Cut(s, 0, n), ansi.Cut(s, 0, n); got != want {
					t.Errorf("GraphemeWidth Cut(%q, 0, %d) = %q, want ansi.Cut's %q", s, n, got, want)
				}
				if got, want := wc.Cut(s, 0, n), ansi.CutWc(s, 0, n); got != want {
					t.Errorf("WcWidth Cut(%q, 0, %d) = %q, want ansi.CutWc's %q", s, n, got, want)
				}
				if n == 0 {
					continue // the wrappers divide by the limit; ansi's own helpers are undefined at 0
				}
				if got, want := grapheme.Wrap(s, n, ""), ansi.Wrap(s, n, ""); got != want {
					t.Errorf("GraphemeWidth Wrap(%q, %d) = %q, want ansi.Wrap's %q", s, n, got, want)
				}
				if got, want := wc.Wrap(s, n, ""), ansi.WrapWc(s, n, ""); got != want {
					t.Errorf("WcWidth Wrap(%q, %d) = %q, want ansi.WrapWc's %q", s, n, got, want)
				}
				if got, want := grapheme.Wordwrap(s, n, ""), ansi.Wordwrap(s, n, ""); got != want {
					t.Errorf("GraphemeWidth Wordwrap(%q, %d) = %q, want ansi.Wordwrap's %q", s, n, got, want)
				}
				if got, want := wc.Wordwrap(s, n, ""), ansi.WordwrapWc(s, n, ""); got != want {
					t.Errorf("WcWidth Wordwrap(%q, %d) = %q, want ansi.WordwrapWc's %q", s, n, got, want)
				}
				if got, want := grapheme.Hardwrap(s, n, true), ansi.Hardwrap(s, n, true); got != want {
					t.Errorf("GraphemeWidth Hardwrap(%q, %d) = %q, want ansi.Hardwrap's %q", s, n, got, want)
				}
				if got, want := wc.Hardwrap(s, n, true), ansi.HardwrapWc(s, n, true); got != want {
					t.Errorf("WcWidth Hardwrap(%q, %d) = %q, want ansi.HardwrapWc's %q", s, n, got, want)
				}
			}
		})
	}
}

// The disagreement the authority exists to resolve, spelled out on one line rather than left to
// the parity test's indirection: under WcWidth — what an unquestioned terminal actually paints —
// "a⚠️b" is three columns and a two-column cut keeps the warning sign; under GraphemeWidth it is
// four and the same cut stops before it. A dependency bump that moves either column changes what
// the TUI paints and should fail here.
func TestWidthAuthorityMeasuresVS16TheWayItIsPainted(t *testing.T) {
	t.Parallel()

	const line = "a" + vs16Warning + "b"
	wc := newWidthAuthority()
	grapheme := widthAuthority{method: ansi.GraphemeWidth}

	if got := wc.Width(line); got != 3 {
		t.Errorf("WcWidth Width(%q) = %d, want 3", line, got)
	}
	if got := grapheme.Width(line); got != 4 {
		t.Errorf("GraphemeWidth Width(%q) = %d, want 4", line, got)
	}
	if got, want := wc.Cut(line, 0, 2), "a"+vs16Warning; got != want {
		t.Errorf("WcWidth Cut(%q, 0, 2) = %q, want %q", line, got, want)
	}
	if got, want := grapheme.Cut(line, 0, 2), "a"; got != want {
		t.Errorf("GraphemeWidth Cut(%q, 0, 2) = %q, want %q", line, got, want)
	}
	if got, want := wc.Truncate(line, 3, ""), line; got != want {
		t.Errorf("WcWidth Truncate(%q, 3) = %q, want the whole line %q", line, got, want)
	}
	if got, want := grapheme.Truncate(line, 3, ""), "a"+vs16Warning; got != want {
		t.Errorf("GraphemeWidth Truncate(%q, 3) = %q, want %q", line, got, want)
	}
}

// A cut with a non-zero LEFT is what a mouse selection is, and it is the one operation the
// authority cannot delegate to ansi.Method: on the WcWidth branch — the painter's default —
// x/ansi@v0.11.7 binds cut's left-truncation to TruncateWc rather than TruncateLeftWc, so the
// library's own Cut spends left as a width rather than as an offset and hands back the first
// `left` columns — Cut("abcdef", 2, 5) is "ab", not "cde". The parity test above cannot see it:
// every case there cuts from column 0.
func TestWidthAuthorityCutsFromTheLeft(t *testing.T) {
	t.Parallel()

	const line = "abcdef"
	for _, w := range []widthAuthority{newWidthAuthority(), {method: ansi.GraphemeWidth}} {
		if got, want := w.Cut(line, 2, 5), "cde"; got != want {
			t.Errorf("%v Cut(%q, 2, 5) = %q, want %q", w.Method(), line, got, want)
		}
		if got, want := w.Cut(line, 4, 99), "ef"; got != want {
			t.Errorf("%v Cut(%q, 4, 99) = %q, want the rest of the line %q", w.Method(), line, got, want)
		}
		if got := w.Cut(line, 3, 3); got != "" {
			t.Errorf("%v Cut(%q, 3, 3) = %q, want the empty span", w.Method(), line, got)
		}
	}

	// And on the grapheme the two measures disagree about: a click past the ⚠️ names the glyph
	// after it in the measure that is actually being painted, one column apart in the two.
	const vs16 = "a" + vs16Warning + "bc"
	if got, want := newWidthAuthority().Cut(vs16, 2, 3), "b"; got != want {
		t.Errorf("WcWidth Cut(%q, 2, 3) = %q, want %q — the ⚠️ paints one column there", vs16, got, want)
	}
	if got, want := (widthAuthority{method: ansi.GraphemeWidth}).Cut(vs16, 3, 4), "b"; got != want {
		t.Errorf("GraphemeWidth Cut(%q, 3, 4) = %q, want %q — the ⚠️ paints two columns there", vs16, got, want)
	}
}

// The authority hangs on the theme, which hangs on the Model, and the Model is copied by value on
// every Update (ADR 0011; doc.go). So it must be a value all the way down: no pointer, no slice,
// no map, no channel, nothing whose copy shares state with its original. This mirrors
// TestModelNoBuilderByValue's structural walk rather than testing a behaviour, for the same
// reason — a field added later must fail the guard even if no test happens to exercise it.
func TestWidthAuthoritySurvivesAValueCopy(t *testing.T) {
	t.Parallel()

	var walk func(typ reflect.Type, path string)
	walk = func(typ reflect.Type, path string) {
		switch typ.Kind() {
		case reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64, reflect.String:
			// A plain scalar: a copy is wholly its own.
		case reflect.Struct:
			for i := range typ.NumField() {
				f := typ.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		case reflect.Array:
			walk(typ.Elem(), path+"[]")
		default:
			t.Errorf("widthAuthority reaches a %s at %s — the authority rides on the value-copied "+
				"Model and must be a plain value, so a copy shares nothing with its original", typ.Kind(), path)
		}
	}
	walk(reflect.TypeOf(widthAuthority{}), "widthAuthority")

	// And behaviourally: a copy that observes a report leaves the original measuring as it was.
	original := newWidthAuthority()
	adopted := original.observe(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
	if original.Method() != ansi.WcWidth || adopted.Method() != ansi.GraphemeWidth {
		t.Fatalf("copy is entangled: original %v, adopted %v", original.Method(), adopted.Method())
	}
	if got, want := original.Width(vs16Warning), 1; got != want {
		t.Errorf("the original now measures %q as %d, want %d", vs16Warning, got, want)
	}
	if got, want := adopted.Width(vs16Warning), 2; got != want {
		t.Errorf("the adopted copy measures %q as %d, want %d", vs16Warning, got, want)
	}
}

// End to end through the real Update: bubbletea hands the mode report to the model after acting
// on it itself (tea.go:786-798 does not `continue`), which is the whole reason strategy (a) —
// follow the painter — is available at all. If a bubbletea bump ever stops forwarding the
// message, this fails and the authority is silently stuck on WcWidth.
func TestUpdateFollowsThePainterToGraphemeWidth(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	if got := m.th.measure.Method(); got != ansi.WcWidth {
		t.Fatalf("a fresh model measures with %v, want the painter's ansi.WcWidth", got)
	}

	after := step(t, m, tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
	if got := after.th.measure.Method(); got != ansi.GraphemeWidth {
		t.Errorf("after the terminal confirmed mode 2027 the model measures with %v, want "+
			"ansi.GraphemeWidth — the measure the painter just switched to", got)
	}

	unrelated := step(t, m, tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeSet})
	if got := unrelated.th.measure.Method(); got != ansi.WcWidth {
		t.Errorf("an unrelated mode report moved the measure to %v; only mode 2027 says anything "+
			"about width", got)
	}
}
