package tui

// The 2026-11-23 TUI performance audit's benchmarks, kept so a later change can be measured against
// the same workloads the audit profiled. The audit report is docs/reviews/tui-perf-audit-2026-11-23.md
// (its Profile section reads these numbers); later plans cite the helper and benchmark names below,
// so keep them stable.
//
// Every workload is a resumed session (opts.Resumed, the --resume path) of N tool blocks at
// paWidth×paHeight, driven through Update and View. Run one benchmark at a time, without -race:
//
//	go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem ./internal/tui/

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

const (
	paWidth  = 120
	paHeight = 40
)

// paAnswer is an assistant reply with real markdown: a heading, a list, inline code and a fence.
func paAnswer(i int) string {
	return fmt.Sprintf("## Step %d\n\nThe fold turns each `domain.Event` into a transcript entry and the\n"+
		"renderer turns entries into lines. Here is what changed:\n\n"+
		"- `render.go` now caches the **paint** of settled blocks\n"+
		"- `fold.go` keeps the pending tail apart\n\n"+
		"```go\nfunc (tr *transcript) renderView(th theme, width int) renderedTranscript {\n"+
		"\tfor i := range tr.entries {\n\t\tpaint(tr.entries[i], width) // %d\n\t}\n\treturn out\n}\n```\n\n"+
		"That keeps a repaint cheap while a reply streams.", i, i)
}

// paEditRegions is a ten-line diff body: three context lines either side of a two-for-three swap.
func paEditRegions(i int) []domain.EditRegion {
	return []domain.EditRegion{{
		BeforeStart: 10 + i, AfterStart: 10 + i,
		Leading:  []string{"func paint() {", "\tvar out []string", "\tfor _, e := range entries {"},
		Removed:  []string{"\t\tout = append(out, render(e))", "\t\tout = wrap(out, width)", "\t\tcount++"},
		Inserted: []string{fmt.Sprintf("\t\tout = append(out, cached(e, %d))", i), "\t\tcount += len(out)"},
		Trailing: []string{"\t}", "\treturn out", "}"},
	}}
}

// paTerminalOutput is twenty lines of test output.
func paTerminalOutput(i int) string {
	var b strings.Builder
	for k := range 20 {
		fmt.Fprintf(&b, "ok  \tgithub.com/example/pkg%d/sub%d\t0.%03ds\n", i, k, k*7)
	}
	b.WriteString("PASS")
	return b.String()
}

// paSession builds a transcript with `tools` tool blocks, one third each read / edit (diff body) /
// terminal, every block its own (assistant narration between them, as a narrating model writes),
// and returns it encoded as the session record's scrollback blob.
func paSession(tb testing.TB, tools int) []byte {
	tr := &transcript{}
	rounds := tools / 3
	for i := range rounds {
		tr.addUser(fmt.Sprintf("question %d — please fix the paint path and run the tests", i), nil)
		tr.apply(domain.MessageEvent{Text: paAnswer(i)})
		readCall(tr, fmt.Sprintf("r%d", i), fmt.Sprintf("internal/pkg/file%d.go", i), 1, 120, 0)
		tr.apply(domain.MessageEvent{Text: "Now the edit."})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: fmt.Sprintf("e%d", i), Tool: "single_find_and_replace",
			Arguments: []byte(fmt.Sprintf(`{"path":"internal/pkg/file%d.go","oldText":"out = wrap(out, width)","newText":"count += len(out)"}`, i))}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: fmt.Sprintf("e%d", i),
			Content: fmt.Sprintf("replaced text in internal/pkg/file%d.go", i), Summary: domain.EditRegions{Regions: paEditRegions(i)}}})
		tr.apply(domain.MessageEvent{Text: "And the tests."})
		runCall(tr, fmt.Sprintf("t%d", i), "go test ./...", paTerminalOutput(i), 0)
		tr.apply(domain.MessageEvent{Text: fmt.Sprintf("All green in round %d. The paint path now reuses settled blocks.", i)})
	}
	blob, err := encodeTranscript(tr)
	if err != nil {
		tb.Fatal(err)
	}
	return blob
}

// paModel is a model resumed (opts.Resumed → resumedSeed, the --resume path) onto a session with
// `tools` tool blocks, sized to paWidth×paHeight.
func paModel(tb testing.TB, tools int) Model {
	opts := testOpts
	opts.Resumed = &ResumedSession{Transcript: paSession(tb, tools), Title: "perf audit"}
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: paWidth, Height: paHeight})
	m = next.(Model)
	_ = m.View()
	if tools > 0 && len(m.lines) < tools {
		tb.Fatalf("the resumed session painted %d lines for %d tool blocks", len(m.lines), tools)
	}
	return m
}

func paStep(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	m = next.(Model)
	_ = m.View()
	return m
}

func paViewString(m Model) string { return m.View().Content }

// ---------------------------------------------------------------- stream

