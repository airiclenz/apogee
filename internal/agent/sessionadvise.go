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

// The session advise lanes (ADR 0076 D6, 2026-10-19 and 2026-10-21 amendments): ADR 0076's non-tool
// advise slots, one lane per Moment, each fencing its answers ahead of a user-role message's text.
//
// The session-start lane serves the session-started notice, held by the session briefing
// (sessionBriefing, briefing.go). At every top-level Session boundary the agent starts each
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
//
// The compaction lane serves the compaction-finished notice the same way, keyed by its own Moment
// and holding its own runs (sessionBriefing.compaction). A top-level fold that passes its gate
// starts each armed advise entry on compaction-finished in the background as the fold BEGINS
// (sessionBriefing.foldBegan), so the handler runs while the summary call does; a fold that ran
// hands them to the lane after its Replace (sessionBriefing.foldEnded, through
// sessionAdviseLane.adopt, in place of an earlier fold's), and a fold that faulted, was cancelled or
// folded nothing discards them. They are joined like the session-start runs, at the next user-role
// message: the opening of the next Exchange for a fold between Exchanges
// (sessionBriefing.joinOpening, which joins both lanes), or the overflow bridge for a fold
// mid-Exchange (sessionBriefing.joinBridge) — never the summary, never the system prompt. A
// compaction-finished answer re-briefs a model whose history was just folded, so when it lands it
// retires the same reaction's earlier fences from the protected prefix's first user message
// (retireStaleAdvice). A Session boundary and Close discard both lanes' runs.

// sessionAdviseLane is one Moment's background advise runs — a Session boundary's, or a fold's: the
// ones still waiting for the next user-role message, and the ones taken into the open Exchange. The zero value is an
// empty lane. mu guards it: a boundary starts runs from the Driver's goroutine, a live swap adds
// the ones it arms inside the boundary's pre-first-prompt window from the settings surface's, the
// join takes them on the loop goroutine, and Close cancels from wherever it is called.
type sessionAdviseLane struct {
	mu sync.Mutex
	// ctx is what the current boundary's runs run under, and cancel ends it: the next boundary and
	// Close call it. Both are nil while the boundary has started no run.
	ctx    context.Context
	cancel context.CancelFunc
	// held holds the runs no opening has taken yet, in ladder order, and the ones taken into the
	// open Exchange that answered: an abort puts those back ahead of pending (restoreDelivered), a
	// close that keeps the opening forgets them (forgetDelivered). A run the join reported — timed
	// out, errored or panicked — is never delivered: its report is its one outcome, so no later
	// opening reports or attaches it again.
	held heldLedger[*sessionAdviseRun]
}

// sessionAdviseRun is one entry's background run: the reaction, the seam document it runs with —
// whose Event is the lane's Moment —, the instant its deadline ends, and — once done is closed —
// what the handler answered.
type sessionAdviseRun struct {
	spec     domain.Reaction
	doc      domain.SeamPayload
	deadline time.Time
	timeout  time.Duration
	done     chan struct{}
	// out, err and panicked are written by the run's goroutine before done closes, and read only
	// after it has.
	out      domain.Outcome
	err      error
	panicked any
}

// replace installs a boundary's runs, cancelling the previous boundary's first: its unattached
// results are discarded with it. It returns the context the runs must run under, which the next
// boundary or Close cancels — nil when runs is empty, since the boundary then holds no cancel.
func (l *sessionAdviseLane) replace(runs []*sessionAdviseRun) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
	l.ctx, l.cancel = nil, nil
	l.held.reset()
	l.held.hold(runs...)
	if len(runs) > 0 {
		l.ctx, l.cancel = context.WithCancel(context.Background())
	}
	return l.ctx
}

// extend adds runs a live swap armed after the boundary to the boundary's pending runs, behind the
// ones already there, and returns the context they must run under: the boundary's own, or — when
// the boundary started none — a fresh one whose cancel the lane now holds, so the next boundary or
// Close ends them exactly as it ends the boundary's own.
func (l *sessionAdviseLane) extend(runs []*sessionAdviseRun) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel == nil {
		l.ctx, l.cancel = context.WithCancel(context.Background())
	}
	l.held.hold(runs...)
	return l.ctx
}

