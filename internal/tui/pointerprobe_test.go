package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The OSC 22 capability probe and the all-motion gate (pointerprobe.go)
// ----------------------------------------------------------------------------
//
// The gate has two halves — the human's ui.pointer-shapes and the terminal's answer to the one
// query a session sends — and the frame's mouse mode is the observable that says whether it is
// open. These tests drive the probe through the Msgs the program delivers (Init's Cmds, the
// ultraviolet events, the /settings apply) and read the answer off View and the Cmds written.

// wantProbeWrite is the exact write a probe makes: the query, then the DA1 sentinel, in one Raw.
const wantProbeWrite = "\x1b]22;?default,text,pointer\x1b\\" + "\x1b[c"

// pointerShapesOpts is testOpts with ui.pointer-shapes set to on.
func pointerShapesOpts(on bool) Options {
	opts := testOpts
	opts.UI.PointerShapes = on
	return opts
}

// probeWrites counts the Msgs cmd delivers that are the probe's query write.
func probeWrites(t *testing.T, cmd tea.Cmd) int {
	t.Helper()
	n := 0
	for _, msg := range expandBatch(cmd) {
		if raw, isRaw := msg.(tea.RawMsg); isRaw && raw.Msg == wantProbeWrite {
			n++
		}
	}
	return n
}

// startedModel is newModel plus its Init and the first WindowSizeMsg — the start-up a real program
// runs — returning the model and how many probe queries Init wrote.
func startedModel(t *testing.T, opts Options) (Model, int) {
	t.Helper()
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	queries := probeWrites(t, m.Init())
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}), queries
}

// oscReply is the terminal's answer to the query as ultraviolet hands it to Update: the whole
// sequence, introducer and terminator included.
func oscReply(answer, terminator string) uv.UnknownOscEvent {
	return uv.UnknownOscEvent("\x1b]22;" + answer + terminator)
}

// da1Reply is a DA1 answer, the probe's sentinel.
func da1Reply() uv.PrimaryDeviceAttributesEvent { return uv.PrimaryDeviceAttributesEvent{62, 22} }

func TestPointerProbeOffSendsNoQueryAndKeepsCellMotion(t *testing.T) {
	t.Parallel()

	m, queries := startedModel(t, pointerShapesOpts(false))
	m = step(t, m, oscReply(pointerShapeFullSupport, "\x1b\\")) // an answer nobody asked for

	if queries != 0 {
		t.Errorf("Init wrote %d probe queries with the setting off, want 0", queries)
	}
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("View asked for mouse mode %v with the setting off, want cell motion", got)
	}
}

func TestPointerProbeAtStartupWritesTheQueryOnce(t *testing.T) {
	t.Parallel()

	_, queries := startedModel(t, pointerShapesOpts(true))

	if queries != 1 {
		t.Errorf("Init wrote %d probe queries with the setting on, want 1 (%q)", queries, wantProbeWrite)
	}
}

// The verdict decides the mode, and only an answer that arrives ahead of the sentinel counts.
func TestPointerProbeVerdictDecidesTheMouseMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		replies []tea.Msg
		want    tea.MouseMode
	}{
		{"full answer ST-terminated", []tea.Msg{oscReply("1,1,1", "\x1b\\"), da1Reply()}, tea.MouseModeAllMotion},
		{"full answer BEL-terminated", []tea.Msg{oscReply("1,1,1", "\a"), da1Reply()}, tea.MouseModeAllMotion},
		{"one shape missing", []tea.Msg{oscReply("1,0,1", "\x1b\\"), da1Reply()}, tea.MouseModeCellMotion},
		{"DA1 first, late answer ignored", []tea.Msg{da1Reply(), oscReply("1,1,1", "\x1b\\")}, tea.MouseModeCellMotion},
		{"no reply at all", nil, tea.MouseModeCellMotion},
		{"another OSC, then DA1", []tea.Msg{uv.UnknownOscEvent("\x1b]99;x\x1b\\"), da1Reply()}, tea.MouseModeCellMotion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, _ := startedModel(t, pointerShapesOpts(true))

			for _, reply := range tc.replies {
				m = step(t, m, reply)
			}

			if got := m.View().MouseMode; got != tc.want {
				t.Errorf("View asked for mouse mode %v, want %v", got, tc.want)
			}
		})
	}
}

