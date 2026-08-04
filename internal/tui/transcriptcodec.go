package tui

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The transcript codec (session-system plan §3 — the Record.Transcript blob)
// ----------------------------------------------------------------------------
//
// This file is the TUI-owned, versioned wire form of the scrollback: the opaque
// Record.Transcript blob the session store carries alongside the engine envelope, exactly as
// Session.State is opaque to domain. The store never decodes it; only this package understands
// its shape, and only this layer versions it — session.RecordVersion versions the wrapper,
// domain.SessionVersion the engine payload, transcriptVersion the scrollback (plan's
// layer-ownership-of-versions rule).
//
// The wire structs mirror [entry] and its views with exported fields, and the entry kind is
// serialized as a STRING enum rather than the [entryKind] iota, so a future reordering of those
// constants can never re-interpret an old file. Decode is defensive: a session file is untrusted
// disk input, so every text field is re-run through stripEscapes on the way back in (defence in
// depth — a tampered file cannot smuggle a terminal escape sequence into the transcript), a
// version newer than this build is refused (ErrTranscriptVersion), and a malformed blob returns
// an error the caller degrades to a no-replay note.

// transcriptVersion is the schema version encodeTranscript stamps on every scrollback blob it
// writes. A blob whose version exceeds this build's is refused with ErrTranscriptVersion — the
// same reject-forward rule domain.SessionVersion and session.RecordVersion apply, owned by this
// layer alone.
const transcriptVersion = 1

// ErrTranscriptVersion is returned by decodeTranscript when a blob's version exceeds this
// build's transcriptVersion — scrollback written by a newer Apogee, refused rather than
// misread. The caller degrades to resuming with no scrollback replay.
var ErrTranscriptVersion = errors.New("apogee: unsupported transcript version")

// wireEnvelope is the top-level serialized form of the scrollback: a version tag plus the
// committed entries in display order. The version gates forward compatibility for the whole
// blob; individual entry kinds are additive within a version.
type wireEnvelope struct {
	Version int         `json:"version"`
	Entries []wireEntry `json:"entries"`
}

// wireEntry is the serialized form of one committed [entry]. Kind is the string enum (never the
// iota); the payload fields mirror entry's, and the two view structs hang off their own optional
// members so a non-tool / non-presented entry serializes without them. The in-progress pending
// buffer, the one-time start-up box and any display-only (ephemeral) entry never reach here (see
// encodeTranscript).
type wireEntry struct {
	Kind       string          `json:"kind"`
	Text       string          `json:"text,omitempty"`
	Depth      int             `json:"depth,omitempty"`
	CallID     string          `json:"callID,omitempty"`
	Done       bool            `json:"done,omitempty"`
	Skills     []string        `json:"skills,omitempty"`
	SkillSpans []wireSkillSpan `json:"skillSpans,omitempty"`
	Tool       *wireToolView   `json:"tool,omitempty"`
	Presented  *wirePresented  `json:"presented,omitempty"`
}

// wireSkillSpan is the serialized form of a [skillSpan]: the byte range one invoked "/token"
// occupies in the entry's own Text. Offsets travel rather than the token text, because the text is
// the record and a span only points into it — which is also why decode re-checks a span against
// the text it arrives with (fromWireEntry → spansWithin) instead of trusting the file.
//
// The member is ADDITIVE within transcriptVersion and needs no bump: a blob written before it
// decodes with no spans and its blocks paint plain, and an older build ignores a member it does
// not know. Both are the pre-production degrade the plan accepted — no migration.
// Neither offset takes omitempty: start 0 is the ordinary case (a message opening with its token),
// and a half-written span would read as a live one. The slice member carries the omission instead.
type wireSkillSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// wireToolView is the serialized form of a [toolView], including its unexported name — the raw
// tool id enrichWithResult keys the result extractor on, so a replayed tool-call entry keeps
// working exactly as a freshly-folded one.
type wireToolView struct {
	Label   string           `json:"label,omitempty"`
	Verb    string           `json:"verb,omitempty"`
	Target  string           `json:"target,omitempty"`
	Name    string           `json:"name,omitempty"`
	Summary wireDetailLine   `json:"summary"`
	Details []wireDetailLine `json:"details,omitempty"`
}

