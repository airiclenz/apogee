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
//
// GROWING THE INTERFACE IS A BREAKING CHANGE, and IsExecutionCapable was added knowing it
// (2026-09-22). PresentRequest and PresentOutcome are structs precisely so post-v1 growth stays
// additive for an out-of-tree implementer (see PresentRequest below); a new METHOD has no such
// escape — every Presenter, including one written against the re-exported apogee.Presenter, must
// answer it. It was taken deliberately: the engine cannot tell the user why a presentation
// degraded without asking the host what its ladder can actually do, and only the host knows.
type Presenter interface {
	Present(ctx context.Context, req PresentRequest) (PresentOutcome, error)

	// IsExecutionCapable reports whether the opener rung THIS host has wired can execute a
	// program of the user's own choosing — true exactly when a non-empty present.command is
	// configured for a session running on the user's machine, false for every rung-0/1/2-only
	// wiring, a nil opener and a remote session alike.
	//
	// It is a FACT the engine reads, not a permission it spends: answering it executes nothing,
	// resolves nothing and changes no mode, classification or ladder behaviour. It exists so a
	// tool result can word a degraded presentation in terms of the mechanism the host actually
	// holds, without a live host handle crossing the quiescent boundary (ADR 0008).
	//
	// It is answered per call rather than captured once: the rungs are rebuilt in place when a
	// `present.*` key is committed live (ADR 0037), so a cached answer would outlive its truth.
	IsExecutionCapable() bool
}

// PresentRequest is the document put in front of the user. It is a STRUCT (not a bare path)
// for freeze-safety (D7), the same reason AskRequest is one: a post-v1 field (a content-type
// hint, a "reveal in folder" flag) is then an additive, non-breaking change to the v1.0.0
// surface.
type PresentRequest struct {
	// Path is the ABSOLUTE path of the document to present. The tool has already resolved it
	// inside the workspace root — or, since 2026-09-15, inside one of the host's read mounts
	// (the session scratch dir, a skills library) — and confirmed it is an existing regular
	// file, so a Presenter receives a path it may hand straight to a mechanism. A mechanism
	// fenced to the workspace (the doc server) refuses a mounted document and the Presenter
	// degrades to the baseline rung, which the tool's result wording already states.
	Path string

	// DisplayPath is Path in its workspace-relative form — the text the transcript carries as
	// plain text on its own line, for the terminal (Zed / VS Code / iTerm2 / WezTerm / kitty)
	// to linkify. For a document under a read mount it is Path itself, absolute: a
	// mount-relative name would read as a workspace file that does not exist. It is
	// display-only: mechanisms use Path.
	DisplayPath string

	// Title is an optional human label for the document; it MAY be empty. The host renders it
	// above the path when set.
	Title string

	// Depth is the sub-agent nesting level of the agent that presented: 0 = the top-level agent,
	// a child = its parent + 1 (ADR 0013), the same number EventBase.Depth carries on that
	// agent's events. A Driver that rails a nested run needs it to draw the presentation at the
	// depth its run is drawn at — without it a child's document surfaces as if the top-level
	// agent had shown it.
	//
	// 0 is an honest value, not an absence, which is why it rides a ctx carrier installed for
	// every tool call (WithSubAgentDepth) rather than one only a child installs.
	Depth int

	// SpawnCallID is the run identity of the presenting agent: the id of the sub_agent call that
	// spawned it (EventBase.CallID), empty for the top-level agent. Depth alone cannot tell two
	// SIBLING runs of a depth-0 fan-out apart (ADR 0039), so this is what lets a Driver place the
	// presentation inside the run that raised it instead of merely at the right level. It rides
	// WithSpawnCallID, installed beside the depth.
	SpawnCallID string
}

// PresentOutcome reports which rung of the presentation ladder actually carried the document
// to the user. A STRUCT for the same freeze-safety reason as PresentRequest, and the reason
// the tool never has to assert a success it cannot observe: it relays this outcome verbatim.
type PresentOutcome struct {
	// Method is the rung that ran — the highest one that succeeded, degraded to PresentShown
	// when everything above the baseline failed or did not apply.
	Method PresentMethod

	// Location is the DisplayPath, on every rung — never a served URL: the tool result is model
	// context, sent upstream on the next Turn and persisted with the session, and the doc-server
	// URL carries a capability token (ADR 0019 §3). Where the user finds a served document is the
	// transcript entry's to say.
	Location string

	// CommandWithheld says the ONE degradation the user can act on: this host has an
	// execution-capable rung 3 wired (a present.command on a local session) and did not run it,
	// because `present.command-on-model-documents` is not set. Every other degradation is a fact
	// about the machine — no opener, a server that could not bind — and reads the same whatever
	// the user does next; this one is a setting of theirs, so the tool result names it.
	//
	// It is a FIELD rather than a live handle for the reason PresentOutcome is a struct at all
	// (freeze-safety, and ADR 0008's quiescent boundary): the reason crosses back as data the tool
	// renders, not as a host the tool may ask again later.
	//
	// False on every other outcome, including an opened one — a rung that ran withheld nothing.
	CommandWithheld bool
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
