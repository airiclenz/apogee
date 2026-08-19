package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Tool presentation (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// This file turns a tool call+result into a compact, human-facing view: a friendly label for
// the header line (✦ Read), the target that leads the branch beneath it, and the one-line
// summary the branch row seats in its right-aligned outcome slot, a dotted leader running
// between the two (┕ main.go ⋯⋯⋯ 154 lines). It is pure — no lipgloss,
// no I/O — so it is trivially table-testable (TestPresentToolCall); render.go owns the styling
// and the block shape.
//
// What lives here is PRESENTATION vocabulary: labels, verbs, targets, and the wording of the
// one-line outcome. What does NOT live here is the outcome's facts. A tool that computed
// something worth showing reports it as a typed domain.ToolSummary beside the prose Content
// (internal/tools), and that tool's own stat hook below words the value. The view prefers not to
// parse a result string to find out what a tool did: prose written for the MODEL is not an
// interface, and re-deriving facts from it meant a wording change in internal/tools silently
// degraded a card with no compiler nudge and no failing test in the package that changed. Where
// the ratified table asks for a fact no typed summary carries and design call 14 forbids growing
// one, a stat hook does read a header the tool formats deliberately — anchored, and total, so an
// unrecognised shape falls back to that tool's own first line rather than to a wrong number.
//
// The two halves stay independent. The wording is the view's own — that several of these
// lines read like the tool's own header today is what makes the change checkable, not a
// contract — and a result carrying NO summary still renders from prose exactly as before:
// the registry's detail extractor quotes a fixed first line, or hands free-form output on as
// the block's body (which the collapsed paint compresses to a first line plus a remainder
// count). Quoting or compressing is rendering; re-deriving a number from a sentence was not.
//
// The label+extractor map is an OPEN, name-keyed registry, not a closed switch: the Phase-3
// tool fan-out (P3.7–P3.11, ~30 tools, ADR 0002) adds one entry per tool (terminal→"Terminal",
// git→"Git Status", find_replace→"Replace", …) rather than editing a control-flow statement. An
// unknown tool falls back to its raw name and labelled arguments (argumentDetails), so a tool
// with no registry entry still renders legibly.
//
// The same entry also carries the tool's active verb ("reading", "running"), which the live
// status line pairs with the target while the call is in flight — the per-tool knowledge
// stays in this one registry instead of growing a second, parallel switch elsewhere.

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
}

// The wordings that mark an outcome as a FAILURE, whatever produced it: the "error: …" line a
// faulted result is summarised with (enrichWithResult), and the two bare verdicts a call that never
// ran carries. The painter reads them to give the outcome slot its red (summaryStyle, render.go),
// and design call 11 of docs/plans/"2026-08-10 - 04" makes that red the ONLY failure marking — no
// glyph and no header changes colour — so the vocabulary and the mark stay one fact in one place.
const (
	errorSummaryPrefix = "error: "
	deniedSummary      = "denied"
	cancelledSummary   = "cancelled"
)

// namedSummary is a summary in the presenter's OWN words — a typed phrase, or a sentence naming a
// path — so the shortening seam spells that path relative to the workspace.
func namedSummary(line detailLine) branchSummary {
	return branchSummary{detailLine: line}
}

