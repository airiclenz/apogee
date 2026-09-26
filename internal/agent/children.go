package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Child addressing (ADR 0063, ADR 0086) — a running sub-agent is reachable by its run id
// ----------------------------------------------------------------------------
//
// A delegation is opaque to everything outside the engine: runSubAgent drives the child to its
// boundary inside the parent's Turn (ADR 0013 D5) and nobody else holds the child. The two types
// here are the whole seam that makes a RUNNING child addressable anyway — a registry the parent
// publishes its live children in, and a mailbox each child drains at its own between-Steps
// boundaries. Together they let a Driver say "this message is for that sub-agent" with nothing
// but the id it already paints the delegation by, and they add no goroutine: the child's own
// Step-driving loop does the delivering.

// childRegistry is the set of sub-agents ONE Agent currently has running, keyed by each child's
// run id (domain.EventBase.RunID) — the engine-minted identity the child stamps on every Event it
// emits, so a caller addresses a child by the identity it already sees. It is keyed by run id and
// not by the spawning call's id because a call id is the model's to choose and can collide (ADR
// 0059 §6): two delegations sharing one call id would share one entry, and a message meant for
// one would reach the other. Run ids are unique within the tree (runIDMinter).
//
// Membership is exactly the child's run: runSubAgent registers before it drives the child and
// unregisters in the defer that closes it, so a lookup that succeeds names a child that was
// running at the moment of the lookup. It is guarded because the depth-0 fan-out registers and
// unregisters from several pool workers at once, while lookups arrive from the host's goroutine.
//
// Beside each entry the registry holds the run's STOP HANDLE (ADR 0086 D4): the cancel of the
// context the child's work runs under, armed only while a stop can still cut that work short —
// the child's Run, then the fold a stopped run is given — and withdrawn between the two, so a stop
// that lands once the run has returned finds nothing to cancel (StopChild).
//
// And it holds the run ids of a pooled group's delegations that have not been armed yet (ADR 0086
// D4: a stop reaches a queued child too). dispatchGroup enters every pooled delegation here before
// the pool starts; each entry is the stop mark a StopChild sets, read at the pool's dequeue
// (dequeue) and, for a slot dequeued before its child is armed, carried into arm, which cancels the
// child's context as it is created. The mark and its readers share this one lock, so a stop lands
// either before the dequeue — the child never starts — or after it, on the child it becomes.
//
// The zero value is ready to use.
type childRegistry struct {
	mu      sync.Mutex
	byRunID map[string]*Agent
	stops   map[string]context.CancelCauseFunc
	// queued maps the run id of a pooled delegation not yet armed to its stop mark: false while
	// nothing has asked to stop it, true once a StopChild has.
	queued map[string]bool
}

// register publishes child under its run id. Run ids are minted unique within the tree, so a
// second registration under one id does not arise; were it to, the later child replaces the
// earlier rather than the pair silently sharing a mailbox.
func (r *childRegistry) register(runID string, child *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byRunID == nil {
		r.byRunID = make(map[string]*Agent, 1)
	}
	r.byRunID[runID] = child
}

// unregister removes the entry for runID. It is a no-op for an id that is not registered, so the
// defer that calls it is safe on every early return runSubAgent takes before registering.
func (r *childRegistry) unregister(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byRunID, runID)
	delete(r.stops, runID)
}

// arm makes the run registered under runID stoppable through cancel until disarm withdraws it.
// Arming again replaces the handle: the fold a stopped run is given takes the one its Run held.
// A run still held as queued leaves that set here, and a stop marked on it after the pool dequeued
// it — in the window before this arm — is carried in: cancel fires at once, so the child's context
// is cancelled as it is created.
func (r *childRegistry) arm(runID string, cancel context.CancelCauseFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stops == nil {
		r.stops = make(map[string]context.CancelCauseFunc, 1)
	}
	r.stops[runID] = cancel
	if marked, ok := r.queued[runID]; ok {
		delete(r.queued, runID)
		if marked {
			cancel(errDelegationStopped)
		}
	}
}

// queue holds runID as a pooled delegation that has not started, stoppable by a mark until the
// pool dequeues it (dequeue) and its child is armed (arm). A run id of "" is never queued.
func (r *childRegistry) queue(runID string) {
	if runID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queued == nil {
		r.queued = make(map[string]bool, 1)
	}
	r.queued[runID] = false
}

