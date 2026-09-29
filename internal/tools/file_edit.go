package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var fileEditSpec = toolSpec{
	name:        "edit_existing_file",
	description: "Edit an existing file. Accepts either full replacement content or a patch in \"*** Begin Patch\" format. Each @@ hunk needs a ' ' context or '-' line as its anchor, except in an empty file or *** Add File.",
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
// file-edit-tool, including its hunk parser; its applier matches each hunk on whole lines and
// refuses a hunk that matches more than one place.
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
// A patch sent with CRLF endings has each line's "\r" dropped, so hunk lines carry no ending
// of their own and take the target file's (see applyPatch).
func parsePatchHunks(content string) []patchHunk {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
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

// errHunkMismatch reports a hunk whose oldLines do not occur in the file as consecutive whole lines.
var errHunkMismatch = errors.New("patch hunk did not match file content")

// applyPatch applies each hunk to original by locating its oldLines as consecutive whole lines of
// the text and substituting its newLines. Lines are compared without their line ending, a
// trailing "\r" counting as part of the ending, so a hunk matches a CRLF file line by line just
// as it matches an LF one. Trailing empty context lines are dropped from both sides first, so a
// patch's closing blank context line does not demand a blank line in the file. A hunk that
// matches nowhere returns errHunkMismatch; one that matches more than once is refused as
// ambiguous, as find_replace refuses repeated old text, rather than landing on whichever
// occurrence comes first. A pure-insertion hunk (no oldLines) has no anchor, so it appends only
// when original is empty or the hunk sits under "*** Add File"; anywhere else it is refused with
// errUnanchoredHunk. An append onto text lacking a final newline adds the separator first and
// ends the appended lines with a newline, so it never glues onto the last line. Any refusal
// returns an error, leaving the caller to discard the result so the file is never corrupted.
//
// The substituted lines are joined with the line ending found where the hunk lands (see
// lineEndingAt), and the matched block's own final ending — or its absence at the end of the
// text — is kept, so a CRLF file stays CRLF and every line outside the block is untouched. In a
// file of mixed endings the whole block takes the ending of the line it starts on: a context
// line inside it that ended differently is rewritten with that ending, which beats guessing a
// per-line ending for added lines that have no counterpart in the file. When an earlier hunk
// has left the text without any newline, the ending of original's last line stands in, so a
// CRLF file does not turn LF halfway through a patch.
//
// A hunk with no newLines removes its matched lines together with their endings (see
// removeLines) rather than leaving an empty line in their place.
//
// The empty remainder after a final newline is not a line, so a hunk whose oldLines end in an
// empty line never matches there: "-b" then "-" (an empty removal line) against "b\n" is refused
// as a mismatch, exactly as a lone empty needle is, while "-b" alone removes the line cleanly.
func applyPatch(original string, hunks []patchHunk) (string, error) {
	result := original
	fallbackEOL := lineEndingAt(original, len(original), "\n")

	for _, hunk := range hunks {
		oldLines, newLines := trimTrailingEmptyContext(hunk.oldLines, hunk.newLines)

		if len(oldLines) == 0 {
			if original != "" && !hunk.addFile {
				return "", errUnanchoredHunk
			}
			result = appendLines(result, newLines, fallbackEOL)
			continue
		}

		lines := splitTextLines(result)
		matches := wholeLineMatches(lines, oldLines)
		switch {
		case len(matches) == 0:
			return "", errHunkMismatch
		case len(matches) > 1:
			return "", fmt.Errorf("patch hunk matched %d places (must match exactly one)%s: "+
				"add unchanged context lines (prefixed with a space) until the hunk is unique",
				len(matches), matchLinesNote(matches))
		}

		if len(newLines) == 0 {
			result = removeLines(result, lines, matches[0], len(oldLines))
			continue
		}

		first, last := lines[matches[0]], lines[matches[0]+len(oldLines)-1]
		end := last.start + len(last.content)
		eol := lineEndingAt(result, first.start, fallbackEOL)
		result = result[:first.start] + strings.Join(newLines, eol) + result[end:]
	}

	return result, nil
}

// removeLines removes count lines of text starting at line index from, endings included, so no
// empty line is left behind. When the block runs to an unterminated last line, the ending of the
// line before it goes too: "a\nb" minus "b" is "a", not "a\n", so the removal never invents a
// final newline the file did not have. Removing every line leaves empty text.
func removeLines(text string, lines []textLine, from, count int) string {
	start, end := lines[from].start, len(text)
	if next := from + count; next < len(lines) {
		end = lines[next].start
	} else if !strings.HasSuffix(text, "\n") && from > 0 {
		prev := lines[from-1]
		start = prev.start + len(prev.content)
	}
	return text[:start] + text[end:]
}

// trimTrailingEmptyContext drops the empty lines both sides of a hunk end with — trailing blank
// context lines, which a model's patch often carries as an artefact and which would otherwise
// have to match a blank line in the file.
func trimTrailingEmptyContext(oldLines, newLines []string) ([]string, []string) {
	for len(oldLines) > 0 && len(newLines) > 0 &&
		oldLines[len(oldLines)-1] == "" && newLines[len(newLines)-1] == "" {
		oldLines = oldLines[:len(oldLines)-1]
		newLines = newLines[:len(newLines)-1]
	}
	return oldLines, newLines
}

// appendLines appends lines to text, joined with the ending of text's last line, or fallback when
// text holds no newline. Text that is empty or already ends with a newline takes the joined lines
// as they are; non-empty text lacking a final newline gets a separator first, and the appended
// lines end with a newline so the file ends on a whole line.
func appendLines(text string, lines []string, fallback string) string {
	eol := lineEndingAt(text, len(text), fallback)
	joined := strings.Join(lines, eol)
	if text == "" || strings.HasSuffix(text, "\n") {
		return text + joined
	}
	return text + eol + joined + eol
}

// lineEndingAt returns the line ending ("\r\n" or "\n") of the line containing byte offset at,
// or, when that line is the text's unterminated last one, of the line before it; text with no
// newline at all gets fallback.
func lineEndingAt(text string, at int, fallback string) string {
	nl := strings.IndexByte(text[at:], '\n')
	if nl >= 0 {
		nl += at
	} else {
		nl = strings.LastIndexByte(text[:at], '\n')
	}
	switch {
	case nl < 0:
		return fallback
	case nl > 0 && text[nl-1] == '\r':
		return "\r\n"
	default:
		return "\n"
	}
}

// textLine is one line of the text a hunk is matched against: the byte offset it starts at and
// its content without its line ending.
type textLine struct {
	start   int
	content string
}

// splitTextLines splits text into lines at each "\n", stripping the "\n" and a "\r" before it
// (or a "\r" ending the unterminated last line) from the content. The empty remainder after a
// final newline is not a line; empty text is one empty line.
func splitTextLines(text string) []textLine {
	var lines []textLine
	for start := 0; ; {
		n := strings.IndexByte(text[start:], '\n')
		if n < 0 {
			if start < len(text) || start == 0 {
				lines = append(lines, textLine{start: start, content: strings.TrimSuffix(text[start:], "\r")})
			}
			return lines
		}
		lines = append(lines, textLine{start: start, content: strings.TrimSuffix(text[start:start+n], "\r")})
		start += n + 1
	}
}

// wholeLineMatches returns the index of every line in lines where want occurs as consecutive
// whole lines. Overlapping matches all count, since each is a place the hunk could land.
func wholeLineMatches(lines []textLine, want []string) []int {
	var matches []int
	for i := 0; i+len(want) <= len(lines); i++ {
		if linesEqualAt(lines[i:], want) {
			matches = append(matches, i)
		}
	}
	return matches
}

// linesEqualAt reports whether lines begins with want, line for line.
func linesEqualAt(lines []textLine, want []string) bool {
	for k, w := range want {
		if lines[k].content != w {
			return false
		}
	}
	return true
}

// matchLinesNote names the 1-based lines where each match starts, as " — at lines 2, 4",
// from the 0-based line indices wholeLineMatches returns.
func matchLinesNote(matches []int) string {
	spelled := make([]string, len(matches))
	for i, line := range matches {
		spelled[i] = strconv.Itoa(line + 1)
	}
	return " — at lines " + strings.Join(spelled, ", ")
}

var (
	_ domain.ReadOnlyTool   = (*EditExistingFile)(nil)
	_ workspaceScopedWriter = (*EditExistingFile)(nil)
)
