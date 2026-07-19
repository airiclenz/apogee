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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
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
// sandbox root.
type Grep struct {
	toolSpec
	root string
}

// NewGrep returns a grep tool that resolves paths within root.
func NewGrep(root string) *Grep { return &Grep{toolSpec: grepSpec, root: root} }

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

	return okResult(call.ID, renderMatches(matches, args.MaxResults, args.Offset)), nil
}

// search collects matches from a single file or by walking a directory.
func (t *Grep) search(ctx context.Context, root string, info os.FileInfo, re *regexp.Regexp, globs []string) ([]string, error) {
	matches := make([]string, 0, defaultGrepResults)

	if !info.IsDir() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		searchFile(root, t.relative(root), re, &matches)
		return matches, nil
	}

	walkErr := fs.WalkDir(os.DirFS(root), ".", func(rel string, entry fs.DirEntry, err error) error {
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
		searchFile(filepath.Join(root, rel), rel, re, &matches)
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

// relative renders path relative to the sandbox root for display, falling back to the
// absolute path if it cannot be made relative.
func (t *Grep) relative(path string) string {
	if rel, err := filepath.Rel(t.root, path); err == nil {
		return rel
	}
	return path
}

// searchFile appends "rel:line:text" for every matching line in path, skipping a file
// that is oversized or binary (contains a NUL byte in its leading bytes).
func searchFile(path, rel string, re *regexp.Regexp, matches *[]string) {
	if info, err := os.Stat(path); err != nil || info.Size() > maxGrepFileBytes {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

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
			*matches = append(*matches, fmt.Sprintf("%s:%d:%s", rel, lineNumber, line))
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

// renderMatches paginates from offset and prepends a header naming the total count.
func renderMatches(matches []string, maxResults, offset int) string {
	if len(matches) == 0 {
		return "No matches found"
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
	return header + "\n" + strings.Join(shown, "\n")
}

var _ domain.ReadOnlyTool = (*Grep)(nil)
