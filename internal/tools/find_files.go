package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var findFilesSpec = toolSpec{
	name: "find_files",
	// The description is the whole discovery surface: a model reading only the tool list must be
	// able to tell this tool from grep without trying one. So it says what is matched (the NAME,
	// never the path), what is not (a path pattern like "src/**/*.go"), and where content search
	// lives instead.
	description: "Find files by NAME. Returns workspace-relative paths whose file name matches a comma-separated list of globs, searching subdirectories recursively. Basename globs only — a path pattern such as \"src/**/*.go\" never matches; narrow the subtree with the path parameter instead. An absolute path under a configured read-only root (such as the skills library) can be searched too. Use grep to search file CONTENT.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["pattern"],
  "properties": {
    "pattern": {"type": "string", "description": "Comma-separated file-name globs, e.g. \"*.go,Makefile\". Matched against each file's base NAME only, never against its directory path"},
    "path": {"type": "string", "description": "Directory to search within, relative to the workspace root or absolute (default: the whole workspace). The search always recurses into subdirectories"},
    "max_results": {"type": "integer", "description": "Maximum paths to return (default 50)"},
    "offset": {"type": "integer", "description": "Number of paths to skip for pagination (default 0)"}
  }
}`),
}

type findFilesArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
	Offset     int    `json:"offset"`
}

// maxFindFilesPaths bounds the paths find_files collects across the whole tree, mirroring
// grep's collection bound: a wide glob on a large workspace cannot exhaust memory, and the
// result notes the cap.
const maxFindFilesPaths = 1000

// defaultFindFilesResults is the page size when the call names none.
const defaultFindFilesResults = 50

// errFindFilesStop unwinds the WalkDir once the path cap is reached.
var errFindFilesStop = errors.New("find_files: path cap reached")

// FindFiles finds files by NAME — the discovery half of the pair whose other half is grep
// (content). It is a read-only tool scoped to a sandbox root plus any extra read-only roots
// the host mounted (readScope): the walk enumerates names only, it never descends through a
// symlink (fs.WalkDir over os.DirFS recurses into real directories alone), and an entry that
// is not a regular file is only reported when it still resolves to one THROUGH the fence of
// the root the search path was accepted under — so no name it reports lies outside that root.
type FindFiles struct {
	toolSpec
	scope readScope
}

// NewFindFiles returns a find_files tool that resolves paths within root, and — for ABSOLUTE
// paths only — within any extra read-only root mounts reports at call time, plus any virtual
// mount it names. A zero ReadMounts means workspace-only: byte-identical to the fence before
// either mount seam existed.
func NewFindFiles(root string, mounts ReadMounts) *FindFiles {
	return &FindFiles{toolSpec: findFilesSpec, scope: mounts.scope(root)}
}

// ReadOnly reports that find_files performs no writes (domain.ReadOnlyTool).
func (t *FindFiles) ReadOnly() bool { return true }

// Execute walks the workspace (or the subtree named by path) and returns the root-relative
// paths whose base name matches pattern, honouring ctx cancellation. A missing pattern, a
// missing path, or a path escaping every root is an IsError result.
//
// The search path is resolved over the workspace root first, then — for an ABSOLUTE path only —
// over the configured extra read-only roots, and the WHOLE walk is pinned to the root that
// accepted it: names are measured from that root and the reportability check opens through its
// fence, never the workspace's.
func (t *FindFiles) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[findFilesArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return errorResult(call.ID, "pattern is required"), nil
	}

	searchPath := args.Path
	if searchPath == "" {
		searchPath = "."
	}
	target, refusal := t.scope.searchTarget(searchPath, args.Path)
	if refusal != "" {
		return errorResult(call.ID, refusal), nil
	}

	// The glob list is parsed by grep's own include parser, so the two tools agree on the
	// syntax — comma-separated basename globs — and on a malformed glob, which filepath.Match
	// reports as "no match" rather than an error the model has to decode.
	globs := parseIncludeGlobs(args.Pattern)
	found, err := t.walk(ctx, target, globs)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}
	return okResult(call.ID, renderFoundPaths(found, searchScope(args.Path, globs), args.MaxResults, args.Offset)), nil
}

// walk collects the matching paths under target as the names that target REPORTS them under
// (searchTarget, path_read.go): root-relative on disk, the mount's announced address in a virtual
// mount. The tree is used to ENUMERATE names only; nothing is opened by an absolute path the walk
// handed out. A target that is a file is matched by its own base name, so naming one file is a
// legal (if pointless) query rather than an error.
//
// The walk skips the same noise directories grep skips (grepExcludeDirs) and is always
// recursive: name discovery is this tool's whole point, so there is no depth or recursion
// parameter to get wrong.
func (t *FindFiles) walk(ctx context.Context, target searchTarget, globs []string) ([]string, error) {
	found := make([]string, 0, defaultFindFilesResults)

	if !target.isDir() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if matchesInclude(path.Base(target.rel), globs) {
			found = append(found, target.rel)
		}
		return found, nil
	}

	walkErr := fs.WalkDir(target.tree, ".", func(rel string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the whole walk
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
		if !t.reportable(target, entry, target.openName(rel)) {
			return nil
		}
		found = append(found, target.reportName(rel))
		if len(found) >= maxFindFilesPaths {
			return errFindFilesStop
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errFindFilesStop) {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return nil, walkErr
		}
	}
	return found, nil
}

// reportable reports whether an entry the walk NAMED may be reported under rel, its name
// relative to root. A regular file always may: the walk never descends through a symlink, so
// every component above it is a real directory inside the searched subtree. Anything else — a
// symlink, a device, a socket — is only reported when it still opens to a regular file THROUGH
// the target's own fence (target.open), so a planted `notes.txt -> ~/.ssh/id_rsa` is refused rather
// than named — and a walk that began under an extra read-only root is checked against THAT
// root, so a link escaping it is refused just as one escaping the workspace is. A refused entry
// is silently absent, the same contract grep gives an unreadable file.
func (t *FindFiles) reportable(target searchTarget, entry fs.DirEntry, rel string) bool {
	if entry.Type().IsRegular() {
		return true
	}
	file, err := target.open(rel)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular()
}

// renderFoundPaths paginates from offset and prepends a header naming the total count and the
// scope the walk covered (searchScope), in the shape grep's header has (total, an optional
// collection cap, the scope, and the shown range). A page that leaves paths behind ends in a
// truncation note naming the offset that continues the listing, so a model that wants the rest
// does not have to work the arithmetic out.
func renderFoundPaths(found []string, scope string, maxResults, offset int) string {
	if len(found) == 0 {
		return "No files found in " + scope
	}
	if maxResults <= 0 {
		maxResults = defaultFindFilesResults
	}
	if offset < 0 {
		offset = 0
	}

	total := len(found)
	start := offset
	if start > total {
		start = total
	}
	end := start + maxResults
	if end > total {
		end = total
	}

	capped := ""
	if total >= maxFindFilesPaths {
		capped = fmt.Sprintf(" (capped at %d)", maxFindFilesPaths)
	}
	header := fmt.Sprintf("[%d files found%s in %s, showing %d-%d]", total, capped, scope, start+1, end)

	// A path is data inside a one-row-per-line grammar: a filename carrying a line break
	// would otherwise forge rows the model reads as this tool's own header or notes.
	rows := make([]string, 0, end-start)
	for _, name := range found[start:end] {
		rows = append(rows, escapeRowBreaks(name))
	}

	text := header + "\n" + strings.Join(rows, "\n")
	if end < total {
		text += fmt.Sprintf("\n[...%d more, continue with offset %d]", total-end, end)
	}
	return text
}

var _ domain.ReadOnlyTool = (*FindFiles)(nil)
