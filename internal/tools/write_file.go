package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

var writeFileSpec = toolSpec{
	name:        "write_file",
	description: "Create or overwrite a file with the given content. Parent directories are created as needed.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": {"type": "string", "description": "File path to write, relative to the workspace root or absolute"},
    "content": {"type": "string", "description": "The full content to write to the file"}
  }
}`),
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteFile creates or overwrites a file with the given content, creating parent
// directories as needed. It is a write tool — the loop routes it through Approval in
// Ask-Before before Execute is called (P1.2).
type WriteFile struct {
	toolSpec
	root string
}

// NewWriteFile returns a write_file tool that resolves paths within root.
func NewWriteFile(root string) *WriteFile { return &WriteFile{toolSpec: writeFileSpec, root: root} }

// ReadOnly reports that write_file is write-capable — it returns false, the signal
// that the loop must gate it through Approval in Ask-Before (domain.ReadOnlyTool).
func (t *WriteFile) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would write so dispatch can
// classify in- vs out-of-workspace before Execute (the workspaceScopedWriter marker,
// confinement-execution-contract §3). It performs no write — pure path resolution using
// the same root the Execute path resolves against, WITHOUT the containment check, so an
// out-of-workspace target resolves rather than erroring (that is the classification
// dispatch needs). A call with no decodable path yields ok=false (treated as in-bounds).
// This method being unexported is what makes write_file an Apogee-own writer no
// third-party tool can fake (contract §3.2) — the method stays per-type even though
// every writer shares one body (pathArgWriteTarget), because the marker IS the method set.
func (t *WriteFile) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return pathArgWriteTarget(call, t.root)
}

// Execute writes content to the file named in call.Arguments, honouring ctx
// cancellation. Bad arguments, oversized content, or a path that escapes the root are
// reported as IsError results; the write itself is atomic to the model's view (it
// either fully succeeds or the result is an error).
func (t *WriteFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[writeFileArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	if len(args.Content) > maxFileContentBytes {
		return errorResult(call.ID, fmt.Sprintf("content too large: %d bytes (max %d)", len(args.Content), maxFileContentBytes)), nil
	}

	// Where this write REALLY lands, read before the write rather than after it, so the
	// sentence below says the same thing the approval pane said about the same call — after
	// the write the final name is a plain file whatever it was, and the two surfaces would
	// part company on exactly the call worth disclosing (resolvedTargetNote).
	resolved := resolvedTargetNote(args.Path, t.root)

	// TOCTOU-safe write: the workspace fence is enforced AT WRITE TIME through an
	// os.Root pinned at t.root, so a path component swapped to an outside-pointing
	// symlink — including a concurrent swap by a confined subprocess — is refused
	// rather than followed (security review H1). Parent directories are created within
	// the same fence.
	if err := safeWriteFile(args.Path, t.root, []byte(args.Content), 0o644); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	return okSummary(call.ID,
		fmt.Sprintf("wrote %d bytes to %s%s", len(args.Content), args.Path, resolved),
		domain.WroteBytes{Bytes: len(args.Content)}), nil
}

var (
	_ domain.Tool           = (*WriteFile)(nil)
	_ workspaceScopedWriter = (*WriteFile)(nil)
)
