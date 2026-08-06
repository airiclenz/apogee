package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
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
	// Config is the agent construction surface. Its Approver, Asker, Presenter and Events
	// fields are replaced by Once; everything else is used as given.
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
	// SubAgents reports how full each delegated run's context got, one entry per sub-agent
	// run, in FINISH order (so a nested run precedes the run that spawned it). It is flat:
	// the entries carry no nesting, because a reading belongs to exactly one run and never
	// rolls up. Runs that reported no usage at all are absent, and a Firing that delegated
	// nothing carries none.
	SubAgents []SubAgentUsage
	// Err is the run's own error — the loop's failure, or the cancellation that stopped it
	// before an answer. It is nil on a Firing that reached its answer, even one whose
	// record then failed to save (that failure is the returned error only).
	Err error
}

// SubAgentUsage is one finished sub-agent run's context fill. It exists because that fill is
// otherwise unobservable to a Firing's caller: the child fills a window of its OWN, so the
// Firing's reading (the record's CtxUsed) says nothing about it, and the child Agent is
// discarded the moment its run ends.
type SubAgentUsage struct {
	// Used is the run's final fill: the token total of the LAST usage its Turns reported,
	// never a sum across them — each Turn restates the whole fill, it does not add to it.
	Used int
	// Limit is the window that fill sits in: the Firing's own Context.MaxContextTokens,
	// which a sub-agent inherits verbatim. 0 means the Config named no window; a fill only
	// means something beside its limit, so a surface omits the reading rather than
	// spelling it against nothing.
	Limit int
	// Task is the first line of the delegated task, "" when the call carried none. Like
	// FinalText it is RAW model output — a surface escape-strips it at its own render seam.
	Task string
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
	tap := &eventTap{inner: spec.Config.Events, window: spec.Config.Context.MaxContextTokens}
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

	if err := a.Submit(domain.UserInput{Text: spec.Prompt}); err != nil {
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
		SubAgents: tap.subAgentRuns(),
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
	rec := session.Record{
		Meta: session.Meta{
			ID:           session.NewID(finishedAt),
			Title:        res.Title,
			CreatedAt:    startedAt.UTC(),
			UpdatedAt:    finishedAt,
			Workspace:    cfg.WorkspaceDir,
			Model:        cfg.Model,
			ScheduleID:   spec.ScheduleID,
			ScheduleName: spec.ScheduleName,
			UserMsgs:     1, // a Firing submits exactly one prompt
			CtxUsed:      tap.fill(),
		},
		Session: snap,
	}
	if err := spec.Store.Save(rec); err != nil {
		return res, errors.Join(runErr, fmt.Errorf("apogee: save the firing's record: %w", err))
	}
	res.SessionID = rec.Meta.ID
	return res, runErr
}

// titleMax bounds a derived title at the same 50 runes the interactive browser's titles
// take, so a Firing's row is no wider than any other session's.
const titleMax = 50

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
	// caller's clock happened to be located in. promptTitle formats what it is given and never
	// relocates it, so this line is the whole of the zone choice for this path.
	return promptTitle(s.Prompt, now.Local())
}

// promptTitle derives a one-line title from the prompt: the first line as-is when it fits,
// otherwise truncated to titleMax runes at the last word boundary past 60% (falling back to
// a hard cut) and closed with an ellipsis. A prompt that is empty or opens a code fence has
// no useful title, so it falls back to a dated label — every record still gets one. It formats
// now in whatever zone now already carries: the zone is its caller's stated choice (see title),
// never this function's, so nothing here can move a caller's spelling from under it.
//
// It duplicates the interactive browser's heuristic rather than sharing it: that one lives
// in internal/tui, which this package must not import (ADR 0010). The duplication is one
// small pure function and keeps the dependency arrow pointing down.
func promptTitle(text string, now time.Time) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "```") {
		return "Session " + now.Format("2006-01-02")
	}
	firstLine := trimmed
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		firstLine = trimmed[:i]
	}
	runes := []rune(firstLine)
	if len(runes) <= titleMax {
		return firstLine
	}
	truncated := string(runes[:titleMax])
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > titleMax*6/10 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "…"
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
// three facts the Result cannot recover once the run is over — the latest top-level
// UsageEvent's token total, the latest top-level MessageEvent's text, and the final token
// total of each SUB-AGENT run.
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
// with the child Agent unless it is caught here. So the tap BRACKETS each run: the delegating
// sub_agent ToolCallEvent at depth d opens a bracket for child depth d+1, usage at depth d+1
// updates it, and the tool result closing that same call closes it into Result.SubAgents.
// Nothing accrues across depths — a usage event only ever reaches the bracket at its OWN
// depth — so a nested run's fill never lands on the run that spawned it.
//
// One bracket per depth suffices because delegation is SERIALIZED (ADR 0014): a second
// delegation at a given depth cannot begin before the first one's result has landed. Should
// concurrent fan-out ever land, UsageEvent would need a run identity and this bracketing
// would have to follow it; until then an event with no matching bracket is dropped.
type eventTap struct {
	inner domain.EventSink
	// window is the Firing's context window, stamped onto each finished run's reading: a
	// sub-agent inherits the parent's Config verbatim, so its limit IS this number.
	window int

	mu    sync.Mutex
	total int
	final string
	// open holds the in-flight sub-agent run per child depth; runs is the finished ones in
	// finish order.
	open map[int]*openSubAgent
	runs []SubAgentUsage
}

