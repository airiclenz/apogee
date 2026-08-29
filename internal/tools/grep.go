package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var grepSpec = toolSpec{
	name:        "grep",
	description: "Search workspace files for lines matching a regular expression. Returns file:line:text matches; with context_lines set, the surrounding lines ride along as file:line-text; an absolute path under a configured read-only root (such as the skills library) can be searched too.",
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

// maxGrepContextLines bounds context_lines. A larger request is silently narrowed rather
// than refused — the number is the model's hint about how much surrounding code it wants,
// not a contract worth failing a search over.
const maxGrepContextLines = 10

// grepMatch is one matching line. openPath is the workspace-relative name the file is
// opened by through the fence; display is the name the match is REPORTED under (relative to
// the searched directory, which differs from openPath whenever `path` names a subtree);
// line is 1-based.
type grepMatch struct {
	openPath string
	display  string
	line     int
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
// within any extra read-only root extraReadRoots reports at call time. A nil extraReadRoots
// means workspace-only: byte-identical to the fence before extra roots existed.
func NewGrep(root string, extraReadRoots func() []string) *Grep {
	return &Grep{toolSpec: grepSpec, scope: readScope{root: root, extra: extraReadRoots}}
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
	root, resolved, err := t.scope.resolve(searchPath)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return errorResult(call.ID, "path not found: "+args.Path), nil
	}

	globs := parseIncludeGlobs(args.Include)
	matches, err := t.search(ctx, root, resolved, info, re, globs)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}

	scope := searchScope(args.Path, globs)
	text, matched := t.renderMatches(root, scope, matches, args.MaxResults, args.Offset, clampContextLines(args.ContextLines))
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

// search collects matches from a single file or by walking a directory. root is the root the
// search path was accepted under and target is its resolved absolute path; target is used to
// ENUMERATE names only — every file is opened by its root-relative name through that root's
// fence, never by an absolute path the walk handed out.
func (t *Grep) search(ctx context.Context, root, target string, info os.FileInfo, re *regexp.Regexp, globs []string) ([]grepMatch, error) {
	matches := make([]grepMatch, 0, defaultGrepResults)
	// The name is measured from the MATCHED root, symlink-resolved by the shared guard:
	// resolveInRoot hands back real paths, so on a host where a root is reached through a link
	// (macOS's /tmp, a symlinked /home) the raw root is a prefix of nothing.
	targetRel := workspaceRelative(target, root)

	if !info.IsDir() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t.searchFile(root, targetRel, targetRel, re, &matches)
		return matches, nil
	}

	// prefix lifts a walk-relative name to a root-relative one ("" when the search
	// root IS the matched root). fs.WalkDir yields slash-separated names, so the join
	// is path.Join, not filepath.Join.
	prefix := filepath.ToSlash(targetRel)
	if prefix == "." {
		prefix = ""
	}

	walkErr := fs.WalkDir(os.DirFS(target), ".", func(rel string, entry fs.DirEntry, err error) error {
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
		t.searchFile(root, path.Join(prefix, rel), rel, re, &matches)
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
// root, skipping a file that is oversized or binary (contains a NUL byte in its leading
// bytes).
//
// rel is opened THROUGH root's fence (os.Root-pinned, security.SafeOpen), so a walked entry
// that is a symlink out of that root — a clone can plant `notes.txt -> ~/.ssh/id_rsa`, and
// grep is read-only, hence unapproved in every mode — is refused rather than followed, and
// the size bound is an fstat of the very descriptor the content is then read from. A search
// that began under an extra read-only root is fenced by THAT root, so its files never open
// through the workspace's. A refusal is skipped like any other unreadable file: grep reports
// matches, not an inventory, so a silently absent file is the existing contract.
func (t *Grep) searchFile(root, rel, display string, re *regexp.Regexp, matches *[]grepMatch) {
	file, err := security.SafeOpen(root, rel)
	if err != nil {
		return
	}
	defer file.Close()

	if info, err := file.Stat(); err != nil || info.IsDir() || info.Size() > maxGrepFileBytes {
		return
	}

	reader := bufio.NewReader(file)
	if sniff, _ := reader.Peek(512); bytes.IndexByte(sniff, 0) >= 0 {
		return // binary file
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxGrepFileBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if re.MatchString(line) {
			*matches = append(*matches, grepMatch{openPath: rel, display: display, line: lineNumber, text: line})
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
// scope the search ran over (searchScope). It returns the total as a domain.MatchedLines on
// BOTH paths — a search that found nothing reports Total 0 rather than no summary, so a host
// reads a number instead of testing the "No matches found" sentence for a prefix.
//
// Pagination counts MATCHES only: contextLines rides along free, so the header's numbers and
// the truncation note mean the same thing whether context was asked for or not. With
// contextLines 0 the body is byte-identical to the pre-context output.
func (t *Grep) renderMatches(root, scope string, matches []grepMatch, maxResults, offset, contextLines int) (string, domain.MatchedLines) {
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
	shown := matches[start:end]

	capped := ""
	if total >= maxGrepMatches {
		capped = fmt.Sprintf(" (capped at %d)", maxGrepMatches)
	}
	header := fmt.Sprintf("[%d total matches%s in %s, showing %d-%d]", total, capped, scope, start+1, end)
	body := plainMatchLines(shown)
	if contextLines > 0 {
		body = t.renderContextMatches(root, shown, contextLines)
	}
	return header + "\n" + strings.Join(body, "\n"), domain.MatchedLines{Total: total}
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
func (t *Grep) renderContextMatches(root string, matches []grepMatch, contextLines int) []string {
	lines := make([]string, 0, len(matches)*(2*contextLines+1))
	for start := 0; start < len(matches); {
		end := start + 1
		for end < len(matches) && matches[end].openPath == matches[start].openPath {
			end++
		}
		lines = append(lines, t.renderFileGroup(root, matches[start:end], contextLines)...)
		start = end
	}
	return lines
}

// renderFileGroup renders one file's matches plus their merged context spans, separating
// non-adjacent spans with the conventional "--" line. A file whose context cannot be reread
// (deleted or newly refused since the match pass) degrades to bare match lines rather than
// failing the whole search.
func (t *Grep) renderFileGroup(root string, group []grepMatch, contextLines int) []string {
	spans := mergeContextSpans(group, contextLines)
	surrounding := t.readContextLines(root, group[0].openPath, spans)
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
// spans in ascending order. Windows that overlap OR merely touch are merged, so no line is
// ever printed twice and a "--" separator only ever falls on a real gap. Matches arrive in
// ascending line order (one forward scan per file).
func mergeContextSpans(group []grepMatch, contextLines int) []lineSpan {
	spans := make([]lineSpan, 0, len(group))
	for _, m := range group {
		from := m.line - contextLines
		if from < 1 {
			from = 1
		}
		span := lineSpan{from: from, to: m.line + contextLines}
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
// root, keyed by 1-based line number. The file is reopened THROUGH the same root's fence
// (security.SafeOpen), exactly as the match pass opened it, so context can never be gathered
// from a file the fence refuses; a refused or vanished file yields nil. Lines in the gaps
// BETWEEN spans are scanned past, never retained, so two far-apart matches in a large file
// do not pull the whole file into memory.
func (t *Grep) readContextLines(root, openPath string, spans []lineSpan) map[int]string {
	file, err := security.SafeOpen(root, openPath)
	if err != nil {
		return nil
	}
	defer file.Close()

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
