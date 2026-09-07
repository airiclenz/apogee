package eventjson

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// fixedClock is the stamp every test pins `time` to, so a whole line is comparable as a string.
// RFC3339Nano drops trailing zeros, so this renders as "2026-09-07T12:00:00Z".
var fixedClock = func() time.Time { return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC) }

// recordingSink is the inner sink a Writer wraps: it keeps every Event it was handed, in order.
type recordingSink struct{ events []domain.Event }

func (r *recordingSink) Emit(ev domain.Event) { r.events = append(r.events, ev) }

// failingWriter refuses every write, which is what a closed pipe looks like from inside Emit.
type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// lines splits what the Writer produced into its lines, asserting the stream is newline-terminated
// JSONL rather than one blob.
func lines(t *testing.T, out *bytes.Buffer) []string {
	t.Helper()

	text := out.String()
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatalf("stream does not end in a newline: %q", text)
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// TestWriterEnvelopeOrderAndNulls pins whole lines, byte for byte: the nine envelope members in
// their contract order, the numbers a line carries for an Event, and the nulls a frame carries in
// their place. It also pins both frame objects, which are this package's own contract and have no
// domain type behind them to derive from.
func TestWriterEnvelopeOrderAndNulls(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := New(&out, Options{Session: "sess-1", Now: fixedClock})

	w.Emit(domain.TokenEvent{EventBase: domain.EventBase{Turn: 3}, Text: "hel"})
	w.Emit(domain.TokenEvent{EventBase: domain.EventBase{Turn: 4, Depth: 1, CallID: "call-9"}, Text: "lo"})
	w.RunStarted(RunStarted{
		Session:   "sess-1",
		Workspace: "/w",
		Model:     "gpt-oss-20b",
		Server:    "http://host.internal:1111",
		Mode:      "auto",
		Confined:  true,
		Version:   "0.20.9",
	})
	w.RunFinished(RunFinished{ExitCode: 0, Turns: 2, Saved: true, FinalText: "done"})

	want := []string{
		`{"event":"token","v":1,"seq":1,"time":"2026-09-07T12:00:00Z","session":"sess-1","turn":3,"depth":0,"call_id":null,"data":{"text":"hel"}}`,
		`{"event":"token","v":1,"seq":2,"time":"2026-09-07T12:00:00Z","session":"sess-1","turn":4,"depth":1,"call_id":"call-9","data":{"text":"lo"}}`,
		`{"event":"run_started","v":1,"seq":3,"time":"2026-09-07T12:00:00Z","session":"sess-1","turn":null,"depth":null,"call_id":null,` +
			`"data":{"session":"sess-1","workspace":"/w","model":"gpt-oss-20b","server":"http://host.internal:1111","mode":"auto","bypass":false,"confined":true,"version":"0.20.9"}}`,
		`{"event":"run_finished","v":1,"seq":4,"time":"2026-09-07T12:00:00Z","session":"sess-1","turn":null,"depth":null,"call_id":null,` +
			`"data":{"exit_code":0,"turns":2,"denied":0,"faulted":false,"fault":"","error":null,"title":"","final_text":"done","wrote":null,` +
			`"context_files":{"files":null,"standing_tokens":0,"system_share":0},"undo_note":"","saved":true,` +
			`"usage":{"calls":0,"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"cached_prompt_tokens":0},"sub_agents":null}}`,
	}

	got := lines(t, &out)
	if len(got) != len(want) {
		t.Fatalf("wrote %d lines, want %d:\n%s", len(got), len(want), out.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %s\nwant %s", i+1, got[i], want[i])
		}
	}
}

// TestWriterSeqCountsFramesAndSkipsWire holds the two halves of the sequence rule together: seq
// starts at 1 and both frames consume one, while a WireEvent consumes none — so a gap in seq means
// a LOST line and never an Inspector event a consumer was not meant to see.
func TestWriterSeqCountsFramesAndSkipsWire(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := New(&out, Options{Now: fixedClock})

	w.RunStarted(RunStarted{})
	w.Emit(domain.TokenEvent{Text: "a"})
	w.Emit(domain.WireEvent{Direction: "request", Payload: `{"wire-only":true}`})
	w.Emit(domain.MessageEvent{Text: "a"})
	w.RunFinished(RunFinished{})

	got := lines(t, &out)
	wantKinds := []string{"run_started", "token", "message", "run_finished"}
	if len(got) != len(wantKinds) {
		t.Fatalf("wrote %d lines, want %d:\n%s", len(got), len(wantKinds), out.String())
	}
	for i, kind := range wantKinds {
		wantPrefix := `{"event":"` + kind + `","v":1,"seq":` + strconv.Itoa(i+1) + `,`
		if !strings.HasPrefix(got[i], wantPrefix) {
			t.Errorf("line %d: got %s\nwant prefix %s", i+1, got[i], wantPrefix)
		}
	}
	if strings.Contains(out.String(), "wire-only") {
		t.Errorf("the WireEvent payload reached the stream:\n%s", out.String())
	}
}

// TestWriterForwardsEveryEventToInner pins the decorator half of the contract: the Writer is a
// pass-through for the sink it displaced, so installing it never costs a Driver an observer — the
// excluded WireEvent included, since the TUI's Inspector is exactly such an observer.
func TestWriterForwardsEveryEventToInner(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	inner := &recordingSink{}
	w := New(&out, Options{Now: fixedClock})
	sink := w.Wrap(inner)

	emitted := []domain.Event{
		domain.TokenEvent{Text: "a"},
		domain.WireEvent{Direction: "response", Payload: "{}"},
		domain.TurnEvent{Status: "ok"},
	}
	for _, ev := range emitted {
		sink.Emit(ev)
	}

	if len(inner.events) != len(emitted) {
		t.Fatalf("inner sink saw %d events, want %d", len(inner.events), len(emitted))
	}
	for i := range emitted {
		if inner.events[i] != emitted[i] {
			t.Errorf("event %d: inner sink saw %#v, want %#v", i, inner.events[i], emitted[i])
		}
	}
}

// TestWriterStopsAfterFirstWriteErrorAndReportsOnce pins the one boundary losslessness has: a
// closed pipe costs the Driver exactly one report, silences the stream for good, and leaves the run
// — and every other observer — running.
func TestWriterStopsAfterFirstWriteErrorAndReportsOnce(t *testing.T) {
	t.Parallel()

	broken := errors.New("broken pipe")
	var reported []error
	inner := &recordingSink{}
	w := New(failingWriter{err: broken}, Options{
		Now:    fixedClock,
		Report: func(err error) { reported = append(reported, err) },
	})
	sink := w.Wrap(inner)

	sink.Emit(domain.TokenEvent{Text: "a"})
	sink.Emit(domain.TokenEvent{Text: "b"})
	w.RunFinished(RunFinished{ExitCode: 1})

	if len(reported) != 1 {
		t.Fatalf("Report called %d times, want exactly 1: %v", len(reported), reported)
	}
	if !errors.Is(reported[0], broken) {
		t.Errorf("reported %v, want the writer's own error %v", reported[0], broken)
	}
	if len(inner.events) != 2 {
		t.Errorf("inner sink saw %d events, want 2 — forwarding must survive the stop", len(inner.events))
	}
}

// TestWriterSessionNullUntilSet pins when a line acquires an identity: null before the Driver mints
// the id, the id from the next line on, and no back-dating of the lines already written.
func TestWriterSessionNullUntilSet(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := New(&out, Options{Now: fixedClock})

	w.Emit(domain.TokenEvent{Text: "a"})
	w.SetSession("sess-2")
	w.Emit(domain.TokenEvent{Text: "b"})

	got := lines(t, &out)
	if len(got) != 2 {
		t.Fatalf("wrote %d lines, want 2:\n%s", len(got), out.String())
	}
	if !strings.Contains(got[0], `"session":null,`) {
		t.Errorf("line 1 carries a session before one was set: %s", got[0])
	}
	if !strings.Contains(got[1], `"session":"sess-2",`) {
		t.Errorf("line 2 does not carry the session that was set: %s", got[1])
	}
}
