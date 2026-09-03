package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// The Firing's own scrollback fold: the bounded projection from the engine's Event stream onto
// [session.Entry], so an unattended record REPLAYS in /sessions instead of taking ADR 0022's
// no-scrollback degrade path. It is this Driver's counterpart to the TUI's transcript fold and
// deliberately a much smaller one — ADR 0031's north star says every Driver writes the same
// neutral blob, not that every Driver writes the same richness into it.
//
// What it folds is exactly what the STREAM says: the submitted prompt, committed assistant text,
// tool calls with their bounded arguments, tool results, errors, and the note a pruning pass
// leaves behind (domain.PruneEvent) — each at the Depth that emitted it and stamped with the
// delegating call that spawned that run, so a delegate's work replays nested under its head
// rather than flattened into the Firing's own.
//
// What it does NOT fold is anything a PRESENTER decided. A tool card's label, verb, target, its
// promoted outcome phrase and the stacked body beneath it are verdicts the TUI's presenter reaches
// while painting; the runner never paints, so a runner-written card carries the call's raw name and
// nothing invented beside it. A surface replaying such a record falls back to the same raw-name
// label it falls back to for any card with no friendly one. The rule is one-directional: where the
// stream lacks a fact an entry needs, the entry (or the member) is omitted, never synthesized.
//
// The Firing-closure kinds the wire knows ([session.EntryKindSchedule], the host notes and the
// presented-document entry) are for the same reason absent: a Firing's stream carries no schedule
// block — that entry belongs to the PARENT session that launched the Firing, not to the Firing's
// own scrollback — and a Firing pins its Asker and Presenter off, so neither kind has a fact to
// fold from.

// boundArgsFieldCap is the largest a single string value may be before [boundArgs] replaces it
// with its own size. It MIRRORS internal/tui/wireargs.go's wireArgsFieldCap and must keep its
// value: internal/run may not import internal/tui (ADR 0010 — a Driver-neutral runner reaches no
// Driver), and wireArgs is unexported with paint-path callers only, so the two constants are
// mirrored rather than shared. A record's arguments must read the same whichever Driver wrote it.
const boundArgsFieldCap = 1024

// boundArgsCap is the largest a whole call's stored arguments may be, mirroring
// internal/tui/wireargs.go's wireArgsCap on the same terms as the field cap above. A payload still
// over it after the per-field elisions is stored as its own size instead: the record is a review
// aid, not a transport, and one pathological call must not dominate an unattended session file.
const boundArgsCap = 4096

// transcriptFold accumulates one Firing's scrollback while the run is in flight. It is owned by
// [eventTap], which folds each Event into it as the Event passes, and read once at the save site.
//
// Its own mutex guards it rather than the tap's, because the two hold unrelated state: the tap
// accumulates READINGS (the latest fill, the open delegation brackets) while this accumulates a
// SEQUENCE, and a fold that grew a second reason to take the tap's lock would tie the record's
// shape to the accounting's.
type transcriptFold struct {
	mu      sync.Mutex
	entries []session.Entry
}

// newTranscriptFold starts a Firing's scrollback with the one entry no Event ever reports: the
// prompt the caller submitted. The engine emits nothing for a submission — it is the caller's own
// act — so a fold that waited for the stream would replay a conversation whose first line is the
// model answering a question nobody asked. An empty prompt seeds nothing.
func newTranscriptFold(prompt string) *transcriptFold {
	f := &transcriptFold{}
	if prompt != "" {
		f.entries = append(f.entries, session.Entry{Kind: session.EntryKindUser, Text: prompt})
	}
	return f
}

// fold appends what one Event contributes to the scrollback, or nothing at all. Every variant the
// stream carries that this fold does not name — readings, phases, approvals, mechanism firings,
// Floor-guard firings, the token stream behind a committed message — contributes nothing on
// purpose: a reading is not a block, and the streamed tokens are the message that MessageEvent then
// commits in full.
//
// A domain.FloorGuardEvent is named here to say it is left out deliberately, not overlooked: a
// guard repairing the model's own failure is engine behaviour the reader of a record is owed
// nothing about, exactly as a Mechanism firing is, while the PruneEvent below changes what the
// conversation still holds and earns its note (ADR 0071).
func (f *transcriptFold) fold(e domain.Event) {
	switch ev := e.(type) {
	case domain.MessageEvent:
		f.appendText(session.EntryKindAssistant, ev.Text, ev.EventBase)
	case domain.ToolCallEvent:
		f.appendToolCall(ev)
	case domain.ToolResultEvent:
		f.appendToolResult(ev)
	case domain.ErrorEvent:
		// The TUI words an error entry the same way (transcript.addError): the source that failed,
		// then what it said. A record written by either Driver reads alike.
		f.appendText(session.EntryKindError, ev.Source+": "+ev.Err, ev.EventBase)
	case domain.PruneEvent:
		// A note, not an error: the engine dropped tool results it had already acted on, which is
		// housekeeping the reader of the record is owed a line about. Worded exactly as the TUI
		// words it (transcript.addPrune) and rendered verbatim from the event, so neither Driver
		// holds a chars-per-token ratio of its own.
		f.appendText(session.EntryKindNote,
			fmt.Sprintf("pruned %d tool results (~%d tokens)", ev.Results, ev.Tokens), ev.EventBase)
	}
}

