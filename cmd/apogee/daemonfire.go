package main

// The daemon's Firing composition (ADR 0034, ADR 0055) — the third Driver over the embeddable
// engine (ADR 0031), beside the TUI's scheduleWiring (schedule.go) and runHeadless (headless.go).
//
// A Firing raised here is the same unattended run those two compose — literally the same composer,
// firingConfig in wire_firing.go — resolved from a different set of facts: not the session's live
// binding and not one command's flags, but one validated entry of
// `~/.apogee/daemon/schedules.yaml` — which server it names, which model, which workspace, which
// mode — read against a `config.yaml` this process loaded once at startup.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
)

// daemonWiring turns one Firing into one unattended run. It is the value wired into
// [schedule.Config.Fire], and its shape follows the daemon's two reload surfaces rather than the
// TUI's live session:
//
//   - The HOST half — the resolved config, the key sources, the confinement backend, the sessions
//     store, the validated `mechanisms:` list — is resolved ONCE, by newDaemonWiring, because
//     `config.yaml` is read once at startup and rebinding servers is a daemon restart (ADR 0055).
//   - The ENTRY half — which server, which model, which workspace, which mode — is resolved per
//     Firing off the adopted set, because `schedules.yaml` IS live-reloaded (ADR 0034) and the
//     entry a name resolves to is whatever the last accepted edit said it was.
//
// That split is why the adopted set lives here rather than beside the daemon's loop: fire runs on
// the Scheduler's own goroutines while a reload swaps the set on the watcher's, so the map and its
// lock belong with the reader that has to survive the swap.
//
// The delegates that assume a human are never composed at all. run.Once pins its own fail-safe
// denier and leaves ask_user and present_document unregistered (ADR 0033, decision 2), and Tools
// stays nil because a Firing reaches no external MCP server (ADR 0034) — the engine builds its own
// registry instead.
type daemonWiring struct {
	// opts is the host's resolved configuration: the startup server selection flattened onto it,
	// the `servers:` list a schedule binds into by name, and every file-only key an unattended run
	// must honour for the one-configuration reason headless honours them (ADR 0031).
	opts config.Options
	// manualIDs is the enable list an explicit `mechanisms:` block spells, validated once at
	// startup — enabled keys AND disabled ones, exactly as a session validates them, because the
	// engine only ever sees the enabled IDs and a typo'd disabled key would otherwise never be
	// reported (ADR 0015 §1).
	manualIDs []apogee.MechanismID
	// keys resolves an entry's key SOURCE into the token its Firings send. One resolver for the
	// daemon's lifetime, so a `api-key-cmd:` runs once per entry rather than once per Firing — it
	// is goroutine-safe, which is what lets Firings on different Schedules share it.
	keys *config.KeyResolver
	// confiner is this host's confinement backend, built once through the newConfiner seam. A
	// Firing takes it whatever its mode: an Auto entry is fenced by it, and a Plan entry's terminal
	// commands are confined by the same posture a session's are. Whether this host may run Auto
	// unattended at all was ruled on at VALIDATION (internal/daemon's Host.AutoEligible), where the
	// refusal could name the entry, so nothing here re-asks. [daemonWiring.closeConfiner] is the
	// other end of it: a backend that has to put the disk back is torn down when the daemon stops.
	confiner apogee.Confiner
	// store is the shared sessions store every Firing's record lands in, so a schedule's runs are
	// browsable in /sessions beside the conversations and headless runs on this host (ADR 0034).
	store *session.Store

	// mu guards adopted, which the daemon's reload replaces wholesale while Firings read it.
	mu sync.RWMutex
	// adopted is the daemon's name→Entry map: the daemon-only half of `run:` — workspace, server,
	// model — which deliberately does not travel through [schedule.Spec] because the scheduler
	// library is runner-agnostic (ADR 0033). A Firing carries the name; this is what turns it back
	// into the instruction the file states.
	adopted map[string]daemon.Entry
}

