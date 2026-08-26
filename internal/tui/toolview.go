package tui

import (
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The tool card — the value type, and the lifecycle that fills it
// ----------------------------------------------------------------------------
//
// This file holds the CARD a tool call becomes and the two moments that fill it (ADR 0043);
// toolregistry.go keeps the presentation vocabulary itself — the labels, verbs, targets and
// per-tool hooks the lifecycle here reads out of the registry.
//
// The card is [toolView]: the friendly label for the header line (✦ Read), the target that leads
// the branch beneath it, the one-line summary the branch row seats in its right-aligned outcome
// slot, and the body lines under it — built out of [detailLine]s a [detailKind] colours and a
// [toolBody] carries, with [branchSummary] saying whether the slot's text is the view's own
// wording or a line the tool itself printed (quoted).
//
// The lifecycle is two calls a block's life runs through, and nothing else writes a card:
// [presentToolCall] builds the header (and, for the tools whose ARGUMENTS already say what the
// call will change, its body) the moment the call is seen, and [toolView.enrichWithResult]
// absorbs the result when it lands. Both leave through [toolView.finishDisplay], which is where
// the two rules that must hold for every card live once rather than per producer:
// [toolView.sanitize] escape-strips every string a hostile model or repo owns (doc.go's second
// invariant) and [toolView.shortenPaths] spells the paths the card NAMES relative to the
// workspace root. The run aggregation below them ([runAggregate] and the sums under it) is the
// same card shape one level up: what a whole group of calls did, ADDED from the typed values its
// members carry ([statValue]) rather than read back out of the phrases they show.
//
// It stays pure — no lipgloss, no I/O — so it is trivially table-testable (TestPresentToolCall);
// render.go owns the styling and the block shape.

// detailKind tags a tool-detail line so the renderer can colour it. The diff kinds are emitted by
// diffBody (view_diff's body renderer) and by changedLines (the edit tools' and write_file's body,
// derived from their own arguments), and rendered red/green in render.go.
type detailKind int

const (
	detailPlain detailKind = iota
	detailDiffAdded
	detailDiffRemoved
)

// detailLine is one line of a tool call's outcome — a short summary (detailPlain) or a
// red/green diff line (detailDiffAdded/detailDiffRemoved). Where a line lands is not its own
// business but that of the [toolView] field holding it: the Summary rides the branch row's
// right-aligned outcome slot, a Details line lays out beneath it (render.go owns that shape).
type detailLine struct {
	Kind detailKind

	// Gutter is the CHROME column this line carries between its frame's own prefix and its text,
	// empty on every line that has none. Only the stacked diff reading fills it today, with the
	// right-aligned line number the row sits on ([stackedRow.line]).
	//
	// It is a member of its own rather than the head of Text because the two are painted
	// differently: a diff kind's band is the TEXT's field, so a number sharing that string would be
	// tinted along with it, where ratified call 3 of docs/plans/"2026-08-19 - 05" ("the gutter stays
	// chrome") holds the numbers out of the band. No wrap rail can make that split for itself — it
	// receives one opaque string and cannot tell a number from the code beside it — so the line
	// arrives at the style seam already parted ([bodyFrame.paint], which widens the row's hanging
	// prefix by this column and lets the primitives keep prefixes chrome). The split panes hold
	// their numbers out the same way ([splitCell.paint]), so the two readings of one body agree
	// about what is chrome.
	Gutter string

	Text string
}

// branchSummary is the one-line outcome filling the branch row's outcome slot, bound to the one
// fact the shortening seam depends on: WHOSE words it is. Most summaries are the presenter's own —
// a typed phrase worded by the tool's stat hook ("154 lines", "+2 −2"), a tool's own report sentence
// ("replaced text in <path>"), an "error: …" line — and the workspace root is shortened out of the
// paths those NAME (toolView.shortenPaths). Some are not: a line the block did not word is PROMOTED
// into this slot as it stands (promotedOutput), and it is quoted content, no different from a body.
// Output that came to exactly one line is one such line (outputDetail) — a `cat` printing an
// in-workspace path must show the spelling the file holds, not the transcript's shorter one for it
// — and so is the answer a human typed into an ask_user question (quotedFirstLineDetail), which is
// their words and not a report about a path.
//
// The mark travels WITH the text: the summary a prose extractor builds and the summary the view
// carries are one type, so the fact cannot be dropped in the hand-off (enrichWithResult), and no
// seam has to guess it from the text — a line of output can look exactly like a path. A body needs
// no such mark: it is quoted whole, so the shortening seam simply never reaches it.
type branchSummary struct {
	detailLine
	quoted bool

	// failed is the block's OWN verdict that this call failed — the red the outcome slot takes
	// (summaryStyle), which design call 11 makes the whole of the failure marking. Only a summary
	// the block WORDED carries it: a named summary spelled in the failure vocabulary above
	// (namedSummary), a run's count of its failed members (runAggregate), a delegation whose head
	// reported one (subAgentSummary). A quoted line never does — a `cat` of a log opening
	// "error: no such host" is the FILE's word about itself, and lifting it into the slot moved
	// where it sits without making it apogee's verdict about the call (F-29).
	//
	// It rides WITH the text for the reason quoted does: the wording is put to the vocabulary once,
	// at the seam that words it (failedSummary), and every reader downstream asks the field instead
	// of reading the same sentence back out — summaryStyle, failedCalls, subAgentFinished.
	failed bool

	// stat is the ARITHMETIC the text spells out, for the summaries a presenter worded from a fact
	// it had counted: a run's type row adds its members' stats up from these rather than reading its
	// own wording back out of them (sumStats). It is empty on every other summary — a quoted
	// promotion is the tool's words, a prose sentence counts nothing, and a call still in flight has
	// no outcome at all — which is exactly the set a run may not sum.
	//
	// It always spells the text beside it (typedSummary), and stays in step with it through the
	// display seams: only a phrase with no arithmetic can name a path or carry an ESC byte, and such
	// a phrase reaches the slot as text with no value beside it.
	stat statValue
}

// The wordings that mark an outcome as a FAILURE, whatever produced it: the "error: …" line a
// faulted result is summarised with (enrichWithResult), and the three bare verdicts a call that
// never ran — or never finished — carries. The painter reads them to give the outcome slot its red
// (summaryStyle, render.go), and design call 11 of docs/plans/"2026-08-10 - 04" makes that red the
// ONLY failure marking — no glyph and no header changes colour — so the vocabulary and the mark stay
// one fact in one place.
//
// interruptedSummary is the one no live fold ever writes: it is what REPLAY closes a call with that
// was still open when the record was written (closeInterruptedCalls), so a delegation the record
// caught mid-run never replays as a run that reported. Being in this vocabulary is what makes
// subAgentFinished answer false for such a head — an interrupted run wears no done ✓.
const (
	errorSummaryPrefix = "error: "
	deniedSummary      = "denied"
	cancelledSummary   = "cancelled"
	interruptedSummary = "interrupted — the run did not finish"
)

// namedSummary is a summary in the presenter's OWN words — a typed phrase, or a sentence naming a
// path — so the shortening seam spells that path relative to the workspace.
//
// It is the ONE place named wording is put to the failure vocabulary (failedSummary): the words
// here are the block's own, so a verdict spelled in them IS the block's verdict, and it is settled
// onto the summary rather than re-derived by each seam that later has to know it.
func namedSummary(line detailLine) branchSummary {
	return branchSummary{detailLine: line, failed: failedSummary(line.Text)}
}

// quotedSummary is a summary carrying text the block QUOTES rather than words of its own — a
// one-line tool output promoted onto the branch — which no seam respells.
//
// It carries no verdict either (branchSummary.failed), for the same reason: whether the call failed
// is not the tool's to spell, so a promoted line reading "error: …" fills the slot without
// colouring it.
func quotedSummary(line detailLine) branchSummary {
	return branchSummary{detailLine: line, quoted: true}
}

// typedSummary is a summary the presenter worded from a [statValue] — a stat hook's counted noun or
// diffstat — spelled once here so the slot's text and the arithmetic carried beside it cannot come
// to say different things.
//
// Only a value with arithmetic is carried: a phrase with no sum in it ("exit 0", "clean", a short
// hash, the table's blank `—`) reaches the slot as the text it is, which is what keeps the carried
// value in step with the text through the display seams — those respell a plain phrase (a path
// shortened out of it, an ESC byte stripped) and can touch neither a count's digits nor a
// diffstat's.
func typedSummary(v statValue) branchSummary {
	line := detailLine{Text: v.spell()}
	if !v.sums() {
		// A stat's phrase is a READING and never a verdict (branchSummary.failed): "exit 0" says
		// what the call came to whatever number stands in it, so it is not put to the failure
		// vocabulary the way a worded sentence is.
		return branchSummary{detailLine: line}
	}
	return branchSummary{detailLine: line, stat: v}
}

// statKind says which reading a [statValue] carries. The three are exactly the shapes the
// registry's stat hooks answer in (toolregistry.go).
type statKind uint8

const (
	// statPlain is a phrase with no arithmetic — "exit 0", "PASS", "clean", "done", a short commit
	// hash, and the empty string the table spells `—`. It says what it says and never joins a sum.
	statPlain statKind = iota

	// statCounted is a counted noun — "12 lines", "1 entries", "3 hits" — held as the number and the
	// word beside it.
	statCounted

	// statDiffed is a diffstat — the added and removed line counts a slot spells "+8 −3".
	statDiffed
)

// statValue is what a tool's outcome slot SAYS, held as the fact rather than as the sentence: the
// registry's stat hooks answer in these, the view carries one beside every phrase they worded
// (branchSummary.stat, toolView.stat), and a type row adds a whole run of them up (sumStats).
// Spelling is the last step and happens in one place ([statValue.spell]) — the prose the slot shows
// is this value's rendering and never an input to anything, so no summer has to read a wording back
// out to get at the number under it.
//
// The counted variant keeps the noun AS ITS PRODUCER SPELLED IT rather than a stem to re-pluralise,
// because the spellings are not all mechanical: list_dir has always read "1 entries" and git_status
// "2 changed". A sum therefore carries both spellings it has seen — the first from a member counting
// exactly one, the first from a member counting anything else — and picks between them by the
// TOTAL's plurality, which is how a producer's fixed wording survives being added up.
type statValue struct {
	kind statKind

	// text is the plain variant's phrase, and the only member a display seam may rewrite: a count's
	// digits and a diffstat's numbers name no path and hold no ESC byte (stripped, shortened).
	text string

	// n is the count, and nounForOne / nounForMany the two spellings [statValue.spell] chooses
	// between. A value straight from a producer fills exactly the one its own count asks for; a sum
	// may hold both.
	n           int
	nounForOne  string
	nounForMany string

	// added and removed are the diffstat's two halves.
	added   int
	removed int
}

// plainStat is a slot phrase with no arithmetic. The empty string is the table's `—`: a slot
// stated blank, not one left over from a prose sentence (toolView.applyStat).
func plainStat(text string) statValue {
	return statValue{kind: statPlain, text: text}
}

// countedStat is a count together with the noun its producer spells THIS count with — "1 entries"
// and "2 changed" are as much a producer's word as "12 lines" is, so the word travels with the
// number instead of being re-derived from a stem.
func countedStat(n int, noun string) statValue {
	v := statValue{kind: statCounted, n: n}
	if n == 1 {
		v.nounForOne = noun
	} else {
		v.nounForMany = noun
	}
	return v
}

// pluralStat is [plural]'s typed counterpart: a count spelled by the house rule (the bare word for
// one, a trailing "s" for anything else), for the producers that word themselves that way. It goes
// through [pluralNoun], the same rule plural itself spells, so a typed count and a phrase written
// the old way cannot read differently.
func pluralStat(n int, word string) statValue {
	return countedStat(n, pluralNoun(n, word))
}

// diffedStat is a diffstat held as its two halves.
func diffedStat(added, removed int) statValue {
	return statValue{kind: statDiffed, added: added, removed: removed}
}

// diffCounts is the house spelling of a diffstat — "+8 −3", with the table's typographic minus
// (U+2212) rather than a hyphen, so the two halves read as a matched pair at any weight. Every slot
// that carries one is spelled here, through the value that holds it ([statValue.spell]).
func diffCounts(added, removed int) string {
	return "+" + strconv.Itoa(added) + " −" + strconv.Itoa(removed)
}

// sums says whether this value has arithmetic in it — whether a run of them adds up at all
// ([statValue.add]). A plain phrase does not, which is what leaves a type row over "exit 0" and
// "PASS" blank.
func (v statValue) sums() bool {
	return v.kind == statCounted || v.kind == statDiffed
}

// blank says the value words nothing at all — the zero value a view carries where no stat was ever
// produced, and the empty phrase a deliberately blank slot is stated with.
func (v statValue) blank() bool {
	return v.kind == statPlain && v.text == ""
}

// spell writes the value out as the phrase the outcome slot shows. A count is spelled with the noun
// its own plurality asks for, and falls back to the house plural only where no producer ever spelled
// that plurality — two members counting one each, making a two nobody wrote a word for.
func (v statValue) spell() string {
	switch v.kind {
	case statCounted:
		if noun := v.noun(); noun != "" {
			return strconv.Itoa(v.n) + " " + noun
		}
		return plural(v.n, v.stem())
	case statDiffed:
		return diffCounts(v.added, v.removed)
	}
	return v.text
}

// noun is the spelling this count's own plurality asks for, or "" where the value holds only the
// other one.
func (v statValue) noun() string {
	if v.n == 1 {
		return v.nounForOne
	}
	return v.nounForMany
}

// stem is the noun with a plural "s" trimmed off it: what two counts must share to be counting the
// same thing ([statValue.add]), and the word the house rule re-pluralises where a sum has no
// spelling of its own ([statValue.spell]).
func (v statValue) stem() string {
	if v.nounForOne != "" {
		return strings.TrimSuffix(v.nounForOne, "s")
	}
	return strings.TrimSuffix(v.nounForMany, "s")
}

// add totals two stat values and reports whether they add up at all. Two diffstats always do, on
// both halves. Two counts do where they count the SAME thing, judged on the noun with a plural "s"
// trimmed off it, so "1 line" and "12 lines" add and "3 hits" beside "2 files" does not. Nothing
// else adds — a plain phrase has no arithmetic, and two different readings are not one fact.
//
// The total keeps the FIRST spelling seen of each plurality, which is what makes a fold over a run
// answer exactly what a scan of it would: the sum spells itself with the earliest member whose count
// reads the way the total does.
func (v statValue) add(w statValue) (statValue, bool) {
	if v.kind != w.kind {
		return statValue{}, false
	}
	switch v.kind {
	case statCounted:
		if v.stem() != w.stem() {
			return statValue{}, false
		}
		sum := statValue{
			kind:        statCounted,
			n:           v.n + w.n,
			nounForOne:  v.nounForOne,
			nounForMany: v.nounForMany,
		}
		if sum.nounForOne == "" {
			sum.nounForOne = w.nounForOne
		}
		if sum.nounForMany == "" {
			sum.nounForMany = w.nounForMany
		}
		return sum, true
	case statDiffed:
		return diffedStat(v.added+w.added, v.removed+w.removed), true
	}
	return statValue{}, false
}

// stripped is the value's half of the card's escape-stripping seam (toolView.sanitize). Only a plain
// phrase can hold an ESC byte: a count is digits and a noun this package spells, and a diffstat is
// digits and two signs.
func (v statValue) stripped() statValue {
	if v.kind != statPlain {
		return v
	}
	return plainStat(stripEscapes(v.text))
}

// shortened is the value's half of the shortening seam (toolView.shortenPaths), on the same rule:
// only a plain phrase can name a path.
func (v statValue) shortened(ws workspaceRoot) statValue {
	if v.kind != statPlain {
		return v
	}
	return plainStat(ws.shorten(v.text))
}

// toolBody is a tool call's retained body — the detail lines that lay out beneath its branch line.
// Nothing stands above those lines: a body is what it holds, every line carrying its own Kind and
// so its own colour, and no summary of the set is kept beside them. There was one — a DIFF flag —
// and it sized the collapsed paint until one house budget replaced the per-kind caps
// (collapsedBodyRows, render.go); with no painter left to ask, a fact carried only to be re-derived
// in tests was machinery, and it is gone.
//
// The body is a type rather than a bare []detailLine so the acts that belong to a body travel with
// it — the sanitize seam's strip, the growth an arriving result folds in — and so the shape stays
// the one place to add whatever a body must next carry about itself. It is retained WHOLE
// (layout.md): the collapsed cap is the painter's, never a truncation performed where the lines
// are made.
type toolBody struct {
	lines []detailLine
}

// newToolBody makes a body from the lines that lay out beneath the branch — the one seam through
// which lines become a body, which is what gives that step somewhere to live. The zero toolBody
// needs no constructor: no lines is a body with nothing to lay out.
func newToolBody(lines []detailLine) toolBody {
	return toolBody{lines: lines}
}

// all is the body's lines, in order — what the painter lays out.
func (b toolBody) all() []detailLine { return b.lines }

// len is how many lines the body retains. It says nothing about the block's shape: that follows
// from which halves of the outcome are filled, never from how many lines there are (render.go).
func (b toolBody) len() int { return len(b.lines) }

// with returns the body extended by more lines — a result folding into a view that already has one
// (enrichWithResult). It goes back through the constructor, so a grown body is made the way every
// other body is. Adding nothing returns the body untouched, which is what keeps a not-yet-enriched
// call's nil body nil.
func (b toolBody) with(more []detailLine) toolBody {
	if len(more) == 0 {
		return b
	}
	return newToolBody(append(b.lines, more...))
}

// stripEscapes runs the package's control-character strip over every line's text in place (the
// sanitize seam's work on the body). It cannot disturb how a body paints: a line's Kind is set by
// its producer and the strip only ever rewrites Text.
func (b *toolBody) stripEscapes() {
	for i := range b.lines {
		b.lines[i].Text = stripEscapes(b.lines[i].Text)
	}
}

// toolView is the presentation model of a tool call (later enriched by its result): a
// friendly Label, the active Verb for the status line, the Target it acts on (a path, a
// directory, a pattern), and the outcome split in two — the one-line Summary that fills the
// branch row's outcome slot ("154 lines", "+2 −2", "error: …") and the Details body laid
// out beneath it (a command's output, a diff's lines). Either half may be empty: an empty
// Summary.Text means the call has no one-line outcome (one still in flight, a command run),
// and an empty Details means nothing hangs beneath. That split IS the block's grammar —
// the shape follows from which halves are filled, never from how many Details there are
// (render.go). name is the raw tool id, kept to pick the result extractor and as the
// raw-fallback label. Every Label renders the same way — bold gold (render.go) — so a raw
// fallback is not visually singled out.
type toolView struct {
	Label  string
	Verb   string
	Target string

	// Summary is the one-line outcome riding the branch line, carrying whose words it is
	// (branchSummary): the shortening seam respells the phrases the block writes itself and
	// never a one-line output promoted into this slot.
	Summary branchSummary

	// Details is the body laid out beneath the branch line (toolBody): the lines are retained
	// whole, each carrying its own Kind, and what a collapsed block shows of them is the
	// painter's cap rather than anything decided here.
	Details toolBody

	// Regions is the change itself, as the tool that applied it RECORDED it (domain.EditRegion,
	// ADR 0052): the removed and inserted lines of each changed region with up to three unchanged
	// lines of context each side, and the line numbers they sit on in the before and the after
	// file. The domain type is carried as it stands rather than mirrored here, because the facts
	// are the tool's and a second shape of them would be a second thing to keep true.
	//
	// It is the structured half of the same body Details already holds in rows: the stacked
	// reading is built from these regions (stackedDiffLines) and the split reading composes them
	// into two panes at paint time, where the width is known. A view with no regions — a tool
	// that recorded none, a result that carried no summary — keeps whatever body it was presented
	// with, which is the argument-derived -/+ list this block always showed.
	//
	// The lines are tool-recorded FILE CONTENT, so they are display text like every other string
	// here and are escape-stripped with them (sanitize).
	Regions []domain.EditRegion

	// RegionFiles is the file each of Regions was cut from, one entry per region and ALIGNED
	// index-for-index with it. It is how a diff that spans SEVERAL files is carried — a printed
	// git diff does (ratified call 10) — and it annotates the regions rather than re-grouping
	// them: the change itself keeps one home, and either reading regroups the two back into the
	// file sections it paints (regionFileSections, diffFileRegions).
	//
	// It is empty for every source that names no file: the three edit tools, whose block's own row
	// already names the file they wrote, and view_diff's whole-file body. Such a body paints
	// exactly as it did before file sections existed — no header row over it.
	//
	// The names are tool output like the region lines beside them, and are escape-stripped with
	// them (sanitize).
	RegionFiles []string

	// stat is the other reading of a PROMOTED outcome: the presenter's own typed phrase for the
	// fact the quoted Summary spells out in the tool's words ("1 line"), carried beside it so the
	// painter can choose between the two by measure (promotable, demoted). It is empty on every
	// view whose summary the block worded itself — those have only one reading — and on a promotion
	// the guard must never take back (promotedOutput).
	//
	// It travels the sanitize and shortening seams with the Summary it stands in for, because it
	// reaches the very same slot — and a value with arithmetic in it passes through both untouched,
	// having no path to spell and no ESC byte to lose (statValue.stripped, statValue.shortened).
	stat statValue

	// argStat is the slot phrase this call's own ARGUMENTS word (toolPresenter.argStat): a write's
	// line count, an edit's diffstat. It is settled when the call is presented — the facts are in
	// the request — and kept so the arriving result cannot quietly change what the slot says: the
	// prose layers write a result sentence into the summary, and this is re-applied over it.
	//
	// It holds the ANSWER rather than the arguments it was read from, which is what keeps a
	// write_file's whole file content out of the view for the life of the session.
	argStat statValue

	name string

	// agentName is the short name a delegation was given (the sub_agent call's optional `name`
	// argument, normalised the way the tool normalises it: trimmed first line, empty when absent).
	// It is the same text the Target already leads with on a named delegation, kept apart from it
	// because the two answer different questions: Target is the header's text whatever filled it —
	// the task's first line on an unnamed call — while this says whether a NAME was given at all,
	// which is what the live status line looks up to word a phrase as "<name> · reading main.go"
	// rather than "sub-agent · reading main.go".
	//
	// It is display text like the fields above it and is escape-stripped with them (sanitize), but
	// deliberately NOT on the wire (wireToolView): the status line is live-only, and the persisted
	// header display already rides Target.
	agentName string

	// task is the delegated prompt in full — the sub_agent call's `task` argument verbatim, newlines
	// and all — kept on the head of the run it opens. The header's Target holds only that text's
	// first line, clipped to the branch's budget (subAgentTarget), and an expanded run opens its body
	// with the whole prompt rendered as markdown, so the two answer different questions: what the
	// collapsed row says the delegation is, and what the delegation was actually ASKED.
	//
	// It is retained rather than read back off args because args is dropped for every presenter with
	// no result hook, sub_agent among them (presentToolCall) — and dropping it is the rule that keeps
	// a write_file's whole file content out of the view. Retaining this one string keeps that rule
	// intact: a delegation's prompt is bounded by what a model wrote to open a run, and it is the one
	// argument the block goes on to paint.
	//
	// It is display text and travels the sanitize seam with the fields above it, but not the
	// shortening one: the prompt is text the block QUOTES, so an absolute path inside it is the
	// model's own wording and respelling it would show a prompt the child never received
	// (shortenPaths). Unlike agentName it IS on the wire (wireToolView.Task) — the body a resumed
	// session paints is built from it, so a record that lost it would come back a different block.
	task string

	// finished says this view's row wears the done ✓ after its name (design call 6 of
	// docs/plans/"2026-08-11 - 01"; leaderRow). It is a PAINT-TIME reading and never a presented
	// fact: whether a delegation came off is on its entry (entry.done) and in the verdict its own
	// summary words, and the painters that already copy a view to say what a collapsed delegation
	// shows are the ones that set it (collapsedSubAgentView, renderSubAgentGroup). Keeping it here
	// rather than threading a flag through renderGroupMember is what lets ONE reading of the mark
	// serve the collapsed row, the expanded ┌─┶ header and the lone run alike — a second wording
	// would part company with this one the first time either moved.
	//
	// It is deliberately not on the wire (wireToolView) and not display text: nothing sanitises a
	// bool, and a replayed record re-derives the mark from the entry it decoded.
	finished bool

	// solo marks a call that must never be folded into a grouped block, however well it matches its
	// neighbours (groupable, render.go). Grouping's own rule is about the SHAPE of a call — a target
	// to lead a member's leader row — and says nothing about what the block MEANS; solo is where a
	// presenter states that meaning, for a record whose block is a thing in its own right rather than
	// one of a batch. Two calls say it today: the answered ask_user question, whose block keeps the
	// permanent record of an exchange (askUserAnswerRecord) and reads as a card, not as a row in a
	// list of questions; and the sub_agent call, whose block heads a whole run and frames the work
	// beneath it (presentToolCall) — a fact about the call, so it holds from the moment the call is
	// built, including for a delegation that never produced a run to frame.
	//
	// It is the presenter's word and not the painter's guess, which is the point: the body a record
	// carries used to keep it out of a group as a side effect of the old shape rule, and a side effect
	// is exactly what a later change to that rule takes away.
	solo bool

	// args is the call's parsed arguments, kept for the one presenter shape that needs the REQUEST
	// back when the result lands (toolPresenter.outcome). It is retained only for such a presenter
	// and dropped at presentation time for every other, so a write_file's whole file content is not
	// held for the life of the session behind a field one tool reads.
	//
	// It is display state's raw material and never display text: sanitize does not reach it, because
	// nothing here is painted — every line the hook builds from it is a body line, and the seam
	// strips those on the way out (enrichWithResult defers finishDisplay). It is deliberately not on
	// the wire either (wireToolView): what a saved transcript keeps is the finished record, and the
	// only call that could miss its arguments on replay is one still awaiting its answer — which
	// falls back to the summary-only block it had before this existed.
	args map[string]any
}

// headsRun reports whether this card is a delegation's — the sub_agent call whose block heads a run
// ([entry.headsRun] is the same question asked of the entry that carries the card). It matches the
// RETAINED tool name rather than the "Sub-Agent" label for [subAgentSpan]'s reason: a relabelling
// must not switch the rule off, and a third-party tool that happens to share the label must not
// switch it on. That the answer is knowable from the name alone is what lets a decoded record
// re-derive its own solo mark rather than trust one the wire may predate (fromWireToolView).
func (tv toolView) headsRun() bool {
	return tv.name == subAgentToolName
}

// toolOutcome is what a prose extractor returns: the one-line Summary that fills the branch
// row's outcome slot, and the Details body laid out beneath it. Either half may be empty
// — a fixed result sentence is summary-only ("HTTP 200 OK") and multi-line free-form output is
// body-only (every one of its lines). A tool whose result carries a domain.ToolSummary
// does not come through here at all: its stat hook words the branch line and the presenter's
// body renderer fills the half beneath it. An edit's body — and a write's —
// comes from neither half of the result: it was derived from the call's arguments before it
// (argBody).
//
// The Summary is a branchSummary rather than a bare line because the extractor is the one thing
// that knows whose words it just wrote — its own sentence, or the tool's output promoted onto the
// branch — and the view it hands the outcome to inherits that fact with the text.
//
// Solo is the third thing an extractor may say, and it is about the BLOCK rather than about either
// half: this record must stand on its own and never fold into a group (toolView.solo). It travels
// with the outcome because the fact only becomes true when the result lands — an ask_user question
// still on screen is an ordinary card and groups like one.
type toolOutcome struct {
	Summary branchSummary
	Details []detailLine
	Solo    bool

	// Stat is the presenter's own typed reading of the same fact the promoted Summary carries
	// ("1 line") — what the outcome slot says INSTEAD when the row is too narrow to hold the
	// promoted line without eating the target (the promote-guard, design call 5). It is the
	// second half of a promotion and blank on every other outcome: a summary the block WORDED is
	// already the typed value, so there is nothing to fall back to.
	Stat statValue
}

// summaryOnly is the outcome of a tool whose whole result is one plain line in the PRESENTER's
// wording — a fixed sentence, a phrase it composed: it fills the branch row's outcome slot,
// nothing hangs beneath it, and the shortening seam spells any path it names relative to the
// workspace.
func summaryOnly(text string) toolOutcome {
	return toolOutcome{Summary: namedSummary(detailLine{Text: text})}
}

// promotedOutput is the outcome of a tool whose one-line result is text it did not WORD — a
// command's whole output when that came to one line (outputDetail), the answer a human typed into
// an ask_user question (quotedFirstLineDetail): the line is promoted into the branch row's outcome
// slot. Promotion moves where the text sits and changes nothing about
// whose text it is — it is quoted, so it is marked as such and reaches the screen with the spelling
// it was written with (branchSummary).
//
// Promotion is a CANDIDACY rather than a settled fact, because whether the line may have the slot
// depends on a width this file knows nothing about: a long line on a narrow row would push the
// target off it, and design call 5 holds 15 cells of target back for exactly that. So a promoter
// hands over both readings of its outcome — the line, and stat, its own typed phrase for the same
// fact ("1 line") — and the painter picks by measure (toolView.demoted, guardRefuses in render.go).
//
// A promoter with no such reading passes the blank value and its line is never taken back. The guard cannot
// demote what it has nothing to put in the slot's place, and ask_user is that case for a second
// reason: its body is the RECORD of the exchange (askUserAnswerRecord), which the answer would be
// repeated above rather than folded into.
func promotedOutput(text string, stat statValue) toolOutcome {
	return toolOutcome{Summary: quotedSummary(detailLine{Text: text}), Stat: stat}
}

// presentToolCall builds the header view of a tool call — and, for the tools whose arguments
// already say what the call will change, its body too (argBody: the edit tools' -/+ lines, a
// write's + lines).
// That body is derived here, at presentation time, from arguments the model has already sent:
// nothing is asked of the tool, nothing is added to a result, and the block shows the change
// before the result even lands. A known tool gets its friendly
// label, its active verb, and a target pulled from the arguments; an unknown tool falls back
// to its raw name (styled like any other label) with its arguments as LABELLED detail lines
// (argumentDetails — the same rendering the approval prompt reads a decision off, so the two
// surfaces spell one call one way), so a not-yet-registered tool still renders and a malformed
// argument is shown verbatim rather than dropped (the approval flow is a security surface — the
// model's request is never hidden).
// The verb mirrors that fallback: an unregistered tool is "running <raw name>", which stays a
// truthful sentence fragment for a dynamic MCP tool nobody has a verb for.
// Everything the header states traces back to the model's own JSON arguments — the target on every
// registered tool, and the raw name behind an unknown tool's label, verb and labelled body —
// so both exits leave through finishDisplay, which escape-strips the view and spells the paths it
// NAMES relative to the workspace root ws names.
//
// resolved is the ONE fact here the model did not write: the path this call's argument really
// points at, sent by the engine only when it differs from the argument itself
// (domain.ToolCallEvent.ResolvedPath) and empty on every ordinary call. It joins the target rather
// than replacing it — the argument stays on the screen as the model wrote it, with where it lands
// beside it — because a surface that silently swapped one for the other would be answering a
// question the reader did not ask and hiding the one they did.
func presentToolCall(call domain.ToolCall, resolved string, ws workspaceRoot) toolView {
	p, ok := toolRegistry[call.Tool]
	if !ok {
		tv := toolView{
			Label:   call.Tool,
			Verb:    "running " + call.Tool,
			name:    call.Tool,
			Details: newToolBody(argumentDetails(call.Arguments)),
		}
		tv.finishDisplay(ws)
		return tv
	}
	tv := toolView{Label: p.label, Verb: p.verb, name: call.Tool}
	// A sub-agent call is a block in its own right and never a row in a list: it HEADS a run, and
	// what hangs beneath it is a whole delegation (renderSubAgentRun). The painter's own rule for
	// that shape keys on the run's span, which is not the same question — a delegation refused at
	// the depth bound (executeRefuse, internal/agent) leaves a head with no span at all, and the
	// shape rule alone would read two refusals in a row as one "Sub-Agent (2)". So the fact is
	// stated here, where the call is recognised, against the same constant the span rule matches on
	// (subAgentToolName) so the two cannot drift apart.
	tv.solo = call.Tool == subAgentToolName
	args := parseArgs(call.Arguments)
	// A delegation's name is recorded beside the Target rather than read back out of it: the header
	// text is the same string on a named call, but only this says a name was GIVEN, which is the
	// question the live status line asks (toolView.agentName). The Target is settled by the
	// registry's own extractor either way (subAgentTarget), so the two cannot disagree.
	// The delegated prompt is retained whole for the same reason, one step further on: the run's
	// expanded body opens with it (toolView.task), and args is about to be dropped for a presenter
	// with no result hook — which sub_agent is.
	if call.Tool == subAgentToolName {
		tv.agentName = subAgentName(args)
		tv.task = stringArg("task")(args)
	}
	if p.target != nil {
		tv.Target = p.target(args)
	}
	// Where the call's path really points, when that is not where the argument said (resolved,
	// from the engine). It rides the TARGET rather than the body because a targeted block hides
	// its body whole while collapsed (collapsedCall) — a disclosure a reader has to open the
	// block to find is one they will not read — and the branch row is the one row every shape of
	// this block paints. On a row too narrow for both, the clip ends it in " …" exactly as it
	// ends an over-long path, which is the same promise the row already makes about its target.
	if note := resolvedPathNote(resolved); note != "" && tv.Target != "" {
		tv.Target += " " + note
	}
	if p.argBody != nil {
		tv.Details = newToolBody(p.argBody(args))
	}
	// The request half of the outcome slot, settled now: what a write puts in a file and what an
	// edit changes are stated in the call, so the block says them from the moment it is announced
	// rather than waiting for a result to repeat them back (toolPresenter.argStat).
	if p.argStat != nil {
		if s, ok := p.argStat(args); ok {
			tv.argStat = s
			tv.Summary = typedSummary(s)
		}
	}
	if p.outcome != nil {
		tv.args = args // the request this presenter's result hook reads back (toolView.args)
	}
	tv.finishDisplay(ws)
	return tv
}

// finishDisplay is the seam every freshly built or freshly enriched view leaves through, and it is
// two acts in one order: escape-strip every display field (sanitize), then shorten the workspace
// root out of the paths the view NAMES (shortenPaths — the target and the summary; a body is text
// the block quotes and keeps its own spelling). The order is load-bearing — an ESC byte buried
// inside a path splits the root's spelling in two, so a shortener running first would not
// recognise the mention and would leave the absolute path on screen, which is a repo that controls
// a filename opting itself out of the rule.
//
// The two exits of presentToolCall and the one of enrichWithResult go through here rather than
// calling either half directly, so a later tool or a later branch cannot pick up one discipline and
// miss the other.
func (tv *toolView) finishDisplay(ws workspaceRoot) {
	tv.sanitize()
	tv.shortenPaths(ws)
}

// shortenPaths spells the paths the view NAMES relative to the workspace — the target it acts on,
// and the one-line summary of its outcome when that summary is the block's OWN wording
// (workspaceRoot.shorten). It is presentation and nothing else: the model's arguments and the
// tool's own result are untouched, so the agent's view of a path never changes with the
// transcript's spelling of it.
//
// What the block QUOTES is left alone, and that boundary is the rule's whole point. A body is
// quoted text rather than a path the block names — a diff's hunk lines, an edit's replacement
// string, an unregistered tool's verbatim arguments — so an absolute in-workspace path occurring
// inside it is file CONTENT, and shortening it would show the human approving a write a spelling
// the file will not contain. The SUMMARY is quoted in that same sense whenever the line was
// promoted into the slot as it stands rather than worded there (promotedOutput): `cat` printing
// one path names nothing — it prints a file's contents, which happen to fit on the branch — and an
// ask_user answer names nothing either, being what the human typed.
//
// Nothing here reads the text to tell the two apart: a line of output can look exactly like a path,
// so the distinction is structural — it is what the SLOT was filled with, marked by the presenter
// that filled it (branchSummary.quoted) and read here. Should a presenter ever build a BODY line
// that genuinely is a path (a listed file, a search hit's file name), it must say so the same way —
// a mark on detailLine, set by the producer, which is the only place that knows.
//
// Label and Verb are left out for their own reason. They name the TOOL — a friendly label, or an
// unregistered tool's raw id behind "running <name>" — and a tool id is not a path, so shortening
// them could only ever mangle a name that happened to read like one.
func (tv *toolView) shortenPaths(ws workspaceRoot) {
	if ws.root == "" {
		return
	}
	tv.Target = ws.shorten(tv.Target)
	if !tv.Summary.quoted {
		tv.Summary.Text = ws.shorten(tv.Summary.Text)
	}
	// The stat is the block's OWN wording for the same outcome and may take the slot at paint time
	// (toolView.demoted), by which point this seam has long run — so it is spelled here, with the
	// summary it stands in for, rather than left to reach the screen as the only unshortened text
	// the block ever wrote.
	tv.stat = tv.stat.shortened(ws)
}

// sanitize escape-strips every DISPLAY field of the view — label, verb, target, a delegation's name
// and its retained prompt (toolView.task), the one-line
// summary, the typed stat standing by to replace it (toolView.stat), each detail line, and the Edit
// regions a diff body paints from together with the file names over them — so no
// ESC byte from a tool call or its result can reach the
// terminal (stripEscapes). It is the tool card's security seam, run on the way out of
// presentToolCall and of enrichWithResult (finishDisplay) rather than left to the two dozen target
// and detail extractors above to remember one at a time.
//
// The threat is concrete and needs no user action: a malicious repo owns the first line of any file
// the model reads and the first line of any command's output (firstLineDetail, outputDetail), a
// hostile model owns every argument a target is pulled from, and the frame is painted through
// ultraviolet's cell buffer, which deliberately HONOURS OSC 8 hyperlinks and never resets the link
// state across cells or newlines — one unterminated opener turns the rest of the frame into a
// clickable link to the attacker's URL, which is aimed squarely at ADR 0019's rung 0 ("cmd+click
// the path we print").
//
// name is deliberately left alone: it is the registry lookup key enrichWithResult reads, never
// rendered — Label carries the displayed copy of it, and Label is stripped.
//
// How a body PAINTS is not this seam's business and never was a rule a body path had to remember:
// a line's Kind is set by its producer and the strip below rewrites only text — so a sanitized body
// shows exactly the colours it did before. The summary's mark is the same: whose words a line is
// does not change when an ESC byte leaves it.
func (tv *toolView) sanitize() {
	tv.Label = stripEscapes(tv.Label)
	tv.Verb = stripEscapes(tv.Verb)
	tv.Target = stripEscapes(tv.Target)
	tv.agentName = stripEscapes(tv.agentName)
	tv.task = stripEscapes(tv.task)
	tv.Summary.Text = stripEscapes(tv.Summary.Text)
	tv.stat = tv.stat.stripped()
	tv.Details.stripEscapes()
	tv.Regions = strippedRegions(tv.Regions)
	tv.RegionFiles = stripEscapesAll(tv.RegionFiles)
}

// strippedRegions is the strip run over the lines a set of Edit regions carries — the region half
// of the seam above, and the reason it exists at all: a region holds tool-recorded FILE CONTENT,
// which a malicious repo owns every byte of, and both readings of the body paint straight from it
// (stackedDiffLines, and the split composer at paint time).
//
// It COPIES rather than rewriting in place, unlike the body's own strip: the slices arrive on the
// tool's result (domain.EditRegions) and are shared with the value the engine holds, so writing
// through them would have a display seam rewrite the engine's own data. The copy is bounded — a
// region is a handful of lines — and the whole region is copied by value first, so a field added to
// domain.EditRegion travels rather than being silently dropped here.
func strippedRegions(regions []domain.EditRegion) []domain.EditRegion {
	if len(regions) == 0 {
		return regions
	}
	out := make([]domain.EditRegion, len(regions))
	for i, region := range regions {
		region.Leading = stripEscapesAll(region.Leading)
		region.Removed = stripEscapesAll(region.Removed)
		region.Inserted = stripEscapesAll(region.Inserted)
		region.Trailing = stripEscapesAll(region.Trailing)
		out[i] = region
	}
	return out
}

// promotable says whether the outcome slot holds a PROMOTED line the guard may still take back: a
// summary the block quotes rather than words, with a typed stat to stand in its place. Both halves
// are required. The block's own wording is not a promotion — there is nothing to demote it TO — and
// a promotion whose promoter offered no stat is one the guard was told to leave alone
// (promotedOutput).
func (tv toolView) promotable() bool {
	return !tv.stat.blank() && tv.Summary.quoted && tv.Summary.Text != ""
}

// demoted is the view as it reads once the promote-guard refuses the promotion (design call 5): the
// quoted line leaves the outcome slot, the presenter's typed stat takes its place, and the line
// lands where it would have been had the tool printed two of them — the first line of the body.
// Nothing is lost by the swap, which is the guard's whole licence to make it: the text is one click
// away in a block that now has something to reveal, and the slot still says what happened.
//
// It is the pure half of the guard. The MEASURE that calls it is the painter's (guardRefuses,
// render.go), because whether a row keeps 15 cells of target is a question about a width, and this
// file knows nothing about widths.
//
// The body is rebuilt rather than prepended in place: a view is a value the entry hands out copies
// of, and writing through the slice it shares would put the line in the entry's own body as well —
// once per repaint. Demoting is idempotent for the same reason it is safe: the stat leaves with the
// promotion, so a view already demoted is no longer promotable.
func (tv toolView) demoted() toolView {
	if !tv.promotable() {
		return tv
	}
	lines := make([]detailLine, 0, tv.Details.len()+1)
	lines = append(lines, tv.Summary.detailLine)
	tv.Details = newToolBody(append(lines, tv.Details.all()...))
	tv.Summary = typedSummary(tv.stat)
	tv.stat = statValue{}
	return tv
}

// errorNoun is the word a TYPE ROW counts its run's failures with — "1 error", "3 errors" — and the
// only wording apogee reserves in the outcome slot beyond the three a single call may carry
// (errorSummaryPrefix and the two bare verdicts). Reserving it is what lets the failure test stay
// one reading of the TEXT for a member row and an aggregate alike (failedSummary, render.go).
const errorNoun = "error"

// runAggregate is the outcome slot of a whole RUN — what a super-group's type row says about the
// calls folded behind it (design call 10, docs/layout/tool-layout.md, "Fold states and
// interaction"). Three answers, in this order:
//
//   - a run of ONE is its own member's summary, verbatim, whatever kind that is. Summing one call is
//     that call, so nothing has to be invented: a lone failure keeps its "error: …" sentence instead
//     of being counted as "1 error", and a promoted line stays the quoted line it is.
//   - any FAILED member and the row counts them, "N errors", and that count is the row's OWN
//     verdict: the summary it words goes through namedSummary like any other sentence in the
//     failure vocabulary, so the type row carries failed and needs nothing read back out of it.
//   - otherwise the run's natural sum, where the members' typed stats sum at all (sumStats), and
//     nothing when they do not: an empty slot lets the dots run to the ▶, which is the spec's "else
//     blank".
//
// It is pure and lipgloss-free like everything else in this file: it words the slot, and the tone
// that wording earns is the painter's (summaryStyle).
func runAggregate(views []toolView) branchSummary {
	if len(views) == 0 {
		return branchSummary{}
	}
	if len(views) == 1 {
		return views[0].Summary
	}
	if n := failedCalls(views); n > 0 {
		return namedSummary(detailLine{Text: plural(n, errorNoun)})
	}
	if sum, ok := sumStats(views); ok {
		return typedSummary(sum)
	}
	return branchSummary{}
}

// failedCalls counts the members of a run whose outcome says the call failed. It reads each
// member's own verdict ([branchSummary.failed]) and never its wording: the question was answered
// where the summary was worded, and asking the text again here would count a line the TOOL printed
// as a failure the call never had (F-29).
func failedCalls(views []toolView) int {
	n := 0
	for _, tv := range views {
		if tv.Summary.failed {
			n++
		}
	}
	return n
}

// sumStats adds a run's typed stats up into one value, and reports whether they add up at all. Two
// shapes sum, and they are the two shapes the registry's stat hooks actually produce: a diffstat
// (statDiffed) and a counted noun (statCounted). Everything else — "exit 0", "PASS", "clean",
// "done", a short hash — has no arithmetic and comes back false, which the type row prints as an
// empty slot.
//
// It reads the members' VALUES and never their wording (branchSummary.stat), which is also what
// keeps a promoted line out of the arithmetic: a line the tool printed carries no stat, and one that
// happened to read "3 errors" or "12 lines" would otherwise be added into a sum it was never part
// of. A call still in flight carries none either, so a run with an open member does not sum.
func sumStats(views []toolView) (statValue, bool) {
	if len(views) == 0 {
		return statValue{}, false
	}
	total := views[0].Summary.stat
	if !total.sums() {
		return statValue{}, false
	}
	for _, tv := range views[1:] {
		sum, ok := total.add(tv.Summary.stat)
		if !ok {
			return statValue{}, false
		}
		total = sum
	}
	return total, true
}

// countPhrase splits a counted noun into its number and its word — "12 lines" into 12 and "lines".
// It is deliberately total and deliberately strict: a phrase that is not exactly an integer, one
// space and one word is not a count, so "exit 0", "PASS", "a1b2c3d" and a promoted sentence all
// answer false and reach the slot untouched.
//
// The stats themselves are typed and are never read back through here (sumStats). What is left is
// the one reading that has only ever had the TEXT to go on: the failure test, which asks whether an
// outcome — a member's or a whole run's — words itself as a count of errors (failedSummary).
func countPhrase(text string) (n int, noun string, ok bool) {
	num, rest, found := strings.Cut(text, " ")
	if !found || rest == "" || strings.ContainsRune(rest, ' ') {
		return 0, "", false
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return 0, "", false
	}
	return n, rest, true
}

// enrichWithResult folds a tool's result into the view, in four layers. An error result
// (the tool flagged it IsError — a normal in-band outcome the model reacts to) is worded by
// absorbFailure, which fills the one-line summary and — for a tool that reads its own failure off
// its output — the body under it, so an errored call still groups with its neighbours. A result
// carrying a typed domain.ToolSummary skips the prose layers, its body coming from the presenter's
// `body` hook and its slot from its stat hook. Everything else falls to prose, through one of
// two extractor shapes: a presenter's `outcome` hook reads the retained REQUEST beside the result
// (ask_user alone — its block records the question the answer replies to), and a plain `detail`
// extractor reads the result alone. An unknown tool's result is shown raw as body lines, so
// nothing is ever silently dropped.
//
// A diff-bodied result then hands its body over to the change's own rows: the Edit regions the
// tool RECORDED while it applied that change, or — for a tool that applies nothing and merely
// PRINTS a diff — the regions this package recovers from that output (toolPresenter.regions).
// Neither reaches every result, and one that yields no regions at all keeps the body the layers
// above gave it.
//
// The first two layers WORD the summary themselves — an "error: …" line, a typed phrase — so both
// mark it as the block's own (namedSummary). Both prose layers hand their outcome's mark straight
// through, because an extractor is the one thing that may promote the tool's output onto the branch
// instead of wording anything (outputDetail, quotedFirstLineDetail), and only it knows which it did.
//
// Every one of those layers words itself from result.Content, which is tool output and therefore
// repo-controlled, so the finishDisplay seam is deferred rather than repeated: it runs on whichever
// branch returns, and on any branch a later tool adds. Re-finishing the fields the call already
// left through costs one pass and is exactly idempotent — a stripped line has no ESC left to strip
// and a shortened path no longer spells the root — which is what lets the seam stay one call.
func (tv *toolView) enrichWithResult(result domain.ToolResult, ws workspaceRoot) {
	defer tv.finishDisplay(ws)
	if result.IsError {
		tv.absorbFailure(result.Content)
		return
	}
	p, known := toolRegistry[tv.name]
	tv.absorbProse(p, known, result)
	if regions, ok := recordedRegions(result); ok {
		tv.showRegions(regions.Regions)
	} else if known && p.regions != nil {
		tv.showFileRegions(p.regions(result))
	}
	// The request-derived stat is re-applied because the prose layers may have written over it
	// with a result sentence; it is the same phrase the call was presented with, so a block does
	// not change what its slot says when its result lands.
	if !tv.argStat.blank() {
		tv.applyStat(tv.argStat, true)
	}
	if known && p.stat != nil {
		tv.applyStat(p.stat(result))
	}
}

// absorbFailure fills the halves from a FAILED result. The summary is the "error: …" line the
// painter reads as the block's red (errorSummaryPrefix), worded by the tool's own failure hook
// where it has one and by the result's first line everywhere else — for a tool that fails in prose
// that first line IS the error message, which is why it stays the floor.
//
// A hook that worded the slot also hands back the output left once it has read the failure off it,
// and that output lays out beneath the branch. A failed subprocess call therefore reads as its
// clean twin does — the exit code in the slot over the lines the command printed — instead of
// spending the slot on whichever line the output happened to open with.
func (tv *toolView) absorbFailure(content string) {
	if p, known := toolRegistry[tv.name]; known && p.failure != nil {
		if word, output, ok := p.failure(content); ok {
			tv.Summary = namedSummary(detailLine{Text: errorSummaryPrefix + word})
			tv.Details = tv.Details.with(outputBody(output))
			return
		}
	}
	tv.Summary = namedSummary(detailLine{Text: errorSummaryPrefix + firstLine(content)})
}

// absorbProse fills the two halves from the result's PROSE — the layers that predate the ratified
// per-tool table, and still the only thing a tool with no typed outcome has. A result carrying a
// domain.ToolSummary skips them: what its slot says is its stat hook's word, and all such a result
// leaves here is the body its presenter reads off it (view_diff's diff, read_file's located lines).
func (tv *toolView) absorbProse(p toolPresenter, known bool, result domain.ToolResult) {
	if result.Summary != nil {
		if known && p.body != nil {
			tv.Details = tv.Details.with(p.body(result))
		}
		return
	}
	if known && p.outcome != nil {
		out := p.outcome(tv.args, result.Content)
		tv.Summary = out.Summary
		tv.stat = out.Stat
		tv.Details = tv.Details.with(out.Details)
		tv.solo = tv.solo || out.Solo
		return
	}
	if known && p.detail != nil {
		out := p.detail(result.Content)
		tv.Summary = out.Summary
		tv.stat = out.Stat
		tv.Details = tv.Details.with(out.Details)
		return
	}
	// Unknown (or summary-less) tool: surface the raw result so it is never silently dropped.
	raw := splitLines(strings.TrimRight(result.Content, "\n"))
	lines := make([]detailLine, 0, len(raw))
	for _, ln := range raw {
		lines = append(lines, detailLine{Text: ln})
	}
	tv.Details = tv.Details.with(lines)
}

// recordedRegions is the Edit regions a tool RECORDED on its result, and whether it recorded any
// (domain.EditRegions, attached at apply time by the three edit tools). It is the one reading of
// that summary — the stat hook and the enrichment path both ask here — so what counts as "this
// result has regions" cannot come to mean two things.
//
// A summary with an empty region list answers false, exactly as no summary at all does: an edit
// whose change could not be cut into regions (an over-budget pair, internal/tools) has nothing to
// paint and nothing to count, and the block keeps the argument-derived body and slot it was
// presented with (ratified call 9).
func recordedRegions(res domain.ToolResult) (domain.EditRegions, bool) {
	v, ok := res.Summary.(domain.EditRegions)
	if !ok || len(v.Regions) == 0 {
		return domain.EditRegions{}, false
	}
	return v, true
}

// showRegions puts a set of Edit regions on the view and REPLACES its body with the rows they
// render as (stackedDiffLines). It is the half both sources of regions share — the tool's own
// record and this package's recovery — so which of the two a diff block got cannot change what its
// body reads like.
//
// The regions are kept BESIDE those rows rather than only as rows: the split reading composes them
// into two panes at paint time, where the width is known (splitDiffRows), and rows cannot be
// un-stacked back into that.
//
// The body it puts up is REPLACED rather than grown, which matters for the recorded half: what a
// call was presented with is the change the model ASKED for, read off its own arguments before any
// result existed (argBody), and what a record carries is the change that LANDED, with the line
// numbers and the context the arguments never held. Keeping both would show the same edit twice.
// The diffstat over those rows is not this function's business: the tool's own stat hook counts it
// off the very same regions (editRegionsStat), so the rows and the number over them stay one
// reading without either half telling the other.
//
// An empty set changes nothing at all, which is the fallback both sources fall to and the one
// answer they must give alike: an edit whose pair was over the diff budget, a printed diff whose
// output carried no tags to walk. Such a block keeps the body it already had (ratified call 9).
//
// It is the ONE-SECTION case of showFileRegions below, where the work happens: a set of regions
// with no file name over it is exactly what an edit tool records and what a whole-file view_diff
// yields, and a diff that names its files is the same thing several times over.
func (tv *toolView) showRegions(regions []domain.EditRegion) {
	tv.showFileRegions([]diffFileRegions{{Regions: regions}})
}

// diffFileRegions is one FILE's part of a printed diff: the path the tool named that file by, and
// the Edit regions cut from its hunks. It is what the recovery hook answers in
// (toolPresenter.regions) and what both readings of such a body are painted from, section by
// section — a muted header row naming the file, then that file's regions beneath it (ratified
// call 10).
//
// The path is empty when the output names no file: view_diff prints one file's diff and never says
// which, and an edit tool's recorded regions arrive here as one nameless section too. A nameless
// section paints no header, which is what makes a printed diff of a single file read exactly like
// the change an edit block shows.
type diffFileRegions struct {
	File    string
	Regions []domain.EditRegion
}

// showFileRegions is the ONE writer of a view's regions and of the body they render as. It flattens
// the sections it is given onto the view — the regions themselves, the file name each was cut from
// beside them (toolView.RegionFiles) — and REPLACES the body with the stacked rows of every
// section in turn, each behind its file's header row where the section names one.
//
// The stacked rows are built per SECTION rather than over the flattened whole, which is what keeps
// two files' numbers from being read as one file's: each file sizes its own number gutter, and the
// elision rule between two regions is only ever asked within a file (stackedDiffLines).
//
// A section with no regions is skipped and a set with no regions at all changes nothing, which is
// the fallback every source shares (showRegions).
func (tv *toolView) showFileRegions(sections []diffFileRegions) {
	var (
		regions []domain.EditRegion
		files   []string
		named   bool
		body    []detailLine
	)
	for _, section := range sections {
		if len(section.Regions) == 0 {
			continue
		}
		if section.File != "" {
			named = true
			body = append(body, detailLine{Text: clipDetail(section.File)})
		}
		body = append(body, stackedDiffLines(section.Regions)...)
		for range section.Regions {
			files = append(files, section.File)
		}
		regions = append(regions, section.Regions...)
	}
	if len(regions) == 0 {
		return
	}
	tv.Regions, tv.Details = regions, newToolBody(body)
	if named {
		tv.RegionFiles = files
	}
}

// regionFileSections regroups a view's flattened regions into the file sections they were cut from,
// which is what the SPLIT reading paints them by (splitBody): the stacked rows were composed when
// the regions landed, but the panes are composed at paint time, where the width is known, and they
// need the same section boundaries.
//
// A view whose names are missing or do not line up with its regions is one nameless section — the
// three edit tools and view_diff leave the names empty, and a decoded record that lost them (the
// names are display state, not the change) must still paint the change rather than nothing.
func regionFileSections(regions []domain.EditRegion, files []string) []diffFileRegions {
	if len(regions) == 0 {
		return nil
	}
	if len(files) != len(regions) {
		return []diffFileRegions{{Regions: regions}}
	}
	sections := make([]diffFileRegions, 0, 1)
	for i, region := range regions {
		if i == 0 || files[i] != files[i-1] {
			sections = append(sections, diffFileRegions{File: files[i]})
		}
		last := &sections[len(sections)-1]
		last.Regions = append(last.Regions, region)
	}
	return sections
}

// applyStat settles the right-hand outcome slot from a stat hook's answer (toolPresenter.stat).
// Three outcomes, and each is a different thing the table can say about a tool:
//
//   - ok false — the tool had nothing typed to say about THIS result (no summary on it, no
//     recognisable header in its output). Whatever the prose layers put in the slot stays, which
//     is the degraded floor: a tool's own first line, never a blank where a fact used to be.
//   - the slot already holds a PROMOTED line — the tool's own one-line output, quoted. It keeps
//     the slot, because a line the tool printed says more than a phrase about it; the stat becomes
//     the fallback the promote-guard swaps in on a row too narrow for both (design call 5).
//   - otherwise the stat IS the slot, the blank value included: the table's `—` is a blank slot with
//     the dots running to the `▶`, and it is stated rather than left over from a prose sentence.
func (tv *toolView) applyStat(v statValue, ok bool) {
	if !ok {
		return
	}
	if tv.Summary.quoted && tv.Summary.Text != "" {
		tv.stat = v
		return
	}
	tv.Summary = typedSummary(v)
	tv.stat = statValue{}
}
