package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// countOccurrences returns the number of non-overlapping occurrences of needle in
// haystack. It mirrors the oracle's count helper (string-utils.ts): an empty needle
// yields 0 so a find-replace with no search text reports "not found" rather than
// matching everywhere.
func countOccurrences(haystack, needle string) int {
	if needle == "" {
		return 0
	}
	return strings.Count(haystack, needle)
}

// ----------------------------------------------------------------------------
// single_find_and_replace
// ----------------------------------------------------------------------------

var singleFindReplaceSpec = toolSpec{
	name:        "single_find_and_replace",
	description: "Find and replace text in a file. The old text must appear exactly once in the file.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path", "oldText", "newText"],
  "properties": {
    "path": {"type": "string", "description": "The file path, relative to the workspace root or absolute"},
    "oldText": {"type": "string", "description": "The exact text to find (must appear exactly once)"},
    "newText": {"type": "string", "description": "The replacement text"}
  }
}`),
}

type singleFindReplaceArgs struct {
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// SingleFindReplace replaces one exact occurrence of oldText with newText in a file,
// requiring oldText to appear exactly once. It is a write tool scoped to a sandbox root
// and carries the workspaceScopedWriter marker (Apogee's own path-safety-bounded write,
// ADR 0012 D1). Ported from the oracle's find-replace-tool.
type SingleFindReplace struct {
	toolSpec
	root string
}

// NewSingleFindReplace returns a single_find_and_replace tool that resolves paths within root.
func NewSingleFindReplace(root string) *SingleFindReplace {
	return &SingleFindReplace{toolSpec: singleFindReplaceSpec, root: root}
}

// ReadOnly reports that single_find_and_replace is write-capable (domain.ReadOnlyTool):
// it returns false, the signal the loop gates it through Approval in Ask-Before.
func (t *SingleFindReplace) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would write, so dispatch can
// classify in- vs out-of-workspace before Execute (the workspaceScopedWriter marker,
// confinement-execution-contract §3). It performs no write — pure path resolution
// without the containment check. A call with no decodable path yields ok=false.
func (t *SingleFindReplace) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return pathArgWriteTarget(call, t.root)
}

// Execute finds oldText in the file and replaces it with newText, honouring ctx
// cancellation. A missing file, a path escape, oldText not found, oldText found more
// than once, or oversized newText are reported as IsError results, not Go errors.
//
// A success carries the Edit regions of what landed as its domain.ToolSummary (okEditRegions,
// regions.go) — the structured half a host paints the Split diff from (ADR 0052). The prose
// sentence the model reads is unchanged by that, and a failure carries no summary at all.
func (t *SingleFindReplace) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[singleFindReplaceArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	if args.OldText == "" {
		return errorResult(call.ID, "oldText is required"), nil
	}
	if len(args.NewText) > maxFileContentBytes {
		return errorResult(call.ID, fmt.Sprintf("replacement text exceeds maximum size (%d bytes)", maxFileContentBytes)), nil
	}

	// TOCTOU-safe read+write: both operations resolve through an os.Root pinned at
	// t.root, so an escaping-symlink component (including one swapped in between the read
	// and the write by a confined subprocess) is refused rather than followed (H1).
	content, err := readWriteTarget(ctx, args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, readFileErrorMessage(err, args.Path)), nil
	}

	// Which file those bytes came from, read BEFORE the write replaces a symlinked name with
	// a regular file: an in-root symlinked final NAME is the one link a read still follows
	// (a symlinked parent is refused by the write), and echoing the argument alone would let
	// this call disclose and overwrite somewhere else under the name the operator approved.
	resolved := resolvedTargetNote(args.Path, t.root)

	count := countOccurrences(string(content), args.OldText)
	if count == 0 {
		return errorResult(call.ID, closestRegion(string(content), args.OldText)), nil
	}
	if count > 1 {
		return errorResult(call.ID, fmt.Sprintf("old text found %d times (must appear exactly once)%s",
			count, occurrenceNote(string(content), args.OldText))), nil
	}

	updated := strings.Replace(string(content), args.OldText, args.NewText, 1)
	if err := safeWriteFile(ctx, args.Path, t.root, []byte(updated), 0o644); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	return okEditRegions(call.ID, "replaced text in "+args.Path+resolved+syntaxTrailer(args.Path, updated),
		string(content), updated), nil
}

// ----------------------------------------------------------------------------
// multi_find_and_replace
// ----------------------------------------------------------------------------

var multiFindReplaceSpec = toolSpec{
	name: "multi_find_and_replace",
	description: "Find and replace multiple text occurrences in a single file atomically. " +
		"Each old text must appear exactly once at the time it is applied. " +
		"Replacements are applied sequentially in array order. " +
		"If any replacement fails, the file is not modified. " +
		"Prefer this over multiple single_find_and_replace calls when making several edits to the same file.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path", "replacements"],
  "properties": {
    "path": {"type": "string", "description": "The file path, relative to the workspace root or absolute"},
    "replacements": {
      "type": "array",
      "description": "Ordered array of find-and-replace operations applied sequentially",
      "minItems": 1,
      "items": {
        "type": "object",
        "required": ["oldText", "newText"],
        "properties": {
          "oldText": {"type": "string", "description": "The exact text to find (must appear exactly once when applied)"},
          "newText": {"type": "string", "description": "The replacement text (empty string to delete)"}
        }
      }
    }
  }
}`),
}

