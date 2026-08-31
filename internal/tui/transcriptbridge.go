package tui

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The transcript bridge (the TUI's half of the neutral codec)
// ----------------------------------------------------------------------------
//
// The versioned wire form of the scrollback — the Record.Transcript blob a session record carries
// beside the engine envelope — lives in [session] (transcript.go there), because a run's scrollback
// is a fact about the conversation rather than about the surface that painted it and any Driver
// must be able to write and replay it (ADR 0031). What lives HERE is the only part of it that is
// the TUI's: the projection between this package's presentation model ([entry], [toolView],
// [presentedView], [statValue]) and those neutral types.
//
// The package's own vocabulary is unchanged: [encodeTranscript] and [decodeTranscript] still take
// and return the TUI's entries, and the codec they now call through is session's. Two rules that
// look like codec rules are consumer rules and stay on this side of the seam:
//
//   - an entry whose Kind this build does not know is SKIPPED (fromWireEntry) — session hands it
//     back as stored, because only a consumer knows what to do with a kind it cannot paint;
//   - a [skillSpan] is re-checked against the text it arrives with (spansWithin) — session hands
//     the offsets back verbatim, because only the consumer holds the text they must land in.
//
// Escape-stripping is two-layer by design. [session.DecodeTranscript] strips every field a Driver
// can paint on the way out of the file, and the card's own [toolView.sanitize] still runs on the
// way in here: stripping is idempotent, and the card-wide pass is the presenter's standing defence
// over every field a card carries rather than a restatement of the codec's list.

// encodeTranscript serializes a transcript's committed entries into the versioned wire blob
// ([session.EncodeTranscript] stamps the version). Only committed entries are written, and only the
// persisted ones: the one-time start-up box (entryStartup) is skipped — it is re-seeded fresh on
// resume — display-only entries (entry.ephemeral, e.g. the "resumed: <title>" notice) are skipped
// for the same reason one kind over, and the in-progress pending buffer is never touched, because
// tokens that were never committed to an entry were never part of the scrollback.
//
// Skipping is deliberately encode-side only: nothing ephemeral ever reaches the wire, so decode
// needs no counterpart and the wire format is unchanged (an old file keeps decoding identically).
func encodeTranscript(t *transcript) ([]byte, error) {
	entries := make([]session.Entry, 0, len(t.entries))
	for i := range t.entries {
		e := &t.entries[i]
		if e.ephemeral {
			continue // display-only: re-derived at startup/resume, so persisting it only accumulates
		}
		name := e.kind.persistedName()
		if name == "" {
			continue // entryStartup (or any future non-persisted kind): opening chrome, not conversation
		}
		entries = append(entries, toWireEntry(e, name))
	}
	return session.EncodeTranscript(entries)
}

// decodeTranscript turns a stored scrollback blob back into committed entries for replay. Empty or
// nil data is the legacy / never-recorded case and yields (nil, nil) — the caller resumes with no
// scrollback. A version newer than this build is refused ([session.ErrTranscriptVersion]) and any
// other malformed input returns a decode error; the caller degrades both to a no-replay note. An
// entry whose kind is unknown (a future variant) is skipped here rather than failing the rest —
// the consumer rule the codec deliberately leaves to this side.
//
// One kind is not replayed exactly as it was stored: a firing block still open (`!done`) when the
// record was written comes back CLOSED, because the Firing it announced died with the TUI that
// scheduled it (ADR 0033, closeInterruptedFiring). It is a post-decode pass over the entries this
// call just built, in their own order, so the rewrite stands beside the codec rather than inside
// it — nothing about the wire form changed, only a fact that changed between the write and the
// read.
func decodeTranscript(data []byte) ([]entry, error) {
	wire, err := session.DecodeTranscript(data)
	if err != nil {
		return nil, err
	}
	if wire == nil {
		return nil, nil
	}
	entries := make([]entry, 0, len(wire))
	for i := range wire {
		if e, ok := fromWireEntry(&wire[i]); ok {
			entries = append(entries, e)
		}
	}
	for i := range entries {
		if entries[i].kind == entrySchedule && !entries[i].done {
			closeInterruptedFiring(&entries[i])
		}
	}
	return entries, nil
}

// closeInterruptedCalls closes every tool call a decoded record left OPEN, wording each one with the
// outcome that actually befell it (interruptedSummary), and reports how many it closed. It is the
// firing rule one kind over (decodeTranscript): a record can now be written mid-Turn while a
// delegation runs (the progress save, ADR 0022's 2026-08-25 addendum), so the blob's last sub_agent
// head — and every child call standing under it — is stored open, and the work behind it died with
// the engine that was running it. A resume that replayed those as stored would paint a dead child as
// running, with no later fold able to correct it: the result those calls are waiting for is never
// coming, because a resumed record re-attempts the delegating Turn from its boundary rather than
// rejoining it (ADR 0007). It also covers records the cancelled-Turn path has always written with
// open calls.
//
// It runs over the TUI's OWN entries rather than the wire's, which is why it is a pass here and not
// a call into [session.CloseInterruptedCalls] (the same rewrite, for the Drivers that read the
// neutral entries): the caller needs the COUNT of what it closed in the scrollback it is about to
// replay (one note is added when anything was closed, replayScrollback), and by this point the
// entries are cards, not payloads. An entry the firing rule already closed is skipped by the same
// clause that skips every other settled call — it comes back done, and its own account of itself
// (scheduleInterruptedSummary) stands.
//
// It mutates the entries in place, which is what a decoded slice is for: it is the caller's own
// freshly built scrollback, not yet handed to the transcript.
func closeInterruptedCalls(entries []entry) (closed int) {
	for i := range entries {
		e := &entries[i]
		if e.kind != entryToolCall || e.done {
			continue
		}
		e.done = true
		e.tool.Summary = namedSummary(detailLine{Text: interruptedSummary})
		closed++
	}
	return closed
}

// toWireEntry projects one committed entry onto its neutral form. The tool and presented views are
// attached only for the kinds that carry them, so every other kind serializes without an empty
// sub-object. A firing block (entrySchedule) is one of the kinds that carry a view: it borrows the
// toolView slot whole, so it borrows the wire's tool slot whole too rather than growing a second one
// that would have to be kept in step with it.
func toWireEntry(e *entry, kind string) session.Entry {
	w := session.Entry{
		Kind:        kind,
		Text:        e.text,
		Depth:       e.depth,
		CallID:      e.callID,
		SpawnCallID: e.spawnCallID,
		Done:        e.done,
		CtxUsed:     e.ctxUsed,
		CtxLimit:    e.ctxLimit,
		CtxModel:    e.ctxModel,

		UsageCalls:              e.usage.Calls,
		UsagePromptTokens:       e.usage.PromptTokens,
		UsageCachedPromptTokens: e.usage.CachedPromptTokens,
		UsageCompletionTokens:   e.usage.CompletionTokens,
		UsageTotalTokens:        e.usage.TotalTokens,

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
func toWireSkillSpans(spans []skillSpan) []session.SkillSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]session.SkillSpan, 0, len(spans))
	for _, sp := range spans {
		out = append(out, session.SkillSpan{Start: sp.start, End: sp.end})
	}
	return out
}

