package tui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
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
	return session.EncodeTranscript(entriesToRecords(t.entries))
}

// entriesToRecords projects committed entries onto their neutral form under encodeTranscript's
// skip rules — it IS the save path's encoder, split out so a cut prefix ([transcript.prefixThrough])
// can be encoded exactly as the whole scrollback is: a forked child's transcript blob then holds
// what the parent's would, up to the cut, less the spend the fork clears first (withoutSpend), and
// nothing a resume would have skipped.
func entriesToRecords(entries []entry) []session.Entry {
	records := make([]session.Entry, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.ephemeral {
			continue // display-only: re-derived at startup/resume, so persisting it only accumulates
		}
		name := e.kind.persistedName()
		if name == "" {
			continue // entryStartup (or any future non-persisted kind): opening chrome, not conversation
		}
		records = append(records, toWireEntry(e, name))
	}
	return records
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

// replayedDelegationSummary is a replayed sub_agent head's summary with its delegation verdict
// read back off the record ([branchSummary.verdict]) by the one parser of a delegation's words
// (delegationVerdictFromWords): the slot's text, the stat stored behind a quoted slot, and the body
// a never-started delegation's failure laid out beneath it. A record written before the human's
// acts took their neutral verdict stored the failure's `error` over such a body; that slot is
// re-worded with the words the live row now shows.
func replayedDelegationSummary(summary branchSummary, stat statValue, body []detailLine) branchSummary {
	texts := make([]string, 0, len(body))
	for _, d := range body {
		texts = append(texts, d.Text)
	}
	verdict, outcome := delegationVerdictFromWords(delegationWords{
		recorded: true,
		slot:     summary.Text,
		quoted:   summary.quoted,
		stat:     stat.spell(),
		prose:    strings.Join(texts, "\n"),
	})
	if verdict.neverStarted() && verdict.humanSettled() && !summary.quoted && summary.Text == erroredSummary {
		words := detailLine{Kind: summary.Kind, Text: delegationOutcomeVerdict(outcome)}
		summary = branchSummary{detailLine: words}
	}
	return summary.withVerdict(verdict)
}

// closeInterruptedCalls closes every tool call a decoded record left OPEN, wording each one with the
// outcome that actually befell it (interruptedSummary), and reports how many it closed. It is the
// firing rule one kind over (decodeTranscript): a record can now be written mid-Turn while a
// delegation runs (the progress save, ADR 0022's 2026-08-25 addendum), so the blob's last sub_agent
// head — and every child call standing under it — is stored open, and the work behind it died with
// the engine that was running it. A resume that replayed those as stored would paint a dead child as
// running, with no later fold able to correct it: the result those calls are waiting for is never
// coming, because a resumed record re-attempts the delegating Turn from its boundary rather than
// rejoining it (ADR 0007). It also covers records written before a cancel settled its Turn (ADR
// 0088): the cancelled Turn's calls were stored open then, where a cancel now pairs every one of
// them with a result before the record is written.
//
// It runs over the TUI's OWN entries rather than the wire's, which is why it is a pass here and not
// a codec step in internal/session (which decodes what was stored and closes nothing): the caller
// needs the COUNT of what it closed in the scrollback it is about to replay (one note is added
// when anything was closed, replayScrollback), and by this point the entries are cards, not
// payloads. An entry the firing rule already closed is skipped by the same
// clause that skips every other settled call — it comes back done, and its own account of itself
// (scheduleInterruptedSummary) stands.
//
// It mutates the entries in place, which is what a decoded slice is for: it is the caller's own
// freshly built scrollback, not yet handed to the transcript.
//
// A Workflow's item heads (entryWorkflowItem) are closed on the same terms as the calls, and a
// workflow block replayed with its structure is closed by its view (workflowView.interrupted): its
// running stages are stopped and counted, and its text's header says it stopped, so a resumed block
// paints — and a later save keeps — no stage running that nothing runs.
func closeInterruptedCalls(entries []entry) (closed int) {
	for i := range entries {
		e := &entries[i]
		switch {
		case e.kind == entryWorkflow && !e.done:
			var stopped int
			e.workflow, stopped = e.workflow.interrupted()
			if stopped > 0 {
				e.done = true
				e.text = e.workflow.retitled(e.text)
			}
			closed += stopped
		case (e.kind == entryToolCall || e.kind == entryWorkflowItem) && !e.done:
			e.done = true
			e.tool.Summary = namedSummary(detailLine{Text: interruptedSummary})
			if e.tool.headsRun() {
				e.tool.Summary = e.tool.Summary.withVerdict(delegationVerdictInterrupted)
			}
			closed++
		}
	}
	return closed
}

// toWireEntry projects one committed entry onto its neutral form. The tool and presented views are
// attached only for the kinds that carry them, so every other kind serializes without an empty
// sub-object. A firing block (entrySchedule) is one of the kinds that carry a view: it borrows the
// toolView slot whole, so it borrows the wire's tool slot whole too rather than growing a second one
// that would have to be kept in step with it, and so is a Workflow item's run head
// (entryWorkflowItem), whose card is a delegation row's; its place in the Workflow rides the wire's
// Item. A block or finish line carrying a Workflow's spend (entry.carriesWorkflowSpend) borrows the
// slot for the Workflow's name alone. A workflow block's view rides the wire's Workflow, so a
// reopened block paints its stage rows again (toWireWorkflow).
func toWireEntry(e *entry, kind string) session.Entry {
	w := session.Entry{
		Kind:        kind,
		At:          e.at,
		Text:        e.text,
		Depth:       e.depth,
		CallID:      e.callID,
		SpawnCallID: e.spawnCallID,
		RunID:       e.runID,
		SpawnRunID:  e.spawnRunID,
		Done:        e.done,
		Aborted:     e.aborted,
		CtxUsed:     e.ctxUsed,
		CtxLimit:    e.ctxLimit,
		CtxModel:    e.ctxModel,

		UsageCalls:              e.usage.Calls,
		UsagePromptTokens:       e.usage.PromptTokens,
		UsageCachedPromptTokens: e.usage.CachedPromptTokens,
		UsageCompletionTokens:   e.usage.CompletionTokens,
		UsageTotalTokens:        e.usage.TotalTokens,
		UsageCostMicros:         e.usage.CostMicros,
		UsagePricedCalls:        e.usage.PricedCalls,
		UsageUnpricedCalls:      e.usage.UnpricedCalls,

		SkillSpans: toWireSkillSpans(e.skillSpans),
		Images:     toWireImageMarks(e.images),
	}
	switch {
	case e.kind == entryToolCall || e.kind == entrySchedule || e.kind == entryWorkflowItem:
		w.Tool = toWireToolView(e.tool)
	case e.carriesWorkflowSpend():
		// A workflow block or a background workflow's finish line that carries a Workflow's spend
		// keeps the Workflow's name in the one slot a name already rides in, so a reopened session's
		// /usage row is named as the live one was (usageAgentName) without a member of its own.
		w.Tool = &session.ToolView{Target: e.tool.Target}
	}
	if e.kind == entryPresented {
		w.Presented = toWirePresented(e.presented)
	}
	if e.kind == entryWorkflowItem {
		w.Item = &session.WorkflowItem{
			Stage:   e.item.stage,
			Round:   e.item.round,
			Index:   e.item.index,
			Attempt: e.item.attempt,
		}
	}
	if e.kind == entryWorkflow {
		w.Workflow = toWireWorkflow(e.workflow)
	}
	return w
}

// workflowStageStateNames spells each stage state on the wire (session.WorkflowStage.State).
var workflowStageStateNames = map[workflowStageState]string{
	stagePending: session.WorkflowStagePending,
	stageRunning: session.WorkflowStageRunning,
	stageDone:    session.WorkflowStageDone,
	stageFailed:  session.WorkflowStageFailed,
	stageStopped: session.WorkflowStageStopped,
	stageSkipped: session.WorkflowStageSkipped,
}

// toWireWorkflow projects a workflow block's view onto the wire: its name, end, cause and resume
// hint, its stage rows (each with its kind), its finished items and its end phase's tally. The live run's own state — its id, the stage running and an `ask`
// stage's question — is not kept: the Workflow does not run in the session that reopens it. A view
// with nothing to draw from (one replayed from an older record) writes nothing, so its block stays
// the text it came back as.
func toWireWorkflow(v workflowView) *session.Workflow {
	if !v.drawsStages() {
		return nil
	}
	w := &session.Workflow{Name: v.name, End: string(v.end), Cause: v.cause, Resume: v.resume}
	for _, s := range v.stages {
		w.Stages = append(w.Stages, session.WorkflowStage{
			Name:     s.name,
			Kind:     s.kind,
			Round:    s.round,
			Rounds:   s.rounds,
			Items:    s.items,
			Finished: s.finished,
			Troubled: s.troubled,
			Entered:  s.entered,
			State:    workflowStageStateNames[s.state],
		})
	}
	for _, item := range v.items {
		w.Items = append(w.Items, session.WorkflowItemResult{
			Stage:   item.stage,
			Label:   item.label,
			Status:  item.status,
			Summary: item.summary,
		})
	}
	if v.tally != nil {
		w.Tally = &session.WorkflowTally{OK: v.tally.OK, Partial: v.tally.Partial, Blocked: v.tally.Blocked}
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

// toWireImageMarks projects a sent user block's image marks onto the wire — names and sizes only,
// nil for nil, so a send without images serializes without the member at all.
func toWireImageMarks(marks []imageMark) []session.ImageMark {
	if len(marks) == 0 {
		return nil
	}
	out := make([]session.ImageMark, 0, len(marks))
	for _, mark := range marks {
		out = append(out, session.ImageMark{Name: mark.name, Size: mark.size})
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
		Chars:     tv.chars,
		// The estimate rides beside the size it was made from, a count like it.
		EstimatedTokens: tv.estimatedTokens,
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
		at:          w.At,
		text:        w.Text,
		depth:       w.Depth,
		callID:      w.CallID,
		spawnCallID: w.SpawnCallID,
		runID:       w.RunID,
		spawnRunID:  w.SpawnRunID,
		done:        w.Done,
		aborted:     w.Aborted,
		ctxUsed:     w.CtxUsed,
		ctxLimit:    w.CtxLimit,
		ctxModel:    w.CtxModel,
		usage: domain.Usage{
			Calls:              w.UsageCalls,
			PromptTokens:       w.UsagePromptTokens,
			CachedPromptTokens: w.UsageCachedPromptTokens,
			CompletionTokens:   w.UsageCompletionTokens,
			TotalTokens:        w.UsageTotalTokens,
			CostMicros:         w.UsageCostMicros,
			PricedCalls:        w.UsagePricedCalls,
			UnpricedCalls:      w.UsageUnpricedCalls,
		},
	}
	// The offsets were measured against the text as SENT, and the text above has just been
	// re-stripped as untrusted disk input, so a span is kept only while it still locates a run of
	// what came back — a corrupt or shortened record paints plain rather than slicing out of range.
	e.skillSpans = spansWithin(e.text, fromWireSkillSpans(w.SkillSpans))
	e.images = fromWireImageMarks(w.Images)
	if w.Tool != nil {
		// done travels with the view because one solo verdict is not knowable from the view alone: an
		// ask_user record becomes a card of its own only once its answer landed, which is the same fact
		// this bit keeps (fromWireToolView).
		e.tool = fromWireToolView(w.Tool, e.done)
		// A background sub_agent's call is re-derived from the record's own arguments and spawned
		// run id, exactly as the live fold read it (backgroundSubAgentCall), so it replays as the
		// background-workflow call it was, never as a delegation's head.
		e.tool.background = backgroundSubAgentCall(e.tool.name, e.tool.argsWire, e.spawnRunID)
	}
	if w.Presented != nil {
		e.presented = fromWirePresented(w.Presented)
	}
	if kind == entryWorkflowItem {
		fromWireWorkflowItem(&e, w.Item)
	}
	if kind == entryWorkflow && w.Workflow != nil {
		e.workflow = fromWireWorkflow(w.Workflow)
	}
	return e, true
}

// fromWireWorkflow rebuilds a workflow block's view from the wire as a REPLAYED one: it draws the
// stage rows the live one drew, and carries no Workflow id, so no phase of a run in this session —
// a run of the same Workflow id included — ever folds into it (workflowAt). A stage state this build
// does not know reads as pending, an end it does not know reads as stopped (workflowEndOf), and an
// item's result line is not rebuilt: the entry's text keeps it, and only a fold re-renders the text.
func fromWireWorkflow(w *session.Workflow) workflowView {
	v := workflowView{replayed: true, name: w.Name, end: workflowEndOf(w.End), cause: w.Cause, resume: w.Resume}
	for _, s := range w.Stages {
		v.stages = append(v.stages, workflowStage{
			name:     s.Name,
			kind:     s.Kind,
			round:    s.Round,
			rounds:   s.Rounds,
			items:    s.Items,
			finished: s.Finished,
			troubled: s.Troubled,
			entered:  s.Entered,
			state:    workflowStageStateOf(s.State),
		})
	}
	for _, item := range w.Items {
		v.items = append(v.items, workflowItem{
			stage:   item.Stage,
			label:   item.Label,
			status:  item.Status,
			summary: item.Summary,
		})
	}
	if w.Tally != nil {
		v.tally = &domain.WorkflowTally{OK: w.Tally.OK, Partial: w.Tally.Partial, Blocked: w.Tally.Blocked}
	}
	return v
}

// workflowEndOf reads a Workflow's end off the wire as one of the three ends the header paints: ""
// for a Workflow still running when the record was written, and stopped for an end this build does
// not know — a Workflow the record says ended, so it is not reopened as running — so no recorded
// string reaches the header as text (session stripWorkflow leaves End alone as a closed enum).
func workflowEndOf(end string) domain.WorkflowPhase {
	switch phase := domain.WorkflowPhase(end); phase {
	case "", domain.WorkflowFinished, domain.WorkflowStopped, domain.WorkflowFailed:
		return phase
	default:
		return domain.WorkflowStopped
	}
}

// workflowStageStateOf reads a stage state off the wire, pending for a name this build does not know.
func workflowStageStateOf(name string) workflowStageState {
	for state, spelled := range workflowStageStateNames {
		if spelled == name {
			return state
		}
	}
	return stagePending
}

// fromWireWorkflowItem restores what a Workflow item's run head keeps beside its card: its place in
// the Workflow, and the receipt's verdict, re-derived from the status the record keeps as the stat
// (workflowItemSummary) — the quoted summary the wire carries holds no verdict of its own.
func fromWireWorkflowItem(e *entry, w *session.WorkflowItem) {
	if w != nil {
		e.item = workflowItemPlace{stage: w.Stage, round: w.Round, index: w.Index, attempt: w.Attempt}
	}
	if status := e.tool.stat.spell(); status != "" {
		e.tool.Summary = workflowItemSummary(status, e.tool.Summary.Text)
	}
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

// fromWireImageMarks rebuilds a sent user block's image marks from the wire — nil for none, which is
// how a record written before the member existed reads: a text-only send.
func fromWireImageMarks(ws []session.ImageMark) []imageMark {
	if len(ws) == 0 {
		return nil
	}
	out := make([]imageMark, 0, len(ws))
	for _, w := range ws {
		out = append(out, imageMark{name: w.Name, size: w.Size})
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
		// The result's size comes back as the number it is: a count, never display text.
		chars: w.Chars,
		// And the estimate beside it, as the count it is; an older record reads zero.
		estimatedTokens: w.EstimatedTokens,
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
	// The first is the REGISTRY's word about the tool (toolPresenter.solo), knowable from the
	// retained name alone, so it is read off the same row the live producer copies it from
	// (presentToolCall); a name with no row — a dynamic tool's — reads the zero row, which is
	// solo=false, the same answer the live path gives it. A sub-agent head is a block in its own
	// right by rule, not by circumstance — without this, two span-less heads (two delegations
	// refused at the depth bound) replay as one "✦ Sub-agents (2)".
	//
	// An ANSWERED user question is the other, and it is the one no row settles: the record
	// materialises with the answer (askUserAnswerRecord, reached from the RESULT hook), so a question
	// still awaiting one is an ordinary pending call and groups like one. What decode matches on is
	// the RECORD's own footprint — done, a non-empty body, and no failure verdict on the slot. done
	// is the wire's copy of the same fact the hook waits for (a tool call is done once its result
	// landed, transcript.go), and the body is what only that hook or a FAILURE can have put there:
	// ask_user's registry entry sets neither argBody nor body, so the two writers of an ask_user
	// record's Details are askUserAnswerRecord — which is exactly when it also set Solo — and
	// absorbFailure, which lays a failed call's whole message beneath the branch (the ratified call
	// on failed tool rows, plan "2026-09-03 - 02", item 7). An ERRORED question is what the verdict
	// half keeps out, and it has to be kept out: enrichWithResult returns on IsError before the
	// outcome hook, so live such a question is groupable, and forcing solo here would paint two
	// failed questions as one "✦ Ask User (2)" while running and as two blocks after a reload. The
	// verdict is the one the slot already carries ([branchSummary.failed], settled by namedSummary on
	// the way in), so this reads the same fact the painter does rather than re-deriving it, and all
	// three halves are read off the same name the presenter uses (askUserToolName), so the rules
	// cannot drift apart.
	//
	// A SKILL FETCH is the second row that carries the mark, settled exactly as a delegation head
	// is: the presenter copies it the moment the call is recognised (presentToolCall), because a
	// fetch groups with its own kind rather than with the reads around it. Reading the same row
	// here is what lets a session recorded before the mark existed replay never-group instead of
	// folding into its neighbour's umbrella.
	//
	// Nothing else is re-derived here; every other Solo is a result-time verdict decode cannot reach.
	if toolRegistry[tv.name].solo ||
		(tv.name == askUserToolName && done && len(w.Details) > 0 && !tv.Summary.failed) {
		tv.solo = true
	}
	// The header-fold mark is not on the wire at all and is re-derived rather than trusted for the
	// same reason a delegation head's solo is: it is the REGISTRY's word about the tool
	// (toolPresenter.collapsesToHeader), knowable from the retained name alone, and a record that
	// kept its own copy would replay a shape the live paint no longer has the day the entry
	// changes. Reading the same entry field the live producer reads (presentToolCall) is what keeps
	// the two paints one.
	tv.collapsesToHeader = toolRegistry[tv.name].collapsesToHeader
	// The LABEL is the registry's word too, for a tool the registry knows: a display name and not
	// a fact about the call, so a record written before a relabelling (the Title Case labels
	// sub_agent and task_list once wore) replays under the label the live presenter gives the same
	// tool today rather than the one it gave then. A name with no row — a dynamic tool's — keeps the
	// record's own label, which is the label the live path gave it (unregisteredLabel: an MCP tool's
	// `<server> · <tool>`, the raw name otherwise); it is not re-derived, so a record written before
	// the split replays under the raw name it was written with. A background sub_agent's
	// card wears the delegation's label as well (backgroundSubAgentPresenter), so reading the
	// sub_agent row serves both.
	if p, known := toolRegistry[tv.name]; known && p.label != "" {
		tv.Label = p.label
	}
	if len(w.Details) > 0 {
		lines := make([]detailLine, 0, len(w.Details))
		for _, d := range w.Details {
			lines = append(lines, detailLine{Kind: detailKind(d.Kind), Gutter: d.Gutter, Text: d.Text})
		}
		tv.Details = newToolBody(lines)
	}
	// A refused call's denied mark is not on the wire (toolView.denied) and is re-derived from the
	// failure body the record kept, which is the refusal absorbFailure laid out beneath the branch.
	// Only a failed slot is read, so a successful call whose output opens with the same words is
	// never counted as denied.
	if tv.Summary.failed {
		tv.denied = deniedFailureBody(tv.Details.all())
	}
	// A delegation head's verdict is not on the wire ([branchSummary.verdict]) and is read back off
	// the record's words by the one parser of them (delegationVerdictFromWords, fed the slot's text,
	// or the stat behind a quoted slot) — the same rendered words the live row showed
	// (ADR 0083 §1). A delegation the human's act settled before it started is re-derived from its
	// stored body as well rather than trusted: a session recorded before those rows took their
	// neutral verdict stored the failure's `error`, and replaying it as such would paint red what the
	// live row now paints in the marker tone, so such a slot is re-worded too. The body is the very
	// failure body absorbFailure laid the result out as, so it reads back whole.
	if tv.headsRun() {
		tv.Summary = replayedDelegationSummary(tv.Summary, tv.stat, tv.Details.all())
	}
	// The header's count is the third thing re-derived rather than trusted, and for the same reason:
	// it is not on the wire at all (toolView.count), and the rows it is read off ARE — task_list's
	// body is exactly its rows (taskListDetail) — so the decoded Details word it again through the
	// very hook the live producer used (enrichWithResult, toolPresenter.count).
	//
	// A record written before the count existed carries the stat that used to close the list
	// ("2 open") as its retained Summary, and replaying it beside the header count would show one
	// card counting itself twice. So where the entry counts, the record's own WORDING of the slot is
	// discarded — the header supersedes it — while a verdict or a promoted line is kept, since those
	// are what the live card keeps too: a failed call's `error` (absorbFailure), and a refusal's
	// sentence the tool printed on one line (outputDetail), which the blank stat never takes back.
	if count := toolRegistry[tv.name].count; count != nil {
		if !tv.Summary.quoted && !tv.Summary.failed {
			tv.Summary = branchSummary{}
		}
		texts := make([]string, 0, len(w.Details))
		for _, d := range w.Details {
			texts = append(texts, d.Text)
		}
		if c, ok := count(texts); ok {
			tv.count = c
		}
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

// deniedFailureBody reads a replayed failed call's body for the engine's refusal (deniedContent).
// The failure body is the result's own lines in the plain kind; an edit or a write lays its -/+
// lines from the call's arguments ABOVE it (argBody), so the first plain line is where the result
// begins.
func deniedFailureBody(lines []detailLine) bool {
	for _, line := range lines {
		if line.Kind == detailPlain {
			return deniedContent(line.Text)
		}
	}
	return false
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

// ----------------------------------------------------------------------------
// Delegation words: the one parser
// ----------------------------------------------------------------------------
//
// A delegation's verdict travels typed wherever the engine decides it (domain.DelegationOutcome,
// [branchSummary.verdict]); words are read back in exactly two places, and both come through
// delegationVerdictFromWords. REPLAY reads the words a record kept — the slot's rendered verdict and
// the body beneath it — because the record keeps rendered words and no typed verdict (ADR 0083 §1).
// A LIVE result carrying no outcome — one a reaction re-summarised, an older producer's line — is
// read off the engine's own text (delegationOutcomeOf). Every matcher and every restated engine
// sentence below is this parser's private part: no other seam reads a delegation's words.

// delegationWords is what a delegation left in WORDS, as delegationVerdictFromWords reads them: the
// engine's own text, and — for a record — the words its slot was rendered with.
type delegationWords struct {
	// recorded says the words are a session record's (replayedDelegationSummary) rather than a live
	// result's; a record's verdict is read off its slot, a live result's off its text.
	recorded bool
	// slot is a record's slot text: the verdict's words, or the child's report where quoted is set.
	slot string
	// quoted says the record's slot quotes the child's one-line report, so the verdict's words are
	// the stat stored behind it (applyStat) rather than the slot's own.
	quoted bool
	// stat is the stat a record stored behind its slot.
	stat string
	// prose is the engine's text: a live result's content, or the body a record kept beneath its
	// slot — the failure body a never-started delegation's result laid out as (absorbFailure).
	prose string
	// isError is a live result's error status; a record keeps none, and reads its slot instead.
	isError bool
}

// delegationVerdictFromWords is the ONE reader of a delegation's words: it maps them to the verdict
// the row paints and to the outcome they state.
//
// A record's verdict is read off the words its slot was rendered with (delegationOutcomeVerdict,
// absorbFailure, closeInterruptedCalls) — the slot's own text, or the stat behind a quoted report —
// unless its body opens under the unstarted head (unstartedDelegationPrefix): such a delegation never
// ran, and why is read off that body, which is also how an older record's `error` slot over a
// human's act is told from a failure. A live result's outcome is read off the engine's envelope
// (proseDelegationOutcome, or proseFailedDelegationOutcome for an error-shaped one) and its verdict
// follows from it as a typed outcome's would (delegationVerdictOf).
//
// The reading is TOTAL: words that name no verdict read as the zero verdict, which withholds
// nothing, and the outcome comes back zero wherever the words state none — a record's slot carries a
// verdict, never the facts beneath it.
func delegationVerdictFromWords(words delegationWords) (delegationVerdict, domain.DelegationOutcome) {
	if !words.recorded {
		outcome := proseDelegationOutcome(words.prose)
		if words.isError {
			outcome = proseFailedDelegationOutcome(words.prose)
		}
		return delegationVerdictOf(outcome, words.isError), outcome
	}
	if strings.HasPrefix(words.prose, unstartedDelegationPrefix) {
		outcome := proseFailedDelegationOutcome(words.prose)
		return delegationVerdictOf(outcome, true), outcome
	}
	if words.quoted {
		return delegationVerdictOfSlot(words.stat), domain.DelegationOutcome{}
	}
	return delegationVerdictOfSlot(words.slot), domain.DelegationOutcome{}
}

// delegationVerdictOfSlot reads a started delegation's verdict back off the words its slot was
// rendered with. Each verdict is matched on the WHOLE phrase, alone or with the steering cell
// (isDelegationVerdictWord), so a report line that merely mentions the words is not the verdict; a
// bound's words share one lead whatever the bound. Words that name no verdict read as the zero
// verdict.
func delegationVerdictOfSlot(word string) delegationVerdict {
	switch {
	case isDelegationVerdictWord(word, delegationDoneVerdict):
		return delegationVerdictDone
	case isDelegationVerdictWord(word, delegationNoReportVerdict):
		return delegationVerdictNoReport
	case word == delegationPreemptedVerdict:
		return delegationVerdictPreempted
	case isDelegationVerdictWord(word, delegationStoppedVerdict):
		return delegationVerdictStopped
	case strings.HasPrefix(word, delegationBoundLead):
		return delegationVerdictCapped
	case word == interruptedSummary:
		return delegationVerdictInterrupted
	case failedSummary(word):
		return delegationVerdictFailed
	}
	return delegationVerdictNone
}

// isDelegationVerdictWord reports whether word is the verdict, alone or with the steering cell a
// human's messages append to whichever verdict stands (delegationSteeredCell, ADR 0063 D3).
func isDelegationVerdictWord(word, verdict string) bool {
	return word == verdict || strings.HasPrefix(word, verdict+slotSeparator+delegationSteeredLead)
}

// unstartedDelegationPrefix opens the result of a delegation the engine settled BEFORE it started —
// pre-empted by a queued user message, refused past the reply's fan-out ceiling, or stopped by the
// human while it queued (internal/agent/dispatch.go's skippedDelegationContent,
// fanOutCeilingResultFormat and stoppedQueuedDelegationContent; ADR 0039, ADR 0086 D4). It is
// restated here because this package cannot read the engine's constants: the unstarted kinds share
// the head so a reader can tell them from a child's own failure, and a rewording over
// there has to fail the test that pins it here.
const unstartedDelegationPrefix = "sub-agent not started:"

// delegationBoundHead matches the marker line a delegation stopped at one of its BOUNDS opens with
// — "[delegate stopped at its step cap (3 steps); partial result — engine summary and closing report follow]",
// or the same shape naming its token budget or its time limit (internal/agent's
// stepCapResultFormat, tokenCapResultFormat and timeCapResultFormat) — anchored at the START, where
// the engine writes it, so a line the child itself printed cannot be read as the marker. The one
// submatch is the bound's name, which the verdict repeats.
var delegationBoundHead = regexp.MustCompile(
	`^\[delegate stopped at its (step cap \(\d+ steps?\)|token budget \(\d+ tokens?\)|time limit \([^)]+\));`)

// delegationBoundNamed is the bound a matched head's submatch names — "step cap (3 steps)" is the
// step cap — read through delegationBoundNames. A name no bound is spelled with is
// DelegationUnbounded, which the head's own pattern never lets through.
func delegationBoundNamed(match string) domain.DelegationBound {
	name, _, _ := strings.Cut(match, " (")
	for bound, spelled := range delegationBoundNames {
		if spelled == name {
			return bound
		}
	}
	return domain.DelegationUnbounded
}

// delegationSteeredTail matches the PARENT NOTICE a steered delegation's result closes with — "(the
// user sent 2 messages to this sub-agent while it ran)" (internal/agent's userSteeredTrailer, ADR
// 0063 D3) — anchored at the END, which is both where the engine appends it and the half the
// structural clamp keeps, so a result clipped on its way to the parent still reads here.
var delegationSteeredTail = regexp.MustCompile(`\n\(the user sent (\d+) messages? to this sub-agent while it ran\)$`)

// readDelegationSteering splits the parent notice off a delegation's result: the content without it,
// and how many messages it reports. A result carrying no notice comes back whole with a zero, which
// is the ordinary unsteered case and the only answer a foreign shape can give.
func readDelegationSteering(content string) (body string, steered int) {
	trimmed := strings.TrimRight(content, "\n")
	m := delegationSteeredTail.FindStringSubmatchIndex(trimmed)
	if m == nil {
		return content, 0
	}
	n, err := strconv.Atoi(trimmed[m[2]:m[3]])
	if err != nil || n <= 0 {
		return content, 0
	}
	return strings.TrimRight(trimmed[:m[0]], "\n"), n
}

// delegationStoppedHead is the marker line a delegation the HUMAN stopped opens with (internal/agent's
// stoppedResultHead, ADR 0086 D4): a non-error partial result, the engine fold and the child's last
// words beneath it. delegationStoppedQueuedContent is the whole of the error-shaped result a pooled
// delegation stopped before it started carries (internal/agent's stoppedQueuedDelegationContent),
// under the unstarted head (unstartedDelegationPrefix) every never-ran kind shares. Both are spelled
// again here as the bound head is: the engine formats them deliberately. They are read by this
// parser alone — off a live result carrying no typed outcome (proseDelegationOutcome,
// proseFailedDelegationOutcome), and off the body a record kept beneath a never-started delegation's
// slot — since the engine attaches the outcome, the unstarted one included, to every result it
// renders for a delegation.
//
// Two more never-ran kinds are the human's act rather than the child's failure, and are spelled
// again here for the same reason: delegationCancelledQueuedContent is the whole result of a
// delegation still queued when the human cancelled the turn (internal/agent's
// cancelledQueuedDelegationContent) — a stop of the whole turn, worded `stopped by you` like the
// queued stop — and delegationPreemptedContent is the whole result of one a queued user message
// pre-empted (internal/agent's skippedDelegationContent, ADR 0025), worded
// delegationPreemptedVerdict. Both stay error-shaped on the wire for the model; only the row's
// verdict is neutral (proseFailedDelegationOutcome, absorbFailure).
const (
	delegationStoppedHead          = "[stopped by the user — engine summary follows]"
	delegationStoppedQueuedContent = "sub-agent not started: the user stopped it before it started; " +
		"delegate again if the task is still needed"
	delegationCancelledQueuedContent = "sub-agent not started: the user cancelled the turn before it started; " +
		"delegate again if the task is still needed"
	delegationPreemptedContent = "sub-agent not started: the user sent a message while this group was running; " +
		"delegate again if the task is still needed"
)

// delegationStoppedByUser reads the human's stop off a delegation's result: the stopped head as the
// body's first line — anchored at the START, where the engine writes it, so a line the child printed
// cannot be read as the marker — or the whole text of a queued stop or of a turn cancel that caught
// the delegation queued. body is the result with the steering notice already taken off
// (readDelegationSteering).
func delegationStoppedByUser(body string) bool {
	head, _, _ := strings.Cut(body, "\n")
	whole := strings.TrimSpace(body)
	return head == delegationStoppedHead || whole == delegationStoppedQueuedContent ||
		whole == delegationCancelledQueuedContent
}

// proseFailedDelegationOutcome reads an error-shaped delegation result the engine attached no
// outcome to: the human's act that settled it before its child ran — a queued stop
// (delegationStoppedQueuedContent), a turn cancel that caught it queued
// (delegationCancelledQueuedContent), a pre-emption by a queued message (delegationPreemptedContent)
// — the human's stop of a started run (delegationStoppedHead), or, for any other result under the
// unstarted head (unstartedDelegationPrefix), the fan-out ceiling's refusal. Everything else is a
// child's own failure, the zero outcome with the steering its notice reports, which the failure hook
// carries onto the red line (delegationFailure). The human's acts are matched on the WHOLE content,
// so a failure that merely quotes the words is still a failure.
func proseFailedDelegationOutcome(content string) domain.DelegationOutcome {
	body, steered := readDelegationSteering(content)
	whole := strings.TrimSpace(body)
	head, _, _ := strings.Cut(body, "\n")
	switch {
	case whole == delegationStoppedQueuedContent:
		return domain.DelegationOutcome{Unstarted: domain.DelegationStoppedQueued}
	case whole == delegationCancelledQueuedContent:
		return domain.DelegationOutcome{Unstarted: domain.DelegationCancelled}
	case whole == delegationPreemptedContent:
		return domain.DelegationOutcome{Unstarted: domain.DelegationSkipped}
	case head == delegationStoppedHead:
		return domain.DelegationOutcome{IsStoppedByUser: true}
	case strings.HasPrefix(whole, unstartedDelegationPrefix):
		return domain.DelegationOutcome{Unstarted: domain.DelegationRefused}
	}
	return domain.DelegationOutcome{SteeredMessages: steered}
}

// delegationNoReportMarker is the whole of the result a child gets back to its parent when its
// closing text was a bare acknowledgement — "Done.", "OK" (internal/agent's noReportMarker, spelled
// again here as the bound head is: the engine formats it deliberately, and this package reads it
// off a result carrying no typed outcome, proseDelegationOutcome).
const delegationNoReportMarker = "[delegate returned no report]"

// delegationEndedWithoutReport reads the no-report half of the result envelope: a delegation that
// reached its own boundary — no bound head (delegationBoundHead), which is the engine's word that
// the run was stopped short and outranks this one — and handed back either the no-report marker or
// a closing text the shared classifier judges not a report (floor.IsNonReport: narration of a next
// step, a pasted file, a grep dump). body is the result with the steering notice already taken off
// (readDelegationSteering), so a steered child's narration is read as the narration it is; the
// engine's body notes are taken off it here (delegationChildText), for the same reason.
//
// It is the ROW's reading and nothing more (ratified call "Non-report" of plan "2026-09-24 - 01"):
// the parent model receives the result exactly as the engine wrote it, and the classifier is the one
// internal/agent's capped path judges the same text with, so the row and the capped head cannot
// come to disagree about what a report is.
func delegationEndedWithoutReport(body string) bool {
	if delegationBoundHead.MatchString(body) || delegationStoppedByUser(body) {
		return false
	}
	text := delegationChildText(body)
	return strings.TrimSpace(text) == delegationNoReportMarker || floor.IsNonReport(text)
}

// delegationBodyNote matches the body notes the engine appends BENEATH a delegation's text on any
// outcome — the routing note a result gains when its call asked for the Sub-agent server and ran on
// the session server instead (internal/agent's SeatFallbackNote, ADR 0069), and the clamp note a
// `max_steps` above the configured cap earns (its stepCapClampNoteFormat). Each is a whole line the
// engine formats deliberately, anchored at both ends so a line the child wrote that merely opens the
// same way is not one.
var delegationBodyNote = regexp.MustCompile(
	`^(?:note: ran on the session server — the sub-agents server was unavailable|` +
		`\[max_steps \d+ requested; the configured cap is \d+ — \d+ applied\])$`)

// delegationChildText is a delegation's body with the engine's trailing body notes
// (delegationBodyNote) taken off: the text the CHILD closed on, which is what the no-report reading
// judges. Without it a narrating child whose call was clamped or fell back would close on the
// engine's note rather than on its own last line, and read as a report — a fallen-back result must
// classify exactly as the plain one does.
func delegationChildText(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for len(lines) > 0 && delegationBodyNote.MatchString(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// proseDelegationOutcome reads a delegation's ending off the RESULT ENVELOPE the engine wraps a
// child's answer in — the head it opens with, the no-report reading of what it carries, and the
// parent notice it closes with — for a result that carries no typed outcome. Each is anchored on a
// line the engine formats deliberately, and the reading is TOTAL: a shape it does not recognise is
// the plain reported outcome. The human's stop (delegationStoppedByUser) outranks the bound head,
// which outranks the no-report reading, the order the engine decides them in.
func proseDelegationOutcome(content string) domain.DelegationOutcome {
	body, steered := readDelegationSteering(content)
	outcome := domain.DelegationOutcome{SteeredMessages: steered}
	bound := delegationBoundHead.FindStringSubmatch(body)
	switch {
	case delegationStoppedByUser(body):
		outcome.IsStoppedByUser = true
	case bound != nil:
		outcome.Bound = delegationBoundNamed(bound[1])
	case delegationEndedWithoutReport(body):
		outcome.HasNoReport = true
	}
	return outcome
}
