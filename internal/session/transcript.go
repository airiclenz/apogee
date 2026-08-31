package session

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/sanitize"
)

// The neutral transcript model and codec — the versioned wire form of a Record.Transcript blob.
//
// This file is Driver-neutral by design (ADR 0031): the scrollback a run produces is a fact about
// the conversation, not about the surface that painted it, so any Driver — the TUI, a bench
// harness, the daemon — writes and replays it through the same types. The store still treats the
// blob as opaque; what changed is that the shape is declared HERE rather than inside one Driver.
//
// The types below are the wire form and nothing else: exported mirrors of what a record holds, with
// their JSON tags as the compatibility contract. A Driver's own presentation model (the TUI's
// entry / toolView, say) projects onto them and rebuilds from them; a member added to a Driver's
// model must be chosen onto the wire here rather than arriving by accident.
//
// Version ownership follows ADR 0022 §5: this package version-checks only its OWN payloads, and the
// transcript blob is a payload of its own — TranscriptVersion gates the scrollback, RecordVersion
// gates the wrapper, and domain.SessionVersion gates the engine envelope. The three move
// independently and each rejects forward with its own sentinel.
//
// Decode is defensive: a session file is untrusted disk input, so every rendered string field is
// re-run through sanitize.StripEscapes on the way back in (defence in depth — a tampered file
// cannot smuggle a terminal escape sequence into a Driver's frame), a version newer than this build
// is refused with ErrTranscriptVersion, and a malformed blob returns an error the caller degrades
// to a no-replay note. Two rules are deliberately NOT applied here, because they belong to whoever
// consumes the entries: an entry whose Kind this build does not know comes back AS STORED (skipping
// it is the consumer's call), and a SkillSpan is handed back verbatim (only the consumer knows
// whether the offsets still locate a run of the text it means to paint).

// TranscriptVersion is the schema version EncodeTranscript stamps on every scrollback blob it
// writes. A blob whose version exceeds this build's is refused with ErrTranscriptVersion — the same
// reject-forward rule domain.SessionVersion and RecordVersion apply, owned by this payload alone
// and independent of them.
const TranscriptVersion = 1

// ErrTranscriptVersion is returned by DecodeTranscript when a blob's version exceeds this build's
// TranscriptVersion — scrollback written by a newer Apogee, refused rather than misread. The caller
// degrades to resuming with no scrollback replay.
var ErrTranscriptVersion = errors.New("apogee: unsupported transcript version")

// The nine persisted entry kinds. The kind is serialized as a STRING enum rather than a Driver's
// own iota, so a future reordering of that Driver's constants can never re-interpret an old file.
// A Driver kind with no name here — the TUI's one-time start-up box, say — is simply never written,
// and a name this build does not know decodes as an entry of an unrecognised kind.
const (
	EntryKindUser        = "user"
	EntryKindAssistant   = "assistant"
	EntryKindToolCall    = "toolCall"
	EntryKindToolResult  = "toolResult"
	EntryKindError       = "error"
	EntryKindNote        = "note"
	EntryKindPresented   = "presented"
	EntryKindInterjected = "interjected"
	EntryKindSchedule    = "schedule"
)

// interruptedSummary is the outcome CloseInterruptedCalls words a still-open call with — the one
// wording no live fold ever writes, because it describes what befell a call between the write and
// the read rather than anything the call reported.
const interruptedSummary = "interrupted — the run did not finish"

