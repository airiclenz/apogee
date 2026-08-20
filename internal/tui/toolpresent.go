package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
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
		stat:    diffLinesStat,       // "+A −R", counted off the unified diff the tool printed
		regions: gitDiffRangeRegions, // git's own diff, cut into numbered regions file by file
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
//
// It is now that tool's FLOOR rather than its usual reading: a body whose lines carry the diff's
// tags is walked into Edit regions and painted as their rows instead (viewDiffRegions), and what
// still renders from here is the output that carries none — the over-budget diffstat-only
// sentence, which is prose about a diff rather than one.
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

// diffRegionContext is how many unchanged lines a RECOVERED region carries each side of its
// change. Three is the ratified figure and necessarily the same three an edit tool records
// (internal/tools' editRegionContext, ADR 0052): the two are one layout — a recovered region
// paints through the very rows a recorded one does (stackedDiffLines) — so a reader must not be
// able to tell which kind of diff block they are looking at by counting its context.
const diffRegionContext = 3

// The two-cell tags a rendered line diff wears down its left edge: context, removal, addition.
// Reading them is exact because internal/tools' unifiedLineDiff puts EVERY line behind one of
// them and emits no "+++ b/…" file header, so a file line that itself begins with "+" always
// arrives behind a tag of its own.
//
// They are the same two cells the stacked reading paints as its marker column
// (stackedContextMarker and its pair) and are kept apart from them deliberately: these three are
// what a tool's output IS, those three are what this package DRAWS, and the day either side moves
// the other must not follow it silently.
const (
	diffContextTag = "  "
	diffRemovedTag = "- "
	diffAddedTag   = "+ "
)

// viewDiffRegions recovers the Edit regions of view_diff's body. That tool applies nothing, so
// there is no apply time for it to record them at (ADR 0052 §2) — but what it prints is a
// WHOLE-FILE diff, every line of both files tagged and none of them elided, so the numbers a
// region needs are simply the lines counted from 1. The result-shaped signature is the registry's
// (toolPresenter.regions).
//
// Expanded view_diff therefore stops painting the whole file: it shows the changed regions with
// three lines of context each side, exactly as an edit block shows them. That is the ratified
// behaviour change and not a truncation — the diff the MODEL reads is untouched, and the slot's
// diffstat still counts the whole of it (diffStatStat).
//
// It answers as ONE nameless section (diffFileRegions): view_diff diffs a single file and its
// output never names it, so there is nothing for a header row to say that the block's own row does
// not already say better.
func viewDiffRegions(res domain.ToolResult) []diffFileRegions {
	regions := taggedDiffRegions(res.Content)
	if len(regions) == 0 {
		return nil
	}
	return []diffFileRegions{{Regions: regions}}
}

// taggedDiffRegions cuts a tagged line diff into regions, walking it once and counting each file's
// lines from 1. It is the recovery half of the builder internal/tools runs over the two TEXTS at
// apply time (editRegions), and it answers exactly as that one does: one region per run of
// consecutive changed lines, up to diffRegionContext unchanged lines of context each side, and
// neighbouring changes left as SEPARATE regions whose context tiles the lines between them without
// overlap — the earlier region takes up to three of them as trailing context and the later takes
// what is left. Two regions that end up meeting are painted with no elision rule between them
// (regionsMeet), so the reading is the one a merge would have given.
//
// Removed and Inserted hold changed lines only, so the regions add up to the diffstat the tool
// counted over the same operations — this walk re-derives the POSITIONS the rendered diff dropped,
// never the counts, which stay the tool's (domain.DiffStat).
//
// It is total and all-or-nothing: a line carrying none of the three tags says this output is not a
// rendered diff at all — the "No changes detected" sentinel, the over-budget diffstat-only
// sentence — and NO regions come back, which leaves the whole body rendering plain (diffBody)
// rather than half-walked.
func taggedDiffRegions(content string) []domain.EditRegion {
	cutter := diffRegionCutter{beforeLine: 1, afterLine: 1}
	for _, line := range splitLines(strings.TrimRight(content, "\n")) {
		tag, text, ok := diffTagOf(line)
		if !ok {
			return nil
		}
		cutter.take(tag, text)
	}
	return cutter.finish()
}

