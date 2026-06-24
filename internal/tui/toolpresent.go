package tui

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Tool presentation (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// This file turns a tool call+result into a compact, human-facing view: a friendly label
// and a target on the header line (✦ [Read File] main.go), and a one-line summary hanging
// off a tree branch (┕ 1 - 100). It is pure — no lipgloss, no I/O — so it is trivially
// table-testable (TestPresentToolCall); render.go owns the styling.
//
// The label+extractor map is an OPEN, name-keyed registry, not a closed switch: the Phase-3
// tool fan-out (P3.7–P3.11, ~30 tools, ADR 0002) adds one entry per tool (terminal→"Run",
// git→"Git", find_replace→"Edit File", …) rather than editing a control-flow statement. An
// unknown tool falls back to its raw name and pretty-printed arguments, so a tool with no
// registry entry still renders legibly.

// detailKind tags a tool-detail line so the renderer can colour it. The diff kinds are
// defined and rendered (render.go) but have no producer yet: an edit/diff tool arrives with
// the P3.7 file-editing family, at which point a detail extractor emits them. Until then they
// are a reserved seam (the same posture as the sub-agent block).
type detailKind int

const (
	detailPlain detailKind = iota
	detailDiffAdded
	detailDiffRemoved
)

// detailLine is one branch line under a tool header — a short summary (detailPlain) or, once
// an edit tool exists, a red/green diff line (detailDiffAdded/detailDiffRemoved).
type detailLine struct {
	Kind detailKind
	Text string
}

// toolView is the presentation model of a tool call (later enriched by its result): a
// friendly Label, the Target it acts on (a path, a directory, a pattern), and the detail
// lines summarising the outcome. name is the raw tool id, kept to pick the result extractor
// and as the raw-fallback label; bracket reports whether the label is a known friendly one
// the renderer wraps in [brackets] (a raw fallback is shown bare).
type toolView struct {
	Label   string
	Target  string
	Details []detailLine

	name    string
	bracket bool
}

// toolPresenter maps a tool name to its friendly label, a header extractor that pulls the
// Target from the call's arguments, and a detail extractor that parses the tool's fixed
// result header into summary lines. A nil extractor is valid (the tool has no target or no
// summarisable result).
type toolPresenter struct {
	label  string
	target func(args map[string]any) string
	detail func(content string) []detailLine
}

// toolRegistry is the open, name-keyed catalogue. Each later tool adds one entry here; the
// renderer and the transcript never grow a per-tool branch. The four entries below are the
// Phase-2 default tools (internal/tools); their result headers are fixed strings, so the
// detail extractors parse them with small, anchored patterns.
var toolRegistry = map[string]toolPresenter{
	"read_file": {
		label:  "Read File",
		target: stringArg("path"),
		detail: detailFromPattern(reReadRange, func(m []string) string { return m[1] + " - " + m[2] }),
	},
	"write_file": {
		label:  "Write File",
		target: stringArg("path"),
		detail: detailFromPattern(reWriteBytes, func(m []string) string { return "+" + m[1] + " bytes" }),
	},
	"list_dir": {
		label:  "List Dir",
		target: stringArg("path"),
		detail: detailFromPattern(reListEntries, func(m []string) string { return m[1] + " entries" }),
	},
	"grep": {
		label:  "Search",
		target: stringArg("pattern"),
		detail: grepDetail,
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
// label and a target pulled from the arguments; an unknown tool falls back to its raw name
// (shown bare, not bracketed) with the pretty-printed arguments as plain detail lines, so a
// not-yet-registered tool still renders and a malformed argument is shown verbatim (the
// approval flow is a security surface — the model's request is never hidden).
func presentToolCall(call domain.ToolCall) toolView {
	p, ok := toolRegistry[call.Tool]
	if !ok {
		return toolView{Label: call.Tool, name: call.Tool, Details: prettyJSONDetails(call.Arguments)}
	}
	tv := toolView{Label: p.label, name: call.Tool, bracket: true}
	if p.target != nil {
		tv.Target = p.target(parseArgs(call.Arguments))
	}
	return tv
}

// enrichWithResult folds a tool's result into the view as summary detail lines. An error
// result (the tool flagged it IsError — a normal in-band outcome the model reacts to) shows
// the error text. A known tool's result is summarised by its extractor; an unknown tool's
// result is shown raw (nothing is hidden); an unparseable known result falls back to its
// verbatim first line.
func (tv *toolView) enrichWithResult(result domain.ToolResult) {
	if result.IsError {
		tv.Details = append(tv.Details, detailLine{Text: "error: " + firstLine(result.Content)})
		return
	}
	if p, ok := toolRegistry[tv.name]; ok && p.detail != nil {
		tv.Details = append(tv.Details, p.detail(result.Content)...)
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
// non-string value yields the empty target (the header then shows just the label).
func stringArg(key string) func(map[string]any) string {
	return func(args map[string]any) string {
		if v, ok := args[key].(string); ok {
			return v
		}
		return ""
	}
}

// detailFromPattern returns a detail extractor that runs re against the result's first line
// and formats the submatches with build. A non-match falls back to the verbatim first line,
// so an unexpected result is shown rather than summarised away.
func detailFromPattern(re *regexp.Regexp, build func(match []string) string) func(string) []detailLine {
	return func(content string) []detailLine {
		head := firstLine(content)
		if m := re.FindStringSubmatch(head); m != nil {
			return []detailLine{{Text: build(m)}}
		}
		return []detailLine{{Text: head}}
	}
}

// grepDetail summarises a grep result: the "No matches found" sentinel becomes "0 matches",
// and the "[N total matches…]" header becomes "N matches".
func grepDetail(content string) []detailLine {
	head := firstLine(content)
	if strings.HasPrefix(head, "No matches") {
		return []detailLine{{Text: "0 matches"}}
	}
	if m := reGrepMatches.FindStringSubmatch(head); m != nil {
		return []detailLine{{Text: m[1] + " matches"}}
	}
	return []detailLine{{Text: head}}
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

// prettyJSONDetails renders a tool call's arguments as plain detail lines for the
// unknown-tool fallback: the pretty-printed JSON (or the verbatim text when it does not
// parse) split into one detailLine per line. Empty/null arguments add no lines.
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