// wireDetailLine is the serialized form of a [detailLine]. Kind is stored as its underlying
// integer value, so the [detailKind] constants (detailPlain=0, detailDiffAdded=1,
// detailDiffRemoved=2) are pinned by value: they must never be reordered or renumbered, or an
// old file's diff colours would shift. A string enum was not used here because detailKind is a
// closed rendering hint, not an evolving vocabulary like the entry kinds.
type wireDetailLine struct {
	Kind int    `json:"kind,omitempty"`
	Text string `json:"text,omitempty"`
}

// wirePresented is the serialized form of a [presentedView]. Method is stored as its domain
// string (domain.PresentMethod's underlying value) rather than an internal code, so the wire
// form reads honestly and survives a constant reorder.
type wirePresented struct {
	Title    string `json:"title,omitempty"`
	Path     string `json:"path,omitempty"`
	Location string `json:"location,omitempty"`
	Method   string `json:"method,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// entryKindNames maps each persisted entry kind to its stable wire string. entryStartup is
// deliberately absent: the start-up box is opening chrome re-seeded fresh on resume, never
// serialized (encodeTranscript skips any kind with no name here), and a "startup" string on
// decode is unknown and skipped — symmetric.
//
// A new name here is ADDITIVE within transcriptVersion and needs no bump (the wireEnvelope rule):
// an older build simply does not find the string and skips that entry, exactly as it skips a
// "future-variant" today. "schedule" — the firing block — joined v1 on those terms.
var entryKindNames = map[entryKind]string{
	entryUser:        "user",
	entryAssistant:   "assistant",
	entryToolCall:    "toolCall",
	entryToolResult:  "toolResult",
	entryError:       "error",
	entryNote:        "note",
	entryPresented:   "presented",
	entryInterjected: "interjected",
	entrySchedule:    "schedule",
}

// entryKindByName is the decode-side inverse of entryKindNames, built once at init so decode is
// a map lookup. An unrecognised kind string (a future variant, or the excluded "startup") is not
// found and its entry is skipped rather than failing the whole replay.
var entryKindByName = func() map[string]entryKind {
	m := make(map[string]entryKind, len(entryKindNames))
	for k, name := range entryKindNames {
		m[name] = k
	}
	return m
}()

// encodeTranscript serializes a transcript's committed entries into the versioned wire blob.
// Only committed entries are written, and only the persisted ones: the one-time start-up box
// (entryStartup) is skipped — it is re-seeded fresh on resume — display-only entries
// (entry.ephemeral, e.g. the "resumed: <title>" notice) are skipped for the same reason one kind
// over, and the in-progress pending buffer is never touched, because tokens that were never
// committed to an entry were never part of the scrollback. The result is stamped with
// transcriptVersion.
//
// Skipping is deliberately encode-side only: nothing ephemeral ever reaches the wire, so decode
// needs no counterpart and the wire format is unchanged (an old file keeps decoding identically).
func encodeTranscript(t *transcript) ([]byte, error) {
	env := wireEnvelope{Version: transcriptVersion, Entries: make([]wireEntry, 0, len(t.entries))}
	for i := range t.entries {
		e := &t.entries[i]
		if e.ephemeral {
			continue // display-only: re-derived at startup/resume, so persisting it only accumulates
		}
		name, ok := entryKindNames[e.kind]
		if !ok {
			continue // entryStartup (or any future non-persisted kind): opening chrome, not conversation
		}
		env.Entries = append(env.Entries, toWireEntry(e, name))
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("apogee: encode transcript: %w", err)
	}
	return data, nil
}

// decodeTranscript turns a stored scrollback blob back into committed entries for replay. Empty
// or nil data is the legacy / never-recorded case and yields (nil, nil) — the caller resumes with
// no scrollback. A version newer than this build is refused (ErrTranscriptVersion) and any other
// malformed input returns a decode error; the caller degrades both to a no-replay note. An entry
// whose kind is unknown (a future variant) is skipped rather than failing the rest. Every text
// field passes back through stripEscapes — a session file is untrusted disk input, so the same
// terminal-escape defence the fold applies on the way in is re-applied on the way out.
func decodeTranscript(data []byte) ([]entry, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var env wireEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("apogee: decode transcript: %w", err)
	}
	if env.Version > transcriptVersion {
		return nil, ErrTranscriptVersion
	}
	entries := make([]entry, 0, len(env.Entries))
	for i := range env.Entries {
		if e, ok := fromWireEntry(&env.Entries[i]); ok {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

// toWireEntry projects one committed entry onto its wire form. The tool and presented views are
// attached only for the kinds that carry them, so every other kind serializes without an empty
// sub-object. A firing block (entrySchedule) is one of the kinds that carry a view: it borrows the
// toolView slot whole, so it borrows its wire form whole too rather than growing a second one that
// would have to be kept in step with it.
func toWireEntry(e *entry, kind string) wireEntry {
	w := wireEntry{
		Kind:       kind,
		Text:       e.text,
		Depth:      e.depth,
		CallID:     e.callID,
		Done:       e.done,
		Skills:     e.skills,
		SkillSpans: toWireSkillSpans(e.skillSpans),
	}
	if e.kind == entryToolCall || e.kind == entrySchedule {
		w.Tool = toWireToolView(e.tool)
	}
	if e.kind == entryPresented {
		w.Presented = toWirePresented(e.presented)
	}
	return w
}

// toWireSkillSpans projects an entry's skill-token spans onto the wire — nil for nil, so a message
// that invoked no skill serializes without the member at all.
func toWireSkillSpans(spans []skillSpan) []wireSkillSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]wireSkillSpan, 0, len(spans))
	for _, sp := range spans {
		out = append(out, wireSkillSpan{Start: sp.start, End: sp.end})
	}
	return out
}

// toWireToolView projects a toolView (its unexported name included) onto the wire.
func toWireToolView(tv toolView) *wireToolView {
	w := &wireToolView{
		Label:   tv.Label,
		Verb:    tv.Verb,
		Target:  tv.Target,
		Name:    tv.name,
		Summary: wireDetailLine{Kind: int(tv.Summary.Kind), Text: tv.Summary.Text},
	}
	if tv.Details.len() > 0 {
		w.Details = make([]wireDetailLine, 0, tv.Details.len())
		for _, d := range tv.Details.all() {
			w.Details = append(w.Details, wireDetailLine{Kind: int(d.Kind), Text: d.Text})
		}
	}
	return w
}

// toWirePresented projects a presentedView onto the wire, storing Method as its domain string.
func toWirePresented(pv presentedView) *wirePresented {
	return &wirePresented{
		Title:    pv.Title,
		Path:     pv.Path,
		Location: pv.Location,
		Method:   string(pv.Method),
		Reason:   pv.Reason,
	}
}

// fromWireEntry rebuilds one committed entry from its wire form, escape-stripping every text
// field on the way in. An unrecognised kind returns ok=false so the caller skips it.
//
// One kind is not replayed exactly as it was stored: a firing block still open (`!done`) when the
// record was written comes back CLOSED, because the Firing it announced died with the TUI that
// scheduled it (ADR 0033, closeInterruptedFiring). Nothing else is rewritten here — the rule is
// about a fact that changed between the write and the read, not about the wire form.
func fromWireEntry(w *wireEntry) (entry, bool) {
	kind, ok := entryKindByName[w.Kind]
	if !ok {
		return entry{}, false
	}
	e := entry{
		kind:   kind,
		text:   stripEscapes(w.Text),
		depth:  w.Depth,
		callID: w.CallID,
		done:   w.Done,
		skills: stripEscapesAll(w.Skills),
	}
	// The offsets were measured against the text as SENT, and the text above has just been
	// re-stripped as untrusted disk input, so a span is kept only while it still locates a run of
	// what came back — a corrupt or shortened record paints plain rather than slicing out of range.
	e.skillSpans = spansWithin(e.text, fromWireSkillSpans(w.SkillSpans))
	if w.Tool != nil {
		e.tool = fromWireToolView(w.Tool)
	}
	if w.Presented != nil {
		e.presented = fromWirePresented(w.Presented)
	}
	if e.kind == entrySchedule && !e.done {
		closeInterruptedFiring(&e)
	}
	return e, true
}

// fromWireSkillSpans rebuilds the skill-token spans from the wire, verbatim. Nothing is validated
// here — that is the caller's job, which alone holds the text the offsets must land in.
func fromWireSkillSpans(ws []wireSkillSpan) []skillSpan {
	if len(ws) == 0 {
		return nil
	}
	out := make([]skillSpan, 0, len(ws))
	for _, w := range ws {
		out = append(out, skillSpan{start: w.Start, end: w.End})
	}
	return out
}

// fromWireToolView rebuilds a toolView from the wire and finishes it through the presenter's own
// seams: the stored lines become a body through newToolBody, which settles the kind the wire never
// carried (each line keeps its own Kind and nothing stands above them, so a resumed diff would come
// back capped at one line if decode built the body any other way), and sanitize escape-strips every
// rendered field. name is stripped here instead, because sanitize leaves it alone by design (it is
// the registry key enrichWithResult reads, never rendered) and it still has to be restored for that
// lookup. A body-less card stays body-less (never a non-nil empty line slice) so a not-yet-enriched
// call round-trips exactly.
//
// The summary's MARK (branchSummary — whose words the line is) is deliberately not on the wire and
// comes back unset, for the same reason the body's kind is re-derived rather than stored: what a
// record keeps is FINISHED display text, spelled the way it was shown, and the mark governs an act
// that happened on the way IN. This path runs sanitize alone and never finishDisplay, so no
// replayed line is respelled and none needs a mark to protect it; a resumed call still awaiting its
// result carries no summary at all, and enrichWithResult words the slot afresh with a mark of its
// own when the result lands (TestTranscriptCodecReplaysAPromotedSummaryAsShown).
func fromWireToolView(w *wireToolView) toolView {
	tv := toolView{
		Label:   w.Label,
		Verb:    w.Verb,
		Target:  w.Target,
		name:    stripEscapes(w.Name),
		Summary: namedSummary(detailLine{Kind: detailKind(w.Summary.Kind), Text: w.Summary.Text}),
	}
	if len(w.Details) > 0 {
		lines := make([]detailLine, 0, len(w.Details))
		for _, d := range w.Details {
			lines = append(lines, detailLine{Kind: detailKind(d.Kind), Text: d.Text})
		}
		tv.Details = newToolBody(lines)
	}
	tv.sanitize()
	return tv
}

// fromWirePresented rebuilds a presentedView from the wire, escape-stripping its free-text
// fields. Method is restored verbatim as a domain.PresentMethod: it is a closed enum matched
// against constants, and an unrecognised value simply falls to the baseline "path shown" wording
// rather than reaching the terminal as text.
func fromWirePresented(w *wirePresented) presentedView {
	return presentedView{
		Title:    stripEscapes(w.Title),
		Path:     stripEscapes(w.Path),
		Location: stripEscapes(w.Location),
		Method:   domain.PresentMethod(w.Method),
		Reason:   stripEscapes(w.Reason),
	}
}
