package main

// The presentation seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// The host-side presentation ladder (ADR 0019) as this machine can walk it: the builder that wires
// only the rungs this session could use, and the holder that owns the `present:` block as it stands
// now — rebuilding the ladder, re-installing it on the presenter, and owning the doc server's
// lifetime, since a rebuild is the only thing that can end one mid-session (ADR 0037 binding D).

import (
	"fmt"
	"sync"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/present"
	"github.com/airiclenz/apogee/internal/tui"
)

// presentationRungs builds the host-side presentation ladder (ADR 0019) from the resolved
// `present:` block and this session's environment: the mechanisms that exist on THIS machine, for
// the TUI's presenter to walk. goos and env are injected — every seam in internal/present is, for
// exactly this reason — so the wiring is table-testable off whatever machine the tests run on.
//
// A rung is wired only where this session could walk it, because internal/tui reads a zero field
// as "a rung this host did not wire" rather than as a failure (tui.Presentation):
//
//   - the Opener (rungs 1 and 3) on a LOCAL session with auto-open on. Remote is excluded here
//     rather than inside the Opener because an opener fired on a remote box opens into a display
//     nobody is watching; `auto-open: false` wires none either, which covers the command override
//     too — the key says whether a document is opened, present.command only says by what.
//   - the doc server (rung 2) on a REMOTE session, where the user's browser is on another machine.
//     It binds nothing until the first served presentation, so wiring it costs one struct. Its
//     advertised address is resolved HERE, once: AdvertiseHost may probe the routing table, and
//     where the user reaches this box from cannot change mid-session. It is also handed the
//     workspace root — the same root the file tools are scoped to — because it re-checks every
//     served document against that fence on every request, not once at the grant.
//
// Rung 0 — the transcript line carrying the path — is deliberately absent: it needs no mechanism,
// it is never skipped, and nothing in the config can turn it off.
func presentationRungs(p config.PresentSettings, workspace, goos string, env func(string) string) tui.Presentation {
	rungs := tui.Presentation{Local: present.Locality(env) == present.Local}
	if rungs.Local && p.AutoOpen {
		rungs.Opener = &present.Opener{GOOS: goos, Env: env, CommandOverride: p.Command}
	}
	if !rungs.Local {
		rungs.Docs = &present.DocServer{
			Host: present.AdvertiseHost(env, p.Host),
			Port: p.Port,
			Root: workspace,
		}
	}
	return rungs
}

// livePresentation owns the `present:` block as it stands right now and the ladder built from it —
// the presentation half of ADR 0037 decision 1. The four keys are editable in the `/settings` pane,
// and a committed one lands here: the block is updated, the rungs are rebuilt exactly as startup
// built them, and they are re-installed on the presenter (tui.Bridge.SetPresentation, which swaps
// the rungs of the presenter the engine captured rather than making a second one).
//
// It also owns the doc server's LIFETIME, because a rebuild is the only thing that can end one
// mid-session: the app's own teardown closes whatever is current, and an address change closes the
// server it displaced (ADR 0037 binding D — the URLs a moved listener issued die with it, which is
// inherent to changing the port).
//
// The mutex guards the pair — settings and rungs must never disagree — and the writes come from the
// Update goroutine while the presenter reads its own copy of the rungs under its own lock.
type livePresentation struct {
	mu       sync.Mutex
	settings config.PresentSettings
	rungs    tui.Presentation

	// The three inputs presentationRungs takes beside the block, held so a rebuild is the identical
	// call startup made. env is injected for the same reason it is injected there: so the whole
	// holder is testable off whatever machine the tests run on.
	workspace string
	goos      string
	env       func(string) string
	// install hands the rebuilt ladder to the renderer; wired to tui.Bridge.SetPresentation.
	install func(tui.Presentation)
}

// newLivePresentation builds this session's ladder and installs it — the call that also makes
// bridge.Presenter() non-nil, and with it registers present_document.
func newLivePresentation(p config.PresentSettings, workspace, goos string, env func(string) string,
	install func(tui.Presentation)) *livePresentation {
	live := &livePresentation{
		settings:  p,
		workspace: workspace,
		goos:      goos,
		env:       env,
		install:   install,
	}
	live.rungs = presentationRungs(p, workspace, goos, env)
	install(live.rungs)
	return live
}

// apply moves one `present.` key and re-installs the ladder built from the block it leaves behind.
// The value is the one the file now spells; it is parsed here rather than trusted, exactly as the
// other dispatcher entries parse theirs.
func (l *livePresentation) apply(key, value string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	next := l.settings
	switch key {
	case "present.auto-open":
		on, err := settingBool(key, value)
		if err != nil {
			return err
		}
		next.AutoOpen = on
	case "present.command":
		next.Command = value
	case "present.port":
		port, err := settingInt(key, value)
		if err != nil {
			return err
		}
		next.Port = port
	case "present.host":
		next.Host = value
	default:
		return fmt.Errorf("apogee: %s is not a presentation setting", key)
	}
	// The startup check on the whole block, so a value that would fail deep inside the first
	// presentation is refused here instead — the same validate() the registry's own validator runs.
	if err := next.Validate(); err != nil {
		return err
	}

	rungs := presentationRungs(next, l.workspace, l.goos, l.env)
	// A doc server whose address did not move keeps SERVING: the listener stays bound and the grants
	// it issued stay valid, because nothing about it changed (an auto-open or command edit on a
	// remote session rebuilds an identical rung). One whose address did move is displaced, and the
	// old one is closed below — after the new ladder is installed, so no presentation in between
	// climbs to a server that is going away.
	displaced := l.rungs.Docs
	if displaced != nil && rungs.Docs != nil && sameDocServer(displaced, rungs.Docs) {
		rungs.Docs, displaced = displaced, nil
	}

	l.settings, l.rungs = next, rungs
	l.install(rungs)
	if displaced != nil {
		_ = displaced.Close()
	}
	return nil
}

// sameDocServer reports whether two doc servers would bind and advertise the same thing — the whole
// of what configuration says about one (its three exported fields; everything else is the running
// state a rebuilt value has none of). Equal means the bound listener may simply be carried over.
func sameDocServer(a, b *present.DocServer) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Root == b.Root
}

// close ends the session's presentation: the current doc server's listener, whichever one that is
// after any number of `present.port` edits. Idempotent, like DocServer.Close itself.
func (l *livePresentation) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rungs.Docs != nil {
		_ = l.rungs.Docs.Close()
	}
}