// quotedSummary is a summary carrying text the block QUOTES rather than words of its own — a
// one-line tool output promoted onto the branch — which no seam respells.
func quotedSummary(line detailLine) branchSummary {
	return branchSummary{detailLine: line, quoted: true}
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

	// stat is the other reading of a PROMOTED outcome: the presenter's own typed phrase for the
	// fact the quoted Summary spells out in the tool's words ("1 line"), carried beside it so the
	// painter can choose between the two by measure (promotable, demoted). It is empty on every
	// view whose summary the block worded itself — those have only one reading — and on a promotion
	// the guard must never take back (promotedOutput).
	//
	// It is display text and travels the sanitize and shortening seams with the Summary it stands
	// in for, because it reaches the very same slot.
	stat string

	// argStat is the slot phrase this call's own ARGUMENTS word (toolPresenter.argStat): a write's
	// line count, an edit's diffstat. It is settled when the call is presented — the facts are in
	// the request — and kept so the arriving result cannot quietly change what the slot says: the
	// prose layers write a result sentence into the summary, and this is re-applied over it.
	//
	// It holds the ANSWER rather than the arguments it was read from, which is what keeps a
	// write_file's whole file content out of the view for the life of the session.
	argStat string

	// diffStat is the slot's diffstat as a NUMBER PAIR, kept beside the phrase that spells it out,
	// and hasDiffStat says a typed summary supplied it. Only a tool that reported one sets them
	// (absorbRegions): a slot worded from arguments or from a tool's prose has no typed reading and
	// leaves them zero.
	//
	// They exist so the run aggregate can ADD the members of a run up without reading its own
	// wording back out of them (sumDiffCounts): parseDiffCounts is the inverse of one spelling, and
	// a feature that hands the summer a fact it already holds typed must not become that parser's
	// next input. The parser stays the floor for the producers that have only text.
	diffStat    domain.DiffStat
	hasDiffStat bool

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

	// Stat is the presenter's own typed phrase for the same fact the promoted Summary carries
	// ("1 line") — what the outcome slot says INSTEAD when the row is too narrow to hold the
	// promoted line without eating the target (the promote-guard, design call 5). It is the
	// second half of a promotion and empty on every other outcome: a summary the block WORDED is
	// already the typed phrase, so there is nothing to fall back to.
	Stat string
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
// A promoter with no such phrase passes "" and its line is never taken back. The guard cannot
// demote what it has nothing to put in the slot's place, and ask_user is that case for a second
// reason: its body is the RECORD of the exchange (askUserAnswerRecord), which the answer would be
// repeated above rather than folded into.
func promotedOutput(text, stat string) toolOutcome {
	return toolOutcome{Summary: quotedSummary(detailLine{Text: text}), Stat: stat}
}

// toolPresenter maps a tool name to its friendly label, the active verb naming what the tool
// is doing while it runs, a header extractor that pulls the Target from the call's
// arguments, a prose extractor that turns a summary-less result into a [toolOutcome], and the two
// body renderers — one for a RESULT whose branch line came from its typed summary (the one tool
// that has one), one for a body derived from the call's own ARGUMENTS before any result exists
// (the edit tools). A nil extractor is valid (the tool has no target, no summarisable result, or
// nothing in its request worth showing).
//
// label and verb are two views of the same tool for two places: label titles the finished
// header line ("Read"), verb is the lowercase present participle the live status line
// reads as a sentence fragment ("reading main.go") — never a title.
type toolPresenter struct {
	label  string
	verb   string
	target func(args map[string]any) string

	// failure words a FAILED result's summary — the text after "error: " — for a tool whose output
	// does not open with its error message. The two subprocess tools are what it is for: a command
	// that failed says so in its EXIT CODE, and the first line it printed is as likely to be a
	// listing header as a diagnostic. The bool is "this result is not the shape I read", which
	// leaves the result's own first line as the wording — what every tool without the hook keeps.
	//
	// Its second string is the output left once the failure has been read off it. That lays out as
	// the body beneath the branch, so a failed call shows what it printed exactly as a clean one
	// does — the failed half of the mirror the slot wording completes (design call 4 of
	// docs/plans/"2026-08-13 - 00").
	failure func(content string) (word, output string, ok bool)

	// detail renders a result that carries NO domain.ToolSummary — the degraded floor for a
	// summary-bearing tool (its verbatim first line) and the only path for the tools that
	// report nothing structured (a fixed sentence, free-form output).
	detail func(content string) toolOutcome

	// outcome renders a result the way detail does, but with the call's OWN ARGUMENTS in hand
	// beside the content — the seam for a tool whose outcome is only half a result. It takes
	// precedence over detail (enrichWithResult), so an entry setting both would leave the latter
	// unreachable; the one entry that sets it, ask_user, sets it alone.
	//
	// ask_user is what the shape is for. What the human answered comes back in the result, but the
	// question they answered and the choices they were offered were never in one: those are in the
	// CALL, and a block that records the exchange after the popup is gone needs both halves. Reading
	// them here costs nothing anyone can see — they crossed the wire before the tool ran, so the
	// record is a render-time act and the engine stays wire-silent (ADR 0031).
	outcome func(args map[string]any, content string) toolOutcome

	// stat words the right-hand outcome slot — the `<tool-top-level-details>` column of the
	// ratified table (docs/layout/tool-layout.md) — from the RESULT: the typed domain.ToolSummary
	// the tool reports, or a header it wrote into its own output.
	//
	// The bool is the difference between "this tool has nothing typed to say HERE" and "this
	// tool's slot is deliberately blank". False leaves whatever the prose layers put in the slot
	// untouched — the degraded floor a summary-less result still renders from — while true with
	// an empty string is the table's `—`: the slot prints nothing and the dots run to the `▶`.
	//
	// A stat never takes the slot back from a PROMOTED line: a one-line output that reached the
	// slot as the tool's own words keeps it, and the stat becomes the phrase the promote-guard
	// swaps in when the row is too narrow for both (toolView.stat, design call 5).
	stat func(res domain.ToolResult) (string, bool)

	// argStat words the same slot from the call's OWN ARGUMENTS, at the moment the call is
	// presented — the request half of the table's stat column, and argBody's counterpart: a
	// write's line count and an edit's diffstat are facts about what was ASKED FOR, so the slot
	// can say them before the result lands, without the tool reporting anything and without a
	// byte more crossing the wire (ADR 0031).
	//
	// Its answer is kept on the view (toolView.argStat) rather than re-read later, so the
	// arguments themselves need not be retained for the life of the session: a write's whole file
	// content stays out of memory behind a field that only ever yields one short phrase.
	argStat func(args map[string]any) (string, bool)

	// body renders the lines laid out BENEATH the branch when the result's typed summary supplied
	// the branch line itself. It takes the whole result rather than its prose, because what such
	// a body is read off differs by tool: view_diff renders its Content, read_file the located
	// line numbers its domain.ReadSpan carries. A body derived from what the call ASKED for is
	// argBody's instead.
	body func(res domain.ToolResult) []detailLine

	// argBody renders the body from the call's OWN ARGUMENTS, at the moment the call is
	// presented: before any result exists, and never touching one. The edit tools and write_file
	// set it, because what such a call puts in a file is already in the request — the block can
	// show those lines without the tool reporting them and without a byte more crossing the wire,
	// which is what keeps this display-only (the engine stays wire-silent, ADR 0031: no tool
	// result grows, no token is spent). It reads the same map the target extractor reads, so a
	// call whose arguments are absent or malformed yields no body rather than a guess.
	argBody func(args map[string]any) []detailLine
}

// askUserToolName is the raw tool id whose ANSWERED record stands alone: the block keeps the
// permanent record of an exchange and never becomes a row in a list of questions
// (askUserAnswerRecord, which marks it solo when the answer lands). The transcript codec re-derives
// that verdict for a record written before it rode the wire and matches on this same constant
// (fromWireToolView), so the presenter's rule and the decoder's cannot drift apart — the reason
// subAgentToolName sits beside the span rule that reads it.
const askUserToolName = "ask_user"

// toolRegistry is the open, name-keyed catalogue. Each later tool adds one entry here; the
// renderer and the transcript never grow a per-tool branch. It covers the full built-in set
// (internal/tools DefaultToolsWithHost); only a dynamic tool (an MCP server's) falls to the
// raw-name fallback.
//
// Every entry's LABEL, target and stat are the ratified table's three display columns
// (docs/layout/tool-layout.md, "Display details per tool"): the label heads the block, the
// target leads its branch row, and the stat words the right-aligned outcome slot. A cell the
// table spells `—` is a stat hook returning ("", true) — a deliberately blank slot — and a cell
// the engine cannot supply without growing a wire (design call 14) is a hook returning false,
// which leaves the tool's own prose floor in the slot rather than inventing a number.
//
// Every detail extractor here renders PROSE. The six tools that report a typed summary
// (read_file, write_file, list_dir, grep, view_diff, web_search) word their slot
// from that summary through their stat hook, and keep firstLineDetail as the floor for a result
// that carries none — a degraded card is that tool's own first line, never a file dumped into the
// transcript. The rest quote their fixed sentence or hand free-form output (a command run, a
// sub-agent report) on as a body the collapsed paint shows the gist of: the chat compresses it
// to a first line plus a remainder count until the block is expanded, and the model gets the
// full text either way. One tool's result is nobody's words but the human's — ask_user's, which is
// the answer they typed — so its branch line goes through quotedFirstLineDetail and the block quotes
// that line rather than respelling it. It is also the one tool whose result is only half its
// outcome, so it is the one entry setting an `outcome` hook instead of a `detail` one: the question
// and the choices it recalls beneath that line were in the CALL (askUserAnswerRecord).
//
// The three edit tools and write_file are the group whose body owes nothing to a result: what they
// put in a file is stated in the REQUEST, so each sets an argBody that reads its own arguments as
// -/+ lines (changedLines) and the block shows the change from the moment the call is announced.
var toolRegistry = map[string]toolPresenter{
	"read_file": {
		label:  "Read",
		verb:   "reading",
		target: readFileTarget,  // path, plus ":12–80" and `· locate "…"` when the call asked
		detail: firstLineDetail, // floor; the slot's line count comes from domain.ReadSpan
		stat:   readSpanStat,
		body:   readFileBody, // the located line numbers, when a term was asked for
	},
	"write_file": {
		label:   "Write",
		verb:    "writing",
		target:  stringArg("path"),
		detail:  firstLineDetail,  // floor; the tool's own "+N bytes" header
		argStat: writtenLinesStat, // the lines the REQUEST writes — the result reports bytes
		argBody: writtenLines,     // the content the call writes, as + lines
	},
	"list_dir": {
		label:  "List",
		verb:   "listing",
		target: listDirTarget, // path, plus "· recursive" when the call asked for one
		detail: firstLineDetail,
		stat:   listedEntriesStat,
	},
	"grep": {
		label:  "Grep",
		verb:   "searching",
		target: grepTarget, // pattern, plus "· <glob>" when the call scoped it
		detail: firstLineDetail,
		stat:   matchedLinesStat, // "N hits"; the table's "· M files" is not on the result
	},
	"find_files": {
		label:  "Find Files",
		verb:   "finding",
		target: stringArg("pattern"),
		detail: firstLineDetail, // the tool's own header: "[N files found, showing …]"
		stat:   foundFilesStat,
	},
	"single_find_and_replace": {
		label:   "Replace",
		verb:    "editing",
		target:  stringArg("path"),
		detail:  firstLineDetail,       // "replaced text in <path>"
		stat:    editRegionsStat,       // "+A −R" off the regions the apply recorded, once they land
		argStat: singleReplacementStat, // "+A −R", counted off the pair the call asks for
		argBody: singleReplacementBody, // the one oldText → newText pair, as -/+ lines
	},
	"multi_find_and_replace": {
		label:   "Replace",
		verb:    "editing",
		target:  stringArg("path"),
		detail:  firstLineDetail,      // "applied N replacements to <path>"
		stat:    editRegionsStat,      // "+A −R" off the regions the apply recorded, once they land
		argStat: multiReplacementStat, // "N changes" — one per replacement the call lists
		argBody: multiReplacementBody, // one -/+ pair per replacement, in argument order
	},
	"edit_existing_file": {
		label:   "Edit",
		verb:    "editing",
		target:  stringArg("path"),
		detail:  firstLineDetail, // "applied patch to <path> (N hunks)" / "updated <path>"
		stat:    editRegionsStat, // "+A −R" off the regions the apply recorded, once they land
		argStat: fileEditStat,    // "+A −R", counted off the patch (or content) the call sends
		argBody: fileEditBody,    // a patch's hunks, or full replacement content as + lines
	},
	"view_diff": {
		label:  "Diff Preview",
		verb:   "diffing",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the "No changes detected" sentinel renders here too
		stat:   diffStatStat,
		body:   viewDiffBody, // the coloured diff beneath a domain.DiffStat branch line
	},
	"copy_file": {
		label:  "Copy",
		verb:   "copying",
		target: sourceDestinationTarget,
		detail: firstLineDetail, // "copied a.txt to b.txt"
		stat:   blankStat,       // the table's `—`: the target already says what happened
	},
	"move_file": {
		label:  "Move",
		verb:   "moving",
		target: sourceDestinationTarget,
		detail: firstLineDetail, // "moved a.txt to b.txt"
		stat:   blankStat,
	},
	"delete_file": {
		label:  "Delete",
		verb:   "deleting",
		target: stringArg("path"),
		detail: firstLineDetail, // "deleted a.txt"
		stat:   blankStat,
	},
	"terminal": {
		label:   "Terminal",
		verb:    "running",
		target:  stringArg("command"),
		failure: exitCodeFailure,
		detail:  outputDetail,
		stat:    exitCodeStat,
	},
	"python_exec": {
		label:   "Python",
		verb:    "running python",
		target:  firstLineArg("code"),
		failure: exitCodeFailure,
		detail:  outputDetail,
		stat:    exitCodeStat,
	},
	"git_branch": {
		label:  "Git Branch",
		verb:   "branching",
		target: joinedArgs("action", "name"),
		detail: outputDetail, // a branch list is multi-line; create/switch is one line
		stat:   blankStat,
	},
	"git_commit": {
		label:  "Git Commit",
		verb:   "committing",
		target: firstLineArg("message"),
		// "[main abc1234] subject" + the diffstat lines, with the one-line shape kept out of the
		// slot so the hash holds it at every width (commitDetail).
		detail: commitDetail,
		stat:   commitHashStat,
	},
	"git_diff_range": {
		label:  "Git Diff",
		verb:   "diffing",
		target: refRangeTarget,
		detail: outputDetail,
		stat:   diffLinesStat, // "+A −R", counted off the unified diff the tool printed
	},
	"git_status": {
		label: "Git Status",
		verb:  "checking",
		// No target: the tool takes no arguments — the repository IS the target.
		detail: outputDetail, // the branch line plus the staged/unstaged/untracked sections
		stat:   changedFilesStat,
	},
	"git_log": {
		label:  "Git Log",
		verb:   "reading",
		target: gitLogTarget,
		detail: outputDetail, // one line per commit
		stat:   commitCountStat,
	},
	"diagnostics": {
		label:  "Diagnostics",
		verb:   "checking",
		target: stringArg("path"),
		detail: outputDetail,
		stat:   cleanStat, // findings come back as an error result, so a success IS clean
	},
	"run_tests": {
		label: "Tests",
		verb:  "running tests",
		// Both arguments are optional and the common call carries neither — the whole suite is
		// the target then, and runTestsTarget renders the empty target a bare "running tests" needs.
		target: runTestsTarget,
		detail: firstLineDetail, // the tool's own verdict line: "PASS (go test)" / "FAIL …"
		stat:   testVerdictStat,
	},
	"web_fetch": {
		label:  "Fetch",
		verb:   "fetching",
		target: stringArg("url"),
		detail: firstLineDetail, // "HTTP 200 OK" — the body never floods the chat
	},
	"http_request": {
		label:  "HTTP",
		verb:   "requesting",
		target: methodURLTarget,
		detail: firstLineDetail, // "HTTP 200 OK"
	},
	"web_search": {
		label:  "Search",
		verb:   "searching the web",
		target: stringArg("query"),
		detail: firstLineDetail,
		stat:   searchHitsStat,
	},
	"sub_agent": {
		label:  "Sub-Agent",
		verb:   "delegating",
		target: subAgentTarget, // the delegation's name when it was given one, else the task's first line
		detail: outputDetail,   // the report's gist; the nested run already rendered railed
		stat:   delegationStat, // "done"; a failed delegation is an error result and reads red
	},
	askUserToolName: {
		label:   "Ask User",
		verb:    "asking",
		target:  firstLineArg("question"),
		outcome: askUserAnswerRecord, // the answer quoted on the branch, the exchange recorded beneath
	},
	"present_document": {
		label:  "Present",
		verb:   "presenting",
		target: presentDocumentTarget, // the document's title, falling back to its path
		detail: firstLineDetail,       // "Presented <path>: opened on the user's machine."
		stat:   blankStat,
	},
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
			tv.Summary = namedSummary(detailLine{Text: s})
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
	tv.stat = ws.shorten(tv.stat)
}

// sanitize escape-strips every DISPLAY field of the view — label, verb, target, a delegation's name
// and its retained prompt (toolView.task), the one-line
// summary, the typed stat standing by to replace it (toolView.stat) and each detail line — so no
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
	tv.stat = stripEscapes(tv.stat)
	tv.Details.stripEscapes()
	tv.Regions = strippedRegions(tv.Regions)
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
	return tv.stat != "" && tv.Summary.quoted && tv.Summary.Text != ""
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
	tv.Summary = namedSummary(detailLine{Text: tv.stat})
	tv.stat = ""
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
//   - any FAILED member and the row counts them, "N errors", which reads red for exactly the reason
//     a member's own failure does — the wording (failedSummary).
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
	if text, ok := sumStats(views); ok {
		return namedSummary(detailLine{Text: text})
	}
	return branchSummary{}
}

// failedCalls counts the members of a run whose outcome says the call failed. It asks
// [failedSummary] (render.go) rather than restating the three wordings, because "does this outcome
// say failure" is one question and a second answer to it would drift the first time the vocabulary
// moved.
func failedCalls(views []toolView) int {
	n := 0
	for _, tv := range views {
		if failedSummary(tv.Summary.Text) {
			n++
		}
	}
	return n
}

// sumStats adds a run's typed stats up into one phrase, and reports whether they add up at all. Two
// shapes sum, and they are the two shapes the registry's stat hooks actually write: a diffstat
// ("+8 −3", diffCounts) and a counted noun ("12 lines", "4 entries", "2 hits"). Everything else —
// "exit 0", "PASS", "clean", "done", a short hash — has no sum and comes back false, which the type
// row prints as an empty slot.
//
// Only stats the presenter WORDED are summed (statPhrase): a promoted line is the tool's own text,
// and one that happened to read "3 errors" or "12 lines" would otherwise be added into an arithmetic
// it was never part of.
func sumStats(views []toolView) (string, bool) {
	if text, ok := sumDiffCounts(views); ok {
		return text, true
	}
	return sumCountPhrases(views)
}