// toWireToolView projects a toolView (its unexported name included) onto the wire. The stored
// arguments travel as the card already holds them (toolView.argsWire): they were bounded once at
// build time, and this seam neither re-bounds nor re-encodes them — the codec's own Marshal emits
// exactly the bytes wireArgs produced.
func toWireToolView(tv toolView) *session.ToolView {
	w := &session.ToolView{
		Label:     tv.Label,
		Verb:      tv.Verb,
		Target:    tv.Target,
		Name:      tv.name,
		Solo:      tv.solo,
		Stat:      tv.stat.spell(),
		StatValue: toWireStatValue(tv.stat),
		Task:      tv.task,
		Args:      tv.argsWire,
		Summary: session.BranchSummary{
			DetailLine: session.DetailLine{Kind: int(tv.Summary.Kind), Text: tv.Summary.Text},
			Quoted:     tv.Summary.quoted,
			Stat:       toWireStatValue(tv.Summary.stat),
		},
	}
	if tv.Details.len() > 0 {
		w.Details = make([]session.DetailLine, 0, tv.Details.len())
		for _, d := range tv.Details.all() {
			w.Details = append(w.Details, session.DetailLine{Kind: int(d.Kind), Gutter: d.Gutter, Text: d.Text})
		}
	}
	// The regions ride BESIDE the rows they were rendered into, not instead of them: Details keeps
	// the stacked reading an older build replays as it stands, and these carry the change a split
	// reading has to be re-composed from at paint time. The file names travel with them because they
	// only mean anything beside them — one name per region, in the regions' own order.
	if len(tv.Regions) > 0 {
		w.Regions = make([]session.EditRegion, 0, len(tv.Regions))
		for _, r := range tv.Regions {
			w.Regions = append(w.Regions, session.EditRegion{
				BeforeStart: r.BeforeStart,
				AfterStart:  r.AfterStart,
				Leading:     r.Leading,
				Removed:     r.Removed,
				Inserted:    r.Inserted,
				Trailing:    r.Trailing,
			})
		}
		w.RegionFiles = tv.RegionFiles
	}
	return w
}

