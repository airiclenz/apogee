package tui

import (
	"encoding/json"
	"fmt"
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
// unknown tool falls back to its raw name and pretty-printed arguments, so a tool with no
// registry entry still renders legibly.
//
// The same entry also carries the tool's active verb ("reading", "running"), which the live
// status line pairs with the target while the call is in flight — the per-tool knowledge
// stays in this one registry instead of growing a second, parallel switch elsewhere.

// detailKind tags a tool-detail line so the renderer can colour it. The diff kinds are
// emitted by diffBody (view_diff's body renderer) and rendered red/green in render.go.
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

// toolBody is a tool call's retained body — the detail lines that lay out beneath its branch line
// — bound to the one fact the collapsed paint's cap depends on: whether those lines are a DIFF (a
// body carrying at least one red/green line). A diff body keeps diffDetailCap lines when the block
// is collapsed, any other multi-line body keeps its first (collapsedDetails, render.go).
//
// The lines and their kind travel as ONE value, and newToolBody is the only thing that puts lines
// in, so the pair cannot be written stale: there is no way to hand a painter a diff body that says
// it is plain, because saying so is not a thing a caller does — the kind is derived from the lines
// it is derived WITH. That is deliberate, and it is what keeps the cap correct without re-deriving
// the kind at paint: the body is retained whole (layout.md) and capped only when it is drawn, so a
// rule read off the lines at that seam would walk a command's whole output on every repaint.
//
// The kind is never persisted — the wire carries each line's own Kind, from which decode builds the
// body through this same constructor (fromWireToolView).
type toolBody struct {
	lines []detailLine
	diff  bool
}

// newToolBody is the ONE place a body's lines and its kind are paired, and so the one place the
// kind is derived. The zero toolBody needs no constructor and is consistent by construction: no
// lines, no diff.
func newToolBody(lines []detailLine) toolBody {
	return toolBody{lines: lines, diff: bodyIsDiff(lines)}
}

// all is the body's lines, in order — what the painter lays out.
func (b toolBody) all() []detailLine { return b.lines }

// len is how many lines the body retains. It says nothing about the block's shape: that follows
// from which halves of the outcome are filled, never from how many lines there are (render.go).
func (b toolBody) len() int { return len(b.lines) }

// isDiff reports the kind newToolBody settled: true when the body carries a red/green line.
func (b toolBody) isDiff() bool { return b.diff }

// with returns the body extended by more lines — a result folding into a view that already has one
// (enrichWithResult). It goes back through the constructor, so the grown body's kind describes what
// it now holds rather than what it held before. Adding nothing returns the body untouched, which is
// what keeps a not-yet-enriched call's nil body nil.
func (b toolBody) with(more []detailLine) toolBody {
	if len(more) == 0 {
		return b
	}
	return newToolBody(append(b.lines, more...))
}

// stripEscapes removes the ESC byte from every line's text in place (the sanitize seam's work on
// the body). It cannot disturb the settled kind: a line's Kind is set by its producer and the strip
// only ever rewrites Text.
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
// raw-fallback label. Every Label renders the same way — bold orange (render.go) — so a raw
// fallback is not visually singled out.
type toolView struct {
	Label   string
	Verb    string
	Target  string
	Summary detailLine

	// Details is the body laid out beneath the branch line, carrying its own kind (toolBody):
	// the painter asks the body what it is rather than the view keeping a second, separately
	// assignable answer that a new body path could leave stale.
	Details toolBody

	name string
}

// toolOutcome is what a prose extractor returns: the one-line Summary that rides the branch
// line beside the target, and the Details body laid out beneath it. Either half may be empty
// — a fixed result sentence is summary-only ("HTTP 200 OK") and multi-line free-form output is
// body-only (every one of its lines). A tool whose result carries a domain.ToolSummary
// does not come through here at all: summaryLine words the branch line and the presenter's
// body renderer (view_diff's alone) fills the half beneath it.
type toolOutcome struct {
	Summary detailLine
	Details []detailLine
}

// summaryOnly is the outcome of a tool whose whole result is one plain line: it rides the
// branch line beside the target and nothing hangs beneath it.
func summaryOnly(text string) toolOutcome {
	return toolOutcome{Summary: detailLine{Text: text}}
}

// toolPresenter maps a tool name to its friendly label, the active verb naming what the tool
// is doing while it runs, a header extractor that pulls the Target from the call's
// arguments, a prose extractor that turns a summary-less result into a [toolOutcome], and —
// for the one tool that has one — a body renderer for a result whose branch line comes from
// its typed summary. A nil extractor is valid (the tool has no target, or no summarisable
// result).
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

	// body renders the lines laid out BENEATH the branch when the summary supplied the
	// branch line itself. Exactly one entry sets it — view_diff, the only tool with both a
	// summary and a body — so the asymmetry is intentional, not an oversight.
	body func(content string) []detailLine
}

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
// full text either way.
var toolRegistry = map[string]toolPresenter{
	"read_file": {
		label:  "Read File",
		verb:   "reading",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the span comes from domain.ReadSpan
	},
	"write_file": {
		label:  "Write File",
		verb:   "writing",
		target: stringArg("path"),
		detail: firstLineDetail, // floor; the count comes from domain.WroteBytes
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
	"single_find_and_replace": {
		label:  "Edit File",
		verb:   "editing",
		target: stringArg("path"),
		detail: firstLineDetail, // "replaced text in <path>"
	},
	"multi_find_and_replace": {
		label:  "Edit File",
		verb:   "editing",
		target: stringArg("path"),
		detail: firstLineDetail, // "applied N replacements to <path>"
	},
	"edit_existing_file": {
		label:  "Edit File",
		verb:   "editing",
		target: stringArg("path"),
		detail: firstLineDetail, // "applied patch to <path> (N hunks)" / "updated <path>"
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
	"diagnostics": {
		label:  "Diagnostics",
		verb:   "checking",
		target: stringArg("path"),
		detail: outputDetail,
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
		target: firstLineArg("task"),
		detail: outputDetail, // the report's gist; the nested run already rendered railed
	},
	"ask_user": {
		label:  "Ask User",
		verb:   "asking",
		target: firstLineArg("question"),
		detail: firstLineDetail, // the user's own answer
	},
	"present_document": {
		label:  "Present",
		verb:   "presenting",
		target: stringArg("path"),
		detail: firstLineDetail, // "Presented <path>: opened on the user's machine."
	},
}

// presentToolCall builds the header view of a tool call. A known tool gets its friendly
// label, its active verb, and a target pulled from the arguments; an unknown tool falls back
// to its raw name (styled like any other label) with the pretty-printed arguments as plain
// detail lines, so a not-yet-registered tool still renders and a malformed argument is shown
// verbatim (the approval flow is a security surface — the model's request is never hidden).
// The verb mirrors that fallback: an unregistered tool is "running <raw name>", which stays a
// truthful sentence fragment for a dynamic MCP tool nobody has a verb for.
// Everything the header states traces back to the model's own JSON arguments — the target on every
// registered tool, and the raw name behind an unknown tool's label, verb and pretty-printed body —
// so both exits sanitize before the view leaves this function.
func presentToolCall(call domain.ToolCall) toolView {
	p, ok := toolRegistry[call.Tool]
	if !ok {
		tv := toolView{
			Label:   call.Tool,
			Verb:    "running " + call.Tool,
			name:    call.Tool,
			Details: newToolBody(prettyJSONDetails(call.Arguments)),
		}
		tv.sanitize()
		return tv
	}
	tv := toolView{Label: p.label, Verb: p.verb, name: call.Tool}
	if p.target != nil {
		tv.Target = p.target(parseArgs(call.Arguments))
	}
	tv.sanitize()
	return tv
}

// sanitize escape-strips every DISPLAY field of the view — label, verb, target, the one-line
// summary and each detail line — so no ESC byte from a tool call or its result can reach the
// terminal (stripEscapes). It is the tool card's seam, called once on the way out of
// presentToolCall and of enrichWithResult, rather than left to the two dozen target and detail
// extractors above to remember one at a time.
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
// The body's KIND is not this seam's business and never was a rule a body path had to remember: a
// body carries its own kind (toolBody), settled where its lines were, and the strip below rewrites
// only line text — so a sanitized body says exactly what it said before.
func (tv *toolView) sanitize() {
	tv.Label = stripEscapes(tv.Label)
	tv.Verb = stripEscapes(tv.Verb)
	tv.Target = stripEscapes(tv.Target)
	tv.Summary.Text = stripEscapes(tv.Summary.Text)
	tv.Details.stripEscapes()
}

// bodyIsDiff reports whether a body's lines carry a red/green diff line — the exact test for a diff
// body, since diffBody is the diff kinds' only producer and never emits an untagged body ("No
// changes detected" carries no diff at all and never reaches it). It runs once per body, in
// newToolBody; the painter reads the answer off the body (toolBody.isDiff) rather than asking
// again.
func bodyIsDiff(details []detailLine) bool {
	for _, d := range details {
		if d.Kind == detailDiffAdded || d.Kind == detailDiffRemoved {
			return true
		}
	}
	return false
}

// enrichWithResult folds a tool's result into the view, in three layers. An error result
// (the tool flagged it IsError — a normal in-band outcome the model reacts to) is the
// one-line summary, so an errored call still groups with its neighbours. A result carrying a
// typed domain.ToolSummary is worded by summaryLine, with the presenter's body renderer
// filling the half beneath it (view_diff alone). Everything else falls to prose: a known
// tool's extractor splits the text into a summary and a body, and an unknown tool's result is
// shown raw as body lines, so nothing is ever silently dropped.
//
// Every one of those layers words itself from result.Content, which is tool output and therefore
// repo-controlled, so the sanitize seam is deferred rather than repeated: it runs on whichever
// branch returns, and on any branch a later tool adds.
func (tv *toolView) enrichWithResult(result domain.ToolResult) {
	defer tv.sanitize()
	if result.IsError {
		tv.Summary = detailLine{Text: "error: " + firstLine(result.Content)}
		return
	}
	p, known := toolRegistry[tv.name]
	if line, ok := summaryLine(result.Summary); ok {
		tv.Summary = line
		if known && p.body != nil {
			tv.Details = tv.Details.with(p.body(result.Content))
		}
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
func firstLineDetail(content string) toolOutcome {
	return summaryOnly(clipDetail(firstLine(content)))
}

// outputDetail splits free-form output (a command run, a diagnostics report, a sub-agent
// report) into the half its own size dictates, keeping EVERY line it was given. Which half it
// fills follows the same rule as every other extractor: output that comes to exactly ONE
// non-empty-led line — a single-line result, or none at all — is that call's whole outcome and
// rides the branch beside the target ("┕ true (no output)"), which is also what keeps such
// calls grouping; output with more to say is a body and lays out beneath the target instead,
// because two lines cannot share a branch (layout.md's Run sketch).
//
// It truncates NOTHING. The collapsed paint's "first line plus … +N more lines" is a render-time
// act on this retained body (collapsedDetails, render.go), so expanding the block can show what
// the compact shape hides. Only the per-line clip stays here — a 160-rune cap on one line, which
// keeps a minified blob from flooding a row in either state and is not a truncation of the body.
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
		return summaryOnly(clipDetail(body[0]))
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
// starts with "+" always arrives behind a tag. It returns every line: the collapsed paint's
// diffDetailCap and its remainder marker are the painter's (collapsedDetails, render.go).
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

// detailClipRunes caps one detail/target line so a minified blob or a wall-of-text report
// cannot flood a row (the renderer soft-wraps, so an uncapped line becomes many rows).
const detailClipRunes = 160

// clipDetail truncates s to detailClipRunes runes with an ellipsis.
func clipDetail(s string) string {
	return clipRunes(s, detailClipRunes)
}

// clipRunes truncates s to n runes with an ellipsis, counting runes rather than bytes so a
// multi-byte path is not cut mid-character. The status line clips far tighter than the
// transcript does (statusTargetRunes), so the cap is a parameter rather than the one constant.
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

// prettyJSONDetails renders a tool call's arguments as the plain body of the unknown-tool
// fallback: the pretty-printed JSON (or the verbatim text when it does not parse) split into
// one detailLine per line. It is body-only by construction — an unregistered tool has no
// target, so the block takes the targetless shape and these lines are the branches themselves
// (render.go). Empty/null arguments add no lines.
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