// newDaemonWiring resolves everything about the HOST that every Firing of this daemon shares, and
// fails before a clock is started when any of it is wrong: a `mechanisms:` key naming no Mechanism
// is a defect in the config the daemon must report at startup rather than at 3am in a saved record.
//
// The adopted set starts empty. A daemon adopts its first file immediately after this, through the
// same [daemonWiring.adopt] call every later reload makes.
func newDaemonWiring(opts config.Options) (*daemonWiring, error) {
	roots, err := resolveRoots(opts.ConfigDir, "")
	if err != nil {
		return nil, err
	}
	manualIDs, err := mechanismIDs(opts.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		return nil, err
	}

	// The scratch sweep, run once at startup for the reason runRoot runs it (wire.go): a daemon
	// mints a fresh dir per Firing and never passes the TUI's boot, so this is the only beat on
	// which a host that only ever runs daemons reclaims the dirs its Firings left behind.
	// Best-effort and silent, exactly as it is there — GC is never a reason a daemon fails to start.
	gcScratchDirs(roots.scratch, time.Now())

	return &daemonWiring{
		opts:      opts,
		manualIDs: manualIDs,
		keys:      config.NewKeyResolver(),
		confiner:  newConfiner(),
		store:     session.NewStore(roots.sessions),
		adopted:   make(map[string]daemon.Entry),
	}, nil
}

// closeConfiner tears the confinement backend down at the end of the daemon's life and reports the
// one failure that has no other surface. Two of the three backends need nothing; the Windows token
// backend has to put the disk back (ADR 0020 §2), and a teardown that could not is a silently
// mutated disk the user is otherwise never told about — which is why runRoot and runHeadless make
// this same optional-interface assertion, through the same wording (internal/platform).
//
// It returns the notice rather than printing it, because where a daemon's narration goes is the
// daemon's decision (daemon.go), not this file's.
func (w *daemonWiring) closeConfiner() string {
	closer, ok := w.confiner.(interface{ Close() error })
	if !ok {
		return ""
	}
	return platform.ConfinementTeardownNotice(closer.Close())
}

