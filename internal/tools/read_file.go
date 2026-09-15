package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/doctext"
	"github.com/airiclenz/apogee/internal/domain"
)

var readFileSpec = toolSpec{
	name:        "read_file",
	description: "Read the contents of a file by path, optionally restricted to a line range, and optionally locating the line numbers where a substring occurs; absolute paths under a configured read-only root (such as the skills library) are also readable. Without a range the first 400 lines (or 40 KiB) come back and the tail says how to get the rest. PDF files are detected by content and returned as extracted plain text with [Page N] markers; other binary files are refused.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "File path to read, relative to the workspace root or absolute"},
    "start_line": {"type": "integer", "description": "Optional 1-based start line"},
    "end_line": {"type": "integer", "description": "Optional 1-based end line (inclusive)"},
    "max_lines": {"type": "integer", "description": "Maximum number of lines to return"},
    "locate": {"type": "string", "description": "Optional substring to locate; the result reports the absolute 1-based line numbers where it occurs. The whole file is always scanned, even when a line range narrows the returned content. Without a range the content is a window of 10 lines around each hit rather than the whole file."}
  }
}`),
}

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	MaxLines  int    `json:"max_lines"`
	Locate    string `json:"locate"`
}

// What one read_file call returns when the call itself set no bound — constants, not config keys
// (ratified 2026-09-14): a session-mining round found 1,410 reads over 500 lines and a
// 1,020,168-character read of a CHANGELOG, each one a context window spent on a file the model
// wanted a page of. A call that names a range or max_lines is honoured as written.
const (
	// defaultReadLines is the most lines an open-ended read returns.
	defaultReadLines = 400
	// defaultReadBytes is the most body bytes an open-ended read returns; whichever of the two
	// bounds is reached first ends the body, always after at least one line.
	defaultReadBytes = 40 * 1024
	// locateWindowLines is the context on either side of a locate hit when no range narrows the
	// read — the same ±10 a grep with context_lines: 10 shows.
	locateWindowLines = 10
)

// ReadFile reads a file's contents, optionally restricted to a line range and optionally
// reporting where a substring occurs. It is a read-only tool scoped to a sandbox root plus
// any extra read-only roots the host mounted (readScope).
type ReadFile struct {
	toolSpec
	scope readScope
}

// NewReadFile returns a read_file tool that resolves paths within root, and — for ABSOLUTE
// paths only — within any extra read-only root mounts reports at call time, plus any virtual
// mount it names. A zero ReadMounts means workspace-only: byte-identical to the fence before
// either mount seam existed.
func NewReadFile(root string, mounts ReadMounts) *ReadFile {
	return &ReadFile{toolSpec: readFileSpec, scope: mounts.scope(root)}
}

// ReadOnly reports that read_file performs no writes (domain.ReadOnlyTool).
func (t *ReadFile) ReadOnly() bool { return true }

// Execute reads the file named in call.Arguments and returns its content, honouring
// ctx cancellation. Bad arguments, a missing file, an oversized file, or a path that
// escapes the root are reported as IsError results, not Go errors.
//
// The fence is enforced at OPEN time through an os.Root pinned at the root the path was
// accepted under — the workspace, or, for an absolute path the workspace refuses, the first
// configured extra read-only root that contains it — so a path component swapped to point
// outside that root, including a concurrent swap by a confined subprocess mid-call, is
// refused rather than followed (security review H1). A path under no root is refused with
// the workspace's own uniform escape message, whatever the extra roots happen to be.
// The open, the size check and the read share ONE descriptor (readWorkspaceFileBounded),
// so there is no check/use gap between them: a rename mid-call changes nothing the call
// sees, and a file grown past the cap mid-read is refused (see the SCOPE note in
// internal/security/safeio.go).
//
// The pinned root resolves RELATIVE components only, so an in-root symlink whose target is
// spelled as an absolute path is refused even when that target is inside the root. That is
// narrower than the former resolveInRoot + unfenced stat + unfenced read trio, which read
// such a link; the fence is tighten-only, so the narrowing is kept and recorded in the
// CHANGELOG (Unreleased → Security). Relative in-root symlinks read as they did before.
//
// A read that DOES follow a link discloses it: the result text ends with the same
// ` → resolves to <path>` tail the write tools append when the argument named one path and the
// operation landed on another (resolvedTargetNote). An ordinary read grows nothing.
//
// A file whose bytes say it is a PDF is returned as its EXTRACTED TEXT (internal/doctext), never as
// raw bytes: a document that cannot be read is an IsError result carrying the extractor's
// model-facing sentence. Everything after the extraction is the plain-read pipeline unchanged —
// start_line, end_line, max_lines and locate address the extracted text's lines, and only the
// header's display path says the file was a PDF. Any OTHER file with a NUL in its head is a binary
// and is refused in one line (looksBinary, shared with grep) rather than served as text.
//
// What comes back is bounded by the CALL before it is bounded by the file: an open-ended read
// stops at defaultReadLines / defaultReadBytes and says so in its tail, an inverted or past-the-end
// range is an IsError result rather than an empty success, and a locate with no range returns
// windows around the hits rather than the whole file (renderFile).
func (t *ReadFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[readFileArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	content, failMessage := t.scope.readBounded(args.Path)
	if failMessage != "" {
		return errorResult(call.ID, failMessage), nil
	}

	body, displayPath, extractFailure := readableText(ctx, content, args.Path)
	if extractFailure != "" {
		return errorResult(call.ID, extractFailure), nil
	}

	// Where these bytes REALLY came from, when that is not where the argument said
	// (resolvedTargetNote — the writers' disclosure tail, appended to the rendered text so the
	// model and the transcript read the same sentence the write tools give). A read FOLLOWS a
	// symlink rather than replacing it, so the note is the only place the redirection is said
	// out loud: without it the header quotes an argument that named one file while the body
	// carries another's bytes.
	//
	// The root is the one that served the READ, not the workspace assumed (readScope.readRoot):
	// an absolute path accepted by a configured read-only root is resolved under that root. The
	// live roots are evaluated a second time here, which cannot disagree with the read's own
	// evaluation about anything this note says: a relative path never consults the extra roots
	// at all (readScope.extraRoots), and for an absolute path the root argument is unused —
	// resolveTargetUnbounded cleans such a path on its own.
	text, span, rangeFailure := renderFile(displayPath, body, args)
	if rangeFailure != "" {
		return errorResult(call.ID, rangeFailure), nil
	}
	return okSummary(call.ID, text+resolvedTargetNote(args.Path, t.scope.readRoot(args.Path)), span), nil
}

// readableText turns the bytes read off disk into the text renderFile should show and the path
// its header should name. Plain files pass through untouched; a PDF — judged by its content, so
// a text file called notes.pdf still reads as text — becomes its extracted text under a display
// path annotated with the format and page count.
//
// A non-empty failMessage is meant to go straight to errorResult: the extractor's own model-facing
// sentence for a PDF, or the one-line binary refusal for any other file whose head holds a NUL
// (looksBinary — judged AFTER the PDF test, since a PDF's own bytes would fail it). read_file never
// falls back to raw bytes either way, because a wall of binary teaches the model nothing and costs
// it a context window to learn it: a session-mining round found a 10 MB executable served as
// 42,654 lines of text, after which the model's final reply was a single token.
//
// Extraction is bounded by the SAME ceiling the raw read is (maxFileReadBytes, path_read.go) and
// by the call's context: a document is a file this tool refuses above ten mebibytes, so the text
// walked out of one has no business exceeding what the file itself was allowed to be, and a
// cancelled call stops a long walk instead of finishing it for nobody.
func readableText(ctx context.Context, content []byte, path string) (body, displayPath, failMessage string) {
	if !doctext.IsPDF(content) {
		if looksBinary(content) {
			return "", "", fmt.Sprintf("read_file: %s is a binary file (%d bytes)", path, len(content))
		}
		return string(content), path, ""
	}

	extracted, pages, extractFailure := doctext.ExtractPDF(ctx, content, maxFileReadBytes)
	if extractFailure != "" {
		return "", "", extractFailure
	}
	return extracted, pdfDisplayPath(path, pages), ""
}

// pdfDisplayPath annotates the path for renderFile's header so the model reads the lines below
// as a document's extracted text rather than a file's own bytes, and knows how much document
// they cover. It is a HEADER annotation only — the fence and the resolved-target note keep
// using the argument's real path. The words come from doctext, which is also where the @file
// block's header gets them, so one document never announces itself two ways.
func pdfDisplayPath(path string, pages int) string {
	return path + " (" + doctext.PDFAnnotation(pages) + ")"
}

// renderFile selects the requested line range and prepends a header naming the file
// and the lines shown, mirroring the oracle's read output. It returns the same three
// numbers the header states as a domain.ReadSpan, so a host reads the span as data
// instead of parsing it back out of the sentence. A non-empty failMessage is a refusal of
// the RANGE the call asked for (rangeRefusal) — an inverted range or a start past the end
// — worded for the model; nothing else is returned with it.
//
// An open-ended read — no end_line and no max_lines — is capped at defaultReadLines /
// defaultReadBytes and, when the cap bit, the body ends with a tail naming the lines shown and
// how to ask for the rest; the header and the span state the CAPPED range, never the whole
// file's. An explicit end_line or max_lines is honoured as written (renderRange).
//
// When args.Locate is set, the WHOLE file is scanned — not just the selected range — and a
// single "Located …" line naming the absolute 1-based line numbers is emitted between the
// header and the content, so a match outside a narrowed span is still reported. The span
// carries those same numbers as data. With no range on the call, the content beneath that line is
// the ±locateWindowLines window around each hit rather than the file (locateWindows): a miss
// renders the plain capped body. An empty Locate means none was requested and the output is
// byte-identical to a plain read.
func renderFile(displayPath, content string, args readFileArgs) (text string, span domain.ReadSpan, failMessage string) {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	if failMessage := rangeRefusal(args, totalLines); failMessage != "" {
		return "", domain.ReadSpan{}, failMessage
	}

	var locatedOn []int
	if args.Locate != "" {
		for i, line := range lines {
			if strings.Contains(line, args.Locate) {
				locatedOn = append(locatedOn, i+1)
			}
		}
	}

	var body string
	hasRange := args.StartLine > 0 || args.EndLine > 0 || args.MaxLines > 0
	if len(locatedOn) > 0 && !hasRange {
		body, span = locateWindows(lines, locatedOn)
	} else {
		body, span = renderRange(lines, args)
	}
	span.Total = totalLines

	header := fmt.Sprintf("[File: %s, %d lines total, showing lines %d-%d]",
		displayPath, totalLines, span.Start, span.End)
	if args.Locate == "" {
		return header + "\n" + body, span, ""
	}

	span.Locate = args.Locate
	span.LocatedOn = locatedOn
	return header + "\n" + locateReport(span.Locate, span.LocatedOn) + "\n" + body, span, ""
}

// rangeRefusal words the two range arguments a read cannot honour: an end_line before its
// start_line, and a start_line past the file's last line. Each was an EMPTY success before —
// "0 lines" with outcome ok, which a session-mining round saw a model take at face value fifteen
// times in one session — and is a refusal now, so the model learns the range was wrong rather than
// that the file was empty. An empty string means the range is honourable.
func rangeRefusal(args readFileArgs, totalLines int) string {
	if args.StartLine > 0 && args.EndLine > 0 && args.EndLine < args.StartLine {
		return fmt.Sprintf("read_file: end_line (%d) is before start_line (%d)", args.EndLine, args.StartLine)
	}
	if args.StartLine > totalLines {
		return fmt.Sprintf("read_file: start_line (%d) is past the end of the file (%d lines)", args.StartLine, totalLines)
	}
	return ""
}

// renderRange selects the lines the call asked for and bounds them: an explicit max_lines
// truncates with the "[...truncated at N lines]" marker as it always did; a call that set neither
// end_line nor max_lines is open-ended and stops at the default cap (capDefault), with the
// "[showing lines S-E of M — pass start_line/end_line for the rest]" tail when the cap bit. A
// start_line on its own leaves the tail open and is capped like a bare read — a model handed the
// tail's hint and passing only start_line would otherwise be served the whole remainder. The span
// carries the lines actually shown; its Total is the caller's to set.
func renderRange(lines []string, args readFileArgs) (string, domain.ReadSpan) {
	totalLines := len(lines)
	start := 0
	if args.StartLine > 0 {
		start = args.StartLine - 1
	}
	end := totalLines
	if args.EndLine > 0 && args.EndLine < end {
		end = args.EndLine
	}
	selected := lines[start:end]

	tail := ""
	switch {
	case args.MaxLines > 0 && len(selected) > args.MaxLines:
		selected = selected[:args.MaxLines]
		tail = fmt.Sprintf("\n[...truncated at %d lines]", args.MaxLines)
	case args.EndLine == 0 && args.MaxLines == 0:
		if shown := capDefault(selected); len(shown) < len(selected) {
			selected = shown
			tail = fmt.Sprintf("\n[showing lines %d-%d of %d — pass start_line/end_line for the rest]",
				start+1, start+len(selected), totalLines)
		}
	}

	span := domain.ReadSpan{Start: start + 1, End: start + len(selected)}
	return strings.Join(selected, "\n") + tail, span
}

// capDefault returns the longest prefix of selected that fits the default bounds: at most
// defaultReadLines lines, and at most defaultReadBytes bytes once joined with newlines — but never
// fewer than one line, so a single oversized line is shown whole rather than the body being empty.
// Lines are never cut in the middle: what the model reads is always whole lines of the file.
func capDefault(selected []string) []string {
	if len(selected) > defaultReadLines {
		selected = selected[:defaultReadLines]
	}
	size := -1 // the first line brings no joining newline
	for i, line := range selected {
		size += len(line) + 1
		if size > defaultReadBytes && i > 0 {
			return selected[:i]
		}
	}
	return selected
}

// locateWindows renders the ±locateWindowLines window around each located line, merged where
// they overlap or touch (mergeLineWindows, the same merge grep's context uses) and clipped to
// the file, with a lone "…" line between windows that do not meet. A union that covers the whole
// file is byte-identical to the plain read. The span is the union's first Start and last End —
// the lines the body reaches across, not a count of the lines it holds.
func locateWindows(lines []string, locatedOn []int) (string, domain.ReadSpan) {
	windows := mergeLineWindows(locatedOn, locateWindowLines)
	parts := make([]string, 0, len(windows))
	for _, w := range windows {
		if w.to > len(lines) {
			w.to = len(lines)
		}
		parts = append(parts, strings.Join(lines[w.from-1:w.to], "\n"))
	}
	first, last := windows[0], windows[len(windows)-1]
	span := domain.ReadSpan{Start: first.from, End: min(last.to, len(lines))}
	return strings.Join(parts, "\n…\n"), span
}

// locateReport words the one-line locate result: the 1-based line numbers the term was
// found on, or "on no lines" when it occurs nowhere. The sentence is BUILT from the same
// numbers the summary carries, so the two can never disagree.
func locateReport(locate string, locatedOn []int) string {
	if len(locatedOn) == 0 {
		return fmt.Sprintf("Located %q on no lines", locate)
	}

	numbers := make([]string, len(locatedOn))
	for i, n := range locatedOn {
		numbers[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf("Located %q on lines: %s", locate, strings.Join(numbers, ", "))
}

// Ensure ReadFile satisfies the domain.Tool contract at compile time. The same guard
// is repeated for each tool so a signature drift fails the build here, not at wiring.
var _ domain.ReadOnlyTool = (*ReadFile)(nil)