// envelope is the top-level serialized form of the scrollback: a version tag plus the committed
// entries in display order. The version gates forward compatibility for the whole blob; individual
// entry kinds are additive within a version. It stays unexported — callers hand over and receive
// entries, and the framing is the codec's own business.
type envelope struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Entry is the serialized form of one committed scrollback entry. Kind is the string enum above;
// the two view structs hang off their own optional members so a non-tool / non-presented entry
// serializes without them. Nothing in-progress reaches here — a Driver's pending buffer, its
// one-time chrome and its display-only notices are filtered out before EncodeTranscript is called.
//
// Members are ADDITIVE within TranscriptVersion and need no bump: each takes omitempty, so a record
// that has nothing to say about a member writes no bytes for it, an older build ignores a member it
// cannot place, and a blob written before a member existed decodes to that member's zero — which is
// the reading such a record was actually written under. A member can also be RETIRED on the mirror
// of that rule: json.Unmarshal ignores what no field claims any more.
type Entry struct {
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Depth  int    `json:"depth,omitempty"`
	CallID string `json:"callID,omitempty"`
	// SpawnCallID is the run identity of a delegated entry: the id of the sub_agent call that
	// spawned the agent whose event it folded from. A top-level entry writes nothing, and a blob
	// written before it existed decodes to "" for every entry — the one run a serialized session
	// ever had, which is how such a record was written and how it still reads.
	SpawnCallID string `json:"spawnCallID,omitempty"`
	Done        bool   `json:"done,omitempty"`
	// The fill a sub-agent run's head wears: what the delegate's context held when it last
	// reported, and the window that reading filled. The two travel together, because a fill says
	// nothing without its limit. Frozen at fold time, so what a record keeps is the reading as it
	// stood when the run reported.
	CtxUsed  int `json:"ctxUsed,omitempty"`
	CtxLimit int `json:"ctxLimit,omitempty"`
	// CtxModel is the model a sub-agent run used when it was not the session's own — routing to the
	// Sub-agent server (ADR 0045) — and absent when it was. Frozen at fold time like the pair
	// above, so a resumed session reports the model the run ACTUALLY used rather than the one it
	// happens to reopen on.
	CtxModel string `json:"ctxModel,omitempty"`
	// The cumulative token accounting a sub-agent run's head wears: what the delegate spent over
	// the whole run, as of its last report. UsageCachedPromptTokens travels between the prompt count
	// and the completion count because that is what it is — the share of THOSE prompt tokens the
	// server answered from its own cache, not a spend beside them.
	UsageCalls              int         `json:"usageCalls,omitempty"`
	UsagePromptTokens       int         `json:"usagePromptTokens,omitempty"`
	UsageCachedPromptTokens int         `json:"usageCachedPromptTokens,omitempty"`
	UsageCompletionTokens   int         `json:"usageCompletionTokens,omitempty"`
	UsageTotalTokens        int         `json:"usageTotalTokens,omitempty"`
	SkillSpans              []SkillSpan `json:"skillSpans,omitempty"`
	Tool                    *ToolView   `json:"tool,omitempty"`
	Presented               *Presented  `json:"presented,omitempty"`
}

