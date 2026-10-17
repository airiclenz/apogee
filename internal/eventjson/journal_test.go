package eventjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/testtime"
)

// updateJournalGolden rewrites testdata/journal.jsonl from the journal the golden test builds,
// so a deliberate change to the coalesced variant is one command, never a hand edit.
var updateJournalGolden = flag.Bool("update", false, "rewrite internal/eventjson/testdata/journal.jsonl")

// journalGoldenPath is the coalesced variant's own golden (ADR 0103 decision 2).
const journalGoldenPath = "testdata/journal.jsonl"

// stretchedWindow is long enough that no timer fires while a test runs, so every flush in it is
// event-driven and the lines it produces are deterministic.
var stretchedWindow = testtime.Fixed(time.Hour)

// lockedBuffer is an io.Writer a timer goroutine may write while the test goroutine reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// journalOptions are the options every event-driven test opens a journal with.
func journalOptions() JournalOptions {
	return JournalOptions{
		Session:   "s-1",
		Title:     "fix the parser",
		Workspace: "/work/project",
		Entries:   3,
		Now:       fixedClock,
		Window:    stretchedWindow,
	}
}

// decodeLines decodes every line of a journal, failing the test on any it cannot read.
func decodeLines(t *testing.T, out *bytes.Buffer) []Line {
	t.Helper()

	raw := lines(t, out)
	decoded := make([]Line, 0, len(raw))
	for _, line := range raw {
		got, err := Decode([]byte(line))
		if err != nil {
			t.Fatalf("Decode(%s): %v", line, err)
		}
		decoded = append(decoded, got)
	}
	return decoded
}

// kindsOf lists the kinds of decoded lines, in order.
func kindsOf(decoded []Line) []string {
	kinds := make([]string, 0, len(decoded))
	for _, line := range decoded {
		kinds = append(kinds, line.Kind)
	}
	return kinds
}

// deltaText is the text a decoded token or reasoning line carries, and "" for any other line.
func deltaText(line Line) string {
	switch event := line.Event.(type) {
	case domain.TokenEvent:
		return event.Text
	case domain.ReasoningEvent:
		return event.Text
	}
	return ""
}

// depthZero and childRun are the two streams the tests interleave: the top-level agent's Turn 1,
// and a delegated run at depth 1.
var (
	depthZero = domain.EventBase{Turn: 1}
	childRun  = domain.EventBase{Turn: 1, Depth: 1, CallID: "call-1", RunID: "run-1"}
)

func TestJournalCoalescesDeltasIntoTheirConcatenation(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	journal := NewJournal(&out, journalOptions())
	for _, text := range []string{"Hel", "lo", "", " wor", "ld"} {
		journal.Emit(domain.TokenEvent{EventBase: depthZero, Text: text})
	}
	journal.Close()

	decoded := decodeLines(t, &out)
	if got, want := kindsOf(decoded), []string{kindRunStarted, kindToken, kindRunFinished}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	if got := deltaText(decoded[1]); got != "Hello world" {
		t.Errorf("merged text = %q, want the concatenation %q", got, "Hello world")
	}
}

func TestJournalWritesThePendingDeltaBeforeAnyOtherLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		emit  func(*Journal)
		kinds []string
	}{
		{
			name: "a non-delta event",
			emit: func(j *Journal) {
				j.Emit(domain.TokenEvent{EventBase: depthZero, Text: "said"})
				j.Emit(domain.MessageEvent{EventBase: depthZero, Text: "said"})
			},
			kinds: []string{kindRunStarted, kindToken, kindMessage, kindRunFinished},
		},
		{
			name: "a change of kind",
			emit: func(j *Journal) {
				j.Emit(domain.ReasoningEvent{EventBase: depthZero, Text: "thinking"})
				j.Emit(domain.TokenEvent{EventBase: depthZero, Text: "answer"})
			},
			kinds: []string{kindRunStarted, kindReasoning, kindToken, kindRunFinished},
		},
		{
			name: "a change of run",
			emit: func(j *Journal) {
				j.Emit(domain.TokenEvent{EventBase: depthZero, Text: "parent"})
				j.Emit(domain.TokenEvent{EventBase: childRun, Text: "child"})
			},
			kinds: []string{kindRunStarted, kindToken, kindToken, kindRunFinished},
		},
		{
			name: "a change of turn",
			emit: func(j *Journal) {
				j.Emit(domain.TokenEvent{EventBase: depthZero, Text: "first"})
				j.Emit(domain.TokenEvent{EventBase: domain.EventBase{Turn: 2}, Text: "second"})
			},
			kinds: []string{kindRunStarted, kindToken, kindToken, kindRunFinished},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			journal := NewJournal(&out, journalOptions())
			tt.emit(journal)
			journal.Close()

			if got := kindsOf(decodeLines(t, &out)); !reflect.DeepEqual(got, tt.kinds) {
				t.Errorf("kinds = %v, want %v", got, tt.kinds)
			}
		})
	}
}

