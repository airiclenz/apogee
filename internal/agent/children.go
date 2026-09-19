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
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Child addressing (ADR 0063) — a running sub-agent is reachable by its spawn call-ID
// ----------------------------------------------------------------------------
//
// A delegation is opaque to everything outside the engine: runSubAgent drives the child to its
// boundary inside the parent's Turn (ADR 0013 D5) and nobody else holds the child. The two types
// here are the whole seam that makes a RUNNING child addressable anyway — a registry the parent
// publishes its live children in, and a mailbox each child drains at its own between-Steps
// boundaries. Together they let a Driver say "this message is for that sub-agent" with nothing
// but the id it already paints the delegation by, and they add no goroutine: the child's own
// Step-driving loop does the delivering.

// childRegistry is the set of sub-agents ONE Agent currently has running, keyed by the id of the
// sub_agent call that spawned each — the same id the child stamps on every Event it emits, so a
// caller addresses a child by the identity it already sees.
//
// Membership is exactly the child's run: runSubAgent registers before it drives the child and
// unregisters in the defer that closes it, so a lookup that succeeds names a child that was
// running at the moment of the lookup. It is guarded because the depth-0 fan-out registers and
// unregisters from several pool workers at once, while lookups arrive from the host's goroutine.
//
// The zero value is ready to use.
type childRegistry struct {
	mu       sync.Mutex
	byCallID map[string]*Agent
}

// register publishes child under its spawn call-ID. Registering the same id twice replaces the
// entry: ids are the model's to choose and two calls of one Turn can collide (ADR 0059 §6), so
// last-in wins rather than the pair silently sharing a mailbox.
func (r *childRegistry) register(spawnCallID string, child *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byCallID == nil {
		r.byCallID = make(map[string]*Agent, 1)
	}
	r.byCallID[spawnCallID] = child
}

// unregister removes the entry for spawnCallID. It is a no-op for an id that is not registered,
// so the defer that calls it is safe on every early return runSubAgent takes before registering.
func (r *childRegistry) unregister(spawnCallID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byCallID, spawnCallID)
}

// lookup returns the running child registered under spawnCallID.
func (r *childRegistry) lookup(spawnCallID string) (*Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	child, ok := r.byCallID[spawnCallID]
	return child, ok
}

// all returns a snapshot of the running children, in unspecified order. It is a copy so the
// caller can recurse into each child without holding this registry's lock — which it must not,
// because a grandchild's registry is locked one level down.
func (r *childRegistry) all() []*Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	children := make([]*Agent, 0, len(r.byCallID))
	for _, child := range r.byCallID {
		children = append(children, child)
	}
	return children
}

// retainedDelegate is what a parent keeps of ONE delegation the engine stopped at a bound (plan
// 2026-09-18 - 00, P6): everything a continuation needs to spawn a fresh child that picks up where
// the capped one left off — the task and roster the spawning call asked for, the output path it
// named, the engine fold and closing text the capped result carried, and which bound ended it.
// It is a value: every field is copied at retention, after the child's run has been read and
// reported, so nothing here aliases a child that runSubAgent's defer is about to close.
type retainedDelegate struct {
	task          string               // the delegated task, as the spawning call spelled it
	name          string               // the display name the child ended its run wearing — the key it is retained under
	tools         tools.SubAgentRoster // the `tools` argument as the call asked it, unresolved: re-resolved against the parent's menu on a continuation
	outputPath    string               // the `output_path` argument as the call spelled it; "" when it named none
	fold          string               // the engine fold the capped result carried under `[engine summary]` (Agent.capFold)
	closingReport string               // the child's closing text as the capped result forwarded it (Agent.lastVisibleText); "" for a wordless child
	bound         delegateBound        // which bound ended the child (Agent.capHit)
	spawnCallID   string               // the sub_agent call the capped child answered
}

// retainedDelegates is the set of capped delegations ONE Agent holds for the rest of its Exchange,
// keyed by delegation name — the handle the parent model already knows a delegation by, and the
// only one it can spell back. It exists in memory only: the map is cleared as the next Exchange
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

// retain keeps d under its name. The same name replaces an earlier entry: two capped children a
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

// lookup returns the capped delegation retained under name.
func (r *retainedDelegates) lookup(name string) (retainedDelegate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byName[name]
	return d, ok
}

// take returns the capped delegation retained under name and FORGETS it: a continuation consumes
// the entry it starts from, so the same fold is never continued twice — the continued child is
// retained anew, under the same name, if it caps again.
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
// dispatch outcome runSubAgent returned — the five words the ledger's rows spell.
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
	// delegationCancelled is a child the human stopped; its Turn was rolled back with the parent's.
	delegationCancelled delegationOutcome = "cancelled"
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
// every entry into runSubAgent, refused ones included.
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
	if clamped := clampRunes(head, delegationCauseMaxRunes); clamped != head {
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

// InterjectChild queues a user message for the RUNNING sub-agent spawned by spawnCallID, anywhere
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
// It returns domain.ErrNoSuchChild when spawnCallID names no running sub-agent — the child
// finished, was cancelled, or never existed — and that refusal is the message's whole account:
// nothing was queued, so no ChildInterjectionEvent follows. On success exactly one
// ChildInterjectionEvent will report the message's fate, Landed either way.
func (a *Agent) InterjectChild(spawnCallID string, in domain.UserInput) error {
	if spawnCallID == "" {
		return domain.ErrNoSuchChild
	}
	if child, ok := a.children.lookup(spawnCallID); ok {
		if child.mailbox.add(in) {
			return nil
		}
		// Registered but already closing: the child ended between the lookup and the add, so it
		// is no more addressable than one that was never there.
		return domain.ErrNoSuchChild
	}
	for _, child := range a.children.all() {
		if err := child.InterjectChild(spawnCallID, in); err == nil {
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
			a.reportUndelivered(turn, queued[i:])
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
// to guess what became of one it painted as queued.
func (a *Agent) reportUndelivered(turn int, queued []domain.UserInput) {
	for _, in := range queued {
		a.cfg.Events.Emit(domain.ChildInterjectionEvent{EventBase: a.base(turn), Input: in, Landed: false})
	}
}
