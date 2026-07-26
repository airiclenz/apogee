package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var openFileSpec = toolSpec{
	name:        "open_file",
	description: "Open a file and read its content, optionally locating the line numbers of a substring.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "The file path to open, relative to the workspace root or absolute"},
    "locate": {"type": "string", "description": "Optional substring to locate; the result reports the 1-based line numbers where it occurs"}
  }
}`),
}

type openFileArgs struct {
	Path   string `json:"path"`
	Locate string `json:"locate"`
}

// OpenFile reads a file and, optionally, locates a substring within it — the
// editor-affordance read tool (the oracle's "currently open file", adapted for a TUI that
// has no active editor: the file is named explicitly). It returns the file's content with
// a header and, when locate is given, the 1-based line numbers where it appears. It is
// read-only and carries no writer marker.
type OpenFile struct {
	toolSpec
	root string
}

// NewOpenFile returns an open_file tool that resolves paths within root.
func NewOpenFile(root string) *OpenFile { return &OpenFile{toolSpec: openFileSpec, root: root} }

// ReadOnly reports that open_file performs no writes (domain.ReadOnlyTool) — it runs in
// Plan and never gates.
func (t *OpenFile) ReadOnly() bool { return true }

// Execute reads the file named in call.Arguments and returns its content, honouring ctx
// cancellation. When locate is set, the 1-based line numbers where the substring occurs
// are prepended. A missing file, a directory, an oversized file, or a path escape are
// reported as IsError results, not Go errors. A successful read carries the body's line
// count and the located lines as a domain.OpenedFile summary.
//
// The workspace fence is enforced at OPEN time through an os.Root pinned at t.root, so a
// path component swapped to point outside the root — including a concurrent swap by a
// confined subprocess mid-call — is refused rather than followed (security review H1).
// The open, the size check and the read share ONE descriptor (readWorkspaceFileBounded),
// so there is no check/use gap between them: a rename mid-call changes nothing the call
// sees, and a file grown past the cap mid-read is refused (see the SCOPE note in
// internal/security/safeio.go).
//
// As on read_file, the pinned root resolves RELATIVE components only: an in-root symlink
// whose target is spelled as an absolute path is refused even when that target is inside
// the root, where the former trio read it. The fence is tighten-only, so the narrowing is
// kept and recorded in the CHANGELOG (Unreleased → Security); relative in-root symlinks
// read exactly as they did before.
func (t *OpenFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[openFileArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	content, failMessage := readWorkspaceFileBounded(args.Path, t.root)
	if failMessage != "" {
		return errorResult(call.ID, failMessage), nil
	}

	text, opened := renderOpenFile(args.Path, string(content), args.Locate)
	return okSummary(call.ID, text, opened), nil
}

// renderOpenFile builds the open_file output: a header naming the file, an optional
// "found on lines …" locate report, then the file content.
//
// It returns the same facts as a domain.OpenedFile: the body's own line count and, when a
// locate term was requested, the line numbers the sentence lists — the sentence is BUILT
// from those numbers, so a host reads them as data instead of parsing the sentence back.
// An empty Locate means none was requested; a set Locate with an empty LocatedOn means one
// was requested and matched nothing, which the prose cannot distinguish without a prefix
// test.
func renderOpenFile(displayPath, content, locate string) (string, domain.OpenedFile) {
	body := strings.Split(content, "\n")
	opened := domain.OpenedFile{Lines: len(body), Locate: locate}

	header := "File: " + displayPath
	if locate == "" {
		return header + "\n\n" + content, opened
	}

	for i, line := range body {
		if strings.Contains(line, locate) {
			opened.LocatedOn = append(opened.LocatedOn, i+1)
		}
	}

	located := fmt.Sprintf("Located %q on no lines", locate)
	if len(opened.LocatedOn) > 0 {
		numbers := make([]string, len(opened.LocatedOn))
		for i, n := range opened.LocatedOn {
			numbers[i] = strconv.Itoa(n)
		}
		located = fmt.Sprintf("Located %q on lines: %s", locate, strings.Join(numbers, ", "))
	}
	return header + "\n" + located + "\n\n" + content, opened
}

var _ domain.ReadOnlyTool = (*OpenFile)(nil)
