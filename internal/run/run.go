package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/refs"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/title"
	"github.com/airiclenz/apogee/internal/tools"
)

// ErrMode is returned when a Spec names an autonomy mode a Firing may not run in. A Firing
// runs read-only (Plan) or confined-and-unattended (Auto); Ask-Before and Allow-Edits both
// exist to consult a human, and there is none (ADR 0033, decision 2).
var ErrMode = errors.New("apogee: a firing runs in plan or auto mode only")

// Spec is one Firing's complete input. The Config is composed by the CALLER — endpoint,
// model, workspace, mode, confinement posture, tools, mechanisms — because only the caller
// knows the binding a Firing should run against; Once overrides just the delegates that
// assume a human (see the package doc).
type Spec struct {
	// Config is the agent construction surface. Its Approver, Asker and Presenter fields are
	// replaced by Once; its Events sink is WRAPPED, not replaced — Once's eventTap forwards
	// every Event on to it (nil ⇒ discarded) while catching what the Result cannot recover
	// once the run is over. Everything else is used as given.
	Config domain.Config

	// Prompt is the single user message the Firing submits.
	Prompt string

	// ScheduleID and ScheduleName are the Schedule identity stamped onto the saved record's
	// browsable Meta (ADR 0033). Both empty ⇒ the record is an ordinary session, which is
	// what a bench or a bare `apogee headless` run wants.
	ScheduleID   string
	ScheduleName string

	// Store persists the Firing's record at completion. nil ⇒ the run is not persisted at
	// all (the bench case) and Result.SessionID stays empty.
	Store *session.Store

	// RecordID is the id the saved record is filed under, minted by the CALLER before the
	// Firing starts so anything the caller keys on that id — the Firing's scratch dir — exists
	// under the same name from the first tool call. Empty ⇒ Once mints one at completion, as
	// it always has; the bench and every caller that keys nothing on the id leave it empty.
	RecordID string

	// Title overrides the record's title. Empty ⇒ Once derives one: "<schedule name> —
	// <HH:MM>" in local time when the Schedule identity is present, so a Schedule's runs
	// read chronologically under its name, else the first-prompt heuristic.
	Title string

	// Now is the clock behind the derived title and the record's timestamps; nil ⇒
	// time.Now. It exists so a test pins both without touching the machine clock — the
	// injectable-clock shape session.Store and the TUI's session host already use.
	Now func() time.Time
}

// Result reports what one Firing did. It is returned even when the run failed, so a caller
// can report the partial outcome rather than only the failure.
type Result struct {
	// SessionID is the saved record's id, empty when the Spec carried no Store.
	SessionID string
	// Title is the record's title — the Spec's, or the one Once derived.
	Title string
	// FinalText is the Firing's answer: the text of the run's final TOP-LEVEL assistant
	// message, empty when the run produced none (cancelled mid-tool, errored before an
	// answer). Two halves to its contract, and a caller must honour both. It is RAW model
	// output — a surface escape-strips it at its own render seam, this library does not
	// (ADR 0010: the answer crosses as plain data). And it is top-level ONLY: a
	// sub-agent's message (Depth > 0) reports to its parent, not to the Firing's caller,
	// so it never fills this field.
	FinalText string
	// Turns is how many Turns the Exchange took (the final Turn's index plus one).
	Turns int
	// Denied is how many gated actions the fail-safe denier refused. A non-zero count on a
	// Plan Firing means the model kept reaching past its read-only floor; on an Auto Firing
	// it means the run needed a human it did not have.
	Denied int
	// Faulted reports that the Firing's final Turn was ABANDONED (domain.StepResult.Faulted):
	// the Exchange reached its boundary, so Err is nil and FinalText is whatever the run last
	// said, but that text is NOT the answer to the prompt. A caller that treats FinalText as the
	// product must read this first — it is the one field that tells the two apart.
	Faulted bool
	// Fault is why that final Turn was abandoned — the Agent's LastFault(), which is the very
	// ErrorEvent text an attached event sink already saw. Empty when Faulted is false, and empty
	// on the rare fault that surfaced no ErrorEvent at all, which a caller renders as a fault
	// naming no cause rather than as no fault.
	Fault string
	// SubAgents reports how full each delegated run's context got and what it spent, one entry
	// per sub-agent run, in FINISH order (so a nested run precedes the run that spawned it). It
	// is flat: the entries carry no nesting, because a reading belongs to exactly one run and
	// never rolls up. Runs that reported no usage at all are absent, and a Firing that delegated
	// nothing carries none.
	SubAgents []SubAgentUsage
	// Usage is the Firing's OWN cumulative token accounting — the top-level agent's totals for
	// the whole run, compaction folds included. It is the spend beside Meta.CtxUsed's fill: the
	// fill says how full the window ended, these totals say what the run cost to get there. A
	// delegated run's spend is its own and is absent here (SubAgents carries it), so a
	// session-wide figure is the sum across the two — which Once itself takes when it writes
	// the record (Meta.Usage plus Meta.DelegateUsage), and a caller reading the Result takes
	// for itself.
	Usage Usage
	// Err is the run's own error — the loop's failure, or the cancellation that stopped it
	// before an answer. It is nil on a Firing that reached its answer, even one whose
	// record then failed to save (that failure is the returned error only).
	Err error
}

