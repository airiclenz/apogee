package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/present"
)

// ----------------------------------------------------------------------------
// Presentation (ADR 0019) — the host walking the ladder for a finished document
// ----------------------------------------------------------------------------

// Presentation is the host-side kit the composition root resolves from config and hands the
// [Bridge]: the rungs of the presentation ladder this TUI can actually walk. It is the
// presentation analogue of [ConfinementInfo] — facts and mechanisms the binary owns, handed to
// the renderer rather than derived by it, so internal/tui never reads config or the environment
// itself.
//
// Every field may be zero, and a zero field is a rung the ladder skips rather than a fault: rung
// 0 — the transcript entry — is the only rung that always runs, and it needs nothing from here
// (ADR 0019 §2).
type Presentation struct {
	// Opener is rungs 1 and 3: the OS opener, or the application named in present.command. It is
	// consulted only on a Local session (see Local). A nil Opener means those rungs are NOT wired
	// — `present.auto-open: false`, or a host that wants no opening at all — and the ladder skips
	// straight to the baseline without calling it a failure.
	Opener *present.Opener
	// Docs is rung 2: the capability-token doc server a Remote session serves browser-renderable
	// documents through. A nil server means the rung is not wired and a remote presentation stays
	// at the baseline.
	Docs *present.DocServer
	// Local reports whether this session runs on the user's own machine (present.Locality ==
	// present.Local). It is the ladder's own gate on rung 1/3: an opener fired from a remote box
	// opens into a display nobody is watching (ADR 0019, "auto-opening on the remote box" —
	// rejected), so locality is asked here and never delegated.
	Local bool
	// CommandOnModelDocuments is `present.command-on-model-documents`, the file-only opt-in that
	// lets an EXECUTION-CAPABLE rung 3 — an Opener carrying a present.command — run on a document
	// the model named. False (the default, and an absent key) means the ladder skips rung 3 and
	// degrades straight to the baseline rather than to rung 1: present.command REPLACES the OS
	// opener wherever it is set (present.Opener.CommandOverride), so there is no lower opener rung
	// left to fall to, and inventing one would open a document in an application the user did not
	// choose.
	//
	// It lives HERE, with the ladder, rather than on the Opener, for the reason the Opener's own
	// doc gives: the Opener decides WHAT to run, and whether a rung runs at all is the ladder's
	// call. The Opener is handed a document the model named on every rung, so it could not tell
	// this case apart in any event.
	CommandOnModelDocuments bool
}

// executionCapable is the ladder's own answer to "can this wiring run a program of the user's
// choosing": a LOCAL session (rung 1/3's gate — see Local) whose Opener carries a non-empty
// present.command. A nil Opener, a blank override and a remote session all answer false, because
// none of them can reach an application the user named.
//
// It is a method on the SNAPSHOT rather than on the presenter so the question and the climb that
// acts on it read the same rungs: one Present call sees one ladder (see ladder), and a predicate
// that took its own snapshot could answer about a ladder the climb never walked.
func (p Presentation) executionCapable() bool {
	return p.Local && p.Opener != nil && strings.TrimSpace(p.Opener.CommandOverride) != ""
}

// uiPresenter is the host's Presenter: the delegate present_document routes a finished
// deliverable to, and the thing that actually walks the presentation ladder (ADR 0019 §2). It is
// the sibling of [uiAsker] — the same late-bound programRef, the same "called synchronously
// inside a Step, on the worker goroutine" contract — with one deliberate difference: there is NO
// human rendezvous. It picks a rung, attempts it, hands the Update loop a [presentedMsg], and
// returns; it never blocks on the UI, so a presentation cannot stall a Turn waiting for a human.
//
// Rung 0 is the presentedMsg itself and is unconditional: whatever the mechanisms above it did,
// the transcript carries the workspace-relative path. That is why a failed mechanism is not an
// error — it is a degraded outcome the entry describes in words ("no opener on this machine —
// path shown") and the tool result relays truthfully (ADR 0019 §4).
//
// THE DESKTOP GATE LIVES IN EXACTLY ONE PLACE, AND IT IS NOT HERE (decided 2026-07-21). The
// ladder gates rung 1/3 on locality alone and lets [present.Opener] answer "is there anything on
// this machine to open into", because a configured present.command deliberately STANDS IN for the
// desktop check (an OS with no built-in opener is precisely the case the override exists for). A
// second HasDesktop test here would contradict that on the one configuration the user was most
// explicit about. Locality is the ladder's own and is never bypassed: present.command says which
// application shows a document, not which machine the user is sitting at.
//
// The rungs are MUTABLE behind a lock (ADR 0037 decision 1): a `present.*` key committed in the
// `/settings` pane rebuilds the ladder and installs it here, on the presenter the engine already
// holds — the engine captured this pointer at construction, so replacing the presenter would be
// invisible and swapping its rungs is the only way a live edit can reach a presentation. The lock
// is real: the write comes from the Update goroutine (the pane's keypress) and climb reads from the
// worker goroutine, inside a Step.
type uiPresenter struct {
	prog *programRef

	mu    sync.RWMutex
	rungs Presentation
}