// statPhrase is one member's slot text WHEN it is the presenter's own typed phrase — the only kind
// that may be summed. A quoted promotion, and a call still in flight with no slot at all, answer
// false.
func statPhrase(tv toolView) (string, bool) {
	if tv.Summary.quoted || tv.Summary.Text == "" {
		return "", false
	}
	return tv.Summary.Text, true
}

// sumDiffCounts adds a run of diffstats up: "+2 −1" and "+6 −2" make "+8 −3". Every member must
// carry one, so a run where a single call errored or is still open does not sum — and does not need
// to, since a failure is counted by the branch above it (runAggregate).
//
// What a member is asked for is its NUMBERS, not its wording (memberDiffCounts): a block whose stat
// came from a typed summary hands them over as it holds them, and only a member that has nothing
// but the phrase is read back out of it.
func sumDiffCounts(views []toolView) (string, bool) {
	added, removed := 0, 0
	for _, tv := range views {
		a, r, ok := memberDiffCounts(tv)
		if !ok {
			return "", false
		}
		added, removed = added+a, removed+r
	}
	return diffCounts(added, removed), true
}

// memberDiffCounts is one member's diffstat as a number pair, in the order the two readings are
// trusted: the typed value a summary supplied (toolView.diffStat), else the phrase in the slot
// parsed back (parseDiffCounts, the floor for every producer that only ever had text).
//
// The slot must still hold the presenter's OWN typed phrase either way (statPhrase): a promoted
// line is the tool's words, and a run that promoted one is not summing its members' stats at all.
func memberDiffCounts(tv toolView) (added, removed int, ok bool) {
	if _, ok := statPhrase(tv); !ok {
		return 0, 0, false
	}
	if tv.hasDiffStat {
		return tv.diffStat.Added, tv.diffStat.Removed, true
	}
	return parseDiffCounts(tv.Summary.Text)
}

// parseDiffCounts reads a diffstat back out of the phrase diffCounts wrote — the one direction the
// wording is ever read in, and deliberately anchored on that function's exact spelling (the ASCII
// "+" and the U+2212 minus) so a change to how a diffstat is written cannot leave a summer quietly
// parsing the old one.
func parseDiffCounts(text string) (added, removed int, ok bool) {
	head, tail, found := strings.Cut(text, " −")
	if !found || !strings.HasPrefix(head, "+") {
		return 0, 0, false
	}
	a, err := strconv.Atoi(strings.TrimPrefix(head, "+"))
	if err != nil {
		return 0, 0, false
	}
	r, err := strconv.Atoi(tail)
	if err != nil {
		return 0, 0, false
	}
	return a, r, true
}

// sumCountPhrases adds a run of counted nouns up. Every member must count the SAME thing, judged on
// the noun with a plural "s" trimmed off it, so "1 line" and "12 lines" sum and "3 hits" beside
// "2 files" does not.
//
// The total is then spelled the way the run's own members spell it: the noun is borrowed from a
// member whose count has the same plurality as the total, which is what keeps a producer's fixed
// spelling ("1 entries", "0 changed") intact instead of re-pluralising it into "1 entrie" or
// "2 changeds". Only where no member shares the total's plurality — two ones making a two — does it
// fall back to the naive plural, which is the very rule those members were written by.
func sumCountPhrases(views []toolView) (string, bool) {
	total, stem := 0, ""
	for i, tv := range views {
		text, ok := statPhrase(tv)
		if !ok {
			return "", false
		}
		n, noun, ok := countPhrase(text)
		if !ok {
			return "", false
		}
		if s := strings.TrimSuffix(noun, "s"); i == 0 {
			stem = s
		} else if s != stem {
			return "", false
		}
		total += n
	}
	for _, tv := range views {
		n, noun, _ := countPhrase(tv.Summary.Text)
		if (n == 1) == (total == 1) {
			return strconv.Itoa(total) + " " + noun, true
		}
	}
	return plural(total, stem), true
}