// adopt replaces the set a Firing resolves its name against. It is called once with the file the
// daemon started on and once per accepted reload, always with the WHOLE validated set: an edit is
// all-or-nothing (ADR 0034), so there is no partial swap to express.
//
// A Firing already in flight is unaffected — it resolved its entry when it started, which is the
// instruction it was raised under.
func (w *daemonWiring) adopt(entries []daemon.Entry) {
	adopted := make(map[string]daemon.Entry, len(entries))
	for _, entry := range entries {
		adopted[entry.Name] = entry
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.adopted = adopted
}

// entryFor answers for one schedule name with the entry the daemon last adopted under it.
func (w *daemonWiring) entryFor(name string) (daemon.Entry, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	entry, adopted := w.adopted[name]
	return entry, adopted
}

// fire performs one Firing and reports the record it left behind. It is [schedule.Config.Fire] for
// the daemon's single Scheduler, and it runs on that Scheduler's goroutine — never the reload's —
// which is why everything it touches is its own copy or explicitly goroutine-safe (the adopted map,
// the key resolver, the store, the skills Provider, the Confiner).
//
// The Config it runs against is composed by [firingConfig] (wire_firing.go), the one composer every
// Driver's unattended run is built by — an unattended run is an unattended run whichever Driver
// raised it (ADR 0031). What this Driver decides is what the ENTRY, rather than a flag or a live
// session, decides: the server it names, the model it overlays, and the workspace it runs in.
func (w *daemonWiring) fire(ctx context.Context, f schedule.Firing) (schedule.Outcome, error) {
	entry, adopted := w.entryFor(f.ScheduleName)
	if !adopted {
		return schedule.Outcome{}, fmt.Errorf("apogee: daemon: the %q schedule fired but no entry of that name is "+
			"adopted — the schedule was taken off the clock while this tick was due", f.ScheduleName)
	}

	// The `servers:` entry this Firing binds to, and the roots it lives in — the entry's own
	// workspace over the daemon's working directory, which is the one root a schedule decides.
	// Everything else those roots name is home-derived and shared by every Firing.
	server, err := w.serverFor(entry)
	if err != nil {
		return schedule.Outcome{}, err
	}
	roots, err := resolveRoots(w.opts.ConfigDir, entry.Run.Workspace)
	if err != nil {
		return schedule.Outcome{}, err
	}

	// This Firing's own record id, minted here because the runner is handed it beside the Config
	// (run.Spec) and the composer creates the run's scratch dir under that name — so the saved
	// record and the working files its model left behind are one thing to find and one thing to
	// sweep. A daemon Firing had no scratch dir at all before: nothing here minted a session id, so
	// its model was offered no writable scratch and put its working files wherever else it could
	// reach. Minting per Firing rather than per daemon also keeps two schedules that fire on the
	// same minute out of each other's files.
	recordID := session.NewID(time.Now())

	// The construction surface every unattended run shares (wire_firing.go), reached from this
	// Driver's own facts: the bound entry, that entry's roots, the daemon's own key resolver — so an
	// `api-key-cmd:` runs once per entry rather than once per Firing — and the mode the Schedule
	// fired, which is the FIRING's and never re-read off the entry (ADR 0033, decision 3).
	//
	// The `model:` overlay is handed over as the entry states it. It is legal here because
	// validation already refused it where a model name would be a request to ACTUATE a load rather
	// than a per-request selection (ADR 0055 decision 2); absent, the composer falls back to the
	// bound entry's own `model:` — and on a launcher-fronted server that is whatever is serving,
	// because the daemon never actuates the launcher (decision 3): nothing serving means this Firing
	// fails visibly in its record and the next tick behaves normally.
	//
	// No skills catalog and no width source: a daemon holds no longer-lived provider to share and
	// has no heartbeat to take a slot count off, which is exactly what the composer's nil defaults
	// answer with. The rebind notices are dropped — they are a launch's narration, and a Firing's
	// narration is the session record it leaves behind.
	cfg, _, err := firingConfig(ctx, firingInputs{
		opts:      w.opts,
		entry:     server,
		keys:      w.keys,
		roots:     roots,
		manualIDs: w.manualIDs,
		confiner:  w.confiner,
		model:     entry.Run.Model,
		mode:      f.Mode,
		recordID:  recordID,
	})
	if err != nil {
		return schedule.Outcome{}, fmt.Errorf("apogee: daemon: resolve the %q schedule's bindings: %w", entry.Name, err)
	}

	// Through the package's runner seam (headless.go) rather than run.Once directly: production
	// never reassigns it, so this is the same call, and it is what lets a test read the Config a
	// Firing composed without a live model.
	res, err := runOnce(ctx, run.Spec{
		Config:       cfg,
		Prompt:       f.Prompt,
		ScheduleID:   f.ScheduleID,
		ScheduleName: f.ScheduleName,
		Store:        w.store,
		RecordID:     recordID,
	})
	// Everything the run learned about itself, mapped onto the scheduler's report in one place so
	// both ends of this function tell the daemon's log the same story. The library reads none of it
	// — it is runner-agnostic (ADR 0033) — and the Notify line renders the Firing from these fields
	// alone: the answer without decoding a record, the counts without a second seam onto the run.
	out := schedule.Outcome{
		RecordID:  res.SessionID,
		Title:     res.Title,
		FinalText: res.FinalText,
		Turns:     res.Turns,
		Denied:    res.Denied,
		Faulted:   res.Faulted,
		Fault:     res.Fault,
	}
	if err != nil {
		// A failed Firing still reports what it salvaged: run.Once saves whatever completed before
		// it stopped, and naming that record is what lets a human open the interrupted run rather
		// than guess at it (the wording scheduleWiring.fire and runHeadless both use).
		if res.SessionID != "" {
			return out, fmt.Errorf("%w (partial run saved as %s)", err, res.SessionID)
		}
		return out, err
	}
	return out, nil
}

// serverFor resolves the `servers:` entry one schedule's Firings bind to: the entry it names, or
// the startup default this host would give a fresh session when it names none (ADR 0055 decision
// 1). A name no entry answers to is already refused when the schedules file is validated, so
// reaching that branch means `config.yaml` and the adopted set have drifted apart — which the
// Firing reports rather than silently firing at the wrong server.
func (w *daemonWiring) serverFor(entry daemon.Entry) (config.ServerEntry, error) {
	named := strings.TrimSpace(entry.Run.Server)
	if named == "" {
		return startupEntry(w.opts), nil
	}
	for _, server := range w.opts.Servers {
		if server.Name == named {
			return server, nil
		}
	}
	return config.ServerEntry{}, fmt.Errorf("apogee: daemon: the %q schedule binds to server %q, which no servers: "+
		"entry in config.yaml answers to — the daemon reads config.yaml once at startup, so restart it after "+
		"editing that list", entry.Name, named)
}