// Usage is one agent's CUMULATIVE token accounting for a whole run: every completion that
// agent accounted for, itself included. A caller READS it off the latest UsageEvent the agent
// stamped rather than summing the stream (domain.UsageEvent), so the figure is whole even for
// an observer that joined late — and it counts the maintenance work a Compaction fold does,
// which no fill reading shows.
//
// It is per-agent as REPORTED: a sub-agent starts from zero and its totals stay its own on
// Result.SubAgents, so this figure is the top-level agent's alone. The session-wide sum is taken
// once, where the record is written: Once folds the delegated runs' counters into the record's
// Meta.DelegateUsage beside this figure's Meta.Usage, because an unattended record has no Driver
// to take that sum for it. Every counter is zero when nothing accounted for the agent at all — an
// Upstream that reports no usage, or a run that never completed a call.
type Usage struct {
	// Calls is how many completed upstream calls the agent accounted for, Compaction folds
	// included.
	Calls int
	// PromptTokens is the sum of the prompt (context) tokens those calls were charged.
	PromptTokens int
	// CompletionTokens is the sum of the tokens they generated.
	CompletionTokens int
	// TotalTokens is the sum of the totals the SERVER reported for them, folded as reported
	// rather than recomputed from the two parts above, so it stays consistent with the server's
	// own arithmetic (which may count cached or reasoning tokens the split does not show). A
	// server that reports the parts and omits the sum therefore leaves this at zero.
	TotalTokens int
	// CachedPromptTokens is the share of PromptTokens the Upstream answered from its prefix
	// cache, where it reports one (0 on every server that omits the breakdown). It is
	// INFORMATIONAL: it is already counted inside PromptTokens and no bound reads it — a cached
	// prompt token is still context the model reads, only the bill differs.
	CachedPromptTokens int
}