// diffTagOf splits a rendered diff line into the tag it carries and the file line behind it. A
// line too short to carry one, or carrying anything else, is not a diff line — which includes the
// single empty line an empty body splits into, so prose and emptiness fail the same way.
func diffTagOf(line string) (tag, text string, ok bool) {
	const cells = len(diffContextTag) // one width for all three, which is what makes this a slice
	if len(line) < cells {
		return "", "", false
	}
	switch tag := line[:cells]; tag {
	case diffContextTag, diffRemovedTag, diffAddedTag:
		return tag, line[cells:], true
	}
	return "", "", false
}

// diffRegionCutter is the state of one walk down a tagged diff: the line the walk has reached in
// each file, the region being built, and the unchanged lines seen since that region's last changed
// line — lines whose role is not settled yet, because how many of them arrive before the next
// change is what says whether they are all this region's trailing context or only the first three
// are, with the rest falling to the next region's leading context.
type diffRegionCutter struct {
	regions []domain.EditRegion

	// open is the region under construction, nil between regions.
	open *domain.EditRegion

	// beforeLine and afterLine are the 1-based lines the next tagged line of each kind sits on.
	beforeLine int
	afterLine  int

	unchanged []string
}

// take folds one tagged line into the walk, advancing the counter of each file that line occupies
// a line of: a context line advances both, a removal only the before file, an insertion only the
// after file.
func (c *diffRegionCutter) take(tag, text string) {
	if tag == diffContextTag {
		c.unchanged = append(c.unchanged, text)
		c.beforeLine++
		c.afterLine++
		return
	}

	c.reachChange()
	if tag == diffRemovedTag {
		c.open.Removed = append(c.open.Removed, text)
		c.beforeLine++
		return
	}
	c.open.Inserted = append(c.open.Inserted, text)
	c.afterLine++
}

// reachChange readies an open region for a changed line. A change that follows the previous one
// with no unchanged line between them continues the same region; any unchanged line at all ends
// that region and opens the next, the two sharing out the lines between them.
func (c *diffRegionCutter) reachChange() {
	switch {
	case c.open == nil:
		c.begin()
	case len(c.unchanged) > 0:
		c.end()
		c.begin()
	}
}

// begin opens a region at the position the walk has reached, backing its start lines up over the
// leading context it takes — whatever the previous region left behind, or the run before the first
// change. Fewer lines are available at the head of a file, and the start lines then stop where the
// file does.
func (c *diffRegionCutter) begin() {
	leading := lastLines(c.unchanged, diffRegionContext)
	c.open = &domain.EditRegion{
		BeforeStart: c.beforeLine - len(leading),
		AfterStart:  c.afterLine - len(leading),
		Leading:     leading,
	}
	c.unchanged = nil
}

// end closes the open region with the trailing context it takes from the unchanged lines that
// follow it, and files it. It CONSUMES the lines it takes: what remains is what a following region
// may claim as its leading context, so no line is ever context for two regions at once.
func (c *diffRegionCutter) end() {
	trailing := firstLines(c.unchanged, diffRegionContext)
	c.open.Trailing = trailing
	c.regions = append(c.regions, *c.open)
	c.open = nil
	c.unchanged = c.unchanged[len(trailing):]
}

// hunk moves the walk to the lines a new hunk states each file resumes at, closing whatever region
// the previous hunk left open first. It is what a diff with ELISIONS in it needs and a whole-file
// one never asks for (taggedDiffRegions counts from 1 and never jumps).
//
// The unchanged lines it had not yet placed are DROPPED rather than carried over: git elided
// everything between the two hunks, so a line before that gap can be no region's leading context
// after it — the closing region takes its three as trailing context and the rest are simply lines
// this reading does not show.
func (c *diffRegionCutter) hunk(before, after int) {
	if c.open != nil {
		c.end()
	}
	c.unchanged = nil
	c.beforeLine, c.afterLine = before, after
}

