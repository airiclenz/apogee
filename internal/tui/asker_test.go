package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestAskerReturnsUIAnswer proves the ask-user rendezvous returns whatever the UI typed,
// delivered from another goroutine (the Update loop) — the free-text analogue of the
// Approval rendezvous (P3.11).
func TestAskerReturnsUIAnswer(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	prog.answerWith("teal")
	ref := &programRef{}
	ref.bind(prog)
	asker := &uiAsker{prog: ref}

	got, err := asker.Ask(context.Background(), domain.AskRequest{Question: "what colour?"})
	if err != nil {
		t.Fatalf("Ask: unexpected error %v", err)
	}
	if got.Text != "teal" {
		t.Errorf("answer = %q; want %q", got.Text, "teal")
	}
	prog.wait()
}

// TestAskerCancelledCtxReturnsNoLeak proves a user stop while a question is pending returns
// promptly (no deadlock) with an empty answer + ctx.Err(), and the reply channel handed to
// the UI is buffered so a late answer is absorbed (no goroutine leak) — fail-safe by design.
func TestAskerCancelledCtxReturnsNoLeak(t *testing.T) {
	t.Parallel()
	prog := newStubProgram() // no answer hook → the UI never replies
	ref := &programRef{}
	ref.bind(prog)
	asker := &uiAsker{prog: ref}

	ctx, cancel := context.WithCancel(context.Background())

	type outcome struct {
		a   domain.AskAnswer
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		a, err := asker.Ask(ctx, domain.AskRequest{Question: "q?"})
		done <- outcome{a, err}
	}()

	cancel()

	select {
	case got := <-done:
		if got.a.Text != "" {
			t.Errorf("answer = %q; want empty on cancel", got.a.Text)
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("err = %v; want context.Canceled", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not return after ctx cancel (deadlock)")
	}

	msgs := prog.messages()
	if len(msgs) != 1 {
		t.Fatalf("captured %d msgs; want exactly 1 askReqMsg", len(msgs))
	}
	req, ok := msgs[0].(askReqMsg)
	if !ok {
		t.Fatalf("captured msg is %T; want askReqMsg", msgs[0])
	}
	select {
	case req.Reply <- domain.AskAnswer{Text: "late"}:
	default:
		t.Error("reply channel was not buffered; a late UI answer would block (goroutine leak)")
	}
}

// TestAskerUnboundReturnsOnCancel proves the asker does not hang when no program is bound
// (the no-op send): a cancelled ctx still unblocks it cleanly (headless fail-safe).
func TestAskerUnboundReturnsOnCancel(t *testing.T) {
	t.Parallel()
	asker := &uiAsker{prog: &programRef{}} // never bound

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := asker.Ask(ctx, domain.AskRequest{})
	if got.Text != "" {
		t.Errorf("answer = %q; want empty", got.Text)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}