type replacement struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

type multiFindReplaceArgs struct {
	Path         string        `json:"path"`
	Replacements []replacement `json:"replacements"`
}

// MultiFindReplace applies an ordered list of find-and-replace operations to a single
// file atomically: each oldText must appear exactly once at the moment it is applied,
// replacements are applied sequentially in array order, and the file is written only if
// every replacement succeeds. It is a write tool scoped to a sandbox root and carries
// the workspaceScopedWriter marker. Ported from the oracle's multi-find-replace-tool.
type MultiFindReplace struct {
	toolSpec
	root string
}

// NewMultiFindReplace returns a multi_find_and_replace tool that resolves paths within root.
func NewMultiFindReplace(root string) *MultiFindReplace {
	return &MultiFindReplace{toolSpec: multiFindReplaceSpec, root: root}
}

// ReadOnly reports that multi_find_and_replace is write-capable (domain.ReadOnlyTool).
func (t *MultiFindReplace) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would write so dispatch can
// classify in- vs out-of-workspace before Execute (the workspaceScopedWriter marker).
func (t *MultiFindReplace) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return pathArgWriteTarget(call, t.root)
}

// Execute applies the replacements sequentially against an in-memory copy of the file,
// honouring ctx cancellation, and writes the result only if every replacement matched
// exactly once. Any failure leaves the file untouched (atomic to the model's view).
//
// A success carries the Edit regions of the WHOLE applied edit as its domain.ToolSummary
// (okEditRegions, regions.go), cut from the file as it was against the file as written — not
// one summary per replacement, because what a host paints is the file's change, and two
// replacements landing three lines apart are one region of it (ADR 0052).
func (t *MultiFindReplace) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[multiFindReplaceArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	if len(args.Replacements) == 0 {
		return errorResult(call.ID, "replacements must be a non-empty array"), nil
	}
	for i, r := range args.Replacements {
		if r.OldText == "" {
			return errorResult(call.ID, fmt.Sprintf("replacements[%d].oldText is required and must be a non-empty string", i)), nil
		}
		if len(r.NewText) > maxFileContentBytes {
			return errorResult(call.ID, fmt.Sprintf("replacement #%d: newText exceeds maximum size (%d bytes)", i+1, maxFileContentBytes)), nil
		}
	}

	// TOCTOU-safe read+write through an os.Root pinned at t.root (H1).
	raw, err := readWriteTarget(ctx, args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, readFileErrorMessage(err, args.Path)), nil
	}

	// Read before the write, for the reason single_find_and_replace states above: the
	// disclosure has to survive the write that destroys the symlink it describes. The sibling
	// tool carries it, so this one must too — otherwise the same edit dodges the disclosure by
	// arriving as a one-element array.
	resolved := resolvedTargetNote(args.Path, t.root)

	content := string(raw)
	for i, r := range args.Replacements {
		count := countOccurrences(content, r.OldText)
		if count == 0 {
			return errorResult(call.ID, fmt.Sprintf("replacement #%d: %s", i+1, closestRegion(content, r.OldText))), nil
		}
		if count > 1 {
			return errorResult(call.ID, fmt.Sprintf("replacement #%d: old text found %d times (must appear exactly once)%s",
				i+1, count, occurrenceNote(content, r.OldText))), nil
		}

		content = strings.Replace(content, r.OldText, r.NewText, 1)

		if len(content) > maxFileContentBytes {
			return errorResult(call.ID, fmt.Sprintf("after replacement #%d, file would exceed maximum size (%d bytes)", i+1, maxFileContentBytes)), nil
		}
	}

	if err := safeWriteFile(ctx, args.Path, t.root, []byte(content), 0o644); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	n := len(args.Replacements)
	suffix := ""
	if n > 1 {
		suffix = "s"
	}
	message := fmt.Sprintf("applied %d replacement%s to %s%s", n, suffix, args.Path, resolved) +
		syntaxTrailer(args.Path, content)
	return okEditRegions(call.ID, message, string(raw), content), nil
}

var (
	_ domain.ReadOnlyTool   = (*SingleFindReplace)(nil)
	_ workspaceScopedWriter = (*SingleFindReplace)(nil)
	_ domain.ReadOnlyTool   = (*MultiFindReplace)(nil)
	_ workspaceScopedWriter = (*MultiFindReplace)(nil)
)
