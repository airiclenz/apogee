package tui

import (
	"context"
	"errors"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The interjection mailbox and its between-Steps drain (ADR 0025)
// ----------------------------------------------------------------------------

// recorder captures the ordering the delivery contract is about: which Steps ran, and where the
// delivery report landed between them. The worker drives synchronously on the calling goroutine
// in these tests, so a plain slice is enough.
type recorder struct {
	order []string
	msgs  []tea.Msg
}

// notify is the seam the worker sends its per-Turn and delivery Msgs through.
func (r *recorder) notify(msg tea.Msg) {
	r.msgs = append(r.msgs, msg)
	if _, ok := msg.(interjectedMsg); ok {
		r.order = append(r.order, "interjected")
	}
}

// delivered returns the rows carried by the single interjectedMsg the worker sent, and whether it
// sent one at all — the distinction between "delivered nothing" and "reported nothing".
func (r *recorder) delivered(t *testing.T) ([]queuedInterjection, bool) {
	t.Helper()
	var got []queuedInterjection
	var seen bool
	for _, msg := range r.msgs {
		if im, ok := msg.(interjectedMsg); ok {
			if seen {
				t.Fatalf("interjectedMsg sent more than once; want one per drained boundary")
			}
			got, seen = im.items, true
		}
	}
	return got, seen
}

// staged builds a queued row the way the staging half will: the raw editor text kept verbatim
// beside the parsed input the engine consumes.
func staged(id int, text string) queuedInterjection {
	return queuedInterjection{id: id, raw: text, input: domain.UserInput{Text: text}}
}

// TestWorkerDrainsBoxBetweenSteps is the delivery contract: rows staged while a Turn runs are
// committed at the NEXT between-Steps boundary — after the Turn that was in flight, before the
// Step that carries them upstream — in the order they were typed, and reported once.
func TestWorkerDrainsBoxBetweenSteps(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	rec := &recorder{}
	eng := &fakeEngine{}
	eng.stepFn = func(_ context.Context, call int) (domain.StepResult, error) {
		rec.order = append(rec.order, "step")
		if call == 0 {
			// The human types two remarks while the first Turn runs.
			box.push(staged(1, "also check the tests"))
			box.push(staged(2, "and the docs"))
			return domain.StepResult{Status: domain.StatusTurnComplete}, nil
		}
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	texts := make([]string, 0, 2)
	for _, in := range eng.interjections() {
		texts = append(texts, in.Text)
	}
	if len(texts) != 2 || texts[0] != "also check the tests" || texts[1] != "and the docs" {
		t.Fatalf("delivered texts = %q; want both remarks, oldest first", texts)
	}
	// The report rides between the Turn that was running and the Step that carries the remarks:
	// the Update loop moves the rows into the transcript at the moment the model saw them.
	want := []string{"step", "interjected", "step"}
	if len(rec.order) != len(want) {
		t.Fatalf("order = %v; want %v", rec.order, want)
	}
	for i := range want {
		if rec.order[i] != want[i] {
			t.Fatalf("order = %v; want %v", rec.order, want)
		}
	}
	got, seen := rec.delivered(t)
	if !seen {
		t.Fatal("no interjectedMsg sent; the delivered rows must be reported")
	}
	if len(got) != 2 || got[0].id != 1 || got[1].id != 2 {
		t.Errorf("reported rows = %+v; want ids 1,2 in delivery order", got)
	}
	if got[0].raw != "also check the tests" {
		t.Errorf("reported raw = %q; want the verbatim editor text", got[0].raw)
	}
}

// TestWorkerEmptyBoxDeliversNothing pins the quiet path: with nothing staged the worker neither
// touches the engine's Interject nor reports a delivery — an Exchange nobody interjected into is
// byte-for-byte the pre-interjection drive. A nil box (the /compact worker, which drives no
// Exchange) behaves identically rather than panicking.
func TestWorkerEmptyBoxDeliversNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		box  *interjectBox
	}{
		{name: "empty box", box: newInterjectBox()},
		{name: "no box", box: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			eng := &fakeEngine{
				stepFn: scriptedSteps(
					stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete}},
					stepResult{res: domain.StepResult{Status: domain.StatusExchangeComplete}},
				),
			}

			msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, tc.box, rec.notify)

			if _, ok := msg.(exchangeDoneMsg); !ok {
				t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
			}
			if got := eng.interjections(); len(got) != 0 {
				t.Errorf("Interject calls = %d; want none", len(got))
			}
			if _, seen := rec.delivered(t); seen {
				t.Error("interjectedMsg sent for an empty drain; want no report at all")
			}
		})
	}
}

// TestWorkerInterjectErrorHoldsRemainder proves the honest degradation: a refused delivery stops
// the drain where it failed and the report names only what actually landed — nothing. The rows are
// not lost, because the report is what the Model reconciles against; a row missing from it stays on
// the display queue and goes out at the terminal flush.
func TestWorkerInterjectErrorHoldsRemainder(t *testing.T) {
	t.Parallel()
	box := newInterjectBox()
	rec := &recorder{}
	refused := errors.New("no open exchange")
	eng := &fakeEngine{interjectFn: func(domain.UserInput) error { return refused }}
	eng.stepFn = func(_ context.Context, call int) (domain.StepResult, error) {
		if call == 0 {
			box.push(staged(1, "first"))
			box.push(staged(2, "second"))
			return domain.StepResult{Status: domain.StatusTurnComplete}, nil
		}
		return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "go"}, box, rec.notify)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg (a refused interjection never fails the Exchange)", msg)
	}
	attempts := eng.interjections()
	if len(attempts) != 1 || attempts[0].Text != "first" {
		t.Fatalf("Interject attempts = %+v; want the drain to stop at the first refusal", attempts)
	}
	got, seen := rec.delivered(t)
	if !seen {
		t.Fatal("no interjectedMsg sent; a drain that delivered nothing must still report, so the rows stay held")
	}
	if len(got) != 0 {
		t.Errorf("reported rows = %+v; want none — nothing was committed", got)
	}
}

// TestInterjectBoxRaceClean drives the one place the Update and worker goroutines share state:
// pushes racing drains. Under -race it proves the mutex covers both sides, and the accounting
// proves the box neither loses a row nor hands one out twice.
func TestInterjectBoxRaceClean(t *testing.T) {
	t.Parallel()
	const (
		pushers = 8
		perGo   = 50
		drains  = 4
	)
	box := newInterjectBox()

	var mu sync.Mutex
	seen := map[int]int{}
	collect := func(items []queuedInterjection) {
		mu.Lock()
		defer mu.Unlock()
		for _, it := range items {
			seen[it.id]++
		}
	}

	var wg sync.WaitGroup
	for p := range pushers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGo {
				box.push(staged(p*perGo+i, "row"))
			}
		}()
	}
	for range drains {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGo {
				collect(box.drainAll())
			}
		}()
	}
	wg.Wait()
	collect(box.drainAll()) // whatever the racing drains left behind

	if len(seen) != pushers*perGo {
		t.Fatalf("drained %d distinct rows; want every pushed row (%d)", len(seen), pushers*perGo)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %d drained %d times; want exactly once", id, n)
		}
	}
}
