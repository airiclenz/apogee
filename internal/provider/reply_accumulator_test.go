package provider

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// accumulatorSink collects what a replyAccumulator yields. refuseAt, when positive, is the
// 1-based Delta the consumer refuses: it is recorded, and yield reports false for it.
type accumulatorSink struct {
	deltas   []Delta
	refuseAt int
}

// yield is the sink as the accumulator's consumer.
func (s *accumulatorSink) yield(d Delta) bool {
	s.deltas = append(s.deltas, d)
	return len(s.deltas) != s.refuseAt
}

// terminals counts the terminal Deltas the sink received.
func (s *accumulatorSink) terminals() int {
	count := 0
	for _, d := range s.deltas {
		if d.Kind == DeltaDone || d.Kind == DeltaError {
			count++
		}
	}
	return count
}

// accumulatorCall is a tool-call fragment opening a call at index with its id, name and a
// first piece of its arguments.
func accumulatorCall(index int, id, name, arguments string) sseToolCall {
	frag := sseToolCall{ID: id, Index: &index}
	frag.Function.Name = name
	frag.Function.Arguments = arguments
	return frag
}

// requireDeltas fails the test when the sink did not receive exactly want.
func requireDeltas(t *testing.T, sink *accumulatorSink, want []Delta) {
	t.Helper()

	if !reflect.DeepEqual(sink.deltas, want) {
		t.Fatalf("deltas =\n%s\nwant\n%s", dumpDeltas(abridgeDeltas(sink.deltas)), dumpDeltas(abridgeDeltas(want)))
	}
}

// accumulatorTextCapFault is the terminal Delta of a reply that crossed the text cap.
func accumulatorTextCapFault() Delta {
	return terminalLocalFault(fmt.Sprintf(
		"apogee: streamed reply exceeded the %d MiB text limit",
		maxReplyTextBytes>>20,
	))
}

