package tui

import (
	"encoding/json"
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
// emitted by diffDetail (the view_diff extractor) and rendered red/green in render.go.
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
	Details []detailLine

	name string
}

// toolOutcome is what a result extractor returns: the one-line Summary that rides the branch
// line beside the target, and the Details body laid out beneath it. Either half may be empty
// — a fixed result header is summary-only ("1 - 154"), free-form output is body-only (its
// first line plus the remainder count), and view_diff is both (a "+2 -2" diffstat over a body
// of coloured diff lines).
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
// arguments, and a detail extractor that turns the tool's result into a [toolOutcome] — the
// one-line summary, the body beneath it, or both. A nil extractor is valid (the tool has no
// target or no summarisable result).
//
// label and verb are two views of the same tool for two places: label titles the finished
// header line ("Read File"), verb is the lowercase present participle the live status line
// reads as a sentence fragment ("reading main.go") — never a title.
type toolPresenter struct {
	label  string
	verb   string
	target func(args map[string]any) string
	detail func(content string) toolOutcome
}

// toolRegistry is the open, name-keyed catalogue. Each later tool adds one entry here; the
// renderer and the transcript never grow a per-tool branch. It covers the full built-in set
// (internal/tools DefaultToolsWithHost); only a dynamic tool (an MCP server's) falls to the
// raw-name fallback. Fixed result headers are parsed with small, anchored patterns;
// free-form output (a command run, a sub-agent report) is compressed to its first line plus
// a remainder count — the chat shows the gist, the model still gets the full text.
var toolRegistry = map[string]toolPresenter{
	"read_file": {
		label:  "Read File",
		verb:   "reading",
		target: stringArg("path"),
		detail: detailFromPattern(reReadRange, func(m []string) string { return m[1] + " - " + m[2] }),
	},
	"write_file": {
		label:  "Write File",
		verb:   "writing",
		target: stringArg("path"),
		detail: detailFromPattern(reWriteBytes, func(m []string) string { return "+" + m[1] + " bytes" }),
	},
	"list_dir": {
		label:  "List Dir",
		verb:   "listing",
		target: stringArg("path"),
		detail: detailFromPattern(reListEntries, func(m []string) string { return m[1] + " entries" }),
	},
	"grep": {
		label:  "Search",
		verb:   "searching",
		target: stringArg("pattern"),
		detail: grepDetail,
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
		detail: diffDetail,
	},
	"open_file": {
		label:  "Open File",
		verb:   "opening",
		target: stringArg("path"),
		detail: openFileDetail,
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
		detail: searchDetail,
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

// The fixed result headers the default tools emit (internal/tools). The patterns are
// anchored to the documented shapes, not free text, so an unexpected result falls through to
// the verbatim-first-line fallback rather than mis-summarising.
var (
	reReadRange   = regexp.MustCompile(`showing lines (\d+)-(\d+)`)
	reWriteBytes  = regexp.MustCompile(`wrote (\d+) bytes`)
	reListEntries = regexp.MustCompile(`\[(\d+) entries total`)
	reGrepMatches = regexp.MustCompile(`\[(\d+) total matches`)
)

// presentToolCall builds the header view of a tool call. A known tool gets its friendly
// label, its active verb, and a target pulled from the arguments; an unknown tool falls back
// to its raw name (styled like any other label) with the pretty-printed arguments as plain
// detail lines, so a not-yet-registered tool still renders and a malformed argument is shown
// verbatim (the approval flow is a security surface — the model's request is never hidden).
// The verb mirrors that fallback: an unregistered tool is "running <raw name>", which stays a
// truthful sentence fragment for a dynamic MCP tool nobody has a verb for.
func presentToolCall(call domain.ToolCall) toolView {
	p, ok := toolRegistry[call.Tool]
	if !ok {
		return toolView{
			Label:   call.Tool,
			Verb:    "running " + call.Tool,
			name:    call.Tool,
			Details: prettyJSONDetails(call.Arguments),
		}
	}
	tv := toolView{Label: p.label, Verb: p.verb, name: call.Tool}
	if p.target != nil {
		tv.Target = p.target(parseArgs(call.Arguments))
	}
	return tv
}

// enrichWithResult folds a tool's result into the view. An error result (the tool flagged it
// IsError — a normal in-band outcome the model reacts to) is the one-line summary, so an
// errored call still groups with its neighbours. A known tool's result is split by its
// extractor into that summary and the body beneath it; an unknown tool's result is shown raw
// as body lines (nothing is hidden); an unparseable known result falls back to its verbatim
// first line.
func (tv *toolView) enrichWithResult(result domain.ToolResult) {
	if result.IsError {
		tv.Summary = detailLine{Text: "error: " + firstLine(result.Content)}
		return
	}
	if p, ok := toolRegistry[tv.name]; ok && p.detail != nil {
		out := p.detail(result.Content)
		tv.Summary = out.Summary
		tv.Details = append(tv.Details, out.Details...)
		return
	}
	// Unknown (or summary-less) tool: surface the raw result so it is never silently dropped.
	for _, ln := range splitLines(strings.TrimRight(result.Content, "\n")) {
		tv.Details = append(tv.Details, detailLine{Text: ln})
	}
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

// detailFromPattern returns a detail extractor that runs re against the result's first line
// and formats the submatches with build. The result is one line, so it is the branch-riding
// summary with no body. A non-match falls back to the verbatim first line, so an unexpected
// result is shown rather than summarised away.
func detailFromPattern(re *regexp.Regexp, build func(match []string) string) func(string) toolOutcome {
	return func(content string) toolOutcome {
		head := firstLine(content)
		if m := re.FindStringSubmatch(head); m != nil {
			return summaryOnly(build(m))
		}
		return summaryOnly(head)
	}
}

// grepDetail summarises a grep result: the "No matches found" sentinel becomes "0 matches",
// and the "[N total matches…]" header becomes "N matches".
func grepDetail(content string) toolOutcome {
	head := firstLine(content)
	if strings.HasPrefix(head, "No matches") {
		return summaryOnly("0 matches")
	}
	if m := reGrepMatches.FindStringSubmatch(head); m != nil {
		return summaryOnly(m[1] + " matches")
	}
	return summaryOnly(head)
}

// firstLineDetail summarises a result to its first line, clipped — for tools whose result
// is a short fixed sentence ("updated main.go") or opens with a status header ("HTTP 200
// OK"): one line carries the outcome, the rest is the model's food, not the chat's.
func firstLineDetail(content string) toolOutcome {
	return summaryOnly(clipDetail(firstLine(content)))
}

// outputDetail compresses free-form output (a command run, a diagnostics report, a
// sub-agent report) to its first non-empty line plus a remainder count. Which half it fills
// follows the same rule as every other extractor: output that compresses to exactly ONE line
// — a single-line result, or none at all — is that call's whole outcome and rides the branch
// beside the target ("┕ true (no output)"), which is also what keeps such calls grouping;
// output that needs the "… +N more lines" remainder is a body and lays out beneath the target
// instead, because two lines cannot share a branch (layout.md's Run sketch). The full text
// still reaches the model; the chat shows the gist.
func outputDetail(content string) toolOutcome {
	lines := splitLines(strings.TrimRight(content, "\n"))
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first == len(lines) {
		return summaryOnly("(no output)")
	}
	head := clipDetail(lines[first])
	rest := len(lines) - first - 1
	if rest == 0 {
		return summaryOnly(head)
	}
	return toolOutcome{Details: []detailLine{
		{Text: head},
		{Text: "… +" + plural(rest, "more line")},
	}}
}

// searchDetail summarises web_search: a structured render (numbered "N. title" hits)
// becomes a result count; anything else — "No results found for: …", the disabled notice, a
// custom backend's pass-through — falls back to its first line.
func searchDetail(content string) toolOutcome {
	if n := len(reSearchHit.FindAllString(content, -1)); n > 0 {
		return summaryOnly(plural(n, "result"))
	}
	return firstLineDetail(content)
}

// reSearchHit matches the numbered result lines of web_search's structured render.
var reSearchHit = regexp.MustCompile(`(?m)^\d+\. `)

// diffDetailCap bounds how many diff lines reach the chat — enough to read a focused
// change, not enough for a rewrite to flood the transcript.
const diffDetailCap = 20

// diffDetail splits view_diff's unified output into the "+A -R" diffstat that rides the
// branch beside the path and the coloured body beneath it — "+ " lines green, "- " lines red,
// context plain — capped at diffDetailCap with a remainder count (layout.md's Update File
// sketch). The diffstat counts the WHOLE diff, not just the lines that survive the cap: it is
// the one number a truncated body cannot tell you. Counting on the leading "+"/"-" is exact
// here because internal/tools' unifiedLineDiff tags every line "  ", "- " or "+ " and emits
// no "+++ b/…" / "--- a/…" file header, so a content line that itself starts with "+" always
// arrives behind a tag. A diff with neither an addition nor a removal is not a diff at all —
// the "No changes detected" sentinel — and passes through as its single plain summary line.
func diffDetail(content string) toolOutcome {
	lines := splitLines(strings.TrimRight(content, "\n"))
	added, removed := 0, 0
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "+"):
			added++
		case strings.HasPrefix(ln, "-"):
			removed++
		}
	}
	if added == 0 && removed == 0 {
		return summaryOnly(clipDetail(firstLine(content)))
	}

	body := make([]detailLine, 0, min(len(lines), diffDetailCap+1))
	for i, ln := range lines {
		if i == diffDetailCap {
			body = append(body, detailLine{Text: "… +" + plural(len(lines)-i, "more line")})
			break
		}
		kind := detailPlain
		switch {
		case strings.HasPrefix(ln, "+"):
			kind = detailDiffAdded
		case strings.HasPrefix(ln, "-"):
			kind = detailDiffRemoved
		}
		body = append(body, detailLine{Kind: kind, Text: clipDetail(ln)})
	}
	return toolOutcome{
		Summary: detailLine{Text: "+" + strconv.Itoa(added) + " -" + strconv.Itoa(removed)},
		Details: body,
	}
}

// openFileDetail summarises open_file: the "Located …" line when a locate was requested
// (the interesting outcome), otherwise the content's line count — the header's "File: …"
// repeats the target and the content itself belongs to the model.
func openFileDetail(content string) toolOutcome {
	lines := splitLines(content)
	if len(lines) > 1 && strings.HasPrefix(lines[1], "Located ") {
		return summaryOnly(clipDetail(lines[1]))
	}
	n := len(lines) - 2 // the "File: …" header and its blank separator precede the content
	if n < 0 {
		n = 0
	}
	return summaryOnly(plural(n, "line"))
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