// dequeue is the pool's read of runID's stop mark as a worker takes the slot. A marked slot leaves
// the set and true is returned: the delegation is not started. An unmarked one stays held until
// its child is armed (arm) or the slot settles without one (unqueue), so a stop landing in between
// still reaches it.
func (r *childRegistry) dequeue(runID string) (stopped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queued[runID] {
		delete(r.queued, runID)
		return true
	}
	return false
}

// unqueue forgets runID's queued entry — the pool's closing act for a slot, so a delegation that
// settled without ever arming a child (a refusal before one exists, a pre-empted slot) leaves no
// stop mark behind to report a stop that changed nothing. A no-op once arm has taken the entry.
func (r *childRegistry) unqueue(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.queued, runID)
}

// disarm withdraws runID's stop handle; the run stays registered and addressable otherwise.
func (r *childRegistry) disarm(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.stops, runID)
}

// stop cancels runID's armed work with errDelegationStopped as the cause, or marks runID stopped
// while it is still held as queued, and reports whether either reached it. A cancel func never
// blocks, so calling it under the lock is safe.
func (r *childRegistry) stop(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel, ok := r.stops[runID]; ok {
		cancel(errDelegationStopped)
		return true
	}
	if _, ok := r.queued[runID]; ok {
		r.queued[runID] = true
		return true
	}
	return false
}

// lookup returns the running child registered under runID.
func (r *childRegistry) lookup(runID string) (*Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	child, ok := r.byRunID[runID]
	return child, ok
}

// all returns a snapshot of the running children, in unspecified order. It is a copy so the
// caller can recurse into each child without holding this registry's lock — which it must not,
// because a grandchild's registry is locked one level down.
func (r *childRegistry) all() []*Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	children := make([]*Agent, 0, len(r.byRunID))
	for _, child := range r.byRunID {
		children = append(children, child)
	}
	return children
}

// errDelegationStopped is the cancel cause a human's stop carries (StopChild): read back through
// context.Cause, it is what tells a run the human stopped from one whose parent was cancelled.
var errDelegationStopped = errors.New("delegation stopped by the user")

// retainedDelegate is what a parent keeps of ONE delegation the engine stopped at a bound (plan
// 2026-09-18 - 00, P6), that FAULTED with the parent still live (ADR 0082), or that the human
// stopped (StopChild, ADR 0086 D4): everything a
// continuation needs to spawn a fresh child that picks up where the capped or faulted one left
// off — the task and roster the spawning call asked for, the output path it named, the engine
// fold and closing text the child left, and which bound ended it. It is a value: every field is
// copied at retention, after the child's run has been read and reported, so nothing here aliases
// a child that runSubAgent's defer is about to close.
type retainedDelegate struct {
	task          string               // the delegated task, as the spawning call spelled it
	name          string               // the display name the child ended its run wearing — the key it is retained under
	tools         tools.SubAgentRoster // the `tools` argument as the call asked it, unresolved: re-resolved against the parent's menu on a continuation
	outputPath    string               // the `output_path` argument as the call spelled it; "" when it named none
	fold          string               // the engine fold written at the bound or the fault (Agent.capFold) — under `[engine summary]` in a capped result, retained only for a faulted one
	closingReport string               // the child's last words (Agent.lastVisibleText): the closing text a capped result forwarded, the last narration a faulted child committed; "" for a wordless child
	bound         delegateBound        // which bound ended a capped child (Agent.capHit); the zero value for a faulted one, which no bound ended
	spawnCallID   string               // the sub_agent call the retained child answered
}

// retainedDelegates is the set of capped, faulted or stopped delegations ONE Agent holds for the rest of
// its Exchange, keyed by delegation name — the handle the parent model already knows a delegation
// by, and the only one it can spell back. It exists in memory only: the map is cleared as the next Exchange
// opens (Agent.step) and never reaches the session snapshot (ADR 0022 D8, ADR 0013 §5 — a
// delegation is opaque to everything outside the engine, and a continuation belongs to the
// Exchange that started the work it continues). It is guarded because the depth-0 fan-out retains
// from several pool workers at once (ADR 0039).
//
// The zero value is ready to use.
type retainedDelegates struct {
	mu     sync.Mutex
	byName map[string]retainedDelegate
}

// retain keeps d under its name. The same name replaces an earlier entry: two retained children a
// parent named alike are two attempts at one piece of work, and the latest is the one a
// continuation should pick up from. An unnamed delegation (d.name == "") is not retained — there is
// no handle a continuation could name it by.
func (r *retainedDelegates) retain(d retainedDelegate) {
	if d.name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byName == nil {
		r.byName = make(map[string]retainedDelegate, 1)
	}
	r.byName[d.name] = d
}

