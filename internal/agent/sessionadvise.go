package agent

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// The session-start advise lane (ADR 0076 D6, 2026-10-19 amendment): ADR 0076's non-tool advise
// slot for the session-started notice. At every top-level Session boundary the agent starts each
// armed advise-class Reaction on session-started in the BACKGROUND, under its deadline (D7); the
// first request that carries a new user message — the opening of the session's next Exchange —
// joins them, waiting only for what is left of each entry's deadline, and fences their answers, in
// ladder order, ahead of the user's text in that one message (Conversation.AdviseUserMessage). It
// is fail-open: a failure, a panic or a missed deadline adds nothing and is reported at the join.
//
// Three rules keep it off every caller that cannot wait. The boundary only STARTS the runs — it
// never waits, emits or reports, because every boundary a TUI crosses runs on the Bubble Tea
// Update goroutine. Everything the Driver can observe — the advice, the ReactionFiredEvent it is
// booked under, a failure's report — happens at the join, on the loop goroutine, so a result a
// later boundary discards is never booked. And the join is ctx-aware, so a cancel (Esc) ends the
// wait and leaves the runs for the next opening.
//
// The lane dispatches by handler KIND itself rather than through adviseUser, whose default branch
// refuses everything but a command and a webhook: an engine-origin Go handler (SessionStartedFunc,
// the shape the bench arms) is called directly, and a command or webhook goes through the sync
// lane's executor (runSyncArgv / runSyncWebhook) and the secret redaction adviseUser applies, so
// admitting the user's `advise:` key on session-started stays a validation change only.

// sessionAdviseLane is one Session boundary's background advise runs: the ones still waiting for
// the session's next opening, and the ones taken into the open Exchange. The zero value is an
// empty lane. mu guards it: a boundary starts runs from the Driver's goroutine, the join takes them
// on the loop goroutine, and Close cancels from wherever it is called.
type sessionAdviseLane struct {
	mu sync.Mutex
	// cancel ends the current boundary's runs: the next boundary and Close call it.
	cancel context.CancelFunc
	// pending is the runs no opening has taken yet, in ladder order.
	pending []*sessionAdviseRun
	// delivered is the runs taken into the open Exchange that answered: an abort puts them back
	// ahead of pending (restoreDelivered), a close that keeps the opening forgets them
	// (forgetDelivered). A run the join reported — timed out, errored or panicked — is never here:
	// its report is its one outcome, so no later opening reports or attaches it again.
	delivered []*sessionAdviseRun
}

// sessionAdviseRun is one entry's background run: the reaction, the instant its deadline ends,
// and — once done is closed — what the handler answered.
type sessionAdviseRun struct {
	spec     domain.Reaction
	deadline time.Time
	timeout  time.Duration
	done     chan struct{}
	// out, err and panicked are written by the run's goroutine before done closes, and read only
	// after it has.
	out      domain.Outcome
	err      error
	panicked any
}

// replace installs a boundary's runs and the cancel that ends them, cancelling the previous
// boundary's first: its unattached results are discarded with it.
func (l *sessionAdviseLane) replace(cancel context.CancelFunc, runs []*sessionAdviseRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
	l.cancel, l.pending, l.delivered = cancel, runs, nil
}

// stop cancels the current boundary's runs and discards them — the agent's shutdown.
func (l *sessionAdviseLane) stop() {
	l.replace(nil, nil)
}

// waiting returns the runs no opening has taken yet.
func (l *sessionAdviseLane) waiting() []*sessionAdviseRun {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.pending)
}

// take removes runs from pending, leaving any run pending gained since the read (none can: a
// boundary refuses while an Exchange is open), and moves only answered — the runs among them the
// join did not report — into the open Exchange. A reported run is dropped here for good.
func (l *sessionAdviseLane) take(runs, answered []*sessionAdviseRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = slices.DeleteFunc(l.pending, func(r *sessionAdviseRun) bool { return slices.Contains(runs, r) })
	l.delivered = append(l.delivered, answered...)
}

// restoreDelivered puts the runs an aborted Exchange took that answered back ahead of pending: the
// abort dropped the opening that carried their advice, so the next opening carries it instead. A
// run the join reported was never taken into the Exchange (take), so it stays gone.
func (l *sessionAdviseLane) restoreDelivered() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = append(l.delivered, l.pending...)
	l.delivered = nil
}

// forgetDelivered drops the runs an Exchange that closed with its opening kept had taken: their
// advice reached the conversation and is never delivered again.
func (l *sessionAdviseLane) forgetDelivered() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.delivered = nil
}

