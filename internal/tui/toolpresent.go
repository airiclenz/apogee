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
// the header line (✦ Read File), the target that leads the branch beneath it, and the one-line
// summary that follows the target on that branch (┕ main.go 1 - 100). It is pure — no lipgloss,
// no I/O — so it is trivially table-testable (TestPresentToolCall); render.go owns the styling
// and the block shape.
//
// What lives here is PRESENTATION vocabulary: labels, verbs, targets, and the wording of the
// one-line outcome. What does NOT live here is the outcome's facts. A tool that computed
// something worth showing reports it as a typed domain.ToolSummary beside the prose Content
// (internal/tools), and summaryLine below renders that value. The view never parses a result
// string to find out what a tool did: prose written for the MODEL is not an interface, and
// re-deriving facts from it meant a wording change in internal/tools silently degraded a card
// with no compiler nudge and no failing test in the package that changed.
//
// The two halves stay independent. The wording is the view's own — that several of these
// lines read like the tool's own header today is what makes the change checkable, not a
// contract — and a result carrying NO summary still renders from prose exactly as before:
// the registry's detail extractor quotes a fixed first line, or hands free-form output on as
// the block's body (which the collapsed paint compresses to a first line plus a remainder
// count). Quoting or compressing is rendering; re-deriving a number from a sentence was not.
//
// The label+extractor map is an OPEN, name-keyed registry, not a closed switch: the Phase-3
// tool fan-out (P3.7–P3.11, ~30 tools, ADR 0002) adds one entry per tool (terminal→"Run",
// git→"Git", find_replace→"Edit File", …) rather than editing a control-flow statement. An
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
// business but that of the [toolView] field holding it: the Summary rides the branch line
// beside the target, a Details line lays out beneath it (render.go owns that shape).
type detailLine struct {
	Kind detailKind
	Text string
}

// branchSummary is the one-line outcome riding the branch line beside the target, bound to the one
// fact the shortening seam depends on: WHOSE words it is. Most summaries are the presenter's own —
// a typed phrase worded by summaryLine ("1 - 154", "+2 -2"), a tool's own report sentence
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
// directory, a pattern), and the outcome split in two — the one-line Summary that rides the
// branch line beside the target ("1 - 154", "+2 -2", "error: …") and the Details body laid
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

	// solo marks a call that must never be folded into a grouped block, however well it matches its
	// neighbours (groupable, render.go). Grouping's own rule is about the SHAPE of a call — a target
	// to lead an aligned member row — and says nothing about what the block MEANS; solo is where a
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

// toolOutcome is what a prose extractor returns: the one-line Summary that rides the branch
// line beside the target, and the Details body laid out beneath it. Either half may be empty
// — a fixed result sentence is summary-only ("HTTP 200 OK") and multi-line free-form output is
// body-only (every one of its lines). A tool whose result carries a domain.ToolSummary
// does not come through here at all: summaryLine words the branch line and the presenter's
// body renderer (view_diff's alone) fills the half beneath it. An edit's body — and a write's —
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
}

// summaryOnly is the outcome of a tool whose whole result is one plain line in the PRESENTER's
// wording — a fixed sentence, a phrase it composed: it rides the branch line beside the target,
// nothing hangs beneath it, and the shortening seam spells any path it names relative to the
// workspace.
func summaryOnly(text string) toolOutcome {
	return toolOutcome{Summary: namedSummary(detailLine{Text: text})}
}