// lookup returns the delegation retained under name.
func (r *retainedDelegates) lookup(name string) (retainedDelegate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byName[name]
	return d, ok
}

// take returns the delegation retained under name and FORGETS it: a continuation consumes
// the entry it starts from, so the same fold is never continued twice — the continued child is
// retained anew, under the same name, if it caps or faults again.
func (r *retainedDelegates) take(name string) (retainedDelegate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byName[name]
	if ok {
		delete(r.byName, name)
	}
	return d, ok
}

// names returns the retained names, sorted, so a refusal that lists them reads the same on every
// run. Empty when nothing is retained.
func (r *retainedDelegates) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// clear forgets every retained delegation — the Exchange that owned them has ended.
func (r *retainedDelegates) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName = nil
}

// ----------------------------------------------------------------------------
// The delegate ledger (apogee-clb) — what the engine saw every delegation of an Exchange do
// ----------------------------------------------------------------------------
//
// A coordinator that delegates several times has been seen misremembering which delegates faulted
// and which wrote their output: each result was read once, Turns ago, and the model's own recall of
// them is what a later request is built on. The ledger is the host's record of the same facts —
// one row per delegation the Exchange spawned, in spawn order — rendered onto every request tail
// as an engine note (delegationsNoteTopic, Agent.buildRequest) once the Exchange holds two or more
// delegations or any one that did not complete. It states facts and asks nothing: what to do about
// a faulted delegate or a missing file is the coordinator's call, and a note that issued orders
// would be a Reaction wearing the engine's header. Like retainedDelegates it lives in memory only,
// is cleared as the next Exchange opens (Agent.step) and never reaches the session snapshot (ADR
// 0022 D8): the note is a per-request projection, never a conversation message.

// delegationOutcome is how one delegation ended, as the engine classified it from the result and
// dispatch outcome runSubAgent returned — the six words the ledger's rows spell.
type delegationOutcome string

const (
	// delegationCompleted is a child that ran to its own reply and reported it.
	delegationCompleted delegationOutcome = "completed"
	// delegationCapped is a child the engine stopped at a bound (step, token or time) and that
	// handed back a partial result.
	delegationCapped delegationOutcome = "capped"
	// delegationFaulted is a child that ran and came back as an error result: an Upstream fault,
	// a recovered panic, a loop-level Run error, or a reply the engine refused to hand over as a
	// report (a missing output file, tool-call markup, a degenerate repeat).
	delegationFaulted delegationOutcome = "faulted"
	// delegationCancelled is a child the human cancelled with the whole Turn; its Turn was rolled
	// back with the parent's.
	delegationCancelled delegationOutcome = "cancelled"
	// delegationStopped is a child the human stopped singly (StopChild, ADR 0086 D4): the parent's
	// Turn went on and read the engine fold of the child's work as a partial result.
	delegationStopped delegationOutcome = "stopped"
	// delegationRefused is a sub_agent call no child was ever built or started for: the depth
	// bound, bad arguments, an unknown `continue`, a bad seat or roster, a construction or Submit
	// failure — the parent read an error result and no delegation ran.
	delegationRefused delegationOutcome = "refused"
)

// delegationRecord is ONE row of the ledger: the delegation's spawn order, the call it answered,
// the label the parent model knows it by, how it ended and — for a faulted or refused one — the
// head line of the text that said so. outputPath is the child's RESOLVED output target
// (Agent.outputTarget: workspace-joined, symlinks followed), "" when the spawn named none or the
// child ran in Plan mode, where no write was ever possible (as outputMissing reads it); presence
// is read from the filesystem when the note is rendered, never at recording time, so a file a
// later delegation or the coordinator itself wrote in between reads as present.
type delegationRecord struct {
	spawnIndex int
	callID     string
	name       string
	outcome    delegationOutcome
	cause      string
	outputPath string
}

// delegationLedger is the ordered set of delegationRecords ONE Agent holds for its current
// Exchange. It is guarded because the depth-0 fan-out spawns and reports from several pool workers
// at once (ADR 0039). The spawn index is the order the parent model ISSUED its calls in: a pooled
// group reserves one per delegation in call order before its workers start (reserve, dispatchGroup),
// because the workers dequeue and finish in an order of their own; a width-1 delegation takes the
// next index as its run opens (open). The render sorts by it.
//
// The zero value is ready to use.
type delegationLedger struct {
	mu       sync.Mutex
	spawned  int
	reserved map[string]int
	records  []delegationRecord
}