// SkillSpan is the byte range one invoked "/token" occupies in the entry's own Text. Offsets travel
// rather than the token text, because the text is the record and a span only points into it — which
// is why a consumer re-checks a span against the text it arrives with instead of trusting the file.
//
// Neither offset takes omitempty: start 0 is the ordinary case (a message opening with its token),
// and a half-written span would read as a live one. The slice member carries the omission instead.
type SkillSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ToolView is the serialized form of a tool call's card, including the raw tool Name — the id a
// result extractor keys on, so a replayed tool-call entry keeps working exactly as a freshly folded
// one.
//
// Several members are a presenter's VERDICT rather than a fact the call reported, and they travel
// because the decode path never re-runs a presenter: Solo (whether the card stands alone or folds
// into its neighbours' group), Stat and StatValue (the promoted outcome's phrase and the arithmetic
// under it, which a run's type row adds up), and Summary.Quoted (whose words the branch line is). A
// record that came back without them would replay as a scrollback that changed shape across a
// restart, which is exactly what the round trip exists to prevent.
//
// Task is BODY rather than lookup: a sub-agent run's expanded span opens with the delegated prompt,
// and unlike every other body here that text is the call's own argument.
//
// Regions and RegionFiles are the CHANGE ITSELF rather than a rendering of it (domain.EditRegion —
// ADR 0052 §5). Details keeps carrying the stacked rows, so a diff block replays its body without
// them; what cannot be replayed without them is the SPLIT reading, composed at paint time where the
// width is known. RegionFiles is the file each region was cut from, one name per region and ALIGNED
// index-for-index with Regions.
//
// Args answers a different question from every member above it: those are what the card SHOWED,
// this is what the call ASKED — the bounded, compact copy of the model's own arguments, carried so a
// delegate's tool use can still be read back once its run is closed. It is STORED and not replayed:
// the codec hands it back exactly as it stands, and the surface that eventually shows it is the
// surface that must strip it.
type ToolView struct {
	Label       string          `json:"label,omitempty"`
	Verb        string          `json:"verb,omitempty"`
	Target      string          `json:"target,omitempty"`
	Name        string          `json:"name,omitempty"`
	Solo        bool            `json:"solo,omitempty"`
	Stat        string          `json:"stat,omitempty"`
	StatValue   *StatValue      `json:"statValue,omitempty"`
	Task        string          `json:"task,omitempty"`
	Summary     BranchSummary   `json:"summary"`
	Details     []DetailLine    `json:"details,omitempty"`
	Regions     []EditRegion    `json:"regions,omitempty"`
	RegionFiles []string        `json:"regionFiles,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
}

// DetailLine is one line of a tool card's body. Kind is stored as its underlying integer value, so
// a Driver's rendering-hint constants (plain=0, diff-added=1, diff-removed=2) are pinned BY VALUE:
// they must never be reordered or renumbered, or an old file's diff colours would shift. A string
// enum was not used here because that hint is a closed rendering vocabulary, not an evolving one
// like the entry kinds.
//
// Gutter carries the chrome column a line leads with — a stacked diff row's line number — because a
// body is replayed from these lines and not rebuilt: a record whose gutters were left off the wire
// would come back as numberless diff rows.
type DetailLine struct {
	Kind   int    `json:"kind,omitempty"`
	Gutter string `json:"gutter,omitempty"`
	Text   string `json:"text,omitempty"`
}

// BranchSummary is the branch line's text together with the two facts that are not IN that text —
// whose words it is (Quoted) and the arithmetic under it (Stat). It embeds DetailLine rather than
// restating its members, so the line's kind and gutter travel with its text inside the one
// "summary" object ({"text":"…","quoted":true}).
type BranchSummary struct {
	DetailLine
	Quoted bool       `json:"quoted,omitempty"`
	Stat   *StatValue `json:"stat,omitempty"`
}

// StatValue is the serialized form of an outcome phrase that HAS arithmetic — the counted noun or
// the diffstat the slot spells out. A phrase with none is the text it already is and rides the wire
// as that text alone (BranchSummary.Text, ToolView.Stat), so nothing here is ever written for one.
//
// Counted is the discriminator: the two readings share no member, and a diffstat is the shape a
// record without it carries. The nouns travel as their PRODUCER spelled them ("1 entry",
// "2 changed"), which is the only thing that lets a replayed run spell its total the way the live
// one did; NounForOne and NounForMany are the spellings for a count of one and for any other count.
type StatValue struct {
	Counted     bool   `json:"counted,omitempty"`
	N           int    `json:"n,omitempty"`
	NounForOne  string `json:"nounForOne,omitempty"`
	NounForMany string `json:"nounForMany,omitempty"`
	Added       int    `json:"added,omitempty"`
	Removed     int    `json:"removed,omitempty"`
}

// EditRegion is the serialized form of a domain.EditRegion: one changed region of a diff — the
// lines it removed and the lines it inserted, the unchanged context bracketing them, and the 1-based
// line the region starts on in the before and in the after file.
//
// The domain type is MIRRORED here rather than serialized straight. What a session record looks like
// on disk is this file's decision, and a member added to domain.EditRegion must not change the wire
// form without someone choosing it here — the same reason every other struct in this file is a
// mirror.
//
// Every member takes omitempty. The two starts are 1-based, so the zero they omit is a value no
// region carries; an absent line slice and an empty one are the same fact — no lines there — so a
// region comes back rendering exactly as it was written.
type EditRegion struct {
	BeforeStart int      `json:"beforeStart,omitempty"`
	AfterStart  int      `json:"afterStart,omitempty"`
	Leading     []string `json:"leading,omitempty"`
	Removed     []string `json:"removed,omitempty"`
	Inserted    []string `json:"inserted,omitempty"`
	Trailing    []string `json:"trailing,omitempty"`
}

// Presented is the serialized form of a presented-document entry. Method is stored as its domain
// string (domain.PresentMethod's underlying value) rather than an internal code, so the wire form
// reads honestly and survives a constant reorder, and an unrecognised value falls to a consumer's
// baseline wording rather than reaching the terminal as text.
type Presented struct {
	Title    string `json:"title,omitempty"`
	Path     string `json:"path,omitempty"`
	Location string `json:"location,omitempty"`
	Method   string `json:"method,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// EncodeTranscript serializes committed entries into the versioned wire blob, stamped with
// TranscriptVersion. It writes exactly what it is handed: deciding which of a Driver's entries are
// committed and persisted — and dropping the ones that are neither — happens before the call, so
// this seam adds no filtering rule a Driver would have to know about.
func EncodeTranscript(entries []Entry) ([]byte, error) {
	// The member has no omitempty, so a nil slice would write "entries":null where an empty
	// scrollback has always written "entries":[]. Both decode the same, but the bytes are the
	// contract this codec pins.
	if entries == nil {
		entries = []Entry{}
	}
	data, err := json.Marshal(envelope{Version: TranscriptVersion, Entries: entries})
	if err != nil {
		return nil, fmt.Errorf("apogee: encode transcript: %w", err)
	}
	return data, nil
}

// DecodeTranscript turns a stored scrollback blob back into committed entries for replay. Empty or
// nil data is the legacy / never-recorded case and yields (nil, nil) — the caller resumes with no
// scrollback. A version newer than this build is refused (ErrTranscriptVersion) and any other
// malformed input returns a decode error; the caller degrades both to a no-replay note.
//
// Every rendered string field passes through sanitize.StripEscapes on the way out: a session file is
// untrusted disk input, so the terminal-escape defence a Driver applies on the way in is re-applied
// here (stripEntry). Args is the one string-ish member deliberately left alone — it is stored, never
// painted, and the surface that eventually shows it is the surface that must strip it.
//
// An entry whose Kind this build does not know comes back AS STORED; skipping it is the consumer's
// rule, not the codec's.
func DecodeTranscript(data []byte) ([]Entry, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("apogee: decode transcript: %w", err)
	}
	if env.Version > TranscriptVersion {
		return nil, ErrTranscriptVersion
	}
	for i := range env.Entries {
		stripEntry(&env.Entries[i])
	}
	return env.Entries, nil
}