// SubAgentUsage is one finished sub-agent run's context fill and cumulative spend. It exists
// because both are otherwise unobservable to a Firing's caller: the child fills a window of its
// OWN and spends tokens of its own, so the Firing's readings (the record's CtxUsed, Result.Usage)
// say nothing about them, and the child Agent is discarded the moment its run ends.
type SubAgentUsage struct {
	// Used is the run's final fill: the token total of the LAST usage its Turns reported,
	// never a sum across them — each Turn restates the whole fill, it does not add to it.
	Used int
	// Limit is the window that fill sits in: the CHILD's own, as its readings reported it —
	// which for a delegation routed to the Sub-agent server (ADR 0045) is the Delegation
	// target's window and NOT the Firing's, a routed child working against a window of its
	// own. A run whose readings named no window falls back to the Firing's
	// Context.MaxContextTokens, which is the right answer for every unrouted child (it
	// inherits the parent's Config verbatim). 0 means neither named one; a fill only
	// means something beside its limit, so a surface omits the reading rather than
	// spelling it against nothing.
	Limit int
	// Task is the first line of the delegated task, "" when the call carried none. Like
	// FinalText it is RAW model output — a surface escape-strips it at its own render seam.
	Task string
	// Name is the OPTIONAL short name the sub_agent call gave the delegation, "" when it named
	// none — the signal to fall back to Task, which is what every surface did before names
	// existed. RAW model output on the same terms as Task: a surface escape-strips and clips it
	// at its own render seam.
	Name string
	// Model is the model this run went to when that is NOT the model the Firing itself is bound
	// to — a delegation routed to the Sub-agent server (ADR 0045) — and "" when the two match,
	// which is every run with routing off and every same-model target. The comparison is made
	// here rather than at a render seam so the reading carries its own answer to "is this worth
	// saying": a surface prints the field when it is set and adds no cell when it is not, which
	// is the self-hiding rule Used/Limit already answer to.
	//
	// It is a server-reported id (the heartbeat resolves it), so a surface treats it as wire data
	// and makes it line-safe at its own render seam exactly as it does Task and Name.
	Model string

	// The five below are this run's CUMULATIVE accounting, on Usage's terms exactly (its doc
	// carries the semantics): the child's own totals, latest-wins from its own events, counting
	// only the calls IT made. They are spelled flat here rather than reached through a member
	// so a caller reads a run's spend beside the fill it produced. Zero throughout when the
	// child's Upstream reported no usage.
	Calls              int
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	CachedPromptTokens int
}