// reserve takes the next spawn index for the sub_agent call callID ahead of its run — what a
// pooled group does for each of its delegations in call order, so the numbers the note spells are
// the model's own call order and not the pool's dequeue order. open hands the index back to the
// run that answers that call.
func (l *delegationLedger) reserve(callID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reserved == nil {
		l.reserved = make(map[string]int, 1)
	}
	l.spawned++
	l.reserved[callID] = l.spawned
}

// open returns the spawn index for the delegation answering callID as its run begins: the one
// reserve set aside for it, consumed, or else the next fresh index. Indices are 1-based and count
// every entry into runSubAgent, refused ones included — and every call refused past the reply's
// fan-out ceiling, which never enters runSubAgent and is opened and recorded by dispatchGroup
// itself (recordCeilingRefusal), after a pooled group's running slots have been reserved, so the
// refused rows number behind them — and every pooled delegation the human stopped before it
// started, which never enters runSubAgent either and takes the index its group reserved for it
// (stopQueuedDelegation).
func (l *delegationLedger) open(callID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i, ok := l.reserved[callID]; ok {
		delete(l.reserved, callID)
		return i
	}
	l.spawned++
	return l.spawned
}

// record appends one finished delegation's row.
func (l *delegationLedger) record(r delegationRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r)
}

// rows returns a copy of the records in spawn order.
func (l *delegationLedger) rows() []delegationRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]delegationRecord, len(l.records))
	copy(out, l.records)
	sort.Slice(out, func(i, j int) bool { return out[i].spawnIndex < out[j].spawnIndex })
	return out
}

// clear forgets every row — the Exchange that owned them has ended.
func (l *delegationLedger) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.spawned = 0
	l.reserved = nil
	l.records = nil
}

// delegationsNoteTopic is the engine-note topic the ledger is fenced under on the request tail —
// `[engine — delegations]` … `[end engine — delegations]` — and the key NoteOnTail's idempotence
// reads.
const delegationsNoteTopic = "delegations"

// delegationsNoteHead is the first line of the note and the marker its AppendToSystem fallback
// is keyed on (Agent.buildRequest): a system message already carrying it is not noted twice.
const delegationsNoteHead = "delegations this exchange, as the engine recorded them (spawn order):"

// delegationCauseMaxRunes bounds the cause a row quotes — the head line of a fault or refusal —
// so one verbose failure cannot swell the note the coordinator reads on every request.
const delegationCauseMaxRunes = 160

// note renders the ledger as the engine note buildRequest stamps, and reports whether the ratified
// trigger holds: two or more delegations this Exchange, or any one that did not complete. One
// delegation that completed is the case the parent model gets right unaided, so no note rides
// then. Each row reads `#<n> <name> — <outcome>[: <cause>] — output <present|missing|none>`;
// presence is read here, at render time, by the same rule outputMissing applies to a capped
// result: `missing` is a certainly-absent file (fs.ErrNotExist), anything else that resolved is
// `present`, and a delegation with no resolved target reads `none`.
func (l *delegationLedger) note() (string, bool) {
	rows := l.rows()
	notable := len(rows) >= 2
	for _, r := range rows {
		if r.outcome != delegationCompleted {
			notable = true
		}
	}
	if !notable {
		return "", false
	}
	var b strings.Builder
	b.WriteString(delegationsNoteHead)
	for _, r := range rows {
		b.WriteString("\n")
		b.WriteString(r.render())
	}
	return b.String(), true
}

// render spells one row. The cause rides only where there is one (a fault or a refusal); the
// other outcomes name themselves.
func (r delegationRecord) render() string {
	line := fmt.Sprintf("#%d %s — %s", r.spawnIndex, r.name, r.outcome)
	if r.cause != "" {
		line += ": " + r.cause
	}
	return line + " — output " + outputPresence(r.outputPath)
}

// outputPresence reads a delegation's output target off the filesystem at render time.
func outputPresence(target string) string {
	if target == "" {
		return "none"
	}
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		return "missing"
	}
	return "present"
}

// delegationCause is the head line of a fault or refusal result, clamped to delegationCauseMaxRunes,
// so a row quotes what the parent already read at the top of that result and no more.
func delegationCause(content string) string {
	head := strings.TrimSpace(headLines(content, 1))
	if clamped := sanitize.ClampRunes(head, delegationCauseMaxRunes); clamped != head {
		return clamped + "…"
	}
	return head
}