// stripEntry re-runs the terminal-escape defence over every field of one decoded entry that a
// Driver can paint. The list is the union of the two enumerations a Driver's own decode path used
// to hold — the fields it stripped one by one, and the ones its card-wide sanitize pass covered —
// so nothing that reaches a frame is left to a Driver to remember. Stripping is idempotent, so a
// Driver keeping its own pass loses nothing by running it again.
//
// Ids are not stripped: CallID and SpawnCallID are match keys, never rendered. Name is stripped
// even though it is a lookup key rather than display text, because a smuggled escape in it would
// reach the raw-fallback label a card falls back to when it has no friendly one.
func stripEntry(e *Entry) {
	e.Text = sanitize.StripEscapes(e.Text)
	e.CtxModel = sanitize.StripEscapes(e.CtxModel)
	if e.Tool != nil {
		stripToolView(e.Tool)
	}
	if e.Presented != nil {
		p := e.Presented
		p.Title = sanitize.StripEscapes(p.Title)
		p.Path = sanitize.StripEscapes(p.Path)
		p.Location = sanitize.StripEscapes(p.Location)
		p.Reason = sanitize.StripEscapes(p.Reason)
		// Method is a closed enum matched against domain constants; an unrecognised value falls to a
		// consumer's baseline wording rather than reaching the terminal as text.
	}
}