// appendText appends one text-carrying entry of kind, attributed to the run that emitted it. An
// empty text appends nothing — a committed message with no words is the absence of a message, not
// a blank block in the scrollback.
func (f *transcriptFold) appendText(kind, text string, base domain.EventBase) {
	if text == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, session.Entry{
		Kind:        kind,
		Text:        text,
		Depth:       base.Depth,
		SpawnCallID: spawnOf(base),
	})
}

// appendToolCall records the call the model asked for: its id (what the result pairs back by), the
// raw tool name, and the bounded copy of the arguments it sent. The entry is stored OPEN — Done
// stays false until appendToolResult closes it — which is what makes a record written while a
// delegation was still running replay as interrupted rather than as finished
// ([session.CloseInterruptedCalls]).
func (f *transcriptFold) appendToolCall(ev domain.ToolCallEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, session.Entry{
		Kind:        session.EntryKindToolCall,
		Depth:       ev.Depth,
		CallID:      ev.Call.ID,
		SpawnCallID: spawnOf(ev.EventBase),
		Tool: &session.ToolView{
			Name: ev.Call.Tool,
			Args: boundArgs(ev.Call.Arguments),
		},
	})
}

// appendToolResult closes the call the result answers and appends the result's own entry. The two
// halves are one act: a call left open would replay as interrupted work even though its result is
// in the very same record, and a result with no call to close is still worth keeping — a stray
// result is exactly what an honest scrollback should show.
//
// The result's text is worded as the TUI's own orphan branch words it (transcript.addToolResult):
// an error leads with "error: ". No escape stripping happens here — the blob is stripped on the way
// back OUT ([session.DecodeTranscript]), which is the seam that owns that defence now that more
// than one Driver writes the record.
func (f *transcriptFold) appendToolResult(ev domain.ToolResultEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := &f.entries[i]
		if e.Kind == session.EntryKindToolCall && !e.Done && e.CallID == ev.Result.CallID {
			e.Done = true
			break
		}
	}
	text := ev.Result.Content
	if ev.Result.IsError {
		text = "error: " + text
	}
	if text == "" {
		return
	}
	f.entries = append(f.entries, session.Entry{
		Kind:        session.EntryKindToolResult,
		Text:        text,
		Depth:       ev.Depth,
		SpawnCallID: spawnOf(ev.EventBase),
	})
}

// blob returns the Firing's scrollback as the versioned wire form the record keeps, or nil when
// there is nothing to keep or the encode failed.
//
// An encode failure DEGRADES rather than fails the save (ADR 0022's degrade posture): the record's
// engine envelope is the resumable half, and a run whose scrollback could not be spelled is still a
// run worth keeping — it simply replays with the no-scrollback note an older record replays with.
// The failure is swallowed rather than reported because this package is wire-silent and has no sink
// of its own for it: Config.Events belongs to the run, and the run is over by the time this is read.
func (f *transcriptFold) blob() json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		return nil
	}
	data, err := session.EncodeTranscript(f.entries)
	if err != nil {
		return nil
	}
	return data
}

// spawnOf reports the run identity a delegated event carries: the id of the sub_agent call that
// spawned the agent emitting it. A top-level event carries none — its CallID names the call it is
// ABOUT, not a run — so the member stays empty and the entry replays at the Firing's own level.
func spawnOf(base domain.EventBase) string {
	if base.Depth == 0 {
		return ""
	}
	return base.CallID
}

// boundArgs returns the bounded, compact JSON a Firing's record keeps as one tool call's
// arguments, or nil where there is nothing worth keeping. It mirrors internal/tui/wireArgs — the
// same two caps, the same per-field elision, the same UseNumber decode so a large integer is
// stored as the model spelled it rather than re-spelled through a float64 — with one deliberate
// difference: the TUI DROPS the content-carrying keys of the write/edit tools because its card's
// Regions and Details already carry what the edit did (ADR 0052), and a runner-written card carries
// neither, so dropping them here would lose the edit from the record entirely. The field cap
// elides an oversized body anyway, which is the bound that matters.
//
// Nothing here is sanitised: this form is stored, never painted, and the surface that later shows
// it is the surface that must strip it — the same store-only call the TUI's copy states.
func boundArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return nil // not a JSON object — a fragment, a bare array — is nothing a record can keep
	}
	if len(args) == 0 {
		return nil
	}
	for key, value := range args {
		args[key] = boundedArgValue(value)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil // a value no encoder can spell is a value no record can keep
	}
	if len(encoded) > boundArgsCap {
		return json.RawMessage(fmt.Sprintf(`{"elided":"%d bytes"}`, len(encoded)))
	}
	return json.RawMessage(encoded)
}

// boundedArgValue returns value with every over-long string in it replaced by its own size. It
// recurses through objects and arrays so a long string nested inside one is bounded where it sits,
// rather than being left to push the whole call over [boundArgsCap] and collapse the useful keys
// beside it. It mirrors internal/tui/wireargs.go's boundedArgValue.
func boundedArgValue(value any) any {
	switch typed := value.(type) {
	case string:
		if len(typed) > boundArgsFieldCap {
			return fmt.Sprintf("…[%d bytes]", len(typed))
		}
		return typed
	case map[string]any:
		for key, member := range typed {
			typed[key] = boundedArgValue(member)
		}
		return typed
	case []any:
		for i, member := range typed {
			typed[i] = boundedArgValue(member)
		}
		return typed
	default:
		return value
	}
}