func TestJournalTimerWritesAQuietStream(t *testing.T) {
	t.Parallel()

	var out lockedBuffer
	options := journalOptions()
	options.Window = testtime.Fixed(time.Millisecond)
	journal := NewJournal(&out, options)
	journal.Emit(domain.TokenEvent{EventBase: depthZero, Text: "quiet"})

	testtime.Eventually(t, testtime.Budget(5*time.Second), func() bool {
		return strings.Contains(out.String(), `"event":"token"`)
	}, "the window's timer to write the pending token line")

	journal.Close()
	if got := strings.Count(out.String(), `"text":"quiet"`); got != 1 {
		t.Errorf("the quiet token was written %d times, want exactly once", got)
	}
}

func TestJournalForwardsEveryEventAtOnceAndUnmerged(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	inner := &recordingSink{}
	journal := NewJournal(&out, journalOptions())
	sink := journal.Wrap(inner)

	emitted := []domain.Event{
		domain.TokenEvent{EventBase: depthZero, Text: "a"},
		domain.TokenEvent{EventBase: depthZero, Text: "b"},
		domain.ReasoningEvent{EventBase: depthZero, Text: "c"},
		domain.TokenEvent{EventBase: depthZero, Text: "d"},
	}
	for _, event := range emitted {
		sink.Emit(event)
	}

	if !reflect.DeepEqual(inner.events, emitted) {
		t.Errorf("inner sink saw %v, want every Event as emitted %v", inner.events, emitted)
	}
	if got, want := kindsOf(decodeLines(t, &out)), []string{kindRunStarted, kindToken, kindReasoning}; !reflect.DeepEqual(got, want) {
		t.Errorf("journal kinds while %q is pending = %v, want %v", "d", got, want)
	}
}

func TestJournalStopsAtTheFirstWriteError(t *testing.T) {
	t.Parallel()

	var reports []error
	refused := errors.New("disk full")
	options := journalOptions()
	options.Report = func(err error) { reports = append(reports, err) }
	inner := &recordingSink{}
	journal := NewJournal(failingWriter{err: refused}, options)
	sink := journal.Wrap(inner)

	sink.Emit(domain.TokenEvent{EventBase: depthZero, Text: "lost"})
	sink.Emit(domain.MessageEvent{EventBase: depthZero, Text: "lost"})
	journal.Prompt("next", false)
	journal.Close()

	if len(reports) != 1 || !errors.Is(reports[0], refused) {
		t.Errorf("reports = %v, want exactly one report of %v", reports, refused)
	}
	if len(inner.events) != 2 {
		t.Errorf("inner sink saw %d events, want both forwarded despite the stopped journal", len(inner.events))
	}
}