// Once performs one Firing and returns its Result: it validates the mode, constructs a
// FRESH Agent (no state is carried between Firings — ADR 0033, decision 5), submits the
// prompt, drives the loop to the quiescent boundary, and saves the resulting session
// record once, at completion.
//
// It returns the Result and an error. The two are deliberately separate: a run failure is
// reported on BOTH (Result.Err and the returned error), while a persistence failure after
// a successful run is the returned error alone — the Firing did its work, only the record
// did not land. A failed or cancelled run still saves whatever completed, so an unattended
// run is never silently lost; the record notes the interruption through its ordinary state
// (a snapshot taken mid-task), not through any new record field.
func Once(ctx context.Context, spec Spec) (Result, error) {
	if spec.Config.Mode != domain.ModePlan && spec.Config.Mode != domain.ModeAuto {
		return Result{}, fmt.Errorf("%w (got %q)", ErrMode, spec.Config.Mode)
	}

	now := spec.Now
	if now == nil {
		now = time.Now
	}
	startedAt := now()

	// Pin the delegates that assume a human, whatever the Spec carried: the denier refuses
	// every gate without parking, and nil Asker/Presenter unregister ask_user and
	// present_document. The tap wraps the caller's sink (nil ⇒ a discard) so a Firing has
	// the EventSink construction requires, the record can relight its context gauge and the
	// Result can carry the answer.
	den := &denier{}
	tap := &eventTap{
		inner:  spec.Config.Events,
		window: spec.Config.Context.MaxContextTokens,
		model:  spec.Config.Model,
		// The scrollback is seeded with the prompt about to be submitted: the engine reports no
		// event for a submission, so the fold cannot learn the run's first line from the stream.
		scrollback: newTranscriptFold(spec.Prompt),
	}
	cfg := spec.Config
	cfg.Approver = den
	cfg.Asker = nil
	cfg.Presenter = nil
	cfg.Events = tap

	a, err := agent.New(cfg)
	if err != nil {
		return Result{}, fmt.Errorf("apogee: construct the firing's agent: %w", err)
	}
	defer func() { _ = a.Close() }()

	// The prompt carries the same @file and /skill grammars a chat message does
	// (internal/refs), so a Firing's references resolve in the loop exactly as a session's do.
	in := domain.UserInput{
		Text:     spec.Prompt,
		FileRefs: refs.FileRefs(spec.Prompt),
		SkillIDs: refs.SkillRefs(spec.Prompt, knownSkillID(spec.Config.Skills)),
	}
	if err := a.Submit(in); err != nil {
		return Result{}, fmt.Errorf("apogee: submit the firing's prompt: %w", err)
	}

	step, runErr := a.Run(ctx)
	if runErr == nil && step.Status == domain.StatusCancelled {
		// A cancel is not a loop error, but it IS the reason this Firing has no answer, and
		// an unattended caller has no event stream to learn that from.
		runErr = fmt.Errorf("apogee: the firing was cancelled: %w", context.Cause(ctx))
	}

	res := Result{
		Title:     spec.title(startedAt),
		FinalText: tap.finalText(),
		Turns:     step.TurnIndex + 1,
		Denied:    den.count(),
		Faulted:   step.Faulted,
		Fault:     a.LastFault(),
		SubAgents: tap.subAgentRuns(),
		Usage:     tap.totals(),
		Err:       runErr,
	}
	if spec.Store == nil {
		return res, runErr
	}

	snap, err := a.Snapshot()
	if err != nil {
		return res, errors.Join(runErr, fmt.Errorf("apogee: snapshot the firing: %w", err))
	}
	finishedAt := now().UTC()
	// The caller's id when it named one — it has already keyed something on it (its scratch
	// dir) — else the mint that has always happened here. Nothing validates its shape: Store.Save
	// refuses an id that cannot name a file inside the store, and that refusal is this function's
	// "save the firing's record" error.
	recordID := spec.RecordID
	if recordID == "" {
		recordID = session.NewID(finishedAt)
	}
	rec := session.Record{
		Meta: session.Meta{
			ID:            recordID,
			Title:         res.Title,
			CreatedAt:     startedAt.UTC(),
			UpdatedAt:     finishedAt,
			Workspace:     cfg.WorkspaceDir,
			Model:         cfg.Model,
			ScheduleID:    spec.ScheduleID,
			ScheduleName:  spec.ScheduleName,
			UserMsgs:      1, // a Firing submits exactly one prompt
			CtxUsed:       tap.fill(),
			Usage:         sessionUsage(tap.totals()),
			DelegateUsage: delegateTotals(tap.subAgentRuns()),
		},
		Session: snap,
		// The run's own scrollback, so an unattended record REPLAYS in /sessions rather than
		// taking ADR 0022's no-scrollback degrade path (transcript.go). A blob that could not be
		// spelled comes back nil and degrades exactly as an older record does — never a failed save.
		Transcript: tap.scrollback.blob(),
	}
	if err := spec.Store.Save(rec); err != nil {
		return res, errors.Join(runErr, fmt.Errorf("apogee: save the firing's record: %w", err))
	}
	res.SessionID = rec.Meta.ID
	return res, runErr
}

// sessionUsage restates a Firing's own accounting in the record's shape. run.Usage and
// session.Usage carry the same five counters in a DIFFERENT field order, so the conversion is
// written out field by field on purpose: a positional literal would transpose them silently.
func sessionUsage(u Usage) session.Usage {
	return session.Usage{
		Calls:              u.Calls,
		PromptTokens:       u.PromptTokens,
		CachedPromptTokens: u.CachedPromptTokens,
		CompletionTokens:   u.CompletionTokens,
		TotalTokens:        u.TotalTokens,
	}
}