// startSessionAdvice is a Session boundary's share of the lane: it cancels the previous boundary's
// runs and starts, in the background, every armed advise Reaction on session-started that Bypass
// leaves on (D9), each under its own deadline (sessionAdviseTimeout). It returns at once whatever
// the handlers do. A delegate starts no session and so starts no runs.
func (a *Agent) startSessionAdvice(start domain.SessionStart) {
	if a.isDelegate() {
		return
	}
	var specs []domain.Reaction
	for _, r := range a.armedLadder() {
		if r.spec.Class == domain.ClassAdvise && subscribes(r.spec, domain.MomentSessionStarted) &&
			!a.bypassSkips(r.spec) {
			specs = append(specs, r.spec)
		}
	}
	if len(specs) == 0 {
		a.sessionAdvice.replace(nil, nil)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	turn := a.turns.index
	runs := make([]*sessionAdviseRun, 0, len(specs))
	for _, r := range specs {
		timeout := syncTimeout(r)
		run := &sessionAdviseRun{
			spec:     r,
			deadline: time.Now().Add(timeout),
			timeout:  timeout,
			done:     make(chan struct{}),
		}
		runs = append(runs, run)
		go a.runSessionAdvice(ctx, turn, start, run)
	}
	a.sessionAdvice.replace(cancel, runs)
}

// runSessionAdvice is one run's goroutine: the handler called under the run's deadline, its answer
// recorded for the join. A panic is recovered here — a goroutine's panic would end the process —
// and recorded for the join to report, since nothing may be emitted off the loop goroutine.
func (a *Agent) runSessionAdvice(
	ctx context.Context,
	turn int,
	start domain.SessionStart,
	run *sessionAdviseRun,
) {
	defer close(run.done)
	defer func() {
		if recovered := recover(); recovered != nil {
			run.panicked = recovered
		}
	}()
	ctx, cancel := context.WithDeadline(ctx, run.deadline)
	defer cancel()
	run.out, run.err = a.callSessionAdvice(ctx, turn, start, run.spec)
}

// callSessionAdvice dispatches one session-started advise reaction by its handler KIND: a Go handler
// is called directly, a command or a webhook goes through the sync lane's executor with the
// session-started document, and its answer is secret-redacted as adviseUser's is. The Outcome's
// Inject is the advice; empty is silence.
func (a *Agent) callSessionAdvice(
	ctx context.Context,
	turn int,
	start domain.SessionStart,
	r domain.Reaction,
) (domain.Outcome, error) {
	doc := domain.SeamPayload{Event: domain.MomentSessionStarted, Start: start}
	var answer string
	var err error
	switch handler := r.Handler.(type) {
	case domain.SessionStartedFunc:
		out, err := handler(ctx, start)
		if err != nil {
			return domain.Outcome{}, err
		}
		return domain.Outcome{Inject: out.Inject, Detail: out.Detail}, ctx.Err()
	case domain.ArgvHandler:
		answer, err = a.runSyncArgv(ctx, turn, r, doc)
	case domain.WebhookHandler:
		answer, err = a.runSyncWebhook(ctx, turn, r, doc, adviseReplyCap)
	default:
		return domain.Outcome{}, wrongHandler(domain.MomentSessionStarted, r.Handler)
	}
	if err != nil {
		return domain.Outcome{}, err
	}
	return domain.Outcome{Inject: tools.RedactSecrets(answer, a.cfg.SecretEnvNames())}, nil
}

// joinSessionAdvice is the lane's join, run by step right after an Exchange's opening is appended:
// it waits for each pending run until it answers or its deadline passes — never longer, so a slow
// handler costs the first request only what is left of its deadline — then fences each answer, in
// ladder order, as leading advice on the opening (the last message) and takes the runs off pending.
// A run that failed, panicked or missed its deadline adds nothing, is reported here and is dropped
// here: only the runs that answered are taken into the Exchange, so an abort puts back only those
// (restoreDelivered) and a reported run is never reported nor attached again, even when its handler
// finishes after the join. Under Bypass (read live, D9) the runs are taken and nothing is attached.
//
// A cancel during the wait (Esc) ends it at once and takes nothing: the runs stay pending for the
// next opening, which the cancelled Turn's own exit leaves to the host.
func (a *Agent) joinSessionAdvice(ctx context.Context, turn int) {
	runs := a.sessionAdvice.waiting()
	if len(runs) == 0 {
		return
	}
	for _, run := range runs {
		if !waitForRun(ctx, run) {
			return
		}
	}
	opening := a.conv.Len() - 1
	answered := make([]*sessionAdviseRun, 0, len(runs))
	for _, run := range runs {
		if a.bypassSkips(run.spec) || a.attachSessionAdvice(turn, opening, run) {
			answered = append(answered, run)
		}
	}
	a.sessionAdvice.take(runs, answered)
}

// waitForRun waits until run answers or its deadline passes, and reports false when ctx ended the
// wait first.
func waitForRun(ctx context.Context, run *sessionAdviseRun) bool {
	// An answer already in is taken whatever ctx says: select picks at random among ready cases.
	select {
	case <-run.done:
		return true
	default:
	}
	remaining := time.Until(run.deadline)
	if remaining <= 0 {
		return true
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-run.done:
		return true
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// attachSessionAdvice turns one joined run into what the Driver sees: its capped, fenced advice on
// the opening at index opening, booked as an advise firing under the same Detail a tool-result
// firing books (advice.bookedDetail) — or, for a run that produced nothing
// usable, the report a sync-lane failure gets (reportReaction) or the ErrorEvent a recovered panic
// gets (recoverHook). A run still going at its deadline is a timeout. It reports whether the run
// answered: false for a run it reported, which the join then drops.
func (a *Agent) attachSessionAdvice(turn, opening int, run *sessionAdviseRun) bool {
	m := domain.MomentSessionStarted
	select {
	case <-run.done:
	default:
		a.reportReaction(turn, run.spec.ID, m, fmt.Errorf("no answer within %s: %w", run.timeout, context.DeadlineExceeded))
		return false
	}
	if run.panicked != nil {
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: a.base(turn),
			Source:    run.spec.ID,
			Err:       fmt.Sprintf("panic: %v", run.panicked),
		})
		return false
	}
	if run.err != nil {
		a.reportReaction(turn, run.spec.ID, m, run.err)
		return false
	}

	adv, ok := adviceOf(turn, m, run.spec, run.out)
	if !ok || !a.conv.AdviseUserMessage(opening, adv.span, adv.text) {
		return true
	}
	var result domain.Outcome
	var fired []string
	a.bookFiring(turn, m, armedReaction{spec: run.spec}, domain.Outcome{Inject: adv.text, Detail: adv.bookedDetail(run.out.Detail)}, &result, &fired)
	return true
}
