package domain

import "context"

// ----------------------------------------------------------------------------
// Presentation (ADR 0019) — the host delegate that shows a finished document
// ----------------------------------------------------------------------------

// Presenter is the host-supplied delegate the present_document tool routes a finished
// deliverable to: the model names a document it has just written and the HOST decides how
// the user sees it (the presentation ladder — the transcript baseline always, the OS opener
// when the session is local and a desktop exists, a doc-server URL when it is remote, a
// user-configured command when one is set). The model supplies a path, never a mechanism.
//
// It is the sibling of Asker (P3.11): the same host-decides delegate shape, for showing a
// document rather than asking a question. Like Asker it is NOT a safety gate — a Presenter
// carries no allow/deny semantics and never bypasses the Approval/disposition machinery —
// and it is MODE-INDEPENDENT: presenting writes nothing, so the tool is ReadOnly and runs in
// every mode, Plan included.
//
// It is consulted synchronously inside a Step (on the worker goroutine) but, unlike Asker,
// it awaits no human rendezvous and must never block on the user. It must FAIL SAFE under
// cancellation: when ctx is cancelled it returns promptly rather than finishing a mechanism,
// so a cancelled Turn is never held open by a presentation.
//
// A nil Presenter means the present_document tool is simply NOT REGISTERED (graceful), so a
// headless / non-interactive host never offers the model an affordance nobody can honour.
//
// Fail visible, degrade to the baseline (ADR 0019): a mechanism that fails — no opener on
// this box, a doc server that cannot bind — is not an error, because the baseline rung has
// already put the path in front of the user. An implementation reports the rung it actually
// reached (PresentShown) instead; an error is reserved for a presentation that reached the
// user in no form at all.
type Presenter interface {
	Present(ctx context.Context, req PresentRequest) (PresentOutcome, error)
}

// PresentRequest is the document put in front of the user. It is a STRUCT (not a bare path)
// for freeze-safety (D7), the same reason AskRequest is one: a post-v1 field (a content-type
// hint, a "reveal in folder" flag) is then an additive, non-breaking change to the v1.0.0
// surface.
type PresentRequest struct {
	// Path is the ABSOLUTE path of the document to present. The tool has already resolved it
	// inside the workspace root and confirmed it is an existing regular file, so a Presenter
	// receives a path it may hand straight to a mechanism.
	Path string

	// DisplayPath is Path in its workspace-relative form — the text the transcript carries as
	// plain text on its own line, for the terminal (Zed / VS Code / iTerm2 / WezTerm / kitty)
	// to linkify. It is display-only: mechanisms use Path.
	DisplayPath string

	// Title is an optional human label for the document; it MAY be empty. The host renders it
	// above the path when set.
	Title string
}

// PresentOutcome reports which rung of the presentation ladder actually carried the document
// to the user. A STRUCT for the same freeze-safety reason as PresentRequest, and the reason
// the tool never has to assert a success it cannot observe: it relays this outcome verbatim.
type PresentOutcome struct {
	// Method is the rung that ran — the highest one that succeeded, degraded to PresentShown
	// when everything above the baseline failed or did not apply.
	Method PresentMethod

	// Location is where the user finds the document: the served URL for PresentServed, the
	// DisplayPath otherwise.
	Location string
}

// PresentMethod names the presentation-ladder rung that carried a document to the user. The
// tool result echoes it so the model can tell the user the truth ("opened on your machine"
// vs. "the path is shown in the transcript") rather than claiming an outcome it never saw.
// The set is open (additively extensible — treat unknown values defensively).
type PresentMethod string

const (
	// PresentOpened: the host opened the document on the user's own machine — the OS opener
	// (rung 1) or the configured present.command (rung 3).
	PresentOpened PresentMethod = "opened"
	// PresentServed: the document is registered with the doc server and its URL joined the
	// transcript entry (rung 2), for the user's terminal to linkify into the host's browser.
	PresentServed PresentMethod = "served"
	// PresentShown: the baseline alone (rung 0) — the workspace-relative path stands in the
	// transcript for the user to open. It is equally the outcome of a degraded higher rung,
	// which is why it is a normal result and not an error.
	PresentShown PresentMethod = "shown"
)