// delegateTotals sums the five flat counters of every finished sub-agent run into the single
// figure Meta.DelegateUsage holds. It is the ONE roll-up this package takes, and it is taken
// here rather than left to a caller because an unattended record has no other producer: the
// Firing's caller is a scheduler, not a Driver holding run heads, and the /sessions spend cell
// reads Meta.Usage + Meta.DelegateUsage off the record alone. Per-run detail is not lost by
// summing — Result.SubAgents still carries it, entry by entry, to a caller that wants it.
// A Firing that delegated nothing sums to the zero Usage, which omitzero keeps out of the JSON.
func delegateTotals(runs []SubAgentUsage) session.Usage {
	var total session.Usage
	for _, r := range runs {
		total.Calls += r.Calls
		total.PromptTokens += r.PromptTokens
		total.CachedPromptTokens += r.CachedPromptTokens
		total.CompletionTokens += r.CompletionTokens
		total.TotalTokens += r.TotalTokens
	}
	return total
}

// knownSkillID is the catalog membership test refs.SkillSpans needs to tell a skill token
// apart from prose — the Firing's counterpart to the TUI's own, built off the SAME resolver the
// loop will inject the body through, so a token this test accepts is a token the loop can
// resolve. A nil resolver returns nil, which SkillSpans reads as "no catalog is wired": no "/"
// token is a reference then, and the prompt travels exactly as it did before this seam existed.
func knownSkillID(r domain.SkillResolver) func(string) bool {
	if r == nil {
		return nil
	}
	return func(id string) bool { return len(r.ResolveSkills([]string{id})) == 1 }
}

// title is the record's title for this Firing: the Spec's own when set, else the Schedule
// form "<name> — <HH:MM>", else the first-prompt heuristic.
//
// Now is an injectable seam, so the instant handed in carries whatever zone a Driver's clock
// is located in — the Scheduler's own is UTC-located under test. Which zone a derived title is
// SPELLED in is therefore a decision each derived form makes FOR ITSELF, on its own line below,
// never by inheriting the neighbour's: the two forms serve different callers (a Schedule's
// Firing vs. any Once), so a future change to one must not silently move the other. They happen
// to agree on local today; they agree by two stated choices, not by one shared conversion. The
// stamps beside the title (Meta.CreatedAt / UpdatedAt) stay UTC, untouched by either.
func (s Spec) title(now time.Time) string {
	if s.Title != "" {
		return s.Title
	}
	if s.ScheduleName != "" {
		// The Schedule form is LOCAL: a human reads a schedule's runs against the same wall clock
		// they set the schedule by, and the record's stamps carry the UTC truth beside it.
		return s.ScheduleName + " — " + now.Local().Format("15:04")
	}
	// The generic Once fallback is LOCAL as well, by its own reasoning: its dated label is read in
	// /sessions by the person who ran it, and "which day was that?" is their day, not the day the
	// caller's clock happened to be located in. title.Derive formats what it is given and never
	// relocates it, so this line is the whole of the zone choice for this path. Its cap is the
	// browser's own, so a Firing's row is no wider than any other session's.
	return title.Derive(s.Prompt, title.MaxRunes, now.Local())
}

// denier is a Firing's Approver: it refuses every gated action immediately and counts the
// refusal. The engine records the refusal itself — it emits an ApprovalEvent carrying the
// request (with the human-facing reason the ladder computed) and this deny verdict — so the
// reason is on the record without this type inventing a channel for it. Refusing rather
// than blocking is the whole point: a Firing has nobody to ask, so a gate must FAIL, never
// park (the headless-Asker pattern, CONTEXT.md → Ask).
//
// The mutex is defensive rather than load-bearing: the engine consults the Approver
// synchronously on the goroutine driving the loop, and count() runs after that goroutine
// has returned, but the counter is cheap to make safe for any caller that drives Once
// differently.
type denier struct {
	mu sync.Mutex
	n  int
}

// Approve refuses the call and counts it.
func (d *denier) Approve(context.Context, domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	d.mu.Lock()
	d.n++
	d.mu.Unlock()
	return domain.ApprovalDeny, nil
}

// count reports how many gated actions were refused.
func (d *denier) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

