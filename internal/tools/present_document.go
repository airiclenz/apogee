package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// PresentDocumentToolName is the stable name the model calls to show a finished document to the
// human. It is exported for the same reason AskUserToolName is: the engine withholds the tool from
// every sub-agent by name (plan 2026-09-14 - 03, item 5).
const PresentDocumentToolName = "present_document"

var presentDocumentSpec = toolSpec{
	name:        PresentDocumentToolName,
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
// when one is configured AND the file-only present.command-on-model-documents key opts that
// rung in — ADR 0019 §5's 2026-09-22 addendum; without it the rung is skipped for the baseline
// and the result below says so). The model never reasons about platforms and never supplies a
// command — but it does supply the ARGUMENT, which is why an execution-capable rung 3 needs
// the opt-in.
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
//
// The path is resolved over the same read scope the read tools use — the workspace first, then
// the host's read mounts (2026-09-15) — because a document is a thing the model READS back to the
// user: a report it drafted in the announced scratch dir, or a skill's bundled reference, is as
// presentable as one in the workspace, and a tool that refused the very dir the orientation named
// as the place for drafts was the regression this closes. A document under a MOUNT degrades one
// rung on a remote session: the doc server fences every grant to the workspace root (present
// .DocServer.Serve), so rung 2 cannot serve it and the presenter reports the baseline — the
// result wording says so, so the model relays a truthful claim on either kind of session.
type PresentDocument struct {
	toolSpec
	root      string
	scope     readScope
	presenter domain.Presenter
}

// NewPresentDocument returns a present_document tool that resolves paths within root — and, for
// an absolute path the workspace refuses, over mounts, exactly as the read tools do — and routes
// them to presenter. A nil presenter yields a tool whose Execute reports the delegate is
// unavailable (the registry omits it in practice).
func NewPresentDocument(root string, mounts ReadMounts, presenter domain.Presenter) *PresentDocument {
	return &PresentDocument{
		toolSpec:  presentDocumentSpec,
		root:      root,
		scope:     readScope{root: root, mounts: mounts},
		presenter: presenter,
	}
}

// ReadOnly reports that present_document performs no writes (showing a document mutates
// nothing), so the disposition runs it freely in every mode — including Plan.
func (t *PresentDocument) ReadOnly() bool { return true }

// IsExecutionCapable forwards the host Presenter's own answer: whether the opener rung the host
// wired can execute a program of the user's choosing (a configured present.command on a local
// session). It asks the delegate the tool already holds rather than a live handle, so the seam
// stays stateless across Turns (ADR 0008), and a nil delegate — a hand-built registry that
// registered the tool without a Presenter — answers false, the same graceful reading Execute
// gives it.
//
// It executes nothing and decides nothing: present_document remains ReadOnly and runs in every
// mode. The answer only lets the result wording name the mechanism the host actually holds.
func (t *PresentDocument) IsExecutionCapable() bool {
	if t.presenter == nil {
		return false
	}
	return t.presenter.IsExecutionCapable()
}

// Execute resolves the named document inside the workspace or a read mount, confirms it is an
// existing regular file, and hands it to the Presenter, returning result text that names the rung
// the host actually reached so the model can relay it truthfully.
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

	// Resolved over the read scope: the workspace first, then the mounts, and a path no root
	// accepts is the workspace's own uniform escape refusal (readScope.resolve). The matched root
	// is what every fenced step below is pinned to — a document under a mount must not be
	// measured against the workspace.
	root, path, err := t.scope.resolve(args.Path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	// The existence-and-kind check runs on a descriptor opened THROUGH the fence, named by the
	// root-relative form of the resolved path: a plain os.Stat on the resolved string re-walks
	// it and would follow a component swapped to point outside the root after resolve checked
	// it. Re-fencing what the PRESENTER later opens is the document server's own business (the
	// per-request fence), not this tool's.
	relative := workspaceRelative(path, root)
	info, err := statInRoot(relative, root)
	if err != nil {
		return errorResult(call.ID, notFoundOrRefusal(err, "file not found: ", root, relative, args.Path)), nil
	}
	if !info.Mode().IsRegular() {
		return errorResult(call.ID, "not a file: "+args.Path), nil
	}
	// The transcript's display name: workspace-relative for a workspace document (the short name
	// the terminal linkifies against the project), the resolved ABSOLUTE path for one under a
	// mount — a mount-relative name would read as a workspace file that does not exist.
	display := relative
	mounted := root != t.root
	if mounted {
		display = path
	}

	outcome, err := t.presenter.Present(ctx, domain.PresentRequest{
		Path:        path,
		DisplayPath: display,
		Title:       strings.TrimSpace(args.Title),
		// Which run is presenting, read off the ctx the engine installed it on: the depth places
		// the document at the presenting agent's level and the spawn call ID inside that agent's
		// own run, which is what keeps a child's presentation from surfacing as the top-level
		// agent's. Both are stamped here, where the request is BUILT, for the same reason the
		// asking agent's identity is stamped in ask_user — it is a fact about the presentation,
		// and the Presenter is an interface boundary away from the Agent that knows it. A CHILD
		// presentation is no longer reachable (2026-09-15, plan 2026-09-14 - 03, item 5: the tool
		// is withheld from every sub-agent), so both read as the top-level run's honest zero
		// values; they are still stamped from the ctx rather than hard-coded, because the carrier
		// is the engine's and the tool has no business assuming its depth.
		Depth:       domain.SubAgentDepthFromContext(ctx),
		SpawnCallID: domain.SpawnCallIDFromContext(ctx),
	})
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		return okResult(call.ID, renderPresented(display, domain.PresentOutcome{Method: domain.PresentShown}, mounted)), nil
	}
	return okResult(call.ID, renderPresented(display, outcome, mounted)), nil
}