// toWirePresented projects a presentedView onto the wire, storing Method as its domain string.
//
// A SERVED entry's Location is dropped on the way out. The doc server is started lazily and closed
// on shutdown (ADR 0019 §3), so its URL is dead on every resume and the capability token inside it
// is the only thing the record would actually keep. Decode is unchanged: the field comes back
// empty and a restored served entry prints no Location line (startupbox.renderPresentedBlock).
func toWirePresented(pv presentedView) *session.Presented {
	location := pv.Location
	if pv.Method == domain.PresentServed {
		location = ""
	}
	return &session.Presented{
		Title:    pv.Title,
		Path:     pv.Path,
		Location: location,
		Method:   string(pv.Method),
		Reason:   pv.Reason,
	}
}

// toWireStatValue projects the arithmetic half of a stat value onto the wire, and answers nil for a
// phrase that has none — the omitempty case every plain slot takes.
func toWireStatValue(v statValue) *session.StatValue {
	if !v.sums() {
		return nil
	}
	if v.kind == statCounted {
		return &session.StatValue{
			Counted:     true,
			N:           v.n,
			NounForOne:  v.nounForOne,
			NounForMany: v.nounForMany,
		}
	}
	return &session.StatValue{Added: v.added, Removed: v.removed}
}

// fromWireEntry rebuilds one committed entry from its neutral form. An unrecognised kind returns
// ok=false so the caller skips it — the one filtering rule the codec leaves to its consumer.
//
// No field is escape-stripped here: [session.DecodeTranscript] has already re-run the terminal-escape
// defence over every field a frame can reach, which is the seam that owns it now that the blob is
// read by more than one Driver.
func fromWireEntry(w *session.Entry) (entry, bool) {
	kind, ok := entryKindByName[w.Kind]
	if !ok {
		return entry{}, false
	}
	e := entry{
		kind:        kind,
		text:        w.Text,
		depth:       w.Depth,
		callID:      w.CallID,
		spawnCallID: w.SpawnCallID,
		done:        w.Done,
		ctxUsed:     w.CtxUsed,
		ctxLimit:    w.CtxLimit,
		ctxModel:    w.CtxModel,
		usage: usageTotals{
			Calls:              w.UsageCalls,
			PromptTokens:       w.UsagePromptTokens,
			CachedPromptTokens: w.UsageCachedPromptTokens,
			CompletionTokens:   w.UsageCompletionTokens,
			TotalTokens:        w.UsageTotalTokens,
		},
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
	return e, true
}

// fromWireSkillSpans rebuilds the skill-token spans from the wire, verbatim. Nothing is validated
// here — that is the caller's job, which alone holds the text the offsets must land in.
func fromWireSkillSpans(ws []session.SkillSpan) []skillSpan {
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
// kept for it, which is all a body is — and sanitize runs over the card as a whole. The escape
// defence itself is the codec's ([session.DecodeTranscript] strips every paintable field, name
// included); the card-wide pass stays because it is the presenter's standing rule over every field a
// card carries, and stripping is idempotent. A body-less card stays body-less (never a non-nil empty
// line slice) so a not-yet-enriched call round-trips exactly.
//
// The summary's MARK (branchSummary — whose words the line is) rides the wire beside the text
// ([session.BranchSummary]) and is restored through the presenter's own two constructors, so a record
// keeps the verdict the presenter reached rather than one guessed here from a line that could read
// either way. Nothing on this path acts on it: what a record keeps is FINISHED display text, spelled
// the way it was shown, and this path runs sanitize alone and never finishDisplay, so no replayed
// line is respelled whatever the mark says (TestTranscriptCodecReplaysAPromotedSummaryAsShown). A
// resumed call still awaiting its result carries no summary at all, and enrichWithResult words the
// slot afresh with a mark of its own when the result lands.
//
// The stored arguments ([session.ToolView.Args]) come back untouched onto the card's own argsWire.
// They are not display text and no seam here paints them, which is why sanitize below leaves them
// alone exactly as it does on the way in — the surface that eventually shows them is the surface that
// must strip them. Restoring them is what keeps a re-saved session's cards from silently shedding
// the arguments the record already held.
func fromWireToolView(w *session.ToolView, done bool) toolView {
	line := detailLine{Kind: detailKind(w.Summary.Kind), Text: w.Summary.Text}
	summary := namedSummary(line)
	if w.Summary.Quoted {
		summary = quotedSummary(line)
	} else if stat := fromWireStatValue(w.Summary.Stat, ""); stat.sums() {
		// The slot's own arithmetic, so a replayed run's type row totals its members exactly as the
		// live one did (sumStats). The TEXT stays the record's — decode never re-spells a phrase it
		// was handed — and a record with no value carries none, which is a blank total rather than a
		// wrong one.
		summary.stat = stat
	}
	tv := toolView{
		Label:  w.Label,
		Verb:   w.Verb,
		Target: w.Target,
		name:   w.Name,
		solo:   w.Solo,
		// The record's copy of what the call asked, restored as it stands. It is not display text
		// and nothing reads it on this path — it comes back so the card a resumed session carries
		// is the one the record kept, arguments included, rather than one that quietly lost them on
		// the next save (toolView.argsWire).
		argsWire: w.Args,
		// A value with arithmetic in it has nothing to strip and comes back as the numbers it is.
		stat:    fromWireStatValue(w.StatValue, w.Stat),
		task:    w.Task,
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
	if tv.headsRun() || (tv.name == askUserToolName && done && len(w.Details) > 0) {
		tv.solo = true
	}
	if len(w.Details) > 0 {
		lines := make([]detailLine, 0, len(w.Details))
		for _, d := range w.Details {
			lines = append(lines, detailLine{Kind: detailKind(d.Kind), Gutter: d.Gutter, Text: d.Text})
		}
		tv.Details = newToolBody(lines)
	}
	// The regions come back as the domain type the view carries, and the names with them — a record
	// that has no regions annotates nothing, so its names (if a hand-written blob carried any) are
	// dropped rather than left to line up against nothing. A record from before they rode the wire
	// takes this branch not at all and replays the stacked rows above, which is how it was written.
	if len(w.Regions) > 0 {
		regions := make([]domain.EditRegion, 0, len(w.Regions))
		for _, r := range w.Regions {
			regions = append(regions, domain.EditRegion{
				BeforeStart: r.BeforeStart,
				AfterStart:  r.AfterStart,
				Leading:     r.Leading,
				Removed:     r.Removed,
				Inserted:    r.Inserted,
				Trailing:    r.Trailing,
			})
		}
		tv.Regions, tv.RegionFiles = regions, w.RegionFiles
	}
	tv.sanitize()
	return tv
}

// fromWirePresented rebuilds a presentedView from the wire. Method is restored verbatim as a
// domain.PresentMethod: it is a closed enum matched against constants, and an unrecognised value
// simply falls to the baseline "path shown" wording rather than reaching the terminal as text.
func fromWirePresented(w *session.Presented) presentedView {
	return presentedView{
		Title:    w.Title,
		Path:     w.Path,
		Location: w.Location,
		Method:   domain.PresentMethod(w.Method),
		Reason:   w.Reason,
	}
}

// fromWireStatValue reads the arithmetic under an outcome phrase back, falling back to the phrase as
// a plain value — which is what a record written before the value rode the wire carries, and what
// every slot with no arithmetic in it carries by rule.
//
// For the old record that fallback is an ACCEPTED one-way break. A slot written before 113b3078
// carries no statValue, so it decodes as a plain value; a plain value does not sum (statValue.sums),
// so the grouped type row of a replayed multi-call run shows an empty total where the live run
// showed one. The value is deliberately NOT re-derived from the phrase: the phrase parsing that once
// read a total back out of the members' wording is gone and does not come back, so the same file
// reads differently than it did before that commit. Only runs of two or more members are affected,
// and only session files already on disk — everything written since carries the arithmetic.
func fromWireStatValue(w *session.StatValue, text string) statValue {
	if w == nil {
		return plainStat(text)
	}
	if w.Counted {
		return statValue{
			kind:        statCounted,
			n:           w.N,
			nounForOne:  w.NounForOne,
			nounForMany: w.NounForMany,
		}
	}
	return diffedStat(w.Added, w.Removed)
}