// stripToolView is the card's half of that defence: every rendered field of the call, its outcome
// slot, its body and the diff regions beneath it. A region holds tool-recorded FILE CONTENT, which a
// malicious repo owns every byte of and both readings of the body paint straight from, so the region
// lines and the file names beside them are stripped like any other display text.
func stripToolView(tv *ToolView) {
	tv.Label = sanitize.StripEscapes(tv.Label)
	tv.Verb = sanitize.StripEscapes(tv.Verb)
	tv.Target = sanitize.StripEscapes(tv.Target)
	tv.Name = sanitize.StripEscapes(tv.Name)
	tv.Stat = sanitize.StripEscapes(tv.Stat)
	tv.Task = sanitize.StripEscapes(tv.Task)
	stripStatValue(tv.StatValue)
	tv.Summary.Text = sanitize.StripEscapes(tv.Summary.Text)
	tv.Summary.Gutter = sanitize.StripEscapes(tv.Summary.Gutter)
	stripStatValue(tv.Summary.Stat)
	for i := range tv.Details {
		tv.Details[i].Text = sanitize.StripEscapes(tv.Details[i].Text)
		tv.Details[i].Gutter = sanitize.StripEscapes(tv.Details[i].Gutter)
	}
	for i := range tv.Regions {
		r := &tv.Regions[i]
		r.Leading = sanitize.StripEscapesAll(r.Leading)
		r.Removed = sanitize.StripEscapesAll(r.Removed)
		r.Inserted = sanitize.StripEscapesAll(r.Inserted)
		r.Trailing = sanitize.StripEscapesAll(r.Trailing)
	}
	tv.RegionFiles = sanitize.StripEscapesAll(tv.RegionFiles)
}

// stripStatValue strips the nouns an arithmetic outcome spells its total with. The numbers have
// nothing to strip; the nouns are producer-spelled text that a tampered record could carry an escape
// in, and they are painted straight into a run's type row.
func stripStatValue(v *StatValue) {
	if v == nil {
		return
	}
	v.NounForOne = sanitize.StripEscapes(v.NounForOne)
	v.NounForMany = sanitize.StripEscapes(v.NounForMany)
}

// CloseInterruptedCalls closes every tool call a decoded record left OPEN, wording each one with the
// outcome that actually befell it, and reports how many it closed.
//
// A record can be written mid-Turn while a delegation runs (the progress save, ADR 0022's 2026-08-25
// addendum), so the blob's last sub_agent head — and every child call standing under it — is stored
// open, and the work behind it died with the engine that was running it. A resume that replayed
// those as stored would show a dead child as running, with no later fold able to correct it: the
// result those calls are waiting for is never coming, because a resumed record re-attempts the
// delegating Turn from its boundary rather than rejoining it (ADR 0007). It also covers records the
// cancelled-Turn path has always written with open calls.
//
// It is a pass over the whole slice rather than a per-entry rewrite, because the caller needs the
// COUNT — one note is worth adding when anything was closed. An entry already settled is skipped by
// the clause that skips every other closed call, and so is a toolCall carrying no card at all: Tool
// is a pointer on the wire, and a hand-written or truncated blob can perfectly well omit it.
//
// It mutates the entries in place, which is what a freshly decoded slice is for.
func CloseInterruptedCalls(entries []Entry) (closed int) {
	for i := range entries {
		e := &entries[i]
		if e.Kind != EntryKindToolCall || e.Done || e.Tool == nil {
			continue
		}
		e.Done = true
		e.Tool.Summary = BranchSummary{DetailLine: DetailLine{Text: interruptedSummary}}
		closed++
	}
	return closed
}
