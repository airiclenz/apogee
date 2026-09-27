package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var fileEditSpec = toolSpec{
	name:        "edit_existing_file",
	description: "Edit an existing file. Accepts either full replacement content or a patch in \"*** Begin Patch\" format. Each @@ hunk needs at least one context (' ') or removal ('-') line to anchor it: a hunk of only '+' lines is refused unless the file is empty.",
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

// ArgRoles declares `content` — the body written — as payload (domain.ArgRolePayload); the
// target `path` stays fully inspected.
func (t *EditExistingFile) ArgRoles() map[string]domain.ArgRole {
	return map[string]domain.ArgRole{"content": domain.ArgRolePayload}
}

// workspaceWriteTarget resolves the absolute path this call would write so dispatch can
// classify in- vs out-of-workspace before Execute (the workspaceScopedWriter marker).
func (t *EditExistingFile) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return pathArgWriteTarget(call, t.root)
}

// Execute edits the file named in call.Arguments, honouring ctx cancellation. If content
// is a "*** Begin Patch" block it is parsed into hunks and applied against the existing
// file (a non-matching hunk leaves the file untouched); otherwise content fully replaces
// the file. A missing file, a path escape, oversized content, or a non-applying patch are
// reported as IsError results, not Go errors.
//
// Both forms carry the Edit regions of what landed as their domain.ToolSummary (okEditRegions,
// regions.go) — the structured half a host paints the Split diff from (ADR 0052). They are cut
// from the file as it was READ against the file as WRITTEN, so a patch whose hunks the applier
// placed somewhere other than where the model pictured them reports where they actually landed,
// and a full replacement reports only the lines that differ rather than the whole file. The
// prose sentence the model reads is unchanged by that, and a failure carries no summary at all.
func (t *EditExistingFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[fileEditArgs](call)
	if !ok {
		return fail, nil
	}

	// The one resolution of this call's path (writeScope.target): the read, the disclosure and
	// the write below all go through this value, so they describe the same file as the one
	// dispatch classified. An empty path is its refusal.
	target, err := writeScopeOf(ctx, t.root).target(args.Path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	if len(args.Content) > maxFileContentBytes {
		return errorResult(call.ID, fmt.Sprintf("content exceeds maximum size (%d bytes)", maxFileContentBytes)), nil
	}

	// TOCTOU-safe read+write through an os.Root pinned at t.root: an escaping-symlink
	// component (including one swapped in between the read and the write) is refused
	// rather than followed (security review H1). An in-root symlink is FOLLOWED by the read
	// and REFUSED on the write's parent chain (security's symlink policy), so the one path
	// this edit can still take through a link is a symlinked final NAME — which the read
	// followed and the write is about to replace.
	original, err := target.read()
	if err != nil {
		return errorResult(call.ID, target.notFound(err, "file not found: ")), nil
	}

	// Which file those bytes actually came from, read BEFORE the write: the write replaces a
	// symlinked name with a regular file, so afterwards nothing is left to say that "edit
	// docs/notes.md" read — and disclosed — the contents of somewhere else.
	resolved := target.note()

	if isPatchContent(args.Content) {
		hunks := parsePatchHunks(args.Content)
		if len(hunks) == 0 {
			return errorResult(call.ID, "patch contained no hunks"), nil
		}
		patched, err := applyPatch(string(original), hunks)
		if err != nil {
			return errorResult(call.ID, err.Error()), nil
		}
		if err := target.write([]byte(patched), 0o644); err != nil {
			return errorResult(call.ID, err.Error()), nil
		}
		suffix := ""
		if len(hunks) > 1 {
			suffix = "s"
		}
		content := fmt.Sprintf("applied patch to %s (%d hunk%s)%s", args.Path, len(hunks), suffix, resolved) +
			syntaxTrailer(args.Path, patched)
		return okEditRegions(call.ID, content, string(original), patched), nil
	}

	if err := target.write([]byte(args.Content), 0o644); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	return okEditRegions(call.ID, "updated "+args.Path+resolved+syntaxTrailer(args.Path, args.Content),
		string(original), args.Content), nil
}

// ----------------------------------------------------------------------------
// Patch parsing and application (ported from the oracle's file-edit-tool)
// ----------------------------------------------------------------------------

// patchHunk is one @@ block of a "*** Begin Patch" edit: the original lines it removes
// (oldLines) and the lines it inserts (newLines). A context (space-prefixed) line
// appears in both. addFile records that the hunk sits under an "*** Add File" section,
// the one place a hunk with no oldLines has a position (the file's end).
type patchHunk struct {
	oldLines []string
	newLines []string
	addFile  bool
}

var (
	patchStart  = regexp.MustCompile(`(?i)^\*{3}\s*Begin\s+Patch`)
	patchEnd    = regexp.MustCompile(`(?i)^\*{3}\s*End\s+Patch`)
	patchFile   = regexp.MustCompile(`(?i)^\*{3}\s*(?:Update|Add|Delete)\s+File:\s*`)
	patchAdd    = regexp.MustCompile(`(?i)^\*{3}\s*Add\s+File:\s*`)
	patchHeader = regexp.MustCompile(`^@@`)
)

// isPatchContent reports whether content opens with a "*** Begin Patch" marker (after
// leading whitespace), distinguishing a patch from full-file replacement content.
func isPatchContent(content string) bool {
	return patchStart.MatchString(strings.TrimLeft(content, " \t\r\n"))
}

// parsePatchHunks splits a patch into hunks. Begin/End/File markers are skipped; each @@
// header opens a new hunk; a '-' line removes, a '+' line inserts, and a ' ' (space) line
// is context kept in both. Lines outside a hunk are ignored, mirroring the oracle. A hunk
// opened after an "*** Add File" marker (and before the next File marker) is flagged addFile.
func parsePatchHunks(content string) []patchHunk {
	lines := strings.Split(content, "\n")
	var hunks []patchHunk
	inHunk := false
	var current patchHunk
	have := false
	addSection := false

	flush := func() {
		if have && (len(current.oldLines) > 0 || len(current.newLines) > 0) {
			hunks = append(hunks, current)
		}
	}

	for _, line := range lines {
		if patchFile.MatchString(line) {
			addSection = patchAdd.MatchString(line)
			continue
		}
		if patchStart.MatchString(line) || patchEnd.MatchString(line) {
			continue
		}

		if patchHeader.MatchString(line) {
			flush()
			current = patchHunk{addFile: addSection}
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

// errUnanchoredHunk refuses a pure-insertion hunk (no context or removal line) against a
// non-empty file outside an "*** Add File" section: nothing in the hunk says where its lines
// belong, and the oracle's answer — append at file-end — silently misplaces an insertion
// meant for the middle of the file.
var errUnanchoredHunk = errors.New("patch hunk has only '+' lines, so its position in the file is unknown: " +
	"include at least one unchanged context line (prefixed with a space) or a '-' line next to the insertion")

// errHunkMismatch reports a hunk whose joined oldLines do not occur in the file.
var errHunkMismatch = errors.New("patch hunk did not match file content")

// applyPatch applies each hunk to original by locating its joined oldLines verbatim and
// substituting its joined newLines. A pure-insertion hunk (no oldLines) has no anchor, so it
// appends only when original is empty or the hunk sits under "*** Add File"; anywhere else it
// is refused with errUnanchoredHunk. Any refusal or unmatched hunk returns an error, leaving
// the caller to discard the result so the file is never corrupted. Ported from the oracle's
// indexOf-based applier.
func applyPatch(original string, hunks []patchHunk) (string, error) {
	result := original

	for _, hunk := range hunks {
		if len(hunk.oldLines) == 0 {
			if original != "" && !hunk.addFile {
				return "", errUnanchoredHunk
			}
			result += strings.Join(hunk.newLines, "\n")
			continue
		}

		needle := strings.Join(hunk.oldLines, "\n")
		idx := strings.Index(result, needle)
		if idx == -1 {
			return "", errHunkMismatch
		}

		before := result[:idx]
		after := result[idx+len(needle):]
		result = before + strings.Join(hunk.newLines, "\n") + after
	}

	return result, nil
}

var (
	_ domain.ReadOnlyTool   = (*EditExistingFile)(nil)
	_ workspaceScopedWriter = (*EditExistingFile)(nil)
)