// adopt installs the runs a fold staged on staged — the context they run under included — in place
// of the lane's own, cancelling and discarding those first: a fold that ran supersedes an earlier
// fold's unjoined runs. staged is left empty, so its stop no longer reaches the adopted runs.
func (l *sessionAdviseLane) adopt(staged *sessionAdviseLane) {
	staged.mu.Lock()
	ctx, cancel, runs := staged.ctx, staged.cancel, staged.held.pending()
	staged.ctx, staged.cancel = nil, nil
	staged.held.reset()
	staged.mu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
	}
	l.ctx, l.cancel = ctx, cancel
	l.held.reset()
	l.held.hold(runs...)
}

// stop cancels the current boundary's runs and discards them — the agent's shutdown, a Session
// boundary's discard of the compaction lane, and a fold's discard of the runs it staged.
func (l *sessionAdviseLane) stop() {
	l.replace(nil)
}

// waiting returns the runs no opening has taken yet.
func (l *sessionAdviseLane) waiting() []*sessionAdviseRun {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held.pending()
}

// take removes runs from pending, leaving any run pending gained since the read (none can: a
// boundary refuses while an Exchange is open, a live swap adds runs only inside the
// pre-first-prompt window, which the opening's Step shut before its join read, and a fold adopts
// runs only on the loop goroutine that joins, or from /compact, which runs at a quiescent boundary
// with no Step in flight), and moves only
// answered — the runs among them the join did not report — into the open Exchange. A reported run
// is dropped here for good.
func (l *sessionAdviseLane) take(runs, answered []*sessionAdviseRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held.take(runs, func(r *sessionAdviseRun) bool { return slices.Contains(answered, r) })
}

// restoreDelivered puts the runs an aborted Exchange took that answered back ahead of pending: the
// abort dropped the opening that carried their advice, so the next opening carries it instead. A
// run the join reported was never taken into the Exchange (take), so it stays gone.
func (l *sessionAdviseLane) restoreDelivered() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held.restore()
}

// forgetDelivered drops the runs an Exchange that closed with its opening kept had taken: their
// advice reached the conversation and is never delivered again.
func (l *sessionAdviseLane) forgetDelivered() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held.forget()
}

// sessionStartedDoc is the seam document a session-started run is called with.
func sessionStartedDoc(start domain.SessionStart) domain.SeamPayload {
	return domain.SeamPayload{Event: domain.MomentSessionStarted, Start: start}
}

// sessionAdviseSpecs returns the armed advise Reactions on m that Bypass leaves on (D9), in ladder
// order: on session-started the entries a boundary starts and the ones a live swap reads for newly
// armed, on compaction-finished the entries a fold starts.
func (a *Agent) sessionAdviseSpecs(m domain.Moment) []domain.Reaction {
	var specs []domain.Reaction
	for _, r := range a.armedLadder() {
		if r.spec.Class == domain.ClassAdvise && subscribes(r.spec, m) &&
			!a.bypassSkips(r.spec) {
			specs = append(specs, r.spec)
		}
	}
	return specs
}

// newSessionAdviseRuns builds one not-yet-started run per spec, called with doc, its deadline
// counted from now.
func newSessionAdviseRuns(specs []domain.Reaction, doc domain.SeamPayload) []*sessionAdviseRun {
	runs := make([]*sessionAdviseRun, 0, len(specs))
	for _, r := range specs {
		timeout := syncTimeout(r)
		runs = append(runs, &sessionAdviseRun{
			spec:     r,
			doc:      doc,
			deadline: time.Now().Add(timeout),
			timeout:  timeout,
			done:     make(chan struct{}),
		})
	}
	return runs
}

// callSessionAdvice is the session briefing's adviseCaller: it dispatches one lane's advise reaction by its handler KIND: a Go handler — a
// SessionStartedFunc, the one the lanes know — is called directly, a command or a webhook goes
// through the sync lane's executor with doc, the lane's seam document, and its answer is
// secret-redacted as adviseUser's is. The Outcome's Inject is the advice; empty is silence.
func (a *Agent) callSessionAdvice(
	ctx context.Context,
	turn int,
	doc domain.SeamPayload,
	r domain.Reaction,
) (domain.Outcome, error) {
	var answer string
	var err error
	switch handler := r.Handler.(type) {
	case domain.SessionStartedFunc:
		out, err := handler(ctx, doc.Start)
		if err != nil {
			return domain.Outcome{}, err
		}
		return domain.Outcome{Inject: out.Inject, Detail: out.Detail}, ctx.Err()
	case domain.ArgvHandler:
		answer, err = a.runSyncArgv(ctx, turn, r, doc)
	case domain.WebhookHandler:
		answer, err = a.runSyncWebhook(ctx, turn, r, doc, adviseReplyCap)
	default:
		return domain.Outcome{}, wrongHandler(doc.Event, r.Handler)
	}
	if err != nil {
		return domain.Outcome{}, err
	}
	return domain.Outcome{Inject: tools.RedactSecrets(answer, a.cfg.SecretEnvNames())}, nil
}