func TestJournalFrames(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	journal := NewJournal(&out, journalOptions())
	journal.Close()
	raw := lines(t, &out)
	if len(raw) != 2 {
		t.Fatalf("got %d lines, want the two frames: %v", len(raw), raw)
	}

	tests := []struct {
		name string
		line string
		data string
	}{
		{
			name: "run_started carries the record's identity and entry count",
			line: raw[0],
			data: `{"session":"s-1","workspace":"/work/project","title":"fix the parser","entries":3}`,
		},
		{
			name: "run_finished is an empty object",
			line: raw[1],
			data: `{}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var envelope struct {
				Session *string         `json:"session"`
				Turn    *int            `json:"turn"`
				Depth   *int            `json:"depth"`
				Data    json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal([]byte(tt.line), &envelope); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.line, err)
			}
			if string(envelope.Data) != tt.data {
				t.Errorf("data = %s, want %s", envelope.Data, tt.data)
			}
			if envelope.Session == nil || *envelope.Session != "s-1" {
				t.Errorf("session = %v, want the journal's session stamped on the frame", envelope.Session)
			}
			if envelope.Turn != nil || envelope.Depth != nil {
				t.Errorf("turn, depth = %v, %v, want null on a frame", envelope.Turn, envelope.Depth)
			}
		})
	}
}

func TestJournalPromptWritesThePendingDeltaFirst(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	journal := NewJournal(&out, journalOptions())
	journal.Emit(domain.TokenEvent{EventBase: depthZero, Text: "partial"})
	journal.Prompt("also check the tests", true)
	journal.Close()

	decoded := decodeLines(t, &out)
	if got, want := kindsOf(decoded), []string{kindRunStarted, kindToken, kindPrompt, kindRunFinished}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	if got, want := *decoded[2].Prompt, (Prompt{Text: "also check the tests", Interjection: true}); got != want {
		t.Errorf("prompt = %+v, want %+v", got, want)
	}
}

func TestJournalWritesNothingAfterClose(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	inner := &recordingSink{}
	journal := NewJournal(&out, journalOptions())
	sink := journal.Wrap(inner)
	journal.Close()
	sink.Emit(domain.MessageEvent{EventBase: depthZero, Text: "late"})
	journal.Prompt("late", false)
	journal.Close()

	if got, want := kindsOf(decodeLines(t, &out)), []string{kindRunStarted, kindRunFinished}; !reflect.DeepEqual(got, want) {
		t.Errorf("kinds = %v, want only the two frames", got)
	}
	if len(inner.events) != 1 {
		t.Errorf("inner sink saw %d events, want the late one still forwarded", len(inner.events))
	}
}

// journalStream is the stream the golden and the decode comparison share: two streams of
// reasoning and tokens interleaved with a delegation, and every boundary that writes a pending
// delta — a non-delta event, a change of kind, run and Turn, a Prompt and Close.
func journalStream() []domain.Event {
	return []domain.Event{
		domain.ReasoningEvent{EventBase: depthZero, Text: "Look at "},
		domain.ReasoningEvent{EventBase: depthZero, Text: "the parser."},
		domain.TokenEvent{EventBase: depthZero, Text: "Delegat"},
		domain.TokenEvent{EventBase: depthZero, Text: "ing."},
		domain.MessageEvent{EventBase: depthZero, Text: "Delegating."},
		domain.TokenEvent{EventBase: childRun, Text: "Child "},
		domain.TokenEvent{EventBase: childRun, Text: "reads."},
		domain.TokenEvent{EventBase: depthZero, Text: "Parent "},
		domain.TokenEvent{EventBase: depthZero, Text: "waits."},
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 2}, Text: "Do"},
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 2}, Text: "ne."},
	}
}

// writeJournalStream records journalStream with a Prompt opening it and an Interjection landing
// while a delta is pending.
func writeJournalStream(out *bytes.Buffer) {
	journal := NewJournal(out, journalOptions())
	journal.Prompt("fix the parser", false)
	for i, event := range journalStream() {
		if i == 8 {
			journal.Prompt("and its tests", true)
		}
		journal.Emit(event)
	}
	journal.Close()
}

func TestJournalGolden(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	writeJournalStream(&out)

	if *updateJournalGolden {
		if err := os.MkdirAll(filepath.Dir(journalGoldenPath), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(journalGoldenPath, out.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(journalGoldenPath)
	if err != nil {
		t.Fatalf("read golden (create it with `go test ./internal/eventjson -run TestJournalGolden -update`): %v", err)
	}
	if got := out.String(); got != string(want) {
		t.Errorf("journal differs from %s (update it deliberately with -update)\n got:\n%s\nwant:\n%s",
			journalGoldenPath, got, want)
	}
}

func TestJournalDecodesToTheUncoalescedText(t *testing.T) {
	t.Parallel()

	var uncoalesced bytes.Buffer
	plain := New(&uncoalesced, Options{Now: fixedClock})
	for _, event := range journalStream() {
		plain.Emit(event)
	}
	var journaled bytes.Buffer
	journal := NewJournal(&journaled, journalOptions())
	for _, event := range journalStream() {
		journal.Emit(event)
	}
	journal.Close()

	if got, want := textByStream(decodeLines(t, &journaled)), textByStream(decodeLines(t, &uncoalesced)); !reflect.DeepEqual(got, want) {
		t.Errorf("journal text by stream = %v, want the uncoalesced stream's %v", got, want)
	}
}

// deltaStream names one stream of deltas: the kind and the EventBase they merge under.
type deltaStream struct {
	kind string
	base domain.EventBase
}

// textByStream concatenates the delta text of decoded lines per stream, and lists every other
// Event's kind in order under the zero stream, so two decodings compare as the same content.
func textByStream(decoded []Line) map[deltaStream]string {
	text := map[deltaStream]string{}
	for _, line := range decoded {
		if line.Event == nil {
			continue
		}
		base := domain.EventBase{Turn: line.Turn, Depth: line.Depth, CallID: line.CallID, RunID: line.RunID}
		if line.Kind == kindToken || line.Kind == kindReasoning {
			text[deltaStream{kind: line.Kind, base: base}] += deltaText(line)
			continue
		}
		text[deltaStream{}] += line.Kind + ";"
	}
	return text
}