// promotedOutput is the outcome of a tool whose one-line result is text it did not WORD — a
// command's whole output when that came to one line (outputDetail), the answer a human typed into
// an ask_user question (quotedFirstLineDetail): the line is promoted into the summary slot and
// rides the branch beside the target. Promotion moves where the text sits and changes nothing about
// whose text it is — it is quoted, so it is marked as such and reaches the screen with the spelling
// it was written with (branchSummary).
func promotedOutput(text string) toolOutcome {
	return toolOutcome{Summary: quotedSummary(detailLine{Text: text})}
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
// header line ("Read File"), verb is the lowercase present participle the live status line
// reads as a sentence fragment ("reading main.go") — never a title.
type toolPresenter struct {
	label  string
	verb   string
	target func(args map[string]any) string

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

	// body renders the lines laid out BENEATH the branch when the summary supplied the
	// branch line itself. Exactly one entry sets it — view_diff, the only tool whose body is
	// read off its RESULT — so the asymmetry is intentional, not an oversight. A body derived
	// from what the call ASKED for is argBody's instead.
	body func(content string) []detailLine

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
// Every detail extractor here renders PROSE. The seven tools that report a typed summary
// (read_file, write_file, list_dir, grep, view_diff, web_search, open_file) get their branch
// line from summaryLine instead, and keep firstLineDetail as the floor for a result that
// carries none — a degraded card is that tool's own first line, never a file dumped into the
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
		label:  "Read File",
		verb:   "reading",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the span comes from domain.ReadSpan
	},
	"write_file": {
		label:   "Write File",
		verb:    "writing",
		target:  stringArg("path"),
		detail:  firstLineDetail, // floor; the count comes from domain.WroteBytes
		argBody: writtenLines,    // the content the call writes, as + lines
	},
	"list_dir": {
		label:  "List Dir",
		verb:   "listing",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the count comes from domain.ListedEntries
	},
	"grep": {
		label:  "Search",
		verb:   "searching",
		target: stringArg("pattern"),
		detail: firstLineDetail, // floor; the count comes from domain.MatchedLines
	},
	"find_files": {
		label:  "Find Files",
		verb:   "finding",
		target: stringArg("pattern"),
		detail: firstLineDetail, // the tool's own header: "[N files found, showing …]"
	},
	"single_find_and_replace": {
		label:   "Edit File",
		verb:    "editing",
		target:  stringArg("path"),
		detail:  firstLineDetail,       // "replaced text in <path>"
		argBody: singleReplacementBody, // the one oldText → newText pair, as -/+ lines
	},
	"multi_find_and_replace": {
		label:   "Edit File",
		verb:    "editing",
		target:  stringArg("path"),
		detail:  firstLineDetail,      // "applied N replacements to <path>"
		argBody: multiReplacementBody, // one -/+ pair per replacement, in argument order
	},
	"edit_existing_file": {
		label:   "Edit File",
		verb:    "editing",
		target:  stringArg("path"),
		detail:  firstLineDetail, // "applied patch to <path> (N hunks)" / "updated <path>"
		argBody: fileEditBody,    // a patch's hunks, or full replacement content as + lines
	},
	"view_diff": {
		label:  "View Diff",
		verb:   "diffing",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the "No changes detected" sentinel renders here too
		body:   diffBody,        // the coloured diff beneath a domain.DiffStat branch line
	},
	"open_file": {
		label:  "Open File",
		verb:   "opening",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the locate report comes from domain.OpenedFile
	},
	"copy_file": {
		label:  "Copy File",
		verb:   "copying",
		target: sourceDestinationTarget,
		detail: firstLineDetail, // "copied a.txt to b.txt"
	},
	"move_file": {
		label:  "Move File",
		verb:   "moving",
		target: sourceDestinationTarget,
		detail: firstLineDetail, // "moved a.txt to b.txt"
	},
	"delete_file": {
		label:  "Delete File",
		verb:   "deleting",
		target: stringArg("path"),
		detail: firstLineDetail, // "deleted a.txt"
	},
	"terminal": {
		label:  "Run",
		verb:   "running",
		target: stringArg("command"),
		detail: outputDetail,
	},
	"python_exec": {
		label:  "Run Python",
		verb:   "running python",
		target: firstLineArg("code"),
		detail: outputDetail,
	},
	"git_branch": {
		label:  "Git Branch",
		verb:   "branching",
		target: joinedArgs("action", "name"),
		detail: outputDetail, // a branch list is multi-line; create/switch is one line
	},
	"git_commit": {
		label:  "Git Commit",
		verb:   "committing",
		target: firstLineArg("message"),
		detail: outputDetail, // "[main abc1234] subject" + the diffstat lines
	},
	"git_diff_range": {
		label:  "Git Diff",
		verb:   "diffing",
		target: refRangeTarget,
		detail: outputDetail,
	},
	"git_status": {
		label: "Git Status",
		verb:  "checking",
		// No target: the tool takes no arguments — the repository IS the target.
		detail: outputDetail, // the branch line plus the staged/unstaged/untracked sections
	},
	"git_log": {
		label:  "Git Log",
		verb:   "reading",
		target: gitLogTarget,
		detail: outputDetail, // one line per commit
	},
	"diagnostics": {
		label:  "Diagnostics",
		verb:   "checking",
		target: stringArg("path"),
		detail: outputDetail,
	},
	"run_tests": {
		label: "Run Tests",
		verb:  "running tests",
		// Both arguments are optional and the common call carries neither — the whole suite is
		// the target then, and joinedArgs renders the empty target a bare "running tests" needs.
		target: joinedArgs("path", "filter"),
		detail: firstLineDetail, // the tool's own verdict line: "PASS (go test)" / "FAIL …"
	},
	"web_fetch": {
		label:  "Web Fetch",
		verb:   "fetching",
		target: stringArg("url"),
		detail: firstLineDetail, // "HTTP 200 OK" — the body never floods the chat
	},
	"http_request": {
		label:  "HTTP Request",
		verb:   "requesting",
		target: methodURLTarget,
		detail: firstLineDetail, // "HTTP 200 OK"
	},
	"web_search": {
		label:  "Web Search",
		verb:   "searching the web",
		target: stringArg("query"),
		detail: firstLineDetail, // floor; a hit count comes from domain.SearchHits
	},
	"sub_agent": {
		label:  "Sub-Agent",
		verb:   "delegating",
		target: subAgentTarget, // the delegation's name when it was given one, else the task's first line
		detail: outputDetail,   // the report's gist; the nested run already rendered railed
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
		target: stringArg("path"),
		detail: firstLineDetail, // "Presented <path>: opened on the user's machine."
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
func presentToolCall(call domain.ToolCall, ws workspaceRoot) toolView {
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
	if call.Tool == subAgentToolName {
		tv.agentName = subAgentName(args)
	}
	if p.target != nil {
		tv.Target = p.target(args)
	}
	if p.argBody != nil {
		tv.Details = newToolBody(p.argBody(args))
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

// shortenPaths spells the paths the view NAMES relative to the workspace — the target that leads
// the branch line, and the one-line summary beside it when that summary is the block's OWN wording
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
}

// sanitize escape-strips every DISPLAY field of the view — label, verb, target, the one-line
// summary and each detail line — so no ESC byte from a tool call or its result can reach the
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
	tv.Summary.Text = stripEscapes(tv.Summary.Text)
	tv.Details.stripEscapes()
}

// enrichWithResult folds a tool's result into the view, in four layers. An error result
// (the tool flagged it IsError — a normal in-band outcome the model reacts to) is the
// one-line summary, so an errored call still groups with its neighbours. A result carrying a
// typed domain.ToolSummary is worded by summaryLine, with the presenter's body renderer
// filling the half beneath it (view_diff alone). Everything else falls to prose, through one of
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
		tv.Summary = namedSummary(detailLine{Text: errorSummaryPrefix + firstLine(result.Content)})
		return
	}
	p, known := toolRegistry[tv.name]
	if line, ok := summaryLine(result.Summary); ok {
		tv.Summary = namedSummary(line)
		if known && p.body != nil {
			tv.Details = tv.Details.with(p.body(result.Content))
		}
		return
	}
	if known && p.outcome != nil {
		out := p.outcome(tv.args, result.Content)
		tv.Summary = out.Summary
		tv.Details = tv.Details.with(out.Details)
		tv.solo = tv.solo || out.Solo
		return
	}
	if known && p.detail != nil {
		out := p.detail(result.Content)
		tv.Summary = out.Summary
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

// summaryLine words a tool's structured outcome as the one-line summary that rides the
// branch beside the target. It is the view's ONE switch over domain.ToolSummary, and ok is
// false for a nil summary and for a variant this view has no line for — either way the caller
// falls through to the prose path, so a tool that reports nothing structured (and a variant
// added before this view knows what to say about it) renders exactly as it always did.
//
// The wording is the VIEW's. A summary carries numbers; what a card says about them is
// presentation, and this file may reword any line here without touching internal/tools.
func summaryLine(s domain.ToolSummary) (detailLine, bool) {
	switch v := s.(type) {
	case domain.ReadSpan:
		return detailLine{Text: strconv.Itoa(v.Start) + " - " + strconv.Itoa(v.End)}, true
	case domain.WroteBytes:
		return detailLine{Text: "+" + strconv.Itoa(v.Bytes) + " bytes"}, true
	case domain.ListedEntries:
		// "entries" and "matches" are FIXED plurals, deliberately not plural(): the card has
		// always read "1 entries", and plural() would both change that and render "matchs".
		return detailLine{Text: strconv.Itoa(v.Total) + " entries"}, true
	case domain.MatchedLines:
		return detailLine{Text: strconv.Itoa(v.Total) + " matches"}, true
	case domain.DiffStat:
		return detailLine{Text: "+" + strconv.Itoa(v.Added) + " -" + strconv.Itoa(v.Removed)}, true
	case domain.SearchHits:
		return detailLine{Text: plural(v.Count, "result")}, true
	case domain.OpenedFile:
		return detailLine{Text: openedFileLine(v)}, true
	}
	return detailLine{}, false
}

// openedFileLine words open_file's outcome: the locate report when a term was requested —
// including the "on no lines" case, which only the typed summary can tell apart from "no
// locate was asked for" — and otherwise the body's line count, since the file's content
// belongs to the model and the header would only repeat the target.
func openedFileLine(v domain.OpenedFile) string {
	if v.Locate == "" {
		return plural(v.Lines, "line")
	}
	if len(v.LocatedOn) == 0 {
		return clipDetail(fmt.Sprintf("Located %q on no lines", v.Locate))
	}
	numbers := make([]string, len(v.LocatedOn))
	for i, n := range v.LocatedOn {
		numbers[i] = strconv.Itoa(n)
	}
	return clipDetail(fmt.Sprintf("Located %q on lines: %s", v.Locate, strings.Join(numbers, ", ")))
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
func quotedFirstLineDetail(content string) toolOutcome {
	return promotedOutput(clipDetail(firstLine(content)))
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
// calls — and now that a Run and its output group like anything else, the exclusion has to be said
// rather than inherited.
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
// rides the branch beside the target ("┕ true (no output)"), which is also what keeps such
// calls grouping; output with more to say is a body and lays out beneath the target instead,
// because two lines cannot share a branch (layout.md's Run sketch).
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
	lines := splitLines(strings.TrimRight(content, "\n"))
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first == len(lines) {
		return summaryOnly("(no output)")
	}
	body := lines[first:]
	if len(body) == 1 {
		return promotedOutput(clipDetail(body[0]))
	}
	details := make([]detailLine, 0, len(body))
	for _, ln := range body {
		details = append(details, detailLine{Text: clipDetail(ln)})
	}
	return toolOutcome{Details: details}
}

// diffBody renders view_diff's unified output as the coloured body beneath the branch — "+ "
// lines green, "- " lines red, context plain (layout.md's Update File sketch). Tagging on the
// leading "+"/"-" is exact here because internal/tools' unifiedLineDiff tags every line "  ",
// "- " or "+ " and emits no "+++ b/…" / "--- a/…" file header, so a content line that itself
// starts with "+" always arrives behind a tag. It returns every line: the collapsed paint's cap
// and its remainder marker are the painter's (collapsedBodyRows, collapsedDetails, render.go).
//
// It counts NOTHING. The "+A -R" diffstat riding the branch above it comes from the tool's
// domain.DiffStat, counted from the diff operations themselves — which is why the stat still
// describes the whole diff when the collapsed paint stops at the cap, and why a "No changes
// detected" result (no diff, hence no stat) never reaches here at all. That last rule is also
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
// The "+N bytes" the result reports keeps riding the branch beside the target: the summary says how
// much was written and the body says what, and neither is derived from the other. Content that is
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
// screen, which is why toolPhrase (activity.go) spends its own cap through the width authority. The
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
// text when it does not parse) split into one detailLine per line. It is what argumentDetails
// degrades to where there is nothing to label — a bare array, a malformed fragment — so a blob
// with no names still reaches the screen as it arrived instead of being dropped. Empty/null
// arguments add no lines.
func prettyJSONDetails(raw json.RawMessage) []detailLine {
	pretty := prettyJSON(raw)
	if pretty == "" {
		return nil
	}
	lines := splitLines(pretty)
	details := make([]detailLine, 0, len(lines))
	for _, ln := range lines {
		details = append(details, detailLine{Text: ln})
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
// rule. And a single value with no flat shape (a nested object, an array of objects) is indented
// JSON under its own label, since nothing else states its structure without lying about it. What
// never comes back is an envelope around the argument SET: the labels ARE the object.
//
// Both surfaces that show a call's raw arguments read this one rendering: the approval prompt a
// human decides on, and the transcript block that records a call the presenter does not recognise
// (presentToolCall's unregistered-tool fallback). One call is spelled one way wherever it appears —
// the transcript block then collapses to the house budget like any other body (render.go), which is
// a question about how many of these lines a surface seats, not about what they say.
func argumentDetails(raw json.RawMessage) []detailLine {
	pairs, ok := orderedArgs(raw)
	if !ok {
		return prettyJSONDetails(raw)
	}
	var details []detailLine
	for _, p := range pairs {
		details = append(details, detailLine{Text: p.name + ":"})
		for _, ln := range argumentValueLines(p.value) {
			details = append(details, detailLine{Text: argumentValueIndent + ln})
		}
	}
	return details
}

// argumentPair is one argument as the model wrote it: its name, and its value still encoded, so the
// value's own rendering (argumentValueLines) decides what shape it takes on the screen.
type argumentPair struct {
	name  string
	value json.RawMessage
}

// orderedArgs decodes a tool call's arguments into name/value pairs in WIRE order, reporting false
// when there is nothing to label — absent or null arguments, a top-level value that is not an
// object, a blob that does not parse, or one carrying anything after its closing brace. Every false
// leaves the caller to show the arguments as they arrived: half a labelled rendering of a malformed
// blob would be a claim about the call that the bytes do not support.
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
	return pairs, true
}

// argumentValueLines renders one argument's value as the lines that sit under its label: a string as
// its OWN lines, so the newline a JSON blob spells `\n` is a line break here; any other scalar as
// the literal the model sent (a `null` says null rather than going quiet, which is why only a
// decoded STRING takes the first exit); and a value with no flat shape as indented JSON. It
// truncates nothing and wraps nothing — how many of these lines a surface can seat is that
// surface's own business.
func argumentValueLines(value json.RawMessage) []string {
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