// setRungs installs a rebuilt ladder for every presentation from here on. A presentation already
// climbing keeps the rungs it read — one Present call sees one ladder — which is what makes a swap
// safe beside a running Turn.
func (p *uiPresenter) setRungs(rungs Presentation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rungs = rungs
}

// ladder is the read half: one snapshot per presentation, taken before the first rung is attempted.
func (p *uiPresenter) ladder() Presentation {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rungs
}

// uiPresenter is the engine's Presenter.
var _ domain.Presenter = (*uiPresenter)(nil)

// IsExecutionCapable reports whether the ladder this host has wired can run a program of the
// user's own choosing: a LOCAL session (rung 1/3's own gate — see Presentation.Local) whose
// Opener carries a non-empty present.command. A nil Opener, a blank override and a remote
// session all answer false, because none of them can reach an application the user named.
//
// It reads the same one-snapshot-per-question ladder Present does and answers from configuration
// alone — nothing is resolved, launched or classified here.
func (p *uiPresenter) IsExecutionCapable() bool {
	return p.ladder().executionCapable()
}

// Present walks the ladder for one document and records the result in the transcript. It returns
// an error ONLY when ctx is already cancelled — a stopping Turn gets no presentation at all, which
// is the fail-safe direction (ADR 0007: the loop rolls the Turn back). Every other outcome,
// including a mechanism that failed, is a normal [domain.PresentOutcome]: the baseline rung ran,
// so the document did reach the user.
func (p *uiPresenter) Present(ctx context.Context, req domain.PresentRequest) (domain.PresentOutcome, error) {
	if err := ctx.Err(); err != nil {
		return domain.PresentOutcome{}, err
	}

	walk := p.climb(ctx, req)

	// Rung 0, unconditionally and last: the entry describes whatever the rungs above it managed.
	// send is asynchronous (bridge.go), so the worker never waits on the Update loop.
	//
	// Depth and SpawnCallID ride along untouched: they are the presenting agent's identity as the
	// engine stamped it (domain.PresentRequest), and rung 0 is the only place that can put the
	// entry where that run's other entries are.
	p.prog.send(presentedMsg{
		Title:       req.Title,
		Path:        req.DisplayPath,
		Location:    walk.location,
		Method:      walk.method,
		Reason:      walk.reason,
		Depth:       req.Depth,
		SpawnCallID: req.SpawnCallID,
	})

	// The outcome's Location is the display path on EVERY rung (domain.PresentOutcome), including a
	// served one: the served URL carries the doc server's capability token (ADR 0019 §3), and the
	// outcome is model context — relayed in the tool result, POSTed upstream on the next Turn and
	// persisted with the session. The URL's whole reach is the entry sent just above.
	return domain.PresentOutcome{
		Method:   walk.method,
		Location: req.DisplayPath,
		// The one degradation the user can act on travels back as data, so the tool result can name
		// the key that would have changed it (domain.PresentOutcome.CommandWithheld).
		CommandWithheld: walk.commandWithheld,
	}, nil
}

// climbResult is what one walk of the ladder produced: the rung reached, the served URL (empty
// unless rung 2 carried it), the short reason the transcript entry shows when a rung was tried or
// withheld, and whether an execution-capable rung 3 was the thing withheld. It is a struct rather
// than a fourth return value because the last two are one story told twice — once to the user in
// the transcript, once to the model in the tool result — and they must never disagree.
type climbResult struct {
	method   domain.PresentMethod
	location string
	reason   string
	// commandWithheld is set on exactly one branch: an execution-capable ladder with
	// present.command-on-model-documents off. It rides out on domain.PresentOutcome.
	commandWithheld bool
}