// TestReplyAccumulator_TerminalPaths pins that every way a stream can end yields exactly one
// terminal Delta, and that every method afterwards is a no-op reporting ended.
func TestReplyAccumulator_TerminalPaths(t *testing.T) {
	t.Parallel()

	readErr := io.ErrUnexpectedEOF
	rows := []struct {
		name string
		// end drives the accumulator to its end.
		end func(a *replyAccumulator)
		// refuseAt is accumulatorSink.refuseAt for the row.
		refuseAt int
		want     []Delta
	}{
		{
			name: "finish",
			end:  func(a *replyAccumulator) { a.finish() },
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "bare EOF inside run",
			end: func(a *replyAccumulator) {
				a.run(strings.NewReader("data: one\n\n"), func(string) bool { return false })
			},
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "terminator inside run",
			end: func(a *replyAccumulator) {
				a.run(strings.NewReader("data: end\n\ndata: late\n\n"), func(string) bool {
					a.finish()
					return true
				})
			},
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "read fault",
			end: func(a *replyAccumulator) {
				body := io.MultiReader(strings.NewReader("data: one\n\n"), iotest.ErrReader(readErr))
				a.run(body, func(string) bool {
					a.skipMalformed()
					return false
				})
			},
			want: []Delta{{
				Kind:            DeltaError,
				Err:             "apogee: read stream: unexpected EOF (1 malformed chunks skipped)",
				Retryable:       true,
				MalformedChunks: 1,
				Fault:           readFault(readErr),
			}},
		},
		{
			name: "in-band error",
			end: func(a *replyAccumulator) {
				a.skipMalformed()
				a.fail(Delta{Kind: DeltaError, Err: "upstream said no"})
			},
			want: []Delta{{Kind: DeltaError, Err: "upstream said no", MalformedChunks: 1}},
		},
		{
			name: "codec-local fault",
			end: func(a *replyAccumulator) {
				a.skipMalformed()
				a.failLocal(terminalLocalFault("codec fault"))
			},
			want: []Delta{terminalLocalFault("codec fault")},
		},
		{
			name: "text cap",
			end:  func(a *replyAccumulator) { a.charge(maxReplyTextBytes + 1) },
			want: []Delta{accumulatorTextCapFault()},
		},
		{
			name: "tool-call byte cap",
			end: func(a *replyAccumulator) {
				a.foldCall(accumulatorCall(0, "call_1", "read", strings.Repeat("x", maxToolCallBytes+1)))
			},
			want: []Delta{terminalLocalFault(toolCallBytesTripped)},
		},
		{
			name: "tool-call count cap",
			end: func(a *replyAccumulator) {
				for i := 0; i <= maxOpenToolCalls; i++ {
					a.foldCall(accumulatorCall(i, fmt.Sprintf("call_%d", i), "read", ""))
				}
			},
			want: []Delta{terminalLocalFault(toolCallCountTripped)},
		},
		{
			name:     "consumer break",
			end:      func(a *replyAccumulator) { a.emit(Delta{Kind: DeltaContent, Content: "hi"}) },
			refuseAt: 1,
			want:     []Delta{{Kind: DeltaContent, Content: "hi"}},
		},
		{
			name: "consumer break inside the flush",
			end: func(a *replyAccumulator) {
				a.foldCall(accumulatorCall(0, "call_1", "read", "{}"))
				a.finish()
			},
			refuseAt: 1,
			want:     []Delta{terminalCall("call_1", "read", "{}")},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			sink := &accumulatorSink{refuseAt: row.refuseAt}
			a := newReplyAccumulator(nil, sink.yield, nil)

			row.end(a)

			requireDeltas(t, sink, row.want)
			wantTerminals := 1
			if row.refuseAt > 0 {
				wantTerminals = 0
			}
			if got := sink.terminals(); got != wantTerminals {
				t.Errorf("terminal deltas = %d, want %d", got, wantTerminals)
			}

			// Everything after the end is a no-op reporting ended.
			content := Delta{Kind: DeltaContent, Content: "late"}
			for name, ended := range map[string]bool{
				"charge":    a.charge(1),
				"emit":      a.emit(content),
				"text":      a.text(content, len(content.Content)),
				"text(0)":   a.text(Delta{Kind: DeltaContent}, 0),
				"foldCall":  a.foldCall(accumulatorCall(9, "call_late", "read", "{}")),
				"fail":      a.fail(Delta{Kind: DeltaError, Err: "late"}),
				"failLocal": a.failLocal(Delta{Kind: DeltaError, Err: "late"}),
			} {
				if !ended {
					t.Errorf("%s after the end reported ended = false", name)
				}
			}
			a.skipMalformed()
			a.setFinish("length")
			a.setUsage(Usage{})
			a.setModel("late")
			a.patchOpenCalls(func(string) string { return "late" })
			a.finish()
			a.run(strings.NewReader("data: late\n\n"), func(string) bool {
				t.Error("run after the end read an event")
				return false
			})
			requireDeltas(t, sink, row.want)
		})
	}
}

// TestReplyAccumulator_RunHandsOverDataPayloads pins the scan loop: only `data:` lines reach
// the codec, stripped of their prefix, and an event reporting ended stops the read.
func TestReplyAccumulator_RunHandsOverDataPayloads(t *testing.T) {
	t.Parallel()

	sink := &accumulatorSink{}
	a := newReplyAccumulator(nil, sink.yield, nil)
	body := "event: x\n: comment\n\ndata: one\n  data: two  \ndata:three\ndata: stop\ndata: late\n"
	var got []string

	a.run(strings.NewReader(body), func(data string) bool {
		got = append(got, data)
		return data == "stop"
	})

	if want := []string{"one", "two", "stop"}; !reflect.DeepEqual(got, want) {
		t.Errorf("payloads = %q, want %q", got, want)
	}
	if len(sink.deltas) != 0 {
		t.Errorf("an event that ended the read itself yielded %s", dumpDeltas(sink.deltas))
	}
}

// TestReplyAccumulator_ChargeAtTheLimit pins the text cap's edge: a reply exactly at the limit
// passes, and one byte over trips it, with neither the malformed count nor its note (D7).
func TestReplyAccumulator_ChargeAtTheLimit(t *testing.T) {
	t.Parallel()

	sink := &accumulatorSink{}
	a := newReplyAccumulator(nil, sink.yield, nil)
	a.skipMalformed()

	if a.charge(maxReplyTextBytes) {
		t.Fatalf("charge at the limit ended the stream: %s", dumpDeltas(sink.deltas))
	}
	if !a.charge(1) {
		t.Fatal("charge one byte over the limit did not end the stream")
	}

	requireDeltas(t, sink, []Delta{accumulatorTextCapFault()})
}

