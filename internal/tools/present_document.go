package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var presentDocumentSpec = toolSpec{
	name:        "present_document",
	description: "Show a finished document to the user. Call this after writing a report or other deliverable file the user should read; it opens or links the document for them.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "The document to show, relative to the workspace root or absolute. The file must already exist."},
    "title": {"type": "string", "description": "Optional short label for the document, shown to the user above the path."}
  }
}`),
}

type presentDocumentArgs struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// PresentDocument shows a finished document — the report a Skill just wrote, any deliverable
// the user is meant to read — to the human (ADR 0019). It is the single, dumb, explicitly
// named affordance that replaces platform guessing: the model names a file, the HOST picks
// the mechanism through the presentation ladder (the transcript baseline always, the OS
// opener on a local desktop, a doc-server URL when remote, the user's own present.command
// when configured). The model never reasons about platforms and never supplies a command.
//
// It routes through the host-supplied Presenter delegate, the sibling of Asker (P3.11): it is
// mode-INDEPENDENT (always the delegate, never the Approval/disposition gate), it is NOT a
// safety gate, and it is NOT an ExternalEffectTool — the user's own display is no more a
// non-forkable remote for the bench to stub than the human answering ask_user is. It is
// ReadOnly() — presenting writes nothing — so the disposition runs it freely in every mode,
// INCLUDING Plan, which is where a plan document most wants to be read.
//
// A nil Presenter means the tool is never registered (DefaultToolsWithHost omits it), so by
// construction Execute always has a non-nil Presenter; the defensive nil-check below keeps a
// hand-built registry that registers it with a nil Presenter from panicking. Stateless across
// Turns (ADR 0008): a delegate reference and a root, no live handle — the doc server behind
// the delegate is the HOST's, with a lifetime tied to the app rather than to a Turn.
type PresentDocument struct {
	toolSpec
	root      string
	presenter domain.Presenter
}

// NewPresentDocument returns a present_document tool that resolves paths within root and
// routes them to presenter. A nil presenter yields a tool whose Execute reports the delegate
// is unavailable (the registry omits it in practice).
func NewPresentDocument(root string, presenter domain.Presenter) *PresentDocument {
	return &PresentDocument{toolSpec: presentDocumentSpec, root: root, presenter: presenter}
}

// ReadOnly reports that present_document performs no writes (showing a document mutates
// nothing), so the disposition runs it freely in every mode — including Plan.
func (t *PresentDocument) ReadOnly() bool { return true }

// Execute resolves the named document inside the workspace, confirms it is an existing
// regular file, and hands it to the Presenter, returning result text that names the rung the
// host actually reached so the model can relay it truthfully.
//
// A cancelled ctx is a Go error so the loop rolls the Turn back (ADR 0007). Any OTHER
// Presenter error is deliberately NOT an error result: rung 0 — the path in the transcript —
// happened host-side regardless, so a failed mechanism degrades to the "shown" wording rather
// than telling the model a presentation it can see in the transcript never happened (ADR 0019
// §4, fail visible, degrade to the baseline). Only the model's own mistakes — a missing
// argument, a path that escapes the workspace, a directory or a file that is not there — are
// error results.
func (t *PresentDocument) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[presentDocumentArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return errorResult(call.ID, "path is required"), nil
	}
	if t.presenter == nil {
		return errorResult(call.ID, "present_document is unavailable: no Presenter delegate is configured"), nil
	}

	path, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return errorResult(call.ID, "file not found: "+args.Path), nil
	}
	if !info.Mode().IsRegular() {
		return errorResult(call.ID, "not a file: "+args.Path), nil
	}

	display := workspaceRelative(path, t.root)
	outcome, err := t.presenter.Present(ctx, domain.PresentRequest{
		Path:        path,
		DisplayPath: display,
		Title:       strings.TrimSpace(args.Title),
	})
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		return okResult(call.ID, renderPresented(display, domain.PresentOutcome{Method: domain.PresentShown})), nil
	}
	return okResult(call.ID, renderPresented(display, outcome)), nil
}

// renderPresented turns the outcome into the sentence the model relays. Each rung gets its
// own truthful claim — "opened on the user's machine" is a promise only the opener rung can
// keep — and everything else falls through to the baseline wording, which is the one claim
// that is never wrong: an unknown Method (the enum is open, ADR 0019) and a served rung with
// no URL both mean the model may only say the path is in the transcript.
func renderPresented(display string, outcome domain.PresentOutcome) string {
	switch outcome.Method {
	case domain.PresentOpened:
		return "Presented " + display + ": opened on the user's machine."
	case domain.PresentServed:
		if strings.TrimSpace(outcome.Location) != "" {
			return "Presented " + display + ": shown in the transcript with a link (" + outcome.Location + ")."
		}
	}
	return "Presented " + display + ": the path is shown in the transcript for the user to open."
}

// workspaceRelative renders an already-resolved absolute path in its workspace-relative form —
// the short text the transcript carries for the terminal to linkify. It measures against the
// SYMLINK-RESOLVED root because resolveInRoot returns a real path: on a box where the root is
// reached through a symlink (macOS /tmp) a plain Rel against the configured root would answer
// with a "../.."-laden path. Anything that still will not relativise falls back to the
// absolute path, which is longer but never wrong.
func workspaceRelative(path, root string) string {
	rel, err := filepath.Rel(security.EvalRealPath(filepath.Clean(root)), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

var (
	_ domain.Tool         = (*PresentDocument)(nil)
	_ domain.ReadOnlyTool = (*PresentDocument)(nil)
)