// finish ends the walk, closing a region the last line left open, and returns the regions — none
// at all when the diff carried no changed line.
func (c *diffRegionCutter) finish() []domain.EditRegion {
	if c.open != nil {
		c.end()
	}
	return c.regions
}

// firstLines returns a copy of the first count lines, or of all of them when there are fewer. The
// copy is the point: a region outlives the walk — it is kept on the view for the session — and
// must not hold the walk's own buffer, where the lines it did not take would be retained with it.
func firstLines(lines []string, count int) []string {
	if len(lines) > count {
		lines = lines[:count]
	}
	return slices.Clone(lines)
}

// lastLines returns a copy of the last count lines, or of all of them when there are fewer — the
// mirror of firstLines, and a copy for the same reason.
func lastLines(lines []string, count int) []string {
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return slices.Clone(lines)
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

// ----------------------------------------------------------------------------
// git_diff_range — the regions of git's own unified diff
// ----------------------------------------------------------------------------

// The line shapes git's unified diff is built out of. A file section opens on
// "diff --git a/<path> b/<path>", and a hunk within it opens on "@@ -a,b +c,d @@" — which is where
// this recovery gets its numbers, git having ELIDED everything between one hunk and the next. The
// no-newline marker is git's note ABOUT the line above it rather than a line of either file.
const (
	gitDiffFilePrefix = "diff --git "
	gitDiffHunkPrefix = "@@"
	gitDiffNoNewline  = `\ No newline at end of file`
)

// The two headers this recovery reads rather than skips: the file a section is about (the b-side
// path — the name the change left the file under) and the lines each side of a hunk resumes at. A
// count of 1 is written without its comma, which is why the counts are optional here; they are not
// read at all, because what a hunk holds is the lines it then prints.
var (
	gitDiffFilePattern = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	gitDiffHunkPattern = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

// gitDiffHeaderPrefixes are the extended-header lines a TEXTUAL diff carries between its file line
// and its first hunk: the blob index, the mode lines, and the two "---"/"+++" names. They say
// nothing this recovery needs — every number it wants is in a hunk header — so they are recognised
// and skipped.
//
// What is deliberately NOT here is any header saying the section carries no text to paint:
// "Binary files … differ", a rename's "similarity index" and "rename from"/"rename to", a "GIT
// binary patch". A line none of these prefixes claims stops the walk dead (gitDiffFileSections),
// so such a body keeps the plain output rendering it had before this existed — which is the honest
// answer for it: git said something about that file this reading cannot show.
var gitDiffHeaderPrefixes = []string{
	"index ", "--- ", "+++ ", "old mode ", "new mode ", "new file mode ", "deleted file mode ",
}

// gitDiffRangeRegions recovers the Edit regions of git_diff_range's output. That tool applies
// nothing either, so it has no apply time to record a change at (ADR 0052 §2) — and what it prints
// is git's own unified diff, which unlike view_diff's whole-file body shows only the neighbourhood
// of each change. The numbers therefore come from the "@@" header each hunk opens with, which is
// exactly what those headers are for. The result-shaped signature is the registry's
// (toolPresenter.regions).
//
// The answer is one section per file the range touched (ratified call 10): git's output spans
// files, each numbering its own lines, and the block paints a muted row naming the file over that
// file's regions.
func gitDiffRangeRegions(res domain.ToolResult) []diffFileRegions {
	return gitDiffFileSections(res.Content)
}

// gitDiffFileSections walks git's unified output into those sections. It shares its cutter with the
// walk down a whole-file diff (taggedDiffRegions), so a region recovered here and a region
// recovered there are the same thing and paint through the same rows: one region per run of
// consecutive changed lines, up to diffRegionContext unchanged lines of context each side, and
// neighbouring changes left as separate regions whose context tiles the lines between them.
//
// It is TOTAL and ALL-OR-NOTHING. A line no rule above claims — a binary section's "Binary files …
// differ", a rename's "similarity index", the columns of a `--stat` call, the "No differences
// found" sentinel, a warning that reached the output — returns NO sections at all, and the whole
// body then renders as the plain output it always did (outputDetail). So does a file section that
// yielded no region: a body painted with one of its files silently missing is exactly the
// half-parsed mix the fallback exists to avoid.
func gitDiffFileSections(content string) []diffFileRegions {
	var walk gitDiffWalk
	for _, line := range splitLines(strings.TrimRight(content, "\n")) {
		if !walk.take(line) {
			return nil
		}
	}
	if !walk.closeFile() {
		return nil
	}
	return walk.sections
}

// gitDiffWalk is the state of one walk down a printed git diff: the sections closed so far, the
// file the open one is about, and the region cutter filling it. inHunk is what tells a header line
// from a line of file content — the two are told apart by WHERE they stand, since a hunk's content
// lines can spell anything at all behind their one-cell tag.
type gitDiffWalk struct {
	sections []diffFileRegions
	file     string
	open     bool
	inHunk   bool
	cutter   diffRegionCutter
}

// take folds one line of the output into the walk and reports whether it belonged to a diff at all.
// A false answer is final: the caller abandons the whole reading rather than skipping the line.
func (w *gitDiffWalk) take(line string) bool {
	switch {
	case strings.HasPrefix(line, gitDiffFilePrefix):
		m := gitDiffFilePattern.FindStringSubmatch(line)
		return m != nil && w.startFile(m[2])
	case !w.open:
		return false
	case strings.HasPrefix(line, gitDiffHunkPrefix):
		return w.startHunk(line)
	case w.inHunk:
		return w.takeHunkLine(line)
	default:
		return gitDiffHeaderLine(line)
	}
}

// startFile closes the section the walk was in and opens one for path.
func (w *gitDiffWalk) startFile(path string) bool {
	if !w.closeFile() {
		return false
	}
	w.file, w.open, w.inHunk, w.cutter = path, true, false, diffRegionCutter{}
	return true
}

// closeFile files the open section and reports whether it held a change to paint. A section that
// yielded no region — a rename, a mode change, a binary file — fails the walk instead of being
// dropped, because dropping it would show a diff of fewer files than the tool printed.
func (w *gitDiffWalk) closeFile() bool {
	if !w.open {
		return true
	}
	regions := w.cutter.finish()
	if len(regions) == 0 {
		return false
	}
	w.sections = append(w.sections, diffFileRegions{File: w.file, Regions: regions})
	return true
}

// startHunk reads the two starting lines off a hunk header and moves the cutter to them. A "@@"
// line that is not a hunk header is not something this reading can place, so it fails the walk.
func (w *gitDiffWalk) startHunk(line string) bool {
	m := gitDiffHunkPattern.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	before, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	after, err := strconv.Atoi(m[2])
	if err != nil {
		return false
	}
	w.cutter.hunk(before, after)
	w.inHunk = true
	return true
}

// takeHunkLine folds one line of a hunk's body into the cutter. git tags such a line in ONE cell —
// a space, a "-" or a "+" — where the diff internal/tools renders tags in two (diffContextTag), so
// the tag is translated here and the cutter goes on reading a single shape of them.
//
// The no-newline marker is skipped rather than counted: it is a note about the line above it, and
// taking it for a line of the file would push every number after it one out.
func (w *gitDiffWalk) takeHunkLine(line string) bool {
	if line == gitDiffNoNewline {
		return true
	}
	switch {
	case strings.HasPrefix(line, " "):
		w.cutter.take(diffContextTag, line[1:])
	case strings.HasPrefix(line, "-"):
		w.cutter.take(diffRemovedTag, line[1:])
	case strings.HasPrefix(line, "+"):
		w.cutter.take(diffAddedTag, line[1:])
	default:
		return false
	}
	return true
}

// gitDiffHeaderLine reports whether a line standing between a file's "diff --git" line and its
// first hunk is one of the extended headers this reading skips (gitDiffHeaderPrefixes).
func gitDiffHeaderLine(line string) bool {
	for _, prefix := range gitDiffHeaderPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
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