// eventTap is the EventSink Once installs: it forwards every Event to the caller's sink
// (nil ⇒ discarded, so a Firing needs no observer to satisfy Config.Events) and catches the
// facts the Result cannot recover once the run is over — the latest top-level UsageEvent's
// token total and cumulative totals, the latest top-level MessageEvent's text, and the final
// token total and cumulative totals of each SUB-AGENT run.
//
// The cumulative halves ride along the readings they accompany, on the same keying and the
// same latest-wins rule (noteUsage states how the two differ where they differ): the top-level
// one becomes Result.Usage, each run's own lands on its Result.SubAgents entry.
//
// The top-level total becomes the record's CtxUsed, so a resumed Firing relights the context
// gauge exactly as an interactive session does; the text becomes Result.FinalText, the
// Firing's answer. Neither is read back off the snapshot: the fill is not in the snapshot at
// all, and the answer must reach an UNPERSISTED run's Result too (there is no snapshot on
// that path). Both cost one type assertion to catch in flight.
//
// Those first two readings stay top-level only (Depth == 0): a sub-agent's usage fills a
// window of its own rather than its parent's, and a sub-agent's message is reported back to
// its parent, never to the Firing's caller. The third reading is the deeper half of that same
// fact, not an exception to it — a delegated run's fill is real, is nobody else's, and dies
// with the child Agent unless it is caught here. So the tap BRACKETS each run BY THE CALL THAT
// ASKED FOR IT: the delegating sub_agent ToolCallEvent opens a bracket under its call id, the
// child's usage — stamped with that same id as its run identity (domain.EventBase.CallID) —
// updates it, and the tool result closing that call closes the bracket into Result.SubAgents.
//
// The call id is what makes the bracketing survive CONCURRENT delegation (ADR 0039): siblings
// spawned by one reply share a depth, so a depth-keyed bracket would braid their fills
// together and report whichever landed last as both. Each run's identity is its own, so
// nothing accrues across runs — a nested run's fill never lands on the run that spawned it,
// and a reading with no matching bracket is dropped.
type eventTap struct {
	inner domain.EventSink
	// window is the Firing's context window, the FALLBACK stamped onto a finished run's reading
	// when the run's own readings named none: an unrouted sub-agent inherits the parent's Config
	// verbatim, so its limit IS this number, while a routed one (ADR 0045) reports its own.
	window int
	// model is the Firing's own bound model — the yardstick a child's model is measured
	// against, never a value reported on its own. It is what makes SubAgentUsage.Model mean
	// "different from the session's" rather than "whatever this run used".
	model string
	// scrollback is the Firing's own transcript fold — the entries the saved record replays from
	// (transcript.go). It carries its own lock, so nothing here takes the tap's for it.
	scrollback *transcriptFold

	mu    sync.Mutex
	total int
	// usage is the top-level agent's latest cumulative reading — its running totals for the
	// whole Firing, which become Result.Usage.
	usage Usage
	final string
	// open holds the in-flight sub-agent runs, keyed by the id of the delegating call that
	// opened each one; runs is the finished ones in finish order.
	open map[string]*openSubAgent
	runs []SubAgentUsage
}

// openSubAgent is one sub-agent run in flight: the task it was given, the optional name it was
// given with it, the latest fill its own Turns have reported, the model and window those readings
// came from, and its latest cumulative reading. The delegating call that will close it is the map
// key it is filed under, not a member — one run, one identity.
//
// model is held RAW and measured against the Firing's only when the run closes: the "is it worth
// saying" question belongs to the reading that gets filed (SubAgentUsage.Model), not to every
// event that updates one. window is held on the same terms and resolved against the Firing's at
// the same moment, though as a fallback rather than a yardstick (SubAgentUsage.Limit).
type openSubAgent struct {
	task   string
	name   string
	used   int
	model  string
	window int
	usage  Usage
}