// TestReplyAccumulator_ToolCapFaultsCarryNoMalformedCount pins D7 for the two tool-call caps.
func TestReplyAccumulator_ToolCapFaultsCarryNoMalformedCount(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		trip func(a *replyAccumulator) (ended bool)
		want string
	}{
		{
			name: "argument bytes",
			trip: func(a *replyAccumulator) bool {
				if a.foldCall(accumulatorCall(0, "call_1", "read", strings.Repeat("x", maxToolCallBytes))) {
					return false // at the limit must pass
				}
				frag := sseToolCall{}
				frag.Function.Arguments = "x"
				return a.foldCall(frag)
			},
			want: toolCallBytesTripped,
		},
		{
			name: "open calls",
			trip: func(a *replyAccumulator) bool {
				for i := range maxOpenToolCalls {
					if a.foldCall(accumulatorCall(i, fmt.Sprintf("call_%d", i), "read", "")) {
						return false // at the limit must pass
					}
				}
				return a.foldCall(accumulatorCall(maxOpenToolCalls, "call_over", "read", ""))
			},
			want: toolCallCountTripped,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			sink := &accumulatorSink{}
			a := newReplyAccumulator(nil, sink.yield, nil)
			a.skipMalformed()

			if !row.trip(a) {
				t.Fatal("the cap did not trip one past its limit")
			}

			requireDeltas(t, sink, []Delta{terminalLocalFault(row.want)})
		})
	}
}

// TestReplyAccumulator_ToolFragmentRunsOnEveryFold pins that the fragment callback given at
// construction reaches the open set.
func TestReplyAccumulator_ToolFragmentRunsOnEveryFold(t *testing.T) {
	t.Parallel()

	sink := &accumulatorSink{}
	fragments := 0
	a := newReplyAccumulator(func() { fragments++ }, sink.yield, nil)

	a.foldCall(accumulatorCall(0, "call_1", "read", "{"))
	a.foldCall(accumulatorCall(0, "", "", "}"))

	if fragments != 2 {
		t.Errorf("tool fragments = %d, want 2", fragments)
	}
}

// TestReplyAccumulator_TextYieldsUnderTheCap pins text and emit: text charges then yields,
// a fragment of no bytes yields nothing and does not end, and emit yields without charging.
func TestReplyAccumulator_TextYieldsUnderTheCap(t *testing.T) {
	t.Parallel()

	sink := &accumulatorSink{}
	a := newReplyAccumulator(nil, sink.yield, nil)
	content := Delta{Kind: DeltaContent, Content: "hello"}
	block := Delta{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(`{}`)}

	if a.text(Delta{Kind: DeltaContent}, 0) {
		t.Error("text with n == 0 ended the stream")
	}
	if len(sink.deltas) != 0 {
		t.Fatalf("text with n == 0 yielded %s", dumpDeltas(sink.deltas))
	}
	if a.text(content, len(content.Content)) {
		t.Error("text under the cap ended the stream")
	}
	if a.emit(block) {
		t.Error("emit ended the stream")
	}
	// emit charged nothing: only the five content bytes stand against the cap.
	if a.charge(maxReplyTextBytes - len(content.Content)) {
		t.Error("emit was charged against the text cap")
	}

	requireDeltas(t, sink, []Delta{content, block})
}

// TestReplyAccumulator_MalformedOnly pins the malformed-only check: a stream that charged no
// text and opened no tool call faults on its skipped chunks before the epilogue runs, while
// charged bytes or an open call let the reply end cleanly with the count on its Done.
func TestReplyAccumulator_MalformedOnly(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		// carry is what the stream carried besides its two malformed chunks.
		carry        func(a *replyAccumulator)
		wantEpilogue bool
		want         []Delta
	}{
		{
			name:  "nothing else",
			carry: func(*replyAccumulator) {},
			want:  []Delta{terminalMalformedOnly(2)},
		},
		{
			name:         "charged bytes that yielded nothing",
			carry:        func(a *replyAccumulator) { a.charge(1) },
			wantEpilogue: true,
			want:         []Delta{{Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 2}},
		},
		{
			name:         "an open tool call",
			carry:        func(a *replyAccumulator) { a.foldCall(accumulatorCall(0, "call_1", "read", "{}")) },
			wantEpilogue: true,
			want: []Delta{
				terminalCall("call_1", "read", "{}"),
				{Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 2},
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			sink := &accumulatorSink{}
			ranEpilogue := false
			a := newReplyAccumulator(nil, sink.yield, func() bool {
				ranEpilogue = true
				return true
			})
			a.skipMalformed()
			a.skipMalformed()
			row.carry(a)

			a.finish()

			requireDeltas(t, sink, row.want)
			if ranEpilogue != row.wantEpilogue {
				t.Errorf("epilogue ran = %v, want %v", ranEpilogue, row.wantEpilogue)
			}
		})
	}
}