// countPhrase splits a counted noun into its number and its word — "12 lines" into 12 and "lines".
// It is deliberately total and deliberately strict: a phrase that is not exactly an integer, one
// space and one word is not a count, so "exit 0", "PASS", "a1b2c3d" and a promoted sentence all
// answer false and reach the slot untouched.
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
		tv.absorbRegions(regions)
	}
	// The request-derived stat is re-applied because the prose layers may have written over it
	// with a result sentence; it is the same phrase the call was presented with, so a block does
	// not change what its slot says when its result lands.
	if tv.argStat != "" {
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

// absorbRegions folds an edit tool's recorded regions into the view: the regions themselves (which
// the split reading composes at paint time), the stacked rows they render as, and the typed
// diffstat the slot and the run aggregate read.
//
// The body is REPLACED rather than grown. What the call was presented with is the change the model
// ASKED for, read off its own arguments before any result existed (argBody); what arrives here is
// the change that LANDED, with the line numbers and the context the arguments never held. Keeping
// both would show the same edit twice, and the recorded one is the one that happened.
func (tv *toolView) absorbRegions(regions domain.EditRegions) {
	tv.Regions = regions.Regions
	tv.Details = newToolBody(stackedDiffLines(regions.Regions))
	tv.diffStat, tv.hasDiffStat = regions.Stat(), true
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
//   - otherwise the stat IS the slot, empty string included: the table's `—` is a blank slot with
//     the dots running to the `▶`, and it is stated rather than left over from a prose sentence.
func (tv *toolView) applyStat(text string, ok bool) {
	if !ok {
		return
	}
	if tv.Summary.quoted && tv.Summary.Text != "" {
		tv.stat = text
		return
	}
	tv.Summary = namedSummary(detailLine{Text: text})
	tv.stat = ""
}

// ----------------------------------------------------------------------------
// Outcome-slot stats — the table's `<tool-top-level-details>` column, one hook per tool
// ----------------------------------------------------------------------------
//
// Each of these words ONE tool's right-hand slot (toolPresenter.stat). They come in three kinds,
// and the kind is the honest answer to "where does this fact live?":
//
//   - off the typed domain.ToolSummary the tool already reports (readSpanStat, listedEntriesStat,
//     …) — the contract that exists precisely so a host need not read a sentence;
//   - off the call's OWN ARGUMENTS (writtenLinesStat, the edit stats) — a write's line count and
//     an edit's diffstat are facts about the REQUEST, so the slot can say them without the tool
//     reporting anything and without a byte more crossing the wire (ADR 0031);
//   - off a fixed HEADER the tool writes into its own output (testVerdictStat, foundFilesStat,
//     changedFilesStat, commitCountStat, commitHashStat, diffLinesStat). This is the reading the
//     file's opening note warns about, and it is taken only because design call 14 rules out
//     growing the engine for presentation. Every one of them is anchored on a token the tool
//     formats deliberately and every one is TOTAL: a shape it does not recognise returns false,
//     which leaves that tool's prose floor in the slot rather than a wrong number. A wording
//     change in internal/tools degrades such a card to what it showed before this existed.
//
// A stat is the block's OWN wording, so the shortening seam spells any path it names relative to
// the workspace (shortenPaths) — the same treatment every phrase this file writes gets.

// blankStat is the table's `—`: a tool whose outcome is already fully said by its header and
// target (a copy's two paths, a delete's one). The slot prints nothing and the dots run to the
// `▶`. It is stated rather than left empty by omission, because omitting the hook would leave the
// tool's prose sentence in a slot the table says is blank.
func blankStat(domain.ToolResult) (string, bool) { return "", true }

// readSpanStat words read_file's slot as the number of lines the call returned, counted off the
// span the tool reports (domain.ReadSpan, 1-based and inclusive). A file with no lines at all
// yields a span whose End precedes its Start, which is 0 lines rather than a negative count.
func readSpanStat(res domain.ToolResult) (string, bool) {
	v, ok := res.Summary.(domain.ReadSpan)
	if !ok {
		return "", false
	}
	n := v.End - v.Start + 1
	if n < 0 {
		n = 0
	}
	return plural(n, "line"), true
}

// writtenLinesStat words write_file's slot as the number of lines the call WRITES, read off its
// own content argument — the table asks for lines and the tool reports bytes (domain.WroteBytes),
// and the request already holds the answer. It is the same reading the body takes (writtenLines),
// so the two cannot disagree about what the call puts in the file.
func writtenLinesStat(args map[string]any) (string, bool) {
	content, ok := args["content"].(string)
	if !ok {
		return "", false
	}
	return plural(len(editLines(content)), "line"), true
}

// listedEntriesStat words list_dir's slot as the directory's total entry count. "entries" is a
// FIXED plural, deliberately not plural(): the card has always read "1 entries".
func listedEntriesStat(res domain.ToolResult) (string, bool) {
	v, ok := res.Summary.(domain.ListedEntries)
	if !ok {
		return "", false
	}
	return strconv.Itoa(v.Total) + " entries", true
}

// matchedLinesStat words grep's slot as its hit count. The table also asks for the number of FILES
// those hits fall in; no result carries it (domain.MatchedLines is a total alone), so the slot
// says the half that exists rather than a second number derived from a listing (design call 14).
func matchedLinesStat(res domain.ToolResult) (string, bool) {
	v, ok := res.Summary.(domain.MatchedLines)
	if !ok {
		return "", false
	}
	return plural(v.Total, "hit"), true
}

// searchHitsStat words web_search's slot as its result count.
func searchHitsStat(res domain.ToolResult) (string, bool) {
	v, ok := res.Summary.(domain.SearchHits)
	if !ok {
		return "", false
	}
	return plural(v.Count, "result"), true
}

// diffStatStat words view_diff's slot as the diffstat the tool counted off its own operations. A
// "No changes detected" result carries no domain.DiffStat and so keeps its sentence.
func diffStatStat(res domain.ToolResult) (string, bool) {
	v, ok := res.Summary.(domain.DiffStat)
	if !ok {
		return "", false
	}
	return diffCounts(v.Added, v.Removed), true
}

// editRegionsStat words the three edit tools' slot as the diffstat of what LANDED, summed over the
// regions the tool recorded while it held both sides of the file (domain.EditRegions.Stat — the one
// derivation of that pair, so the slot and the rows beneath it cannot disagree).
//
// It is the result half of a slot the REQUEST already worded: an edit's argument-derived diffstat
// is on the view from the moment the call is announced (toolView.argStat), and a result that
// recorded no regions leaves it standing. What the two say differs where it matters — the arguments
// know what was asked for, the regions know what the file took — and the landed reading wins once
// it exists.
func editRegionsStat(res domain.ToolResult) (string, bool) {
	regions, ok := recordedRegions(res)
	if !ok {
		return "", false
	}
	stat := regions.Stat()
	return diffCounts(stat.Added, stat.Removed), true
}

// diffCounts is the house spelling of a diffstat — "+8 −3", with the table's typographic minus
// (U+2212) rather than a hyphen, so the two halves read as a matched pair at any weight. Every
// slot that carries one goes through here: view_diff's typed stat, the edit tools' argument-
// derived one, git_diff's counted one.
func diffCounts(added, removed int) string {
	return "+" + strconv.Itoa(added) + " −" + strconv.Itoa(removed)
}

// pairCounts totals what a set of edit pairs adds and removes — the same pairs the body renders
// (changedLines), so an edit's slot and its lines are two readings of one answer.
func pairCounts(pairs []editPair) (added, removed int) {
	for _, p := range pairs {
		added += len(p.inserted)
		removed += len(p.removed)
	}
	return added, removed
}

// singleReplacementStat words single_find_and_replace's slot as the diffstat of the one pair the
// call asks for. A call with neither side is not an edit at all and keeps its prose floor.
func singleReplacementStat(args map[string]any) (string, bool) {
	removed, _ := args["oldText"].(string)
	inserted, _ := args["newText"].(string)
	if removed == "" && inserted == "" {
		return "", false
	}
	a, r := pairCounts([]editPair{replacedText(removed, inserted)})
	return diffCounts(a, r), true
}

// multiReplacementStat words multi_find_and_replace's slot as the NUMBER OF CHANGES the call
// lists, which is what the table asks of it — a batch's shape is how many edits it makes, and the
// lines they touch are beneath (multiReplacementBody). Malformed arguments keep the prose floor.
func multiReplacementStat(args map[string]any) (string, bool) {
	list, ok := args["replacements"].([]any)
	if !ok {
		return "", false
	}
	return plural(len(list), "change"), true
}

// fileEditStat words edit_existing_file's slot as the diffstat of what the call sends: a patch's
// hunks, or full replacement content that removes nothing and inserts the lot — the same two
// readings its body takes (fileEditBody).
func fileEditStat(args map[string]any) (string, bool) {
	content, ok := args["content"].(string)
	if !ok {
		return "", false
	}
	pairs := []editPair{replacedText("", content)}
	if isPatchArgument(content) {
		pairs = patchEditPairs(content)
	}
	a, r := pairCounts(pairs)
	return diffCounts(a, r), true
}

// exitCodeStat words the slot of the two tools that run a process. A non-zero exit is an ERROR
// result (internal/tools: terminal and python_exec both flag it), which reads red from the error
// layer above — so a result reaching here exited cleanly, and the slot says so. The table asks
// for a duration beside it; no result carries one, so the code stands alone (design call 14).
func exitCodeStat(domain.ToolResult) (string, bool) { return "exit 0", true }

// exitCodeMarker matches the "[exit code N]" line subprocessToolResult appends to a FAILED
// subprocess result (internal/tools/terminal.go). It is anchored at the end of the output, where
// the tool writes it, so a command that printed the same phrase cannot be read as the marker — the
// real one is always appended after it. The code may be negative: a run whose leader exited but
// whose pipe stayed held is reported as -1.
var exitCodeMarker = regexp.MustCompile(`\n?\[exit code (-?\d+)\]\s*$`)

// exitCodeFailure words a failed subprocess call's slot from that marker — "exit 2", the red
// counterpart of a clean exit's "exit 0" (exitCodeStat) — and hands back the output with the
// marker taken off, which is the body laid out beneath it. The code is the one thing that always
// says the command failed; its first output line often says something else entirely ("total
// 20760"), which is what the slot used to spend itself on.
//
// A result with no marker is not this shape — a run the tool refused, a fault raised before the
// process started — so it falls back to that first line, where such a result does word its own
// failure.
func exitCodeFailure(content string) (string, string, bool) {
	m := exitCodeMarker.FindStringSubmatchIndex(content)
	if m == nil {
		return "", "", false
	}
	return "exit " + content[m[2]:m[3]], content[:m[0]], true
}

// cleanStat words diagnostics' slot. Findings come back flagged as an error result, so a result
// that reaches here found none — the table's `clean` half; its `N issues` half is the red error
// line the failure layer already paints.
func cleanStat(domain.ToolResult) (string, bool) { return "clean", true }

// delegationStat words a sub-agent's slot. A delegation that failed comes back as an error result
// and reads red, so a result reaching here is one that finished. The table asks for a step count
// beside the verdict; the engine exposes none on the result (design call 14), so the verdict
// stands alone.
func delegationStat(domain.ToolResult) (string, bool) { return "done", true }

// testVerdictHead matches the verdict token run_tests opens its condensed report with — "PASS (go
// test)", "FAIL (pytest) — 3 failing tests" — anchored at the start so a later line reading "FAIL"
// cannot be mistaken for the header.
var testVerdictHead = regexp.MustCompile(`^(PASS|FAIL)\b`)

// testVerdictStat words run_tests' slot as its bare verdict. The table asks for a duration beside
// it; the result carries none (design call 14), so the verdict stands alone. Output the tool
// worded some other way keeps its own first line in the slot.
func testVerdictStat(res domain.ToolResult) (string, bool) {
	m := testVerdictHead.FindStringSubmatch(strings.TrimSpace(firstLine(res.Content)))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// foundFilesHead matches the header find_files opens its listing with — "[12 files found, showing
// 1-12]", the count being the FULL total rather than the page.
var foundFilesHead = regexp.MustCompile(`^\[(\d+) files found\b`)

// foundFilesStat words find_files' slot as that total, and reads the tool's own empty-result
// sentence as the zero it states.
func foundFilesStat(res domain.ToolResult) (string, bool) {
	head := strings.TrimSpace(firstLine(res.Content))
	if head == "No files found" {
		return plural(0, "file"), true
	}
	m := foundFilesHead.FindStringSubmatch(head)
	if m == nil {
		return "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return "", false
	}
	return plural(n, "file"), true
}

// gitStatusSection matches one section header of git_status' report — "Staged (3):" — whose count
// is the FULL one even where the list beneath it was capped.
var gitStatusSection = regexp.MustCompile(`(?m)^(?:Staged|Unstaged|Untracked) \((\d+)\):`)

// changedFilesStat words git_status' slot as how many files the working tree has changed: the sum
// of the three section counts, or the zero its clean-tree sentence states. A report in neither
// shape keeps its prose floor.
func changedFilesStat(res domain.ToolResult) (string, bool) {
	if strings.Contains(res.Content, "Working tree clean") {
		return "0 changed", true
	}
	sections := gitStatusSection.FindAllStringSubmatch(res.Content, -1)
	if len(sections) == 0 {
		return "", false
	}
	total := 0
	for _, s := range sections {
		n, err := strconv.Atoi(s[1])
		if err != nil {
			return "", false
		}
		total += n
	}
	return strconv.Itoa(total) + " changed", true
}

// commitCountStat words git_log's slot as how many commits it listed. The tool prints one line per
// commit ("--format=%h %ad %s", a subject being single-line by construction), so the lines ARE the
// count; its empty-result sentence states the zero.
func commitCountStat(res domain.ToolResult) (string, bool) {
	trimmed := strings.TrimSpace(res.Content)
	if trimmed == "" {
		return "", false
	}
	if trimmed == "No commits found" {
		return plural(0, "commit"), true
	}
	n := 0
	for _, ln := range splitLines(trimmed) {
		if strings.TrimSpace(ln) != "" {
			n++
		}
	}
	return plural(n, "commit"), true
}

// commitHashHead matches the short hash in either shape git_commit returns: the
// `git log -1 --oneline` line it reports on success — "a1b2c3d subject" — and, on the fallback
// branch that relays git's own commit output, "[main a1b2c3d] subject" or
// "[detached HEAD a1b2c3d] subject". Anchored at the line's start in both cases.
var commitHashHead = regexp.MustCompile(`^(?:\[[^\]]*\b([0-9a-f]{7,40})\]|([0-9a-f]{7,40})) `)

// commitHashOf reads that hash off a commit result's own output. It is the ONE derivation of it,
// shared by the two halves that have to agree about it: the slot's stat (commitHashStat) and the
// prose half that withholds its promotion whenever the slot is going to say it (commitDetail). Were
// they to read it apart they could disagree on a shape neither anticipated — the promotion withheld
// and the slot left blank — so they read it here.
func commitHashOf(content string) (string, bool) {
	m := commitHashHead.FindStringSubmatch(strings.TrimSpace(firstLine(content)))
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		return m[1], true
	}
	return m[2], true
}

// commitHashStat words git_commit's slot as that short hash — the one thing a later call needs and
// the header line does not repeat. A result in another shape (nothing to commit, a hook's message)
// keeps its prose floor.
func commitHashStat(res domain.ToolResult) (string, bool) {
	return commitHashOf(res.Content)
}

// diffLinesStat words git_diff_range's slot as the diffstat of the unified diff the tool printed,
// counting the tagged lines and skipping the "+++"/"---" file headers that are not content. A call
// asking for `--stat` or `--name-only` prints no tagged lines at all and keeps its prose floor,
// which is the honest answer: that output states its own totals.
func diffLinesStat(res domain.ToolResult) (string, bool) {
	added, removed := 0, 0
	for _, ln := range splitLines(res.Content) {
		switch {
		case strings.HasPrefix(ln, "+++"), strings.HasPrefix(ln, "---"):
		case strings.HasPrefix(ln, "+"):
			added++
		case strings.HasPrefix(ln, "-"):
			removed++
		}
	}
	if added == 0 && removed == 0 {
		return "", false
	}
	return diffCounts(added, removed), true
}

// ----------------------------------------------------------------------------
// Extractor helpers
// ----------------------------------------------------------------------------

// stringArg returns a target extractor that reads one string argument by key. A missing or
// non-string value yields the empty target (the block then has no target line at all — its
// details are the branches themselves).
func stringArg(key string) func(map[string]any) string {
	return func(args map[string]any) string {
		if v, ok := args[key].(string); ok {
			return v
		}
		return ""
	}
}

// firstLineArg returns a target extractor for a possibly multi-line string argument (a
// commit message, a Python script, a sub-agent task): the first line, clipped, so the
// branch shows the gist without flooding a row.
func firstLineArg(key string) func(map[string]any) string {
	return func(args map[string]any) string {
		if v, ok := args[key].(string); ok {
			return clipDetail(firstLine(v))
		}
		return ""
	}
}

// intArg reads one whole-number argument by key. JSON has no integer type, so a count the model
// sent arrives as a float64 and is read as one; anything else — absent, a string, a fraction that
// is not one — is not a number this view will act on and yields 0, the "not given" answer every
// caller here already has a rendering for.
func intArg(args map[string]any, key string) int {
	v, ok := args[key].(float64)
	if !ok || v != float64(int(v)) {
		return 0
	}
	return int(v)
}

// qualifiedTarget joins a target's head with the QUALIFIER a call put on it — "· recursive",
// `· locate "x"` — in the table's own separator. It is one function rather than four spellings of
// the same concatenation so every qualified target reads alike; an unqualified call is the head
// alone, and a call with a qualifier but no head (a filter with no path) is the qualifier alone
// rather than a row opening on a stray separator.
func qualifiedTarget(head, qualifier string) string {
	switch {
	case qualifier == "":
		return head
	case head == "":
		return qualifier
	}
	return head + " · " + qualifier
}

// readFileTarget leads read_file's branch with the path, carrying the LINE RANGE the call asked
// for ("main.go:12–80") when it asked for one — the table's ranged form, which is what tells two
// reads of one file apart in a group. A half-open request states the half it gave: a start with no
// end reads "…:12–", an end with no start "…:–80". A plain read is the bare path.
//
// A locate term rides the same target as the qualifier the table spells it with, composing with
// the range rather than replacing it (`main.go:12–80 · locate "x"`): the range says WHICH lines
// came back and the term says what the call was hunting for, and the whole file is scanned for it
// whatever the range (domain.ReadSpan). The lines it was found on lay out beneath the branch
// (readFileBody).
func readFileTarget(args map[string]any) string {
	head, _ := args["path"].(string)
	start, end := intArg(args, "start_line"), intArg(args, "end_line")
	if start > 0 || end > 0 {
		span := ""
		if start > 0 {
			span = strconv.Itoa(start)
		}
		span += "–"
		if end > 0 {
			span += strconv.Itoa(end)
		}
		head += ":" + span
	}
	locate := stringArg("locate")(args)
	if locate != "" {
		locate = `locate "` + locate + `"`
	}
	return qualifiedTarget(head, locate)
}

// listDirTarget leads list_dir's branch with the path, marked "· recursive" when the call asked
// for a walk rather than a listing — the one argument that changes what the entry count means.
func listDirTarget(args map[string]any) string {
	recursive := ""
	if v, _ := args["recursive"].(bool); v {
		recursive = "recursive"
	}
	return qualifiedTarget(stringArg("path")(args), recursive)
}

// grepTarget leads grep's branch with the pattern and the include glob that scoped it — a search
// of one file type is a different search, and the hit count in the slot is only readable beside it.
func grepTarget(args map[string]any) string {
	return qualifiedTarget(stringArg("pattern")(args), stringArg("include")(args))
}

// runTestsTarget leads run_tests' branch with the package path and the filter that narrowed the
// run. Both arguments are optional and the common call carries neither — the whole suite is the
// target then, and the empty target a bare "running tests" needs is what comes back.
func runTestsTarget(args map[string]any) string {
	return qualifiedTarget(stringArg("path")(args), stringArg("filter")(args))
}

// presentDocumentTarget leads present_document's branch with the document's TITLE — what the human
// was shown — falling back to its path when the call named no title, so the row always says which
// document was presented.
func presentDocumentTarget(args map[string]any) string {
	if title := stringArg("title")(args); title != "" {
		return title
	}
	return stringArg("path")(args)
}

// subAgentName reads the optional short name off a sub_agent call's arguments, normalised the way
// the tool itself normalises it before stamping it on the child (delegationName, internal/agent):
// the trimmed first line, empty when the call named nothing. The clip is the branch line's own
// budget, so a model that sends a sentence where a name was asked for still leads one row.
func subAgentName(args map[string]any) string {
	v, ok := args["name"].(string)
	if !ok {
		return ""
	}
	return clipDetail(strings.TrimSpace(firstLine(v)))
}

// subAgentTarget leads a delegation's header with the name the model gave it, falling back to the
// delegated task's first line when it gave none — which is every delegation written before the
// argument existed, and every one a Mechanism synthesises (guided decomposition names nothing).
// The two spellings get the same treatment, so a name is clipped and escape-stripped exactly as a
// task line is (firstLineArg, finishDisplay).
//
// The fallback is decided on the RENDERED form — the name as the view's own escape strip will leave
// it, run here and again on the way out (sanitize, idempotent) — because a name is model output and
// the strip can empty one out: a "name" of nothing but control characters is non-empty as it
// arrives, so deciding on the raw string would pick it over the task and then paint a blank slot.
// The trim goes with the strip for the same reason, a control character being all that separated
// two spaces. headlessSubAgentTarget (cmd/apogee) decides the same question the same way, so the
// Driver with no header to paint and the one that paints it name a child alike.
func subAgentTarget(args map[string]any) string {
	if n := strings.TrimSpace(stripEscapes(subAgentName(args))); n != "" {
		return n
	}
	return firstLineArg("task")(args)
}

// joinedArgs returns a target extractor that joins the named string arguments with a space,
// skipping missing ones ("create feature-x", or just "list").
func joinedArgs(keys ...string) func(map[string]any) string {
	return func(args map[string]any) string {
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, " ")
	}
}

// refRangeTarget renders git_diff_range's base/head args as "base...head" (the three-dot
// range the tool diffs).
func refRangeTarget(args map[string]any) string {
	base, _ := args["base"].(string)
	head, _ := args["head"].(string)
	if base == "" && head == "" {
		return ""
	}
	return base + "..." + head
}

// gitLogTarget renders git_log's target as the ref being logged (defaulting to HEAD, matching
// the tool), so the row never omits what the call actually read.
func gitLogTarget(args map[string]any) string {
	ref, _ := args["ref"].(string)
	if ref = strings.TrimSpace(ref); ref == "" {
		return "HEAD"
	}
	return ref
}

// sourceDestinationTarget renders the file-operation pair's target as "source → destination":
// both halves are the point of a copy or a move, and a row naming only one of them would leave
// the reader unable to tell what the call did. A call missing one half still shows the other,
// so a malformed call reads as the partial thing it is rather than as nothing.
func sourceDestinationTarget(args map[string]any) string {
	source, _ := args["source"].(string)
	destination, _ := args["destination"].(string)
	switch {
	case source == "":
		return destination
	case destination == "":
		return source
	}
	return source + " → " + destination
}

// methodURLTarget renders http_request's target as "METHOD url" (method defaults to GET,
// matching the tool).
func methodURLTarget(args map[string]any) string {
	u, _ := args["url"].(string)
	m, _ := args["method"].(string)
	m = strings.ToUpper(strings.TrimSpace(m))
	if m == "" {
		m = "GET"
	}
	return strings.TrimSpace(m + " " + u)
}

// firstLineDetail summarises a result to its first line, clipped — for tools whose result
// is a short fixed sentence ("updated main.go") or opens with a status header ("HTTP 200
// OK"): one line carries the outcome, the rest is the model's food, not the chat's.
//
// That line is the tool's REPORT — the block's own words in the sense the shortening seam means —
// so a path it names is spelled relative to the workspace. A tool whose first line is not a report
// but text handed back verbatim takes quotedFirstLineDetail instead.
func firstLineDetail(content string) toolOutcome {
	return summaryOnly(clipDetail(firstLine(content)))
}

// quotedFirstLineDetail is firstLineDetail's other half: the same shortening to one clipped line,
// for the tool whose result is not a sentence about what it did but content it hands back as it
// stands — ask_user, whose result IS the answer the human typed. The line is marked quoted
// (promotedOutput), so the workspace root is not spelled out of it: a human who answers with an
// absolute path wrote that path, and the block quotes people the way it quotes files.
// The promotion carries no stat: the answer is the whole point of the row and the record beneath it
// already holds every line the human typed, so there is neither a phrase worth swapping in nor a
// body line to demote to (promotedOutput, askUserAnswerRecord).
func quotedFirstLineDetail(content string) toolOutcome {
	return promotedOutput(clipDetail(firstLine(content)), "")
}

// askUserAnswerRecord renders an ANSWERED ask_user call. The branch line is what it always was —
// the human's answer, quoted and never respelled (quotedFirstLineDetail) — and beneath it the block
// now keeps the permanent RECORD of an exchange the screen otherwise took away with the popup:
// every line of the question as it was put, one line per offered choice ticked or unticked, and any
// answer line no choice accounts for (askExchangeLines).
//
// It runs only when a result lands, so a question still on screen is untouched: while the human is
// answering, the popup IS the live view of the offering and the block stays the summary-only card it
// has always been. The record materialises with the answer.
//
// Nothing in it crossed the wire for its sake. The question and the choices are the model's own
// arguments, kept on the view at presentation time (toolView.args), and the answer is the result the
// tool already returned — so the engine is untouched, the tool's result content is still exactly
// AskAnswer.Text, and no token is spent on the record (ADR 0031). That is what earns the tool's
// description the right to tell the model NOT to restate a question it asks: the transcript keeps
// this instead.
// The record is also what makes the block SOLO (toolOutcome.Solo): an answered question is a card
// the reader comes back to, not one row of a batch, so it never folds into a group of its
// neighbours. It used to be kept out of one by the body it carries — grouping admitted only bodiless
// calls — and now that a Terminal call and its output group like anything else, the exclusion has
// to be said rather than inherited.
func askUserAnswerRecord(args map[string]any, content string) toolOutcome {
	out := quotedFirstLineDetail(content)
	out.Details = askExchangeLines(args, content)
	out.Solo = true
	return out
}

// askExchangeLines lays the answered exchange out beneath the branch, in three groups: the
// question's own lines, the offered choices each behind "[x]" or "[ ]", and then every answer line
// that named no choice. A question offering none still gets the first group — the record is uniform,
// and a free-text question with its answer on the branch above it reads as one card either way.
//
// The third group is what makes the record honest about a MULTI-LINE answer. The branch holds only
// the first line of what the human typed (quotedFirstLineDetail), so the rest of a several-line
// answer used to reach the screen nowhere at all; here every line of it lands, either as a tick
// beside the choice it names or as a line of its own.
//
// A question that offered NO choices is the one place that group starts at the second line. Its
// first line is already the branch directly above, with no list in between, so recording it would
// open the body by repeating the row over it. Where choices WERE offered the same line is kept: a
// list of unticked boxes says only that the human took none of them, and the line beneath says what
// they said instead — the two are read together, and the list stands between them.
//
// A line names a choice when it EQUALS that choice as the human was offered it — trimmed, then
// escape-stripped, the same two acts in the same order the tool and the popup perform on the way out
// (tools.sanitiseChoices, Model.checkedLabels) — so what is ticked here is what was ticked there,
// and a hostile choice string carrying an ESC byte is compared as it was painted rather than
// quietly failing to match. Equality is the whole test: the answer contract is the label verbatim,
// one per line (domain.AskAnswer), and anything looser would tick a box the human did not.
//
// The markers are the popup's own pinned ASCII pair rather than a second spelling of them
// (askCheckedMarker/askUncheckedMarker, docs/layout/user-questions-layout.md), and they are drawn
// for a single-select question exactly as for a multi-select one: this is a record of what was
// asked and answered, not a menu anyone can still act on.
func askExchangeLines(args map[string]any, content string) []detailLine {
	choices := offeredChoices(args)
	answers := answerLines(content)

	given := make(map[string]bool, len(answers))
	for _, a := range answers {
		given[a] = true
	}
	offered := make(map[string]bool, len(choices))
	for _, c := range choices {
		offered[c] = true
	}

	lines := make([]detailLine, 0, len(choices)+len(answers)+1)
	question, _ := args["question"].(string)
	if question = strings.TrimRight(question, "\n"); question != "" {
		for _, ln := range splitLines(question) {
			lines = append(lines, detailLine{Text: clipDetail(ln)})
		}
	}
	for _, c := range choices {
		marker := askUncheckedMarker
		if given[c] {
			marker = askCheckedMarker
		}
		lines = append(lines, detailLine{Text: clipDetail(marker + " " + c)})
	}
	for i, a := range answers {
		if i == 0 && len(choices) == 0 {
			continue // already the branch line directly above, with no list between the two
		}
		if !offered[a] {
			lines = append(lines, detailLine{Text: clipDetail(a)})
		}
	}
	return lines
}

// offeredChoices reads the choices an ask_user call put to the human — in the order they were
// offered, spelled the way the popup painted them: trimmed and blank-dropped (the tool's own
// sanitiseChoices, which is what actually reached the human) and then escape-stripped (the popup's).
// A non-string entry is skipped rather than guessed at, and an absent or malformed array yields
// none, which is simply the free-text question: a record with no checkbox list, never an error.
func offeredChoices(args map[string]any) []string {
	list, ok := args["choices"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		choice, ok := item.(string)
		if !ok {
			continue
		}
		if cleaned := stripEscapes(strings.TrimSpace(choice)); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

// answerLines splits an ask_user result into the lines the reply contract puts in it: one ticked
// label per line for a multi-select answer, a single line for everything else (domain.AskAnswer).
// The trailing newline is dropped so a reply ending in one does not record a blank line the human
// never typed; a blank line BETWEEN two lines is kept, because that is the shape of what they wrote.
// An empty answer has no lines at all rather than one empty one.
func answerLines(content string) []string {
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return nil
	}
	return splitLines(trimmed)
}

// outputDetail splits free-form output (a command run, a diagnostics report, a sub-agent
// report) into the half its own size dictates, keeping EVERY line it was given. Which half it
// fills follows the same rule as every other extractor: output that comes to exactly ONE
// non-empty-led line — a single-line result, or none at all — is that call's whole outcome and
// fills the branch row's outcome slot ("┕ true ⋯⋯⋯ (no output)"), which is also what keeps such
// calls grouping; output with more to say is a body and lays out beneath the target instead,
// because two lines cannot share a row (docs/layout/tool-layout.md, "Single tool collapsed").
//
// The one-line half is an OFFER, not a settlement: a line too long to share a narrow row with the
// target is put back into the body by the painter's promote-guard, which is why that half hands over
// the typed stat to take the slot in its place (promotedOutput, design call 5). "(no output)" makes
// no such offer — the block wrote that phrase itself, and there is nothing to demote.
//
// It truncates NOTHING. The collapsed paint's "+N more lines" marker is a render-time act on this
// retained body (collapsedDetails, render.go) — it counts the body whole and paints none of it, so
// expanding the block is what shows anything the compact shape hides. Only the per-line clip stays here — a 160-rune cap on one line, which
// keeps a minified blob from flooding a row in either state and is not a truncation of the body.
//
// It respells nothing either, and the one-line half is where that has to be SAID rather than
// merely done: those lines go into the summary slot, which otherwise holds the presenter's own
// wording and is shortened against the workspace as such. Output promoted there is quoted content
// exactly as the body beneath it is — one line of a file, one line of a log — so it is handed over
// as promotedOutput and the shortening seam leaves it alone. "(no output)" is this function's own
// phrase and goes the other way, as a named summary.
func outputDetail(content string) toolOutcome {
	body := outputBody(content)
	if len(body) == 0 {
		return summaryOnly("(no output)")
	}
	if len(body) == 1 {
		// The one-line half is a promotion the painter may still refuse, so the stat travels with
		// it: the line count this output would have been summarised by had it come to two lines,
		// which is exactly the shape the guard demotes it into (promotedOutput, design call 5).
		return promotedOutput(body[0].Text, plural(len(body), "line"))
	}
	return toolOutcome{Details: body}
}

// outputBody is the body half of that split on its own: free-form output as the lines it lays out
// as, trailing blank lines and a leading run of them dropped, each under the per-line clip. Output
// that is blank throughout has no lines at all — the callers word that case themselves, one as
// "(no output)" and the other as the exit code standing alone (absorbFailure).
func outputBody(content string) []detailLine {
	lines := splitLines(strings.TrimRight(content, "\n"))
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first == len(lines) {
		return nil
	}
	body := make([]detailLine, 0, len(lines)-first)
	for _, ln := range lines[first:] {
		body = append(body, detailLine{Text: clipDetail(ln)})
	}
	return body
}

// commitDetail is git_commit's prose half: outputDetail's split with its one-line PROMOTION
// withheld whenever that line carries the hash the slot is about to say (commitHashOf). A commit's
// one-line output is "6fd6ff7 feat: x" and the target leading the row beside it is already
// "feat: x", so promoting it spells the subject twice and leaves the hash — the one fact the header
// does NOT repeat, and what the ratified table gives this tool's slot
// (docs/layout/tool-layout.md) — showing only on a row too narrow to promote. Withheld, the line
// lays out as the body's first row, exactly where the promote-guard would have put it
// (toolView.demoted), and the hash takes the slot at EVERY width.
//
// Refusing the promotion is the promoter's call and not the stat's: a stat never takes the slot
// back from a promoted line (applyStat), and the answer for a line that should not have been
// offered is to not offer it — which leaves the guard's own rule untouched for every tool that
// still has two readings of its outcome.
//
// A result in another shape — "nothing to commit", a hook's message — has no hash for the slot, so
// its promotion stands: that prose floor is all such a block has to say, and blanking the slot to
// enforce a hash that is not there would say less than the tool did.
func commitDetail(content string) toolOutcome {
	out := outputDetail(content)
	if !out.Summary.quoted || out.Summary.Text == "" {
		return out
	}
	if _, ok := commitHashOf(content); !ok {
		return out
	}
	return toolOutcome{Details: []detailLine{out.Summary.detailLine}}
}

// viewDiffBody is view_diff's body hook: the coloured diff read off the result's prose, which is
// where that tool's body lives (diffBody). The result-shaped signature is the registry's
// (toolPresenter.body) — read_file's body is read off its typed summary instead.
func viewDiffBody(res domain.ToolResult) []detailLine {
	return diffBody(res.Content)
}

// readFileBody lays read_file's LOCATE REPORT out beneath the branch: the lines the requested term
// was found on, or the statement that it was found on none — a case only the typed summary can
// tell apart from "no locate was asked for" (domain.ReadSpan's Locate/LocatedOn pair). A read that
// asked for no term has no report and so no body: the file's content belongs to the model, and the
// slot's line count already says how much of it came back.
//
// The numbers are ABSOLUTE and may fall outside the span the row's target names, because the tool
// scans the whole file whatever range the call asked for — that is the point of asking a ranged
// read to locate something.
func readFileBody(res domain.ToolResult) []detailLine {
	v, ok := res.Summary.(domain.ReadSpan)
	if !ok || v.Locate == "" {
		return nil
	}
	if len(v.LocatedOn) == 0 {
		return []detailLine{{Text: clipDetail(fmt.Sprintf("Located %q on no lines", v.Locate))}}
	}
	numbers := make([]string, len(v.LocatedOn))
	for i, n := range v.LocatedOn {
		numbers[i] = strconv.Itoa(n)
	}
	return []detailLine{{Text: clipDetail(fmt.Sprintf("Located %q on lines: %s", v.Locate, strings.Join(numbers, ", ")))}}
}

// diffBody renders view_diff's unified output as the coloured body beneath the row — "+ "
// lines green, "- " lines red, context plain (layout.md, "A change is coloured the one way wherever
// a block shows one"; docs/layout/tool-layout.md's per-tool table). Tagging on the
// leading "+"/"-" is exact here because internal/tools' unifiedLineDiff tags every line "  ",
// "- " or "+ " and emits no "+++ b/…" / "--- a/…" file header, so a content line that itself
// starts with "+" always arrives behind a tag. It returns every line: the collapsed paint's cap
// and its remainder marker are the painter's (collapsedBodyRows, collapsedDetails, render.go).
//
// It counts NOTHING. The "+A −R" diffstat in the outcome slot of the branch above it comes from
// the tool's domain.DiffStat, counted from the diff operations themselves — which is why the
// stat still describes the whole diff when the collapsed paint stops at the cap, and why a "No
// changes detected" result (no diff, hence no stat) never reaches here at all. That last rule is also
// what makes the painter's kind-sniffing exact: a body from here always carries a tagged line.
func diffBody(content string) []detailLine {
	lines := splitLines(strings.TrimRight(content, "\n"))
	body := make([]detailLine, 0, len(lines))
	for _, ln := range lines {
		kind := detailPlain
		switch {
		case strings.HasPrefix(ln, "+"):
			kind = detailDiffAdded
		case strings.HasPrefix(ln, "-"):
			kind = detailDiffRemoved
		}
		body = append(body, detailLine{Kind: kind, Text: clipDetail(ln)})
	}
	return body
}

// editPair is one changed region an edit call ASKS FOR: the lines it removes and the lines it
// puts there instead. It is the shape all three edit tools reduce to — a single find-and-replace
// is one pair, a multi find-and-replace is its ordered list of them, a patch is one per hunk — so
// each tool's extractor has only to say what its own arguments mean and one renderer turns the
// answer into a body (changedLines). Either half may be empty: a pure insertion removes nothing,
// a deletion inserts nothing.
type editPair struct {
	removed  []string
	inserted []string
}

// replacedText builds the pair a find-and-replace argument names, splitting each side into the
// lines it changes (editLines).
func replacedText(removed, inserted string) editPair {
	return editPair{removed: editLines(removed), inserted: editLines(inserted)}
}

// editLines splits one side of an edit into the lines it changes. An empty side has none — there
// is no such thing as removing the empty string — and a single trailing newline is the last line's
// TERMINATOR rather than a line of its own, so a replacement written "a\nb\n" changes two lines and
// not two and a blank. Nothing else is dropped: what a body retains is every line it was given, and
// the compact shape is the painter's business (collapsedBodyRows, render.go).
func editLines(text string) []string {
	if text == "" {
		return nil
	}
	return splitLines(strings.TrimSuffix(text, "\n"))
}

// changedLines renders edit pairs as the display-only diff body an edit block hangs beneath its
// branch: per pair, the removed lines behind "- ", then the inserted lines behind "+ ", pairs in
// the order the call listed them. It is DERIVED FROM THE ARGUMENTS and goes nowhere near the wire
// — no tool result grows, no token is spent, and the model's own view of the call is untouched.
//
// The two tags are the ones diffBody emits, so the lines paint through the very red/green styles
// view_diff's hunks do; the house collapsed cap then holds an
// edit block to the same three rows as every other block (collapsedBodyRows, render.go). It
// truncates nothing — the entry keeps every line — and the per-line clip is the same 160-rune
// guard against a minified blob every other detail line carries.
//
// Pairs with nothing on either side yield NO body at all, which is what lets a call with absent or
// malformed arguments render exactly as it did before: a target, a summary, and nothing beneath.
func changedLines(pairs []editPair) []detailLine {
	n := 0
	for _, p := range pairs {
		n += len(p.removed) + len(p.inserted)
	}
	if n == 0 {
		return nil
	}
	body := make([]detailLine, 0, n)
	for _, p := range pairs {
		body = appendTagged(body, p.removed, "- ", detailDiffRemoved)
		body = appendTagged(body, p.inserted, "+ ", detailDiffAdded)
	}
	return body
}

// appendTagged appends one side of a pair, one detail line per line of text, each behind its diff
// tag and clipped like any other detail line.
func appendTagged(body []detailLine, lines []string, tag string, kind detailKind) []detailLine {
	for _, ln := range lines {
		body = append(body, detailLine{Kind: kind, Text: clipDetail(tag + ln)})
	}
	return body
}

// The marker column of the stacked reading: two cells, the same width in every row, carrying `-` on
// a removed line, `+` on an inserted one and nothing on context (docs/layout/split-diff-layout.md).
// The glyphs are the change's palette-proof signal — colour never carries it alone (ratified call 6)
// — and they are the very tags the argument-derived body already writes (changedLines), so the two
// bodies an edit block can show read as one thing.
const (
	stackedRemovedMarker  = "- "
	stackedInsertedMarker = "+ "
	stackedContextMarker  = "  "
)

// stackedRegionRuleCells is how wide the damped `⋯` rule between two regions is drawn. The rule
// stands for the lines elided between them, and it is a fixed short run rather than one spanning
// the body: these are detail lines, built with no width in hand — the block's width is the
// painter's, settled at paint time — and a run long enough to fill a wide block would wrap onto a
// second row in a narrow one, which is a rule that reads as two.
const stackedRegionRuleCells = 8

// stackedDiffLines renders recorded Edit regions as the STACKED reading of a diff body: per region
// its leading context, its removed lines behind `-` at their before-file numbers, its inserted
// lines behind `+` at their after-file numbers, then its trailing context — the layout
// docs/layout/split-diff-layout.md sketches, and the reading a body falls back to at every width
// the split panes do not fit (ratified call 5).
//
// It is the ONE builder of those rows. Every block that has regions renders through it — the three
// edit tools, whose tools record them at apply time, and the two diff tools, whose renderers
// recover them — so the narrow reading of a diff cannot come to differ per tool.
//
// The number gutter is sized once for the whole body and right-aligned, so the numbers line up
// down the block however far apart the regions are. Context rows carry the BEFORE file's number:
// the column then reads as one file's numbering, with the inserted lines — which have no before
// line at all — marked as the exceptions they are. Wrapping is nobody's business here: a row too
// wide for the block wraps at paint time through the same machinery every other detail line does
// (hangingWrap), and the per-line clip is the 160-rune ceiling they all answer to (clipDetail).
//
// No regions is no body, which is what leaves a call with nothing recorded showing the
// argument-derived lines it was presented with (ratified call 9).
func stackedDiffLines(regions []domain.EditRegion) []detailLine {
	rows := stackedRows(regions)
	if len(rows) == 0 {
		return nil
	}
	gutter := stackedGutter(rows)
	out := make([]detailLine, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.line(gutter))
	}
	return out
}

// stackedRow is one row of the stacked reading before the body's gutter width is known: the line
// number it shows, the marker column it wears, the colour its kind gives it, and its text. A row
// with number 0 shows no number and no marker — the `⋯` rule between two regions is the only such
// row — which is what lets the sizing pass below ignore it without a second shape.
type stackedRow struct {
	number int
	marker string
	kind   detailKind
	text   string
}

// line is the row as a detail line, its number right-aligned into a gutter of the given width. The
// composed text is clipped like every other detail line, and clipping the whole row rather than its
// text alone is deliberate: the cut takes the tail, so the number and the marker always survive it.
func (r stackedRow) line(gutter int) detailLine {
	if r.number == 0 {
		return detailLine{Kind: r.kind, Text: r.text}
	}
	return detailLine{Kind: r.kind, Text: clipDetail(fmt.Sprintf("%*d %s%s", gutter, r.number, r.marker, r.text))}
}

// stackedRows lays the regions out as unsized rows, in file order, with the `⋯` rule laid between
// two regions that do NOT meet in the file's numbering (regionsMeet). Regions that DO meet are
// painted end to end with nothing between them: a tool records neighbouring changes as separate
// regions whose context tiles the lines between them without overlap (domain.EditRegion), so the
// rows already run continuously and a rule there would claim an elision that did not happen.
func stackedRows(regions []domain.EditRegion) []stackedRow {
	rows := make([]stackedRow, 0, len(regions)*4)
	for i, region := range regions {
		if i > 0 && !regionsMeet(regions[i-1], region) {
			rows = append(rows, stackedRow{text: strings.Repeat(glyphLeaderDot, stackedRegionRuleCells)})
		}
		before, after := region.BeforeStart, region.AfterStart
		for _, text := range region.Leading {
			rows = append(rows, stackedRow{number: before, marker: stackedContextMarker, text: text})
			before, after = before+1, after+1
		}
		for _, text := range region.Removed {
			rows = append(rows, stackedRow{number: before, marker: stackedRemovedMarker, kind: detailDiffRemoved, text: text})
			before++
		}
		for _, text := range region.Inserted {
			rows = append(rows, stackedRow{number: after, marker: stackedInsertedMarker, kind: detailDiffAdded, text: text})
			after++
		}
		for _, text := range region.Trailing {
			rows = append(rows, stackedRow{number: before, marker: stackedContextMarker, text: text})
			before++
		}
	}
	return rows
}

// regionsMeet reports whether the later of two regions starts on the very line the earlier one
// ends: its BeforeStart against the earlier region's own span in the before file, which is its
// context and its removed lines (an inserted line occupies no line of that file).
//
// It is the elision question and nothing else. Two regions that meet were cut apart only because
// each change gets its own record; painted end to end they read exactly as one region would, which
// is what the tiling rule was chosen to make true (domain.EditRegion).
func regionsMeet(prev, next domain.EditRegion) bool {
	return next.BeforeStart == prev.BeforeStart+len(prev.Leading)+len(prev.Removed)+len(prev.Trailing)
}

// stackedGutter is how many cells the body's number column takes: the digits of the widest number
// any of its rows shows. One width for the whole body is what makes the numbers a column rather
// than a ragged edge, and it is measured over the rows themselves so a region whose after-file
// numbers have drifted past its before-file ones widens it like any other.
func stackedGutter(rows []stackedRow) int {
	widest := 0
	for _, row := range rows {
		if row.number > widest {
			widest = row.number
		}
	}
	return len(strconv.Itoa(widest))
}

// singleReplacementBody derives single_find_and_replace's changed lines from its own arguments:
// the one oldText → newText pair the call asks for.
func singleReplacementBody(args map[string]any) []detailLine {
	removed, _ := args["oldText"].(string)
	inserted, _ := args["newText"].(string)
	return changedLines([]editPair{replacedText(removed, inserted)})
}

// multiReplacementBody derives multi_find_and_replace's changed lines: one pair per entry of the
// replacements array, in the order the tool applies them (sequentially, internal/tools), so the
// body reads in the order the edit happens. An entry that is not an object is skipped rather than
// guessed at — a malformed argument shows fewer pairs, never a wrong one.
func multiReplacementBody(args map[string]any) []detailLine {
	list, ok := args["replacements"].([]any)
	if !ok {
		return nil
	}
	pairs := make([]editPair, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		removed, _ := m["oldText"].(string)
		inserted, _ := m["newText"].(string)
		pairs = append(pairs, replacedText(removed, inserted))
	}
	return changedLines(pairs)
}

// fileEditBody derives edit_existing_file's changed lines from its content argument, which the
// tool reads in one of two ways and so does this: a "*** Begin Patch" block is a list of hunks,
// each of them a pair (patchEditPairs), and anything else is full replacement content — one pair
// that removes nothing and inserts the lot, which is exactly what the call does to the file.
func fileEditBody(args map[string]any) []detailLine {
	content, ok := args["content"].(string)
	if !ok {
		return nil
	}
	if isPatchArgument(content) {
		return changedLines(patchEditPairs(content))
	}
	return changedLines([]editPair{replacedText("", content)})
}

// patchOpener matches the "*** Begin Patch" marker edit_existing_file's patch form opens with,
// with the same tolerance for case and spacing the tool's own parser has (internal/tools,
// file_edit.go, which is the format's authority). The view reads the format rather than importing
// the parser: it needs the changed LINES, not the applier's hunks, and a patch it failed to
// recognise degrades to a body of "+ " lines rather than to anything untrue.
var patchOpener = regexp.MustCompile(`(?i)^\*{3}\s*Begin\s+Patch`)

// isPatchArgument reports whether an edit_existing_file content argument is a patch rather than
// full replacement content.
func isPatchArgument(content string) bool {
	return patchOpener.MatchString(strings.TrimLeft(content, " \t\r\n"))
}

// patchEditPairs reads a patch's hunks as edit pairs, one per "@@" header: within a hunk a "-"
// line is removed and a "+" line inserted. A CONTEXT line is neither — it is there so the applier
// can find the place — and a block showing what CHANGED has nothing to say about it. Begin/End/File
// markers and anything before the first hunk fall out for free, since none of them opens with a
// tag.
func patchEditPairs(content string) []editPair {
	var (
		pairs   []editPair
		current editPair
		inHunk  bool
	)
	flush := func() {
		if len(current.removed) > 0 || len(current.inserted) > 0 {
			pairs = append(pairs, current)
		}
		current = editPair{}
	}
	for _, ln := range splitLines(content) {
		if strings.HasPrefix(ln, "@@") {
			flush()
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(ln, "-"):
			current.removed = append(current.removed, ln[1:])
		case strings.HasPrefix(ln, "+"):
			current.inserted = append(current.inserted, ln[1:])
		}
	}
	flush()
	return pairs
}

// writtenLines derives write_file's body from the content the call asks to be written: every line
// of it behind "+ ", since a write puts all of them in the file and takes nothing out — the same
// pair shape and the same renderer an edit's full-replacement form uses (changedLines), so a write
// and an edit that say the same thing read the same way.
//
// The outcome slot on the branch above it says "N lines" instead (writtenLinesStat, the ratified
// table's wording): the slot says how much was written and the body says what, and neither is
// derived from the other — both read the same content argument. Content that is
// absent, empty or of the wrong type yields no body — an empty file is a write with nothing to
// show, not a body of one blank line.
func writtenLines(args map[string]any) []detailLine {
	content, ok := args["content"].(string)
	if !ok {
		return nil
	}
	return changedLines([]editPair{replacedText("", content)})
}

// detailClipRunes caps one detail/target line so a minified blob or a wall-of-text report cannot
// flood the transcript (the renderer soft-wraps, so an uncapped line becomes many rows).
//
// The cap is a FLOOD bound and it is deliberately spent in RUNES, not in the cells the screen
// bills. No rune paints more than two cells, so 160 runes buy at most 320 cells and therefore at
// most twice the rows the same 160 runes of ASCII take — a wall of double-width text costs scroll,
// never content. Cell-exactness is the STATUS LINE's requirement, not the transcript's: that row is
// shared with the context gauge, so an over-wide left slot pushes something the reader needs off the
// screen — which is why that row carries the tool's verb alone now (toolActivityVerb, activity.go)
// rather than a target it would have to cap in cells through the width authority. The
// transcript shares nothing — a wide line wraps onto rows of its own and the block behind it paints
// lower down, whole. TestPaintedWideDetailLineWrapsWithoutDisplacement (paint_test.go) is the probe
// that measured all three of those claims and the pin that keeps them true.
const detailClipRunes = 160

// clipDetail truncates s to detailClipRunes runes with an ellipsis.
func clipDetail(s string) string {
	return clipRunes(s, detailClipRunes)
}

// clipRunes truncates s to n runes with an ellipsis, counting runes rather than bytes so a
// multi-byte path is not cut mid-character. Its callers are clipDetail and the approval pane's
// Sub-agent line (approvalTaskClipRunes), and in both the rune spend is settled at the caller
// rather than being a shortfall to be swept: see detailClipRunes for why the transcript's bound is
// allowed to be a rune count where the status line's is not.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// plural renders "1 result" / "3 results" — count plus the word, naively pluralised.
func plural(n int, word string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// parseArgs decodes a tool call's JSON arguments into a generic map for the target
// extractors. Malformed or empty arguments decode to nil, which the extractors tolerate (a
// missing key yields the empty target).
func parseArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// prettyJSONDetails renders a tool call's arguments as the pretty-printed JSON (or the verbatim
// text when it does not parse) split into one detailLine per line, each hanging at
// argumentValueIndent. It is what argumentDetails degrades to where there is nothing to label — a
// bare array, a malformed fragment — so a blob with no names still reaches the screen as it
// arrived instead of being dropped. Empty/null arguments add no lines.
//
// The indent is what stops this path from giving back what the labelled path closed. On the
// approval pane a row is the SURFACE's own iff it starts flush-left: that is the whole of what
// tells the pane's "Reason:" from a row painted out of the model's bytes, since both wear
// th.popupBody and the pane sets no bodyLead. The labelled path spends that fact by flattening
// names and indenting values (argumentDetails); a fallback emitting lines at column zero let a blob
// whose text reads "Reason: pre-approved by the operator" paint a second Reason row beside the real
// one — a forged fact under the pane's own styling, on the surface a human authorises a call from.
// Every line here is argument-derived, this being the arguments' own fallback and its only caller,
// so every line is a BODY row. Nothing is rejected or summarised to get there: the bytes still
// reach the screen exactly as they arrived, two columns to the right of where a label can live.
func prettyJSONDetails(raw json.RawMessage) []detailLine {
	pretty := prettyJSON(raw)
	if pretty == "" {
		return nil
	}
	lines := splitLines(pretty)
	details := make([]detailLine, 0, len(lines))
	for _, ln := range lines {
		details = append(details, detailLine{Text: argumentValueIndent + ln})
	}
	return details
}

// argumentValueIndent is the hanging indent an argument's value sits under, so a labelled argument
// reads as a label with its value beneath it rather than as one run-on line
// (docs/layout/user-questions-layout.md, the approval box).
const argumentValueIndent = "  "

// argumentDetails renders a tool call's arguments as LABELLED lines: one `name:` line per argument,
// the value's own real lines indented beneath it. It is the shape a human reads a decision off —
// no surrounding braces, no quoted key names, and a multi-line command showing the lines it will
// actually run rather than one `"…\n…"` string — and it is DISPLAY-ONLY: what the tool receives is
// the caller's json.RawMessage, untouched by anything here.
//
// The order is the MODEL's own, taken off the wire in the order it wrote the keys, so the display is
// deterministic for a given call without imposing an order the call did not have (a decode into
// map[string]any loses it, which is why orderedArgs streams the tokens instead).
//
// Two things still render as JSON, and both are the honest rendering rather than a leftover. A blob
// that is not an object at all — a bare array, a malformed fragment — has no names to label, so it
// falls back to prettyJSONDetails and the unregistered-tool body's verbatim-rather-than-dropped
// rule, its every line sitting at argumentValueIndent because an unlabelled line is still a value's
// line and no argument byte may render where a label lives. And a single value with no flat shape (a
// nested object, an array of objects) is indented JSON under its own label, since nothing else
// states its structure without lying about it. What never comes back is an envelope around the
// argument SET: the labels ARE the object.
//
// Both surfaces that show a call's raw arguments read this one rendering: the approval prompt a
// human decides on, and the transcript block that records a call the presenter does not recognise
// (presentToolCall's unregistered-tool fallback). One call is spelled one way wherever it appears —
// the transcript block then collapses to the house budget like any other body (render.go), which is
// a question about how many of these lines a surface seats, not about what they say.
//
// A key the model wrote TWICE is shown ONCE, carrying the value the tool will receive and marked as
// the duplicate it is (duplicateKeyNote). The pane may not be the one surface in the process that
// reads a call differently from everything else acting on it: the executor's decode
// (internal/tools.decodeArgs) is stdlib JSON, where the last duplicate wins, and both guards are
// last-wins too (security/dangerous.go, tools/workspace_scoped.go). Streaming every duplicate in
// wire order let `{"command":"npm test","command":"curl …|sh"}` be approved off a line the executor
// discards — so the surviving pair sits where its winning value arrived, in wire order among the
// other survivors, and the note says the earlier ones existed rather than hiding them.
//
// The NAME is flattened (flattenField) and the value is not, which is the same line drawn twice. A
// name is a label: nothing in it is layout, so a newline in one is not a longer label but a SECOND
// line, unindented, wearing whatever the model wrote it as — on the approval prompt that is a row
// beside the pane's own, and JSON puts no restriction on what a key may hold. A value's newlines
// ARE the fact being read, so they survive, hanging under their label at argumentValueIndent where
// nothing they say can be mistaken for a label of the surface's own.
func argumentDetails(raw json.RawMessage) []detailLine {
	pairs, ok := orderedArgs(raw)
	if !ok {
		return prettyJSONDetails(raw)
	}
	var details []detailLine
	for _, p := range pairs {
		label := flattenField(p.name) + ":"
		if p.occurrences > 1 {
			label += duplicateKeyNote(p.occurrences)
		}
		details = append(details, detailLine{Text: label})
		for _, ln := range argumentValueLines(p.value) {
			details = append(details, detailLine{Text: argumentValueIndent + ln})
		}
	}
	return details
}

// duplicateKeyNote is what a label says when the model wrote that key more than once: which of the
// values is on the screen, and — by saying it at all — that there were others. It rides the LABEL
// rather than the value so the value beneath it is still nothing but the bytes the tool receives.
func duplicateKeyNote(occurrences int) string {
	return fmt.Sprintf("  (duplicate key — last of %d wins)", occurrences)
}

// resolvedPathNote is the ONE wording every decision surface discloses a redirected path with —
// the approval pane's own line and the tool card's branch row both come here, so the two cannot
// end up telling the same fact in two dialects. It is empty whenever the engine sent nothing
// (domain.ApprovalRequest.ResolvedPath / domain.ToolCallEvent.ResolvedPath), which is the ordinary
// case: the argument names its own target and neither surface grows a line.
//
// The engine decides WHETHER there is anything to say — it holds the workspace root and the
// resolution the gate judged the call by — and this decides how it reads. That split is what keeps
// the pane from computing a second opinion about a path off arguments it would have to re-resolve
// on the render goroutine.
//
// The path is model-authored like every other field these surfaces paint: it is what the model's
// own argument resolved to, so it is escape-stripped and FLATTENED here rather than at each call
// site. Flattening is what makes it safe to hand the approval pane, which paints one row per line
// and would otherwise let a path carrying "\n" write rows of its own beneath a label it did not
// author.
func resolvedPathNote(resolved string) string {
	if resolved == "" {
		return ""
	}
	return "→ resolves to " + flattenField(stripEscapes(resolved))
}

// argumentPair is one argument as the model wrote it: its name, and its value still encoded, so the
// value's own rendering (argumentValueLines) decides what shape it takes on the screen.
// occurrences is how many times that name appeared in the call — 1 for an ordinary argument, more
// where the model repeated a key and this pair carries the last value it wrote (orderedArgs).
type argumentPair struct {
	name        string
	value       json.RawMessage
	occurrences int
}

// orderedArgs decodes a tool call's arguments into name/value pairs in WIRE order, reporting false
// when there is nothing to label — absent or null arguments, a top-level value that is not an
// object, a blob that does not parse, or one carrying anything after its closing brace. Every false
// leaves the caller to show the arguments as they arrived: half a labelled rendering of a malformed
// blob would be a claim about the call that the bytes do not support.
//
// A repeated key yields ONE pair, carrying the LAST value the model wrote for it and counting the
// occurrences, because that is the value everything downstream acts on (argumentDetails states the
// rule and why the pane may not differ from it).
func orderedArgs(raw json.RawMessage) ([]argumentPair, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	open, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if delim, isDelim := open.(json.Delim); !isDelim || delim != '{' {
		return nil, false
	}
	var pairs []argumentPair
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, isString := key.(string)
		if !isString {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		pairs = append(pairs, argumentPair{name: name, value: value})
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, false
	}
	// The stream must END there. Asking for one more token is what says so for EVERY tail — a second
	// document behind the first, loose text, and the stray `}`/`]` that dec.More() reads as "no more
	// input" rather than as the garbage it is.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return lastWins(pairs), true
}

// lastWins collapses repeated keys the way every consumer of the same bytes does: one pair per
// name, holding the value of its LAST occurrence and standing at that occurrence's place among the
// survivors, with the occurrence count carried through so the label can say the key was repeated.
func lastWins(pairs []argumentPair) []argumentPair {
	occurrences := make(map[string]int, len(pairs))
	last := make(map[string]int, len(pairs))
	for i, p := range pairs {
		occurrences[p.name]++
		last[p.name] = i
	}
	out := make([]argumentPair, 0, len(occurrences))
	for i, p := range pairs {
		if last[p.name] != i {
			continue
		}
		p.occurrences = occurrences[p.name]
		out = append(out, p)
	}
	return out
}

// argumentValueMaxLines is the most lines ONE argument's value may spend on the surfaces that show
// a call's arguments. It exists so no single value can evict its siblings: the approval pane's body
// budget is a handful of rows on a stock 80×24 window (popupBudget), so an uncapped two-hundred-line
// `content` took every row the pane had and the `path:` it was being written to never reached the
// screen. Eight is long enough to read a command or a short file off, short enough that a two- or
// three-argument call still shows every label it has.
const argumentValueMaxLines = 8

// argumentValueLines renders one argument's value as the lines that sit under its label: a string as
// its OWN lines, so the newline a JSON blob spells `\n` is a line break here; any other scalar as
// the literal the model sent (a `null` says null rather than going quiet, which is why only a
// decoded STRING takes the first exit); and a value with no flat shape as indented JSON.
//
// It wraps nothing — how WIDE these lines may be is the surface's own business — but it does bound
// how MANY there are (argumentValueMaxLines), and an elided value keeps its TAIL as well as its
// head: head lines, the elision marker counting what is not shown, then the value's LAST line
// (elisionSplit, popup.go, is the shared rule, and popupElisionMarker the one wording for the fact).
// A value's last line is where a payload appended to an innocent-looking body lives, and a surface
// that shows only heads is one an approval can be given on falsely.
func argumentValueLines(value json.RawMessage) []string {
	return elideValueLines(decodedValueLines(value))
}

// decodedValueLines is argumentValueLines' rendering before its cap: the value's real lines, however
// many it has.
func decodedValueLines(value json.RawMessage) []string {
	var decoded any
	if err := json.Unmarshal(value, &decoded); err == nil {
		if s, isString := decoded.(string); isString {
			return splitLines(s)
		}
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, value, "", "  "); err != nil {
		return splitLines(strings.TrimSpace(string(value)))
	}
	return splitLines(buf.String())
}

// elideValueLines seats lines in argumentValueMaxLines rows, head + marker + tail (elisionSplit),
// and returns a short-enough value untouched.
func elideValueLines(lines []string) []string {
	head, tail, hidden := elisionSplit(len(lines), argumentValueMaxLines)
	if hidden == 0 {
		return lines
	}
	out := make([]string, 0, head+1+tail)
	out = append(out, lines[:head]...)
	out = append(out, popupElisionMarker(hidden))
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

// firstLine returns the first line of s (without its newline), or s when it has none.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// splitLines splits s on newlines into its physical lines.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
