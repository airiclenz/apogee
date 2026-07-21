package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

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
type uiPresenter struct {
	prog  *programRef
	rungs Presentation
}

// uiPresenter is the engine's Presenter.
var _ domain.Presenter = (*uiPresenter)(nil)

// Present walks the ladder for one document and records the result in the transcript. It returns
// an error ONLY when ctx is already cancelled — a stopping Turn gets no presentation at all, which
// is the fail-safe direction (ADR 0007: the loop rolls the Turn back). Every other outcome,
// including a mechanism that failed, is a normal [domain.PresentOutcome]: the baseline rung ran,
// so the document did reach the user.
func (p *uiPresenter) Present(ctx context.Context, req domain.PresentRequest) (domain.PresentOutcome, error) {
	if err := ctx.Err(); err != nil {
		return domain.PresentOutcome{}, err
	}

	method, location, reason := p.climb(ctx, req)

	// Rung 0, unconditionally and last: the entry describes whatever the rungs above it managed.
	// send is asynchronous (bridge.go), so the worker never waits on the Update loop.
	p.prog.send(presentedMsg{
		Title:    req.Title,
		Path:     req.DisplayPath,
		Location: location,
		Method:   method,
		Reason:   reason,
	})

	// The outcome's Location is where the user finds the document (domain.PresentOutcome): the URL
	// when one was served, the display path otherwise — which is what the transcript shows.
	if location == "" {
		location = req.DisplayPath
	}
	return domain.PresentOutcome{Method: method, Location: location}, nil
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
func (p *uiPresenter) climb(ctx context.Context, req domain.PresentRequest) (domain.PresentMethod, string, string) {
	if p.rungs.Local {
		if p.rungs.Opener == nil || ctx.Err() != nil {
			return domain.PresentShown, "", ""
		}
		err := p.rungs.Opener.Open(req.Path)
		switch {
		case err == nil:
			return domain.PresentOpened, "", ""
		case errors.Is(err, present.ErrNoOpener):
			// Not a failure: this machine has nothing to open into (a headless Linux session, an
			// OS with no opener and no present.command). The baseline rung is the right answer.
			return domain.PresentShown, "", "no opener on this machine"
		default:
			return domain.PresentShown, "", "could not open: " + clipDetail(firstLine(err.Error()))
		}
	}

	if p.rungs.Docs == nil || !browserRenderable(req.Path) || ctx.Err() != nil {
		return domain.PresentShown, "", ""
	}
	url, err := p.rungs.Docs.Serve(req.Path)
	if err != nil {
		return domain.PresentShown, "", "could not serve: " + clipDetail(firstLine(err.Error()))
	}
	return domain.PresentServed, url, ""
}

// browserRenderableExts is the set rung 2 serves: the documents a browser renders itself rather
// than downloads (ADR 0019 §2). The doc server is deliberately extension-AGNOSTIC — it will serve
// anything it is handed — so the judgement of what is worth a URL lives here, with the ladder, and
// a later markdown→HTML rung is a change to this set rather than to the server.
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
