package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/domain"
)

var writeFileSchema = json.RawMessage(`{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": {"type": "string", "description": "File path to write, relative to the workspace root or absolute"},
    "content": {"type": "string", "description": "The full content to write to the file"}
  }
}`)

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteFile creates or overwrites a file with the given content, creating parent
// directories as needed. It is a write tool — the loop routes it through Approval in
// Ask-Before before Execute is called (P1.2).
type WriteFile struct{ root string }

// NewWriteFile returns a write_file tool that resolves paths within root.
func NewWriteFile(root string) *WriteFile { return &WriteFile{root: root} }

// Name returns the stable identifier the model calls.
func (t *WriteFile) Name() string { return "write_file" }

// Description returns the model-facing summary of the tool.
func (t *WriteFile) Description() string {
	return "Create or overwrite a file with the given content. Parent directories are created as needed."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *WriteFile) Schema() json.RawMessage { return writeFileSchema }

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
// third-party tool can fake (contract §3.2).
func (t *WriteFile) workspaceWriteTarget(call domain.ToolCall) (string, bool) {
	var args writeFileArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return "", false
	}
	return resolveTargetUnbounded(args.Path, t.root)
}

// Execute writes content to the file named in call.Arguments, honouring ctx
// cancellation. Bad arguments, oversized content, or a path that escapes the root are
// reported as IsError results; the write itself is atomic to the model's view (it
// either fully succeeds or the result is an error).
func (t *WriteFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args writeFileArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	if len(args.Content) > maxFileContentBytes {
		return errorResult(call.ID, fmt.Sprintf("content too large: %d bytes (max %d)", len(args.Content), maxFileContentBytes)), nil
	}

	path, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errorResult(call.ID, "could not create parent directory: "+err.Error()), nil
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	return okResult(call.ID, fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path)), nil
}

var (
	_ domain.Tool           = (*WriteFile)(nil)
	_ workspaceScopedWriter = (*WriteFile)(nil)
)
