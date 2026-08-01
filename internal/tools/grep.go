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
	description: "Search workspace files for lines matching a regular expression. Returns file:line:text matches.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["pattern"],
  "properties": {
    "pattern": {"type": "string", "description": "Regular expression to search for (a literal substring if it is not a valid regex)"},
    "path": {"type": "string", "description": "File or directory to search within, relative to the workspace root (default: the whole workspace)"},
    "include": {"type": "string", "description": "Comma-separated file-name globs to include, e.g. \"*.go,*.md\" (default: all files)"},
    "max_results": {"type": "integer", "description": "Maximum matches to return (default 50)"},
    "offset": {"type": "integer", "description": "Number of matches to skip for pagination (default 0)"}
  }
}`),
}

type grepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Include    string `json:"include"`
	MaxResults int    `json:"max_results"`
	Offset     int    `json:"offset"`
}

// maxGrepMatches bounds the matches grep collects across the whole tree, so a broad
// pattern on a large workspace cannot exhaust memory; the result notes truncation.
const maxGrepMatches = 1000

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
// sandbox root: the walk enumerates names, but every file it reads is opened THROUGH
// the workspace fence, so a symlink pointing out of the root yields nothing.
type Grep struct {
	toolSpec
	root string
	// realRoot is root resolved through symlinks. resolveInRoot hands back real paths, so
	// the workspace-relative name a walked entry is opened by must be measured from the
	// real root — on a host where the workspace itself is reached through a link (macOS's
	// /tmp, a symlinked /home) the raw root is a prefix of nothing.
	realRoot string
}

// NewGrep returns a grep tool that resolves paths within root.
func NewGrep(root string) *Grep {
	return &Grep{toolSpec: grepSpec, root: root, realRoot: security.EvalRealPath(root)}
}

// ReadOnly reports that grep performs no writes (domain.ReadOnlyTool).
func (t *Grep) ReadOnly() bool { return true }

// Execute searches the file or directory named in call.Arguments, honouring ctx
// cancellation. A pattern that is not a valid regex is treated as a literal substring;
// a missing path or a path escaping the root is an IsError result.
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
	resolved, err := resolveInRoot(searchPath, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return errorResult(call.ID, "path not found: "+args.Path), nil
	}

	globs := parseIncludeGlobs(args.Include)
	matches, err := t.search(ctx, resolved, info, re, globs)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}

	text, matched := renderMatches(matches, args.MaxResults, args.Offset)
	return okSummary(call.ID, text, matched), nil
}

// search collects matches from a single file or by walking a directory. target is the
// resolved absolute path of the search root; it is used to ENUMERATE names only — every
// file is opened by its workspace-relative name through the fence, never by an absolute
// path the walk handed out.
func (t *Grep) search(ctx context.Context, target string, info os.FileInfo, re *regexp.Regexp, globs []string) ([]string, error) {
	matches := make([]string, 0, defaultGrepResults)
	targetRel := t.relative(target)

	if !info.IsDir() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t.searchFile(targetRel, targetRel, re, &matches)
		return matches, nil
	}

	// prefix lifts a walk-relative name to a workspace-relative one ("" when the search
	// root IS the workspace root). fs.WalkDir yields slash-separated names, so the join
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
		t.searchFile(path.Join(prefix, rel), rel, re, &matches)
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

// relative renders p relative to the sandbox root — the name a file is both displayed by
// and opened by — falling back to the absolute path if it cannot be made relative (which
// the fenced open then refuses, the safe direction).
func (t *Grep) relative(p string) string {
	if rel, err := filepath.Rel(t.realRoot, p); err == nil {
		return rel
	}
	return p
}

// searchFile appends "display:line:text" for every matching line in the workspace file
// named by rel, skipping a file that is oversized or binary (contains a NUL byte in its
// leading bytes).
//
// rel is opened THROUGH the workspace fence (os.Root-pinned, security.SafeOpen), so a
// walked entry that is a symlink out of the workspace — a clone can plant `notes.txt ->
// ~/.ssh/id_rsa`, and grep is read-only, hence unapproved in every mode — is refused
// rather than followed, and the size bound is an fstat of the very descriptor the content
// is then read from. A refusal is skipped like any other unreadable file: grep reports
// matches, not an inventory, so a silently absent file is the existing contract.
func (t *Grep) searchFile(rel, display string, re *regexp.Regexp, matches *[]string) {
	file, err := security.SafeOpen(t.root, rel)
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
			*matches = append(*matches, fmt.Sprintf("%s:%d:%s", display, lineNumber, line))
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

// renderMatches paginates from offset and prepends a header naming the total count. It
// returns the total as a domain.MatchedLines on BOTH paths — a search that found nothing
// reports Total 0 rather than no summary, so a host reads a number instead of testing the
// "No matches found" sentence for a prefix.
func renderMatches(matches []string, maxResults, offset int) (string, domain.MatchedLines) {
	if len(matches) == 0 {
		return "No matches found", domain.MatchedLines{Total: 0}
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
	header := fmt.Sprintf("[%d total matches%s, showing %d-%d]", total, capped, start+1, end)
	return header + "\n" + strings.Join(shown, "\n"), domain.MatchedLines{Total: total}
}

var _ domain.ReadOnlyTool = (*Grep)(nil)
