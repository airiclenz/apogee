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
// constants can never re-interpret an old file — the strings themselves are the behaviour table's
// persistedName column (entrykind.go), and a kind with none is simply never written. Decode is defensive: a session file is untrusted
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
	CtxUsed  int `json:"ctxUsed,omitempty"`
	CtxLimit int `json:"ctxLimit,omitempty"`
	// the model a sub-agent run's head wears (entry.ctxModel): the model the delegate ran on when
	// it was not the session's own — routing to the Sub-agent server (ADR 0045) — and absent when
	// it was. ADDITIVE within transcriptVersion on the same wireEntry rule: it takes omitempty, so
	// every unrouted run writes nothing new, and a blob written before it existed decodes to "" —
	// the run that ran where the session ran, which is what a record from a build without routing
	// describes. Frozen at fold time like the pair above, so a resumed session repaints the model
	// the run ACTUALLY used rather than the one it happens to reopen on.
	CtxModel string `json:"ctxModel,omitempty"`
	// the cumulative token accounting a sub-agent run's head wears (entry.usage): what the delegate
	// spent over the whole run, as of its last report. The four members are ADDITIVE within
	// transcriptVersion on the same wireEntry rule as the pair above — each takes omitempty, so a
	// run that never reported writes none of them and a blob written before they existed decodes to
	// zero totals, the same nothing-to-report state a pre-feature session reopens in.
	UsageCalls            int             `json:"usageCalls,omitempty"`
	UsagePromptTokens     int             `json:"usagePromptTokens,omitempty"`
	UsageCompletionTokens int             `json:"usageCompletionTokens,omitempty"`
	UsageTotalTokens      int             `json:"usageTotalTokens,omitempty"`
	SkillSpans            []wireSkillSpan `json:"skillSpans,omitempty"`
	Tool                  *wireToolView   `json:"tool,omitempty"`
	Presented             *wirePresented  `json:"presented,omitempty"`
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
//
// StatValue is the ARITHMETIC under that phrase (statValue), and it travels for a third-generation
// version of the same reason: the phrase is what the slot shows, the value is what a run's type row
// ADDS UP (sumStats), and a demoted member with only its wording left would drop out of its run's
// sum — a resumed session's type row going blank where the live one totalled its calls. It is
// written only where the phrase has arithmetic in it, so a verdict or an exit code grows no bytes,
// and a blob written before it decodes to the phrase alone.
//
// Task travels because it is BODY, not lookup: a sub-agent run's expanded span opens with the
// delegated prompt (toolView.task), and unlike every other body on this struct that text never came
// from a result — it is the call's own argument, and the arguments are not on the wire. A record
// that dropped it would replay as a run whose opening prompt block vanished: the scrollback changing
// shape across a restart, the same thing Solo and Stat are here to prevent. It is ADDITIVE on the
// same rule and carries omitempty — only a sub_agent head ever fills it, so no other record's blob
// grows a byte, and a blob written before it decodes with no prompt, which is the shape every such
// record was written under.
//
// Regions travel because they are the CHANGE ITSELF rather than a rendering of it (toolView.Regions,
// domain.EditRegion — ADR 0052 §5). Details keeps carrying the stacked rows, so a diff block replays
// its body without them; what cannot be replayed without them is the SPLIT reading, which is composed
// at paint time where the width is known and has nothing to compose from once the regions are gone. A
// resumed session would paint every block it had split as stacked rows instead — the scrollback
// changing shape across a restart, the same thing Solo, Stat and Task are each here to prevent.
// RegionFiles rides beside them on the same rule: it is the file each region was cut from, one name
// per region and ALIGNED index-for-index with Regions, and it is how a diff spanning several files
// keeps its per-file header rows (regionFileSections). A record that lost it degrades to one nameless
// section — a missing header, never a wrong body — which is why the two are one decision and not two.
//
// Both members are ADDITIVE within transcriptVersion and need no bump, on the wireEntry rule: they
// take omitempty, so every record that is not diff-bodied writes exactly the bytes it wrote before
// (TestTranscriptCodecGoldenV1), an older build ignores members it cannot place, and a blob written
// before them decodes with no regions and paints the stacked rows its Details already hold — which is
// the reading every such record was written under.
type wireToolView struct {
	Label       string            `json:"label,omitempty"`
	Verb        string            `json:"verb,omitempty"`
	Target      string            `json:"target,omitempty"`
	Name        string            `json:"name,omitempty"`
	Solo        bool              `json:"solo,omitempty"`
	Stat        string            `json:"stat,omitempty"`
	StatValue   *wireStatValue    `json:"statValue,omitempty"`
	Task        string            `json:"task,omitempty"`
	Summary     wireBranchSummary `json:"summary"`
	Details     []wireDetailLine  `json:"details,omitempty"`
	Regions     []wireEditRegion  `json:"regions,omitempty"`
	RegionFiles []string          `json:"regionFiles,omitempty"`
}

