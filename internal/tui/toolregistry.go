package tui

import (
	"fmt"
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
	stat func(res domain.ToolResult) (statValue, bool)

	// argStat words the same slot from the call's OWN ARGUMENTS, at the moment the call is
	// presented — the request half of the table's stat column, and argBody's counterpart: a
	// write's line count and an edit's diffstat are facts about what was ASKED FOR, so the slot
	// can say them before the result lands, without the tool reporting anything and without a
	// byte more crossing the wire (ADR 0031).
	//
	// Its answer is kept on the view (toolView.argStat) rather than re-read later, so the
	// arguments themselves need not be retained for the life of the session: a write's whole file
	// content stays out of memory behind a field that only ever yields one short phrase.
	argStat func(args map[string]any) (statValue, bool)

	// body renders the lines laid out BENEATH the branch when the result's typed summary supplied
	// the branch line itself. It takes the whole result rather than its prose, because what such
	// a body is read off differs by tool: view_diff renders its Content, read_file the located
	// line numbers its domain.ReadSpan carries. A body derived from what the call ASKED for is
	// argBody's instead.
	body func(res domain.ToolResult) []detailLine

	// regions RECOVERS the Edit regions of a diff the tool PRINTED rather than recorded. The two
	// diff-READING tools apply nothing, so they have no apply time to record a change at (ADR 0052
	// §2) — but their output carries the positions anyway, view_diff's as a whole-file diff whose
	// lines can simply be counted from 1 and git_diff_range's in the `@@` header each of git's own
	// hunks opens with, so this hook cuts that output into the same three-context regions an edit
	// tool attaches and the block gets both readings of its diff like every other diff-bodied one
	// (toolView.Regions).
	//
	// It is TOTAL: output it cannot walk yields NO regions rather than a guess, which leaves
	// standing whatever the tool's own body hook rendered — the plain reading such a result had
	// before this existed (ratified call 9).
	//
	// It answers in FILE SECTIONS, because a printed diff need not be about one file:
	// git_diff_range's spans every file the range touched and each of them numbers its own lines,
	// so a section names the file its regions were cut from and the block paints that name over
	// them (ratified call 10). The one section a whole-file view_diff yields names none — that
	// block's own row already says which file it read.
	regions func(res domain.ToolResult) []diffFileRegions

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
// Every detail extractor here renders PROSE. The ten tools that report a typed summary
// (read_file, write_file, list_dir, grep, view_diff, web_search, git_status, and the three edit
// tools single_find_and_replace, multi_find_and_replace and edit_existing_file) word their slot
// from that summary through their stat hook, and keep their detail extractor as the floor for a
// result that carries none — firstLineDetail for all but git_status, whose report is free-form
// output and floors on outputDetail — so a degraded card is that tool's own words, never a file
// dumped into the transcript. A summary-bearing tool whose floor lays out a BODY has to register a
// body hook as well, because the typed path skips the extractor altogether
// (toolView.absorbProse) and the body would simply go missing: read_file, view_diff and git_status
// each set one. git_diff_range sets one ahead of that need — its floor lays out a body too, so the
// day it reports a typed outcome the card keeps the output git printed instead of losing it. The rest quote their fixed sentence or hand free-form output (a command run, a
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
		label:   "Diff Preview",
		verb:    "diffing",
		target:  stringArg("path"),
		detail:  firstLineDetail, // floor; the "No changes detected" sentinel renders here too
		stat:    diffStatStat,
		body:    viewDiffBody,    // the plain floor: a body carrying no tags to walk
		regions: viewDiffRegions, // the whole-file diff, cut into numbered regions
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
	// The Console family (ADR 0059) drives ONE live process across four calls, so the four rows
	// are written to be read together: the id the model steers by leads every branch but the
	// open's, where the command leads and the id is what the call produced (consoleOpenStat).
	// The labels stay distinct — a family whose members share one label would fold a close into
	// the read above it, and "which call was that" is the one thing a console run needs to say.
	"console_open": {
		label:  "Console",
		verb:   "opening",
		target: stringArg("command"),
		detail: consoleOpenDetail,
		stat:   consoleOpenStat,
	},
	"console_send": {
		label:  "Console Send",
		verb:   "sending to",
		target: consoleSendTarget,
		detail: consoleDetail,
		stat:   consoleStatusStat,
	},
	"console_read": {
		label:  "Console Read",
		verb:   "reading",
		target: consoleTarget,
		detail: consoleDetail,
		stat:   consoleStatusStat,
	},
	"console_close": {
		label:  "Console Close",
		verb:   "closing",
		target: consoleTarget,
		detail: consoleDetail,
		stat:   consoleStatusStat,
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
		label:   "Git Diff",
		verb:    "diffing",
		target:  refRangeTarget,
		detail:  outputDetail,        // the plain floor: output this package cannot walk as a diff
		stat:    diffLinesStat,       // "+A −R", counted off the regions that walk recovers
		body:    gitDiffRangeBody,    // that same plain floor, kept beneath the row on the typed path
		regions: gitDiffRangeRegions, // git's own diff, cut into numbered regions file by file
	},
	"git_status": {
		label: "Git Status",
		verb:  "checking",
		// No target: the tool takes no arguments — the repository IS the target.
		detail: outputDetail, // floor: the branch line plus the staged/unstaged/untracked sections
		stat:   changedFilesStat,
		body:   gitStatusBody, // that same report, kept beneath the row on the typed path
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
//   - off the OUTPUT the tool printed (testVerdictStat, foundFilesStat, commitCountStat,
//     commitHashStat off a fixed header the tool writes into it; diffLinesStat off git's own diff
//     grammar, read through the very walk the body beneath it is painted from, so the slot and
//     those rows cannot come to count different things). This is the reading the
//     file's opening note warns about, and it is taken only because design call 14 rules out
//     growing the engine for presentation. Every one of them is anchored on a token its writer
//     formats deliberately — the tool's own header, or git's diff grammar — and every one is
//     TOTAL: a shape it does not recognise returns false,
//     which leaves that tool's prose floor in the slot rather than a wrong number. A wording
//     change in internal/tools degrades such a card to what it showed before this existed.
//
// A stat is the block's OWN wording, so the shortening seam spells any path it names relative to
// the workspace (shortenPaths) — the same treatment every phrase this file writes gets.

// blankStat is the table's `—`: a tool whose outcome is already fully said by its header and
// target (a copy's two paths, a delete's one). The slot prints nothing and the dots run to the
// `▶`. It is stated rather than left empty by omission, because omitting the hook would leave the
// tool's prose sentence in a slot the table says is blank.
func blankStat(domain.ToolResult) (statValue, bool) { return plainStat(""), true }

// readSpanStat words read_file's slot as the number of lines the call returned, counted off the
// span the tool reports (domain.ReadSpan, 1-based and inclusive). A file with no lines at all
// yields a span whose End precedes its Start, which is 0 lines rather than a negative count.
func readSpanStat(res domain.ToolResult) (statValue, bool) {
	v, ok := res.Summary.(domain.ReadSpan)
	if !ok {
		return statValue{}, false
	}
	n := v.End - v.Start + 1
	if n < 0 {
		n = 0
	}
	return pluralStat(n, "line"), true
}

// writtenLinesStat words write_file's slot as the number of lines the call WRITES, read off its
// own content argument — the table asks for lines and the tool reports bytes (domain.WroteBytes),
// and the request already holds the answer. It is the same reading the body takes (writtenLines),
// so the two cannot disagree about what the call puts in the file.
func writtenLinesStat(args map[string]any) (statValue, bool) {
	content, ok := args["content"].(string)
	if !ok {
		return statValue{}, false
	}
	return pluralStat(len(editLines(content)), "line"), true
}

// listedEntriesStat words list_dir's slot as the directory's total entry count. "entries" is a
// FIXED plural, deliberately not plural(): the card has always read "1 entries".
func listedEntriesStat(res domain.ToolResult) (statValue, bool) {
	v, ok := res.Summary.(domain.ListedEntries)
	if !ok {
		return statValue{}, false
	}
	return countedStat(v.Total, "entries"), true
}

// matchedLinesStat words grep's slot as its hit count. The table also asks for the number of FILES
// those hits fall in; no result carries it (domain.MatchedLines is a total alone), so the slot
// says the half that exists rather than a second number derived from a listing (design call 14).
func matchedLinesStat(res domain.ToolResult) (statValue, bool) {
	v, ok := res.Summary.(domain.MatchedLines)
	if !ok {
		return statValue{}, false
	}
	return pluralStat(v.Total, "hit"), true
}

// searchHitsStat words web_search's slot as its result count.
func searchHitsStat(res domain.ToolResult) (statValue, bool) {
	v, ok := res.Summary.(domain.SearchHits)
	if !ok {
		return statValue{}, false
	}
	return pluralStat(v.Count, "result"), true
}

// diffStatStat words view_diff's slot as the diffstat the tool counted off its own operations. A
// "No changes detected" result carries no domain.DiffStat and so keeps its sentence.
func diffStatStat(res domain.ToolResult) (statValue, bool) {
	v, ok := res.Summary.(domain.DiffStat)
	if !ok {
		return statValue{}, false
	}
	return diffedStat(v.Added, v.Removed), true
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
func editRegionsStat(res domain.ToolResult) (statValue, bool) {
	regions, ok := recordedRegions(res)
	if !ok {
		return statValue{}, false
	}
	stat := regions.Stat()
	return diffedStat(stat.Added, stat.Removed), true
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
func singleReplacementStat(args map[string]any) (statValue, bool) {
	removed, _ := args["oldText"].(string)
	inserted, _ := args["newText"].(string)
	if removed == "" && inserted == "" {
		return statValue{}, false
	}
	a, r := pairCounts([]editPair{replacedText(removed, inserted)})
	return diffedStat(a, r), true
}

// multiReplacementStat words multi_find_and_replace's slot as the NUMBER OF CHANGES the call
// lists, which is what the table asks of it — a batch's shape is how many edits it makes, and the
// lines they touch are beneath (multiReplacementBody). Malformed arguments keep the prose floor.
func multiReplacementStat(args map[string]any) (statValue, bool) {
	list, ok := args["replacements"].([]any)
	if !ok {
		return statValue{}, false
	}
	return pluralStat(len(list), "change"), true
}

// fileEditStat words edit_existing_file's slot as the diffstat of what the call sends: a patch's
// hunks, or full replacement content that removes nothing and inserts the lot — the same two
// readings its body takes (fileEditBody).
func fileEditStat(args map[string]any) (statValue, bool) {
	content, ok := args["content"].(string)
	if !ok {
		return statValue{}, false
	}
	pairs := []editPair{replacedText("", content)}
	if isPatchArgument(content) {
		pairs = patchEditPairs(content)
	}
	a, r := pairCounts(pairs)
	return diffedStat(a, r), true
}

// exitCodeStat words the slot of the two tools that run a process. A non-zero exit is an ERROR
// result (internal/tools: terminal and python_exec both flag it), which reads red from the error
// layer above — so a result reaching here exited cleanly, and the slot says so. The table asks
// for a duration beside it; no result carries one, so the code stands alone (design call 14).
func exitCodeStat(domain.ToolResult) (statValue, bool) { return plainStat("exit 0"), true }

// exitCodeMarker matches the "[exit code N]" line subprocessToolResult appends to a FAILED
// subprocess result (internal/tools/terminal.go). It is anchored at the end of the output, where
// the tool writes it, so a command that printed the same phrase cannot be read as the marker — the
// real one is always appended after it. The code may be negative: a run whose leader exited but
// whose pipe stayed held is reported as -1, and the code may be followed inside the brackets by a
// note the tool added about WHY the run failed (the terminal's fail-fast note) — the slot wants the
// code, so everything after it up to the closing bracket is matched and dropped.
//
// Its two groups are the shape exitMarkerPhrase reads: the whole marker, then the code inside it.
var exitCodeMarker = regexp.MustCompile(`\n?(\[exit code (-?\d+)[^\]]*\])\s*$`)

// consoleStatusMarker matches the status line every Console result ends with — "alive", "exited
// with code 2", "killed" (consoleStatus, internal/tools/console_common.go). Unlike exitCodeMarker's
// bracketed phrase, those words are ordinary prose, so end-anchoring alone would read the last word
// of a program's own last line ("the dev server is alive") as the verdict on it: the status must
// BEGIN a line as well as end the output, which is what `(?:^|\n)` says and an optional `\n?` did
// not. It captures the status first and the code inside it second, exitMarkerPhrase's shape.
var consoleStatusMarker = regexp.MustCompile(`(?:^|\n)(alive|exited with code (-?\d+)|killed)\s*$`)

// exitMarkerPhrase words one anchored process-status marker for the outcome slot and hands back
// the output left once the marker has been taken off it — the body that then lays out beneath the
// branch. It is one reading two families of execution tool share: a one-shot command ends its
// FAILED output with "[exit code 2]", and a Console ends every result with how its process stands.
// Two spellings of one fact, so the marker is a parameter rather than the difference between two
// functions that would drift apart.
//
// A marker must capture the status it spells FIRST and, where that status carries an exit code,
// the code SECOND: a code is worded "exit N" whatever sentence the tool wrapped it in, and a
// status carrying none — "alive", "killed" — is its own word.
func exitMarkerPhrase(marker *regexp.Regexp, content string) (phrase, output string, ok bool) {
	m := marker.FindStringSubmatchIndex(content)
	if m == nil {
		return "", "", false
	}
	phrase = content[m[2]:m[3]]
	if len(m) >= 6 && m[4] >= 0 {
		phrase = "exit " + content[m[4]:m[5]]
	}
	return phrase, content[:m[0]], true
}

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
	return exitMarkerPhrase(exitCodeMarker, content)
}

// consoleStatusStat words the slot of the three Console calls that report on a live process
// (console_send, console_read, console_close): "alive" while the program is still running, "exit
// N" once it has ended, "killed" when a signal ended it. Unlike a one-shot command's exit, none of
// those is an error — a dev server the model asked to close exited exactly as asked — so the
// verdict reaches the slot through the stat hook rather than through the failure layer.
//
// A result in no such shape keeps its own prose floor: the Console refusals are error results
// whose first line IS the message ("no console 7 (open consoles: 1, 2)").
func consoleStatusStat(res domain.ToolResult) (statValue, bool) {
	phrase, _, ok := exitMarkerPhrase(consoleStatusMarker, res.Content)
	if !ok {
		return statValue{}, false
	}
	return plainStat(phrase), true
}

// consoleOpenedHead matches the header console_open writes its result with — "console 3 opened:
// npm run dev" (internal/tools/console_open.go) — anchored at the START, where the tool writes it,
// so a line the program printed cannot be read as the header.
var consoleOpenedHead = regexp.MustCompile(`^console (\d+) opened: `)

// consoleOpenedID reads the Console id off that header and hands back the output beneath it. A
// result in another shape — an id-less refusal — is handed back whole and unread.
func consoleOpenedID(content string) (id, output string, ok bool) {
	head, rest, _ := strings.Cut(content, "\n")
	m := consoleOpenedHead.FindStringSubmatch(head)
	if m == nil {
		return "", content, false
	}
	return "console " + m[1], rest, true
}

// consoleOpenStat words console_open's slot with the ID the model must now drive the Console by,
// which is what the call PRODUCED — the command it started is already the row's target, and the id
// is nowhere else on the card. A program that was over before the call returned is worded by its
// exit instead: an id nothing can address any more is not the outcome of that open.
func consoleOpenStat(res domain.ToolResult) (statValue, bool) {
	if phrase, _, ok := exitMarkerPhrase(consoleStatusMarker, res.Content); ok {
		return plainStat(phrase), true
	}
	if id, _, ok := consoleOpenedID(res.Content); ok {
		return plainStat(id), true
	}
	return statValue{}, false
}

// cleanStat words diagnostics' slot. Findings come back flagged as an error result, so a result
// that reaches here found none — the table's `clean` half; its `N issues` half is the red error
// line the failure layer already paints.
func cleanStat(domain.ToolResult) (statValue, bool) { return plainStat("clean"), true }

// delegationStat words a sub-agent's slot. A delegation that failed comes back as an error result
// and reads red, so a result reaching here is one that finished. The table asks for a step count
// beside the verdict; the engine exposes none on the result (design call 14), so the verdict
// stands alone.
func delegationStat(domain.ToolResult) (statValue, bool) { return plainStat("done"), true }

// testVerdictHead matches the verdict token run_tests opens its condensed report with — "PASS (go
// test)", "FAIL (pytest) — 3 failing tests" — anchored at the start so a later line reading "FAIL"
// cannot be mistaken for the header.
var testVerdictHead = regexp.MustCompile(`^(PASS|FAIL)\b`)

// testVerdictStat words run_tests' slot as its bare verdict. The table asks for a duration beside
// it; the result carries none (design call 14), so the verdict stands alone. Output the tool
// worded some other way keeps its own first line in the slot.
func testVerdictStat(res domain.ToolResult) (statValue, bool) {
	m := testVerdictHead.FindStringSubmatch(strings.TrimSpace(firstLine(res.Content)))
	if m == nil {
		return statValue{}, false
	}
	return plainStat(m[1]), true
}

// foundFilesHead matches the header find_files opens its listing with — "[12 files found, showing
// 1-12]", the count being the FULL total rather than the page.
var foundFilesHead = regexp.MustCompile(`^\[(\d+) files found\b`)

// foundFilesStat words find_files' slot as that total, and reads the tool's own empty-result
// sentence as the zero it states.
func foundFilesStat(res domain.ToolResult) (statValue, bool) {
	head := strings.TrimSpace(firstLine(res.Content))
	if head == "No files found" {
		return pluralStat(0, "file"), true
	}
	m := foundFilesHead.FindStringSubmatch(head)
	if m == nil {
		return statValue{}, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return statValue{}, false
	}
	return pluralStat(n, "file"), true
}

// changedFilesStat words git_status' slot as how many paths the working tree has changed: the sum
// of the three section counts the tool reports as a domain.ChangedFiles, which are the FULL counts
// even where the printed list was capped, and three zeros on a clean tree. A result carrying no
// summary keeps its prose floor, exactly as every other typed slot degrades — the slot never reads
// the report's sentences, so a path NAMED like one of them cannot be mistaken for one.
func changedFilesStat(res domain.ToolResult) (statValue, bool) {
	v, ok := res.Summary.(domain.ChangedFiles)
	if !ok {
		return statValue{}, false
	}
	return countedStat(v.Staged+v.Unstaged+v.Untracked, "changed"), true
}

// commitCountStat words git_log's slot as how many commits it listed. The tool prints one line per
// commit ("--format=%h %ad %s", a subject being single-line by construction), so the lines ARE the
// count; its empty-result sentence states the zero.
func commitCountStat(res domain.ToolResult) (statValue, bool) {
	trimmed := strings.TrimSpace(res.Content)
	if trimmed == "" {
		return statValue{}, false
	}
	if trimmed == "No commits found" {
		return pluralStat(0, "commit"), true
	}
	n := 0
	for _, ln := range splitLines(trimmed) {
		if strings.TrimSpace(ln) != "" {
			n++
		}
	}
	return pluralStat(n, "commit"), true
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
func commitHashStat(res domain.ToolResult) (statValue, bool) {
	hash, ok := commitHashOf(res.Content)
	if !ok {
		return statValue{}, false
	}
	return plainStat(hash), true
}

// diffLinesStat words git_diff_range's slot as the diffstat of the unified diff the tool printed,
// counted off the SAME walk the body beneath it is painted from (gitDiffFileSections): a region
// holds changed lines only (diffRegionCutter), so every section's regions add up to exactly the
// lines git tagged. Added is the sum of the Inserted lines and removed the sum of the Removed
// ones. Reading the walk rather than the raw text is what keeps the slot and the rows under it
// counting one thing — and it is the fix for a removed line whose content begins "--" (a
// "--flag"), or an added one beginning "++" (a "++i"), which a bare prefix test over the output
// mistakes for the "---"/"+++" file headers and drops.
//
// A walk that REFUSES — it is all-or-nothing, so a binary section, a rename, a `--stat` call or
// any line it cannot place yields nothing (gitDiffFileSections) — falls back to counting the
// tagged lines directly, with the one state bit whose absence made the old prefix test wrong: a
// "---"/"+++" line is a file header only OUTSIDE a hunk, so the fallback tracks inHunk exactly as
// gitDiffWalk does (a "diff --git" line clears it, an "@@" line sets it) and a "--"-prefixed line
// standing inside a hunk counts as the removal it is.
//
// Either path answers the same way when nothing was tagged at all: a call asking for `--stat` or
// `--name-only` keeps its prose floor, which is the honest answer — that output states its own
// totals.
func diffLinesStat(res domain.ToolResult) (statValue, bool) {
	added, removed := walkedDiffCounts(res.Content)
	if added == 0 && removed == 0 {
		return statValue{}, false
	}
	return diffedStat(added, removed), true
}

// walkedDiffCounts is diffLinesStat's counting half: the regions the diff walk recovered, or —
// when it recovered none — the header-aware tagged-line count that stands in for them.
func walkedDiffCounts(content string) (added, removed int) {
	sections := gitDiffFileSections(content)
	if len(sections) == 0 {
		return taggedDiffCounts(content)
	}
	for _, section := range sections {
		for _, region := range section.Regions {
			added += len(region.Inserted)
			removed += len(region.Removed)
		}
	}
	return added, removed
}

// taggedDiffCounts counts the tagged lines of output the walk refused. It is the old loop plus the
// state bit it was missing — inHunk, kept exactly as gitDiffWalk keeps it — and not a second
// parser: it places no line, it only knows whether the one in front of it stands inside a hunk,
// which is the whole difference between a file header and a line of content that happens to spell
// one.
func taggedDiffCounts(content string) (added, removed int) {
	inHunk := false
	for _, ln := range splitLines(content) {
		switch {
		case strings.HasPrefix(ln, gitDiffFilePrefix):
			inHunk = false
		case strings.HasPrefix(ln, gitDiffHunkPrefix):
			inHunk = true
		case !inHunk && (strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---")):
		case strings.HasPrefix(ln, "+"):
			added++
		case strings.HasPrefix(ln, "-"):
			removed++
		}
	}
	return added, removed
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

// consoleTarget leads a Console call's branch with the console it drives — "console 3" — which is
// the whole of what console_read and console_close name. The id arrives as a JSON number, or as
// the numeric STRING a model that quotes its arguments sends, which the tool accepts too
// (consoleID, internal/tools/console_common.go): the row says the same thing either way rather
// than losing its target to a quoting habit. An id in neither shape yields no target, and the
// call's arguments lead the block instead.
func consoleTarget(args map[string]any) string {
	id := intArg(args, "id")
	if id <= 0 {
		if quoted, ok := args["id"].(string); ok {
			id, _ = strconv.Atoi(strings.TrimSpace(quoted))
		}
	}
	if id <= 0 {
		return ""
	}
	return "console " + strconv.Itoa(id)
}

// consoleSendTarget carries what was TYPED as the qualifier on that console — `console 3 · npm
// test` — because the console alone is what every send in a run has in common and the input is
// what tells them apart. It is single-lined and clipped like any other multi-line argument
// (firstLineArg), and a send of nothing at all — the empty input that presses Enter — is the
// console standing alone rather than a row opening on a stray separator (qualifiedTarget).
func consoleSendTarget(args map[string]any) string {
	return qualifiedTarget(consoleTarget(args), firstLineArg("input")(args))
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
	return promotedOutput(clipDetail(firstLine(content)), statValue{})
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
// question's own lines, the offered choices each behind "[✔]" or "[ ]", and then every answer line
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
		return promotedOutput(body[0].Text, pluralStat(len(body), "line"))
	}
	return toolOutcome{Details: body}
}

// consoleDetail lays out what a Console printed, WITHOUT the status line the slot already words
// (consoleStatusStat). The line is the tool's verdict on the process rather than something the
// program said, and a body repeating "alive" under a branch that already reads "alive" spends a
// row saying nothing twice. Everything else is free-form output and lays out as such.
func consoleDetail(content string) toolOutcome {
	if _, output, ok := exitMarkerPhrase(consoleStatusMarker, content); ok {
		content = output
	}
	return outputDetail(content)
}

// consoleOpenDetail is consoleDetail for console_open, whose result opens with a header of the
// tool's own — "console 3 opened: npm run dev". Both of its facts are already on the row: the
// command IS the target, and the id is in the slot (consoleOpenStat). So the header comes off and
// the body is what the program actually printed in the open's wait window, which is the one thing
// on the card the model has not already been told.
func consoleOpenDetail(content string) toolOutcome {
	if _, output, ok := consoleOpenedID(content); ok {
		content = output
	}
	return consoleDetail(content)
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

// gitStatusBody lays git_status' REPORT out beneath the branch — the branch line and the
// staged/unstaged/untracked sections, the same lines outputDetail lays out for a result that
// carries no summary. It exists because a result carrying one skips the detail extractor
// altogether (toolView.absorbProse): without a body hook, gaining a typed slot would COST this
// tool the output it printed, leaving a count on a row with nothing under it. read_file and
// view_diff keep their bodies across that same seam for the same reason.
//
// It reads the CONTENT and not the summary: the counts are the slot's and the paths are the
// body's, so the two halves of the card say different things about one report rather than the
// same thing twice.
func gitStatusBody(res domain.ToolResult) []detailLine {
	return outputBody(res.Content)
}

// gitDiffRangeBody keeps git's printed output beneath the branch on the typed path, for the same
// reason gitStatusBody does: this entry's floor is outputDetail, and a result carrying a
// domain.ToolSummary skips the detail extractor altogether (toolView.absorbProse), so without a
// body hook such a result would leave a diffstat sitting over nothing.
//
// It is a FLOOR and rarely the reading that shows: output the diff walk can place is repainted as
// the numbered per-file regions (gitDiffRangeRegions), which replace the body wholesale. What
// renders from here is the output that walk refuses — a binary section, a rename, a `--stat`
// call — which is the plain reading such a result has always had.
func gitDiffRangeBody(res domain.ToolResult) []detailLine {
	return outputBody(res.Content)
}
