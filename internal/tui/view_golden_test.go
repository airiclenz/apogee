package tui

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// updateFrames rewrites the golden frames under testdata/frames from the current tree
// (`go test -run TestViewFrameGolden ./internal/tui/ -update`). The goldens were captured BEFORE the
// transcript body stopped being drawn by the viewport widget, so a run without the flag is the proof
// that apogee's own composition of the body draws the frame the widget drew, byte for byte.
var updateFrames = flag.Bool("update", false, "rewrite the golden frames under testdata/frames")

// goldenTranscript seeds the shapes a body composer can get wrong: a line filled to the column that
// ends in VARIATION SELECTOR-16 glyphs (one painted cell, two to the widget — the reserve's case), a
// line of wide CJK graphemes, styled markdown (SGR runs), and an expanded tool result whose output
// carries tabs. A second prompt follows so the scrollback holds more than one user block for the sticky
// header to hand over between.
func goldenTranscript(t *testing.T, m Model) Model {
	t.Helper()
	m.transcript.reset()
	m.transcript.addUser("go", nil)
	// −4: the assistant marker "✦ " spends two painted cells and the two ⚠️ spend one each.
	m.transcript.commitAssistant(strings.Repeat("a", m.transcriptWidth()-4)+"⚠️⚠️\n\n"+
		"漢字とかなの行 — wide graphemes ✔️ ℹ️\n\nSome **bold** and `code` text.", runRef{})
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID: "c1", Content: "ok\ta\t0.1s\nok\tb\t0.2s\nPASS"}})
	m.transcript.entries[len(m.transcript.entries)-1].expanded = true // draw the tab-bearing output
	m.transcript.addUser("and then?", nil)
	reply := make([]string, 0, 60)
	for i := range 60 {
		reply = append(reply, strings.Repeat("line ", 1+i%9)+"漢")
	}
	m.transcript.commitAssistant(strings.Join(reply, "\n\n"), runRef{})
	m.refreshViewport()
	return m
}

// TestViewFrameGolden pins the whole composed frame — transcript body, scroll bar, sticky header,
// block-cursor bar, side panel — on fixtures that exercise each, against frames captured from the
// tree where the bubbles viewport still drew the body.
func TestViewFrameGolden(t *testing.T) {
	t.Parallel()
	sized := func(t *testing.T, w, h int, opts func(*Options)) Model {
		o := testOpts
		if opts != nil {
			opts(&o)
		}
		m := newModel(t.Context(), &fakeEngine{}, o, nil)
		return step(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	}
	fixtures := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"short-tail", func(t *testing.T) Model {
			m := sized(t, 80, 24, nil)
			m.transcript.reset()
			m.transcript.addUser("hi", nil)
			m.transcript.commitAssistant("Hello ⚠️ 漢字.", runRef{})
			m.refreshViewport()
			return m
		}},
		{"tail-scrollbar-on", func(t *testing.T) Model {
			return goldenTranscript(t, sized(t, 80, 24, nil))
		}},
		{"tail-scrollbar-off", func(t *testing.T) Model {
			return goldenTranscript(t, sized(t, 80, 24, func(o *Options) { o.UI.ShowScrollbar = false }))
		}},
		{"detached", func(t *testing.T) Model {
			m := goldenTranscript(t, sized(t, 80, 24, nil))
			m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
			m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
			if !m.detached {
				t.Fatal("setup: two PgUp did not detach the view")
			}
			return m
		}},
		{"detached-top", func(t *testing.T) Model {
			m := goldenTranscript(t, sized(t, 80, 24, nil))
			for range 20 {
				m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
			}
			return m
		}},
		{"block-cursor", func(t *testing.T) Model {
			m := goldenTranscript(t, sized(t, 80, 24, nil))
			return step(t, m, keyAltUp())
		}},
		{"side-panel", func(t *testing.T) Model {
			m := goldenTranscript(t, sized(t, 140, 40, func(o *Options) {
				o.UI.SidePanelShow = domain.SidePanelShowAuto
			}))
			if !m.panelShown() {
				t.Fatal("setup: the side panel is not shown at 140×40")
			}
			return m
		}},
		{"side-panel-off", func(t *testing.T) Model {
			return goldenTranscript(t, sized(t, 140, 40, nil))
		}},
	}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			got := fx.build(t).View().Content
			path := filepath.Join("testdata", "frames", fx.name+".golden")
			if *updateFrames {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to capture it): %v", err)
			}
			if got != string(want) {
				t.Errorf("the %s frame drifted from its golden:\n got: %q\nwant: %q", fx.name, got, want)
			}
		})
	}
}

