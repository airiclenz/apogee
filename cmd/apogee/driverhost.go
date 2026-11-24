package main

// The Driver host: the process-scope steps every Driver — the TUI, `apogee headless`, the daemon —
// takes before its first session or Firing, built once here instead of three times beside each
// Driver (adr-0083-amend-1123, which reopened ADR 0083 part 5 for exactly these steps). It opens
// the state roots and sweeps the stale scratch dirs, builds the host's Confiner and the per-server
// stats recorder, opens the home's session store with its share warning and its session and
// snapshot sweeps, and tears the confiner and the recorder down again in one close.
//
// What it does NOT own is as deliberate as what it does. It wraps no sink — each Driver wraps its
// own Events in the recorder (ADR 0103 D1, ADR 0075 D9) — and it judges no confinement posture: it
// hands each Driver the posture as a value (confinementPosture) and the Driver rules on it (the TUI
// degrades, headless refuses, the daemon latches). The key resolver, the skills, the Bridge and the
// Firing composer stay the Drivers' own (the amendment's part (d)).

import (
	"io"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// prewarmLabelWalk is the seam onto the Windows label-walk pre-warm, for the same reason hardExit
// is one: platform.PrewarmLabelWalk is an empty function off Windows
// (internal/platform/prewarm_other.go), so a test that only asserted "the run made no noise" would
// pass identically against a tree that never calls it at all. Replacing this variable is how the
// suite proves a confined-Auto boot reaches the pre-warm on the one host where it does work.
// Production never reassigns it.
var prewarmLabelWalk = platform.PrewarmLabelWalk

// driverHost holds the process-scope facilities one Driver opens at start-up and keeps for its
// whole life. A nil *driverHost is valid for close, which is what lets a Driver defer the teardown
// before it knows whether the host opened.
type driverHost struct {
	// roots are the state roots this Driver lives in.
	roots stateRoots
	// files is the config source the roots' Project root came from — zero for a Driver opened
	// without one (the daemon, whose Project root is each Firing's own).
	files config.Files
	// confiner is the host's confinement backend, built once for the process.
	confiner domain.Confiner
	// stats is the per-server stats recorder; each Driver wraps its own sink in it.
	stats *statsRecorder
	// sessions is the retention and share policy the session store is opened and swept under.
	sessions config.SessionSettings
}

// confinementPosture is what the host's confinement backend says about one mode under one
// `confine-to-workspace:` value: the backend's label and capabilities, and each sentence the
// posture owes ("" when it owes none). It is a value and judges nothing — whether a sentence is a
// notice, a refusal or a once-per-workspace latch is each Driver's own call.
type confinementPosture struct {
	// backend is the backend's label (probe.BackendName).
	backend string
	// caps is the backend's capability matrix, the one probe.AutoUnattendedBlocked takes.
	caps apogee.ConfinementCaps
	// unconfined is Auto's warning when confinement is switched off, else "".
	unconfined string
	// degraded is the notice Auto owes when confinement is asked for and cannot be enforced.
	degraded string
	// residual is the disclosure of what a fencing backend knowingly leaves open.
	residual string
	// prewarm reports whether start-up should run the label walk ahead of the first command.
	prewarm bool
}

// newDriverHost builds the host's facilities over roots that are already resolved: the Confiner
// through newConfiner (nil is the production route, platform.NewConfiner) and the stats recorder
// the `server-stats:` key asks for. It sweeps nothing — openDriverHost does that — and it leaves
// files zero for the caller that holds a config source to record.
func newDriverHost(roots stateRoots, opts config.Options, newConfiner func() apogee.Confiner) *driverHost {
	if newConfiner == nil {
		newConfiner = platform.NewConfiner
	}
	return &driverHost{
		roots:    roots,
		confiner: newConfiner(),
		stats:    newStatsRecorder(serverStatsPath(roots.config), opts.ServerStats),
		sessions: opts.Sessions,
	}
}

// openDriverHost resolves this Driver's state roots, sweeps the stale scratch dirs and builds the
// host over them. withProject asks for the session's roots with the Project root the config
// source names (sessionRoots, which resolves an empty workspace to the current directory) — the TUI
// and headless; without it the roots are the apogee home's alone and carry no Project root or
// config source — the daemon, whose workspace and Project root are each Firing's.
//
// The scratch sweep runs here, before any Driver mints a dir of its own, best-effort and silent:
// GC is never a reason a Driver fails to start.
func openDriverHost(opts config.Options, withProject bool, newConfiner func() apogee.Confiner) (*driverHost, error) {
	var (
		roots stateRoots
		files config.Files
		err   error
	)
	if withProject {
		roots, files, err = sessionRoots(opts)
	} else {
		roots, err = resolveRoots(opts.ConfigDir, "")
	}
	if err != nil {
		return nil, err
	}

	gcScratchDirs(roots.scratch, time.Now())

	h := newDriverHost(roots, opts, newConfiner)
	h.files = files
	return h, nil
}

// openStore opens the home's session store and runs its two boot sweeps. With
// `sessions.share-group:` set the store writes every file for that group, and the warning a
// group-readable config.yaml owes is said through say. keep, when set, is handed the open store
// and names the session ids the sweeps must spare — the record a --resume or --continue start
// resolved; its error is returned as the start-up error. The session sweep runs before the
// snapshot sweep, so a record it just pruned is already gone when the snapshot sweep looks for it.
func (h *driverHost) openStore(say func(tui.Notice), keep func(*session.Store) ([]string, error)) (*session.Store, error) {
	storeOpts, shareWarning, err := sessionStoreOptions(h.sessions, h.roots.config)
	if err != nil {
		return nil, err
	}
	if shareWarning != "" && say != nil {
		say(tui.Notice{Text: shareWarning, Kind: tui.NoticeShareGroupConfig})
	}
	store := session.NewStore(h.roots.sessions, storeOpts...)

	var kept []string
	if keep != nil {
		if kept, err = keep(store); err != nil {
			return nil, err
		}
	}
	gcSessions(store, h.sessions, kept...)
	gcSnapshotDirs(h.roots.snapshots, store, time.Now(), kept...)
	return store, nil
}

// posture reads the host's confinement backend for mode under confine (`confine-to-workspace:`)
// and answers every sentence that posture owes, judging none of them.
func (h *driverHost) posture(mode apogee.Mode, confine bool) confinementPosture {
	backend := probe.BackendName(h.confiner)
	caps := h.confiner.Capabilities()
	p := confinementPosture{
		backend:  backend,
		caps:     caps,
		degraded: probe.DegradedNotice(backend, caps, mode, confine),
		residual: probe.ResidualNotice(backend, caps, mode, confine),
		prewarm:  shouldPrewarmLabelWalk(mode, confine, caps.FSWrite),
	}
	if mode == domain.ModeAuto && !confine {
		p.unconfined = unconfinedAutoWarning
	}
	return p
}

// prewarm runs the confinement backend's label walk over workspace now, its progress notice on
// out (ADR 0020 §2), through the prewarmLabelWalk seam. The caller has asked posture first.
func (h *driverHost) prewarm(workspace string, out io.Writer) {
	prewarmLabelWalk(h.confiner, workspace, out)
}

// close stops the stats recorder and tears the confinement backend down, and returns the one
// sentence a teardown that could not put the disk back owes ("" otherwise) — the Driver says it on
// its own stream. Only the Windows token backend has a teardown (ADR 0020 §2); domain.Confiner
// grows no Close for one OS, so it is an optional-interface assertion here. It is nil-safe.
func (h *driverHost) close() string {
	if h == nil {
		return ""
	}
	h.stats.close()
	closer, ok := h.confiner.(interface{ Close() error })
	if !ok {
		return ""
	}
	return platform.ConfinementTeardownNotice(closer.Close())
}
