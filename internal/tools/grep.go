package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/airiclenz/apogee/internal/domain"
)

var grepSpec = toolSpec{
	name:        "grep",
	description: "Search workspace files for lines matching a regular expression. Returns file:line:text matches (a line wider than 512 characters is clipped around its first match); with context_lines set, the surrounding lines ride along as file:line-text; an absolute path under a configured read-only root (such as the skills library) can be searched too.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["pattern"],
  "properties": {
    "pattern": {"type": "string", "description": "Regular expression to search for (a literal substring if it is not a valid regex)"},
    "path": {"type": "string", "description": "File or directory to search within, relative to the workspace root or absolute (default: the whole workspace)"},
    "include": {"type": "string", "description": "Comma-separated file-name globs to include, e.g. \"*.go,*.md\" (default: all files)"},
    "max_results": {"type": "integer", "description": "Maximum matches to return (default 50)"},
    "offset": {"type": "integer", "description": "Number of matches to skip for pagination (default 0)"},
    "context_lines": {"type": "integer", "description": "Lines of context to show before AND after each match (default 0, clamped to 10). Context lines are shown as file:line-text and do not count against max_results"}
  }
}`),
}

type grepArgs struct {
	Pattern      string `json:"pattern"`
	Path         string `json:"path"`
	Include      string `json:"include"`
	MaxResults   int    `json:"max_results"`
	Offset       int    `json:"offset"`
	ContextLines int    `json:"context_lines"`
}

// maxGrepMatches bounds the matches grep collects across the whole tree, so a broad
// pattern on a large workspace cannot exhaust memory; the result notes truncation.
const maxGrepMatches = 1000

// grepCapNudge closes the header once the match cap bit: the model is told HOW to get under
// it rather than left to page through a capped total. It names the arguments grep honours
// today — naming one it silently ignores (decodeArgs drops unknown fields) would announce a
// knob that does nothing.
const grepCapNudge = " — narrow with include or path"

// maxGrepRowChars bounds the characters of a matched line that reach the model. A minified
// bundle or a `.jsonl` record can put tens of thousands of characters on one line, and a
// match inside one is worth its neighbourhood, not the whole row: a wider line is clipped
// to this many characters around its first match, `…` marking each cut end, and the header
// counts the rows it clipped.
const maxGrepRowChars = 512

// clipMark marks a cut end of a clipped row.
const clipMark = "…"

// maxGrepContextLines bounds context_lines. A larger request is silently narrowed rather
// than refused — the number is the model's hint about how much surrounding code it wants,
// not a contract worth failing a search over.
const maxGrepContextLines = 10

// grepMatch is one matching line. openPath is the workspace-relative name the file is
// opened by through the fence; display is the name the match is REPORTED under (relative to
// the searched directory, which differs from openPath whenever `path` names a subtree);
// line is 1-based; column is the byte offset of the first match in text, which is where a
// clipped row is centred.
type grepMatch struct {
	openPath string
	display  string
	line     int
	column   int
	text     string
}

// lineSpan is an inclusive, 1-based run of lines to print — a match plus its context, after
// overlapping and touching runs have been merged.
type lineSpan struct {
	from int
	to   int
}

// grepExcludeDirs are directories grep never descends into — version-control and
// build-output noise (ported from the TS oracle).
var grepExcludeDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, "coverage": true, "__pycache__": true,
}

// errGrepStop unwinds the WalkDir once the match cap is reached.
var errGrepStop = errors.New("grep: match cap reached")

// Grep searches workspace files for lines matching a regular expression, in pure Go
// (io/fs walk + regexp — no external grep, §3a). It is a read-only tool scoped to a
// sandbox root plus any extra read-only roots the host mounted (readScope): the walk
// enumerates names, but every file it reads is opened THROUGH the fence of the root the
// search path was accepted under, so a symlink pointing out of that root yields nothing.
type Grep struct {
	toolSpec
	scope readScope
}

// NewGrep returns a grep tool that resolves paths within root, and — for ABSOLUTE paths only —
// within any extra read-only root mounts reports at call time, plus any virtual mount it names.
// A zero ReadMounts means workspace-only: byte-identical to the fence before either mount seam
// existed.
func NewGrep(root string, mounts ReadMounts) *Grep {
	return &Grep{toolSpec: grepSpec, scope: mounts.scope(root)}
}

// ReadOnly reports that grep performs no writes (domain.ReadOnlyTool).
func (t *Grep) ReadOnly() bool { return true }

// Execute searches the file or directory named in call.Arguments, honouring ctx
// cancellation. A pattern that is not a valid regex is treated as a literal substring;
// a missing path or a path escaping every root is an IsError result.
//
// The search path is resolved over the workspace root first, then — for an ABSOLUTE path only —
// over the configured extra read-only roots, and the WHOLE search is pinned to the root that
// accepted it: names are measured from that root and every file is opened through its fence,
// never the workspace's.
func (t *Grep) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[grepArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Pattern == "" {
		return errorResult(call.ID, "pattern is required"), nil
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		re = regexp.MustCompile(regexp.QuoteMeta(args.Pattern)) // fall back to a literal match
	}

	searchPath := args.Path
	if searchPath == "" {
		searchPath = "."
	}
	target, refusal := t.scope.searchTarget(searchPath, args.Path)
	if refusal != "" {
		return errorResult(call.ID, refusal), nil
	}

	globs := parseIncludeGlobs(args.Include)
	matches, err := t.search(ctx, target, re, globs)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}

	scope := searchScope(args.Path, globs)
	text, matched := t.renderMatches(target, scope, matches, args.MaxResults, args.Offset, clampContextLines(args.ContextLines))
	return okSummary(call.ID, text, matched), nil
}

// clampContextLines pins a requested context width to 0–maxGrepContextLines.
func clampContextLines(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxGrepContextLines {
		return maxGrepContextLines
	}
	return n
}

// search collects matches from a single file or by walking a directory. target carries the tree to
// enumerate and the opener every file is read through (searchTarget, path_read.go): the tree is
// used to ENUMERATE names only — a name is opened through its own source's fence, never by an
// absolute path the walk handed out.
func (t *Grep) search(ctx context.Context, target searchTarget, re *regexp.Regexp, globs []string) ([]grepMatch, error) {
	matches := make([]grepMatch, 0, defaultGrepResults)

	if !target.isDir() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t.searchFile(target, target.rel, target.rel, re, &matches)
		return matches, nil
	}

	walkErr := fs.WalkDir(target.tree, ".", func(rel string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the whole search
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && grepExcludeDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !matchesInclude(entry.Name(), globs) {
			return nil
		}
		t.searchFile(target, target.openName(rel), rel, re, &matches)
		if len(matches) >= maxGrepMatches {
			return errGrepStop
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errGrepStop) {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return nil, walkErr
		}
	}
	return matches, nil
}

// searchFile appends a grepMatch for every matching line in the file named by rel within
// root, skipping a file that is oversized or binary (looksBinary — the sniff read_file shares,
// so the two tools never disagree about which files are text).
//
// rel is opened THROUGH the target's own fence (target.open — os.Root-pinned on disk), so a walked entry
// that is a symlink out of that root — a clone can plant `notes.txt -> ~/.ssh/id_rsa`, and
// grep is read-only, hence unapproved in every mode — is refused rather than followed, and
// the size bound is an fstat of the very descriptor the content is then read from. A search
// that began under an extra read-only root is fenced by THAT root, so its files never open
// through the workspace's. A refusal is skipped like any other unreadable file: grep reports
// matches, not an inventory, so a silently absent file is the existing contract.
func (t *Grep) searchFile(target searchTarget, rel, display string, re *regexp.Regexp, matches *[]grepMatch) {
	file, err := target.open(rel)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	if info, err := file.Stat(); err != nil || info.IsDir() || info.Size() > maxGrepFileBytes {
		return
	}

	reader := bufio.NewReader(file)
	if sniff, _ := reader.Peek(binarySniffBytes); looksBinary(sniff) {
		return
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxGrepFileBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if loc := re.FindStringIndex(line); loc != nil {
			*matches = append(*matches, grepMatch{openPath: rel, display: display, line: lineNumber, column: loc[0], text: line})
			if len(*matches) >= maxGrepMatches {
				return
			}
		}
	}
}

// parseIncludeGlobs splits a comma-separated include argument into trimmed globs; an
// empty argument means "every file".
func parseIncludeGlobs(include string) []string {
	if strings.TrimSpace(include) == "" {
		return nil
	}
	globs := make([]string, 0)
	for _, part := range strings.Split(include, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			globs = append(globs, trimmed)
		}
	}
	return globs
}

// searchScope names the scope a search ran over, for the header and the no-match sentence grep
// and find_files write. given is the `path` argument AS THE MODEL SPELLED IT — an announced value
// the model can hand straight back — and an empty or "." path is the whole workspace; the glob
// list that narrowed the search rides along in parentheses. The scope is data inside the header
// row's grammar, so it is escaped like a path: a scope carrying a line break would forge rows.
func searchScope(given string, globs []string) string {
	scope := strings.TrimSpace(given)
	if scope == "" || scope == "." {
		scope = "the workspace"
	}
	if len(globs) > 0 {
		scope += " (" + strings.Join(globs, ",") + ")"
	}
	return escapeRowBreaks(scope)
}

// matchesInclude reports whether a file name matches any include glob; no globs means
// every file matches.
func matchesInclude(name string, globs []string) bool {
	if len(globs) == 0 {
		return true
	}
	for _, glob := range globs {
		if ok, _ := filepath.Match(glob, name); ok {
			return true
		}
	}
	return false
}

// renderMatches paginates from offset and prepends a header naming the total count and the
// scope the search ran over (searchScope), plus how many of the page's rows were clipped
// (clipWideRows) and, once the match cap bit, the nudge that says how to narrow the search
// (grepCapNudge). It returns the total as a domain.MatchedLines on
// BOTH paths — a search that found nothing reports Total 0 rather than no summary, so a host
// reads a number instead of testing the "No matches found" sentence for a prefix.
//
// Pagination counts MATCHES only: contextLines rides along free, so the header's numbers and
// the truncation note mean the same thing whether context was asked for or not. With
// contextLines 0 the body is byte-identical to the pre-context output.
func (t *Grep) renderMatches(target searchTarget, scope string, matches []grepMatch, maxResults, offset, contextLines int) (string, domain.MatchedLines) {
	if len(matches) == 0 {
		return "No matches found in " + scope, domain.MatchedLines{Total: 0}
	}
	if maxResults <= 0 {
		maxResults = defaultGrepResults
	}
	if offset < 0 {
		offset = 0
	}

	total := len(matches)
	start := offset
	if start > total {
		start = total
	}
	end := start + maxResults
	if end > total {
		end = total
	}
	shown, clipped := clipWideRows(matches[start:end])

	capped, nudge := "", ""
	if total >= maxGrepMatches {
		capped = fmt.Sprintf(" (capped at %d)", maxGrepMatches)
		nudge = grepCapNudge
	}
	clippedNote := ""
	if clipped > 0 {
		clippedNote = fmt.Sprintf(" (%d rows clipped at %d chars)", clipped, maxGrepRowChars)
	}
	header := fmt.Sprintf("[%d total matches%s in %s, showing %d-%d%s]%s", total, capped, scope, start+1, end, clippedNote, nudge)
	body := plainMatchLines(shown)
	if contextLines > 0 {
		body = t.renderContextMatches(target, shown, contextLines)
	}
	return header + "\n" + strings.Join(body, "\n"), domain.MatchedLines{Total: total}
}

// clipWideRows returns a copy of the page's matches with every row wider than maxGrepRowChars
// clipped around its first match (clipRow), and how many rows it clipped. It runs BEFORE the
// renderers, so a wide row is clipped the same way whether or not context was asked for; a
// page with no wide row is handed back as it was.
func clipWideRows(matches []grepMatch) ([]grepMatch, int) {
	clipped := 0
	var out []grepMatch
	for i, m := range matches {
		text, cut := clipRow(m.text, m.column)
		if !cut {
			continue
		}
		if out == nil {
			out = make([]grepMatch, len(matches))
			copy(out, matches)
		}
		out[i].text = text
		clipped++
	}
	if out == nil {
		return matches, 0
	}
	return out, clipped
}

// clipRow clips a matched line wider than maxGrepRowChars to a window of that many characters
// centred on the byte offset column of its first match, with clipMark at each cut end. It
// counts characters, not bytes, so a multibyte rune is never split. A line within the bound is
// returned as it is with false.
func clipRow(text string, column int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= maxGrepRowChars {
		return text, false
	}
	matchAt := utf8.RuneCountInString(text[:min(column, len(text))])
	start := matchAt - maxGrepRowChars/2
	if start > len(runes)-maxGrepRowChars {
		start = len(runes) - maxGrepRowChars
	}
	if start < 0 {
		start = 0
	}
	end := start + maxGrepRowChars

	var b strings.Builder
	if start > 0 {
		b.WriteString(clipMark)
	}
	b.WriteString(string(runes[start:end]))
	if end < len(runes) {
		b.WriteString(clipMark)
	}
	return b.String(), true
}

// plainMatchLines renders matches in the bare "display:line:text" form — the output shape
// grep has always had, and the one contextLines 0 must still produce exactly.
func plainMatchLines(matches []grepMatch) []string {
	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		// Only the display PATH is escaped: m.text is the row's payload, already split on
		// the line breaks the file held, while the path is data inside the row's grammar.
		lines = append(lines, fmt.Sprintf("%s:%d:%s", escapeRowBreaks(m.display), m.line, m.text))
	}
	return lines
}

// renderContextMatches renders matches with contextLines lines of context on each side.
// Matches from one file arrive contiguously (each file is scanned once and pagination keeps
// the order), so a file's group is a consecutive run and its context is gathered in a single
// reopen of that file.
func (t *Grep) renderContextMatches(target searchTarget, matches []grepMatch, contextLines int) []string {
	lines := make([]string, 0, len(matches)*(2*contextLines+1))
	for start := 0; start < len(matches); {
		end := start + 1
		for end < len(matches) && matches[end].openPath == matches[start].openPath {
			end++
		}
		lines = append(lines, t.renderFileGroup(target, matches[start:end], contextLines)...)
		start = end
	}
	return lines
}

// renderFileGroup renders one file's matches plus their merged context spans, separating
// non-adjacent spans with the conventional "--" line. A file whose context cannot be reread
// (deleted or newly refused since the match pass) degrades to bare match lines rather than
// failing the whole search.
func (t *Grep) renderFileGroup(target searchTarget, group []grepMatch, contextLines int) []string {
	spans := mergeContextSpans(group, contextLines)
	surrounding := t.readContextLines(target, group[0].openPath, spans)
	if surrounding == nil {
		return plainMatchLines(group)
	}

	matched := make(map[int]string, len(group))
	for _, m := range group {
		matched[m.line] = m.text
	}

	// The path is data inside the "path:line:text" grammar; a break in it would forge rows.
	display := escapeRowBreaks(group[0].display)
	lines := make([]string, 0, len(group))
	for i, span := range spans {
		if i > 0 {
			lines = append(lines, "--")
		}
		for n := span.from; n <= span.to; n++ {
			if text, ok := matched[n]; ok {
				lines = append(lines, fmt.Sprintf("%s:%d:%s", display, n, text))
				continue
			}
			if text, ok := surrounding[n]; ok {
				lines = append(lines, fmt.Sprintf("%s:%d-%s", display, n, text))
			}
		}
	}
	return lines
}

// mergeContextSpans turns each match's ±contextLines window into disjoint, non-touching
// spans in ascending order (mergeLineWindows over the matches' line numbers). Matches arrive
// in ascending line order (one forward scan per file).
func mergeContextSpans(group []grepMatch, contextLines int) []lineSpan {
	lines := make([]int, len(group))
	for i, m := range group {
		lines[i] = m.line
	}
	return mergeLineWindows(lines, contextLines)
}

// mergeLineWindows turns each 1-based line number's ±contextLines window into disjoint,
// non-touching spans in ascending order. Windows that overlap OR merely touch are merged, so
// no line is ever printed twice and a separator only ever falls on a real gap. The numbers
// must arrive ascending. A span's `to` is NOT clipped to the file's length — grep reads its
// context by line number and never asks for a line past the end, and read_file's locate
// windows clip it against the lines they hold (locateWindows).
func mergeLineWindows(lineNumbers []int, contextLines int) []lineSpan {
	spans := make([]lineSpan, 0, len(lineNumbers))
	for _, n := range lineNumbers {
		from := n - contextLines
		if from < 1 {
			from = 1
		}
		span := lineSpan{from: from, to: n + contextLines}
		if last := len(spans) - 1; last >= 0 && span.from <= spans[last].to+1 {
			if span.to > spans[last].to {
				spans[last].to = span.to
			}
			continue
		}
		spans = append(spans, span)
	}
	return spans
}

// readContextLines reads the lines covered by spans from the file named by openPath within
// the target, keyed by 1-based line number. The file is reopened THROUGH the same fence
// (target.open), exactly as the match pass opened it, so context can never be gathered
// from a file the fence refuses; a refused or vanished file yields nil. Lines in the gaps
// BETWEEN spans are scanned past, never retained, so two far-apart matches in a large file
// do not pull the whole file into memory.
func (t *Grep) readContextLines(target searchTarget, openPath string, spans []lineSpan) map[int]string {
	file, err := target.open(openPath)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	surrounding := make(map[int]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxGrepFileBytes)
	span := 0
	for n := 1; scanner.Scan(); n++ {
		for span < len(spans) && n > spans[span].to {
			span++
		}
		if span >= len(spans) {
			break
		}
		if n >= spans[span].from {
			surrounding[n] = scanner.Text()
		}
	}
	return surrounding
}

var _ domain.ReadOnlyTool = (*Grep)(nil)