// Emit records a top-level usage total and answer, tracks the sub-agent runs that pass
// through, folds the event into the Firing's scrollback, and forwards the event unchanged.
func (t *eventTap) Emit(e domain.Event) {
	// The scrollback fold runs beside the extraction below rather than inside it: the two read the
	// same stream for different reasons — one accumulates readings, the other a sequence — and a
	// fold folded into the switch would have to grow a case for every variant either one wants.
	if t.scrollback != nil {
		t.scrollback.fold(e)
	}
	switch ev := e.(type) {
	case domain.UsageEvent:
		t.noteUsage(ev)
	case domain.MessageEvent:
		if ev.Depth != 0 {
			break
		}
		t.mu.Lock()
		t.final = ev.Text
		t.mu.Unlock()
	case domain.ToolCallEvent:
		if ev.Call.Tool == tools.SubAgentToolName {
			t.openSubAgentRun(ev.Call)
		}
	case domain.ToolResultEvent:
		// A tool result carries no tool NAME, so the call id is what identifies it as the
		// delegation's: only the result closing the call that opened the bracket closes it.
		t.closeSubAgentRun(ev.Result.CallID)
	}
	if t.inner != nil {
		t.inner.Emit(e)
	}
}

// noteUsage files one accounting event under the agent that reported it: the Firing's own at
// depth 0, else the sub-agent run the event's own run identity names (callID — the delegating
// call that spawned the reporting agent). An event with no run to belong to is dropped.
//
// TWO readings travel on one event and they fold on different rules. The FILL is the Turn's
// restatement of the whole window, so the latest one wins (a Turn restates rather than adds)
// and a Maintenance event must not touch it: that event is the Compaction call, whose prompt
// counts describe the summarizer's own request and not the conversation (domain.UsageEvent).
// The CUMULATIVE totals are the emitting agent's running counters, already summed by the
// engine, so the latest event wins there too — and a Maintenance event DOES carry them, which
// is exactly what keeps a Firing's totals honest across a fold.
//
// Each reading is taken only when it says something: a zero fill is the absence of a fill and
// a reading that counted no call is the absence of accounting (a pre-feature event stream, an
// Upstream that reports no usage), so neither overwrites what an earlier event established.
func (t *eventTap) noteUsage(ev domain.UsageEvent) {
	// Prefer the server's total; fall back to prompt+completion when it omits the sum (the same
	// degrade the interactive gauge applies).
	fill := ev.TotalTokens
	if fill == 0 {
		fill = ev.PromptTokens + ev.CompletionTokens
	}
	countsFill := fill > 0 && !ev.Maintenance
	cumulative := Usage{
		Calls:              ev.CumulativeCalls,
		PromptTokens:       ev.CumulativePromptTokens,
		CompletionTokens:   ev.CumulativeCompletionTokens,
		TotalTokens:        ev.CumulativeTotalTokens,
		CachedPromptTokens: ev.CumulativeCachedPromptTokens,
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.Depth == 0 {
		if countsFill {
			t.total = fill
		}
		if cumulative.Calls > 0 {
			t.usage = cumulative
		}
		return
	}
	run := t.open[ev.CallID]
	if run == nil {
		return
	}
	if countsFill {
		run.used = fill
	}
	// The model travels with the reading and is taken whenever the event names one, fill or no
	// fill: a child that only ever reported a maintenance reading still ran on the model it ran
	// on, and an event from an agent bound before its heartbeat names none and leaves the last
	// answer standing rather than blanking it.
	if ev.Model != "" {
		run.model = ev.Model
	}
	// The window rides the reading for the same reason and is taken on the same terms: a routed
	// child fills the Delegation target's window (ADR 0045), and a reading that names none leaves
	// the last answer standing rather than dropping the run back onto the Firing's.
	if ev.ContextWindow > 0 {
		run.window = ev.ContextWindow
	}
	if cumulative.Calls > 0 {
		run.usage = cumulative
	}
}

// openSubAgentRun starts the bracket for the run call is delegating, filed under that call's
// own id — the identity the spawned agent will stamp on every event it emits. A second
// delegation opens a bracket of its own, whether it starts after the first one closed or
// beside it.
func (t *eventTap) openSubAgentRun(call domain.ToolCall) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil {
		t.open = make(map[string]*openSubAgent)
	}
	t.open[call.ID] = &openSubAgent{
		task: firstTaskLine(call.Arguments),
		name: delegationName(call.Arguments),
	}
}

