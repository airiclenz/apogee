package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
// reported as IsError results, not Go errors.
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

	path, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return errorResult(call.ID, "file not found: "+args.Path), nil
	}
	if info.IsDir() {
		return errorResult(call.ID, "not a file: "+args.Path), nil
	}
	if info.Size() > maxFileReadBytes {
		return errorResult(call.ID, fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxFileReadBytes)), nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	return okResult(call.ID, renderOpenFile(args.Path, string(content), args.Locate)), nil
}

// renderOpenFile builds the open_file output: a header naming the file, an optional
// "found on lines …" locate report, then the file content.
func renderOpenFile(displayPath, content, locate string) string {
	header := "File: " + displayPath
	if locate == "" {
		return header + "\n\n" + content
	}

	var matches []string
	for i, line := range strings.Split(content, "\n") {
		if strings.Contains(line, locate) {
			matches = append(matches, fmt.Sprintf("%d", i+1))
		}
	}

	located := fmt.Sprintf("Located %q on lines: %s", locate, strings.Join(matches, ", "))
	if len(matches) == 0 {
		located = fmt.Sprintf("Located %q on no lines", locate)
	}
	return header + "\n" + located + "\n\n" + content
}

var _ domain.ReadOnlyTool = (*OpenFile)(nil)
