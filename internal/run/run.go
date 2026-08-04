package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
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
	// Err is the run's own error — the loop's failure, or the cancellation that stopped it
	// before an answer. It is nil on a Firing that reached its answer, even one whose
	// record then failed to save (that failure is the returned error only).
	Err error
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
	tap := &eventTap{inner: spec.Config.Events}
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
// form "<name> — <HH:MM>" in LOCAL time (a human reads a schedule's runs in their own
// clock, and the record's timestamps carry the UTC truth), else the first-prompt heuristic.
//
// The local conversion is FORCED here rather than inherited from the instant handed in: Now
// is an injectable seam, a Driver's clock may hand this a UTC-located time (the Scheduler's
// own does under test), and a title is the one thing on a record a human reads as a clock.
// Which zone it is SPELLED in is therefore this function's decision, not its caller's — while
// the stamps beside it (Meta.CreatedAt / UpdatedAt) stay UTC, untouched by this.
func (s Spec) title(now time.Time) string {
	if s.Title != "" {
		return s.Title
	}
	if s.ScheduleName != "" {
		return s.ScheduleName + " — " + now.Local().Format("15:04")
	}
	return promptTitle(s.Prompt, now)
}

// promptTitle derives a one-line title from the prompt: the first line as-is when it fits,
// otherwise truncated to titleMax runes at the last word boundary past 60% (falling back to
// a hard cut) and closed with an ellipsis. A prompt that is empty or opens a code fence has
// no useful title, so it falls back to a dated label — every record still gets one.
//
// It duplicates the interactive browser's heuristic rather than sharing it: that one lives
// in internal/tui, which this package must not import (ADR 0010). The duplication is one
// small pure function and keeps the dependency arrow pointing down.
func promptTitle(text string, now time.Time) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "```") {
		// Local for the same reason the clock form is (title): the date a human reads is the
		// date it was where they are, not the one the caller's clock happened to carry.
		return "Session " + now.Local().Format("2006-01-02")
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
// two facts the Result cannot recover once the run is over — the latest top-level
// UsageEvent's token total and the latest top-level MessageEvent's text.
//
// The total becomes the record's CtxUsed, so a resumed Firing relights the context gauge
// exactly as an interactive session does; the text becomes Result.FinalText, the Firing's
// answer. Neither is read back off the snapshot: the fill is not in the snapshot at all,
// and the answer must reach an UNPERSISTED run's Result too (there is no snapshot on that
// path). Both cost one type assertion to catch in flight.
//
// Both readings are top-level only (Depth == 0): a sub-agent's usage belongs to its
// parent's Turn, and a sub-agent's message is reported back to its parent, never to the
// Firing's caller.
type eventTap struct {
	inner domain.EventSink

	mu    sync.Mutex
	total int
	final string
}

// Emit records a top-level usage total and answer, and forwards the event unchanged.
func (t *eventTap) Emit(e domain.Event) {
	switch ev := e.(type) {
	case domain.UsageEvent:
		if ev.Depth != 0 {
			break
		}
		// Prefer the server's total; fall back to prompt+completion when it omits the sum
		// (the same degrade the interactive gauge applies).
		total := ev.TotalTokens
		if total == 0 {
			total = ev.PromptTokens + ev.CompletionTokens
		}
		if total > 0 {
			t.mu.Lock()
			t.total = total
			t.mu.Unlock()
		}
	case domain.MessageEvent:
		if ev.Depth != 0 {
			break
		}
		t.mu.Lock()
		t.final = ev.Text
		t.mu.Unlock()
	}
	if t.inner != nil {
		t.inner.Emit(e)
	}
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