// climb attempts the highest rung that applies to this session and reports what happened: the
// method reached, the served URL (empty unless rung 2 carried it), and — when a rung was tried and
// did not deliver — a short reason for the entry to show. A skipped rung yields no reason: nothing
// failed when a host simply wired no opener.
//
// The two branches are exclusive by design. A Local session climbs rung 1/3 and, if that does not
// deliver, degrades to the baseline rather than falling through to rung 2: a URL is only useful to
// a machine that is not this one, and on a local box with no desktop there is no browser to open it
// (ADR 0019 rung 2 is remote by definition). ctx is re-checked before each mechanism so a user stop
// lands promptly instead of after a launch grace or a bind.
//
// One rung is WITHHELD rather than attempted: an execution-capable ladder (a present.command the
// user configured) opens a document the MODEL named only with
// present.command-on-model-documents set. Without it the walk stops here — before the Opener is
// consulted at all, so nothing is resolved and nothing is launched — and the document reaches the
// user on the baseline rung with a reason that names the key.
func (p *uiPresenter) climb(ctx context.Context, req domain.PresentRequest) climbResult {
	rungs := p.ladder()
	if rungs.Local {
		if rungs.Opener == nil || ctx.Err() != nil {
			return climbResult{method: domain.PresentShown}
		}
		if rungs.executionCapable() && !rungs.CommandOnModelDocuments {
			return climbResult{
				method:          domain.PresentShown,
				reason:          commandWithheldReason,
				commandWithheld: true,
			}
		}
		err := rungs.Opener.Open(req.Path)
		switch {
		case err == nil:
			return climbResult{method: domain.PresentOpened}
		case errors.Is(err, present.ErrNoOpener):
			// Not a failure: this machine has nothing to open into (a headless Linux session, an
			// OS with no opener and no present.command). The baseline rung is the right answer.
			return climbResult{method: domain.PresentShown, reason: "no opener on this machine"}
		default:
			return climbResult{
				method: domain.PresentShown,
				reason: "could not open: " + clipDetail(firstLine(err.Error())),
			}
		}
	}

	if rungs.Docs == nil || !browserRenderable(req.Path) || ctx.Err() != nil {
		return climbResult{method: domain.PresentShown}
	}
	url, err := rungs.Docs.Serve(req.Path)
	if err != nil {
		return climbResult{
			method: domain.PresentShown,
			reason: "could not serve: " + clipDetail(firstLine(err.Error())),
		}
	}
	return climbResult{method: domain.PresentServed, location: url}
}

// commandWithheldReason is the transcript entry's short reason for the one withheld rung — the
// user's own words for their own setting, in the register the other reasons use ("no opener on
// this machine"). The model is told the same thing in the tool result, in its own longer wording
// (tools.presentedCommandWithheldNote); this is the half the user reads.
const commandWithheldReason = "present.command not run on a model-named document " +
	"(present.command-on-model-documents is off)"

// browserRenderableExts is the set rung 2 serves: the documents a browser renders itself rather
// than downloads (ADR 0019 §2). The doc server is deliberately extension-AGNOSTIC — it will serve
// anything it is handed — so the judgement of what is worth a URL lives here, with the ladder, and
// a later markdown→HTML rung is a change to this set rather than to the server.
//
// It is no longer the narrow half of a nested pair. Rung 1's own set (present.OpenerRenderable) is
// still wider for everything INERT — an OS handler shows the .docx and .png a browser would only
// download — but the three active formats here (.html, .htm, .svg) were removed from it on
// 2026-08-12 (ADR 0019, fourth amendment), so the two sets now CROSS on .pdf rather than nest.
//
// The crossing is what the ladder is for rather than a hole in it: a served document carries a
// restrictive Content-Security-Policy (internal/present, documentCSP), and a file:// launch carries
// none, so the rung that can bound active content is the rung that shows it. The consequence is
// visible to the user — a LOCAL `present_document report.html` degrades to the baseline transcript
// rung and launches no browser, because climb's two branches are exclusive. A test in this package
// pins the crossing, in both directions.
var browserRenderableExts = map[string]bool{
	".html": true,
	".htm":  true,
	".svg":  true,
	".pdf":  true,
}

// browserRenderable reports whether path is worth serving to the user's browser. The extension is
// lowercased first, so a Windows-authored REPORT.HTML is the same document as report.html.
func browserRenderable(path string) bool {
	return browserRenderableExts[strings.ToLower(filepath.Ext(path))]
}