// TestReplyAccumulator_EpilogueRunsBeforeTheFlush pins the epilogue's place: what it emits
// lands before the tool calls, and patchOpenCalls rewrites their arguments before the flush —
// on finish and on a bare EOF inside run alike.
func TestReplyAccumulator_EpilogueRunsBeforeTheFlush(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		end  func(a *replyAccumulator)
	}{
		{name: "finish", end: func(a *replyAccumulator) { a.finish() }},
		{
			name: "bare EOF inside run",
			end: func(a *replyAccumulator) {
				a.run(strings.NewReader(""), func(string) bool { return false })
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			sink := &accumulatorSink{}
			layout := Delta{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(`{"layout":true}`)}
			var a *replyAccumulator
			a = newReplyAccumulator(nil, sink.yield, func() bool {
				a.patchOpenCalls(func(args string) string {
					if args == "" {
						return "{}"
					}
					return args
				})
				return !a.emit(layout)
			})
			a.foldCall(accumulatorCall(0, "call_empty", "list", ""))
			a.foldCall(accumulatorCall(1, "call_full", "read", `{"path":"a"}`))

			row.end(a)

			requireDeltas(t, sink, []Delta{
				layout,
				terminalCall("call_empty", "list", "{}"),
				terminalCall("call_full", "read", `{"path":"a"}`),
				{Kind: DeltaDone, FinishReason: "stop"},
			})
		})
	}
}

// TestReplyAccumulator_EpilogueStopsBeforeDone pins an epilogue that reports the consumer
// gone: no tool call and no Done follow it, and the stream has ended.
func TestReplyAccumulator_EpilogueStopsBeforeDone(t *testing.T) {
	t.Parallel()

	sink := &accumulatorSink{}
	a := newReplyAccumulator(nil, sink.yield, func() bool { return false })
	a.foldCall(accumulatorCall(0, "call_1", "read", "{}"))

	a.finish()

	requireDeltas(t, sink, nil)
	if !a.emit(Delta{Kind: DeltaContent, Content: "late"}) {
		t.Error("emit after a stopped epilogue reported ended = false")
	}
	requireDeltas(t, sink, nil)
}

// TestReplyAccumulator_DoneCarriesWhatWasSet pins the terminal Done: the finish reason
// defaults to stop, and each setter overwrites what was set before.
func TestReplyAccumulator_DoneCarriesWhatWasSet(t *testing.T) {
	t.Parallel()

	last := Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}
	rows := []struct {
		name string
		set  func(a *replyAccumulator)
		want Delta
	}{
		{
			name: "nothing set",
			set:  func(*replyAccumulator) {},
			want: Delta{Kind: DeltaDone, FinishReason: "stop"},
		},
		{
			name: "set once",
			set: func(a *replyAccumulator) {
				a.setFinish("length")
				a.setUsage(last)
				a.setModel("model-a")
			},
			want: Delta{Kind: DeltaDone, FinishReason: "length", Usage: &last, Model: "model-a"},
		},
		{
			name: "set twice",
			set: func(a *replyAccumulator) {
				a.setFinish("length")
				a.setUsage(Usage{PromptTokens: 1})
				a.setModel("model-a")
				a.setFinish("tool_calls")
				a.setUsage(last)
				a.setModel("model-b")
			},
			want: Delta{Kind: DeltaDone, FinishReason: "tool_calls", Usage: &last, Model: "model-b"},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			sink := &accumulatorSink{}
			a := newReplyAccumulator(nil, sink.yield, nil)
			row.set(a)

			a.finish()

			requireDeltas(t, sink, []Delta{row.want})
		})
	}
}