// /settings turns the feature on in a session that never asked: the query goes out then, once, and
// a later off-and-on reuses the kept answer instead of asking again.
func TestPointerProbeSettingsToggleQueriesOnce(t *testing.T) {
	t.Parallel()
	m, _ := startedModel(t, pointerShapesOpts(false))

	m, _, firstOn, _, err := m.settingsApplyLocal(domain.UIKeyPointerShapes, settingTrue)
	if err != nil {
		t.Fatalf("apply on: %v", err)
	}
	m = step(t, m, oscReply(pointerShapeFullSupport, "\x1b\\"))
	liveMode := m.View().MouseMode
	m, _, off, _, err := m.settingsApplyLocal(domain.UIKeyPointerShapes, settingFalse)
	if err != nil {
		t.Fatalf("apply off: %v", err)
	}
	offMode := m.View().MouseMode
	m, _, secondOn, _, err := m.settingsApplyLocal(domain.UIKeyPointerShapes, settingTrue)
	if err != nil {
		t.Fatalf("apply on again: %v", err)
	}

	if n := probeWrites(t, firstOn); n != 1 {
		t.Errorf("turning the setting on wrote %d queries, want 1", n)
	}
	if n := probeWrites(t, off) + probeWrites(t, secondOn); n != 0 {
		t.Errorf("turning it off and on again wrote %d more queries, want 0 (the answer is kept)", n)
	}
	if liveMode != tea.MouseModeAllMotion || offMode != tea.MouseModeCellMotion {
		t.Errorf("mouse mode on/off = %v/%v, want all motion then cell motion", liveMode, offMode)
	}
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Errorf("after turning it on again View asked for %v, want all motion off the kept answer", got)
	}
}

// --tui-diag says which way the probe went and why, so a bug report about a missing shape can tell
// an unsupported terminal from a setting that was off.
func TestPointerProbeRecordsTheVerdictInTheDiagLog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		reply tea.Msg
		want  string
	}{
		{"supported", oscReply("1,1,1", "\x1b\\"), "pointer-probe: supported"},
		{"partial answer", oscReply("1,0,1", "\x1b\\"), "pointer-probe: unsupported (answered 1,0,1)"},
		{"sentinel first", da1Reply(), "pointer-probe: unsupported (no answer before DA1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "diag.txt")
			diag, err := newDiagLog(path)
			if err != nil {
				t.Fatalf("newDiagLog: %v", err)
			}
			m, _ := startedModel(t, pointerShapesOpts(true))
			m.diag = diag // Run wires it between newModel and the program; a test wires it the same way

			step(t, m, tc.reply)

			if err := diag.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			var recorded []string
			for _, line := range strings.Split(readFileString(t, path), "\n") {
				if strings.HasPrefix(line, diagPointerProbe+":") {
					recorded = append(recorded, line)
				}
			}
			if len(recorded) != 1 || recorded[0] != tc.want {
				t.Errorf("diag log recorded %q, want [%q]", recorded, tc.want)
			}
		})
	}
}

// BenchmarkPointerProbeHoverUpdateAndView prices what the open gate adds: every hovered cell is a
// button-less motion Msg, so an Update and the View composition the program draws after it. The
// closed gate never sees these Msgs (cell-motion reports nothing without a button).
func BenchmarkPointerProbeHoverUpdateAndView(b *testing.B) {
	var t testing.T // the fixtures take a *testing.T only to fail on a broken premise
	m, _ := startedModel(&t, pointerShapesOpts(true))
	m = step(&t, m, oscReply(pointerShapeFullSupport, "\x1b\\"))
	for i := range 25 {
		m.transcript.addUser(fmt.Sprintf("question %d — what does the fold do here?", i), nil)
		m.transcript.apply(domain.MessageEvent{Text: fmt.Sprintf("Answer %d, long enough to wrap once or twice "+
			"across the eighty columns of the frame so the transcript has real rows to compose.", i)})
	}
	m.refreshViewport()
	var next tea.Model = m
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		next, _ = next.(Model).Update(tea.MouseMotionMsg{X: i % 80, Y: i % 24, Button: tea.MouseNone})
		_ = next.View()
	}
}