// childMailbox holds the user messages queued for ONE agent while it runs as somebody's child,
// in the order they were queued. It is the handover between the goroutine a message is typed on
// and the goroutine driving the child's Steps: adding is non-blocking and safe from anywhere,
// draining happens only on the driving goroutine, at a between-Steps boundary where Interject is
// legal (ADR 0025's caller rule). Between the two, a queued message is also a PREDICATE the
// child's dispatch reads (hasPending): a grandchild not yet started is skipped rather than run
// while a message waits for the boundary, exactly as a top-level agent's queued message skips
// its unstarted children through Config.InterjectionPending.
//
// It closes exactly once, when the child's run ends, and refuses everything after: a message that
// cannot be delivered must be refused at the door rather than accepted into a mailbox nothing will
// ever drain, because every accepted message owes its sender a ChildInterjectionEvent.
//
// The zero value is ready to use.
type childMailbox struct {
	mu     sync.Mutex
	queued []domain.UserInput
	closed bool
}

// add queues in and reports whether the mailbox accepted it. A closed mailbox accepts nothing.
func (m *childMailbox) add(in domain.UserInput) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.queued = append(m.queued, in)
	return true
}

// hasPending reports whether a message is queued and not yet drained — the child-side answer to
// Config.InterjectionPending, read by the dispatch that is about to start a grandchild
// (Agent.interjectionPending). A closed mailbox holds nothing deliverable and reports false.
func (m *childMailbox) hasPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && len(m.queued) > 0
}

// drain takes everything queued so far, leaving the mailbox open for more.
func (m *childMailbox) drain() []domain.UserInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	queued := m.queued
	m.queued = nil
	return queued
}

// close takes everything still queued and refuses every later add. The returned messages never
// reached the model and are the caller's to account for.
func (m *childMailbox) close() []domain.UserInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	queued := m.queued
	m.queued = nil
	return queued
}

// InterjectChild queues a user message for the RUNNING sub-agent whose run id is runID, anywhere
// in this Agent's tree: its own children first, then — recursively — theirs, so a host holding
// only the top-level Agent reaches a grandchild at depth 2. The message lands at that child's next
// between-Steps boundary as an ordinary interjection (Agent.Interject), with the child's own tool
// set, mode and confinement unchanged: addressing a child grants it nothing (ADR 0005, ADR 0063 D6).
//
// Contract: non-blocking and safe from ANY goroutine — it only appends to a guarded mailbox and
// never touches the child's conversation. It is the second engine call legal from an interactive
// host's own goroutine while the loop runs, beside AbortExchange; the delivery it schedules is
// performed by the goroutine that owns the child's Steps.
//
// runID is the child's run identity (domain.EventBase.RunID), the id every Event it emits carries.
// It returns domain.ErrNoSuchChild when runID names no running sub-agent — the child
// finished, was cancelled, or never existed — and that refusal is the message's whole account:
// nothing was queued, so no ChildInterjectionEvent follows. On success exactly one
// ChildInterjectionEvent will report the message's fate, Landed either way.
func (a *Agent) InterjectChild(runID string, in domain.UserInput) error {
	if runID == "" {
		return domain.ErrNoSuchChild
	}
	if child, ok := a.children.lookup(runID); ok {
		if child.mailbox.add(in) {
			return nil
		}
		// Registered but already closing: the child ended between the lookup and the add, so it
		// is no more addressable than one that was never there.
		return domain.ErrNoSuchChild
	}
	for _, child := range a.children.all() {
		if err := child.InterjectChild(runID, in); err == nil {
			return nil
		}
	}
	return domain.ErrNoSuchChild
}

