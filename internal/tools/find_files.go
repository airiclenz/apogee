package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var findFilesSpec = toolSpec{
	name: "find_files",
	// The description is the whole discovery surface: a model reading only the tool list must be
	// able to tell this tool from grep without trying one. So it says what is matched (the NAME,
	// never the path), what is not (a path pattern like "src/**/*.go"), and where content search
	// lives instead.
	description: "Find files by NAME. Returns workspace-relative paths whose file name matches a comma-separated list of globs, searching subdirectories recursively. Basename globs only — a path pattern such as \"src/**/*.go\" never matches; narrow the subtree with the path parameter instead. Use grep to search file CONTENT.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["pattern"],
  "properties": {
    "pattern": {"type": "string", "description": "Comma-separated file-name globs, e.g. \"*.go,Makefile\". Matched against each file's base NAME only, never against its directory path"},
    "path": {"type": "string", "description": "Directory to search within, relative to the workspace root (default: the whole workspace). The search always recurses into subdirectories"},
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

// FindFiles finds workspace files by NAME — the discovery half of the pair whose other half
// is grep (content). It is a read-only tool scoped to a sandbox root: the walk enumerates
// names only, it never descends through a symlink (fs.WalkDir over os.DirFS recurses into
// real directories alone), and an entry that is not a regular file is only reported when it
// still resolves to one THROUGH the workspace fence — so no name it reports lies outside the
// workspace.
type FindFiles struct {
	toolSpec
	root string
	// realRoot is root resolved through symlinks, for the same reason grep keeps one: the
	// walk's absolute paths come back symlink-resolved, so the workspace-relative name is only
	// measurable from the real root on a host whose workspace is reached through a link.
	realRoot string
}

// NewFindFiles returns a find_files tool that resolves paths within root.
func NewFindFiles(root string) *FindFiles {
	return &FindFiles{toolSpec: findFilesSpec, root: root, realRoot: security.EvalRealPath(root)}
}

// ReadOnly reports that find_files performs no writes (domain.ReadOnlyTool).
func (t *FindFiles) ReadOnly() bool { return true }

// Execute walks the workspace (or the subtree named by path) and returns the
// workspace-relative paths whose base name matches pattern, honouring ctx cancellation. A
// missing pattern, a missing path, or a path escaping the root is an IsError result.
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
	resolved, err := resolveInRoot(searchPath, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return errorResult(call.ID, "path not found: "+args.Path), nil
	}

	// The glob list is parsed by grep's own include parser, so the two tools agree on the
	// syntax — comma-separated basename globs — and on a malformed glob, which filepath.Match
	// reports as "no match" rather than an error the model has to decode.
	globs := parseIncludeGlobs(args.Pattern)
	found, err := t.walk(ctx, resolved, info, globs)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}
	return okResult(call.ID, renderFoundPaths(found, args.MaxResults, args.Offset)), nil
}

// walk collects the matching paths under target — the resolved absolute path of the search
// root — as workspace-relative names. target is used to ENUMERATE names only; nothing is
// opened by an absolute path the walk handed out. A target that is a file is matched by its
// own base name, so naming one file is a legal (if pointless) query rather than an error.
//
// The walk skips the same noise directories grep skips (grepExcludeDirs) and is always
// recursive: name discovery is this tool's whole point, so there is no depth or recursion
// parameter to get wrong.
func (t *FindFiles) walk(ctx context.Context, target string, info os.FileInfo, globs []string) ([]string, error) {
	found := make([]string, 0, defaultFindFilesResults)
	targetRel := filepath.ToSlash(t.relative(target))

	if !info.IsDir() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if matchesInclude(path.Base(targetRel), globs) {
			found = append(found, targetRel)
		}
		return found, nil
	}

	// prefix lifts a walk-relative name to a workspace-relative one ("" when the search root IS
	// the workspace root). fs.WalkDir yields slash-separated names, so the join is path.Join.
	prefix := targetRel
	if prefix == "." {
		prefix = ""
	}

	walkErr := fs.WalkDir(os.DirFS(target), ".", func(rel string, entry fs.DirEntry, err error) error {
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
		name := path.Join(prefix, rel)
		if !t.reportable(entry, name) {
			return nil
		}
		found = append(found, name)
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

// relative renders p relative to the sandbox root — the workspace-relative name a match is
// reported under — falling back to the absolute path if it cannot be made relative (which the
// fenced check then refuses, the safe direction).
func (t *FindFiles) relative(p string) string {
	if rel, err := filepath.Rel(t.realRoot, p); err == nil {
		return rel
	}
	return p
}

// reportable reports whether an entry the walk NAMED may be reported under rel, its
// workspace-relative name. A regular file always may: the walk never descends through a
// symlink, so every component above it is a real directory inside the searched subtree.
// Anything else — a symlink, a device, a socket — is only reported when it still opens to a
// regular file THROUGH the workspace fence (security.SafeOpen), so a planted `notes.txt ->
// ~/.ssh/id_rsa` is refused rather than named. A refused entry is silently absent, the same
// contract grep gives an unreadable file.
func (t *FindFiles) reportable(entry fs.DirEntry, rel string) bool {
	if entry.Type().IsRegular() {
		return true
	}
	file, err := security.SafeOpen(t.root, rel)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular()
}

// renderFoundPaths paginates from offset and prepends a header naming the total count, in the
// shape grep's header has (total, an optional collection cap, and the shown range). A page
// that leaves paths behind ends in a truncation note naming the offset that continues the
// listing, so a model that wants the rest does not have to work the arithmetic out.
func renderFoundPaths(found []string, maxResults, offset int) string {
	if len(found) == 0 {
		return "No files found"
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
	header := fmt.Sprintf("[%d files found%s, showing %d-%d]", total, capped, start+1, end)

	text := header + "\n" + strings.Join(found[start:end], "\n")
	if end < total {
		text += fmt.Sprintf("\n[...%d more, continue with offset %d]", total-end, end)
	}
	return text
}

var _ domain.ReadOnlyTool = (*FindFiles)(nil)