// join is the lanes' one join, onto the user-role message that just entered the request
// (sessionBriefing.joinOpening, joinBridge). It waits for each pending run of every lane until it
// answers or its deadline passes — never longer, so a slow handler costs the request only what is
// left of its deadline — then lands each, lane by lane in ladder order, with land — the Agent's,
// which fences each answer as leading advice on that message — and takes the runs off pending. A run that failed, panicked or missed its deadline adds
// nothing, is reported here and is dropped here: only the runs that answered are taken into the
// Exchange, so an abort puts back only those (restoreDelivered) and a reported run is never
// reported nor attached again, even when its handler finishes after the join. Under Bypass (read
// live, D9) land takes the runs and attaches nothing.
//
// A cancel during the wait (Esc) ends it at once and takes nothing: the runs stay pending for the
// next opening, which the cancelled Turn's own exit leaves to the host.
func join(ctx context.Context, land adviceLanding, lanes ...*sessionAdviseLane) {
	waiting := make([][]*sessionAdviseRun, len(lanes))
	for i, lane := range lanes {
		waiting[i] = lane.waiting()
		for _, run := range waiting[i] {
			if !waitForRun(ctx, run) {
				return
			}
		}
	}
	for i, lane := range lanes {
		if len(waiting[i]) == 0 {
			continue
		}
		answered := make([]*sessionAdviseRun, 0, len(waiting[i]))
		for _, run := range waiting[i] {
			if land(run) {
				answered = append(answered, run)
			}
		}
		lane.take(waiting[i], answered)
	}
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

// landAdvice is the Agent's landing for a join at Turn turn (adviceLanding): under Bypass (read
// live, D9) it takes the run and attaches nothing; otherwise it attaches the run on the user-role
// message that just entered the request, the conversation's last (attachSessionAdvice).
func (a *Agent) landAdvice(turn int) adviceLanding {
	return func(run *sessionAdviseRun) bool {
		return a.bypassSkips(run.spec) || a.attachSessionAdvice(turn, a.conv.Len()-1, run)
	}
}

// attachSessionAdvice turns one joined run into what the Driver sees: its capped, fenced advice on
// the user message at index target — the reaction's stale fences retired first (retireStaleAdvice)
// —, booked as an advise firing under the same Detail a tool-result firing books
// (advice.bookedDetail) — or, for a run that produced nothing usable, the report a sync-lane
// failure gets (reportReaction) or the ErrorEvent a recovered panic gets (recoverHook), with every
// earlier fence left standing. A run still going at its deadline is a timeout. It reports whether
// the run answered: false for a run it reported, which the join then drops.
func (a *Agent) attachSessionAdvice(turn, target int, run *sessionAdviseRun) bool {
	m := run.doc.Event
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
	if !ok || a.conv.At(target).Role != domain.RoleUser {
		return true
	}
	a.retireStaleAdvice(target, m, run.spec.ID)
	a.conv.AdviseUserMessage(target, adv.span, adv.text)
	var result domain.Outcome
	var fired []string
	a.bookFiring(turn, m, armedReaction{spec: run.spec}, domain.Outcome{Inject: adv.text, Detail: adv.bookedDetail(run.out.Detail)}, &result, &fired)
	return true
}

// retireStaleAdvice strips reaction's earlier leading fences from the user message at index target
// — at a join that lands one entry's answer on both Moments, the later one replaces the earlier, so
// the message carries one fence — and, for a compaction-finished answer, from the protected
// prefix's first user message too: the fold kept that message verbatim, so a briefing an earlier
// boundary or fold left there would stand beside the re-brief that supersedes it. Every other
// reaction's fences stay (Conversation.StripReactionAdvice).
func (a *Agent) retireStaleAdvice(target int, m domain.Moment, reaction string) {
	a.conv.StripReactionAdvice(target, reaction)
	if m == domain.MomentCompactionFinished {
		a.conv.StripReactionAdvice(a.conv.PrefixEnd()-1, reaction)
	}
}