// renderPresented turns the outcome into the sentence the model relays. Each rung gets its
// own truthful claim — "opened on the user's machine" is a promise only the opener rung can
// keep — and everything else falls through to the baseline wording, which is the one claim
// that is never wrong: an unknown Method (the enum is open, ADR 0019) means the model may only
// say the path is in the transcript.
//
// outcome.Location is interpolated on NO rung. A served URL carries the doc server's capability
// token (ADR 0019 §3) and this result is model context — POSTed upstream on the next Turn and
// persisted with the session — so the link's whole reach is the transcript entry. Defence in
// depth: the Presenter already hands back the display path, and a host that still returned a URL
// could not leak it through here.
//
// mounted says the document lies under a read mount rather than the workspace, and appends the
// one degradation that carries: the doc server serves the workspace alone, so on a remote session
// such a document reaches the user as its path and nothing more. It is stated on every rung — the
// tool cannot see which kind of session it runs in, and the model relays what it is told.
//
// outcome.CommandWithheld appends the OTHER degradation the result can word: the host holds an
// application the user named and did not run it on a document the model named, because the opt-in
// is off. It is the only degradation the user can act on, so the sentence names the key and how to
// set it — a model that reports "the path is shown" and nothing else leaves the user with no way
// to find out why their present.command did not fire.
func renderPresented(display string, outcome domain.PresentOutcome, mounted bool) string {
	var rung string
	switch outcome.Method {
	case domain.PresentOpened:
		rung = "opened on the user's machine."
	case domain.PresentServed:
		rung = "shown in the transcript with a link."
	default:
		rung = "the path is shown in the transcript for the user to open."
	}
	if outcome.CommandWithheld {
		rung += " " + presentedCommandWithheldNote
	}
	if mounted {
		rung += " " + presentedMountNote
	}
	return "Presented " + display + ": " + rung
}

// presentedMountNote is the sentence a document under a read mount carries in its result: the
// rung-2 degradation stated once, in the model's own result text, rather than left for a remote
// user to discover.
const presentedMountNote = "Outside the workspace it is served locally; a remote session shows the path only."

// presentedCommandWithheldNote is the sentence a withheld rung 3 carries in its result: the user's
// present.command was not run on this document because the opt-in that allows it on a document the
// MODEL named is off. It names the key and the value that turns it on, in the config file that is
// the only place it is set — the user reading the model's report is the one person who can act on
// it, and a degradation nobody can locate is no better than a silent one.
const presentedCommandWithheldNote = "The user's present.command was not run on it: " +
	"set `present.command-on-model-documents: true` in the present: block of ~/.apogee/config.yaml to allow that."

var (
	_ domain.Tool         = (*PresentDocument)(nil)
	_ domain.ReadOnlyTool = (*PresentDocument)(nil)
)