// wireDetailLine is the serialized form of a [detailLine]. Kind is stored as its underlying
// integer value, so the [detailKind] constants (detailPlain=0, detailDiffAdded=1,
// detailDiffRemoved=2) are pinned by value: they must never be reordered or renumbered, or an
// old file's diff colours would shift. A string enum was not used here because detailKind is a
// closed rendering hint, not an evolving vocabulary like the entry kinds.
//
// Gutter carries the chrome column a line leads with ([detailLine.Gutter] — a stacked diff row's
// line number) because a body is replayed from these lines and not rebuilt: a record whose gutters
// were left off the wire would come back as numberless diff rows. It is ADDITIVE within
// transcriptVersion on the wireEntry rule — it takes omitempty, so every line that has no such
// column writes nothing new, and a blob written before it decodes with the empty gutter those
// records were written under.
type wireDetailLine struct {
	Kind   int    `json:"kind,omitempty"`
	Gutter string `json:"gutter,omitempty"`
	Text   string `json:"text,omitempty"`
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
// Stat rides beside them on the same rule and for the reason wireToolView.StatValue states: the
// arithmetic a run's type row adds up lives under the phrase, not in it, and a record that came back
// with the phrase alone would replay a group whose total had gone blank.
type wireBranchSummary struct {
	wireDetailLine
	Quoted bool           `json:"quoted,omitempty"`
	Stat   *wireStatValue `json:"stat,omitempty"`
}

// wireStatValue is the serialized form of a [statValue] that HAS arithmetic — the counted noun or
// the diffstat an outcome slot's phrase spells out. A phrase with none is the text it already is and
// rides the wire as that text alone (wireBranchSummary.Text, wireToolView.Stat), so nothing here is
// ever written for one.
//
// It is a mirror of the value rather than the value itself, on this file's standing rule: what a
// session record looks like on disk is decided here, and a member added to the presenter's type must
// not change the wire form without someone choosing it. Counted is the discriminator — the two
// readings share no member, and a diffstat is the shape a record without it carries.
//
// The nouns travel as their PRODUCER spelled them ("1 entries", "2 changed"), which is what the value
// itself carries and the only thing that lets a replayed run spell its total the way the live one
// did. NounForOne and NounForMany are the spellings for a count of one and for any other count; a
// record straight off a producer fills the one its own count asks for, a record of a summed run may
// carry both.
type wireStatValue struct {
	Counted     bool   `json:"counted,omitempty"`
	N           int    `json:"n,omitempty"`
	NounForOne  string `json:"nounForOne,omitempty"`
	NounForMany string `json:"nounForMany,omitempty"`
	Added       int    `json:"added,omitempty"`
	Removed     int    `json:"removed,omitempty"`
}

// toWireStatValue projects the arithmetic half of a stat value onto the wire, and answers nil for a
// phrase that has none — the omitempty case every plain slot takes.
func toWireStatValue(v statValue) *wireStatValue {
	if !v.sums() {
		return nil
	}
	if v.kind == statCounted {
		return &wireStatValue{
			Counted:     true,
			N:           v.n,
			NounForOne:  v.nounForOne,
			NounForMany: v.nounForMany,
		}
	}
	return &wireStatValue{Added: v.added, Removed: v.removed}
}

// fromWireStatValue reads that arithmetic back, falling back to the phrase as a plain value — which
// is what a record written before the value rode the wire carries, and what every slot with no
// arithmetic in it carries by rule.
//
// For the old record that fallback is an ACCEPTED one-way break. A slot written before 113b3078
// carries no statValue, so it decodes as a plain value; a plain value does not sum (statValue.sums),
// so the grouped type row of a replayed multi-call run shows an empty total where the live run
// showed one. The value is deliberately NOT re-derived from the phrase: the phrase parsing that once
// read a total back out of the members' wording is gone and does not come back, so the same file
// reads differently than it did before that commit. Only runs of two or more members are affected,
// and only session files already on disk — everything written since carries the arithmetic.
func fromWireStatValue(w *wireStatValue, text string) statValue {
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

// wireEditRegion is the serialized form of a [domain.EditRegion]: one changed region of a diff — the
// lines it removed and the lines it inserted, the unchanged context bracketing them, and the 1-based
// line the region starts on in the before and in the after file.
//
// The domain type is MIRRORED here rather than serialized straight, unlike the view field that holds
// it (toolView.Regions reuses the domain type because the facts are the tool's). What a session
// record looks like on disk is this file's decision, and a member added to domain.EditRegion must not
// change the wire form without someone choosing it here — the same reason every other struct in this
// file is a mirror.
//
// Every member takes omitempty. The two starts are 1-based, so the zero they omit is a value no
// region carries, and an omitted number decodes to that same zero either way; an absent line slice
// and an empty one are the same fact — no lines there — so a region comes back rendering exactly as
// it was written.
type wireEditRegion struct {
	BeforeStart int      `json:"beforeStart,omitempty"`
	AfterStart  int      `json:"afterStart,omitempty"`
	Leading     []string `json:"leading,omitempty"`
	Removed     []string `json:"removed,omitempty"`
	Inserted    []string `json:"inserted,omitempty"`
	Trailing    []string `json:"trailing,omitempty"`
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
		name := e.kind.persistedName()
		if name == "" {
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
		CtxModel:    e.ctxModel,

		UsageCalls:            e.usage.Calls,
		UsagePromptTokens:     e.usage.PromptTokens,
		UsageCompletionTokens: e.usage.CompletionTokens,
		UsageTotalTokens:      e.usage.TotalTokens,

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
		Label:     tv.Label,
		Verb:      tv.Verb,
		Target:    tv.Target,
		Name:      tv.name,
		Solo:      tv.solo,
		Stat:      tv.stat.spell(),
		StatValue: toWireStatValue(tv.stat),
		Task:      tv.task,
		Summary: wireBranchSummary{
			wireDetailLine: wireDetailLine{Kind: int(tv.Summary.Kind), Text: tv.Summary.Text},
			Quoted:         tv.Summary.quoted,
			Stat:           toWireStatValue(tv.Summary.stat),
		},
	}
	if tv.Details.len() > 0 {
		w.Details = make([]wireDetailLine, 0, tv.Details.len())
		for _, d := range tv.Details.all() {
			w.Details = append(w.Details, wireDetailLine{Kind: int(d.Kind), Gutter: d.Gutter, Text: d.Text})
		}
	}
	// The regions ride BESIDE the rows they were rendered into, not instead of them: Details keeps
	// the stacked reading an older build replays as it stands, and these carry the change a split
	// reading has to be re-composed from at paint time. The file names travel with them because they
	// only mean anything beside them — one name per region, in the regions' own order.
	if len(tv.Regions) > 0 {
		w.Regions = make([]wireEditRegion, 0, len(tv.Regions))
		for _, r := range tv.Regions {
			w.Regions = append(w.Regions, wireEditRegion{
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
func toWirePresented(pv presentedView) *wirePresented {
	location := pv.Location
	if pv.Method == domain.PresentServed {
		location = ""
	}
	return &wirePresented{
		Title:    pv.Title,
		Path:     pv.Path,
		Location: location,
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
		ctxModel:    stripEscapes(w.CtxModel),
		usage: usageTotals{
			Calls:            w.UsageCalls,
			PromptTokens:     w.UsagePromptTokens,
			CompletionTokens: w.UsageCompletionTokens,
			TotalTokens:      w.UsageTotalTokens,
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
	if e.kind == entrySchedule && !e.done {
		closeInterruptedFiring(&e)
	}
	return e, true
}

// closeInterruptedCalls closes every tool call a decoded record left OPEN, wording each one with the
// outcome that actually befell it (interruptedSummary), and reports how many it closed. It is the
// firing rule one kind over (fromWireEntry): a record can now be written mid-Turn while a delegation
// runs (the progress save, ADR 0022's 2026-08-25 addendum), so the blob's last sub_agent head — and
// every child call standing under it — is stored open, and the work behind it died with the engine
// that was running it. A resume that replayed those as stored would paint a dead child as running,
// with no later fold able to correct it: the result those calls are waiting for is never coming,
// because a resumed record re-attempts the delegating Turn from its boundary rather than rejoining
// it (ADR 0007). It also covers records the cancelled-Turn path has always written with open calls.
//
// It is a POST-DECODE pass over the whole slice rather than a per-entry rewrite inside fromWireEntry,
// for the two reasons the firing rule is the opposite: the caller needs the COUNT (one note is added
// when anything was closed, replayScrollback), and this rule is about every kind of call rather than
// the one kind a firing block is. An entry the firing rule already closed is skipped by the same
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
		name:   stripEscapes(w.Name),
		solo:   w.Solo,
		// sanitize below strips the stat with the other display fields; a value with arithmetic in
		// it has nothing to strip and comes back as the numbers it is.
		stat:    fromWireStatValue(w.StatValue, w.Stat),
		task:    w.Task, // ditto: a delegated prompt off disk is untrusted text like any other
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
	// sanitize below escape-strips the region lines and the names with every other display field:
	// they are tool-recorded FILE CONTENT off disk, which is exactly what that seam is for.
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