// openSubAgent is one sub-agent run in flight: the delegating call that will close it, the
// task it was given, and the latest fill its own Turns have reported.
type openSubAgent struct {
	callID string
	task   string
	used   int
}

// Emit records a top-level usage total and answer, tracks the sub-agent runs that pass
// through, and forwards the event unchanged.
func (t *eventTap) Emit(e domain.Event) {
	switch ev := e.(type) {
	case domain.UsageEvent:
		// Prefer the server's total; fall back to prompt+completion when it omits the sum
		// (the same degrade the interactive gauge applies).
		total := ev.TotalTokens
		if total == 0 {
			total = ev.PromptTokens + ev.CompletionTokens
		}
		if total > 0 {
			t.noteUsage(ev.Depth, total)
		}
	case domain.MessageEvent:
		if ev.Depth != 0 {
			break
		}
		t.mu.Lock()
		t.final = ev.Text
		t.mu.Unlock()
	case domain.ToolCallEvent:
		if ev.Call.Tool == tools.SubAgentToolName {
			t.openSubAgentRun(ev.Depth+1, ev.Call)
		}
	case domain.ToolResultEvent:
		// A tool result carries no tool NAME, so the call id is what identifies it as the
		// delegation's: only the result closing the call that opened the bracket closes it.
		t.closeSubAgentRun(ev.Depth+1, ev.Result.CallID)
	}
	if t.inner != nil {
		t.inner.Emit(e)
	}
}

// noteUsage files one Turn's token total under the agent that reported it: the Firing's own
// at depth 0, else the sub-agent run open at that depth. The latest total wins — a Turn
// restates the whole fill rather than adding to it — and a total with no run to belong to is
// dropped.
func (t *eventTap) noteUsage(depth, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if depth == 0 {
		t.total = total
		return
	}
	if run := t.open[depth]; run != nil {
		run.used = total
	}
}

// openSubAgentRun starts the bracket for the run call is delegating, which will report at
// childDepth. An unfinished bracket already at that depth cannot happen while delegation is
// serialized, and replacing it is the safe read of that contract having moved.
func (t *eventTap) openSubAgentRun(childDepth int, call domain.ToolCall) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil {
		t.open = make(map[int]*openSubAgent)
	}
	t.open[childDepth] = &openSubAgent{callID: call.ID, task: firstTaskLine(call.Arguments)}
}

// closeSubAgentRun finishes the run at childDepth when callID is the delegating call's,
// appending its reading in finish order. A result for any OTHER call — a plain tool the same
// Turn ran — leaves the bracket alone, and a run that reported no usage at all is dropped: a
// zero fill is the absence of a reading, not a reading of zero.
func (t *eventTap) closeSubAgentRun(childDepth int, callID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.open[childDepth]
	if run == nil || run.callID != callID {
		return
	}
	delete(t.open, childDepth)
	if run.used <= 0 {
		return
	}
	t.runs = append(t.runs, SubAgentUsage{Used: run.used, Limit: t.window, Task: run.task})
}

// firstTaskLine reads the sub_agent call's task argument and returns its first line, "" when
// the arguments are malformed or name no task. It is the gist the TUI puts on a delegation's
// branch row, derived here rather than shared because internal/tui sits above this package
// and cannot be imported from it (ADR 0010) — the same reason promptTitle is duplicated.
func firstTaskLine(args json.RawMessage) string {
	var decoded struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(args, &decoded); err != nil {
		return ""
	}
	line := decoded.Task
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
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