// TestTranscriptBodyMatchesTheViewportPass is the composer against its oracle: a real bubbles
// viewport handed the real lines — what View drew before the widget was handed placeholders — and
// transcriptBody over a widget holding placeholders of the same count, at every offset, on a corpus
// of the shapes a pad pass can get wrong (VS16 and wide graphemes, tabs, SGR runs, blank lines), a
// short tail that leaves the body shorter than the widget, and one over-wide row the cut must clip.
func TestTranscriptBodyMatchesTheViewportPass(t *testing.T) {
	t.Parallel()
	painted := goldenTranscript(t, newTestModel(t))
	corpora := []struct {
		name  string
		lines []string
		w, h  int
	}{
		{"painted", painted.lines, painted.viewport.Width(), painted.viewport.Height()},
		{"synthetic", []string{
			"\x1b[1mbold\x1b[m and \x1b[38;2;200;200;240mcolour\x1b[m",
			"tab\there\tand\tthere",
			"漢字とかな",
			"⚠️ warn ✔️ ok ℹ️",
			"",
			"plain text",
			"\x1b[48;2;74;74;74m❯ a prompt band                 \x1b[m",
			"",
			"last",
		}, 40, 4},
		{"short-tail", []string{"one ⚠️", "two 漢", "three\tx"}, 40, 10},
		{"over-wide", []string{"fits", strings.Repeat("漢", 30), "fits too"}, 40, 3},
	}
	for _, c := range corpora {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for off := range len(c.lines) + 1 {
				oracle := viewport.New()
				oracle.SoftWrap = false
				oracle.SetHorizontalStep(0)
				oracle.SetWidth(c.w)
				oracle.SetHeight(c.h)
				oracle.SetContentLines(slices.Clone(c.lines))
				oracle.SetYOffset(off)

				m := painted
				m.lines = c.lines
				vp := viewport.New()
				vp.SoftWrap = false
				vp.SetHorizontalStep(0)
				vp.SetWidth(c.w)
				vp.SetHeight(c.h)
				vp.SetContentLines(placeholderLines(c.lines))
				vp.SetYOffset(off)

				if got, want := vp.YOffset(), oracle.YOffset(); got != want {
					t.Fatalf("offset %d: the placeholder widget sits at %d, the real one at %d", off, got, want)
				}
				if got, want := vp.TotalLineCount(), oracle.TotalLineCount(); got != want {
					t.Fatalf("offset %d: the placeholder widget counts %d lines, the real one %d", off, got, want)
				}
				if got, want := m.transcriptBody(vp), oracle.View(); got != want {
					t.Errorf("offset %d: the composed body differs from the viewport's pass:\n got: %q\nwant: %q",
						off, got, want)
				}
			}
		})
	}
}

// TestTranscriptLinesHoldNoNewlines pins what the placeholders rely on: the widget splits a content
// line at "\r" / "\n" into extra rows, which a blank placeholder never is, so one stored line holding
// either would make the widget's count and the paint's part — and every row-addressed reader below
// it would address the wrong line. The fixture streams a reply carrying both into a painted
// transcript, so the streaming preview is held to it as well as the settled blocks.
func TestTranscriptLinesHoldNoNewlines(t *testing.T) {
	t.Parallel()
	m := goldenTranscript(t, newTestModel(t))
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "streamed\r\nreply\nwith\ttabs\r\n"}})
	if !slices.ContainsFunc(m.lines, func(ln string) bool { return strings.Contains(ln, "streamed") }) {
		t.Fatal("setup: the streamed reply is not in the painted lines")
	}
	for i, ln := range m.lines {
		if strings.ContainsAny(ln, "\r\n") {
			t.Errorf("stored line %d holds a line break: %q", i, ln)
		}
	}
	if got, want := m.viewport.TotalLineCount(), len(m.lines); got != want {
		t.Errorf("the widget counts %d lines for %d stored lines", got, want)
	}
}