// closeSubAgentRun finishes the run callID delegated, appending its reading in finish order. A
// result for any OTHER call — a plain tool the same Turn ran, or a leaf tool the child itself
// ran — matches no bracket and leaves them all alone, and a run that reported no usage at all
// is dropped: a zero fill is the absence of a reading, not a reading of zero.
func (t *eventTap) closeSubAgentRun(callID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.open[callID]
	if run == nil {
		return
	}
	delete(t.open, callID)
	if run.used <= 0 {
		return
	}
	t.runs = append(t.runs, SubAgentUsage{
		Used:               run.used,
		Limit:              runWindow(run.window, t.window),
		Task:               run.task,
		Name:               run.name,
		Model:              differingModel(run.model, t.model),
		Calls:              run.usage.Calls,
		PromptTokens:       run.usage.PromptTokens,
		CompletionTokens:   run.usage.CompletionTokens,
		TotalTokens:        run.usage.TotalTokens,
		CachedPromptTokens: run.usage.CachedPromptTokens,
	})
}

// differingModel answers SubAgentUsage.Model: the child's model when it is not the Firing's, and
// "" when it is (or when either side is unknown, which is a question no reading can answer). It is
// the one place the "worth saying" rule lives on this Driver, so a surface never has to hold the
// session's model to decide whether a child's is news.
func differingModel(child, firing string) string {
	if child == "" || child == firing {
		return ""
	}
	return child
}

// runWindow answers SubAgentUsage.Limit: the window the child's own readings named — the Delegation
// target's for a routed run (ADR 0045) — and the Firing's where they named none. It is the fill's
// counterpart to differingModel above and inverts its rule on purpose: a model the reading does not
// name is nothing to say, while a window it does not name is the Firing's own, an unrouted child
// inheriting the parent's Config verbatim.
func runWindow(child, firing int) int {
	if child > 0 {
		return child
	}
	return firing
}

// firstTaskLine reads the sub_agent call's task argument and returns its first line, "" when
// the arguments are malformed or name no task. It is the gist the TUI puts on a delegation's
// branch row; the JSON decode is this package's, the first-line rule is title.FirstLine's.
func firstTaskLine(args json.RawMessage) string {
	var decoded struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(args, &decoded); err != nil {
		return ""
	}
	return title.FirstLine(decoded.Task)
}

// delegationName reads the sub_agent call's OPTIONAL name argument and normalises it the way the
// recursion point does (title.FirstLine, the one rule both apply): "" when the arguments are
// malformed or name none, which is the delegation-is-unnamed signal every surface reads as "fall
// back to the task". It sits beside firstTaskLine and shares its shape — the JSON decode here, the
// first-line rule below.
func delegationName(args json.RawMessage) string {
	var decoded struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &decoded); err != nil {
		return ""
	}
	return title.FirstLine(decoded.Name)
}

// subAgentRuns reports the runs that finished with a reading, in finish order; nil when the
// Firing delegated nothing (or nothing it delegated reported usage).
func (t *eventTap) subAgentRuns() []SubAgentUsage {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.runs) == 0 {
		return nil
	}
	return append([]SubAgentUsage(nil), t.runs...)
}

// totals reports the Firing's own cumulative accounting — the latest reading its TOP-LEVEL
// agent stamped, Compaction folds included — zero throughout when nothing accounted for it.
func (t *eventTap) totals() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage
}

// fill reports the last observed context fill, 0 when the Upstream reported no usage.
func (t *eventTap) fill() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

// finalText reports the last observed top-level assistant message, "" when the run produced
// none.
func (t *eventTap) finalText() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.final
}
