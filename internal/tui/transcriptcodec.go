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
//
// A member can also be RETIRED within transcriptVersion, on the mirror of the additive rule: a v1
// blob written while sent blocks still carried skill display-name chips holds a "skills" member,
// no field here claims it any more, and json.Unmarshal ignores what it cannot place — so such a
// record decodes as the plain send it now paints as, its invocations recorded by SkillSpans alone.
type wireEntry struct {
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Depth  int    `json:"depth,omitempty"`
	CallID string `json:"callID,omitempty"`
	// SpawnCallID is the run identity of a delegated entry (entry.spawnCallID): the id of the
	// sub_agent call that spawned the agent whose event it folded from. It is ADDITIVE within
	// transcriptVersion and needs no bump, on the wireEntry rule above — it takes omitempty, so a
	// top-level entry writes nothing new and an older build ignores a member it cannot place,
	// while a blob written before it existed decodes to "" for every entry: the one run a
	// serialized session ever had, which is how such a record was written and how it still reads.
	SpawnCallID string `json:"spawnCallID,omitempty"`
	Done        bool   `json:"done,omitempty"`
	// the fill a sub-agent run's head wears (entry.ctxUsed / ctxLimit): what the delegate's context
	// held when it last reported, and the window that reading filled. Both members are ADDITIVE
	// within transcriptVersion and travel together, because a fill says nothing without its limit —
	// a blob written before they existed decodes to the zero pair, which is the same
	// nothing-to-say case the summary line already hides (render.go, subAgentFill). Frozen at fold
	// time, so what a record keeps is the reading as it stood when the run reported.
	CtxUsed    int             `json:"ctxUsed,omitempty"`
	CtxLimit   int             `json:"ctxLimit,omitempty"`
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
//
// Solo travels for the same reason the name does: it is a presenter's verdict — reached when the
// call was built, or when its result landed (toolView.solo) — and the decode path never re-runs a
// presenter. A record that
// stands alone in a live session and folds into its neighbours' group on resume would be the
// scrollback changing shape across a restart, which is what the round trip exists to prevent.
//
// The member is ADDITIVE within transcriptVersion: a blob written before it decodes with Solo
// false, which for most records is the truth. Two are exceptions decode RE-DERIVES rather than
// trusts — the sub-agent head, knowable from Name alone, and the ANSWERED user question, knowable
// from Name, the record's done bit and the Details only its answer hook writes — see
// fromWireToolView.
//
// Stat travels for the reason Solo does: it is the second reading of a PROMOTED outcome
// (toolView.stat), the typed phrase the promote-guard swaps into the slot on a narrow row, and the
// decode path never re-runs the presenter that worded it. Without it a resumed session's one-line
// `cat` could no longer be demoted and would crowd its own command off the row at widths where the
// live session kept it — the scrollback changing shape across a restart. It is ADDITIVE on the same
// rule: a blob written before it decodes with no stat, which is the reading every record was written
// under, and such a promotion simply stays put.
type wireToolView struct {
	Label   string            `json:"label,omitempty"`
	Verb    string            `json:"verb,omitempty"`
	Target  string            `json:"target,omitempty"`
	Name    string            `json:"name,omitempty"`
	Solo    bool              `json:"solo,omitempty"`
	Stat    string            `json:"stat,omitempty"`
	Summary wireBranchSummary `json:"summary"`
	Details []wireDetailLine  `json:"details,omitempty"`
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

// wireBranchSummary is the serialized form of a [branchSummary]: the branch line's text together
// with the one fact that is not IN that text — whose words it is. It embeds wireDetailLine rather
// than restating its members, so the mark travels with the text on the wire exactly as it does in
// the presenter, inside the one "summary" object ({"text":"…","quoted":true}).
//
// Quoted is a presenter's verdict reached on the way IN, and decode never re-runs a presenter, so a
// verdict left off the wire could not be recovered on the way out: a line PROMOTED into the slot as
// it stands (promotedOutput — a one-line tool output, the answer typed into an ask_user question) is
// quoted content no seam may respell, while a line the block worded itself names paths that
// shortenPaths spells relative to the workspace. Today's replay path reads neither — it runs
// sanitize alone, never finishDisplay — so this changes no painted row; it carries the verdict
// because a record whose summary comes back claiming the wrong authorship is a record that lies to
// whatever seam reads it next.
//
// The member is ADDITIVE within transcriptVersion and needs no bump, on the wireEntry rule: it takes
// omitempty, so a summary in the block's own words writes nothing new and an older build ignores
// what it does not know, while a blob written before it decodes with Quoted false — the mark every
// such record was written under.
type wireBranchSummary struct {
	wireDetailLine
	Quoted bool `json:"quoted,omitempty"`
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
		Kind:        kind,
		Text:        e.text,
		Depth:       e.depth,
		CallID:      e.callID,
		SpawnCallID: e.spawnCallID,
		Done:        e.done,
		CtxUsed:     e.ctxUsed,
		CtxLimit:    e.ctxLimit,
		SkillSpans:  toWireSkillSpans(e.skillSpans),
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
		Label:  tv.Label,
		Verb:   tv.Verb,
		Target: tv.Target,
		Name:   tv.name,
		Solo:   tv.solo,
		Stat:   tv.stat,
		Summary: wireBranchSummary{
			wireDetailLine: wireDetailLine{Kind: int(tv.Summary.Kind), Text: tv.Summary.Text},
			Quoted:         tv.Summary.quoted,
		},
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
		kind:        kind,
		text:        stripEscapes(w.Text),
		depth:       w.Depth,
		callID:      w.CallID,
		spawnCallID: w.SpawnCallID,
		done:        w.Done,
		ctxUsed:     w.CtxUsed,
		ctxLimit:    w.CtxLimit,
	}
	// The offsets were measured against the text as SENT, and the text above has just been
	// re-stripped as untrusted disk input, so a span is kept only while it still locates a run of
	// what came back — a corrupt or shortened record paints plain rather than slicing out of range.
	e.skillSpans = spansWithin(e.text, fromWireSkillSpans(w.SkillSpans))
	if w.Tool != nil {
		// done travels with the view because one solo verdict is not knowable from the view alone: an
		// ask_user record becomes a card of its own only once its answer landed, which is the same fact
		// this bit keeps (fromWireToolView).
		e.tool = fromWireToolView(w.Tool, e.done)
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
// seams: the stored lines become a body through newToolBody — each line carrying the Kind the wire
// kept for it, which is all a body is — and sanitize escape-strips every rendered field. name is
// stripped here instead, because sanitize leaves it alone by design (it is
// the registry key enrichWithResult reads, never rendered) and it still has to be restored for that
// lookup. A body-less card stays body-less (never a non-nil empty line slice) so a not-yet-enriched
// call round-trips exactly.
//
// The summary's MARK (branchSummary — whose words the line is) rides the wire beside the text
// (wireBranchSummary) and is restored through the presenter's own two constructors, so a record
// keeps the verdict the presenter reached rather than one guessed here from a line that could read
// either way. Nothing on this path acts on it: what a record keeps is FINISHED display text, spelled
// the way it was shown, and this path runs sanitize alone and never finishDisplay, so no replayed
// line is respelled whatever the mark says (TestTranscriptCodecReplaysAPromotedSummaryAsShown). A
// resumed call still awaiting its result carries no summary at all, and enrichWithResult words the
// slot afresh with a mark of its own when the result lands.
func fromWireToolView(w *wireToolView, done bool) toolView {
	line := detailLine{Kind: detailKind(w.Summary.Kind), Text: w.Summary.Text}
	summary := namedSummary(line)
	if w.Summary.Quoted {
		summary = quotedSummary(line)
	}
	tv := toolView{
		Label:   w.Label,
		Verb:    w.Verb,
		Target:  w.Target,
		name:    stripEscapes(w.Name),
		solo:    w.Solo,
		stat:    w.Stat, // sanitize below strips it with the other display fields
		Summary: summary,
	}
	// Two verdicts are re-derived rather than trusted, and only in the direction that can ADD solo. A
	// blob written before Solo rode the wire carries false for both, and replaying that folds records
	// the live presenter keeps apart into one counted block: the scrollback changing shape across a
	// restart, which is the very thing the round trip exists to prevent.
	//
	// A sub-agent head is a block in its own right by rule, not by circumstance (presentToolCall, the
	// same subAgentToolName constant), so the answer is knowable from the name alone — without this,
	// two span-less heads (two delegations refused at the depth bound) replay as one "✦ Sub-Agent (2)".
	//
	// An ANSWERED user question is the other, and it is the one no name settles: the record
	// materialises with the answer (askUserAnswerRecord, reached from the RESULT hook), so a question
	// still awaiting one is an ordinary pending call and groups like one. What decode matches on is
	// the RECORD's own footprint — done beside a non-empty body. done is the wire's copy of the same
	// fact the hook waits for (a tool call is done once its result landed, transcript.go), and the
	// body is what only that hook can have put there: ask_user's registry entry sets neither argBody
	// nor body, so an ask_user record carries Details if and only if askUserAnswerRecord wrote them —
	// which is exactly when it also set Solo. An ERRORED question is what the pair keeps out, and it
	// has to be kept out: enrichWithResult returns on IsError before the outcome hook, so live it
	// stays body-less and groupable, and forcing solo here would paint two failed questions as one
	// "✦ Ask User (2)" while running and as two blocks after a reload. Both halves are read off the
	// same name the presenter uses (askUserToolName), so the two rules cannot drift apart.
	//
	// Nothing else is re-derived here; every other Solo is a result-time verdict decode cannot reach.
	if tv.name == subAgentToolName || (tv.name == askUserToolName && done && len(w.Details) > 0) {
		tv.solo = true
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