// StopChild stops the RUNNING sub-agent whose run id is runID, anywhere in this Agent's tree, and
// every delegation under it, while the Turn that spawned it goes on (ADR 0086 D4). Nothing rolls
// back: the stopped child's work is folded by the engine (Agent.finishAtStop) and its tool result —
// a non-error partial result opening on stoppedResultHead — is committed like any other, the run is
// retained for a `continue` as a capped one is, and the delegate ledger records it `stopped`. A
// second StopChild on the same run id while that fold is running skips the fold, and the result
// carries the unavailable marker in its place.
//
// Contract: non-blocking and safe from ANY goroutine, like InterjectChild — it cancels a context
// and returns; the child's own goroutine unwinds its run and reports it. The cancel is the only
// mechanism (ADR 0031): the child's context is a child of the parent's, so a whole-Turn cancel
// still reaches it.
//
// A pooled delegation still waiting for a worker is stopped too: it is marked, and the pool's
// dequeue settles it without starting it — no child, no fold — on the error-shaped
// stoppedQueuedDelegationContent, recorded `stopped` in the ledger, while its siblings run on
// (runPool). One dequeued but not yet running takes the mark into its child, whose context is
// cancelled as it is created. A stopped grandchild's result goes to its own parent child like any
// tool result, and that child runs on.
//
// It returns domain.ErrNoSuchChild when runID names no run a stop can still cut short — one that
// never existed, has finished, or has returned from its run and is being reported — and nothing
// was changed.
func (a *Agent) StopChild(runID string) error {
	if runID == "" {
		return domain.ErrNoSuchChild
	}
	if a.children.stop(runID) {
		return nil
	}
	if _, ok := a.children.lookup(runID); ok {
		// Registered but disarmed: the run has returned and its result is being rendered, so
		// the stop can no longer change what the parent reads.
		return domain.ErrNoSuchChild
	}
	for _, child := range a.children.all() {
		if err := child.StopChild(runID); err == nil {
			return nil
		}
	}
	return domain.ErrNoSuchChild
}

// drainMailbox commits everything queued for this child into its open Exchange, in queue order,
// and reports each message's fate. It is called by Run at a between-Steps boundary it is about to
// step past — the one place Interject's caller rule is satisfied without a host driving Step —
// and only for a CHILD: a top-level Run drains nothing and emits no ChildInterjectionEvent,
// because a top-level interjection stays the host's own call between the Steps it drives
// (ADR 0025; ADR 0063 D1 supersedes that rejection for depth > 0 only). The top level's only
// signal that a message waits is the Config.InterjectionPending seam its dispatch reads to skip
// the delegations it has not started (Agent.interjectionPending) — a predicate, never a drain
// (ADR 0025, amended 2026-09-14).
//
// ctx is the child's Run context — the one the Step it is about to make runs under — and bounds
// only the reference resolution inside Interject; a cancel there skips a document, never the
// message, so the drain's refusal rule below is untouched by it. turn is the Turn the messages
// are about to reach, which is what the events report.
func (a *Agent) drainMailbox(ctx context.Context, turn int) {
	if !a.isDelegate() {
		return
	}
	queued := a.mailbox.drain()
	for i, in := range queued {
		if err := a.Interject(ctx, in); err != nil {
			// The first refusal STOPS the drain, exactly as the TUI's own delivery does
			// (deliverInterjections): an Interject error is a statement about the Exchange, not
			// about that one message, so pressing on would produce more of the same and deliver
			// the human's remarks out of order.
			a.reportUndelivered(turn, queued[i:], domain.UndeliveredRefused)
			return
		}
		// Counted here and nowhere else: what LANDED is what the parent is told about when this
		// child's result comes back (runSubAgent's trailer), so a refused or undelivered message
		// never inflates the count.
		a.steered++
		a.cfg.Events.Emit(domain.ChildInterjectionEvent{EventBase: a.base(turn), Input: in, Landed: true})
	}
}

// reportUndelivered emits the Landed:false half of the delivery contract for messages that never
// reached the model — the tail of a refused drain, and whatever the mailbox still held when the
// child's run ended. Every accepted message is accounted for exactly once, so a Driver never has
// to guess what became of one it painted as queued. reason is why none of them landed, and rides
// every event so a Driver can say so rather than guess (domain.UndeliveredReason).
func (a *Agent) reportUndelivered(turn int, queued []domain.UserInput, reason domain.UndeliveredReason) {
	for _, in := range queued {
		a.cfg.Events.Emit(domain.ChildInterjectionEvent{
			EventBase: a.base(turn),
			Input:     in,
			Landed:    false,
			Reason:    reason,
		})
	}
}

// undeliveredReason maps how a delegation ended (classifyDelegation) onto why the messages its
// mailbox still held never landed. A refusal can only leave a message behind when the child was
// registered and then never reached its Run — a panic in between — so it reads as the child not
// taking the message, which is what happened.
func undeliveredReason(ended delegationOutcome) domain.UndeliveredReason {
	switch ended {
	case delegationCapped:
		return domain.UndeliveredCapped
	case delegationFaulted:
		return domain.UndeliveredFaulted
	case delegationCancelled:
		return domain.UndeliveredCancelled
	case delegationStopped:
		return domain.UndeliveredStopped
	case delegationRefused:
		return domain.UndeliveredRefused
	default:
		return domain.UndeliveredCompleted
	}
}
