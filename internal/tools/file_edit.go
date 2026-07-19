package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var fileEditSpec = toolSpec{
	name:        "edit_existing_file",
	description: "Edit an existing file. Accepts either full replacement content or a patch in \"*** Begin Patch\" format.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": {"type": "string", "description": "The file path to edit, relative to the workspace root or absolute"},
    "content": {"type": "string", "description": "The new content for the file, or a patch in \"*** Begin Patch\" format"}
  }
}`),
}

type fileEditArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// EditExistingFile edits an existing file, accepting either full replacement content or
// a patch in the "*** Begin Patch" format (a sequence of @@ hunks of -/+/space lines).
// It is a write tool scoped to a sandbox root and carries the workspaceScopedWriter
// marker (Apogee's own path-safety-bounded write). Ported from the oracle's
// file-edit-tool, including its hunk parser and indexOf-based applier.
type EditExistingFile struct {
	toolSpec
	root string
}

// NewEditExistingFile returns an edit_existing_file tool that resolves paths within root.
func NewEditExistingFile(root string) *EditExistingFile {
	return &EditExistingFile{toolSpec: fileEditSpec, root: root}
}

// ReadOnly reports that edit_existing_file is write-capable (domain.ReadOnlyTool).
func (t *EditExistingFile) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would write so dispatch can
// classify in- vs out-of-workspace before Execute (the workspaceScopedWriter marker).
func (t *EditExistingFile) workspaceWriteTarget(call domain.ToolCall) (string, bool) {
	var args fileEditArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return "", false
	}
	return resolveTargetUnbounded(args.Path, t.root)
}

// Execute edits the file named in call.Arguments, honouring ctx cancellation. If content
// is a "*** Begin Patch" block it is parsed into hunks and applied against the existing
// file (a non-matching hunk leaves the file untouched); otherwise content fully replaces
// the file. A missing file, a path escape, oversized content, or a non-applying patch are
// reported as IsError results, not Go errors.
func (t *EditExistingFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[fileEditArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	if len(args.Content) > maxFileContentBytes {
		return errorResult(call.ID, fmt.Sprintf("content exceeds maximum size (%d bytes)", maxFileContentBytes)), nil
	}

	// TOCTOU-safe read+write through an os.Root pinned at t.root: an escaping-symlink
	// component (including one swapped in between the read and the write) is refused
	// rather than followed (security review H1).
	original, err := safeReadFile(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, readFileErrorMessage(err, args.Path)), nil
	}

	if isPatchContent(args.Content) {
		hunks := parsePatchHunks(args.Content)
		if len(hunks) == 0 {
			return errorResult(call.ID, "patch contained no hunks"), nil
		}
		patched, ok := applyPatch(string(original), hunks)
		if !ok {
			return errorResult(call.ID, "patch hunk did not match file content"), nil
		}
		if err := safeWriteFile(args.Path, t.root, []byte(patched), 0o644); err != nil {
			return errorResult(call.ID, err.Error()), nil
		}
		suffix := ""
		if len(hunks) > 1 {
			suffix = "s"
		}
		return okResult(call.ID, fmt.Sprintf("applied patch to %s (%d hunk%s)", args.Path, len(hunks), suffix)), nil
	}

	if err := safeWriteFile(args.Path, t.root, []byte(args.Content), 0o644); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	return okResult(call.ID, "updated "+args.Path), nil
}

// ----------------------------------------------------------------------------
// Patch parsing and application (ported from the oracle's file-edit-tool)
// ----------------------------------------------------------------------------

// patchHunk is one @@ block of a "*** Begin Patch" edit: the original lines it removes
// (oldLines) and the lines it inserts (newLines). A context (space-prefixed) line
// appears in both.
type patchHunk struct {
	oldLines []string
	newLines []string
}

var (
	patchStart  = regexp.MustCompile(`(?i)^\*{3}\s*Begin\s+Patch`)
	patchEnd    = regexp.MustCompile(`(?i)^\*{3}\s*End\s+Patch`)
	patchFile   = regexp.MustCompile(`(?i)^\*{3}\s*(?:Update|Add|Delete)\s+File:\s*`)
	patchHeader = regexp.MustCompile(`^@@`)
)

// isPatchContent reports whether content opens with a "*** Begin Patch" marker (after
// leading whitespace), distinguishing a patch from full-file replacement content.
func isPatchContent(content string) bool {
	return patchStart.MatchString(strings.TrimLeft(content, " \t\r\n"))
}

// parsePatchHunks splits a patch into hunks. Begin/End/File markers are skipped; each @@
// header opens a new hunk; a '-' line removes, a '+' line inserts, and a ' ' (space) line
// is context kept in both. Lines outside a hunk are ignored, mirroring the oracle.
func parsePatchHunks(content string) []patchHunk {
	lines := strings.Split(content, "\n")
	var hunks []patchHunk
	inHunk := false
	var current patchHunk
	have := false

	flush := func() {
		if have && (len(current.oldLines) > 0 || len(current.newLines) > 0) {
			hunks = append(hunks, current)
		}
	}

	for _, line := range lines {
		if patchStart.MatchString(line) || patchEnd.MatchString(line) || patchFile.MatchString(line) {
			continue
		}

		if patchHeader.MatchString(line) {
			flush()
			current = patchHunk{}
			have = true
			inHunk = true
			continue
		}

		if !inHunk || !have {
			continue
		}

		switch {
		case strings.HasPrefix(line, "-"):
			current.oldLines = append(current.oldLines, line[1:])
		case strings.HasPrefix(line, "+"):
			current.newLines = append(current.newLines, line[1:])
		case strings.HasPrefix(line, " "):
			current.oldLines = append(current.oldLines, line[1:])
			current.newLines = append(current.newLines, line[1:])
		}
	}

	flush()
	return hunks
}

// applyPatch applies each hunk to original by locating its joined oldLines verbatim and
// substituting its joined newLines. A pure-insertion hunk (no oldLines) appends. It
// returns ok=false if any hunk's old text is not found, leaving the caller to discard the
// result so the file is never corrupted. Ported from the oracle's indexOf-based applier.
func applyPatch(original string, hunks []patchHunk) (string, bool) {
	result := original

	for _, hunk := range hunks {
		if len(hunk.oldLines) == 0 {
			result += strings.Join(hunk.newLines, "\n")
			continue
		}

		needle := strings.Join(hunk.oldLines, "\n")
		idx := strings.Index(result, needle)
		if idx == -1 {
			return "", false
		}

		before := result[:idx]
		after := result[idx+len(needle):]
		result = before + strings.Join(hunk.newLines, "\n") + after
	}

	return result, true
}

var (
	_ domain.ReadOnlyTool   = (*EditExistingFile)(nil)
	_ workspaceScopedWriter = (*EditExistingFile)(nil)
)