// BenchmarkPerfAuditStream: one op is a whole 4096-delta reply (≈8 bytes each, markdown with code
// fences) folded through Update as eventMsg{TokenEvent} with a View after every delta — the sink's
// worst case, one delta per Msg. ns/delta and the first/last 512-delta costs are reported so growth
// with the reply's length shows.
func BenchmarkPerfAuditStream(b *testing.B) {
	reply := strings.Repeat(paAnswer(7)+"\n\n", 80)
	const deltas, size = 4096, 8
	chunks := make([]string, 0, deltas)
	for i := 0; i < deltas && (i+1)*size <= len(reply); i++ {
		chunks = append(chunks, reply[i*size:(i+1)*size])
	}
	if len(chunks) != deltas {
		b.Fatalf("reply too short: %d chunks", len(chunks))
	}
	for _, tools := range []int{0, 300} {
		b.Run(fmt.Sprintf("session=%d", tools), func(b *testing.B) {
			b.ReportAllocs()
			var first, last time.Duration
			for range b.N {
				b.StopTimer()
				m := paModel(b, tools)
				m.state = stateRunning
				b.StartTimer()
				for i, c := range chunks {
					start := time.Now()
					m = paStep(m, eventMsg{Event: domain.TokenEvent{Text: c}})
					d := time.Since(start)
					if i < 512 {
						first += d
					} else if i >= deltas-512 {
						last += d
					}
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*deltas), "ns/delta")
			b.ReportMetric(float64(first.Nanoseconds())/float64(b.N*512), "ns/delta-first512")
			b.ReportMetric(float64(last.Nanoseconds())/float64(b.N*512), "ns/delta-last512")
		})
	}
}

// ---------------------------------------------------------------- blocks

// BenchmarkPerfAuditBlocks: a resumed session of N tool blocks at the bottom of the transcript.
// view: View alone on an unchanged model. repaint: refreshViewport (the paint-cache path every
// Update's repaint tail takes when the transcript moved) then View. resize: a WindowSizeMsg
// alternating 120/119 columns (invalidates every block's paint) then View.
func BenchmarkPerfAuditBlocks(b *testing.B) {
	for _, tools := range []int{48, 300, 900} {
		b.Run(fmt.Sprintf("view/N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			b.ReportAllocs()
			for b.Loop() {
				_ = m.View()
			}
		})
		b.Run(fmt.Sprintf("repaint/N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			b.ReportAllocs()
			for b.Loop() {
				m.refreshViewport()
				_ = m.View()
			}
		})
		b.Run(fmt.Sprintf("resize/N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				m = paStep(m, tea.WindowSizeMsg{Width: paWidth - i%2, Height: paHeight})
				i++
			}
		})
	}
}

// ---------------------------------------------------------------- fold

// BenchmarkPerfAuditFold: navigation mode (⌥↑ then ↑ to a tool block whose ⏎ moves the frame), then
// each op is one ⏎ — the fold flips open/closed — through Update and View.
func BenchmarkPerfAuditFold(b *testing.B) {
	for _, tools := range []int{48, 300} {
		b.Run(fmt.Sprintf("N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			m = paStep(m, keyAltUp())
			found := false
			for range 12 {
				before := paViewString(m)
				probe := paStep(m, keyEnter())
				if paViewString(probe) != before && probe.cursor.active {
					m = paStep(probe, keyEnter()) // back to closed
					found = true
					break
				}
				m = paStep(m, tea.KeyPressMsg{Code: tea.KeyUp})
			}
			if !found {
				b.Fatal("no stop near the bottom folds on ⏎")
			}
			if tg := m.lineTargets[m.cursor.line]; true {
				b.Logf("fold target: line %d kind %v entry %d (%v) lines=%d", m.cursor.line, tg.kind, tg.entry,
					m.transcript.entries[tg.entry].kind, len(m.lines))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m = paStep(m, keyEnter())
			}
		})
	}
}

// ---------------------------------------------------------------- scroll

// BenchmarkPerfAuditScroll: per key, through Update and View. page: 20 PgUp then 20 PgDn from the
// bottom, cycling. wheel: 30 wheel-up then 30 wheel-down over the transcript, cycling.
func BenchmarkPerfAuditScroll(b *testing.B) {
	for _, tools := range []int{48, 300} {
		b.Run(fmt.Sprintf("page/N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			up, down := tea.KeyPressMsg{Code: tea.KeyPgUp}, tea.KeyPressMsg{Code: tea.KeyPgDown}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%40 < 20 {
					m = paStep(m, up)
				} else {
					m = paStep(m, down)
				}
			}
		})
		b.Run(fmt.Sprintf("wheel/N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			up := tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 10, Y: 5}
			down := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 10, Y: 5}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%60 < 30 {
					m = paStep(m, up)
				} else {
					m = paStep(m, down)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- type

// BenchmarkPerfAuditType: per key, through Update and View, idle with the session behind the box:
// 64 printable runes then 64 backspaces, cycling, so the draft stays short.
func BenchmarkPerfAuditType(b *testing.B) {
	for _, tools := range []int{0, 48, 300} {
		b.Run(fmt.Sprintf("N=%d", tools), func(b *testing.B) {
			m := paModel(b, tools)
			bs := tea.KeyPressMsg{Code: tea.KeyBackspace}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%128 < 64 {
					m = paStep(m, keyRune('a'+rune(i%26)))
				} else {
					m = paStep(m, bs)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- idle

// BenchmarkPerfAuditIdle: one spinner tick through Update and View while a Turn runs with the
// session behind it. settled: every block closed, so the tick changes only the status line. live: an
// open tool call at the tail, so its live star blinks and the ticks that flip the phase repaint.
func BenchmarkPerfAuditIdle(b *testing.B) {
	for _, tools := range []int{48, 300} {
		for _, live := range []bool{false, true} {
			name := "settled"
			if live {
				name = "live"
			}
			b.Run(fmt.Sprintf("%s/N=%d", name, tools), func(b *testing.B) {
				m := paModel(b, tools)
				m.state = stateRunning
				if live {
					m = paStep(m, eventMsg{Event: domain.ToolCallEvent{Call: domain.ToolCall{ID: "live1", Tool: "terminal",
						Arguments: []byte(`{"command":"go test ./..."}`)}}})
				}
				_ = m.spin.arm(m.opts.UI.Spinner)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					m = paStep(m, spinnerTickMsg{gen: m.spin.gen})
				}
			})
		}
	}
}
